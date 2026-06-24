# Alpha6 Final AWS Validation

Date: 2026-06-24

## Scope

- Provider: Terraform Registry `nowakeai/betternat` `0.1.0-alpha.6`
- Runtime: `betternat_version = "v0.1.0-alpha.2"`
- AWS profile: `601427795217_AdministratorAccess`
- Region: `us-west-2`
- AZ: `us-west-2a`
- Run ID: `bnat-alpha6-final-20260624105333`
- Fixture: isolated copy of `examples/terraform-aws-supplemental`
- Cost posture: Spot gateway nodes and Spot private client

## Results

Terraform apply succeeded from the public Registry provider path:

```text
Apply complete! Resources: 16 added, 0 changed, 0 destroyed.
```

The provider derived runtime artifact URLs and checksums from
`betternat_version`; no explicit artifact URL overrides were used.

Initial runtime state:

- active gateway: `i-02a936e084657e227`, private IP `10.88.1.118`, public EIP
  `184.34.128.79`
- standby gateway: `i-0d539fcbf045358f9`, private IP `10.88.1.205`, no public
  IP
- private client: `i-02ae1f34e358b3d8e`, private IP `10.88.11.231`

Active gateway checks passed after cloud-init completed:

- `betternat version`: `v0.1.0-alpha.2`
- `betternat-agent --version`: `v0.1.0-alpha.2`
- `betternat-agent.service`: active
- `doctor --live`: warning only for known rollback/ASG-discovery alpha items
- private client egress: `https://checkip.amazonaws.com` returned
  `184.34.128.79`

## New Release Blocker

The standby gateway did not complete bootstrap while it had no public IP. It was
reachable on the VPC network, but SSM did not come online and ports `9108` and
`9109` refused connections.

The root cause is the current non-AMI stable-EIP topology:

- gateway nodes are in a public subnet whose default route targets the Internet
  Gateway,
- only the current active node owns the stable EIP,
- a standby gateway without public IP cannot download Docker/LoxiLB/runtime
  dependencies or reach SSM/control-plane APIs,
- after handover, the old active can hit the same problem when it loses the
  stable EIP.

To complete the test, a temporary EIP was manually associated to the standby
node:

```text
eipalloc-0c65913bf7541543c / 16.146.168.203
```

After that temporary EIP was attached, standby completed bootstrap and joined
the daemon registry.

This is a production-preview blocker for the non-AMI stable-EIP HA path. It is
not a blocker for describing the current release as an alpha technical preview
with documented limitations.

## Follow-Up Decision

After this validation, the provider-derived install plan was changed so gateway
nodes default to ordinary auto-assigned public IPv4 addresses. Those addresses
are for bootstrap and management/control-plane reachability. The shared EIP in
stable mode remains the intended private-workload egress identity.

This should remove the standby bootstrap blocker and must be revalidated in AWS
without manually attaching temporary public IPs.

There is still a deeper AWS identity concern: associating a shared EIP to an
instance's primary private IPv4 can replace that instance's auto-assigned public
IPv4. If BetterNAT needs every gateway node to retain a separate management
public IPv4 even while owning the shared egress EIP, the stable egress identity
should move to a secondary private IP or secondary ENI, and LoxiLB should SNAT
private workload traffic to that egress private IP.

## Handover Evidence

With both gateway nodes healthy, active-to-standby handover succeeded:

```text
handover completed: i-02a936e084657e227 -> i-0d539fcbf045358f9 generation=2
```

AWS control-plane truth after handover:

- private default route target: `i-0d539fcbf045358f9`
- stable EIP `184.34.128.79`: associated to `i-0d539fcbf045358f9`

Client monitor:

- duration: 90 seconds
- request interval: about 1 second
- failed samples: 0
- expected stable IP: `184.34.128.79`
- one sample returned temporary IP `16.146.168.203` during handover because the
  standby had a manually attached bootstrap EIP

The zero-failure result proves the handover path can work when both nodes are
healthy. The temporary-IP sample proves the workaround is not acceptable product
behavior for stable-EIP mode.

## Cleanup

The temporary EIP was released before Terraform destroy.

Terraform destroy succeeded:

```text
Destroy complete! Resources: 16 destroyed.
```

Residual scans:

- Resource Groups tag scan found only terminated EC2 instance records.
- Direct checks found no run-id DynamoDB tables, ASGs, security groups, EIPs,
  or IAM roles.

Terminated instance records:

- `i-02a936e084657e227`
- `i-0d539fcbf045358f9`
- `i-02ae1f34e358b3d8e`

## Follow-Up

Before production-preview:

1. Re-run this validation with default gateway public IPv4 and without manually
   attaching temporary public IPs.
2. Confirm both gateway nodes can bootstrap, remain registered, reach
   control-plane APIs, and survive handover while only the stable EIP is
   user-visible.
3. Decide whether to implement secondary private IP or secondary ENI based
   shared-EIP ownership for strict management/egress public identity separation.
