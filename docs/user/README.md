# BetterNAT User Documentation

This directory is organized by user task. Start with the install path, then use
operations and reference docs as needed.

## Getting Started

- [Quick Start](getting-started/QUICK_START.md) — Disposable-VPC install,
  verification, destroy, and cleanup guide.
- [Existing VPC Install](getting-started/EXISTING_VPC_INSTALL.md) — Existing
  VPC install and route ownership warnings.
- [Configuration](getting-started/CONFIGURATION.md) — Terraform
  `betternat_gateway` input reference and runtime configuration notes.

## Operations

- [Operations Guide](operations/OPERATIONS_GUIDE.md) — Day-2 operations, CLI,
  metrics, AWS checks, SSM access, troubleshooting, and cleanup.
- [Observability Guide](operations/OBSERVABILITY_GUIDE.md) — Prometheus
  metrics, CLI checks, AWS cross-checks, alerts, and observability limits.
- [Rollback Guide](operations/ROLLBACK_GUIDE.md) — Safe destroy, private route
  restoration, manual rollback, and residual-resource checks.
- [Upgrade And Replacement Guide](operations/UPGRADE_REPLACEMENT_GUIDE.md) —
  In-place capacity updates, explicit replacement, blue/green upgrades, and
  alpha rolling-upgrade limits.
- [Failure Modes](operations/FAILURE_MODES.md) — Failure behavior, handover
  semantics, and troubleshooting signals.

## Reference

- [Cost Model](reference/COST_MODEL.md) — NAT Gateway processing-fee model,
  BetterNAT cost formula, savings examples, and endpoint guidance.
- [IAM Policy](reference/IAM_POLICY.md) — Terraform execution and gateway
  runtime IAM requirements.
- [Security Hardening](reference/SECURITY_HARDENING.md) — Current alpha
  security posture, IAM/network/bootstrap hardening, and artifact integrity.
- [Limitations](reference/LIMITATIONS.md) — SLA, failover, cost, performance,
  bootstrap, and tuning limitations.

## Releases

Release notes are kept under [releases/](releases/) and named by tag. See the
[release notes index](releases/README.md) for release-note rules.

- [v0.1.0-alpha.8](releases/v0.1/v0.1.0-alpha.8.md)
- [v0.1.0-alpha.7](releases/v0.1/v0.1.0-alpha.7.md)
- [v0.1.0-alpha.6](releases/v0.1/v0.1.0-alpha.6.md)
- [v0.1.0-alpha.2](releases/v0.1/v0.1.0-alpha.2.md)
- [v0.1.0-alpha.1](releases/v0.1/v0.1.0-alpha.1.md)
