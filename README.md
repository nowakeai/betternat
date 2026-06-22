# BetterNAT

Self-owned, observable, highly available egress for high-volume AWS private subnet workloads.

BetterNAT targets the NAT Gateway bill line that hurts at scale: per-GB data processing. It is built for crawler fleets, blockchain/RPC nodes syncing from public peers, Kubernetes nodes pulling large public images, and other private workloads that download tens of TB per month from the public internet.

Better not be surprised by NAT Gateway bills.

## Why BetterNAT

AWS [NAT Gateway pricing](https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html) charges for each hour it is available and each GB it processes. The [AWS VPC pricing page](https://aws.amazon.com/vpc/pricing/) also states that data processing charges apply for each GB processed through NAT Gateway regardless of traffic source or destination, and standard data transfer charges still apply.

For private-subnet download-heavy workloads, this matters:

```text
private worker -> small request -> public peer/API/registry
public peer/API/registry -> large response/download -> private worker
```

The large response returns through NAT Gateway and contributes to processed GB. BetterNAT replaces that managed per-GB NAT processing fee with a self-managed EC2 appliance pool.

Direction matters. NAT Gateway processing is metered on both request bytes and response bytes through the gateway. BetterNAT has no equivalent per-GB NAT processing fee; after replacement, the remaining AWS data-transfer bill depends on traffic direction. That is why BetterNAT is especially strong for workloads that send small requests and pull large responses into AWS.

NAT-layer monthly estimate at `$0.045/GB`, `$0.045/hour` for one NAT Gateway, and two `$0.05/hour` BetterNAT appliances:

| Monthly NAT-processed data | NAT Gateway | BetterNAT | Savings | Savings % |
| ---: | ---: | ---: | ---: | ---: |
| 10 TB | about `$494/month` | about `$73/month` | about `$421/month` | about `85%` |
| 30 TB | about `$1,415/month` | about `$73/month` | about `$1,342/month` | about `95%` |
| 50 TB | about `$2,337/month` | about `$73/month` | about `$2,264/month` | about `97%` |
| 100 TB | about `$4,641/month` | about `$73/month` | about `$4,568/month` | about `98%` |

The BetterNAT column includes only illustrative appliance instance hours: `2 appliances * $0.05/hour * 730 hours = $73/month`. It excludes EBS, EIP/public IPv4, DynamoDB, monitoring, operational cost, and standard AWS data transfer charges. Upload-heavy workloads still pay normal AWS internet egress charges in both designs; download-heavy workloads often feel the largest improvement because NAT Gateway was adding a per-GB processing charge to return traffic. See [Cost Model](docs/user/COST_MODEL.md) for formulas, direction examples, caveats, and CLI usage.

## What You Get

- Lower NAT Gateway processing cost for suitable high-volume workloads.
- Stable egress IP failover mode with a shared EIP.
- ASG-backed appliance pool with active/standby ownership.
- LoxiLB/eBPF primary datapath with nftables fallback.
- DynamoDB lease/fencing for route and EIP ownership.
- Prometheus metrics for HA, datapath, traffic counters, and failover state.
- Terraform provider install UX through `nowakeai/betternat`.
- Rollback-oriented route ownership model for existing VPC adoption.

## Quick Start

Start in a disposable VPC:

```sh
export AWS_PROFILE="<your-profile>"
export AWS_REGION="us-west-2"
export BETTERNAT_AZ="us-west-2a"
export BETTERNAT_VERSION="v0.1.0-alpha.1"
```

Use the Terraform Registry provider:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.2"
    }
  }
}
```

Then follow:

- [Quick Start](docs/user/QUICK_START.md) for a disposable VPC.
- [Existing VPC Install](docs/user/EXISTING_VPC_INSTALL.md) when you are ready to test against real route tables.
- [Configuration](docs/user/CONFIGURATION.md) for all `betternat_gateway` fields.

Minimal resource shape:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress-a"
  region = "us-west-2"
  vpc_id = aws_vpc.main.id

  ami_id           = data.aws_ami.al2023_arm64.id
  instance_type    = "t4g.small"
  use_spot         = false
  min_size         = 1
  desired_capacity = 2
  max_size         = 3

  agent_binary_url    = var.agent_binary_url
  agent_binary_sha256 = var.agent_binary_sha256
  cli_binary_url      = var.cli_binary_url
  cli_binary_sha256   = var.cli_binary_sha256

  public_subnet_ids = {
    "us-west-2a" = aws_subnet.public_a.id
  }

  private_route_table_ids = {
    "us-west-2a" = [aws_route_table.private_a.id]
  }

  private_cidrs = [aws_vpc.main.cidr_block]

  stable_egress_ip    = true
  ha_profile          = "stable"
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

## Verify

Run on a gateway appliance through SSM:

```sh
betternat doctor --live --config /etc/betternat/agent.json
```

Check public egress from a private client:

```sh
curl -fsS https://checkip.amazonaws.com
```

Scrape metrics:

```text
http://<gateway-private-ip>:9108/metrics
```

Estimate the cost shape:

```sh
betternat cost estimate --gb 51200 --appliance-hourly 0.05 --appliances 2
```

## When To Use It

BetterNAT is worth evaluating when:

- NAT Gateway data processing fees dominate the bill.
- Private workloads pull or receive large amounts of public internet data.
- You can operate a small EC2 appliance pool.
- New-flow recovery after failover is acceptable.
- You want Prometheus metrics and appliance-local diagnostics.
- You can test in a disposable or non-critical VPC first.

Use AWS NAT Gateway instead when:

- you need AWS-managed service semantics and SLA,
- active connection preservation matters,
- multi-AZ managed NAT behavior is required immediately,
- you do not want to own EC2, IAM, routes, EIPs, DynamoDB, metrics, and rollback state.

## Architecture

BetterNAT deploys an Auto Scaling Group of gateway appliances in one AZ.

Each appliance runs:

- `betternat-agent`,
- LoxiLB in standalone mode,
- the `betternat` CLI,
- Prometheus metrics.

The active appliance owns:

- the DynamoDB lease,
- the private route table default route,
- the shared EIP when `stable_egress_ip=true`.

On failure, a standby appliance takes over by reconciling datapath state, claiming the EIP when configured, and replacing the private route target.

Architecture docs:

- [Architecture](docs/architecture.md)
- [Architecture Diagram](docs/architecture-diagram.md)
- [Failure Modes](docs/user/FAILURE_MODES.md)

## Alpha Status

`v0.1.0-alpha.1` is an early technical preview.

Current scope:

- AWS only.
- Single-AZ HA group.
- Terraform provider first.
- No published BetterNAT AMI in the first alpha.
- Install path is Terraform plus cloud-init bootstrap on an explicit Linux AMI.
- LoxiLB/eBPF is the primary datapath.
- nftables/nf_conntrack remains a fallback path.
- New connections recover after failover; active connections may reset.
- No NAT Gateway equivalent SLA.
- High-volume savings are modeled, not proven by expensive multi-TB benchmark runs.

Read before using real route tables:

- [Limitations](docs/user/LIMITATIONS.md)
- [Rollback Guide](docs/user/ROLLBACK_GUIDE.md)
- [Upgrade And Replacement Guide](docs/user/UPGRADE_REPLACEMENT_GUIDE.md)
- [Security And Supply Chain Guide](docs/user/SECURITY_HARDENING.md)

## Documentation

- [Documentation Index](docs/README.md)
- [Cost Model](docs/user/COST_MODEL.md)
- [Operations Guide](docs/user/OPERATIONS_GUIDE.md)
- [Observability Guide](docs/user/OBSERVABILITY_GUIDE.md)
- [IAM Policy](docs/user/IAM_POLICY.md)
- [Release Notes](docs/user/RELEASE_NOTES_v0.1.0-alpha.1.md)

## Development

Use direct Go commands as the portable baseline.

Run tests:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
```

Build the Terraform provider:

```sh
GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat
```

The repo-local `./manage` script is an optional convenience wrapper, not the only supported workflow.

## License

BetterNAT is licensed under the Apache License 2.0. See [LICENSE](LICENSE).

Third-party notices are recorded in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
