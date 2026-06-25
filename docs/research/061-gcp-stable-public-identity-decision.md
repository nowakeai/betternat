# GCP Stable Public Identity Decision

Date: 2026-06-25

## Decision

GCP alpha remains route-only and non-stable for public egress identity.

BetterNAT must not claim stable GCP public identity as a GA contract until
access-config handover is protocol-validated and documented with its GCP
network prerequisites. The runtime now has live evidence for moving an existing
regional static external IPv4 address during a disposable MIG failover, but the
provider does not create or delete the static address.

## Rationale

The live GCP protocol failover smoke showed the expected route-only behavior:
new flows recovered after route handover, but the observed public source IP
changed when the route target moved from one gateway VM to another.

Google Compute Engine supports assigning static external IP addresses to VM
instances, and the static external IP documentation says only one resource can
use a static external IP address at a time. It also documents changing an
instance's external IPv4 address by removing the existing access configuration
and adding a new one. The `gcloud compute instances delete-access-config`
reference states that deleting the access config removes the external IP from
the VM interface and traffic to that external IP will no longer reach that VM.

BetterNAT now maps those primitives to its existing shared public identity
contract for GCP: `ha.public_identity.mode="shared_eip"` uses
`allocation_id` as the regional static address name, removes conflicting
access configs, detaches the address from the previous instance when needed,
and adds it to the target gateway access config. Live validation in
`docs/research/063-gcp-mig-stable-ip-results.md` proved takeover during active
gateway deletion, but also proved two required prerequisites: the gateway subnet
needs Private Google Access or an equivalent private Google API path, and the
runtime service account needs `compute.addresses.use` plus
`compute.subnetworks.useExternalIp`. A production design still needs to prove
at least:

- source-IP continuity for private-client protocol checks after handover,
- interaction with route delete/insert because GCP route replacement is not
  atomic,
- behavior when detach succeeds but attach fails,
- behavior when the old active restarts after losing the external IP,
- organization-policy impact for external IP restrictions,
- cleanup of static external addresses after Terraform destroy.

## Current Product Contract

For GCP alpha:

- public identity mode is route-only,
- each gateway VM uses its own external IPv4 when public egress is required,
- route handover can change the public source IP for new flows,
- stable allowlist semantics are unsupported,
- `betternat status` and `doctor --live` must report route-only status rather
  than pretending a shared public identity exists.
- experimental support exists behind `stable_public_identity_address_name`, but
  it must not be marketed as GA until protocol checks, documentation, and
  cleanup semantics are complete.

For GCP GA:

- either implement and live-validate shared static external IP handover, or
- declare GCP GA as route-only/non-stable and keep stable public identity out
  of the GA promise.

## References

- Google Cloud, "Configure static external IP addresses":
  <https://docs.cloud.google.com/compute/docs/ip-addresses/configure-static-external-ip-address>
- Google Cloud SDK, `gcloud compute instances delete-access-config`:
  <https://docs.cloud.google.com/sdk/gcloud/reference/compute/instances/delete-access-config>
- Live route-only protocol evidence:
  `docs/research/059-gcp-protocol-failover-results.md`
- Live MIG plus static IP handover evidence:
  `docs/research/063-gcp-mig-stable-ip-results.md`
