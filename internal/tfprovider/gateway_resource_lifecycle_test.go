package tfprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/nowakeai/betternat/internal/install/aws"
)

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
