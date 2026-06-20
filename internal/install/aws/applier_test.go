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
	if ec2Client.associateAddressInput == nil {
		t.Fatal("expected EIP association")
	}
	if awssdk.ToString(ec2Client.associateAddressInput.AllocationId) != "eipalloc-123" {
		t.Fatalf("unexpected EIP allocation association: %#v", ec2Client.associateAddressInput)
	}
	if awssdk.ToString(ec2Client.associateAddressInput.InstanceId) != "i-active" {
		t.Fatalf("EIP should be associated to active instance: %#v", ec2Client.associateAddressInput)
	}
	if ec2Client.associateAddressInput.AllowReassociation == nil || !*ec2Client.associateAddressInput.AllowReassociation {
		t.Fatalf("EIP association should allow reassociation: %#v", ec2Client.associateAddressInput)
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
	if result.PreviousRouteTargets["rtb-a"] != "nat-old" {
		t.Fatalf("unexpected previous route target: %#v", result)
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
		UseSpot:             true,
		IAMRoleName:         "betternat-prod-egress-agent",
		InstanceProfileName: "betternat-prod-egress-agent",
		SecurityGroupName:   "betternat-prod-egress-appliance",
		LeaseTableName:      "betternat-prod-egress-leases",
		EIPAllocationNames: map[string]string{
			"us-west-2a": "betternat-prod-egress-us-west-2a",
		},
		Appliances: []installplan.Appliance{
			{Name: "prod-egress-us-west-2a-active", AvailabilityZone: "us-west-2a", SubnetID: "subnet-public-a", Role: "active"},
			{Name: "prod-egress-us-west-2a-standby", AvailabilityZone: "us-west-2a", SubnetID: "subnet-public-a", Role: "standby"},
		},
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-a", AvailabilityZone: "us-west-2a", DestinationCIDR: "0.0.0.0/0"},
		},
		Tags: map[string]string{"ManagedBy": "betternat", "Name": "gateway-name"},
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
	if countEC2Tag(firstRun.TagSpecifications[0].Tags, "Name") != 1 {
		t.Fatalf("expected exactly one Name tag: %#v", firstRun.TagSpecifications[0].Tags)
	}
	if firstRun.InstanceMarketOptions == nil || firstRun.InstanceMarketOptions.MarketType != ec2types.MarketTypeSpot {
		t.Fatalf("expected spot market options: %#v", firstRun.InstanceMarketOptions)
	}
	if firstRun.InstanceMarketOptions.SpotOptions == nil || firstRun.InstanceMarketOptions.SpotOptions.SpotInstanceType != ec2types.SpotInstanceTypeOneTime {
		t.Fatalf("expected one-time spot options: %#v", firstRun.InstanceMarketOptions)
	}
	if awssdk.ToString(firstRun.UserData) == "" || awssdk.ToString(firstRun.UserData) == "#!/bin/bash\ntrue\n" {
		t.Fatalf("user data should be base64 encoded: %#v", firstRun.UserData)
	}
	if len(ec2Client.modifyInputs) != 2 {
		t.Fatalf("expected source/dest disabled for launched instances: %#v", ec2Client.modifyInputs)
	}
	if awssdk.ToString(ec2Client.associateAddressInput.InstanceId) != "i-launched-active" {
		t.Fatalf("EIP should point to launched active instance: %#v", ec2Client.associateAddressInput)
	}
	if awssdk.ToString(ec2Client.replaceRouteInput.InstanceId) != "i-launched-active" {
		t.Fatalf("route should point to launched active instance: %#v", ec2Client.replaceRouteInput)
	}
	if result.LaunchedInstances["prod-egress-us-west-2a-active"] != "i-launched-active" {
		t.Fatalf("unexpected launched instances: %#v", result.LaunchedInstances)
	}
}

func countEC2Tag(tags []ec2types.Tag, key string) int {
	count := 0
	for _, tag := range tags {
		if awssdk.ToString(tag.Key) == key {
			count++
		}
	}
	return count
}

func TestRestoreRoutesToNATGateway(t *testing.T) {
	ec2Client := &fakeEC2{}
	applier := Applier{EC2: ec2Client}

	err := applier.RestoreRoutes(context.Background(), []RollbackRoute{
		{RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "nat-old"},
	})
	if err != nil {
		t.Fatalf("restore routes: %v", err)
	}
	if awssdk.ToString(ec2Client.replaceRouteInput.RouteTableId) != "rtb-a" {
		t.Fatalf("unexpected route table: %#v", ec2Client.replaceRouteInput)
	}
	if awssdk.ToString(ec2Client.replaceRouteInput.NatGatewayId) != "nat-old" {
		t.Fatalf("rollback should use nat gateway target: %#v", ec2Client.replaceRouteInput)
	}
	if ec2Client.replaceRouteInput.InstanceId != nil {
		t.Fatalf("rollback should not use instance target: %#v", ec2Client.replaceRouteInput)
	}
}

func TestRestoreRoutesRejectsUnknownTarget(t *testing.T) {
	applier := Applier{EC2: &fakeEC2{}}
	err := applier.RestoreRoutes(context.Background(), []RollbackRoute{
		{RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0", Target: "unknown"},
	})
	if err == nil {
		t.Fatal("expected unknown target error")
	}
}

func TestCleanup(t *testing.T) {
	ec2Client := &fakeEC2{
		securityGroupID:  "sg-123",
		describeGroupIDs: []string{"sg-123"},
		addresses: []ec2types.Address{
			{
				AllocationId:  awssdk.String("eipalloc-123"),
				AssociationId: awssdk.String("eipassoc-123"),
			},
		},
	}
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
	}

	err := applier.Cleanup(context.Background(), plan, CleanupInputs{InstanceIDs: []string{"i-active", "i-standby", "i-active", ""}})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(ec2Client.terminateInputs) != 1 || len(ec2Client.terminateInputs[0].InstanceIds) != 2 {
		t.Fatalf("expected two unique instance ids: %#v", ec2Client.terminateInputs)
	}
	if ec2Client.describeAddressesInput == nil {
		t.Fatal("expected eip lookup")
	}
	if awssdk.ToString(ec2Client.disassociateInput.AssociationId) != "eipassoc-123" {
		t.Fatalf("expected eip disassociation: %#v", ec2Client.disassociateInput)
	}
	if awssdk.ToString(ec2Client.releaseAddressInput.AllocationId) != "eipalloc-123" {
		t.Fatalf("expected eip release: %#v", ec2Client.releaseAddressInput)
	}
	if awssdk.ToString(ddbClient.deleteTableInput.TableName) != "betternat-prod-egress-leases" {
		t.Fatalf("expected lease table delete: %#v", ddbClient.deleteTableInput)
	}
	if awssdk.ToString(iamClient.removeRoleInput.InstanceProfileName) != "betternat-prod-egress-agent" {
		t.Fatalf("expected role/profile detach: %#v", iamClient.removeRoleInput)
	}
	if awssdk.ToString(iamClient.deleteRolePolicyInput.PolicyName) != "betternat-runtime" {
		t.Fatalf("expected runtime policy delete: %#v", iamClient.deleteRolePolicyInput)
	}
	if awssdk.ToString(iamClient.deleteRoleInput.RoleName) != "betternat-prod-egress-agent" {
		t.Fatalf("expected role delete: %#v", iamClient.deleteRoleInput)
	}
	if awssdk.ToString(ec2Client.deleteSecurityGroupInput.GroupId) != "sg-123" {
		t.Fatalf("expected security group delete: %#v", ec2Client.deleteSecurityGroupInput)
	}
}

func TestRead(t *testing.T) {
	ec2Client := &fakeEC2{
		addresses: []ec2types.Address{
			{
				PublicIp:   awssdk.String("203.0.113.10"),
				InstanceId: awssdk.String("i-active"),
			},
		},
	}
	applier := Applier{EC2: ec2Client}
	result, err := applier.Read(context.Background(), installplan.Plan{
		Name: "prod-egress",
		EIPAllocationNames: map[string]string{
			"us-west-2a": "betternat-prod-egress-us-west-2a",
		},
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-a", DestinationCIDR: "0.0.0.0/0"},
		},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result.RouteTargets["rtb-a"] != "nat-old" {
		t.Fatalf("unexpected route targets: %#v", result)
	}
	if result.EgressPublicIPs["us-west-2a"] != "203.0.113.10" {
		t.Fatalf("unexpected public ips: %#v", result)
	}
	if result.PublicIdentityInstanceIDs["us-west-2a"] != "i-active" {
		t.Fatalf("unexpected identity instances: %#v", result)
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
	removeRoleInput               *iam.RemoveRoleFromInstanceProfileInput
	deleteInstanceProfileInput    *iam.DeleteInstanceProfileInput
	deleteRolePolicyInput         *iam.DeleteRolePolicyInput
	deleteRoleInput               *iam.DeleteRoleInput
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

func (f *fakeIAM) RemoveRoleFromInstanceProfile(_ context.Context, params *iam.RemoveRoleFromInstanceProfileInput, _ ...func(*iam.Options)) (*iam.RemoveRoleFromInstanceProfileOutput, error) {
	f.removeRoleInput = params
	return &iam.RemoveRoleFromInstanceProfileOutput{}, nil
}

func (f *fakeIAM) DeleteInstanceProfile(_ context.Context, params *iam.DeleteInstanceProfileInput, _ ...func(*iam.Options)) (*iam.DeleteInstanceProfileOutput, error) {
	f.deleteInstanceProfileInput = params
	return &iam.DeleteInstanceProfileOutput{}, nil
}

func (f *fakeIAM) DeleteRolePolicy(_ context.Context, params *iam.DeleteRolePolicyInput, _ ...func(*iam.Options)) (*iam.DeleteRolePolicyOutput, error) {
	f.deleteRolePolicyInput = params
	return &iam.DeleteRolePolicyOutput{}, nil
}

func (f *fakeIAM) DeleteRole(_ context.Context, params *iam.DeleteRoleInput, _ ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	f.deleteRoleInput = params
	return &iam.DeleteRoleOutput{}, nil
}

type fakeEC2 struct {
	allocationID             string
	publicIP                 string
	securityGroupID          string
	describeGroupIDs         []string
	runInstanceIDs           []string
	addresses                []ec2types.Address
	createSecurityGroupError error
	allocateInput            *ec2.AllocateAddressInput
	associateAddressInput    *ec2.AssociateAddressInput
	createSecurityGroupInput *ec2.CreateSecurityGroupInput
	deleteSecurityGroupInput *ec2.DeleteSecurityGroupInput
	createTagsInputs         []*ec2.CreateTagsInput
	describeAddressesInput   *ec2.DescribeAddressesInput
	disassociateInput        *ec2.DisassociateAddressInput
	modifyInputs             []*ec2.ModifyInstanceAttributeInput
	replaceRouteInput        *ec2.ReplaceRouteInput
	releaseAddressInput      *ec2.ReleaseAddressInput
	describeSecurityInput    *ec2.DescribeSecurityGroupsInput
	describeRouteTablesInput *ec2.DescribeRouteTablesInput
	runInputs                []*ec2.RunInstancesInput
	terminateInputs          []*ec2.TerminateInstancesInput
}

func (f *fakeEC2) AllocateAddress(_ context.Context, params *ec2.AllocateAddressInput, _ ...func(*ec2.Options)) (*ec2.AllocateAddressOutput, error) {
	f.allocateInput = params
	return &ec2.AllocateAddressOutput{AllocationId: awssdk.String(f.allocationID), PublicIp: awssdk.String(f.publicIP)}, nil
}

func (f *fakeEC2) AssociateAddress(_ context.Context, params *ec2.AssociateAddressInput, _ ...func(*ec2.Options)) (*ec2.AssociateAddressOutput, error) {
	f.associateAddressInput = params
	return &ec2.AssociateAddressOutput{}, nil
}

func (f *fakeEC2) CreateSecurityGroup(_ context.Context, params *ec2.CreateSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	f.createSecurityGroupInput = params
	return &ec2.CreateSecurityGroupOutput{GroupId: awssdk.String(f.securityGroupID)}, f.createSecurityGroupError
}

func (f *fakeEC2) CreateTags(_ context.Context, params *ec2.CreateTagsInput, _ ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error) {
	f.createTagsInputs = append(f.createTagsInputs, params)
	return &ec2.CreateTagsOutput{}, nil
}

func (f *fakeEC2) DeleteSecurityGroup(_ context.Context, params *ec2.DeleteSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error) {
	f.deleteSecurityGroupInput = params
	return &ec2.DeleteSecurityGroupOutput{}, nil
}

func (f *fakeEC2) DescribeAddresses(_ context.Context, params *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	f.describeAddressesInput = params
	return &ec2.DescribeAddressesOutput{Addresses: f.addresses}, nil
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, params *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	f.describeSecurityInput = params
	groups := make([]ec2types.SecurityGroup, 0, len(f.describeGroupIDs))
	for _, groupID := range f.describeGroupIDs {
		groups = append(groups, ec2types.SecurityGroup{GroupId: awssdk.String(groupID)})
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: groups}, nil
}

func (f *fakeEC2) DescribeRouteTables(_ context.Context, params *ec2.DescribeRouteTablesInput, _ ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error) {
	f.describeRouteTablesInput = params
	destination := "0.0.0.0/0"
	return &ec2.DescribeRouteTablesOutput{
		RouteTables: []ec2types.RouteTable{
			{
				Routes: []ec2types.Route{
					{DestinationCidrBlock: awssdk.String(destination), NatGatewayId: awssdk.String("nat-old")},
				},
			},
		},
	}, nil
}

func (f *fakeEC2) DisassociateAddress(_ context.Context, params *ec2.DisassociateAddressInput, _ ...func(*ec2.Options)) (*ec2.DisassociateAddressOutput, error) {
	f.disassociateInput = params
	return &ec2.DisassociateAddressOutput{}, nil
}

func (f *fakeEC2) ModifyInstanceAttribute(_ context.Context, params *ec2.ModifyInstanceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyInstanceAttributeOutput, error) {
	f.modifyInputs = append(f.modifyInputs, params)
	return &ec2.ModifyInstanceAttributeOutput{}, nil
}

func (f *fakeEC2) ReplaceRoute(_ context.Context, params *ec2.ReplaceRouteInput, _ ...func(*ec2.Options)) (*ec2.ReplaceRouteOutput, error) {
	f.replaceRouteInput = params
	return &ec2.ReplaceRouteOutput{}, nil
}

func (f *fakeEC2) ReleaseAddress(_ context.Context, params *ec2.ReleaseAddressInput, _ ...func(*ec2.Options)) (*ec2.ReleaseAddressOutput, error) {
	f.releaseAddressInput = params
	return &ec2.ReleaseAddressOutput{}, nil
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

func (f *fakeEC2) TerminateInstances(_ context.Context, params *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.terminateInputs = append(f.terminateInputs, params)
	return &ec2.TerminateInstancesOutput{}, nil
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
	deleteTableInput *dynamodb.DeleteTableInput
	createTableError error
}

func (f *fakeDynamoDB) CreateTable(_ context.Context, params *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.createTableInput = params
	return &dynamodb.CreateTableOutput{}, f.createTableError
}

func (f *fakeDynamoDB) DeleteTable(_ context.Context, params *dynamodb.DeleteTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error) {
	f.deleteTableInput = params
	return &dynamodb.DeleteTableOutput{}, nil
}
