# BetterNAT Release Checklist

Date: 2026-06-24

## Purpose

This checklist defines what must be true before publishing BetterNAT releases.

It separates:

- **v0.1.0-alpha / private preview**: usable by technical early adopters in disposable or non-critical AWS environments.
- **production-ready release**: credible for real private-subnet egress workloads where route/EIP failover affects application availability.

This document complements:

- `docs/release/AMI_RELEASE_PLAN.md`
- `docs/testing/AWS_SUPPLEMENTAL_TEST_PLAN.md`
- `docs/testing/AWS_SUPPLEMENTAL_RUNBOOK.md`
- `docs/research/031-aws-low-cost-supplemental-results.md`
- `docs/research/035-p0-open-source-release-acceptance-results.md`
- `docs/research/037-v0.1.0-alpha-aws-release-candidate-results.md`
- `docs/research/032-failover-stability-industry-patterns.md`

## Release Levels

### v0.1.0-alpha

Goal:

> A technical user can deploy BetterNAT in AWS with Terraform, verify egress, observe HA state, test failover, and destroy all resources.

Allowed limitations:

- no NAT Gateway equivalent SLA,
- no active connection preservation,
- no high-volume benchmark claim,
- AWS only,
- single-AZ HA group only,
- no published BetterNAT AMI in the first alpha,
- install path is Terraform plus cloud-init bootstrap on an explicit Linux AMI,
- docs may require technical AWS familiarity.

Not allowed:

- unclear cleanup path,
- hidden public SSH requirement,
- provider creates resources that it cannot destroy,
- failover requires manual `AssociateAddress` or `ReplaceRoute`,
- release artifacts are not versioned,
- examples depend on local-only paths or presigned URLs.

### Production-Ready

Goal:

> BetterNAT can be recommended for production private-subnet egress where users accept self-managed appliance tradeoffs and new-connection failover semantics.

Production release requires:

- published AMIs,
- stable Terraform provider release,
- complete user documentation,
- documented IAM/security model,
- default HA timing,
- rollback and recovery documentation,
- repeatable AWS acceptance tests,
- documented limitations and SLO language.

## v0.1.0-alpha Checklist

### 1. Product Scope

- [x] README says BetterNAT is alpha/private preview.
- [x] README states AWS-only support.
- [x] README states single-AZ HA group support.
- [x] README states new connections recover after failover; active connections may reset.
- [x] README states no NAT Gateway equivalent SLA.
- [x] README states high-volume cost savings are modeled but not proven by large-transfer tests.

Evidence:

- `README.md`
- `docs/spec-v0.md`
- `docs/README.md`

### 2. Terraform Provider

- [x] Provider builds locally:

```sh
go build -o terraform-provider-betternat ./cmd/terraform-provider-betternat
```

- [x] Provider exposes required v0 UX:
  - [x] `name`
  - [x] `region`
  - [x] `vpc_id`
  - [x] `public_subnet_ids`
  - [x] `private_route_table_ids`
  - [x] `private_cidrs`
  - [x] `instance_type`
  - [x] `use_spot`
  - [x] `min_size`
  - [x] `desired_capacity`
  - [x] `max_size`
  - [x] `stable_egress_ip`
  - [x] `ha_profile`
  - [x] `ha_lease_ttl_seconds`
  - [x] `ha_renew_interval_seconds`
  - [x] `prometheus_enabled`
  - [x] `rollback_on_destroy`

- [x] Capacity-only updates are in-place.
- [x] Non-capacity updates require replacement or are explicitly supported.
- [x] Terraform examples validate:

```sh
source scripts/setup-provider-github-mirror.sh
terraform -chdir=examples/terraform init -upgrade -input=false
terraform -chdir=examples/terraform validate
terraform -chdir=examples/terraform-aws-supplemental init -upgrade -input=false
terraform -chdir=examples/terraform-aws-supplemental validate
terraform -chdir=examples/terraform-localstack init -upgrade -input=false
terraform -chdir=examples/terraform-localstack validate
```

Evidence:

- `internal/tfprovider/gateway_resource.go`
- `internal/tfprovider/gateway_resource_test.go`
- `examples/terraform/`
- `examples/terraform-aws-supplemental/`
- `examples/terraform-localstack/`

### 3. Agent Runtime

- [x] `betternat-agent` builds for Linux arm64.
- [x] `betternat-agent` builds for Linux amd64.
- [x] agent loads `/etc/betternat/agent.json`.
- [x] agent starts metrics endpoint.
- [x] agent disables source/destination check on AWS.
- [x] agent reconciles LoxiLB SNAT.
- [x] agent supports nftables fallback path.
- [x] agent runs decentralized HA supervisor.
- [x] stale HA metrics do not report false active.
- [x] HA step timeouts prevent stuck SDK/datapath calls from freezing the loop.

Commands:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o tmp/release/betternat-agent-linux-arm64 ./cmd/betternat-agent
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tmp/release/betternat-agent-linux-amd64 ./cmd/betternat-agent
```

Evidence:

- `internal/agent/`
- `internal/ha/`
- `internal/metrics/`
- `internal/datapath/`

### 4. CLI And Diagnostics

- [x] `betternat` CLI builds for local dev.
- [x] `betternat doctor` exists and documents current checks.
- [x] `betternat datapath ready` works for local/AMI use.
- [x] CLI output avoids requiring users to inspect raw logs for basic health.

Commands:

```sh
go build -o betternat ./cmd/betternat
go test ./internal/cli ./internal/doctor
```

Evidence:

- `internal/cli/`
- `internal/doctor/`

### 5. Bootstrap Packaging

Alpha minimum:

- [x] Release notes explicitly state that `v0.1.0-alpha.2` does not publish a BetterNAT AMI.
- [x] Release clearly uses an explicit `ami_id` with cloud-init bootstrap.
- [x] Recommended alpha fixture uses the latest official Amazon Linux 2023 arm64 AMI unless the user overrides `ami_id`.
- [x] Bootstrap downloads and verifies `betternat-agent`.
- [x] Bootstrap downloads and verifies `betternat` CLI.
- [x] Bootstrap starts LoxiLB runtime.
- [x] Bootstrap installs or wraps `loxicmd`.
- [x] Bootstrap writes `/etc/betternat/agent.json`.
- [x] Bootstrap installs and starts `betternat-agent.service`.
- [x] Bootstrap applies baseline sysctl tuning:
  - [x] `net.ipv4.ip_forward = 1`,
  - [x] `net.ipv4.conf.all.rp_filter = 0`,
  - [x] `net.ipv4.conf.default.rp_filter = 0`,
  - [x] conditional `net.netfilter.nf_conntrack_max = 1048576` when the kernel exposes the sysctl.
- [x] Release notes state that advanced performance tuning is not yet claimed or benchmarked.
- [x] Public alpha binaries are attached to a GitHub Release:
  - [x] Linux arm64 `betternat-agent`
  - [x] Linux amd64 `betternat-agent`
  - [x] Linux arm64 `betternat`
  - [x] Linux amd64 `betternat`
  - [x] Terraform provider binary or documented Registry install path
  - [x] `SHA256SUMS`
  - [x] `manifest.json`
- [x] User-facing install docs use GitHub Release asset URLs, not S3.
- [x] GitHub Release asset URLs return HTTP 200 before tagging the release as ready.
- [x] Checksums in `SHA256SUMS` match uploaded release assets.

Release artifact validation recorded on 2026-06-22:

- GitHub Release: `v0.1.0-alpha.1`.
- Release workflow: https://github.com/nowakeai/betternat/actions/runs/27931536630.
- Assets verified with HTTP 200: Linux arm64/amd64 `betternat-agent`, Linux arm64/amd64 `betternat`, Linux amd64 legacy provider binary, `SHA256SUMS`, and `manifest.json`.
- Downloaded assets passed `shasum -a 256 -c SHA256SUMS`.

Explicitly deferred from first alpha:

- published BetterNAT AMI,
- AMI channel resolver,
- AMI boot-to-ready SLO,
- baked LoxiLB image,
- baked license bundle inside the AMI,
- advanced kernel/NIC tuning profile.

Production requirement:

- [ ] AMI is the primary path.
- [ ] AMI is built by a repeatable Packer or EC2 Image Builder pipeline.
- [ ] AMI names include version, date, arch, and base OS.
- [ ] arm64 and x86_64 AMIs are published or explicitly scoped.
- [ ] `ami_channel` resolves to real AMI IDs.

Evidence:

- `docs/release/ALPHA_BOOTSTRAP_RELEASE_PATH.md`
- `docs/user/RELEASE_NOTES_v0.1.0-alpha.2.md`
- `internal/bootstrap/bootstrap.go`
- `internal/bootstrap/bootstrap_test.go`
- `docs/release/AMI_RELEASE_PLAN.md`
- P0 AWS bootstrap acceptance result

### 6. AWS Acceptance Tests

Alpha minimum must pass in isolated `us-west-2a` test VPC:

- [x] Terraform apply creates isolated VPC fixture.
- [x] ASG reaches two healthy appliances.
- [x] private client egress works.
- [x] stable EIP mode baseline returns fixed EIP.
- [x] non-stable mode baseline returns active instance public IP.
- [x] owner termination causes standby takeover.
- [x] ASG launches replacement.
- [x] replacement joins as standby.
- [x] route target matches current lease owner.
- [x] EIP target matches current lease owner when `stable_egress_ip=true`.
- [x] final client egress works after failover.
- [x] Terraform destroy succeeds.
- [x] temporary artifact bucket is deleted when internal AWS tests use S3 for unreleased binaries.
- [x] residual resource scan is empty:
  - [x] VPC
  - [x] EIP
  - [x] ENI
  - [x] EBS volume
  - [x] ASG
  - [x] DynamoDB table
  - [x] S3 bucket used by internal test fixture

Already proven by low-cost supplemental runs:

- `bnat-20260620182614`: stable EIP mode.
- `bnat-20260620191841`: non-stable egress mode.
- `bnat-p0-20260621044411`: bootstrap release artifact path, appliance-local `doctor --live`, IAM negative test, private egress, and cleanup.
- 2026-06-24 retained-environment comparison: non-stable route-only proactive
  handover recorded `240` client samples, `0` failures, and a visible public
  source-IP switch from `52.24.117.43` to `52.24.240.255` in about `435 ms` at
  client probe sampling granularity. This is materially faster than the stable
  shared-EIP path because it skips EIP reassociation and public-identity
  verification, but the public source IP changes by design.

Must repeat before alpha if release artifacts differ from the tested build:

- [x] provider binary changed after last AWS test; covered by the 2026-06-21 release-candidate run.
- [x] agent binary changed after last AWS test; covered by the 2026-06-21 release-candidate run.
- [x] AMI/bootstrap changed after last AWS test; cloud-init bootstrap covered by the 2026-06-21 release-candidate run. Production AMI remains deferred.
- [x] Terraform fixture changed after last AWS test; covered by the 2026-06-21 release-candidate run.

Evidence:

- `docs/research/031-aws-low-cost-supplemental-results.md`
- `docs/research/035-p0-open-source-release-acceptance-results.md`
- `docs/research/037-v0.1.0-alpha-aws-release-candidate-results.md`
- `docs/research/040-alpha-low-cost-soak-results.md`
- fresh AWS run logs under ignored `tmp/aws-alpha-results/`
- Terraform apply/destroy output
- AWS residual scan output

### 7. Local Test Suite

- [x] Full Go test suite passes:

```sh
GOCACHE=$PWD/tmp/go-build-cache go test ./...
```

- [x] Diff whitespace check passes:

```sh
git diff --check
```

- [x] Terraform examples validate with provider `0.1.0-alpha.4` installed from
  the provider GitHub release as a filesystem mirror.
- [x] LocalStack expectations are documented, including current ASG limitation.

Evidence:

- command output
- `docs/dev/TERRAFORM_PROVIDER_LOCAL_TESTING.md`

### 8. Security

Alpha minimum:

- [x] No secrets or presigned URLs committed.
- [x] No local absolute paths committed.
- [x] SSM is the default access path.
- [x] No inbound public SSH in default examples.
- [x] IMDSv2 is required where provider controls EC2 metadata options.
- [x] IAM policy is scoped to BetterNAT-created resources where currently practical.
- [x] Required AWS actions are documented.

Suggested scans:

```sh
rg -n "X-Amz|AWSAccessKeyId|BEGIN (RSA|OPENSSH|EC|PRIVATE) KEY|BETTERNAT_AGENT_BINARY_URL=|/Users/|/mnt/mac/" . --glob '!tmp/**' --glob '!.git/**'
```

Production requirement:

- [ ] least-privilege IAM reviewed.
- [x] AMI supply-chain story documented.
- [x] systemd hardening reviewed.
- [ ] public release artifacts are signed or otherwise supply-chain hardened beyond checksums.

Evidence:

- `docs/research/013-security-model.md`
- `docs/user/SECURITY_HARDENING.md`
- `docs/spec-v0.md`
- release artifact checksums

### 9. License, Notices, And Trademark Boundaries

License and trademark review is a release blocker for any public AMI, binary distribution, container image, or CloudFormation template.

First-release positioning:

- [x] First public release is positioned as free and open source.
- [x] User-facing first-release docs do not mention paid editions, hosted services, future Pro features, or non-OSS distribution channels.
- [x] First-release artifacts are distributed as free/open-source artifacts.

Alpha minimum:

- [x] Third-party dependency inventory exists for distributed artifacts:
  - [x] Go modules,
  - [x] LoxiLB runtime,
  - [x] `loxicmd`,
  - [x] nftables/conntrack packages,
  - [x] OS/base AMI components only when redistributed or materially packaged.
- [x] LoxiLB license is recorded as Apache License 2.0.
- [x] LoxiLB version and artifact digest are recorded in release metadata.
- [x] LoxiLB license text is included in AMI/release artifacts when LoxiLB is bundled.
- [x] Upstream LoxiLB copyright and attribution are preserved.
- [x] Upstream LoxiLB `NOTICE`, if present in the distributed artifact/source, is included.
- [x] BetterNAT docs describe LoxiLB as an integrated third-party datapath dependency, not as a BetterNAT-owned component.
- [x] BetterNAT docs and marketing do not imply NetLOX/LoxiLB endorsement, partnership, certification, or official support unless explicitly approved.
- [x] `THIRD_PARTY_NOTICES.md` exists or release notes clearly state where third-party notices are shipped.
- [x] If an AMI is distributed later, it contains third-party license files, for example:
  - [x] `/usr/share/doc/betternat/licenses/loxilb/LICENSE`,
  - [x] `/usr/share/doc/betternat/THIRD_PARTY_NOTICES.md`.
- [x] Release notes state that this is not legal advice.

Production requirement:

- [ ] Legal review completed for third-party licenses.
- [ ] Trademark review completed for BetterNAT, LoxiLB, AWS, Terraform, Prometheus, Grafana, and any cloud/provider names used in public copy.
- [ ] Product name, logo, domain, and package names are approved.
- [ ] Any use of "powered by", "integrates with", or similar third-party wording is approved.
- [x] Open-source license for BetterNAT itself is chosen and documented.
- [x] Vulnerability and dependency disclosure process is documented.

Evidence:

- `THIRD_PARTY_NOTICES.md`
- AMI file listing or Packer/EC2 Image Builder manifest
- release manifest with third-party versions and checksums
- legal review record before production release

### 10. Observability

Alpha minimum:

- [x] Prometheus metrics endpoint works.
- [x] HA state metric exists.
- [x] stale HA status metric exists.
- [x] lease owner match metric exists.
- [x] route target match metric exists.
- [x] public identity match metric exists for stable EIP mode.
- [x] datapath readiness is observable.

Production requirement:

- [x] Grafana dashboard or example Prometheus queries.
- [x] alert suggestions for stale HA state, route mismatch, public identity mismatch, and datapath not ready.
- [x] top-N attribution story is clearly scoped.

Evidence:

- `internal/metrics/`
- `docs/research/014-observability-mvp.md`
- AWS SSM metrics scrape output

### 11. Documentation

Documentation is a release blocker, not a follow-up polish task.

Alpha minimum:

- [x] User-facing docs follow `docs/dev/USER_DOCUMENTATION_GUIDE.md`.
- [x] fck-nat reference docs have been reviewed for install UX, limitations, IAM, sizing, and operational clarity.
- [x] LoxiLB reference docs have been reviewed for datapath description, runtime inspection, and attribution boundaries.
- [x] Quick Start exists.
- [x] First-release docs describe BetterNAT as free/open-source software and do not mention paid editions or future Pro features.
- [x] AWS prerequisites listed.
- [x] Terraform example included.
- [x] existing-VPC install guide exists.
- [x] disposable-test-VPC install guide exists.
- [x] user-facing architecture diagram exists.
- [x] Stable vs non-stable egress IP behavior explained.
- [x] Default HA timing explained.
- [x] Legacy HA profile aliases documented.
- [x] Failover limitations explained.
- [x] Cleanup procedure included.
- [x] pricing/cost caveats explained:
  - [x] BetterNAT avoids NAT Gateway per-GB processing charges,
  - [x] normal EC2, EBS, EIP, data transfer, DynamoDB, and CloudWatch charges still apply,
  - [x] high-volume savings are workload dependent.
- [x] minimum IAM policy is documented.
- [x] rollback behavior is documented.
- [x] upgrade/replacement behavior is documented for non-capacity changes.
- [x] Troubleshooting section includes:
  - [x] SSM access,
  - [x] agent service logs,
  - [x] metrics,
  - [x] route/EIP owner mismatch,
  - [x] DynamoDB lease state.

Production requirement:

- [x] upgrade guide.
- [x] rollback guide.
- [x] cost calculator docs.
- [x] security hardening docs.
- [ ] AMI refresh policy.
- [x] operations guide:
  - [x] how to detect active owner,
  - [x] how to inspect route/EIP ownership,
  - [x] how to inspect DynamoDB lease,
  - [x] how to recover from partial deploy,
  - [x] how to safely destroy or roll back.
- [x] observability guide:
  - [x] Prometheus scrape example,
  - [x] Grafana dashboard or starter queries,
  - [x] alerts for stale HA, route mismatch, EIP mismatch, datapath not ready.
- [x] production limitations page:
  - [x] no SLA equivalence to AWS NAT Gateway,
  - [x] no active connection preservation,
  - [x] single-AZ HA group scope,
  - [x] failure-mode table,
  - [x] measured failover timing and test conditions.
- [ ] docs have been followed by someone other than the primary developer in a clean account or disposable VPC.

Evidence:

- `README.md`
- `docs/README.md`
- `docs/dev/USER_DOCUMENTATION_GUIDE.md`
- `docs/user/QUICK_START.md`
- `docs/user/EXISTING_VPC_INSTALL.md`
- `docs/user/CONFIGURATION.md`
- `docs/user/IAM_POLICY.md`
- `docs/user/LIMITATIONS.md`
- `docs/user/FAILURE_MODES.md`
- `docs/user/RELEASE_NOTES_v0.1.0-alpha.2.md`
- `docs/testing/AWS_SUPPLEMENTAL_RUNBOOK.md`

## CloudFormation Delivery Checklist

CloudFormation should be considered for AWS-native UX, but it should not replace Terraform provider as the first implementation path.

Rationale:

- Terraform provider remains the richest UX for early users and can encapsulate BetterNAT's custom lifecycle.
- CloudFormation is valuable for users who prefer AWS-native stacks.
- It can deploy the full BetterNAT topology without requiring users to write all dependent AWS resources manually.

Alpha:

- [x] CloudFormation is optional and not required.
- [x] Terraform is the primary supported install path.

Beta / CloudFormation preparation:

- [ ] Create a CloudFormation template for a single-AZ BetterNAT HA group:
  - [ ] VPC selection parameters,
  - [ ] public subnet parameter,
  - [ ] private route table IDs parameter,
  - [ ] private CIDR allowlist parameter,
  - [ ] stable egress IP option,
  - [ ] HA profile option,
  - [ ] instance type,
  - [ ] ASG min/desired/max,
  - [ ] IAM role/profile,
  - [ ] DynamoDB lease table,
  - [ ] security groups,
  - [ ] launch template,
  - [ ] ASG,
  - [ ] outputs for EIP, ASG name, lease table, and metrics endpoint guidance.
- [ ] Validate CloudFormation create/update/delete in a disposable AWS account.
- [ ] Ensure CloudFormation stack delete restores or clearly handles private routes.
- [ ] Avoid custom Lambda resources unless strictly necessary.
- [ ] If custom resources are necessary:
  - [ ] document permissions,
  - [ ] test failure rollback,
  - [ ] ensure no seller-controlled external dependency is required.
- [ ] Build an architecture diagram for the template.
- [ ] Avoid AZ-specific assumptions; parameters must work across accounts where AZ names can map differently.

Production:

- [x] Decide whether CloudFormation is:
  - [ ] a first-class supported install path,
  - [x] or deferred.

Useful references:

- AWS CloudFormation registry extensions: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/registry.html

## Production-Ready Additional Checklist

Do not mark BetterNAT production-ready until alpha checklist is complete plus:

- [ ] AMI pipeline is repeatable and documented.
- [ ] AMI channel resolver is implemented.
- [ ] complete user documentation has passed a clean-account walkthrough.
- [x] CloudFormation decision is made and documented.
- [ ] stable-profile AWS test pass is complete.
- [ ] long soak test is complete.
- [ ] retry/backoff policy for AWS/DynamoDB transient failures is implemented.
- [ ] explicit-failure fast path is evaluated or implemented:
  - [ ] EC2 owner state check,
  - [x] graceful lease release on systemd stop,
  - [x] ASG lifecycle hook or interruption notice handling implemented locally,
  - [x] ASG lifecycle hook behavior verified in AWS,
  - [x] Spot interruption handling follows the documented AWS IMDS path; forced interruption validation is not a first-alpha release gate.
- [x] transient non-EIP leakage in stable mode is fixed or documented with clear conditions.
- [ ] IAM least-privilege policy is reviewed.
- [x] provider release process is documented.
- [x] release artifacts have checksums.
- [x] published docs include limitations and SLO language.
- [ ] benchmark results are reproducible.

## P1 Post-Alpha Checklist

P1 items are not required to publish the first alpha, but they should be prioritized before recommending BetterNAT for wider non-critical use.

### Release Distribution

- [x] Publish public binaries only through GitHub Release assets.
- [x] Add a repeatable GitHub release workflow:
  - [x] build artifacts,
  - [x] generate `SHA256SUMS`,
  - [x] generate `manifest.json`,
  - [x] upload release assets,
  - [x] verify release asset URLs,
  - [x] verify checksum file against uploaded assets.
- [x] Document Terraform provider installation for users who are not building from source.
- [x] Decide whether provider binaries are distributed through GitHub Releases only or later through the Terraform Registry.
- [x] Add a release artifact smoke test that verifies GitHub Release URLs and
  checksums instead of temporary S3 URLs.
- [x] Add a release deploy smoke test that applies Terraform using GitHub
  Release URLs instead of temporary S3 URLs.
  - [x] Repeatable smoke harness added:
    `scripts/release-deploy-smoke.sh`.
  - [x] Plan-only smoke passed with `BETTERNAT_VERSION=v0.1.0-alpha.2`,
    `BETTERNAT_PROVIDER_VERSION=0.1.0-alpha.3`, and
    `BETTERNAT_PROVIDER_INSTALL=github-mirror` in run
    `bnat-release-plan-alpha3-20260624034106`.
  - [x] Live disposable AWS apply/destroy passed in run
    `bnat-release-apply-alpha3-20260624034150`: Terraform created `16`
    resources and destroyed `16` resources.
  - [x] Post-destroy residual scan for run
    `bnat-release-apply-alpha3-20260624034150` found no matching VPC,
    non-terminated instance, ASG, DynamoDB table, EIP, IAM role, IAM instance
    profile, or launch template.

Release artifact smoke validation recorded on 2026-06-24:

- `scripts/check-release-pins.sh`: passed and verifies the current BetterNAT to
  LoxiLB pin plus the default `scripts/release-build.sh` version.
- `BETTERNAT_RELEASE_DIR=$PWD/tmp/release-default-check
  scripts/release-build.sh`: passed without explicitly setting
  `BETTERNAT_VERSION`; `manifest.json` reported `v0.1.0-alpha.2`.
- `BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-url-smoke.sh`: passed for
  Linux arm64 agent and CLI checksum verification.
- `BETTERNAT_VERSION=v0.1.0-alpha.2 BETTERNAT_SMOKE_ARCH=amd64
  scripts/release-url-smoke.sh`: passed for Linux amd64 agent and CLI checksum
  verification; amd64 CLI `version` executed successfully on the local host.
- `BETTERNAT_VERSION=v0.1.0-alpha.2
  BETTERNAT_PROVIDER_VERSION=0.1.0-alpha.3
  BETTERNAT_PROVIDER_INSTALL=github-mirror
  scripts/release-deploy-smoke.sh`: plan-only smoke passed through GitHub
  release artifact checksum verification, provider release checksum
  verification, Terraform init, validate, and plan.
- `BETTERNAT_VERSION=v0.1.0-alpha.2
  BETTERNAT_PROVIDER_VERSION=0.1.0-alpha.3
  BETTERNAT_PROVIDER_INSTALL=github-mirror
  BETTERNAT_RELEASE_DEPLOY_APPLY=1
  scripts/release-deploy-smoke.sh`: disposable AWS apply/destroy passed.
  Terraform Registry `0.1.0-alpha.3` was still unavailable, so this deploy
  smoke used the GitHub provider release as a Terraform filesystem mirror.
- `source scripts/setup-provider-github-mirror.sh` followed by `terraform init
  -upgrade` and `terraform validate`: passed for `examples/terraform`,
  `examples/terraform-aws-supplemental`, and `examples/terraform-localstack`
  with provider `0.1.0-alpha.3`.
- Final local release verification on 2026-06-24 after documentation refresh:
  - `GOCACHE=$PWD/tmp/go-build go test ./...`: passed.
  - `GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat
    ./cmd/betternat-agent ./cmd/terraform-provider-betternat`: passed.
  - `scripts/check-release-pins.sh`: passed.
  - `BETTERNAT_RELEASE_DIR=$PWD/tmp/release-final-check
    scripts/release-build.sh`: passed and generated local release artifacts.
  - `BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-url-smoke.sh`: passed
    for Linux arm64 agent and CLI.
  - `BETTERNAT_VERSION=v0.1.0-alpha.2 BETTERNAT_SMOKE_ARCH=amd64
    scripts/release-url-smoke.sh`: passed for Linux amd64 agent and CLI.
  - `scripts/release-dependency-scan.sh`: passed with `99` modules scanned,
    `0` missing license files, and `0` restricted license keyword hits.
  - `BETTERNAT_VERSION=v0.1.0-alpha.2
    BETTERNAT_PROVIDER_VERSION=0.1.0-alpha.3
    BETTERNAT_PROVIDER_INSTALL=github-mirror
    BETTERNAT_RELEASE_DEPLOY_RUN_ID=bnat-final-plan-alpha3-20260624-profile
    scripts/release-deploy-smoke.sh`: plan-only smoke passed with
    `AWS_PROFILE=601427795217_AdministratorAccess`; Terraform planned
    `16` creates, `0` changes, and `0` destroys.
- [x] Create `github.com/nowakeai/terraform-provider-betternat` for Registry-compatible provider publishing.
- [x] Expose a thin main-repo public provider factory for the provider repo.
- [x] Add provider-specific alpha release workflow for Linux provider zip artifacts.
- [x] Add provider-specific GoReleaser or equivalent workflow for full Terraform Registry artifact format.
- [x] Configure provider checksum signing.
- [x] Test provider install through Terraform.
- [x] Test provider local dev override through Terraform.
- [x] Test provider local dev override through OpenTofu.
- [x] Test provider filesystem mirror install through Terraform release zip.
- [x] Test provider filesystem mirror install through OpenTofu release zip with explicit `registry.terraform.io/nowakeai/betternat` source.
- [x] Publish and test provider through Terraform Registry.
- [x] Publish provider through OpenTofu-native registry path.

Provider Registry validation recorded on 2026-06-22:

- Terraform Registry provider version: `nowakeai/betternat` `0.1.0-alpha.2`.
- Terraform `v1.15.6` `terraform init` and `terraform validate`: passed with `source = "nowakeai/betternat"`.
- OpenTofu `v1.12.3` `tofu init` and `tofu validate`: passed with `source = "registry.terraform.io/nowakeai/betternat"`.
- Signing key ID observed by Terraform/OpenTofu: `F2D78A307FAB2914`.
- OpenTofu-native registry tracking issues: https://github.com/opentofu/registry/issues/4494 and https://github.com/opentofu/registry/issues/4496.

OpenTofu Registry update recorded on 2026-06-24:

- OpenTofu Registry provider protocol lists `nowakeai/betternat`
  `0.1.0-alpha.2`, `0.1.0-alpha.3`, and `0.1.0-alpha.4`.
- OpenTofu Registry download metadata for `0.1.0-alpha.4` is available for
  darwin/arm64, linux/amd64, and linux/arm64 and points to the signed GitHub
  provider release assets.
- Local `tofu init` validation was not rerun in this workspace because the
  `tofu` binary is not installed here.

### Operations And Observability

- [x] Add `betternat status` or equivalent HA status command:
  - [x] local role,
  - [x] lease owner,
  - [x] route owner,
  - [x] EIP owner,
  - [x] ASG/desired capacity health through the daemon registry view,
  - [x] datapath readiness.
- [x] Add `betternat support bundle`:
  - [x] config redaction,
  - [x] agent logs,
  - [x] systemd status,
  - [x] LoxiLB state,
  - [x] daemon status and handover summaries,
  - [x] metrics snapshot.
- [x] Ship Prometheus alert rule examples.
- [x] Ship Grafana starter dashboard or starter queries.
- [x] Clarify top-N source/destination attribution scope and limitations.

### Reliability

- [x] Add AWS SDK retry/backoff policy review and tests.
- [x] Add graceful shutdown lease release on systemd stop.
- [x] Add ASG termination lifecycle hook and IMDS Spot/ASG termination watcher.
- [x] Verify ASG lifecycle hook behavior in AWS. Spot interruption follows the documented IMDS path but is not practical to force on demand as a release gate.
- [x] Add LoxiLB restart reconciliation test.
  - [x] Unit coverage:
    `TestReconcileReplaysRulesAfterLoxiLBRestartRuleLoss` verifies that
    BetterNAT recreates desired LoxiLB SNAT rules when the firewall rule list
    becomes empty after a simulated LoxiLB restart/rule-loss event.
  - [x] Live AWS validation:
    restarting LoxiLB on the active node during the 2026-06-24 low-cost soak
    recovered through automatic handover and ended with healthy route/EIP owner
    convergence.
- [x] Run a low-cost soak test with periodic egress probes and agent restarts.
  - [x] Reusable egress probe monitor added:
    `scripts/egress-probe-monitor.sh`.
  - [x] Low-cost soak runbook added:
    `docs/testing/LOW_COST_SOAK_RUNBOOK.md`.
  - [x] Actual AWS soak smoke executed on 2026-06-24:
    `2400` private-client samples, `2396` ok, `4` failed, `0` unexpected
    public IP samples, longest consecutive failure run `1`, with standby agent
    restart, manual handover, and active LoxiLB restart during the probe.
  - [x] Active systemd-stop handover validated on 2026-06-24:
    `systemd-stop-1782271270264168584` completed
    `i-048fd34e26867122f -> i-073ab0073edde40ba`; client probe recorded
    `360` samples, `359` ok, `1` failed, and `0` unexpected public IP samples.
- [x] Document transient public-IP leakage conditions in non-stable and stable modes, or fix them if observed.

Reliability validation update on 2026-06-23:

- Private dev AMI `ami-072757363df299006` passed boot smoke and was rolled into
  ASG `betternat-bnat-lifecycle-20260623023753-us-west-2a` with launch template
  version `15`.
- Instance refresh `c7c091e4-63b6-4895-a160-ef75f7113a6f` completed
  successfully from `2026-06-23T18:27:10Z` to `2026-06-23T18:29:40Z`.
- The ASG lifecycle-triggered handover path completed during refresh, and two
  manual proactive handovers completed afterward on the AMI nodes.
- Client egress probing during one manual handover recorded `240` samples with
  `0` failed samples.
- The same probe observed `5` successful samples through non-shared gateway
  public IPs during handover because this temporary environment still assigns
  per-node public IPv4 addresses. Production AMI rollout must remove per-node
  public IP assignment before claiming stable shared-EIP identity.
- Follow-up no-public-IP validation on 2026-06-23 used VPC endpoints for
  private AWS API reachability, refreshed the ASG to launch template version
  `16` with `AssociatePublicIpAddress=false`, and completed a manual handover
  between no-public-IP gateway nodes. The client probe observed `0` non-shared
  public IP samples and `3` one-second curl timeouts out of `240` samples.
- Non-stable public-IP validation on 2026-06-24 refreshed the ASG to launch
  template version `17` with `AssociatePublicIpAddress=true` and no
  `ha.public_identity`. Manual route-only handover completed, and the client
  probe observed `0` failures out of `240` samples while the public source IP
  changed from `52.24.117.43` to `52.24.240.255`.
- The retained environment was restored to stable/no-public-IP launch template
  version `16` after the non-stable validation.
- Stale paired `systemd-stop-*` handover records remained in intermediate
  states after the ASG lifecycle handover completed. Follow-up code now filters
  and best-effort deletes expired handover records, and `handover history`
  hides stale non-terminal records from older lease generations by default.
- 2026-06-24 low-cost soak evidence:
  `docs/research/040-alpha-low-cost-soak-results.md`.

### Security And Supply Chain

- [x] Review runtime IAM least-privilege policy against real AWS actions.
- [x] Review systemd hardening options.
- [x] Add dependency/license scan to release workflow.
- [x] Add artifact signing decision:
  - [x] no signing for alpha with checksums only,
  - [ ] implement cosign/minisign/GPG for later releases.
- [x] Add vulnerability disclosure and patch policy to user docs.

### Documentation

- [x] Run the Quick Start from a clean clone using GitHub Release URLs.
  - [x] Clean clone at `v0.1.0-alpha.1` can read release `SHA256SUMS` and resolve arm64 agent/CLI checksums.
  - [x] Clean clone can install `nowakeai/betternat` `0.1.0-alpha.2` from Terraform Registry and validate `examples/terraform`.
  - [x] Provider `0.1.0-alpha.3` GitHub release artifact checksum verification passed for Linux amd64, Linux arm64, and manifest.
  - [x] Provider `0.1.0-alpha.4` GitHub release artifact checksum verification passed for Linux amd64.
  - [ ] Current provider release Terraform Registry install validation after Registry propagation.
    Rechecked on 2026-06-24 with Terraform `v1.14.7`: `0.1.0-alpha.3` was
    still unavailable in the Terraform Registry; `0.1.0-alpha.2` Registry
    install and `terraform validate` still passed.
    Rechecked again later on 2026-06-24 with Terraform `v1.14.7`:
    `0.1.0-alpha.3` was still unavailable; `0.1.0-alpha.2` still installed and
    validated from Terraform Registry.
    Rechecked with release deploy smoke run
    `bnat-registry-alpha3-recheck-20260624`: release URL checksum checks
    passed, but Terraform Registry install still failed with no available
    releases matching `0.1.0-alpha.3`.
    Rechecked after provider `0.1.0-alpha.4` publication: the Terraform
    Registry public API still reported latest `0.1.0-alpha.2` with no docs.
  - [x] Public examples use provider `0.1.0-alpha.4` plus
    `scripts/setup-provider-github-mirror.sh` until Terraform Registry
    propagation catches up.
  - [x] Clean clone `examples/terraform-aws-supplemental init` and `validate` passed with `HTTP_PROXY`/`HTTPS_PROXY` set to `http://127.0.0.1:10808`.
- [x] Add provider installation guide.
- [x] Add observability guide.
- [x] Add rollback guide.
- [x] Add upgrade/replacement guide.
- [x] Add cost calculator docs or a documented cost-model worksheet.

## P2 Backlog

P2 items are valuable, but should not block alpha or early P1 hardening.

### Packaging And Installation

- [x] Build repeatable AMI pipeline with Packer or EC2 Image Builder.
- [ ] Publish arm64 and amd64 AMIs.
- [ ] Add `ami_channel` resolver.
- [ ] Preload LoxiLB image or binary in AMI.
- [ ] Include third-party license bundle inside AMI.
- [x] Add CloudFormation template or make an explicit decision to defer it.
- [x] Evaluate Terraform Registry publication.

### Performance And Tuning

- [ ] Create reproducible benchmark harness:
  - [ ] nftables fallback,
  - [ ] LoxiLB/eBPF,
  - [ ] different instance families,
  - [ ] connection churn,
  - [ ] large-response workloads.
- [ ] Define benchmark-backed instance sizing guidance.
- [ ] Add optional advanced tuning profile:
  - [ ] conntrack hash buckets for fallback,
  - [ ] conntrack timeout profile for fallback,
  - [ ] ephemeral port range,
  - [ ] backlog settings,
  - [ ] ENA/RSS/IRQ guidance.
- [ ] Validate high-volume cost claims with bounded representative tests, not multi-TB release gates.

### Product Extensions

- [ ] Multi-AZ topology design.
- [ ] Multi-cloud abstraction review.
- [ ] Kubernetes/EKS pod attribution integration.
- [ ] Central observation server or hosted dashboard design.
- [ ] Policy-based egress routing.
- [ ] Cost attribution reports.

## Release Decision Template

Use this for each release candidate:

```text
Release:
Git commit:
Provider version:
Agent version:
CLI version:
AMI IDs:
  v0.1.0-alpha.2: No published BetterNAT AMI. Bootstrap path uses explicit user/provider-selected Linux AMI.
  us-west-2 arm64:
  us-west-2 amd64:

Release level:
  alpha | production

Scope:
  AWS:
  AZ model:
  Stable EIP:
  Non-stable egress:

Validation:
  go test ./...:
  terraform validate examples/terraform:
  terraform validate examples/terraform-aws-supplemental:
  AWS stable-EIP run id:
  AWS non-stable run id:
  Residual cleanup scan:
  Security scan:
  Dependency/license scan:

Known limitations:
  -

Release decision:
  ship | hold

Approver:
Date:
```

## Current Status Snapshot

As of 2026-06-24:

- Low-cost AWS complete-loop testing is complete for the current cloud-init development path.
- `stable_egress_ip=true` and `stable_egress_ip=false` modes have both passed owner-termination HA tests.
- Terraform provider exposes `ha_profile = "default"` plus advanced lease timing overrides.
- ASG repair and replacement standby behavior have passed.
- GitHub Release assets and checksums have been published and verified for the first alpha path.
- Public examples use provider `0.1.0-alpha.4` from the provider GitHub release
  via a Terraform filesystem mirror until Terraform Registry propagation catches
  up.
- User-facing install docs use GitHub Release asset URLs; internal AWS test runbooks may still use temporary S3 URLs for unreleased binaries.
- The agent handles SIGTERM/SIGINT and releases the locally owned HA lease on graceful shutdown using the fenced lease generation.
- The provider creates ASG termination lifecycle hooks, and the agent watches IMDS Spot/ASG termination notices to release lease and complete the lifecycle action.
- The main blockers for production are AMI release pipeline, retry/backoff hardening, stable-profile soak, and broader production hardening.
