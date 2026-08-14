#!/usr/bin/env bash
# 20-kernel-hold-debian.sh - enforce the Debian kernel policy for Rosetta.
#
# In order, this step:
#   1. asserts the running Debian kernel matches IOLBOX_KERNEL_SERIES;
#   2. holds only the Debian kernel packages which are actually installed;
#   3. gives Debian backports kernels a negative apt priority; and
#   4. records the same key=value policy format as the Ubuntu step, including
#      the selected profile and the Debian kernel's inferred/UNVERIFIED status.
#
# The realistic Debian escape route is bookworm-backports, which carries 6.12;
# that is a newer kernel family than the measured macOS 13.5 Rosetta canary
# permits.  This is a policy hold, not a claim that Debian 6.1 has been
# executed on Apple Silicon.  Debian 12 is a CANDIDATE, UNVERIFIED on hardware.
# --verify is read-only.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

IOLBOX_KERNEL_PREFS="${IOLBOX_KERNEL_PREFS:-/etc/apt/preferences.d/99-iolbox-kernel-hold}"
IOLBOX_PROVISION_DATE="${IOLBOX_PROVISION_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
IOLBOX_PROFILE="${IOLBOX_PROFILE:-debian12}"
IOLBOX_PROFILE_STATUS="${IOLBOX_PROFILE_STATUS:-CANDIDATE, UNVERIFIED}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert Debian kernel holds, policy, preferences, and running
              kernel without changing anything.
  -h, --help  show this help.
EOF
}

debian_codename() {
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

assert_qualified_kernel() {
    local actual expected

    actual="$(kernel_series)"
    expected="$(printf '%s\n' "$IOLBOX_KERNEL_SERIES" | cut -d. -f1,2)"
    [ "$actual" = "$expected" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "guest kernel series '$actual' is outside qualified '$IOLBOX_KERNEL_SERIES'; Linux >= 6.3 emits auxv type 28 (AT_RSEQ_ALIGN), which the measured macOS/Rosetta pair aborts on"
}

package_is_installed() {
    local package="$1"
    [ "$(dpkg-query -W -f='${Status}\n' "$package" 2>/dev/null || true)" = \
        'install ok installed' ]
}

installed_kernel_packages() {
    local package
    local -a candidates=(
        linux-image-arm64
        linux-image-cloud-arm64
        linux-headers-arm64
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
    local rc package
    local -a packages=()

    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(get_kernel_packages)
    if [ "${#packages[@]}" -eq 0 ]; then
        log 'no supported Debian kernel package is installed; nothing to hold'
        return 0
    fi
    if apt-mark hold "${packages[@]}"; then
        log "held installed Debian kernel packages: ${packages[*]}"
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
    local codename="$1" tmp

    install -d -m 0755 -- "$(dirname "$IOLBOX_KERNEL_PREFS")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create apt preferences directory'
    tmp="$(mktemp "${IOLBOX_KERNEL_PREFS}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create kernel preferences temporary file'
    printf '%s\n' \
        "# iolbox Debian kernel policy for $codename; profile=$IOLBOX_PROFILE." \
        '#' \
        '# Debian bookworm-backports carries a 6.12 kernel.  Pulling a backports' \
        '# kernel is the realistic Debian escape route, but it crosses the' \
        '# measured macOS 13.5 Rosetta boundary unless the exact pair passes the' \
        '# executable canary.  Keep backports kernels below apt install priority.' \
        '#' \
        '# This negative pin complements apt-mark holds on packages installed now.' \
        'Package: linux-image* linux-headers*' \
        "Pin: release n=${codename}-backports" \
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
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create policy directory'
    tmp="$(mktemp "${IOLBOX_POLICY_FILE}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create policy temporary file'
    printf '%s\n' \
        'iolbox macOS/Lima guest kernel policy' \
        "profile=$IOLBOX_PROFILE" \
        "profile_status=$IOLBOX_PROFILE_STATUS" \
        "provisioned_at=$IOLBOX_PROVISION_DATE" \
        "host_macos=$IOLBOX_HOST_MACOS" \
        "host_lima=$IOLBOX_HOST_LIMA" \
        "machine=$IOLBOX_MACHINE" \
        "qualified_kernel_series=$IOLBOX_KERNEL_SERIES" \
        'kernel_safety=inferred safe and UNVERIFIED on hardware (Debian kernel/Rosetta pair has never been executed on Apple Silicon)' \
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
            die "$IOLBOX_EXIT_VERIFY" \
                "hold assertion: installed Debian kernel package is not held: $package"
        fi
    done < <(get_kernel_packages)
}

verify_end_state() {
    local codename

    codename="$(debian_codename)"
    assert_qualified_kernel
    [ -f "$IOLBOX_KERNEL_PREFS" ] || die "$IOLBOX_EXIT_VERIFY" \
        "preferences assertion: missing $IOLBOX_KERNEL_PREFS"
    grep -Fqx "Pin: release n=${codename}-backports" "$IOLBOX_KERNEL_PREFS" || \
        die "$IOLBOX_EXIT_VERIFY" \
        "preferences assertion: ${codename}-backports kernel pin is missing"
    grep -Fqx 'Pin-Priority: -1' "$IOLBOX_KERNEL_PREFS" || \
        die "$IOLBOX_EXIT_VERIFY" 'preferences assertion: negative backports pin is missing'
    [ -f "$IOLBOX_POLICY_FILE" ] || die "$IOLBOX_EXIT_VERIFY" \
        "policy assertion: missing $IOLBOX_POLICY_FILE"
    grep -Fqx "profile=$IOLBOX_PROFILE" "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" "policy assertion: profile=$IOLBOX_PROFILE is missing"
    grep -Fq 'kernel_safety=inferred safe and UNVERIFIED on hardware' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" \
        'policy assertion: Debian inferred/UNVERIFIED kernel safety record is missing'
    assert_holds
}

run_provision() {
    local codename

    codename="$(debian_codename)"
    assert_qualified_kernel
    hold_installed_kernels
    write_kernel_preferences "$codename"
    write_policy_file
    log "Debian kernel policy recorded at $IOLBOX_POLICY_FILE"
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
        '20-kernel-hold-debian.sh must run as root'
    if [ "$verify" -eq 1 ]; then
        verify_end_state
    else
        run_provision
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
