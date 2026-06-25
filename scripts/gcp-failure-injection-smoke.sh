#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/gcp-failure-injection-smoke.sh --project PROJECT --zone ZONE --name NAME

Required:
  --project PROJECT       GCP project ID.
  --zone ZONE             GCE zone for the disposable BetterNAT run.
  --name NAME             BetterNAT run name.

Optional:
  --route ROUTE_NAME      Default: NAME-default-via-gw.
  --ssh-mode MODE         iap or external. Default: iap.
  --output-dir DIR        Evidence directory. Default: tmp/gcp-failure-NAME-TIMESTAMP.
  --wait SECONDS          Degradation wait window. Default: 90.

This script assumes a disposable betternat_gcp_gateway fixture already exists.
It injects a reversible local iptables OUTPUT reject for tcp:443 on the current
active gateway, verifies the active agent reports DEGRADED instead of ACTIVE
while Firestore/Compute API access is unavailable, removes the rule, and
captures recovery evidence.
EOF
}

project_id=""
zone=""
name=""
route_name=""
ssh_mode="iap"
output_dir=""
wait_seconds="90"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project) project_id="${2:-}"; shift 2 ;;
    --zone) zone="${2:-}"; shift 2 ;;
    --name) name="${2:-}"; shift 2 ;;
    --route) route_name="${2:-}"; shift 2 ;;
    --ssh-mode) ssh_mode="${2:-}"; shift 2 ;;
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    --wait) wait_seconds="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$project_id" || -z "$zone" || -z "$name" ]]; then
  usage >&2
  exit 2
fi
case "$ssh_mode" in
  iap|external) ;;
  *) echo "unsupported --ssh-mode: $ssh_mode" >&2; exit 2 ;;
esac
if ! [[ "$wait_seconds" =~ ^[0-9]+$ ]] || [[ "$wait_seconds" -lt 10 ]]; then
  echo "--wait must be an integer >= 10" >&2
  exit 2
fi

route_name="${route_name:-${name}-default-via-gw}"
timestamp="$(date -u +%Y%m%d%H%M%S)"
output_dir="${output_dir:-tmp/gcp-failure-${name}-${timestamp}}"
mkdir -p "$output_dir"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd gcloud
require_cmd python3

gcloud_base=(gcloud --quiet --project "$project_id")
ssh_base=("${gcloud_base[@]}" compute ssh --zone "$zone")
if [[ "$ssh_mode" == "iap" ]]; then
  ssh_base+=(--tunnel-through-iap)
fi

route_target() {
  "${gcloud_base[@]}" compute routes describe "$route_name" \
    --format='value(nextHopInstance.basename())'
}

run_ssh() {
  local instance="$1"
  shift
  "${ssh_base[@]}" "$instance" --command "$*"
}

capture_json() {
  local path="$1"
  shift
  "$@" --format=json >"$path"
}

injection_active=""
cleanup_rule() {
  if [[ -n "$injection_active" ]]; then
    run_ssh "$injection_active" "sudo iptables -D OUTPUT -p tcp --dport 443 -j REJECT 2>/dev/null || true" >/dev/null 2>&1 || true
  fi
}
trap cleanup_rule EXIT

echo "BetterNAT GCP failure-injection smoke"
echo "project: $project_id"
echo "zone: $zone"
echo "name: $name"
echo "route: $route_name"
echo "ssh_mode: $ssh_mode"
echo "output: $output_dir"

active="$(route_target)"
if [[ -z "$active" ]]; then
  echo "route $route_name has no nextHopInstance" >&2
  exit 1
fi
standby=""
while IFS= read -r candidate; do
  if [[ "$candidate" != "$active" ]]; then
    standby="$candidate"
    break
  fi
done < <("${gcloud_base[@]}" compute instances list --filter "name~${name}-gw" --format='value(name)' | sort)
if [[ -z "$standby" ]]; then
  echo "could not find standby gateway" >&2
  exit 1
fi

echo "$active" >"$output_dir/active-before.txt"
echo "$standby" >"$output_dir/standby-before.txt"
capture_json "$output_dir/route-before.json" "${gcloud_base[@]}" compute routes describe "$route_name"
run_ssh "$active" "sudo /usr/local/bin/betternat status --host unix:///run/betternat/agent.sock --output json" \
  >"$output_dir/active-daemon-status-before.json"
run_ssh "$standby" "sudo /usr/local/bin/betternat status --host unix:///run/betternat/agent.sock --output json" \
  >"$output_dir/standby-daemon-status-before.json"

injection_active="$active"
run_ssh "$active" "sudo iptables -C OUTPUT -p tcp --dport 443 -j REJECT 2>/dev/null || sudo iptables -I OUTPUT 1 -p tcp --dport 443 -j REJECT; sudo ss -K state established '( dport = :443 )' || true; sudo iptables -S OUTPUT" \
  >"$output_dir/local-egress-reject-create.txt"
date -u +%FT%TZ >"$output_dir/injection-start.txt"

deadline=$((SECONDS + wait_seconds))
degraded_seen="0"
while [[ "$SECONDS" -lt "$deadline" ]]; do
  run_ssh "$active" "sudo journalctl -u betternat-agent --since '2 minutes ago' --no-pager | tail -200" \
    >"$output_dir/active-agent-recent.log" || true
  if grep -q 'betternat_ha_step state=DEGRADED' "$output_dir/active-agent-recent.log"; then
    degraded_seen="1"
    break
  fi
  sleep 3
done
run_ssh "$active" "sudo /usr/local/bin/betternat status --host unix:///run/betternat/agent.sock --output json" \
  >"$output_dir/active-daemon-status-during.json" || true
capture_json "$output_dir/route-during.json" "${gcloud_base[@]}" compute routes describe "$route_name"

run_ssh "$active" "sudo iptables -D OUTPUT -p tcp --dport 443 -j REJECT 2>/dev/null || true; sudo iptables -S OUTPUT" \
  >"$output_dir/local-egress-reject-delete.txt"
injection_active=""
date -u +%FT%TZ >"$output_dir/injection-end.txt"

sleep 20
final_target="$(route_target || true)"
echo "$final_target" >"$output_dir/route-target-final.txt"
if [[ -n "$final_target" ]]; then
  run_ssh "$final_target" "sudo /usr/local/bin/betternat status --host unix:///run/betternat/agent.sock --output json" \
    >"$output_dir/final-active-daemon-status.json" || true
  run_ssh "$final_target" "sudo /usr/local/bin/betternat doctor --live --config /etc/betternat/agent.json" \
    >"$output_dir/final-active-doctor.txt" || true
fi
capture_json "$output_dir/route-final.json" "${gcloud_base[@]}" compute routes describe "$route_name"

if [[ "$degraded_seen" != "1" ]]; then
  echo "active agent did not report DEGRADED within ${wait_seconds}s" >&2
  exit 1
fi

cat >"$output_dir/SUMMARY.md" <<EOF
# GCP Failure-Injection Smoke

Date: $(date -u +%F)
Project: $project_id
Zone: $zone
Name: $name

## Result

- Active before: $active
- Standby before: $standby
- Injected rule: local iptables OUTPUT tcp/443 REJECT on $active
- DEGRADED observed: yes
- Final route target: ${final_target:-unknown}

## Evidence

- \`active-agent-recent.log\`
- \`active-daemon-status-before.json\`
- \`active-daemon-status-during.json\`
- \`route-before.json\`
- \`route-during.json\`
- \`route-final.json\`
- \`local-egress-reject-create.txt\`
- \`local-egress-reject-delete.txt\`
- \`final-active-daemon-status.json\`
- \`final-active-doctor.txt\`
EOF

echo "GCP failure-injection smoke passed"
grep 'betternat_ha_step state=DEGRADED' "$output_dir/active-agent-recent.log" | tail -5
