package tfprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nowakeai/betternat/internal/config"
)

func TestGCPInputsDefaultToForwardingStartupScript(t *testing.T) {
	model := baseGCPGatewayModel()
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	if err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs); err != nil {
		t.Fatalf("enrich bootstrap: %v", err)
	}
	applyGCPComputedPlan(&model, inputs)

	if model.EnableAgentHA.ValueBool() {
		t.Fatal("agent HA should default to disabled")
	}
	if model.AgentConfigHash.ValueString() != "" || model.AgentConfigJSON.ValueString() != "" {
		t.Fatalf("default forwarding path should not render agent config: hash=%q json=%q", model.AgentConfigHash.ValueString(), model.AgentConfigJSON.ValueString())
	}
	if model.ServiceAccountEmail.ValueString() != "" {
		t.Fatalf("default forwarding path should not require service account: %q", model.ServiceAccountEmail.ValueString())
	}
	perms, err := listStrings(context.Background(), model.RuntimeIAMPermissions)
	if err != nil {
		t.Fatalf("runtime iam permissions: %v", err)
	}
	if !stringSliceContains(perms, "compute.routes.create") || !stringSliceContains(perms, "datastore.entities.update") {
		t.Fatalf("runtime iam permissions missing route/firestore actions: %#v", perms)
	}
	if !strings.Contains(model.StartupScript.ValueString(), "nft add rule ip nat postrouting ip saddr 10.91.0.0/24 masquerade") {
		t.Fatalf("default path should keep nftables forwarding startup script: %s", model.StartupScript.ValueString())
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestGCPAgentHABootstrapRendersConfigAndUserData(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.BetterNATVersion = types.StringValue("v0.1.0")
	model.FirestoreDatabaseID = types.StringValue("(default)")
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	if err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs); err != nil {
		t.Fatalf("enrich bootstrap: %v", err)
	}
	applyGCPComputedPlan(&model, inputs)

	if len(model.AgentConfigHash.ValueString()) != 64 {
		t.Fatalf("unexpected agent config hash: %q", model.AgentConfigHash.ValueString())
	}
	if !strings.Contains(model.StartupScript.ValueString(), "betternat-agent.service") {
		t.Fatalf("agent HA startup script should install agent service: %s", model.StartupScript.ValueString())
	}
	if !strings.Contains(model.StartupScript.ValueString(), "docker.io") {
		t.Fatalf("GCP Debian bootstrap should use docker.io fallback: %s", model.StartupScript.ValueString())
	}
	if !strings.Contains(model.AgentBinaryURL.ValueString(), "betternat-agent_v0.1.0_linux_amd64") {
		t.Fatalf("unexpected derived agent artifact: %s", model.AgentBinaryURL.ValueString())
	}
	if inputs.ServiceAccountEmail != "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("service account should be passed to GCE inputs: %s", inputs.ServiceAccountEmail)
	}
	if model.AgentBinarySHA256.ValueString() == "" || model.CLIBinarySHA256.ValueString() == "" {
		t.Fatalf("expected derived artifact checksums: agent=%q cli=%q", model.AgentBinarySHA256.ValueString(), model.CLIBinarySHA256.ValueString())
	}

	var cfg config.Config
	if err := json.Unmarshal([]byte(model.AgentConfigJSON.ValueString()), &cfg); err != nil {
		t.Fatalf("unmarshal agent config: %v", err)
	}
	if cfg.Cloud != "gcp" || cfg.GCP.ProjectID != "shared-resources-alt" || cfg.GCP.Network != "bnat-net" {
		t.Fatalf("unexpected GCP config: %#v", cfg.GCP)
	}
	if cfg.HA.Lease.Backend != "firestore" || cfg.Coordination.Backend != "firestore" {
		t.Fatalf("expected Firestore HA config: lease=%#v coordination=%#v", cfg.HA.Lease, cfg.Coordination)
	}
	if len(cfg.HA.RouteFailover.RouteTableIDs) != 1 || cfg.HA.RouteFailover.RouteTableIDs[0] != "bnat-gcp-default-via-gateway" {
		t.Fatalf("route names should be rendered as route targets: %#v", cfg.HA.RouteFailover)
	}
	if cfg.HA.PublicIdentity.Mode != "" {
		t.Fatalf("GCP route-only HA should not configure public identity: %#v", cfg.HA.PublicIdentity)
	}
}

func TestGCPAgentHABootstrapRequiresArtifacts(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs)
	if err == nil {
		t.Fatal("expected missing artifact error")
	}
	if !strings.Contains(err.Error(), "enable_agent_ha requires betternat_version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGCPAgentHABootstrapRequiresServiceAccount(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.BetterNATVersion = types.StringValue("v0.1.0")
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs)
	if err == nil {
		t.Fatal("expected missing service account error")
	}
	if !strings.Contains(err.Error(), "enable_agent_ha requires service_account_email") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func baseGCPGatewayModel() GCPGatewayResourceModel {
	return GCPGatewayResourceModel{
		Name:                types.StringValue("bnat-gcp"),
		ProjectID:           types.StringValue("shared-resources-alt"),
		Region:              types.StringValue("us-west1"),
		Zone:                types.StringValue("us-west1-a"),
		Network:             types.StringValue("bnat-net"),
		Subnetwork:          types.StringValue("bnat-subnet"),
		ClientTag:           types.StringValue("bnat-client"),
		RouteName:           types.StringNull(),
		RoutePriority:       types.Int64Null(),
		RouteDestRange:      types.StringNull(),
		MachineType:         types.StringNull(),
		ImageProject:        types.StringNull(),
		ImageFamily:         types.StringNull(),
		GatewayCount:        types.Int64Null(),
		ServiceAccountEmail: types.StringNull(),
		EnableAgentHA:       types.BoolNull(),
		FirestoreDatabaseID: types.StringNull(),
	}
}
