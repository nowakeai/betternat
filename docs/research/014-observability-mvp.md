# Observability MVP

Date: 2026-06-19

## Question

What should BetterNAT's observability MVP include, and how should it avoid excessive overhead or Prometheus cardinality problems?

## Short Answer

Current decision as of 2026-06-25:

The observability MVP is now LoxiLB-first. v0 should poll LoxiLB firewall counters and conntrack state, normalize them inside `betternat-agent`, and export BetterNAT Prometheus metrics. nftables/conntrack metrics are legacy diagnostics while that code remains. The tested LoxiLB API did not expose `/metrics`, so native LoxiLB scraping is not assumed.

Superseded fallback note: BetterNAT no longer has a product fallback datapath.
Existing nftables/nf_conntrack metrics are legacy diagnostics while the code
remains; they are not a release or operator fallback path. Current source of
truth: `docs/architecture.md`, `docs/spec-v0.md`, and
`docs/research/055-no-nftables-fallback-decision.md`.

Observability is one of the core product differentiators. MVP should answer:

- who is using NAT traffic,
- where traffic is going,
- whether the NAT appliance is healthy,
- whether conntrack/network limits are close,
- what happened during failover,
- how much NAT Gateway cost this traffic would have implied.

v0 should ship useful observability without writing custom eBPF. LoxiLB provides the primary datapath counters and conntrack data; BetterNAT re-exports normalized metrics. v1 can still add custom eBPF flow accounting if LoxiLB-derived attribution is insufficient.

Do not expose unbounded per-flow labels in Prometheus.

## Observability Goals

The product should answer operational questions:

```text
Which source IP/subnet/team used the most bytes?
Which destinations are responsible for egress cost?
Is conntrack close to full?
Are packets being dropped?
Is the appliance CPU/pps bound?
Which node is active?
Did failover happen?
Did failover preserve the expected EIP for new connections?
How much would this traffic have cost through NAT Gateway?
```

AWS NAT Gateway metrics are gateway-level. BetterNAT should differentiate with source attribution and appliance-local health.

## Observability Layers

### Layer 0: Cloud/provider metrics

Sources:

- EC2 instance metrics.
- ENA/network allowance metrics.
- CloudWatch logs/events.
- NAT Gateway metrics for comparison during migration.

Use:

- baseline instance health,
- capacity planning,
- migration comparison.

### Layer 1: Linux datapath metrics

Sources:

- interface counters,
- nftables counters,
- conntrack counters,
- sysctls,
- CPU softirq.

Use:

- v0 MVP.
- no eBPF dependency.
- basic health and capacity signals.

### Layer 2: eBPF flow accounting

Sources:

- TC ingress/egress programs,
- per-CPU maps,
- ring buffer for sampled events.

Use:

- v1 attribution.
- top sources/destinations.
- richer per-protocol accounting.

### Layer 3: Metadata enrichment

Sources:

- static CIDR/team config,
- AWS tags/ENI/instance metadata,
- Kubernetes API pod IP mapping,
- optional CMDB import.

Use:

- turn IPs into teams/workloads/cost centers.

## v0 Metrics: No eBPF Required

### Agent and HA

```text
betternat_agent_up
betternat_agent_build_info{version,commit}
betternat_active{gateway,ha_group,node}
betternat_ha_state{state}
betternat_lease_generation
betternat_lease_renew_errors_total
betternat_failover_events_total{reason,result}
betternat_failover_duration_seconds{phase}
betternat_cloud_api_latency_seconds{operation}
betternat_route_target_match
betternat_public_identity_match
```

### Datapath

```text
betternat_ip_forwarding_enabled
betternat_nftables_rules_loaded
betternat_conntrack_entries
betternat_conntrack_limit
betternat_conntrack_usage_ratio
betternat_conntrack_insert_failed_total
betternat_conntrack_drop_total
betternat_interface_rx_bytes_total{interface}
betternat_interface_tx_bytes_total{interface}
betternat_interface_rx_packets_total{interface}
betternat_interface_tx_packets_total{interface}
betternat_interface_rx_drops_total{interface}
betternat_interface_tx_drops_total{interface}
betternat_softirq_seconds_total{cpu}
```

### Cost

```text
betternat_processed_bytes_total{direction}
betternat_estimated_nat_gateway_cost_usd_total{region}
betternat_estimated_nat_gateway_processing_cost_usd_total{region}
```

v0 cost attribution by team can be done with configured CIDR-level nftables counters:

```yaml
owners:
  - name: payments
    cidrs: ["10.20.0.0/20"]
  - name: analytics
    cidrs: ["10.40.0.0/16"]
```

Then generate nftables counters per owner/CIDR.

Limit:

- This works for coarse subnet/team attribution.
- It is not ideal for per-IP top talkers.

## v1 Metrics: eBPF Flow Accounting

eBPF should provide bounded top-N and aggregate counters.

Desired dimensions:

- source IP,
- source owner/team,
- destination IP or destination CIDR,
- protocol,
- destination port,
- direction,
- verdict/drop reason where available.

Implementation pattern:

```text
TC ingress/egress observe packet
extract src/dst/proto/port/bytes
update per-CPU counters
agent periodically aggregates
agent exports bounded metrics and top-N CLI views
```

Prometheus should not get every raw source/destination pair by default.

## Prometheus Cardinality Policy

Safe labels:

- gateway,
- ha_group,
- node,
- interface,
- direction,
- protocol,
- owner/team,
- source_subnet,
- state,
- operation,
- result.

Risky labels:

- source IP,
- destination IP,
- destination domain,
- pod name,
- full 5-tuple,
- URL/path.

Default rule:

```text
Prometheus gets stable aggregates.
CLI or flow store gets high-cardinality top-N/detail.
```

Examples:

Good:

```text
betternat_owner_bytes_total{owner="payments",direction="egress"}
```

Risky:

```text
betternat_flow_bytes_total{src_ip="10.0.1.23",dst_ip="1.2.3.4",dst_port="443"}
```

Allow advanced users to enable high-cardinality labels with explicit warnings.

## CLI UX

Required commands:

```sh
betternat status
betternat top sources --window 1h
betternat top destinations --window 1h
betternat top owners --window 24h
betternat cost estimate --window 24h
betternat failover history
betternat doctor
```

Example:

```text
$ betternat top sources --window 1h
SOURCE TYPE   NAME                  BYTES    EST_NATGW_COST
owner         analytics             420 GB   $18.90
pod           prod/api              180 GB   $8.10
vm            i-abc123              95 GB    $4.28
subnet        10.40.0.0/16          70 GB    $3.15
unknown       10.0.99.44            12 GB    $0.54
```

## Grafana Dashboards

Ship dashboards as JSON:

### Overview

- active node,
- throughput,
- estimated NAT Gateway equivalent cost,
- conntrack usage,
- drops/errors,
- failover events.

### Cost Attribution

- bytes by owner/team,
- estimated cost by owner/team,
- top source subnets,
- top EKS node/pod/workload if enabled.

### Capacity

- CPU/softirq,
- pps,
- conntrack usage,
- ENA allowance counters,
- interface drops,
- new connections/sec when available.

### HA

- active owner timeline,
- lease generation,
- route target match,
- EIP match,
- failover duration phases,
- cloud API latency.

## Flow Store: Needed Or Not?

MVP should not require a database.

v0/v1 can keep:

- in-memory rolling top-N,
- Prometheus aggregates,
- optional log samples.

Later, add optional flow export:

- OTLP,
- ClickHouse,
- S3 parquet,
- Loki logs,
- CloudWatch Logs.

Do not build a custom flow database in v0.

## Domain Attribution

Domain attribution is useful but best-effort.

Methods:

- observe DNS responses at NAT appliance,
- ingest Route 53 Resolver Query Logs,
- correlate destination IPs to recent DNS answers,
- Kubernetes CoreDNS logs if available.

Limitations:

- DNS cache hides queries,
- DoH/DoT bypasses observable DNS,
- many domains share IPs,
- CDNs rotate addresses,
- service mesh/proxy hides original destination.

Product claim:

> Best-effort domain attribution when DNS telemetry is available.

Not:

> Exact domain-level billing.

## EKS/Kubernetes Integration

Use conclusions from `009-observability-eks-pod-attribution.md`:

- default EKS node SNAT means node-level attribution only,
- pod-level attribution requires pod source IP preservation or cluster-side telemetry,
- Kubernetes metadata mapping should be optional v1.

Dashboard should gracefully mix:

- VM,
- node,
- pod,
- subnet,
- owner,
- unknown.

## Alerts

Default alert rules:

```text
ConntrackUsageHigh
ConntrackInsertFailures
InterfaceDrops
HighSoftirq
RouteTargetMismatch
PublicIdentityMismatch
LeaseRenewFailures
FailoverOccurred
AgentDown
UnexpectedSourceCIDR
EgressCostSpike
```

Alert thresholds must be conservative and tunable.

## Cost Attribution

Cost calculation:

```text
estimated_nat_gateway_processing_cost =
  processed_gb * region_nat_gateway_processing_price_per_gb
```

Need:

- region-aware pricing source or config,
- owner/team mapping,
- windowed byte counters,
- reminder that standard data transfer out is not avoided.

Cost dashboard should show:

- estimated avoided NAT Gateway processing cost,
- top owners by estimated processing cost,
- unknown/unattributed traffic percentage.

## Observability MVP Scope

v0:

- Prometheus exporter.
- HA metrics.
- route/EIP verification metrics.
- conntrack metrics.
- interface and datapath metrics.
- estimated cost total.
- CIDR/team attribution by LoxiLB firewall counters and conntrack summaries.
- legacy diagnostic CIDR/team attribution by nftables counters while that code
  remains.
- Grafana overview dashboard.
- `top owners` using configured CIDRs.

v1:

- eBPF flow accounting.
- top source IPs.
- top destination IPs/CIDRs.
- Kubernetes metadata enrichment.
- richer cost attribution.
- more dashboards.

v2:

- optional flow export.
- domain attribution integrations.
- Cilium/Hubble/service mesh integrations.

## Decision

BetterNAT should lead with observability, but ship it in stages:

1. v0: reliable appliance health and coarse cost attribution.
2. v1: eBPF-powered flow attribution.
3. v2: richer metadata and flow export.

Keep Prometheus low-cardinality by default. Put high-cardinality detail in CLI top-N or optional flow export.
