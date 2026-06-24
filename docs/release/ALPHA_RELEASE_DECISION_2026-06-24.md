# BetterNAT Alpha Release Decision

Date: 2026-06-24

## Decision

Ship the current BetterNAT alpha path with documented limitations.

Release posture:

- release level: `alpha`
- runtime version: `v0.1.0-alpha.2`
- Terraform provider version for public examples: `0.1.0-alpha.5`
- verification base commit: `19a23b7`
- AWS scope: single-AZ HA group in AWS
- install path: Terraform plus cloud-init bootstrap on an explicit Linux AMI
- AMI publication: deferred

## Why This Can Ship As Alpha

The first alpha promise is narrow: a technical user can deploy BetterNAT in AWS
with Terraform, verify private-subnet egress, observe HA state, test failover,
and destroy resources.

Current evidence satisfies that alpha promise:

- release artifacts and checksums exist for `v0.1.0-alpha.2`,
- public examples use GitHub Release URLs, not maintainer-only S3 URLs,
- provider `0.1.0-alpha.5` is usable through the provider GitHub release as a
  Terraform filesystem mirror,
- disposable AWS apply/destroy smoke passed with no residual resources,
- stable and non-stable egress modes have AWS validation evidence,
- daemon-backed `betternat status`, `doctor --live`, handover commands, and
  `support bundle` are implemented and documented,
- final local release verification passed on 2026-06-24.

## Final Verification Evidence

Local and release checks:

- `GOCACHE=$PWD/tmp/go-build go test ./...`: passed.
- `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`: passed.
- `scripts/check-release-pins.sh`: passed.
- `BETTERNAT_RELEASE_DIR=$PWD/tmp/release-final-check scripts/release-build.sh`: passed.
- `BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-url-smoke.sh`: passed.
- `BETTERNAT_VERSION=v0.1.0-alpha.2 BETTERNAT_SMOKE_ARCH=amd64 scripts/release-url-smoke.sh`: passed.
- `scripts/release-dependency-scan.sh`: passed with `99` modules scanned, `0`
  missing license files, and `0` restricted license keyword hits.
- `source scripts/setup-provider-github-mirror.sh` followed by Terraform
  `init -upgrade` and `validate` passed for all public examples.

Release deploy smoke:

- `bnat-final-plan-alpha3-20260624-profile`: plan-only smoke passed with
  `AWS_PROFILE=601427795217_AdministratorAccess`, runtime
  `v0.1.0-alpha.2`, provider `0.1.0-alpha.3`, and provider install
  `github-mirror`.
- Terraform planned `16` creates, `0` changes, and `0` destroys.

AWS acceptance evidence:

- `bnat-p0-20260621044411`: bootstrap release artifact path, private egress,
  appliance-local `doctor --live`, IAM negative test, destroy, and cleanup.
- `bnat-release-apply-alpha3-20260624034150`: release deploy smoke created
  `16` resources, destroyed `16` resources, and residual scan was clean.
- `docs/research/040-alpha-low-cost-soak-results.md`: stable/no-public-IP soak
  smoke, active systemd-stop handover, live LoxiLB restart recovery, and
  non-stable route-only handover comparison.

## Known Alpha Limitations

These limitations are acceptable for the current alpha and must remain visible
in user-facing docs:

- no NAT Gateway equivalent SLA,
- no active connection preservation,
- AWS only,
- single-AZ HA group only,
- no published BetterNAT AMI,
- bootstrap depends on package repositories, Docker/image pull, and GitHub
  release artifact reachability,
- Terraform Registry provider releases newer than `0.1.0-alpha.2` have not
  propagated yet; public
  examples use `scripts/setup-provider-github-mirror.sh`,
- Spot interruption handling follows AWS IMDS documentation, but forced Spot
  interruption was not required as a first-alpha release gate,
- high-volume savings are modeled, not proven by multi-TB benchmark runs,
- non-stable handover can be faster, but public source IP changes by design.

## Deferred Work

Post-alpha / production work:

- Terraform Registry propagation beyond `0.1.0-alpha.2` and removal of the GitHub
  filesystem-mirror workaround,
- bootstrap-first production-preview UX polish, including easier runtime
  artifact URL/checksum selection,
- optional arm64 and amd64 AMIs only if ongoing snapshot-retention cost is
  accepted,
- optional `ami_channel` resolver only if public AMIs become supported,
- stronger artifact signing beyond checksums as later hardening,
- benchmark harness and sizing guidance as post-production-preview evidence,
- broader hardening and longer soak tests as follow-up evidence,
- CloudFormation delivery if it becomes a supported install path.

## Release Decision

```text
Release decision: ship alpha
Reason: alpha gates are satisfied; remaining gaps are documented alpha
limitations or post-alpha/production work.
```
