# GCP Stable Public Identity Decision

Date: 2026-06-25

## Decision

GCP alpha remains route-only and non-stable for public egress identity.

BetterNAT must not claim stable GCP public identity until access-config
handover is live-validated. The runtime now has local support for using an
existing regional static external IPv4 address as a shared public identity, but
the mode is not validated on live GCE yet and the provider does not create or
delete the static address.

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
and adds it to the target gateway access config. A production design still
needs to prove at least:

- exact Compute API calls and IAM permissions for detach/attach access config,
- operation ordering with the Firestore lease generation,
- interaction with route delete/insert because GCP route replacement is not
  atomic,
- behavior when detach succeeds but attach fails,
- behavior when the old active restarts after losing the external IP,
- organization-policy impact for external IP restrictions,
- source-IP continuity for new flows after handover,
- cleanup of static external addresses after Terraform destroy.

## Current Product Contract

For GCP alpha:

- public identity mode is route-only,
- each gateway VM uses its own external IPv4 when public egress is required,
- route handover can change the public source IP for new flows,
- stable allowlist semantics are unsupported,
- `betternat status` and `doctor --live` must report route-only status rather
  than pretending a shared public identity exists.
- experimental local support exists behind
  `stable_public_identity_address_name`, but it must not be marketed as
  validated until a live GCE handover run proves source-IP continuity and
  cleanup.

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
