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

NS="${BETTERNAT_SMOKE_NS:-bn-cleanup}"

cleanup() {
  "${SUDO[@]}" ip netns del "$NS" >/dev/null 2>&1 || true
}
trap cleanup EXIT

cleanup
"${SUDO[@]}" ip netns add "$NS"

"${SUDO[@]}" ip netns exec "$NS" nft -f - <<'NFT'
table inet keepalive_user {
  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    counter
  }
}

table inet betternat {
  chain betternat_postrouting {
    type nat hook postrouting priority srcnat; policy accept;
    ip saddr 10.252.0.0/16 counter masquerade comment "betternat:10.252.0.0/16"
  }
}
NFT

"${SUDO[@]}" ip netns exec "$NS" nft delete table inet betternat

if "${SUDO[@]}" ip netns exec "$NS" nft list table inet betternat >/dev/null 2>&1; then
  echo "cleanup safety failed: BetterNAT table still exists" >&2
  exit 1
fi

if ! "${SUDO[@]}" ip netns exec "$NS" nft list table inet keepalive_user >/dev/null 2>&1; then
  echo "cleanup safety failed: unrelated user table was removed" >&2
  exit 1
fi

echo "nftables cleanup safety ok"
