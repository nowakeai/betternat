# GCP MIG And Stable IP Results

Date: 2026-06-25

## Summary

Disposable GCP validation passed for the first combined GCP GA-readiness slice:

- `capacity_repair_mode = "mig"` created a zonal instance template and managed
  instance group with two gateway instances.
- Non-owner gateway deletion was repaired by the MIG while route and static
  public identity stayed on the active gateway.
- Active gateway deletion caused the standby to take over both the tagged route
  and the existing regional static external IPv4 address.
- Final Terraform destroy and residual scan passed after run-scoped Firestore
  handover history cleanup.

This validates the implementation direction, but it does not close every GCP GA
gate. SSH/IAP access was unavailable in this project, so private-client protocol
checks were not rerun in this pass. Earlier route-only protocol evidence remains
in `docs/research/059-gcp-protocol-failover-results.md`.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Run ID: `bnat-gcp-migpi-20260625141300`
- Firestore database: `(default)`
- Provider/runtime artifacts: local branch builds uploaded to the run-scoped
  `gs://bnat-gcp-migpi-20260625141300-artifacts` bucket, then deleted.
- Gateway subnet: `private_ip_google_access = true`

## Important Findings

Stable public identity on GCP needs a private path from the gateway to Google
APIs. Without Private Google Access or an equivalent path, deleting the target
gateway's temporary external access config can remove its only API path before
the static address attach operation finishes.

The runtime service account also needs:

- `compute.addresses.use`
- `compute.subnetworks.useExternalIp`

The first live attempt exposed both requirements. After enabling Private Google
Access and using a fresh apply with the expanded runtime custom role, static IP
handover converged.

Destroy also exposed a cleanup-ordering bug: deleting the route while live
agents still exist can race with route repair. The GCP cleanup path should
delete gateway capacity first, then delete the route, then delete the instance
template.

## Evidence

Fresh apply produced:

```text
route_target = "bnat-gcp-migpi-20260625141300-gw-000"
stable_public_identity_address = "34.20.164.68"
runtime_iam_permissions included:
  compute.addresses.use
  compute.subnetworks.useExternalIp
```

After initial settle, static public identity and route converged to `gw-001`:

```text
check 5 14:36:13
route=bnat-gcp-migpi-20260625141300-gw-001
address=34.20.164.68 .../instances/bnat-gcp-migpi-20260625141300-gw-001
```

Non-owner deletion repaired capacity without changing ownership:

```text
check 1 14:41:47
mig=gw-000 STAGING CREATING; gw-001 RUNNING NONE
route=bnat-gcp-migpi-20260625141300-gw-001
address=34.20.164.68 gw-001

check 2 14:42:06
mig=gw-000 RUNNING NONE; gw-001 RUNNING NONE
route=bnat-gcp-migpi-20260625141300-gw-001
address=34.20.164.68 gw-001
```

Active deletion repaired capacity and moved route plus static public identity:

```text
check 1 14:45:25
mig=gw-000 RUNNING NONE; gw-001 STAGING CREATING
route=bnat-gcp-migpi-20260625141300-gw-001
address=34.20.164.68 gw-000

check 2 14:45:42
mig=gw-000 RUNNING NONE; gw-001 RUNNING NONE
route=bnat-gcp-migpi-20260625141300-gw-000
address=34.20.164.68 gw-000
```

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

## Remaining Gaps

- Rerun private-client TCP/HTTPS/UDP/download protocol checks with stable public
  identity once a reliable operator access path is available.
- Add an automated disposable script for combined MIG plus stable-IP validation.
- Decide whether provider-owned GCP subnet/module code should enable Private
  Google Access by default for stable public identity mode.
- Validate regional or multi-zone MIG behavior if it remains in GCP GA scope.
