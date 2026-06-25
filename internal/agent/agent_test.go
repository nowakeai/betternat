package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/ha"
	"github.com/nowakeai/betternat/internal/lease"
)

type fakeFactory struct {
	engine datapath.Engine
}

func (f fakeFactory) NewEngine(config.DatapathConfig) (datapath.Engine, error) {
	return f.engine, nil
}

type fakeEngine struct {
	reconciled     bool
	reconcileCount int
	onReconcile    func()
}

func (e *fakeEngine) Name() string { return "fake" }

func (e *fakeEngine) EnsureReady(context.Context, config.DatapathConfig) error { return nil }

func (e *fakeEngine) Reconcile(context.Context, config.DatapathConfig) error {
	e.reconciled = true
	e.reconcileCount++
	if e.onReconcile != nil {
		e.onReconcile()
	}
	return nil
}

func (e *fakeEngine) Status(context.Context) (datapath.Status, error) {
	return datapath.Status{Ready: e.reconciled, Engine: e.Name(), Message: "ready"}, nil
}

func (e *fakeEngine) Counters(context.Context) (datapath.Counters, error) {
	return datapath.Counters{
		Rules: []datapath.RuleCounter{
			{CIDR: "10.0.0.0/8", Packets: 12, Bytes: 3456},
		},
	}, nil
}

func (e *fakeEngine) ConntrackSummary(context.Context) (datapath.ConntrackSummary, error) {
	return datapath.ConntrackSummary{
		Entries:     3,
		Established: map[string]uint64{"tcp": 2},
		UDPEntries:  1,
	}, nil
}

func (e *fakeEngine) Cleanup(context.Context) error { return nil }

type blockingStatusEngine struct {
	fakeEngine
}

func (e blockingStatusEngine) Status(ctx context.Context) (datapath.Status, error) {
	<-ctx.Done()
	return datapath.Status{}, ctx.Err()
}

type fakeInstancePreparer struct {
	instanceID string
}

func (p *fakeInstancePreparer) DisableSourceDestCheck(_ context.Context, instanceID string) error {
	p.instanceID = instanceID
	return nil
}

type fakeHASupervisorFactory struct {
	supervisor *fakeHASupervisor
	cfg        config.Config
	engine     datapath.Engine
	reporter   ha.StatusReporter
}

func (f *fakeHASupervisorFactory) NewSupervisor(_ context.Context, cfg config.Config, engine datapath.Engine, reporter ha.StatusReporter) (HASupervisor, error) {
	f.cfg = cfg
	f.engine = engine
	f.reporter = reporter
	if f.supervisor == nil {
		f.supervisor = &fakeHASupervisor{}
	}
	return f.supervisor, nil
}

type fakeHASupervisor struct {
	run         bool
	instanceID  string
	interval    int64
	cancelOnRun context.CancelFunc
}

func (s *fakeHASupervisor) Run(ctx context.Context, _ config.Config, localInstanceID string, interval time.Duration) error {
	s.run = true
	s.instanceID = localInstanceID
	s.interval = int64(interval)
	if s.cancelOnRun != nil {
		s.cancelOnRun()
	}
	<-ctx.Done()
	return nil
}

type fakeTerminationWatcher struct {
	action cloud.LifecycleAction
	err    error
}

func (w fakeTerminationWatcher) Run(context.Context) (cloud.LifecycleAction, error) {
	return w.action, w.err
}

type fakeLifecycleCompleter struct {
	action cloud.LifecycleAction
	cancel context.CancelFunc
	err    error
}

func (c *fakeLifecycleCompleter) CompleteLifecycleAction(_ context.Context, action cloud.LifecycleAction) error {
	c.action = action
	if c.cancel != nil {
		c.cancel()
	}
	return c.err
}

type fakeStatusReporter struct {
	snapshot ha.StatusSnapshot
}

func (r fakeStatusReporter) Snapshot() ha.StatusSnapshot {
	return r.snapshot
}

type fakeHandoverStore struct {
	current      lease.Record
	records      map[string]coordination.HandoverRecord
	createErr    error
	missFirstGet bool
	getCalls     int
	created      []coordination.HandoverRecord
	updated      []coordination.HandoverRecord
}

func (s *fakeHandoverStore) CreateHandover(_ context.Context, record coordination.HandoverRecord, _ time.Duration) (coordination.HandoverRecord, error) {
	if s.createErr != nil {
		return coordination.HandoverRecord{}, s.createErr
	}
	if s.records == nil {
		s.records = map[string]coordination.HandoverRecord{}
	}
	s.records[record.RequestID] = record
	s.created = append(s.created, record)
	return record, nil
}

func (s *fakeHandoverStore) UpdateHandover(_ context.Context, record coordination.HandoverRecord, _ time.Duration) (coordination.HandoverRecord, error) {
	if s.records == nil {
		s.records = map[string]coordination.HandoverRecord{}
	}
	s.records[record.RequestID] = record
	s.updated = append(s.updated, record)
	return record, nil
}

func (s *fakeHandoverStore) GetHandover(_ context.Context, requestID string) (coordination.HandoverRecord, error) {
	s.getCalls++
	if s.missFirstGet && s.getCalls == 1 {
		return coordination.HandoverRecord{}, os.ErrNotExist
	}
	if s.records == nil {
		return coordination.HandoverRecord{}, os.ErrNotExist
	}
	record, ok := s.records[requestID]
	if !ok {
		return coordination.HandoverRecord{}, os.ErrNotExist
	}
	return record, nil
}

func (s *fakeHandoverStore) Current(context.Context) (lease.Record, error) {
	if s.current.OwnerInstanceID == "" {
		return lease.Record{}, os.ErrNotExist
	}
	return s.current, nil
}

func TestRuntimeVersionDoesNotRequireConfig(t *testing.T) {
	var out bytes.Buffer
	runtime := Runtime{Stdout: &out}
	if err := runtime.Run(context.Background(), Options{Version: true}); err != nil {
		t.Fatalf("runtime version: %v", err)
	}
	if !strings.Contains(out.String(), "betternat-agent version=dev") {
		t.Fatalf("unexpected version output: %s", out.String())
	}
}

func TestRuntimeRunOnceReconcilesDatapath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	engine := &fakeEngine{}
	var out bytes.Buffer
	runtime := Runtime{Factory: fakeFactory{engine: engine}, Stdout: &out}

	if err := runtime.Run(context.Background(), Options{ConfigPath: configPath, Once: true}); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if !engine.reconciled {
		t.Fatal("engine was not reconciled")
	}
	if !bytes.Contains(out.Bytes(), []byte(`"gateway_id":"prod-egress"`)) {
		t.Fatalf("missing gateway id in output: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"engine":"fake"`)) {
		t.Fatalf("missing datapath status in output: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"packets":12`)) {
		t.Fatalf("missing datapath counters in output: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"udp_entries":1`)) {
		t.Fatalf("missing conntrack summary in output: %s", out.String())
	}
}

func TestRuntimeContinuousStartsHASupervisorWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validHAConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	supervisor := &fakeHASupervisor{cancelOnRun: cancel}
	factory := &fakeHASupervisorFactory{supervisor: supervisor}
	engine := &fakeEngine{}
	runtime := Runtime{
		Factory:              fakeFactory{engine: engine},
		HASupervisorFactory:  factory,
		Stdout:               ioDiscard{},
		MetricsListenAddress: "127.0.0.1:0",
		DisableMetricsServer: true,
		DisableTermination:   true,
	}

	if err := runtime.Run(ctx, Options{ConfigPath: configPath}); err != nil {
		t.Fatalf("runtime continuous HA: %v", err)
	}
	if !supervisor.run {
		t.Fatal("HA supervisor was not started")
	}
	if supervisor.instanceID != "i-local" {
		t.Fatalf("unexpected local instance id: %q", supervisor.instanceID)
	}
	if factory.engine != engine {
		t.Fatal("HA supervisor did not receive datapath engine")
	}
	if engine.reconcileCount != 0 {
		t.Fatalf("plain reconcile loop should not run before HA supervisor, got %d", engine.reconcileCount)
	}
}

func TestDatapathStatusForRegistryTimesOut(t *testing.T) {
	start := time.Now()
	status := datapathStatusForRegistry(context.Background(), &blockingStatusEngine{}, 10*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("registry datapath status should not block on a stuck engine")
	}
	if status.Ready {
		t.Fatalf("stuck engine should not report ready: %#v", status)
	}
	if status.Engine != "fake" {
		t.Fatalf("unexpected engine name: %#v", status)
	}
	if !strings.Contains(status.Message, "deadline exceeded") {
		t.Fatalf("unexpected timeout message: %#v", status)
	}
}

func TestPublicIPForRegistryUsesOwnedSharedEIP(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "i-active"
	snapshot := ha.StatusSnapshot{
		PublicIdentity: cloud.PublicIdentity{
			InstanceID: "i-active",
			PublicIP:   "52.24.117.43",
		},
	}
	if got := publicIPForRegistry(cfg, snapshot); got != "52.24.117.43" {
		t.Fatalf("unexpected public ip: %q", got)
	}
	snapshot.PublicIdentity.InstanceID = "i-other"
	if got := publicIPForRegistry(cfg, snapshot); got != "" {
		t.Fatalf("non-owned public identity should not publish: %q", got)
	}
}

func TestHandoverPrepareVerifiesRequesterIsActiveLeaseOwner(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "i-standby"
	store := &fakeHandoverStore{current: lease.Record{
		HAGroupID:       cfg.HAGroupID,
		OwnerInstanceID: "i-active",
		Generation:      2,
		ExpiresAt:       time.Now().Add(time.Minute),
	}}
	handler := newHandoverPrepareHandler(cfg, store)

	rejected := handler(context.Background(), agentapi.HandoverPrepareRequest{
		RequestID:       "req-1",
		SourceNodeID:    "i-other",
		TargetNodeID:    "i-standby",
		LeaseGeneration: 2,
	})
	if rejected.Error == "" {
		t.Fatalf("expected non-active requester rejection: %#v", rejected)
	}

	prepared := handler(context.Background(), agentapi.HandoverPrepareRequest{
		RequestID:       "req-1",
		SourceNodeID:    "i-active",
		TargetNodeID:    "i-standby",
		LeaseGeneration: 2,
	})
	if prepared.Status != "prepared" || prepared.Error != "" {
		t.Fatalf("expected prepared response: %#v", prepared)
	}
}

func TestPeerControlAuthenticationRequiresBearerToken(t *testing.T) {
	handler := authenticatePeer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "secret")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without bearer token, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected authorized request, got %d", rec.Code)
	}
}

func TestStandbyHandoverForwardsToActivePeer(t *testing.T) {
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.HandoverPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		var req agentapi.HandoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode forwarded request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{
			SchemaVersion:   "v1",
			RequestID:       req.RequestID,
			Status:          "completed",
			SourceNodeID:    "i-active",
			TargetNodeID:    req.TargetNodeID,
			LeaseGeneration: 3,
		})
	}))
	defer server.Close()

	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "i-standby"
	cfg.Control.PeerAPI.AuthToken = "secret"
	cache := newControlStatusCache(cfg)
	cache.status.RouteTarget = "i-active"
	cache.status.Cache.Mode = "cached"
	cache.status.Instances = []agentapi.StatusInstance{
		{NodeID: "i-active", Role: "active", Fresh: true, ControlURL: server.URL},
		{NodeID: "i-standby", Role: "standby", Fresh: true},
	}
	handler := newHandoverHandler(cfg, cache, fakeStatusReporter{}, nil)

	resp := handler(context.Background(), agentapi.HandoverRequest{
		RequestID:    "req-1",
		TargetNodeID: "auto",
		Reason:       "test",
	})
	if resp.Status != "completed" || resp.SourceNodeID != "i-active" {
		t.Fatalf("unexpected forwarded response: %#v", resp)
	}
	if sawAuth != "Bearer secret" {
		t.Fatalf("missing peer auth header: %q", sawAuth)
	}
}

func TestActiveHandoverUsesFreshLeaseOverStaleStatusCache(t *testing.T) {
	forwarded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded = true
		_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{Status: "completed"})
	}))
	defer server.Close()

	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "i-active"
	cfg.Control.PeerAPI.AuthToken = "secret"
	store := &fakeHandoverStore{
		current: lease.Record{
			HAGroupID:       cfg.HAGroupID,
			OwnerInstanceID: "i-active",
			Generation:      9,
			ExpiresAt:       time.Now().Add(time.Minute),
		},
	}
	cache := newControlStatusCache(cfg)
	cache.status.RouteTarget = "i-old-active"
	cache.status.Cache.Mode = "cached"
	cache.status.Instances = []agentapi.StatusInstance{
		{NodeID: "i-old-active", Role: "active", Fresh: true, ControlURL: server.URL},
		{NodeID: "i-active", Role: "active", Health: "Healthy", Fresh: true},
	}
	reporter := fakeStatusReporter{snapshot: ha.StatusSnapshot{Lease: store.current}}
	handler := newHandoverHandler(cfg, cache, reporter, store)

	resp := handler(context.Background(), agentapi.HandoverRequest{
		RequestID:    "req-lease-wins",
		TargetNodeID: "auto",
		Reason:       "test",
	})
	if forwarded {
		t.Fatal("active daemon forwarded based on stale status cache")
	}
	if strings.Contains(resp.Error, "local daemon is not the active route target") {
		t.Fatalf("fresh lease should prevent stale-cache forwarding: %#v", resp)
	}
	if len(store.created) != 1 || store.created[0].LeaseGeneration != 9 {
		t.Fatalf("handover should use fresh lease record: created=%#v", store.created)
	}
}

func TestHandoverRequestIDReturnsExistingDurableRecord(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store := &fakeHandoverStore{records: map[string]coordination.HandoverRecord{
		"req-1": {
			RequestID:       "req-1",
			Status:          "completed",
			SourceNodeID:    "i-active",
			TargetNodeID:    "i-standby",
			LeaseGeneration: 4,
			Message:         "already done",
		},
	}}
	cache := newControlStatusCache(cfg)
	handler := newHandoverHandler(cfg, cache, fakeStatusReporter{}, store)

	resp := handler(context.Background(), agentapi.HandoverRequest{RequestID: "req-1", TargetNodeID: "auto"})
	if resp.Status != "completed" || resp.LeaseGeneration != 4 || resp.Message != "already done" {
		t.Fatalf("expected existing durable handover response: %#v", resp)
	}
	if len(store.created) != 0 || len(store.updated) != 0 {
		t.Fatalf("duplicate request should not create or update records: created=%#v updated=%#v", store.created, store.updated)
	}
}

func TestHandoverCreateConflictReturnsExistingDurableRecord(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.NodeID = "i-active"
	store := &fakeHandoverStore{
		createErr:    errors.New("conditional check failed"),
		missFirstGet: true,
		current: lease.Record{
			HAGroupID:       cfg.HAGroupID,
			OwnerInstanceID: "i-active",
			Generation:      3,
			ExpiresAt:       time.Now().Add(time.Minute),
		},
		records: map[string]coordination.HandoverRecord{
			"req-1": {
				RequestID:       "req-1",
				Status:          "committing",
				SourceNodeID:    "i-active",
				TargetNodeID:    "i-standby",
				LeaseGeneration: 3,
			},
		},
	}
	cache := newControlStatusCache(cfg)
	cache.status.RouteTarget = "i-active"
	cache.status.Cache.Mode = "cached"
	cache.status.Instances = []agentapi.StatusInstance{
		{NodeID: "i-active", Role: "active", Health: "Healthy", Fresh: true},
		{NodeID: "i-standby", Role: "standby", Health: "Healthy", Fresh: true},
	}
	reporter := fakeStatusReporter{snapshot: ha.StatusSnapshot{Lease: store.current}}
	handler := newHandoverHandler(cfg, cache, reporter, store)

	resp := handler(context.Background(), agentapi.HandoverRequest{RequestID: "req-1", TargetNodeID: "auto"})
	if resp.Status != "committing" || resp.TargetNodeID != "i-standby" {
		t.Fatalf("expected existing durable record after create conflict: %#v", resp)
	}
	if len(store.updated) != 0 {
		t.Fatalf("create conflict should not continue operation: %#v", store.updated)
	}
}

func TestParseArgsRequiresConfig(t *testing.T) {
	_, err := parseArgs([]string{"--once"})
	if err == nil {
		t.Fatal("expected config error")
	}
}

func TestRuntimeValidateOnlyDoesNotReconcile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	engine := &fakeEngine{}
	var out bytes.Buffer
	runtime := Runtime{Factory: fakeFactory{engine: engine}, Stdout: &out}

	if err := runtime.Run(context.Background(), Options{ConfigPath: configPath, ValidateOnly: true}); err != nil {
		t.Fatalf("runtime validate-only: %v", err)
	}
	if engine.reconciled {
		t.Fatal("engine should not be reconciled during validate-only")
	}
	if !bytes.Contains(out.Bytes(), []byte(`"status":"valid"`)) {
		t.Fatalf("missing valid output: %s", out.String())
	}
}

func TestRuntimePreparesAWSAutoInstance(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"local": {"primary_interface": "ens5"}`, `"local": {"instance_id":"auto","primary_interface": "ens5"}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	engine := &fakeEngine{}
	preparer := &fakeInstancePreparer{}
	var out bytes.Buffer
	runtime := Runtime{
		Factory:          fakeFactory{engine: engine},
		InstancePreparer: preparer,
		ResolveInstanceID: func(context.Context, string) (string, error) {
			return "i-local", nil
		},
		Stdout: &out,
	}

	if err := runtime.Run(context.Background(), Options{ConfigPath: configPath, Once: true}); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if preparer.instanceID != "i-local" {
		t.Fatalf("source/dest check was not disabled for resolved instance: %#v", preparer)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"node":"i-local"`)) {
		t.Fatalf("runtime output should use resolved instance id: %s", out.String())
	}
}

func TestRuntimeResolvesAutoSharedEIP(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validHAConfigJSON(), `"allocation_id": "eipalloc-123"`, `"allocation_id": "auto"`, 1)
	raw = strings.Replace(raw, `"local": {"instance_id":"i-local","primary_interface": "ens5"}`, `"local": {"instance_id":"auto","availability_zone":"us-west-2a","primary_interface": "ens5"}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	supervisor := &fakeHASupervisor{cancelOnRun: cancel}
	factory := &fakeHASupervisorFactory{supervisor: supervisor}
	preparer := &fakeInstancePreparer{}
	runtime := Runtime{
		Factory:             fakeFactory{engine: &fakeEngine{}},
		HASupervisorFactory: factory,
		InstancePreparer:    preparer,
		ResolveInstanceID: func(context.Context, string) (string, error) {
			return "i-local", nil
		},
		ResolveSharedEIP: func(_ context.Context, region string, gatewayID string, az string) (string, error) {
			if region != "us-west-2" || gatewayID != "prod-egress" || az != "us-west-2a" {
				t.Fatalf("unexpected resolver input: region=%s gateway=%s az=%s", region, gatewayID, az)
			}
			return "eipalloc-resolved", nil
		},
		Stdout:               ioDiscard{},
		MetricsListenAddress: "127.0.0.1:0",
		DisableMetricsServer: true,
		DisableTermination:   true,
	}

	if err := runtime.Run(ctx, Options{ConfigPath: configPath}); err != nil {
		t.Fatalf("runtime continuous HA: %v", err)
	}
	if factory.cfg.HA.PublicIdentity.AllocationID != "eipalloc-resolved" {
		t.Fatalf("shared EIP was not resolved: %#v", factory.cfg.HA.PublicIdentity)
	}
}

func TestRuntimeCompletesLifecycleActionAfterTerminationEvent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validHAConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	supervisor := &fakeHASupervisor{}
	factory := &fakeHASupervisorFactory{supervisor: supervisor}
	ctx, cancel := context.WithCancel(context.Background())
	completer := &fakeLifecycleCompleter{cancel: cancel}
	runtime := Runtime{
		Factory:             fakeFactory{engine: &fakeEngine{}},
		HASupervisorFactory: factory,
		TerminationWatcher: fakeTerminationWatcher{action: cloud.LifecycleAction{
			AutoScalingGroupName: "betternat-prod-egress-us-west-2a",
			LifecycleHookName:    "betternat-prod-egress-us-west-2a-terminating",
			InstanceID:           "i-local",
			Reason:               "test",
		}},
		LifecycleCompleter:   completer,
		Stdout:               ioDiscard{},
		MetricsListenAddress: "127.0.0.1:0",
		DisableMetricsServer: true,
	}

	if err := runtime.Run(ctx, Options{ConfigPath: configPath}); err != nil {
		t.Fatalf("runtime continuous HA: %v", err)
	}
	if completer.action.InstanceID != "i-local" {
		t.Fatalf("lifecycle action was not completed: %#v", completer.action)
	}
}

func TestWatchGracefulStopRunsHandoverBeforeCancel(t *testing.T) {
	parent, stop := context.WithCancel(context.Background())
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	called := make(chan agentapi.HandoverRequest, 1)
	watchGracefulStop(parent, cancelRun, func(_ context.Context, req agentapi.HandoverRequest) agentapi.HandoverResponse {
		called <- req
		return agentapi.HandoverResponse{SchemaVersion: "v1", Status: "completed"}
	})

	stop()
	select {
	case req := <-called:
		if req.TargetNodeID != "auto" || req.Reason != "systemd-stop" {
			t.Fatalf("unexpected graceful stop handover request: %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("handover was not called")
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("run context was not cancelled")
	}
}

func TestWatchTerminationRunsHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	action := cloud.LifecycleAction{
		AutoScalingGroupName: "asg",
		LifecycleHookName:    "hook",
		InstanceID:           "i-local",
		Reason:               "spot-instance-action",
	}
	called := make(chan cloud.LifecycleAction, 1)
	actions := watchTermination(ctx, fakeTerminationWatcher{action: action}, func(action cloud.LifecycleAction) {
		called <- action
		cancel()
	})
	select {
	case got := <-actions:
		if got.Reason != "spot-instance-action" {
			t.Fatalf("unexpected lifecycle action: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("termination action was not published")
	}
	select {
	case got := <-called:
		if got.InstanceID != "i-local" {
			t.Fatalf("unexpected handler action: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("termination handler was not called")
	}
}

func TestParseArgsDefaultsToContinuous(t *testing.T) {
	opts, err := parseArgs([]string{"--config", "agent.json"})
	if err != nil {
		t.Fatalf("parse args: %v", err)
	}
	if opts.Once {
		t.Fatal("agent should default to continuous mode")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
