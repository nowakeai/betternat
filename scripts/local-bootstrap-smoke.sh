#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd go
require_cmd bash

export GOCACHE="${GOCACHE:-$ROOT_DIR/tmp/go-build}"

go test ./internal/bootstrap

echo "bootstrap render smoke ok"
