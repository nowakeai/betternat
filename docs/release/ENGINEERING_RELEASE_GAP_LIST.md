# BetterNAT Engineering Release Gap List

Date: 2026-06-24

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
v0.1.0-alpha.2
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
- `betternat doctor --live` adds first-pass node-local live checks.

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

Deferred polish:

- P1 polish for the static rollback-config warning when running on a gateway node.

Acceptance:

- command returns JSON,
- exits nonzero on critical failure,
- works on the gateway node,
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
- `CODE_OF_CONDUCT.md`
- GitHub issue templates:
  - bug report,
  - feature request,
  - operations question.
- Pull request template.

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

Completed for alpha:

- Release notes and user-facing docs explicitly describe the bootstrap path and
  state that no BetterNAT AMI is published in the current alpha.

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

Last updated: 2026-06-24.

Completed locally:

- [x] Full Go test suite passed:
  - `GOCACHE=$PWD/tmp/go-build-cache go test ./...`
  - final 2026-06-24 release verification also passed:
    `GOCACHE=$PWD/tmp/go-build go test ./...`
- [x] Terraform provider binary builds:
  - `GOCACHE=$PWD/tmp/go-build-cache go build -o terraform-provider-betternat ./cmd/terraform-provider-betternat`
  - final 2026-06-24 release verification also passed:
    `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`
- [x] Terraform examples validate with local provider dev override and `TMPDIR=$PWD/tmp`:
  - `examples/terraform`
  - `examples/terraform-aws-supplemental`
  - `examples/terraform-localstack`
- [x] Terraform examples validate with provider `0.1.0-alpha.5` installed from
  the provider GitHub release as a filesystem mirror:
  - `source scripts/setup-provider-github-mirror.sh`
  - `terraform -chdir=examples/terraform init -upgrade -input=false`
  - `terraform -chdir=examples/terraform validate`
  - `terraform -chdir=examples/terraform-aws-supplemental init -upgrade -input=false`
  - `terraform -chdir=examples/terraform-aws-supplemental validate`
  - `terraform -chdir=examples/terraform-localstack init -upgrade -input=false`
  - `terraform -chdir=examples/terraform-localstack validate`
- [x] Release build script generates artifacts:
  - `BETTERNAT_VERSION=v0.1.0-alpha.test BETTERNAT_RELEASE_DIR=$PWD/tmp/release-test scripts/release-build.sh`
  - `SHA256SUMS` includes host CLI, provider, Linux arm64/amd64 agent, and Linux arm64/amd64 CLI artifacts.
  - final 2026-06-24 release verification passed:
    `BETTERNAT_RELEASE_DIR=$PWD/tmp/release-final-check scripts/release-build.sh`
- [x] Release URL smoke passed for current public runtime artifacts:
  - `BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-url-smoke.sh`
  - `BETTERNAT_VERSION=v0.1.0-alpha.2 BETTERNAT_SMOKE_ARCH=amd64 scripts/release-url-smoke.sh`
- [x] Release deploy plan-only smoke passed with provider GitHub mirror:
  - run id `bnat-final-plan-alpha3-20260624-profile`
  - provider `0.1.0-alpha.3`
  - runtime `v0.1.0-alpha.2`
  - Terraform plan: `16` creates, `0` changes, `0` destroys
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
- [x] Linux VM LoxiLB smoke is classified as an environment limitation in
  OrbStack:
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
GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat
source scripts/setup-provider-github-mirror.sh
terraform -chdir=examples/terraform init -upgrade -input=false
terraform -chdir=examples/terraform validate
terraform -chdir=examples/terraform-aws-supplemental init -upgrade -input=false
terraform -chdir=examples/terraform-aws-supplemental validate
terraform -chdir=examples/terraform-localstack init -upgrade -input=false
terraform -chdir=examples/terraform-localstack validate
```

Notes:

- Provider `0.1.0-alpha.5` is currently installed from the provider GitHub
  release as a Terraform filesystem mirror because Terraform Registry
  propagation is not complete.
- The mirror path is a temporary alpha workaround; once Registry
  propagation catches up to the current provider release, examples can return
  to plain Registry install.

### 3. Linux VM Smoke Tests

Required before alpha:

- nftables datapath smoke,
- nftables UDP smoke,
- nftables throughput sanity,
- agent metrics with nftables fallback,
- LoxiLB smoke if the local Linux environment supports it; otherwise AWS LoxiLB
  validation is the authoritative alpha datapath proof.

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
- The 2026-06-24 route-only/non-stable handover comparison is recorded in
  `docs/research/040-alpha-low-cost-soak-results.md`: `240` client samples, `0`
  failures, and a visible public source-IP switch in about `435 ms`. The
  release conclusion is that non-stable handover can be much faster than stable
  shared-EIP handover because it avoids EIP reassociation, but it is only
  appropriate when changing public egress IP is acceptable.

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

Current:

```sh
betternat support bundle --config /etc/betternat/agent.json --output /tmp/betternat-support.tgz
```

Bundle:

- redacted config,
- CLI status output,
- datapath ready output,
- metrics snapshot,
- recent agent logs,
- daemon status and handover summaries,
- LoxiLB state,
- local network snapshots.

Acceptance:

- [x] redacts obvious secrets,
- [x] does not include Terraform state by default,
- [x] documented in operations guide,
- [x] unit coverage verifies peer API token redaction.

### 2. HA Status Command

Current:

```sh
betternat status
betternat status --watch --interval 2s
```

The dedicated `ha status` command is no longer needed for alpha because the
daemon-backed `status` command now covers the intended HA view.

Implemented status view:

- local instance id,
- lease owner,
- active/standby state,
- route target match,
- EIP match,
- datapath ready,
- metrics freshness,
- public IP,
- version per node,
- lightweight RX/TX bandwidth.

Acceptance:

- [x] default command reads `/etc/betternat/agent.json`,
- [x] daemon-backed by default with `--direct` debug fallback,
- [x] supports `--watch`,
- [x] supports JSON output for scripts,
- [x] table output is borderless and pipe-friendly,
- [x] documented in operations guide.

### 3. Agent Registry Coordination Backend

Current:

- `betternat status` can aggregate gateway instances through AWS ASG/EC2 discovery plus metrics scraping.
- This is useful for alpha validation but too AWS-specific and permission-heavy as the long-term CLI/status path.

Planned:

- [x] Add a provider-neutral coordination backend abstraction.
- [x] On AWS, use DynamoDB as the first coordination backend implementation.
- [x] Move the target AWS shape to one coordination table keyed by `ha_group_id` and `record_id`.
- [x] Store fenced HA ownership in `record_id=lease`.
- [x] Store agent registry records in `record_id=agent#<node_id>`.
- [x] Reserve the same table for future message, drain, and upgrade-intent records.
- [x] Have each agent register its own identity, version, private IP, metrics URL, HA state, datapath readiness, and freshness TTL.
- [x] Add provider-owned infrastructure reconciliation so provider upgrades can create the coordination table and replace the BetterNAT-managed `betternat-runtime` IAM policy in place.
- [x] Make `betternat status` registry-first:
  - read lease owner,
  - query fresh agent records,
  - scrape each agent's metrics URL,
  - render active/standby, versions, IPs, and traffic.

Reference:

- `docs/research/038-agent-registry-control-plane-plan.md`

Acceptance:

- [x] `betternat status` does not require `autoscaling:DescribeAutoScalingGroups` or `ec2:DescribeInstances` in normal operation.
- [x] stale records disappear after TTL.
- [x] graceful shutdown removes the local record on clean agent exit.
- [x] route/EIP mutation safety remains tied to lease generation, not registry state.
- [x] provider upgrade can add/remove BetterNAT-managed IAM permissions and create provider-owned coordination resources without replacing gateway nodes or mutating route/EIP ownership.

AWS validation on 2026-06-23:

- Test environment: `bnat-lifecycle-20260623023753`, `us-west-2a`, all gateway nodes on Spot.
- Coordination table created: `betternat-bnat-lifecycle-20260623023753-coordination`.
- Runtime IAM policy updated in place:
  - added `dynamodb:Query`,
  - removed `autoscaling:DescribeAutoScalingGroups`,
  - removed `ec2:DescribeInstances`.
- First rolling update to `v0.1.0-alpha.coord-20260623055746` exposed a registry-refresh bug: active agent registry publishing could stall when datapath status sampling blocked.
- Fixed by timing out registry datapath status sampling and still publishing the agent record with `datapath_ready=false` when the sample is unavailable.
- Second rolling update to `v0.1.0-alpha.coordfix-20260623061100` succeeded:
  - ASG instance refresh `cc201797-ecc0-4503-81ac-9001e0ffa376`,
  - status `Successful`,
  - active `i-0eea9f6ca48b64e7a`,
  - standby `i-045bcb415c357b5e8`,
  - route `0.0.0.0/0` points at `i-0eea9f6ca48b64e7a`,
  - shared EIP `52.24.117.43` is associated with `i-0eea9f6ca48b64e7a`,
  - `betternat status --output json` reports both instances with version `v0.1.0-alpha.coordfix-20260623061100`, roles `active` and `standby`, and `metrics:"ok"`.
- `doctor --live` on the active instance passed IAM, lease, route, EIP, source/destination check, Prometheus, and source-IP probe checks with the reduced runtime policy. Remaining warnings were expected:
  - rollback route targets are not captured yet,
  - ASG discovery is skipped because coordination registry is configured.
- Revalidated on 2026-06-23 after the daemon/handover design update:
  - ASG desired capacity is 2 and both gateway instances are `InService` and `Healthy`,
  - coordination table contains two `agent#...` records plus `lease`,
  - lease owner, route target, and shared EIP owner all match `i-0eea9f6ca48b64e7a`,
  - `sudo betternat status --output json` succeeds from both gateway instances and reports the same active/standby view,
  - `sudo betternat doctor --live --config /etc/betternat/agent.json` succeeds on the active instance with the same expected warnings.

### 4. Agent Daemon API For Fast CLI

Current:

- `betternat status` is registry-first and no longer needs ASG/EC2 discovery permissions.
- Phase 1 local daemon API is implemented in code:
  - `betternat-agent` starts a Unix socket control API at `/run/betternat/agent.sock` when possible,
  - `GET /v1/status` serves a cached status response,
  - the daemon refreshes registry and peer-metrics state in the background,
  - `betternat status` calls the daemon by default,
  - `betternat status --direct --config /etc/betternat/agent.json` keeps the old debug path.

Remaining:

- add richer daemon endpoints for datapath, failover, doctor, config, refresh, peers, drain, and handover,
- decide whether post-alpha read-only socket access remains root-only or uses a
  `betternat` group.

Reference:

- `docs/research/039-agent-daemon-api-and-handover-plan.md`

Acceptance:

- [x] `betternat status` returns from local cached daemon state without creating DynamoDB clients or scraping peers in the CLI process.
- [x] status output marks stale cached data clearly.
- [x] daemon API remains separate from Prometheus metrics.
- [x] local socket access is restricted to root or a BetterNAT operator group.
- [x] AWS runtime artifact published and verified with daemon-backed status.
- [x] status table output is pipe-friendly and reports the active shared public IP.

AWS validation on 2026-06-23:

- Published `v0.1.0-alpha.daemon-20260623073549` runtime artifacts to the existing alpha artifact bucket.
- Created launch template version `7` and updated ASG `betternat-bnat-lifecycle-20260623023753-us-west-2a` to use it.
- First refresh against the old ASG-pinned launch template version was harmless but still produced LT `6` instances; after updating the ASG launch template version explicitly, refresh `4ac19c25-64b3-4a4c-ac5b-a431c19770ba` completed successfully.
- New gateway instances:
  - active `i-0cf9c5b33ceca117d`,
  - standby `i-0e2091a2b696bdee5`,
  - both `InService`, `Healthy`, and LT `7`.
- `sudo test -S /run/betternat/agent.sock` passed on both instances.
- Default `sudo betternat status --output json` returned daemon API `schema_version:"v1"` and fresh cache metadata on both instances.
- `sudo betternat status --direct --config /etc/betternat/agent.json --output json --sample 0s` still worked as the direct fallback.
- Route `0.0.0.0/0` and shared EIP `52.24.117.43` both pointed at active `i-0cf9c5b33ceca117d`.
- `doctor --live` on active passed IAM, lease, route, EIP, source/destination check, Prometheus, and source-IP probe with the same expected warnings.

Status UX follow-up validation on 2026-06-23:

- Published `v0.1.0-alpha.statusux-20260623081543`.
- Created launch template version `9`.
- Instance refresh `e2d822ce-979d-4603-b5cc-2e6a24aeec1c` completed successfully.
- New gateway instances:
  - active `i-0095956b45e4462f5`,
  - standby `i-07884bbdd4a162a9a`,
  - both `InService`, `Healthy`, and LT `9`.
- Default `sudo betternat status` on both instances used borderless aligned columns instead of box-drawing table borders.
- Default `sudo betternat status --output json` reported shared public IP `52.24.117.43` in the summary and active instance row.
- Live SSM recheck command `6f56c079-7b8e-45d5-a6f0-ff90cb2edff0` confirmed the same output from both gateway instances.
- Route `0.0.0.0/0` and shared EIP `52.24.117.43` both pointed at active `i-0095956b45e4462f5`.
- `doctor --live` on active passed key live checks with the same expected warnings.

### 5. Proactive Handover

Current:

- Hard-failure takeover is lease-expiry based.
- Manual active-local proactive handover is implemented in code:
  - `lease.Transfer` moves ownership only when owner, generation, and expiry conditions still match,
  - active daemon `POST /v1/handover` validates that the local daemon is active and the target is a healthy fresh standby from daemon cache,
  - durable `handover#<request_id>` records are written to the coordination table for requested, preparing, committing, completed, failed, and rejected outcomes,
  - duplicate `request_id` submissions return the existing durable handover state instead of creating a competing operation,
  - standby-local handover requests can forward to the active daemon when the peer control API is configured,
  - authenticated peer prepare requests require a Bearer token and the standby verifies that the requester is the current active lease owner before accepting prepare,
  - systemd stop, ASG lifecycle termination, and Spot interruption events trigger automatic handover before local supervisor shutdown when the coordination registry is configured,
  - controller moves EIP/route to target, verifies cloud ownership, then transfers the lease,
  - if lease transfer fails after cloud mutation, controller attempts to revert EIP/route to the source,
  - `betternat handover start --to auto|<instance-id>` calls the daemon; there is no direct mutation fallback.

Remaining:

- add richer `/v1/handover` state reporting and operation listing,
- add structured phase-duration metrics and logs for handover operations,
- add AWS validation for standby-local CLI request forwarding.
- Spot interruption follows the AWS IMDS `spot/instance-action` path and is
  not required as a manually forced release gate; optional validation can use
  AWS-supported Spot interruption initiation or FIS when available.
- stale automatic handover operation records are filtered and best-effort
  deleted; `handover history --include-stale` remains available for raw support
  evidence.

Reference:

- `docs/research/039-agent-daemon-api-and-handover-plan.md`

Acceptance:

- [x] graceful handover is faster than waiting for lease expiry.
- [x] route/EIP mutation remains fenced by the current lease owner and generation.
- [x] if lease transfer fails after cloud mutation, active attempts to revert route/EIP before returning failure.
- [x] hard-crash passive failover continues to work.
- [x] AWS handover validation completed.
- [x] duplicate handover request IDs are idempotent.
- [x] standby peer prepare rejects non-active requesters.
- [x] AWS ASG lifecycle handover validation completed.
- [x] Standalone systemd-stop automatic handover validation completed.
- [x] Spot interruption handling follows the documented AWS IMDS path; forced
      AWS interruption validation is not a first-alpha release gate.

AWS validation on 2026-06-23:

- Published `v0.1.0-alpha.handover-20260623080252`.
- Created launch template version `8` and refreshed ASG with refresh `7117f616-2132-4d74-a513-c2f50bbd71d6`; refresh completed successfully.
- Pre-handover state:
  - active `i-056fcf70d3dc3061c`,
  - standby `i-09360439fc37ef0d0`,
  - both `InService`, `Healthy`, and LT `8`.
- Ran `sudo betternat handover start --to auto --reason manual-aws-validation --output json` on active.
- CLI command output reported:
  - status `completed`,
  - source `i-056fcf70d3dc3061c`,
  - target `i-09360439fc37ef0d0`,
  - lease generation `2`.
- Command-internal elapsed time was about `2.8s`; SSM outer wall time was about `10.6s`.
- Post-handover verification:
  - coordination lease owner is `i-09360439fc37ef0d0`, generation `2`,
  - private route `0.0.0.0/0` points at `i-09360439fc37ef0d0`,
  - shared EIP `52.24.117.43` is associated with `i-09360439fc37ef0d0`,
  - both daemon status views agree on new active/standby roles,
  - `doctor --live` on the new active passed IAM, lease, route, EIP, source/destination check, Prometheus, and source-IP probe with the same expected warnings.

Local implementation follow-up on 2026-06-23:

- Added coordination-table durable handover records keyed by `handover#<request_id>`.
- Added request-id idempotency for daemon handover requests.
- Added standby request forwarding to the active peer control URL when peer auth is configured.
- Added authenticated peer control API support using Bearer token checks.
- Added peer prepare validation so the standby reads the current lease and rejects prepare from non-active requesters or stale generations.
- Added automatic handover trigger paths for:
  - systemd stop / SIGTERM context cancellation,
  - ASG lifecycle termination events from IMDS,
  - Spot interruption events from IMDS.
- Terraform provider now generates a random sensitive `peer_api_auth_token`, stores it in state, renders peer API config into provider-managed agent config, and bumps the provider infrastructure revision to trigger safe in-place reconciliation.
- Local validation passed:
  - `GOCACHE=$PWD/tmp/go-build go test ./...`,
  - `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat`.
- Follow-up AWS automatic-trigger validation is recorded below.

AWS automatic-trigger and AMI validation on 2026-06-23:

- Built and boot-smoked private dev AMI `ami-072757363df299006` from the Packer
  AL2023 Docker path.
- The AMI bakes BetterNAT `v0.1.0-alpha.2` and pinned LoxiLB
  `ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052`.
- Launch template `lt-0e610263c6ef023f7` version `15` uses the AMI and an
  AMI-mode user-data script that only writes `/etc/betternat/agent.json`,
  applies sysctls, and starts baked services.
- ASG instance refresh `c7c091e4-63b6-4895-a160-ef75f7113a6f` completed
  successfully from `2026-06-23T18:27:10Z` to `2026-06-23T18:29:40Z`.
- Final gateway nodes:
  - active `i-04a1815ed94a74088`, LT `15`, AMI `ami-072757363df299006`,
  - standby `i-0a4ccdacb96d4ab07`, LT `15`, AMI `ami-072757363df299006`.
- During the refresh, the ASG lifecycle-triggered operation
  `termination-i-0e1486d3bb0920c6f-autoscaling-target-lifecycle-state-Terminated-betternat-bnat-lifecycle-20260623023753-us-west-2a-terminating`
  completed and handed over to `i-04a1815ed94a74088`.
- Manual proactive handover was revalidated on the AMI nodes:
  - `i-04a1815ed94a74088 -> i-0a4ccdacb96d4ab07`, generation `6`,
  - `i-0a4ccdacb96d4ab07 -> i-04a1815ed94a74088`, generation `7`.
- A client egress probe from `i-0ec999731bb6cb25b` during the second manual
  handover recorded `240` samples at about `250ms` spacing with `0` failed
  samples.
- Finding: this temporary environment still assigns per-node public IPv4
  addresses to gateway nodes. During handover, `5` successful probe samples
  exited through non-shared public IPs between `2026-06-23T18:32:39.520Z` and
  `2026-06-23T18:32:46.896Z`. The production AMI path must remove per-node
  public IP assignment or otherwise prevent non-shared egress identity leakage.
- Follow-up implemented on 2026-06-24: expired handover records are filtered and
  best-effort deleted by the DynamoDB coordination backend. `betternat handover
  history` also hides stale non-terminal records from older lease generations by
  default while retaining `--include-stale` for support evidence collection.

AWS documentation basis:

- EC2 Spot interruption notices are exposed through instance metadata and AWS
  recommends checking every 5 seconds:
  https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-instance-termination-notices.html
- EC2 Auto Scaling target lifecycle state is exposed through instance metadata
  at `autoscaling/target-lifecycle-state`:
  https://docs.aws.amazon.com/autoscaling/ec2/userguide/retrieving-target-lifecycle-state-through-imds.html
- Auto Scaling lifecycle hooks give instances time to complete custom actions
  before transitioning:
  https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html

### 6. Prometheus Alert Rules

Implemented:

```text
examples/prometheus/betternat-alerts.yaml
```

Rules:

- no active node,
- HA status stale,
- route target mismatch,
- EIP mismatch,
- datapath not ready,
- lease renew errors,
- repeated takeover attempts.

Acceptance:

- syntax checked with `promtool` if available,
- documented in observability guide.

### 7. Grafana Starter Dashboard

Implemented:

```text
examples/grafana/betternat-starter-dashboard.json
```

Minimum panels:

- active owner,
- standby health,
- processed bytes,
- owner bytes,
- conntrack entries,
- route/EIP match,
- failover attempts/success.

### 8. Bootstrap-First Production Packaging

For alpha and the first production-preview release, a documented bootstrap path
is the intended release path. Public BetterNAT AMIs remain optional because each
published AMI version and region creates ongoing EBS snapshot-retention cost.

For production-preview, improve:

- provider/user workflow for selecting the current runtime release artifact
  URLs and checksums,
- clear bootstrap dependency documentation,
- explicit base Linux AMI requirements,
- rollback/replacement guidance when bootstrap inputs change,
- smoke validation for the bootstrap path.

Optional AMI acceleration path, if the project later accepts the ongoing cost:

- Packer or EC2 Image Builder definition,
- AMI naming/versioning,
- arm64 and amd64 builds or explicit architecture scope,
- AMI file listing validation,
- license files installed into AMI.

Current implementation:

- `packer/betternat.pkr.hcl` defines an Amazon EBS AMI build from Amazon Linux
  2023.
- `scripts/ami/provision-betternat-ami.sh` installs BetterNAT binaries, Docker,
  nftables, conntrack tools, a pinned LoxiLB image, `loxicmd`, systemd units,
  and baseline sysctls.

Still needed for the optional AMI path:

- publish arm64 and amd64 AMIs,
- wire provider `ami_channel` resolution to published AMI metadata,
- add AMI license bundle validation,
- convert the temporary private AWS API reachability requirements into durable
  provider/user documentation before AMI publication.

When `stable_egress_ip=true`, the provider-derived plan now sets
`AssociatePublicIpAddress=false`. When `stable_egress_ip=false`, nodes may keep
per-node public IPv4 addresses because failover is allowed to change the public
source IP. The no-public-IP standby bootstrap/control-plane path has been
validated with temporary VPC endpoints in the retained AWS test environment.
The durable production path must cover DynamoDB, EC2, Auto Scaling, STS, SSM,
EC2 Messages, SSM Messages, and CloudWatch where enabled.

AWS no-public-IP validation on 2026-06-23:

- Added temporary VPC endpoints to the retained test VPC:
  - DynamoDB gateway endpoint,
  - EC2, Auto Scaling, STS, SSM, SSM Messages, and EC2 Messages interface
    endpoints.
- Created launch template version `16` from version `15` with
  `AssociatePublicIpAddress=false`.
- ASG instance refresh `2cf3c2c8-2381-4e4b-976f-3fe55b728aa0` completed
  successfully from `2026-06-23T19:00:30Z` to `2026-06-23T19:03:42Z`.
- Final no-public-IP gateway state:
  - active `i-0cf0b4eb48268a08b`, private `10.88.1.39`, public
    `52.24.117.43` from the shared EIP only,
  - standby `i-02c90f1f8314ca8ab`, private `10.88.1.12`, no public IP.
- `betternat status` reported both nodes `Healthy`, metrics `ok`, and control
  `ok`.
- Manual proactive handover
  `i-02c90f1f8314ca8ab -> i-0cf0b4eb48268a08b` completed at generation `11`.
- Client egress probe during handover recorded `240` samples, `3` one-second
  curl timeouts, longest consecutive failure run `2`, and `0` non-shared public
  IP samples.
- Temporary VPC endpoints were retained with the manual test environment so
  no-public-IP standby nodes keep private AWS API reachability.

AWS non-stable public-IP validation on 2026-06-24:

- Created launch template version `17` from version `16` with
  `AssociatePublicIpAddress=true` and an agent config without
  `ha.public_identity`.
- ASG instance refresh `824ec267-c1a2-47a8-b363-a04d57974c66` completed
  successfully from `2026-06-23T19:11:01Z` to `2026-06-23T19:13:41Z`.
- Agent config on active reported `ha.public_identity=null`; runtime was
  route-only / non-stable mode.
- Manual proactive handover
  `i-0a89f292e07b04460 -> i-0d08059b2f4708db6` completed at generation `15`.
- Client egress probe during handover recorded `240` samples, `0` failures,
  and the expected public source IP change from `52.24.117.43` to
  `52.24.240.255`.
- Timing conclusion: non-stable route-only handover was materially faster than
  stable EIP handover in this AWS probe. The last old-IP sample was
  `2026-06-24T02:06:34.767Z`; the first new-IP sample was
  `2026-06-24T02:06:35.202Z`, so the visible switch window was about `435 ms`
  at the probe's sampling granularity. This is expected because non-stable mode
  avoids EIP reassociation and public-identity verification. The tradeoff is
  that public source IP changes after handover.
- The environment was restored to stable/no-public-IP launch template version
  `16` with instance refresh `1574c0a3-a7cd-4c8b-a5b7-5077f7ab5a89`, which
  completed successfully from `2026-06-24T02:09:29Z` to
  `2026-06-24T02:12:42Z`.
- Final retained gateway state:
  - active `i-048fd34e26867122f`, private `10.88.1.135`, shared EIP
    `52.24.117.43`,
  - standby `i-073ab0073edde40ba`, private `10.88.1.85`, no public IP.

AWS low-cost soak and systemd-stop validation on 2026-06-24:

- Used retained stable/no-public-IP environment with ASG
  `betternat-bnat-lifecycle-20260623023753-us-west-2a`, launch template
  version `16`, shared EIP `52.24.117.43`, and private client
  `i-0ec999731bb6cb25b`.
- Ran `2400` private-client egress probe samples while restarting the standby
  agent, running a manual proactive handover, and restarting LoxiLB on the
  active node. Result: `2396` ok, `4` failed, `0` unexpected public IP samples,
  longest consecutive failure run `1`, and no public IP switches.
- Directly restarted active `betternat-agent` on `i-048fd34e26867122f`.
  Durable handover record `systemd-stop-1782271270264168584` completed from
  `i-048fd34e26867122f` to `i-073ab0073edde40ba` at generation `20`.
- A short client probe during active systemd restart recorded `360` samples,
  `359` ok, `1` failed, `0` unexpected public IP samples, and no public IP
  switches.
- Final state: daemon status from both gateway nodes agreed that
  `i-073ab0073edde40ba` was active, route and shared EIP both pointed at
  `i-073ab0073edde40ba`, and both nodes were healthy.
- Detailed evidence:
  `docs/research/040-alpha-low-cost-soak-results.md`

### 9. Advanced Kernel/NIC Tuning Profile

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
- `doctor --live` is now implemented for node-local cloud checks; static rollback-config warning needs P1 UX polish.
- `betternat-agent` handles SIGTERM/SIGINT and releases its currently owned HA lease on graceful shutdown using the fenced lease generation.
- Provider-created ASG termination lifecycle hooks and agent-side IMDS Spot/ASG termination handling are implemented. ASG lifecycle behavior and standalone active systemd-stop handover have been verified in AWS; forced Spot interruption validation is not a first-alpha release gate.
- Current CLI fleet status uses the daemon/coordination-registry path by default; the older direct AWS discovery mode remains a debug fallback.
- Release packaging now has a bootstrap artifact path; AMI packaging remains the biggest production-release workflow gap.
- The first release should avoid making unproven performance claims.

## Suggested Implementation Order

1. Re-run final local Go/Terraform/release/hygiene verification before tagging
   the next alpha.
2. Recheck Terraform Registry availability for the current provider release; remove
   the GitHub filesystem-mirror workaround from public docs once Registry
   propagation catches up.
3. Decide whether to publish another runtime tag for post-RC source changes or
   keep `v0.1.0-alpha.2` as the current runtime artifact set.
4. Treat bootstrap-first install UX polish, especially runtime artifact
   URL/checksum selection, as the remaining production-preview blocker. Keep
   public AMI publication, `ami_channel` resolution, runtime artifact signing,
   benchmark harness work, and broader retry/backoff hardening as follow-up
   production hardening rather than release blockers.
