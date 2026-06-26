# AGENTS

This repository is BetterNAT: a self-owned, observable, highly available egress gateway for high-volume private subnet workloads.

## Product Direction

- Product name: `BetterNAT`
- User-facing promise: lower NAT Gateway processing cost, useful per-workload observability, and reliable low-cost failover.
- Primary target users: teams running high-volume private subnet workloads such as crawler fleets, EKS nodes pulling large artifacts, and blockchain/RPC nodes pulling large amounts of public network data.
- Current cloud target: AWS first.
- Current v0 install UX: `terraform-provider-betternat`.
- Runtime control plane: `betternat-agent`.

## Current Architecture

- Language: Go.
- Primary datapath: LoxiLB standalone egress SNAT.
- Fallback datapath: none. This is a global BetterNAT product decision, not a
  GCP-specific exception. Existing Linux nftables/nf_conntrack code may remain
  temporarily as legacy diagnostics while it is phased out.
- Runtime HA: `betternat-agent` with lease/fencing, EIP association, route replacement, and verification.
- Terraform provider: creates/records gateway infrastructure and renders agent config/user data.
- AWS operations: use AWS SDKs, not shelling out to `aws` or Terraform from the agent.

## Product And Engineering Principles

- Build for users, not for a clever datapath demo.
- Prefer mature existing components over custom implementations when the component fits the product boundary.
- Prefer current supported dependency versions unless there is a concrete compatibility, stability, or security reason to pin older versions.
- Keep LoxiLB as the primary datapath investment unless new evidence invalidates the spike results.
- Do not treat nftables/nf_conntrack as a product fallback, release fallback,
  cloud fallback, or acceptance substitute. Existing code can remain
  temporarily for legacy diagnostics and gradual cleanup.
- Do not re-propose nftables fallback work unless the product decision is
  explicitly reopened in a new architecture decision record.
- Do not expose unnecessary implementation detail in top-level product copy. Advanced technical detail belongs in architecture, operator, and benchmark docs.
- Optimize for a Terraform-first product experience that feels close to configuring a managed NAT gateway.
- Keep AWS first, but avoid hard-coding AWS concepts into core interfaces when a provider-neutral shape is practical.

## Codex Working Rules

- Default language for chat responses: Chinese.
- Code, comments, durable docs, examples, and product copy should stay in English unless an existing file is already Chinese.
- Read `CODEX.md`, `README.md`, and `docs/README.md` before broad changes.
- Keep changes scoped. Do not refactor unrelated areas while fixing one issue.
- Use `rg` and focused file reads before editing.
- Use `apply_patch` for manual file edits.
- Validate behavior after meaningful changes instead of stopping at static inspection.
- Prefer portable direct commands and scripts for core workflows.
- Use `./manage` as a convenience wrapper for common local checks, not as the only supported entrypoint.
- Do not bake a developer-specific environment such as OrbStack, Lima, Multipass, Docker Desktop, or a personal AWS profile into core harness behavior.
- If a workflow is useful more than once, either document the portable command or add a small environment-agnostic script. Add `./manage` wrappers only when they remain clearly optional.

## Operations Harness

- Standard Go, Terraform, shell, and Linux commands are first-class entrypoints.
- `./manage` is a repo-local convenience wrapper for common checks.
- Use `./manage help` to discover convenience commands, but document direct portable commands in durable docs.
- Use `GOCACHE=$PWD/tmp/go-build go test ./...` for the default local validation loop.
- Use `./manage verify` or the equivalent direct command sequence before considering a broad implementation change complete.
- Use `./manage deps check` or `go list -m -u all` for dependency freshness checks when network access is available.
- Commands that create cloud resources must be explicit and must document cleanup behavior before they are added.

## Files And Ownership

- `cmd/betternat`: operator CLI.
- `cmd/betternat-agent`: appliance runtime agent.
- `cmd/terraform-provider-betternat`: Terraform provider entrypoint.
- `internal/agent`: runtime reconcile loop and metrics serving.
- `internal/datapath/loxilb`: LoxiLB wrapper and parsers.
- `internal/datapath/nftables`: legacy nftables diagnostic wrapper and parsers
  retained temporarily; do not add or depend on fallback behavior.
- `internal/ha`: failover orchestration.
- `internal/install/aws`: AWS install applier used by the Terraform provider.
- `internal/tfprovider`: Terraform provider resource model.
- `examples/`: agent and Terraform examples.
- `scripts/`: spike and operational helper scripts.
- `docs/`: durable docs, research, workflow, and dev logs.
- `tmp/`: local scratch output and build cache; do not treat it as product source.

## Validation Order

Use the lightest useful validation first, then escalate when the touched area requires it:

1. `GOCACHE=$PWD/tmp/go-build go test ./...`
2. `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`
3. Targeted CLI smoke checks, for example `./manage smoke doctor`.
4. Linux-only datapath validation on any suitable Linux host or EC2 appliance.
5. AWS integration/spike validation only when the task requires cloud behavior and cleanup is explicit.

## Documentation Rules

- All durable project docs live under `docs/`.
- Update `docs/README.md` when adding a durable new document.
- Use `docs/research/` for evidence-gathering, spikes, and decision records.
- Use `docs/dev-logs/` for session summaries, implementation notes, and architecture pivots.
- Use `docs/deployment/` for workflow, dependency, release, local setup, and operator-runbook docs.
- If architecture, user-visible behavior, install UX, validation workflow, or dependency policy changes, update docs in the same change.
- Treat older research as design history when it conflicts with `docs/architecture.md` or `docs/spec-v0.md`.

## Release Notes Rules

- Every release must have release notes before publication, regardless of
  whether it is alpha, beta, RC, GA, patch, test, runtime, provider, AMI, or any
  other tagged release.
- GitHub Release pages must not be left as empty shells, auto-generated
  changelog-only pages, or generic checksum-only messages.
- Release notes must state what changed, who should use the release, validation
  evidence, known limitations, upgrade or compatibility notes, and artifact
  integrity guidance when artifacts are published.
- BetterNAT runtime/main-repo release notes live under
  `docs/user/releases/<minor>/` using the full `<tag>.md` filename, for example
  `docs/user/releases/v0.1/v0.1.0-alpha.8.md`.
- Split Terraform provider release notes live in the provider repository under
  `docs/release-notes/<tag>.md`.
- Release workflows should prefer an explicit checked-in release note file over
  generated notes.

## Dependency Rules

- Prefer current supported versions.
- Prefer mature libraries and cloud SDKs over custom protocol clients.
- Do not add a dependency just to avoid a small clear standard-library implementation.
- Before adding a dependency, check license, maintenance activity, transitive footprint, and whether it improves product reliability or UX.
- When network is available, use `./manage deps check` before dependency upgrade work and document any intentional pins.

## Version Compatibility Rules

- Follow SemVer for public BetterNAT runtime and Terraform provider releases once a release line is declared supportable.
- Patch releases must not introduce breaking user-facing behavior, Terraform schema incompatibility, runtime config incompatibility, or required replacement beyond documented bug/security fixes.
- Minor releases may add backward-compatible fields, supported runtime versions, metrics, CLI commands, or safe provider-owned infrastructure migrations.
- Major releases are the only normal place for intentional breaking changes.
- Pre-1.0 alpha releases may still change behavior, but every breaking change must be called out in release notes and upgrade docs.
- Once a runtime/provider line is declared production-supportable, the Terraform
  provider must maintain a runtime version support matrix for
  `betternat_version`. Do not remove support for a previously documented
  runtime version from a patch provider release.
- During the alpha line, avoid version churn just to update a formal support
  matrix. Alpha releases should document the recommended runtime/provider pair,
  any explicitly validated artifact override path, and breaking changes in the
  release notes. Promote the formal support matrix before GA.
- When adding a runtime release to the provider built-in artifact manifest,
  update validation evidence in the same change.

## Current Known State

- BetterNAT v0 is LoxiLB-first with no product fallback datapath. Existing
  nftables code is legacy-only while retained.
- The Terraform provider computes config and calls the AWS install applier during apply.
- macOS local testing uses fakes for datapath, AWS, leases, metrics, and provider behavior.
- Real LoxiLB datapath behavior requires Linux. nftables code is legacy-only
  while retained.
- Real route replacement, EIP reassociation, and EC2 appliance installation require AWS integration tests.
