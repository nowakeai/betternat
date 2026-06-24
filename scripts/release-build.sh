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
require_cmd git

export GOCACHE="${GOCACHE:-$ROOT_DIR/tmp/go-build-cache}"

if command -v shasum >/dev/null 2>&1; then
  SHA256_CMD=(shasum -a 256)
elif command -v sha256sum >/dev/null 2>&1; then
  SHA256_CMD=(sha256sum)
else
  echo "missing required command: shasum or sha256sum" >&2
  exit 2
fi

VERSION="${BETTERNAT_VERSION:-v0.1.0-alpha.2}"
COMMIT="${BETTERNAT_COMMIT:-$(git rev-parse --short=12 HEAD)}"
BUILD_DATE="${BETTERNAT_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
HOST_GOOS="$(go env GOOS)"
HOST_GOARCH="$(go env GOARCH)"
OUT_DIR="${BETTERNAT_RELEASE_DIR:-$ROOT_DIR/tmp/release/$VERSION}"

mkdir -p "$OUT_DIR"

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X github.com/nowakeai/betternat/internal/buildinfo.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/nowakeai/betternat/internal/buildinfo.Commit=$COMMIT"
LDFLAGS="$LDFLAGS -X github.com/nowakeai/betternat/internal/buildinfo.Date=$BUILD_DATE"

build_go() {
  local goos="$1"
  local goarch="$2"
  local package="$3"
  local output="$4"

  echo "building $output"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT_DIR/$output" "$package"
}

build_go "$HOST_GOOS" "$HOST_GOARCH" ./cmd/betternat "betternat_${VERSION}_${HOST_GOOS}_${HOST_GOARCH}"
build_go "$HOST_GOOS" "$HOST_GOARCH" ./cmd/terraform-provider-betternat "terraform-provider-betternat_${VERSION}_${HOST_GOOS}_${HOST_GOARCH}"
build_go linux arm64 ./cmd/betternat "betternat_${VERSION}_linux_arm64"
build_go linux amd64 ./cmd/betternat "betternat_${VERSION}_linux_amd64"
build_go linux arm64 ./cmd/betternat-agent "betternat-agent_${VERSION}_linux_arm64"
build_go linux amd64 ./cmd/betternat-agent "betternat-agent_${VERSION}_linux_amd64"

(
  cd "$OUT_DIR"
  "${SHA256_CMD[@]}" betternat_* betternat-agent_* terraform-provider-betternat_* > SHA256SUMS
)

cat > "$OUT_DIR/manifest.json" <<EOF
{
  "version": "$VERSION",
  "commit": "$COMMIT",
  "build_date": "$BUILD_DATE",
  "go_version": "$(go version)",
  "host": {
    "goos": "$HOST_GOOS",
    "goarch": "$HOST_GOARCH"
  },
  "artifacts": [
    {
      "name": "betternat_${VERSION}_${HOST_GOOS}_${HOST_GOARCH}",
      "package": "./cmd/betternat",
      "goos": "$HOST_GOOS",
      "goarch": "$HOST_GOARCH"
    },
    {
      "name": "terraform-provider-betternat_${VERSION}_${HOST_GOOS}_${HOST_GOARCH}",
      "package": "./cmd/terraform-provider-betternat",
      "goos": "$HOST_GOOS",
      "goarch": "$HOST_GOARCH"
    },
    {
      "name": "betternat_${VERSION}_linux_arm64",
      "package": "./cmd/betternat",
      "goos": "linux",
      "goarch": "arm64"
    },
    {
      "name": "betternat_${VERSION}_linux_amd64",
      "package": "./cmd/betternat",
      "goos": "linux",
      "goarch": "amd64"
    },
    {
      "name": "betternat-agent_${VERSION}_linux_arm64",
      "package": "./cmd/betternat-agent",
      "goos": "linux",
      "goarch": "arm64"
    },
    {
      "name": "betternat-agent_${VERSION}_linux_amd64",
      "package": "./cmd/betternat-agent",
      "goos": "linux",
      "goarch": "amd64"
    }
  ]
}
EOF

echo "release artifacts written to $OUT_DIR"
