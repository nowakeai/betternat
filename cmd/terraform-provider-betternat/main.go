package main

import (
	"context"
	"log"

	"github.com/betternat/betternat/internal/buildinfo"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/betternat/betternat/internal/tfprovider"
)

func main() {
	if err := providerserver.Serve(context.Background(), tfprovider.New(buildinfo.Version), providerserver.ServeOpts{
		Address: "registry.terraform.io/betternat/betternat",
	}); err != nil {
		log.Fatal(err)
	}
}
