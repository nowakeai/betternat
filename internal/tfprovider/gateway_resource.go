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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/betternat/betternat/internal/bootstrap"
	"github.com/betternat/betternat/internal/installplan"
	"github.com/betternat/betternat/internal/provider"
)

var _ resource.Resource = (*GatewayResource)(nil)
var _ resource.ResourceWithConfigure = (*GatewayResource)(nil)

type GatewayResource struct {
	installerFactory  InstallerFactory
	rollbackerFactory RollbackerFactory
	cleanerFactory    CleanerFactory
	readerFactory     ReaderFactory
}

func NewGatewayResource() resource.Resource {
	return NewGatewayResourceWithInstaller(defaultInstallerFactory)
}

func NewGatewayResourceWithInstaller(factory InstallerFactory) resource.Resource {
	return NewGatewayResourceWithFactories(factory, defaultRollbackerFactory, defaultCleanerFactory, defaultReaderFactory)
}

func NewGatewayResourceWithFactories(installerFactory InstallerFactory, rollbackerFactory RollbackerFactory, cleanerFactory CleanerFactory, readerFactory ReaderFactory) resource.Resource {
	return &GatewayResource{installerFactory: installerFactory, rollbackerFactory: rollbackerFactory, cleanerFactory: cleanerFactory, readerFactory: readerFactory}
}

type GatewayResourceModel struct {
	ID                       types.String `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	Cloud                    types.String `tfsdk:"cloud"`
	Region                   types.String `tfsdk:"region"`
	VPCID                    types.String `tfsdk:"vpc_id"`
	AMIID                    types.String `tfsdk:"ami_id"`
	AMIChannel               types.String `tfsdk:"ami_channel"`
	InstanceType             types.String `tfsdk:"instance_type"`
	UseSpot                  types.Bool   `tfsdk:"use_spot"`
	AgentBinaryURL           types.String `tfsdk:"agent_binary_url"`
	LoxiCMDBinaryURL         types.String `tfsdk:"loxicmd_binary_url"`
	PublicSubnetIDs          types.Map    `tfsdk:"public_subnet_ids"`
	PrivateRouteTableIDs     types.Map    `tfsdk:"private_route_table_ids"`
	PrivateCIDRs             types.List   `tfsdk:"private_cidrs"`
	DatapathEngine           types.String `tfsdk:"datapath_engine"`
	FallbackDatapathEngine   types.String `tfsdk:"fallback_datapath_engine"`
	StableEgressIP           types.Bool   `tfsdk:"stable_egress_ip"`
	PrometheusEnabled        types.Bool   `tfsdk:"prometheus_enabled"`
	RouteMode                types.String `tfsdk:"route_mode"`
	RouteDestinationCIDR     types.String `tfsdk:"route_destination_cidr"`
	RouteTargetType          types.String `tfsdk:"route_target_type"`
	RollbackOnDestroy        types.Bool   `tfsdk:"rollback_on_destroy"`
	AllowDestroyNoRollback   types.Bool   `tfsdk:"allow_destroy_without_rollback"`
	Tags                     types.Map    `tfsdk:"tags"`
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
	ControlPlaneStatusJSON   types.String `tfsdk:"control_plane_status_json"`
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
	r.rollbackerFactory = data.RollbackerFactory
	r.cleanerFactory = data.CleanerFactory
	r.readerFactory = data.ReaderFactory
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
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("aws"),
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
			"ami_channel": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("stable"),
			},
			"instance_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("t3.small"),
			},
			"use_spot": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"agent_binary_url": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
			},
			"loxicmd_binary_url": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
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
				Default:  stringdefault.StaticString("loxilb"),
			},
			"fallback_datapath_engine": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("nftables"),
			},
			"stable_egress_ip": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"prometheus_enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"route_mode": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("replace_route"),
			},
			"route_destination_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0.0.0.0/0"),
			},
			"route_target_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("instance"),
			},
			"rollback_on_destroy": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"allow_destroy_without_rollback": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"tags": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
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
			"control_plane_status_json": schema.StringAttribute{
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
		if cleanupErr := cleanupGatewayResources(ctx, plan, r.cleanerFactory); cleanupErr != nil {
			resp.Diagnostics.AddWarning("Cleanup failed BetterNAT gateway create", cleanupErr.Error())
		}
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
	if err := readGatewayState(ctx, &state, r.readerFactory); err != nil {
		resp.Diagnostics.AddWarning("Read BetterNAT gateway state", err.Error())
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
		if cleanupErr := cleanupGatewayResources(ctx, plan, r.cleanerFactory); cleanupErr != nil {
			resp.Diagnostics.AddWarning("Cleanup failed BetterNAT gateway update", cleanupErr.Error())
		}
		resp.Diagnostics.AddError("Update BetterNAT gateway", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *GatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state GatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rollbackOnDestroy := boolDefault(state.RollbackOnDestroy, true)
	allowDestroyNoRollback := boolDefault(state.AllowDestroyNoRollback, false)
	if rollbackOnDestroy && !allowDestroyNoRollback && rollbackTargetsUnknown(state.RollbackRouteTargetsJSON.ValueString()) {
		resp.Diagnostics.AddError(
			"Refusing to destroy BetterNAT gateway without rollback targets",
			"rollback_on_destroy is true, but rollback_route_targets_json does not contain concrete previous route targets. Set allow_destroy_without_rollback = true only if you have manually restored or accepted the private route table state.",
		)
		return
	}
	if rollbackOnDestroy && !allowDestroyNoRollback {
		if err := rollbackGatewayRoutes(ctx, state, r.rollbackerFactory); err != nil {
			resp.Diagnostics.AddError("Rollback BetterNAT routes", err.Error())
			return
		}
	}
	if err := cleanupGatewayResources(ctx, state, r.cleanerFactory); err != nil {
		resp.Diagnostics.AddError("Cleanup BetterNAT gateway resources", err.Error())
		return
	}
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
	cloud := stringDefault(plan.Cloud, "aws")
	if cloud != "aws" {
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
	tags := map[string]string{}
	if !plan.Tags.IsNull() && !plan.Tags.IsUnknown() {
		tags, err = mapStrings(ctx, plan.Tags)
		if err != nil {
			return GatewayResourceModel{}, fmt.Errorf("tags: %w", err)
		}
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
	useSpot := boolDefault(plan.UseSpot, false)
	amiChannel := stringDefault(plan.AMIChannel, "stable")
	routeMode := stringDefault(plan.RouteMode, "replace_route")
	routeDestinationCIDR := stringDefault(plan.RouteDestinationCIDR, "0.0.0.0/0")
	routeTargetType := stringDefault(plan.RouteTargetType, "instance")
	rollbackOnDestroy := boolDefault(plan.RollbackOnDestroy, true)
	allowDestroyNoRollback := boolDefault(plan.AllowDestroyNoRollback, false)
	if amiChannel != "stable" && amiChannel != "candidate" && amiChannel != "dev" {
		return GatewayResourceModel{}, fmt.Errorf("unsupported ami_channel %q", amiChannel)
	}
	if routeMode != "replace_route" {
		return GatewayResourceModel{}, fmt.Errorf("unsupported route_mode %q", routeMode)
	}
	if routeTargetType != "instance" {
		return GatewayResourceModel{}, fmt.Errorf("unsupported route_target_type %q", routeTargetType)
	}
	leaseTable := "betternat-" + plan.Name.ValueString() + "-leases"

	azs := sortedKeys(routeTablesByAZ)
	firstAZ := azs[0]
	gatewaySpec := provider.GatewaySpec{
		Name:         plan.Name.ValueString(),
		Cloud:        cloud,
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
			RouteMode:             routeMode,
			RouteDestinationCIDR:  routeDestinationCIDR,
			RouteTargetType:       routeTargetType,
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
		RouteDestinationCIDR: routeDestinationCIDR,
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
		AgentConfig:      string(configBytes),
		AgentBinaryURL:   stringDefault(plan.AgentBinaryURL, ""),
		LoxiCMDBinaryURL: stringDefault(plan.LoxiCMDBinaryURL, ""),
	})
	if err != nil {
		return GatewayResourceModel{}, err
	}
	managedRouteTableIDs := flattenRouteTableIDs(routeTablesByAZ)
	rollbackJSON, err := plannedRollbackJSON(routeTablesByAZ, routeDestinationCIDR)
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
		AMIChannel:           amiChannel,
		InstanceType:         instanceType,
		UseSpot:              useSpot,
		RouteDestinationCIDR: routeDestinationCIDR,
		RouteTargetType:      routeTargetType,
		Tags:                 tags,
	})
	if err != nil {
		return GatewayResourceModel{}, err
	}
	installPlanBytes, err := json.Marshal(installPlan)
	if err != nil {
		return GatewayResourceModel{}, fmt.Errorf("marshal install plan: %w", err)
	}

	result.ID = types.StringValue(plan.Name.ValueString())
	result.Cloud = types.StringValue(cloud)
	result.AMIChannel = types.StringValue(amiChannel)
	result.DatapathEngine = types.StringValue(datapathEngine)
	result.FallbackDatapathEngine = types.StringValue(fallbackEngine)
	result.InstanceType = types.StringValue(instanceType)
	result.UseSpot = types.BoolValue(useSpot)
	result.StableEgressIP = types.BoolValue(stableEgressIP)
	result.PrometheusEnabled = types.BoolValue(prometheusEnabled)
	result.RouteMode = types.StringValue(routeMode)
	result.RouteDestinationCIDR = types.StringValue(routeDestinationCIDR)
	result.RouteTargetType = types.StringValue(routeTargetType)
	result.RollbackOnDestroy = types.BoolValue(rollbackOnDestroy)
	result.AllowDestroyNoRollback = types.BoolValue(allowDestroyNoRollback)
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
	result.ControlPlaneStatusJSON = types.StringValue("{}")
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

func rollbackTargetsUnknown(raw string) bool {
	if raw == "" {
		return true
	}
	var entries map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return true
	}
	if len(entries) == 0 {
		return true
	}
	for _, entry := range entries {
		target := entry["target"]
		if target == "" || target == "unknown" {
			return true
		}
	}
	return false
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
