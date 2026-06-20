package tfprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/betternat/betternat/internal/install/aws"
	"github.com/betternat/betternat/internal/installplan"
)

func TestDeriveGatewayState(t *testing.T) {
	plan := GatewayResourceModel{
		Name:   types.StringValue("prod-egress"),
		Cloud:  types.StringValue("aws"),
		Region: types.StringValue("us-west-2"),
		VPCID:  types.StringValue("vpc-123"),
		PublicSubnetIDs: mustStringMap(map[string]string{
			"us-west-2a": "subnet-public-a",
			"us-west-2b": "subnet-public-b",
		}),
		PrivateRouteTableIDs: mustMapList(map[string][]string{
			"us-west-2a": []string{"rtb-private-a"},
			"us-west-2b": []string{"rtb-private-b"},
		}),
		PrivateCIDRs: mustStringList([]string{"10.0.0.0/8"}),
	}

	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if derived.ID.ValueString() != "prod-egress" {
		t.Fatalf("unexpected id: %s", derived.ID.ValueString())
	}
	if derived.DatapathEngine.ValueString() != "loxilb" {
		t.Fatalf("unexpected datapath engine: %s", derived.DatapathEngine.ValueString())
	}
	if derived.InstanceType.ValueString() != "t3.small" {
		t.Fatalf("unexpected instance type: %s", derived.InstanceType.ValueString())
	}
	if derived.LeaseTableName.ValueString() != "betternat-prod-egress-leases" {
		t.Fatalf("unexpected lease table: %s", derived.LeaseTableName.ValueString())
	}
	if len(derived.AgentConfigHash.ValueString()) != 64 {
		t.Fatalf("unexpected config hash: %s", derived.AgentConfigHash.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "betternat-agent.service") {
		t.Fatalf("missing agent service in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), `"gateway_id":"prod-egress"`) {
		t.Fatalf("missing agent config in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"iam_role_name":"betternat-prod-egress-agent"`) {
		t.Fatalf("missing iam role in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"security_group_name":"betternat-prod-egress-appliance"`) {
		t.Fatalf("missing security group in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"role":"standby"`) {
		t.Fatalf("missing standby appliance in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"instance_type":"t3.small"`) {
		t.Fatalf("missing instance type in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if derived.Status.ValueString() != "planned" {
		t.Fatalf("unexpected status: %s", derived.Status.ValueString())
	}
	if !strings.Contains(derived.RollbackRouteTargetsJSON.ValueString(), `"rtb-private-a"`) {
		t.Fatalf("missing rollback route slot: %s", derived.RollbackRouteTargetsJSON.ValueString())
	}
	if !strings.Contains(derived.RollbackRouteTargetsJSON.ValueString(), `"destination_cidr":"0.0.0.0/0"`) {
		t.Fatalf("missing rollback destination: %s", derived.RollbackRouteTargetsJSON.ValueString())
	}

	var agentConfig map[string]any
	if err := json.Unmarshal([]byte(derived.AgentConfigJSON.ValueString()), &agentConfig); err != nil {
		t.Fatalf("agent config is not json: %v", err)
	}
	if agentConfig["gateway_id"] != "prod-egress" {
		t.Fatalf("unexpected agent config: %#v", agentConfig)
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"route_table_ids":["rtb-private-a"]`) {
		t.Fatalf("agent config should use first AZ route table: %s", derived.AgentConfigJSON.ValueString())
	}
}

func TestDeriveGatewayStateRequiresRoutes(t *testing.T) {
	plan := GatewayResourceModel{
		Name:                 types.StringValue("prod-egress"),
		Cloud:                types.StringValue("aws"),
		Region:               types.StringValue("us-west-2"),
		PublicSubnetIDs:      mustStringMap(map[string]string{"us-west-2a": "subnet-public-a"}),
		PrivateRouteTableIDs: mustMapList(map[string][]string{}),
		PrivateCIDRs:         mustStringList([]string{"10.0.0.0/8"}),
	}

	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected route table validation error")
	}
}

func TestInstallGatewayStateUpdatesCreatedState(t *testing.T) {
	plan := validGatewayPlan()
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	factory := func(context.Context, string) (Installer, error) {
		return fakeInstaller{
			result: awsinstall.Result{
				AllocatedPublicIPs: map[string]string{"us-west-2a": "203.0.113.10"},
				LaunchedInstances: map[string]string{
					"prod-egress-us-west-2a-active":  "i-active",
					"prod-egress-us-west-2a-standby": "i-standby",
				},
			},
		}, nil
	}

	if err := installGatewayState(context.Background(), &derived, factory); err != nil {
		t.Fatalf("install gateway state: %v", err)
	}
	publicIPs, err := mapStrings(context.Background(), derived.EgressPublicIPs)
	if err != nil {
		t.Fatalf("public ips map: %v", err)
	}
	activeIDs, err := mapStrings(context.Background(), derived.ActiveInstanceIDs)
	if err != nil {
		t.Fatalf("active ids map: %v", err)
	}
	standbyIDs, err := mapStrings(context.Background(), derived.StandbyInstanceIDs)
	if err != nil {
		t.Fatalf("standby ids map: %v", err)
	}
	if publicIPs["us-west-2a"] != "203.0.113.10" {
		t.Fatalf("unexpected public ips: %#v", publicIPs)
	}
	if activeIDs["us-west-2a"] != "i-active" {
		t.Fatalf("unexpected active ids: %#v", activeIDs)
	}
	if standbyIDs["us-west-2a"] != "i-standby" {
		t.Fatalf("unexpected standby ids: %#v", standbyIDs)
	}
	if derived.Status.ValueString() != "created" {
		t.Fatalf("unexpected status: %s", derived.Status.ValueString())
	}
}

func TestDeriveGatewayStateRejectsUnsupportedCloud(t *testing.T) {
	plan := validGatewayPlan()
	plan.Cloud = types.StringValue("gcp")
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected unsupported cloud error")
	}
}

func TestDeriveGatewayStateRequiresMatchingPublicSubnetAZ(t *testing.T) {
	plan := validGatewayPlan()
	plan.PrivateRouteTableIDs = mustMapList(map[string][]string{
		"us-west-2c": []string{"rtb-private-c"},
	})
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected matching public subnet AZ error")
	}
}

func validGatewayPlan() GatewayResourceModel {
	return GatewayResourceModel{
		Name:   types.StringValue("prod-egress"),
		Cloud:  types.StringValue("aws"),
		Region: types.StringValue("us-west-2"),
		VPCID:  types.StringValue("vpc-123"),
		PublicSubnetIDs: mustStringMap(map[string]string{
			"us-west-2a": "subnet-public-a",
		}),
		PrivateRouteTableIDs: mustMapList(map[string][]string{
			"us-west-2a": []string{"rtb-private-a"},
		}),
		PrivateCIDRs: mustStringList([]string{"10.0.0.0/8"}),
	}
}

type fakeInstaller struct {
	result awsinstall.Result
}

func (f fakeInstaller) Install(context.Context, installplan.Plan, awsinstall.Inputs) (awsinstall.Result, error) {
	return f.result, nil
}

func mustMapList(values map[string][]string) types.Map {
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = mustStringList(value)
	}
	result, diags := types.MapValue(types.ListType{ElemType: types.StringType}, elements)
	if diags.HasError() {
		panic(diags.Errors()[0].Detail())
	}
	return result
}
