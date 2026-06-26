# GCP Connectivity-First And Multi-NIC Results

Date: 2026-06-26

## Summary

GCP multi-NIC should not become the default GCP GA solution just to mimic the
AWS ENI structure.

The live control-plane measurements show that moving a static external IPv4
address through `accessConfig` has the same order of latency on `nic1` as on
`nic0`. Multi-NIC is still useful for a future management-plane separation, but
it does not remove the slow static-address detach/attach operation that affects
stable egress identity handover.

The current GCP direction should remain connectivity-first handover:

1. move private workload routes to the target gateway first;
2. transfer the lease once route ownership is verified;
3. allow a temporary non-stable source IP window;
4. let the new active owner converge the stable public identity afterward.

This matches the product priority: preserving outbound connectivity is more
important than keeping the egress source IP stable at every instant during
handover.

## Official Constraints

- A single GCE network interface can have only one external IPv4 address.
- GCE multiple network interfaces are configured at VM creation time and each
  interface attaches to a distinct VPC network.
- With multiple NICs, `nic0` remains the default route interface; guest OS
  policy routing can be required when traffic must use another interface.

References:

- <https://cloud.google.com/compute/docs/ip-addresses/configure-static-external-ip-address>
- <https://cloud.google.com/vpc/docs/multiple-interfaces-concepts>
- <https://cloud.google.com/compute/docs/instances/create-instance-multiple-nics>

## Live Microbenchmarks

Project: `smooth-calling-490406-d9`
Region: `us-west2`
Zone: `us-west2-a`

All resources were disposable and run-scoped. Final residual scans passed.

### Static External IPv4 Movement

Run: `bnat-gcp-mnic-20260626060035`

Two VMs were created with two NICs:

- `nic0`: management VPC
- `nic1`: data VPC

The test moved regional static external IPv4 addresses through GCE
`add-access-config` and `delete-access-config`.

| Operation | Duration |
| --- | ---: |
| `nic0` attach to A | `5.331s` |
| `nic0` delete from A | `4.665s` |
| `nic0` attach to B | `6.435s` |
| `nic0` delete from B | `6.124s` |
| `nic1` attach to A | `6.184s` |
| `nic1` delete from A | `6.012s` |
| `nic1` attach to B | `6.181s` |
| `nic1` delete from B | `6.208s` |

Observed move cost:

- `nic0` A-to-B delete+attach: `11.100s`
- `nic1` A-to-B delete+attach: `12.193s`

Conclusion: moving the static external IPv4 on `nic1` was not faster than moving
it on `nic0`. Multi-NIC does not solve stable-IP handover latency by itself.

Cleanup:

```text
GCP residual scan passed
```

### Route Mutation

Run: `bnat-gcp-route-20260626060639`

| Operation | Duration |
| --- | ---: |
| create route to A | `9.499s` |
| delete route to A | `6.634s` |
| create route to B | `9.837s` |
| create shadow route to A | `9.821s` |
| delete canonical route to B | `7.088s` |
| recreate canonical route to A | `11.124s` |
| delete shadow route | `6.531s` |

Observed route mutation cost:

- plain delete+create: `16.471s`
- full shadow sequence: `34.564s`
- shadow route effective first-hop change: about `9.821s`

Conclusion: GCP route mutation is also not instant. Shadow-route handover is
useful because it can introduce the new preferred path before deleting the
canonical route, not because the full sequence is shorter.

Cleanup:

```text
GCP residual scan passed
```

## Live BetterNAT Handover

Run: `bnat-gcp-cf-20260626061148`

The fixture used current branch artifacts, MIG-backed capacity repair, GCP
Firestore coordination, stable public identity, and the connectivity-first
handover implementation.

Artifact hashes:

- `betternat-agent`: `a92d71095f57a96720ee7bcad687d84dd3167136f87f7262f38b1b0e13b33aef`
- `betternat`: `f6751b253352e0eb14f92cd81e8a7be67cb56644567b93f30e8d3c1d18a406c1`

Initial state:

- route target: `bnat-gcp-cf-20260626061148-gw-000`
- target: `bnat-gcp-cf-20260626061148-gw-001`
- stable IP: `34.102.98.65`

Handover record:

```json
{
  "status": "completed",
  "source_node_id": "bnat-gcp-cf-20260626061148-gw-000",
  "target_node_id": "bnat-gcp-cf-20260626061148-gw-001",
  "lease_generation": 2,
  "created_at": "2026-06-26T06:23:03.361246Z",
  "updated_at": "2026-06-26T06:23:35.356031Z"
}
```

Client probe:

| Metric | Value |
| --- | ---: |
| Samples | `220` |
| OK | `215` |
| Failed | `5` |
| Longest consecutive failures | `5` |
| First IP | `34.102.98.65` |
| Middle IP | `34.20.217.40` |
| Last IP | `34.102.98.65` |
| IP switches | `2` |

Probe timeline:

```text
index 0   2026-06-26T06:22:53.951Z ok   34.102.98.65
index 26  2026-06-26T06:23:12.403Z ok   34.20.217.40
index 70  2026-06-26T06:23:43.571Z fail curl SSL connection timeout
index 74  2026-06-26T06:23:49.630Z fail curl connect timeout
index 75  2026-06-26T06:23:51.145Z ok   34.102.98.65
```

Interpretation:

- Route handover restored new-flow connectivity early enough that successful
  samples appeared through the target gateway's ephemeral IP before the stable
  IP converged.
- Static-IP convergence still created a short outage window. Counting failed
  samples at 0.5 second intervals gives `2.5s`; using wall-clock timestamps
  from first failed sample to next successful sample gives about `7.6s` because
  each failed curl waited for its own timeout.
- This is materially better than the previous stable-IP handover runs that
  showed about `22s` of failed-sample recovery, but it is still slower than the
  best AWS lifecycle handover evidence.

Final state before destroy:

- route target: `bnat-gcp-cf-20260626061148-gw-001`
- static address user: `bnat-gcp-cf-20260626061148-gw-001`
- `betternat status`: active `gw-001`, route target match `true`, lease
  generation `2`, public IP `34.102.98.65`

Cleanup:

```text
Destroy complete! Resources: 7 destroyed.
GCP residual scan failed: found 2 Firestore handover records.
Deleted both Firestore records.
GCP residual scan passed.
```

### Repeatable Smoke Harness Validation

Run: `bnat-gcp-cf2-20260626085310`

The repeat run used the same connectivity-first behavior through the disposable
Terraform fixture and the protocol failover smoke script.

Artifact hashes:

- `betternat-agent`: `27b2b41b3537b5482a3ae0a0283ab3e253732d56d31f8da6175b731ed80b4f61`
- `betternat`: `e4176acd18e19e8ba41fcf125851599fe8dfc2cb3855d7afa2a2bdb4fbbd2e74`

The first scripted attempt exposed a harness race rather than a datapath
failure. During baseline, normal ownership convergence moved the route, lease,
and static IP from the initially selected gateway to the other gateway. The
script then tried to start proactive handover from the stale active gateway and
the agent correctly rejected the target as not visible from that old owner's
fresh registry view.

The smoke script now refreshes the route owner immediately before triggering
proactive handover and recomputes the standby target from the current gateway
instances.

Rerun result:

| Metric | Value |
| --- | ---: |
| Samples | `220` |
| OK | `214` |
| Failed | `6` |
| Longest consecutive failures | `6` |
| First IP | `34.94.161.106` |
| Middle IP | `34.20.217.40` |
| Last IP | `34.94.161.106` |
| IP switches | `2` |

Probe timeline:

```text
index 0   2026-06-26T09:02:38.602Z ok   34.94.161.106
index 28  2026-06-26T09:02:58.490Z ok   34.20.217.40
index 93  2026-06-26T09:03:44.645Z fail curl timeout
index 98  2026-06-26T09:03:52.213Z fail curl timeout
index 99  2026-06-26T09:03:53.727Z ok   34.94.161.106
```

Interpretation:

- Connectivity-first handover again preserved a useful temporary egress path
  through the target gateway's ephemeral public IP before stable public identity
  returned.
- The stable-IP repair window still caused a short outage: `6` failed samples,
  with about `9.1s` wall-clock from first failed sample to next successful
  sample because failed curl calls waited for their timeout.
- `doctor --live` still reported `public_identity` as critical in this run
  because the live artifact did not pass `region` into the GCP cloud provider
  factory. The branch fix is to pass the runtime region into the live provider;
  a later live artifact should no longer report this false diagnostic.

Cleanup:

```text
Destroyed Terraform resources.
Deleted the handover-created dynamic route that still referenced the VPC.
Deleted the artifact bucket.
Deleted 4 run-scoped Firestore records.
GCP residual scan passed.
```

## Decision

For GCP GA, keep the default design focused on connectivity-first handover over
single-NIC gateways.

Do not implement multi-NIC as the next latency optimization. Multi-NIC can be
designed later for management-plane isolation, but it should not block the
current GCP GA path unless a separate spike proves a different forwarding
primitive that avoids static external IPv4 access-config movement.

## Follow-Up Work

- Keep reducing the stable-IP repair outage after route handover. The next
  useful optimization is to make stable-IP convergence less disruptive after
  route ownership has moved.
- GCP live doctor behavior was updated in the same branch so shared public
  identity is checked rather than reported as route-only unsupported.
- The GCP protocol smoke harness now switches SSH proxy gateway after handover
  so client probe results can be collected without manual recovery. Keep
  promoting this into broader repeatable smoke automation.
- Keep multi-NIC as a deferred management-plane design, not a current GA
  latency requirement.
