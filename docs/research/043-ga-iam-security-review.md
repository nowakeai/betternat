# GA IAM And Security Posture Review

Date: 2026-06-24

## Scope

This review covers the BetterNAT AWS runtime role and default gateway-node
security posture after provider `0.1.0-alpha.7`.

Sources reviewed:

- `internal/installplan/plan.go`
- `internal/iamcheck/iamcheck.go`
- `internal/install/aws/applier.go`
- `internal/cloud/aws/`
- `internal/coordination/dynamodb/`
- `internal/lease/dynamodb/`
- `internal/bootstrap/bootstrap.go`
- `scripts/ami/provision-betternat-ami.sh`
- `internal/tfprovider/gateway_resource.go`
- `docs/user/reference/IAM_POLICY.md`
- `docs/user/reference/SECURITY_HARDENING.md`

This is an engineering review, not legal or compliance advice.

## Summary

No release-blocking defect was found in the default alpha7 bootstrap path for:

- no inbound public SSH by default,
- IMDSv2 required in launch templates,
- SSM Session Manager access by default,
- local agent config written with mode `0600`,
- peer API requiring a bearer token,
- support bundle redacting the peer API token,
- checksum-verified BetterNAT bootstrap artifacts when checksums are supplied,
- provider-managed security group scoped to configured private CIDRs for
  ingress and `0.0.0.0/0` for node outbound/bootstrap/egress.

The largest remaining GA hardening item is IAM scoping. The provider currently
creates one inline runtime policy with the required action list and
`Resource="*"`. This is acceptable for alpha and production-preview validation,
but a final GA policy should scope the actions that can be cleanly scoped to
specific BetterNAT resources.

The second GA hardening item is network exposure inside the VPC. The gateway
security group permits all protocols from configured private CIDRs so private
workloads can use the node as a NAT target. That also means unauthenticated
Prometheus metrics on port `9108` and authenticated peer API on port `9109` are
reachable from those CIDRs unless operators add narrower network controls.

## Runtime IAM Actions

The runtime action list in `internal/installplan/plan.go` and
`internal/iamcheck/iamcheck.go` currently contains:

```text
autoscaling:CompleteLifecycleAction
ec2:AssociateAddress
ec2:DescribeAddresses
ec2:DescribeInstanceAttribute
ec2:DescribeRouteTables
ec2:ModifyInstanceAttribute
ec2:ReplaceRoute
dynamodb:DeleteItem
dynamodb:GetItem
dynamodb:Query
dynamodb:UpdateItem
iam:SimulatePrincipalPolicy
sts:GetCallerIdentity
```

Reviewed usage:

| Action | Current use | Review |
| --- | --- | --- |
| `autoscaling:CompleteLifecycleAction` | ASG lifecycle/Spot interruption graceful handover path | Required for lifecycle hook completion. Scope to provider-created ASG/hook when the policy generator supports account/region ARNs. |
| `ec2:AssociateAddress` | Shared EIP handover in stable egress IP mode | Required when `stable_egress_ip=true`; not needed in non-stable mode. Scope by allocation ID where possible. |
| `ec2:DescribeAddresses` | Resolve and verify shared EIP ownership/public identity | Required for stable mode and diagnostics. |
| `ec2:DescribeInstanceAttribute` | Verify source/destination check | Required for `doctor --live` and runtime verification. |
| `ec2:DescribeRouteTables` | Verify route ownership | Required for route convergence checks and doctor. Scope by route table ARN if possible. |
| `ec2:ModifyInstanceAttribute` | Disable source/destination check on the local node | Required. AWS resource scoping for this API should be validated before tightening. |
| `ec2:ReplaceRoute` | Move private default route to active gateway node | Required. Scope to configured private route table ARNs if possible. |
| `dynamodb:DeleteItem` | Release legacy lease, delete agent and handover records | Required while both lease manager and coordination backend use deletes. Scope to the BetterNAT coordination table and any legacy lease table during upgrade windows. |
| `dynamodb:GetItem` | Read lease/handover records | Required. Scope to BetterNAT table ARN. |
| `dynamodb:Query` | List agent and handover records | Required. Scope to BetterNAT table ARN. |
| `dynamodb:UpdateItem` | Acquire/renew/transfer lease, register agents, update handover records | Required. Scope to BetterNAT table ARN. |
| `iam:SimulatePrincipalPolicy` | `doctor --live` IAM check | Optional diagnostics from a strict-production perspective. Keep for alpha; consider a `diagnostics_iam_simulation_enabled` style option later. |
| `sts:GetCallerIdentity` | Resolve current assumed role into IAM role ARN for diagnostics | Required only for the IAM simulation path. |

`autoscaling:DescribeAutoScalingGroups` and `ec2:DescribeInstances` are still
used by direct/debug CLI and doctor-style inspection paths, but are not in the
required runtime action list. This is intentional for the registry-backed normal
runtime path: agents self-register through DynamoDB and `betternat status` uses
the daemon/coordination view. Operators who need direct AWS discovery from the
gateway role may add these as diagnostics permissions, but they are not part of
the default minimal runtime set.

## Current IAM Policy Shape

The provider currently writes an inline role policy named `betternat-runtime`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["..."],
      "Resource": "*"
    }
  ]
}
```

It also attaches:

```text
arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore
```

Review result:

- Alpha/prod-preview: acceptable with documentation because the role is created
  for BetterNAT gateway nodes and the action list is small.
- GA: not final least privilege. The policy generator should split statements
  and scope DynamoDB, EIP, route-table, lifecycle-hook, and instance operations
  where AWS supports resource-level conditions cleanly.

Recommended GA policy shape:

- DynamoDB statement scoped to:
  - coordination table ARN,
  - legacy lease table ARN only during migrations.
- EC2 route statement scoped to configured private route table ARNs.
- EIP statement scoped to provider-created allocation IDs in stable mode.
- Source/destination check statement scoped to provider-created gateway
  instances where possible, otherwise constrained by resource tags.
- Lifecycle hook completion constrained to provider-created ASG and lifecycle
  hook names.
- Diagnostics statement for `iam:SimulatePrincipalPolicy` and
  `sts:GetCallerIdentity`, controlled by an explicit provider option if strict
  roles are needed.

## Network Exposure Review

Default provider-created gateway security group:

- ingress: all protocols from configured `private_cidrs`,
- egress: all protocols to `0.0.0.0/0`.

Default launch template:

- no public SSH rule is created,
- IMDSv2 is required,
- hop limit is `1`,
- gateway nodes use SSM for administrative access,
- default `cloud_init` mode associates per-node public IPv4 so ordinary Linux
  AMIs can download packages, Docker images, and release artifacts.

Review result:

- No public management ingress is created by default.
- The all-protocol private-CIDR ingress is functional for NAT traffic, but it
  also makes port `9108` metrics and port `9109` peer API reachable from those
  private CIDRs.
- Port `9109` requires `Authorization: Bearer <peer_api_auth_token>`.
- Port `9108` is unauthenticated and must be protected by security groups,
  routing, and monitoring-network design.

GA hardening options:

- add separate security-group controls for monitoring and peer API access,
- keep broad forwarding ingress only where required for datapath traffic,
- document that private workloads in `private_cidrs` should not be treated as
  fully untrusted relative to the gateway node,
- add an option to bind metrics and peer API to narrower listen addresses when
  the deployment topology supports it.

## Local Config And Peer API Token

The provider renders `peer_api_auth_token` into `/etc/betternat/agent.json`.
The bootstrap script writes that file with mode `0600`. Config loading rejects
peer API enabled without an auth token. The support bundle redacts the token.

Peer API calls use:

```text
Authorization: Bearer <token>
```

Review result:

- Acceptable for alpha/prod-preview inside the provider-managed VPC security
  boundary.
- Not a replacement for transport security if peer API is ever exposed outside
  trusted private networks.
- GA hardening should consider token rotation and distribution semantics if
  rolling upgrades or multi-AZ groups become first-class.

## systemd And Datapath Privilege

The cloud-init service sets:

```ini
NoNewPrivileges=true
Restart=always
RestartSec=2s
```

The AMI scaffold uses the same baseline for `betternat-agent`.

Review result:

- The current hardening is intentionally conservative because the agent needs
  AWS SDK access, local config access, and datapath coordination.
- LoxiLB runs as a privileged host-network container in the default bootstrap
  path. That is expected for the current LoxiLB datapath model, but it remains a
  packaging hardening item.
- Additional systemd hardening options are already documented in
  `docs/user/reference/SECURITY_HARDENING.md` and must be Linux/AWS validated before
  becoming defaults.

## Release Gate Decision

For the current GA checklist:

- IAM least-privilege review: complete.
- Security posture review: complete.
- No immediate code blocker was found for the current Terraform Registry plus
  `cloud_init` production-preview path.

Remaining GA hardening:

1. Implement scoped runtime IAM statements instead of `Resource="*"`.
2. Decide whether `iam:SimulatePrincipalPolicy` and `sts:GetCallerIdentity`
   remain default runtime permissions or become optional diagnostics.
3. Split datapath forwarding ingress from metrics/peer API access controls.
4. Validate stronger systemd hardening on real Linux/AWS nodes.
5. Add token rotation/distribution design before multi-AZ or broader handover
   topologies.
