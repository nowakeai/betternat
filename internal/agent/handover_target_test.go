package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/agentapi"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/coordination"
	"github.com/nowakeai/betternat/internal/lease"
)

type fakeAgentReader struct {
	current lease.Record
	agents  []coordination.AgentRecord
}

func (r fakeAgentReader) Current(context.Context) (lease.Record, error) {
	if r.current.OwnerInstanceID == "" {
		return lease.Record{}, os.ErrNotExist
	}
	return r.current, nil
}

func (r fakeAgentReader) ListAgents(context.Context) ([]coordination.AgentRecord, error) {
	return r.agents, nil
}

func TestSelectHandoverTargetRejectsStaleLeaseGeneration(t *testing.T) {
	status := agentapi.StatusResponse{
		Cloud:           "gcp",
		LeaseGeneration: 9,
		Instances: []agentapi.StatusInstance{
			{NodeID: "gce-active", Role: "active", Fresh: true, LeaseGeneration: 9},
			{NodeID: "gce-stale", Role: "standby", Health: "Healthy", Fresh: true, LeaseGeneration: 8},
			{NodeID: "gce-fresh", Role: "standby", Health: "Healthy", Fresh: true, LeaseGeneration: 9},
		},
	}
	target, err := selectHandoverTarget(status, "gce-active", "auto")
	if err != nil {
		t.Fatalf("select auto target: %v", err)
	}
	if target != "gce-fresh" {
		t.Fatalf("expected fresh standby, got %q", target)
	}

	_, err = selectHandoverTarget(status, "gce-active", "gce-stale")
	if err == nil || !strings.Contains(err.Error(), "stale lease generation") {
		t.Fatalf("expected requested stale-generation target rejection, got %v", err)
	}
}

func TestSelectHandoverTargetRequiresGenerationForGCP(t *testing.T) {
	status := agentapi.StatusResponse{
		Cloud:           "gcp",
		LeaseGeneration: 9,
		Instances: []agentapi.StatusInstance{
			{NodeID: "gce-active", Role: "active", Fresh: true, LeaseGeneration: 9},
			{NodeID: "gce-unknown", Role: "standby", Health: "Healthy", Fresh: true},
		},
	}
	_, err := selectHandoverTarget(status, "gce-active", "gce-unknown")
	if err == nil || !strings.Contains(err.Error(), "missing lease generation") {
		t.Fatalf("expected requested missing-generation target rejection, got %v", err)
	}

	status.Cloud = "aws"
	target, err := selectHandoverTarget(status, "gce-active", "gce-unknown")
	if err != nil {
		t.Fatalf("missing generation should remain compatible outside GCP: %v", err)
	}
	if target != "gce-unknown" {
		t.Fatalf("unexpected target: %q", target)
	}
}

func TestControlStatusRegistryIncludesLeaseGenerationAndRouteMatch(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(validGCPHAConfigJSON()))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cache := newControlStatusCache(cfg)
	routeTargetMatch := true
	status := agentapi.StatusResponse{}
	cache.refreshRegistryStatus(context.Background(), cfg, fakeAgentReader{
		current: lease.Record{
			HAGroupID:       cfg.HAGroupID,
			OwnerInstanceID: "gce-active",
			Generation:      7,
			ExpiresAt:       time.Now().Add(time.Minute),
		},
		agents: []coordination.AgentRecord{{
			NodeID:           "gce-active",
			DatapathReady:    true,
			HAState:          "ACTIVE",
			LeaseGeneration:  7,
			RouteTargetMatch: true,
			UpdatedAt:        time.Now(),
		}},
	}, lease.Record{}, &status, time.Now())

	if status.LeaseGeneration != 7 || len(status.Instances) != 1 {
		t.Fatalf("unexpected registry status: %#v", status)
	}
	row := status.Instances[0]
	if row.LeaseGeneration != 7 {
		t.Fatalf("missing instance lease generation: %#v", row)
	}
	if row.RouteTargetMatch == nil || *row.RouteTargetMatch != routeTargetMatch {
		t.Fatalf("missing instance route target match: %#v", row)
	}
}
