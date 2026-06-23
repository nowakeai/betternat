# BetterNAT Failure Modes And Limitations

Date: 2026-06-23

## Purpose

This document describes expected BetterNAT behavior during common failures.

It is intended for users evaluating whether the first free/open-source release fits their workloads.

## Core Semantics

BetterNAT v0 provides active/standby HA for new egress connections.

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
- non-stable mode may change public source IP.

In the current alpha handover path, route and EIP ownership are both verified
after mutation, but AWS control-plane convergence is not perfectly atomic. A
2026-06-23 handover timing test observed a short timeout window and one
successful private-client request leaving through a non-shared instance public
IP before traffic returned to the shared EIP. Users that require strict egress
identity during every failover sample should wait for the production hardening
that gates route cutover on shared-EIP ownership or removes per-node public IPs
from the production AMI path.

## Summary Table

| Failure | Expected Behavior | User Impact | Primary Signals | Recovery |
| --- | --- | --- | --- | --- |
| Active EC2 terminates | Standby acquires lease, claims EIP if configured, replaces route | Short new-connection outage; active flows reset | lease generation changes, route/EIP owner changes, failover metrics | Automatic if standby is healthy |
| Active ASG scale-in or Spot interruption notice | Agent observes IMDS termination signal, releases local lease, and completes the ASG lifecycle action | Shorter new-connection outage than waiting for lease TTL when notice arrives in time | termination event log, lease release, lifecycle action completion, route/EIP owner changes | Automatic if standby is healthy |
| Standby EC2 terminates | Active continues serving; ASG launches replacement | HA degraded until replacement ready | ASG unhealthy, no standby, stale standby metrics | Automatic ASG repair |
| LoxiLB process restarts | Agent reconciles missing datapath rules | Possible datapath interruption on affected instance | `betternat_datapath_ready=0`, LoxiLB command errors | Automatic reconcile if process returns |
| Agent process stops on active | Graceful SIGTERM releases the local lease; crashes rely on lease expiry | Outage depends on HA profile, stop path, and detection timing | stale HA status, lease expiry or release, takeover attempt | Automatic if standby is healthy |
| DynamoDB temporarily unavailable | Current active may keep datapath but lease renew can fail | Failover decisions may be delayed or conservative | lease renew errors | Automatic after DDB recovers |
| `ReplaceRoute` fails | Lease may move but traffic still points to old target | New traffic may fail or continue through old node | route target mismatch | Agent retry/future rollback needed |
| EIP association fails | Route may move but stable public IP not attached | Stable mode can leak different public IP or lose egress | public identity mismatch, outbound probe mismatch | Agent retry; manual AWS check |
| ASG cannot launch replacement | Active may continue, but HA remains degraded | No standby protection | ASG activity failure | Fix capacity/quota/AMI/subnet/IAM |
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
- failover and stable-EIP timing may be similar because both depend on lease and route convergence, but stable mode has additional EIP correctness requirements.

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

Current alpha policy:

- capacity-only Terraform updates may be in-place,
- non-capacity updates require replacement unless explicitly supported,
- blue/green replacement is safer than in-place mutation.

Expected future production policy:

1. update standby first,
2. verify readiness,
3. planned failover,
4. replace old active,
5. keep rollback path.

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

## First-Release Limitations

- AWS only.
- Single-AZ HA group scope.
- No active-active NAT.
- No active connection preservation.
- No central server.
- No automatic multi-account inventory.
- No built-in support bundle command yet.
- `doctor` live cloud checks are not fully wired yet.
- Large multi-TB benchmark is not part of first-release validation.
