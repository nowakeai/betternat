# BetterNAT Configuration

Date: 2026-06-21

## Terraform Provider Inputs

Use `betternat_aws_gateway` to deploy the AWS gateway stack.

Provider version is specified in Terraform/OpenTofu `required_providers`, not on the resource itself:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0"
    }
  }
}
```

The gateway runtime version is separate from the provider version. Set
`betternat_version` to a supported BetterNAT release tag from the current Quick
Start or release notes. The provider derives the matching agent and CLI GitHub
Release artifact URLs and SHA256 checksums from its built-in release manifest.

OpenTofu can use the same `source = "nowakeai/betternat"` address now that
the provider is registered in the OpenTofu Registry.

### Required

| Name | Description |
| --- | --- |
| `name` | Gateway name. Used in resource names and HA group identity. |
| `region` | AWS region. |
| `vpc_id` | Target VPC ID. |
| `public_subnet_ids` | Map of AZ name to public subnet ID. |
| `private_route_table_ids` | Map of AZ name to private route table ID list. |
| `private_cidrs` | CIDRs allowed to use the gateway for SNAT. |

### Bootstrap

| Name | Description |
| --- | --- |
| `ami_id` | Explicit Linux AMI ID. Required for the `cloud_init` path. |
| `ami_channel` | Reserved AMI channel selector. Accepted values are `stable`, `candidate`, and `dev`, but BetterNAT does not currently resolve channels into public AMIs. Set `ami_id` directly. |
| `bootstrap_mode` | `cloud_init` by default. Use `cloud_init` for ordinary Linux AMIs that install BetterNAT at first boot. Use `prebaked_ami` only for BetterNAT AMIs that already contain Docker or the selected LoxiLB runtime, LoxiLB, `betternat`, `betternat-agent`, `loxicmd`, sysctl settings, and systemd units. |
| `associate_public_ip_address` | Optional advanced override for the launch template network interface public IPv4 setting. Leave unset for provider-derived behavior. |
| `betternat_version` | BetterNAT runtime release tag. The provider uses it with `instance_type` to derive agent/CLI bootstrap URLs and checksums. |
| `agent_binary_url` | Sensitive URL override for `betternat-agent`. Usually leave unset when `betternat_version` is set. |
| `agent_binary_sha256` | SHA256 checksum override for the agent artifact. Usually leave unset when `betternat_version` is set. |
| `cli_binary_url` | Sensitive URL override for the `betternat` CLI. Usually leave unset when `betternat_version` is set. |
| `cli_binary_sha256` | SHA256 checksum override for the CLI artifact. Usually leave unset when `betternat_version` is set. |
| `loxicmd_binary_url` | Optional URL for a host `loxicmd` binary. If empty, bootstrap installs a Docker wrapper. |
| `loxicmd_binary_sha256` | Optional checksum for `loxicmd_binary_url`. |

Launch templates created by the provider require IMDSv2 and set the metadata hop limit to `1`.

In `cloud_init` mode, gateway nodes launch in the configured public subnets with
auto-assigned public IPv4 enabled by default. That per-node public IPv4 is for
bootstrap and management/control-plane reachability: package repositories,
Docker image pull, GitHub release artifacts, SSM, and AWS APIs. It is separate
from `stable_egress_ip`, which controls whether BetterNAT also manages a shared
EIP as the public identity for private-subnet egress.

In `prebaked_ami` mode, user data only writes runtime config, applies the
baseline sysctl profile, and starts preinstalled services. With
`stable_egress_ip=true`, the provider disables per-node auto-assigned public
IPv4 because bootstrap downloads are not required and the shared EIP provides
the egress identity. With `stable_egress_ip=false`, per-node public IPv4 remains
enabled because the active gateway node's public IP is the egress identity.

Set `associate_public_ip_address` only when you deliberately want to override
that derived behavior. For example, a private VPC with NAT/VPC endpoints may set
it to `false` even in `cloud_init` mode. A troubleshooting environment may set
it to `true` even for a prebaked stable-EIP AMI.

### Capacity

| Name | Default | Description |
| --- | --- | --- |
| `instance_type` | `t3.small` | Gateway node instance type. The AWS supplemental fixture uses arm64 `t4g.small`. |
| `use_spot` | `false` | Use Spot instances. Good for disposable tests; be cautious for real egress. |
| `min_size` | `1` | ASG minimum size. |
| `desired_capacity` | `2` | ASG desired capacity. Use `2` for active/standby HA. |
| `max_size` | `3` | ASG maximum size. |

Capacity-only updates are intended to be in-place. Other topology or bootstrap changes may require replacement.

### Datapath

| Name | Default | Description |
| --- | --- | --- |
| `datapath_engine` | `loxilb` | BetterNAT node datapath. |
| `fallback_datapath_engine` | `nftables` | Fallback/debug datapath engine value rendered into runtime config. Currently accepts `nftables`. |

LoxiLB has its own eBPF conntrack state. Linux `nf_conntrack_max` is not the primary LoxiLB capacity knob.

### Egress Identity

| Name | Default | Description |
| --- | --- | --- |
| `stable_egress_ip` | `true` | If true, BetterNAT manages a shared EIP so new private-subnet egress flows converge back to the same public IP after failover. Gateway nodes may still have ordinary per-node public IPv4 addresses for bootstrap and management. If false, BetterNAT skips the shared EIP and the public source IP changes to the active instance's public IP after failover. |

### HA Timing

| Name | Default | Description |
| --- | --- | --- |
| `ha_profile` | `default` | Timing profile. Use `default`. Legacy values `stable`, `balanced`, and `fast` are accepted as aliases for `default`. |
| `ha_lease_ttl_seconds` | profile default | Advanced override for lease TTL. |
| `ha_renew_interval_seconds` | profile default | Advanced override for renew interval. |

The default profile uses a 10 second lease TTL and a 1 second renew/check interval. Override the two advanced fields only when you need a deliberately different timing envelope.

### Observability

| Name | Default | Description |
| --- | --- | --- |
| `prometheus_enabled` | `true` | Expose Prometheus metrics from each node. |

Default metrics endpoint:

```text
http://<gateway-private-ip>:9108/metrics
```

### Route And Rollback

| Name | Default | Description |
| --- | --- | --- |
| `route_mode` | `replace_route` | Current AWS route failover mode. Currently supports `replace_route`. |
| `route_destination_cidr` | `0.0.0.0/0` | Destination route managed by BetterNAT. |
| `route_target_type` | `instance` | Current route target type. Currently supports `instance`. |
| `rollback_on_destroy` | `true` | Attempt to restore captured route targets during destroy. |
| `allow_destroy_without_rollback` | `false` | Allow destroy to proceed when rollback cannot be performed. Use carefully. |

Use the [Rollback Guide](../operations/ROLLBACK_GUIDE.md) before changing
rollback defaults in an existing VPC.

### Tags

| Name | Default | Description |
| --- | --- | --- |
| `tags` | none | Additional tags applied to provider-managed AWS resources where supported. |

Tag changes require replacement because only capacity fields are updated in
place.

### Computed Outputs

These attributes are written by the provider and are useful for runbooks,
dashboards, and support:

| Name | Description |
| --- | --- |
| `lease_table_name` | Legacy lease table name for older environments. |
| `coordination_table_name` | Coordination table used for HA lease, agent registry, peer discovery, and handover records. |
| `managed_route_table_ids` | Flattened list of private route table IDs managed by BetterNAT. |
| `egress_public_ips` | Public egress IPs recorded by the provider. |
| `active_instance_ids` | Active gateway instance IDs by HA group or AZ. |
| `standby_instance_ids` | Standby gateway instance IDs by HA group or AZ. |
| `rollback_route_targets_json` | Captured previous route targets used during destroy rollback. |
| `control_plane_status_json` | Provider-recorded AWS control-plane status snapshot. |
| `status` | Provider status summary. |

Internal/sensitive computed values include `agent_config_json`,
`agent_config_hash`, `user_data`, `install_plan_json`, `peer_api_auth_token`,
and `provider_infrastructure_revision`. Treat them as provider state, not as
operator inputs.

## Provider Data Sources

Use `betternat_runtime_artifacts` to inspect the provider's built-in runtime
artifact manifest:

```hcl
data "betternat_runtime_artifacts" "current" {
  version = "v0.1.0"
  os      = "linux"
  arch    = "arm64"
}
```

Use `betternat_aws_gateway_status` for read-only AWS control-plane status when
you already have the provider-generated install plan:

```hcl
data "betternat_aws_gateway_status" "egress" {
  name              = betternat_aws_gateway.egress.name
  region            = betternat_aws_gateway.egress.region
  install_plan_json = betternat_aws_gateway.egress.install_plan_json
}
```

Use `betternat_gcp_gateway_status` for read-only GCP alpha Compute status:

```hcl
data "betternat_gcp_gateway_status" "egress" {
  name       = betternat_gcp_gateway.egress.name
  project_id = betternat_gcp_gateway.egress.project_id
  region     = betternat_gcp_gateway.egress.region
  zone       = betternat_gcp_gateway.egress.zone
  network    = betternat_gcp_gateway.egress.network
  subnetwork = betternat_gcp_gateway.egress.subnetwork
  client_tag = betternat_gcp_gateway.egress.client_tag
  route_name = betternat_gcp_gateway.egress.route_name
}
```

## GCP Alpha Resource

`betternat_gcp_gateway` is an alpha resource for disposable GCP validation.
It manages provider-owned GCE gateway VMs with `canIpForward=true`, an nftables
masquerade startup script, and one tagged default route to the active gateway.
It is not yet a GCP product-parity HA release. BetterNAT's HA value over a raw
LoxiLB appliance requires live agent-owned lease fencing, route mutation,
passive failover, proactive handover, observability, and rollback evidence.

The runtime now has an implementation path for Firestore-backed lease
coordination and GCP tagged-route mutation, but it has not yet passed live GCE
HA validation. Treat the resource as alpha until active/passive failover,
proactive handover, LoxiLB-on-GCE, GKE migration safety, IAM, and observability
gates are complete.

Minimal shape:

```hcl
resource "betternat_gcp_gateway" "egress" {
  name       = "lab-egress"
  project_id = "shared-resources-alt"
  region     = "us-west1"
  zone       = "us-west1-a"

  network    = google_compute_network.lab.name
  subnetwork = google_compute_subnetwork.lab.name
  client_tag = "lab-private-client"

  private_cidrs = ["10.91.0.0/24"]
}
```

Private client VMs must have the configured `client_tag` and no broader route
with a higher priority that bypasses the BetterNAT route.

Experimental provider-rendered GCP agent HA bootstrap is available behind an
explicit switch:

```hcl
resource "betternat_gcp_gateway" "egress" {
  name       = "lab-egress"
  project_id = "shared-resources-alt"
  region     = "us-west2"
  zone       = "us-west2-a"

  network    = google_compute_network.lab.name
  subnetwork = google_compute_subnetwork.lab.name
  client_tag = "lab-private-client"

  private_cidrs = ["10.91.0.0/24"]

  enable_agent_ha       = true
  service_account_email = "betternat-runtime@shared-resources-alt.iam.gserviceaccount.com"
  manage_runtime_iam    = true
  firestore_database_id = "(default)"
  betternat_version     = "v0.1.0"
}
```

When `enable_agent_ha = true`, `service_account_email` is required and is
attached to the gateway VMs. The service account must be granted enough access
to read/write the Firestore gateway coordination records, describe/delete/create
the configured static route, read route operation status, and read instance
metadata. The provider renders `agent_config_json`, `agent_config_hash`,
`peer_api_auth_token`, runtime artifact URLs/checksums, and `startup_script`
for a Firestore-backed, route-only agent HA smoke.
The required permission list is exposed as computed
`runtime_iam_permissions`; see [IAM Policy](../reference/IAM_POLICY.md#gcp-alpha-runtime-service-account).
Set `manage_runtime_iam = true` to let this resource create or update the
project-level BetterNAT runtime custom role and bind `service_account_email` to
it. Leave it false when IAM is managed by a separate Terraform stack or an
infra-admin workflow.
Explicit `agent_binary_url`, `agent_binary_sha256`, `cli_binary_url`, and
`cli_binary_sha256` overrides are supported for local mirrors and unreleased
test builds. This path is still experimental until live two-agent route
fencing, passive failover, proactive handover, LoxiLB-on-GCE, IAM, and cleanup
evidence are complete.

The rendered GCP agent config leaves `local.node_id = "auto"`. At runtime the
agent resolves that value from GCE metadata to the local instance name, which is
the same identifier used as the GCP static route `nextHopInstance` target.

Experimental GCP agent HA config uses Firestore coordination and route-only
public identity:

```yaml
cloud: gcp
region: us-west2
gcp:
  project_id: shared-resources-alt
  zone: us-west2-a
  network: lab-vpc
  client_tag: lab-private-client
  route_priority: 800
  firestore_database_id: betternat-test
ha:
  enabled: true
  lease:
    backend: firestore
    key: lab-egress-us-west2-a
  route_failover:
    mode: replace_route
    route_table_ids:
      - lab-egress-default-via-gateway
    destination_cidr: 0.0.0.0/0
    target_type: instance
  public_identity: {}
coordination:
  backend: firestore
```

For GCP, `route_table_ids` currently means GCP static route names. Shared public
identity is intentionally unsupported until a stable GCP public IP handover
strategy is proven.

## Update Behavior

The provider updates only `min_size`, `desired_capacity`, and `max_size` in
place. Most other input changes require replacing the `betternat_aws_gateway`
resource.
Use the [Upgrade And Replacement Guide](../operations/UPGRADE_REPLACEMENT_GUIDE.md)
before changing runtime, AMI, subnet, route, datapath, EIP, HA timing, tag, or
bootstrap fields.

`betternat_gcp_gateway` updates are intentionally not implemented in the first
alpha. Replace the resource to change GCP gateway topology.

## Runtime Config

Terraform renders `/etc/betternat/agent.json` onto each node.

Users normally should not edit this file by hand. If you do, restart:

```sh
systemctl restart betternat-agent.service
```

Then verify:

```sh
betternat doctor --live
```
