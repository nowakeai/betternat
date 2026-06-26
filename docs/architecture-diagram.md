# BetterNAT Architecture Diagrams

Date: 2026-06-20

These diagrams show how BetterNAT, LoxiLB, and AWS work together.

BetterNAT-specific flows are shown as Mermaid diagrams so they stay versioned with this repository. For LoxiLB's upstream architecture and product visuals, see the LoxiLB project overview image and docs:

[![LoxiLB overview](https://github.com/loxilb-io/loxilb/assets/75648333/87da0183-1a65-493f-b6fe-5bc738ba5468)](https://github.com/loxilb-io/loxilb)

- [LoxiLB architecture in brief](https://github.com/loxilb-io/loxilbdocs/blob/main/docs/arch.md)
- [LoxiLB standalone mode](https://github.com/loxilb-io/loxilbdocs/blob/main/docs/standalone.md)

In BetterNAT, LoxiLB is the local datapath on each appliance. BetterNAT owns Terraform UX, AWS route/EIP failover, DynamoDB lease/fencing, rollback, and normalized observability.

## 1. Deployment Topology

```mermaid
flowchart TB
  internet((Internet))

  subgraph aws[AWS account / VPC]
    eip[Shared EIP]
    igw[Internet Gateway]

    subgraph public[Public subnet]
      active[BetterNAT appliance A<br/>EC2 instance<br/>betternat-agent<br/>LoxiLB]
      standby[BetterNAT appliance B<br/>EC2 instance<br/>betternat-agent<br/>LoxiLB]
    end

    subgraph private[Private subnets]
      rt[Private route table<br/>0.0.0.0/0 -> active appliance]
      vm[VM workloads]
      eks[EKS nodes / pods]
      crawler[Crawler / RPC / ingest workers]
    end

    ddb[(DynamoDB lease table)]
    cw[(CloudWatch / optional logs)]
  end

  vm --> rt
  eks --> rt
  crawler --> rt
  rt --> active
  active --> eip
  standby -. standby, ready but not route target .-> eip
  eip --> igw
  igw --> internet

  active <--> ddb
  standby <--> ddb
  active -. metrics/logs .-> cw
  standby -. metrics/logs .-> cw
```

Key point:

- AWS route tables decide which appliance receives private-subnet egress traffic.
- AWS EIP decides the public source IP.
- LoxiLB only handles local packet forwarding/NAT on the appliance.
- BetterNAT owns AWS failover and runtime reconciliation.

## 2. Component Responsibilities

```mermaid
flowchart LR
  user[User / Platform team]
  tf[terraform-provider-betternat]
  awsapi[AWS APIs]
  agent[betternat-agent]
  loxi[LoxiLB<br/>local datapath]
  nft[nftables<br/>legacy diagnostics]
  prom[Prometheus / Grafana]
  ddb[(DynamoDB lease)]
  rt[AWS route tables]
  eip[AWS EIP]

  user -->|terraform apply| tf
  tf -->|create EC2 / IAM / SG / EIP / DDB / initial route| awsapi
  tf -->|render config| agent

  agent -->|lease acquire / renew| ddb
  agent -->|AssociateAddress| eip
  agent -->|ReplaceRoute| rt
  agent -->|apply/reconcile SNAT rule| loxi
  agent -->|legacy diagnostics only| nft
  agent -->|export metrics| prom

  loxi -->|firewall counters / conntrack| agent
  nft -->|counters / conntrack| agent
```

Ownership boundary:

| Layer | Owner |
| --- | --- |
| Terraform state and install lifecycle | `terraform-provider-betternat` |
| Runtime HA, lease, AWS failover | `betternat-agent` |
| Primary packet datapath | LoxiLB |
| Product fallback datapath | None; LoxiLB readiness is a release gate |
| Legacy diagnostics while retained | nftables/nf_conntrack |
| Metrics normalization | `betternat-agent` |
| Cloud primitives | AWS APIs |

## 3. Data Plane

```mermaid
flowchart LR
  src[Private workload<br/>10.0.10.25]
  rt[Private route table<br/>0.0.0.0/0 -> appliance]
  eni[Active appliance ENI<br/>source/dest check disabled]
  loxi[LoxiLB egress firewall rule<br/>sourceIP:10.0.0.0/8<br/>SNAT -> appliance private IP]
  eip[EIP<br/>stable public source IP]
  dst[Internet destination]

  src -->|packet to internet| rt
  rt --> eni
  eni --> loxi
  loxi -->|SNAT + conntrack| eip
  eip --> dst
  dst -->|return packet| eip
  eip --> loxi
  loxi -->|DNAT via conntrack| eni
  eni --> src
```

Validated LoxiLB rule shape:

```sh
loxicmd create firewall \
  --firewallRule=sourceIP:<private-cidr>,preference:<priority> \
  --snat=<appliance-private-ip> \
  --egress
```

## 4. Control Plane Calls

```mermaid
sequenceDiagram
  participant TF as terraform-provider-betternat
  participant AWS as AWS APIs
  participant A as betternat-agent A
  participant B as betternat-agent B
  participant LoxiA as LoxiLB A
  participant LoxiB as LoxiLB B
  participant DDB as DynamoDB lease

  TF->>AWS: Create EC2/IAM/SG/EIP/DynamoDB/routes
  TF->>A: Render agent config
  TF->>B: Render agent config

  A->>LoxiA: Reconcile egress SNAT rules
  B->>LoxiB: Reconcile egress SNAT rules

  A->>DDB: Acquire/renew active lease
  A->>AWS: Verify EIP and route target
  B->>DDB: Observe lease
  B->>AWS: Stay ready, no mutation
```

## 5. Failover Sequence

```mermaid
sequenceDiagram
  participant Workload as Private workload
  participant AWS as AWS route/EIP APIs
  participant Active as Agent A / old active
  participant Standby as Agent B / standby
  participant LoxiB as LoxiLB B
  participant DDB as DynamoDB lease

  Workload->>Active: Existing/new egress flows
  Active--xStandby: Health checks fail or lease expires

  Standby->>DDB: Conditional acquire lease, generation++
  Standby->>LoxiB: Ensure/reconcile SNAT rule
  Standby->>AWS: AssociateAddress(shared EIP -> B)
  Standby->>AWS: ReplaceRoute(0.0.0.0/0 -> B)
  Standby->>AWS: Verify EIP owner and route target
  Standby->>Workload: Outbound probe sees expected EIP
  Standby->>DDB: Verify generation still current
  Standby->>Standby: Become ACTIVE

  Workload->>Standby: New connections use same EIP
```

v0 failover contract:

- New connections recover through the standby appliance.
- Public egress IP remains stable when shared EIP mode is enabled.
- Active connection preservation is not promised.

## 6. Runtime Reconciliation Loop

```mermaid
flowchart TD
  tick[Timer / boot / failover event]
  readCfg[Read desired config]
  checkLoxi[Check LoxiLB ready]
  getFW[Read LoxiLB firewall rules]
  diff[Compare desired vs actual]
  apply[Create missing SNAT rules]
  counters[Read counters + conntrack]
  export[Export BetterNAT metrics]
  degraded[Mark datapath degraded]

  tick --> readCfg
  readCfg --> checkLoxi
  checkLoxi -->|ready| getFW
  checkLoxi -->|error| degraded
  getFW --> diff
  diff -->|missing rule| apply
  diff -->|in sync| counters
  apply --> counters
  counters --> export
```

Reason this loop is required:

- In the spike, LoxiLB firewall rules disappeared after `docker restart loxilb`.
- BetterNAT must treat LoxiLB runtime config as ephemeral.
- Desired state belongs to `betternat-agent` config and is continuously reconciled.
