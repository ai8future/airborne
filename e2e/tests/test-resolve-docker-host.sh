#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
socket="$tmp/docker.sock"
# Bash's test -S needs a real socket; start a short-lived UNIX listener.
python3 - "$socket" <<'PY' &
import socket, sys, time
s=socket.socket(socket.AF_UNIX); s.bind(sys.argv[1]); time.sleep(10)
PY
pid=$!; sleep .1
mkdir "$tmp/bin"
cat >"$tmp/bin/docker" <<EOF2
#!/usr/bin/env bash
case "\$1" in
  context)
    if [[ "\$2" == show ]]; then
      echo "\${FAKE_DOCKER_CONTEXT:-orbstack}"
    elif [[ "\$2" == inspect ]]; then
      echo "\${FAKE_DOCKER_ENDPOINT:-unix://$socket}"
    fi
    ;;
  --host) exit 0 ;;
esac
EOF2
chmod +x "$tmp/bin/docker"
env -u DOCKER_HOST PATH="$tmp/bin:$PATH" "$root/scripts/resolve-docker-host.sh" | grep -qx "unix://$socket"
DOCKER_HOST="tcp://docker.example:2376" PATH="$tmp/bin:$PATH" "$root/scripts/resolve-docker-host.sh" | grep -qx 'tcp://docker.example:2376'
if DOCKER_HOST="unix://$tmp/missing.sock" PATH="$tmp/bin:$PATH" "$root/scripts/resolve-docker-host.sh" >/dev/null 2>&1; then exit 1; fi
env -u DOCKER_HOST \
  FAKE_DOCKER_CONTEXT=default \
  FAKE_DOCKER_ENDPOINT='<no value>' \
  DOCKER_DEFAULT_SOCKET="$socket" \
  PATH="$tmp/bin:$PATH" \
  "$root/scripts/resolve-docker-host.sh" | grep -qx "unix://$socket"
kill "$pid" 2>/dev/null || true
wait "$pid" 2>/dev/null || true
echo 'PASS: explicit host precedence, active context, default socket fallback, and missing socket failure'
