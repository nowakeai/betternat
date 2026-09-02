package tfprovider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/nowakeai/betternat/internal/install/aws"
	"github.com/nowakeai/betternat/internal/installplan"
)

func TestModifyPlanMarksUnsafeUpdateForReplacement(t *testing.T) {
	ctx := context.Background()
	resourceUnderTest := &GatewayResource{}
	var schemaResp resource.SchemaResponse
	resourceUnderTest.Schema(ctx, resource.SchemaRequest{}, &schemaResp)

	stateInput := validGatewayPlan()
	stateInput.GenerationID = types.StringValue("a1b2c3d4e5f6")
	stateInput.Tags = types.MapNull(types.StringType)
	state, err := DeriveGatewayState(ctx, &stateInput)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	plan := state
	plan.EIPAllocationIDs = mustStringMap(map[string]string{"us-west-2a": "eipalloc-external"})

	req := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema},
		State:  tfsdk.State{Schema: schemaResp.Schema},
		Plan:   tfsdk.Plan{Schema: schemaResp.Schema},
	}
	if diags := req.State.Set(ctx, state); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	if diags := req.Plan.Set(ctx, plan); diags.HasError() {
		t.Fatalf("set plan: %v", diags)
	}
	req.Config.Raw = req.Plan.Raw
	resp := resource.ModifyPlanResponse{Plan: req.Plan}
	resourceUnderTest.ModifyPlan(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("modify plan: %v", resp.Diagnostics)
	}
	if len(resp.RequiresReplace) != 1 || resp.RequiresReplace[0].String() != "install_plan_json" {
		t.Fatalf("expected plan-time replacement, got %#v", resp.RequiresReplace)
	}
}

func TestDeleteSkipsSupersededGeneration(t *testing.T) {
	ctx := context.Background()
	resourceUnderTest := &GatewayResource{}
	var schemaResp resource.SchemaResponse
	resourceUnderTest.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	input := validGatewayPlan()
	input.GenerationID = types.StringValue("old-generation")
	input.Tags = types.MapNull(types.StringType)
	state, err := DeriveGatewayState(ctx, &input)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	state.RollbackRouteTargetsJSON = types.StringValue(`{"rtb-private-a":{"destination_cidr":"0.0.0.0/0","target":"nat-previous"}}`)
	superseded := true
	rollbackCalls := 0
	cleanupCalls := 0
	resourceUnderTest.readerFactory = func(context.Context, string) (Reader, error) {
		return fakeReader{generationSuperseded: &superseded}, nil
	}
	resourceUnderTest.rollbackerFactory = func(context.Context, string) (Rollbacker, error) {
		return lifecycleRollbacker{calls: &rollbackCalls}, nil
	}
	resourceUnderTest.cleanerFactory = func(context.Context, string) (Cleaner, error) {
		return lifecycleCleaner{calls: &cleanupCalls}, nil
	}
	req := resource.DeleteRequest{State: tfsdk.State{Schema: schemaResp.Schema}}
	if diags := req.State.Set(ctx, state); diags.HasError() {
		t.Fatalf("set state: %v", diags)
	}
	var resp resource.DeleteResponse
	resourceUnderTest.Delete(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("delete superseded generation: %v", resp.Diagnostics)
	}
	if rollbackCalls != 0 || cleanupCalls != 0 {
		t.Fatalf("superseded generation touched current infrastructure: rollback=%d cleanup=%d", rollbackCalls, cleanupCalls)
	}
}

func TestFailedCreateRetryUsesFreshGenerationAndPlansNoFurtherReplacement(t *testing.T) {
	ctx := context.Background()
	var schemaResp resource.SchemaResponse
	resourceUnderTest := &GatewayResource{}
	resourceUnderTest.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	input := validGatewayPlan()
	input.Tags = types.MapNull(types.StringType)
	planned, err := DeriveGatewayState(ctx, &input)
	if err != nil {
		t.Fatalf("derive plan: %v", err)
	}
	planned.GenerationID = types.StringUnknown()
	plan := tfsdk.Plan{Schema: schemaResp.Schema}
	if diags := plan.Set(ctx, planned); diags.HasError() {
		t.Fatalf("set create plan: %v", diags)
	}
	installer := &lifecycleInstaller{failures: 1}
	cleanupCalls := 0
	resourceUnderTest.installerFactory = func(context.Context, string) (Installer, error) { return installer, nil }
	resourceUnderTest.cleanerFactory = func(context.Context, string) (Cleaner, error) {
		return lifecycleCleaner{calls: &cleanupCalls}, nil
	}

	first := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	resourceUnderTest.Create(ctx, resource.CreateRequest{Plan: plan}, &first)
	if !first.Diagnostics.HasError() || cleanupCalls != 1 {
		t.Fatalf("failed create should report error and clean up once: diagnostics=%v cleanup=%d", first.Diagnostics, cleanupCalls)
	}
	second := resource.CreateResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	resourceUnderTest.Create(ctx, resource.CreateRequest{Plan: plan}, &second)
	if second.Diagnostics.HasError() {
		t.Fatalf("retry create: %v", second.Diagnostics)
	}
	if len(installer.plans) != 2 || installer.plans[0].GenerationID == installer.plans[1].GenerationID {
		t.Fatalf("retry must use a fresh generation: %#v", installer.plans)
	}
	if installer.plans[0].Pools[0].ASGName == installer.plans[1].Pools[0].ASGName {
		t.Fatalf("retry reused physical pool name: %#v", installer.plans)
	}
	var recovered GatewayResourceModel
	if diags := second.State.Get(ctx, &recovered); diags.HasError() {
		t.Fatalf("read recovered state: %v", diags)
	}

	stablePlan := tfsdk.Plan{Schema: schemaResp.Schema}
	if diags := stablePlan.Set(ctx, recovered); diags.HasError() {
		t.Fatalf("set stable plan: %v", diags)
	}
	stableState := tfsdk.State{Schema: schemaResp.Schema}
	if diags := stableState.Set(ctx, recovered); diags.HasError() {
		t.Fatalf("set stable state: %v", diags)
	}
	modifyReq := resource.ModifyPlanRequest{
		Config: tfsdk.Config{Raw: stablePlan.Raw, Schema: schemaResp.Schema},
		State:  stableState,
		Plan:   stablePlan,
	}
	modifyResp := resource.ModifyPlanResponse{Plan: stablePlan}
	resourceUnderTest.ModifyPlan(ctx, modifyReq, &modifyResp)
	if modifyResp.Diagnostics.HasError() || len(modifyResp.RequiresReplace) != 0 {
		t.Fatalf("recovered generation should plan no further replacement: diagnostics=%v replace=%#v", modifyResp.Diagnostics, modifyResp.RequiresReplace)
	}
}

type lifecycleRollbacker struct{ calls *int }

func (f lifecycleRollbacker) RestoreRoutes(context.Context, []awsinstall.RollbackRoute) error {
	(*f.calls)++
	return nil
}

type lifecycleCleaner struct{ calls *int }

func (f lifecycleCleaner) Cleanup(context.Context, installplan.Plan, awsinstall.CleanupInputs) error {
	(*f.calls)++
	return nil
}

type lifecycleInstaller struct {
	failures int
	plans    []installplan.Plan
}

func (f *lifecycleInstaller) Install(_ context.Context, plan installplan.Plan, _ awsinstall.Inputs) (awsinstall.Result, error) {
	f.plans = append(f.plans, plan)
	if f.failures > 0 {
		f.failures--
		return awsinstall.Result{}, errors.New("injected create failure")
	}
	return awsinstall.Result{}, nil
}

func (f *lifecycleInstaller) UpdateCapacity(context.Context, installplan.Plan) error { return nil }
func (f *lifecycleInstaller) UpdatePools(context.Context, installplan.Plan, map[string]string) error {
	return nil
}
func (f *lifecycleInstaller) ReconcileInfrastructure(context.Context, installplan.Plan) error {
	return nil
}

func TestCapacityOnlyUpdateIgnoresPoolCapacity(t *testing.T) {
	statePlan := validGatewayPlan()
	statePlan.DesiredCapacity = types.Int64Value(2)
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	nextPlan := validGatewayPlan()
	nextPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	nextPlan.DesiredCapacity = types.Int64Value(3)
	nextPlan.MaxSize = types.Int64Value(5)
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if !capacityOnlyUpdate(state, next) {
		t.Fatal("expected capacity-only update")
	}

	next.InstanceType = types.StringValue("t3.medium")
	next, err = DeriveGatewayState(context.Background(), &next)
	if err != nil {
		t.Fatalf("derive changed instance type: %v", err)
	}
	if capacityOnlyUpdate(state, next) {
		t.Fatal("instance type change must not be treated as capacity-only")
	}
}

func TestGatewayReplacementRequiredForAgentBinaryURLChange(t *testing.T) {
	statePlan := validGatewayPlan()
	statePlan.AgentBinaryURL = types.StringValue("https://example.invalid/old-agent")
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	nextPlan := validGatewayPlan()
	nextPlan.AgentBinaryURL = types.StringValue("https://example.invalid/new-agent")
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if !gatewayReplacementRequired(state, next) {
		t.Fatal("agent_binary_url change must require replacement")
	}

	capacityPlan := statePlan
	capacityPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	capacityPlan.DesiredCapacity = types.Int64Value(3)
	capacity, err := DeriveGatewayState(context.Background(), &capacityPlan)
	if err != nil {
		t.Fatalf("derive capacity update: %v", err)
	}
	if gatewayReplacementRequired(state, capacity) {
		t.Fatal("capacity-only change should not require replacement")
	}
}

func TestGatewayReplacementRequiredForBetterNATVersionChange(t *testing.T) {
	statePlan := validGatewayPlan()
	statePlan.AgentBinaryURL = types.StringValue("https://example.invalid/agent")
	statePlan.AgentBinarySHA256 = types.StringValue("old-agent-sha")
	statePlan.CLIBinaryURL = types.StringValue("https://example.invalid/cli")
	statePlan.CLIBinarySHA256 = types.StringValue("old-cli-sha")
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}

	nextPlan := validGatewayPlan()
	nextPlan.BetterNATVersion = types.StringValue("v0.1.0-alpha.2")
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if !gatewayReplacementRequired(state, next) {
		t.Fatal("betternat_version change must require replacement")
	}
}

func TestGatewayReplacementNotRequiredForProviderInfrastructureRevisionChange(t *testing.T) {
	statePlan := validGatewayPlan()
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	state.ProviderInfraRevision = types.StringValue("2026-06-01-legacy")

	nextPlan := validGatewayPlan()
	nextPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if gatewayReplacementRequired(state, next) {
		t.Fatal("provider-owned infrastructure revision change should reconcile in-place")
	}
}

func TestGatewayReplacementNotRequiredForManagedEIPRetentionChange(t *testing.T) {
	statePlan := validGatewayPlan()
	statePlan.RetainManagedEIPs = types.BoolValue(false)
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}

	nextPlan := validGatewayPlan()
	nextPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	nextPlan.RetainManagedEIPs = types.BoolValue(true)
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if gatewayReplacementRequired(state, next) {
		t.Fatal("managed EIP retention must be a state-only in-place change")
	}
}

func TestGatewayReplacementRequiredForExternalEIPChange(t *testing.T) {
	statePlan := validGatewayPlan()
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}

	nextPlan := validGatewayPlan()
	nextPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	nextPlan.EIPAllocationIDs = mustStringMap(map[string]string{"us-west-2a": "eipalloc-external"})
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}

	if !gatewayReplacementRequired(state, next) {
		t.Fatal("changing EIP ownership must require replacement")
	}
}

func TestDeriveGatewayStatePreservesPeerAPIAuthToken(t *testing.T) {
	statePlan := validGatewayPlan()
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	nextPlan := validGatewayPlan()
	nextPlan.PeerAPIAuthToken = state.PeerAPIAuthToken
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}
	if next.PeerAPIAuthToken.ValueString() != state.PeerAPIAuthToken.ValueString() {
		t.Fatalf("peer api auth token should be preserved across derives")
	}
	if !strings.Contains(next.UserData.ValueString(), `"auth_token":"`+state.PeerAPIAuthToken.ValueString()+`"`) {
		t.Fatalf("user data should render preserved peer token")
	}
}

func TestGatewayReplacementRequiredForHATimingChange(t *testing.T) {
	statePlan := validGatewayPlan()
	state, err := DeriveGatewayState(context.Background(), &statePlan)
	if err != nil {
		t.Fatalf("derive state: %v", err)
	}
	nextPlan := validGatewayPlan()
	nextPlan.HALeaseTTLSeconds = types.Int64Value(20)
	next, err := DeriveGatewayState(context.Background(), &nextPlan)
	if err != nil {
		t.Fatalf("derive next: %v", err)
	}
	if !gatewayReplacementRequired(state, next) {
		t.Fatal("HA timing change must require replacement")
	}
}

func TestDeriveGatewayStateRejectsUnsupportedCloud(t *testing.T) {
	plan := validGatewayPlan()
	plan.Cloud = types.StringValue("gcp")
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected unsupported cloud error")
	}
}

func TestDeriveGatewayStateRequiresMatchingPublicSubnetAZ(t *testing.T) {
	plan := validGatewayPlan()
	plan.PrivateRouteTableIDs = mustMapList(map[string][]string{
		"us-west-2c": []string{"rtb-private-c"},
	})
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected matching public subnet AZ error")
	}
}

func TestDeriveGatewayStateRejectsUnsupportedAMIChannel(t *testing.T) {
	plan := validGatewayPlan()
	plan.AMIChannel = types.StringValue("nightly")
	_, err := DeriveGatewayState(context.Background(), &plan)
	if err == nil {
		t.Fatal("expected unsupported ami channel error")
	}
}

func TestRollbackTargetsUnknown(t *testing.T) {
	if !rollbackTargetsUnknown(`{"rtb-a":{"destination_cidr":"0.0.0.0/0","target":"unknown"}}`) {
		t.Fatal("unknown rollback target should be unsafe")
	}
	if rollbackTargetsUnknown(`{"rtb-a":{"destination_cidr":"0.0.0.0/0","target":"nat-old"}}`) {
		t.Fatal("concrete rollback target should be safe")
	}
}

func TestParseRollbackRoutes(t *testing.T) {
	routes, err := parseRollbackRoutes(`{"rtb-a":{"destination_cidr":"0.0.0.0/0","target":"nat-old"}}`)
	if err != nil {
		t.Fatalf("parse rollback routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("unexpected routes: %#v", routes)
	}
	if routes[0].RouteTableID != "rtb-a" || routes[0].DestinationCIDR != "0.0.0.0/0" || routes[0].Target != "nat-old" {
		t.Fatalf("unexpected route: %#v", routes[0])
	}
}

func TestParseRollbackRoutesRejectsUnknown(t *testing.T) {
	_, err := parseRollbackRoutes(`{"rtb-a":{"destination_cidr":"0.0.0.0/0","target":"unknown"}}`)
	if err == nil {
		t.Fatal("expected unknown rollback target error")
	}
}

func TestGatewayInstanceIDs(t *testing.T) {
	state := validGatewayPlan()
	state.ActiveInstanceIDs = mustStringMap(map[string]string{"us-west-2a": "i-active"})
	state.StandbyInstanceIDs = mustStringMap(map[string]string{"us-west-2a": "i-standby"})

	ids, err := gatewayInstanceIDs(context.Background(), state)
	if err != nil {
		t.Fatalf("gateway instance ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("unexpected ids: %#v", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen["i-active"] || !seen["i-standby"] {
		t.Fatalf("unexpected ids: %#v", ids)
	}
}

func TestReadGatewayState(t *testing.T) {
	state := validGatewayPlan()
	derived, err := DeriveGatewayState(context.Background(), &state)
	if err != nil {
		t.Fatalf("derive gateway state: %v", err)
	}
	factory := func(context.Context, string) (Reader, error) {
		return fakeReader{
			result: awsinstall.ReadResult{
				RouteTargets:              map[string]string{"rtb-private-a": "i-active"},
				EgressPublicIPs:           map[string]string{"us-west-2a": "203.0.113.10"},
				PublicIdentityInstanceIDs: map[string]string{"us-west-2a": "i-active"},
			},
		}, nil
	}

	if err := readGatewayState(context.Background(), &derived, factory); err != nil {
		t.Fatalf("read gateway state: %v", err)
	}
	if derived.Status.ValueString() != "active" {
		t.Fatalf("unexpected status: %s", derived.Status.ValueString())
	}
	if !strings.Contains(derived.ControlPlaneStatusJSON.ValueString(), `"rtb-private-a":"i-active"`) {
		t.Fatalf("missing route target status: %s", derived.ControlPlaneStatusJSON.ValueString())
	}
	publicIPs, err := mapStrings(context.Background(), derived.EgressPublicIPs)
	if err != nil {
		t.Fatalf("public ips: %v", err)
	}
	if publicIPs["us-west-2a"] != "203.0.113.10" {
		t.Fatalf("unexpected public ips: %#v", publicIPs)
	}
}
