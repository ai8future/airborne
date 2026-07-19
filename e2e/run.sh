#!/usr/bin/env bash
# Runs deterministic black-box checks against an isolated production-image stack.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ARTIFACTS=${E2E_ARTIFACTS_DIR:-"$ROOT/e2e/artifacts"}
PROJECT=${COMPOSE_PROJECT_NAME:-"airborne-e2e-${USER:-local}-$$"}
COMPOSE=(docker compose -p "$PROJECT" -f "$ROOT/e2e/docker-compose.yml")
ADMIN_TOKEN=airborne-e2e-token
DASHBOARD_TOKEN=dashboard-e2e-token
mkdir -p "$ARTIFACTS"

[[ ${POSTGRES_E2E_IMAGE:-} == *@sha256:* ]] || { echo 'POSTGRES_E2E_IMAGE must be digest-pinned' >&2; exit 2; }
[[ ${REDIS_E2E_IMAGE:-} == *@sha256:* ]] || { echo 'REDIS_E2E_IMAGE must be digest-pinned' >&2; exit 2; }
[[ -n ${AIRBORNE_E2E_IMAGE:-} ]] || { echo 'AIRBORNE_E2E_IMAGE must name the tested production image' >&2; exit 2; }
cli=${AIRBORNE_E2E_CLI:-"$ROOT/bin/airborne-cli"}
probe=${AIRBORNE_E2E_PROBE:-"$ROOT/bin/airborne-e2e-probe"}
freezer=${AIRBORNE_E2E_FREEZER:-"$ROOT/bin/airborne-freeze"}
for executable in "$cli" "$probe" "$freezer"; do
  [[ -x "$executable" ]] || { echo "required E2E executable is missing: $executable" >&2; exit 2; }
done
[[ -x "$ROOT/dashboard/node_modules/.bin/playwright" ]] || {
  echo 'dashboard dependencies and Playwright Chromium must be installed before E2E' >&2
  exit 2
}

export DOCKER_HOST="$($ROOT/scripts/resolve-docker-host.sh --check)"
export COMPOSE_PROJECT_NAME="$PROJECT"

echo 'E2E-008: freeze resolved configuration before production startup'
frozen_config="$ARTIFACTS/frozen.json"
rm -f "$frozen_config"
env -u DOPPLER_TOKEN \
  AIRBORNE_CONFIG="$ROOT/e2e/fixtures/airborne.yaml" \
  AIRBORNE_CONFIGS_DIR="$ROOT/e2e/fixtures/configs" \
  AIRBORNE_FROZEN_CONFIG_PATH="$frozen_config" \
  AIRBORNE_ADMIN_TOKEN="$ADMIN_TOKEN" \
  AIRBORNE_AUTH_MODE=static \
  ADMIN_ENABLED=true \
  DATABASE_ENABLED=true \
  DATABASE_URL='postgres://airborne_app:airborne-app-e2e@postgres:5432/airborne?sslmode=disable' \
  OPENAI_API_KEY=deterministic-e2e-key \
  "$freezer" >"$ARTIFACTS/freezer.stdout" 2>"$ARTIFACTS/freezer.stderr"
python3 - "$frozen_config" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
assert data.get("global_config"), "frozen global config missing"
assert len(data.get("tenant_configs", [])) == 1, "expected one frozen tenant"
raw = path.read_text()
for secret in ("airborne-e2e-token", "deterministic-e2e-key", "airborne-app-e2e"):
    assert secret not in raw, f"plaintext secret leaked into frozen config: {secret}"
PY
chmod 0644 "$frozen_config"
export AIRBORNE_E2E_FROZEN_CONFIG="$frozen_config"

cleanup() {
  status=$?
  trap - EXIT INT TERM
  "${COMPOSE[@]}" ps >"$ARTIFACTS/ps.txt" 2>&1 || true
  "${COMPOSE[@]}" logs --no-color >"$ARTIFACTS/compose.log" 2>&1 || true
  "${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || status=1

  {
    docker ps -aq --filter "label=com.docker.compose.project=$PROJECT"
    docker network ls -q --filter "label=com.docker.compose.project=$PROJECT"
    docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT"
  } | sed '/^$/d' >"$ARTIFACTS/residual-resources.txt"
  if [[ -s "$ARTIFACTS/residual-resources.txt" ]]; then
    echo "E2E cleanup left resources for project $PROJECT" >&2
    cat "$ARTIFACTS/residual-resources.txt" >&2
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"${COMPOSE[@]}" up --build --wait --wait-timeout 120

# The runtime network is deliberately internal, so Docker does not publish
# host ports. Both Linux CI and OrbStack route bridge addresses from the host;
# use those addresses directly without giving the stack an egress-capable NIC.
service_ip() {
  local container
  container=$("${COMPOSE[@]}" ps -q "$1")
  [[ -n "$container" ]] || { echo "missing Compose service: $1" >&2; return 1; }
  docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container"
}
airborne_ip=$(service_ip airborne)
dashboard_ip=$(service_ip dashboard)
admin="http://${airborne_ip}:8473"
grpc_addr="${airborne_ip}:50612"
dashboard="http://${dashboard_ip}:4848"

wait_for_admin() {
  for _ in $(seq 1 60); do
    airborne_ip=$(service_ip airborne)
    admin="http://${airborne_ip}:8473"
    if curl --connect-timeout 2 --max-time 5 --fail --silent --show-error "$admin/admin/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "admin endpoint did not become ready within 120 seconds" >&2
  return 1
}

echo 'E2E-001: production image and gRPC health'
"${COMPOSE[@]}" exec -T airborne /app/airborne --health-check
provider_request_count() {
  "${COMPOSE[@]}" exec -T provider-stub python -c \
    "import json; from urllib.request import urlopen; print(len(json.load(urlopen('http://localhost:8080/requests'))['requests']))"
}
provider_count_before=$(provider_request_count)
"$probe" --addr "$grpc_addr" --token "$ADMIN_TOKEN" --tenant ai8 --prompt 'deterministic grpc e2e' >"$ARTIFACTS/grpc-chat.json"
grep -q 'deterministic-e2e-response' "$ARTIFACTS/grpc-chat.json"
grep -q 'PROVIDER_OPENAI' "$ARTIFACTS/grpc-chat.json"
provider_count_after_first=$(provider_request_count)
[[ "$provider_count_after_first" -eq $((provider_count_before + 1)) ]]
"$probe" --addr "$grpc_addr" --token "$ADMIN_TOKEN" --tenant ai8 --prompt 'deterministic grpc e2e' >"$ARTIFACTS/grpc-chat-replay.json"
cmp "$ARTIFACTS/grpc-chat.json" "$ARTIFACTS/grpc-chat-replay.json"
provider_count_after_replay=$(provider_request_count)
[[ "$provider_count_after_replay" -eq "$provider_count_after_first" ]]

echo 'E2E-002: public health and protected auth boundary'
curl --fail --silent --show-error "$admin/admin/health" >"$ARTIFACTS/admin-health.json"
grep -q '"database":"healthy"' "$ARTIFACTS/admin-health.json"
grep -q '"redis":"healthy"' "$ARTIFACTS/admin-health.json"
[[ $(curl --silent --output /dev/null --write-out '%{http_code}' "$admin/admin/activity") == 401 ]]
[[ $(curl --silent --output /dev/null --write-out '%{http_code}' -H 'Authorization: Bearer wrong-token' "$admin/admin/activity") == 401 ]]
curl --fail --silent --show-error -H "Authorization: Bearer $ADMIN_TOKEN" "$admin/admin/activity" >"$ARTIFACTS/activity-initial.json"

echo 'E2E-003/004: CLI and direct gRPC traverse the deterministic provider'
"$cli" --url "$admin" --token "$ADMIN_TOKEN" health >"$ARTIFACTS/cli-health.txt"
"$cli" --url "$admin" --token "$ADMIN_TOKEN" --json test --tenant ai8 --provider openai 'deterministic cli e2e' >"$ARTIFACTS/cli-test.json"
grep -q 'deterministic-e2e-response' "$ARTIFACTS/cli-test.json"
grep -q '"provider"[[:space:]]*:[[:space:]]*"openai"' "$ARTIFACTS/cli-test.json"
"${COMPOSE[@]}" exec -T provider-stub python -c "from urllib.request import urlopen; print(urlopen('http://localhost:8080/requests').read().decode())" >"$ARTIFACTS/provider-requests.json"
grep -q '"path": "/v1/responses"' "$ARTIFACTS/provider-requests.json"

echo 'E2E-005: successful gRPC/CLI turns persist and reach the admin feed'
for _ in $(seq 1 60); do
  curl --fail --silent --show-error -H "Authorization: Bearer $ADMIN_TOKEN" "$admin/admin/activity?tenant_id=ai8&limit=50" >"$ARTIFACTS/activity.json"
  message_count=$("${COMPOSE[@]}" exec -T postgres psql -U airborne -d airborne -Atc \
    "select count(*) from airborne_chat_messages where tenant_id='ai8'")
  if grep -q 'deterministic-e2e-response' "$ARTIFACTS/activity.json" && [[ "$message_count" -ge 4 ]]; then
    break
  fi
  sleep 1
done
grep -q 'deterministic-e2e-response' "$ARTIFACTS/activity.json"
[[ "$message_count" -ge 4 ]]

echo 'E2E-006: the non-owner application role enforces cross-tenant RLS'
"${COMPOSE[@]}" exec -T postgres psql -U airborne -d airborne -v ON_ERROR_STOP=1 -c \
  "insert into airborne_files (tenant_id,user_id,filename,hash) values ('email4ai','owner-fixture','secret.txt','e2e-cross-tenant')" >/dev/null
visible=$("${COMPOSE[@]}" exec -T -e PGPASSWORD=airborne-app-e2e postgres psql -U airborne_app -d airborne -Atc \
  "select set_config('airborne.tenant_id','ai8',false); select count(*) from airborne_files where hash='e2e-cross-tenant'" | tail -1)
[[ "$visible" == 0 ]]
if "${COMPOSE[@]}" exec -T -e PGPASSWORD=airborne-app-e2e postgres psql -U airborne_app -d airborne -v ON_ERROR_STOP=1 -c \
  "select set_config('airborne.tenant_id','ai8',false); insert into airborne_files (tenant_id,user_id,filename) values ('email4ai','attacker','denied.txt')" >"$ARTIFACTS/rls-denial.txt" 2>&1; then
  echo 'cross-tenant insert unexpectedly succeeded' >&2
  exit 1
fi
grep -qi 'row-level security policy' "$ARTIFACTS/rls-denial.txt"

echo 'E2E-007: disabled upload mode has one strict, documented result'
upload_status=$(curl --silent --show-error --output "$ARTIFACTS/upload.json" --write-out '%{http_code}' \
  -H "Authorization: Bearer $ADMIN_TOKEN" -F 'tenant_id=ai8' -F "file=@$ROOT/e2e/fixtures/upload.txt;type=text/plain" "$admin/admin/upload")
[[ "$upload_status" == 400 ]]
grep -q 'gemini provider not enabled for tenant: ai8' "$ARTIFACTS/upload.json"

echo 'E2E-008: production server is running from the secret-free frozen snapshot'
"${COMPOSE[@]}" exec -T airborne sh -c 'test "$AIRBORNE_USE_FROZEN" = true && test -r "$AIRBORNE_FROZEN_CONFIG_PATH"'
grep -q 'ENV=AIRBORNE_ADMIN_TOKEN' "$frozen_config"

echo 'E2E-009/010: live dashboard proxy and Chromium browser invocation'
curl --fail --silent --show-error "$dashboard" >"$ARTIFACTS/dashboard.html"
dashboard_status=$(curl --silent --show-error --output "$ARTIFACTS/dashboard-test.json" --write-out '%{http_code}' \
  -H "Authorization: Bearer $DASHBOARD_TOKEN" -H 'Content-Type: application/json' \
  --data '{"prompt":"deterministic dashboard e2e","tenant_id":"ai8","provider":"openai"}' "$dashboard/api/test")
[[ "$dashboard_status" == 200 ]]
grep -q 'deterministic-e2e-response' "$ARTIFACTS/dashboard-test.json"
if ! (cd "$ROOT/dashboard" && \
  DASHBOARD_E2E_LIVE=1 DASHBOARD_E2E_TOKEN="$DASHBOARD_TOKEN" \
  PLAYWRIGHT_LIVE_STACK=1 PLAYWRIGHT_BASE_URL="$dashboard" \
  PLAYWRIGHT_HTML_OUTPUT_DIR="$ARTIFACTS/playwright-report" \
  npx playwright test e2e/dashboard.spec.ts e2e/live-stack.spec.ts \
    --output "$ARTIFACTS/playwright-results") >"$ARTIFACTS/playwright.log" 2>&1; then
  cat "$ARTIFACTS/playwright.log" >&2
  exit 1
fi
cat "$ARTIFACTS/playwright.log"

echo 'E2E-012: provider 429, 5xx, and malformed JSON responses map to bounded failures'
for scenario in 429 500 malformed; do
  status=$(curl --connect-timeout 5 --max-time 120 --silent --show-error \
    --output "$ARTIFACTS/provider-${scenario}.json" --write-out '%{http_code}' \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
    --data "{\"prompt\":\"fixture-${scenario}\",\"tenant_id\":\"ai8\",\"provider\":\"openai\"}" \
    "$admin/admin/test")
  [[ "$status" == 502 ]]
  grep -Eq 'provider test failed|provider request failed' "$ARTIFACTS/provider-${scenario}.json"
done

echo 'E2E-012: a genuinely delayed provider exceeds the deliberately bounded test client budget'
timeout_started=$SECONDS
if curl --connect-timeout 2 --max-time 2 --silent --show-error \
  --output "$ARTIFACTS/provider-timeout.json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  --data '{"prompt":"fixture-timeout","tenant_id":"ai8","provider":"openai"}' \
  "$admin/admin/test"; then
  echo 'delayed provider unexpectedly completed inside the test client timeout' >&2
  exit 1
else
  timeout_status=$?
fi
[[ "$timeout_status" == 28 ]]
[[ $((SECONDS - timeout_started)) -le 5 ]]
"${COMPOSE[@]}" exec -T provider-stub python -c \
  "from urllib.request import urlopen; print(urlopen('http://localhost:8080/requests').read().decode())" \
  >"$ARTIFACTS/provider-timeout-requests.json"
grep -q '"scenario": "timeout"' "$ARTIFACTS/provider-timeout-requests.json"

echo 'E2E-011: graceful restart restores readiness from the frozen configuration'
airborne_container=$("${COMPOSE[@]}" ps -q airborne)
[[ -n "$airborne_container" ]] || { echo 'airborne container disappeared before restart' >&2; exit 1; }
docker stop --time 30 "$airborne_container" >/dev/null
[[ $(docker inspect --format '{{.State.Running}}' "$airborne_container") == false ]]
airborne_exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$airborne_container")
if [[ "$airborne_exit_code" != 0 ]]; then
  echo "airborne did not exit cleanly for restart (exit $airborne_exit_code)" >&2
  exit 1
fi
docker start "$airborne_container" >/dev/null
[[ $("${COMPOSE[@]}" ps -q airborne) == "$airborne_container" ]]
"${COMPOSE[@]}" up --wait --wait-timeout 120 airborne
wait_for_admin
curl --fail --silent --show-error "$admin/admin/health" | grep -q '"status":"healthy"'

echo 'E2E PASS: image, auth, CLI, gRPC, persistence, RLS, upload, freezer, dashboard/browser, provider failures, and restart passed'
