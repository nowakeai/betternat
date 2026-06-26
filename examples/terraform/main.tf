terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.2.0"
    }
  }
}

provider "betternat" {}

resource "betternat_aws_gateway" "egress" {
  name   = "prod-egress"
  region = "us-west-2"

  vpc_id = "vpc-123"

  # Use an explicit Linux AMI for the current cloud-init install path.
  ami_id        = "ami-xxxxxxxx"
  instance_type = "t3.small"
  # Keep false for production-like examples. Low-cost AWS supplement tests can
  # override this to true when interruption risk is acceptable.
  use_spot = false

  # BetterNAT currently runs one gateway HA group per AZ. desired_capacity=2
  # gives the standard active + standby shape; use 1 for cheapest non-HA mode
  # or 3+ for extra standby capacity.
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  public_subnet_ids = {
    us-west-2a = "subnet-public-a"
  }

  private_route_table_ids = {
    us-west-2a = ["rtb-private-a"]
  }

  private_cidrs = ["10.0.0.0/8"]

  datapath_engine     = "loxilb"
  betternat_version   = "v0.2.0"
  stable_egress_ip    = true
  ha_profile          = "default"
  prometheus_enabled  = true
  rollback_on_destroy = true
}

output "agent_config_hash" {
  value = betternat_aws_gateway.egress.agent_config_hash
}

output "managed_route_table_ids" {
  value = betternat_aws_gateway.egress.managed_route_table_ids
}
