# 2026-06-23 Agent Daemon Status API

## Summary

Implemented the first daemon-backed CLI path from `docs/research/039-agent-daemon-api-and-handover-plan.md`.

Changes:

- Added shared daemon status API response types in `internal/agentapi`.
- Added a local Unix socket control API to `betternat-agent`.
- Added `GET /v1/status` and `GET /v1/healthz`.
- Added a background status cache refresher in the agent.
- Moved normal `betternat status` to the daemon API by default.
- Kept the old direct aggregation path behind `betternat status --direct`.

## Current Boundary

This implements the fast status path only.

It does not yet implement:

- `/v1/datapath`,
- `/v1/failover`,
- `/v1/doctor`,
- `/v1/peers`,
- `/v1/drain`,
- `/v1/handover`,
- lease transfer,
- proactive route/EIP handover.

Mutating runtime control should remain inside the agent and must not be added to CLI direct mode.

## Validation

Passed:

```sh
GOCACHE=$PWD/tmp/go-build go test ./internal/agent ./internal/cli
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

AWS validation after publishing a new runtime artifact:

- version: `v0.1.0-alpha.daemon-20260623073549`,
- launch template version: `7`,
- successful instance refresh: `4ac19c25-64b3-4a4c-ac5b-a431c19770ba`,
- active: `i-0cf9c5b33ceca117d`,
- standby: `i-0e2091a2b696bdee5`,
- both instances had `/run/betternat/agent.sock`,
- default `sudo betternat status --output json` returned daemon API `schema_version:"v1"` with fresh cache metadata,
- direct fallback `sudo betternat status --direct --config /etc/betternat/agent.json --output json --sample 0s` still worked,
- route and EIP both matched the active instance,
- `doctor --live` passed key live checks with the same expected warnings.

## Proactive Handover Extension

Implemented and validated the first manual proactive handover path:

- added `lease.Transfer` with owner/generation/expiry fencing,
- implemented DynamoDB and coordination-table transfer operations,
- added `ha.Controller.Handover`,
- exposed daemon `POST /v1/handover`,
- added `betternat handover start --to auto|<instance-id>`,
- kept mutating handover out of direct CLI fallback,
- added an HA group ownership mutex so normal active repair and handover cloud mutations cannot interleave inside one agent process.

Current boundary:

- manual active-local handover is implemented,
- standby request forwarding is not implemented,
- durable handover operation records and idempotency are not implemented yet,
- automatic systemd stop, ASG lifecycle, and Spot interruption handover integration is not implemented yet,
- authenticated peer API remains deferred.

AWS validation:

- version: `v0.1.0-alpha.handover-20260623080252`,
- launch template version: `8`,
- successful instance refresh: `7117f616-2132-4d74-a513-c2f50bbd71d6`,
- pre-handover active: `i-056fcf70d3dc3061c`,
- pre-handover standby: `i-09360439fc37ef0d0`,
- command: `sudo betternat handover start --to auto --reason manual-aws-validation --output json`,
- command result: `completed`,
- command-internal elapsed time: about `2.8s`,
- post-handover active: `i-09360439fc37ef0d0`,
- lease generation: `2`,
- route and shared EIP both moved to the new active,
- both daemon status views agreed after handover,
- `doctor --live` passed key live checks on the new active with the same expected warnings.

## Status UX Follow-up

Fixed two daemon-backed `betternat status` UX issues:

- active shared-EIP public IP now propagates from the local HA snapshot into the coordination registry and daemon status response,
- human table output uses borderless aligned columns so it is easier to pipe into tools such as `awk`.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
git diff --check
```

AWS validation:

- version: `v0.1.0-alpha.statusux-20260623081543`,
- launch template version: `9`,
- successful instance refresh: `e2d822ce-979d-4603-b5cc-2e6a24aeec1c`,
- active: `i-0095956b45e4462f5`,
- standby: `i-07884bbdd4a162a9a`,
- default `sudo betternat status` had no box-drawing borders,
- default `sudo betternat status --output json` reported `52.24.117.43` in the summary and active row public IP fields,
- live SSM recheck command `6f56c079-7b8e-45d5-a6f0-ff90cb2edff0` confirmed the same output from both gateway instances,
- route and shared EIP both matched active `i-0095956b45e4462f5`,
- `doctor --live` passed key live checks with the same expected warnings.

## Automatic Handover Integration

Implemented the next handover control-plane layer:

- durable coordination records keyed as `handover#<request_id>`,
- request-id idempotency for repeated handover submissions,
- standby request forwarding to the active daemon when peer control URL and auth token are available,
- authenticated peer control API using Bearer tokens,
- standby-side prepare validation that checks the requester against the current lease owner and generation,
- automatic handover attempts before:
  - systemd stop / SIGTERM-driven supervisor shutdown,
  - ASG lifecycle termination completion,
  - Spot interruption shutdown.

Provider-managed configs now enable the peer API and publish peer `control_url` through the agent registry. The Terraform provider generates a random sensitive `peer_api_auth_token`, stores it in resource state, and reuses it on later updates so provider upgrades do not rotate peer credentials or force appliance replacement unless the rendered runtime config otherwise changes.

Validation:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

AWS validation is still pending for this follow-up build.
