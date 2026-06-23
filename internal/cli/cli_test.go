package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	dynamodbcoord "github.com/nowakeai/betternat/internal/coordination/dynamodb"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/doctor"
	"github.com/nowakeai/betternat/internal/iamcheck"
	"github.com/nowakeai/betternat/internal/lease"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"version"}, &out); err != nil {
		t.Fatalf("run version: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if !strings.Contains(got, "betternat version=dev") {
		t.Fatalf("version output = %q", got)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"nope"}, &out); err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestRunDoctorValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"doctor", "--config", configPath}, &out); err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	if !strings.Contains(out.String(), `"status":"ok"`) {
		t.Fatalf("missing ok report: %s", out.String())
	}
	if !strings.Contains(out.String(), `"message":"valid"`) {
		t.Fatalf("missing valid config check: %s", out.String())
	}
}

func TestRunDoctorInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(`{"version":"v0"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"doctor", "--config", configPath}, &out); err == nil {
		t.Fatal("expected critical doctor error")
	}
	if !strings.Contains(out.String(), `"status":"critical"`) {
		t.Fatalf("missing critical report: %s", out.String())
	}
	if !strings.Contains(out.String(), `"name":"config"`) {
		t.Fatalf("missing config check: %s", out.String())
	}
}

func TestRunDoctorLiveUsesFakeDependencies(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte(validHAConfigYAML()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	restoreDatapath := newDatapathEngine
	restoreCloud := newLiveCloudProvider
	restoreASG := newLiveASGInspector
	restoreIAM := newLiveIAMEvaluator
	restoreLease := newLiveLeaseManager
	restoreResolveInstance := resolveLocalInstanceID
	restoreResolveRole := resolveCurrentRoleARN
	restorePromClient := liveDoctorPrometheusClient
	restoreProbeClient := liveDoctorSourceProbeClient
	defer func() {
		newDatapathEngine = restoreDatapath
		newLiveCloudProvider = restoreCloud
		newLiveASGInspector = restoreASG
		newLiveIAMEvaluator = restoreIAM
		newLiveLeaseManager = restoreLease
		resolveLocalInstanceID = restoreResolveInstance
		resolveCurrentRoleARN = restoreResolveRole
		liveDoctorPrometheusClient = restorePromClient
		liveDoctorSourceProbeClient = restoreProbeClient
	}()

	newDatapathEngine = func(config.DatapathConfig) (datapath.Engine, error) {
		return fakeReadinessEngine{
			status:   datapath.Status{Engine: "fake", Ready: true, Message: "ready"},
			counters: datapath.Counters{Rules: []datapath.RuleCounter{{CIDR: "10.0.0.0/8", Packets: 1, Bytes: 2}}},
		}, nil
	}
	newLiveCloudProvider = func(context.Context, string) (liveCloudProvider, error) {
		return fakeLiveCloud{}, nil
	}
	newLiveASGInspector = func(context.Context, string) (doctor.ASGInspector, error) {
		return fakeASGInspector{}, nil
	}
	newLiveIAMEvaluator = func(context.Context, string, string) (doctor.IAMChecker, error) {
		return doctor.IAMChecker{Evaluator: fakeIAMEvaluator{}}, nil
	}
	newLiveLeaseManager = func(context.Context, string, string, string, time.Duration) (lease.Manager, error) {
		manager := lease.NewMemoryManager("prod-egress-a", time.Minute, time.Now)
		if _, err := manager.Acquire(context.Background(), "i-active"); err != nil {
			t.Fatalf("acquire fake lease: %v", err)
		}
		return manager, nil
	}
	resolveLocalInstanceID = func(context.Context, string) (string, error) {
		return "i-active", nil
	}
	resolveCurrentRoleARN = func(context.Context, string) (string, error) {
		return "arn:aws:iam::123456789012:role/betternat-prod-egress-agent", nil
	}
	liveDoctorPrometheusClient = fakeHTTPClient{status: 200, body: "ok"}
	liveDoctorSourceProbeClient = fakeHTTPClient{status: 200, body: "35.85.131.212\n"}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"doctor", "--live", "--config", configPath}, &out); err != nil {
		t.Fatalf("run live doctor: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"name":"datapath","status":"ok"`) {
		t.Fatalf("missing datapath live check: %s", body)
	}
	if !strings.Contains(body, `"name":"route","status":"ok"`) {
		t.Fatalf("missing route live check: %s", body)
	}
	if !strings.Contains(body, `"name":"iam","status":"ok"`) {
		t.Fatalf("missing iam live check: %s", body)
	}
	if !strings.Contains(body, `"name":"asg","status":"ok"`) {
		t.Fatalf("missing asg live check: %s", body)
	}
	if !strings.Contains(body, `"name":"public_identity","status":"ok"`) {
		t.Fatalf("missing public identity live check: %s", body)
	}
	if !strings.Contains(body, `"name":"prometheus","status":"ok"`) {
		t.Fatalf("missing prometheus live check: %s", body)
	}
	if !strings.Contains(body, `"name":"source_ip_probe","status":"ok"`) {
		t.Fatalf("missing source ip probe live check: %s", body)
	}
}

func TestRunCostEstimate(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"cost", "estimate", "--gb", "10240", "--node-hourly", "0.05", "--nodes", "2"}, &out)
	if err != nil {
		t.Fatalf("run cost estimate: %v", err)
	}
	if !strings.Contains(out.String(), `"processed_gb":10240`) {
		t.Fatalf("missing processed gb: %s", out.String())
	}
	if !strings.Contains(out.String(), `"estimated_savings_usd"`) {
		t.Fatalf("missing savings: %s", out.String())
	}
}

func TestRunStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--direct", "--config", configPath}, &out); err != nil {
		t.Fatalf("run status: %v", err)
	}
	if !strings.Contains(out.String(), "prod-egress") {
		t.Fatalf("missing gateway id: %s", out.String())
	}
	if !strings.Contains(out.String(), "loxilb") {
		t.Fatalf("missing datapath: %s", out.String())
	}
	if strings.ContainsAny(out.String(), "┌┬┐└┴┘│├┼┤─") {
		t.Fatalf("status table should not render box borders: %s", out.String())
	}
}

func TestRunStatusJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--direct", "--config", configPath, "--output", "json", "--sample", "0s"}, &out); err != nil {
		t.Fatalf("run status: %v", err)
	}
	if !strings.Contains(out.String(), `"gateway_id":"prod-egress"`) {
		t.Fatalf("missing gateway id: %s", out.String())
	}
	if !strings.Contains(out.String(), `"metrics_addr":"0.0.0.0:9108"`) {
		t.Fatalf("missing metrics addr: %s", out.String())
	}
}

func TestRunStatusUsesDefaultConfigPath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := defaultConfigPath
	defaultConfigPath = configPath
	defer func() { defaultConfigPath = restore }()

	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--direct", "--output", "json", "--sample", "0s"}, &out); err != nil {
		t.Fatalf("run status: %v", err)
	}
	if !strings.Contains(out.String(), `"gateway_id":"prod-egress"`) {
		t.Fatalf("missing gateway id: %s", out.String())
	}
}

func TestRunStatusUsesRegistryWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"coordination":{"backend":"dynamodb","table":"coordination"},"observability": {}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restoreRegistry := newStatusRegistry
	restoreResolveInstance := resolveLocalInstanceID
	defer func() {
		newStatusRegistry = restoreRegistry
		resolveLocalInstanceID = restoreResolveInstance
	}()
	newStatusRegistry = func(context.Context, config.Config) (statusRegistry, error) {
		return fakeStatusRegistry{}, nil
	}
	resolveLocalInstanceID = func(context.Context, string) (string, error) {
		return "", nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--direct", "--config", configPath, "--output", "json", "--sample", "0s"}, &out); err != nil {
		t.Fatalf("run status: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"instance_count":2`) {
		t.Fatalf("missing registry instances: %s", body)
	}
	if !strings.Contains(body, `"node_id":"i-active","role":"active"`) {
		t.Fatalf("missing active registry row: %s", body)
	}
	if !strings.Contains(body, `"version":"v-registry"`) {
		t.Fatalf("missing registry version: %s", body)
	}
	if !strings.Contains(body, `"public_ip":"35.85.131.212"`) {
		t.Fatalf("missing active public ip: %s", body)
	}
}

func TestRunStatusUsesDaemonByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.StatusPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(agentapi.StatusResponse{
			SchemaVersion:   "v1",
			GatewayID:       "prod-egress",
			HAGroupID:       "prod-egress-a",
			Region:          "us-west-2",
			Datapath:        "loxilb",
			MetricsAddr:     "0.0.0.0:9108",
			RouteTarget:     "i-active",
			LeaseGeneration: 7,
			LeaseExpiresIn:  8.5,
			InstanceCount:   1,
			Instances: []agentapi.StatusInstance{{
				NodeID:     "i-active",
				Role:       "active",
				ControlURL: "http://10.0.1.10:9109",
				Version:    "v-test",
				Metrics:    "ok",
				Fresh:      true,
				AgeSeconds: 1.2,
			}},
			Cache: agentapi.CacheInfo{Mode: "cached", Fresh: true},
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--host", server.URL, "--output", "json"}, &out); err != nil {
		t.Fatalf("run status: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"schema_version":"v1"`) {
		t.Fatalf("missing daemon schema: %s", body)
	}
	if !strings.Contains(body, `"node_id":"i-active"`) {
		t.Fatalf("missing daemon node: %s", body)
	}
	if !strings.Contains(body, `"lease_generation":7`) {
		t.Fatalf("missing lease generation: %s", body)
	}
	if !strings.Contains(body, `"cache_mode":"cached"`) {
		t.Fatalf("missing cache mode: %s", body)
	}
	if !strings.Contains(body, `"control_url":"http://10.0.1.10:9109"`) {
		t.Fatalf("missing control url: %s", body)
	}
}

func TestRunStatusWatchUsesDaemonUntilContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.StatusPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		call := atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(agentapi.StatusResponse{
			SchemaVersion: "v1",
			GatewayID:     "prod-egress",
			HAGroupID:     "prod-egress-a",
			Region:        "us-west-2",
			Datapath:      "loxilb",
			MetricsAddr:   "0.0.0.0:9108",
			InstanceCount: 1,
			Instances: []agentapi.StatusInstance{{
				NodeID:  "i-active",
				Role:    "active",
				Version: "v-test",
				Metrics: "ok",
			}},
			Cache: agentapi.CacheInfo{Mode: "cached", Fresh: true},
		})
		if call >= 3 {
			go cancel()
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := run(ctx, []string{"status", "--watch", "--interval", "1ms", "--host", server.URL, "--output", "json"}, &out); err != nil {
		t.Fatalf("run status watch: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected repeated status requests, got %d", calls)
	}
	if strings.Count(out.String(), `"schema_version":"v1"`) < 2 {
		t.Fatalf("expected repeated json output: %s", out.String())
	}
}

func TestRunHandoverStartUsesDaemon(t *testing.T) {
	var sawPost bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.HandoverPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		sawPost = true
		_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{
			SchemaVersion:   "v1",
			Status:          "completed",
			SourceNodeID:    "i-active",
			TargetNodeID:    "i-standby",
			LeaseGeneration: 2,
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "start", "--host", server.URL, "--to", "i-standby", "--output", "json"}, &out); err != nil {
		t.Fatalf("run handover start: %v", err)
	}
	if !sawPost {
		t.Fatal("daemon was not called")
	}
	if !strings.Contains(out.String(), `"status":"completed"`) {
		t.Fatalf("missing completion response: %s", out.String())
	}
}

func TestRunHandoverStartReturnsDaemonRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(agentapi.HandoverResponse{
			SchemaVersion: "v1",
			Status:        "rejected",
			Error:         "local daemon is not the active route target",
		})
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{"handover", "start", "--host", server.URL, "--to", "i-standby"}, &out)
	if err == nil {
		t.Fatal("expected handover rejection")
	}
	if !strings.Contains(err.Error(), "not the active") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunHandoverHistoryUsesCoordinationRecords(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"coordination":{"backend":"dynamodb","table":"coordination"},"observability": {}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := newHandoverStoreReader
	defer func() { newHandoverStoreReader = restore }()
	newHandoverStoreReader = func(context.Context, config.Config) (handoverStoreReader, error) {
		return fakeHandoverReader{records: []dynamodbcoord.HandoverRecord{
			{
				RequestID:       "old",
				Status:          "failed",
				SourceNodeID:    "i-old",
				TargetNodeID:    "i-standby",
				LeaseGeneration: 1,
				UpdatedAt:       time.Unix(100, 0),
			},
			{
				RequestID:       "new",
				Status:          "completed",
				SourceNodeID:    "i-active",
				TargetNodeID:    "i-standby",
				LeaseGeneration: 2,
				UpdatedAt:       time.Unix(200, 0),
			},
		}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "history", "--config", configPath, "--status", "completed", "--output", "json"}, &out); err != nil {
		t.Fatalf("run handover history: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"request_id":"new"`) || strings.Contains(body, `"request_id":"old"`) {
		t.Fatalf("unexpected history output: %s", body)
	}
	if !strings.Contains(body, `"source_node_id":"i-active"`) {
		t.Fatalf("missing source node: %s", body)
	}
}

func TestRunHandoverHistoryDefaultsToTable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"coordination":{"backend":"dynamodb","table":"coordination"},"observability": {}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := newHandoverStoreReader
	defer func() { newHandoverStoreReader = restore }()
	newHandoverStoreReader = func(context.Context, config.Config) (handoverStoreReader, error) {
		return fakeHandoverReader{records: []dynamodbcoord.HandoverRecord{{
			RequestID:       "req-1",
			Status:          "completed",
			SourceNodeID:    "i-active",
			TargetNodeID:    "i-standby",
			LeaseGeneration: 2,
			UpdatedAt:       time.Unix(200, 0),
		}}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "history", "--config", configPath}, &out); err != nil {
		t.Fatalf("run handover history: %v", err)
	}
	body := out.String()
	if strings.Contains(body, `"records"`) {
		t.Fatalf("expected table output, got json: %s", body)
	}
	for _, want := range []string{"REQUEST", "STATUS", "req-1", "completed", "i-active", "i-standby"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in table output: %s", want, body)
		}
	}
}

func TestRunHandoverInspectUsesCoordinationRecord(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"coordination":{"backend":"dynamodb","table":"coordination"},"observability": {}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := newHandoverStoreReader
	defer func() { newHandoverStoreReader = restore }()
	newHandoverStoreReader = func(context.Context, config.Config) (handoverStoreReader, error) {
		return fakeHandoverReader{records: []dynamodbcoord.HandoverRecord{{
			RequestID:       "req-1",
			Status:          "completed",
			SourceNodeID:    "i-active",
			TargetNodeID:    "i-standby",
			LeaseGeneration: 2,
			Message:         "handover completed",
		}}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "inspect", "handover#req-1", "--config", configPath, "--output", "json"}, &out); err != nil {
		t.Fatalf("run handover inspect: %v", err)
	}
	if !strings.Contains(out.String(), `"request_id":"req-1"`) || !strings.Contains(out.String(), `"message":"handover completed"`) {
		t.Fatalf("unexpected inspect output: %s", out.String())
	}
}

func TestRunDatapathStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"datapath", "status", "--config", configPath}, &out); err != nil {
		t.Fatalf("run datapath status: %v", err)
	}
	if !strings.Contains(out.String(), "loxilb") {
		t.Fatalf("missing datapath engine: %s", out.String())
	}
	if strings.Contains(out.String(), "fallback") {
		t.Fatalf("fallback should be hidden in table output: %s", out.String())
	}
}

func TestRunDatapathStatusJSONIncludesFallback(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"datapath", "status", "--config", configPath, "--output", "json"}, &out); err != nil {
		t.Fatalf("run datapath status: %v", err)
	}
	if !strings.Contains(out.String(), `"fallback_engine":"nftables"`) {
		t.Fatalf("missing fallback engine in json output: %s", out.String())
	}
}

func TestDatapathReadinessReportsExpectedRules(t *testing.T) {
	engine := fakeReadinessEngine{
		status:   datapath.Status{Engine: "fake", Ready: true, Message: "ready"},
		counters: datapath.Counters{Rules: []datapath.RuleCounter{{CIDR: "10.0.0.0/8"}}},
	}
	result, err := datapathReadiness(context.Background(), config.DatapathConfig{
		PrivateCIDRs: []string{"10.0.0.0/8"},
	}, engine)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected ready: %#v", result)
	}
	if len(result.MissingSNATCIDRs) != 0 {
		t.Fatalf("unexpected missing rules: %#v", result)
	}
}

func TestDatapathReadinessReportsMissingRules(t *testing.T) {
	engine := fakeReadinessEngine{
		status:   datapath.Status{Engine: "fake", Ready: true, Message: "ready"},
		counters: datapath.Counters{Rules: []datapath.RuleCounter{{CIDR: "10.1.0.0/16"}}},
	}
	result, err := datapathReadiness(context.Background(), config.DatapathConfig{
		PrivateCIDRs: []string{"10.0.0.0/8"},
	}, engine)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if result.Ready {
		t.Fatalf("expected not ready: %#v", result)
	}
	if len(result.MissingSNATCIDRs) != 1 || result.MissingSNATCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("unexpected missing rules: %#v", result)
	}
}

func TestRunFailoverStatus(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte(validHAConfigYAML()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"failover", "status", "--config", configPath, "--output", "json"}, &out); err != nil {
		t.Fatalf("run failover status: %v", err)
	}
	if !strings.Contains(out.String(), `"enabled":true`) {
		t.Fatalf("missing enabled flag: %s", out.String())
	}
	if !strings.Contains(out.String(), `"stable_egress_ip_likely":true`) {
		t.Fatalf("missing stable egress flag: %s", out.String())
	}
	if !strings.Contains(out.String(), `"route_table_ids":["rtb-private-a"]`) {
		t.Fatalf("missing route table ids: %s", out.String())
	}
	if !strings.Contains(out.String(), `"outbound_probe_enabled":true`) {
		t.Fatalf("missing outbound probe status: %s", out.String())
	}
}

type fakeReadinessEngine struct {
	status   datapath.Status
	counters datapath.Counters
}

type fakeStatusRegistry struct{}

type fakeHandoverReader struct {
	records []dynamodbcoord.HandoverRecord
}

func (f fakeHandoverReader) GetHandover(_ context.Context, requestID string) (dynamodbcoord.HandoverRecord, error) {
	for _, record := range f.records {
		if record.RequestID == requestID {
			return record, nil
		}
	}
	return dynamodbcoord.HandoverRecord{}, os.ErrNotExist
}

func (f fakeHandoverReader) ListHandovers(context.Context) ([]dynamodbcoord.HandoverRecord, error) {
	return append([]dynamodbcoord.HandoverRecord(nil), f.records...), nil
}

func (fakeStatusRegistry) Current(context.Context) (lease.Record, error) {
	return lease.Record{HAGroupID: "prod-egress-a", OwnerInstanceID: "i-active", Generation: 3}, nil
}

func (fakeStatusRegistry) ListAgents(context.Context) ([]dynamodbcoord.AgentRecord, error) {
	return []dynamodbcoord.AgentRecord{
		{NodeID: "i-active", PrivateIP: "10.0.1.10", PublicIP: "35.85.131.212", Version: "v-registry", DatapathReady: true, HAState: "active"},
		{NodeID: "i-standby", PrivateIP: "10.0.1.11", Version: "v-registry", DatapathReady: true, HAState: "standby"},
	}, nil
}

func (f fakeReadinessEngine) Name() string { return "fake" }

func (f fakeReadinessEngine) EnsureReady(context.Context, config.DatapathConfig) error { return nil }

func (f fakeReadinessEngine) Reconcile(context.Context, config.DatapathConfig) error { return nil }

func (f fakeReadinessEngine) Status(context.Context) (datapath.Status, error) {
	return f.status, nil
}

func (f fakeReadinessEngine) Counters(context.Context) (datapath.Counters, error) {
	return f.counters, nil
}

func (f fakeReadinessEngine) ConntrackSummary(context.Context) (datapath.ConntrackSummary, error) {
	return datapath.ConntrackSummary{}, nil
}

func (f fakeReadinessEngine) Cleanup(context.Context) error { return nil }

type fakeLiveCloud struct{}

func (fakeLiveCloud) ReplaceRoute(context.Context, cloud.RouteTarget) error { return nil }

func (fakeLiveCloud) AssociateEIP(context.Context, string, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, nil
}

func (fakeLiveCloud) DescribeRoute(_ context.Context, routeTableID string, destinationCIDR string) (cloud.RouteTarget, error) {
	return cloud.RouteTarget{RouteTableID: routeTableID, DestinationCIDR: destinationCIDR, Target: "i-active"}, nil
}

func (fakeLiveCloud) DescribePublicIdentity(context.Context, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{AllocationID: "eipalloc-123", PublicIP: "35.85.131.212", InstanceID: "i-active"}, nil
}

func (fakeLiveCloud) DescribeInstance(context.Context, string) (cloud.InstanceInfo, error) {
	return cloud.InstanceInfo{InstanceID: "i-active", SourceDestCheckEnabled: false, PrivateIP: "10.0.1.10", PublicIP: "35.85.131.212"}, nil
}

func (fakeLiveCloud) SourceDestCheckEnabled(context.Context, string) (bool, error) {
	return false, nil
}

type fakeASGInspector struct{}

func (fakeASGInspector) DescribeASG(context.Context, string) (cloud.ASGInfo, error) {
	return cloud.ASGInfo{
		Name:            "betternat-prod-egress-us-west-2a",
		DesiredCapacity: 2,
		Instances: []cloud.ASGInstance{
			{InstanceID: "i-active", LifecycleState: "InService", HealthStatus: "Healthy"},
			{InstanceID: "i-standby", LifecycleState: "InService", HealthStatus: "Healthy"},
		},
	}, nil
}

type fakeIAMEvaluator struct{}

func (fakeIAMEvaluator) Evaluate(context.Context, []string) (iamcheck.Result, error) {
	return iamcheck.Result{Allowed: append([]string(nil), iamcheck.RequiredRuntimeActions...)}, nil
}

type fakeHTTPClient struct {
	status int
	body   string
}

func (c fakeHTTPClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
	}, nil
}

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

func validHAConfigYAML() string {
	return `
version: v0
gateway_id: prod-egress
ha_group_id: prod-egress-a
cloud: aws
region: us-west-2
local:
  availability_zone: us-west-2a
  primary_interface: ens5
datapath:
  engine: loxilb
  fallback_engine: nftables
  private_cidrs: ["10.0.0.0/8"]
  loxilb:
    snat_to: auto
    snat_interface: ens5
ha:
  enabled: true
  lease:
    backend: dynamodb
    table: betternat-prod-egress-leases
    key: prod-egress-a
  route_failover:
    mode: replace_route
    route_table_ids: ["rtb-private-a"]
    destination_cidr: 0.0.0.0/0
    target_type: instance
  public_identity:
    mode: shared_eip
    allocation_id: eipalloc-123
observability:
  prometheus:
    listen_address: 0.0.0.0
    listen_port: 9108
  outbound_probe:
    enabled: true
    url: https://checkip.amazonaws.com
    expected_ip: ""
rollback: {}
`
}
