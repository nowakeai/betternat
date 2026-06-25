# Existing Building Blocks for BetterNAT

Date: 2026-06-19

## Question

Besides Cilium/Hubble, what existing wheels can BetterNAT reuse for datapath, observability, failover, and eBPF development?

## Short Answer

Current decision as of 2026-06-25:

The v0 architecture is now LoxiLB-first with no product fallback datapath.
Standalone LoxiLB passed AWS route-through egress NAT, DNS/UDP, high-response
download, and EIP + `ReplaceRoute` failover spikes. `nftables` +
`nf_conntrack` may remain only as legacy diagnostic code while it is phased out.

Superseded fallback note: BetterNAT no longer has a product fallback datapath.
The current sources of truth are `docs/architecture.md`, `docs/spec-v0.md`,
and `docs/research/055-no-nftables-fallback-decision.md`. Older fallback
language in this document is design history only.

There are useful existing components, but no obvious turnkey open-source "AWS NAT Gateway replacement with eBPF observability and EIP failover."

Previous practical stack before the LoxiLB spikes:

1. `nftables` + Linux `nf_conntrack` for NAT correctness.
2. `conntrack-tools` / `conntrackd` as an optional state-sync reference or advanced HA mode.
3. A custom Go AWS control-plane agent for EIP/route/ENI failover.
4. `cilium/ebpf` for custom eBPF observability programs.
5. `bpftrace`/BCC as debugging and research tools, not production dependencies.

High-performance alternatives like VPP are real, but they make the appliance a different product with a heavier operational model.

## Candidate Matrix

| Component | What It Gives Us | Fit | Recommendation |
| --- | --- | --- | --- |
| nftables + nf_conntrack | Kernel NAT, masquerade, stateful conntrack | Historical baseline | Legacy diagnostics only while retained |
| iptables + nf_conntrack | Older but widely known kernel NAT | Historical alternative | Do not add unless a new architecture decision supersedes LoxiLB |
| conntrack-tools / conntrackd | Conntrack inspection and HA state sync | Useful for HA experiments | Optional; do not require initially |
| Keepalived / VRRP | Classic virtual IP failover | Weak fit on AWS VPC | Borrow health-check ideas, not VRRP design |
| AWS EIP / route / ENI APIs | Cloud-native failover primitive | Required | Implement directly in Go agent |
| cilium/ebpf | Pure Go eBPF loader and map/program APIs | Excellent | Use for Go agent eBPF integration |
| libbpf / CO-RE | Mature C eBPF portability path | Excellent but C-heavy | Consider for low-level datapath |
| Aya | Rust-native eBPF framework | Good if Rust is chosen | Use only if project chooses Rust control plane |
| BCC | Rapid tracing/tooling | Good for dev/debug | Not for production appliance |
| bpftrace | High-level dynamic tracing | Excellent for support/debug | Ship scripts or docs, not daemon dependency |
| VPP / FD.io | Very fast userspace L2-L4 dataplane, NAT44/NAT64 | Powerful but heavy | Consider for separate "VPP edition" only |
| Katran | XDP L4 load-balancer datapath | Poor direct fit | Study patterns; do not embed |
| IPVS | Kernel L4 load balancing | Poor direct fit | Not a generic egress NAT solution |
| Open vSwitch | Programmable switch, conntrack/NAT actions | Too heavy for simple appliance | Avoid unless building SDN/overlay product |
| Suricata | IDS/NSM, XDP bypass | Different problem | Optional security add-on later |
| Pixie | eBPF Kubernetes observability | Kubernetes-only product shape | Same limitation as Hubble |

## Best Baseline: nftables + nf_conntrack

For BetterNAT, nftables is the best first datapath because it gives us boring correctness.

The nftables NAT documentation says stateful NAT uses the `nf_conntrack` kernel engine, and describes that as the common recommended approach. That is exactly what we want for MVP: let the kernel handle the tricky NAT state machine while we build product value around deployment, failover, metrics, and cost attribution.

Initial production path:

- Enable Linux forwarding.
- Disable EC2 source/destination check.
- Configure nftables postrouting masquerade or SNAT to the EIP/interface address.
- Configure forwarding policy and basic safety rules.
- Use `conntrack` for inspection and diagnostics.
- Add Prometheus metrics from `/proc`, netlink, conntrack, and later eBPF counters.

Why this is the right first wheel:

- NAT behavior is mature and widely deployed.
- Debugging is understandable.
- Kernel handles TCP/UDP/ICMP and related conntrack behavior.
- We can benchmark against it before writing any mutating eBPF datapath.
- It gives us a fallback path even after eBPF fast path exists.

Downside:

- Performance still depends on conntrack table sizing, CPU softirq, IRQ/RSS tuning, instance type, and packet size.
- It does not solve per-source attribution elegantly by itself.
- HA failover does not preserve all flows unless state is synchronized and routing/EIP failover is clean.

## conntrack-tools / conntrackd

`conntrack-tools` includes:

- `conntrack`: user-space CLI for inspecting/manipulating conntrack state.
- `conntrackd`: daemon for state-table synchronization in HA firewall setups.

This is highly relevant because one NAT failover problem is "what happens to active connections when backup takes over?"

Possible use:

- MVP: use `conntrack` only for `doctor` and debugging.
- Later HA mode: evaluate `conntrackd` state sync between active and standby nodes.

Reasons not to require it in v1:

- AWS EIP/route failover itself may break many active connections regardless of state sync.
- Conntrack state sync adds operational complexity.
- The product should first guarantee fast recovery for new connections and application-level retry, then separately explore active-flow preservation.

## Keepalived / VRRP

Keepalived is the classic Linux HA answer for VIP failover, but AWS VPC is not a normal L2 network where gratuitous ARP and VRRP solve everything.

For this project, failover should be cloud-native:

- Reassociate EIP, or
- Replace private subnet route target, or
- Move/attach a secondary ENI.

Keepalived can still be useful as a reference for:

- Health check configuration.
- State transitions.
- Active/backup mental model.

But BetterNAT should not center on VRRP.

## AWS APIs Are The Real HA Wheel

For AWS, the reusable primitive is not a Linux HA daemon; it is the EC2 control plane.

The custom agent should directly implement:

- EIP reassociation with `AssociateAddress` and reassociation enabled.
- Route-table failover with `ReplaceRoute`.
- Optional ENI attach/detach or secondary private IP reassignment.
- Lease/fencing through DynamoDB, SSM Parameter Store, or EC2 tags.

This is custom code, but not a huge amount of code. The hard part is correctness:

- Avoid split-brain.
- Scope IAM permissions tightly.
- Make takeover idempotent.
- Emit clear events with AWS request IDs.
- Back off correctly on API throttling or transient AWS failures.

## eBPF Development Wheels

### cilium/ebpf

This is the best match if the control plane is Go. It is a pure-Go library for loading, compiling, and debugging eBPF programs, with minimal dependencies and long-running-process use in mind.

Recommended use:

- Production eBPF loader.
- BPF map pinning and reading.
- TC/XDP program attach.
- Ring buffer or perf event reading.
- CO-RE style generated objects through `bpf2go`.

This does not mean using Cilium itself; it means using the lower-level Go eBPF library maintained by the Cilium/Cloudflare ecosystem.

### libbpf / CO-RE

This is the most standard low-level path for portable C eBPF programs. It is mature and close to kernel conventions.

Good for:

- Serious datapath work.
- Kernel-version portability.
- Smaller runtime dependencies.

Tradeoff:

- More C/toolchain complexity.
- Less pleasant if the rest of the appliance is Go.

### Aya

Aya is the Rust-native option. It is attractive if the project chooses Rust for both agent and datapath tooling.

Good for:

- Rust-only product direction.
- Stronger type system in user-space control code.

Tradeoff:

- Smaller ecosystem than Go + cilium/ebpf for operations examples.
- More friction if Terraform/Packer/agent ecosystem expects Go.

### BCC and bpftrace

These are excellent for research and support:

- Prototype tracepoints quickly.
- Debug skb path and conntrack behavior.
- Capture kernel counters during benchmarks.

They should not be production dependencies for the NAT appliance. They pull in extra runtime/toolchain assumptions and are better used as support scripts or documentation.

## VPP / FD.io

VPP is a serious high-performance userspace network stack. It has NAT44/NAT64 support, and current VPP docs describe it as a fast scalable L2-L4 network stack. Its NAT44-ED plugin implements endpoint-dependent NAT behavior.

This is the strongest non-kernel alternative if the product becomes a performance appliance.

Pros:

- Very high throughput potential.
- Existing NAT44/NAT64 implementation.
- Mature packet-processing architecture.
- Good fit for specialized network appliances.

Cons:

- More operationally invasive than Linux NAT.
- Different interface model and packet path.
- Harder for ordinary cloud teams to debug.
- May require DPDK/VPP-specific tuning.
- Less natural integration with Linux security groups, conntrack tools, and standard distro networking.

Recommendation:

- Do not use VPP for the main MVP.
- Keep it as a later "performance edition" research branch if nftables/eBPF cannot hit targets.

## Katran

Katran is a high-performance XDP L4 load balancer from Meta/Facebook. It is useful evidence that XDP can run very high-performance packet forwarding in production.

But it is not a generic egress NAT Gateway replacement:

- It targets L4 load balancing/VIP-to-real routing.
- The control-plane and topology assumptions differ.
- It does not solve AWS EIP/route failover for private subnet egress.
- Adapting it may be harder than writing a smaller TC/eBPF accounting layer.

Recommendation:

- Study Katran's map layout, XDP patterns, and testing style.
- Do not embed or fork it for MVP.

## IPVS

IPVS is Linux's in-kernel L4 load balancer. It is mature and useful for service load balancing.

It is not the right core for this product:

- It routes traffic to backend real servers.
- BetterNAT needs arbitrary private-source egress NAT to the internet.
- IPVS NAT mode is about load-balancer topology, not replacing AWS NAT Gateway.

Recommendation:

- Ignore for MVP except as background reading.

## Open vSwitch

Open vSwitch has conntrack integration and NAT actions. It is powerful for programmable virtual switching and SDN-style networking.

But for a simple AWS egress appliance it is probably too much machinery:

- Requires OVS bridge/datapath setup.
- Adds OpenFlow/control-plane concepts.
- Debugging becomes OVS-specific.
- Still relies on Linux conntrack for many stateful NAT behaviors.

Recommendation:

- Avoid unless the product shifts toward multi-tenant SDN/overlay routing.

## Observability Wheels

### Prometheus

Prometheus should be the default metrics surface:

- NAT byte/packet counters.
- Per-source top-N from eBPF maps.
- Conntrack table usage.
- Drop counters.
- AWS failover state.
- Agent health.

### Grafana

Ship dashboards as JSON. Do not build a custom UI first.

### Flow logs

VPC Flow Logs already exist, but they are delayed and not appliance-local. They are useful for validation, not for real-time NAT attribution.

### Hubble/Pixie/Suricata

These can be optional integrations, not core dependencies:

- Hubble: useful for Cilium/Kubernetes users.
- Pixie: Kubernetes-native eBPF app observability.
- Suricata: IDS/NSM/security add-on, not NAT accounting.

## Recommended Reuse Plan

### Version 0

Use existing Linux networking only:

- nftables.
- nf_conntrack.
- conntrack CLI.
- Prometheus node/exporter style metrics.
- Custom Go AWS failover agent.

### Version 1

Add low-risk eBPF:

- cilium/ebpf loader.
- TC classifier for flow counters.
- Ring buffer for sampled flow events.
- bpftrace/BCC support scripts for debug.

Forwarding remains nftables.

### Version 2

Add optional fast path:

- TC eBPF SNAT/DNAT for selected flows.
- Keep legacy nftables diagnostics stable only while the code remains.
- Benchmark before enabling by default.

### Version 3 / Alternate Edition

Research:

- VPP NAT44 appliance mode.
- conntrackd active-backup state sync.
- Cilium/Hubble integration for Kubernetes environments.

## Decision

The main existing wheel to use is not Cilium. It is the Linux NAT stack:

> nftables + nf_conntrack for correctness, cilium/ebpf for custom observability, and a custom Go agent for AWS failover.

That gives the project a credible product path without taking on full NAT datapath implementation risk on day one.

## Sources

- nftables NAT documentation, including stateful NAT through nf_conntrack: https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_%28NAT%29
- nft command/netfilter man page: https://www.netfilter.org/projects/nftables/manpage.html
- conntrack-tools overview: https://conntrack-tools.netfilter.org/
- conntrack-tools manual, including HA state-table synchronization: https://conntrack-tools.netfilter.org/manual.html
- conntrackd manual page summary: https://man.archlinux.org/man/conntrackd.8.en
- VPP NAT overview: https://wiki.fd.io/view/VPP/NAT
- VPP NAT44-ED docs: https://s3-docs.fd.io/vpp/26.02/developer/plugins/nat44_ed_doc.html
- VPP overview: https://fd.io/docs/vpp/master
- Katran repository: https://github.com/facebookincubator/katran
- Meta engineering post on Katran: https://engineering.fb.com/2018/05/22/open-source/open-sourcing-katran-a-scalable-network-load-balancer/
- IPVS overview: https://kb.linuxvirtualserver.org/wiki/IPVS
- Linux IPVS sysctl docs: https://docs.kernel.org/networking/ipvs-sysctl.html
- Open vSwitch conntrack tutorial: https://docs.openvswitch.org/en/latest/tutorials/ovs-conntrack/
- Open vSwitch actions reference: https://docs.openvswitch.org/en/latest/ref/ovs-actions.7/
- cilium/ebpf Go package docs: https://pkg.go.dev/github.com/cilium/ebpf
- ebpf-go docs: https://ebpf-go.dev/
- Aya book: https://aya-rs.dev/book/
- BCC reference guide: https://github.com/iovisor/bcc/blob/master/docs/reference_guide.md
- bpftrace docs: https://bpftrace.org/docs/0.22
- Suricata eBPF/XDP docs: https://docs.suricata.io/en/latest/capture-hardware/ebpf-xdp.html
- Pixie overview: https://px.dev/
