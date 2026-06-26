# EKS Terraform Module Integration

Date: 2026-06-24

## Purpose

This guide shows how to integrate BetterNAT into an existing modular Terraform
repository that manages AWS networking and EKS clusters.

Use the disposable [Quick Start](QUICK_START.md) first. This guide is for the
next step, when you want to adapt an existing `networking` module instead of
copying the disposable fixture.

## Assumed Terraform Shape

This guide assumes a common layout:

```text
aws/
  main.tf
  variables.tf
  environments/<env>/terraform.tfvars
  modules/networking/
    main.tf
    variables.tf
    outputs.tf
  modules/eks-cluster/
  modules/eks-node-groups/
```

The root module calls `modules/networking`, EKS node groups use private subnet
IDs from that module, and private subnet default routes currently point to an
AWS NAT Gateway.

## Add A NAT Backend Selector

Keep the existing `enable_nat_gateway` input and add a backend selector:

```hcl
variable "enable_nat_gateway" {
  type    = bool
  default = true
}

variable "nat_backend" {
  type    = string
  default = "aws_nat_gateway"

  validation {
    condition     = contains(["aws_nat_gateway", "betternat", "none"], var.nat_backend)
    error_message = "nat_backend must be aws_nat_gateway, betternat, or none."
  }
}
```

Then an environment `terraform.tfvars` switches the backend:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"
```

## Plan Review Before Apply

Before applying the switch in a real environment, run a plan and check the
resource ownership change:

```sh
terraform plan
```

Expected:

- the old `aws_route` default route resource is removed or no longer planned,
- `aws_nat_gateway.main` and `aws_eip.nat` are destroyed when the backend is
  `betternat`,
- one `betternat_aws_gateway` resource is created,
- EKS cluster and node group subnet IDs do not change,
- private route table IDs do not change,
- unrelated IAM, EKS, node group, and security group changes are not bundled
  into the same apply.

Do not apply if Terraform still plans to manage the same private
`0.0.0.0/0` route through both `aws_route` and `betternat_aws_gateway`.

## Make The Existing NAT Gateway Conditional

Tie the existing EIP, NAT Gateway, and private default route to
`nat_backend == "aws_nat_gateway"`:

```hcl
resource "aws_eip" "nat" {
  count = var.enable_nat_gateway && var.nat_backend == "aws_nat_gateway" ? 1 : 0

  domain = "vpc"
}

resource "aws_nat_gateway" "main" {
  count = var.enable_nat_gateway && var.nat_backend == "aws_nat_gateway" ? 1 : 0

  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public[var.availability_zones[0]].id
}

resource "aws_route" "private_nat_gateway" {
  count = var.enable_nat_gateway && var.nat_backend == "aws_nat_gateway" ? 1 : 0

  route_table_id         = aws_route_table.private[0].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main[0].id
}
```

This prevents Terraform from creating an unused NAT Gateway after the backend
switches to BetterNAT.

## Add BetterNAT Beside It

Add the BetterNAT provider at the root or module boundary where providers are
declared:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0"
    }
  }
}
```

Add a Linux AMI data source for the cloud-init path:

```hcl
data "aws_ami" "al2023_arm64" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-arm64"]
  }

  filter {
    name   = "architecture"
    values = ["arm64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}
```

Add BetterNAT in the networking module, using the same public subnet and
private route table:

```hcl
resource "betternat_aws_gateway" "egress" {
  count = var.enable_nat_gateway && var.nat_backend == "betternat" ? 1 : 0

  name   = "${var.project_name}-egress-a"
  region = var.aws_region
  vpc_id = aws_vpc.main.id

  public_subnet_ids = {
    "${var.aws_region}${var.availability_zones[0]}" =
      aws_subnet.public[var.availability_zones[0]].id
  }

  private_route_table_ids = {
    "${var.aws_region}${var.availability_zones[0]}" =
      [aws_route_table.private[0].id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  ami_id              = data.aws_ami.al2023_arm64.id
  instance_type       = "t4g.small"
  use_spot            = false
  desired_capacity    = 2
  max_size            = 3
  betternat_version   = "v0.1.0"
  stable_egress_ip    = true
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

## Preserve Existing Outputs

If callers already read `nat_gateway_ip`, keep that output stable:

```hcl
output "nat_gateway_ip" {
  value = try(
    var.enable_nat_gateway && var.nat_backend == "aws_nat_gateway" ?
    aws_eip.nat[0].public_ip :
    values(betternat_aws_gateway.egress[0].egress_public_ips)[0],
    null
  )
}
```

Also expose the route tables that BetterNAT owns:

```hcl
output "private_route_table_ids" {
  value = [for rt in aws_route_table.private : rt.id]
}

output "rollback_route_targets_json" {
  value = try(betternat_aws_gateway.egress[0].rollback_route_targets_json, null)
}
```

## EKS Notes

EKS node groups can keep using the same private subnet IDs. The important change
is the private route table default route:

```text
before: 0.0.0.0/0 -> NAT Gateway
after:  0.0.0.0/0 -> active BetterNAT gateway node
```

After apply, verify:

```sh
terraform output
betternat status
betternat doctor --live
```

Then run an egress probe from a private EKS node or private test workload.

Also confirm rollback metadata is populated:

```sh
terraform output rollback_route_targets_json
```

If the output is empty or does not contain the previous private route targets,
do not rely on automated destroy rollback. Record manual `aws ec2 replace-route`
commands before moving production traffic.

## Next Step

Use [Existing VPC Install](EXISTING_VPC_INSTALL.md) for the cutover checklist,
rollback expectations, and route ownership warnings.
