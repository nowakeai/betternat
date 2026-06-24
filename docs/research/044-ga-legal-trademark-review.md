# GA Legal And Trademark Wording Review

Date: 2026-06-24

## Scope

This review covers public BetterNAT repository wording for:

- BetterNAT name and package names,
- LoxiLB and NetLOX attribution,
- AWS service references,
- Terraform and HashiCorp references,
- OpenTofu references,
- Prometheus and CNCF/Linux Foundation references,
- Grafana references,
- third-party notices and release-note disclaimers.

This is an engineering release-readiness review. It is not legal advice and does
not replace formal counsel review before paid distribution, marketplace
publication, public AMIs, or commercial co-marketing.

## Official Sources Checked

- AWS Trademark Guidelines: https://aws.amazon.com/trademark-guidelines/
- AWS Site Terms trademark language: https://aws.amazon.com/terms/
- HashiCorp Trademark Policy: https://www.hashicorp.com/en/trademark-policy
- OpenTofu Registry policy, which points project policies to LF Projects:
  https://github.com/opentofu/registry/blob/main/POLICY.md
- OpenTofu docs footer, which points trademark policy to LF Projects:
  https://opentofu.org/docs/v1.7/
- Linux Foundation trademark page:
  https://www.linuxfoundation.org/legal/trademarks
- CNCF Prometheus project page:
  https://www.cncf.io/projects/prometheus/
- CNCF brand guidelines, which point trademark usage to Linux Foundation
  guidelines: https://www.cncf.io/brand-guidelines/
- Grafana Labs trademark policy:
  https://grafana.com/trademark-policy/
- Grafana Labs trademark list:
  https://grafana.com/trademark-policy/trademark-list/
- LoxiLB upstream repository and license:
  https://github.com/loxilb-io/loxilb

## Repository Wording Review

Search terms reviewed:

```text
BetterNAT
LoxiLB
NetLOX
AWS
Amazon
Terraform
HashiCorp
OpenTofu
Prometheus
Grafana
endorsed
sponsored
partner
powered by
certified
official
affiliated
trademark
license
Apache
NOTICE
```

Reviewed files:

- `README.md`
- `docs/`
- `SECURITY.md`
- `THIRD_PARTY_NOTICES.md`
- `LICENSE`
- `.github/`

Result:

- No claim was found that BetterNAT is endorsed, sponsored, certified, or
  officially partnered with AWS, Amazon, HashiCorp, Terraform, OpenTofu, Linux
  Foundation, CNCF, Prometheus, Grafana Labs, NetLOX, or LoxiLB.
- No third-party logos are shipped or used in the top-level docs.
- Third-party names are used descriptively to identify APIs, tools,
  compatibility, or runtime dependencies.
- `THIRD_PARTY_NOTICES.md` already states that third-party names may be
  trademarks of their owners and that BetterNAT is not affiliated with,
  endorsed by, or sponsored by those projects or companies.
- Runtime release notes state that BetterNAT integrates LoxiLB as a third-party
  datapath component and does not imply NetLOX/LoxiLB endorsement, partnership,
  certification, or official support.
- BetterNAT's own license is Apache License 2.0.

## LoxiLB And NetLOX

Current wording describes LoxiLB as:

- a third-party datapath dependency,
- primary local datapath runtime for egress SNAT,
- Apache License 2.0,
- copyright NetLOX Inc. and contributors,
- pinned by version and digest in release metadata.

Review result:

- Current wording is acceptable for alpha/GA documentation because it is
  descriptive and avoids endorsement/partnership claims.
- Public AMI or bundled binary distribution must continue preserving LoxiLB
  license text and any upstream NOTICE file if present.
- Do not use LoxiLB or NetLOX marks in BetterNAT product names, logos, badges,
  marketplace titles, or co-marketing without explicit permission.

## AWS

Current wording uses AWS and AWS service names to describe:

- AWS-only current target,
- AWS APIs used by the provider/agent,
- AWS NAT Gateway cost comparison,
- AWS Systems Manager, EC2, Auto Scaling, DynamoDB, EIP, VPC, and IMDS behavior.

Review result:

- Current wording is descriptive and does not imply AWS sponsorship.
- No AWS logos or partner badges are used.
- Avoid "AWS certified", "AWS partner", "available in AWS Marketplace", or
  "Buy with AWS" language unless the project actually enters those programs and
  follows AWS program guidelines.

## Terraform And HashiCorp

Current wording uses Terraform to describe:

- the provider installation path,
- Terraform Registry distribution,
- Terraform CLI examples,
- Terraform state/replacement behavior.

Review result:

- Current wording is descriptive and does not imply HashiCorp endorsement.
- The provider repository name follows the Terraform provider naming convention
  `terraform-provider-betternat`.
- Avoid implying HashiCorp support or certification. If any future copy says
  "official Terraform provider", make clear it is the official BetterNAT
  provider, not an official HashiCorp provider.

## OpenTofu

Current wording uses OpenTofu to describe:

- registry compatibility,
- provider source-address notes,
- OpenTofu install validation status.

Review result:

- Current wording is descriptive and does not imply OpenTofu/LF Projects
  endorsement.
- OpenTofu Registry sync status is now treated as non-blocking when Terraform
  Registry has synced, per the current release process.
- Avoid using OpenTofu logos or co-marketing language without following LF
  Projects trademark policy.

## Prometheus And Grafana

Current wording uses Prometheus to describe:

- the metrics endpoint format,
- scrape examples,
- alerting examples.

Current wording uses Grafana mostly for optional starter dashboard/query
context.

Review result:

- Prometheus usage is descriptive and tied to metrics compatibility.
- Grafana usage is descriptive and optional; no Grafana logos or modified
  Grafana distribution are shipped.
- If BetterNAT later ships a Grafana dashboard bundle, keep it as a dashboard
  artifact for use with Grafana, not a Grafana-branded product, and retain a
  clear non-affiliation notice.

## Product Name And Package Names

Reviewed names:

- `BetterNAT`
- `betternat`
- `betternat-agent`
- `terraform-provider-betternat`
- Terraform provider source `nowakeai/betternat`

Review result:

- No collision was identified in the repository wording review.
- This review does not include a formal trademark search or legal clearance for
  the BetterNAT name.
- Formal clearance is still required before heavy brand investment,
  marketplace publication, or paid product launch.

## Release Gate Decision

For the current GA checklist:

- Legal/trademark engineering wording review: complete.
- No release-blocking wording issue was found in current public docs.
- Formal legal counsel review remains outside this engineering checklist and
  should happen before paid distribution, marketplace publication, public AMIs,
  or co-marketing.

Follow-up hardening:

1. Add exact third-party license bundles if AMIs or container images include
   LoxiLB, `loxicmd`, OS packages, or dashboard assets.
2. Do a formal BetterNAT name clearance before GA marketing spend.
3. Re-review marketplace-specific wording if AWS Marketplace publication is
   added later.
4. Re-review Grafana-specific attribution if BetterNAT ships Grafana dashboards
   as packaged artifacts.
