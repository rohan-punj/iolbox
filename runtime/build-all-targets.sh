#!/usr/bin/env bash
# build-all-targets.sh — one command that (re)builds EVERY deployment target
# from the current worktree: the versioned supervisor binary, then every
# packaging format runtime/ knows how to produce (WSL tar, VMware appliance,
# OVA, Proxmox LXC template, native tarball, QEMU-compat qcow2). Docker is
# intentionally NOT included here — it builds its own image straight from
# the repo root (see docker/README.md) and doesn't consume runtime/build/.
#
# This wraps, rather than replaces, the existing scripts:
#   repo root  build-release.sh   -> supervisor/bin/supervisor-linux-amd64
#   runtime/   build-all.sh        -> rootfs + WSL tar + VMware appliance
#   runtime/   pack-ova.sh          -> OVA
#   runtime/   pack-lxc.sh          -> LXC template
#   runtime/   pack-native.sh       -> native tarball
#   runtime/   pack-qemu.sh         -> qcow2 (qemu-compat provider)
#
# Must run as root on a Linux builder (same requirement as build-all.sh:
# loop devices, chroot, grub-install for the disk-image targets). The
# non-disk targets (LXC, native) don't need root but are harmless under it.
#
# Usage:
#   sudo ./build-all-targets.sh
#   sudo ./build-all-targets.sh --skip-vmware --skip-ova   # only what you need
#
# VERSION defaults to `git describe --tags --always --dirty` from the repo
# root — the SAME string build-release.sh bakes into the binary via -X
# main.version, so the OVA/LXC/native artifact filenames and the running
# supervisor's own `hello.supervisor` report line up. Override with
# --version if you want a different label on the artifact names.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"

SKIP_WSL=0
SKIP_VMWARE=0
SKIP_OVA=0
SKIP_LXC=0
SKIP_NATIVE=0
SKIP_QEMU=0
VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"

usage() {
    cat <<EOF
Usage: sudo $0 [options]

  --version VER     Override the artifact-name/version stamp (default:
                     git describe --tags --always --dirty -> $VERSION)
  --build-dir DIR   Output root (default: $BUILD_DIR)
  --skip-wsl        Don't produce iolab-rootfs.tar
  --skip-vmware     Don't produce the VMware vmdk/vmx appliance
  --skip-ova        Don't produce the .ova
  --skip-lxc        Don't produce the Proxmox LXC template tarball
  --skip-native     Don't produce the native install tarball
  --skip-qemu       Don't produce the qemu-compat qcow2
  -h, --help        This help

Runs, in order:
  1. repo root  build-release.sh      -> supervisor/bin/supervisor-linux-amd64
                                          (GUI embedded, version-stamped)
  2. runtime/   build-all.sh           -> rootfs/ + iolab-rootfs.tar (WSL)
                                          + iolab-appliance-<VER>.{vmdk,vmx}
  3. runtime/   pack-ova.sh             -> iolab-appliance-<VER>.ova
  4. runtime/   pack-lxc.sh             -> iolab-ct-<VER>.tar.zst
  5. runtime/   pack-native.sh          -> iolab-server-<VER>.tar.gz
  6. runtime/   pack-qemu.sh            -> iolab-disk-<VER>.qcow2

Docker is a separate build (context = repo root, not runtime/build/) — see
docker/README.md; it isn't part of this wrapper.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --build-dir) BUILD_DIR="$2"; shift 2 ;;
        --skip-wsl) SKIP_WSL=1; shift ;;
        --skip-vmware) SKIP_VMWARE=1; shift ;;
        --skip-ova) SKIP_OVA=1; shift ;;
        --skip-lxc) SKIP_LXC=1; shift ;;
        --skip-native) SKIP_NATIVE=1; shift ;;
        --skip-qemu) SKIP_QEMU=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "build-all-targets.sh must run as root end-to-end (disk-image targets need it)." >&2
    echo "Try: sudo $0 $*" >&2
    exit 1
fi

export IOLAB_BUILD_DIR="$BUILD_DIR"
SUPERVISOR_BIN="$REPO_ROOT/supervisor/bin/supervisor-linux-amd64"

echo "############################################################"
echo "# iolab build-all-targets"
echo "#   version:   $VERSION"
echo "#   build dir: $BUILD_DIR"
echo "############################################################"

echo "== build-all-targets: build-release.sh (repo root) =="
( cd "$REPO_ROOT" && bash build-release.sh )

BUILD_ALL_ARGS=(--supervisor-bin "$SUPERVISOR_BIN" --build-dir "$BUILD_DIR" --version "$VERSION")
[ "$SKIP_WSL" -eq 1 ] && BUILD_ALL_ARGS+=(--skip-wsl)
[ "$SKIP_VMWARE" -eq 1 ] && BUILD_ALL_ARGS+=(--skip-vmware)

echo "== build-all-targets: build-all.sh (rootfs + WSL + VMware) =="
"$SCRIPT_DIR/build-all.sh" "${BUILD_ALL_ARGS[@]}"

if [ "$SKIP_OVA" -eq 0 ]; then
    echo "== build-all-targets: pack-ova.sh =="
    "$SCRIPT_DIR/pack-ova.sh" --build-dir "$BUILD_DIR" --version "$VERSION"
else
    echo "== build-all-targets: --skip-ova given, skipping =="
fi

if [ "$SKIP_LXC" -eq 0 ]; then
    echo "== build-all-targets: pack-lxc.sh =="
    "$SCRIPT_DIR/pack-lxc.sh" --build-dir "$BUILD_DIR" --version "$VERSION"
else
    echo "== build-all-targets: --skip-lxc given, skipping =="
fi

if [ "$SKIP_NATIVE" -eq 0 ]; then
    echo "== build-all-targets: pack-native.sh =="
    "$SCRIPT_DIR/pack-native.sh" --supervisor-bin "$SUPERVISOR_BIN" --build-dir "$BUILD_DIR" --version "$VERSION"
else
    echo "== build-all-targets: --skip-native given, skipping =="
fi

if [ "$SKIP_QEMU" -eq 0 ]; then
    echo "== build-all-targets: pack-qemu.sh =="
    "$SCRIPT_DIR/pack-qemu.sh" --build-dir "$BUILD_DIR" --version "$VERSION"
else
    echo "== build-all-targets: --skip-qemu given, skipping =="
fi

echo "############################################################"
echo "# iolab build-all-targets: DONE (version $VERSION)"
echo "############################################################"
find "$BUILD_DIR" -maxdepth 1 -type f \( -name '*.tar' -o -name '*.tar.zst' -o -name '*.tar.gz' \
    -o -name '*.vmdk' -o -name '*.vmx' -o -name '*.ova' -o -name '*.qcow2*' \) -exec ls -lh {} \;

echo
echo "Not built by this wrapper (separate, context = repo root):"
echo "  docker build -f docker/Dockerfile -t iolab:\$VERSION .   # see docker/README.md"
echo
echo "See runtime/REDEPLOY.md for per-target redeploy steps."
