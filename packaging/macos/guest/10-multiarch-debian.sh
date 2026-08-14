#!/usr/bin/env bash
# 10-multiarch-debian.sh - configure Debian multiarch for the translated payload.
#
# In order, this step:
#   1. proves the guest is Debian bookworm or trixie;
#   2. detects Debian's current deb822 source layout or a legacy sources.list;
#   3. adds amd64 to a restrictive deb822 Architectures field when necessary;
#   4. enables dpkg amd64 and installs the suite-selected libc6/OpenSSL set;
#   5. asserts the foreign architecture and x86-64 loader/libc/libssl objects.
#
# Debian differs from Ubuntu here: deb.debian.org serves amd64 and arm64 from
# the same host, so no Ubuntu-style ports.ubuntu.com source surgery is needed.
# Keep that difference explicit; adding Ubuntu's archive/security rewrite here
# would make the Debian source policy less correct, not more portable.
#
# Current Debian cloud images use /etc/apt/sources.list.d/debian.sources
# (deb822). Older images may use a one-line /etc/apt/sources.list.  We detect
# which exists.  Only a deb822 Architectures restriction needs an idempotent,
# one-time .iolbox-orig backup.
# Debian 12 remains a CANDIDATE and unqualified while its image is unpinned.
# Debian's single deb.debian.org host serves amd64 and arm64; do not import
# Ubuntu's ports/archive mirror surgery here.  The only source exception is
# adding amd64 to every existing restrictive deb822 Architectures field.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

DEBIAN_SOURCES_FILE="${DEBIAN_SOURCES_FILE:-/etc/apt/sources.list.d/debian.sources}"
LEGACY_SOURCES_FILE="${LEGACY_SOURCES_FILE:-/etc/apt/sources.list}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert the Debian multiarch end state without changing files or
              invoking apt.
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
        bookworm|trixie) printf '%s\n' "$codename" ;;
        *) die "$IOLBOX_EXIT_PREFLIGHT" \
            "Debian identity assertion: expected VERSION_CODENAME=bookworm or trixie, detected '${codename:-unknown}'" ;;
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
            # Unlike Ubuntu's ports archive, Debian's deb.debian.org host
            # publishes both architectures.  A legacy one-line file therefore
            # needs no [arch=arm64] pinning or extra amd64 source file.
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

runtime_packages() {
    local codename="$1"

    printf '%s\n' libc6:amd64
    case "$codename" in
        bookworm) printf '%s\n' libssl3:amd64 ;;
        trixie) printf '%s\n' libssl3t64:amd64 ;;
        *) die "$IOLBOX_EXIT_PREFLIGHT" "unsupported Debian suite for runtime package selection: $codename" ;;
    esac
}

find_amd64_libssl() {
    local package="$1"

    dpkg-query -L "$package" 2>/dev/null | awk '/\/libssl\.so\.[0-9]+$/ { print; exit }'
}

package_is_installed() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Architecture} ${Status}\n' "$package" 2>/dev/null || true)" = \
        "${package##*:} install ok installed" ]
}

verify_runtime() {
    local codename="$1" exit_code="$2" package libc_path ssl_package ssl_path
    local foreign_architectures

    while IFS= read -r package; do
        package_is_installed "$package" || die "$exit_code" \
            "package assertion: $package is not installed with architecture amd64"
    done < <(runtime_packages "$codename")
    foreign_architectures="$(dpkg --print-foreign-architectures 2>/dev/null || true)"
    if ! text_contains_exact_line "$foreign_architectures" amd64; then
        die "$exit_code" \
            'architecture assertion: dpkg --print-foreign-architectures does not contain amd64'
    fi
    libc_path="$(find_amd64_libc || true)"
    [ -n "$libc_path" ] || die "$exit_code" \
        'ELF assertion: dpkg has no libc6:amd64 libc.so.6 path'
    ssl_package="$(runtime_packages "$codename" | tail -n1)"
    ssl_path="$(find_amd64_libssl "$ssl_package" || true)"
    [ -n "$ssl_path" ] || die "$exit_code" \
        "ELF assertion: dpkg has no selected amd64 libssl object for Debian $codename"
    assert_amd64_elf "$IOLBOX_LOADER"
    assert_amd64_elf "$libc_path"
    assert_amd64_elf "$ssl_path"
}

run_provision() {
    local rc codename package
    local -a packages=()

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
    # Install and verify the same suite-selected package list.  Bookworm uses
    # libssl3:amd64; Debian 13 trixie renamed it libssl3t64:amd64 for the
    # 64-bit time_t transition.  deb.debian.org serves both architectures, so
    # no mirror rewrite is needed.
    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(runtime_packages "$codename")
    if DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ${packages[@]+"${packages[@]}"}; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${packages[*]}"
    fi
    verify_runtime "$codename" "$IOLBOX_EXIT_APT"
    log "Debian $codename amd64 multiarch runtime is installed and ELF-verified"
}

verify_end_state() {
    local codename

    codename="$(guest_codename)"
    verify_sources
    verify_runtime "$codename" "$IOLBOX_EXIT_VERIFY"
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
        '10-multiarch-debian.sh must run as root'
    if [ "$verify" -eq 1 ]; then
        verify_end_state
    else
        run_provision
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
