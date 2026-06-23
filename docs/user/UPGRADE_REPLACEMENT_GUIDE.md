# BetterNAT Upgrade And Replacement Guide

Date: 2026-06-23

## Purpose

This guide explains how to change a BetterNAT deployment safely in the first alpha.

The short version:

- capacity-only changes are supported in-place,
- provider-owned infrastructure migrations should be reconciled in-place when they do not affect runtime or route/EIP ownership,
- runtime, bootstrap, datapath, HA timing, route, subnet, AMI, and EIP-mode changes require explicit replacement,
- the first alpha does not provide seamless rolling software or AMI upgrades,
- use blue/green replacement for safer upgrades of real workloads.

## Version Model

BetterNAT has two version layers:

1. Terraform provider version.
2. Gateway runtime version.

The provider version is set in Terraform/OpenTofu:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.2"
    }
  }
}
```

For OpenTofu, use the Terraform Registry hostname until the OpenTofu-native registry entry is approved:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "registry.terraform.io/nowakeai/betternat"
      version = "= 0.1.0-alpha.2"
    }
  }
}
```

The gateway runtime version is controlled separately. In the first alpha, it is selected by bootstrap artifact URLs and checksums:

```hcl
agent_binary_url    = var.agent_binary_url
agent_binary_sha256 = var.agent_binary_sha256
cli_binary_url      = var.cli_binary_url
cli_binary_sha256   = var.cli_binary_sha256
```

Changing the provider version does not automatically upgrade running gateway nodes.

Provider upgrades can still reconcile provider-owned infrastructure in place. For example, a newer provider may update the BetterNAT-managed IAM inline policy, create a new coordination table, update tags, or record migration metadata without replacing the gateway nodes.

## Supported In-Place Updates

The first alpha supports user-requested in-place updates for capacity fields:

```hcl
min_size         = 2
desired_capacity = 3
max_size         = 3
```

When only these fields change, the provider updates ASG capacity and keeps the existing gateway identity, route ownership, rollback metadata, and runtime configuration.

Use this for:

- adding standby capacity,
- restoring a degraded standby pool,
- shrinking from three nodes to two after a test.

Before applying a capacity change, verify:

```sh
terraform plan
terraform output active_instance_ids
terraform output standby_instance_ids
```

After applying:

```sh
terraform apply
terraform output active_instance_ids
terraform output standby_instance_ids
```

Then check metrics or node-local diagnostics:

```sh
sudo betternat doctor --live --config /etc/betternat/agent.json
sudo betternat datapath ready --config /etc/betternat/agent.json
```

## Provider-Owned Infrastructure Reconciliation

Provider-owned infrastructure changes are different from user-requested runtime changes.

The provider should reconcile its own supporting infrastructure in place when the change is safe and does not affect the active datapath.

Safe in-place reconciliation examples:

- overwriting the BetterNAT-managed IAM inline policy named `betternat-runtime`,
- removing permissions that a newer provider no longer needs,
- adding permissions for a new provider-owned coordination backend,
- creating a provider-owned DynamoDB coordination table,
- updating provider-owned tags or metadata,
- recording migration metadata in Terraform state.

These changes must not:

- stop or replace running gateway nodes,
- mutate private route table targets,
- associate or disassociate EIPs,
- change bootstrap user data,
- change the agent runtime config without an explicit runtime rollout.

The provider may only reconcile resources it owns by contract. If you attach your own IAM policy to the BetterNAT role, the provider should leave that user-managed policy alone. The provider can replace the content of its own `betternat-runtime` inline policy because that policy is generated and owned by BetterNAT.

This means a provider upgrade that tightens permissions should converge the BetterNAT-managed policy on the next apply without requiring a gateway replacement.

The provider records an internal `provider_infrastructure_revision` value in resource state. When a newer provider needs a safe infrastructure-only migration, it can bump that revision so Terraform plans an in-place update even if the user's configuration did not change. That update path reconciles provider-owned infrastructure such as IAM policy documents and coordination tables, but it must not roll nodes or mutate route/EIP ownership.

## Changes That Require Replacement

The provider intentionally rejects non-capacity updates in the first alpha.

Replacement is required for changes such as:

- `ami_id`
- `ami_channel`
- `agent_binary_url`
- `agent_binary_sha256`
- `cli_binary_url`
- `cli_binary_sha256`
- `loxicmd_binary_url`
- `loxicmd_binary_sha256`
- `instance_type`
- `public_subnet_ids`
- `private_route_table_ids`
- `private_cidrs`
- `datapath_engine`
- `stable_egress_ip`
- `ha_profile`
- `ha_lease_ttl_seconds`
- `ha_renew_interval_seconds`
- `route_mode`
- `route_destination_cidr`
- `route_target_type`
- tags and other installation inputs.

If you try to update these fields in place, Terraform reports:

```text
BetterNAT gateway replacement required
```

This is deliberate. A silent in-place mutation could leave private routes, EIP association, lease ownership, datapath rules, and rollback metadata inconsistent.

## Replacement Options

### Option A: Explicit Terraform Replacement

For disposable or non-critical environments, replace the resource explicitly:

```sh
terraform apply -replace=betternat_gateway.egress
```

Use this only when you accept disruption. Active flows may reset, and new flows may fail until the replacement is ready and routes converge.

Recommended checks after replacement:

```sh
terraform output betternat_status
terraform output active_instance_ids
terraform output standby_instance_ids
terraform output egress_public_ips
```

From a private client:

```sh
curl -fsS https://checkip.amazonaws.com
```

On the active node:

```sh
sudo betternat doctor --live --config /etc/betternat/agent.json
sudo betternat datapath ready --config /etc/betternat/agent.json
```

### Option B: Blue/Green Gateway Replacement

For important workloads, prefer blue/green replacement.

High-level flow:

1. Keep the existing BetterNAT gateway serving production route tables.
2. Deploy a new BetterNAT gateway with a different `name`.
3. Attach the new gateway to a test private route table first.
4. Verify egress, metrics, HA state, route ownership, and EIP behavior.
5. Migrate selected private route tables to the new gateway.
6. Keep the old gateway during the rollback window.
7. Destroy the old gateway only after rollback is no longer needed.

Example naming:

```hcl
resource "betternat_gateway" "egress_v1" {
  name = "prod-egress-v1"
  # old runtime or AMI
}

resource "betternat_gateway" "egress_v2" {
  name = "prod-egress-v2"
  # new runtime or AMI
}
```

Do not attach both gateways to the same private route table at the same time. A private route table default route should have one owner.

## Blue/Green Checklist

Before creating the new gateway:

- record the old gateway outputs,
- confirm `rollback_route_targets_json` contains concrete route targets,
- confirm the old gateway has at least one healthy active node,
- decide which private route tables will be used for testing.

After creating the new gateway:

- verify ASG healthy capacity,
- verify one active node,
- verify standby capacity if `desired_capacity >= 2`,
- verify `betternat_datapath_ready == 1`,
- verify `betternat_route_target_match == 1`,
- verify `betternat_public_identity_match == 1` when stable egress IP is enabled,
- run an egress IP probe from a test private client.

Before moving production route tables:

- ensure users understand active connections may reset,
- choose a low-risk window for sensitive workloads,
- keep manual route rollback commands ready,
- keep the old NAT Gateway or old BetterNAT gateway available until the rollback window ends.

## Rollback During Replacement

If the new gateway fails validation, keep or restore the private route table default route to the old target.

For a route table that should point back to a NAT Gateway:

```sh
aws ec2 replace-route \
  --route-table-id <rtb-id> \
  --destination-cidr-block 0.0.0.0/0 \
  --nat-gateway-id <nat-gateway-id>
```

For a route table that should point back to a BetterNAT instance:

```sh
aws ec2 replace-route \
  --route-table-id <rtb-id> \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id <instance-id>
```

Also check the stable EIP association if `stable_egress_ip = true`:

```sh
aws ec2 describe-addresses \
  --allocation-ids <eipalloc-id>
```

See [ROLLBACK_GUIDE.md](ROLLBACK_GUIDE.md) for the full destroy and rollback procedure.

## Desired Capacity Guidance

### desired_capacity = 1

This has no warm standby.

Use only for disposable tests or workloads that can tolerate gateway downtime. Any replacement, stop, reboot, or EC2 failure can interrupt new connections until the instance is healthy again.

### desired_capacity = 2

This is the recommended alpha HA shape:

- one active,
- one standby,
- automatic failover for owner termination,
- ASG repair for failed instances.

It is still not a seamless software upgrade mechanism in the first alpha.

### desired_capacity >= 3

This can provide extra standby capacity and more flexibility for manual operations, but the first alpha provider still does not orchestrate standby-first rolling upgrades.

Use it when you want more spare capacity, not because it turns alpha replacement into a fully managed upgrade.

## What Terraform Does Not Do Yet

The first alpha does not yet:

- replace standby nodes before active nodes,
- trigger planned failover,
- ask the active node to step down,
- run ASG instance refresh safely around BetterNAT ownership,
- protect the active owner from ASG scale-in,
- expose an `upgrade_strategy` field,
- provide a user-triggered planned drain command.

The agent releases its locally owned HA lease on graceful SIGTERM/systemd stop. It also watches IMDS for Spot interruption and ASG target termination state, then completes the ASG termination lifecycle hook after release. These paths help a standby take over without waiting for the full lease TTL when AWS provides notice in time, but they are not yet a complete planned failover or rolling-upgrade workflow. Software and AMI changes are still modeled as explicit replacement, not as a transparent rolling upgrade.

## Future Production Direction

A production-grade rolling upgrade should eventually:

1. create a new launch template or AMI version,
2. replace standby nodes first,
3. verify new standby readiness,
4. trigger planned failover to a ready new node,
5. verify lease, route, EIP, metrics, and egress probe,
6. replace the old active after it becomes standby,
7. abort and keep the old active if readiness fails.

This is the target design, not a first-alpha feature.

## Practical Recommendation

For the first alpha:

- use in-place updates only for `min_size`, `desired_capacity`, and `max_size`,
- use `terraform apply -replace=...` only for disposable or accepted-disruption environments,
- use blue/green replacement for important route tables,
- keep rollback metadata and manual route rollback commands ready,
- do not remove the old gateway until the new gateway has passed egress, HA, datapath, and observability checks.
