#!/usr/bin/env bash
# 20-kernel-hold.sh — enforce the kernel policy required by macOS Rosetta.
#
# In order, this step:
#   1. rejects a running kernel outside the qualified 5.15 series;
#   2. holds only installed Jammy kernel packages;
#   3. pins HWE kernel packages below apt's install threshold; and
#   4. records the complete, inspectable policy with host/Lima provenance.
#
# Linux >= 6.3 emits auxv type 28 (AT_RSEQ_ALIGN). The Rosetta build measured
# on macOS 13.5 aborts on that auxv entry, so the macOS version which fixes it
# remains UNVERIFIED and this guest stays on 5.15. --verify is read-only.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

IOLBOX_KERNEL_PREFS="${IOLBOX_KERNEL_PREFS:-/etc/apt/preferences.d/99-iolbox-kernel-hold}"
IOLBOX_PROVISION_DATE="${IOLBOX_PROVISION_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert the kernel holds, policy file, preferences, and running
              kernel without changing anything.
  -h, --help  show this help.
EOF
}

running_kernel_series() {
    kernel_series
}

assert_qualified_kernel() {
    local series
    series="$(running_kernel_series)"
    [ "$series" = "$IOLBOX_KERNEL_SERIES" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "guest kernel series '$series' is outside qualified '$IOLBOX_KERNEL_SERIES'; Linux >= 6.3 emits auxv type 28 (AT_RSEQ_ALIGN), which the measured macOS/Rosetta pair aborts on"
}

package_is_installed() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Status}\n' "$package" 2>/dev/null || true)" = \
        'install ok installed' ]
}

installed_kernel_packages() {
    local package
    local -a candidates=(
        linux-generic
        linux-image-generic
        linux-headers-generic
        linux-image-virtual
        "linux-image-$(uname -r)"
    )

    for package in "${candidates[@]}"; do
        if package_is_installed "$package"; then
            printf '%s\n' "$package"
        fi
    done
}

get_kernel_packages() {
    local package
    local -a packages=()

    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(installed_kernel_packages)
    printf '%s\n' "${packages[@]}"
}

hold_installed_kernels() {
    local rc
    local -a packages=()

    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(get_kernel_packages)

    if [ "${#packages[@]}" -eq 0 ]; then
        log "no supported kernel package is installed; nothing to hold"
        return 0
    fi
    if apt-mark hold "${packages[@]}"; then
        log "held installed kernel packages: ${packages[*]}"
    else
        rc=$?
        die "$IOLBOX_EXIT_PREFLIGHT" \
            "command failed (exit $rc): apt-mark hold ${packages[*]}"
    fi
}

write_if_changed() {
    local destination="$1" mode="$2" tmp="$3"

    chmod "$mode" -- "$tmp" || die "$IOLBOX_EXIT_PREFLIGHT" \
        "could not set permissions on temporary file for $destination"
    if cmp -s -- "$destination" "$tmp" 2>/dev/null; then
        rm -f -- "$tmp"
    else
        mv -f -- "$tmp" "$destination" || die "$IOLBOX_EXIT_PREFLIGHT" \
            "could not install generated file: $destination"
    fi
}

write_kernel_preferences() {
    local tmp

    install -d -m 0755 -- "$(dirname "$IOLBOX_KERNEL_PREFS")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" "could not create apt preferences directory"
    tmp="$(mktemp "${IOLBOX_KERNEL_PREFS}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" "could not create kernel preferences temporary file"
    printf '%s\n' \
        '# iolbox kernel policy: keep the guest on the qualified Jammy 5.15 series.' \
        '#' \
        '# Linux >= 6.3 emits auxv type 28 (AT_RSEQ_ALIGN). The Rosetta build' \
        '# qualified with macOS 13.5 aborts when it sees that entry, so an HWE' \
        '# upgrade to a 6.x kernel would make every amd64 payload executable' \
        '# fail before main(). The macOS version that fixes this is UNVERIFIED.' \
        '#' \
        '# This negative pin complements apt-mark holds on packages installed now.' \
        'Package: linux-*-hwe-22.04*' \
        'Pin: release *' \
        'Pin-Priority: -1' > "$tmp"
    write_if_changed "$IOLBOX_KERNEL_PREFS" 0644 "$tmp"
}

write_policy_file() {
    local tmp held_list package
    local -a packages=()

    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(get_kernel_packages)
    held_list='(none)'
    if [ "${#packages[@]}" -gt 0 ]; then
        held_list="${packages[*]}"
    fi

    install -d -m 0755 -- "$(dirname "$IOLBOX_POLICY_FILE")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" "could not create policy directory"
    tmp="$(mktemp "${IOLBOX_POLICY_FILE}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" "could not create policy temporary file"
    printf '%s\n' \
        'iolbox macOS/Lima guest kernel policy' \
        "provisioned_at=$IOLBOX_PROVISION_DATE" \
        "host_macos=$IOLBOX_HOST_MACOS" \
        "host_lima=$IOLBOX_HOST_LIMA" \
        "machine=$IOLBOX_MACHINE" \
        "qualified_kernel_series=$IOLBOX_KERNEL_SERIES" \
        'why=macOS Rosetta aborts on auxv type 28 (AT_RSEQ_ALIGN), emitted by Linux kernels >= 6.3; the macOS version that fixes this is UNVERIFIED' \
        "held_kernel_packages=$held_list" \
        'check_holds=apt-mark showhold' \
        'intentional_lift=qualify the exact macOS/Rosetta and guest-kernel pair first, then run apt-mark unhold <package-list>, remove /etc/apt/preferences.d/99-iolbox-kernel-hold, and reboot deliberately' \
        > "$tmp"
    write_if_changed "$IOLBOX_POLICY_FILE" 0644 "$tmp"
}

assert_holds() {
    local holds package rc

    if holds="$(apt-mark showhold 2>/dev/null)"; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_VERIFY" "hold assertion: apt-mark showhold failed (exit $rc)"
    fi
    while IFS= read -r package; do
        [ -n "$package" ] || continue
        if ! printf '%s\n' "$holds" | grep -Fxq -- "$package"; then
            die "$IOLBOX_EXIT_VERIFY" "hold assertion: installed kernel package is not held: $package"
        fi
    done < <(get_kernel_packages)
}

verify_end_state() {
    assert_qualified_kernel
    [ -f "$IOLBOX_KERNEL_PREFS" ] || die "$IOLBOX_EXIT_VERIFY" \
        "preferences assertion: missing $IOLBOX_KERNEL_PREFS"
    grep -Fqx 'Package: linux-*-hwe-22.04*' "$IOLBOX_KERNEL_PREFS" || \
        die "$IOLBOX_EXIT_VERIFY" "preferences assertion: HWE package pattern is missing"
    grep -Fqx 'Pin-Priority: -1' "$IOLBOX_KERNEL_PREFS" || \
        die "$IOLBOX_EXIT_VERIFY" "preferences assertion: negative HWE pin is missing"
    [ -f "$IOLBOX_POLICY_FILE" ] || die "$IOLBOX_EXIT_VERIFY" \
        "policy assertion: missing $IOLBOX_POLICY_FILE"
    assert_holds
}

run_provision() {
    assert_qualified_kernel
    hold_installed_kernels
    write_kernel_preferences
    write_policy_file
    log "kernel policy recorded at $IOLBOX_POLICY_FILE"
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
        "20-kernel-hold.sh must run as root"
    if [ "$verify" -eq 1 ]; then
        verify_end_state
    else
        run_provision
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
