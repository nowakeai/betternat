# nftables Performance for BetterNAT

Date: 2026-06-19

## Question

Is nftables fast enough to use as the first datapath for BetterNAT?

## Short Answer

Current decision as of 2026-06-25:

nftables was fast enough for early datapath experiments, but it is not the
BetterNAT product datapath and there is no product fallback datapath. LoxiLB is
the supported datapath after the AWS standalone egress and failover spikes. Keep
this document as historical performance context for legacy diagnostics only.

Superseded fallback note: BetterNAT no longer has a product fallback datapath.
This document is retained as historical datapath research and legacy diagnostic
context only. The current sources of truth are `docs/architecture.md`,
`docs/spec-v0.md`, and `docs/research/055-no-nftables-fallback-decision.md`.

Historical answer from the original 2026-06-19 research follows. It is not the
current product direction:

- It is the right correctness baseline.
- It can likely handle meaningful NAT Gateway replacement workloads on properly sized EC2 instances.
- Its real bottleneck is usually not the nftables rule engine itself, but connection tracking, packet rate, instance/network limits, IRQ/RSS tuning, and rule design.
- It should not be marketed as "eBPF-level" or "AWS NAT Gateway-level" until measured.

Superseded conclusion: the product should not start with nftables, should not
add nftables fallback UX, and should not use nftables validation as a release
substitute. BetterNAT now validates LoxiLB directly.

## What nftables Actually Is

nftables is the modern Netfilter packet filtering and NAT framework. Netfilter describes nftables as a replacement for iptables, ip6tables, arptables, and ebtables. It reuses existing Netfilter subsystems including hooks, connection tracking, NAT, queueing, and logging.

For NAT, the key point is that stateful NAT uses the kernel connection tracking engine, `nf_conntrack`. So BetterNAT's baseline datapath would be:

```text
packet -> Linux forwarding path -> nf_conntrack -> nftables NAT rule -> ENA/Nitro
```

This is a mature Linux kernel path, not an experimental one.

## Expected Performance Character

### Good

nftables should perform well when:

- The rule set is small and uses maps/sets instead of long linear chains.
- NAT is simple: private CIDR to single public source address/interface.
- Conntrack table is sized correctly.
- Instance type has enough network bandwidth and packets-per-second headroom.
- IRQ/RSS/RPS/XPS are tuned or at least not pathological.
- Flow cardinality is moderate relative to memory and conntrack hash buckets.

### Weak

nftables/conntrack will struggle when:

- There are many new connections per second.
- There are many tiny packets, making pps the bottleneck before Gbps.
- Conntrack table approaches capacity.
- The conntrack hash table is undersized, creating long bucket chains.
- TCP timeouts keep dead flows around too long.
- Rules are written as long sequential chains instead of sets/maps.
- One CPU queue becomes hot.

## The Main Bottleneck: nf_conntrack

For NAT, conntrack is not optional in the normal stateful Linux NAT model. It stores flow state so return packets can be translated back to the original private source.

The Linux kernel documentation exposes several important parameters:

- `nf_conntrack_count`: current number of allocated flow entries.
- `nf_conntrack_max`: maximum tracked entries.
- `nf_conntrack_buckets`: hash table size.
- protocol-specific timeout settings.
- optional per-flow accounting.

If the conntrack table is full, new flows can be dropped. That makes conntrack capacity and hash sizing a first-class product metric, not just a tuning detail.

## Performance Positioning

### Against AWS NAT Gateway

AWS NAT Gateway is managed, horizontally engineered, and not directly comparable to a single EC2 appliance. A single nftables NAT instance can be much cheaper, but capacity is bounded by:

- EC2 instance network bandwidth.
- Packets per second.
- ENA behavior.
- CPU softirq capacity.
- Conntrack table memory.
- Single-AZ failover design.

So the claim should be:

> Lower-cost self-hosted NAT for high-volume, cost-sensitive workloads that can accept appliance ownership.

Not:

> Same performance and availability as AWS NAT Gateway.

### Against iptables

nftables is the better baseline for new work:

- Modern replacement for legacy xtables.
- Better rule management.
- Native sets/maps.
- Atomic ruleset updates.
- Less duplication across IPv4/IPv6/ARP/bridge families.

But for a very small NAT ruleset, nftables vs iptables may not be the dominant factor. Conntrack and EC2 limits usually matter more.

### Against eBPF

A custom eBPF datapath can theoretically reduce overhead, especially for targeted fast paths. But writing correct stateful NAT is much harder than using nftables.

The right comparison is:

- nftables: mature, debuggable, correct, good enough until proven otherwise.
- eBPF accounting: low-risk, useful, can be added early.
- eBPF NAT fast path: high-risk optimization, justified only by benchmark data.

### Against VPP

VPP may outperform Linux NAT for specialized high-throughput appliances, but it is heavier operationally and changes the product shape. It should remain a later research branch, not the default.

## Rule Design Guidance

Keep the MVP ruleset intentionally simple:

```nft
table inet betternat {
  chain forward {
    type filter hook forward priority filter; policy drop;

    ct state established,related accept
    iifname "private0" oifname "public0" ip saddr 10.0.0.0/8 accept
  }

  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;

    oifname "public0" ip saddr 10.0.0.0/8 masquerade
  }
}
```

Production should likely generate rules from config, but the model should stay compact:

- One or a few private CIDRs.
- One public interface or EIP source address.
- Sets/maps for multiple CIDRs.
- No per-host linear rule chain.
- No logging on hot path by default.
- No expensive string matching or deep inspection.

## Tuning Checklist

The product should own these defaults:

- `net.ipv4.ip_forward=1`
- EC2 source/destination check disabled.
- `nf_conntrack_max` sized for expected concurrent flows.
- `nf_conntrack_buckets` / module hashsize sized with memory.
- TCP established timeout reviewed for NAT appliance behavior.
- UDP timeout reviewed for DNS/short-lived traffic.
- `nf_conntrack_count / nf_conntrack_max` exported as a metric.
- conntrack insert/drop/error counters exported.
- ENA driver and IRQ queue distribution verified.
- `somaxconn`, ephemeral port ranges, and local port exhaustion monitored where relevant.

Do not blindly set huge conntrack limits. The table consumes kernel memory and can make failure modes worse if memory pressure is ignored.

## What We Need To Benchmark

The first benchmark suite should answer:

1. Maximum Gbps for large packets.
2. Maximum pps for small packets.
3. New connections per second.
4. Concurrent flow capacity before degradation.
5. CPU usage by softirq and ksoftirqd.
6. Latency overhead p50/p95/p99.
7. Conntrack table occupancy and insert failures.
8. Behavior when conntrack is full.
9. Failover recovery time for new connections.
10. Flow survival behavior during EIP/route/ENI failover.

Minimum benchmark matrix:

- Instance families: small baseline, compute optimized, network optimized, Graviton.
- Packet sizes: 64B, 512B, 1500B, and realistic mixed traffic.
- Flow types: long-lived TCP, short-lived TCP, UDP, DNS-like UDP.
- Rulesets: single CIDR, multiple CIDRs through sets, intentionally bad linear chain.

## Superseded Product Implication

The original 2026-06-19 implication below is superseded and retained only to
explain the design history:

- It lets us ship a correct NAT appliance sooner.
- It was originally considered as a conservative Linux fallback if a custom
  eBPF fast path failed to load.
- It gives us benchmark data before we invest in custom NAT datapath work.
- It keeps the early product focused on the real differentiators: AWS failover, operational packaging, cost attribution, and observability.

The original later eBPF story was:

1. Use eBPF first for attribution and visibility.
2. Keep the Linux NAT path for forwarding.
3. Add eBPF fast path only for measured bottlenecks.

Current implication: LoxiLB is the supported BetterNAT datapath. nftables may
remain only as legacy diagnostic code while it is phased out.

## Decision

Do not use nftables as a BetterNAT product fallback datapath.

Do not claim fixed throughput numbers before testing on real EC2 instance
types. The credible claim is that nftables is mature, production-proven Linux
NAT technology, but BetterNAT supportability depends on LoxiLB passing the
relevant cloud, kernel, packaging, and release gates.

## Sources

- Netfilter nftables project page, describing nftables as a replacement for iptables and noting reuse of Netfilter hooks, conntrack, NAT, queueing, and logging: https://www.netfilter.org/projects/nftables/index.html
- nftables wiki, "What is nftables?": https://wiki.nftables.org/wiki-nftables/index.php/What_is_nftables%3F
- nftables NAT documentation, including stateful NAT and connection tracking: https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_%28NAT%29
- nftables sets documentation, for avoiding long linear rule chains: https://wiki.nftables.org/wiki-nftables/index.php/Sets
- nftables maps documentation: https://wiki.nftables.org/wiki-nftables/index.php/Maps
- nftables netfilter hooks and priority documentation: https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks
- Linux kernel nf_conntrack sysctl documentation: https://docs.kernel.org/networking/nf_conntrack-sysctl.html
- conntrack-tools manual: https://conntrack-tools.netfilter.org/manual.html
- Red Hat nftables overview describing nftables as a modern, efficient alternative to iptables: https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/8/html/configuring_and_managing_networking/getting-started-with-nftables_configuring-and-managing-networking
