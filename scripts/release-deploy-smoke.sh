#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage:
  BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-deploy-smoke.sh

By default this runs init, validate, and plan only. To create a disposable AWS
environment, set:

  BETTERNAT_RELEASE_DEPLOY_APPLY=1

Apply mode destroys the environment before exit unless:

  BETTERNAT_RELEASE_DEPLOY_KEEP=1

Environment:
  BETTERNAT_VERSION                Release tag to verify. Required unless passed as $1.
  BETTERNAT_RELEASE_BASE           Override GitHub release base URL.
  BETTERNAT_RELEASE_DEPLOY_APPLY   Set to 1 to run terraform apply.
  BETTERNAT_RELEASE_DEPLOY_KEEP    Set to 1 to skip destroy after apply.
  BETTERNAT_RELEASE_DEPLOY_REGION  AWS region. Default: us-west-2.
  BETTERNAT_RELEASE_DEPLOY_AZ      AWS availability zone. Default: us-west-2a.
  BETTERNAT_RELEASE_DEPLOY_RUN_ID  Run id. Default: bnat-release-smoke-<timestamp>.
  BETTERNAT_RELEASE_DEPLOY_DIR     Work dir. Default: tmp/release-deploy-smoke/<run-id>.
  BETTERNAT_PROVIDER_VERSION       Terraform provider version. Default: 0.1.0-alpha.6.
  BETTERNAT_PROVIDER_INSTALL       github-mirror or registry. Default: github-mirror.
  BETTERNAT_PROVIDER_RELEASE_BASE  Override provider release base URL.
  BETTERNAT_INSTANCE_TYPE          Gateway instance type. Default: t4g.small.
  BETTERNAT_MIN_SIZE               Gateway ASG min size. Default: 1.
  BETTERNAT_DESIRED_CAPACITY       Gateway ASG desired capacity. Default: 2.
  BETTERNAT_MAX_SIZE               Gateway ASG max size. Default: 3.
  BETTERNAT_STABLE_EGRESS_IP       Stable EIP mode. Default: true.
  BETTERNAT_HA_PROFILE             HA profile. Default: default.
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

version="${1:-${BETTERNAT_VERSION:-}}"
if [ -z "$version" ]; then
  usage
  exit 2
fi

case "$version" in
  v*) ;;
  *)
    echo "release version must start with v: $version" >&2
    exit 2
    ;;
esac

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
timestamp="$(date -u +%Y%m%d%H%M%S)"
run_id="${BETTERNAT_RELEASE_DEPLOY_RUN_ID:-bnat-release-smoke-$timestamp}"
work_dir="${BETTERNAT_RELEASE_DEPLOY_DIR:-$root_dir/tmp/release-deploy-smoke/$run_id}"
artifact_dir="$work_dir/artifacts"
tf_dir="$work_dir/terraform"
region="${BETTERNAT_RELEASE_DEPLOY_REGION:-us-west-2}"
az="${BETTERNAT_RELEASE_DEPLOY_AZ:-us-west-2a}"
instance_type="${BETTERNAT_INSTANCE_TYPE:-t4g.small}"
min_size="${BETTERNAT_MIN_SIZE:-1}"
desired_capacity="${BETTERNAT_DESIRED_CAPACITY:-2}"
max_size="${BETTERNAT_MAX_SIZE:-3}"
stable_egress_ip="${BETTERNAT_STABLE_EGRESS_IP:-true}"
ha_profile="${BETTERNAT_HA_PROFILE:-default}"
provider_version="${BETTERNAT_PROVIDER_VERSION:-0.1.0-alpha.6}"
provider_install="${BETTERNAT_PROVIDER_INSTALL:-github-mirror}"

require_cmd awk
require_cmd curl
require_cmd perl
require_cmd terraform

mkdir -p "$artifact_dir" "$tf_dir"
exec > >(tee "$work_dir/run.log") 2>&1

echo "release deploy smoke run id: $run_id"
echo "work dir: $work_dir"
echo "log: $work_dir/run.log"
echo "provider version: $provider_version"
echo "provider install: $provider_install"

release_output="$(
  BETTERNAT_SMOKE_ARCH=arm64 \
  BETTERNAT_SMOKE_DIR="$artifact_dir" \
  BETTERNAT_RELEASE_BASE="${BETTERNAT_RELEASE_BASE:-}" \
  "$root_dir/scripts/release-url-smoke.sh" "$version"
)"
printf '%s\n' "$release_output"

agent_binary_url="$(printf '%s\n' "$release_output" | awk -F= '$1 == "agent_binary_url" {print $2; exit}')"
agent_binary_sha256="$(printf '%s\n' "$release_output" | awk -F= '$1 == "agent_binary_sha256" {print $2; exit}')"
cli_binary_url="$(printf '%s\n' "$release_output" | awk -F= '$1 == "cli_binary_url" {print $2; exit}')"
cli_binary_sha256="$(printf '%s\n' "$release_output" | awk -F= '$1 == "cli_binary_sha256" {print $2; exit}')"

if [ -z "$agent_binary_url" ] || [ -z "$agent_binary_sha256" ] || [ -z "$cli_binary_url" ] || [ -z "$cli_binary_sha256" ]; then
  echo "failed to parse release artifact URLs/checksums from release-url-smoke output" >&2
  exit 1
fi

cp "$root_dir/examples/terraform-aws-supplemental/main.tf" "$tf_dir/main.tf"
perl -0pi -e 's#(betternat = \{\n\s*source\s*=\s*")[^"]+(")#$1registry.terraform.io/nowakeai/betternat$2#; s#(betternat = \{.*?version\s*=\s*")= [^"]+(")#$1= '"$provider_version"'$2#s' "$tf_dir/main.tf"

case "$provider_install" in
  github-mirror)
    if ! command -v sha256sum >/dev/null 2>&1; then
      echo "missing required command: sha256sum" >&2
      exit 2
    fi

    host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    host_arch="$(uname -m)"
    case "$host_arch" in
      x86_64) host_arch="amd64" ;;
      aarch64|arm64) host_arch="arm64" ;;
    esac

    provider_release_base="${BETTERNAT_PROVIDER_RELEASE_BASE:-https://github.com/nowakeai/terraform-provider-betternat/releases/download/v$provider_version}"
    provider_zip="terraform-provider-betternat_${provider_version}_${host_os}_${host_arch}.zip"
    provider_sums="terraform-provider-betternat_${provider_version}_SHA256SUMS"
    provider_mirror="$work_dir/provider-mirror"
    provider_mirror_dir="$provider_mirror/registry.terraform.io/nowakeai/betternat"
    tf_cli_config="$work_dir/terraform.tfrc"

    mkdir -p "$provider_mirror_dir"
    curl -fsSL "$provider_release_base/$provider_sums" -o "$provider_mirror_dir/$provider_sums"
    curl -fsSL "$provider_release_base/$provider_zip" -o "$provider_mirror_dir/$provider_zip"
    (
      cd "$provider_mirror_dir"
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
    path    = "$provider_mirror"
    include = ["registry.terraform.io/nowakeai/betternat"]
  }

  direct {
    exclude = ["registry.terraform.io/nowakeai/betternat"]
  }
}
EOF
    export TF_CLI_CONFIG_FILE="$tf_cli_config"
    ;;
  registry)
    ;;
  *)
    echo "unsupported BETTERNAT_PROVIDER_INSTALL: $provider_install" >&2
    exit 2
    ;;
esac

terraform_args=(
  -var "region=$region"
  -var "az=$az"
  -var "run_id=$run_id"
  -var "instance_type=$instance_type"
  -var "min_size=$min_size"
  -var "desired_capacity=$desired_capacity"
  -var "max_size=$max_size"
  -var "stable_egress_ip=$stable_egress_ip"
  -var "ha_profile=$ha_profile"
  -var "agent_binary_url=$agent_binary_url"
  -var "agent_binary_sha256=$agent_binary_sha256"
  -var "cli_binary_url=$cli_binary_url"
  -var "cli_binary_sha256=$cli_binary_sha256"
)

cleanup() {
  if [ "${apply_started:-0}" = "1" ] && [ "${BETTERNAT_RELEASE_DEPLOY_KEEP:-0}" != "1" ]; then
    echo "destroying release deploy smoke environment: $run_id"
    terraform -chdir="$tf_dir" destroy -auto-approve -input=false "${terraform_args[@]}"
  fi
}
trap cleanup EXIT

terraform -chdir="$tf_dir" init -upgrade -input=false
terraform -chdir="$tf_dir" validate
terraform -chdir="$tf_dir" plan -input=false -out="$work_dir/plan.tfplan" "${terraform_args[@]}"

if [ "${BETTERNAT_RELEASE_DEPLOY_APPLY:-0}" = "1" ]; then
  apply_started=1
  terraform -chdir="$tf_dir" apply -input=false -auto-approve "$work_dir/plan.tfplan"
  terraform -chdir="$tf_dir" output
else
  echo "plan-only release deploy smoke passed. Set BETTERNAT_RELEASE_DEPLOY_APPLY=1 to create and destroy a disposable AWS environment."
fi
