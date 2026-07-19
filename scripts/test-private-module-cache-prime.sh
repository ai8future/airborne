#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
subject=$repo_root/scripts/prime-private-go-modules.sh
workflow=$repo_root/.github/workflows/docker-build.yml
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT HUP INT TERM

fake_bin=$test_root/bin
runner_temp=$test_root/runner
home=$test_root/home
mkdir -p "$fake_bin" "$runner_temp" "$home"
log=$test_root/commands.log
config_state=$test_root/git-config
export CONTRACT_LOG=$log CONTRACT_CONFIG_STATE=$config_state

cat >"$fake_bin/curl" <<'EOF'
#!/bin/sh
printf 'curl' >>"$CONTRACT_LOG"
for arg do printf ' <%s>' "$arg" >>"$CONTRACT_LOG"; done
printf '\n' >>"$CONTRACT_LOG"
if test "${CONTRACT_INVALID_META:-}" = 1; then
  printf '%s\n' '{"ssh_keys":["not-a-host-key"]}'
else
  printf '%s\n' '{"ssh_keys":["ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeTlsAuthenticatedHostKey"]}'
fi
EOF

cat >"$fake_bin/ssh-keygen" <<'EOF'
#!/bin/sh
case "$1" in
  -y)
    test "$2" = -P
    test -z "$3"
    test "$4" = -f
    test -s "$5"
    ;;
  -lf)
    test -s "$2"
    grep -q '^github.com ssh-ed25519 ' "$2"
    ;;
  *) exit 66 ;;
esac
EOF

cat >"$fake_bin/git" <<'EOF'
#!/bin/sh
printf 'git' >>"$CONTRACT_LOG"
for arg do printf ' <%s>' "$arg" >>"$CONTRACT_LOG"; done
printf '\n' >>"$CONTRACT_LOG"
case "$*" in
  'config --global --add '*)
    printf '%s\n%s\n' "$4" "$5" >"$CONTRACT_CONFIG_STATE"
    ;;
  'config --global --get-all '*)
    test -s "$CONTRACT_CONFIG_STATE"
    sed -n '2p' "$CONTRACT_CONFIG_STATE"
    ;;
  'config --global --fixed-value --unset-all '*)
    if test -s "$CONTRACT_CONFIG_STATE" &&
       test "$(sed -n '1p' "$CONTRACT_CONFIG_STATE")" = "$5" &&
       test "$(sed -n '2p' "$CONTRACT_CONFIG_STATE")" = "$6"; then
      rm -f "$CONTRACT_CONFIG_STATE"
    fi
    ;;
  *) exit 64 ;;
esac
EOF

cat >"$fake_bin/go" <<'EOF'
#!/bin/sh
test "$GOWORK" = off
test "$GOPRIVATE" = github.com/ai8future/chassis-go-addons
test "$GONOSUMDB" = github.com/ai8future/chassis-go-addons
test "$GIT_TERMINAL_PROMPT" = 0
test "$1" = mod
test "$2" = download
test "$3" = github.com/ai8future/chassis-go-addons/pgkit@v1.2.10
test "$4" = github.com/ai8future/chassis-go-addons/rediskit@v1.2.10
test "$5" = github.com/ai8future/chassis-go-addons/ssrfcheck@v1.2.10
test "$#" -eq 5
case "$GIT_SSH_COMMAND" in
  *' -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile='*) ;;
  *) exit 65 ;;
esac
key_path=${GIT_SSH_COMMAND#* -i }
key_path=${key_path%% -o *}
known_hosts_path=${GIT_SSH_COMMAND##*UserKnownHostsFile=}
test "$(stat -c %a "$key_path")" = 600
test "$(stat -c %a "$known_hosts_path")" = 600
test "$(cat "$known_hosts_path")" = 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeTlsAuthenticatedHostKey'
test "$(sed -n '1p' "$CONTRACT_CONFIG_STATE")" = 'url.ssh://git@github.com/ai8future/chassis-go-addons.insteadOf'
test "$(sed -n '2p' "$CONTRACT_CONFIG_STATE")" = 'https://github.com/ai8future/chassis-go-addons'
printf '%s\n%s\n' "$key_path" "$known_hosts_path" >"$CONTRACT_LOG.paths"
EOF

chmod +x "$fake_bin"/*

secret='contract-secret-must-stay-silent'
output=$test_root/output
PATH="$fake_bin:$PATH" HOME="$home" RUNNER_TEMP="$runner_temp" \
	CHASSIS_GO_ADDONS_DEPLOY_KEY="$secret" "$subject" >"$output" 2>&1

test ! -s "$output"
! grep -F "$secret" "$output" "$log" >/dev/null
test ! -e "$config_state"
while IFS= read -r path; do test ! -e "$path"; done <"$log.paths"
grep -F 'curl <--fail> <--silent> <--show-error> <--proto> <=https> <--tlsv1.2> <https://api.github.com/meta>' "$log" >/dev/null
grep -F 'git <config> <--global> <--add> <url.ssh://git@github.com/ai8future/chassis-go-addons.insteadOf> <https://github.com/ai8future/chassis-go-addons>' "$log" >/dev/null
grep -F 'git <config> <--global> <--fixed-value> <--unset-all> <url.ssh://git@github.com/ai8future/chassis-go-addons.insteadOf> <https://github.com/ai8future/chassis-go-addons>' "$log" >/dev/null
! grep -F 'ssh-keyscan' "$subject" >/dev/null
! grep -F 'GITHUB_ENV' "$subject" "$workflow" >/dev/null

missing_output=$test_root/missing-output
if PATH="$fake_bin:$PATH" HOME="$home" RUNNER_TEMP="$runner_temp" \
	CHASSIS_GO_ADDONS_DEPLOY_KEY= "$subject" >"$missing_output" 2>&1; then
	echo 'private module priming accepted a missing deploy key' >&2
	exit 1
fi
grep -Fx 'CHASSIS_GO_ADDONS_DEPLOY_KEY is required' "$missing_output" >/dev/null
test ! -e "$config_state"
test ! -e "$runner_temp/airborne-chassis-go-addons-key"
test ! -e "$runner_temp/airborne-github-known-hosts"

invalid_output=$test_root/invalid-output
if PATH="$fake_bin:$PATH" HOME="$home" RUNNER_TEMP="$runner_temp" \
	CONTRACT_INVALID_META=1 CHASSIS_GO_ADDONS_DEPLOY_KEY="$secret" \
	"$subject" >"$invalid_output" 2>&1; then
	echo 'private module priming accepted invalid GitHub meta host keys' >&2
	exit 1
fi
! grep -F "$secret" "$invalid_output" "$log" >/dev/null
test ! -e "$config_state"
test ! -e "$runner_temp/airborne-chassis-go-addons-key"
test ! -e "$runner_temp/airborne-github-known-hosts"

ruby - "$workflow" <<'RUBY'
require "yaml"

workflow = YAML.safe_load(File.read(ARGV.fetch(0)), aliases: true)
%w[verify-go verify-e2e].each do |job_name|
  steps = workflow.fetch("jobs").fetch(job_name).fetch("steps")
  setup = steps.find { |step| step["uses"] == "actions/setup-go@v5" }
  raise "#{job_name} setup-go cache must be false" unless setup.dig("with", "cache") == false

  primers = steps.select { |step| step["name"] == "Prime exact private Go modules" }
  raise "#{job_name} must have exactly one primer" unless primers.length == 1
  primer = primers.first
  raise "#{job_name} primer command drifted" unless primer["run"] == "./scripts/prime-private-go-modules.sh"
  expected_env = {"CHASSIS_GO_ADDONS_DEPLOY_KEY" => "${{ secrets.CHASSIS_GO_ADDONS_DEPLOY_KEY }}"}
  raise "#{job_name} primer secret scope drifted" unless primer["env"] == expected_env

  later_steps = steps.drop(steps.index(primer) + 1)
  raise "#{job_name} leaks credential env after primer" if later_steps.any? { |step| step.fetch("env", {}).key?("CHASSIS_GO_ADDONS_DEPLOY_KEY") }
end
RUBY

echo 'private module cache-prime contract passed'
