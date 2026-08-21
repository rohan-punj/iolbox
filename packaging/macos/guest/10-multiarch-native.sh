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
# Those packages are REDISTRIBUTED BY IOLBOX inside the linux/arm64 payload,
# not fetched by the guest's apt at provisioning time. That is a deliberate
# decision -- iolbox is a learner tool, and the archive working
# self-contained matters more than minimizing what we ship. It also removes a
# real failure mode: the previous apt path broke whenever a Debian trixie
# point release moved libc6 out from under the pinned guest image. See
# THIRD_PARTY.md and docs/macos-native-arm64-qemu-redistribution-plan.md.
#
# In order, this step:
#   1. proves the guest is Debian trixie (the native-arm64 profile's pinned
#      suite; see packaging/macos/lima/profiles.env);
#   2. enables the amd64 foreign architecture;
#   3. extracts the bundled .deb set from the payload tarball and verifies
#      every file against the bundle MANIFEST, in both directions;
#   4. checks the Multi-Arch: same version agreement between the bundled
#      amd64 packages and the guest's native arm64 ones;
#   5. installs whatever is not already at the pinned version with dpkg, then
#      configures and audits; and
#   6. asserts the foreign architecture, the qemu-x86_64 handler, and the
#      x86-64 loader/libc/libssl objects.
#
# There is no apt-get anywhere in this script and no network fallback. If the
# bundle is absent or fails verification, provisioning fails.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

IOLBOX_QEMU_X86_64_BINFMT="${IOLBOX_QEMU_X86_64_BINFMT:-/proc/sys/fs/binfmt_misc/qemu-x86_64}"

usage() {
    cat <<EOF
Usage: $0 [--verify]

  --verify    assert the native-arm64 translator/multiarch end state without
              changing files or installing anything.
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

# ---------------------------------------------------------------------------
# Bundled-package install.
#
# iolbox redistributes the translator packages rather than having the guest
# apt-get them at provisioning time (see THIRD_PARTY.md and
# docs/macos-native-arm64-qemu-redistribution-plan.md). The .deb files ride
# inside the linux/arm64 payload tarball, which the launcher has already
# staged on the guest before this step runs -- staging completes before the
# step sequence begins and IOLBOX_PAYLOAD_TARBALL is in every step's
# environment (tools/iolab-launcher/macos_lifecycle.go).
#
# There is deliberately NO apt fallback anywhere below. A fallback would mean
# the offline guarantee silently evaporates the first time the bundle is
# malformed, which is exactly the failure this change exists to remove. If
# the bundle is missing or bad, provisioning fails loudly.
# ---------------------------------------------------------------------------

IOLBOX_BUNDLE_SUBDIR="${IOLBOX_BUNDLE_SUBDIR:-guest-assets/qemu-user}"

# Unpack just the bundle subtree out of the payload tarball.
extract_bundle() {
    local dest="$1" tarball="${IOLBOX_PAYLOAD_TARBALL:-}"

    [ -n "$tarball" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        'bundle assertion: IOLBOX_PAYLOAD_TARBALL is not set'
    [ -f "$tarball" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "bundle assertion: payload tarball not found: $tarball"

    # The payload's top-level directory name carries the version, so match the
    # bundle path by wildcard rather than hardcoding it.
    if ! tar -xzf "$tarball" -C "$dest" --strip-components=1 \
             --wildcards "*/$IOLBOX_BUNDLE_SUBDIR" 2>/dev/null; then
        die "$IOLBOX_EXIT_APT" \
            "bundle assertion: payload $tarball does not contain $IOLBOX_BUNDLE_SUBDIR"
    fi
    [ -d "$dest/$IOLBOX_BUNDLE_SUBDIR" ] || die "$IOLBOX_EXIT_APT" \
        "bundle assertion: $IOLBOX_BUNDLE_SUBDIR missing after extraction"
}

# Validate the bundle strictly before handing anything to dpkg. These files
# are executed as root through package maintainer scripts, so "the manifest
# named it" is not a sufficient basis for installing it: the manifest is
# checked against the directory in BOTH directions, and every basename is
# constrained.
verify_bundle() {
    local dir="$1"
    local manifest="$dir/MANIFEST"
    local package version arch sha256 filename path actual listed=0 seen
    local found

    [ -f "$manifest" ] || die "$IOLBOX_EXIT_APT" \
        "bundle assertion: MANIFEST missing from $dir"

    seen=""
    while IFS='|' read -r package version arch sha256 filename || [ -n "${package:-}" ]; do
        case "${package:-}" in ''|'#'*) continue ;; esac
        [ -n "$version" ] && [ -n "$arch" ] && [ -n "$sha256" ] && [ -n "$filename" ] || \
            die "$IOLBOX_EXIT_APT" "bundle assertion: malformed MANIFEST row: $package"
        case "$arch" in
            amd64|arm64) ;;
            *) die "$IOLBOX_EXIT_APT" "bundle assertion: bad arch '$arch' for $package" ;;
        esac
        # No path separators, no traversal, no surprises.
        case "$filename" in
            */*|*..*|.*|'') die "$IOLBOX_EXIT_APT" \
                "bundle assertion: unsafe filename in MANIFEST: $filename" ;;
        esac
        case "$filename" in
            *.deb) ;;
            *) die "$IOLBOX_EXIT_APT" "bundle assertion: not a .deb: $filename" ;;
        esac
        printf '%s' "$sha256" | grep -Eq '^[0-9a-f]{64}$' || die "$IOLBOX_EXIT_APT" \
            "bundle assertion: malformed sha256 for $package"
        if text_contains_exact_line "$seen" "$arch/$filename"; then
            die "$IOLBOX_EXIT_APT" "bundle assertion: duplicate MANIFEST row: $arch/$filename"
        fi
        seen="$seen$arch/$filename
"
        path="$dir/$arch/$filename"
        [ -f "$path" ] && [ ! -L "$path" ] || die "$IOLBOX_EXIT_APT" \
            "bundle assertion: $arch/$filename is missing or not a regular file"
        actual="$(sha256sum "$path" | awk '{print $1}')"
        [ "$actual" = "$sha256" ] || die "$IOLBOX_EXIT_APT" \
            "bundle assertion: sha256 mismatch for $arch/$filename (expected $sha256, got $actual)"
        listed=$((listed + 1))
    done < "$manifest"

    [ "$listed" -gt 0 ] || die "$IOLBOX_EXIT_APT" \
        'bundle assertion: MANIFEST listed no packages'

    # Reverse direction: refuse any .deb present on disk but absent from the
    # manifest. Without this, an extra unlisted .deb would be installed by the
    # glob below having passed no checksum check at all.
    found=0
    for path in "$dir"/arm64/*.deb "$dir"/amd64/*.deb; do
        [ -e "$path" ] || continue
        found=$((found + 1))
        if ! text_contains_exact_line "$seen" \
             "$(basename "$(dirname "$path")")/$(basename "$path")"; then
            die "$IOLBOX_EXIT_APT" \
                "bundle assertion: $path is present but not listed in MANIFEST"
        fi
    done
    [ "$found" -eq "$listed" ] || die "$IOLBOX_EXIT_APT" \
        "bundle assertion: MANIFEST lists $listed packages but $found are present"

    log "bundle verified: $listed packages, all checksums match"
}

# Multi-Arch: same packages must be co-installed at IDENTICAL versions across
# architectures. The guest's native arm64 versions come from the pinned Lima
# image; the amd64 versions come from our lock, which is pinned to a Debian
# snapshot matching that image's build date so the two agree by construction.
#
# If the image pin is ever moved without regenerating the lock, that agreement
# breaks and dpkg emits a confusing "different version" error deep in a
# transaction. Check it up front and say what actually went wrong.
check_multiarch_versions() {
    local dir="$1"
    local manifest="$dir/MANIFEST"
    local package version arch sha256 filename native

    while IFS='|' read -r package version arch sha256 filename || [ -n "${package:-}" ]; do
        case "${package:-}" in ''|'#'*) continue ;; esac
        [ "$arch" = amd64 ] || continue
        native="$(dpkg-query -W -f='${Version}' "$package" 2>/dev/null || true)"
        [ -n "$native" ] || continue
        if [ "$native" != "$version" ]; then
            die "$IOLBOX_EXIT_APT" \
"multiarch assertion: $package is installed natively at '$native' but the bundle pins '$version'.
  Debian requires Multi-Arch: same packages to be co-installed at identical
  versions, so dpkg will reject this bundle.
  Cause: the pinned Lima image (packaging/macos/lima/pinned-image-native-arm64.env)
  and the package lock (packaging/macos/guest-assets/qemu-user.lock) have drifted.
  Fix: re-run packaging/macos/guest-assets/generate-lock.sh with a snapshot
  timestamp matching the image's build date, and commit the regenerated lock."
        fi
    done < "$manifest"
}

# Install only what is not already present at the pinned version. `dpkg -i` on
# an already-installed identical version is NOT a no-op -- it re-unpacks,
# re-runs maintainer scripts and re-fires triggers, which for binfmt-support
# and qemu means touching live binfmt registration state. Provisioning re-runs
# are supported, so skip what is already correct.
install_bundle() {
    local dir="$1"
    local manifest="$dir/MANIFEST"
    local package version arch sha256 filename installed rc audit_output
    local to_install=""

    while IFS='|' read -r package version arch sha256 filename || [ -n "${package:-}" ]; do
        case "${package:-}" in ''|'#'*) continue ;; esac
        if [ "$arch" = amd64 ]; then
            installed="$(dpkg-query -W -f='${Status} ${Version}' "$package:amd64" 2>/dev/null || true)"
        else
            installed="$(dpkg-query -W -f='${Status} ${Version}' "$package" 2>/dev/null || true)"
        fi
        if [ "$installed" = "install ok installed $version" ]; then
            log "already at $version, skipping: $package:$arch"
            continue
        fi
        to_install="$to_install $dir/$arch/$filename"
    done < "$manifest"

    if [ -z "$to_install" ]; then
        log 'every bundled package is already installed at the pinned version'
        return 0
    fi

    # One invocation for the whole set: dpkg unpacks all, then configures all,
    # so intra-set dependencies do not impose an ordering on us. The set is
    # dependency-complete by construction (generate-lock.sh resolves the
    # transitive closure), which is what makes this safe without apt.
    #
    # --force-depends is deliberately NOT used. It would convert a genuine
    # closure bug into a half-configured guest that fails later and further
    # away from the cause.
    # shellcheck disable=SC2086
    if dpkg --install $to_install; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" \
            "command failed (exit $rc): dpkg --install (bundled packages)"
    fi

    # dpkg can leave packages unpacked-but-unconfigured when configuration
    # ordering is awkward. This is a configuration pass, not a dependency
    # escape hatch: --audit below still fails if anything is genuinely broken.
    if dpkg --configure --pending; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" "command failed (exit $rc): dpkg --configure --pending"
    fi

    audit_output="$(dpkg --audit 2>&1 || true)"
    if [ -n "$audit_output" ]; then
        die "$IOLBOX_EXIT_APT" \
            "dpkg --audit reports a broken package state after installing the bundle: $audit_output"
    fi
}

run_provision() {
    local rc codename work_dir bundle_dir

    codename="$(guest_codename)"

    if dpkg --add-architecture amd64; then
        :
    else
        rc=$?
        die "$IOLBOX_EXIT_APT" "command failed (exit $rc): dpkg --add-architecture amd64"
    fi

    work_dir="$(mktemp -d /tmp/iolbox-qemu-bundle.XXXXXX)" || die "$IOLBOX_EXIT_APT" \
        'could not create a temporary directory for the package bundle'
    # shellcheck disable=SC2064
    trap "rm -rf '$work_dir'" EXIT INT TERM

    extract_bundle "$work_dir"
    bundle_dir="$work_dir/$IOLBOX_BUNDLE_SUBDIR"
    verify_bundle "$bundle_dir"
    check_multiarch_versions "$bundle_dir"
    install_bundle "$bundle_dir"

    rm -rf "$work_dir"
    trap - EXIT INT TERM

    # Debian's qemu-user-binfmt packaging normally registers the handlers via
    # update-binfmts automatically; make it explicit and idempotent so a
    # provisioning re-run -- or a run that skipped installs because everything
    # was already present -- cannot leave the handler disabled.
    if have update-binfmts; then
        update-binfmts --enable qemu-x86_64 2>/dev/null || true
    fi
    verify_runtime "$IOLBOX_EXIT_APT"
    log "native-arm64 translator (qemu-user) and amd64 runtime are installed from the bundled packages and verified: $codename"
}

verify_end_state() {
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
