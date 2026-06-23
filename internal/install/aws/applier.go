package awsinstall

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	astypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"

	"github.com/nowakeai/betternat/internal/installplan"
)

type EC2API interface {
	AllocateAddress(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error)
	AuthorizeSecurityGroupEgress(ctx context.Context, params *ec2.AuthorizeSecurityGroupEgressInput, optFns ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupEgressOutput, error)
	AuthorizeSecurityGroupIngress(ctx context.Context, params *ec2.AuthorizeSecurityGroupIngressInput, optFns ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	AssociateAddress(ctx context.Context, params *ec2.AssociateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error)
	CreateLaunchTemplate(ctx context.Context, params *ec2.CreateLaunchTemplateInput, optFns ...func(*ec2.Options)) (*ec2.CreateLaunchTemplateOutput, error)
	CreateSecurityGroup(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
	DeleteLaunchTemplate(ctx context.Context, params *ec2.DeleteLaunchTemplateInput, optFns ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error)
	DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	DescribeAddresses(ctx context.Context, params *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeRouteTables(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DisassociateAddress(ctx context.Context, params *ec2.DisassociateAddressInput, optFns ...func(*ec2.Options)) (*ec2.DisassociateAddressOutput, error)
	ModifyInstanceAttribute(ctx context.Context, params *ec2.ModifyInstanceAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error)
	ReplaceRoute(ctx context.Context, params *ec2.ReplaceRouteInput, optFns ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error)
	ReleaseAddress(ctx context.Context, params *ec2.ReleaseAddressInput, optFns ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error)
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

type AutoScalingAPI interface {
	CreateAutoScalingGroup(ctx context.Context, params *autoscaling.CreateAutoScalingGroupInput, optFns ...func(*autoscaling.Options)) (*autoscaling.CreateAutoScalingGroupOutput, error)
	DeleteAutoScalingGroup(ctx context.Context, params *autoscaling.DeleteAutoScalingGroupInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DeleteAutoScalingGroupOutput, error)
	DeleteLifecycleHook(ctx context.Context, params *autoscaling.DeleteLifecycleHookInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DeleteLifecycleHookOutput, error)
	DescribeAutoScalingGroups(ctx context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
	PutLifecycleHook(ctx context.Context, params *autoscaling.PutLifecycleHookInput, optFns ...func(*autoscaling.Options)) (*autoscaling.PutLifecycleHookOutput, error)
	UpdateAutoScalingGroup(ctx context.Context, params *autoscaling.UpdateAutoScalingGroupInput, optFns ...func(*autoscaling.Options)) (*autoscaling.UpdateAutoScalingGroupOutput, error)
}

type DynamoDBAPI interface {
	CreateTable(ctx context.Context, params *dynamodb.CreateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	DeleteTable(ctx context.Context, params *dynamodb.DeleteTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error)
}

type IAMAPI interface {
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	CreateInstanceProfile(ctx context.Context, params *iam.CreateInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error)
	AddRoleToInstanceProfile(ctx context.Context, params *iam.AddRoleToInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error)
	AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	DeleteInstanceProfile(ctx context.Context, params *iam.DeleteInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.DeleteInstanceProfileOutput, error)
	DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
	DeleteRolePolicy(ctx context.Context, params *iam.DeleteRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error)
	DetachRolePolicy(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
	RemoveRoleFromInstanceProfile(ctx context.Context, params *iam.RemoveRoleFromInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.RemoveRoleFromInstanceProfileOutput, error)
}

type Applier struct {
	EC2         EC2API
	AutoScaling AutoScalingAPI
	DynamoDB    DynamoDBAPI
	IAM         IAMAPI
}

type Inputs struct {
	ApplianceInstanceIDs map[string]string
	UserData             string
}

type Result struct {
	AllocatedEIPs        map[string]string `json:"allocated_eips"`
	AllocatedPublicIPs   map[string]string `json:"allocated_public_ips"`
	AutoScalingGroups    map[string]string `json:"auto_scaling_groups"`
	InitialRouteTargets  map[string]string `json:"initial_route_targets"`
	LaunchedInstances    map[string]string `json:"launched_instances"`
	LaunchTemplates      map[string]string `json:"launch_templates"`
	OwnerInstances       map[string]string `json:"owner_instances"`
	PreviousRouteTargets map[string]string `json:"previous_route_targets"`
}

type RollbackRoute struct {
	RouteTableID    string
	DestinationCIDR string
	Target          string
}

type CleanupInputs struct {
	InstanceIDs []string
}

type ReadResult struct {
	RouteTargets              map[string]string `json:"route_targets"`
	EgressPublicIPs           map[string]string `json:"egress_public_ips"`
	PublicIdentityInstanceIDs map[string]string `json:"public_identity_instance_ids"`
}

func (a Applier) Apply(ctx context.Context, plan installplan.Plan, inputs Inputs) (Result, error) {
	if a.EC2 == nil {
		return Result{}, fmt.Errorf("ec2 client is required")
	}
	if a.DynamoDB == nil {
		return Result{}, fmt.Errorf("dynamodb client is required")
	}
	if a.IAM == nil {
		return Result{}, fmt.Errorf("iam client is required")
	}
	if len(plan.Pools) > 0 && a.AutoScaling == nil {
		return Result{}, fmt.Errorf("autoscaling client is required")
	}
	if err := a.createIAM(ctx, plan); err != nil {
		return Result{}, err
	}
	securityGroupID, err := a.ensureSecurityGroup(ctx, plan)
	if err != nil {
		return Result{}, err
	}
	if err := a.createLeaseTable(ctx, plan); err != nil {
		return Result{}, err
	}
	if err := a.createCoordinationTable(ctx, plan); err != nil {
		return Result{}, err
	}
	result := Result{
		AllocatedEIPs:        map[string]string{},
		AllocatedPublicIPs:   map[string]string{},
		AutoScalingGroups:    map[string]string{},
		InitialRouteTargets:  map[string]string{},
		LaunchedInstances:    map[string]string{},
		LaunchTemplates:      map[string]string{},
		OwnerInstances:       map[string]string{},
		PreviousRouteTargets: map[string]string{},
	}
	instanceIDs := map[string]string{}
	for name, id := range inputs.ApplianceInstanceIDs {
		instanceIDs[name] = id
	}
	if len(plan.Pools) > 0 {
		for _, pool := range plan.Pools {
			launchTemplateID, err := a.createLaunchTemplate(ctx, plan, pool, securityGroupID, inputs.UserData)
			if err != nil {
				return Result{}, err
			}
			result.LaunchTemplates[pool.AvailabilityZone] = launchTemplateID
			if err := a.createAutoScalingGroup(ctx, plan, pool, launchTemplateID); err != nil {
				return Result{}, err
			}
			if err := a.putTerminationLifecycleHook(ctx, pool); err != nil {
				return Result{}, err
			}
			result.AutoScalingGroups[pool.AvailabilityZone] = pool.ASGName
			ownerInstanceID, err := a.waitForPoolOwner(ctx, pool.ASGName)
			if err != nil {
				return Result{}, err
			}
			instanceIDs[pool.Name] = ownerInstanceID
			result.OwnerInstances[pool.AvailabilityZone] = ownerInstanceID
			if err := a.disableSourceDestCheck(ctx, ownerInstanceID); err != nil {
				return Result{}, err
			}
		}
	} else {
		for _, appliance := range plan.Appliances {
			instanceID := instanceIDs[appliance.Name]
			if instanceID == "" {
				instanceID, err = a.launchAppliance(ctx, plan, appliance, securityGroupID, inputs.UserData)
				if err != nil {
					return Result{}, err
				}
				instanceIDs[appliance.Name] = instanceID
				result.LaunchedInstances[appliance.Name] = instanceID
			}
			if err := a.disableSourceDestCheck(ctx, instanceID); err != nil {
				return Result{}, err
			}
		}
	}
	for az, name := range plan.EIPAllocationNames {
		allocationID, publicIP, err := a.allocateEIP(ctx, name, plan.Tags)
		if err != nil {
			return Result{}, err
		}
		instanceID := instanceIDs[plan.Name+"-"+az+"-active"]
		if instanceID == "" {
			instanceID = instanceIDs[plan.Name+"-"+az]
		}
		if instanceID == "" {
			return Result{}, fmt.Errorf("missing active instance id for %s", az)
		}
		if err := a.associateEIP(ctx, allocationID, instanceID); err != nil {
			return Result{}, err
		}
		result.AllocatedEIPs[az] = allocationID
		result.AllocatedPublicIPs[az] = publicIP
	}
	for _, route := range plan.ManagedRoutes {
		previousTarget, err := a.describeRouteTarget(ctx, route.RouteTableID, route.DestinationCIDR)
		if err != nil {
			return Result{}, fmt.Errorf("snapshot previous route %s: %w", route.RouteTableID, err)
		}
		result.PreviousRouteTargets[route.RouteTableID] = previousTarget
		instanceID := instanceIDs[plan.Name+"-"+route.AvailabilityZone+"-active"]
		if instanceID == "" {
			instanceID = instanceIDs[plan.Name+"-"+route.AvailabilityZone]
		}
		if instanceID == "" {
			return Result{}, fmt.Errorf("missing active instance id for %s", route.AvailabilityZone)
		}
		input, err := replaceRouteInput(route.RouteTableID, route.DestinationCIDR, instanceID)
		if err != nil {
			return Result{}, fmt.Errorf("build initial route %s: %w", route.RouteTableID, err)
		}
		if _, err := a.EC2.ReplaceRoute(ctx, input); err != nil {
			return Result{}, fmt.Errorf("replace initial route %s: %w", route.RouteTableID, err)
		}
		result.InitialRouteTargets[route.RouteTableID] = instanceID
	}
	return result, nil
}

func (a Applier) UpdateCapacity(ctx context.Context, plan installplan.Plan) error {
	if len(plan.Pools) == 0 {
		return fmt.Errorf("capacity update requires ASG pools")
	}
	if a.AutoScaling == nil {
		return fmt.Errorf("autoscaling client is required")
	}
	for _, pool := range plan.Pools {
		if pool.ASGName == "" {
			return fmt.Errorf("asg name is required for pool %s", pool.Name)
		}
		_, err := a.AutoScaling.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: awssdk.String(pool.ASGName),
			MinSize:              awssdk.Int32(pool.MinSize),
			MaxSize:              awssdk.Int32(pool.MaxSize),
			DesiredCapacity:      awssdk.Int32(pool.DesiredCapacity),
		})
		if err != nil {
			return fmt.Errorf("update asg capacity %s: %w", pool.ASGName, err)
		}
	}
	return nil
}

func (a Applier) ReconcileInfrastructure(ctx context.Context, plan installplan.Plan) error {
	if a.DynamoDB == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if a.IAM == nil {
		return fmt.Errorf("iam client is required")
	}
	if err := a.createCoordinationTable(ctx, plan); err != nil {
		return err
	}
	return a.createIAM(ctx, plan)
}

func (a Applier) RestoreRoutes(ctx context.Context, routes []RollbackRoute) error {
	if a.EC2 == nil {
		return fmt.Errorf("ec2 client is required")
	}
	for _, route := range routes {
		input, err := replaceRouteInput(route.RouteTableID, route.DestinationCIDR, route.Target)
		if err != nil {
			return fmt.Errorf("build rollback route %s: %w", route.RouteTableID, err)
		}
		if _, err := a.EC2.ReplaceRoute(ctx, input); err != nil {
			if isStaleRollbackTarget(err) {
				continue
			}
			return fmt.Errorf("rollback route %s: %w", route.RouteTableID, err)
		}
	}
	return nil
}

func isStaleRollbackTarget(err error) bool {
	return isAPIError(err, "InvalidNetworkInterfaceID.NotFound") ||
		isAPIError(err, "InvalidNatGatewayID.NotFound") ||
		isAPIError(err, "InvalidInstanceID.NotFound")
}

func (a Applier) Cleanup(ctx context.Context, plan installplan.Plan, inputs CleanupInputs) error {
	if a.EC2 == nil {
		return fmt.Errorf("ec2 client is required")
	}
	if a.DynamoDB == nil {
		return fmt.Errorf("dynamodb client is required")
	}
	if a.IAM == nil {
		return fmt.Errorf("iam client is required")
	}
	if len(plan.Pools) > 0 && a.AutoScaling == nil {
		return fmt.Errorf("autoscaling client is required")
	}
	if err := a.deletePools(ctx, plan); err != nil {
		return err
	}
	if err := a.terminateAppliances(ctx, inputs.InstanceIDs); err != nil {
		return err
	}
	if err := a.releaseEIPs(ctx, plan); err != nil {
		return err
	}
	if err := a.deleteLeaseTable(ctx, plan); err != nil {
		return err
	}
	if err := a.deleteCoordinationTable(ctx, plan); err != nil {
		return err
	}
	if err := a.deleteIAM(ctx, plan); err != nil {
		return err
	}
	if err := a.deleteSecurityGroup(ctx, plan); err != nil {
		return err
	}
	return nil
}

func (a Applier) deletePools(ctx context.Context, plan installplan.Plan) error {
	for _, pool := range plan.Pools {
		if pool.ASGName != "" {
			if _, err := a.AutoScaling.DeleteLifecycleHook(ctx, &autoscaling.DeleteLifecycleHookInput{
				AutoScalingGroupName: awssdk.String(pool.ASGName),
				LifecycleHookName:    awssdk.String(terminationLifecycleHookName(pool)),
			}); err != nil && !isAPIError(err, "ValidationError") && !isAPIError(err, "ResourceContention") {
				return fmt.Errorf("delete lifecycle hook %s: %w", terminationLifecycleHookName(pool), err)
			}
			if _, err := a.AutoScaling.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
				AutoScalingGroupName: awssdk.String(pool.ASGName),
				MinSize:              awssdk.Int32(0),
				DesiredCapacity:      awssdk.Int32(0),
			}); err != nil && !isAPIError(err, "ValidationError") {
				return fmt.Errorf("scale down asg %s: %w", pool.ASGName, err)
			}
			if _, err := a.AutoScaling.DeleteAutoScalingGroup(ctx, &autoscaling.DeleteAutoScalingGroupInput{
				AutoScalingGroupName: awssdk.String(pool.ASGName),
				ForceDelete:          awssdk.Bool(true),
			}); err != nil && !isAPIError(err, "ValidationError") {
				return fmt.Errorf("delete asg %s: %w", pool.ASGName, err)
			}
		}
		if pool.LaunchTemplateName != "" {
			if err := a.deleteLaunchTemplate(ctx, pool.LaunchTemplateName); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a Applier) deleteLaunchTemplate(ctx context.Context, name string) error {
	input := &ec2.DeleteLaunchTemplateInput{LaunchTemplateName: awssdk.String(name)}
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := a.EC2.DeleteLaunchTemplate(ctx, input); err != nil {
			if isAPIError(err, "InvalidLaunchTemplateName.NotFoundException") {
				return nil
			}
			if isAPIError(err, "DependencyViolation") || isAPIError(err, "ResourceInUse") {
				lastErr = err
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					continue
				}
			}
			return fmt.Errorf("delete launch template %s: %w", name, err)
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("delete launch template %s after dependency wait: %w", name, lastErr)
	}
	return nil
}

func (a Applier) Read(ctx context.Context, plan installplan.Plan) (ReadResult, error) {
	if a.EC2 == nil {
		return ReadResult{}, fmt.Errorf("ec2 client is required")
	}
	result := ReadResult{
		RouteTargets:              map[string]string{},
		EgressPublicIPs:           map[string]string{},
		PublicIdentityInstanceIDs: map[string]string{},
	}
	for _, route := range plan.ManagedRoutes {
		target, err := a.describeRouteTarget(ctx, route.RouteTableID, route.DestinationCIDR)
		if err != nil {
			return ReadResult{}, fmt.Errorf("read route %s: %w", route.RouteTableID, err)
		}
		result.RouteTargets[route.RouteTableID] = target
	}
	for az, name := range plan.EIPAllocationNames {
		addresses, err := a.describeBetterNATAddresses(ctx, plan.Name, name)
		if err != nil {
			return ReadResult{}, fmt.Errorf("read eip %s: %w", name, err)
		}
		if len(addresses) == 0 {
			continue
		}
		address := addresses[0]
		result.EgressPublicIPs[az] = awssdk.ToString(address.PublicIp)
		result.PublicIdentityInstanceIDs[az] = awssdk.ToString(address.InstanceId)
	}
	return result, nil
}

func (a Applier) terminateAppliances(ctx context.Context, instanceIDs []string) error {
	ids := uniqueNonEmpty(instanceIDs)
	if len(ids) == 0 {
		return nil
	}
	if _, err := a.EC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: ids}); err != nil {
		if isAPIError(err, "InvalidInstanceID.NotFound") {
			return nil
		}
		return fmt.Errorf("terminate appliances: %w", err)
	}
	return nil
}

func (a Applier) releaseEIPs(ctx context.Context, plan installplan.Plan) error {
	for _, name := range plan.EIPAllocationNames {
		addresses, err := a.describeBetterNATAddresses(ctx, plan.Name, name)
		if err != nil {
			return fmt.Errorf("describe eip %s: %w", name, err)
		}
		for _, address := range addresses {
			allocationID := awssdk.ToString(address.AllocationId)
			if allocationID == "" {
				continue
			}
			if associationID := awssdk.ToString(address.AssociationId); associationID != "" {
				if _, err := a.EC2.DisassociateAddress(ctx, &ec2.DisassociateAddressInput{AssociationId: awssdk.String(associationID)}); err != nil && !isAPIError(err, "InvalidAssociationID.NotFound") {
					return fmt.Errorf("disassociate eip %s: %w", allocationID, err)
				}
			}
			if _, err := a.EC2.ReleaseAddress(ctx, &ec2.ReleaseAddressInput{AllocationId: awssdk.String(allocationID)}); err != nil {
				if isAPIError(err, "InvalidAllocationID.NotFound") {
					continue
				}
				return fmt.Errorf("release eip %s: %w", allocationID, err)
			}
		}
	}
	return nil
}

func (a Applier) describeBetterNATAddresses(ctx context.Context, gatewayName string, eipName string) ([]ec2types.Address, error) {
	output, err := a.EC2.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{
		Filters: []ec2types.Filter{
			{Name: awssdk.String("tag:Name"), Values: []string{eipName}},
			{Name: awssdk.String("tag:BetterNATGateway"), Values: []string{gatewayName}},
		},
	})
	if err != nil {
		return nil, err
	}
	return output.Addresses, nil
}

func (a Applier) deleteLeaseTable(ctx context.Context, plan installplan.Plan) error {
	if plan.LeaseTableName == "" {
		return nil
	}
	if _, err := a.DynamoDB.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: awssdk.String(plan.LeaseTableName)}); err != nil {
		if isAPIError(err, "ResourceNotFoundException") {
			return nil
		}
		return fmt.Errorf("delete lease table %s: %w", plan.LeaseTableName, err)
	}
	return nil
}

func (a Applier) deleteCoordinationTable(ctx context.Context, plan installplan.Plan) error {
	if plan.CoordinationTableName == "" {
		return nil
	}
	if _, err := a.DynamoDB.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: awssdk.String(plan.CoordinationTableName)}); err != nil {
		if isAPIError(err, "ResourceNotFoundException") {
			return nil
		}
		return fmt.Errorf("delete coordination table %s: %w", plan.CoordinationTableName, err)
	}
	return nil
}

func (a Applier) deleteIAM(ctx context.Context, plan installplan.Plan) error {
	if plan.IAMRoleName == "" && plan.InstanceProfileName == "" {
		return nil
	}
	if plan.InstanceProfileName != "" && plan.IAMRoleName != "" {
		if _, err := a.IAM.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{
			InstanceProfileName: awssdk.String(plan.InstanceProfileName),
			RoleName:            awssdk.String(plan.IAMRoleName),
		}); err != nil && !isAPIError(err, "NoSuchEntity") {
			return fmt.Errorf("remove role from instance profile %s: %w", plan.InstanceProfileName, err)
		}
	}
	if plan.InstanceProfileName != "" {
		if _, err := a.IAM.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{
			InstanceProfileName: awssdk.String(plan.InstanceProfileName),
		}); err != nil && !isAPIError(err, "NoSuchEntity") {
			return fmt.Errorf("delete instance profile %s: %w", plan.InstanceProfileName, err)
		}
	}
	if plan.IAMRoleName != "" {
		if _, err := a.IAM.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   awssdk.String(plan.IAMRoleName),
			PolicyName: awssdk.String("betternat-runtime"),
		}); err != nil && !isAPIError(err, "NoSuchEntity") {
			return fmt.Errorf("delete role policy %s: %w", plan.IAMRoleName, err)
		}
		if _, err := a.IAM.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  awssdk.String(plan.IAMRoleName),
			PolicyArn: awssdk.String(ssmManagedInstanceCorePolicyARN),
		}); err != nil && !isAPIError(err, "NoSuchEntity") {
			return fmt.Errorf("detach ssm managed policy %s: %w", plan.IAMRoleName, err)
		}
		if _, err := a.IAM.DeleteRole(ctx, &iam.DeleteRoleInput{
			RoleName: awssdk.String(plan.IAMRoleName),
		}); err != nil && !isAPIError(err, "NoSuchEntity") {
			return fmt.Errorf("delete role %s: %w", plan.IAMRoleName, err)
		}
	}
	return nil
}

func (a Applier) deleteSecurityGroup(ctx context.Context, plan installplan.Plan) error {
	if plan.SecurityGroupName == "" {
		return nil
	}
	groupID, err := a.findSecurityGroup(ctx, plan)
	if err != nil {
		if strings.Contains(err.Error(), "could not be described") {
			return nil
		}
		return err
	}
	input := &ec2.DeleteSecurityGroupInput{GroupId: awssdk.String(groupID)}
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		if _, err := a.EC2.DeleteSecurityGroup(ctx, input); err != nil {
			if isAPIError(err, "InvalidGroup.NotFound") {
				return nil
			}
			if isAPIError(err, "DependencyViolation") {
				lastErr = err
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					continue
				}
			}
			return fmt.Errorf("delete security group %s: %w", groupID, err)
		}
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("delete security group %s after dependency wait: %w", groupID, lastErr)
	}
	return nil
}

func replaceRouteInput(routeTableID string, destinationCIDR string, target string) (*ec2.ReplaceRouteInput, error) {
	if routeTableID == "" {
		return nil, fmt.Errorf("route table id is required")
	}
	if destinationCIDR == "" {
		return nil, fmt.Errorf("destination cidr is required")
	}
	if target == "" || target == "unknown" {
		return nil, fmt.Errorf("concrete route target is required")
	}
	input := &ec2.ReplaceRouteInput{
		RouteTableId:         awssdk.String(routeTableID),
		DestinationCidrBlock: awssdk.String(destinationCIDR),
	}
	switch {
	case strings.HasPrefix(target, "i-"):
		input.InstanceId = awssdk.String(target)
	case strings.HasPrefix(target, "eni-"):
		input.NetworkInterfaceId = awssdk.String(target)
	case strings.HasPrefix(target, "nat-"):
		input.NatGatewayId = awssdk.String(target)
	case strings.HasPrefix(target, "tgw-"):
		input.TransitGatewayId = awssdk.String(target)
	case strings.HasPrefix(target, "pcx-"):
		input.VpcPeeringConnectionId = awssdk.String(target)
	case strings.HasPrefix(target, "eigw-"):
		input.EgressOnlyInternetGatewayId = awssdk.String(target)
	case strings.HasPrefix(target, "igw-"), strings.HasPrefix(target, "vgw-"):
		input.GatewayId = awssdk.String(target)
	default:
		return nil, fmt.Errorf("unsupported route target %q", target)
	}
	return input, nil
}

func (a Applier) describeRouteTarget(ctx context.Context, routeTableID string, destinationCIDR string) (string, error) {
	output, err := a.EC2.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
		RouteTableIds: []string{routeTableID},
	})
	if err != nil {
		return "", fmt.Errorf("describe route table: %w", err)
	}
	for _, table := range output.RouteTables {
		for _, route := range table.Routes {
			if awssdk.ToString(route.DestinationCidrBlock) != destinationCIDR {
				continue
			}
			target := routeTarget(route)
			if target == "" {
				return "", fmt.Errorf("route %s in %s has no supported target", destinationCIDR, routeTableID)
			}
			return target, nil
		}
	}
	return "", fmt.Errorf("route %s not found", destinationCIDR)
}

func routeTarget(route ec2types.Route) string {
	switch {
	case route.InstanceId != nil:
		return awssdk.ToString(route.InstanceId)
	case route.NetworkInterfaceId != nil:
		return awssdk.ToString(route.NetworkInterfaceId)
	case route.NatGatewayId != nil:
		return awssdk.ToString(route.NatGatewayId)
	case route.GatewayId != nil:
		return awssdk.ToString(route.GatewayId)
	case route.TransitGatewayId != nil:
		return awssdk.ToString(route.TransitGatewayId)
	case route.VpcPeeringConnectionId != nil:
		return awssdk.ToString(route.VpcPeeringConnectionId)
	case route.EgressOnlyInternetGatewayId != nil:
		return awssdk.ToString(route.EgressOnlyInternetGatewayId)
	default:
		return ""
	}
}

func (a Applier) ensureSecurityGroup(ctx context.Context, plan installplan.Plan) (string, error) {
	output, err := a.EC2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   awssdk.String(plan.SecurityGroupName),
		Description: awssdk.String("BetterNAT appliance security group"),
		VpcId:       awssdk.String(plan.VPCID),
	})
	if err != nil {
		if isAPIError(err, "InvalidGroup.Duplicate") {
			groupID, findErr := a.findSecurityGroup(ctx, plan)
			if findErr != nil {
				return "", findErr
			}
			if ruleErr := a.ensureSecurityGroupRules(ctx, groupID, plan); ruleErr != nil {
				return "", ruleErr
			}
			return groupID, nil
		}
		return "", fmt.Errorf("create security group %s: %w", plan.SecurityGroupName, err)
	}
	groupID := awssdk.ToString(output.GroupId)
	if groupID == "" {
		return "", fmt.Errorf("create security group %s returned empty group id", plan.SecurityGroupName)
	}
	if _, err := a.EC2.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{groupID},
		Tags:      ec2TagsWithName(plan.Tags, plan.SecurityGroupName),
	}); err != nil {
		return "", fmt.Errorf("tag security group %s: %w", groupID, err)
	}
	if err := a.ensureSecurityGroupRules(ctx, groupID, plan); err != nil {
		return "", err
	}
	return groupID, nil
}

func (a Applier) ensureSecurityGroupRules(ctx context.Context, groupID string, plan installplan.Plan) error {
	privateCIDRs := plan.PrivateCIDRs
	if len(privateCIDRs) == 0 {
		privateCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	for _, cidr := range privateCIDRs {
		if cidr == "" {
			continue
		}
		_, err := a.EC2.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
			GroupId: awssdk.String(groupID),
			IpPermissions: []ec2types.IpPermission{
				{
					IpProtocol: awssdk.String("-1"),
					IpRanges:   []ec2types.IpRange{{CidrIp: awssdk.String(cidr), Description: awssdk.String("BetterNAT private subnet traffic")}},
				},
			},
		})
		if err != nil && !isAPIError(err, "InvalidPermission.Duplicate") {
			return fmt.Errorf("authorize appliance ingress %s: %w", cidr, err)
		}
	}
	_, err := a.EC2.AuthorizeSecurityGroupEgress(ctx, &ec2.AuthorizeSecurityGroupEgressInput{
		GroupId: awssdk.String(groupID),
		IpPermissions: []ec2types.IpPermission{
			{
				IpProtocol: awssdk.String("-1"),
				IpRanges:   []ec2types.IpRange{{CidrIp: awssdk.String("0.0.0.0/0"), Description: awssdk.String("BetterNAT outbound traffic")}},
			},
		},
	})
	if err != nil && !isAPIError(err, "InvalidPermission.Duplicate") {
		return fmt.Errorf("authorize appliance egress: %w", err)
	}
	return nil
}

func (a Applier) findSecurityGroup(ctx context.Context, plan installplan.Plan) (string, error) {
	output, err := a.EC2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: awssdk.String("group-name"), Values: []string{plan.SecurityGroupName}},
			{Name: awssdk.String("vpc-id"), Values: []string{plan.VPCID}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("describe security group %s: %w", plan.SecurityGroupName, err)
	}
	if len(output.SecurityGroups) == 0 {
		return "", fmt.Errorf("security group %s already exists but could not be described", plan.SecurityGroupName)
	}
	groupID := awssdk.ToString(output.SecurityGroups[0].GroupId)
	if groupID == "" {
		return "", fmt.Errorf("security group %s has empty group id", plan.SecurityGroupName)
	}
	return groupID, nil
}

func (a Applier) createLaunchTemplate(ctx context.Context, plan installplan.Plan, pool installplan.Pool, securityGroupID string, userData string) (string, error) {
	if plan.AMIID == "" {
		return "", fmt.Errorf("ami id is required to create launch template %q", pool.LaunchTemplateName)
	}
	data := &ec2types.RequestLaunchTemplateData{
		ImageId:      awssdk.String(plan.AMIID),
		InstanceType: ec2types.InstanceType(plan.InstanceType),
		IamInstanceProfile: &ec2types.LaunchTemplateIamInstanceProfileSpecificationRequest{
			Name: awssdk.String(plan.InstanceProfileName),
		},
		MetadataOptions: &ec2types.LaunchTemplateInstanceMetadataOptionsRequest{
			HttpEndpoint:            ec2types.LaunchTemplateInstanceMetadataEndpointStateEnabled,
			HttpTokens:              ec2types.LaunchTemplateHttpTokensStateRequired,
			HttpPutResponseHopLimit: awssdk.Int32(1),
		},
		TagSpecifications: []ec2types.LaunchTemplateTagSpecificationRequest{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: append(ec2TagsWithName(plan.Tags, pool.Name),
					ec2types.Tag{Key: awssdk.String("BetterNATApplianceAZ"), Value: awssdk.String(pool.AvailabilityZone)},
					ec2types.Tag{Key: awssdk.String("BetterNATPool"), Value: awssdk.String(pool.Name)},
				),
			},
		},
	}
	networkInterface := ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest{
		AssociatePublicIpAddress: awssdk.Bool(plan.AssociatePublicIP),
		DeleteOnTermination:      awssdk.Bool(true),
		DeviceIndex:              awssdk.Int32(0),
	}
	if securityGroupID != "" {
		networkInterface.Groups = []string{securityGroupID}
	}
	data.NetworkInterfaces = []ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest{networkInterface}
	if plan.UseSpot {
		data.InstanceMarketOptions = &ec2types.LaunchTemplateInstanceMarketOptionsRequest{
			MarketType: ec2types.MarketTypeSpot,
			SpotOptions: &ec2types.LaunchTemplateSpotMarketOptionsRequest{
				SpotInstanceType: ec2types.SpotInstanceTypeOneTime,
			},
		}
	}
	if userData != "" {
		data.UserData = awssdk.String(base64.StdEncoding.EncodeToString([]byte(userData)))
	}
	output, err := a.EC2.CreateLaunchTemplate(ctx, &ec2.CreateLaunchTemplateInput{
		LaunchTemplateName: awssdk.String(pool.LaunchTemplateName),
		LaunchTemplateData: data,
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeLaunchTemplate,
				Tags:         ec2TagsWithName(plan.Tags, pool.LaunchTemplateName),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create launch template %s: %w", pool.LaunchTemplateName, err)
	}
	launchTemplateID := awssdk.ToString(output.LaunchTemplate.LaunchTemplateId)
	if launchTemplateID == "" {
		return "", fmt.Errorf("create launch template %s returned empty id", pool.LaunchTemplateName)
	}
	return launchTemplateID, nil
}

func (a Applier) createAutoScalingGroup(ctx context.Context, plan installplan.Plan, pool installplan.Pool, launchTemplateID string) error {
	input := &autoscaling.CreateAutoScalingGroupInput{
		AutoScalingGroupName: awssdk.String(pool.ASGName),
		MinSize:              awssdk.Int32(pool.MinSize),
		MaxSize:              awssdk.Int32(pool.MaxSize),
		DesiredCapacity:      awssdk.Int32(pool.DesiredCapacity),
		VPCZoneIdentifier:    awssdk.String(pool.SubnetID),
		LaunchTemplate: &astypes.LaunchTemplateSpecification{
			LaunchTemplateId: awssdk.String(launchTemplateID),
			Version:          awssdk.String("$Latest"),
		},
		Tags: autoScalingTags(plan.Tags, pool.Name),
	}
	var err error
	for attempt := 0; attempt < 12; attempt++ {
		_, err = a.AutoScaling.CreateAutoScalingGroup(ctx, input)
		if err == nil {
			return nil
		}
		if !isLaunchTemplatePropagationError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if err != nil {
		return fmt.Errorf("create asg %s: %w", pool.ASGName, err)
	}
	return nil
}

func (a Applier) putTerminationLifecycleHook(ctx context.Context, pool installplan.Pool) error {
	if pool.ASGName == "" {
		return fmt.Errorf("asg name is required")
	}
	hookName := terminationLifecycleHookName(pool)
	_, err := a.AutoScaling.PutLifecycleHook(ctx, &autoscaling.PutLifecycleHookInput{
		AutoScalingGroupName: awssdk.String(pool.ASGName),
		DefaultResult:        awssdk.String("CONTINUE"),
		HeartbeatTimeout:     awssdk.Int32(120),
		LifecycleHookName:    awssdk.String(hookName),
		LifecycleTransition:  awssdk.String("autoscaling:EC2_INSTANCE_TERMINATING"),
	})
	if err != nil {
		return fmt.Errorf("put lifecycle hook %s: %w", hookName, err)
	}
	return nil
}

func terminationLifecycleHookName(pool installplan.Pool) string {
	return pool.ASGName + "-terminating"
}

func (a Applier) waitForPoolOwner(ctx context.Context, asgName string) (string, error) {
	var lastInstanceID string
	for attempt := 0; attempt < 30; attempt++ {
		output, err := a.AutoScaling.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
			AutoScalingGroupNames: []string{asgName},
		})
		if err != nil {
			return "", fmt.Errorf("describe asg %s: %w", asgName, err)
		}
		for _, group := range output.AutoScalingGroups {
			for _, instance := range group.Instances {
				instanceID := awssdk.ToString(instance.InstanceId)
				if instanceID == "" {
					continue
				}
				lastInstanceID = instanceID
				if instance.LifecycleState == astypes.LifecycleStateInService {
					return instanceID, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if lastInstanceID != "" {
		return lastInstanceID, nil
	}
	return "", fmt.Errorf("asg %s did not produce an instance", asgName)
}

func (a Applier) launchAppliance(ctx context.Context, plan installplan.Plan, appliance installplan.Appliance, securityGroupID string, userData string) (string, error) {
	if plan.AMIID == "" {
		return "", fmt.Errorf("ami id is required to launch appliance %q", appliance.Name)
	}
	input := &ec2.RunInstancesInput{
		ImageId:      awssdk.String(plan.AMIID),
		InstanceType: ec2types.InstanceType(plan.InstanceType),
		MinCount:     awssdk.Int32(1),
		MaxCount:     awssdk.Int32(1),
		SubnetId:     awssdk.String(appliance.SubnetID),
		IamInstanceProfile: &ec2types.IamInstanceProfileSpecification{
			Name: awssdk.String(plan.InstanceProfileName),
		},
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: append(ec2TagsWithName(plan.Tags, appliance.Name),
					ec2types.Tag{Key: awssdk.String("BetterNATApplianceRole"), Value: awssdk.String(appliance.Role)},
					ec2types.Tag{Key: awssdk.String("BetterNATApplianceAZ"), Value: awssdk.String(appliance.AvailabilityZone)},
				),
			},
		},
	}
	if securityGroupID != "" {
		input.SecurityGroupIds = []string{securityGroupID}
	}
	if plan.UseSpot {
		input.InstanceMarketOptions = &ec2types.InstanceMarketOptionsRequest{
			MarketType: ec2types.MarketTypeSpot,
			SpotOptions: &ec2types.SpotMarketOptions{
				SpotInstanceType: ec2types.SpotInstanceTypeOneTime,
			},
		}
	}
	if userData != "" {
		input.UserData = awssdk.String(base64.StdEncoding.EncodeToString([]byte(userData)))
	}
	var output *ec2.RunInstancesOutput
	var err error
	for attempt := 0; attempt < 12; attempt++ {
		output, err = a.EC2.RunInstances(ctx, input)
		if err == nil {
			break
		}
		if !isInstanceProfilePropagationError(err) {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if err != nil {
		return "", fmt.Errorf("launch appliance %s: %w", appliance.Name, err)
	}
	if len(output.Instances) == 0 {
		return "", fmt.Errorf("launch appliance %s returned no instances", appliance.Name)
	}
	instanceID := awssdk.ToString(output.Instances[0].InstanceId)
	if instanceID == "" {
		return "", fmt.Errorf("launch appliance %s returned empty instance id", appliance.Name)
	}
	return instanceID, nil
}

func isInstanceProfilePropagationError(err error) bool {
	if !isAPIError(err, "InvalidParameterValue") {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "iamInstanceProfile") || strings.Contains(message, "IAM Instance Profile")
}

func isLaunchTemplatePropagationError(err error) bool {
	if !isAPIError(err, "ValidationError") && !isAPIError(err, "InvalidParameterValue") {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "iamInstanceProfile") ||
		strings.Contains(message, "IAM Instance Profile") ||
		strings.Contains(message, "valid fully-formed launch template")
}

func (a Applier) createIAM(ctx context.Context, plan installplan.Plan) error {
	if _, err := a.IAM.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 awssdk.String(plan.IAMRoleName),
		AssumeRolePolicyDocument: awssdk.String(ec2AssumeRolePolicy),
		Tags:                     iamTags(plan.Tags),
	}); err != nil {
		if !isAPIError(err, "EntityAlreadyExists") {
			return fmt.Errorf("create iam role %s: %w", plan.IAMRoleName, err)
		}
	} else {
		// Role creation succeeded; the inline runtime policy below is always reconciled.
	}
	if _, err := a.IAM.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
		RoleName:       awssdk.String(plan.IAMRoleName),
		PolicyName:     awssdk.String("betternat-runtime"),
		PolicyDocument: awssdk.String(runtimePolicy(plan.RequiredIAMActions)),
	}); err != nil {
		return fmt.Errorf("put iam role policy %s: %w", plan.IAMRoleName, err)
	}
	if _, err := a.IAM.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  awssdk.String(plan.IAMRoleName),
		PolicyArn: awssdk.String(ssmManagedInstanceCorePolicyARN),
	}); err != nil {
		return fmt.Errorf("attach ssm managed policy %s: %w", plan.IAMRoleName, err)
	}
	if _, err := a.IAM.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{
		InstanceProfileName: awssdk.String(plan.InstanceProfileName),
		Tags:                iamTags(plan.Tags),
	}); err != nil {
		if !isAPIError(err, "EntityAlreadyExists") {
			return fmt.Errorf("create instance profile %s: %w", plan.InstanceProfileName, err)
		}
	}
	if _, err := a.IAM.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: awssdk.String(plan.InstanceProfileName),
		RoleName:            awssdk.String(plan.IAMRoleName),
	}); err != nil {
		if isAPIError(err, "LimitExceeded") && strings.Contains(err.Error(), "InstanceSessionsPerInstanceProfile") {
			return nil
		}
		return fmt.Errorf("add role to instance profile %s: %w", plan.InstanceProfileName, err)
	}
	return nil
}

const ec2AssumeRolePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`

const ssmManagedInstanceCorePolicyARN = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"

func runtimePolicy(actions []string) string {
	data, _ := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   actions,
				"Resource": "*",
			},
		},
	})
	return string(data)
}

func (a Applier) createLeaseTable(ctx context.Context, plan installplan.Plan) error {
	_, err := a.DynamoDB.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: awssdk.String(plan.LeaseTableName),
		AttributeDefinitions: []ddbtypes.AttributeDefinition{
			{
				AttributeName: awssdk.String("ha_group_id"),
				AttributeType: ddbtypes.ScalarAttributeTypeS,
			},
		},
		KeySchema: []ddbtypes.KeySchemaElement{
			{
				AttributeName: awssdk.String("ha_group_id"),
				KeyType:       ddbtypes.KeyTypeHash,
			},
		},
		BillingMode: ddbtypes.BillingModePayPerRequest,
		Tags:        dynamoTags(plan.Tags),
	})
	if err != nil {
		if isAPIError(err, "ResourceInUseException") {
			return nil
		}
		return fmt.Errorf("create lease table %s: %w", plan.LeaseTableName, err)
	}
	return nil
}

func (a Applier) createCoordinationTable(ctx context.Context, plan installplan.Plan) error {
	if plan.CoordinationTableName == "" {
		return nil
	}
	_, err := a.DynamoDB.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: awssdk.String(plan.CoordinationTableName),
		AttributeDefinitions: []ddbtypes.AttributeDefinition{
			{
				AttributeName: awssdk.String("ha_group_id"),
				AttributeType: ddbtypes.ScalarAttributeTypeS,
			},
			{
				AttributeName: awssdk.String("record_id"),
				AttributeType: ddbtypes.ScalarAttributeTypeS,
			},
		},
		KeySchema: []ddbtypes.KeySchemaElement{
			{
				AttributeName: awssdk.String("ha_group_id"),
				KeyType:       ddbtypes.KeyTypeHash,
			},
			{
				AttributeName: awssdk.String("record_id"),
				KeyType:       ddbtypes.KeyTypeRange,
			},
		},
		BillingMode: ddbtypes.BillingModePayPerRequest,
		Tags:        dynamoTags(plan.Tags),
	})
	if err != nil {
		if isAPIError(err, "ResourceInUseException") {
			return nil
		}
		return fmt.Errorf("create coordination table %s: %w", plan.CoordinationTableName, err)
	}
	return nil
}

func (a Applier) disableSourceDestCheck(ctx context.Context, instanceID string) error {
	_, err := a.EC2.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:      awssdk.String(instanceID),
		SourceDestCheck: &ec2types.AttributeBooleanValue{Value: awssdk.Bool(false)},
	})
	if err != nil {
		return fmt.Errorf("disable source/dest check for %s: %w", instanceID, err)
	}
	return nil
}

func (a Applier) allocateEIP(ctx context.Context, name string, tags map[string]string) (string, string, error) {
	output, err := a.EC2.AllocateAddress(ctx, &ec2.AllocateAddressInput{
		Domain: ec2types.DomainTypeVpc,
	})
	if err != nil {
		return "", "", fmt.Errorf("allocate eip %s: %w", name, err)
	}
	allocationID := awssdk.ToString(output.AllocationId)
	if allocationID == "" {
		return "", "", fmt.Errorf("allocate eip %s returned empty allocation id", name)
	}
	if _, err := a.EC2.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{allocationID},
		Tags:      ec2TagsWithName(tags, name),
	}); err != nil {
		return "", "", fmt.Errorf("tag eip %s: %w", allocationID, err)
	}
	return allocationID, awssdk.ToString(output.PublicIp), nil
}

func (a Applier) associateEIP(ctx context.Context, allocationID string, instanceID string) error {
	if allocationID == "" {
		return fmt.Errorf("allocation id is required")
	}
	if instanceID == "" {
		return fmt.Errorf("instance id is required")
	}
	if _, err := a.EC2.AssociateAddress(ctx, &ec2.AssociateAddressInput{
		AllocationId:       awssdk.String(allocationID),
		InstanceId:         awssdk.String(instanceID),
		AllowReassociation: awssdk.Bool(true),
	}); err != nil {
		return fmt.Errorf("associate eip %s to %s: %w", allocationID, instanceID, err)
	}
	return nil
}

func ec2TagsWithName(tags map[string]string, name string) []ec2types.Tag {
	result := make([]ec2types.Tag, 0, len(tags)+1)
	for key, value := range tags {
		if key == "Name" {
			continue
		}
		result = append(result, ec2types.Tag{Key: awssdk.String(key), Value: awssdk.String(value)})
	}
	return append(result, ec2types.Tag{Key: awssdk.String("Name"), Value: awssdk.String(name)})
}

func ec2Tags(tags map[string]string) []ec2types.Tag {
	result := make([]ec2types.Tag, 0, len(tags))
	for key, value := range tags {
		result = append(result, ec2types.Tag{Key: awssdk.String(key), Value: awssdk.String(value)})
	}
	return result
}

func dynamoTags(tags map[string]string) []ddbtypes.Tag {
	result := make([]ddbtypes.Tag, 0, len(tags))
	for key, value := range tags {
		result = append(result, ddbtypes.Tag{Key: awssdk.String(key), Value: awssdk.String(value)})
	}
	return result
}

func iamTags(tags map[string]string) []iamtypes.Tag {
	result := make([]iamtypes.Tag, 0, len(tags))
	for key, value := range tags {
		result = append(result, iamtypes.Tag{Key: awssdk.String(key), Value: awssdk.String(value)})
	}
	return result
}

func autoScalingTags(tags map[string]string, name string) []astypes.Tag {
	result := make([]astypes.Tag, 0, len(tags)+1)
	for key, value := range tags {
		if key == "Name" {
			continue
		}
		result = append(result, astypes.Tag{
			Key:               awssdk.String(key),
			Value:             awssdk.String(value),
			PropagateAtLaunch: awssdk.Bool(true),
		})
	}
	return append(result, astypes.Tag{
		Key:               awssdk.String("Name"),
		Value:             awssdk.String(name),
		PropagateAtLaunch: awssdk.Bool(true),
	})
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func isAPIError(err error, code string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == code
	}
	return false
}
