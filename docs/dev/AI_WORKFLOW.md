# AI Workflow

Last updated: 2026-06-20

## Purpose

This document defines the recommended workflow for AI-assisted work in BetterNAT. It keeps agent instructions, validation habits, dependency choices, and documentation updates consistent across sessions.

## Source Of Truth

Use these files in order:

1. `AGENTS.md` for repo constraints, current architecture, and working rules.
2. `CODEX.md` for concise session bootstrap notes.
3. `README.md` for project overview and local commands.
4. `docs/README.md` for durable documentation index.
5. `docs/architecture.md` and `docs/spec-v0.md` for current product architecture.

Research docs are important evidence, but older research does not override the current architecture/spec pair.
Any older research that recommends nftables-first or nftables-fallback behavior
is design history only. The standing rule is global: no product fallback
datapath, and no nftables fallback work unless a new ADR explicitly supersedes
`docs/research/055-no-nftables-fallback-decision.md`.

## Default Working Pattern

1. Read the relevant bootstrap files.
2. Inspect the current workspace state.
3. Use `rg` and focused file reads to understand the target area.
4. Make the smallest coherent product-quality change.
5. Use mature existing components where they fit the boundary.
6. Run the lightest validation that credibly covers the change.
7. Update durable docs when behavior, workflow, architecture, or dependency policy changed.

## Product Bias

BetterNAT should be built as a product for operators, not as a networking experiment.

Prefer:

- Terraform-first install UX,
- clear `doctor` and status output,
- reliable rollback metadata,
- explicit cloud cleanup for spikes,
- normalized BetterNAT metrics over raw implementation detail,
- LoxiLB as the supported datapath, without adding or expanding nftables
  fallback behavior.
- direct LoxiLB validation for AWS, GCP, and future-cloud work; nftables is not
  a fallback, acceptance substitute, or topic to re-propose without a new ADR.

Avoid:

- broad custom eBPF work before product proof demands it,
- user-facing copy that over-explains internal technology,
- ad-hoc cloud scripts that bypass the product install model,
- hidden assumptions that only work in the developer's AWS account.

## Validation Ladder

Use the lightest useful validation first:

1. `GOCACHE=$PWD/tmp/go-build go test ./...`
2. `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`
3. `GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat doctor --config examples/agent-config.yaml`
4. `GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat failover status --config examples/agent-config.yaml`
5. Optional shortcut: `./manage verify`
6. Linux datapath validation for real LoxiLB behavior.
7. Isolated AWS integration validation for route/EIP/EC2 behavior.

`./manage` is a convenience wrapper around common checks. It should not be the only documented path for a workflow.

Common direct commands:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat doctor --config examples/agent-config.yaml
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat failover status --config examples/agent-config.yaml
```

## Local Environment Expectations

- Keep Go build artifacts under `tmp/go-build`.
- Do not require network access for the default test loop.
- macOS validation should rely on unit tests, fakes, provider builds, and static CLI smoke checks.
- Real datapath validation requires Linux with LoxiLB and conntrack available.
  nftables is only relevant for legacy tests when that code is touched.
- Linux validation must be environment-agnostic. It may run on OrbStack, Lima, Multipass, Docker-in-VM, a bare Linux host, or EC2, but core scripts and docs must not require one developer-specific VM product.
- Real AWS validation must use isolated test resources and explicit cleanup.

## Documentation Update Rules

Update docs when any of these changes:

- architecture,
- user-visible behavior,
- Terraform/provider UX,
- agent config schema,
- validation workflow,
- local setup assumptions,
- dependency policy,
- runtime caveats that materially affect development or operators.

Preferred destinations:

- architecture and v0 behavior: `docs/architecture.md`, `docs/spec-v0.md`, `docs/architecture-diagram.md`
- evidence and spikes: `docs/research/`
- user install and operations docs: `docs/user/`
- release gates, packaging, and publication docs: `docs/release/`
- repeatable test plans and runbooks: `docs/testing/`
- contributor workflow, dependency policy, and local validation: `docs/dev/`
- Linux datapath validation: `docs/dev/LINUX_DATAPATH_VALIDATION.md`
- implementation notes and pivots: `docs/dev-logs/`

When adding a durable doc, update `docs/README.md`.

## Session Logging

Add a dev-log entry when:

- an architecture decision changes,
- a cloud/datapath spike produces durable evidence,
- a non-obvious workaround is introduced,
- dependency policy or install workflow changes,
- an operational incident or cleanup teaches a reusable lesson.

Use `docs/dev-logs/YYYY-MM/` for monthly notes, or a root-level dated file for one-off summaries.
