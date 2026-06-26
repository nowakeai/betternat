package gcp

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/googleapi"
	gcpiam "google.golang.org/api/iam/v1"
)

func TestRuntimeIAMPermissionsIncludeFirestoreNativeCoordination(t *testing.T) {
	permissions := RuntimeIAMPermissions()
	required := []string{
		"datastore.databases.get",
		"datastore.databases.getMetadata",
		"datastore.databases.list",
		"datastore.entities.allocateIds",
		"datastore.entities.create",
		"datastore.entities.delete",
		"datastore.entities.get",
		"datastore.entities.list",
		"datastore.entities.update",
		"datastore.namespaces.get",
		"datastore.namespaces.list",
		"datastore.schemas.list",
		"datastore.statistics.get",
		"datastore.statistics.list",
		"compute.addresses.use",
		"compute.networks.updatePolicy",
		"compute.subnetworks.useExternalIp",
		"resourcemanager.projects.get",
	}
	for _, permission := range required {
		if !slices.Contains(permissions, permission) {
			t.Fatalf("runtime IAM permissions missing %q", permission)
		}
	}
}

func TestRuntimeIAMApplyCreatesRoleAndBinding(t *testing.T) {
	api := &fakeRuntimeIAMAPI{
		getRoleErr: &googleapi.Error{Code: 404, Message: "not found"},
		policy:     &cloudresourcemanager.Policy{},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Apply(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("apply runtime iam: %v", err)
	}
	if api.created == nil {
		t.Fatal("expected custom role creation")
	}
	if api.createParent != "projects/shared-resources-alt" || api.createRoleID != runtimeRoleID {
		t.Fatalf("unexpected create request: parent=%q role_id=%q", api.createParent, api.createRoleID)
	}
	if api.created.Name != "" {
		t.Fatalf("create role request must not set role.name, got %q", api.created.Name)
	}
	wantRole := "projects/shared-resources-alt/roles/betterNATRuntime"
	if api.setPolicy == nil || len(api.setPolicy.Bindings) != 1 {
		t.Fatalf("expected project policy binding, got %#v", api.setPolicy)
	}
	binding := api.setPolicy.Bindings[0]
	if binding.Role != wantRole {
		t.Fatalf("unexpected binding role %q", binding.Role)
	}
	wantMember := "serviceAccount:betternat-runtime@example.iam.gserviceaccount.com"
	if !reflect.DeepEqual(binding.Members, []string{wantMember}) {
		t.Fatalf("unexpected binding members %#v", binding.Members)
	}
}

func TestRuntimeIAMApplyPatchesChangedRole(t *testing.T) {
	api := &fakeRuntimeIAMAPI{
		role: &gcpiam.Role{
			Name:                "projects/shared-resources-alt/roles/betterNATRuntime",
			Title:               "old",
			Description:         "old",
			IncludedPermissions: []string{"compute.routes.get"},
			Stage:               "ALPHA",
			Etag:                "etag-1",
		},
		policy: &cloudresourcemanager.Policy{Bindings: []*cloudresourcemanager.Binding{{
			Role:    "projects/shared-resources-alt/roles/betterNATRuntime",
			Members: []string{"serviceAccount:betternat-runtime@example.iam.gserviceaccount.com"},
		}}},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Apply(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("apply runtime iam: %v", err)
	}
	if api.patched == nil {
		t.Fatal("expected role patch")
	}
	if api.patched.Etag != "etag-1" {
		t.Fatalf("expected etag to be preserved, got %q", api.patched.Etag)
	}
	if api.setPolicy != nil {
		t.Fatalf("policy should be unchanged when binding already exists: %#v", api.setPolicy)
	}
}

func TestRuntimeIAMApplyUndeletesRemovedRole(t *testing.T) {
	roleName := "projects/shared-resources-alt/roles/betterNATRuntime"
	api := &fakeRuntimeIAMAPI{
		role: &gcpiam.Role{
			Name:    roleName,
			Deleted: true,
			Etag:    "deleted-etag",
		},
		undeletedRole: &gcpiam.Role{
			Name: roleName,
			Etag: "undeleted-etag",
		},
		policy: &cloudresourcemanager.Policy{},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Apply(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("apply runtime iam: %v", err)
	}
	if api.undeletedName != roleName {
		t.Fatalf("expected role undelete, got %q", api.undeletedName)
	}
	if api.patched == nil {
		t.Fatal("expected undeleted role patch")
	}
	if api.patched.Etag != "undeleted-etag" {
		t.Fatalf("expected undeleted etag, got %q", api.patched.Etag)
	}
}

func TestRuntimeIAMApplyRetriesServiceAccountPropagation(t *testing.T) {
	previousDelay := runtimeIAMSetPolicyRetryDelay
	runtimeIAMSetPolicyRetryDelay = time.Nanosecond
	defer func() { runtimeIAMSetPolicyRetryDelay = previousDelay }()

	api := &fakeRuntimeIAMAPI{
		getRoleErr: &googleapi.Error{Code: 404, Message: "not found"},
		policy:     &cloudresourcemanager.Policy{},
		setPolicyErrs: []error{
			&googleapi.Error{Code: 400, Message: "Service account betternat-runtime@example.iam.gserviceaccount.com does not exist."},
			nil,
		},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Apply(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("apply runtime iam: %v", err)
	}
	if api.setPolicyCalls != 2 {
		t.Fatalf("expected set policy retry, got %d calls", api.setPolicyCalls)
	}
}

func TestRuntimeIAMApplyIsIdempotent(t *testing.T) {
	roleName := "projects/shared-resources-alt/roles/betterNATRuntime"
	api := &fakeRuntimeIAMAPI{
		role: runtimeRole(roleName),
		policy: &cloudresourcemanager.Policy{Bindings: []*cloudresourcemanager.Binding{{
			Role:    roleName,
			Members: []string{"serviceAccount:betternat-runtime@example.iam.gserviceaccount.com"},
		}}},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Apply(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("apply runtime iam: %v", err)
	}
	if api.created != nil || api.patched != nil || api.setPolicy != nil {
		t.Fatalf("expected no changes, create=%#v patch=%#v policy=%#v", api.created, api.patched, api.setPolicy)
	}
}

func TestRuntimeIAMCleanupRemovesBindingAndRole(t *testing.T) {
	roleName := "projects/shared-resources-alt/roles/betterNATRuntime"
	api := &fakeRuntimeIAMAPI{
		policy: &cloudresourcemanager.Policy{Bindings: []*cloudresourcemanager.Binding{
			{
				Role:    roleName,
				Members: []string{"serviceAccount:betternat-runtime@example.iam.gserviceaccount.com"},
			},
			{
				Role:    "roles/viewer",
				Members: []string{"user:ops@example.com"},
			},
		}},
	}
	manager := RuntimeIAMManager{API: api}

	err := manager.Cleanup(context.Background(), RuntimeIAMInputs{
		ProjectID:           "shared-resources-alt",
		ServiceAccountEmail: "betternat-runtime@example.iam.gserviceaccount.com",
	})
	if err != nil {
		t.Fatalf("cleanup runtime iam: %v", err)
	}
	if api.deletedName != roleName {
		t.Fatalf("unexpected deleted role %q", api.deletedName)
	}
	if api.setPolicy == nil || len(api.setPolicy.Bindings) != 1 || api.setPolicy.Bindings[0].Role != "roles/viewer" {
		t.Fatalf("unexpected cleanup policy: %#v", api.setPolicy)
	}
}

func TestRuntimeIAMValidateRequiredFields(t *testing.T) {
	err := (RuntimeIAMManager{API: &fakeRuntimeIAMAPI{}}).Apply(context.Background(), RuntimeIAMInputs{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

type fakeRuntimeIAMAPI struct {
	role         *gcpiam.Role
	getRoleErr   error
	policy       *cloudresourcemanager.Policy
	getPolicyErr error

	createParent   string
	createRoleID   string
	created        *gcpiam.Role
	patched        *gcpiam.Role
	undeletedName  string
	undeletedRole  *gcpiam.Role
	setPolicy      *cloudresourcemanager.Policy
	setPolicyErrs  []error
	setPolicyCalls int
	deletedName    string
}

func (f *fakeRuntimeIAMAPI) GetRole(_ context.Context, _ string) (*gcpiam.Role, error) {
	if f.getRoleErr != nil {
		return nil, f.getRoleErr
	}
	if f.role == nil {
		return nil, errors.New("missing fake role")
	}
	return f.role, nil
}

func (f *fakeRuntimeIAMAPI) CreateRole(_ context.Context, parent string, request *gcpiam.CreateRoleRequest) (*gcpiam.Role, error) {
	f.createParent = parent
	f.createRoleID = request.RoleId
	f.created = request.Role
	return request.Role, nil
}

func (f *fakeRuntimeIAMAPI) PatchRole(_ context.Context, _ string, role *gcpiam.Role) (*gcpiam.Role, error) {
	f.patched = role
	return role, nil
}

func (f *fakeRuntimeIAMAPI) UndeleteRole(_ context.Context, name string) (*gcpiam.Role, error) {
	f.undeletedName = name
	if f.undeletedRole != nil {
		return f.undeletedRole, nil
	}
	return &gcpiam.Role{Name: name}, nil
}

func (f *fakeRuntimeIAMAPI) DeleteRole(_ context.Context, name string) (*gcpiam.Role, error) {
	f.deletedName = name
	return &gcpiam.Role{Name: name}, nil
}

func (f *fakeRuntimeIAMAPI) GetPolicy(_ context.Context, _ string) (*cloudresourcemanager.Policy, error) {
	if f.getPolicyErr != nil {
		return nil, f.getPolicyErr
	}
	if f.policy == nil {
		f.policy = &cloudresourcemanager.Policy{}
	}
	return f.policy, nil
}

func (f *fakeRuntimeIAMAPI) SetPolicy(_ context.Context, _ string, policy *cloudresourcemanager.Policy) (*cloudresourcemanager.Policy, error) {
	f.setPolicyCalls++
	if len(f.setPolicyErrs) > 0 {
		err := f.setPolicyErrs[0]
		f.setPolicyErrs = f.setPolicyErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	f.setPolicy = policy
	return policy, nil
}
