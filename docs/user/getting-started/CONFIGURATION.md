# BetterNAT Configuration

Date: 2026-06-21

## Terraform Provider Inputs

Use `betternat_gateway` to deploy the AWS gateway stack.

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
| `cloud` | Optional cloud selector. Defaults to `aws`; BetterNAT currently supports AWS. |
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

## Update Behavior

The provider updates only `min_size`, `desired_capacity`, and `max_size` in
place. Most other input changes require replacing the `betternat_gateway`
resource.
Use the [Upgrade And Replacement Guide](../operations/UPGRADE_REPLACEMENT_GUIDE.md)
before changing runtime, AMI, subnet, route, datapath, EIP, HA timing, tag, or
bootstrap fields.

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
