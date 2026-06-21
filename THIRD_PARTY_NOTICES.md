# Third-Party Notices

This file records third-party components that BetterNAT integrates with or may distribute in release artifacts.

The first BetterNAT release is free and open source. This notice is for attribution and license hygiene; it is not legal advice.

## LoxiLB

- Project: LoxiLB
- Upstream: https://github.com/loxilb-io/loxilb
- License: Apache License 2.0
- Copyright: NetLOX Inc. and contributors
- Use in BetterNAT: primary local datapath runtime for egress SNAT.
- Alpha image reference: `ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052`
- Platform manifests observed on 2026-06-21:
  - `linux/amd64`: `sha256:f435d5170eaf7cb13ec738a1ea5c82a943187b2fc6cae432539a304632a9febf`
  - `linux/arm64`: `sha256:70613f1f4c80427424f0563db51723e154feee0b11226addef3959bfd64c4eaf`

BetterNAT should preserve the LoxiLB license text and any upstream NOTICE file when bundling LoxiLB or `loxicmd` into AMIs or other release artifacts.

## nftables / conntrack tools

- Projects: nftables, conntrack-tools
- Use in BetterNAT: fallback datapath and diagnostics.
- Packaging source: operating system packages in the target AMI/base OS.

When distributing an AMI, include or reference the license information provided by the base operating system packages.

## Go Modules

BetterNAT uses Go modules listed in `go.mod` and `go.sum`.

Release artifacts should include a generated dependency inventory or SBOM before production release.

## AWS, Terraform, Prometheus, Grafana

BetterNAT integrates with AWS APIs, Terraform, Prometheus, and optionally Grafana.

Their names may be trademarks of their respective owners. BetterNAT is not affiliated with, endorsed by, or sponsored by those projects or companies unless explicitly stated in a future written agreement.
