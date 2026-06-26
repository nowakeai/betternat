# BetterNAT Failure Modes

Date: 2026-06-23

## Purpose

This document describes expected BetterNAT behavior during common failures.

It is intended for users evaluating whether BetterNAT fits their workloads. Use
it as a risk catalog. For step-by-step operations, use:

- [Operations Guide](OPERATIONS_GUIDE.md) for normal incident triage,
- [Rollback Guide](ROLLBACK_GUIDE.md) for route restore and destroy rollback,
- [Observability Guide](OBSERVABILITY_GUIDE.md) for metrics, alerts, and PromQL,
- [Upgrade And Replacement Guide](UPGRADE_REPLACEMENT_GUIDE.md) for replacement
  planning,
- [Limitations](../reference/LIMITATIONS.md) for release-scope boundaries.

## Core Semantics

BetterNAT provides active/standby HA for new egress connections.

It does not promise:

- AWS NAT Gateway equivalent SLA,
- zero packet loss,
- active connection preservation,
- active-active NAT,
- transparent state migration between nodes.

When failover happens:

- new connections should recover after standby takeover,
- existing flows may reset,
- stable EIP mode should converge new connections back to the shared public EIP,
- non-stable mode may change public source IP,
- GCP stable public identity handover prioritizes route movement and outbound
  connectivity before static public IP convergence.

In the current handover path, route and EIP ownership are both verified after
mutation, but AWS control-plane convergence is not perfectly atomic. Stable EIP
mode has a longer control-plane path than route-only non-stable mode because it
must also move and verify public identity. Treat retained validation timings as
environment-specific evidence, not SLAs.

## Summary Table

| Failure | Expected Behavior | User Impact | Primary Signals | Recovery |
| --- | --- | --- | --- | --- |
| Active EC2 terminates | Standby acquires lease, claims EIP if configured, replaces route | Short new-connection outage; active flows reset | lease generation changes, route/EIP owner changes, failover metrics | Automatic if standby is healthy |
| Active ASG scale-in or Spot interruption notice | Agent observes IMDS termination signal and attempts proactive handover before completing the ASG lifecycle action | Shorter new-connection outage when proactive handover completes; otherwise standby recovers through fenced lease takeover | termination event log, durable handover record, lease generation change, route/EIP owner changes | Automatic if standby is healthy |
| Standby EC2 terminates | Active continues serving; ASG launches replacement | HA degraded until replacement ready | ASG unhealthy, no standby, stale standby metrics | Automatic ASG repair |
| LoxiLB process restarts | Agent reconciles missing datapath rules | Possible datapath interruption on affected instance | `betternat_datapath_ready=0`, LoxiLB command errors | Automatic reconcile if process returns |
| Agent process stops on active | Graceful SIGTERM releases the local lease; crashes rely on lease expiry | Outage depends on HA profile, stop path, and detection timing | stale HA status, lease expiry or release, takeover attempt | Automatic if standby is healthy |
| DynamoDB temporarily unavailable | Current active may keep datapath but lease renew can fail | Failover decisions may be delayed or conservative | lease renew errors | Automatic after DDB recovers |
| `ReplaceRoute` fails | Lease may move but traffic still points to old target | New traffic may fail or continue through old node | route target mismatch | Agent retry; use rollback guide if route remains wrong |
| EIP association fails | Route may move but stable public IP not attached | Stable mode can leak different public IP or lose egress | public identity mismatch, outbound probe mismatch | Agent retry; manual AWS check |
| ASG cannot launch replacement | Active may continue, but HA remains degraded | No standby protection | ASG activity failure | Fix capacity/quota/AMI/subnet/IAM |
| GCP Firestore unavailable | Active cannot safely renew or verify HA lease | Availability may degrade; split-brain should be avoided | lease read/renew errors, status degraded | Restore Firestore/API/IAM access |
| GCP route mutation fails | Lease may remain fenced but route target does not move | Private clients may continue through old owner or lose egress | route target mismatch, Compute API errors | Agent retry; use rollback guide if route remains wrong |
| GCP stable address handover slow or fails | Route can move before static public IP convergence | Temporary source-IP change or short outage while address converges | public identity mismatch, address user mismatch | Agent retry; verify Private Google Access and IAM |
| GCP MIG cannot repair capacity | Active may continue, but HA remains degraded | No standby protection | MIG target/actual mismatch | Fix quota, image, subnet, IAM, or health policy |
| Terraform destroy interrupted | Some resources may remain | Cost leak or route drift | residual resource scan | Rerun destroy or manual cleanup |
| Bad AMI/agent rollout | New instances may fail readiness | HA degraded or failover failure | service logs, datapath readiness, ASG health | Roll back AMI/config |

## Active EC2 Failure

Scenario:

- current active node is terminated or becomes unreachable.

Expected:

1. Active stops renewing lease.
2. Standby observes expired or acquirable lease.
3. Standby reconciles local datapath.
4. Standby associates shared EIP if stable mode is enabled.
5. Standby replaces private route table target.
6. Standby verifies route/EIP ownership.
7. Standby becomes active.
8. ASG launches replacement instance.

Impact:

- active flows may reset,
- new flows recover after takeover,
- measured outage depends on HA profile and AWS API timing.

Low-cost AWS supplemental tests observed about 12 seconds for owner termination under tested conditions. This is evidence, not a product SLA.

AWS SDK calls use a bounded retry policy for transient errors and throttling:
up to four attempts with retry backoff capped at three seconds. HA controller
step deadlines still bound total reconciliation time.

## ASG Termination Or Spot Interruption

Scenario:

- Auto Scaling moves the active gateway into terminating lifecycle state,
- or EC2 Spot interruption metadata appears on the active gateway.

Expected:

1. Agent observes IMDS termination metadata.
2. Agent starts a durable handover operation when the coordination registry is
   configured.
3. Active attempts to prepare a healthy standby peer.
4. Active moves shared EIP and private route ownership to the target.
5. Active transfers the fenced lease.
6. Agent completes the ASG lifecycle action.

Fallback:

- if the terminating active cannot complete proactive handover before shutdown
  or AWS API timeout, the standby should still acquire the lease after expiry
  and repair route/EIP ownership through the normal takeover path.

Validation note:

- validation covered both passive fenced takeover and proactive ASG
  lifecycle handover paths. Some tests converged through passive takeover after
  a failed proactive record; later validation completed the proactive lifecycle
  handover with no failed client probe samples. Treat the measurements as
  evidence from retained test environments, not as SLAs.

Signals:

```text
betternat handover history --limit 20
betternat status
betternat_lease_generation
betternat_route_target_match
betternat_public_identity_match
```

Operator response lives in the [Operations Guide failed lifecycle handover
section](OPERATIONS_GUIDE.md#failed-asg-lifecycle-handover-record). If private
egress is down and the previous route target is known, use the [Rollback Guide
emergency restore path](ROLLBACK_GUIDE.md#emergency-route-restore).

## Standby EC2 Failure

Scenario:

- standby instance is terminated while active remains healthy.

Expected:

- active continues serving traffic,
- ASG launches replacement,
- replacement boots agent and becomes standby,
- HA returns to healthy state.

Impact:

- no immediate egress outage,
- reduced redundancy until replacement is ready.

Alert:

- no standby ready,
- ASG desired capacity not healthy,
- stale metrics from former standby.

## Agent Stop Or Hang

Scenario:

- `betternat-agent` exits or hangs on active.

Expected:

- if the process receives graceful SIGTERM/systemd stop, it releases its currently owned HA lease before exit,
- if IMDS reports a Spot interruption or ASG target termination state, the agent releases its currently owned HA lease and completes the ASG lifecycle action,
- if the process crashes or cannot run shutdown logic, standby eventually takes over after lease expiry,
- if the process is still alive but unable to update status, stale HA metrics should prevent false-active reporting.

Impact:

- outage can be shorter on graceful stop, but crashes can last until lease expiry or explicit failure detection,
- active flows may reset after route/EIP moves.

Mitigations:

- default HA timing,
- systemd restart policy,
- lease TTL/renew interval tuning,
- future explicit health/failure fast path.

## LoxiLB Restart Or Rule Loss

Scenario:

- LoxiLB restarts or loses runtime firewall/SNAT rules.

Expected:

- agent reconciliation loop detects missing rules,
- rules are recreated from BetterNAT config.

Impact:

- traffic through that instance may fail until rules are restored,
- standby should remain ready if its datapath is healthy.

Signals:

```text
betternat_datapath_ready
betternat_loxilb_rule_present
betternat_loxilb_rule_packets_total
```

## DynamoDB Lease Failure

Scenario:

- DynamoDB API is slow, unavailable, throttled, or IAM denies lease writes.

Expected:

- active lease renew errors increase,
- standby should not take over without valid fencing,
- split-brain must be avoided even if availability is temporarily reduced.

Impact:

- current active may keep forwarding if datapath and route remain in place,
- failover can be delayed,
- recovery depends on DDB/API returning.

Mitigations:

- retry/backoff,
- DynamoDB capacity/on-demand mode,
- least-privilege IAM validation,
- alerts on renew errors and lease expiry.

## GCP Firestore Or Compute API Failure

Scenario:

- Firestore API is unavailable or denied,
- Compute route or address APIs are unavailable, throttled, or denied,
- gateway subnet lacks a reliable private path to Google APIs during stable
  public identity handover.

Expected:

- active gateway avoids claiming healthy active state if it cannot verify the
  lease or route ownership,
- standby does not take over without a valid Firestore fence,
- route and public identity repair retry after transient API errors,
- split-brain prevention takes priority over optimistic failover.

Impact:

- private-client egress can degrade until API access recovers or a rollback is
  performed,
- stable public identity can temporarily mismatch while route ownership has
  already moved,
- active flows may reset.

Signals:

```text
sudo betternat status
sudo betternat doctor --live
sudo betternat handover history --limit 20
betternat_lease_renew_errors_total
betternat_route_target_match
betternat_public_identity_match
```

Operator response:

1. Verify Firestore and Compute APIs are enabled and reachable.
2. Verify runtime service account permissions from the IAM reference.
3. Verify the gateway subnet has Private Google Access when stable public
   identity is enabled.
4. Check GCP route target and regional address user.
5. Use the rollback guide if the route remains wrong and private egress is down.

## GCP MIG Capacity Failure

Scenario:

- the zonal MIG cannot launch or repair a gateway instance.

Expected:

- an existing active gateway can continue serving,
- HA remains degraded until a standby is healthy,
- Terraform/provider state may still show the intended gateway group, but MIG
  actual capacity is below target.

Impact:

- no immediate outage if the active gateway is healthy,
- failover protection is reduced or absent.

Signals:

```sh
gcloud compute instance-groups managed describe <mig-name> --zone <zone>
sudo betternat status
```

Operator response:

1. Check quota, image, subnet, service account, and firewall errors in MIG
   events.
2. Fix the underlying GCP capacity or IAM issue.
3. Wait for a healthy standby before planned handover or maintenance.

## AWS Route Replacement Failure

Scenario:

- `ec2:ReplaceRoute` fails or is slow.

Expected:

- agent should report route mismatch,
- standby should not claim full healthy active status until route target verifies.

Impact:

- private subnet traffic may continue to old owner or fail,
- if old owner is dead, egress remains down until route updates.

Signals:

```text
betternat_route_target_match
betternat_takeover_attempts_total
betternat_takeover_success_total
```

## EIP Association Failure

Scenario:

- shared EIP cannot be associated to the new active node.

Expected:

- stable mode verification fails,
- public identity mismatch is reported.

Impact:

- new egress may use wrong public IP if route moved before EIP is attached,
- or egress may fail depending on instance public IP and routing.

Stable EIP mode should treat this as a serious failure. Users who depend on allowlisted egress IPs should alert on any mismatch.

Signal:

```text
betternat_public_identity_match
```

## Non-Stable Egress Mode

Scenario:

- `stable_egress_ip=false`.

Expected:

- route failover moves private egress to another node,
- public source IP can change to standby's public IP.

Impact:

- cheaper/simpler,
- not suitable for destinations that allowlist one egress IP,
- route-only handover can be materially faster than stable EIP handover because
  it does not need EIP reassociation or public-identity verification.

Validation note:

- retained 2026-06-24 validation showed non-stable route-only handover can be
  materially faster than stable EIP handover in the tested environment.
- stable mode converges back to the shared EIP, but strict "every successful
  sample always returns only the shared EIP" semantics remain future hardening
  for the `cloud_init` path when per-node public IPv4 is enabled.
- see [Limitations](../reference/LIMITATIONS.md#bootstrap-semantics) for the
  stable-EIP/bootstrap caveat.

## ASG Replacement Failure

Scenario:

- ASG cannot launch a replacement instance.

Causes:

- insufficient capacity,
- Spot interruption/capacity issue,
- bad launch template,
- missing AMI,
- IAM instance profile problem,
- subnet IP exhaustion,
- service quota.

Expected:

- active may continue serving,
- HA group remains degraded.

Impact:

- no standby or reduced standby count,
- next active failure may cause outage.

## Terraform Apply Failure

Scenario:

- Terraform apply fails after some resources are created.

Expected:

- Terraform state should contain created resources when possible,
- rerun apply after fixing the issue,
- use destroy for cleanup if deployment is not needed.

Risks:

- private route may be partially changed,
- EIP/DDB/ASG may exist,
- bootstrap may fail after infrastructure exists.

Mitigation:

- use disposable test VPC for first install,
- keep rollback metadata,
- inspect Terraform plan before apply,
- do not manually mutate resources unless recovering carefully.

## Terraform Destroy Failure

Scenario:

- destroy is interrupted or fails.

Expected:

- rerun destroy,
- perform residual scan,
- manually clean only after identifying ownership tags and route impact.

Check for residual:

- VPC resources,
- EIP,
- ENI,
- EBS volume,
- ASG,
- Launch Template,
- DynamoDB table,
- IAM role/profile,
- security groups.

## Upgrade Failure

Scenario:

- new AMI, agent, or config fails readiness.

Current policy:

- capacity-only Terraform updates may be in-place,
- non-capacity updates require replacement unless explicitly supported,
- blue/green replacement is safer than in-place mutation.

Use the [Upgrade And Replacement Guide](UPGRADE_REPLACEMENT_GUIDE.md) for the
supported replacement model and future rolling-upgrade direction.

## User Responsibility

Users must monitor:

- active/standby health,
- ASG health,
- route/EIP match,
- lease renew errors,
- datapath readiness,
- egress probe,
- AWS resource costs.

Users must also understand:

- self-managed nodes require operational ownership,
- EC2 instance type and capacity affect throughput,
- AWS API availability affects failover,
- BetterNAT does not remove normal AWS data-transfer charges.

## Release Limitations

Use [Limitations](../reference/LIMITATIONS.md) as the authoritative release
scope document. The failure-mode summary here should not duplicate the complete
limitations list.

Operationally important reminders:

- use `betternat support bundle` to collect local redacted diagnostics when
  opening an issue or sharing evidence,
- `doctor --live` is node-local; use `betternat status`, Prometheus, and AWS
  state checks for fleet-wide review,
- BetterNAT is self-managed infrastructure and does not publish an AWS NAT
  Gateway equivalent SLA.
