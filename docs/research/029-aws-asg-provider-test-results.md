# AWS ASG Provider Test Results

Date: 2026-06-20

## Summary

The ASG-first Terraform provider path was validated against real AWS in `us-west-2a`.

Final verified run:

```text
Run ID: bnat-20260620141534
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Capacity: min=1, desired=2, max=3
Market: Spot
AMI: Amazon Linux 2023 arm64
Agent packaging: temporary private S3 presigned URL via cloud-init
```

Final result:

- Terraform apply succeeded.
- One Launch Template and one ASG pool were created for the AZ.
- ASG desired capacity was 2.
- Both ASG instances became `InService` and `Healthy`.
- The provider selected an owner from the ASG pool.
- Stable EIP was allocated and associated to the owner.
- Private default route was replaced to the owner instance.
- Both owner and warm candidate had source/destination check disabled.
- Terraform destroy succeeded.
- Temporary S3 artifact bucket was removed.
- Tagged EC2 instances, EIPs, ENIs, and volumes were zero after cleanup.
- ASG deletion briefly appeared in a post-destroy count due to AWS eventual consistency, then a follow-up query returned no ASG.

## Final Evidence

ASG state:

```json
{
  "Name": "betternat-bnat-20260620141534-us-west-2a",
  "Min": 1,
  "Desired": 2,
  "Max": 3,
  "Instances": [
    {
      "InstanceId": "i-04867dcc5e9534890",
      "LifecycleState": "InService",
      "HealthStatus": "Healthy"
    },
    {
      "InstanceId": "i-0a5bb3dfc6406bf4b",
      "LifecycleState": "InService",
      "HealthStatus": "Healthy"
    }
  ]
}
```

Private default route:

```json
[
  {
    "DestinationCidrBlock": "0.0.0.0/0",
    "InstanceId": "i-04867dcc5e9534890",
    "InstanceOwnerId": "601427795217",
    "NetworkInterfaceId": "eni-0b29c46bd4fa7466e",
    "Origin": "CreateRoute",
    "State": "active"
  }
]
```

Stable EIP:

```json
[
  {
    "AllocationId": "eipalloc-0dd3acd29fb5176cc",
    "PublicIp": "54.148.196.242",
    "InstanceId": "i-04867dcc5e9534890",
    "AssociationId": "eipassoc-0b771a09d57e98979"
  }
]
```

Source/destination check:

```json
[
  {
    "InstanceId": "i-0a5bb3dfc6406bf4b",
    "State": "running",
    "SourceDestCheck": false,
    "PublicIp": "35.162.208.121",
    "PrivateIp": "10.88.1.122"
  },
  {
    "InstanceId": "i-04867dcc5e9534890",
    "State": "running",
    "SourceDestCheck": false,
    "PublicIp": "54.148.196.242",
    "PrivateIp": "10.88.1.241"
  }
]
```

Cleanup check:

```text
instances=0
addresses=0
enis=0
volumes=0
asgs=0 after follow-up eventual-consistency check
```

## Issues Found And Fixed

### 1. LocalStack Auto Scaling Is Not Available On The Tested Hobby Tier

LocalStack health reported Auto Scaling as available, but `CreateAutoScalingGroup` and `UpdateAutoScalingGroup` returned HTTP 501 license errors.

Impact:

- LocalStack can validate provider schema, plugin loading, endpoint wiring, and non-ASG AWS fixture resources.
- It cannot prove the ASG-first apply/destroy lifecycle in the currently available environment.
- Real AWS remains required for ASG acceptance tests.

Fix:

- Updated local testing docs to mark ASG lifecycle as AWS-required under the current LocalStack tier.

### 2. ASG Create Raced IAM Instance Profile Propagation

First ASG AWS attempt failed with:

```text
You must use a valid fully-formed launch template.
Value (...) for parameter iamInstanceProfile.name is invalid.
Invalid IAM Instance Profile name
```

Cause:

- IAM role/profile creation returned before Auto Scaling could validate the instance profile through the Launch Template.

Fix:

- `createAutoScalingGroup` now retries propagation-shaped Launch Template/IAM validation errors.

### 3. Warm Candidate Kept Source/Destination Check Enabled

An intermediate successful ASG apply showed:

```text
owner SourceDestCheck=false
candidate SourceDestCheck=true
```

Cause:

- The provider disabled source/destination check only for the initially selected owner.
- AWS Launch Template primary network interface request does not expose `SourceDestCheck`.
- The agent did not yet self-disable source/destination check at boot.

Fix:

- `betternat-agent` now resolves `local.instance_id = "auto"` through AWS IMDS.
- On AWS, the agent calls `ModifyInstanceAttribute` to disable source/destination check for its own instance before datapath reconcile.
- Runtime IAM policy now includes `ec2:ModifyInstanceAttribute`.

Final AWS run confirmed both ASG nodes had `SourceDestCheck=false`.

## Local Verification

Before final AWS rerun:

```text
go test ./internal/installplan ./internal/agent ./internal/cloud/aws ./internal/tfprovider ./internal/install/aws
./manage verify
terraform validate:
  examples/terraform
  examples/terraform-localstack
  examples/terraform-aws-supplemental
terraform plan:
  examples/terraform
```

The Go command emitted a non-fatal module stat-cache warning on the host, but exited successfully.

## Remaining Gaps

This test proves provider-created ASG infrastructure and initial owner routing.

It does not yet prove:

- agent-driven lease election and failover between ASG nodes,
- ASG owner termination recovery timing,
- stable-IP failover timing under agent control,
- route-only failover timing under agent control,
- LoxiLB datapath health from a private client in the ASG fixture,
- AMI-baked startup time.

Those should be covered by the next AWS supplemental pass after the agent failover loop is wired end to end.

## Decision

The Terraform provider is now ready for ASG-first AWS supplemental testing.

The default product shape should remain:

```text
one ASG appliance pool per AZ
min_size=1
desired_capacity=2
max_size=3
agent disables source/destination check at boot
provider chooses initial owner and routes traffic to it
```
