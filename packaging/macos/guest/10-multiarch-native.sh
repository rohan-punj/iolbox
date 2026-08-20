#!/usr/bin/env bash
# 10-multiarch-native.sh — configure the native-arm64 guest's translator and
# amd64 runtime for the still-x86_64-only IOL device-image binary.
#
# "native-arm64" means the supervisor/vpcs/toollaunch userspace stack itself
# is a real arm64 build (see runtime/pack-native.sh --arch arm64 and
# docs/m7-phase4-file-mapping.md). It does NOT mean every executable in the
# guest is arm64: the owner's IOL device-image binary is x86_64-only, and
# Phase 3 (docs/macos-m7-phase3-execution-plan.md) measured and selected
# qemu-user as the sole correctness-eligible in-guest translator for that one
# binary. This step installs and registers that translator, plus the amd64
# runtime libraries qemu-user's binfmt handler needs to resolve the IOL
# binary's own dynamic dependencies — the same amd64 libc6/libssl set the
# Rosetta path installs via 10-multiarch-debian.sh, for the same reason.
#
# In order, this step:
#   1. proves the guest is Debian trixie (the native-arm64 profile's pinned
#      suite; see packaging/macos/lima/profiles.env);
#   2. installs qemu-user-static and binfmt-support, and confirms Debian's
#      packaging registered the qemu-x86_64 binfmt handler;
#   3. installs the same amd64 libc6/libssl runtime set 10-multiarch-debian.sh
#      installs, using the identical deb822/legacy source handling; and
#   4. asserts the foreign architecture, the qemu-x86_64 handler, and the
#      x86-64 loader/libc/libssl objects.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

DEBIAN_SOURCES_FILE="${DEBIAN_SOURCES_FILE:-/etc/apt/sources.list.d/debian.sources}"
LEGACY_SOURCES_FILE="${LEGACY_SOURCES_FILE:-/etc/apt/sources.list}"
IOLBOX_QEMU_X86_64_BINFMT="${IOLBOX_QEMU_X86_64_BINFMT:-/proc/sys/fs/binfmt_misc/qemu-x86_64}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert the native-arm64 translator/multiarch end state without
              changing files or invoking apt.
  -h, --help  show this help.
EOF
}

guest_codename() {
    local distro_id='' codename=''

    [ -r /etc/os-release ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        'Debian identity assertion: /etc/os-release is missing'
    # shellcheck disable=SC1091
    . /etc/os-release
    distro_id="${ID:-}"
    codename="${VERSION_CODENAME:-}"
    [ "$distro_id" = debian ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "Debian identity assertion: expected ID=debian, detected '${distro_id:-unknown}'"
    case "$codename" in
        trixie) printf '%s\n' "$codename" ;;
        *) die "$IOLBOX_EXIT_PREFLIGHT" \
            "Debian identity assertion: expected VERSION_CODENAME=trixie (native-arm64 pins the same image as debian13), detected '${codename:-unknown}'" ;;
    esac
}

source_layout() {
    if [ -f "$DEBIAN_SOURCES_FILE" ]; then
        printf 'deb822\n'
    elif [ -f "$LEGACY_SOURCES_FILE" ]; then
        printf 'legacy\n'
    else
        die "$IOLBOX_EXIT_PREFLIGHT" \
            "Debian apt source assertion: neither $DEBIAN_SOURCES_FILE nor $LEGACY_SOURCES_FILE exists"
    fi
}

deb822_needs_amd64() {
    local line arch_list restricted=0

    while IFS= read -r line || [ -n "$line" ]; do
        if [[ "$line" =~ ^[[:space:]]*Architectures:[[:space:]]*(.*)$ ]]; then
            restricted=1
            arch_list="${BASH_REMATCH[1]}"
            if [[ " $arch_list " != *' amd64 '* ]]; then
                return 0
            fi
        fi
    done < "$DEBIAN_SOURCES_FILE"
    [ "$restricted" -eq 1 ] && return 1
    return 1
}

enable_deb822_amd64() {
    local backup tmp line arch_list prefix changed=0

    if ! deb822_needs_amd64; then
        return 0
    fi
    backup="${DEBIAN_SOURCES_FILE}.iolbox-orig"
    if [ ! -e "$backup" ]; then
        cp -- "$DEBIAN_SOURCES_FILE" "$backup" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not back up Debian deb822 sources: $DEBIAN_SOURCES_FILE"
    fi
    tmp="$(mktemp "${DEBIAN_SOURCES_FILE}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create a Debian sources temporary file'
    while IFS= read -r line || [ -n "$line" ]; do
        if [[ "$line" =~ ^([[:space:]]*)Architectures:[[:space:]]*(.*)$ ]]; then
            prefix="${BASH_REMATCH[1]}"
            arch_list="${BASH_REMATCH[2]}"
            if [[ " $arch_list " != *' amd64 '* ]]; then
                printf '%sArchitectures: %s amd64\n' "$prefix" "$arch_list" >> "$tmp"
                changed=1
            else
                printf '%s\n' "$line" >> "$tmp"
            fi
        else
            printf '%s\n' "$line" >> "$tmp"
        fi
    done < "$DEBIAN_SOURCES_FILE"
    if [ "$changed" -eq 1 ]; then
        mv -f -- "$tmp" "$DEBIAN_SOURCES_FILE" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not install amended Debian sources: $DEBIAN_SOURCES_FILE"
    else
        rm -f -- "$tmp"
    fi
}

prepare_sources() {
    local layout

    layout="$(source_layout)"
    case "$layout" in
        deb822)
            enable_deb822_amd64
            log "using Debian deb822 sources: $DEBIAN_SOURCES_FILE; deb.debian.org serves both amd64 and arm64, so no mirror rewrite is needed"
            ;;
        legacy)
            log "using legacy Debian one-line sources: $LEGACY_SOURCES_FILE; no source rewriting is needed"
            ;;
    esac
}

verify_sources() {
    local layout line arch_list

    layout="$(source_layout)"
    if [ "$layout" = deb822 ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            if [[ "$line" =~ ^[[:space:]]*Architectures:[[:space:]]*(.*)$ ]]; then
                arch_list="${BASH_REMATCH[1]}"
                [[ " $arch_list " == *' amd64 '* ]] || die "$IOLBOX_EXIT_VERIFY" \
                    "source assertion: restrictive Architectures field omits amd64 in $DEBIAN_SOURCES_FILE"
            fi
        done < "$DEBIAN_SOURCES_FILE"
    fi
}

find_amd64_libc() {
    dpkg-query -L libc6:amd64 2>/dev/null | awk '/\/libc\.so\.6$/ { print; exit }'
}

find_amd64_libssl() {
    dpkg-query -L libssl3t64:amd64 2>/dev/null | awk '/\/libssl\.so\.[0-9]+$/ { print; exit }'
}

package_is_installed() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Architecture} ${Status}\n' "$package" 2>/dev/null || true)" = \
        "${package##*:} install ok installed" ]
}

package_is_installed_native() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Status}\n' "$package" 2>/dev/null || true)" = 'install ok installed' ]
}

qemu_x86_64_binfmt_state() {
    if [ -r "$IOLBOX_QEMU_X86_64_BINFMT" ]; then
        printf 'registered: %s' "$(tr '\n' ' ' < "$IOLBOX_QEMU_X86_64_BINFMT")"
    else
        printf 'absent: %s is unreadable or missing' "$IOLBOX_QEMU_X86_64_BINFMT"
    fi
}

verify_runtime() {
    local exit_code="$1" libc_path ssl_path foreign_architectures

    package_is_installed_native qemu-user-static || die "$exit_code" \
        'package assertion: qemu-user-static is not installed'
    package_is_installed_native binfmt-support || die "$exit_code" \
        'package assertion: binfmt-support is not installed'
    if [ ! -r "$IOLBOX_QEMU_X86_64_BINFMT" ] || ! grep -q '^enabled' "$IOLBOX_QEMU_X86_64_BINFMT"; then
        die "$exit_code" \
            "translator assertion: qemu-x86_64 binfmt handler is not registered/enabled ($(qemu_x86_64_binfmt_state))"
    fi

    package_is_installed libc6:amd64 || die "$exit_code" \
        'package assertion: libc6:amd64 is not installed'
    package_is_installed libssl3t64:amd64 || die "$exit_code" \
        'package assertion: libssl3t64:amd64 is not installed'
    foreign_architectures="$(dpkg --print-foreign-architectures 2>/dev/null || true)"
    if ! text_contains_exact_line "$foreign_architectures" amd64; then
        die "$exit_code" \
            'architecture assertion: dpkg --print-foreign-architectures does not contain amd64'
    fi
    libc_path="$(find_amd64_libc || true)"
    [ -n "$libc_path" ] || die "$exit_code" \
        'ELF assertion: dpkg has no libc6:amd64 libc.so.6 path'
    ssl_path="$(find_amd64_libssl || true)"
    [ -n "$ssl_path" ] || die "$exit_code" \
        'ELF assertion: dpkg has no libssl3t64:amd64 libssl.so path'
    assert_amd64_elf "$libc_path"
    assert_amd64_elf "$ssl_path"
}

run_provision() {
    local rc codename

    codename="$(guest_codename)"
    prepare_sources

    if dpkg --add-architecture amd64; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" "command failed (exit $rc): dpkg --add-architecture amd64"
    fi
    if DEBIAN_FRONTEND=noninteractive apt-get update; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): DEBIAN_FRONTEND=noninteractive apt-get update"
    fi
    if DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        qemu-user-static binfmt-support libc6:amd64 libssl3t64:amd64; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends qemu-user-static binfmt-support libc6:amd64 libssl3t64:amd64"
    fi
    # Debian's qemu-user-static postinst normally registers binfmt handlers
    # via update-binfmts automatically; make it explicit and idempotent so a
    # provisioning re-run cannot silently skip it.
    if have update-binfmts; then
        update-binfmts --enable qemu-x86_64 2>/dev/null || true
    fi
    verify_runtime "$IOLBOX_EXIT_APT"
    log "native-arm64 translator (qemu-user) and amd64 runtime are installed and verified: $codename"
}

verify_end_state() {
    verify_sources
    verify_runtime "$IOLBOX_EXIT_VERIFY"
}

main() {
    local verify=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --verify) verify=1; shift ;;
            -h|--help) usage; return 0 ;;
            *) usage >&2; die "$IOLBOX_EXIT_USAGE" "unknown option: $1" ;;
        esac
    done

    [ "$(id -u)" -eq 0 ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        '10-multiarch-native.sh must run as root'
    if [ "$verify" -eq 1 ]; then
        verify_end_state
    else
        run_provision
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
