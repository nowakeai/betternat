package gcpcloud

import (
	"context"
	"fmt"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"

	"github.com/nowakeai/betternat/internal/cloud"
)

type Config struct {
	ProjectID         string
	Zone              string
	Network           string
	ClientTag         string
	RoutePriority     int64
	OperationPollTime time.Duration
}

type RouteAPI interface {
	Get(ctx context.Context, projectID string, routeName string) (*compute.Route, error)
	Insert(ctx context.Context, projectID string, route *compute.Route) (*compute.Operation, error)
	Delete(ctx context.Context, projectID string, routeName string) (*compute.Operation, error)
}

type GlobalOperationAPI interface {
	Get(ctx context.Context, projectID string, name string) (*compute.Operation, error)
}

type Provider struct {
	cfg        Config
	routes     RouteAPI
	operations GlobalOperationAPI
}

var _ cloud.Provider = (*Provider)(nil)

func New(ctx context.Context, cfg Config) (*Provider, error) {
	service, err := compute.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create gcp compute service: %w", err)
	}
	return NewFromAPI(cfg, computeRoutes{service: service}, computeGlobalOperations{service: service}), nil
}

func NewFromAPI(cfg Config, routes RouteAPI, operations GlobalOperationAPI) *Provider {
	return &Provider{cfg: cfg, routes: routes, operations: operations}
}

func (p *Provider) ReplaceRoute(ctx context.Context, target cloud.RouteTarget) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := validateRouteTarget(target); err != nil {
		return err
	}
	deleteOp, err := p.routes.Delete(ctx, p.cfg.ProjectID, target.RouteTableID)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("gcp compute delete route %q: %w", target.RouteTableID, err)
	}
	if err == nil {
		if err := p.waitGlobalOperation(ctx, operationName(deleteOp)); err != nil {
			return fmt.Errorf("wait for gcp route %q delete: %w", target.RouteTableID, err)
		}
	}
	route := &compute.Route{
		Name:            target.RouteTableID,
		Network:         globalLink(p.cfg.ProjectID, "networks", p.cfg.Network),
		DestRange:       target.DestinationCIDR,
		Priority:        routePriority(p.cfg.RoutePriority),
		Tags:            []string{p.cfg.ClientTag},
		NextHopInstance: zoneLink(p.cfg.ProjectID, p.cfg.Zone, "instances", target.Target),
	}
	insertOp, err := p.routes.Insert(ctx, p.cfg.ProjectID, route)
	if err != nil {
		return fmt.Errorf("gcp compute insert route %q: %w", target.RouteTableID, err)
	}
	if err := p.waitGlobalOperation(ctx, operationName(insertOp)); err != nil {
		return fmt.Errorf("wait for gcp route %q insert: %w", target.RouteTableID, err)
	}
	return nil
}

func (p *Provider) DescribeRoute(ctx context.Context, routeName string, destinationCIDR string) (cloud.RouteTarget, error) {
	if err := p.validate(); err != nil {
		return cloud.RouteTarget{}, err
	}
	if routeName == "" {
		return cloud.RouteTarget{}, fmt.Errorf("route name is required")
	}
	route, err := p.routes.Get(ctx, p.cfg.ProjectID, routeName)
	if err != nil {
		return cloud.RouteTarget{}, fmt.Errorf("gcp compute get route %q: %w", routeName, err)
	}
	if destinationCIDR != "" && route.DestRange != destinationCIDR {
		return cloud.RouteTarget{}, fmt.Errorf("route %q destination is %q, expected %q", routeName, route.DestRange, destinationCIDR)
	}
	return cloud.RouteTarget{
		RouteTableID:    routeName,
		DestinationCIDR: route.DestRange,
		Target:          baseName(route.NextHopInstance),
	}, nil
}

func (p *Provider) AssociateEIP(context.Context, string, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, fmt.Errorf("gcp route-only HA does not support shared public identity")
}

func (p *Provider) DescribePublicIdentity(context.Context, string) (cloud.PublicIdentity, error) {
	return cloud.PublicIdentity{}, fmt.Errorf("gcp route-only HA does not support shared public identity")
}

func (p *Provider) validate() error {
	if p.routes == nil {
		return fmt.Errorf("gcp routes api is required")
	}
	if p.operations == nil {
		return fmt.Errorf("gcp global operations api is required")
	}
	missing := []string{}
	for name, value := range map[string]string{
		"project_id": p.cfg.ProjectID,
		"zone":       p.cfg.Zone,
		"network":    p.cfg.Network,
		"client_tag": p.cfg.ClientTag,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required GCP cloud config: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateRouteTarget(target cloud.RouteTarget) error {
	if target.RouteTableID == "" {
		return fmt.Errorf("route name is required")
	}
	if target.DestinationCIDR == "" {
		return fmt.Errorf("destination cidr is required")
	}
	if target.Target == "" {
		return fmt.Errorf("route target is required")
	}
	return nil
}

func (p *Provider) waitGlobalOperation(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	poll := p.cfg.OperationPollTime
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		op, err := p.operations.Get(ctx, p.cfg.ProjectID, name)
		if err != nil {
			return err
		}
		if op.Status == "DONE" {
			return operationError(op)
		}
		if err := sleepContext(ctx, poll); err != nil {
			return err
		}
	}
}

func operationError(op *compute.Operation) error {
	if op.Error == nil || len(op.Error.Errors) == 0 {
		return nil
	}
	parts := make([]string, 0, len(op.Error.Errors))
	for _, item := range op.Error.Errors {
		parts = append(parts, item.Message)
	}
	return fmt.Errorf("gcp operation %s failed: %s", op.Name, strings.Join(parts, "; "))
}

func operationName(op *compute.Operation) string {
	if op == nil {
		return ""
	}
	return op.Name
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "googleapi: Error 404")
}

func routePriority(value int64) int64 {
	if value == 0 {
		return 800
	}
	return value
}

func globalLink(projectID, collection, name string) string {
	if strings.HasPrefix(name, "http") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return fmt.Sprintf("projects/%s/global/%s/%s", projectID, collection, name)
}

func zoneLink(projectID, zone, collection, name string) string {
	if strings.HasPrefix(name, "http") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return fmt.Sprintf("projects/%s/zones/%s/%s/%s", projectID, zone, collection, name)
}

func baseName(link string) string {
	parts := strings.Split(strings.TrimRight(link, "/"), "/")
	return parts[len(parts)-1]
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type computeRoutes struct {
	service *compute.Service
}

func (r computeRoutes) Get(ctx context.Context, projectID string, routeName string) (*compute.Route, error) {
	return r.service.Routes.Get(projectID, routeName).Context(ctx).Do()
}

func (r computeRoutes) Insert(ctx context.Context, projectID string, route *compute.Route) (*compute.Operation, error) {
	return r.service.Routes.Insert(projectID, route).Context(ctx).Do()
}

func (r computeRoutes) Delete(ctx context.Context, projectID string, routeName string) (*compute.Operation, error) {
	return r.service.Routes.Delete(projectID, routeName).Context(ctx).Do()
}

type computeGlobalOperations struct {
	service *compute.Service
}

func (o computeGlobalOperations) Get(ctx context.Context, projectID string, name string) (*compute.Operation, error) {
	return o.service.GlobalOperations.Get(projectID, name).Context(ctx).Do()
}
