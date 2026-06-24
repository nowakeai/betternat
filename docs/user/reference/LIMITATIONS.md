# BetterNAT Limitations

Date: 2026-06-21

## Alpha Quality

`v0.1.0-alpha.2` is for technical evaluation in disposable or non-critical AWS environments.

It is not a drop-in AWS NAT Gateway SLA replacement.

The alpha release does not publish an availability SLO, failover-time SLO, or
packet-loss SLO. Timing measurements in the docs are validation evidence from
specific test environments, not service-level commitments.

## Platform Scope

Current alpha scope:

- AWS only,
- one AZ per HA group,
- Terraform provider first,
- cloud-init bootstrap instead of a published BetterNAT AMI,
- LoxiLB/eBPF datapath.

Not included:

- multi-cloud runtime,
- CloudFormation delivery,
- AWS Marketplace delivery,
- active-active NAT,
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

Observed low-cost AWS tests saw about 12 seconds of client-visible outage for owner termination under test conditions. This is evidence, not an SLA.

The 2026-06-24 route-only/non-stable proactive handover comparison was much
faster in the retained alpha environment: the client observed the public source
IP switch within about 435 ms at probe sampling granularity and recorded zero
failed samples. This does not change the limitation: use non-stable mode only
when downstream systems do not require a fixed allowlisted egress IP.

## Cost Semantics

BetterNAT avoids NAT Gateway per-GB processing charges.

It does not eliminate:

- EC2 instance charges,
- EBS charges,
- public IPv4 and EIP charges where applicable,
- public internet data transfer charges,
- DynamoDB charges,
- CloudWatch, SSM, and logging charges.

High-volume savings are workload dependent and modeled, not proven by expensive multi-TB benchmark runs in the alpha.

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

The first alpha boot path depends on:

- package repositories,
- Docker install/start,
- LoxiLB image pull,
- artifact URL reachability,
- checksum verification,
- cloud-init execution.

Boot-to-ready timing is not representative of a future prebuilt AMI.

The default `cloud_init` path uses ordinary auto-assigned public IPv4 addresses
for bootstrap and management/control-plane egress. In stable EIP mode, the
shared EIP remains the intended private-workload egress identity; the per-node
public IPv4 addresses are operational reachability, not fixed allowlist
addresses.

Current GA scope accepts this documented caveat: stable mode converges back to
the shared EIP, but during a transition a successful new-flow sample can briefly
egress through a gateway node's ordinary public IPv4 when per-node public IPv4
is enabled. Strict "every successful sample always returns only the shared EIP"
semantics are future hardening and likely require secondary private IP or ENI
based egress identity.

Private prebaked AMIs can opt into `bootstrap_mode="prebaked_ami"`; stable EIP
deployments in that mode disable per-node auto-assigned public IPv4.

## Tuning Semantics

The alpha bootstrap applies conservative gateway sysctls.

Linux `nf_conntrack_max` is not the primary LoxiLB/eBPF conntrack capacity knob.

Advanced tuning such as conntrack buckets, timeouts, ephemeral port ranges, backlog, IRQ/RSS, and ENA settings is deferred until benchmark-backed profiles exist.
