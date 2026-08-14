#!/usr/bin/env bash
# source-policy-test.sh - offline fixtures for Ubuntu/Debian multiarch policy.
#
# The fixtures call the shipped helper functions with temporary files and stub
# apt commands. No guest, network, or Lima installation is needed.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
guest_dir="$repo_root/packaging/macos/guest"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-source-policy.XXXXXX")"
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

ubuntu_sources="$tmp_dir/ubuntu.sources.list"
ubuntu_sources_dir="$tmp_dir/ubuntu.sources.d"
mkdir -p "$ubuntu_sources_dir"
cat > "$ubuntu_sources" <<'FIXTURE'
# hostile cloud-init source fixture
deb [arch=amd64 signed-by=/x.gpg] http://ports.ubuntu.com/ubuntu-ports/ jammy main
deb [arch=arm64,amd64] http://ports.ubuntu.com/ubuntu-ports/ jammy-updates main
deb http://ports.ubuntu.com/ubuntu-ports/ jammy-security main
deb-src [signed-by=/x.gpg arch=i386] http://ports.ubuntu.com/ubuntu-ports/ jammy main
FIXTURE

SOURCES_LIST="$ubuntu_sources"
SOURCES_LIST_DIR="$ubuntu_sources_dir"
AMD64_SOURCES_LIST="$ubuntu_sources_dir/iolbox-amd64.list"
# shellcheck source=../guest/10-multiarch.sh
require_file "$guest_dir/10-multiarch.sh"
if ! . "$guest_dir/10-multiarch.sh"; then
    printf 'ERROR: could not source guest multiarch policy: %s\n' "$guest_dir/10-multiarch.sh" >&2
    exit 1
fi

if ! pin_existing_sources; then
    printf 'ERROR: Ubuntu source-policy setup failed while repairing fixture sources\n' >&2
    exit 1
fi

if awk '
    /^[[:space:]]*deb(-src)?[[:space:]]/ {
        if ($0 !~ /arch=arm64/ || $0 ~ /arch=amd64/ || $0 ~ /arch=arm64,amd64/ || $0 ~ /arch=i386/) exit 1
        if ($0 ~ /ports\.ubuntu\.com/ && $0 !~ /arch=arm64/) exit 1
    }
    END { exit 0 }
' "$ubuntu_sources"; then
    if grep -Fq 'signed-by=/x.gpg' "$ubuntu_sources"; then
        ok 'Ubuntu hostile source entries are repaired to arm64 and preserve signed-by'
    else
        fail_case 'Ubuntu hostile source entries preserve signed-by'
    fi
else
    fail_case 'Ubuntu hostile source entries are repaired to exactly arm64'
fi

ubuntu_hash_before="$(hash_file "$ubuntu_sources")"
pin_existing_sources
ubuntu_hash_after="$(hash_file "$ubuntu_sources")"
if [ "$ubuntu_hash_before" = "$ubuntu_hash_after" ]; then
    ok 'Ubuntu source repair is byte-idempotent on the second run'
else
    fail_case 'Ubuntu source repair is byte-idempotent on the second run'
fi

if ! write_amd64_sources jammy; then
    printf 'ERROR: Ubuntu source-policy setup failed while writing managed amd64 sources\n' >&2
    exit 1
fi
if awk '
    /ports\.ubuntu\.com/ && /arch=amd64/ { exit 1 }
    /archive\.ubuntu\.com|security\.ubuntu\.com/ && /arch=amd64/ { found++ }
    END { exit(found == 3 ? 0 : 1) }
' "$AMD64_SOURCES_LIST"; then
    ok 'managed amd64 sources use archive/security hosts and never ports.ubuntu.com'
else
    fail_case 'managed amd64 source policy is exact'
fi

# The Debian package mapping is exercised by running the real provisioning
# function with apt/dpkg stubs. This catches a comment-only mapping: the
# command line must select libssl3 for bookworm and libssl3t64 for trixie.
apt_bin="$tmp_dir/bin"
mkdir -p "$apt_bin"
cat > "$apt_bin/dpkg" <<'STUB'
#!/usr/bin/env bash
if [ "${1:-}" = '--add-architecture' ]; then exit 0; fi
exit 0
STUB
cat > "$apt_bin/apt-get" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${IOLBOX_TEST_APT_LOG:?}"
exit 0
STUB
chmod 0755 "$apt_bin/dpkg" "$apt_bin/apt-get"

run_debian_package_probe() {
    local script="$1" suite_name="$2" expected="$3" unexpected="$4"
    local log_file="$tmp_dir/apt-$suite_name.log"

    : > "$log_file"
    export IOLBOX_TEST_APT_LOG="$log_file"
    export PATH="$apt_bin:$PATH"
    export IOLBOX_LOADER=/bin/true
    # The package mapping is the subject of this fixture; avoid making the
    # offline test depend on a host dpkg database or ELF files.
    require_file "$script"
    # shellcheck source=../guest/10-multiarch-debian.sh
    if ! . "$script"; then
        printf 'ERROR: could not source Debian multiarch policy: %s\n' "$script" >&2
        return 1
    fi
    guest_codename() { printf '%s\n' "$suite_name"; }
    prepare_sources() { :; }
    verify_runtime() { :; }
    if ! run_provision; then
        printf 'ERROR: Debian package mapping setup failed for %s\n' "$suite_name" >&2
        return 1
    fi
    if grep -Fq -- "$expected" "$log_file" && ! grep -Fq -- "$unexpected" "$log_file"; then
        ok "$suite_name selects $expected and not $unexpected"
    else
        fail_case "$suite_name package mapping ($expected vs $unexpected)"
    fi
}

run_debian_package_probe "$guest_dir/10-multiarch-debian.sh" bookworm \
    'libssl3:amd64' 'libssl3t64:amd64'
run_debian_package_probe "$guest_dir/10-multiarch-debian.sh" trixie \
    'libssl3t64:amd64' 'libssl3:amd64'

for fact in macos_product macos_build; do
    if grep -Fq -- "$fact" "$guest_dir/30-canary.sh"; then
        ok "canary record includes $fact"
    else
        fail_case "canary record includes $fact"
    fi
done

printf 'Summary: %d cases, %d failures\n' "$total" "$failures"
[ "$failures" -eq 0 ]
