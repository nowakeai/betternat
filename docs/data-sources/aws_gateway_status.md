# Data Source: betternat_aws_gateway_status

Reads current AWS control-plane state for an existing BetterNAT AWS gateway
without modifying resources.

This data source is for module authors and validation workflows. It does not
replace gateway-local `betternat status` or `betternat doctor --live`.

## Example

```hcl
data "betternat_aws_gateway_status" "egress" {
  name              = betternat_aws_gateway.egress.name
  region            = betternat_aws_gateway.egress.region
  install_plan_json = betternat_aws_gateway.egress.install_plan_json
}
```

## Outputs

- `egress_public_ips`
- `route_targets`
- `active_instance_ids`
- `coordination_table_name`
- `control_plane_status_json`
- `status`
