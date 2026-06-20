# LoxiLB Extended Spike Results

Date: 2026-06-20

## Summary

This extended AWS spike validated LoxiLB more deeply as BetterNAT's leading datapath candidate.

Result:

- Standalone LoxiLB egress NAT works for private-subnet AWS route-through appliance mode.
- TCP, HTTPS, DNS/UDP, concurrent short flows, and larger response downloads worked.
- EIP reassociation plus `ReplaceRoute` failover worked and preserved the public egress IP for new connections.
- LoxiLB exposes useful firewall counters and conntrack entries for source/destination attribution.
- LoxiLB does not persist firewall/SNAT rules across container restart in the tested container mode.
- No ready Prometheus `/metrics` endpoint was found on the tested API port.

Provisional product decision:

```text
Use LoxiLB as the primary v0 datapath candidate.
Keep nftables as a mandatory fallback.
Make betternat-agent own rule reconciliation, metrics re-export, and HA.
```

## Test Environment

AWS profile:

```text
The spike used a disposable AWS profile supplied through `AWS_PROFILE`.
```

Region/AZ:

```text
us-west-2 / us-west-2a
```

Spike tag:

```text
SpikeId=betternat-loxilb-20260620T000000Z
```

Topology:

```text
VPC: 10.77.0.0/16
Public subnet: 10.77.1.0/24
Private subnet: 10.77.2.0/24

Primary LoxiLB: i-0740c40f2e6526a88 / 10.77.1.125
Backup LoxiLB:  i-0c4e96e76f4f6cd38 / 10.77.1.65
Private client: i-03af5b25ea8f064f0 / 10.77.2.87
EIP: 52.43.35.239
```

Both LoxiLB instances:

- ran as Spot EC2 instances,
- used standalone container mode,
- had source/destination check disabled,
- ran `ghcr.io/loxilb-io/loxilb:latest`,
- reported LoxiLB `0.9.8.6-beta`,
- had eBPF loaded on `ens5`.

## Egress NAT Configuration

Primary rule:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:10.77.2.0/24,preference:100 \
  --snat=10.77.1.125 \
  --egress
```

Backup rule:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:10.77.2.0/24,preference:100 \
  --snat=10.77.1.65 \
  --egress
```

The private route table initially pointed `0.0.0.0/0` to the primary LoxiLB instance, then was changed to the backup with `ReplaceRoute`.

## Functional Tests

Before SNAT rule creation, the private client timed out on internet curls.

After SNAT rule creation:

```text
curl https://checkip.amazonaws.com -> 52.43.35.239
curl -I https://example.com -> HTTP/2 200
```

DNS/UDP:

```text
dig @1.1.1.1 example.com A -> 172.66.147.243, 104.20.23.154
dig @8.8.8.8 example.com A -> 172.66.147.243, 104.20.23.154
```

Concurrent short flows:

```text
30 parallel curl requests to checkip.amazonaws.com succeeded.
```

Downloads after failover:

```text
10MB file:  code=200 bytes=10485760 time=2.469496 speed=4246113
137MB file: code=200 bytes=144057680 time=0.509503 speed=282741573
```

The 137MB sample used a CDN-cached Linux kernel tarball, so this is only a functional/high-response-volume smoke test, not a rigorous throughput benchmark.

## Failover Test

Failover actions:

```text
associate-address --allocation-id eipalloc-06665976ac7daefe5 --instance-id backup --allow-reassociation
replace-route --route-table-id private-rt --destination-cidr-block 0.0.0.0/0 --instance-id backup
```

Observed CLI timing:

```text
EIP reassociation command: about 3.4s
ReplaceRoute command: about 1.6s
```

After failover, the route table showed:

```json
{
  "DestinationCidrBlock": "0.0.0.0/0",
  "InstanceId": "i-0c4e96e76f4f6cd38",
  "NetworkInterfaceId": "eni-059106c68f2e0810c",
  "State": "active"
}
```

EIP state:

```json
{
  "PublicIp": "52.43.35.239",
  "InstanceId": "i-0c4e96e76f4f6cd38",
  "PrivateIpAddress": "10.77.1.65"
}
```

Client validation after failover:

```text
try=1  52.43.35.239
try=2  52.43.35.239
...
try=10 52.43.35.239
curl -I https://example.com -> HTTP/2 200
dig @1.1.1.1 example.com A -> success
```

Interpretation:

- New connections recovered successfully after route/EIP failover.
- Public egress IP stayed the same.
- Active connection preservation was not tested and should not be promised.

## Observability Findings

LoxiLB firewall counters worked.

Primary after pre-failover traffic:

```text
counter="19448:151279514"
```

Backup after failover traffic:

```text
counter="10821:155385172"
```

Backup conntrack showed expected SNAT and DNAT entries:

```text
sourceIP=10.77.2.87 conntrackAct=snat-10.77.1.65:<port>:w0
destinationIP=10.77.1.65 conntrackAct=dnat-10.77.2.87:<port>:w0
protocol=tcp/udp
conntrackState=est / udp-est
```

This is enough for a BetterNAT exporter to build useful source/destination attribution, subject to cardinality controls.

Prometheus endpoint status:

```text
GET /metrics       -> 404
GET /prometheus    -> 404
GET /debug/metrics -> 404
```

The API port was `11111`, but no ready scrape endpoint was found in this run.

Recommendation:

- `betternat-agent` should poll LoxiLB's API/CLI and re-export normalized Prometheus metrics.
- Do not rely on upstream LoxiLB exposing exactly the BetterNAT metrics model.

## Persistence Findings

The tested LoxiLB container mode did not persist firewall/SNAT configuration across container restart.

Before restart:

```text
fwAttr contained the egress SNAT rule.
```

After:

```text
docker restart loxilb
loxicmd get firewall -o json -> "fwAttr": []
```

Product implication:

- BetterNAT must treat LoxiLB configuration as ephemeral runtime state.
- `betternat-agent` must reconcile desired config on boot, restart, failover, and periodic drift checks.
- Terraform should generate desired config; runtime reconciliation belongs in the agent.

## Security/IAM Findings

In datapath-only mode, LoxiLB did not need AWS API permissions.

AWS APIs were used by:

- Terraform/spike scripts for resource creation,
- failover operations: EIP reassociation and `ReplaceRoute`,
- SSM for test command execution.

This supports the intended split:

```text
LoxiLB: local datapath only
betternat-agent: AWS SDK failover, health, reconciliation
Terraform provider: resource lifecycle and desired state
```

## Cleanup Result

The spike resources were cleaned up after testing.

Verified:

- all three EC2 instances terminated,
- no VPCs found with the spike tag,
- no EIPs found with the spike tag,
- no subnets found with the spike tag,
- no security groups found with the spike tag.

## Updated Recommendation

LoxiLB should be the default datapath target for v0 development, with nftables retained as fallback.

Recommended v0 architecture:

```text
terraform-provider-betternat
  creates AWS resources and desired config

betternat-agent
  owns health checks, lease/fencing, route/EIP failover,
  LoxiLB rule reconciliation, metrics export, rollback metadata

LoxiLB
  owns packet datapath and conntrack on the active appliance

nftables
  remains fallback datapath engine
```

Do not claim yet:

- active connection preservation,
- final performance numbers,
- automatic LoxiLB config persistence,
- native Prometheus coverage,
- multi-cloud parity.

Next implementation step:

Build a local `datapath.Engine` interface and implement:

1. `loxilb` engine: rule apply/read/reconcile using `loxicmd` or API.
2. `nftables` engine: fallback masquerade/SNAT.
3. `metrics` exporter: normalize LoxiLB firewall/conntrack data into BetterNAT metrics.
