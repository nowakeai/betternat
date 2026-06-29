package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/googleapi"
	gcpiam "google.golang.org/api/iam/v1"
)

func TestRuntimeServiceAccountApplyCreatesMissingAccount(t *testing.T) {
	previousDelay := runtimeServiceAccountReadyDelay
	runtimeServiceAccountReadyDelay = time.Nanosecond
	defer func() { runtimeServiceAccountReadyDelay = previousDelay }()

	api := &fakeRuntimeServiceAccountAPI{
		getErrs: []error{
			&googleapi.Error{Code: 404, Message: "not found before create"},
			&googleapi.Error{Code: 404, Message: "not visible yet"},
			nil,
		},
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
	if api.createProject != "projects/shared-resources-alt" {
		t.Fatalf("unexpected create project: %s", api.createProject)
	}
	if api.createRequest.AccountId != "bnat-runtime" {
		t.Fatalf("unexpected account id: %s", api.createRequest.AccountId)
	}
	if api.getCalls != 3 {
		t.Fatalf("expected visibility polling after create, got %d get calls", api.getCalls)
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

func TestRuntimeServiceAccountCleanupRetainsAccount(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{}
	manager := RuntimeServiceAccountManager{API: api}

	err := manager.Cleanup(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("cleanup runtime service account: %v", err)
	}
	if api.deletedName != "" {
		t.Fatalf("runtime service account should be retained, deleted %s", api.deletedName)
	}
}

func TestRuntimeServiceAccountCleanupDoesNotCallDelete(t *testing.T) {
	api := &fakeRuntimeServiceAccountAPI{deleteErr: &googleapi.Error{Code: 404, Message: "not found"}}
	manager := RuntimeServiceAccountManager{API: api}

	err := manager.Cleanup(context.Background(), RuntimeServiceAccountInputs{
		ProjectID: "shared-resources-alt",
		AccountID: "bnat-runtime",
	})
	if err != nil {
		t.Fatalf("cleanup runtime service account: %v", err)
	}
	if api.deletedName != "" {
		t.Fatalf("runtime service account cleanup should not delete account: %s", api.deletedName)
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
	getErrs       []error
	getCalls      int
	createProject string
	createRequest *gcpiam.CreateServiceAccountRequest
	deletedName   string
	deleteErr     error
}

func (f *fakeRuntimeServiceAccountAPI) GetServiceAccount(_ context.Context, _ string) (*gcpiam.ServiceAccount, error) {
	f.getCalls++
	if len(f.getErrs) > 0 {
		err := f.getErrs[0]
		f.getErrs = f.getErrs[1:]
		if err != nil {
			return nil, err
		}
	}
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
