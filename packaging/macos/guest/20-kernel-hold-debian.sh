#!/usr/bin/env bash
# 20-kernel-hold-debian.sh - enforce the Debian kernel reproducibility policy.
#
# In order, this step:
#   1. asserts the running Debian kernel matches the profile's series and,
#      when supplied, exact uname -r;
#   2. holds only the Debian kernel packages which are actually installed;
#   3. gives Debian backports kernels a negative apt priority; and
#   4. records the same key=value reproducibility policy format as the Ubuntu
#      step, including profile/image/kernel qualification and provenance.
#
# This is not a Rosetta safety claim. The canary is the authority for the exact
# host/kernel pair. Debian 12 remains a candidate while its image is unpinned;
# trixie preserves the exact kernel supplied by its pinned image.
# --verify is read-only.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

IOLBOX_KERNEL_PREFS="${IOLBOX_KERNEL_PREFS:-/etc/apt/preferences.d/99-iolbox-kernel-hold}"
IOLBOX_PROVISION_DATE="${IOLBOX_PROVISION_DATE:-}"
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
    local codename

    codename="$(debian_codename)"
    assert_kernel_qualification "$IOLBOX_EXIT_PREFLIGHT" \
        "$(iolbox_expected_uname_r "$codename")"
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

    for package in ${candidates[@]+"${candidates[@]}"}; do
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
    printf '%s\n' ${packages[@]+"${packages[@]}"}
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
    if apt-mark hold ${packages[@]+"${packages[@]}"}; then
        log "held installed Debian kernel packages: ${packages[*]}"
    else
        rc=$?
        die "$IOLBOX_EXIT_PREFLIGHT" \
            "command failed (exit $rc): apt-mark hold ${packages[*]}"
    fi
}

write_if_changed() {
    local destination="$1" mode="$2" tmp="$3"

    chmod "$mode" "$tmp" || die "$IOLBOX_EXIT_PREFLIGHT" \
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
        '# This is a reproducibility hold; the executable canary is the authority' \
        '# for the exact host/kernel pair and no kernel series is universally' \
        '# declared safe or unsafe by this policy.' \
        '#' \
        '# This negative pin complements apt-mark holds on packages installed now.' \
        'Package: linux-image* linux-headers*' \
        "Pin: release n=${codename}-backports" \
        'Pin-Priority: -1' > "$tmp"
    write_if_changed "$IOLBOX_KERNEL_PREFS" 0644 "$tmp"
}

write_policy_file() {
    local tmp held_list package provision_date codename
    local -a packages=()

    while IFS= read -r package; do
        [ -n "$package" ] && packages+=("$package")
    done < <(get_kernel_packages)
    held_list='(none)'
    if [ "${#packages[@]}" -gt 0 ]; then
        held_list="${packages[*]}"
    fi
    codename="$(debian_codename)"
    provision_date=''
    if [ -f "$IOLBOX_POLICY_FILE" ]; then
        provision_date="$(sed -n 's/^provisioned_at=//p' "$IOLBOX_POLICY_FILE" | head -n1)"
    fi
    [ -n "$provision_date" ] || provision_date="$IOLBOX_PROVISION_DATE"
    [ -n "$provision_date" ] || provision_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    install -d -m 0755 -- "$(dirname "$IOLBOX_POLICY_FILE")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create policy directory'
    tmp="$(mktemp "${IOLBOX_POLICY_FILE}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create policy temporary file'
    printf '%s\n' \
        'iolbox macOS/Lima guest kernel policy' \
        "profile=$IOLBOX_PROFILE" \
        "profile_status=$IOLBOX_PROFILE_STATUS" \
        'purpose=reproducibility' \
        'canary_is_authority=true' \
        "provisioned_at=$provision_date" \
        "host_macos=$IOLBOX_HOST_MACOS" \
        "host_lima=$IOLBOX_HOST_LIMA" \
        "machine=$IOLBOX_MACHINE" \
        "qualified_kernel_series=$IOLBOX_KERNEL_SERIES" \
        "qualified_kernel=$(iolbox_kernel_qualification "$codename")" \
        "image_qualification=$(iolbox_image_qualification "$codename")" \
        "held_kernel_packages=$held_list" \
        'security_update_tradeoff=holding the installed kernel and pinning backports delays kernel security updates; this is deliberate for reproducibility' \
        'check_holds=apt-mark showhold' \
        'deliberate_requalification=run the executable canary on the exact host/kernel pair, review the policy and security updates, then apt-mark unhold the listed packages, remove the preferences pin, update, and reboot deliberately' \
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
        if ! text_contains_exact_line "$holds" "$package"; then
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
    grep -Fqx 'purpose=reproducibility' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: purpose=reproducibility is missing'
    grep -Fqx 'canary_is_authority=true' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: canary_is_authority=true is missing'
    grep -Fq 'qualified_kernel=' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: kernel qualification is missing'
    grep -Fq 'image_qualification=' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: image qualification is missing'
    grep -Fq 'security_update_tradeoff=' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: security-update tradeoff is missing'
    grep -Fq 'deliberate_requalification=' "$IOLBOX_POLICY_FILE" || \
        die "$IOLBOX_EXIT_VERIFY" 'policy assertion: deliberate requalification steps are missing'
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
