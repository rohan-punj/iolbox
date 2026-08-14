#!/usr/bin/env bash
# 30-canary.sh — execute the smallest useful amd64 program through Rosetta.
#
# This is the hard compatibility gate for the macOS/Lima guest. The loader is
# deliberately used instead of an iolbox binary: it tests Rosetta, the guest
# kernel's auxv contract, binfmt registration, and the amd64 multiarch install
# before any payload is started. The order is: inspect binfmt, run the loader
# with a timeout, classify the captured result, persist it, then report it.
#
# The classifier and the failure renderer are pure functions so they can be
# exercised on any bash host without Lima, a guest, or Rosetta. Set
# IOLBOX_CANARY_LIB_ONLY=1 before sourcing this file to load those functions
# without running main().
#
# Environment variables used in addition to lib.sh:
#   IOLBOX_CANARY_TIMEOUT  timeout in seconds (default: 10)

set -euo pipefail

# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# canary_extract_version <output_text>
#
# Print the complete, recognisable ld.so/glibc version fragment. Keeping this
# deliberately narrow is important: an exit-0 loader that prints silence, a
# warning, or an unrelated string is not a passing canary.
canary_extract_version() {
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

# canary_classify <exit_status> <output_text>
#
# This function is intentionally independent of files, environment variables,
# and host facts. It prints exactly one verdict and returns zero so callers can
# use it in command substitutions under set -e.
canary_classify() {
    local exit_status="$1" output_text="$2"

    if [[ "$exit_status" =~ ^0$ ]] && canary_extract_version "$output_text" >/dev/null; then
        printf 'PASS\n'
    elif [[ "$output_text" == *"unhandled auxillary vector type 28"* ]]; then
        printf 'FAIL_AUXV\n'
    elif [[ "$exit_status" = 127 && "$output_text" == *"ld-linux-x86-64.so.2"* && "$output_text" == *"No such file or directory"* ]]; then
        printf 'FAIL_MISSING\n'
    elif [[ "$exit_status" = 126 && "$output_text" == *"Exec format error"* ]]; then
        printf 'FAIL_NOEXEC\n'
    else
        printf 'FAIL_OTHER\n'
    fi
}

# canary_binfmt_state
#
# Read the named Rosetta binfmt entry before executing anything. The entry is
# diagnostic context only: the executable canary remains the authority because
# a present registration can still be incompatible with the guest kernel.
canary_binfmt_state() {
    local entry='/proc/sys/fs/binfmt_misc/rosetta' status='/proc/sys/fs/binfmt_misc/status'
    local entries=''

    if [ -r "$entry" ]; then
        printf 'registered: %s' "$(tr '\n' ' ' < "$entry")"
        return 0
    fi

    if [ -r "$status" ]; then
        entries="$(find /proc/sys/fs/binfmt_misc -maxdepth 1 -type f -printf '%f ' 2>/dev/null || true)"
        printf 'rosetta entry absent; binfmt_misc status: %s; entries: %s' \
            "$(tr '\n' ' ' < "$status")" "$entries"
    else
        printf 'unavailable: /proc/sys/fs/binfmt_misc/rosetta and binfmt_misc/status are unreadable or absent'
    fi
}

# canary_remediation <verdict>
#
# Pure text used by the human-readable failure block. In particular, the
# macOS fix point is deliberately not guessed from the observed 13.5 failure.
canary_remediation() {
    local verdict="$1"

    case "$verdict" in
        FAIL_AUXV)
            printf '%s' "The exact current Rosetta/kernel pair failed because the guest emitted AT_RSEQ_ALIGN (auxv type 28), which Rosetta on macOS ${IOLBOX_HOST_MACOS:-unknown} could not handle. This is not a blanket kernel >= 6.3 restriction: macOS 26.6.1 has passed with kernels 6.8 and 6.12. Inspect the Lima Rosetta share and binfmt registration; when Rosetta/binfmt is absent or Lima warned while configuring Rosetta, reinstall Lima with brew reinstall lima and recreate or restart the guest. The jammy profile (Ubuntu 22.04, kernel 5.15) is the compatibility profile. Re-run this canary after remediation."
            ;;
        FAIL_NOEXEC)
            printf '%s' 'The amd64 loader reached the kernel but was rejected with Exec format error. Start Lima with VZ and Rosetta, verify the Rosetta binfmt registration, and re-run this canary.'
            ;;
        FAIL_MISSING)
            printf '%s' 'The amd64 loader is absent. Run the amd64 multiarch provisioning step (10-multiarch.sh) and install libc6:amd64, then re-run this canary before installing the payload.'
            ;;
        FAIL_OTHER)
            printf '%s' 'The amd64 loader did not produce a valid glibc version. Inspect the captured error, binfmt registration, and amd64 package installation, correct the guest, and re-run this canary.'
            ;;
        *)
            printf '%s' 'The canary returned an unknown verdict. Treat the guest as incompatible, inspect the captured output, and re-run the canary after correcting the guest.'
            ;;
    esac
}

# canary_render_failure <verdict> <macos> <lima> <machine> <kernel> <arch>
#   <binfmt> <loader_path> <loader_exists> <captured_error>
#
# The renderer is pure: all facts are explicit arguments. This keeps the
# actionable diagnostic independently testable from the hardware-only main.
canary_render_failure() {
    local verdict="$1" host_macos="$2" host_lima="$3" machine="$4"
    local kernel="$5" arch="$6" binfmt="$7" loader_path="$8"
    local loader_exists="$9" captured_error="${10}"

    printf '%s\n' \
        'Rosetta canary: FAILED' \
        "Verdict: $verdict" \
        "macOS product/build: $host_macos" \
        "Lima version: $host_lima" \
        "Lima machine: $machine" \
        "Guest kernel: $kernel" \
        "Guest arch: $arch" \
        "Binfmt registration: $binfmt" \
        "Loader path: $loader_path" \
        "Loader exists: $loader_exists" \
        'Actual captured error text:'

    if [ -n "$captured_error" ]; then
        printf '%s\n' "$captured_error"
    else
        printf '%s\n' '<empty>'
    fi

    printf 'Remediation: %s\n' "$(canary_remediation "$verdict")"
}

# canary_json_escape <text>
#
# Escape the characters that JSON strings cannot contain literally. Bash
# cannot store NUL bytes, but it can store the other JSON control characters;
# the common escapes are emitted canonically and all remaining controls become
# six-character unicode escapes.
canary_json_escape() {
    local LC_ALL=C
    local value="$1" char code i

    for ((i = 0; i < ${#value}; i++)); do
        char="${value:i:1}"
        case "$char" in
            \\) printf '\\\\' ;;
            '"') printf '\\"' ;;
            $'\b') printf '\\b' ;;
            $'\f') printf '\\f' ;;
            $'\n') printf '\\n' ;;
            $'\r') printf '\\r' ;;
            $'\t') printf '\\t' ;;
            *)
                printf -v code '%d' "'${char}"
                if [ "$code" -lt 32 ]; then
                    printf '\\u%04x' "$code"
                else
                    printf '%s' "$char"
                fi
                ;;
        esac
    done
}

# canary_json_object <verdict> <version> <kernel> <binfmt> <error>
canary_json_object() {
    local verdict="$1" version="$2" kernel="$3" binfmt="$4" error_text="$5"
    local macos_product="${6:-unknown}"
    local macos_build="${7:-unknown}"
    local lima_version="${8:-unknown}"
    local profile="${9:-unknown}"
    local timestamp="${10:-}"
    local escaped_verdict escaped_version escaped_kernel escaped_binfmt escaped_error
    local escaped_product escaped_build escaped_lima escaped_profile escaped_timestamp

    [ -n "$timestamp" ] || timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    escaped_verdict="$(canary_json_escape "$verdict")"
    escaped_version="$(canary_json_escape "$version")"
    escaped_kernel="$(canary_json_escape "$kernel")"
    escaped_binfmt="$(canary_json_escape "$binfmt")"
    escaped_error="$(canary_json_escape "$error_text")"
    escaped_product="$(canary_json_escape "$macos_product")"
    escaped_build="$(canary_json_escape "$macos_build")"
    escaped_lima="$(canary_json_escape "$lima_version")"
    escaped_profile="$(canary_json_escape "$profile")"
    escaped_timestamp="$(canary_json_escape "$timestamp")"

    printf '{"schema":1,"macos_product":"%s","macos_build":"%s","lima_version":"%s","profile":"%s","kernel":"%s","binfmt":"%s","verdict":"%s","timestamp":"%s","version":"%s","error":"%s"}\n' \
        "$escaped_product" "$escaped_build" "$escaped_lima" "$escaped_profile" \
        "$escaped_kernel" "$escaped_binfmt" "$escaped_verdict" "$escaped_timestamp" \
        "$escaped_version" "$escaped_error"
}

canary_write_record() {
    # NOTE: these must be three separate `local` statements. Bash expands every
    # argument to an assignment builtin BEFORE performing any of the
    # assignments, so `local a=1 b="$a/x"` evaluates $a while it is still
    # unset — which aborts under `set -u`. Found on hardware (macOS 26.6.1,
    # Jammy guest): "30-canary.sh: line 192: record_dir: unbound variable",
    # exit 1, i.e. the gate crashed instead of returning a verdict.
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
Usage: 30-canary.sh [--quiet | --json]

Run the Rosetta amd64 loader canary. The default prints a human-readable
failure diagnostic; --quiet prints only the verdict and --json prints one
JSON object. Both flags preserve the normal exit-code contract.
USAGE
}

main() {
    local quiet=0 json_mode=0 arg
    local timeout_seconds="${IOLBOX_CANARY_TIMEOUT:-10}"
    local tmp_dir stdout_file stderr_file stdout_text stderr_text captured_text
    local exit_status verdict version kernel arch binfmt loader_exists json_text
    local macos_product macos_build lima_version profile timestamp host_macos

    while [ "$#" -gt 0 ]; do
        arg="$1"
        case "$arg" in
            --quiet)
                quiet=1
                ;;
            --json)
                json_mode=1
                ;;
            -h|--help)
                usage
                return "$IOLBOX_EXIT_OK"
                ;;
            *)
                usage >&2
                return "$IOLBOX_EXIT_USAGE"
                ;;
        esac
        shift
    done

    if [ "$quiet" -eq 1 ] && [ "$json_mode" -eq 1 ]; then
        printf '%s\n' '30-canary.sh: --quiet and --json are mutually exclusive' >&2
        return "$IOLBOX_EXIT_USAGE"
    fi

    case "$timeout_seconds" in
        ''|*[!0-9]*)
            printf '30-canary.sh: IOLBOX_CANARY_TIMEOUT must be a positive integer, got %s\n' \
                "$timeout_seconds" >&2
            return "$IOLBOX_EXIT_USAGE"
            ;;
    esac
    if [ "$timeout_seconds" -eq 0 ]; then
        printf '%s\n' '30-canary.sh: IOLBOX_CANARY_TIMEOUT must be greater than zero' >&2
        return "$IOLBOX_EXIT_USAGE"
    fi
    have timeout || die "$IOLBOX_EXIT_USAGE" 'timeout(1) is required to run the Rosetta canary safely'

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

    binfmt="$(canary_binfmt_state)"
    kernel="$(uname -r)"
    arch="$(uname -m)"
    if [ -e "$IOLBOX_LOADER" ]; then
        loader_exists='yes'
    else
        loader_exists='no'
    fi

    tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-canary.XXXXXX")"
    canary_tmp_dir="$tmp_dir"
    stdout_file="$tmp_dir/stdout"
    stderr_file="$tmp_dir/stderr"
    trap canary_cleanup EXIT

    # Keep stdout and stderr separate so a version-looking diagnostic on
    # stderr can never turn an exit-0-but-wrong-output run into PASS. For
    # failures, classification receives the complete captured stream so the
    # measured Rosetta auxv text is still authoritative.
    if timeout --foreground --kill-after=2s "${timeout_seconds}s" "$IOLBOX_LOADER" --version >"$stdout_file" 2>"$stderr_file"; then
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
        verdict="$(canary_classify "$exit_status" "$stdout_text")"
    else
        verdict="$(canary_classify "$exit_status" "$captured_text")"
    fi
    version="$(canary_extract_version "$stdout_text" || true)"
    if [ "$verdict" = 'PASS' ]; then
        json_text="$(canary_json_object "$verdict" "$version" "$kernel" "$binfmt" '' \
            "$macos_product" "$macos_build" "$lima_version" "$profile" "$timestamp")"
    else
        json_text="$(canary_json_object "$verdict" "$version" "$kernel" "$binfmt" "$captured_text" \
            "$macos_product" "$macos_build" "$lima_version" "$profile" "$timestamp")"
    fi
    if ! canary_write_record "$json_text"; then
        if [ "$verdict" = 'PASS' ]; then
            die "$IOLBOX_EXIT_USAGE" \
                'could not persist /var/lib/iolbox/macos-canary.json'
        fi
        # Preserve the hard-gate exit code when the compatibility failure is
        # real; the warning makes the missing last-known record explicit.
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

    canary_render_failure "$verdict" "$IOLBOX_HOST_MACOS" "$IOLBOX_HOST_LIMA" \
        "$IOLBOX_MACHINE" "$kernel" "$arch" "$binfmt" "$IOLBOX_LOADER" \
        "$loader_exists" "$captured_text" >&2

    return "$IOLBOX_EXIT_CANARY"
}

if [ "${IOLBOX_CANARY_LIB_ONLY:-0}" != '1' ]; then
    main "$@"
fi
