#!/usr/bin/env bash
set -euo pipefail

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

require_cmd ip
require_cmd nft
require_cmd conntrack
require_cmd nc
require_cmd timeout

CLIENT_NS="${BETTERNAT_SMOKE_CLIENT_NS:-bn-udp-client}"
GW_NS="${BETTERNAT_SMOKE_GW_NS:-bn-udp-gw}"
SERVER_NS="${BETTERNAT_SMOKE_SERVER_NS:-bn-udp-server}"
TABLE_NAME="${BETTERNAT_SMOKE_TABLE:-betternat_udp_smoke}"
REQUEST_FILE="/tmp/betternat-udp-smoke-request"

cleanup() {
  "${SUDO[@]}" ip netns exec "$GW_NS" nft delete table ip "$TABLE_NAME" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$CLIENT_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$GW_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$SERVER_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" rm -f "$REQUEST_FILE" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup

"${SUDO[@]}" ip netns add "$CLIENT_NS"
"${SUDO[@]}" ip netns add "$GW_NS"
"${SUDO[@]}" ip netns add "$SERVER_NS"

"${SUDO[@]}" ip link add bn-uc type veth peer name bn-ugc
"${SUDO[@]}" ip link set bn-uc netns "$CLIENT_NS"
"${SUDO[@]}" ip link set bn-ugc netns "$GW_NS"

"${SUDO[@]}" ip link add bn-ugs type veth peer name bn-us
"${SUDO[@]}" ip link set bn-ugs netns "$GW_NS"
"${SUDO[@]}" ip link set bn-us netns "$SERVER_NS"

"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip addr add 10.251.1.2/24 dev bn-uc
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set bn-uc up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip route add default via 10.251.1.1

"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.251.1.1/24 dev bn-ugc
"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.251.2.1/24 dev bn-ugs
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-ugc up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-ugs up
"${SUDO[@]}" ip netns exec "$GW_NS" sysctl -q -w net.ipv4.ip_forward=1

"${SUDO[@]}" ip netns exec "$SERVER_NS" ip addr add 10.251.2.2/24 dev bn-us
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set bn-us up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip route add default via 10.251.2.1

"${SUDO[@]}" ip netns exec "$GW_NS" nft -f - <<NFT
table ip $TABLE_NAME {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 10.251.1.0/24 oifname "bn-ugs" counter masquerade
  }
}
NFT

"${SUDO[@]}" ip netns exec "$SERVER_NS" sh -c "timeout 3 nc -u -l -p 15353 > '$REQUEST_FILE'" &
SERVER_PID=$!
sleep 0.3

"${SUDO[@]}" ip netns exec "$CLIENT_NS" sh -c 'printf "dns-like-query\n" | nc -u -w 1 10.251.2.2 15353'
wait "$SERVER_PID" >/dev/null 2>&1 || true

if ! grep -q '^dns-like-query$' "$REQUEST_FILE"; then
  echo "udp smoke failed: server did not receive client payload" >&2
  [[ -f "$REQUEST_FILE" ]] && cat "$REQUEST_FILE" >&2
  exit 1
fi

RULES="$("${SUDO[@]}" ip netns exec "$GW_NS" nft list table ip "$TABLE_NAME")"
if ! grep -Eq 'counter packets [1-9][0-9]* bytes [1-9][0-9]* masquerade' <<<"$RULES"; then
  echo "nftables UDP counter did not increase" >&2
  echo "$RULES" >&2
  exit 1
fi

CONNTRACK_OUTPUT="$("${SUDO[@]}" ip netns exec "$GW_NS" conntrack -L -p udp 2>/dev/null || true)"
if ! grep -q '10.251.2.2' <<<"$CONNTRACK_OUTPUT"; then
  echo "conntrack UDP output did not include expected flow" >&2
  echo "$CONNTRACK_OUTPUT" >&2
  exit 1
fi

echo "nftables udp smoke ok"
