# BetterNAT Quick Start

Date: 2026-06-21

## Purpose

This guide shows the Terraform migration shape first, then deploys BetterNAT into a disposable AWS VPC, verifies private-subnet egress, and destroys all resources.

Use this first. Do not start by replacing a production NAT Gateway.

## Scope

This guide is for the current public alpha Terraform install path: provider
`0.1.0-alpha.8` with BetterNAT runtime `v0.1.0-alpha.6`.

Important:

- BetterNAT does not publish a BetterNAT AMI in the current alpha.
- Terraform launches an explicit Linux AMI and uses cloud-init to install release artifacts at boot.
- Runtime `v0.1.0-alpha.8` exists for GA hardening validation, but this quick
  start uses `v0.1.0-alpha.6` because that is the runtime in the provider's
  built-in artifact manifest.
- The example uses one AZ.
- The example uses small EC2 instances and tiny HTTP probes.
- It does not run expensive multi-TB traffic tests.

## Terraform UX: Replace The NAT Backend

If your Terraform currently provisions a single-AZ AWS NAT Gateway, it probably has this shape:

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

With BetterNAT, the user-facing replacement is one resource. BetterNAT creates the node pool, EIP, lease table, IAM role, security group, launch template, ASG, and owns the private route target:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress-a"
  region = "us-west-2"
  vpc_id = aws_vpc.main.id

  public_subnet_ids = {
    "us-west-2a" = aws_subnet.public["us-west-2a"].id
  }

  private_route_table_ids = {
    "us-west-2a" = [aws_route_table.private["us-west-2a"].id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  ami_id              = data.aws_ami.al2023_arm64.id
  instance_type       = "t4g.small"
  desired_capacity    = 2
  max_size            = 3
  betternat_version   = "v0.1.0-alpha.6"
  stable_egress_ip    = true
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

The important migration rule:

- before: Terraform owns `aws_route.private_default` with `nat_gateway_id`,
- after: `betternat_gateway` owns that route table's default route so `betternat-agent` can move it during failover.

Do not keep a separate `aws_route` resource managing the same `0.0.0.0/0` private route after BetterNAT is active.

### Module-Level Switch

In a networking module, keep the old input and add a backend selector:

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

Then callers change only:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"
```

The module can keep compatibility outputs such as `nat_gateway_ip` by returning either the AWS NAT Gateway EIP or the BetterNAT EIP.

## Flow Diagram

Before:

![Before BetterNAT: AWS NAT Gateway route path](../../assets/betternat-before.svg)

After:

![After BetterNAT: node route, shared EIP, and AWS failover control plane](../../assets/betternat-after.svg)

For the datapath component BetterNAT uses inside each node, see the upstream [LoxiLB overview image](https://github.com/loxilb-io/loxilb/assets/75648333/87da0183-1a65-493f-b6fe-5bc738ba5468) and [LoxiLB standalone documentation](https://github.com/loxilb-io/loxilbdocs/blob/main/docs/standalone.md). BetterNAT uses LoxiLB as a local egress SNAT datapath; AWS route/EIP failover is handled by `betternat-agent`.

## Prerequisites

Install locally:

- Terraform,
- AWS CLI,
- an AWS profile with permission to create EC2, Auto Scaling, IAM, DynamoDB, and SSM resources.

Choose:

```sh
export AWS_PROFILE="<your-profile>"
export AWS_REGION="us-west-2"
export BETTERNAT_AZ="us-west-2a"
export BETTERNAT_VERSION="v0.1.0-alpha.6"
export BETTERNAT_RUN_ID="betternat-alpha-test-$(date -u +%Y%m%d%H%M%S)"
```

Expected AWS costs:

- EC2 gateway instances,
- one private test EC2 client,
- EBS volumes,
- EIP when stable egress IP is enabled,
- DynamoDB lease table,
- normal public internet data transfer,
- CloudWatch/SSM/logging if enabled by your account defaults.

## Select Runtime Version

Set `betternat_version` on the `betternat_gateway` resource. The provider uses
that version plus `instance_type` to select the correct Linux release artifacts
and built-in SHA256 checksums for bootstrap.

For unreleased local builds, use the maintainer AWS supplemental runbook instead
of this user quick start. That runbook may override `agent_binary_url`,
`agent_binary_sha256`, `cli_binary_url`, and `cli_binary_sha256` for test-only
binaries.

## Install Provider

The public Quick Start uses provider version `0.1.0-alpha.8`:

```hcl
source  = "nowakeai/betternat"
version = "= 0.1.0-alpha.8"
```

Terraform Registry install is the default path:

```sh
terraform -chdir=examples/terraform-aws-supplemental init
```

If Registry availability is temporarily delayed, install the provider from the
GitHub release as a filesystem mirror:

```sh
source scripts/setup-provider-github-mirror.sh
```

When using the mirror fallback, keep the `TF_CLI_CONFIG_FILE` environment
variable exported in the same shell for `terraform init`, `terraform plan`,
`terraform apply`, and `terraform destroy`.

## Deploy Disposable VPC

Initialize if needed:

```sh
terraform -chdir=examples/terraform-aws-supplemental init
```

Apply:

```sh
terraform -chdir=examples/terraform-aws-supplemental apply \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "betternat_version=$BETTERNAT_VERSION"
```

Expected:

- isolated VPC,
- public and private subnet,
- two BetterNAT gateway nodes in an ASG,
- one private test client,
- DynamoDB lease table,
- route table ownership moved to the active node,
- EIP associated to the active node when `stable_egress_ip=true`.

## Verify

Get outputs:

```sh
terraform -chdir=examples/terraform-aws-supplemental output
```

Use SSM to run on the active gateway node:

```sh
betternat version
betternat-agent --version
systemctl is-active betternat-agent.service
betternat status
betternat doctor --live
```

From the private test client, verify public egress:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

For stable EIP mode, the source IP should match the BetterNAT EIP.

## Destroy

Destroy Terraform resources:

```sh
terraform -chdir=examples/terraform-aws-supplemental destroy \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "betternat_version=$BETTERNAT_VERSION"
```

Residual scan:

```sh
aws resourcegroupstaggingapi get-resources \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --tag-filters "Key=BetterNATRunId,Values=$BETTERNAT_RUN_ID"
```

Terminated EC2 instances can remain visible briefly in tag results. Confirm direct EC2 state before treating them as live resources.
