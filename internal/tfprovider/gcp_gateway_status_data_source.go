package tfprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = (*GCPGatewayStatusDataSource)(nil)

type GCPGatewayStatusDataSource struct{}

type GCPGatewayStatusDataSourceModel struct {
	Name      types.String `tfsdk:"name"`
	ProjectID types.String `tfsdk:"project_id"`
	Region    types.String `tfsdk:"region"`
}

func NewGCPGatewayStatusDataSource() datasource.DataSource {
	return &GCPGatewayStatusDataSource{}
}

func (d *GCPGatewayStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_gateway_status"
}

func (d *GCPGatewayStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reserved GCP gateway status data source. GCP support is deferred until the GCP alpha spike validates route replacement, forwarding, and coordination behavior.",
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
		},
	}
}

func (d *GCPGatewayStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config GCPGatewayStatusDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.AddError(
		"BetterNAT GCP gateway status is not implemented",
		"GCP support is reserved for a later alpha after disposable GCP validation proves route replacement, gateway forwarding, and coordination semantics.",
	)
}
