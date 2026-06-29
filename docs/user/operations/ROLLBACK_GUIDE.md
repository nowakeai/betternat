# BetterNAT Rollback Guide

Date: 2026-06-26

This guide explains how to safely remove or roll back a BetterNAT deployment.

Use this guide before deleting gateway instances, route tables, EIPs, Auto Scaling groups, or DynamoDB tables by hand.
For GCP, use it before deleting gateway instances, routes, regional static
addresses, Managed Instance Groups, service accounts, or Firestore records by
hand.

## Emergency Route Restore

Use this section when private workloads have lost egress and you already know
the previous route target.

Set the route table and fallback target:

```sh
export AWS_REGION=us-west-2
export PRIVATE_RTB_ID=rtb-xxxxxxxx
export FALLBACK_NAT_GW_ID=nat-xxxxxxxx
```

Restore the private default route to the previous NAT Gateway:

```sh
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id "$PRIVATE_RTB_ID" \
  --destination-cidr-block 0.0.0.0/0 \
  --nat-gateway-id "$FALLBACK_NAT_GW_ID"
```

If the fallback target is an EC2 NAT instance instead:

```sh
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id "$PRIVATE_RTB_ID" \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id i-xxxxxxxx
```

If the fallback target is an ENI:

```sh
aws ec2 replace-route \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-id "$PRIVATE_RTB_ID" \
  --destination-cidr-block 0.0.0.0/0 \
  --network-interface-id eni-xxxxxxxx
```

Verify the route and egress:

```sh
aws ec2 describe-route-tables \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --route-table-ids "$PRIVATE_RTB_ID" \
  --query 'RouteTables[].Routes[?DestinationCidrBlock==`0.0.0.0/0`]'

curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

After egress is restored, decide whether to keep BetterNAT running for
diagnostics or run Terraform destroy. Do not delete BetterNAT instances, route
tables, EIPs, or the coordination table by hand before route state is safe.

## What Rollback Means

BetterNAT sends private-subnet egress through cloud routes such as:

```text
0.0.0.0/0 -> active BetterNAT gateway node
```

During install, the provider snapshots the previous target for every managed private route table. During destroy, it can restore those routes before deleting BetterNAT-managed resources.

On GCP, BetterNAT owns the tagged static route named by `route_name`. The
module does not create or delete an existing regional static external IPv4
address used for stable public identity, so that address remains in the calling
Terraform stack or infra-admin stack.

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

The relevant module/provider fields are:

```hcl
resource "betternat_aws_gateway" "egress" {
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

## Choose The Right Path

| Situation | Path |
| --- | --- |
| Private egress is down now and the old route target still exists. | Use [Emergency Route Restore](#emergency-route-restore), then investigate. |
| You are cleaning up a disposable test deployment. | Use [Normal Destroy](#normal-destroy). |
| `terraform destroy` refuses to continue because rollback metadata is missing. | Use [If Destroy Refuses To Roll Back](#if-destroy-refuses-to-roll-back). |
| The previous route target was deleted. | Read [Stale Rollback Targets](#stale-rollback-targets) before destroying anything else. |
| You are migrating production from NAT Gateway to BetterNAT. | Follow [Production Rollback Pattern](#production-rollback-pattern). |
| You are testing GCP in a disposable VPC. | Use [GCP Destroy And Cleanup](#gcp-destroy-and-cleanup). |

## Normal Destroy

For a disposable or test deployment, use Terraform destroy:

```sh
terraform -chdir=examples/terraform-aws-supplemental destroy \
  -var "run_id=$BETTERNAT_RUN_ID" \
  -var "region=$AWS_REGION" \
  -var "az=$BETTERNAT_AZ" \
  -var "betternat_version=$BETTERNAT_VERSION"
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

## GCP Destroy And Cleanup

For a disposable GCP deployment, use Terraform destroy first:

```sh
terraform destroy
```

Expected order:

1. Terraform removes BetterNAT gateway capacity.
2. The provider removes the tagged route it owns.
3. The provider removes instance templates, MIGs, custom IAM bindings, and
   Firestore database only when those lifecycles were enabled in the same
   stack.
4. Provider-managed GCP runtime service accounts are retained during gateway
   cleanup for reliable same-name replacement. Remove a retained runtime
   service account only after all gateways using it are destroyed.
5. Terraform removes surrounding VPC fixture resources if the example created
   them.

After destroy, verify no run-scoped resources remain:

```sh
gcloud compute instances list --filter="name~<run-id>"
gcloud compute routes list --filter="name~<run-id>"
gcloud compute firewall-rules list --filter="name~<run-id>"
gcloud iam service-accounts list --filter="email~<run-id>"
```

The service-account check may show a retained BetterNAT runtime service account
when `manage_runtime_service_account = true`. That is expected. Confirm no live
gateway still uses it before deleting it.

If stable public identity used a regional static external IPv4 address owned by
the calling stack, confirm whether that address should remain reserved or be
destroyed by its owning Terraform resource. Do not delete a shared address by
hand while another route or allowlist still depends on it.

Firestore handover records may outlive a destroyed disposable gateway when the
Firestore database is shared. Delete only run-scoped BetterNAT documents after
confirming the gateway stack is gone.

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
terraform -chdir=<your-config-dir> state list | grep betternat
terraform -chdir=<your-config-dir> state show <betternat-resource-address>

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

Manual route restore means replacing the private route table default route with
a known-good fallback target, usually the previous NAT Gateway, NAT instance, or
ENI. Use the commands in [Emergency Route Restore](#emergency-route-restore).

After manual restore:

1. Verify the AWS route table points to the intended fallback target.
2. Verify egress from a private workload.
3. Keep BetterNAT resources intact until Terraform state, route ownership, and
   rollback metadata are understood.
4. Run Terraform destroy only after route state is safe.

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
- Do not delete GCP Firestore records, MIGs, routes, or regional static
  addresses before route ownership is understood.
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

## Current Limitations

- There is no managed BetterNAT control server for one-click rollback.
- Active flows may reset during route changes.
- Rollback is route-target based; it does not recreate a deleted previous NAT Gateway.
- Destroy is not an upgrade mechanism. For upgrades, prefer the upgrade/replacement guide.
