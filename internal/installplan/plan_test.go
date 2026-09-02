package installplan

import "testing"

func TestBuild(t *testing.T) {
	plan, err := Build(Input{
		Name:   "prod-egress",
		Region: "us-west-2",
		VPCID:  "vpc-123",
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
			"us-west-2b": "subnet-public-b",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": []string{"rtb-a"},
			"us-west-2b": []string{"rtb-b1", "rtb-b2"},
		},
		StableEgressIP:  true,
		AgentConfigHash: "abc123",
		Tags:            map[string]string{"BetterNATGateway": "wrong", "BetterNATRunId": "bnat-test", "ManagedBy": "custom"},
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.IAMRoleName != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected iam role: %#v", plan)
	}
	if plan.InstanceProfileName != "betternat-prod-egress-agent" {
		t.Fatalf("unexpected instance profile: %#v", plan)
	}
	if plan.SecurityGroupName != "betternat-prod-egress-appliance" {
		t.Fatalf("unexpected security group: %#v", plan)
	}
	if plan.InstanceType != "t3.small" {
		t.Fatalf("unexpected instance type: %#v", plan)
	}
	if plan.UseSpot {
		t.Fatalf("use spot should default false: %#v", plan)
	}
	if !plan.AssociatePublicIP {
		t.Fatalf("associate public IP should default true for bootstrap compatibility: %#v", plan)
	}
	if plan.AMIChannel != "stable" {
		t.Fatalf("unexpected ami channel: %#v", plan)
	}
	if plan.LeaseTableName != "betternat-prod-egress-leases" {
		t.Fatalf("unexpected lease table: %#v", plan)
	}
	if plan.CoordinationTableName != "betternat-prod-egress-coordination" {
		t.Fatalf("unexpected coordination table: %#v", plan)
	}
	if len(plan.Pools) != 2 {
		t.Fatalf("expected 2 pools, got %#v", plan.Pools)
	}
	if plan.Pools[0].DesiredCapacity != 2 || plan.Pools[0].MinSize != 1 || plan.Pools[0].MaxSize != 3 {
		t.Fatalf("unexpected default capacity: %#v", plan.Pools[0])
	}
	if plan.Pools[0].ASGName != "betternat-prod-egress-us-west-2a" {
		t.Fatalf("unexpected asg name: %#v", plan.Pools[0])
	}
	if len(plan.EIPAllocationNames) != 2 {
		t.Fatalf("expected eips per az: %#v", plan.EIPAllocationNames)
	}
	if len(plan.ManagedRoutes) != 3 {
		t.Fatalf("expected 3 routes, got %#v", plan.ManagedRoutes)
	}
	if plan.Tags["BetterNATAgentConfigHash"] != "abc123" {
		t.Fatalf("missing config hash tag: %#v", plan.Tags)
	}
	if plan.Tags["BetterNATRunId"] != "bnat-test" {
		t.Fatalf("missing user tag: %#v", plan.Tags)
	}
	if plan.Tags["ManagedBy"] != "betternat" {
		t.Fatalf("missing managed tag: %#v", plan.Tags)
	}
	if plan.Tags["BetterNATGateway"] != "prod-egress" {
		t.Fatalf("managed gateway tag should not be user-overridable: %#v", plan.Tags)
	}
	if !containsString(plan.RequiredIAMActions, "ec2:ModifyInstanceAttribute") {
		t.Fatalf("runtime policy must allow agent source/dest check self-disable: %#v", plan.RequiredIAMActions)
	}
	if !containsString(plan.RequiredIAMActions, "autoscaling:CompleteLifecycleAction") {
		t.Fatalf("runtime policy must allow lifecycle hook completion: %#v", plan.RequiredIAMActions)
	}
	if !containsString(plan.RequiredIAMActions, "dynamodb:Query") {
		t.Fatalf("runtime policy must allow registry query: %#v", plan.RequiredIAMActions)
	}
}

func TestBuildUseSpot(t *testing.T) {
	plan, err := Build(Input{
		Name:   "prod-egress",
		Region: "us-west-2",
		VPCID:  "vpc-123",
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": []string{"rtb-a"},
		},
		UseSpot: true,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if !plan.UseSpot {
		t.Fatalf("use spot should be preserved: %#v", plan)
	}
}

func TestBuildScopesPoolResourcesToGeneration(t *testing.T) {
	plan, err := Build(Input{
		Name:           "prod-egress",
		GenerationID:   "a1b2c3d4e5f6",
		Region:         "us-west-2",
		VPCID:          "vpc-123",
		StableEgressIP: true,
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": {"rtb-a"},
		},
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.Pools[0].ASGName != "betternat-prod-egress-us-west-2a-a1b2c3d4e5f6" {
		t.Fatalf("unexpected generated ASG name: %s", plan.Pools[0].ASGName)
	}
	if plan.Pools[0].LaunchTemplateName != "betternat-prod-egress-us-west-2a-a1b2c3d4e5f6" {
		t.Fatalf("unexpected generated launch template name: %s", plan.Pools[0].LaunchTemplateName)
	}
	if plan.Tags["BetterNATGeneration"] != "a1b2c3d4e5f6" {
		t.Fatalf("missing generation tag: %#v", plan.Tags)
	}
	if plan.IAMRoleName != "betternat-prod-egress-agent" || plan.EIPAllocationNames["us-west-2a"] != "betternat-prod-egress-us-west-2a" {
		t.Fatalf("shared resource names must remain stable: %#v", plan)
	}
}

func TestBuildUsesExternalEIPAllocationIDsPerAZ(t *testing.T) {
	plan, err := Build(Input{
		Name:           "prod-egress",
		Region:         "us-west-2",
		VPCID:          "vpc-123",
		StableEgressIP: true,
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
			"us-west-2b": "subnet-public-b",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": {"rtb-a"},
			"us-west-2b": {"rtb-b"},
		},
		ExternalEIPAllocationIDs: map[string]string{"us-west-2a": "eipalloc-external"},
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.ExternalEIPAllocationIDs["us-west-2a"] != "eipalloc-external" {
		t.Fatalf("missing external allocation: %#v", plan.ExternalEIPAllocationIDs)
	}
	if _, managed := plan.EIPAllocationNames["us-west-2a"]; managed {
		t.Fatalf("external allocation must not also be provider managed: %#v", plan.EIPAllocationNames)
	}
	if plan.EIPAllocationNames["us-west-2b"] == "" {
		t.Fatalf("omitted zone should retain provider-managed behavior: %#v", plan.EIPAllocationNames)
	}
}

func TestBuildRejectsExternalEIPWithoutStableEgress(t *testing.T) {
	_, err := Build(Input{
		Name:                     "prod-egress",
		Region:                   "us-west-2",
		VPCID:                    "vpc-123",
		StableEgressIP:           false,
		PublicSubnetIDs:          map[string]string{"us-west-2a": "subnet-public-a"},
		PrivateRouteTableIDs:     map[string][]string{"us-west-2a": {"rtb-a"}},
		ExternalEIPAllocationIDs: map[string]string{"us-west-2a": "eipalloc-external"},
	})
	if err == nil {
		t.Fatal("expected stable egress validation error")
	}
}

func TestBuildRejectsExternalEIPAssignedToMultipleAZs(t *testing.T) {
	_, err := Build(Input{
		Name:           "prod-egress",
		Region:         "us-west-2",
		VPCID:          "vpc-123",
		StableEgressIP: true,
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
			"us-west-2b": "subnet-public-b",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": {"rtb-a"},
			"us-west-2b": {"rtb-b"},
		},
		ExternalEIPAllocationIDs: map[string]string{
			"us-west-2a": "eipalloc-shared",
			"us-west-2b": "eipalloc-shared",
		},
	})
	if err == nil {
		t.Fatal("expected duplicate external EIP validation error")
	}
}

func TestBuildCanDisableAssociatedPublicIP(t *testing.T) {
	associatePublicIP := false
	plan, err := Build(Input{
		Name:   "prod-egress",
		Region: "us-west-2",
		VPCID:  "vpc-123",
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": []string{"rtb-a"},
		},
		AssociatePublicIP: &associatePublicIP,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.AssociatePublicIP {
		t.Fatalf("associate public IP should be disabled: %#v", plan)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestBuildCustomRouteDestination(t *testing.T) {
	plan, err := Build(Input{
		Name:   "prod-egress",
		Region: "us-west-2",
		VPCID:  "vpc-123",
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
		},
		PrivateRouteTableIDs: map[string][]string{
			"us-west-2a": []string{"rtb-a"},
		},
		RouteDestinationCIDR: "10.20.0.0/16",
		RouteTargetType:      "instance",
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.ManagedRoutes[0].DestinationCIDR != "10.20.0.0/16" {
		t.Fatalf("unexpected route destination: %#v", plan.ManagedRoutes)
	}
}

func TestBuildRejectsMissingRouteTablesForAZ(t *testing.T) {
	_, err := Build(Input{
		Name:   "prod-egress",
		Region: "us-west-2",
		VPCID:  "vpc-123",
		PublicSubnetIDs: map[string]string{
			"us-west-2a": "subnet-public-a",
		},
		PrivateRouteTableIDs: map[string][]string{},
	})
	if err == nil {
		t.Fatal("expected route table error")
	}
}
