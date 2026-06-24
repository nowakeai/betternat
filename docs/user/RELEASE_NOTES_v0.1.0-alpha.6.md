# BetterNAT v0.1.0-alpha.6 Release Notes

Date: 2026-06-24

## Status

`v0.1.0-alpha.6` is a BetterNAT main-repository alpha release used to support
the current Terraform provider and production-preview bootstrap path.

For Terraform users, the recommended provider release is
`nowakeai/betternat` `0.1.0-alpha.7`. The recommended runtime version for the
current alpha install path remains:

```hcl
betternat_version = "v0.1.0-alpha.2"
```

## What Changed

- Added provider-facing support for `bootstrap_mode`.
- Added provider-facing support for `associate_public_ip_address`.
- Updated the default `cloud_init` launch-template behavior so gateway nodes
  can get ordinary auto-assigned public IPv4 addresses for first-boot package,
  Docker image, release artifact, SSM, and control-plane reachability.
- Kept the shared EIP as the intended stable private-workload egress identity
  when `stable_egress_ip=true`.
- Documented the private/future AMI path with
  `bootstrap_mode = "prebaked_ami"`, where stable-EIP deployments can disable
  per-node auto-assigned public IPv4 because runtime dependencies are already
  present.
- Added release evidence for the provider alpha7 clean AWS validation.

## Validation

Provider `0.1.0-alpha.7` was validated against this main-repository release
line in a clean AWS disposable VPC run:

- Terraform installed provider `0.1.0-alpha.7` from the public Terraform
  Registry with no local provider override.
- Terraform apply created `16` resources.
- Both gateway nodes bootstrapped and reached SSM `Online` without manually
  attaching a temporary standby EIP.
- Private client baseline egress returned stable EIP `44.227.137.203` for
  `10` of `10` samples.
- Manual proactive handover completed from `i-06057b9370299c4ad` to
  `i-07e05fdc9ce5e2d19`.
- Post-handover route, lease, EIP ownership, `betternat status`, and
  `betternat doctor --live` converged.
- Terraform destroy completed with `16` resources destroyed.
- Residual scan found only terminated EC2 instance history.

Detailed evidence:

- `docs/research/042-provider-alpha7-clean-aws-validation.md`
- `docs/release/RELEASE_CHECKLIST.md`

## Known Limitations

- This is still an alpha technical preview.
- No NAT Gateway equivalent SLA.
- No active connection preservation.
- No public BetterNAT AMI is published.
- `ami_channel` resolution is not implemented.
- OpenTofu Registry propagation can lag Terraform Registry propagation; verify
  the OpenTofu Registry before relying on a newly published provider version.
- In stable-EIP mode, gateway nodes can also have ordinary per-node public IPv4
  addresses for bootstrap and management reachability. During the alpha7 clean
  AWS validation, proactive handover recorded `1` curl timeout and `2`
  transient samples from the standby node's ordinary public IPv4 before traffic
  returned to the shared EIP. If the GA contract requires every successful
  handover sample to return only the shared EIP, the likely implementation path
  is a secondary private IP or secondary ENI egress identity with LoxiLB SNAT to
  that address.

## Upgrade Notes

- Existing alpha users should treat this as an evaluation release and test in a
  disposable VPC first.
- For Terraform installs, pin provider `0.1.0-alpha.7` and runtime
  `v0.1.0-alpha.2` unless intentionally testing newer main-repository assets.
- If using an ordinary Linux AMI with `cloud_init`, leave
  `associate_public_ip_address` unset unless you have a specific network design
  that provides first-boot package and artifact reachability another way.

## Artifact Integrity

Verify downloads with the attached `SHA256SUMS` file.

This release note is not legal advice.
