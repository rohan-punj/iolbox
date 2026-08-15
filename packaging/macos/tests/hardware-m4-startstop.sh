#!/usr/bin/env bash
# hardware-m4-startstop.sh — standalone start/stop lifecycle sub-test.
#
# Runs ONLY the forced-launcher-kill / forced-VM-stop / recovery sequence
# from hardware-m4.sh's item 7, skipping items 1-6/8 (NAT, extnet, soak,
# four-node capacity). Sources hardware-m4.sh's function definitions
# (everything before its own unconditional `main "$@"` call) and drives
# them directly, so the exact same preflight/sentinel/cleanup discipline
# applies without re-implementing it.
set -euo pipefail

test_dir="$(cd "$(dirname "$0")" && pwd)"

# Source every function definition from hardware-m4.sh without triggering
# its own main() — strip the trailing `main "$@"` invocation line first.
funcs_only="$(mktemp)"
trap 'rm -f "$funcs_only"' EXIT
grep -v '^main "\$@"$' "$test_dir/hardware-m4.sh" > "$funcs_only"
# shellcheck source=/dev/null
source "$funcs_only"

limactl_bin="$(printenv IOLBOX_LIMACTL 2>/dev/null || true)"
[ -n "$limactl_bin" ] || limactl_bin="/opt/homebrew/bin/limactl"
machine="iolbox-m4-e2e"
gui_port="$(printenv IOLBOX_M4_GUI_PORT 2>/dev/null || true)"
[ -n "$gui_port" ] || gui_port=4001
launcher_path="$(printenv IOLBOX_M4_LAUNCHER 2>/dev/null || true)"
[ -n "$launcher_path" ] || launcher_path="$HOME/iolbox-m1/iolbox-launcher-m4"
test_binary="$(printenv IOLBOX_M4_TEST_BINARY 2>/dev/null || true)"
[ -n "$test_binary" ] || test_binary="$HOME/iolbox-m1/iolbox-launcher-hardware-m4.test"
assets_dir="$(printenv IOLBOX_M4_ASSETS_DIR 2>/dev/null || true)"
[ -n "$assets_dir" ] || assets_dir="$HOME/iolbox-m1/packaging-m4"
fixtures_dir="$(printenv IOLBOX_M4_FIXTURES 2>/dev/null || true)"
[ -n "$fixtures_dir" ] || fixtures_dir="$HOME/iolbox-m1/m4-fixtures"
tarball_path="$(printenv IOLBOX_M4_TARBALL 2>/dev/null || true)"
[ -n "$tarball_path" ] || tarball_path="$HOME/iolbox-m0/iolbox-server-v0.5.2.tar.gz"
image_path="$(printenv IOLBOX_M4_IMAGE 2>/dev/null || true)"
[ -n "$image_path" ] || image_path="$HOME/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin"
evidence_parent="$(printenv IOLBOX_M4_EVIDENCE_PARENT 2>/dev/null || true)"
[ -n "$evidence_parent" ] || evidence_parent="$HOME/iolbox-m1/evidence-m4"
owner_dir="$HOME/.iolbox-m4-owner"

new_run_id
evidence_dir="$evidence_parent/startstop-$run_id"

preflight
write_owner
create_sentinels
launcher_start baseline
create_guest_sentinel
sentinel_checkpoint after-launch
ownership_snapshot before-forced

launcher_stop before-forced-launcher
record_command forced-launcher-start sh -c "nohup env IOLBOX_GUI_PORT='$gui_port' IOLBOX_TARBALL='$tarball_path' '$launcher_path' start --assets-dir '$assets_dir' --machine '$machine' --limactl '$limactl_bin' --no-browser >'$evidence_dir/item-7-forced-launcher.log' 2>&1 < /dev/null & echo \$!"
phase="$(tail -1 "$evidence_dir/commands/forced-launcher-start.stdout" | tr -d ' ')"
printf '%s\n' "$phase" >"$evidence_dir/item-7-forced-launcher.pid"
sleep 5
record_command forced-launcher-kill kill -KILL "$phase" || true
launcher_start after-forced-launcher
record_command limactl-stop-help "$limactl_bin" stop --help || true
grep -E -- '--force|--kill' "$evidence_dir/commands/limactl-stop-help.stdout" >/dev/null 2>&1 || die 'Lima forced-stop syntax unavailable'
record_command forced-vm-stop "$limactl_bin" stop --force "$machine" || die 'forced VM stop failed'
sentinel_checkpoint after-forced-vm-stop
launcher_start after-forced-vm
run_phase item-7 || die 'item 7 recovery failed'
sentinel_checkpoint after-recovery
ownership_snapshot after-recovery
launcher_stop final
sentinel_checkpoint final

printf 'start/stop sub-test PASSED; evidence at %s\n' "$evidence_dir"
