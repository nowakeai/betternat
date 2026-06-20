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

	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
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
	engine := &fakeEngine{reconciled: true}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metricsHandler(cfg, engine).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "betternat_datapath_ready") {
		t.Fatalf("missing metrics output: %s", rec.Body.String())
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
