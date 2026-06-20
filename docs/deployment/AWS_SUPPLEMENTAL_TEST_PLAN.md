# AWS Supplemental Test Plan

Last updated: 2026-06-20

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
- clean all resources after each run,
- store only summarized evidence in docs.

Expensive or noisy tests are explicitly deferred.

## Priority Matrix

| ID | Test | Cost | Priority | Run Now? | Why |
|----|------|------|----------|----------|-----|
| SUP-001 | Route-only failover timing | Low | P0 | Yes | This was the main missing comparison: egress IP changes, only `ReplaceRoute` runs |
| SUP-002 | Stable-IP failover repeated timing | Low | P0 | Yes | First run had one sample only; need P50/P95-ish data |
| SUP-003 | Client recovery timing for new flows | Low | P0 | Yes | AWS control-plane time is not the same as workload recovery time |
| SUP-004 | Route rollback timing | Low | P1 | Yes | Product needs safe rollback from BetterNAT route to previous target |
| SUP-005 | Agent-driven AWS SDK loop | Low/Medium | P1 | Yes, after agent implementation | Manual API test does not prove product behavior |
| SUP-006 | IAM least-privilege runtime policy | Low | P1 | Yes | Runtime role must be narrower than admin/install role |
| SUP-007 | LoxiLB restart reconciliation | Low | P1 | Yes | Reboot/restart should restore rules without manual action |
| SUP-008 | Long-lived TCP behavior during failover | Low | P2 | Optional | Useful to document v0 limitations; likely v0 does not preserve existing flows |
| SUP-009 | Multi-route-table failover | Low | P2 | Optional | Common AWS VPCs may have several private route tables |
| SUP-010 | Cross-AZ failover | Medium | P2 | Later | More realistic but more variables and possible cross-AZ data costs |
| SUP-011 | Concurrent flow pressure | Medium | P2 | Later | 10 curl smoke is not enough, but real pressure testing can wait |
| SUP-012 | High-throughput download benchmark | Higher | P3 | Defer | Can create meaningful data transfer cost |
| SUP-013 | EKS pod attribution | Medium/Higher | P3 | Defer | Requires EKS setup and separate observability design |
| SUP-014 | Terraform provider full lifecycle | Low/Medium | P1 | Done for non-AMI dev path | Provider lifecycle passed disposable AWS apply/bootstrap/inspect/destroy with AL2023 cloud-init |

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

Use the same topology as the first AWS integration test:

```text
public subnet:
  active appliance
  standby appliance

private subnet:
  private client

private route table:
  0.0.0.0/0 -> active or standby appliance
```

For route-only mode, each appliance can use its own public IP/EIP, or one can use an auto-assigned public IP if the test only checks client reachability and changed source IP.

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
```

Then:

- run agent reconciliation or manual rule creation,
- verify `loxicmd get firewall -o json`,
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

Do not spend money on large repeated downloads until needed. One small 100 MB test is enough to document behavior.

## Deferred Tests

### High-Throughput Benchmark

Defer until the datapath and agent loop are stable.

Reasons:

- can create data transfer cost,
- requires instance sizing decisions,
- requires repeatable traffic source/sink,
- needs CPU/ENA/conntrack instrumentation.

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
