#!/usr/bin/env bash
# Resolve a usable Docker endpoint without assuming Docker Desktop's default socket.
set -euo pipefail

usage() { echo "usage: $0 [--check]" >&2; }
check=false
case "${1:-}" in
  '') ;;
  --check) check=true ;;
  *) usage; exit 2 ;;
esac

require_docker() {
  command -v docker >/dev/null 2>&1 || { echo "Docker CLI is required" >&2; exit 1; }
}

validate_endpoint() {
  local endpoint=$1 path
  case "$endpoint" in
    unix://*)
      path=${endpoint#unix://}
      [[ -S "$path" ]] || { echo "Docker socket is unavailable: $path" >&2; exit 1; }
      ;;
    tcp://*|ssh://*|npipe://*) ;;
    *) echo "Unsupported Docker endpoint: $endpoint" >&2; exit 1 ;;
  esac
}

require_docker
if [[ -n "${DOCKER_HOST:-}" ]]; then
  endpoint=$DOCKER_HOST
else
  context=$(docker context show 2>/dev/null || true)
  [[ -n "$context" && "$context" != "default" ]] || context=default
  endpoint=$(docker context inspect "$context" --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)
  [[ -n "$endpoint" && "$endpoint" != "<no value>" ]] || {
    echo "Unable to resolve Docker endpoint from active context $context" >&2
    exit 1
  }
fi
validate_endpoint "$endpoint"
if "$check"; then
  docker --host "$endpoint" version --format '{{.Server.Version}}' >/dev/null || {
    echo "Docker daemon is not reachable at $endpoint" >&2
    exit 1
  }
fi
printf '%s\n' "$endpoint"
