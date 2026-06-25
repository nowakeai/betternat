package tfprovider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/nowakeai/betternat/internal/installplan"
)

var _ datasource.DataSource = (*AWSGatewayStatusDataSource)(nil)
var _ datasource.DataSourceWithConfigure = (*AWSGatewayStatusDataSource)(nil)

type AWSGatewayStatusDataSource struct {
	readerFactory ReaderFactory
}

type AWSGatewayStatusDataSourceModel struct {
	Name                   types.String `tfsdk:"name"`
	Region                 types.String `tfsdk:"region"`
	InstallPlanJSON        types.String `tfsdk:"install_plan_json"`
	EgressPublicIPs        types.Map    `tfsdk:"egress_public_ips"`
	RouteTargets           types.Map    `tfsdk:"route_targets"`
	ActiveInstanceIDs      types.Map    `tfsdk:"active_instance_ids"`
	CoordinationTableName  types.String `tfsdk:"coordination_table_name"`
	ControlPlaneStatusJSON types.String `tfsdk:"control_plane_status_json"`
	Status                 types.String `tfsdk:"status"`
}

func NewAWSGatewayStatusDataSourceWithReader(readerFactory ReaderFactory) datasource.DataSource {
	return &AWSGatewayStatusDataSource{readerFactory: readerFactory}
}

func (d *AWSGatewayStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_gateway_status"
}

func (d *AWSGatewayStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected BetterNAT provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.readerFactory = data.ReaderFactory
}

func (d *AWSGatewayStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads BetterNAT AWS gateway control-plane status from an existing install plan without modifying cloud resources.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "BetterNAT gateway name.",
			},
			"region": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "AWS region for the gateway.",
			},
			"install_plan_json": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "Install plan JSON from betternat_aws_gateway.install_plan_json. Current alpha status reads require this explicit plan context.",
			},
			"egress_public_ips": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Current public egress IPs by availability zone when stable public identity is enabled.",
			},
			"route_targets": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Current managed default-route targets by route table ID.",
			},
			"active_instance_ids": schema.MapAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Current public-identity owner instance IDs by availability zone when available.",
			},
			"coordination_table_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Provider-owned DynamoDB coordination table name from the install plan.",
			},
			"control_plane_status_json": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Raw JSON status returned by the AWS reader.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Best-effort status summary: active, degraded, or created.",
			},
		},
	}
}

func (d *AWSGatewayStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config AWSGatewayStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state, err := readAWSGatewayStatus(ctx, config, d.readerFactory)
	if err != nil {
		resp.Diagnostics.AddError("Read BetterNAT AWS gateway status", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func readAWSGatewayStatus(ctx context.Context, config AWSGatewayStatusDataSourceModel, factory ReaderFactory) (AWSGatewayStatusDataSourceModel, error) {
	if factory == nil {
		return AWSGatewayStatusDataSourceModel{}, fmt.Errorf("reader factory is not configured")
	}
	var plan installplan.Plan
	if err := json.Unmarshal([]byte(config.InstallPlanJSON.ValueString()), &plan); err != nil {
		return AWSGatewayStatusDataSourceModel{}, fmt.Errorf("decode install plan: %w", err)
	}
	if plan.Name != config.Name.ValueString() {
		return AWSGatewayStatusDataSourceModel{}, fmt.Errorf("install plan name %q does not match requested gateway %q", plan.Name, config.Name.ValueString())
	}
	if plan.Region != config.Region.ValueString() {
		return AWSGatewayStatusDataSourceModel{}, fmt.Errorf("install plan region %q does not match requested region %q", plan.Region, config.Region.ValueString())
	}
	reader, err := factory(ctx, config.Region.ValueString())
	if err != nil {
		return AWSGatewayStatusDataSourceModel{}, err
	}
	result, err := reader.Read(ctx, plan)
	if err != nil {
		return AWSGatewayStatusDataSourceModel{}, err
	}
	statusBytes, err := json.Marshal(result)
	if err != nil {
		return AWSGatewayStatusDataSourceModel{}, fmt.Errorf("marshal control plane status: %w", err)
	}
	config.EgressPublicIPs = mustStringMap(result.EgressPublicIPs)
	config.RouteTargets = mustStringMap(result.RouteTargets)
	config.ActiveInstanceIDs = mustStringMap(result.PublicIdentityInstanceIDs)
	config.CoordinationTableName = types.StringValue(plan.CoordinationTableName)
	config.ControlPlaneStatusJSON = types.StringValue(string(statusBytes))
	config.Status = types.StringValue(statusFromReadResult(plan, result))
	return config, nil
}
