# Dependency Policy

Last updated: 2026-06-20

## Default Rule

Use current supported dependency versions unless there is a concrete reason not to.

BetterNAT should prefer mature libraries and official SDKs over custom implementations when they fit the product boundary.

## What To Prefer

- Official cloud SDKs for cloud API operations.
- Terraform Plugin Framework for Terraform provider behavior.
- LoxiLB for the primary datapath.
- LoxiLB as the supported datapath. nftables code is legacy/diagnostic only
  while retained and must not be expanded into a product fallback.
- Standard library packages for small, obvious logic.

## Before Adding A Dependency

Check:

- maintenance activity,
- license compatibility,
- security posture,
- transitive dependency footprint,
- compatibility with Go's supported versions,
- whether it improves product reliability, user experience, or operator safety.

Do not add a dependency only to avoid a small clear standard-library implementation.

## Upgrade Workflow

When network access is available:

```sh
./manage deps check
```

For actual upgrades:

```sh
./manage deps upgrade
./manage verify
```

If the latest version is not adopted, document the reason in the relevant doc or dev-log. Acceptable reasons include:

- upstream regression,
- incompatible API change,
- Terraform Plugin Framework version compatibility,
- cloud SDK behavior change,
- unacceptable transitive dependency or license change,
- production stability concerns.

## Network Constraints

Dependency freshness checks may require network access and may need a local proxy. The default local validation loop must not depend on network access.

If a dependency command fails because the sandbox cannot reach a local proxy, rerun with the appropriate approved network context or record that the freshness check could not be completed.

## Custom Networking Code

Custom datapath or cloud protocol implementations are high-risk. Prefer existing mature components first:

- use LoxiLB before custom eBPF datapath work,
- do not add a second fallback datapath by default; treat LoxiLB failures as
  product blockers or explicit architecture decisions,
- use AWS SDK before shelling out to `aws`,
- use provider interfaces and fakes for tests rather than mocking external binaries with fragile text fixtures.
