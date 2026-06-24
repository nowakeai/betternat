#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${BETTERNAT_DEPENDENCY_SCAN_DIR:-"$ROOT_DIR/tmp/dependency-scan"}"

mkdir -p "$OUT_DIR"

python3 - "$ROOT_DIR" "$OUT_DIR" <<'PY'
import datetime
import json
import os
import pathlib
import re
import subprocess
import sys

root = pathlib.Path(sys.argv[1])
out_dir = pathlib.Path(sys.argv[2])

restricted_patterns = [
    re.compile(r"\bAGPL\b", re.IGNORECASE),
    re.compile(r"GNU Affero General Public License", re.IGNORECASE),
    re.compile(r"\bGPL\b", re.IGNORECASE),
    re.compile(r"\bLGPL\b", re.IGNORECASE),
    re.compile(r"\bSSPL\b", re.IGNORECASE),
    re.compile(r"Business Source License", re.IGNORECASE),
    re.compile(r"Commons Clause", re.IGNORECASE),
    re.compile(r"Elastic License", re.IGNORECASE),
]

allowed_license_patterns = [
    ("Apache-2.0", re.compile(r"Apache License\s*,?\s*Version 2\.0", re.IGNORECASE)),
    ("MIT", re.compile(r"\bMIT License\b|Permission is hereby granted, free of charge", re.IGNORECASE)),
    ("BSD", re.compile(r"Redistribution and use in source and binary forms", re.IGNORECASE)),
    ("MPL-2.0", re.compile(r"Mozilla Public License\s*,?\s*version 2\.0", re.IGNORECASE)),
    ("ISC", re.compile(r"\bISC License\b", re.IGNORECASE)),
]

license_name_patterns = (
    "LICENSE",
    "LICENCE",
    "COPYING",
    "NOTICE",
)


def run(cmd):
    return subprocess.check_output(cmd, cwd=root, text=True)


def parse_go_list_modules(raw):
    decoder = json.JSONDecoder()
    modules = []
    idx = 0
    while idx < len(raw):
        while idx < len(raw) and raw[idx].isspace():
            idx += 1
        if idx >= len(raw):
            break
        obj, idx = decoder.raw_decode(raw, idx)
        modules.append(obj)
    return modules


def license_files(module_dir):
    result = []
    path = pathlib.Path(module_dir)
    if not path.exists():
        return result
    for child in sorted(path.iterdir()):
        if not child.is_file():
            continue
        upper = child.name.upper()
        if any(upper.startswith(prefix) for prefix in license_name_patterns):
            result.append(child)
    return result


def scan_restricted(files):
    hits = []
    for file_path in files:
        try:
            text = file_path.read_text(errors="ignore")
        except OSError as exc:
            hits.append({"file": str(file_path), "pattern": "read-error", "error": str(exc)})
            continue
        for pattern in restricted_patterns:
            if pattern.search(text):
                hits.append({"file": str(file_path), "pattern": pattern.pattern})
    return hits


def detect_allowed_license(files):
    detected = []
    for file_path in files:
        try:
            text = file_path.read_text(errors="ignore")
        except OSError:
            continue
        for name, pattern in allowed_license_patterns:
            if pattern.search(text):
                detected.append(name)
    return sorted(set(detected))


raw = run(["go", "list", "-m", "-json", "all"])
modules = parse_go_list_modules(raw)

records = []
missing_license = []
restricted_hits = []

for module in modules:
    if module.get("Main"):
        continue
    module_dir = module.get("Dir", "")
    files = license_files(module_dir)
    rel_files = []
    for file_path in files:
        try:
            rel_files.append(str(file_path.relative_to(pathlib.Path.home())))
        except ValueError:
            rel_files.append(str(file_path))
    detected_licenses = detect_allowed_license(files)

    record = {
        "path": module.get("Path", ""),
        "version": module.get("Version", ""),
        "dir": module_dir,
        "license_files": rel_files,
        "detected_licenses": detected_licenses,
    }
    records.append(record)

    if not files:
        missing_license.append(record)

    if not detected_licenses:
        hits = scan_restricted(files)
        for hit in hits:
            hit["module"] = module.get("Path", "")
            hit["version"] = module.get("Version", "")
            restricted_hits.append(hit)

report = {
    "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "module_count": len(records),
    "missing_license_count": len(missing_license),
    "restricted_hit_count": len(restricted_hits),
    "restricted_patterns": [pattern.pattern for pattern in restricted_patterns],
    "modules": records,
    "missing_license": missing_license,
    "restricted_hits": restricted_hits,
}

json_path = out_dir / "go-dependency-license-report.json"
json_path.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")

markdown_lines = [
    "# BetterNAT Go Dependency License Scan",
    "",
    f"Generated: `{report['generated_at']}`",
    "",
    f"- modules scanned: `{report['module_count']}`",
    f"- modules missing license files: `{report['missing_license_count']}`",
    f"- restricted license keyword hits: `{report['restricted_hit_count']}`",
    "",
    "## Modules",
    "",
    "| Module | Version | Detected licenses | License files |",
    "| --- | --- | --- | --- |",
]
for record in records:
    files = "<br>".join(record["license_files"]) if record["license_files"] else "_missing_"
    licenses = ", ".join(record["detected_licenses"]) if record["detected_licenses"] else "_unknown_"
    markdown_lines.append(f"| `{record['path']}` | `{record['version']}` | {licenses} | {files} |")
markdown_path = out_dir / "go-dependency-license-report.md"
markdown_path.write_text("\n".join(markdown_lines) + "\n")

print(f"wrote {json_path}")
print(f"wrote {markdown_path}")
print(f"modules scanned: {report['module_count']}")
print(f"modules missing license files: {report['missing_license_count']}")
print(f"restricted license keyword hits: {report['restricted_hit_count']}")

if missing_license:
    print("modules missing license files:", file=sys.stderr)
    for record in missing_license:
        print(f"  {record['path']} {record['version']} ({record['dir']})", file=sys.stderr)

if restricted_hits:
    print("restricted license keyword hits:", file=sys.stderr)
    for hit in restricted_hits:
        print(f"  {hit['module']} {hit.get('version', '')}: {hit['file']} matched {hit['pattern']}", file=sys.stderr)

if missing_license or restricted_hits:
    sys.exit(1)
PY
