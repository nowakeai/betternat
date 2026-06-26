package tfprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = (*RuntimeArtifactsDataSource)(nil)

type RuntimeArtifactsDataSource struct{}

type RuntimeArtifactsDataSourceModel struct {
	Version             types.String `tfsdk:"version"`
	OS                  types.String `tfsdk:"os"`
	Arch                types.String `tfsdk:"arch"`
	AgentBinaryURL      types.String `tfsdk:"agent_binary_url"`
	AgentBinarySHA256   types.String `tfsdk:"agent_binary_sha256"`
	CLIBinaryURL        types.String `tfsdk:"cli_binary_url"`
	CLIBinarySHA256     types.String `tfsdk:"cli_binary_sha256"`
	LoxiCMDBinaryURL    types.String `tfsdk:"loxicmd_binary_url"`
	LoxiCMDBinarySHA256 types.String `tfsdk:"loxicmd_binary_sha256"`
}

func NewRuntimeArtifactsDataSource() datasource.DataSource {
	return &RuntimeArtifactsDataSource{}
}

func (d *RuntimeArtifactsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runtime_artifacts"
}

func (d *RuntimeArtifactsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Returns provider-supported BetterNAT runtime artifact URLs and SHA256 checksums.",
		Attributes: map[string]schema.Attribute{
			"version": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "BetterNAT runtime release tag, for example v0.2.0.",
			},
			"os": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Runtime operating system. Current supported value: linux.",
			},
			"arch": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Runtime architecture. Current supported values: amd64 or arm64.",
			},
			"agent_binary_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "betternat-agent release artifact URL.",
			},
			"agent_binary_sha256": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "SHA256 checksum for agent_binary_url.",
			},
			"cli_binary_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "betternat CLI release artifact URL.",
			},
			"cli_binary_sha256": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "SHA256 checksum for cli_binary_url.",
			},
			"loxicmd_binary_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Reserved for a future provider-managed loxicmd artifact URL. Empty in current releases.",
			},
			"loxicmd_binary_sha256": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Reserved for a future provider-managed loxicmd checksum. Empty in current releases.",
			},
		},
	}
}

func (d *RuntimeArtifactsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config RuntimeArtifactsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	artifacts, err := runtimeArtifacts(config.Version.ValueString(), config.OS.ValueString(), config.Arch.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Resolve BetterNAT runtime artifacts", err.Error())
		return
	}
	config.AgentBinaryURL = types.StringValue(artifacts.AgentBinaryURL)
	config.AgentBinarySHA256 = types.StringValue(artifacts.AgentBinarySHA256)
	config.CLIBinaryURL = types.StringValue(artifacts.CLIBinaryURL)
	config.CLIBinarySHA256 = types.StringValue(artifacts.CLIBinarySHA256)
	config.LoxiCMDBinaryURL = types.StringValue("")
	config.LoxiCMDBinarySHA256 = types.StringValue("")
	resp.Diagnostics.Append(resp.State.Set(ctx, config)...)
}
