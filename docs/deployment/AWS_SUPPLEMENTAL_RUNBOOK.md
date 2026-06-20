# AWS Supplemental Test Runbook

Date: 2026-06-20

## Scope

This runbook is the execution checklist for the low-cost AWS supplemental pass.

It assumes:

- region: `us-west-2`,
- first AZ: `us-west-2a`,
- isolated test VPC only,
- Spot EC2 where BetterNAT launches appliances,
- no NAT Gateway,
- no high-volume transfer tests,
- cleanup is part of the test.

Do not run against an existing production VPC.

## Readiness Gate

Before starting AWS:

```sh
./manage verify
./manage docs check
terraform -chdir=examples/terraform-localstack validate
```

Provider status:

- LocalStack apply/destroy has passed for the Terraform lifecycle.
- Real AWS test is still required because LocalStack does not prove route convergence, EIP reassociation timing, Spot behavior, EC2 boot, or real LoxiLB datapath.
- Published BetterNAT AMIs are not required for this pass.
- The supplemental fixture uses the latest official Amazon Linux 2023 arm64 AMI and cloud-init.
- `ami_channel` is not yet a resolver; do not test AMI channel behavior in this pass.

## AWS Inputs

Use a unique run ID:

```sh
export BETTERNAT_RUN_ID="bnat-$(date -u +%Y%m%d%H%M%S)"
export AWS_PROFILE="601427795217_AdministratorAccess"
export AWS_REGION="us-west-2"
export BETTERNAT_AZ="us-west-2a"
```

If the local network needs the proxy:

```sh
export HTTP_PROXY="http://127.0.0.1:10808"
export HTTPS_PROXY="http://127.0.0.1:10808"
export NO_PROXY="169.254.169.254,localhost,127.0.0.1"
```

Build a temporary Linux arm64 agent binary:

```sh
mkdir -p tmp/aws-supplemental
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -o tmp/aws-supplemental/betternat-agent ./cmd/betternat-agent
```

Upload it to a temporary private S3 location and generate a short-lived presigned URL:

```sh
export BETTERNAT_ARTIFACT_BUCKET="$BETTERNAT_RUN_ID-artifacts"
aws s3 mb "s3://$BETTERNAT_ARTIFACT_BUCKET" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION"

aws s3 cp tmp/aws-supplemental/betternat-agent \
  "s3://$BETTERNAT_ARTIFACT_BUCKET/betternat-agent-linux-arm64" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION"

export BETTERNAT_AGENT_BINARY_URL="$(
  aws s3 presign "s3://$BETTERNAT_ARTIFACT_BUCKET/betternat-agent-linux-arm64" \
    --expires-in 3600 \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION"
)"
```

The fixture can optionally accept `loxicmd_binary_url`, but it is not required for the first cloud-init pass. If no URL is provided, bootstrap creates a host `loxicmd` wrapper that executes `loxicmd` inside the LoxiLB container.

## Test Order

Run tests in this order:

1. `SUP-014` Terraform provider full lifecycle.
2. `SUP-001` route-only failover timing.
3. `SUP-002` stable-IP failover timing.
4. `SUP-003` client recovery timing.
5. `SUP-004` rollback timing.
6. `SUP-006` IAM least privilege.
7. `SUP-007` LoxiLB restart reconciliation.

Stop after any cleanup failure.

## SUP-014: Terraform Provider Lifecycle

Goal: prove the provider can create, read, roll back, and destroy real AWS resources in an isolated environment.

Fixture:

```text
examples/terraform-aws-supplemental
```

Initialize:

```sh
terraform -chdir=examples/terraform-aws-supplemental init
```

Plan:

```sh
terraform -chdir=examples/terraform-aws-supplemental plan \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL"
```

Apply:

```sh
terraform -chdir=examples/terraform-aws-supplemental apply \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL"
```

The fixture creates:

- isolated VPC,
- public subnet in `us-west-2a`,
- private subnet in `us-west-2a`,
- private route table with an existing default route so rollback can be proven,
- `betternat_gateway` with:

```hcl
ami_id        = data.aws_ami.al2023_arm64.id
instance_type = "t4g.small"
use_spot      = true
agent_binary_url = var.agent_binary_url
stable_egress_ip    = true
rollback_on_destroy = true
tags = {
  BetterNATRunId = var.run_id
}
```

Expected create behavior:

- IAM role/profile/policy created,
- security group created,
- DynamoDB lease table created,
- two Spot-backed appliance instances launched,
- source/destination check disabled,
- EIP allocated,
- EIP associated to the active appliance in stable-IP mode,
- previous route target captured,
- private route replaced to active appliance,
- state records route/EIP/instance IDs.

Expected destroy behavior:

- previous route target restored,
- instances terminated,
- EIP released,
- lease table deleted,
- IAM resources deleted,
- security group deleted.

Destroy:

```sh
terraform -chdir=examples/terraform-aws-supplemental destroy \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL"
```

## Timing Evidence

For each failover trial capture:

```text
t0 trigger start
t1 AWS API returned
t2 describe confirms desired route/EIP state
t3 private client new-flow success
t4 observed source IP
```

Use UTC timestamps with milliseconds:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
```

Minimum sample count:

- 5 route-only trials,
- 5 stable-IP trials.

## Cleanup Verification

After destroy or manual cleanup, verify by tag:

```sh
aws ec2 describe-instances \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --filters "Name=tag:BetterNATRunId,Values=$BETTERNAT_RUN_ID"

aws ec2 describe-addresses \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --filters "Name=tag:BetterNATRunId,Values=$BETTERNAT_RUN_ID"

aws ec2 describe-network-interfaces \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --filters "Name=tag:BetterNATRunId,Values=$BETTERNAT_RUN_ID"

aws ec2 describe-volumes \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --filters "Name=tag:BetterNATRunId,Values=$BETTERNAT_RUN_ID"
```

Also verify the lease table and IAM names used by the test no longer exist.

Remove the temporary artifact bucket:

```sh
aws s3 rm "s3://$BETTERNAT_ARTIFACT_BUCKET" --recursive \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION"

aws s3 rb "s3://$BETTERNAT_ARTIFACT_BUCKET" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION"
```

## Result Document

Write the run result to:

```text
docs/research/026-aws-supplemental-test-results.md
```

Minimum result sections:

- run metadata,
- exact topology,
- Provider lifecycle result,
- route-only timing table,
- stable-IP timing table,
- client recovery table,
- rollback result,
- IAM result,
- LoxiLB restart result,
- cleanup evidence,
- blockers and follow-up actions.

## Known Limits Before Running

- `ami_channel` does not resolve to a real AMI yet.
- Published BetterNAT AMIs do not exist yet; this is intentional for the current phase.
- Full datapath tests depend on cloud-init successfully installing Docker, starting LoxiLB, downloading `betternat-agent`, and creating the host `loxicmd` wrapper.
- This pass validates body/runtime behavior first. AMI boot-to-ready behavior comes later.
- LocalStack cannot validate AWS route propagation, EIP reassociation timing, Spot market behavior, SSM reachability, or LoxiLB kernel datapath.
