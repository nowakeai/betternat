# BetterNAT v0.1.0-alpha.8 Release Notes

Date: 2026-06-24

## Status

`v0.1.0-alpha.8` is a BetterNAT runtime alpha release for GA hardening of
proactive handover and ASG lifecycle termination handling.

Terraform users should wait for a Terraform provider release that explicitly
lists this runtime version in its support matrix before using:

```hcl
betternat_version = "v0.1.0-alpha.8"
```

Until that provider release is available, keep using provider
`nowakeai/betternat` `0.1.0-alpha.8` with runtime `v0.1.0-alpha.6`.

## What Changed

- Hardened proactive handover route replacement.
- Added per-attempt timeouts around `ec2:ReplaceRoute` during handover so one
  slow AWS API call does not consume the full handover request budget.
- Added short retry/backoff behavior for handover route replacement.
- Added route verification after an ambiguous route replacement error. If AWS
  applied the route but the client saw a timeout or canceled response,
  BetterNAT now accepts the operation after `DescribeRoute` confirms the route
  target converged.
- Increased the local daemon handover request timeout from `30s` to `45s`.
- Added unit tests for ambiguous route replacement errors and retry-to-converge
  behavior.

## Validation

Local validation before publishing:

- `GOCACHE=$PWD/tmp/go-build go test ./internal/ha ./internal/agent`: passed.
- `GOCACHE=$PWD/tmp/go-build go test ./...`: passed.
- `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`: passed.
- `git diff --check`: passed.

AWS validation of this exact runtime should be run through a provider release
that supports `v0.1.0-alpha.8`. The target scenario is ASG active instance
termination with lifecycle-triggered proactive handover.

## Known Limitations

- This is still an alpha technical preview.
- No NAT Gateway equivalent SLA.
- No active connection preservation.
- No public BetterNAT AMI is published.
- This release improves route replacement retry behavior, but it does not
  remove the need for fenced lease takeover as the final safety path.
- DynamoDB timeout or IAM-denial negative tests remain GA hardening follow-up
  work.

## Upgrade Notes

- Existing alpha users should test in a disposable VPC before replacing gateway
  nodes.
- Provider support is required before the public Terraform install path can use
  this runtime through `betternat_version`.
- Updating a launch template does not update already-running gateway nodes by
  itself. Use a controlled ASG replacement or rolling update procedure.
- This release is intended to be compatible with the alpha6 bootstrap contract.

## Artifact Integrity

Verify downloads with the attached `SHA256SUMS` file.

This release note is not legal advice.
