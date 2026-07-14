#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
artifact="$root/markdown_svc/clients/go/coverage.out"
cleanup() { rm -f "$artifact"; }
trap cleanup EXIT

mkdir -p "$(dirname "$artifact")"
: >"$artifact"

if output=$("$root/scripts/assert-verification-clean.sh" 2>&1); then
  echo "expected the cleanliness gate to reject $artifact" >&2
  exit 1
fi
if [[ "$output" != *"markdown_svc/clients/go/coverage.out"* ]]; then
  echo "cleanliness gate did not identify the nested coverage artifact:" >&2
  printf '%s\n' "$output" >&2
  exit 1
fi

make -C "$root" clean >/dev/null
if [[ -e "$artifact" ]]; then
  echo "make clean left $artifact behind" >&2
  exit 1
fi

echo "PASS: nested Go coverage is explicitly rejected and removed"
