# LoxiLB Evaluation

Date: 2026-06-19

Current note as of 2026-06-25: fallback recommendations in this evaluation are
superseded. BetterNAT now has no product fallback datapath; see
`docs/research/055-no-nftables-fallback-decision.md`.

## Question

Could LoxiLB be used as the core datapath or HA foundation for BetterNAT?

## Short Answer

LoxiLB should be the primary BetterNAT v0 datapath target.

The AWS spikes validated the exact route-through NAT Gateway replacement use case: standalone LoxiLB can SNAT private-subnet traffic, preserve a stable EIP for new connections after EIP + `ReplaceRoute` failover, handle TCP/HTTPS/DNS/UDP, and expose useful firewall/conntrack state for BetterNAT observability.

nftables/nf_conntrack should remain only as legacy diagnostic code while it is
phased out, not as a product fallback.

It is stronger than the earlier "study only" candidates because it already has:

- Go/eBPF datapath,
- multiple NAT modes,
- standalone mode,
- Kubernetes service load-balancer integration,
- Kubernetes egress support,
- HA modes,
- AWS multi-AZ HA documentation using EIP/secondary interface,
- multi-cloud HA documentation,
- visibility/statistics and Grafana-related work.

However, LoxiLB is still only one layer. BetterNAT's primary v0 target is a complete AWS private subnet NAT Gateway replacement experience for VM and mixed VM/EKS traffic, with Terraform-provider UX, cost attribution, route/EIP failover, and FinOps-driven observability.

Recommendation:

1. Treat LoxiLB as the default datapath target for v0 implementation.
2. Do not add nftables as a required or supported product fallback.
3. Keep the product-level Terraform provider, cost calculator, security model, and observability UX as BetterNAT-owned.
4. Do not assume LoxiLB solves generic VPC route failover, FinOps attribution, Terraform provider UX, or AWS least-privilege/security requirements out of the box.

Follow-up spike results:

- `021-loxilb-spike-results.md` validated standalone route-through egress SNAT in AWS. A private client in `10.77.2.0/24` successfully exited through a LoxiLB appliance EIP after configuring `loxicmd create firewall --snat ... --egress`.
- `022-loxilb-extended-spike-results.md` validated DNS/UDP, concurrent short flows, high response-volume downloads, and basic EIP + `ReplaceRoute` failover to a backup LoxiLB appliance.

## What LoxiLB Is

LoxiLB describes itself as an open-source cloud-native load balancer based on Go/eBPF, intended to work across on-prem, public-cloud, hybrid Kubernetes, telco, edge, and IoT environments.

Its README says its main use case is Kubernetes Service type LoadBalancer, but it can run in-cluster or external to the cluster. It also lists kube-proxy replacement, ingress support, Gateway API, HA-capable Kubernetes egress, and network policies.

This makes it relevant to BetterNAT, but also means its center of gravity is not exactly "replace AWS NAT Gateway for arbitrary private subnet egress."

## Relevant LoxiLB Capabilities

### NAT modes

LoxiLB documents several NAT modes:

- Normal NAT,
- One-ARM,
- Full-NAT,
- L2-DSR,
- L3-DSR.

The NAT docs say normal NAT uses DNAT on incoming requests and SNAT on outgoing responses, and requires return packets to traverse LoxiLB because it relies on statefulness.

For BetterNAT, the important question is different:

```text
private source -> internet destination
return internet traffic -> original private source
```

LoxiLB's NAT model clearly supports stateful NAT for load-balancing scenarios. We still need to validate whether it can act cleanly as a generic many-source outbound SNAT gateway for arbitrary VPC private subnets.

### Kubernetes egress

LoxiLB has Kubernetes egress docs. It describes Kubernetes egress as outbound pod traffic and says LoxiLB provides HA-enabled management of outgoing pod traffic as a forward proxy. The docs emphasize stable IP representation, controlled outbound IP ranges, traffic insights, and HA.

This overlaps strongly with our EKS/pod-attribution and stable egress IP goals.

But the documented egress flow appears Kubernetes-focused and uses LoxiLB-specific egress CRDs and, in the shown deployment, secondary interfaces / Multus style configuration. It may be an excellent EKS egress solution, but not necessarily a generic VPC NAT Gateway replacement for VMs.

### Standalone mode

LoxiLB supports standalone mode decoupled from Kubernetes. Docs show Docker and systemd/deb package installation. This matters because BetterNAT cannot depend on Kubernetes for generic VM workloads.

Validated:

- Standalone LoxiLB can be configured as a simple outbound SNAT gateway.
- It can SNAT a private VPC CIDR source without Kubernetes service objects.
- It can run as a route-through AWS appliance with source/destination check disabled.
- It can support basic active/standby failover when BetterNAT owns EIP reassociation and `ReplaceRoute`.
- It exposes useful firewall counters and conntrack state for source/destination attribution.

Open questions:

- Can it persist exact rules across restarts and upgrades? The tested container mode did not; BetterNAT should reconcile rules itself.
- Can it expose or support the per-source metrics and top-N data we need without a custom re-exporter? The tested API port did not expose `/metrics`.
- What is the production-safe install path: container, package, or BetterNAT-managed service?

### AWS multi-AZ HA

LoxiLB has an AWS multi-AZ HA guide. It describes two LoxiLB instances in different AZs operating active/backup. The active instance gets a secondary network interface with a private IP, and an EIP is associated to that private IP. During failover, the new active gets a secondary interface and the private IP/EIP association moves to the new active instance.

This is very close to the advanced dynamic ENI/private-IP mode we previously deferred.

Important differences:

- The LoxiLB AWS guide is oriented around exposing EKS services through a stable EIP.
- It uses kube-loxilb and service objects.
- The sample IAM policy in the guide is `"Action": "*", "Resource": "*"`, which is not acceptable for BetterNAT production defaults.
- The guide claims active sessions can be maintained, but we need to benchmark this for our traffic pattern and failure modes before adopting a product claim.

### Multi-cloud HA

LoxiLB docs also describe multi-cloud/multi-region HA. They note limitations: EIP is region-bound and cross-region connection synchronization is not possible; warm standby is used cross-region, and GCP EIP support is described as not fully available in that guide.

This is directionally aligned with our provider abstraction, but it reinforces that "multi-cloud HA" is provider-specific and not a trivial generic primitive.

### Performance

LoxiLB publishes a performance report page with links to single-node and bare-metal reports. It also has release-note references to conntrack scaling, XDP/RSS improvements, and eBPF conntrack support.

We should not reuse published performance claims for our product without reproducing tests in our target AWS NAT Gateway replacement topology.

## Potential Integration Models

## Model A: Keep current design; treat LoxiLB as reference

Current status: rejected after the AWS LoxiLB spikes. This model remains here only as historical context.

```text
BetterNAT datapath: nftables/nf_conntrack
BetterNAT HA: our agent
LoxiLB: reference for eBPF NAT/HA ideas
```

Pros:

- Lowest product risk.
- Keeps v0 simple.
- Keeps full control over AWS-specific security, Terraform provider, observability, and cost UX.

Cons:

- May duplicate mature LoxiLB eBPF/NAT work.
- Leaves performance improvements for later.

Use if LoxiLB does not cleanly support generic VPC outbound SNAT gateway mode.

## Model B: LoxiLB as optional datapath engine

```text
BetterNAT provider/agent owns:
  Terraform UX
  AWS resource lifecycle
  lease/fencing
  route/EIP failover
  cost/observability UX

LoxiLB owns:
  packet datapath
  NAT/conntrack implementation
```

Pros:

- Reuses existing eBPF NAT datapath.
- Keeps BetterNAT product differentiation.
- Avoids adding a second supported datapath contract.

Cons:

- Need to learn and control LoxiLB API/config.
- Need to package/secure/upgrade LoxiLB.
- Need to bridge metrics into our observability model.
- Need to ensure LoxiLB does not fight our agent's HA model.

This may be the best long-term model if a spike validates generic outbound SNAT.

## Model C: Build BetterNAT around LoxiLB

```text
BetterNAT = Terraform provider + FinOps/UX wrapper around LoxiLB
```

Pros:

- Fastest path to eBPF datapath if fit is good.
- Strong technical story.
- May inherit Kubernetes egress and HA features.

Cons:

- Product becomes coupled to LoxiLB's roadmap and operational model.
- Harder to maintain our own security posture if upstream defaults/examples are broad.
- Risk of mismatch with VM-heavy generic VPC NAT use case.
- Debugging and support surface becomes LoxiLB-specific.

Do not choose this without a hands-on spike.

## Model D: Fork LoxiLB

Not recommended initially.

Forking gives control but creates a maintenance burden. If changes are needed, upstream contributions or a wrapper integration are preferable.

## Fit Against BetterNAT Product Pillars

| Pillar | LoxiLB Fit | Notes |
| --- | --- | --- |
| Low-cost self-hosted NAT | Potentially strong | Need generic VPC outbound SNAT validation |
| Better observability | Partial | LoxiLB has visibility/statistics, but our FinOps/top-N UX still likely custom |
| Low-cost HA | Strong candidate | AWS multi-AZ HA and egress HA docs are very relevant |
| Terraform-native UX | Not solved | We still need `terraform-provider-betternat` |
| Stable egress IP | Strong candidate | EIP/private-IP reassociation model overlaps our advanced HA mode |
| Mixed VM/EKS VPC | Unknown | Kubernetes support is strong; VM/private subnet gateway mode needs testing |
| Security model | Needs wrapping | Upstream examples are not least-privilege production posture |

## Key Questions To Validate

### Datapath

- Can standalone LoxiLB act as generic outbound SNAT gateway for a VPC private route table?
- Can it SNAT arbitrary private CIDRs to an EIP/interface address?
- How does it allocate ports and handle port exhaustion?
- Does it support many source IPs and high concurrent connections for internet egress?
- How are timeouts, ICMP, fragments, UDP flows handled?
- Can it coexist with AWS source/destination check disabled and VPC routing?

### HA

- Does LoxiLB HA need kube-loxilb, or can standalone HA run without Kubernetes?
- What is the exact lease/election mechanism?
- Is there split-brain fencing comparable to our DynamoDB lease model?
- Can we use LoxiLB only for datapath while our agent owns route/EIP failover?
- Are active sessions preserved for outbound egress, or only for its documented service LB scenario?

### AWS integration

- Which AWS API calls does LoxiLB make?
- Can IAM be least-privilege scoped?
- Does it require creating subnets/ENIs dynamically?
- Can those operations be moved under our Terraform provider instead?
- Does it support route-table failover, or only EIP/ENI private-IP movement?

### Observability

- What metrics does LoxiLB expose?
- Can we get per-source private IP, destination, protocol, byte counters?
- Can we map LoxiLB metrics to our Prometheus cardinality policy?
- Can we integrate cost attribution without high-cardinality explosion?

### Operations

- Container vs systemd package?
- Kernel/OS support matrix?
- Upgrade behavior?
- Config persistence and reconciliation?
- Debugging commands/API stability?
- CNCF Sandbox maturity and release cadence.

## Impact On Earlier Decisions

### Datapath decision

Earlier decision:

> v0 uses nftables/nf_conntrack; eBPF NAT fast path later.

Revised decision:

> Use LoxiLB as the primary v0 datapath target with no product fallback datapath. The standalone AWS spikes validated generic outbound SNAT, DNS/UDP, larger response downloads, and basic EIP + `ReplaceRoute` failover for new connections.

### HA decision

Earlier decision:

> route failover first, dynamic ENI/private-IP later.

Revised decision:

> LoxiLB's AWS multi-AZ HA makes dynamic ENI/private-IP movement more credible. Still, it remains more complex than route failover for generic private-subnet NAT. We should benchmark both route failover and LoxiLB-style EIP/private-IP movement if adopting LoxiLB.

### Observability decision

Earlier decision:

> v0 no eBPF, v1 custom eBPF flow accounting.

Revised decision:

> If LoxiLB provides usable per-source/per-destination counters, v1 eBPF accounting may be unnecessary or should be implemented by consuming LoxiLB stats instead.

### MVP scope

Earlier decision:

> no eBPF NAT in v0.

Revised decision:

> no self-built eBPF NAT in v0. Existing LoxiLB eBPF datapath may be allowed after spike validation.

## Recommended Spike

Create a short milestone before M0/M1:

## M-1: LoxiLB NAT Gateway Spike

Goal:

Determine whether LoxiLB can be the packet datapath for BetterNAT.

Test topology:

```text
AWS VPC
public subnet:
  LoxiLB instance with EIP

private subnet:
  test VM
  route table 0.0.0.0/0 -> LoxiLB instance/ENI

internet:
  public echo endpoint
```

Tests:

1. Private VM reaches internet through LoxiLB.
2. Public echo endpoint sees expected EIP.
3. Return traffic works for TCP/UDP.
4. Multiple private source IPs work.
5. Concurrent connection and port exhaustion behavior observed.
6. LoxiLB metrics expose useful source/destination/byte counters.
7. LoxiLB can run without Kubernetes for this scenario.
8. Failover can be controlled by our agent or cleanly delegated to LoxiLB.
9. IAM permissions can be narrowed to acceptable scope.

Acceptance:

- If LoxiLB passes generic outbound SNAT and metrics tests, make it a supported datapath engine candidate.
- If it only fits Kubernetes egress/service LB, keep it as an optional EKS integration or reference.

## Proposed Architecture If LoxiLB Passes

```text
terraform-provider-betternat:
  creates AWS resources
  installs/configures LoxiLB
  installs betternat-agent

betternat-agent:
  owns HA lease/fencing
  owns cloud route/EIP verification
  queries/controls LoxiLB API
  exports normalized BetterNAT metrics

LoxiLB:
  owns eBPF NAT datapath
  owns datapath conntrack/maps
```

Do not add nftables fallback UX:

```hcl
datapath_engine = "loxilb"
```

## Decision

LoxiLB is important enough to add a spike and may change the datapath decision.

But the BetterNAT product should remain defined by:

- Terraform-native gateway UX,
- FinOps/cost attribution,
- cloud-specific HA safety,
- stable egress identity,
- production security posture,
- mixed VM/EKS private subnet support.

LoxiLB may become the datapath engine. It should not automatically become the whole product.

## Sources

- LoxiLB GitHub README, describing Go/eBPF cloud-native load balancer and features including HA-capable Kubernetes egress: https://github.com/loxilb-io/loxilb
- CNCF project page, LoxiLB accepted as CNCF Sandbox on August 30, 2024: https://www.cncf.io/projects/loxilb/
- LoxiLB NAT modes documentation: https://docs.loxilb.io/nat/
- LoxiLB Kubernetes egress documentation: https://docs.loxilb.io/main/loxilb-egress/
- LoxiLB standalone mode documentation: https://docs.loxilb.io/main/standalone/
- LoxiLB high availability documentation: https://docs.loxilb.io/main/ha-deploy/
- LoxiLB AWS multi-AZ HA guide: https://docs.loxilb.io/main/aws-multi-az/
- LoxiLB multi-cloud HA guide: https://docs.loxilb.io/main/multi-cloud-ha/
- LoxiLB roadmap/release notes, including FullNAT, NAT64/NAT66, eBPF conntrack, AWS/EKS support, policy-based IP masquerade/SNAT, multi-AZ/region HA: https://github.com/loxilb-io/loxilbdocs/blob/main/docs/roadmap.md
