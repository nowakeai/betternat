# devops-tf BetterNAT Adoption Plan

Date: 2026-06-27

## Question

How should BetterNAT fit into `~/code/devops-harness/repos/devops-tf` for the
operator's AWS and GCP infrastructure?

## Summary

`devops-tf` is a strong fit for module-level BetterNAT adoption. Keep the
user-facing switch in the existing networking modules instead of requiring each
environment to hand-write `betternat_aws_gateway` or `betternat_gcp_gateway`.

Recommended order:

1. Use released runtime/provider artifacts for all new validation:
   BetterNAT runtime `v0.2.1` and Terraform provider `0.2.1`.
2. Run a disposable BetterNAT stack outside production routes.
3. Add a disabled BetterNAT backend selector to `devops-tf`.
4. Pilot AWS first in one non-production private-subnet environment.
5. Pilot GCP only for a tagged, narrow private-node workload before replacing
   cluster-wide Cloud NAT.
6. Move production default egress only after source-IP allowlists, rollback,
   and live failover checks are explicit.

## Current Release Status

As of 2026-06-29, the GKE compatibility fixes from this spike are available in
published artifacts:

- BetterNAT runtime `v0.2.1` is published at
  <https://github.com/nowakeai/betternat/releases/tag/v0.2.1>.
- Terraform provider `0.2.1` is published at
  <https://github.com/nowakeai/terraform-provider-betternat/releases/tag/v0.2.1>
  and is visible through the Terraform Registry as `nowakeai/betternat`
  version `0.2.1`.
- The provider `0.2.1` Registry install path was validated with Terraform
  `v1.14.7`: `terraform init` installed `nowakeai/betternat v0.2.1` on
  `linux_amd64`, and a Registry-backed `betternat_runtime_artifacts` plan
  resolved the `v0.2.1` arm64 agent artifact URL.
- The provider built-in runtime artifact manifest supports direct
  `betternat_version = "v0.2.1"` for both `linux/amd64` and `linux/arm64`.

The disposable GKE fixture and temporary public artifact bucket used for this
validation were destroyed after the release checks. A residual scan found no
matching GCE instances, static addresses, routes, firewall rules, GKE cluster,
Cloud Router, service accounts, or GCS bucket.

## Evidence

### AWS

`devops-tf/aws/modules/networking` currently owns:

- one VPC,
- public subnets,
- optional private subnets,
- one private route table,
- one `aws_nat_gateway`,
- one `aws_eip`,
- one `0.0.0.0/0` private default route to the NAT Gateway.

The root module exposes only `enable_nat_gateway` and
`enable_private_subnets`, then exports `nat_gateway_ip`.

This means BetterNAT can be hidden behind the same networking module boundary:
callers should select an egress backend, while the module passes VPC, public
subnet, private route table, and private CIDR details to the BetterNAT module.

`jpmainnet2` currently sets both `enable_nat_gateway = false` and
`enable_private_subnets = false`, so it is not a BetterNAT target until that
environment intentionally moves workloads into private subnets.

Local AWS CLI status on 2026-06-27:

- `default`: no credentials available.
- `601427795217_AdministratorAccess`: token expired.

AWS live route/NAT state was therefore not verified in this pass.

### GCP

`devops-tf/gcp/modules/vpc` currently owns:

- VPC network,
- GKE node subnet and secondary ranges,
- Cloud Router,
- `google_compute_router_nat`,
- optional static regional external NAT IP resources,
- firewall rules.

`gcp-cluster-1` and `gcp-cluster-2` both configure:

```hcl
enable_nat_logging     = true
nat_ip_allocate_option = "MANUAL_ONLY"
nat_min_ports_per_vm   = 64
nat_ip_count           = 3
```

Live GCP read-only checks on 2026-06-27 confirmed:

- `gcp-cluster-1-vpc-nat` exists in `us-west1`, uses `MANUAL_ONLY`, and is
  attached to three regional static address resources.
- `gcp-cluster-2-vpc-nat` exists in `us-west1`, uses `MANUAL_ONLY`, and is
  attached to three regional static address resources.
- `altllm-dev` has standalone `cloud-claw-nat` on the default network with
  `AUTO_ONLY`.

`gcp/environments/rollup-utils/terraform.tfvars` hard-codes the three
`gcp-cluster-2` NAT IPs as `/32` allowlist sources, so replacing Cloud NAT can
break downstream firewall assumptions unless stable public identity is planned
first.

## Recommended Terraform Shape

### AWS networking module

Add:

```hcl
variable "nat_backend" {
  description = "Private subnet egress backend: aws_nat_gateway, betternat, or none."
  type        = string
  default     = "aws_nat_gateway"

  validation {
    condition     = contains(["aws_nat_gateway", "betternat", "none"], var.nat_backend)
    error_message = "nat_backend must be aws_nat_gateway, betternat, or none."
  }
}
```

Keep `enable_nat_gateway` temporarily as the old compatibility switch inside
`devops-tf`, but derive behavior through a local:

```hcl
locals {
  effective_nat_backend = var.enable_nat_gateway ? var.nat_backend : "none"
}
```

Then gate the existing NAT Gateway resources behind
`local.effective_nat_backend == "aws_nat_gateway"` and create
`module "betternat"` behind `local.effective_nat_backend == "betternat"`.

For the current AWS networking module, the BetterNAT inputs can be derived from
existing resources:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/aws"
  version = "~> 0.2"

  name   = "${var.project_name}-egress"
  vpc_id = aws_vpc.main.id

  azs                     = ["${var.aws_region}${var.availability_zones[0]}"]
  public_subnet_ids       = [aws_subnet.public[var.availability_zones[0]].id]
  private_route_table_ids = [aws_route_table.private[0].id]
  private_cidrs           = [aws_vpc.main.cidr_block]

  betternat_version   = "v0.2.1"
  stable_egress_ip    = true
  prometheus_enabled  = true
  rollback_on_destroy = true
}
```

Do not keep `aws_route.private_nat_gateway` active when BetterNAT owns the same
private default route.

### GCP VPC module

Do not start by replacing cluster-wide Cloud NAT in `gcp-cluster-1` or
`gcp-cluster-2`.

Instead add a narrower selector:

```hcl
variable "egress_backend" {
  description = "Private node egress backend: cloud_nat, betternat, or none."
  type        = string
  default     = "cloud_nat"
}
```

Use Cloud NAT as the default. Introduce BetterNAT first as an additional tagged
route path for a small private-node pool or test VM tag, for example:

```hcl
module "betternat" {
  source  = "nowakeai/betternat/google"
  version = "~> 0.2"

  name       = "${var.project_name}-egress"
  project_id = var.project_id
  region     = var.region
  zone       = "${var.region}-a"

  network    = google_compute_network.vpc.name
  subnetwork = google_compute_subnetwork.gke_nodes.name
  client_tag = "${var.network_name}-betternat-client"

  private_cidrs = [var.subnet_cidr]

  betternat_version = "v0.2.1"

  manage_runtime_service_account = true
  manage_runtime_iam             = true
}
```

Only after pilot validation should the module consider disabling
`google_compute_router_nat.nat` for the same source population.

For `gcp-cluster-1` and `gcp-cluster-2`, stable public identity must be planned
before any allowlisted workload moves. BetterNAT's GCP module expects an
existing regional static external IPv4 address name for stable public identity.
The current Cloud NAT address resources are natural migration candidates, but
moving one out of Cloud NAT requires a staged Terraform change and an explicit
rollback path.

## Pilot Targets

### AWS

Best first target:

- `stg20230731v070mp` in `us-west-2`, assuming private workload egress is active
  and AWS credentials are refreshed.

Avoid as first target:

- `stacksmainnetbl`, because it is production and has VPC Flow Logs/Athena
  dependencies.
- `jpmainnet2`, because it is currently public-only and has no NAT/private
  subnet path.

### GCP

Best first target:

- a new test tag or test node pool inside `gcp-cluster-2`, because the current
  infrastructure already has private node pools and well-known static Cloud NAT
  IPs, and downstream allowlists make source-IP behavior easy to verify.

Avoid as first target:

- full replacement of `gcp-cluster-1` or `gcp-cluster-2` Cloud NAT.
- `altllm-dev`, unless the goal is specifically to replace default-network VM
  Cloud NAT; it currently uses auto-allocated Cloud NAT identity.

## Validation Before Production

For AWS:

- refresh AWS CLI credentials,
- run a disposable BetterNAT AWS quick start,
- add disabled HCL behind `nat_backend = "aws_nat_gateway"`,
- run `terraform init` and `terraform plan` against the chosen non-production
  environment,
- switch only that environment to `nat_backend = "betternat"`,
- verify route owner, stable EIP, private client egress, `betternat doctor
  --live`, metrics, failover, and destroy rollback.

For GCP:

- use Terraform provider `nowakeai/betternat` `>= 0.2.1` and
  `betternat_version = "v0.2.1"`,
- run the disposable GCP quick start or an equivalent tagged test path,
- verify Firestore, route ownership, gateway MIG health, LoxiLB, and private
  client egress,
- introduce a test-only `client_tag` path while Cloud NAT remains available,
- verify source IP and downstream allowlists,
- only then plan stable public identity migration from a Cloud NAT static
  address.

## GKE Compatibility Spike

Date: 2026-06-29

A disposable GCP fixture in `tmp/gcp-gke-compat` validated BetterNAT with a
private-node GKE cluster in project `smooth-calling-490406-d9`, region
`us-west1`, zone `us-west1-a`. The fixture kept Cloud NAT only as bootstrap
egress and installed a tagged BetterNAT default route for the private GKE node
tag `bnat-gke-compat-20260628-gke-betternat-client`.

The baseline compatibility result was positive:

- GKE private-node Pods reached `oauth2.googleapis.com` through BetterNAT.
- Pod-observed public egress IP matched the active BetterNAT gateway public IP.
- The GCP route target moved to the active BetterNAT gateway instance.
- A restored `e2-small` environment after the matrix still returned HTTP 200
  from a private-node Pod through BetterNAT.

### Machine Type Matrix

Matrix command:

```bash
TF_CLI_CONFIG_FILE=/tmp/betternat-tfrc \
  MACHINE_TYPES='e2-small e2-highcpu-2 e2-standard-2' \
  TRIALS=2 \
  STABILIZE_SECONDS=90 \
  tmp/gcp-gke-compat/run-machine-type-matrix.sh
```

Raw local results were written under
`tmp/gcp-gke-compat/results/20260629T055622Z`. This run used local artifacts
built from the service-account lifecycle and startup-readiness fixes:

- `betternat-agent_linux_amd64`:
  `9beb828891d146738d50c3440065acbb8a1f92b3a0b1710bdb943fdbb3ec8409`
- `betternat_linux_amd64`:
  `bab7e502813bd11a46e5d0232bed2aaf4ded11c87f945cf82851acd7f0fbc8d7`

| Machine type | Trial | Result | Agent-to-clean-active | Probe result | Notes |
| --- | ---: | --- | ---: | --- | --- |
| `e2-small` | 1 | Pass | 9s | 10/10 HTTP 200 | 1s INIT window, no DEGRADED, two transient `loxicmd` killed events, one firewall JSON parse error. |
| `e2-small` | 2 | Pass | Not captured | 10/10 HTTP 200 | 0s INIT window, no DEGRADED, two transient `loxicmd` killed events. Serial pagination missed the later ACTIVE line, but Pod egress worked. |
| `e2-highcpu-2` | 1 | Pass | 19s | 10/10 HTTP 200 | 12s INIT window, no DEGRADED, one transient `loxicmd` killed event, one firewall JSON parse error, three retryable route readiness errors. |
| `e2-highcpu-2` | 2 | Pass | Not captured | 10/10 HTTP 200 | No active-gateway DEGRADED. Serial pagination missed the later ACTIVE line, but Pod egress worked. |
| `e2-standard-2` | 1 | Pass | 6s | 10/10 HTTP 200 | No INIT or DEGRADED on the active route target. |
| `e2-standard-2` | 2 | Pass | 6s | 10/10 HTTP 200 | No INIT or DEGRADED on the active route target. |

After the matrix, the live fixture was restored from `e2-standard-2` to
`e2-small` with `-replace=betternat_gcp_gateway.egress`. The replacement create
finished in 49s without the previous service-account IAM failure. The first
immediate Pod probe ran before the gateway reached steady ACTIVE and timed out;
serial logs then showed continuous clean `ACTIVE err=""` from `gw-000` and
clean `STANDBY err=""` from `gw-001`. A repeat private-node Pod probe succeeded
with `public-ip=34.83.255.93` and `HTTP/2 200`.

The current evidence still does not prove that the earlier `e2-small` startup
delay was caused by machine performance. In the fixed matrix, both `e2-small`
trials passed the GKE Pod egress probe and reported no active-gateway
`DEGRADED` state. The `e2-standard-2` runs reached clean ACTIVE in 6s, but the
dominant earlier failures were service-account lifecycle and startup
classification issues, not CPU or memory pressure.

The matrix parser's OOM detector was tightened for this rerun. All trials
reported `oom_like_serial_hits=0`; no serial evidence of a real kernel OOM kill
was found.

### Sizing Guidance

`e2-standard-2` is likely over-provisioned as the default low-cost NAT appliance
shape. It provides 2 vCPU and 8 GiB RAM, while the valid tests did not show
memory pressure.

Recommended GCP defaults after this spike:

- Keep `e2-small` as the low-cost test and small-traffic default candidate.
- Use `e2-highcpu-2` when a small non-shared-core shape is preferred; it
  provides 2 vCPU and 2 GiB RAM without jumping to 8 GiB.
- Treat `e2-standard-2` as a conservative memory-margin option, not the default
  recommendation for NAT gateway baseline sizing.

Approximate `us-west1` on-demand monthly estimates from the GCP Billing Catalog
API and Compute Engine machine type metadata:

- `e2-small`: about USD 12/month per VM, using the shared-core shape estimate.
- `e2-highcpu-2`: about USD 36/month per VM.
- `e2-custom-2-2048`: about USD 38/month per VM.
- `e2-custom-2-4096`: about USD 42/month per VM.
- `e2-standard-2`: about USD 49/month per VM.

### Issues Found And Fixes Applied

1. Provider GCP runtime service account lifecycle needs hardening.

   Gateway replacement exposed two identity race/lifecycle failures:

   - a newly booted gateway returned metadata/auth errors like
     `Service account is deleted, disabled, or not found`;
   - a later recreate failed during IAM setup with
     `Service account ... does not exist`.

   The provider should wait for runtime service account visibility before IAM
   binding and instance/template creation, retry IAM binding on this GCP
   eventual-consistency error, and avoid deleting/recreating the runtime service
   account during ordinary gateway replacement when the name is unchanged.

   Fix applied in this working tree:

   - wait for `GetServiceAccount` visibility after create;
   - retry project IAM `setPolicy` for service-account propagation errors;
   - retain the provider-managed runtime service account during gateway cleanup
     so repeated same-name replacement does not hit GCP delete/recreate
     tombstone behavior.

   Validation: the fixed 6-trial matrix and the final `e2-standard-2` to
   `e2-small` replacement completed without the previous service-account
   metadata or IAM-binding errors.

2. Agent startup readiness should distinguish convergence from true degraded
   service.

   Valid runs across `e2-small` and `e2-standard-2` still showed transient
   startup events such as `loxicmd` being killed, invalid LoxiLB firewall JSON,
   and retryable GCP route `resourceNotReady` errors. The agent should gate
   datapath reconciliation until LoxiLB is ready enough to return valid command
   output, use bounded backoff and clear startup-phase logging, and avoid
   reporting a normal boot convergence window as persistent `DEGRADED`.

   Fix applied in this working tree:

   - the HA supervisor now reports startup datapath, lease-verify, and
     ownership-repair convergence failures as `INIT` during a bounded startup
     grace period until the process has reported a healthy `ACTIVE`;
   - once the process has reported healthy `ACTIVE`, the same failures still
     report `DEGRADED`.

   Validation: the fixed matrix showed no active-gateway `DEGRADED` windows.
   Startup route readiness errors in `e2-highcpu-2` trial 1 were classified as
   `INIT` and then converged to clean `ACTIVE`.

3. The GKE compatibility harness should use stricter log parsing.

   The matrix script needs a precise kernel OOM detector, serial-log markers or
   Cloud Logging collection to avoid pagination gaps, and a separate
   `CONVERGING`/`READY` timing summary so startup noise is easier to compare
   between machine types.

   Partial fix applied in the local fixture: the OOM detector now matches
   kernel-style OOM text instead of arbitrary `oom` substrings, and the parser
   reports INIT and DEGRADED windows separately. Remaining gap: serial-port
   pagination can still miss later clean ACTIVE lines; use Cloud Logging or
   explicit serial markers for future benchmark-quality evidence.

## Open Risks

- BetterNAT is self-managed and does not replace NAT Gateway or Cloud NAT SLA.
- Current BetterNAT groups are one AWS AZ or one GCP zone per HA group.
- Active flows can reset during failover.
- GCP stable public identity is connectivity-first, so source-IP continuity is
  best effort during transition.
- Current `devops-tf` GCP allowlists depend on existing Cloud NAT IPs.
- AWS live state still needs verification after credentials are refreshed.
