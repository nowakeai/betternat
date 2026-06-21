# BetterNAT User Documentation Guide

Date: 2026-06-21

## Purpose

This guide defines how to write the first public BetterNAT user-facing documentation.

Primary references:

- `code_references/fck-nat`
- `code_references/loxilb`

The goal is not to copy their wording. The goal is to borrow the parts that make their docs useful: clear positioning, fast install path, honest limitations, concrete AWS tradeoffs, and operational recipes.

## Reference: fck-nat

Use fck-nat as the main reference for product documentation shape.

Important files:

- `code_references/fck-nat/README.md`
- `code_references/fck-nat/docs/deploying.md`
- `code_references/fck-nat/docs/configuration.md`
- `code_references/fck-nat/docs/features.md`
- `code_references/fck-nat/docs/limitations.md`
- `code_references/fck-nat/docs/choosing_an_instance_size.md`
- `code_references/fck-nat/docs/ami-support-policy.md`

What to borrow:

- Put the cost problem and target user in the first screen.
- Be explicit about when a managed NAT Gateway is still the better choice.
- Show the recommended IaC path before manual wiring.
- Keep the happy path short.
- Put optional features in a separate feature page.
- Put configuration knobs in a table.
- Put IAM permissions in a table tied to features.
- Put limitations in their own page, not hidden in footnotes.
- Explain instance-size selection with AWS internet bandwidth constraints.
- Document source/destination check, route table ownership, and security group requirements.
- Treat HA as an explicit operating mode with clear failure semantics.
- Make cleanup and rollback easy to find.

What not to copy directly:

- AMI-first language for the first BetterNAT alpha. BetterNAT `v0.1.0-alpha.1` intentionally uses Terraform plus cloud-init bootstrap and does not publish a BetterNAT AMI.
- fck-nat's ENI takeover architecture. BetterNAT uses route/EIP reconciliation with ASG active/standby appliances.
- fck-nat's performance claims. BetterNAT should only claim what its own tests prove.
- fck-nat's exact tuning knobs. BetterNAT currently applies only a conservative baseline sysctl profile.

BetterNAT-specific differences to explain:

- BetterNAT is Terraform-provider first for alpha.
- BetterNAT supports an ASG pool with active/standby HA.
- Stable egress IP mode uses EIP reassociation.
- Non-stable mode changes public source IP after failover.
- Existing connections may reset during failover.
- `doctor --live` is the primary appliance-local health check.
- Prometheus metrics are the default observability surface.
- No public SSH should be required in the default path; SSM is preferred.

## Reference: LoxiLB

Use LoxiLB as the main reference for integrated datapath dependency documentation.

Important files:

- `code_references/loxilb/README.md`
- `code_references/loxilb/LICENSE`
- `code_references/loxilb/SECURITY.md`
- `code_references/loxilb/api/swagger.yml`
- `code_references/loxilb/options/options.go`
- `code_references/loxilb/api/prometheus/prometheus.go`

What to borrow:

- Describe LoxiLB as a Go/eBPF L4 datapath component.
- Link or point users to upstream LoxiLB documentation for advanced LoxiLB internals.
- Keep BetterNAT docs focused on how BetterNAT configures and supervises LoxiLB.
- Document the runtime shape:
  - LoxiLB container,
  - host networking,
  - privileged mode,
  - `loxicmd` wrapper when no host binary is supplied.
- Document how to inspect LoxiLB from the appliance.
- Document LoxiLB license and attribution.

What not to copy directly:

- Broad Kubernetes service load-balancer positioning. BetterNAT is not a Kubernetes Service LoadBalancer product.
- Telco/SCTP/GTP/Ingress/Gateway API feature claims unless BetterNAT explicitly exposes and tests them.
- LoxiLB HA claims as BetterNAT HA claims. BetterNAT's HA behavior is controlled by the BetterNAT agent and AWS primitives.
- Upstream performance claims unless reproduced in BetterNAT's exact AWS NAT-gateway workload.
- Linux `nf_conntrack` tuning language as if it controlled LoxiLB's eBPF conntrack capacity.

Required LoxiLB wording discipline:

- Say "BetterNAT integrates LoxiLB" or "BetterNAT uses LoxiLB as a datapath component."
- Do not imply that LoxiLB endorses, certifies, or officially supports BetterNAT.
- Preserve Apache 2.0 attribution in notices.
- Keep BetterNAT support boundaries clear: BetterNAT supports its own integration; upstream LoxiLB remains a third-party dependency.

## First Alpha User Docs

Minimum user-facing docs before `v0.1.0-alpha.1`:

- `README.md`
- Quick Start
- Terraform install guide
- Existing VPC migration guide
- Operations guide
- Limitations page
- Failure modes page
- Release notes

README structure should be:

1. What BetterNAT is.
2. Why it exists: NAT Gateway per-GB processing charges and high-volume private-subnet egress.
3. Who it is for:
   - crawler fleets,
   - blockchain/RPC nodes syncing from public peers,
   - Kubernetes nodes pulling large public images,
   - workloads with tens of TB/month of public internet downloads.
4. Who should not use it:
   - workloads that require AWS-managed NAT Gateway SLA,
   - multi-AZ managed service semantics,
   - active connection preservation,
   - hands-off infrastructure.
5. Current alpha status:
   - AWS only,
   - single-AZ HA group,
   - Terraform plus cloud-init bootstrap,
   - no published BetterNAT AMI,
   - no high-volume benchmark claim yet.
6. Quick Terraform example.
7. Verification commands:
   - `betternat doctor --live`,
   - SSM,
   - private client egress probe,
   - Prometheus metrics.
8. Cleanup command.

## Quick Start Requirements

The Quick Start must:

- start from a clean AWS account/region assumption,
- use a disposable VPC path first,
- list expected AWS charges,
- require no public SSH,
- use Spot only where clearly marked,
- use artifact checksums,
- avoid local absolute paths,
- include `terraform destroy`,
- include residual cleanup checks.

The first alpha Quick Start must say:

```text
BetterNAT v0.1.0-alpha.1 does not publish a BetterNAT AMI.
It launches an explicit Linux AMI and uses cloud-init to install release artifacts at boot.
```

## Configuration Docs

Use fck-nat's configuration table style.

BetterNAT config sections to document:

- required Terraform inputs:
  - `name`,
  - `region`,
  - `vpc_id`,
  - `public_subnet_ids`,
  - `private_route_table_ids`,
  - `private_cidrs`.
- capacity:
  - `instance_type`,
  - `use_spot`,
  - `min_size`,
  - `desired_capacity`,
  - `max_size`.
- egress identity:
  - `stable_egress_ip=true`,
  - `stable_egress_ip=false`.
- HA:
  - `ha_profile`,
  - `ha_lease_ttl_seconds`,
  - `ha_renew_interval_seconds`.
- bootstrap:
  - `ami_id`,
  - `agent_binary_url`,
  - `agent_binary_sha256`,
  - `cli_binary_url`,
  - `cli_binary_sha256`,
  - optional `loxicmd_binary_url`,
  - optional `loxicmd_binary_sha256`.
- observability:
  - `prometheus_enabled`,
  - metrics port,
  - `doctor --live`.

Do not expose advanced kernel/NIC tuning as a supported user knob until it is implemented and benchmarked.

When discussing `nf_conntrack_max`, state that it is retained for nftables fallback and Linux-netfilter compatibility. LoxiLB conntrack is inspected through LoxiLB/BetterNAT metrics, not Linux `conntrack -L`.

## Limitations Page

The limitations page must be blunt.

Include:

- alpha quality,
- AWS only,
- single-AZ HA group,
- no NAT Gateway equivalent SLA,
- no active connection preservation,
- stable EIP converges after failover, but failure detection and control-plane convergence take time,
- non-stable mode changes source public IP after failover,
- boot time depends on package repositories, container pulls, and artifact URLs in the alpha bootstrap path,
- no published BetterNAT AMI in first alpha,
- high-volume savings are modeled, not proven by expensive multi-TB tests,
- EC2 instance bandwidth and packet limits still apply,
- AWS security group connection tracking quotas can matter,
- BetterNAT still incurs EC2, EBS, EIP, data transfer, DynamoDB, CloudWatch/SSM/logging costs.

## Instance Sizing Page

Use fck-nat's instance-sizing doc as the model, but do not copy its numbers blindly.

BetterNAT sizing docs should:

- explain AWS "up to" vs baseline bandwidth,
- explain EC2 internet egress limits,
- explain that throughput depends on instance family, packet size, connection churn, datapath, and security group conntrack,
- start with conservative recommendations:
  - `t4g.small` for disposable tests,
  - larger Graviton/network-optimized instances for sustained traffic,
  - no blanket high-volume production recommendation until benchmarked.
- include a future plan to generate instance guidance from AWS APIs and measured BetterNAT benchmark runs.

## Operations Docs

Operations docs should use concrete commands instead of prose-only explanations.

Include:

- check active appliance,
- run `betternat doctor --live`,
- scrape Prometheus metrics,
- inspect systemd logs,
- inspect DynamoDB lease,
- inspect route target,
- inspect EIP association,
- test private client egress,
- destroy and verify cleanup,
- what to do when bootstrap fails,
- what to do when LoxiLB is not ready,
- what to do when `doctor --live` reports IAM/ASG/route/EIP failures.

## Documentation Tone

Use a serious but approachable tone.

Allowed:

- "BetterNAT helps avoid NAT Gateway per-GB processing charges for high-volume private-subnet egress."
- "Better not be surprised by NAT Gateway bills."

Avoid:

- profanity,
- overclaiming performance,
- implying AWS-managed service equivalence,
- implying LoxiLB endorsement,
- hiding alpha limitations.

## Release Checklist Integration

Before cutting the first alpha:

- [ ] README follows this guide.
- [ ] Quick Start follows this guide.
- [ ] Configuration page includes Terraform input table.
- [ ] Limitations page exists and is linked from README.
- [ ] LoxiLB attribution is present.
- [ ] fck-nat-inspired instance sizing section exists or is explicitly deferred.
- [ ] No user-facing first-release docs mention paid editions or future Pro features.
