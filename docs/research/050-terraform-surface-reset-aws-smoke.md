# Terraform Surface Reset AWS Smoke

Date: 2026-06-25

## Summary

Validated the Terraform surface reset against a disposable AWS environment
without publishing a provider release.

Provider install path:

- local `terraform-provider-betternat` build from the `terraform-surface-reset`
  branch,
- filesystem mirror presenting the provider as `nowakeai/betternat v0.2.0`,
- scratch Terraform copy of `examples/terraform-aws-supplemental`,
- AWS profile `601427795217_AdministratorAccess`,
- region `us-west-2`,
- AZ `us-west-2a`.

No Registry provider or module release was published for this smoke.

## Run

Run ID:

```text
bnat-surface-20260625041202
```

Scratch evidence directory:

```text
tmp/aws-surface-smoke-bnat-surface-20260625041202/
```

Terraform used the local mirror and installed:

```text
nowakeai/betternat v0.2.0
hashicorp/aws v6.52.0
```

Apply result:

```text
Apply complete! Resources: 16 added, 0 changed, 0 destroyed.
```

Created key resources:

| Item | Value |
| --- | --- |
| Gateway | `bnat-surface-20260625041202` |
| ASG | `betternat-bnat-surface-20260625041202-us-west-2a` |
| Initial active node | `i-0790ee121d36e3d83` |
| Standby node | `i-0bff38e48342d3add` |
| Private client | `i-07386a9a943e5f2d6` |
| Private route table | `rtb-0f9e9fb9359575a5a` |
| Stable EIP | `44.246.38.151` |

## Provider Surface

The smoke used the new cloud-specific resource:

```hcl
resource "betternat_aws_gateway" "egress" {
  # ...
}
```

The private route converged to the active instance:

```json
[
  {
    "DestinationCidrBlock": "0.0.0.0/0",
    "InstanceId": "i-0790ee121d36e3d83",
    "InstanceOwnerId": "601427795217",
    "NetworkInterfaceId": "eni-040c06eacd4cb9d6c",
    "Origin": "CreateRoute",
    "State": "active"
  }
]
```

## Runtime Checks

Gateway command succeeded through SSM.

Versions:

```text
betternat version=v0.1.0 commit=75a09d21db77 date=2026-06-24T18:26:07Z go=go1.25.0 os=linux arch=arm64
betternat-agent version=v0.1.0 commit=75a09d21db77 date=2026-06-24T18:26:07Z go=go1.25.0 os=linux arch=arm64
```

`betternat status` showed two healthy nodes:

```text
i-0790ee121d36e3d83  active   Healthy  ACTIVE   v0.1.0  10.88.1.130  44.246.38.151
i-0bff38e48342d3add  standby  Healthy  STANDBY  v0.1.0  10.88.1.55   unknown
```

`betternat doctor --live` returned `status=warning` only because
`rollback_config` reported that rollback route targets were not captured yet in
the node-local diagnostic. Critical live checks passed:

- datapath,
- IAM,
- lease,
- route,
- public identity,
- source/destination check,
- Prometheus,
- source IP probe.

## Private Client Egress

Private client SSM command:

```sh
curl -fsS --max-time 10 https://checkip.amazonaws.com
curl -fsS --max-time 10 https://ifconfig.me
```

Both returned the stable EIP:

```text
44.246.38.151
44.246.38.151
```

## Handover

Started proactive handover from the initial active node:

```sh
sudo betternat handover start --to auto --reason surface-reset-smoke -o json
```

Result:

```json
{
  "schema_version": "v1",
  "request_id": "1782361077924331799",
  "status": "completed",
  "source_node_id": "i-0790ee121d36e3d83",
  "target_node_id": "i-0bff38e48342d3add",
  "lease_generation": 2,
  "message": "handover completed"
}
```

After handover, `betternat status` showed:

```text
i-0790ee121d36e3d83  standby  Healthy  STANDBY  v0.1.0  10.88.1.130  unknown
i-0bff38e48342d3add  active   Healthy  ACTIVE   v0.1.0  10.88.1.55   44.246.38.151
```

The private default route moved to the new active instance:

```json
[
  {
    "DestinationCidrBlock": "0.0.0.0/0",
    "InstanceId": "i-0bff38e48342d3add",
    "InstanceOwnerId": "601427795217",
    "NetworkInterfaceId": "eni-0eca5e42cb78d5fff",
    "Origin": "CreateRoute",
    "State": "active"
  }
]
```

Private client post-handover egress sampled `10/10` responses from the stable
EIP:

```text
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
44.246.38.151
```

## Data Source

Added `betternat_aws_gateway_status` to the scratch Terraform config and ran a
no-change apply.

Result:

```text
data_source_status = "active"
data_source_route_targets = {
  "rtb-0f9e9fb9359575a5a" = "i-0bff38e48342d3add"
}
```

This validates the new AWS status data source against real AWS control-plane
state when supplied with the provider-generated install plan.

## Destroy And Cleanup

Destroy result:

```text
Destroy complete! Resources: 16 destroyed.
```

Residual scan at `2026-06-25T04:24:36Z` for
`bnat-surface-20260625041202` returned empty results for:

- EC2 instances,
- VPCs,
- subnets,
- route tables,
- security groups,
- EIPs,
- launch templates,
- Auto Scaling Groups,
- DynamoDB tables,
- IAM roles,
- instance profiles.

## Decision

AWS smoke passes for the unpublished Terraform surface reset using a local
provider mirror. This is sufficient evidence to keep the PRs open for review
without publishing. Registry install and module Registry validation remain
release-time gates after the provider/module releases exist.
