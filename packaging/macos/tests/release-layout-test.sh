#!/usr/bin/env bash
# Verify the exact M6 archive contract and rebuild it twice from independent
# packer staging directories to prove deterministic bytes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MACOS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$MACOS_DIR/../.." && pwd)"
PACKER="$MACOS_DIR/pack-release.sh"

ARCHIVE=""
LAUNCHER=""
PAYLOAD=""
PAYLOAD_SHA256=""
VERSION=""
SOURCE_DATE_EPOCH=""

usage() {
    cat <<'EOF'
Usage: packaging/macos/tests/release-layout-test.sh \
  --archive PATH --launcher PATH --payload PATH --payload-sha256 SHA256 \
  --version VERSION --source-date-epoch EPOCH
EOF
}
die() { echo "release-layout-test: $*" >&2; exit 1; }
while [ "$#" -gt 0 ]; do
    case "$1" in
        --archive) ARCHIVE="$2"; shift 2 ;;
        --launcher) LAUNCHER="$2"; shift 2 ;;
        --payload) PAYLOAD="$2"; shift 2 ;;
        --payload-sha256) PAYLOAD_SHA256="$2"; shift 2 ;;
        --version) VERSION="$2"; shift 2 ;;
        --source-date-epoch) SOURCE_DATE_EPOCH="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
done
for required in ARCHIVE LAUNCHER PAYLOAD PAYLOAD_SHA256 VERSION SOURCE_DATE_EPOCH; do
    [ -n "${!required}" ] || die "--$(echo "$required" | tr '[:upper:]' '[:lower:]' | tr '_' '-') is required"
done
[ -f "$ARCHIVE" ] || die "archive does not exist: $ARCHIVE"
[ "$(basename "$ARCHIVE")" = "iolbox-macos-arm64.tar.gz" ] || die "archive basename is not iolbox-macos-arm64.tar.gz"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-macos-layout.XXXXXX")"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

tar --version | grep -F 'GNU tar' >/dev/null || die "GNU tar is required"
archive_dir="$(cd "$(dirname "$ARCHIVE")" && pwd)"
archive_name="$(basename "$ARCHIVE")"
( cd "$archive_dir" && sha256sum -c "$archive_name.sha256" ) >/dev/null || die "outer checksum beside archive does not verify"

tar -tzf "$ARCHIVE" > "$TMP/raw-list.txt"
sed 's#/$##' "$TMP/raw-list.txt" | LC_ALL=C sort > "$TMP/actual-list.txt"
cat > "$TMP/expected-list.txt" <<EOF
iolbox-macos-arm64
iolbox-macos-arm64/LICENSE
iolbox-macos-arm64/README.md
iolbox-macos-arm64/SHA256SUMS
iolbox-macos-arm64/guest
iolbox-macos-arm64/guest/10-multiarch-debian.sh
iolbox-macos-arm64/guest/10-multiarch.sh
iolbox-macos-arm64/guest/20-kernel-hold-debian.sh
iolbox-macos-arm64/guest/20-kernel-hold.sh
iolbox-macos-arm64/guest/30-canary.sh
iolbox-macos-arm64/guest/40-install-payload.sh
iolbox-macos-arm64/guest/50-verify.sh
iolbox-macos-arm64/guest/lib.sh
iolbox-macos-arm64/iolbox
iolbox-macos-arm64/iolbox-server-${VERSION}.tar.gz
iolbox-macos-arm64/lima
iolbox-macos-arm64/lima/iolbox-bookworm.yaml
iolbox-macos-arm64/lima/iolbox-jammy.yaml
iolbox-macos-arm64/lima/iolbox-trixie.yaml
iolbox-macos-arm64/lima/pinned-image-debian12.env
iolbox-macos-arm64/lima/pinned-image-debian13.env
iolbox-macos-arm64/lima/pinned-image.env
iolbox-macos-arm64/lima/profiles.env
iolbox-macos-arm64/notices
iolbox-macos-arm64/notices/THIRD_PARTY.md
EOF
if ! cmp -s "$TMP/actual-list.txt" "$TMP/expected-list.txt"; then
    echo "expected archive members:" >&2; cat "$TMP/expected-list.txt" >&2
    echo "actual archive members:" >&2; cat "$TMP/actual-list.txt" >&2
    die "archive member set is not exact"
fi

# The first character in GNU tar's verbose type flags must be directory or
# regular file. This rejects symlinks, hard links, devices, FIFOs, and other
# special nodes even if their names happen to be allowed.
tar -tvzf "$ARCHIVE" > "$TMP/verbose-list.txt"
while IFS= read -r line; do
    [ -n "$line" ] || continue
    type="${line:0:1}"
    case "$type" in
        d|-) ;;
        *) die "archive contains a non-directory/non-regular member: $line" ;;
    esac
done < "$TMP/verbose-list.txt"
if LC_ALL=C grep -aE -i 'SCHILY\.(xattr|acl)|LIBARCHIVE\.(xattr|acl)|GNU\.sparse|com\.apple\.(ResourceFork|FinderInfo)|PaxHeader' "$ARCHIVE" >/dev/null; then
    die "archive contains xattr/ACL/resource-fork pax metadata"
fi
if LC_ALL=C grep -E -i '(^|/)(\._[^/]*|\.DS_Store|.*namedfork.*|.*resourcefork.*|.*cisco.*|.*\.i86bi[^/]*|.*\.iol[^/]*)$|(^|/)tests(/|$)|iolbox-mac\.sh|pack-release\.sh|release-manifest\.txt' "$TMP/actual-list.txt" >/dev/null; then
    die "archive contains a forbidden AppleDouble/resource/Cisco/test member"
fi

mkdir "$TMP/extract"
tar -xzf "$ARCHIVE" -C "$TMP/extract"
ROOT="$TMP/extract/iolbox-macos-arm64"
[ "$(stat -c '%a' "$ROOT")" = 755 ] || die "archive root mode is not 0755"
while IFS= read -r path; do
    [ -n "$path" ] || continue
    full="$TMP/extract/$path"
    [ -e "$full" ] || die "listed member did not extract: $path"
    if [ -d "$full" ]; then
        [ "$(stat -c '%a' "$full")" = 755 ] || die "directory mode is not 0755: $path"
    else
        expected_mode=644
        [ "$path" = "iolbox-macos-arm64/iolbox" ] && expected_mode=755
        [ "$(stat -c '%a' "$full")" = "$expected_mode" ] || die "file mode is not $expected_mode: $path"
    fi
done < "$TMP/actual-list.txt"

( cd "$ROOT" && sha256sum -c SHA256SUMS ) || die "internal checksums do not verify"
[ "$(wc -l < "$ROOT/SHA256SUMS" | tr -d ' ')" = 20 ] || die "internal checksum file does not cover exactly 20 files"

file_launcher="$(file -b "$ROOT/iolbox")"
case "$file_launcher" in
    *"Mach-O 64-bit"*"arm64"*) ;;
    *) die "extracted launcher is not Mach-O arm64: $file_launcher" ;;
esac
payload_path="$ROOT/iolbox-server-${VERSION}.tar.gz"
[ -f "$payload_path" ] || die "expected native payload is missing"
[ "$(sha256sum "$payload_path" | awk '{print tolower($1)}')" = "$(printf '%s' "$PAYLOAD_SHA256" | tr '[:upper:]' '[:lower:]')" ] || die "extracted payload is not the trusted build-linux payload"
[ "$(find "$ROOT" -maxdepth 1 -type f -name 'iolbox-server-*.tar.gz' | wc -l | tr -d ' ')" = 1 ] || die "archive does not contain exactly one native payload"

while IFS='|' read -r source dest extra || [ -n "${source:-}" ]; do
    case "${source:-}" in ""|\#*) continue ;; esac
    [ -f "$REPO_ROOT/$source" ] && [ ! -L "$REPO_ROOT/$source" ] || die "manifest source is not regular/non-symlink: $source"
done < "$MACOS_DIR/release-manifest.txt"

# Re-run the packer twice into fresh output paths. The packer itself creates
# two independent stage directories for each invocation; comparing both fresh
# outputs prevents a single lingering stage from masking nondeterminism.
for n in 1 2; do
    mkdir "$TMP/repro-$n"
    bash "$PACKER" --launcher "$LAUNCHER" --payload "$PAYLOAD" \
        --payload-sha256 "$PAYLOAD_SHA256" --version "$VERSION" \
        --output "$TMP/repro-$n/iolbox-macos-arm64.tar.gz" \
        --source-date-epoch "$SOURCE_DATE_EPOCH" > "$TMP/repro-$n.log"
done
cmp -s "$TMP/repro-1/iolbox-macos-arm64.tar.gz" "$TMP/repro-2/iolbox-macos-arm64.tar.gz" || die "independent reproducibility outputs differ"
repro_hash_1="$(sha256sum "$TMP/repro-1/iolbox-macos-arm64.tar.gz" | awk '{print $1}')"
repro_hash_2="$(sha256sum "$TMP/repro-2/iolbox-macos-arm64.tar.gz" | awk '{print $1}')"
archive_hash="$(sha256sum "$ARCHIVE" | awk '{print $1}')"
[ "$repro_hash_1" = "$repro_hash_2" ] || die "independent reproducibility hashes differ"
[ "$repro_hash_1" = "$archive_hash" ] || die "checked archive differs from deterministic rebuild"

echo "release-layout-test: PASS"
echo "release-layout-test: archive=$ARCHIVE sha256=$archive_hash"
echo "release-layout-test: independent-stage/rebuild hash=$repro_hash_1"
echo "release-layout-test: launcher=$file_launcher"
