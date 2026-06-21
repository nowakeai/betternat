# AWS Test Plan

Last updated: 2026-06-20

## Purpose

This document defines the AWS integration tests that local VM validation cannot prove.

Local VM tests cover Linux build behavior, nftables/nf_conntrack fallback behavior, basic agent reconciliation, metrics rendering, and local HA/lease simulations. AWS tests must prove the cloud control plane and real appliance datapath:

- `ec2:ReplaceRoute`,
- EIP association/reassociation and stable public egress IP,
- EC2 source/destination check behavior,
- IAM scope,
- DynamoDB lease/fencing under real conditional writes,
- LoxiLB on EC2 kernel/runtime,
- real private subnet egress through BetterNAT,
- cleanup and rollback.

## Safety Rules

AWS tests must be disposable and isolated.

Use a dedicated test VPC and tag every resource:

```text
Project=betternat
Environment=aws-integration-test
Owner=<operator>
TTL=<yyyy-mm-dd>
```

Rules:

- Do not reuse production VPCs, route tables, subnets, EIPs, IAM roles, or DynamoDB tables.
- Prefer Spot instances for workload/client nodes when interruption is acceptable.
- Keep one explicit cleanup command or script per create command.
- Capture resource IDs into a manifest under `tmp/`, not into tracked docs.
- Never commit account IDs, local filesystem paths, private keys, or generated state files.
- Before deletion, list resources by tags and show the planned deletion set.
- After deletion, verify there are no tagged resources left except intentionally retained AMIs or logs.

Recommended environment variables:

```sh
export AWS_PROFILE=<test-profile>
export AWS_REGION=us-west-2
export BETTERNAT_TEST_AZ=us-west-2a
export BETTERNAT_TEST_TAG_TTL=<yyyy-mm-dd>
```

## Test Topology

Minimum AWS topology:

```text
Internet
  |
  | EIP
  v
public subnet
  |
  +-- betternat-active EC2 appliance
  +-- betternat-standby EC2 appliance

private subnet
  |
  +-- client EC2 instance

DynamoDB
  |
  +-- HA lease row

VPC route table
  |
  +-- private default route 0.0.0.0/0 -> active appliance instance
```

Recommended first pass:

- one VPC,
- one public subnet in one AZ,
- one private subnet in the same AZ,
- two appliance EC2 instances,
- one private client EC2 instance,
- one EIP shared by the active appliance,
- one DynamoDB lease table,
- one private route table.

Cross-AZ tests should come after same-AZ tests are stable.

## Test Matrix

| ID | Area | Required AWS Resources | Success Criteria | Cleanup Required |
|----|------|------------------------|------------------|------------------|
| AWS-001 | Isolated VPC bootstrap | VPC, IGW, public/private subnets, route tables, SGs | Tagged VPC exists; public subnet has IGW route; private subnet has no NAT Gateway | Delete VPC stack |
| AWS-002 | Appliance source/dest check | 1 EC2 appliance | `SourceDestCheck=false` on appliance ENI or instance | Terminate instance |
| AWS-003 | LoxiLB starts on EC2 | 1 EC2 appliance | LoxiLB API ready; `loxicmd get lbversion` succeeds | Terminate instance |
| AWS-004 | LoxiLB SNAT rule create/read | 1 EC2 appliance | BetterNAT or `loxicmd` creates egress SNAT rule; readback includes expected private CIDR and SNAT target | Delete rule / terminate instance |
| AWS-005 | Private client egress | appliance + private client + route | private client reaches internet through appliance; no AWS NAT Gateway exists | Terminate instances, delete route |
| AWS-006 | Stable public egress IP | EIP + appliance + client | private client sees the assigned EIP from `checkip`/equivalent | Release EIP |
| AWS-007 | ReplaceRoute active switch | 2 appliances + route table | `ReplaceRoute` changes `0.0.0.0/0` from active to standby and `DescribeRoute` confirms target | Restore/delete route |
| AWS-008 | EIP reassociation | 2 appliances + EIP | EIP moves from active to standby; `DescribeAddresses` confirms new association | Release EIP |
| AWS-009 | End-to-end failover | 2 appliances + EIP + client + route | after active failure, route and EIP point to standby; new client egress uses same public IP | Terminate instances, release EIP |
| AWS-010 | DynamoDB lease contention | DynamoDB table + 2 agents | only one owner wins; generation/fencing prevents stale active from mutating route/EIP | Delete table |
| AWS-011 | IAM minimum runtime policy | IAM role/policy + agent | agent can perform required route/EIP/lease calls and is denied unrelated calls | Delete role/policy |
| AWS-012 | Observability counters | appliance + traffic generator | Prometheus metrics expose datapath readiness, rule counters, conntrack summary, owner attribution where configured | Terminate instances |
| AWS-013 | DNS/UDP workload | client + resolver/download target | UDP/DNS traffic works through appliance and appears in counters/conntrack where supported | Terminate client |
| AWS-014 | Large download workload | client + large HTTP object | sustained download works; public egress IP stable; counters increase plausibly | Terminate client |
| AWS-015 | Concurrent flow workload | client fleet or iperf/http loop | many concurrent flows complete without obvious drops or agent/datapath crash | Terminate clients |
| AWS-016 | Reboot reconciliation | appliance | after appliance reboot, LoxiLB and agent reconcile expected state | Terminate instance |
| AWS-017 | Rollback route | previous route target + BetterNAT route | rollback restores previous target and verifies `DescribeRoute` | Delete route stack |
| AWS-018 | Full cleanup | all test resources | no tagged test resources remain; no EIPs, ENIs, NAT Gateways, DynamoDB tables, or Spot requests left behind | N/A |

## Execution Phases

### Phase 0: Preflight

Goal: prove credentials and region are correct before creating resources.

Checks:

```sh
aws sts get-caller-identity
aws ec2 describe-availability-zones --region "$AWS_REGION"
aws service-quotas get-service-quota --service-code ec2 --quota-code L-0263D0A3
```

Also verify:

- the selected AZ supports the chosen instance type,
- the account can allocate at least one EIP,
- there are no existing `Project=betternat,Environment=aws-integration-test` resources from a prior run.

### Phase 1: Network And Instance Baseline

Goal: create isolated AWS plumbing and verify a private instance has no accidental egress before BetterNAT is installed.

Steps:

1. Create VPC, public subnet, private subnet, IGW, route tables, security groups.
2. Launch private client instance without a NAT Gateway route.
3. Confirm client cannot reach internet directly.
4. Confirm no AWS NAT Gateway exists in the test VPC.

Evidence:

- VPC/subnet/route table IDs,
- `DescribeNatGateways` returns none for the test VPC,
- client egress probe fails before BetterNAT route is installed.

### Phase 2: Single Appliance Datapath

Goal: prove one BetterNAT appliance can provide private subnet egress.

Steps:

1. Launch one appliance in the public subnet.
2. Disable source/destination check.
3. Attach or associate the test EIP to the appliance.
4. Start LoxiLB and `betternat-agent`.
5. Create private route `0.0.0.0/0 -> appliance instance`.
6. From the private client, run:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsS https://example.com >/dev/null
dig +short example.com
```

Success:

- client egress works,
- observed source IP equals the test EIP,
- LoxiLB/firewall counters increase,
- BetterNAT Prometheus metrics show datapath ready.

### Phase 3: HA Control Plane

Goal: prove active/standby route and EIP ownership.

Steps:

1. Launch active and standby appliances.
2. Create DynamoDB lease table.
3. Start agents on both nodes.
4. Confirm only one node owns the lease.
5. Confirm route and EIP point to the lease owner.
6. Stop active agent or terminate active instance.
7. Measure time until:
   - standby acquires lease,
   - EIP is associated to standby,
   - route target is standby,
   - private client egress resumes with the same public IP.

Record:

```text
t0 failure injected
t1 lease acquired by standby
t2 EIP reassociated
t3 route replaced
t4 client egress probe succeeds
```

Success target for v0 spike:

- new egress flows recover automatically,
- public egress IP remains the same,
- no split-brain route/EIP mutation occurs.

The exact SLO can be tightened after measured data; do not claim 2-5 seconds until AWS evidence supports it.

### Phase 4: Workload Tests

Goal: match target customer pain points.

Workloads:

- DNS/UDP-heavy client,
- large HTTP/object download,
- concurrent HTTP downloads,
- long-lived TCP download,
- optional blockchain/RPC-like peer sync simulation.

Minimum commands from private client:

```sh
dig example.com
curl -fL https://speed.hetzner.de/100MB.bin -o /tmp/100MB.bin
seq 1 50 | xargs -n1 -P10 -I{} curl -fsS https://example.com -o /dev/null
```

Use neutral public endpoints or a controlled S3 bucket for repeatability. Avoid endpoints that rate-limit or block cloud traffic.

Evidence:

- completion count,
- bytes downloaded,
- observed public IP,
- LoxiLB/nftables counters,
- CPU/network metrics on appliance,
- conntrack or LoxiLB connection summary.

### Phase 5: Failure And Reconciliation

Goal: prove restart and rollback behavior.

Tests:

- restart LoxiLB container/service,
- restart `betternat-agent`,
- reboot standby,
- reboot active,
- terminate active Spot instance,
- temporarily deny one AWS API call if practical,
- rollback route to previous target.

Success:

- desired datapath rules are restored,
- stale owner cannot mutate after losing lease,
- route/EIP state matches current lease owner,
- rollback restores previous route target.

## Metrics To Capture

Minimum:

- failover duration by phase,
- route replacement API latency,
- EIP association API latency,
- private client outage duration,
- observed public IP before and after failover,
- appliance CPU,
- appliance network throughput,
- LoxiLB or nftables rule counters,
- conntrack entries,
- agent logs,
- CloudTrail events for `ReplaceRoute`, `AssociateAddress`, `ModifyInstanceAttribute`, DynamoDB conditional writes.

Optional:

- packet loss during failover,
- p95/p99 request latency during workload,
- softirq CPU,
- ENA interface counters,
- LoxiLB logs around eBPF attach/reconcile.

## Cleanup Checklist

Always run cleanup even after failed tests.

Delete or verify deletion of:

- EC2 instances,
- Spot requests,
- EIPs,
- ENIs left by instances,
- security groups,
- route tables,
- subnets,
- IGW,
- VPC,
- DynamoDB tables,
- IAM roles and instance profiles,
- CloudWatch log groups created by the test,
- SSM parameters or secrets if used.

Final verification commands:

```sh
aws ec2 describe-instances --filters "Name=tag:Project,Values=betternat" "Name=tag:Environment,Values=aws-integration-test"
aws ec2 describe-addresses --filters "Name=tag:Project,Values=betternat" "Name=tag:Environment,Values=aws-integration-test"
aws ec2 describe-nat-gateways --filter "Name=tag:Project,Values=betternat" "Name=tag:Environment,Values=aws-integration-test"
aws dynamodb list-tables
aws iam list-roles
```

For DynamoDB and IAM, filter by the BetterNAT test naming prefix because not all list APIs support tag filters directly.

## Pass / Fail Policy

Pass:

- all resources are tagged and cleaned,
- private client egress works through BetterNAT,
- observed public source IP is the assigned EIP,
- route and EIP fail over to standby,
- lease/fencing prevents split brain,
- datapath and agent metrics are available,
- rollback works.

Fail:

- any leaked chargeable resource remains,
- any test touches non-test resources,
- source/destination check remains enabled on an appliance,
- client egress bypasses BetterNAT,
- public source IP changes unexpectedly during HA failover,
- stale active can mutate route/EIP after losing lease,
- cleanup cannot prove all test resources are gone.

Blocked:

- AWS quota unavailable,
- selected AZ lacks instance capacity,
- LoxiLB cannot initialize on the selected EC2 kernel/AMI,
- public test endpoints rate-limit or block the account.

Blocked results should keep evidence logs and resource IDs, then cleanup before retrying.
