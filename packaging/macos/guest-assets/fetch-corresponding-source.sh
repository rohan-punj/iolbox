#!/usr/bin/env bash
# fetch-corresponding-source.sh — assemble the complete corresponding source
# for every GPL/LGPL-covered Debian package the macOS archive redistributes.
#
# WHY THIS EXISTS
# ---------------
# Once iolbox ships the binaries, iolbox owes the source. GPLv2 §3 and GPLv3
# §6 both let a distributor discharge that by ACCOMPANYING the binaries with
# the corresponding source; the "written offer" alternatives carry extra
# conditions (GPLv2 §3(b) requires a three-year offer valid to any third
# party; §3(c)'s pass-through is limited to noncommercial redistribution).
# Merely linking to Debian is the weakest of the available options and is not
# obviously sufficient on its own.
#
# So iolbox publishes the source itself, from the same place as the binary:
# this script produces iolbox-macos-arm64-corresponding-source.tar.gz, which
# is uploaded as a release asset alongside the archive. The source then
# travels with the binary from the same distribution point, and the offer in
# SOURCE-OFFER.txt points at something iolbox actually controls.
#
# The source is Debian's, unmodified, exactly as the binaries are.
#
# Usage:
#   fetch-corresponding-source.sh --out DIR [--archive PATH] [--lockfile PATH]
#
# This is a RELEASE step, not part of the archive build: the source bundle is
# a separate, much larger asset and must not be embedded in the archive users
# download to run iolbox.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCKFILE="${IOLBOX_QEMU_LOCKFILE:-$SCRIPT_DIR/qemu-user.lock}"
LICENSES_LOCK="${IOLBOX_LICENSES_LOCK:-$SCRIPT_DIR/licenses.lock}"
SNAPSHOT_TIMESTAMP="${IOLBOX_DEBIAN_SNAPSHOT:-20260810T000000Z}"
PRIMARY_BASE="${IOLBOX_DEBIAN_MIRROR:-https://deb.debian.org/debian}"
SNAPSHOT_BASE="https://snapshot.debian.org/archive/debian/$SNAPSHOT_TIMESTAMP"

OUT_DIR=""
ARCHIVE=""

usage() {
    cat <<EOF
Usage: $0 --out DIR [--archive PATH]

  --out DIR       Directory to assemble the source tree into.
  --archive PATH  Also produce a .tar.gz at PATH.
  --lockfile PATH Override the binary lockfile (default: $LOCKFILE)
  -h, --help      This help.

Downloads the .dsc for every source package behind the redistributed
binaries, then every component file the .dsc lists, verifying each against
the checksums in the .dsc itself.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT_DIR="$2"; shift 2 ;;
        --archive) ARCHIVE="$2"; shift 2 ;;
        --lockfile) LOCKFILE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "fetch-corresponding-source: unknown option: $1" >&2; usage >&2; exit 1 ;;
    esac
done

[ -n "$OUT_DIR" ] || { echo "fetch-corresponding-source: --out is required" >&2; exit 1; }
for f in "$LOCKFILE" "$LICENSES_LOCK"; do
    [ -f "$f" ] || { echo "fetch-corresponding-source: lockfile not found: $f" >&2; exit 1; }
done
for tool in curl sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "fetch-corresponding-source: '$tool' is required" >&2; exit 1; }
done

mkdir -p "$OUT_DIR"

fetch_verified() {
    local rel="$1" expected="$2" dest="$3" url actual
    for url in "$PRIMARY_BASE/$rel" "$SNAPSHOT_BASE/$rel"; do
        if ! curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 30 \
                 --max-time 1800 -o "$dest.tmp" "$url"; then
            rm -f -- "$dest.tmp"; continue
        fi
        if [ -n "$expected" ]; then
            actual="$(sha256sum "$dest.tmp" | awk '{print $1}')"
            if [ "$actual" != "$expected" ]; then
                rm -f -- "$dest.tmp"
                echo "fetch-corresponding-source: checksum mismatch for $rel" >&2
                echo "  expected: $expected" >&2
                echo "  actual:   $actual" >&2
                exit 1
            fi
        fi
        mv -f -- "$dest.tmp" "$dest"
        return 0
    done
    echo "fetch-corresponding-source: could not fetch $rel" >&2
    return 1
}

# The source package and version for each shipped binary come from
# sources.lock, which generate-lock.sh derived from Debian's OpenPGP-verified
# binary index.
#
# DO NOT be tempted to list the pool directory and pick a .dsc from it. A pool
# directory holds many versions simultaneously, so that approach silently
# fetches the wrong source -- during development it produced bookworm's qemu
# 7.2 source to accompany a trixie qemu 10.0.11 binary, which is a compliance
# failure rather than a cosmetic one. The lock is the authority.
SOURCES_LOCK="${IOLBOX_SOURCES_LOCK:-$SCRIPT_DIR/sources.lock}"
[ -f "$SOURCES_LOCK" ] || {
    echo "fetch-corresponding-source: sources lockfile not found: $SOURCES_LOCK" >&2
    echo "  Regenerate it with packaging/macos/guest-assets/generate-lock.sh" >&2
    exit 1; }

echo "== fetch-corresponding-source: resolving source packages from sources.lock =="

count=0
while IFS='|' read -r src srcver dsc_path || [ -n "${src:-}" ]; do
    case "${src:-}" in ''|'#'*) continue ;; esac
    [ -n "$srcver" ] && [ -n "$dsc_path" ] || {
        echo "fetch-corresponding-source: malformed sources.lock row: $src" >&2; exit 1; }
    case "$dsc_path" in
        pool/*) ;;
        *) echo "fetch-corresponding-source: unsafe dsc path: $dsc_path" >&2; exit 1 ;;
    esac
    case "$dsc_path" in
        *..*) echo "fetch-corresponding-source: dsc path contains '..': $dsc_path" >&2; exit 1 ;;
    esac

    dir="$(dirname "$dsc_path")"
    dsc="$(basename "$dsc_path")"

    install -d -m 0755 "$OUT_DIR/$src"
    echo "   $src $srcver -> $dsc"
    fetch_verified "$dsc_path" "" "$OUT_DIR/$src/$dsc"

    # Cross-check that the .dsc we got really is the version the lock names,
    # so a mirror serving something else cannot go unnoticed.
    got_ver="$(awk '/^Version:/{print $2; exit}' "$OUT_DIR/$src/$dsc")"
    if [ "$got_ver" != "$srcver" ]; then
        echo "fetch-corresponding-source: $src .dsc declares version '$got_ver'," >&2
        echo "  but sources.lock pins '$srcver'" >&2
        exit 1
    fi

    # The .dsc lists its component files under Checksums-Sha256. Those
    # checksums are the authority for the tarballs, and the .dsc itself is
    # OpenPGP-signed by the Debian maintainer.
    awk '/^Checksums-Sha256:/{f=1;next} /^[^ ]/{f=0} f && NF==3 {print $1" "$3}' \
        "$OUT_DIR/$src/$dsc" | while read -r sha name; do
        [ -n "$name" ] || continue
        case "$name" in */*|*..*) echo "unsafe component name: $name" >&2; exit 1 ;; esac
        fetch_verified "$dir/$name" "$sha" "$OUT_DIR/$src/$name"
        echo "      $name"
    done
    count=$((count + 1))
done < "$SOURCES_LOCK"

install -m 0644 "$SCRIPT_DIR/SOURCE-OFFER.txt" "$OUT_DIR/SOURCE-OFFER.txt"

echo "== fetch-corresponding-source: assembled $count source packages =="
du -sh "$OUT_DIR" 2>/dev/null || true

if [ -n "$ARCHIVE" ]; then
    install -d -m 0755 "$(dirname "$ARCHIVE")"
    tar --create --gzip --file "$ARCHIVE" \
        --directory "$(dirname "$OUT_DIR")" "$(basename "$OUT_DIR")"
    echo "== fetch-corresponding-source: wrote $ARCHIVE =="
    ls -lh "$ARCHIVE"
fi
