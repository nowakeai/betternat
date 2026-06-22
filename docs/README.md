# BetterNAT Documentation

This directory is split by audience and document lifecycle.

- `user/` is for people installing, operating, or evaluating BetterNAT.
- `release/` is for maintainers preparing releases, AMIs, and publication gates.
- `testing/` is for repeatable local/AWS test plans and runbooks.
- `dev/` is for contributor workflow, dependency policy, and development validation.
- `research/` is for design records, spikes, feasibility analysis, and historical decisions.
- `dev-logs/` is for dated implementation notes and session summaries.
- `assets/` is for shared diagrams and images referenced by user and maintainer docs.

Older research may describe nftables-first or custom-eBPF-first thinking. Treat those as history when they conflict with the current architecture and spec.

## Start Here

- [architecture.md](architecture.md) — Current architecture: LoxiLB datapath, Terraform install UX, and agent-owned HA.
- [architecture-diagram.md](architecture-diagram.md) — Mermaid diagrams for AWS route replacement, agent/LoxiLB interaction, runtime reconciliation, and failover.
- [spec-v0.md](spec-v0.md) — v0 product and implementation spec.
- [user/QUICK_START.md](user/QUICK_START.md) — Disposable-VPC install, verification, destroy, and cleanup guide for the first alpha.
- [release/RELEASE_CHECKLIST.md](release/RELEASE_CHECKLIST.md) — Alpha and production release gates.

## User Docs

These are meant to be readable by external users.
Read them in this order for the first full pass:

- [user/QUICK_START.md](user/QUICK_START.md) — First alpha quick start.
- [user/EXISTING_VPC_INSTALL.md](user/EXISTING_VPC_INSTALL.md) — Existing-VPC install and route ownership warnings.
- [user/COST_MODEL.md](user/COST_MODEL.md) — NAT Gateway processing-fee model, BetterNAT cost formula, example savings, endpoint guidance, and CLI estimate usage.
- [user/CONFIGURATION.md](user/CONFIGURATION.md) — `betternat_gateway` Terraform input reference and runtime configuration notes.
- [user/IAM_POLICY.md](user/IAM_POLICY.md) — Terraform execution and gateway runtime IAM requirements.
- [user/SECURITY_HARDENING.md](user/SECURITY_HARDENING.md) — Current alpha security posture, IAM/network/bootstrap hardening, artifact integrity, and production checklist.
- [user/OPERATIONS_GUIDE.md](user/OPERATIONS_GUIDE.md) — Day-2 operations: CLI, metrics, alerts, AWS checks, SSM access, troubleshooting, and cleanup.
- [user/OBSERVABILITY_GUIDE.md](user/OBSERVABILITY_GUIDE.md) — Prometheus metrics, CLI checks, AWS cross-checks, alerts, attribution scope, and current observability limits.
- [user/ROLLBACK_GUIDE.md](user/ROLLBACK_GUIDE.md) — Safe destroy, private route restoration, manual rollback, and residual-resource checks.
- [user/UPGRADE_REPLACEMENT_GUIDE.md](user/UPGRADE_REPLACEMENT_GUIDE.md) — In-place capacity updates, explicit replacement, blue/green upgrade workflow, and alpha rolling-upgrade limits.
- [user/FAILURE_MODES.md](user/FAILURE_MODES.md) — Failure-mode behavior and limitations.
- [user/LIMITATIONS.md](user/LIMITATIONS.md) — Alpha limitations: SLA, failover, cost, performance, bootstrap, and tuning.
- [user/RELEASE_NOTES_v0.1.0-alpha.1.md](user/RELEASE_NOTES_v0.1.0-alpha.1.md) — First alpha release notes.

## Release And Packaging

These are maintainer-facing release documents.

- [release/RELEASE_CHECKLIST.md](release/RELEASE_CHECKLIST.md) — Release gates, evidence requirements, validation commands, and decision template.
- [release/OPEN_SOURCE_RELEASE_PLAN.md](release/OPEN_SOURCE_RELEASE_PLAN.md) — First public release plan for the free/open-source edition.
- [release/ENGINEERING_RELEASE_GAP_LIST.md](release/ENGINEERING_RELEASE_GAP_LIST.md) — Code, feature, and test gaps by priority.
- [release/ALPHA_BOOTSTRAP_RELEASE_PATH.md](release/ALPHA_BOOTSTRAP_RELEASE_PATH.md) — Temporary cloud-init based release path before published AMIs exist.
- [release/AMI_RELEASE_PLAN.md](release/AMI_RELEASE_PLAN.md) — AMI-first production release contract and AMI readiness tests.
- [release/TERRAFORM_PROVIDER_DISTRIBUTION_PLAN.md](release/TERRAFORM_PROVIDER_DISTRIBUTION_PLAN.md) — Split-repo provider publishing plan, Registry release model, versioning, and OpenTofu compatibility.

## Testing

These are executable plans and runbooks, not product docs.

- [testing/AWS_TEST_PLAN.md](testing/AWS_TEST_PLAN.md) — AWS integration test plan for route replacement, EIP failover, DynamoDB lease, LoxiLB on EC2, public egress, rollback, and cleanup.
- [testing/AWS_SUPPLEMENTAL_TEST_PLAN.md](testing/AWS_SUPPLEMENTAL_TEST_PLAN.md) — Low-cost supplemental AWS tests and deferred expensive work.
- [testing/AWS_SUPPLEMENTAL_RUNBOOK.md](testing/AWS_SUPPLEMENTAL_RUNBOOK.md) — Execution checklist for the low-cost AWS supplemental test pass.

## Development

These documents guide contributors and agents working in the repo.

- [dev/AI_WORKFLOW.md](dev/AI_WORKFLOW.md) — AI-assisted workflow, validation ladder, documentation update rules, and product bias.
- [dev/DEPENDENCY_POLICY.md](dev/DEPENDENCY_POLICY.md) — Dependency freshness, mature-component preference, and upgrade policy.
- [dev/LOCAL_VM_TEST_MATRIX.md](dev/LOCAL_VM_TEST_MATRIX.md) — Local VM test matrix and AWS boundaries.
- [dev/LINUX_DATAPATH_VALIDATION.md](dev/LINUX_DATAPATH_VALIDATION.md) — Environment-agnostic Linux validation plan for nftables, conntrack, and LoxiLB.
- [dev/TERRAFORM_PROVIDER_LOCAL_TESTING.md](dev/TERRAFORM_PROVIDER_LOCAL_TESTING.md) — Local provider testing layers: Go tests, Terraform CLI dev overrides, LocalStack, and AWS acceptance.
- [dev/USER_DOCUMENTATION_GUIDE.md](dev/USER_DOCUMENTATION_GUIDE.md) — User-facing documentation rules based on fck-nat and LoxiLB references.

## Development Logs

- [dev-logs/README.md](dev-logs/README.md) — How to record durable implementation notes, session summaries, and architecture pivots.
- [dev-logs/2026-06-20-harness-and-dependency-refresh.md](dev-logs/2026-06-20-harness-and-dependency-refresh.md) — Repo harness setup and dependency refresh notes.

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
