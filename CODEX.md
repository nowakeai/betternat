# CODEX

Repository bootstrap notes for Codex sessions.

## Start Here

1. Read `AGENTS.md`.
2. Read `README.md`.
3. Read `docs/README.md`.
4. Check current worktree state if Git metadata is available.
5. Use `rg` to inspect relevant code paths before editing.
6. Prefer direct portable commands for canonical workflows; use `./manage help` for repo-local shortcuts.

## Project Snapshot

- Product: `BetterNAT`
- Purpose: lower-cost, observable, highly available egress for high-volume private subnet workloads.
- Language: Go.
- Primary datapath: LoxiLB standalone egress SNAT.
- Fallback datapath: nftables/nf_conntrack.
- Cloud target: AWS first.
- Runtime control plane: `betternat-agent`.
- Install UX: `terraform-provider-betternat`.

## Important Paths

- `cmd/betternat/`: operator CLI.
- `cmd/betternat-agent/`: runtime appliance agent.
- `cmd/terraform-provider-betternat/`: Terraform provider binary.
- `internal/agent/`: reconcile loop and metrics serving.
- `internal/datapath/loxilb/`: LoxiLB command wrapper and JSON parsing.
- `internal/datapath/nftables/`: fallback datapath wrapper and conntrack parsing.
- `internal/ha/`: failover controller.
- `internal/install/aws/`: AWS install applier.
- `internal/tfprovider/`: Terraform provider resource and install wiring.
- `docs/architecture.md`: current architecture baseline.
- `docs/spec-v0.md`: v0 product and implementation spec.
- `docs/research/`: research and spike history.
- `docs/deployment/`: harness, workflow, dependency, and operations docs.

## Local Commands

Direct portable commands are the canonical form:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat doctor --config examples/agent-config.yaml
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat failover status --config examples/agent-config.yaml
```

`./manage` provides optional convenience wrappers:

```sh
./manage help
./manage test
./manage verify
./manage smoke doctor
./manage smoke failover
./manage build provider
./manage deps check
./manage deps upgrade
```

## Environment Assumptions

- macOS can run unit tests, provider builds, and static CLI smoke checks.
- Linux is required for real LoxiLB/nftables datapath execution. Any suitable Linux host is acceptable; OrbStack is only one local option.
- AWS integration tests must use isolated disposable resources and explicit cleanup.
- Network-dependent dependency checks may require the local proxy configured outside the sandbox.

## Change Discipline

- Keep docs in `docs/`.
- Update `docs/README.md` when adding durable docs.
- Prefer mature libraries and official SDKs over custom protocol implementations.
- Prefer latest supported dependency versions unless an intentional pin is documented.
- Keep Terraform/provider UX user-oriented; do not leak unnecessary implementation detail into top-level product copy.
- Treat AWS mutation paths as high risk; cover them with fakes locally and cloud spikes only when needed.

## Suggested Validation

- Docs-only change: inspect links and run `./manage docs check` when applicable.
- Go logic change: `GOCACHE=$PWD/tmp/go-build go test ./...` or `./manage test`.
- Provider/install change: direct test/build/smoke sequence or `./manage verify`.
- CLI behavior change: direct `go run ./cmd/betternat ...` smoke plus the relevant targeted smoke.
- Datapath change: unit tests locally, then Linux validation.
- AWS route/EIP/install change: local fakes first, then isolated AWS spike with cleanup.
