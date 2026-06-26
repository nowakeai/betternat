# GCP LoxiLB Restart Results

Date: 2026-06-25

## Summary

This run validated the GCP route-only HA path with the real LoxiLB datapath on
GCE. It closed the GCP gate for LoxiLB firewall counters, rule replay after
LoxiLB restart, support-bundle collection, and post-test cleanup.

The run did not validate raw LoxiLB HA, stable GCP public identity handover, or
split-brain failure injection. Those remain separate GCP gates.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Run ID: `bnat-gcp-dp-20260625100635`
- Scratch directory: `tmp/bnat-gcp-dp-20260625100635`
- Artifact bucket: `gs://bnat-gcp-dp-20260625100635-artifacts`
- Initial agent SHA256:
  `694046d0dba002910a62e59879c2c8d0d05ac279b5bf593a2935f7cec315f892`
- CLI SHA256:
  `c76deeb718aaf192ab8aa3236a5e0a40b6b112c48d669977ce5c1a09adaa724b`
- Final live agent SHA256 after retry hardening:
  `22f109fae37455c893bb92abf0d5f7844180bf686169fbfa19636fe8faeaf246`

Bootstrap used a disposable public-read GCS artifact bucket because the current
fixture downloads runtime artifacts with `curl`. That is acceptable only for
throwaway validation; production packaging still needs the normal release or
prebaked image path.

## Preflight And Apply

`scripts/gcp-ha-preflight.sh` passed against the `(default)` Firestore Native
database in `smooth-calling-490406-d9`.

Terraform apply created a two-gateway HA group and one private client. Initial
route ownership was:

- active gateway: `bnat-gcp-dp-20260625100635-gw-a`
- standby gateway: `bnat-gcp-dp-20260625100635-gw-b`
- route target: `gw-a`
- active public IP: `34.94.153.80`
- standby public IP: `34.20.164.68`

`betternat status`, `betternat status --direct`, and `betternat doctor --live`
on the active node reported active ownership, Firestore lease/registry
agreement, route-target agreement, Prometheus reachability, source-IP probe
success, and LoxiLB datapath readiness.

## Datapath Counters

The active gateway had one LoxiLB SNAT firewall rule:

```text
sourceIP=10.95.0.0/24
toIP=10.95.0.3
doSnat=true
```

Counter evidence:

| Step | LoxiLB counter | Prometheus packets |
| --- | ---: | ---: |
| Before client traffic | `444:370926` | `446` |
| After 20 client curls | `922:541783` | `923` |

The private client source-IP probe returned `34.94.153.80`, matching the active
gateway public IP in route-only GCP mode.

## LoxiLB Restart

Restarting the active LoxiLB container caused a short datapath warmup period.
During this window the agent reported `DEGRADED` while LoxiLB CLI/API output was
not yet parseable or rule creation returned a transient process error. The HA
supervisor intentionally bounds datapath reconcile time under the lease renewal
interval, so this transient degraded state is the safe behavior.

After warmup:

- the active node recovered to `ACTIVE`,
- route ownership stayed on `gw-a`,
- the SNAT rule was replayed with `toIP=10.95.0.3`,
- client egress resumed from `34.94.153.80`,
- LoxiLB and Prometheus counters increased again.

Post-restart traffic evidence:

| Step | LoxiLB counter | Prometheus packets |
| --- | ---: | ---: |
| First restart plus traffic | `556:257609` | `557` |
| Retry-hardened agent restart plus traffic | `435:85585` | `445` |

Code hardening added in the same change:

- parse `Error: no FW rules found` as an empty LoxiLB firewall table,
- retry transient LoxiLB restart errors within the caller's context budget,
- preserve the HA supervisor's bounded reconcile timeout so an active node does
  not block lease renewal indefinitely.

## Support Bundle

The active gateway produced a support bundle at:

```text
tmp/bnat-gcp-dp-20260625100635/betternat-gcp-support.tar.gz
```

The bundle included:

- `cloud/gcp/firestore-databases.json`
- `cloud/gcp/metadata-instance-name.txt`
- `cloud/gcp/metadata-project-id.txt`
- `cloud/gcp/metadata-service-accounts.txt`
- `cloud/gcp/metadata-zone.txt`
- `cloud/gcp/route-01.json`
- `config.redacted.json`
- `datapath/loxilb-firewall.txt`
- `datapath/loxilb-version.txt`
- `handover-current.json`
- `metadata.json`
- `metrics.prom`
- `network/ip-addr.txt`
- `network/ip-route.txt`
- `network/nft-ruleset.txt`
- `status.json`
- `systemd/betternat-agent.journal.txt`
- `systemd/betternat-agent.status.txt`

`network/nft-ruleset.txt` is legacy diagnostic evidence only. It is not a
fallback datapath proof or release acceptance item.

## Cleanup

Terraform destroy removed the gateway, client VM, GCP firewall rules, subnet,
and VPC. The artifact bucket was deleted. The first residual scan found six
run-scoped Firestore handover history documents written during systemd stops;
they were explicitly deleted through the Firestore API.

Final residual scan:

```text
instances: 0
routes: 0
firewall-rules: 0
addresses: 0
service-accounts: 0
firestore records: 0
GCP residual scan passed
```

## Decision Impact

GCP LoxiLB datapath counters and restart replay are now validated for the
route-only HA topology. This does not make GCP production-equivalent. Remaining
gates include raw LoxiLB HA comparison, split-brain/failure injection,
TCP/UDP/DNS/long-download failover behavior, capacity repair, and the stable
public identity decision.
