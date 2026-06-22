# BetterNAT Cost Model

Date: 2026-06-22

## Purpose

BetterNAT is designed for private-subnet workloads where NAT Gateway data processing charges dominate the bill.

It does not make AWS networking free. It replaces the managed NAT Gateway per-GB processing line item with a self-owned EC2 appliance pool, while normal AWS costs still apply.

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

Examples for `50 TB` total traffic through the NAT layer:

| Traffic shape | Private -> internet | Internet -> private | NAT Gateway processed GB | Why the impact differs |
| --- | ---: | ---: | ---: | --- |
| Download-heavy sync/crawling | 1 TB | 49 TB | 50 TB | NAT Gateway charges the 49 TB return path; BetterNAT removes that NAT processing line. |
| Balanced API traffic | 25 TB | 25 TB | 50 TB | NAT Gateway charges both directions; BetterNAT removes NAT processing, but standard egress transfer remains for outbound bytes. |
| Upload-heavy export | 49 TB | 1 TB | 50 TB | NAT processing savings are still real, but normal AWS internet egress transfer can dominate the total bill. |

## Example Processing Fee And Savings

Using an illustrative NAT Gateway processing price of `$0.045/GB`, one NAT Gateway at `$0.045/hour`, and two BetterNAT appliances at `$0.05/hour` each:

| Monthly NAT-processed data | NAT Gateway estimate | BetterNAT estimate | Estimated savings | Savings percent |
| ---: | ---: | ---: | ---: | ---: |
| 10 TB | about `$494/month` | about `$73/month` | about `$421/month` | about `85%` |
| 30 TB | about `$1,415/month` | about `$73/month` | about `$1,342/month` | about `95%` |
| 50 TB | about `$2,337/month` | about `$73/month` | about `$2,264/month` | about `97%` |
| 100 TB | about `$4,641/month` | about `$73/month` | about `$4,568/month` | about `98%` |

Assumptions:

- `1 TB = 1024 GB`,
- `730` hours/month,
- NAT Gateway estimate includes one NAT Gateway hourly charge plus processed GB,
- BetterNAT estimate includes only EC2 appliance instance hours,
- excludes standard data transfer charges,
- excludes EBS, EIP/public IPv4, DynamoDB, monitoring, and operational costs.

Pricing varies by region and can change. Always verify current AWS pricing for your region.

## Example Savings Shape

This example uses:

- `50 TB/month` processed by NAT Gateway,
- `$0.045/hour` NAT Gateway hourly price,
- `$0.045/GB` NAT Gateway processing price,
- `730` hours/month,
- two BetterNAT appliances,
- `$0.05/hour` per appliance.

Run:

```sh
betternat cost estimate \
  --gb 51200 \
  --nat-gateway-hourly 0.045 \
  --nat-gateway-processing-per-gb 0.045 \
  --appliance-hourly 0.05 \
  --appliances 2
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

This is not a quote. It is a directional model for deciding whether the workload is worth testing.

## Formula

Approximate NAT Gateway monthly cost:

```text
processed_gb =
  private_to_internet_request_or_upload_gb
  + internet_to_private_response_or_download_gb

nat_gateway_monthly_cost =
  nat_gateway_count * nat_gateway_hourly_price * monthly_hours
  + processed_gb * nat_gateway_processing_price_per_gb
```

Approximate BetterNAT monthly cost:

```text
betternat_monthly_cost =
  appliance_count * appliance_hourly_price * monthly_hours
  + ebs_monthly_cost
  + public_ipv4_or_eip_monthly_cost
  + dynamodb_monthly_cost
  + monitoring_monthly_cost
  + extra_cross_az_data_transfer_cost
```

Approximate savings:

```text
savings =
  nat_gateway_monthly_cost
  - betternat_monthly_cost
```

Break-even processed GB:

```text
break_even_gb =
  (betternat_monthly_cost - nat_gateway_hourly_cost_replaced)
  / nat_gateway_processing_price_per_gb
```

If your processed GB is low, AWS NAT Gateway simplicity may be worth the cost even if BetterNAT is cheaper on paper.

## Costs BetterNAT Can Reduce

BetterNAT can reduce or remove:

- NAT Gateway per-GB data processing charges for traffic moved to BetterNAT,
- NAT Gateway hourly charges for NAT Gateways you delete,
- cross-AZ NAT path costs when you deploy per-AZ and keep routes aligned.

BetterNAT can also make NAT spend easier to reason about through appliance metrics and owner labels.

## Costs BetterNAT Does Not Remove

BetterNAT does not remove:

- standard internet data transfer charges,
- EC2 appliance instance charges,
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

The first alpha does not yet import CloudWatch NAT Gateway metrics automatically. Use AWS CloudWatch, Cost Explorer, VPC Flow Logs, and BetterNAT estimates together.

## How To Use The CLI Estimate

Basic:

```sh
betternat cost estimate --gb 10240
```

Region-specific prices are not fetched automatically in the first alpha. Override prices explicitly:

```sh
betternat cost estimate \
  --gb 30720 \
  --nat-gateway-hourly <price-per-hour> \
  --nat-gateway-processing-per-gb <price-per-gb> \
  --appliance-hourly <your-ec2-price> \
  --appliances 2
```

Use your own EC2 price, expected appliance count, and region-specific NAT Gateway price.

## Sources

- AWS NAT Gateway pricing documentation: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html
- AWS VPC pricing page: https://aws.amazon.com/vpc/pricing/
- AWS NAT Gateway CloudWatch metrics: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html
