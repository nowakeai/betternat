package main

import (
	"context"
	"log"

	"github.com/nowakeai/betternat/internal/buildinfo"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/nowakeai/betternat/internal/tfprovider"
)

func main() {
	if err := providerserver.Serve(context.Background(), tfprovider.New(buildinfo.Version), providerserver.ServeOpts{
		Address: "registry.terraform.io/nowakeai/betternat",
	}); err != nil {
		log.Fatal(err)
	}
}
