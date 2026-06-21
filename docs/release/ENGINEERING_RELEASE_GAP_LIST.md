# BetterNAT Engineering Release Gap List

Date: 2026-06-21

## Purpose

This document lists the code, feature, and test work still needed before the first free/open-source alpha release.

It complements:

- `docs/release/RELEASE_CHECKLIST.md`
- `docs/release/OPEN_SOURCE_RELEASE_PLAN.md`
- `docs/user/OPERATIONS_GUIDE.md`
- `docs/user/FAILURE_MODES.md`

## Release Target

Target:

```text
v0.1.0-alpha.1
```

Positioning:

- free and open source,
- AWS only,
- Terraform-first,
- LoxiLB-first datapath,
- nftables fallback,
- ASG active/standby HA,
- Prometheus metrics,
- no central server,
- no paid edition mentioned in user-facing docs.

## Priority Definitions

P0:

- required before alpha release candidate,
- blocks user trust, installation, cleanup, or basic troubleshooting.

P1:

- strongly recommended for alpha,
- can ship with a documented limitation only if time-boxed and low risk.

P2:

- useful, but safe to defer past the first alpha.

## P0 Code And Feature Gaps

### 1. Live Doctor

Current:

- `betternat doctor` performs static/config-level checks.
- `betternat doctor --live` adds first-pass appliance-local live checks.

Implemented:

- Check:
  - local datapath readiness,
  - IAM runtime permission simulation through AWS IAM `SimulatePrincipalPolicy`,
  - ASG fleet health aggregation for the local AZ pool,
  - Prometheus endpoint reachability,
  - DynamoDB lease row,
  - route target match,
  - EIP/public identity match when stable EIP mode is enabled,
  - source/destination check disabled,
  - outbound source-IP probe when configured.

Still needed:

- P1 polish for the static rollback-config warning when running on a gateway appliance.

Acceptance:

- command returns JSON,
- exits nonzero on critical failure,
- works on the gateway appliance,
- does not mutate AWS state,
- has unit tests with fake checker dependencies,
- documented in operations guide.

### 2. Version Injection

Current:

- CLI, agent, Terraform provider, and Prometheus build-info metrics share build-time version metadata.

Implemented:

- Shared build info variables for:
  - version,
  - commit,
  - build date,
  - Go version,
  - OS/arch.
- Support:
  - `betternat version`,
  - `betternat-agent --version`,
  - Terraform provider version,
  - `betternat_agent_build_info`.

Acceptance:

- release build can set values with `-ldflags`,
- default dev build still works,
- unit tests cover version output shape.

### 3. Open-Source Release Files

Current:

- `.gitignore` exists.
- Required public release files exist.

Implemented:

- `LICENSE`
- `SECURITY.md`
- `CONTRIBUTING.md`
- `THIRD_PARTY_NOTICES.md`

Acceptance:

- BetterNAT project license is explicit.
- Security disclosure process is clear.
- LoxiLB Apache 2.0 attribution is present.
- Third-party notice path matches release checklist.

### 4. Release Build And Manifest Script

Current:

- `scripts/release-build.sh` builds release artifacts and writes `SHA256SUMS` plus `manifest.json`.

Needed:

- Expand provider artifact matrix when publishing for more local developer platforms.

Acceptance:

- one command builds local release artifacts under `tmp/release/`,
- manifest records version, commit, build date, Go version, OS/arch, artifact checksum,
- script does not require network access after dependencies are available.

### 5. Bootstrap Release Path

Current:

- no official BetterNAT AMI exists.
- AWS tests used AL2023 + cloud-init/bootstrap style paths.
- `docs/release/ALPHA_BOOTSTRAP_RELEASE_PATH.md` defines the alpha bootstrap release path.
- Terraform supplemental fixture accepts agent and CLI artifact URLs with SHA256 checksums, plus optional `loxicmd_binary_sha256`.
- The first public alpha intentionally does not build or publish a BetterNAT AMI.

Still needed for alpha:

- Release notes and user-facing docs must explicitly describe the bootstrap path and say no BetterNAT AMI is published in the first alpha.

Acceptance:

- user can deploy from the Terraform example without local absolute paths,
- release notes state whether AMI is prebuilt or bootstrap-based,
- bootstrap installs/runs agent, LoxiLB, `loxicmd`, nftables fallback tools, and systemd unit.
- bootstrap applies the documented baseline sysctl profile.

### 6. Release Artifact Hygiene

Needed:

- Ensure ignored local files are not tracked:
  - `.env`,
  - `*.tfstate`,
  - `.terraform.lock.hcl` if generated only by local examples,
  - built binaries,
  - temporary AWS outputs.
- Scan for:
  - presigned URLs,
  - AWS credentials,
  - private keys,
  - local absolute paths.

Acceptance:

```sh
rg -n "X-Amz|AWSAccessKeyId|BEGIN (RSA|OPENSSH|EC|PRIVATE) KEY|BETTERNAT_AGENT_BINARY_URL=|/Users/|/mnt/mac/" . --glob '!tmp/**' --glob '!.git/**'
```

returns no release-blocking results.

## P0 Test Gaps

### Current P0 Verification Snapshot

Last updated: 2026-06-21.

Completed locally:

- [x] Full Go test suite passed:
  - `GOCACHE=$PWD/tmp/go-build-cache go test ./...`
- [x] Terraform provider binary builds:
  - `GOCACHE=$PWD/tmp/go-build-cache go build -o terraform-provider-betternat ./cmd/terraform-provider-betternat`
- [x] Terraform examples validate with local provider dev override and `TMPDIR=$PWD/tmp`:
  - `examples/terraform`
  - `examples/terraform-aws-supplemental`
  - `examples/terraform-localstack`
- [x] Release build script generates artifacts:
  - `BETTERNAT_VERSION=v0.1.0-alpha.test BETTERNAT_RELEASE_DIR=$PWD/tmp/release-test scripts/release-build.sh`
  - `SHA256SUMS` includes host CLI, provider, Linux arm64/amd64 agent, and Linux arm64/amd64 CLI artifacts.
- [x] `git diff --check` passes.
- [x] Tracked-file hygiene check shows no tracked `.env`, Terraform state, local release artifacts, or `tmp/**` files.
- [x] Secret/local-path scan has no release-blocking hits. Current hits are documentation examples or historical notes:
  - command examples in this file and `RELEASE_CHECKLIST.md`,
  - bootstrap environment variable examples in `ALPHA_BOOTSTRAP_RELEASE_PATH.md` and `AWS_SUPPLEMENTAL_RUNBOOK.md`,
  - historical presigned URL bug discussion in `026-aws-supplemental-test-results.md`.
- [x] Linux VM nftables datapath smoke passed in OrbStack Ubuntu arm64:
  - `scripts/linux-smoke-nftables.sh`
  - `scripts/linux-smoke-nftables-udp.sh`
  - `scripts/linux-smoke-nftables-throughput.sh`
- [x] Linux VM agent metrics with nftables fallback passed:
  - `scripts/linux-smoke-agent-nftables.sh`
- [x] `doctor --live` P0 local unit coverage passed:
  - `GOCACHE=$PWD/tmp/go-build-cache go test ./internal/cli ./internal/doctor ./internal/cloud/aws ./internal/iamcheck/aws`
  - includes IAM simulation wiring, ASG fleet health aggregation, and critical-status nonzero return behavior.
- [ ] Linux VM LoxiLB smoke is not proven in OrbStack:
  - `scripts/linux-smoke-loxilb.sh` started the container, but LoxiLB aborted or never became ready in this VM kernel/eBPF environment.
  - `scripts/linux-smoke-agent-loxilb.sh` timed out waiting for LoxiLB readiness in the same environment.
  - Treat LoxiLB datapath validation as AWS-required for alpha, not locally proven by this VM.

Completed in disposable AWS:

- [x] AWS low-cost acceptance with current bootstrap release artifacts:
  - `docs/research/035-p0-open-source-release-acceptance-results.md`
  - final passing run: `bnat-p0-20260621044411`
  - private-subnet egress returned stable EIP `52.36.9.40` and `HTTP/2 200`
  - Terraform destroy completed with `Resources: 16 destroyed`
  - temporary artifact bucket was deleted
  - direct EC2 check confirmed all test instances were `terminated`
- [x] IAM negative test in disposable AWS:
  - temporary explicit deny on `autoscaling:DescribeAutoScalingGroups`
  - `doctor --live` returned nonzero and `critical`
  - IAM and ASG checks reported the missing/denied permission
  - deleting the temporary deny restored `doctor --live` to exit `0`

### 1. Full Go Test Suite

Required:

```sh
GOCACHE=$PWD/tmp/go-build-cache go test ./...
```

Acceptance:

- passes on clean checkout after dependencies are available.

### 2. Terraform Provider Validation

Required:

```sh
go build -o terraform-provider-betternat ./cmd/terraform-provider-betternat
terraform -chdir=examples/terraform validate
terraform -chdir=examples/terraform-aws-supplemental validate
terraform -chdir=examples/terraform-localstack validate
```

Notes:

- In the current sandbox, Terraform provider validate may require local dev override and unsandboxed execution.
- Document exact commands in `TERRAFORM_PROVIDER_LOCAL_TESTING.md`.

### 3. Linux VM Smoke Tests

Required before alpha:

- nftables datapath smoke,
- nftables UDP smoke,
- nftables throughput sanity,
- agent metrics with nftables fallback,
- LoxiLB smoke if local Linux environment supports it.

Existing scripts:

- `scripts/linux-smoke-nftables.sh`
- `scripts/linux-smoke-nftables-udp.sh`
- `scripts/linux-smoke-nftables-throughput.sh`
- `scripts/linux-smoke-agent-nftables.sh`
- `scripts/linux-smoke-loxilb.sh`
- `scripts/linux-smoke-agent-loxilb.sh`

Acceptance:

- results documented,
- failures classified as environment limitation or product bug.

### 4. AWS Low-Cost Acceptance Test

Required if release artifacts differ from the last recorded AWS test.

Must cover:

- isolated VPC fixture,
- ASG desired capacity 2,
- stable EIP baseline,
- stable EIP failover,
- non-stable egress baseline,
- non-stable failover,
- owner termination,
- ASG replacement,
- standby rejoin,
- route target match,
- public identity match,
- metrics scrape,
- Terraform destroy,
- residual resource scan.

Do not include:

- tens-of-TB traffic tests,
- expensive long-running benchmarks.

Acceptance:

- documented under `docs/research/`,
- includes absolute timestamps, region/AZ, run ids, costs if known,
- cleanup scan is empty.

Current status:

- P0 bootstrap acceptance passed in `bnat-p0-20260621044411`.
- Full HA timing matrix remains covered by earlier supplemental runs and should be repeated after AMI packaging or major HA changes.

### 5. IAM Negative Test

Needed:

- remove or deny one required permission at a time in a disposable environment:
  - `ec2:ReplaceRoute`,
  - `ec2:AssociateAddress`,
  - DynamoDB update permission.

Acceptance:

- agent/doctor reports understandable failure,
- no unrelated resources are mutated,
- cleanup remains possible.

Current status:

- P0 negative test passed for `autoscaling:DescribeAutoScalingGroups`.
- Additional per-action negative tests for `ec2:ReplaceRoute`, `ec2:AssociateAddress`, and DynamoDB write denial are useful P1 hardening, not current P0 blockers.

## P1 Code And Feature Gaps

### 1. Support Bundle

Add:

```sh
betternat support bundle --config /etc/betternat/agent.json --out /tmp/betternat-support.tgz
```

Bundle:

- redacted config,
- CLI status output,
- doctor output,
- datapath ready output,
- metrics snapshot,
- recent agent logs,
- optional AWS state snapshots if credentials are available.

Acceptance:

- redacts obvious secrets,
- does not include Terraform state by default,
- documented in operations guide.

### 2. HA Status Command

Add:

```sh
betternat ha status --config /etc/betternat/agent.json
```

Should aggregate:

- local instance id,
- lease owner,
- active/standby state,
- route target match,
- EIP match,
- datapath ready,
- metrics freshness.

Can overlap with `doctor --live`.

### 3. Prometheus Alert Rules

Add:

```text
examples/prometheus/betternat-alerts.yaml
```

Rules:

- no active appliance,
- HA status stale,
- route target mismatch,
- EIP mismatch,
- datapath not ready,
- lease renew errors,
- repeated takeover attempts.

Acceptance:

- syntax checked with `promtool` if available,
- documented in operations guide.

### 4. Grafana Starter Dashboard

Add a starter dashboard or documented queries.

Minimum panels:

- active owner,
- standby health,
- processed bytes,
- owner bytes,
- conntrack entries,
- route/EIP match,
- failover attempts/success.

### 5. AMI Build Pipeline

For alpha, a documented bootstrap path is the intended release path.

For production, add:

- Packer or EC2 Image Builder definition,
- AMI naming/versioning,
- arm64 and amd64 builds or explicit architecture scope,
- AMI file listing validation,
- license files installed into AMI.

### 6. Advanced Kernel/NIC Tuning Profile

Current alpha bootstrap applies only the conservative baseline gateway sysctls documented in `ALPHA_BOOTSTRAP_RELEASE_PATH.md`.

LoxiLB/eBPF has its own conntrack state, so Linux `nf_conntrack_max` is not a primary LoxiLB capacity knob. BetterNAT keeps it only as a conditional fallback/compatibility setting when the kernel exposes it.

Defer until benchmark-backed:

- conntrack hash bucket sizing,
- conntrack TCP/UDP timeout profile,
- ephemeral port range tuning,
- backlog tuning,
- IRQ/RSS/queue tuning,
- ENA or instance-family-specific settings.

## P2 Deferrable Work

- CloudFormation install path.
- Multi-cloud runtime.
- EKS pod attribution.
- top-N source/destination CLI.
- flow store.
- custom eBPF.
- active-active NAT.
- active connection migration.
- central server.
- hosted service.
- paid/commercial packaging.
- large multi-TB validation.

## Current Known Implementation Notes

- ASG pool model has been validated in low-cost AWS tests.
- Stable EIP and non-stable failover have both been tested.
- Observed owner-termination outage in low-cost AWS test was about 12 seconds under test conditions.
- `doctor --live` is now implemented for appliance-local cloud checks; static rollback-config warning needs P1 UX polish.
- Release packaging now has a bootstrap artifact path; AMI packaging remains the biggest production-release workflow gap.
- The first release should avoid making unproven performance claims.

## Suggested Implementation Order

1. Re-run final local Go/Terraform/release/hygiene verification after the P0 edits.
2. Add Prometheus alert rules.
3. Add support bundle or HA status command if we want a stronger alpha troubleshooting story.
4. Finish user-facing quick-start documentation.
5. Cut `v0.1.0-alpha.1` when the alpha checklist is satisfied.
