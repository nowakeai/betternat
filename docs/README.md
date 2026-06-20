# BetterNAT Documentation Index

This index is ordered by planning priority, not just by folder name.

- `Current Priority` documents describe the product and implementation work that should drive v0.
- `Research / Decision History` documents preserve evidence and pivots.
- Older research may describe nftables-first or custom-eBPF-first thinking. Treat those as history when they conflict with the current architecture/spec pair.

## Current Priority

### Architecture And Spec

- [architecture.md](architecture.md) — Current BetterNAT architecture baseline: LoxiLB-first datapath, nftables fallback, Terraform install UX, and agent-owned HA.
- [architecture-diagram.md](architecture-diagram.md) — Mermaid diagrams for AWS route replacement, agent/LoxiLB interaction, runtime reconciliation, and failover.
- [spec-v0.md](spec-v0.md) — v0 product and implementation spec.

### Workflow And Harness

- [deployment/AI_WORKFLOW.md](deployment/AI_WORKFLOW.md) — AI-assisted workflow, validation ladder, documentation update rules, and product bias.
- [deployment/DEPENDENCY_POLICY.md](deployment/DEPENDENCY_POLICY.md) — Dependency freshness, mature-component preference, and upgrade policy.

### Development Logs

- [dev-logs/README.md](dev-logs/README.md) — How to record durable implementation notes, session summaries, and architecture pivots.
- [dev-logs/2026-06-20-harness-and-dependency-refresh.md](dev-logs/2026-06-20-harness-and-dependency-refresh.md) — Repo harness setup and dependency refresh notes.

## Key Research Results

Read these first when revisiting product or architecture direction:

- [research/021-loxilb-spike-results.md](research/021-loxilb-spike-results.md) — Initial AWS LoxiLB egress NAT spike.
- [research/022-loxilb-extended-spike-results.md](research/022-loxilb-extended-spike-results.md) — Extended LoxiLB validation: DNS/UDP, downloads, concurrent flows, failover, and persistence caveats.
- [research/017-loxilb-evaluation.md](research/017-loxilb-evaluation.md) — LoxiLB as the primary BetterNAT datapath candidate.
- [research/016-mvp-scope-milestones.md](research/016-mvp-scope-milestones.md) — MVP scope and milestone planning.
- [research/019-target-workloads.md](research/019-target-workloads.md) — Target workloads and cost pain: crawlers, EKS image pulls, blockchain/RPC nodes, and high-response-volume downloads.

## Supporting Research

- [research/014-observability-mvp.md](research/014-observability-mvp.md) — Observability MVP and normalized metrics.
- [research/013-security-model.md](research/013-security-model.md) — Security model and IAM/runtime boundaries.
- [research/012-agent-architecture.md](research/012-agent-architecture.md) — Agent responsibilities and runtime architecture.
- [research/011-benchmark-slo.md](research/011-benchmark-slo.md) — Benchmark and SLO planning.
- [research/010-install-terraform-ux.md](research/010-install-terraform-ux.md) — Terraform-first install UX.
- [research/009-observability-eks-pod-attribution.md](research/009-observability-eks-pod-attribution.md) — Pod attribution considerations for EKS.
- [research/008-ha-replaceroute-lease-multicloud.md](research/008-ha-replaceroute-lease-multicloud.md) — Route replacement, lease/fencing, and multi-cloud notes.
- [research/007-ha-primitive-selection.md](research/007-ha-primitive-selection.md) — HA primitive selection.
- [research/006-datapath-comparison-nftables-ebpf-vpp.md](research/006-datapath-comparison-nftables-ebpf-vpp.md) — nftables/eBPF/VPP comparison.
- [research/005-cost-model-review.md](research/005-cost-model-review.md) — Cost model review.
- [research/004-product-pillars-feasibility.md](research/004-product-pillars-feasibility.md) — Product pillars: cost, observability, HA, install UX.
- [research/003-nftables-performance.md](research/003-nftables-performance.md) — nftables performance notes.
- [research/002-existing-building-blocks.md](research/002-existing-building-blocks.md) — Existing components and reuse options.
- [research/001-cilium-hubble-ebpf-feasibility.md](research/001-cilium-hubble-ebpf-feasibility.md) — Cilium/Hubble/custom eBPF feasibility.

## Spike Plans And Evidence

- [research/020-loxilb-spike-plan.md](research/020-loxilb-spike-plan.md) — AWS LoxiLB spike plan.

## Documentation Rules

- Add durable docs under `docs/`.
- Update this index when adding a durable new document.
- Use `docs/research/` for evidence and decision records.
- Use `docs/deployment/` for workflow, dependency, release, local setup, and operator-runbook docs.
- Use `docs/dev-logs/` for implementation notes and architecture pivots.
