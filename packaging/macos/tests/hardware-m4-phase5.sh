#!/usr/bin/env bash
# hardware-m4-phase5.sh — M7 Phase 5's full-matrix M4 run.
#
# Runs the exact same items-1-through-7 sequence as hardware-m4.sh's main(),
# in the exact same order, sourcing that script's function definitions
# unchanged (the same technique hardware-m4-power.sh and
# hardware-m4-startstop.sh already use for their own narrower subsets — see
# those scripts' own header comments). The only substantive difference is
# skipping hardware-m4.sh's `preserve_vm` calls against five specific
# by-name VMs (m1jammy, m1trixie, iolbox-m1-e2e, iolbox-m2-e2e,
# iolbox-m3-e2e) before/around item 5's RAM-reclaim path.
#
# Why: those five VMs were disposable per-milestone evidence machines from
# the historical M1-M3 hardware runs. None of them exist on the physical Mac
# any more (independently confirmed via `limactl list` across every known
# LIMA_HOME on this Mac before writing this script — see
# docs/m7-evidence/phase5/STATUS.md). hardware-m4.sh's preserve_vm() hard-
# dies with "unresolved VM: <name>" for any Missing VM, so running it
# unmodified is not possible in this environment; fabricating stand-in VMs
# under those exact names to force the precondition to pass would be worse
# — it would produce evidence that LOOKS like the historical witness-
# preservation proof without actually being it. Real fabricated evidence is
# exactly what this whole project's protocol forbids.
#
# What this script does instead: takes real before/after ownership
# snapshots (ownership_snapshot, ps/lsof/ifconfig/iptables — the same
# mechanism hardware-m4.sh already uses at every other checkpoint) around
# item 5, and separately records the state of every OTHER real VM that
# actually exists on this Mac right now (the ones this session was told are
# protected and must never be touched: iolbox-m5-e2e,
# iolbox-m7-native-arm64-qemu, plus Phase 4's own iolbox-debian13/
# iolbox-native-arm64 under ~/.lima-iolbox-p4) before and after item 5, so a
# real regression — this run touching a VM it has no business touching —
# would still show up in the diff. This is a strictly more relevant witness
# set for this Mac's actual current state than five VMs that no longer
# exist would have been.
#
# Every other item (1 VPCS/IOL, 2 multi-link, 3 NAT, 4 extnet, 6 soak, 5
# four-node capacity, 7 forced termination, final record/verify) runs
# exactly as hardware-m4.sh's main() runs it, via the same sourced
# functions, with the same sentinel/ownership/preflight discipline.
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
[ -n "$machine" ] || machine="iolbox-p5-m4-e2e"
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

# Real substitute witnesses: every other VM that actually exists on this Mac
# right now, across every LIMA_HOME this session was told about. Format:
# "LIMA_HOME|name".
other_real_vms="
|iolbox-m5-e2e
$HOME/.lima-iolbox-m7p3|iolbox-m7-native-arm64-qemu
$HOME/.lima-iolbox-p4|iolbox-debian13
$HOME/.lima-iolbox-p4|iolbox-native-arm64
"

snapshot_other_vms() {
    local label out
    label="$1"; out="$evidence_dir/item-5-other-vms-$label.txt"
    : >"$out"
    printf '%s\n' "$other_real_vms" | while IFS='|' read -r home name; do
        [ -n "$name" ] || continue
        if [ -n "$home" ]; then
            printf '== LIMA_HOME=%s %s ==\n' "$home" "$name" >>"$out"
            LIMA_HOME="$home" "$limactl_bin" list "$name" >>"$out" 2>&1 || true
        else
            printf '== default LIMA_HOME %s ==\n' "$name" >>"$out"
            "$limactl_bin" list "$name" >>"$out" 2>&1 || true
        fi
    done
}

main() {
    local target phase
    [ -n "$run_id" ] || new_run_id
    evidence_dir="$evidence_parent/$run_id"
    preflight; write_owner; create_sentinels; launcher_start baseline; create_guest_sentinel
    sentinel_checkpoint after-launch; ownership_snapshot item-1-before
    run_phase item-1 || die 'item 1 VPCS/IOL failed'; sentinel_checkpoint after-item-1; ownership_snapshot item-1-after
    run_phase item-2 || die 'item 2 multi-link failed'; sentinel_checkpoint after-item-2
    target="$(env_or IOLBOX_M4_NAT_TARGET 1.1.1.1)"; mkdir -p "$evidence_dir/item-3"
    printf 'numeric_target=%s\nrationale=selected before NAT controls\n' "$target" >"$evidence_dir/item-3-target.txt"
    record_command nat-before-mac ping -c 3 -t 2 "$target" || true; record_command nat-before-guest "$limactl_bin" shell "$machine" ping -c 3 -W 2 "$target" || true
    run_phase item-3 || die 'item 3 NAT failed or unverified'
    record_command nat-after-mac ping -c 3 -t 2 "$target" || true; record_command nat-after-guest "$limactl_bin" shell "$machine" ping -c 3 -W 2 "$target" || true
    sentinel_checkpoint after-item-3; ownership_snapshot item-3-after; extnet_phase; sentinel_checkpoint after-item-4; ownership_snapshot item-4-after
    mkdir -p "$evidence_dir/item-6"
    nohup env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE=item-6 IOLBOX_M4_PHASE_SUFFIX= IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_IMAGE="$image_path" IOLBOX_M4_SOAK_SECONDS="$(env_or IOLBOX_M4_SOAK_SECONDS 600)" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v >"$evidence_dir/item-6/soak-process.log" 2>&1 < /dev/null &
    phase="$!"
    printf '%s\n' "$phase" >"$evidence_dir/item-6/soak.pid"
    while kill -0 "$phase" >/dev/null 2>&1; do sleep 30; done
    set +e; wait "$phase"; target="$?"; set -e
    [ "$target" -eq 0 ] || die 'isolated soak failed; later phases are locked'
    record_command soak-seal-verify "$test_binary" -test.run '^TestM4VerifySoakSeal$' -test.v -m4-soak "$evidence_dir/item-6/SOAK-COMPLETE" || die 'soak seal verification failed'
    [ -f "$evidence_dir/item-6/SOAK-COMPLETE" ] || die 'soak seal is missing'
    snapshot_other_vms before-item-5
    ownership_snapshot item-5-before; set +e; run_phase item-5 attempt-1; phase="$?"; set -e
    if [ "$phase" -ne 0 ]; then
        grep -F '"hard_wall": true' "$evidence_dir/item-5/attempt-1/phase.json" >/dev/null 2>&1 || die 'item 5 failed without recorded hard wall'
        record_command reclaim-physmem top -l 1 -s 0 || true; record_command reclaim-pressure memory_pressure -Q || true; record_command reclaim-swap sysctl vm.swapusage || true
        # hardware-m4.sh's own original ordering here is
        # "launcher_stop; sentinel_checkpoint; launcher_start" -- calling
        # sentinel_checkpoint (which dials the GUEST via `limactl shell`,
        # requiring a running VM) between a stop and the next start.  Found
        # on real hardware: this guarantees failure the moment the retry
        # path is actually exercised, since the VM the guest sentinel check
        # dials is stopped at that exact point ("FAIL: guest sentinel
        # failed at after-ram-reclaim"), which explains why every real
        # item-5 hard-wall retry died right here with no further evidence.
        # No M4 run in this project's history appears to have exercised
        # this branch on real hardware before Phase 5. Fixed here (not in
        # the frozen hardware-m4.sh) by checking the guest sentinel AFTER
        # the VM is back up post-restart, which is also the only point
        # where "did state survive the reclaim cycle" is actually
        # answerable.
        launcher_stop ram-reclaim; launcher_start cold-retry; sentinel_checkpoint after-ram-reclaim
        run_phase item-5 attempt-2 || die 'item 5 retry hard wall: M4 BLOCKED/UNVERIFIED (no m1jammy/m1trixie/iolbox-m1..m3-e2e witnesses exist on this Mac to reclaim RAM from -- see this script header)'
    fi
    sentinel_checkpoint after-item-5; ownership_snapshot item-5-after; snapshot_other_vms after-item-5
    launcher_stop before-forced-launcher
    record_command forced-launcher-start sh -c "nohup env IOLBOX_GUI_PORT='$gui_port' IOLBOX_TARBALL='$tarball_path' '$launcher_path' start --assets-dir '$assets_dir' --machine '$machine' --limactl '$limactl_bin' --no-browser >'$evidence_dir/item-7-forced-launcher.log' 2>&1 < /dev/null & echo \$!"
    phase="$(tail -1 "$evidence_dir/commands/forced-launcher-start.stdout" | tr -d ' ')"; printf '%s\n' "$phase" >"$evidence_dir/item-7-forced-launcher.pid"
    sleep 5; record_command forced-launcher-kill kill -KILL "$phase" || true; launcher_start after-forced-launcher
    record_command limactl-stop-help "$limactl_bin" stop --help || true
    grep -E -- '--force|--kill' "$evidence_dir/commands/limactl-stop-help.stdout" >/dev/null 2>&1 || die 'Lima forced-stop syntax unavailable'
    record_command forced-vm-stop "$limactl_bin" stop --force "$machine" || die 'forced VM stop failed'
    # Same ordering bug as the item-5 reclaim path above (see that comment):
    # sentinel_checkpoint dials the guest via `limactl shell`, which needs a
    # running VM, but forced-vm-stop just stopped it. Confirmed on real
    # hardware ("FAIL: guest sentinel failed at after-forced-vm-stop") by
    # manually driving this exact sequence. Fixed the same way: check the
    # guest sentinel after launcher_start brings the VM back up, not before.
    launcher_start after-forced-vm; sentinel_checkpoint after-forced-vm-stop; run_phase item-7 || die 'item 7 recovery failed'
    sentinel_checkpoint after-recovery; ownership_snapshot item-7-after
    # Third instance of the same ordering bug: unlike the other two (which
    # have a subsequent launcher_start to move the checkpoint after), this
    # is the terminal stop with nothing after it to bring the guest back --
    # confirmed on real hardware ("FAIL: guest sentinel failed at final")
    # driving this exact sequence. Fixed by checking the guest sentinel
    # while it is still reachable, before this final stop, matching every
    # other mid-run checkpoint's implicit assumption that the VM it is
    # sentinel-checking is currently running.
    build_requirements; sentinel_checkpoint final; launcher_stop final
    env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE=final IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_PLAN_SHA256="$(env_or IOLBOX_M4_PLAN_SHA256 unknown)" IOLBOX_M4_PLAN_UNCHANGED="$(env_or IOLBOX_M4_PLAN_UNCHANGED 0)" IOLBOX_M4_BASE_COMMIT="$(env_or IOLBOX_M4_BASE_COMMIT unknown)" IOLBOX_M4_RUN_START_UTC="$(grep '^start_utc=' "$evidence_dir/README.txt" | cut -d= -f2)" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v >"$evidence_dir/final-summary.log" 2>&1 || true
    set +e; "$test_binary" -test.run '^TestM4VerifyRecord$' -test.v -m4-record "$evidence_dir/summary.json" >"$evidence_dir/verifier.log" 2>&1; phase="$?"; set -e
    printf '%s\n' "$phase" >"$evidence_dir/verifier.status"; printf 'M4 evidence retained at %s (verifier status %s)\n' "$evidence_dir" "$phase"
}

main "$@"
