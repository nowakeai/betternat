package tfprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	gcpinstall "github.com/nowakeai/betternat/internal/install/gcp"
)

func prepareGCPFirestoreDatabasePlan(model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageFirestoreDatabase, false) {
		if model.FirestoreLocationID.IsNull() || model.FirestoreLocationID.IsUnknown() {
			model.FirestoreLocationID = types.StringValue("")
		}
		return nil
	}
	if !boolDefault(model.EnableAgentHA, false) {
		return fmt.Errorf("manage_firestore_database requires enable_agent_ha")
	}
	databaseID := stringDefault(model.FirestoreDatabaseID, "(default)")
	locationID := stringDefault(model.FirestoreLocationID, model.Region.ValueString())
	inputs := gcpinstall.FirestoreDatabaseInputs{
		ProjectID:  model.ProjectID.ValueString(),
		DatabaseID: databaseID,
		LocationID: locationID,
	}
	if err := inputs.Validate(); err != nil {
		return err
	}
	model.FirestoreDatabaseID = types.StringValue(databaseID)
	model.FirestoreLocationID = types.StringValue(locationID)
	return nil
}

func applyGCPFirestoreDatabase(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageFirestoreDatabase, false) {
		return nil
	}
	manager, inputs, err := gcpFirestoreDatabaseManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	return manager.Apply(ctx, inputs)
}

func cleanupGCPFirestoreDatabase(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageFirestoreDatabase, false) {
		return nil
	}
	manager, inputs, err := gcpFirestoreDatabaseManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	return manager.Cleanup(ctx, inputs)
}

func gcpFirestoreDatabaseManagerAndInputs(ctx context.Context, model *GCPGatewayResourceModel) (gcpinstall.FirestoreDatabaseManager, gcpinstall.FirestoreDatabaseInputs, error) {
	inputs, err := gcpFirestoreDatabaseInputs(model)
	if err != nil {
		return gcpinstall.FirestoreDatabaseManager{}, gcpinstall.FirestoreDatabaseInputs{}, err
	}
	api, err := gcpinstall.NewFirestoreDatabaseAPI(ctx)
	if err != nil {
		return gcpinstall.FirestoreDatabaseManager{}, gcpinstall.FirestoreDatabaseInputs{}, err
	}
	return gcpinstall.FirestoreDatabaseManager{API: api}, inputs, nil
}

func gcpFirestoreDatabaseInputs(model *GCPGatewayResourceModel) (gcpinstall.FirestoreDatabaseInputs, error) {
	if !boolDefault(model.ManageFirestoreDatabase, false) {
		return gcpinstall.FirestoreDatabaseInputs{}, nil
	}
	if !boolDefault(model.EnableAgentHA, false) {
		return gcpinstall.FirestoreDatabaseInputs{}, fmt.Errorf("manage_firestore_database requires enable_agent_ha")
	}
	inputs := gcpinstall.FirestoreDatabaseInputs{
		ProjectID:         model.ProjectID.ValueString(),
		DatabaseID:        stringDefault(model.FirestoreDatabaseID, "(default)"),
		LocationID:        stringDefault(model.FirestoreLocationID, model.Region.ValueString()),
		OperationPollTime: 2 * time.Second,
	}
	if err := inputs.Validate(); err != nil {
		return gcpinstall.FirestoreDatabaseInputs{}, err
	}
	return inputs, nil
}

func prepareGCPRuntimeServiceAccountPlan(model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeServiceAccount, false) {
		if model.RuntimeServiceAccountID.IsNull() || model.RuntimeServiceAccountID.IsUnknown() {
			model.RuntimeServiceAccountID = types.StringValue("")
		}
		return nil
	}
	if !boolDefault(model.EnableAgentHA, false) {
		return fmt.Errorf("manage_runtime_service_account requires enable_agent_ha")
	}
	accountID := stringDefault(model.RuntimeServiceAccountID, "")
	if accountID == "" {
		accountID = defaultGCPRuntimeServiceAccountID(model.Name.ValueString())
	}
	inputs := gcpinstall.RuntimeServiceAccountInputs{
		ProjectID: model.ProjectID.ValueString(),
		AccountID: accountID,
	}
	if err := inputs.Validate(); err != nil {
		return err
	}
	model.RuntimeServiceAccountID = types.StringValue(accountID)
	if stringDefault(model.ServiceAccountEmail, "") == "" {
		model.ServiceAccountEmail = types.StringValue(inputs.Email())
	}
	return nil
}

func applyGCPRuntimeServiceAccount(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeServiceAccount, false) {
		return nil
	}
	manager, inputs, err := gcpRuntimeServiceAccountManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	_, err = manager.Apply(ctx, inputs)
	return err
}

func cleanupGCPRuntimeServiceAccount(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeServiceAccount, false) {
		return nil
	}
	manager, inputs, err := gcpRuntimeServiceAccountManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	return manager.Cleanup(ctx, inputs)
}

func gcpRuntimeServiceAccountManagerAndInputs(ctx context.Context, model *GCPGatewayResourceModel) (gcpinstall.RuntimeServiceAccountManager, gcpinstall.RuntimeServiceAccountInputs, error) {
	inputs, err := gcpRuntimeServiceAccountInputs(model)
	if err != nil {
		return gcpinstall.RuntimeServiceAccountManager{}, gcpinstall.RuntimeServiceAccountInputs{}, err
	}
	api, err := gcpinstall.NewRuntimeServiceAccountAPI(ctx)
	if err != nil {
		return gcpinstall.RuntimeServiceAccountManager{}, gcpinstall.RuntimeServiceAccountInputs{}, err
	}
	return gcpinstall.RuntimeServiceAccountManager{API: api}, inputs, nil
}

func gcpRuntimeServiceAccountInputs(model *GCPGatewayResourceModel) (gcpinstall.RuntimeServiceAccountInputs, error) {
	if !boolDefault(model.ManageRuntimeServiceAccount, false) {
		return gcpinstall.RuntimeServiceAccountInputs{}, nil
	}
	inputs := gcpinstall.RuntimeServiceAccountInputs{
		ProjectID: model.ProjectID.ValueString(),
		AccountID: stringDefault(model.RuntimeServiceAccountID, ""),
	}
	if err := inputs.Validate(); err != nil {
		return gcpinstall.RuntimeServiceAccountInputs{}, err
	}
	return inputs, nil
}

func applyGCPRuntimeIAM(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeIAM, false) {
		return nil
	}
	manager, inputs, err := gcpRuntimeIAMManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	return manager.Apply(ctx, inputs)
}

func cleanupGCPRuntimeIAM(ctx context.Context, model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeIAM, false) {
		return nil
	}
	manager, inputs, err := gcpRuntimeIAMManagerAndInputs(ctx, model)
	if err != nil {
		return err
	}
	return manager.Cleanup(ctx, inputs)
}

func prepareGCPRuntimeIAMPlan(model *GCPGatewayResourceModel) error {
	if !boolDefault(model.ManageRuntimeIAM, false) {
		if model.RuntimeIAMRoleID.IsNull() || model.RuntimeIAMRoleID.IsUnknown() {
			model.RuntimeIAMRoleID = types.StringValue("")
		}
		return nil
	}
	if !boolDefault(model.EnableAgentHA, false) {
		return fmt.Errorf("manage_runtime_iam requires enable_agent_ha")
	}
	roleID := stringDefault(model.RuntimeIAMRoleID, "")
	if roleID == "" {
		roleID = defaultGCPRuntimeIAMRoleID(model.Name.ValueString())
	}
	inputs := gcpinstall.RuntimeIAMInputs{
		ProjectID:           model.ProjectID.ValueString(),
		ServiceAccountEmail: stringDefault(model.ServiceAccountEmail, ""),
		RoleID:              roleID,
	}
	if err := inputs.Validate(); err != nil {
		return err
	}
	model.RuntimeIAMRoleID = types.StringValue(roleID)
	return nil
}

func gcpRuntimeIAMManagerAndInputs(ctx context.Context, model *GCPGatewayResourceModel) (gcpinstall.RuntimeIAMManager, gcpinstall.RuntimeIAMInputs, error) {
	inputs, err := gcpRuntimeIAMInputs(model)
	if err != nil {
		return gcpinstall.RuntimeIAMManager{}, gcpinstall.RuntimeIAMInputs{}, err
	}
	api, err := gcpinstall.NewRuntimeIAMAPI(ctx)
	if err != nil {
		return gcpinstall.RuntimeIAMManager{}, gcpinstall.RuntimeIAMInputs{}, err
	}
	return gcpinstall.RuntimeIAMManager{API: api}, inputs, nil
}

func gcpRuntimeIAMInputs(model *GCPGatewayResourceModel) (gcpinstall.RuntimeIAMInputs, error) {
	if !boolDefault(model.ManageRuntimeIAM, false) {
		return gcpinstall.RuntimeIAMInputs{}, nil
	}
	if !boolDefault(model.EnableAgentHA, false) {
		return gcpinstall.RuntimeIAMInputs{}, fmt.Errorf("manage_runtime_iam requires enable_agent_ha")
	}
	inputs := gcpinstall.RuntimeIAMInputs{
		ProjectID:           model.ProjectID.ValueString(),
		ServiceAccountEmail: stringDefault(model.ServiceAccountEmail, ""),
		RoleID:              stringDefault(model.RuntimeIAMRoleID, ""),
	}
	if err := inputs.Validate(); err != nil {
		return gcpinstall.RuntimeIAMInputs{}, err
	}
	return inputs, nil
}

func defaultGCPRuntimeServiceAccountID(name string) string {
	return sanitizeGCPServiceAccountID(name + "-runtime")
}

func defaultGCPRuntimeIAMRoleID(name string) string {
	return sanitizeGCPIAMRoleID(name)
}

func sanitizeGCPIAMRoleID(value string) string {
	out := make([]rune, 0, len(value))
	capNext := false
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if len(out) == 0 && r >= '0' && r <= '9' {
				out = append(out, 'b')
			}
			if capNext && r >= 'a' && r <= 'z' {
				r += 'A' - 'a'
			}
			out = append(out, r)
			capNext = false
			continue
		}
		capNext = len(out) > 0
	}
	if len(out) == 0 {
		out = []rune("betterNAT")
	}
	suffix := []rune("Runtime")
	if len(out) > 64 {
		out = out[:64]
	}
	if len(out) < len(suffix) || string(out[len(out)-len(suffix):]) != string(suffix) {
		maxPrefix := 64 - len(suffix)
		if len(out) > maxPrefix {
			out = out[:maxPrefix]
		}
		out = append(out, suffix...)
	}
	if len(out) < 3 {
		for len(out) < 3 {
			out = append(out, '0')
		}
	}
	return string(out)
}

func sanitizeGCPServiceAccountID(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	for len(out) > 0 && out[0] == '-' {
		out = out[1:]
	}
	if len(out) == 0 || out[0] < 'a' || out[0] > 'z' {
		out = append([]rune{'b'}, out...)
	}
	if len(out) > 30 {
		out = out[:30]
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if len(out) < 6 {
		for len(out) < 6 {
			out = append(out, '0')
		}
	}
	return string(out)
}
