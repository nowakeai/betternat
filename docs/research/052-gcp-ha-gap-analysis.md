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

The current GCP alpha implementation proves a necessary but insufficient layer:
GCE forwarding VMs, nftables masquerade, tagged route replacement, provider
status reads, and provider cleanup. It is not yet a BetterNAT-equivalent GCP HA
implementation.

## Current GCP Alpha State

Validated:

- private client egress through a `canIpForward=true` gateway VM,
- nftables masquerade on Debian 12 GCE gateway VMs,
- tagged default route replacement from `gw-a` to `gw-b`,
- provider-created gateway VMs and route,
- provider read path observing out-of-band route handover,
- destroy and residual cleanup.

Not validated or implemented:

- GCP lease backend,
- GCP agent registry and peer discovery,
- GCP proactive handover state machine,
- GCP passive lease-expiry failover,
- GCP public identity handover,
- LoxiLB datapath on GCE,
- GCP-specific IAM least privilege,
- multi-zone and GKE/private-node topologies,
- production migration from Cloud NAT.

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

### 3. Passive Failover

Hard crashes still need lease-expiry failover:

1. Standby observes expired Firestore lease.
2. Standby conditionally acquires a new generation.
3. Standby verifies its datapath.
4. Standby mutates route to itself.
5. Standby verifies route and reports active.

Test cases must prove that two standbys racing after expiry cannot both mutate
the route. The winner is the only node whose acquire transaction succeeds.

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

## Revised GCP Gates

Do not treat GCP as product-parity BetterNAT until all P0 gates pass.

### P0: HA Correctness

- [x] Firestore lease backend implemented with acquire, renew, release, transfer,
  and current.
- [x] Unit tests prove acquire, renew, release, expired takeover, and transfer
  fencing decisions.
- [ ] Live Firestore spike proves two contenders cannot both acquire an unexpired
  lease.
- [ ] Agent on GCE mutates routes only after lease verification.
- [ ] Passive failover after active crash works.
- [ ] Proactive handover works.
- [x] Provider destroy remains safe after out-of-band route movement.

### P1: Datapath And Public Identity

- LoxiLB on GCE validated or explicitly rejected with evidence.
- nftables fallback remains tested.
- Stable public IP is validated or explicitly not supported in GCP alpha.
- Existing connections are documented as not preserved.

### P2: Production Fit

- Least-privilege IAM documented and tested.
- Multi-zone behavior documented and tested.
- GKE/private-node install path tested in a disposable project.
- Observability and support bundle include GCP-specific HA evidence.
- Cleanup and residual scans include Firestore records and service accounts.

## Immediate Recommendation

Reframe the current `betternat_gcp_gateway` as a forwarding substrate spike, not
the GCP product alpha.

The next implementation step after the Firestore lease backend is live
coordination validation:

1. Run a live `shared-resources-alt` Firestore contention spike with two or
   more contenders.
2. Add a provider-neutral coordination record interface if the current
   DynamoDB-specific registry/handover structs cannot be reused cleanly.
3. Only then wire GCP agent HA to route replacement.

Until then, GCP should remain explicitly marked as non-HA alpha substrate work.
