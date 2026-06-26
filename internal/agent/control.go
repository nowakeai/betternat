package agent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/ha"
	"github.com/nowakeai/betternat/internal/lease"
)

const (
	controlRefreshInterval = 2 * time.Second
	controlFreshFor        = 10 * time.Second
	controlHTTPTimeout     = 1500 * time.Millisecond
	handoverTimeout        = 90 * time.Second
	handoverRecordTimeout  = 10 * time.Second
)

type handoverStore interface {
	CreateHandover(context.Context, coordination.HandoverRecord, time.Duration) (coordination.HandoverRecord, error)
	UpdateHandover(context.Context, coordination.HandoverRecord, time.Duration) (coordination.HandoverRecord, error)
	GetHandover(context.Context, string) (coordination.HandoverRecord, error)
	Current(context.Context) (lease.Record, error)
}

type controlStatusCache struct {
	mu       sync.RWMutex
	status   agentapi.StatusResponse
	updated  time.Time
	warnings []string

	previous map[string]controlMetrics
}

func newControlStatusCache(cfg config.Config) *controlStatusCache {
	now := time.Now()
	return &controlStatusCache{
		status: agentapi.StatusResponse{
			SchemaVersion:    "v1",
			GeneratedAt:      now,
			GatewayID:        cfg.GatewayID,
			HAGroupID:        cfg.HAGroupID,
			Cloud:            cfg.Cloud,
			Region:           cfg.Region,
			AvailabilityZone: cfg.Local.AvailabilityZone,
			HAEnabled:        cfg.HA.Enabled,
			Datapath:         cfg.Datapath.Engine,
			Cache: agentapi.CacheInfo{
				Mode:       "warming",
				AgeSeconds: 0,
				Fresh:      false,
				UpdatedAt:  now,
			},
		},
		updated:  now,
		previous: map[string]controlMetrics{},
	}
}

func (c *controlStatusCache) get() agentapi.StatusResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := c.status
	status.GeneratedAt = time.Now()
	age := time.Since(c.updated)
	status.Cache.AgeSeconds = age.Seconds()
	status.Cache.Fresh = status.Cache.Mode == "cached" && age <= controlFreshFor
	status.Cache.UpdatedAt = c.updated
	if !status.Cache.Fresh {
		status.Warnings = append(append([]string(nil), status.Warnings...), fmt.Sprintf("status cache is stale: age %.1fs", age.Seconds()))
	}
	return status
}

func (c *controlStatusCache) refresh(ctx context.Context, cfg config.Config, registry coordination.AgentReader, engine datapath.Engine, haStatus interface{ Snapshot() ha.StatusSnapshot }, metricsAddress string) {
	now := time.Now()
	status := agentapi.StatusResponse{
		SchemaVersion:    "v1",
		GeneratedAt:      now,
		GatewayID:        cfg.GatewayID,
		HAGroupID:        cfg.HAGroupID,
		Cloud:            cfg.Cloud,
		Region:           cfg.Region,
		AvailabilityZone: cfg.Local.AvailabilityZone,
		HAEnabled:        cfg.HA.Enabled,
		Datapath:         cfg.Datapath.Engine,
		MetricsAddr:      metricsAddress,
		Cache: agentapi.CacheInfo{
			Mode:      "cached",
			Fresh:     true,
			UpdatedAt: now,
		},
	}
	snapshot := currentHASnapshot(haStatus)
	status.RouteTarget = snapshot.Lease.OwnerInstanceID
	applyLeaseStatus(&status, snapshot.Lease, snapshot)

	if registry != nil {
		c.refreshRegistryStatus(ctx, cfg, registry, snapshot.Lease, &status, now)
	} else {
		c.refreshLocalStatus(ctx, cfg, engine, snapshot, &status, now)
	}

	if len(status.Instances) == 0 {
		c.refreshLocalStatus(ctx, cfg, engine, snapshot, &status, now)
	}
	status.InstanceCount = len(status.Instances)

	c.mu.Lock()
	c.status = status
	c.updated = now
	c.mu.Unlock()
}

func (c *controlStatusCache) refreshRegistryStatus(ctx context.Context, cfg config.Config, registry coordination.AgentReader, leaseRecord lease.Record, status *agentapi.StatusResponse, now time.Time) {
	current, err := registry.Current(ctx)
	if err != nil {
		status.Warnings = append(status.Warnings, "registry lease: "+err.Error())
	} else {
		leaseRecord = current
		status.RouteTarget = current.OwnerInstanceID
		applyLeaseStatus(status, current, currentHASnapshot(nil))
	}
	agents, err := registry.ListAgents(ctx)
	if err != nil {
		status.Warnings = append(status.Warnings, "registry agents: "+err.Error())
		return
	}
	if len(agents) == 0 {
		status.Warnings = append(status.Warnings, "registry agents: no fresh records")
		return
	}
	for _, agentRecord := range agents {
		nodeID := agentRecordNodeID(agentRecord)
		row := agentapi.StatusInstance{
			NodeID:          nodeID,
			Role:            roleForInstance(nodeID, leaseRecord.OwnerInstanceID),
			Health:          healthForAgent(agentRecord),
			LifecycleState:  agentRecord.HAState,
			PrivateIP:       agentRecord.PrivateIP,
			PublicIP:        agentRecord.PublicIP,
			ControlURL:      agentRecord.ControlURL,
			Version:         agentRecord.Version,
			Metrics:         "registry",
			Fresh:           true,
			LeaseGeneration: agentRecord.LeaseGeneration,
		}
		routeTargetMatch := agentRecord.RouteTargetMatch
		row.RouteTargetMatch = &routeTargetMatch
		if !agentRecord.UpdatedAt.IsZero() {
			row.AgeSeconds = now.Sub(agentRecord.UpdatedAt).Seconds()
		}
		if row.Role == "active" && status.PublicIP == "" {
			status.PublicIP = agentRecord.PublicIP
		}
		if agentRecord.MetricsURL != "" {
			metrics, err := scrapeControlMetrics(ctx, agentRecord.MetricsURL)
			if err != nil {
				row.Metrics = "unreachable"
			} else {
				if metrics.Version != "" {
					row.Version = metrics.Version
				}
				row.RXMbps, row.TXMbps = c.rates(nodeID, metrics, now)
				row.Metrics = "ok"
			}
		}
		status.Instances = append(status.Instances, row)
	}
}

func applyLeaseStatus(status *agentapi.StatusResponse, record lease.Record, snapshot ha.StatusSnapshot) {
	if status == nil {
		return
	}
	status.LeaseGeneration = record.Generation
	if !record.ExpiresAt.IsZero() {
		status.LeaseExpiresIn = time.Until(record.ExpiresAt).Seconds()
	}
	if snapshot.HasRouteTargetCheck {
		match := snapshot.RouteTargetMatches
		status.RouteTargetMatch = &match
	}
	if snapshot.HasPublicIdentityCheck {
		match := snapshot.PublicIdentityMatches
		status.PublicIPMatch = &match
	}
}

func (c *controlStatusCache) refreshLocalStatus(ctx context.Context, cfg config.Config, engine datapath.Engine, snapshot ha.StatusSnapshot, status *agentapi.StatusResponse, now time.Time) {
	row := agentapi.StatusInstance{
		NodeID:         cfg.Local.NodeID,
		Role:           roleForInstance(cfg.Local.NodeID, snapshot.Lease.OwnerInstanceID),
		LifecycleState: string(snapshot.State),
		Version:        buildinfo.Current("betternat-agent").Version,
		Metrics:        "local",
		Fresh:          true,
	}
	if row.NodeID == "" || row.NodeID == "auto" {
		row.NodeID = "unknown"
	}
	dpStatus := datapathStatusForRegistry(ctx, engine, registryStatusTimeout)
	row.Health = "Degraded"
	if dpStatus.Ready {
		row.Health = "Healthy"
	}
	if counters, err := engine.Counters(ctx); err == nil {
		total := sumCounterBytes(counters)
		metrics := controlMetrics{RXBytes: &total, TXBytes: &total}
		row.RXMbps, row.TXMbps = c.rates(row.NodeID, metrics, now)
	} else {
		status.Warnings = append(status.Warnings, "local counters: "+err.Error())
	}
	status.Instances = append(status.Instances, row)
}

func (c *controlStatusCache) rates(key string, metrics controlMetrics, now time.Time) (float64, float64) {
	if key == "" {
		return 0, 0
	}
	previous, ok := c.previous[key]
	c.previous[key] = metrics.withObservedAt(now)
	if !ok || previous.ObservedAt.IsZero() {
		return 0, 0
	}
	elapsed := now.Sub(previous.ObservedAt)
	return controlRateMbps(previous.RXBytes, metrics.RXBytes, elapsed), controlRateMbps(previous.TXBytes, metrics.TXBytes, elapsed)
}

func runControlStatusRefresher(ctx context.Context, cache *controlStatusCache, cfg config.Config, registry coordination.AgentReader, engine datapath.Engine, haStatus interface{ Snapshot() ha.StatusSnapshot }, metricsAddress string) {
	cache.refresh(ctx, cfg, registry, engine, haStatus, metricsAddress)
	ticker := time.NewTicker(controlRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, controlRefreshInterval)
			cache.refresh(refreshCtx, cfg, registry, engine, haStatus, metricsAddress)
			cancel()
		}
	}
}

func controlHandler(cache *controlStatusCache, handover func(context.Context, agentapi.HandoverRequest) agentapi.HandoverResponse, prepare func(context.Context, agentapi.HandoverPrepareRequest) agentapi.HandoverResponse) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc(agentapi.StatusPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cache.get()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc(agentapi.HandoverPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{SchemaVersion: "v1", Status: "idle"})
		case http.MethodPost:
			if handover == nil {
				http.Error(w, "handover is not available", http.StatusServiceUnavailable)
				return
			}
			var req agentapi.HandoverRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			handoverCtx, cancel := detachedHandoverContext()
			defer cancel()
			resp := handover(handoverCtx, req)
			if resp.Error != "" {
				w.WriteHeader(http.StatusConflict)
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(agentapi.HandoverPreparePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if prepare == nil {
			http.Error(w, "handover prepare is not available", http.StatusServiceUnavailable)
			return
		}
		var req agentapi.HandoverPrepareRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := prepare(r.Context(), req)
		if resp.Error != "" {
			w.WriteHeader(http.StatusConflict)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func detachedHandoverContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), handoverTimeout+handoverRecordTimeout)
}

func newHandoverHandler(cfg config.Config, cache *controlStatusCache, haStatus interface{ Snapshot() ha.StatusSnapshot }, store handoverStore) func(context.Context, agentapi.HandoverRequest) agentapi.HandoverResponse {
	return func(ctx context.Context, req agentapi.HandoverRequest) agentapi.HandoverResponse {
		source := cfg.Local.NodeID
		if req.RequestID == "" {
			req.RequestID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		resp := agentapi.HandoverResponse{
			SchemaVersion: "v1",
			RequestID:     req.RequestID,
			Status:        "rejected",
			SourceNodeID:  source,
		}
		if existing, ok := existingHandover(ctx, store, req.RequestID); ok {
			return handoverResponseFromRecord(existing)
		}
		if source == "" || source == "auto" {
			resp.Error = "local node id is not resolved"
			return resp
		}
		status := cache.get()
		current := currentHASnapshot(haStatus).Lease
		if store != nil {
			fresh, err := store.Current(ctx)
			if err != nil {
				resp.Error = "read current lease: " + err.Error()
				return resp
			}
			current = fresh
			status.RouteTarget = fresh.OwnerInstanceID
		}
		if status.RouteTarget != source {
			forwarded, err := forwardHandoverToActive(ctx, cfg, status, req)
			if err != nil {
				resp.Error = "local daemon is not the active route target: " + err.Error()
				return resp
			}
			return forwarded
		}
		if err := createHandover(ctx, store, coordination.HandoverRecord{
			RequestID:       req.RequestID,
			Status:          "requested",
			SourceNodeID:    source,
			Reason:          req.Reason,
			LeaseGeneration: current.Generation,
		}, cfg); err != nil {
			if existing, ok := existingHandover(ctx, store, req.RequestID); ok {
				return handoverResponseFromRecord(existing)
			}
			resp.Error = "create durable handover record: " + err.Error()
			return resp
		}
		target, err := selectHandoverTarget(status, source, requestedTargetNode(req))
		if err != nil {
			resp.Error = err.Error()
			updateHandover(ctx, store, coordination.HandoverRecord{
				RequestID:       req.RequestID,
				Status:          "rejected",
				SourceNodeID:    source,
				Reason:          req.Reason,
				LeaseGeneration: current.Generation,
				Error:           resp.Error,
			}, cfg)
			return resp
		}
		resp.TargetNodeID = target
		record := coordination.HandoverRecord{
			RequestID:       req.RequestID,
			Status:          "preparing",
			SourceNodeID:    source,
			TargetNodeID:    target,
			Reason:          req.Reason,
			LeaseGeneration: current.Generation,
		}
		updateHandover(ctx, store, record, cfg)
		if err := prepareHandoverTarget(ctx, cfg, status, req, source, target, current.Generation); err != nil {
			resp.Error = err.Error()
			record.Status = "rejected"
			record.Error = resp.Error
			updateHandover(ctx, store, record, cfg)
			return resp
		}
		controller, err := defaultHandoverController(ctx, cfg)
		if err != nil {
			resp.Error = err.Error()
			record.Status = "rejected"
			record.Error = resp.Error
			updateHandover(ctx, store, record, cfg)
			return resp
		}
		record.Status = "committing"
		updateHandover(ctx, store, record, cfg)
		handoverCtx, cancel := context.WithTimeout(ctx, handoverTimeout)
		defer cancel()
		result, err := controller.Handover(handoverCtx, cfg, source, target, current)
		if err != nil {
			resp.Error = err.Error()
			if result.Reverted {
				resp.Message = "handover failed after cloud mutation; ownership was reverted"
			}
			record.Status = "failed"
			record.Error = resp.Error
			record.Message = resp.Message
			updateHandover(ctx, store, record, cfg)
			return resp
		}
		resp.Status = "completed"
		resp.LeaseGeneration = result.NewLease.Generation
		resp.Message = "handover completed"
		record.Status = "completed"
		record.LeaseGeneration = result.NewLease.Generation
		record.Message = resp.Message
		updateHandover(ctx, store, record, cfg)
		return resp
	}
}

func newHandoverPrepareHandler(cfg config.Config, store handoverStore) func(context.Context, agentapi.HandoverPrepareRequest) agentapi.HandoverResponse {
	return func(ctx context.Context, req agentapi.HandoverPrepareRequest) agentapi.HandoverResponse {
		resp := agentapi.HandoverResponse{
			SchemaVersion:   "v1",
			RequestID:       req.RequestID,
			Status:          "rejected",
			SourceNodeID:    requestSourceNode(req),
			TargetNodeID:    requestTargetNode(req),
			LeaseGeneration: req.LeaseGeneration,
		}
		if requestTargetNode(req) != cfg.Local.NodeID {
			resp.Error = "handover prepare target does not match local node"
			return resp
		}
		if store == nil {
			resp.Error = "coordination backend is required for peer handover prepare"
			return resp
		}
		current, err := store.Current(ctx)
		if err != nil {
			resp.Error = "read current lease: " + err.Error()
			return resp
		}
		if current.OwnerInstanceID != requestSourceNode(req) || current.Generation != req.LeaseGeneration {
			resp.Error = "handover requester is not the current active lease owner"
			return resp
		}
		resp.Status = "prepared"
		resp.Message = "target verified requester ownership"
		return resp
	}
}

func existingHandover(ctx context.Context, store handoverStore, requestID string) (coordination.HandoverRecord, bool) {
	if store == nil || requestID == "" {
		return coordination.HandoverRecord{}, false
	}
	record, err := store.GetHandover(ctx, requestID)
	return record, err == nil
}

func createHandover(ctx context.Context, store handoverStore, record coordination.HandoverRecord, cfg config.Config) error {
	if store == nil {
		return nil
	}
	_, err := store.CreateHandover(ctx, record, handoverTTL(cfg))
	return err
}

func updateHandover(ctx context.Context, store handoverStore, record coordination.HandoverRecord, cfg config.Config) {
	if store == nil {
		return
	}
	if _, err := store.UpdateHandover(ctx, record, handoverTTL(cfg)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: update handover record: %v\n", err)
	}
}

func handoverResponseFromRecord(record coordination.HandoverRecord) agentapi.HandoverResponse {
	return agentapi.HandoverResponse{
		SchemaVersion:   "v1",
		RequestID:       record.RequestID,
		Status:          record.Status,
		SourceNodeID:    handoverRecordSourceNodeID(record),
		TargetNodeID:    handoverRecordTargetNodeID(record),
		LeaseGeneration: record.LeaseGeneration,
		Message:         record.Message,
		Error:           record.Error,
	}
}

func forwardHandoverToActive(ctx context.Context, cfg config.Config, status agentapi.StatusResponse, req agentapi.HandoverRequest) (agentapi.HandoverResponse, error) {
	if cfg.Control.PeerAPI.AuthToken == "" {
		return agentapi.HandoverResponse{}, fmt.Errorf("peer API auth token is not configured")
	}
	active := findStatusInstance(status, status.RouteTarget)
	if active.ControlURL == "" {
		return agentapi.HandoverResponse{}, fmt.Errorf("active peer control URL is not available")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(active.ControlURL, "/")+agentapi.HandoverPath, bytes.NewReader(body))
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.Control.PeerAPI.AuthToken)
	httpResp, err := (&http.Client{Timeout: handoverTimeout}).Do(httpReq)
	if err != nil {
		return agentapi.HandoverResponse{}, err
	}
	defer httpResp.Body.Close()
	var resp agentapi.HandoverResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return agentapi.HandoverResponse{}, err
	}
	return resp, nil
}

func defaultHandoverController(ctx context.Context, cfg config.Config) (ha.Controller, error) {
	cloudProvider, err := defaultCloudProvider(ctx, cfg)
	if err != nil {
		return ha.Controller{}, err
	}
	leaseManager, err := defaultLeaseManager(ctx, cfg)
	if err != nil {
		return ha.Controller{}, err
	}
	return ha.Controller{Cloud: cloudProvider, Lease: leaseManager, OwnershipMu: ownershipLock(cfg.HAGroupID)}, nil
}

var ownershipLocks sync.Map

func ownershipLock(key string) *sync.Mutex {
	if key == "" {
		key = "default"
	}
	value, _ := ownershipLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func startControlServer(ctx context.Context, socketPath string, handler http.Handler) (*http.Server, net.Listener, error) {
	if socketPath == "" {
		socketPath = agentapi.DefaultSocketPath
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		return nil, nil, err
	}
	server := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: control api: %v\n", err)
		}
	}()
	return server, listener, nil
}

func startPeerControlServer(ctx context.Context, cfg config.Config, handler http.Handler) (*http.Server, net.Listener, error) {
	if !cfg.Control.PeerAPI.Enabled {
		return nil, nil, nil
	}
	if cfg.Control.PeerAPI.AuthToken == "" {
		return nil, nil, fmt.Errorf("control.peer_api.auth_token is required when peer API is enabled")
	}
	address := cfg.Control.PeerAPI.ListenAddress
	if address == "" {
		address = "0.0.0.0"
	}
	port := cfg.Control.PeerAPI.ListenPort
	if port <= 0 {
		port = 9109
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(address, strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}
	server := &http.Server{Handler: authenticatePeer(handler, cfg.Control.PeerAPI.AuthToken)}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: peer control api: %v\n", err)
		}
	}()
	return server, listener, nil
}

func authenticatePeer(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func roleForInstance(instanceID string, owner string) string {
	if owner == "" {
		return "unknown"
	}
	if instanceID == owner {
		return "active"
	}
	return "standby"
}

func healthForAgent(agent coordination.AgentRecord) string {
	if agent.DatapathReady {
		return "Healthy"
	}
	return "Degraded"
}
