package rollback

import (
	"context"
	"fmt"

	"github.com/betternat/betternat/internal/cloud"
	"github.com/betternat/betternat/internal/config"
)

func SnapshotRoutes(ctx context.Context, provider cloud.Provider, cfg config.Config) (config.RollbackConfig, error) {
	if provider == nil {
		return config.RollbackConfig{}, fmt.Errorf("cloud provider is required")
	}
	failover := cfg.HA.RouteFailover
	if failover.DestinationCIDR == "" {
		return config.RollbackConfig{}, fmt.Errorf("route failover destination cidr is required")
	}
	if len(failover.RouteTableIDs) == 0 {
		return config.RollbackConfig{}, fmt.Errorf("route table ids are required")
	}

	previous := make(map[string]config.PreviousRouteTarget, len(failover.RouteTableIDs))
	for _, routeTableID := range failover.RouteTableIDs {
		if routeTableID == "" {
			return config.RollbackConfig{}, fmt.Errorf("route table id is empty")
		}
		route, err := provider.DescribeRoute(ctx, routeTableID, failover.DestinationCIDR)
		if err != nil {
			return config.RollbackConfig{}, fmt.Errorf("describe previous route %s %s: %w", routeTableID, failover.DestinationCIDR, err)
		}
		previous[routeTableID] = config.PreviousRouteTarget{
			DestinationCIDR: failover.DestinationCIDR,
			Target:          route.Target,
		}
	}
	return config.RollbackConfig{PreviousRouteTargets: previous}, nil
}
