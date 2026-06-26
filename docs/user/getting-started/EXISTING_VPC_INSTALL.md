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

If your Terraform currently provisions a single-AZ AWS NAT Gateway, it probably
has this shape:

```hcl
resource "aws_eip" "nat" {
  domain = "vpc"
}

resource "aws_nat_gateway" "main" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public["us-west-2a"].id
}

resource "aws_route" "private_default" {
  route_table_id         = aws_route_table.private["us-west-2a"].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main.id
}
```

With BetterNAT, the user-facing replacement is one resource. BetterNAT creates
the node pool, EIP, coordination table, IAM role, security group, launch
template, ASG, and owns the private route target.

The important migration rule:

- before: Terraform owns the private default route with `nat_gateway_id`,
- after: `betternat_aws_gateway` owns that route table's default route so
  `betternat-agent` can move it during failover.

Do not keep a separate `aws_route` resource managing the same `0.0.0.0/0`
private route after BetterNAT is active.

For a production or shared staging VPC, do not delete the previous NAT Gateway
in the same apply that first installs BetterNAT. Keep the previous target alive
until:

- BetterNAT status and live checks pass,
- private workloads can reach the internet through BetterNAT,
- `rollback_route_targets_json` contains the previous route target,
- you have tested or rehearsed the manual `replace-route` rollback command.

If Terraform destroys the old NAT Gateway during the first BetterNAT apply, the
captured rollback target may point to a resource that no longer exists.

## Requirements

You need:

- one public subnet in the target AZ,
- one or more private route tables in the same AZ,
- private CIDR ranges allowed to use the gateway,
- IAM permission to create the BetterNAT gateway node stack,
- SSM access to gateway instances,
- a rollback plan.

Current scope:

- use one AZ only,
- use an explicit Linux AMI and cloud-init bootstrap,
- do not use BetterNAT for production workloads until you have tested your rollback path.

## Terraform Shape

```hcl
resource "betternat_aws_gateway" "egress" {
  name   = "prod-egress-a"
  region = var.region
  vpc_id = var.vpc_id

  ami_id           = data.aws_ami.al2023_arm64.id
  instance_type    = "t4g.small"
  use_spot         = false
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  betternat_version = "v0.2.0"

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

## Preflight

Set the values you will inspect:

```sh
export AWS_REGION=us-west-2
export PRIVATE_RTB_ID=rtb-xxxxxxxx
export FALLBACK_NAT_GW_ID=nat-xxxxxxxx
```

Record the current private default route:

```sh
aws ec2 describe-route-tables \
  --region "$AWS_REGION" \
  --route-table-ids "$PRIVATE_RTB_ID" \
  --query 'RouteTables[].Routes[?DestinationCidrBlock==`0.0.0.0/0`]'
```

Keep the output with your change record. You want to know the exact target that
would restore private-subnet egress if BetterNAT does not pass validation.

Confirm the old NAT Gateway still exists:

```sh
aws ec2 describe-nat-gateways \
  --region "$AWS_REGION" \
  --nat-gateway-ids "$FALLBACK_NAT_GW_ID" \
  --query 'NatGateways[].{Id:NatGatewayId,State:State,Subnet:SubnetId}'
```

Confirm Terraform will not keep a competing route resource:

```sh
terraform state list | grep 'aws_route'
```

It is fine for Terraform to have other route resources. The thing to avoid is a
separate `aws_route` that still manages the same private route table
`0.0.0.0/0` entry after `betternat_aws_gateway` is active.

## Plan Review

Run a plan before the cutover:

```sh
terraform plan
```

Review the plan for these points:

- `betternat_aws_gateway` will be created,
- the private default route will become BetterNAT-owned,
- no separate `aws_route` will continue managing the same default route,
- the previous NAT Gateway is not destroyed in the first production cutover,
- no unrelated EKS, node group, subnet, or route table changes are included.

If you intentionally use a single selector such as `nat_backend = "betternat"`,
make sure the conditional logic keeps the old NAT Gateway for the first
production migration. Delete it in a later apply after the rollback window.

## Cutover Checklist

Before apply:

- record current private route targets,
- confirm no managed Terraform `aws_route` resource will fight BetterNAT for the same default route,
- confirm security groups allow private CIDRs to reach gateway nodes,
- confirm SSM access works in the VPC,
- choose a maintenance window for first migration.

Apply:

```sh
terraform apply
```

After apply, check the provider outputs first:

```sh
terraform output active_instance_ids
terraform output egress_public_ips
terraform output rollback_route_targets_json
```

`rollback_route_targets_json` should contain the previous route target for every
private route table BetterNAT manages. If it is empty or missing a route table,
stop and review the rollback path before moving production traffic further.

Then verify AWS route state:

```sh
aws ec2 describe-route-tables \
  --region "$AWS_REGION" \
  --route-table-ids "$PRIVATE_RTB_ID" \
  --query 'RouteTables[].Routes[?DestinationCidrBlock==`0.0.0.0/0`]'
```

Expected result: the default route points to the active BetterNAT gateway node
or its ENI, depending on the configured route target mode.

On a gateway node, verify local and fleet state:

```sh
sudo betternat status
sudo betternat doctor --live
```

From a private workload or a temporary private EC2 instance, verify egress:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

Post-apply checklist:

- verify ASG has two healthy instances,
- run `betternat doctor --live` on the active gateway node,
- verify route target matches the active lease owner,
- verify EIP association when `stable_egress_ip=true`,
- test private subnet egress,
- scrape metrics.

Rollback:

- keep the previous NAT Gateway or NAT instance alive during the rollback window,
- keep one tested `aws ec2 replace-route` command for every private route table,
- if `rollback_on_destroy=true`, Terraform destroy attempts to restore captured route targets,
- if rollback fails or route ownership was changed outside BetterNAT, manually replace the private default route with the previous target.

Use the [Rollback Guide](../operations/ROLLBACK_GUIDE.md) for the exact
destroy flow, manual `replace-route` commands, stale-target behavior, and
post-rollback verification.

## Important Terraform Drift Warning

Do not let a separate Terraform `aws_route` resource manage the same private default route after BetterNAT is active.

BetterNAT's runtime HA loop must be able to replace that route during failover. If another Terraform resource tries to restore the old target, scale and destroy operations can become unsafe.
