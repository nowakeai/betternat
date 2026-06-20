package ha

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/betternat/betternat/internal/cloud"
	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/lease"
	"github.com/betternat/betternat/internal/probe"
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

type fakeCloud struct {
	calls    []string
	routes   map[string]cloud.RouteTarget
	identity cloud.PublicIdentity
}

func (f *fakeCloud) ReplaceRoute(_ context.Context, target cloud.RouteTarget) error {
	if f.routes == nil {
		f.routes = map[string]cloud.RouteTarget{}
	}
	f.calls = append(f.calls, "replace:"+target.RouteTableID+":"+target.DestinationCIDR+":"+target.Target)
	f.routes[target.RouteTableID+":"+target.DestinationCIDR] = target
	return nil
}

func (f *fakeCloud) AssociateEIP(_ context.Context, allocationID string, instanceID string) (cloud.PublicIdentity, error) {
	f.calls = append(f.calls, "associate:"+allocationID+":"+instanceID)
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
	record lease.Record
}

func (f *fakeLease) Acquire(_ context.Context, owner string) (lease.Record, error) {
	f.record = lease.Record{
		HAGroupID:       "prod-egress-a",
		OwnerInstanceID: owner,
		Generation:      1,
		ExpiresAt:       time.Unix(100, 0),
		UpdatedAt:       time.Unix(90, 0),
	}
	return f.record, nil
}

func (f *fakeLease) Renew(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, errors.New("not implemented")
}

func (f *fakeLease) Release(context.Context, lease.Record) error {
	return errors.New("not implemented")
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
