package ha

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/lease"
	"github.com/nowakeai/betternat/internal/probe"
)

type Controller struct {
	Cloud       cloud.Provider
	Lease       lease.Manager
	Datapath    datapath.Engine
	Probe       ProbeRunner
	Now         lease.Clock
	OwnershipMu *sync.Mutex
}

type ActivationResult struct {
	Lease          lease.Record         `json:"lease"`
	PublicIdentity cloud.PublicIdentity `json:"public_identity"`
	Routes         []cloud.RouteTarget  `json:"routes"`
	Probe          probe.Result         `json:"probe"`
}

type HandoverResult struct {
	PreviousLease  lease.Record         `json:"previous_lease"`
	NewLease       lease.Record         `json:"new_lease"`
	PublicIdentity cloud.PublicIdentity `json:"public_identity"`
	Routes         []cloud.RouteTarget  `json:"routes"`
	Reverted       bool                 `json:"reverted"`
}

type ProbeRunner interface {
	Run(ctx context.Context) (probe.Result, error)
}

var handoverRouteReplaceBackoffs = []time.Duration{
	0,
	250 * time.Millisecond,
	750 * time.Millisecond,
	1500 * time.Millisecond,
}

var handoverRouteReplaceAttemptTimeout = 8 * time.Second

// Activate claims ownership and points the cloud control plane at this appliance.
func (c Controller) Activate(ctx context.Context, cfg config.Config, localInstanceID string) (ActivationResult, error) {
	if !cfg.HA.Enabled {
		return ActivationResult{}, nil
	}
	if localInstanceID == "" || localInstanceID == "auto" {
		return ActivationResult{}, fmt.Errorf("local instance id is required for HA activation")
	}
	if c.Cloud == nil {
		return ActivationResult{}, fmt.Errorf("cloud provider is required for HA activation")
	}
	if c.Lease == nil {
		return ActivationResult{}, fmt.Errorf("lease manager is required for HA activation")
	}

	record, err := c.Lease.Acquire(ctx, localInstanceID)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("acquire HA lease: %w", err)
	}
	fail := func(format string, args ...any) (ActivationResult, error) {
		activationErr := fmt.Errorf(format, args...)
		if releaseErr := c.Lease.Release(ctx, record); releaseErr != nil {
			return ActivationResult{}, fmt.Errorf("%w; release HA lease after failed activation: %v", activationErr, releaseErr)
		}
		return ActivationResult{}, activationErr
	}
	if c.Datapath != nil {
		if err := c.Datapath.Reconcile(ctx, cfg.Datapath); err != nil {
			return fail("reconcile datapath before activation: %w", err)
		}
	}
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return fail("verify HA lease before cloud activation: %w", err)
	}

	unlock := c.lockOwnership()
	defer unlock()
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return fail("verify HA lease after acquiring ownership lock: %w", err)
	}

	result := ActivationResult{Lease: record}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		if cfg.HA.PublicIdentity.AllocationID == "" {
			return fail("ha.public_identity.allocation_id is required for shared_eip")
		}
		identity, err := c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, localInstanceID)
		if err != nil {
			return fail("associate EIP %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
		}
		result.PublicIdentity = identity
	} else if cfg.HA.PublicIdentity.Mode != "" {
		return fail("unsupported public identity mode %q", cfg.HA.PublicIdentity.Mode)
	}

	routes, err := routeTargets(cfg, localInstanceID)
	if err != nil {
		return fail("%w", err)
	}
	for _, target := range routes {
		if err := c.Cloud.ReplaceRoute(ctx, target); err != nil {
			return fail("replace route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		result.Routes = append(result.Routes, target)
	}
	for _, target := range result.Routes {
		actual, err := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if err != nil {
			return fail("verify route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		if actual.Target != target.Target {
			return fail("route %s %s target is %q, expected %q", target.RouteTableID, target.DestinationCIDR, actual.Target, target.Target)
		}
	}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		actual, err := c.Cloud.DescribePublicIdentity(ctx, cfg.HA.PublicIdentity.AllocationID)
		if err != nil {
			return fail("verify public identity %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
		}
		if actual.InstanceID != localInstanceID {
			return fail("public identity %q is on %q, expected %q", cfg.HA.PublicIdentity.AllocationID, actual.InstanceID, localInstanceID)
		}
		result.PublicIdentity = actual
	}
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return fail("verify HA lease after cloud activation: %w", err)
	}
	if cfg.Observability.OutboundProbe.Enabled {
		runner := c.Probe
		if runner == nil {
			runner = probe.SourceIPProbe{
				URL:        cfg.Observability.OutboundProbe.URL,
				ExpectedIP: cfg.Observability.OutboundProbe.ExpectedIP,
			}
		}
		probeResult, err := runner.Run(ctx)
		if err != nil {
			return fail("outbound source IP probe: %w", err)
		}
		if !probeResult.Matched {
			return fail("outbound source IP probe observed %s, expected %s", probeResult.ObservedIP, probeResult.ExpectedIP)
		}
		result.Probe = probeResult
	}
	return result, nil
}

func (c Controller) VerifyLease(ctx context.Context, record lease.Record, localInstanceID string) error {
	if c.Lease == nil {
		return fmt.Errorf("lease manager is required")
	}
	current, err := c.Lease.Current(ctx)
	if err != nil {
		return fmt.Errorf("read HA lease: %w", err)
	}
	if current.OwnerInstanceID != localInstanceID || current.Generation != record.Generation {
		return fmt.Errorf("HA lease changed during activation")
	}
	if !c.now().Before(current.ExpiresAt) {
		return fmt.Errorf("HA lease expired at %s", current.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c Controller) EnsureOwnership(ctx context.Context, cfg config.Config, localInstanceID string) (ActivationResult, error) {
	if !cfg.HA.Enabled {
		return ActivationResult{}, nil
	}
	if localInstanceID == "" || localInstanceID == "auto" {
		return ActivationResult{}, fmt.Errorf("local instance id is required for HA ownership")
	}
	if c.Cloud == nil {
		return ActivationResult{}, fmt.Errorf("cloud provider is required for HA ownership")
	}
	unlock := c.lockOwnership()
	defer unlock()

	result := ActivationResult{}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		if cfg.HA.PublicIdentity.AllocationID == "" {
			return ActivationResult{}, fmt.Errorf("ha.public_identity.allocation_id is required for shared_eip")
		}
		identity, err := c.Cloud.DescribePublicIdentity(ctx, cfg.HA.PublicIdentity.AllocationID)
		if err != nil {
			return ActivationResult{}, fmt.Errorf("describe public identity %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
		}
		if identity.InstanceID != localInstanceID {
			identity, err = c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, localInstanceID)
			if err != nil {
				return ActivationResult{}, fmt.Errorf("associate EIP %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
			}
		}
		if identity.InstanceID != localInstanceID {
			return ActivationResult{}, fmt.Errorf("public identity %q is on %q, expected %q", cfg.HA.PublicIdentity.AllocationID, identity.InstanceID, localInstanceID)
		}
		result.PublicIdentity = identity
	} else if cfg.HA.PublicIdentity.Mode != "" {
		return ActivationResult{}, fmt.Errorf("unsupported public identity mode %q", cfg.HA.PublicIdentity.Mode)
	}

	routes, err := routeTargets(cfg, localInstanceID)
	if err != nil {
		return ActivationResult{}, err
	}
	for _, target := range routes {
		actual, err := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if err != nil {
			return ActivationResult{}, fmt.Errorf("describe route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		if actual.Target != target.Target {
			if err := c.Cloud.ReplaceRoute(ctx, target); err != nil {
				return ActivationResult{}, fmt.Errorf("replace route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
			}
			actual, err = c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
			if err != nil {
				return ActivationResult{}, fmt.Errorf("verify route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
			}
		}
		if actual.Target != target.Target {
			return ActivationResult{}, fmt.Errorf("route %s %s target is %q, expected %q", target.RouteTableID, target.DestinationCIDR, actual.Target, target.Target)
		}
		result.Routes = append(result.Routes, actual)
	}
	return result, nil
}

func (c Controller) Handover(ctx context.Context, cfg config.Config, localInstanceID string, targetInstanceID string, record lease.Record) (HandoverResult, error) {
	if !cfg.HA.Enabled {
		return HandoverResult{}, fmt.Errorf("HA is disabled")
	}
	if localInstanceID == "" || localInstanceID == "auto" {
		return HandoverResult{}, fmt.Errorf("local instance id is required for HA handover")
	}
	if targetInstanceID == "" || targetInstanceID == "auto" {
		return HandoverResult{}, fmt.Errorf("target instance id is required for HA handover")
	}
	if targetInstanceID == localInstanceID {
		return HandoverResult{}, fmt.Errorf("handover target must be different from local instance")
	}
	if c.Cloud == nil {
		return HandoverResult{}, fmt.Errorf("cloud provider is required for HA handover")
	}
	if c.Lease == nil {
		return HandoverResult{}, fmt.Errorf("lease manager is required for HA handover")
	}
	transferer, ok := c.Lease.(lease.Transferer)
	if !ok {
		return HandoverResult{}, fmt.Errorf("lease manager does not support transfer")
	}
	if record.OwnerInstanceID == "" {
		current, err := c.Lease.Current(ctx)
		if err != nil {
			return HandoverResult{}, fmt.Errorf("read HA lease before handover: %w", err)
		}
		record = current
	}
	if record.OwnerInstanceID != localInstanceID {
		return HandoverResult{}, fmt.Errorf("local instance is not active lease owner")
	}
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return HandoverResult{}, fmt.Errorf("verify HA lease before handover: %w", err)
	}

	unlock := c.lockOwnership()
	defer unlock()
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return HandoverResult{}, fmt.Errorf("verify HA lease after acquiring ownership lock: %w", err)
	}

	result := HandoverResult{PreviousLease: record}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		if cfg.HA.PublicIdentity.AllocationID == "" {
			return HandoverResult{}, fmt.Errorf("ha.public_identity.allocation_id is required for shared_eip")
		}
		identity, err := c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, targetInstanceID)
		if err != nil {
			return HandoverResult{}, fmt.Errorf("associate EIP %q to handover target %q: %w", cfg.HA.PublicIdentity.AllocationID, targetInstanceID, err)
		}
		result.PublicIdentity = identity
	} else if cfg.HA.PublicIdentity.Mode != "" {
		return HandoverResult{}, fmt.Errorf("unsupported public identity mode %q", cfg.HA.PublicIdentity.Mode)
	}

	routes, err := routeTargets(cfg, targetInstanceID)
	if err != nil {
		return HandoverResult{}, err
	}
	for _, target := range routes {
		if err := c.replaceRouteForHandover(ctx, target, targetInstanceID); err != nil {
			return HandoverResult{}, fmt.Errorf("replace route %s %s to handover target %q: %w", target.RouteTableID, target.DestinationCIDR, targetInstanceID, err)
		}
		result.Routes = append(result.Routes, target)
	}
	if err := c.verifyTargetOwnership(ctx, cfg, targetInstanceID, result.Routes); err != nil {
		revertErr := c.revertHandover(ctx, cfg, localInstanceID)
		result.Reverted = revertErr == nil
		if revertErr != nil {
			return result, fmt.Errorf("%w; revert handover ownership to %q: %v", err, localInstanceID, revertErr)
		}
		return result, err
	}
	newLease, err := transferer.Transfer(ctx, record, targetInstanceID)
	if err != nil {
		revertErr := c.revertHandover(ctx, cfg, localInstanceID)
		result.Reverted = revertErr == nil
		if revertErr != nil {
			return result, fmt.Errorf("transfer HA lease to %q: %w; revert handover ownership to %q: %v", targetInstanceID, err, localInstanceID, revertErr)
		}
		return result, fmt.Errorf("transfer HA lease to %q: %w", targetInstanceID, err)
	}
	result.NewLease = newLease
	return result, nil
}

func (c Controller) replaceRouteForHandover(ctx context.Context, target cloud.RouteTarget, targetInstanceID string) error {
	var lastErr error
	for attempt, backoff := range handoverRouteReplaceBackoffs {
		if backoff > 0 {
			if err := sleepContext(ctx, backoff); err != nil {
				if lastErr != nil {
					return lastErr
				}
				return err
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, handoverRouteReplaceAttemptTimeout)
		err := c.Cloud.ReplaceRoute(attemptCtx, target)
		cancel()
		if err != nil {
			lastErr = err
		}
		actual, verifyErr := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if verifyErr == nil && actual.Target == targetInstanceID {
			return nil
		}
		if err == nil && verifyErr != nil {
			lastErr = fmt.Errorf("verify route after replace attempt %d: %w", attempt+1, verifyErr)
		} else if err == nil {
			lastErr = fmt.Errorf("route target is %q after replace attempt %d, expected %q", actual.Target, attempt+1, targetInstanceID)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("route did not converge to %q", targetInstanceID)
	}
	return lastErr
}

func (c Controller) verifyTargetOwnership(ctx context.Context, cfg config.Config, targetInstanceID string, routes []cloud.RouteTarget) error {
	for _, target := range routes {
		actual, err := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if err != nil {
			return fmt.Errorf("verify handover route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		if actual.Target != targetInstanceID {
			return fmt.Errorf("route %s %s target is %q, expected handover target %q", target.RouteTableID, target.DestinationCIDR, actual.Target, targetInstanceID)
		}
	}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		actual, err := c.Cloud.DescribePublicIdentity(ctx, cfg.HA.PublicIdentity.AllocationID)
		if err != nil {
			return fmt.Errorf("verify handover public identity %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
		}
		if actual.InstanceID != targetInstanceID {
			return fmt.Errorf("public identity %q is on %q, expected handover target %q", cfg.HA.PublicIdentity.AllocationID, actual.InstanceID, targetInstanceID)
		}
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c Controller) revertHandover(ctx context.Context, cfg config.Config, localInstanceID string) error {
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		if _, err := c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, localInstanceID); err != nil {
			return err
		}
	}
	routes, err := routeTargets(cfg, localInstanceID)
	if err != nil {
		return err
	}
	for _, target := range routes {
		if err := c.Cloud.ReplaceRoute(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (c Controller) lockOwnership() func() {
	if c.OwnershipMu == nil {
		return func() {}
	}
	c.OwnershipMu.Lock()
	return c.OwnershipMu.Unlock
}

func routeTargets(cfg config.Config, localInstanceID string) ([]cloud.RouteTarget, error) {
	failover := cfg.HA.RouteFailover
	if failover.Mode == "" {
		return nil, nil
	}
	if failover.Mode != "replace_route" {
		return nil, fmt.Errorf("unsupported route failover mode %q", failover.Mode)
	}
	if failover.TargetType != "instance" {
		return nil, fmt.Errorf("unsupported route target type %q", failover.TargetType)
	}
	if failover.DestinationCIDR == "" {
		return nil, fmt.Errorf("ha.route_failover.destination_cidr is required")
	}
	if len(failover.RouteTableIDs) == 0 {
		return nil, fmt.Errorf("ha.route_failover.route_table_ids is required")
	}

	targets := make([]cloud.RouteTarget, 0, len(failover.RouteTableIDs))
	for _, routeTableID := range failover.RouteTableIDs {
		if routeTableID == "" {
			return nil, fmt.Errorf("ha.route_failover.route_table_ids contains an empty id")
		}
		targets = append(targets, cloud.RouteTarget{
			RouteTableID:    routeTableID,
			DestinationCIDR: failover.DestinationCIDR,
			Target:          localInstanceID,
		})
	}
	return targets, nil
}
