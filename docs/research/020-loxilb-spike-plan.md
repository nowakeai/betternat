# LoxiLB Spike Execution Plan

Date: 2026-06-19

## Goal

Validate whether standalone LoxiLB can act as BetterNAT's datapath engine for a generic AWS private-subnet NAT Gateway replacement use case.

The test must answer:

- Can a private EC2 instance route internet-bound traffic through a standalone LoxiLB appliance?
- Does outbound traffic SNAT to the expected EIP/public IP?
- Does return traffic work for TCP and UDP?
- Can LoxiLB expose useful metrics/API data for BetterNAT observability?
- Does this mode require Kubernetes, kube-loxilb, or broad AWS permissions?

## AWS Safety Constraints

Use only the provided profile and region/AZ:

```text
AWS profile: set `AWS_PROFILE` to a disposable test account profile before running the spike.
Region: us-west-2
AZ: us-west-2a
```

Use a unique spike tag on every created resource:

```text
Project=BetterNAT
Purpose=LoxiLBSpike
SpikeId=betternat-loxilb-20260619T154348Z
Owner=Codex
```

Do not modify any existing VPC, subnet, route table, EIP, security group, IAM role, or instance.

Use Spot instances for EC2.

Clean up all created resources after the test.

If network access from the local machine is unreliable, use:

```text
HTTP_PROXY=<optional local proxy>
HTTPS_PROXY=<optional local proxy>
```

## Proposed Isolated Topology

```text
VPC: 10.77.0.0/16

Public subnet: 10.77.1.0/24, us-west-2a
  - betternat-loxilb appliance
  - public IP / EIP
  - source/destination check disabled

Private subnet: 10.77.2.0/24, us-west-2a
  - test client instance
  - default route 0.0.0.0/0 -> LoxiLB appliance ENI or instance

Internet gateway:
  - attached to spike VPC

Route tables:
  - public route table: 0.0.0.0/0 -> IGW
  - private route table: 0.0.0.0/0 -> LoxiLB target
```

## Preferred Access Model

Use SSM Session Manager where possible. For this spike, SSH ingress can be avoided.

Both instances should have:

- Amazon Linux 2023 or Ubuntu LTS,
- SSM agent available/running,
- IAM instance profile with `AmazonSSMManagedInstanceCore`,
- outbound internet path.

The public LoxiLB appliance has direct public-subnet egress.

The private test instance uses LoxiLB for outbound egress after route setup.

## Instance Sizing

Use small Spot instances for functional validation:

```text
LoxiLB appliance: t3.small or t3.medium spot
Private test client: t3.micro or t3.small spot
```

This spike is not a performance benchmark.

## Test Steps

### 1. Create isolated AWS resources

- VPC.
- Internet gateway.
- Public subnet.
- Private subnet.
- Public route table.
- Private route table.
- Security groups.
- IAM role/instance profile for SSM.
- Spot LoxiLB appliance instance.
- Spot private test instance.
- Disable source/destination check on LoxiLB appliance ENI/instance.

### 2. Install LoxiLB standalone

Try the least invasive supported standalone path:

- package install if available,
- or container mode if faster.

Record:

- install command,
- version,
- whether kernel/eBPF prerequisites are met,
- exposed API/metrics ports.

### 3. Configure generic outbound NAT

Attempt to configure LoxiLB as an outbound SNAT gateway for:

```text
source CIDR: 10.77.2.0/24
egress interface: public interface
public identity: appliance public IP/EIP
```

If LoxiLB standalone cannot express this, record the limitation clearly.

### 4. Route private traffic through LoxiLB

Set private route table:

```text
0.0.0.0/0 -> LoxiLB appliance instance or ENI
```

### 5. Functional tests

From private test instance:

```sh
curl -4 https://checkip.amazonaws.com
curl -4 https://ifconfig.me
curl -I https://example.com
dig example.com
```

Expected:

- commands succeed,
- public echo endpoint sees LoxiLB public IP/EIP.

### 6. TCP/UDP tests

Run simple tests:

- TCP HTTPS download.
- UDP DNS query to a public resolver, if allowed.
- multiple concurrent curls.

Record:

- success/failure,
- observed source IP,
- errors/drops.

### 7. Metrics/API review

Check LoxiLB metrics/API:

- source IP counters,
- destination counters,
- connection state,
- NAT/conntrack stats,
- Prometheus endpoint availability.

### 8. HA boundary review

Do not build full HA in this spike. Instead answer:

- Can BetterNAT agent own route/EIP failover while LoxiLB only owns datapath?
- Does LoxiLB try to own AWS ENI/EIP lifecycle in standalone mode?
- What permissions would be needed if LoxiLB HA is enabled?

## Pass / Partial / Fail Criteria

### Pass

- Standalone LoxiLB runs without Kubernetes.
- Private instance can reach internet through LoxiLB.
- Return traffic works.
- Public source IP is expected.
- Multiple source flows work.
- LoxiLB exposes usable metrics/API.
- No broad AWS permissions are required for datapath-only mode.

### Partial

- LoxiLB works only with Kubernetes egress/service model.
- LoxiLB works only with broad AWS permissions.
- LoxiLB forwards traffic but does not expose useful attribution metrics.
- LoxiLB requires an HA model that conflicts with BetterNAT's agent.

### Fail

- Standalone generic outbound SNAT cannot be configured.
- Private VPC route-through appliance mode does not work.
- Return traffic fails.
- Operational/security requirements are incompatible.

## Cleanup Checklist

Delete only resources with:

```text
SpikeId=betternat-loxilb-20260619T154348Z
```

Cleanup order:

1. Restore/delete private route table route if needed.
2. Terminate Spot instances.
3. Release EIP, if allocated.
4. Delete instance profile role attachments.
5. Delete instance profile.
6. Delete IAM role/policies created for spike.
7. Delete security groups.
8. Delete route tables.
9. Delete subnets.
10. Detach/delete internet gateway.
11. Delete VPC.

Verification:

- `describe-instances` returns no non-terminated spike instances.
- no EIPs with spike tag remain.
- no ENIs with spike tag remain.
- no VPC/subnet/route table/security group/IAM resources with spike tag remain.

## Result Template

```text
SpikeId:
Date:
Region/AZ:
LoxiLB version:
Install mode:
Datapath mode:

Functional result:
TCP result:
UDP result:
Observed egress IP:
Metrics/API result:
AWS permissions required:
Operational notes:

Decision:
  pass | partial | fail

Recommended BetterNAT action:
  use loxilb engine | keep as optional integration | nftables only
```
