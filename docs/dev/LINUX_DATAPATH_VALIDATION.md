# Linux Datapath Validation

Last updated: 2026-06-20

## Purpose

BetterNAT can run most unit tests and static CLI smoke checks on macOS, but real datapath behavior requires Linux.

This document defines the portable Linux validation target. It should work on any suitable Linux host:

- a local VM such as OrbStack, Lima, Multipass, UTM, or VMware,
- a bare Linux workstation,
- a disposable EC2 instance,
- a CI runner with the required kernel privileges.

Do not make the test design depend on one developer-specific VM product.

## Validation Layers

### L0: Linux Build And Unit Tests

Purpose:

- confirm Linux/arm64 or Linux/amd64 compilation,
- catch Linux-specific path, permission, syscall, and build issues.

Portable command:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
```

Equivalent convenience command:

```sh
./manage verify
```

### L1: Legacy nftables/nf_conntrack Integration

Purpose:

- validate legacy nftables diagnostics against real Linux nftables while the
  code remains,
- verify conntrack parsing against real conntrack output,
- verify counters and cleanup behavior,
- exercise a local network namespace topology without cloud dependencies.

Minimum tools:

```sh
sudo apt-get update
sudo apt-get install -y nftables conntrack iproute2 iperf3 dnsutils tcpdump jq curl ca-certificates
```

Minimum host capabilities:

- `sudo` for namespace and nftables operations,
- `ip netns` support,
- `nft` available,
- `conntrack` available,
- kernel forwarding support.

Suggested topology:

```text
client namespace
  |
  veth
  |
gateway namespace or root namespace
  |
  veth
  |
server namespace
```

The test should:

1. create temporary namespaces and veth pairs,
2. enable forwarding,
3. apply BetterNAT-owned nftables masquerade rules,
4. send TCP and UDP traffic,
5. verify rule counters increase,
6. verify conntrack entries are visible,
7. remove only BetterNAT-owned rules,
8. delete all temporary namespaces and links.

The portable smoke script is:

```sh
scripts/linux-smoke-nftables.sh
```

That script should not call OrbStack, Lima, Multipass, Docker Desktop, AWS, or any personal profile directly.

Run it directly on any prepared Linux host:

```sh
./scripts/linux-smoke-nftables.sh
```

### L2: LoxiLB Local Smoke

Purpose:

- validate local LoxiLB control through `loxicmd` or a future API client,
- verify firewall rule creation/readback,
- verify counter parsing,
- verify agent reconciliation after LoxiLB restart.

Minimum tools:

- container runtime or native LoxiLB installation,
- privileged networking support,
- `loxicmd`.

Caveats:

- local VM architecture matters; arm64 hosts need an arm64-compatible LoxiLB image or binary,
- container `--privileged` and `--network host` behavior depends on the VM/runtime,
- if LoxiLB local mode is blocked by VM/runtime constraints, run this layer on a disposable Linux EC2 instance instead.

### L3: AWS Integration

Purpose:

- validate real `ReplaceRoute`,
- validate EIP reassociation,
- validate EC2 source/destination check behavior,
- validate IAM policy scope,
- validate public egress IP failover.

This layer must use disposable resources and explicit cleanup. It does not belong in the default local validation loop.

## Example: Running On A Local VM

If the repo is mounted into a Linux VM, run direct commands inside the VM:

```sh
cd /path/to/fuck-nat
GOCACHE=$PWD/tmp/go-build go test ./...
```

For VM products that support executing a command from the host, pass the same direct command through the VM tool. For example, with OrbStack:

```sh
orbctl run -m ubuntu -w <repo-on-linux> \
  GOCACHE=<repo-on-linux>/tmp/go-build go test ./...
```

The OrbStack command is only an example. Do not encode it into the core test script.

## Current Local Probe

One observed local VM was:

```text
Ubuntu 24.04 noble
arm64
kernel 7.0.11-orbstack
network namespaces supported
sudo available without password
ip_forward enabled
missing by default: go, docker, nft, conntrack, loxicmd
```

This is suitable for L0 after Go installation and L1 after installing nftables/conntrack tools. L2 depends on LoxiLB arm64/runtime support.
