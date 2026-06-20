package awsinstall

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go"

	"github.com/betternat/betternat/internal/installplan"
)

func TestApply(t *testing.T) {
	ec2Client := &fakeEC2{allocationID: "eipalloc-123", publicIP: "203.0.113.10", securityGroupID: "sg-123"}
	ddbClient := &fakeDynamoDB{}
	iamClient := &fakeIAM{}
	applier := Applier{EC2: ec2Client, DynamoDB: ddbClient, IAM: iamClient}
	plan := installplan.Plan{
		Name:                "prod-egress",
		VPCID:               "vpc-123",
		IAMRoleName:         "betternat-prod-egress-agent",
		InstanceProfileName: "betternat-prod-egress-agent",
		SecurityGroupName:   "betternat-prod-egress-appliance",
		LeaseTableName:      "betternat-prod-egress-leases",
		EIPAllocationNames: map[string]string{
			"us-west-2a": "betternat-prod-egress-us-west-2a",
		},
		Appliances: []installplan.Appliance{
			{Name: "prod-egress-us-west-2a-active", AvailabilityZone: "us-west-2a", Role: "active"},
			{Name: "prod-egress-us-west-2a-standby", AvailabilityZone: "us-west-2a", Role: "standby"},
		},
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-a", AvailabilityZone: "us-west-2a", DestinationCIDR: "0.0.0.0/0"},
		},
		RequiredIAMActions: []string{"ec2:ReplaceRoute", "dynamodb:UpdateItem"},
		Tags:               map[string]string{"ManagedBy": "betternat"},
	}

	result, err := applier.Apply(context.Background(), plan, Inputs{
		ApplianceInstanceIDs: map[string]string{
			"prod-egress-us-west-2a-active":  "i-active",
			"prod-egress-us-west-2a-standby": "i-standby",
		},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if awssdk.ToString(ddbClient.createTableInput.TableName) != "betternat-prod-egress-leases" {
		t.Fatalf("unexpected table: %#v", ddbClient.createTableInput)
	}
	if awssdk.ToString(ec2Client.createSecurityGroupInput.GroupName) != "betternat-prod-egress-appliance" {
		t.Fatalf("unexpected security group: %#v", ec2Client.createSecurityGroupInput)
	}
	if awssdk.ToString(ec2Client.createSecurityGroupInput.VpcId) != "vpc-123" {
		t.Fatalf("unexpected security group vpc: %#v", ec2Client.createSecurityGroupInput)
	}
	if awssdk.ToString(iamClient.createRoleInput.RoleName) != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected role: %#v", iamClient.createRoleInput)
	}
	if awssdk.ToString(iamClient.createInstanceProfileInput.InstanceProfileName) != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected instance profile: %#v", iamClient.createInstanceProfileInput)
	}
	if awssdk.ToString(iamClient.putRolePolicyInput.PolicyName) != "betternat-runtime" {
		t.Fatalf("unexpected role policy: %#v", iamClient.putRolePolicyInput)
	}
	if awssdk.ToString(iamClient.addRoleToInstanceProfileInput.RoleName) != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected add role call: %#v", iamClient.addRoleToInstanceProfileInput)
	}
	if len(ec2Client.modifyInputs) != 2 {
		t.Fatalf("expected source/dest check disabled for 2 instances: %#v", ec2Client.modifyInputs)
	}
	for _, input := range ec2Client.modifyInputs {
		if input.SourceDestCheck == nil || *input.SourceDestCheck.Value {
			t.Fatalf("source/dest check not disabled: %#v", input)
		}
	}
	if ec2Client.allocateInput == nil {
		t.Fatal("expected EIP allocation")
	}
	if !ec2Client.hasTaggedResource("sg-123") {
		t.Fatalf("security group was not tagged: %#v", ec2Client.createTagsInputs)
	}
	if !ec2Client.hasTaggedResource("eipalloc-123") {
		t.Fatalf("eip was not tagged: %#v", ec2Client.createTagsInputs)
	}
	if awssdk.ToString(ec2Client.replaceRouteInput.InstanceId) != "i-active" {
		t.Fatalf("route should point to active instance: %#v", ec2Client.replaceRouteInput)
	}
	if result.AllocatedEIPs["us-west-2a"] != "eipalloc-123" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.AllocatedPublicIPs["us-west-2a"] != "203.0.113.10" {
		t.Fatalf("unexpected public ip result: %#v", result)
	}
	if result.InitialRouteTargets["rtb-a"] != "i-active" {
		t.Fatalf("unexpected route result: %#v", result)
	}
	if len(result.LaunchedInstances) != 0 {
		t.Fatalf("should not launch provided instances: %#v", result)
	}
}

func TestApplyRequiresAMIIDToLaunchMissingInstances(t *testing.T) {
	applier := Applier{EC2: &fakeEC2{securityGroupID: "sg-123"}, DynamoDB: &fakeDynamoDB{}, IAM: &fakeIAM{}}
	_, err := applier.Apply(context.Background(), installplan.Plan{
		LeaseTableName: "leases",
		Appliances: []installplan.Appliance{
			{Name: "appliance-a"},
		},
	}, Inputs{})
	if err == nil {
		t.Fatal("expected ami id error")
	}
}

func TestApplyLaunchesMissingApplianceInstances(t *testing.T) {
	ec2Client := &fakeEC2{
		allocationID:     "eipalloc-123",
		publicIP:         "203.0.113.10",
		securityGroupID:  "sg-123",
		runInstanceIDs:   []string{"i-launched-active", "i-launched-standby"},
		describeGroupIDs: []string{"sg-123"},
	}
	applier := Applier{EC2: ec2Client, DynamoDB: &fakeDynamoDB{}, IAM: &fakeIAM{}}
	plan := installplan.Plan{
		Name:                "prod-egress",
		VPCID:               "vpc-123",
		AMIID:               "ami-123",
		InstanceType:        "t3.small",
		IAMRoleName:         "betternat-prod-egress-agent",
		InstanceProfileName: "betternat-prod-egress-agent",
		SecurityGroupName:   "betternat-prod-egress-appliance",
		LeaseTableName:      "betternat-prod-egress-leases",
		Appliances: []installplan.Appliance{
			{Name: "prod-egress-us-west-2a-active", AvailabilityZone: "us-west-2a", SubnetID: "subnet-public-a", Role: "active"},
			{Name: "prod-egress-us-west-2a-standby", AvailabilityZone: "us-west-2a", SubnetID: "subnet-public-a", Role: "standby"},
		},
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-a", AvailabilityZone: "us-west-2a", DestinationCIDR: "0.0.0.0/0"},
		},
		Tags: map[string]string{"ManagedBy": "betternat"},
	}

	result, err := applier.Apply(context.Background(), plan, Inputs{UserData: "#!/bin/bash\ntrue\n"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(ec2Client.runInputs) != 2 {
		t.Fatalf("expected 2 run instances calls: %#v", ec2Client.runInputs)
	}
	firstRun := ec2Client.runInputs[0]
	if awssdk.ToString(firstRun.ImageId) != "ami-123" {
		t.Fatalf("unexpected ami: %#v", firstRun)
	}
	if firstRun.InstanceType != ec2types.InstanceTypeT3Small {
		t.Fatalf("unexpected instance type: %#v", firstRun)
	}
	if awssdk.ToString(firstRun.SubnetId) != "subnet-public-a" {
		t.Fatalf("unexpected subnet: %#v", firstRun)
	}
	if awssdk.ToString(firstRun.IamInstanceProfile.Name) != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected instance profile: %#v", firstRun.IamInstanceProfile)
	}
	if len(firstRun.SecurityGroupIds) != 1 || firstRun.SecurityGroupIds[0] != "sg-123" {
		t.Fatalf("unexpected security groups: %#v", firstRun.SecurityGroupIds)
	}
	if awssdk.ToString(firstRun.UserData) == "" || awssdk.ToString(firstRun.UserData) == "#!/bin/bash\ntrue\n" {
		t.Fatalf("user data should be base64 encoded: %#v", firstRun.UserData)
	}
	if len(ec2Client.modifyInputs) != 2 {
		t.Fatalf("expected source/dest disabled for launched instances: %#v", ec2Client.modifyInputs)
	}
	if awssdk.ToString(ec2Client.replaceRouteInput.InstanceId) != "i-launched-active" {
		t.Fatalf("route should point to launched active instance: %#v", ec2Client.replaceRouteInput)
	}
	if result.LaunchedInstances["prod-egress-us-west-2a-active"] != "i-launched-active" {
		t.Fatalf("unexpected launched instances: %#v", result.LaunchedInstances)
	}
}

func TestApplyTreatsExistingResourcesAsIdempotent(t *testing.T) {
	ec2Client := &fakeEC2{
		allocationID:             "eipalloc-123",
		createSecurityGroupError: &smithy.GenericAPIError{Code: "InvalidGroup.Duplicate", Message: "exists"},
		describeGroupIDs:         []string{"sg-123"},
	}
	ddbClient := &fakeDynamoDB{createTableError: &smithy.GenericAPIError{Code: "ResourceInUseException", Message: "exists"}}
	iamClient := &fakeIAM{
		createRoleError:            &smithy.GenericAPIError{Code: "EntityAlreadyExists", Message: "exists"},
		createInstanceProfileError: &smithy.GenericAPIError{Code: "EntityAlreadyExists", Message: "exists"},
	}
	applier := Applier{EC2: ec2Client, DynamoDB: ddbClient, IAM: iamClient}

	_, err := applier.Apply(context.Background(), installplan.Plan{
		Name:                "prod-egress",
		VPCID:               "vpc-123",
		IAMRoleName:         "betternat-prod-egress-agent",
		InstanceProfileName: "betternat-prod-egress-agent",
		SecurityGroupName:   "betternat-prod-egress-appliance",
		LeaseTableName:      "betternat-prod-egress-leases",
		Appliances: []installplan.Appliance{
			{Name: "prod-egress-us-west-2a-active", AvailabilityZone: "us-west-2a"},
		},
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-a", AvailabilityZone: "us-west-2a", DestinationCIDR: "0.0.0.0/0"},
		},
	}, Inputs{
		ApplianceInstanceIDs: map[string]string{"prod-egress-us-west-2a-active": "i-active"},
	})
	if err != nil {
		t.Fatalf("apply should ignore already-existing resources: %v", err)
	}
	if len(ec2Client.modifyInputs) != 1 {
		t.Fatalf("expected apply to continue after duplicate sg: %#v", ec2Client.modifyInputs)
	}
	if ec2Client.replaceRouteInput == nil {
		t.Fatal("expected apply to continue to route replacement")
	}
}

type fakeIAM struct {
	createRoleInput               *iam.CreateRoleInput
	createInstanceProfileInput    *iam.CreateInstanceProfileInput
	addRoleToInstanceProfileInput *iam.AddRoleToInstanceProfileInput
	putRolePolicyInput            *iam.PutRolePolicyInput
	createRoleError               error
	createInstanceProfileError    error
}

func (f *fakeIAM) CreateRole(_ context.Context, params *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	f.createRoleInput = params
	return &iam.CreateRoleOutput{}, f.createRoleError
}

func (f *fakeIAM) CreateInstanceProfile(_ context.Context, params *iam.CreateInstanceProfileInput, _ ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error) {
	f.createInstanceProfileInput = params
	return &iam.CreateInstanceProfileOutput{}, f.createInstanceProfileError
}

func (f *fakeIAM) AddRoleToInstanceProfile(_ context.Context, params *iam.AddRoleToInstanceProfileInput, _ ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error) {
	f.addRoleToInstanceProfileInput = params
	return &iam.AddRoleToInstanceProfileOutput{}, nil
}

func (f *fakeIAM) PutRolePolicy(_ context.Context, params *iam.PutRolePolicyInput, _ ...func(*iam.Options)) (*iam.PutRolePolicyOutput, error) {
	f.putRolePolicyInput = params
	return &iam.PutRolePolicyOutput{}, nil
}

type fakeEC2 struct {
	allocationID             string
	publicIP                 string
	securityGroupID          string
	describeGroupIDs         []string
	runInstanceIDs           []string
	createSecurityGroupError error
	allocateInput            *ec2.AllocateAddressInput
	createSecurityGroupInput *ec2.CreateSecurityGroupInput
	createTagsInputs         []*ec2.CreateTagsInput
	modifyInputs             []*ec2.ModifyInstanceAttributeInput
	replaceRouteInput        *ec2.ReplaceRouteInput
	describeSecurityInput    *ec2.DescribeSecurityGroupsInput
	runInputs                []*ec2.RunInstancesInput
}

func (f *fakeEC2) AllocateAddress(_ context.Context, params *ec2.AllocateAddressInput, _ ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	f.allocateInput = params
	return &ec2.AllocateAddressOutput{AllocationId: awssdk.String(f.allocationID), PublicIp: awssdk.String(f.publicIP)}, nil
}

func (f *fakeEC2) CreateSecurityGroup(_ context.Context, params *ec2.CreateSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	f.createSecurityGroupInput = params
	return &ec2.CreateSecurityGroupOutput{GroupId: awssdk.String(f.securityGroupID)}, f.createSecurityGroupError
}

func (f *fakeEC2) CreateTags(_ context.Context, params *ec2.CreateTagsInput, _ ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	f.createTagsInputs = append(f.createTagsInputs, params)
	return &ec2.CreateTagsOutput{}, nil
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, params *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	f.describeSecurityInput = params
	groups := make([]ec2types.SecurityGroup, 0, len(f.describeGroupIDs))
	for _, groupID := range f.describeGroupIDs {
		groups = append(groups, ec2types.SecurityGroup{GroupId: awssdk.String(groupID)})
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: groups}, nil
}

func (f *fakeEC2) ModifyInstanceAttribute(_ context.Context, params *ec2.ModifyInstanceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error) {
	f.modifyInputs = append(f.modifyInputs, params)
	return &ec2.ModifyInstanceAttributeOutput{}, nil
}

func (f *fakeEC2) ReplaceRoute(_ context.Context, params *ec2.ReplaceRouteInput, _ ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error) {
	f.replaceRouteInput = params
	return &ec2.ReplaceRouteOutput{}, nil
}

func (f *fakeEC2) RunInstances(_ context.Context, params *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	f.runInputs = append(f.runInputs, params)
	instanceID := "i-launched"
	if len(f.runInstanceIDs) > 0 {
		instanceID = f.runInstanceIDs[0]
		f.runInstanceIDs = f.runInstanceIDs[1:]
	}
	return &ec2.RunInstancesOutput{
		Instances: []ec2types.Instance{{InstanceId: awssdk.String(instanceID)}},
	}, nil
}

func (f *fakeEC2) hasTaggedResource(resourceID string) bool {
	for _, input := range f.createTagsInputs {
		for _, resource := range input.Resources {
			if resource == resourceID {
				return true
			}
		}
	}
	return false
}

type fakeDynamoDB struct {
	createTableInput *dynamodb.CreateTableInput
	createTableError error
}

func (f *fakeDynamoDB) CreateTable(_ context.Context, params *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.createTableInput = params
	return &dynamodb.CreateTableOutput{}, f.createTableError
}
