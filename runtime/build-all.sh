#!/usr/bin/env bash
# build-all.sh — orchestrator: fetch-vpcs (if needed) -> build-rootfs ->
# pack-wsl + pack-vmware. One command, one rootfs, both artifacts. See
# runtime/README.md "Quick start (on a Linux builder)".
#
# Must run as root end-to-end (build-rootfs.sh and pack-vmware.sh both
# require it; fetch-vpcs.sh and pack-wsl.sh don't strictly need root but
# are harmless to run as root too).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SUPERVISOR_BIN="$SCRIPT_DIR/../supervisor/bin/supervisor-linux-amd64"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
HOSTONLY_IP="192.168.171.2"
SKIP_VMWARE=0
SKIP_WSL=0
EXTRA_ROOTFS_ARGS=()

usage() {
    cat <<EOF
Usage: $0 --supervisor-bin PATH [options]

  --supervisor-bin PATH   Path to the built Go supervisor binary (required;
                           see supervisor/ — GOOS=linux GOARCH=amd64 go build)
  --build-dir DIR          Output root (default: $BUILD_DIR)
  --hostonly-ip IP         Fixed VMware host-only IP (default: $HOSTONLY_IP)
  --skip-wsl               Don't produce iolab-rootfs.tar
  --skip-vmware             Don't produce the vmdk/vmx appliance
  --no-i386                 Forwarded to build-rootfs.sh (smaller, 64-bit-IOL-only build)
  -h, --help                This help

Runs, in order:
  1. ./fetch-vpcs.sh          (only if runtime/build/vpcs/vpcs is missing)
  2. ./build-rootfs.sh         -> runtime/build/rootfs/
  3. ./pack-wsl.sh              -> runtime/build/iolab-rootfs.tar   (unless --skip-wsl)
  4. ./pack-vmware.sh           -> runtime/build/iolab-appliance.{vmdk,vmx} (unless --skip-vmware)
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --supervisor-bin) SUPERVISOR_BIN="$2"; shift 2 ;;
        --build-dir) BUILD_DIR="$2"; shift 2 ;;
        --hostonly-ip) HOSTONLY_IP="$2"; shift 2 ;;
        --skip-wsl) SKIP_WSL=1; shift ;;
        --skip-vmware) SKIP_VMWARE=1; shift ;;
        --no-i386) EXTRA_ROOTFS_ARGS+=(--no-i386); shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "build-all.sh must run as root end-to-end (build-rootfs.sh / pack-vmware.sh need it)." >&2
    echo "Try: sudo $0 $*" >&2
    exit 1
fi

export IOLAB_BUILD_DIR="$BUILD_DIR"

echo "############################################################"
echo "# iolab runtime build-all"
echo "#   supervisor: $SUPERVISOR_BIN"
echo "#   build dir:  $BUILD_DIR"
echo "#   hostonly ip: $HOSTONLY_IP"
echo "############################################################"

VPCS_BIN="$BUILD_DIR/vpcs/vpcs"
if [ ! -x "$VPCS_BIN" ]; then
    echo "== build-all: vpcs binary missing, running fetch-vpcs.sh =="
    "$SCRIPT_DIR/fetch-vpcs.sh"
else
    echo "== build-all: reusing existing vpcs binary at $VPCS_BIN =="
fi

echo "== build-all: build-rootfs.sh =="
"$SCRIPT_DIR/build-rootfs.sh" \
    --build-dir "$BUILD_DIR" \
    --supervisor-bin "$SUPERVISOR_BIN" \
    --vpcs-bin "$VPCS_BIN" \
    "${EXTRA_ROOTFS_ARGS[@]}"

if [ "$SKIP_WSL" -eq 0 ]; then
    echo "== build-all: pack-wsl.sh =="
    "$SCRIPT_DIR/pack-wsl.sh" --build-dir "$BUILD_DIR"
else
    echo "== build-all: --skip-wsl given, skipping =="
fi

if [ "$SKIP_VMWARE" -eq 0 ]; then
    echo "== build-all: pack-vmware.sh =="
    "$SCRIPT_DIR/pack-vmware.sh" --build-dir "$BUILD_DIR" --hostonly-ip "$HOSTONLY_IP"
else
    echo "== build-all: --skip-vmware given, skipping =="
fi

echo "############################################################"
echo "# iolab runtime build-all: DONE"
echo "############################################################"
find "$BUILD_DIR" -maxdepth 1 -type f \( -name '*.tar' -o -name '*.vmdk' -o -name '*.vmx' \) -exec ls -lh {} \;
