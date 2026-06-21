# P0 Open-Source Release Acceptance Results

Date: 2026-06-21

## Summary

This document records the P0 release-readiness validation for the first BetterNAT open-source alpha path.

Scope:

- release artifact build path,
- bootstrap-based AWS deployment path,
- appliance-local `doctor --live`,
- runtime IAM validation,
- private-subnet egress through BetterNAT,
- Terraform destroy and AWS cleanup.

High-volume traffic was intentionally not tested. All AWS network checks used tiny HTTP probes.

## Environment

```text
Region: us-west-2
AZ: us-west-2a
Fixture: examples/terraform-aws-supplemental
Instance type: t4g.small
Market: Spot
Capacity: desired=2
Release version: v0.1.0-alpha.test
Commit: 5b3fe230711f
```

Release artifacts used in the final passing run:

```text
betternat-agent_v0.1.0-alpha.test_linux_arm64
sha256: 5bd2ba47678b00bebe83f515ba65a2b9a2bfae27015e79a7b6318248f4ea4d98

betternat_v0.1.0-alpha.test_linux_arm64
sha256: e18858fcb81683ced7422564a7537c94a4e170de94e8200e0f510972646b1ddf
```

Temporary artifact buckets were deleted after each run.

## Run 1: bnat-p0-20260621041820

Purpose:

- prove the current AWS fixture can deploy and provide private-subnet egress with release artifacts.

Passed:

- Terraform apply created the isolated VPC fixture and BetterNAT resources.
- ASG reached two `InService` appliances.
- Route and EIP pointed at the active appliance.
- Private client egress worked through the stable EIP.

Observed:

```text
Active instance: i-0035aca5aa795afc0
Standby instance: i-070717dd61529504d
Private client: i-0ee818fa00135dc15
Route table: rtb-0ebc3e1fc706cd9ed
Observed EIP: 44.224.151.98
Private probe: HTTP/2 200
```

Failed:

- the bootstrap path installed `betternat-agent` but did not install the `betternat` CLI on the appliance,
- therefore `doctor --live` could not be run on the gateway.

Fix:

- bootstrap now supports `cli_binary_url`, `cli_binary_sha256`, and installs the CLI to `/usr/local/bin/betternat`,
- Terraform provider and AWS supplemental example now pass the CLI artifact and checksum,
- release build script now builds Linux arm64 and amd64 CLI artifacts.

Cleanup:

- Terraform destroy completed,
- the temporary artifact bucket was deleted,
- EC2 residual check found only terminated test instances.

## Run 2: bnat-p0-20260621043530

Purpose:

- prove CLI bootstrap and run appliance-local live diagnostics.

Passed:

- Terraform apply created the AWS fixture.
- ASG reached two healthy appliances.
- CLI and agent were installed on the active appliance.
- `betternat version` and `betternat-agent --version` returned release metadata.

Observed:

```text
Active instance: i-057995a99e7c82ac0
Standby instance: i-044005cb0e0c0fa96
Private client: i-0e0f707f9a4e112d3
Route table: rtb-0dfe711b8241dd8e9
Observed EIP: 44.226.44.191
```

Failed:

- `doctor --live` correctly returned `critical`,
- runtime IAM was missing:
  - `iam:SimulatePrincipalPolicy`,
  - `autoscaling:DescribeAutoScalingGroups`.

Fix:

- runtime required IAM actions now include:
  - `autoscaling:DescribeAutoScalingGroups`,
  - `iam:SimulatePrincipalPolicy`,
  - `sts:GetCallerIdentity`,
- install-plan IAM policy generation and `doctor --live` required-action checks were updated together.

Cleanup:

- Terraform destroy completed,
- the temporary artifact bucket was deleted.

## Run 3: bnat-p0-20260621044411

Purpose:

- prove the fixed P0 path end to end,
- prove `doctor --live` can detect runtime IAM regression,
- prove cleanup.

Baseline:

```text
Run ID: bnat-p0-20260621044411
Active instance: i-0916d52f2542bea7c
Standby instance: i-0aac92bb4806c2867
Private client: i-00b48808620e3c0ee
Route table: rtb-0ecd9d685e843d5d7
Observed EIP: 52.36.9.40
```

Passed:

- Terraform apply created the isolated AWS fixture.
- ASG reached two healthy `InService` appliances.
- Route target matched the active lease owner.
- EIP association matched the active lease owner.
- `betternat-agent.service` was active.
- CLI and agent versions reported the expected build metadata.
- `doctor --live` exited `0`.
- `doctor --live` reported these key checks as `ok`:
  - datapath,
  - IAM,
  - ASG fleet health,
  - DynamoDB lease,
  - route target,
  - public identity,
  - source/destination check,
  - Prometheus endpoint,
  - source-IP probe.
- Private client egress returned the fixed EIP and `HTTP/2 200`.

Private client probe:

```text
2026-06-21T04:51:41.812Z
52.36.9.40
HTTP/2 200
```

Diagnostic caveat:

- `doctor --live` overall status was `warning`, not `ok`, because static rollback-config validation could not verify local rollback targets from appliance runtime config.
- This did not affect the command exit code or live cloud checks.
- Treat this as a P1 diagnostic UX improvement, not a P0 blocker.

## IAM Negative Test

During run `bnat-p0-20260621044411`, a temporary inline deny policy was attached to the BetterNAT agent role:

```text
Policy: betternat-p0-negative-deny-asg
Denied action: autoscaling:DescribeAutoScalingGroups
Role: betternat-bnat-p0-20260621044411-agent
```

Expected result:

- `doctor --live` should fail without mutating AWS state.

Observed result:

- SSM command returned `Failed`.
- `doctor --live` exited nonzero.
- overall doctor status was `critical`.
- IAM check reported the missing action.
- ASG check reported the explicit deny from AWS.

Recovery:

- the temporary deny policy was deleted,
- `doctor --live` returned to exit `0`,
- live checks returned to `ok` except the known rollback-config warning.

Result: pass.

## Cleanup

Run `bnat-p0-20260621044411` cleanup:

- Terraform destroy completed:

```text
Destroy complete! Resources: 16 destroyed.
```

- temporary artifact bucket was emptied and removed:

```text
601427795217-bnat-p0-20260621044411-artifacts
```

- direct EC2 state check confirmed all run instances were terminated:

```text
i-0916d52f2542bea7c terminated
i-0aac92bb4806c2867 terminated
i-00b48808620e3c0ee terminated
```

The AWS tag index still returned terminated EC2 instance ARNs immediately after cleanup. Direct EC2 state confirmed they were not running.

## Result

P0 release acceptance for the bootstrap-based alpha path is complete enough to proceed to the remaining alpha checklist.

Validated:

- release build artifacts include Linux agent and CLI binaries,
- Terraform provider can deploy the AWS supplemental fixture with artifact checksums,
- appliance bootstrap installs both agent and CLI,
- private-subnet egress works through BetterNAT,
- `doctor --live` works on the appliance,
- `doctor --live` catches runtime IAM regressions,
- Terraform destroy and artifact cleanup work.

Not covered by this P0 pass:

- high-volume traffic,
- AMI packaging,
- long soak testing,
- full stable/non-stable failover matrix,
- Prometheus alert rule validation,
- Grafana dashboard,
- support bundle.

Those remain alpha/production hardening items tracked outside P0.
