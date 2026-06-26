# GCP GA User Documentation Checklist

Date: 2026-06-26

## Purpose

Track the user-facing documentation work needed before GCP can be promoted from
validated implementation evidence to a readable install and operations story.

This checklist covers the main BetterNAT docs, Terraform provider Registry
docs, and Terraform module Registry docs. It intentionally avoids copying
research evidence into every page. Research docs remain the evidence source;
user docs should explain decisions, workflows, limits, and recovery steps.

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

## Current First-Pass Findings

- Root `README.md` is still AWS-oriented and does not yet give GCP users a
  clear entry point.
- `docs/user/` has a strong AWS evaluation and operations path, but no GCP
  install path with prerequisites, route ownership, Firestore, IAM, validation,
  destroy, and rollback.
- `docs/user/reference/LIMITATIONS.md` still says current platform scope is
  AWS only.
- `docs/user/operations/OPERATIONS_GUIDE.md` mentions GCP live doctor/status,
  but the operational checks are still mostly AWS-shaped.
- The GCP module README is a useful Registry seed, but it needs more user
  context before publication: when to use stable public identity, Private
  Google Access requirements, cleanup expectations, and alpha/GA language.
- The provider schema includes GCP resource descriptions, but the main repo
  should have Registry-style provider docs for `betternat_gcp_gateway` and
  `betternat_gcp_gateway_status` before users see the release.
- Research docs contain the best GCP evidence, especially MIG capacity repair,
  stable public identity, and connectivity-first handover, but that evidence is
  not yet distilled into user docs.

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

- [ ] Update root `README.md` to present BetterNAT as module-first:
  `nowakeai/betternat/aws` for AWS and `nowakeai/betternat/google` for GCP
  when GCP is published.
- [ ] Add a short cloud-support table to root `README.md` with support level,
  HA backend, capacity repair model, stable public identity behavior, and main
  limitation per cloud.
- [ ] Update `docs/user/README.md` so AWS and GCP users can follow separate
  evaluation paths without reading research docs.
- [ ] Keep the top-level value proposition concise: cost, observability, HA,
  and self-managed ownership. Move cloud-specific implementation detail to the
  getting-started and reference pages.

### GCP Getting Started

- [ ] Add `docs/user/getting-started/GCP_QUICK_START.md`.
- [ ] Cover prerequisites: Terraform, Google provider auth, enabled APIs,
  existing VPC/subnetwork, private-client network tag, Firestore Native
  database, Compute Engine, IAM, and Private Google Access when stable public
  identity is enabled.
- [ ] Use the GCP module as the default install surface:
  `source = "nowakeai/betternat/google"`.
- [ ] Explain route ownership: BetterNAT owns the tagged private default route
  named by `route_name`; users must not manage another route with that name.
- [ ] Explain `betternat_version`: it must point to a GCP-capable runtime
  release unless explicit artifact URLs and checksums are supplied in a
  maintainer-only validation run.
- [ ] Include verification commands for private-client egress, route target,
  `betternat status`, `betternat doctor --live`, Prometheus metrics, and
  handover history.
- [ ] Include destroy and residual checks for GCE instances, routes, firewall
  rules, addresses, service accounts, and Firestore handover records.

### GCP User Reference

- [ ] Add or extend a GCP IAM reference with Terraform execution permissions,
  runtime service account permissions, Firestore permissions, route mutation,
  MIG repair, and stable public identity permissions.
- [ ] Update `docs/user/reference/LIMITATIONS.md` so platform scope no longer
  says AWS-only once GCP is included.
- [ ] In limitations, clearly distinguish GCP route-only/non-stable behavior
  from stable public identity behavior.
- [ ] Document the GCP connectivity-first handover contract: preserve outbound
  connectivity first; the stable public IP may converge afterward.
- [ ] Document that active flows may reset and observed recovery timings are
  validation evidence, not an SLA.
- [ ] Update `docs/user/reference/PROVIDER_DATA_SOURCES.md` for
  `betternat_gcp_gateway_status`.

### Operations And Recovery

- [ ] Add GCP-specific operational checks to `docs/user/operations/OPERATIONS_GUIDE.md`:
  Firestore lease owner, registry freshness, GCP route target, MIG capacity,
  stable address user, and gateway datapath readiness.
- [ ] Update `docs/user/operations/FAILURE_MODES.md` with GCP passive failover,
  proactive handover, Compute/Firestore API errors, MIG repair, and stable
  public identity convergence.
- [ ] Update `docs/user/operations/ROLLBACK_GUIDE.md` with GCP route restore,
  static address cleanup, Firestore record cleanup, and managed IAM cleanup.
- [ ] Update `docs/user/operations/OBSERVABILITY_GUIDE.md` with GCP status and
  doctor behavior, metric interpretation, and support-bundle expectations.
- [ ] Keep SSH as a testing/access option only where explicitly enabled. Do not
  imply production GCP deployments require SSH.

### Terraform Provider Registry Docs

- [ ] Ensure provider docs include a concise overview that says modules are the
  recommended user surface and provider resources are advanced primitives.
- [ ] Add Registry-style docs for `betternat_aws_gateway`.
- [ ] Add Registry-style docs for `betternat_gcp_gateway`.
- [ ] Add Registry-style docs for `betternat_runtime_artifacts`.
- [ ] Add Registry-style docs for `betternat_aws_gateway_status`.
- [ ] Add Registry-style docs for `betternat_gcp_gateway_status`.
- [ ] Make GCP resource docs clear about replacement semantics: updates are
  not in-place for the alpha resource.
- [ ] Avoid exposing maintainer-only artifact override paths as normal user
  examples.

### Terraform Module Registry Docs

- [ ] AWS module README: confirm the first screen is still the recommended AWS
  path and does not over-explain provider internals.
- [ ] AWS module README: add a short link path to Quick Start, limitations,
  operations, and rollback docs.
- [ ] GCP module README: add a "When to use this module" section for private
  workloads using tagged routes in an existing VPC.
- [ ] GCP module README: add a "Before you apply" checklist for APIs,
  Firestore, runtime IAM, VPC tag, Private Google Access, and cleanup scope.
- [ ] GCP module README: explain default `capacity_repair_mode = "mig"` and
  why `unmanaged` is an escape hatch rather than the GA path.
- [ ] GCP module README: explain stable public identity in user terms,
  including the connectivity-first transition behavior.
- [ ] GCP module examples: keep examples minimal, but ensure each example has
  enough variables and comments to be runnable after release.
- [ ] GCP module release notes: remove "do not publish" language before the
  actual release, and include validation evidence and known limits.

### Release Notes And Cross-Linking

- [ ] Add runtime release notes for the GCP-capable BetterNAT release.
- [ ] Add provider release notes that call out the Terraform surface reset and
  GCP support level.
- [ ] Add AWS module release notes.
- [ ] Add GCP module release notes.
- [ ] Cross-link release notes to the exact quick start and limitations pages.
- [ ] Verify all user-facing links from root README, docs index, AWS module
  README, GCP module README, and provider docs.

## Suggested Review Order

1. Root `README.md` and `docs/user/README.md`.
2. GCP module README, because it becomes the Terraform Registry first screen.
3. New GCP quick start.
4. Limitations and failure modes.
5. Operations, rollback, observability, and IAM reference.
6. Provider Registry docs.
7. Release notes and final link check.
