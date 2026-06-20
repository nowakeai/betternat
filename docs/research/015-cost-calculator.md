# Cost Calculator and FinOps Model

Date: 2026-06-19

## Question

How should BetterNAT estimate savings, break-even points, and per-team cost attribution for high-volume NAT Gateway replacement?

## Short Answer

The cost calculator should be a product feature, not a spreadsheet in the docs.

It should:

- use region-aware NAT Gateway pricing,
- model processed bytes, hourly gateway charges, EC2/EIP/EBS/monitoring costs,
- show what costs are avoided and what costs remain,
- estimate break-even GB/month,
- recommend VPC endpoints for AWS service traffic that should bypass NAT entirely,
- integrate with observability so users can attribute traffic by source/team/workload.

Do not imply BetterNAT removes standard internet data transfer charges.

## Cost Components

### AWS NAT Gateway cost

AWS NAT Gateway charges include:

- NAT Gateway hours,
- data processed per GB.

AWS docs state that NAT Gateway is charged for each hour available and each GB processed.

Formula:

```text
nat_gateway_monthly_cost =
  nat_gateway_count * nat_gateway_hourly_price * monthly_hours
  + processed_gb * nat_gateway_processing_price_per_gb
```

Important:

- standard AWS data transfer charges are separate,
- cross-AZ data transfer may apply depending on topology,
- regional pricing varies.

### BetterNAT cost

BetterNAT replacement cost includes:

- EC2 instance hours,
- standby instance hours,
- EBS,
- public IPv4/EIP cost,
- CloudWatch logs/metrics,
- DynamoDB lease table,
- optional VPC endpoints,
- operational ownership.

Formula:

```text
betternat_monthly_cost =
  active_instance_hours * active_instance_hourly_price
  + standby_instance_hours * standby_instance_hourly_price
  + ebs_monthly_cost
  + public_ipv4_or_eip_monthly_cost
  + monitoring_monthly_cost
  + lease_backend_monthly_cost
  + extra_cross_az_monthly_cost
```

The appliance does not pay per-GB NAT Gateway processing fees, but it still pays normal data transfer charges.

## Processed Bytes Semantics

For NAT Gateway pricing, the billable meter is data processed by the NAT Gateway.

For normal private-subnet internet egress:

```text
private instance -> NAT Gateway -> internet
internet -> NAT Gateway -> private instance
```

Both directions pass through the gateway and contribute to processed bytes.

Safe product wording:

> NAT Gateway charges per GB processed by the gateway. For private-subnet internet egress, outbound packets and return packets both pass through the gateway and contribute to processed data volume.

Avoid:

> AWS double-charges every byte.

That is imprecise and can create billing misunderstandings.

## Ingress-heavy Return Traffic

BetterNAT's savings can be especially strong for private workloads that initiate outbound connections and receive large responses.

Clarification:

- NAT Gateway is not an inbound load balancer and does not receive unsolicited internet traffic for private instances.
- But response traffic for private-initiated connections returns through the NAT Gateway.
- From the private workload's perspective this is ingress/download traffic.
- From NAT Gateway pricing's perspective it is still processed data.

Example:

```text
request:
  private instance -> NAT Gateway -> public service
  10 MB

response:
  public service -> NAT Gateway -> private instance
  100 GB
```

NAT Gateway data processing is approximately:

```text
100.01 GB processed
```

The public-service-to-private-instance response can dominate the NAT Gateway bill even though normal AWS data transfer into AWS may be free or cheaper than data transfer out. BetterNAT removes the managed NAT per-GB processing fee for this path, but it does not change standard AWS data transfer pricing.

Calculator implication:

The calculator should ask for traffic direction split:

```text
private_to_internet_gb
internet_to_private_response_gb
```

and report:

```text
nat_gateway_processed_gb =
  private_to_internet_gb
  + internet_to_private_response_gb
```

This makes ingress-heavy response/download workloads visible instead of hiding them under a generic monthly GB number.

## Break-even Formula

Approximate:

```text
monthly_savings =
  nat_gateway_monthly_cost_replaced
  - betternat_monthly_cost
```

Break-even processed GB:

```text
break_even_gb =
  (betternat_fixed_monthly_cost - nat_gateway_hourly_cost_replaced)
  / nat_gateway_processing_price_per_gb
```

If `betternat_fixed_monthly_cost` is lower than NAT Gateway hourly cost, break-even can be near zero, but this does not mean the product is operationally worth it for low traffic.

The calculator should still warn:

> Below a traffic threshold, managed NAT Gateway simplicity may be worth the cost.

## Required Inputs

Minimal:

- cloud provider,
- region,
- monthly processed GB,
- number of AZs,
- number of NAT gateways being replaced,
- HA mode,
- stable egress IP required,
- instance profile or instance type.

Better:

- average Mbps,
- peak Gbps,
- p95 pps,
- p95 new connections/sec,
- concurrent flows,
- cross-AZ percentage,
- current NAT Gateway IDs,
- CloudWatch metrics import.

## Outputs

Calculator should output:

- estimated current NAT Gateway monthly cost,
- estimated BetterNAT monthly cost,
- estimated monthly savings,
- break-even GB/month,
- savings at 10/30/50/100 TB,
- one-AZ/two-AZ/three-AZ comparison,
- per-owner/team cost attribution if observability data exists,
- warnings about cross-AZ paths,
- endpoint optimization recommendations.

Example:

```text
Region: us-west-2
Processed traffic: 50 TB/month
NAT Gateway processing estimate: $2,304/month
NAT Gateway hourly estimate: $65/month for 2 gateways
BetterNAT estimate: $X/month
Estimated savings: $Y/month
Break-even: Z TB/month
```

Use live pricing when possible rather than hard-coded examples.

## Pricing Data Source

Preferred:

- AWS Price List API for current regional pricing.

Fallback:

- user-supplied pricing config,
- bundled static pricing table with version/date,
- clear warning that pricing may be stale.

Config example:

```yaml
pricing:
  aws:
    us-west-2:
      nat_gateway_hourly: 0.045
      nat_gateway_processing_per_gb: 0.045
      public_ipv4_hourly: 0.005
```

Pricing should be cacheable. Do not make the agent depend on public internet access to compute basic cost.

## VPC Endpoint Recommendations

The calculator should not assume all NAT traffic should be moved to BetterNAT.

AWS docs explicitly note that Gateway Type VPC Endpoints can avoid NAT Gateway data processing charges for S3 traffic and that gateway endpoints have no hourly or data processing charges.

BetterNAT should recommend:

- S3 Gateway Endpoint,
- DynamoDB Gateway Endpoint,
- Interface Endpoints for high-volume AWS APIs where economics are favorable,
- ECR endpoints for image-pull-heavy EKS/ECS workloads,
- CloudWatch Logs endpoint if logs dominate NAT traffic,
- STS/Secrets Manager/KMS endpoints where security/cost justify it.

Recommendation logic:

```text
if destination appears to be S3/DynamoDB in same region:
  recommend gateway endpoint before appliance replacement

if destination is high-volume AWS service with PrivateLink:
  compare interface endpoint hourly+GB cost vs NAT processing cost
```

This makes the product more credible:

> BetterNAT reduces unavoidable internet egress NAT cost; VPC endpoints should remove avoidable AWS-service NAT traffic first.

## Cost Attribution

Cost attribution should combine:

- observed bytes,
- owner/team mapping,
- region NAT Gateway processing rate,
- time window.

Formula:

```text
owner_estimated_nat_gateway_processing_cost =
  owner_processed_gb * region_processing_price_per_gb
```

Attribution dimensions:

- source IP,
- source subnet,
- owner/team,
- EKS namespace/workload if available,
- VM instance tag,
- destination class.

Need unknown bucket:

```text
unknown_or_unmapped_bytes
unknown_or_unmapped_cost
```

The dashboard should show percentage of unattributed traffic.

## Cross-AZ Cost Warning

Self-hosted NAT can accidentally create cross-AZ hairpin traffic:

```text
private subnet in us-west-2a -> NAT appliance in us-west-2b
```

This can add data transfer costs and larger blast radius.

Calculator should model:

- per-AZ deployment,
- centralized deployment,
- estimated cross-AZ percentage.

Default recommendation:

> Deploy one HA group per AZ and route private subnets to the NAT appliance in the same AZ.

## CLI UX

Commands:

```sh
betternat cost estimate \
  --cloud aws \
  --region us-west-2 \
  --monthly-gb 51200 \
  --az-count 2 \
  --instance-profile balanced
```

```sh
betternat cost from-cloudwatch \
  --nat-gateway-id nat-123 \
  --window 30d
```

```sh
betternat cost attribution \
  --window 24h \
  --group-by owner
```

```sh
betternat recommend-endpoints \
  --window 7d
```

Terraform provider should expose computed estimates:

```hcl
output "estimated_monthly_savings" {
  value = betternat_gateway.egress.estimated_monthly_savings
}
```

## What Not To Include In Savings

Do not count these as BetterNAT savings:

- standard internet data transfer out,
- unrelated cross-region transfer,
- application-level traffic reduction,
- savings from VPC endpoints unless separately itemized,
- labor/ops cost unless user supplies assumptions.

Separate categories:

```text
NAT Gateway replacement savings
VPC endpoint optimization savings
architecture/routing correction savings
```

## MVP Scope

v0:

- static region pricing config,
- manual monthly GB input,
- EC2 instance cost input or simple profile,
- NAT Gateway vs BetterNAT estimate,
- break-even,
- warning about standard data transfer.

v1:

- AWS Price List API integration,
- CloudWatch NAT Gateway metric import,
- cost attribution from observed bytes,
- endpoint recommendations.

v2:

- CUR integration,
- multi-account aggregation,
- per-team showback report,
- multi-cloud pricing calculators.

## Decision

Build the cost calculator early.

It supports:

- product positioning,
- instance sizing,
- customer qualification,
- observability value,
- endpoint recommendations,
- honest savings claims.

The calculator should say:

> Here is the NAT Gateway processing cost you can avoid, here is the appliance cost you take on, here is the traffic you should remove with endpoints first, and here is the break-even point.

## Sources

- AWS NAT Gateway pricing docs, charged per available hour and per GB processed: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html
- AWS VPC pricing, including note that Gateway Type VPC endpoints avoid NAT Gateway data processing charges and have no hourly/data processing charges: https://aws.amazon.com/vpc/pricing/
- AWS Price List API user guide: https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/price-changes.html
- AWS NAT Gateway CloudWatch metrics for bytes and packets processed: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html
