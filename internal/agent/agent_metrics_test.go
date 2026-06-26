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
	cfg.Local.NodeID = "i-local"
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

func TestReadInterfaceStatsFromRoot(t *testing.T) {
	root := t.TempDir()
	statsDir := filepath.Join(root, "ens5", "statistics")
	if err := os.MkdirAll(statsDir, 0o755); err != nil {
		t.Fatalf("mkdir stats: %v", err)
	}
	files := map[string]string{
		"rx_bytes":   "100\n",
		"rx_packets": "10\n",
		"rx_errors":  "1\n",
		"rx_dropped": "2\n",
		"tx_bytes":   "200\n",
		"tx_packets": "20\n",
		"tx_errors":  "3\n",
		"tx_dropped": "4\n",
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(statsDir, name), []byte(value), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	stats, err := readInterfaceStatsFromRoot(root, "ens5")
	if err != nil {
		t.Fatalf("read interface stats: %v", err)
	}
	if stats.Name != "ens5" || stats.RXBytes != 100 || stats.RXPackets != 10 || stats.RXErrors != 1 || stats.RXDropped != 2 ||
		stats.TXBytes != 200 || stats.TXPackets != 20 || stats.TXErrors != 3 || stats.TXDropped != 4 {
		t.Fatalf("unexpected interface stats: %#v", stats)
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
