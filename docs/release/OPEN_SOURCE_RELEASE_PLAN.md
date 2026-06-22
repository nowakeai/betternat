# BetterNAT Open Source Release Plan

Date: 2026-06-21

## Purpose

The first BetterNAT public release is a free and open-source release.

This document defines the release posture, repository hygiene, artifact expectations, and user-facing documentation requirements for that first release.

## Positioning

User-facing first-release copy should describe BetterNAT as:

```text
A free, open-source, self-managed NAT gateway appliance for AWS private subnet egress.
```

Core claims:

- avoids AWS NAT Gateway per-GB processing charges,
- runs as user-owned EC2 appliances,
- supports Terraform-first installation,
- supports active/standby failover for new connections,
- supports stable public egress IP when shared EIP mode is enabled,
- exports Prometheus metrics,
- uses LoxiLB as the primary datapath and nftables as fallback.

Do not claim:

- AWS NAT Gateway equivalent SLA,
- zero packet loss failover,
- active connection preservation,
- active-active NAT,
- multi-cloud runtime support,
- full Kubernetes pod attribution,
- proven tens-of-TB benchmark results.

Do not mention in first-release user docs:

- paid editions,
- hosted services,
- future Pro features,
- commercial distribution channels,
- Marketplace plans.

## Repository Requirements

Required before public release:

- [x] `README.md` with clear alpha/private-preview status.
- [x] `LICENSE` for BetterNAT itself.
- [x] `SECURITY.md` with vulnerability reporting process.
- [x] `CONTRIBUTING.md`.
- [x] `THIRD_PARTY_NOTICES.md`.
- [x] `.gitignore` excludes build outputs, local state, secrets, Terraform state, and local paths.
- [x] No local absolute paths are committed.
- [x] No presigned URLs, AWS keys, tokens, or private endpoints are committed.
- [x] Examples do not require maintainer-only local files.
- [x] Documentation index includes all release-blocking docs.

Optional but recommended:

- [ ] `CODE_OF_CONDUCT.md`.
- [ ] GitHub issue templates.
- [ ] Pull request template.
- [x] Release drafter or changelog automation.

## License Choice

The BetterNAT project license must be chosen before public release.

Good candidates:

- Apache License 2.0: mature, permissive, patent grant, compatible with LoxiLB's Apache 2.0 license.
- MIT: simpler, permissive, no explicit patent grant.

Recommendation:

- Use Apache License 2.0 unless there is a strong reason not to.

Reasoning:

- BetterNAT is infrastructure software.
- LoxiLB is Apache 2.0.
- The patent grant is useful for networking/control-plane software.
- It is accepted in enterprise environments.

## Third-Party Notices

The release must include third-party notices for distributed artifacts.

At minimum, record:

- LoxiLB license and version,
- `loxicmd` license and version,
- Go module licenses where distributed in binaries,
- OS/package dependencies bundled in the AMI,
- base AMI family and source,
- nftables/conntrack package provenance.

For AMIs, include:

```text
/usr/share/doc/betternat/THIRD_PARTY_NOTICES.md
/usr/share/doc/betternat/licenses/loxilb/LICENSE
```

Release manifest should record:

```text
betternat version
betternat-agent checksum
betternat CLI checksum
terraform provider checksum
LoxiLB version
LoxiLB artifact digest
base AMI id
base OS version
```

## Versioning

Use one product version for the first release:

```text
v0.1.0-alpha.1
```

Artifacts should include the same product version:

- `betternat-agent`,
- `betternat` CLI,
- `terraform-provider-betternat`,
- AMI name,
- documentation release,
- examples.

The binaries should expose:

```sh
betternat version
betternat-agent --version
```

Version output should include:

- version,
- git commit,
- build date,
- target OS/arch.

## Release Artifacts

Alpha artifacts:

- Linux arm64 `betternat-agent`,
- Linux amd64 `betternat-agent`,
- Linux arm64 `betternat` CLI,
- Linux amd64 `betternat` CLI,
- local-dev `betternat` CLI if supported,
- Terraform provider binary,
- checksums,
- release manifest,
- source archive,
- example Terraform configuration.

Public alpha artifacts are distributed as GitHub Release assets under:

```text
https://github.com/nowakeai/betternat/releases/download/<version>/
```

The user-facing Quick Start must reference GitHub Release asset URLs plus `SHA256SUMS`. BetterNAT does not provide a public S3 artifact bucket, and users should not need to create one to install the public release.

Temporary private S3 or equivalent hosting is allowed only for maintainer-run AWS tests of unreleased local builds.

Production artifacts later:

- arm64 AMI,
- amd64 AMI,
- AMI release manifest,
- Packer or EC2 Image Builder logs,
- SBOM if available.

## User-Facing Documentation

Required first-release docs:

- Quick Start.
- Existing VPC install.
- Disposable test VPC install.
- Uninstall and cleanup.
- Operations guide.
- Failure modes and limitations.
- Security model.
- IAM policy.
- Observability guide.
- Cost model and caveats.
- Release checklist.

Docs must explain:

- BetterNAT reduces NAT Gateway processing charges, but normal AWS resource and data-transfer charges still apply.
- Stable EIP mode preserves the public source IP for new connections after failover.
- Non-stable mode may change public source IP after failover.
- Active connections may reset.
- Single-AZ HA group is the v0 scope.
- Users own operation, monitoring, and instance capacity.

## Validation Gate

Before tagging:

```sh
GOCACHE=$PWD/tmp/go-build-cache go test ./...
git diff --check
BETTERNAT_VERSION=v0.1.0-alpha.1 scripts/release-build.sh
```

Terraform examples must validate with local provider override.

AWS low-cost acceptance test must pass if the provider, agent, bootstrap, or examples changed after the last recorded AWS result.

Before publishing:

- create the GitHub release for the tag,
- upload `betternat-agent`, `betternat`, provider artifacts, `SHA256SUMS`, and `manifest.json`,
- verify the Quick Start artifact URLs return HTTP 200,
- verify checksums in `SHA256SUMS` match the uploaded assets,
- verify no user-facing install docs require an S3 bucket.

## Release Notes

Release notes must include:

- status: alpha/private preview,
- supported cloud and region assumptions,
- supported architecture/OS,
- known limitations,
- installation links,
- cleanup instructions,
- checksums,
- LoxiLB dependency/version,
- security disclosure contact,
- upgrade notes from previous release, if any.

## Non-Goals For First Release

- Hosted control plane.
- Paid edition.
- Marketplace listing.
- Multi-cloud runtime.
- Full production SLA.
- High-volume benchmark claim.
- Active-active NAT.
- Transparent active-flow migration.
