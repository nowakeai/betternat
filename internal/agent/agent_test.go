package agent

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/config"
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

type fakeStatusReporter struct {
	snapshot ha.StatusSnapshot
}

func (r fakeStatusReporter) Snapshot() ha.StatusSnapshot {
	return r.snapshot
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
	}

	if err := runtime.Run(ctx, Options{ConfigPath: configPath}); err != nil {
		t.Fatalf("runtime continuous HA: %v", err)
	}
	if factory.cfg.HA.PublicIdentity.AllocationID != "eipalloc-resolved" {
		t.Fatalf("shared EIP was not resolved: %#v", factory.cfg.HA.PublicIdentity)
	}
}

func TestRuntimeRunOnceCanRenderPrometheus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	engine := &fakeEngine{}
	var out bytes.Buffer
	runtime := Runtime{Factory: fakeFactory{engine: engine}, Stdout: &out}

	err := runtime.Run(context.Background(), Options{
		ConfigPath: configPath,
		Once:       true,
		Prometheus: true,
	})
	if err != nil {
		t.Fatalf("runtime run prometheus: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`betternat_datapath_ready{engine="fake",gateway="prod-egress",ha_group="prod-egress-a"} 1`)) {
		t.Fatalf("missing ready metric in output: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`betternat_agent_build_info{commit="unknown",gateway="prod-egress",ha_group="prod-egress-a",node="i-local",version="dev"} 1`)) &&
		!bytes.Contains(out.Bytes(), []byte(`betternat_agent_build_info{commit="unknown",gateway="prod-egress",ha_group="prod-egress-a",node="",version="dev"} 1`)) {
		t.Fatalf("missing build info metric in output: %s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`betternat_loxilb_rule_packets_total{cidr="10.0.0.0/8",engine="fake",gateway="prod-egress",ha_group="prod-egress-a"} 12`)) {
		t.Fatalf("missing counter metric in output: %s", out.String())
	}
}

func TestRuntimeContinuousReconcilesUntilContextCancelled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine := &fakeEngine{onReconcile: cancel}
	runtime := Runtime{
		Factory:              fakeFactory{engine: engine},
		Stdout:               ioDiscard{},
		MetricsListenAddress: "127.0.0.1:0",
		DisableMetricsServer: true,
	}

	if err := runtime.Run(ctx, Options{ConfigPath: configPath}); err != nil {
		t.Fatalf("runtime continuous: %v", err)
	}
	if engine.reconcileCount == 0 {
		t.Fatal("continuous runtime did not reconcile")
	}
}

func TestMetricsHandler(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Local.InstanceID = "i-local"
	engine := &fakeEngine{reconciled: true}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	status := ha.NewMemoryStatus()
	status.Report(ha.StepResult{
		State: ha.StateActive,
		Lease: lease.Record{
			OwnerInstanceID: "i-local",
			Generation:      7,
			ExpiresAt:       time.Now().Add(10 * time.Second),
		},
	})
	metricsHandler(cfg, engine, status).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "betternat_datapath_ready") {
		t.Fatalf("missing metrics output: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `betternat_agent_build_info{commit="unknown",gateway="prod-egress",ha_group="prod-egress-a",node="i-local",version="dev"} 1`) {
		t.Fatalf("missing build info metric: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `betternat_ha_state{gateway="prod-egress",ha_group="prod-egress-a",node="i-local",state="ACTIVE"} 1`) {
		t.Fatalf("missing HA state metric: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `betternat_lease_owner_match{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 1`) {
		t.Fatalf("missing lease owner match metric: %s", rec.Body.String())
	}
}

func TestMetricsHandlerMarksStaleHAStatusInactive(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	engine := &fakeEngine{reconciled: true}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	status := fakeStatusReporter{snapshot: ha.StatusSnapshot{
		State:     ha.StateActive,
		UpdatedAt: time.Now().Add(-45 * time.Second),
		Lease: lease.Record{
			OwnerInstanceID: "i-local",
			Generation:      8,
			ExpiresAt:       time.Now().Add(-30 * time.Second),
		},
		HasRouteTargetCheck:     true,
		RouteTargetMatches:      true,
		HasPublicIdentityCheck:  true,
		PublicIdentityMatches:   true,
		SecondsUntilLeaseExpiry: -30,
	}}

	metricsHandler(cfg, engine, status).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertMetricContains(t, body, `betternat_ha_state{gateway="prod-egress",ha_group="prod-egress-a",node="i-local",state="STALE"} 1`)
	assertMetricContains(t, body, `betternat_ha_status_stale{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 1`)
	assertMetricContains(t, body, `betternat_active{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 0`)
	assertMetricContains(t, body, `betternat_lease_owner_match{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 0`)
	assertMetricContains(t, body, `betternat_route_target_match{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 0`)
	assertMetricContains(t, body, `betternat_public_identity_match{gateway="prod-egress",ha_group="prod-egress-a",node="i-local"} 0`)
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

func TestOwnerCounters(t *testing.T) {
	counters := datapath.Counters{Rules: []datapath.RuleCounter{
		{CIDR: "10.1.0.0/16", Packets: 10, Bytes: 1000},
		{CIDR: "10.2.0.0/16", Packets: 20, Bytes: 2000},
		{CIDR: "192.168.0.0/16", Packets: 30, Bytes: 3000},
	}}
	owners := []config.OwnerConfig{
		{Name: "crawler", CIDRs: []string{"10.0.0.0/8"}},
	}
	result := ownerCounters(owners, counters)
	if len(result) != 2 {
		t.Fatalf("expected crawler and unattributed counters: %#v", result)
	}
	byOwner := map[string]metricsCounter{}
	for _, counter := range result {
		byOwner[counter.Owner] = metricsCounter{packets: counter.Packets, bytes: counter.Bytes}
	}
	if byOwner["crawler"].packets != 30 || byOwner["crawler"].bytes != 3000 {
		t.Fatalf("unexpected crawler counter: %#v", byOwner["crawler"])
	}
	if byOwner["unattributed"].packets != 30 || byOwner["unattributed"].bytes != 3000 {
		t.Fatalf("unexpected unattributed counter: %#v", byOwner["unattributed"])
	}
}

func assertMetricContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in:\n%s", want, text)
	}
}

type metricsCounter struct {
	packets uint64
	bytes   uint64
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func validConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "local": {"primary_interface": "ens5"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens5"
	    }
	  },
	  "ha": {},
	  "observability": {},
	  "rollback": {}
	}`
}

func validHAConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "prod-egress",
	  "ha_group_id": "prod-egress-a",
	  "cloud": "aws",
	  "region": "us-west-2",
	  "local": {"instance_id":"i-local","primary_interface": "ens5"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens5"
	    }
	  },
	  "ha": {
	    "enabled": true,
	    "lease": {
	      "backend": "dynamodb",
	      "table": "betternat-test-leases",
	      "key": "prod-egress-a",
	      "ttl_seconds": 10,
	      "renew_interval_seconds": 3
	    },
	    "route_failover": {
	      "mode": "replace_route",
	      "route_table_ids": ["rtb-a"],
	      "destination_cidr": "0.0.0.0/0",
	      "target_type": "instance"
	    },
	    "public_identity": {
	      "mode": "shared_eip",
	      "allocation_id": "eipalloc-123"
	    }
	  },
	  "observability": {},
	  "rollback": {}
	}`
}
