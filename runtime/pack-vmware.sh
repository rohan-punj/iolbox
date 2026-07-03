#!/usr/bin/env bash
# pack-vmware.sh — turn the rootfs built by build-rootfs.sh into a bootable
# VMware appliance: a GPT disk (bios_grub + root) with legacy-BIOS GRUB and a
# Debian kernel, converted to monolithicSparse .vmdk, plus a templated .vmx
# (runtime/files/iolab-appliance.vmx.tmpl). See docs/providers.md
# "vmware (primary)" for the contract.
#
# The disk-image build itself (partition -> loop -> mkfs -> rsync rootfs ->
# chroot kernel/grub/open-vm-tools -> MAC-matched networkd configs) lives in
# the shared runtime/lib-disk.sh (also consumed by pack-ova.sh / pack-qemu.sh)
# so all appliance formats build byte-identical disk contents. This script is
# just: call build_raw_disk (with VMware options), then raw -> vmdk, then
# template the vmx.
#
# Must run as root (loop devices, mount, chroot, grub-install all need it)
# on a real Linux box. Requires: qemu-img, parted, mkfs.ext4, rsync (grub
# tooling is installed INSIDE the chroot, so the builder needs only
# qemu-utils, parted, e2fsprogs, rsync).
#
# Disk layout (GPT, legacy BIOS boot — VMware Workstation's default; the vmx
# template deliberately does not set firmware="efi"):
#   p1  bios_grub  1MiB..2MiB
#   p2  root ext4  2MiB..100%   (LABEL=iolab-root)
# 16 GB virtual size (the image library lives INSIDE the appliance);
# monolithicSparse vmdk so actual on-disk bytes stay near what's written.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"

# --version stamps the artifact names. Default empty -> historical unversioned
# names (iolab-appliance.vmdk/.vmx) so existing tooling keeps working.
VERSION=""

# Size levers (opt-in; forwarded to lib-disk.sh's build_raw_disk). Defaults
# keep the historical byte-comparable eager-mkfs, i386-included, vmtools-in
# behavior.
LAZY_INIT=0
INODE_COUNT=""
NO_I386=0
ZEROFREE=0

recompute_paths() {
    ROOTFS_DIR="$BUILD_DIR/rootfs"
    WORK_DIR="$BUILD_DIR/vmware-work"
    RAW_DISK="$WORK_DIR/iolab-appliance.raw"
    # Versioned artifact names: iolab-appliance-<version>.{vmdk,vmx}. Empty
    # VERSION -> the historical unversioned names.
    local stem="iolab-appliance"
    [ -n "$VERSION" ] && stem="iolab-appliance-$VERSION"
    OUT_VMDK="$BUILD_DIR/$stem.vmdk"
    OUT_VMX="$BUILD_DIR/$stem.vmx"
}
recompute_paths

DISK_SIZE_MB=16384         # 16 GB virtual; sparse vmdk only consumes written blocks

# Fixed NIC MACs — MUST match files/iolab-appliance.vmx.tmpl. lib-disk.sh
# writes MAC-matched networkd configs so "which NIC is host-only" never
# depends on interface naming (ens160 vs ens33 vs eth0).
MAC_HOSTONLY="00:50:56:3f:ab:01"
MAC_NAT="00:50:56:3f:ab:02"

usage() {
    cat <<EOF
Usage: $0 [options]

  --build-dir DIR     Root containing rootfs/ (default: $BUILD_DIR)
  --version VER       Stamp artifact names as iolab-appliance-VER.{vmdk,vmx}
                       (default: unversioned iolab-appliance.{vmdk,vmx})
  --zerofree          Zero free blocks before conversion — THE size lever:
                       measured ~1.55 GiB -> ~0.81 GiB vmdk (needs the
                       'zerofree' package; dd fallback otherwise)
  --lazy-init         mkfs.ext4 lazy_itable_init+lazy_journal_init (measured to
                       NOT shrink the vmdk; kept for completeness)
  --inode-count N     mkfs.ext4 -N N (negligible size effect; kept for tuning)
  --no-i386           Exclude i386 multiarch libs from the disk (64-bit-only)
  -h, --help          This help

Defaults (no size levers) reproduce the historical byte-comparable build.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; recompute_paths; shift 2 ;;
        --version) VERSION="$2"; recompute_paths; shift 2 ;;
        --zerofree) ZEROFREE=1; shift ;;
        --lazy-init) LAZY_INIT=1; shift ;;
        --inode-count) INODE_COUNT="$2"; shift 2 ;;
        --no-i386) NO_I386=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "pack-vmware.sh must run as root (loop/mount/chroot/grub-install)." >&2
    exit 1
fi

if [ ! -d "$ROOTFS_DIR" ]; then
    echo "pack-vmware: no rootfs at $ROOTFS_DIR — run ./build-rootfs.sh first" >&2
    exit 1
fi

# shellcheck source=lib-disk.sh
source "$SCRIPT_DIR/lib-disk.sh"

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"

# ---------------------------------------------------------------------------
# Stage 1-4: build the bootable raw disk (shared lib). VMware keeps
# open-vm-tools (--no-vmtools NOT passed) and the MAC-matched NIC configs.
# ---------------------------------------------------------------------------
disk_opts=(
    --rootfs "$ROOTFS_DIR"
    --out "$RAW_DISK"
    --files-dir "$SCRIPT_DIR/files"
    --size-mb "$DISK_SIZE_MB"
    --hostonly-mac "$MAC_HOSTONLY"
    --nat-mac "$MAC_NAT"
)
[ "$ZEROFREE" -eq 1 ] && disk_opts+=(--zerofree)
[ "$LAZY_INIT" -eq 1 ] && disk_opts+=(--lazy-init)
[ -n "$INODE_COUNT" ] && disk_opts+=(--inode-count "$INODE_COUNT")
[ "$NO_I386" -eq 1 ] && disk_opts+=(--no-i386)

build_raw_disk "${disk_opts[@]}"

# ---------------------------------------------------------------------------
# Stage 5: raw -> vmdk
# ---------------------------------------------------------------------------
echo "== pack-vmware: converting raw -> vmdk =="
# monolithicSparse: single-file, growable. VMware Workstation reads this
# subformat natively; the split/streamOptimized variants only matter for
# OVA/ESXi distribution (that's pack-ova.sh's job).
qemu-img convert -f raw -O vmdk -o subformat=monolithicSparse \
    "$RAW_DISK" "$OUT_VMDK"
rm -f "$RAW_DISK"

# ---------------------------------------------------------------------------
# Stage 6: template the .vmx
# ---------------------------------------------------------------------------
echo "== pack-vmware: templating .vmx =="
VMDK_FILENAME="$(basename "$OUT_VMDK")"
sed \
    -e "s|@@VMDK_FILENAME@@|$VMDK_FILENAME|g" \
    "$SCRIPT_DIR/files/iolab-appliance.vmx.tmpl" > "$OUT_VMX"

echo "== pack-vmware: done =="
ls -lh "$OUT_VMDK" "$OUT_VMX"
cat <<EOF

Appliance ready:
  $OUT_VMDK
  $OUT_VMX

Windows-side (see docs/providers.md "vmware (primary)"):
  vmrun -T ws start "$(basename "$OUT_VMX")" nogui
  vmrun -T ws getGuestIPAddress "$(basename "$OUT_VMX")" -wait   # open-vm-tools installed
  # GUI:      http://<that-ip>:4001
  # no-tools fallback: grep $MAC_HOSTONLY C:\\ProgramData\\VMware\\vmnetdhcp.leases
  vmrun -T ws stop "$(basename "$OUT_VMX")" nogui
EOF
