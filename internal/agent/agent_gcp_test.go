package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/config"
)

func TestRuntimePreparesGCPAutoInstance(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validGCPHAConfigJSON(), `"local": {"node_id":"gce-a","primary_interface": "ens4"}`, `"local": {"node_id":"auto","primary_interface": "ens4"}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	engine := &fakeEngine{}
	preparer := &fakeInstancePreparer{}
	var out bytes.Buffer
	runtime := Runtime{
		Factory:          fakeFactory{engine: engine},
		InstancePreparer: preparer,
		ResolveInstanceID: func(_ context.Context, region string) (string, error) {
			if region != "us-west2" {
				t.Fatalf("unexpected region: %s", region)
			}
			return "bnat-gcp-gw-a", nil
		},
		Stdout: &out,
	}

	if err := runtime.Run(context.Background(), Options{ConfigPath: configPath, Once: true}); err != nil {
		t.Fatalf("runtime run: %v", err)
	}
	if preparer.instanceID != "" {
		t.Fatalf("GCP should not use AWS source/dest check preparer: %#v", preparer)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"node":"bnat-gcp-gw-a"`)) {
		t.Fatalf("runtime output should use resolved GCP instance name: %s", out.String())
	}
}

func TestValidateHAConfigAcceptsGCPFirestoreRouteOnly(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validGCPHAConfigJSON()))
	if err != nil {
		t.Fatalf("load gcp config: %v", err)
	}
	cfg.Local.NodeID = "gce-a"

	if err := validateHAConfig(cfg); err != nil {
		t.Fatalf("validate gcp ha config: %v", err)
	}
}

func TestValidateHAConfigRejectsGCPDynamoDBLease(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validGCPHAConfigJSON()))
	if err != nil {
		t.Fatalf("load gcp config: %v", err)
	}
	cfg.Local.NodeID = "gce-a"
	cfg.HA.Lease.Backend = "dynamodb"

	err = validateHAConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "cloud=gcp") {
		t.Fatalf("expected gcp lease backend validation error, got %v", err)
	}
}

func TestValidateHAConfigAcceptsGCPSharedPublicIdentity(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validGCPHAConfigJSON()))
	if err != nil {
		t.Fatalf("load gcp config: %v", err)
	}
	cfg.Local.NodeID = "gce-a"
	cfg.HA.PublicIdentity.Mode = "shared_eip"
	cfg.HA.PublicIdentity.AllocationID = "bnat-static-egress"

	if err := validateHAConfig(cfg); err != nil {
		t.Fatalf("validate gcp public identity config: %v", err)
	}
}

func TestValidateHAConfigRequiresGCPSharedPublicIdentityAddress(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validGCPHAConfigJSON()))
	if err != nil {
		t.Fatalf("load gcp config: %v", err)
	}
	cfg.Local.NodeID = "gce-a"
	cfg.HA.PublicIdentity.Mode = "shared_eip"

	err = validateHAConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "allocation_id") {
		t.Fatalf("expected missing public identity address validation error, got %v", err)
	}
}
