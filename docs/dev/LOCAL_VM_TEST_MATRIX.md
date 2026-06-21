# Local VM Test Matrix

Last updated: 2026-06-20

## Purpose

This document lists the BetterNAT test items that can run in a local VM environment.

It is intentionally environment-agnostic. A "local VM" can be OrbStack, Lima, Multipass, UTM, VMware, a bare Linux workstation, or any Linux runner with sufficient privileges.

Use this alongside:

- `docs/dev/LINUX_DATAPATH_VALIDATION.md`
- `docs/dev/AI_WORKFLOW.md`
- `docs/architecture.md`
- `docs/spec-v0.md`

## VM Shapes

### Single Linux VM

Useful for:

- Linux build and unit tests,
- nftables/nf_conntrack integration,
- agent local datapath reconciliation,
- metrics rendering,
- LoxiLB local smoke if the VM supports the runtime.

Minimum:

- Linux kernel with network namespaces,
- `sudo`,
- `iproute2`,
- Go toolchain compatible with `go.mod`.

Recommended packages:

```sh
sudo apt-get update
sudo apt-get install -y \
  ca-certificates curl jq \
  iproute2 nftables conntrack netcat-openbsd \
  tcpdump iperf3 dnsutils \
  golang-go
```

### Two Linux VMs

Useful for:

- active/standby process-level HA simulation,
- multi-node lease/fencing behavior with local fake/cloud mock,
- failure injection by stopping one VM or one agent,
- observing metrics from separate nodes,
- testing install/bootstrap assumptions across separate machine identities.

### Three Or More Linux VMs

Useful for:

- quorum-like lease stress tests, even if v0 uses a single DynamoDB row,
- multiple standby candidates,
- rolling upgrade simulation,
- mixed-version compatibility checks,
- multi-AZ-like topology simulation without AWS route tables.

This still does not replace real AWS route/EIP testing.

## Test Matrix

| ID | Test Area | VM Count | Requires Root | Current Status | What It Proves | What It Does Not Prove |
|----|-----------|----------|---------------|----------------|----------------|------------------------|
| VM-001 | Linux build and unit tests | 1 | No | Ready | Code compiles and tests pass on Linux/arm64 or Linux/amd64 | Real datapath behavior |
| VM-002 | CLI static smoke | 1 | No | Ready | `doctor`, `status`, `datapath status`, and `failover status` parse config and report expected static state | Real cloud/datapath mutation |
| VM-003 | Terraform provider build | 1 | No | Ready | Provider compiles on Linux | Terraform CLI plan/apply lifecycle |
| VM-004 | Agent validate-only config load | 1 | No | Ready | JSON/YAML config parsing and validation on Linux | Runtime reconciliation |
| VM-005 | Prometheus text rendering | 1 | No | Ready | Metrics output format remains parseable on Linux | Real counter collection |
| VM-006 | nftables masquerade smoke | 1 | Yes | Ready: `scripts/linux-smoke-nftables.sh` | Real nftables NAT, counters, conntrack visibility, namespace cleanup | AWS routing, public egress IP |
| VM-007 | nftables UDP/DNS smoke | 1 | Yes | Ready: `scripts/linux-smoke-nftables-udp.sh` | UDP NAT and DNS-like traffic through fallback datapath | Long-lived TCP behavior |
| VM-008 | nftables cleanup safety | 1 | Yes | Ready: `scripts/linux-smoke-nftables-cleanup.sh` | BetterNAT cleanup removes only BetterNAT-owned tables/chains/rules | Full firewall coexistence in production |
| VM-009 | conntrack parser against live output | 1 | Yes | Ready: covered by VM-006 and VM-007 | Parser handles real TCP and UDP `conntrack -L` shape | Large table performance |
| VM-010 | agent nftables fallback reconcile | 1 | Yes | Ready: `scripts/linux-smoke-agent-nftables.sh` | `betternat-agent --once` can apply fallback rules on Linux | HA or cloud failover |
| VM-011 | agent metrics from live nftables datapath | 1 | Yes | Ready: `scripts/linux-smoke-agent-nftables.sh` | Agent exports Prometheus text from a real fallback datapath namespace | LoxiLB counters |
| VM-012 | local route forwarding throughput baseline | 1 | Yes | Ready: `scripts/linux-smoke-nftables-throughput.sh` | Rough per-VM iperf throughput baseline for fallback datapath | AWS ENA/Nitro performance |
| VM-013 | LoxiLB container starts | 1 | Yes / privileged runtime | Attempted: `scripts/linux-smoke-loxilb.sh`; blocked by current OrbStack VM runtime/kernel behavior | Local runtime can start or reach LoxiLB when `loxicmd`/runtime exists | AWS datapath behavior |
| VM-014 | LoxiLB rule create/read | 1 | Yes / privileged runtime | Blocked in current VM: LoxiLB API did not become ready | `loxicmd` or API can create and read egress SNAT rules | Rule persistence across AWS appliance restart |
| VM-015 | LoxiLB counter parsing | 1 | Yes / privileged runtime | Blocked in current VM: no live LoxiLB API output | BetterNAT parser works against live LoxiLB output | High-volume production accuracy |
| VM-016 | LoxiLB restart reconciliation | 1 | Yes / privileged runtime | Blocked in current VM: no live LoxiLB API output | Agent can replay desired rules after LoxiLB restart | EC2 reboot and EIP failover |
| VM-017 | lease manager local contention | 1+ | No | Ready: `scripts/local-ha-sim.sh` | Only one owner wins with a shared in-process lease backend | DynamoDB conditional write behavior |
| VM-018 | agent active/standby state machine | 1+ | No | Ready at unit/simulation level: `scripts/local-ha-sim.sh` | Activation flow and state transitions are exercised without cloud mutation | AWS route/EIP mutation |
| VM-019 | split-brain guard simulation | 1+ | No | Ready at unit/simulation level: `scripts/local-ha-sim.sh` | Fencing/generation checks prevent stale ownership in local mocks | Real AWS API race behavior |
| VM-020 | metrics labels across multiple agents | 2+ | No | Partially covered by VM-005 and VM-011 | Prometheus labels render consistently for configured node/gateway/HA group | Prometheus deployment integration |
| VM-021 | bootstrap script lint/smoke | 1 | No | Ready: `scripts/local-bootstrap-smoke.sh` | User-data renderer stays valid and test-covered | AMI image parity or live systemd/docker setup |
| VM-022 | dependency freshness check | 1 | No | Ready if network available | Go dependency status from Linux network environment | Compatibility after upgrade unless verify also runs |
| VM-023 | race/stress test subset | 1 | No | Ready: `scripts/local-ha-sim.sh` | Local concurrency bugs in lease/agent/failover code | Kernel datapath correctness |
| VM-024 | mixed architecture smoke | 2+ optional | No | Not available in current local VM set: arm64 only | Behavior across arm64/amd64 builds if both VM types exist | AWS instance family performance |

## Ready-To-Run Commands

### Linux Build And Static Verification

Inside any Linux VM where the repo is mounted:

```sh
cd /path/to/betternat
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat doctor --config examples/agent-config.yaml
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat failover status --config examples/agent-config.yaml
```

Optional wrapper:

```sh
./manage verify
```

### nftables/conntrack Smoke

Install prerequisites:

```sh
sudo apt-get update
sudo apt-get install -y nftables conntrack iproute2 netcat-openbsd tcpdump iperf3 dnsutils jq curl ca-certificates
```

Run:

```sh
./scripts/linux-smoke-nftables.sh
./scripts/linux-smoke-nftables-udp.sh
./scripts/linux-smoke-nftables-cleanup.sh
./scripts/linux-smoke-agent-nftables.sh
./scripts/linux-smoke-nftables-throughput.sh
```

Current expected success output:

```text
nftables smoke ok
nftables udp smoke ok
nftables cleanup safety ok
agent nftables smoke ok
nftables throughput smoke ok: <mbits> Mbits/sec
```

### HA, Bootstrap, And Dependency Smokes

```sh
./scripts/local-ha-sim.sh
./scripts/local-bootstrap-smoke.sh
./manage deps check
```

`local-ha-sim.sh` runs targeted lease/HA tests with Go's race detector and repeats the local ownership/fencing tests.

`local-bootstrap-smoke.sh` intentionally validates render/lint behavior only. It does not run the generated user-data against the host VM because that would modify systemd, Docker, sysctl, and `/etc`.

### LoxiLB Runtime Probe

```sh
./scripts/linux-smoke-loxilb.sh
./scripts/linux-smoke-agent-loxilb.sh
```

These scripts exit `77` when the VM does not have `loxicmd`, Docker, or Podman. That is a skip, not a pass.

When a container runtime is present, failure to reach the LoxiLB API is a real local-environment failure and should be recorded with logs. In the current OrbStack Ubuntu VM, Podman could pull and start the image, but the LoxiLB API did not become ready.

### Example: OrbStack Runner

OrbStack is just one runner. Do not put OrbStack commands inside portable scripts.

Example:

```sh
orbctl run -m ubuntu -w <repo-on-linux> ./manage verify
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-agent-nftables.sh
```

## What Multiple Local VMs Add

Multiple local VMs are useful for control-plane behavior:

- independent process identity,
- independent network stack,
- active/standby lifecycle,
- failure injection,
- metrics from more than one agent,
- rolling upgrade simulation.

They are less useful for cloud-plane behavior:

- no AWS route table propagation,
- no EIP association,
- no EC2 source/destination check,
- no IAM enforcement,
- no real public egress identity transition.

## Local VM Tests That Still Need New Harness Work

The following are good next implementation targets:

1. `scripts/linux-smoke-loxilb-container.sh`
   - refine the current LoxiLB container smoke for a Linux VM/kernel where LoxiLB can fully initialize its eBPF datapath and API.
2. `scripts/local-ha-process-sim.sh`
   - start two or more independent agent processes with fake cloud/lease backends and simulate active failure.
3. Go integration tests behind a build tag such as `linux_integration`
   - useful for CI runners that can provide `CAP_NET_ADMIN`.
4. Mixed-architecture runner coverage.
   - requires both arm64 and amd64 local VMs or CI runners.

## Out Of Scope For Local VM

These must remain AWS integration tests or spikes:

- real `ec2:ReplaceRoute`,
- real EIP association/reassociation,
- EC2 `SourceDestCheck` behavior,
- IAM policy simulation against real roles,
- route table rollback against real AWS route tables,
- public egress IP verification after failover,
- cross-AZ route-table and subnet behavior,
- instance family performance under ENA/Nitro.

## Current Observed Local VM

Observed on 2026-06-20:

```text
Ubuntu 24.04 noble
arm64
kernel 7.0.11-orbstack
network namespaces supported
sudo available without password
ip_forward enabled
repo mounted from macOS
```

Validated:

```sh
orbctl run -m ubuntu -w <repo-on-linux> ./manage verify
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables-udp.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables-cleanup.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-agent-nftables.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables-throughput.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/local-ha-sim.sh
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/local-bootstrap-smoke.sh
orbctl run -m ubuntu -w <repo-on-linux> ./manage deps check
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-loxilb.sh
```

Results:

- Linux/arm64 build/unit/static smoke passed.
- Real nftables masquerade + conntrack smoke passed.
- Real nftables UDP NAT + UDP conntrack smoke passed.
- BetterNAT-owned nftables cleanup safety smoke passed.
- `betternat-agent --once` applied real nftables fallback rules and emitted Prometheus text in a network namespace.
- Local nftables throughput baseline passed; observed output on the test VM was approximately `99433 Mbits/sec`.
- Lease/HA race and repeated local fencing simulation passed.
- Bootstrap render smoke passed.
- Dependency freshness check completed and reported available updates for several Go modules; dependency upgrade was not mixed into this local-test commit.
- A cloned second local VM also passed `./manage verify`, `scripts/linux-smoke-nftables.sh`, `scripts/linux-smoke-agent-nftables.sh`, and `scripts/local-ha-sim.sh`.
- Go toolchain auto-downloaded `go1.25.0 linux/arm64` from Ubuntu's Go 1.22 bootstrap package.
- Podman was installed to attempt LoxiLB local live smoke. `ghcr.io/loxilb-io/loxilb:latest` was pulled and started with `--api --fallback`, but `loxicmd get lbversion -o json` never returned ready API output. Logs included libbpf/TC hook errors such as `Kernel error message: Invalid handle`; one concurrent run also hit `llb_xh_init` assertion after two containers contended for the host datapath. Treat VM-013 through VM-016 as blocked in this OrbStack VM, not passed.
