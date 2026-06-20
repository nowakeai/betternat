package tfprovider

import (
	"context"
	"encoding/json"
	"fmt"

	awssdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/betternat/betternat/internal/install/aws"
	"github.com/betternat/betternat/internal/installplan"
)

type Installer interface {
	Install(ctx context.Context, plan installplan.Plan, inputs awsinstall.Inputs) (awsinstall.Result, error)
}

type InstallerFactory func(ctx context.Context, region string) (Installer, error)

type providerData struct {
	InstallerFactory InstallerFactory
}

type awsInstaller struct {
	applier awsinstall.Applier
}

func (i awsInstaller) Install(ctx context.Context, plan installplan.Plan, inputs awsinstall.Inputs) (awsinstall.Result, error) {
	return i.applier.Apply(ctx, plan, inputs)
}

func defaultInstallerFactory(ctx context.Context, region string) (Installer, error) {
	cfg, err := awssdkconfig.LoadDefaultConfig(ctx, awssdkconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return awsInstaller{
		applier: awsinstall.Applier{
			EC2:      ec2.NewFromConfig(cfg),
			DynamoDB: dynamodb.NewFromConfig(cfg),
			IAM:      iam.NewFromConfig(cfg),
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
	state.ActiveInstanceIDs = mustStringMap(active)
	state.StandbyInstanceIDs = mustStringMap(standby)
	state.Status = types.StringValue("created")
	return nil
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
