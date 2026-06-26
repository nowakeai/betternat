# Data Source: betternat_gcp_gateway_status

Reads current GCP Compute state for an existing BetterNAT GCP gateway without
modifying resources.

This data source reports gateway instances, per-gateway public IPs, and the
configured route target. It does not replace gateway-local `betternat status`,
`betternat doctor --live`, Prometheus metrics, or Firestore handover history.

## Example

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

## Outputs

- `gateway_statuses`
- `egress_public_ips`
- `route_target`
- `status`
