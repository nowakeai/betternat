# Datapath Comparison: nftables/nf_conntrack vs eBPF vs VPP

Date: 2026-06-19

## Question

For BetterNAT, should the NAT datapath be built on:

1. Linux `nftables` + `nf_conntrack`,
2. custom eBPF,
3. FD.io VPP,
4. or a combination?

## Short Answer

Current decision as of 2026-06-25:

This comparison is superseded by the LoxiLB spikes and the no-fallback
decision. The supported v0 datapath is LoxiLB standalone egress SNAT.
`nftables` + `nf_conntrack` may remain only as legacy diagnostic code while it
is phased out. Custom eBPF NAT and VPP remain deferred.

Superseded fallback note: BetterNAT no longer has a product fallback datapath.
This comparison is retained as historical research only. The current sources of
truth are `docs/architecture.md`, `docs/spec-v0.md`, and
`docs/research/055-no-nftables-fallback-decision.md`.

Use a layered strategy:

```text
Default v0 datapath:
  LoxiLB standalone egress SNAT

Product fallback datapath:
  none

Default v1 observability:
  custom eBPF flow accounting

Optional future acceleration:
  TC eBPF NAT fast path

Optional separate performance edition:
  VPP NAT44 appliance
```

Do not start with custom eBPF NAT or VPP as the default product path. Both can be powerful, but they move the project from "reliable AWS NAT appliance" into "network dataplane engineering product" too early.

## One-line Comparison

| Option | Best At | Worst At | Fit For BetterNAT |
| --- | --- | --- | --- |
| `nftables` + `nf_conntrack` | Correct, boring Linux NAT | Extreme pps / huge connection churn | Historical baseline; legacy diagnostics only |
| custom eBPF | Low-overhead observability and targeted fast path | Full stateful NAT correctness | Use for observability first |
| VPP | Purpose-built high-performance packet processing | Operational simplicity on EC2 | Later performance edition |

## Option 1: nftables + nf_conntrack

### What it is

`nftables` is the modern Linux Netfilter framework replacing iptables/ip6tables/arptables/ebtables. For stateful NAT, nftables uses the kernel `nf_conntrack` engine.

For BetterNAT, the datapath would be:

```text
private EC2
  -> VPC route table
  -> NAT appliance ENI
  -> Linux forwarding path
  -> nf_conntrack
  -> nftables SNAT/masquerade
  -> public ENI/EIP
  -> internet
```

Return packets traverse the same appliance and conntrack translates them back.

### Why it fits

This is the lowest-risk implementation for a NAT Gateway replacement:

- Mature Linux kernel path.
- Handles TCP/UDP/ICMP stateful NAT semantics.
- Debuggable with standard tools: `nft`, `conntrack`, `ss`, `ip`, `ethtool`, `tcpdump`.
- Works with normal distro packaging.
- Easy to bootstrap in AMI or cloud-init.
- Easy to roll back.
- Does not require us to implement NAT state machine on day one.

### Performance profile

Good when:

- Ruleset is small.
- Traffic is mostly normal MTU TCP/UDP.
- Conntrack table is sized correctly.
- Instance has enough ENA bandwidth and pps headroom.
- IRQ/RSS/queue distribution is not broken.

Bad when:

- Extremely high packets-per-second.
- Very high new connections per second.
- Conntrack table is near full.
- Large numbers of tiny UDP flows.
- Hash buckets are undersized.
- Rules are written as long linear chains.
- Logging or complex matching is on the hot path.

### Main bottleneck

The bottleneck is usually not "nftables syntax." It is:

- `nf_conntrack`.
- CPU softirq.
- ENA/instance pps limits.
- conntrack memory.
- IRQ/RSS imbalance.
- route/failover topology.

### Observability

Baseline observability:

- nftables counters.
- conntrack count/max.
- conntrack errors/drops.
- interface bytes/packets/drops.
- CPU softirq.

Limitations:

- Per-source top talkers are awkward if implemented only with nftables counters.
- Per-flow attribution can become expensive if represented as many rules.
- Domain attribution requires DNS correlation or flow logs.

### HA impact

Works well with AWS-native HA:

- Route table target failover.
- EIP reassociation.
- Secondary ENI/IP failover.

But existing connections usually reset during failover unless state and path continuity are solved. `conntrackd` can be researched later for state sync, but it should not block MVP.

### Development cost

Low.

Most work is product engineering:

- Terraform.
- AMI/bootstrap.
- nft ruleset generation.
- sysctl tuning.
- monitoring.
- AWS failover agent.
- doctor/rollback.

### Verdict

Use this as the default v0/v1 datapath.

## Option 2: Custom eBPF

There are two very different eBPF ideas:

1. eBPF for observability.
2. eBPF for NAT forwarding.

They should not be treated as the same risk level.

## eBPF for Observability

### What it is

Attach eBPF programs to TC, XDP, tracepoints, kprobes, or cgroup hooks to count and sample traffic without putting rules on the packet hot path.

For BetterNAT, the first useful version is probably TC ingress/egress accounting:

```text
packet enters/leaves NAT appliance
  -> eBPF observes 5-tuple/source/destination/bytes
  -> updates per-CPU maps
  -> Go agent exports Prometheus metrics and top-N views
```

### Why it fits

This directly supports the strongest product differentiator: observability.

It can answer:

- Which source private IP is using bandwidth?
- Which destination IP/CIDR is expensive?
- Which protocol/port dominates?
- Are drops happening at interface/conntrack/datapath level?
- What changed around failover?

### Risk level

Moderate and acceptable.

Observational eBPF is much easier than mutating NAT packets:

- No port allocator.
- No SNAT/DNAT state machine.
- No checksum rewrite for forwarding correctness.
- No need to replace conntrack.
- Failure can degrade metrics while forwarding continues through nftables.

### Development stack

Recommended:

- Go agent.
- `github.com/cilium/ebpf`.
- `bpf2go`.
- TC attach through netlink or helper libraries.
- BPF maps pinned under `/sys/fs/bpf/betternat`.
- Prometheus exporter.

### Verdict

Do this in v1, possibly even late v0. It is high product value with controlled blast radius.

## eBPF for NAT Fast Path

### What it is

Implement SNAT/DNAT directly in eBPF, probably at TC rather than XDP:

```text
private packet
  -> TC egress/ingress
  -> lookup/create NAT mapping in BPF map
  -> rewrite source IP/port
  -> update checksums
  -> redirect/allow forward

return packet
  -> lookup reverse mapping
  -> rewrite destination IP/port
  -> update checksums
  -> forward to private source
```

### Why TC before XDP

TC is a better first place for NAT than XDP:

- TC has skb context.
- It is closer to Linux routing/qdisc integration.
- Checksum helpers are mature.
- It can attach ingress and egress.
- XDP is excellent for early drop/redirect, but full stateful NAT at XDP adds more constraints.

### Why it is hard

Full NAT needs:

- Bidirectional flow table.
- Ephemeral port allocation.
- Collision handling.
- TCP/UDP timeout policy.
- ICMP error handling.
- Fragment behavior.
- Checksum updates.
- Map sizing and eviction.
- Per-CPU consistency.
- Failover state strategy.
- Kernel-version portability.
- Good fallback when verifier rejects the program.

The Linux eBPF verifier protects the kernel by statically validating programs. That is good, but it means code shape, bounded loops, pointer checks, helper availability, and kernel version matter.

### Performance upside

Potentially meaningful if benchmarks show:

- nftables/conntrack CPU cost is the bottleneck.
- Traffic pattern is suitable for a fast path.
- We can safely fall back for unsupported flows.

But without real EC2 benchmarks, this is speculative.

### HA impact

Custom eBPF NAT makes HA harder:

- NAT state lives in BPF maps.
- Backup needs state sync or accepts flow reset.
- Program/map versioning matters during upgrades.
- Debugging failover is harder than with conntrack.

### Development cost

High.

This is a real dataplane project. It needs packet-level tests, network namespace tests, EC2 benchmark tests, verifier/load tests, and careful operational fallback.

### Verdict

Do not make this v0. Keep it as v2/v3 optimization after nftables baseline and observability are proven.

## Option 3: VPP

### What it is

FD.io VPP is a fast userspace L2-L4 network stack. VPP includes NAT44/NAT64 support, including NAT44 endpoint-dependent behavior.

A VPP-based BetterNAT would route traffic through a userspace packet-processing engine instead of the standard Linux forwarding/nftables path.

### Why it is attractive

VPP is designed for high-performance packet processing:

- Vectorized packet processing.
- Mature NAT44/NAT64 plugin.
- Appliance-like network stack.
- Good for specialized routers, gateways, load balancers, and NFV.

If the project becomes "maximum throughput per instance," VPP deserves serious evaluation.

### Why it is risky for the default product

It changes the operational model:

- More specialized network stack.
- Different CLI/config/runtime.
- Different debugging workflow.
- Possible DPDK/interface binding complexity depending on deployment.
- Less natural for ordinary AWS users than Linux routing/nftables.
- More complex AMI and support matrix.
- Harder rollback if users are unfamiliar with VPP.

On AWS, the biggest product risks are not only raw packet speed. They are:

- Correct route/EIP/ENI ownership.
- HA convergence.
- Split-brain prevention.
- Per-source attribution.
- Easy install.
- Safe rollback.

VPP does not remove those requirements.

### Observability

VPP has counters, tracing, and CLI tooling, but the product would need to translate that into the same Prometheus/Grafana/CLI UX. It would not automatically solve cost attribution.

### HA impact

VPP still needs AWS failover:

- Route replacement.
- EIP reassociation.
- ENI/IP move.
- lease/fencing.

It may also need VPP-specific state handling on failover.

### Development cost

Medium to high.

Less work than writing NAT from scratch, but more operational integration work than nftables.

### Verdict

Keep VPP as a separate research branch or future "performance edition." Do not use it for the first default path.

## Direct Comparison

| Dimension | nftables/nf_conntrack | eBPF observability | eBPF NAT fast path | VPP |
| --- | --- | --- | --- | --- |
| NAT correctness | Strong, mature | Not a NAT datapath | Must build/prove | Existing NAT plugins |
| First-version risk | Low | Moderate-low | High | Medium-high |
| Raw performance ceiling | Good, instance-bound | N/A | Potentially high | High |
| Debuggability | Excellent | Moderate | Hard | Specialized |
| AWS fit | Excellent | Excellent | Good but complex | Possible but heavier |
| HA integration | Straightforward | Observes HA | Complicates state | Needs custom integration |
| Per-source attribution | Limited alone | Strong | Strong if built | Needs integration |
| Install UX | Simple | Additive | More complex | More complex |
| Rollback | Easy | Easy | Needs fallback | Harder |
| Team skill required | Linux networking | eBPF basics | advanced eBPF/datapath | VPP/NFV |
| Recommended phase | fallback | optional later | deferred | later branch |

## Recommended Architecture

### v0: LoxiLB-first NAT appliance

```text
primary datapath: LoxiLB standalone egress SNAT
product fallback datapath: none
control: Go AWS failover agent
metrics: BetterNAT exporter from LoxiLB counters/conntrack, plus node/interface metrics
UX: Terraform + AMI/bootstrap + doctor + rollback
```

Goal:

- Prove cost reduction.
- Prove correctness.
- Prove deployment UX.
- Prove basic HA.
- Prove LoxiLB rule reconciliation and observability export.

### v0 fallback: Conservative Linux NAT

```text
datapath: nftables + nf_conntrack
use: LoxiLB unavailable, unsupported, or explicitly disabled
scope: simple SNAT/masquerade, conntrack metrics, doctor support
```

Goal:

- Keep BetterNAT usable when LoxiLB cannot run.
- Provide a simple debugging and rollback path.
- Avoid heavy product investment unless benchmarks force it.

### v1: Observability and benchmark hardening

```text
datapath: LoxiLB
observability: normalized Prometheus exporter, top-N, cost attribution
benchmarks: LoxiLB capacity profiles; legacy nftables diagnostics only
```

Goal:

- Attribute NAT cost by private source IP/subnet/team.
- Add top destinations.
- Add better drop/error visibility.
- Publish reproducible EC2 capacity profiles.

- Publish measured failover behavior.
- Avoid split-brain.
- Make rollback and maintenance boring.

### v3: Performance experiments

Two branches:

```text
Branch A:
  TC eBPF NAT fast path for measured bottlenecks

Branch B:
  VPP NAT44 appliance edition
```

Goal:

- Only optimize where nftables benchmarks prove insufficient.

## Product Messaging

### External headline

Do not put the implementation stack in the headline. The headline should describe the product value, not the packet-processing mechanism.

Good headline-style wording:

> BetterNAT is a low-cost, observable, highly available egress gateway for AWS private subnets.

Alternative:

> Cut high-volume NAT Gateway processing charges with a self-owned AWS egress gateway that shows who is using traffic and fails over automatically.

The technical stack belongs in an architecture section, not the first sentence.

### Internal architecture wording

Use this wording in design docs and implementation notes:

> BetterNAT uses LoxiLB as its supported standalone egress NAT datapath, keeps
> legacy Linux nftables/nf_conntrack code only while it is phased out, and puts
> HA, metrics, cost attribution, and Terraform UX in BetterNAT-owned
> control-plane components.

### Avoid

> BetterNAT is eBPF-powered NAT.

Unless forwarding is actually eBPF.

Also avoid leading with:

> Built on nftables/nf_conntrack.

That is accurate, but it is not a strong product headline for the target user.

### Better split

Use these labels:

- **Primary datapath**: LoxiLB standalone egress SNAT.
- **Product fallback datapath**: none.
- **Observability**: BetterNAT metrics exporter using LoxiLB counters/conntrack, with optional custom eBPF later.
- **Control plane**: Go AWS failover agent.
- **Deployment**: Terraform + AMI.

## Decision

Default to `nftables` + `nf_conntrack`.

Add eBPF first for observability, not packet rewriting.

Keep VPP and eBPF NAT as measured, optional future paths.

This gives the project a credible route to production without abandoning the long-term performance story.

## Sources

- nftables stateful NAT uses `nf_conntrack` and is the recommended common NAT approach: https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_%28NAT%29
- nftables is the modern Netfilter framework and replacement for iptables-family tools: https://www.netfilter.org/projects/nftables/index.html
- nft command and nf_tables kernel subsystem man page: https://www.netfilter.org/projects/nftables/manpage.html
- nftables connection tracking system overview: https://wiki.nftables.org/wiki-nftables/index.php/Connection_Tracking_System
- Linux kernel `nf_conntrack` sysctl documentation: https://docs.kernel.org/networking/nf_conntrack-sysctl.html
- eBPF verifier documentation: https://docs.kernel.org/bpf/verifier.html
- libbpf overview and CO-RE support: https://docs.kernel.org/bpf/libbpf/libbpf_overview.html
- BPF CO-RE concept: https://docs.ebpf.io/concepts/core/
- cilium/ebpf Go package: https://pkg.go.dev/github.com/cilium/ebpf
- eBPF SCHED_CLS/TC program type documentation: https://docs.ebpf.io/linux/program-type/BPF_PROG_TYPE_SCHED_CLS/
- eBPF helper reference, including checksum-related helpers: https://man7.org/linux/man-pages/man7/bpf-helpers.7.html
- FD.io VPP overview: https://fd.io/docs/vpp/master
- VPP NAT overview: https://wiki.fd.io/view/VPP/NAT
- VPP NAT44-ED documentation: https://s3-docs.fd.io/vpp/26.02/developer/plugins/nat44_ed_doc.html
