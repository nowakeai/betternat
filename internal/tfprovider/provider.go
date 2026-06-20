package tfprovider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = (*Provider)(nil)

type Provider struct {
	version          string
	installerFactory InstallerFactory
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{version: version, installerFactory: defaultInstallerFactory}
	}
}

func NewWithInstaller(version string, factory InstallerFactory) func() provider.Provider {
	return func() provider.Provider {
		return &Provider{version: version, installerFactory: factory}
	}
}

func (p *Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "betternat"
	resp.Version = p.version
}

func (p *Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "BetterNAT provider.",
	}
}

func (p *Provider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = providerData{InstallerFactory: p.installerFactory}
}

func (p *Provider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		func() resource.Resource {
			return NewGatewayResourceWithInstaller(p.installerFactory)
		},
	}
}

func (p *Provider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
