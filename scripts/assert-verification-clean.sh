#!/usr/bin/env bash
# Fail when verification leaked repository changes or labelled E2E containers.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

known_ignored_artifacts=(
  "markdown_svc/clients/go/coverage.out"
)
for artifact in "${known_ignored_artifacts[@]}"; do
  if [[ -e "$artifact" ]]; then
    echo "verification artifact remains: $artifact" >&2
    exit 1
  fi
done

if command -v docker >/dev/null 2>&1; then
  residual=$(docker ps -aq --filter label=com.ai8future.airborne.e2e=true)
  if [[ -n "$residual" ]]; then
    echo "verification left E2E containers behind:" >&2
    docker ps -a --filter label=com.ai8future.airborne.e2e=true >&2
    exit 1
  fi
fi

dirty=$(git status --porcelain=v1 --untracked-files=all)
if [[ -n "$dirty" ]]; then
  echo "verification changed the checkout:" >&2
  printf '%s\n' "$dirty" >&2
  exit 1
fi

echo "PASS: checkout is clean and no labelled E2E containers remain"
