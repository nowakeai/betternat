# BetterNAT Artifact Signing Decision

Date: 2026-06-24

## Decision

For the current `v0.1.0-alpha` BetterNAT application release artifacts, use:

- GitHub Release assets,
- `SHA256SUMS`,
- release workflow checksum verification,
- Terraform/provider checksum inputs for bootstrap downloads.

Do not add BetterNAT application artifact signatures to the current alpha
release train.

This decision applies to:

- `betternat-agent`,
- `betternat` CLI,
- release `manifest.json`,
- bootstrap-time agent and CLI downloads.

Terraform provider distribution is separate. Provider Registry distribution and
the split provider release path have their own checksum/signing requirements.

## Rationale

The first alpha is a technical preview for disposable or non-critical AWS
environments. The highest current risks are install correctness, route/EIP
ownership, cleanup, runtime HA behavior, and transparent limitations.

Checksums are already enforced in the paths that matter for alpha:

- `scripts/release-build.sh` writes `SHA256SUMS`,
- the release workflow runs `sha256sum -c SHA256SUMS`,
- Quick Start asks users to derive agent and CLI checksums from `SHA256SUMS`,
- bootstrap verifies `agent_binary_sha256` and `cli_binary_sha256` when provided,
- release checklist requires uploaded assets to match `SHA256SUMS`.

Adding signatures now would create key-management and documentation obligations
without materially changing the alpha support boundary. It would also risk
publishing a half-supported signing story before AMI publication, provider
distribution, and release automation are stable.

## Production Target

Before a production-ready release, BetterNAT should choose and implement one
supported signing path:

- Sigstore/cosign keyless signing for release assets and attestations,
- minisign for a small static public-key verification story,
- GPG signatures for `SHA256SUMS` if maintainers want the Terraform-provider
  style checksum-signature model.

Production signing should cover:

- release asset checksums,
- release manifest,
- AMI manifest or image provenance,
- SBOM or dependency inventory,
- provider artifacts if they are published outside the Terraform Registry path.

The production signing design must document:

- public key or identity discovery,
- key rotation,
- revocation or compromised-key response,
- exact user verification commands,
- CI permissions required for signing.

## User-Facing Language

For alpha:

```text
BetterNAT alpha release assets are checksum-verified but not signed. Use the
official GitHub Release assets and verify SHA256SUMS before deployment. Do not
use untrusted mirrors for production-like tests.
```

For production, do not claim signed artifacts until the release workflow
produces and verifies signatures automatically.
