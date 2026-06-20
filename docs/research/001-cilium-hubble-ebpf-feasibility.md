# Cilium/Hubble vs Self-built eBPF NAT Feasibility

Date: 2026-06-19

## Question

For BetterNAT, should the first implementation use Cilium/Hubble as the core datapath and observability layer, or build a smaller purpose-built eBPF datapath/agent?

## Short Answer

Do not make Cilium/Hubble a hard dependency for the first non-Kubernetes AWS NAT appliance.

Use this staged approach instead:

1. Start with a conventional Linux NAT appliance as the correctness baseline.
2. Add a Go control-plane agent for AWS failover and metrics.
3. Add custom eBPF flow accounting first, without changing packet forwarding.
4. Only after the appliance is stable, experiment with a purpose-built TC eBPF NAT datapath.
5. Treat Cilium/Hubble as an optional integration for Kubernetes/Cilium users, not the default runtime.

## Why Cilium Is Attractive

Cilium already solves many hard networking problems:

- eBPF program loading and lifecycle management.
- BPF maps for connection tracking and load balancing.
- eBPF-based masquerading for Kubernetes pod egress.
- Host routing and kube-proxy replacement modes.
- Hubble flow visibility and Prometheus metrics.
- Operational familiarity for Kubernetes platform teams.

The strongest product positioning would be:

> If the customer already runs Kubernetes with Cilium, BetterNAT can integrate with Cilium/Hubble context. If the customer only wants an AWS NAT Gateway replacement for generic private subnets, BetterNAT should run as a lightweight EC2 appliance without requiring Kubernetes.

## Hard Boundary: Cilium/Hubble Are Kubernetes-first

The official Cilium quick install path stores state in Kubernetes CRDs, and Cilium's own component overview describes the agent as listening for orchestration system events such as Kubernetes to learn when workloads start and stop.

Hubble setup documentation is explicit that Hubble is Cilium's observability layer for a Kubernetes cluster and assumes Cilium is already installed in that cluster.

This matters because an AWS NAT appliance forwarding traffic for arbitrary EC2 private instances does not naturally have:

- Cilium endpoints.
- Pod identity.
- Kubernetes namespace/labels.
- CiliumNetworkPolicy verdicts.
- Cilium-managed service metadata.

Hubble can still observe traffic local to a Cilium agent, but the rich context that makes Hubble compelling mostly comes from Cilium-managed workloads. For arbitrary EC2 source IPs, we would still need our own attribution layer.

## Cilium eBPF Masquerading Fit

Cilium has production-ready IPv4 eBPF masquerading, and IPv6 BPF masquerading is marked beta in current docs. This proves the kernel technique is viable.

However, Cilium masquerading is designed around Cilium's cluster networking model. A generic NAT Gateway replacement needs a different product contract:

- Private subnet instances route `0.0.0.0/0` to a NAT appliance or ENI.
- The appliance SNATs arbitrary VPC source IPs to an EIP/public interface.
- Return packets must be DNATed back to the original private source.
- Flow accounting must remain useful even when the source instance is not a Cilium endpoint.
- Failover must coordinate AWS EIP, ENI, or route table state.

That is adjacent to Cilium, but not the same as simply enabling `bpf.masquerade=true`.

## XDP vs TC for This Product

The original plan says "XDP driver-layer NAT." That is too aggressive for the first version.

TC is the more practical starting point for NAT:

- TC sees skb context and has mature checksum helpers.
- TC is usable for ingress and egress hooks.
- NAT needs IP and L4 checksum updates, bidirectional state, port allocation, and redirect/routing interaction.
- XDP is ingress-oriented and lower-level; it is excellent for early drop, redirect, and high-performance load balancing, but a full stateful NAT appliance at XDP is a larger engineering project.

XDP can still be useful later for:

- DDoS/drop filters.
- Stateless allow/deny filtering.
- Fast path for simple redirects.
- L4 load-balancing experiments.

But for NAT correctness, TC-first is safer.

## What We Would Have To Build Ourselves

Even if using Cilium libraries or design ideas, a standalone NAT appliance needs product-specific pieces:

- AWS control plane:
  - EIP reassociation or route replacement.
  - Optional ENI attach/detach failover.
  - IAM-scoped permissions.
  - Split-brain prevention through DynamoDB/SSM/EC2 tag leases.
- Datapath:
  - SNAT and DNAT mapping.
  - Ephemeral port allocation.
  - TCP, UDP, ICMP handling.
  - Checksum updates.
  - Map sizing, eviction, and flow timeout policy.
  - Fragment and MTU behavior.
  - Multi-queue/RSS and per-CPU performance tuning.
- Observability:
  - Per-source private IP byte/packet counters.
  - Top destination IP/domain approximation.
  - Drop reasons.
  - Map pressure.
  - Failover events.
  - Prometheus exporter.
- Operations:
  - Kernel version pinning.
  - AMI build and test matrix.
  - Upgrade/rollback.
  - Recovery path back to AWS NAT Gateway.

## Main Risks of Building eBPF Ourselves

### Verifier and portability

The Linux eBPF verifier checks program safety before load. This is good for safety but creates a real development tax: code shape, bounded loops, pointer checks, helper availability, and kernel-version differences matter.

Mitigation:

- Use libbpf CO-RE or cilium/ebpf.
- Pin supported kernels, for example Amazon Linux 2023 on Nitro.
- Run verifier/load tests across every supported kernel in CI.
- Keep the first eBPF program observational, not mutating.

### NAT correctness

NAT is deceptively hard. The implementation must handle:

- TCP and UDP timeout differences.
- ICMP errors that embed original packet headers.
- Fragmented packets.
- Port collision and exhaustion.
- Checksum repair.
- Flow eviction under pressure.
- Asymmetric routing during failover.

Mitigation:

- Keep Linux iptables/nftables NAT as the first correctness baseline.
- Build packet-level integration tests with network namespaces.
- Compare behavior against Linux conntrack/NAT before trying to outperform it.

### AWS-specific networking constraints

The EC2 NAT appliance must fit AWS VPC semantics:

- Source/destination check disabled.
- Correct route table target.
- Public subnet placement.
- Security group/NACL behavior.
- EIP/ENI failover propagation.

These constraints are independent of Cilium/eBPF.

### Operational debugging

When the datapath is inside eBPF maps and programs, debugging is harder than inspecting iptables/nftables and conntrack.

Mitigation:

- Ship a `betternat doctor` command.
- Export map pressure and flow samples.
- Keep a feature flag to fall back to Linux NAT.
- Log control-plane changes with AWS request IDs.

## Recommended Product Architecture

### MVP

- Linux NAT appliance using nftables or iptables.
- Go agent:
  - AWS failover.
  - Heartbeat.
  - Prometheus metrics.
  - Health checks.
- Terraform/Packer deployment.
- Clear benchmark against AWS NAT Gateway and a baseline NAT instance.

### MVP+1

- eBPF flow accounting only.
- Attach TC programs to relevant interfaces.
- Count bytes/packets by source private IP, destination CIDR, protocol, and verdict.
- Keep forwarding in Linux NAT.

### MVP+2

- Optional TC eBPF SNAT/DNAT fast path.
- Fall back to Linux NAT on unsupported kernel or verifier failure.
- Benchmark pps/Gbps/CPU/map pressure.

### Later

- Cilium/Hubble integration mode for Kubernetes clusters already running Cilium.
- Optional Hubble-style UI, but backed by our own flow events for non-Kubernetes EC2.

## Decision

Cilium/Hubble are valuable references and possible optional integrations, but they should not be the first product core for generic AWS NAT replacement.

The first implementation should be a purpose-built AWS NAT appliance with:

- Linux NAT correctness baseline.
- Custom Go control plane.
- Custom eBPF observability.
- Optional custom TC eBPF datapath after measurement.

This preserves the strongest product promise: lower NAT Gateway cost with operationally understandable behavior. It also avoids starting the project by bending a Kubernetes-first networking stack into a generic EC2 NAT appliance.

## Sources

- Cilium quick installation says default install stores state in Kubernetes CRDs: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/
- Cilium component overview says the agent listens to orchestration systems such as Kubernetes and manages eBPF programs for workload traffic: https://docs.cilium.io/en/stable/overview/component-overview/
- Hubble setup describes Hubble as Cilium observability for Kubernetes clusters and assumes Cilium is installed: https://docs.cilium.io/en/stable/observability/hubble/setup/
- Hubble node-local API behavior: https://docs.cilium.io/en/stable/observability/hubble/
- Cilium masquerading, including production-ready IPv4 BPF masquerading and beta IPv6 BPF masquerading: https://docs.cilium.io/en/latest/network/concepts/masquerading/
- Cilium eBPF map capacity and connection-tracking map limits: https://docs.cilium.io/en/latest/network/ebpf/maps/
- Linux eBPF verifier documentation: https://docs.kernel.org/bpf/verifier.html
- libbpf overview and CO-RE portability: https://docs.kernel.org/bpf/libbpf/libbpf_overview.html
- BPF CO-RE concept: https://docs.ebpf.io/concepts/core/
- BPF checksum helpers: https://man7.org/linux/man-pages/man7/bpf-helpers.7.html
- XDP program type and packet-size limitation: https://docs.ebpf.io/linux/program-type/BPF_PROG_TYPE_XDP/
- cilium/ebpf Go package: https://pkg.go.dev/github.com/cilium/ebpf
