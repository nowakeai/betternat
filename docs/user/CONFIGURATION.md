# BetterNAT Configuration

Date: 2026-06-21

## Terraform Provider Inputs

Use `betternat_gateway` to deploy the alpha AWS gateway stack.

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
| `ami_channel` | Future AMI channel selector. Do not rely on it for `v0.1.0-alpha.1`. |
| `agent_binary_url` | Sensitive URL for `betternat-agent`. |
| `agent_binary_sha256` | SHA256 checksum for the agent artifact. |
| `cli_binary_url` | Sensitive URL for the `betternat` CLI. |
| `cli_binary_sha256` | SHA256 checksum for the CLI artifact. |
| `loxicmd_binary_url` | Optional URL for a host `loxicmd` binary. If empty, bootstrap installs a Docker wrapper. |
| `loxicmd_binary_sha256` | Optional checksum for `loxicmd_binary_url`. |

Launch templates created by the provider require IMDSv2 and set the metadata hop limit to `1`.

### Capacity

| Name | Default | Description |
| --- | --- | --- |
| `instance_type` | `t3.small` | Gateway appliance instance type. The AWS supplemental fixture uses arm64 `t4g.small`. |
| `use_spot` | `false` | Use Spot instances. Good for disposable tests; be cautious for real egress. |
| `min_size` | provider default | ASG minimum size. |
| `desired_capacity` | provider default | ASG desired capacity. Use `2` for active/standby HA. |
| `max_size` | provider default | ASG maximum size. |

Capacity-only updates are intended to be in-place. Other topology or bootstrap changes may require replacement.

### Datapath

| Name | Default | Description |
| --- | --- | --- |
| `datapath_engine` | `loxilb` | Primary datapath. |
| `fallback_datapath_engine` | `nftables` | Fallback datapath for support and emergency use. |

LoxiLB has its own eBPF conntrack state. Linux `nf_conntrack_max` is retained for nftables fallback and host compatibility; it is not the primary LoxiLB capacity knob.

### Egress Identity

| Name | Default | Description |
| --- | --- | --- |
| `stable_egress_ip` | `true` | If true, BetterNAT manages a shared EIP so new flows converge back to the same public IP after failover. If false, the public source IP changes to the active instance's public IP after failover. |

### HA Timing

| Name | Default | Description |
| --- | --- | --- |
| `ha_profile` | `stable` | Timing profile. Use `stable`, `balanced`, or `fast`. |
| `ha_lease_ttl_seconds` | profile default | Advanced override for lease TTL. |
| `ha_renew_interval_seconds` | profile default | Advanced override for renew interval. |

Guidance:

- `stable`: conservative default for real workloads.
- `balanced`: moderate failover speed for testing or less sensitive workloads.
- `fast`: lower latency but more sensitive to AWS/API jitter; useful for tests.

### Observability

| Name | Default | Description |
| --- | --- | --- |
| `prometheus_enabled` | `true` | Expose Prometheus metrics from each appliance. |

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

Terraform renders `/etc/betternat/agent.json` onto each appliance.

Users normally should not edit this file by hand. If you do, restart:

```sh
systemctl restart betternat-agent.service
```

Then verify:

```sh
betternat doctor --live --config /etc/betternat/agent.json
```
