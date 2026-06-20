# MVP Scope and Milestones

Date: 2026-06-19

## Question

Given the research so far, what should BetterNAT build first, what should be deferred, and how should the work be organized into milestones?

## Product Thesis

BetterNAT is a self-owned egress gateway for high-volume cloud private subnets.

Core value:

1. Reduce NAT Gateway per-GB processing charges for suitable high-volume workloads.
2. Provide better egress observability and cost attribution.
3. Provide low-cost, cloud-native HA with clear failover behavior.
4. Offer Terraform-native installation as if configuring a managed NAT Gateway.

Primary target workloads:

- blockchain RPC/full nodes syncing large volumes from public P2P peers,
- large-scale crawler/scraper fleets,
- Kubernetes nodes frequently pulling large images/artifacts,
- public data ingestion workers pulling data into private storage.

Public headline should emphasize product value, not low-level implementation:

> Low-cost, observable, highly available egress for AWS private subnets.

## MVP Product Contract

v0 should promise:

- self-hosted AWS NAT appliance,
- Terraform provider UX,
- reliable datapath engine with Linux NAT fallback,
- route-based HA,
- optional stable EIP for new connections after failover,
- DynamoDB lease/fencing,
- Prometheus metrics,
- `doctor`,
- rollback metadata,
- cost estimate,
- reproducible benchmark profile.

v0 should not promise:

- AWS NAT Gateway equivalent SLA,
- zero packet loss failover,
- active connection preservation,
- guaranteed 2-5 second failover,
- self-built eBPF NAT datapath,
- pod-level attribution in all EKS setups,
- multi-cloud support.

## v0 Must-Have

### Terraform provider

`terraform-provider-betternat` with AWS backend.

Resource:

```hcl
resource "betternat_gateway" "egress" {
  name   = "prod-egress"
  cloud  = "aws"
  region = "us-west-2"

  vpc_id                  = var.vpc_id
  public_subnet_ids       = var.public_subnet_ids
  private_route_table_ids = var.private_route_table_ids

  high_availability = true
  stable_egress_ip  = true
  observability     = "basic"
}
```

Provider responsibilities:

- create/manage EC2 appliance instances,
- configure IAM role/policy,
- configure security groups,
- configure EIP(s),
- create DynamoDB lease table,
- configure initial route,
- generate agent config,
- expose computed status,
- support import/read/delete safely,
- store rollback target.

### Agent

`betternat-agent` running on each appliance.

v0 responsibilities:

- load generated config,
- configure/check LoxiLB egress SNAT,
- reconcile LoxiLB rules after restart or drift,
- configure/check nftables fallback NAT,
- check sysctls/source-dest forwarding prerequisites,
- renew/acquire DynamoDB lease,
- execute route failover using AWS SDK,
- optionally reassociate shared EIP,
- verify route target and EIP owner,
- export Prometheus metrics,
- expose local health/status,
- structured logs.

### Datapath

v0 default datapath:

- LoxiLB standalone egress SNAT,
- BetterNAT-owned rule reconciliation,
- LoxiLB firewall counters and conntrack reads,
- configured private CIDR allowlist.

v0 fallback datapath:

- Linux IP forwarding,
- nftables SNAT/masquerade,
- nf_conntrack tuning,
- dedicated nftables table,
- configured private CIDR allowlist.

No self-built NAT eBPF in v0.

LoxiLB is now the leading datapath candidate after the M-1 standalone AWS egress spike. v0 should support:

```hcl
datapath_engine = "loxilb" # or "nftables"
```

nftables remains the mandatory fallback until LoxiLB packaging, observability, HA, and benchmark work are complete.

### HA

v0 HA:

- per-AZ active/standby,
- route-table failover with `ReplaceRoute`,
- DynamoDB lease/fencing,
- optional shared-EIP reassociation,
- explicit health checks,
- no connection preservation promise.

### Observability

v0 observability:

- Prometheus exporter,
- HA state metrics,
- route/EIP match metrics,
- conntrack metrics,
- interface counters,
- cost estimate total,
- coarse owner/CIDR attribution if configured,
- basic Grafana dashboard JSON.

No eBPF flow accounting in v0 unless it is extremely low-risk late in the cycle.

### CLI

`betternat` CLI:

- `discover aws`,
- `doctor`,
- `status`,
- `cost estimate`,
- `failover status`,
- `rollback` helper.

CLI uses official SDKs. It does not replace Terraform provider as the main install path.

### Benchmark

v0 must include:

- reproducible benchmark harness,
- one published AWS instance profile,
- target workload profile tests for blockchain-P2P/crawler/image-pull/data-ingest shapes,
- route-only failover measurement,
- shared-EIP failover measurement,
- conntrack pressure test,
- cost break-even example.

## v0 Explicitly Out Of Scope

- eBPF NAT fast path.
- VPP datapath.
- dynamic ENI movement.
- active-active NAT.
- active connection preservation.
- conntrackd state sync.
- multi-cloud provider backends.
- Kubernetes pod attribution.
- Cilium/Hubble integration.
- service mesh integration.
- custom hosted UI.
- automatic modification of every route table in a VPC.
- SSH-enabled admin by default.

## Recommended Repo Structure

```text
.
├── cmd/
│   ├── betternat/
│   └── betternat-agent/
├── internal/
│   ├── agent/
│   │   ├── ha/
│   │   ├── datapath/
│   │   ├── metrics/
│   │   ├── health/
│   │   └── config/
│   ├── cloud/
│   │   ├── aws/
│   │   ├── gcp/
│   │   ├── azure/
│   │   └── alicloud/
│   ├── lease/
│   ├── cost/
│   └── observability/
├── provider/
│   └── terraform-provider-betternat/
│       ├── internal/provider/
│       ├── internal/cloud/aws/
│       └── examples/
├── datapath/
│   ├── nftables/
│   └── ebpf/
├── packer/
├── benchmarks/
├── dashboards/
├── docs/
│   └── research/
└── examples/
```

The `internal/cloud` package should expose provider-neutral interfaces. v0 only implements AWS.

## Milestones

## M-1: LoxiLB NAT Gateway Spike

Goal:

Determine whether LoxiLB can serve as the packet datapath for BetterNAT's generic AWS private-subnet NAT Gateway replacement use case.

Deliverables:

- AWS spike environment.
- Standalone LoxiLB install.
- private subnet route table pointing to LoxiLB instance/ENI.
- private VM egress test through LoxiLB.
- TCP/UDP return-path test.
- multi-source test.
- observed public EIP test.
- LoxiLB metrics/API review.
- HA ownership review.
- IAM/API permission review.

Acceptance:

- private VM reaches internet through standalone LoxiLB,
- public echo endpoint sees expected EIP,
- multiple private source IPs work,
- TCP and UDP return traffic work,
- LoxiLB can run without Kubernetes for this scenario,
- LoxiLB exposes enough stats or APIs for BetterNAT observability,
- LoxiLB can be controlled or embedded without conflicting with BetterNAT agent HA,
- required AWS permissions can be narrowed to an acceptable production scope.

Current result:

- Functional AWS route-through egress SNAT: passed.
- Private client public IP matched appliance EIP: passed.
- TCP return path: passed.
- LoxiLB conntrack/API visibility: passed for CLI/API-derived attribution; native Prometheus endpoint not found.
- UDP/DNS: passed with direct `dig @1.1.1.1` and `dig @8.8.8.8`.
- EIP + `ReplaceRoute` failover: passed for new connections, with stable public egress IP.
- High response-volume smoke test: passed with 10MB and 137MB downloads.
- Config persistence: failed in tested container mode; rules disappeared after `docker restart loxilb`.
- Prometheus endpoint: not validated; direct `/metrics` returned `404`.
- HA: basic active/standby route/EIP failover tested; active connection preservation not tested.

Detailed results:

- `docs/research/021-loxilb-spike-results.md`
- `docs/research/022-loxilb-extended-spike-results.md`

Decision after M-1:

- If pass: support `datapath_engine = "loxilb"` candidate and benchmark against nftables.
- If partial: keep LoxiLB for Kubernetes egress integration/reference only.
- If fail: proceed with nftables-only v0.

Current decision:

- Promote LoxiLB to leading v0 datapath target.
- Keep nftables as mandatory fallback.
- Make `betternat-agent` responsible for LoxiLB rule reconciliation and metrics re-export.

## M0: Local NAT Appliance Prototype

Goal:

Prove the fallback Linux NAT baseline works in a controlled environment.

Deliverables:

- nftables rules generator,
- sysctl setup,
- local network namespace test,
- basic conntrack metrics,
- minimal agent process.

Acceptance:

- private namespace can reach internet/test endpoint through appliance namespace,
- return traffic works,
- counters increment,
- no Terraform/provider yet.

## M1: AWS Single-node NAT Appliance

Goal:

Run one BetterNAT appliance in AWS.

Deliverables:

- Packer/bootstrap install,
- EC2 instance NAT appliance,
- source/destination check disabled,
- private route table points to appliance,
- Prometheus metrics,
- `doctor`.

Acceptance:

- private test instance reaches internet through appliance,
- observed public IP matches appliance EIP,
- `doctor` passes,
- rollback to previous NAT Gateway/route target works.

## M2: Terraform Provider AWS Create/Read

Goal:

Make users create the appliance via `betternat_gateway`.

Deliverables:

- Terraform provider skeleton,
- AWS backend create/read,
- basic schema,
- EC2/IAM/security group/EIP/route creation,
- generated agent config,
- import/read status.

Acceptance:

- `terraform apply` creates working single-node NAT,
- `terraform refresh` reads current state,
- provider exposes egress IP and status,
- no manual AWS resource creation needed.

## M3: HA Route Failover

Goal:

Implement active/standby route failover.

Deliverables:

- DynamoDB lease backend,
- HA state machine,
- route failover via AWS SDK `ReplaceRoute`,
- failover metrics,
- structured audit logs.

Acceptance:

- stopping active instance triggers standby takeover,
- route target changes to standby,
- new outbound connections recover,
- split-brain test does not flap route,
- failover event logged with lease generation and AWS request IDs.

## M4: Shared EIP Mode

Goal:

Keep public egress IP stable for new connections after failover.

Deliverables:

- shared EIP reassociation,
- EIP ownership verification,
- outbound source IP probe,
- provider schema for `stable_egress_ip`.

Acceptance:

- before failover, private probe sees EIP-X,
- after failover, new private probe sees same EIP-X,
- agent does not report active until route and EIP verification pass,
- existing connections may reset and are documented.

## M5: Observability MVP

Goal:

Deliver differentiated visibility without eBPF.

Deliverables:

- Prometheus exporter,
- conntrack metrics,
- interface metrics,
- HA metrics,
- route/EIP mismatch metrics,
- estimated NAT Gateway cost metric,
- Grafana overview dashboard,
- `top owners` from configured CIDR counters.

Acceptance:

- dashboard shows active node, throughput, conntrack, failovers, cost estimate,
- alerts fire for route mismatch and conntrack pressure,
- metric cardinality remains bounded.

## M6: Benchmark Harness

Goal:

Publish first reproducible capacity profile.

Deliverables:

- benchmark scripts,
- traffic generator setup,
- pps/throughput/new connection tests,
- HA failover benchmark,
- shared-EIP failover benchmark,
- cost profile.

Acceptance:

- raw results committed/published,
- one recommended AWS instance profile documented,
- public claims match measured data.

## M7: Public Beta Packaging

Goal:

Make it usable by early adopters.

Deliverables:

- provider release,
- AMI release,
- docs quickstart,
- security guide,
- rollback guide,
- benchmark report,
- example configs.

Acceptance:

- new user can deploy to existing VPC from docs,
- can verify with `doctor`,
- can roll back,
- can inspect metrics,
- known limitations are explicit.

## v1 Roadmap

After v0:

- eBPF flow accounting,
- top source IP/destination CLI,
- Kubernetes metadata enrichment,
- richer cost attribution,
- AWS Price List API integration,
- CloudWatch NAT Gateway import,
- planned failover/maintenance mode,
- Terraform provider update/delete hardening.

## v2 Roadmap

Later:

- dynamic ENI/private IP advanced mode,
- conntrackd or flow-state sync research,
- multi-cloud backends,
- Cilium/Hubble integrations,
- service mesh/egress gateway integrations,
- optional flow export to ClickHouse/S3/OTLP,
- eBPF NAT fast path only if benchmarks justify it.

## Key Risks

### Terraform provider complexity

Mitigation:

- start with narrow AWS schema,
- keep reference module/prototype for backend validation,
- acceptance tests in disposable AWS account.

### Terraform state vs runtime HA drift

Mitigation:

- provider treats active target as computed runtime state,
- agent owns runtime route mutation,
- provider `Read` understands expected active/standby changes.

### Failover timing variability

Mitigation:

- publish measured p50/p95/p99,
- do not promise fixed seconds before proof,
- separate detection/API/convergence/probe phases.

### Security blast radius

Mitigation:

- generated least-privilege IAM,
- agent allowlists,
- lease/fencing,
- SSM-only,
- audit logs.

### Performance uncertainty

Mitigation:

- nftables fallback baseline,
- LoxiLB spike before final datapath commitment,
- benchmark before acceleration,
- self-built eBPF/VPP deferred.

## Decision

Build v0 as an AWS-first, Terraform-provider-first, route-failover NAT appliance with an explicit datapath engine decision.

Current default:

- nftables/nf_conntrack remains the safe fallback.
- LoxiLB gets an M-1 spike before finalizing the v0 datapath.
- self-built eBPF NAT and VPP remain deferred.

The fastest credible path is:

```text
M-1 LoxiLB NAT Gateway spike
M0 local datapath
M1 AWS single-node appliance
M2 Terraform provider
M3 route HA
M4 shared EIP
M5 observability
M6 benchmark
M7 public beta
```

This scope is ambitious but coherent. It directly supports the three product pillars without taking on self-built eBPF NAT, VPP, multi-cloud, or pod attribution too early. LoxiLB may become a reused datapath engine if the spike proves it fits generic AWS private-subnet egress.
