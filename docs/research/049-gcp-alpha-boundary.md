# GCP Alpha Boundary

Date: 2026-06-25

## Summary

GCP support is feasible but not a provider-schema-only change. BetterNAT should
not expose a working `betternat_gcp_gateway` resource until a disposable GCP
spike validates forwarding, static route replacement, coordination semantics,
and cleanup.

The provider may reserve `betternat_gcp_gateway_status` as a clear
not-implemented stub during the Terraform surface reset, but product docs must
not present GCP as supported.

## Current Business Signal

The strongest near-term target remains the GKE cluster projects identified in
the scratch research:

- `gcp-cluster-2`
- `gcp-cluster-1`

Those projects showed a roughly `$926` 30-day gross Cloud NAT baseline in the
billing-export snapshot, with about `$851` from Cloud NAT data processing.
Credits can reduce net cash cost, so future go/no-go work should refresh live
billing before making a product or migration claim.

## Alpha Scope

An alpha should start with:

- disposable VPC only,
- single region,
- single-zone HA group,
- GCE gateway VMs with `canIpForward=true`,
- LoxiLB primary datapath and nftables fallback,
- static route replacement for new-flow recovery,
- Firestore or GCS conditional-write coordination,
- explicit cleanup and residual scans.

Do not include production GKE route replacement or existing Cloud NAT migration
in the first alpha.

## Deferred Until Spike Evidence

- `internal/install/gcp`
- working `betternat_gcp_gateway`
- working `betternat_gcp_gateway_status`
- GCP module repository
- GCP IAM least-privilege guide
- GCP stable public identity guarantees

## Decision

Keep AWS as the implemented provider surface for this reset. Reserve GCP names
only where they prevent future naming churn, and gate real GCP implementation
on `docs/testing/GCP_SPIKE_PLAN.md`.
