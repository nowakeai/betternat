package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/lease"
)

func TestRunDoctorLiveSupportsGCP(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte(validGCPHAConfigYAML()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	restoreDatapath := newDatapathEngine
	restoreCloud := newLiveGCPCloudProvider
	restoreLease := newLiveFirestoreLeaseManager
	restoreResolveInstance := resolveGCPLocalInstanceID
	restorePromClient := liveDoctorPrometheusClient
	defer func() {
		newDatapathEngine = restoreDatapath
		newLiveGCPCloudProvider = restoreCloud
		newLiveFirestoreLeaseManager = restoreLease
		resolveGCPLocalInstanceID = restoreResolveInstance
		liveDoctorPrometheusClient = restorePromClient
	}()

	newDatapathEngine = func(config.DatapathConfig) (datapath.Engine, error) {
		return fakeReadinessEngine{
			status:   datapath.Status{Engine: "fake", Ready: true, Message: "ready"},
			counters: datapath.Counters{Rules: []datapath.RuleCounter{{CIDR: "10.0.0.0/8", Packets: 1, Bytes: 2}}},
		}, nil
	}
	newLiveGCPCloudProvider = func(context.Context, config.Config) (cloud.Provider, error) {
		return fakeLiveCloud{}, nil
	}
	newLiveFirestoreLeaseManager = func(context.Context, config.Config) (lease.Manager, error) {
		manager := lease.NewMemoryManager("prod-egress-a", time.Minute, time.Now)
		if _, err := manager.Acquire(context.Background(), "i-active"); err != nil {
			t.Fatalf("acquire fake lease: %v", err)
		}
		return manager, nil
	}
	resolveGCPLocalInstanceID = func(context.Context, string) (string, error) {
		return "i-active", nil
	}
	liveDoctorPrometheusClient = fakeHTTPClient{status: 200, body: "ok"}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"doctor", "--live", "--config", configPath}, &out); err != nil {
		t.Fatalf("run gcp live doctor: %v", err)
	}
	body := out.String()
	if strings.Contains(body, "live doctor currently supports cloud=aws only") {
		t.Fatalf("unexpected AWS-only warning: %s", body)
	}
	if !strings.Contains(body, `"name":"ha_config","status":"ok"`) {
		t.Fatalf("missing GCP HA config check: %s", body)
	}
	if !strings.Contains(body, `"name":"lease","status":"ok"`) {
		t.Fatalf("missing Firestore lease check: %s", body)
	}
	if !strings.Contains(body, `"name":"route","status":"ok"`) {
		t.Fatalf("missing GCP route check: %s", body)
	}
	if !strings.Contains(body, `"name":"public_identity","status":"ok"`) {
		t.Fatalf("missing route-only public identity check: %s", body)
	}
	if strings.Contains(body, `"name":"source_dest_check"`) {
		t.Fatalf("GCP live doctor should not run AWS source/destination check: %s", body)
	}
}

func validGCPHAConfigYAML() string {
	return `
version: v0
gateway_id: prod-egress
ha_group_id: prod-egress-a
cloud: gcp
region: us-west2
gcp:
  project_id: shared-resources-alt
  zone: us-west2-a
  network: default
  client_tag: betternat-client
  route_priority: 800
  firestore_database_id: "(default)"
local:
  node_id: auto
  primary_interface: ens4
datapath:
  engine: nftables
  fallback_engine: nftables
  private_cidrs:
    - 10.0.0.0/8
ha:
  enabled: true
  lease:
    backend: firestore
    key: prod-egress-a
    ttl_seconds: 15
  route_failover:
    mode: replace_route
    route_table_ids:
      - bnat-prod-egress-default
    destination_cidr: 0.0.0.0/0
    target_type: instance
  public_identity: {}
observability:
  prometheus:
    listen_address: 127.0.0.1
    listen_port: 9108
  outbound_probe:
    enabled: false
rollback:
  previous_route_targets:
    bnat-prod-egress-default:
      destination_cidr: 0.0.0.0/0
      target: default-internet-gateway
`
}
