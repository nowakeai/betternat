# GCP Alpha Boundary

Date: 2026-06-25

## Summary

GCP support is feasible but not a provider-schema-only change. A disposable GCP
spike has validated forwarding, tagged static route replacement, and cleanup.
The nftables masquerade part of that spike is historical substrate evidence,
not a product fallback or GCP acceptance path. It has not validated BetterNAT
agent
coordination, stable public IP handover, or production GKE route migration.

The provider may expose a narrow alpha `betternat_gcp_gateway` resource for the
validated topology, but product docs must not present GCP as production
supported.

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
- LoxiLB datapath only; no nftables fallback acceptance path,
- static route replacement for new-flow recovery,
- Firestore or GCS conditional-write coordination,
- explicit cleanup and residual scans.

Do not include production GKE route replacement or existing Cloud NAT migration
in the first alpha.

## Deferred Until Spike Evidence

## Accepted After Forwarding Spike

- `internal/install/gcp` for the verified GCE forwarding topology.
- Alpha `betternat_gcp_gateway` managing provider-owned gateway VMs and a
  tagged default route.
- Alpha `betternat_gcp_gateway_status` reading Compute route and instance
  state.

## Still Deferred

- GCP module repository
- GCP IAM least-privilege guide
- GCP stable public identity guarantees
- GCP lease backend and agent fencing
- LoxiLB as the primary GCP datapath

## Decision

Keep AWS as the production-oriented provider surface for this reset. Add GCP
only as a clearly documented alpha path backed by
`docs/research/051-gcp-forwarding-spike-results.md`.
