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

	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
	"github.com/betternat/betternat/internal/datapath/loxilb"
	"github.com/betternat/betternat/internal/datapath/nftables"
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
			if err := renderPrometheusResult(output(r.Stdout), cfg, result); err != nil {
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
	return runContinuous(ctx, cfg, engine, metricsAddress, !r.DisableMetricsServer)
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

func runContinuous(ctx context.Context, cfg config.Config, engine datapath.Engine, metricsAddress string, enableMetrics bool) error {
	if enableMetrics {
		server, listener, err := startMetricsServer(ctx, metricsAddress, metricsHandler(cfg, engine))
		if err != nil {
			return err
		}
		defer shutdownServer(server)
		defer listener.Close()
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

func renderPrometheusResult(w io.Writer, cfg config.Config, result RunResult) error {
	snapshot := metrics.Snapshot{
		GatewayID: result.GatewayID,
		HAGroupID: result.HAGroupID,
		Node:      result.Node,
		AgentUp:   true,
		HAState:   "unknown",
		Datapath:  result.Datapath,
		Counters:  result.Counters,
		Conntrack: result.Conntrack,
		Owners:    ownerCounters(cfg.Observability.Attribution.Owners, result.Counters),
		Processed: processedCounter(result.Counters),
	}
	if err := metrics.RenderPrometheus(w, snapshot); err != nil {
		return fmt.Errorf("encode prometheus metrics: %w", err)
	}
	return nil
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

func metricsHandler(cfg config.Config, engine datapath.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		result, err := collectSnapshot(r.Context(), cfg, engine)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_ = renderPrometheusResult(w, cfg, result)
	})
	return mux
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
