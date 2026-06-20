# Naming Collision Check

Date: 2026-06-19

## Question

Which candidate names are usable for a serious but slightly playful NAT Gateway replacement product, and which names have obvious collision risk?

## Scope

This is a practical first-pass search, not a legal trademark clearance.

Checked signals:

- obvious GitHub/open-source collisions,
- adjacent cloud/networking/software products,
- obvious company/product names,
- Terraform naming suitability,
- search clarity.

Not fully checked:

- USPTO/EUIPO/WIPO legal clearance,
- domain availability,
- all package registries,
- all Terraform Registry namespaces,
- corporate name conflicts in every jurisdiction.

Before launch, a real trademark/domain review is still required.

## Summary

Best current direction:

1. **BetterNAT**: clear, serious, slightly playful, and currently no obvious same-category collision found in first-pass search.
2. **NotNAT**: strongest clean-ish candidate from the NAT/not pun direction.
3. **NATwise**: usable if we want more serious and less cheeky, but less memorable.
4. **NATpilot / EgressPilot**: decent product names, but less unique and less NAT-specific.

Names to avoid or de-prioritize:

- **NATlas**: already used by a network discovery/auto-diagramming project.
- **Egressor**: close collision with a Kubernetes/eBPF data transfer intelligence project.
- **Gatewise**: existing access-control SaaS company.
- **NATcracker/Nutcracker**: collides with twemproxy/nutcracker and Netcracker telecom software brand.
- **NATZero**: too close to NetZero, which is crowded across internet access and climate/software.

## Candidate Review

| Name | Collision Risk | Notes | Recommendation |
| --- | --- | --- | --- |
| BetterNAT | Low to medium | No obvious exact same-category NAT Gateway product found in first-pass search. "better NAT" is a common phrase in NAT traversal/support discussions, so search uniqueness is moderate. | Strong candidate; needs trademark/domain check |
| NotNAT | Low to medium | Initial searches did not show an obvious NAT/networking product named NotNAT. There is unrelated `NOTNA SOFTWARE`. | Strong candidate; needs trademark/domain check |
| NATlas | High | Existing GitHub project `MJL85/natlas` for network discovery and auto-diagramming. Also close to ATLAS/RIPE Atlas. | Avoid/de-prioritize |
| NATcracker | High | Strong association with Nutcracker/twemproxy and Netcracker telecom software/trademarks. "cracker" also has security ambiguity. | Avoid |
| Nutcracker | High | Existing twemproxy nickname and many unrelated projects. | Avoid |
| Egressor | Medium-high | Search found an "Egressor" data transfer intelligence platform for Kubernetes using eBPF/Go. Very close category. | Avoid/de-prioritize |
| EgressPilot | Medium | No strong exact collision found in quick search, but "pilot" product names are crowded. | Possible |
| NATpilot | Medium | No obvious exact networking collision found, but many "pilot" names and `openpilot` ecosystem noise. | Possible |
| Gatewise | High | Existing Gatewise access-control SaaS and app. | Avoid |
| ClearNAT | Low-medium | No obvious exact collision in quick search, but "Clear" brands are crowded. | Possible but bland |
| NATwise | Medium | No obvious exact product found, but close to ConnectWise/Netwise/Thinkwise naming space. | Possible |
| NATZero | High | Too close to NetZero, a known internet/software brand, plus many net-zero climate products. | Avoid |
| NATless | Medium | Semantically risky because product still performs NAT; no strong exact collision in quick search. | Possible but confusing |

## Detailed Notes

### BetterNAT

Pros:

- Immediately understandable.
- Serious enough for enterprise use.
- Slightly playful without sounding unserious.
- More technically clear than `NotNAT`; it does not imply "this is not NAT."
- Terraform and CLI naming work well:

```hcl
resource "betternat_gateway" "egress" {}
```

```sh
betternat doctor
betternat top sources
betternat cost estimate
```

Cons:

- "better NAT" is a common descriptive phrase in NAT traversal, gaming, and support discussions.
- Less distinctive than coined names.
- Harder to own as a trademark because it is somewhat descriptive.

Current recommendation:

> Strong candidate. Better than `NotNAT` if we value clarity and enterprise tone over the NAT/not pun.

### NotNAT

Pros:

- Uses the NAT/not pun directly.
- Memorable.
- Product story is easy:
  - "Not NAT Gateway."
  - "Not a black box."
  - "Not another surprise bill."
- Terraform naming works:

```hcl
resource "notnat_gateway" "egress" {}
```

CLI works:

```sh
notnat doctor
notnat top sources
notnat cost estimate
```

Cons:

- May sound like "this is not NAT," which is technically confusing.
- Needs tagline to clarify:

> NotNAT is a self-owned egress gateway for cloud private networks.

Current recommendation:

> Best current candidate if the product wants a light pun without sounding unserious.

### NATlas

Pros:

- Strong meaning: NAT + atlas, egress map, traffic visibility.
- Professional and memorable.

Cons:

- Existing `MJL85/natlas` GitHub project is explicitly network discovery and auto-diagramming.
- "Atlas" is heavily used in software and networking measurement contexts, including RIPE Atlas.
- Could confuse search and GitHub discovery.

Current recommendation:

> De-prioritize despite being a good semantic fit.

### NATcracker / Nutcracker

Pros:

- Fun pun.
- "Crack open NAT costs" is a usable slogan.

Cons:

- `Nutcracker` is the well-known nickname for Twitter's `twemproxy`.
- Netcracker is an established telecom software company with trademarks.
- "cracker" has cybersecurity/offensive connotations.

Current recommendation:

> Avoid.

### Egressor / EgressPilot

`Egressor` is too close to an existing data-transfer/egress intelligence project found in search. That is an adjacent problem space, so it is not worth the collision risk.

`EgressPilot` is less risky but also less distinctive.

Current recommendation:

- Avoid `Egressor`.
- Keep `EgressPilot` as a backup.

### NATwise

Pros:

- Serious.
- Communicates smarter NAT decisions/cost optimization.
- Less jokey than NotNAT.

Cons:

- Less memorable.
- Close to broader "wise" software naming field: ConnectWise, Netwise, Thinkwise, EdgeWise.

Current recommendation:

> Usable backup if NotNAT feels too playful.

## Recommended Shortlist

### 1. BetterNAT

Best tagline:

> Better NAT for high-volume cloud egress.

More specific:

> BetterNAT is a low-cost, observable, highly available egress gateway for cloud private networks.

Resource naming:

```hcl
resource "betternat_gateway" "egress" {}
```

### 2. NotNAT

Best tagline:

> Not NAT Gateway. Not a black box. Not another surprise bill.

More serious tagline:

> NotNAT is a low-cost, observable, highly available egress gateway for cloud private networks.

Resource naming:

```hcl
resource "notnat_gateway" "egress" {}
```

### 3. NATwise

Tagline:

> Smarter cloud egress without the managed NAT surprise bill.

Resource naming:

```hcl
resource "natwise_gateway" "egress" {}
```

### 4. EgressPilot

Tagline:

> Steer private subnet egress with lower cost, clearer attribution, and automated failover.

Resource naming:

```hcl
resource "egresspilot_gateway" "egress" {}
```

## Naming Recommendation

Use **BetterNAT** as the working public name, subject to a deeper trademark/domain check.

Keep **NotNAT** only as a rejected/backup naming path. BetterNAT has clearer enterprise tone and avoids the technical confusion that "not NAT" could create.

Use this positioning:

> BetterNAT is a self-owned cloud egress gateway that cuts high-volume NAT processing charges, shows where traffic comes from, and fails over automatically.

Optional wordplay:

> Better NAT for high-volume cloud egress. Better not get surprised by your NAT Gateway bill.

Avoid overusing the joke. Keep it in one line, then be serious.

## Required Next Checks

Before final adoption:

- GitHub org/repo availability.
- Terraform provider namespace availability.
- Domain availability: `betternat.io`, `betternat.dev`, `betternat.cloud`.
- Domain availability: `notnat.io`, `notnat.dev`, `notnat.cloud`.
- Package names: Homebrew, Docker Hub/GHCR, npm if relevant.
- USPTO trademark search for software/SaaS classes.
- EUIPO/WIPO if commercial launch is expected.
- Search cloud marketplaces.

## Sources

- Existing `MJL85/natlas` GitHub project for network discovery and auto-diagramming: https://github.com/MJL85/natlas
- Reddit mention describing Natlas as network discovery and auto-diagramming: https://www.reddit.com/r/networking/comments/8sdlz7/network_automation_question/
- RIPE Atlas software probe context, showing Atlas is a crowded network-measurement term: https://labs.ripe.net/author/stephen_strowes/reviewing-ripe-atlas-software-probes/
- `twitter/twemproxy`, aka Nutcracker: https://github.com/twitter/twemproxy
- Wikimedia Nutcracker/twemproxy reference: https://wikitech.wikimedia.org/wiki/Nutcracker
- Netcracker company/product presence: https://www.netcracker.com/
- NETCRACKER trademark search result: https://www.trademarkia.com/netcracker-75224194
- Gatewise existing access-control SaaS: https://gatewise.com/
- Egressor adjacent Kubernetes/eBPF data transfer intelligence project mention: https://www.linkedin.com/posts/bamulligan_github-phonginreallifeegressor-egressor-activity-7422651169887518720-80-j
- NetZero existing internet/software and crowded brand space: https://www.netzero.net/
- USPTO trademark search starting point: https://www.uspto.gov/trademarks
- Search results for exact `"BetterNAT"` did not reveal an obvious same-category product in first-pass search; generic "better NAT" results are common in NAT traversal/support contexts.
