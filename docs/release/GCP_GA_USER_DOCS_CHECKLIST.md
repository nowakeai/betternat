# GCP GA User Documentation Checklist

Date: 2026-06-26

## Purpose

Track the user-facing documentation work needed before GCP can be promoted from
validated implementation evidence to a readable install and operations story.

This checklist covers the main BetterNAT docs, Terraform provider Registry
docs, and Terraform module Registry docs. It intentionally avoids copying
research evidence into every page. Research docs remain the evidence source;
user docs should explain decisions, workflows, limits, and recovery steps.

## Current Release Hold

Status: do not publish a new GCP-capable release until the 2026-06-29 GKE
compatibility fixes are carried through release artifacts, user cleanup
documentation, and final release evidence.

Resolved in the 2026-06-29 local-artifact validation pass:

- GCP runtime service account lifecycle hardening passed repeated gateway
  replacement after adding service account visibility polling, IAM propagation
  retry, and managed runtime service account retention on cleanup.
- Agent startup readiness now distinguishes normal LoxiLB/route convergence
  from persistent post-ready datapath degradation by reporting startup
  convergence as `INIT` until a healthy `ACTIVE` has been observed.
- The GKE compatibility matrix passed for `e2-small`, `e2-highcpu-2`, and
  `e2-standard-2` with two replacement trials each, private-node Pod egress,
  route target verification, and no active-gateway `DEGRADED` windows.

Remaining release blockers:

- Publishable release artifacts must include the fixed runtime and provider
  behavior validated in the local-artifact GKE pass.
- Release packaging must include the updated user docs that explain retained
  provider-managed GCP runtime service accounts and manual removal after all
  gateways using the account are destroyed.
- Final release evidence must rerun the GCP quick start or equivalent
  disposable install using release-versioned artifacts, not only local artifact
  overrides.

## Documentation Principles

- Lead with the supported user path: module first, provider resources as
  advanced primitives.
- Keep AWS and GCP differences explicit where they affect operations.
- State limits once in a clear limitations page, then link to it instead of
  repeating caveats everywhere.
- Keep examples runnable with released artifacts only; pre-release artifact
  override examples belong in maintainer runbooks.
- Do not document nftables fallback as a product path. LoxiLB is the supported
  datapath across clouds.

## Initial First-Pass Findings Addressed

This checklist was created from these first-pass gaps, all addressed by the
current documentation pass:

- root `README.md` needed a GCP entry point and cloud-support table,
- `docs/user/` needed a GCP install path with prerequisites, route ownership,
  Firestore, IAM, validation, destroy, and rollback,
- `docs/user/reference/LIMITATIONS.md` needed to stop presenting current scope
  as AWS-only,
- `docs/user/operations/OPERATIONS_GUIDE.md` needed GCP-specific operational
  checks rather than AWS-shaped checks only,
- the GCP module README needed Registry-friendly user context before
  publication,
- the provider needed Registry-style docs for `betternat_gcp_gateway` and
  `betternat_gcp_gateway_status`,
- GCP research evidence needed to be distilled into user-facing docs.

## Source Of Truth Map

| User question | User-facing target | Evidence / maintainer source |
| --- | --- | --- |
| Which cloud should I start with? | Root `README.md`, `docs/user/README.md` | `docs/release/TERRAFORM_SURFACE_RESET_IMPLEMENTATION_PLAN.md` |
| How do I install on AWS? | AWS module Registry README, AWS quick start | AWS module repo, AWS smoke research |
| How do I install on GCP? | GCP module Registry README, new GCP quick start | GCP module repo, GCP disposable runbook |
| What does HA mean on GCP? | GCP quick start, limitations, failure modes | GCP HA and handover research docs |
| What are the GCP prerequisites? | GCP quick start, GCP IAM reference | GCP provider lifecycle and MIG/stable-IP results |
| What changes during failover? | Limitations, failure modes, operations guide | Protocol and connectivity-first results |
| How do I verify and clean up? | GCP quick start, rollback/cleanup guide | GCP disposable integration runbook |

## Checklist

### Main User Entry Points

- [x] Update root `README.md` to present BetterNAT as module-first:
  `nowakeai/betternat/aws` for AWS and `nowakeai/betternat/google` for GCP
  when GCP is published.
- [x] Add a short cloud-support table to root `README.md` with support level,
  HA backend, capacity repair model, stable public identity behavior, and main
  limitation per cloud.
- [x] Update `docs/user/README.md` so AWS and GCP users can follow separate
  evaluation paths without reading research docs.
- [x] Keep the top-level value proposition concise: cost, observability, HA,
  and self-managed ownership. Move cloud-specific implementation detail to the
  getting-started and reference pages.

### GCP Getting Started

- [x] Add `docs/user/getting-started/GCP_QUICK_START.md`.
- [x] Cover prerequisites: Terraform, Google provider auth, enabled APIs,
  existing VPC/subnetwork, private-client network tag, Firestore Native
  database, Compute Engine, IAM, and Private Google Access when stable public
  identity is enabled.
- [x] Use the GCP module as the default install surface:
  `source = "nowakeai/betternat/google"`.
- [x] Explain route ownership: BetterNAT owns the tagged private default route
  named by `route_name`; users must not manage another route with that name.
- [x] Explain `betternat_version`: it must point to a GCP-capable runtime
  release unless explicit artifact URLs and checksums are supplied in a
  maintainer-only validation run.
- [x] Include verification commands for private-client egress, route target,
  `betternat status`, `betternat doctor --live`, Prometheus metrics, and
  handover history.
- [x] Include destroy and residual checks for GCE instances, routes, firewall
  rules, addresses, service accounts, and Firestore handover records.

### GCP User Reference

- [x] Add or extend a GCP IAM reference with Terraform execution permissions,
  runtime service account permissions, Firestore permissions, route mutation,
  MIG repair, and stable public identity permissions.
- [x] Update `docs/user/reference/LIMITATIONS.md` so platform scope no longer
  says AWS-only once GCP is included.
- [x] In limitations, clearly distinguish GCP route-only/non-stable behavior
  from stable public identity behavior.
- [x] Document the GCP connectivity-first handover contract: preserve outbound
  connectivity first; the stable public IP may converge afterward.
- [x] Document that active flows may reset and observed recovery timings are
  validation evidence, not an SLA.
- [x] Update `docs/user/reference/PROVIDER_DATA_SOURCES.md` for
  `betternat_gcp_gateway_status`.

### Operations And Recovery

- [x] Add GCP-specific operational checks to `docs/user/operations/OPERATIONS_GUIDE.md`:
  Firestore lease owner, registry freshness, GCP route target, MIG capacity,
  stable address user, and gateway datapath readiness.
- [x] Update `docs/user/operations/FAILURE_MODES.md` with GCP passive failover,
  proactive handover, Compute/Firestore API errors, MIG repair, and stable
  public identity convergence.
- [x] Update `docs/user/operations/ROLLBACK_GUIDE.md` with GCP route restore,
  static address cleanup, Firestore record cleanup, and managed IAM cleanup.
- [x] Update `docs/user/operations/OBSERVABILITY_GUIDE.md` with GCP status and
  doctor behavior, metric interpretation, and support-bundle expectations.
- [x] Keep SSH as a testing/access option only where explicitly enabled. Do not
  imply production GCP deployments require SSH.

### Terraform Provider Registry Docs

- [x] Ensure provider docs include a concise overview that says modules are the
  recommended user surface and provider resources are advanced primitives.
- [x] Add Registry-style docs for `betternat_aws_gateway`.
- [x] Add Registry-style docs for `betternat_gcp_gateway`.
- [x] Add Registry-style docs for `betternat_runtime_artifacts`.
- [x] Add Registry-style docs for `betternat_aws_gateway_status`.
- [x] Add Registry-style docs for `betternat_gcp_gateway_status`.
- [x] Make GCP resource docs clear about replacement semantics: updates are
  not in-place for the alpha resource.
- [x] Avoid exposing maintainer-only artifact override paths as normal user
  examples.

### Terraform Module Registry Docs

- [x] AWS module README: confirm the first screen is still the recommended AWS
  path and does not over-explain provider internals.
- [x] AWS module README: add a short link path to Quick Start, limitations,
  operations, and rollback docs.
- [x] GCP module README: add a "When to use this module" section for private
  workloads using tagged routes in an existing VPC.
- [x] GCP module README: add a "Before you apply" checklist for APIs,
  Firestore, runtime IAM, VPC tag, Private Google Access, and cleanup scope.
- [x] GCP module README: explain default `capacity_repair_mode = "mig"` and
  why `unmanaged` is an escape hatch rather than the GA path.
- [x] GCP module README: explain stable public identity in user terms,
  including the connectivity-first transition behavior.
- [x] GCP module examples: keep examples minimal, but ensure each example has
  enough variables and comments to be runnable after release.
- [x] GCP module release notes: remove "do not publish" language before the
  actual release, and include validation evidence and known limits.

### Release Notes And Cross-Linking

- [x] Add runtime release notes for the GCP-capable BetterNAT release.
- [x] Add provider release notes that call out the Terraform surface reset and
  GCP support level.
- [x] Add AWS module release notes.
- [x] Add GCP module release notes.
- [x] Cross-link release notes to the exact quick start and limitations pages.
- [x] Verify all user-facing links from root README, docs index, AWS module
  README, GCP module README, and provider docs.

## Suggested Review Order

1. Root `README.md` and `docs/user/README.md`.
2. GCP module README, because it becomes the Terraform Registry first screen.
3. New GCP quick start.
4. Limitations and failure modes.
5. Operations, rollback, observability, and IAM reference.
6. Provider Registry docs.
7. Release notes and final link check.

## GCP Smoke Evidence

- [x] 2026-06-29 disposable GKE compatibility matrix in
  `smooth-calling-490406-d9`, region `us-west1`, zone `us-west1-a`, passed
  two replacement trials each for `e2-small`, `e2-highcpu-2`, and
  `e2-standard-2` using fixed local artifacts. Each trial verified route target
  ownership and 10/10 private-node Pod egress probes, with no active-gateway
  `DEGRADED` window.
- [x] After the matrix, the fixture was restored to `e2-small`; replacement
  completed without the previous runtime service account IAM failure, Terraform
  plan returned no changes, and a post-readiness private-node Pod probe returned
  `public-ip=34.83.255.93` and `HTTP/2 200`.
- [x] 2026-06-26 disposable GCP module smoke in `smooth-calling-490406-d9`
  covered module apply, Firestore HA, MIG capacity repair, private-client
  egress, stable public identity, proactive handover, passive-stop failover,
  destroy, and residual scan.
- [x] Proactive handover preserved stable egress IP `34.102.98.65`; observed
  probe result was 95/100 successful samples with a longest failure window of
  5 samples at 0.5 seconds.
- [x] Passive-stop failover preserved stable egress IP `34.102.98.65`; observed
  probe result was 62/100 successful samples with a longest failure window of
  38 samples at 0.5 seconds.
- [x] Cleanup removed Compute, IAM, route, firewall, address, service account,
  and GCS artifact resources. The only residuals were three run-scoped
  Firestore handover history records, which were deleted before the final
  residual scan passed.

## AWS Module Smoke Evidence

- [x] 2026-06-26 disposable AWS module smoke in account `601427795217`,
  region `us-west-2`, covered module apply, two-gateway ASG bootstrap,
  private-client egress, stable EIP, proactive handover, destroy, artifact
  bucket deletion, and residual scan.
- [x] Private-client egress returned stable EIP `54.71.83.128` for 10
  consecutive pre-handover samples; HTTPS, DNS, and 1 MiB download checks
  succeeded.
- [x] Proactive handover moved active ownership from
  `i-0f685166cc3926f60` to `i-07418f61b47294563`; observed probe result was
  120/120 successful samples with a longest failure window of 0 samples at
  0.5 seconds, and all samples retained `54.71.83.128`.
- [x] Cleanup destroyed 16 Terraform resources, deleted the temporary S3
  artifact bucket, and found no live run-scoped EC2, VPC, subnet, route table,
  security group, EIP, launch template, ASG, DynamoDB, IAM, or S3 residuals.

## Post-Publication Registry Evidence

- [x] BetterNAT runtime `v0.2.0` GitHub Release published and checksum
  verification passed for downloaded assets.
- [x] Terraform Registry provider install passed for
  `nowakeai/betternat = 0.2.0`; `betternat_runtime_artifacts` resolved the
  published `v0.2.0` agent URL and checksum.
- [x] Terraform Registry AWS module install and `terraform validate` passed for
  `nowakeai/betternat/aws = 0.2.0`.
- [x] Terraform Registry GCP module install and `terraform validate` passed for
  `nowakeai/betternat/google = 0.2.0`.
- [ ] OpenTofu Registry install was not rerun in this pass because `tofu` is not
  installed in the release workstation.
