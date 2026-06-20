# AWS Integration Test Results

Date: 2026-06-20

## Summary

BetterNAT's AWS appliance pattern passed the most important AWS-only functional tests in an isolated `us-west-2a` VPC:

- public-subnet LoxiLB appliance with AWS source/destination check disabled,
- private subnet default route to the appliance instance,
- LoxiLB egress SNAT rule for the private subnet,
- private client egress to the internet,
- stable public source IP through an associated EIP,
- manual route/EIP failover from active appliance to standby appliance,
- post-failover private client egress using the same EIP,
- LoxiLB counters and conntrack visibility,
- DynamoDB conditional write/fencing behavior,
- full cleanup of tagged AWS resources.

The test did not validate an autonomous `betternat-agent` HA loop. The failover was performed manually with AWS APIs to prove the AWS primitives and datapath behavior.

## Test Scope

Run ID:

```text
betternat-aws-20260620T064000Z
```

Region and AZ:

```text
Region: us-west-2
AZ: us-west-2a
```

Topology:

```text
VPC: 10.77.0.0/16
Public subnet: 10.77.1.0/24
Private subnet: 10.77.2.0/24
Active appliance private IP: 10.77.1.70
Standby appliance private IP: 10.77.1.148
Initial private client IP: 10.77.2.179
Post-failover private client IP: 10.77.2.138
EIP: 44.239.101.52
```

Instances were launched as Spot instances.

## Preflight

Confirmed:

- target AWS identity resolved under the intended profile,
- target account was `601427795217`,
- `us-west-2a` was available,
- no pre-existing BetterNAT AWS integration VPC was found before the run.

## Single-Appliance Datapath

Active appliance:

```text
Instance: i-00008057ca7a01161
Private IP: 10.77.1.70
Public EIP: 44.239.101.52
SourceDestCheck: false
```

Private route table:

```text
0.0.0.0/0 -> i-00008057ca7a01161
```

LoxiLB on the active appliance was ready:

```json
{
  "buildInfo": "2026_06_19_08h:28m-nogit",
  "version": "0.9.8.6-beta"
}
```

Active LoxiLB SNAT rule:

```text
sourceIP: 10.77.2.0/24
destinationIP: 0.0.0.0/0
preference: 100
doSnat: true
toIP: 10.77.1.70
onDefault: true
```

Before the LoxiLB rule was applied, the private client repeatedly timed out when calling:

```sh
curl -4 https://checkip.amazonaws.com
curl -4 -I https://example.com
```

After the rule was applied, the private client succeeded:

```text
checkip: 44.239.101.52
example.com: HTTP/2 200
```

This proves the route-through appliance pattern works in AWS with LoxiLB on EC2.

## Manual Failover

Standby appliance:

```text
Instance: i-0c0052789044a570f
Private IP: 10.77.1.148
SourceDestCheck: false
```

Standby LoxiLB was ready and accepted the SNAT rule:

```text
sourceIP: 10.77.2.0/24
destinationIP: 0.0.0.0/0
preference: 100
doSnat: true
toIP: 10.77.1.148
onDefault: true
```

Manual control-plane switch:

```text
Start: 2026-06-20T06:55:51Z
End:   2026-06-20T06:55:55Z
```

Operations:

```text
AssociateAddress(EIP -> standby appliance)
ReplaceRoute(0.0.0.0/0 -> standby appliance)
```

Post-switch AWS state:

```text
EIP 44.239.101.52 -> i-0c0052789044a570f / 10.77.1.148
Private route 0.0.0.0/0 -> i-0c0052789044a570f
```

Post-failover private client:

```text
Instance: i-0b5715da502be3e49
Private IP: 10.77.2.138
```

The post-failover client immediately observed the same public IP:

```text
checkip: 44.239.101.52
example.com: HTTP/2 200
```

The client repeated the result multiple times.

This proves that the AWS primitives can preserve egress public IP across route/EIP failover for new flows.

## Workload Smoke

On the post-failover private client, SSM became available through the BetterNAT path. Direct workload smoke passed:

```text
curl https://checkip.amazonaws.com -> 44.239.101.52
curl -I https://example.com -> HTTP/2 200
getent hosts example.com -> IPv6 records returned
10 parallel curl downloads from example.com -> completed
```

The DNS command used `getent hosts`, which returned IPv6 records for `example.com`. The HTTP tests used IPv4 egress.

## LoxiLB Observability

Standby firewall counter after workload:

```text
counter: 15461 packets / 147549023 bytes
```

LoxiLB conntrack included both TCP and UDP/NTP flows.

Examples:

```text
TCP established SNAT:
sourceIP=10.77.2.138 destinationPort=443 conntrackState=est
conntrackAct=snat-10.77.1.148:<port>:w0

UDP established SNAT:
sourceIP=10.77.2.138 destinationPort=123 conntrackState=udp-est
conntrackAct=snat-10.77.1.148:<port>:w0

Reverse DNAT:
destinationIP=10.77.1.148 conntrackAct=dnat-10.77.2.138:<port>:w0
```

This supports the plan for BetterNAT to poll LoxiLB state and re-export normalized metrics.

## DynamoDB Lease / Fencing

Created a temporary PAY_PER_REQUEST table:

```text
betternat-aws-20260620T064000Z-leases
```

Validated:

- first owner `PutItem` with `attribute_not_exists(ha_group_id)` succeeded,
- second owner `PutItem` with the same condition failed with `ConditionalCheckFailedException`,
- current owner generation update succeeded,
- stale owner generation update failed with `ConditionalCheckFailedException`.

Final item:

```json
{
  "ha_group_id": "prod-egress-us-west-2a",
  "owner_instance_id": "i-00008057ca7a01161",
  "generation": 2,
  "expires_at": 1790000000
}
```

This validates the core DynamoDB primitive needed for BetterNAT lease/fencing.

## Cleanup Result

Cleanup completed.

Deleted or verified removed:

- EC2 instances,
- Spot instance requests,
- EIP,
- EBS volumes,
- ENIs,
- security groups,
- route tables,
- subnets,
- internet gateway,
- VPC,
- DynamoDB table,
- IAM role and instance profile.

Verification:

```text
VPCs with run tag: none
EIPs with run tag: none
Volumes with run tag: none
ENIs with run tag: none
DynamoDB table: ResourceNotFoundException
IAM role: NoSuchEntity
Spot requests: closed / instance-terminated-by-user
```

Terminated instance records still appear in EC2 history, which is expected and non-billable:

```text
i-05aee8c10b00361e9 terminated
i-00008057ca7a01161 terminated
i-0c0052789044a570f terminated
i-0b5715da502be3e49 terminated
```

## Gaps

Not covered in this run:

- autonomous `betternat-agent` active/standby loop,
- automatic failure detection timing,
- long-lived connection preservation across failover,
- high-throughput benchmark under ENA/Nitro load,
- pod-level attribution from EKS,
- IAM least-privilege policy enforcement,
- cross-AZ failover,
- full Terraform provider create/update/delete lifecycle.

Important nuance:

- The manual EIP + route switch completed in about four seconds from the CLI command sequence, but this is not an end-to-end HA SLO. Real SLO must include failure detection, lease acquisition, AWS API retries, route/EIP verification, and client recovery measurement.

## Decision Impact

The core AWS design remains viable:

- LoxiLB works on EC2 where it failed in the local OrbStack VM.
- AWS private route to instance works for BetterNAT's appliance model.
- Shared EIP plus `ReplaceRoute` can preserve public egress IP for new flows after failover.
- DynamoDB conditional writes are suitable for the lease/fencing primitive.
- LoxiLB counters and conntrack are good enough to justify building BetterNAT's normalized metrics exporter.

Next implementation focus should be:

1. productize the AWS test harness into a repeatable integration suite,
2. wire real `betternat-agent` to DynamoDB + AWS SDK + LoxiLB reconciliation,
3. run the same AWS test through the agent instead of manual CLI/API calls,
4. add full cleanup verification as a non-optional gate.
