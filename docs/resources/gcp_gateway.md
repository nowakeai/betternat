# Resource: betternat_gcp_gateway

Manages a GCP BetterNAT gateway group.

Most users should prefer the GCP module:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/google"
  version = "~> 0.2"

  name       = "prod-egress"
  project_id = var.project_id
  region     = "us-west2"
  zone       = "us-west2-a"

  network    = google_compute_network.main.name
  subnetwork = google_compute_subnetwork.private.name
  client_tag = "private-egress-client"

  private_cidrs = ["10.10.0.0/16"]

  betternat_version = "v0.2.0"
}
```

Use this resource directly when you need the lower-level provider primitive.

## Example

```hcl
resource "betternat_gcp_gateway" "egress" {
  name       = "prod-egress"
  project_id = var.project_id
  region     = "us-west2"
  zone       = "us-west2-a"

  network    = google_compute_network.main.name
  subnetwork = google_compute_subnetwork.private.name
  client_tag = "private-egress-client"

  private_cidrs = ["10.10.0.0/16"]

  enable_agent_ha       = true
  capacity_repair_mode  = "mig"
  betternat_version     = "v0.2.0"
  firestore_database_id = "(default)"

  manage_runtime_service_account = true
  manage_runtime_iam             = true
}
```

## Route Ownership

BetterNAT owns the tagged static route named by `route_name`. The route applies
to VMs with `client_tag`. Do not manage another route with the same name while
BetterNAT is active.

## Stable Public Identity

Set `stable_public_identity_address_name` to an existing regional static
external IPv4 address name when private workloads need a stable egress identity.
The provider does not create or delete that address.

GCP handover is connectivity-first: BetterNAT moves the private workload route
first, then converges the static public identity. During that transition,
successful new connections may temporarily use the target gateway's ordinary
public IP before the static IP returns.

## Updates

In-place updates are not implemented for the GCP resource. Replace the resource
to change topology, route, capacity, image, bootstrap, or HA settings.

## Destroy

Terraform destroy removes provider-owned gateway resources. Provider-managed
runtime service accounts are retained during gateway cleanup so same-name
replacement remains reliable on GCP; remove them only after all gateways using
the account are destroyed. If you use a shared Firestore database or a static
public address owned by another stack, those shared resources remain outside
this resource's lifecycle.
