#!/bin/sh

set -eu

module_root=github.com/ai8future/chassis-go-addons
https_root=https://github.com/ai8future/chassis-go-addons
ssh_root=ssh://git@github.com/ai8future/chassis-go-addons
rewrite_key=url."$ssh_root".insteadOf
key_path=${RUNNER_TEMP:?RUNNER_TEMP is required}/airborne-chassis-go-addons-key
known_hosts_path=$RUNNER_TEMP/airborne-github-known-hosts

cleanup() {
	git config --global --fixed-value --unset-all "$rewrite_key" "$https_root" >/dev/null 2>&1 || :
	rm -f -- "$key_path" "$known_hosts_path"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

test -n "${CHASSIS_GO_ADDONS_DEPLOY_KEY:-}" || {
	echo "CHASSIS_GO_ADDONS_DEPLOY_KEY is required" >&2
	exit 1
}

umask 077
printf '%s\n' "$CHASSIS_GO_ADDONS_DEPLOY_KEY" >"$key_path"
unset CHASSIS_GO_ADDONS_DEPLOY_KEY
chmod 600 "$key_path"
ssh-keygen -y -P '' -f "$key_path" >/dev/null

curl --fail --silent --show-error --proto '=https' --tlsv1.2 \
	https://api.github.com/meta |
	jq -er '
        [.ssh_keys[]?
          | select(type == "string")
          | select(test("^(ssh-ed25519|ecdsa-sha2-nistp256|ssh-rsa) [A-Za-z0-9+/]+={0,3}$"))]
        | if length > 0 then .[] else error("GitHub meta contained no valid SSH host keys") end
      ' |
	awk '{ print "github.com " $0 }' >"$known_hosts_path"
test -s "$known_hosts_path"
test "$(awk 'NF != 3 || $1 != "github.com" { bad = 1 } END { print bad + 0 }' "$known_hosts_path")" -eq 0
chmod 600 "$known_hosts_path"
ssh-keygen -lf "$known_hosts_path" >/dev/null

git config --global --add "$rewrite_key" "$https_root"
test "$(git config --global --get-all "$rewrite_key")" = "$https_root"

GOWORK=off \
	GOPRIVATE="$module_root" \
	GONOSUMDB="$module_root" \
	GIT_TERMINAL_PROMPT=0 \
	GIT_SSH_COMMAND="ssh -i $key_path -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$known_hosts_path" \
	go mod download \
	"$module_root/pgkit@v1.2.10" \
	"$module_root/rediskit@v1.2.10" \
	"$module_root/ssrfcheck@v1.2.10"
