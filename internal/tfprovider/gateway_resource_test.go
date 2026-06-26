package tfprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/nowakeai/betternat/internal/install/aws"
	"github.com/nowakeai/betternat/internal/installplan"
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
		Tags:         mustStringMap(map[string]string{"BetterNATRunId": "bnat-test"}),
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
	if derived.Cloud.ValueString() != "aws" {
		t.Fatalf("unexpected cloud default: %s", derived.Cloud.ValueString())
	}
	if derived.AMIChannel.ValueString() != "stable" {
		t.Fatalf("unexpected ami channel: %s", derived.AMIChannel.ValueString())
	}
	if derived.BootstrapMode.ValueString() != "cloud_init" {
		t.Fatalf("unexpected bootstrap mode: %s", derived.BootstrapMode.ValueString())
	}
	if !derived.AssociatePublicIPAddress.ValueBool() {
		t.Fatal("associate_public_ip_address should default true for cloud_init")
	}
	if derived.InstanceType.ValueString() != "t3.small" {
		t.Fatalf("unexpected instance type: %s", derived.InstanceType.ValueString())
	}
	if derived.UseSpot.ValueBool() {
		t.Fatal("use_spot should default false")
	}
	if derived.RouteMode.ValueString() != "replace_route" {
		t.Fatalf("unexpected route mode: %s", derived.RouteMode.ValueString())
	}
	if derived.RouteDestinationCIDR.ValueString() != "0.0.0.0/0" {
		t.Fatalf("unexpected route destination: %s", derived.RouteDestinationCIDR.ValueString())
	}
	if derived.RouteTargetType.ValueString() != "instance" {
		t.Fatalf("unexpected route target type: %s", derived.RouteTargetType.ValueString())
	}
	if derived.HAProfile.ValueString() != "default" {
		t.Fatalf("unexpected HA profile: %s", derived.HAProfile.ValueString())
	}
	if derived.HALeaseTTLSeconds.ValueInt64() != 10 {
		t.Fatalf("unexpected HA lease TTL: %d", derived.HALeaseTTLSeconds.ValueInt64())
	}
	if derived.HARenewIntervalSeconds.ValueInt64() != 1 {
		t.Fatalf("unexpected HA renew interval: %d", derived.HARenewIntervalSeconds.ValueInt64())
	}
	if !derived.RollbackOnDestroy.ValueBool() {
		t.Fatal("rollback_on_destroy should default true")
	}
	if derived.AllowDestroyNoRollback.ValueBool() {
		t.Fatal("allow_destroy_without_rollback should default false")
	}
	if derived.LeaseTableName.ValueString() != "betternat-prod-egress-leases" {
		t.Fatalf("unexpected lease table: %s", derived.LeaseTableName.ValueString())
	}
	if derived.CoordinationTableName.ValueString() != "betternat-prod-egress-coordination" {
		t.Fatalf("unexpected coordination table: %s", derived.CoordinationTableName.ValueString())
	}
	if len(derived.PeerAPIAuthToken.ValueString()) != 64 {
		t.Fatalf("unexpected peer api auth token length: %s", derived.PeerAPIAuthToken.ValueString())
	}
	if derived.ProviderInfraRevision.ValueString() != providerInfrastructureRevision {
		t.Fatalf("unexpected provider infrastructure revision: %s", derived.ProviderInfraRevision.ValueString())
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
	if !strings.Contains(derived.UserData.ValueString(), `"peer_api":{"enabled":true`) {
		t.Fatalf("missing peer api config in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), `"auth_token":"`) {
		t.Fatalf("missing peer api auth token in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"iam_role_name":"betternat-prod-egress-agent"`) {
		t.Fatalf("missing iam role in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"coordination_table_name":"betternat-prod-egress-coordination"`) {
		t.Fatalf("missing coordination table in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"security_group_name":"betternat-prod-egress-appliance"`) {
		t.Fatalf("missing security group in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"pools"`) {
		t.Fatalf("missing pools in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"desired_capacity":2`) {
		t.Fatalf("missing default desired capacity in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"BetterNATRunId":"bnat-test"`) {
		t.Fatalf("missing user tag in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"instance_type":"t3.small"`) {
		t.Fatalf("missing instance type in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"associate_public_ip_address":true`) {
		t.Fatalf("gateway nodes should associate public IPs for bootstrap and management egress: %s", derived.InstallPlanJSON.ValueString())
	}
	if strings.Contains(derived.InstallPlanJSON.ValueString(), `"use_spot":true`) {
		t.Fatalf("use_spot should be omitted when false: %s", derived.InstallPlanJSON.ValueString())
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
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"ttl_seconds":10`) {
		t.Fatalf("agent config should use default HA TTL: %s", derived.AgentConfigJSON.ValueString())
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"renew_interval_seconds":1`) {
		t.Fatalf("agent config should use default HA renew interval: %s", derived.AgentConfigJSON.ValueString())
	}
}

func TestDeriveGatewayStateHAProfileDefault(t *testing.T) {
	plan := validGatewayPlan()
	plan.HAProfile = types.StringValue("default")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if derived.HALeaseTTLSeconds.ValueInt64() != 10 || derived.HARenewIntervalSeconds.ValueInt64() != 1 {
		t.Fatalf("unexpected fast HA timing: ttl=%d renew=%d", derived.HALeaseTTLSeconds.ValueInt64(), derived.HARenewIntervalSeconds.ValueInt64())
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"ttl_seconds":10`) {
		t.Fatalf("agent config should use default HA TTL: %s", derived.AgentConfigJSON.ValueString())
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"renew_interval_seconds":1`) {
		t.Fatalf("agent config should use default HA renew interval: %s", derived.AgentConfigJSON.ValueString())
	}
}

func TestDeriveGatewayStateHAProfileAliasesUseDefaultTiming(t *testing.T) {
	for _, profile := range []string{"stable", "balanced", "fast"} {
		t.Run(profile, func(t *testing.T) {
			plan := validGatewayPlan()
			plan.HAProfile = types.StringValue(profile)
			derived, err := DeriveGatewayState(context.Background(), &plan)
			if err != nil {
				t.Fatalf("derive gateway state: %v", err)
			}
			if derived.HAProfile.ValueString() != "default" {
				t.Fatalf("legacy profile should normalize to default, got %q", derived.HAProfile.ValueString())
			}
			if derived.HALeaseTTLSeconds.ValueInt64() != 10 || derived.HARenewIntervalSeconds.ValueInt64() != 1 {
				t.Fatalf("unexpected alias HA timing: ttl=%d renew=%d", derived.HALeaseTTLSeconds.ValueInt64(), derived.HARenewIntervalSeconds.ValueInt64())
			}
		})
	}
}

func TestDeriveGatewayStateHATimingOverrides(t *testing.T) {
	plan := validGatewayPlan()
	plan.HAProfile = types.StringValue("default")
	plan.HALeaseTTLSeconds = types.Int64Value(45)
	plan.HARenewIntervalSeconds = types.Int64Value(6)
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if derived.HALeaseTTLSeconds.ValueInt64() != 45 || derived.HARenewIntervalSeconds.ValueInt64() != 6 {
		t.Fatalf("unexpected HA timing override: ttl=%d renew=%d", derived.HALeaseTTLSeconds.ValueInt64(), derived.HARenewIntervalSeconds.ValueInt64())
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"ttl_seconds":45`) {
		t.Fatalf("agent config should use HA TTL override: %s", derived.AgentConfigJSON.ValueString())
	}
	if !strings.Contains(derived.AgentConfigJSON.ValueString(), `"renew_interval_seconds":6`) {
		t.Fatalf("agent config should use HA renew override: %s", derived.AgentConfigJSON.ValueString())
	}
}

func TestDeriveGatewayStateRejectsInvalidHATiming(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GatewayResourceModel)
	}{
		{
			name: "bad profile",
			mutate: func(plan *GatewayResourceModel) {
				plan.HAProfile = types.StringValue("unsafe")
			},
		},
		{
			name: "ttl zero",
			mutate: func(plan *GatewayResourceModel) {
				plan.HALeaseTTLSeconds = types.Int64Value(0)
			},
		},
		{
			name: "renew zero",
			mutate: func(plan *GatewayResourceModel) {
				plan.HARenewIntervalSeconds = types.Int64Value(0)
			},
		},
		{
			name: "renew equals ttl",
			mutate: func(plan *GatewayResourceModel) {
				plan.HALeaseTTLSeconds = types.Int64Value(10)
				plan.HARenewIntervalSeconds = types.Int64Value(10)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validGatewayPlan()
			tt.mutate(&plan)
			if _, err := DeriveGatewayState(context.Background(), &plan); err == nil {
				t.Fatal("expected HA timing validation error")
			}
		})
	}
}

func TestDeriveGatewayStateBinaryURLs(t *testing.T) {
	plan := validGatewayPlan()
	plan.AgentBinaryURL = types.StringValue("https://example.invalid/betternat-agent")
	plan.AgentBinarySHA256 = types.StringValue("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	plan.CLIBinaryURL = types.StringValue("https://example.invalid/betternat")
	plan.CLIBinarySHA256 = types.StringValue("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	plan.LoxiCMDBinaryURL = types.StringValue("https://example.invalid/loxicmd")
	plan.LoxiCMDBinarySHA256 = types.StringValue("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if !strings.Contains(derived.UserData.ValueString(), "https://example.invalid/betternat-agent") {
		t.Fatalf("missing agent binary url in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "https://example.invalid/loxicmd") {
		t.Fatalf("missing loxicmd binary url in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "https://example.invalid/betternat") {
		t.Fatalf("missing CLI binary url in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("missing agent checksum in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc") {
		t.Fatalf("missing CLI checksum in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") {
		t.Fatalf("missing loxicmd checksum in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"associate_public_ip_address":true`) {
		t.Fatalf("stable EIP path should still associate per-node public IPs for bootstrap and management egress: %s", derived.InstallPlanJSON.ValueString())
	}
}

func TestDeriveGatewayStatePrebakedAMIStableEgressDisablesPublicIP(t *testing.T) {
	plan := validGatewayPlan()
	plan.BootstrapMode = types.StringValue("prebaked_ami")
	plan.StableEgressIP = types.BoolValue(true)
	plan.BetterNATVersion = types.StringValue("v9.9.9")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if derived.BootstrapMode.ValueString() != "prebaked_ami" {
		t.Fatalf("unexpected bootstrap mode: %s", derived.BootstrapMode.ValueString())
	}
	if derived.AssociatePublicIPAddress.ValueBool() {
		t.Fatal("prebaked AMI with stable EIP should derive associate_public_ip_address=false")
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"associate_public_ip_address":false`) {
		t.Fatalf("prebaked AMI with stable EIP should disable per-node public IPs: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "systemctl enable --now loxilb.service") {
		t.Fatalf("prebaked AMI should start preinstalled loxilb service: %s", derived.UserData.ValueString())
	}
	if strings.Contains(derived.UserData.ValueString(), "install_package docker") {
		t.Fatalf("prebaked AMI should not install docker in user data: %s", derived.UserData.ValueString())
	}
	if strings.Contains(derived.UserData.ValueString(), "curl -fsSL") {
		t.Fatalf("prebaked AMI should not download runtime artifacts in user data: %s", derived.UserData.ValueString())
	}
}

func TestDeriveGatewayStatePrebakedAMINonStableEgressKeepsPublicIP(t *testing.T) {
	plan := validGatewayPlan()
	plan.BootstrapMode = types.StringValue("prebaked_ami")
	plan.StableEgressIP = types.BoolValue(false)
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"associate_public_ip_address":true`) {
		t.Fatalf("non-stable egress needs per-node public IPs: %s", derived.InstallPlanJSON.ValueString())
	}
}

func TestDeriveGatewayStateAssociatePublicIPOverride(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*GatewayResourceModel)
		wantPublic bool
	}{
		{
			name: "disable cloud-init public ip",
			mutate: func(plan *GatewayResourceModel) {
				plan.AssociatePublicIPAddress = types.BoolValue(false)
			},
			wantPublic: false,
		},
		{
			name: "enable prebaked stable public ip",
			mutate: func(plan *GatewayResourceModel) {
				plan.BootstrapMode = types.StringValue("prebaked_ami")
				plan.StableEgressIP = types.BoolValue(true)
				plan.AssociatePublicIPAddress = types.BoolValue(true)
			},
			wantPublic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validGatewayPlan()
			tt.mutate(&plan)
			derived, err := DeriveGatewayState(context.Background(), &plan)
			if err != nil {
				t.Fatalf("derive gateway state: %v", err)
			}
			if derived.AssociatePublicIPAddress.ValueBool() != tt.wantPublic {
				t.Fatalf("unexpected associate_public_ip_address: %v", derived.AssociatePublicIPAddress.ValueBool())
			}
			wantJSON := fmt.Sprintf(`"associate_public_ip_address":%t`, tt.wantPublic)
			if !strings.Contains(derived.InstallPlanJSON.ValueString(), wantJSON) {
				t.Fatalf("install plan should contain %s: %s", wantJSON, derived.InstallPlanJSON.ValueString())
			}
		})
	}
}

func TestDeriveGatewayStateRejectsInvalidBootstrapMode(t *testing.T) {
	plan := validGatewayPlan()
	plan.BootstrapMode = types.StringValue("magic")
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected unsupported bootstrap mode error")
	}
	if !strings.Contains(err.Error(), "unsupported bootstrap_mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeriveGatewayStateRejectsPrebakedAMIArtifactOverrides(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GatewayResourceModel)
	}{
		{
			name: "agent url",
			mutate: func(plan *GatewayResourceModel) {
				plan.AgentBinaryURL = types.StringValue("https://example.invalid/betternat-agent")
			},
		},
		{
			name: "agent checksum",
			mutate: func(plan *GatewayResourceModel) {
				plan.AgentBinarySHA256 = types.StringValue("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			},
		},
		{
			name: "cli url",
			mutate: func(plan *GatewayResourceModel) {
				plan.CLIBinaryURL = types.StringValue("https://example.invalid/betternat")
			},
		},
		{
			name: "loxicmd url",
			mutate: func(plan *GatewayResourceModel) {
				plan.LoxiCMDBinaryURL = types.StringValue("https://example.invalid/loxicmd")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validGatewayPlan()
			plan.BootstrapMode = types.StringValue("prebaked_ami")
			tt.mutate(&plan)
			_, err := DeriveGatewayState(context.Background(), &plan)
			if err == nil {
				t.Fatal("expected bootstrap artifact override error")
			}
			if !strings.Contains(err.Error(), "prebaked_ami cannot use bootstrap artifact overrides") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeriveGatewayStateBetterNATVersionDerivesArm64Artifacts(t *testing.T) {
	plan := validGatewayPlan()
	plan.BetterNATVersion = types.StringValue("v0.1.0-alpha.2")
	plan.InstanceType = types.StringValue("t4g.small")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if got := derived.AgentBinaryURL.ValueString(); got != "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.2/betternat-agent_v0.1.0-alpha.2_linux_arm64" {
		t.Fatalf("unexpected agent url: %s", got)
	}
	if got := derived.AgentBinarySHA256.ValueString(); got != "94c96e730035070f7c4aab291b30e2c14c91d980fc334c6aae28aa4199fef89c" {
		t.Fatalf("unexpected agent checksum: %s", got)
	}
	if got := derived.CLIBinaryURL.ValueString(); got != "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.2/betternat_v0.1.0-alpha.2_linux_arm64" {
		t.Fatalf("unexpected cli url: %s", got)
	}
	if got := derived.CLIBinarySHA256.ValueString(); got != "003f422c7e44aacc7ed78b3abc3b439e17e73d31b752e8b56b9d5fc5b63527e5" {
		t.Fatalf("unexpected cli checksum: %s", got)
	}
	if !strings.Contains(derived.UserData.ValueString(), "betternat-agent_v0.1.0-alpha.2_linux_arm64") {
		t.Fatalf("missing derived arm64 artifact in user data: %s", derived.UserData.ValueString())
	}
}

func TestDeriveGatewayStateBetterNATVersionDerivesAMD64Artifacts(t *testing.T) {
	plan := validGatewayPlan()
	plan.BetterNATVersion = types.StringValue("v0.1.0-alpha.2")
	plan.InstanceType = types.StringValue("t3.small")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if !strings.Contains(derived.UserData.ValueString(), "betternat-agent_v0.1.0-alpha.2_linux_amd64") {
		t.Fatalf("missing derived amd64 agent artifact in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "5c49231100870243f0f31af0703d765f79af5dc8f7248e59f7df36afd48ef5a7") {
		t.Fatalf("missing derived amd64 agent checksum in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "betternat_v0.1.0-alpha.2_linux_amd64") {
		t.Fatalf("missing derived amd64 cli artifact in user data: %s", derived.UserData.ValueString())
	}
	if !strings.Contains(derived.UserData.ValueString(), "0e671ebeb1b2a93fd88a1e2bcdb5c93de01d35313b10ce776ef6dcc49885d200") {
		t.Fatalf("missing derived amd64 cli checksum in user data: %s", derived.UserData.ValueString())
	}
}

func TestDeriveGatewayStateBetterNATVersionDerivesAlpha6Artifacts(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		arch         string
		agentSHA     string
		cliSHA       string
	}{
		{
			name:         "arm64",
			instanceType: "t4g.small",
			arch:         "arm64",
			agentSHA:     "e5ed963c523a84fb5e496b8a13358662cb80afaf228182cc8e3379741cc8b8c5",
			cliSHA:       "ff4663fa49daeb42113f015c886c77680472a4c32ad3f29122dd95a703bb4f59",
		},
		{
			name:         "amd64",
			instanceType: "t3.small",
			arch:         "amd64",
			agentSHA:     "93ff333bb50d52aca6536eadc8abe8e6f9bf1ec02c56155195f40129525dde56",
			cliSHA:       "5d5c5cf6a216cab0f12eef3c3c8163c3673f794a427b30fcfb024acd2a87fe66",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validGatewayPlan()
			plan.BetterNATVersion = types.StringValue("v0.1.0-alpha.6")
			plan.InstanceType = types.StringValue(tt.instanceType)
			derived, err := DeriveGatewayState(context.Background(), &plan)
			if err != nil {
				t.Fatalf("derive gateway state: %v", err)
			}
			wantAgentURL := "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.6/betternat-agent_v0.1.0-alpha.6_linux_" + tt.arch
			if got := derived.AgentBinaryURL.ValueString(); got != wantAgentURL {
				t.Fatalf("unexpected agent url: %s", got)
			}
			if got := derived.AgentBinarySHA256.ValueString(); got != tt.agentSHA {
				t.Fatalf("unexpected agent checksum: %s", got)
			}
			wantCLIURL := "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.6/betternat_v0.1.0-alpha.6_linux_" + tt.arch
			if got := derived.CLIBinaryURL.ValueString(); got != wantCLIURL {
				t.Fatalf("unexpected cli url: %s", got)
			}
			if got := derived.CLIBinarySHA256.ValueString(); got != tt.cliSHA {
				t.Fatalf("unexpected cli checksum: %s", got)
			}
			if !strings.Contains(derived.UserData.ValueString(), "betternat-agent_v0.1.0-alpha.6_linux_"+tt.arch) {
				t.Fatalf("missing derived alpha6 artifact in user data: %s", derived.UserData.ValueString())
			}
		})
	}
}

func TestDeriveGatewayStateBetterNATVersionDerivesGAArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		arch         string
		agentSHA     string
		cliSHA       string
	}{
		{
			name:         "arm64",
			instanceType: "t4g.small",
			arch:         "arm64",
			agentSHA:     "68ef98b9b55fb7e1eb6874331c91d5755e77d5a27ad8a6af6c0eb742bc0c0305",
			cliSHA:       "e2608e894adf30097c49ba14e0babf8a365491d5f56f3c6ea1b82b857b39ce1d",
		},
		{
			name:         "amd64",
			instanceType: "t3.small",
			arch:         "amd64",
			agentSHA:     "1443bb7c069d5674238d95ebae6656e0931df296d2067f38caa2b6fbca8970c5",
			cliSHA:       "9118b3e620a5eed0cb5e551faf5293e2b6ad2f9856cdf9d834bcdb675b959946",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validGatewayPlan()
			plan.BetterNATVersion = types.StringValue("v0.1.0")
			plan.InstanceType = types.StringValue(tt.instanceType)
			derived, err := DeriveGatewayState(context.Background(), &plan)
			if err != nil {
				t.Fatalf("derive gateway state: %v", err)
			}
			wantAgentURL := "https://github.com/nowakeai/betternat/releases/download/v0.1.0/betternat-agent_v0.1.0_linux_" + tt.arch
			if got := derived.AgentBinaryURL.ValueString(); got != wantAgentURL {
				t.Fatalf("unexpected agent url: %s", got)
			}
			if got := derived.AgentBinarySHA256.ValueString(); got != tt.agentSHA {
				t.Fatalf("unexpected agent checksum: %s", got)
			}
			wantCLIURL := "https://github.com/nowakeai/betternat/releases/download/v0.1.0/betternat_v0.1.0_linux_" + tt.arch
			if got := derived.CLIBinaryURL.ValueString(); got != wantCLIURL {
				t.Fatalf("unexpected cli url: %s", got)
			}
			if got := derived.CLIBinarySHA256.ValueString(); got != tt.cliSHA {
				t.Fatalf("unexpected cli checksum: %s", got)
			}
		})
	}
}

func TestDeriveGatewayStateBetterNATVersionAllowsExplicitArtifactOverrides(t *testing.T) {
	plan := validGatewayPlan()
	plan.BetterNATVersion = types.StringValue("v0.1.0-alpha.2")
	plan.InstanceType = types.StringValue("t4g.small")
	plan.AgentBinaryURL = types.StringValue("https://example.invalid/custom-agent")
	plan.AgentBinarySHA256 = types.StringValue("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if got := derived.AgentBinaryURL.ValueString(); got != "https://example.invalid/custom-agent" {
		t.Fatalf("explicit agent url should win, got: %s", got)
	}
	if got := derived.AgentBinarySHA256.ValueString(); got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("explicit agent checksum should win, got: %s", got)
	}
	if got := derived.CLIBinaryURL.ValueString(); got != "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.2/betternat_v0.1.0-alpha.2_linux_arm64" {
		t.Fatalf("cli url should still derive, got: %s", got)
	}
}

func TestDeriveGatewayStateRejectsUnsupportedBetterNATVersion(t *testing.T) {
	plan := validGatewayPlan()
	plan.BetterNATVersion = types.StringValue("v9.9.9")
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected unsupported betternat_version error")
	}
	if !strings.Contains(err.Error(), "unsupported betternat_version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeriveGatewayStateUseSpot(t *testing.T) {
	plan := validGatewayPlan()
	plan.UseSpot = types.BoolValue(true)
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if !derived.UseSpot.ValueBool() {
		t.Fatal("use_spot should be true")
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"use_spot":true`) {
		t.Fatalf("missing use_spot in install plan: %s", derived.InstallPlanJSON.ValueString())
	}
}

func TestDeriveGatewayStateNonStableEgressOmitsPublicIdentity(t *testing.T) {
	plan := validGatewayPlan()
	plan.StableEgressIP = types.BoolValue(false)
	derived, err := DeriveGatewayState(context.Background(), &plan)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	if strings.Contains(derived.AgentConfigJSON.ValueString(), `"mode":"shared_eip"`) {
		t.Fatalf("non-stable egress must not configure shared EIP: %s", derived.AgentConfigJSON.ValueString())
	}
	if strings.Contains(derived.InstallPlanJSON.ValueString(), `"eip_allocation_names":{"`) {
		t.Fatalf("non-stable egress must not allocate EIPs: %s", derived.InstallPlanJSON.ValueString())
	}
	if !strings.Contains(derived.InstallPlanJSON.ValueString(), `"associate_public_ip_address":true`) {
		t.Fatalf("non-stable egress should associate per-node public IPs: %s", derived.InstallPlanJSON.ValueString())
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
				AllocatedPublicIPs:   map[string]string{"us-west-2a": "203.0.113.10"},
				OwnerInstances:       map[string]string{"us-west-2a": "i-active"},
				PreviousRouteTargets: map[string]string{"rtb-private-a": "nat-old"},
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
	if publicIPs["us-west-2a"] != "203.0.113.10" {
		t.Fatalf("unexpected public ips: %#v", publicIPs)
	}
	if activeIDs["us-west-2a"] != "i-active" {
		t.Fatalf("unexpected active ids: %#v", activeIDs)
	}
	if derived.Status.ValueString() != "created" {
		t.Fatalf("unexpected status: %s", derived.Status.ValueString())
	}
	if !strings.Contains(derived.RollbackRouteTargetsJSON.ValueString(), `"target":"nat-old"`) {
		t.Fatalf("missing concrete rollback target: %s", derived.RollbackRouteTargetsJSON.ValueString())
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

func (f fakeInstaller) UpdateCapacity(context.Context, installplan.Plan) error {
	return nil
}

func (f fakeInstaller) ReconcileInfrastructure(context.Context, installplan.Plan) error {
	return nil
}

type fakeReader struct {
	result awsinstall.ReadResult
}

func (f fakeReader) Read(context.Context, installplan.Plan) (awsinstall.ReadResult, error) {
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
