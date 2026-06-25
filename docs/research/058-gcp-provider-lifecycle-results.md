# GCP Provider Lifecycle Results

Date: 2026-06-25

## Summary

This validation closed the live GCP gate for provider-owned runtime service
account, runtime IAM custom role and binding, and Firestore Native database
lifecycle.

The first run proved that the lifecycle works, but exposed an unsafe shared
custom role default. `manage_runtime_iam` used the fixed role ID
`betterNATRuntime`; destroying one gateway could delete the shared project role
used by another gateway. The provider was changed to derive a per-gateway
`runtime_iam_role_id` and expose it in state.

The second run validated the fixed behavior live.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Terraform provider: local dev override from this branch
- Artifact source: disposable public-read GCS bucket for each run

## Run 1: Shared Role Finding

- Run ID: `bnat-gcp-lc-20260625104016`
- Firestore database: `bnatlc104016`
- Runtime service account:
  `bnat-gcp-lc-20260625104016-run@smooth-calling-490406-d9.iam.gserviceaccount.com`
- Runtime role before fix: `projects/smooth-calling-490406-d9/roles/betterNATRuntime`

Apply passed and created:

- Firestore Native database in `us-west2`,
- runtime service account,
- runtime custom role with the provider permission contract,
- IAM binding for the runtime service account,
- one gateway VM and tagged default route.

Destroy passed. The Firestore database was deleted, service account and IAM
binding were removed, and Compute residual scan passed. GCP retained the custom
role in soft-deleted state, which is normal for deleted project custom roles.

Finding: the fixed role ID was not safe for provider-owned lifecycle because it
was shared across all BetterNAT gateways in a project.

## Fix

`betternat_gcp_gateway` now exposes:

```hcl
runtime_iam_role_id = optional string
```

When unset and `manage_runtime_iam = true`, the provider derives a gateway-name
scoped role ID. This keeps provider-owned IAM lifecycle isolated per gateway.

## Run 2: Per-Gateway Role Validation

- Run ID: `bnat-gcp-lc2-20260625105420`
- Firestore database: `bnatlc2105420`
- Runtime service account:
  `bnat-gcp-lc2-20260625105420-ru@smooth-calling-490406-d9.iam.gserviceaccount.com`
- Runtime role:
  `projects/smooth-calling-490406-d9/roles/bnatGcpLc220260625105420Runtime`
- Agent config hash:
  `76a1eca45a6188197826c7432ad94b2582327bfb3b52e4736558851b56f7f632`
- Route target: `bnat-gcp-lc2-20260625105420-gw-a`

Apply evidence:

- `runtime_iam_role_id` output was
  `bnatGcpLc220260625105420Runtime`.
- Firestore database was `FIRESTORE_NATIVE`, `STANDARD`, in `us-west2`, with
  delete protection disabled.
- Runtime service account existed with BetterNAT display name and description.
- Runtime role contained the provider's GCP route-only HA permission contract.
- Project IAM policy bound the runtime service account to the per-gateway role.
- The tagged route pointed at the gateway instance.

Destroy evidence:

- Terraform destroy completed; provider resource deletion took `5m34s`, mostly
  waiting for Firestore database deletion.
- Artifact bucket deletion completed.
- Residual scan found zero instances, routes, firewall rules, addresses,
  service accounts, or Firestore records. The provider-owned Firestore database
  was absent.
- Post-destroy IAM check found no matching service accounts and no IAM binding.
- The per-gateway custom role remained only in GCP soft-deleted state.

## Decision Impact

The provider-owned GCP lifecycle gates for runtime service account,
`manage_runtime_iam`, and `manage_firestore_database` are now live-validated.
The remaining GCP GA blockers are still raw LoxiLB HA comparison,
split-brain/failure injection, TCP/UDP/DNS/long-download failover behavior,
stable public identity decision, capacity repair, and release packaging.
