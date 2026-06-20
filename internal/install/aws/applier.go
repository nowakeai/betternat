package awsinstall

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"

	"github.com/betternat/betternat/internal/installplan"
)

type EC2API interface {
	AllocateAddress(ctx context.Context, params *ec2.AllocateAddressInput, optFns ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error)
	CreateSecurityGroup(ctx context.Context, params *ec2.CreateSecurityGroupInput, optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput, optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	ModifyInstanceAttribute(ctx context.Context, params *ec2.ModifyInstanceAttributeInput, optFns ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error)
	ReplaceRoute(ctx context.Context, params *ec2.ReplaceRouteInput, optFns ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error)
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
}

type DynamoDBAPI interface {
	CreateTable(ctx context.Context, params *dynamodb.CreateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
}

type IAMAPI interface {
	CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	CreateInstanceProfile(ctx context.Context, params *iam.CreateInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error)
	AddRoleToInstanceProfile(ctx context.Context, params *iam.AddRoleToInstanceProfileInput, optFns ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error)
	PutRolePolicy(ctx context.Context, params *iam.PutRolePolicyInput, optFns ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error)
}

type Applier struct {
	EC2      EC2API
	DynamoDB DynamoDBAPI
	IAM      IAMAPI
}

type Inputs struct {
	ApplianceInstanceIDs map[string]string
	UserData             string
}

type Result struct {
	AllocatedEIPs       map[string]string `json:"allocated_eips"`
	AllocatedPublicIPs  map[string]string `json:"allocated_public_ips"`
	InitialRouteTargets map[string]string `json:"initial_route_targets"`
	LaunchedInstances   map[string]string `json:"launched_instances"`
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
	result := Result{
		AllocatedEIPs:       map[string]string{},
		AllocatedPublicIPs:  map[string]string{},
		InitialRouteTargets: map[string]string{},
		LaunchedInstances:   map[string]string{},
	}
	instanceIDs := map[string]string{}
	for name, id := range inputs.ApplianceInstanceIDs {
		instanceIDs[name] = id
	}
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
	for az, name := range plan.EIPAllocationNames {
		allocationID, publicIP, err := a.allocateEIP(ctx, name, plan.Tags)
		if err != nil {
			return Result{}, err
		}
		result.AllocatedEIPs[az] = allocationID
		result.AllocatedPublicIPs[az] = publicIP
	}
	for _, route := range plan.ManagedRoutes {
		activeName := plan.Name + "-" + route.AvailabilityZone + "-active"
		instanceID := instanceIDs[activeName]
		if instanceID == "" {
			return Result{}, fmt.Errorf("missing active instance id for %s", route.AvailabilityZone)
		}
		if _, err := a.EC2.ReplaceRoute(ctx, &ec2.ReplaceRouteInput{
			RouteTableId:         awssdk.String(route.RouteTableID),
			DestinationCidrBlock: awssdk.String(route.DestinationCIDR),
			InstanceId:           awssdk.String(instanceID),
		}); err != nil {
			return Result{}, fmt.Errorf("replace initial route %s: %w", route.RouteTableID, err)
		}
		result.InitialRouteTargets[route.RouteTableID] = instanceID
	}
	return result, nil
}

func (a Applier) ensureSecurityGroup(ctx context.Context, plan installplan.Plan) (string, error) {
	output, err := a.EC2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   awssdk.String(plan.SecurityGroupName),
		Description: awssdk.String("BetterNAT appliance security group"),
		VpcId:       awssdk.String(plan.VPCID),
	})
	if err != nil {
		if isAPIError(err, "InvalidGroup.Duplicate") {
			return a.findSecurityGroup(ctx, plan)
		}
		return "", fmt.Errorf("create security group %s: %w", plan.SecurityGroupName, err)
	}
	groupID := awssdk.ToString(output.GroupId)
	if groupID == "" {
		return "", fmt.Errorf("create security group %s returned empty group id", plan.SecurityGroupName)
	}
	tags := ec2Tags(plan.Tags)
	tags = append(tags, ec2types.Tag{Key: awssdk.String("Name"), Value: awssdk.String(plan.SecurityGroupName)})
	if _, err := a.EC2.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{groupID},
		Tags:      tags,
	}); err != nil {
		return "", fmt.Errorf("tag security group %s: %w", groupID, err)
	}
	return groupID, nil
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
				Tags: append(ec2Tags(plan.Tags),
					ec2types.Tag{Key: awssdk.String("Name"), Value: awssdk.String(appliance.Name)},
					ec2types.Tag{Key: awssdk.String("BetterNATApplianceRole"), Value: awssdk.String(appliance.Role)},
					ec2types.Tag{Key: awssdk.String("BetterNATApplianceAZ"), Value: awssdk.String(appliance.AvailabilityZone)},
				),
			},
		},
	}
	if securityGroupID != "" {
		input.SecurityGroupIds = []string{securityGroupID}
	}
	if userData != "" {
		input.UserData = awssdk.String(base64.StdEncoding.EncodeToString([]byte(userData)))
	}
	output, err := a.EC2.RunInstances(ctx, input)
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
		return fmt.Errorf("add role to instance profile %s: %w", plan.InstanceProfileName, err)
	}
	return nil
}

const ec2AssumeRolePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`

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
	ec2Tags := ec2Tags(tags)
	ec2Tags = append(ec2Tags, ec2types.Tag{Key: awssdk.String("Name"), Value: awssdk.String(name)})
	if _, err := a.EC2.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{allocationID},
		Tags:      ec2Tags,
	}); err != nil {
		return "", "", fmt.Errorf("tag eip %s: %w", allocationID, err)
	}
	return allocationID, awssdk.ToString(output.PublicIp), nil
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

func isAPIError(err error, code string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == code
	}
	return false
}
