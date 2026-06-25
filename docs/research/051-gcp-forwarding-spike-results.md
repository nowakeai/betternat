# GCP Forwarding Spike Results

Date: 2026-06-25

## Summary

Disposable GCP forwarding is viable for a first BetterNAT GCP alpha, with
important limits.

The spike proved:

- a private GCE client without a public IP can reach the internet through a
  `canIpForward=true` gateway VM,
- nftables masquerade worked as historical forwarding substrate evidence, but
  it is not a GCP fallback datapath or acceptance path,
- a tagged `0.0.0.0/0` route can be moved from one gateway VM to another for
  new-flow recovery,
- cleanup is deterministic in a disposable VPC.

The spike did not prove:

- LoxiLB datapath behavior on GCE,
- BetterNAT agent lease or fencing on GCP,
- Firestore or GCS generation-precondition coordination,
- stable public IP handover,
- production GKE or existing-Cloud-NAT migration safety.

## Environment

- Project: `shared-resources-alt`
- Region: `us-west1`
- Zone: `us-west1-a`
- Run ID: `bnat-gcp-spike-20260625044021`
- Network: `bnat-gcp-spike-20260625044021-net`
- Subnet: `10.91.0.0/24`
- Client tag: `bnat-gcp-spike-20260625044021-client`
- Route: `bnat-gcp-spike-20260625044021-default-via-gw`

All GCP commands used `--project shared-resources-alt` explicitly.

## Topology

- One disposable VPC and one subnet.
- Gateway `gw-a`:
  - internal IP `10.91.0.2`
  - external IP `34.168.92.39`
  - `canIpForward=true`
  - Debian 12 startup script enabling `net.ipv4.ip_forward=1` and nftables
    masquerade for `10.91.0.0/16`.
- Client:
  - internal IP `10.91.0.3`
  - no public IP
  - route target selected by network tag.
- Standby gateway `gw-b`:
  - internal IP `10.91.0.4`
  - external IP `8.231.221.166`
  - same forwarding and nftables startup script.

## Validation Evidence

Initial route:

```text
bnat-gcp-spike-20260625044021-default-via-gw
destRange: 0.0.0.0/0
nextHopInstance: us-west1-a/instances/bnat-gcp-spike-20260625044021-gw-a
priority: 800
tag: bnat-gcp-spike-20260625044021-client
```

Client serial console after explicit verification reboot:

```text
BETTERNAT_GCP_VERIFY_START 2026-06-25T04:44:36+00:00
default via 10.91.0.1 dev ens4 proto dhcp src 10.91.0.3 metric 100
BETTERNAT_GCP_EGRESS_IP=34.168.92.39
BETTERNAT_GCP_VERIFY_OK 2026-06-25T04:44:37+00:00
```

Route replacement:

```text
bnat-gcp-spike-20260625044021-default-via-gw
nextHopInstance: us-west1-a/instances/bnat-gcp-spike-20260625044021-gw-b
priority: 800
tag: bnat-gcp-spike-20260625044021-client
```

Client serial console after route replacement:

```text
BETTERNAT_GCP_VERIFY_START 2026-06-25T04:47:18+00:00
default via 10.91.0.1 dev ens4 proto dhcp src 10.91.0.3 metric 100
BETTERNAT_GCP_EGRESS_IP=8.231.221.166
BETTERNAT_GCP_VERIFY_OK 2026-06-25T04:47:19+00:00
```

The egress IP changed from `gw-a` to `gw-b`, proving the tagged route moved new
client flows to the new gateway.

## Operational Notes

The current account could create and delete Compute resources, but IAP TCP
tunneling returned:

```text
Error while connecting [4033: 'not authorized']
```

The spike therefore used startup scripts and serial console output for
verification instead of SSH.

Guest agent Cloud Logging writes also returned `logging.logEntries.create`
permission errors. That did not block Compute resource creation, forwarding, or
serial-console evidence.

## Cleanup Evidence

Cleanup deleted:

- client VM,
- `gw-a`,
- `gw-b`,
- tagged route,
- internal and IAP SSH firewall rules,
- subnet,
- VPC network.

Cleanup completed at `2026-06-25T04:49:29+00:00`.

Residual scans for run ID `bnat-gcp-spike-20260625044021` returned no
instances, routes, firewall rules, subnets, networks, or addresses.

## Terraform Provider Smoke

After adding the GCP alpha provider resource, a second unpublished local-provider
smoke validated the Terraform path.

- Run ID: `bnat-gcp-tf-20260625045906`
- Provider install mode: Terraform CLI `dev_overrides` pointing at a local
  `terraform-provider-betternat` binary built from this branch.
- Precreated with `gcloud`: disposable VPC, subnet, internal firewall rule, and
  private client VM without a public IP.
- Managed by `betternat_gcp_gateway`: two gateway VMs and the tagged default
  route.

`terraform apply` created one `betternat_gcp_gateway` resource. Outputs:

```text
egress_public_ips = {
  "bnat-gcp-tf-20260625045906-gw-a" = "34.168.92.39"
  "bnat-gcp-tf-20260625045906-gw-b" = "8.231.221.166"
}
route_target = "bnat-gcp-tf-20260625045906-gw-a"
status_route_target = "bnat-gcp-tf-20260625045906-gw-a"
```

The `betternat_gcp_gateway_status` data source read the same route target after
apply.

Private client serial console evidence:

```text
BETTERNAT_GCP_TF_EGRESS_IP=34.168.92.39
BETTERNAT_GCP_TF_VERIFY_OK 2026-06-25T05:00:55+00:00
BETTERNAT_GCP_TF_EGRESS_IP=34.168.92.39
BETTERNAT_GCP_TF_VERIFY_OK 2026-06-25T05:01:36+00:00
```

`terraform destroy` destroyed the `betternat_gcp_gateway` resource and removed
the provider-owned route and gateway VMs. The precreated client, firewall,
subnet, and VPC were then deleted with `gcloud`. Residual scans for instances,
routes, firewall rules, subnets, networks, and addresses were empty.

Terraform CLI note: with provider `dev_overrides`, `terraform init` still tried
to query the Registry for unpublished version `0.2.0` and failed. Running
`terraform apply` directly with `TF_CLI_CONFIG_FILE` and the dev override
worked as intended.

## Terraform Provider Handover Smoke

A third unpublished local-provider smoke validated provider-created resources
under an out-of-band route handover, which is closer to the future agent
control-plane model.

- Run ID: `bnat-gcp-ho-20260625051732`
- Provider install mode: Terraform CLI `dev_overrides` pointing at a local
  `terraform-provider-betternat` binary built from this branch.
- Precreated with `gcloud`: disposable VPC, subnet, internal firewall rule, and
  private client VM without a public IP.
- Managed by `betternat_gcp_gateway`: two gateway VMs and the tagged default
  route.

Initial `terraform apply` outputs:

```text
egress_public_ips = {
  "bnat-gcp-ho-20260625051732-gw-a" = "34.168.92.39"
  "bnat-gcp-ho-20260625051732-gw-b" = "8.231.221.166"
}
route_target = "bnat-gcp-ho-20260625051732-gw-a"
status_route_target = "bnat-gcp-ho-20260625051732-gw-a"
```

Initial client egress:

```text
BETTERNAT_GCP_HO_EGRESS_IP=34.168.92.39
BETTERNAT_GCP_HO_VERIFY_OK 2026-06-25T05:20:07+00:00
```

The route was then deleted and recreated with next hop
`bnat-gcp-ho-20260625051732-gw-b`.

Post-cutover client egress:

```text
BETTERNAT_GCP_HO_EGRESS_IP=8.231.221.166
BETTERNAT_GCP_HO_VERIFY_OK 2026-06-25T05:21:33+00:00
```

`terraform apply -refresh-only` detected the out-of-band change:

```text
route_target = "bnat-gcp-ho-20260625051732-gw-b"
status_route_target = "bnat-gcp-ho-20260625051732-gw-b"
```

`terraform destroy` then removed the provider-owned gateway VMs and route even
after the route target had moved to `gw-b`. The precreated client, firewall,
subnet, and VPC were then deleted with `gcloud`. Residual scans for instances,
routes, firewall rules, subnets, networks, and addresses were empty.

## Decision

Proceed with a narrow GCP alpha provider resource for the verified topology:

- provider-owned GCE gateway VMs,
- `canIpForward=true`,
- historical Linux forwarding startup script for substrate proof only,
- tagged default route to the active gateway,
- read-only status from Compute route and instance state,
- explicit alpha documentation.

Keep these out of scope until separately validated:

- LoxiLB on GCE,
- BetterNAT agent HA on GCP,
- lease backend selection,
- stable public IP handover,
- production GKE migration.
