# Agent Registry And Coordination Backend Plan

Date: 2026-06-23

## Status

Design plan.

This document updates the runtime control-plane direction for BetterNAT after alpha CLI/fleet-status testing showed that discovering peers through ASG and EC2 APIs is too AWS-specific and too permission-heavy.

## Decision

BetterNAT should treat DynamoDB as the AWS implementation of a small, reliable coordination backend, not only as a lease store.

The coordination backend should support:

- fenced HA lease ownership,
- agent self-registration,
- service discovery,
- node health and version inventory,
- lightweight control messages,
- future agent-to-agent data exchange through a backend-mediated mailbox.

On AWS, DynamoDB is the first implementation. On other platforms, the same interface can be backed by equivalent managed stores or self-hosted systems such as:

- Google Cloud Firestore or Spanner,
- Azure Cosmos DB or Table Storage,
- Redis with expirations,
- SQL with row locks and TTL cleanup,
- etcd or Consul in environments that already operate them.

The core agent and CLI should depend on a provider-neutral coordination interface. AWS-specific code should stay in the AWS backend implementation and install/provider layers.

## Why Change

The current alpha CLI fleet status path can discover other appliances by:

1. reading local config,
2. deriving the ASG name,
3. calling `DescribeAutoScalingGroups`,
4. calling `DescribeInstances`,
5. reading the route table to determine the active instance,
6. scraping each agent's metrics endpoint.

That works for AWS tests, but it is the wrong long-term shape because:

- it expands runtime permissions beyond the operations the agent should need,
- it bakes ASG and EC2 instance discovery into the CLI status path,
- it does not transfer cleanly to other clouds,
- it duplicates fleet membership information that each agent already knows about itself,
- it makes a simple status command depend on multiple cloud APIs.

BetterNAT already needs a reliable control-plane dependency for HA fencing. The same class of dependency should also serve as the neutral rendezvous point for agents.

## Non-Goals

This plan does not make DynamoDB part of the packet datapath.

This plan does not replace Prometheus metrics. The registry stores discovery and coarse status. Detailed counters and time-series telemetry still come from each agent's metrics endpoint or a user's monitoring stack.

This plan does not make registry data authoritative for HA ownership. Lease fencing remains the source of truth for whether an agent is allowed to mutate route and EIP state.

This plan does not require direct peer-to-peer agent networking for correctness.

## Backend Model

Use one provider-neutral interface with separate responsibility areas:

```go
type CoordinationBackend interface {
    LeaseStore
    AgentRegistry
    MessageStore
}
```

The first implementation can keep these as smaller interfaces and wire only the needed parts.

### Lease Store

Purpose:

- elect and fence the active owner,
- prevent stale owners from mutating cloud route/EIP state,
- preserve generation-based ownership semantics.

Required operations:

- `AcquireLease(groupID, instanceID, ttl)`
- `RenewLease(groupID, instanceID, generation, ttl)`
- `ReleaseLease(groupID, instanceID, generation)`
- `CurrentLease(groupID)`

Correctness requirements:

- conditional writes,
- monotonic generation,
- owner/generation fencing,
- no dependency on registry freshness for route/EIP mutation safety.

### Agent Registry

Purpose:

- let agents publish their own discovery and status records,
- let CLI and other agents list current peers without cloud-specific discovery APIs.

Required operations:

- `PutAgent(record, ttl)`
- `DeleteAgent(groupID, instanceID)`
- `ListAgents(groupID)`
- `GetAgent(groupID, instanceID)`

Agent record shape:

```json
{
  "gateway_id": "prod-egress",
  "ha_group_id": "prod-egress-us-west-2a",
  "instance_id": "i-abc",
  "node_id": "i-abc",
  "cloud": "aws",
  "region": "us-west-2",
  "availability_zone": "us-west-2a",
  "private_ip": "10.0.1.10",
  "public_ip": "203.0.113.10",
  "metrics_url": "http://10.0.1.10:9108/metrics",
  "version": "v0.1.0-alpha.1",
  "commit": "abcdef123456",
  "datapath_engine": "loxilb",
  "datapath_ready": true,
  "ha_state": "active",
  "lease_generation": 12,
  "route_target_match": true,
  "public_identity_match": true,
  "started_at": 1782190000,
  "updated_at": 1782190060,
  "expires_at": 1782190090
}
```

Freshness rules:

- agents refresh registry records at a short interval, for example every 5 seconds,
- `expires_at` should be 2-4x the refresh interval,
- stale records are ignored by CLI and agents even before backend TTL cleanup removes them,
- graceful shutdown should delete the registry record after releasing or shortening the lease.

### Message Store

Purpose:

- provide a future backend-mediated exchange path without direct peer RPC,
- support maintenance coordination, drain notices, upgrade intent, or diagnostic requests.

This should be P2 after alpha. The registry schema should leave room for it, but alpha does not need to implement it.

Possible operations:

- `PutMessage(groupID, targetInstanceID, message, ttl)`
- `ListMessages(groupID, instanceID)`
- `AckMessage(messageID)`

Message correctness must not be required for HA safety. HA safety remains lease-based.

## AWS DynamoDB Shape

Use one coordination table for the long-term AWS shape.

Recommended AWS shape:

```text
Table: betternat-<gateway>-coordination
PK: ha_group_id
SK: record_id
TTL attribute: expires_at
```

This avoids a new table for every coordination feature. New capabilities add new item types, not new DynamoDB schemas.

Example records:

```text
PK                                  SK
prod-egress-us-west-2a              lease
prod-egress-us-west-2a              agent#i-abc
prod-egress-us-west-2a              agent#i-def
prod-egress-us-west-2a              message#i-abc#1782190012#01
prod-egress-us-west-2a              drain#i-abc
```

### Lease Record

Shape:

```text
PK: ha_group_id
SK: lease
```

One item per HA group:

```text
ha_group_id
owner_instance_id
generation
expires_at
updated_at
```

Lease correctness still depends on conditional writes against the `lease` record. Registry or message records must never be required to fence route/EIP ownership.

### Agent Records

Shape:

```text
PK: ha_group_id
SK: agent#<instance_id>
TTL attribute: expires_at
```

Access pattern:

- agent: `PutItem` or `UpdateItem` own `(ha_group_id, agent#<instance_id>)` record,
- agent: `DeleteItem` own record on graceful shutdown,
- CLI: `Query` by `ha_group_id` and filter or prefix-match `record_id` beginning with `agent#`,
- agent: optionally `Query` peers in its own `ha_group_id`.

### Message And Intent Records

Future record families can use the same table:

```text
message#<target_instance_id>#<timestamp>#<nonce>
drain#<instance_id>
upgrade#<rollout_id>
```

Each family should define its own TTL and ack/delete semantics. None of these records should be needed for HA mutation safety.

IAM can be narrowed with leading-key conditions where practical:

- allow `dynamodb:UpdateItem` and `dynamodb:DeleteItem` for the configured coordination table,
- allow `dynamodb:Query` scoped to the configured `ha_group_id`,
- allow `dynamodb:GetItem` for exact records,
- avoid ASG/EC2 discovery permissions in normal `betternat status`.

For multi-AZ gateways, the same coordination table can hold all HA groups. `ha_group_id` remains the partition key.

Existing alpha environments with the old single-key lease table cannot change that table's primary key in place. Migration should create the new coordination table, write the current lease to `record_id=lease`, roll or replace agents onto the new config, and then retire the old lease table during cleanup.

## CLI Status Flow

Target flow:

```text
betternat status
  -> load /etc/betternat/agent.json
  -> read current lease from coordination backend
  -> query agent registry by ha_group_id
  -> drop stale records
  -> mark active by lease owner/generation
  -> scrape metrics_url for each fresh record
  -> render table
```

Normal `betternat status` should not call:

- `autoscaling:DescribeAutoScalingGroups`,
- `ec2:DescribeInstances`,
- cloud-provider fleet discovery APIs.

Cloud API checks should move to:

- `betternat doctor --live`,
- explicit debug commands,
- fallback code paths when the registry is missing.

This creates a clean split:

- registry = what agents say about themselves,
- lease = who is allowed to own route/EIP mutation,
- metrics = detailed counters,
- cloud live checks = independent verification.

## Agent Runtime Flow

On startup:

1. load config,
2. resolve local identity,
3. start metrics server,
4. register initial agent record,
5. start HA supervisor,
6. periodically refresh registry record with current status snapshot.

During normal operation:

```text
every registry_refresh_interval:
  snapshot local status
  read current HA snapshot
  update own registry item with expires_at
```

On graceful shutdown:

1. publish `ha_state=draining`,
2. release or shorten the lease if this node owns it,
3. complete ASG lifecycle action if present,
4. delete own registry record,
5. exit.

On ungraceful shutdown:

- registry record expires,
- lease eventually expires or is taken over,
- CLI ignores stale record after `expires_at`.

## Permissions Direction

Normal runtime permissions should trend toward:

- DynamoDB coordination table:
  - `GetItem`,
  - `Query`,
  - `UpdateItem`,
  - `DeleteItem`.
- EC2:
  - `ReplaceRoute`,
  - `DescribeRouteTables` for verification.
- EIP stable identity:
  - `AssociateAddress`,
  - `DescribeAddresses`.
- ASG lifecycle:
  - `CompleteLifecycleAction`.

Permissions that should not be needed for normal status:

- `autoscaling:DescribeAutoScalingGroups`,
- `ec2:DescribeInstances`.

Those can remain in maintainer/debug/live-doctor scopes if needed.

## Multi-Cloud Contract

Add a coordination backend config shape that does not expose AWS names in core agent code:

```json
{
  "coordination": {
    "backend": "dynamodb",
    "table": "betternat-prod-egress-coordination",
    "registry_refresh_interval_seconds": 5,
    "registry_ttl_seconds": 20
  }
}
```

For v0 compatibility, this can initially be derived from existing `ha.lease` config:

- `coordination.backend` defaults to `ha.lease.backend`,
- `coordination.table` defaults to a newly created `betternat-<gateway_id>-coordination` table for new installs.

Long term, `ha.lease` can become a compatibility alias over the coordination backend config.

For old alpha installs, `ha.lease.table` points at the legacy lease table. Once the coordination table exists, new agent configs should point lease and registry operations at `coordination.table`.

## Provider Upgrade And Reconcile Contract

Provider upgrades must not turn every provider-owned infrastructure change into a gateway replacement.

The Terraform provider should distinguish:

- provider-owned infrastructure drift,
- runtime appliance changes,
- data-plane ownership changes.

Provider-owned infrastructure should be reconciled in place when the change does not require moving route/EIP ownership or replacing running appliances.

Examples:

- update the BetterNAT-managed inline IAM policy,
- remove permissions that the new provider no longer requires,
- add permissions required by the coordination backend,
- create the coordination table,
- update provider-owned tags and metadata,
- write migration metadata into Terraform state.

The provider may overwrite only resources it owns by contract. For IAM, that means:

- the BetterNAT-managed inline policy named `betternat-runtime` can be replaced with the newly rendered policy document,
- BetterNAT-managed attachments can be attached or detached when explicitly owned by the provider,
- user-managed inline or attached policies on the same role must not be deleted.

Runtime and data-plane changes remain guarded:

- changes to bootstrap/user-data, AMI, datapath config, route target model, EIP mode, or agent runtime config should require explicit replacement or a provider-managed rolling update,
- provider-owned IAM/table reconciliation must not stop appliances,
- provider-owned IAM/table reconciliation must not mutate private route table targets,
- provider-owned IAM/table reconciliation must not associate or disassociate EIPs.

This contract is important for safe upgrades. A provider release that tightens permissions or adds a coordination table should converge existing installations without causing a fleet-wide outage.

Implementation trigger: the Terraform resource should keep an internal `provider_infrastructure_revision` state value. Bumping that value in a provider release creates a safe in-place update that runs provider-owned infrastructure reconciliation even when the user's HCL has no functional diff.

For the coordination-table migration:

1. create the new coordination table,
2. write or import the current lease record as `record_id=lease` only when the migration path is explicitly enabled,
3. update the BetterNAT-managed runtime IAM policy in place,
4. record the table name in Terraform state,
5. roll or replace agents through a separate runtime upgrade path.

## Rollout Plan

### Phase 1: Add Registry Backend

Code:

- add `internal/registry` or `internal/coordination`,
- implement in-memory registry for tests,
- implement DynamoDB registry,
- add schema tests for record encode/decode and stale filtering.

Provider/install:

- add coordination table creation with `ha_group_id` and `record_id` keys,
- render table name into agent config,
- add runtime IAM actions for the coordination table.
- add in-place reconciliation for provider-owned IAM policy and coordination table creation.

Agent:

- publish self record on startup,
- refresh record periodically,
- delete record on graceful shutdown.

CLI:

- change `betternat status` to registry-first,
- keep current AWS discovery as debug fallback only.

### Phase 2: Tighten Permissions

- remove `DescribeAutoScalingGroups` and `DescribeInstances` from normal runtime status requirements,
- keep them only for `doctor --live` if live cloud verification needs them,
- update `docs/user/reference/IAM_POLICY.md`,
- add negative tests showing `betternat status` works without ASG/EC2 discovery permissions.

### Phase 3: Agent Discovery Consumers

- let standby agents query registry records for peer visibility,
- expose peer freshness metrics,
- use registry records for drain/upgrade visibility,
- keep HA mutation safety tied to lease generation only.

### Phase 4: Message Store

- add backend-mediated messages for planned drain, diagnostics, and future upgrade orchestration,
- add TTL and ack semantics,
- avoid making message delivery required for failover safety.

## Alpha Acceptance Criteria

Minimum acceptance for replacing cloud discovery in CLI:

- two-agent AWS ASG test shows both agents in the registry,
- `betternat status` shows active/standby, versions, IPs, and metrics using registry plus metrics scrape,
- `betternat status` still works when the runtime role lacks:
  - `autoscaling:DescribeAutoScalingGroups`,
  - `ec2:DescribeInstances`,
- stale registry records disappear from status after TTL,
- graceful shutdown deletes or marks the node record stale/draining,
- route/EIP ownership remains fenced by the lease generation, not registry state.

## Open Questions

- Should the coordination table be created per gateway or shared per account/region with gateway-prefixed keys?
- Should the registry expose only private `metrics_url`, or also a future local authenticated control URL?
- Should record refresh be coupled to metrics snapshot collection or kept as a separate lightweight loop?
- Should non-AWS backends be implemented as build-time packages or plugin-style providers?
