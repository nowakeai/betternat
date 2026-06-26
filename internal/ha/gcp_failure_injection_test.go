package ha

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nowakeai/betternat/internal/cloud"
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
