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
	if plan.LeaseTableName != "betternat-prod-egress-leases" {
		t.Fatalf("unexpected lease table: %#v", plan)
	}
	if len(plan.Appliances) != 4 {
		t.Fatalf("expected 4 appliances, got %#v", plan.Appliances)
	}
	if plan.Appliances[0].SourceDestCheck {
		t.Fatalf("source/dest check should be disabled: %#v", plan.Appliances[0])
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
