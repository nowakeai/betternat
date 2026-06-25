# GCP HA Gap Analysis

Date: 2026-06-25

## Summary

BetterNAT's core value over a raw LoxiLB or nftables gateway is not merely
packet forwarding. The product value is the HA control plane around the
datapath:

- fenced active/standby ownership,
- safe route mutation,
- stable public identity when supported,
- proactive handover for shutdowns and upgrades,
- passive failover after hard crashes,
- observable status and recovery signals,
- deterministic cleanup and drift handling.

This does not mean LoxiLB has no HA. LoxiLB documents active/backup,
active/active BGP ECMP, connection sync, BFD fast failover, kube-loxilb role
arbitration, and multi-cloud HA patterns. BetterNAT should not compete by
claiming to invent those datapath or service-load-balancer primitives. The
BetterNAT product boundary is narrower and more cloud-NAT-specific:

- Terraform-first creation of the cloud substrate and runtime config,
- provider-owned route, identity, IAM, and cleanup lifecycle,
- agent-owned lease fencing before any route or public identity mutation,
- stable egress identity where the cloud supports it,
- operator-visible status for lease, route, identity, datapath, and handover,
- safe rollback to the customer's previous cloud NAT or route owner.

References:

- LoxiLB HA deployment scenarios:
  <https://docs.loxilb.io/main/ha-deploy/>
- LoxiLB multi-cloud HA notes:
  <https://github.com/loxilb-io/loxilbdocs/blob/main/docs/multi-cloud-ha.md>
- kube-loxilb deployment and role/BGP controls:
  <https://docs.loxilb.io/kube-loxilb/>

The current GCP alpha implementation proves a necessary but insufficient layer:
GCE forwarding VMs, nftables masquerade, tagged route replacement, provider
status reads, and provider cleanup. It is not yet a BetterNAT-equivalent GCP HA
implementation.

Important correction from the 2026-06-25 review: because BetterNAT's main value
over a raw LoxiLB appliance is the HA product layer, GCP must not be framed as
"mostly done" after forwarding, route replacement, or provider bootstrap. Those
items are substrate. The product milestone is live agent-owned HA.

## Current GCP Alpha State

Validated:

- private client egress through a `canIpForward=true` gateway VM,
- nftables masquerade on Debian 12 GCE gateway VMs,
- tagged default route replacement from `gw-a` to `gw-b`,
- provider-created gateway VMs and route,
- provider read path observing out-of-band route handover,
- destroy and residual cleanup.

Implemented but not live-validated:

- Firestore lease backend,
- Firestore agent registry and handover records,
- GCP route controller for tagged static route mutation,
- agent runtime wiring for `cloud=gcp`,
- GCP runtime service-account and IAM permission contract,
- optional provider-owned Firestore Native database lifecycle,
- GCP preflight and residual-scan scripts,
- GCP support bundle collection.

Not yet validated or still incomplete:

- live Firestore contention with a real database,
- two-agent route mutation guarded by a live Firestore lease,
- passive lease-expiry failover after a hard active crash,
- proactive handover on graceful shutdown or upgrade,
- GCP public identity handover,
- LoxiLB datapath on GCE,
- applied least-privilege runtime IAM with no broad project role,
- multi-zone and GKE/private-node topologies,
- production migration from Cloud NAT.

## Raw LoxiLB Comparison

The GCP decision must compare against two different baselines:

1. Raw LoxiLB as a datapath or Kubernetes service load balancer.
2. BetterNAT as a private-subnet egress replacement for managed cloud NAT.

Raw LoxiLB can already provide important HA primitives, especially in
Kubernetes or BGP-friendly environments. The missing BetterNAT work is the
cloud egress appliance product layer around those primitives.

| Area | Raw LoxiLB strength | BetterNAT requirement |
| --- | --- | --- |
| Datapath | eBPF L4/NAT datapath, conntrack, BGP, BFD, connection sync scenarios | Use LoxiLB as the primary local SNAT engine and reconcile desired egress rules after restart or failover |
| HA election | kube-loxilb can arbitrate roles for service load balancer use cases | Cloud-independent lease fencing must guard route and public identity mutation |
| Cloud route ownership | LoxiLB examples include cloud HA patterns, but route ownership is not the BetterNAT Terraform contract | Provider creates owned routes; agent mutates only configured routes while holding a valid lease |
| Public identity | AWS Elastic IP pattern exists; GCP support is explicitly incomplete in LoxiLB multi-cloud notes | Product must validate stable egress identity per cloud or document route-only non-stable identity |
| Install UX | LoxiLB install is component-focused | BetterNAT must install an appliance pool, service account/IAM, config, systemd, metrics, and rollback metadata |
| Observability | LoxiLB exposes component state and counters | BetterNAT must expose normalized operator signals: active owner, lease generation, route target, public identity, datapath readiness, handover phase, and API errors |
| Cleanup/rollback | Component teardown depends on deployment model | Terraform destroy and rollback must restore or preserve customer routes safely and scan residual cloud resources |

Therefore the GCP gate is not "can a GCE VM forward packets with LoxiLB or
nftables?" That only proves a datapath substrate. The gate is "can the
BetterNAT agent safely own and move the cloud egress role under failure,
upgrade, and cleanup conditions?"

## HA Product Boundary Audit

The GCP work should be judged against the feature delta that BetterNAT adds on
top of a raw LoxiLB node. The following items are the core product boundary, not
nice-to-have polish:

| Capability | Why raw LoxiLB is not enough for BetterNAT | Current GCP state | Required proof |
| --- | --- | --- | --- |
| Fenced active owner | Cloud route and public identity mutation must be guarded by a cloud-independent lease generation | Firestore backend exists and agent can construct it | Live two-agent contention where only the lease winner mutates the route |
| Passive failover | A hard-crashed active must be replaced without an operator running route commands | HA controller supports acquire-after-expiry in code | Kill or stop active GCE VM and prove standby acquires, routes, verifies, and reports active |
| Proactive handover | Upgrades, shutdown, Spot/MIG lifecycle, and manual maintenance should move ownership before the old active exits | Generic handover path exists; GCP live path unvalidated | Manual handover and systemd stop on active GCE node produce completed handover records and no route split-brain |
| Route verification | GCP static route delete/insert is not an atomic AWS `ReplaceRoute` equivalent | GCP provider describes and replaces route | Every mutation verifies target and degrades on Compute API or propagation failure |
| Stable public identity | Many egress users care about allowlists and source-IP continuity | GCP route-only mode intentionally has non-stable public identity | Either validate an address handover design or state that GCP alpha is non-stable only |
| Datapath reconciliation | LoxiLB state can be lost after restart; BetterNAT owns desired-state replay | AWS path has this design; GCP tests so far used nftables substrate | LoxiLB on GCE install, counters, restart replay, and nftables fallback evidence |
| Peer readiness | A standby must be selected only if it is healthy enough to receive traffic | Registry and peer prepare APIs exist | Handover refuses stale, unhealthy, or wrong-generation standby records in live GCE smoke |
| Observability | Operators need to see whether HA is working before failure | Metrics/status are provider-neutral, but GCP support bundle not proven | GCP status includes lease, route, datapath, handover, Firestore errors, and Compute operation IDs |
| Rollback and destroy | Terraform cleanup must not fight or orphan agent-owned route changes | Provider cleanup passed substrate spike | Destroy after agent-owned handover restores/removes provider-owned routes and leaves no residual resources |

This audit means the GCP acceptance bar is not lower because LoxiLB can provide
its own HA patterns. BetterNAT can reuse LoxiLB as the local datapath, but the
product still owns cloud route safety, lease fencing, lifecycle handover,
operator status, and rollback.

## Revised HA Gate

GCP should not be considered a BetterNAT-equivalent alpha until the following
sequence passes in a disposable project:

1. Two GCE gateway nodes boot with the BetterNAT agent, publish registry
   records, and keep LoxiLB or nftables ready.
2. Exactly one node acquires a Firestore lease generation and mutates the
   configured tagged default route.
3. The active node reports `ACTIVE` only after route verification and datapath
   readiness pass.
4. The standby reports `STANDBY` and refuses route mutation while another
   unexpired owner holds the lease.
5. A hard active crash causes the standby to acquire the next lease generation,
   move the route, verify egress, and expose failover metrics.
6. A graceful handover moves route ownership and transfers the lease without a
   split-brain window.
7. Destroy after handover removes or restores the provider-owned route and
   leaves no residual instances, service accounts, addresses, Firestore
   records, or firewall rules.

Evidence that is insufficient by itself:

- a single GCE VM forwarding packets,
- manual `gcloud compute routes delete/create` replacement,
- provider status reading a route target,
- Firestore unit tests without live contention,
- GCP bootstrap rendering without an agent-owned failover.

The first group proves BetterNAT's HA product layer. The second group proves
only substrate readiness.

## Raw LoxiLB HA Research Implications

The upstream LoxiLB docs matter because BetterNAT should be honest about what
it adds. LoxiLB's HA documentation covers flat-L2 active/backup, L3
active/backup with BGP, active/active BGP ECMP, connection sync, and BFD-based
fast failover. `kube-loxilb` can choose an active LoxiLB pod, monitor health,
and elect a replacement in Kubernetes service-load-balancer deployments. The
multi-cloud notes also describe AWS floating-IP style HA, while explicitly
calling out that full elastic-IP support for GCP is not available in that
pattern.

That changes the BetterNAT bar in two ways:

- Do not market "HA" as a generic LoxiLB capability. LoxiLB already has HA
  modes.
- Market and test the narrower BetterNAT layer: Terraform-owned appliance
  lifecycle, cloud route/identity ownership, fenced mutation, cost-oriented NAT
  replacement UX, rollback, and normalized status.

For GCP this means a raw-LoxiLB baseline should be run, but passing raw-LoxiLB
HA does not automatically pass BetterNAT. The pass condition is whether
BetterNAT can wrap the datapath in a safe cloud egress ownership model that a
Terraform user can install, observe, fail over, and destroy.

## Additional Underweighted Areas

The review also found several areas that were not weighted strongly enough in
the first GCP spike plan:

- MIG or equivalent capacity repair. AWS uses ASG repair as a separate loop
  after fast failover. GCP needs an explicit decision on unmanaged instances
  versus MIGs, and tests must prove replacement nodes join standby without
  disrupting the active owner.
- Zone failure semantics. GCP routes are VPC-global, while next-hop instances
  are zonal. Same-zone active/standby is not enough to understand cross-zone
  behavior, route propagation, and cost.
- GKE/private-node integration. The target users include private Kubernetes
  nodes. Network tags, route priority, subnet scope, and coexistence with
  Cloud NAT must be tested with a private-node shape, not only a standalone VM.
- Bootstrap dependency risk. Startup-script installs are acceptable for spikes,
  but production HA should not depend on first-boot package repositories and
  GitHub downloads during replacement after a failure. Prebaked image or
  private artifact mirror behavior needs a gate before GA.
- Failure injection coverage. Tests need forced failures at the dangerous
  points: after route delete, after route insert before lease transfer, after
  Firestore transfer, during Compute operation polling, and during LoxiLB
  restart.
- IAM lifecycle. A permission list is useful, but GCP HA smoke needs a service
  account with the custom role actually applied and verified by the agent. Broad
  project roles must not count as product evidence.
- Supportability without project-owner access. A support bundle should be able
  to explain a failed failover from local logs, Firestore records, route state,
  and operation IDs without requiring an engineer to have owner access to the
  customer's project.
- Cloud route failure semantics. GCP static routes with next-hop instances have
  important behavior that the HA controller must account for: `canIpForward`
  is required, Google Cloud does not validate guest routing software health,
  stopped/deleted next-hop behavior depends on competing routes, and a route
  by instance name does not update when the instance is deleted. BetterNAT must
  verify the route and datapath from the agent instead of trusting route API
  success.
- Public identity honesty. Route-only GCP failover can restore egress while
  changing the observed public IP. That is useful for some workloads but is not
  the same product promise as AWS shared-EIP mode.
- Runtime image quality. HA is weak if replacement nodes must fetch unsigned or
  unavailable packages during an outage. The GCP path needs a prebaked image or
  private artifact mirror gate before GA.

## DynamoDB Equivalent

The closest GCP counterpart for the AWS DynamoDB coordination table is
Firestore in Native mode.

Candidate mapping:

| AWS role | GCP candidate | Decision |
| --- | --- | --- |
| DynamoDB lease record | Firestore document in a per-HA-group collection | Preferred |
| DynamoDB conditional update | Firestore transaction or update precondition | Preferred |
| DynamoDB coordination table | Firestore collection group | Preferred |
| DynamoDB TTL for stale metadata cleanup | Firestore TTL policy | Useful for cleanup, not fencing |
| Minimal conditional blob store | GCS object with generation preconditions | Backup option |

Firestore is the better product fit because the existing AWS design needs more
than one lock object. It needs lease, agent registry, drain, and handover
records with indexed reads and generation-scoped writes. Firestore transactions
support atomic read/write flows and retry on concurrent edits. The Go client
also supports update/delete preconditions on document update time. Firestore TTL
can clean stale metadata, but TTL deletion must not be used as the HA authority.
Agents must compare `expires_at` themselves during every lease acquire, renew,
transfer, and status read.

GCS generation preconditions are viable for a very small lease object because
Cloud Storage request preconditions prevent mutating an object unless its
generation or metageneration matches the expected state. That is useful for
safe read-modify-write updates, but it is weaker for BetterNAT's broader
coordination needs: peer registry queries, handover record listing, state
history, and support/debug views become awkward object-prefix scans instead of
document queries.

References:

- Firestore transactions and batched writes:
  <https://firebase.google.com/docs/firestore/manage-data/transactions>
- Firestore Go update preconditions:
  <https://pkg.go.dev/cloud.google.com/go/firestore>
- Firestore TTL policies:
  <https://firebase.google.com/docs/firestore/ttl>
- Cloud Storage request preconditions:
  <https://docs.cloud.google.com/storage/docs/request-preconditions>

## Required GCP HA Architecture

The GCP implementation should reuse the provider-neutral HA model already used
on AWS:

```text
LeaseBackend:
  acquire(owner)
  renew(owner, generation)
  transfer(current_owner, generation, new_owner)
  release(owner, generation)
  current()

CloudRouteController:
  current route target
  replace route target
  verify route target

PublicIdentityController:
  current public identity owner
  move public identity
  verify public identity
```

For the first real GCP HA milestone, `PublicIdentityController` may be
explicitly disabled. In that mode GCP HA is route-only and the public egress IP
changes to the active gateway's per-VM public IP. That is acceptable only if it
is documented as non-stable public identity.

### Firestore Record Shape

Use one collection per BetterNAT gateway or a collection group with `gateway_id`
and `ha_group_id` fields.

Suggested logical keys:

```text
gateways/{gateway_id}/ha_groups/{ha_group_id}/records/lease
gateways/{gateway_id}/ha_groups/{ha_group_id}/records/agent#{node_id}
gateways/{gateway_id}/ha_groups/{ha_group_id}/records/handover#{generation}
gateways/{gateway_id}/ha_groups/{ha_group_id}/records/drain#{node_id}
```

Lease document fields:

```json
{
  "record_type": "lease",
  "gateway_id": "prod-egress",
  "ha_group_id": "prod-egress-us-west1-a",
  "owner_instance_id": "gce-instance-a",
  "owner_node_id": "prod-egress-gw-a",
  "generation": 42,
  "expires_at": "2026-06-25T05:30:10Z",
  "updated_at": "2026-06-25T05:30:00Z"
}
```

Acquire transaction:

1. Read `lease`.
2. If missing, expired, or already owned by local owner, write local owner with
   `generation + 1` and new `expires_at`.
3. If held by another unexpired owner, fail.

Renew transaction:

1. Read `lease`.
2. Require owner and generation to match local record.
3. Require `expires_at > now`.
4. Extend `expires_at`, keep generation unchanged.

Transfer transaction:

1. Read `lease`.
2. Require owner and generation to match active owner.
3. Require `expires_at > now`.
4. Set new owner, increment generation, set new `expires_at`.

Release transaction:

1. Read `lease`.
2. Require owner and generation to match.
3. Delete or clear owner.

The transaction can use Firestore server commit semantics for atomicity, but
lease expiry comparisons still depend on timestamp values supplied by the
agent. The implementation should explicitly budget for clock skew and prefer
short renewal intervals with a TTL that leaves enough margin for GCP API
latency.

## GCP Route HA Differences

The GCP route model is similar enough for route-based failover but has important
differences from AWS:

- routes live at VPC network scope and can be selectively applied with tags,
- route changes are propagated with an eventually consistent design,
- next-hop instances must have `canIpForward=true`,
- Google Cloud does not health-check next-hop instances for the product's
  datapath readiness,
- stopped/deleted next-hop behavior depends on routing order and available
  alternate routes,
- routes with next-hop instance by name are not a direct AWS `ReplaceRoute`
  equivalent; in practice our spike deleted and recreated the route.

The HA controller must therefore verify route target after every mutation and
must not treat successful route API completion as proof that the datapath is
healthy.

References:

- GCP VPC routes:
  <https://docs.cloud.google.com/vpc/docs/routes>
- GCP static route next-hop instance requirements:
  <https://docs.cloud.google.com/vpc/docs/static-routes>

## Missing Product-Significant Areas

### 1. Agent HA On GCP

The current provider alpha creates resources but does not run the BetterNAT
agent as the control-plane owner. A real GCP alpha needs the agent to:

- publish registry records to Firestore,
- acquire/renew/release/transfer Firestore leases,
- verify local datapath readiness,
- mutate GCP routes only while fenced,
- expose daemon status and handover state,
- degrade rather than falsely report active when Firestore or Compute API is
  unreachable.

### 2. Proactive Handover

BetterNAT's AWS direction favors proactive handover for graceful shutdown,
Spot/ASG interruption, manual handover, and upgrades. GCP needs the same shape:

1. Active owns Firestore lease generation `N`.
2. Active selects a fresh standby from registry.
3. Standby proves datapath readiness and Firestore reachability.
4. Active writes `handover#N`.
5. Active re-verifies lease generation `N`.
6. Active changes the tagged route to the standby.
7. Active verifies route target and client-side datapath if possible.
8. Active transfers the lease to standby as generation `N+1`.
9. Standby observes lease ownership and reports active only after route
   verification.

The route-moved-before-lease-transfer window exists on GCP just as it does on
AWS. The old active must keep renewing the lease until it either completes the
lease transfer or reverts the route.

Additional cases to test:

- active receives shutdown while Firestore is reachable but Compute route
  mutation fails,
- active receives shutdown after route mutation but before lease transfer,
- standby is present in the registry but has stale datapath readiness,
- two standbys race to accept a handover after the active disappears,
- handover request is replayed after lease generation has changed.

### 3. Passive Failover

Hard crashes still need lease-expiry failover:

1. Standby observes expired Firestore lease.
2. Standby conditionally acquires a new generation.
3. Standby verifies its datapath.
4. Standby mutates route to itself.
5. Standby verifies route and reports active.

Test cases must prove that two standbys racing after expiry cannot both mutate
the route. The winner is the only node whose acquire transaction succeeds.

Additional cases to test:

- active VM poweroff or GCE stop without systemd shutdown hooks,
- LoxiLB process crash while the VM and agent remain alive,
- Firestore transient outage on active only,
- Firestore transient outage on standby only,
- Compute route API transient failure after lease acquisition,
- stale route target that already points at a dead instance before takeover.

### 4. Public Identity

The current GCP resource uses per-VM public IPv4 addresses. That means failover
changes public egress IP. For many BetterNAT users, stable egress IP is part of
the product promise.

GCP public identity options still need a separate spike:

- route-only, non-stable public identity: simplest, already validated,
- reserved external address reassignment to a gateway VM/NIC: must validate API
  semantics and OS/SNAT behavior,
- next-hop internal passthrough load balancer plus Cloud NAT or external
  identity layer: different product shape, may undermine the "replace managed
  NAT Gateway/Cloud NAT processing cost" goal,
- no stable public identity in GCP alpha: acceptable only with explicit docs.

Do not imply GCP parity with AWS stable EIP until this is proven.

The public identity gate is independent from route failover. A GCP alpha may
ship route-only HA, but only if the Terraform schema, status data source,
release notes, and docs all say that egress public IP is not stable across
failover.

### 5. LoxiLB On GCE

The current GCP tests used nftables. That is useful as a fallback and debugging
baseline, but it does not prove the preferred BetterNAT datapath.

Required tests:

- install LoxiLB on GCE,
- configure egress SNAT equivalent to AWS,
- verify TCP/UDP/DNS,
- verify counters,
- verify restart behavior,
- verify fallback to nftables,
- compare throughput/CPU enough to decide whether LoxiLB remains primary on
  GCP.

This also needs a raw-LoxiLB baseline run. If raw LoxiLB already provides a
cleaner GCP HA mode for this exact egress use case, BetterNAT should either
reuse that primitive behind the agent/provider contract or explicitly document
why BetterNAT-owned route fencing is still required.

### 6. Multi-Zone And Regional Semantics

The current tests are single-zone. Product HA needs at least:

- same-zone active/standby behavior,
- cross-zone route target behavior and latency/cost implications,
- subnet/tag scoping for private clients across zones,
- route priority collisions with customer routes,
- behavior when one zone is unavailable but VPC/global routes still exist.

### 7. IAM And Security

GCP least privilege needs its own policy. The runtime service account should be
able to:

- read its own instance metadata,
- read/list peer gateway instances only as needed,
- read/create/update/delete specific Firestore records under its gateway path,
- read and mutate only provider-owned or explicitly configured routes,
- read public IP and route state for verification.

It should not have broad project editor permissions.

### 8. Observability And Supportability

GCP alpha needs the same operator signals as AWS:

- active/standby state,
- lease owner/generation/expiry,
- route target match,
- public identity match or explicit unsupported status,
- datapath readiness,
- Firestore API errors and latency,
- Compute route API errors and latency,
- handover phase durations,
- support bundle content that redacts tokens.

### 9. Provider Versus Agent Ownership

The Terraform provider should create durable infrastructure and initial config.
It should not be the active HA controller. Once the agent HA path exists, the
provider should:

- create Firestore database/collection policy or accept an existing one,
- create service accounts and IAM bindings,
- render agent config,
- create gateway capacity,
- expose read-only status data sources,
- clean up provider-owned resources safely.

Runtime route mutation should be agent-owned and lease-fenced.

Current implementation note: the provider can now render experimental
Firestore-backed GCP agent config, runtime artifact checksums, peer API token,
and cloud-init user data when `enable_agent_ha = true`. That is a prerequisite
for live HA smoke, not proof that live GCE HA is complete.

The agent runtime can also resolve GCP `local.node_id = "auto"` through GCE
metadata. This is required because GCP route-only HA uses the instance name as
the `nextHopInstance` route target.

The GCP resource can attach an explicit runtime `service_account_email` to
gateway VMs. The experimental agent HA path can also create a provider-owned
runtime service account when `manage_runtime_service_account = true`, so live
smoke does not depend on the broad and environment-specific Compute Engine
default service account.
The provider also exposes the runtime permission contract as computed
`runtime_iam_permissions`. It can manage the project-level BetterNAT runtime
custom role and service-account binding when `manage_runtime_iam = true`; that
IAM lifecycle is still pending live validation in the GCP HA smoke.

### 10. Agent Packaging And Bootstrap

GCP HA requires the same appliance bootstrap quality as AWS:

- install BetterNAT agent and CLI artifacts with checksum verification,
- install and start LoxiLB with a pinned or documented version,
- write config with Firestore, route, network tag, and zone/project settings,
- run systemd units with restart policy and ordered shutdown handover hooks,
- expose logs and metrics consistently with AWS,
- avoid startup-script-only behavior as the production control plane.

### 11. Customer Route And Cloud NAT Migration

GCP adoption is risky unless route ownership is explicit. The provider and docs
must cover:

- importing or replacing an existing `0.0.0.0/0` private route,
- coexistence with Cloud NAT during migration,
- rollback from BetterNAT route to Cloud NAT or a previous next hop,
- route priority conflicts and network tag scoping,
- behavior for subnets or workloads not carrying the BetterNAT route tag.

### 12. Support Bundle And Postmortem Evidence

The support workflow must collect enough data to debug HA incidents without
project-owner access:

- Firestore lease, registry, and handover records for the gateway path,
- route object, operation IDs, and last observed target. The local support
  bundle now attempts best-effort GCP route describes for configured route
  names,
- local datapath status and LoxiLB/nftables counters,
- agent logs around lease renewals and route operations,
- redacted service account and metadata identity. The local support bundle now
  captures GCE metadata identity when `cloud=gcp`,
- cleanup residual scan output.

## Revised GCP Gates

Do not treat GCP as product-parity BetterNAT until all P0 gates pass.

### P0: HA Correctness

- [x] Firestore lease backend implemented with acquire, renew, release, transfer,
  and current.
- [x] Unit tests prove acquire, renew, release, expired takeover, and transfer
  fencing decisions.
- [ ] Live Firestore spike proves two contenders cannot both acquire an
  unexpired lease. The reusable integration test exists, but
  `shared-resources-alt` has no Firestore database and database creation is
  currently blocked by IAM for `renjie@altresear.ch`.
- [x] Agent runtime can construct Firestore coordination and a GCP route
  provider for `cloud=gcp`.
- [x] Provider can render experimental GCP agent HA config and bootstrap user
  data behind an explicit switch.
- [x] Agent can resolve GCE instance name from metadata for `local.node_id =
  "auto"`.
- [x] Provider can attach an explicit runtime service account to GCE gateway
  VMs for agent HA smoke.
- [x] Provider has opt-in runtime service-account lifecycle behind
  `manage_runtime_service_account`.
- [x] Provider exposes the GCP runtime IAM permission contract for validation
  custom roles.
- [x] Provider has opt-in runtime IAM custom-role and binding lifecycle behind
  `manage_runtime_iam`.
- [ ] Live `manage_runtime_iam` validation. Current `shared-resources-alt`
  preflight for `renjie@altresear.ch` is missing `iam.roles.create`,
  `iam.roles.update`, and `iam.roles.delete`.
- [x] Provider has opt-in Firestore Native database lifecycle behind
  `manage_firestore_database`.
- [ ] Live `manage_firestore_database` validation. Current
  `shared-resources-alt` preflight for `renjie@altresear.ch` is missing
  Firestore database create/delete permission and the project has no existing
  Firestore database.
- [ ] Agent on GCE mutates routes only after lease verification in live
  validation.
- [x] GCP `cloud.Provider` route replace/describe implementation exists for
  tagged static routes with `nextHopInstance`.
- [ ] Passive failover after active crash works.
- [ ] Proactive handover works.
- [ ] Route mutation cannot occur without a current lease generation. The
  controller now verifies the current lease generation before and after active
  repair, activation, and handover cloud mutations in local tests, but live GCE
  evidence is still required.
- [ ] Agent degrades instead of reporting active when Firestore or Compute route
  verification is unavailable. Local supervisor tests now cover degradation
  when a previously active node cannot read the lease backend and when active
  ownership verification fails, but live GCE evidence is still required.
- [x] Provider destroy remains safe after out-of-band route movement.

### P1: Datapath And Public Identity

- LoxiLB on GCE validated or explicitly rejected with evidence.
- Raw LoxiLB GCP HA behavior compared against BetterNAT-owned route fencing.
- nftables fallback remains tested.
- Stable public IP is validated or explicitly not supported in GCP alpha.
- Existing connections are documented as not preserved.
- Bootstrap installs agent, CLI, datapath, config, metrics, and systemd units
  with artifact integrity checks in live GCE validation.

### P2: Production Fit

- Least-privilege IAM documented and tested. The current implementation can
  attach an explicit runtime service account, exposes the runtime permission
  contract, and has opt-in provider-owned service-account, custom role, and IAM
  binding lifecycle. Live GCP validation is still pending.
- Multi-zone behavior documented and tested.
- GKE/private-node install path tested in a disposable project.
- Observability and support bundle include GCP-specific HA evidence. Local
  support bundle collection now includes GCP metadata, Firestore database list,
  and configured route describe attempts; live GCE bundle evidence is still
  pending.
- Cleanup and residual scans include Firestore records and service accounts.
  `scripts/gcp-residual-scan.sh` now provides a read-only residual gate for
  Compute instances, routes, firewall rules, addresses, service accounts, and
  BetterNAT Firestore coordination records; live post-destroy evidence is still
  pending.
- Cloud NAT migration and rollback route ownership are documented and tested.

## Immediate Recommendation

Reframe the current `betternat_gcp_gateway` as a forwarding substrate plus
experimental HA bootstrap path, not the GCP product alpha. BetterNAT's GCP bar
is agent-owned HA around the datapath, not raw packet forwarding or manually
driven route replacement.

The next implementation step after the Firestore lease backend and local agent
wiring is live coordination and HA validation:

1. Run a live `shared-resources-alt` Firestore contention spike with two or
   more contenders.
2. Run the extended live smoke against an existing or temporary Firestore
   database to validate lease, registry, and handover records together.
3. Run the agent on two GCE gateways and prove route mutation is lease-fenced.
4. Validate passive failover, proactive handover, and LoxiLB-on-GCE before
   promoting the GCP provider resource beyond substrate alpha.
5. Decide and document the GCP capacity-repair model: unmanaged provider-owned
   instances for alpha only, or MIG-backed replacement before GA.
6. Test destroy/rollback after an agent-owned handover, not only after provider
   initial route creation.

Until then, GCP should remain explicitly marked as non-HA alpha substrate work.
