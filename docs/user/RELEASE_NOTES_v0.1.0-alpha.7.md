# BetterNAT v0.1.0-alpha.7 Release Notes

Date: 2026-06-24

## Status

`v0.1.0-alpha.7` is a main-repository alpha release used to publish the
provider module changes needed by split provider `0.1.0-alpha.8`.

For Terraform users, use provider `nowakeai/betternat` `0.1.0-alpha.8` or
newer and keep the runtime on the already validated alpha6 artifact set:

```hcl
betternat_version = "v0.1.0-alpha.6"
```

## What Changed

- Added `v0.1.0-alpha.6` to the Terraform provider runtime artifact support
  matrix.
- Added arm64 and amd64 checksum entries for the alpha6 `betternat-agent` and
  `betternat` release assets.
- Added provider tests that verify alpha6 artifact URL and checksum derivation.
- Updated Terraform examples and user docs to recommend provider alpha8 with
  runtime alpha6.

## Validation

- `GOCACHE=$PWD/tmp/go-build go test ./internal/tfprovider`: passed.
- `GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat`:
  passed.
- The alpha6 release asset checksums were read from the published
  `SHA256SUMS` file before adding them to the provider support matrix.

AWS soak validation is tracked separately in the GA checklist. Do not treat
this release note as a production SLA or benchmark result.

## Known Limitations

- This is still an alpha technical preview.
- No NAT Gateway equivalent SLA.
- No active connection preservation.
- No public BetterNAT AMI is published.
- Provider `0.1.0-alpha.8` intentionally supports the already published
  `v0.1.0-alpha.6` runtime artifact set. Use explicit artifact URL overrides
  only for unreleased or experimental runtime builds.

## Upgrade Notes

- Existing alpha users should test in a disposable VPC before replacing gateway
  nodes.
- If moving from runtime `v0.1.0-alpha.2` to `v0.1.0-alpha.6`, plan a gateway
  replacement or controlled rolling replacement of the ASG nodes. Updating a
  launch template alone does not prove the running nodes changed binaries.
- Keep `associate_public_ip_address` unset unless your network design provides
  first-boot package, Docker, GitHub release artifact, SSM, and AWS API
  reachability another way.

## Artifact Integrity

Verify downloads with the attached `SHA256SUMS` file.

This release note is not legal advice.
