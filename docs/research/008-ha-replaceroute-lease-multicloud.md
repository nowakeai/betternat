# HA Deep Dive: ReplaceRoute, Lease/Fencing, and Multi-cloud Compatibility

Date: 2026-06-19

## Questions

1. What does AWS `ReplaceRoute` actually do?
2. Is DynamoDB lease/fencing really necessary?
3. Can this HA design work later on GCP, Azure, or other clouds?

## Short Answer

`ReplaceRoute` is an AWS control-plane API that changes the target of an existing route in a VPC route table. It does not move packets itself, run a routing protocol, or preserve connections. After the route target is changed, the VPC's distributed routing system sends new matching traffic to the new target.

DynamoDB lease/fencing is necessary if NAT nodes self-elect and both active/standby agents are allowed to call cloud APIs. Without a lease, heartbeat-only failover can create split-brain.

The design can be multi-cloud compatible if we abstract it as:

```text
CloudRouteController:
  get current next hop
  replace route next hop
  verify route convergence

LeaseBackend:
  acquire lease
  renew lease
  release/demote
  fencing generation
```

AWS, GCP, and Azure can all fit the broad "route traffic to a network appliance" model, but the implementation details differ.

## 1. What ReplaceRoute Does In AWS

### AWS VPC route table model

AWS VPC route tables contain route rules:

```text
destination CIDR -> target
```

For private subnet internet egress through a NAT instance, the route is typically:

```text
0.0.0.0/0 -> i-xxxxxxxx
```

or:

```text
0.0.0.0/0 -> eni-xxxxxxxx
```

AWS documentation describes a route table as the traffic controller for a VPC. Each subnet is associated with a route table, and the route table rules determine where subnet traffic is directed.

### ReplaceRoute API semantics

`ReplaceRoute` replaces an existing route within a route table.

Inputs include:

- route table ID,
- destination CIDR block or prefix list ID,
- exactly one new target type, such as instance, network interface, NAT gateway, transit gateway, etc.

For BetterNAT failover, the important call is conceptually:

```sh
aws ec2 replace-route \
  --route-table-id rtb-private-az-a \
  --destination-cidr-block 0.0.0.0/0 \
  --network-interface-id eni-standby
```

or:

```sh
aws ec2 replace-route \
  --route-table-id rtb-private-az-a \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id i-standby
```

### What happens after the API call

Conceptually:

```text
before:
  private subnet route table:
    0.0.0.0/0 -> active NAT target

standby detects failure
standby acquires lease
standby calls ReplaceRoute

after:
  private subnet route table:
    0.0.0.0/0 -> standby NAT target
```

Then new packets from instances associated with that route table match `0.0.0.0/0` and are delivered by the VPC dataplane to the new target.

### What ReplaceRoute is not

It is not:

- BGP.
- VRRP.
- ARP.
- A Linux route change inside the NAT instance.
- A guarantee that existing TCP connections survive.
- A hard realtime dataplane operation with a universal 2-second SLO.

It is a cloud control-plane update to the VPC routing config.

## Ingress Route vs Egress Public Identity

`ReplaceRoute` solves the private-side routing problem:

```text
private subnet -> which NAT appliance should receive internet-bound packets?
```

It does not, by itself, solve the public-side identity problem:

```text
internet sees traffic from which public IP?
```

For BetterNAT, these are two separate failover concerns:

```text
Private next hop:
  private route table default route -> active NAT appliance

Public egress identity:
  SNAT source -> active appliance public IP / EIP
```

If the product must guarantee the same public egress IP after failover, the HA action needs an EIP/public IP step in addition to route failover.

## Keeping The Same Egress IP After Failover On AWS

AWS supports this with Elastic IPs.

There are three practical modes.

### Mode 1: Per-node EIP, no stable shared egress IP

Each NAT appliance has its own EIP:

```text
active A -> EIP-A
standby B -> EIP-B
```

Failover only changes the private route:

```text
0.0.0.0/0 -> B
```

After failover, new outbound traffic uses `EIP-B`.

Pros:

- Fastest/simple route-only failover.
- No public IP reassociation step.
- Fewer AWS API actions during incident.

Cons:

- Public egress IP changes.
- Bad fit for vendor IP allowlists.

Use when stable public egress IP is not required.

### Mode 2: Shared EIP reassociation

One EIP represents the HA group public identity:

```text
before:
  route -> A
  EIP-X -> A

after:
  route -> B
  EIP-X -> B
```

Failover sequence:

```text
acquire lease
ReplaceRoute private route target to B
AssociateAddress EIP-X to B with reassociation
verify route target
verify EIP association
probe outbound source IP == EIP-X
declare active
```

Pros:

- New outbound connections use the same public IP after failover.
- Works well for third-party allowlists.
- Conceptually simple.

Cons:

- More API operations than route-only failover.
- Existing TCP connections usually reset anyway.
- During failover there may be a short interval where route and EIP state are not aligned.
- Must ensure local SNAT uses the EIP-facing address/interface correctly.

This is likely the default mode for users who care about stable outbound IP.

### Mode 3: Secondary private IP movement with attached EIP

AWS allows an EIP to be associated with a private IPv4 address on an ENI. AWS CLI docs also note that when a secondary private IP address is moved to another network interface, an EIP associated with that private IP moves with it.

Conceptually:

```text
stable secondary private IP: 10.0.1.50
stable public EIP: EIP-X -> 10.0.1.50

failover:
  move 10.0.1.50 from A's ENI to B's ENI
  EIP-X follows the private IP
```

Pros:

- Clean public identity model.
- EIP follows a stable private IP.
- Can reduce explicit EIP reassociation logic.

Cons:

- Still needs private route failover unless route target also follows the moved identity.
- Secondary IP reassignment is asynchronous.
- OS must observe and configure the moved IP.
- More cloud/OS state to verify.

This is an advanced mode, not the simplest default.

## Can We Guarantee Same Egress IP?

For AWS new connections: yes, if failover includes EIP reassociation or secondary private IP/EIP movement and verification confirms the EIP is active on the new NAT appliance.

For existing connections: no practical v0/v1 guarantee.

Reasons:

- TCP peers saw the old 5-tuple through the old appliance.
- NAT state may be gone or not synchronized.
- Route and EIP changes are control-plane operations with convergence time.
- Even if conntrack state is synchronized later, in-flight packets during failover can be lost.

The honest product contract is:

> Stable egress IP for new outbound connections after failover, when shared-EIP mode is enabled. Existing connections may reset.

## Suggested AWS HA Profiles

### Profile A: fastest/simple

```yaml
ha:
  private_failover: replace_route
  public_identity: per_node_eip
```

Use when:

- no third-party allowlist,
- fastest/simplest failover is preferred,
- source IP can change during failover.

### Profile B: stable egress IP

```yaml
ha:
  private_failover: replace_route
  public_identity: shared_eip_reassociation
```

Use when:

- users require one fixed outbound public IP per HA group,
- vendors/firewalls whitelist the NAT egress IP,
- slightly more failover complexity is acceptable.

### Profile C: advanced identity movement

```yaml
ha:
  private_failover: replace_route_or_stable_eni
  public_identity: secondary_private_ip_with_eip
```

Use when:

- AWS-specific advanced HA is acceptable,
- the team wants public identity tied to a movable private IP,
- hotplug/IP reassignment behavior has been tested.

## Failover Ordering For Shared EIP

There is no perfect ordering that preserves existing flows. For new-flow recovery, prefer an idempotent ordered plan:

```text
1. acquire lease
2. make standby datapath ready
3. associate shared EIP to standby
4. replace private route to standby
5. verify route table target
6. verify EIP association
7. run outbound probe and confirm source IP
8. mark active
```

Alternative ordering can replace the route before EIP. That may send a short burst of traffic out through the standby's old public IP if SNAT is already active. EIP-first usually better preserves the external identity for new flows, but both orderings must be benchmarked.

The agent should support a strict verification gate:

```text
do not report ACTIVE until:
  route target == me
  EIP association == me
  outbound probe source IP == expected EIP
  lease generation == current
```

### Why this works for NAT HA

The private instances do not need to know the NAT node changed. They still send traffic to their subnet default route. AWS VPC routing decides which appliance receives the traffic.

This is exactly the right layer for failover:

- private instances stay untouched,
- OS routes inside private instances stay untouched,
- route table target changes from failed NAT to healthy NAT.

### Important implementation detail

The agent must verify after the API call:

1. `DescribeRouteTables` shows the expected target.
2. New probe traffic succeeds.
3. The lease generation still belongs to this node.

The route-table API response alone is not enough to declare the failover healthy.

## Instance Target vs ENI Target

AWS supports routes to NAT instances and routes to network interfaces.

### Route to instance ID

```text
0.0.0.0/0 -> i-active
```

Pros:

- Simple.
- Matches classic NAT instance documentation.
- Terraform support is straightforward.

Cons:

- Less explicit about which interface is the appliance path.
- Multi-NIC appliances can be ambiguous operationally.
- The network identity is tied to the instance.

### Route to ENI ID

```text
0.0.0.0/0 -> eni-active
```

Pros:

- More explicit network target.
- Better mental model for appliance routing.
- Aligns better with future secondary ENI failover.

Cons:

- Slightly more complex Terraform and agent logic.
- Need to ensure the ENI is the correct NAT-facing interface.

### Current recommendation

Support both, but prefer ENI target for serious HA mode if testing confirms clean behavior:

```yaml
ha:
  mode: route
  route_target: eni
```

For the first proof of concept, route-to-instance is acceptable because it is simpler.

## Why Not Dynamic ENI Binding First?

Dynamic ENI binding can be a strong AWS-specific HA mode, but it is not the best default for the first version.

The main reason is operational complexity, not lack of technical merit.

Route replacement changes the VPC next hop:

```text
route target: old appliance -> new appliance
```

Dynamic ENI binding moves the network device identity:

```text
secondary ENI: old appliance -> detached -> new appliance
```

That introduces more states to test:

- old instance alive but unhealthy,
- old instance unreachable,
- ENI detach pending,
- forced detach,
- ENI available but not yet attached,
- ENI attached in AWS but not configured in Linux,
- Linux interface present but nftables/SNAT source not ready,
- EIP association or source address mismatch,
- stale old active recovering after losing the ENI.

It also makes the multi-cloud abstraction less clean. AWS ENIs, GCP NICs/alias IPs, and Azure NIC/IP configurations do not map as directly as "replace the private route next hop."

So the sequencing should be:

1. Prove route-based HA first.
2. Build the lease/fencing, health probes, rollback, and verification framework.
3. Add ENI movement as an AWS advanced mode if route failover has unacceptable convergence or operational drawbacks.

## 2. Is DynamoDB Lease/Fencing Necessary?

### If agents self-elect: yes

If each NAT node runs an agent and standby is allowed to initiate failover, a lease/fencing mechanism is necessary.

Heartbeat alone answers:

> Can I hear my peer?

It does not answer:

> Am I the only node allowed to mutate cloud routing right now?

That second question is the important one.

### Split-brain example

```text
t0:
  node A is active
  node B is standby

t1:
  network issue prevents A and B from talking to each other
  both can still call AWS APIs

t2:
  B misses heartbeats and decides A is dead
  B calls ReplaceRoute to B

t3:
  A is still alive and still believes it is active
  A renews local state or calls ReplaceRoute back to A

result:
  route target can flap
  observability and ownership state disagree
  failover is unsafe
```

A lease does not magically solve every distributed-system problem, but it creates one cloud-backed ownership record that both nodes must respect before acting.

### What fencing adds

A lease says:

```text
owner = node B
expires_at = T
```

Fencing adds a generation/token:

```text
owner = node B
generation = 42
expires_at = T
```

Every active action is tied to the current generation. If node A wakes up with old generation `41`, it must demote and stop acting as active.

### Why DynamoDB specifically

DynamoDB gives us:

- conditional writes,
- strongly consistent reads if requested,
- simple per-HA-group key/value record,
- TTL cleanup for stale records,
- AWS-native IAM scoping,
- low operational overhead.

The important feature is conditional write:

```text
Acquire if:
  attribute_not_exists(pk)
  OR lease_expires_at < now
  OR owner_instance_id = me
```

Then increment `generation`.

### Is TTL enough?

No.

DynamoDB TTL is useful for cleanup, but not precise failover timing. The agent should use the `lease_expires_at` field in conditional expressions and treat TTL as background cleanup only.

### When a lease might not be needed

You can avoid node-level lease if there is a single external controller that is the only actor allowed to mutate routes:

```text
CloudWatch/EventBridge/Lambda/controller detects failure
controller calls ReplaceRoute
NAT nodes never call ReplaceRoute
```

That architecture shifts leader election out of the NAT nodes. It may still need idempotency and state, but the split-brain surface is smaller.

Tradeoff:

- external controller is another component,
- detection can be slower or less local,
- multi-cloud story is different,
- the NAT appliance is less self-contained.

For BetterNAT's self-contained appliance model, lease/fencing is the right default.

## 3. Multi-cloud Compatibility

### The portable concept

The portable concept is not "AWS ReplaceRoute."

The portable concept is:

```text
Private subnet default route points to the active NAT appliance.
On failure, the control plane changes that next hop to a healthy appliance.
```

That maps to multiple clouds.

### Cloud abstraction

Define a provider interface:

```go
type RouteController interface {
    CurrentRoute(ctx, RouteRef) (RouteState, error)
    ReplaceRoute(ctx, RouteRef, NextHop) error
    VerifyRoute(ctx, RouteRef, NextHop) error
}

type LeaseBackend interface {
    Acquire(ctx, LeaseKey, Candidate, TTL) (Lease, error)
    Renew(ctx, Lease, TTL) error
    Release(ctx, Lease) error
    Current(ctx, LeaseKey) (Lease, error)
}
```

Cloud-specific implementations:

```text
AWS:
  RouteController = EC2 route table ReplaceRoute
  NextHop = instance ID or ENI ID
  LeaseBackend = DynamoDB

GCP:
  RouteController = VPC custom static route update/delete+create, depending on API semantics
  NextHop = next-hop instance or next-hop IP/ILB depending on topology
  LeaseBackend = Firestore, Cloud Spanner, GCS generation preconditions, or etcd/Consul

Azure:
  RouteController = route table UDR update
  NextHop = Virtual appliance private IP or internal load balancer IP
  LeaseBackend = Azure Blob lease, Cosmos DB conditional write, Table Storage/etcd/Consul
```

### GCP fit

Google Cloud VPC supports custom static routes with next-hop instances and next-hop IPs. GCP docs note that routes can use VM instances as next hops, and static route docs discuss next-hop IP behavior.

GCP-specific issues:

- VM IP forwarding must be enabled for appliance VMs.
- Route priority and tags matter.
- Static route update semantics may require delete/recreate instead of in-place "replace" in some cases.
- GCP Cloud NAT already exists, but self-managed NAT appliance can still target cost/observability niches.

### Azure fit

Azure User Defined Routes support next hop type `Virtual appliance`, where the next hop is an IP address.

Azure-specific issues:

- IP forwarding must be enabled on the appliance NIC and OS.
- UDR effective routes need validation.
- Azure route table propagation and subnet associations differ from AWS.
- Azure NAT Gateway pricing/feature comparison is different; product positioning must be revalidated.

### What is portable

Portable:

- NAT appliance datapath.
- flow observability.
- owner/team attribution.
- active/standby state machine.
- lease/fencing concept.
- Terraform module pattern.
- doctor checks, with provider-specific probes.

Provider-specific:

- route update API,
- route target type,
- public IP reassociation semantics,
- NIC/IP failover semantics,
- IAM/RBAC,
- metadata service,
- pricing model,
- route convergence behavior,
- managed NAT competitor cost model.

## Provider-specific Route Replacement Patterns

The product-level abstraction should be `ReplaceRoute`, but not every cloud has an AWS-style API with that exact behavior.

### AWS

AWS has a direct `ReplaceRoute` API.

Conceptual operation:

```text
route table rtb-a:
  0.0.0.0/0 -> eni-active

ReplaceRoute(rtb-a, 0.0.0.0/0, eni-standby)

route table rtb-a:
  0.0.0.0/0 -> eni-standby
```

Typical next hops for this product:

- EC2 instance ID.
- ENI ID.
- NAT Gateway for rollback.

AWS-specific notes:

- This is the cleanest provider implementation.
- Verify with `DescribeRouteTables`.
- Route to ENI is preferable for serious appliance mode after testing.

### GCP

GCP has VPC routes with destination range, priority, tags, and a single next hop. Supported custom static route next hops include next-hop instance, next-hop IP address, next-hop gateway, VPN tunnel, and internal passthrough Network Load Balancer in relevant modes.

GCP's Compute Engine Routes REST resource exposes methods such as `insert`, `delete`, `get`, and `list`; it does not map cleanly to an AWS-style in-place `ReplaceRoute`.

That means the GCP provider probably implements replacement as one of these patterns:

#### Pattern A: delete + insert

```text
delete old route:
  0.0.0.0/0 -> active appliance

insert new route:
  0.0.0.0/0 -> standby appliance
```

This is simple but may create a short no-route or wrong-route gap depending on ordering and existing defaults.

#### Pattern B: priority switch

Maintain two routes with different priorities:

```text
route-active:
  0.0.0.0/0 -> active appliance
  priority 100

route-standby:
  0.0.0.0/0 -> standby appliance
  priority 200
```

Failover would create/delete or adjust which route wins. If route priority cannot be updated in place, the provider still uses delete/insert but can reduce ambiguity by predefining names and tags.

#### Pattern C: next-hop internal load balancer

Use a next-hop internal passthrough Network Load Balancer in front of healthy NAT appliances.

This may give a more cloud-native HA shape on GCP than mutating route next hops directly, but it needs separate research:

- source NAT behavior,
- health checks,
- symmetric return path,
- cost,
- whether it preserves the cost/observability goals.

GCP-specific requirements:

- Appliance VMs must have IP forwarding enabled.
- Routes can be scoped by network tags; tag design becomes part of Terraform UX.
- Route priority and longest-prefix behavior must be modeled carefully.

Suggested first GCP implementation:

```text
RouteController = delete/insert static custom route
NextHop = next-hop instance or next-hop IP
LeaseBackend = Firestore/Spanner/GCS conditional generation or Consul/etcd
```

But this should be treated as a separate provider project, not a trivial port of AWS `ReplaceRoute`.

### Azure

Azure uses route tables with user-defined routes. For a network virtual appliance, the route's next hop type is `Virtual appliance`, and the next hop value is an IP address.

Conceptual operation:

```text
route table rt-a:
  0.0.0.0/0 -> Virtual appliance 10.0.1.10

update route:
  0.0.0.0/0 -> Virtual appliance 10.0.1.11
```

Typical next hop for this product:

- private IP of the active NAT appliance,
- possibly private IP of an internal load balancer in front of appliances.

Azure-specific notes:

- IP forwarding must be enabled on the appliance NIC and in the OS.
- Use Network Watcher next hop/effective routes for diagnostics.
- Azure's route table and UDR model maps well to the generic `RouteController`, but the next hop is an IP rather than an instance/ENI ID.
- A stronger Azure-native design might use an internal load balancer plus HA ports/NVA pattern; that needs separate validation against SNAT observability goals.

Suggested first Azure implementation:

```text
RouteController = update UDR next hop IP
NextHop = appliance private IP
LeaseBackend = Azure Blob lease, Cosmos DB conditional write, or etcd/Consul
```

### Alibaba Cloud

Alibaba Cloud VPC route tables support routes whose next hop can be resources such as NAT Gateway, ECS instance, elastic network interface, and other gateway/network resources.

Alibaba Cloud has `ModifyRouteEntry` for modifying custom route entry attributes including next hop. However, docs also call out important constraints:

- `ModifyRouteEntry` does not support concurrent modification of the same custom route entry and can return `TaskConflict`; retry/backoff is required.
- For some gateway route table entry updates, changing directly from one ECS instance or ENI next hop to another may not be allowed; the documented flow can require changing next hop to `Local` first, then changing to the target instance/ENI.
- `DeleteRouteEntry` is asynchronous and cannot be concurrently deleted in the same VPC/VBR route table context.

Conceptual operation if direct modify is allowed:

```text
route table vtb-a:
  0.0.0.0/0 -> ECS active

ModifyRouteEntry(nextHopType=Instance, nextHopId=ecs-standby)

route table vtb-a:
  0.0.0.0/0 -> ECS standby
```

If direct modify is not allowed for the route-table type:

```text
ModifyRouteEntry(nextHopType=Local)
wait/verify
ModifyRouteEntry(nextHopType=Instance or NetworkInterface, nextHopId=standby)
wait/verify
```

That two-step flow is operationally riskier because it can create a temporary routing gap or local route state that is not a NAT path.

Alibaba-specific notes:

- ECS instance and ENI next hops appear to fit the appliance model.
- Route modification conflict handling is a first-class requirement.
- The provider must model route-table type differences; normal VPC custom route entries and gateway route table entries may not behave identically.
- Verification via route entry list/status is mandatory after each async or conflict-prone operation.

Suggested first Alibaba Cloud implementation:

```text
RouteController = ModifyRouteEntry where supported, otherwise delete/create or documented two-step flow
NextHop = ECS instance ID or ENI ID
LeaseBackend = Tablestore/Redis/etcd/Consul or another conditional-write store
```

This provider needs deeper validation before being promised, because the two-step `Local` constraint can materially affect failover behavior.

## Multi-cloud Design Consequence

The core HA state machine must treat route replacement as an eventually consistent provider operation:

```text
Acquire lease
Request route replacement
Poll provider operation/status
Verify effective route or route table state
Run datapath probe
Confirm lease generation still current
Become active
```

Provider implementations should expose capability flags:

```yaml
capabilities:
  in_place_route_replace: true | false
  route_target_types: [instance, eni, ip, ilb]
  operation_is_async: true | false
  supports_effective_route_probe: true | false
  supports_public_ip_reassociation: true | false
```

This lets AWS be the clean first implementation while keeping GCP, Azure, and Alibaba Cloud possible without pretending they are identical.

## Product Architecture Recommendation

Keep AWS as the first-class provider, but avoid hard-coding AWS concepts into the core agent.

Suggested repo shape:

```text
internal/ha/
  state_machine.go
  lease.go
  route_controller.go

internal/provider/aws/
  route_controller.go
  dynamodb_lease.go
  metadata.go

internal/provider/gcp/
  route_controller.go
  lease_backend.go

internal/provider/azure/
  route_controller.go
  lease_backend.go
```

Terraform modules can be provider-specific:

```text
terraform/aws
terraform/gcp
terraform/azure
```

Do not try to make one universal Terraform module.

## Revised Default Decision

For AWS v0/v1:

```text
Route failover:
  ReplaceRoute on private route tables

Ownership:
  DynamoDB conditional-write lease with generation/fencing

Target:
  start with instance target for POC
  prefer ENI target for HA profile after testing

EIP:
  optional add-on for stable outbound public IP

SLO:
  publish measured new-connection recovery, not guaranteed connection preservation
```

For future multi-cloud:

```text
Keep the HA state machine cloud-neutral.
Implement route mutation and lease backend per provider.
```

## Sources

- AWS `ReplaceRoute` API: replaces an existing route in a VPC route table and requires destination plus exactly one target: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_ReplaceRoute.html
- AWS CLI `replace-route` reference, including target types such as NAT instance and network interface: https://docs.aws.amazon.com/cli/latest/reference/ec2/replace-route.html
- AWS Elastic IP concepts and association with instance or network interface: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/elastic-ip-addresses-eip.html
- EC2 `AssociateAddress` API for associating/reassociating an Elastic IP with an instance, network interface, or private IP: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_AssociateAddress.html
- AWS CLI `assign-private-ip-addresses` notes that an EIP associated with a secondary private IP moves when the private IP is moved to another network interface: https://docs.aws.amazon.com/cli/latest/reference/ec2/assign-private-ip-addresses.html
- AWS route tables: route table rules determine where subnet traffic is directed: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html
- AWS subnet route tables: each subnet is associated with a route table: https://docs.aws.amazon.com/vpc/latest/userguide/subnet-route-tables.html
- AWS NAT instance docs: private subnet route table sends internet traffic to the NAT instance: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html
- AWS `DescribeRouteTables` API for verification: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeRouteTables.html
- DynamoDB condition expressions for conditional writes: https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Expressions.ConditionExpressions.html
- DynamoDB TTL docs: TTL is for automatic item deletion/cleanup: https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/TTL.html
- Amazon Builders' Library on leader election and lease pitfalls: https://aws.amazon.com/builders-library/leader-election-in-distributed-systems/
- Google Cloud VPC routes overview: https://docs.cloud.google.com/vpc/docs/routes
- Google Cloud using routes, including next-hop instance considerations: https://docs.cloud.google.com/vpc/docs/using-routes
- Google Cloud static routes, including next-hop IP behavior: https://docs.cloud.google.com/vpc/docs/static-routes
- Google Cloud Compute Engine Routes REST API methods: https://cloud.google.com/compute/docs/reference/rest/v1/routes
- Azure virtual network traffic routing and user-defined routes: https://learn.microsoft.com/en-us/azure/virtual-network/virtual-networks-udr-overview
- Azure manage route tables, including next hop type `Virtual appliance` and next hop address: https://learn.microsoft.com/en-us/azure/virtual-network/manage-route-table
- Alibaba Cloud VPC `ModifyRouteEntry` API for modifying a custom route entry name, description, or next hop: https://www.alibabacloud.com/help/en/vpc/developer-reference/api-vpc-2016-04-28-modifyrouteentry
- Alibaba Cloud VPC `UpdateGatewayRouteTableEntryAttribute` API and note about switching Instance/NetworkInterface next hop to Local first: https://www.alibabacloud.com/help/en/vpc/developer-reference/api-vpc-2016-04-28-updategatewayroutetableentryattribute
- Alibaba Cloud VPC `DeleteRouteEntry` API: https://www.alibabacloud.com/help/en/vpc/developer-reference/api-vpc-2016-04-28-deleterouteentry
- Alibaba Cloud VPC `DescribeRouteTables` API for route table verification: https://www.alibabacloud.com/help/en/vpc/developer-reference/api-vpc-2016-04-28-describeroutetables
