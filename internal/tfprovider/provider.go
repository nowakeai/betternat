package tfprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*Provider)(nil)

type Provider struct {
	version           string
	installerFactory  InstallerFactory
	rollbackerFactory RollbackerFactory
	cleanerFactory    CleanerFactory
	readerFactory     ReaderFactory
}

type ProviderModel struct {
	AWSEndpointURL types.String `tfsdk:"aws_endpoint_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{version: version, installerFactory: defaultInstallerFactory, rollbackerFactory: defaultRollbackerFactory, cleanerFactory: defaultCleanerFactory, readerFactory: defaultReaderFactory}
	}
}

func NewWithInstaller(version string, factory InstallerFactory) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{version: version, installerFactory: factory, rollbackerFactory: defaultRollbackerFactory, cleanerFactory: defaultCleanerFactory, readerFactory: defaultReaderFactory}
	}
}

func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "betternat"
	resp.Version = p.version
}

func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT provider.",
		Attributes: map[string]schema.Attribute{
			"aws_endpoint_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional AWS-compatible endpoint URL for local testing, such as LocalStack. Leave unset for real AWS.",
			},
		},
	}
}

func (p *Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config ProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	installerFactory := p.installerFactory
	rollbackerFactory := p.rollbackerFactory
	cleanerFactory := p.cleanerFactory
	readerFactory := p.readerFactory
	if !config.AWSEndpointURL.IsNull() && !config.AWSEndpointURL.IsUnknown() && config.AWSEndpointURL.ValueString() != "" {
		endpointURL := config.AWSEndpointURL.ValueString()
		installerFactory = endpointInstallerFactory(endpointURL)
		rollbackerFactory = endpointRollbackerFactory(endpointURL)
		cleanerFactory = endpointCleanerFactory(endpointURL)
		readerFactory = endpointReaderFactory(endpointURL)
	}
	resp.ResourceData = providerData{InstallerFactory: installerFactory, RollbackerFactory: rollbackerFactory, CleanerFactory: cleanerFactory, ReaderFactory: readerFactory}
}

func (p *Provider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource {
			return NewGatewayResourceWithFactories(p.installerFactory, p.rollbackerFactory, p.cleanerFactory, p.readerFactory)
		},
	}
}

func (p *Provider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
