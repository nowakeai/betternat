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
  betternat_version   = "v0.1.0"
  stable_egress_ip    = true
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

## Route Ownership

BetterNAT owns the selected private default routes while the resource exists.
Do not also manage those same `0.0.0.0/0` routes with separate `aws_route`
resources.

## Runtime Behavior

The active gateway owns the DynamoDB lease, private route target, and shared EIP
when `stable_egress_ip=true`. Active connections may reset during failover; new
connections recover after route and public identity ownership converge.

## Destroy

Keep `rollback_on_destroy=true` unless you have already restored route state.
Read the user rollback guide before deleting gateway nodes, EIPs, route tables,
or coordination tables by hand.
