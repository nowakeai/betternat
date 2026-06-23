# BetterNAT Rollback Guide

Date: 2026-06-22

This guide explains how to safely remove or roll back a BetterNAT alpha deployment.

Use this guide before deleting gateway instances, route tables, EIPs, Auto Scaling groups, or DynamoDB tables by hand.

## What Rollback Means

BetterNAT sends private-subnet egress through AWS private route table entries such as:

```text
0.0.0.0/0 -> active BetterNAT gateway node
```

During install, the provider snapshots the previous target for every managed private route table. During destroy, it can restore those routes before deleting BetterNAT-managed resources.

Supported rollback targets include:

- NAT Gateway IDs: `nat-*`
- EC2 instance IDs: `i-*`
- ENI IDs: `eni-*`
- Internet Gateway IDs: `igw-*`
- Virtual Private Gateway IDs: `vgw-*`
- Transit Gateway IDs: `tgw-*`
- VPC Peering Connection IDs: `pcx-*`
- Egress-only Internet Gateway IDs: `eigw-*`

If the previous target is missing or unknown, BetterNAT will not silently destroy the gateway and leave private subnets without a known route unless you explicitly opt in.

## Terraform Defaults

The relevant resource fields are:

```hcl
resource "betternat_gateway" "egress" {
  rollback_on_destroy              = true
  allow_destroy_without_rollback   = false
}
```

Defaults:

| Field | Default | Meaning |
| --- | --- | --- |
| `rollback_on_destroy` | `true` | Restore captured private route table targets during `terraform destroy`. |
| `allow_destroy_without_rollback` | `false` | Refuse destroy if rollback targets are unknown or incomplete. |

Keep these defaults unless you have manually restored the private route table state or you intentionally accept temporary egress loss.

## Normal Destroy

For a disposable or test deployment, use Terraform destroy:

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

Expected order:

1. Terraform reads `rollback_route_targets_json` from state.
2. The provider restores the previous target for each managed private route table.
3. The provider scales down and deletes BetterNAT Auto Scaling groups.
4. The provider deletes launch templates, EIPs, DynamoDB lease table, IAM resources, and the BetterNAT security group.
5. Terraform deletes the surrounding fixture resources if the example created them.

## Verify Route Restoration

After destroy, inspect the private route tables that BetterNAT managed:

```sh
aws ec2 describe-route-tables \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-ids rtb-xxxxxxxx \
  --query 'RouteTables[].Routes[?DestinationCidrBlock==`0.0.0.0/0`]'
```

Confirm that `0.0.0.0/0` no longer points to a BetterNAT gateway node or ENI unless that is the route state you intentionally chose.

Also scan for tagged residual resources:

```sh
aws resourcegroupstaggingapi get-resources \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --tag-filters "Key=BetterNATRunId,Values=$BETTERNAT_RUN_ID"
```

Terminated EC2 instances may remain visible briefly. Check direct EC2 state before treating them as live resources.

## If Destroy Refuses To Roll Back

You may see an error like:

```text
Refusing to destroy BetterNAT gateway without rollback targets
```

This means:

- `rollback_on_destroy` is `true`,
- `allow_destroy_without_rollback` is `false`,
- and Terraform state does not contain concrete previous route targets.

Do not immediately set `allow_destroy_without_rollback = true`.

First inspect the route tables:

```sh
terraform -chdir=<your-config-dir> state show betternat_gateway.egress

aws ec2 describe-route-tables \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-ids <private-route-table-id>
```

Then decide one of these paths:

1. Restore private routes manually to a known-good target, such as the previous NAT Gateway.
2. Re-run `terraform destroy` after the route state is safe.
3. Only if you have already restored or intentionally accepted the route state, set:

```hcl
allow_destroy_without_rollback = true
```

Then run destroy again.

## Manual Route Restore

If you need to restore a private route table manually, use the correct AWS route target for your fallback path.

Example: restore to a NAT Gateway:

```sh
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id rtb-xxxxxxxx \
  --destination-cidr-block 0.0.0.0/0 \
  --nat-gateway-id nat-xxxxxxxx
```

Example: restore to an EC2 NAT instance:

```sh
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id rtb-xxxxxxxx \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id i-xxxxxxxx
```

After manual restore, verify egress from a private instance:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

## Stale Rollback Targets

If the captured previous target no longer exists, the provider skips some stale-target errors during rollback:

- missing ENI,
- missing NAT Gateway,
- missing EC2 instance.

That prevents destroy from being permanently blocked by an already-deleted fallback target, but it also means you must verify the private route table after destroy. A skipped stale target may leave the route table in a state that still needs manual correction.

## What Not To Do

Avoid these actions unless you are following a deliberate manual recovery plan:

- Do not delete BetterNAT EC2 instances before route rollback.
- Do not delete the DynamoDB lease table while agents are still running.
- Do not delete EIPs before checking which public identity private subnets should use after rollback.
- Do not delete route tables managed by another Terraform stack.
- Do not run competing Terraform resources that also manage the same `0.0.0.0/0` route while BetterNAT is active.

## Production Rollback Pattern

For production migration from NAT Gateway to BetterNAT:

1. Keep the existing NAT Gateway until BetterNAT is verified.
2. Use BetterNAT on selected private route tables first.
3. Verify private egress and observability.
4. Confirm `rollback_route_targets_json` contains concrete previous targets.
5. Keep a manual `replace-route` command ready for each private route table.
6. Only remove the old NAT Gateway after the rollback window is over.

## Current Alpha Limitations

- There is no managed BetterNAT control server for one-click rollback.
- Active flows may reset during route changes.
- Rollback is route-target based; it does not recreate a deleted previous NAT Gateway.
- Destroy is not an upgrade mechanism. For upgrades, prefer the upgrade/replacement guide once it is available.
