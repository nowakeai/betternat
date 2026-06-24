# BetterNAT Optional AMI Release Plan

Date: 2026-06-20

## Purpose

This document describes the optional BetterNAT AMI path.

The first production-preview release is bootstrap-first: users provide an
explicit Linux AMI and Terraform/cloud-init installs BetterNAT release artifacts
at boot. Public BetterNAT AMIs are not required because each published AMI
version and copied region creates ongoing EBS snapshot-retention cost.

Prebuilt AMIs remain useful later for faster boot, lower bootstrap dependency
surface, and simpler first-run behavior. Treat this plan as an optional
acceleration path, not a production-preview release blocker.

## Optional AMI Contract

Each BetterNAT AMI should contain:

- `betternat-agent`,
- `betternat` CLI,
- LoxiLB runtime,
- `loxicmd`,
- nftables fallback dependencies,
- conntrack diagnostics,
- Docker or the selected LoxiLB service runtime,
- SSM agent,
- CloudWatch agent if CloudWatch metric export is enabled,
- sysctl profile,
- systemd unit files,
- `betternat doctor`.

Terraform should set:

```hcl
bootstrap_mode = "prebaked_ami"
```

User data should only provide runtime configuration:

- write `/etc/betternat/agent.json`,
- set strict file permissions,
- start or restart `betternat-agent`,
- avoid long package installs.

In `prebaked_ami` mode, stable EIP deployments can avoid per-node public IPv4
addresses because the AMI already contains the runtime and the shared EIP is the
private-workload egress identity. Non-stable deployments still need per-node
public IPv4 because the active gateway node's public IP is the egress identity.
Operators can still set `associate_public_ip_address` explicitly when they want
to override the provider-derived launch-template behavior for a specific
environment.

The bootstrap-first `cloud_init` path uses public subnet auto-assigned public
IPv4 by default so new nodes can download packages, pull the LoxiLB image, fetch
release artifacts, join SSM, and call AWS APIs.

When `stable_egress_ip=true`, the shared EIP is the intended private-workload
egress identity. Gateway nodes may still have ordinary public IPv4 addresses for
management. If production AMIs need strict separation between management public
IPv4 and stable egress EIP, the shared EIP should attach to a secondary private
IP or secondary ENI and the datapath should SNAT to that egress private IP.

Before using no-public-IP standby nodes, the install environment must provide a
bootstrap/control-plane reachability path. One possible shape is private AWS API
reachability through VPC endpoints. Another is routing standby egress through
the current active gateway, provided the gateway subnet routing is designed so
the active node itself still has a valid internet path. A standby node without a
public IP or the shared EIP still needs to renew/register with DynamoDB, observe
ASG termination state, complete lifecycle actions, call EC2 route/EIP APIs
during takeover, and use STS/IAM/SSM checks where configured.

## Naming

Use predictable AMI names:

```text
betternat-al2023-hvm-<version>-<date>-arm64-ebs
betternat-al2023-hvm-<version>-<date>-x86_64-ebs
```

Example:

```text
betternat-al2023-hvm-v0.1.0-20260620-arm64-ebs
```

## Channels

Terraform provider UX should support:

```hcl
ami_channel = "stable"
ami_channel = "candidate"
ami_channel = "dev"
ami_id      = "ami-..."
```

Current state:

- `ami_channel` exists in the provider schema and install plan.
- `ami_id` is the only launchable path until channel resolution is implemented.
- A maintainer Packer template exists at `packer/betternat.pkr.hcl` as the
  repeatable AMI build starting point.
- AWS supplemental tests should not wait for a BetterNAT AMI. Use the latest official Amazon Linux 2023 AMI plus cloud-init and a temporary `betternat-agent` binary URL while debugging the product body.

Channel resolver behavior:

- `ami_id` wins when set.
- Otherwise resolve the latest AMI for the requested channel, region, architecture, and owner.
- Resolver must fail before mutating routes if no AMI is found.
- Provider state should record the resolved `ami_id`.

## Bootstrap Contract

Default config path:

```text
/etc/betternat/agent.json
```

Default service:

```text
betternat-agent.service
```

Required boot readiness checks:

```sh
systemctl is-active betternat-agent.service
systemctl is-active docker
test -s /etc/betternat/agent.json
betternat doctor --config /etc/betternat/agent.json
loxicmd get firewall -o json
```

During development, failures in `betternat-agent` readiness are allowed to block datapath tests, but should not block pure Terraform control-plane tests.

## Packer Build

Build release binaries first:

```sh
BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-build.sh
```

Then initialize and build the AMI:

```sh
packer init packer/betternat.pkr.hcl
packer build \
  -var-file=packer/betternat-al2023.pkrvars.hcl \
  -var-file=packer/betternat-arm64.pkrvars.hcl \
  -var "version=v0.1.0-alpha.2" \
  -var "aws_region=us-west-2" \
  -var "agent_binary_path=tmp/release/v0.1.0-alpha.2/betternat-agent_v0.1.0-alpha.2_linux_arm64" \
  -var "cli_binary_path=tmp/release/v0.1.0-alpha.2/betternat_v0.1.0-alpha.2_linux_arm64" \
  packer/betternat.pkr.hcl
```

For x86_64, use `packer/betternat-x86_64.pkrvars.hcl` and the
`linux_amd64` release binaries.

Public, all-region publication is intentionally explicit:

```sh
packer build \
  -var-file=packer/betternat-al2023.pkrvars.hcl \
  -var-file=packer/betternat-arm64.pkrvars.hcl \
  -var-file=packer/betternat-public-all-regions.pkrvars.hcl \
  -var "version=v0.1.0-alpha.2" \
  -var "aws_region=us-west-2" \
  -var "agent_binary_path=tmp/release/v0.1.0-alpha.2/betternat-agent_v0.1.0-alpha.2_linux_arm64" \
  -var "cli_binary_path=tmp/release/v0.1.0-alpha.2/betternat_v0.1.0-alpha.2_linux_arm64" \
  packer/betternat.pkr.hcl
```

The AL2023 Docker flavor:

- starts from the latest Amazon Linux 2023 minimal kernel 6.12 AMI for the selected architecture,
- installs `betternat-agent` and `betternat`,
- installs Docker, nftables, conntrack tools, SSM agent, CloudWatch agent, and network diagnostics,
- pre-pulls the pinned LoxiLB image,
- installs `loxicmd` as a host wrapper,
- installs `loxilb.service` and `betternat-agent.service`,
- writes the baseline sysctl profile,
- copies BetterNAT `LICENSE` and `THIRD_PARTY_NOTICES.md`,
- records `/usr/share/doc/betternat/AMI_MANIFEST`.
- writes a local Packer manifest, `packer-manifest.json`, by default.

The template is not yet wired into provider `ami_channel` resolution and does
not publish AMIs automatically.

Validation recorded on 2026-06-23:

```sh
packer init packer/betternat.pkr.hcl
packer validate -var-file=packer/betternat-al2023.pkrvars.hcl -var-file=packer/betternat-arm64.pkrvars.hcl ...
packer validate -var-file=packer/betternat-al2023.pkrvars.hcl -var-file=packer/betternat-x86_64.pkrvars.hcl ...
```

AL2023 arm64 and AL2023 x86_64 template validation passed. No AMI was created
by this validation pass.

Follow-up build validation found that the upstream LoxiLB `v0.9.7` `.deb`
pre-install script requires OpenSSL 3. Ubuntu 20.04 Focal ships OpenSSL 1.1.1,
so a temporary systemd experiment moved to Ubuntu 24.04 Noble.

Ubuntu 24.04 Noble build validation then found a release blocker on 2026-06-23:

- Noble installed and booted successfully on AWS, and SSM came online.
- The raw `v0.9.7-1` package unit uses `ExecStart=/usr/local/sbin/loxilb`
  without `--api`, so port `11111` is not exposed.
- On AWS Ubuntu 24.04 kernel `6.17.0-1017-aws`, LoxiLB `v0.9.7-1` fails
  eBPF object loading and restarts.
- In-place upgrade to LoxiLB `v0.9.8-1` with a BetterNAT override
  `ExecStart=/usr/local/sbin/loxilb --api --fallback` still fails eBPF object
  loading and never exposes the API on `127.0.0.1:11111`.
- LoxiLB issue `#953` and PR `#956` indicate the kernel `6.12+` fix landed
  after the latest public `.deb` asset, while newer Docker images continue to
  be published.

The Ubuntu systemd `.deb` flavor has been removed from the release path until
newer upstream `.deb` packages exist or BetterNAT owns a supported package build.
The release AMI path is AL2023 with Docker and a pinned LoxiLB image digest.
The current release pin is recorded in
[`DEPENDENCY_PINS.md`](DEPENDENCY_PINS.md): BetterNAT `v0.1.0-alpha.2`
corresponds to LoxiLB `v0.9.8.6`.

## Sysctl Profile

Baseline:

```text
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.netfilter.nf_conntrack_max = 1048576
```

Later sizing profiles can tune:

- `net.ipv4.ip_local_port_range`,
- `net.netfilter.nf_conntrack_tcp_timeout_established`,
- conntrack buckets,
- SYN backlog,
- TCP keepalive.

## Release Testing

Before marking an AMI stable:

1. Launch one appliance from the AMI in a disposable VPC.
2. Verify SSM access without public SSH.
3. Verify `betternat-agent.service` starts.
4. Verify LoxiLB readiness.
5. Verify nftables fallback tools are present.
6. Verify `betternat doctor` output.
7. Verify private client egress through the appliance.
8. Verify route-only failover to a second appliance.
9. Verify stable-EIP failover to a second appliance.
10. Verify cleanup leaves no tagged resources.
11. Verify standby operation with no auto-assigned public IPv4 address and
    private AWS API reachability.

## Impact On AWS Supplemental Tests

The next AWS supplemental pass can start before AMI release work if it is scoped correctly:

- Terraform provider lifecycle: ready for AWS with official AL2023 + cloud-init.
- Route-only timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- Stable-IP timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- Client recovery timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- AMI boot-to-ready timing: blocked until a dev AMI exists.

Record cloud-init boot timings now if using a development AMI or bootstrap path. Repeat after a real AMI exists.

2026-06-23 dev AMI validation:

- Private AL2023 arm64 dev AMI `ami-072757363df299006` passed boot smoke.
- SSM was online about `25s` after EC2 launch time.
- User data completed about `27s` after EC2 launch time.
- Baked Docker, LoxiLB, and BetterNAT agent services were active by the
  verification check at about `49s` instance uptime.
- LoxiLB listened on `*:11111`, reported `0.9.8.6-beta`, and exposed the
  expected SNAT rule for `10.88.0.0/16`.
- The same AMI was rolled into the active ASG with launch template version `15`;
  instance refresh `c7c091e4-63b6-4895-a160-ef75f7113a6f` completed
  successfully.
- Follow-up before stable AMI publication: convert temporary VPC endpoint
  requirements into provider-managed or clearly documented install behavior.

2026-06-23 no-public-IP stable-EIP validation:

- Temporary private AWS control-plane reachability was added with VPC endpoints
  for DynamoDB, EC2, Auto Scaling, STS, SSM, SSM Messages, and EC2 Messages.
- Launch template version `16` used `AssociatePublicIpAddress=false`.
- ASG instance refresh `2cf3c2c8-2381-4e4b-976f-3fe55b728aa0` completed
  successfully.
- Final gateway nodes had no per-node public IPv4 addresses:
  - active node had only the shared EIP `52.24.117.43`,
  - standby node had no public IP.
- Manual handover to the no-public-IP standby completed successfully.
- Client egress probe during handover observed `0` non-shared public IP
  samples; it recorded `3` one-second curl timeouts out of `240` samples.

2026-06-24 non-stable public-IP validation:

- Launch template version `17` used `AssociatePublicIpAddress=true` and an
  agent config without `ha.public_identity`.
- ASG instance refresh `824ec267-c1a2-47a8-b363-a04d57974c66` completed
  successfully.
- Manual route-only handover completed from `i-0a89f292e07b04460` to
  `i-0d08059b2f4708db6`.
- Client egress probe observed `0` failures out of `240` samples and the
  expected public source IP change from `52.24.117.43` to `52.24.240.255`.
- The retained environment was restored to stable/no-public-IP launch template
  version `16` afterward.
