#!/usr/bin/env bash
# negative-rosetta-unavailable.sh - prove the canary fails closed when Lima's
# Rosetta share/binfmt registration is absent even though amd64 libc is present.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
guest_dir="$repo_root/packaging/macos/guest"
limactl_bin="${IOLBOX_LIMACTL:-limactl}"
machine_name='iolbox-neg-rosetta-unavailable'
readonly machine_name
min_free_kib=$((3 * 1024 * 1024))
keep=0
dry_run=0
machine_created=0
machine_create_attempted=0
cleanup_done=0
cleanup_error=0
machine_listing=''
before_listing=''

usage() {
    cat <<'USAGE'
Usage: negative-rosetta-unavailable.sh [--keep] [--dry-run]

Create a disposable Ubuntu 22.04 VZ+Rosetta guest, install the amd64 loader,
disable the Rosetta binfmt registration, and require FAIL_NOEXEC (exit 2).

  --keep      retain the exact throwaway guest for investigation
  --dry-run   print commands without invoking Lima or changing the host
  -h, --help  show this help
USAGE
}

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

guard_machine_name() {
    case "$machine_name" in
        iol22|m1jammy|m1trixie)
            fail "catastrophic safety violation: protected machine name selected: $machine_name"
            ;;
        iolbox-neg-rosetta-unavailable) ;;
        *) fail "unexpected throwaway machine name: $machine_name" ;;
    esac
}

capture_listing() {
    if ! machine_listing="$($limactl_bin list --format '{{.Name}}|{{.Status}}' 2>&1)"; then
        return 1
    fi
    return 0
}

# Safety existence check treats a malformed row with the exact target name as
# existing. A malformed listing must never authorize deletion.
machine_row_exists() {
    local target="$1" listing="$2"
    printf '%s\n' "$listing" | awk -F '|' -v wanted="$target" \
        '$1 == wanted { found = 1 } END { exit(found ? 0 : 1) }'
}

machine_exists() {
    capture_listing || return 2
    machine_row_exists "$machine_name" "$machine_listing"
}

machine_rows_without_target() {
    local listing="$1"
    printf '%s\n' "$listing" | awk -F '|' -v target="$machine_name" \
        '$1 != target { print }' | sort
}

cleanup() {
    local status=$? after_listing before_without after_without

    if [ "$cleanup_done" -eq 1 ]; then
        exit "$status"
    fi
    cleanup_done=1
    guard_machine_name

    if [ "$machine_created" -eq 0 ] && [ "$machine_create_attempted" -eq 1 ]; then
        if machine_exists; then
            machine_created=1
        fi
    fi

    if [ "$machine_created" -eq 1 ] && [ "$keep" -eq 0 ]; then
        printf 'Deleting throwaway Lima machine: %s\n' "$machine_name" >&2
        if ! "$limactl_bin" delete --force "$machine_name"; then
            printf 'FAIL: cleanup could not delete exact throwaway machine: %s\n' "$machine_name" >&2
            cleanup_error=1
        fi
        if capture_listing; then
            after_listing="$machine_listing"
            before_without="$(machine_rows_without_target "$before_listing")"
            after_without="$(machine_rows_without_target "$after_listing")"
            if [ "$before_without" != "$after_without" ]; then
                printf 'FAIL: cleanup changed a machine other than %s\n' "$machine_name" >&2
                cleanup_error=1
            fi
            if machine_row_exists "$machine_name" "$after_listing"; then
                printf 'FAIL: exact throwaway machine still exists after cleanup: %s\n' "$machine_name" >&2
                cleanup_error=1
            fi
        else
            printf 'FAIL: could not verify Lima listing after cleanup\n' >&2
            cleanup_error=1
        fi
    elif [ "$machine_created" -eq 1 ]; then
        printf 'Keeping throwaway Lima machine: %s\n' "$machine_name" >&2
    fi

    if [ "$cleanup_error" -ne 0 ] && [ "$status" -eq 0 ]; then
        status=1
    fi
    exit "$status"
}

print_dry_run() {
    printf '%s\n' 'DRY-RUN: no Lima commands will be executed and no VM will be created.'
    printf 'DRY-RUN: protected names: iol22 m1jammy m1trixie\n'
    printf 'DRY-RUN: exact throwaway name: %s\n' "$machine_name"
    printf 'DRY-RUN: minimum free disk: %d KiB (3 GiB)\n' "$min_free_kib"
    printf '%s\n' "$limactl_bin list --format '{{.Name}}|{{.Status}}'  # capture the complete listing"
    printf '%s\n' "$limactl_bin create --name=$machine_name template://ubuntu-22.04"
    printf '%s\n' "$limactl_bin start $machine_name --vm-type=vz --rosetta --cpus=1 --memory=1 --disk=8 --mount-none --tty=false"
    printf '%s\n' "$limactl_bin shell $machine_name sudo bash /opt/iolbox-provision/10-multiarch.sh  # install libc6:amd64 and assert the loader"
    printf '%s\n' "$limactl_bin shell $machine_name sudo sh -c 'printf -1 > /proc/sys/fs/binfmt_misc/rosetta'"
    printf '%s\n' "$limactl_bin shell $machine_name sudo bash /opt/iolbox-provision/30-canary.sh  # require FAIL_NOEXEC, exit 2"
    printf '%s\n' "$limactl_bin delete --force $machine_name  # exact name only, unless --keep"
}

guard_machine_name
trap cleanup EXIT
trap 'exit 130' INT TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --keep) keep=1; shift ;;
        --dry-run) dry_run=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; exit 1 ;;
    esac
done

guard_machine_name

if [ "$dry_run" -eq 1 ]; then
    print_dry_run
    exit 0
fi

command -v "$limactl_bin" >/dev/null 2>&1 || fail "limactl not found: $limactl_bin"
case "$(uname -s 2>/dev/null || true):$(uname -m 2>/dev/null || true)" in
    Darwin:arm64) ;;
    *) fail "real Rosetta negative test requires Apple Silicon macOS; detected $(uname -s)/$(uname -m)" ;;
esac

free_kib="$(df -Pk "${HOME:-.}" | awk 'NR == 2 { print $4 }')"
case "$free_kib" in ''|*[!0-9]*) fail 'could not determine free disk space' ;; esac
[ "$free_kib" -ge "$min_free_kib" ] || \
    fail "insufficient free disk: ${free_kib} KiB available, ${min_free_kib} KiB required"

[ -f "$guest_dir/lib.sh" ] || fail "missing guest helper: $guest_dir/lib.sh"
[ -f "$guest_dir/10-multiarch.sh" ] || fail "missing guest multiarch step: $guest_dir/10-multiarch.sh"
[ -f "$guest_dir/30-canary.sh" ] || fail "missing guest canary: $guest_dir/30-canary.sh"

capture_listing || fail "could not query Lima machine list: $machine_listing"
before_listing="$machine_listing"
if machine_row_exists "$machine_name" "$before_listing"; then
    fail "refusing to use or delete an already-existing test machine: $machine_name"
fi

printf 'Creating throwaway Lima machine: %s\n' "$machine_name"
machine_create_attempted=1
if "$limactl_bin" create --name="$machine_name" template://ubuntu-22.04; then
    machine_created=1
else
    fail "could not create $machine_name"
fi
"$limactl_bin" start "$machine_name" --vm-type=vz --rosetta --cpus=1 --memory=1 --disk=8 --mount-none --tty=false

"$limactl_bin" shell "$machine_name" sudo mkdir -p /opt/iolbox-provision
"$limactl_bin" copy "$guest_dir/lib.sh" "$machine_name:/tmp/iolbox-lib.sh"
"$limactl_bin" copy "$guest_dir/10-multiarch.sh" "$machine_name:/tmp/iolbox-10-multiarch.sh"
"$limactl_bin" copy "$guest_dir/30-canary.sh" "$machine_name:/tmp/iolbox-30-canary.sh"
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-lib.sh /opt/iolbox-provision/lib.sh
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-10-multiarch.sh /opt/iolbox-provision/10-multiarch.sh
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-30-canary.sh /opt/iolbox-provision/30-canary.sh
"$limactl_bin" shell "$machine_name" sudo chmod 0755 /opt/iolbox-provision/10-multiarch.sh /opt/iolbox-provision/30-canary.sh

printf 'Installing amd64 loader before disabling Rosetta binfmt; no payload is staged.\n'
"$limactl_bin" shell "$machine_name" sudo bash /opt/iolbox-provision/10-multiarch.sh
loader_state="$($limactl_bin shell "$machine_name" sudo sh -c 'test -x /lib64/ld-linux-x86-64.so.2 && printf yes || printf no')"
[ "$loader_state" = yes ] || fail 'amd64 loader is not present after multiarch installation'
printf 'ok: amd64 loader is present\n'

"$limactl_bin" shell "$machine_name" sudo bash -s <<'GUEST_DISABLE_ROSETTA'
set -euo pipefail
loader=/lib64/ld-linux-x86-64.so.2
test -x "$loader"
entry=/proc/sys/fs/binfmt_misc/rosetta
if [ -e "$entry" ]; then
    printf '%s\n' -1 > "$entry"
fi
if [ -e "$entry" ]; then
    grep -Fq disabled "$entry"
fi
test ! -e /opt/iolbox/supervisor
GUEST_DISABLE_ROSETTA

canary_output=''
canary_status=0
set +e
canary_output="$($limactl_bin shell "$machine_name" sudo -E env \
    IOLBOX_HOST_MACOS='negative fixture' IOLBOX_HOST_LIMA='unknown' \
    IOLBOX_MACHINE="$machine_name" IOLBOX_PROFILE='jammy' \
    IOLBOX_PROVISION_DIR=/opt/iolbox-provision \
    bash /opt/iolbox-provision/30-canary.sh 2>&1)"
canary_status=$?
set -e
printf '%s\n' "$canary_output"

[ "$canary_status" -eq 2 ] || fail "canary exit status is $canary_status, expected 2"
case "$canary_output" in
    *'FAIL_NOEXEC'*) ;;
    *) fail 'canary output did not classify the unavailable Rosetta registration as FAIL_NOEXEC' ;;
esac
case "$canary_output" in *'FAIL_AUXV'*) fail 'unavailable Rosetta was misclassified as FAIL_AUXV' ;; esac
case "$canary_output" in
    *Rosetta*binfmt*share*'brew reinstall lima'*) ;;
    *) fail 'remediation does not name the Rosetta/binfmt/share failure and brew reinstall lima' ;;
esac
printf 'ok: missing Rosetta registration fails closed as FAIL_NOEXEC\n'

if "$limactl_bin" shell "$machine_name" sudo test -e /opt/iolbox/supervisor || \
    "$limactl_bin" shell "$machine_name" sudo test -e /opt/iolbox/supervisor/supervisor; then
    fail 'negative test installed an iolbox supervisor payload'
else
    printf 'ok: no payload or /opt/iolbox/supervisor was installed\n'
fi

exit 0
