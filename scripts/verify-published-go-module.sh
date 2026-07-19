#!/usr/bin/env bash
set -euo pipefail

version=${1:-${AIRBORNE_MODULE_VERSION:-}}
if [[ -z "$version" ]]; then
  echo "usage: $0 <published-airborne-version>" >&2
  exit 2
fi
if [[ "$version" != v* ]]; then
  echo "published Airborne version must begin with v: $version" >&2
  exit 2
fi

work=$(mktemp -d)
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
mkdir -p "$work/consumer" "$work/modcache" "$work/buildcache"

cat >"$work/consumer/go.mod" <<MOD
module example.com/airborne-release-verifier

go 1.26.5

require github.com/ai8future/airborne $version
MOD
cat >"$work/consumer/api_test.go" <<'GO'
package releaseverifier

import (
	"testing"

	airbornev1 "github.com/ai8future/airborne/gen/go/airborne/v1"
)

func TestGeneratedAPIIsConsumable(t *testing.T) {
	request := &airbornev1.GenerateReplyRequest{}
	if request == nil {
		t.Fatal("generated request is nil")
	}
}
GO

cd "$work/consumer"
export GOWORK=off
export GOFLAGS=-mod=mod
export GOMODCACHE="$work/modcache"
export GOCACHE="$work/buildcache"

published_mod=$(go mod download -json "github.com/ai8future/airborne@$version" | python3 -c 'import json, sys; print(json.load(sys.stdin)["GoMod"])')
if grep -Eq '^[[:space:]]*replace([[:space:](]|$)' "$published_mod"; then
  echo "published Airborne module contains a replacement directive" >&2
  exit 1
fi

go mod tidy
if ! go mod edit -json | python3 -c 'import json, sys; raise SystemExit(0 if json.load(sys.stdin).get("Replace") is None else 1)'; then
  echo "fresh downstream module unexpectedly contains a replacement directive" >&2
  exit 1
fi
go list -m all >/dev/null
go build ./...
go test -count=1 ./...

echo "PASS: $version is consumable from a fresh replacement-free Go module"
