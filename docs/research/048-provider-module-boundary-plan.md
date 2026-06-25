# Terraform Provider And Module Boundary Plan

Date: 2026-06-25

## Purpose

BetterNAT currently exposes its v0 install UX through the
`nowakeai/betternat` Terraform provider and the `betternat_gateway` resource.
That provider already implements most of the AWS install workflow. This is
correct for the first release, but the boundary is too broad for the long term:
the provider should keep only the lifecycle behavior Terraform modules cannot
express reliably, while cloud-specific composition, defaults, examples, and
product-shaped install UX should move into Terraform modules.

This plan defines that boundary and gives the next provider/module structure.
Although provider `v0.1.1` has been published, it has not been promoted and has
no known external users. The next release can make breaking Terraform schema
changes if they produce a cleaner long-term multi-cloud design.

The next product iteration should use this plan for two related changes:

1. Split user-facing Terraform install UX into cloud-specific modules.
2. Add initial GCP support behind the existing `nowakeai/betternat` provider
   name.

The provider remains the core implementation surface. The modules become the
default product install surface.

## Compatibility Stance

Do not optimize the next version around backward compatibility with the
published `v0.1.1` provider schema.

The goal is the cleanest long-term Terraform interface before broader
promotion:

- breaking resource renames are allowed,
- breaking field renames are allowed,
- splitting AWS-specific and GCP-specific resources is allowed,
- removing convenience fields that belong in modules is allowed,
- state migration from `v0.1.1` is not required unless it is cheap and does not
  complicate the new design.

The release notes must clearly state that the next release is a Terraform
surface reset before public promotion.

## Provider Versus Module

A Terraform provider extends Terraform with new resource behavior. It owns API
clients, custom CRUD semantics, state migration, validation, and actions that
cannot be represented safely as ordinary Terraform resource composition.

A Terraform module is a reusable Terraform configuration package. It does not
add new provider capabilities. It gives users a simpler input surface, sensible
defaults, examples, outputs, and cloud-specific composition around one or more
providers.

For BetterNAT:

```text
Terraform CLI
  -> BetterNAT cloud module: product-shaped install UX
    -> BetterNAT provider: core BetterNAT lifecycle resources
      -> cloud SDK / coordination backend / route and public identity lifecycle / agent config
```

The module should not reimplement BetterNAT lifecycle logic. It should call the
provider resource.

## Target Split

Keep these responsibilities in the provider:

- Cloud-specific gateway lifecycle resources and schemas.
- Read-only data sources that expose BetterNAT-specific computed information
  or cloud/runtime state that modules should consume but not reimplement.
- Agent config rendering and sensitive config handling.
- Runtime artifact URL/checksum resolution.
- Coordination backend schema and bootstrap contract.
- Route ownership capture, rollback metadata, and destroy-time rollback.
- Route target replacement and cloud API safety checks.
- Shared EIP/public identity ownership where the cloud API requires custom
  lifecycle behavior.
- ASG/lifecycle-hook integration when it is tightly coupled to BetterNAT
  handover or fencing semantics.
- AWS install cleanup that Terraform cannot model deterministically, such as
  waiting on security-group ENI dependencies before deleting provider-managed
  security groups.
- Provider state upgrades and compatibility guards.
- Future provider-neutral abstractions that must be shared by AWS, GCP, and
  other clouds.

Move these responsibilities to modules over time:

- Common AMI image lookup for the default non-AMI bootstrap path.
- Mapping common VPC module outputs into `public_subnet_ids` and
  `private_route_table_ids`.
- Opinionated defaults for production-like installs:
  - `use_spot`
  - `desired_capacity`
  - `max_size`
  - `stable_egress_ip`
  - `bootstrap_mode`
  - `ha_profile`
  - `prometheus_enabled`
- Multi-AZ fan-out where each AZ is still one provider resource internally.
- Scenario-specific examples:
  - existing VPC
  - EKS private nodes
  - stable shared EIP
  - non-stable public IP
  - prebaked AMI
  - disposable full VPC demo
- User-friendly outputs, such as active node IDs, ASG names, route table IDs,
  egress IPs, coordination table name, and gateway security group IDs.
- Optional surrounding infrastructure for examples and tests, such as a sample
  VPC, private client, or SSM-enabled smoke-test instance.
- Registry-facing README, inputs, outputs, resources, dependencies, and example
  layout.

## Why These Stay In The Provider

Terraform modules are declarative composition. They are good at wiring existing
resources together, choosing defaults, reshaping inputs, and exposing outputs.
They are not good at implementing product-specific lifecycle semantics that
require conditional cloud API calls, runtime coordination, verification, or
stateful rollback decisions.

### Route Failover And Rollback

BetterNAT must capture the previous route target, replace the route target
during activation/failover, verify the active target, and restore the old route
on destroy when requested.

A pure module can create `aws_route` or `google_compute_route`, but it cannot
reliably express "capture whatever route existed before this resource took
ownership, then restore it if destroy succeeds" without external imperative
logic. Terraform resources also do not model "this route is normally owned by
the active node elected at runtime" cleanly.

This belongs in provider code because it needs custom read/modify/write,
rollback metadata, cloud-specific error handling, and safety checks.

### Public Identity Handover

AWS shared EIP reassociation and GCP reserved external IP behavior are not just
static Terraform resources in the BetterNAT model. The active runtime node owns
the public identity, and ownership can change because of lease expiry,
proactive handover, ASG/MIG termination, or manual operation.

A module can allocate a static IP, but it should not decide which runtime node
owns it during failover. That decision depends on fencing and live gateway
state. The provider and agent should own this contract.

### Lease, Fencing, And Coordination

The coordination backend is part of BetterNAT's correctness model. It stores
ownership, node registry state, operation records, and handover metadata.

A module can create a DynamoDB table, Firestore collection prerequisites, or a
GCS bucket, but it should not implement conditional ownership transitions or
encode lease semantics in Terraform expressions. Lease/fencing must be tested
as product logic and shared by runtime components.

### Agent Config And Sensitive Runtime Material

The provider renders the exact agent config consumed by `betternat-agent`,
including cloud IDs, coordination backend references, route targets, public
identity settings, HA timing, and peer API authentication material.

Modules should supply high-level inputs. They should not duplicate config JSON
rendering logic or construct sensitive runtime contracts independently. If the
agent config schema changes, one provider implementation should adapt it.

### Runtime Artifact Manifest

The provider owns supported BetterNAT runtime versions and checksums. Modules
should consume a data source for artifact metadata rather than copying release
URLs or SHA256 values.

This keeps artifact integrity policy in one place and avoids module/provider
version drift.

### Lifecycle Hooks And Runtime Termination

ASG lifecycle hooks and GCP MIG termination behavior are only useful when tied
to BetterNAT handover and fencing. A module can create a lifecycle hook, but it
cannot make the terminating node safely transfer active ownership by itself.

The provider should create the cloud hooks when needed, and the runtime agent
should perform handover.

### Destroy Cleanup And Cloud Eventual Consistency

Destroy cleanup often needs imperative waits and diagnostics. The AWS security
group dependency fix is a concrete example: the provider must inspect dependent
ENIs before deleting a security group and retry `DependencyViolation` with
useful error context.

Terraform modules cannot add custom retry loops to arbitrary provider resource
deletes. That logic must stay in provider code.

### Provider State And Schema Validation

The provider must validate cloud-specific invariants and maintain Terraform
state. Modules can validate basic input shapes, but they cannot replace provider
schema validation for the resource lifecycle itself.

## Provider Data Sources

The provider currently exposes no data sources. That is acceptable for the
first release, but it leaves modules and advanced users without a clean
read-only interface for BetterNAT-specific information.

Add data sources when the information is BetterNAT-owned, generated by the
provider, or difficult for a pure module to compute safely. Do not add data
sources just to wrap generic AWS queries that the AWS provider already exposes.

Recommended data sources:

### `betternat_runtime_artifacts`

Purpose: return the release artifact URLs and SHA256 checksums for a supported
BetterNAT runtime version.

Example:

```hcl
data "betternat_runtime_artifacts" "current" {
  version = "v0.1.0"
  os      = "linux"
  arch    = "arm64"
}
```

Possible outputs:

- `agent_binary_url`
- `agent_binary_sha256`
- `cli_binary_url`
- `cli_binary_sha256`
- `loxicmd_binary_url`
- `loxicmd_binary_sha256`

This lets modules and users inspect the provider's built-in artifact manifest
without copying it into Terraform code.

### `betternat_aws_gateway_status`

Purpose: read current control-plane state for an existing gateway without
creating or modifying it.

Example:

```hcl
data "betternat_aws_gateway_status" "egress" {
  name   = "prod-egress-a"
  region = "us-west-2"
}
```

Possible outputs:

- `egress_public_ips`
- `route_targets`
- `active_instance_ids`
- `standby_instance_ids`
- `coordination_table_name`

This should be read-only and best-effort. It should not become a replacement
for `betternat status`, because Terraform data sources are evaluated during
planning and are not a live operational UI.

### `betternat_gcp_gateway_status`

Purpose: read current GCP gateway state once GCP support exists.

Example:

```hcl
data "betternat_gcp_gateway_status" "egress" {
  name       = "prod-egress-a"
  project_id = "gcp-cluster-2"
  region     = "us-west1"
}
```

Possible outputs:

- `active_node_ids`
- `standby_node_ids`
- `egress_public_ips`
- `route_targets`
- `coordination_backend`
- `mig_names`

### `betternat_aws_ami`

Purpose: optionally resolve a BetterNAT-published AMI in the future if public
or private AMI channels become part of the product.

Do not add this until AMI publication is real. For the current non-AMI default
path, users and modules can use the AWS provider's `aws_ami` data source.

### Not Recommended

Avoid provider-owned data sources for generic cloud inventory that the AWS
provider already handles:

- `aws_ami` replacement for ordinary Linux AMIs.
- `aws_vpc` / `aws_subnet` / `aws_route_tables` wrappers.
- AWS pricing lookups.

Those belong in modules or user Terraform, not the BetterNAT provider.

## Clean Provider Resource Shape

The next provider should not keep `betternat_gateway` as the primary resource.
That name made sense while AWS was the only target, but it becomes ambiguous
once GCP support exists. Prefer explicit cloud resources:

```hcl
resource "betternat_aws_gateway" "this" {
  name   = "prod-egress-a"
  region = "us-west-2"

  vpc_id = var.vpc_id

  public_subnet_ids = {
    us-west-2a = var.public_subnet_id
  }

  private_route_table_ids = {
    us-west-2a = var.private_route_table_ids
  }

  private_cidrs = var.private_cidrs

  ami_id        = var.ami_id
  instance_type = var.instance_type

  stable_egress_ip = true
}
```

```hcl
resource "betternat_gcp_gateway" "this" {
  name       = "prod-egress-a"
  project_id = var.project_id
  region     = "us-west1"
  network    = var.network

  zones = {
    us-west1-a = {
      subnetwork  = var.subnetwork
      route_names = var.route_names
    }
  }

  private_cidrs = var.private_cidrs

  image        = var.image
  machine_type = var.machine_type

  stable_egress_ip = true
}
```

This is cleaner than one product-level `betternat_gateway` resource with a
large `cloud` switch and many cloud-specific optional fields.

Remove `betternat_gateway` from the primary provider surface. Keep an
undocumented alias only if it is nearly free and does not complicate the
implementation, tests, or docs. Prefer removal.

## UX Principles

The Terraform UX should have two layers:

1. A cloud module for normal users.
2. A cloud-specific provider resource for module authors and advanced users.

The module should answer: "How do I install BetterNAT in my environment?"

The provider resource should answer: "What is the exact BetterNAT gateway
lifecycle primitive for this cloud?"

Avoid making users choose between dozens of low-level fields in the first
screen. The module should provide the smallest practical install surface and
link to advanced provider resource docs only when needed.

### AWS User UX

Recommended first-screen AWS module:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/aws"
  version = "~> 0.2"

  name   = "prod-egress"
  vpc_id = module.vpc.vpc_id

  azs                     = module.vpc.azs
  public_subnet_ids       = module.vpc.public_subnets
  private_route_table_ids = module.vpc.private_route_table_ids

  private_cidrs = [module.vpc.vpc_cidr_block]
}
```

AWS module defaults:

- `stable_egress_ip = true`
- `use_spot = true`
- `desired_capacity = 2`
- `max_size = 3`
- `instance_type = "t4g.small"` or a documented default selected by benchmark
  evidence
- `bootstrap_mode = "cloud_init"` until AMI publication is real
- `ha_profile = "default"`
- `prometheus_enabled = true`
- `rollback_on_destroy = true`

Advanced AWS module inputs:

- `betternat_version`
- `ami_id` or `ami_filter`
- `associate_public_ip_address`
- `instance_type`
- `capacity`
- `stable_egress_ip`
- `tags`
- explicit artifact URL/checksum overrides for development only

AWS module outputs:

- `egress_public_ips`
- `gateway_ids`
- `active_node_ids`
- `standby_node_ids`
- `asg_names`
- `coordination_table_name`
- `security_group_id`
- `private_route_table_ids`

### GCP User UX

Recommended first-screen GCP module:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/google"
  version = "~> 0.2"

  name       = "prod-egress"
  project_id = var.project_id
  region     = "us-west1"
  network    = module.vpc.network_name

  zones = {
    us-west1-a = {
      subnetwork  = module.vpc.private_subnets["us-west1-a"]
      route_names = ["private-default-a"]
    }
  }

  private_cidrs = module.vpc.private_cidrs
}
```

GCP module defaults should stay conservative until spike evidence exists:

- `stable_egress_ip = false` unless reserved external IP handover is validated.
- `desired_capacity = 2` for HA tests, but document cost impact.
- `machine_type` based on spike throughput evidence.
- `bootstrap_mode = "cloud_init"` or equivalent startup-script install until a
  GCE image is published.
- `coordination_backend = "firestore"` if transaction latency and semantics
  pass validation; otherwise keep it explicit.

GCP module outputs:

- `egress_public_ips`
- `gateway_ids`
- `active_node_ids`
- `standby_node_ids`
- `mig_names`
- `route_names`
- `service_account_email`
- `coordination_backend`

### Provider Resource UX

Provider resources should use explicit cloud names and cloud-native terms where
needed. Do not hide real cloud differences behind generic fields when that
creates ambiguity.

Good:

```hcl
resource "betternat_aws_gateway" "this" {
  vpc_id                  = var.vpc_id
  public_subnet_ids       = var.public_subnet_ids
  private_route_table_ids = var.private_route_table_ids
}
```

```hcl
resource "betternat_gcp_gateway" "this" {
  project_id = var.project_id
  network    = var.network
  zones      = var.zones
}
```

Avoid:

```hcl
resource "betternat_gateway" "this" {
  cloud = "gcp"

  # Mostly-null AWS and GCP fields mixed together.
}
```

The provider resource should remain explicit and unsurprising even if it is
more verbose than the module.

## Module-First User Shape

Users should normally install BetterNAT through modules, not direct provider
resources.

Provider resources are for:

- module authors,
- advanced users who need low-level control,
- provider acceptance tests,
- internal spike work.

Modules are for:

- first-time users,
- existing VPC/EKS/GKE integrations,
- Registry product discovery,
- examples and production defaults.

## AWS Module Shape

Create a separate module repository, likely one of:

- `terraform-aws-betternat`
- `terraform-betternat-aws`

The Registry module source should be AWS-specific:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/aws"
  version = "~> 0.2"

  name   = "prod-egress"
  vpc_id = module.vpc.vpc_id

  public_subnet_ids       = module.vpc.public_subnets
  private_route_table_ids = module.vpc.private_route_table_ids
  azs                     = module.vpc.azs
}
```

The recommended repository name is `terraform-aws-betternat`, because it maps
cleanly to the Registry source `nowakeai/betternat/aws`.

The module can convert common list-shaped VPC outputs into the provider's
map-by-AZ shape:

```hcl
resource "betternat_aws_gateway" "this" {
  for_each = local.az_configs

  name   = "${var.name}-${each.key}"
  region = local.region
  vpc_id = var.vpc_id

  public_subnet_ids = {
    (each.key) = each.value.public_subnet_id
  }

  private_route_table_ids = {
    (each.key) = each.value.private_route_table_ids
  }

  private_cidrs      = var.private_cidrs
  ami_id             = local.ami_id
  instance_type      = var.instance_type
  desired_capacity   = var.desired_capacity
  max_size           = var.max_size
  betternat_version  = var.betternat_version
  stable_egress_ip   = var.stable_egress_ip
  bootstrap_mode     = var.bootstrap_mode
  ha_profile         = var.ha_profile
  prometheus_enabled = var.prometheus_enabled
}
```

## GCP Module Shape

Create a separate GCP module repository when GCP support starts:

- `terraform-google-betternat`

The Registry module source should be:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/google"
  version = "~> 0.2"

  name       = "prod-egress"
  project_id = "gcp-cluster-2"
  region     = "us-west1"
  network    = "gcp-cluster-2-vpc"

  zones = {
    us-west1-a = {
      subnetwork  = "private-a"
      route_names = ["private-default-a"]
    }
  }
}
```

The exact GCP module input shape should follow the GCP spike, but the module
should own these user-experience concerns:

- translating existing VPC/GKE module outputs into BetterNAT provider inputs,
- choosing image/startup-script defaults,
- choosing machine type and MIG sizing defaults,
- exposing stable versus non-stable public identity modes,
- outputting current gateway IPs, MIG names, route names, service account, and
  coordination backend names.

The GCP module should not implement route failover, lease/fencing, or stable IP
handover by itself. Those belong in the provider and runtime agent.

## Next Version Work Plan

The next version should be treated as a provider/module boundary release plus a
GCP alpha foundation. It should intentionally reset the Terraform surface before
promotion. Do not try to make GCP production-ready in the same pass.

### Phase 1: Provider Boundary Cleanup

- Keep the provider source address as `nowakeai/betternat`.
- Replace the primary AWS resource with `betternat_aws_gateway`.
- Add `betternat_gcp_gateway` as the GCP alpha resource once the spike supports
  it.
- Remove or de-emphasize the old `betternat_gateway` resource before broader
  promotion.
- Introduce provider-neutral internal naming where it reduces future GCP
  friction:
  - `AssociateEIP` -> `AttachPublicIdentity`
  - `AllocationID` -> `PublicIdentityID`
  - `SourceDestCheck` -> `ForwardingCapability`
  - `ASGInfo` -> `PoolInfo`
  - `LifecycleAction` -> `TerminationAction`
- Split install planning into cloud-specific packages instead of forcing one
  generic plan to carry false equivalence:
  - `internal/install/aws`
  - `internal/install/gcp`
  - shared lifecycle interfaces only where they are actually common.
- Make provider state reset behavior explicit in release notes.
- Do not add state migration solely for `v0.1.1` compatibility unless it is
  trivial.

### Phase 2: Provider Data Sources

Add read-only data sources before the modules depend on copied constants:

- `betternat_runtime_artifacts`
- `betternat_aws_gateway_status`
- `betternat_gcp_gateway_status`

Defer `betternat_aws_ami` until BetterNAT AMI publication is real.

### Phase 3: AWS Module

- Create and publish the AWS module.
- Move the primary user quick start to the AWS module after AWS module smoke
  tests pass.
- Keep `betternat_aws_gateway` docs as advanced/reference documentation.
- Keep provider examples small and low-level; put full VPC and EKS examples in
  the module.

### Phase 4: GCP Spike

Use the GCP research result as the implementation spike boundary:

- disposable GCP VPC,
- one private client VM without public IP,
- one or two BetterNAT gateway VMs with `canIpForward=true`,
- a route from private subnet egress to the active gateway VM,
- LoxiLB first, nftables fallback if needed,
- Firestore transaction or GCS generation-precondition lease backend,
- optional reserved static external IP test for stable public identity,
- complete cleanup verification.

Acceptance criteria:

- private client reaches public internet through BetterNAT,
- datapath counters increase,
- route replacement moves new-flow egress to standby,
- new-flow recovery time is measured,
- stable public IP behavior is either validated or explicitly scoped out,
- cleanup leaves no static IP, disk, route, firewall rule, service account, or
  coordination object behind.

### Phase 5: GCP Provider Alpha

Only after the spike validates route and public identity behavior:

- Add GCP provider resource support.
- Add GCP module support.
- Add GCP user docs as alpha docs, not GA docs.
- Target low-risk functional testing first; cost-driven rollout should target
  high Cloud NAT data-processing projects after disposable validation.

The current GCP cost research points to `gcp-cluster-2` and `gcp-cluster-1` as
the economic targets. `altllm-dev` may be useful as a low-risk functional
environment, but it is not a strong savings target.

## Provider Refactoring Path

Because `v0.1.1` has not been promoted and has no known users, prefer a clean
breaking refactor over incremental compatibility.

Recommended migration sequence:

1. Rename the AWS resource to `betternat_aws_gateway`.
2. Move AWS convenience/defaulting concerns into `terraform-aws-betternat`.
3. Add provider data sources needed by modules.
4. Add the GCP spike implementation behind `betternat_gcp_gateway`.
5. Publish AWS and GCP modules separately.
6. Make the main user Quick Start prefer the AWS module once the module is
   tested.
7. Keep provider resource docs as reference material for advanced users and
   module authors.
8. Move example-only surrounding AWS infrastructure out of provider examples and
   into module examples.
9. After this reset, return to normal SemVer discipline: patch releases must not
   break schema, supported runtime versions, or state compatibility.

## What Not To Move

Do not move these into modules:

- Handover/failover orchestration.
- Lease/fencing semantics.
- Durable operation records.
- Agent registry and coordination backend contract.
- Runtime artifact checksums.
- Route rollback capture.
- Destroy cleanup that depends on cloud eventual consistency.
- Any behavior that needs custom retry, verification, or state migration beyond
  normal Terraform dependency ordering.

Modules can expose inputs for these behaviors, but the provider should own the
implementation.

## Multi-Cloud Naming

The provider is currently named `nowakeai/betternat`, not `nowakeai/betternat-aws`.
That is the better long-term name if BetterNAT will support more clouds.

Terraform providers can contain multiple resources and multiple cloud-specific
implementations. Add cloud-specific resources:

```hcl
resource "betternat_aws_gateway" "this" {
  # AWS-specific fields
}

resource "betternat_gcp_gateway" "this" {
  # GCP-specific fields
}
```

Do not use one product-level `betternat_gateway` resource with
`cloud = "aws"` or `cloud = "gcp"`. That approach keeps a single name but makes
the schema awkward once AWS and GCP require different inputs.

For the next version, choose the clean cloud-specific resource path:

- Add `betternat_aws_gateway`.
- Add `betternat_gcp_gateway`.
- Remove or hide `betternat_gateway` before public promotion.

Do not rename the provider. `nowakeai/betternat` can remain the cross-cloud
BetterNAT provider even if the first resource was AWS-focused.

For modules, use cloud-specific Registry modules:

```text
nowakeai/betternat/aws
nowakeai/betternat/google
```

That split matches Terraform Registry conventions: one product provider can
back multiple cloud-specific modules.

## Current Recommendation

- Keep the provider source address as `nowakeai/betternat`.
- Do not rename the provider to `betternat-aws`.
- Break the unpromoted `v0.1.1` Terraform schema now if needed to get to a
  cleaner multi-cloud design.
- Use explicit provider resources: `betternat_aws_gateway` and
  `betternat_gcp_gateway`.
- Add an AWS module in the next version and make it the default AWS user install
  path after it passes the same AWS smoke tests as the provider.
- Add GCP support under the same provider name, but keep GCP alpha scoped until
  route failover, public identity behavior, lease backend, and cleanup are
  validated in a disposable GCP environment.
- Publish a GCP module as `nowakeai/betternat/google` once the GCP provider
  resource exists.
- Keep the provider as the advanced and module-author interface.

This keeps the current GA provider usable while giving BetterNAT a cleaner
long-term product surface.
