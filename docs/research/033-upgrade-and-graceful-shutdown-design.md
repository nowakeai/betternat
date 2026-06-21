# Upgrade And Graceful Shutdown Design

Date: 2026-06-21

## Question

How should BetterNAT upgrade gateway appliances, and how should an appliance shut down gracefully without causing unnecessary route/EIP downtime?

## Short Answer

Terraform can and should drive upgrades, but raw Terraform/ASG primitives are not enough by themselves.

BetterNAT must distinguish:

- **capacity updates**: safe to do in-place today,
- **software/config/AMI upgrades**: require BetterNAT-aware rolling upgrade,
- **runtime failover**: owned by `betternat-agent`,
- **graceful shutdown**: active owner should step down before the instance disappears.

Current state:

- `min_size`, `desired_capacity`, and `max_size` can be updated in-place.
- AMI, agent binary, LoxiLB binary, datapath, route topology, stable EIP mode, and HA timing changes currently require replacement.
- Automatic HA failover works for owner termination, but production upgrade UX needs planned failover and graceful shutdown support.

Recommended target:

```text
Terraform provider changes desired version
  -> creates new launch template version
  -> replaces standby first
  -> verifies new standby readiness
  -> asks active to step down / planned failover
  -> replaces old active after it becomes standby
  -> verifies owner, route, EIP, metrics
```

## Upgrade Types

### 1. Capacity Update

Examples:

```hcl
desired_capacity = 3
max_size         = 3
```

Current behavior:

- provider updates ASG capacity in-place,
- runtime HA ownership is preserved,
- ASG may terminate any instance during scale-in unless protected.

Status:

- implemented,
- tested in AWS low-cost supplemental run,
- still needs production guardrails around scale-in and active owner protection.

### 2. AMI Upgrade

Examples:

```hcl
ami_channel = "stable"
ami_id      = "ami-..."
```

Target behavior:

- new launch template version,
- ASG instance refresh,
- replace standby before active,
- planned failover before replacing active,
- rollback to previous launch template version if readiness fails.

Current behavior:

- replacement required,
- no first-class rolling upgrade flow yet.

### 3. Agent / LoxiLB Binary Upgrade

Development path:

```hcl
agent_binary_url   = "s3://..."
loxicmd_binary_url = "s3://..."
```

Production path:

- binaries baked into AMI,
- no presigned URL dependency during boot.

Target behavior:

- same as AMI upgrade,
- binary/config hash must be visible in state and metrics.

### 4. HA Timing / Datapath / Route Topology Change

Examples:

```hcl
ha_profile          = "stable"
stable_egress_ip    = false
route_target_type   = "instance"
private_cidrs       = ["10.0.0.0/8"]
```

These are not ordinary rolling upgrades. They can change runtime behavior and safety properties.

Target behavior:

- require replacement or a specialized migration plan,
- never silently mutate route/EIP semantics in-place,
- document expected disruption.

## Terraform Provider Role

The provider should expose a simple user-facing upgrade policy:

```hcl
upgrade_strategy = "safe_rolling" # none | safe_rolling | replace
```

Future richer form:

```hcl
upgrade_policy = {
  strategy            = "rolling"
  replace_active_last = true
  planned_failover    = true
  max_unavailable     = 1
  wait_for_datapath   = true
  rollback_on_failure = true
}
```

Provider responsibilities:

- decide whether a change is capacity-only, rolling-upgrade-safe, or replacement-required,
- create/update launch template versions,
- call ASG instance refresh or orchestrate instance termination manually,
- avoid terminating the active owner first,
- check agent readiness before moving ownership,
- surface upgrade status in Terraform state,
- fail with clear diagnostics instead of partial silent upgrades.

Provider should not:

- perform uncoordinated `destroy/create` for active gateways by default,
- modify active private routes except through BetterNAT-safe control paths,
- assume ASG knows which instance is active.

## Agent Role

The agent must own runtime safety:

- maintain lease,
- know whether it is active or standby,
- expose readiness,
- perform planned stepdown,
- release or shorten lease during graceful shutdown,
- prevent stale owners from mutating route/EIP after losing lease.

Required future agent commands/API:

```sh
betternat status --config /etc/betternat/agent.json --json
betternat failover --planned --to <instance-id-or-auto>
betternat stepdown --reason upgrade
betternat datapath ready --config /etc/betternat/agent.json
```

Possible local HTTP endpoints:

```text
GET  /readyz
GET  /status
POST /stepdown
POST /planned-failover
```

For alpha, SSM commands are acceptable for tests. For production, a local authenticated control socket or restricted loopback endpoint is better.

## Rolling Upgrade Algorithm

Target single-AZ HA group with desired capacity 2:

```text
Initial:
  A = active old version
  B = standby old version

Step 1: update launch template to new version
Step 2: replace standby B -> B'
Step 3: wait for B' readiness:
  - instance running
  - SSM online or health signal present
  - betternat-agent active service
  - LoxiLB ready
  - datapath SNAT rules reconciled
  - HA state = STANDBY
  - metrics fresh
Step 4: planned failover A -> B'
Step 5: verify:
  - lease owner = B'
  - route target = B'
  - EIP target = B' when stable mode
  - client probe succeeds
Step 6: replace old A -> A'
Step 7: wait for A' readiness as standby
Step 8: complete upgrade
```

For desired capacity 3+:

- replace all standbys first,
- planned failover to a new-version standby,
- replace the old active last,
- ensure at least one new-version standby remains ready.

For desired capacity 1:

- no warm standby exists,
- rolling upgrade cannot be no-downtime,
- provider must warn or require `allow_disruptive_upgrade = true`.

## Graceful Shutdown

Graceful shutdown matters for:

- planned upgrades,
- Terraform destroy,
- ASG scale-in,
- EC2 stop/terminate,
- Spot interruption,
- kernel/package reboot,
- operator maintenance.

### Active Owner Shutdown

If the local node is active, it should not simply disappear and wait for lease expiry.

Preferred sequence:

```text
1. mark local node draining
2. stop accepting planned ownership
3. confirm at least one standby is ready
4. optionally trigger planned failover to ready standby
5. verify route/EIP moved
6. release lease or shorten lease
7. stop datapath service
8. exit
```

If no standby is ready:

- do not release ownership early,
- log a critical warning,
- keep forwarding as long as possible,
- let normal failure detection handle the final outage.

### Standby Shutdown

If the local node is standby:

```text
1. mark local node draining
2. stop attempting takeover
3. keep datapath unchanged
4. exit
```

No route/EIP mutation is required.

## systemd Integration

Production AMI should include systemd hooks:

```ini
[Service]
ExecStart=/usr/local/bin/betternat-agent --config /etc/betternat/agent.json
ExecStop=/usr/local/bin/betternat stepdown --reason systemd-stop --config /etc/betternat/agent.json
TimeoutStopSec=45s
KillSignal=SIGTERM
Restart=always
RestartSec=2s
```

Important:

- `ExecStop` must be bounded.
- If planned stepdown cannot complete within the timeout, the service should exit and rely on lease expiry.
- `TimeoutStopSec` must be longer than expected planned failover but shorter than unsafe indefinite shutdown.

## ASG Lifecycle Hooks

ASG lifecycle hooks can improve graceful termination:

```text
autoscaling:EC2_INSTANCE_TERMINATING
  -> notify instance or SSM/Lambda coordinator
  -> call stepdown if active
  -> wait for completion
  -> complete lifecycle action
```

Pros:

- gives active owner time to step down before termination,
- reduces outage compared with waiting for lease TTL,
- useful for scale-in and instance refresh.

Cons:

- adds AWS resources and IAM permissions,
- lifecycle hook timeout/failure handling must be correct,
- Lambda/SSM coordinator may become another moving part.

Recommendation:

- alpha: do not require ASG lifecycle hooks,
- production: add optional lifecycle hook support after planned stepdown exists.

## Spot Interruption

Spot interruption gives a two-minute warning when AWS reclaims capacity.

If `use_spot=true`, agent should watch IMDS interruption notice:

```text
http://169.254.169.254/latest/meta-data/spot/instance-action
```

On interruption notice:

```text
if active:
  trigger planned failover
else:
  mark draining
```

Alpha:

- Spot is acceptable for low-cost tests.
- Production examples should default `use_spot=false`.

Production:

- Spot support should include interruption handling before being recommended.

## Terraform Destroy

Destroy is not upgrade, but graceful shutdown affects destroy safety.

Current behavior:

- provider can roll private routes back using stored rollback metadata,
- provider cleans BetterNAT resources.

Target behavior:

```text
1. read current owner
2. restore private routes to rollback targets
3. tell active agent to step down / stop mutating routes
4. release EIP if provider-owned
5. delete ASG/launch template/IAM/DDB/security group
```

Important invariant:

- after provider begins route rollback, agents must not fight the rollback by replacing routes back to BetterNAT owner.

Potential design:

- write a `gateway_draining` marker to the lease/control table,
- agents seeing the marker stop route/EIP reconciliation,
- provider then restores routes and destroys resources.

## State And Observability

Upgrade status should be visible.

Provider computed fields:

```hcl
upgrade_status
upgrade_generation
active_version
standby_versions
```

Agent metrics:

```text
betternat_build_info{version,commit,datapath_engine}
betternat_upgrade_state{node}
betternat_draining{node}
betternat_planned_failover_total{result}
betternat_graceful_shutdown_total{result}
```

Logs:

- planned failover start/end,
- stepdown start/end,
- old/new owner,
- route/EIP verification result,
- reason: upgrade, scale-in, spot-interruption, systemd-stop, destroy.

## Safety Rules

1. Never terminate the only ready owner unless disruptive upgrade is explicitly allowed.
2. Never move route/EIP to a node that is not datapath-ready.
3. Never let a stale owner mutate route/EIP after losing lease generation.
4. Never preempt a healthy active owner just because a higher-priority node returns.
5. Never treat ASG health alone as BetterNAT readiness.
6. Never hide disruptive replacement behind a normal Terraform update.

## Alpha Policy

For alpha/private preview:

- capacity updates are supported,
- AMI/agent/config upgrades require replacement,
- docs must clearly state replacement can be disruptive,
- recommended safe upgrade is blue/green:
  1. deploy new BetterNAT gateway in a test route table,
  2. verify egress,
  3. migrate selected private route tables,
  4. destroy old gateway after rollback window.

## Production Policy

Before production-ready release:

- implement planned failover,
- implement graceful active stepdown,
- implement standby-first rolling upgrade,
- implement or decide against ASG lifecycle hooks,
- test AMI rolling upgrade in AWS,
- test stable and non-stable egress modes,
- test desired capacity 1, 2, and 3 behavior,
- document rollback and failed-upgrade recovery.

## Test Matrix

Minimum AWS upgrade tests:

| ID | Scenario | Expected |
|----|----------|----------|
| UPG-001 | capacity scale-out 2 -> 3 | no owner disruption |
| UPG-002 | capacity scale-in 3 -> 2, active not selected | no failover |
| UPG-003 | capacity scale-in 3 -> 2, active selected | planned or automatic failover, client recovers |
| UPG-004 | replace standby AMI | standby returns ready |
| UPG-005 | planned failover to upgraded standby | route/EIP moves, client recovers |
| UPG-006 | replace old active after failover | replacement joins standby |
| UPG-007 | failed new standby readiness | abort upgrade, old active remains owner |
| UPG-008 | systemd stop active | graceful stepdown if standby ready |
| UPG-009 | systemd stop standby | no route/EIP mutation |
| UPG-010 | Spot interruption active | planned failover before termination when possible |
| UPG-011 | Terraform destroy active gateway | route rollback not fought by agents |

## Open Design Questions

- Should planned failover be initiated by Terraform provider, CLI, or agent-to-agent control plane?
- Should provider use ASG Instance Refresh or manually terminate selected instances?
- How should provider identify old-version standby versus active owner robustly?
- Should graceful shutdown release lease or shorten lease after route/EIP verification?
- Should lifecycle hooks be required for production or optional?
- Should route rollback use a DDB `gateway_draining` marker?
- Should upgrade state live only in Terraform state, or also in DynamoDB?

## Current Recommendation

Do not promise seamless software/AMI upgrades in alpha.

For alpha:

- support capacity updates,
- require replacement for risky changes,
- document blue/green upgrade guidance.

For production:

- make `safe_rolling` the default upgrade strategy once planned failover and graceful shutdown are implemented and tested.
