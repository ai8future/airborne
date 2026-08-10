#!/usr/bin/env bash
set -euo pipefail

# Produces one atomic all-package profile, then verifies the approved 75%
# filtered coverage floor and writes a machine-readable evidence inventory.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

profile="${GO_COVERAGE_PROFILE:-coverage.out}"
inventory="${GO_PACKAGE_EVIDENCE:-testdata/go-package-evidence.json}"
markdown_profile="${MARKDOWN_GO_COVERAGE_PROFILE:-markdown_svc/clients/go/coverage.out}"
case "$markdown_profile" in
  /*) ;;
  *) markdown_profile="$root/$markdown_profile" ;;
esac

python3 scripts/generate-go-package-evidence.py --output "$inventory" --strict
# Coverage is a required integration gate: resolve the active engine (including
# OrbStack), export it for Testcontainers, and refuse to turn an absent DB suite
# into a successful profile.
export DOCKER_HOST="$(scripts/resolve-docker-host.sh --check)"
export AIRBORNE_REQUIRE_INTEGRATION=1
# One all-package invocation produces an atomic profile.  Do not use
# -coverpkg here: across ./... it duplicates source blocks per test binary and
# makes the aggregate denominator invalid.
go test -count=1 -race -coverprofile="$profile" ./...

# The checked-in markdown client is its own Go module, so ./... above cannot
# discover it. Produce a separate atomic profile without corrupting the root
# profile, then apply the same substantive-package protection. Generated
# protobuf sources remain excluded by coverageaudit.
mkdir -p "$(dirname "$markdown_profile")"
(cd markdown_svc/clients/go && go test -count=1 -race -coverprofile="$markdown_profile" ./...)

go run ./tools/coverageaudit \
  -profile "$profile" \
  -minimum "${GO_COVERAGE_MINIMUM:-75}" \
  -package-minimum "${GO_PACKAGE_COVERAGE_MINIMUM:-60}"
go run ./tools/coverageaudit \
  -profile "$markdown_profile" \
  -minimum "${MARKDOWN_GO_COVERAGE_MINIMUM:-60}" \
  -package-minimum "${MARKDOWN_GO_PACKAGE_COVERAGE_MINIMUM:-60}"
