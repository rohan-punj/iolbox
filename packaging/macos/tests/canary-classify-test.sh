#!/usr/bin/env bash
# canary-classify-test.sh — offline negative coverage for the Rosetta canary.
#
# This test loads only the canary's pure classifier and failure renderer. It
# intentionally needs no guest, Lima machine, amd64 loader, or Rosetta, so a
# machine that is unavailable can still test the gate's fail-closed behavior.

set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IOLBOX_CANARY_LIB_ONLY=1
export IOLBOX_CANARY_LIB_ONLY
# shellcheck source=../guest/30-canary.sh
if [ ! -r "$test_dir/../guest/30-canary.sh" ]; then
    printf 'ERROR: required canary fixture is missing or unreadable: %s\n' "$test_dir/../guest/30-canary.sh" >&2
    exit 1
fi
if ! . "$test_dir/../guest/30-canary.sh"; then
    printf 'ERROR: could not source canary fixture: %s\n' "$test_dir/../guest/30-canary.sh" >&2
    exit 1
fi

total=0
failures=0

assert_verdict() {
    local name="$1" exit_status="$2" output_text="$3" expected="$4" actual

    total=$((total + 1))
    actual="$(canary_classify "$exit_status" "$output_text")"
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

m0_pass='ld.so (Ubuntu GLIBC 2.35-0ubuntu3.14)'
m0_auxv=$'rosetta error: unhandled auxillary vector type 28\ntimeout: the monitored command dumped core'

assert_verdict 'real M0 pass output' 0 "$m0_pass" PASS
assert_verdict 'real M0 auxv failure output' 134 "$m0_auxv" FAIL_AUXV
assert_verdict 'exit 0 with empty output is not a pass' 0 '' FAIL_OTHER
assert_verdict 'missing amd64 loader' 127 'bash: /lib64/ld-linux-x86-64.so.2: No such file or directory' FAIL_MISSING
assert_verdict 'binfmt Exec format error' 126 'bash: /lib64/ld-linux-x86-64.so.2: cannot execute binary file: Exec format error' FAIL_NOEXEC
assert_verdict 'unrecognised error' 1 'loader failed for an unrecognised reason' FAIL_OTHER
assert_verdict 'near-miss glibc line without closing parenthesis' 0 'ld.so (Ubuntu GLIBC 2.35-0ubuntu3.14' FAIL_OTHER

rendered_auxv="$(canary_render_failure \
    FAIL_AUXV \
    '13.5 (22G74)' \
    '2.2.0' \
    'iolbox-neg68' \
    '6.8.0-31-generic' \
    'aarch64' \
    'registered: enabled interpreter=/mnt/lima-rosetta/rosetta magic=02003e00' \
    '/lib64/ld-linux-x86-64.so.2' \
    'yes' \
    "$m0_auxv")"

assert_contains 'auxv message names macOS product/build' "$rendered_auxv" '13.5 (22G74)'
assert_contains 'auxv message names Lima version' "$rendered_auxv" '2.2.0'
assert_contains 'auxv message names guest kernel' "$rendered_auxv" '6.8.0-31-generic'
assert_contains 'auxv message names AT_RSEQ_ALIGN' "$rendered_auxv" 'AT_RSEQ_ALIGN'
assert_contains 'auxv remediation offers Jammy compatibility profile' "$rendered_auxv" 'jammy profile'
total=$((total + 1))
if [[ "$rendered_auxv" == *'brew reinstall lima'* && "$rendered_auxv" == *'Rosetta'* && "$rendered_auxv" == *'binfmt'* ]]; then
    printf 'ok - auxv remediation names Rosetta/binfmt repair and brew reinstall lima\n'
else
    printf 'FAIL - auxv remediation names Rosetta/binfmt repair and brew reinstall lima\n'
    failures=$((failures + 1))
fi
assert_contains 'auxv message gives re-run action' "$rendered_auxv" 'Re-run this canary'

printf 'Summary: %d cases, %d failures\n' "$total" "$failures"
[ "$failures" -eq 0 ]
