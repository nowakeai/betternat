package tfprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nowakeai/betternat/internal/config"
)

func TestGCPInputsDefaultToLegacySubstrateStartupScript(t *testing.T) {
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
	if model.ManageRuntimeServiceAccount.ValueBool() {
		t.Fatal("runtime service account management should default to disabled")
	}
	if model.ManageRuntimeIAM.ValueBool() {
		t.Fatal("runtime IAM management should default to disabled")
	}
	if model.ManageFirestoreDatabase.ValueBool() {
		t.Fatal("firestore database management should default to disabled")
	}
	perms, err := listStrings(context.Background(), model.RuntimeIAMPermissions)
	if err != nil {
		t.Fatalf("runtime iam permissions: %v", err)
	}
	if !stringSliceContains(perms, "compute.routes.create") || !stringSliceContains(perms, "datastore.entities.update") {
		t.Fatalf("runtime iam permissions missing route/firestore actions: %#v", perms)
	}
	if !strings.Contains(model.StartupScript.ValueString(), "nft add rule ip nat postrouting ip saddr 10.91.0.0/24 masquerade") {
		t.Fatalf("default substrate path should keep legacy forwarding startup script: %s", model.StartupScript.ValueString())
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

func TestGCPAgentHABootstrapRendersStablePublicIdentity(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.BetterNATVersion = types.StringValue("v0.1.0")
	model.FirestoreDatabaseID = types.StringValue("(default)")
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")
	model.StablePublicIdentityAddress = types.StringValue("bnat-static-egress")
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	if err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs); err != nil {
		t.Fatalf("enrich bootstrap: %v", err)
	}

	var cfg config.Config
	if err := json.Unmarshal([]byte(model.AgentConfigJSON.ValueString()), &cfg); err != nil {
		t.Fatalf("unmarshal agent config: %v", err)
	}
	if cfg.HA.PublicIdentity.Mode != "shared_eip" || cfg.HA.PublicIdentity.AllocationID != "bnat-static-egress" {
		t.Fatalf("expected GCP stable public identity config: %#v", cfg.HA.PublicIdentity)
	}
}

func TestGCPStablePublicIdentityRequiresAgentHA(t *testing.T) {
	model := baseGCPGatewayModel()
	model.StablePublicIdentityAddress = types.StringValue("bnat-static-egress")
	privateCIDRs := []string{"10.91.0.0/24"}

	inputs := gcpInputs(model, privateCIDRs)
	err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs)
	if err == nil || !strings.Contains(err.Error(), "stable_public_identity_address_name requires enable_agent_ha") {
		t.Fatalf("expected stable identity validation error, got %v", err)
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

func TestGCPManagedRuntimeServiceAccountDerivesEmail(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageRuntimeServiceAccount = types.BoolValue(true)
	model.BetterNATVersion = types.StringValue("v0.1.0")
	privateCIDRs := []string{"10.91.0.0/24"}

	if err := prepareGCPRuntimeServiceAccountPlan(&model); err != nil {
		t.Fatalf("prepare runtime service account: %v", err)
	}
	if model.RuntimeServiceAccountID.ValueString() != "bnat-gcp-runtime" {
		t.Fatalf("unexpected runtime service account id: %s", model.RuntimeServiceAccountID.ValueString())
	}
	if model.ServiceAccountEmail.ValueString() != "bnat-gcp-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("unexpected derived service account email: %s", model.ServiceAccountEmail.ValueString())
	}

	inputs := gcpInputs(model, privateCIDRs)
	if err := enrichGCPAgentBootstrap(&model, &inputs, privateCIDRs); err != nil {
		t.Fatalf("enrich bootstrap with managed service account: %v", err)
	}
	if inputs.ServiceAccountEmail != "bnat-gcp-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("service account should be passed to inputs: %s", inputs.ServiceAccountEmail)
	}
}

func TestGCPManagedRuntimeServiceAccountRequiresAgentHA(t *testing.T) {
	model := baseGCPGatewayModel()
	model.ManageRuntimeServiceAccount = types.BoolValue(true)

	err := prepareGCPRuntimeServiceAccountPlan(&model)
	if err == nil {
		t.Fatal("expected manage_runtime_service_account without agent HA to fail")
	}
	if !strings.Contains(err.Error(), "manage_runtime_service_account requires enable_agent_ha") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGCPRuntimeServiceAccountInputs(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageRuntimeServiceAccount = types.BoolValue(true)
	if err := prepareGCPRuntimeServiceAccountPlan(&model); err != nil {
		t.Fatalf("prepare runtime service account: %v", err)
	}

	inputs, err := gcpRuntimeServiceAccountInputs(&model)
	if err != nil {
		t.Fatalf("runtime service account inputs: %v", err)
	}
	if inputs.ProjectID != "shared-resources-alt" || inputs.AccountID != "bnat-gcp-runtime" {
		t.Fatalf("unexpected runtime service account inputs: %#v", inputs)
	}
}

func TestSanitizeGCPServiceAccountID(t *testing.T) {
	cases := map[string]string{
		"BNAT_GCP":                         "bnat-gcp-runtime",
		"123":                              "b123-runtime",
		"this-name-is-definitely-too-long": "this-name-is-definitely-too-lo",
		"---":                              "runtime",
		"prod.egress":                      "prod-egress-runtime",
	}
	for input, want := range cases {
		if got := defaultGCPRuntimeServiceAccountID(input); got != want {
			t.Fatalf("default service account id for %q = %q, want %q", input, got, want)
		}
	}
}

func TestGCPRuntimeIAMInputsRequireAgentHA(t *testing.T) {
	model := baseGCPGatewayModel()
	model.ManageRuntimeIAM = types.BoolValue(true)
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")

	_, err := gcpRuntimeIAMInputs(&model)
	if err == nil {
		t.Fatal("expected manage_runtime_iam without agent HA to fail")
	}
	if !strings.Contains(err.Error(), "manage_runtime_iam requires enable_agent_ha") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGCPRuntimeIAMInputsUseRuntimeServiceAccount(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageRuntimeIAM = types.BoolValue(true)
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")

	if err := prepareGCPRuntimeIAMPlan(&model); err != nil {
		t.Fatalf("prepare runtime iam: %v", err)
	}
	inputs, err := gcpRuntimeIAMInputs(&model)
	if err != nil {
		t.Fatalf("runtime iam inputs: %v", err)
	}
	if inputs.ProjectID != "shared-resources-alt" {
		t.Fatalf("unexpected project id: %s", inputs.ProjectID)
	}
	if inputs.ServiceAccountEmail != "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("unexpected service account: %s", inputs.ServiceAccountEmail)
	}
	if inputs.RoleID != "bnatGcpRuntime" {
		t.Fatalf("unexpected role id: %s", inputs.RoleID)
	}
}

func TestGCPRuntimeIAMInputsUseExplicitRoleID(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageRuntimeIAM = types.BoolValue(true)
	model.ServiceAccountEmail = types.StringValue("betternat-runtime@shared-resources-alt.iam.gserviceaccount.com")
	model.RuntimeIAMRoleID = types.StringValue("customBetterNATRuntime")

	if err := prepareGCPRuntimeIAMPlan(&model); err != nil {
		t.Fatalf("prepare runtime iam: %v", err)
	}
	inputs, err := gcpRuntimeIAMInputs(&model)
	if err != nil {
		t.Fatalf("runtime iam inputs: %v", err)
	}
	if inputs.RoleID != "customBetterNATRuntime" {
		t.Fatalf("unexpected role id: %s", inputs.RoleID)
	}
}

func TestSanitizeGCPIAMRoleID(t *testing.T) {
	cases := map[string]string{
		"bnat-gcp": "bnatGcpRuntime",
		"123":      "b123Runtime",
		"---":      "betterNATRuntime",
		"prod.egress.gateway.with.a.very.long.name.that.must.be.trimmed": "prodEgressGatewayWithAVeryLongNameThatMustBeTrimmedRuntime",
	}
	for input, want := range cases {
		if got := defaultGCPRuntimeIAMRoleID(input); got != want {
			t.Fatalf("default role id for %q = %q, want %q", input, got, want)
		}
	}
}

func TestGCPRuntimeIAMInputsRequireServiceAccount(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageRuntimeIAM = types.BoolValue(true)

	_, err := gcpRuntimeIAMInputs(&model)
	if err == nil {
		t.Fatal("expected missing service account error")
	}
	if !strings.Contains(err.Error(), "service_account_email") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGCPManagedFirestoreDatabaseDefaultsLocation(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageFirestoreDatabase = types.BoolValue(true)

	if err := prepareGCPFirestoreDatabasePlan(&model); err != nil {
		t.Fatalf("prepare firestore database: %v", err)
	}
	if model.FirestoreDatabaseID.ValueString() != "(default)" {
		t.Fatalf("unexpected firestore database id: %s", model.FirestoreDatabaseID.ValueString())
	}
	if model.FirestoreLocationID.ValueString() != "us-west1" {
		t.Fatalf("unexpected firestore location id: %s", model.FirestoreLocationID.ValueString())
	}
}

func TestGCPManagedFirestoreDatabaseRequiresAgentHA(t *testing.T) {
	model := baseGCPGatewayModel()
	model.ManageFirestoreDatabase = types.BoolValue(true)

	err := prepareGCPFirestoreDatabasePlan(&model)
	if err == nil {
		t.Fatal("expected manage_firestore_database without agent HA to fail")
	}
	if !strings.Contains(err.Error(), "manage_firestore_database requires enable_agent_ha") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGCPFirestoreDatabaseInputs(t *testing.T) {
	model := baseGCPGatewayModel()
	model.EnableAgentHA = types.BoolValue(true)
	model.ManageFirestoreDatabase = types.BoolValue(true)
	model.FirestoreDatabaseID = types.StringValue("betternat-ha")
	model.FirestoreLocationID = types.StringValue("nam5")

	inputs, err := gcpFirestoreDatabaseInputs(&model)
	if err != nil {
		t.Fatalf("firestore database inputs: %v", err)
	}
	if inputs.ProjectID != "shared-resources-alt" || inputs.DatabaseID != "betternat-ha" || inputs.LocationID != "nam5" {
		t.Fatalf("unexpected firestore database inputs: %#v", inputs)
	}
}

func baseGCPGatewayModel() GCPGatewayResourceModel {
	return GCPGatewayResourceModel{
		Name:                        types.StringValue("bnat-gcp"),
		ProjectID:                   types.StringValue("shared-resources-alt"),
		Region:                      types.StringValue("us-west1"),
		Zone:                        types.StringValue("us-west1-a"),
		Network:                     types.StringValue("bnat-net"),
		Subnetwork:                  types.StringValue("bnat-subnet"),
		ClientTag:                   types.StringValue("bnat-client"),
		RouteName:                   types.StringNull(),
		RoutePriority:               types.Int64Null(),
		RouteDestRange:              types.StringNull(),
		MachineType:                 types.StringNull(),
		ImageProject:                types.StringNull(),
		ImageFamily:                 types.StringNull(),
		GatewayCount:                types.Int64Null(),
		ServiceAccountEmail:         types.StringNull(),
		RuntimeServiceAccountID:     types.StringNull(),
		ManageRuntimeServiceAccount: types.BoolNull(),
		RuntimeIAMRoleID:            types.StringNull(),
		ManageRuntimeIAM:            types.BoolNull(),
		EnableAgentHA:               types.BoolNull(),
		FirestoreDatabaseID:         types.StringNull(),
		FirestoreLocationID:         types.StringNull(),
		ManageFirestoreDatabase:     types.BoolNull(),
	}
}
