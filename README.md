# BetterNAT

BetterNAT is a self-owned, observable, highly available egress gateway for high-volume AWS private subnet workloads.

Current v0 direction:

```text
Primary datapath: LoxiLB standalone egress SNAT
Fallback datapath: Linux nftables/nf_conntrack
Runtime control plane: betternat-agent
Install UX: terraform-provider-betternat
Implementation language: Go
```

Start with:

- `docs/architecture.md`
- `docs/spec-v0.md`
- `docs/README.md`
- `AGENTS.md`
- `CODEX.md`

## Development

The repo-local harness is `./manage`:

```sh
./manage help
./manage test
./manage verify
./manage smoke doctor
./manage deps check
```

Use `./manage` for recurring local workflows. Direct Go commands are still useful for focused work.

Run tests:

```sh
GOCACHE=$PWD/tmp/go-build go test ./...
```

The explicit `GOCACHE` keeps build artifacts inside the workspace.

Build the Terraform provider:

```sh
GOCACHE=$PWD/tmp/go-build go build ./cmd/terraform-provider-betternat
```

An example provider configuration lives in `examples/terraform/main.tf`. The provider computes `install_plan_json`, `agent_config_json`, `agent_config_hash`, `user_data`, route metadata, and rollback metadata, then uses the AWS SDK during Terraform apply to install the BetterNAT appliance stack.

Estimate NAT Gateway processing cost versus BetterNAT appliance cost:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat cost estimate --gb 51200 --appliance-hourly 0.05 --appliances 2
```

Validate the example agent config:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat-agent --config examples/agent-config.json --validate-only
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat-agent --config examples/agent-config.yaml --validate-only
```

Run local static doctor checks:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat doctor --config examples/agent-config.json
```

Inspect configured gateway and datapath state:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat status --config examples/agent-config.yaml
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat datapath status --config examples/agent-config.yaml
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat failover status --config examples/agent-config.yaml
```

Run one datapath reconciliation pass:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat-agent --config examples/agent-config.json --once
```

Render one datapath metrics snapshot in Prometheus text format:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat-agent --config examples/agent-config.json --once --prometheus
```

Run the continuous agent:

```sh
GOCACHE=$PWD/tmp/go-build go run ./cmd/betternat-agent --config examples/agent-config.json
```

On macOS the datapath commands are expected to fail unless the configured Linux interface and `loxicmd` are available. Unit tests use fakes for local validation, including the continuous reconcile loop and metrics handler.

## Documentation

Durable docs live under `docs/`.

- `docs/README.md` is the documentation index.
- `docs/deployment/AI_WORKFLOW.md` defines the harness and validation workflow.
- `docs/deployment/DEPENDENCY_POLICY.md` defines dependency freshness and reuse policy.
- `docs/dev-logs/` is for durable implementation notes and architecture pivots.
