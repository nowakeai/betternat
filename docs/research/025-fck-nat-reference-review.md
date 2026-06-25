# fck-nat Reference Review

Date: 2026-06-20

## Question

What should BetterNAT borrow from the `code_references/fck-nat` project before continuing AWS supplemental testing and production packaging?

## Short Answer

fck-nat is most useful as a reference for product packaging, AMI operations, documentation, and user-facing simplicity.

It is less useful as a direct datapath or HA blueprint for BetterNAT because BetterNAT's current direction is:

- LoxiLB-first datapath,
- no product fallback datapath; legacy nftables diagnostics only while retained,
- Terraform provider-owned AWS lifecycle,
- route/EIP failover,
- richer observability and rollback.

fck-nat proves that a NAT replacement product can win by being extremely easy to deploy as an AMI and by documenting the exact AWS tradeoffs. BetterNAT should borrow that product discipline, while keeping its own control plane and datapath design.

## Reviewed Files

- `code_references/fck-nat/README.md`
- `code_references/fck-nat/docs/configuration.md`
- `code_references/fck-nat/docs/deploying.md`
- `code_references/fck-nat/docs/features.md`
- `code_references/fck-nat/docs/limitations.md`
- `code_references/fck-nat/docs/choosing_an_instance_size.md`
- `code_references/fck-nat/docs/ami-support-policy.md`
- `code_references/fck-nat/packer/fck-nat.pkr.hcl`
- `code_references/fck-nat/service/fck-nat.sh`
- `code_references/fck-nat/service/fck-nat.service`
- `code_references/fck-nat/service/post-install.sh`
- `code_references/fck-nat/Makefile`

## What fck-nat Does Well

### 1. AMI-First Product Shape

fck-nat's user-facing product is an AMI, not a script repository.

It ships Amazon Linux 2023 AMIs for both ARM and x86. The AMI already contains:

- the NAT service,
- iptables,
- CloudWatch agent,
- SSM agent,
- conntrack tools,
- kernel live patch support.

BetterNAT should do the same for production. Development can keep using cloud-init, but production should prefer a prebuilt AMI so startup does not depend on package downloads, container pulls, GitHub availability, or slow first-boot compilation.

Recommended BetterNAT action:

- create a Packer-based AMI build path,
- publish arm64 and x86_64 variants,
- preinstall LoxiLB, `betternat-agent`, SSM agent, diagnostics tools, and
  systemd units; legacy nftables tools may remain only as diagnostics while
  that code is phased out,
- keep cloud-init limited to small runtime config.

### 2. Simple Runtime Config Contract

fck-nat uses a small `/etc/fck-nat.conf` file, mostly passed once through EC2 user data.

Important knobs:

- `eni_id`,
- `eip_id`,
- CloudWatch agent options,
- conntrack/sysctl tuning.

BetterNAT should preserve the same product feel: one generated config, one systemd service, one obvious restart path.

Recommended BetterNAT action:

- make `/etc/betternat/agent.json` the canonical runtime file,
- make user data only write this file and restart/enable `betternat-agent`,
- include a `betternat doctor` command that explains config, datapath, cloud identity, route, EIP, and lease state.

### 3. Production-Tuned Sysctls

fck-nat exposes useful production knobs:

- `net.ipv4.ip_local_port_range`,
- `net.netfilter.nf_conntrack_max`,
- conntrack hash buckets,
- established TCP timeout,
- TCP keepalive,
- SYN backlog,
- IPv4 forwarding,
- reverse path filter disable.

Even with LoxiLB as the supported datapath, BetterNAT still needs Linux
forwarding. Linux conntrack visibility is useful for host-networking and legacy
diagnostics while retained, but it is not a product fallback path.

Recommended BetterNAT action:

- add an AMI/sysctl profile,
- expose selected tuning knobs through Terraform,
- document defaults and tradeoffs,
- include conntrack pressure in metrics and `doctor`.

### 4. Instance Sizing Documentation

fck-nat documents EC2 internet egress constraints clearly, including the practical 5 Gbps cap for smaller instances and the cost jump above that.

BetterNAT needs the same, but tuned for its target workloads:

- high-volume crawlers,
- blockchain/RPC nodes,
- image-pulling Kubernetes nodes,
- response-heavy ingress/download scenarios,
- stable-egress-IP requirements.

Recommended BetterNAT action:

- build a BetterNAT instance sizing guide,
- include monthly cost comparison against NAT Gateway data processing charges,
- separate low idle cost, sustained bandwidth, PPS, and connection-count guidance,
- include ARM-first recommendations.

### 5. AMI Refresh Policy

fck-nat explicitly documents why AMIs must be refreshed periodically because public AMIs become less discoverable after AWS deprecation behavior.

BetterNAT should not treat AMI publication as a one-time release.

Recommended BetterNAT action:

- define `ami_channel = "stable" | "candidate" | "dev"`,
- publish AMI names with version, date, arch, and base OS,
- refresh latest stable AMIs periodically,
- make Terraform provider resolve AMI channel to AMI ID,
- allow explicit `ami_id` override.

### 6. Operational Access

fck-nat installs SSM agent by default.

BetterNAT should do the same. NAT appliances should not require public SSH in production.

Recommended BetterNAT action:

- include SSM agent in the AMI,
- document IAM policy for SSM access separately from runtime failover policy,
- keep SSH optional and disabled by default in production examples.

### 7. CloudWatch/NAT Gateway Metric Parity

fck-nat tries to map instance metrics to familiar NAT Gateway-style names through CloudWatch agent.

BetterNAT's observability story is broader, but this is still a good UX idea: users migrating away from NAT Gateway should see familiar counters first, then richer BetterNAT-specific metrics.

Recommended BetterNAT action:

- keep Prometheus native metrics,
- optionally emit CloudWatch metrics,
- include NAT Gateway-like names for bytes, packets, drops, connection attempts, conntrack pressure,
- add BetterNAT-specific route/EIP/lease/datapath health metrics.

## What Not To Copy Directly

### 1. ENI-Attachment HA As The Default

fck-nat HA centers around attaching a configured ENI at startup so route tables can point to a stable internal endpoint.

BetterNAT has already decided route-table failover is the simpler v0 default. Dynamic ENI movement remains attractive later, but it is a more complex failover primitive and can add attach/detach convergence and device setup issues.

BetterNAT should keep:

- route replacement as v0 default,
- stable EIP as an optional public identity layer,
- ENI movement as a later advanced mode.

### 2. iptables As The Primary Datapath

fck-nat uses iptables MASQUERADE. BetterNAT should not move backward from LoxiLB-first just because fck-nat is simpler.

BetterNAT should keep:

- LoxiLB as primary datapath candidate,
- no product fallback datapath; legacy nftables diagnostics only while retained,
- no custom eBPF in v0.

### 3. Shell As The Main Control Plane

fck-nat is intentionally thin shell. BetterNAT needs a more explicit Go control plane because it owns:

- route/EIP failover,
- DynamoDB lease/fencing,
- provider lifecycle,
- observability,
- rollback,
- multi-cloud extension points.

Shell is fine for AMI provisioning and smoke helpers, not for the core agent.

### 4. ASG Replacement As The Main Recovery Story

fck-nat documents that replacement through ASG can take minutes, while planned overlap can reduce disruption.

BetterNAT's product promise is faster active/standby failover. ASG can still replace failed capacity in the background, but failover should be handled by the agent and standby appliance.

## Product Implications For BetterNAT

### Provider UX

fck-nat's deploy docs make deployment feel simple through CDK/Terraform modules. BetterNAT should do the same through its provider and module wrapper.

Target user config should stay close to:

```hcl
enable_nat_gateway = true
nat_backend        = "betternat"

betternat = {
  stable_egress_ip = true
  instance_type    = "t3.small"
  ami_channel      = "stable"
}
```

Avoid exposing low-level details unless users opt into advanced mode.

### AMI Release

fck-nat strongly reinforces that production BetterNAT should be AMI-first.

Recommended release artifacts:

- `betternat-al2023-hvm-<version>-<date>-arm64-ebs`,
- `betternat-al2023-hvm-<version>-<date>-x86_64-ebs`,
- optional `candidate` and `dev` channels,
- Terraform provider AMI channel resolver,
- documented support/refresh policy.

### Observability

fck-nat's CloudWatch metric parity is a useful baseline but not enough.

BetterNAT should provide:

- NAT Gateway-like counters for migration familiarity,
- route/EIP/lease state,
- LoxiLB rule/counter state,
- conntrack pressure,
- per-source attribution where available,
- `doctor` output that explains failures.

### Documentation

fck-nat's docs are a good reminder that users need practical guides, not only architecture documents.

BetterNAT docs should add:

- choosing instance size,
- choosing failover mode,
- choosing stable IP vs route-only,
- AMI support policy,
- production IAM model,
- CloudWatch/Prometheus cost notes,
- limitations and when to use NAT Gateway instead.

## Impact On AWS Supplemental Tests

Before production AMI work, provider tests can continue using cloud-init/user-data.

However, AWS supplemental tests should start collecting evidence that will matter for AMI design:

- first boot time with cloud-init today,
- LoxiLB ready time,
- agent ready time,
- route/EIP ready time,
- SSM access availability,
- whether package pulls/container pulls are on the critical path.

Once AMI build exists, repeat the same tests and compare:

- cloud-init prototype boot-to-ready,
- AMI boot-to-ready,
- failover standby readiness,
- reboot reconciliation.

## Recommended Backlog Items

1. Add `docs/release/AMI_RELEASE_PLAN.md`.
2. Add Packer skeleton for BetterNAT AMI.
3. Add `/etc/betternat/agent.json` and systemd unit contract to docs.
4. Add AMI channel resolver to Terraform provider.
5. Add instance sizing guide.
6. Add sysctl/conntrack tuning profile.
7. Add optional CloudWatch metrics guide.
8. Add SSM-first operational access guide.

## Conclusion

fck-nat's biggest lesson is not "use iptables." Its biggest lesson is that a NAT Gateway replacement becomes credible when it is packaged as a boring, well-documented AMI with a tiny user-facing config surface.

BetterNAT should borrow that release and UX discipline while keeping its LoxiLB-first datapath, Terraform provider lifecycle, and route/EIP failover control plane.
