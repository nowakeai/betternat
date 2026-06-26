# LoxiLB Spike Results

Date: 2026-06-19

## Summary

Standalone LoxiLB passed the first functional BetterNAT datapath spike for AWS private-subnet egress.

Current decision note: this spike is historical evidence. BetterNAT now has no
product fallback datapath; LoxiLB readiness is a release gate. Do not use the
older fallback example below as current Terraform or release guidance. See
`docs/research/055-no-nftables-fallback-decision.md`.

In an isolated `us-west-2a` VPC, a private EC2 instance successfully routed internet-bound traffic through a public-subnet LoxiLB appliance. After adding a LoxiLB egress SNAT firewall rule, `https://checkip.amazonaws.com` from the private instance returned the appliance EIP:

```text
100.21.116.205
```

The same private instance also received `HTTP/2 200` from `https://example.com`.

This validates that LoxiLB can express the core route-through NAT appliance behavior BetterNAT needs:

```text
private subnet source -> LoxiLB appliance -> internet
internet response -> LoxiLB appliance -> original private source
```

## Test Environment

All resources were created in an isolated AWS test VPC with the spike tag:

```text
Project=BetterNAT
Purpose=LoxiLBSpike
SpikeId=betternat-loxilb-20260619T154348Z
Owner=Codex
```

Environment:

```text
Region: us-west-2
AZ: us-west-2a
VPC: 10.77.0.0/16
Public subnet: 10.77.1.0/24
Private subnet: 10.77.2.0/24
LoxiLB appliance private IP: 10.77.1.201
LoxiLB appliance EIP: 100.21.116.205
Private client IP: 10.77.2.140
Private route: 0.0.0.0/0 -> LoxiLB instance
```

The LoxiLB appliance had AWS source/destination check disabled.

## LoxiLB Runtime

LoxiLB ran in standalone container mode:

```text
Image: ghcr.io/loxilb-io/loxilb:latest
Version: 0.9.8.6-beta
Build: 2026_06_19_08h:28m-nogit
API: http://127.0.0.1:11111
```

Observed dataplane state:

- LoxiLB attached eBPF to `ens5`.
- Linux `net.ipv4.ip_forward` was enabled.
- LoxiLB discovered `ens5`, `llb0`, `docker0`, and VLAN helper interfaces.
- The appliance had a default route via `10.77.1.1`.

## Working Egress SNAT Rule

The key rule was:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:10.77.2.0/24,preference:100 \
  --snat=10.77.1.201 \
  --egress
```

LoxiLB confirmed the rule as:

```json
{
  "sourceIP": "10.77.2.0/24",
  "destinationIP": "0.0.0.0/0",
  "preference": 100,
  "doSnat": true,
  "toIP": "10.77.1.201",
  "onDefault": true
}
```

After traffic ran, the rule counter reached:

```text
10530 packets
104295186 bytes
```

## Functional Result

Before the LoxiLB egress SNAT rule, the private client timed out:

```text
curl: (28) Connection timed out after 5000 milliseconds
```

After the rule, the private client repeatedly succeeded:

```text
curl https://checkip.amazonaws.com
100.21.116.205

curl -I https://example.com
HTTP/2 200
```

This confirms:

- the private route-table appliance pattern works,
- SNAT to the appliance private IP works,
- AWS maps that private IP to the associated EIP,
- TCP return traffic is DNATed back to the private client.

## Conntrack Evidence

LoxiLB conntrack showed established outbound SNAT flows:

```text
sourceIP=10.77.2.140 destinationPort=443 conntrackState=est
conntrackAct=snat-10.77.1.201:<port>:w0
servName=snat:10.77.1.201:0
```

It also showed reverse DNAT flows:

```text
destinationIP=10.77.1.201 sourcePort=443 conntrackState=est
conntrackAct=dnat-10.77.2.140:<port>:w0
servName=snat:10.77.1.201:0
```

UDP/NTP flows also appeared as `udp-est` with SNAT/DNAT actions. This is useful evidence that UDP is not obviously broken, but it is not enough to claim a full UDP/DNS validation because the private client was not reachable through SSM for direct test commands.

## Observability Result

LoxiLB exposes useful API/CLI state for BetterNAT:

- egress SNAT rule counters,
- per-flow conntrack entries,
- original private source IP,
- destination IP/port,
- protocol,
- packet and byte counters,
- SNAT/DNAT action.

This is materially better than plain nftables counters for attribution, at least at debug/API level.

Important gap:

- `http://127.0.0.1:11111/metrics` returned `404 Not Found` in this container run.

So Prometheus integration is not yet validated. BetterNAT should not assume LoxiLB provides a ready Prometheus endpoint for the metrics we need. We may need to poll LoxiLB API/CLI and re-export normalized metrics from `betternat-agent`.

## HA Result

HA was not tested in this spike.

The spike only validates datapath compatibility with BetterNAT's planned route/EIP ownership model:

- Terraform/provider creates AWS resources.
- BetterNAT agent owns lease/fencing and `ReplaceRoute`.
- LoxiLB owns packet forwarding and NAT state on the active appliance.

Open HA questions:

- Can LoxiLB state be recreated deterministically after failover?
- Can conntrack state be synced or should v0 explicitly drop active-connection preservation?
- Should BetterNAT use only route failover, or combine route failover with shared EIP reassociation?
- Does LoxiLB standalone HA conflict with BetterNAT's DynamoDB lease/fencing model?

## Security And IAM Result

The datapath-only mode did not require LoxiLB itself to call AWS APIs.

AWS APIs were only used by the spike scripts to create resources and by SSM to run commands. This supports the preferred production split:

- Terraform provider and BetterNAT agent use cloud SDKs.
- LoxiLB runs as local datapath engine.
- LoxiLB does not need broad cloud IAM permissions for the datapath-only mode.

## Decision Impact

This spike changes LoxiLB from "interesting candidate" to "serious primary datapath candidate."

Recommended v0 posture:

```hcl
datapath_engine = "loxilb"
```

Do not use a product fallback datapath. LoxiLB passed functional NAT, but
production readiness still depends on packaging, config persistence, metrics
integration, failure behavior, and performance under BetterNAT workloads.

## Remaining Work Before Choosing LoxiLB As Default

Required next spikes:

1. Package/install spike:
   - container vs systemd package,
   - boot-time rule reconciliation,
   - config persistence,
   - upgrade behavior.

2. Observability spike:
   - API polling model,
   - Prometheus re-exporter,
   - cardinality controls,
   - top source/destination byte attribution.

3. HA spike:
   - active/standby route failover,
   - shared EIP reassociation,
   - cold-start rule replay time,
   - failure detection and rollback.

4. Benchmark spike:
   - nftables vs LoxiLB,
   - high response-volume downloads,
   - many concurrent TCP flows,
   - UDP/DNS/P2P traffic,
   - conntrack/port exhaustion behavior.

## Provisional Verdict

LoxiLB is feasible for BetterNAT's core low-cost NAT datapath.

It should be promoted to the leading datapath candidate, but not yet treated as a fully solved product foundation. BetterNAT still owns the product-critical layers: Terraform UX, AWS-safe HA, lease/fencing, cost model, normalized observability, packaging, and operational guardrails.
