# Terraform Workflow: devops-tf NAT Gateway Compatibility

Date: 2026-06-20

## Question

Can BetterNAT's Terraform provider fit into an existing AWS networking module so that users configure it almost like an AWS NAT Gateway, or even more conveniently?

## Short Answer

Yes.

The referenced `devops-tf` AWS networking module is a good target for BetterNAT UX because NAT is already hidden behind one module-level switch:

```hcl
enable_nat_gateway = true
```

The module internally creates:

- one EIP,
- one `aws_nat_gateway`,
- one private route table,
- one default route from the private route table to the NAT Gateway.

BetterNAT can preserve this experience by changing the module contract from a boolean to a small NAT backend selector:

```hcl
nat_backend = "aws_nat_gateway"
```

or:

```hcl
nat_backend = "betternat"
```

For maximum backward compatibility, `enable_nat_gateway = true` can keep meaning "create outbound NAT", while a new variable chooses the implementation.

## Current devops-tf NAT Shape

Reviewed source files:

- `devops-tf/aws/modules/networking/main.tf`
- `devops-tf/aws/modules/networking/variables.tf`
- `devops-tf/aws/modules/networking/outputs.tf`
- `devops-tf/aws/main.tf`
- `devops-tf/aws/variables.tf`
- representative environment overrides under `devops-tf/aws/environments/`

The current networking module creates a single NAT Gateway in the first public AZ:

```hcl
resource "aws_eip" "nat" {
  count = var.enable_nat_gateway ? 1 : 0

  domain     = "vpc"
  depends_on = [aws_internet_gateway.main]
}

resource "aws_nat_gateway" "main" {
  count = var.enable_nat_gateway ? 1 : 0

  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public[var.availability_zones[0]].id
}

resource "aws_route" "private_nat_gateway" {
  count = var.enable_nat_gateway && var.enable_private_subnets ? 1 : 0

  route_table_id         = aws_route_table.private[0].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main[0].id
}
```

The root module passes:

```hcl
module "networking" {
  source = "./modules/networking"

  project_name           = var.project_name
  vpc_base_ip            = var.cluster_vpc_base_ip
  availability_zones     = var.cluster_availability_zones
  enable_nat_gateway     = var.enable_nat_gateway
  enable_private_subnets = var.enable_private_subnets
  aws_region             = var.aws_region
  tags                   = local.common_tags
}
```

Output:

```hcl
output "nat_gateway_ip" {
  value = var.enable_nat_gateway ? aws_eip.nat[0].public_ip : null
}
```

Important observation:

- callers do not manually configure EIP IDs,
- callers do not manually configure routes,
- callers do not manually know NAT implementation details,
- callers only opt into private subnet egress.

BetterNAT should preserve that shape.

## Current BetterNAT Provider Shape

The current BetterNAT provider has a `betternat_gateway` resource with inputs close to what the networking module already owns:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"
  vpc_id = aws_vpc.main.id

  public_subnet_ids = {
    "us-west-2a" = aws_subnet.public["a"].id
  }

  private_route_table_ids = {
    "us-west-2a" = [aws_route_table.private[0].id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]
}
```

This is already compatible with a networking module integration, but it is still too verbose for end users if exposed directly.

The best UX is:

- end users configure `nat_backend` or `egress_gateway`,
- the networking module passes subnets/routes/CIDRs to BetterNAT internally,
- BetterNAT provider owns appliance/EIP/DynamoDB/IAM/bootstrap,
- existing consumers keep using `module.networking.nat_gateway_ip`.

## Recommended UX

### Backward-Compatible Variables

Keep the old boolean:

```hcl
variable "enable_nat_gateway" {
  description = "Enable outbound NAT for private subnets"
  type        = bool
  default     = true
}
```

Add a backend selector:

```hcl
variable "nat_backend" {
  description = "Outbound NAT implementation: aws_nat_gateway, betternat, or none"
  type        = string
  default     = "aws_nat_gateway"

  validation {
    condition     = contains(["aws_nat_gateway", "betternat", "none"], var.nat_backend)
    error_message = "nat_backend must be aws_nat_gateway, betternat, or none."
  }
}
```

Derive effective NAT behavior:

```hcl
locals {
  nat_backend = var.enable_nat_gateway ? var.nat_backend : "none"
}
```

This lets old environments keep working:

```hcl
enable_nat_gateway = true
```

New BetterNAT environments use:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"
```

### BetterNAT Options

Add a compact object for product-level behavior:

```hcl
variable "betternat" {
  description = "BetterNAT options when nat_backend is betternat"
  type = object({
    instance_type    = optional(string, "t3.small")
    ami_channel      = optional(string, "stable")
    ami_id           = optional(string)
    stable_egress_ip = optional(bool, true)
    datapath_engine  = optional(string, "loxilb")
    fallback_engine  = optional(string, "nftables")
    prometheus       = optional(bool, true)
    rollback_on_destroy = optional(bool, true)
  })
  default = {}
}
```

This is simpler than exposing appliance count, IAM, DynamoDB, LoxiLB, route ownership, and bootstrap fields to normal users.

## Module-Level Implementation

Inside `modules/networking`, create AWS NAT Gateway only when requested:

```hcl
locals {
  use_aws_nat_gateway = local.nat_backend == "aws_nat_gateway" && var.enable_private_subnets
  use_betternat       = local.nat_backend == "betternat" && var.enable_private_subnets
}

resource "aws_eip" "nat" {
  count = local.use_aws_nat_gateway ? 1 : 0

  domain     = "vpc"
  depends_on = [aws_internet_gateway.main]
}

resource "aws_nat_gateway" "main" {
  count = local.use_aws_nat_gateway ? 1 : 0

  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public[var.availability_zones[0]].id
}

resource "aws_route" "private_nat_gateway" {
  count = local.use_aws_nat_gateway ? 1 : 0

  route_table_id         = aws_route_table.private[0].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main[0].id
}
```

Then add BetterNAT:

```hcl
resource "betternat_gateway" "main" {
  count = local.use_betternat ? 1 : 0

  name   = "${var.project_name}-egress"
  region = var.aws_region
  vpc_id = aws_vpc.main.id

  public_subnet_ids = {
    for az in var.availability_zones :
    "${var.aws_region}${az}" => aws_subnet.public[az].id
  }

  private_route_table_ids = {
    for az in var.availability_zones :
    "${var.aws_region}${az}" => [aws_route_table.private[0].id]
  }

  private_cidrs             = [aws_vpc.main.cidr_block]
  instance_type             = try(var.betternat.instance_type, "t3.small")
  ami_channel               = try(var.betternat.ami_channel, "stable")
  ami_id                    = try(var.betternat.ami_id, null)
  stable_egress_ip          = try(var.betternat.stable_egress_ip, true)
  datapath_engine           = try(var.betternat.datapath_engine, "loxilb")
  fallback_datapath_engine  = try(var.betternat.fallback_engine, "nftables")
  prometheus_enabled        = try(var.betternat.prometheus, true)
  rollback_on_destroy       = try(var.betternat.rollback_on_destroy, true)

  depends_on = [
    aws_internet_gateway.main,
    aws_route_table_association.private
  ]
}
```

Important provider gap:

- Terraform Plugin Framework optional object defaults may need a slightly different variable definition in real Terraform code.
- The conceptual shape is correct; exact HCL should be validated with `terraform validate`.

## Output Compatibility

Keep the existing output name for callers:

```hcl
output "nat_gateway_ip" {
  description = "Outbound NAT public IP"
  value = (
    local.use_aws_nat_gateway ? aws_eip.nat[0].public_ip :
    local.use_betternat ? one(values(betternat_gateway.main[0].egress_public_ips)) :
    null
  )
}
```

Optionally add clearer outputs:

```hcl
output "nat_backend" {
  value = local.nat_backend
}

output "egress_public_ips" {
  value = local.use_betternat ? betternat_gateway.main[0].egress_public_ips : (
    local.use_aws_nat_gateway ? { default = aws_eip.nat[0].public_ip } : {}
  )
}

output "betternat_active_instance_ids" {
  value = local.use_betternat ? betternat_gateway.main[0].active_instance_ids : {}
}
```

This lets existing consumers continue using `nat_gateway_ip`, while newer users get richer BetterNAT-specific outputs.

## User Experience Examples

### Existing AWS NAT Gateway

No meaningful change:

```hcl
module "networking" {
  source = "./modules/networking"

  project_name           = var.project_name
  vpc_base_ip            = var.cluster_vpc_base_ip
  availability_zones     = var.cluster_availability_zones
  enable_private_subnets = true

  enable_nat_gateway = true
  nat_backend        = "aws_nat_gateway"
}
```

### BetterNAT With Stable Egress IP

```hcl
module "networking" {
  source = "./modules/networking"

  project_name           = var.project_name
  vpc_base_ip            = var.cluster_vpc_base_ip
  availability_zones     = var.cluster_availability_zones
  enable_private_subnets = true

  enable_nat_gateway = true
  nat_backend        = "betternat"

  betternat = {
    stable_egress_ip = true
    instance_type    = "t3.small"
  }
}
```

### BetterNAT Route-Only Mode

Cheaper/faster failover mode, egress IP may change:

```hcl
module "networking" {
  source = "./modules/networking"

  project_name           = var.project_name
  vpc_base_ip            = var.cluster_vpc_base_ip
  availability_zones     = var.cluster_availability_zones
  enable_private_subnets = true

  enable_nat_gateway = true
  nat_backend        = "betternat"

  betternat = {
    stable_egress_ip = false
  }
}
```

### Public-Only Networking

Existing behavior remains:

```hcl
enable_nat_gateway     = false
enable_private_subnets = false
```

## Is This More Convenient Than AWS NAT Gateway?

It can be, if the provider absorbs the hard parts.

AWS NAT Gateway user work today:

- set `enable_nat_gateway = true`,
- pay AWS managed NAT cost,
- no HA details exposed,
- little observability.

BetterNAT should target:

- set `nat_backend = "betternat"`,
- optionally choose stable IP mode,
- provider creates appliances, EIP, DynamoDB, IAM, routes, bootstrap,
- output keeps `nat_gateway_ip`,
- BetterNAT adds metrics, source attribution, failover status, rollback info.

The product is only "more convenient" if the user does not have to wire:

- AMI lookup,
- appliance user-data,
- source/dest check,
- route replacement,
- EIP reassociation,
- DynamoDB lease table,
- IAM role and instance profile,
- LoxiLB firewall rule syntax.

Those must stay behind the provider/module boundary.

## Provider Changes Needed

Partially implemented in the current provider:

- `cloud` defaults to `aws`, so normal AWS users do not need to set it.
- `ami_channel` defaults to `stable`; `ami_id` remains an explicit private-AMI override.
- `route_mode`, `route_destination_cidr`, and `route_target_type` are explicit provider fields with conservative defaults.
- `rollback_on_destroy` defaults to true, and destroy refuses to silently drop state when rollback targets are still unknown unless `allow_destroy_without_rollback = true`.
- AWS apply snapshots existing route targets before `ReplaceRoute` and writes concrete `rollback_route_targets_json` into Terraform state.
- Terraform destroy now uses `rollback_route_targets_json` to restore previous AWS route targets when `rollback_on_destroy = true`.
- Terraform destroy also performs conservative managed-resource cleanup for known BetterNAT appliances, EIPs, DynamoDB lease table, IAM role/profile/policy, and security group.

The provider still needs these improvements for devops-tf-style UX:

1. **Delete lifecycle**
   - Route rollback is implemented for concrete previous targets.
   - Conservative managed-resource cleanup is implemented from Terraform state and install plan names.
   - Still need rollback verification after AWS `ReplaceRoute`.
   - Still need wait/retry polish for EC2 termination and security-group dependency convergence.

2. **Read lifecycle**
   - Current `Read` mostly returns state.
   - Need real AWS readback: appliances, EIPs, route targets, DynamoDB table, status.

3. **Route ownership policy**
   - Basic fields exist.
   - Still need richer ownership semantics such as route import, conflict policy, and drift handling.

4. **Existing route import / rollback capture**
   - AWS apply now captures previous targets before route replacement.
   - Still need import support for resources created outside Terraform and explicit rollback verification.

5. **AMI channel**
   - Schema and install plan support `ami_channel = "stable"`.
   - Still need real AMI channel resolution once BetterNAT publishes AMIs.

6. **Provider docs and examples**
   - Need examples matching this module style:
     - existing VPC,
     - EKS private subnet egress,
     - stable-IP mode,
     - route-only mode,
     - migration from AWS NAT Gateway.

7. **Acceptance tests**
   - Terraform apply/destroy in a disposable VPC.
   - Confirm no route/EIP/instance leaks.

## Recommended Migration Path For devops-tf

### Step 1: Add Backend Selector Without Behavior Change

Add:

```hcl
nat_backend = "aws_nat_gateway"
```

Keep default behavior identical.

### Step 2: Add BetterNAT Provider Behind Feature Flag

Add `betternat` provider requirement but do not enable it by default.

### Step 3: Enable BetterNAT In A Non-Production Environment

Use:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"
```

Run validation:

- private egress source IP,
- route table target,
- failover,
- rollback,
- cleanup.

### Step 4: Preserve Output Compatibility

Keep:

```hcl
module.networking.nat_gateway_ip
```

Even if the backend is BetterNAT.

### Step 5: Production Migration

For existing AWS NAT Gateway users:

1. create BetterNAT appliances without changing route,
2. run `doctor`,
3. switch route to BetterNAT during a maintenance window,
4. keep old NAT Gateway temporarily as rollback target,
5. delete NAT Gateway after observation.

## Conclusion

The devops-tf networking module is almost ideal for BetterNAT integration because it already hides NAT implementation details from callers.

The best product UX is not to ask users to write a new low-level `betternat_gateway` resource everywhere. Instead:

- expose a small `nat_backend` selector in existing network modules,
- keep `enable_nat_gateway` for backward compatibility,
- have the module feed subnets, route tables, and VPC CIDR into `betternat_gateway`,
- keep `nat_gateway_ip` output compatible,
- add richer BetterNAT outputs for users who want observability and HA status.

This gives users an experience close to AWS NAT Gateway:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"
```

while making BetterNAT more useful than AWS NAT Gateway through lower data processing cost, explicit failover mode, and better observability.
