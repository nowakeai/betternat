# GCP Proactive Handover Results

Date: 2026-06-25

## Summary

Disposable run `bnat-gcp-ho-20260625093238` in project
`smooth-calling-490406-d9` validated live GCP proactive handover for the
route-only BetterNAT HA path.

This was not a release run. It used local provider development overrides and
temporary GCS-hosted artifacts. No release artifacts were published.

## Environment

- Project: `smooth-calling-490406-d9`
- Region: `us-west2`
- Zone: `us-west2-a`
- Firestore database: `(default)`
- Route: `bnat-gcp-ho-20260625093238-default-via-gw`
- Client tag: `bnat-gcp-ho-20260625093238-client`
- Gateways:
  - `bnat-gcp-ho-20260625093238-gw-a`
  - `bnat-gcp-ho-20260625093238-gw-b`

Runtime artifacts used for the fixed live validation:

- fixed `betternat-agent` linux/amd64 sha256:
  `ee2f4763ef6e6d2a1c3277970f142fb1a5740dad03bf2dd81d28ee0bf96b8e0f`
- fixed `betternat` linux/amd64 sha256:
  `cb025b3dfd501e9527117ca95edecfe166e195fc7fcd56d9ef42b5ef9f3555d8`

## Finding

The first live proactive handover exposed a correctness bug in the HA
controller. The active node held the ownership lock while GCP route replacement
ran, but the cached lease record could expire before the route operation
completed. The command returned `rejected` because lease verification observed a
changed lease after mutation, while the route and active state had already moved
to the target node.

That mismatch is not acceptable for release because operators would see a
failed handover even though cloud ownership had changed.

## Fix

The controller now renews and re-verifies the active lease fence during
proactive handover:

- after acquiring the local ownership lock,
- before public identity mutation,
- before each route mutation attempt,
- before transferring the lease to the target.

This keeps the active lease alive across slow cloud route operations and
preserves the generation fence used to reject stale active owners.

## Validation

After replacing the live agent binary on both gateways, manual handover from
`gw-a` to `gw-b` completed:

- request id: `1782380853884689933`
- status: `completed`
- source: `bnat-gcp-ho-20260625093238-gw-a`
- target: `bnat-gcp-ho-20260625093238-gw-b`
- lease generation: `3`
- final route next hop: `bnat-gcp-ho-20260625093238-gw-b`
- final status: `gw-b active`, `gw-a standby`

Reverse handover from `gw-b` back to `gw-a` also completed:

- request id: `1782381076144001952`
- status: `completed`
- source: `bnat-gcp-ho-20260625093238-gw-b`
- target: `bnat-gcp-ho-20260625093238-gw-a`
- lease generation: `4`
- final route next hop: `bnat-gcp-ho-20260625093238-gw-a`
- final status: `gw-a active`, `gw-b standby`

`betternat handover history --output json` was fixed to read Firestore-backed
coordination records for `cloud=gcp`. Live history returned both successful
manual handovers and the earlier failed pre-fix record.

`betternat status --direct --output json` on the active node reported:

- `route_target_match: true`
- `lease_generation: 4`
- both gateway registry records fresh and healthy
- active node: `bnat-gcp-ho-20260625093238-gw-a`

`betternat doctor --live` returned warning status only because rollback route
targets were not captured in the disposable fixture. The HA and datapath checks
were healthy:

- `datapath`: ok
- `lease`: ok, owner `gw-a`, generation `4`
- `route`: ok
- `public_identity`: ok, GCP route-only mode has no shared identity
- `prometheus`: ok
- `source_ip_probe`: ok, observed source IP `34.94.153.80`

Independent client egress through a gateway bastion returned `34.94.153.80`,
matching the active gateway public IP.

## Cleanup

Cleanup completed after validation:

- temporary operator SSH firewall deleted,
- Terraform destroy removed provider and fixture resources,
- temporary GCS artifact bucket deleted,
- Firestore handover records under the run gateway path deleted,
- final residual scan passed with:
  - instances: 0
  - routes: 0
  - firewall rules: 0
  - addresses: 0
  - service accounts: 0
  - Firestore records: 0

`shared-resources-alt` was rechecked for BetterNAT-named compute residue after
the run; no matching instances, routes, firewall rules, or addresses were
listed.

## Remaining Gaps

This run closes the proactive handover and GCP Firestore handover-history
support gap for route-only HA. It does not close every GCP alpha gate.

Still pending:

- LoxiLB counter validation and restart replay on GCE,
- raw LoxiLB GCP HA baseline comparison,
- split-brain and stale-generation failure injection,
- route delete/insert operation failure injection,
- TCP, UDP, DNS, and long-download behavior across route-only failover,
- stable public identity design, which remains unsupported for GCP route-only
  alpha.

Per the project datapath decision, nftables fallback is not an acceptance item
for any of these remaining gates. GCP must validate LoxiLB directly or keep the
gap open.
