#!/usr/bin/env bash
# fetch-vpcs.sh — clone and build the community VPCS binary with UDP-tunnel
# support (the same wire format IOL's iol_wrapper uses for inter-node
# links, per docs/protocol.md "Interface addressing (IOL)" — VPCS speaks
# UDP tunnels natively, no relay/hub translation needed on that side).
#
# Called by build-rootfs.sh, but also runnable standalone for iterating on
# just the VPCS build without redoing the whole debootstrap. Network
# access is required (git clone + apt build-deps); if the builder is
# airgapped, see the "manual drop path" note below instead.
#
# Output: ./build/vpcs/vpcs  (a single static-ish amd64 ELF binary)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
VPCS_SRC_DIR="$BUILD_DIR/vpcs-src"
VPCS_OUT_DIR="$BUILD_DIR/vpcs"

# Upstream: the maintained community fork (the original vpcs.googlecode.com
# / sourceforge project is unmaintained; GNS3's fork is what every current
# IOL-adjacent lab tool, including our own PNetLab work, actually builds
# from). Pinned to a tag, not a moving branch, for reproducibility — bump
# deliberately, not silently on every rebuild.
VPCS_REPO="${VPCS_REPO:-https://github.com/GNS3/vpcs.git}"
VPCS_REF="${VPCS_REF:-v0.8.3}"   # same release the proven iolab-rt vpcs was built from

echo "== fetch-vpcs: $VPCS_REPO @ $VPCS_REF =="

# --- Manual drop path (airgapped builder) ----------------------------------
# If this builder has no network access, skip this script entirely and
# instead place a prebuilt linux/amd64 vpcs binary at:
#
#     runtime/build/vpcs/vpcs
#
# before running build-rootfs.sh (which looks for exactly that path — see
# its --vpcs-bin flag / VPCS_BIN default below). build-rootfs.sh does not
# require this script to have run; it just requires the binary to exist.
# -----------------------------------------------------------------------

if [ -x "$VPCS_OUT_DIR/vpcs" ]; then
    echo "fetch-vpcs: $VPCS_OUT_DIR/vpcs already built, skipping (delete it to force a rebuild)"
    exit 0
fi

command -v git >/dev/null 2>&1 || { echo "fetch-vpcs: git is required" >&2; exit 1; }
command -v make >/dev/null 2>&1 || { echo "fetch-vpcs: make is required (apt install build-essential)" >&2; exit 1; }
command -v cc >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1 || {
    echo "fetch-vpcs: a C compiler is required (apt install build-essential)" >&2
    exit 1
}

mkdir -p "$BUILD_DIR"

if [ -d "$VPCS_SRC_DIR/.git" ]; then
    echo "fetch-vpcs: reusing existing clone at $VPCS_SRC_DIR"
    git -C "$VPCS_SRC_DIR" fetch --tags origin
else
    rm -rf "$VPCS_SRC_DIR"
    git clone --branch "$VPCS_REF" --depth 1 "$VPCS_REPO" "$VPCS_SRC_DIR" \
        || {
            # Tag checkout can fail with --depth 1 --branch if the ref is a
            # commit rather than a tag/branch; fall back to a full clone.
            echo "fetch-vpcs: shallow clone of ref '$VPCS_REF' failed, retrying with full clone"
            rm -rf "$VPCS_SRC_DIR"
            git clone "$VPCS_REPO" "$VPCS_SRC_DIR"
        }
fi

git -C "$VPCS_SRC_DIR" checkout "$VPCS_REF"

# The GNS3/vpcs source layout builds from src/ with per-OS makefiles —
# src/Makefile.linux is the one that matters here (PROVEN: the reference
# iolab-rt VM's vpcs was built exactly this way). Some tags also carry a
# plain src/Makefile or src/unix/Makefile.linux; try the proven layout
# first, then the historical ones.
pushd "$VPCS_SRC_DIR/src" >/dev/null

echo "fetch-vpcs: building..."
# CFLAGS/LDFLAGS note: the binary lands inside a Debian-bookworm rootfs
# whose glibc may be OLDER than this builder's (e.g. building on Ubuntu
# 24.04/glibc 2.39 for bookworm/glibc 2.36). A dynamic binary built here
# can pick up newer versioned symbols and fail to start in the rootfs, so
# link vpcs statically — it's a small, old-school C program and takes a
# static glibc link without trouble.
if [ -f Makefile.linux ]; then
    make -f Makefile.linux LDFLAGS="-static -lpthread -lutil"
elif [ -f Makefile ]; then
    make LDFLAGS="-static -lpthread -lutil"
elif [ -f unix/Makefile.linux ]; then
    make -C unix -f Makefile.linux LDFLAGS="-static -lpthread -lutil"
else
    echo "fetch-vpcs: no recognizable Makefile under $VPCS_SRC_DIR/src" >&2
    exit 1
fi

popd >/dev/null

BUILT_BIN=$(find "$VPCS_SRC_DIR" -maxdepth 3 -type f -name vpcs -perm -u+x | head -n1)
if [ -z "$BUILT_BIN" ]; then
    echo "fetch-vpcs: build finished but no 'vpcs' executable was found under $VPCS_SRC_DIR" >&2
    exit 1
fi

mkdir -p "$VPCS_OUT_DIR"
cp "$BUILT_BIN" "$VPCS_OUT_DIR/vpcs"
chmod 0755 "$VPCS_OUT_DIR/vpcs"

# Sanity check: must be a linux/amd64 ELF, since it's going straight into
# an amd64 rootfs. This catches "built on an arm64 laptop" mistakes early
# rather than shipping a broken binary that only fails at runtime inside
# the VM.
if command -v file >/dev/null 2>&1; then
    FILE_OUT="$(file -b "$VPCS_OUT_DIR/vpcs")"
    echo "fetch-vpcs: built $VPCS_OUT_DIR/vpcs ($FILE_OUT)"
    case "$FILE_OUT" in
        *"ELF 64-bit"*"x86-64"*) : ;;
        *) echo "fetch-vpcs: WARNING - binary does not look like linux/amd64 ELF64: $FILE_OUT" >&2 ;;
    esac
    case "$FILE_OUT" in
        *"statically linked"*) : ;;
        *) echo "fetch-vpcs: WARNING - binary is NOT statically linked; it may fail inside the rootfs if the builder's glibc is newer than bookworm's" >&2 ;;
    esac
else
    echo "fetch-vpcs: built $VPCS_OUT_DIR/vpcs ('file' not available to verify arch)"
fi

echo "== fetch-vpcs: done -> $VPCS_OUT_DIR/vpcs =="
