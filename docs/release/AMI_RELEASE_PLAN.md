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

## Impact On AWS Supplemental Tests

The next AWS supplemental pass can start before AMI release work if it is scoped correctly:

- Terraform provider lifecycle: ready for AWS with official AL2023 + cloud-init.
- Route-only timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- Stable-IP timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- Client recovery timing: ready after cloud-init installs the agent/LoxiLB path successfully.
- AMI boot-to-ready timing: blocked until a dev AMI exists.

Record cloud-init boot timings now if using a development AMI or bootstrap path. Repeat after a real AMI exists.
