#!/usr/bin/env bash
# Generate one atomic all-package Go profile and a machine-readable summary.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
profile="${GO_COVERAGE_PROFILE:-$root/coverage.out}"
report="${GO_COVERAGE_REPORT:-$root/coverage-summary.json}"
inventory="${GO_PACKAGE_EVIDENCE:-$root/go-package-evidence.json}"
minimum="${GO_COVERAGE_MINIMUM:-75}"

cd "$root"

# Keeping this check here makes local-replace failures actionable instead of
# producing an incomplete profile that looks like a successful coverage run.
go mod vendor

go test -race -covermode=atomic -coverpkg=./... -coverprofile="$profile" ./...
python3 - "$profile" "$report" "$minimum" <<'PY'
import json
import sys
from pathlib import Path

profile = Path(sys.argv[1])
report = Path(sys.argv[2])
minimum = float(sys.argv[3])
covered = statements = 0
for line in profile.read_text().splitlines()[1:]:
    location, count = line.rsplit(" ", 1)
    file_part, block = location.rsplit(":", 1)
    if "/gen/go/" in file_part or ("/cmd/" in file_part and file_part.endswith("/main.go")):
        continue
    stmt_count = int(block.split(",")[1].split()[0])
    statements += stmt_count
    if int(count):
        covered += stmt_count
percent = 0 if not statements else covered * 100 / statements
data = {"schema_version": 1, "covered_statements": covered,
        "total_statements": statements, "coverage_percent": round(percent, 2),
        "minimum_percent": minimum, "passed": percent >= minimum}
report.write_text(json.dumps(data, indent=2) + "\n")
print(json.dumps(data))
if percent < minimum:
    raise SystemExit(f"filtered Go coverage {percent:.2f}% is below {minimum:.2f}%")
PY

"$root/scripts/generate-go-package-evidence.py" --root "$root" --output "$inventory" --strict
