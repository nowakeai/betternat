# BetterNAT Quick Start

Date: 2026-06-21

## Purpose

This guide deploys BetterNAT into a disposable AWS VPC, verifies private-subnet egress, and destroys all resources.

Use this first. Do not start by replacing a production NAT Gateway.

## Scope

This guide is for `v0.1.0-alpha.1`.

Important:

- BetterNAT does not publish a BetterNAT AMI in the first alpha.
- Terraform launches an explicit Linux AMI and uses cloud-init to install release artifacts at boot.
- The example uses one AZ.
- The example uses small EC2 instances and tiny HTTP probes.
- It does not run expensive multi-TB traffic tests.

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
export BETTERNAT_VERSION="v0.1.0-alpha.1"
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

## Select Release Artifacts

The public alpha install path downloads binaries from GitHub Release assets. BetterNAT does not provide or require a user-managed S3 artifact bucket.

For the default arm64 test fixture, use these release assets:

```text
betternat-agent_<version>_linux_arm64
betternat_<version>_linux_arm64
SHA256SUMS
```

Set release URLs:

```sh
export BETTERNAT_RELEASE_BASE="https://github.com/nowakeai/betternat/releases/download/$BETTERNAT_VERSION"

export BETTERNAT_AGENT_BINARY_URL="$BETTERNAT_RELEASE_BASE/betternat-agent_${BETTERNAT_VERSION}_linux_arm64"
export BETTERNAT_CLI_BINARY_URL="$BETTERNAT_RELEASE_BASE/betternat_${BETTERNAT_VERSION}_linux_arm64"
export BETTERNAT_SHA256SUMS_URL="$BETTERNAT_RELEASE_BASE/SHA256SUMS"
```

Read checksums from the release checksum file:

```sh
curl -fsSL "$BETTERNAT_SHA256SUMS_URL" -o "tmp/SHA256SUMS-$BETTERNAT_VERSION"

export BETTERNAT_AGENT_BINARY_SHA256="$(
  awk -v f="betternat-agent_${BETTERNAT_VERSION}_linux_arm64" '$2 == f {print $1}' "tmp/SHA256SUMS-$BETTERNAT_VERSION"
)"

export BETTERNAT_CLI_BINARY_SHA256="$(
  awk -v f="betternat_${BETTERNAT_VERSION}_linux_arm64" '$2 == f {print $1}' "tmp/SHA256SUMS-$BETTERNAT_VERSION"
)"
```

Check that both checksums were found:

```sh
test -n "$BETTERNAT_AGENT_BINARY_SHA256"
test -n "$BETTERNAT_CLI_BINARY_SHA256"
```

For unreleased local builds, use the maintainer AWS supplemental runbook instead of this user quick start. That runbook may use temporary private artifact hosting for test-only binaries.

## Use Registry Provider

The public Quick Start uses the Terraform Registry provider:

```hcl
source  = "nowakeai/betternat"
version = "= 0.1.0-alpha.2"
```

Do not set `TF_CLI_CONFIG_FILE` for this guide. Local provider override files are for provider development only.

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
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL" \
  -var "agent_binary_sha256=$BETTERNAT_AGENT_BINARY_SHA256" \
  -var "cli_binary_url=$BETTERNAT_CLI_BINARY_URL" \
  -var "cli_binary_sha256=$BETTERNAT_CLI_BINARY_SHA256"
```

Expected:

- isolated VPC,
- public and private subnet,
- two BetterNAT gateway appliances in an ASG,
- one private test client,
- DynamoDB lease table,
- route table ownership moved to the active appliance,
- EIP associated to the active appliance when `stable_egress_ip=true`.

## Verify

Get outputs:

```sh
terraform -chdir=examples/terraform-aws-supplemental output
```

Use SSM to run on the active gateway appliance:

```sh
betternat version
betternat-agent --version
systemctl is-active betternat-agent.service
betternat doctor --live --config /etc/betternat/agent.json
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
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL" \
  -var "agent_binary_sha256=$BETTERNAT_AGENT_BINARY_SHA256" \
  -var "cli_binary_url=$BETTERNAT_CLI_BINARY_URL" \
  -var "cli_binary_sha256=$BETTERNAT_CLI_BINARY_SHA256"
```

Residual scan:

```sh
aws resourcegroupstaggingapi get-resources \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --tag-filters "Key=BetterNATRunId,Values=$BETTERNAT_RUN_ID"
```

Terminated EC2 instances can remain visible briefly in tag results. Confirm direct EC2 state before treating them as live resources.
