package tfprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/betternat/betternat/internal/bootstrap"
	"github.com/betternat/betternat/internal/installplan"
	"github.com/betternat/betternat/internal/provider"
)

var _ resource.Resource = (*GatewayResource)(nil)
var _ resource.ResourceWithConfigure = (*GatewayResource)(nil)

type GatewayResource struct {
	installerFactory InstallerFactory
}

func NewGatewayResource() resource.Resource {
	return NewGatewayResourceWithInstaller(defaultInstallerFactory)
}

func NewGatewayResourceWithInstaller(factory InstallerFactory) resource.Resource {
	return &GatewayResource{installerFactory: factory}
}

type GatewayResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Cloud                    types.String `tfsdk:"cloud"`
	Region                   types.String `tfsdk:"region"`
	VPCID                    types.String `tfsdk:"vpc_id"`
	AMIID                    types.String `tfsdk:"ami_id"`
	InstanceType             types.String `tfsdk:"instance_type"`
	PublicSubnetIDs          types.Map    `tfsdk:"public_subnet_ids"`
	PrivateRouteTableIDs     types.Map    `tfsdk:"private_route_table_ids"`
	PrivateCIDRs             types.List   `tfsdk:"private_cidrs"`
	DatapathEngine           types.String `tfsdk:"datapath_engine"`
	FallbackDatapathEngine   types.String `tfsdk:"fallback_datapath_engine"`
	StableEgressIP           types.Bool   `tfsdk:"stable_egress_ip"`
	PrometheusEnabled        types.Bool   `tfsdk:"prometheus_enabled"`
	LeaseTableName           types.String `tfsdk:"lease_table_name"`
	AgentConfigJSON          types.String `tfsdk:"agent_config_json"`
	AgentConfigHash          types.String `tfsdk:"agent_config_hash"`
	UserData                 types.String `tfsdk:"user_data"`
	InstallPlanJSON          types.String `tfsdk:"install_plan_json"`
	ManagedRouteTableIDs     types.List   `tfsdk:"managed_route_table_ids"`
	EgressPublicIPs          types.Map    `tfsdk:"egress_public_ips"`
	ActiveInstanceIDs        types.Map    `tfsdk:"active_instance_ids"`
	StandbyInstanceIDs       types.Map    `tfsdk:"standby_instance_ids"`
	RollbackRouteTargetsJSON types.String `tfsdk:"rollback_route_targets_json"`
	Status                   types.String `tfsdk:"status"`
}

func (r *GatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gateway"
}

func (r *GatewayResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected BetterNAT provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.installerFactory = data.InstallerFactory
}

func (r *GatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT gateway resource. v0 installs appliance infrastructure and records runtime metadata.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
			},
			"name": schema.StringAttribute{
				Required: true,
			},
			"cloud": schema.StringAttribute{
				Required: true,
			},
			"region": schema.StringAttribute{
				Required: true,
			},
			"vpc_id": schema.StringAttribute{
				Required: true,
			},
			"ami_id": schema.StringAttribute{
				Optional: true,
			},
			"instance_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"public_subnet_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Required:    true,
			},
			"private_route_table_ids": schema.MapAttribute{
				ElementType: types.ListType{ElemType: types.StringType},
				Required:    true,
			},
			"private_cidrs": schema.ListAttribute{
				ElementType: types.StringType,
				Required:    true,
			},
			"datapath_engine": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"fallback_datapath_engine": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"stable_egress_ip": schema.BoolAttribute{
				Optional: true,
				Computed: true,
			},
			"prometheus_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
			},
			"lease_table_name": schema.StringAttribute{
				Computed: true,
			},
			"agent_config_json": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
			},
			"agent_config_hash": schema.StringAttribute{
				Computed: true,
			},
			"user_data": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
			},
			"install_plan_json": schema.StringAttribute{
				Computed: true,
			},
			"managed_route_table_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"egress_public_ips": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"active_instance_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"standby_instance_ids": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"rollback_route_targets_json": schema.StringAttribute{
				Computed: true,
			},
			"status": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (r *GatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan GatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(applyGatewayPlan(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := installGatewayState(ctx, &plan, r.installerFactory); err != nil {
		resp.Diagnostics.AddError("Install BetterNAT gateway", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *GatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *GatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan GatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(applyGatewayPlan(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := installGatewayState(ctx, &plan, r.installerFactory); err != nil {
		resp.Diagnostics.AddError("Update BetterNAT gateway", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *GatewayResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

func applyGatewayPlan(ctx context.Context, plan *GatewayResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	derived, err := DeriveGatewayState(ctx, plan)
	if err != nil {
		diags.AddError("Invalid BetterNAT gateway plan", err.Error())
		return diags
	}
	*plan = derived
	return diags
}

func DeriveGatewayState(ctx context.Context, plan *GatewayResourceModel) (GatewayResourceModel, error) {
	result := *plan
	if plan.Name.IsNull() || plan.Name.IsUnknown() || plan.Name.ValueString() == "" {
		return GatewayResourceModel{}, fmt.Errorf("name is required")
	}
	if plan.Cloud.ValueString() != "aws" {
		return GatewayResourceModel{}, fmt.Errorf("only cloud %q is supported in v0", "aws")
	}
	if plan.Region.IsNull() || plan.Region.IsUnknown() || plan.Region.ValueString() == "" {
		return GatewayResourceModel{}, fmt.Errorf("region is required")
	}
	if plan.VPCID.IsNull() || plan.VPCID.IsUnknown() || plan.VPCID.ValueString() == "" {
		return GatewayResourceModel{}, fmt.Errorf("vpc_id is required")
	}
	privateCIDRs, err := listStrings(ctx, plan.PrivateCIDRs)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("private_cidrs: %w", err)
	}
	routeTablesByAZ, err := mapListStrings(ctx, plan.PrivateRouteTableIDs)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("private_route_table_ids: %w", err)
	}
	publicSubnetsByAZ, err := mapStrings(ctx, plan.PublicSubnetIDs)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("public_subnet_ids: %w", err)
	}
	if len(publicSubnetsByAZ) == 0 {
		return GatewayResourceModel{}, fmt.Errorf("at least one public subnet is required")
	}
	if len(routeTablesByAZ) == 0 {
		return GatewayResourceModel{}, fmt.Errorf("at least one private route table is required")
	}

	datapathEngine := stringDefault(plan.DatapathEngine, "loxilb")
	fallbackEngine := stringDefault(plan.FallbackDatapathEngine, "nftables")
	if datapathEngine != "loxilb" && datapathEngine != "nftables" {
		return GatewayResourceModel{}, fmt.Errorf("unsupported datapath_engine %q", datapathEngine)
	}
	if fallbackEngine != "" && fallbackEngine != "nftables" {
		return GatewayResourceModel{}, fmt.Errorf("unsupported fallback_datapath_engine %q", fallbackEngine)
	}
	for az := range routeTablesByAZ {
		if _, ok := publicSubnetsByAZ[az]; !ok {
			return GatewayResourceModel{}, fmt.Errorf("private_route_table_ids includes AZ %q without a matching public_subnet_ids entry", az)
		}
	}
	stableEgressIP := boolDefault(plan.StableEgressIP, true)
	prometheusEnabled := boolDefault(plan.PrometheusEnabled, true)
	instanceType := stringDefault(plan.InstanceType, "t3.small")
	leaseTable := "betternat-" + plan.Name.ValueString() + "-leases"

	azs := sortedKeys(routeTablesByAZ)
	firstAZ := azs[0]
	gatewaySpec := provider.GatewaySpec{
		Name:         plan.Name.ValueString(),
		Cloud:        plan.Cloud.ValueString(),
		Region:       plan.Region.ValueString(),
		PrivateCIDRs: privateCIDRs,
		Datapath: provider.DatapathSpec{
			Engine:         datapathEngine,
			FallbackEngine: fallbackEngine,
			SNATInterface:  "ens5",
		},
		HA: provider.HASpec{
			Enabled:               true,
			LeaseTable:            leaseTable,
			SharedEIPAllocationID: "auto",
		},
		Observability: provider.ObservabilitySpec{
			PrometheusListenAddress: "0.0.0.0",
			PrometheusListenPort:    9108,
			OutboundProbeURL:        "https://checkip.amazonaws.com",
		},
	}
	if !stableEgressIP {
		gatewaySpec.HA.SharedEIPAllocationID = ""
	}
	if !prometheusEnabled {
		gatewaySpec.Observability.PrometheusListenPort = 0
	}
	agentConfig, err := provider.RenderAgentConfig(gatewaySpec, provider.ApplianceSpec{
		HAGroupID:            plan.Name.ValueString() + "-" + firstAZ,
		InstanceID:           "auto",
		AvailabilityZone:     firstAZ,
		PrimaryInterface:     "ens5",
		RouteTableIDs:        routeTablesByAZ[firstAZ],
		RouteDestinationCIDR: "0.0.0.0/0",
	})
	if err != nil {
		return GatewayResourceModel{}, err
	}
	configBytes, err := json.Marshal(agentConfig)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("marshal agent config: %w", err)
	}
	configHash := sha256.Sum256(configBytes)
	userData, err := bootstrap.RenderUserData(bootstrap.Spec{
		AgentConfig: string(configBytes),
	})
	if err != nil {
		return GatewayResourceModel{}, err
	}
	managedRouteTableIDs := flattenRouteTableIDs(routeTablesByAZ)
	rollbackJSON, err := plannedRollbackJSON(routeTablesByAZ, "0.0.0.0/0")
	if err != nil {
		return GatewayResourceModel{}, err
	}
	installPlan, err := installplan.Build(installplan.Input{
		Name:                 plan.Name.ValueString(),
		Region:               plan.Region.ValueString(),
		VPCID:                plan.VPCID.ValueString(),
		PublicSubnetIDs:      publicSubnetsByAZ,
		PrivateRouteTableIDs: routeTablesByAZ,
		StableEgressIP:       stableEgressIP,
		LeaseTableName:       leaseTable,
		AgentConfigHash:      hex.EncodeToString(configHash[:]),
		AMIID:                stringDefault(plan.AMIID, ""),
		InstanceType:         instanceType,
	})
	if err != nil {
		return GatewayResourceModel{}, err
	}
	installPlanBytes, err := json.Marshal(installPlan)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("marshal install plan: %w", err)
	}

	result.ID = types.StringValue(plan.Name.ValueString())
	result.DatapathEngine = types.StringValue(datapathEngine)
	result.FallbackDatapathEngine = types.StringValue(fallbackEngine)
	result.InstanceType = types.StringValue(instanceType)
	result.StableEgressIP = types.BoolValue(stableEgressIP)
	result.PrometheusEnabled = types.BoolValue(prometheusEnabled)
	result.LeaseTableName = types.StringValue(leaseTable)
	result.AgentConfigJSON = types.StringValue(string(configBytes))
	result.AgentConfigHash = types.StringValue(hex.EncodeToString(configHash[:]))
	result.UserData = types.StringValue(userData)
	result.InstallPlanJSON = types.StringValue(string(installPlanBytes))
	result.ManagedRouteTableIDs = mustStringList(managedRouteTableIDs)
	result.EgressPublicIPs = mustStringMap(emptyByAZ(publicSubnetsByAZ))
	result.ActiveInstanceIDs = mustStringMap(emptyByAZ(publicSubnetsByAZ))
	result.StandbyInstanceIDs = mustStringMap(emptyByAZ(publicSubnetsByAZ))
	result.RollbackRouteTargetsJSON = types.StringValue(rollbackJSON)
	result.Status = types.StringValue("planned")
	return result, nil
}

func plannedRollbackJSON(routeTablesByAZ map[string][]string, destinationCIDR string) (string, error) {
	entries := make(map[string]map[string]string)
	for _, routeTableID := range flattenRouteTableIDs(routeTablesByAZ) {
		entries[routeTableID] = map[string]string{
			"destination_cidr": destinationCIDR,
			"target":           "unknown",
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal planned rollback targets: %w", err)
	}
	return string(data), nil
}

func stringDefault(value types.String, fallback string) string {
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return fallback
	}
	return value.ValueString()
}

func boolDefault(value types.Bool, fallback bool) bool {
	if value.IsNull() || value.IsUnknown() {
		return fallback
	}
	return value.ValueBool()
}

func listStrings(ctx context.Context, value types.List) ([]string, error) {
	var out []string
	diags := value.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
	}
	return out, nil
}

func mapStrings(ctx context.Context, value types.Map) (map[string]string, error) {
	var out map[string]string
	diags := value.ElementsAs(ctx, &out, false)
	if diags.HasError() {
		return nil, fmt.Errorf("%s", diags.Errors()[0].Detail())
	}
	return out, nil
}

func mapListStrings(ctx context.Context, value types.Map) (map[string][]string, error) {
	out := map[string][]string{}
	for key, raw := range value.Elements() {
		list, ok := raw.(types.List)
		if !ok {
			return nil, fmt.Errorf("value for %q must be a list of strings", key)
		}
		values, err := listStrings(ctx, list)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = values
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func flattenRouteTableIDs(routeTablesByAZ map[string][]string) []string {
	var ids []string
	for _, az := range sortedKeys(routeTablesByAZ) {
		ids = append(ids, routeTablesByAZ[az]...)
	}
	sort.Strings(ids)
	return ids
}

func emptyByAZ(publicSubnetsByAZ map[string]string) map[string]string {
	result := make(map[string]string, len(publicSubnetsByAZ))
	for az := range publicSubnetsByAZ {
		result[az] = ""
	}
	return result
}

func mustStringList(values []string) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	result, diags := types.ListValue(types.StringType, elements)
	if diags.HasError() {
		panic(diags.Errors()[0].Detail())
	}
	return result
}

func mustStringMap(values map[string]string) types.Map {
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	result, diags := types.MapValue(types.StringType, elements)
	if diags.HasError() {
		panic(diags.Errors()[0].Detail())
	}
	return result
}
