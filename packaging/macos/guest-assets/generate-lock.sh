#!/usr/bin/env bash
# generate-lock.sh — regenerate qemu-user.lock from Debian's SIGNED archive
# metadata, resolving the dependency closure rather than trusting a hand-
# maintained package list.
#
# THIS IS A MAINTAINER TOOL, NOT A BUILD STEP. The release build consumes the
# committed qemu-user.lock via fetch-qemu-user.sh and never runs this. Run
# this when the pinned Lima guest image moves (see
# packaging/macos/lima/pinned-image-native-arm64.env) or when a package needs
# to be re-pinned, then commit the regenerated lock and re-run hardware
# validation.
#
# PROVENANCE CHAIN
# ----------------
# Committed sha256 hashes prove that what we downloaded has not changed. They
# do NOT prove the original bytes were authentic Debian packages -- a
# compromised workstation or HTTPS session could bless arbitrary bytes once,
# permanently. .deb maintainer scripts run as root in the guest, so that gap
# matters. This script therefore derives every hash from Debian's own signed
# metadata:
#
#   InRelease  (OpenPGP-signed by the Debian archive key)
#     -> sha256 of main/binary-<arch>/Packages.xz, taken from InRelease
#        -> sha256 of each .deb, taken from the verified Packages index
#
# Each link is checked, and the InRelease sha256 is recorded in the lock
# header so a reviewer can re-derive the whole chain.
#
# WHY A SNAPSHOT TIMESTAMP, NOT "CURRENT TRIXIE"
# ----------------------------------------------
# Two independent reasons, both load-bearing:
#
#   1. Reproducibility. deb.debian.org serves a moving suite and REMOVES
#      superseded pool files. A lock generated against "current trixie" stops
#      being re-fetchable the moment a point release lands.
#
#   2. Multi-Arch correctness -- the subtle one. libc6, libgcc-s1,
#      gcc-14-base, libssl3t64, libzstd1 and zlib1g are all `Multi-Arch:
#      same`, and Debian requires co-installed instances of such packages to
#      be at the IDENTICAL version across architectures. The guest already
#      has the arm64 instances, frozen at whatever the pinned cloud image
#      shipped. If we pin the amd64 instances to a different (e.g. newer)
#      version, `dpkg -i` refuses them.
#
#      Pinning the snapshot timestamp to the pinned image's own build date
#      makes the amd64 versions we ship the same versions the image already
#      carries natively, so the constraint is satisfied by construction
#      rather than by luck.
#
# So IOLBOX_DEBIAN_SNAPSHOT must track pinned-image-native-arm64.env's image
# date. That coupling is the whole design; do not bump one without the other.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUITE="${IOLBOX_DEBIAN_SUITE:-trixie}"

# Must match the build date of the image pinned in
# packaging/macos/lima/pinned-image-native-arm64.env (currently the
# 20260810-2566 genericcloud arm64 image). See the header.
SNAPSHOT_TIMESTAMP="${IOLBOX_DEBIAN_SNAPSHOT:-20260810T000000Z}"
BASE="https://snapshot.debian.org/archive/debian/$SNAPSHOT_TIMESTAMP"

# Roots of the dependency closure, per architecture.
#   arm64: the translator itself.
#   amd64: the runtime the x86_64-only IOL binary links against. These two
#          names are exactly what 10-multiarch-native.sh always installed;
#          everything else on the amd64 side is closure that apt used to
#          resolve silently and an offline dpkg will not.
ARM64_ROOTS="${IOLBOX_ARM64_ROOTS:-qemu-user qemu-user-binfmt qemu-user-static binfmt-support}"
AMD64_ROOTS="${IOLBOX_AMD64_ROOTS:-libc6 libssl3t64}"

# base-files carries /usr/share/common-licenses/*, the license texts that every
# Debian copyright file REFERENCES rather than embeds. Those texts must ship
# with the binaries (see THIRD_PARTY.md), but base-files itself is never
# installed into the guest -- it is a licence-text source only. It therefore
# gets its own lock file so the guest-side install manifest cannot accidentally
# pick it up, while still being provenance-pinned exactly like everything else.
LICENSE_SOURCE_PACKAGE="${IOLBOX_LICENSE_SOURCE_PACKAGE:-base-files}"
LICENSES_OUT="${IOLBOX_LICENSES_LOCK_OUT:-$SCRIPT_DIR/licenses.lock}"

KEYRING="${IOLBOX_DEBIAN_KEYRING:-/usr/share/keyrings/debian-archive-keyring.gpg}"
OUT="${IOLBOX_LOCK_OUT:-$SCRIPT_DIR/qemu-user.lock}"
WORK=""
ALLOW_UNVERIFIED=0

usage() {
    cat <<EOF
Usage: $0 [--out PATH] [--snapshot TIMESTAMP] [--keyring PATH] [--allow-unverified-signature]

  --out PATH        Where to write the lock (default: $OUT)
  --snapshot TS     Debian snapshot timestamp (default: $SNAPSHOT_TIMESTAMP).
                    MUST correspond to the pinned Lima image's build date.
  --keyring PATH    OpenPGP keyring for InRelease verification
                    (default: $KEYRING; from Debian's debian-archive-keyring)
  --allow-unverified-signature
                    Skip the InRelease signature check. Intended ONLY for
                    hosts with no Debian keyring available. The generated
                    lock is marked as unverified and MUST NOT be committed.
  -h, --help        This help.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT="$2"; shift 2 ;;
        --snapshot) SNAPSHOT_TIMESTAMP="$2"; BASE="https://snapshot.debian.org/archive/debian/$2"; shift 2 ;;
        --keyring) KEYRING="$2"; shift 2 ;;
        --allow-unverified-signature) ALLOW_UNVERIFIED=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "generate-lock: unknown option: $1" >&2; usage >&2; exit 1 ;;
    esac
done

for tool in curl sha256sum xz python3; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "generate-lock: '$tool' is required but not on PATH" >&2; exit 1; }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "== generate-lock: snapshot $SNAPSHOT_TIMESTAMP, suite $SUITE =="

# --- link 1: the signed InRelease -------------------------------------------
curl -fsSL --retry 3 --connect-timeout 30 --max-time 600 \
    -o "$WORK/InRelease" "$BASE/dists/$SUITE/InRelease" || {
    echo "generate-lock: could not fetch InRelease from $BASE" >&2; exit 1; }
INRELEASE_SHA="$(sha256sum "$WORK/InRelease" | awk '{print $1}')"

SIGNATURE_STATE="verified against $KEYRING"
if [ "$ALLOW_UNVERIFIED" -eq 1 ]; then
    SIGNATURE_STATE="NOT VERIFIED (--allow-unverified-signature) -- DO NOT COMMIT"
    echo "generate-lock: WARNING: skipping InRelease signature verification" >&2
else
    command -v gpgv >/dev/null 2>&1 || {
        echo "generate-lock: gpgv is required to verify InRelease. Install" >&2
        echo "  debian-archive-keyring + gpgv, or pass --allow-unverified-signature" >&2
        echo "  (which produces a lock that must not be committed)." >&2
        exit 1; }
    [ -f "$KEYRING" ] || {
        echo "generate-lock: keyring not found: $KEYRING" >&2
        echo "  On Debian/Ubuntu: apt-get install debian-archive-keyring" >&2
        exit 1; }
    gpgv --keyring "$KEYRING" "$WORK/InRelease" 2>&1 | sed 's/^/   gpgv: /' || {
        echo "generate-lock: InRelease signature verification FAILED" >&2; exit 1; }
    echo "   InRelease signature verified"
fi

# --- link 2: Packages.xz, hash-checked against InRelease --------------------
for arch in arm64 amd64; do
    rel="main/binary-$arch/Packages.xz"
    # The SHA256 block in InRelease lists "<sha256> <size> <path>". Take the
    # 64-hex entry for this exact path; MD5Sum/SHA1 rows for the same path are
    # rejected by the length filter rather than by block position.
    want="$(awk -v p="$rel" '$3==p && length($1)==64 {print $1; exit}' "$WORK/InRelease")"
    [ -n "$want" ] || { echo "generate-lock: InRelease has no sha256 for $rel" >&2; exit 1; }
    curl -fsSL --retry 3 --connect-timeout 30 --max-time 900 \
        -o "$WORK/Packages-$arch.xz" "$BASE/dists/$SUITE/$rel" || {
        echo "generate-lock: could not fetch $rel" >&2; exit 1; }
    got="$(sha256sum "$WORK/Packages-$arch.xz" | awk '{print $1}')"
    [ "$got" = "$want" ] || {
        echo "generate-lock: $rel sha256 does not match InRelease" >&2
        echo "  expected $want" >&2; echo "  actual   $got" >&2; exit 1; }
    xz -dc "$WORK/Packages-$arch.xz" > "$WORK/Packages-$arch"
    echo "   $rel verified against InRelease"
done

# --- link 3: closure + per-package hashes from the verified indices ---------
python3 - "$WORK" "$OUT" "$SNAPSHOT_TIMESTAMP" "$SUITE" "$INRELEASE_SHA" \
         "$SIGNATURE_STATE" "$ARM64_ROOTS" "$AMD64_ROOTS" \
         "$LICENSES_OUT" "$LICENSE_SOURCE_PACKAGE" <<'PY'
import os, re, sys

(work, out, ts, suite, inrel_sha, sig_state, arm_roots, amd_roots,
 licenses_out, license_pkg) = sys.argv[1:11]

def load(path):
    d = {}
    with open(path, encoding='utf-8', errors='replace') as fh:
        for stanza in fh.read().split('\n\n'):
            m = re.search(r'^Package: (.+)$', stanza, re.M)
            if m:
                d.setdefault(m.group(1).strip(), stanza)
    return d

def field(st, f):
    m = re.search(r'^%s: (.+(?:\n [^\n]*)*)$' % f, st, re.M)
    return m.group(1).replace('\n ', ' ') if m else ''

def deps(st):
    out = []
    for f in ('Pre-Depends', 'Depends'):
        s = field(st, f)
        if not s:
            continue
        for clause in s.split(','):
            # Debian's default choice is the first alternative.
            first = clause.split('|')[0].strip()
            name = re.split(r'[\s(:]', first)[0]
            if name:
                out.append(name)
    return out

def closure(index, roots, arch, native_arch):
    """Transitive Depends/Pre-Depends closure.

    Stops at Essential:yes / Priority:required packages: those are guaranteed
    present in any Debian rootfs, so bundling them would be dead weight.
    For the FOREIGN architecture nothing may be assumed present, so every
    non-essential node is bundled. For the NATIVE architecture we bundle only
    the roots' own subtree that is not already implied by the base image --
    handled by the caller's root choice.
    """
    seen, stack, keep, skipped = set(), list(roots), [], []
    while stack:
        p = stack.pop(0)
        if p in seen:
            continue
        seen.add(p)
        st = index.get(p)
        if st is None:
            raise SystemExit("generate-lock: %s:%s not found in index" % (p, arch))
        if p not in roots and (field(st, 'Essential') == 'yes'
                               or field(st, 'Priority') == 'required'):
            skipped.append(p)
            continue
        keep.append(p)
        stack.extend(deps(st))
    return keep, skipped

arm = load(work + '/Packages-arm64')
amd = load(work + '/Packages-amd64')

rows = []
notes = []

# arm64: the translator. Its closure also reaches libc6/libgcc-s1/gcc-14-base,
# but those are the guest's OWN native packages, already installed from the
# pinned image -- bundling them would mean shipping a native-library upgrade,
# which is exactly what we do not want to do mid-provisioning.
arm_keep, arm_skip = closure(arm, arm_roots.split(), 'arm64', True)
arm_native_present = {'libc6', 'libgcc-s1', 'gcc-14-base', 'libpipeline1'}
for p in arm_keep:
    if p in arm_native_present and p not in arm_roots.split():
        # libpipeline1 is the exception: it is a binfmt-support dependency and
        # is NOT guaranteed to be in a minimal cloud image, so it is bundled.
        if p != 'libpipeline1':
            notes.append('#   arm64 %s: already native in the pinned image, not bundled' % p)
            continue
    st = arm[p]
    rows.append((p, field(st, 'Version'), 'arm64', field(st, 'SHA256'),
                 field(st, 'Filename'), field(st, 'Source') or p,
                 field(st, 'Multi-Arch')))

# amd64: foreign architecture -- nothing may be assumed present except
# Essential/required packages, which dpkg guarantees.
amd_keep, amd_skip = closure(amd, amd_roots.split(), 'amd64', False)
for p in amd_keep:
    st = amd[p]
    rows.append((p, field(st, 'Version'), 'amd64', field(st, 'SHA256'),
                 field(st, 'Filename'), field(st, 'Source') or p,
                 field(st, 'Multi-Arch')))

# Sort for a stable, reviewable diff: arch first, then package name.
rows.sort(key=lambda r: (r[2], r[0]))

for p, v, a, sha, fn, src, ma in rows:
    if not (v and sha and fn):
        raise SystemExit("generate-lock: incomplete index data for %s:%s" % (p, a))
    if not re.fullmatch(r'[0-9a-f]{64}', sha):
        raise SystemExit("generate-lock: bad sha256 for %s:%s" % (p, a))
    if not fn.startswith('pool/') or '..' in fn:
        raise SystemExit("generate-lock: unsafe pool path for %s:%s: %s" % (p, a, fn))

ma_same = sorted({p for p, v, a, s, f, src, ma in rows if ma == 'same' and a == 'amd64'})

with open(out, 'w', newline='\n') as fh:
    fh.write("""\
# qemu-user.lock -- the exact Debian binary packages the macOS native-arm64
# profile redistributes. THIS FILE, NOT THE MIRROR, IS THE PIN.
#
# GENERATED by packaging/macos/guest-assets/generate-lock.sh. Do not hand-edit:
# every hash below was derived from Debian's OpenPGP-signed archive metadata,
# and hand-editing silently breaks that provenance chain.
#
#   snapshot:            {ts}
#   suite:               {suite}
#   InRelease sha256:    {inrel}
#   InRelease signature: {sig}
#
# Re-derive with:
#   packaging/macos/guest-assets/generate-lock.sh --snapshot {ts}
#
# THE SNAPSHOT TIMESTAMP IS NOT ARBITRARY. It tracks the build date of the
# Lima guest image pinned in packaging/macos/lima/pinned-image-native-arm64.env.
# The amd64 packages below are `Multi-Arch: same`, which Debian requires to be
# co-installed at the IDENTICAL version as the guest's existing arm64
# instances. Pinning to the image's own date makes that true by construction.
# Moving the image pin WITHOUT regenerating this lock will make `dpkg -i`
# reject the bundle in the guest.
#
# Multi-Arch: same amd64 packages subject to that constraint:
#   {masame}
#
# Format: package|version|arch|sha256|pool path (relative to a Debian archive root)
#
# arm64 rows are the translator. Note qemu-user-static is a TRANSITIONAL
# package in trixie containing no emulator at all -- qemu-user carries the
# actual binaries, and qemu-user-binfmt the registration files. It is bundled
# anyway (70 KB) so 10-multiarch-native.sh's existing qemu-user-static
# assertion and the M7 P7-02 evidence stay continuous.
#
# amd64 rows are the runtime the x86_64-only IOL binary links against: libc6
# and libssl3t64 are what was always installed, the rest is closure that apt
# used to resolve silently and an offline dpkg will not.
#
# See docs/macos-native-arm64-qemu-redistribution-plan.md and THIRD_PARTY.md.
""".format(ts=ts, suite=suite, inrel=inrel_sha, sig=sig_state,
           masame=' '.join(ma_same)))
    if notes:
        fh.write('\n'.join(sorted(set(notes))) + '\n')
    for p, v, a, sha, fn, src, ma in rows:
        fh.write('%s|%s|%s|%s|%s\n' % (p, v, a, sha, fn))

print("   wrote %d package rows to %s" % (len(rows), out))
for p, v, a, sha, fn, src, ma in rows:
    print("     %-26s %-24s %s  (source: %s)" % (p, v, a, src))

# --- licence-text source, pinned the same way -------------------------------
lst = arm.get(license_pkg)
if lst is None:
    raise SystemExit("generate-lock: %s not found in the arm64 index" % license_pkg)
lv, lsha, lfn = field(lst, 'Version'), field(lst, 'SHA256'), field(lst, 'Filename')
if not re.fullmatch(r'[0-9a-f]{64}', lsha) or not lfn.startswith('pool/'):
    raise SystemExit("generate-lock: bad index data for %s" % license_pkg)

with open(licenses_out, 'w', newline='\n') as fh:
    fh.write("""\
# licenses.lock -- the Debian package that supplies /usr/share/common-licenses.
#
# GENERATED by packaging/macos/guest-assets/generate-lock.sh alongside
# qemu-user.lock, from the same OpenPGP-verified archive metadata.
#
#   snapshot:            {ts}
#   suite:               {suite}
#   InRelease sha256:    {inrel}
#   InRelease signature: {sig}
#
# WHY THIS IS SEPARATE FROM qemu-user.lock
# ----------------------------------------
# Debian copyright files do not embed the GPL/LGPL/Apache texts; they REFERENCE
# /usr/share/common-licenses/<NAME>. That arrangement works on an installed
# Debian system because base-files supplies those files. An iolbox macOS
# archive is not an installed Debian system, so we must ship the referenced
# texts ourselves or the copyright files point at nothing.
#
# base-files is used ONLY as the source of those texts. It is NEVER installed
# into the guest and MUST NOT appear in the guest install manifest -- which is
# exactly why it lives in its own lock file rather than in qemu-user.lock.
#
# Format: package|version|arch|sha256|pool path
{p}|{v}|arm64|{sha}|{fn}
""".format(ts=ts, suite=suite, inrel=inrel_sha, sig=sig_state,
           p=license_pkg, v=lv, sha=lsha, fn=lfn))
print("   wrote licence-text source (%s %s) to %s" % (license_pkg, lv, licenses_out))

# --- corresponding-source map ----------------------------------------------
# GPL compliance needs the source for the EXACT binaries we ship. The source
# name and version are not derivable from the binary name and version:
#
#   - a binNMU ships binary 2.2.2-7+b1 from source 2.2.2-7;
#   - zlib1g 1:1.3.dfsg+really1.3.1-1+b1 comes from source zlib
#     1:1.3.dfsg+really1.3.1-1;
#   - libc6 comes from source glibc, libgcc-s1 from gcc-14.
#
# and a pool directory contains MANY versions, so listing it and picking one
# is how you end up shipping bookworm's qemu source next to a trixie binary.
# The Source: field of the verified index is the authority, so it is resolved
# here, once, and written down.
src_rows = {}
for p, v, a, sha, fn, src, ma in rows + [(license_pkg, lv, 'arm64', lsha, lfn,
                                          field(lst, 'Source') or license_pkg, '')]:
    m = re.match(r'^(\S+)\s+\((.+)\)$', src.strip())
    if m:
        sname, sver = m.group(1), m.group(2)
    else:
        sname, sver = (src.strip().split() or [p])[0], v
    # Debian pool layout: pool/main/<l>/<source>/ where <l> is "libX" for
    # source names starting with lib, else the first letter.
    letter = sname[:4] if sname.startswith('lib') else sname[0]
    # Source filenames drop the epoch.
    novep = re.sub(r'^\d+:', '', sver)
    src_rows[sname] = (sver, 'pool/main/%s/%s/%s_%s.dsc' % (letter, sname, sname, novep))

with open(os.path.join(os.path.dirname(out), 'sources.lock'), 'w', newline='\n') as fh:
    fh.write("""\
# sources.lock -- the corresponding SOURCE package for every binary package
# redistributed in the macOS native-arm64 archive.
#
# GENERATED by packaging/macos/guest-assets/generate-lock.sh from the same
# OpenPGP-verified archive metadata as qemu-user.lock.
#
#   snapshot:            {ts}
#   suite:               {suite}
#   InRelease sha256:    {inrel}
#   InRelease signature: {sig}
#
# Consumed by fetch-corresponding-source.sh, which publishes this source as a
# release asset so the source accompanies the binary from the same
# distribution point (see SOURCE-OFFER.txt and THIRD_PARTY.md).
#
# DO NOT derive these by listing a pool directory and picking a version. Pool
# directories hold many versions at once, and binary version != source version
# for binNMUs (binfmt-support binary 2.2.2-7+b1 <- source 2.2.2-7) and for
# renamed sources (libc6 <- glibc, libgcc-s1 <- gcc-14). Shipping the wrong
# "corresponding source" is a compliance failure, not a cosmetic one.
#
# Format: source package|source version|dsc pool path
""".format(ts=ts, suite=suite, inrel=inrel_sha, sig=sig_state))
    for sname in sorted(src_rows):
        sver, dsc = src_rows[sname]
        fh.write('%s|%s|%s\n' % (sname, sver, dsc))

print("   wrote %d source-package rows to sources.lock" % len(src_rows))
for sname in sorted(src_rows):
    print("     %-18s %s" % (sname, src_rows[sname][0]))
PY

echo "== generate-lock: done =="
