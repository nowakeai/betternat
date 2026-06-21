# Install and Terraform UX

Date: 2026-06-19

## Question

How should BetterNAT be installed and configured so that it is fast, safe, auditable, and production-friendly?

## Short Answer

Make Terraform the primary installation path, but distinguish two product layers:

1. **Terraform module path**: use existing cloud providers such as `hashicorp/aws` to compose EC2, IAM, routes, EIP, and DynamoDB. This is the fastest implementation path.
2. **Custom Terraform provider path**: expose BetterNAT as a first-class resource, so users can configure it like a native managed NAT Gateway.

The product vision should be the custom provider path:

```hcl
resource "betternat_gateway" "egress" {
  name     = "prod-egress"
  provider = betternat.aws

  vpc_id = "vpc-123"

  subnets = {
    public = {
      "us-west-2a" = "subnet-public-a"
      "us-west-2b" = "subnet-public-b"
    }
  }

  private_route_tables = {
    "us-west-2a" = ["rtb-private-a"]
    "us-west-2b" = ["rtb-private-b"]
  }

  high_availability = {
    enabled         = true
    public_identity = "shared_eip"
  }

  observability = {
    prometheus = true
    flow_attribution = true
  }
}
```

The provider, not the user, owns the cloud-specific orchestration.

Implementation model:

```text
Terraform provider exposes product-level resources.
Provider implementation calls AWS/GCP/Azure/AliCloud SDKs.
AMI/bootstrap configures the appliance OS.
betternat-agent handles runtime HA and observability.
betternat CLI assists with discovery, import, doctor, and local debugging.
```

Do not make the CLI secretly mutate infrastructure as the main path. Terraform should remain the source of truth for durable infrastructure. Runtime HA operations are still performed by the agent, but the Terraform provider should understand that runtime ownership model.

All cloud mutations in the provider, agent, and CLI should use official cloud SDKs. Terraform is the declarative interface and state layer, not the runtime control mechanism. Shelling out to `aws`, `gcloud`, `az`, or Terraform should be limited to documentation/debug examples, not product internals.

## UX Principle

The user is changing the default route for private subnets. A bad install can break egress for production workloads.

So the install experience must optimize for:

- explicitness,
- dry-run visibility,
- rollback,
- least privilege,
- blast-radius control,
- post-install verification.

The product should feel easy, but not reckless.

## Recommended User Flow

### 1. Discover

```sh
betternat discover aws \
  --vpc-id vpc-123 \
  --region us-west-2
```

Outputs:

- VPC CIDRs.
- public subnets.
- private subnets.
- route tables and current default routes.
- existing NAT Gateways.
- monthly NAT Gateway processed bytes if CloudWatch access exists.
- candidate per-AZ HA groups.

No mutation.

### 2. Generate Terraform

```sh
betternat init aws \
  --vpc-id vpc-123 \
  --mode ha \
  --output terraform/aws-prod-egress
```

Generates:

- Terraform provider configuration and `betternat_gateway` resource.
- variables file.
- optional backend example.
- comments showing which route tables would be managed.

No mutation.

### 3. Plan

```sh
cd terraform/aws-prod-egress
terraform init
terraform plan
```

or:

```sh
betternat plan
```

`betternat plan` may wrap Terraform, but should still show Terraform's plan.

### 4. Apply

```sh
terraform apply
```

or:

```sh
betternat apply
```

If using `betternat apply`, it should shell out to Terraform or clearly state that Terraform is the underlying executor.

### 5. Verify

```sh
betternat doctor aws
betternat doctor failover --dry-run
betternat top sources
```

### 6. Roll back

```sh
betternat rollback --to previous-route-target
```

or Terraform variable:

```hcl
rollback_mode = "nat_gateway"
```

Rollback must be designed before production use.

## Terraform Module Shape

There are two possible delivery shapes.

### Product target: custom provider

Suggested repo layout:

```text
terraform-provider-betternat/
  internal/provider/
    provider.go
    resource_gateway.go
    resource_gateway_ha_test.go
  internal/cloud/
    aws/
    gcp/
    azure/
    alicloud/
  docs/
    resources/gateway.md
  examples/
    aws-single-az/
    aws-multi-az-ha/
    aws-shared-eip/
```

Provider configuration:

```hcl
terraform {
  required_providers {
    betternat = {
      source  = "nowakeai/betternat"
      version = "~> 0.1"
    }
  }
}

provider "betternat" {
  cloud  = "aws"
  region = "us-west-2"
}
```

Primary resource:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  vpc_id = var.vpc_id

  public_subnet_ids = {
    "us-west-2a" = "subnet-public-a"
    "us-west-2b" = "subnet-public-b"
  }

  private_route_table_ids = {
    "us-west-2a" = ["rtb-private-a"]
    "us-west-2b" = ["rtb-private-b"]
  }

  allowed_private_cidrs = ["10.0.0.0/8"]

  ha {
    enabled          = true
    lease_backend    = "managed"
    private_failover = "replace_route"
    public_identity  = "shared_eip"
  }

  observability {
    prometheus        = true
    flow_attribution  = true
    grafana_dashboard = true
  }
}
```

The provider creates and manages the underlying resources:

- compute instances or autoscaling primitives,
- network interfaces,
- EIPs/public IPs,
- route table entries,
- IAM/RBAC,
- lease backend,
- security groups/firewall rules,
- bootstrap/agent configuration.

### Fast implementation path: module

Before the provider is mature, a Terraform module using existing cloud providers is still useful:

Suggested repo layout:

```text
terraform/
  aws/
    modules/
      appliance/
      ha-group/
      observability/
    examples/
      single-az-simple/
      multi-az-ha/
      shared-eip/
      existing-vpc/
```

Top-level module:

```hcl
module "betternat" {
  source = "github.com/org/betternat//terraform/aws/modules/ha-group"

  name   = "prod-egress"
  vpc_id = var.vpc_id

  public_subnet_ids = {
    "us-west-2a" = "subnet-public-a"
    "us-west-2b" = "subnet-public-b"
  }

  private_route_table_ids = {
    "us-west-2a" = ["rtb-private-a"]
    "us-west-2b" = ["rtb-private-b"]
  }

  allowed_private_cidrs = ["10.0.0.0/8"]

  ha = {
    enabled         = true
    lease_backend   = "dynamodb"
    private_failover = "replace_route"
    public_identity = "shared_eip_reassociation"
  }

  observability = {
    prometheus = true
    grafana_dashboards = true
    ebpf_flow_accounting = false
  }
}
```

The module can later become the reference implementation for the provider's AWS backend, but the public UX should converge on `betternat_gateway`.

## Custom Provider Resource Schema

The provider should expose product-level fields, not raw AWS implementation details as the main UX.

Core fields:

- `name`
- `cloud`
- `region`
- `vpc_id`
- `public_subnet_ids`
- `private_route_table_ids`
- `allowed_private_cidrs`
- `capacity_profile`
- `instance_type`, optional override
- `ha`
- `public_identity`
- `observability`
- `rollback`
- `tags`

Computed fields:

- `gateway_id`
- `status`
- `active_nodes`
- `egress_ips`
- `managed_route_table_ids`
- `lease_backend`
- `prometheus_endpoints`
- `doctor_command`
- `rollback_summary`

The provider can still expose escape hatches:

- `advanced.aws.route_target_type`
- `advanced.aws.ami_id`
- `advanced.aws.iam_permissions_boundary`
- `advanced.aws.existing_dynamodb_table`
- `advanced.aws.existing_eip_allocation_ids`

But the happy path should not require users to understand every underlying AWS resource.

## Terraform Provider Lifecycle

The custom provider should implement normal Terraform resource lifecycle:

- Create: provision BetterNAT resources and initial route state.
- Read: inspect cloud resources, agent status, route ownership, and expose status.
- Update: safely change config where possible; require replacement for risky topology changes.
- Delete: restore routes or require explicit rollback behavior before removing appliances.
- Import: adopt existing BetterNAT deployments or possibly existing NAT Gateway routes as rollback targets.

Deletion must be conservative. Destroying the resource should not silently remove the only internet egress path for private subnets.

Potential delete policy:

```hcl
rollback_on_destroy = true
rollback_target = "nat-previous"
```

or:

```hcl
allow_destroy_without_rollback = false
```

## Terraform Inputs

Required:

- `name`
- `vpc_id`
- `public_subnet_ids`
- `private_route_table_ids`
- `allowed_private_cidrs`
- `instance_type`
- `ami_id` or `ami_channel`
- `ha.enabled`

Important optional:

- `route_target_type`: `instance` or `eni`.
- `public_identity`: `per_node_eip`, `shared_eip_reassociation`, or `none`.
- `existing_eip_allocation_ids`.
- `fallback_route_targets`.
- `enable_ssm`.
- `ssh_ingress_cidrs`, default empty.
- `create_dynamodb_table`.
- `existing_dynamodb_table_name`.
- `tags`.
- `cost_center_mapping`.
- `eks_clusters_for_metadata`.

## Terraform Outputs

Expose:

- NAT instance IDs.
- NAT ENI IDs.
- EIP allocation IDs and public IPs.
- managed route table IDs.
- current expected route targets.
- DynamoDB lease table name.
- IAM role ARN.
- Prometheus endpoint.
- doctor command hints.
- rollback target summary.

## Resource Ownership Boundaries

This is a critical UX decision.

With a custom provider, the provider owns the underlying cloud resources. Users should not need to directly define `aws_route`, `aws_instance`, `aws_eip`, or `aws_dynamodb_table` unless they choose an advanced bring-your-own mode.

### Should Terraform manage route entries?

Yes, but carefully.

Routes are the highest-risk resource. The module should require the user to explicitly pass route table IDs and should not auto-select every private route table by default.

Bad:

```hcl
manage_all_private_route_tables = true
```

Good:

```hcl
private_route_table_ids = {
  "us-west-2a" = ["rtb-abc"]
  "us-west-2b" = ["rtb-def"]
}
```

### Existing routes

The installer should detect and report existing default routes:

```text
rtb-abc 0.0.0.0/0 currently -> nat-123
rtb-def 0.0.0.0/0 currently -> nat-456
```

Store previous targets for rollback, either in Terraform outputs or agent config.

### Terraform vs runtime route changes

HA runtime will call `ReplaceRoute`, which means runtime state can differ from the route target that was active at resource creation time.

This needs careful design.

Options:

#### Option A: Custom provider understands runtime ownership

Pros:

- Best product UX.
- Terraform resource can report the current active target as computed state.
- Provider can avoid fighting the HA agent.
- Underlying cloud resources remain implementation details.

Cons:

- Requires writing and maintaining provider logic.
- Provider `Read` must distinguish expected runtime movement from real drift.

Recommended product target.

#### Option B: Module owns only initial route, agent mutates at runtime

Pros:

- Simple.
- HA agent can fail over without Terraform.

Cons:

- Later `terraform apply` might try to "correct" the route back to the declared target.

Mitigation:

- Use `lifecycle.ignore_changes` for the active route target where possible.
- Or model active/standby route targets outside Terraform after bootstrap.
- Document clearly that runtime HA owns the active route target.

#### Option C: Terraform never owns active route after bootstrap

Terraform creates instances/IAM/DynamoDB and outputs commands. Agent creates/updates route.

Pros:

- No Terraform drift fight.

Cons:

- Less auditable.
- Harder initial setup.

Recommended:

- Custom provider path: Terraform manages a high-level `betternat_gateway`; provider understands runtime HA state.
- Module path: Terraform manages resources and initial route; HA agent manages active route after bootstrap; route target drift must be ignored or explicitly handled.
- `doctor` reports Terraform/runtime drift explicitly in either mode.

This area needs prototype validation because Terraform resource lifecycle behavior around route target drift matters.

## Why Build A Custom Terraform Provider?

The custom provider is justified if the intended UX is "as easy as native NAT Gateway."

Native AWS style:

```hcl
resource "aws_nat_gateway" "this" {
  subnet_id     = aws_subnet.public.id
  allocation_id = aws_eip.nat.id
}
```

BetterNAT style should be similarly high-level:

```hcl
resource "betternat_gateway" "this" {
  vpc_id                  = var.vpc_id
  public_subnet_ids       = var.public_subnet_ids
  private_route_table_ids = var.private_route_table_ids

  high_availability = true
  stable_egress_ip  = true
  observability     = "flow_attribution"
}
```

Benefits:

- Encapsulates many cloud resources behind one product resource.
- Makes multi-cloud possible behind one conceptual model.
- Can validate dangerous topology before apply.
- Can encode rollback rules.
- Can show product-level computed status.
- Reduces burden on users to understand every cloud primitive.

Costs:

- More engineering than a module.
- Acceptance tests are harder.
- Provider versioning and schema migrations become product responsibilities.
- Must be careful not to hide too much from users in production networking.

Recommended sequencing:

```text
v0 internal:
  module/prototype to prove AWS primitives

v0 public beta:
  custom provider with limited AWS support

v1:
  provider becomes primary UX
  module remains examples/reference
```

## AMI vs Bootstrap

### Prebuilt AMI

Pros:

- Fast boot.
- Repeatable.
- Can include tested kernel/packages.
- Better for production.

Cons:

- Need AMI per region/account sharing strategy.
- Release pipeline required.
- Users may require custom hardening.

### Bootstrap/cloud-init

Pros:

- Transparent.
- Easy for early adopters.
- No AMI distribution problem.

Cons:

- Slower boot.
- More failure points during instance launch.
- Package repo availability affects deployment.

Recommendation:

- v0: support bootstrap for development and AMI for recommended production.
- v1: publish AMIs built by Packer or EC2 Image Builder.

AMI should include:

- `betternat-agent`.
- `nftables`.
- `conntrack-tools`.
- `ethtool`, `iproute2`, `tcpdump` optional.
- `bpftool` when eBPF observability is enabled.
- systemd units.
- sysctl defaults.
- SSM agent.
- Prometheus exporter endpoint.

## Security Defaults

Default to SSM-only administration.

AWS Session Manager supports managing EC2 instances without opening inbound ports, bastion hosts, or SSH keys. That fits this product well.

Defaults:

- No inbound SSH.
- SSM enabled.
- Security group allows required health/metrics traffic only from configured CIDRs.
- IMDSv2 required.
- IAM least privilege:
  - only specific route tables,
  - only specific EIPs,
  - only specific ENIs/instances,
  - only specific DynamoDB lease table.
- All resources tagged with product and HA group.

## IAM Scope

Agent permissions should be scoped to resources by ARN/tag wherever AWS supports it.

Needed categories:

- `ec2:DescribeRouteTables`
- `ec2:ReplaceRoute`
- `ec2:DescribeAddresses`
- `ec2:AssociateAddress` if shared EIP mode.
- `ec2:DescribeInstances`
- `ec2:DescribeNetworkInterfaces`
- ENI attach/detach permissions only if advanced ENI mode.
- DynamoDB `GetItem`, `PutItem`, `UpdateItem`, `DeleteItem` on the lease table.
- CloudWatch metrics/logs if enabled.

The module should output the exact IAM policy for audit.

## Doctor Command

`betternat doctor` is a required product feature, not a nice-to-have.

Checks:

- EC2 source/destination check disabled.
- IP forwarding enabled.
- nftables rules loaded.
- conntrack module loaded.
- conntrack count/max sane.
- route table target matches current active owner.
- EIP association matches current active owner, if configured.
- DynamoDB lease readable/writable.
- agent can renew lease.
- Prometheus endpoint reachable.
- outbound probe succeeds.
- outbound probe source IP matches expected EIP.
- Terraform-managed route tables are exactly the intended set.
- no unexpected public SSH ingress.

Output should be actionable:

```text
FAIL source_dest_check: enabled on eni-123
fix: aws ec2 modify-network-interface-attribute --no-source-dest-check --network-interface-id eni-123
```

## Rollback Design

Rollback must be available before users move production routes.

Rollback targets:

- previous NAT Gateway ID,
- previous instance/ENI target,
- Internet Gateway route if this was a lab,
- manually supplied route target.

Store previous route target:

- in Terraform state/output,
- in generated config,
- and optionally in DynamoDB/SSM parameter as deployment metadata.

Rollback command:

```sh
betternat rollback aws \
  --route-table-id rtb-abc \
  --destination 0.0.0.0/0 \
  --to nat-previous
```

Rollback should also be expressible in Terraform for users who require IaC-only changes.

## Safe Defaults and Guardrails

Require explicit confirmation for:

- modifying route tables,
- replacing NAT Gateway routes,
- shared EIP reassociation,
- disabling source/destination checks on existing instances/ENIs,
- enabling HA route mutation permissions.

Refuse or warn if:

- no rollback target is known,
- route table spans multiple AZs unexpectedly,
- private subnet route would hairpin cross-AZ,
- no DynamoDB lease is configured in HA mode,
- source/destination check remains enabled,
- security group exposes SSH to `0.0.0.0/0`,
- NAT instance type has too little network capacity for estimated traffic.

## Multi-cloud UX

Do not build one universal Terraform module as the main UX.

Use one Terraform provider with cloud-specific backends:

```text
provider "betternat" {
  cloud  = "aws"
  region = "us-west-2"
}

provider "betternat" {
  alias  = "gcp"
  cloud  = "gcp"
  region = "us-central1"
}
```

Internally, provider backends can share the same high-level schema and state machine while implementing route/public-IP/lease operations per cloud.

Keep provider-specific examples and optional reference modules:

```text
terraform/aws
terraform/gcp
terraform/azure
terraform/alicloud
```

Keep CLI UX similar:

```sh
betternat init aws
betternat init gcp
betternat init azure
betternat init alicloud
```

The generated config should reflect provider-specific route and identity models.

## MVP Install Scope

v0 should include:

- custom Terraform provider with AWS backend.
- optional AWS reference module for internal testing/examples.
- Existing VPC support.
- Single-AZ simple mode.
- Per-AZ active/standby mode.
- route failover.
- optional shared EIP.
- DynamoDB lease table.
- SSM-only access.
- bootstrap script or AMI.
- `doctor`.
- rollback metadata.

Out of scope for v0:

- multi-cloud provider backends,
- automatic discovery and modification of all private route tables,
- dynamic ENI movement,
- EKS pod attribution installer,
- custom web UI.

## Decision

Make the product Terraform-first and validation-heavy.

Recommended public install story:

> Install BetterNAT with a Terraform provider resource, verify it with `betternat doctor`, and keep a generated rollback path to your previous NAT Gateway.

Recommended internal architecture:

```text
Terraform provider:
  high-level `betternat_gateway` resource
  cloud-specific AWS/GCP/Azure/AliCloud orchestration
  product-level validation and computed status

Agent:
  runtime HA, lease renewal, route/EIP failover, metrics

CLI:
  discovery, generation, validation, doctor, rollback

Reference modules:
  useful for prototyping and examples, not the long-term primary UX
```

This creates a product that feels fast but remains auditable and production-safe.

## Sources

- Terraform Plugin Framework overview for building custom providers: https://developer.hashicorp.com/terraform/plugin/framework
- Terraform Plugin Framework resources and CRUD lifecycle concepts: https://developer.hashicorp.com/terraform/plugin/framework/resources
- Terraform provider publishing and registry docs: https://developer.hashicorp.com/terraform/registry/providers/publishing
- Terraform AWS `aws_instance` resource: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance
- Terraform AWS `aws_route` resource and warning about standalone route vs inline routes: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
- Terraform AWS `aws_launch_template` resource: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/launch_template
- Terraform AWS `aws_dynamodb_table` resource: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table
- AWS NAT instance docs and source/destination check requirement: https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html
- AWS Systems Manager Session Manager, no inbound ports or SSH keys required: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager.html
- Packer Amazon EBS builder creates EBS-backed AMIs: https://developer.hashicorp.com/packer/integrations/hashicorp/amazon/latest/components/builder/ebs
- EC2 Image Builder creates secure custom AMIs and image pipelines: https://docs.aws.amazon.com/imagebuilder/latest/userguide/what-is-image-builder.html
