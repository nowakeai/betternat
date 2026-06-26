package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	"github.com/nowakeai/betternat/internal/lease"
)

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
	newHandoverStoreReader = func(context.Context, config.Config) (coordination.HandoverReader, error) {
		return fakeHandoverReader{currentGeneration: 2, records: []coordination.HandoverRecord{
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

func TestRunHandoverHistoryUsesFirestoreForGCP(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := `{
	  "version": "v0",
	  "gateway_id": "gcp-egress",
	  "ha_group_id": "gcp-egress-us-west2-a",
	  "cloud": "gcp",
	  "region": "us-west2",
	  "gcp": {
	    "project_id": "test-project",
	    "zone": "us-west2-a",
	    "network": "test-network",
	    "client_tag": "test-client",
	    "route_priority": 800,
	    "firestore_database_id": "(default)"
	  },
	  "local": {"primary_interface": "ens4"},
	  "datapath": {
	    "engine": "loxilb",
	    "private_cidrs": ["10.0.0.0/8"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens4"
	    }
	  },
	  "ha": {
	    "enabled": true,
	    "lease": {"backend": "firestore", "key": "gcp-egress-us-west2-a", "ttl_seconds": 15}
	  },
	  "observability": {},
	  "rollback": {}
	}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := newHandoverStoreReader
	defer func() { newHandoverStoreReader = restore }()
	var captured config.Config
	newHandoverStoreReader = func(_ context.Context, cfg config.Config) (coordination.HandoverReader, error) {
		captured = cfg
		return fakeHandoverReader{records: []coordination.HandoverRecord{{
			RequestID:       "gcp-handover",
			Status:          "completed",
			SourceNodeID:    "gw-a",
			TargetNodeID:    "gw-b",
			LeaseGeneration: 3,
			UpdatedAt:       time.Unix(300, 0),
		}}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "history", "--config", configPath, "--output", "json"}, &out); err != nil {
		t.Fatalf("run handover history: %v", err)
	}
	if captured.Cloud != "gcp" || captured.HA.Lease.Backend != "firestore" || captured.Coordination.Table != "" {
		t.Fatalf("unexpected captured config: %#v", captured)
	}
	if captured.GCP.ProjectID != "test-project" || captured.GCP.FirestoreDatabaseID != "(default)" {
		t.Fatalf("unexpected firestore config: %#v", captured.GCP)
	}
	if !strings.Contains(out.String(), `"request_id":"gcp-handover"`) {
		t.Fatalf("missing gcp handover record: %s", out.String())
	}
}

func TestRunHandoverHistoryHidesStaleIntermediateRecords(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"coordination":{"backend":"dynamodb","table":"coordination"},"observability": {}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	restore := newHandoverStoreReader
	defer func() { newHandoverStoreReader = restore }()
	newHandoverStoreReader = func(context.Context, config.Config) (coordination.HandoverReader, error) {
		return fakeHandoverReader{currentGeneration: 3, records: []coordination.HandoverRecord{
			{
				RequestID:       "stale-preparing",
				Status:          "preparing",
				SourceNodeID:    "i-old-active",
				TargetNodeID:    "i-old-standby",
				LeaseGeneration: 2,
				UpdatedAt:       time.Unix(200, 0),
			},
			{
				RequestID:       "completed",
				Status:          "completed",
				SourceNodeID:    "i-active",
				TargetNodeID:    "i-standby",
				LeaseGeneration: 3,
				UpdatedAt:       time.Unix(300, 0),
			},
		}}, nil
	}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"handover", "history", "--config", configPath, "--output", "json"}, &out); err != nil {
		t.Fatalf("run handover history: %v", err)
	}
	body := out.String()
	if strings.Contains(body, `"request_id":"stale-preparing"`) {
		t.Fatalf("stale intermediate record should be hidden by default: %s", body)
	}
	if !strings.Contains(body, `"request_id":"completed"`) {
		t.Fatalf("current completed record missing: %s", body)
	}

	out.Reset()
	if err := run(context.Background(), []string{"handover", "history", "--config", configPath, "--output", "json", "--include-stale"}, &out); err != nil {
		t.Fatalf("run handover history include stale: %v", err)
	}
	body = out.String()
	if !strings.Contains(body, `"request_id":"stale-preparing"`) || !strings.Contains(body, `"request_id":"completed"`) {
		t.Fatalf("include-stale should show both records: %s", body)
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
	newHandoverStoreReader = func(context.Context, config.Config) (coordination.HandoverReader, error) {
		return fakeHandoverReader{records: []coordination.HandoverRecord{{
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
	newHandoverStoreReader = func(context.Context, config.Config) (coordination.HandoverReader, error) {
		return fakeHandoverReader{records: []coordination.HandoverRecord{{
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

type fakeHandoverReader struct {
	currentGeneration uint64
	records           []coordination.HandoverRecord
}

func (f fakeHandoverReader) GetHandover(_ context.Context, requestID string) (coordination.HandoverRecord, error) {
	for _, record := range f.records {
		if record.RequestID == requestID {
			return record, nil
		}
	}
	return coordination.HandoverRecord{}, os.ErrNotExist
}

func (f fakeHandoverReader) ListHandovers(context.Context) ([]coordination.HandoverRecord, error) {
	return append([]coordination.HandoverRecord(nil), f.records...), nil
}

func (f fakeHandoverReader) Current(context.Context) (lease.Record, error) {
	if f.currentGeneration == 0 {
		return lease.Record{}, os.ErrNotExist
	}
	return lease.Record{HAGroupID: "prod-egress-a", OwnerInstanceID: "i-active", Generation: f.currentGeneration}, nil
}
