## Summary

Describe what changed and why.

## Scope

- [ ] Provider / Terraform UX
- [ ] Agent runtime
- [ ] CLI
- [ ] HA / failover / handover
- [ ] Datapath
- [ ] Packaging / release
- [ ] Documentation only

## Validation

List the commands, tests, AWS checks, or manual verification performed.

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
GOCACHE=$PWD/tmp/go-build go build ./cmd/betternat ./cmd/betternat-agent ./cmd/terraform-provider-betternat
git diff --check
```

## Operational Impact

- Route/EIP behavior changed: yes/no
- IAM policy changed: yes/no
- Terraform replacement behavior changed: yes/no
- Release artifact or bootstrap behavior changed: yes/no
- User docs updated: yes/no

## Risk And Rollback

Describe the main failure mode and how to roll back or recover.

## Checklist

- [ ] I did not commit credentials, private keys, Terraform state, presigned URLs, or local-only paths.
- [ ] I updated docs for user-visible behavior, config, metrics, release, or operational changes.
- [ ] I used the lightest sufficient validation and documented any tests I could not run.
