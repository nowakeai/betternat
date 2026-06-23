# BetterNAT Operations Guide

Date: 2026-06-21

## Purpose

This guide describes how to operate a BetterNAT gateway after deployment.

The first release is decentralized:

- each gateway instance runs `betternat-agent`,
- each instance exposes Prometheus metrics,
- local CLI diagnostics run on each gateway node,
- cloud state is inspected through AWS APIs, Terraform outputs, or AWS CLI.

There is no central BetterNAT server in the first release.

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
betternat status --config /etc/betternat/agent.json
betternat status --watch --interval 2s
betternat doctor --config /etc/betternat/agent.json
betternat doctor --live --config /etc/betternat/agent.json
betternat failover status --config /etc/betternat/agent.json
betternat datapath status --config /etc/betternat/agent.json
betternat datapath ready --config /etc/betternat/agent.json
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
- `handover history` and `handover inspect` read durable handover operation records from the coordination table.
- `support bundle` creates a local redacted `.tar.gz` with config, daemon status, handover state, metrics, systemd logs, LoxiLB state, and network snapshots for troubleshooting.
- `cost estimate` estimates NAT Gateway processing-cost avoidance.

Important:

- Run datapath commands on the gateway node, usually through SSM Session Manager.
- The CLI does not currently connect to a central BetterNAT API.
- The CLI now has a live doctor path for AWS IAM/DynamoDB/route/EIP/datapath/Prometheus checks, but it is still node-local. Fleet-level visibility comes from the coordination registry and per-agent metrics.

## Metrics Collection

`betternat-agent` exposes Prometheus metrics:

```text
http://<gateway-private-ip>:9108/metrics
```

The endpoint is configured by:

```yaml
observability:
  prometheus:
    listen_address: 0.0.0.0
    listen_port: 9108
```

Prometheus should scrape every gateway instance.

Restrict access to the metrics port with security groups. The endpoint should be reachable from the monitoring network, not from the public internet.

## Key Metrics

Agent and HA:

```text
betternat_agent_up
betternat_agent_build_info
betternat_active
betternat_ha_state
betternat_ha_status_age_seconds
betternat_ha_status_stale
betternat_lease_generation
betternat_lease_owner_match
betternat_lease_seconds_until_expiry
betternat_route_target_match
betternat_public_identity_match
betternat_takeover_attempts_total
betternat_takeover_success_total
betternat_lease_renew_errors_total
```

Datapath and traffic:

```text
betternat_datapath_engine_info
betternat_datapath_ready
betternat_conntrack_entries
betternat_conntrack_udp_entries
betternat_conntrack_established
betternat_loxilb_rule_present
betternat_loxilb_rule_packets_total
betternat_loxilb_rule_bytes_total
betternat_owner_packets_total
betternat_owner_bytes_total
betternat_processed_packets_total
betternat_processed_bytes_total
```

Failover:

```text
betternat_failover_events_total
betternat_failover_duration_seconds
```

## Suggested Alerts

No active gateway node:

```promql
sum by (gateway, ha_group) (betternat_active) != 1
```

HA status stale:

```promql
betternat_ha_status_stale == 1
```

Route target mismatch:

```promql
betternat_route_target_match == 0
```

Stable EIP mismatch:

```promql
betternat_public_identity_match == 0
```

Datapath not ready:

```promql
betternat_datapath_ready == 0
```

Lease renew errors:

```promql
increase(betternat_lease_renew_errors_total[5m]) > 0
```

Repeated takeover attempts:

```promql
increase(betternat_takeover_attempts_total[15m]) > 1
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
sudo betternat status --config /etc/betternat/agent.json
sudo betternat doctor --config /etc/betternat/agent.json
sudo betternat doctor --live --config /etc/betternat/agent.json
sudo betternat datapath ready --config /etc/betternat/agent.json
sudo betternat support bundle --config /etc/betternat/agent.json
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

## Failover Interpretation

BetterNAT v0 failover semantics:

- new connections recover after route/EIP takeover,
- active connections may reset,
- stable EIP mode preserves public source IP for new connections,
- non-stable mode may change public source IP,
- observed low-cost AWS tests showed about 12 seconds of outage for owner termination under the tested conditions.

Do not treat the measured timing as a universal SLA. It depends on:

- HA profile,
- instance health signal,
- AWS API latency,
- ASG replacement timing,
- datapath readiness,
- client retry behavior.

## Troubleshooting

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

Check:

1. `betternat-agent` service is active.
2. Security group allows Prometheus to reach port `9108`.
3. Config has nonzero Prometheus listen port.
4. `/metrics` returns HTTP 200 locally.
5. Prometheus target is configured with private IPs.

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

These are known first-release gaps to track:

- `doctor --live` is node-local. Run it on each gateway node or pair it with Prometheus/AWS CLI for fleet-wide review.
- No central CLI command yet aggregates every HA group across AWS accounts, DynamoDB, ASG, datapath, and metrics.
- No bundled Grafana dashboard yet.
- No support bundle command yet.
- No automated planned failover/drain CLI yet.
