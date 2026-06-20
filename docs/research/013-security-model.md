# Security Model

Date: 2026-06-19

## Question

How should BetterNAT be secured, given that the agent and Terraform provider can modify cloud routes, public IP bindings, lease records, and local packet forwarding?

## Short Answer

Security must be designed as layered blast-radius reduction:

1. Terraform provider validates topology before creating resources.
2. IAM/RBAC grants only the narrow cloud actions needed.
3. Cloud mutations are scoped to explicit route tables, EIPs, ENIs, and lease records.
4. Agent config contains its own allowlist and refuses unexpected resources even if IAM would allow them.
5. Instances use SSM-only access, IMDSv2, systemd hardening, and no inbound SSH by default.
6. AMIs and releases are signed/reproducible enough for production trust.

Do not rely on one control alone. IAM condition/resource support varies by API, so the agent must also enforce product-level scoping.

## Threat Model

Primary risks:

- A compromised NAT appliance modifies the wrong route table.
- A compromised agent steals/reassociates the wrong EIP.
- Split-brain causes route flapping.
- Terraform provider destroy removes the only egress path.
- Over-broad IAM lets the appliance control unrelated VPC resources.
- Inbound SSH/admin exposure compromises the appliance.
- IMDS credentials are stolen through SSRF or local compromise.
- eBPF/nftables bugs break forwarding or hide traffic.
- Malicious AMI/binary supply chain compromise.

Out of scope for v0:

- Protecting against a fully compromised AWS account admin.
- Transparent preservation of active flows under malicious failover.
- Formal verification of eBPF programs.

## Security Principles

- Least privilege.
- Explicit resource allowlists.
- Tag scoping where supported.
- Runtime self-checks.
- Fail closed for control-plane mutations.
- Fail open or degrade carefully for datapath, to avoid needless outage.
- No inbound admin path by default.
- Auditable cloud mutations.
- Safe destroy/rollback behavior.

## IAM / Cloud Permission Model

Agent needs permissions for:

- read identity/state,
- update only configured private route tables,
- associate only configured EIP(s),
- read only configured network interfaces/instances where possible,
- write only its HA group lease record,
- emit metrics/logs.

AWS actions likely needed:

```text
ec2:DescribeInstances
ec2:DescribeNetworkInterfaces
ec2:DescribeRouteTables
ec2:ReplaceRoute
ec2:DescribeAddresses
ec2:AssociateAddress
dynamodb:GetItem
dynamodb:PutItem
dynamodb:UpdateItem
dynamodb:DeleteItem
cloudwatch:PutMetricData
logs:CreateLogStream
logs:PutLogEvents
```

Advanced ENI mode would add:

```text
ec2:AttachNetworkInterface
ec2:DetachNetworkInterface
ec2:AssignPrivateIpAddresses
ec2:UnassignPrivateIpAddresses
```

### Important IAM caveat

Not every EC2 action supports equally precise resource-level permissions or condition keys. The exact policy must be generated and tested against AWS's service authorization reference.

Therefore, use defense in depth:

- IAM scoped by resource ARN/tag where supported.
- Agent config allowlists exact route table IDs, EIP allocation IDs, ENI IDs, and destination CIDRs.
- Provider tags all managed resources.
- Agent refuses to mutate resources that are not in config even if IAM allows it.
- `doctor` checks IAM is not broader than expected where possible.

## Agent-side Allowlist

Agent config should include:

```yaml
allowed_mutations:
  route_tables:
    - route_table_id: rtb-abc
      destinations:
        - 0.0.0.0/0
      allowed_targets:
        - eni-active
        - eni-standby
        - nat-rollback
  eips:
    - eipalloc-123
  lease_keys:
    - prod-us-west-2a
```

Before any cloud mutation:

1. Validate requested mutation against local config.
2. Validate current lease generation.
3. Validate target resource tags/identity.
4. Execute SDK call.
5. Log request ID and result.
6. Verify post-state.

This protects against IAM policy mistakes and agent bugs.

## Split-brain Security

Split-brain is a security and availability issue.

Controls:

- DynamoDB conditional-write lease.
- generation/fencing token.
- active actions require current generation.
- stale active demotes on generation mismatch.
- route/EIP mutations include lease generation in logs.

Never allow heartbeat-only takeover in production HA mode.

## Administrative Access

Default:

- SSM Session Manager enabled.
- No inbound SSH rule.
- No key pair required.
- Security group ingress minimal.
- Admin API bound to localhost or Unix socket.

AWS Session Manager provides secure instance management without opening inbound ports, bastion hosts, or SSH keys. This should be the default operational path.

If SSH is enabled:

- require explicit opt-in,
- restrict CIDRs,
- document it as less secure,
- surface warning in `doctor`.

## Instance Metadata

Require IMDSv2:

- Terraform provider sets metadata options to require tokens.
- Agent uses official AWS SDK with IMDSv2 support.
- `doctor` verifies IMDSv2 required.

Consider hop limit:

- NAT appliance itself may not run untrusted workloads, so hop limit can be strict.
- If any local containerized component is introduced, revisit IMDS exposure.

## Local OS Hardening

Systemd service hardening where compatible:

```text
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_NETLINK AF_UNIX
SystemCallFilter=...
CapabilityBoundingSet=CAP_NET_ADMIN CAP_BPF CAP_SYS_ADMIN? ...
```

Need care:

- nftables requires network admin privileges.
- eBPF loading may need `CAP_BPF`, `CAP_PERFMON`, `CAP_NET_ADMIN`, or older kernels may require broader caps.
- Over-hardening can break datapath operations.

v0 can run as root with strict systemd boundaries. Later, split:

- privileged datapath helper,
- less-privileged control/metrics process.

## Network Security Groups

Default security group:

- inbound: none, except optional health/metrics from configured monitoring CIDRs.
- outbound: needed AWS APIs, package repos during bootstrap if used, egress traffic forwarding.

For private AWS API access, recommend VPC endpoints:

- SSM,
- EC2 messages,
- SSM messages,
- DynamoDB gateway endpoint or interface pattern depending on region/service support,
- CloudWatch Logs/Metrics,
- EC2 API if available through PrivateLink in target region.

Do not require VPC endpoints in v0, but document them for hardened deployments.

## Data Plane Security

Datapath should:

- allow only configured private CIDRs as NAT sources,
- avoid being an open forwarder,
- drop unexpected source ranges,
- avoid packet logging on hot path by default,
- expose drop counters.

nftables dedicated table:

```text
table inet betternat
```

Rules should be generated from config and validated before applying.

Avoid:

- modifying unrelated user firewall tables,
- enabling forwarding for unexpected interfaces,
- accepting traffic from public interface to private sources unless explicitly required.

## Observability Security

Metrics may leak:

- private IPs,
- destination IPs/domains,
- Kubernetes namespaces/workloads,
- cost centers.

Controls:

- Prometheus endpoint bound to localhost or private monitoring CIDR.
- optional TLS/auth through reverse proxy or existing monitoring stack.
- redact or aggregate high-sensitivity labels.
- avoid logging full flow samples by default.
- keep exact pod/destination data out of default public dashboards if not needed.

## eBPF Security

eBPF program loading should be treated as privileged code deployment.

Controls:

- ship prebuilt, versioned eBPF objects,
- verify expected kernel/BTF compatibility,
- load only bundled programs,
- pin maps under product path,
- unload cleanly,
- degrade observability if eBPF fails to load,
- do not accept arbitrary user eBPF snippets.

## Terraform Provider Safety

Provider must prevent dangerous operations:

- refuse destroy if no rollback target is configured, unless user explicitly opts in,
- validate route tables belong to expected VPC,
- validate route tables/AZ mapping if per-AZ mode,
- detect existing default routes and store rollback metadata,
- reject public SSH unless explicitly enabled,
- warn when using broad IAM mode,
- prevent managing all route tables by implicit discovery.

Provider `Read` should expose:

- route target,
- active node,
- EIP owner,
- lease owner,
- degraded status.

## Supply Chain

Release artifacts:

- signed agent binaries,
- signed checksums,
- versioned AMIs,
- SBOM,
- container/image provenance if containers are used,
- Terraform provider checksum through Terraform registry.

AMI:

- minimal base image,
- pinned package versions where practical,
- security updates strategy,
- no baked secrets,
- SSM agent included,
- CIS/hardening profile considered later.

## Audit Events

Every control-plane mutation should emit:

- timestamp,
- gateway ID,
- HA group ID,
- instance ID,
- operation,
- resource ID,
- old state,
- new state,
- lease generation,
- cloud request ID,
- success/failure,
- latency.

Events should go to:

- local structured logs,
- CloudWatch Logs if configured,
- Prometheus counters,
- optional webhook/SNS/EventBridge later.

## Secure Defaults

Default v0 security posture:

- SSM-only, no SSH.
- IMDSv2 required.
- least-privilege IAM generated by provider.
- route/EIP/lease allowlists in agent config.
- DynamoDB lease/fencing required in HA mode.
- shared EIP disabled unless explicitly configured.
- eBPF disabled by default until stable.
- metrics private by default.
- rollback target required before managing production routes.

## Decision

Security must be a product feature.

The most important early controls are:

1. Custom Terraform provider validates topology and generates narrow IAM.
2. Agent enforces local resource allowlists before every SDK mutation.
3. DynamoDB lease/fencing is mandatory in HA mode.
4. SSM-only and IMDSv2 are defaults.
5. Destroy/rollback is conservative.
6. Every route/EIP mutation is audited.

## Sources

- AWS EC2 actions, resources, and condition keys: https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazonec2.html
- AWS IAM condition keys overview: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_condition-keys.html
- Identity-based policies for Amazon EC2: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-policies-for-amazon-ec2.html
- DynamoDB actions, resources, and condition keys: https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazondynamodb.html
- AWS Session Manager provides access without inbound ports, bastion hosts, or SSH keys: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager.html
- EC2 instance metadata service and IMDSv2 SDK support: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html
- Configure new instances to require IMDSv2: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-IMDS-new-instances.html
- Modify existing instances to require IMDSv2: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-IMDS-existing-instances.html
