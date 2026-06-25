#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/gcp-residual-scan.sh --project PROJECT_ID --name BETTERNAT_NAME [--database DATABASE_ID]

Environment:
  BETTERNAT_GCP_PROJECT
      Default project when --project is omitted.

  BETTERNAT_GCP_NAME
      Default BetterNAT run name when --name is omitted.

  BETTERNAT_GCP_FIRESTORE_DATABASE
      Default Firestore database ID when --database is omitted.

This is a read-only residual scan. It does not delete resources.

Exit codes:
  0  no BetterNAT residual resources were found
  1  residual resources were found
  2  invalid usage or missing local tooling
EOF
}

project_id="${BETTERNAT_GCP_PROJECT:-}"
run_name="${BETTERNAT_GCP_NAME:-}"
database_id="${BETTERNAT_GCP_FIRESTORE_DATABASE:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      project_id="${2:-}"
      shift 2
      ;;
    --name)
      run_name="${2:-}"
      shift 2
      ;;
    --database)
      database_id="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$project_id" ]]; then
  echo "missing --project or BETTERNAT_GCP_PROJECT" >&2
  exit 2
fi
if [[ -z "$run_name" ]]; then
  echo "missing --name or BETTERNAT_GCP_NAME" >&2
  exit 2
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd gcloud
require_cmd python3
require_cmd curl

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "BetterNAT GCP residual scan"
echo "project: $project_id"
echo "name: $run_name"

scan_json() {
  local label="$1"
  local output_file="$2"
  shift 2
  echo "scan: $label"
  if "$@" --format=json >"$output_file"; then
    python3 - "$label" "$output_file" <<'PY'
import json
import sys

label = sys.argv[1]
path = sys.argv[2]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
count = len(data) if isinstance(data, list) else (1 if data else 0)
print(f"  {label}: {count}")
if count:
    for item in data if isinstance(data, list) else [data]:
        name = item.get("name") or item.get("email") or item.get("id") or item.get("selfLink") or "<unknown>"
        print(f"    {name}")
PY
  else
    echo "  warning: failed to scan $label" >&2
    printf '[]\n' >"$output_file"
  fi
}

scan_json "instances" "$tmp_dir/instances.json" \
  gcloud --project "$project_id" compute instances list --filter "name~${run_name}"
scan_json "routes" "$tmp_dir/routes.json" \
  gcloud --project "$project_id" compute routes list --filter "name~${run_name}"
scan_json "firewall-rules" "$tmp_dir/firewalls.json" \
  gcloud --project "$project_id" compute firewall-rules list --filter "name~${run_name}"
scan_json "addresses" "$tmp_dir/addresses.json" \
  gcloud --project "$project_id" compute addresses list --filter "name~${run_name}"
scan_json "service-accounts" "$tmp_dir/service-accounts.json" \
  gcloud --project "$project_id" iam service-accounts list --filter "email~${run_name}"

firestore_records_file="$tmp_dir/firestore-records.json"
printf '[]\n' >"$firestore_records_file"

if [[ -n "$database_id" ]]; then
  echo "scan: firestore records"
  token="$(gcloud auth print-access-token)"
  request_json="$(RUN_NAME="$run_name" python3 - <<'PY'
import json
import os

print(json.dumps({
    "structuredQuery": {
        "from": [{"collectionId": "records", "allDescendants": True}],
        "limit": 500,
    }
}))
PY
)"
  firestore_response="$tmp_dir/firestore-response.json"
  if curl -fsS \
    -H "Authorization: Bearer ${token}" \
    -H "Content-Type: application/json" \
    -X POST \
    "https://firestore.googleapis.com/v1/projects/${project_id}/databases/${database_id}/documents:runQuery" \
    -d "$request_json" >"$firestore_response"; then
    RUN_NAME="$run_name" RESPONSE_FILE="$firestore_response" OUTPUT_FILE="$firestore_records_file" python3 - <<'PY'
import json
import os

run_name = os.environ["RUN_NAME"]
response_file = os.environ["RESPONSE_FILE"]
output_file = os.environ["OUTPUT_FILE"]
with open(response_file, "r", encoding="utf-8") as fh:
    rows = json.load(fh)
records = []
for row in rows:
    doc = row.get("document")
    if not doc:
        continue
    name = doc.get("name", "")
    if f"/gateways/{run_name}/" in name:
        records.append({"name": name})
with open(output_file, "w", encoding="utf-8") as fh:
    json.dump(records, fh)
print(f"  firestore records: {len(records)}")
for record in records:
    print("    " + record["name"])
PY
  else
    echo "  warning: failed to scan Firestore records" >&2
  fi
else
  echo "scan: firestore records skipped (no database configured)"
fi

residual_count="$(python3 - "$tmp_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
count = 0
for name in [
    "instances.json",
    "routes.json",
    "firewalls.json",
    "addresses.json",
    "service-accounts.json",
    "firestore-records.json",
]:
    with open(root / name, "r", encoding="utf-8") as fh:
        data = json.load(fh)
    if isinstance(data, list):
        count += len(data)
    elif data:
        count += 1
print(count)
PY
)"

if [[ "$residual_count" != "0" ]]; then
  echo "GCP residual scan failed: found ${residual_count} residual item(s)" >&2
  exit 1
fi

echo "GCP residual scan passed"
