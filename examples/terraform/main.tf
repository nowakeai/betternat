terraform {
  required_providers {
    betternat = {
      source = "betternat/betternat"
    }
  }
}

provider "betternat" {}

resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"

  vpc_id = "vpc-123"

  ami_id        = "ami-0123456789abcdef0"
  instance_type = "t3.small"

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
  stable_egress_ip         = true
  prometheus_enabled       = true
}

output "agent_config_hash" {
  value = betternat_gateway.egress.agent_config_hash
}

output "managed_route_table_ids" {
  value = betternat_gateway.egress.managed_route_table_ids
}
