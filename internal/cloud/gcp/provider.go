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
	Region            string
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

type ZoneOperationAPI interface {
	Get(ctx context.Context, projectID string, zone string, name string) (*compute.Operation, error)
}

type InstanceAPI interface {
	Get(ctx context.Context, projectID string, zone string, instanceName string) (*compute.Instance, error)
	DeleteAccessConfig(ctx context.Context, projectID string, zone string, instanceName string, accessConfigName string, networkInterface string) (*compute.Operation, error)
	AddAccessConfig(ctx context.Context, projectID string, zone string, instanceName string, networkInterface string, accessConfig *compute.AccessConfig) (*compute.Operation, error)
}

type AddressAPI interface {
	Get(ctx context.Context, projectID string, region string, addressName string) (*compute.Address, error)
}

type Provider struct {
	cfg              Config
	routes           RouteAPI
	globalOperations GlobalOperationAPI
	zoneOperations   ZoneOperationAPI
	instances        InstanceAPI
	addresses        AddressAPI
}

var _ cloud.Provider = (*Provider)(nil)

func New(ctx context.Context, cfg Config) (*Provider, error) {
	service, err := compute.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create gcp compute service: %w", err)
	}
	return NewFromAPIs(cfg, computeRoutes{service: service}, computeGlobalOperations{service: service}, computeZoneOperations{service: service}, computeInstances{service: service}, computeAddresses{service: service}), nil
}

func NewFromAPI(cfg Config, routes RouteAPI, operations GlobalOperationAPI) *Provider {
	return NewFromAPIs(cfg, routes, operations, nil, nil, nil)
}

func NewFromAPIs(cfg Config, routes RouteAPI, globalOperations GlobalOperationAPI, zoneOperations ZoneOperationAPI, instances InstanceAPI, addresses AddressAPI) *Provider {
	return &Provider{
		cfg:              cfg,
		routes:           routes,
		globalOperations: globalOperations,
		zoneOperations:   zoneOperations,
		instances:        instances,
		addresses:        addresses,
	}
}

func (p *Provider) ReplaceRoute(ctx context.Context, target cloud.RouteTarget) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := validateRouteTarget(target); err != nil {
		return err
	}
	previous, err := p.routes.Get(ctx, p.cfg.ProjectID, target.RouteTableID)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("gcp compute get route %q before replace: %w", target.RouteTableID, err)
	}
	if err != nil {
		previous = nil
	}
	deleteOp, err := p.routes.Delete(ctx, p.cfg.ProjectID, target.RouteTableID)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("gcp compute delete route %q: %w", target.RouteTableID, err)
	}
	if err == nil {
		if err := p.waitGlobalOperation(ctx, operationName(deleteOp)); err != nil {
			return p.restorePreviousRoute(ctx, target.RouteTableID, previous, fmt.Errorf("wait for gcp route %q delete: %w", target.RouteTableID, err))
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
		return p.restorePreviousRoute(ctx, target.RouteTableID, previous, fmt.Errorf("gcp compute insert route %q: %w", target.RouteTableID, err))
	}
	if err := p.waitGlobalOperation(ctx, operationName(insertOp)); err != nil {
		return p.restorePreviousRoute(ctx, target.RouteTableID, previous, fmt.Errorf("wait for gcp route %q insert: %w", target.RouteTableID, err))
	}
	return nil
}

func (p *Provider) restorePreviousRoute(ctx context.Context, routeName string, previous *compute.Route, cause error) error {
	if previous == nil || ctx.Err() != nil {
		return cause
	}
	insertOp, err := p.routes.Insert(ctx, p.cfg.ProjectID, restorableRoute(previous))
	if err != nil {
		return fmt.Errorf("%w; failed to restore previous gcp route %q: %v", cause, routeName, err)
	}
	if err := p.waitGlobalOperation(ctx, operationName(insertOp)); err != nil {
		return fmt.Errorf("%w; failed to wait for previous gcp route %q restore: %v", cause, routeName, err)
	}
	return fmt.Errorf("%w; previous gcp route %q restored", cause, routeName)
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

func (p *Provider) AssociateEIP(ctx context.Context, allocationID string, instanceID string) (cloud.PublicIdentity, error) {
	if err := p.validatePublicIdentity(); err != nil {
		return cloud.PublicIdentity{}, err
	}
	if strings.TrimSpace(allocationID) == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp static address name is required")
	}
	if strings.TrimSpace(instanceID) == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("target instance is required")
	}
	address, err := p.addresses.Get(ctx, p.cfg.ProjectID, p.cfg.Region, allocationID)
	if err != nil {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp compute get address %q: %w", allocationID, err)
	}
	if strings.TrimSpace(address.Address) == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp address %q has no external IP", allocationID)
	}
	holderInstance, holderZone := addressHolder(address.Users)
	if holderInstance != "" && holderInstance != instanceID {
		if err := p.deleteExternalAccessConfig(ctx, holderZone, holderInstance); err != nil {
			return cloud.PublicIdentity{}, fmt.Errorf("detach gcp address %q from %q: %w", allocationID, holderInstance, err)
		}
	}
	instance, err := p.instances.Get(ctx, p.cfg.ProjectID, p.cfg.Zone, instanceID)
	if err != nil {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp compute get instance %q: %w", instanceID, err)
	}
	if err := p.removeConflictingAccessConfigs(ctx, instance, address.Address); err != nil {
		return cloud.PublicIdentity{}, err
	}
	if !instanceHasPublicIP(instance, address.Address) {
		op, err := p.instances.AddAccessConfig(ctx, p.cfg.ProjectID, p.cfg.Zone, instanceID, primaryNICName(instance), &compute.AccessConfig{
			Name:  defaultAccessConfigName(instance),
			Type:  "ONE_TO_ONE_NAT",
			NatIP: address.Address,
		})
		if err != nil {
			return cloud.PublicIdentity{}, fmt.Errorf("gcp compute add access config to %q: %w", instanceID, err)
		}
		if err := p.waitZoneOperation(ctx, p.cfg.Zone, operationName(op)); err != nil {
			return cloud.PublicIdentity{}, fmt.Errorf("wait for gcp access config attach to %q: %w", instanceID, err)
		}
	}
	return cloud.PublicIdentity{AllocationID: allocationID, PublicIP: address.Address, InstanceID: instanceID}, nil
}

func (p *Provider) DescribePublicIdentity(ctx context.Context, allocationID string) (cloud.PublicIdentity, error) {
	if err := p.validatePublicIdentity(); err != nil {
		return cloud.PublicIdentity{}, err
	}
	if strings.TrimSpace(allocationID) == "" {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp static address name is required")
	}
	address, err := p.addresses.Get(ctx, p.cfg.ProjectID, p.cfg.Region, allocationID)
	if err != nil {
		return cloud.PublicIdentity{}, fmt.Errorf("gcp compute get address %q: %w", allocationID, err)
	}
	instanceID, _ := addressHolder(address.Users)
	return cloud.PublicIdentity{AllocationID: allocationID, PublicIP: address.Address, InstanceID: instanceID}, nil
}

func (p *Provider) validate() error {
	if p.routes == nil {
		return fmt.Errorf("gcp routes api is required")
	}
	if p.globalOperations == nil {
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

func (p *Provider) validatePublicIdentity() error {
	if p.instances == nil {
		return fmt.Errorf("gcp instances api is required for stable public identity")
	}
	if p.addresses == nil {
		return fmt.Errorf("gcp addresses api is required for stable public identity")
	}
	if p.zoneOperations == nil {
		return fmt.Errorf("gcp zone operations api is required for stable public identity")
	}
	missing := []string{}
	for name, value := range map[string]string{
		"project_id": p.cfg.ProjectID,
		"region":     p.cfg.Region,
		"zone":       p.cfg.Zone,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required GCP public identity config: %s", strings.Join(missing, ", "))
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
		op, err := p.globalOperations.Get(ctx, p.cfg.ProjectID, name)
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

func (p *Provider) waitZoneOperation(ctx context.Context, zone string, name string) error {
	if name == "" {
		return nil
	}
	poll := p.cfg.OperationPollTime
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		op, err := p.zoneOperations.Get(ctx, p.cfg.ProjectID, zone, name)
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

func (p *Provider) deleteExternalAccessConfig(ctx context.Context, zone string, instanceID string) error {
	if zone == "" {
		zone = p.cfg.Zone
	}
	instance, err := p.instances.Get(ctx, p.cfg.ProjectID, zone, instanceID)
	if err != nil {
		return err
	}
	for _, nic := range instance.NetworkInterfaces {
		nicName := valueOr(nic.Name, "nic0")
		for _, accessConfig := range nic.AccessConfigs {
			op, err := p.instances.DeleteAccessConfig(ctx, p.cfg.ProjectID, zone, instanceID, valueOr(accessConfig.Name, "External NAT"), nicName)
			if err != nil {
				return err
			}
			if err := p.waitZoneOperation(ctx, zone, operationName(op)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provider) removeConflictingAccessConfigs(ctx context.Context, instance *compute.Instance, publicIP string) error {
	for _, nic := range instance.NetworkInterfaces {
		nicName := valueOr(nic.Name, "nic0")
		for _, accessConfig := range nic.AccessConfigs {
			if accessConfig.NatIP == publicIP {
				continue
			}
			op, err := p.instances.DeleteAccessConfig(ctx, p.cfg.ProjectID, p.cfg.Zone, instance.Name, valueOr(accessConfig.Name, "External NAT"), nicName)
			if err != nil {
				return fmt.Errorf("gcp compute delete conflicting access config from %q: %w", instance.Name, err)
			}
			if err := p.waitZoneOperation(ctx, p.cfg.Zone, operationName(op)); err != nil {
				return fmt.Errorf("wait for gcp conflicting access config delete from %q: %w", instance.Name, err)
			}
		}
	}
	return nil
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

func restorableRoute(route *compute.Route) *compute.Route {
	if route == nil {
		return nil
	}
	return &compute.Route{
		Name:             route.Name,
		Description:      route.Description,
		Network:          route.Network,
		DestRange:        route.DestRange,
		Priority:         route.Priority,
		Tags:             append([]string(nil), route.Tags...),
		NextHopGateway:   route.NextHopGateway,
		NextHopInstance:  route.NextHopInstance,
		NextHopIp:        route.NextHopIp,
		NextHopNetwork:   route.NextHopNetwork,
		NextHopPeering:   route.NextHopPeering,
		NextHopVpnTunnel: route.NextHopVpnTunnel,
	}
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

func addressHolder(users []string) (string, string) {
	for _, user := range users {
		instance, zone := instanceAndZoneFromLink(user)
		if instance != "" {
			return instance, zone
		}
	}
	return "", ""
}

func instanceAndZoneFromLink(link string) (string, string) {
	parts := strings.Split(strings.TrimRight(link, "/"), "/")
	instance := ""
	zone := ""
	for i, part := range parts {
		if part == "instances" && i+1 < len(parts) {
			instance = parts[i+1]
		}
		if part == "zones" && i+1 < len(parts) {
			zone = parts[i+1]
		}
	}
	return instance, zone
}

func instanceHasPublicIP(instance *compute.Instance, publicIP string) bool {
	for _, nic := range instance.NetworkInterfaces {
		for _, accessConfig := range nic.AccessConfigs {
			if accessConfig.NatIP == publicIP {
				return true
			}
		}
	}
	return false
}

func primaryNICName(instance *compute.Instance) string {
	if len(instance.NetworkInterfaces) == 0 {
		return "nic0"
	}
	return valueOr(instance.NetworkInterfaces[0].Name, "nic0")
}

func defaultAccessConfigName(instance *compute.Instance) string {
	if len(instance.NetworkInterfaces) == 0 || len(instance.NetworkInterfaces[0].AccessConfigs) == 0 {
		return "External NAT"
	}
	return valueOr(instance.NetworkInterfaces[0].AccessConfigs[0].Name, "External NAT")
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

type computeZoneOperations struct {
	service *compute.Service
}

func (o computeZoneOperations) Get(ctx context.Context, projectID string, zone string, name string) (*compute.Operation, error) {
	return o.service.ZoneOperations.Get(projectID, zone, name).Context(ctx).Do()
}

type computeInstances struct {
	service *compute.Service
}

func (i computeInstances) Get(ctx context.Context, projectID string, zone string, instanceName string) (*compute.Instance, error) {
	return i.service.Instances.Get(projectID, zone, instanceName).Context(ctx).Do()
}

func (i computeInstances) DeleteAccessConfig(ctx context.Context, projectID string, zone string, instanceName string, accessConfigName string, networkInterface string) (*compute.Operation, error) {
	return i.service.Instances.DeleteAccessConfig(projectID, zone, instanceName, accessConfigName, networkInterface).Context(ctx).Do()
}

func (i computeInstances) AddAccessConfig(ctx context.Context, projectID string, zone string, instanceName string, networkInterface string, accessConfig *compute.AccessConfig) (*compute.Operation, error) {
	return i.service.Instances.AddAccessConfig(projectID, zone, instanceName, networkInterface, accessConfig).Context(ctx).Do()
}

type computeAddresses struct {
	service *compute.Service
}

func (a computeAddresses) Get(ctx context.Context, projectID string, region string, addressName string) (*compute.Address, error) {
	return a.service.Addresses.Get(projectID, region, addressName).Context(ctx).Do()
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
