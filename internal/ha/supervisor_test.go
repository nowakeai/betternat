package ha

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/betternat/betternat/internal/cloud"
	"github.com/betternat/betternat/internal/config"
	"github.com/betternat/betternat/internal/datapath"
	"github.com/betternat/betternat/internal/lease"
)

func TestSupervisorRenewsWhenLocalInstanceAlreadyOwnsLease(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	record, err := manager.Acquire(context.Background(), "i-local")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	engine := &fakeDatapath{}
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-local"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "i-local"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-local", PublicIP: "198.51.100.10"},
	}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: manager, Datapath: engine},
		Now:        func() time.Time { return now },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-local")
	if result.Err != nil {
		t.Fatalf("step: %v", result.Err)
	}
	if result.State != StateActive {
		t.Fatalf("expected active, got %#v", result)
	}
	if result.Lease.Generation != record.Generation {
		t.Fatalf("renew should preserve generation: %#v", result.Lease)
	}
	if result.Activation.Lease.OwnerInstanceID != "i-local" {
		t.Fatalf("active ownership result should include renewed lease: %#v", result.Activation.Lease)
	}
	if !result.Lease.ExpiresAt.After(record.ExpiresAt) && !result.Lease.ExpiresAt.Equal(record.ExpiresAt) {
		t.Fatalf("unexpected expiry: before=%s after=%s", record.ExpiresAt, result.Lease.ExpiresAt)
	}
	if engine.reconcileCount != 1 {
		t.Fatalf("expected active datapath reconcile, got %d", engine.reconcileCount)
	}
}

func TestSupervisorStaysStandbyWhenAnotherOwnerLeaseIsValid(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-owner"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	cloudProvider := &fakeCloud{}
	engine := &fakeDatapath{}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: manager, Datapath: engine},
		Now:        func() time.Time { return now },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-standby")
	if result.Err != nil {
		t.Fatalf("step: %v", result.Err)
	}
	if result.State != StateStandby {
		t.Fatalf("expected standby, got %#v", result)
	}
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("standby should not mutate cloud: %#v", cloudProvider.calls)
	}
	if engine.reconcileCount != 1 {
		t.Fatalf("standby should keep datapath ready, got %d", engine.reconcileCount)
	}
}

func TestSupervisorDoesNotExitWhenStandbyDatapathReconcileFails(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-owner"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	supervisor := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: manager, Datapath: &fakeDatapath{reconcileErr: errors.New("loxilb not ready")}},
		Now:        func() time.Time { return now },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-standby")
	if result.Err == nil {
		t.Fatal("expected datapath reconcile error")
	}
	if result.State != StateStandby {
		t.Fatalf("standby datapath errors should not be terminal, got %#v", result)
	}
}

func TestSupervisorTimesOutBlockedDatapathReconcile(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-owner"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	supervisor := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: manager, Datapath: &fakeDatapath{blockUntilContextDone: true}},
		Now:        func() time.Time { return now },
	}

	start := time.Now()
	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-standby")
	elapsed := time.Since(start)
	if result.Err == nil {
		t.Fatal("expected datapath timeout error")
	}
	if result.State != StateStandby {
		t.Fatalf("blocked standby datapath should not be terminal, got %#v", result)
	}
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("datapath reconcile was not bounded: %s", elapsed)
	}
}

func TestSupervisorTimesOutBlockedLeaseCurrent(t *testing.T) {
	cfg := validSupervisorConfig()
	cfg.HA.Lease.TTLSeconds = 2
	supervisor := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: &blockingCurrentLease{}},
		Now:        func() time.Time { return time.Unix(100, 0) },
	}

	start := time.Now()
	result := supervisor.Step(context.Background(), cfg, "i-local")
	elapsed := time.Since(start)
	if result.Err == nil {
		t.Fatal("expected lease timeout error")
	}
	if result.State != StateStandby {
		t.Fatalf("blocked lease current should not be terminal, got %#v", result)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("lease current was not bounded: %s", elapsed)
	}
}

func TestSupervisorTakesOverExpiredLease(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-old"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	now = time.Unix(111, 0)
	cloudProvider := &fakeCloud{}
	engine := &fakeDatapath{}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: manager, Datapath: engine},
		Now:        func() time.Time { return now },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-new")
	if result.Err != nil {
		t.Fatalf("step: %v", result.Err)
	}
	if result.State != StateActive {
		t.Fatalf("expected active after takeover, got %#v", result)
	}
	if result.Lease.OwnerInstanceID != "i-new" {
		t.Fatalf("unexpected owner: %#v", result.Lease)
	}
	wantCalls := []string{
		"associate:eipalloc-123:i-new",
		"replace:rtb-a:0.0.0.0/0:i-new",
		"replace:rtb-b:0.0.0.0/0:i-new",
		"describe-route:rtb-a:0.0.0.0/0",
		"describe-route:rtb-b:0.0.0.0/0",
		"describe-eip:eipalloc-123",
	}
	if !equalStrings(cloudProvider.calls, wantCalls) {
		t.Fatalf("unexpected cloud calls: got %#v want %#v", cloudProvider.calls, wantCalls)
	}
	if engine.reconcileCount != 1 {
		t.Fatalf("takeover should reconcile datapath once, got %d", engine.reconcileCount)
	}
}

func TestSupervisorDoesNotMutateCloudWhenLeaseAcquireFails(t *testing.T) {
	supervisor := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: &failingAcquireLease{}},
		Now:        func() time.Time { return time.Unix(100, 0) },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-new")
	if result.Err == nil {
		t.Fatal("expected acquire failure")
	}
	if result.State != StateStandby {
		t.Fatalf("expected standby on failed takeover, got %#v", result)
	}
	cloudProvider := supervisor.Controller.Cloud.(*fakeCloud)
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("cloud should not be mutated before lease acquisition: %#v", cloudProvider.calls)
	}
}

func TestSupervisorDemotesWhenRenewIsFenced(t *testing.T) {
	current := lease.Record{
		HAGroupID:       "prod-egress-a",
		OwnerInstanceID: "i-local",
		Generation:      1,
		ExpiresAt:       time.Unix(120, 0),
		UpdatedAt:       time.Unix(100, 0),
	}
	supervisor := Supervisor{
		Controller: Controller{Lease: &renewFailLease{CurrentRecord: current}},
		Now:        func() time.Time { return time.Unix(112, 0) },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-local")
	if result.Err == nil {
		t.Fatal("expected renew fencing failure")
	}
	if result.State != StateDegraded {
		t.Fatalf("expected degraded after fenced renew, got %#v", result)
	}
}

func TestSupervisorDoesNotRepairOwnershipAfterRenewedLeaseExpires(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-local"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	now = time.Unix(105, 0)
	cloudProvider := &fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "i-other"},
		},
		identity: cloud.PublicIdentity{AllocationID: "eipalloc-123", InstanceID: "i-other", PublicIP: "198.51.100.10"},
	}
	engine := &fakeDatapath{onReconcile: func() {
		now = time.Unix(116, 0)
	}}
	supervisor := Supervisor{
		Controller: Controller{Cloud: cloudProvider, Lease: manager, Datapath: engine},
		Now:        func() time.Time { return now },
	}

	result := supervisor.Step(context.Background(), validSupervisorConfig(), "i-local")
	if result.Err == nil {
		t.Fatal("expected expired renewed lease error")
	}
	if result.State != StateDegraded {
		t.Fatalf("expected degraded state, got %#v", result)
	}
	if len(cloudProvider.calls) != 0 {
		t.Fatalf("expired active lease must not mutate cloud: %#v", cloudProvider.calls)
	}
}

func TestSupervisorOnlyOneCandidateWinsExpiredLease(t *testing.T) {
	now := time.Unix(100, 0)
	manager := lease.NewMemoryManager("prod-egress-a", 10*time.Second, func() time.Time { return now })
	if _, err := manager.Acquire(context.Background(), "i-old"); err != nil {
		t.Fatalf("acquire old: %v", err)
	}
	now = time.Unix(111, 0)
	first := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: manager},
		Now:        func() time.Time { return now },
	}
	second := Supervisor{
		Controller: Controller{Cloud: &fakeCloud{}, Lease: manager},
		Now:        func() time.Time { return now },
	}

	firstResult := first.Step(context.Background(), validSupervisorConfig(), "i-first")
	secondResult := second.Step(context.Background(), validSupervisorConfig(), "i-second")

	if firstResult.State != StateActive || firstResult.Err != nil {
		t.Fatalf("first candidate should win: %#v", firstResult)
	}
	if secondResult.State != StateStandby || secondResult.Err != nil {
		t.Fatalf("second candidate should observe valid owner and stay standby: %#v", secondResult)
	}
	current, err := manager.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.OwnerInstanceID != "i-first" {
		t.Fatalf("unexpected owner: %#v", current)
	}
}

type fakeDatapath struct {
	reconcileCount        int
	reconcileErr          error
	onReconcile           func()
	blockUntilContextDone bool
}

func (f *fakeDatapath) Name() string { return "fake" }

func (f *fakeDatapath) EnsureReady(context.Context, config.DatapathConfig) error { return nil }

func (f *fakeDatapath) Reconcile(ctx context.Context, _ config.DatapathConfig) error {
	f.reconcileCount++
	if f.onReconcile != nil {
		f.onReconcile()
	}
	if f.blockUntilContextDone {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.reconcileErr != nil {
		return f.reconcileErr
	}
	return nil
}

func (f *fakeDatapath) Status(context.Context) (datapath.Status, error) {
	return datapath.Status{Ready: true, Engine: f.Name()}, nil
}

func (f *fakeDatapath) Counters(context.Context) (datapath.Counters, error) {
	return datapath.Counters{}, nil
}

func (f *fakeDatapath) ConntrackSummary(context.Context) (datapath.ConntrackSummary, error) {
	return datapath.ConntrackSummary{}, nil
}

func (f *fakeDatapath) Cleanup(context.Context) error { return nil }

type failingAcquireLease struct{}

func (f *failingAcquireLease) Acquire(context.Context, string) (lease.Record, error) {
	return lease.Record{}, errors.New("lease unavailable")
}

func (f *failingAcquireLease) Renew(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, errors.New("not implemented")
}

func (f *failingAcquireLease) Release(context.Context, lease.Record) error {
	return errors.New("not implemented")
}

func (f *failingAcquireLease) Current(context.Context) (lease.Record, error) {
	return lease.Record{}, errors.New("lease is not held")
}

type blockingCurrentLease struct{}

func (b *blockingCurrentLease) Acquire(ctx context.Context, _ string) (lease.Record, error) {
	<-ctx.Done()
	return lease.Record{}, ctx.Err()
}

func (b *blockingCurrentLease) Renew(ctx context.Context, _ lease.Record) (lease.Record, error) {
	<-ctx.Done()
	return lease.Record{}, ctx.Err()
}

func (b *blockingCurrentLease) Release(ctx context.Context, _ lease.Record) error {
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingCurrentLease) Current(ctx context.Context) (lease.Record, error) {
	<-ctx.Done()
	return lease.Record{}, ctx.Err()
}

type renewFailLease struct {
	CurrentRecord lease.Record
}

func (s *renewFailLease) Acquire(context.Context, string) (lease.Record, error) {
	return lease.Record{}, errors.New("not implemented")
}

func (s *renewFailLease) Renew(context.Context, lease.Record) (lease.Record, error) {
	return lease.Record{}, errors.New("fencing check failed")
}

func (s *renewFailLease) Release(context.Context, lease.Record) error {
	return errors.New("not implemented")
}

func (s *renewFailLease) Current(context.Context) (lease.Record, error) {
	return s.CurrentRecord, nil
}

func validSupervisorConfig() config.Config {
	cfg := validHAConfig()
	cfg.HA.Lease.TTLSeconds = 10
	cfg.HA.Lease.RenewIntervalSeconds = 3
	return cfg
}
