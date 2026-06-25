# No nftables Fallback Decision

Date: 2026-06-25

## Decision

BetterNAT does not have a product fallback datapath.

The supported datapath is LoxiLB. If LoxiLB cannot pass a cloud, kernel,
packaging, or release acceptance gate, that is a product blocker or a new
architecture decision. It must not be bypassed by passing the release with
nftables.

This applies to all clouds and all release gates. It is not a GCP-only
decision.

## Current Codebase Treatment

Existing nftables/nf_conntrack code may remain temporarily to avoid risky
removal and to preserve legacy diagnostics while the codebase is simplified.
That code is not a supported product fallback and must not be expanded into one.

Allowed while the code remains:

- keep existing tests green when touching the legacy code,
- use existing smoke scripts as legacy Linux diagnostics,
- remove the code gradually during future cleanup.

Not allowed:

- requiring nftables for release acceptance,
- documenting nftables as an operator recovery path,
- adding new Terraform or runtime UX around nftables fallback,
- using nftables to pass AWS, GCP, or future-cloud datapath readiness, HA,
  smoke, soak, or release validation when LoxiLB is not ready.

## Superseded History

Older research documents may describe nftables-first or nftables-fallback
plans. Those notes are design history only. The current source of truth is:

- `docs/architecture.md`
- `docs/spec-v0.md`
- this decision record

AWS, GCP, and future-cloud preparation must validate LoxiLB directly. A
route-only or HA smoke that does not prove LoxiLB datapath readiness is useful
substrate evidence, not a complete datapath release gate.
