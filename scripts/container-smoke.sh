#!/bin/sh
set -eu

expected_id=${EXPECTED_DISTRO_ID:-ubuntu}
expected_adapter=${EXPECTED_DISTRO_ADAPTER:-ubuntu}
sample=${SMOKE_SAMPLE:-100ms}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

daffy --help >"$tmp/help"
grep -q 'Usage: daffy' "$tmp/help"
daffy --version | grep -q '^daffy '

# Scan and report are bounded, read-only invocations; findings are not health passes.
daffy scan --sample "$sample" --timeout 1s --json >"$tmp/scan.json"
daffy report --sample "$sample" --timeout 1s --json >"$tmp/report.json"
daffy doctor --sample "$sample" --timeout 1s --json >"$tmp/doctor.json"

# Require parseable reports and exact identity/adapter metadata; do not infer health from exit status.
python3 - "$tmp/report.json" "$tmp/doctor.json" "$expected_id" "$expected_adapter" "$tmp/scan.json" <<'PY'
import json, sys
report = json.load(open(sys.argv[1]))
doctor = json.load(open(sys.argv[2]))
assert isinstance(report, dict) and isinstance(report.get("findings"), list), report
system = report.get("system", {})
assert system.get("distro_id") == sys.argv[3], system
assert system.get("distro_adapter") == sys.argv[4], system
assert isinstance(doctor, list), doctor
assert any(check.get("name") == "distro" and check.get("available") is True
           and check.get("detail") == sys.argv[4] for check in doctor), doctor
scan = json.load(open(sys.argv[5]))
assert isinstance(scan.get("findings"), list), scan
assert scan.get("system", {}).get("distro_id") == sys.argv[3], scan
assert scan.get("system", {}).get("distro_adapter") == sys.argv[4], scan
PY
