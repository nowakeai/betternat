# BetterNAT v0.1.0-alpha.1 Release Notes

Date: 2026-06-21

## Status

`v0.1.0-alpha.1` is the first free/open-source technical preview of BetterNAT.

It is intended for evaluation in disposable or non-critical AWS environments.

## What Is Included

- AWS single-AZ active/standby gateway group.
- Terraform provider resource: `betternat_gateway`.
- LoxiLB/eBPF datapath.
- DynamoDB-backed lease/fencing.
- AWS route replacement for private default routes.
- Optional shared EIP for stable public egress identity.
- ASG-based capacity repair.
- Prometheus metrics.
- Appliance-local CLI diagnostics:
  - `betternat doctor`,
  - `betternat doctor --live`,
  - `betternat datapath ready`,
  - `betternat status`,
  - `betternat cost estimate`.
- Release build script with checksums and manifest.

## Packaging

This release does not publish a BetterNAT AMI.

The alpha install path is:

```text
Terraform -> explicit Linux AMI -> cloud-init -> BetterNAT release artifacts
```

Cloud-init downloads and verifies:

- `betternat-agent`,
- `betternat` CLI,
- optional `loxicmd`.

The default LoxiLB runtime image is pinned by digest:

```text
ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052
```

Observed platform manifests on 2026-06-21:

- `linux/amd64`: `sha256:f435d5170eaf7cb13ec738a1ea5c82a943187b2fc6cae432539a304632a9febf`
- `linux/arm64`: `sha256:70613f1f4c80427424f0563db51723e154feee0b11226addef3959bfd64c4eaf`

## Known Limitations

- No NAT Gateway equivalent SLA.
- No active connection preservation.
- AWS only.
- Single-AZ HA group only.
- No published BetterNAT AMI.
- No CloudFormation template.
- No AWS Marketplace listing.
- No high-volume benchmark claim.
- Boot time depends on package repositories, container pull, and artifact URL reachability.
- Stable EIP mode preserves public identity after convergence; failure detection and AWS control-plane convergence still take time.
- Non-stable mode changes public source IP after failover.
- Advanced kernel/NIC tuning is not yet exposed as a supported profile.

## Validation Evidence

Current release-candidate evidence:

- `docs/research/031-aws-low-cost-supplemental-results.md`
- `docs/research/035-p0-open-source-release-acceptance-results.md`

Validated before this release candidate:

- stable EIP baseline and failover,
- non-stable egress baseline and failover,
- ASG repair,
- replacement standby behavior,
- bootstrap artifact install,
- appliance-local `doctor --live`,
- IAM negative test,
- Terraform destroy and artifact cleanup.

## Legal And Attribution

BetterNAT integrates LoxiLB as a third-party datapath component.

LoxiLB is licensed under Apache License 2.0. BetterNAT does not imply NetLOX/LoxiLB endorsement, partnership, certification, or official support.

This release note is not legal advice.
