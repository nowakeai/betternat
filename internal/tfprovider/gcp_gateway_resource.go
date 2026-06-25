package tfprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gcompute "google.golang.org/api/compute/v1"

	gcpinstall "github.com/nowakeai/betternat/internal/install/gcp"
)

var _ resource.Resource = (*GCPGatewayResource)(nil)

type GCPGatewayResource struct{}

type GCPGatewayResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	ProjectID       types.String `tfsdk:"project_id"`
	Region          types.String `tfsdk:"region"`
	Zone            types.String `tfsdk:"zone"`
	Network         types.String `tfsdk:"network"`
	Subnetwork      types.String `tfsdk:"subnetwork"`
	ClientTag       types.String `tfsdk:"client_tag"`
	RouteName       types.String `tfsdk:"route_name"`
	RoutePriority   types.Int64  `tfsdk:"route_priority"`
	RouteDestRange  types.String `tfsdk:"route_destination_cidr"`
	MachineType     types.String `tfsdk:"machine_type"`
	ImageProject    types.String `tfsdk:"image_project"`
	ImageFamily     types.String `tfsdk:"image_family"`
	GatewayCount    types.Int64  `tfsdk:"gateway_count"`
	PrivateCIDRs    types.List   `tfsdk:"private_cidrs"`
	StartupScript   types.String `tfsdk:"startup_script"`
	GatewayStatuses types.Map    `tfsdk:"gateway_statuses"`
	EgressPublicIPs types.Map    `tfsdk:"egress_public_ips"`
	RouteTarget     types.String `tfsdk:"route_target"`
	Status          types.String `tfsdk:"status"`
}

func NewGCPGatewayResource() resource.Resource {
	return &GCPGatewayResource{}
}

func (r *GCPGatewayResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_gateway"
}

func (r *GCPGatewayResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT GCP alpha gateway resource. Manages GCE forwarding gateway VMs and a tagged default route. This alpha path validates GCP forwarding and route replacement only; it does not yet provide BetterNAT agent lease coordination or stable public IP handover.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Base name for provider-owned gateway instances.",
			},
			"project_id": schema.StringAttribute{Required: true},
			"region":     schema.StringAttribute{Required: true},
			"zone":       schema.StringAttribute{Required: true},
			"network": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Existing VPC network name.",
			},
			"subnetwork": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Existing regional subnetwork name for gateway NICs.",
			},
			"client_tag": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Network tag applied to private client VMs that should use the BetterNAT route.",
			},
			"route_name": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"route_priority": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(800),
			},
			"route_destination_cidr": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("0.0.0.0/0"),
			},
			"machine_type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("e2-small"),
			},
			"image_project": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("debian-cloud"),
			},
			"image_family": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("debian-12"),
			},
			"gateway_count": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(2),
			},
			"private_cidrs": schema.ListAttribute{
				ElementType:         types.StringType,
				Required:            true,
				MarkdownDescription: "Private CIDR ranges to masquerade on gateway instances.",
			},
			"startup_script":    schema.StringAttribute{Computed: true, Sensitive: true},
			"gateway_statuses":  schema.MapAttribute{ElementType: types.StringType, Computed: true},
			"egress_public_ips": schema.MapAttribute{ElementType: types.StringType, Computed: true},
			"route_target":      schema.StringAttribute{Computed: true},
			"status":            schema.StringAttribute{Computed: true},
		},
	}
}

func (r *GCPGatewayResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan GCPGatewayResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, plan)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway", err.Error())
		return
	}
	result, err := applier.Apply(ctx, inputs)
	if err != nil {
		resp.Diagnostics.AddError("Create GCP gateway", err.Error())
		return
	}
	applyGCPResult(&plan, inputs, gcpinstall.ReadResult{
		GatewayInstances: result.GatewayInstances,
		EgressPublicIPs:  result.EgressPublicIPs,
		RouteTarget:      result.RouteTarget,
		Status:           "active",
	})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *GCPGatewayResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GCPGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway", err.Error())
		return
	}
	result, err := applier.Read(ctx, inputs)
	if err != nil {
		resp.Diagnostics.AddError("Read GCP gateway", err.Error())
		return
	}
	if result.Status == "missing" {
		resp.State.RemoveResource(ctx)
		return
	}
	applyGCPResult(&state, inputs, result)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *GCPGatewayResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"GCP gateway updates are not implemented",
		"Replace the betternat_gcp_gateway resource to change GCP alpha gateway topology. This avoids mutating route and gateway ownership until GCP lease coordination is implemented.",
	)
}

func (r *GCPGatewayResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state GCPGatewayResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	applier, inputs, err := gcpApplierAndInputs(ctx, state)
	if err != nil {
		resp.Diagnostics.AddError("Configure GCP gateway cleanup", err.Error())
		return
	}
	if err := applier.Cleanup(ctx, inputs); err != nil {
		resp.Diagnostics.AddError("Delete GCP gateway", err.Error())
	}
}

func gcpApplierAndInputs(ctx context.Context, model GCPGatewayResourceModel) (gcpinstall.Applier, gcpinstall.Inputs, error) {
	privateCIDRs, err := listStrings(ctx, model.PrivateCIDRs)
	if err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, err
	}
	service, err := gcompute.NewService(ctx)
	if err != nil {
		return gcpinstall.Applier{}, gcpinstall.Inputs{}, fmt.Errorf("create GCP compute service: %w", err)
	}
	inputs := gcpInputs(model, privateCIDRs)
	return gcpinstall.Applier{Compute: service}, inputs, nil
}

func gcpInputs(model GCPGatewayResourceModel, privateCIDRs []string) gcpinstall.Inputs {
	name := model.Name.ValueString()
	routeName := stringDefault(model.RouteName, name+"-default-via-gateway")
	inputs := gcpinstall.Inputs{
		Name:           name,
		ProjectID:      model.ProjectID.ValueString(),
		Region:         model.Region.ValueString(),
		Zone:           model.Zone.ValueString(),
		Network:        model.Network.ValueString(),
		Subnetwork:     model.Subnetwork.ValueString(),
		ClientTag:      model.ClientTag.ValueString(),
		RouteName:      routeName,
		RoutePriority:  int64Default(model.RoutePriority, 800),
		RouteDestRange: stringDefault(model.RouteDestRange, "0.0.0.0/0"),
		MachineType:    stringDefault(model.MachineType, "e2-small"),
		ImageProject:   stringDefault(model.ImageProject, "debian-cloud"),
		ImageFamily:    stringDefault(model.ImageFamily, "debian-12"),
		GatewayCount:   int64Default(model.GatewayCount, 2),
		PrivateCIDRs:   privateCIDRs,
		Labels: map[string]string{
			"betternat_name": sanitizeGCPLabel(name),
			"betternat":      "true",
		},
		OperationPollTime: 2 * time.Second,
	}
	if inputs.RoutePriority == 0 {
		inputs.RoutePriority = 800
	}
	if inputs.GatewayCount == 0 {
		inputs.GatewayCount = 2
	}
	inputs.StartupScript = gcpinstall.GatewayStartupScript(gcpinstall.StartupScriptInputs{PrivateCIDRs: privateCIDRs})
	return inputs
}

func applyGCPResult(model *GCPGatewayResourceModel, inputs gcpinstall.Inputs, result gcpinstall.ReadResult) {
	model.ID = types.StringValue(fmt.Sprintf("%s/%s/%s", inputs.ProjectID, inputs.Zone, inputs.Name))
	model.RouteName = types.StringValue(inputs.RouteName)
	model.RoutePriority = types.Int64Value(inputs.RoutePriority)
	model.RouteDestRange = types.StringValue(inputs.RouteDestRange)
	model.MachineType = types.StringValue(inputs.MachineType)
	model.ImageProject = types.StringValue(inputs.ImageProject)
	model.ImageFamily = types.StringValue(inputs.ImageFamily)
	model.GatewayCount = types.Int64Value(inputs.GatewayCount)
	model.StartupScript = types.StringValue(inputs.StartupScript)
	model.GatewayStatuses = mustStringMap(result.GatewayInstances)
	model.EgressPublicIPs = mustStringMap(result.EgressPublicIPs)
	model.RouteTarget = types.StringValue(result.RouteTarget)
	model.Status = types.StringValue(result.Status)
}

func sanitizeGCPLabel(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out = append(out, r)
		} else if r >= 'A' && r <= 'Z' {
			out = append(out, r+'a'-'A')
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "betternat"
	}
	if len(out) > 63 {
		out = out[:63]
	}
	return string(out)
}
