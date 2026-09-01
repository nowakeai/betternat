# Resource: betternat_aws_gateway

Manages an AWS BetterNAT gateway group.

Most users should prefer the AWS module:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/aws"
  version = "~> 0.2"

  name   = "prod-egress"
  vpc_id = module.vpc.vpc_id

  azs                     = module.vpc.azs
  public_subnet_ids       = module.vpc.public_subnets
  private_route_table_ids = module.vpc.private_route_table_ids

  private_cidrs = [module.vpc.vpc_cidr_block]
}
```

Use this resource directly when you need the lower-level provider primitive.

## Example

```hcl
resource "betternat_aws_gateway" "egress" {
  name   = "prod-egress-a"
  region = "us-west-2"
  vpc_id = aws_vpc.main.id

  public_subnet_ids = {
    "us-west-2a" = aws_subnet.public_a.id
  }

  private_route_table_ids = {
    "us-west-2a" = [aws_route_table.private_a.id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  ami_id              = data.aws_ami.al2023_arm64.id
  instance_type       = "t4g.small"
  desired_capacity    = 2
  max_size            = 3
  betternat_version   = "v0.2.0"
  stable_egress_ip    = true
  primary_interface   = "auto"
  snat_interface      = "auto"
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

For production public identity, manage the EIP independently and pass its
allocation ID to BetterNAT:

```hcl
resource "aws_eip" "betternat" {
  domain = "vpc"

  tags = {
    Name = "prod-egress-us-west-2a"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "betternat_aws_gateway" "egress" {
  # ...
  stable_egress_ip = true
  eip_allocation_ids = {
    us-west-2a = aws_eip.betternat.id
  }
}
```

Externally managed EIPs are associated and observed by BetterNAT but are never
released by this resource. They therefore survive gateway replacement and final
gateway destroy. Remove `prevent_destroy` and destroy the `aws_eip` explicitly
only when the public identity is no longer needed.

For compatibility with provider-managed EIPs, set
`retain_managed_eips_on_destroy = true`. Destroy then leaves the tagged EIPs in
the account, and a same-name gateway recreation adopts them instead of
allocating new addresses. Before a final destroy that should release those
EIPs, set the option back to `false`, apply that state-only change, and destroy
the gateway. Independent `aws_eip` resources remain the recommended production
ownership model because their lifecycle is explicit in Terraform.

## Route Ownership

BetterNAT owns the selected private default routes while the resource exists.
Do not also manage those same `0.0.0.0/0` routes with separate `aws_route`
resources.

## Runtime Behavior

The active gateway owns the DynamoDB lease, private route target, and shared EIP
when `stable_egress_ip=true`. Active connections may reset during failover; new
connections recover after route and public identity ownership converge.

Without `eip_allocation_ids` or `retain_managed_eips_on_destroy`, stability is
limited to instance failover inside the gateway. Full Terraform resource
replacement releases provider-managed EIPs by default.

`primary_interface` and `snat_interface` default to `auto`. During bootstrap,
BetterNAT detects the Linux interface that owns the IPv4 default route and
writes that concrete name into the runtime agent config. Both fields accept an
explicit interface name for unusual multi-interface appliances.

## Destroy

Keep `rollback_on_destroy=true` unless you have already restored route state.
Read the user rollback guide before deleting gateway nodes, EIPs, route tables,
or coordination tables by hand.
