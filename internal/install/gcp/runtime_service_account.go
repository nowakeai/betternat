package gcp

import (
	"context"
	"fmt"
	"strings"

	gcpiam "google.golang.org/api/iam/v1"
)

type RuntimeServiceAccountInputs struct {
	ProjectID   string
	AccountID   string
	DisplayName string
}

type RuntimeServiceAccountAPI interface {
	GetServiceAccount(ctx context.Context, name string) (*gcpiam.ServiceAccount, error)
	CreateServiceAccount(ctx context.Context, projectName string, request *gcpiam.CreateServiceAccountRequest) (*gcpiam.ServiceAccount, error)
	DeleteServiceAccount(ctx context.Context, name string) error
}

type RuntimeServiceAccountManager struct {
	API RuntimeServiceAccountAPI
}

func NewRuntimeServiceAccountAPI(ctx context.Context) (RuntimeServiceAccountAPI, error) {
	iamService, err := gcpiam.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP IAM service: %w", err)
	}
	return googleRuntimeServiceAccountAPI{iam: iamService}, nil
}

func (m RuntimeServiceAccountManager) Apply(ctx context.Context, inputs RuntimeServiceAccountInputs) (*gcpiam.ServiceAccount, error) {
	if err := inputs.validate(); err != nil {
		return nil, err
	}
	if m.API == nil {
		return nil, fmt.Errorf("gcp runtime service account api is required")
	}
	name := runtimeServiceAccountName(inputs.ProjectID, inputs.Email())
	existing, err := m.API.GetServiceAccount(ctx, name)
	if err == nil {
		return existing, nil
	}
	if !isGoogleNotFound(err) {
		return nil, fmt.Errorf("get gcp runtime service account %q: %w", name, err)
	}
	account, err := m.API.CreateServiceAccount(ctx, "projects/"+inputs.ProjectID, &gcpiam.CreateServiceAccountRequest{
		AccountId: inputs.AccountID,
		ServiceAccount: &gcpiam.ServiceAccount{
			DisplayName: valueOr(inputs.DisplayName, "BetterNAT Runtime"),
			Description: "Runtime identity for BetterNAT GCP route-only HA gateways.",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create gcp runtime service account %q: %w", inputs.AccountID, err)
	}
	return account, nil
}

func (m RuntimeServiceAccountManager) Cleanup(ctx context.Context, inputs RuntimeServiceAccountInputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	if m.API == nil {
		return fmt.Errorf("gcp runtime service account api is required")
	}
	name := runtimeServiceAccountName(inputs.ProjectID, inputs.Email())
	if err := m.API.DeleteServiceAccount(ctx, name); err != nil && !isGoogleNotFound(err) {
		return fmt.Errorf("delete gcp runtime service account %q: %w", name, err)
	}
	return nil
}

func (i RuntimeServiceAccountInputs) Email() string {
	return i.AccountID + "@" + i.ProjectID + ".iam.gserviceaccount.com"
}

func (i RuntimeServiceAccountInputs) Validate() error {
	return i.validate()
}

func (i RuntimeServiceAccountInputs) validate() error {
	missing := []string{}
	if strings.TrimSpace(i.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if strings.TrimSpace(i.AccountID) == "" {
		missing = append(missing, "account_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required GCP runtime service account inputs: %s", strings.Join(missing, ", "))
	}
	if len(i.AccountID) < 6 || len(i.AccountID) > 30 {
		return fmt.Errorf("runtime service account id must be 6-30 characters")
	}
	if !validServiceAccountID(i.AccountID) {
		return fmt.Errorf("runtime service account id must match [a-z]([-a-z0-9]*[a-z0-9])")
	}
	return nil
}

func validServiceAccountID(value string) bool {
	for index, r := range value {
		if index == 0 {
			if r < 'a' || r > 'z' {
				return false
			}
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	last := value[len(value)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}

func runtimeServiceAccountName(projectID string, email string) string {
	return "projects/" + projectID + "/serviceAccounts/" + email
}

type googleRuntimeServiceAccountAPI struct {
	iam *gcpiam.Service
}

func (a googleRuntimeServiceAccountAPI) GetServiceAccount(ctx context.Context, name string) (*gcpiam.ServiceAccount, error) {
	return a.iam.Projects.ServiceAccounts.Get(name).Context(ctx).Do()
}

func (a googleRuntimeServiceAccountAPI) CreateServiceAccount(ctx context.Context, projectName string, request *gcpiam.CreateServiceAccountRequest) (*gcpiam.ServiceAccount, error) {
	return a.iam.Projects.ServiceAccounts.Create(projectName, request).Context(ctx).Do()
}

func (a googleRuntimeServiceAccountAPI) DeleteServiceAccount(ctx context.Context, name string) error {
	_, err := a.iam.Projects.ServiceAccounts.Delete(name).Context(ctx).Do()
	return err
}
