#!/usr/bin/env bash
set -u

probe_url="${BETTERNAT_PROBE_URL:-https://checkip.amazonaws.com}"
samples="${BETTERNAT_PROBE_SAMPLES:-240}"
interval_seconds="${BETTERNAT_PROBE_INTERVAL_SECONDS:-0.25}"
connect_timeout_seconds="${BETTERNAT_PROBE_CONNECT_TIMEOUT_SECONDS:-1}"
max_time_seconds="${BETTERNAT_PROBE_MAX_TIME_SECONDS:-2}"
expected_ip="${BETTERNAT_EXPECTED_IP:-}"
output_path="${BETTERNAT_PROBE_OUTPUT:-}"

usage() {
  cat <<'USAGE' >&2
Usage:
  BETTERNAT_PROBE_SAMPLES=240 scripts/egress-probe-monitor.sh

Environment:
  BETTERNAT_PROBE_URL                     Probe URL. Default: https://checkip.amazonaws.com
  BETTERNAT_PROBE_SAMPLES                 Number of samples. Default: 240
  BETTERNAT_PROBE_INTERVAL_SECONDS        Sleep between samples. Default: 0.25
  BETTERNAT_PROBE_CONNECT_TIMEOUT_SECONDS curl connect timeout. Default: 1
  BETTERNAT_PROBE_MAX_TIME_SECONDS        curl total timeout. Default: 2
  BETTERNAT_EXPECTED_IP                   Optional expected public IP.
  BETTERNAT_PROBE_OUTPUT                  Optional path for TSV sample output.

Output columns:
  timestamp	status	observed_ip	rc	error
USAGE
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
  "")
    ;;
  *)
    usage
    exit 2
    ;;
esac

if ! command -v curl >/dev/null 2>&1; then
  echo "betternat egress probe monitor: curl is required" >&2
  exit 127
fi

if ! [[ "$samples" =~ ^[0-9]+$ ]] || [ "$samples" -lt 1 ]; then
  echo "betternat egress probe monitor: BETTERNAT_PROBE_SAMPLES must be a positive integer" >&2
  exit 2
fi

tmp_err="$(mktemp "${TMPDIR:-/tmp}/betternat-egress-probe.XXXXXX")"
cleanup() {
  rm -f "$tmp_err"
}
trap cleanup EXIT

write_sample() {
  if [ -n "$output_path" ]; then
    printf '%s\t%s\t%s\t%s\t%s\n' "$@" >>"$output_path"
  else
    printf '%s\t%s\t%s\t%s\t%s\n' "$@"
  fi
}

if [ -n "$output_path" ]; then
  : >"$output_path"
fi

total=0
ok=0
failed=0
unexpected=0
current_fail_run=0
longest_fail_run=0
first_ip=""
last_ip=""
switches=0

for i in $(seq 1 "$samples"); do
  ts="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
  body="$(curl -fsS --connect-timeout "$connect_timeout_seconds" --max-time "$max_time_seconds" "$probe_url" 2>"$tmp_err")"
  rc=$?
  total=$((total + 1))

  if [ "$rc" -eq 0 ]; then
    observed_ip="$(printf '%s' "$body" | awk 'NF {print $1; exit}')"
    ok=$((ok + 1))
    current_fail_run=0

    if [ -z "$first_ip" ]; then
      first_ip="$observed_ip"
    elif [ "$observed_ip" != "$last_ip" ]; then
      switches=$((switches + 1))
    fi
    last_ip="$observed_ip"

    if [ -n "$expected_ip" ] && [ "$observed_ip" != "$expected_ip" ]; then
      unexpected=$((unexpected + 1))
      write_sample "$ts" "unexpected" "$observed_ip" "$rc" ""
    else
      write_sample "$ts" "ok" "$observed_ip" "$rc" ""
    fi
  else
    failed=$((failed + 1))
    current_fail_run=$((current_fail_run + 1))
    if [ "$current_fail_run" -gt "$longest_fail_run" ]; then
      longest_fail_run="$current_fail_run"
    fi
    err="$(tr '\r\n\t' '   ' <"$tmp_err" | sed 's/[[:space:]][[:space:]]*/ /g; s/^ //; s/ $//')"
    write_sample "$ts" "fail" "" "$rc" "$err"
  fi

  if [ "$i" -lt "$samples" ]; then
    sleep "$interval_seconds"
  fi
done

cat >&2 <<SUMMARY
betternat egress probe summary
  url: $probe_url
  samples: $total
  ok: $ok
  failed: $failed
  unexpected_ip: $unexpected
  longest_consecutive_failures: $longest_fail_run
  first_ip: ${first_ip:-unknown}
  last_ip: ${last_ip:-unknown}
  ip_switches: $switches
SUMMARY
