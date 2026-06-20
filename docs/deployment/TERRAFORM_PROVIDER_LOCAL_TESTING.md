# Terraform Provider Local Testing

Date: 2026-06-20

## Purpose

BetterNAT's Terraform provider has three useful local test layers:

1. Go unit tests for provider schema and lifecycle behavior.
2. Terraform CLI tests using a local provider binary override.
3. Local AWS API simulation with LocalStack.
4. AWS acceptance tests in a disposable VPC.

Only the fourth layer needs cloud resources.

## Layer 1: Go Tests

This is the default local loop:

```bash
./manage test
./manage build provider
```

Useful packages:

```bash
go test ./internal/tfprovider
go test ./internal/install/aws
go test ./internal/installplan
```

This validates:

- provider defaults,
- install plan rendering,
- rollback metadata,
- AWS SDK request construction through fakes,
- destroy rollback and cleanup behavior through fakes,
- read lifecycle status updates through fakes.

It does not validate Terraform CLI protocol behavior.

## Layer 2: Terraform CLI With Local Provider Override

Install Terraform or OpenTofu locally, then build the provider:

```bash
./manage build provider
```

Create a CLI config outside the repo, for example:

```hcl
provider_installation {
  dev_overrides {
    "betternat/betternat" = "<absolute-path-to-repo>"
  }

  direct {}
}
```

Then run:

```bash
TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform init
TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform validate
TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform plan
```

Notes:

- Terraform expects the provider binary name to be `terraform-provider-betternat`.
- The local override path should point to the directory containing that binary.
- `examples/terraform/main.tf` contains placeholder VPC/subnet/route IDs, so `plan` may require real-looking or mocked values depending on the provider lifecycle path being exercised.
- `terraform validate` is the best no-cloud CLI compatibility check.

## Layer 3: Local AWS API Simulation

Use LocalStack to mock the AWS APIs used by BetterNAT's provider:

- EC2,
- IAM,
- DynamoDB.

This is the best local approximation of a full Terraform `apply/read/destroy` cycle without touching AWS.

Expected coverage:

- Terraform provider process and protocol behavior,
- AWS SDK endpoint wiring,
- IAM role/profile create/delete calls,
- DynamoDB lease table create/delete calls,
- EC2 security group create/delete calls,
- EC2 instance launch/terminate calls,
- EIP allocate/describe/release calls,
- route table `DescribeRouteTables` and `ReplaceRoute` behavior if LocalStack's current EC2 implementation supports the exact route target shape.

Known boundaries:

- LocalStack does not prove real AWS control-plane latency.
- It does not prove ENA/Nitro networking, source/destination-check behavior, LoxiLB datapath, or real private-subnet egress.
- EC2 route/EIP edge cases may differ from AWS. Treat this as provider lifecycle testing, not production HA proof.

Manual AWS provider endpoint shape:

```hcl
provider "aws" {
  access_key                  = "test"
  secret_key                  = "test"
  region                      = "us-east-1"
  skip_credentials_validation = true
  skip_metadata_api_check     = true

  endpoints {
    dynamodb = "http://localhost:4566"
    ec2      = "http://localhost:4566"
    iam      = "http://localhost:4566"
    sts      = "http://localhost:4566"
  }
}
```

BetterNAT provider work needed before this is useful:

- `provider "betternat"` supports `aws_endpoint_url = "http://localhost:4566"`,
- a disposable LocalStack Terraform fixture exists under `examples/terraform-localstack/`,
- seed or create a route table/default route shape that the provider can snapshot before `ReplaceRoute`,
- add `manage tf localstack` once Terraform/OpenTofu and LocalStack are available in the developer environment.

Current fixture:

```hcl
provider "betternat" {
  aws_endpoint_url = "http://localhost:4566"
}
```

Expected local flow:

```bash
./manage build provider

export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_REGION=us-east-1

TMPDIR=$PWD/tmp TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform-localstack init
TMPDIR=$PWD/tmp TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform-localstack validate
TMPDIR=$PWD/tmp TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform-localstack plan -refresh=false
TMPDIR=$PWD/tmp TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform-localstack apply
TMPDIR=$PWD/tmp TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=examples/terraform-localstack destroy
```

Provider development override example:

```hcl
provider_installation {
  dev_overrides {
    "betternat/betternat" = "<repo-root>"
  }

  direct {}
}
```

In sandboxed local runs, Terraform provider plugins may need `TMPDIR=$PWD/tmp` so the plugin RPC Unix socket is created inside the repo workspace. Without it, plugin startup can fail with a `bind: operation not permitted` error.

Current local result:

- `terraform init -backend=false` for `examples/terraform` passes with the local BetterNAT provider dev override.
- `terraform validate` and `terraform plan -refresh=false` for `examples/terraform` pass when run with `TMPDIR=$PWD/tmp`.
- `terraform init -backend=false`, `terraform validate`, and `terraform plan -refresh=false` for `examples/terraform-localstack` pass.
- Full LocalStack `apply/destroy` passes when `localstack/localstack:latest` is started with `LOCALSTACK_AUTH_TOKEN` from `.env`.
- The LocalStack run exercised BetterNAT provider calls for IAM role/profile creation, DynamoDB lease table creation, EC2 security group creation, EC2 instance launch, source/destination check modification, EIP allocation, route snapshot, initial `ReplaceRoute`, readback through `DescribeRouteTables` and `DescribeAddresses`, destroy-time route rollback, instance termination, EIP release, DynamoDB table deletion, IAM cleanup, and security group deletion.

LocalStack container command used:

```bash
docker run -d \
  --name betternat-localstack \
  --env-file .env \
  -p 127.0.0.1:4566:4566 \
  -e SERVICES=ec2,iam,dynamodb,sts \
  -e DEBUG=0 \
  localstack/localstack:latest
```

Cleanup command:

```bash
docker rm -f betternat-localstack
```

Alternative:

- Moto server can run as a standalone AWS-compatible HTTP server for SDK clients, but it is a lighter mock layer. Use it for narrow unit/integration checks, not as the main Terraform provider lifecycle simulator.

## Layer 4: Disposable AWS Acceptance Test

Use this only with an isolated test VPC and explicit cleanup:

```bash
TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=<disposable-test-dir> apply
TF_CLI_CONFIG_FILE=<path-to-cli-config> terraform -chdir=<disposable-test-dir> destroy
```

Acceptance coverage should verify:

- route snapshot before `ReplaceRoute`,
- route target after apply,
- EIP allocation and association,
- EC2 source/destination check disabled,
- provider `Read` sees route/EIP state,
- destroy restores previous route,
- destroy cleans appliances, EIPs, DynamoDB, IAM, and security group,
- no tagged BetterNAT test resources remain.

## Current Local Machine Note

Terraform CLI is not installed in the current local environment. The repo can still run layer 1 today. Layer 2 needs Terraform or OpenTofu installed first.
