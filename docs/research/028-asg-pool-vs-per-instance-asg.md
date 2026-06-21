# ASG Pool vs Per-Instance ASG

Date: 2026-06-20

## Summary

BetterNAT should use one Auto Scaling Group per availability zone, with multiple homogeneous NAT appliance instances inside that ASG.

It should not use one ASG per appliance instance as the default production model.

Recommended default:

```text
one AZ = one BetterNAT HA group = one ASG pool

min_size         = 1
desired_capacity = 2
max_size         = 3
```

This gives:

- one current owner,
- at least one warm candidate in the standard HA profile,
- ASG-backed capacity repair after instance loss,
- flexible scale-out to more standby capacity,
- flexible scale-in to low-cost single-node mode.

## Decision

Use this model:

```text
VPC private route table
  0.0.0.0/0 -> current owner instance

public subnet in us-west-2a
  ASG betternat-prod-us-west-2a
    instance i-a
      betternat-agent
      LoxiLB

    instance i-b
      betternat-agent
      LoxiLB

    optional instance i-c ...

DynamoDB lease
  ha_group_id         = prod-us-west-2a
  owner_instance_id   = i-a
  generation          = 42
  expires_at          = timestamp
```

All instances are equivalent. There is no provisioned active node or provisioned standby node. "Owner" is runtime state decided by the agent lease.

## Why Not One Instance Per ASG

One ASG per instance looks simple because it maps to a fixed active/standby mental model:

```text
ASG active  -> desired=1
ASG standby -> desired=1
```

That model is not the best fit for BetterNAT.

### 1. It Reintroduces Fixed Roles

With one ASG per instance, the system tends to encode active and standby as infrastructure roles.

BetterNAT wants the opposite:

- any healthy node can become owner,
- the owner can change after failure, restart, scale-in, or spot interruption,
- Terraform should not care which EC2 instance is active.

Runtime ownership belongs in `betternat-agent`, not in Terraform topology.

### 2. It Makes Scaling Awkward

If a user wants three appliances in one AZ, one-ASG-per-instance becomes:

```text
ASG node-0
ASG node-1
ASG node-2
```

Scaling from 2 to 3 is no longer a simple capacity change. The provider has to create another ASG, decide its identity, tag it, include it in discovery, and handle later deletion.

With one ASG pool:

```hcl
desired_capacity = 3
max_size         = 5
```

The product UX is closer to how users already think about capacity.

### 3. It Adds More AWS Objects Without Better Failover

Per AZ, one-ASG-per-instance doubles or triples:

- ASG objects,
- launch template references,
- lifecycle policies,
- CloudWatch/health signals,
- Terraform state objects,
- destroy ordering,
- drift surfaces.

It does not make failover faster. Failover speed is controlled by:

- agent health detection,
- DynamoDB lease expiry/fencing,
- `AssociateAddress` when stable egress IP is enabled,
- `ReplaceRoute`,
- local datapath readiness.

The ASG is the slow repair loop, not the fast failover loop.

### 4. It Makes Scale-In Semantics Harder

In a pool, scale-in means "reduce capacity in this AZ."

In per-instance ASGs, scale-in means deciding which ASG identity to remove. If that ASG happens to contain the current owner, the provider may accidentally act like an HA controller.

BetterNAT should avoid that. Terraform changes desired capacity; agents handle ownership.

### 5. It Is Less Portable

Other clouds expose managed instance groups, scale sets, or node pools. The portable abstraction is:

```text
one HA group has N interchangeable appliance nodes
```

It is not:

```text
one HA group has N one-node autoscaling groups
```

The pool abstraction maps better to AWS ASG, GCP Managed Instance Group, Azure Virtual Machine Scale Set, and Alibaba Cloud Scaling Group.

## Agent Model For A Multi-Instance ASG

Every node runs the same `betternat-agent` config.

At boot, each agent should:

1. Discover local identity from IMDS:
   - instance ID,
   - AZ,
   - VPC/subnet,
   - primary ENI.
2. Disable source/destination check for its own instance.
3. Start or validate the local datapath engine.
4. Join the HA group identified by config/tags.
5. Compete for the DynamoDB lease.

The owner agent:

- renews the lease,
- associates the shared EIP when stable-IP mode is enabled,
- replaces private route table targets,
- verifies route and EIP convergence,
- exports owner metrics.

Non-owner agents:

- keep datapath ready,
- keep source/destination check disabled,
- watch lease expiry,
- attempt takeover only when the lease is expired or owner is proven unhealthy,
- export candidate metrics.

## Lease Rules

The lease is the fencing primitive. It should include:

```text
ha_group_id
owner_instance_id
owner_private_ip
generation
expires_at
updated_at
```

Acquire:

- allowed when lease is absent,
- allowed when lease is expired,
- uses a conditional write.

Renew:

- allowed only when `owner_instance_id` and `generation` still match,
- extends `expires_at`,
- does not change owner.

Transfer:

- new owner increments `generation`,
- new owner performs AWS mutations only after acquiring the lease,
- old owner must stop owner-only work when renew fails.

This lets a single ASG contain any number of candidates without split-brain ownership.

## Scaling Semantics

### desired=1

Cheapest mode.

```text
one node
node is owner if healthy
no warm candidate
ASG replaces failed capacity after failure
```

This mode is not highly available, but it is a valid low-cost NAT replacement.

### desired=2

Default HA mode.

```text
one owner
one warm candidate
```

If owner fails:

1. candidate sees lease expire,
2. candidate acquires lease,
3. candidate moves EIP if needed,
4. candidate replaces route,
5. ASG later launches a replacement for the failed node.

### desired=3+

Higher resilience mode.

```text
one owner
multiple warm candidates
```

The first candidate to acquire the conditional lease becomes owner. Others remain candidates.

This mode helps when:

- users use Spot,
- node startup occasionally fails,
- scale-in or maintenance events overlap with failures,
- the AZ carries very high traffic and operators want spare capacity.

### Scale-Out

Terraform/provider updates ASG desired capacity.

New instances:

- boot with the same launch template,
- join the same HA group,
- remain candidates unless they win the lease.

Existing owner should not be disturbed.

### Scale-In

ASG may terminate any instance unless protected by lifecycle rules.

Minimum viable behavior:

- if ASG terminates a candidate, no failover occurs,
- if ASG terminates the owner, another candidate takes over after lease expiry.

Better production behavior:

- enable ASG lifecycle hooks,
- have terminating owner voluntarily release or shorten the lease,
- optionally use instance protection for the current owner,
- still treat lease expiry as the final recovery mechanism.

Instance protection is useful, but it should be an optimization. Correctness should not depend on it.

## Terraform Provider UX

The provider should expose pool capacity, not fixed instance slots.

Good UX:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"
  vpc_id = aws_vpc.main.id

  per_az = {
    "us-west-2a" = {
      public_subnet_id = aws_subnet.public_a.id
      private_route_table_ids = [aws_route_table.private_a.id]
    }
  }

  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  stable_egress_ip = true
  datapath_engine  = "loxilb"
}
```

Provider-owned stable resources:

- launch template,
- one ASG per AZ,
- IAM instance profile,
- security group,
- DynamoDB lease table,
- optional EIP per AZ,
- tags for ownership and discovery,
- route rollback metadata.

Provider read should avoid relying on old instance IDs. It should discover runtime state from:

- ASG names and tags,
- ASG instance membership,
- route table targets,
- EIP association,
- DynamoDB lease row when available.

## Product Positioning

The product can present this as:

```text
Self-healing NAT appliance pools
```

or:

```text
One active egress owner, warm capacity behind it, automatic replacement after failure.
```

The headline should not explain ASG internals. ASG is an implementation detail. The user-facing promise is:

- lower NAT processing cost,
- clearer traffic visibility,
- fast failover,
- automatic capacity repair,
- Terraform-native install.

## Test Requirements

Minimum local tests:

- install plan creates one pool per AZ,
- default capacity is `1/2/3`,
- invalid capacity fails validation,
- Terraform schema accepts omitted capacity and explicit capacity,
- provider state records ASG and launch template names.

Minimum LocalStack tests, if service coverage permits:

- create launch template,
- create ASG,
- discover ASG instance membership,
- run initial route replacement,
- destroy ASG before deleting dependent resources.

Minimum AWS tests:

| ID | Scenario | Expected Result |
|----|----------|-----------------|
| ASG-Pool-001 | Apply with desired=2 | One ASG and two target-capacity nodes per AZ |
| ASG-Pool-002 | Terminate candidate | ASG replaces it; owner remains stable |
| ASG-Pool-003 | Terminate owner | Candidate takes lease and route/EIP moves |
| ASG-Pool-004 | Scale desired 2 -> 3 | New node joins as candidate; owner unchanged |
| ASG-Pool-005 | Scale desired 3 -> 1 | Gateway remains functional if final node is healthy |
| ASG-Pool-006 | Destroy | ASG stops replacing nodes; route rollback and cleanup complete |

## Open Implementation Gaps

- Agent self-disable of source/destination check at boot is required so warm candidates are ready before takeover.
- Provider `Read` should discover current ASG membership and owner state instead of preserving create-time instance IDs.
- Lifecycle hook behavior should be added after the basic pool model works.
- Stable-IP failover timing should be measured under ASG after AMI/bootstrap is reliable.

## Conclusion

One ASG per AZ with multiple interchangeable BetterNAT appliances is the right production shape.

It keeps Terraform focused on infrastructure capacity, lets `betternat-agent` own runtime HA, supports scale-out and scale-in cleanly, and maps well to other clouds later.

One-ASG-per-instance should remain only a possible debugging shape, not the product architecture.
