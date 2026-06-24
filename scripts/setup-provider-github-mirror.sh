#!/usr/bin/env bash
betternat_provider_mirror_sourced=0
betternat_provider_mirror_shell_options=""

if [ "${BASH_SOURCE[0]}" != "$0" ]; then
  betternat_provider_mirror_sourced=1
  betternat_provider_mirror_shell_options="$(set +o)"
fi

set -euo pipefail

restore_shell_options() {
  if [ "$betternat_provider_mirror_sourced" = "1" ]; then
    eval "$betternat_provider_mirror_shell_options"
  fi
}

usage() {
  cat >&2 <<'EOF'
Usage:
  source scripts/setup-provider-github-mirror.sh

This creates a temporary Terraform CLI provider mirror for the BetterNAT
Terraform provider GitHub release and exports TF_CLI_CONFIG_FILE.

Environment:
  BETTERNAT_PROVIDER_VERSION       Provider version. Default: 0.1.0-alpha.3.
  BETTERNAT_PROVIDER_RELEASE_BASE  Override provider release base URL.
  BETTERNAT_PROVIDER_MIRROR_DIR    Mirror work dir. Default: tmp/provider-mirror/<version>-<os>-<arch>-<pid>.
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    exit 0
  fi
  restore_shell_options
  return 0
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    if [ "${BASH_SOURCE[0]}" = "$0" ]; then
      exit 2
    fi
    return 2
  fi
}

require_cmd awk
require_cmd curl
require_cmd sha256sum

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
provider_version="${BETTERNAT_PROVIDER_VERSION:-0.1.0-alpha.3}"
host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
host_arch="$(uname -m)"

case "$host_arch" in
  x86_64) host_arch="amd64" ;;
  aarch64|arm64) host_arch="arm64" ;;
esac

provider_release_base="${BETTERNAT_PROVIDER_RELEASE_BASE:-https://github.com/nowakeai/terraform-provider-betternat/releases/download/v$provider_version}"
mirror_root="${BETTERNAT_PROVIDER_MIRROR_DIR:-$root_dir/tmp/provider-mirror/$provider_version-$host_os-$host_arch-$$}"
mirror_dir="$mirror_root/registry.terraform.io/nowakeai/betternat"
tf_cli_config="$mirror_root/terraform.tfrc"
provider_zip="terraform-provider-betternat_${provider_version}_${host_os}_${host_arch}.zip"
provider_sums="terraform-provider-betternat_${provider_version}_SHA256SUMS"

mkdir -p "$mirror_dir"
curl -fsSL "$provider_release_base/$provider_sums" -o "$mirror_dir/$provider_sums"
curl -fsSL "$provider_release_base/$provider_zip" -o "$mirror_dir/$provider_zip"

(
  cd "$mirror_dir"
  awk -v f="$provider_zip" '$2 == f {print}' "$provider_sums" > SHA256SUMS.selected
  if [ ! -s SHA256SUMS.selected ]; then
    echo "missing checksum entry for provider artifact $provider_zip" >&2
    exit 1
  fi
  sha256sum -c SHA256SUMS.selected
)

cat >"$tf_cli_config" <<EOF
provider_installation {
  filesystem_mirror {
    path    = "$mirror_root"
    include = ["registry.terraform.io/nowakeai/betternat"]
  }

  direct {
    exclude = ["registry.terraform.io/nowakeai/betternat"]
  }
}
EOF

export TF_CLI_CONFIG_FILE="$tf_cli_config"

cat <<EOF
BETTERNAT_PROVIDER_VERSION=$provider_version
BETTERNAT_PROVIDER_MIRROR_DIR=$mirror_root
TF_CLI_CONFIG_FILE=$TF_CLI_CONFIG_FILE

Run this in the same shell before terraform init:

  export TF_CLI_CONFIG_FILE="$TF_CLI_CONFIG_FILE"
EOF

restore_shell_options
