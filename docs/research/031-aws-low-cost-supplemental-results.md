# AWS Low-Cost Supplemental Results

Date: 2026-06-20

## Summary

This run validated the ASG-based BetterNAT appliance topology with the new agent supervisor path in a short-lived AWS test VPC.

Run:

```text
Run ID: bnat-20260620153304
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Capacity: desired=2
Market: Spot
Stable egress IP: enabled
Observed EIP: 44.230.174.94
```

High-volume traffic was intentionally not tested. All workload checks used tiny HTTP probes.

## Results

| ID | Result | Notes |
|----|--------|-------|
| SUP-014 | Pass | Terraform apply created isolated VPC, ASG appliances, private SSM client, EIP, route, DynamoDB lease table, IAM, and security groups. Destroy completed. |
| SUP-015 | Pass | Terminating the non-owner candidate caused ASG to launch a replacement. Owner route/EIP stayed stable. |
| SUP-018-A | Pass | Stopping the active agent caused standby takeover. Route/EIP moved to the standby and private client egress recovered. |
| SUP-018-B | Partial | Terminating the owner eventually recovered, but the pre-existing standby did not take over. The new ASG replacement became active after a longer gap. |
| SUP-019 | Partial | ASG capacity repair worked after owner loss, but the ownership transfer path needs investigation because recovery was by replacement, not warm standby. |
| SUP-016 | Blocked | Scale-out/in plan was unsafe because the fixture also managed the same private default route with an `aws_route`. |

## Baseline

Terraform output initially selected one owner, but the runtime agent later reconciled ownership to another instance. This proved the active ownership reconciliation path can repair startup drift between provider-selected owner and lease/cloud state.

Final baseline checks before failure testing:

- ASG had two `InService` and `Healthy` appliances.
- Route and EIP converged to the active appliance.
- Private client egress succeeded through the shared EIP.
- Both appliance instances were reachable through SSM.
- Both appliance instances had source/destination check disabled.

Private client probe:

```text
2026-06-20T15:49:15.348Z
44.230.174.94
HTTP/2 200
```

## Issues Found And Fixed

### Appliance Security Group Lacked Private Subnet Ingress

The provider-created appliance security group originally allowed no ingress from the private subnet. That prevented the private SSM client from reaching the internet through the appliance even though the route target was correct.

Fix:

- install plans now carry private CIDRs,
- AWS install applies appliance ingress from private CIDRs,
- egress-all is ensured,
- tests cover the generated security group rules.

The live test was manually unblocked by adding ingress from `10.88.0.0/16`; the code was then fixed for future runs.

### LoxiLB Firewall JSON Parse Race

Some early agent restarts logged:

```text
parse loxilb firewall json: invalid character 'E' looking for beginning of value
```

The systemd restart loop recovered, but this is a product rough edge. The agent should classify LoxiLB-not-ready output separately from malformed JSON and retry with clearer logs.

### Terraform Route Ownership Conflict

`SUP-016` was not run because the Terraform plan attempted to update a standalone `aws_route` back to the fixture's initial IGW target after runtime HA had moved the route.

Impact:

- scale-out/in cannot be safely tested with the current fixture shape,
- users should not have Terraform and the BetterNAT runtime both managing the same active default route target.

Required fix from this run:

- remove the conflicting standalone `aws_route` from the supplemental fixture, or
- add lifecycle/drift handling so Terraform does not try to restore the active route target during HA operation.

Follow-up status:

- the supplemental fixture now uses a one-shot `terraform_data` local-exec bootstrap route instead of a managed `aws_route`,
- `terraform validate` passes with the local BetterNAT provider dev override,
- `SUP-016` still needs a real AWS rerun to prove scale-out/in behavior after runtime route drift.

## Manual Stable-IP Failover Baseline

The test stopped both agents and manually moved EIP/route between appliances to preserve a control-plane baseline.

Observed result:

- AWS route/EIP describe converged after manual API calls,
- private client did not immediately recover while both agents were stopped,
- restarting agents restored the path.

Result: partial/fail as a product datapath baseline.

Interpretation:

- BetterNAT should not rely on manual API convergence as a success signal.
- Candidate datapath readiness must be checked before timing failover.
- Agent-driven reconciliation is required for a meaningful product result.

## Automatic Agent-Stop Takeover

Owner before trigger:

```text
i-0abdee0efff331971
```

Standby:

```text
i-07a864fe7c6c3cb53
```

Trigger:

```text
2026-06-20T16:05:39.386Z stop betternat-agent on owner
```

Observed:

```text
2026-06-20T16:05:47 route/eip still on i-0abdee0efff331971
2026-06-20T16:05:55 route/eip moved to i-07a864fe7c6c3cb53
```

Private client probe after takeover:

```text
2026-06-20T16:07:25.770Z
44.230.174.94
HTTP/2 200
```

Result: pass.

Approximate observed takeover window from trigger to route/EIP movement: about 16 seconds with the current TTL/poll settings and coarse polling.

## Automatic Owner-Termination Recovery

Current owner at trigger:

```text
i-0abdee0efff331971
```

Trigger:

```text
2026-06-20T16:09:25Z terminate owner instance
```

Observed:

```text
2026-06-20T16:09:31 route/eip still on terminated owner
2026-06-20T16:09:49 eip none, route still old
2026-06-20T16:10:00 route none, eip none
2026-06-20T16:10:30 ASG replacement i-07df85cfac6b19f9f appeared
```

Later diagnostic state:

```text
route target: i-07df85cfac6b19f9f
EIP target: i-07df85cfac6b19f9f
lease owner: i-07df85cfac6b19f9f
```

Private client probe after recovery:

```text
2026-06-20T16:12:58.997Z
44.230.174.94
HTTP/2 200
```

Result: partial.

The system recovered, but not through the intended fast warm-standby path. The new replacement became active instead. This is not acceptable as the final HA behavior because it turns a fast failover path into a boot-time repair path.

Follow-up:

- add explicit supervisor state logs and metrics,
- prove why the existing standby did not acquire the lease,
- add a readiness/status command that shows lease owner, local role, route match, EIP match, and LoxiLB datapath state.

Follow-up status:

- supervisor state logs and metrics have been added for the next run,
- HA appliance readiness must use read-only checks only; `betternat-agent --once` is not safe as a HA readiness probe because it can run reconciliation,
- `betternat datapath ready` is available for local/AMI CLI use,
- the owner-termination scenario still needs a real AWS rerun.

## Follow-up Run: bnat-20260620170030

This run validated the new standby behavior and exposed two more product bugs before destructive HA timing could start.

Passed:

- baseline Terraform apply completed,
- ASG reached two healthy appliances,
- active route and EIP ownership converged,
- private client egress worked through the BetterNAT EIP,
- standby datapath reconciliation errors no longer caused the supervisor to exit; the standby stayed in `STANDBY` instead of restarting into a later accidental takeover.

Failed or invalidated:

- `SUP-016` scale-out from desired capacity 2 to 3 completed, but it changed the stable egress EIP. Stable egress mode must not allocate or expose a new public IP during a capacity-only update.
- provider destroy tried to roll the private route back to a stale ENI target from earlier state and failed. Test cleanup required manual provider-resource cleanup plus `terraform state rm betternat_gateway.egress`.
- the appliance readiness command used during the run included `betternat-agent --once`. That command is not read-only in HA mode, so subsequent HA state observations from this run are not valid for failover timing.

Cleanup:

- provider-managed ASG, launch template, EIP, DynamoDB table, IAM role/profile, and appliance security group were manually removed after the rollback failure,
- Terraform then destroyed the remaining VPC fixture,
- the temporary artifact bucket was removed,
- final AWS checks found no VPC, EIP, ENI, EBS volume, ASG, DynamoDB table, or S3 bucket remaining for the run id.

Fix status before the next combined AWS run:

- capacity-only provider updates now have a dedicated ASG update path in local tests; this still needs AWS verification,
- provider route rollback now skips stale rollback targets in local tests; this still needs AWS destroy verification,
- HA readiness commands in the runbook are now read-only.

## Follow-up Run: bnat-20260620173109

This run validated the capacity-only provider update and destroy fixes, but exposed a metrics correctness bug during HA churn.

Run:

```text
Run ID: bnat-20260620173109
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Market: Spot
Stable egress IP: enabled
Observed EIP: 44.255.220.184
```

Passed:

- baseline Terraform apply completed,
- ASG reached two healthy appliances,
- route, EIP, and DynamoDB lease converged to one owner,
- private client egress worked through the shared EIP,
- read-only readiness checks showed active/standby state and datapath readiness,
- `SUP-016` scale-out from desired capacity 2 to 3 completed in place without changing the EIP,
- route and EIP stayed on the original owner during scale-out,
- private client egress continued through the same EIP after scale-out,
- scale-in from 3 to 2 completed and eventually recovered control plane and data plane after AWS selected the owner for termination,
- private client egress recovered through the same EIP after scale-in,
- provider destroy completed without the previous stale rollback target failure,
- artifact bucket cleanup and final AWS residual checks found no VPC, EIP, ENI, EBS volume, ASG, DynamoDB table, or S3 bucket remaining for the run id.

Baseline probe:

```text
2026-06-20T17:35:12.787Z
44.255.220.184
HTTP/2 200
```

Scale-out probe:

```text
2026-06-20T17:37:26.338Z
44.255.220.184
HTTP/2 200
```

Scale-in recovery probe:

```text
2026-06-20T17:42:38.387Z
44.255.220.184
HTTP/2 200
```

Failed or invalidated:

- after scale-in/HA churn, two appliance metrics endpoints reported `ACTIVE` and `lease_owner_match=1`,
- AWS route, EIP, and DynamoDB agreed on only one actual owner,
- the run cannot be counted as complete automatic HA proof because the monitoring surface could produce a false double-active result.

Fix status after this run:

- `/metrics` now includes `betternat_ha_status_age_seconds` and `betternat_ha_status_stale`,
- stale supervisor snapshots are downgraded to `STALE`,
- stale snapshots force `betternat_active=0`, `betternat_lease_owner_match=0`, `betternat_route_target_match=0`, and `betternat_public_identity_match=0`,
- local `go test ./...` covers stale HA metrics behavior.

## Cleanup

Terraform destroy completed and the temporary artifact bucket was removed.

Post-cleanup read-only checks:

```text
tagged EIPs: []
tagged ASGs: []
DynamoDB lease table: not found
tagged VPCs: []
tagged ENIs: []
tagged EBS volumes: []
artifact bucket: not found
```

Tagged EC2 instances were visible only as terminated history:

```text
i-07a864fe7c6c3cb53 terminated
i-053b988ae82378346 terminated
i-0abdee0efff331971 terminated
i-078886eb81a41a0c3 terminated
i-07df85cfac6b19f9f terminated
```

## Combined Stable-Egress Run: bnat-20260620182614

This run validated the stale-metrics build, stable EIP failover, ASG repair, and capacity-only Terraform updates in one low-cost AWS run.

Run:

```text
Run ID: bnat-20260620182614
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Market: Spot
Stable egress IP: enabled
Observed EIP: 35.85.131.212
```

Baseline passed:

- ASG reached two healthy appliances.
- DynamoDB lease, private route, and EIP ownership converged to one owner.
- Private client egress returned the fixed EIP `35.85.131.212`.
- `https://example.com` returned `HTTP/2 200`.

Agent-stop failover:

```text
Trigger: 2026-06-20T18:31:25.822Z stop betternat-agent on active i-01faca559db72c430
Control plane moved route/EIP to standby i-034d1fd4ce1196572 around 2026-06-20T18:31:39Z
```

Observed result:

- client loop had no hard `FAIL`,
- one sample at `2026-06-20T18:31:39.314Z` returned non-EIP `16.145.96.36`,
- subsequent samples returned the fixed EIP `35.85.131.212`.

Interpretation:

- stable EIP mode converges back to the fixed EIP,
- during active-agent failure, old datapath may continue forwarding briefly before the cloud identity fully converges,
- product docs should say stable egress identity is guaranteed after failover convergence for new flows; transient samples during failure detection may leak the instance public IP until this path is hardened.

Owner-termination failover:

```text
Trigger: terminate active i-01faca559db72c430
New owner: i-034d1fd4ce1196572
Observed client outage: about 12 seconds
```

Observed result:

- route/EIP moved to the pre-existing standby in about 9 seconds by control-plane observation,
- client loop failed from about `18:47:03` through `18:47:13`,
- client recovered at `18:47:15` with the same fixed EIP `35.85.131.212`,
- no non-EIP leak was captured in this termination loop.

ASG repair:

- ASG launched replacement `i-0b9d1f571a0709cea`,
- after the supervisor step-timeout fix was hot updated, 90 seconds of observation kept owner, route, and EIP stable,
- active metrics reported fresh HA status and `stale=0`,
- standby metrics reported fresh HA status and `stale=0`.

Terraform provider capacity updates:

- scale-out desired capacity 2 to 3 completed in place: `0 added, 1 changed, 0 destroyed`,
- route/EIP remained under the HA agent's runtime ownership,
- scale-in 3 to 2 completed in place: `0 added, 1 changed, 0 destroyed`,
- AWS selected the current owner for termination during scale-in; HA moved ownership to `i-0ffa0419a91d31aad`,
- final private client egress still returned `35.85.131.212` and `HTTP/2 200`.

Cleanup:

- Terraform destroy completed: `Resources: 16 destroyed`,
- artifact bucket `bnat-20260620182614-artifacts` was removed,
- residual checks found no VPC, EIP, ENI, EBS volume, ASG, DynamoDB table, or S3 bucket for the run id.

## Combined Non-Stable-Egress Run: bnat-20260620191841

This run validated non-fixed public egress identity, automatic standby takeover, ASG replacement, and replacement standby behavior.

Run:

```text
Run ID: bnat-20260620191841
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Market: Spot
Stable egress IP: disabled
Terraform output egress_public_ips: {}
```

Baseline passed:

- ASG had two `InService` and `Healthy` appliances:
  - active candidate `i-08e433ac78de45266`, public IP `54.245.164.82`,
  - standby candidate `i-0c6bacc0aca498978`, public IP `34.210.117.59`.
- agent config correctly rendered no shared EIP:

```json
"public_identity":{"mode":"","allocation_id":""}
```

- DynamoDB lease owner was `i-08e433ac78de45266`.
- private default route target was `i-08e433ac78de45266`.
- active/standby logs showed continuous HA steps with no errors.
- private client egress returned `54.245.164.82`.
- `https://example.com` returned `HTTP/2 200`.

Owner-termination failover:

```text
Trigger: 2026-06-20T19:25:43.3Z terminate active i-08e433ac78de45266
New owner: i-0c6bacc0aca498978
Old public IP: 54.245.164.82
New public IP: 34.210.117.59
```

Control-plane observation:

```text
2026-06-20T19:25:46.3Z lease=i-08e433ac78de45266 route=i-08e433ac78de45266
2026-06-20T19:25:57.3Z lease=i-08e433ac78de45266 route=i-0c6bacc0aca498978
2026-06-20T19:26:05.3Z lease=i-0c6bacc0aca498978 route=i-0c6bacc0aca498978
```

Client-visible result:

```text
2026-06-20T19:25:43.932Z OK 54.245.164.82
2026-06-20T19:25:45.147Z OK 54.245.164.82
2026-06-20T19:25:46.332Z OK 54.245.164.82
2026-06-20T19:25:47.512Z FAIL
2026-06-20T19:25:50.522Z FAIL
2026-06-20T19:25:53.533Z FAIL
2026-06-20T19:25:56.544Z FAIL
2026-06-20T19:25:59.554Z OK 34.210.117.59
```

Observed failover timing:

- client-visible outage was about 12 seconds from first failure to first recovery,
- route moved before the DynamoDB lease scan showed the new owner, which is consistent with takeover doing route activation before the next coarse poll observed the lease,
- egress IP changed, as expected for non-stable mode.

ASG repair:

- ASG replaced the terminated owner with `i-07561e6b0d6af3ffa`,
- ASG returned to two healthy `InService` appliances,
- the replacement joined as `STANDBY`,
- the new active remained `i-0c6bacc0aca498978`,
- 90 seconds of observation showed lease and route stable on the new active.

Metrics after repair:

```text
i-07561e6b0d6af3ffa: state=STANDBY, betternat_ha_status_stale=0
i-0c6bacc0aca498978: state=ACTIVE, betternat_ha_status_stale=0
```

Final client probe:

```text
final_checkip=34.210.117.59
HTTP/2 200
```

Cleanup:

- Terraform destroy completed: `Resources: 16 destroyed`,
- artifact bucket `bnat-20260620191841-artifacts` was removed,
- residual checks found no VPC, EIP, ENI, EBS volume, ASG, DynamoDB table, or S3 bucket for the run id.

## Bugs Found And Fixed During The Combined Runs

- Provider non-capacity updates are now rejected with a replacement-required diagnostic instead of silently mutating unsafe fields in place.
- Capacity-only provider updates use the ASG update path and were validated in AWS.
- HA activation now releases an acquired lease if activation fails before cloud mutation completes.
- HA activation and active supervision now verify the lease after datapath/cloud reconciliation to reduce split-brain risk.
- HA supervisor steps are bounded by context timeout so a hung AWS SDK or datapath call cannot leave metrics permanently stale while the process remains alive.
- Datapath reconciliation in the supervisor is separately timeout-bounded.
- HA metrics now expose status age and stale state; stale snapshots are downgraded instead of reporting false active.
- Non-stable egress config no longer renders `public_identity.mode="shared_eip"` with an empty allocation ID.

## Current Decision

The low-cost AWS supplemental matrix is now complete enough for the current product decision:

- stable EIP mode works for baseline egress, agent-stop failover, owner-termination failover, ASG repair, and capacity-only scale-out/scale-in,
- non-stable mode works for baseline egress, owner-termination failover, ASG repair, and replacement standby behavior,
- complete automatic HA is proven for the single-AZ ASG-pool design without manual `AssociateAddress` or `ReplaceRoute` during failure handling,
- high-volume crawler/RPC/image-pull simulations remain intentionally deferred because they are cost-heavy and not required to validate control-plane correctness.

Remaining production-hardening work:

- reduce or eliminate transient non-EIP leakage during stable-mode agent-process failure,
- add structured retry/backoff policy around transient AWS/DynamoDB errors,
- add a first-class appliance readiness/status command for AMI builds,
- run the same matrix after AMI packaging replaces cloud-init bootstrap,
- run longer soak tests before promising an SLO.
