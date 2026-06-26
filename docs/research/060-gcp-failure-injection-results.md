# GCP Failure-Injection Results

Date: 2026-06-25

## Summary

Disposable GCP failure-injection validation passed in
`smooth-calling-490406-d9` for run `bnat-gcp-fail-20260625120735`.

The run proved that a previously active GCP gateway reports `DEGRADED` instead
of continuing to report `ACTIVE` when its Firestore/Compute API path becomes
unavailable. The test used a reversible local iptables OUTPUT reject for
`tcp/443` on the active gateway. This is intentionally a fault-injection tool,
not a product datapath path.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Run ID: `bnat-gcp-fail-20260625120735`
- Firestore database: `(default)`
- Initial active route target: `bnat-gcp-fail-20260625120735-gw-a`
- Standby: `bnat-gcp-fail-20260625120735-gw-b`
- Runtime IAM role: `bnatGcpFail20260625120735Runtime`
- Runtime service account:
  `bnat-gcp-fail-20260625120735-r@smooth-calling-490406-d9.iam.gserviceaccount.com`

## Validation

Command:

```sh
scripts/gcp-failure-injection-smoke.sh \
  --project smooth-calling-490406-d9 \
  --zone us-west2-a \
  --name bnat-gcp-fail-20260625120735 \
  --ssh-mode external \
  --wait 90 \
  --output-dir tmp/bnat-gcp-fail-20260625120735/failure-smoke-iptables
```

Before injection, daemon status on `gw-a` reported:

```text
role=active
lifecycle_state=ACTIVE
route_target=bnat-gcp-fail-20260625120735-gw-a
lease_generation=1
route_target_match=true
```

The script inserted this local rule on `gw-a`:

```text
-A OUTPUT -p tcp -m tcp --dport 443 -j REJECT --reject-with icmp-port-unreachable
```

It also attempted to close established `tcp/443` sockets with `ss -K` as a
best-effort accelerator. The active agent then reported:

```text
betternat_ha_step state=DEGRADED
lease_owner=bnat-gcp-fail-20260625120735-gw-a
lease_generation=1
err="verify HA lease before ownership repair: read HA lease: firestore current coordination lease: context deadline exceeded"
```

After the local reject rule was removed, the standby acquired generation `2`
and the route moved to `gw-b`. Final daemon status on `gw-b` reported:

```text
route_target=bnat-gcp-fail-20260625120735-gw-b
lease_generation=2
route_target_match=true
gw-a lifecycle_state=STANDBY
gw-b lifecycle_state=ACTIVE
```

Final live doctor on `gw-b` reported datapath, lease, route, Prometheus, and
source-IP probe checks as `ok`.

## Negative Control

A first attempt used a GCP VPC firewall egress deny for `tcp/443` targeting the
active gateway tag. That did not trigger degradation within 90 seconds because
the agent continued to use already-established Google API connections. The
reusable smoke therefore uses local iptables on the active VM for this specific
fault injection.

## Cleanup

Terraform destroy removed provider-owned Compute, route, runtime service
account, IAM binding, and custom role resources. The temporary operator SSH
firewall and artifact bucket were deleted manually.

Because this run used the shared `(default)` Firestore database, two run-scoped
handover records were deleted explicitly. Final residual scan passed:

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

This closes the live-evidence portion of the P0 gate that requires the GCP
agent to degrade instead of reporting active when Firestore or route ownership
verification cannot be completed. It does not close the broader remaining GCP
GA items for multi-zone behavior, GKE/private-node topologies, and production
Cloud NAT migration.

## Local Control-Plane Fault Injection

Date: 2026-06-26

Additional local fault-injection coverage was added for active GCP route
ownership repair. The important behavior is that a transient `DescribeRoute`
failure must degrade the active supervisor and must not trigger blind route
mutation. A missing route is still repaired because that is a concrete drift
condition.

Covered cases:

- `EnsureOwnershipFenced` returns a route describe error without calling
  `ReplaceRoute` when route describe fails with a transient Compute/API error.
- `EnsureOwnershipFenced` still repairs an explicitly missing route.
- `Supervisor.Step` moves an active gateway to `DEGRADED` when route describe
  fails after lease renewal, without mutating the route.
- A stale local active handover cache is overridden by the fresh coordination
  lease; if ownership moved, the old active forwards to the current owner and
  does not create local handover records.
- A restarted old active with an active-status snapshot but a fresh lease owned
  by another node becomes `STANDBY`, only reconciles local datapath, and does
  not repair route or public identity ownership.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./internal/agent
GOCACHE=$PWD/tmp/go-build go test ./internal/ha
GOCACHE=$PWD/tmp/go-build go test ./internal/cloud/gcp
```

Live GCE clock-skew injection is de-scoped from the current GCP GA gate. The
Firestore lease layer keeps the conservative 2 second skew allowance, and local
decision tests cover acquire, renew, and transfer boundaries.
