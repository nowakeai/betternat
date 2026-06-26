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
  version = "v0.2.0"
  os      = "linux"
  arch    = "arm64"
}
```

Inputs:

| Name | Description |
| --- | --- |
| `version` | BetterNAT runtime release tag, for example `v0.2.0`. |
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

Reads current GCP Compute state for a BetterNAT GCP alpha gateway without
modifying cloud resources.

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

Inputs:

| Name | Description |
| --- | --- |
| `name` | BetterNAT GCP alpha gateway base name. |
| `project_id` | GCP project ID. |
| `region` | GCP region. |
| `zone` | GCP zone containing provider-owned gateway VMs. |
| `network` | Existing VPC network name. |
| `subnetwork` | Existing regional subnetwork name. |
| `client_tag` | GCE network tag used by private clients. |
| `route_name` | Optional route name. Defaults to `<name>-default-via-gateway`. |
| `gateway_count` | Optional expected provider-owned gateway count. Defaults to `2`. |

Outputs:

| Name | Description |
| --- | --- |
| `gateway_statuses` | GCE instance status by provider-owned gateway instance name. |
| `egress_public_ips` | Per-gateway public IPv4 addresses. Stable public identity, when configured, is checked through gateway-local status and GCP address ownership. |
| `route_target` | Current route next-hop instance base name. |
| `status` | Best-effort summary: `active` when gateway instances and route target are present, otherwise `missing`. |

This data source reports GCP Compute state only. It does not replace
gateway-local `betternat status`, `doctor --live`, Prometheus metrics, or
Firestore handover history for runtime HA checks.
