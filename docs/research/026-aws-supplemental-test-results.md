# AWS Supplemental Test Results

Date: 2026-06-20

## Summary

The Terraform-provider-driven AWS supplemental run proved that BetterNAT can create, bootstrap, inspect, and destroy an isolated AWS appliance topology without a prebuilt BetterNAT AMI.

The run exposed several provider/bootstrap issues that were not visible in LocalStack:

- IAM instance profile propagation can race EC2 `RunInstances`.
- User-provided `Name` tags must not create duplicate EC2 `Name` tag keys.
- Stable EIP mode must explicitly associate the newly allocated EIP to the active appliance during create.
- Presigned binary URLs must be shell-quoted inside generated cloud-init scripts.
- BetterNAT reserved tags must not be user-overridable because cleanup depends on them.

All of these were fixed and verified in a follow-up AWS rerun.

## Scope

Run IDs:

```text
bnat-20260620115600: initial provider lifecycle pass and bug discovery
bnat-20260620122935: EIP association verified; bootstrap URL quoting bug discovered
bnat-20260620124248: final successful apply/bootstrap/destroy verification
```

Region and AZ:

```text
Region: us-west-2
AZ: us-west-2a
```

Fixture:

```text
examples/terraform-aws-supplemental
```

Runtime packaging:

- official Amazon Linux 2023 arm64 AMI,
- cloud-init bootstrap,
- temporary Linux arm64 `betternat-agent` binary from a private S3 presigned URL,
- LoxiLB container,
- Spot `t4g.small` active/standby appliances.

No published BetterNAT AMI was required for this pass.

## Attempts

### Attempt 1: IAM Propagation Race

`terraform apply` failed when EC2 rejected the new instance profile during `RunInstances`.

Result:

- provider-created partial resources were identified,
- manual cleanup removed the orphan security group, DynamoDB table, IAM role/profile/policy,
- provider was updated to retry `RunInstances` on IAM instance profile propagation errors.

### Attempt 2: Duplicate EC2 Name Tag

`terraform apply` failed because EC2 rejected duplicate `Name` tag keys in instance tag specifications.

Result:

- `terraform destroy` completed cleanly,
- provider was updated so resource-specific EC2 names override any generic user `Name` tag.

### Attempt 3: Apply Succeeded

`terraform apply` completed and created:

- isolated VPC,
- public subnet,
- private subnet,
- private route table,
- active Spot appliance,
- standby Spot appliance,
- security group,
- IAM role/profile/policy,
- DynamoDB lease table,
- stable-mode EIP allocation.

Observed AWS state:

```text
Active instance: i-089815f9e8b8e108f
Standby instance: i-0942af4abb2a3a247
VPC: vpc-0e3bb87e66640dbff
Public subnet: subnet-00f4e65e50d52b212
Private subnet: subnet-0c3de619d8a40ff3f
Private route table: rtb-0a7d2f254e591f9f3
EIP allocation: eipalloc-059954d05002de906
EIP public IP: 32.186.82.91
```

Confirmed:

- both appliance instances were running as Spot instances,
- source/destination check was disabled on both appliances,
- private default route pointed to the active instance,
- DynamoDB lease table was active.

Open issue found during this attempt:

- the EIP allocation existed but was not associated to the active instance.

That gap means stable egress IP was not complete in the provider-created initial state. The provider now calls `AssociateAddress` with `AllowReassociation=true` after EIP allocation and before route replacement; this requires a follow-up AWS rerun.

### Attempt 4: EIP Association Fixed, Bootstrap Failed

Run ID:

```text
bnat-20260620122935
```

`terraform apply` completed and verified the stable EIP control-plane fix:

```text
Active instance: i-0a6341fe051c4d6e5
Standby instance: i-0b8b2d58d586247de
Private route table: rtb-049dcf4cba263cfd2
EIP allocation: eipalloc-0fa979b9b23a22b16
EIP public IP: 54.218.142.232
```

Confirmed:

- EIP associated to the active instance,
- private default route pointed to the same active instance,
- both appliances were running as Spot instances,
- source/destination check was disabled on both appliances.

SSM inspection then showed cloud-init failed before installing the agent. The generated script contained an unquoted presigned URL, so shell split the query string at `&` and tried to execute `X-Amz-*` fragments as commands.

Fix:

- bootstrap template now shell-quotes download URLs and destination paths,
- bootstrap test now includes URLs with `&`,
- provider binary was rebuilt before rerun.

Destroy completed, but an EIP remained. Root cause was a cleanup lookup bug: EIP cleanup filtered on `ManagedBy=betternat`, while the fixture passed a custom `ManagedBy` tag. The EIP was manually released.

Fix:

- install plan now re-applies reserved BetterNAT tags after user tags,
- EIP cleanup no longer depends on `ManagedBy`; it identifies EIPs by BetterNAT gateway tag and EIP resource name.

### Attempt 5: Final Successful Rerun

Run ID:

```text
bnat-20260620124248
```

`terraform apply` completed and produced:

```text
Active instance: i-0950979a9a125a507
Standby instance: i-0761942a6d6cd80ac
Private route table: rtb-0b6041b44ea9c912f
EIP allocation: eipalloc-0e7373cd4200b45e2
EIP public IP: 52.10.2.80
```

Confirmed AWS control-plane state:

- EIP associated to active instance `i-0950979a9a125a507`,
- private default route pointed to active instance `i-0950979a9a125a507`,
- both appliances were running as Spot instances,
- source/destination check was disabled on both appliances.

## Bootstrap Inspection

SSM was temporarily attached to the runtime role to inspect the instances. The policy was detached before destroy.

Final rerun bootstrap result:

- cloud-init status was `done` on both appliances,
- `/usr/local/bin/betternat-agent` existed and was executable,
- `/usr/local/bin/loxicmd` existed and was executable,
- `betternat-agent.service` was `active`,
- LoxiLB container `loxilb` was running,
- `loxicmd get firewall -o json` returned the expected SNAT rule on both appliances.

Active appliance firewall rule:

```text
sourceIP: 10.88.0.0/16
destinationIP: 0.0.0.0/0
preference: 100
doSnat: true
toIP: 10.88.1.171
onDefault: true
```

Standby appliance firewall rule:

```text
sourceIP: 10.88.0.0/16
destinationIP: 0.0.0.0/0
preference: 100
doSnat: true
toIP: 10.88.1.190
onDefault: true
```

## Destroy And Cleanup

`terraform destroy` completed successfully after the final rerun:

- BetterNAT resource destroy ran,
- VPC resources were destroyed,
- appliance instances were terminated,
- EIP was released,
- IAM role/profile were removed,
- DynamoDB lease table was deleted.

Cleanup verification after destroy:

- no tagged VPC remained,
- tagged instances were terminated,
- no tagged ENI remained,
- no tagged security group remained,
- no tagged EBS volume remained,
- DynamoDB lease table returned not found,
- IAM role returned not found,
- IAM instance profile returned not found.

The temporary S3 artifact bucket was emptied and removed.

## Code Changes From Findings

Implemented from this run:

- retry EC2 `RunInstances` when IAM instance profile propagation is still converging,
- best-effort provider cleanup after create/update failure,
- avoid duplicate EC2 `Name` tags,
- support Spot appliance launches through provider schema and install plan,
- support cloud-init download of a temporary Linux agent binary,
- provide a host `loxicmd` wrapper around the LoxiLB container,
- associate the stable EIP to the active appliance during create,
- shell-quote bootstrap download URLs and paths,
- make BetterNAT reserved tags non-overridable,
- make EIP cleanup robust to custom user tags.

## Current Status

SUP-014 passed for the current non-AMI development path:

- Terraform provider AWS apply/destroy lifecycle passed.
- AL2023 cloud-init bootstrap passed.
- Stable EIP initial association passed.
- Agent and LoxiLB health passed on both active and standby appliances.
- Terraform destroy and explicit cleanup verification passed.

Not covered in this pass:

- workload egress from a private client through the provider-created topology,
- route-only failover timing,
- stable-IP failover timing,
- agent-driven autonomous HA failover,
- AMI packaging.
