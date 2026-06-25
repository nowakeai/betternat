package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
)

func TestRunStatusDirectSupportsGCPRouteAndFirestoreRegistry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(configPath, []byte(validGCPHAConfigYAML()), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	restoreRegistry := newStatusRegistry
	restoreCloud := newLiveGCPCloudProvider
	restoreResolveInstance := resolveGCPLocalInstanceID
	restoreHTTPClient := statusHTTPClient
	defer func() {
		newStatusRegistry = restoreRegistry
		newLiveGCPCloudProvider = restoreCloud
		resolveGCPLocalInstanceID = restoreResolveInstance
		statusHTTPClient = restoreHTTPClient
	}()

	newStatusRegistry = func(context.Context, config.Config) (coordination.AgentReader, error) {
		return fakeStatusRegistry{}, nil
	}
	newLiveGCPCloudProvider = func(context.Context, config.Config) (cloud.Provider, error) {
		return fakeLiveCloud{}, nil
	}
	resolveGCPLocalInstanceID = func(context.Context, string) (string, error) {
		return "i-active", nil
	}
	statusHTTPClient = fakeHTTPClient{status: 200, body: statusMetricsBody("i-active")}

	var out bytes.Buffer
	if err := run(context.Background(), []string{"status", "--direct", "--config", configPath, "--output", "json", "--sample", "0s"}, &out); err != nil {
		t.Fatalf("run gcp status: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, `"cloud":"gcp"`) {
		t.Fatalf("missing gcp cloud: %s", body)
	}
	if !strings.Contains(body, `"route_target":"i-active"`) {
		t.Fatalf("missing route target: %s", body)
	}
	if !strings.Contains(body, `"route_target_match":true`) {
		t.Fatalf("missing route target match: %s", body)
	}
	if !strings.Contains(body, `"instance_count":2`) {
		t.Fatalf("missing registry instances: %s", body)
	}
}

func statusMetricsBody(node string) string {
	return `betternat_agent_build_info{node="` + node + `",version="v-test",commit="test"} 1
betternat_active{node="` + node + `"} 1
betternat_interface_rx_bytes_total 100
betternat_interface_tx_bytes_total 200
`
}
