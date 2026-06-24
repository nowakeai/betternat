# Existing VPC Install

Date: 2026-06-21

## Purpose

This guide explains how to evaluate BetterNAT in an existing AWS VPC.

Use the disposable [Quick Start](QUICK_START.md) first. Existing-VPC migration changes private route tables and can interrupt egress.

## Migration Model

BetterNAT replaces the private subnet default route:

```text
0.0.0.0/0 -> NAT Gateway
```

with:

```text
0.0.0.0/0 -> active BetterNAT gateway node
```

The BetterNAT agent owns runtime route failover after deployment.

## Requirements

You need:

- one public subnet in the target AZ,
- one or more private route tables in the same AZ,
- private CIDR ranges allowed to use the gateway,
- IAM permission to create the BetterNAT gateway node stack,
- SSM access to gateway instances,
- a rollback plan.

For the first alpha:

- use one AZ only,
- use an explicit Linux AMI and cloud-init bootstrap,
- do not use BetterNAT for production workloads until you have tested your rollback path.

## Terraform Shape

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress-a"
  region = var.region
  vpc_id = var.vpc_id

  ami_id           = data.aws_ami.al2023_arm64.id
  instance_type    = "t4g.small"
  use_spot         = false
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  betternat_version = "v0.1.0-alpha.6"

  public_subnet_ids = {
    "us-west-2a" = var.public_subnet_id
  }

  private_route_table_ids = {
    "us-west-2a" = var.private_route_table_ids
  }

  private_cidrs = [var.vpc_cidr]

  stable_egress_ip    = true
  ha_profile          = "default"
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

## Cutover Checklist

Before apply:

- record current private route targets,
- confirm no managed Terraform `aws_route` resource will fight BetterNAT for the same default route,
- confirm security groups allow private CIDRs to reach gateway nodes,
- confirm SSM access works in the VPC,
- choose a maintenance window for first migration.

After apply:

- verify ASG has two healthy instances,
- run `betternat doctor --live` on the active gateway node,
- verify route target matches the active lease owner,
- verify EIP association when `stable_egress_ip=true`,
- test private subnet egress,
- scrape metrics.

Rollback:

- if `rollback_on_destroy=true`, Terraform destroy attempts to restore captured route targets,
- if rollback fails or route ownership was changed outside BetterNAT, manually replace the private default route with the previous NAT Gateway or instance target.

## Important Terraform Drift Warning

Do not let a separate Terraform `aws_route` resource manage the same private default route after BetterNAT is active.

BetterNAT's runtime HA loop must be able to replace that route during failover. If another Terraform resource tries to restore the old target, scale and destroy operations can become unsafe.
