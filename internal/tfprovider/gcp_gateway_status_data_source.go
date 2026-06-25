package tfprovider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	gcompute "google.golang.org/api/compute/v1"

	gcpinstall "github.com/nowakeai/betternat/internal/install/gcp"
)

var _ datasource.DataSource = (*GCPGatewayStatusDataSource)(nil)

type GCPGatewayStatusDataSource struct{}

type GCPGatewayStatusDataSourceModel struct {
	Name            types.String `tfsdk:"name"`
	ProjectID       types.String `tfsdk:"project_id"`
	Region          types.String `tfsdk:"region"`
	Zone            types.String `tfsdk:"zone"`
	Network         types.String `tfsdk:"network"`
	Subnetwork      types.String `tfsdk:"subnetwork"`
	ClientTag       types.String `tfsdk:"client_tag"`
	RouteName       types.String `tfsdk:"route_name"`
	GatewayCount    types.Int64  `tfsdk:"gateway_count"`
	GatewayStatuses types.Map    `tfsdk:"gateway_statuses"`
	EgressPublicIPs types.Map    `tfsdk:"egress_public_ips"`
	RouteTarget     types.String `tfsdk:"route_target"`
	Status          types.String `tfsdk:"status"`
}

func NewGCPGatewayStatusDataSource() datasource.DataSource {
	return &GCPGatewayStatusDataSource{}
}

func (d *GCPGatewayStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_gateway_status"
}

func (d *GCPGatewayStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read-only status for a BetterNAT GCP alpha gateway.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required: true,
			},
			"project_id": schema.StringAttribute{
				Required: true,
			},
			"region": schema.StringAttribute{
				Required: true,
			},
			"zone": schema.StringAttribute{
				Required: true,
			},
			"network": schema.StringAttribute{
				Required: true,
			},
			"subnetwork": schema.StringAttribute{
				Required: true,
			},
			"client_tag": schema.StringAttribute{
				Required: true,
			},
			"route_name": schema.StringAttribute{
				Optional: true,
				Computed: true,
			},
			"gateway_count": schema.Int64Attribute{
				Optional: true,
				Computed: true,
			},
			"gateway_statuses": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"egress_public_ips": schema.MapAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
			"route_target": schema.StringAttribute{
				Computed: true,
			},
			"status": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *GCPGatewayStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config GCPGatewayStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	service, err := gcompute.NewService(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Create GCP compute service", err.Error())
		return
	}
	inputs := gcpStatusInputs(config)
	result, err := (gcpinstall.Applier{Compute: service}).Read(ctx, inputs)
	if err != nil {
		resp.Diagnostics.AddError("Read GCP gateway status", err.Error())
		return
	}
	config.RouteName = types.StringValue(inputs.RouteName)
	config.GatewayCount = types.Int64Value(inputs.GatewayCount)
	config.GatewayStatuses = mustStringMap(result.GatewayInstances)
	config.EgressPublicIPs = mustStringMap(result.EgressPublicIPs)
	config.RouteTarget = types.StringValue(result.RouteTarget)
	config.Status = types.StringValue(result.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func gcpStatusInputs(model GCPGatewayStatusDataSourceModel) gcpinstall.Inputs {
	name := model.Name.ValueString()
	return gcpinstall.Inputs{
		Name:          name,
		ProjectID:     model.ProjectID.ValueString(),
		Region:        model.Region.ValueString(),
		Zone:          model.Zone.ValueString(),
		Network:       model.Network.ValueString(),
		Subnetwork:    model.Subnetwork.ValueString(),
		ClientTag:     model.ClientTag.ValueString(),
		RouteName:     stringDefault(model.RouteName, fmt.Sprintf("%s-default-via-gateway", name)),
		GatewayCount:  int64Default(model.GatewayCount, 2),
		RoutePriority: 800,
	}
}
