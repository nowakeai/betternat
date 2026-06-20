#!/usr/bin/env bash
set -euo pipefail

IMAGE="${BETTERNAT_LOXILB_IMAGE:-ghcr.io/loxilb-io/loxilb:latest}"
CONTAINER="${BETTERNAT_LOXILB_CONTAINER:-betternat-loxilb-smoke}"
RULE="${BETTERNAT_LOXILB_RULE:-sourceIP:10.255.0.0/16,destinationIP:0.0.0.0/0,preference:32000}"
SNAT_TO="${BETTERNAT_LOXILB_SNAT_TO:-127.0.0.1}"

if [[ "${EUID}" -eq 0 ]]; then
  SUDO=()
else
  SUDO=(sudo)
fi

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

check_loxicmd() {
  local cmd=("$@")
  if "${cmd[@]}" get lbversion -o json >/tmp/betternat-loxilb-version.json 2>/tmp/betternat-loxilb-version.err; then
    if ! grep -Eq '\{|\[' /tmp/betternat-loxilb-version.json; then
      echo "loxilb smoke failed: loxicmd did not return JSON" >&2
      cat /tmp/betternat-loxilb-version.json >&2
      exit 1
    fi
    return 0
  fi
  return 1
}

if has_cmd loxicmd; then
  if check_loxicmd loxicmd; then
    echo "loxilb smoke ok"
    exit 0
  fi
  echo "loxilb smoke: host loxicmd exists but no local LoxiLB service is reachable; trying container runtime" >&2
  cat /tmp/betternat-loxilb-version.err >&2 || true
fi

RUNTIME=""
if has_cmd podman; then
  RUNTIME="podman"
elif has_cmd docker; then
  RUNTIME="docker"
fi

if [[ -z "$RUNTIME" ]]; then
  echo "loxilb smoke skipped: loxicmd/docker/podman not available in this VM" >&2
  exit 77
fi

cleanup() {
  "${SUDO[@]}" "$RUNTIME" rm -f "$CONTAINER" >/dev/null 2>&1 || true
  "${SUDO[@]}" rm -f /tmp/betternat-loxilb-*.json /tmp/betternat-loxilb-*.err /tmp/betternat-loxilb-container.id >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup

"${SUDO[@]}" "$RUNTIME" run -d \
  --name "$CONTAINER" \
  --privileged \
  --network host \
  -v /lib/modules:/lib/modules:ro \
  "$IMAGE" --api --fallback >/tmp/betternat-loxilb-container.id

for _ in $(seq 1 30); do
  "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd get lbversion -o json >/tmp/betternat-loxilb-version.json 2>/tmp/betternat-loxilb-version.err || true
  if grep -Eq '\{|\[' /tmp/betternat-loxilb-version.json; then
    "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd create firewall --firewallRule="$RULE" --snat="$SNAT_TO" --egress >/tmp/betternat-loxilb-create.json
    "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd get firewall -o json >/tmp/betternat-loxilb-firewall.json
    if ! grep -q '10.255.0.0/16' /tmp/betternat-loxilb-firewall.json; then
      echo "loxilb smoke failed: created SNAT rule was not visible in firewall output" >&2
      cat /tmp/betternat-loxilb-firewall.json >&2
      exit 1
    fi
    "${SUDO[@]}" "$RUNTIME" exec "$CONTAINER" loxicmd delete firewall --firewallRule="$RULE" >/tmp/betternat-loxilb-delete.json
    echo "loxilb smoke ok"
    exit 0
  fi
  sleep 1
done

echo "loxilb smoke failed: container started but loxicmd did not become ready" >&2
"${SUDO[@]}" "$RUNTIME" logs "$CONTAINER" >&2 || true
cat /tmp/betternat-loxilb-version.err >&2 || true
exit 1
