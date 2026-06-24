# BetterNAT Security And Supply Chain Guide

Date: 2026-06-22

## Purpose

This guide describes the current BetterNAT security posture, first-alpha limits, and production hardening checklist.

BetterNAT is a self-managed network node. Treat it as privileged infrastructure: it changes VPC routes, owns egress identity, and runs a local datapath.

## Current Alpha Posture

The first alpha includes:

- Terraform-created IAM role and instance profile,
- AWS SDK based runtime operations,
- SSM Session Manager access by default,
- no public SSH requirement,
- IMDSv2 required in launch templates,
- source/destination check disabled by the agent,
- Prometheus metrics endpoint on the node,
- SHA256 verification for downloaded BetterNAT binaries when checksums are provided,
- Apache-2.0 project license,
- third-party notices for LoxiLB and other integrated components.

Important alpha limitations:

- runtime IAM is not yet a final least-privilege production policy,
- cloud-init downloads runtime artifacts during boot because no BetterNAT AMI is published yet,
- LoxiLB runs in a privileged container in the alpha bootstrap path,
- release binaries use checksums, but BetterNAT application artifacts are not yet signed,
- no generated SBOM is attached to releases yet,
- no hosted BetterNAT dashboard or central security/audit server exists.

## IAM Surfaces

BetterNAT has two IAM surfaces:

1. Terraform execution identity.
2. Gateway runtime role.

The Terraform identity creates and destroys EC2, ASG, IAM, security group, DynamoDB, route, and EIP resources.

The runtime role is used by `betternat-agent` for:

- DynamoDB lease and fencing,
- EC2 route replacement,
- EIP association and verification,
- source/destination check self-disable,
- live diagnostics.

See [IAM_POLICY.md](IAM_POLICY.md) for the action list and current scope.

Production hardening targets:

- scope DynamoDB actions to the lease table,
- scope EC2 route operations to selected route tables where practical,
- scope EIP operations to the configured allocation ID when stable egress IP is enabled,
- decide whether `iam:SimulatePrincipalPolicy` remains enabled by default or becomes optional diagnostics permission,
- remove unused permissions after AWS acceptance tests verify the exact call set.

## Network Exposure

Default intended exposure:

- no inbound SSH,
- SSM Session Manager for node access,
- private-subnet traffic allowed from configured `private_cidrs`,
- outbound node traffic allowed for egress, AWS APIs, bootstrap, and LoxiLB image/artifact pulls,
- Prometheus port reachable only from the monitoring network.

Alpha provider-created node security group allows forwarded traffic from configured private CIDRs and outbound traffic to `0.0.0.0/0`.

Hardening recommendations:

- restrict `private_cidrs` to the actual VPC/private subnet ranges that should use BetterNAT,
- do not expose port `9108` publicly,
- use SSM instead of public SSH,
- if SSH is added manually, restrict it to a controlled bastion or VPN source,
- keep BetterNAT nodes in public subnets only because they need public egress; route private workloads through them from private subnets,
- avoid cross-AZ routing unless it is intentional and costed.

## Instance Metadata

Launch templates require IMDSv2:

```text
HttpEndpoint = enabled
HttpTokens = required
HttpPutResponseHopLimit = 1
```

Do not weaken these settings unless you have a specific compatibility reason and understand the credential exposure tradeoff.

## Bootstrap And Artifact Integrity

The first alpha bootstraps from an explicit Linux AMI and cloud-init.

Current protections:

- BetterNAT agent and CLI downloads support SHA256 checks,
- user data writes `/etc/betternat/agent.json` with mode `0600`,
- launch templates require IMDSv2,
- release assets are published through GitHub Releases,
- release workflow runs dependency/license scanning for Go modules.

Current gaps:

- no published BetterNAT AMI,
- no signed BetterNAT application artifact bundle,
- no generated SBOM attached to release assets,
- no pinned OS package repository snapshot,
- LoxiLB image is pulled at boot in the alpha path,
- alpha bootstrap may rely on auto-assigned per-node public IPv4 addresses for
  package and artifact downloads.

Recommended alpha usage:

- use official GitHub Release assets,
- verify `SHA256SUMS`,
- avoid untrusted artifact mirrors,
- pin LoxiLB image digests where possible,
- test bootstrap in a disposable VPC before using existing route tables.

The alpha artifact signing decision is documented in
`docs/release/ARTIFACT_SIGNING_DECISION.md`: BetterNAT application artifacts are
checksum-verified but not signed in the current alpha.

Production targets:

- publish versioned AMIs,
- bake BetterNAT binaries and LoxiLB into the AMI,
- decide whether to keep per-node public IPv4 for simple management reachability
  or move to private AWS API reachability. If strict separation is required,
  bind the shared egress EIP to a secondary private IP or secondary ENI rather
  than the primary management private IP,
- attach SBOM and dependency inventory to releases,
- sign release metadata or artifacts,
- record third-party license notices inside the AMI,
- document AMI refresh and security patch policy.

## systemd Hardening

The alpha service currently sets:

```ini
NoNewPrivileges=true
Restart=always
RestartSec=2s
```

Future AMI builds should evaluate additional hardening options carefully. The agent needs AWS SDK access, local config access, and datapath control. LoxiLB needs privileged datapath capabilities.

Candidate options for `betternat-agent`:

```ini
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/run /var/lib/betternat /etc/betternat
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW
RestrictAddressFamilies=AF_INET AF_INET6 AF_NETLINK AF_UNIX
SystemCallFilter=@system-service @network-io
```

Do not blindly apply these to the alpha cloud-init path. Validate them on Linux with LoxiLB, metrics, AWS SDK calls, and graceful shutdown behavior.

## Datapath Privilege

BetterNAT uses LoxiLB as the primary datapath in the first alpha.

Alpha bootstrap runs LoxiLB as a privileged host-network container because it needs kernel/network datapath access. This is acceptable for a technical preview, but production packaging should prefer a more controlled AMI-integrated runtime with a reviewed capability set.

Do not remove network administration privileges until LoxiLB datapath behavior has been validated under the proposed hardening profile.

## Metrics Security

The Prometheus endpoint does not currently implement authentication.

Protect it with network controls:

- security groups,
- private IP scraping,
- monitoring subnet allowlists,
- no public internet exposure.

Metrics can reveal:

- gateway names,
- HA state,
- instance IDs,
- private CIDRs,
- owner labels,
- traffic counters.

Treat metrics as internal operational data.

## Secrets And Sensitive Data

Do not commit:

- `.env`,
- `.secrets/`,
- private keys,
- presigned artifact URLs,
- Terraform state files,
- copied agent configs from production nodes.

The Terraform provider marks generated user data and agent config as sensitive, but Terraform state can still contain operational metadata. Protect state with encryption, access controls, and normal Terraform backend hygiene.

## Vulnerability Reporting

Use private vulnerability reporting. Do not open public GitHub issues for suspected vulnerabilities.

See the top-level [SECURITY.md](../../SECURITY.md).

Include:

- affected version or commit,
- deployment mode,
- AWS region if relevant,
- impact,
- reproduction steps,
- logs and metrics with secrets removed.

## Third-Party Components

BetterNAT integrates with:

- LoxiLB,
- AWS SDK for Go,
- Terraform Plugin Framework,
- Prometheus-compatible metrics.

See [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) for current notices.

Before production:

- generate a dependency inventory or SBOM,
- review dependency licenses,
- review LoxiLB license and NOTICE propagation for AMI distribution,
- document trademark usage for AWS, Terraform, OpenTofu, Prometheus, Grafana, and LoxiLB names.

## Production Hardening Checklist

Before using BetterNAT for critical workloads:

- review runtime IAM against real AWS actions,
- restrict metrics access,
- confirm no public SSH is required,
- verify route rollback commands,
- verify `rollback_route_targets_json`,
- test failover in a disposable VPC,
- test destroy and cleanup,
- decide whether Spot is acceptable,
- pin artifact versions and checksums,
- enable logs/metrics retention according to your policy,
- monitor active owner, route target, EIP owner, datapath readiness, and lease renew errors.

BetterNAT is not a managed AWS NAT Gateway SLA replacement. Security and operational ownership are part of the tradeoff for lower processing cost and better control.
