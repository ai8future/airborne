#!/usr/bin/env bash
set -euo pipefail

# Produces one atomic all-package profile, then verifies the approved 75%
# filtered coverage floor and writes a machine-readable evidence inventory.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

profile="${GO_COVERAGE_PROFILE:-coverage.out}"
inventory="${GO_PACKAGE_EVIDENCE:-testdata/go-package-evidence.json}"
packages="$(go list ./... | paste -sd, -)"

go test -race -coverpkg="$packages" -coverprofile="$profile" ./...
go run ./tools/coverageaudit -profile "$profile" -minimum "${GO_COVERAGE_MINIMUM:-75}" -inventory "$inventory"
