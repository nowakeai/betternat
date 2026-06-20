# Benchmark and SLO Design

Date: 2026-06-19

## Question

How should BetterNAT prove that it is fast enough, reliable enough, and cost-effective enough for tens-of-TB-per-month NAT workloads?

## Short Answer

Publish reproducible benchmark profiles instead of generic claims.

The product should benchmark:

1. NAT datapath throughput.
2. Packets per second.
3. New connections per second.
4. Concurrent connection capacity.
5. Conntrack pressure and failure behavior.
6. HA failover time for new connections.
7. Shared-EIP failover behavior.
8. Observability overhead.
9. Cost break-even.
10. Target workload profiles: blockchain/P2P sync, crawler fleets, Kubernetes image pulls, and public data ingestion.

Do not publish claims like "2-5 second failover" or "10 Gbps NAT" until measured on specific EC2 instance types and traffic profiles.

## Why Benchmarking Is A Product Feature

The target user has tens of TB per month through NAT Gateway. They need to know:

- Which instance type should I use?
- How much traffic can it sustain?
- What happens during bursts?
- How quickly does failover recover?
- What will break first?
- How much money do I save after EC2, EIP, monitoring, and HA overhead?

So benchmarks should be part of the public docs and release process, not a one-off engineering note.

## Important AWS Network Constraint

AWS EC2 documentation says instance network bandwidth depends on instance type/vCPU count, but instances may not achieve that bandwidth if they exceed instance-level network allowances such as packets per second or tracked connections.

Therefore, BetterNAT capacity profiles must include:

- Gbps.
- packets per second.
- flows/connections.
- new connections per second.
- ENA/network allowance exceeded counters.
- CPU softirq saturation.

Bandwidth alone is not enough.

## Benchmark Categories

## 0. Target Workload Profiles

BetterNAT's target workloads are not generic packet forwarding. The benchmark suite must include:

### Blockchain / P2P sync profile

```text
private RPC/full nodes
many public peers
long-lived TCP streams
large inbound response/sync volume
catch-up bursts
```

Measure:

- sustained throughput,
- concurrent peer connections,
- retransmits/resets,
- conntrack pressure,
- top destination peers,
- failover impact on active sync,
- time for application retry/recovery.

### Crawler profile

```text
many workers
many destinations
high new connections/sec
small requests
mixed/large responses
bursty traffic
```

Measure:

- new connections/sec,
- concurrent flows,
- destination diversity,
- p95/p99 request latency,
- conntrack insert failures,
- port exhaustion signals,
- cost by worker/team.

### Kubernetes image/artifact pull profile

```text
many nodes
large image layers/artifacts
bursts during scale-up/deploys
registry traffic
```

Measure:

- aggregate Gbps,
- pull completion time,
- node/source attribution,
- retry/failure rate,
- endpoint recommendation opportunities.

### Public data ingest profile

```text
large long-lived downloads
few to moderate destinations
high sustained throughput
application retries acceptable
```

Measure:

- sustained throughput,
- retransmits,
- failover impact on active downloads,
- time to recover new/retried downloads.

## 1. Datapath Throughput

Goal:

Measure maximum sustained NAT throughput through the appliance.

Traffic:

- TCP long-lived streams.
- UDP streams.
- realistic mixed traffic.
- 1500-byte packets.
- jumbo frame test only if the VPC path supports it and docs clearly mark it.

Metrics:

- Gbps in and out.
- CPU total and per-core.
- softirq usage.
- packet drops.
- retransmits.
- latency p50/p95/p99.
- ENA allowance exceeded counters.
- interface rx/tx errors/drops.

Result format:

```text
instance_type: c7gn.large
datapath: loxilb
traffic: TCP 1500B long-lived
sustained_throughput_gbps: X
cpu_p95: Y%
softirq_p95: Z%
drops: 0
duration: 30m
```

## 2. Packets Per Second

Goal:

Find the pps ceiling. This matters because small packets can saturate CPU/pps before Gbps looks high.

Traffic:

- 64-byte UDP.
- 128-byte UDP.
- 512-byte UDP.
- 1500-byte UDP.
- mixed packet sizes.

Metrics:

- packets/sec forwarded.
- drops/sec.
- CPU softirq.
- ksoftirqd usage.
- ENA allowance exceeded counters.
- nftables/conntrack counters.

Pass condition:

- No drops for sustained test window at the published profile rate.

## 3. New Connections Per Second

Goal:

Measure connection churn. NAT workloads often fail on connection creation rate before they fail on steady throughput.

Traffic:

- short-lived TCP connections.
- HTTP/HTTPS-like request patterns.
- DNS-like UDP churn.

Metrics:

- successful new connections/sec.
- failed connections/sec.
- TCP retransmits/resets.
- conntrack insert failures.
- conntrack table growth.
- CPU usage.

Important:

NAT Gateway has metrics such as `ActiveConnectionCount`, `ConnectionAttemptCount`, `ErrorPortAllocation`, and drop/error counters. BetterNAT should publish analogous metrics so users can compare behavior.

## 4. Concurrent Connection Capacity

Goal:

Find safe limits before conntrack pressure causes drops or high latency.

Test:

- Open N concurrent TCP connections.
- Hold them for multiple timeout windows.
- Add new connection churn on top.
- Repeat for UDP pseudo-flows.

Metrics:

- `nf_conntrack_count`.
- `nf_conntrack_max`.
- conntrack insert failed.
- conntrack drops.
- memory usage.
- latency.
- CPU.

Capacity profile should specify:

```text
recommended_max_concurrent_flows
default_nf_conntrack_max
memory_required
alert_thresholds
```

## 5. Failure Behavior Under Pressure

Goal:

Document what happens when the appliance is overloaded.

Scenarios:

- conntrack table full.
- CPU softirq saturation.
- pps overload.
- EIP association failure.
- route replacement failure.
- DynamoDB lease write throttling/failure.
- AWS API throttling.
- agent restart.
- nftables reload failure.

Expected behavior:

- Fail loudly.
- Export metrics.
- Avoid split-brain.
- Do not silently blackhole traffic.
- Keep rollback possible.

## 6. HA Failover Benchmark

Goal:

Measure recovery for new outbound connections.

Scenarios:

- agent process killed.
- NAT instance stopped.
- public interface failure simulation.
- private interface failure simulation.
- route table permission denied.
- DynamoDB unavailable/throttled.
- active loses peer heartbeat but still has AWS API.
- standby loses AWS API but can hear active.

Metrics:

- failure detection time.
- lease acquisition time.
- route replacement API latency.
- EIP reassociation API latency, if enabled.
- route verification time.
- time until new probe connection succeeds.
- time until expected public egress IP is observed.
- existing connection survival rate.

Published SLO should be framed as:

```text
new_connection_recovery_p50
new_connection_recovery_p95
new_connection_recovery_p99
```

Do not frame it as "all traffic restored" unless existing connection behavior is measured and scoped.

## 7. Shared-EIP Benchmark

Goal:

If stable egress IP is enabled, prove that new connections after failover use the same EIP.

Test:

```text
before failover:
  curl ifconfig endpoint from private probe -> EIP-X

trigger failover

after failover:
  repeat curl until success -> EIP-X
```

Metrics:

- time to route success.
- time to EIP association visible.
- time to observed source IP == expected EIP.
- any interval where standby emitted traffic from wrong EIP.

The provider/agent should not mark the gateway `ACTIVE` until the outbound source IP probe confirms the expected EIP.

## 8. Observability Overhead

Goal:

Measure the cost of metrics and eBPF flow accounting.

Compare:

```text
baseline:
  LoxiLB NAT only

metrics:
  LoxiLB NAT + BetterNAT Prometheus exporter

fallback:
  nftables NAT + BetterNAT Prometheus exporter
```

Metrics:

- throughput delta.
- pps delta.
- CPU delta.
- memory delta.
- metric cardinality.
- map pressure.
- exporter scrape duration.

Product rule:

Do not enable high-cardinality per-pod/per-destination Prometheus labels by default.

## 9. Cost Benchmark

Goal:

Publish concrete cost profiles.

Inputs:

- region.
- monthly processed GB.
- AZ count.
- instance type.
- active/standby count.
- EIP count.
- EBS.
- CloudWatch/metrics/logging.
- expected cross-AZ cost, if any.

Outputs:

- AWS NAT Gateway estimated monthly cost.
- BetterNAT estimated monthly cost.
- break-even GB/month.
- savings at 10/30/50/100 TB.

Cost profile should clearly state:

- NAT Gateway processing fee avoided.
- standard data transfer not avoided.
- operational ownership not priced unless user supplies an hourly ops cost.

## Benchmark Topology

Minimum AWS topology:

```text
public subnet:
  BetterNAT active
  BetterNAT standby

private subnet:
  traffic generators
  traffic receivers or public internet endpoints

route:
  private route table 0.0.0.0/0 -> active NAT target
```

For controlled throughput tests, use private receiver instances in another subnet/VPC path only if the route still exercises NAT semantics. For public-internet tests, document internet path variability.

Preferred controlled setup:

- traffic generators in private subnet behind NAT.
- receivers on public IPs or controlled external environment.
- separate tests for intra-region limitations vs real internet egress.

## Tools

Candidate tools:

- `iperf3` for TCP/UDP throughput.
- `wrk`, `vegeta`, or `h2load` for HTTP connection churn.
- `pktgen`, `trafgen`, or DPDK tools for pps tests where appropriate.
- `conntrack` for conntrack state.
- `nft` for counters.
- `ethtool -S` for ENA/interface counters.
- `sar`, `mpstat`, `pidstat` for CPU/softirq.
- CloudWatch for EC2 and NAT Gateway comparison.
- custom probe daemon for failover measurement.

The benchmark harness should automate collection and write machine-readable results:

```json
{
  "version": "0.1.0",
  "region": "us-west-2",
  "instance_type": "c7gn.large",
  "datapath": "nftables",
  "test": "tcp-throughput-1500b",
  "duration_seconds": 1800,
  "results": {
    "throughput_gbps_p50": 0,
    "throughput_gbps_p95": 0,
    "drops_total": 0
  }
}
```

## Instance Profile Publishing

Do not say:

> Use c7gn.large for 50 TB/month.

Monthly TB alone is not enough.

Instead publish profiles like:

```text
Profile: steady-throughput-small
Traffic shape:
  average: X Mbps
  peak: Y Gbps
  pps: Z
  new_conn_per_sec: N
  concurrent_flows: M
Recommended:
  instance_type: ...
  conntrack_max: ...
  HA mode: ...
```

Required user inputs:

- monthly GB,
- peak Gbps,
- p95 pps,
- p95 new connections/sec,
- p95 concurrent connections,
- stable egress IP required or not.

If users only know monthly TB, the product can estimate average bandwidth:

```text
average_mbps = monthly_TB * 1024 * 8 * 1,000,000 / seconds_in_month / 1,000,000
```

But it must warn that peak traffic and pps determine instance sizing.

## Release Gate

Before calling a release production-ready:

- Benchmark at least three instance sizes.
- Run sustained 30-minute and 24-hour soak tests.
- Run HA failover tests at idle and under load.
- Run shared-EIP failover test.
- Run conntrack pressure test.
- Run agent restart test.
- Run Terraform provider create/update/delete/import tests.
- Publish raw benchmark artifacts.

## Public Claims

Safe before full benchmark:

- "Designed for high-volume NAT Gateway cost reduction."
- "Publishes reproducible instance profiles."
- "Measures new-connection failover recovery."

Unsafe before full benchmark:

- "10 Gbps NAT appliance."
- "2-5 second failover."
- "Zero packet loss failover."
- "Handles 50 TB/month" without traffic-shape qualifiers.
- "Drop-in AWS NAT Gateway replacement."

## Decision

Benchmark/SLO work should be part of MVP, not a later marketing task.

Required v0 benchmark deliverables:

1. Reproducible AWS benchmark harness.
2. At least one published AWS instance profile.
3. HA failover measurement for route-only mode.
4. HA failover measurement for shared-EIP mode.
5. Cost break-even calculator.
6. Clear statement of what is not guaranteed.

## Sources

- Amazon EC2 instance network bandwidth: instance bandwidth depends on instance type, and instances may not achieve bandwidth if they exceed allowances such as packets per second or tracked connections: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-network-bandwidth.html
- Enhanced networking on EC2 and ENA: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/enhanced-networking.html
- Monitor network performance for ENA settings and network allowance metrics: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-network-performance-ena.html
- Test whether ENA is enabled with `ethtool -i`: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/test-enhanced-networking-ena.html
- NAT Gateway CloudWatch metrics, including active connections, bytes, packets, drops, and error metrics: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html
- Monitor NAT Gateway with CloudWatch metrics at 1-minute intervals: https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway-cloudwatch.html
- Linux kernel nf_conntrack sysctl documentation: https://docs.kernel.org/networking/nf_conntrack-sysctl.html
