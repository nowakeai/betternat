# BetterNAT Provider

BetterNAT is a Terraform-first egress gateway for private subnet workloads.

Most users should install BetterNAT through a cloud module:

```hcl
module "betternat" {
  source = "nowakeai/betternat/aws"
}

module "betternat" {
  source = "nowakeai/betternat/google"
}
```

The provider resources are lower-level lifecycle primitives for module authors,
advanced users, and validation workflows.

## Resources

- `betternat_aws_gateway` manages an AWS BetterNAT gateway group.
- `betternat_gcp_gateway` manages a GCP BetterNAT gateway group.

## Data Sources

- `betternat_runtime_artifacts` reads provider-supported runtime artifact URLs
  and checksums.
- `betternat_aws_gateway_status` reads AWS gateway control-plane state.
- `betternat_gcp_gateway_status` reads GCP gateway Compute state.

## Datapath

LoxiLB is the supported BetterNAT datapath. BetterNAT does not expose nftables
fallback behavior as a product feature on AWS, GCP, or future clouds.
