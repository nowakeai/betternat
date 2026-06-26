package ha

import (
	"context"
	"fmt"

	"github.com/nowakeai/betternat/internal/cloud"
	"github.com/nowakeai/betternat/internal/config"
	"github.com/nowakeai/betternat/internal/lease"
)

func connectivityFirstHandover(cfg config.Config) bool {
	return cfg.Cloud == "gcp" && cfg.HA.PublicIdentity.Mode == "shared_eip"
}

func (c Controller) handoverConnectivityFirst(ctx context.Context, cfg config.Config, localInstanceID string, targetInstanceID string, record lease.Record, transferer lease.Transferer, result HandoverResult) (HandoverResult, error) {
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
	if err := c.verifyRouteOwnership(ctx, targetInstanceID, result.Routes); err != nil {
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

func (c Controller) verifyRouteOwnership(ctx context.Context, targetInstanceID string, routes []cloud.RouteTarget) error {
	for _, target := range routes {
		actual, err := c.Cloud.DescribeRoute(ctx, target.RouteTableID, target.DestinationCIDR)
		if err != nil {
			return fmt.Errorf("verify handover route %s %s: %w", target.RouteTableID, target.DestinationCIDR, err)
		}
		if actual.Target != targetInstanceID {
			return fmt.Errorf("route %s %s target is %q, expected handover target %q", target.RouteTableID, target.DestinationCIDR, actual.Target, targetInstanceID)
		}
	}
	return nil
}
