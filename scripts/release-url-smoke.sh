#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage:
  BETTERNAT_VERSION=v0.1.0-alpha.2 scripts/release-url-smoke.sh

Environment:
  BETTERNAT_VERSION       Release tag to verify. Required unless passed as $1.
  BETTERNAT_RELEASE_BASE  Override release base URL.
  BETTERNAT_SMOKE_ARCH    Target architecture. Defaults to arm64.
  BETTERNAT_SMOKE_DIR     Output directory. Defaults to tmp/release-url-smoke/<version>-<arch>.
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

arch="${BETTERNAT_SMOKE_ARCH:-arm64}"
case "$arch" in
  amd64|arm64) ;;
  *)
    echo "unsupported BETTERNAT_SMOKE_ARCH: $arch" >&2
    exit 2
    ;;
esac

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_base="${BETTERNAT_RELEASE_BASE:-https://github.com/nowakeai/betternat/releases/download/$version}"
out_dir="${BETTERNAT_SMOKE_DIR:-$root_dir/tmp/release-url-smoke/$version-linux-$arch}"

require_cmd curl
require_cmd awk

if command -v sha256sum >/dev/null 2>&1; then
  sha256_verify=(sha256sum -c)
elif command -v shasum >/dev/null 2>&1; then
  sha256_verify=(shasum -a 256 -c)
else
  echo "missing required command: sha256sum or shasum" >&2
  exit 2
fi

mkdir -p "$out_dir"

agent="betternat-agent_${version}_linux_${arch}"
cli="betternat_${version}_linux_${arch}"

echo "release base: $release_base"
echo "output dir: $out_dir"

curl -fsSL "$release_base/SHA256SUMS" -o "$out_dir/SHA256SUMS"

agent_sha="$(awk -v f="$agent" '$2 == f {print $1}' "$out_dir/SHA256SUMS")"
cli_sha="$(awk -v f="$cli" '$2 == f {print $1}' "$out_dir/SHA256SUMS")"

if [ -z "$agent_sha" ]; then
  echo "missing checksum entry for $agent" >&2
  exit 1
fi
if [ -z "$cli_sha" ]; then
  echo "missing checksum entry for $cli" >&2
  exit 1
fi

curl -fsSL "$release_base/$agent" -o "$out_dir/$agent"
curl -fsSL "$release_base/$cli" -o "$out_dir/$cli"

(
  cd "$out_dir"
  printf '%s  %s\n' "$agent_sha" "$agent" > SHA256SUMS.selected
  printf '%s  %s\n' "$cli_sha" "$cli" >> SHA256SUMS.selected
  "${sha256_verify[@]}" SHA256SUMS.selected
)

chmod +x "$out_dir/$cli"

host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
host_arch="$(uname -m)"
case "$host_arch" in
  x86_64) host_arch="amd64" ;;
  aarch64|arm64) host_arch="arm64" ;;
esac

if [ "$host_os" = "linux" ] && [ "$host_arch" = "$arch" ]; then
  "$out_dir/$cli" version
else
  echo "skipping CLI execution on host ${host_os}/${host_arch}; downloaded target is linux/${arch}"
fi

cat <<EOF
Release URL smoke passed.
agent_binary_url=$release_base/$agent
agent_binary_sha256=$agent_sha
cli_binary_url=$release_base/$cli
cli_binary_sha256=$cli_sha
EOF
