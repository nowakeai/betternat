package gcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/googleapi"
	gcpiam "google.golang.org/api/iam/v1"
)

const runtimeRoleID = "betterNATRuntime"

var runtimeIAMSetPolicyRetryDelay = 5 * time.Second
var runtimeIAMSetPolicyAttempts = 60

type RuntimeIAMInputs struct {
	ProjectID           string
	ServiceAccountEmail string
	RoleID              string
}

type RuntimeIAMAPI interface {
	GetRole(ctx context.Context, name string) (*gcpiam.Role, error)
	CreateRole(ctx context.Context, parent string, request *gcpiam.CreateRoleRequest) (*gcpiam.Role, error)
	PatchRole(ctx context.Context, name string, role *gcpiam.Role) (*gcpiam.Role, error)
	UndeleteRole(ctx context.Context, name string) (*gcpiam.Role, error)
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
		createRole := *role
		createRole.Name = ""
		if _, err := m.API.CreateRole(ctx, "projects/"+inputs.ProjectID, &gcpiam.CreateRoleRequest{
			RoleId: inputs.roleID(),
			Role:   &createRole,
		}); err != nil {
			return fmt.Errorf("create gcp runtime iam role %q: %w", roleName, err)
		}
	} else if existing.Deleted {
		undeleted, err := m.API.UndeleteRole(ctx, roleName)
		if err != nil {
			return fmt.Errorf("undelete gcp runtime iam role %q: %w", roleName, err)
		}
		role.Etag = undeleted.Etag
		if _, err := m.API.PatchRole(ctx, roleName, role); err != nil {
			return fmt.Errorf("patch undeleted gcp runtime iam role %q: %w", roleName, err)
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
		if err := m.setPolicyWithRetry(ctx, inputs.ProjectID, policy); err != nil {
			return fmt.Errorf("set gcp project iam policy %q: %w", inputs.ProjectID, err)
		}
	}
	return nil
}

func (m RuntimeIAMManager) setPolicyWithRetry(ctx context.Context, projectID string, policy *cloudresourcemanager.Policy) error {
	var lastErr error
	attempts := runtimeIAMSetPolicyAttempts
	if attempts <= 0 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if _, err := m.API.SetPolicy(ctx, projectID, policy); err != nil {
			lastErr = err
			if !isServiceAccountPropagationError(err) {
				return err
			}
			if attempt+1 == attempts {
				break
			}
			if err := sleepContext(ctx, runtimeIAMSetPolicyRetryDelay); err != nil {
				return err
			}
			continue
		}
		return nil
	}
	return lastErr
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

func (a googleRuntimeIAMAPI) UndeleteRole(ctx context.Context, name string) (*gcpiam.Role, error) {
	return a.iam.Projects.Roles.Undelete(name, &gcpiam.UndeleteRoleRequest{}).Context(ctx).Do()
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
		Description:         "Runtime permissions for BetterNAT GCP HA gateways.",
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

func isServiceAccountPropagationError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "googleapi: error 400") &&
		strings.Contains(message, "service account") &&
		(strings.Contains(message, "does not exist") ||
			strings.Contains(message, "not found") ||
			strings.Contains(message, "deleted") ||
			strings.Contains(message, "disabled"))
}
