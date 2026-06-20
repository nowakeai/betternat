# Cost Model Review

Date: 2026-06-19

## User Target

The target workload is not a small dev VPC. The intended customer profile is:

- Tens of TB per month through AWS NAT Gateway.
- NAT Gateway data processing charges are the primary pain.
- The user is specifically concerned that traffic processed by NAT Gateway includes both outbound traffic and return traffic.

This is the right profile for the product. The per-GB NAT Gateway charge is large enough to justify a self-hosted appliance, observability, and HA engineering.

## AWS Pricing Fact

AWS says NAT Gateway charges include:

- An hourly charge while the NAT Gateway is available.
- A charge for each GB of data the NAT Gateway processes.

The safest wording is:

> NAT Gateway charges per GB processed by the gateway. For normal private-subnet internet egress, both outbound packets and return packets pass through the NAT Gateway and contribute to processed data volume.

Avoid saying "AWS charges twice for one byte" because that is imprecise. The better product language is "processed traffic is metered in both directions through the gateway."

## High-volume Response / Ingress-heavy Workloads

The strongest savings case may be ingress-heavy return traffic for connections initiated by private workloads.

Important terminology:

- **NAT Gateway does not support unsolicited inbound internet connections to private instances.** It is not an inbound load balancer.
- But for private instances that initiate outbound connections, the response traffic from the internet returns through the NAT Gateway.
- That response traffic is "ingress" from the perspective of the private workload, and it still counts as NAT Gateway processed data.

Example:

```text
private worker -> small HTTPS request -> public API/object store
public service -> 10 GB response/download -> private worker
```

The small request and the large response both traverse NAT Gateway, so the 10 GB response materially increases NAT Gateway data processing charges.

This matters because normal cloud data-transfer pricing is asymmetric: data transfer into AWS is often free or much cheaper than data transfer out, but NAT Gateway data processing is charged for bytes processed by the gateway. Replacing NAT Gateway with BetterNAT removes that managed NAT per-GB processing fee for the return path, while standard data transfer charges still follow normal AWS rules.

Best target workloads:

- pulling large artifacts from external registries,
- downloading datasets into private workers,
- receiving large API responses from partner services,
- cross-cloud private workers pulling data from GCP/Azure/SaaS endpoints through a fixed egress IP,
- EKS/ECS image pull patterns when VPC endpoints are not available or not configured.

Product wording:

> BetterNAT is especially attractive for private workloads that initiate small outbound requests and receive large responses through NAT Gateway. NAT Gateway bills per GB processed in both directions; BetterNAT removes the managed NAT processing fee while preserving a controlled egress path.

Non-goal:

> BetterNAT is not an inbound internet load balancer for unsolicited public traffic.

## Example Cost Shape

Using the common public example rate of `$0.045/GB`:

| Monthly processed data | Monthly processing fee | Annual processing fee |
| ---: | ---: | ---: |
| 10 TB | about $460 | about $5,529 |
| 30 TB | about $1,382 | about $16,589 |
| 50 TB | about $2,304 | about $27,648 |
| 100 TB | about $4,608 | about $55,296 |

Assumption: `1 TB = 1024 GB`.

This excludes:

- NAT Gateway hourly charges.
- Standard EC2/internet data transfer charges.
- Cross-AZ data transfer.
- CloudWatch/logging costs.
- Costs of any replacement EC2 appliances.

## Why This Is A Strong Product Wedge

At tens of TB/month, NAT Gateway processing charges dominate the gateway cost. If a self-hosted NAT appliance can handle the traffic profile, the monthly savings can easily justify:

- Two EC2 instances per AZ for active/standby.
- EIP(s).
- EBS.
- DynamoDB lease table.
- Prometheus/Grafana/CloudWatch monitoring.
- Engineering ownership.

This makes the initial target user:

> High-volume AWS users whose private subnets send tens of TB/month through NAT Gateway and who need better cost attribution and controlled HA.

## What BetterNAT Can Save

Potentially avoided or reduced:

- NAT Gateway per-GB data processing charge.
- NAT Gateway hourly charge, if gateways are removed.
- Cross-AZ NAT path costs, if the appliance is deployed per-AZ and routes are aligned.
- Some NAT traffic entirely, if the product recommends S3/DynamoDB gateway endpoints or PrivateLink.

Not avoided:

- Internet data transfer out.
- Inter-AZ data transfer unrelated to NAT path design.
- EC2 instance cost.
- EIP cost, if applicable.
- Observability/storage cost.
- Operational ownership.

## Required Product Feature: Cost Attribution

For this target user, observability and cost are the same feature.

The product should not just show total bytes. It should answer:

- Which private IPs produced NAT cost?
- Which subnets/teams/cost centers produced NAT cost?
- Which destinations are responsible?
- How much would this traffic have cost through NAT Gateway?
- Which traffic should bypass NAT via VPC endpoints?

Suggested CLI:

```sh
betternat cost estimate --monthly-gb 50000 --region us-west-2
betternat top sources --window 1h
betternat top destinations --window 1h
betternat recommend-endpoints --flow-log s3://...
```

## Break-even Formula

Approximate monthly savings:

```text
savings =
  nat_gateway_data_processing_gb * nat_gateway_processing_price_per_gb
  + nat_gateway_count * nat_gateway_hourly_price * monthly_hours
  - betternat_ec2_monthly_cost
  - betternat_eip_monthly_cost
  - betternat_monitoring_monthly_cost
  - extra_cross_az_data_transfer_cost
```

Break-even processed GB:

```text
break_even_gb =
  (betternat_monthly_cost - nat_gateway_hourly_cost_replaced)
  / nat_gateway_processing_price_per_gb
```

This must be region-aware.

## Design Consequence

Because the buyer is moving tens of TB/month, the product must support:

- Per-AZ deployment to avoid accidental cross-AZ NAT paths.
- Clear capacity profiles by EC2 instance type.
- A rollback path to NAT Gateway.
- Reproducible benchmarks.
- Cost attribution dashboard.
- Alerts before conntrack or network capacity saturation.

## Review Decision

The low-cost pillar is valid and strong.

Recommended commitment:

> BetterNAT targets workloads where NAT Gateway data processing fees dominate the bill, especially tens-of-TB-per-month private-subnet egress. It replaces managed per-GB processing charges with a self-owned EC2 NAT appliance, while preserving a rollback path to AWS NAT Gateway.

Recommended non-commitment:

> BetterNAT is not free NAT, not a universal NAT Gateway replacement, and not exempt from standard AWS data transfer charges.

## Sources

- AWS NAT Gateway pricing: charged per available hour and each GB processed: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html
- AWS VPC pricing examples for NAT Gateway hourly and data processing charges: https://aws.amazon.com/vpc/pricing/
- AWS NAT Gateway monitoring metrics, including bytes and packets processed: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html
