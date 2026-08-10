#!/usr/bin/env bash
# Run all dashboard release gates without retaining Next's generated next-env rewrite.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
next_env="$root/dashboard/next-env.d.ts"
snapshot=$(mktemp "${TMPDIR:-/tmp}/airborne-next-env.XXXXXX")
cp "$next_env" "$snapshot"

restore_next_env() {
  cp "$snapshot" "$next_env"
  rm -f "$snapshot"
}
trap restore_next_env EXIT

cd "$root/dashboard"
CI=1 npm test
npm run test:coverage
npm run lint
npm run build
npm run test:e2e
