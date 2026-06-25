package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/googleapi"
	gcpiam "google.golang.org/api/iam/v1"
)

const runtimeRoleID = "betterNATRuntime"

type RuntimeIAMInputs struct {
	ProjectID           string
	ServiceAccountEmail string
	RoleID              string
}

type RuntimeIAMAPI interface {
	GetRole(ctx context.Context, name string) (*gcpiam.Role, error)
	CreateRole(ctx context.Context, parent string, request *gcpiam.CreateRoleRequest) (*gcpiam.Role, error)
	PatchRole(ctx context.Context, name string, role *gcpiam.Role) (*gcpiam.Role, error)
	DeleteRole(ctx context.Context, name string) (*gcpiam.Role, error)
	GetPolicy(ctx context.Context, projectID string) (*cloudresourcemanager.Policy, error)
	SetPolicy(ctx context.Context, projectID string, policy *cloudresourcemanager.Policy) (*cloudresourcemanager.Policy, error)
}

type RuntimeIAMManager struct {
	API RuntimeIAMAPI
}

func NewRuntimeIAMAPI(ctx context.Context) (RuntimeIAMAPI, error) {
	iamService, err := gcpiam.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP IAM service: %w", err)
	}
	resourceManagerService, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("create GCP resource manager service: %w", err)
	}
	return googleRuntimeIAMAPI{iam: iamService, resourceManager: resourceManagerService}, nil
}

func (m RuntimeIAMManager) Apply(ctx context.Context, inputs RuntimeIAMInputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	if m.API == nil {
		return fmt.Errorf("gcp runtime iam api is required")
	}
	roleName := runtimeRoleName(inputs.ProjectID, inputs.roleID())
	role := runtimeRole(roleName)
	if existing, err := m.API.GetRole(ctx, roleName); err != nil && !isGoogleNotFound(err) {
		return fmt.Errorf("get gcp runtime iam role %q: %w", roleName, err)
	} else if err != nil {
		if _, err := m.API.CreateRole(ctx, "projects/"+inputs.ProjectID, &gcpiam.CreateRoleRequest{
			RoleId: inputs.roleID(),
			Role:   role,
		}); err != nil {
			return fmt.Errorf("create gcp runtime iam role %q: %w", roleName, err)
		}
	} else if !samePermissions(existing.IncludedPermissions, role.IncludedPermissions) || existing.Title != role.Title || existing.Description != role.Description || existing.Stage != role.Stage {
		role.Etag = existing.Etag
		if _, err := m.API.PatchRole(ctx, roleName, role); err != nil {
			return fmt.Errorf("patch gcp runtime iam role %q: %w", roleName, err)
		}
	}
	policy, err := m.API.GetPolicy(ctx, inputs.ProjectID)
	if err != nil {
		return fmt.Errorf("get gcp project iam policy %q: %w", inputs.ProjectID, err)
	}
	member := "serviceAccount:" + inputs.ServiceAccountEmail
	if addBindingMember(policy, roleName, member) {
		if _, err := m.API.SetPolicy(ctx, inputs.ProjectID, policy); err != nil {
			return fmt.Errorf("set gcp project iam policy %q: %w", inputs.ProjectID, err)
		}
	}
	return nil
}

func (m RuntimeIAMManager) Cleanup(ctx context.Context, inputs RuntimeIAMInputs) error {
	if err := inputs.validate(); err != nil {
		return err
	}
	if m.API == nil {
		return fmt.Errorf("gcp runtime iam api is required")
	}
	roleName := runtimeRoleName(inputs.ProjectID, inputs.roleID())
	policy, err := m.API.GetPolicy(ctx, inputs.ProjectID)
	if err != nil && !isGoogleNotFound(err) {
		return fmt.Errorf("get gcp project iam policy %q: %w", inputs.ProjectID, err)
	}
	if err == nil && removeBindingMember(policy, roleName, "serviceAccount:"+inputs.ServiceAccountEmail) {
		if _, err := m.API.SetPolicy(ctx, inputs.ProjectID, policy); err != nil {
			return fmt.Errorf("set gcp project iam policy %q: %w", inputs.ProjectID, err)
		}
	}
	if _, err := m.API.DeleteRole(ctx, roleName); err != nil && !isGoogleNotFound(err) {
		return fmt.Errorf("delete gcp runtime iam role %q: %w", roleName, err)
	}
	return nil
}

type googleRuntimeIAMAPI struct {
	iam             *gcpiam.Service
	resourceManager *cloudresourcemanager.Service
}

func (a googleRuntimeIAMAPI) GetRole(ctx context.Context, name string) (*gcpiam.Role, error) {
	return a.iam.Projects.Roles.Get(name).Context(ctx).Do()
}

func (a googleRuntimeIAMAPI) CreateRole(ctx context.Context, parent string, request *gcpiam.CreateRoleRequest) (*gcpiam.Role, error) {
	return a.iam.Projects.Roles.Create(parent, request).Context(ctx).Do()
}

func (a googleRuntimeIAMAPI) PatchRole(ctx context.Context, name string, role *gcpiam.Role) (*gcpiam.Role, error) {
	return a.iam.Projects.Roles.Patch(name, role).
		UpdateMask("title,description,includedPermissions,stage").
		Context(ctx).
		Do()
}

func (a googleRuntimeIAMAPI) DeleteRole(ctx context.Context, name string) (*gcpiam.Role, error) {
	return a.iam.Projects.Roles.Delete(name).Context(ctx).Do()
}

func (a googleRuntimeIAMAPI) GetPolicy(ctx context.Context, projectID string) (*cloudresourcemanager.Policy, error) {
	return a.resourceManager.Projects.GetIamPolicy(projectID, &cloudresourcemanager.GetIamPolicyRequest{}).Context(ctx).Do()
}

func (a googleRuntimeIAMAPI) SetPolicy(ctx context.Context, projectID string, policy *cloudresourcemanager.Policy) (*cloudresourcemanager.Policy, error) {
	return a.resourceManager.Projects.SetIamPolicy(projectID, &cloudresourcemanager.SetIamPolicyRequest{Policy: policy}).Context(ctx).Do()
}

func runtimeRoleName(projectID string, roleID string) string {
	return "projects/" + projectID + "/roles/" + roleID
}

func runtimeRole(name string) *gcpiam.Role {
	return &gcpiam.Role{
		Name:                name,
		Title:               "BetterNAT Runtime",
		Description:         "Runtime permissions for BetterNAT GCP route-only HA gateways.",
		IncludedPermissions: RuntimeIAMPermissions(),
		Stage:               "ALPHA",
	}
}

func (i RuntimeIAMInputs) roleID() string {
	if strings.TrimSpace(i.RoleID) != "" {
		return i.RoleID
	}
	return runtimeRoleID
}

func (i RuntimeIAMInputs) Validate() error {
	return i.validate()
}

func (i RuntimeIAMInputs) validate() error {
	missing := []string{}
	if strings.TrimSpace(i.ProjectID) == "" {
		missing = append(missing, "project_id")
	}
	if strings.TrimSpace(i.ServiceAccountEmail) == "" {
		missing = append(missing, "service_account_email")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required GCP runtime IAM inputs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func addBindingMember(policy *cloudresourcemanager.Policy, role string, member string) bool {
	if policy == nil {
		return false
	}
	for _, binding := range policy.Bindings {
		if binding.Role != role {
			continue
		}
		for _, existing := range binding.Members {
			if existing == member {
				return false
			}
		}
		binding.Members = append(binding.Members, member)
		sort.Strings(binding.Members)
		return true
	}
	policy.Bindings = append(policy.Bindings, &cloudresourcemanager.Binding{
		Role:    role,
		Members: []string{member},
	})
	return true
}

func removeBindingMember(policy *cloudresourcemanager.Policy, role string, member string) bool {
	if policy == nil {
		return false
	}
	changed := false
	out := policy.Bindings[:0]
	for _, binding := range policy.Bindings {
		if binding.Role != role {
			out = append(out, binding)
			continue
		}
		members := binding.Members[:0]
		for _, existing := range binding.Members {
			if existing == member {
				changed = true
				continue
			}
			members = append(members, existing)
		}
		binding.Members = members
		if len(binding.Members) > 0 {
			out = append(out, binding)
		}
	}
	policy.Bindings = out
	return changed
}

func samePermissions(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func isGoogleNotFound(err error) bool {
	if err == nil {
		return false
	}
	if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == 404 {
		return true
	}
	return strings.Contains(err.Error(), "googleapi: Error 404")
}
