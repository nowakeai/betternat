#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "${EUID}" -eq 0 ]]; then
  SUDO=()
else
  SUDO=(sudo)
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd go
require_cmd ip
require_cmd nft
require_cmd conntrack

NS="${BETTERNAT_SMOKE_NS:-bn-agent-nft}"
BIN="$ROOT_DIR/tmp/betternat-agent-linux-smoke"
CONFIG_FILE="$ROOT_DIR/tmp/betternat-agent-nftables-smoke.json"
JSON_OUT="$ROOT_DIR/tmp/betternat-agent-nftables-smoke.out.json"
PROM_OUT="$ROOT_DIR/tmp/betternat-agent-nftables-smoke.prom"

cleanup() {
  "${SUDO[@]}" ip netns exec "$NS" nft delete table inet betternat >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$NS" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "$ROOT_DIR/tmp"
cleanup

GOCACHE="${GOCACHE:-$ROOT_DIR/tmp/go-build}" go build -o "$BIN" "$ROOT_DIR/cmd/betternat-agent"

cat >"$CONFIG_FILE" <<'JSON'
{
  "version": "v0",
  "gateway_id": "local-vm-smoke",
  "ha_group_id": "local-vm-smoke-a",
  "cloud": "local",
  "region": "local",
  "local": {
    "instance_id": "vm-agent-nftables",
    "availability_zone": "local-a",
    "primary_interface": "lo"
  },
  "datapath": {
    "engine": "nftables",
    "private_cidrs": ["10.253.0.0/16"],
    "nftables": {
      "table_name": "betternat",
      "chain_prefix": "betternat"
    }
  },
  "ha": {
    "enabled": false
  },
  "observability": {
    "prometheus": {
      "listen_address": "127.0.0.1",
      "listen_port": 9108
    },
    "attribution": {
      "owners": [
        {
          "name": "local-smoke",
          "cidrs": ["10.253.0.0/16"]
        }
      ]
    }
  }
}
JSON

"${SUDO[@]}" ip netns add "$NS"
"${SUDO[@]}" ip netns exec "$NS" "$BIN" --config "$CONFIG_FILE" --once >"$JSON_OUT"
"${SUDO[@]}" ip netns exec "$NS" "$BIN" --config "$CONFIG_FILE" --once --prometheus >"$PROM_OUT"

if ! grep -q '"engine":"nftables"' "$JSON_OUT"; then
  echo "agent nftables smoke failed: JSON output did not report nftables engine" >&2
  cat "$JSON_OUT" >&2
  exit 1
fi

if ! "${SUDO[@]}" ip netns exec "$NS" nft list table inet betternat | grep -q 'comment "betternat:10.253.0.0/16"'; then
  echo "agent nftables smoke failed: expected nftables rule was not created" >&2
  "${SUDO[@]}" ip netns exec "$NS" nft list ruleset >&2 || true
  exit 1
fi

if ! grep -q 'betternat_datapath_ready{engine="nftables",gateway="local-vm-smoke",ha_group="local-vm-smoke-a"} 1' "$PROM_OUT"; then
  echo "agent nftables smoke failed: Prometheus datapath readiness metric missing" >&2
  cat "$PROM_OUT" >&2
  exit 1
fi

if ! grep -q 'betternat_owner_packets_total{direction="egress",gateway="local-vm-smoke",ha_group="local-vm-smoke-a",owner="local-smoke"}' "$PROM_OUT"; then
  echo "agent nftables smoke failed: Prometheus owner metric missing" >&2
  cat "$PROM_OUT" >&2
  exit 1
fi

echo "agent nftables smoke ok"
