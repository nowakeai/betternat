package gcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	gcpiam "google.golang.org/api/iam/v1"
)

var (
	runtimeServiceAccountReadyAttempts = 12
	runtimeServiceAccountReadyDelay    = 5 * time.Second
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
			Description: "Runtime identity for BetterNAT GCP HA gateways.",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create gcp runtime service account %q: %w", inputs.AccountID, err)
	}
	ready, err := m.waitUntilReady(ctx, name)
	if err != nil {
		return nil, err
	}
	if ready != nil {
		return ready, nil
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
	// Keep provider-managed runtime service accounts across gateway replacement.
	// GCP can reject IAM bindings for a same-email service account recreated
	// shortly after deletion for several minutes, which makes Terraform
	// replacement unreliable. IAM bindings are still removed separately.
	return nil
}

func (i RuntimeServiceAccountInputs) Email() string {
	return i.AccountID + "@" + i.ProjectID + ".iam.gserviceaccount.com"
}

func (i RuntimeServiceAccountInputs) Validate() error {
	return i.validate()
}

func (m RuntimeServiceAccountManager) waitUntilReady(ctx context.Context, name string) (*gcpiam.ServiceAccount, error) {
	attempts := runtimeServiceAccountReadyAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		account, err := m.API.GetServiceAccount(ctx, name)
		if err == nil {
			return account, nil
		}
		lastErr = err
		if !isGoogleNotFound(err) {
			return nil, fmt.Errorf("get created gcp runtime service account %q: %w", name, err)
		}
		if attempt+1 == attempts {
			break
		}
		if err := sleepContext(ctx, runtimeServiceAccountReadyDelay); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("wait for gcp runtime service account %q to become visible: %w", name, lastErr)
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
