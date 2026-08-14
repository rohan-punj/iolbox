#!/usr/bin/env bash
# negative-kernel68.sh — prove that the measured 6.8-kernel incompatibility
# is rejected by the executable canary before any iolbox payload is installed.
#
# This test creates only the following disposable machine geometry:
#   Ubuntu 24.04 arm64, VZ + Rosetta, 1 vCPU, 1 GiB RAM, 8 GiB disk
# It stages lib.sh, agent B's 10-multiarch.sh, and 30-canary.sh; no payload,
# images, or iolbox installation are performed. The host must have at least
# 3 GiB free on the filesystem containing $HOME before Lima is started. The
# disk image may be sparse, but the image download and amd64 libc install need
# real host headroom. Use --keep only when retaining the disposable VM for
# investigation is intentional.

set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
guest_dir="$repo_root/packaging/macos/guest"
machine_name='iolbox-neg68'
min_free_kib=$((3 * 1024 * 1024))
limactl_bin="${IOLBOX_LIMACTL:-limactl}"
keep=0
dry_run=0
machine_created=0
machine_create_attempted=0
cleanup_done=0

usage() {
    cat <<'USAGE'
Usage: negative-kernel68.sh [--keep] [--dry-run]

Create a throwaway Ubuntu 24.04/kernel-6.8 Lima guest and assert that the
Rosetta canary rejects it with FAIL_AUXV and exit status 2.

  --keep      retain iolbox-neg68 for investigation after the test
  --dry-run   print the exact host/guest commands without using Lima
USAGE
}

fail() {
    printf 'FAIL: %s\n' "$*"
    exit 1
}

machine_exists() {
    "$limactl_bin" list --format '{{.Name}}' 2>/dev/null | grep -Fxq "$machine_name"
}

cleanup() {
    local status=$?

    if [ "$cleanup_done" -eq 1 ]; then
        exit "$status"
    fi
    cleanup_done=1

    # If an interrupt arrived while limactl start was in progress, the start
    # command may not have returned to set machine_created. The name was
    # confirmed absent immediately before the attempt, so an extant exact-name
    # machine here is the throwaway this test created.
    if [ "$machine_created" -eq 0 ] && [ "$machine_create_attempted" -eq 1 ] && \
        [ "$machine_name" = 'iolbox-neg68' ] && machine_exists; then
        machine_created=1
    fi

    if [ "$machine_created" -eq 1 ] && [ "$keep" -eq 0 ]; then
        if [ "$machine_name" = 'iolbox-neg68' ]; then
            printf 'Deleting throwaway Lima machine: %s\n' "$machine_name" >&2
            "$limactl_bin" delete --force "$machine_name" || \
                printf 'WARNING: could not delete throwaway Lima machine: %s\n' "$machine_name" >&2
        else
            printf 'WARNING: refusing to delete unexpected machine name: %s\n' "$machine_name" >&2
        fi
    elif [ "$machine_created" -eq 1 ]; then
        printf 'Keeping throwaway Lima machine: %s\n' "$machine_name" >&2
    fi

    exit "$status"
}

print_dry_run() {
    printf '%s\n' 'DRY-RUN: no Lima commands will be executed.'
    printf 'DRY-RUN: requirement: at least %d KiB (3 GiB) free on the filesystem containing $HOME.\n' "$min_free_kib"
    printf '%s\n' 'df -Pk "$HOME"'
    printf '%s\n' "${limactl_bin} list --format '{{.Name}}'"
    printf '%s\n' "${limactl_bin} start --name=${machine_name} --vm-type=vz --rosetta --cpus=1 --memory=1 --disk=8 --mount-none --tty=false template://ubuntu-24.04"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo mkdir -p /opt/iolbox-provision"
    printf '%s\n' "${limactl_bin} shell ${machine_name} uname -r  # assert 6.8 before installing libc6:amd64"
    printf '%s\n' "${limactl_bin} copy '${guest_dir}/lib.sh' '${machine_name}:/tmp/iolbox-lib.sh'"
    printf '%s\n' "${limactl_bin} copy '${guest_dir}/10-multiarch.sh' '${machine_name}:/tmp/iolbox-10-multiarch.sh'"
    printf '%s\n' "${limactl_bin} copy '${guest_dir}/30-canary.sh' '${machine_name}:/tmp/iolbox-30-canary.sh'"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo mv /tmp/iolbox-lib.sh /opt/iolbox-provision/lib.sh"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo mv /tmp/iolbox-10-multiarch.sh /opt/iolbox-provision/10-multiarch.sh"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo mv /tmp/iolbox-30-canary.sh /opt/iolbox-provision/30-canary.sh"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo chmod 0755 /opt/iolbox-provision/10-multiarch.sh /opt/iolbox-provision/30-canary.sh"
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo -E env IOLBOX_PROVISION_DIR=/opt/iolbox-provision IOLBOX_MACHINE=${machine_name} IOLBOX_HOST_MACOS='<detected macOS product/build>' IOLBOX_HOST_LIMA='<detected Lima version>' bash -s <<'GUEST_MULTIARCH'"
    printf '%s\n' 'set -euo pipefail'
    printf '%s\n' 'SOURCES_LIST=/etc/apt/sources.list'
    printf '%s\n' 'SOURCES_LIST_DIR=/etc/apt/sources.list.d'
    printf '%s\n' 'AMD64_SOURCES_LIST="$SOURCES_LIST_DIR/iolbox-amd64.list"'
    printf '%s\n' 'for source_file in "$SOURCES_LIST_DIR"/*.sources; do [ -f "$source_file" ] || continue; mv -- "$source_file" "$source_file.iolbox-disabled"; done'
    printf '%s\n' "printf '%s\\n' 'deb http://ports.ubuntu.com/ubuntu-ports/ noble main restricted universe multiverse' 'deb http://ports.ubuntu.com/ubuntu-ports/ noble-updates main restricted universe multiverse' 'deb http://ports.ubuntu.com/ubuntu-ports/ noble-security main restricted universe multiverse' > \"\$SOURCES_LIST\""
    printf '%s\n' '. "$IOLBOX_PROVISION_DIR/10-multiarch.sh"'
    printf '%s\n' 'pin_existing_sources'
    printf '%s\n' 'write_amd64_sources noble'
    printf '%s\n' 'dpkg --add-architecture amd64'
    printf '%s\n' 'DEBIAN_FRONTEND=noninteractive apt-get update'
    printf '%s\n' 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libc6:amd64'
    printf '%s\n' 'assert_amd64_elf "$IOLBOX_LOADER"'
    printf '%s\n' 'GUEST_MULTIARCH'
    printf '%s\n' "${limactl_bin} shell ${machine_name} sudo -E env IOLBOX_PROVISION_DIR=/opt/iolbox-provision IOLBOX_MACHINE=${machine_name} IOLBOX_HOST_MACOS='<detected macOS product/build>' IOLBOX_HOST_LIMA='<detected Lima version>' bash /opt/iolbox-provision/30-canary.sh"
    printf '%s\n' "${limactl_bin} delete --force ${machine_name}  # cleanup unless --keep"
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --keep)
            keep=1
            ;;
        --dry-run)
            dry_run=1
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            usage >&2
            exit 1
            ;;
    esac
    shift
done

if [ "$dry_run" -eq 1 ]; then
    print_dry_run
    exit 0
fi

trap cleanup EXIT
trap 'exit 130' INT TERM

command -v "$limactl_bin" >/dev/null 2>&1 || fail "limactl not found: $limactl_bin"

free_kib="$(df -Pk "$HOME" | awk 'NR == 2 { print $4 }')"
case "$free_kib" in
    ''|*[!0-9]*)
        fail "could not determine free disk space from df -Pk \$HOME"
        ;;
esac
printf 'Preflight free disk: %d KiB (%.2f GiB); requirement: %d KiB (3 GiB).\n' \
    "$free_kib" "$((free_kib / 1024 / 1024))" "$min_free_kib"
if [ "$free_kib" -lt "$min_free_kib" ]; then
    fail "insufficient free disk: ${free_kib} KiB available, ${min_free_kib} KiB required"
fi

[ -f "$guest_dir/lib.sh" ] || fail "missing shared guest helper: $guest_dir/lib.sh"
[ -f "$guest_dir/10-multiarch.sh" ] || fail "missing agent B multiarch step: $guest_dir/10-multiarch.sh"
[ -f "$guest_dir/30-canary.sh" ] || fail "missing canary step: $guest_dir/30-canary.sh"

if machine_exists; then
    fail "refusing to use or delete existing Lima machine: $machine_name"
fi

host_macos="${IOLBOX_HOST_MACOS:-unknown}"
if [ "$host_macos" = 'unknown' ] && command -v sw_vers >/dev/null 2>&1; then
    host_macos="$(sw_vers -productVersion) ($(sw_vers -buildVersion))"
fi
host_lima="${IOLBOX_HOST_LIMA:-$($limactl_bin --version 2>/dev/null || printf 'unknown')}"

printf 'Creating throwaway Lima machine: %s\n' "$machine_name"
machine_create_attempted=1
if "$limactl_bin" start \
    --name="$machine_name" \
    --vm-type=vz \
    --rosetta \
    --cpus=1 \
    --memory=1 \
    --disk=8 \
    --mount-none \
    --tty=false \
    template://ubuntu-24.04; then
    machine_created=1
else
    if machine_exists; then
        machine_created=1
    fi
    fail "could not create $machine_name"
fi

"$limactl_bin" shell "$machine_name" sudo mkdir -p /opt/iolbox-provision
guest_kernel="$($limactl_bin shell "$machine_name" uname -r)"
printf 'Throwaway guest kernel: %s\n' "$guest_kernel"
case "$guest_kernel" in
    6.8.*) ;;
    *) fail "Ubuntu 24.04 template did not provide the required 6.8 kernel: $guest_kernel" ;;
esac
"$limactl_bin" copy "$guest_dir/lib.sh" "$machine_name:/tmp/iolbox-lib.sh"
"$limactl_bin" copy "$guest_dir/10-multiarch.sh" "$machine_name:/tmp/iolbox-10-multiarch.sh"
"$limactl_bin" copy "$guest_dir/30-canary.sh" "$machine_name:/tmp/iolbox-30-canary.sh"
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-lib.sh /opt/iolbox-provision/lib.sh
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-10-multiarch.sh /opt/iolbox-provision/10-multiarch.sh
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-30-canary.sh /opt/iolbox-provision/30-canary.sh
"$limactl_bin" shell "$machine_name" sudo chmod 0755 \
    /opt/iolbox-provision/10-multiarch.sh /opt/iolbox-provision/30-canary.sh

printf 'Running the shared amd64 source-policy helpers from 10-multiarch.sh; no payload is installed.\n'
"$limactl_bin" shell "$machine_name" sudo -E env \
    IOLBOX_PROVISION_DIR=/opt/iolbox-provision \
    IOLBOX_MACHINE="$machine_name" \
    IOLBOX_HOST_MACOS="$host_macos" \
    IOLBOX_HOST_LIMA="$host_lima" \
    bash -s <<'GUEST_MULTIARCH'
set -euo pipefail
SOURCES_LIST=/etc/apt/sources.list
SOURCES_LIST_DIR=/etc/apt/sources.list.d
AMD64_SOURCES_LIST="$SOURCES_LIST_DIR/iolbox-amd64.list"

# Noble cloud images use deb822 sources. Move those image-owned files aside
# in this disposable guest. Agent B's 10-multiarch.sh intentionally rejects
# Noble when run as its Jammy provisioner, so source it as the shared helper
# library and call its source/ELF functions directly; the amd64 source policy
# remains one implementation and is not duplicated in this test.
for source_file in "$SOURCES_LIST_DIR"/*.sources; do
    [ -f "$source_file" ] || continue
    mv -- "$source_file" "$source_file.iolbox-disabled"
done
printf '%s\n' \
    'deb http://ports.ubuntu.com/ubuntu-ports/ noble main restricted universe multiverse' \
    'deb http://ports.ubuntu.com/ubuntu-ports/ noble-updates main restricted universe multiverse' \
    'deb http://ports.ubuntu.com/ubuntu-ports/ noble-security main restricted universe multiverse' \
    > "$SOURCES_LIST"

. "$IOLBOX_PROVISION_DIR/10-multiarch.sh"
pin_existing_sources
write_amd64_sources noble
dpkg --add-architecture amd64
DEBIAN_FRONTEND=noninteractive apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libc6:amd64
assert_amd64_elf "$IOLBOX_LOADER"
GUEST_MULTIARCH

canary_output=''
canary_status=0
set +e
canary_output="$("$limactl_bin" shell "$machine_name" sudo -E env \
    IOLBOX_PROVISION_DIR=/opt/iolbox-provision \
    IOLBOX_MACHINE="$machine_name" \
    IOLBOX_HOST_MACOS="$host_macos" \
    IOLBOX_HOST_LIMA="$host_lima" \
    bash /opt/iolbox-provision/30-canary.sh 2>&1)"
canary_status=$?
set -e
printf '%s\n' "$canary_output"

if [ "$canary_status" -eq 0 ]; then
    printf 'UNEXPECTED-PASS: %s returned exit 0 on the 6.8-kernel guest; this Mac'\''s Rosetta handled AT_RSEQ_ALIGN, which is currently UNVERIFIED for every macOS version.\n' "$machine_name" >&2
    exit 1
fi
if [ "$canary_status" -ne 2 ]; then
    printf 'FAIL: expected canary exit status 2, got %d.\n' "$canary_status" >&2
    exit 1
fi
case "$canary_output" in
    *'FAIL_AUXV'*'AT_RSEQ_ALIGN'*'Ubuntu 22.04'*'kernel 5.15'*'UNVERIFIED'*)
        printf 'ok: 6.8-kernel guest rejected with actionable FAIL_AUXV canary.\n'
        ;;
    *)
        printf 'FAIL: exit 2 did not include the specific actionable auxv rejection.\n' >&2
        exit 1
        ;;
esac

exit 0
