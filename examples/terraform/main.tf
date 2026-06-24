terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.6"
    }
  }
}

provider "betternat" {}

resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  region = "us-west-2"

  vpc_id = "vpc-123"

  # ami_channel is the happy path once BetterNAT publishes AMI channels.
  # ami_id remains available as an explicit override for private AMIs.
  ami_channel   = "stable"
  instance_type = "t3.small"
  # Keep false for production-like examples. Low-cost AWS supplement tests can
  # override this to true when interruption risk is acceptable.
  use_spot = false

  # BetterNAT runs as one appliance pool per AZ. desired_capacity=2 gives the
  # standard owner + warm candidate shape; use 1 for cheapest non-HA mode or
  # 3+ for extra standby capacity.
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  public_subnet_ids = {
    us-west-2a = "subnet-public-a"
    us-west-2b = "subnet-public-b"
  }

  private_route_table_ids = {
    us-west-2a = ["rtb-private-a"]
    us-west-2b = ["rtb-private-b"]
  }

  private_cidrs = ["10.0.0.0/8"]

  datapath_engine          = "loxilb"
  fallback_datapath_engine = "nftables"
  betternat_version        = "v0.1.0-alpha.2"
  stable_egress_ip         = true
  ha_profile               = "default"
  prometheus_enabled       = true
  rollback_on_destroy      = true
}

output "agent_config_hash" {
  value = betternat_gateway.egress.agent_config_hash
}

output "managed_route_table_ids" {
  value = betternat_gateway.egress.managed_route_table_ids
}
