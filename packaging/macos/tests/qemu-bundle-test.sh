#!/usr/bin/env bash
# qemu-bundle-test.sh — exercise 10-multiarch-native.sh's bundled-package
# handling without a Lima guest.
#
# The real provisioning path only runs on an Apple Silicon Mac, so the parts
# that CAN be tested everywhere are tested here: payload extraction and the
# bundle verification gate. Those are the security-relevant halves -- the
# files this gate accepts are handed to dpkg and executed as root by package
# maintainer scripts, so "the manifest named it" must not be enough.
#
# dpkg itself is not exercised (no dpkg on a Mac/Windows dev host, and no
# arm64 Debian guest). See docs/macos-native-arm64-qemu-redistribution-plan.md
# for what still requires hardware validation.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"          # packaging/macos
GUEST="$ROOT/guest"

FAILURES=0
CASES=0

ok()   { CASES=$((CASES+1)); printf 'ok - %s\n' "$1"; }
fail() { CASES=$((CASES+1)); FAILURES=$((FAILURES+1)); printf 'FAIL - %s\n' "$1"; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Source the script under test without running main(). It guards main() behind
# a BASH_SOURCE/$0 comparison, so sourcing is safe.
# shellcheck source=../guest/10-multiarch-native.sh
. "$GUEST/10-multiarch-native.sh"

# --- build a synthetic bundle ------------------------------------------------
make_bundle() {
    local dir="$1"
    rm -rf "$dir"
    mkdir -p "$dir/arm64" "$dir/amd64"
    printf 'fake qemu-user payload\n'  > "$dir/arm64/qemu-user_1_arm64.deb"
    printf 'fake libc6 payload\n'      > "$dir/amd64/libc6_2_amd64.deb"
    {
        printf 'qemu-user|1|arm64|%s|qemu-user_1_arm64.deb\n' \
            "$(sha256sum "$dir/arm64/qemu-user_1_arm64.deb" | awk '{print $1}')"
        printf 'libc6|2|amd64|%s|libc6_2_amd64.deb\n' \
            "$(sha256sum "$dir/amd64/libc6_2_amd64.deb" | awk '{print $1}')"
    } > "$dir/MANIFEST"
}

# verify_bundle calls die(), which exits; run each case in a subshell.
expect_ok() {
    local label="$1" dir="$2"
    if ( verify_bundle "$dir" >/dev/null 2>&1 ); then
        ok "$label"
    else
        fail "$label (expected acceptance, got rejection)"
    fi
}

expect_reject() {
    local label="$1" dir="$2" pattern="$3" out rc
    out="$( ( verify_bundle "$dir" ) 2>&1 )" && rc=0 || rc=$?
    if [ "$rc" -eq 0 ]; then
        fail "$label (expected rejection, was accepted)"
        return
    fi
    if [ "$rc" -ne "$IOLBOX_EXIT_APT" ]; then
        fail "$label (expected exit $IOLBOX_EXIT_APT, got $rc)"
        return
    fi
    case "$out" in
        *"$pattern"*) ok "$label" ;;
        *) fail "$label (message did not mention '$pattern': $out)" ;;
    esac
}

B="$WORK/bundle"

make_bundle "$B"
expect_ok "a well-formed bundle is accepted" "$B"

make_bundle "$B"
rm -f "$B/MANIFEST"
expect_reject "a bundle with no MANIFEST is rejected" "$B" "MANIFEST missing"

make_bundle "$B"
printf 'tampered\n' > "$B/arm64/qemu-user_1_arm64.deb"
expect_reject "a tampered .deb is rejected on checksum" "$B" "sha256 mismatch"

make_bundle "$B"
printf 'unlisted\n' > "$B/amd64/evil_9_amd64.deb"
expect_reject "an unlisted extra .deb is rejected" "$B" "not listed in MANIFEST"

make_bundle "$B"
rm -f "$B/amd64/libc6_2_amd64.deb"
expect_reject "a MANIFEST row with no file is rejected" "$B" "missing or not a regular file"

make_bundle "$B"
head -1 "$B/MANIFEST" >> "$B/MANIFEST"
expect_reject "a duplicate MANIFEST row is rejected" "$B" "duplicate MANIFEST row"

make_bundle "$B"
printf 'evil|1|arm64|%064d|../../etc/passwd\n' 0 >> "$B/MANIFEST"
expect_reject "a traversing filename is rejected" "$B" "unsafe filename"

make_bundle "$B"
printf 'evil|1|arm64|deadbeef|x.deb\n' >> "$B/MANIFEST"
expect_reject "a malformed sha256 is rejected" "$B" "malformed sha256"

make_bundle "$B"
printf 'evil|1|riscv64|%064d|x.deb\n' 0 >> "$B/MANIFEST"
expect_reject "an unexpected architecture is rejected" "$B" "bad arch"

make_bundle "$B"
printf 'evil|1|arm64|%064d|notadeb.txt\n' 0 >> "$B/MANIFEST"
expect_reject "a non-.deb manifest entry is rejected" "$B" "not a .deb"

make_bundle "$B"
: > "$B/MANIFEST"
expect_reject "an empty MANIFEST is rejected" "$B" "listed no packages"

# --- extraction from a payload tarball ---------------------------------------
PAYLOAD_ROOT="$WORK/payload/iolbox-server-9.9.9-linux-arm64"
mkdir -p "$PAYLOAD_ROOT/guest-assets"
make_bundle "$PAYLOAD_ROOT/guest-assets/qemu-user"
mkdir -p "$PAYLOAD_ROOT/bin"
printf 'x\n' > "$PAYLOAD_ROOT/bin/supervisor"
TARBALL="$WORK/payload.tar.gz"
tar --create --gzip --file "$TARBALL" --directory "$WORK/payload" \
    "iolbox-server-9.9.9-linux-arm64"

# extract_bundle uses `tar --wildcards`, a GNU tar option. The guest is
# Debian, so that is correct for the code under test -- but this suite also
# runs on macOS dev hosts, where /usr/bin/tar is bsdtar and rejects the flag.
# Skip rather than report a product failure that is really a host difference;
# reporting it as FAIL on the very platform this feature targets trains people
# to ignore the suite.
TAR_HAS_WILDCARDS=1
tar --help 2>&1 | grep -q -- '--wildcards' || TAR_HAS_WILDCARDS=0

DEST="$WORK/extract"
mkdir -p "$DEST"
if [ "$TAR_HAS_WILDCARDS" -eq 0 ]; then
    ok "SKIP payload extraction (host tar is not GNU tar; the guest's is)"
    ok "SKIP extracted-bundle verification (host tar is not GNU tar)"
elif ( IOLBOX_PAYLOAD_TARBALL="$TARBALL" extract_bundle "$DEST" >/dev/null 2>&1 ) \
     && [ -f "$DEST/guest-assets/qemu-user/MANIFEST" ]; then
    ok "the bundle subtree is extracted from a versioned payload tarball"
    # The extracted bundle must still pass verification end to end.
    expect_ok "the extracted bundle verifies" "$DEST/guest-assets/qemu-user"
else
    fail "the bundle subtree is extracted from a versioned payload tarball"
fi

DEST2="$WORK/extract2"; mkdir -p "$DEST2"
out="$( ( IOLBOX_PAYLOAD_TARBALL="$WORK/nope.tar.gz" extract_bundle "$DEST2" ) 2>&1 )" \
    && rc=0 || rc=$?
if [ "$rc" -eq "$IOLBOX_EXIT_PREFLIGHT" ]; then
    ok "a missing payload tarball is a preflight failure"
else
    fail "a missing payload tarball is a preflight failure (got exit $rc: $out)"
fi

DEST3="$WORK/extract3"; mkdir -p "$DEST3"
mkdir -p "$WORK/empty/iolbox-server-9.9.9-linux-arm64"
printf 'x\n' > "$WORK/empty/iolbox-server-9.9.9-linux-arm64/README.txt"
tar --create --gzip --file "$WORK/empty.tar.gz" --directory "$WORK/empty" \
    "iolbox-server-9.9.9-linux-arm64"
out="$( ( IOLBOX_PAYLOAD_TARBALL="$WORK/empty.tar.gz" extract_bundle "$DEST3" ) 2>&1 )" \
    && rc=0 || rc=$?
if [ "$rc" -eq "$IOLBOX_EXIT_APT" ]; then
    ok "a payload with no bundle is rejected, not silently ignored"
else
    fail "a payload with no bundle is rejected (got exit $rc: $out)"
fi

# --- the offline guarantee is structural, not aspirational -------------------
# These assert that a future edit cannot quietly reintroduce a network
# fallback, which would defeat the whole point of bundling. Comments are
# stripped first: the script legitimately DISCUSSES apt-get and
# --force-depends in explaining why it does not use them, and an assertion
# that forbids naming a thing is an assertion people route around.
CODE="$WORK/native-code.sh"
sed 's/[[:space:]]*#.*$//' "$GUEST/10-multiarch-native.sh" > "$CODE"

if grep -qE '(^|[^[:alnum:]_-])apt-get([^[:alnum:]_-]|$)' "$CODE"; then
    fail "10-multiarch-native.sh must contain no apt-get invocation"
else
    ok "10-multiarch-native.sh contains no apt-get invocation"
fi

if grep -q -- '--force-depends' "$CODE"; then
    fail "10-multiarch-native.sh must not use dpkg --force-depends"
else
    ok "10-multiarch-native.sh does not use dpkg --force-depends"
fi

# The bundle must be installed from local files only: no http(s) URL should
# appear in the provisioning code path at all.
if grep -qE 'https?://' "$CODE"; then
    fail "10-multiarch-native.sh must not reference any URL in code"
else
    ok "10-multiarch-native.sh references no URL in code"
fi

# --- the multiarch preflight must arch-qualify its dpkg-query ----------------
# On a provisioning RE-RUN the amd64 instances are already installed, so an
# unqualified `dpkg-query -W -f='${Version}' libc6` returns one record per
# architecture -- concatenated, because the format has no newline. That made
# check_multiarch_versions compare "2.41-12+deb13u32.41-12+deb13u3" against
# "2.41-12+deb13u3" and abort provisioning with a drift error that had not
# happened. Reproduced on real hardware. The stubs below model dpkg's actual
# behaviour so this cannot regress off-hardware.
MA="$WORK/multiarch"
rm -rf "$MA"; mkdir -p "$MA/arm64" "$MA/amd64"
printf 'x\n' > "$MA/amd64/libc6_1_amd64.deb"
printf 'libc6|2.41|amd64|%s|libc6_1_amd64.deb\n' \
    "$(sha256sum "$MA/amd64/libc6_1_amd64.deb" | awk '{print $1}')" > "$MA/MANIFEST"

dpkg() { [ "${1:-}" = "--print-architecture" ] && printf 'arm64\n'; }

# Both instances installed at the SAME (correct) version -- the re-run case.
dpkg-query() {
    local fmt="" name=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -f=*) fmt="${1#-f=}" ;;
            -W) : ;;
            *) name="$1" ;;
        esac
        shift
    done
    local out=""
    case "$name" in
        libc6)       out='install ok installed|2.41|install ok installed|2.41' ;;
        libc6:arm64) out='install ok installed|2.41' ;;
        libc6:amd64) out='install ok installed|2.41' ;;
        *) return 1 ;;
    esac
    case "$fmt" in
        '${Status}|${Version}') printf '%s' "$out" ;;
        '${Version}')           printf '%s' "$(printf '%s' "$out" | sed 's/install ok installed|//g')" ;;
        *)                      printf '%s' "$out" ;;
    esac
}

if ( check_multiarch_versions "$MA" >/dev/null 2>&1 ); then
    ok "the multiarch preflight accepts a re-run with both architectures installed"
else
    fail "the multiarch preflight rejects a re-run (unqualified dpkg-query regression)"
fi

# Genuine drift must still be caught: native arm64 at a different version.
dpkg-query() {
    local fmt="" name=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -f=*) fmt="${1#-f=}" ;;
            -W) : ;;
            *) name="$1" ;;
        esac
        shift
    done
    case "$name" in
        libc6:arm64) printf 'install ok installed|2.42' ;;
        libc6:amd64) printf 'install ok installed|2.41' ;;
        *) return 1 ;;
    esac
}
out="$( ( check_multiarch_versions "$MA" ) 2>&1 )" && rc=0 || rc=$?
if [ "$rc" -eq "$IOLBOX_EXIT_APT" ] && case "$out" in *"multiarch assertion"*) true ;; *) false ;; esac; then
    ok "the multiarch preflight still catches genuine image/lock drift"
else
    fail "the multiarch preflight must catch genuine drift (rc=$rc: $out)"
fi

# A config-files-only remnant carries a Version but must not constrain us.
dpkg-query() {
    local fmt="" name=""
    while [ $# -gt 0 ]; do
        case "$1" in -f=*) fmt="${1#-f=}" ;; -W) : ;; *) name="$1" ;; esac
        shift
    done
    case "$name" in
        libc6:arm64) printf 'deinstall ok config-files|2.42' ;;
        *) return 1 ;;
    esac
}
if ( check_multiarch_versions "$MA" >/dev/null 2>&1 ); then
    ok "a config-files-only native remnant does not trip the multiarch preflight"
else
    fail "a config-files-only native remnant must not trip the multiarch preflight"
fi

unset -f dpkg dpkg-query

# --- the bundle gate must not depend on a filesystem executable bit ----------
# Every script under packaging/macos is committed mode 0644 and invoked as
# `bash <script>`. pack-native.sh's --bundle-guest-qemu preflight originally
# tested `-x` on fetch-qemu-user.sh, so the release build aborted before
# fetching anything on any checkout without the bit -- which is every fresh
# actions/checkout. Assert the guard stays mode-independent.
PACK_NATIVE="$(cd "$ROOT/../.." && pwd)/runtime/pack-native.sh"
if [ -f "$PACK_NATIVE" ]; then
    if grep -qE '\[ ! -x "\$GUEST_ASSETS_SRC/fetch-qemu-user\.sh" \]' "$PACK_NATIVE"; then
        fail "pack-native.sh's bundle guard must not test the executable bit"
    else
        ok "pack-native.sh's bundle guard does not test the executable bit"
    fi

    # Functional, not just textual: lift the guard's own condition out of
    # pack-native.sh and evaluate it against a real mode-0644 copy. Running
    # pack-native.sh itself would not reach this line (--validate-only exits
    # earlier, and a full run needs go plus a 67 MiB download), so evaluating
    # the committed expression is the strongest check available offline.
    GA="$WORK/guest-assets"
    mkdir -p "$GA"
    cp "$ROOT/guest-assets/fetch-qemu-user.sh" "$GA/fetch-qemu-user.sh"
    chmod 0644 "$GA/fetch-qemu-user.sh"
    GUARD="$(grep -F 'GUEST_ASSETS_SRC/fetch-qemu-user.sh" ]; then' "$PACK_NATIVE" \
             | head -1 | sed -e 's/^[[:space:]]*//' -e 's/^if //' -e 's/; then$//')"
    if [ -z "$GUARD" ]; then
        fail "could not locate pack-native.sh's bundle guard condition"
    elif [ -x "$GA/fetch-qemu-user.sh" ]; then
        # Some filesystems (Git-for-Windows, exFAT) cannot drop the bit, so
        # the case this regression is about is not reproducible here.
        ok "SKIP mode-0644 guard check (filesystem does not honour chmod)"
    elif ( GUEST_ASSETS_SRC="$GA"; eval "$GUARD" ); then
        fail "a mode-0644 fetch-qemu-user.sh must not fail the bundle guard"
    else
        ok "a mode-0644 fetch-qemu-user.sh passes the bundle guard"
    fi
else
    fail "pack-native.sh not found at $PACK_NATIVE"
fi

printf '\nSummary: %d cases, %d failures\n' "$CASES" "$FAILURES"
[ "$FAILURES" -eq 0 ]
