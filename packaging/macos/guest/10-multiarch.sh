#!/usr/bin/env bash
# 10-multiarch.sh — configure Jammy multiarch for the translated payload.
#
# In order, this step:
#   1. proves this is the Jammy guest this provisioner qualifies;
#   2. backs up and pins existing one-line Ubuntu apt sources to exactly
#      arch=arm64, replacing any pre-existing architecture restriction;
#   3. adds amd64-only Ubuntu archive/security sources;
#   4. enables dpkg's amd64 foreign architecture and installs the measured
#      libc6/libssl3 runtime set; and
#   5. asserts that the files which landed are really x86-64 ELF objects.
#
# The source-list rewrite is deliberately a shell function over a caller-
# supplied SOURCES_LIST path so it can be exercised against a fixture without
# touching a real guest.  --verify performs only assertions and writes nothing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

SOURCES_LIST="${SOURCES_LIST:-/etc/apt/sources.list}"
SOURCES_LIST_DIR="${SOURCES_LIST_DIR:-/etc/apt/sources.list.d}"
AMD64_SOURCES_LIST="${AMD64_SOURCES_LIST:-$SOURCES_LIST_DIR/iolbox-amd64.list}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert the Jammy multiarch end state without changing files or
              invoking apt.
  -h, --help  show this help.
EOF
}

# pin_sources_text < input > output
#
# Every active pre-existing Ubuntu one-line source must be arm64-only. When an
# option block already contains arch= (including arch=amd64 or
# arch=arm64,amd64), replace only that token and preserve unrelated options
# such as signed-by.
pin_sources_text() {
    local line option_text prefix suffix normalised_options

    while IFS= read -r line || [ -n "$line" ]; do
        if [[ "$line" =~ ^([[:space:]]*deb(-src)?[[:space:]]+)\[([^]]*)\](.*)$ ]]; then
            # Save these before the nested [[ =~ ]] check changes
            # BASH_REMATCH.
            prefix="${BASH_REMATCH[1]}"
            option_text="${BASH_REMATCH[3]}"
            suffix="${BASH_REMATCH[4]}"
            if [[ "$option_text" =~ (^|[[:space:]])arch[[:space:]]*= ]]; then
                normalised_options="$(printf '%s\n' "$option_text" | sed -E 's/(^|[[:space:]])arch[[:space:]]*=[[:space:]]*[^[:space:]]+/\1arch=arm64/g')"
            else
                normalised_options="arch=arm64 $option_text"
            fi
            printf '%s[%s]%s\n' "$prefix" "$normalised_options" "$suffix"
        elif [[ "$line" =~ ^([[:space:]]*deb(-src)?[[:space:]]+)(.*)$ ]]; then
            printf '%s[arch=arm64] %s\n' \
                "${BASH_REMATCH[1]}" "${BASH_REMATCH[3]}"
        else
            printf '%s\n' "$line"
        fi
    done
}

reject_unsupported_deb822_sources() {
    local path line active
    local -a deb822_files=()

    [ -d "$SOURCES_LIST_DIR" ] || return 0
    shopt -s nullglob
    deb822_files=("$SOURCES_LIST_DIR"/*.sources)
    shopt -u nullglob
    for path in ${deb822_files[@]+"${deb822_files[@]}"}; do
        active=0
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                ''|[[:space:]]*'#'*) continue ;;
                'Enabled:'[[:space:]]*'no') continue ;;
                *) active=1; break ;;
            esac
        done < "$path"
        if [ "$active" -eq 1 ]; then
            die "$IOLBOX_EXIT_PREFLIGHT" \
                "Ubuntu deb822 source layout is not safely normalisable: $path; convert it to one-line sources or remove it before provisioning"
        fi
    done
}

pin_sources_file() {
    local path="$1" backup tmp

    [ -f "$path" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "apt source file is missing: $path"

    backup="${path}.iolbox-orig"
    # Never overwrite the first snapshot: it is the operator's escape hatch
    # back to the cloud-init state that existed before iolbox provisioned it.
    if [ ! -e "$backup" ]; then
        cp -- "$path" "$backup" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not back up apt source file: $path"
    fi

    tmp="$(mktemp "${path}.iolbox-tmp.XXXXXX")" || die "$IOLBOX_EXIT_PREFLIGHT" \
        "could not create a temporary apt source file beside: $path"
    if ! pin_sources_text < "$path" > "$tmp"; then
        rm -f -- "$tmp"
        die "$IOLBOX_EXIT_PREFLIGHT" "could not rewrite apt source file: $path"
    fi

    if cmp -s -- "$path" "$tmp"; then
        rm -f -- "$tmp"
    else
        mv -f -- "$tmp" "$path" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not install rewritten apt source file: $path"
    fi
}

pin_existing_sources() {
    local path
    local -a source_files=()

    if ! reject_unsupported_deb822_sources; then
        return 1
    fi
    pin_sources_file "$SOURCES_LIST"
    if [ -d "$SOURCES_LIST_DIR" ]; then
        shopt -s nullglob
        source_files=("$SOURCES_LIST_DIR"/*.list)
        shopt -u nullglob
        for path in ${source_files[@]+"${source_files[@]}"}; do
            # This file is generated below, not cloud-init input.  Skipping it
            # avoids creating a needless backup of our own managed output on
            # the second run.
            [ "$path" = "$AMD64_SOURCES_LIST" ] && continue
            pin_sources_file "$path"
        done
    fi
}

source_arch_value() {
    local line="$1" option_text arch_value

    if [[ "$line" =~ ^[[:space:]]*deb(-src)?[[:space:]]+\[([^]]*)\] ]]; then
        option_text="${BASH_REMATCH[2]}"
        if [[ "$option_text" =~ (^|[[:space:]])arch[[:space:]]*=[[:space:]]*([^[:space:]]+) ]]; then
            arch_value="${BASH_REMATCH[2]}"
            printf '%s\n' "$arch_value"
            return 0
        fi
    fi
    return 1
}

source_line_has_amd64() {
    local line="$1" arch_value token

    arch_value="$(source_arch_value "$line" || true)"
    arch_value="${arch_value//,/ }"
    for token in $arch_value; do
        [ "$token" = amd64 ] && return 0
    done
    return 1
}

source_line_is_arm64_only() {
    local line="$1" arch_value

    arch_value="$(source_arch_value "$line" || true)"
    [ "$arch_value" = arm64 ]
}

source_line_has_arch() {
    local line="$1"

    source_arch_value "$line" >/dev/null
}

verify_sources_file() {
    local path="$1" line

    [ -f "$path" ] || die "$IOLBOX_EXIT_VERIFY" \
        "source assertion: expected file is missing: $path"
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            ''|[[:space:]]*'#'*) continue ;;
        esac
        if [[ "$line" =~ ^[[:space:]]*deb(-src)?[[:space:]]+ ]] && \
            [[ "$line" == *ports.ubuntu.com* ]] && source_line_has_amd64 "$line"; then
            die "$IOLBOX_EXIT_VERIFY" \
                "source assertion: amd64-enabled Ubuntu ports.ubuntu.com entry is forbidden in $path: $line"
        fi
        if [[ "$line" =~ ^[[:space:]]*deb(-src)?[[:space:]]+ ]] && \
            ! source_line_is_arm64_only "$line"; then
            die "$IOLBOX_EXIT_VERIFY" \
                "source assertion: active Ubuntu one-line entry is not exactly arch=arm64 in $path: $line"
        fi
    done < "$path"
}

expected_amd64_sources() {
    local suite="$1"

    printf '# Managed by iolbox 10-multiarch.sh; amd64 is intentionally not sourced from ports.ubuntu.com.\n'
    printf 'deb [arch=amd64] http://archive.ubuntu.com/ubuntu/ %s main restricted universe multiverse\n' "$suite"
    printf 'deb [arch=amd64] http://archive.ubuntu.com/ubuntu/ %s-updates main restricted universe multiverse\n' "$suite"
    printf 'deb [arch=amd64] http://security.ubuntu.com/ubuntu/ %s-security main restricted universe multiverse\n' "$suite"
}

write_amd64_sources() {
    local suite="$1" tmp

    install -d -m 0755 -- "$SOURCES_LIST_DIR" || die "$IOLBOX_EXIT_PREFLIGHT" \
        "could not create apt source directory: $SOURCES_LIST_DIR"
    tmp="$(mktemp "${AMD64_SOURCES_LIST}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" "could not create amd64 source temporary file"
    expected_amd64_sources "$suite" > "$tmp"
    chmod 0644 "$tmp" || die "$IOLBOX_EXIT_PREFLIGHT" \
        "could not set permissions on amd64 source temporary file"
    if cmp -s -- "$AMD64_SOURCES_LIST" "$tmp" 2>/dev/null; then
        rm -f -- "$tmp"
    else
        mv -f -- "$tmp" "$AMD64_SOURCES_LIST" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not install amd64 apt source file: $AMD64_SOURCES_LIST"
    fi
}

guest_suite() {
    local suite=""

    if have lsb_release; then
        suite="$(lsb_release -cs 2>/dev/null || true)"
    fi
    if [ -z "$suite" ] && [ -r /etc/os-release ]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        suite="${VERSION_CODENAME:-${UBUNTU_CODENAME:-}}"
    fi
    printf '%s\n' "$suite"
}

find_amd64_libc() {
    dpkg-query -L libc6:amd64 2>/dev/null | awk '/\/libc\.so\.6$/ { print; exit }'
}

package_is_installed() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Architecture} ${Status}\n' "$package" 2>/dev/null || true)" = \
        "${package##*:} install ok installed" ]
}

verify_package_set() {
    local package libc_path

    for package in libc6:amd64 libssl3:amd64; do
        package_is_installed "$package" || die "$IOLBOX_EXIT_VERIFY" \
            "package assertion: $package is not installed with architecture amd64"
    done
    libc_path="$(find_amd64_libc || true)"
    [ -n "$libc_path" ] || die "$IOLBOX_EXIT_VERIFY" \
        "ELF assertion: dpkg has no libc6:amd64 libc.so.6 path"
    [ "$(elf_machine "$IOLBOX_LOADER" || true)" = "3e" ] || die "$IOLBOX_EXIT_VERIFY" \
        "ELF assertion: $IOLBOX_LOADER is not an x86-64 ELF"
    [ "$(elf_machine "$libc_path" || true)" = "3e" ] || die "$IOLBOX_EXIT_VERIFY" \
        "ELF assertion: $libc_path is not an x86-64 ELF"
}

verify_end_state() {
    local suite path foreign_architectures
    suite="$(guest_suite)"
    [ "$suite" = "jammy" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "qualified guest assertion: expected Jammy (suite jammy), detected '${suite:-unknown}'"

    reject_unsupported_deb822_sources
    verify_sources_file "$SOURCES_LIST"
    if [ -d "$SOURCES_LIST_DIR" ]; then
        while IFS= read -r path; do
            [ "$path" = "$AMD64_SOURCES_LIST" ] && continue
            verify_sources_file "$path"
        done < <(find "$SOURCES_LIST_DIR" -maxdepth 1 -type f -name '*.list' -print | sort)
    fi

    [ -f "$AMD64_SOURCES_LIST" ] || die "$IOLBOX_EXIT_VERIFY" \
        "source assertion: amd64 source file is missing: $AMD64_SOURCES_LIST"
    if ! cmp -s <(expected_amd64_sources "$suite") "$AMD64_SOURCES_LIST"; then
        die "$IOLBOX_EXIT_VERIFY" \
            "source assertion: $AMD64_SOURCES_LIST does not match the Jammy amd64 policy"
    fi
    foreign_architectures="$(dpkg --print-foreign-architectures 2>/dev/null || true)"
    if ! text_contains_exact_line "$foreign_architectures" amd64; then
        die "$IOLBOX_EXIT_VERIFY" \
            "architecture assertion: dpkg --print-foreign-architectures does not contain amd64"
    fi
    verify_package_set
}

run_provision() {
    local suite libc_path rc foreign_architectures

    suite="$(guest_suite)"
    [ "$suite" = "jammy" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "this provisioner is qualified for Jammy only; detected suite '${suite:-unknown}'"
    pin_existing_sources
    write_amd64_sources "$suite"

    # libc6:amd64 supplies the translated glibc loader and C runtime.  The
    # supervisor is Go, so no amd64 Go package is needed; vpcs is C and is
    # covered by libc6.  libssl3:amd64 is the measured M0 runtime set and is
    # retained for the native payload's OpenSSL-linked code paths.
    if dpkg --add-architecture amd64; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): dpkg --add-architecture amd64"
    fi
    if DEBIAN_FRONTEND=noninteractive apt-get update; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): DEBIAN_FRONTEND=noninteractive apt-get update"
    fi
    if DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        libc6:amd64 libssl3:amd64; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libc6:amd64 libssl3:amd64"
    fi

    foreign_architectures="$(dpkg --print-foreign-architectures 2>/dev/null || true)"
    if ! text_contains_exact_line "$foreign_architectures" amd64; then
        die "$IOLBOX_EXIT_APT" \
            "architecture assertion: dpkg did not retain foreign architecture amd64"
    fi
    assert_amd64_elf "$IOLBOX_LOADER"
    libc_path="$(find_amd64_libc || true)"
    [ -n "$libc_path" ] || die "$IOLBOX_EXIT_APT" \
        "ELF assertion: dpkg installed libc6:amd64 but no libc.so.6 path was found"
    assert_amd64_elf "$libc_path"
    log "amd64 multiarch runtime is installed and ELF-verified: libc=$libc_path"
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
        "10-multiarch.sh must run as root"
    if [ "$verify" -eq 1 ]; then
        verify_end_state
    else
        run_provision
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
