# BetterNAT Alpha Bootstrap Release Path

Date: 2026-06-23

## Purpose

The current open-source alpha can ship before a published BetterNAT AMI exists.

This document defines the temporary bootstrap-based release path for `v0.1.0-alpha.2`.

Production should still move to a prebuilt AMI once the runtime body is stable, but the current public alpha intentionally does not publish a BetterNAT AMI.

## Release Shape

Alpha bootstrap path:

1. Build release artifacts with `scripts/release-build.sh`.
2. Publish the release artifacts to GitHub Releases for public alpha users.
3. Provide Terraform with `betternat_version`; the provider derives the
   matching agent/CLI GitHub Release artifact URLs and SHA256 checksums from its
   built-in release manifest.
4. Optional private or unreleased test builds may still override:
   - `agent_binary_url`,
   - `agent_binary_sha256`,
   - `cli_binary_url`,
   - `cli_binary_sha256`,
   - `loxicmd_binary_url`,
   - `loxicmd_binary_sha256`.
5. Terraform launches an explicit user/provider-selected Linux AMI.
6. Cloud-init downloads and verifies the agent and CLI.
7. Cloud-init applies the baseline BetterNAT sysctl profile.
8. Cloud-init starts LoxiLB in Docker.
9. Cloud-init writes `/etc/betternat/agent.json`.
10. Cloud-init installs and starts `betternat-agent.service`.

This is acceptable for alpha because it avoids maintaining a premature AMI pipeline while preserving a reproducible test/install path.

## Build Artifacts

Build:

```sh
BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-build.sh
```

Output:

```text
tmp/release/v0.1.0-alpha.2/
  betternat_<version>_<host-os>_<host-arch>
  betternat_<version>_linux_arm64
  betternat_<version>_linux_amd64
  terraform-provider-betternat_<version>_<host-os>_<host-arch>
  betternat-agent_<version>_linux_arm64
  betternat-agent_<version>_linux_amd64
  SHA256SUMS
  manifest.json
```

For AWS supplemental tests on `t4g.small`, use:

```text
betternat-agent_<version>_linux_arm64
betternat_<version>_linux_arm64
```

## Artifact Hosting

The public release path uses GitHub Release assets.

BetterNAT does not provide a user-facing S3 artifact bucket, and users should not need to create one for the public alpha Quick Start.

Expected public URLs:

```text
https://github.com/nowakeai/betternat/releases/download/<version>/betternat-agent_<version>_linux_arm64
https://github.com/nowakeai/betternat/releases/download/<version>/betternat_<version>_linux_arm64
https://github.com/nowakeai/betternat/releases/download/<version>/SHA256SUMS
```

The provider's built-in release manifest must match the exact uploaded files.
When updating supported runtime versions, calculate SHAs from `SHA256SUMS`:

```sh
awk '$2 == "betternat-agent_v0.1.0-alpha.2_linux_arm64" {print $1}' SHA256SUMS
awk '$2 == "betternat_v0.1.0-alpha.2_linux_arm64" {print $1}' SHA256SUMS
```

Internal pre-release AWS tests may still use a temporary private S3 bucket and presigned URLs because the binaries are not yet public release assets. That is a maintainer testing transport, not the user-facing distribution path.

## Terraform Inputs

The supplemental fixture supports the public path:

```hcl
betternat_version = var.betternat_version
```

The default provider path uses:

```hcl
bootstrap_mode = "cloud_init"
```

Use this for ordinary Linux AMIs. In this mode the launch template keeps
per-node auto-assigned public IPv4 enabled so new gateway nodes can download
packages, pull the LoxiLB image, fetch BetterNAT release artifacts, join SSM,
and call AWS APIs before any node owns the shared EIP.

Private BetterNAT AMIs that already include the runtime can opt into:

```hcl
bootstrap_mode = "prebaked_ami"
```

In `prebaked_ami` mode, user data writes `/etc/betternat/agent.json`, reapplies
baseline sysctls, starts preinstalled `loxilb.service`, and restarts or enables
`betternat-agent.service`. With `stable_egress_ip=true`, the provider disables
per-node auto-assigned public IPv4 because bootstrap downloads are not needed.
With `stable_egress_ip=false`, per-node public IPv4 remains enabled because the
active gateway node's public IP is the egress identity.

Operators can explicitly set `associate_public_ip_address` to override the
derived launch-template value. Leave it unset for the provider defaults above.
Only disable it in `cloud_init` mode when the VPC provides another path for
package repositories, Docker image pull, BetterNAT release artifacts, AWS APIs,
and any management channel you expect to use.

and private test-build overrides:

```hcl
agent_binary_url      = var.agent_binary_url
agent_binary_sha256   = var.agent_binary_sha256
cli_binary_url        = var.cli_binary_url
cli_binary_sha256     = var.cli_binary_sha256
loxicmd_binary_url    = var.loxicmd_binary_url
loxicmd_binary_sha256 = var.loxicmd_binary_sha256
```

If `loxicmd_binary_url` is empty, cloud-init creates a host wrapper:

```sh
loxicmd -> docker exec loxilb loxicmd
```

## Cloud-Init Responsibilities

The generated bootstrap script:

- installs `curl` and Docker if missing,
- downloads `betternat-agent`,
- verifies `agent_binary_sha256` when provided,
- downloads `betternat`,
- verifies `cli_binary_sha256` when provided,
- optionally downloads and verifies `loxicmd`,
- writes `/etc/betternat/agent.json` with `0600` permissions,
- enables IP forwarding and baseline gateway sysctls,
- starts LoxiLB container with host networking,
- installs `betternat-agent.service`,
- enables and starts the agent.

## Baseline Sysctl Profile

The current cloud-init bootstrap writes `/etc/sysctl.d/99-betternat.conf` and applies it with `sysctl --system`.

Always-written settings:

```text
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
```

After `sysctl --system`, bootstrap also disables reverse-path filtering on every already-existing IPv4 interface:

```sh
for rp_filter in /proc/sys/net/ipv4/conf/*/rp_filter; do
  [ -e "$rp_filter" ] && echo 0 > "$rp_filter"
done
```

Conditionally written when `/proc/sys/net/netfilter/nf_conntrack_max` exists:

```text
net.netfilter.nf_conntrack_max = 1048576
```

Interpretation:

- `ip_forward` is required for the appliance to route private-subnet traffic.
- `rp_filter=0` avoids reverse-path filtering breaking asymmetric or takeover-related forwarding paths. `all/default` covers policy and future interfaces; the interface sweep covers interfaces that already existed before the sysctl file was applied.
- `nf_conntrack_max=1048576` gives the nftables fallback and any Linux-netfilter-conntrack-dependent host path a larger baseline connection table.

Important distinction:

- LoxiLB has its own eBPF conntrack state and is inspected with `loxicmd get conntrack -o json`.
- Linux `nf_conntrack_max` is not the primary LoxiLB NAT capacity knob.
- This setting is retained for fallback/compatibility and is applied only when the kernel exposes it.

Not included yet:

- `nf_conntrack_buckets` / module hashsize tuning,
- conntrack TCP/UDP timeout profile,
- ephemeral port range tuning,
- `somaxconn` / backlog tuning,
- IRQ/RSS/queue tuning,
- instance-family-specific ENA tuning,
- benchmark-derived profiles by instance size.

For `v0.1.0-alpha.2`, this is a conservative baseline, not a high-volume performance claim.

## Security Notes

- Use GitHub Release assets for public alpha users.
- Use short-lived presigned URLs only for maintainer pre-release AWS tests.
- Do not commit URLs or Terraform variable files containing URLs.
- Always provide `agent_binary_sha256` in release-candidate tests.
- Always provide `cli_binary_sha256` in release-candidate tests.
- Delete temporary artifact buckets after internal tests if S3 is used for unreleased binaries.

## Known Limitations

- First boot depends on package repositories, Docker pull, and artifact URL reachability.
- Boot-to-ready timing is not representative of a prebuilt AMI.
- LoxiLB image is still pulled at boot unless a future AMI preloads it. The alpha default image is pinned by digest to avoid `latest` drift.
- This path is for alpha and test environments, not the final production recommendation.
- Kernel/NIC tuning is intentionally minimal in the first alpha. Advanced tuning should be added only with repeatable benchmark evidence.
- Gateway nodes default to ordinary auto-assigned public IPv4 for bootstrap and
  management/control-plane egress. The shared EIP in stable mode is the
  private-workload egress identity, not the only public address a gateway node
  may have.
- AWS associates only one public IPv4 identity with a given private IPv4 at a
  time. If production-preview requires a gateway node to keep its management
  public IPv4 while also owning the shared egress EIP, BetterNAT should move the
  shared EIP to a secondary private IP or secondary ENI and configure LoxiLB
  SNAT to that egress identity.

## Exit Criteria

Before using this path for `v0.1.0-alpha.2`:

- [x] release artifact build succeeds,
- [x] `SHA256SUMS` contains the Linux arm64 agent,
- [x] `SHA256SUMS` contains the Linux arm64 CLI,
- [x] GitHub Release contains the Linux arm64 agent,
- [x] GitHub Release contains the Linux arm64 CLI,
- [x] GitHub Release contains `SHA256SUMS`,
- [x] Terraform validates with `agent_binary_sha256` and `cli_binary_sha256`,
- [x] AWS supplemental apply uses the checksum,
- [x] cloud-init verifies both checksums successfully,
- [x] cloud-init applies `/etc/sysctl.d/99-betternat.conf`,
- [x] agent service starts,
- [x] LoxiLB datapath becomes ready,
- [x] private client egress works,
- [x] destroy and residual cleanup pass.

Evidence is recorded in:

- `docs/release/RELEASE_CHECKLIST.md`
- `docs/research/035-p0-open-source-release-acceptance-results.md`
- `docs/research/037-v0.1.0-alpha-aws-release-candidate-results.md`

Final alpha6 Registry validation on 2026-06-24 created and destroyed a
disposable AWS environment with provider `0.1.0-alpha.6` and runtime
`v0.1.0-alpha.2`. Apply, active gateway bootstrap, private client egress,
handover, and destroy passed, but the standby gateway required a manually
attached temporary public IP to finish non-AMI bootstrap. The provider-derived
install plan now defaults gateway nodes to auto-assigned public IPv4; repeat the
AWS validation before considering this bootstrap topology production-preview
ready.
