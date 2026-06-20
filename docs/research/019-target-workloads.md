# Target Workloads

Date: 2026-06-19

## Question

Which workloads best illustrate BetterNAT's core cost pain point?

## Short Answer

BetterNAT should target high-volume private workloads where NAT Gateway processed-byte charges are dominated by return/download traffic.

The core pain point is not a single vertical like crawlers or Kubernetes. The core pain point is:

```text
private workload initiates outbound connection
public internet peer/service sends large response or stream back
NAT Gateway charges processed GB for both directions
```

BetterNAT removes the managed NAT Gateway per-GB processing fee for that traffic path by replacing it with a self-owned egress appliance.

Important workload examples:

1. Blockchain RPC / full nodes syncing large amounts of P2P data from public peers.
2. Large-scale crawler/scraper clusters.
3. Kubernetes nodes frequently pulling large container images/artifacts.
4. Data ingestion workers pulling public internet data into private storage.
5. Batch/ETL systems downloading large datasets from SaaS, partner APIs, object stores, or public mirrors.

This should be presented as a billing-model pain point, not as a narrow workload category.

## Why These Workloads Fit

These workloads often send outbound requests, handshakes, or peer messages and receive much larger response streams:

```text
private node/worker -> request/handshake/query
internet peer/service -> large response/stream/download -> private node/worker
```

For AWS NAT Gateway:

```text
outbound request bytes + inbound response bytes = NAT Gateway processed bytes
```

The response/download side can dominate the bill.

BetterNAT replaces the managed per-GB NAT Gateway processing fee with a self-owned NAT appliance cost. Standard data transfer pricing still applies, but the NAT Gateway processing line item can be reduced or removed.

## Primary Persona

Platform or infrastructure team running:

- private subnets,
- high-volume outbound-initiated fetch/download/P2P workloads,
- NAT Gateway bill measured in tens of TB/month or more,
- need for stable egress IPs,
- need to understand which team/workload generated traffic,
- willingness to own an appliance if it saves meaningful cost.

## Workload 0: Blockchain RPC / Full Nodes / P2P Sync

### Traffic Shape

- Nodes initiate outbound P2P connections to public peers.
- Peers send large blocks, state data, mempool traffic, snapshots, or historical sync data back.
- Traffic is often long-lived and high-volume.
- Peer count can be high.
- Source identity may be VM/node/pod depending on deployment.
- Stable public egress IP can matter for peer reputation, firewall policy, or operational consistency.

### Product Requirements

- sustained throughput benchmark,
- concurrent connection tracking,
- long-lived TCP behavior,
- per-node/source attribution,
- peer/destination visibility,
- stable egress IP mode,
- HA with clear retry behavior,
- conntrack pressure and port exhaustion visibility.

### Cost Pain

From the node's perspective, much of the data is "ingress" because the node is pulling/syncing data from public peers. But because the flow was initiated from a private subnet, the return traffic traverses NAT Gateway and contributes to NAT Gateway processed GB.

This can create large NAT Gateway bills even when the architecture feels like it is "downloading public data into AWS."

### BetterNAT Fit

Very strong.

This is one of the clearest BetterNAT use cases:

```text
private blockchain node -> public peers
public peers -> large sync/stream data -> private blockchain node
```

BetterNAT should make this a first-class example in docs and cost calculator scenarios.

## Workload 1: Large-scale Crawler / Scraper Clusters

### Traffic Shape

- Many outbound connections.
- High new connections/sec.
- Many destination IPs/domains.
- Often large response bodies.
- Burst-heavy.
- May require stable egress IP pools or allowlisted IPs.

### Product Requirements

- high new connection/sec benchmark,
- high concurrent connection tracking,
- per-source/team attribution,
- top destination/domain best-effort reporting,
- stable egress IP mode,
- route/EIP HA,
- port exhaustion visibility,
- rate/destination visibility.

### Risks

- public target sites may rate-limit or block egress IPs,
- many small objects can be pps/new-connection bound,
- DNS/domain attribution is best-effort,
- legal/ToS considerations are outside product scope.

### BetterNAT Fit

Very strong if the user owns the crawling infrastructure and needs lower NAT processing fees plus visibility.

## Workload 2: Kubernetes Nodes Pulling Large Images / Artifacts

### Traffic Shape

- Large downloads from registries and artifact stores.
- Burst during node scale-up, rolling deploys, cluster upgrades.
- Traffic source may appear as node IP if node-level SNAT occurs.
- Some traffic can be removed with VPC endpoints depending on registry/service.

### Product Requirements

- node-level attribution,
- EKS/Kubernetes metadata optional,
- high-throughput download benchmark,
- burst handling,
- VPC endpoint recommendations for ECR/S3 where applicable,
- alert on cost spikes during deployments.

### Important Optimization

Do not route avoidable AWS service traffic through BetterNAT if VPC endpoints solve it better.

Examples:

- ECR API/DKR endpoints,
- S3 gateway endpoint for image layers or artifacts where applicable,
- CloudWatch Logs endpoint,
- STS/KMS/Secrets Manager endpoints if relevant.

BetterNAT should recommend endpoints before claiming savings.

### BetterNAT Fit

Strong for:

- non-AWS registries,
- public registries,
- cross-cloud artifact pulls,
- self-hosted artifact stores outside AWS,
- workloads where endpoints are unavailable or incomplete.

Less compelling when:

- all heavy pulls can use cheap/free VPC endpoints.

## Workload 3: Public Data Ingestion / Data Lake Ingest

### Traffic Shape

- relatively few request streams or jobs,
- very large downloads,
- long-lived TCP connections,
- high throughput,
- less connection churn than crawler workloads.

### Product Requirements

- sustained throughput benchmark,
- stable egress IP if data providers allowlist,
- source/team/cost attribution,
- failover behavior for long-lived downloads,
- retry guidance because active downloads may reset during failover.

### BetterNAT Fit

Very strong for high-volume downloads into private storage, especially when NAT Gateway processing fees dominate and application retries are acceptable.

## Product Positioning Update

BetterNAT should not be positioned as only a crawler/image-pull/data-ingest tool.

It should say:

> BetterNAT is for private workloads that pull or receive large amounts of public internet data through NAT Gateway. NAT Gateway charges processed GB in both directions; BetterNAT removes the managed NAT processing fee while keeping controlled egress, attribution, and failover.

Examples can include:

- blockchain RPC/full nodes syncing from P2P peers,
- crawler fleets,
- Kubernetes image/artifact pulls,
- public data ingestion pipelines.

Suggested headline:

> Better NAT for high-volume cloud ingress over private egress.

Suggested subhead:

> BetterNAT helps private workloads that pull large public data streams cut managed NAT processing fees, attribute traffic by source, and fail over without losing a stable outbound IP.

Optional wordplay:

> Better not pay managed NAT fees just to bring public data into your private network.

## Benchmark Implications

Benchmark suite must include these traffic shapes:

### Blockchain P2P sync profile

```text
long-lived TCP connections
many public peers
large inbound response/stream volume
moderate to high concurrent connections
periodic bursts during sync/catch-up
```

Metrics:

- sustained throughput,
- concurrent flows,
- retransmits/resets,
- conntrack pressure,
- top peer/destination IPs,
- failover impact on active sync,
- retry recovery behavior.

### Crawler profile

```text
high new connections/sec
mixed response sizes
many destinations
DNS churn
burst traffic
```

Metrics:

- successful new connections/sec,
- p95/p99 latency,
- conntrack insert failures,
- port exhaustion,
- pps,
- CPU softirq.

### Image pull profile

```text
N nodes pulling M image layers concurrently
large HTTP range/download traffic
burst during cluster scale-up
```

Metrics:

- aggregate Gbps,
- time to complete pulls,
- drops/retries,
- source node attribution,
- estimated NAT Gateway processing cost avoided.

### Data ingest profile

```text
long-lived TCP downloads
large objects
few to moderate destinations
```

Metrics:

- sustained throughput,
- p95 CPU/softirq,
- retransmits,
- failover impact on active downloads,
- retry recovery time.

## Observability Implications

Default dashboards should include:

- download-heavy traffic ratio,
- top source workers/nodes,
- top destination domains/IPs best-effort,
- crawler burst timeline,
- image-pull spike detection,
- cost by source/team,
- response/download bytes vs request bytes if direction can be inferred.

CLI examples:

```sh
betternat top sources --profile crawler --window 1h
betternat top destinations --window 1h
betternat cost attribution --group-by team --window 24h
betternat recommend-endpoints --profile k8s-image-pulls
```

## Cost Calculator Implications

Calculator should explicitly support:

```text
download_gb_per_month
upload_request_gb_per_month
```

Display:

```text
NAT Gateway processed GB:
  requests: X GB
  responses/downloads: Y GB
  total: X + Y GB
```

For image pulls:

Inputs:

- nodes,
- average image size,
- deploys per day,
- node churn/scale events,
- registry type: ECR/public DockerHub/GHCR/custom/cross-cloud.

For crawlers:

- requests/day,
- average response size,
- concurrent workers,
- destination diversity.

For ingestion:

- TB/day,
- number of sources,
- retry tolerance.

For blockchain/P2P nodes:

- number of nodes,
- average sync/download GB per node per day,
- peer count,
- long-lived connection count,
- catch-up/snapshot frequency,
- stable egress IP requirement.

## Terraform Provider UX Implications

Provider could expose workload profiles:

```hcl
resource "betternat_gateway" "egress" {
  workload_profile = "crawler" # crawler | k8s_image_pulls | data_ingest | generic
}
```

This can tune:

- default metrics dashboard,
- benchmark recommendation,
- conntrack defaults,
- alerts,
- cost calculator prompts.

Do not overfit datapath behavior solely based on profile in v0.

## What Not To Claim

Avoid:

- "Free ingress."
- "Free downloads."
- "No data transfer cost."
- "BetterNAT makes crawling undetectable."
- "Guaranteed uninterrupted downloads during failover."

Use:

- "Removes managed NAT Gateway per-GB processing fees for traffic that would otherwise traverse NAT Gateway."
- "Standard cloud data transfer charges still apply."
- "Existing downloads may need retry during failover."

## Decision

Update target positioning:

> BetterNAT is for high-volume private workloads that initiate outbound connections and receive large amounts of public internet data back through NAT Gateway. This includes blockchain RPC/full nodes syncing from public peers, crawler fleets, Kubernetes image/artifact pulls, and data ingestion pipelines.

This target should influence:

- benchmark profiles,
- cost calculator inputs,
- observability dashboards,
- README examples,
- Terraform workload profiles.
