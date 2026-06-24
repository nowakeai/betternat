# BetterNAT Observability Guide

Date: 2026-06-22

## Purpose

This guide describes what BetterNAT exposes for monitoring and debugging in the first alpha.

BetterNAT v0 is decentralized. Each gateway node runs `betternat-agent`, owns its local datapath reconciliation, and exposes local health data. There is no central BetterNAT control server in the first alpha.

## What You Can Observe

The first alpha is designed to answer these questions:

- Is the agent process running?
- Which node is active for an HA group?
- Is the HA status fresh?
- Does the DynamoDB lease match the local owner view?
- Do private route tables point to the expected active target?
- In stable egress IP mode, is the EIP associated with the expected owner?
- Is the LoxiLB datapath ready?
- Are SNAT rule counters increasing?
- Are failover attempts and durations being recorded?
- Does a private client see the expected public egress IP?

## Observability Surfaces

### Terraform State And Outputs

The `betternat_gateway` resource records deployment state that is useful for runbooks and dashboards:

- `status`
- `control_plane_status_json`
- `managed_route_table_ids`
- `egress_public_ips`
- `active_instance_ids`
- `standby_instance_ids`
- `rollback_route_targets_json`

Use these outputs to locate the ASG, route tables, active nodes, and expected public egress identity.

Example:

```sh
terraform output betternat_status
terraform output active_instance_ids
terraform output standby_instance_ids
terraform output egress_public_ips
```

### Local CLI

Run CLI diagnostics on a gateway node, usually through SSM Session Manager:

```sh
sudo betternat status --config /etc/betternat/agent.json
sudo betternat doctor --config /etc/betternat/agent.json
sudo betternat doctor --live --config /etc/betternat/agent.json
sudo betternat failover status --config /etc/betternat/agent.json
sudo betternat datapath status --config /etc/betternat/agent.json
sudo betternat datapath ready --config /etc/betternat/agent.json
```

Use `doctor` for static configuration checks. Use `doctor --live` when you want local datapath, IAM, lease, route, EIP, Prometheus, and egress-probe checks from the node's point of view. Use `status` for registry-backed fleet visibility across active and standby nodes.

### Prometheus Metrics

When `prometheus_enabled = true`, each node exposes:

```text
http://<gateway-private-ip>:9108/metrics
```

Prometheus should scrape every gateway node, not only the current active node. Standby metrics are important because they show whether failover capacity is actually ready.

Restrict access to the metrics port with security groups. It should be reachable from your monitoring network, not from the public internet.

Minimal scrape job:

```yaml
scrape_configs:
  - job_name: betternat
    static_configs:
      - targets:
          - 10.0.1.10:9108
          - 10.0.1.11:9108
```

In production, prefer EC2 service discovery filtered by BetterNAT tags or generate scrape targets from Terraform outputs and ASG membership.

Starter Prometheus alert rules are available at:

```text
examples/prometheus/betternat-alerts.yaml
```

Starter Grafana dashboard JSON is available at:

```text
examples/grafana/betternat-starter-dashboard.json
```

Treat both files as starting points. Tune alert durations, severity labels,
dashboard variables, and routing labels to match your monitoring stack and
incident policy.

### AWS Control Plane

Use AWS APIs or AWS CLI to cross-check what the agent reports:

```sh
aws autoscaling describe-auto-scaling-groups \
  --auto-scaling-group-names <asg-name>

aws ec2 describe-route-tables \
  --route-table-ids <rtb-id>

aws ec2 describe-addresses \
  --allocation-ids <eipalloc-id>

aws dynamodb get-item \
  --table-name <lease-table> \
  --key '{"pk":{"S":"<ha-group-id>"}}'
```

### Appliance Logs And Datapath State

On the node:

```sh
sudo systemctl status betternat-agent.service
sudo journalctl -u betternat-agent.service -n 200 --no-pager
curl -fsS http://127.0.0.1:9108/metrics | head
loxicmd get firewall -o json
loxicmd get conntrack -o json
```

Use LoxiLB state for datapath-level debugging and Prometheus metrics for alerting and historical trends.

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

## Starter Alerts

Exactly one active node per HA group:

```promql
sum by (gateway, ha_group) (betternat_active) != 1
```

Stale HA state:

```promql
betternat_ha_status_stale == 1
```

Route does not match the expected active owner:

```promql
betternat_route_target_match == 0
```

Stable EIP does not match the expected active owner:

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

No traffic through a gateway that should be serving egress:

```promql
rate(betternat_processed_bytes_total[10m]) == 0
```

## Useful Queries

Current active owner:

```promql
betternat_active == 1
```

Datapath engine in use:

```promql
betternat_datapath_engine_info
```

Bytes by configured owner:

```promql
sum by (gateway, ha_group, owner) (rate(betternat_owner_bytes_total[5m]))
```

SNAT rule packet rate by private CIDR:

```promql
sum by (gateway, ha_group, cidr) (rate(betternat_loxilb_rule_packets_total[5m]))
```

Failover duration by phase:

```promql
betternat_failover_duration_seconds
```

## Egress IP Probe

From a private client instance:

```sh
curl -fsS https://checkip.amazonaws.com
```

Expected behavior:

- stable egress IP mode: the result should match the configured EIP before and after failover for new connections,
- non-stable mode: the result may change after failover.

This probe is still useful even when Prometheus is healthy because it validates the full path from private workload to public internet.

## Attribution Scope

BetterNAT can expose counters grouped by configured owner labels. This is useful when you map known private CIDR ranges to teams, node pools, or workload classes.

The first alpha does not provide full Kubernetes pod-level attribution by itself. If private traffic comes from EKS nodes, BetterNAT normally sees node or VPC-level source addresses after the cluster networking layer. For pod-level attribution, combine BetterNAT gateway metrics with Kubernetes-side telemetry such as CNI flow logs, eBPF flow observability, application metrics, or VPC flow logs with ENI/IP metadata.

The first alpha also does not provide exact per-tenant billing attribution, packet capture at scale, or a bundled fleet dashboard.

## Troubleshooting Patterns

### Metrics Endpoint Is Down

Check:

1. `betternat-agent` service status.
2. `/etc/betternat/agent.json` Prometheus listen address and port.
3. Local endpoint with `curl -fsS http://127.0.0.1:9108/metrics`.
4. Security group ingress from the monitoring network.
5. Prometheus target configuration.

### Active Count Is Not One

Check:

1. DynamoDB lease item for the HA group.
2. `betternat_ha_status_stale`.
3. ASG healthy instance count.
4. Agent logs around lease acquisition and renewal.
5. Route target and EIP owner.

### Route Or EIP Mismatch

Check:

1. `betternat_route_target_match`.
2. `betternat_public_identity_match`.
3. IAM permissions for `ec2:ReplaceRoute` and `ec2:AssociateAddress`.
4. AWS route table and EIP association state.
5. Whether an old instance is still running and renewing the lease.

### Datapath Not Ready

Check:

1. `sudo betternat datapath ready --config /etc/betternat/agent.json`.
2. `loxicmd get firewall -o json`.
3. `loxicmd get conntrack -o json`.
4. Agent logs around LoxiLB reconciliation.
5. Appliance service logs if LoxiLB is not reconciling.

## Current Limits

The first alpha intentionally keeps observability local and simple:

- no central BetterNAT API server,
- no hosted BetterNAT dashboard or managed metric retention,
- no automatic fleet-wide CLI aggregation across accounts and regions,
- no built-in pod-level attribution,
- no long-term metric retention.

Use Prometheus, the starter dashboard, AWS APIs, and node-local CLI checks as
the supported first-release workflow.
