package ha

import (
	"context"
	"errors"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/lease"
	"github.com/nowakeai/betternat/internal/probe"
)

type fakeCloud struct {
	calls                   []string
	routes                  map[string]cloud.RouteTarget
	identity                cloud.PublicIdentity
	associateErrs           []error
	associateMutatesOnError bool
	associateDelay          time.Duration
	replaceErr              error
	replaceErrs             []error
	replaceMutatesOnError   bool
	describeRouteResults    []cloud.RouteTarget
	describeRouteErrs       []error
	onAssociate             func()
	onReplace               func()
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

func (f *fakeCloud) AssociateEIP(ctx context.Context, allocationID string, instanceID string) (cloud.PublicIdentity, error) {
	f.calls = append(f.calls, "associate:"+allocationID+":"+instanceID)
	if f.onAssociate != nil {
		f.onAssociate()
	}
	if f.associateDelay > 0 {
		timer := time.NewTimer(f.associateDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return cloud.PublicIdentity{}, ctx.Err()
		case <-timer.C:
		}
	}
	var err error
	if len(f.associateErrs) > 0 {
		err = f.associateErrs[0]
		f.associateErrs = f.associateErrs[1:]
	}
	if err != nil && !f.associateMutatesOnError {
		return cloud.PublicIdentity{}, err
	}
	f.identity = cloud.PublicIdentity{
		AllocationID: allocationID,
		PublicIP:     "198.51.100.10",
		InstanceID:   instanceID,
		PrivateIP:    "10.0.1.10",
	}
	if err != nil {
		return cloud.PublicIdentity{}, err
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
	renewCount   int
	transferErr  error
	currentErrs  []error
	renewErrs    []error
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
	f.renewCount++
	if len(f.renewErrs) > 0 {
		err := f.renewErrs[0]
		f.renewErrs = f.renewErrs[1:]
		if err != nil {
			return lease.Record{}, err
		}
	}
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
	if len(f.currentErrs) > 0 {
		err := f.currentErrs[0]
		f.currentErrs = f.currentErrs[1:]
		if err != nil {
			return lease.Record{}, err
		}
	}
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
