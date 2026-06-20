# Local VM Test Matrix

Last updated: 2026-06-20

## Purpose

This document lists the BetterNAT test items that can run in a local VM environment.

It is intentionally environment-agnostic. A "local VM" can be OrbStack, Lima, Multipass, UTM, VMware, a bare Linux workstation, or any Linux runner with sufficient privileges.

Use this alongside:

- `docs/deployment/LINUX_DATAPATH_VALIDATION.md`
- `docs/deployment/AI_WORKFLOW.md`
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
| VM-007 | nftables UDP/DNS smoke | 1 | Yes | Planned | UDP NAT and DNS-like traffic through fallback datapath | Long-lived TCP behavior |
| VM-008 | nftables cleanup safety | 1 | Yes | Planned | BetterNAT cleanup removes only BetterNAT-owned tables/chains/rules | Full firewall coexistence in production |
| VM-009 | conntrack parser against live output | 1 | Yes | Partially covered by VM-006 | Parser handles real `conntrack -L` shape | Large table performance |
| VM-010 | agent nftables fallback reconcile | 1 | Yes | Planned | `betternat-agent --once` can apply fallback rules on Linux | HA or cloud failover |
| VM-011 | agent metrics from live nftables counters | 1 | Yes | Planned | Agent exports counters from real fallback datapath | LoxiLB counters |
| VM-012 | local route forwarding throughput baseline | 1 | Yes | Planned | Rough per-VM iperf throughput and CPU baseline for fallback datapath | AWS ENA/Nitro performance |
| VM-013 | LoxiLB container starts | 1 | Yes / privileged runtime | Planned | Local runtime can start LoxiLB | AWS datapath behavior |
| VM-014 | LoxiLB rule create/read | 1 | Yes / privileged runtime | Planned | `loxicmd` or API can create and read egress SNAT rules | Rule persistence across AWS appliance restart |
| VM-015 | LoxiLB counter parsing | 1 | Yes / privileged runtime | Planned | BetterNAT parser works against live LoxiLB output | High-volume production accuracy |
| VM-016 | LoxiLB restart reconciliation | 1 | Yes / privileged runtime | Planned | Agent can replay desired rules after LoxiLB restart | EC2 reboot and EIP failover |
| VM-017 | lease manager local contention | 2+ | No | Planned | Only one agent becomes active with a shared local/fake lease backend | DynamoDB conditional write behavior |
| VM-018 | agent active/standby state machine | 2+ | No | Planned | Standby transitions after active failure in a local simulation | AWS route/EIP mutation |
| VM-019 | split-brain guard simulation | 2+ | No | Planned | Fencing/generation checks prevent stale active state in local mocks | Real AWS API race behavior |
| VM-020 | metrics labels across multiple agents | 2+ | No | Planned | Prometheus labels remain stable across active/standby nodes | Prometheus deployment integration |
| VM-021 | bootstrap script lint/smoke | 1 | Maybe | Planned | Rendered user-data can run enough setup steps on Ubuntu | AMI image parity |
| VM-022 | dependency freshness check | 1 | No | Ready if network available | Go dependency status from Linux network environment | Compatibility after upgrade unless verify also runs |
| VM-023 | race/stress test subset | 1 | No | Planned | Local concurrency bugs in lease/agent/failover code | Kernel datapath correctness |
| VM-024 | mixed architecture smoke | 2+ optional | No | Planned | Behavior across arm64/amd64 builds if both VM types exist | AWS instance family performance |

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
```

Current expected success output:

```text
nftables smoke ok
```

### Example: OrbStack Runner

OrbStack is just one runner. Do not put OrbStack commands inside portable scripts.

Example:

```sh
orbctl run -m ubuntu -w <repo-on-linux> ./manage verify
orbctl run -m ubuntu -w <repo-on-linux> ./scripts/linux-smoke-nftables.sh
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

1. `scripts/linux-smoke-nftables-udp.sh`
   - validate UDP traffic and conntrack visibility.
2. `scripts/linux-smoke-agent-nftables.sh`
   - run `betternat-agent --once` against a temporary namespace/fallback config.
3. `scripts/linux-smoke-loxilb.sh`
   - start LoxiLB if a compatible runtime is present and verify rule create/read/counters.
4. `scripts/local-ha-sim.sh`
   - start two or more agents with fake cloud/lease backends and simulate active failure.
5. Go integration tests behind a build tag such as `linux_integration`
   - useful for CI runners that can provide `CAP_NET_ADMIN`.

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
```

Results:

- Linux/arm64 build/unit/static smoke passed.
- Real nftables masquerade + conntrack smoke passed.
- Go toolchain auto-downloaded `go1.25.0 linux/arm64` from Ubuntu's Go 1.22 bootstrap package.
