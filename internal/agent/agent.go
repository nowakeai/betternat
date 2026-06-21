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
	"time"

	"github.com/betternat/betternat/internal/cloud"
	awscloud "github.com/betternat/betternat/internal/cloud/aws"
	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
	"github.com/betternat/betternat/internal/datapath/loxilb"
	"github.com/betternat/betternat/internal/datapath/nftables"
	"github.com/betternat/betternat/internal/ha"
	dynamodblease "github.com/betternat/betternat/internal/lease/dynamodb"
	"github.com/betternat/betternat/internal/metrics"
)

type Options struct {
	ConfigPath   string
	Once         bool
	ValidateOnly bool
	Prometheus   bool
}

type Runtime struct {
	Factory              EngineFactory
	HASupervisorFactory  HASupervisorFactory
	InstancePreparer     cloud.InstancePreparer
	ResolveInstanceID    func(context.Context, string) (string, error)
	ResolveSharedEIP     func(context.Context, string, string, string) (string, error)
	Stdout               io.Writer
	MetricsListenAddress string
	DisableMetricsServer bool
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

type DefaultHASupervisorFactory struct{}

func (DefaultHASupervisorFactory) NewSupervisor(ctx context.Context, cfg config.Config, engine datapath.Engine, reporter ha.StatusReporter) (HASupervisor, error) {
	if err := validateHAConfig(cfg); err != nil {
		return nil, err
	}
	cloudProvider, err := awscloud.New(ctx, cfg.Region)
	if err != nil {
		return nil, err
	}
	leaseManager, err := dynamodblease.New(ctx, cfg.Region, cfg.HA.Lease.Table, leaseKey(cfg), leaseTTL(cfg))
	if err != nil {
		return nil, err
	}
	return ha.Supervisor{
		Controller: ha.Controller{
			Cloud:    cloudProvider,
			Lease:    leaseManager,
			Datapath: engine,
		},
		Reporter: reporter,
	}, nil
}

type RunResult struct {
	GatewayID string                    `json:"gateway_id"`
	HAGroupID string                    `json:"ha_group_id"`
	Node      string                    `json:"node"`
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
	return runContinuous(ctx, cfg, engine, metricsAddress, !r.DisableMetricsServer, haFactory)
}

func (r Runtime) prepareLocalInstance(ctx context.Context, cfg config.Config) (config.Config, error) {
	if cfg.Cloud != "aws" || cfg.Local.InstanceID != "auto" {
		return cfg, nil
	}
	resolve := r.ResolveInstanceID
	if resolve == nil {
		resolve = awscloud.ResolveLocalInstanceID
	}
	instanceID, err := resolve(ctx, cfg.Region)
	if err != nil {
		return config.Config{}, err
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
	cfg.Local.InstanceID = instanceID
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
		Node:      cfg.Local.InstanceID,
		Datapath:  status,
		Counters:  counters,
		Conntrack: conntrack,
	}, nil
}

func runContinuous(ctx context.Context, cfg config.Config, engine datapath.Engine, metricsAddress string, enableMetrics bool, haFactory HASupervisorFactory) error {
	haStatus := ha.NewMemoryStatus()
	if enableMetrics {
		server, listener, err := startMetricsServer(ctx, metricsAddress, metricsHandler(cfg, engine, haStatus))
		if err != nil {
			return err
		}
		defer shutdownServer(server)
		defer listener.Close()
	}

	if cfg.HA.Enabled {
		if haFactory == nil {
			return fmt.Errorf("HA supervisor factory is required")
		}
		supervisor, err := haFactory.NewSupervisor(ctx, cfg, engine, haStatus)
		if err != nil {
			return err
		}
		return supervisor.Run(ctx, cfg, cfg.Local.InstanceID, 0)
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

func validateHAConfig(cfg config.Config) error {
	if !cfg.HA.Enabled {
		return nil
	}
	if cfg.Cloud != "aws" {
		return fmt.Errorf("ha.enabled requires cloud=aws")
	}
	if cfg.Region == "" {
		return fmt.Errorf("ha.enabled requires region")
	}
	if cfg.Local.InstanceID == "" || cfg.Local.InstanceID == "auto" {
		return fmt.Errorf("ha.enabled requires resolved local.instance_id")
	}
	if cfg.HA.Lease.Backend != "dynamodb" {
		return fmt.Errorf("unsupported ha.lease.backend %q", cfg.HA.Lease.Backend)
	}
	if cfg.HA.Lease.Table == "" {
		return fmt.Errorf("ha.lease.table is required")
	}
	if leaseKey(cfg) == "" {
		return fmt.Errorf("ha.lease.key or ha_group_id is required")
	}
	if cfg.HA.RouteFailover.Mode == "" {
		return fmt.Errorf("ha.route_failover.mode is required")
	}
	if cfg.HA.RouteFailover.Mode != "replace_route" {
		return fmt.Errorf("unsupported ha.route_failover.mode %q", cfg.HA.RouteFailover.Mode)
	}
	if cfg.HA.RouteFailover.TargetType != "instance" {
		return fmt.Errorf("unsupported ha.route_failover.target_type %q", cfg.HA.RouteFailover.TargetType)
	}
	if cfg.HA.RouteFailover.DestinationCIDR == "" {
		return fmt.Errorf("ha.route_failover.destination_cidr is required")
	}
	if len(cfg.HA.RouteFailover.RouteTableIDs) == 0 {
		return fmt.Errorf("ha.route_failover.route_table_ids is required")
	}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" && cfg.HA.PublicIdentity.AllocationID == "" {
		return fmt.Errorf("ha.public_identity.allocation_id is required for shared_eip")
	}
	if cfg.HA.PublicIdentity.Mode != "" && cfg.HA.PublicIdentity.Mode != "shared_eip" {
		return fmt.Errorf("unsupported ha.public_identity.mode %q", cfg.HA.PublicIdentity.Mode)
	}
	return nil
}

func leaseKey(cfg config.Config) string {
	if cfg.HA.Lease.Key != "" {
		return cfg.HA.Lease.Key
	}
	return cfg.HAGroupID
}

func leaseTTL(cfg config.Config) time.Duration {
	if cfg.HA.Lease.TTLSeconds > 0 {
		return time.Duration(cfg.HA.Lease.TTLSeconds) * time.Second
	}
	return 15 * time.Second
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
		Datapath:                result.Datapath,
		Counters:                result.Counters,
		Conntrack:               result.Conntrack,
		Owners:                  ownerCounters(cfg.Observability.Attribution.Owners, result.Counters),
		Processed:               processedCounter(result.Counters),
	}
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
	return RunResult{
		GatewayID: cfg.GatewayID,
		HAGroupID: cfg.HAGroupID,
		Node:      cfg.Local.InstanceID,
		Datapath:  status,
		Counters:  counters,
		Conntrack: conntrack,
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
		default:
			return Options{}, fmt.Errorf("unknown agent argument %q", args[i])
		}
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
