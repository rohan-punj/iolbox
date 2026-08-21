#!/usr/bin/env bash
# fetch-qemu-user.sh — acquire the pinned Debian .deb set that the macOS
# native-arm64 profile redistributes, and stage it for inclusion in the
# linux/arm64 native payload.
#
# WHY THIS EXISTS
# ---------------
# The native-arm64 profile has no Rosetta, so the owner's x86_64-only IOL
# binary is translated in-guest by qemu-user. Until now those packages were
# installed by the guest's own apt at provisioning time
# (packaging/macos/guest/10-multiarch-native.sh). The owner decided iolbox
# should redistribute them itself instead: iolbox is a learner tool, and the
# archive working self-contained matters more than minimizing what we ship.
# See docs/macos-native-arm64-qemu-redistribution-plan.md for the full design
# and THIRD_PARTY.md for the redistribution notice and GPL source offer.
#
# WHY NOT apt-get download
# ------------------------
# `apt-get download` resolves against whatever the mirror currently serves.
# Trixie point releases and security updates move libc6/libssl3t64/qemu
# without notice, so the same command would produce different bytes on
# different days — and the builder is Ubuntu, so it would additionally need a
# trixie sources.list, a foreign architecture and an apt sandbox. Instead we
# fetch exact pool paths over HTTPS and verify every file against the
# committed lockfile. THE LOCKFILE, NOT THE MIRROR, IS THE PIN: a mirror that
# serves the wrong bytes fails the hash and the build stops.
#
# Two sources are tried per package, sha256 deciding in both cases:
#   1. deb.debian.org  — fast and CDN-backed, but Debian REMOVES superseded
#                        pool files, so this stops working after an update.
#   2. snapshot.debian.org — the permanent archive; slower and rate-limited,
#                        but immune to supersession. This is what keeps the
#                        build reproducible once the pool rotates.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCKFILE="${IOLBOX_QEMU_LOCKFILE:-$SCRIPT_DIR/qemu-user.lock}"

# Pool rotation fallback. Any snapshot timestamp at or after the pinned
# versions' publication works; this one was verified to serve every row.
SNAPSHOT_TIMESTAMP="${IOLBOX_DEBIAN_SNAPSHOT:-20260801T000000Z}"
PRIMARY_BASE="${IOLBOX_DEBIAN_MIRROR:-https://deb.debian.org/debian}"
SNAPSHOT_BASE="https://snapshot.debian.org/archive/debian/$SNAPSHOT_TIMESTAMP"

OUT_DIR=""

usage() {
    cat <<EOF
Usage: $0 --out DIR [--lockfile PATH]

  --out DIR        Directory to stage the bundle into. Populated with
                   arm64/, amd64/, the copyright files, MANIFEST and
                   SOURCE-OFFER.txt.
  --lockfile PATH  Override the pinned package list
                   (default: $LOCKFILE)
  -h, --help       This help.

Exits non-zero if any pinned package cannot be fetched or fails its
checksum. There is deliberately no "carry on without the bundle" path.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT_DIR="$2"; shift 2 ;;
        --lockfile) LOCKFILE="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "fetch-qemu-user: unknown option: $1" >&2; usage >&2; exit 1 ;;
    esac
done

[ -n "$OUT_DIR" ] || { echo "fetch-qemu-user: --out is required" >&2; usage >&2; exit 1; }
[ -f "$LOCKFILE" ] || { echo "fetch-qemu-user: lockfile not found: $LOCKFILE" >&2; exit 1; }

for tool in curl sha256sum; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "fetch-qemu-user: '$tool' is required but not on PATH" >&2; exit 1; }
done

mkdir -p "$OUT_DIR/arm64" "$OUT_DIR/amd64"

# Fetch one pool path into a destination file, trying the primary mirror then
# the snapshot archive, and verifying the expected sha256 in both cases. A
# file that already exists and matches is left alone so repeated local builds
# do not re-download 64 MiB.
fetch_one() {
    local pool_path="$1" expected="$2" dest="$3"
    local actual url

    if [ -f "$dest" ]; then
        actual="$(sha256sum "$dest" | awk '{print $1}')"
        if [ "$actual" = "$expected" ]; then
            echo "   cached  $(basename "$dest")"
            return 0
        fi
        rm -f -- "$dest"
    fi

    for url in "$PRIMARY_BASE/$pool_path" "$SNAPSHOT_BASE/$pool_path"; do
        if ! curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 30 \
                 --max-time 900 -o "$dest.tmp" "$url"; then
            rm -f -- "$dest.tmp"
            echo "   miss    $url" >&2
            continue
        fi
        actual="$(sha256sum "$dest.tmp" | awk '{print $1}')"
        if [ "$actual" != "$expected" ]; then
            rm -f -- "$dest.tmp"
            # A hash mismatch is never retried against the other mirror as if
            # it were a transient miss: it means the archive served content
            # that is not what this release pinned, and that is fatal.
            echo "fetch-qemu-user: checksum mismatch for $pool_path" >&2
            echo "  url:      $url" >&2
            echo "  expected: $expected" >&2
            echo "  actual:   $actual" >&2
            exit 1
        fi
        mv -f -- "$dest.tmp" "$dest"
        echo "   fetched $(basename "$dest")"
        return 0
    done

    echo "fetch-qemu-user: could not fetch $pool_path from any configured source" >&2
    echo "  tried: $PRIMARY_BASE/$pool_path" >&2
    echo "  tried: $SNAPSHOT_BASE/$pool_path" >&2
    return 1
}

echo "== fetch-qemu-user: staging pinned Debian packages into $OUT_DIR =="

MANIFEST="$OUT_DIR/MANIFEST"
: > "$MANIFEST"
count=0

while IFS='|' read -r package version arch sha256 pool_path || [ -n "${package:-}" ]; do
    case "${package:-}" in ''|'#'*) continue ;; esac
    [ -n "$version" ] && [ -n "$arch" ] && [ -n "$sha256" ] && [ -n "$pool_path" ] || {
        echo "fetch-qemu-user: malformed lockfile row: $package" >&2; exit 1; }
    case "$arch" in
        amd64|arm64) ;;
        *) echo "fetch-qemu-user: lockfile row has unexpected arch '$arch': $package" >&2; exit 1 ;;
    esac
    printf '%s' "$sha256" | grep -Eq '^[0-9a-f]{64}$' || {
        echo "fetch-qemu-user: lockfile row has malformed sha256: $package" >&2; exit 1; }
    # Refuse traversal or absolute pool paths from the lockfile.
    case "$pool_path" in
        pool/*) ;;
        *) echo "fetch-qemu-user: lockfile pool path must start with pool/: $pool_path" >&2; exit 1 ;;
    esac
    case "$pool_path" in
        *..*) echo "fetch-qemu-user: lockfile pool path contains '..': $pool_path" >&2; exit 1 ;;
    esac

    base="$(basename "$pool_path")"
    fetch_one "$pool_path" "$sha256" "$OUT_DIR/$arch/$base"
    printf '%s|%s|%s|%s|%s\n' "$package" "$version" "$arch" "$sha256" "$base" >> "$MANIFEST"
    count=$((count + 1))
done < "$LOCKFILE"

[ "$count" -gt 0 ] || { echo "fetch-qemu-user: lockfile contained no package rows" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Notices.
#
# Every package we redistribute carries its own obligations -- not just QEMU
# and binfmt-support. The bundle spans eight Debian SOURCE packages (qemu,
# binfmt-support, glibc, gcc-14, openssl, libzstd, zlib, libpipeline), and
# each has a copyright file that must travel with its binaries.
#
# Two levels are needed, because a Debian copyright file is not a self-
# contained licence:
#
#   notices/<source>.copyright   Debian's DEP-5 file -- per-file copyright
#                                attribution and licence identification.
#   notices/licenses/<NAME>      the actual licence TEXTS. DEP-5 files
#                                reference /usr/share/common-licenses/GPL-2
#                                and friends rather than embedding them,
#                                which resolves on an installed Debian system
#                                because base-files provides that directory.
#                                An iolbox archive is not an installed Debian
#                                system, so we ship the texts ourselves --
#                                otherwise every copyright file we ship points
#                                at a path that does not exist for the reader.
#
# These are EXTRACTED FROM THE PINNED .debs at build time rather than
# committed to the repo, so the notices cannot drift out of sync with the
# lockfile: the copyright file that ships is by construction the one from the
# exact package that ships.
# ---------------------------------------------------------------------------
LICENSES_LOCK="${IOLBOX_LICENSES_LOCK:-$SCRIPT_DIR/licenses.lock}"
[ -f "$LICENSES_LOCK" ] || {
    echo "fetch-qemu-user: licences lockfile not found: $LICENSES_LOCK" >&2; exit 1; }

command -v python3 >/dev/null 2>&1 || command -v python >/dev/null 2>&1 || {
    echo "fetch-qemu-user: python3 is required to extract licence notices" >&2; exit 1; }
PYTHON="$(command -v python3 2>/dev/null || command -v python)"

# base-files supplies /usr/share/common-licenses/*. It is a licence-text
# source only and is deliberately staged OUTSIDE the guest install tree so
# the guest can never be told to install it.
BASE_FILES_DEB=""
while IFS='|' read -r package version arch sha256 pool_path || [ -n "${package:-}" ]; do
    case "${package:-}" in ''|'#'*) continue ;; esac
    BASE_FILES_DEB="$OUT_DIR/.licence-source/$(basename "$pool_path")"
    mkdir -p "$OUT_DIR/.licence-source"
    fetch_one "$pool_path" "$sha256" "$BASE_FILES_DEB"
done < "$LICENSES_LOCK"
[ -n "$BASE_FILES_DEB" ] || {
    echo "fetch-qemu-user: licences lockfile contained no rows" >&2; exit 1; }

install -d -m 0755 "$OUT_DIR/notices" "$OUT_DIR/notices/licenses"

"$PYTHON" - "$OUT_DIR" "$BASE_FILES_DEB" <<'PY'
import io, os, re, sys, tarfile, lzma, gzip, bz2

out_dir, base_files_deb = sys.argv[1], sys.argv[2]

def ar_members(path):
    """Minimal `ar` reader. dpkg-deb is not available on every build host
    (macOS, Windows dev boxes), and this format is 60-byte headers."""
    data = open(path, 'rb').read()
    if data[:8] != b'!<arch>\n':
        raise SystemExit('not a .deb archive: %s' % path)
    off, members = 8, {}
    while off + 60 <= len(data):
        hdr = data[off:off + 60]
        name = hdr[0:16].decode('ascii', 'replace').strip().rstrip('/')
        try:
            size = int(hdr[48:58].decode('ascii').strip())
        except ValueError:
            raise SystemExit('corrupt ar header in %s' % path)
        off += 60
        members[name] = data[off:off + size]
        off += size + (size % 2)
    return members

def decompress(name, blob):
    if name.endswith('.xz'):
        return lzma.decompress(blob)
    if name.endswith('.gz'):
        return gzip.decompress(blob)
    if name.endswith('.bz2'):
        return bz2.decompress(blob)
    if name.endswith('.zst'):
        raise SystemExit('zstd-compressed .deb members are not supported')
    return blob

def open_part(members, prefix):
    for name, blob in members.items():
        if name.startswith(prefix):
            return tarfile.open(fileobj=io.BytesIO(decompress(name, blob)))
    raise SystemExit('no %s member found' % prefix)

def control_fields(members):
    tar = open_part(members, 'control.tar')
    for name in tar.getnames():
        if name.rstrip('/').endswith('control'):
            return tar.extractfile(name).read().decode('utf-8', 'replace')
    raise SystemExit('no control file found')

referenced, by_source, written = set(), {}, []

for arch in ('arm64', 'amd64'):
    arch_dir = os.path.join(out_dir, arch)
    if not os.path.isdir(arch_dir):
        continue
    for fn in sorted(os.listdir(arch_dir)):
        if not fn.endswith('.deb'):
            continue
        members = ar_members(os.path.join(arch_dir, fn))
        ctl = control_fields(members)
        pkg = re.search(r'^Package: (.+)$', ctl, re.M).group(1).strip()
        src_m = re.search(r'^Source: (\S+)', ctl, re.M)
        source = src_m.group(1) if src_m else pkg

        data = open_part(members, 'data.tar')
        names = data.getnames()
        want = './usr/share/doc/%s/copyright' % pkg
        if want not in names:
            # Some binaries (libgcc-s1, qemu-user-binfmt) ship their doc dir
            # as a symlink to a sibling package's, so the copyright file is
            # simply not in this .deb. The sibling from the same source
            # package supplies it; skip rather than fail.
            cands = [n for n in names if n.endswith('/copyright')]
            if not cands:
                continue
            want = cands[0]
        blob = data.extractfile(want).read()
        text = blob.decode('utf-8', 'replace')
        for m in re.finditer(r'/usr/share/common-licenses/([A-Za-z0-9.+-]*[A-Za-z0-9])',
                             text):
            referenced.add(m.group(1).rstrip('.'))
        by_source.setdefault(source, blob)

if not by_source:
    raise SystemExit('fetch-qemu-user: extracted no copyright files')

for source, blob in sorted(by_source.items()):
    path = os.path.join(out_dir, 'notices', '%s.copyright' % source)
    with open(path, 'wb') as fh:
        fh.write(blob)
    written.append(source)

# Ship the complete common-licenses set, not just the referenced subset: it is
# ~240 KB total, and shipping all of it removes an ongoing "did we miss a
# reference" audit every time a pinned version changes.
members = ar_members(base_files_deb)
data = open_part(members, 'data.tar')
shipped, links = [], []
for ti in data.getmembers():
    if '/common-licenses/' not in ti.name:
        continue
    base = os.path.basename(ti.name.rstrip('/'))
    if not base:
        continue
    dest = os.path.join(out_dir, 'notices', 'licenses', base)
    if ti.issym() or ti.islnk():
        links.append((base, os.path.basename(ti.linkname)))
        continue
    if not ti.isfile():
        continue
    with open(dest, 'wb') as fh:
        fh.write(data.extractfile(ti).read())
    shipped.append(base)

# Resolve symlinks (GPL -> GPL-3 etc.) into real copies: the archive layout
# test rejects symlink members outright, and a copy is more useful to a reader
# extracting only part of the tree.
for base, target in links:
    src = os.path.join(out_dir, 'notices', 'licenses', target)
    if os.path.isfile(src):
        with open(src, 'rb') as fh:
            blob = fh.read()
        with open(os.path.join(out_dir, 'notices', 'licenses', base), 'wb') as fh:
            fh.write(blob)
        shipped.append(base)

missing = sorted(r for r in referenced if r not in set(shipped))
if missing:
    raise SystemExit('fetch-qemu-user: copyright files reference licence texts '
                     'that were not shipped: %s' % ', '.join(missing))

print('   notices: %d copyright files (%s)' % (len(written), ', '.join(written)))
print('   notices: %d licence texts, covering all %d referenced'
      % (len(shipped), len(referenced)))
PY

rm -rf "$OUT_DIR/.licence-source"

[ -f "$SCRIPT_DIR/SOURCE-OFFER.txt" ] || {
    echo "fetch-qemu-user: required source offer missing: $SCRIPT_DIR/SOURCE-OFFER.txt" >&2; exit 1; }
install -m 0644 "$SCRIPT_DIR/SOURCE-OFFER.txt" "$OUT_DIR/SOURCE-OFFER.txt"

find "$OUT_DIR" -type f -exec chmod 0644 {} +
find "$OUT_DIR" -type d -exec chmod 0755 {} +
echo "== fetch-qemu-user: staged $count packages + notices =="
