package rollback

import (
	"context"
	"errors"
	"testing"

	"github.com/betternat/betternat/internal/cloud"
	"github.com/betternat/betternat/internal/config"
)

func TestSnapshotRoutes(t *testing.T) {
	provider := fakeCloud{
		routes: map[string]cloud.RouteTarget{
			"rtb-a:0.0.0.0/0": {RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "nat-aaa"},
			"rtb-b:0.0.0.0/0": {RouteTableID: "rtb-b", DestinationCIDR: "0.0.0.0/0", Target: "eni-bbb"},
		},
	}
	rollback, err := SnapshotRoutes(context.Background(), provider, config.Config{
		HA: config.HAConfig{
			RouteFailover: config.RouteFailoverConfig{
				RouteTableIDs:   []string{"rtb-a", "rtb-b"},
				DestinationCIDR: "0.0.0.0/0",
			},
		},
	})
	if err != nil {
		t.Fatalf("snapshot routes: %v", err)
	}
	if rollback.PreviousRouteTargets["rtb-a"].Target != "nat-aaa" {
		t.Fatalf("unexpected rollback target: %#v", rollback)
	}
	if rollback.PreviousRouteTargets["rtb-b"].Target != "eni-bbb" {
		t.Fatalf("unexpected rollback target: %#v", rollback)
	}
}

func TestSnapshotRoutesRequiresProvider(t *testing.T) {
	_, err := SnapshotRoutes(context.Background(), nil, config.Config{})
	if err == nil {
		t.Fatal("expected provider error")
	}
}

type fakeCloud struct {
	routes map[string]cloud.RouteTarget
}

func (f fakeCloud) ReplaceRoute(context.Context, cloud.RouteTarget) error {
	return errors.New("not implemented")
}

func (f fakeCloud) AssociateEIP(context.Context, string, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, errors.New("not implemented")
}

func (f fakeCloud) DescribeRoute(_ context.Context, routeTableID string, destinationCIDR string) (cloud.RouteTarget, error) {
	route, ok := f.routes[routeTableID+":"+destinationCIDR]
	if !ok {
		return cloud.RouteTarget{}, errors.New("not found")
	}
	return route, nil
}

func (f fakeCloud) DescribePublicIdentity(context.Context, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, errors.New("not implemented")
}
