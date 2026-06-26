package ha

import (
	"context"
	"errors"
	"testing"

	"github.com/nowakeai/betternat/internal/cloud"
)

func TestHandoverRevertsCloudOwnershipWhenLeaseTransferFails(t *testing.T) {
	leaseManager := &fakeLease{transferErr: errors.New("fenced")}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
	}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), validHAConfig(), "i-active", "i-standby", current)
	if err == nil {
		t.Fatal("expected transfer failure")
	}
	if !result.Reverted {
		t.Fatalf("expected revert flag: %#v", result)
	}
	if cloudProvider.identity.InstanceID != "i-active" {
		t.Fatalf("EIP should be reverted to active: %#v", cloudProvider.identity)
	}
	for _, route := range cloudProvider.routes {
		if route.Target != "i-active" {
			t.Fatalf("routes should be reverted to active: %#v", cloudProvider.routes)
		}
	}
}
