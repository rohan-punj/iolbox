#!/usr/bin/env bash
# pack-qemu.sh — turn the rootfs built by build-rootfs.sh into a compressed
# qcow2 disk for the bundled `qemu` (compatibility) provider — the exe-shipped
# QEMU-TCG backend driven by tools/iolab-launcher. See docs/providers.md
# "qemu (compatibility)" and runtime/qemu-compat.md.
#
# The disk-image build itself (partition -> loop -> mkfs -> rsync rootfs ->
# chroot kernel/grub -> networkd configs) lives in the shared runtime/lib-disk.sh
# (also consumed by pack-vmware.sh / pack-ova.sh) so all appliance formats build
# from identical disk-building code. This script is just: call build_raw_disk
# with the QEMU-appropriate options, then raw -> compressed qcow2.
#
# QEMU-specific option choices (vs the VMware pack):
#   --no-vmtools      open-vm-tools is the VMware guest agent; useless under
#                     qemu user-mode net (the launcher reaches the guest purely
#                     over 127.0.0.1 hostfwd ports, never a guest-assigned IP,
#                     so there's no getGuestIPAddress to power). Drops ~tens of
#                     MB from the image.
#   --no-nic-configs  the VMware fixed-MAC 10-hostonly/20-nat networkd configs
#                     don't apply — qemu's virtio-net NIC gets whatever MAC the
#                     launcher/qemu assigns. The rootfs's generic
#                     80-ethernet-dhcp.network (en* match) governs it instead:
#                     DHCP from qemu's built-in user-mode DHCP, which also hands
#                     out the default route the guest's own no-default-route
#                     networkd drop-in then declines (xml.cisco.com abort
#                     protection — see runtime/qemu-compat.md).
#   --zerofree        THE size lever: zeroes freed apt/.deb blocks so the qcow2
#                     compressor (qemu-img convert -c) actually sparse/compress-
#                     skips them. Without it the compressed qcow2 still carries
#                     the non-zero residue of unpacked-then-cleaned kernel/grub
#                     debs. Needs the 'zerofree' package on the builder (dd
#                     zero-fill fallback otherwise).
#
# Must run as root (loop devices, mount, chroot, grub-install) on a real Linux
# box. Requires: qemu-img, parted, mkfs.ext4, rsync (grub tooling is installed
# INSIDE the chroot).
#
# Disk layout (identical to the VMware/OVA disks — GPT, legacy-BIOS GRUB):
#   p1  bios_grub  1MiB..2MiB
#   p2  root ext4  2MiB..100%   (LABEL=iolbox-root)
# 16 GB virtual size (the image library lives INSIDE the appliance); the qcow2
# stays sparse + is compressed, so actual on-disk bytes stay small.
#
# The guest's on-disk kernel is Debian's generic linux-image-amd64 (installed by
# lib-disk.sh), whose stock config builds in virtio_blk/virtio_net — so the
# launcher can attach this disk as `if=virtio` with no separate virtio kernel
# build. (Debian's initramfs MODULES=most also carries them, so even an
# IDE/SCSI attach would boot; virtio is simply the faster path under TCG.)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLBOX_BUILD_DIR:-$SCRIPT_DIR/build}"

# --version stamps the artifact name. Default "dev" per the launcher kickoff
# (the exe looks for iolbox-disk.qcow2, so a release build points --out at that
# fixed name; --version just gives a distinguishable dev artifact).
VERSION="dev"

recompute_paths() {
    ROOTFS_DIR="$BUILD_DIR/rootfs"
    WORK_DIR="$BUILD_DIR/qemu-work"
    RAW_DISK="$WORK_DIR/iolbox-disk.raw"
    # Versioned artifact name: iolbox-disk-<version>.qcow2. An explicit --out
    # overrides this entirely (release builds point it at plain
    # iolbox-disk.qcow2, the name the launcher looks for next to the exe).
    local stem="iolbox-disk"
    [ -n "$VERSION" ] && stem="iolbox-disk-$VERSION"
    OUT_QCOW2="$BUILD_DIR/$stem.qcow2"
}
recompute_paths

# Explicit --out wins over the version-derived name.
OUT_OVERRIDE=""

DISK_SIZE_MB=16384   # 16 GB virtual; compressed qcow2 only carries written blocks

usage() {
    cat <<EOF
Usage: $0 [options]

  --build-dir DIR     Root containing rootfs/ (default: $BUILD_DIR)
  --rootfs DIR        Rootfs directory (default: <build-dir>/rootfs)
  --version VER       Stamp artifact name as iolbox-disk-VER.qcow2
                       (default: dev -> iolbox-disk-dev.qcow2)
  --out FILE          Explicit output qcow2 path (overrides --version naming;
                       release builds use --out .../iolbox-disk.qcow2)
  -h, --help          This help

Always builds with --no-vmtools --no-nic-configs --zerofree (the QEMU profile).
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; recompute_paths; shift 2 ;;
        --rootfs)    ROOTFS_OVERRIDE="$2"; shift 2 ;;
        --version)   VERSION="$2"; recompute_paths; shift 2 ;;
        --out)       OUT_OVERRIDE="$2"; shift 2 ;;
        -h|--help)   usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

# --rootfs override (default runtime/build/rootfs per recompute_paths).
ROOTFS_DIR="${ROOTFS_OVERRIDE:-$ROOTFS_DIR}"
# --out override wins over the version-derived qcow2 name.
[ -n "$OUT_OVERRIDE" ] && OUT_QCOW2="$OUT_OVERRIDE"

if [ "$(id -u)" -ne 0 ]; then
    echo "pack-qemu.sh must run as root (loop/mount/chroot/grub-install)." >&2
    exit 1
fi

if [ ! -d "$ROOTFS_DIR" ]; then
    echo "pack-qemu: no rootfs at $ROOTFS_DIR — run ./build-rootfs.sh first" >&2
    exit 1
fi

# shellcheck source=lib-disk.sh
source "$SCRIPT_DIR/lib-disk.sh"

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"
mkdir -p "$(dirname "$OUT_QCOW2")"

# ---------------------------------------------------------------------------
# Stage 1-4: build the bootable raw disk (shared lib), QEMU profile:
# no open-vm-tools, generic (not MAC-matched) NIC config, zeroed free space.
# ---------------------------------------------------------------------------
build_raw_disk \
    --rootfs "$ROOTFS_DIR" \
    --out "$RAW_DISK" \
    --files-dir "$SCRIPT_DIR/files" \
    --size-mb "$DISK_SIZE_MB" \
    --no-vmtools \
    --no-nic-configs \
    --zerofree

# ---------------------------------------------------------------------------
# Stage 5: raw -> compressed qcow2
# ---------------------------------------------------------------------------
echo "== pack-qemu: converting raw -> compressed qcow2 =="
# -c compresses (zlib) each cluster; combined with the zerofree'd free space
# this yields the small release artifact the launcher ships next to the exe.
qemu-img convert -f raw -O qcow2 -c "$RAW_DISK" "$OUT_QCOW2"
rm -f "$RAW_DISK"

echo "== pack-qemu: done =="
ls -lh "$OUT_QCOW2"
qemu-img info "$OUT_QCOW2" 2>/dev/null || true
cat <<EOF

qcow2 disk ready:
  $OUT_QCOW2

The tools/iolab-launcher Windows exe boots this under qemu-system-x86_64 (TCG),
forwarding 127.0.0.1:4001 (GUI/WS) + the console/capture port blocks. Rename or
--out it to iolbox-disk.qcow2 to sit next to the exe for a release bundle.
EOF
