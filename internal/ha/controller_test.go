package ha

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/lease"
	"github.com/nowakeai/betternat/internal/probe"
)

func TestActivateAssociatesEIPThenReplacesRoutes(t *testing.T) {
	cloudProvider := &fakeCloud{}
	leaseManager := &fakeLease{}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Activate(context.Background(), validHAConfig(), "i-active")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	if result.Lease.OwnerInstanceID != "i-active" {
		t.Fatalf("unexpected lease owner: %#v", result.Lease)
	}
	if result.PublicIdentity.AllocationID != "eipalloc-123" {
		t.Fatalf("unexpected public identity: %#v", result.PublicIdentity)
	}
	if len(result.Routes) != 2 {
		t.Fatalf("expected 2 replaced routes, got %#v", result.Routes)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-active",
		"replace:rtb-a:0.0.0.0/0:i-active",
		"replace:rtb-b:0.0.0.0/0:i-active",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestActivateRequiresLease(t *testing.T) {
	controller := Controller{Cloud: &fakeCloud{}}
	_, err := controller.Activate(context.Background(), validHAConfig(), "i-active")
	if err == nil {
		t.Fatal("expected missing lease error")
	}
}

func TestActivateRejectsUnsupportedRouteTargetType(t *testing.T) {
	cfg := validHAConfig()
	cfg.HA.RouteFailover.TargetType = "eni"
	controller := Controller{Cloud: &fakeCloud{}, Lease: &fakeLease{}}

	_, err := controller.Activate(context.Background(), cfg, "i-active")
	if err == nil {
		t.Fatal("expected unsupported target type error")
	}
}

func TestActivateNoopsWhenHADisabled(t *testing.T) {
	cfg := validHAConfig()
	cfg.HA.Enabled = false
	controller := Controller{}

	result, err := controller.Activate(context.Background(), cfg, "i-active")
	if err != nil {
		t.Fatalf("disabled activation should not fail: %v", err)
	}
	if len(result.Routes) != 0 {
		t.Fatalf("disabled activation should not replace routes: %#v", result.Routes)
	}
}

func TestActivateRunsOutboundProbeWhenEnabled(t *testing.T) {
	cfg := validHAConfig()
	cfg.Observability.OutboundProbe = config.OutboundProbeConfig{
		Enabled:    true,
		URL:        "https://checkip.example",
		ExpectedIP: "198.51.100.10",
	}
	probeRunner := &fakeProbe{result: probe.Result{ObservedIP: "198.51.100.10", ExpectedIP: "198.51.100.10", Matched: true}}
	controller := Controller{Cloud: &fakeCloud{}, Lease: &fakeLease{}, Probe: probeRunner}

	result, err := controller.Activate(context.Background(), cfg, "i-active")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if !probeRunner.called {
		t.Fatal("probe was not called")
	}
	if !result.Probe.Matched {
		t.Fatalf("unexpected probe result: %#v", result.Probe)
	}
}

func TestActivateFailsWhenOutboundProbeMismatches(t *testing.T) {
	cfg := validHAConfig()
	cfg.Observability.OutboundProbe = config.OutboundProbeConfig{
		Enabled:    true,
		URL:        "https://checkip.example",
		ExpectedIP: "198.51.100.10",
	}
	controller := Controller{
		Cloud: &fakeCloud{},
		Lease: &fakeLease{},
		Probe: &fakeProbe{result: probe.Result{ObservedIP: "203.0.113.10", ExpectedIP: "198.51.100.10", Matched: false}},
	}

	_, err := controller.Activate(context.Background(), cfg, "i-active")
	if err == nil {
		t.Fatal("expected probe mismatch error")
	}
}

func TestActivateReleasesLeaseWhenDatapathReconcileFails(t *testing.T) {
	leaseManager := &fakeLease{}
	controller := Controller{
		Cloud:    &fakeCloud{},
		Lease:    leaseManager,
		Datapath: &fakeDatapath{reconcileErr: errors.New("datapath not ready")},
	}

	_, err := controller.Activate(context.Background(), validHAConfig(), "i-active")
	if err == nil {
		t.Fatal("expected activation failure")
	}
	if leaseManager.releaseCount != 1 {
		t.Fatalf("expected failed activation to release lease, got %d", leaseManager.releaseCount)
	}
	if leaseManager.record.OwnerInstanceID != "" {
		t.Fatalf("lease should be cleared after failed activation: %#v", leaseManager.record)
	}
}

func TestActivateReleasesLeaseWhenRouteReplacementFails(t *testing.T) {
	leaseManager := &fakeLease{}
	controller := Controller{
		Cloud: &fakeCloud{replaceErr: errors.New("replace route failed")},
		Lease: leaseManager,
	}

	_, err := controller.Activate(context.Background(), validHAConfig(), "i-active")
	if err == nil {
		t.Fatal("expected activation failure")
	}
	if leaseManager.releaseCount != 1 {
		t.Fatalf("expected failed activation to release lease, got %d", leaseManager.releaseCount)
	}
	if leaseManager.record.OwnerInstanceID != "" {
		t.Fatalf("lease should be cleared after failed activation: %#v", leaseManager.record)
	}
}

func TestActivateFailsWhenLeaseExpiresDuringActivation(t *testing.T) {
	now := time.Unix(200, 0)
	controller := Controller{
		Cloud: &fakeCloud{},
		Lease: &fakeLease{expiresAt: time.Unix(199, 0)},
		Now:   func() time.Time { return now },
	}

	_, err := controller.Activate(context.Background(), validHAConfig(), "i-active")
	if err == nil {
		t.Fatal("expected expired lease error")
	}
}

func TestActivateDoesNotMutateRouteWhenAnotherOwnerHoldsLease(t *testing.T) {
	cfg := validRouteOnlyHAConfig()
	leaseManager := lease.NewMemoryManager("prod-egress-a", time.Minute, time.Now)
	if _, err := leaseManager.Acquire(context.Background(), "i-active"); err != nil {
		t.Fatalf("acquire active lease: %v", err)
	}
	cloudProvider := &fakeCloud{}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	_, err := controller.Activate(context.Background(), cfg, "i-standby")
	if err == nil {
		t.Fatal("expected standby activation to fail while active lease is held")
	}
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("standby must not mutate cloud while another owner holds the lease: %#v", cloudProvider.calls)
	}
}

func TestEnsureOwnershipRepairsDrift(t *testing.T) {
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-old"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-old"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-old", PublicIP: "198.51.100.10"},
	}
	controller := Controller{Cloud: cloudProvider}

	result, err := controller.EnsureOwnership(context.Background(), validHAConfig(), "i-active")
	if err != nil {
		t.Fatalf("ensure ownership: %v", err)
	}
	if result.PublicIdentity.InstanceID != "i-active" {
		t.Fatalf("unexpected identity: %#v", result.PublicIdentity)
	}
	for _, route := range result.Routes {
		if route.Target != "i-active" {
			t.Fatalf("route was not repaired: %#v", result.Routes)
		}
	}
	wantCalls := []string{
		"describe-eip:eipalloc-123",
		"associate:eipalloc-123:i-active",
		"describe-route:rtb-a:0.0.0.0/0",
		"replace:rtb-a:0.0.0.0/0:i-active",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
		"replace:rtb-b:0.0.0.0/0:i-active",
		"describe-route:rtb-b:0.0.0.0/0",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestEnsureOwnershipRepairsMissingRoute(t *testing.T) {
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
	}
	controller := Controller{Cloud: cloudProvider}

	result, err := controller.EnsureOwnership(context.Background(), validHAConfig(), "i-active")
	if err != nil {
		t.Fatalf("ensure ownership: %v", err)
	}
	if len(result.Routes) != 2 {
		t.Fatalf("expected both routes after repair, got %#v", result.Routes)
	}
	wantCalls := []string{
		"describe-eip:eipalloc-123",
		"describe-route:rtb-a:0.0.0.0/0",
		"replace:rtb-a:0.0.0.0/0:i-active",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestEnsureOwnershipDoesNotMutateWhenAlreadyOwned(t *testing.T) {
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
	}
	controller := Controller{Cloud: cloudProvider}

	if _, err := controller.EnsureOwnership(context.Background(), validHAConfig(), "i-active"); err != nil {
		t.Fatalf("ensure ownership: %v", err)
	}
	wantCalls := []string{
		"describe-eip:eipalloc-123",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestEnsureOwnershipFencedStopsWhenLeaseChangesBeforeRouteRepair(t *testing.T) {
	leaseManager := &fakeLease{}
	record, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-old"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-old"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-old", PublicIP: "198.51.100.10"},
		onAssociate: func() {
			leaseManager.record.Generation++
			leaseManager.record.OwnerInstanceID = "i-other"
		},
	}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	_, err = controller.EnsureOwnershipFenced(context.Background(), validHAConfig(), "i-active", record)
	if err == nil {
		t.Fatal("expected fenced ownership repair to fail")
	}
	wantCalls := []string{
		"describe-eip:eipalloc-123",
		"associate:eipalloc-123:i-active",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("route repair must not continue after lease changes: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverMovesCloudOwnershipThenTransfersLease(t *testing.T) {
	leaseManager := &fakeLease{}
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
	if err != nil {
		t.Fatalf("handover: %v", err)
	}
	if result.NewLease.OwnerInstanceID != "i-standby" || result.NewLease.Generation != current.Generation+1 {
		t.Fatalf("unexpected new lease: %#v", result.NewLease)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-standby",
		"replace:rtb-a:0.0.0.0/0:i-standby",
		"describe-route:rtb-a:0.0.0.0/0",
		"replace:rtb-b:0.0.0.0/0:i-standby",
		"describe-route:rtb-b:0.0.0.0/0",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverStopsWhenLeaseChangesBeforeRouteMutation(t *testing.T) {
	leaseManager := &fakeLease{}
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
		onAssociate: func() {
			leaseManager.record.Generation++
			leaseManager.record.OwnerInstanceID = "i-other"
		},
	}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	_, err = controller.Handover(context.Background(), validHAConfig(), "i-active", "i-standby", current)
	if err == nil {
		t.Fatal("expected handover to fail after lease change")
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-standby",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("handover route mutation must not continue after lease changes: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverAcceptsAmbiguousRouteReplaceErrorWhenRouteConverged(t *testing.T) {
	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:              cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		replaceErrs:           []error{context.DeadlineExceeded},
		replaceMutatesOnError: true,
	}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), validHAConfig(), "i-active", "i-standby", current)
	if err != nil {
		t.Fatalf("handover: %v", err)
	}
	if result.NewLease.OwnerInstanceID != "i-standby" {
		t.Fatalf("unexpected new lease: %#v", result.NewLease)
	}
}

func TestHandoverRetriesRouteReplaceUntilRouteConverges(t *testing.T) {
	oldBackoffs := handoverRouteReplaceBackoffs
	handoverRouteReplaceBackoffs = []time.Duration{0, 0}
	defer func() { handoverRouteReplaceBackoffs = oldBackoffs }()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:    cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		replaceErrs: []error{errors.New("transient route timeout")},
	}
	cfg := validHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"rtb-a"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "i-active", "i-standby", current)
	if err != nil {
		t.Fatalf("handover: %v", err)
	}
	if result.NewLease.OwnerInstanceID != "i-standby" {
		t.Fatalf("unexpected new lease: %#v", result.NewLease)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-standby",
		"replace:rtb-a:0.0.0.0/0:i-standby",
		"describe-route:rtb-a:0.0.0.0/0",
		"replace:rtb-a:0.0.0.0/0:i-standby",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRevertsRouteOnlyWhenRouteDoesNotConverge(t *testing.T) {
	oldBackoffs := handoverRouteReplaceBackoffs
	handoverRouteReplaceBackoffs = []time.Duration{0, 0}
	defer func() { handoverRouteReplaceBackoffs = oldBackoffs }()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "gce-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
		describeRouteResults: []cloud.RouteTarget{
			{RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
			{RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
	}
	cfg := validRouteOnlyHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "gce-active", "gce-standby", current)
	if err == nil {
		t.Fatal("expected handover to fail when route does not converge")
	}
	if !result.Reverted {
		t.Fatalf("expected route-only handover to revert failed ownership: %#v", result)
	}
	if leaseManager.record.OwnerInstanceID != "gce-active" {
		t.Fatalf("lease must not transfer after failed route convergence: %#v", leaseManager.record)
	}
	if got := cloudProvider.routes["gcp-route:0.0.0.0/0"].Target; got != "gce-active" {
		t.Fatalf("route should be reverted to active, got %q", got)
	}
	wantCalls := []string{
		"replace:gcp-route:0.0.0.0/0:gce-standby",
		"describe-route:gcp-route:0.0.0.0/0",
		"replace:gcp-route:0.0.0.0/0:gce-standby",
		"describe-route:gcp-route:0.0.0.0/0",
		"replace:gcp-route:0.0.0.0/0:gce-active",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRevertsRouteOnlyWhenLeaseChangesAfterRouteMutation(t *testing.T) {
	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "gce-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	replaceCalls := 0
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"gcp-route:0.0.0.0/0": {RouteTableID: "gcp-route", DestinationCIDR: "0.0.0.0/0", Target: "gce-active"},
		},
		onReplace: func() {
			if replaceCalls == 0 {
				leaseManager.record.OwnerInstanceID = "gce-other"
				leaseManager.record.Generation++
			}
			replaceCalls++
		},
	}
	cfg := validRouteOnlyHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"gcp-route"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "gce-active", "gce-standby", current)
	if err == nil {
		t.Fatal("expected handover to fail after lease generation changes")
	}
	if !result.Reverted {
		t.Fatalf("expected route-only handover to revert failed ownership: %#v", result)
	}
	if leaseManager.record.OwnerInstanceID != "gce-other" || leaseManager.record.Generation != current.Generation+1 {
		t.Fatalf("stale active must not transfer the lease back to itself or standby: %#v", leaseManager.record)
	}
	if got := cloudProvider.routes["gcp-route:0.0.0.0/0"].Target; got != "gce-active" {
		t.Fatalf("route should be reverted to original active, got %q", got)
	}
	wantCalls := []string{
		"replace:gcp-route:0.0.0.0/0:gce-standby",
		"replace:gcp-route:0.0.0.0/0:gce-active",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRejectsNonActiveRequester(t *testing.T) {
	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	_, err = controller.Handover(context.Background(), validHAConfig(), "i-standby", "i-other", current)
	if err == nil {
		t.Fatal("expected non-active handover rejection")
	}
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("non-active requester must not mutate cloud: %#v", cloudProvider.calls)
	}
}

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

type fakeCloud struct {
	calls                 []string
	routes                map[string]cloud.RouteTarget
	identity              cloud.PublicIdentity
	replaceErr            error
	replaceErrs           []error
	replaceMutatesOnError bool
	describeRouteResults  []cloud.RouteTarget
	describeRouteErrs     []error
	onAssociate           func()
	onReplace             func()
}

func (f *fakeCloud) ReplaceRoute(_ context.Context, target cloud.RouteTarget) error {
	if f.routes == nil {
		f.routes = map[string]cloud.RouteTarget{}
	}
	f.calls = append(f.calls, "replace:"+target.RouteTableID+":"+target.DestinationCIDR+":"+target.Target)
	err := f.replaceErr
	if len(f.replaceErrs) > 0 {
		err = f.replaceErrs[0]
		f.replaceErrs = f.replaceErrs[1:]
	}
	if err == nil || f.replaceMutatesOnError {
		f.routes[target.RouteTableID+":"+target.DestinationCIDR] = target
	}
	if f.onReplace != nil {
		f.onReplace()
	}
	if err != nil {
		return err
	}
	return nil
}

func (f *fakeCloud) AssociateEIP(_ context.Context, allocationID string, instanceID string) (cloud.PublicIdentity, error) {
	f.calls = append(f.calls, "associate:"+allocationID+":"+instanceID)
	if f.onAssociate != nil {
		f.onAssociate()
	}
	f.identity = cloud.PublicIdentity{
		AllocationID: allocationID,
		PublicIP:     "198.51.100.10",
		InstanceID:   instanceID,
		PrivateIP:    "10.0.1.10",
	}
	return f.identity, nil
}

func (f *fakeCloud) DescribeRoute(_ context.Context, routeTableID string, destinationCIDR string) (cloud.RouteTarget, error) {
	f.calls = append(f.calls, "describe-route:"+routeTableID+":"+destinationCIDR)
	if len(f.describeRouteResults) > 0 || len(f.describeRouteErrs) > 0 {
		var route cloud.RouteTarget
		if len(f.describeRouteResults) > 0 {
			route = f.describeRouteResults[0]
			f.describeRouteResults = f.describeRouteResults[1:]
		}
		var err error
		if len(f.describeRouteErrs) > 0 {
			err = f.describeRouteErrs[0]
			f.describeRouteErrs = f.describeRouteErrs[1:]
		}
		return route, err
	}
	route, ok := f.routes[routeTableID+":"+destinationCIDR]
	if !ok {
		return cloud.RouteTarget{}, errors.New("route not found")
	}
	return route, nil
}

func (f *fakeCloud) DescribePublicIdentity(_ context.Context, allocationID string) (cloud.PublicIdentity, error) {
	f.calls = append(f.calls, "describe-eip:"+allocationID)
	if f.identity.AllocationID == "" {
		return cloud.PublicIdentity{}, errors.New("identity not found")
	}
	return f.identity, nil
}

type fakeProbe struct {
	called bool
	result probe.Result
	err    error
}

func (f *fakeProbe) Run(context.Context) (probe.Result, error) {
	f.called = true
	return f.result, f.err
}

type fakeLease struct {
	record       lease.Record
	expiresAt    time.Time
	releaseCount int
	transferErr  error
}

func (f *fakeLease) Acquire(_ context.Context, owner string) (lease.Record, error) {
	expiresAt := f.expiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Hour)
	}
	f.record = lease.Record{
		HAGroupID:       "prod-egress-a",
		OwnerInstanceID: owner,
		Generation:      1,
		ExpiresAt:       expiresAt,
		UpdatedAt:       time.Now(),
	}
	return f.record, nil
}

func (f *fakeLease) Renew(_ context.Context, record lease.Record) (lease.Record, error) {
	if f.record.OwnerInstanceID == "" {
		return lease.Record{}, errors.New("lease not held")
	}
	if f.record.OwnerInstanceID != record.OwnerInstanceID || f.record.Generation != record.Generation {
		return lease.Record{}, errors.New("lease fencing check failed")
	}
	expiresAt := f.expiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Hour)
	}
	f.record.ExpiresAt = expiresAt
	f.record.UpdatedAt = time.Now()
	return f.record, nil
}

func (f *fakeLease) Release(context.Context, lease.Record) error {
	f.releaseCount++
	f.record = lease.Record{}
	return nil
}

func (f *fakeLease) Transfer(_ context.Context, record lease.Record, newOwner string) (lease.Record, error) {
	if f.transferErr != nil {
		return lease.Record{}, f.transferErr
	}
	if f.record.OwnerInstanceID != record.OwnerInstanceID || f.record.Generation != record.Generation {
		return lease.Record{}, errors.New("lease fencing check failed")
	}
	f.record.OwnerInstanceID = newOwner
	f.record.Generation++
	return f.record, nil
}

func (f *fakeLease) Current(context.Context) (lease.Record, error) {
	if f.record.OwnerInstanceID == "" {
		return lease.Record{}, errors.New("lease not held")
	}
	return f.record, nil
}

func validHAConfig() config.Config {
	return config.Config{
		HA: config.HAConfig{
			Enabled: true,
			RouteFailover: config.RouteFailoverConfig{
				Mode:            "replace_route",
				RouteTableIDs:   []string{"rtb-a", "rtb-b"},
				DestinationCIDR: "0.0.0.0/0",
				TargetType:      "instance",
			},
			PublicIdentity: config.PublicIdentityConfig{
				Mode:         "shared_eip",
				AllocationID: "eipalloc-123",
			},
		},
	}
}

func validRouteOnlyHAConfig() config.Config {
	cfg := validHAConfig()
	cfg.HA.PublicIdentity = config.PublicIdentityConfig{}
	return cfg
}

func equalStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
