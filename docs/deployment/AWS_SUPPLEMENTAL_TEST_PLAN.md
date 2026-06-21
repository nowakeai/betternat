# AWS Supplemental Test Plan

Last updated: 2026-06-21

## Purpose

This document lists the AWS tests that were not covered by the first AWS integration run and prioritizes the low-cost follow-up tests.

Execution checklist: `docs/deployment/AWS_SUPPLEMENTAL_RUNBOOK.md`.

The first AWS run proved the core path:

- LoxiLB works on EC2,
- private subnet egress through an appliance works,
- EIP + `ReplaceRoute` can preserve public egress IP after manual failover,
- DynamoDB conditional writes work for lease/fencing,
- resources can be cleaned safely.

It did not fully benchmark failover modes or prove the finished product loop.

## Cost Policy

Keep this supplemental pass cheap:

- use one isolated VPC,
- use one AZ first,
- use Spot EC2 where acceptable,
- use short-lived tests,
- use tiny/small instances unless testing throughput,
- avoid NAT Gateway,
- avoid large data transfer tests,
- do not run multi-TB or tens-of-TB traffic tests,
- do not run billing-scale crawler/RPC/image-pull simulations,
- use only tiny probe traffic for egress checks such as `checkip`, HTTP headers, and small downloads,
- clean all resources after each run,
- store only summarized evidence in docs.

Expensive or noisy tests are explicitly deferred.

## Priority Matrix

| ID | Test | Cost | Priority | Run Now? | Why |
|----|------|------|----------|----------|-----|
| SUP-001 | Route-only failover timing | Low | P0 | Yes, after fixture apply confirms SSM client reachability | This was the main missing comparison: egress IP changes, only `ReplaceRoute` runs |
| SUP-002 | Stable-IP failover repeated timing | Low | P0 | Yes, after fixture apply confirms SSM client reachability | First run had one sample only; need P50/P95-ish data |
| SUP-003 | Client recovery timing for new flows | Low | P0 | Yes, after fixture apply confirms SSM client reachability | AWS control-plane time is not the same as workload recovery time |
| SUP-004 | Route rollback timing | Low | P1 | Yes | Product needs safe rollback from BetterNAT route to previous target |
| SUP-005 | Agent-driven AWS SDK loop | Low/Medium | P1 | Yes, after agent implementation | Manual API test does not prove product behavior |
| SUP-006 | IAM least-privilege runtime policy | Low | P1 | Yes | Runtime role must be narrower than admin/install role |
| SUP-007 | LoxiLB restart reconciliation | Low | P1 | After helper | Reboot/restart should restore rules without manual action; needs appliance access/readiness helper before the test is runnable |
| SUP-008 | Long-lived TCP behavior during failover | Low | P2 | Optional | Useful to document v0 limitations; likely v0 does not preserve existing flows |
| SUP-009 | Multi-route-table failover | Low | P2 | Optional | Common AWS VPCs may have several private route tables |
| SUP-010 | Cross-AZ failover | Medium | P2 | Later | More realistic but more variables and possible cross-AZ data costs |
| SUP-011 | Concurrent flow pressure | Medium | P2 | Later, small only | 10 curl smoke is not enough, but real pressure testing can wait; current pass may use only small request loops |
| SUP-012 | High-throughput or high-volume download benchmark | Higher | P3 | Excluded from current pass | Do not run multi-TB/tens-of-TB traffic or billing-scale workload tests in this phase |
| SUP-013 | EKS pod attribution | Medium/Higher | P3 | Defer | Requires EKS setup and separate observability design |
| SUP-014 | Terraform provider full lifecycle | Low/Medium | P1 | Done for ASG-first create/destroy | Real AWS run `bnat-20260620141534` passed Launch Template/ASG desired=2, owner EIP/route, source/destination check on both nodes, destroy, and cleanup |
| SUP-015 | ASG candidate repair | Low | P0 | Yes | Terminate non-owner and prove ASG launches a replacement without changing owner route/EIP |
| SUP-016 | ASG scale-out and scale-in | Low | P1 | Yes | Prove `desired_capacity` changes are usable product controls and do not disturb the owner unnecessarily |
| SUP-017 | Owner termination without agent takeover | Low | P1 | Historical baseline | This was useful before the HA supervisor existed; do not treat ASG-only repair as HA |
| SUP-018 | Agent lease takeover after owner loss | Low/Medium | P0 | After local validation | The actual product HA proof: candidate acquires lease, moves EIP/route, and new flows recover |
| SUP-019 | ASG repair after owner loss | Low/Medium | P1 | After SUP-018 | Prove capacity repair happens after failover: ASG replaces the terminated owner and pool returns to desired capacity |

## Combined Run Strategy

Most low-cost supplemental tests can run in one AWS fixture lifecycle, but they should not be run as one unordered batch.

Use one isolated VPC and one ASG appliance pool, then execute in phases. This is the preferred next AWS pass because it reuses the same fixture and avoids paying the repeated setup/teardown cost.

Pre-run gates:

- local `go test ./...` passes,
- the provider binary is rebuilt and Terraform validate passes for the supplemental fixture,
- HA metrics include `betternat_ha_status_stale` and `betternat_ha_status_age_seconds`,
- stale HA status must not report `betternat_active=1`,
- appliance readiness checks are read-only.

1. **Baseline phase**
   - apply the fixture,
   - confirm both appliances are healthy,
   - confirm private client egress,
   - confirm route/EIP ownership,
   - confirm cleanup tags and outputs are usable.
2. **Provider lifecycle phase**
   - run `SUP-016` scale-out from 2 to 3,
   - verify the existing owner route/EIP remains stable,
   - run scale-in from 3 to 2,
   - if the owner is selected for scale-in, treat the result as an HA event and verify route/EIP/DynamoDB/metrics converge.
3. **Non-owner disruption phase**
   - run `SUP-015` by terminating a non-owner candidate,
   - verify ASG replacement joins,
   - verify owner route/EIP remains stable.
4. **Manual/control-plane timing phase**
   - run `SUP-001`, `SUP-002`, `SUP-003`, and `SUP-004` only after candidate datapath readiness is confirmed,
   - do not claim product HA success from these manual API timings.
5. **Automatic HA phase**
   - run `SUP-018-A` by stopping the owner agent,
   - run `SUP-018-B` by terminating the owner instance,
   - run `SUP-019` as the follow-through capacity repair check.
6. **Cleanup phase**
   - destroy Terraform resources,
   - delete temporary artifact bucket,
   - verify VPC, EIP, ENI, EBS, ASG, DynamoDB, IAM, and S3 cleanup.

Stop the combined run after any ownership, metrics correctness, or cleanup failure. Continuing after a failed route/EIP mutation can make later timing and scale results ambiguous.

Current combined-run status:

- `SUP-014`, `SUP-015`, and `SUP-018-A` have passed in AWS run `bnat-20260620153304`.
- `SUP-018-B` and `SUP-019` are partial because recovery happened through the ASG replacement rather than the pre-existing standby.
- `SUP-016` has now passed scale-out at AWS level and partially passed scale-in: control-plane/data-plane recovered with stable EIP, but stale metrics caused the run to be invalid for final HA proof.
- stale metrics are now a hard gate: a node with an old supervisor snapshot must report `STALE`, `betternat_active=0`, and `betternat_ha_status_stale=1`.
- appliance readiness in HA mode must use read-only checks only: `/metrics`, service status, LoxiLB container status, journal logs, AWS route/EIP state, and DynamoDB lease state.
- high-volume, multi-TB, crawler-scale, RPC sync-scale, and image-pull fleet simulations remain excluded from this low-cost pass.

## P0: Failover Timing Comparison

### Goal

Measure two product modes separately:

1. **Route-only mode**
   - operation: `ReplaceRoute` only,
   - egress IP can change,
   - expected to be cheaper/faster because no EIP reassociation is needed.

2. **Stable egress IP mode**
   - operations: `AssociateAddress --allow-reassociation` + `ReplaceRoute`,
   - egress IP should stay the same,
   - first run measured one manual sample at about 4 seconds.

### Minimal Topology

Use the supplemental Terraform fixture. It creates:

```text
public subnet:
  ASG appliance pool
    current owner
    warm candidate

private subnet:
  SSM-managed private client

private route table:
  0.0.0.0/0 -> current owner or candidate appliance
```

For route-only mode, each appliance can use its own public IP/EIP, or one can use an auto-assigned public IP if the test only checks client reachability and changed source IP.

The fixture exposes `stable_egress_ip` as a variable. For an all-in-one low-cost timing pass, applying with `stable_egress_ip=true` is acceptable: route-only timing can still be measured by running only `ReplaceRoute` and observing the candidate's own public IPv4 address.

### Measurements

For each trial, record:

```text
t0 api call start
t1 AWS API call returned
t2 DescribeRoute / DescribeAddress confirms desired state
t3 private client new-flow check succeeds
t4 observed public source IP
```

Run at least:

- 5 trials for route-only,
- 5 trials for stable-IP,
- alternate direction active -> standby and standby -> active if possible.

Do not claim product SLO from these numbers. These are control-plane/manual-trigger measurements only.

### Route-Only Procedure

Preconditions:

- both appliances have LoxiLB ready,
- both appliances have SNAT rule for the private subnet,
- both appliances have source/dest check disabled,
- route initially points to appliance A,
- private client can egress through appliance A.

Trial:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 replace-route \
  --route-table-id <private-rtb> \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id <appliance-b>
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 describe-route-tables --route-table-ids <private-rtb>
```

Then from a private client:

```sh
curl -4 -fsS https://checkip.amazonaws.com
curl -4 -fsSI https://example.com | head
```

Success:

- `DescribeRoute` shows target appliance B,
- new client flow succeeds,
- observed source IP is appliance B's public egress identity,
- observed source IP is allowed to differ from appliance A.

### Stable-IP Procedure

Preconditions:

- both appliances have LoxiLB ready,
- both appliances have SNAT rule using their own private IP as `toIP`,
- shared EIP initially points to appliance A,
- route initially points to appliance A,
- private client sees the shared EIP.

Trial:

```sh
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 associate-address \
  --allocation-id <shared-eip-allocation> \
  --instance-id <appliance-b> \
  --allow-reassociation
aws ec2 replace-route \
  --route-table-id <private-rtb> \
  --destination-cidr-block 0.0.0.0/0 \
  --instance-id <appliance-b>
date -u +%Y-%m-%dT%H:%M:%S.%3NZ
aws ec2 describe-addresses --allocation-ids <shared-eip-allocation>
aws ec2 describe-route-tables --route-table-ids <private-rtb>
```

Then from a private client:

```sh
curl -4 -fsS https://checkip.amazonaws.com
curl -4 -fsSI https://example.com | head
```

Success:

- EIP points to appliance B,
- route points to appliance B,
- new client flow succeeds,
- observed source IP is unchanged.

## P0: Client Recovery Timing

Control-plane timing alone is not enough. Also measure when a workload can create a new successful connection.

Low-cost approach:

1. Start a private client loop before failover:

```sh
while true; do
  date -u +%Y-%m-%dT%H:%M:%S.%3NZ
  curl -4 --connect-timeout 1 --max-time 2 -fsS https://checkip.amazonaws.com || echo FAIL
  sleep 1
done
```

2. Trigger failover.
3. Capture:
   - last successful timestamp before switch,
   - first failed timestamp if any,
   - first successful timestamp after switch,
   - observed public source IP before/after.

Success:

- loop recovers without manual client changes,
- route-only mode may show changed IP,
- stable-IP mode should show unchanged IP.

## P1: Rollback Test

Goal: prove BetterNAT can restore a previous route target.

Low-cost manual version:

1. Record original route target:

```sh
aws ec2 describe-route-tables --route-table-ids <private-rtb>
```

2. Replace route to appliance B.
3. Verify egress.
4. Replace route back to appliance A.
5. Verify egress again.

Success:

- route target returns to previous value,
- client new-flow egress works after rollback.

This does not require extra instances beyond the failover test topology.

## P0: ASG Candidate Repair

Goal: prove the ASG capacity-repair loop works independently of agent failover.

This can be tested now because it does not require automatic ownership transfer.

Preconditions:

- `SUP-014` fixture applied successfully,
- ASG desired capacity is 2,
- both instances are `InService` and `Healthy`,
- identify owner from EIP association or private route target,
- identify candidate as the other ASG instance.

Procedure:

```sh
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names <asg-name>

aws ec2 describe-addresses --allocation-ids <shared-eip-allocation>
aws ec2 describe-route-tables --route-table-ids <private-rtb>

aws ec2 terminate-instances --instance-ids <candidate-instance-id>
```

Poll:

```sh
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names <asg-name>

aws ec2 describe-route-tables --route-table-ids <private-rtb>
aws ec2 describe-addresses --allocation-ids <shared-eip-allocation>
```

Success:

- ASG launches a replacement instance,
- ASG returns to desired capacity 2,
- original owner remains route/EIP target,
- replacement instance eventually has source/destination check disabled,
- no Terraform intervention is required.

Record:

- terminated candidate ID,
- replacement instance ID,
- time from termination to new instance `InService`,
- whether EIP/route stayed stable.

## P1: ASG Scale-Out And Scale-In

Goal: prove users can treat ASG capacity as a product control.

This can be tested now with the Terraform fixture.

Scale-out test:

1. Apply with `desired_capacity=2`.
2. Change to `desired_capacity=3`, `max_size=3` or higher.
3. Run Terraform apply.
4. Verify ASG has three `InService` instances.
5. Verify existing owner route/EIP target is unchanged.
6. Verify new candidate has source/destination check disabled.

Scale-in test:

1. Start from desired capacity 3.
2. Change to `desired_capacity=2`.
3. Run Terraform apply.
4. Verify ASG returns to two instances.
5. Verify gateway remains in a known state.

Success:

- capacity changes are reflected in ASG,
- provider state remains readable,
- no unexpected route/EIP movement occurs unless ASG terminates the current owner.

If ASG terminates the owner during scale-in, verify the HA supervisor moves ownership to a surviving candidate. If it does not, record it as an automatic HA failure.

## P1: Owner Termination Baseline

Goal: preserve the older ASG-only baseline and make sure it is not confused with automatic HA.

This is a historical negative/baseline test. It should not be presented as the current product success criterion.

Procedure:

1. Apply the ASG fixture.
2. Identify owner from route/EIP target.
3. Terminate owner.
4. Observe:
   - ASG replacement behavior,
   - route target state,
   - EIP association state,
   - private client new-flow behavior if a client exists.

Historical ASG-only behavior:

- ASG replaces capacity,
- route/EIP may remain attached to a dead or terminating owner until manually changed or provider/agent reconciles,
- ASG repair alone is not HA.

Value:

- creates a baseline for the agent failover test,
- prevents accidentally claiming HA from ASG repair alone.

## P0: Agent Lease Takeover After Owner Loss

Goal: prove the real product HA loop.

Run this after local validation passes and the AWS fixture confirms both appliances are SSM-managed.

Preconditions:

- every ASG node runs `betternat-agent`,
- every ASG node has source/destination check disabled,
- all candidates can reconcile LoxiLB datapath,
- DynamoDB lease table is active,
- owner renews lease,
- non-owners watch lease expiry,
- candidates can call `AssociateAddress` and `ReplaceRoute`.

Procedure:

1. Apply ASG fixture with desired capacity 2.
2. Verify owner and candidate.
3. Start private client new-flow loop.
4. Terminate owner instance or stop agent on owner.
5. Record:
   - t0 failure trigger,
   - t1 lease expiry or takeover attempt,
   - t2 `AssociateAddress` returned,
   - t3 `ReplaceRoute` returned,
   - t4 route/EIP describe confirms new owner,
   - t5 private client new-flow success.

Success:

- candidate acquires lease,
- EIP moves to candidate in stable-IP mode,
- private default route points to candidate,
- new flows recover without Terraform,
- ASG later restores desired capacity,
- no split-brain owner is observed.

Run this for:

- stable-IP mode,
- route-only mode after that mode is exposed in the fixture/provider.

## P1: ASG Repair After Owner Loss

Goal: prove the slow repair loop works after the fast failover loop.

This is the follow-through after `SUP-018`.

Success:

- after owner termination and candidate takeover, ASG launches a replacement,
- replacement joins as non-owner candidate,
- final pool size returns to desired capacity,
- route/EIP remain on the current owner,
- all nodes have source/destination check disabled.

## P1: IAM Least Privilege

Goal: prove runtime agent does not need broad admin permissions.

Create a runtime role with only:

- `ec2:DescribeRouteTables`,
- `ec2:ReplaceRoute`,
- `ec2:DescribeAddresses`,
- `ec2:AssociateAddress`,
- `ec2:DescribeInstances`,
- `ec2:ModifyInstanceAttribute` only if source/dest check is runtime-managed,
- DynamoDB lease table permissions:
  - `GetItem`,
  - `PutItem`,
  - `UpdateItem`,
  - `DeleteItem`,
  - `DescribeTable`.

Test:

- allowed calls succeed on tagged/test resources,
- unrelated call such as broad instance termination or unrelated route mutation is denied.

Keep this separate from install role testing. Terraform/provider install permissions are broader than runtime agent permissions.

## P1: LoxiLB Restart Reconciliation

Goal: prove rule replay after LoxiLB restart.

Manual low-cost version:

```sh
docker rm -f loxilb
docker run -d --name loxilb --restart unless-stopped --privileged --network host ghcr.io/loxilb-io/loxilb:latest
sudo systemctl restart betternat-agent.service
```

Then:

- wait for the running agent to reconcile datapath state,
- verify `/metrics` reports `betternat_datapath_ready 1`,
- verify private client egress.

This should eventually be an agent test, not a manual `loxicmd` test.

## P2: Long-Lived TCP Behavior

Goal: document what happens to existing flows during failover.

Low-cost version:

```sh
curl -4 -L https://speed.hetzner.de/100MB.bin -o /tmp/100MB.bin
```

Trigger failover mid-download.

Expected v0 behavior:

- existing flow probably breaks,
- new flows recover.

Do not spend money on large repeated downloads until needed. One small 100 MB or smaller test is enough to document behavior.

## Deferred Tests

### High-Throughput Benchmark

Excluded from the current AWS supplemental pass. Defer until the datapath and agent loop are stable and there is an explicit reason to spend data-transfer budget.

Reasons:

- can create data transfer cost,
- requires instance sizing decisions,
- requires repeatable traffic source/sink,
- needs CPU/ENA/conntrack instrumentation.

Do not use this supplemental pass for:

- multi-TB or tens-of-TB traffic,
- billing-scale crawler simulations,
- blockchain/RPC sync-scale downloads,
- large image-pull fleet simulations,
- repeated large object downloads.

Those scenarios are product motivation and cost-model inputs, not required functional acceptance tests for the current implementation.

### EKS Pod Attribution

Defer until the base appliance path is productized.

Reasons:

- requires EKS cluster setup,
- attribution depends on CNI/source-IP behavior,
- likely needs Kubernetes-side telemetry integration.

### Cross-AZ Failover

Defer until same-AZ HA is measured.

Reasons:

- adds route table/subnet/AZ variables,
- may incur cross-AZ data charges,
- failure semantics differ from same-AZ active/standby.

### Terraform Provider Full Lifecycle

The provider is ready to enter a low-cost AWS supplemental test.

LocalStack validation already proved the Terraform lifecycle and AWS SDK call graph:

- create IAM role/profile/policy,
- create security group,
- create DynamoDB lease table,
- launch appliance instances,
- disable source/destination check,
- allocate EIP,
- snapshot route target,
- replace route to active appliance,
- read route/EIP status,
- rollback route on destroy,
- terminate appliances,
- release EIP,
- delete DynamoDB/IAM/security group resources.

The AWS supplemental version should validate that the same lifecycle works against real AWS control-plane behavior.

Current readiness:

- Provider lifecycle against AWS can start without a BetterNAT AMI by using the official Amazon Linux 2023 AMI plus cloud-init.
- `use_spot = true` is available for low-cost test appliance launches.
- `ami_channel` is schema/plan only until the AMI resolver is implemented.
- Full datapath/failover tests need cloud-init to install Docker, start LoxiLB, download `betternat-agent`, and provide a host `loxicmd` wrapper.

Keep the test scoped:

- one isolated VPC,
- one public subnet,
- one private route table,
- one AZ,
- tiny or small Spot instances,
- short lifetime,
- no high-volume traffic.

This is still not the final production AMI release test. During development, cloud-init/user-data bootstrap is acceptable. Production release should prefer a prebuilt AMI so boot and failover recovery are faster and less dependent on network package pulls.

## Evidence Template

Create a result document under `docs/research/` for each supplemental run.

Minimum fields:

```text
Run ID:
Date:
Region/AZ:
Mode:
Instances:
EIP(s):
Route table:
Trial count:

Route-only timing:
Stable-IP timing:
Client recovery timing:

Pass:
Fail:
Blocked:
Cleanup result:
```

Include concrete timestamps and observed source IPs.

## Cleanup Gate

The supplemental run is not complete until cleanup is proven.

Verify:

```sh
aws ec2 describe-vpcs --filters Name=tag:SpikeId,Values=<run-id>
aws ec2 describe-addresses --filters Name=tag:SpikeId,Values=<run-id>
aws ec2 describe-volumes --filters Name=tag:SpikeId,Values=<run-id>
aws ec2 describe-network-interfaces --filters Name=tag:SpikeId,Values=<run-id>
aws ec2 describe-spot-instance-requests --filters Name=tag:SpikeId,Values=<run-id>
aws dynamodb describe-table --table-name <run-id>-leases
```

Acceptable final state:

- VPC/EIP/volume/ENI queries return empty,
- Spot requests are `closed`,
- DynamoDB table is not found,
- IAM role/profile are not found,
- terminated EC2 instance history may remain visible.
