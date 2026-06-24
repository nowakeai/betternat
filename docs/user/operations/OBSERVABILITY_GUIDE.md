# BetterNAT Observability Guide

Date: 2026-06-22

## Purpose

This guide describes what BetterNAT exposes for monitoring and debugging.

BetterNAT is decentralized. Each gateway node runs `betternat-agent`, owns its
local datapath reconciliation, and exposes local health data. There is no
central BetterNAT control server.

## What You Can Observe

BetterNAT is designed to answer these questions:

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

Run CLI diagnostics on a gateway node when you need a live point-in-time view:

```sh
sudo betternat status
sudo betternat doctor --live
```

Use the [Operations Guide](OPERATIONS_GUIDE.md#cli-commands) for the full CLI
command list and command-by-command behavior. This guide focuses on the
monitoring signals those commands expose.

### Prometheus Metrics

When `prometheus_enabled = true`, each node exposes:

```text
http://<gateway-private-ip>:9108/metrics
```

Prometheus should scrape every gateway node, not only the current active node. Standby metrics are important because they show whether failover capacity is actually ready.

Restrict access to the metrics port with security groups. It should be reachable from your monitoring network, not from the public internet.

For the disposable Quick Start, generate scrape targets from the ASG output:

```sh
export BETTERNAT_ASG_NAME="$(
  terraform -chdir=examples/terraform-aws-supplemental output -raw asg_name
)"

export BETTERNAT_GATEWAY_INSTANCE_IDS="$(
  aws autoscaling describe-auto-scaling-groups \
    --region "$AWS_REGION" \
    --auto-scaling-group-names "$BETTERNAT_ASG_NAME" \
    --query "AutoScalingGroups[0].Instances[].InstanceId" \
    --output text
)"

aws ec2 describe-instances \
  --region "$AWS_REGION" \
  --instance-ids $BETTERNAT_GATEWAY_INSTANCE_IDS \
  --query "Reservations[].Instances[].PrivateIpAddress" \
  --output text |
  tr '\t' '\n' |
  sed 's/$/:9108/'
```

Minimal scrape job:

```yaml
scrape_configs:
  - job_name: betternat
    static_configs:
      - targets:
          - 10.0.1.10:9108
          - 10.0.1.11:9108
```

In production, prefer EC2 service discovery filtered by the tags you pass to
`betternat_gateway`, or generate scrape targets from Terraform outputs and ASG
membership.

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

Use AWS APIs to cross-check what the agent reports: ASG membership, route table
targets, EIP association, and DynamoDB lease state. The exact AWS CLI commands
live in the [Operations Guide](OPERATIONS_GUIDE.md#aws-checks).

### Gateway Logs And Datapath State

For local debugging, collect gateway logs and LoxiLB state from the node:

```sh
sudo systemctl status betternat-agent.service
sudo journalctl -u betternat-agent.service -n 200 --no-pager
loxicmd get firewall -o json
loxicmd get conntrack -o json
```

Use LoxiLB state for datapath-level debugging and Prometheus metrics for
alerting and historical trends. Use
[`betternat support bundle`](OPERATIONS_GUIDE.md#support-bundle) when you need a
redacted archive for incident review.

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

Takeover attempts without matching successes:

```promql
increase(betternat_takeover_attempts_total[15m]) - increase(betternat_takeover_success_total[15m]) > 0
```

Recent failed lifecycle handover records are not currently a Prometheus metric.
Check the durable records through the CLI:

```sh
betternat handover history --limit 20
```

A failed `termination-*` handover record means the proactive ASG or Spot
termination path did not complete. Check whether `betternat status`, route
owner, EIP owner, and lease owner have still converged through normal lease
takeover before treating it as an active outage.

No traffic through a gateway that should be serving egress. Use this only for
workloads that should have continuous traffic:

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

Use a private-client synthetic probe to validate the full path from private
workload to public internet:

```sh
curl -fsS https://checkip.amazonaws.com
```

This is a black-box complement to Prometheus. Mode-specific interpretation lives
in the [Operations Guide](OPERATIONS_GUIDE.md#egress-probe).

## Attribution Scope

BetterNAT can expose counters grouped by configured owner labels. This is useful when you map known private CIDR ranges to teams, node pools, or workload classes.

Current scope:

- owner attribution is configured explicitly in the agent config under
  `observability.attribution.owners`,
- each owner maps one or more private CIDRs to a stable `owner` label,
- unmatched CIDRs are reported as `owner="unattributed"`,
- exported owner metrics are aggregate counters:
  - `betternat_owner_packets_total{owner, direction}`,
  - `betternat_owner_bytes_total{owner, direction}`,
- BetterNAT does not currently export automatic top-N source IP, destination IP,
  destination hostname, port, protocol, pod, namespace, or tenant labels.

Top owner throughput:

```promql
topk(10, sum by (gateway, ha_group, owner, direction) (rate(betternat_owner_bytes_total[5m])))
```

Top owner packet rate:

```promql
topk(10, sum by (gateway, ha_group, owner, direction) (rate(betternat_owner_packets_total[5m])))
```

Total processed throughput, without owner attribution:

```promql
sum by (gateway, ha_group, direction) (rate(betternat_processed_bytes_total[5m]))
```

BetterNAT does not provide full Kubernetes pod-level attribution by itself. If
private traffic comes from EKS nodes, BetterNAT normally sees node or VPC-level
source addresses after the cluster networking layer. For pod-level attribution,
combine BetterNAT gateway metrics with Kubernetes-side telemetry such as CNI
flow logs, eBPF flow observability, application metrics, or VPC flow logs with
ENI/IP metadata.

BetterNAT also does not provide exact per-tenant billing attribution,
automatic source/destination cardinality analysis, or packet capture at scale.

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

1. whether all gateway nodes are being scraped,
2. `betternat_ha_status_stale`,
3. `betternat_lease_owner_match`,
4. `betternat_route_target_match`,
5. `betternat_public_identity_match` when stable EIP mode is enabled.

For operator-side investigation, use the [Operations Guide
troubleshooting](OPERATIONS_GUIDE.md#troubleshooting) sections for route/EIP
mismatch, failed handover records, and datapath readiness.

## Current Limits

BetterNAT intentionally keeps observability local and simple:

- no central BetterNAT API server,
- no hosted BetterNAT dashboard or managed metric retention,
- no automatic fleet-wide CLI aggregation across accounts and regions,
- no built-in pod-level attribution,
- no long-term metric retention.

Use Prometheus, the starter dashboard, AWS APIs, and node-local CLI checks as
the supported workflow.
