package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSupportBundleRedactsConfigAndCollectsCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	raw := strings.Replace(validConfigJSON(), `"observability": {}`, `"control":{"peer_api":{"enabled":true,"listen_address":"0.0.0.0","listen_port":9109,"auth_token":"secret-token"}},"observability":{"prometheus":{"listen_address":"127.0.0.1","listen_port":0}}`, 1)
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	outputPath := filepath.Join(dir, "support.tar.gz")
	restore := runSupportCommand
	defer func() { runSupportCommand = restore }()
	runSupportCommand = func(_ context.Context, name string, args ...string) (supportCommandResult, error) {
		return supportCommandResult{Stdout: []byte(name + " " + strings.Join(args, " ") + "\n")}, nil
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"support", "bundle", "--config", configPath, "--output", outputPath, "--host", "http://127.0.0.1:1", "--timeout", "1ms"}, &out)
	if err != nil {
		t.Fatalf("run support bundle: %v", err)
	}
	files := readSupportBundle(t, outputPath)
	configBody := string(files["config.redacted.json"])
	if strings.Contains(configBody, "secret-token") {
		t.Fatalf("support bundle leaked token: %s", configBody)
	}
	if !strings.Contains(configBody, "[REDACTED]") {
		t.Fatalf("support bundle did not redact token: %s", configBody)
	}
	if !strings.Contains(string(files["systemd/betternat-agent.status.txt"]), "systemctl status betternat-agent") {
		t.Fatalf("missing systemctl output: %s", string(files["systemd/betternat-agent.status.txt"]))
	}
	if !strings.Contains(out.String(), outputPath) {
		t.Fatalf("missing output path: %s", out.String())
	}
}

func TestRunSupportBundleCollectsGCPEvidence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(configPath, []byte(validGCPConfigJSON()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	outputPath := filepath.Join(dir, "support.tar.gz")
	restore := runSupportCommand
	defer func() { runSupportCommand = restore }()
	runSupportCommand = func(_ context.Context, name string, args ...string) (supportCommandResult, error) {
		return supportCommandResult{Stdout: []byte(name + " " + strings.Join(args, " ") + "\n")}, nil
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{"support", "bundle", "--config", configPath, "--output", outputPath, "--host", "http://127.0.0.1:1", "--timeout", "1ms"}, &out)
	if err != nil {
		t.Fatalf("run support bundle: %v", err)
	}
	files := readSupportBundle(t, outputPath)
	if !strings.Contains(string(files["cloud/gcp/metadata-instance-name.txt"]), "metadata.google.internal/computeMetadata/v1/instance/name") {
		t.Fatalf("missing GCP instance metadata evidence: %s", string(files["cloud/gcp/metadata-instance-name.txt"]))
	}
	if !strings.Contains(string(files["cloud/gcp/firestore-databases.json"]), "gcloud --project shared-resources-alt firestore databases list --format=json") {
		t.Fatalf("missing Firestore database evidence: %s", string(files["cloud/gcp/firestore-databases.json"]))
	}
	if !strings.Contains(string(files["cloud/gcp/route-01.json"]), "gcloud --project shared-resources-alt compute routes describe bnat-gcp-default-via-gateway --format=json") {
		t.Fatalf("missing GCP route evidence: %s", string(files["cloud/gcp/route-01.json"]))
	}
}

func readSupportBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open support bundle: %v", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	files := map[string][]byte{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		files[header.Name] = body
	}
	return files
}

func validGCPConfigJSON() string {
	return `{
	  "version": "v0",
	  "gateway_id": "bnat-gcp",
	  "ha_group_id": "bnat-gcp-us-west2-a",
	  "cloud": "gcp",
	  "region": "us-west2",
	  "gcp": {
	    "project_id": "shared-resources-alt",
	    "zone": "us-west2-a",
	    "network": "bnat-vpc",
	    "client_tag": "bnat-client",
	    "route_priority": 800,
	    "firestore_database_id": "(default)"
	  },
	  "local": {"primary_interface": "ens4"},
	  "datapath": {
	    "engine": "loxilb",
	    "fallback_engine": "nftables",
	    "private_cidrs": ["10.91.0.0/24"],
	    "loxilb": {
	      "api_address": "127.0.0.1",
	      "api_port": 11111,
	      "snat_to": "auto",
	      "snat_interface": "ens4"
	    }
	  },
	  "ha": {
	    "enabled": true,
	    "route_failover": {
	      "mode": "replace_route",
	      "route_table_ids": ["bnat-gcp-default-via-gateway"],
	      "destination_cidr": "0.0.0.0/0",
	      "target_type": "instance"
	    }
	  },
	  "observability": {},
	  "rollback": {}
	}`
}
