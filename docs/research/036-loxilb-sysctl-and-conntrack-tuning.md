# LoxiLB, Sysctl, And Conntrack Tuning

Date: 2026-06-21

## Question

BetterNAT's primary datapath is LoxiLB/eBPF. Do Linux `nf_conntrack` tuning parameters such as `net.netfilter.nf_conntrack_max` still matter?

## Short Answer

They are not primary LoxiLB performance knobs.

BetterNAT should keep a small, conservative gateway sysctl profile, but should not claim that Linux `nf_conntrack_max` increases LoxiLB/eBPF NAT capacity.

Recommended alpha behavior:

- always enable IPv4 forwarding,
- always disable reverse path filtering for forwarding safety, including already-existing interfaces,
- set Linux `nf_conntrack_max` only if the kernel exposes that sysctl,
- document `nf_conntrack_max` as legacy/compatibility tuning, not LoxiLB tuning,
- defer advanced tuning profiles until benchmark evidence exists.

## Why

### LoxiLB has its own conntrack state

The BetterNAT LoxiLB engine reads conntrack through:

```text
loxicmd get conntrack -o json
```

This is parsed from LoxiLB's `ctAttr` output, not from Linux `conntrack -L`.

In the LoxiLB reference code:

- `loxilb-ebpf/kernel/llb_kern_ct.c` is the LoxiLB kernel eBPF conntrack implementation,
- `loxilb-ebpf/kernel/llb_kern_cdefs.h` defines `ct_map` as a `BPF_MAP_TYPE_HASH`,
- `loxilb-ebpf/common/llb_dpapi.h` defines `LLB_CT_MAP_ENTRIES` as `256*1024*LLB_MAX_LB_NODES`,
- `LLB_MAX_LB_NODES` is currently `2`,
- `loxilb_libdp.c` ages entries in `LL_DP_CT_MAP`.

Therefore, Linux `net.netfilter.nf_conntrack_max` is not the limit for LoxiLB's eBPF conntrack map.

### Linux nf_conntrack still matters for legacy diagnostics and host behavior

BetterNAT still has legacy nftables diagnostic code while it is being phased
out. That path uses Linux NAT and Linux conntrack:

```text
nft ... masquerade
conntrack -L
```

For that legacy diagnostic path, `nf_conntrack_max` is relevant.

It may also matter for unrelated host networking features that use kernel netfilter conntrack, such as Docker or security-group-observed host flows, but it should not be described as the LoxiLB NAT capacity control.

## fck-nat Reference

fck-nat exposes opt-in tuning knobs:

- `ip_local_port_range`,
- `nf_conntrack_max`,
- `nf_conntrack_buckets`,
- `nf_conntrack_tcp_timeout_established`,
- `tcp_keepalive_time`,
- `tcp_max_syn_backlog`.

That makes sense for fck-nat because its core datapath is iptables/kernel NAT, so Linux conntrack is the main state table.

BetterNAT should borrow the documentation discipline, not blindly copy the defaults:

- explain what each knob controls,
- tie knobs to the actual datapath,
- avoid setting advanced values without workload evidence,
- make risky memory/timeout changes explicit.

## LoxiLB Reference

LoxiLB's relevant tuning surface is different:

- eBPF map capacity and memory,
- LoxiLB conntrack aging behavior,
- LoxiLB firewall/NAT rule counts,
- LoxiLB readiness and `loxicmd`/API observability,
- host kernel/eBPF compatibility,
- instance memory and CPU.

Current BetterNAT does not expose LoxiLB eBPF map sizing as a supported user knob. If we need to tune that later, it should be handled as a LoxiLB version/build/runtime compatibility question, not as a Linux `sysctl` setting.

## Alpha Bootstrap Decision

The alpha cloud-init profile should write:

```text
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
```

After applying sysctls, bootstrap should also sweep existing interfaces:

```sh
for rp_filter in /proc/sys/net/ipv4/conf/*/rp_filter; do
  [ -e "$rp_filter" ] && echo 0 > "$rp_filter"
done
```

This mirrors the fck-nat appliance pattern of disabling `rp_filter` across interfaces and is consistent with LoxiLB's own initialization behavior for `llb0`.

And should append this only if `/proc/sys/net/netfilter/nf_conntrack_max` exists:

```text
net.netfilter.nf_conntrack_max = 1048576
```

This gives the legacy nftables diagnostic path a better baseline without making
bootstrap depend on a non-primary sysctl being present.

## Do Not Default Yet

Do not default these in the first alpha:

- `nf_conntrack_buckets` / `/sys/module/nf_conntrack/parameters/hashsize`,
- `nf_conntrack_tcp_timeout_established`,
- `ip_local_port_range`,
- `tcp_keepalive_time`,
- `tcp_max_syn_backlog`,
- IRQ/RSS/queue tuning,
- ENA-specific tuning.

Reasons:

- most are not LoxiLB main-path controls,
- some affect host TCP behavior rather than forwarded egress NAT,
- timeout changes can break idle long-lived flows,
- bucket sizing depends on module load order and memory sizing,
- network queue tuning is instance-family-specific,
- we do not have benchmark evidence yet.

## Product Guidance

User-facing docs should say:

- BetterNAT uses LoxiLB/eBPF as the primary datapath.
- BetterNAT also keeps nftables/nf_conntrack as a fallback.
- The alpha bootstrap applies minimal gateway sysctls.
- `nf_conntrack_max` is a fallback/compatibility setting when available.
- LoxiLB conntrack should be inspected through BetterNAT metrics or `loxicmd get conntrack -o json`.
- High-volume tuning will be benchmark-backed in a later release.
