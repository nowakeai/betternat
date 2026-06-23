# BetterNAT AMI Release Plan

Date: 2026-06-20

## Purpose

Production BetterNAT should be distributed as a prebuilt AMI.

Cloud-init/user-data remains useful for development and AWS control-plane tests, but it is not the preferred production path because first boot should not depend on package downloads, container pulls, GitHub availability, or build-time network behavior.

## Release Contract

Each production AMI should contain:

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

User data should only provide runtime configuration:

- write `/etc/betternat/agent.json`,
- set strict file permissions,
- start or restart `betternat-agent`,
- avoid long package installs.

Production AMI launches should not rely on per-node public IPv4 addresses.
The alpha bootstrap path may temporarily use public subnet auto-assigned
public IPs so new nodes can download packages and release artifacts before a
BetterNAT AMI exists. That is a development/install bootstrap compromise, not a
production networking contract.

Before disabling per-node public IPs by default, the production install path
must provide private AWS API reachability for standby nodes. A standby node
without the shared EIP still needs to renew/register with DynamoDB, observe ASG
termination state, complete lifecycle actions, call EC2 route/EIP APIs during
takeover, and use STS/IAM checks where configured. Production installs should
therefore use VPC endpoints or an equivalent private control-plane path for at
least DynamoDB, EC2, Auto Scaling, STS, IAM, SSM, and CloudWatch when those
features are enabled.

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
