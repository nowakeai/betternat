# Provider Alpha7 Clean AWS Validation

Date: 2026-06-24

## Scope

- Provider: Terraform Registry `nowakeai/betternat` `0.1.0-alpha.7`
- Runtime: `betternat_version = "v0.1.0-alpha.2"`
- AWS profile: `601427795217_AdministratorAccess`
- Region: `us-west-2`
- AZ: `us-west-2a`
- Run ID: `bnat-ga-clean-20260624123001`
- Fixture: isolated copy of `examples/terraform-aws-supplemental`
- Provider install path: public Terraform Registry, no local provider override
- Bootstrap mode: default `cloud_init`
- Gateway public IPv4 behavior: default provider-derived behavior, no manual
  temporary EIP workaround

## Pre-Test Cleanup

The retained test environment `bnat-lifecycle-20260623023753` was cleaned before
the clean validation run.

Residual scan summary:

```json
{
  "asg": 0,
  "instances_total": 3,
  "instances_non_terminated": 0,
  "vpcs": 0,
  "subnets": 0,
  "route_tables": 0,
  "igws": 0,
  "eips": 0,
  "security_groups": 0,
  "tagged_enis": 0,
  "vpc_endpoints": 0,
  "ddb_tables": 0,
  "s3_buckets": 0
}
```

Only terminated EC2 instance history remained.

## Registry And Provider Release Verification

The split provider repository released `v0.1.0-alpha.7` from commit `8710025`.
The GitHub release workflow completed successfully and published Terraform
provider artifacts for `linux_amd64`, `linux_arm64`, and `darwin_arm64`.
Downloaded release assets passed `sha256sum -c` against the published
`SHA256SUMS` file.

After Terraform Registry resync, Terraform installed the provider directly from
the public Registry:

```text
Terraform has been successfully initialized!
Success! The configuration is valid.
```

OpenTofu validation was not completed in this workspace because the `tofu` CLI
is not installed. The OpenTofu Registry API still reported `0.1.0-alpha.6` as
latest during this validation window, so OpenTofu Registry sync remains a
publication follow-up rather than an AWS runtime blocker.

## Terraform Apply

The clean fixture applied successfully:

```text
Apply complete! Resources: 16 added, 0 changed, 0 destroyed.
```

Created runtime state:

- Auto Scaling Group:
  `betternat-bnat-ga-clean-20260624123001-us-west-2a`
- Stable EIP: `44.227.137.203`
- Private route table: `rtb-0fb457dafbce622bf`
- Initial active gateway: `i-06057b9370299c4ad`
- Standby gateway: `i-07e05fdc9ce5e2d19`
- Private client: `i-062448b6f199c167f`

Both gateway nodes reached ASG `InService` and SSM `Online`, and both registered
in the coordination table. The private client also reached SSM `Online`.

## Baseline Egress And Health

Private client egress baseline:

- `10` of `10` samples returned the expected stable EIP `44.227.137.203`.

Active gateway CLI checks:

- `sudo betternat status` reported `2` nodes with one active and one standby.
- `sudo betternat doctor --live` returned warning-only status.
- Critical checks passed for datapath, IAM, lease, route, public identity,
  source/destination check, Prometheus, and outbound source-IP probe.

Known warnings:

- rollback config was not captured on the node,
- ASG discovery was skipped because the coordination registry was configured.

## Proactive Handover

Manual proactive handover was run from the active gateway:

```text
handover completed: i-06057b9370299c4ad -> i-07e05fdc9ce5e2d19 generation=2
```

Post-handover AWS control-plane truth:

- DynamoDB lease generation: `2`
- private default route target: `i-07e05fdc9ce5e2d19`
- stable EIP `44.227.137.203`: associated to `i-07e05fdc9ce5e2d19`
- `sudo betternat status`: `i-07e05fdc9ce5e2d19` active and
  `i-06057b9370299c4ad` standby
- `sudo betternat doctor --live`: warning-only status

Client probe during handover recorded:

```text
total=238 ok=236 fail=1 unexpected=2 longest_fail_run=1 first_ip=44.227.137.203 last_ip=44.227.137.203 switches=2
```

The single failed sample was a curl timeout. The two unexpected samples returned
the standby node's ordinary public IPv4, `34.214.74.195`, before traffic
converged back to the shared stable EIP.

## Result

The alpha6 bootstrap blocker is resolved for the default non-AMI `cloud_init`
path:

- standby bootstrapped without a manually attached temporary EIP,
- both gateway nodes stayed registered and manageable,
- proactive handover completed,
- route, lease, and EIP ownership converged correctly,
- final private-client egress returned the stable EIP.

There is still a narrower stable-identity hardening issue: when gateway nodes
also have ordinary per-node public IPv4 addresses, stable mode can briefly leak
the target node's ordinary public IPv4 during handover. Strict "all successful
samples always return the shared EIP" semantics likely require moving the
stable egress identity to a secondary private IP or secondary ENI and
configuring the datapath to SNAT private workload traffic to that egress
identity.

This is not the same blocker as alpha6. Alpha6 blocked healthy standby
bootstrap. Alpha7 validates healthy standby bootstrap and convergence, while
leaving strict public identity preservation as a GA hardening decision.

## Destroy And Residual Scan

Terraform destroy completed successfully:

```text
Destroy complete! Resources: 16 destroyed.
```

Residual scan summary:

```json
{
  "asg": 0,
  "instances_total": 3,
  "instances_non_terminated": 0,
  "vpcs": 0,
  "subnets": 0,
  "route_tables": 0,
  "igws": 0,
  "eips": 0,
  "security_groups": 0,
  "tagged_enis": 0,
  "ddb_tables": 0,
  "s3_buckets": 0
}
```

Only terminated EC2 instance history remained.
