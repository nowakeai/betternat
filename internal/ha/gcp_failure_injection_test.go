package ha

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/lease"
)

func TestEnsureOwnershipFencedDoesNotMutateRouteWhenDescribeFails(t *testing.T) {
	leaseManager := &fakeLease{}
	record, err := leaseManager.Acquire(context.Background(), "gce-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
		describeRouteErrs: []error{errors.New("gcp compute get route: connection reset by peer")},
	}
	cfg := validRouteOnlyHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	_, err = controller.EnsureOwnershipFenced(context.Background(), cfg, "gce-active", record)
	if err == nil || !strings.Contains(err.Error(), "describe route") {
		t.Fatalf("expected describe route failure, got %v", err)
	}
	wantCalls := []string{"describe-route:gcp-route:0.0.0.0/0"}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("transient describe failure must not trigger route mutation: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestEnsureOwnershipFencedRepairsMissingRoute(t *testing.T) {
	leaseManager := &fakeLease{}
	record, err := leaseManager.Acquire(context.Background(), "gce-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{},
	}
	cfg := validRouteOnlyHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.EnsureOwnershipFenced(context.Background(), cfg, "gce-active", record)
	if err != nil {
		t.Fatalf("ensure ownership: %v", err)
	}
	if len(result.Routes) != 1 || result.Routes[0].Target != "gce-active" {
		t.Fatalf("missing route should be repaired to active: %#v", result.Routes)
	}
	wantCalls := []string{
		"describe-route:gcp-route:0.0.0.0/0",
		"replace:gcp-route:0.0.0.0/0:gce-active",
		"describe-route:gcp-route:0.0.0.0/0",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestSupervisorDegradesWithoutRouteMutationWhenActiveDescribeFails(t *testing.T) {
	leaseManager := &fakeLease{}
	if _, err := leaseManager.Acquire(context.Background(), "gce-active"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
		describeRouteErrs: []error{errors.New("gcp compute get route: connection reset by peer")},
	}
	cfg := validSupervisorConfig()
	cfg.HA.PublicIdentity.Mode = ""
	cfg.HA.PublicIdentity.AllocationID = ""
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: leaseManager, Datapath: &fakeDatapath{}},
	}

	result := supervisor.Step(context.Background(), cfg, "gce-active")
	if result.Err == nil {
		t.Fatal("expected route describe failure")
	}
	if result.State != StateDegraded {
		t.Fatalf("active should degrade on route describe failure, got %#v", result)
	}
	wantCalls := []string{"describe-route:gcp-route:0.0.0.0/0"}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("active describe failure must not mutate route: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestSupervisorRestartedOldActiveStaysStandbyWhenLeaseMoved(t *testing.T) {
	leaseManager := &fakeLease{}
	leaseManager.record = lease.Record{
		HAGroupID:       "prod-egress-a",
		OwnerInstanceID: "gce-new-active",
		Generation:      12,
		ExpiresAt:       time.Now().Add(time.Minute),
		UpdatedAt:       time.Now(),
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-new-active"},
		},
	}
	reporter := NewMemoryStatus()
	reporter.Report(StepResult{
		State: StateActive,
		Lease: lease.Record{
			HAGroupID:       "prod-egress-a",
			OwnerInstanceID: "gce-old-active",
			Generation:      11,
			ExpiresAt:       time.Now().Add(time.Minute),
		},
	})
	cfg := validSupervisorConfig()
	cfg.HA.PublicIdentity.Mode = ""
	cfg.HA.PublicIdentity.AllocationID = ""
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	engine := &fakeDatapath{}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: leaseManager, Datapath: engine},
		Reporter:   reporter,
	}

	result := supervisor.Step(context.Background(), cfg, "gce-old-active")
	if result.Err != nil {
		t.Fatalf("step: %v", result.Err)
	}
	if result.State != StateStandby {
		t.Fatalf("old active should become standby after lease moves, got %#v", result)
	}
	if result.Lease.OwnerInstanceID != "gce-new-active" || result.Lease.Generation != 12 {
		t.Fatalf("expected fresh moved lease, got %#v", result.Lease)
	}
	if engine.reconcileCount != 1 {
		t.Fatalf("standby should only reconcile local datapath, got %d", engine.reconcileCount)
	}
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("restarted old active must not repair cloud ownership: %#v", cloudProvider.calls)
	}
}
