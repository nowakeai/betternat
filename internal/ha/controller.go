package ha

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/datapath"
	"github.com/nowakeai/betternat/internal/lease"
	"github.com/nowakeai/betternat/internal/probe"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

var handoverPublicIdentityBackoffs = []time.Duration{
	0,
	500 * time.Millisecond,
	1500 * time.Millisecond,
	3 * time.Second,
}

var handoverPublicIdentityAttemptTimeout = 30 * time.Second

var leaseFenceReadBackoffs = []time.Duration{
	0,
	150 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
}

var leaseFenceMinRenewDelay = 1 * time.Second
var leaseFenceMaxRenewDelay = 5 * time.Second

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
		if err := c.verifyLeaseFence(ctx, record, localInstanceID, "before shared public identity activation"); err != nil {
			return fail("%w", err)
		}
		identity, err := c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, localInstanceID)
		if err != nil {
			return fail("associate EIP %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
		}
		if err := c.verifyLeaseFence(ctx, record, localInstanceID, "after shared public identity activation"); err != nil {
			return fail("%w", err)
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
		if err := c.verifyLeaseFence(ctx, record, localInstanceID, "before route activation"); err != nil {
			return fail("%w", err)
		}
		if err := c.Cloud.ReplaceRoute(ctx, target); err != nil {
			return fail("replace route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		if err := c.verifyLeaseFence(ctx, record, localInstanceID, "after route activation"); err != nil {
			return fail("%w", err)
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
	current, err := c.currentLeaseWithRetry(ctx)
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

func (c Controller) verifyLeaseFence(ctx context.Context, record lease.Record, localInstanceID string, phase string) error {
	if record.OwnerInstanceID == "" {
		return nil
	}
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return fmt.Errorf("verify HA lease %s: %w", phase, err)
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
	return c.ensureOwnership(ctx, cfg, localInstanceID, lease.Record{})
}

func (c Controller) EnsureOwnershipFenced(ctx context.Context, cfg config.Config, localInstanceID string, record lease.Record) (ActivationResult, error) {
	if record.OwnerInstanceID == "" {
		return ActivationResult{}, fmt.Errorf("lease record is required for fenced ownership repair")
	}
	return c.ensureOwnership(ctx, cfg, localInstanceID, record)
}

func (c Controller) ensureOwnership(ctx context.Context, cfg config.Config, localInstanceID string, record lease.Record) (ActivationResult, error) {
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
			if err := c.verifyLeaseFence(ctx, record, localInstanceID, "before public identity repair"); err != nil {
				return ActivationResult{}, err
			}
			identity, err = c.Cloud.AssociateEIP(ctx, cfg.HA.PublicIdentity.AllocationID, localInstanceID)
			if err != nil {
				return ActivationResult{}, fmt.Errorf("associate EIP %q: %w", cfg.HA.PublicIdentity.AllocationID, err)
			}
			if err := c.verifyLeaseFence(ctx, record, localInstanceID, "after public identity repair"); err != nil {
				return ActivationResult{}, err
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
		if err != nil || actual.Target != target.Target {
			if err := c.verifyLeaseFence(ctx, record, localInstanceID, "before route repair"); err != nil {
				return ActivationResult{}, err
			}
			if err := c.Cloud.ReplaceRoute(ctx, target); err != nil {
				return ActivationResult{}, fmt.Errorf("replace route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
			}
			if err := c.verifyLeaseFence(ctx, record, localInstanceID, "after route repair"); err != nil {
				return ActivationResult{}, err
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
	var err error
	record, err = c.renewLeaseFence(ctx, record, localInstanceID, "after acquiring ownership lock")
	if err != nil {
		return HandoverResult{}, fmt.Errorf("verify HA lease after acquiring ownership lock: %w", err)
	}

	result := HandoverResult{PreviousLease: record}
	if cfg.HA.PublicIdentity.Mode == "shared_eip" {
		if cfg.HA.PublicIdentity.AllocationID == "" {
			return HandoverResult{}, fmt.Errorf("ha.public_identity.allocation_id is required for shared_eip")
		}
		var identity cloud.PublicIdentity
		record, identity, err = c.associatePublicIdentityForHandover(ctx, cfg, targetInstanceID, localInstanceID, record)
		if err != nil {
			associateErr := fmt.Errorf("associate EIP %q to handover target %q: %w", cfg.HA.PublicIdentity.AllocationID, targetInstanceID, err)
			if fenceErr := c.verifyLeaseFence(ctx, record, localInstanceID, "before failed public identity handover revert"); fenceErr != nil {
				return result, associateErr
			}
			revertErr := c.revertHandover(ctx, cfg, localInstanceID)
			result.Reverted = revertErr == nil
			if revertErr != nil {
				return result, fmt.Errorf("%w; revert handover ownership to %q: %v", associateErr, localInstanceID, revertErr)
			}
			return result, associateErr
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
		record, err = c.replaceRouteForHandover(ctx, target, targetInstanceID, localInstanceID, record)
		if err != nil {
			replaceErr := fmt.Errorf("replace route %s %s to handover target %q: %w", target.RouteTableID, target.DestinationCIDR, targetInstanceID, err)
			revertErr := c.revertHandover(ctx, cfg, localInstanceID)
			result.Reverted = revertErr == nil
			if revertErr != nil {
				return result, fmt.Errorf("%w; revert handover ownership to %q: %v", replaceErr, localInstanceID, revertErr)
			}
			return result, replaceErr
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
	record, err = c.renewLeaseFence(ctx, record, localInstanceID, "before handover lease transfer")
	if err != nil {
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

func (c Controller) associatePublicIdentityForHandover(ctx context.Context, cfg config.Config, targetInstanceID string, localInstanceID string, record lease.Record) (lease.Record, cloud.PublicIdentity, error) {
	var lastErr error
	allocationID := cfg.HA.PublicIdentity.AllocationID
	for attempt, backoff := range handoverPublicIdentityBackoffs {
		if backoff > 0 {
			if err := sleepContext(ctx, backoff); err != nil {
				if lastErr != nil {
					return record, cloud.PublicIdentity{}, lastErr
				}
				return record, cloud.PublicIdentity{}, err
			}
		}
		renewed, err := c.renewLeaseFence(ctx, record, localInstanceID, "before handover public identity mutation")
		if err != nil {
			return record, cloud.PublicIdentity{}, err
		}
		record = renewed
		attemptCtx, cancel := context.WithTimeout(ctx, handoverPublicIdentityAttemptTimeout)
		var identity cloud.PublicIdentity
		record, err = c.maintainLeaseDuring(attemptCtx, record, "handover public identity mutation", func(runCtx context.Context) error {
			var associateErr error
			identity, associateErr = c.Cloud.AssociateEIP(runCtx, allocationID, targetInstanceID)
			return associateErr
		})
		cancel()
		if err != nil {
			lastErr = err
			actual, verifyErr := c.Cloud.DescribePublicIdentity(ctx, allocationID)
			if verifyErr == nil && actual.InstanceID == targetInstanceID {
				return record, actual, nil
			}
			if verifyErr != nil {
				lastErr = fmt.Errorf("associate public identity attempt %d: %w; verify public identity: %v", attempt+1, err, verifyErr)
			}
			continue
		}
		if fenceErr := c.verifyLeaseFence(ctx, record, localInstanceID, "after handover public identity mutation"); fenceErr != nil {
			return record, cloud.PublicIdentity{}, fenceErr
		}
		return record, identity, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("public identity did not converge to %q", targetInstanceID)
	}
	return record, cloud.PublicIdentity{}, lastErr
}

type leaseRenewalResult struct {
	record lease.Record
	err    error
}

func (c Controller) maintainLeaseDuring(ctx context.Context, record lease.Record, phase string, run func(context.Context) error) (lease.Record, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	renewals := make(chan leaseRenewalResult, 64)
	go c.renewLeaseUntilDone(ctx, record, phase, done, renewals, cancel)
	err := run(runCtx)
	close(done)
	latest := record
	var renewErr error
	for {
		select {
		case result := <-renewals:
			if result.err != nil {
				renewErr = result.err
				continue
			}
			latest = result.record
		default:
			if renewErr != nil {
				return latest, renewErr
			}
			return latest, err
		}
	}
}

func (c Controller) renewLeaseUntilDone(ctx context.Context, record lease.Record, phase string, done <-chan struct{}, results chan<- leaseRenewalResult, cancelRun context.CancelFunc) {
	current := record
	timer := time.NewTimer(nextLeaseRenewDelay(current, c.now()))
	defer timer.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			renewed, err := c.renewLeaseWithRetry(ctx, current)
			if err != nil {
				results <- leaseRenewalResult{err: fmt.Errorf("renew HA lease during %s: %w", phase, err)}
				cancelRun()
				return
			}
			current = renewed
			results <- leaseRenewalResult{record: renewed}
			timer.Reset(nextLeaseRenewDelay(current, c.now()))
		}
	}
}

func nextLeaseRenewDelay(record lease.Record, now time.Time) time.Duration {
	delay := time.Until(record.ExpiresAt) / 3
	if !now.IsZero() {
		delay = record.ExpiresAt.Sub(now) / 3
	}
	if delay < leaseFenceMinRenewDelay {
		return leaseFenceMinRenewDelay
	}
	if delay > leaseFenceMaxRenewDelay {
		return leaseFenceMaxRenewDelay
	}
	return delay
}

func (c Controller) renewLeaseFence(ctx context.Context, record lease.Record, localInstanceID string, phase string) (lease.Record, error) {
	if err := c.VerifyLease(ctx, record, localInstanceID); err != nil {
		return lease.Record{}, fmt.Errorf("verify HA lease %s: %w", phase, err)
	}
	renewed, err := c.renewLeaseWithRetry(ctx, record)
	if err != nil {
		return lease.Record{}, fmt.Errorf("renew HA lease %s: %w", phase, err)
	}
	if err := c.VerifyLease(ctx, renewed, localInstanceID); err != nil {
		return lease.Record{}, fmt.Errorf("verify renewed HA lease %s: %w", phase, err)
	}
	return renewed, nil
}

func (c Controller) currentLeaseWithRetry(ctx context.Context) (lease.Record, error) {
	var lastErr error
	for attempt, backoff := range leaseFenceReadBackoffs {
		if backoff > 0 {
			if err := sleepContext(ctx, backoff); err != nil {
				if lastErr != nil {
					return lease.Record{}, lastErr
				}
				return lease.Record{}, err
			}
		}
		current, err := c.Lease.Current(ctx)
		if err == nil {
			return current, nil
		}
		lastErr = err
		if attempt == len(leaseFenceReadBackoffs)-1 || !retryableLeaseControlError(ctx, err) {
			return lease.Record{}, err
		}
	}
	return lease.Record{}, lastErr
}

func (c Controller) renewLeaseWithRetry(ctx context.Context, record lease.Record) (lease.Record, error) {
	var lastErr error
	for attempt, backoff := range leaseFenceReadBackoffs {
		if backoff > 0 {
			if err := sleepContext(ctx, backoff); err != nil {
				if lastErr != nil {
					return lease.Record{}, lastErr
				}
				return lease.Record{}, err
			}
		}
		renewed, err := c.Lease.Renew(ctx, record)
		if err == nil {
			return renewed, nil
		}
		lastErr = err
		if attempt == len(leaseFenceReadBackoffs)-1 || !retryableLeaseControlError(ctx, err) {
			return lease.Record{}, err
		}
	}
	return lease.Record{}, lastErr
}

func retryableLeaseControlError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted:
		return true
	}
	text := strings.ToLower(err.Error())
	for _, token := range []string{
		"connection reset",
		"connection refused",
		"connection closed",
		"eof",
		"server closed",
		"temporary",
		"timeout awaiting response headers",
		"http2: client connection lost",
	} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func (c Controller) replaceRouteForHandover(ctx context.Context, target cloud.RouteTarget, targetInstanceID string, localInstanceID string, record lease.Record) (lease.Record, error) {
	var lastErr error
	for attempt, backoff := range handoverRouteReplaceBackoffs {
		if backoff > 0 {
			if err := sleepContext(ctx, backoff); err != nil {
				if lastErr != nil {
					return lease.Record{}, lastErr
				}
				return lease.Record{}, err
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, handoverRouteReplaceAttemptTimeout)
		renewed, err := c.renewLeaseFence(ctx, record, localInstanceID, "before handover route mutation")
		if err != nil {
			cancel()
			return lease.Record{}, err
		}
		record = renewed
		err = c.Cloud.ReplaceRoute(attemptCtx, target)
		cancel()
		if fenceErr := c.verifyLeaseFence(ctx, record, localInstanceID, "after handover route mutation"); fenceErr != nil {
			return lease.Record{}, fenceErr
		}
		if err != nil {
			lastErr = err
		}
		actual, verifyErr := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if verifyErr == nil && actual.Target == targetInstanceID {
			return record, nil
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
	return lease.Record{}, lastErr
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
