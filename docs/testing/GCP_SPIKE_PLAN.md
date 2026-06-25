# GCP Spike Plan

Date: 2026-06-25

## Purpose

Validate whether GCP can support the BetterNAT product model before adding a
working `betternat_gcp_gateway` resource or Google module.

This is a disposable-environment spike. It must not replace existing Cloud NAT
or mutate production GKE routes.

## Target Project

Use `shared-resources-alt` for functional tests unless a different disposable
project is explicitly selected.

Always pass the project explicitly:

```sh
gcloud --project shared-resources-alt ...
```

## Topology

- One disposable VPC.
- One private subnet and one public/gateway subnet in `us-west1`.
- One private client VM without public IP.
- One or two gateway VMs with `canIpForward=true`.
- A static default route from the private subnet tag to the active gateway VM.
- LoxiLB first; nftables fallback if LoxiLB forwarding does not work.
- Firestore transaction or GCS generation-precondition lease backend candidate.
- Optional reserved static external IP handover test.

## Validation Steps

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

## Required Evidence

- Route mutation timing.
- Handover/new-flow recovery timing.
- Public IP behavior before and after route replacement.
- LoxiLB or nftables datapath counters.
- Lease backend transaction behavior.
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
- route replacement moves new flows to a standby gateway,
- the selected lease backend supports safe conditional ownership,
- cleanup is deterministic,
- stable public identity behavior is either validated or explicitly deferred.
