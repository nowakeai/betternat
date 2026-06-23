# Release Dependency Pins

BetterNAT releases must pin the third-party datapath runtime they ship or
bootstrap. A BetterNAT version is not allowed to float to a different LoxiLB tag
or image digest without a new BetterNAT release.

## Current Pins

| BetterNAT version | LoxiLB version | Image reference | Package reference | Status |
| --- | --- | --- | --- | --- |
| `v0.1.0-alpha.2` | `v0.9.8.6` | `ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052` | <https://github.com/orgs/loxilb-io/packages/container/loxilb/960366893?tag=v0.9.8.6> | current alpha |

## Policy

- Pin by immutable digest in code, bootstrap templates, Packer variables, AMI
  provisioning scripts, and release docs.
- Record the human-readable upstream tag next to the digest in this document.
- Do not use `latest` in released artifacts.
- Do not change the LoxiLB digest for an already-published BetterNAT release.
- If a LoxiLB upgrade is required, cut a new BetterNAT release and update this
  file in the same change.
- If BetterNAT publishes AMIs, the AMI manifest must include the exact LoxiLB
  image reference used at build time.

## Validation

Run:

```sh
scripts/check-release-pins.sh
```

This checks that the current release pin appears consistently in:

- `internal/bootstrap/bootstrap.go`,
- `internal/bootstrap/bootstrap_test.go`,
- `packer/betternat.pkr.hcl`,
- `scripts/ami/provision-betternat-ami.sh`,
- this document,
- `THIRD_PARTY_NOTICES.md`.
