# BetterNAT Configuration

Date: 2026-06-21

## Terraform Provider Inputs

Use `betternat_gateway` to deploy the alpha AWS gateway stack.

Provider version is specified in Terraform/OpenTofu `required_providers`, not on the resource itself:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.6"
    }
  }
}
```

The gateway runtime version is separate from the provider version. Set
`betternat_version` to a supported BetterNAT release tag, such as
`v0.1.0-alpha.2`. The provider derives the matching agent and CLI GitHub Release
artifact URLs and SHA256 checksums from its built-in release manifest.

Terraform Registry install is the default path. If Registry availability is
temporarily delayed, install the provider from the GitHub release as a
filesystem mirror:

```sh
source scripts/setup-provider-github-mirror.sh
```

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
| `ami_id` | Explicit Linux AMI ID. Required for the first alpha bootstrap path. |
| `ami_channel` | Future AMI channel selector. Do not rely on it for `v0.1.0-alpha.2`. |
| `bootstrap_mode` | `cloud_init` by default. Use `cloud_init` for ordinary Linux AMIs that install BetterNAT at first boot. Use `prebaked_ami` only for BetterNAT AMIs that already contain Docker or the selected LoxiLB runtime, LoxiLB, `betternat`, `betternat-agent`, `loxicmd`, sysctl settings, and systemd units. |
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

### Capacity

| Name | Default | Description |
| --- | --- | --- |
| `instance_type` | `t3.small` | Gateway node instance type. The AWS supplemental fixture uses arm64 `t4g.small`. |
| `use_spot` | `false` | Use Spot instances. Good for disposable tests; be cautious for real egress. |
| `min_size` | provider default | ASG minimum size. |
| `desired_capacity` | provider default | ASG desired capacity. Use `2` for active/standby HA. |
| `max_size` | provider default | ASG maximum size. |

Capacity-only updates are intended to be in-place. Other topology or bootstrap changes may require replacement.

### Datapath

| Name | Default | Description |
| --- | --- | --- |
| `datapath_engine` | `loxilb` | BetterNAT node datapath. |

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
| `route_mode` | `replace_route` | Current AWS route failover mode. |
| `route_destination_cidr` | `0.0.0.0/0` | Destination route managed by BetterNAT. |
| `route_target_type` | `instance` | Current route target type. |
| `rollback_on_destroy` | `true` | Attempt to restore captured route targets during destroy. |
| `allow_destroy_without_rollback` | `false` | Allow destroy to proceed when rollback cannot be performed. Use carefully. |

## Runtime Config

Terraform renders `/etc/betternat/agent.json` onto each node.

Users normally should not edit this file by hand. If you do, restart:

```sh
systemctl restart betternat-agent.service
```

Then verify:

```sh
betternat doctor --live --config /etc/betternat/agent.json
```
