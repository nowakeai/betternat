# BetterNAT v0 Specification

Date: 2026-06-20

## Status

This document is the v0 engineering contract.

Current baseline:

```text
Primary datapath: LoxiLB standalone egress SNAT
Fallback datapath: Linux nftables/nf_conntrack
Cloud target: AWS
Runtime control plane: betternat-agent
User-facing install path: terraform-provider-betternat
Implementation language: Go
```

This spec follows `docs/architecture.md` and supersedes older nftables-first research notes.

## Product Contract

BetterNAT v0 provides a self-hosted AWS egress gateway for private subnets.

v0 implementation language is Go for:

- `betternat-agent`,
- `betternat` CLI,
- `terraform-provider-betternat`,
- cloud/provider integrations,
- metrics exporter,
- datapath reconciliation wrappers.

v0 MUST provide:

- AWS private subnet egress through self-owned EC2 appliances.
- LoxiLB-based egress SNAT as the primary datapath.
- nftables/nf_conntrack fallback mode.
- Terraform-managed install and lifecycle.
- Active/standby HA for new connections.
- Stable public egress IP for new connections after failover when `stable_egress_ip = true`.
- Prometheus metrics exported by `betternat-agent`.
- Basic source/CIDR traffic attribution.
- `doctor` checks for datapath, route, EIP, IAM, lease, and metrics health.
- Rollback metadata for restoring previous private route targets.

v0 MUST NOT promise:

- AWS NAT Gateway equivalent SLA.
- Zero packet loss failover.
- Active connection preservation.
- Active-active NAT.
- Multi-cloud runtime support.
- Full EKS pod attribution.
- Native LoxiLB Prometheus compatibility.
- Self-built eBPF NAT.
- VPP datapath.

## Terraform Provider

The v0 product target is a custom Terraform provider.

Provider name:

```text
terraform-provider-betternat
```

Primary resource:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"

  vpc_id = "vpc-123"

  public_subnet_ids = {
    "us-west-2a" = "subnet-public-a"
    "us-west-2b" = "subnet-public-b"
  }

  private_route_table_ids = {
    "us-west-2a" = ["rtb-private-a"]
    "us-west-2b" = ["rtb-private-b"]
  }

  high_availability = {
    enabled = true
    mode    = "active_standby"
  }

  public_identity = {
    mode = "shared_eip"
  }

  datapath = {
    engine          = "loxilb"
    fallback_engine = "nftables"
    private_cidrs   = ["10.0.0.0/8"]
  }

  observability = {
    prometheus = true
    topn       = true
  }

  rollback = {
    enabled = true
  }
}
```

### Required Arguments

The provider MUST require:

- `name`
- `cloud`
- `region`
- `vpc_id`
- optional `ami_id`
- optional `instance_type`
- optional `use_spot`
- at least one public subnet
- at least one private route table
- private CIDR allowlist for NAT

### Computed Attributes

The provider SHOULD expose:

```hcl
egress_public_ips
active_instance_ids
standby_instance_ids
lease_table_name
agent_config_hash
managed_route_table_ids
rollback_route_targets
status
```

### Provider Responsibilities

The provider MUST create or manage:

- EC2 appliance instances or launch templates.
- IAM instance profile for `betternat-agent`.
- Security groups.
- EIP allocation or association, if `public_identity.mode = "shared_eip"`.
- DynamoDB lease table.
- Agent config file/user data/bootstrap input.
- Initial private route target.
- Tags for ownership and discovery.
- Rollback metadata.

The provider MUST NOT perform runtime failover. Runtime failover belongs to `betternat-agent`.

The current Go implementation represents provider work as a deterministic install plan. The AWS applier consumes that plan with the AWS SDK. If appliance instance IDs are not supplied by an outer installer, the applier MUST launch EC2 appliances from `ami_id`; otherwise it MUST fail before route mutation. `instance_type` defaults to `t3.small`.

`use_spot` MAY be enabled for low-cost tests and interruption-tolerant deployments. It MUST default to `false` because production NAT appliances should not silently use interruptible capacity.

Before a BetterNAT AMI exists, v0 development tests MAY use a standard cloud Linux AMI plus cloud-init. In that mode the provider MAY accept a sensitive `agent_binary_url` and optional `loxicmd_binary_url`; bootstrap downloads the agent and either downloads `loxicmd` or creates a host wrapper for the LoxiLB container.

## AWS Resource Model

v0 AWS deployment is per-AZ active/standby.

Minimum single-AZ HA shape:

```text
public subnet:
  active appliance candidate
  standby appliance candidate

private route table:
  0.0.0.0/0 -> active appliance instance or ENI

public identity:
  shared EIP associated to active appliance

lease:
  DynamoDB row per HA group
```

Appliance instances MUST have source/destination check disabled.

LoxiLB itself SHOULD NOT receive AWS IAM permissions. AWS mutations are performed by `betternat-agent`.

## Agent Config

The Terraform provider MUST render a local agent config.

Example:

```yaml
version: v0
gateway_id: prod-egress
ha_group_id: prod-egress-us-west-2a
cloud: aws
region: us-west-2

local:
  instance_id: auto
  availability_zone: auto
  primary_interface: ens5

datapath:
  engine: loxilb
  fallback_engine: nftables
  private_cidrs:
    - 10.0.0.0/8
  loxilb:
    api_address: 127.0.0.1
    api_port: 11111
    snat_to: auto
    snat_interface: ens5
    rule_preference_base: 100
    reconcile_interval_seconds: 10
  nftables:
    table_name: betternat
    chain_prefix: betternat

ha:
  enabled: true
  lease:
    backend: dynamodb
    table: betternat-prod-egress-leases
    key: prod-egress-us-west-2a
    ttl_seconds: 10
    renew_interval_seconds: 3
  route_failover:
    mode: replace_route
    route_table_ids:
      - rtb-private-a
    destination_cidr: 0.0.0.0/0
    target_type: instance
  public_identity:
    mode: shared_eip
    allocation_id: eipalloc-123

observability:
  prometheus:
    listen_address: 0.0.0.0
    listen_port: 9108
  attribution:
    owners:
      - name: default
        cidrs: ["10.0.0.0/8"]

rollback:
  previous_route_targets:
    rtb-private-a:
      destination_cidr: 0.0.0.0/0
      target: nat-previous
```

Config SHOULD include an expected hash or signature later, but v0 may start with a plain file plus strict file permissions.

## Datapath Engine Contract

The agent MUST implement a datapath abstraction.

Conceptual interface:

```go
type DatapathEngine interface {
    Name() string
    EnsureReady(ctx context.Context, cfg DatapathConfig) error
    Reconcile(ctx context.Context, cfg DatapathConfig) error
    Status(ctx context.Context) (DatapathStatus, error)
    Counters(ctx context.Context) (DatapathCounters, error)
    ConntrackSummary(ctx context.Context) (ConntrackSummary, error)
    Cleanup(ctx context.Context) error
}
```

### LoxiLB Engine

The LoxiLB engine MUST:

- verify LoxiLB is running,
- verify API/CLI access,
- verify eBPF is attached to the expected interface when possible,
- create egress SNAT firewall rules for configured private CIDRs,
- read firewall counters,
- read conntrack summaries,
- reconcile missing rules after LoxiLB restart,
- report degraded state if LoxiLB API/CLI is unavailable.

Rule shape:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:<cidr>,preference:<preference> \
  --snat=<local-private-ip> \
  --egress
```

The LoxiLB engine MUST treat LoxiLB rules as ephemeral. Desired state lives in BetterNAT config, not only inside LoxiLB.

### nftables Fallback Engine

The nftables engine MUST:

- enable/check IP forwarding,
- create a dedicated `betternat` nftables table,
- configure SNAT or masquerade for configured private CIDRs,
- read basic nftables counters,
- read `nf_conntrack` usage,
- expose fallback health.

The fallback engine SHOULD be simple and conservative. It is not the main performance/observability path.

## HA State Machine

Agent states:

```text
INIT
STANDBY
TAKING_OVER
ACTIVE
DEMOTING
ERROR
MAINTENANCE
```

### INIT

MUST:

- load config,
- identify local instance through metadata,
- initialize AWS client,
- verify required IAM actions,
- verify source/destination check status if possible,
- start datapath reconcile loop,
- start metrics server.

Exit:

- `ACTIVE` if lease and route/EIP already point to this node,
- otherwise `STANDBY`.

### STANDBY

MUST:

- keep local datapath ready,
- monitor active health,
- watch lease expiry,
- avoid route/EIP mutation unless takeover conditions pass.

### TAKING_OVER

MUST execute:

1. Acquire DynamoDB lease with a new fencing generation.
2. Reconcile local datapath.
3. Associate shared EIP to this instance if configured.
4. Replace configured private routes to this instance or ENI.
5. Verify route state.
6. Verify EIP association.
7. Run outbound source-IP probe if configured.
8. Re-read lease generation.
9. Transition to `ACTIVE`.

If any step fails, the agent MUST stop mutating and report `ERROR` or return to `STANDBY` depending on whether it owns the lease.

### ACTIVE

MUST:

- renew lease,
- reconcile datapath continuously,
- verify private route target still matches self,
- verify shared EIP still matches self,
- export active status metrics.

If lease renewal fails beyond threshold, the node MUST stop cloud mutations and enter degraded state. Forwarding may continue briefly, but the node must not claim active control-plane ownership.

## Lease And Fencing

v0 lease backend:

```text
DynamoDB
```

Lease row SHOULD include:

```json
{
  "ha_group_id": "prod-egress-us-west-2a",
  "owner_instance_id": "i-abc",
  "generation": 42,
  "expires_at": 1780000000,
  "updated_at": 1779999990
}
```

Takeover MUST use conditional writes:

- acquire only if lease expired or owner is self,
- increment generation on takeover,
- verify generation before declaring `ACTIVE`.

EC2 tags MUST NOT be the primary lock.

## Failover Contract

v0 failover promise:

```text
BetterNAT restores egress for new connections after active appliance failure.
```

v0 does not promise active connection survival.

When `stable_egress_ip = true`, successful failover MUST preserve the observed public egress IP for new connections.

The agent MUST NOT mark the gateway active until:

- lease is owned,
- datapath is ready,
- route table points to self,
- EIP points to self if configured,
- optional outbound probe succeeds.

## Observability Contract

`betternat-agent` MUST export Prometheus metrics.

Default listen:

```text
0.0.0.0:9108
```

Metric names are preliminary but should follow this shape.

### Agent And HA

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
betternat_datapath_engine_info{engine}
betternat_datapath_ready
betternat_datapath_reconcile_total{engine,result}
betternat_datapath_reconcile_duration_seconds{engine}
betternat_loxilb_rule_present{cidr}
betternat_loxilb_rule_packets_total{cidr}
betternat_loxilb_rule_bytes_total{cidr}
betternat_conntrack_entries{engine}
betternat_conntrack_established{engine,protocol}
betternat_conntrack_udp_entries{engine}
betternat_interface_rx_bytes_total{interface}
betternat_interface_tx_bytes_total{interface}
betternat_interface_rx_drops_total{interface}
betternat_interface_tx_drops_total{interface}
```

### Attribution And Cost

```text
betternat_owner_bytes_total{owner,direction}
betternat_owner_packets_total{owner,direction}
betternat_processed_bytes_total{direction}
betternat_estimated_nat_gateway_processing_cost_usd_total{region}
```

The exporter MUST avoid unbounded labels such as raw per-flow destination IP in normal Prometheus metrics.

Top-N data SHOULD be exposed through an admin API or bounded metric set, not unlimited labels.

## Doctor Checks

`betternat doctor` MUST check:

- agent config parse,
- instance metadata access,
- IAM permissions,
- source/destination check disabled,
- LoxiLB running and API reachable,
- LoxiLB egress rules present,
- nftables fallback availability,
- IP forwarding enabled,
- DynamoDB lease table reachable,
- route table target matches expected active,
- EIP association matches expected active,
- outbound probe source IP,
- Prometheus endpoint reachable.

Doctor SHOULD return machine-readable JSON with status:

```text
ok
warning
critical
unknown
```

## CLI Scope

v0 CLI MAY include:

- `betternat doctor`
- `betternat status`
- `betternat datapath status`
- `betternat failover status`
- `betternat cost estimate`
- `betternat discover aws`

CLI MUST NOT be the primary durable infrastructure mutation path. Terraform remains the source of truth for install/lifecycle.

## Security Requirements

v0 MUST:

- run without inbound SSH by default,
- use least-privilege runtime IAM where practical,
- avoid broad LoxiLB cloud permissions,
- bind local admin API to localhost by default unless explicitly configured,
- avoid exposing LoxiLB API publicly,
- tag all managed cloud resources,
- store rollback metadata,
- log cloud mutations with request ID and result.

Temporary SSH may be used in spike/test environments, but must not be part of the default production install.

## Rollback

The provider MUST capture previous route targets before mutating private route tables.

Rollback SHOULD support:

- restore route target to previous NAT Gateway, instance, ENI, or gateway ID,
- detach BetterNAT active route ownership,
- leave logs explaining what changed.

Agent-side rollback helper MAY exist, but Terraform state must remain authoritative for managed resources.

## Packaging

v0 can start with AMI/bootstrap installation.

Appliance MUST install or start:

- LoxiLB,
- betternat-agent,
- nftables fallback prerequisites,
- metrics endpoint,
- OS sysctl settings.

Open decision:

- LoxiLB as container,
- LoxiLB as package/systemd service,
- BetterNAT-managed bundled install.

The spike used container mode, but rule persistence failed across container restart, so packaging must include agent-driven rule replay.

## Acceptance Criteria

v0 single-node acceptance:

- Terraform creates one BetterNAT appliance.
- Private test instance reaches internet through BetterNAT.
- `checkip` returns the expected EIP.
- DNS/UDP works.
- LoxiLB rule counters increase.
- Prometheus metrics are scrapeable.
- `doctor` returns ok or documented warnings.
- Rollback route target is recorded.

v0 HA acceptance:

- Terraform creates active/standby appliances.
- Standby has datapath ready before takeover.
- DynamoDB lease fences failover.
- EIP reassociates to standby.
- Private route table changes to standby.
- New connections use same public EIP after failover.
- Agent exports failover event and duration metrics.
- Active connection preservation is not required.

v0 fallback acceptance:

- User can select `datapath.engine = "nftables"`.
- Private test instance reaches internet.
- Basic counters and conntrack usage are exported.
- Doctor clearly reports fallback mode.

## Open Questions

- Exact LoxiLB API endpoints to use instead of `loxicmd`.
- Production packaging model for LoxiLB.
- Port exhaustion behavior under many sources and destinations.
- Fragment/ICMP behavior under real workloads.
- Best top-N attribution storage and API shape.
- Whether active connection preservation is worth v1 research.
