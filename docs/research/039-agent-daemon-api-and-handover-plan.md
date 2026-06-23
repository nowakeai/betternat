# Agent Daemon API And Proactive Handover Plan

Date: 2026-06-23

## Decision

`betternat-agent` should become the long-lived local control daemon for BetterNAT.

The `betternat` CLI should behave like `docker` or `etcdctl`: it should be a thin client that talks to a daemon API by default, instead of rebuilding fleet state itself from DynamoDB, Prometheus endpoints, and cloud APIs on every command.

The coordination backend remains the durable, provider-neutral relay for discovery, lease fencing, and low-frequency control-plane records. The agent daemon should hide that backend from the CLI and maintain a fast local view.

This design borrows several mature control-plane ideas from etcd:

- separate client and peer surfaces,
- advertise reachable peer/client URLs instead of making clients rediscover topology,
- reuse long-lived clients and bound every request with deadlines,
- use leases/TTLs for liveness-sensitive records,
- use watch-like background loops rather than foreground CLI fanout,
- require authenticated peer traffic before enabling remote peer writes.

BetterNAT should not copy etcd's Raft model. DynamoDB or another coordination backend remains the durable consensus/conditional-write substrate for BetterNAT. The etcd lesson here is API and operational shape: a stable daemon serving clients from local state, with explicit peer surfaces and strong transport/auth boundaries.

## Why

Recent AWS validation showed that registry-first CLI status is functionally correct, but it is still doing too much work synchronously:

1. read config,
2. create a DynamoDB client,
3. read the lease,
4. query agent records,
5. scrape metrics from each peer,
6. format output.

That is acceptable for tests, but not for an operator CLI that should feel immediate.

The daemon already runs continuously, renews leases, refreshes registry records, serves metrics, and watches HA state. It is the right place to:

- keep a local cache of peer records,
- scrape peer metrics in the background,
- track freshness,
- expose a stable local API,
- coordinate graceful handover.

## Target Shape

Each gateway appliance runs:

```text
betternat CLI
  -> unix:///run/betternat/agent.sock
  -> local betternat-agent daemon
       -> cached local status
       -> cached peer registry records
       -> cached peer metrics summaries
       -> HA supervisor state
       -> coordination backend
       -> optional peer agent API
```

The default CLI path should be local and fast:

```sh
betternat status
betternat failover status
betternat datapath status
betternat doctor
```

The CLI should support explicit remote/control targets later:

```sh
betternat --host unix:///run/betternat/agent.sock status
betternat --host https://10.0.1.10:9109 status
BETTERNAT_HOST=unix:///run/betternat/agent.sock betternat status
```

The direct-config path can remain as a fallback/debug mode:

```sh
betternat status --config /etc/betternat/agent.json --direct
```

But direct mode should not be the default operator UX.

### Client And Peer Surfaces

Model the daemon surfaces after etcd's client-vs-peer split.

BetterNAT should maintain two distinct endpoint classes:

| Surface | Default | Users | Purpose |
| --- | --- | --- | --- |
| Local client API | `unix:///run/betternat/agent.sock` | local CLI, support tools | fast local operator API |
| Metrics API | `http://<private-ip>:9108/metrics` | Prometheus | scrape-only observability |
| Peer control API | disabled initially; future `https://<private-ip>:9109` | other agents | authenticated handover/drain coordination |

Configuration should distinguish listen and advertise addresses:

```json
{
  "control": {
    "client": {
      "listen": "unix:///run/betternat/agent.sock"
    },
    "peer": {
      "enabled": false,
      "listen_address": "0.0.0.0",
      "listen_port": 9109,
      "advertise_url": "https://10.0.1.10:9109"
    }
  }
}
```

Rules:

- local client API is for local operators and should not be advertised through the registry,
- peer API advertise URL may be published in `agent#<instance_id>` records only when peer API is enabled and authenticated,
- Prometheus metrics URL remains a separate scrape URL and must not accept mutating operations,
- CLI `--host` targets the client API, not the peer API, unless explicitly documented for remote admin use.

This mirrors the etcd pattern of separating client URLs from peer URLs while avoiding an unnecessary distributed-database implementation inside BetterNAT.

## Agent API

Add a local control API separate from Prometheus.

Initial local transport:

```text
unix:///run/betternat/agent.sock
```

Optional future peer transport:

```text
https://<agent-private-ip>:9109
```

Prometheus remains:

```text
http://<agent-private-ip>:9108/metrics
```

The control API should use compact JSON endpoints:

```text
GET    /v1/status
GET    /v1/peers
GET    /v1/datapath
GET    /v1/failover
GET    /v1/doctor
GET    /v1/config
GET    /v1/handover
POST   /v1/handover
POST   /v1/handover/abort
POST   /v1/drain
DELETE /v1/drain
POST   /v1/refresh
```

Maintenance/debug endpoints should be explicit and not mixed into the hot status path:

```text
GET    /v1/healthz
GET    /v1/readyz
GET    /v1/debug/cache
GET    /v1/debug/coordination
GET    /v1/debug/handover
```

Rules:

- `/v1/healthz` checks daemon process health only,
- `/v1/readyz` checks whether the daemon has loaded config and initialized core workers,
- `/v1/debug/*` may expose deeper internal state and should require privileged local access,
- debug endpoints must redact credentials and signed URLs,
- debug endpoints are for support bundles and troubleshooting, not normal CLI status rendering.

`/v1/status` should return the fast cached view:

```json
{
  "gateway_id": "prod-egress",
  "ha_group_id": "prod-egress-us-west-2a",
  "local_instance_id": "i-local",
  "active_instance_id": "i-active",
  "route_target": "i-active",
  "public_ip": "52.24.117.43",
  "instances": [
    {
      "instance_id": "i-active",
      "role": "active",
      "version": "v0.1.0-alpha",
      "private_ip": "10.0.1.10",
      "metrics_fresh": true,
      "rx_mbps": 12.3,
      "tx_mbps": 1.2
    }
  ],
  "warnings": []
}
```

The API should distinguish:

- `cached`: fast local view,
- `fresh`: recently refreshed from peers/backend,
- `stale`: known but outside freshness budget,
- `unknown`: not observed yet.

### Daemon Services

The daemon should provide these services to the local CLI and to future peer agents.

| Service | API surface | Backing source | Freshness target | Blocks CLI? |
| --- | --- | --- | --- | --- |
| Local status | `/v1/status` | in-memory snapshot | 1-2 seconds | No |
| Fleet status | `/v1/status`, `/v1/peers` | registry cache + peer metrics cache | 5-10 seconds | No |
| Datapath status | `/v1/datapath` | local datapath collector | 1-5 seconds | No |
| HA/failover state | `/v1/failover` | HA supervisor reporter | every HA step | No |
| Doctor summary | `/v1/doctor` | cached local checks + optional refresh | 10-60 seconds | No by default |
| Config summary | `/v1/config` | loaded local config, redacted | on config load/reload | No |
| Refresh trigger | `/v1/refresh` | bounded background refresh | caller-selected timeout | Optional |
| Handover control | `/v1/handover` | HA supervisor + coordination backend | state-machine driven | Bounded |
| Drain control | `/v1/drain` | local daemon + coordination backend | state-machine driven | Bounded |

Daemon responsibilities:

- own all long-lived cloud/backend clients,
- own all peer discovery and peer metrics polling,
- expose cached state with explicit cache age,
- bound every slow dependency with timeouts,
- surface stale or partial state instead of making the CLI hang,
- own local control operations such as drain and handover,
- keep Prometheus serving independent from the control API,
- keep debug/maintenance endpoints separate from normal operator status endpoints.

CLI responsibilities:

- parse user intent,
- connect to the local daemon by default,
- render tables or JSON,
- return useful errors when the daemon is unavailable,
- offer explicit `--direct` mode for debug and bootstrap recovery,
- avoid owning long-lived AWS/DynamoDB/peer-metrics logic in the normal path.

The CLI should not:

- create a DynamoDB client for normal `status`,
- scrape every peer metrics endpoint for normal `status`,
- infer failover ownership from cloud state when the daemon is available,
- execute route/EIP mutations directly.

### API Response Rules

Every daemon response should include:

```json
{
  "schema_version": "v1",
  "generated_at": "2026-06-23T06:00:00Z",
  "cache": {
    "mode": "cached",
    "age_seconds": 1.2,
    "fresh": true
  },
  "warnings": []
}
```

Rules:

- `generated_at` is when the daemon assembled the response.
- `cache.age_seconds` is the age of the oldest required snapshot in the response.
- stale fields must remain visible, not silently omitted.
- warnings must be machine-readable enough for CLI rendering and support bundles.
- the daemon should prefer partial output with warnings over request failure.
- request failure is reserved for local daemon/API failures, authorization failures, malformed requests, or unsafe operation rejection.

### Endpoint Details

#### `GET /v1/status`

Purpose: default CLI status.

Must include:

- local instance id,
- active instance id from lease/cache,
- route target if known,
- public IP/EIP owner if known,
- all fresh peers,
- stale peers with `fresh=false` when useful,
- per-peer role, version, private IP, metrics freshness, and traffic summary.

Should not synchronously call:

- DynamoDB,
- EC2,
- Auto Scaling,
- peer metrics endpoints.

Optional query parameters:

```text
?refresh=false
?include_stale=true
```

If `refresh=true`, the daemon may trigger bounded refresh work before responding, but the default must remain cached.

#### `GET /v1/peers`

Purpose: service discovery and peer freshness debugging.

Must include:

- raw-ish registry-derived peer records,
- cache age,
- expiry time,
- last metrics scrape time,
- last control API probe result when peer API is enabled.

#### `GET /v1/datapath`

Purpose: local datapath health.

Must include:

- configured engine,
- ready state,
- last reconcile time,
- last reconcile error,
- last status sample time,
- traffic counter summary if available.

The daemon should never let a blocked datapath call block this endpoint. The collector should time out and mark the datapath snapshot stale or degraded.

#### `GET /v1/failover`

Purpose: HA and ownership view.

Must include:

- HA enabled,
- local HA state,
- lease owner,
- lease generation,
- seconds until lease expiry,
- route target match,
- public identity match,
- takeover counters,
- current handover/drain state when present.

#### `GET /v1/doctor`

Purpose: operator health summary.

Default behavior should use cached checks. A query parameter can request a bounded live refresh:

```text
?refresh=true&timeout=10s
```

Checks should be classified as:

- `ok`,
- `warning`,
- `critical`,
- `unknown`.

#### `POST /v1/refresh`

Purpose: ask the daemon to refresh selected caches.

Example request:

```json
{
  "targets": ["registry", "peer_metrics", "datapath", "cloud"],
  "timeout_seconds": 5
}
```

The daemon should return what was refreshed and what was skipped or timed out.

#### `POST /v1/drain`

Purpose: mark local node as unavailable for new planned ownership.

Drain should:

- publish `drain#<instance_id>`,
- keep forwarding traffic while active unless handover is requested,
- prevent this node from being selected as a planned handover target,
- remain TTL-bounded so a crashed drained node does not poison the group forever.

#### `POST /v1/handover`

Purpose: request proactive handover.

Example request:

```json
{
  "request_id": "8c5b28fc-6e6b-46d6-8f4d-8a2f2bcb0a5d",
  "target_instance_id": "auto",
  "reason": "systemd-stop",
  "deadline_seconds": 90,
  "allow_route_only": false
}
```

Rules:

- only the active owner may commit route/EIP changes,
- standby may accept prepare requests but must not mutate cloud ownership,
- standby must verify the requester is the current active lease owner before accepting prepare, commit, or abort requests,
- manual handover from a standby should be forwarded to the active owner when peer API exists, or recorded as a coordination request for the active owner to process,
- if `target_instance_id=auto`, active picks the best fresh standby.

Every mutating request should include `request_id`. If omitted, the daemon may generate one for local interactive use, but automated callers such as the provider should supply it.

Idempotency rules:

- same `request_id` and same payload returns the existing operation state,
- same `request_id` with different payload is rejected,
- different `request_id` while an operation is active returns conflict and the active operation,
- operation records store `request_id`, requester, reason, and payload hash.

### Request Routing

If the local daemon is standby and receives a local CLI handover request:

1. If peer API is enabled and active is known, forward the request to active's client/peer endpoint.
2. If peer API is unavailable, write `handover-request#<request_id>` to the coordination backend.
3. Active's daemon watcher may accept the request only if it still owns the lease.
4. If no active owner is known, return a clear error and do not try to mutate cloud state.

The CLI should show where the request landed:

```text
handover request recorded; waiting for active i-active to accept
```

or:

```text
handover forwarded to active i-active
```

Requests from a standby must never cause that standby to mutate route/EIP directly.

## Daemon Background Workers

The agent daemon should maintain state with independent bounded loops:

1. `registryPublisher`: publish local record to coordination backend.
2. `registryWatcher`: query peer records from coordination backend.
3. `peerMetricsCollector`: scrape peer metrics summaries.
4. `localDatapathCollector`: sample local datapath with timeouts.
5. `haSupervisor`: renew/acquire/transfer lease and reconcile route/EIP.
6. `terminationWatcher`: handle ASG lifecycle and Spot interruption events.
7. `controlServer`: serve local CLI API.

No collector should block the daemon API. Slow datapath or peer metrics calls should degrade freshness, not the CLI.

## Coordination Backend Records

Keep one provider-neutral table/namespace:

```text
PK = ha_group_id
SK = record_id
```

Existing records:

```text
lease
agent#<instance_id>
```

Add control records:

```text
handover#<generation>
drain#<instance_id>
message#<id>
upgrade#<id>
```

The coordination backend is a relay and durable state store. It is not the fast CLI serving path.

### Record Revisions And Watch-Like Loops

etcd clients commonly build local state from a list/watch pattern. BetterNAT should use the same shape conceptually even when the backend is DynamoDB, Redis, SQL, or another provider-specific service.

Every coordination record should include monotonic or comparable metadata:

```json
{
  "record_id": "agent#i-123",
  "record_type": "agent",
  "revision": 17,
  "updated_at": 1782195000,
  "expires_at": 1782195020
}
```

Backend requirements:

- conditional updates should increment `revision`,
- watchers should ignore older revisions when a newer local copy exists,
- TTL expiry should be treated as deletion for status/handover selection,
- records should include enough type information for forward-compatible parsing,
- unknown record types should be ignored by older agents unless explicitly required.

Daemon background loops should follow this pattern:

```text
initial list ha_group_id
  -> build local cache
  -> poll/query/watch changes from last observed revision when backend supports it
  -> expire stale local entries by expires_at
  -> expose cached view to CLI
```

DynamoDB does not provide an etcd-style watch API directly. The AWS implementation can start with bounded polling. A future backend may map the same internal watcher interface to:

- DynamoDB Streams,
- Redis keyspace notifications or streams,
- SQL change tables or polling,
- a real etcd watch API if etcd is used as the backend.

The CLI should not care which watcher implementation is used.

### Leases And TTLs

Use distinct lease concepts:

| Concept | Purpose | Authoritative? |
| --- | --- | --- |
| HA lease record `lease` | route/EIP mutation authority | Yes |
| Agent registry TTL | peer liveness/discovery freshness | No |
| Drain TTL | temporary scheduling exclusion | No |
| Handover TTL | cleanup of operation metadata | No |

Only the HA lease grants mutation authority.

Registry, drain, and handover TTLs are operational hints. They should expire stale records and prevent permanent poisoning, but they must not be interpreted as permission to mutate cloud state.

This follows the etcd distinction between leases for liveness and application-level correctness checks: the existence of a live key is useful, but the application must still check the correct owner/generation before acting.

### Version Compatibility

The daemon API should be explicitly versioned.

Rules:

- every API response includes `schema_version`,
- every daemon exposes `daemon_version`, `api_min_version`, and `api_max_version`,
- CLI checks API compatibility before rendering complex responses,
- incompatible CLI/daemon pairs fail with a clear message,
- unknown JSON fields are ignored by older CLIs,
- removing or changing field meaning requires an API version bump.

Suggested compatibility policy:

```text
CLI v0.1.x can speak daemon API v1.
Daemon API v1 may add optional fields.
Daemon API v1 must not remove fields or change their meaning.
```

`betternat version` should print:

```text
client:
  version: v0.1.0
  api_supported: v1
daemon:
  version: v0.1.0
  api_min: v1
  api_max: v1
  reachable: true
```

### Daemon Restart And Recovery

Daemon restart must be safe and explicit.

On startup:

1. load config and resolve local identity,
2. start local control socket,
3. initialize cache state as `unknown`,
4. read coordination records for the local HA group,
5. start registry publisher,
6. start registry watcher and peer metrics collector,
7. start HA supervisor,
8. if the local instance appears to own the lease, verify route/EIP before reporting `ACTIVE`.

Rules:

- do not report `ACTIVE` from stale local memory,
- do not infer active ownership from route/EIP alone,
- do not mutate route/EIP until the lease owner/generation is verified,
- existing registry records for the same instance may be overwritten by the new daemon process,
- stale handover records older than the current lease generation should be ignored,
- stale drain records should be honored until TTL expiry unless explicitly canceled by this instance.

The daemon API should expose startup state:

```json
{
  "daemon_state": "starting",
  "cache": {
    "mode": "warming",
    "fresh": false
  }
}
```

Status should become `fresh` only after required caches have completed at least one successful refresh or have failed with a recorded warning.

## Handover Model

Passive failover is still needed for hard crashes, but graceful events should use proactive handover.

Triggers:

- `systemd stop betternat-agent`,
- ASG lifecycle termination,
- Spot interruption,
- manual `betternat handover`,
- future rolling upgrade/drain workflow.

Target promise:

- pick a fresh standby,
- preflight standby readiness,
- switch route/EIP while the current active is still alive,
- transfer lease ownership,
- then stop the old active.

This minimizes the interruption window because the old active can perform the cloud mutations before it exits.

## Safe Handover Protocol

Lease safety remains mandatory. Registry freshness or peer API success must not grant route/EIP mutation authority.

Proposed AWS protocol:

1. Active owns lease generation `N`.
2. Active selects standby from fresh registry/cache.
3. Active asks standby to prepare:
   - datapath ready,
   - source/destination check disabled,
   - agent version compatible,
   - metrics/control API reachable.
4. Active writes `handover#N` with:
   - source instance,
   - target instance,
   - lease generation,
   - status `preparing`.
5. Standby acknowledges readiness through peer API or coordination record.
6. Active writes `handover#N` status `committing`.
7. Active, still fenced by lease `N`, performs cloud mutations:
   - associate shared EIP to standby when configured,
   - replace private route target to standby instance or ENI.
8. Active conditionally transfers lease to standby with generation `N+1`.
9. Standby observes lease owner self, verifies route/EIP, and becomes active.
10. Old active stops renewing and exits.

Recovery rules:

- If prepare fails, abort without route/EIP mutation.
- If route/EIP mutation fails, active keeps lease and remains active.
- If route/EIP succeeds but lease transfer fails, active must retry transfer or revert route/EIP before exiting.
- Standby must not mutate route/EIP until it owns the lease.
- All steps must be idempotent and generation-scoped.

This protocol is more complex than lease-expiry failover, but it is the right path for graceful shutdown and rolling upgrades.

### Handover State Machine

Use a generation-scoped handover record:

```text
PK = ha_group_id
SK = handover#<lease_generation>
```

Record fields:

```json
{
  "record_type": "handover",
  "handover_id": "prod-egress-us-west-2a#42",
  "source_instance_id": "i-active",
  "target_instance_id": "i-standby",
  "lease_generation": 42,
  "status": "preparing",
  "reason": "asg-lifecycle",
  "created_at": 1782195000,
  "updated_at": 1782195001,
  "expires_at": 1782195120,
  "deadline_at": 1782195090,
  "route_table_ids": ["rtb-..."],
  "destination_cidr": "0.0.0.0/0",
  "public_identity_mode": "shared_eip",
  "allocation_id": "eipalloc-...",
  "error": ""
}
```

States:

```text
none
  -> requested
  -> preparing
  -> prepared
  -> committing
  -> verifying
  -> transferring
  -> completed

requested/preparing/prepared/committing/verifying/transferring
  -> aborting
  -> aborted

committing/verifying/transferring
  -> recovering
  -> completed | aborted | failed_manual_intervention
```

State meanings:

| State | Owner | Meaning |
| --- | --- | --- |
| `requested` | CLI/operator/active | Handover requested but target not selected or not prepared. |
| `preparing` | active | Active selected target and is checking readiness. |
| `prepared` | standby | Target standby acknowledged it can accept ownership. |
| `committing` | active | Active is about to mutate EIP/route. Abort is no longer a simple no-op. |
| `verifying` | active | Active mutated EIP/route and is verifying target ownership. |
| `transferring` | active | Active is conditionally transferring lease to target. |
| `completed` | target | Target owns lease and route/EIP verification succeeded. |
| `aborting` | active | Active is undoing pre-commit or post-commit state. |
| `aborted` | active | Active remains owner or no ownership mutation happened. |
| `recovering` | active or target | A post-commit step failed and the system is converging to one safe owner. |
| `failed_manual_intervention` | daemon | Automatic recovery failed; passive lease fencing remains the last safety line. |

The record TTL must exceed the handover deadline long enough for support bundle collection, but it must not persist indefinitely.

### Target Selection

When `target_instance_id=auto`, active selects a standby using this order:

1. not local source instance,
2. not drained,
3. fresh registry record,
4. HA state is `STANDBY`,
5. datapath ready,
6. source/destination check disabled,
7. compatible major/minor runtime version,
8. metrics or control API reachable,
9. same AZ and HA group,
10. lowest observed cache age or deterministic instance-id tie break.

Reject targets that are:

- expired from registry,
- already active for another generation,
- in ASG terminating lifecycle,
- Spot interruption pending,
- route/EIP verification incompatible,
- not version-compatible for the handover protocol.

If no target exists:

- active keeps ownership,
- graceful shutdown falls back to shortening/releasing lease only if the external deadline forces exit,
- ASG lifecycle completion should be delayed until the configured lifecycle hook timeout budget is nearly exhausted.

### Detailed Commit Flow

This is the intended AWS flow for `route_target_type=instance` and `stable_egress_ip=true`.

```text
active(A), standby(B), lease generation=N

1. A reads current lease.
2. A verifies owner=A and generation=N.
3. A creates handover#N status=preparing target=B.
4. B observes prepare or receives peer prepare request.
5. B runs readiness checks:
   - agent healthy,
   - datapath ready,
   - local SNAT configured,
   - source/destination check disabled,
   - not drained,
   - not terminating,
   - can read current lease,
   - can reach coordination backend,
   - requester A is the current lease owner for generation N.
6. B writes ack status=prepared or responds to peer API.
7. A re-verifies lease owner=A generation=N.
8. A writes status=committing.
9. A associates shared EIP to B.
10. A replaces private default route target with B.
11. A verifies EIP owner=B and route target=B.
12. A conditionally transfers lease:
    - condition owner=A,
    - condition generation=N,
    - set owner=B,
    - set generation=N+1,
    - set expires_at=now+ttl.
13. B observes lease owner=B generation=N+1.
14. B verifies route/EIP target=B.
15. B reports ACTIVE.
16. A writes status=completed and exits or becomes drained standby.
```

For `stable_egress_ip=false`, skip EIP association and verify route only.

For future `route_target_type=network_interface`, route and EIP mutations should target B's ENI/private IP instead of instance id.

### Ordering Choice

The active should switch route/EIP before transferring the lease because:

- the active is the currently fenced mutator,
- route/EIP changes can be verified before ownership changes,
- standby must not mutate cloud ownership before it owns the lease,
- if route/EIP mutation fails, active can remain active without a split brain.

The dangerous window is after route/EIP moved to B but before lease transfer. During that window:

- A still owns the lease and must not exit,
- B forwards traffic but does not claim control ownership yet,
- A must either finish lease transfer or revert route/EIP to A.

This is why post-commit failure handling is required.

### Corner Cases And Required Behavior

| Case | Required behavior |
| --- | --- |
| No standby exists | Reject planned handover. Keep active. For forced termination, release/shorten lease near deadline and rely on passive replacement. |
| Standby registry stale | Do not select it. Keep active. |
| Standby datapath not ready | Abort before route/EIP mutation. Keep active. |
| Standby is drained | Do not select it unless user explicitly overrides with a future unsafe flag. |
| Standby receives Spot/ASG termination during prepare | Abort before commit and pick another target if deadline allows. |
| Standby receives prepare from non-active requester | Reject. Re-read lease, report suspected stale or unauthorized handover request, and do not change state. |
| Standby receives prepare with stale generation | Reject. Ignore if local lease generation is newer. |
| Standby receives commit without matching prepared record | Reject. Commit is advisory only; standby still must wait for lease owner self before reporting ACTIVE. |
| Active loses lease before commit | Abort immediately. Do not mutate route/EIP. |
| Active loses lease after route/EIP mutation | Enter recovering. Do not perform new mutations unless it still verifies ownership; rely on new owner/passive repair if lease is gone. |
| EIP association fails | Abort. Keep active and route unchanged. |
| Route replacement fails after EIP moved | Try to move EIP back to active. Keep lease on active. Mark handover aborted or manual intervention if revert fails. |
| EIP moved and route moved, lease transfer fails transiently | Retry transfer until deadline while active keeps renewing lease. |
| EIP moved and route moved, lease transfer condition fails | Re-read lease. If another owner exists, stop mutating and report recovering. If active still owns a newer generation, retry with the new generation only if safe. |
| Active process crashes after route/EIP moved but before lease transfer | Passive failover eventually repairs when lease expires. Standby B may already be route/EIP target but must wait for lease before control ownership. |
| Standby crashes after route/EIP moved but before lease transfer | Active must detect verification/health failure, revert route/EIP to active, keep lease, abort. |
| DynamoDB unavailable before commit | Abort. Keep active. |
| DynamoDB unavailable after route/EIP moved | Retry until deadline. If deadline nears, try route/EIP revert to active. If revert also fails, continue serving and do not complete external termination hook. |
| EC2 route/EIP API throttled | Retry with bounded backoff inside deadline. Keep lease on active. |
| ASG lifecycle deadline near | Stop starting new handover attempts. If already post-commit, prefer completing or reverting before completing lifecycle hook. |
| CLI submits duplicate handover | Return current handover state. Do not create competing records. |
| Two handovers requested concurrently | Only one active generation-scoped handover may exist. Later requests get conflict/current state. |
| Manual abort before commit | Delete/mark handover aborted. Keep active. |
| Manual abort after commit | Treat as recovery request; active must verify lease before attempting revert. |
| Version mismatch | Reject unless compatibility matrix explicitly allows it. |
| Clock skew | Use backend conditional expressions and TTLs for safety; never rely only on local wall clock for lease ownership. |
| Stale completed handover record | Ignore if lease generation is newer than record generation. Expire by TTL. |
| Coordination record deleted mid-flow | Active re-creates or aborts based on local state before commit; after commit, recovery rules apply. |
| Peer API unreachable | Fall back to coordination record polling if deadline allows. |
| Metrics stale but control API ready | Allow prepare only if datapath readiness can be verified through control API or local registry freshness policy. |
| Stable EIP disabled | Route-only handover; public IP may change as documented. |
| Desired capacity is 1 | Planned handover unavailable. CLI/provider must warn. |

### Invariants

These must hold in every implementation and test:

- At most one non-expired lease owner exists for a HA group.
- Only the current lease owner may intentionally mutate route/EIP.
- Peer prepare/ack does not grant mutation authority.
- A standby must verify that a handover requester is the current lease owner before accepting prepare or commit messages.
- A standby must verify lease generation matches the handover record before acknowledging readiness.
- Handover records are advisory; lease record is authoritative.
- Route/EIP verification must happen before marking handover completed.
- If completion cannot be proven, the system must prefer `recovering` over false success.
- Passive lease-expiry failover remains available even if proactive handover fails.

### Time Budgets

Default targets for alpha/prototype:

| Step | Target |
| --- | ---: |
| Target selection | < 1s from cache |
| Standby prepare through coordination polling | < 5s |
| Standby prepare through future peer API | < 500ms |
| EIP association | < 5s typical, bounded by deadline |
| Route replacement | < 2s typical, bounded by deadline |
| Lease transfer | < 1s typical |
| End-to-end handover | < 10s coordination-only, lower with peer API |

The daemon should expose actual phase durations in logs, metrics, and `/v1/handover`.

### Metrics And Logs

Prometheus metrics:

```text
betternat_daemon_api_requests_total{endpoint,method,result}
betternat_daemon_api_request_duration_seconds{endpoint,method}
betternat_daemon_cache_age_seconds{cache}
betternat_daemon_cache_fresh{cache}
betternat_peer_registry_age_seconds{peer}
betternat_peer_metrics_scrape_errors_total{peer}
betternat_peer_control_probe_errors_total{peer}
betternat_handover_attempts_total{reason,result}
betternat_handover_phase_duration_seconds{phase}
betternat_handover_in_progress
betternat_handover_last_generation
betternat_handover_recoveries_total{result}
betternat_drain_state
```

Structured log fields:

```text
handover_id
source_instance_id
target_instance_id
lease_generation
phase
result
reason
error
duration_ms
```

## Direct Peer Notification

Direct peer notification can make handover faster, but it must be authenticated and optional.

Minimum safe path:

- use coordination records for durable handover intent,
- have standby watch/poll at a short interval,
- add direct peer API as an optimization.

Future peer API:

```text
POST /v1/handover/prepare
POST /v1/handover/commit
POST /v1/handover/abort
```

Security requirements:

- private subnet only,
- security group restricted to gateway appliances,
- request authentication,
- no public exposure,
- never accept route/EIP mutation commands from peers without local lease verification,
- never accept handover prepare, commit, or abort requests unless the requester matches the current lease owner and handover generation.

The bootstrap/provider layer should generate or distribute the peer API credential. For alpha, avoid unauthenticated TCP control endpoints.

Hard implementation rule:

- remote peer API defaults to disabled,
- enabling remote peer API requires authentication configuration,
- unauthenticated peer write endpoints must not ship,
- coordination-backed handover must work without remote peer API.

Authentication answers "is this request from a BetterNAT peer?" It does not answer "is this peer allowed to coordinate this handover?" The receiver must still read the current lease and confirm requester ownership before changing local handover state.

## ENI Target Option

Current route target type is `instance`.

For faster and cleaner handover, add a future `network_interface` target mode:

```text
route target = standby primary ENI
EIP association = standby primary ENI/private IP
```

This should be evaluated separately because it changes provider inputs, rollback behavior, and route verification.

## CLI UX

Default:

```sh
betternat status
```

Behavior:

- connect to local agent socket,
- render cached daemon status,
- return quickly.

Flags:

```text
--host <url>       daemon endpoint
--direct           bypass daemon and use config/backend directly
--refresh          ask daemon to refresh before returning, bounded by timeout
--timeout <dur>    client request timeout
```

New commands:

```sh
betternat handover status
betternat handover start --to <instance-id>
betternat drain start
betternat drain cancel
```

Do not make users specify `/etc/betternat/agent.json` for normal local operation.

### CLI Command Contract

Default command behavior should mirror Docker-style client/daemon semantics.

Global flags:

```text
--host <url>          daemon endpoint, default unix:///run/betternat/agent.sock
--output, -o          table | json
--timeout <duration>  client-side request timeout
--direct              bypass daemon and use config/backend code path
--config <path>       config path for --direct mode or bootstrap recovery
```

Environment:

```text
BETTERNAT_HOST=unix:///run/betternat/agent.sock
BETTERNAT_CONFIG=/etc/betternat/agent.json
```

Normal operator commands:

| Command | Default path | API | Notes |
| --- | --- | --- | --- |
| `betternat status` | daemon | `GET /v1/status` | fast fleet summary |
| `betternat peers` | daemon | `GET /v1/peers` | peer registry/cache detail |
| `betternat datapath status` | daemon | `GET /v1/datapath` | local datapath snapshot |
| `betternat datapath ready` | daemon | `GET /v1/datapath` | exits nonzero when not ready |
| `betternat failover status` | daemon | `GET /v1/failover` | HA ownership state |
| `betternat doctor` | daemon | `GET /v1/doctor` | cached checks by default |
| `betternat doctor --live` | daemon | `GET /v1/doctor?refresh=true` | bounded live refresh |
| `betternat refresh` | daemon | `POST /v1/refresh` | manually refresh daemon caches |
| `betternat version` | local binary | none | prints CLI version and daemon version when reachable |
| `betternat cost estimate` | local binary | none | pure calculator |

Control commands:

| Command | API | Safety |
| --- | --- | --- |
| `betternat drain start` | `POST /v1/drain` | local daemon marks self drained |
| `betternat drain cancel` | `DELETE /v1/drain` | local daemon removes drain record |
| `betternat handover status` | `GET /v1/handover` | read-only |
| `betternat handover start --to auto` | `POST /v1/handover` | active selects target |
| `betternat handover start --to <instance-id>` | `POST /v1/handover` | active validates target |
| `betternat handover abort` | `POST /v1/handover/abort` | only safe before commit or by current owner |

Debug and recovery commands:

| Command | Path | Notes |
| --- | --- | --- |
| `betternat status --direct --config /etc/betternat/agent.json` | current direct path | for daemon-down debugging |
| `betternat doctor --direct --live --config /etc/betternat/agent.json` | current direct path | can still be slower |
| `betternat daemon ping` | daemon | verifies socket/API reachability |
| `betternat daemon logs` | local systemd/journal helper | optional future convenience |

The CLI should render a clear daemon-unavailable error:

```text
betternat-agent daemon is not reachable at unix:///run/betternat/agent.sock
Try:
  sudo systemctl status betternat-agent
  sudo betternat status --direct --config /etc/betternat/agent.json
```

### Daemon Authorization

Local Unix socket:

- default path: `/run/betternat/agent.sock`,
- owner: `root`,
- group: `betternat`,
- mode: `0660`,
- CLI usually runs through `sudo` in alpha.

Remote peer API, if enabled later:

- disabled by default until authentication exists,
- listen on private interface only,
- security group allows gateway-to-gateway only,
- mTLS or signed request token required,
- no unauthenticated write endpoints.

Read-only local commands can be allowed to the `betternat` group. Mutating commands such as drain and handover should require root or a separate privileged group once the operator model is defined.

## Provider And Daemon Boundary

The Terraform provider should not become a second runtime control plane.

Provider responsibilities for future safe rolling upgrades:

- render desired infrastructure and runtime config,
- create launch template or AMI updates,
- add capacity or replace standby instances,
- wait for new standby readiness through daemon status,
- request handover through the active daemon API,
- verify route/EIP/lease after daemon-reported completion,
- continue replacing old active only after ownership has moved.

Provider must not:

- directly mutate route/EIP as part of a planned handover,
- bypass lease fencing,
- infer the active owner from ASG lifecycle state alone,
- perform a mutating direct fallback if daemon API is unavailable,
- silently downgrade from safe rolling upgrade to disruptive replacement.

If the daemon API is unavailable:

- read-only provider checks may report degraded state,
- safe rolling upgrade should stop with a clear diagnostic,
- provider may offer an explicit user-approved replacement path,
- provider-owned infrastructure reconciliation such as IAM policy/table updates can still run if it does not touch runtime ownership.

This boundary keeps all runtime ownership decisions inside `betternat-agent`.

## Audit And Authorization

The daemon should record all mutating requests.

Audit fields:

```text
request_id
api_version
operation
requester_kind
requester_uid
requester_gid
requester_peer_instance_id
source_instance_id
target_instance_id
lease_generation
reason
accepted
rejected_reason
created_at
completed_at
```

For Unix sockets, collect peer credentials where the OS supports it.

For peer API, record:

- authenticated peer identity,
- remote address,
- presented certificate or token id,
- claimed instance id,
- lease-owner verification result.

Mutating requests should produce structured logs and coordination records. Support bundles should include recent audit entries with sensitive token material redacted.

## Direct Fallback Policy

Direct mode is for diagnostics and bootstrap recovery.

Allowed direct fallback:

- `status --direct`,
- `doctor --direct`,
- `datapath status --direct`,
- `failover status --direct`,
- support bundle collection.

Disallowed direct fallback:

- handover,
- drain,
- route mutation,
- EIP mutation,
- lease transfer.

If the daemon is unavailable for mutating commands, the CLI should fail closed:

```text
cannot run handover because betternat-agent daemon is unavailable
```

This prevents an emergency/debug path from becoming an unsafe second control plane.

## Implementation Readiness Checklist

Before implementation starts, the following decisions should be treated as fixed for v1 of the daemon API:

- the default CLI path is the local Unix socket, not direct config/backend access,
- Prometheus metrics remains read-only and separate from the control API,
- daemon API responses include schema version, generation time, cache metadata, and warnings,
- stale cached data is visible in table and JSON output,
- mutating operations require daemon availability and fail closed if the daemon is unavailable,
- direct mode is read-only for status, doctor, datapath, failover, and support collection,
- route/EIP mutation authority comes only from the current HA lease owner and generation,
- peer authentication is necessary but not sufficient; receivers must still verify current lease owner and generation,
- peer API stays disabled until authenticated transport and requester authorization are implemented,
- coordination-backed handover works without direct peer TCP,
- older agents ignore unknown coordination record types,
- provider upgrades must be additive and rolling-safe unless the operator explicitly approves a disruptive replacement path.

Open implementation choices that can be decided during Phase 1 without changing the architecture:

- exact JSON field names for traffic summaries,
- table rendering layout,
- whether `betternat version` connects to the daemon by default or only when `--daemon` is specified,
- whether local read-only socket access is root-only in alpha or available to a `betternat` group immediately.

## AWS Validation Scope

The current AWS alpha environment can validate the coordination-backed registry baseline, but it cannot validate daemon API or proactive handover until those phases are implemented.

Current environment validation can prove:

- agents publish registry records into the coordination table,
- CLI direct status can discover peers from the coordination table,
- reduced runtime IAM still supports lease, registry, route, EIP, Prometheus, and source-IP checks,
- rolling update with the current lease-release path keeps the group converged,
- old lease-table migration does not affect the active coordination-table path.

Current environment validation cannot yet prove:

- `betternat status` is local-socket-only,
- CLI status latency is independent of DynamoDB and peer metrics fanout,
- daemon cache freshness is rendered correctly,
- proactive handover completes faster than lease-expiry takeover,
- peer request authentication and active-owner verification work,
- forced process stop during each handover phase recovers correctly.

Those items belong to the Phase 1-5 acceptance tests below.

## Implementation Plan

### Phase 1: Local Daemon API

- Add `internal/agentapi` with request/response structs.
- Add Unix socket server to `betternat-agent`.
- Maintain an in-memory status snapshot in the daemon.
- Make `betternat status` call the daemon socket by default.
- Keep `--direct` fallback using the existing code.
- Add systemd/runtime setup for `/run/betternat`.
- Add CLI daemon-unavailable error with direct-mode recovery hint.
- Add unit tests for:
  - socket server,
  - socket permissions/path selection,
  - CLI fallback,
  - stale snapshot rendering.

Acceptance:

- `betternat status` returns without DynamoDB or metrics fanout from the CLI process.
- CLI status remains useful when peer metrics are stale.
- Direct mode still works for debugging.
- `betternat version` can show both CLI and daemon versions when daemon is reachable.

### Phase 2: Cached Peer State

- Move registry query and peer metrics scrape from CLI into agent background workers.
- Add peer freshness metrics.
- Add daemon API fields for cache age and stale peers.
- Remove normal CLI dependency on coordination backend clients.
- Add explicit stale peer rendering.
- Add support bundle fields for cache age and peer scrape errors.

Acceptance:

- CLI status latency is local-socket latency plus JSON rendering.
- Backend and peer scrape errors show as warnings in cached status, not CLI hangs.
- restarting the daemon rebuilds peer cache from the coordination backend without CLI behavior changes.

### Phase 3: Coordination Messages

- Add record types:
  - `handover#<generation>`,
  - `drain#<instance_id>`,
  - `message#<id>`.
- Add conditional-write helpers and tests.
- Add daemon background watcher for control records.
- Add TTL cleanup rules.
- Add duplicate/conflict handling for same-generation records.

Acceptance:

- Active and standby can coordinate handover intent through the backend without direct peer TCP.
- Records are generation-scoped and TTL-bounded.
- duplicate handover requests return the current state instead of creating competing records.

### Phase 4: Proactive Handover

- Add `lease.Transfer` operation with conditional owner/generation checks.
- Add standby readiness preflight.
- Add active-controlled route/EIP switch to target standby.
- Integrate with:
  - systemd stop,
  - ASG lifecycle termination,
  - Spot interruption,
  - manual CLI handover.
- Add recovery paths for post-commit failures.
- Add metrics and structured logs for every phase.

Acceptance:

- Graceful handover completes faster than lease-expiry takeover.
- If any handover step fails, active either remains active or reverts route/EIP before exit.
- Hard-crash passive failover still works.
- forced active termination during each handover phase has a documented and tested recovery behavior.
- no test permits a standby to mutate route/EIP before it owns the lease.

### Phase 5: Peer API Optimization

- Add authenticated private peer API.
- Use peer API for immediate prepare/ack messages.
- Keep coordination backend as durable fallback.
- Add feature flag/config for peer API.
- Add auth key generation/distribution in provider/bootstrap or AMI path.

Acceptance:

- Direct peer notification reduces handover prepare latency.
- Disabling peer API still leaves coordination-backed handover functional.
- unauthenticated peer write requests are rejected.

## Test Matrix

Local unit/fake tests:

- daemon socket status response,
- CLI daemon default path,
- CLI direct fallback,
- cached stale rendering,
- coordination handover record conditional writes,
- lease transfer success and condition failure,
- handover state-machine transitions,
- duplicate handover request conflict,
- route/EIP commit failure recovery,
- standby cannot mutate without lease.

AWS integration tests:

- daemon-backed `betternat status` latency on gateway,
- rolling update with proactive handover,
- ASG lifecycle active termination with handover,
- Spot interruption simulation where possible,
- forced active process stop at each phase:
  - before prepare,
  - after prepare,
  - after EIP association,
  - after route replacement,
  - before lease transfer,
  - after lease transfer.
- route/EIP/lease consistency after each forced failure,
- fallback passive failover after active crash.

## Risks

- Handover can create split-brain if lease transfer and route/EIP mutation are not generation-fenced.
- A peer API without authentication is worse than slow failover.
- CLI direct fallback must not become the default again.
- Status cache must make staleness visible; fast but stale output is dangerous if not labeled.

## Near-Term Recommendation

Before alpha final:

1. Add local daemon API for `status`.
2. Move CLI status fanout into the agent cache.
3. Keep proactive handover as a documented P1 unless alpha timeline allows a focused AWS-only implementation.

After alpha:

1. Implement coordination-backed handover.
2. Add direct authenticated peer API.
3. Evaluate ENI route target mode.

## References

The BetterNAT daemon should borrow API and operational patterns from etcd, without copying etcd's Raft/storage internals.

Useful etcd references:

- etcd configuration separates listen and advertise URLs for client and peer traffic:
  - https://etcd.io/docs/v3.5/op-guide/configuration/
  - https://etcd.io/docs/v3.3/op-guide/clustering/
- etcd transport security covers both client-to-server and peer traffic:
  - https://etcd.io/docs/v3.6/op-guide/security/
- etcd role-based access control separates authentication from authorization:
  - https://etcd.io/docs/v3.3/op-guide/authentication/
- etcd Watch API lets clients build local state from backend changes and revisions:
  - https://etcd.io/docs/v3.7/learning/api/
  - https://etcd.io/docs/v3.4/learning/api_guarantees/
- etcd Lease API models TTL/liveness and keepalive semantics:
  - https://etcd.io/docs/v3.2/learning/api/
  - https://etcd.io/docs/v3.5/dev-guide/api_reference_v3/
- etcd runtime reconfiguration treats advertised peer URLs as membership-sensitive state:
  - https://etcd.io/docs/v3.3/op-guide/runtime-configuration/

Mapping to BetterNAT:

- etcd client URL -> BetterNAT local daemon API.
- etcd peer URL -> future authenticated BetterNAT peer API.
- etcd watch -> BetterNAT daemon backend watcher/poller feeding local cache.
- etcd lease -> BetterNAT HA lease plus separate non-authoritative TTL records.
- etcd auth/TLS -> BetterNAT local socket permissions and future mTLS/signed peer requests.
- etcd runtime reconfiguration caution -> BetterNAT should treat peer advertise URL changes as coordination-sensitive and avoid accepting stale peer endpoints blindly.
