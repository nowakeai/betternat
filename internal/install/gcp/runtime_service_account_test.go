package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
	gcpiam "google.golang.org/api/iam/v1"
)

func TestRuntimeServiceAccountApplyCreatesMissingAccount(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{
		getErr: &googleapi.Error{Code: 404, Message: "not found"},
	}
	manager := RuntimeServiceAccountManager{API: api}

	account, err := manager.Apply(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("apply runtime service account: %v", err)
	}
	if account.Email != "bnat-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("unexpected service account email: %s", account.Email)
	}
	if api.createProject != "projects/shared-resources-alt" {
		t.Fatalf("unexpected create project: %s", api.createProject)
	}
	if api.createRequest.AccountId != "bnat-runtime" {
		t.Fatalf("unexpected account id: %s", api.createRequest.AccountId)
	}
}

func TestRuntimeServiceAccountApplyIsIdempotent(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{
		account: &gcpiam.ServiceAccount{Email: "bnat-runtime@shared-resources-alt.iam.gserviceaccount.com"},
	}
	manager := RuntimeServiceAccountManager{API: api}

	account, err := manager.Apply(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("apply runtime service account: %v", err)
	}
	if account.Email != "bnat-runtime@shared-resources-alt.iam.gserviceaccount.com" {
		t.Fatalf("unexpected service account email: %s", account.Email)
	}
	if api.createRequest != nil {
		t.Fatalf("service account should not be created when it exists: %#v", api.createRequest)
	}
}

func TestRuntimeServiceAccountCleanupDeletesAccount(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{}
	manager := RuntimeServiceAccountManager{API: api}

	err := manager.Cleanup(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("cleanup runtime service account: %v", err)
	}
	want := "projects/shared-resources-alt/serviceAccounts/bnat-runtime@shared-resources-alt.iam.gserviceaccount.com"
	if api.deletedName != want {
		t.Fatalf("unexpected deleted service account: %s", api.deletedName)
	}
}

func TestRuntimeServiceAccountCleanupIgnoresMissingAccount(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{deleteErr: &googleapi.Error{Code: 404, Message: "not found"}}
	manager := RuntimeServiceAccountManager{API: api}

	err := manager.Cleanup(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("cleanup missing runtime service account: %v", err)
	}
}

func TestRuntimeServiceAccountValidateAccountID(t *testing.T) {
	cases := []string{"short", "Upper", "bad_", "-bad", "bad-", strings.Repeat("a", 31)}
	for _, accountID := range cases {
		err := (RuntimeServiceAccountInputs{ProjectID: "shared-resources-alt", AccountID: accountID}).validate()
		if err == nil {
			t.Fatalf("expected invalid account id %q", accountID)
		}
	}
	if err := (RuntimeServiceAccountInputs{ProjectID: "shared-resources-alt", AccountID: "bnat-runtime"}).validate(); err != nil {
		t.Fatalf("expected valid account id: %v", err)
	}
}

type fakeRuntimeServiceAccountAPI struct {
	account       *gcpiam.ServiceAccount
	getErr        error
	createProject string
	createRequest *gcpiam.CreateServiceAccountRequest
	deletedName   string
	deleteErr     error
}

func (f *fakeRuntimeServiceAccountAPI) GetServiceAccount(_ context.Context, _ string) (*gcpiam.ServiceAccount, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.account == nil {
		return nil, errors.New("missing fake service account")
	}
	return f.account, nil
}

func (f *fakeRuntimeServiceAccountAPI) CreateServiceAccount(_ context.Context, projectName string, request *gcpiam.CreateServiceAccountRequest) (*gcpiam.ServiceAccount, error) {
	f.createProject = projectName
	f.createRequest = request
	return &gcpiam.ServiceAccount{Email: request.AccountId + "@shared-resources-alt.iam.gserviceaccount.com"}, nil
}

func (f *fakeRuntimeServiceAccountAPI) DeleteServiceAccount(_ context.Context, name string) error {
	f.deletedName = name
	return f.deleteErr
}
