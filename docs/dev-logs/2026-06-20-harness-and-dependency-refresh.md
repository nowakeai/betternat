# 2026-06-20 Harness And Dependency Refresh

## Summary

Added a repo-local harness based on the user-center workflow pattern, adapted for BetterNAT's Go/AWS/Terraform shape.

New durable entrypoints:

- `AGENTS.md`
- `CODEX.md`
- `manage`
- `docs/deployment/AI_WORKFLOW.md`
- `docs/deployment/DEPENDENCY_POLICY.md`
- `docs/dev-logs/README.md`

Updated:

- `README.md`
- `docs/README.md`
- `.gitignore`
- `go.mod`
- `go.sum`

## Harness Principles

- Use `./manage` for recurring workflows.
- Keep default validation network-free.
- Use `tmp/go-build` for Go build cache.
- Prefer mature components and official SDKs.
- Prefer current supported dependency versions.
- Keep docs under `docs/` and update `docs/README.md` for durable docs.

## Dependency Refresh

Ran:

```sh
./manage deps check
./manage deps upgrade
GOCACHE=$PWD/tmp/go-build go get -u all
GOCACHE=$PWD/tmp/go-build go mod tidy
./manage verify
```

The upgrade completed successfully and `./manage verify` passed.

Notable upgraded modules included:

- `github.com/aws/smithy-go`
- `github.com/hashicorp/go-plugin`
- `github.com/hashicorp/terraform-svchost`
- `golang.org/x/net`
- `golang.org/x/sys`
- `golang.org/x/text`
- `google.golang.org/grpc`
- `google.golang.org/genproto/googleapis/rpc`

Final `./manage deps check` still reports some available updates in transitive dependency chains, including OTel/GCP detectors, `go-version`, `protoreflect`, `spiffe`, and a few testing/helper modules. The project does not import those directly; they are retained by upstream dependency constraints after `go get -u all`.

`github.com/golang/protobuf` is still reported as deprecated through transitive dependencies. Do not add new direct usage of it; prefer `google.golang.org/protobuf`.

## Validation

Passed:

```sh
./manage help
./manage docs check
./manage test
./manage verify
```
