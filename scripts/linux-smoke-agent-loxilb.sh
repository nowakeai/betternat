#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${BETTERNAT_LOXILB_IMAGE:-ghcr.io/loxilb-io/loxilb:latest}"
CONTAINER="${BETTERNAT_LOXILB_CONTAINER:-betternat-agent-loxilb-smoke}"

if [[ "${EUID}" -eq 0 ]]; then
  SUDO=()
else
  SUDO=(sudo)
fi

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

RUNTIME=""
if has_cmd podman; then
  RUNTIME="podman"
elif has_cmd docker; then
  RUNTIME="docker"
fi

if [[ -z "$RUNTIME" ]]; then
  echo "agent loxilb smoke skipped: docker/podman not available in this VM" >&2
  exit 77
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd go

BIN="$ROOT_DIR/tmp/betternat-agent-loxilb-smoke"
CONFIG_FILE="$ROOT_DIR/tmp/betternat-agent-loxilb-smoke.json"
WRAPPER_DIR="$ROOT_DIR/tmp/loxilb-smoke-bin"
JSON_OUT="$ROOT_DIR/tmp/betternat-agent-loxilb-smoke.out.json"
PROM_OUT="$ROOT_DIR/tmp/betternat-agent-loxilb-smoke.prom"

cleanup() {
  "${SUDO[@]}" "$RUNTIME" rm -f "$CONTAINER" >/dev/null 2>&1 || true
  "${SUDO[@]}" rm -f /tmp/betternat-agent-loxilb-*.json /tmp/betternat-agent-loxilb-*.err /tmp/betternat-agent-loxilb-container.id >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "$ROOT_DIR/tmp" "$WRAPPER_DIR"
cleanup

GOCACHE="${GOCACHE:-$ROOT_DIR/tmp/go-build}" go build -o "$BIN" "$ROOT_DIR/cmd/betternat-agent"

"${SUDO[@]}" "$RUNTIME" run -d \
  --name "$CONTAINER" \
  --privileged \
  --network host \
  -v /lib/modules:/lib/modules:ro \
  "$IMAGE" --api --fallback >/tmp/betternat-agent-loxilb-container.id

for _ in $(seq 1 30); do
  "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd get lbversion -o json >/tmp/betternat-agent-loxilb-version.json 2>/tmp/betternat-agent-loxilb-version.err || true
  if grep -Eq '\{|\[' /tmp/betternat-agent-loxilb-version.json; then
    break
  fi
  sleep 1
done

if ! grep -Eq '\{|\[' /tmp/betternat-agent-loxilb-version.json; then
  echo "agent loxilb smoke failed: LoxiLB did not become ready" >&2
  "${SUDO[@]}" "$RUNTIME" logs "$CONTAINER" >&2 || true
  cat /tmp/betternat-agent-loxilb-version.err >&2 || true
  exit 1
fi

cat >"$WRAPPER_DIR/loxicmd" <<SH
#!/usr/bin/env bash
exec ${SUDO[*]:-} "$RUNTIME" exec "$CONTAINER" loxicmd "\$@"
SH
chmod +x "$WRAPPER_DIR/loxicmd"

cat >"$CONFIG_FILE" <<'JSON'
{
  "version": "v0",
  "gateway_id": "local-loxilb-smoke",
  "ha_group_id": "local-loxilb-smoke-a",
  "cloud": "local",
  "region": "local",
  "local": {
    "instance_id": "vm-agent-loxilb",
    "availability_zone": "local-a",
    "primary_interface": "lo"
  },
  "datapath": {
    "engine": "loxilb",
    "private_cidrs": ["10.255.0.0/16"],
    "loxilb": {
      "api_address": "127.0.0.1",
      "api_port": 11111,
      "snat_to": "127.0.0.1",
      "snat_interface": "lo",
      "rule_preference_base": 32100,
      "reconcile_interval_seconds": 10
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
          "name": "local-loxilb-smoke",
          "cidrs": ["10.255.0.0/16"]
        }
      ]
    }
  }
}
JSON

PATH="$WRAPPER_DIR:$PATH" "$BIN" --config "$CONFIG_FILE" --once >"$JSON_OUT"
PATH="$WRAPPER_DIR:$PATH" "$BIN" --config "$CONFIG_FILE" --once --prometheus >"$PROM_OUT"

if ! grep -q '"engine":"loxilb"' "$JSON_OUT"; then
  echo "agent loxilb smoke failed: JSON output did not report loxilb engine" >&2
  cat "$JSON_OUT" >&2
  exit 1
fi

if ! "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd get firewall -o json | grep -q '10.255.0.0/16'; then
  echo "agent loxilb smoke failed: expected LoxiLB SNAT rule was not created" >&2
  "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd get firewall -o json >&2 || true
  exit 1
fi

if ! grep -q 'betternat_datapath_ready{engine="loxilb",gateway="local-loxilb-smoke",ha_group="local-loxilb-smoke-a"} 1' "$PROM_OUT"; then
  echo "agent loxilb smoke failed: Prometheus datapath readiness metric missing" >&2
  cat "$PROM_OUT" >&2
  exit 1
fi

if ! grep -q 'betternat_loxilb_rule_present{cidr="10.255.0.0/16",engine="loxilb",gateway="local-loxilb-smoke",ha_group="local-loxilb-smoke-a"} 1' "$PROM_OUT"; then
  echo "agent loxilb smoke failed: Prometheus LoxiLB rule metric missing" >&2
  cat "$PROM_OUT" >&2
  exit 1
fi

echo "agent loxilb smoke ok"
