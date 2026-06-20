# BetterNAT Architecture

Date: 2026-06-20

## Status

This is the current architecture baseline for BetterNAT v0.

Engineering contract: `docs/spec-v0.md`.

The product is now LoxiLB-first:

```text
Primary datapath: LoxiLB standalone egress SNAT
Fallback datapath: Linux nftables/nf_conntrack
Cloud target: AWS first
Install UX: Terraform provider first
Runtime control plane: betternat-agent
Implementation language: Go
```

Earlier research documents that recommended nftables as the default datapath are superseded by the LoxiLB spike results in:

- `docs/research/021-loxilb-spike-results.md`
- `docs/research/022-loxilb-extended-spike-results.md`

## Product Goal

BetterNAT is a self-owned, observable, highly available egress gateway for high-volume private cloud workloads.

The first product target is AWS private subnet egress for workloads where NAT Gateway data processing charges dominate cost:

- blockchain RPC/full nodes and P2P sync workloads,
- crawler and scraper fleets,
- Kubernetes nodes pulling large images/artifacts,
- public data ingestion workers pulling large datasets into private storage.

BetterNAT is not trying to be an AWS NAT Gateway clone with the same managed-service SLA. It is a lower-cost appliance model with explicit capacity profiles, better local observability, and automated failover for new connections.

## High-Level Architecture

```text
                  internet
                     ^
                     |
                 shared EIP
                     |
        +------------+------------+
        |                         |
  active BetterNAT          standby BetterNAT
  appliance                 appliance
        |                         |
  betternat-agent           betternat-agent
  LoxiLB datapath           LoxiLB datapath
        |                         |
        +------------+------------+
                     ^
                     |
        AWS private route table 0.0.0.0/0
                     ^
                     |
             private subnets
```

Core path:

```text
private workload
  -> AWS private route table
  -> active BetterNAT appliance
  -> LoxiLB egress SNAT
  -> appliance EIP
  -> internet
  -> return traffic
  -> LoxiLB DNAT/conntrack
  -> original private workload
```

## Component Responsibilities

## Terraform Provider

`terraform-provider-betternat` is the primary user-facing install and lifecycle interface.

It owns:

- BetterNAT gateway resource schema,
- AWS VPC/subnet/route/EIP/IAM/security group integration,
- appliance instance or launch template creation,
- DynamoDB lease table creation,
- initial route target setup,
- agent config rendering,
- import/read/delete behavior,
- rollback metadata.

It does not own:

- runtime leader election,
- failover execution after deployment,
- LoxiLB rule reconciliation,
- metrics serving.

Implementation note for the current Go provider/applier split:

- `terraform-provider-betternat` derives `agent_config_json`, `user_data`, and `install_plan_json`.
- The AWS install applier consumes that install plan and uses the AWS SDK to create IAM, security group, DynamoDB lease table, EIPs, EC2 appliance instances, source/destination check settings, and initial route targets.
- `ami_id` is required when the applier needs to launch appliance instances itself.
- `instance_type` defaults to `t3.small` when not specified.
- `use_spot` is available for low-cost tests and interruption-tolerant environments, but it defaults to `false`.
- Before BetterNAT AMIs exist, AWS tests use an official Linux AMI plus cloud-init. The provider can pass a sensitive `agent_binary_url` so bootstrap downloads the current `betternat-agent` build at first boot.
- Existing appliance instance IDs can still be supplied by an outer installer, which is useful for tests, bring-your-own-AMI flows, or phased migration.

Example target UX:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"

  vpc_id                  = var.vpc_id
  public_subnet_ids       = var.public_subnet_ids
  private_route_table_ids = var.private_route_table_ids

  high_availability = true
  stable_egress_ip  = true

  datapath_engine = "loxilb"
  fallback_engine = "nftables"

  observability = {
    prometheus = true
    topn       = true
  }
}
```

## betternat-agent

`betternat-agent` is the runtime control plane on every appliance.

It owns:

- local health checks,
- local datapath reconciliation,
- LoxiLB rule apply/read/replay,
- nftables fallback rule apply/read/replay,
- lease acquire/renew/release,
- active/standby HA state machine,
- AWS SDK failover operations,
- route/EIP verification,
- Prometheus metrics export,
- cost/counter aggregation,
- `doctor` and local status API.

It must treat LoxiLB runtime state as ephemeral. The extended spike showed that LoxiLB firewall rules disappeared after `docker restart loxilb` in the tested container mode. Therefore desired datapath config must live in BetterNAT config, and the agent must reconcile it on:

- boot,
- LoxiLB process/container restart,
- failover state change,
- periodic drift checks,
- manual `doctor --repair` or equivalent.

## LoxiLB

LoxiLB is the primary datapath engine.

BetterNAT uses it in standalone datapath-only mode:

- no Kubernetes dependency,
- no LoxiLB-owned AWS control plane by default,
- no broad cloud IAM permissions for LoxiLB itself,
- BetterNAT owns AWS failover and desired-state reconciliation.

### Agent-to-LoxiLB control channel

In v0, `betternat-agent` talks to a local LoxiLB process through `loxicmd`.

The agent performs three operations:

- readiness: call `loxicmd get firewall -o json` and treat command failure as datapath-not-ready,
- reconciliation: create missing egress SNAT firewall rules from BetterNAT desired config,
- telemetry: read firewall counters and conntrack state, normalize them, and re-export BetterNAT Prometheus metrics.

LoxiLB does not call AWS APIs in the BetterNAT architecture. It only owns local packet processing on the appliance. Route failover, EIP reassociation, lease fencing, and verification remain in `betternat-agent`.

The LoxiLB HTTP/gRPC API can replace `loxicmd` later, but it should stay behind the same datapath interface so HA, metrics, and Terraform UX do not change.

The egress rule shape is:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:<private-cidr>,preference:<priority> \
  --snat=<appliance-private-ip> \
  --egress
```

The spike validated this mode with:

- private CIDR SNAT,
- TCP/HTTPS,
- DNS/UDP,
- concurrent short flows,
- 10MB and 137MB response downloads,
- EIP + `ReplaceRoute` failover to a backup appliance.

## nftables Fallback

nftables/nf_conntrack remains a required fallback engine, not the main product path.

Fallback use cases:

- LoxiLB install failure,
- unsupported kernel or instance behavior,
- user explicitly selects conservative Linux NAT mode,
- emergency rollback during support,
- minimal local development and smoke tests.

Scope is intentionally small:

- enable IP forwarding,
- configure `nftables` SNAT or masquerade,
- tune/observe `nf_conntrack`,
- expose basic health and counters,
- share the same `DatapathEngine` interface as LoxiLB.

Do not invest v0 effort into advanced nftables-specific observability or performance differentiation unless LoxiLB fails a product-critical benchmark.

## HA Design

v0 HA is active/standby.

The active appliance must hold a lease before mutating cloud state.

```text
standby
  -> health check active
  -> acquire fenced lease
  -> verify local datapath ready
  -> reassociate shared EIP
  -> ReplaceRoute private route tables
  -> verify route target and EIP owner
  -> become active
```

AWS primitives:

- `AssociateAddress` with reassociation enabled for stable public egress IP.
- `ReplaceRoute` for private subnet default route failover.
- DynamoDB conditional writes for lease/fencing.

The extended spike observed:

- EIP reassociation command around 3.4s,
- `ReplaceRoute` command around 1.6s,
- post-failover new connections using the same EIP.

These are command timings from a spike, not a product SLO. v0 should promise recovery for new connections, not active connection preservation.

## Observability

BetterNAT exports its own Prometheus metrics. LoxiLB is an input source, not the final observability interface.

The spike did not find a ready LoxiLB Prometheus endpoint on API port `11111`:

```text
GET /metrics       -> 404
GET /prometheus    -> 404
GET /debug/metrics -> 404
```

Therefore:

- `betternat-agent` polls LoxiLB state through `loxicmd` or API.
- The agent re-exports normalized BetterNAT metrics.
- High-cardinality per-flow labels are not exported directly.
- Top-N source/destination accounting is aggregated before export.

Key metric groups:

- active/standby state,
- lease status,
- failover events and duration,
- route/EIP match,
- datapath engine and health,
- LoxiLB firewall counters,
- LoxiLB conntrack summaries,
- source CIDR/team byte counters,
- destination top-N summaries,
- estimated NAT Gateway avoided processing cost.

## Datapath Engine Interface

The agent should hide datapath details behind an interface.

Conceptual shape:

```go
type DatapathEngine interface {
    Name() string
    EnsureReady(ctx context.Context, cfg DatapathConfig) error
    Reconcile(ctx context.Context, desired DatapathConfig) error
    Status(ctx context.Context) (DatapathStatus, error)
    Counters(ctx context.Context) (DatapathCounters, error)
    ConntrackSummary(ctx context.Context) (ConntrackSummary, error)
    Cleanup(ctx context.Context) error
}
```

`loxilb` implementation:

- verify process/container is running,
- verify eBPF attached to expected interface,
- apply egress SNAT firewall rules,
- read firewall counters,
- read conntrack state,
- replay rules after restart,
- report degraded status if LoxiLB API is unavailable.

`nftables` implementation:

- verify Linux forwarding,
- apply fallback NAT table,
- read nftables counters,
- read `nf_conntrack` counters,
- report conservative status.

## AWS IAM Boundary

LoxiLB should not receive broad AWS permissions.

`betternat-agent` needs narrowly scoped permissions for:

- describing local instance/ENI metadata,
- describing and replacing configured route table entries,
- associating the configured EIP allocation,
- reading/writing the configured DynamoDB lease row,
- publishing logs/metrics if enabled.

Terraform provider needs broader create/delete permissions at install time, but runtime agent permissions should be smaller and resource-scoped where AWS supports it.

## Failure Behavior

If LoxiLB is down on the active node:

- agent marks datapath unhealthy,
- tries local reconcile/restart if policy allows,
- if still unhealthy, standby can take over after lease fencing.

If LoxiLB config drifts:

- agent reapplies desired rules,
- increments reconciliation metrics,
- emits an event.

If AWS API is unavailable:

- active node keeps forwarding existing traffic if datapath is healthy,
- no route/EIP mutation is attempted without lease/fencing,
- standby does not force split-brain behavior.

If metrics collection fails:

- forwarding should continue,
- agent reports observability degraded.

## v0 Non-Goals

v0 does not promise:

- AWS NAT Gateway equivalent SLA,
- zero packet loss failover,
- active connection preservation,
- active-active NAT,
- multi-cloud provider implementations,
- pod-level attribution for every EKS CNI mode,
- native LoxiLB Prometheus compatibility,
- self-built eBPF NAT,
- VPP datapath.

## Implementation Milestones

1. Define config schema and `DatapathEngine`.
2. Implement `loxilb` engine with rule reconcile and counter reads.
3. Implement minimal `nftables` fallback engine.
4. Implement AWS cloud client for route/EIP verification and mutation.
5. Implement DynamoDB lease/fencing.
6. Implement Prometheus exporter from normalized agent state.
7. Build single-node Terraform provider path.
8. Build active/standby failover path.
9. Add benchmark harness and publish capacity profiles.

## Open Questions

- Container vs package vs embedded install path for LoxiLB.
- Exact LoxiLB API endpoints to use instead of shelling out to `loxicmd`.
- Port exhaustion behavior under many sources to one destination.
- Fragment/ICMP behavior under production traffic.
- Best cardinality policy for destination attribution.
- Whether active connection preservation is worth researching after v0.
