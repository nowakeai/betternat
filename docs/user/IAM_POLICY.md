# BetterNAT IAM Policy

Date: 2026-06-21

## Purpose

This document describes the AWS IAM permissions needed by BetterNAT `v0.1.0-alpha.1`.

There are two IAM surfaces:

- Terraform execution identity: creates and destroys the BetterNAT infrastructure.
- Gateway runtime role: attached to BetterNAT EC2 appliances and used by `betternat-agent`.

## Terraform Execution Identity

The Terraform identity must be allowed to create the AWS resources used by the selected example or existing-VPC install.

For the disposable VPC fixture, that includes:

- VPC, subnets, route tables, routes, internet gateway,
- EC2 instances and launch templates,
- Auto Scaling groups,
- security groups,
- IAM role and instance profile,
- DynamoDB lease table,
- EIP when `stable_egress_ip=true`,
- SSM access for validation commands.

The public Quick Start downloads BetterNAT binaries from GitHub Release assets. It does not require S3 permissions for artifact hosting.

For an existing VPC install, scope can be narrower, but the identity still needs permission to create the BetterNAT appliance stack and update the selected private route tables.

## Gateway Runtime Role

The Terraform provider creates an instance role for BetterNAT gateway appliances.

The runtime role is used for:

- lease/fencing,
- route failover,
- EIP failover,
- source/destination check self-disable,
- runtime diagnostics.

Required runtime actions:

| Action | Why |
| --- | --- |
| `autoscaling:DescribeAutoScalingGroups` | `doctor --live` and ASG health checks. |
| `ec2:AssociateAddress` | Move shared EIP in stable egress IP mode. |
| `ec2:DescribeAddresses` | Verify EIP association and public identity. |
| `ec2:DescribeInstanceAttribute` | Verify source/destination check state. |
| `ec2:DescribeInstances` | Inspect appliance instance state where needed by provider/runtime workflows. |
| `ec2:DescribeRouteTables` | Verify private route ownership. |
| `ec2:ModifyInstanceAttribute` | Disable EC2 source/destination check on the appliance. |
| `ec2:ReplaceRoute` | Move private subnet default route to the active appliance. |
| `dynamodb:DeleteItem` | Release or clean lease records. |
| `dynamodb:GetItem` | Read current lease owner. |
| `dynamodb:UpdateItem` | Acquire and renew lease with conditional writes. |
| `iam:SimulatePrincipalPolicy` | `doctor --live` verifies runtime permissions from inside the appliance. |
| `sts:GetCallerIdentity` | Resolve the current assumed role into an IAM role ARN for diagnostics. |

The provider also attaches:

```text
arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
```

This enables SSH-less access through AWS Systems Manager Session Manager.

## Instance Metadata

BetterNAT launch templates require IMDSv2:

```text
HttpEndpoint = enabled
HttpTokens = required
HttpPutResponseHopLimit = 1
```

The agent uses AWS SDK and IMDSv2-capable metadata access for local instance identity when configured with `local.instance_id = "auto"`.

## Policy Scope

The alpha provider scopes IAM to BetterNAT-created resources where currently practical, but the policy is not yet a final least-privilege production policy.

Before production:

- review all IAM resource ARNs,
- narrow EC2 route/EIP permissions to known route tables and allocation IDs where AWS supports it cleanly,
- narrow DynamoDB permissions to the lease table,
- decide whether `iam:SimulatePrincipalPolicy` remains enabled by default or becomes an optional diagnostics permission.

## Diagnostic Behavior

Run on a gateway appliance:

```sh
betternat doctor --live --config /etc/betternat/agent.json
```

If a required permission is denied:

- `doctor --live` exits nonzero,
- overall status becomes `critical`,
- the IAM check lists missing actions,
- dependent checks such as ASG, route, or EIP may also report AWS access errors.

P0 AWS acceptance tested a temporary explicit deny on `autoscaling:DescribeAutoScalingGroups`; `doctor --live` correctly reported the failure and recovered after the deny was removed.
