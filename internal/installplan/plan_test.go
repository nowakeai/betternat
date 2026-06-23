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
