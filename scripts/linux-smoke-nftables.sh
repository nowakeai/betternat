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

CLIENT_NS="${BETTERNAT_SMOKE_CLIENT_NS:-bn-client}"
GW_NS="${BETTERNAT_SMOKE_GW_NS:-bn-gw}"
SERVER_NS="${BETTERNAT_SMOKE_SERVER_NS:-bn-server}"
TABLE_NAME="${BETTERNAT_SMOKE_TABLE:-betternat_smoke}"

cleanup() {
  "${SUDO[@]}" ip netns exec "$GW_NS" nft delete table ip "$TABLE_NAME" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$CLIENT_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$GW_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$SERVER_NS" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup

"${SUDO[@]}" ip netns add "$CLIENT_NS"
"${SUDO[@]}" ip netns add "$GW_NS"
"${SUDO[@]}" ip netns add "$SERVER_NS"

"${SUDO[@]}" ip link add bn-c type veth peer name bn-gc
"${SUDO[@]}" ip link set bn-c netns "$CLIENT_NS"
"${SUDO[@]}" ip link set bn-gc netns "$GW_NS"

"${SUDO[@]}" ip link add bn-gs type veth peer name bn-s
"${SUDO[@]}" ip link set bn-gs netns "$GW_NS"
"${SUDO[@]}" ip link set bn-s netns "$SERVER_NS"

"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip addr add 10.250.1.2/24 dev bn-c
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set bn-c up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip route add default via 10.250.1.1

"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.250.1.1/24 dev bn-gc
"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.250.2.1/24 dev bn-gs
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-gc up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-gs up
"${SUDO[@]}" ip netns exec "$GW_NS" sysctl -q -w net.ipv4.ip_forward=1

"${SUDO[@]}" ip netns exec "$SERVER_NS" ip addr add 10.250.2.2/24 dev bn-s
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set bn-s up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip route add default via 10.250.2.1

"${SUDO[@]}" ip netns exec "$GW_NS" nft -f - <<NFT
table ip $TABLE_NAME {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 10.250.1.0/24 oifname "bn-gs" counter masquerade
  }
}
NFT

"${SUDO[@]}" ip netns exec "$SERVER_NS" sh -c 'printf "ok\n" | nc -l -p 18080 >/tmp/betternat-smoke-request' &
SERVER_PID=$!
sleep 0.3

"${SUDO[@]}" ip netns exec "$CLIENT_NS" sh -c 'printf "hello\n" | nc -w 2 10.250.2.2 18080' >/tmp/betternat-smoke-response
wait "$SERVER_PID" >/dev/null 2>&1 || true

if ! grep -q '^ok$' /tmp/betternat-smoke-response; then
  echo "tcp smoke failed: missing server response" >&2
  exit 1
fi

if ! grep -q '^hello$' /tmp/betternat-smoke-request; then
  echo "tcp smoke failed: server did not receive client payload" >&2
  exit 1
fi

RULES="$("${SUDO[@]}" ip netns exec "$GW_NS" nft list table ip "$TABLE_NAME")"
if ! grep -Eq 'counter packets [1-9][0-9]* bytes [1-9][0-9]* masquerade' <<<"$RULES"; then
  echo "nftables counter did not increase" >&2
  echo "$RULES" >&2
  exit 1
fi

CONNTRACK_OUTPUT="$("${SUDO[@]}" ip netns exec "$GW_NS" conntrack -L -p tcp 2>/dev/null || true)"
if ! grep -q '10.250.2.2' <<<"$CONNTRACK_OUTPUT"; then
  echo "conntrack output did not include expected flow" >&2
  echo "$CONNTRACK_OUTPUT" >&2
  exit 1
fi

echo "nftables smoke ok"
