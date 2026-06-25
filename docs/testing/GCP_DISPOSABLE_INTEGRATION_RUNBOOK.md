# GCP Disposable Integration Runbook

Date: 2026-06-25

## Purpose

This runbook turns the GCP spike plan into a repeatable disposable-environment
validation pass. It is required before the GCP provider path can be promoted
beyond alpha.

BetterNAT's GCP bar is not raw packet forwarding. The pass must prove
agent-owned HA: Firestore lease fencing, route mutation safety, passive
failover, proactive handover, datapath reconciliation, observability evidence,
and deterministic cleanup.

Do not run this against a production VPC, existing Cloud NAT migration, or
production GKE route table.

## Inputs

Set these values explicitly for each run:

```sh
export BETTERNAT_GCP_PROJECT=shared-resources-alt
export BETTERNAT_GCP_REGION=us-west2
export BETTERNAT_GCP_ZONE=us-west2-a
export BETTERNAT_GCP_NAME=bnat-gcp-ga-smoke
export BETTERNAT_GCP_DATABASE='(default)'
export BETTERNAT_VERSION=v0.1.0
```

Use a unique `BETTERNAT_GCP_NAME` per run if a previous pass did not finish
cleanup.

## Preflight

Confirm the current identity and project:

```sh
gcloud config get-value core/account
gcloud --project "$BETTERNAT_GCP_PROJECT" config list
```

Run the read-only base preflight:

```sh
scripts/gcp-ha-preflight.sh \
  --project "$BETTERNAT_GCP_PROJECT" \
  --database "$BETTERNAT_GCP_DATABASE"
```

If this run will let the BetterNAT provider create the Firestore Native
database, require database lifecycle permissions:

```sh
BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE=1 \
  scripts/gcp-ha-preflight.sh \
    --project "$BETTERNAT_GCP_PROJECT" \
    --database "$BETTERNAT_GCP_DATABASE"
```

If this run will let the BetterNAT provider create the runtime custom role and
project IAM binding, require runtime IAM lifecycle permissions:

```sh
BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE=1 \
BETTERNAT_GCP_MANAGE_RUNTIME_IAM=1 \
  scripts/gcp-ha-preflight.sh \
    --project "$BETTERNAT_GCP_PROJECT" \
    --database "$BETTERNAT_GCP_DATABASE"
```

Record the preflight output in the run evidence. A failed preflight blocks the
live apply unless the missing permission is deliberately handled by an
infra-admin stack.

## Build Local Provider

Build the current branch provider before a local-dev Terraform run:

```sh
GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat
```

Use the local provider through Terraform CLI dev overrides. The exact override
location is workstation-specific; do not commit it to this repository.

## Terraform Fixture

Create the disposable VPC, subnet, route tag, gateway VMs, service account, IAM,
and Firestore settings in a scratch Terraform directory under `tmp/`.

Minimum resource shape:

```hcl
terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    betternat = {
      source = "nowakeai/betternat"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

resource "google_compute_network" "lab" {
  name                    = "${var.name}-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "lab" {
  name          = "${var.name}-subnet"
  ip_cidr_range = "10.91.0.0/24"
  region        = var.region
  network       = google_compute_network.lab.id
}

resource "betternat_gcp_gateway" "egress" {
  name       = var.name
  project_id = var.project_id
  region     = var.region
  zone       = var.zone

  network    = google_compute_network.lab.name
  subnetwork = google_compute_subnetwork.lab.name
  client_tag = "${var.name}-client"

  private_cidrs = ["10.91.0.0/24"]

  enable_agent_ha                = true
  manage_firestore_database      = var.manage_firestore_database
  firestore_database_id          = var.firestore_database_id
  firestore_location_id          = var.firestore_location_id
  manage_runtime_service_account = true
  manage_runtime_iam             = var.manage_runtime_iam
  betternat_version              = var.betternat_version
}
```

The fixture must also create:

- a private client VM tagged with `${var.name}-client`,
- a firewall rule that allows SSH/IAP or project-approved debug access,
- a firewall rule allowing peer API and metrics between gateway nodes,
- any additional egress rules required by the disposable subnet.

Do not manage production routes from this fixture.

## Apply Evidence

Run:

```sh
terraform init
terraform plan -out=tfplan
terraform apply tfplan
```

Capture:

- Terraform provider version and git commit,
- plan summary,
- `betternat_gcp_gateway.egress.agent_config_hash`,
- `betternat_gcp_gateway.egress.route_target`,
- gateway instance names,
- service account email,
- route name.

Cross-check GCP resources:

```sh
gcloud --project "$BETTERNAT_GCP_PROJECT" compute instances list \
  --filter="name~${BETTERNAT_GCP_NAME}"
gcloud --project "$BETTERNAT_GCP_PROJECT" compute routes list \
  --filter="name~${BETTERNAT_GCP_NAME}"
gcloud --project "$BETTERNAT_GCP_PROJECT" iam service-accounts list \
  --filter="email~${BETTERNAT_GCP_NAME}"
```

## Private Client Egress

From the private client VM, verify egress through the BetterNAT route:

```sh
curl -fsS https://checkip.amazonaws.com
curl -fsS https://ifconfig.me
```

Record the observed public IP and current route target:

```sh
gcloud --project "$BETTERNAT_GCP_PROJECT" compute routes describe \
  "${BETTERNAT_GCP_NAME}-default-via-gateway" \
  --format=json
```

## Raw LoxiLB Baseline

Before treating BetterNAT GCP HA as a product milestone, run a raw-LoxiLB
comparison in the same disposable network or record why it cannot run in that
environment.

Capture:

- LoxiLB install mode and version,
- whether the HA mode is standalone, BGP active/backup, BGP ECMP active/active,
  kube-loxilb, or another documented upstream pattern,
- whether the mode requires BGP/Cloud Router, Kubernetes, extra routes, or
  privileged peer connectivity,
- who mutates GCP routes or public identity during failover,
- active/standby or active/active status output,
- egress source IP before and after failover,
- datapath counters before and after failover.

Pass condition for BetterNAT comparison:

- the baseline is documented well enough to explain what LoxiLB already solves,
- any LoxiLB primitive reused by BetterNAT is still hidden behind BetterNAT's
  provider-neutral config, status, IAM, and cleanup contracts,
- raw LoxiLB alone is not counted as BetterNAT HA evidence unless it also proves
  lease-fenced cloud route ownership, rollback, and operator-visible status.

## Firestore Contention

Run the reusable Firestore integration test against the same database:

```sh
BETTERNAT_GCP_FIRESTORE_PROJECT="$BETTERNAT_GCP_PROJECT" \
BETTERNAT_GCP_FIRESTORE_DATABASE="$BETTERNAT_GCP_DATABASE" \
GOCACHE=$PWD/tmp/go-build \
  go test ./internal/coordination/firestore \
    -run TestIntegrationFirestoreLeaseContention -count=1 -v
```

Pass condition:

- only one contender holds an unexpired lease,
- stale renew is fenced,
- transfer increments generation,
- registry and handover records are readable,
- cleanup removes the test records.

## Two-Agent HA

On both gateway VMs, collect:

```sh
sudo systemctl status betternat-agent --no-pager
sudo journalctl -u betternat-agent --since -20m --no-pager
curl -fsS http://127.0.0.1:9108/metrics
betternat status
betternat doctor --live
```

Pass condition:

- exactly one node reports active,
- at least one node reports standby and healthy datapath,
- the active node matches the GCP route `nextHopInstance`,
- Firestore lease owner and generation match the active node,
- no standby mutates the route while the active lease is valid.

Record Firestore records for the gateway path with a read-only query or the
Firestore console. Include lease, agent registry, and handover documents in
evidence.

## Passive Failover

Stop or power off the active gateway without a clean handover:

```sh
gcloud --project "$BETTERNAT_GCP_PROJECT" compute instances stop ACTIVE_INSTANCE \
  --zone "$BETTERNAT_GCP_ZONE"
```

Measure:

- time active stopped,
- time standby acquired the next lease generation,
- time route target moved,
- time private client new-flow egress recovered.

Private client probe:

```sh
for i in $(seq 1 60); do
  date -u +%FT%TZ
  curl -fsS --max-time 2 https://checkip.amazonaws.com || true
  sleep 2
done
```

Pass condition:

- standby acquires only after the previous lease expires or is fenced,
- route target moves to the standby instance,
- new flows recover,
- old active does not resume route mutation after restart unless it reacquires a
  current lease generation.

## Proactive Handover

From the active node or an operator host with peer access, request handover to a
healthy standby:

```sh
sudo betternat handover start --to STANDBY_INSTANCE \
  --host unix:///run/betternat/agent.sock
```

Collect:

- command output,
- active and standby agent logs,
- Firestore handover record,
- route object before and after,
- private client probe output.

Pass condition:

- target is prepared and healthy before mutation,
- handover record reaches `completed`,
- route target moves to the standby,
- lease generation transfers to the standby,
- old active does not keep repairing the route back.

## Datapath Restart

On the active node, restart LoxiLB or the selected datapath service:

```sh
sudo systemctl restart loxilb || sudo docker restart loxilb
sudo systemctl restart betternat-agent
```

Collect:

```sh
betternat doctor --live
betternat status
loxicmd get nat || true
curl -fsS http://127.0.0.1:9108/metrics
```

Pass condition:

- the agent reconciles datapath state,
- route ownership does not change solely because of datapath restart,
- datapath counters resume after private client traffic.

## Failure Injection

At minimum, run one controlled failure in each category:

- block Firestore from the active and verify the agent degrades,
- force Compute route operation failure and verify the lease is not advertised
  as active after failed mutation,
- force a failure after route delete but before route insert and record whether
  the previous route is restored,
- make the standby registry stale and verify proactive handover refuses it,
- restart the old active after passive failover and verify it does not repair the
  route back without reacquiring the current lease generation,
- skew one node's clock within the expected tolerance and verify lease renewal
  and takeover behavior remain conservative,
- interrupt route operation polling and verify final route state is checked
  before success.

Record exact command, expected failure, actual failure, and recovery action.

## Destroy And Residual Scan

Destroy the fixture:

```sh
terraform destroy
```

Then scan:

```sh
scripts/gcp-residual-scan.sh \
  --project "$BETTERNAT_GCP_PROJECT" \
  --name "$BETTERNAT_GCP_NAME" \
  --database "$BETTERNAT_GCP_DATABASE"
```

If `manage_firestore_database = false`, delete only BetterNAT records in the
existing database. Do not delete shared Firestore databases owned by another
stack.

The residual scan is read-only. It checks Compute instances, routes, firewall
rules, addresses, service accounts, and BetterNAT Firestore coordination
records under the selected gateway name. It exits nonzero when residual items
are found.

Pass condition:

- no BetterNAT instances remain,
- no BetterNAT routes remain,
- no BetterNAT firewall rules remain,
- provider-owned service account and IAM bindings are gone,
- provider-owned Firestore database is gone, or existing database has no
  BetterNAT records for the run.

## Evidence Template

Create a dated evidence document under `docs/research/` after each complete
run.

Use this outline:

```markdown
# GCP Disposable Integration Results

Date:
Commit:
Project:
Region/zone:
Terraform provider source:
Runtime version:

## Summary

Pass/fail:
Blocking issues:

## Preflight

## Apply

## Private Client Egress

## Firestore Contention

## Raw LoxiLB Baseline

## Two-Agent HA

## Passive Failover

## Proactive Handover

## Datapath Restart

## Failure Injection

## Destroy And Residual Scan

## Decision
```

Do not mark GCP GA complete until this evidence proves every P0 gate in
`docs/research/052-gcp-ha-gap-analysis.md`.
