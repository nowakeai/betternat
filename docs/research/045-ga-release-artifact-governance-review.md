# GA Release Artifact Governance Review

Date: 2026-06-24

## Scope

This review covers BetterNAT release artifact governance after main release
`v0.1.0-alpha.6` and split provider release `v0.1.0-alpha.7`.

Reviewed areas:

- checksum workflow,
- reproducible release commands,
- release notes requirement,
- runtime/provider support matrix,
- SemVer compatibility policy,
- patch-release non-breaking rule,
- artifact signing decision.

## Evidence

Release workflow:

- `.github/workflows/release.yml` runs:
  - `go test ./...`,
  - `git diff --check`,
  - `scripts/release-dependency-scan.sh`,
  - `scripts/release-build.sh`,
  - `sha256sum -c SHA256SUMS`,
  - GitHub Release creation/update,
  - release asset upload,
  - release asset presence verification.

Release build script:

- `scripts/release-build.sh` builds:
  - host `betternat`,
  - host legacy provider binary,
  - Linux `arm64` and `amd64` `betternat`,
  - Linux `arm64` and `amd64` `betternat-agent`.
- It injects version, commit, and build date.
- It writes `SHA256SUMS`.
- It writes `manifest.json` with artifact metadata.

Release notes:

- `AGENTS.md` now requires release notes for every release, including alpha,
  beta, RC, GA, patch, test, runtime, provider, and AMI releases.
- GitHub Release pages must not be left as empty shells, generated changelog
  only pages, or checksum-only messages.
- The main release workflow now prefers
  `docs/user/RELEASE_NOTES_<tag>.md` when present.
- The split provider release workflow now prefers
  `docs/release-notes/<tag>.md` when present.

Version compatibility:

- `docs/release/TERRAFORM_PROVIDER_DISTRIBUTION_PLAN.md` contains the current
  provider/runtime support matrix.
- Provider `0.1.0-alpha.7` supports runtime `v0.1.0-alpha.2`.
- The same document defines the SemVer compatibility policy for provider and
  runtime releases.
- `AGENTS.md` repeats the operating rule that patch releases must not introduce
  breaking user-facing behavior, Terraform schema incompatibility, runtime
  config incompatibility, or required replacement beyond documented bug/security
  fixes.

Dependency and datapath pins:

- `docs/release/DEPENDENCY_PINS.md` records the LoxiLB version and immutable
  image digest for runtime `v0.1.0-alpha.2`.
- `scripts/check-release-pins.sh` verifies that the pin appears consistently in
  bootstrap, Packer, AMI provisioning, notices, and release docs.

Signing decision:

- `docs/release/ARTIFACT_SIGNING_DECISION.md` explicitly documents the current
  checksum-only alpha decision for BetterNAT application artifacts.
- Split Terraform provider artifacts are signed in the provider repository for
  Terraform Registry ingestion.
- Production signing for BetterNAT application artifacts is intentionally
  deferred and documented as a production target.

## Review Result

The release artifact governance checklist is complete for the current
production-preview/GA planning pass:

- checksum workflow documented and reproducible,
- runtime/provider support matrix accurate for current provider/runtime pair,
- SemVer policy documented,
- patch releases explicitly non-breaking,
- artifact signing decision documented even though BetterNAT application
  artifact signing is deferred.

Remaining hardening:

1. Implement application artifact signing before claiming signed BetterNAT
   application releases.
2. Attach SBOM/provenance metadata before a production-ready release if the
   release policy requires it.
3. Continue requiring release notes as a hard publication gate for every new
   tag.
