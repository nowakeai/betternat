# BetterNAT v0.1.0-alpha.2 Release Notes

Date: 2026-06-23

## Status

`v0.1.0-alpha.2` is a technical preview release for disposable or
non-critical AWS environments.

It remains AWS-only and single-AZ for the first public evaluation path.

## What Changed Since v0.1.0-alpha.1

- Added daemon-backed `betternat status` with cached fleet state.
- Added `betternat status --watch`.
- Added DynamoDB-backed agent self-registration through the coordination table.
- Added durable handover operation records:
  - `betternat handover current`,
  - `betternat handover history`,
  - `betternat handover inspect`.
- Added proactive handover support with idempotent request records.
- Added peer API authentication for handover prepare requests.
- Added systemd stop, ASG lifecycle, and Spot interruption trigger paths for
  graceful handover/lease release.
- Added `betternat support bundle` for local redacted diagnostics collection.
- Added bounded AWS SDK retry/backoff policy for runtime and provider clients.
- Reduced normal runtime fleet-status dependency on AWS ASG/EC2 discovery
  permissions by using the coordination table.
- Added a Packer-based AMI build scaffold for production packaging work.
- Renamed user-facing runtime terminology from instance/appliance toward
  gateway node and `node_id`.

## Packaging

This release still does not publish a BetterNAT AMI.

This release pins LoxiLB to `v0.9.8.6` by immutable image digest:

```text
ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052
```

The alpha install path remains:

```text
Terraform -> explicit Linux AMI -> cloud-init -> BetterNAT release artifacts
```

Public alpha users should install from GitHub Release assets and verify
`SHA256SUMS`.

## Known Limitations

- No NAT Gateway equivalent SLA.
- No active connection preservation.
- No published BetterNAT AMI.
- No CloudFormation template.
- No high-volume benchmark claim.
- Boot time still depends on package repositories, container pull, and release
  artifact URL reachability.
- Stable EIP mode requires no per-node public IPv4 addresses to avoid
  non-shared egress identity during handover. With that path enabled, alpha AWS
  validation preserved the shared public IP but still observed a short timeout
  window.
- Non-stable mode changes public source IP after handover and can be faster
  because it avoids EIP reassociation. A 2026-06-24 AWS probe observed the
  visible source-IP switch within about `435 ms` with `0` failed samples.
- Spot interruption handling follows the AWS IMDS interruption-notice path, but
  real Spot interruption is not forced as a release gate.
- Packer AMI build files exist, but AMIs are not published and
  `ami_channel` resolution is not implemented.

## Validation Evidence

Local validation before release:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

AWS validation before this release included:

- daemon-backed status on two Spot gateway nodes,
- manual proactive handover,
- client-side handover interruption timing,
- route and shared-EIP convergence,
- handover history and inspect commands,
- in-place runtime update without ASG refresh.

## Legal And Attribution

BetterNAT integrates LoxiLB as a third-party datapath component.

LoxiLB is licensed under Apache License 2.0. BetterNAT does not imply
NetLOX/LoxiLB endorsement, partnership, certification, or official support.

This release note is not legal advice.
