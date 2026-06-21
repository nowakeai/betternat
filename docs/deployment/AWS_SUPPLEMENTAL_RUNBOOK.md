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
- no multi-TB or tens-of-TB traffic tests,
- no billing-scale crawler/RPC/image-pull simulations,
- only tiny probe traffic for egress checks,
- cleanup is part of the test.

Do not run against an existing production VPC.

## Readiness Gate

Before starting AWS:

```sh
./manage verify
terraform -chdir=examples/terraform-localstack validate
```

Provider status:

- ASG-first Terraform create/destroy has passed in real AWS. See `docs/research/029-aws-asg-provider-test-results.md`.
- LocalStack Hobby cannot currently prove ASG lifecycle because Auto Scaling returns license-related HTTP 501 errors.
- Real AWS is still required for route convergence, EIP reassociation timing, Spot behavior, EC2 boot, ASG repair, and real LoxiLB datapath.
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

## Readiness Review

The AWS materials are ready for a combined low-cost run, but the run must be phased.

Already proven in run `bnat-20260620153304`:

- `SUP-014` Terraform/provider lifecycle,
- private client egress through the ASG appliance path,
- `SUP-015` ASG candidate repair,
- `SUP-018-A` automatic takeover when the owner agent is stopped,
- cleanup of VPC, EIP, ENI, EBS, ASG, DynamoDB, IAM-managed Terraform resources, and S3 artifact bucket.

Not yet cleanly proven:

- `SUP-018-B` owner instance termination by pre-existing standby takeover,
- `SUP-019` owner-loss repair with the replacement joining as standby,
- `SUP-016` scale-out/in.

Current blockers:

- `SUP-018-B` recovered through the ASG replacement instead of the existing standby. Add HA state logs/metrics and a readiness/status helper before treating it as complete.
- `SUP-016` still needs AWS verification. The fixture no longer uses a standalone `aws_route` for the active private default route; it bootstraps the pre-BetterNAT route once, then lets BetterNAT own runtime route targets.
- `SUP-007` still needs a LoxiLB datapath readiness/restart helper.

Recommended execution split inside one fixture lifecycle:

1. Baseline: apply, verify ASG health, route/EIP ownership, private client egress, and lease owner.
2. Non-owner disruption: run `SUP-015`.
3. Manual/control-plane timing: run `SUP-001`, `SUP-002`, `SUP-003`, and `SUP-004` only after datapath readiness is confirmed.
4. Automatic HA: run `SUP-018-A`, then `SUP-018-B`, then `SUP-019`.
5. Scale: run `SUP-016` after automatic HA observations are recorded.
6. Cleanup: destroy and verify no live tagged resources remain.

## Test Order

Run the low-cost batch in this order:

1. Baseline provider lifecycle and private client egress.
2. `SUP-015` ASG candidate repair.
3. `SUP-006` IAM least privilege review.
4. `SUP-001` route-only failover timing.
5. `SUP-002` stable-IP failover timing.
6. `SUP-003` client recovery timing.
7. `SUP-018-A` owner agent stop takeover.
8. `SUP-018-B` owner instance termination takeover.
9. `SUP-019` ASG repair after owner loss.
10. `SUP-004` rollback timing.

Run these after the missing product/test hooks exist:

- `SUP-007` LoxiLB restart reconciliation, after adding appliance access plus datapath readiness checks.

Stop after any cleanup failure.

Do not include large data-transfer or billing-scale workload tests in this run. The goal is low-cost functional evidence, not cost-model reproduction at production traffic volume.

Write the results into a new document, for example:

```text
docs/research/031-aws-low-cost-supplemental-results.md
```

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

Optional route-only fixture apply:

```sh
terraform -chdir=examples/terraform-aws-supplemental apply \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL" \
  -var "stable_egress_ip=false"
```

For the lowest-cost all-in-one timing pass, it is acceptable to apply once with `stable_egress_ip=true` and measure route-only timing by running only `ReplaceRoute` between ASG instances. In that case the shared EIP remains associated to the original owner while the route-only trial observes the candidate instance's own public IPv4 address.

The fixture creates:

- isolated VPC,
- public subnet in `us-west-2a`,
- private subnet in `us-west-2a`,
- private SSM-managed client instance in the private subnet,
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
- Launch Template created,
- one ASG created for `us-west-2a`,
- Spot-backed appliance pool launched with `desired_capacity = 2`,
- current owner instance selected from the ASG,
- EIP allocated,
- EIP associated to the current owner in stable-IP mode,
- previous route target captured,
- private route replaced to current owner,
- state records route/EIP/current owner IDs,
- private client instance created after BetterNAT route installation.

Useful outputs:

```sh
terraform -chdir=examples/terraform-aws-supplemental output aws_cli_context
terraform -chdir=examples/terraform-aws-supplemental output -raw asg_name
terraform -chdir=examples/terraform-aws-supplemental output -raw private_client_instance_id
terraform -chdir=examples/terraform-aws-supplemental output -raw private_route_table_id
```

Expected destroy behavior:

- previous route target restored,
- ASG deleted so it stops replacing instances,
- Launch Template deleted,
- EIP released,
- lease table deleted,
- IAM resources deleted,
- security group deleted.

## Common AWS Queries

Set shell helpers after apply:

```sh
export BETTERNAT_ASG_NAME="$(
  terraform -chdir=examples/terraform-aws-supplemental output -raw asg_name
)"
export BETTERNAT_PRIVATE_CLIENT_ID="$(
  terraform -chdir=examples/terraform-aws-supplemental output -raw private_client_instance_id
)"
export BETTERNAT_PRIVATE_RTB="$(
  terraform -chdir=examples/terraform-aws-supplemental output -raw private_route_table_id
)"
```

List ASG instances:

```sh
aws autoscaling describe-auto-scaling-groups \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --auto-scaling-group-names "$BETTERNAT_ASG_NAME" \
  --query 'AutoScalingGroups[0].Instances[].{InstanceId:InstanceId,LifecycleState:LifecycleState,HealthStatus:HealthStatus}' \
  --output table
```

Find current route owner:

```sh
export BETTERNAT_OWNER_ID="$(
  aws ec2 describe-route-tables \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --route-table-ids "$BETTERNAT_PRIVATE_RTB" \
    --query 'RouteTables[0].Routes[?DestinationCidrBlock==`0.0.0.0/0`].InstanceId | [0]' \
    --output text
)"
```

Find shared EIP allocation, when stable egress IP is enabled:

```sh
export BETTERNAT_EIP_ALLOCATION_ID="$(
  aws ec2 describe-addresses \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --filters "Name=tag:BetterNATRunId,Values=$BETTERNAT_RUN_ID" \
    --query 'Addresses[0].AllocationId' \
    --output text
)"
```

Find candidate instances:

```sh
aws autoscaling describe-auto-scaling-groups \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --auto-scaling-group-names "$BETTERNAT_ASG_NAME" \
  --query "AutoScalingGroups[0].Instances[?InstanceId!='${BETTERNAT_OWNER_ID}'].InstanceId" \
  --output text
```

Set the first candidate as a variable:

```sh
export BETTERNAT_CANDIDATE_ID="$(
  aws autoscaling describe-auto-scaling-groups \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --auto-scaling-group-names "$BETTERNAT_ASG_NAME" \
    --query "AutoScalingGroups[0].Instances[?InstanceId!='${BETTERNAT_OWNER_ID}'].InstanceId | [0]" \
    --output text
)"
```

Run a tiny private-client egress probe through SSM:

```sh
aws ssm send-command \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --instance-ids "$BETTERNAT_PRIVATE_CLIENT_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["date -u +%Y-%m-%dT%H:%M:%S.%3NZ","curl -4 --connect-timeout 2 --max-time 5 -fsS https://checkip.amazonaws.com","curl -4 --connect-timeout 2 --max-time 5 -fsSI https://example.com | head -n 1"]' \
  --query 'Command.CommandId' \
  --output text
```

Fetch command output:

```sh
aws ssm list-command-invocations \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --command-id <command-id> \
  --details \
  --query 'CommandInvocations[0].CommandPlugins[0].Output' \
  --output text
```

If the private client does not appear as SSM-managed, do not continue client recovery timing. Treat that as fixture/bootstrap failure and either inspect the appliance/client boot path or add a temporary SSH/bastion access path.

Run appliance status checks through SSM:

```sh
aws ssm send-command \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --instance-ids "$BETTERNAT_OWNER_ID" "$BETTERNAT_CANDIDATE_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["hostname","systemctl is-active betternat-agent.service || true","systemctl is-active docker || true","docker ps --filter name=loxilb --format {{.Names}}:{{.Status}} || true","curl -fsS http://127.0.0.1:9108/metrics | egrep \"betternat_(ha_state|lease_owner_match|lease_seconds_until_expiry|takeover|route_target_match|public_identity_match|datapath_ready)\" || true","journalctl -u betternat-agent.service -n 120 --no-pager | egrep \"betternat_ha_step|error|failed|lease|takeover\" || true"]' \
  --query 'Command.CommandId' \
  --output text
```

Do not use `betternat-agent --once` as a HA appliance readiness check. In HA mode it can run control-plane reconciliation and may affect lease or ownership state. Use only read-only checks: service status, LoxiLB container status, `/metrics`, journal logs, AWS route/EIP state, and DynamoDB lease state.

Stop the owner agent to trigger an agent-level takeover without terminating the instance:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ssm send-command \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --instance-ids "$BETTERNAT_OWNER_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["sudo systemctl stop betternat-agent.service","date -u +%Y-%m-%dT%H:%M:%S.%3NZ"]' \
  --query 'Command.CommandId' \
  --output text
```

Do not run manual `associate-address` or `replace-route` commands during `SUP-018`. The point of the test is to prove the agent performs those mutations.

## SUP-015: ASG Candidate Repair

Goal: prove ASG repairs non-owner capacity without moving the current route/EIP owner.

Procedure:

1. Apply the fixture.
2. Set the helper environment variables from "Common AWS Queries".
3. Identify candidate:

```sh
export BETTERNAT_CANDIDATE_ID="$(
  aws autoscaling describe-auto-scaling-groups \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --auto-scaling-group-names "$BETTERNAT_ASG_NAME" \
    --query "AutoScalingGroups[0].Instances[?InstanceId!='${BETTERNAT_OWNER_ID}'].InstanceId | [0]" \
    --output text
)"
```

4. Terminate candidate:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 terminate-instances \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --instance-ids "$BETTERNAT_CANDIDATE_ID"
```

5. Poll ASG until there are two `InService` instances again.
6. Verify route and EIP still point to `$BETTERNAT_OWNER_ID`.
7. Verify the replacement instance has `SourceDestCheck=false`.

Success:

- ASG returns to desired capacity 2,
- route owner is unchanged,
- EIP owner is unchanged in stable-IP mode,
- replacement candidate is ready for later takeover tests.

## SUP-016: ASG Scale-Out And Scale-In

Scale out:

```sh
terraform -chdir=examples/terraform-aws-supplemental apply \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL" \
  -var "desired_capacity=3" \
  -var "max_size=3"
```

Verify:

- ASG has three `InService` instances,
- route/EIP owner is still known,
- all three instances eventually have `SourceDestCheck=false`.

Scale back down:

```sh
terraform -chdir=examples/terraform-aws-supplemental apply \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "agent_binary_url=$BETTERNAT_AGENT_BINARY_URL" \
  -var "desired_capacity=2" \
  -var "max_size=3"
```

If ASG chooses to terminate the current owner during scale-in before the HA loop exists, record it as the current limitation and continue to cleanup.

## SUP-018/SUP-019: Automatic HA Takeover

Goal: prove the real product HA loop, then prove ASG restores standby capacity.

Preconditions:

- fixture apply succeeded,
- both appliances are SSM-managed,
- both appliances report `betternat-agent.service` active,
- `/metrics` exposes `betternat_ha_state`, `betternat_lease_owner_match`, and `betternat_datapath_ready`,
- private client egress works,
- route/EIP owner is known,
- candidate has LoxiLB running.

Agent-stop takeover trial:

1. Set common environment variables.
2. Run the private-client egress probe and record source IP.
3. Run appliance status checks.
4. Stop `betternat-agent.service` on `$BETTERNAT_OWNER_ID` through SSM.
5. Poll route and EIP state until they point at `$BETTERNAT_CANDIDATE_ID`.
6. Run private-client egress probe again.
7. Fetch candidate agent logs.

Owner-termination takeover trial:

1. Restore owner/candidate variables after the agent-stop trial.
2. Terminate the current owner instance:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 terminate-instances \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --instance-ids "$BETTERNAT_OWNER_ID"
```

3. Poll route and EIP state until they point at the candidate.
4. Poll ASG until desired capacity is restored.
5. Verify the replacement instance appears in SSM.
6. Verify the replacement joins as standby, not active.

Success:

- no manual EIP or route mutation is used,
- candidate acquires the lease,
- shared EIP moves to candidate in stable-IP mode,
- private route points to candidate,
- private client new flows recover,
- ASG creates a replacement,
- replacement does not steal ownership from the active candidate,
- no two nodes claim active ownership in logs/metrics.

Record:

- failure trigger timestamp,
- first route/EIP describe timestamp showing candidate,
- first private-client success after failure,
- observed source IP before and after,
- old owner ID,
- new owner ID,
- replacement instance ID,
- relevant agent log excerpts.

## SUP-001/SUP-002/SUP-003: Manual Timing Pass

Use only tiny probe traffic.

Route-only trial:

1. Identify owner and candidate.
2. Start a repeated SSM egress probe from the private client, or issue one probe before and one after the route change.
3. Run only `ReplaceRoute`:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id "$BETTERNAT_PRIVATE_RTB" \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id "$BETTERNAT_CANDIDATE_ID"
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
```

4. Poll `DescribeRouteTables` until target is candidate.
5. Run the SSM egress probe and record observed source IP.

Stable-IP trial:

1. Identify current route/EIP owner and candidate.
2. Move EIP then route:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 associate-address \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --allocation-id "$BETTERNAT_EIP_ALLOCATION_ID" \
  --instance-id "$BETTERNAT_CANDIDATE_ID" \
  --allow-reassociation
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id "$BETTERNAT_PRIVATE_RTB" \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id "$BETTERNAT_CANDIDATE_ID"
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
```

3. Poll `DescribeAddresses` and `DescribeRouteTables`.
4. Run the SSM egress probe.

Record:

- API start/end timestamps,
- describe convergence timestamps,
- first successful private-client probe timestamp,
- observed source IP before and after,
- whether the trial was route-only or stable-IP.

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
