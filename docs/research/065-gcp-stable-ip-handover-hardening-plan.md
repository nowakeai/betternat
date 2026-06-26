# GCP Stable IP Handover Hardening Plan

Date: 2026-06-26

## Problem

The first private-client stable-IP protocol run proved source-IP continuity
during passive active loss, but proactive handover rejected after a transient
Compute API zone-operation polling reset while detaching the regional static
external IPv4 address from the old active gateway.

That rejection was safe in the observed run: the route and static address still
ended on the old active gateway. It is not sufficient for GA because a polling
error can be ambiguous. The underlying detach or attach operation may still
complete after the client-side wait call fails.

## Design

Keep the cloud-provider interface stable and harden the GCP implementation as a
convergent primitive:

- retry transient `GlobalOperations.Get` and `ZoneOperations.Get` polling
  failures such as connection resets, EOF, timeouts, HTTP 408/409/429, and
  HTTP 5xx;
- continue treating context cancellation, deadline expiry, permission errors,
  and operation error payloads as hard failures;
- after an ambiguous public-identity handover failure, describe the public
  identity and accept success if the static address already converged to the
  intended target;
- retry proactive public-identity handover under the existing lease fence;
- renew the HA lease while long public-identity mutations are in flight, because
  GCP static-address detach/attach can exceed the 10 second lease TTL;
- cancel the in-flight public-identity mutation if the active gateway cannot
  renew the lease fence;
- if proactive public-identity handover still does not converge and the old
  active still owns the lease, explicitly revert public identity and route
  ownership back to the old active;
- if the lease fence is lost, stop immediately and do not let the stale active
  perform a revert.

This keeps the existing AWS path unchanged. AWS still maps `AssociateEIP` to
`AssociateAddress` with reassociation. GCP continues to map the same product
contract to access-config detach/attach, but now handles ambiguous operation
polling failures.

## Non-Goals

- Do not introduce nftables fallback behavior or acceptance criteria.
- Do not change the public Terraform surface in this hardening step.
- Do not claim connection preservation for existing flows.
- Do not make GCP stable public identity GA until live proactive handover passes
  and cleanup/runbook automation is updated.

## Test Plan

Unit tests:

- GCP zone-operation wait retries a transient `connection reset by peer` and
  completes the static address move.
- HA proactive handover accepts an ambiguous `AssociateEIP` error when
  `DescribePublicIdentity` shows the target already owns the address.
- HA proactive handover reverts public identity and route ownership when the
  address does not converge and the old active still owns the lease.
- HA proactive handover renews the lease while public identity mutation is
  running.
- HA proactive handover cancels the public identity mutation when lease renewal
  fails during the mutation.

Live GCP validation:

- build local branch artifacts and apply a disposable MIG + static-IP fixture in
  `smooth-calling-490406-d9`;
- use a private-client probe with temporary SSH removed before the handover
  trigger;
- run proactive handover from the active gateway to the standby;
- collect probe summary, route target, static address user, handover record, and
  residual cleanup evidence;
- compare failover duration with the previous `45.469s` passive-loss baseline
  and AWS ASG lifecycle evidence.

## Acceptance

For this hardening step, a live GCP run passes if:

- proactive handover completes or an ambiguous operation error is accepted only
  after describe-based convergence to the target;
- route target and static address user both match the target before lease
  transfer;
- successful private-client probe samples only show the stable public IP;
- no run-scoped Compute, Firestore, service-account, or artifact-bucket
  resources remain after cleanup.

## Implementation Notes

Implemented on 2026-06-26:

- GCP operation polling now retries transient `GlobalOperations.Get` and
  `ZoneOperations.Get` failures for connection resets, EOF, timeout-like
  network errors, HTTP 408/409/429, and HTTP 5xx.
- HA handover retries public-identity mutation while preserving the existing
  lease fence.
- Ambiguous public-identity mutation errors are accepted only when
  `DescribePublicIdentity` shows the target already owns the stable address.
- Public-identity mutation is wrapped in a lease heartbeat. The heartbeat renews
  before the 10 second lease TTL expires and cancels the mutation if renewal
  fails.
- Failed public-identity mutation reverts public identity and route ownership
  only if the old active still owns the lease fence.

## Live Results

Three disposable runs in `smooth-calling-490406-d9` were used to validate the
hardening incrementally:

| Run | Finding | Private-client probe |
| --- | --- | --- |
| `bnat-gcp-hard-20260626022317` | Compute operation polling retry worked, but a transient Firestore lease read failed after public identity mutation. Handover record was `failed`. | `260` samples, `244` ok, `16` failed, stable IP only, first fail to recovery `24.222s`. |
| `bnat-gcp-hard2-20260626024312` | Lease-read retry worked, but static-address mutation exceeded the 10 second lease TTL and standby took over. Handover record was `failed` with `HA lease changed during activation`. | `260` samples, `246` ok, `14` failed, stable IP only, first fail to recovery `22.204s`. |
| `bnat-gcp-hard3-20260626030033` | Lease heartbeat during static-address mutation kept the active fenced. Both forward and reverse handover records were `completed`. | Reverse private-client probe: `300` samples, `286` ok, `14` failed, stable IP only, first fail to recovery `22.603s`. |

Representative hard3 durable records:

```json
{
  "records": [
    {
      "status": "completed",
      "source_node_id": "bnat-gcp-hard3-20260626030033-gw-001",
      "target_node_id": "bnat-gcp-hard3-20260626030033-gw-000",
      "lease_generation": 3,
      "message": "handover completed"
    },
    {
      "status": "completed",
      "source_node_id": "bnat-gcp-hard3-20260626030033-gw-000",
      "target_node_id": "bnat-gcp-hard3-20260626030033-gw-001",
      "lease_generation": 2,
      "message": "handover completed"
    }
  ]
}
```

Hard3 cleanup:

```text
Destroy complete! Resources: 8 destroyed.
instances: 0
routes: 0
firewall-rules: 0
addresses: 0
service-accounts: 0
firestore records: 0
GCP residual scan passed
```

## Remaining Work

- Add a repeatable GCP private-client stable-IP handover smoke script so this is
  not only an operator-driven validation.
- Keep failover-duration optimization open. Proactive GCP handover is now
  successful, but the observed client-visible outage is still about `22s`, much
  slower than the latest AWS ASG lifecycle evidence.
- Keep multi-zone/regional capacity validation separate from this single-zone
  MIG hardening result.
