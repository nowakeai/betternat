# GCP Agent HA Smoke Results

Date: 2026-06-25

## Summary

Disposable run `bnat-gcp-ha-20260625082852` in project
`smooth-calling-490406-d9` validated the first live BetterNAT-owned GCP HA path:

- two GCE gateway nodes booted `betternat-agent` with Firestore coordination,
- one node acquired the lease and repaired the tagged default route,
- the standby observed the unexpired owner and did not mutate the route,
- a hard stop of the active gateway caused passive failover to the standby,
- Terraform destroy cleaned the GCE substrate after agent-owned route movement,
- the final residual scan passed after deleting Firestore history records.

This was not a release run and did not publish artifacts. The run used local
provider development overrides and a temporary GCS artifact bucket.

## Validated Evidence

Runtime artifact used for the final pass:

- `betternat-agent` linux/amd64 sha256:
  `122719534005faa34dd6e6492d9d5d0b53119b4093e989ff9465bffc573a95eb`

Terraform apply completed with:

- `route_target = "bnat-gcp-ha-20260625082852-gw-a"`
- `gateway_statuses`: both gateways `RUNNING`
- runtime IAM included Firestore permissions plus `compute.networks.updatePolicy`

During live HA repair, the agent moved the route to `gw-b`:

- route:
  `bnat-gcp-ha-20260625082852-default-via-gw`
- next hop before hard-stop test:
  `bnat-gcp-ha-20260625082852-gw-b`
- active log evidence:
  `state=ACTIVE lease_owner=bnat-gcp-ha-20260625082852-gw-b`

Passive failover test:

- action: stopped `bnat-gcp-ha-20260625082852-gw-b`
- observed route after failover:
  `nextHopInstance = bnat-gcp-ha-20260625082852-gw-a`
- observed instance status:
  - `gw-a`: `RUNNING`
  - `gw-b`: `TERMINATED`
- `gw-a` serial log showed repeated clean active renewals:
  `state=ACTIVE lease_owner=bnat-gcp-ha-20260625082852-gw-a err=""`

Cleanup evidence:

- Terraform destroy completed after manual deletion of a route that had been
  re-created by the active agent during the first cleanup attempt.
- Temporary bucket `gs://bnat-gcp-ha-20260625082852-artifacts` was deleted.
- Temporary `roles/storage.admin` binding for `user:renjie@altresear.ch` was
  removed.
- Stale deleted-service-account `roles/datastore.user` binding was removed.
- Firestore handover records under the run gateway path were deleted.
- Final residual scan passed:
  - instances: 0
  - routes: 0
  - firewall rules: 0
  - addresses: 0
  - service accounts: 0
  - Firestore records: 0

`shared-resources-alt` was also rechecked for BetterNAT-named compute residue;
no matching instances, routes, firewall rules, or addresses were found.

## Fixes Proven By The Run

The live run exposed and validated these implementation fixes:

- GCP runtime custom-role create requests must not set `role.name`.
- Deleted GCP custom roles must be undeleted and patched before reuse.
- Project IAM binding for newly created runtime service accounts needs retry
  because GCP may briefly report that the service account does not exist.
- Firestore Native runtime IAM needs the Datastore metadata, namespace, schema,
  statistics, entity allocation, and project-get permissions used by the client.
- GCP route mutation needs `compute.networks.updatePolicy` in addition to route
  create/delete/get and instance use permissions.
- GCP provider reads must treat existing gateway VMs with a missing route as
  `degraded`, not as a deleted Terraform resource.
- Active ownership repair must recreate a missing route, not only repair a
  route that points to the wrong target.
- LoxiLB empty-firewall output from `loxicmd get firewall -o json` must be
  treated as an empty rule set when it reports no firewall rules.
- GCP cleanup must delete gateway instances before deleting the route; otherwise
  a still-running active agent can recreate the route during destroy.

## Follow-up Validation

Follow-up run `bnat-gcp-ho-20260625093238` closed several gaps from this
smoke. See `docs/research/056-gcp-proactive-handover-results.md`.

Validated after this run:

- private client egress from inside the client VM through the active gateway,
- proactive handover from `gw-a` to `gw-b`,
- reverse proactive handover from `gw-b` to `gw-a`,
- GCP Firestore-backed `betternat handover history`,
- GCP `betternat doctor --live` lease, route, datapath, Prometheus, and
  source-IP probe checks.

## Remaining Gaps

The combined GCP live runs do not close the GCP alpha gate by themselves. Still
missing:

- LoxiLB counter validation and restart replay on GCE,
- raw LoxiLB GCP HA baseline comparison,
- TCP, UDP, DNS, and long-download behavior across route-only failover,
- stable public identity support, which remains unsupported for GCP route-only
  alpha.

GCP must remain unreleased until these gaps are either validated or explicitly
scoped out of the GCP alpha contract.
