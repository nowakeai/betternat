# BetterNAT IAM Policy

Date: 2026-06-24

## Purpose

This document describes the cloud IAM permissions needed by BetterNAT Terraform
install paths and gateway runtimes.

There are two AWS IAM surfaces:

- Terraform execution identity: creates and destroys the BetterNAT infrastructure.
- Gateway runtime role: attached to BetterNAT EC2 nodes and used by `betternat-agent`.

## Terraform Execution Identity

The Terraform identity must be allowed to create the AWS resources used by the selected example or existing-VPC install.

For the disposable VPC fixture, that includes:

- VPC, subnets, route tables, routes, internet gateway,
- EC2 instances and launch templates,
- Auto Scaling groups,
- Auto Scaling lifecycle hooks,
- security groups,
- IAM role and instance profile,
- DynamoDB lease and coordination tables,
- EIP when `stable_egress_ip=true`,
- SSM access for validation commands.

The public Quick Start downloads BetterNAT binaries from GitHub Release assets. It does not require S3 permissions for artifact hosting.

For an existing VPC install, scope can be narrower, but the identity still needs permission to create the BetterNAT node stack and update the selected private route tables.

## Gateway Runtime Role

The Terraform provider creates an instance role for BetterNAT gateway nodes.

The runtime role is used for:

- lease/fencing,
- agent registry and service discovery through the coordination table,
- route failover,
- EIP failover,
- source/destination check self-disable,
- runtime diagnostics,
- lifecycle hook completion for graceful ASG/Spot termination handling.

Required runtime actions:

| Action | Why |
| --- | --- |
| `autoscaling:CompleteLifecycleAction` | Let the agent finish a termination lifecycle hook after releasing its HA lease. |
| `ec2:AssociateAddress` | Move shared EIP in stable egress IP mode. |
| `ec2:DescribeAddresses` | Verify EIP association and public identity. |
| `ec2:DescribeInstanceAttribute` | Verify source/destination check state. |
| `ec2:DescribeRouteTables` | Verify private route ownership. |
| `ec2:ModifyInstanceAttribute` | Disable EC2 source/destination check on the node. |
| `ec2:ReplaceRoute` | Move private subnet default route to the active node. |
| `dynamodb:DeleteItem` | Release leases and delete local agent registry records on shutdown. |
| `dynamodb:GetItem` | Read current lease owner. |
| `dynamodb:Query` | List fresh agent registry records for fleet status. |
| `dynamodb:UpdateItem` | Acquire and renew lease with conditional writes and refresh local registry records. |
| `iam:SimulatePrincipalPolicy` | `doctor --live` verifies runtime permissions from inside the node. |
| `sts:GetCallerIdentity` | Resolve the current assumed role into an IAM role ARN for diagnostics. |

The provider also attaches:

```text
arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
```

This enables SSH-less access through AWS Systems Manager Session Manager.

## What The Runtime Role Does Not Need

In the normal registry-backed path, runtime fleet status does not require:

- `autoscaling:DescribeAutoScalingGroups`,
- `ec2:DescribeInstances`.

Agents self-register through the DynamoDB coordination table. The CLI reads
coordination records and peer metrics instead of discovering the fleet through
ASG and EC2 inventory APIs.

## Instance Metadata

BetterNAT launch templates require IMDSv2:

```text
HttpEndpoint = enabled
HttpTokens = required
HttpPutResponseHopLimit = 1
```

The agent uses AWS SDK and IMDSv2-capable metadata access for local EC2 identity when configured with `local.node_id = "auto"`. It also polls IMDS for Spot interruption and Auto Scaling target lifecycle state so it can release the local HA lease before completing a termination lifecycle hook.

## Policy Scope

The provider scopes IAM to BetterNAT-created resources where currently
practical, but the policy is intentionally broader than a hand-written
environment-specific least-privilege policy.

Before production:

- review all IAM resource ARNs,
- narrow EC2 route/EIP permissions to known route tables and allocation IDs where AWS supports it cleanly,
- narrow DynamoDB permissions to the coordination table and any legacy lease table still used by an old environment,
- decide whether `iam:SimulatePrincipalPolicy` remains enabled by default or becomes an optional diagnostics permission.

Scope notes:

- `autoscaling:CompleteLifecycleAction` remains required for provider-created
  termination lifecycle hooks.
- `ec2:AssociateAddress` is only needed when stable shared-EIP mode is enabled,
  but the current policy still includes it because stable mode is the default.
- `iam:SimulatePrincipalPolicy` and `sts:GetCallerIdentity` are diagnostics
  permissions used by `doctor --live`; production can make this optional if a
  stricter runtime role is required.
- The policy is acceptable for general use, but strict environments should
  still scope route and EIP actions to the exact managed route tables and
  allocation IDs where AWS supports clean scoping.
Strict environments should review the generated policy scope before using
BetterNAT with existing production route tables. Internal IAM review evidence is
kept under `docs/research/`.

## Diagnostic Behavior

Run on a gateway node:

```sh
betternat doctor --live
```

If a required permission is denied:

- `doctor --live` exits nonzero,
- overall status becomes `critical`,
- the IAM check lists missing actions,
- dependent checks such as route or EIP may also report AWS access errors.

Use [Security Hardening](SECURITY_HARDENING.md) for broader security posture,
network exposure, artifact integrity, and production hardening checklist.

## GCP Alpha Runtime Service Account

`betternat_gcp_gateway` is still an experimental alpha resource. The default
forwarding path does not require a dedicated runtime service account, but the
experimental `enable_agent_ha = true` path does.

When `enable_agent_ha = true`, set:

```hcl
service_account_email = "betternat-runtime@PROJECT_ID.iam.gserviceaccount.com"
manage_runtime_iam    = true
```

Attach the following permissions to that service account, ideally through a
project custom role scoped to the validation project:

| Permission | Why |
| --- | --- |
| `compute.globalOperations.get` | Wait for route delete/create operations. |
| `compute.instances.get` | Read gateway instance metadata during route validation and diagnostics. |
| `compute.instances.use` | Use a gateway instance as a static route `nextHopInstance`. |
| `compute.networks.get` | Read the configured VPC network referenced by route creation. |
| `compute.routes.create` | Move route ownership to the active gateway by creating the replacement static route. |
| `compute.routes.delete` | Delete the old static route before recreating it for the new owner. |
| `compute.routes.get` | Verify the current route target. |
| `datastore.databases.get` | Open the configured Firestore Native database. |
| `datastore.entities.create` | Create lease, registry, and handover documents. |
| `datastore.entities.delete` | Release leases and clean local registry/handover documents. |
| `datastore.entities.get` | Read the current lease, agent registry, and handover records. |
| `datastore.entities.list` | List fresh agent and handover records. |
| `datastore.entities.update` | Renew/transfer leases and update registry/handover records. |

The provider exposes the same list as
`betternat_gcp_gateway.runtime_iam_permissions` so validation stacks can render
custom roles from the provider's runtime contract.

When `manage_runtime_iam = true`, `betternat_gcp_gateway` manages the
project-level custom role `projects/PROJECT_ID/roles/betterNATRuntime` and
adds an IAM binding for:

```text
serviceAccount:SERVICE_ACCOUNT_EMAIL
```

Use this only when the Terraform execution identity is intentionally allowed to
manage project custom roles and project IAM policy bindings. Leave
`manage_runtime_iam = false` when an infra-admin stack owns IAM. The provider
does not create the service account itself.

Current GCP limitations:

- provider-owned GCP custom role and IAM binding lifecycle exists behind the
  explicit `manage_runtime_iam` switch, but still needs live validation in the
  GCP HA smoke,
- Firestore database creation may require project-owner or App Engine/Firestore
  admin permissions outside the gateway runtime role,
- route-only GCP HA does not provide stable public egress IP failover yet.
