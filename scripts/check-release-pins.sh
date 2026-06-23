#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BETTERNAT_VERSION="v0.1.0-alpha.2"
LOXILB_VERSION="v0.9.8.6"
LOXILB_IMAGE="ghcr.io/loxilb-io/loxilb@sha256:38f08be39aaa57826cbfb818c34442e34b0e456f9f88a74265c4a298208862cb"
LOXILB_PACKAGE_URL="https://github.com/orgs/loxilb-io/packages/container/loxilb/960366893?tag=v0.9.8.6"

require_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -Fq "$expected" "$file"; then
    echo "release pin check failed: $file does not contain expected value:" >&2
    echo "  $expected" >&2
    exit 1
  fi
}

require_absent() {
  local file="$1"
  local unexpected="$2"
  if grep -Fq "$unexpected" "$file"; then
    echo "release pin check failed: $file contains stale value:" >&2
    echo "  $unexpected" >&2
    exit 1
  fi
}

for file in \
  internal/bootstrap/bootstrap.go \
  internal/bootstrap/bootstrap_test.go \
  packer/betternat.pkr.hcl \
  scripts/ami/provision-betternat-ami.sh \
  docs/release/DEPENDENCY_PINS.md \
  THIRD_PARTY_NOTICES.md; do
  require_contains "$file" "$LOXILB_IMAGE"
done

require_contains docs/release/DEPENDENCY_PINS.md "$BETTERNAT_VERSION"
require_contains docs/release/DEPENDENCY_PINS.md "$LOXILB_VERSION"
require_contains docs/release/DEPENDENCY_PINS.md "$LOXILB_PACKAGE_URL"
require_contains THIRD_PARTY_NOTICES.md "$LOXILB_VERSION"

require_absent internal/bootstrap/bootstrap.go "ghcr.io/loxilb-io/loxilb:latest"
require_absent packer/betternat.pkr.hcl "ghcr.io/loxilb-io/loxilb:latest"
require_absent scripts/ami/provision-betternat-ami.sh "ghcr.io/loxilb-io/loxilb:latest"

echo "release pins ok: BetterNAT $BETTERNAT_VERSION -> LoxiLB $LOXILB_VERSION"
