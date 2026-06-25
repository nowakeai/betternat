# BetterNAT Provider Data Sources

Date: 2026-06-25

## Purpose

This reference covers read-only BetterNAT provider data sources. These data
sources are for module authors, advanced Terraform users, and validation
workflows. They do not replace `betternat status` or `betternat doctor --live`
for operational checks on a gateway node.

## `betternat_runtime_artifacts`

Returns provider-supported BetterNAT runtime artifact URLs and SHA256 checksums.

```hcl
data "betternat_runtime_artifacts" "current" {
  version = "v0.1.0"
  os      = "linux"
  arch    = "arm64"
}
```

Inputs:

| Name | Description |
| --- | --- |
| `version` | BetterNAT runtime release tag, for example `v0.1.0`. |
| `os` | Runtime operating system. Current supported value: `linux`. |
| `arch` | Runtime architecture. Current supported values: `amd64` and `arm64`. |

Outputs:

| Name | Description |
| --- | --- |
| `agent_binary_url` | `betternat-agent` release artifact URL. |
| `agent_binary_sha256` | SHA256 checksum for `agent_binary_url`. |
| `cli_binary_url` | `betternat` CLI release artifact URL. |
| `cli_binary_sha256` | SHA256 checksum for `cli_binary_url`. |
| `loxicmd_binary_url` | Reserved for future provider-managed `loxicmd` artifacts; empty today. |
| `loxicmd_binary_sha256` | Reserved for future provider-managed `loxicmd` checksums; empty today. |

## `betternat_aws_gateway_status`

Reads current AWS control-plane state for an existing BetterNAT gateway without
modifying cloud resources.

```hcl
data "betternat_aws_gateway_status" "egress" {
  name              = betternat_aws_gateway.egress.name
  region            = betternat_aws_gateway.egress.region
  install_plan_json = betternat_aws_gateway.egress.install_plan_json
}
```

Current status reads require `install_plan_json` from `betternat_aws_gateway`.
That keeps the data source explicit: it uses the exact route table, EIP, and
coordination table names the provider generated, instead of guessing from cloud
resource naming conventions.

Inputs:

| Name | Description |
| --- | --- |
| `name` | BetterNAT gateway name. |
| `region` | AWS region. |
| `install_plan_json` | Sensitive install plan JSON from `betternat_aws_gateway.install_plan_json`. |

Outputs:

| Name | Description |
| --- | --- |
| `egress_public_ips` | Current public egress IPs by availability zone when stable public identity is enabled. |
| `route_targets` | Current managed default-route targets by route table ID. |
| `active_instance_ids` | Current public-identity owner instance IDs by availability zone when available. |
| `coordination_table_name` | Provider-owned DynamoDB coordination table name from the install plan. |
| `control_plane_status_json` | Raw JSON status returned by the AWS reader. |
| `status` | Best-effort summary: `active`, `degraded`, or `created`. |

## `betternat_gcp_gateway_status`

This data source name is reserved for the GCP alpha surface. It currently
returns a not-implemented diagnostic because GCP route replacement, gateway
forwarding, and coordination behavior still need disposable-environment
validation before BetterNAT exposes a working GCP provider resource.
