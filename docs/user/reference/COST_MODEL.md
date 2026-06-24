# BetterNAT Cost Model

Date: 2026-06-22

## Purpose

BetterNAT is designed for private-subnet workloads where NAT Gateway data processing charges dominate the bill.

It does not make AWS networking free. It replaces the managed NAT Gateway per-GB processing line item with a self-owned EC2 node pool, while normal AWS costs still apply.

## Fast Evaluation

Start with the NAT Gateway traffic already visible in CloudWatch or Cost
Explorer. Do not try to model every AWS networking line item first.

For a private-subnet workload that initiates internet connections, a practical
first estimate is:

```text
monthly_processed_gb =
  (BytesOutToDestination + BytesInFromDestination) / 1024 / 1024 / 1024
```

Use one side of the gateway path for this estimate. Do not add all four NAT
Gateway byte metrics together, or you will double-count packets crossing the
gateway.

Then run the BetterNAT CLI estimate:

```sh
betternat cost estimate \
  --gb <monthly-processed-gb> \
  --nat-gateway-hourly <nat-gateway-hourly-price> \
  --nat-gateway-processing-per-gb <nat-gateway-processing-price> \
  --node-hourly <expected-betternat-node-hourly-price> \
  --nodes 2
```

Example for `50 TB/month`:

```sh
betternat cost estimate \
  --gb 51200 \
  --nat-gateway-hourly 0.045 \
  --nat-gateway-processing-per-gb 0.045 \
  --node-hourly 0.05 \
  --nodes 2
```

Continue the evaluation when:

- NAT Gateway processing is a meaningful bill line,
- the workload sends small requests and downloads large responses,
- the remaining non-cost tradeoffs in [Limitations](LIMITATIONS.md) are
  acceptable.

Stop early when:

- most NAT traffic can move to VPC endpoints,
- standard internet egress transfer dominates the bill.

For reliability, failover, bootstrap, and ownership go/no-go decisions, use
[Limitations](LIMITATIONS.md) rather than this cost model.

## The Bill Line BetterNAT Targets

AWS NAT Gateway pricing has two main NAT Gateway-specific components:

- NAT Gateway hours,
- data processed per GB.

AWS also says standard data transfer charges still apply for data transferred through NAT Gateway.

The important point for high-volume private-subnet workloads:

> NAT Gateway data processing is charged for each GB processed through the gateway, regardless of traffic source or destination.

For a private workload that initiates an outbound connection and receives a large response, the response traffic returns through the NAT Gateway and contributes to processed GB.

Example:

```text
private worker -> small request -> public peer/API/registry
public peer/API/registry -> large response/download -> private worker
```

That large return path can be the expensive part of the NAT Gateway bill.

BetterNAT is especially relevant for:

- blockchain/RPC/full nodes syncing from public peers,
- crawler fleets pulling large pages, media, or datasets,
- Kubernetes nodes pulling large public images or artifacts,
- data ingestion workers downloading from public APIs or partner endpoints,
- any private subnet workload with tens of TB/month through NAT Gateway.

## Direction Mix Matters

NAT Gateway processed data is bidirectional:

```text
nat_gateway_processed_gb =
  private_to_internet_request_or_upload_gb
  + internet_to_private_response_or_download_gb
```

BetterNAT does not add an equivalent per-GB NAT processing fee. The remaining AWS data-transfer bill depends on direction:

- private-to-internet upload/egress still pays normal AWS internet data transfer out in both designs,
- internet-to-private download/response traffic often has no equivalent AWS data transfer-in charge, but NAT Gateway still charges processed GB while it is in the path,
- this makes BetterNAT especially attractive for workloads that send small requests and download large responses.

Examples for `50 TB/month` total traffic through the NAT layer:

| Traffic shape | Ingress/download | Egress/upload | NAT Gateway processed GB | Standard transfer implication |
| --- | ---: | ---: | ---: | --- |
| Download-heavy sync/crawling | 80% / 40 TB | 20% / 10 TB | 50 TB | NAT Gateway charges the 40 TB return path; BetterNAT removes that NAT processing line, while standard internet egress applies to the 10 TB outbound side in both designs. |
| Balanced API traffic | 50% / 25 TB | 50% / 25 TB | 50 TB | NAT Gateway charges both directions; BetterNAT removes NAT processing, but standard internet egress remains for 25 TB. |
| Upload-heavy export | 20% / 10 TB | 80% / 40 TB | 50 TB | NAT processing savings are still real, but normal AWS internet egress transfer on 40 TB can dominate the total bill in both designs. |

## Direction-Sensitive Savings Examples

These examples use:

- `50 TB/month` through the NAT layer,
- `$0.045/GB` NAT Gateway processing,
- one NAT Gateway at `$0.045/hour`,
- two BetterNAT nodes at `$0.05/hour` each,
- `730` hours/month,
- illustrative standard internet egress transfer at `$0.09/GB`.

| Traffic mix | NAT Gateway design | BetterNAT design | Estimated savings | Savings percent |
| --- | ---: | ---: | ---: | ---: |
| 80% ingress / 20% egress | about `$3,258/month` | about `$995/month` | about `$2,264/month` | about `69%` |
| 50% ingress / 50% egress | about `$4,641/month` | about `$2,377/month` | about `$2,264/month` | about `49%` |
| 20% ingress / 80% egress | about `$6,023/month` | about `$3,759/month` | about `$2,264/month` | about `38%` |

Assumptions:

- `1 TB = 1024 GB`,
- `730` hours/month,
- NAT Gateway design includes one NAT Gateway hourly charge, NAT Gateway processed GB, and illustrative standard internet egress transfer,
- BetterNAT design includes EC2 node instance hours and the same illustrative standard internet egress transfer,
- ingress/download is from the private workload's point of view,
- egress/upload is traffic from the private workload to the internet,
- excludes EBS, EIP/public IPv4, DynamoDB, monitoring, and operational costs.

Total bill shape:

```text
nat_gateway_design_total =
  nat_gateway_specific_cost
  + standard_aws_data_transfer_by_direction
  + other_aws_resource_costs

betternat_design_total =
  betternat_specific_cost
  + standard_aws_data_transfer_by_direction
  + other_aws_resource_costs
```

Pricing varies by region and can change. Always verify current AWS pricing for your region.

## CLI NAT-Layer Estimate

The CLI estimates the NAT-specific bill line only. It does not model standard
AWS data transfer by direction yet.

This CLI example uses:

- `50 TB/month` processed by NAT Gateway,
- `$0.045/hour` NAT Gateway hourly price,
- `$0.045/GB` NAT Gateway processing price,
- `730` hours/month,
- two BetterNAT nodes,
- `$0.05/hour` per node.

Run:

```sh
betternat cost estimate \
  --gb 51200 \
  --nat-gateway-hourly 0.045 \
  --nat-gateway-processing-per-gb 0.045 \
  --node-hourly 0.05 \
  --nodes 2
```

Example output:

```json
{
  "processed_gb": 51200,
  "nat_gateway_usd": 2336.85,
  "betternat_usd": 73,
  "estimated_savings_usd": 2263.85,
  "savings_percent": 96.87613667971843
}
```

This is not a quote. It is a NAT-layer estimate for deciding whether the workload is worth deeper modeling.

Region-specific prices are not fetched automatically. Override prices
explicitly with your region's NAT Gateway price, your chosen EC2 instance price,
and your expected node count.

## Formula

Approximate NAT Gateway monthly cost:

```text
processed_gb =
  private_to_internet_request_or_upload_gb
  + internet_to_private_response_or_download_gb

nat_gateway_specific_cost =
  nat_gateway_count * nat_gateway_hourly_price * monthly_hours
  + processed_gb * nat_gateway_processing_price_per_gb
```

Approximate BetterNAT monthly cost:

```text
betternat_specific_cost =
  node_count * node_hourly_price * monthly_hours
  + ebs_monthly_cost
  + public_ipv4_or_eip_monthly_cost
  + dynamodb_monthly_cost
  + monitoring_monthly_cost
  + extra_cross_az_data_transfer_cost
```

Approximate savings:

```text
savings =
  nat_gateway_specific_cost
  - betternat_specific_cost
```

Break-even processed GB:

```text
break_even_gb =
  (betternat_specific_cost - nat_gateway_hourly_cost_replaced)
  / nat_gateway_processing_price_per_gb
```

If your processed GB is low, AWS NAT Gateway simplicity may be worth the cost even if BetterNAT is cheaper on paper.

## Costs BetterNAT Can Reduce

BetterNAT can reduce or remove:

- NAT Gateway per-GB data processing charges for traffic moved to BetterNAT,
- NAT Gateway hourly charges for NAT Gateways you delete,
- cross-AZ NAT path costs when you deploy per-AZ and keep routes aligned.

BetterNAT can also make NAT spend easier to reason about through node metrics and owner labels.

## Costs BetterNAT Does Not Remove

BetterNAT does not remove:

- standard internet data transfer charges,
- EC2 node instance charges,
- EBS volume charges,
- public IPv4/EIP charges where applicable,
- DynamoDB lease table costs,
- CloudWatch, SSM, Prometheus, or log storage costs,
- operational ownership,
- extra cross-AZ data transfer caused by poor route placement.

It is a replacement for a managed NAT processing fee, not a way to bypass AWS data transfer pricing.

## VPC Endpoints First

If most NAT Gateway traffic goes to AWS services that support VPC endpoints, use endpoints before or alongside BetterNAT.

Common examples:

- S3 gateway endpoints,
- DynamoDB gateway endpoints,
- interface endpoints for supported AWS services where the endpoint economics make sense.

AWS explicitly recommends endpoints as a way to reduce NAT Gateway data processing charges for supported service traffic.

BetterNAT is most useful for traffic that still must go to the public internet or external services after endpoint cleanup.

## What To Measure Before Migrating

Before replacing an existing NAT Gateway, collect:

- NAT Gateway `BytesOutToDestination`,
- NAT Gateway `BytesInFromDestination`,
- per-AZ route table ownership,
- which private subnets use which NAT Gateway,
- whether traffic crosses AZs,
- whether traffic can move to VPC endpoints,
- peak Mbps/Gbps,
- packet rate,
- new connection rate,
- concurrent flow estimate,
- destinations that require stable egress IP allowlisting.

BetterNAT does not yet import CloudWatch NAT Gateway metrics automatically. Use
AWS CloudWatch, Cost Explorer, VPC Flow Logs, and BetterNAT estimates together.

## Sources

- AWS NAT Gateway pricing documentation: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html
- AWS VPC pricing page: https://aws.amazon.com/vpc/pricing/
- AWS NAT Gateway CloudWatch metrics: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html

## Next Step

If the estimated savings are worth continuing, read
[Limitations](LIMITATIONS.md) before running a disposable VPC test.
