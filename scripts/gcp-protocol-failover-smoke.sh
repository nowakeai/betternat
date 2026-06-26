#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/gcp-protocol-failover-smoke.sh --project PROJECT --zone ZONE --name NAME

Required:
  --project PROJECT       GCP project ID.
  --zone ZONE             GCE zone for the disposable BetterNAT run.
  --name NAME             BetterNAT run name.

Optional:
  --route ROUTE_NAME      Default: NAME-default-via-gw.
  --client CLIENT_NAME    Default: NAME-client.
  --mode MODE             proactive or passive-stop. Default: proactive.
  --samples N             Background TCP probe samples. Default: 80.
  --interval SECONDS      Background probe interval. Default: 0.5.
  --ssh-mode MODE         iap or external. Default: iap.
  --client-access MODE    auto, direct, or proxy-gateway. Default: auto.
  --client-proxy-gateway INSTANCE
                          Gateway for proxy-gateway client access. Default:
                          current route target.
  --output-dir DIR        Evidence directory. Default: tmp/gcp-protocol-NAME-TIMESTAMP.

This script assumes a disposable betternat_gcp_gateway fixture already exists.
It does not create infrastructure and it does not clean up. For passive-stop
mode it stops the current route target instance to trigger takeover; the caller
must restart or destroy the fixture afterwards.
EOF
}

project_id=""
zone=""
name=""
route_name=""
client_name=""
mode="proactive"
samples="80"
interval="0.5"
output_dir=""
ssh_mode="iap"
client_access="auto"
client_proxy_gateway=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project) project_id="${2:-}"; shift 2 ;;
    --zone) zone="${2:-}"; shift 2 ;;
    --name) name="${2:-}"; shift 2 ;;
    --route) route_name="${2:-}"; shift 2 ;;
    --client) client_name="${2:-}"; shift 2 ;;
    --mode) mode="${2:-}"; shift 2 ;;
    --samples) samples="${2:-}"; shift 2 ;;
    --interval) interval="${2:-}"; shift 2 ;;
    --ssh-mode) ssh_mode="${2:-}"; shift 2 ;;
    --client-access) client_access="${2:-}"; shift 2 ;;
    --client-proxy-gateway) client_proxy_gateway="${2:-}"; shift 2 ;;
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "$project_id" || -z "$zone" || -z "$name" ]]; then
  usage >&2
  exit 2
fi
case "$mode" in
  proactive|passive-stop) ;;
  *) echo "unsupported --mode: $mode" >&2; exit 2 ;;
esac
case "$ssh_mode" in
  iap|external) ;;
  *) echo "unsupported --ssh-mode: $ssh_mode" >&2; exit 2 ;;
esac
case "$client_access" in
  auto|direct|proxy-gateway) ;;
  *) echo "unsupported --client-access: $client_access" >&2; exit 2 ;;
esac
if ! [[ "$samples" =~ ^[0-9]+$ ]] || [[ "$samples" -lt 1 ]]; then
  echo "--samples must be a positive integer" >&2
  exit 2
fi

route_name="${route_name:-${name}-default-via-gw}"
client_name="${client_name:-${name}-client}"
timestamp="$(date -u +%Y%m%d%H%M%S)"
output_dir="${output_dir:-tmp/gcp-protocol-${name}-${timestamp}}"
mkdir -p "$output_dir"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd gcloud
require_cmd python3
require_cmd ssh

gcloud_base=(gcloud --quiet --project "$project_id")
ssh_base=("${gcloud_base[@]}" compute ssh --zone "$zone")
if [[ "$ssh_mode" == "iap" ]]; then
  ssh_base+=(--tunnel-through-iap)
fi

route_target() {
  "${gcloud_base[@]}" compute routes describe "$route_name" \
    --format='value(nextHopInstance.basename())'
}

instance_private_ip() {
  local instance="$1"
  "${gcloud_base[@]}" compute instances describe "$instance" --zone "$zone" \
    --format='value(networkInterfaces[0].networkIP)'
}

instance_external_ip() {
  local instance="$1"
  "${gcloud_base[@]}" compute instances describe "$instance" --zone "$zone" \
    --format='value(networkInterfaces[0].accessConfigs[0].natIP)'
}

instance_numeric_id() {
  local instance="$1"
  "${gcloud_base[@]}" compute instances describe "$instance" --zone "$zone" \
    --format='value(id)'
}

gcloud_ssh_field() {
  local instance="$1"
  local internal_flag="${2:-}"
  local field="$3"
  local dry_run
  if [[ "$internal_flag" == "internal" ]]; then
    dry_run="$("${gcloud_base[@]}" compute ssh "$instance" --zone "$zone" --internal-ip --dry-run | tail -n1)"
  else
    dry_run="$("${gcloud_base[@]}" compute ssh "$instance" --zone "$zone" --dry-run | tail -n1)"
  fi
  python3 - "$field" "$dry_run" <<'PY'
import shlex
import sys

field = sys.argv[1]
args = shlex.split(sys.argv[2])
if field == "user":
    print(args[-1].split("@", 1)[0])
elif field == "identity":
    print(args[args.index("-i") + 1])
elif field == "known_hosts":
    for idx, arg in enumerate(args):
        if arg == "-o" and idx + 1 < len(args) and args[idx + 1].startswith("UserKnownHostsFile="):
            print(args[idx + 1].split("=", 1)[1])
            break
    else:
        raise SystemExit("UserKnownHostsFile not found")
else:
    raise SystemExit(f"unsupported field {field}")
PY
}

run_ssh() {
  local instance="$1"
  shift
  "${ssh_base[@]}" "$instance" --command "$*"
}

run_client_ssh() {
  local access="$client_access"
  if [[ "$access" == "auto" ]]; then
    if [[ "$ssh_mode" == "iap" ]]; then
      access="direct"
    else
      access="proxy-gateway"
    fi
  fi

  if [[ "$access" == "direct" ]]; then
    run_ssh "$client_name" "$@"
    return
  fi

  local gateway="${client_proxy_gateway:-${active:-}}"
  if [[ -z "$gateway" ]]; then
    echo "client proxy gateway is not known yet" >&2
    return 1
  fi

  local user key_file known_hosts client_ip gateway_ip client_id gateway_id
  user="$(gcloud_ssh_field "$client_name" internal user)"
  key_file="$(gcloud_ssh_field "$client_name" internal identity)"
  known_hosts="$(gcloud_ssh_field "$client_name" internal known_hosts)"
  client_ip="$(instance_private_ip "$client_name")"
  gateway_ip="$(instance_external_ip "$gateway")"
  client_id="$(instance_numeric_id "$client_name")"
  gateway_id="$(instance_numeric_id "$gateway")"
  if [[ -z "$gateway_ip" ]]; then
    echo "proxy gateway $gateway has no external IP" >&2
    return 1
  fi

  local proxy_command
  proxy_command="ssh -i ${key_file} -o CheckHostIP=no -o HashKnownHosts=no -o HostKeyAlias=compute.${gateway_id} -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=${known_hosts} -W %h:%p ${user}@${gateway_ip}"
  ssh -i "$key_file" \
    -o CheckHostIP=no \
    -o HashKnownHosts=no \
    -o "HostKeyAlias=compute.${client_id}" \
    -o IdentitiesOnly=yes \
    -o StrictHostKeyChecking=yes \
    -o "UserKnownHostsFile=${known_hosts}" \
    -o "ProxyCommand=${proxy_command}" \
    "${user}@${client_ip}" "$*"
}

capture_json() {
  local path="$1"
  shift
  "$@" --format=json >"$path"
}

remote_protocol_probe=$(cat <<'REMOTE'
set -euo pipefail
echo "probe_start=$(date -u +%FT%TZ)"
echo "tcp_checkip=$(curl -fsS --connect-timeout 2 --max-time 5 https://checkip.amazonaws.com | tr -d '\r\n')"
echo "tcp_https_status=$(curl -fsS -o /dev/null -w '%{http_code}' --connect-timeout 2 --max-time 8 https://example.com)"
python3 - <<'PY'
import random
import socket
import struct

query_id = random.randrange(0, 65536)
packet = struct.pack("!HHHHHH", query_id, 0x0100, 1, 0, 0, 0)
for label in "example.com".split("."):
    packet += bytes([len(label)]) + label.encode("ascii")
packet += b"\x00" + struct.pack("!HH", 1, 1)
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.settimeout(3)
sock.sendto(packet, ("8.8.8.8", 53))
data, addr = sock.recvfrom(512)
rid, flags, qd, an, ns, ar = struct.unpack("!HHHHHH", data[:12])
if rid != query_id or an < 1:
    raise SystemExit("udp_dns=fail")
print(f"udp_dns=ok answers={an} responder={addr[0]}")
PY
bytes="$(curl -fsSL --connect-timeout 2 --max-time 20 'https://speed.cloudflare.com/__down?bytes=1048576' | wc -c | tr -d ' ')"
echo "download_bytes=$bytes"
test "$bytes" -ge 1048576
echo "probe_end=$(date -u +%FT%TZ)"
REMOTE
)

remote_background_probe=$(cat <<'REMOTE'
set -uo pipefail
out="/tmp/betternat-gcp-failover-probe.tsv"
: >"$out"
samples="${BETTERNAT_PROBE_SAMPLES}"
interval="${BETTERNAT_PROBE_INTERVAL}"
for i in $(seq 1 "$samples"); do
  ts="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
  err="/tmp/betternat-gcp-failover-probe.err"
  body="$(curl -fsS --connect-timeout 1 --max-time 2 https://checkip.amazonaws.com 2>"$err")"
  rc=$?
  if [ "$rc" -eq 0 ]; then
    ip="$(printf '%s' "$body" | awk 'NF {print $1; exit}')"
    printf '%s\tok\t%s\t%s\t\n' "$ts" "$ip" "$rc" >>"$out"
  else
    msg="$(tr '\r\n\t' '   ' <"$err" | sed 's/[[:space:]][[:space:]]*/ /g; s/^ //; s/ $//')"
    printf '%s\tfail\t\t%s\t%s\n' "$ts" "$rc" "$msg" >>"$out"
  fi
  sleep "$interval"
done
REMOTE
)

summarize_probe() {
  local input="$1"
  python3 - "$input" <<'PY'
import sys
from collections import Counter

path = sys.argv[1]
rows = []
with open(path, "r", encoding="utf-8") as fh:
    for line in fh:
        parts = line.rstrip("\n").split("\t")
        if len(parts) >= 4:
            rows.append(parts)
statuses = Counter(row[1] for row in rows)
ips = [row[2] for row in rows if row[1] == "ok" and row[2]]
switches = 0
last = None
for ip in ips:
    if last is not None and ip != last:
        switches += 1
    last = ip
longest = cur = 0
first_fail = last_fail = None
for idx, row in enumerate(rows):
    if row[1] == "ok":
        cur = 0
    else:
        if first_fail is None:
            first_fail = idx
        last_fail = idx
        cur += 1
        longest = max(longest, cur)
print(f"samples={len(rows)}")
print(f"ok={statuses.get('ok', 0)}")
print(f"failed={len(rows) - statuses.get('ok', 0)}")
print(f"first_ip={ips[0] if ips else 'unknown'}")
print(f"last_ip={ips[-1] if ips else 'unknown'}")
print(f"ip_switches={switches}")
print(f"longest_consecutive_failures={longest}")
print(f"first_fail_index={first_fail if first_fail is not None else 'none'}")
print(f"last_fail_index={last_fail if last_fail is not None else 'none'}")
if first_fail is not None and last_fail is not None:
    print(f"failure_window_samples={last_fail - first_fail + 1}")
PY
}

echo "BetterNAT GCP protocol failover smoke"
echo "project: $project_id"
echo "zone: $zone"
echo "name: $name"
echo "route: $route_name"
echo "client: $client_name"
echo "mode: $mode"
echo "ssh_mode: $ssh_mode"
echo "client_access: $client_access"
echo "output: $output_dir"

capture_json "$output_dir/instances-before.json" "${gcloud_base[@]}" compute instances list --filter "name~${name}"
capture_json "$output_dir/route-before.json" "${gcloud_base[@]}" compute routes describe "$route_name"

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
if [[ -z "$standby" && "$mode" == "proactive" ]]; then
  echo "could not find standby gateway for proactive handover" >&2
  exit 1
fi

echo "$active" >"$output_dir/active-before.txt"
echo "$standby" >"$output_dir/standby-before.txt"
echo "active_before=$active"
echo "standby_before=${standby:-none}"
if [[ -z "$client_proxy_gateway" ]]; then
  client_proxy_gateway="$active"
fi
echo "client_proxy_gateway=$client_proxy_gateway"

run_client_ssh "$remote_protocol_probe" >"$output_dir/client-protocol-before.txt"

env_probe="BETTERNAT_PROBE_SAMPLES=$samples BETTERNAT_PROBE_INTERVAL=$interval"
run_client_ssh "nohup env $env_probe bash -lc $(printf '%q' "$remote_background_probe") >/tmp/betternat-gcp-failover-probe.log 2>&1 & echo \$!" >"$output_dir/client-probe-pid.txt"
sleep 3

if [[ "$mode" == "proactive" ]]; then
  echo "trigger=proactive_handover" | tee "$output_dir/failover-trigger.txt"
  run_ssh "$active" "sudo betternat handover start --to '$standby' --host unix:///run/betternat/agent.sock" \
    >"$output_dir/handover-command.txt"
else
  echo "trigger=passive_stop" | tee "$output_dir/failover-trigger.txt"
  "${gcloud_base[@]}" compute instances stop "$active" --zone "$zone" >"$output_dir/stop-active.txt"
fi

deadline=$((SECONDS + 90))
new_target=""
while [[ "$SECONDS" -lt "$deadline" ]]; do
  new_target="$(route_target || true)"
  if [[ -n "$new_target" && "$new_target" != "$active" ]]; then
    break
  fi
  sleep 2
done
echo "$new_target" >"$output_dir/route-target-after.txt"
if [[ -z "$new_target" || "$new_target" == "$active" ]]; then
  echo "route target did not move away from $active" >&2
  capture_json "$output_dir/route-after-failed.json" "${gcloud_base[@]}" compute routes describe "$route_name"
  exit 1
fi
if [[ "$client_access" == "proxy-gateway" || ( "$client_access" == "auto" && "$ssh_mode" != "iap" ) ]]; then
  client_proxy_gateway="$new_target"
  echo "$client_proxy_gateway" >"$output_dir/client-proxy-gateway-after.txt"
  run_ssh "$client_proxy_gateway" "true" >"$output_dir/client-proxy-gateway-after-ssh-warmup.txt" || true
fi

sleep "$(python3 - "$samples" "$interval" <<'PY'
import math
import sys
samples = int(sys.argv[1])
interval = float(sys.argv[2])
print(max(1, math.ceil(samples * interval) + 3))
PY
)"
run_client_ssh "cat /tmp/betternat-gcp-failover-probe.tsv" >"$output_dir/client-probe.tsv"
summarize_probe "$output_dir/client-probe.tsv" >"$output_dir/client-probe-summary.txt"

run_client_ssh "$remote_protocol_probe" >"$output_dir/client-protocol-after.txt"
capture_json "$output_dir/route-after.json" "${gcloud_base[@]}" compute routes describe "$route_name"
capture_json "$output_dir/instances-after.json" "${gcloud_base[@]}" compute instances list --filter "name~${name}"

if [[ "$mode" == "proactive" ]]; then
  run_ssh "$new_target" "sudo betternat status --direct --config /etc/betternat/agent.json --output json" \
    >"$output_dir/status-after.json" || true
  run_ssh "$new_target" "sudo betternat doctor --live --config /etc/betternat/agent.json" \
    >"$output_dir/doctor-after.txt" || true
fi

cat >"$output_dir/SUMMARY.md" <<EOF
# GCP Protocol Failover Smoke

Date: $(date -u +%F)
Project: $project_id
Zone: $zone
Name: $name
Mode: $mode
SSH mode: $ssh_mode
Client access: $client_access

## Result

- Active before: $active
- Standby before: ${standby:-none}
- Route target after: $new_target

## Probe Summary

\`\`\`text
$(cat "$output_dir/client-probe-summary.txt")
\`\`\`

## Protocol Checks

- Before: \`client-protocol-before.txt\`
- After: \`client-protocol-after.txt\`

## Evidence

- \`route-before.json\`
- \`route-after.json\`
- \`instances-before.json\`
- \`instances-after.json\`
- \`client-probe.tsv\`
- \`status-after.json\`
- \`doctor-after.txt\`
EOF

echo "GCP protocol failover smoke passed"
cat "$output_dir/client-probe-summary.txt"
