package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
)

type Inputs struct {
	Name                string
	ProjectID           string
	Region              string
	Zone                string
	Network             string
	Subnetwork          string
	ClientTag           string
	RouteName           string
	RoutePriority       int64
	RouteDestRange      string
	MachineType         string
	ImageProject        string
	ImageFamily         string
	GatewayCount        int64
	PrivateCIDRs        []string
	Labels              map[string]string
	ServiceAccountEmail string
	StartupScript       string
	DeletionTimeout     time.Duration
	OperationPollTime   time.Duration
}

type Result struct {
	GatewayInstances map[string]string
	EgressPublicIPs  map[string]string
	RouteTarget      string
}

type ReadResult struct {
	GatewayInstances map[string]string
	EgressPublicIPs  map[string]string
	RouteTarget      string
	Status           string
}

type Applier struct {
	Compute *compute.Service
}

func (a Applier) Apply(ctx context.Context, inputs Inputs) (Result, error) {
	if err := inputs.validate(); err != nil {
		return Result{}, err
	}
	for _, name := range gatewayNames(inputs.Name, inputs.GatewayCount) {
		if err := a.createInstance(ctx, inputs, name); err != nil {
			return Result{}, err
		}
	}
	if err := a.createRoute(ctx, inputs, gatewayNames(inputs.Name, inputs.GatewayCount)[0]); err != nil {
		return Result{}, err
	}
	read, err := a.Read(ctx, inputs)
	if err != nil {
		return Result{}, err
	}
	return Result{GatewayInstances: read.GatewayInstances, EgressPublicIPs: read.EgressPublicIPs, RouteTarget: read.RouteTarget}, nil
}

func (a Applier) Cleanup(ctx context.Context, inputs Inputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	var firstErr error
	if err := a.deleteRoute(ctx, inputs); err != nil && !isNotFound(err) {
		firstErr = err
	}
	for _, name := range gatewayNames(inputs.Name, inputs.GatewayCount) {
		if err := a.deleteInstance(ctx, inputs, name); err != nil && !isNotFound(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a Applier) Read(ctx context.Context, inputs Inputs) (ReadResult, error) {
	if err := inputs.validate(); err != nil {
		return ReadResult{}, err
	}
	instances := map[string]string{}
	ips := map[string]string{}
	for _, name := range gatewayNames(inputs.Name, inputs.GatewayCount) {
		inst, err := a.Compute.Instances.Get(inputs.ProjectID, inputs.Zone, name).Context(ctx).Do()
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return ReadResult{}, err
		}
		instances[name] = inst.Status
		if len(inst.NetworkInterfaces) > 0 && len(inst.NetworkInterfaces[0].AccessConfigs) > 0 {
			ips[name] = inst.NetworkInterfaces[0].AccessConfigs[0].NatIP
		}
	}
	routeTarget := ""
	route, err := a.Compute.Routes.Get(inputs.ProjectID, inputs.RouteName).Context(ctx).Do()
	if err == nil {
		routeTarget = baseName(route.NextHopInstance)
	} else if !isNotFound(err) {
		return ReadResult{}, err
	}
	status := "missing"
	if len(instances) > 0 && routeTarget != "" {
		status = "active"
	}
	return ReadResult{GatewayInstances: instances, EgressPublicIPs: ips, RouteTarget: routeTarget, Status: status}, nil
}

func (a Applier) createInstance(ctx context.Context, inputs Inputs, name string) error {
	inst := gatewayInstance(inputs, name)
	op, err := a.Compute.Instances.Insert(inputs.ProjectID, inputs.Zone, inst).Context(ctx).Do()
	if err != nil {
		return err
	}
	return a.waitZoneOperation(ctx, inputs, op.Name)
}

func gatewayInstance(inputs Inputs, name string) *compute.Instance {
	inst := &compute.Instance{
		Name:         name,
		CanIpForward: true,
		MachineType:  zoneLink(inputs.ProjectID, inputs.Zone, "machineTypes", valueOr(inputs.MachineType, "e2-small")),
		Labels:       inputs.Labels,
		Tags:         &compute.Tags{Items: []string{name}},
		Disks: []*compute.AttachedDisk{{
			AutoDelete: true,
			Boot:       true,
			Type:       "PERSISTENT",
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: fmt.Sprintf("projects/%s/global/images/family/%s", valueOr(inputs.ImageProject, "debian-cloud"), valueOr(inputs.ImageFamily, "debian-12")),
			},
		}},
		NetworkInterfaces: []*compute.NetworkInterface{{
			Subnetwork: regionalLink(inputs.ProjectID, inputs.Region, "subnetworks", inputs.Subnetwork),
			AccessConfigs: []*compute.AccessConfig{{
				Name: "External NAT",
				Type: "ONE_TO_ONE_NAT",
			}},
		}},
		Metadata: &compute.Metadata{Items: []*compute.MetadataItems{{
			Key:   "startup-script",
			Value: strPtr(valueOr(inputs.StartupScript, GatewayStartupScript(StartupScriptInputs{PrivateCIDRs: inputs.PrivateCIDRs}))),
		}}},
	}
	if inputs.ServiceAccountEmail != "" {
		inst.ServiceAccounts = []*compute.ServiceAccount{{
			Email:  inputs.ServiceAccountEmail,
			Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
		}}
	}
	return inst
}

func (a Applier) createRoute(ctx context.Context, inputs Inputs, gatewayName string) error {
	route := &compute.Route{
		Name:            inputs.RouteName,
		Network:         globalLink(inputs.ProjectID, "networks", inputs.Network),
		DestRange:       valueOr(inputs.RouteDestRange, "0.0.0.0/0"),
		Priority:        valueOrInt(inputs.RoutePriority, 800),
		Tags:            []string{inputs.ClientTag},
		NextHopInstance: zoneLink(inputs.ProjectID, inputs.Zone, "instances", gatewayName),
	}
	op, err := a.Compute.Routes.Insert(inputs.ProjectID, route).Context(ctx).Do()
	if err != nil {
		return err
	}
	return a.waitGlobalOperation(ctx, inputs, op.Name)
}

func (a Applier) deleteRoute(ctx context.Context, inputs Inputs) error {
	op, err := a.Compute.Routes.Delete(inputs.ProjectID, inputs.RouteName).Context(ctx).Do()
	if err != nil {
		return err
	}
	return a.waitGlobalOperation(ctx, inputs, op.Name)
}

func (a Applier) deleteInstance(ctx context.Context, inputs Inputs, name string) error {
	op, err := a.Compute.Instances.Delete(inputs.ProjectID, inputs.Zone, name).Context(ctx).Do()
	if err != nil {
		return err
	}
	return a.waitZoneOperation(ctx, inputs, op.Name)
}

func (a Applier) waitZoneOperation(ctx context.Context, inputs Inputs, name string) error {
	poll := inputs.OperationPollTime
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		op, err := a.Compute.ZoneOperations.Get(inputs.ProjectID, inputs.Zone, name).Context(ctx).Do()
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

func (a Applier) waitGlobalOperation(ctx context.Context, inputs Inputs, name string) error {
	poll := inputs.OperationPollTime
	if poll <= 0 {
		poll = 2 * time.Second
	}
	for {
		op, err := a.Compute.GlobalOperations.Get(inputs.ProjectID, name).Context(ctx).Do()
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

func (i Inputs) validate() error {
	missing := []string{}
	for name, value := range map[string]string{
		"name": i.Name, "project_id": i.ProjectID, "region": i.Region,
		"zone": i.Zone, "network": i.Network, "subnetwork": i.Subnetwork,
		"client_tag": i.ClientTag, "route_name": i.RouteName,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required GCP inputs: %s", strings.Join(missing, ", "))
	}
	if i.GatewayCount < 1 {
		return fmt.Errorf("gateway_count must be at least 1")
	}
	return nil
}

func gatewayNames(name string, count int64) []string {
	if count <= 0 {
		count = 2
	}
	names := make([]string, 0, count)
	for i := int64(0); i < count; i++ {
		names = append(names, fmt.Sprintf("%s-gw-%c", name, 'a'+rune(i)))
	}
	return names
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

func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "googleapi: Error 404")
}

func globalLink(projectID, collection, name string) string {
	if strings.HasPrefix(name, "http") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return fmt.Sprintf("projects/%s/global/%s/%s", projectID, collection, name)
}

func regionalLink(projectID, region, collection, name string) string {
	if strings.HasPrefix(name, "http") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return fmt.Sprintf("projects/%s/regions/%s/%s/%s", projectID, region, collection, name)
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

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func valueOrInt(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}

func strPtr(value string) *string {
	return &value
}
