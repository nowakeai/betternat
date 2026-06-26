package ha

import (
	"context"
	"testing"

	"github.com/nowakeai/betternat/internal/cloud"
)

func TestGCPHandoverPrioritizesRouteConnectivityBeforeStableIdentity(t *testing.T) {
	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "gce-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
		identity: cloud.PublicIdentity{AllocationID: "static-egress", InstanceID: "gce-active", PublicIP: "198.51.100.10"},
	}
	cfg := validHAConfig()
	cfg.Cloud = "gcp"
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	cfg.HA.PublicIdentity.AllocationID = "static-egress"
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "gce-active", "gce-standby", current)
	if err != nil {
		t.Fatalf("handover: %v", err)
	}
	if result.NewLease.OwnerInstanceID != "gce-standby" {
		t.Fatalf("unexpected new lease: %#v", result.NewLease)
	}
	if got := cloudProvider.routes["gcp-route:0.0.0.0/0"].Target; got != "gce-standby" {
		t.Fatalf("route should move to standby before stable IP repair, got %q", got)
	}
	for _, call := range cloudProvider.calls {
		if call == "associate:static-egress:gce-standby" {
			t.Fatalf("gcp handover should not block on stable IP association: %#v", cloudProvider.calls)
		}
	}
	wantCalls := []string{
		"replace:gcp-route:0.0.0.0/0:gce-standby",
		"describe-route:gcp-route:0.0.0.0/0",
		"describe-route:gcp-route:0.0.0.0/0",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}
