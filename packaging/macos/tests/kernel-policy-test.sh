#!/usr/bin/env bash
# kernel-policy-test.sh - offline fixtures for D7/D8 and reproducibility text.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
guest_dir="$repo_root/packaging/macos/guest"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-kernel-policy.XXXXXX")"
trap 'rm -rf -- "$tmp_dir"' EXIT

total=0
failures=0
ok() { total=$((total + 1)); printf 'ok - %s\n' "$1"; }
fail_case() { total=$((total + 1)); failures=$((failures + 1)); printf 'FAIL - %s\n' "$1"; }

require_file() {
    [ -r "$1" ] || {
        printf 'ERROR: required test target is missing or unreadable: %s\n' "$1" >&2
        exit 1
    }
}

hash_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

large_holds() {
    printf '%s\n' "$TEST_HELD_PACKAGE"
    i=0
    while [ "$i" -lt 20000 ]; do
        printf 'linux-image-fixture-%05d\n' "$i"
        i=$((i + 1))
    done
}

run_large_hold_fixture() {
    local script="$1" label="$2"

    require_file "$script"
    # shellcheck source=../guest/20-kernel-hold.sh
    if ! . "$script"; then
        printf 'ERROR: could not source kernel policy: %s\n' "$script" >&2
        return 1
    fi
    TEST_HELD_PACKAGE='linux-image-fixture-target'
    get_kernel_packages() { printf '%s\n' "$TEST_HELD_PACKAGE"; }
    apt-mark() {
        if [ "${1:-}" = showhold ]; then
            large_holds
        else
            return 0
        fi
    }
    if (assert_holds); then
        ok "$label recognizes a held package in a large apt-mark showhold result"
    else
        fail_case "$label recognizes a held package in a large apt-mark showhold result"
    fi
}

run_large_hold_fixture "$guest_dir/20-kernel-hold.sh" Ubuntu
run_large_hold_fixture "$guest_dir/20-kernel-hold-debian.sh" Debian

# Hash the real policy writer twice with the same qualification facts. A
# policy that embeds a fresh date on every invocation is not reproducible.
policy_dir="$tmp_dir/policy"
mkdir -p "$policy_dir"
export IOLBOX_POLICY_FILE="$policy_dir/macos-guest-policy"
export IOLBOX_PROVISION_DATE='2026-08-14T00:00:00Z'
export IOLBOX_PROFILE='debian13'
export IOLBOX_PROFILE_STATUS='DEFAULT'
export IOLBOX_HOST_MACOS='26.6.1 (25G76)'
export IOLBOX_HOST_LIMA='2.2.0'
export IOLBOX_MACHINE='fixture-trixie'
export IOLBOX_KERNEL_SERIES='6.12'
export IOLBOX_EXPECTED_UNAME_R='6.12.101+deb13-cloud-arm64'
export IOLBOX_IMAGE_QUALIFICATION='pinned Debian 13 trixie image'

# shellcheck source=../guest/20-kernel-hold-debian.sh
require_file "$guest_dir/20-kernel-hold-debian.sh"
if ! . "$guest_dir/20-kernel-hold-debian.sh"; then
    printf 'ERROR: could not source Debian kernel policy: %s\n' "$guest_dir/20-kernel-hold-debian.sh" >&2
    exit 1
fi
debian_codename() { printf '%s\n' trixie; }
IOLBOX_POLICY_FILE="$policy_dir/macos-guest-policy"
export IOLBOX_POLICY_FILE
get_kernel_packages() {
    printf '%s\n' linux-image-cloud-arm64 linux-headers-arm64
}
if ! write_policy_file; then
    printf 'ERROR: kernel policy setup failed while writing %s\n' "$IOLBOX_POLICY_FILE" >&2
    exit 1
fi
policy_hash_one="$(hash_file "$IOLBOX_POLICY_FILE")"
if ! write_policy_file; then
    printf 'ERROR: kernel policy setup failed while rewriting %s\n' "$IOLBOX_POLICY_FILE" >&2
    exit 1
fi
policy_hash_two="$(hash_file "$IOLBOX_POLICY_FILE")"
if [ "$policy_hash_one" = "$policy_hash_two" ]; then
    ok 'kernel policy output hashes identically on repeated writes'
else
    fail_case 'kernel policy output hashes identically on repeated writes'
fi

for required in \
    'purpose=reproducibility' \
    'canary_is_authority=true' \
    'profile=debian13' \
    'qualified_kernel_series=6.12' \
    'held_kernel_packages=linux-image-cloud-arm64 linux-headers-arm64' \
    'security_update_tradeoff=' \
    'deliberate_requalification='; do
    if grep -Fq -- "$required" "$IOLBOX_POLICY_FILE"; then
        ok "policy records ${required%%=*}"
    else
        fail_case "policy records ${required%%=*}"
    fi
done

# The frozen policy is reproducibility language, not the obsolete universal
# Rosetta-kernel cutoff. Keep this assertion in the offline suite so old text
# cannot quietly return.
if ! grep -Eiq 'Linux[[:space:]]*>=[[:space:]]*6\.3|macOS version that fixes this is UNVERIFIED' "$IOLBOX_POLICY_FILE"; then
    ok 'policy does not claim a universal Linux 6.3/macOS fix-point refusal'
else
    fail_case 'policy does not claim a universal Linux 6.3/macOS fix-point refusal'
fi

printf 'Summary: %d cases, %d failures\n' "$total" "$failures"
[ "$failures" -eq 0 ]
