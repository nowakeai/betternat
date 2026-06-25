# GCP Firestore Live Contention Results

Date: 2026-06-25

## Summary

Status: `pass`

This run moved the GCP coordination validation from unit-only evidence to a
live Firestore Native database. The test covered the Firestore equivalent of
the AWS DynamoDB coordination table for lease contention, fenced transfer,
registry records, handover records, and cleanup.

This is not a full GCP HA pass. It proves the live coordination backend only.
Two-agent GCE route mutation, passive failover, proactive handover,
LoxiLB-on-GCE, and cleanup after agent-owned handover remain required.

## Environment

- Project: `smooth-calling-490406-d9`
- Account: `renjie@altresear.ch`
- Firestore database: `(default)`
- Firestore location: `us-west2`
- Firestore type: `FIRESTORE_NATIVE`
- Delete protection: `DELETE_PROTECTION_DISABLED`
- Branch: `terraform-surface-reset`

The earlier target project `shared-resources-alt` had the required APIs enabled
but no Firestore database. The current account could edit IAM on that project,
but the GCP validation target was switched to `smooth-calling-490406-d9` before
creating the database. Temporary direct IAM bindings added to
`shared-resources-alt` during diagnosis were removed.

## Setup

Enabled Firestore API:

```sh
gcloud --project smooth-calling-490406-d9 services enable firestore.googleapis.com --quiet
```

Added direct project-level permissions for the current validation user:

```sh
gcloud projects add-iam-policy-binding smooth-calling-490406-d9 \
  --member='user:renjie@altresear.ch' \
  --role='roles/datastore.owner' \
  --condition=None \
  --quiet

gcloud projects add-iam-policy-binding smooth-calling-490406-d9 \
  --member='user:renjie@altresear.ch' \
  --role='roles/iam.roleAdmin' \
  --condition=None \
  --quiet
```

Created the Firestore Native database:

```sh
gcloud --project smooth-calling-490406-d9 firestore databases create \
  --database='(default)' \
  --location=us-west2 \
  --type=firestore-native
```

The database create response reported:

```text
name: projects/smooth-calling-490406-d9/databases/(default)
locationId: us-west2
type: FIRESTORE_NATIVE
deleteProtectionState: DELETE_PROTECTION_DISABLED
```

## Preflight

Command:

```sh
BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE=1 \
BETTERNAT_GCP_MANAGE_RUNTIME_IAM=1 \
  scripts/gcp-ha-preflight.sh \
    --project smooth-calling-490406-d9 \
    --database '(default)'
```

Result:

```text
BetterNAT GCP HA preflight
project: smooth-calling-490406-d9
account: renjie@altresear.ch
api enabled: compute.googleapis.com
api enabled: firestore.googleapis.com
api enabled: iam.googleapis.com
firestore databases:
  projects/smooth-calling-490406-d9/databases/(default)
firestore database selected: (default)
GCP HA preflight passed
```

## Code Finding

The first live test found that the Firestore Go client does not accept `uint64`
document fields:

```text
active acquire: firestore acquire coordination lease: firestore: cannot convert type uint64 to value
```

Fix:

- Keep public BetterNAT lease and coordination types as `uint64`.
- Store Firestore document generation fields as `int64`.
- Convert at the Firestore DTO boundary with overflow protection.

Touched files:

- `internal/coordination/firestore/decision.go`
- `internal/coordination/firestore/records.go`

## Live Test

Command:

```sh
BETTERNAT_GCP_FIRESTORE_PROJECT=smooth-calling-490406-d9 \
BETTERNAT_GCP_FIRESTORE_DATABASE='(default)' \
GOCACHE=$PWD/tmp/go-build \
  go test ./internal/coordination/firestore \
    -run TestIntegrationFirestoreLeaseContention -count=1 -v
```

Result:

```text
=== RUN   TestIntegrationFirestoreLeaseContention
--- PASS: TestIntegrationFirestoreLeaseContention (11.52s)
PASS
ok  	github.com/nowakeai/betternat/internal/coordination/firestore	11.533s
```

Covered by this live test:

- first owner acquires the Firestore lease,
- second contender cannot acquire the unexpired lease,
- transfer fences on owner and generation and increments generation,
- stale renew is rejected,
- current lease read returns the transferred owner,
- agent registry publish/list works,
- handover create/read/update/list works,
- test records are cleaned up.

## Local Validation

```sh
GOCACHE=$PWD/tmp/go-build go test ./internal/coordination/firestore
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

All three commands passed.

## Remaining GCP HA Gates

This result closes the live Firestore contention gate. The following gates remain
open before GCP can be considered GA or product-parity HA:

- disposable GCP apply with agent HA enabled,
- two-agent GCE route mutation guarded by the Firestore lease,
- standby refusal to mutate route under another unexpired owner in live GCE,
- passive failover after active crash,
- proactive handover during graceful shutdown or upgrade,
- LoxiLB-on-GCE datapath counters and restart reconciliation,
- raw LoxiLB GCP HA baseline comparison,
- route delete/insert failure injection and restore evidence,
- cleanup and residual scan after agent-owned handover.
