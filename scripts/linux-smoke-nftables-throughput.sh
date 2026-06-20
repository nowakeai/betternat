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
require_cmd iperf3
require_cmd awk

CLIENT_NS="${BETTERNAT_SMOKE_CLIENT_NS:-bn-perf-client}"
GW_NS="${BETTERNAT_SMOKE_GW_NS:-bn-perf-gw}"
SERVER_NS="${BETTERNAT_SMOKE_SERVER_NS:-bn-perf-server}"
TABLE_NAME="${BETTERNAT_SMOKE_TABLE:-betternat_perf_smoke}"
OUT_FILE="/tmp/betternat-iperf3.out"

cleanup() {
  "${SUDO[@]}" ip netns exec "$SERVER_NS" pkill iperf3 >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns exec "$GW_NS" nft delete table ip "$TABLE_NAME" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$CLIENT_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$GW_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" ip netns del "$SERVER_NS" >/dev/null 2>&1 || true
  "${SUDO[@]}" rm -f "$OUT_FILE" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup

"${SUDO[@]}" ip netns add "$CLIENT_NS"
"${SUDO[@]}" ip netns add "$GW_NS"
"${SUDO[@]}" ip netns add "$SERVER_NS"

"${SUDO[@]}" ip link add bn-pc type veth peer name bn-pgc
"${SUDO[@]}" ip link set bn-pc netns "$CLIENT_NS"
"${SUDO[@]}" ip link set bn-pgc netns "$GW_NS"

"${SUDO[@]}" ip link add bn-pgs type veth peer name bn-ps
"${SUDO[@]}" ip link set bn-pgs netns "$GW_NS"
"${SUDO[@]}" ip link set bn-ps netns "$SERVER_NS"

"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip addr add 10.254.1.2/24 dev bn-pc
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip link set bn-pc up
"${SUDO[@]}" ip netns exec "$CLIENT_NS" ip route add default via 10.254.1.1

"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.254.1.1/24 dev bn-pgc
"${SUDO[@]}" ip netns exec "$GW_NS" ip addr add 10.254.2.1/24 dev bn-pgs
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-pgc up
"${SUDO[@]}" ip netns exec "$GW_NS" ip link set bn-pgs up
"${SUDO[@]}" ip netns exec "$GW_NS" sysctl -q -w net.ipv4.ip_forward=1

"${SUDO[@]}" ip netns exec "$SERVER_NS" ip addr add 10.254.2.2/24 dev bn-ps
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set lo up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip link set bn-ps up
"${SUDO[@]}" ip netns exec "$SERVER_NS" ip route add default via 10.254.2.1

"${SUDO[@]}" ip netns exec "$GW_NS" nft -f - <<NFT
table ip $TABLE_NAME {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 10.254.1.0/24 oifname "bn-pgs" counter masquerade
  }
}
NFT

"${SUDO[@]}" ip netns exec "$SERVER_NS" iperf3 -s -1 >/tmp/betternat-iperf3-server.out 2>&1 &
SERVER_PID=$!
sleep 0.5

"${SUDO[@]}" ip netns exec "$CLIENT_NS" iperf3 -c 10.254.2.2 -t "${BETTERNAT_IPERF_SECONDS:-2}" -f m >"$OUT_FILE"
wait "$SERVER_PID" >/dev/null 2>&1 || true

THROUGHPUT_MBIT="$(awk '/receiver/ {print $(NF-2)}' "$OUT_FILE" | tail -n 1)"
if [[ -z "$THROUGHPUT_MBIT" ]]; then
  echo "throughput smoke failed: could not parse iperf3 receiver throughput" >&2
  cat "$OUT_FILE" >&2
  exit 1
fi

if ! awk -v value="$THROUGHPUT_MBIT" 'BEGIN { exit !(value > 0) }'; then
  echo "throughput smoke failed: non-positive throughput: $THROUGHPUT_MBIT Mbits/sec" >&2
  cat "$OUT_FILE" >&2
  exit 1
fi

RULES="$("${SUDO[@]}" ip netns exec "$GW_NS" nft list table ip "$TABLE_NAME")"
if ! grep -Eq 'counter packets [1-9][0-9]* bytes [1-9][0-9]* masquerade' <<<"$RULES"; then
  echo "throughput smoke failed: nftables counter did not increase" >&2
  echo "$RULES" >&2
  exit 1
fi

echo "nftables throughput smoke ok: ${THROUGHPUT_MBIT} Mbits/sec"
