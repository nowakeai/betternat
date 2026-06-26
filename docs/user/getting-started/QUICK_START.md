# BetterNAT Quick Start

Date: 2026-06-21

## Purpose

This guide deploys BetterNAT into a disposable AWS VPC, verifies private-subnet
egress, and destroys all resources.

Use this first. Do not start by replacing a production NAT Gateway.

## Scope

This guide is for the current Terraform install path.

Important:

- BetterNAT does not publish a public BetterNAT AMI yet.
- Terraform launches an explicit Linux AMI and uses cloud-init to install release artifacts at boot.
- The example uses one AZ.
- The example uses small EC2 instances and tiny HTTP probes.
- It does not run expensive multi-TB traffic tests.

## What This Test Proves

This disposable test answers the first operational questions:

- Terraform can install the BetterNAT provider.
- BetterNAT gateway nodes can bootstrap from release artifacts.
- The active node can own the private route target.
- A private test client can reach the public internet.
- `betternat status`, `doctor --live`, and metrics expose useful state.
- Terraform destroy can clean up the test stack.

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
- `jq`,
- an AWS profile with permission to create EC2, Auto Scaling, IAM, DynamoDB, and SSM resources.

Choose:

```sh
export AWS_PROFILE="<your-profile>"
export AWS_REGION="us-west-2"
export BETTERNAT_AZ="us-west-2a"
export BETTERNAT_VERSION="v0.2.0"
export BETTERNAT_RUN_ID="betternat-test-$(date -u +%Y%m%d%H%M%S)"
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

Set `betternat_version` on the `betternat_aws_gateway` resource. The provider uses
that version plus `instance_type` to select the correct Linux release artifacts
and built-in SHA256 checksums for bootstrap.

For unreleased local builds, use the maintainer AWS supplemental runbook instead
of this user quick start. That runbook may override `agent_binary_url`,
`agent_binary_sha256`, `cli_binary_url`, and `cli_binary_sha256` for test-only
binaries.

## Install Provider

The public Quick Start pins the current provider version:

```hcl
source  = "nowakeai/betternat"
version = "= 0.2.0"
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

Get the active gateway and private client instance IDs:

```sh
export BETTERNAT_ACTIVE_INSTANCE_ID="$(
  terraform -chdir=examples/terraform-aws-supplemental output -json active_instance_ids |
    jq -r 'to_entries[0].value'
)"

export BETTERNAT_PRIVATE_CLIENT_ID="$(
  terraform -chdir=examples/terraform-aws-supplemental output -raw private_client_instance_id
)"
```

Use SSM to run the gateway checks:

```sh
export BETTERNAT_GATEWAY_CHECK_COMMAND_ID="$(
  aws ssm send-command \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --instance-ids "$BETTERNAT_ACTIVE_INSTANCE_ID" \
    --document-name "AWS-RunShellScript" \
    --parameters '{"commands":["betternat version","betternat-agent --version","systemctl is-active betternat-agent.service","sudo betternat status","sudo betternat doctor --live"]}' \
    --query "Command.CommandId" \
    --output text
)"

aws ssm wait command-executed \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --command-id "$BETTERNAT_GATEWAY_CHECK_COMMAND_ID" \
  --instance-id "$BETTERNAT_ACTIVE_INSTANCE_ID"

aws ssm list-command-invocations \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --command-id "$BETTERNAT_GATEWAY_CHECK_COMMAND_ID" \
  --details \
  --query "CommandInvocations[].CommandPlugins[].Output" \
  --output text
```

From the private test client, verify public egress:

```sh
export BETTERNAT_CLIENT_CHECK_COMMAND_ID="$(
  aws ssm send-command \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --instance-ids "$BETTERNAT_PRIVATE_CLIENT_ID" \
    --document-name "AWS-RunShellScript" \
    --parameters '{"commands":["curl -fsS https://checkip.amazonaws.com","curl -fsSI https://example.com"]}' \
    --query "Command.CommandId" \
    --output text
)"

aws ssm wait command-executed \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --command-id "$BETTERNAT_CLIENT_CHECK_COMMAND_ID" \
  --instance-id "$BETTERNAT_PRIVATE_CLIENT_ID"

aws ssm list-command-invocations \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --command-id "$BETTERNAT_CLIENT_CHECK_COMMAND_ID" \
  --details \
  --query "CommandInvocations[].CommandPlugins[].Output" \
  --output text
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

## Next Steps

- Read [Operations Guide](../operations/OPERATIONS_GUIDE.md) to understand
  day-2 status, metrics, handover records, and cleanup checks.
- Read [EKS Terraform Module Integration](EKS_TERRAFORM_MODULE_INTEGRATION.md)
  if you need to adapt an existing modular Terraform/EKS repository.
- Read [Existing VPC Install](EXISTING_VPC_INSTALL.md) before touching real
  private route tables.
