#!/usr/bin/env bash
# 30-canary-native.sh — native-arm64's hard compatibility gate.
#
# This is the native-arm64 counterpart of 30-canary.sh. Where the Rosetta
# canary executes the smallest useful amd64 program THROUGH Rosetta (proving
# translation works), this canary proves the opposite: that the guest's own
# native aarch64 dynamic linker runs WITHOUT any translation layer, and that
# no macOS-host Rosetta binfmt entry is wired into this guest at all. A
# native-arm64 guest that still has a Rosetta interpreter registered has not
# been proven to be running untranslated, even if this canary's own loader
# check passes — so an unexpectedly-present Rosetta entry is also a hard
# failure here, not a warning.
#
# The x86_64 IOL device-image binary itself is a separate, orthogonal
# concern: Phase 3 (docs/macos-m7-phase3-execution-plan.md) measured and
# selected qemu-user as the sole correctness-eligible in-guest translator for
# that one binary, and this canary also confirms qemu-user's binfmt
# registration is present — "native-arm64" means the supervisor/vpcs/
# toollaunch userspace stack is native, not that literally every executable
# in the guest is native.
#
# Order: inspect Rosetta binfmt (must be ABSENT), inspect the in-guest
# qemu-user binfmt (must be PRESENT), run the native aarch64 loader with a
# timeout, classify the captured result, persist it, then report it.
#
# The classifier and the failure renderer are pure functions so they can be
# exercised on any bash host without Lima, a guest, or qemu-user. Set
# IOLBOX_CANARY_LIB_ONLY=1 before sourcing this file to load those functions
# without running main().
#
# Environment variables used in addition to lib.sh:
#   IOLBOX_CANARY_TIMEOUT       timeout in seconds (default: 10)
#   IOLBOX_NATIVE_LOADER         native aarch64 dynamic linker path
#                                 (default: /lib/ld-linux-aarch64.so.1)

set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# lib.sh unconditionally sets IOLBOX_LOADER to the amd64 loader path for the
# Rosetta canary's own use; this profile's hard gate is the native loader,
# not that one.
IOLBOX_NATIVE_LOADER="${IOLBOX_NATIVE_LOADER:-/lib/ld-linux-aarch64.so.1}"

# native_canary_extract_version <output_text>
#
# Same narrow contract as the Rosetta canary's version extractor: an exit-0
# loader that prints silence, a warning, or an unrelated string is not a
# passing canary.
native_canary_extract_version() {
    local output_text="$1" line
    local glibc_re='(ld\.so[[:space:]]+\([^)]*GLIBC[[:space:]]+[0-9]+\.[0-9]+[0-9A-Za-z._+-]*\))'

    while IFS= read -r line; do
        if [[ "$line" =~ $glibc_re ]]; then
            printf '%s' "${BASH_REMATCH[1]}"
            return 0
        fi
    done <<< "$output_text"

    return 1
}

# native_canary_classify <exit_status> <output_text>
#
# Intentionally independent of files, environment variables, and host facts.
# No FAIL_AUXV verdict here: that failure mode is specific to Rosetta's
# translation of amd64 auxv entries, which does not exist on this path.
native_canary_classify() {
    local exit_status="$1" output_text="$2"

    if [[ "$exit_status" =~ ^0$ ]] && native_canary_extract_version "$output_text" >/dev/null; then
        printf 'PASS\n'
    elif [[ "$exit_status" = 127 && "$output_text" == *"No such file or directory"* ]]; then
        printf 'FAIL_MISSING\n'
    elif [[ "$exit_status" = 126 && "$output_text" == *"Exec format error"* ]]; then
        printf 'FAIL_NOEXEC\n'
    else
        printf 'FAIL_OTHER\n'
    fi
}

# native_canary_qemu_user_state
#
# Diagnostic context for the qemu-x86_64 binfmt entry the IOL device-image
# binary still needs. Not the hard gate by itself (a missing IOL image is a
# separate, later concern), but its absence is recorded so a native-arm64
# guest that can run the supervisor natively but cannot yet run x86_64 IOL
# images is visibly distinguishable from one that is fully ready.
native_canary_qemu_user_state() {
    local entry='/proc/sys/fs/binfmt_misc/qemu-x86_64'

    if [ -r "$entry" ]; then
        printf 'registered: %s' "$(tr '\n' ' ' < "$entry")"
    else
        printf 'absent: %s is unreadable or missing' "$entry"
    fi
}

native_canary_qemu_user_present() {
    local entry='/proc/sys/fs/binfmt_misc/qemu-x86_64'
    [ -r "$entry" ] && grep -q '^enabled' "$entry"
}

# native_canary_rosetta_state
#
# Mirrors 30-canary.sh's binfmt inspection, but for THIS profile a present
# Rosetta entry is itself an anomaly worth surfacing loudly: the whole point
# of native-arm64 is a guest that never depends on Rosetta.
native_canary_rosetta_state() {
    local entry='/proc/sys/fs/binfmt_misc/rosetta'

    if [ -r "$entry" ]; then
        printf 'PRESENT (unexpected for native-arm64): %s' "$(tr '\n' ' ' < "$entry")"
    else
        printf 'absent (expected)'
    fi
}

native_canary_rosetta_present() {
    [ -r /proc/sys/fs/binfmt_misc/rosetta ]
}

native_canary_remediation() {
    local verdict="$1"

    case "$verdict" in
        FAIL_MISSING)
            printf '%s' 'The native aarch64 loader is absent. This guest image should ship glibc for its own architecture by default; verify the pinned image and re-provision.'
            ;;
        FAIL_NOEXEC)
            printf '%s' 'The native aarch64 loader was rejected with Exec format error. This strongly suggests the guest kernel/userspace pair is not actually aarch64; inspect uname -m and the pinned image identity.'
            ;;
        FAIL_ROSETTA_PRESENT)
            printf '%s' 'A Rosetta binfmt entry is registered in this guest even though iolbox-native-arm64.yaml sets rosetta.enabled=false. Recreate the machine from the native-arm64 template; do not reuse a machine that was ever provisioned as rosetta-amd64.'
            ;;
        FAIL_QEMU_USER_ABSENT)
            printf '%s' 'The in-guest qemu-x86_64 binfmt handler required to run the x86_64-only IOL device-image binary is not registered. Run the native-arm64 multiarch step (10-multiarch-native.sh) to install and register qemu-user-static, then re-run this canary.'
            ;;
        FAIL_OTHER)
            printf '%s' 'The native loader did not produce a valid glibc version. Inspect the captured error and the pinned image, correct the guest, and re-run this canary before installing the payload.'
            ;;
        *)
            printf '%s' 'The canary returned an unknown verdict. Treat the guest as not proven native, inspect the captured output, and re-run the canary after correcting the guest.'
            ;;
    esac
}

native_canary_render_failure() {
    local verdict="$1" host_macos="$2" host_lima="$3" machine="$4"
    local kernel="$5" arch="$6" rosetta_state="$7" qemu_state="$8" loader_path="$9"
    local loader_exists="${10}" captured_error="${11}"

    printf '%s\n' \
        'Native-arm64 canary: FAILED' \
        "Verdict: $verdict" \
        "macOS product/build: $host_macos" \
        "Lima version: $host_lima" \
        "Lima machine: $machine" \
        "Guest kernel: $kernel" \
        "Guest arch: $arch" \
        "Rosetta binfmt state: $rosetta_state" \
        "qemu-user (qemu-x86_64) binfmt state: $qemu_state" \
        "Loader path: $loader_path" \
        "Loader exists: $loader_exists" \
        'Actual captured error text:'

    if [ -n "$captured_error" ]; then
        printf '%s\n' "$captured_error"
    else
        printf '%s\n' '<empty>'
    fi

    printf 'Remediation: %s\n' "$(native_canary_remediation "$verdict")"
}

native_canary_json_object() {
    local verdict="$1" version="$2" kernel="$3" rosetta_state="$4" qemu_state="$5" error_text="$6"
    local macos_product="${7:-unknown}"
    local macos_build="${8:-unknown}"
    local lima_version="${9:-unknown}"
    local profile="${10:-unknown}"
    local timestamp="${11:-}"
    local escaped_verdict escaped_version escaped_kernel escaped_rosetta escaped_qemu escaped_error
    local escaped_product escaped_build escaped_lima escaped_profile escaped_timestamp

    [ -n "$timestamp" ] || timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    escaped_verdict="$(iolbox_json_escape "$verdict")"
    escaped_version="$(iolbox_json_escape "$version")"
    escaped_kernel="$(iolbox_json_escape "$kernel")"
    escaped_rosetta="$(iolbox_json_escape "$rosetta_state")"
    escaped_qemu="$(iolbox_json_escape "$qemu_state")"
    escaped_error="$(iolbox_json_escape "$error_text")"
    escaped_product="$(iolbox_json_escape "$macos_product")"
    escaped_build="$(iolbox_json_escape "$macos_build")"
    escaped_lima="$(iolbox_json_escape "$lima_version")"
    escaped_profile="$(iolbox_json_escape "$profile")"
    escaped_timestamp="$(iolbox_json_escape "$timestamp")"

    printf '{"schema":1,"macos_product":"%s","macos_build":"%s","lima_version":"%s","profile":"%s","kernel":"%s","binfmt":"%s","qemu_user_binfmt":"%s","verdict":"%s","timestamp":"%s","version":"%s","error":"%s"}\n' \
        "$escaped_product" "$escaped_build" "$escaped_lima" "$escaped_profile" \
        "$escaped_kernel" "$escaped_rosetta" "$escaped_qemu" "$escaped_verdict" "$escaped_timestamp" \
        "$escaped_version" "$escaped_error"
}

native_canary_write_record() {
    # See canary_write_record's NOTE in 30-canary.sh for why these must be
    # separate `local` statements under set -u.
    local record_dir='/var/lib/iolbox'
    local record_file="$record_dir/macos-canary.json"
    local record_tmp="$record_file.tmp.$$" json="$1"

    mkdir -p "$record_dir"
    printf '%s\n' "$json" > "$record_tmp"
    mv -f "$record_tmp" "$record_file"
}

canary_tmp_dir=''

canary_cleanup() {
    if [ -n "$canary_tmp_dir" ]; then
        rm -rf -- "$canary_tmp_dir"
    fi
}

usage() {
    cat <<'USAGE'
Usage: 30-canary-native.sh [--quiet | --json]

Run the native-arm64 loader canary: prove Rosetta is absent, the in-guest
qemu-user x86_64 handler is present, and the native aarch64 loader works.
The default prints a human-readable failure diagnostic; --quiet prints only
the verdict and --json prints one JSON object. Both flags preserve the
normal exit-code contract.
USAGE
}

main() {
    local quiet=0 json_mode=0 arg
    local timeout_seconds="${IOLBOX_CANARY_TIMEOUT:-10}"
    local tmp_dir stdout_file stderr_file stdout_text stderr_text captured_text
    local exit_status verdict version kernel arch rosetta_state qemu_state loader_exists json_text
    local macos_product macos_build lima_version profile timestamp host_macos

    while [ "$#" -gt 0 ]; do
        arg="$1"
        case "$arg" in
            --quiet) quiet=1 ;;
            --json) json_mode=1 ;;
            -h|--help) usage; return "$IOLBOX_EXIT_OK" ;;
            *) usage >&2; return "$IOLBOX_EXIT_USAGE" ;;
        esac
        shift
    done

    if [ "$quiet" -eq 1 ] && [ "$json_mode" -eq 1 ]; then
        printf '%s\n' '30-canary-native.sh: --quiet and --json are mutually exclusive' >&2
        return "$IOLBOX_EXIT_USAGE"
    fi

    case "$timeout_seconds" in
        ''|*[!0-9]*)
            printf '30-canary-native.sh: IOLBOX_CANARY_TIMEOUT must be a positive integer, got %s\n' \
                "$timeout_seconds" >&2
            return "$IOLBOX_EXIT_USAGE"
            ;;
    esac
    if [ "$timeout_seconds" -eq 0 ]; then
        printf '%s\n' '30-canary-native.sh: IOLBOX_CANARY_TIMEOUT must be greater than zero' >&2
        return "$IOLBOX_EXIT_USAGE"
    fi
    have timeout || die "$IOLBOX_EXIT_USAGE" 'timeout(1) is required to run the native canary safely'

    macos_product="${IOLBOX_HOST_MACOS_PRODUCT:-}"
    macos_build="${IOLBOX_HOST_MACOS_BUILD:-}"
    host_macos="$IOLBOX_HOST_MACOS"
    if [ -z "$macos_product" ] && [[ "$host_macos" == *' ('*')' ]]; then
        macos_product="${host_macos%% (*}"
    fi
    if [ -z "$macos_build" ] && [[ "$host_macos" == *' ('*')' ]]; then
        macos_build="${host_macos##* (}"
        macos_build="${macos_build%)}"
    fi
    [ -n "$macos_product" ] || macos_product='unknown'
    [ -n "$macos_build" ] || macos_build='unknown'
    lima_version="${IOLBOX_HOST_LIMA:-unknown}"
    profile="${IOLBOX_PROFILE:-unknown}"
    timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    rosetta_state="$(native_canary_rosetta_state)"
    qemu_state="$(native_canary_qemu_user_state)"
    kernel="$(uname -r)"
    arch="$(uname -m)"
    if [ -e "$IOLBOX_NATIVE_LOADER" ]; then
        loader_exists='yes'
    else
        loader_exists='no'
    fi

    tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-canary-native.XXXXXX")"
    canary_tmp_dir="$tmp_dir"
    stdout_file="$tmp_dir/stdout"
    stderr_file="$tmp_dir/stderr"
    trap canary_cleanup EXIT

    # Fail closed BEFORE running the loader when the guest is structurally
    # wrong for this profile: a present Rosetta entry means this is not
    # proven to be an untranslated guest, regardless of what the loader
    # reports.
    if native_canary_rosetta_present; then
        exit_status=1
        verdict='FAIL_ROSETTA_PRESENT'
        captured_text='Rosetta binfmt entry is registered on a native-arm64 guest.'
    elif ! native_canary_qemu_user_present; then
        exit_status=1
        verdict='FAIL_QEMU_USER_ABSENT'
        captured_text='qemu-x86_64 binfmt handler is not registered.'
    else
        if timeout --foreground --kill-after=2s "${timeout_seconds}s" "$IOLBOX_NATIVE_LOADER" --version >"$stdout_file" 2>"$stderr_file"; then
            exit_status=0
        else
            exit_status=$?
        fi
        stdout_text="$(cat "$stdout_file")"
        stderr_text="$(cat "$stderr_file")"
        captured_text="$stdout_text"
        if [ -n "$stderr_text" ]; then
            if [ -n "$captured_text" ]; then
                captured_text+=$'\n'
            fi
            captured_text+="$stderr_text"
        fi
        if [ "$exit_status" -eq 0 ]; then
            verdict="$(native_canary_classify "$exit_status" "$stdout_text")"
        else
            verdict="$(native_canary_classify "$exit_status" "$captured_text")"
        fi
    fi

    version="$(native_canary_extract_version "${stdout_text:-}" || true)"
    if [ "$verdict" = 'PASS' ]; then
        json_text="$(native_canary_json_object "$verdict" "$version" "$kernel" "$rosetta_state" "$qemu_state" '' \
            "$macos_product" "$macos_build" "$lima_version" "$profile" "$timestamp")"
    else
        json_text="$(native_canary_json_object "$verdict" "$version" "$kernel" "$rosetta_state" "$qemu_state" "$captured_text" \
            "$macos_product" "$macos_build" "$lima_version" "$profile" "$timestamp")"
    fi
    if ! native_canary_write_record "$json_text"; then
        if [ "$verdict" = 'PASS' ]; then
            die "$IOLBOX_EXIT_USAGE" \
                'could not persist /var/lib/iolbox/macos-canary.json'
        fi
        printf '%s\n' \
            'WARNING: could not persist /var/lib/iolbox/macos-canary.json; continuing to report the canary failure' >&2
    fi

    if [ "$verdict" = 'PASS' ]; then
        if [ "$json_mode" -eq 1 ]; then
            printf '%s\n' "$json_text"
        elif [ "$quiet" -eq 1 ]; then
            printf 'PASS\n'
        else
            printf 'PASS: %s\n' "$version"
        fi
        return "$IOLBOX_EXIT_OK"
    fi

    if [ "$json_mode" -eq 1 ]; then
        printf '%s\n' "$json_text"
    elif [ "$quiet" -eq 1 ]; then
        printf '%s\n' "$verdict"
    fi

    native_canary_render_failure "$verdict" "$IOLBOX_HOST_MACOS" "$IOLBOX_HOST_LIMA" \
        "$IOLBOX_MACHINE" "$kernel" "$arch" "$rosetta_state" "$qemu_state" "$IOLBOX_NATIVE_LOADER" \
        "$loader_exists" "$captured_text" >&2

    return "$IOLBOX_EXIT_CANARY"
}

if [ "${IOLBOX_CANARY_LIB_ONLY:-0}" != '1' ]; then
    main "$@"
fi
