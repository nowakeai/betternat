# Automatic HA Implementation Checklist

Date: 2026-06-20

## Question

What must be implemented before BetterNAT can run a complete automatic HA test in AWS?

## Short Answer

Complete automatic HA is no longer blocked by the basic runtime control loop. The agent now has a decentralized active/standby HA supervisor.

The current code has the required primitives:

- DynamoDB lease acquire/renew/release/current,
- AWS `AssociateAddress`, `ReplaceRoute`, route/EIP describe,
- a one-shot HA activation controller,
- a long-running HA supervisor in `betternat-agent`,
- agent datapath reconciliation,
- ASG-based capacity repair.

The AWS low-cost supplemental runs now prove the complete single-AZ automatic HA path for the current ASG-pool design:

- stable EIP mode: baseline egress, active-agent stop failover, owner-termination failover, ASG repair, and capacity-only scale-out/in,
- non-stable egress mode: baseline egress, owner-termination failover, ASG repair, and replacement joining as standby.

What is still missing before calling the feature production-ready is hardening and packaging:

- retry/backoff policy for transient AWS and DynamoDB failures,
- LoxiLB datapath readiness helper,
- AMI-based bootstrap proof instead of cloud-init-only testing,
- longer soak tests.

## Target Test

The implementation is ready for complete automatic HA testing when this AWS scenario can run without Terraform or manual AWS API calls after the failure trigger:

1. Apply the supplemental AWS fixture with ASG desired capacity 2.
2. Verify owner and candidate are both running `betternat-agent`.
3. Verify private client egress works through the owner.
4. Terminate the owner instance, or stop the owner agent.
5. Candidate detects the expired lease.
6. Candidate acquires the lease.
7. Candidate reconciles local datapath.
8. Candidate moves shared EIP if stable-IP mode is enabled.
9. Candidate replaces private default route.
10. Candidate verifies route/EIP ownership.
11. Private client new flows recover.
12. ASG launches a replacement node.
13. Replacement node joins as standby.

## Current State

### Implemented

- `internal/lease`
  - in-memory lease manager for tests,
  - DynamoDB lease manager with conditional acquire/renew/release/current,
  - generation fencing.
- `internal/cloud/aws`
  - `ReplaceRoute`,
  - `AssociateAddress`,
  - route describe,
  - EIP describe,
  - local instance ID resolution through IMDS,
  - source/destination check disable.
- `internal/ha`
  - one-shot `Controller.Activate`,
  - route target rendering from config,
  - activation verification.
- `internal/agent`
  - config load,
  - local instance preparation,
  - datapath reconcile loop,
  - metrics endpoint,
  - source/destination check disable on startup,
  - continuous HA supervisor wiring when `ha.enabled=true`,
  - `shared_eip` allocation ID auto-resolution from BetterNAT EIP tags.
- `internal/ha`
  - long-running supervisor step loop,
  - active lease renewal,
  - standby lease polling,
  - expired lease takeover,
  - renew fencing demotion tests.
- Terraform provider
  - ASG appliance pool,
  - initial owner selection,
  - initial EIP association and route replacement,
  - runtime IAM policy generation,
  - SSM managed access on appliance role,
  - supplemental fixture with private SSM client.

### Missing

- retry/backoff policy for transient AWS and DynamoDB failures,
- appliance datapath readiness helper for AWS tests.

Partially closed after the latest implementation pass:

- the supervisor now reports HA state into an in-memory status reporter,
- `/metrics` can expose HA state, lease owner match, lease expiry, takeover counters, lease renew errors, route target match, and public identity match,
- `/metrics` now exposes `betternat_ha_status_age_seconds` and `betternat_ha_status_stale`,
- stale HA snapshots are downgraded to `STALE`, `betternat_active=0`, and owner/route/EIP match gauges set to false,
- system logs include one structured-ish `betternat_ha_step` line per supervisor step.
- appliance readiness in HA mode must be checked with read-only signals only: `/metrics`, service status, LoxiLB container status, journal logs, AWS route/EIP state, and DynamoDB lease state. Do not use `betternat-agent --once` for HA readiness because it can participate in control-plane reconciliation.

### AWS Proof So Far

Early run `bnat-20260620153304` produced useful but incomplete evidence:

- stopping the active agent caused automatic standby takeover and private client recovery,
- terminating a non-owner candidate caused ASG capacity repair without moving the owner,
- terminating the owner eventually recovered through the ASG replacement, but the pre-existing standby did not take over.

That initial result proved the supervisor path was real, but automatic HA was not yet closed.

Later combined runs closed the low-cost AWS proof:

- `bnat-20260620182614` with stable egress IP enabled:
  - private client baseline used fixed EIP `35.85.131.212`,
  - active-agent stop moved route/EIP to the standby,
  - owner termination moved route/EIP to the pre-existing standby with about 12 seconds of client-visible outage,
  - ASG launched a replacement and the HA state stayed stable,
  - capacity-only scale-out and scale-in completed in place.
- `bnat-20260620191841` with stable egress IP disabled:
  - private client baseline used the active appliance public IP `54.245.164.82`,
  - owner termination moved route to the pre-existing standby,
  - client-visible outage was about 12 seconds,
  - egress IP changed to standby public IP `34.210.117.59`, as expected,
  - ASG launched a replacement that joined as standby,
  - metrics stayed fresh with `betternat_ha_status_stale=0`.

The remaining gap is no longer "can complete automatic HA work"; it is production hardening, AMI packaging, and reducing transient edge cases such as a brief non-EIP sample during stable-mode active-agent failure.

## Engineering Checklist

### 1. Config And Defaults

Status: partially implemented.

Add runtime defaults without making users configure every interval:

- `ha.lease.ttl_seconds`
  - default: 10 to 15 seconds for early AWS tests,
  - production default can be raised after timing data.
- `ha.lease.renew_interval_seconds`
  - default: about one third of TTL.
- standby poll interval
  - can initially reuse renew interval,
  - should later become an explicit config.
- takeover timeout
  - bounded context for `Activate`.
- activation retry backoff
  - avoid hot-looping failed AWS API calls.

Acceptance:

- missing optional HA intervals get sane defaults,
- invalid values fail config validation,
- tests cover defaulting and invalid combinations.

### 2. HA Supervisor

Status: implemented for the minimal v0 local/unit-test path.

Introduce a long-running supervisor, probably in `internal/ha`, with a small interface boundary:

```go
type Supervisor struct {
    Cloud    cloud.Provider
    Lease    lease.Manager
    Datapath datapath.Engine
    Probe    ProbeRunner
}
```

The supervisor should own state transitions:

```text
INIT -> STANDBY -> TAKING_OVER -> ACTIVE
ACTIVE -> DEGRADED -> STANDBY or ERROR
```

Minimal v0 behavior:

- on startup, reconcile datapath,
- read current lease,
- if lease belongs to self and is valid, renew and become active,
- if lease is missing or expired, attempt takeover,
- otherwise stay standby and keep datapath ready,
- active owner renews lease periodically,
- standby nodes poll current lease periodically,
- on lease expiry, standby attempts takeover,
- if renew fails due to fencing, active demotes immediately.

Acceptance:

- deterministic unit tests with fake clock,
- no AWS calls happen before lease acquisition,
- stale owner cannot continue mutating cloud state after renew fencing failure,
- only one candidate wins acquisition in tests.

### 3. Wire Supervisor Into Agent

Status: implemented.

Change `agent.runContinuous` so HA-enabled configs run the HA supervisor instead of only doing datapath reconcile.

Rules:

- `ha.enabled=false`
  - keep current behavior: reconcile datapath periodically.
- `ha.enabled=true`
  - build lease manager,
  - build cloud provider,
  - start HA supervisor,
  - keep metrics endpoint running.

Acceptance:

- existing non-HA agent tests still pass,
- new HA agent tests prove supervisor is started,
- `--once` remains a local datapath inspection mode and does not start HA takeover unless explicitly added later.

### 4. Runtime Constructors

Status: implemented for AWS + DynamoDB.

Add agent-side constructors:

- lease backend:
  - `dynamodb` -> `dynamodblease.New`,
  - future local/test backends can stay internal.
- cloud provider:
  - `aws` -> `awscloud.New`.

Validation:

- `ha.enabled=true` requires:
  - `cloud=aws`,
  - `region`,
  - `local.instance_id` resolved,
  - `ha.lease.backend=dynamodb`,
  - `ha.lease.table`,
  - `ha.lease.key` or `ha_group_id`,
  - route failover config,
  - EIP allocation ID when `public_identity.mode=shared_eip`.

Acceptance:

- startup fails fast on missing HA runtime config,
- errors are explicit enough for `doctor` and cloud-init logs.

### 5. Activation Ordering

Status: mostly implemented.

The existing `Controller.Activate` has the right broad order. Keep this invariant:

1. acquire fenced lease,
2. reconcile local datapath,
3. move public identity,
4. replace private routes,
5. verify cloud state,
6. run outbound probe,
7. re-read lease generation.

Add or preserve these constraints:

- if EIP association succeeds but route replacement fails, report degraded active state,
- do not release lease automatically after partial cloud mutation unless rollback is explicitly implemented,
- after activation failure, next loop should re-read actual cloud state before retrying.

Acceptance:

- unit tests for failed EIP move,
- unit tests for failed route replacement,
- unit tests for lease generation changing during activation.

### 6. Metrics And Status

Status: implemented and proven in the low-cost AWS combined runs.

Expose enough signals to debug AWS tests:

- `betternat_ha_state{gateway_id,ha_group_id,node}`,
- `betternat_ha_status_age_seconds`,
- `betternat_ha_status_stale`,
- `betternat_lease_generation`,
- `betternat_lease_owner_match`,
- `betternat_lease_seconds_until_expiry`,
- `betternat_lease_renew_errors_total`,
- `betternat_takeover_attempts_total`,
- `betternat_takeover_success_total`,
- `betternat_route_target_match`,
- `betternat_public_identity_match`.

The first implementation can use coarse gauges/counters if the metrics package is not ready for labels.

Acceptance:

- `/metrics` shows whether a node is active or standby,
- stale supervisor snapshots do not report a node as active,
- failed takeover is visible without SSHing into the process logs.

### 7. AWS Fixture Hooks

Status: implemented for SSM access.

The current AWS fixture can create a private SSM client, but complete HA testing also needs appliance-side observability.

Add one of:

- SSM managed access on appliance instances in the supplemental fixture, or
- SSH key support for the supplemental fixture, or
- a minimal local status endpoint reachable from the VPC/private client.

For the lowest-friction test pass, SSM on appliances is acceptable as a test fixture feature. It should not silently become required in production.

Acceptance:

- can read agent logs/status on owner and candidate,
- can stop `betternat-agent` on owner without terminating the instance,
- can check LoxiLB status on owner and candidate.

### 8. LoxiLB Readiness Helper

Status: partially implemented.

Add a helper command or status API that answers:

- is LoxiLB container/process running,
- are expected SNAT rules present,
- is local datapath ready for takeover.

Acceptance:

- AWS runbook can block takeover timing until candidate datapath is ready,
- `SUP-007` can restart LoxiLB and prove agent reconciliation.

Current helper path:

- on appliances, use read-only `/metrics` plus AWS route/EIP/DynamoDB state,
- on development hosts or future AMIs with the user-facing CLI installed, use `betternat datapath ready --config <path>`.

### 9. AWS Test Readiness

Status: proven for the current single-AZ ASG-pool design.

`SUP-018` is complete for low-cost AWS testing because all of these are now true:

- two ASG nodes run the HA supervisor,
- active node renews lease,
- standby node observes lease,
- killing active agent causes standby takeover,
- terminating active instance causes standby takeover,
- private client new flows recover,
- ASG replacement joins as standby,
- route/EIP ownership matches the new active node,
- no manual `AssociateAddress` or `ReplaceRoute` is used during the test.

`SUP-019` is also complete for low-cost AWS testing:

- terminating an owner caused ASG to launch a replacement,
- the replacement joined as standby,
- the existing active owner was not disturbed during the post-repair observation window.

## Suggested Implementation Order

1. Build `ha.Supervisor` with fake lease/cloud/datapath tests.
2. Add agent constructors for DynamoDB lease and AWS cloud provider.
3. Wire HA supervisor into `agent.runContinuous`.
4. Add HA state metrics. Done and proven in AWS with stale-state gauges.
5. Add appliance access/status hooks to the supplemental AWS fixture.
6. Add LoxiLB readiness/restart helper.
7. Run local unit tests and provider validate.
8. Run AWS `SUP-018` with agent stop first. Done.
9. Run AWS `SUP-018` with owner instance termination. Done.
10. Run AWS `SUP-019` capacity repair after takeover. Done.

## Non-Goals For This Step

- multi-AZ failover,
- ENI target failover,
- existing long-lived TCP preservation,
- high-throughput or high-volume benchmark,
- EKS pod attribution.

These are useful later, but they should not block the first complete automatic HA test.
