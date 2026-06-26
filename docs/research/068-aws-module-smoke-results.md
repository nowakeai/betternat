# AWS Module Smoke Results

Date: 2026-06-26

## Summary

Validated the AWS Terraform module against a disposable `us-west-2`
environment using a local `nowakeai/betternat v0.2.0` provider mirror and
current unreleased runtime artifacts.

The smoke covered:

- module apply into a disposable VPC,
- two gateway instances in one ASG,
- private-client egress through BetterNAT,
- stable EIP behavior,
- proactive handover,
- Terraform destroy,
- artifact bucket deletion,
- residual scan.

No public runtime, provider, or module release was published for this test.

## Run

| Item | Value |
| --- | --- |
| Run ID | `bnat-awsmod-20260626130906` |
| AWS account | `601427795217` |
| Region | `us-west-2` |
| AZ | `us-west-2a` |
| AWS profile | `601427795217_AdministratorAccess` |
| Evidence directory | `tmp/bnat-awsmod-20260626130906/` |
| Temporary artifact bucket | `601427795217-bnat-awsmod-20260626130906-artifacts` |

The provider was installed from a local filesystem mirror as
`registry.terraform.io/nowakeai/betternat v0.2.0`.

## Implementation Blocker Found

The first apply failed before creating BetterNAT gateway resources because the
split provider dependency did not include the latest provider behavior for
complete explicit artifact overrides.

Fixes merged before rerunning the smoke:

- main repo PR #4: allow complete explicit agent/CLI artifact URL and SHA256
  overrides to bypass the built-in runtime artifact manifest,
- split provider PR #3: bump the provider dependency to the fixed BetterNAT
  commit.

This keeps normal user installs strict: if a user relies on
`betternat_version` to derive public release artifacts, the version must still
exist in the provider manifest. Maintainer validation runs can use an
unreleased `betternat_version` only when all required artifact URLs and
checksums are supplied explicitly.

## Apply

Terraform apply completed after the provider fix.

Created runtime resources:

| Item | Value |
| --- | --- |
| Gateway | `bnat-awsmod-20260626130906` |
| ASG | `betternat-bnat-awsmod-20260626130906-us-west-2a` |
| Gateway instances | `i-07418f61b47294563`, `i-0f685166cc3926f60` |
| Private client | `i-006f3e4f0985781b7` |
| Private route table | `rtb-0e0d2853a58f45be0` |
| Stable EIP | `54.71.83.128` |
| Coordination table | `betternat-bnat-awsmod-20260626130906-coordination` |

Both gateway instances reached ASG `InService` and SSM `Online`.

## Runtime Checks

Gateway SSM checks succeeded on both gateway instances:

- `betternat version`,
- `betternat-agent --version`,
- `systemctl is-active betternat-agent.service`,
- `betternat status --direct --config /etc/betternat/agent.json --output json`,
- `betternat doctor --live --config /etc/betternat/agent.json`.

`betternat status` showed one active and one standby node. Live doctor checks
for datapath, IAM, lease, route, public identity, source/destination check,
Prometheus, and source IP probe passed. The overall doctor status was
`warning` only for non-blocking diagnostics:

- rollback route targets were not captured yet in the node-local diagnostic,
- ASG discovery is skipped when the coordination registry is configured.

## Private Client Egress

Private client checks through SSM passed:

- 10 consecutive `https://checkip.amazonaws.com` samples returned
  `54.71.83.128`,
- HTTPS HEAD to `https://example.com` returned HTTP 200,
- 1 MiB download from `https://speed.cloudflare.com/__down?bytes=1048576`
  completed,
- DNS lookup for `example.com` succeeded.

## Handover

Before handover:

| Role | Instance |
| --- | --- |
| Active | `i-0f685166cc3926f60` |
| Standby | `i-07418f61b47294563` |

Handover command:

```text
sudo betternat handover start --to auto --reason aws-module-smoke -o json
```

Result:

```text
status=completed
source_node_id=i-0f685166cc3926f60
target_node_id=i-07418f61b47294563
lease_generation=2
```

Private-client probe during handover:

| Metric | Value |
| --- | --- |
| Samples | `120` |
| Interval | `0.5s` |
| Successful samples | `120` |
| Failed samples | `0` |
| Longest observed failure window | `0 samples` |
| Observed public IPs | `54.71.83.128` for all samples |

After handover, `betternat status` showed:

| Role | Instance | Public IP |
| --- | --- | --- |
| Active | `i-07418f61b47294563` | `54.71.83.128` |
| Standby | `i-0f685166cc3926f60` | none |

The private route table default route pointed at the new active instance:

```text
0.0.0.0/0 -> i-07418f61b47294563
```

Post-handover private-client checks returned `54.71.83.128` for five
consecutive samples and HTTPS to `example.com` remained successful.

## Cleanup

Terraform destroy completed:

```text
Destroy complete! Resources: 16 destroyed.
```

The temporary S3 artifact bucket was emptied and deleted.

Residual scan result:

- no live EC2 instances,
- no VPCs,
- no subnets,
- no route tables,
- no security groups,
- no EIPs,
- no launch templates,
- no ASGs,
- no run-scoped DynamoDB tables,
- no run-scoped IAM roles,
- no run-scoped instance profiles,
- artifact bucket absent.

`resourcegroupstaggingapi` still returned the three terminated EC2 instance
ARNs immediately after destroy. The instances were all in `terminated` state,
so they were not live residual resources.

## Release Gate Impact

The AWS module disposable smoke gate is passed for the local `v0.2.0` release
candidate path. Public Registry install validation is still a separate
publication-time gate because the provider and modules were intentionally not
published for this smoke.
