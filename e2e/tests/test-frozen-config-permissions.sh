#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
runner="$root/e2e/run.sh"
expected='chmod 0644 "$frozen_config"'

permission_command=$(grep -F -x "$expected" "$runner" || true)
if [[ "$permission_command" != "$expected" ]]; then
  echo 'E2E runner does not make the secret-free frozen config readable by its non-root container' >&2
  exit 1
fi

python3 - "$runner" <<'PY'
from pathlib import Path
import sys

lines = Path(sys.argv[1]).read_text().splitlines()
freeze = next(i for i, line in enumerate(lines) if '"$freezer" >"$ARTIFACTS/freezer.stdout"' in line)
readable = next(i for i, line in enumerate(lines) if line == 'chmod 0644 "$frozen_config"')
compose = next(i for i, line in enumerate(lines) if '"${COMPOSE[@]}" up --build --wait' in line)
assert freeze < readable < compose, "frozen config permissions must be set after freezing and before Compose startup"
PY

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
frozen_config="$tmp/frozen.json"
(umask 077; printf '%s\n' '{"global_config":{}}' >"$frozen_config")
[[ $(python3 -c 'import os, stat, sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$frozen_config") == 0o600 ]]
eval "$permission_command"
[[ $(python3 -c 'import os, stat, sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$frozen_config") == 0o644 ]]

echo 'PASS: secret-free frozen config is made readable before non-root Compose startup'
