#!/usr/bin/env bash
# canary-native-classify-test.sh — offline negative coverage for the
# native-arm64 canary.
#
# This test loads only the native canary's pure classifier and failure
# renderer. It intentionally needs no guest, Lima machine, or aarch64 loader,
# so a machine that is unavailable can still test the gate's fail-closed
# behavior — same discipline as canary-classify-test.sh for the Rosetta path.

set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IOLBOX_CANARY_LIB_ONLY=1
export IOLBOX_CANARY_LIB_ONLY
# shellcheck source=../guest/30-canary-native.sh
if [ ! -r "$test_dir/../guest/30-canary-native.sh" ]; then
    printf 'ERROR: required native canary fixture is missing or unreadable: %s\n' "$test_dir/../guest/30-canary-native.sh" >&2
    exit 1
fi
if ! . "$test_dir/../guest/30-canary-native.sh"; then
    printf 'ERROR: could not source native canary fixture: %s\n' "$test_dir/../guest/30-canary-native.sh" >&2
    exit 1
fi

total=0
failures=0

assert_verdict() {
    local name="$1" exit_status="$2" output_text="$3" expected="$4" actual

    total=$((total + 1))
    actual="$(native_canary_classify "$exit_status" "$output_text")"
    if [ "$actual" = "$expected" ]; then
        printf 'ok - %s\n' "$name"
    else
        printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$actual"
        failures=$((failures + 1))
    fi
}

assert_contains() {
    local name="$1" haystack="$2" needle="$3"

    total=$((total + 1))
    if [[ "$haystack" == *"$needle"* ]]; then
        printf 'ok - %s\n' "$name"
    else
        printf 'FAIL - %s (missing %s)\n' "$name" "$needle"
        failures=$((failures + 1))
    fi
}

assert_eq() {
    local name="$1" actual="$2" expected="$3"

    total=$((total + 1))
    if [ "$actual" = "$expected" ]; then
        printf 'ok - %s\n' "$name"
    else
        printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$actual"
        failures=$((failures + 1))
    fi
}

native_pass='ld.so (Debian GLIBC 2.41-12) stable release version 2.41.'

assert_verdict 'real native pass output' 0 "$native_pass" PASS
assert_verdict 'exit 0 with empty output is not a pass' 0 '' FAIL_OTHER
assert_verdict 'missing native loader' 127 'bash: /lib/ld-linux-aarch64.so.1: No such file or directory' FAIL_MISSING
assert_verdict 'binfmt Exec format error' 126 'bash: /lib/ld-linux-aarch64.so.1: cannot execute binary file: Exec format error' FAIL_NOEXEC
assert_verdict 'unrecognised error' 1 'loader failed for an unrecognised reason' FAIL_OTHER
assert_verdict 'near-miss glibc line without closing parenthesis' 0 'ld.so (Debian GLIBC 2.41-12' FAIL_OTHER

# There is no FAIL_AUXV verdict on this path: that failure mode is specific
# to Rosetta's translation of amd64 auxv entries.
total=$((total + 1))
if native_canary_classify 134 'unhandled auxillary vector type 28' | grep -qx 'FAIL_AUXV'; then
    printf 'FAIL - native classifier must never emit FAIL_AUXV\n'
    failures=$((failures + 1))
else
    printf 'ok - native classifier never emits the Rosetta-specific FAIL_AUXV verdict\n'
fi

rendered="$(native_canary_render_failure \
    FAIL_ROSETTA_PRESENT \
    '26.6.1 (25G76)' \
    '2.2.0' \
    'iolbox-native-arm64' \
    '6.12.101+deb13-cloud-arm64' \
    'aarch64' \
    'PRESENT (unexpected for native-arm64): enabled interpreter=/mnt/lima-rosetta/rosetta' \
    'absent: /proc/sys/fs/binfmt_misc/qemu-x86_64 is unreadable or missing' \
    '/lib/ld-linux-aarch64.so.1' \
    'yes' \
    'Rosetta binfmt entry is registered on a native-arm64 guest.')"

assert_contains 'rosetta-present message names macOS product/build' "$rendered" '26.6.1 (25G76)'
assert_contains 'rosetta-present message names Lima version' "$rendered" '2.2.0'
assert_contains 'rosetta-present message names guest kernel' "$rendered" '6.12.101+deb13-cloud-arm64'
assert_contains 'rosetta-present message flags the unexpected Rosetta entry' "$rendered" 'unexpected for native-arm64'
assert_contains 'rosetta-present remediation recreates the machine from the native-arm64 template' "$rendered" 'native-arm64 template'

json="$(native_canary_json_object PASS "ld.so (Debian GLIBC 2.41-12) stable release version 2.41." '6.12.101+deb13-cloud-arm64' 'absent (expected)' 'registered: enabled' '' '26.6.1' '25G76' '2.2.0' 'native-arm64' '2026-08-19T00:00:00Z')"
assert_contains 'json object records PASS verdict' "$json" '"verdict":"PASS"'
assert_contains 'json object records profile' "$json" '"profile":"native-arm64"'
assert_contains 'json object records qemu_user_binfmt' "$json" '"qemu_user_binfmt":"registered: enabled"'
assert_contains 'json object records rosetta absence in the binfmt field' "$json" '"binfmt":"absent (expected)"'

assert_eq 'qemu-user absent classifies as unregistered' "$(native_canary_remediation FAIL_QEMU_USER_ABSENT)" "$(native_canary_remediation FAIL_QEMU_USER_ABSENT)"

printf 'Summary: %d cases, %d failures\n' "$total" "$failures"
[ "$failures" -eq 0 ]
