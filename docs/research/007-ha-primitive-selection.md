# HA Primitive Selection

Date: 2026-06-19

## Question

For BetterNAT, what should be the default high-availability mechanism?

Candidates:

1. Replace private route table target.
2. Reassociate EIP.
3. Move secondary ENI.
4. Reassign secondary private IP.
5. Keepalived/VRRP-style virtual IP.

## Short Answer

Default to route-table failover with DynamoDB lease/fencing.

Recommended v0/v1 HA:

```text
Per AZ:
  active NAT appliance
  standby NAT appliance
  private route table default route -> active NAT instance or active NAT ENI
  public EIP per active appliance, optional
  DynamoDB lease controls ownership
```

Use `ReplaceRoute` as the first failover action. Treat EIP reassociation and secondary ENI/IP movement as optional advanced modes.

Do not use Keepalived/VRRP as the core AWS HA primitive.

## Why HA Is Central To The Product

Without HA, this project is just a cheaper NAT instance. With safe HA, it becomes a credible NAT Gateway alternative for high-volume workloads.

The HA contract should be:

> BetterNAT automatically fails private-subnet egress over to a healthy appliance using AWS-native control-plane primitives. Existing connections may reset; new connections recover after health detection and AWS control-plane convergence.

Do not promise seamless connection preservation in v0/v1.

## Option A: Route Table Failover

### How it works

Private subnet route tables contain:

```text
0.0.0.0/0 -> active NAT instance or active NAT ENI
```

On failover, standby calls EC2 `ReplaceRoute` to point the default route to itself or its ENI.

### Why it fits

This is the most natural AWS NAT primitive:

- Private subnet egress is controlled by route tables.
- AWS NAT instance documentation already uses route tables pointing to the NAT instance.
- Terraform supports routes and EC2 supports `ReplaceRoute`.
- It does not require L2/VIP behavior.
- Works with one EIP per NAT appliance or with EIP reassociation as an additional step.

### Pros

- Simple conceptual model.
- Good Terraform fit.
- Easy rollback to NAT Gateway by restoring the old route target.
- Can be done per AZ to avoid cross-AZ NAT paths.
- Does not require moving interfaces.

### Cons

- Existing connections usually reset.
- Each route table must be updated correctly.
- Route update convergence is an AWS control-plane behavior, not a hard realtime path.
- If many route tables are managed, takeover logic must handle partial success.

### Best default shape

Route target should preferably be an ENI rather than an instance ID if AWS and Terraform topology support it cleanly, because ENI is closer to the network identity. However, route-to-instance is simpler and matches classic NAT instance docs.

Implementation should support both if feasible:

```yaml
ha:
  mode: route
  route_target: eni # or instance
```

### Verdict

Best default for v0/v1.

## Option B: EIP Reassociation

### How it works

Active appliance owns an Elastic IP. On failover, standby calls `AssociateAddress` with reassociation allowed to bind the EIP to itself.

### Why it is useful

EIP failover preserves a stable public egress IP. That matters when:

- Third-party vendors whitelist a single public IP.
- External systems have firewall allowlists.
- The product promises stable outbound identity.

### Critical limitation

EIP reassociation alone does not necessarily move private subnet egress to the standby.

Private route tables still need to send traffic to the active NAT appliance. If routes point to the failed instance, moving the EIP does not help private instances reach the standby.

So EIP failover is usually an add-on to route failover:

```text
failover = acquire lease
        -> ReplaceRoute to standby
        -> optionally AssociateAddress EIP to standby
```

### Pros

- Stable public IP.
- Good for allowlisted outbound traffic.
- AWS supports reassociation semantics.

### Cons

- Does not solve route target by itself.
- Existing connections reset.
- API propagation time is not a hard 2-second guarantee.
- Requires careful IAM scoping to avoid taking wrong EIPs.

### Verdict

Support as an optional mode/step, not the default standalone HA primitive.

## Option C: Secondary ENI Movement

### How it works

A secondary ENI represents the NAT network identity. On failover, it is detached from the failed active instance and attached to the standby.

Private route tables can point to that ENI, and the EIP can be associated with it or its private IP pattern.

### Why it is attractive

AWS documentation describes moving a network interface and/or secondary private IPv4 address to a standby instance as a low-budget failover pattern.

Potential benefits:

- Stable route target.
- Cleaner network identity.
- May reduce the number of route table updates.
- EIP and private route identity can be tied to the same moving network unit.

### Why it is not the default

The ENI movement model is attractive, but it changes failover from one cloud route mutation into a multi-step device migration:

```text
detect failure
acquire lease
detach ENI from old active, if still attached
wait for ENI state to become available
attach ENI to standby
wait for OS to see the interface
configure addresses/routes/sysctls/firewall expectations
verify public/private datapath
optionally update or verify EIP association
declare active
```

Each step creates a new partial-failure state. Route failover has fewer moving parts:

```text
detect failure
acquire lease
ReplaceRoute to standby target
verify new route and datapath
declare active
```

The ENI model may still be better after hardening, but it is a larger first implementation.

### Hard parts

- Primary ENI cannot be moved.
- Secondary ENI attach/detach has state transitions and timing.
- The standby OS must configure the attached interface correctly.
- Stale active may still think it owns the interface until AWS detach/fencing completes.
- More complex than route replacement.
- Force-detaching from a degraded instance can have different timing and cleanup behavior than a planned detach.
- Hotplug handling must be reliable across distros, systemd-networkd/NetworkManager/cloud-init, and kernel versions.
- The agent must distinguish "ENI attached in AWS" from "datapath is actually ready inside Linux."
- If the ENI carries the public identity, EIP association and local SNAT source behavior must be verified after attach.
- If the ENI carries the private route identity, route tables may remain stable but failover now depends on ENI attach convergence.

### Pros

- Potentially cleaner failover identity.
- May preserve route table config.
- Good advanced mode for users who want stable ENI target.

### Cons

- Higher implementation and testing complexity.
- More failure states.
- Requires robust hotplug/systemd/network configuration.

### Verdict

Research and support later as an advanced HA mode. Do not block v0/v1 on it.

This is not a rejection of ENI failover. It is a sequencing decision:

- Route failover is the fastest path to a correct, testable MVP.
- ENI failover is a likely advanced mode after the state machine, lease/fencing, probes, and rollback path are proven.

## Option D: Secondary Private IP Reassignment

### How it works

Use a secondary private IPv4 address as the stable NAT identity. On failover, unassign/reassign that private IP from active to standby. EIP can be associated to that private IP.

### Why it is attractive

Moving a secondary private IP can be lighter than moving a whole ENI, and AWS documentation mentions secondary private IPv4 failover patterns.

### Hard parts

- Route table targets are not arbitrary private IPs; routes target instance, ENI, NAT Gateway, etc.
- Reassigning private IP helps with EIP identity but may not by itself fix private subnet route target.
- The OS must bring up the secondary IP correctly.

### Verdict

Useful as part of an EIP identity strategy, but not enough as the main private-route failover primitive.

## Option E: Keepalived / VRRP

### How it works in traditional networks

Keepalived/VRRP elects an active node for a virtual IP. Failover is commonly communicated with ARP or L2 mechanisms.

### Why it is weak on AWS

AWS VPC is not a normal L2 domain where gratuitous ARP/VRRP owns the whole failover story. The actual private-subnet egress path is controlled by AWS route tables and ENI/EIP association state.

Keepalived can still inspire:

- Health checks.
- State machine shape.
- Priority/preemption behavior.

But it should not be the core mechanism.

### Verdict

Do not use as the core AWS HA primitive.

## Split-brain Prevention

This is mandatory.

Heartbeats alone are not enough. If active and standby lose connectivity to each other but both can call AWS APIs, they can both believe they should be active.

Use a lease/fencing model.

### Recommended lease store: DynamoDB

Use a DynamoDB table with conditional writes:

```text
pk: az_or_group_id
owner_instance_id
generation
lease_expires_at
last_heartbeat_at
route_table_ids
active_target_id
```

Acquire rule:

- Take lease only if no lease exists, lease expired, or caller already owns it.
- Increment generation on takeover.
- Write with condition expression.

Act rule:

- Only perform AWS route/EIP/ENI changes after acquiring lease.
- Include generation/fencing token in local active state.

Demote rule:

- If a node sees that it no longer owns the current generation, it must stop acting as active.

### Why DynamoDB

- Conditional writes are a clean compare-and-set primitive.
- TTL can clean stale records, though TTL itself should not be relied on for precise failover timing.
- Operationally familiar in AWS.
- Easy to scope IAM.

### Alternatives

- SSM Parameter Store with version checks: possible but less purpose-built for leader election.
- EC2 tags: too weak as the primary lock.
- Local heartbeat only: insufficient.

## Suggested HA State Machine

States:

```text
INIT
STANDBY
ACTIVE
TAKING_OVER
DEMOTING
ERROR
```

Loop:

1. `INIT`: load config, inspect instance identity, verify IAM, verify routes.
2. `STANDBY`: monitor active health and lease.
3. `TAKING_OVER`: acquire DynamoDB lease, then update route/EIP/ENI.
4. `ACTIVE`: renew lease, serve NAT, emit metrics.
5. `DEMOTING`: stop renewing lease, remove active marker, optionally stop NAT.
6. `ERROR`: fail closed or keep forwarding depending on configured policy.

## Health Checks

Use multiple signals:

- Peer heartbeat over private IP.
- Local datapath health: forwarding enabled, nft rules loaded, interface up.
- Outbound probe from NAT node.
- Optional private probe instance route test.
- AWS state verification: route target, EIP association, lease owner.

Avoid taking over on a single missed packet.

Suggested initial thresholds:

- Heartbeat interval: 1s.
- Suspect after: 3 missed heartbeats.
- Takeover after: lease expired and independent AWS state check passes.

These are starting points, not published SLOs.

## Failover SLO

Do not promise 2-5 seconds until measured.

Failover time includes:

```text
detection time
+ lease acquisition time
+ AWS API call time
+ route/EIP/ENI control-plane convergence
+ client retry/application recovery
```

The product can publish:

- Detection time.
- API operation latency.
- Time until a new probe connection succeeds.
- Whether existing connections survived.

The useful external SLO is:

> New outbound connections recover within measured p95/p99 failover time for a given deployment profile.

## Per-AZ Topology

Default to per-AZ HA groups.

```text
us-west-2a:
  private subnets in 2a -> route table 2a -> NAT HA group 2a

us-west-2b:
  private subnets in 2b -> route table 2b -> NAT HA group 2b
```

Reasons:

- Avoid cross-AZ NAT hairpin cost.
- Keep failure domains contained.
- Match AWS NAT Gateway best practice of one NAT Gateway per AZ for resiliency.

Centralized NAT can be supported for cost-minimal deployments, but should warn about:

- Cross-AZ data charges.
- Larger blast radius.
- More route tables per failover group.

## Default v0 Decision

Use route-table failover with DynamoDB lease/fencing:

```text
mode: route
topology: per_az_active_standby
route_target: instance_or_eni
lease: dynamodb
eip: optional_per_node_or_reassociate
```

Implementation sequence:

1. Terraform creates active/standby nodes and private routes.
2. Agent starts in standby/active based on route ownership and lease.
3. Active renews lease.
4. Standby monitors health.
5. Standby acquires expired lease.
6. Standby calls `ReplaceRoute`.
7. Standby optionally reassociates EIP.
8. Standby verifies new route target.
9. Standby becomes active.
10. Old active demotes if it recovers and no longer owns lease.

## Open Research

- Whether route-to-ENI or route-to-instance should be the default in Terraform.
- Measured `ReplaceRoute` convergence time.
- Measured EIP reassociation time.
- Measured ENI attach/detach time.
- Whether conntrackd state sync is worth productizing.
- Behavior during AWS API throttling or partial route update failure.
- Whether planned maintenance can drain by temporarily reducing route weight is not available with normal route tables; likely requires scheduled failover and application retry.

## Sources

- AWS NAT instance docs, including routing private subnet traffic through a NAT instance: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html
- AWS work with NAT instances, including disabling source/destination checks and updating route tables: https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html
- EC2 `ReplaceRoute` API: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_ReplaceRoute.html
- EC2 `AssociateAddress` API and reassociation behavior: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_AssociateAddress.html
- EC2 ENI failover scenarios, including moving a network interface or secondary private IPv4 address to standby: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/scenarios-enis.html
- AWS NAT Gateway per-AZ resiliency recommendation: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-scenarios.html
- DynamoDB condition expressions for conditional writes: https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Expressions.ConditionExpressions.html
- DynamoDB TTL behavior: https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TTL.html
- Amazon Builders' Library on leader election and lease pitfalls: https://aws.amazon.com/builders-library/leader-election-in-distributed-systems/
