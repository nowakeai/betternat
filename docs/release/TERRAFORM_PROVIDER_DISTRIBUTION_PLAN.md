# Terraform Provider Distribution Plan

Date: 2026-06-21

## Decision

Use option C:

- `github.com/nowakeai/betternat` remains the main BetterNAT product repository.
- `github.com/nowakeai/terraform-provider-betternat` becomes the Terraform/OpenTofu provider repository before Registry publication.

The first alpha may keep publishing a provider binary in the main BetterNAT GitHub Release for testing, but that is not the final provider distribution model.

## Why Split The Repo

Terraform Registry provider publishing expects a public provider repository named:

```text
terraform-provider-<provider-name>
```

For BetterNAT, that means:

```text
github.com/nowakeai/terraform-provider-betternat
```

Terraform users would then write:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "~> 0.1"
    }
  }
}
```

The Terraform provider source address is `<namespace>/<provider-name>`, not the GitHub repository name. The GitHub repository includes the `terraform-provider-` prefix; the provider name does not.

## Version Model

There are two independent version axes.

### Provider Version

Specified by Terraform/OpenTofu:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "= 0.1.0-alpha.2"
    }
  }
}
```

This controls:

- `betternat_gateway` schema,
- provider create/read/update/delete behavior,
- state migration behavior,
- Terraform/OpenTofu plugin protocol implementation.

### BetterNAT Runtime Version

Specified by the gateway resource install path.

Current alpha shape:

```hcl
resource "betternat_gateway" "egress" {
  # ...

  agent_binary_url    = "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.1/betternat-agent_v0.1.0-alpha.1_linux_arm64"
  agent_binary_sha256 = "..."

  cli_binary_url      = "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.1/betternat_v0.1.0-alpha.1_linux_arm64"
  cli_binary_sha256   = "..."
}
```

Recommended P1 UX:

```hcl
resource "betternat_gateway" "egress" {
  # ...

  betternat_version = "v0.1.0-alpha.1"
}
```

The provider should derive GitHub Release URLs and checksums from that version, or from a version manifest. Users should still be able to override explicit URLs/checksums for air-gapped, mirrored, or development builds.

Production AMI path:

```hcl
resource "betternat_gateway" "egress" {
  # ...

  ami_channel       = "stable"
  betternat_version = "v0.2.0"
}
```

or explicit pin:

```hcl
ami_id = "ami-..."
```

## Current Technical Blocker

The current provider implementation lives in the main repo and imports main-repo `internal` packages:

```text
internal/tfprovider -> internal/bootstrap
internal/tfprovider -> internal/install/aws
internal/tfprovider -> internal/installplan
internal/tfprovider -> internal/provider
```

A separate `terraform-provider-betternat` repository cannot import these packages because Go's `internal` package boundary only permits imports from within the parent tree.

Therefore, splitting the repository requires one of these approaches.

### Preferred: Public Provider SDK Package

Move provider-consumed product APIs into importable packages:

```text
pkg/bootstrap
pkg/installplan
pkg/install/aws
pkg/gatewayconfig
pkg/rollback
```

Then `terraform-provider-betternat` can import:

```go
github.com/nowakeai/betternat/pkg/installplan
github.com/nowakeai/betternat/pkg/install/aws
```

Pros:

- single source of truth for cloud install logic,
- provider repo stays thin,
- product CLI and provider can share tested code,
- easier to keep AWS behavior consistent.

Cons:

- exposes a public Go API that now needs compatibility discipline,
- requires package rename/move churn.

### Alternative: Provider Repo Owns The Code

Copy the install/backend code into `terraform-provider-betternat`.

Pros:

- provider repo is self-contained,
- Registry release is straightforward.

Cons:

- product and provider behavior can drift,
- duplicated AWS install logic,
- tests and fixes need to be ported twice.

### Not Recommended: Git Subtree Without API Cleanup

Mirroring current files into a provider repo without addressing `internal` imports only moves the problem around. It will either fail to build or require fragile path rewrites.

## Migration Plan

### Phase 0: Alpha Bridge

Status: in progress.

- Keep the provider binary in the main BetterNAT GitHub Release for alpha tests.
- Clearly document that this is not Terraform Registry distribution.
- Use GitHub Release assets for public binaries, not user-provided S3 buckets.
- Keep examples using local provider dev override until Registry publication is ready.

### Phase 1: Provider Repository Preparation

- Create `github.com/nowakeai/terraform-provider-betternat`.
- Make it public before Terraform/OpenTofu registry publication.
- Add:
  - `README.md`,
  - `LICENSE`,
  - `SECURITY.md`,
  - `CONTRIBUTING.md`,
  - provider-specific `.goreleaser.yml`,
  - provider GitHub Actions release workflow,
  - Terraform/OpenTofu examples,
  - acceptance test docs.

Current status:

- Provider repo exists and is public.
- Provider repo has a thin wrapper that imports `github.com/nowakeai/betternat/pkg/tfprovider`.
- Provider repo CI passes.
- Provider repo test release creates Linux amd64/arm64 zip assets and `SHA256SUMS`.
- Terraform local dev override validation passed with Terraform `v1.15.6`.
- OpenTofu local dev override validation passed with OpenTofu `v1.12.3`.

### Phase 2: Extract Importable Product Packages

- Move stable provider-consumed logic out of `internal/` into `pkg/`.
- Keep lower-level runtime-only code in `internal/`.
- Add compatibility notes for the new `pkg/` APIs.
- Update main repo tests after package moves.
- Update provider repo to import `pkg/` APIs from the main BetterNAT module.

### Phase 3: Provider Registry Release

Provider release artifacts should follow Terraform provider conventions, for example:

```text
terraform-provider-betternat_0.1.0-alpha.2_linux_amd64.zip
terraform-provider-betternat_0.1.0-alpha.2_linux_arm64.zip
terraform-provider-betternat_0.1.0-alpha.2_darwin_arm64.zip
terraform-provider-betternat_0.1.0-alpha.2_SHA256SUMS
terraform-provider-betternat_0.1.0-alpha.2_SHA256SUMS.sig
terraform-provider-betternat_0.1.0-alpha.2_manifest.json
```

The first published Terraform Registry provider version is:

```text
0.1.0-alpha.2
```

`0.1.0-alpha.2` was published through `github.com/nowakeai/terraform-provider-betternat` with:

- Linux amd64 provider zip,
- Linux arm64 provider zip,
- Darwin arm64 provider zip,
- `_manifest.json`,
- `_SHA256SUMS` covering the zips and manifest,
- `_SHA256SUMS.sig` signed by key ID `F2D78A307FAB2914`.

Use GoReleaser or an equivalent workflow that can:

- build the provider for supported OS/arch pairs,
- zip provider artifacts in registry format,
- generate checksums,
- sign checksums,
- generate registry manifest,
- upload GitHub Release assets.

### Phase 4: User UX Cleanup

- Update public examples to use Registry provider installation:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "~> 0.1"
    }
  }
}
```

- Add `betternat_version` to `betternat_gateway`.
- Keep explicit URL/checksum overrides for advanced users.
- Add provider upgrade guide and state migration notes.

## OpenTofu Compatibility

OpenTofu compatibility should be treated as a first-class acceptance target, not an assumption.

### What Should Work

BetterNAT uses the Terraform Plugin Framework and normal provider protocol behavior. That should be compatible with both Terraform and OpenTofu as long as:

- the provider is distributed through a registry protocol OpenTofu can resolve,
- the provider binary and checksums are available for the target platform,
- schemas avoid Terraform-CLI-specific assumptions,
- tests run with both CLIs.

### What To Watch

- Registry namespace: Terraform Registry and OpenTofu Registry are distinct distribution surfaces. Publish or document installation for both.
- Provider source address: keep `nowakeai/betternat` consistent where possible.
- Checksums/signing: ensure generated release metadata works for both clients.
- Lock files: do not assume a `.terraform.lock.hcl` generated by one CLI is always the exact desired artifact for the other.
- State compatibility: test `plan`, `apply`, `refresh`, and `destroy` under both clients before claiming support.
- Acceptance tests: run both `terraform` and `tofu` against local provider builds and at least one registry-style install path.
- Documentation: say "Terraform/OpenTofu" only where both are actually tested.

### Required CI Matrix

Before claiming OpenTofu support:

```text
terraform validate examples/terraform
terraform validate examples/terraform-aws-supplemental
tofu validate examples/terraform
tofu validate examples/terraform-aws-supplemental
```

Current validation:

```text
Terraform v1.15.6 local dev override validate: passed
OpenTofu v1.12.3 local dev override validate: passed
Terraform v1.15.6 filesystem mirror install + validate from provider release zip: passed
OpenTofu v1.12.3 filesystem mirror install + validate from provider release zip: passed when source explicitly uses registry.terraform.io/nowakeai/betternat
Terraform v1.15.6 Terraform Registry install + validate for nowakeai/betternat 0.1.0-alpha.2: passed
OpenTofu v1.12.3 Terraform Registry install + validate for registry.terraform.io/nowakeai/betternat 0.1.0-alpha.2: passed
```

Important OpenTofu source-address note:

- Terraform resolves `source = "nowakeai/betternat"` as `registry.terraform.io/nowakeai/betternat`.
- OpenTofu resolves `source = "nowakeai/betternat"` as `registry.opentofu.org/nowakeai/betternat`.
- Current provider binary is served with address `registry.terraform.io/nowakeai/betternat`.
- Until BetterNAT is published through an OpenTofu-native registry path, OpenTofu examples should use the explicit source:

```hcl
source = "registry.terraform.io/nowakeai/betternat"
```

OpenTofu Registry tracking issues:

- Provider entry: https://github.com/opentofu/registry/issues/4494
- Provider key: https://github.com/opentofu/registry/issues/4496

Before production:

```text
terraform apply/destroy disposable AWS fixture
tofu apply/destroy disposable AWS fixture
```

## Near-Term Repo Changes

1. Keep current main-repo release workflow for product binaries.
2. Add a provider-specific release plan and checklist.
3. Add `betternat_version` design to provider docs.
4. Prepare package extraction from `internal` to `pkg`.
5. Create the provider repo when ready to begin Registry-compatible releases.

Do not publish the provider to Terraform/OpenTofu registries until:

- the provider repo is public,
- provider release artifacts are registry-formatted,
- signing is configured,
- Terraform install from Registry is tested,
- OpenTofu install path is tested or explicitly marked unsupported.
