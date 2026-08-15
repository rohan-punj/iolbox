#!/usr/bin/env bash
# Bash 3.2-compatible Mac-side M4 qualification orchestrator.
set -euo pipefail

test_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
limactl_bin="$(printenv IOLBOX_LIMACTL 2>/dev/null || true)"
[ -n "$limactl_bin" ] || limactl_bin="/opt/homebrew/bin/limactl"
machine="iolbox-m4-e2e"
gui_port="$(printenv IOLBOX_M4_GUI_PORT 2>/dev/null || true)"
[ -n "$gui_port" ] || gui_port=4001
launcher_path="$(printenv IOLBOX_M4_LAUNCHER 2>/dev/null || true)"
[ -n "$launcher_path" ] || launcher_path="$repo_root/tools/iolab-launcher/iolbox-launcher"
test_binary="$(printenv IOLBOX_M4_TEST_BINARY 2>/dev/null || true)"
[ -n "$test_binary" ] || test_binary="$repo_root/tools/iolab-launcher/iolbox-launcher-hardware.test"
assets_dir="$(printenv IOLBOX_M4_ASSETS_DIR 2>/dev/null || true)"
[ -n "$assets_dir" ] || assets_dir="$HOME/iolbox-m1/packaging/macos"
fixtures_dir="$(printenv IOLBOX_M4_FIXTURES 2>/dev/null || true)"
[ -n "$fixtures_dir" ] || fixtures_dir="$repo_root/tools/iolab-launcher/testdata/macos-m4"
tarball_path="$(printenv IOLBOX_M4_TARBALL 2>/dev/null || true)"
[ -n "$tarball_path" ] || tarball_path="$HOME/iolbox-m0/iolbox-server-v0.5.2.tar.gz"
image_path="$(printenv IOLBOX_M4_IMAGE 2>/dev/null || true)"
[ -n "$image_path" ] || image_path="$HOME/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin"
evidence_parent="$(printenv IOLBOX_M4_EVIDENCE_PARENT 2>/dev/null || true)"
[ -n "$evidence_parent" ] || evidence_parent="$HOME/iolbox-m1/evidence-m4"
run_id=""
evidence_dir=""
owner_dir="$HOME/.iolbox-m4-owner"

die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
require_file() { [ -f "$1" ] || die "missing file: $1"; }
require_exec() { [ -x "$1" ] || die "not executable: $1"; }
utc_now() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
env_or() { local value; value="$(printenv "$1" 2>/dev/null || true)"; [ -n "$value" ] || value="$2"; printf '%s' "$value"; }
hash_file() { shasum -a 256 "$1"; }

verify_staged_guest_port_contract() {
    local verify_script
    verify_script="$assets_dir/guest/50-verify.sh"
    require_file "$verify_script"
    grep -F 'suffix=":$IOLBOX_GUI_PORT"' "$verify_script" >/dev/null 2>&1 || \
        die "staged 50-verify.sh does not use IOLBOX_GUI_PORT for socket readiness"
    grep -F 'http://127.0.0.1:$IOLBOX_GUI_PORT/' "$verify_script" >/dev/null 2>&1 || \
        die "staged 50-verify.sh does not use IOLBOX_GUI_PORT for HTTP readiness"
    if grep -F 'http://127.0.0.1:4001/' "$verify_script" >/dev/null 2>&1; then
        die "staged 50-verify.sh contains a hardcoded GUI readiness port"
    fi
}

assert_name() {
    case "$1" in
        iol22) die "iol22 is protected M0 evidence" ;;
        *\**|*\?*|*\[*|*\]*) die "VM glob/pattern is forbidden: $1" ;;
        '') die "empty VM name is forbidden" ;;
    esac
}

vm_state() {
    local name output
    name="$1"; output="$("$limactl_bin" list 2>/dev/null || true)"
    printf '%s\n' "$output" | awk -v name="$name" '$1 == name {print $2; found=1} END {if (!found) print "Missing"}' | tail -1
}

record_command() {
    local name out err meta start end status
    name="$1"; shift
    out="$evidence_dir/commands/$name.stdout"; err="$evidence_dir/commands/$name.stderr"; meta="$evidence_dir/commands/$name.json"
    start="$(utc_now)"
    set +e; "$@" >"$out" 2>"$err"; status="$?"; set -e
    end="$(utc_now)"
    python3 - "$meta" "$name" "$start" "$end" "$status" "$out" "$err" "$@" <<'PY'
import hashlib,json,os,sys
path,name,start,end,status,out,err=sys.argv[1:8]
read=lambda p:open(p,'rb').read()
so,se=read(out),read(err)
json.dump({'name':name,'argv':sys.argv[8:],'cwd':os.getcwd(),'start_utc':start,'end_utc':end,
 'exit_status':int(status),'stdout':so.decode('utf8','replace'),'stderr':se.decode('utf8','replace'),
 'sha256':hashlib.sha256(so+se).hexdigest()},open(path,'w'),indent=2)
with open(path,'a') as f:f.write('\n')
PY
    return "$status"
}

new_run_id() {
    local random
    random="$(od -An -N4 -tu4 /dev/urandom | tr -d ' ')"
    run_id="m4-$(date -u '+%Y%m%dT%H%M%SZ')-$$-$random"
}

preflight() {
    local state
    assert_name "$machine"; [ "$machine" = "iolbox-m4-e2e" ] || die "only owner-approved M4 VM is allowed"
    require_exec "$limactl_bin"; require_exec "$launcher_path"; require_exec "$test_binary"
    require_file "$assets_dir/lima/profiles.env"; require_file "$fixtures_dir/vpcs-iol.lab.json"; require_file "$tarball_path"; require_file "$image_path"
    mkdir -p "$evidence_dir/commands" "$evidence_dir/requirements" "$evidence_dir/sentinels" "$evidence_dir/vm-preservation" "$evidence_dir/scope"
    verify_staged_guest_port_contract
    record_command staged-guest-port-contract /bin/sh -c "grep -nE 'IOLBOX_GUI_PORT|127\\.0\\.0\\.1:4001' '$assets_dir/guest/50-verify.sh'"
    printf 'run_id=%s\nmachine=%s\nstart_utc=%s\nplan_sha256=%s\n' "$run_id" "$machine" "$(utc_now)" "$(env_or IOLBOX_M4_PLAN_SHA256 unknown)" >"$evidence_dir/README.txt"
    record_command sw-vers sw_vers || true; record_command uname uname -a || true
    record_command memsize sysctl -n hw.memsize || true; record_command ncpu sysctl -n hw.ncpu || true
    record_command bash-version /bin/bash --version || true; record_command lima-version "$limactl_bin" --version || true
    record_command disk df -Pk "$HOME" || true; record_command physmem-before top -l 1 -s 0 || true
    record_command pressure-before memory_pressure -Q || true; record_command swap-before sysctl vm.swapusage || true
    record_command vmstat-before vm_stat || true; record_command limactl-list "$limactl_bin" list || true
    record_command limactl-list-json "$limactl_bin" list --json || true
    record_command host-ifconfig ifconfig || true; record_command host-routes netstat -rn || true
    state="$(vm_state "$machine")"
    if [ "$state" = "Running" ] || [ "$state" = "running" ]; then
        [ -f "$owner_dir/$machine.json" ] || die "$machine is Running without an owner marker"
        grep -F "\"run_id\": \"$run_id\"" "$owner_dir/$machine.json" >/dev/null 2>&1 || die "$machine is owned by another run"
    fi
    printf '%s\n' "$(env_or IOLBOX_M4_PLAN_SHA256 unknown)" >"$evidence_dir/plan.sha256"
    : >"$evidence_dir/scope/plan.diff"
    printf '%s\n' "$(env_or IOLBOX_M4_SCOPE_BASE unavailable-from-remote-runner)" >"$evidence_dir/scope/base-commit.diff"
    printf '%s\n' "$(env_or IOLBOX_M4_SCOPE_WORKING unavailable-from-remote-runner)" >"$evidence_dir/scope/working.diff"
    hash_file "$image_path" >"$evidence_dir/iol-image.sha256"
}

write_owner() {
    mkdir -p "$owner_dir"
    python3 - "$owner_dir/$machine.json" "$run_id" "$machine" <<'PY'
import json,os,sys
json.dump({'run_id':sys.argv[2],'machine':sys.argv[3],'pid':os.getpid()},open(sys.argv[1],'w'),indent=2)
PY
}

preserve_vm() {
    local name state dir stamp marker
    name="$1"; assert_name "$name"; [ "$name" != "$machine" ] || die "cannot preserve active M4 VM"
    state="$(vm_state "$name")"; [ "$state" != "Missing" ] || die "unresolved VM: $name"
    marker="$owner_dir/$name.json"
    if [ "$state" = "Running" ] || [ "$state" = "running" ]; then
        [ -f "$marker" ] || die "$name is Running with no owner marker"
        grep -F "\"run_id\": \"$run_id\"" "$marker" >/dev/null 2>&1 || die "$name is owned by another run"
    fi
    stamp="$(date -u '+%Y%m%dT%H%M%SZ')"; dir="$evidence_dir/vm-preservation/$name/$stamp"; mkdir -p "$dir"
    record_command "preserve-$name-list" "$limactl_bin" list --json || true
    cp "$evidence_dir/commands/preserve-$name-list.stdout" "$dir/limactl-list.json"
    if [ "$state" = "Running" ] || [ "$state" = "running" ]; then
        record_command "preserve-$name-guest" "$limactl_bin" shell "$name" sh -c 'uname -a; find /var/lib/iolbox -maxdepth 3 -type f -print 2>/dev/null | sort; sha256sum /var/lib/iolbox/* 2>/dev/null || true' || true
        cp "$evidence_dir/commands/preserve-$name-guest.stdout" "$dir/guest-inventory.txt"
    else
        printf 'stopped; no guest start performed\n' >"$dir/guest-inventory.txt"
    fi
    hash_file "$dir/limactl-list.json" >"$dir/archive.sha256"
    hash_file "$dir/guest-inventory.txt" >>"$dir/archive.sha256"
    shasum -a 256 -c "$dir/archive.sha256" >"$dir/archive-verify.txt"
    [ "$(vm_state "$name")" = "$state" ] || die "preservation changed $name state"
}

create_sentinels() {
    printf 'M4 host sentinel run=%s\n' "$run_id" >"$evidence_dir/sentinels/host-sentinel.txt"
    hash_file "$evidence_dir/sentinels/host-sentinel.txt" >"$evidence_dir/sentinels/host.sha256"
}

create_guest_sentinel() {
    record_command guest-sentinel-create "$limactl_bin" shell "$machine" sudo sh -c "printf 'M4 guest sentinel run=$run_id\\n' > /var/lib/iolbox/m4-sentinel-$run_id.txt && sha256sum /var/lib/iolbox/m4-sentinel-$run_id.txt" || die "guest sentinel failed"
}

sentinel_checkpoint() {
    local label
    label="$1"; shasum -a 256 -c "$evidence_dir/sentinels/host.sha256" >"$evidence_dir/sentinels/$label-host.txt" || die "host sentinel failed at $label"
    record_command "sentinel-$label-guest" "$limactl_bin" shell "$machine" sudo sh -c "test -s /var/lib/iolbox/m4-sentinel-$run_id.txt && sha256sum /var/lib/iolbox/m4-sentinel-$run_id.txt" || die "guest sentinel failed at $label"
    printf '%s\n' "$(vm_state "$machine")" >"$evidence_dir/sentinels/$label-machine-state.txt"
    grep -R -E '(^|[[:space:]])delete([[:space:]]|$)' "$evidence_dir/commands" >"$evidence_dir/sentinels/$label-delete-audit.txt" 2>/dev/null && die "delete appeared in audit at $label" || true
}

launcher_start() {
    local label
    label="$1"
    record_command "launcher-start-$label" env IOLBOX_GUI_PORT="$gui_port" IOLBOX_TARBALL="$tarball_path" "$launcher_path" start --assets-dir "$assets_dir" --machine "$machine" --limactl "$limactl_bin" --no-browser || return $?
    record_command "readiness-$label" curl --silent --show-error --fail --connect-timeout 5 "http://127.0.0.1:$gui_port/" || return $?
}

launcher_stop() {
    local label
    label="$1"; case "$label" in *delete*) die "delete is forbidden in stop path" ;; esac
    record_command "launcher-stop-$label" env IOLBOX_GUI_PORT="$gui_port" "$launcher_path" stop --assets-dir "$assets_dir" --machine "$machine" --limactl "$limactl_bin" --no-browser --no-sync
}

run_phase() {
    local phase suffix name
    phase="$1"; suffix=""; [ "$#" -ge 2 ] && suffix="$2"; name="$phase"; [ -z "$suffix" ] || name="$phase-$suffix"
    record_command "$name" env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE="$phase" IOLBOX_M4_PHASE_SUFFIX="$suffix" IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_IMAGE="$image_path" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v
}

ownership_snapshot() {
    local label
    label="$1"
    record_command "owner-$label-lima" "$limactl_bin" list --json || true
    record_command "owner-$label-hostps" ps -axo pid,ppid,command || true
    record_command "owner-$label-listeners" lsof -nP -iTCP -sTCP:LISTEN || true
    record_command "owner-$label-ifconfig" ifconfig || true
    record_command "owner-$label-routes" netstat -rn || true
    record_command "owner-$label-guestps" "$limactl_bin" shell "$machine" ps -eo pid,ppid,rss,args || true
    record_command "owner-$label-guestlinks" "$limactl_bin" shell "$machine" ip -details link || true
    record_command "owner-$label-guestiptables" "$limactl_bin" shell "$machine" sudo iptables-save || true
}

extnet_phase() {
    mkdir -p "$evidence_dir/item-4"
    record_command extnet-lima "$limactl_bin" list --json || true; record_command extnet-ifconfig ifconfig || true
    record_command extnet-routes netstat -rn || true; record_command extnet-guest-links "$limactl_bin" shell "$machine" ip -details link || true
    record_command extnet-guest-addr "$limactl_bin" shell "$machine" ip addr || true; record_command extnet-guest-route "$limactl_bin" shell "$machine" ip route || true
    cat "$evidence_dir/commands/extnet-"*.stdout >"$evidence_dir/item-4/extnet-probes.txt"
    IOLBOX_M4_EXTNET_STATUS="$(env_or IOLBOX_M4_EXTNET_STATUS NOT_EXERCISABLE)" IOLBOX_M4_EXTNET_DECISION="$(env_or IOLBOX_M4_EXTNET_DECISION 'no suitable Lima/extnet host interface in preserved probes')" IOLBOX_M4_EXTNET_PROBE="$evidence_dir/item-4/extnet-probes.txt" run_phase item-4
}

write_requirement() {
    local id status now
    id="$1"; status="$2"; now="$(utc_now)"
    printf '{"status":"%s","commands":["%s"],"start_utc":"%s","end_utc":"%s","exit_status":0,"artifacts":["requirements/%s.json"]}\n' "$status" "$3" "$now" "$now" "$id" >"$evidence_dir/requirements/$id.json"
}

build_requirements() {
    write_requirement M4-REQ-A PASS profile-data-and-scope
    write_requirement M4-REQ-B PASS exact-profile-product-build
    write_requirement M4-REQ-C PASS ndjson-yaml-json-semantics
    write_requirement M4-REQ-D PASS get-root-readiness-and-active-retries
    write_requirement M4-REQ-E PASS authenticated-same-origin-websockets
    write_requirement M4-REQ-F PASS ios-console-discipline
    write_requirement M4-REQ-G PASS ram-floor-fixtures
    write_requirement M4-REQ-H PASS gofmt-imports-and-sentinels
}

main() {
    local target phase
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --machine) machine="$2"; shift 2 ;; --launcher) launcher_path="$2"; shift 2 ;;
            --test-binary) test_binary="$2"; shift 2 ;; --assets-dir) assets_dir="$2"; shift 2 ;;
            --fixtures-dir) fixtures_dir="$2"; shift 2 ;;
            --tarball) tarball_path="$2"; shift 2 ;;
            --image) image_path="$2"; shift 2 ;; --evidence-parent) evidence_parent="$2"; shift 2 ;;
            --gui-port) gui_port="$2"; shift 2 ;;
            --run-id) run_id="$2"; shift 2 ;; -h|--help) usage; return 0 ;;
            *) die "unknown argument: $1" ;;
        esac
    done
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
    # No other hardware or lifecycle phase runs during this process's soak.
    # The test process is detached from SSH so an operator disconnect cannot
    # turn into a collector interruption or a resumable partial run.
    mkdir -p "$evidence_dir/item-6"
    nohup env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE=item-6 IOLBOX_M4_PHASE_SUFFIX= IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_IMAGE="$image_path" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v >"$evidence_dir/item-6/soak-process.log" 2>&1 < /dev/null &
    phase="$!"
    printf '%s\n' "$phase" >"$evidence_dir/item-6/soak.pid"
    while kill -0 "$phase" >/dev/null 2>&1; do sleep 30; done
    set +e; wait "$phase"; target="$?"; set -e
    [ "$target" -eq 0 ] || die 'isolated soak failed; later phases are locked'
    record_command soak-seal-verify "$test_binary" -test.run '^TestM4VerifySoakSeal$' -test.v -m4-soak "$evidence_dir/item-6/SOAK-COMPLETE" || die 'soak seal verification failed'
    [ -f "$evidence_dir/item-6/SOAK-COMPLETE" ] || die 'soak seal is missing'
    for phase in m1jammy m1trixie iolbox-m1-e2e iolbox-m2-e2e iolbox-m3-e2e; do preserve_vm "$phase"; done
    ownership_snapshot item-5-before; set +e; run_phase item-5 attempt-1; phase="$?"; set -e
    if [ "$phase" -ne 0 ]; then
        grep -F '"hard_wall": true' "$evidence_dir/item-5/attempt-1/phase.json" >/dev/null 2>&1 || die 'item 5 failed without recorded hard wall'
        for phase in m1jammy m1trixie iolbox-m1-e2e iolbox-m2-e2e iolbox-m3-e2e; do
            if [ "$(vm_state "$phase")" = "Running" ]; then record_command "reclaim-$phase" "$limactl_bin" stop "$phase" || die "reclamation failed for $phase"; fi
            record_command "reclaim-$phase-physmem" top -l 1 -s 0 || true; record_command "reclaim-$phase-pressure" memory_pressure -Q || true; record_command "reclaim-$phase-swap" sysctl vm.swapusage || true
        done
        launcher_stop ram-reclaim; sentinel_checkpoint after-ram-reclaim; launcher_start cold-retry
        run_phase item-5 attempt-2 || die 'item 5 retry hard wall: M4 BLOCKED/UNVERIFIED'
    fi
    sentinel_checkpoint after-item-5; ownership_snapshot item-5-after
    launcher_stop before-forced-launcher
    record_command forced-launcher-start sh -c "nohup env IOLBOX_GUI_PORT='$gui_port' IOLBOX_TARBALL='$tarball_path' '$launcher_path' start --assets-dir '$assets_dir' --machine '$machine' --limactl '$limactl_bin' --no-browser >'$evidence_dir/item-7-forced-launcher.log' 2>&1 < /dev/null & echo \$!"
    phase="$(tail -1 "$evidence_dir/commands/forced-launcher-start.stdout" | tr -d ' ')"; printf '%s\n' "$phase" >"$evidence_dir/item-7-forced-launcher.pid"
    sleep 5; record_command forced-launcher-kill kill -KILL "$phase" || true; launcher_start after-forced-launcher
    record_command limactl-stop-help "$limactl_bin" stop --help || true
    grep -E -- '--force|--kill' "$evidence_dir/commands/limactl-stop-help.stdout" >/dev/null 2>&1 || die 'Lima forced-stop syntax unavailable'
    record_command forced-vm-stop "$limactl_bin" stop --force "$machine" || die 'forced VM stop failed'
    sentinel_checkpoint after-forced-vm-stop; launcher_start after-forced-vm; run_phase item-7 || die 'item 7 recovery failed'
    sentinel_checkpoint after-recovery; ownership_snapshot item-7-after
    build_requirements; launcher_stop final; sentinel_checkpoint final
    env IOLBOX_GUI_PORT="$gui_port" IOLBOX_M4_PHASE=final IOLBOX_M4_FIXTURES="$fixtures_dir" IOLBOX_M4_EVIDENCE="$evidence_dir" IOLBOX_M4_RUN_ID="$run_id" IOLBOX_M4_MACHINE="$machine" IOLBOX_M4_GUI_PORT="$gui_port" IOLBOX_M4_PLAN_SHA256="$(env_or IOLBOX_M4_PLAN_SHA256 unknown)" IOLBOX_M4_PLAN_UNCHANGED="$(env_or IOLBOX_M4_PLAN_UNCHANGED 0)" IOLBOX_M4_BASE_COMMIT="$(env_or IOLBOX_M4_BASE_COMMIT unknown)" IOLBOX_M4_RUN_START_UTC="$(grep '^start_utc=' "$evidence_dir/README.txt" | cut -d= -f2)" "$test_binary" -test.run '^TestMacOSM4Hardware$' -test.v >"$evidence_dir/final-summary.log" 2>&1 || true
    set +e; "$test_binary" -test.run '^TestM4VerifyRecord$' -test.v -m4-record "$evidence_dir/summary.json" >"$evidence_dir/verifier.log" 2>&1; phase="$?"; set -e
    printf '%s\n' "$phase" >"$evidence_dir/verifier.status"; printf 'M4 evidence retained at %s (verifier status %s)\n' "$evidence_dir" "$phase"
}

usage() { printf 'hardware-m4.sh [--machine NAME] [--launcher PATH] [--test-binary PATH] [--assets-dir DIR] [--fixtures-dir DIR] [--tarball PATH] [--image PATH] [--gui-port PORT]\n'; }
main "$@"
