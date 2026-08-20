#!/usr/bin/env bash
# hardware-m4-phase7.sh — M7 Phase 7's targeted M4 re-verification driver.
#
# Purpose: Phase 7 has exactly two open questions that need the MATURE,
# iteratively-hardened M4 tooling rather than Phase 6's lightweight
# from-scratch Python harness (see docs/macos-m7-phase6-handoff.md,
# "Remaining gaps" 3 and 4):
#
#   Q1. Does the rosetta-amd64 router console stall Phase 6 saw (0/5, 2-node
#       vpcs-iol topology) reproduce under hardware-m4.sh's own console
#       driver (`basicPhase` -> item-1)?
#   Q2. Does four-node capacity (item-5) pass, fail, or hard-wall under the
#       mature tooling — in particular for native-arm64, which Phase 6 only
#       ever tried once with the lightweight harness?
#
# Neither question needs the full item-1 -> item-7 sequence that
# hardware-m4.sh's main() (and hardware-m4-phase5.sh's copy of it) runs.
# Running the whole matrix — including the 1200s traffic soak, NAT, extnet
# disposition, and forced-termination recovery — to answer "does item-1's
# console come up" would cost hours per data point and would additionally
# entangle the answer with the soak, which Phase 5 already proved changes
# item-5's outcome (three standalone item-5 passes vs. a 2/2 post-soak hard
# wall). For Q2 specifically, the post-soak position is exactly the variable
# Phase 5 already measured and the owner already waived; what is unmeasured
# is the standalone/fresh position on native-arm64, so pinning the soak out
# of the picture is the point, not a shortcut.
#
# What this script does NOT relax: it sources hardware-m4.sh's function
# definitions unchanged (same technique hardware-m4-power.sh,
# hardware-m4-startstop.sh and hardware-m4-phase5.sh already use) and runs
# the identical preflight(), write_owner(), create_sentinels(),
# create_guest_sentinel(), sentinel_checkpoint(), ownership_snapshot(),
# launcher_start()/launcher_stop() and run_phase() discipline around every
# item it does run. Host and guest sentinels are created before the first
# item and re-verified after every item; ownership snapshots (ps/lsof/
# ifconfig/iptables/limactl, host and guest) are taken before the first item
# and after every item; the delete-audit in sentinel_checkpoint() still runs
# at every checkpoint. The VM-name assertion in preflight() is untouched, so
# this still refuses to run against anything but iolbox-m4-e2e.
#
# Configuration (in addition to hardware-m4.sh's own IOLBOX_M4_* vars, all
# of which apply unchanged):
#   IOLBOX_M7_ITEMS   space-separated list of phases to run, in order, with
#                     repeats allowed. Default "item-1". Each occurrence gets
#                     its own evidence suffix (p7-1, p7-2, ...) so repeats do
#                     not overwrite each other and TestM4VerifyRecord's own
#                     item-5 attempt-directory scan still sees one directory
#                     per attempt.
#   IOLBOX_M7_STOP_ON_FAIL  1 to abort at the first failing item (default 0 —
#                     Phase 7 wants every repeat's result, including the
#                     failures, since "0/5" vs "3/3" IS the measurement).
#   IOLBOX_M7_CONSOLE_HOST_START / IOLBOX_M7_CAPTURE_HOST_START
#                     when set, passed through to the launcher as
#                     --console-host-start / --capture-host-start. Needed
#                     because the launcher refuses to start when its default
#                     host port ranges (GUI 4001, consoles 9000-9049,
#                     captures 5500-5529) are busy, and on this Mac another
#                     long-lived iolbox instance (the owner's own validation
#                     instance) legitimately holds 4001 and 9000. The M4 Go
#                     driver itself never dials a host console/capture port —
#                     m3OpenConsoleWithRetry dials r.guiAddr (127.0.0.1:GUI
#                     port) and nothing else — so moving these two ranges
#                     changes only which host ports the launcher forwards on,
#                     not anything the measurement observes.
#
# Deliberately NOT implemented here: item-5's reclaim-and-cold-retry path.
# That path belongs to the plan-required post-soak position, which this
# script does not put item-5 in; a cold retry here would answer a question
# nobody asked and would muddy a standalone-position result. A hard wall
# recorded by this script is recorded as-is.
set -euo pipefail

test_dir="$(cd "$(dirname "$0")" && pwd)"

funcs_only="$(mktemp)"
trap 'rm -f "$funcs_only"' EXIT
grep -v '^main "\$@"$' "$test_dir/hardware-m4.sh" > "$funcs_only"
# shellcheck source=/dev/null
source "$funcs_only"

limactl_bin="$(printenv IOLBOX_LIMACTL 2>/dev/null || true)"
[ -n "$limactl_bin" ] || limactl_bin="/opt/homebrew/bin/limactl"
machine="$(printenv IOLBOX_M4_MACHINE 2>/dev/null || true)"
[ -n "$machine" ] || machine="iolbox-m4-e2e"
gui_port="$(printenv IOLBOX_M4_GUI_PORT 2>/dev/null || true)"
[ -n "$gui_port" ] || gui_port=4001
launcher_path="$(printenv IOLBOX_M4_LAUNCHER 2>/dev/null || true)"
[ -n "$launcher_path" ] || die 'IOLBOX_M4_LAUNCHER is required'
test_binary="$(printenv IOLBOX_M4_TEST_BINARY 2>/dev/null || true)"
[ -n "$test_binary" ] || die 'IOLBOX_M4_TEST_BINARY is required'
assets_dir="$(printenv IOLBOX_M4_ASSETS_DIR 2>/dev/null || true)"
[ -n "$assets_dir" ] || die 'IOLBOX_M4_ASSETS_DIR is required'
fixtures_dir="$(printenv IOLBOX_M4_FIXTURES 2>/dev/null || true)"
[ -n "$fixtures_dir" ] || die 'IOLBOX_M4_FIXTURES is required'
tarball_path="$(printenv IOLBOX_M4_TARBALL 2>/dev/null || true)"
[ -n "$tarball_path" ] || die 'IOLBOX_M4_TARBALL is required'
image_path="$(printenv IOLBOX_M4_IMAGE 2>/dev/null || true)"
[ -n "$image_path" ] || die 'IOLBOX_M4_IMAGE is required'
evidence_parent="$(printenv IOLBOX_M4_EVIDENCE_PARENT 2>/dev/null || true)"
[ -n "$evidence_parent" ] || die 'IOLBOX_M4_EVIDENCE_PARENT is required'
owner_dir="$HOME/.iolbox-m4-owner"

items="$(printenv IOLBOX_M7_ITEMS 2>/dev/null || true)"
[ -n "$items" ] || items="item-1"
stop_on_fail="$(printenv IOLBOX_M7_STOP_ON_FAIL 2>/dev/null || true)"
[ -n "$stop_on_fail" ] || stop_on_fail=0
console_host_start="$(printenv IOLBOX_M7_CONSOLE_HOST_START 2>/dev/null || true)"
capture_host_start="$(printenv IOLBOX_M7_CAPTURE_HOST_START 2>/dev/null || true)"

# Override of the sourced launcher_start(). Identical to hardware-m4.sh's own
# except for the two optional host-port-range flags described in the header;
# with neither variable set, the argv this builds is byte-for-byte what the
# frozen function builds. Kept as an explicit override rather than an edit to
# hardware-m4.sh, which is frozen (see hardware-m4-phase5.sh's header for the
# same convention).
launcher_start() {
    local label
    label="$1"
    set -- env IOLBOX_GUI_PORT="$gui_port" IOLBOX_TARBALL="$tarball_path" "$launcher_path" \
        start --assets-dir "$assets_dir" --machine "$machine" --limactl "$limactl_bin" --no-browser
    [ -z "$console_host_start" ] || set -- "$@" --console-host-start "$console_host_start"
    [ -z "$capture_host_start" ] || set -- "$@" --capture-host-start "$capture_host_start"
    record_command "launcher-start-$label" "$@" || return $?
    record_command "readiness-$label" curl --silent --show-error --fail --connect-timeout 5 "http://127.0.0.1:$gui_port/" || return $?
}

main() {
    local item index status failures
    [ -n "$run_id" ] || new_run_id
    evidence_dir="$evidence_parent/$run_id"
    # launcher_start returns the failing status rather than calling die(), so
    # under `set -e` an unguarded call (as in hardware-m4.sh's own main())
    # aborts the whole run with NO message at all -- the run just vanishes,
    # which is precisely the "died silently" symptom Phase 5 spent hours
    # chasing for other reasons. Guarded here so the reason is always printed.
    preflight; write_owner; create_sentinels
    launcher_start baseline || die "launcher_start baseline failed (see $evidence_dir/commands/launcher-start-baseline.stderr)"
    create_guest_sentinel
    sentinel_checkpoint after-launch; ownership_snapshot before-items
    printf 'items=%s\n' "$items" >"$evidence_dir/p7-items.txt"
    : >"$evidence_dir/p7-item-status.txt"
    index=0; failures=0
    for item in $items; do
        index=$((index + 1))
        set +e; run_phase "$item" "p7-$index"; status="$?"; set -e
        printf '%s\tsuffix=p7-%s\texit=%s\n' "$item" "$index" "$status" >>"$evidence_dir/p7-item-status.txt"
        [ "$status" -eq 0 ] || failures=$((failures + 1))
        # Sentinels/ownership are checked after EVERY item, pass or fail --
        # a failing item that also disturbed host or guest state is a
        # materially different (and worse) result than one that did not,
        # and Phase 7 needs to be able to tell those apart.
        sentinel_checkpoint "after-$item-p7-$index"
        ownership_snapshot "after-$item-p7-$index"
        if [ "$status" -ne 0 ] && [ "$stop_on_fail" = "1" ]; then
            printf 'stopping at first failure per IOLBOX_M7_STOP_ON_FAIL\n' >>"$evidence_dir/p7-item-status.txt"
            break
        fi
    done
    # Same terminal ordering hardware-m4-phase5.sh established and verified on
    # real hardware: check the guest sentinel while the guest is still
    # reachable, THEN stop the launcher. hardware-m4.sh's own frozen ordering
    # (stop, then checkpoint) cannot succeed, since the checkpoint dials the
    # guest via `limactl shell`.
    sentinel_checkpoint final; launcher_stop final
    printf 'M7 Phase 7 evidence retained at %s (%s item(s) run, %s failure(s))\n' \
        "$evidence_dir" "$index" "$failures"
    cat "$evidence_dir/p7-item-status.txt"
}

main "$@"
