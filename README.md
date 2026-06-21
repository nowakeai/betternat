# BetterNAT

BetterNAT is a self-managed, observable AWS egress gateway for high-volume private-subnet workloads.

It is built for teams that are surprised by NAT Gateway per-GB processing charges: crawler fleets, blockchain/RPC nodes syncing from public peers, Kubernetes nodes pulling large public images, and other workloads that download tens of TB per month from the public internet.

Better not be surprised by NAT Gateway bills.

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

## When To Use It

BetterNAT is worth evaluating when:

- NAT Gateway data processing fees dominate the bill.
- You can operate a small EC2 appliance pool.
- New-flow recovery after failover is acceptable.
- You want Prometheus metrics and appliance-local diagnostics.
- You are comfortable testing an alpha in a disposable or non-critical VPC first.

Use AWS NAT Gateway instead when:

- you need AWS-managed service semantics and SLA,
- active connection preservation matters,
- multi-AZ managed NAT behavior is required immediately,
- you do not want to own EC2, IAM, route, EIP, and DynamoDB operational state.

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

## Quick Start

Start with a disposable VPC:

- [Quick Start](docs/user/QUICK_START.md)

For an existing VPC:

- [Existing VPC Install](docs/user/EXISTING_VPC_INSTALL.md)

Configuration reference:

- [Configuration](docs/user/CONFIGURATION.md)

Operational docs:

- [Operations Guide](docs/user/OPERATIONS_GUIDE.md)
- [Failure Modes And Limitations](docs/user/FAILURE_MODES.md)
- [Limitations](docs/user/LIMITATIONS.md)

Release notes:

- [v0.1.0-alpha.1](docs/user/RELEASE_NOTES_v0.1.0-alpha.1.md)

## Minimal Terraform Shape

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

Run on the active gateway appliance through SSM:

```sh
betternat doctor --live --config /etc/betternat/agent.json
```

Check the private client source IP:

```sh
curl -fsS https://checkip.amazonaws.com
```

Scrape appliance metrics:

```text
http://<gateway-private-ip>:9108/metrics
```

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

## Documentation

Durable docs live under `docs/`.

- `docs/README.md` is the documentation index.
- `docs/dev/USER_DOCUMENTATION_GUIDE.md` defines user-facing documentation rules.
- `docs/release/RELEASE_CHECKLIST.md` tracks alpha and production release gates.
