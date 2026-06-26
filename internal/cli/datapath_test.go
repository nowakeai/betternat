package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
)

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
