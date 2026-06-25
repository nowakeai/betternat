# GCP Stable IP Protocol Results

Date: 2026-06-25

## Summary

Disposable GCP validation in `smooth-calling-490406-d9` proved that the
experimental GCP stable public identity path can preserve the observed public
source IP for a truly private client during passive active-gateway loss.

The same run also found that proactive handover with stable public identity is
not GA-ready yet. The command rejected safely after a transient Compute API
operation polling reset during static address detach, and final route/static-IP
ownership stayed on the old active gateway.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Run ID: `bnat-gcp-stip-20260625152057`
- Firestore database: `(default)`
- Gateway capacity: zonal MIG, target size `2`
- Stable public identity address: `34.94.153.80`
- Terraform fixture: local provider dev override with branch-built
  `betternat` and `betternat-agent` artifacts
- Client shape: GCE VM in the routed private subnet. For the representative
  probe, the client external access config and operator SSH return route were
  removed before failover was triggered.

## Passive Active Loss

The representative test started a long-running client probe, removed temporary
operator access from the client, then stopped the active gateway instance.

Control-plane observations:

```text
15:49:30 route=gw-001 address=gw-001 instances=gw-000 RUNNING
15:49:43 route=gw-001 address=gw-000 instances=gw-000 RUNNING;gw-001 RUNNING
15:49:55 route=gw-000 address=gw-000 instances=gw-000 RUNNING;gw-001 RUNNING
```

The static address moved before the route target. Full route plus static-IP
convergence was visible within about 25 seconds at the polling granularity.

Private-client probe summary:

```text
samples=260
ok=230
failed=30
unique_success_ips=34.94.153.80
ip_switches=0
first_fail=2026-06-25T15:49:14.564Z
last_fail=2026-06-25T15:49:58.516Z
first_recovery_ok=2026-06-25T15:50:00.033Z
first_fail_to_recovery_seconds=45.469
```

This closes the basic "private client keeps stable source IP after passive
active loss" evidence item. It does not prove long-lived connection
preservation.

## Proactive Handover

A private-client probe was also running while the old active was asked to hand
over to the standby:

```sh
sudo betternat handover start \
  --to bnat-gcp-stip-20260625152057-gw-001 \
  --host unix:///run/betternat/agent.sock
```

The handover was rejected:

```text
handover rejected: gw-000 -> gw-001 error=associate EIP ... detach gcp address
... Get ... zone operation ... read tcp 10.100.0.8:60212->142.251.41.10:443:
read: connection reset by peer
```

Post-failure checks showed the route and static address still owned by the old
active gateway. The partial private-client probe had only the stable IP in
successful samples:

```text
samples=137
ok=132
failed=5
unique_success_ips=34.94.153.80
first_fail_to_recovery_seconds=7.580
```

This is a good safety result but not a passing proactive-handover result. GCP
stable-IP proactive handover needs retry/operation-recovery hardening around
Compute API detach/attach operation polling before GA.

## Negative Control

An earlier run in the same environment used a client with its own external IP.
That run observed an intermediate public IP belonging to the client when the
BetterNAT route/static-IP path was unavailable. That evidence is intentionally
not used as product behavior because production private-subnet workloads do not
have client external IPs.

## Test Harness Findings

- The disposable artifact bucket was initially private, but gateway startup
  fetched artifact URLs without credentials. The local fixture had to make the
  bucket public-read. Production bootstrap should use published artifacts,
  signed/authenticated fetches, or another explicit artifact-access contract.
- `gcloud compute instance-groups managed delete-instances` without
  `--no-decrease-target-size` reduced the MIG target size to zero. MIG repair
  tests should delete the VM directly or pass `--no-decrease-target-size`.
- IAP SSH was enabled in the project, but the current account lacked tunnel
  authorization. Temporary external SSH worked for testing.
- A client VM tagged for the BetterNAT default route needs a narrow
  operator-`/32` return route while temporary external SSH is enabled; otherwise
  SSH replies follow the BetterNAT egress route and the session is asymmetric.

## AWS Comparison

This GCP passive-loss result is materially slower than the latest AWS ASG
lifecycle evidence in `docs/research/047-runtime-alpha8-asg-lifecycle-validation.md`,
where the private-client probe recorded `136/136` successful samples, no
failures, and stable EIP continuity during active termination.

Older AWS supplemental evidence recorded about a 12 second client-visible
outage for one owner-termination case. The GCP stable-IP passive stop measured
about 45.5 seconds from first failure to first recovery in this run. GCP GA
should therefore keep failover-duration optimization open even though stable
source-IP continuity is now proven for passive loss.

## Cleanup

Terraform destroy completed:

```text
Destroy complete! Resources: 8 destroyed.
```

The run-scoped artifact bucket was deleted. Six run-scoped Firestore handover
records were deleted manually from the shared `(default)` database.

Final residual scan passed:

```text
instances: 0
routes: 0
firewall-rules: 0
addresses: 0
service-accounts: 0
firestore records: 0
GCP residual scan passed
```

## Gate Impact

Closed for GCP GA readiness:

- private-client stable public source IP across passive active-gateway loss,
- MIG-backed capacity repair plus static-IP takeover cleanup validation.

Still open:

- proactive stable-IP handover success path,
- retry/recovery after partial static address detach/attach failures,
- automated repeatable private-client stable-IP smoke,
- failover-duration optimization,
- multi-zone/regional capacity behavior if kept in GA scope.
