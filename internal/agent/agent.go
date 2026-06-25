package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/nowakeai/betternat/internal/cloud"
	awscloud "github.com/nowakeai/betternat/internal/cloud/aws"
	gcpcloud "github.com/nowakeai/betternat/internal/cloud/gcp"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/datapath/loxilb"
	"github.com/nowakeai/betternat/internal/datapath/nftables"
	"github.com/nowakeai/betternat/internal/ha"
	"github.com/nowakeai/betternat/internal/metrics"
)

type Options struct {
	ConfigPath   string
	Once         bool
	ValidateOnly bool
	Prometheus   bool
	Version      bool
}

type Runtime struct {
	Factory              EngineFactory
	HASupervisorFactory  HASupervisorFactory
	TerminationWatcher   TerminationWatcher
	LifecycleCompleter   LifecycleCompleter
	InstancePreparer     cloud.InstancePreparer
	ResolveInstanceID    func(context.Context, string) (string, error)
	ResolveSharedEIP     func(context.Context, string, string, string) (string, error)
	Stdout               io.Writer
	MetricsListenAddress string
	ControlSocketPath    string
	DisableMetricsServer bool
	DisableControlServer bool
	DisableTermination   bool
}

type EngineFactory interface {
	NewEngine(cfg config.DatapathConfig) (datapath.Engine, error)
}

type DefaultEngineFactory struct{}

func (DefaultEngineFactory) NewEngine(cfg config.DatapathConfig) (datapath.Engine, error) {
	switch cfg.Engine {
	case "loxilb":
		return loxilb.New(), nil
	case "nftables":
		return nftables.New(), nil
	default:
		return nil, fmt.Errorf("unsupported datapath engine %q", cfg.Engine)
	}
}

type HASupervisor interface {
	Run(context.Context, config.Config, string, time.Duration) error
}

type HASupervisorFactory interface {
	NewSupervisor(context.Context, config.Config, datapath.Engine, ha.StatusReporter) (HASupervisor, error)
}

type TerminationWatcher interface {
	Run(context.Context) (cloud.LifecycleAction, error)
}

type LifecycleCompleter interface {
	CompleteLifecycleAction(context.Context, cloud.LifecycleAction) error
}

type DefaultHASupervisorFactory struct{}

func (DefaultHASupervisorFactory) NewSupervisor(ctx context.Context, cfg config.Config, engine datapath.Engine, reporter ha.StatusReporter) (HASupervisor, error) {
	if err := validateHAConfig(cfg); err != nil {
		return nil, err
	}
	cloudProvider, err := defaultCloudProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}
	leaseManager, err := defaultLeaseManager(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return ha.Supervisor{
		Controller: ha.Controller{
			Cloud:       cloudProvider,
			Lease:       leaseManager,
			Datapath:    engine,
			OwnershipMu: ownershipLock(cfg.HAGroupID),
		},
		Reporter: reporter,
	}, nil
}

type RunResult struct {
	GatewayID string                    `json:"gateway_id"`
	HAGroupID string                    `json:"ha_group_id"`
	Node      string                    `json:"node"`
	Interface metrics.InterfaceStats    `json:"interface"`
	Datapath  datapath.Status           `json:"datapath"`
	Counters  datapath.Counters         `json:"counters"`
	Conntrack datapath.ConntrackSummary `json:"conntrack"`
}

// Run starts the BetterNAT runtime agent.
func Run(ctx context.Context, args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	runtime := Runtime{
		Factory: DefaultEngineFactory{},
		Stdout:  os.Stdout,
	}
	return runtime.Run(ctx, opts)
}

func (r Runtime) Run(ctx context.Context, opts Options) error {
	if opts.Version {
		_, _ = fmt.Fprintln(output(r.Stdout), buildinfo.Current("betternat-agent").String())
		return nil
	}
	if opts.ConfigPath == "" {
		return fmt.Errorf("config path is required")
	}
	cfg, err := config.LoadFile(opts.ConfigPath)
	if err != nil {
		return err
	}
	if opts.ValidateOnly {
		_, _ = fmt.Fprintln(output(r.Stdout), `{"status":"valid"}`)
		return nil
	}
	cfg, err = r.prepareLocalInstance(ctx, cfg)
	if err != nil {
		return err
	}
	factory := r.Factory
	if factory == nil {
		factory = DefaultEngineFactory{}
	}
	engine, err := factory.NewEngine(cfg.Datapath)
	if err != nil {
		return err
	}
	if opts.Once {
		result, err := runOnce(ctx, cfg, engine)
		if err != nil {
			return err
		}
		if opts.Prometheus {
			if err := renderPrometheusResult(output(r.Stdout), cfg, result, ha.StatusSnapshot{}); err != nil {
				return err
			}
			return nil
		}
		if err := json.NewEncoder(output(r.Stdout)).Encode(result); err != nil {
			return fmt.Errorf("encode run result: %w", err)
		}
		return nil
	}
	metricsAddress := metricsListenAddress(cfg)
	if r.MetricsListenAddress != "" {
		metricsAddress = r.MetricsListenAddress
	}
	haFactory := r.HASupervisorFactory
	if haFactory == nil {
		haFactory = DefaultHASupervisorFactory{}
	}
	return runContinuous(ctx, cfg, engine, metricsAddress, !r.DisableMetricsServer, r.ControlSocketPath, !r.DisableControlServer, haFactory, r.TerminationWatcher, r.LifecycleCompleter, !r.DisableTermination)
}

func (r Runtime) prepareLocalInstance(ctx context.Context, cfg config.Config) (config.Config, error) {
	if cfg.Local.NodeID != "auto" {
		return cfg, nil
	}
	resolve := r.ResolveInstanceID
	switch cfg.Cloud {
	case "aws":
		if resolve == nil {
			resolve = awscloud.ResolveLocalInstanceID
		}
	case "gcp":
		if resolve == nil {
			resolve = gcpcloud.ResolveLocalInstanceID
		}
	default:
		return cfg, nil
	}
	instanceID, err := resolve(ctx, cfg.Region)
	if err != nil {
		return config.Config{}, err
	}
	cfg.Local.NodeID = instanceID
	if cfg.Cloud != "aws" {
		return cfg, nil
	}
	preparer := r.InstancePreparer
	if preparer == nil {
		preparer, err = awscloud.New(ctx, cfg.Region)
		if err != nil {
			return config.Config{}, err
		}
	}
	if err := preparer.DisableSourceDestCheck(ctx, instanceID); err != nil {
		return config.Config{}, err
	}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" && cfg.HA.PublicIdentity.AllocationID == "auto" {
		resolveEIP := r.ResolveSharedEIP
		if resolveEIP == nil {
			resolveEIP = awscloud.ResolveSharedEIPAllocationID
		}
		allocationID, err := resolveEIP(ctx, cfg.Region, cfg.GatewayID, cfg.Local.AvailabilityZone)
		if err != nil {
			return config.Config{}, err
		}
		cfg.HA.PublicIdentity.AllocationID = allocationID
	}
	return cfg, nil
}

func runOnce(ctx context.Context, cfg config.Config, engine datapath.Engine) (RunResult, error) {
	if err := engine.Reconcile(ctx, cfg.Datapath); err != nil {
		return RunResult{}, fmt.Errorf("reconcile datapath %q: %w", engine.Name(), err)
	}
	status, err := engine.Status(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath status %q: %w", engine.Name(), err)
	}
	counters, err := engine.Counters(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath counters %q: %w", engine.Name(), err)
	}
	conntrack, err := engine.ConntrackSummary(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath conntrack %q: %w", engine.Name(), err)
	}
	return RunResult{
		GatewayID: cfg.GatewayID,
		HAGroupID: cfg.HAGroupID,
		Node:      cfg.Local.NodeID,
		Datapath:  status,
		Counters:  counters,
		Conntrack: conntrack,
	}, nil
}

func runContinuous(ctx context.Context, cfg config.Config, engine datapath.Engine, metricsAddress string, enableMetrics bool, controlSocketPath string, enableControl bool, haFactory HASupervisorFactory, terminationWatcher TerminationWatcher, lifecycleCompleter LifecycleCompleter, enableTermination bool) error {
	haStatus := ha.NewMemoryStatus()
	if enableMetrics {
		server, listener, err := startMetricsServer(ctx, metricsAddress, metricsHandler(cfg, engine, haStatus))
		if err != nil {
			return err
		}
		defer shutdownServer(server)
		defer listener.Close()
	}
	registry, err := defaultCoordinationStore(ctx, cfg)
	if err != nil {
		return err
	}
	if registry != nil {
		go runAgentRegistry(ctx, registry, cfg, engine, haStatus, metricsAddress)
		defer func() {
			if cfg.Local.NodeID == "" || cfg.Local.NodeID == "auto" {
				return
			}
			deleteCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := registry.DeleteAgent(deleteCtx, cfg.Local.NodeID); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: delete registry record: %v\n", err)
			}
		}()
	}
	var controlCache *controlStatusCache
	var handoverHandler func(context.Context, agentapi.HandoverRequest) agentapi.HandoverResponse
	var prepareHandler func(context.Context, agentapi.HandoverPrepareRequest) agentapi.HandoverResponse
	if enableControl || registry != nil {
		controlCache = newControlStatusCache(cfg)
		go runControlStatusRefresher(ctx, controlCache, cfg, registry, engine, haStatus, metricsAddress)
		var store handoverStore
		if registry != nil {
			store = registry
		}
		handoverHandler = newHandoverHandler(cfg, controlCache, haStatus, store)
		prepareHandler = newHandoverPrepareHandler(cfg, store)
	}
	if enableControl && controlCache != nil {
		handler := controlHandler(controlCache, handoverHandler, prepareHandler)
		server, listener, err := startControlServer(ctx, controlSocketPath, handler)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: control api disabled: %v\n", err)
		} else {
			defer shutdownServer(server)
			defer listener.Close()
		}
		peerServer, peerListener, err := startPeerControlServer(ctx, cfg, handler)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: peer control api disabled: %v\n", err)
		} else if peerServer != nil && peerListener != nil {
			defer shutdownServer(peerServer)
			defer peerListener.Close()
		}
	}

	if cfg.HA.Enabled {
		if haFactory == nil {
			return fmt.Errorf("HA supervisor factory is required")
		}
		supervisor, err := haFactory.NewSupervisor(ctx, cfg, engine, haStatus)
		if err != nil {
			return err
		}
		runCtx := ctx
		var cancel context.CancelFunc
		if registry != nil && handoverHandler != nil {
			runCtx, cancel = context.WithCancel(context.Background())
			defer cancel()
			watchGracefulStop(ctx, cancel, handoverHandler)
		}
		var lifecycleActions <-chan cloud.LifecycleAction
		if enableTermination {
			terminationWatcher, lifecycleCompleter = defaultTerminationHandling(ctx, cfg, terminationWatcher, lifecycleCompleter)
			if terminationWatcher != nil {
				if cancel == nil {
					runCtx, cancel = context.WithCancel(ctx)
					defer cancel()
				}
				lifecycleActions = watchTermination(runCtx, terminationWatcher, func(action cloud.LifecycleAction) {
					if handoverHandler != nil {
						handoverCtx, handoverCancel := context.WithTimeout(context.Background(), handoverTimeout+5*time.Second)
						req := agentapi.HandoverRequest{
							RequestID:    terminationHandoverRequestID(action),
							TargetNodeID: "auto",
							Reason:       action.Reason,
						}
						resp := handoverHandler(handoverCtx, req)
						handoverCancel()
						if resp.Error != "" {
							_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: termination handover failed: %s\n", resp.Error)
						}
					}
					cancel()
				})
			}
		}
		err = supervisor.Run(runCtx, cfg, cfg.Local.NodeID, 0)
		handledTermination := false
		if lifecycleActions != nil {
			select {
			case action := <-lifecycleActions:
				if action.AutoScalingGroupName != "" {
					handledTermination = true
					if lifecycleCompleter != nil {
						completeCtx, completeCancel := context.WithTimeout(context.Background(), lifecycleCompleteTimeout)
						if completeErr := lifecycleCompleter.CompleteLifecycleAction(completeCtx, action); completeErr != nil {
							_, _ = fmt.Fprintf(os.Stderr, "betternat-agent: complete lifecycle action after graceful release: %v\n", completeErr)
						}
						completeCancel()
					} else {
						_, _ = fmt.Fprintln(os.Stderr, "betternat-agent: lifecycle action observed but no completer is available")
					}
				}
			default:
			}
		}
		if handledTermination {
			<-ctx.Done()
		}
		return err
	}

	interval := reconcileInterval(cfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if _, err := runOnce(ctx, cfg, engine); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func renderPrometheusResult(w io.Writer, cfg config.Config, result RunResult, haStatus ha.StatusSnapshot) error {
	haState := "unknown"
	if haStatus.State != "" {
		haState = string(haStatus.State)
	}
	haStatusAge := 0.0
	haStatusStale := false
	if cfg.HA.Enabled {
		if haStatus.UpdatedAt.IsZero() {
			haStatusStale = true
		} else {
			haStatusAge = time.Since(haStatus.UpdatedAt).Seconds()
			haStatusStale = time.Since(haStatus.UpdatedAt) > haStatusStaleAfter(cfg)
		}
		if haStatusStale {
			haState = "STALE"
		}
	}
	leaseHasOwner := haStatus.Lease.OwnerInstanceID != ""
	leaseOwnerMatch := leaseHasOwner && haStatus.Lease.OwnerInstanceID == result.Node
	routeTargetMatches := haStatus.RouteTargetMatches
	publicIdentityMatches := haStatus.PublicIdentityMatches
	if haStatusStale {
		leaseOwnerMatch = false
		routeTargetMatches = false
		publicIdentityMatches = false
	}
	snapshot := metrics.Snapshot{
		GatewayID:               result.GatewayID,
		HAGroupID:               result.HAGroupID,
		Node:                    result.Node,
		AgentUp:                 true,
		Active:                  !haStatusStale && haStatus.State == ha.StateActive,
		HAState:                 haState,
		HAStatusAgeSeconds:      haStatusAge,
		HAStatusStale:           haStatusStale,
		LeaseGeneration:         haStatus.Lease.Generation,
		LeaseOwnerMatch:         leaseOwnerMatch,
		LeaseHasOwner:           leaseHasOwner,
		LeaseSecondsUntilExpiry: haStatus.SecondsUntilLeaseExpiry,
		RouteTargetMatch:        routeTargetMatches,
		RouteTargetChecked:      haStatus.HasRouteTargetCheck,
		PublicIdentityMatch:     publicIdentityMatches,
		PublicIdentityChecked:   haStatus.HasPublicIdentityCheck,
		HATakeoverAttempts:      haStatus.TakeoverAttempts,
		HATakeoverSuccesses:     haStatus.TakeoverSuccesses,
		HALeaseRenewErrors:      haStatus.LeaseRenewErrors,
		Interface:               result.Interface,
		Datapath:                result.Datapath,
		Counters:                result.Counters,
		Conntrack:               result.Conntrack,
		Owners:                  ownerCounters(cfg.Observability.Attribution.Owners, result.Counters),
		Processed:               processedCounter(result.Counters),
	}
	snapshot.Version = buildinfo.Version
	snapshot.Commit = buildinfo.Commit
	if err := metrics.RenderPrometheus(w, snapshot); err != nil {
		return fmt.Errorf("encode prometheus metrics: %w", err)
	}
	return nil
}

func haStatusStaleAfter(cfg config.Config) time.Duration {
	threshold := leaseTTL(cfg)
	if renew := leaseRenewInterval(cfg); renew*2 > threshold {
		threshold = renew * 2
	}
	if threshold <= 0 {
		return 15 * time.Second
	}
	return threshold
}

func leaseRenewInterval(cfg config.Config) time.Duration {
	if cfg.HA.Lease.RenewIntervalSeconds > 0 {
		return time.Duration(cfg.HA.Lease.RenewIntervalSeconds) * time.Second
	}
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second / 3
	}
	return 5 * time.Second
}

const lifecycleCompleteTimeout = 5 * time.Second

func startMetricsServer(ctx context.Context, address string, handler http.Handler) (*http.Server, net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("listen metrics on %s: %w", address, err)
	}
	server := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		shutdownServer(server)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// The reconcile loop reports health; server startup errors are returned before Serve.
		}
	}()
	return server, listener, nil
}

func shutdownServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func metricsHandler(cfg config.Config, engine datapath.Engine, haStatus interface{ Snapshot() ha.StatusSnapshot }) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		result, err := collectSnapshot(r.Context(), cfg, engine)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_ = renderPrometheusResult(w, cfg, result, currentHASnapshot(haStatus))
	})
	return mux
}

func currentHASnapshot(source interface{ Snapshot() ha.StatusSnapshot }) ha.StatusSnapshot {
	if source == nil {
		return ha.StatusSnapshot{}
	}
	return source.Snapshot()
}

func collectSnapshot(ctx context.Context, cfg config.Config, engine datapath.Engine) (RunResult, error) {
	status, err := engine.Status(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath status %q: %w", engine.Name(), err)
	}
	counters, err := engine.Counters(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath counters %q: %w", engine.Name(), err)
	}
	conntrack, err := engine.ConntrackSummary(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("read datapath conntrack %q: %w", engine.Name(), err)
	}
	interfaceStats, _ := readInterfaceStats(primaryInterface(cfg))
	return RunResult{
		GatewayID: cfg.GatewayID,
		HAGroupID: cfg.HAGroupID,
		Node:      cfg.Local.NodeID,
		Interface: interfaceStats,
		Datapath:  status,
		Counters:  counters,
		Conntrack: conntrack,
	}, nil
}

func primaryInterface(cfg config.Config) string {
	if cfg.Local.PrimaryInterface != "" {
		return cfg.Local.PrimaryInterface
	}
	return cfg.Datapath.LoxiLB.SNATInterface
}

func readInterfaceStats(name string) (metrics.InterfaceStats, error) {
	return readInterfaceStatsFromRoot("/sys/class/net", name)
}

func readInterfaceStatsFromRoot(root string, name string) (metrics.InterfaceStats, error) {
	if name == "" {
		return metrics.InterfaceStats{}, nil
	}
	statsRoot := filepath.Join(root, name, "statistics")
	read := func(file string) (uint64, error) {
		raw, err := os.ReadFile(filepath.Join(statsRoot, file))
		if err != nil {
			return 0, err
		}
		return strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	}
	rxBytes, err := read("rx_bytes")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	rxPackets, err := read("rx_packets")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	rxErrors, err := read("rx_errors")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	rxDropped, err := read("rx_dropped")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	txBytes, err := read("tx_bytes")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	txPackets, err := read("tx_packets")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	txErrors, err := read("tx_errors")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	txDropped, err := read("tx_dropped")
	if err != nil {
		return metrics.InterfaceStats{}, err
	}
	return metrics.InterfaceStats{
		Name:      name,
		RXBytes:   rxBytes,
		RXPackets: rxPackets,
		RXErrors:  rxErrors,
		RXDropped: rxDropped,
		TXBytes:   txBytes,
		TXPackets: txPackets,
		TXErrors:  txErrors,
		TXDropped: txDropped,
	}, nil
}

func ownerCounters(owners []config.OwnerConfig, counters datapath.Counters) []metrics.OwnerCounter {
	byOwner := map[string]metrics.OwnerCounter{}
	for _, rule := range counters.Rules {
		owner := ownerForCIDR(owners, rule.CIDR)
		counter := byOwner[owner]
		counter.Owner = owner
		counter.Direction = "egress"
		counter.Packets += rule.Packets
		counter.Bytes += rule.Bytes
		byOwner[owner] = counter
	}
	result := make([]metrics.OwnerCounter, 0, len(byOwner))
	for _, counter := range byOwner {
		result = append(result, counter)
	}
	return result
}

func processedCounter(counters datapath.Counters) metrics.TrafficCounter {
	result := metrics.TrafficCounter{Direction: "egress"}
	for _, rule := range counters.Rules {
		result.Packets += rule.Packets
		result.Bytes += rule.Bytes
	}
	return result
}

func ownerForCIDR(owners []config.OwnerConfig, cidr string) string {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "unattributed"
	}
	for _, owner := range owners {
		for _, ownerCIDR := range owner.CIDRs {
			ownerPrefix, err := netip.ParsePrefix(ownerCIDR)
			if err != nil {
				continue
			}
			if ownerPrefix.Contains(prefix.Addr()) && ownerPrefix.Bits() <= prefix.Bits() {
				if owner.Name != "" {
					return owner.Name
				}
			}
		}
	}
	return "unattributed"
}

func metricsListenAddress(cfg config.Config) string {
	address := cfg.Observability.Prometheus.ListenAddress
	if address == "" {
		address = "0.0.0.0"
	}
	port := cfg.Observability.Prometheus.ListenPort
	if port == 0 {
		port = 9108
	}
	return fmt.Sprintf("%s:%d", address, port)
}

func reconcileInterval(cfg config.Config) time.Duration {
	seconds := cfg.Datapath.LoxiLB.ReconcileIntervalSeconds
	if seconds <= 0 {
		seconds = 10
	}
	return time.Duration(seconds) * time.Second
}

func parseArgs(args []string) (Options, error) {
	opts := Options{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("--config requires a path")
			}
			opts.ConfigPath = args[i+1]
			i++
		case "--once":
			opts.Once = true
		case "--validate-only":
			opts.ValidateOnly = true
		case "--prometheus":
			opts.Prometheus = true
		case "--version", "version":
			opts.Version = true
		default:
			return Options{}, fmt.Errorf("unknown agent argument %q", args[i])
		}
	}
	if opts.Version {
		return opts, nil
	}
	if opts.ConfigPath == "" {
		return Options{}, fmt.Errorf("agent runtime requires --config <path>")
	}
	return opts, nil
}

func output(out io.Writer) io.Writer {
	if out == nil {
		return io.Discard
	}
	return out
}
