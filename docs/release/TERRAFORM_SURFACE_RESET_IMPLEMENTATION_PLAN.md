# Terraform Surface Reset Implementation Plan

Date: 2026-06-25

## Purpose

Track the implementation of the next BetterNAT Terraform surface reset.

The next version is allowed to break the unpromoted `v0.1.1` provider schema in
order to establish a clean multi-cloud structure before broader public
promotion.

Design source:

- [Provider And Module Boundary Plan](../research/048-provider-module-boundary-plan.md)
- GCP support research scratch:
  `tmp/gcp-betternat-support-research-20260625.md`

## Target End State

Provider source address stays generic:

```hcl
source = "nowakeai/betternat"
```

Provider resources become cloud-specific:

```hcl
resource "betternat_aws_gateway" "this" {}
resource "betternat_gcp_gateway" "this" {}
```

Provider data sources:

```hcl
data "betternat_runtime_artifacts" "this" {}
data "betternat_aws_gateway_status" "this" {}
data "betternat_gcp_gateway_status" "this" {}
```

User-facing modules become cloud-specific:

```hcl
module "betternat" {
  source = "nowakeai/betternat/aws"
}

module "betternat" {
  source = "nowakeai/betternat/google"
}
```

The old `betternat_gateway` resource should be removed from the primary
provider surface. Keep an undocumented alias only if it is nearly free and does
not complicate implementation, tests, or docs.

## Release Framing

This is a Terraform surface reset before public promotion.

Release notes must state:

- `v0.1.1` existed but was not promoted and has no known external users.
- The provider schema intentionally changed.
- Use cloud-specific resources or modules going forward.
- Normal SemVer compatibility discipline resumes after this reset.

## Phase 0: Baseline And Branch Hygiene

Status: `complete`

Tasks:

- [x] Commit or explicitly carry current docs-only planning changes.
- [x] Confirm main repo worktree state.
- [x] Confirm split provider repo worktree state.
- [x] Create an implementation branch.
- [x] Record current provider `v0.1.1` release URL and commit.
- [x] Record current runtime release version and commit.

Validation:

```sh
git status --short
GOCACHE=$PWD/tmp/go-build go test ./...
```

Done when:

- Work starts from known commits.
- Existing tests pass before refactor.

## Phase 1: Provider Resource Reset

Status: `complete`

Goal: replace the AWS provider surface with explicit cloud naming.

Tasks:

- [x] Rename provider resource implementation from `GatewayResource` to an AWS
  specific shape internally where practical.
- [x] Expose `betternat_aws_gateway`.
- [x] Remove or hide `betternat_gateway`.
- [x] Update provider tests from `betternat_gateway` to
  `betternat_aws_gateway`.
- [x] Update Terraform examples to use `betternat_aws_gateway`.
- [x] Update provider Registry docs to document `betternat_aws_gateway`.
- [x] Remove stale AWS-first language from provider overview.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./internal/tfprovider ./internal/install/aws
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat
```

Done when:

- Terraform schema exposes `betternat_aws_gateway`.
- No product docs recommend `betternat_gateway`.
- Provider examples validate with local override.

## Phase 2: Provider Data Sources

Status: `complete`

Goal: give modules a clean read-only interface instead of copying provider
constants or runtime manifests.

Tasks:

- [x] Add `betternat_runtime_artifacts`.
- [x] Add tests for supported runtime lookup.
- [x] Add tests for unsupported runtime/version/arch errors.
- [x] Add `betternat_aws_gateway_status`.
- [x] Add tests for AWS gateway status read path using fakes.
- [x] Stub or defer `betternat_gcp_gateway_status` until GCP provider alpha.
- [x] Add `docs/data-sources/runtime_artifacts.md`.
- [x] Add `docs/data-sources/aws_gateway_status.md`.
- [x] Update provider examples to show runtime artifact inspection.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./internal/tfprovider
GOCACHE=$PWD/tmp/go-build go test ./...
```

Done when:

- Modules can consume runtime artifact metadata from a data source.
- AWS gateway status can be read without managing a resource.

## Phase 3: AWS Module Repository

Status: `local implementation complete; cloud smoke pending before release`

Goal: make the AWS module the default user-facing install surface.

Repository:

```text
terraform-aws-betternat
```

Registry source:

```text
nowakeai/betternat/aws
```

Tasks:

- [x] Create module repository.
- [x] Add `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`.
- [x] Wrap `betternat_aws_gateway`.
- [x] Add AMI lookup for the default non-AMI bootstrap path.
- [x] Accept common `terraform-aws-modules/vpc/aws` output shapes.
- [ ] Add examples:
  - [x] `examples/minimal-existing-vpc`
  - [x] `examples/eks-vpc-module`
  - [x] `examples/stable-egress-ip`
  - [x] `examples/non-stable-egress-ip`
  - [x] `examples/full-vpc-smoke`
- [x] Add module README with quick install path.
- [x] Add input/output descriptions suitable for Terraform Registry.
- [x] Add CI validation.
- [x] Add release notes.

Recommended first-screen UX:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/aws"
  version = "~> 0.2"

  name   = "prod-egress"
  vpc_id = module.vpc.vpc_id

  azs                     = module.vpc.azs
  public_subnet_ids       = module.vpc.public_subnets
  private_route_table_ids = module.vpc.private_route_table_ids

  private_cidrs = [module.vpc.vpc_cidr_block]
}
```

Validation:

```sh
terraform fmt -check -recursive
terraform init
terraform validate
```

AWS validation:

- [ ] Disposable VPC apply.
- [ ] Private client egress.
- [ ] `betternat status`.
- [ ] Manual handover.
- [ ] Single destroy.
- [ ] Residual scan.

Done when:

- AWS module can replace the provider resource in the main Quick Start.
- AWS module Registry page has meaningful README, inputs, outputs, resources,
  and examples.

## Phase 4: GCP Spike

Status: `spike plan complete; execution pending`

Goal: validate whether GCP can support the BetterNAT product model before
committing to a production resource.

Scope:

- Disposable GCP VPC.
- Private client VM without public IP.
- One or two gateway VMs with `canIpForward=true`.
- Static route from private subnet egress to the active gateway VM.
- LoxiLB first, nftables fallback if needed.
- Firestore transaction or GCS generation-precondition lease backend.
- Optional reserved static external IP test.

Tasks:

- [x] Create durable GCP spike plan from the scratch research memo.
- [x] Select functional target project.
- [x] Define cleanup checklist.
- [ ] Validate gateway VM forwarding.
- [ ] Validate private client internet egress.
- [ ] Validate LoxiLB counters.
- [ ] Validate route replacement to standby.
- [ ] Measure new-flow recovery time.
- [ ] Validate or reject reserved external IP handover.
- [ ] Validate coordination backend choice.
- [ ] Destroy all resources and scan residuals.

Validation evidence:

- [ ] Route mutation timing.
- [ ] Handover timing.
- [ ] Public IP behavior.
- [ ] Datapath counters.
- [ ] Cleanup evidence.

Done when:

- GCP alpha scope is either accepted with measured limits or deferred with a
  concrete blocker.

## Phase 5: GCP Provider Alpha

Status: `pending`

Goal: expose a GCP alpha resource only after the spike proves the minimum
control-plane behavior.

Tasks:

- [ ] Add `internal/install/gcp`.
- [ ] Add GCP cloud/runtime interfaces.
- [ ] Add GCP lease/coordination backend.
- [ ] Add `betternat_gcp_gateway`.
- [ ] Add `betternat_gcp_gateway_status`.
- [ ] Add provider docs for GCP alpha.
- [ ] Add GCP IAM docs.
- [ ] Add GCP cleanup tests with fakes.
- [ ] Add disposable GCP integration runbook.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

GCP validation:

- [ ] Disposable GCP apply.
- [ ] Private client egress.
- [ ] Route replacement.
- [ ] Cleanup.

Done when:

- GCP alpha works in a disposable environment.
- Docs clearly state alpha limitations and unsupported production guarantees.

## Phase 6: GCP Module Repository

Status: `pending`

Repository:

```text
terraform-google-betternat
```

Registry source:

```text
nowakeai/betternat/google
```

Tasks:

- [ ] Create module repository.
- [ ] Wrap `betternat_gcp_gateway`.
- [ ] Add GKE/VPC-friendly inputs.
- [ ] Add examples:
  - [ ] `examples/minimal-existing-vpc`
  - [ ] `examples/gke-private-nodes`
  - [ ] `examples/non-stable-egress-ip`
  - [ ] `examples/stable-egress-ip` only if validated.
- [ ] Add README, input docs, output docs.
- [ ] Add CI validation.
- [ ] Publish alpha module.

Done when:

- GCP module is the default documented GCP alpha install path.

## Phase 7: Main Documentation Reset

Status: `pending`

Tasks:

- [ ] Update root `README.md` to point users to the AWS module by default.
- [ ] Update `docs/user/getting-started/QUICK_START.md`.
- [ ] Update `docs/user/getting-started/EXISTING_VPC_INSTALL.md`.
- [ ] Update `docs/user/getting-started/EKS_TERRAFORM_MODULE_INTEGRATION.md`.
- [ ] Add GCP alpha docs only after GCP spike/provider alpha validates.
- [ ] Update `docs/user/reference/CONFIGURATION.md` or equivalent provider
  reference.
- [ ] Update `docs/user/reference/IAM_POLICY.md` for AWS provider/module split.
- [ ] Add GCP IAM docs if GCP alpha ships.
- [ ] Update release checklist.

Done when:

- A new user sees module-first install docs.
- Provider docs are clearly advanced/reference docs.
- No user-facing docs recommend the old `betternat_gateway` resource.

## Phase 8: Release And Registry Validation

Status: `pending`

Tasks:

- [ ] Release main BetterNAT runtime if runtime changes are included.
- [ ] Release provider reset version.
- [ ] Release AWS module.
- [ ] Release GCP module only if GCP alpha is included.
- [ ] Verify Terraform Registry provider install.
- [ ] Verify OpenTofu Registry provider install.
- [ ] Verify AWS module Registry page.
- [ ] Verify GCP module Registry page if published.
- [ ] Run AWS smoke through module.
- [ ] Run GCP smoke through module if published.
- [ ] Record release notes for every released artifact.

Done when:

- Registry pages show the intended module-first UX.
- AWS smoke passes through the AWS module.
- Cleanup passes without manual retry.

## Open Decisions

- [ ] Whether to remove `betternat_gateway` completely or keep an undocumented
  alias for one release.
- [ ] Whether `betternat_runtime_artifacts` should support only current runtime
  or a bounded set of versions.
- [ ] Whether AWS module default should use AL2023, Ubuntu, or user-supplied
  AMI until BetterNAT AMIs exist.
- [ ] Whether GCP stable public identity is in the first alpha.
- [ ] Whether GCP lease backend is Firestore or GCS generation preconditions.
- [ ] Provider version number for the surface reset.
- [ ] Module versioning policy while provider and modules are released from
  separate repositories.

## Tracking Notes

Append dated notes here during implementation.

### 2026-06-25

- Created implementation tracker.
- Current plan intentionally allows breaking the unpromoted `v0.1.1` provider
  schema to establish cloud-specific provider resources and module-first UX.
- Baseline provider release: `v0.1.1`,
  `https://github.com/nowakeai/terraform-provider-betternat/releases/tag/v0.1.1`,
  tag commit `1317a2fbd9312a3451ec0a3376d667a7a0f8a93f`, split-provider HEAD
  before reset `df9f1e4140681e6caebe258a420498f5ea3a5971`.
- Baseline runtime release: `v0.1.0`,
  `https://github.com/nowakeai/betternat/releases/tag/v0.1.0`, tag commit
  `8500643a05f88aefb31b68bce617bf7c8c0ca602`, main repo HEAD before reset
  `152512d70a635011412dfbf3d0287c31bdcd2ecd`.
- Implemented main-repo provider surface reset on branch
  `terraform-surface-reset`: `betternat_aws_gateway`,
  `betternat_runtime_artifacts`, `betternat_aws_gateway_status`, and reserved
  `betternat_gcp_gateway_status`.
- Removed `betternat_gateway` from the registered provider resource surface.
  Kept internal model names where changing them would add churn without
  improving the Terraform surface.
- Split `internal/tfprovider/gateway_resource_schema.go` out of the gateway
  resource implementation so the touched provider resource file stays under
  800 lines.
- Updated active examples and user docs to use `betternat_aws_gateway`; old
  release notes and historical research remain unchanged as version history.
- Updated split provider docs/examples on branch `terraform-surface-reset`:
  resource docs now use `docs/resources/aws_gateway.md`, data source docs live
  under `docs/data-sources/`, and release notes include `v0.2.0`.
- Created local AWS module repository `terraform-aws-betternat` with
  cloud-specific module UX, examples, CI, and release notes. Registry source is
  intended to be `nowakeai/betternat/aws` after repository creation/push.
- Added GCP durable planning docs:
  `docs/testing/GCP_SPIKE_PLAN.md` and
  `docs/research/049-gcp-alpha-boundary.md`. GCP implementation remains gated
  on disposable spike evidence.
- Local validation passed:
  `GOCACHE=$PWD/tmp/go-build go test ./...`,
  `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`,
  `terraform fmt -check -recursive` for main examples,
  Terraform dev-override `validate` for `examples/terraform` and
  `examples/terraform-localstack`.
- AWS module validation passed with a local provider `0.2.0` filesystem mirror:
  `terraform fmt -check -recursive`, root `terraform init -backend=false` and
  `terraform validate`, plus validate for all five examples.
- Cloud mutation tests were not run during implementation. AWS profile
  `601427795217_AdministratorAccess` is usable, and GCP must be invoked with
  explicit `--project shared-resources-alt`; perform disposable AWS/GCP smoke
  only after PR review and before any release.
