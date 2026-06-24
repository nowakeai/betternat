# Provider Alpha8 GA Soak Results

Date: 2026-06-24

## Scope

- Provider: Terraform Registry `nowakeai/betternat` `0.1.0-alpha.8`
- Runtime: `betternat_version = "v0.1.0-alpha.6"`
- AWS profile: `601427795217_AdministratorAccess`
- Region: `us-west-2`
- AZ: `us-west-2a`
- Run ID: `bnat-ga-soak-20260624133429`
- Fixture: isolated copy of `examples/terraform-aws-supplemental`
- Provider install path: public Terraform Registry, no local provider override
- Bootstrap mode: default `cloud_init`
- Gateway public IPv4 behavior: provider default, with
  `associate_public_ip_address` unset
- Stable egress identity: `stable_egress_ip=true`
- Capacity: two Spot gateway nodes plus one Spot private client

OpenTofu Registry propagation was intentionally not treated as a blocker for
this run. Terraform Registry was the authoritative production-preview install
path for the validation.

## Terraform Apply

Terraform installed provider `0.1.0-alpha.8` from the public Registry and the
fixture applied successfully:

```text
Apply complete! Resources: 16 added, 0 changed, 0 destroyed.
```

Created runtime state:

- Auto Scaling Group:
  `betternat-bnat-ga-soak-20260624133429-us-west-2a`
- Stable EIP: `54.184.48.49`
- Private route table: `rtb-0f7fa9f4b46af9069`
- Initial active gateway: `i-01bc958b3e526d472`
- Initial standby gateway: `i-0bf91505f9b092700`
- Replacement standby after ASG lifecycle test: `i-0c729bfacd61d021f`
- Private client: `i-067b8d4b44e9380ea`

Both initial gateway nodes reached ASG `InService`, SSM `Online`, and
registered in the coordination table. The private client also reached SSM
`Online`.

## Baseline Health

Both gateway nodes reported:

- `betternat` and `betternat-agent` version `v0.1.0-alpha.6`
- one active and one standby node in `sudo betternat status`
- healthy daemon metrics and peer control status
- `sudo betternat doctor --live` warning-only status
- critical live checks passing for datapath, IAM, lease, route, public
  identity, source/destination check, Prometheus, and source-IP probe

Expected warnings remained:

- rollback route targets are not captured yet,
- ASG discovery is skipped when the coordination registry is configured.

## Fault Injection Timeline

The private client ran a continuous egress probe to
`https://checkip.amazonaws.com` while the gateway fleet was manipulated.

Events:

- `2026-06-24T14:00:53Z`: standby agent restart on
  `i-0bf91505f9b092700`
- `2026-06-24T14:01:52Z`: active agent restart on
  `i-01bc958b3e526d472`
- `2026-06-24T14:04:06Z`: active LoxiLB Docker container restart on
  `i-0bf91505f9b092700`
- `2026-06-24T14:04:58Z`: manual proactive handover from
  `i-0bf91505f9b092700`
- `2026-06-24T14:05:32Z`: explicit `systemctl stop betternat-agent` on
  active `i-01bc958b3e526d472`
- `2026-06-24T14:06:25Z`: restarted the stopped agent on
  `i-01bc958b3e526d472`
- `2026-06-24T14:06:54Z`: terminated active
  `i-0bf91505f9b092700` through the ASG while preserving desired capacity

## Handover Results

Completed handover records:

- `systemd-stop-1782309714614309813`: active agent restart handover,
  `i-01bc958b3e526d472 -> i-0bf91505f9b092700`, generation `2`
- `1782309900212370477`: manual proactive handover,
  `i-0bf91505f9b092700 -> i-01bc958b3e526d472`, generation `3`
- `systemd-stop-1782309934048435474`: explicit systemd-stop handover,
  `i-01bc958b3e526d472 -> i-0bf91505f9b092700`, generation `4`

Expected rejected records:

- `systemd-stop-1782309655954200025`: standby agent restart attempted to
  prepare a target whose peer API was down and was rejected with connection
  refused.
- `systemd-stop-1782310058321799433`: during ASG termination, the active node
  rejected a paired systemd-stop handover because the terminating peer API was
  already refusing connections.

ASG lifecycle finding:

- `termination-i-0bf91505f9b092700-autoscaling-target-lifecycle-state-Terminated-betternat-bnat-ga-soak-20260624133429-us-west-2a-terminating`
  was recorded as `failed`.
- The target was `i-01bc958b3e526d472`, generation `4`.
- The failure happened while replacing the route:

```text
aws ec2 ReplaceRoute: operation error EC2: ReplaceRoute, https response error StatusCode: 0, RequestID: , canceled, context deadline exceeded
```

The fleet still converged through normal fenced lease takeover:

- termination request started at about `2026-06-24T14:06:55Z`,
- the surviving node logged `TAKING_OVER` and then `ACTIVE` by
  `2026-06-24T14:07:20Z`,
- final lease generation was `5`,
- final active gateway was `i-01bc958b3e526d472`,
- ASG launched replacement standby `i-0c729bfacd61d021f`.

This means ASG lifecycle handling is implemented and final service convergence
worked, but the proactive termination handover path did not complete in this
run. Treat this as a GA hardening item for lifecycle-triggered retry/backoff and
shutdown sequencing.

## Client Probe Result

The client probe ran from `2026-06-24T14:00:06.642Z` through
`2026-06-24T14:19:51.150Z`.

Summary:

```text
total=2591 ok=2575 fail=11 unexpected=5 other=0 longest_fail_run=7 first_ip=54.184.48.49 last_ip=54.184.48.49 switches=6 unique_ips=54.191.247.210,35.88.188.93,34.219.204.104,54.184.48.49
```

Anomalies:

- `1` unexpected ordinary public IP sample during active agent restart,
- `7` consecutive one-second curl timeouts during active LoxiLB restart,
- `2` timeout samples and `2` unexpected ordinary public IP samples during
  manual proactive handover,
- `1` timeout sample during explicit systemd-stop handover,
- `2` unexpected ordinary public IP samples during ASG termination/takeover.

The final probe samples returned the stable EIP `54.184.48.49`.

This confirms that the alpha8 provider and alpha6 runtime converge under the
tested restart and handover events. It also reconfirms the existing stable-mode
caveat: when gateway nodes have ordinary per-node public IPv4 addresses for
bootstrap and management reachability, successful samples can briefly egress
through a node's ordinary public IP during transition. Strict "every successful
sample always returns the shared EIP" semantics likely require a secondary
private IP or secondary ENI egress identity.

## Final Cloud State Before Destroy

Final live state before destroy:

- `sudo betternat status` reported two nodes:
  - active `i-01bc958b3e526d472`, private `10.88.1.234`, public
    `54.184.48.49`,
  - standby `i-0c729bfacd61d021f`, private `10.88.1.165`
- private default route `0.0.0.0/0` in `rtb-0f7fa9f4b46af9069` pointed at
  `i-01bc958b3e526d472`
- stable EIP `54.184.48.49` was associated to `i-01bc958b3e526d472`
- ASG had two `Healthy` `InService` instances
- both daemon status views showed metrics and control API as `ok`

## Destroy And Residual Scan

Terraform destroy completed successfully:

```text
Destroy complete! Resources: 16 destroyed.
```

Residual scan summary:

```json
{
  "asg": 0,
  "ddb_tables": 0,
  "eips": 0,
  "enis": 0,
  "vpcs": 0,
  "security_groups": 0,
  "instances_total": 4,
  "instances_non_terminated": 0,
  "tagged_resource_arns": 4
}
```

Only terminated EC2 instance history remained visible through tag-based
resource listing.

## Result

Provider alpha8 plus runtime alpha6 is validated for the production-preview
Terraform Registry path:

- public Registry install worked without local override,
- default `cloud_init` bootstrap worked without manual standby EIP workarounds,
- route, EIP, lease, daemon status, and ASG capacity converged after multiple
  restart and handover events,
- Terraform destroy and residual scan were clean.

Remaining GA hardening from this run:

- improve ASG lifecycle-triggered proactive handover retry/backoff and shutdown
  sequencing so termination events complete the durable handover operation
  instead of relying on lease-expiry takeover,
- decide whether GA accepts the documented stable-mode transient ordinary
  public-IP caveat or requires secondary private IP/ENI egress identity before
  GA,
- continue IAM negative tests for denied `ReplaceRoute`, `AssociateAddress`,
  and DynamoDB write paths.
