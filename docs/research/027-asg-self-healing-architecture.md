# ASG Self-Healing Architecture

Date: 2026-06-20

## Summary

BetterNAT should evolve from directly managed EC2 appliance instances to an Auto Scaling Group based appliance pool.

Recommended production shape:

- one ASG per AZ,
- N homogeneous BetterNAT appliances per ASG,
- `desired_capacity = 2` by default,
- `min_size = 1` for degraded low-cost operation,
- `max_size >= 3` for users who want extra standby capacity,
- `betternat-agent` elects the current owner through DynamoDB lease/fencing.

Direct EC2 remains useful for AWS primitive tests and early development, but it should not be the final production HA model.

For the narrower decision on why BetterNAT should use one ASG pool per AZ instead of one ASG per appliance instance, see `028-asg-pool-vs-per-instance-asg.md`.

## Why Direct EC2 Is Not Enough

The current provider path can create two EC2 instances directly. This is good for validating:

- route replacement,
- EIP association,
- source/destination check disablement,
- LoxiLB bootstrap,
- provider apply/destroy behavior,
- low-cost AWS tests before AMI packaging.

It is not enough for a complete service. If active fails, standby can take over, but the system remains degraded. If the standby then fails before replacement, the gateway is down.

A complete BetterNAT service needs two loops:

1. Fast failover loop:
   - owned by `betternat-agent`,
   - target: seconds,
   - uses lease/fencing, `AssociateAddress`, and `ReplaceRoute`.

2. Capacity repair loop:
   - owned by ASG,
   - target: tens of seconds to minutes,
   - replaces failed or interrupted instances.

## Recommended Shape

Per AZ:

```text
us-west-2a
  ASG betternat-prod-us-west-2a
    min     = 1
    desired = 2
    max     = 5

    instance i-a
      betternat-agent
      LoxiLB

    instance i-b
      betternat-agent
      LoxiLB

    optional instance i-c ...
```

Ownership is dynamic:

```text
DynamoDB lease item:
  ha_group_id = prod-us-west-2a
  owner_instance_id = i-a
  generation = 42
  expires_at = <timestamp>
```

The current lease owner is active. Other healthy nodes are warm candidates.

Stable-IP mode:

```text
owner -> AssociateAddress(shared EIP -> owner)
owner -> ReplaceRoute(private default route -> owner)
```

Route-only mode:

```text
owner -> ReplaceRoute(private default route -> owner)
```

## Terraform UX

The provider should expose capacity as a per-AZ pool, not as fixed active/standby instance IDs.

Example target shape:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"

  vpc_id = aws_vpc.main.id

  per_az = {
    "us-west-2a" = {
      public_subnet_id = aws_subnet.public_a.id
      private_route_table_ids = [
        aws_route_table.private_a.id,
      ]

      min_size         = 1
      desired_capacity = 2
      max_size         = 5
    }
  }

  stable_egress_ip = true
  datapath_engine  = "loxilb"
}
```

Good defaults:

```text
min_size = 1
desired_capacity = 2
max_size = 3
```

Product modes:

| Mode | Capacity | Behavior |
|------|----------|----------|
| Cheapest | desired=1, min=1 | No standby; lower cost |
| Standard HA | desired=2, min=1 | One owner plus one warm candidate |
| Higher resilience | desired=3+ | One owner plus multiple candidates |

## Provider Responsibilities

In the ASG architecture, Terraform/provider owns stable infrastructure:

- launch template,
- one ASG per AZ,
- IAM instance profile,
- security group,
- DynamoDB lease table,
- stable EIP per AZ when enabled,
- tags used for discovery and cleanup,
- rollback metadata,
- destroy ordering.

The provider should not rely on stable EC2 instance IDs after creation. ASG instances are replaceable.

Provider `Read` should discover current runtime state by:

- ASG names,
- EC2 tags,
- EIP association,
- route table targets,
- DynamoDB lease status when available.

Destroy ordering:

1. Disable or delete ASGs first so they stop replacing instances.
2. Restore private route targets if rollback is enabled.
3. Release EIPs.
4. Delete DynamoDB lease table.
5. Delete IAM/security group/launch template.

## Agent Responsibilities

Every ASG node runs the same `betternat-agent` binary and config. There should be no fixed active/standby role baked into the instance.

The agent must:

- discover instance identity and AZ from IMDS,
- know gateway name and HA group from config/tags,
- reconcile local datapath rules on boot,
- compete for or renew the DynamoDB lease,
- execute owner-only AWS operations only while it holds the lease,
- step down when it loses the lease,
- verify EIP and route convergence after failover,
- expose health and role metrics.

Owner is runtime state, not provisioning state.

## Lease And Fencing

The lease item should include:

```text
ha_group_id
owner_instance_id
owner_private_ip
generation
expires_at
updated_at
```

Acquire/renew should use conditional writes:

- acquire when lease is absent or expired,
- renew only when current owner and generation match,
- increment generation on ownership transfer.

Before owner-only AWS operations, the agent should confirm it still owns the latest generation. After AWS operations, it should verify route/EIP state.

## Scale-In And Spot Behavior

Minimum viable scale-in behavior:

- if ASG terminates the current owner, other nodes notice lease expiry and take over,
- new flows recover after route/EIP failover,
- old long-lived flows are not guaranteed to survive.

Better later behavior:

- ASG lifecycle hook on terminating instances,
- owner attempts graceful lease release,
- termination policy or external controller avoids selecting the current owner when possible.

Spot should be opt-in for production:

- ASG replaces interrupted instances,
- agent should watch IMDS interruption notice if possible,
- owner should stop renewing or release lease on interruption notice,
- standby candidates should be ready to take over.

## AMI Interaction

ASG becomes much more valuable after BetterNAT has a published AMI.

Target production path:

```text
Packer -> BetterNAT AMI -> Launch Template -> ASG -> agent lease ownership
```

Cloud-init binary download is acceptable for development tests, but production should prefer AMI-baked binaries for faster boot and reproducibility.

AMI readiness should measure:

- launch to agent active,
- launch to LoxiLB rule present,
- active termination to route/EIP convergence,
- ASG replacement to pool healthy.

## Observability

Useful pool metrics:

```text
betternat_node_role{gateway,az,instance_id} 0|1
betternat_lease_generation{gateway,az}
betternat_lease_seconds_until_expiry{gateway,az}
betternat_pool_desired_capacity{gateway,az}
betternat_pool_healthy_nodes{gateway,az}
betternat_pool_ready_candidates{gateway,az}
betternat_failover_total{gateway,az,mode}
betternat_failover_duration_seconds{gateway,az,mode}
betternat_route_target_match{gateway,az}
betternat_eip_owner_match{gateway,az}
```

Provider/read outputs should summarize:

- current owner instance,
- current EIP owner,
- current route target,
- ASG desired/running capacity,
- degraded status.

## Migration Plan

1. Direct EC2 validation:
   - keep current path for primitive and failover timing tests.

2. ASG provider skeleton:
   - create launch template and one ASG per AZ,
   - preserve current cloud-init path until AMI exists,
   - provider read discovers ASG instances.

3. Agent dynamic ownership:
   - remove fixed active/standby assumptions,
   - use DynamoDB lease to elect owner from pool.

4. Self-healing tests:
   - terminate standby and observe ASG replacement,
   - terminate active and observe agent failover plus ASG replacement,
   - scale desired capacity from 1 to 3 and back,
   - verify cleanup after destroy.

5. AMI release:
   - replace cloud-init binary download with prebuilt AMI,
   - keep user-data as config/bootstrap only.

## Test Plan Additions

| ID | Test | Expected Result |
|----|------|-----------------|
| ASG-001 | Create one ASG per AZ with desired=2 | Two nodes become ready; one owns lease |
| ASG-002 | Terminate non-owner | ASG replaces it; owner remains stable |
| ASG-003 | Terminate owner | Another node acquires lease and route/EIP moves |
| ASG-004 | Scale out desired 2 -> 3 | New node joins without disrupting owner |
| ASG-005 | Scale in desired 3 -> 2 | Pool remains healthy; owner loss, if selected, recovers |
| ASG-006 | Stable-IP failover under ASG | Public egress IP remains unchanged for new flows |
| ASG-007 | Route-only failover under ASG | Route target changes; public egress IP may change |
| ASG-008 | Destroy | ASG stops replacing nodes; route rollback and cleanup complete |

## Decision

For production BetterNAT, prefer one ASG per AZ with dynamic agent-owned lease election.

Keep direct EC2 creation only as:

- early development path,
- low-level AWS primitive test path,
- fallback for users who bring their own lifecycle manager.

The product contract should eventually describe BetterNAT as an appliance pool, not as two fixed instances.
