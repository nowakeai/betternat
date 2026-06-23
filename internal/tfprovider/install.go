package tfprovider

import (
	"context"
	"encoding/json"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awssdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/nowakeai/betternat/internal/install/aws"
	"github.com/nowakeai/betternat/internal/installplan"
)

type Installer interface {
	Install(ctx context.Context, plan installplan.Plan, inputs awsinstall.Inputs) (awsinstall.Result, error)
	UpdateCapacity(ctx context.Context, plan installplan.Plan) error
	ReconcileInfrastructure(ctx context.Context, plan installplan.Plan) error
}

type Rollbacker interface {
	RestoreRoutes(ctx context.Context, routes []awsinstall.RollbackRoute) error
}

type Cleaner interface {
	Cleanup(ctx context.Context, plan installplan.Plan, inputs awsinstall.CleanupInputs) error
}

type Reader interface {
	Read(ctx context.Context, plan installplan.Plan) (awsinstall.ReadResult, error)
}

type InstallerFactory func(ctx context.Context, region string) (Installer, error)
type RollbackerFactory func(ctx context.Context, region string) (Rollbacker, error)
type CleanerFactory func(ctx context.Context, region string) (Cleaner, error)
type ReaderFactory func(ctx context.Context, region string) (Reader, error)

type providerData struct {
	InstallerFactory  InstallerFactory
	RollbackerFactory RollbackerFactory
	CleanerFactory    CleanerFactory
	ReaderFactory     ReaderFactory
}

type awsInstaller struct {
	applier awsinstall.Applier
}

func (i awsInstaller) Install(ctx context.Context, plan installplan.Plan, inputs awsinstall.Inputs) (awsinstall.Result, error) {
	return i.applier.Apply(ctx, plan, inputs)
}

func (i awsInstaller) UpdateCapacity(ctx context.Context, plan installplan.Plan) error {
	return i.applier.UpdateCapacity(ctx, plan)
}

func (i awsInstaller) ReconcileInfrastructure(ctx context.Context, plan installplan.Plan) error {
	return i.applier.ReconcileInfrastructure(ctx, plan)
}

func (i awsInstaller) RestoreRoutes(ctx context.Context, routes []awsinstall.RollbackRoute) error {
	return i.applier.RestoreRoutes(ctx, routes)
}

func (i awsInstaller) Cleanup(ctx context.Context, plan installplan.Plan, inputs awsinstall.CleanupInputs) error {
	return i.applier.Cleanup(ctx, plan, inputs)
}

func (i awsInstaller) Read(ctx context.Context, plan installplan.Plan) (awsinstall.ReadResult, error) {
	return i.applier.Read(ctx, plan)
}

func defaultInstallerFactory(ctx context.Context, region string) (Installer, error) {
	installer, err := defaultAWSLifecycle(ctx, region)
	if err != nil {
		return nil, err
	}
	return installer, nil
}

func defaultRollbackerFactory(ctx context.Context, region string) (Rollbacker, error) {
	rollbacker, err := defaultAWSLifecycle(ctx, region)
	if err != nil {
		return nil, err
	}
	return rollbacker, nil
}

func defaultCleanerFactory(ctx context.Context, region string) (Cleaner, error) {
	cleaner, err := defaultAWSLifecycle(ctx, region)
	if err != nil {
		return nil, err
	}
	return cleaner, nil
}

func defaultReaderFactory(ctx context.Context, region string) (Reader, error) {
	reader, err := defaultAWSLifecycle(ctx, region)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func defaultAWSLifecycle(ctx context.Context, region string) (awsInstaller, error) {
	cfg, err := awssdkconfig.LoadDefaultConfig(ctx, awssdkconfig.WithRegion(region))
	if err != nil {
		return awsInstaller{}, fmt.Errorf("load aws config: %w", err)
	}
	return awsLifecycleFromConfig(cfg, "")
}

func endpointInstallerFactory(endpointURL string) InstallerFactory {
	return func(ctx context.Context, region string) (Installer, error) {
		installer, err := endpointAWSLifecycle(ctx, region, endpointURL)
		if err != nil {
			return nil, err
		}
		return installer, nil
	}
}

func endpointRollbackerFactory(endpointURL string) RollbackerFactory {
	return func(ctx context.Context, region string) (Rollbacker, error) {
		rollbacker, err := endpointAWSLifecycle(ctx, region, endpointURL)
		if err != nil {
			return nil, err
		}
		return rollbacker, nil
	}
}

func endpointCleanerFactory(endpointURL string) CleanerFactory {
	return func(ctx context.Context, region string) (Cleaner, error) {
		cleaner, err := endpointAWSLifecycle(ctx, region, endpointURL)
		if err != nil {
			return nil, err
		}
		return cleaner, nil
	}
}

func endpointReaderFactory(endpointURL string) ReaderFactory {
	return func(ctx context.Context, region string) (Reader, error) {
		reader, err := endpointAWSLifecycle(ctx, region, endpointURL)
		if err != nil {
			return nil, err
		}
		return reader, nil
	}
}

func endpointAWSLifecycle(ctx context.Context, region string, endpointURL string) (awsInstaller, error) {
	cfg, err := awssdkconfig.LoadDefaultConfig(ctx,
		awssdkconfig.WithRegion(region),
		awssdkconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return awsInstaller{}, fmt.Errorf("load aws config: %w", err)
	}
	return awsLifecycleFromConfig(cfg, endpointURL)
}

func awsLifecycleFromConfig(cfg awssdk.Config, endpointURL string) (awsInstaller, error) {
	ec2Options := []func(*ec2.Options){}
	autoScalingOptions := []func(*autoscaling.Options){}
	dynamoOptions := []func(*dynamodb.Options){}
	iamOptions := []func(*iam.Options){}
	if endpointURL != "" {
		ec2Options = append(ec2Options, func(o *ec2.Options) { o.BaseEndpoint = &endpointURL })
		autoScalingOptions = append(autoScalingOptions, func(o *autoscaling.Options) { o.BaseEndpoint = &endpointURL })
		dynamoOptions = append(dynamoOptions, func(o *dynamodb.Options) { o.BaseEndpoint = &endpointURL })
		iamOptions = append(iamOptions, func(o *iam.Options) { o.BaseEndpoint = &endpointURL })
	}
	return awsInstaller{
		applier: awsinstall.Applier{
			EC2:         ec2.NewFromConfig(cfg, ec2Options...),
			AutoScaling: autoscaling.NewFromConfig(cfg, autoScalingOptions...),
			DynamoDB:    dynamodb.NewFromConfig(cfg, dynamoOptions...),
			IAM:         iam.NewFromConfig(cfg, iamOptions...),
		},
	}, nil
}

func installGatewayState(ctx context.Context, state *GatewayResourceModel, factory InstallerFactory) error {
	if factory == nil {
		return fmt.Errorf("installer factory is not configured")
	}
	var plan installplan.Plan
	if err := json.Unmarshal([]byte(state.InstallPlanJSON.ValueString()), &plan); err != nil {
		return fmt.Errorf("decode install plan: %w", err)
	}
	installer, err := factory(ctx, state.Region.ValueString())
	if err != nil {
		return err
	}
	result, err := installer.Install(ctx, plan, awsinstall.Inputs{
		UserData: state.UserData.ValueString(),
	})
	if err != nil {
		return err
	}
	state.EgressPublicIPs = mustStringMap(result.AllocatedPublicIPs)
	active, standby := launchedInstanceMaps(plan, result.LaunchedInstances)
	for az, instanceID := range result.OwnerInstances {
		active[az] = instanceID
	}
	state.ActiveInstanceIDs = mustStringMap(active)
	state.StandbyInstanceIDs = mustStringMap(standby)
	if len(result.PreviousRouteTargets) > 0 {
		rollbackJSON, err := rollbackJSONFromTargets(plan, result.PreviousRouteTargets)
		if err != nil {
			return err
		}
		state.RollbackRouteTargetsJSON = types.StringValue(rollbackJSON)
	}
	state.Status = types.StringValue("created")
	return nil
}

func updateGatewayCapacity(ctx context.Context, state GatewayResourceModel, factory InstallerFactory) error {
	if factory == nil {
		return fmt.Errorf("installer factory is not configured")
	}
	var plan installplan.Plan
	if err := json.Unmarshal([]byte(state.InstallPlanJSON.ValueString()), &plan); err != nil {
		return fmt.Errorf("decode install plan: %w", err)
	}
	installer, err := factory(ctx, state.Region.ValueString())
	if err != nil {
		return err
	}
	if err := installer.ReconcileInfrastructure(ctx, plan); err != nil {
		return err
	}
	return installer.UpdateCapacity(ctx, plan)
}

func launchedInstanceMaps(plan installplan.Plan, launched map[string]string) (map[string]string, map[string]string) {
	active := map[string]string{}
	standby := map[string]string{}
	for _, appliance := range plan.Appliances {
		instanceID := launched[appliance.Name]
		if instanceID == "" {
			continue
		}
		switch appliance.Role {
		case "active":
			active[appliance.AvailabilityZone] = instanceID
		case "standby":
			standby[appliance.AvailabilityZone] = instanceID
		}
	}
	return active, standby
}

func rollbackJSONFromTargets(plan installplan.Plan, targets map[string]string) (string, error) {
	entries := make(map[string]map[string]string, len(plan.ManagedRoutes))
	for _, route := range plan.ManagedRoutes {
		target := targets[route.RouteTableID]
		if target == "" {
			target = "unknown"
		}
		entries[route.RouteTableID] = map[string]string{
			"destination_cidr": route.DestinationCIDR,
			"target":           target,
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal rollback targets: %w", err)
	}
	return string(data), nil
}

func rollbackGatewayRoutes(ctx context.Context, state GatewayResourceModel, factory RollbackerFactory) error {
	if factory == nil {
		return fmt.Errorf("rollbacker factory is not configured")
	}
	routes, err := parseRollbackRoutes(state.RollbackRouteTargetsJSON.ValueString())
	if err != nil {
		return err
	}
	rollbacker, err := factory(ctx, state.Region.ValueString())
	if err != nil {
		return err
	}
	return rollbacker.RestoreRoutes(ctx, routes)
}

func cleanupGatewayResources(ctx context.Context, state GatewayResourceModel, factory CleanerFactory) error {
	if factory == nil {
		return fmt.Errorf("cleaner factory is not configured")
	}
	var plan installplan.Plan
	if err := json.Unmarshal([]byte(state.InstallPlanJSON.ValueString()), &plan); err != nil {
		return fmt.Errorf("decode install plan: %w", err)
	}
	cleaner, err := factory(ctx, state.Region.ValueString())
	if err != nil {
		return err
	}
	instanceIDs, err := gatewayInstanceIDs(ctx, state)
	if err != nil {
		return err
	}
	return cleaner.Cleanup(ctx, plan, awsinstall.CleanupInputs{InstanceIDs: instanceIDs})
}

func readGatewayState(ctx context.Context, state *GatewayResourceModel, factory ReaderFactory) error {
	if factory == nil {
		return fmt.Errorf("reader factory is not configured")
	}
	var plan installplan.Plan
	if err := json.Unmarshal([]byte(state.InstallPlanJSON.ValueString()), &plan); err != nil {
		return fmt.Errorf("decode install plan: %w", err)
	}
	reader, err := factory(ctx, state.Region.ValueString())
	if err != nil {
		return err
	}
	result, err := reader.Read(ctx, plan)
	if err != nil {
		return err
	}
	statusBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal control plane status: %w", err)
	}
	state.ControlPlaneStatusJSON = types.StringValue(string(statusBytes))
	if len(result.EgressPublicIPs) > 0 {
		state.EgressPublicIPs = mustStringMap(result.EgressPublicIPs)
	}
	if len(result.PublicIdentityInstanceIDs) > 0 {
		active, err := mapStrings(ctx, state.ActiveInstanceIDs)
		if err != nil {
			return fmt.Errorf("active_instance_ids: %w", err)
		}
		for az, instanceID := range result.PublicIdentityInstanceIDs {
			if instanceID != "" {
				active[az] = instanceID
			}
		}
		state.ActiveInstanceIDs = mustStringMap(active)
	}
	state.Status = types.StringValue(statusFromReadResult(plan, result))
	return nil
}

func statusFromReadResult(plan installplan.Plan, result awsinstall.ReadResult) string {
	if len(plan.ManagedRoutes) == 0 {
		return "created"
	}
	for _, route := range plan.ManagedRoutes {
		if result.RouteTargets[route.RouteTableID] == "" {
			return "degraded"
		}
	}
	return "active"
}

func parseRollbackRoutes(raw string) ([]awsinstall.RollbackRoute, error) {
	if raw == "" {
		return nil, fmt.Errorf("rollback targets are empty")
	}
	var entries map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("decode rollback targets: %w", err)
	}
	routes := make([]awsinstall.RollbackRoute, 0, len(entries))
	for routeTableID, entry := range entries {
		route := awsinstall.RollbackRoute{
			RouteTableID:    routeTableID,
			DestinationCIDR: entry["destination_cidr"],
			Target:          entry["target"],
		}
		if route.RouteTableID == "" || route.DestinationCIDR == "" || route.Target == "" || route.Target == "unknown" {
			return nil, fmt.Errorf("rollback target for route table %q is incomplete", routeTableID)
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func gatewayInstanceIDs(ctx context.Context, state GatewayResourceModel) ([]string, error) {
	active, err := mapStrings(ctx, state.ActiveInstanceIDs)
	if err != nil {
		return nil, fmt.Errorf("active_instance_ids: %w", err)
	}
	standby, err := mapStrings(ctx, state.StandbyInstanceIDs)
	if err != nil {
		return nil, fmt.Errorf("standby_instance_ids: %w", err)
	}
	var ids []string
	for _, id := range active {
		ids = append(ids, id)
	}
	for _, id := range standby {
		ids = append(ids, id)
	}
	return ids, nil
}
