# BetterNAT Documentation

This directory is split by audience and document lifecycle.

- `user/` is for people installing, operating, or evaluating BetterNAT. It is
  split into `getting-started/`, `operations/`, `reference/`, and `releases/`.
- `release/` is for maintainers preparing releases, AMIs, and publication gates.
- `testing/` is for repeatable local/AWS test plans and runbooks.
- `dev/` is for contributor workflow, dependency policy, and development validation.
- `research/` is for design records, spikes, feasibility analysis, and historical decisions.
- `dev-logs/` is for dated implementation notes and session summaries.
- `assets/` is for shared diagrams and images referenced by user and maintainer docs.

Older research may describe nftables-first, nftables fallback, or
custom-eBPF-first thinking. Treat those as history when they conflict with the
current architecture and spec. Current BetterNAT has no product fallback
datapath on AWS, GCP, or future clouds; LoxiLB readiness is a release gate.
Do not reintroduce nftables fallback UX, tests, or release acceptance paths
without a new architecture decision record.
Existing nftables/nf_conntrack code and smoke scripts may remain temporarily
for legacy diagnostics and cleanup only; their existence is not evidence of a
supported fallback path.

## Start Here

- [architecture.md](architecture.md) — Current architecture: LoxiLB datapath, Terraform install UX, and agent-owned HA.
- [architecture-diagram.md](architecture-diagram.md) — Mermaid diagrams for AWS route replacement, agent/LoxiLB interaction, runtime reconciliation, and failover.
- [spec-v0.md](spec-v0.md) — v0 product and implementation spec.
- [user/README.md](user/README.md) — User documentation index by task.
- [user/getting-started/QUICK_START.md](user/getting-started/QUICK_START.md) — Disposable-VPC install, verification, destroy, and cleanup guide.
- [release/RELEASE_CHECKLIST.md](release/RELEASE_CHECKLIST.md) — Release gates.

## User Docs

These are meant to be readable by external users.
Read them in this order for the first full pass:

- [user/README.md](user/README.md) — User documentation index by task.
- [user/reference/COST_MODEL.md](user/reference/COST_MODEL.md) — NAT Gateway processing-fee model, BetterNAT cost formula, example savings, endpoint guidance, and CLI estimate usage.
- [user/reference/LIMITATIONS.md](user/reference/LIMITATIONS.md) — SLA, failover, cost, performance, bootstrap, and tuning limitations.
- [user/getting-started/QUICK_START.md](user/getting-started/QUICK_START.md) — Quick start for a disposable AWS VPC.
- [user/operations/OPERATIONS_GUIDE.md](user/operations/OPERATIONS_GUIDE.md) — Day-2 operations: CLI, metrics, alerts, AWS checks, SSM access, troubleshooting, and cleanup.
- [user/getting-started/EKS_TERRAFORM_MODULE_INTEGRATION.md](user/getting-started/EKS_TERRAFORM_MODULE_INTEGRATION.md) — Module-level `nat_backend` switch for existing EKS/networking Terraform repos.
- [user/getting-started/EXISTING_VPC_INSTALL.md](user/getting-started/EXISTING_VPC_INSTALL.md) — Existing-VPC install and route ownership warnings.
- [user/getting-started/CONFIGURATION.md](user/getting-started/CONFIGURATION.md) — `betternat_aws_gateway` Terraform input reference and runtime configuration notes.
- [user/reference/PROVIDER_DATA_SOURCES.md](user/reference/PROVIDER_DATA_SOURCES.md) — BetterNAT provider data sources for runtime artifact metadata and gateway status reads.
- [user/reference/IAM_POLICY.md](user/reference/IAM_POLICY.md) — Terraform execution and gateway runtime IAM requirements.
- [user/reference/SECURITY_HARDENING.md](user/reference/SECURITY_HARDENING.md) — Current security posture, IAM/network/bootstrap hardening, artifact integrity, and production checklist.
- [user/operations/OBSERVABILITY_GUIDE.md](user/operations/OBSERVABILITY_GUIDE.md) — Prometheus metrics, CLI checks, AWS cross-checks, alerts, attribution scope, and current observability limits.
- [user/operations/ROLLBACK_GUIDE.md](user/operations/ROLLBACK_GUIDE.md) — Safe destroy, private route restoration, manual rollback, and residual-resource checks.
- [user/operations/UPGRADE_REPLACEMENT_GUIDE.md](user/operations/UPGRADE_REPLACEMENT_GUIDE.md) — In-place capacity updates, explicit replacement, blue/green upgrade workflow, and rolling-upgrade limits.
- [user/operations/FAILURE_MODES.md](user/operations/FAILURE_MODES.md) — Failure-mode behavior and recovery signals.
- [user/releases/README.md](user/releases/README.md) — Release notes index and release-note rules.
- [user/releases/v0.1/v0.1.0.md](user/releases/v0.1/v0.1.0.md) — 0.1.0 GA release notes.
- [user/releases/v0.1/v0.1.0-alpha.8.md](user/releases/v0.1/v0.1.0-alpha.8.md) — Runtime alpha release notes and validation caveats.
- [user/releases/v0.1/v0.1.0-alpha.7.md](user/releases/v0.1/v0.1.0-alpha.7.md) — Provider-module support release notes for split provider alpha8.
- [user/releases/v0.1/v0.1.0-alpha.6.md](user/releases/v0.1/v0.1.0-alpha.6.md) — Runtime alpha release notes for provider alpha8 bootstrap support.
- [user/releases/v0.1/v0.1.0-alpha.2.md](user/releases/v0.1/v0.1.0-alpha.2.md) — Earlier runtime alpha release notes.
- [user/releases/v0.1/v0.1.0-alpha.1.md](user/releases/v0.1/v0.1.0-alpha.1.md) — First alpha release notes.

## Release And Packaging

These are maintainer-facing release documents.

- [release/RELEASE_CHECKLIST.md](release/RELEASE_CHECKLIST.md) — Release gates, evidence requirements, validation commands, and decision template.
- [release/ALPHA_RELEASE_DECISION_2026-06-24.md](release/ALPHA_RELEASE_DECISION_2026-06-24.md) — Current alpha ship decision, evidence, limitations, and deferred work.
- [release/OPEN_SOURCE_RELEASE_PLAN.md](release/OPEN_SOURCE_RELEASE_PLAN.md) — First public release plan for the free/open-source edition.
- [release/ENGINEERING_RELEASE_GAP_LIST.md](release/ENGINEERING_RELEASE_GAP_LIST.md) — Code, feature, and test gaps by priority.
- [release/ALPHA_BOOTSTRAP_RELEASE_PATH.md](release/ALPHA_BOOTSTRAP_RELEASE_PATH.md) — Temporary cloud-init based release path before published AMIs exist.
- [release/AMI_RELEASE_PLAN.md](release/AMI_RELEASE_PLAN.md) — AMI-first production release contract and AMI readiness tests.
- [release/DEPENDENCY_PINS.md](release/DEPENDENCY_PINS.md) — BetterNAT release to LoxiLB version/digest pin table and validation policy.
- [release/ARTIFACT_SIGNING_DECISION.md](release/ARTIFACT_SIGNING_DECISION.md) — Alpha checksum-only decision and production signing target.
- [release/CLOUDFORMATION_DELIVERY_DECISION.md](release/CLOUDFORMATION_DELIVERY_DECISION.md) — Decision to defer CloudFormation while Terraform remains the supported install path.
- [release/TERRAFORM_PROVIDER_DISTRIBUTION_PLAN.md](release/TERRAFORM_PROVIDER_DISTRIBUTION_PLAN.md) — Split-repo provider publishing plan, Registry release model, versioning, and OpenTofu compatibility.
- [release/TERRAFORM_SURFACE_RESET_IMPLEMENTATION_PLAN.md](release/TERRAFORM_SURFACE_RESET_IMPLEMENTATION_PLAN.md) — Implementation tracker for the provider/module split, Terraform surface reset, and GCP alpha path.

## Testing

These are executable plans and runbooks, not product docs.

- [testing/AWS_TEST_PLAN.md](testing/AWS_TEST_PLAN.md) — AWS integration test plan for route replacement, EIP failover, DynamoDB lease, LoxiLB on EC2, public egress, rollback, and cleanup.
- [testing/AWS_SUPPLEMENTAL_TEST_PLAN.md](testing/AWS_SUPPLEMENTAL_TEST_PLAN.md) — Low-cost supplemental AWS tests and deferred expensive work.
- [testing/AWS_SUPPLEMENTAL_RUNBOOK.md](testing/AWS_SUPPLEMENTAL_RUNBOOK.md) — Execution checklist for the low-cost AWS supplemental test pass.
- [testing/LOW_COST_SOAK_RUNBOOK.md](testing/LOW_COST_SOAK_RUNBOOK.md) — Low-cost soak runbook with periodic egress probes, agent restarts, LoxiLB restart checks, and handover evidence collection.
- [testing/GCP_SPIKE_PLAN.md](testing/GCP_SPIKE_PLAN.md) — Disposable GCP validation plan before any GCP alpha provider implementation.
- [testing/GCP_DISPOSABLE_INTEGRATION_RUNBOOK.md](testing/GCP_DISPOSABLE_INTEGRATION_RUNBOOK.md) — Disposable GCP apply, Firestore contention, two-agent HA, failover, handover, datapath, and cleanup evidence checklist.

## Development

These documents guide contributors and agents working in the repo.

- [dev/AI_WORKFLOW.md](dev/AI_WORKFLOW.md) — AI-assisted workflow, validation ladder, documentation update rules, and product bias.
- [dev/DEPENDENCY_POLICY.md](dev/DEPENDENCY_POLICY.md) — Dependency freshness, mature-component preference, and upgrade policy.
- [dev/LOCAL_VM_TEST_MATRIX.md](dev/LOCAL_VM_TEST_MATRIX.md) — Local VM test matrix and AWS boundaries.
- [dev/LINUX_DATAPATH_VALIDATION.md](dev/LINUX_DATAPATH_VALIDATION.md) — Environment-agnostic Linux validation plan for LoxiLB plus legacy nftables/conntrack diagnostics while retained.
- [dev/TERRAFORM_PROVIDER_LOCAL_TESTING.md](dev/TERRAFORM_PROVIDER_LOCAL_TESTING.md) — Local provider testing layers: Go tests, Terraform CLI dev overrides, LocalStack, and AWS acceptance.
- [dev/USER_DOCUMENTATION_GUIDE.md](dev/USER_DOCUMENTATION_GUIDE.md) — User-facing documentation rules based on fck-nat and LoxiLB references.

## Development Logs

- [dev-logs/README.md](dev-logs/README.md) — How to record durable implementation notes, session summaries, and architecture pivots.
- [dev-logs/2026-06-20-harness-and-dependency-refresh.md](dev-logs/2026-06-20-harness-and-dependency-refresh.md) — Repo harness setup and dependency refresh notes.
- [dev-logs/2026-06-23-agent-daemon-status-api.md](dev-logs/2026-06-23-agent-daemon-status-api.md) — First daemon-backed CLI status API implementation notes.

## Key Research Results

Read these first when revisiting product or architecture direction:

- [research/021-loxilb-spike-results.md](research/021-loxilb-spike-results.md) — Initial AWS LoxiLB egress NAT spike.
- [research/022-loxilb-extended-spike-results.md](research/022-loxilb-extended-spike-results.md) — Extended LoxiLB validation: DNS/UDP, downloads, concurrent flows, failover, and persistence caveats.
- [research/023-aws-integration-test-results.md](research/023-aws-integration-test-results.md) — AWS integration test: isolated VPC, LoxiLB on EC2, private egress, EIP/route failover, DynamoDB fencing, and cleanup.
- [research/024-terraform-workflow-devops-tf-nat.md](research/024-terraform-workflow-devops-tf-nat.md) — devops-tf NAT Gateway workflow review and BetterNAT provider/module UX recommendations.
- [research/025-fck-nat-reference-review.md](research/025-fck-nat-reference-review.md) — fck-nat reference review: AMI packaging, config contract, sysctl tuning, metrics, and what BetterNAT should or should not copy.
- [research/027-asg-self-healing-architecture.md](research/027-asg-self-healing-architecture.md) — ASG appliance-pool architecture for self-healing capacity and production HA.
- [research/028-asg-pool-vs-per-instance-asg.md](research/028-asg-pool-vs-per-instance-asg.md) — Decision record for one ASG pool per AZ.
- [research/031-aws-low-cost-supplemental-results.md](research/031-aws-low-cost-supplemental-results.md) — Low-cost AWS supplemental run results.
- [research/033-upgrade-and-graceful-shutdown-design.md](research/033-upgrade-and-graceful-shutdown-design.md) — Gateway upgrade model, graceful shutdown, and alpha/production policy.
- [research/036-loxilb-sysctl-and-conntrack-tuning.md](research/036-loxilb-sysctl-and-conntrack-tuning.md) — LoxiLB/eBPF, sysctl, and `nf_conntrack` tuning decision.
- [research/037-v0.1.0-alpha-aws-release-candidate-results.md](research/037-v0.1.0-alpha-aws-release-candidate-results.md) — v0.1.0-alpha AWS release-candidate result.
- [research/038-agent-registry-control-plane-plan.md](research/038-agent-registry-control-plane-plan.md) — Coordination backend, agent registry, service discovery, and permission-reduction plan.
- [research/039-agent-daemon-api-and-handover-plan.md](research/039-agent-daemon-api-and-handover-plan.md) — Agent daemon API, fast CLI path, cached peer state, and proactive handover plan.
- [research/040-alpha-low-cost-soak-results.md](research/040-alpha-low-cost-soak-results.md) — Alpha low-cost soak smoke with client probes, active systemd-stop handover, and live LoxiLB restart recovery.
- [research/041-alpha6-final-aws-validation.md](research/041-alpha6-final-aws-validation.md) — Final alpha6 Registry/provider AWS validation and non-AMI stable-EIP HA bootstrap blocker.
- [research/042-provider-alpha7-clean-aws-validation.md](research/042-provider-alpha7-clean-aws-validation.md) — Provider alpha7 Registry install, clean AWS bootstrap/handover validation, destroy, residual scan, and stable-EIP identity caveat.
- [research/043-ga-iam-security-review.md](research/043-ga-iam-security-review.md) — GA IAM least-privilege and default security posture review after provider alpha7.
- [research/044-ga-legal-trademark-review.md](research/044-ga-legal-trademark-review.md) — GA legal/trademark wording review for BetterNAT and third-party project references.
- [research/045-ga-release-artifact-governance-review.md](research/045-ga-release-artifact-governance-review.md) — GA release artifact governance review for checksums, notes, compatibility, SemVer, pins, and signing decision.
- [research/046-provider-alpha8-ga-soak-results.md](research/046-provider-alpha8-ga-soak-results.md) — Provider alpha8 Terraform Registry soak with runtime alpha6, restart/handover events, ASG lifecycle finding, destroy, and residual scan.
- [research/047-runtime-alpha8-asg-lifecycle-validation.md](research/047-runtime-alpha8-asg-lifecycle-validation.md) — Runtime alpha8 ASG lifecycle validation through provider artifact overrides, completed durable handover, client probe, and cleanup evidence.
- [research/048-provider-module-boundary-plan.md](research/048-provider-module-boundary-plan.md) — Provider/module responsibility split, AWS module migration path, and multi-cloud naming decision.
- [research/049-gcp-alpha-boundary.md](research/049-gcp-alpha-boundary.md) — GCP alpha scope boundary, business signal, and spike gate.
- [research/050-terraform-surface-reset-aws-smoke.md](research/050-terraform-surface-reset-aws-smoke.md) — Unpublished provider `v0.2.0` local-mirror AWS smoke for `betternat_aws_gateway`, handover, data source reads, destroy, and residual scan.
- [research/051-gcp-forwarding-spike-results.md](research/051-gcp-forwarding-spike-results.md) — Disposable GCP forwarding substrate spike proving GCE `canIpForward`, tagged route replacement, and cleanup.
- [research/052-gcp-ha-gap-analysis.md](research/052-gcp-ha-gap-analysis.md) — GCP HA gap analysis covering Firestore coordination, route fencing, public identity, LoxiLB, IAM, observability, and release gates.
- [research/053-gcp-firestore-live-contention-results.md](research/053-gcp-firestore-live-contention-results.md) — Live Firestore Native contention validation for GCP lease, registry, handover records, and cleanup.
- [research/054-gcp-agent-ha-smoke-results.md](research/054-gcp-agent-ha-smoke-results.md) — Live GCP two-agent HA smoke covering Firestore ownership, route repair, passive failover, cleanup, and remaining GCP alpha gaps.
- [research/055-no-nftables-fallback-decision.md](research/055-no-nftables-fallback-decision.md) — Decision record that BetterNAT has no product nftables fallback; legacy code may remain only while phased out.
- [research/056-gcp-proactive-handover-results.md](research/056-gcp-proactive-handover-results.md) — Live GCP proactive handover validation, lease-renewal fix, Firestore handover history support, client egress, and cleanup.
- [research/057-gcp-loxilb-restart-results.md](research/057-gcp-loxilb-restart-results.md) — Live GCP LoxiLB datapath counter, restart replay, support bundle, and cleanup validation.
- [research/058-gcp-provider-lifecycle-results.md](research/058-gcp-provider-lifecycle-results.md) — Live GCP provider-owned runtime IAM, service-account, Firestore database lifecycle validation and per-gateway role fix.
- [research/059-gcp-protocol-failover-results.md](research/059-gcp-protocol-failover-results.md) — Live GCP route-only protocol failover validation for TCP, HTTPS, UDP DNS, long download, public-IP switch, and cleanup.
- [research/060-gcp-failure-injection-results.md](research/060-gcp-failure-injection-results.md) — Live GCP failure-injection validation proving active gateway degradation when Firestore/Compute API access is unavailable.
- [research/061-gcp-stable-public-identity-decision.md](research/061-gcp-stable-public-identity-decision.md) — Decision that GCP alpha remains route-only/non-stable until access-config handover is designed and live-validated.
- [research/062-gcp-capacity-repair-decision.md](research/062-gcp-capacity-repair-decision.md) — Decision that GCP alpha may use unmanaged VMs, while GA should use MIG-backed capacity repair unless a later ADR changes that direction.
- [research/063-gcp-mig-stable-ip-results.md](research/063-gcp-mig-stable-ip-results.md) — Live GCP combined MIG capacity repair and static external IPv4 handover validation, including Private Google Access and IAM findings.
- [research/064-gcp-stable-ip-protocol-results.md](research/064-gcp-stable-ip-protocol-results.md) — Live GCP private-client stable public IP protocol validation, proactive handover blocker, SSH harness notes, and cleanup evidence.

## Supporting Research

- [research/001-cilium-hubble-ebpf-feasibility.md](research/001-cilium-hubble-ebpf-feasibility.md)
- [research/002-existing-building-blocks.md](research/002-existing-building-blocks.md)
- [research/003-nftables-performance.md](research/003-nftables-performance.md)
- [research/004-product-pillars-feasibility.md](research/004-product-pillars-feasibility.md)
- [research/005-cost-model-review.md](research/005-cost-model-review.md)
- [research/006-datapath-comparison-nftables-ebpf-vpp.md](research/006-datapath-comparison-nftables-ebpf-vpp.md)
- [research/007-ha-primitive-selection.md](research/007-ha-primitive-selection.md)
- [research/008-ha-replaceroute-lease-multicloud.md](research/008-ha-replaceroute-lease-multicloud.md)
- [research/009-observability-eks-pod-attribution.md](research/009-observability-eks-pod-attribution.md)
- [research/010-install-terraform-ux.md](research/010-install-terraform-ux.md)
- [research/011-benchmark-slo.md](research/011-benchmark-slo.md)
- [research/012-agent-architecture.md](research/012-agent-architecture.md)
- [research/013-security-model.md](research/013-security-model.md)
- [research/014-observability-mvp.md](research/014-observability-mvp.md)
- [research/015-cost-calculator.md](research/015-cost-calculator.md)
- [research/016-mvp-scope-milestones.md](research/016-mvp-scope-milestones.md)
- [research/017-loxilb-evaluation.md](research/017-loxilb-evaluation.md)
- [research/018-naming-collision-check.md](research/018-naming-collision-check.md)
- [research/019-target-workloads.md](research/019-target-workloads.md)
- [research/020-loxilb-spike-plan.md](research/020-loxilb-spike-plan.md)
- [research/026-aws-supplemental-test-results.md](research/026-aws-supplemental-test-results.md)
- [research/029-aws-asg-provider-test-results.md](research/029-aws-asg-provider-test-results.md)
- [research/030-automatic-ha-implementation-checklist.md](research/030-automatic-ha-implementation-checklist.md)
- [research/032-failover-stability-industry-patterns.md](research/032-failover-stability-industry-patterns.md)
- [research/034-pro-edition-product-plan.md](research/034-pro-edition-product-plan.md)
- [research/035-p0-open-source-release-acceptance-results.md](research/035-p0-open-source-release-acceptance-results.md)

## Documentation Rules

- Put external install/operation docs in `docs/user/`.
- Put release gate, packaging, and publication docs in `docs/release/`.
- Put repeatable test plans and runbooks in `docs/testing/`.
- Put contributor workflow and local validation docs in `docs/dev/`.
- Put evidence, design decisions, spikes, and research in `docs/research/`.
- Put dated implementation notes in `docs/dev-logs/`.
- Update this index when adding a durable new document.
