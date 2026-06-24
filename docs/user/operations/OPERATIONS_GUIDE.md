# BetterNAT Operations Guide

Date: 2026-06-21

## Purpose

This guide describes how to operate a BetterNAT gateway after deployment.

BetterNAT is decentralized:

- each gateway instance runs `betternat-agent`,
- each instance exposes Prometheus metrics,
- local CLI diagnostics run on each gateway node,
- cloud state is inspected through AWS APIs, Terraform outputs, or AWS CLI.

There is no central BetterNAT server.

## First 10 Minutes After Quick Start

After the disposable [Quick Start](../getting-started/QUICK_START.md), use this
short loop before reading the rest of this guide:

```sh
terraform -chdir=examples/terraform-aws-supplemental output
```

On the active gateway node:

```sh
sudo betternat status
sudo betternat doctor --live
sudo betternat handover history --limit 20
curl -fsS http://127.0.0.1:9108/metrics | head
```

From the private test client:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsSI https://example.com
```

Expected:

- exactly one active gateway node,
- at least one healthy standby when `desired_capacity >= 2`,
- route and EIP checks match the active node,
- datapath is ready,
- the private client reaches the public internet,
- no recent failed handover record remains unexplained.

## Daily Health Checklist

For each HA group, verify:

- one active gateway node exists,
- at least one standby gateway node is healthy,
- private route tables point to the active gateway node,
- shared EIP is associated to the active gateway node when stable egress IP is enabled,
- DynamoDB lease owner matches the active gateway node,
- datapath is ready,
- Prometheus metrics are fresh,
- ASG desired capacity equals healthy capacity,
- client egress still returns the expected public IP.

## CLI Commands

Current CLI commands:

```sh
betternat status
betternat status --watch --interval 2s
betternat doctor
betternat doctor --live
betternat failover status
betternat datapath status
betternat datapath ready
betternat handover current
betternat handover history --limit 20
betternat handover inspect <request-id>
betternat support bundle
betternat cost estimate --gb 10240
betternat version
```

Current behavior:

- `status` reads the local daemon by default, uses cached registry and metrics data, and prints fleet, active/standby, version, IP, lease, cache freshness, peer control, registry age, and traffic summary data.
- `status --watch` refreshes the same view until interrupted. Use `--output json` for newline-delimited machine-readable snapshots.
- `doctor` performs static/config-level checks.
- `doctor --live` adds local datapath, IAM runtime permission simulation, lease, route, EIP, source/destination check, Prometheus, and outbound source-IP probe checks where configured. In registry-backed installs, ASG discovery is skipped; use `status` for fleet health.
- `failover status` prints configured HA/failover settings.
- `datapath status` prints configured datapath settings.
- `datapath ready` performs live local datapath checks through LoxiLB.
- `handover current` shows the local daemon's current handover state.
- `handover history` and `handover inspect` read durable handover operation records from the coordination table. History hides stale non-terminal records from older lease generations by default; use `handover history --include-stale` when collecting support evidence.
- `support bundle` creates a local redacted `.tar.gz` with config, daemon status, handover state, metrics, systemd logs, LoxiLB state, and network snapshots for troubleshooting.
- `cost estimate` estimates NAT Gateway processing-cost avoidance.

Important:

- Run datapath commands on the gateway node, usually through SSM Session Manager.
- Gateway-local commands read `/etc/betternat/agent.json` by default. Use
  `--config <path>` only for debugging a non-default config.
- The CLI does not currently connect to a central BetterNAT API.
- The CLI now has a live doctor path for AWS IAM/DynamoDB/route/EIP/datapath/Prometheus checks, but it is still node-local. Fleet-level visibility comes from the coordination registry and per-agent metrics.

## Monitoring Entry Point

Each gateway node exposes Prometheus metrics on:

```text
http://<gateway-private-ip>:9108/metrics
```

Prometheus should scrape every gateway node, not only the active node. Standby
metrics show whether failover capacity is actually ready.

Use [Observability Guide](OBSERVABILITY_GUIDE.md) for metric names, starter
alerts, dashboard files, attribution scope, and Prometheus queries.

For quick incident triage, start with:

```sh
sudo betternat status
sudo betternat doctor --live
sudo betternat handover history --limit 20
curl -fsS http://127.0.0.1:9108/metrics | head
```

## AWS Checks

Use AWS CLI or console to verify cloud state.

ASG:

```sh
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names <asg-name>
```

Route table:

```sh
aws ec2 describe-route-tables \
  --route-table-ids <rtb-id>
```

EIP:

```sh
aws ec2 describe-addresses \
  --allocation-ids <eipalloc-id>
```

DynamoDB lease:

```sh
aws dynamodb get-item \
  --table-name <lease-table> \
  --key '{"pk":{"S":"<ha-group-id>"}}'
```

Instances:

```sh
aws ec2 describe-instances \
  --filters "Name=tag:betternat:gateway,Values=<gateway-id>"
```

The expected healthy state is:

- ASG has desired number of healthy instances.
- DynamoDB lease owner equals current active instance.
- Private route target equals current active instance or active ENI.
- Shared EIP association points to current active instance in stable mode.

## Accessing A Gateway Node

Preferred access path:

```text
AWS Systems Manager Session Manager
```

Default release posture:

- no public SSH required,
- no inbound SSH rule by default,
- no key pair required by default.

Useful commands on the gateway node:

```sh
sudo systemctl status betternat-agent.service
sudo journalctl -u betternat-agent.service -n 200 --no-pager
sudo betternat status
sudo betternat doctor
sudo betternat doctor --live
sudo betternat datapath ready
sudo betternat support bundle
curl -fsS http://127.0.0.1:9108/metrics | head
loxicmd get firewall -o json
loxicmd get conntrack -o json
```

## Support Bundle

`betternat support bundle` is a read-only local collection command for support
and incident review. It writes a `.tar.gz` file and does not upload it anywhere.

The bundle includes:

- redacted `/etc/betternat/agent.json`,
- local daemon `status` and current handover state when the daemon socket is reachable,
- Prometheus metrics snapshot,
- `systemctl status` and recent `journalctl` output for `betternat-agent`,
- LoxiLB inspection output,
- local `ip addr`, `ip route`, and nftables snapshots.

The command redacts the peer API auth token from the config. Review the archive
before sharing it outside your organization.

## Egress Probe

From a private client instance:

```sh
curl -fsS https://checkip.amazonaws.com
```

Expected:

- stable mode: output matches the configured shared EIP before and after failover,
- non-stable mode: output may change after failover.

The mode choice affects timing. In AWS validation, non-stable
route-only handover was much faster than stable EIP handover: the visible source
IP switch completed within about `435 ms` at client probe granularity with `0`
failed samples. Stable/no-public-IP handover preserved the shared public IP but
recorded a short timeout window because it also has to move and verify the EIP.

## Failover Interpretation

BetterNAT failover semantics:

- new connections recover after route/EIP takeover,
- active connections may reset,
- stable EIP mode preserves public source IP for new connections,
- non-stable mode may change public source IP,
- non-stable route-only handover is expected to be faster than stable EIP
  handover because it avoids EIP reassociation,
- in the default `cloud_init` path, gateway nodes keep ordinary public IPv4 for
  bootstrap and management reachability; stable mode converges back to the
  shared EIP, but a successful new-flow sample may briefly use a node's
  ordinary public IPv4 during transition,
- observed low-cost AWS tests showed about 12 seconds of outage for owner termination under the tested conditions.

Do not treat the measured timing as a universal SLA. It depends on:

- HA profile,
- instance health signal,
- AWS API latency,
- ASG replacement timing,
- datapath readiness,
- client retry behavior.

## Troubleshooting

This section is for incident triage from an operator's point of view. For
metric names, PromQL, dashboard wiring, and Prometheus target debugging, use the
[Observability Guide](OBSERVABILITY_GUIDE.md).

### No Egress From Private Client

Check:

1. Private route table has `0.0.0.0/0` target pointing to the active BetterNAT gateway node.
2. Source/destination check is disabled on the active gateway node.
3. Gateway node security group allows forwarded traffic.
4. LoxiLB datapath is ready.
5. IP forwarding sysctl is enabled.
6. Private source CIDR is included in `datapath.private_cidrs`.
7. Public subnet route table has Internet Gateway route.

### Route Points To Wrong Instance

Check:

1. DynamoDB lease owner.
2. `betternat_route_target_match`.
3. Agent logs around lease acquisition and route replacement.
4. IAM permission for `ec2:ReplaceRoute`.
5. Whether an old gateway node is still renewing lease.

### Failed ASG Lifecycle Handover Record

Check:

1. `betternat handover history --limit 20` for the `termination-*` request.
2. `betternat status` from a surviving gateway node.
3. DynamoDB lease owner and generation.
4. Private route table default route target.
5. Shared EIP association when stable mode is enabled.
6. ASG activity history and replacement instance health.
7. `journalctl -u betternat-agent` around the termination event.

Interpretation:

- if the durable handover record failed but lease, route, EIP, and ASG capacity
  converged, service recovered through the passive fenced takeover path,
- if convergence did not happen after lease expiry, collect a support bundle
  from the surviving node and inspect AWS IAM/API errors such as
  `ec2:ReplaceRoute`, `ec2:AssociateAddress`, or DynamoDB write failures.

### Stable EIP Not Preserved

Check:

1. `ha.public_identity.mode` is `shared_eip`.
2. EIP allocation ID is configured.
3. `betternat_public_identity_match`.
4. IAM permission for `ec2:AssociateAddress`.
5. EIP association in AWS.

### Datapath Not Ready

Check:

1. `betternat datapath ready`.
2. `loxicmd get lbversion -o json`.
3. `loxicmd get firewall -o json`.
4. `loxicmd get conntrack -o json`.
5. Agent logs around LoxiLB reconciliation.

### Metrics Missing

Use the metrics endpoint troubleshooting section in the
[Observability Guide](OBSERVABILITY_GUIDE.md#metrics-endpoint-is-down). The
operations-side check is whether local CLI state and AWS route/EIP state still
show a healthy gateway while monitoring is blind.

## Cleanup

Terraform destroy should remove BetterNAT-managed resources.

After destroy, verify no residual tagged resources:

- VPC fixture resources if BetterNAT created them,
- EIP,
- ENI,
- EBS volume,
- ASG,
- Launch Template,
- DynamoDB table,
- security groups,
- IAM role/profile if managed.

Do not manually delete route tables or EIPs before Terraform destroy unless recovering from a failed deployment and following a rollback procedure.

## Current Gaps

These are known gaps to track:

- `doctor --live` is node-local. Run it on each gateway node or pair it with Prometheus/AWS CLI for fleet-wide review.
- No central CLI command yet aggregates every HA group across AWS accounts, DynamoDB, ASG, datapath, and metrics.
- Proactive `betternat handover start` exists, but there is no complete planned
  drain or rolling-upgrade workflow yet.
