#!/usr/bin/env bash
# pack-wsl.sh — export the rootfs built by build-rootfs.sh as a tarball
# suitable for `wsl --import` (the wsl2 provider, docs/providers.md).
#
# Must run AFTER build-rootfs.sh has produced runtime/build/rootfs/. Does
# not itself need root (plain tar of an existing directory), but if the
# rootfs contains device nodes or files owned by uids that only root can
# read, run this as root too, to be safe (debootstrap can leave a couple
# of root-only files, e.g. under /etc/shadow-ish paths).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
ROOTFS_DIR="$BUILD_DIR/rootfs"
OUT_TAR="$BUILD_DIR/iolab-rootfs.tar"

usage() {
    cat <<EOF
Usage: $0 [--build-dir DIR] [--out FILE]

  --build-dir DIR   Root containing rootfs/ (default: $BUILD_DIR)
  --out FILE        Output tar path (default: $OUT_TAR)
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; ROOTFS_DIR="$BUILD_DIR/rootfs"; OUT_TAR="$BUILD_DIR/iolab-rootfs.tar"; shift 2 ;;
        --out) OUT_TAR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ ! -d "$ROOTFS_DIR" ]; then
    echo "pack-wsl: no rootfs at $ROOTFS_DIR — run ./build-rootfs.sh first" >&2
    exit 1
fi

if [ ! -f "$ROOTFS_DIR/etc/wsl.conf" ]; then
    # Not fatal (WSL2 will still import and boot without it — it'll just
    # fall back to WSL's non-systemd init shim, silently breaking the
    # iolab-supervisor.service autostart), but loud enough that a partial
    # or hand-assembled rootfs doesn't ship broken by accident.
    echo "pack-wsl: WARNING - $ROOTFS_DIR/etc/wsl.conf is missing." >&2
    echo "           Without it, WSL2 will NOT run systemd, and the supervisor" >&2
    echo "           will never autostart. Re-run build-rootfs.sh, which installs" >&2
    echo "           runtime/files/wsl.conf into the rootfs automatically." >&2
fi

echo "== pack-wsl: creating $OUT_TAR from $ROOTFS_DIR =="

# `wsl --import` wants a tar of the root filesystem's CONTENTS (i.e. tar'd
# from inside ROOTFS_DIR, so the archive's top-level entries are etc/,
# opt/, usr/, ... not rootfs/etc/, rootfs/opt/, ...). Getting this wrong
# (tarring the parent dir) is the single most common WSL import mistake —
# the import "succeeds" but produces a distro with an empty-looking root.
#
# --numeric-owner: preserve uid/gid numbers as-is rather than trying (and
# failing, since this builder's /etc/passwd doesn't match the guest's) to
# resolve them to names. WSL2 doesn't care about the archive's embedded
# owner *names*, only the numeric ids, which is what the rootfs's own
# /etc/passwd inside the tar will interpret at runtime.
#
# --xattrs is deliberately NOT passed: some overlay/xattr combinations
# from certain container-based debootstrap invocations produce
# capability xattrs that WSL2's 9p-ish import path has historically choked
# on for a small number of binaries (setcap'd ping, etc.). Since this
# rootfs doesn't setcap anything of its own and ping's default capability
# xattr isn't load-bearing here (root runs everything anyway — see
# iolab-supervisor.service's User=root), dropping xattrs on import is a
# safe simplification, not a functional loss.
tar \
    --create \
    --file "$OUT_TAR" \
    --directory "$ROOTFS_DIR" \
    --numeric-owner \
    --sort=name \
    .

echo "== pack-wsl: done =="
ls -lh "$OUT_TAR"
cat <<EOF

Windows-side import (see docs/providers.md "wsl2"):

  wsl --import iolab <install-dir> "$OUT_TAR"

Example:

  wsl --import iolab C:\\Users\\<you>\\iolab-wsl "$(cygpath -w "$OUT_TAR" 2>/dev/null || echo "$OUT_TAR")"

First boot will run iolab-firstboot-iourc.service then
iolab-supervisor.service automatically (systemd=true in /etc/wsl.conf is
what makes that happen — see runtime/files/wsl.conf). Verify with:

  wsl -d iolab -- systemctl status iolab-supervisor.service
  wsl -d iolab -- ss -ltnp sport = :4000
EOF
