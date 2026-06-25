#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/gcp-ha-preflight.sh --project PROJECT_ID [--database DATABASE_ID]

Environment:
  BETTERNAT_GCP_MANAGE_RUNTIME_IAM=1
      Also require project custom-role create/update/delete permissions.

  BETTERNAT_GCP_REQUIRE_DATABASE_CREATE=1
      Require datastore.databases.create even if a Firestore database already
      exists. By default, an existing database is enough for HA smoke.

  BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE=1
      Also require Firestore database create/delete permissions for provider-owned
      database lifecycle.

This is a read-only preflight. It does not create service accounts, databases,
routes, instances, roles, or IAM bindings.
EOF
}

project_id=""
database_id="${BETTERNAT_GCP_FIRESTORE_DATABASE:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project)
      project_id="${2:-}"
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
  project_id="${BETTERNAT_GCP_PROJECT:-}"
fi
if [[ -z "$project_id" ]]; then
  echo "missing --project or BETTERNAT_GCP_PROJECT" >&2
  exit 2
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

require_cmd gcloud
require_cmd curl
require_cmd python3

account="$(gcloud config get-value core/account 2>/dev/null || true)"
if [[ -z "$account" ]]; then
  echo "gcloud account is not configured" >&2
  exit 2
fi

echo "BetterNAT GCP HA preflight"
echo "project: $project_id"
echo "account: $account"

for api in compute.googleapis.com firestore.googleapis.com iam.googleapis.com; do
  if gcloud --project "$project_id" services list --enabled --filter="config.name=$api" --format='value(config.name)' | grep -qx "$api"; then
    echo "api enabled: $api"
  else
    echo "missing enabled API: $api" >&2
    exit 1
  fi
done

databases_json="$(gcloud --project "$project_id" firestore databases list --format=json)"
database_count="$(DATABASES_JSON="$databases_json" python3 - <<'PY'
import json
import os

databases = json.loads(os.environ["DATABASES_JSON"])
print(len(databases))
PY
)"
if [[ "$database_count" == "0" ]]; then
  echo "firestore database: missing"
else
  echo "firestore databases:"
  DATABASES_JSON="$databases_json" python3 - <<'PY'
import json
import os

for db in json.loads(os.environ["DATABASES_JSON"]):
    print("  " + db.get("name", ""))
PY
fi

permissions=(
  compute.globalOperations.get
  compute.instances.create
  compute.instances.delete
  compute.instances.get
  compute.instances.use
  compute.networks.get
  compute.routes.create
  compute.routes.delete
  compute.routes.get
  datastore.databases.get
  datastore.entities.create
  datastore.entities.delete
  datastore.entities.get
  datastore.entities.list
  datastore.entities.update
  iam.serviceAccounts.actAs
  iam.serviceAccounts.create
  iam.serviceAccounts.delete
  iam.serviceAccounts.get
  resourcemanager.projects.getIamPolicy
  resourcemanager.projects.setIamPolicy
)

if [[ "$database_count" == "0" || "${BETTERNAT_GCP_REQUIRE_DATABASE_CREATE:-0}" == "1" ]]; then
  permissions+=(datastore.databases.create)
fi
if [[ "${BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE:-0}" == "1" ]]; then
  permissions+=(
    datastore.databases.create
    datastore.databases.delete
  )
fi

if [[ "${BETTERNAT_GCP_MANAGE_RUNTIME_IAM:-0}" == "1" ]]; then
  permissions+=(
    iam.roles.create
    iam.roles.delete
    iam.roles.get
    iam.roles.update
  )
else
  permissions+=(iam.roles.get)
fi

token="$(gcloud auth print-access-token)"
request_json="$(PERMISSIONS="${permissions[*]}" python3 - <<'PY'
import json
import os

print(json.dumps({"permissions": os.environ["PERMISSIONS"].split()}))
PY
)"
response_json="$(curl -fsS \
  -H "Authorization: Bearer ${token}" \
  -H "Content-Type: application/json" \
  -X POST \
  "https://cloudresourcemanager.googleapis.com/v1/projects/${project_id}:testIamPermissions" \
  -d "$request_json")"

missing="$(REQUEST_JSON="$request_json" RESPONSE_JSON="$response_json" python3 - <<'PY'
import json
import os

requested = set(json.loads(os.environ["REQUEST_JSON"])["permissions"])
granted = set(json.loads(os.environ["RESPONSE_JSON"]).get("permissions", []))
for permission in sorted(requested - granted):
    print(permission)
PY
)"

if [[ -n "$missing" ]]; then
  echo "missing permissions:" >&2
  while IFS= read -r permission; do
    echo "  $permission" >&2
  done <<< "$missing"
  exit 1
fi

if [[ -n "$database_id" && "$database_count" != "0" ]]; then
  if DATABASES_JSON="$databases_json" DATABASE_ID="$database_id" python3 - <<'PY'
import json
import os
import sys

database_id = os.environ["DATABASE_ID"]
for db in json.loads(os.environ["DATABASES_JSON"]):
    if db.get("name", "").rstrip("/").endswith("/databases/" + database_id):
        sys.exit(0)
sys.exit(1)
PY
  then
    echo "firestore database selected: $database_id"
  elif [[ "${BETTERNAT_GCP_MANAGE_FIRESTORE_DATABASE:-0}" == "1" ]]; then
    echo "firestore database will be created by provider: $database_id"
  else
    echo "firestore database $database_id not found" >&2
    exit 1
  fi
fi

echo "GCP HA preflight passed"
