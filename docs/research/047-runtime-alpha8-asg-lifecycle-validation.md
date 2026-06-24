# Runtime Alpha8 ASG Lifecycle Validation

Date: 2026-06-24

## Scope

This run validated BetterNAT runtime `v0.1.0-alpha.8` against the public
Terraform Registry provider path after the route-replacement retry hardening.

The run intentionally did not publish another provider version. Provider
`0.1.0-alpha.9` was already present in the Terraform Registry, but its built-in
runtime artifact manifest still accepted the same runtime versions as provider
`0.1.0-alpha.8`. To avoid more alpha version churn, this test used explicit
runtime artifact URL and SHA256 overrides while keeping `betternat_version` at
the supported alpha6 value.

## Environment

- AWS profile: `601427795217_AdministratorAccess`
- Region: `us-west-2`
- AZ: `us-west-2a`
- Run ID: `bnat-ga-asg-alpha8-override-20260624151707`
- Fixture: isolated copy of `examples/terraform-aws-supplemental`
- Provider install path: public Terraform Registry
- Provider version: `nowakeai/betternat` `0.1.0-alpha.9`
- Runtime binaries: GitHub Release `v0.1.0-alpha.8` explicit arm64 URL/SHA256
  overrides
- Bootstrap mode: default `cloud_init`
- Stable egress identity: `stable_egress_ip=true`
- Gateway public IPv4 behavior: `associate_public_ip_address=true`
- Capacity: two Spot gateway nodes plus one Spot private client

## Registry And Apply

Terraform installed provider `0.1.0-alpha.9` from the public Registry and
`terraform validate` passed.

The first direct attempt to set `betternat_version = "v0.1.0-alpha.8"` failed
with a clear provider validation error because provider alpha9's built-in
manifest still listed only `v0.1.0-alpha.2` and `v0.1.0-alpha.6`.

The validation run then used explicit artifact overrides:

```hcl
betternat_version = "v0.1.0-alpha.6"

agent_binary_url    = "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.8/betternat-agent_v0.1.0-alpha.8_linux_arm64"
agent_binary_sha256 = "c29a483b146834091f94855c042cd382c926138b3b1eca04b0daaf7cccea7e1c"
cli_binary_url      = "https://github.com/nowakeai/betternat/releases/download/v0.1.0-alpha.8/betternat_v0.1.0-alpha.8_linux_arm64"
cli_binary_sha256   = "adcba081a7f5a726da028982f2436b335272faae586391c4d2816bc48c89bd22"
```

Terraform apply completed with `16` resources created.

Initial instances:

- private client: `i-03aeae0b078f70007`, private IP `10.88.11.33`
- gateway: `i-036d345054b3dbcd7`, private IP `10.88.1.217`
- gateway: `i-0547162f44fde857b`, private IP `10.88.1.29`

The active gateway initially owned the stable EIP `52.43.198.166`.

Runtime verification on the gateway reported:

```text
betternat version=v0.1.0-alpha.8 commit=d8aba151f978 date=2026-06-24T14:46:08Z go=go1.25.0 os=linux arch=arm64
```

`sudo betternat status` reported one active and one standby gateway, both on
runtime `v0.1.0-alpha.8`, with metrics and peer control status healthy.

## ASG Lifecycle Termination

A continuous one-second private-client probe to
`https://checkip.amazonaws.com` ran during the active gateway termination.

The active gateway `i-0547162f44fde857b` was terminated through the Auto Scaling
Group with desired capacity preserved.

Observed convergence:

- ASG replaced the terminated active with `i-0f6e145dc444054fb`.
- Survivor `i-036d345054b3dbcd7` became active.
- The replacement joined as standby.
- The private route table `rtb-0c02ecf9efb0c197e` pointed at
  `i-036d345054b3dbcd7`.
- The stable EIP `52.43.198.166` reassociated to `i-036d345054b3dbcd7`.

Durable handover history showed the lifecycle-triggered operation completed:

```text
termination-i-0547162f44fde857b-autoscaling-target-lifecycle-state-Terminated-betternat-bnat-ga-asg-alpha8-override-20260624151707-us-west-2a-terminating
status=completed
source=i-0547162f44fde857b
target=i-036d345054b3dbcd7
generation=2
message=handover completed
```

A paired `systemd-stop-*` record was rejected because the terminated peer API
was already refusing connections. That rejection is acceptable for this case
because the ASG lifecycle handover operation completed and the final ownership
state converged.

## Client Probe

Probe summary:

```text
total=136 ok=136 fail=0 longest_fail_run=0 first_ip=52.43.198.166 last_ip=52.43.198.166 unique_ips=52.43.198.166
```

This was materially better than the previous alpha8-provider/alpha6-runtime
soak for the ASG lifecycle case: the active termination produced no recorded
client failures and no transient ordinary-public-IP samples during the probe
window.

## Logs And Caveats

The run still observed transient AWS/DynamoDB context errors in gateway logs,
including registry record publication, lease acquisition, and one canceled
handover-record update during shutdown. The durable lifecycle handover record
nevertheless completed and final route/EIP/lease state was healthy.

Keep broader cloud-mutation retry/backoff hardening open for GA, but the
specific ASG lifecycle proactive handover path passed in this run.

## Cleanup

The first `terraform destroy` hit an eventual-consistency dependency while
deleting the provider-created gateway security group:

```text
DependencyViolation: resource sg-06b686b769b334519 has a dependent object
```

At that point ASG resources were already gone, gateway instances were
terminating, and ENIs were released shortly afterward. A second
`terraform destroy` completed successfully:

```text
Destroy complete! Resources: 10 destroyed.
```

Residual scan after cleanup:

```json
{
  "run_id": "bnat-ga-asg-alpha8-override-20260624151707",
  "eips": 0,
  "autoscaling_groups": 0,
  "dynamodb_tables_matching_run": 0
}
```

The EC2 API still returned only terminated instance history for the run, and
the Resource Groups Tagging API briefly returned one already-deleted subnet
from a stale tag index. No running instances, EIPs, ASGs, DynamoDB tables, or
volumes remained.

## Decision

Do not keep publishing alpha provider releases just to update a formal support
matrix. For the alpha line, document the recommended runtime/provider pair and
explicit override evidence. Promote a formal runtime support matrix before the
production-supportable provider line.
