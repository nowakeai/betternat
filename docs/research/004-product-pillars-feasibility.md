# Product Pillars Feasibility: Cost, Observability, HA, and Install UX

Date: 2026-06-19

## Question

BetterNAT's core product value is:

1. Low-overhead self-hosted NAT Gateway replacement.
2. Better observability.
3. Low-cost but stable, reliable, fast high availability.
4. One-command install/configuration and Terraform support.

Are these pillars feasible, and what should each pillar actually mean in the product?

## Executive Summary

Current decision as of 2026-06-25:

The product pillars remain valid, but the default datapath changed. BetterNAT
should now be LoxiLB-first with no product fallback datapath. The product value
still lives above the datapath: Terraform UX, AWS-safe HA,
source/destination attribution, cost visibility, and rollback.

Superseded fallback note: BetterNAT no longer has a product fallback datapath.
The current sources of truth are `docs/architecture.md`, `docs/spec-v0.md`,
and `docs/research/055-no-nftables-fallback-decision.md`. Older fallback
language in this document is design history only.

The product is feasible if we are disciplined about scope:

- Use LoxiLB standalone egress SNAT as the supported NAT datapath.
- Make the first cost win come from removing NAT Gateway per-GB processing fees, not from exotic packet processing.
- Make observability a first-class differentiator: per-source attribution, top talkers, conntrack pressure, drop/error reasons, and AWS failover events.
- Make HA cloud-native: route/EIP/ENI takeover through AWS APIs, guarded by a lease/fencing mechanism.
- Ship with Terraform as the primary UX, plus a generated AMI or bootstrap script.

The project should not promise "same as AWS NAT Gateway" reliability. It should promise a narrower and testable contract:

> A low-cost self-hosted AWS egress appliance with explicit capacity profiles, appliance-local observability, and automated failover for new connections.

## Pillar 1: Low-overhead Self-hosted NAT Gateway

### Feasibility

Feasible.

AWS NAT Gateway charges both hourly and per GB processed. In common regions, AWS public pricing examples show `$0.045/hour` and `$0.045/GB`. At high traffic volume, the per-GB charge dominates. A self-hosted EC2 appliance does not remove normal EC2 data transfer charges, but it can avoid the NAT Gateway data processing line item.

### What "low overhead" should mean

Low overhead has three separate meanings:

1. **Cost overhead**: no NAT Gateway per-GB data processing charge.
2. **Operational overhead**: deployable by Terraform with sane defaults and rollback.
3. **Runtime overhead**: NAT appliance CPU/network usage is efficient enough for published instance profiles.

The project should avoid saying "free NAT." The honest claim is:

> Replaces NAT Gateway hourly/data-processing charges with EC2 instance, EIP, EBS, monitoring, and operational ownership costs.

### MVP implementation

- EC2 instance in public subnet.
- Source/destination check disabled.
- Private route table `0.0.0.0/0` points to NAT instance or NAT ENI.
- Linux forwarding enabled.
- `nftables` SNAT/masquerade.
- `nf_conntrack` tuned with conservative defaults.
- Prometheus metrics for traffic and conntrack.
- Terraform module provisions IAM, security group, EIP, instance/ASG, route entries, alarms.

### Cost model product feature

The product should ship a calculator:

Inputs:

- Region.
- Monthly GB through NAT.
- Number of AZs.
- NAT Gateway count being replaced.
- Instance type.
- EBS size.
- CloudWatch/metrics assumptions.

Outputs:

- Estimated NAT Gateway cost.
- Estimated BetterNAT cost.
- Break-even GB/month.
- Sensitivity table for one-AZ, two-AZ, three-AZ deployment.

This calculator is important because many users will otherwise compare only hourly EC2 cost and miss cross-AZ or monitoring costs.

### Risk boundaries

- NAT Gateway is managed and horizontally engineered; a single EC2 appliance is bounded by instance bandwidth, pps, CPU, and conntrack memory.
- Published performance numbers must be tied to specific instance types and traffic profiles.
- Some traffic should not use NAT at all: S3 Gateway Endpoint, DynamoDB Gateway Endpoint, and PrivateLink can reduce or remove NAT traffic for AWS services.

## Pillar 2: Better Observability

### Feasibility

Feasible, and probably the strongest product differentiator.

AWS NAT Gateway CloudWatch metrics include bytes, packet counts, connection counts, and error metrics. They are useful, but they are gateway-level metrics. They do not directly answer product questions like:

- Which private IP caused the cost spike?
- Which destination CIDR/domain is expensive?
- Which subnet/team/service is producing the most egress?
- Are drops caused by conntrack pressure, route failover, port exhaustion, or instance saturation?
- Did a failover happen, and how long until new connections succeeded?

### What we can observe in MVP

Without custom eBPF:

- Interface bytes/packets.
- nftables counters by configured CIDR/subnet.
- conntrack count/max and errors.
- CPU softirq.
- ENA/interface drops.
- AWS API failover events.
- Route/EIP owner state.

With low-risk eBPF accounting:

- Per-source private IP bytes/packets.
- Per-source connection attempts.
- Top destination IP/CIDR.
- Protocol/port distribution.
- Sampled flow events.
- Drop counters at selected hooks.

With DNS correlation:

- Approximate destination domain attribution by observing DNS responses from private sources.
- This is best-effort only because DNS can be encrypted, cached, externalized, or bypassed.

### MVP observability surface

Prometheus metrics:

- `betternat_bytes_total{direction,source_subnet,source_ip,protocol}`
- `betternat_packets_total{direction,source_subnet,source_ip,protocol}`
- `betternat_conntrack_entries`
- `betternat_conntrack_limit`
- `betternat_conntrack_insert_failed_total`
- `betternat_drops_total{reason}`
- `betternat_failover_events_total{action,result}`
- `betternat_active_owner`
- `betternat_aws_api_latency_seconds{operation}`

CLI:

- `betternat top sources`
- `betternat top destinations`
- `betternat doctor`
- `betternat failover status`
- `betternat cost estimate`

Dashboards:

- Top source IPs by bytes.
- Top destination CIDRs.
- Conntrack pressure.
- Drops/errors.
- Failover timeline.
- Instance saturation.

### Product warning

Do not promise Hubble-level service identity for arbitrary EC2 traffic unless integrated with a source of identity:

- AWS tags.
- ENI metadata.
- Kubernetes labels.
- Static CIDR/team mapping.
- Cloud Map/CMDB.

The product should support an attribution config:

```yaml
owners:
  - name: payments
    cidrs: ["10.20.0.0/20"]
    cost_center: fin-prod
  - name: analytics
    cidrs: ["10.40.0.0/16"]
    cost_center: data
```

## Pillar 3: Low-cost, Stable, Fast HA

### Feasibility

Feasible for fast recovery of new connections.

Not feasible to honestly promise seamless preservation of all active connections in the first version.

### AWS primitives

The product can use three AWS failover strategies:

#### Strategy A: Replace private route target

Private subnet route table points `0.0.0.0/0` to the active NAT instance or ENI. On failover, standby calls `ReplaceRoute`.

Pros:

- Natural for private subnet egress.
- Does not require moving public EIP if each node has its own EIP.
- Can support per-AZ active/standby route ownership.

Cons:

- Existing flows usually break.
- Route propagation/control-plane convergence is not a hard real-time SLO.
- Must manage one or more route tables correctly.

#### Strategy B: Reassociate EIP

Standby calls `AssociateAddress` with reassociation enabled to take the public EIP.

Pros:

- Preserves a fixed public egress IP.
- Simple mental model.

Cons:

- Private subnet routes still need to point at the active node/ENI.
- Existing flows usually break unless state and datapath alignment are solved.
- EIP API is not a deterministic 2-second guarantee.

#### Strategy C: Move secondary ENI or secondary private IP

AWS documentation explicitly describes moving a network interface and/or secondary private IPv4 address to a standby instance for failover.

Pros:

- Stable private route target if route points at ENI.
- EIP can remain associated to secondary private IP/ENI pattern.
- Cleaner ownership model than instance ID failover.

Cons:

- ENI attach/detach has operational details.
- Primary ENI cannot be moved; design must use secondary ENI/IP.
- Linux interface configuration must handle attach events.

### Recommended HA design

Default design:

```text
Per AZ:
  active NAT appliance
  standby NAT appliance
  route table target -> active NAT ENI
  optional EIP on active public side
  DynamoDB lease for ownership
```

Failover loop:

1. Standby observes missed health checks.
2. Standby attempts to acquire lease with TTL/fencing token.
3. Standby verifies current AWS state.
4. Standby updates route target or EIP/ENI association.
5. Standby updates local state to active.
6. Standby emits event and metrics.
7. Old active, if it recovers, sees stale lease token and demotes itself.

### Split-brain prevention

This is mandatory. Heartbeat alone is not enough.

Possible lease stores:

- DynamoDB conditional write with TTL.
- SSM Parameter Store with version/CAS-style logic, if acceptable.
- EC2 tags with conditional semantics are weaker and should be avoided as the primary lock.

Recommended: DynamoDB lease table.

Required properties:

- Owner instance ID.
- Generation/fencing token.
- Expiry timestamp.
- Last heartbeat timestamp.
- Route table/EIP/ENI resources owned.

### HA SLO wording

Do not promise "2-5 second recovery" before measurement.

Use staged wording:

- MVP target: "automated failover for new connections."
- Lab target: "detect and initiate takeover within N seconds."
- Published SLO: only after EC2 tests across multiple regions/instance types.

Example honest claim:

> BetterNAT automates failover of the active NAT route/EIP owner. Existing connections may reset during failover; new connections recover after AWS control-plane convergence and local health checks complete.

### Optional active-flow preservation

Future research:

- `conntrackd` state sync.
- eBPF map state sync.
- Active-active sharding by subnet.
- Drain before planned maintenance.

This is not MVP. It is a later premium feature.

## Pillar 4: One-command Install and Terraform UX

### Feasibility

Feasible and necessary.

For a NAT appliance, UX is not just convenience. Bad setup causes outages. The installer must enforce safe defaults and validate AWS prerequisites.

### Primary UX: Terraform module

The main product surface should be:

```hcl
module "betternat" {
  source = "github.com/.../terraform-aws-betternat"

  vpc_id              = var.vpc_id
  public_subnet_ids   = var.public_subnet_ids
  private_route_table_ids = var.private_route_table_ids

  mode = "ha"

  instance_type = "c7gn.large"
  allowed_private_cidrs = ["10.0.0.0/8"]

  observability = {
    prometheus = true
    grafana_dashboards = true
  }
}
```

Terraform should create:

- IAM role and least-privilege policy.
- Security groups.
- EIP(s), optional.
- EC2 instance(s) or ASG.
- Launch template.
- Source/destination check disabled.
- Route table entries.
- DynamoDB lease table, if HA enabled.
- CloudWatch alarms or SNS hooks, optional.
- SSM access, no inbound SSH by default.

Terraform provider support is already available for the needed primitives, including `source_dest_check` on `aws_instance`, EIP association, and routes.

### AMI build

Use Packer or EC2 Image Builder.

AMI should include:

- `betternat-agent`.
- nftables config template.
- systemd units.
- sysctl defaults.
- Prometheus exporter endpoint.
- `betternat doctor`.
- optional debug tools: `conntrack`, `nft`, `ethtool`, `bpftool`.

Packer's Amazon EBS builder can build an AMI by launching an EC2 instance, provisioning it, and creating an AMI.

### One-command install

For developer UX:

```sh
betternat init aws
betternat plan
betternat apply
```

Under the hood, this should generate Terraform, not hide infrastructure state in an opaque installer.

Better split:

- CLI for discovery and config generation.
- Terraform for actual provisioning.
- Agent for runtime failover and observability.

### Validation UX

`betternat doctor` should check:

- Source/destination check disabled.
- IP forwarding enabled.
- nftables rules loaded.
- conntrack module loaded and limits sane.
- Active route table target points to this owner.
- EIP association, if configured.
- DynamoDB lease health.
- IAM permissions.
- Prometheus endpoint.
- Test outbound connection from a private probe instance, if provided.

### Rollback UX

This is required for trust.

Terraform should support:

```hcl
fallback_nat_gateway_id = "nat-..."
```

Or:

```sh
betternat rollback --to nat-gateway
```

Rollback should restore private route tables to the previous NAT Gateway or previous route target.

## Product Scope Recommendation

### v0: Prove Cost and Correctness

- Single-AZ active/standby.
- nftables NAT.
- Terraform module.
- AMI or bootstrap script.
- Basic Prometheus metrics.
- Route or EIP failover.
- `doctor` command.
- Cost calculator.

### v1: Prove Observability

- eBPF flow accounting.
- Top source/destination views.
- Grafana dashboard.
- owner/team attribution mapping.
- conntrack pressure alarms.

### v2: Prove HA Quality

- DynamoDB lease/fencing.
- planned failover/drain.
- chaos tests.
- measured failover SLO.
- optional conntrackd state sync experiment.

### v3: Optimize Datapath

- TC eBPF fast path only if benchmark data justifies it.
- VPP edition only if Linux NAT cannot hit target profiles.

## Marketing Claims: Safe vs Unsafe

### Headline Positioning

The public headline should emphasize capabilities, not the implementation stack.

Good headline examples:

- "Low-cost, observable, highly available egress for AWS private subnets."
- "Cut high-volume NAT Gateway processing charges without losing visibility or failover."
- "A self-owned AWS egress gateway with traffic attribution, automated failover, and Terraform-first deployment."

Mention `nftables`, `nf_conntrack`, eBPF, or VPP only in architecture sections, benchmark notes, or advanced configuration docs.

### Safe

- "Avoid NAT Gateway per-GB processing charges for suitable workloads."
- "Self-hosted NAT appliance with Terraform-first deployment."
- "Per-source egress attribution unavailable in basic NAT Gateway metrics."
- "Automated route/EIP failover for new connections."
- "Reliable Linux networking baseline with optional egress attribution."

### Unsafe before proof

- "Drop-in replacement for AWS NAT Gateway."
- "Same availability as AWS managed NAT Gateway."
- "2-5 second guaranteed failover."
- "Zero-loss observability."
- "eBPF-powered NAT datapath" if v1 forwarding is actually nftables.
- "No operational overhead."

## Decision

Build the product around these four pillars:

1. **Cost**: remove per-GB NAT Gateway processing fees where appliance ownership makes sense.
2. **Observability**: ship per-source attribution and appliance health as the main differentiator.
3. **HA**: implement AWS-native failover with lease/fencing, but only promise new-connection recovery until measured.
4. **UX**: Terraform-first install, generated AMI/bootstrap, `doctor`, and rollback.

This gives BetterNAT a credible niche: not "AWS NAT Gateway but magically free," but "a transparent, low-cost, self-owned egress appliance for teams whose NAT bill is large enough to justify ownership."

## Sources

- AWS NAT Gateway pricing: charged by available hour and each GB processed: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-pricing.html
- AWS VPC pricing example with NAT Gateway hourly and data processing charges: https://aws.amazon.com/vpc/pricing/
- AWS NAT Gateway CloudWatch metrics, including active connections, bytes, packets, drops, and errors: https://docs.aws.amazon.com/vpc/latest/userguide/metrics-dimensions-nat-gateway.html
- AWS NAT Gateway monitoring with CloudWatch metrics at 1-minute intervals: https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway-cloudwatch.html
- AWS NAT Gateway use case guidance, including per-AZ resiliency recommendation: https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-scenarios.html
- AWS NAT instance documentation: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_NAT_Instance.html
- AWS work with NAT instances: https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html
- EC2 AssociateAddress API with `AllowReassignment`: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_AssociateAddress.html
- EC2 ReplaceRoute API: https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_ReplaceRoute.html
- EC2 ENI failover note for moving interface and/or secondary private IPv4 to standby: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/scenarios-enis.html
- Terraform `aws_instance` supports `source_dest_check` for NAT/VPN routing use cases: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance
- Terraform `aws_eip_association` supports `allow_reassociation`: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip_association
- Terraform `aws_route` resource: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route
- Packer Amazon EBS builder creates AMIs by launching and provisioning EC2 instances: https://developer.hashicorp.com/packer/integrations/hashicorp/amazon/latest/components/builder/ebs
