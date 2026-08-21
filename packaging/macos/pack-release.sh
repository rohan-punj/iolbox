#!/usr/bin/env bash
# Build the unsigned Apple Silicon release archive from explicit inputs.
#
# This script deliberately has no CI-workspace discovery. The caller supplies
# the Darwin launcher, BOTH exact payloads and their trusted digests, the
# release/ref version, output path, and SOURCE_DATE_EPOCH.
#
# Two payloads, because the archive serves two execution profiles:
#   --payload        the historical untagged amd64 tarball, run under Rosetta
#                    by the debian13/jammy/debian12 profiles. Unchanged.
#   --payload-arm64  the linux/arm64 tarball used by the native-arm64 profile.
# The launcher picks between them from the resolved profile; see selectPayload
# in tools/iolab-launcher/macos_lima.go.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANIFEST="$SCRIPT_DIR/release-manifest.txt"
ARCHIVE_ROOT_NAME="iolbox-macos-arm64"

LAUNCHER=""
PAYLOAD=""
PAYLOAD_SHA256=""
PAYLOAD_ARM64=""
PAYLOAD_ARM64_SHA256=""
VERSION=""
OUTPUT=""
SOURCE_DATE_EPOCH=""

usage() {
    cat <<'EOF'
Usage: packaging/macos/pack-release.sh \
  --launcher PATH --payload PATH --payload-sha256 SHA256 \
  --payload-arm64 PATH --payload-arm64-sha256 SHA256 \
  --version VERSION --output PATH --source-date-epoch EPOCH

All eight inputs are required.

  --payload / --payload-sha256              amd64 payload, run under Rosetta by
                                            the debian13/jammy/debian12
                                            profiles. Basename must be
                                            iolbox-server-<version>.tar.gz
  --payload-arm64 / --payload-arm64-sha256  linux/arm64 payload, used by the
                                            native-arm64 profile. Basename must
                                            be
                                            iolbox-server-<version>-linux-arm64.tar.gz

Both payloads are REQUIRED. With the native profile assets in the archive, a
qualifying host's `auto` selects native; an archive missing the arm64 payload
would fail only AFTER the profile is locked in, with no fallback left.
Requiring both makes that state unrepresentable.

Payloads and launcher are never discovered by searching a build directory; the
caller must identify and authenticate them.
EOF
}

die() {
    echo "pack-release: $*" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --launcher) [ "$#" -ge 2 ] || die "--launcher needs a value"; LAUNCHER="$2"; shift 2 ;;
        --payload) [ "$#" -ge 2 ] || die "--payload needs a value"; PAYLOAD="$2"; shift 2 ;;
        --payload-sha256) [ "$#" -ge 2 ] || die "--payload-sha256 needs a value"; PAYLOAD_SHA256="$2"; shift 2 ;;
        --payload-arm64) [ "$#" -ge 2 ] || die "--payload-arm64 needs a value"; PAYLOAD_ARM64="$2"; shift 2 ;;
        --payload-arm64-sha256) [ "$#" -ge 2 ] || die "--payload-arm64-sha256 needs a value"; PAYLOAD_ARM64_SHA256="$2"; shift 2 ;;
        --version) [ "$#" -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
        --output) [ "$#" -ge 2 ] || die "--output needs a value"; OUTPUT="$2"; shift 2 ;;
        --source-date-epoch) [ "$#" -ge 2 ] || die "--source-date-epoch needs a value"; SOURCE_DATE_EPOCH="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
done

[ -n "$LAUNCHER" ] || die "--launcher is required"
[ -n "$PAYLOAD" ] || die "--payload is required"
[ -n "$PAYLOAD_SHA256" ] || die "--payload-sha256 is required"
[ -n "$PAYLOAD_ARM64" ] || die "--payload-arm64 is required"
[ -n "$PAYLOAD_ARM64_SHA256" ] || die "--payload-arm64-sha256 is required"
[ -n "$VERSION" ] || die "--version is required"
[ -n "$OUTPUT" ] || die "--output is required"
[ -n "$SOURCE_DATE_EPOCH" ] || die "--source-date-epoch is required"

[[ "$PAYLOAD_SHA256" =~ ^[0-9A-Fa-f]{64}$ ]] || die "--payload-sha256 must be 64 hexadecimal characters"
[[ "$PAYLOAD_ARM64_SHA256" =~ ^[0-9A-Fa-f]{64}$ ]] || die "--payload-arm64-sha256 must be 64 hexadecimal characters"
[[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "version contains unsupported filename characters: $VERSION"
[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || die "--source-date-epoch must be a non-negative integer"

LAUNCHER="$(cd "$(dirname "$LAUNCHER")" && pwd)/$(basename "$LAUNCHER")"
PAYLOAD="$(cd "$(dirname "$PAYLOAD")" && pwd)/$(basename "$PAYLOAD")"
PAYLOAD_ARM64="$(cd "$(dirname "$PAYLOAD_ARM64")" && pwd)/$(basename "$PAYLOAD_ARM64")"
OUTPUT="$(cd "$(dirname "$OUTPUT")" 2>/dev/null || { mkdir -p "$(dirname "$OUTPUT")"; cd "$(dirname "$OUTPUT")"; } && pwd)/$(basename "$OUTPUT")"

[ -f "$LAUNCHER" ] && [ ! -L "$LAUNCHER" ] || die "launcher must be a regular non-symlink file: $LAUNCHER"
[ -f "$PAYLOAD" ] && [ ! -L "$PAYLOAD" ] || die "payload must be a regular non-symlink file: $PAYLOAD"
[ -f "$PAYLOAD_ARM64" ] && [ ! -L "$PAYLOAD_ARM64" ] || die "arm64 payload must be a regular non-symlink file: $PAYLOAD_ARM64"
command -v file >/dev/null 2>&1 || die "file is required to verify the Darwin launcher"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required on the Ubuntu packer"
command -v tar >/dev/null 2>&1 || die "GNU tar is required"
command -v gzip >/dev/null 2>&1 || die "gzip is required"
tar --version | grep -F 'GNU tar' >/dev/null || die "tar is not GNU tar"

EXPECTED_PAYLOAD="iolbox-server-${VERSION}.tar.gz"
EXPECTED_PAYLOAD_ARM64="iolbox-server-${VERSION}-linux-arm64.tar.gz"
[ "$(basename "$PAYLOAD")" = "$EXPECTED_PAYLOAD" ] || die "payload basename is $(basename "$PAYLOAD"), expected $EXPECTED_PAYLOAD"
[ "$(basename "$PAYLOAD_ARM64")" = "$EXPECTED_PAYLOAD_ARM64" ] || die "arm64 payload basename is $(basename "$PAYLOAD_ARM64"), expected $EXPECTED_PAYLOAD_ARM64"
[ "$(basename "$OUTPUT")" = "${ARCHIVE_ROOT_NAME}.tar.gz" ] || die "output must be named ${ARCHIVE_ROOT_NAME}.tar.gz"

LAUNCHER_FILE="$(file -b "$LAUNCHER")"
case "$LAUNCHER_FILE" in
    *"Mach-O 64-bit"*"arm64"*) ;;
    *) die "launcher is not a Mach-O arm64 executable: $LAUNCHER_FILE" ;;
esac

ACTUAL_PAYLOAD="$(sha256sum "$PAYLOAD" | awk '{print tolower($1)}')"
[ "$ACTUAL_PAYLOAD" = "$(printf '%s' "$PAYLOAD_SHA256" | tr '[:upper:]' '[:lower:]')" ] || die "payload hash does not match trusted --payload-sha256 (actual $ACTUAL_PAYLOAD)"
ACTUAL_PAYLOAD_ARM64="$(sha256sum "$PAYLOAD_ARM64" | awk '{print tolower($1)}')"
[ "$ACTUAL_PAYLOAD_ARM64" = "$(printf '%s' "$PAYLOAD_ARM64_SHA256" | tr '[:upper:]' '[:lower:]')" ] || die "arm64 payload hash does not match trusted --payload-arm64-sha256 (actual $ACTUAL_PAYLOAD_ARM64)"

[ -f "$MANIFEST" ] && [ ! -L "$MANIFEST" ] || die "release manifest is missing or is a symlink: $MANIFEST"

declare -a MANIFEST_SOURCES=()
declare -a MANIFEST_DESTS=()
while IFS='|' read -r source dest extra || [ -n "${source:-}" ]; do
    case "${source:-}" in
        ""|\#*) continue ;;
    esac
    [ -n "${dest:-}" ] && [ -z "${extra:-}" ] || die "manifest line must be source|destination: $source|$dest${extra:+|$extra}"
    case "$source" in
        /*|../*|*/../*|.|*/./*) die "manifest source escapes repository: $source" ;;
    esac
    case "$dest" in
        ""|/*|../*|*/../*|.|*/./*) die "manifest destination is unsafe: $dest" ;;
    esac
    source_path="$REPO_ROOT/$source"
    [ -f "$source_path" ] && [ ! -L "$source_path" ] || die "manifest source is not a regular non-symlink file: $source"
    for existing in "${MANIFEST_DESTS[@]}"; do
        [ "$existing" != "$dest" ] || die "manifest destination appears more than once: $dest"
    done
    MANIFEST_SOURCES+=("$source")
    MANIFEST_DESTS+=("$dest")
done < "$MANIFEST"

[ "${#MANIFEST_SOURCES[@]}" -eq 24 ] || die "manifest has ${#MANIFEST_SOURCES[@]} entries, expected 24"

for required_dest in \
    README.md LICENSE notices/THIRD_PARTY.md \
    lima/profiles.env lima/iolbox-trixie.yaml lima/iolbox-jammy.yaml lima/iolbox-bookworm.yaml \
    lima/pinned-image-debian13.env lima/pinned-image.env lima/pinned-image-debian12.env \
    guest/lib.sh guest/10-multiarch-debian.sh guest/10-multiarch.sh \
    guest/20-kernel-hold-debian.sh guest/20-kernel-hold.sh guest/30-canary.sh \
    guest/40-install-payload.sh guest/50-verify.sh \
    lima/iolbox-native-arm64.yaml lima/pinned-image-native-arm64.env \
    guest/10-multiarch-native.sh guest/30-canary-native.sh \
    guest/40-install-payload-native.sh guest/50-verify-native.sh; do
    found=0
    for dest in "${MANIFEST_DESTS[@]}"; do [ "$dest" = "$required_dest" ] && found=1; done
    [ "$found" -eq 1 ] || die "manifest is missing required destination: $required_dest"
done

OUTPUT_DIR="$(dirname "$OUTPUT")"
mkdir -p "$OUTPUT_DIR"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-macos-pack.XXXXXX")"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

copy_manifest_into_stage() {
    local stage="$1"
    local source dest source_path dest_path
    mkdir -p "$stage/$ARCHIVE_ROOT_NAME"
    while IFS='|' read -r source dest extra || [ -n "${source:-}" ]; do
        case "${source:-}" in
            ""|\#*) continue ;;
        esac
        source_path="$REPO_ROOT/$source"
        dest_path="$stage/$ARCHIVE_ROOT_NAME/$dest"
        mkdir -p "$(dirname "$dest_path")"
        if [ "$dest" = "README.md" ]; then
            sed "s|@VERSION@|$VERSION|g" "$source_path" > "$dest_path"
        else
            cp -- "$source_path" "$dest_path"
        fi
        chmod 0644 "$dest_path"
    done < "$MANIFEST"
    cp -- "$LAUNCHER" "$stage/$ARCHIVE_ROOT_NAME/iolbox"
    chmod 0755 "$stage/$ARCHIVE_ROOT_NAME/iolbox"
    cp -- "$PAYLOAD" "$stage/$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD"
    chmod 0644 "$stage/$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD"
    cp -- "$PAYLOAD_ARM64" "$stage/$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD_ARM64"
    chmod 0644 "$stage/$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD_ARM64"
    find "$stage/$ARCHIVE_ROOT_NAME" -type d -exec chmod 0755 {} +
}

verify_stage_tree() {
    local stage="$1"
    local actual="$stage/actual-members.txt"
    local expected="$stage/expected-members.txt"
    ( cd "$stage" && LC_ALL=C find "$ARCHIVE_ROOT_NAME" -print | LC_ALL=C sort > "$actual" )
    cat > "$expected" <<EOF
$ARCHIVE_ROOT_NAME
$ARCHIVE_ROOT_NAME/LICENSE
$ARCHIVE_ROOT_NAME/README.md
$ARCHIVE_ROOT_NAME/SHA256SUMS
$ARCHIVE_ROOT_NAME/guest
$ARCHIVE_ROOT_NAME/guest/10-multiarch-debian.sh
$ARCHIVE_ROOT_NAME/guest/10-multiarch-native.sh
$ARCHIVE_ROOT_NAME/guest/10-multiarch.sh
$ARCHIVE_ROOT_NAME/guest/20-kernel-hold-debian.sh
$ARCHIVE_ROOT_NAME/guest/20-kernel-hold.sh
$ARCHIVE_ROOT_NAME/guest/30-canary-native.sh
$ARCHIVE_ROOT_NAME/guest/30-canary.sh
$ARCHIVE_ROOT_NAME/guest/40-install-payload-native.sh
$ARCHIVE_ROOT_NAME/guest/40-install-payload.sh
$ARCHIVE_ROOT_NAME/guest/50-verify-native.sh
$ARCHIVE_ROOT_NAME/guest/50-verify.sh
$ARCHIVE_ROOT_NAME/guest/lib.sh
$ARCHIVE_ROOT_NAME/iolbox
$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD_ARM64
$ARCHIVE_ROOT_NAME/$EXPECTED_PAYLOAD
$ARCHIVE_ROOT_NAME/lima
$ARCHIVE_ROOT_NAME/lima/iolbox-bookworm.yaml
$ARCHIVE_ROOT_NAME/lima/iolbox-jammy.yaml
$ARCHIVE_ROOT_NAME/lima/iolbox-native-arm64.yaml
$ARCHIVE_ROOT_NAME/lima/iolbox-trixie.yaml
$ARCHIVE_ROOT_NAME/lima/pinned-image-debian12.env
$ARCHIVE_ROOT_NAME/lima/pinned-image-debian13.env
$ARCHIVE_ROOT_NAME/lima/pinned-image-native-arm64.env
$ARCHIVE_ROOT_NAME/lima/pinned-image.env
$ARCHIVE_ROOT_NAME/lima/profiles.env
$ARCHIVE_ROOT_NAME/notices
$ARCHIVE_ROOT_NAME/notices/THIRD_PARTY.md
EOF
    if ! cmp -s "$actual" "$expected"; then
        echo "expected staged members:" >&2
        cat "$expected" >&2
        echo "actual staged members:" >&2
        cat "$actual" >&2
        die "staged member tree is not the exact expected layout"
    fi
}

write_internal_checksums() {
    local stage="$1"
    local root="$stage/$ARCHIVE_ROOT_NAME"
    local list="$stage/internal-members.txt"
    ( cd "$root" && LC_ALL=C find . -type f ! -name SHA256SUMS -print | sed 's#^\./##' | LC_ALL=C sort > "$list" )
    # 24 manifest files + the launcher + both payloads.
    [ "$(wc -l < "$list" | tr -d ' ')" -eq 27 ] || die "staged file count is not 27 before SHA256SUMS"
    ( cd "$root" && sha256sum $(cat "$list") > SHA256SUMS )
    chmod 0644 "$root/SHA256SUMS"
}

write_stage_members() {
    local stage="$1"
    ( cd "$stage" && LC_ALL=C find "$ARCHIVE_ROOT_NAME" -print | LC_ALL=C sort > stage-members.txt )
}

pack_stage() {
    local stage="$1"
    local output="$2"
    write_stage_members "$stage"
    LC_ALL=C tar --create --format=pax --sort=name --owner=0 --group=0 --numeric-owner \
        --mode='u+rwX,go+rX,go-w' --mtime="@${SOURCE_DATE_EPOCH}" \
        --pax-option=delete=atime,delete=ctime --no-recursion \
        -C "$stage" --files-from="$stage/stage-members.txt" | gzip -n > "$output"
}

for n in 1 2; do
    stage="$WORK/stage$n"
    mkdir -p "$stage"
    copy_manifest_into_stage "$stage"
    write_internal_checksums "$stage"
    verify_stage_tree "$stage"
    pack_stage "$stage" "$WORK/archive$n.tar.gz"
done

cmp -s "$WORK/archive1.tar.gz" "$WORK/archive2.tar.gz" || die "independent stage archives differ"
HASH1="$(sha256sum "$WORK/archive1.tar.gz" | awk '{print $1}')"
HASH2="$(sha256sum "$WORK/archive2.tar.gz" | awk '{print $1}')"
[ "$HASH1" = "$HASH2" ] || die "independent stage hashes differ"
cp -- "$WORK/archive1.tar.gz" "$OUTPUT"
printf '%s  %s\n' "$HASH1" "$(basename "$OUTPUT")" > "$OUTPUT.sha256"
chmod 0644 "$OUTPUT" "$OUTPUT.sha256"

echo "pack-release: launcher=$LAUNCHER_FILE"
echo "pack-release: payload=$(basename "$PAYLOAD") sha256=$ACTUAL_PAYLOAD"
echo "pack-release: payload-arm64=$(basename "$PAYLOAD_ARM64") sha256=$ACTUAL_PAYLOAD_ARM64"
echo "pack-release: source_date_epoch=$SOURCE_DATE_EPOCH"
echo "pack-release: archive=$OUTPUT sha256=$HASH1"
echo "pack-release: independent stages byte-identical (hash=$HASH1)"
