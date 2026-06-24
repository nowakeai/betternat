# BetterNAT Limitations

Date: 2026-06-21

## Release Quality

BetterNAT is a self-managed AWS egress gateway deployment.

It is not a drop-in AWS NAT Gateway SLA replacement.

BetterNAT does not publish an availability SLO, failover-time SLO, or packet-loss
SLO. Timing measurements in the docs are validation evidence from specific test
environments, not service-level commitments.

## Go / No-Go Checklist

Continue to a disposable VPC test when all of these are acceptable:

```text
AWS-only, one AZ per HA group
self-managed EC2 gateway nodes
new-flow recovery after failover
possible active-flow resets during failover
cloud-init bootstrap on a user-selected Linux AMI
no AWS NAT Gateway equivalent SLA
```

Stop or keep AWS NAT Gateway when any of these are hard requirements:

```text
AWS-managed service semantics and SLA
active connection preservation
active-active NAT
multi-AZ BetterNAT gateway groups are required immediately
Marketplace or CloudFormation delivery
strict stable-EIP semantics for every successful packet during transition
```

## Platform Scope

Current scope:

- AWS only,
- one AZ per HA group,
- Terraform provider first,
- cloud-init bootstrap instead of a public BetterNAT AMI,
- LoxiLB/eBPF datapath.

Not included:

- multi-cloud runtime,
- CloudFormation delivery,
- AWS Marketplace delivery,
- active-active NAT,
- multi-AZ BetterNAT gateway groups, planned for a later release,
- active connection migration,
- published BetterNAT AMIs.

## Failover Semantics

BetterNAT targets recovery for new connections.

During failover:

- active flows may reset,
- packets may be dropped,
- new-flow recovery depends on HA profile, AWS API timing, and standby readiness,
- stable EIP mode converges back to the shared EIP for new flows,
- non-stable mode changes public source IP after failover.

Use [Failure Modes](../operations/FAILURE_MODES.md) for behavior by failure
type and retained validation evidence.

## Cost Semantics

BetterNAT avoids NAT Gateway per-GB processing charges for traffic moved to
BetterNAT. It does not eliminate normal AWS data transfer, EC2, EBS, public
IPv4/EIP, DynamoDB, monitoring, logging, or operational ownership costs.

Use [Cost Model](COST_MODEL.md) for formulas, examples, and CLI estimate usage.

## Performance Semantics

Throughput depends on:

- EC2 instance type,
- packet size,
- connection churn,
- LoxiLB datapath behavior,
- security group connection tracking behavior,
- public internet egress limits,
- CPU and memory headroom.

Do not assume NAT Gateway-level scale from a small EC2 gateway node.

## Bootstrap Semantics

The cloud-init boot path depends on package repositories, Docker install/start,
LoxiLB image pull, artifact URL reachability, checksum verification, and
cloud-init execution.

Boot-to-ready timing is not representative of a future prebuilt AMI.

The default `cloud_init` path uses ordinary auto-assigned public IPv4 addresses
for bootstrap and management/control-plane egress. In stable EIP mode, the
shared EIP remains the intended private-workload egress identity; the per-node
public IPv4 addresses are operational reachability, not fixed allowlist
addresses.

Stable mode converges back to the shared EIP, but during a transition a
successful new-flow sample can briefly egress through a gateway node's ordinary
public IPv4 when per-node public IPv4 is enabled. Strict "every successful
sample always returns only the shared EIP" semantics are future hardening and
likely require secondary private IP or ENI based egress identity.

Private prebaked AMIs can opt into `bootstrap_mode="prebaked_ami"`; stable EIP
deployments in that mode disable per-node auto-assigned public IPv4.

Use [Security Hardening](SECURITY_HARDENING.md) for bootstrap and supply-chain
risk details.

## Tuning Semantics

The bootstrap applies conservative gateway sysctls.

Linux `nf_conntrack_max` is not the primary LoxiLB/eBPF conntrack capacity knob.

Advanced tuning such as conntrack buckets, timeouts, ephemeral port ranges, backlog, IRQ/RSS, and ENA settings is deferred until benchmark-backed profiles exist.

## Next Step

If these limitations are acceptable, run the disposable VPC
[Quick Start](../getting-started/QUICK_START.md). Do not start with an existing
production route table.
