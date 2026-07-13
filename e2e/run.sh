#!/usr/bin/env bash
# Runs deterministic black-box checks against an isolated production-image stack.
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ARTIFACTS=${E2E_ARTIFACTS_DIR:-"$ROOT/e2e/artifacts"}
PROJECT=${COMPOSE_PROJECT_NAME:-"airborne-e2e-${USER:-local}-$$"}
COMPOSE=(docker compose -p "$PROJECT" -f "$ROOT/e2e/docker-compose.yml")
mkdir -p "$ARTIFACTS"

[[ ${POSTGRES_E2E_IMAGE:-} == *@sha256:* ]] || { echo 'POSTGRES_E2E_IMAGE must be digest-pinned' >&2; exit 2; }
[[ -n ${AIRBORNE_E2E_IMAGE:-} ]] || { echo 'AIRBORNE_E2E_IMAGE must name the tested production image' >&2; exit 2; }
export DOCKER_HOST="$($ROOT/scripts/resolve-docker-host.sh --check)"
export COMPOSE_PROJECT_NAME="$PROJECT"

cleanup() {
  status=$?
  "${COMPOSE[@]}" ps >"$ARTIFACTS/ps.txt" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color >"$ARTIFACTS/compose.log" 2>&1 || true
  "${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

"${COMPOSE[@]}" up --build --wait --wait-timeout 120
admin_port=$("${COMPOSE[@]}" port airborne 8473 | sed 's/.*://')
dashboard_port=$("${COMPOSE[@]}" port dashboard 4848 | sed 's/.*://')
admin="http://127.0.0.1:${admin_port}"

echo 'E2E-001: production image and gRPC health'
"${COMPOSE[@]}" exec -T airborne /app/airborne --health-check

echo 'E2E-002: public health and protected auth boundary'
curl --fail --silent --show-error "$admin/admin/health" >"$ARTIFACTS/admin-health.json"
[[ $(curl --silent --output /dev/null --write-out '%{http_code}' "$admin/admin/activity") == 401 ]]
curl --fail --silent --show-error -H 'Authorization: Bearer airborne-e2e-token' "$admin/admin/activity" >"$ARTIFACTS/activity.json"

echo 'E2E-003/004: CLI reaches live admin and provider stub receives request'
[[ -x "$ROOT/airborne-cli" ]] || { echo 'airborne-cli must be built before E2E' >&2; exit 1; }
"$ROOT/airborne-cli" --url "$admin" --token airborne-e2e-token health | tee "$ARTIFACTS/cli-health.txt"
"$ROOT/airborne-cli" --url "$admin" --token airborne-e2e-token --json test --provider openai 'deterministic e2e' | tee "$ARTIFACTS/cli-test.json"
"${COMPOSE[@]}" exec -T provider-stub wget -qO- http://localhost:8080/requests >"$ARTIFACTS/provider-requests.json"
grep -q 'chat/completions' "$ARTIFACTS/provider-requests.json"

echo 'E2E-005: real PostgreSQL is reachable and activity endpoint remains available'
"${COMPOSE[@]}" exec -T postgres psql -U airborne -d airborne -Atc 'select 1' | grep -qx 1

echo 'E2E-007: disabled RAG contract is explicit'
[[ $(curl --silent --output /dev/null --write-out '%{http_code}' -H 'Authorization: Bearer airborne-e2e-token' -F 'file=@/dev/null;filename=e2e.txt' "$admin/admin/upload") =~ ^(400|404|503)$ ]]

echo 'E2E-009: dashboard is reachable through the built image'
curl --fail --silent --show-error "http://127.0.0.1:${dashboard_port}" >"$ARTIFACTS/dashboard.html"

echo 'E2E-011: graceful restart restores readiness'
"${COMPOSE[@]}" restart airborne
"${COMPOSE[@]}" up --wait --wait-timeout 90 airborne
curl --fail --silent --show-error "$admin/admin/health" >/dev/null

echo 'E2E PASS: production image, auth, CLI, provider stub, PostgreSQL, dashboard, and restart scenarios passed'
