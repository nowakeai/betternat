# BetterNAT GCP Quick Start

Date: 2026-06-26

## Purpose

This guide deploys BetterNAT into a disposable GCP VPC, verifies private-client
egress through BetterNAT, and destroys the test stack.

Use this before replacing a real GCP egress path.

## What This Test Proves

This disposable test checks that:

- Terraform can install the BetterNAT GCP module and provider.
- Gateway VMs boot with LoxiLB, `betternat-agent`, and the `betternat` CLI.
- Firestore lease/fencing selects one active gateway.
- The active gateway owns the tagged private default route.
- A private client with the configured network tag reaches the public internet.
- `betternat status`, `doctor --live`, handover history, and Prometheus metrics
  expose useful state.
- Terraform destroy removes the test resources and leaves no run-scoped residue.

## Prerequisites

Install locally:

- Terraform,
- Google Cloud SDK,
- `jq`.

Choose a disposable project or folder-owned sandbox. The project must have:

- Compute Engine API enabled,
- Firestore API enabled,
- IAM API enabled if the module creates runtime service accounts or custom
  roles,
- a Firestore Native database, or permission for Terraform to create one,
- quota for two gateway VMs and one private test client,
- permission to create routes, firewall rules, instance templates, managed
  instance groups, and service accounts.

Set:

```sh
export BETTERNAT_GCP_PROJECT="<project-id>"
export BETTERNAT_GCP_REGION="us-west2"
export BETTERNAT_GCP_ZONE="us-west2-a"
export BETTERNAT_VERSION="v0.2.0"
export BETTERNAT_GCP_RUN_ID="betternat-gcp-test-$(date -u +%Y%m%d%H%M%S)"
```

Expected GCP costs:

- gateway VMs,
- one private test VM,
- persistent disks,
- regional static external IPv4 when stable public identity is enabled,
- Firestore reads/writes/storage,
- normal internet egress,
- logs and monitoring if enabled by your project defaults.

## Terraform Shape

The GCP module is the user-facing install path:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/google"
  version = "~> 0.2"

  name       = var.name
  project_id = var.project_id
  region     = var.region
  zone       = var.zone

  network    = google_compute_network.lab.name
  subnetwork = google_compute_subnetwork.lab.name
  client_tag = "${var.name}-client"

  private_cidrs = ["10.91.0.0/24"]

  betternat_version = var.betternat_version

  manage_runtime_service_account = true
  manage_runtime_iam             = true
}
```

Use `betternat_version` for a GCP-capable BetterNAT runtime release. Explicit
binary URL and checksum overrides are for maintainer validation runs, not normal
user installs.

## Stable Public Identity

By default the module does not create a regional static external IPv4 address.
Pass `stable_public_identity_address_name` to use an existing regional static
external address:

```hcl
resource "google_compute_address" "egress" {
  project      = var.project_id
  region       = var.region
  name         = "${var.name}-egress"
  address_type = "EXTERNAL"
}

module "betternat" {
  source = "nowakeai/betternat/google"

  # ...

  stable_public_identity_address_name = google_compute_address.egress.name
}
```

For stable public identity, the gateway subnet needs Private Google Access or
an equivalent private path to Google APIs. BetterNAT uses a connectivity-first
handover on GCP: it moves private workload routes first, then converges the
static public identity. During that transition, successful new flows may
temporarily use the target gateway's ordinary public IP before the static IP
returns.

## Route Ownership

BetterNAT owns the tagged private default route named by `route_name`. The route
applies to VMs with `client_tag`.

Do not manage another route with the same name while BetterNAT is active.

## Verify

After apply, inspect module outputs:

```sh
terraform output
```

Check the managed route:

```sh
gcloud --project "$BETTERNAT_GCP_PROJECT" compute routes describe \
  "$(terraform output -raw route_name)" \
  --format=json
```

Run on the active gateway node:

```sh
sudo betternat status
sudo betternat doctor --live
sudo betternat handover history --limit 20
curl -fsS http://127.0.0.1:9108/metrics | head
```

From a private client VM with `client_tag`, verify public egress:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

Expected:

- one active gateway,
- at least one healthy standby when `gateway_count >= 2`,
- Firestore lease owner matches the active gateway,
- the tagged route target matches the active gateway,
- LoxiLB datapath is ready,
- private-client egress succeeds,
- stable public identity, when configured, converges to the regional static
  external IPv4 address.

## Destroy And Residual Checks

Destroy the stack:

```sh
terraform destroy
```

Then check for run-scoped resources:

```sh
gcloud --project "$BETTERNAT_GCP_PROJECT" compute instances list \
  --filter="name~${BETTERNAT_GCP_RUN_ID}"
gcloud --project "$BETTERNAT_GCP_PROJECT" compute routes list \
  --filter="name~${BETTERNAT_GCP_RUN_ID}"
gcloud --project "$BETTERNAT_GCP_PROJECT" compute firewall-rules list \
  --filter="name~${BETTERNAT_GCP_RUN_ID}"
gcloud --project "$BETTERNAT_GCP_PROJECT" iam service-accounts list \
  --filter="email~${BETTERNAT_GCP_RUN_ID}"
```

If a disposable run wrote handover records to Firestore, remove only the
run-scoped BetterNAT records after confirming the gateway stack is destroyed.
Do not delete a shared Firestore database unless this Terraform stack owns it.

## Next Steps

- Read [Limitations](../reference/LIMITATIONS.md) before using BetterNAT for a
  real route.
- Read [IAM Policy](../reference/IAM_POLICY.md) if service accounts, custom
  roles, or Firestore are managed by a separate infra-admin stack.
- Read [Operations Guide](../operations/OPERATIONS_GUIDE.md) for daily checks.
- Read [Rollback Guide](../operations/ROLLBACK_GUIDE.md) before touching an
  existing egress path.
