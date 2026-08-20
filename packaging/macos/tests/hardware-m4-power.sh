#!/usr/bin/env bash
# hardware-m4-power.sh — standalone power/soak sub-test.
#
# Runs ONLY item 6 (the isolated sustained-traffic soak, including its
# start/end pmset power audit, caffeinate assertion, and kern.boottime
# check), skipping items 1-5/7/8. Sources hardware-m4.sh's function
# definitions (everything before its own unconditional `main "$@"` call)
# and drives just the baseline launch + item-6 sequence directly, so the
# exact same preflight/sentinel/soak-seal discipline applies without
# re-implementing it.
set -euo pipefail

test_dir="$(cd "$(dirname "$0")" && pwd)"

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
evidence_dir="$evidence_parent/power-$run_id"

preflight
write_owner
create_sentinels
launcher_start baseline
create_guest_sentinel
sentinel_checkpoint after-launch
ownership_snapshot before-soak

mkdir -p "$evidence_dir/item-6"
nohup env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE=item-6 IOLBOX_M4_PHASE_SUFFIX= IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_IMAGE="$image_path" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v >"$evidence_dir/item-6/soak-process.log" 2>&1 < /dev/null &
phase="$!"
printf '%s\n' "$phase" >"$evidence_dir/item-6/soak.pid"
while kill -0 "$phase" >/dev/null 2>&1; do sleep 30; done
set +e; wait "$phase"; target="$?"; set -e
[ "$target" -eq 0 ] || die 'isolated soak failed'
record_command soak-seal-verify "$test_binary" -test.run '^TestM4VerifySoakSeal$' -test.v -m4-soak "$evidence_dir/item-6/SOAK-COMPLETE" || die 'soak seal verification failed'
[ -f "$evidence_dir/item-6/SOAK-COMPLETE" ] || die 'soak seal is missing'

sentinel_checkpoint after-soak
ownership_snapshot after-soak
launcher_stop final
sentinel_checkpoint final

printf 'power/soak sub-test PASSED; evidence at %s\n' "$evidence_dir"
