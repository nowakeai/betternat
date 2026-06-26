# Data Source: betternat_runtime_artifacts

Reads provider-supported BetterNAT runtime artifact URLs and SHA256 checksums.

Most users should set `betternat_version` on a module or resource. Use this data
source for module authoring, validation, or artifact inspection.

## Example

```hcl
data "betternat_runtime_artifacts" "current" {
  version = "v0.1.0"
  os      = "linux"
  arch    = "arm64"
}
```

## Outputs

- `agent_binary_url`
- `agent_binary_sha256`
- `cli_binary_url`
- `cli_binary_sha256`
- `loxicmd_binary_url`
- `loxicmd_binary_sha256`
