// Package tfprovider exposes the BetterNAT Terraform/OpenTofu provider factory
// for the standalone terraform-provider-betternat repository.
package tfprovider

import (
	"github.com/hashicorp/terraform-plugin-framework/provider"

	internaltfprovider "github.com/nowakeai/betternat/internal/tfprovider"
)

// New returns a provider factory with the supplied provider version.
func New(version string) func() provider.Provider {
	return internaltfprovider.New(version)
}
