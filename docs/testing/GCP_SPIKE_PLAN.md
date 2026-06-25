# GCP Spike Plan

Date: 2026-06-25

## Purpose

Validate whether GCP can support the BetterNAT product model before promoting a
working `betternat_gcp_gateway` resource or Google module beyond alpha.

BetterNAT's core product value over a raw LoxiLB appliance is HA ownership:
lease fencing, route/public-identity mutation safety, proactive handover,
passive failover, observability, and rollback. A single forwarding GCE VM or a
manual route replacement test is only substrate evidence.

This is a disposable-environment spike. It must not replace existing Cloud NAT
or mutate production GKE routes.

Use [GCP Disposable Integration Runbook](GCP_DISPOSABLE_INTEGRATION_RUNBOOK.md)
for the executable apply, HA, failure-injection, and cleanup pass once preflight
permissions are available.

## Target Project

Use `shared-resources-alt` for functional tests unless a different disposable
project is explicitly selected.

Always pass the project explicitly:

```sh
gcloud --project shared-resources-alt ...
```

Run the read-only preflight before creating resources:

```sh
scripts/gcp-ha-preflight.sh --project shared-resources-alt --database "(default)"
```

When validating provider-owned runtime IAM, include:

```sh
BETTERNAT_GCP_MANAGE_RUNTIME_IAM=1 \
  scripts/gcp-ha-preflight.sh --project shared-resources-alt --database "(default)"
```

When validating provider-owned Firestore database lifecycle, include:

```sh
BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE=1 \
  scripts/gcp-ha-preflight.sh --project shared-resources-alt --database "(default)"
```

The preflight checks enabled APIs, Firestore database presence, and the current
gcloud identity's permissions for Compute route/instance operations, Firestore
coordination records, service-account use, project IAM bindings, and optional
custom-role and Firestore database lifecycle. It does not create or mutate
resources.

## Topology

- One disposable VPC.
- One private subnet and one public/gateway subnet in `us-west1`.
- One private client VM without public IP.
- One or two gateway VMs with `canIpForward=true`.
- A static default route from the private subnet tag to the active gateway VM.
- LoxiLB datapath only. If LoxiLB forwarding does not work, that is a GCP
  blocker, not a reason to pass with nftables. This follows the global
  BetterNAT rule: no nftables product fallback on any cloud.
- Firestore transaction or GCS generation-precondition lease backend candidate.
- Optional reserved static external IP handover test.

## Validation Steps

### Substrate

1. Create the disposable VPC, subnets, firewall rules, and service account.
2. Launch a gateway VM with IP forwarding enabled.
3. Install BetterNAT runtime artifacts and LoxiLB on the gateway VM.
4. Launch a private client VM without public IP.
5. Add a route for the private client tag to the gateway VM.
6. Verify private client egress with:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsS https://ifconfig.me
```

7. Verify datapath counters on the gateway:

```sh
betternat status
betternat doctor --live
loxicmd get nat
```

8. Add a standby gateway VM.
9. Replace the private default route to the standby gateway and measure
   new-flow recovery time.
10. Test reserved external IP handover only after basic route failover works.

### BetterNAT HA

1. Create or select a Firestore Native database and grant the gateway runtime
   service account only the documented runtime permissions.
2. Start `betternat-agent` on both gateway VMs with `cloud=gcp`,
   Firestore coordination, route-only public identity, and `local.node_id=auto`.
3. Verify one active owner and at least one standby in agent status and
   Firestore registry records.
4. Prove lease contention by starting or restarting both agents at the same
   time; only the lease winner may mutate the route.
5. Trigger passive failover by stopping or powering off the active VM without a
   clean handover. The standby must acquire the next lease generation, replace
   the route, verify the route target, and report active.
6. Trigger proactive handover through the BetterNAT control path. The handover
   record must reach a terminal success state, the route target must move to
   the selected standby, and the old active must not keep mutating the route.
7. Restart LoxiLB on the active node and verify the agent reconciles datapath
   state without changing cloud ownership.
8. Destroy the Terraform resource after an agent-owned handover and verify
   route cleanup plus residual scans.

### Failure Injection

Inject at least one failure in each dangerous phase before promotion:

- Compute route mutation fails before route target changes.
- Route target changes but Firestore lease transfer fails.
- Firestore is unavailable to the active while Compute remains reachable.
- Compute operation polling fails after route insert/delete request.
- Standby registry record is stale or reports unhealthy datapath.

## Required Evidence

- Route mutation timing.
- Handover/new-flow recovery timing.
- Public IP behavior before and after route replacement.
- LoxiLB datapath and conntrack counters. nftables is not a GCP acceptance path,
  and it is not an acceptance path for AWS or future clouds either.
- Lease backend transaction behavior.
- Firestore lease generation before and after passive failover.
- Firestore handover records for proactive handover.
- Agent metrics for active/standby state, route target match, datapath
  readiness, and stale status.
- Compute route object and operation IDs for every route mutation.
- Cleanup command output.

## Cleanup

Destroy all disposable resources:

- gateway VMs,
- private client VM,
- instance templates or MIGs if used,
- static routes,
- firewall rules,
- service accounts and IAM bindings created for the spike,
- reserved external IPs,
- Firestore/GCS coordination resources,
- subnets and VPC.

Finish with residual scans:

```sh
gcloud --project shared-resources-alt compute instances list --filter='name~betternat'
gcloud --project shared-resources-alt compute routes list --filter='name~betternat'
gcloud --project shared-resources-alt compute addresses list --filter='name~betternat'
```

## Acceptance Criteria

Accept GCP alpha implementation only if the spike proves:

- private client egress works through a forwarding gateway VM,
- agent-owned route replacement moves new flows to a standby gateway,
- Firestore supports safe conditional ownership under live contention,
- passive failover after active loss works,
- proactive handover works,
- route mutation is lease-fenced and verified,
- cleanup is deterministic,
- LoxiLB-on-GCE is validated or explicitly rejected with evidence,
- stable public identity behavior is either validated or explicitly deferred,
- destroy after agent-owned handover does not leave residual routes, instances,
  IAM bindings, or Firestore records.
