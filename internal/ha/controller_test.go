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
	if len(cloudProvider.replaceFast) != 2 || !cloudProvider.replaceFast[0] || !cloudProvider.replaceFast[1] {
		t.Fatalf("handover route replacement should request fast path: %#v", cloudProvider.replaceFast)
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

func TestHandoverAcceptsAmbiguousPublicIdentityErrorWhenIdentityConverged(t *testing.T) {
	oldBackoffs := handoverPublicIdentityBackoffs
	handoverPublicIdentityBackoffs = []time.Duration{0}
	defer func() { handoverPublicIdentityBackoffs = oldBackoffs }()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:                cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		associateErrs:           []error{errors.New("operation polling reset")},
		associateMutatesOnError: true,
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
		"describe-eip:eipalloc-123",
		"replace:rtb-a:0.0.0.0/0:i-standby",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRetriesTransientLeaseReadAfterPublicIdentityMutation(t *testing.T) {
	oldBackoffs := leaseFenceReadBackoffs
	leaseFenceReadBackoffs = []time.Duration{0, 0}
	defer func() { leaseFenceReadBackoffs = oldBackoffs }()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		onAssociate: func() {
			leaseManager.currentErrs = append(leaseManager.currentErrs, errors.New("rpc error: code = Unavailable desc = error reading from server: read tcp: read: connection reset by peer"))
		},
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
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRenewsLeaseDuringPublicIdentityMutation(t *testing.T) {
	oldMinRenewDelay := leaseFenceMinRenewDelay
	oldMaxRenewDelay := leaseFenceMaxRenewDelay
	leaseFenceMinRenewDelay = time.Millisecond
	leaseFenceMaxRenewDelay = time.Millisecond
	defer func() {
		leaseFenceMinRenewDelay = oldMinRenewDelay
		leaseFenceMaxRenewDelay = oldMaxRenewDelay
	}()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:       cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		associateDelay: 10 * time.Millisecond,
	}
	cfg := validHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"rtb-a"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	if _, err := controller.Handover(context.Background(), cfg, "i-active", "i-standby", current); err != nil {
		t.Fatalf("handover: %v", err)
	}
	if leaseManager.renewCount < 2 {
		t.Fatalf("expected lease renewals during public identity mutation, got %d", leaseManager.renewCount)
	}
}

func TestHandoverCancelsPublicIdentityMutationWhenLeaseRenewFails(t *testing.T) {
	oldMinRenewDelay := leaseFenceMinRenewDelay
	oldMaxRenewDelay := leaseFenceMaxRenewDelay
	oldBackoffs := handoverPublicIdentityBackoffs
	leaseFenceMinRenewDelay = time.Millisecond
	leaseFenceMaxRenewDelay = time.Millisecond
	handoverPublicIdentityBackoffs = []time.Duration{0}
	defer func() {
		leaseFenceMinRenewDelay = oldMinRenewDelay
		leaseFenceMaxRenewDelay = oldMaxRenewDelay
		handoverPublicIdentityBackoffs = oldBackoffs
	}()

	leaseManager := &fakeLease{renewErrs: []error{
		nil, // ownership-lock fence.
		nil, // pre-mutation fence.
		nil, // first heartbeat renewal during the mutation.
		errors.New("lease fencing check failed"),
	}}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:       cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		associateDelay: time.Second,
	}
	cfg := validHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"rtb-a"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "i-active", "i-standby", current)
	if err == nil {
		t.Fatal("expected handover failure")
	}
	if !result.Reverted {
		t.Fatalf("expected public identity revert after lease renew failure: %#v", result)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-standby",
		"describe-eip:eipalloc-123",
		"associate:eipalloc-123:i-active",
		"replace:rtb-a:0.0.0.0/0:i-active",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
}

func TestHandoverRevertsPublicIdentityWhenAssociateDoesNotConverge(t *testing.T) {
	oldBackoffs := handoverPublicIdentityBackoffs
	handoverPublicIdentityBackoffs = []time.Duration{0, 0}
	defer func() { handoverPublicIdentityBackoffs = oldBackoffs }()

	leaseManager := &fakeLease{}
	current, err := leaseManager.Acquire(context.Background(), "i-active")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-active"},
		},
		identity:      cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-active", PublicIP: "198.51.100.10"},
		associateErrs: []error{errors.New("operation polling reset"), errors.New("operation polling reset")},
	}
	cfg := validHAConfig()
	cfg.HA.RouteFailover.RouteTableIDs = []string{"rtb-a"}
	controller := Controller{Cloud: cloudProvider, Lease: leaseManager}

	result, err := controller.Handover(context.Background(), cfg, "i-active", "i-standby", current)
	if err == nil {
		t.Fatal("expected handover failure")
	}
	if !result.Reverted {
		t.Fatalf("expected public identity revert: %#v", result)
	}
	if cloudProvider.identity.InstanceID != "i-active" {
		t.Fatalf("identity should be reverted to active: %#v", cloudProvider.identity)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-standby",
		"describe-eip:eipalloc-123",
		"associate:eipalloc-123:i-standby",
		"describe-eip:eipalloc-123",
		"associate:eipalloc-123:i-active",
		"replace:rtb-a:0.0.0.0/0:i-active",
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
