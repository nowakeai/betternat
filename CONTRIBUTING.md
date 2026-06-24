# Contributing To BetterNAT

BetterNAT is an infrastructure project. Changes should be small, testable, and conservative.

## Development Principles

- Prefer mature existing components over custom datapath logic.
- Keep Terraform as the install/lifecycle source of truth.
- Keep runtime HA in `betternat-agent`.
- Avoid central-server assumptions in the first open-source release.
- Preserve cleanup and rollback behavior.
- Do not add user-facing claims without tests or documentation.

## Local Checks

Run Go tests:

```sh
GOCACHE=$PWD/tmp/go-build-cache go test ./...
```

Check formatting:

```sh
gofmt -w <changed-go-files>
git diff --check
```

Build the Terraform provider:

```sh
go build -o terraform-provider-betternat ./cmd/terraform-provider-betternat
```

Terraform example validation requires the local provider development override described in `docs/deployment/TERRAFORM_PROVIDER_LOCAL_TESTING.md`.

## Documentation

Update documentation when changing:

- provider schema or UX,
- agent config,
- HA behavior,
- datapath behavior,
- metrics,
- release artifacts,
- installation or cleanup behavior.

Release-facing docs live under `docs/release/`.
Research and decision records live under `docs/research/`.

## Security

Do not commit:

- AWS credentials,
- private keys,
- presigned URLs,
- Terraform state,
- local absolute paths,
- `.env` files.

Report vulnerabilities through `SECURITY.md`.
