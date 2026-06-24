# BetterNAT CloudFormation Delivery Decision

Date: 2026-06-24

## Decision

Defer CloudFormation as a supported BetterNAT install path for the current
alpha and production-readiness push.

The supported install path remains:

```text
Terraform Registry provider -> betternat_gateway -> AWS resources + agent config
```

Do not create a first-alpha CloudFormation template.

## Rationale

BetterNAT currently needs lifecycle behavior that is easier and safer to express
through the Terraform provider:

- render and validate agent configuration,
- create and update coordinated AWS resources,
- preserve rollback metadata for private route restoration,
- control replacement behavior for non-capacity changes,
- coordinate release artifact URLs and checksums,
- keep provider code and runtime install logic under one tested Go code path.

A CloudFormation template would add a second infrastructure contract before the
Terraform path and runtime semantics have stabilized. That would increase
documentation, rollback, support, and AWS validation burden without improving
the alpha's primary user journey.

## Future Revisit Criteria

Revisit CloudFormation after these are true:

- published BetterNAT AMIs exist for the supported architectures,
- AMI channel or version resolution is implemented,
- stable single-AZ Terraform install/upgrade/destroy behavior is proven,
- route rollback semantics are documented and tested enough to duplicate in
  another IaC surface,
- support boundaries for CloudFormation updates and stack deletes are clear.

## Future Template Requirements

If CloudFormation becomes supported later, the template must cover:

- VPC and subnet parameters,
- private route table IDs,
- private CIDR allowlist,
- stable egress IP option,
- HA timing option,
- instance type and ASG capacity,
- IAM role/profile,
- DynamoDB coordination table,
- security groups,
- launch template,
- ASG,
- outputs for public egress IP, ASG name, coordination table, and metrics
  endpoint guidance.

The template must be validated for create, update, and delete in a disposable
AWS account before it is advertised. Stack delete behavior must either restore
private routes safely or make the required manual rollback explicit.

Avoid custom Lambda resources unless they are strictly necessary. If custom
resources are introduced, document their IAM permissions and test rollback after
failed custom-resource execution.
