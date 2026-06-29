package tfprovider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	awsinstall "github.com/nowakeai/betternat/internal/install/aws"
	"github.com/nowakeai/betternat/internal/installplan"
)

func TestProviderExposesCloudSpecificSurface(t *testing.T) {
	provider := &Provider{readerFactory: defaultReaderFactory}

	resources := provider.Resources(context.Background())
	if len(resources) != 2 {
		t.Fatalf("unexpected resource count: %d", len(resources))
	}
	resourceNames := map[string]bool{}
	for _, newResource := range resources {
		resourceNames[resourceTypeName(t, newResource())] = true
	}
	for _, want := range []string{"betternat_aws_gateway", "betternat_gcp_gateway"} {
		if !resourceNames[want] {
			t.Fatalf("missing resource %s in %#v", want, resourceNames)
		}
	}

	dataSources := provider.DataSources(context.Background())
	if len(dataSources) != 3 {
		t.Fatalf("unexpected data source count: %d", len(dataSources))
	}
	names := map[string]bool{}
	for _, newDataSource := range dataSources {
		names[dataSourceTypeName(t, newDataSource())] = true
	}
	for _, want := range []string{"betternat_runtime_artifacts", "betternat_aws_gateway_status", "betternat_gcp_gateway_status"} {
		if !names[want] {
			t.Fatalf("missing data source %s in %#v", want, names)
		}
	}
	if resourceNames["betternat_gateway"] {
		t.Fatal("old betternat_gateway resource must not be exposed")
	}
}

func TestRuntimeArtifacts(t *testing.T) {
	artifacts, err := runtimeArtifacts("v0.2.1", "linux", "arm64")
	if err != nil {
		t.Fatalf("runtime artifacts: %v", err)
	}
	if !strings.Contains(artifacts.AgentBinaryURL, "betternat-agent_v0.2.1_linux_arm64") {
		t.Fatalf("unexpected agent artifact URL: %s", artifacts.AgentBinaryURL)
	}
	if artifacts.AgentBinarySHA256 != "b46d9c08cfd23023281252bf20c880e4ac07fe7fcb4847cd82a7bd41b05cda7d" {
		t.Fatalf("unexpected agent checksum: %s", artifacts.AgentBinarySHA256)
	}
}

func TestRuntimeArtifactsRejectUnsupportedValues(t *testing.T) {
	tests := []struct {
		name    string
		version string
		os      string
		arch    string
		want    string
	}{
		{name: "version prefix", version: "0.1.0", os: "linux", arch: "arm64", want: "must start with v"},
		{name: "version", version: "v9.9.9", os: "linux", arch: "arm64", want: "unsupported betternat_version"},
		{name: "os", version: "v0.1.0", os: "darwin", arch: "arm64", want: "unsupported runtime artifact os"},
		{name: "arch", version: "v0.1.0", os: "linux", arch: "riscv64", want: "unsupported runtime artifact architecture"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runtimeArtifacts(tt.version, tt.os, tt.arch)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestReadAWSGatewayStatus(t *testing.T) {
	plan := installplan.Plan{
		Name:                  "prod-egress",
		Region:                "us-west-2",
		CoordinationTableName: "betternat-prod-egress-coordination",
		ManagedRoutes: []installplan.ManagedRoute{
			{RouteTableID: "rtb-private-a", DestinationCIDR: "0.0.0.0/0"},
		},
	}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal install plan: %v", err)
	}
	config := AWSGatewayStatusDataSourceModel{
		Name:            types.StringValue("prod-egress"),
		Region:          types.StringValue("us-west-2"),
		InstallPlanJSON: types.StringValue(string(planBytes)),
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

	state, err := readAWSGatewayStatus(context.Background(), config, factory)
	if err != nil {
		t.Fatalf("read aws gateway status: %v", err)
	}
	if state.Status.ValueString() != "active" {
		t.Fatalf("unexpected status: %s", state.Status.ValueString())
	}
	if state.CoordinationTableName.ValueString() != "betternat-prod-egress-coordination" {
		t.Fatalf("unexpected coordination table: %s", state.CoordinationTableName.ValueString())
	}
	routes, err := mapStrings(context.Background(), state.RouteTargets)
	if err != nil {
		t.Fatalf("route targets: %v", err)
	}
	if routes["rtb-private-a"] != "i-active" {
		t.Fatalf("unexpected routes: %#v", routes)
	}
}

func TestReadAWSGatewayStatusRejectsMismatchedPlan(t *testing.T) {
	plan := installplan.Plan{Name: "other", Region: "us-west-2"}
	planBytes, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal install plan: %v", err)
	}
	config := AWSGatewayStatusDataSourceModel{
		Name:            types.StringValue("prod-egress"),
		Region:          types.StringValue("us-west-2"),
		InstallPlanJSON: types.StringValue(string(planBytes)),
	}
	_, err = readAWSGatewayStatus(context.Background(), config, func(context.Context, string) (Reader, error) {
		return fakeReader{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func resourceTypeName(t *testing.T, r resource.Resource) string {
	t.Helper()
	var resp resource.MetadataResponse
	r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "betternat"}, &resp)
	return resp.TypeName
}

func dataSourceTypeName(t *testing.T, d datasource.DataSource) string {
	t.Helper()
	var resp datasource.MetadataResponse
	d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "betternat"}, &resp)
	return resp.TypeName
}
