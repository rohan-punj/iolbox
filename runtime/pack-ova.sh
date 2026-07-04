#!/usr/bin/env bash
# pack-ova.sh - turn the rootfs built by build-rootfs.sh into a portable OVA
# appliance importable by VirtualBox, VMware Workstation, and ESXi.
#
# Like pack-vmware.sh, the bootable-disk build itself (partition -> loop ->
# mkfs -> rsync rootfs -> chroot kernel/grub/open-vm-tools -> MAC-matched
# networkd configs) lives in the shared runtime/lib-disk.sh, so the OVA disk
# contents are byte-identical to the VMware appliance's. This script's own job
# is only:
#   1. build_raw_disk ... --zerofree            (KEEP vmtools + NIC configs)
#   2. qemu-img convert -O vmdk subformat=streamOptimized  (deflate-compressed)
#   3. template a hand-written OVF with the real disk sizes
#   4. tar the OVA:  OVF FIRST, then the vmdk, and NO .mf manifest
#
# Why these specific choices:
#   * open-vm-tools stays IN (lib-disk default): it's harmless on VirtualBox and
#     powers guest-IP discovery on VMware/ESXi. NIC configs stay IN so the two
#     E1000e adapters land on the right networkd match by MAC.
#   * --zerofree is THE size lever: it zeroes freed apt/kernel blocks so the
#     streamOptimized (deflate) vmdk drops to a few hundred MB instead of ~1.5G.
#   * streamOptimized is the OVF/OVA-standard sparse+compressed vmdk subformat;
#     it's what all three importers expect inside an OVA.
#   * NO manifest: a .mf whose SHA digests don't match the (possibly re-tar'd)
#     files hard-fails import - hard-won prior art. The OVF-first ordering in
#     the tar is what lets a streaming importer read the descriptor before the
#     (large) disk.
#
# Must run as root (loop/mount/chroot/grub-install). Requires: qemu-img,
# parted, mkfs.ext4, rsync, tar, and the `zerofree` package (dd fallback
# otherwise). grub tooling is installed inside the chroot.
#
# Sizing / minimums (also in the OVF annotation + docs/install.md):
#   defaults 4 vCPU / 4096 MB; MINIMUM to boot + run one small IOL node is
#   ~2 GB RAM / 1 vCPU. 16 GB virtual disk (the image library lives inside the
#   appliance); the OVA file itself is a few hundred MB.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"

# resources.env is the single source of truth for vCPU/RAM across every
# deployment target (VMware, OVA, LXC, Docker); the Windows QEMU launcher
# mirrors it in its own flag defaults. See runtime/resources.env.
# shellcheck source=resources.env
source "$SCRIPT_DIR/resources.env"

# --version stamps the artifact name + the OVF Product/VirtualSystem id.
# Default "dev" (siblings use dev for the unversioned dev build).
VERSION="dev"

# Size levers forwarded to lib-disk. For OVA we always want --zerofree; it's
# forced on below regardless of flag, but the flag stays for parity/override.
LAZY_INIT=0
INODE_COUNT=""
NO_I386=0

DISK_SIZE_MB=16384         # 16 GB virtual; streamOptimized vmdk stays sparse

# Fixed NIC MACs - MUST match the two networkd configs lib-disk writes. The OVF
# doesn't pin MACs (importers assign their own), but the in-guest networkd match
# is by these MACs, so lib-disk must write them; the OVF's two E1000e adapters
# get whatever MAC the hypervisor assigns and fall through to the generic
# 80-ethernet-dhcp.network. To keep deterministic host-only/NAT roles the way
# VMware does, we still pass the fixed pair; on VirtualBox both NICs simply DHCP.
MAC_HOSTONLY="00:50:56:3f:ab:01"
MAC_NAT="00:50:56:3f:ab:02"

recompute_paths() {
    ROOTFS_DIR="$BUILD_DIR/rootfs"
    WORK_DIR="$BUILD_DIR/ova-work"
    RAW_DISK="$WORK_DIR/iolab-appliance.raw"
    local stem="iolab-appliance"
    [ -n "$VERSION" ] && stem="iolab-appliance-$VERSION"
    STEM="$stem"
    # vmdk lands inside the OVA staging dir; OVA is the final artifact.
    OVA_STAGE="$WORK_DIR/stage"
    OVF_FILENAME="$stem.ovf"
    VMDK_FILENAME="$stem-disk1.vmdk"
    OUT_OVA="$BUILD_DIR/$stem.ova"
}
recompute_paths

usage() {
    cat <<EOF
Usage: $0 [options]

  --build-dir DIR     Root containing rootfs/ (default: $BUILD_DIR)
  --version VER       Stamp artifact name as iolab-appliance-VER.ova and the
                       OVF Product/VirtualSystem id (default: dev)
  --rootfs DIR        Override the rootfs dir (default: <build-dir>/rootfs)
  --out FILE          Override the output .ova path
  --lazy-init         mkfs.ext4 lazy init (measured NOT to shrink; parity flag)
  --inode-count N     mkfs.ext4 -N N (negligible size effect; parity flag)
  --no-i386           Exclude i386 multiarch libs from the disk (64-bit-only)
  -h, --help          This help

The OVA build always zeroes free blocks (--zerofree in lib-disk) - that is the
size lever that keeps the streamOptimized vmdk to a few hundred MB.
EOF
}

ROOTFS_OVERRIDE=""
OUT_OVERRIDE=""
while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; recompute_paths; shift 2 ;;
        --version) VERSION="$2"; recompute_paths; shift 2 ;;
        --rootfs) ROOTFS_OVERRIDE="$2"; shift 2 ;;
        --out) OUT_OVERRIDE="$2"; shift 2 ;;
        --lazy-init) LAZY_INIT=1; shift ;;
        --inode-count) INODE_COUNT="$2"; shift 2 ;;
        --no-i386) NO_I386=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done
[ -n "$ROOTFS_OVERRIDE" ] && ROOTFS_DIR="$ROOTFS_OVERRIDE"
[ -n "$OUT_OVERRIDE" ] && OUT_OVA="$OUT_OVERRIDE"

if [ "$(id -u)" -ne 0 ]; then
    echo "pack-ova.sh must run as root (loop/mount/chroot/grub-install)." >&2
    exit 1
fi
if [ ! -d "$ROOTFS_DIR" ]; then
    echo "pack-ova: no rootfs at $ROOTFS_DIR - run ./build-rootfs.sh first" >&2
    exit 1
fi
command -v tar >/dev/null 2>&1 || { echo "pack-ova: tar not found" >&2; exit 1; }

# shellcheck source=lib-disk.sh
source "$SCRIPT_DIR/lib-disk.sh"

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR" "$OVA_STAGE"

# ---------------------------------------------------------------------------
# Stage 1-4: build the bootable raw disk (shared lib). KEEP open-vm-tools and
# the MAC-matched NIC configs; force --zerofree (the OVA size lever).
# ---------------------------------------------------------------------------
disk_opts=(
    --rootfs "$ROOTFS_DIR"
    --out "$RAW_DISK"
    --files-dir "$SCRIPT_DIR/files"
    --size-mb "$DISK_SIZE_MB"
    --hostonly-mac "$MAC_HOSTONLY"
    --nat-mac "$MAC_NAT"
    --zerofree
)
[ "$LAZY_INIT" -eq 1 ] && disk_opts+=(--lazy-init)
[ -n "$INODE_COUNT" ] && disk_opts+=(--inode-count "$INODE_COUNT")
[ "$NO_I386" -eq 1 ] && disk_opts+=(--no-i386)

build_raw_disk "${disk_opts[@]}"

# ---------------------------------------------------------------------------
# Stage 5: raw -> streamOptimized vmdk (into the OVA staging dir)
# ---------------------------------------------------------------------------
echo "== pack-ova: converting raw -> streamOptimized vmdk =="
STAGE_VMDK="$OVA_STAGE/$VMDK_FILENAME"
qemu-img convert -f raw -O vmdk -o subformat=streamOptimized \
    "$RAW_DISK" "$STAGE_VMDK"
rm -f "$RAW_DISK"

# ---------------------------------------------------------------------------
# Stage 6: compute disk sizes and template the OVF
#   ovf:capacity      = virtual size in bytes (16 GiB)
#   ovf:size (File)   = actual bytes of the compressed vmdk file
#   ovf:populatedSize = uncompressed bytes actually in use (best-effort from
#                       qemu-img "disk size"; falls back to the file size)
# ---------------------------------------------------------------------------
echo "== pack-ova: templating OVF =="
VMDK_FILE_SIZE="$(stat -c %s "$STAGE_VMDK")"
VMDK_CAPACITY="$(( DISK_SIZE_MB * 1024 * 1024 ))"
# Best-effort populated (virtual "disk size" from qemu-img info); if it can't
# be parsed, fall back to the compressed file size (a safe lower-ish bound).
VMDK_POPULATED="$(qemu-img info --output=json "$STAGE_VMDK" 2>/dev/null \
    | grep -o '"actual-size":[[:space:]]*[0-9]\+' | grep -o '[0-9]\+' | head -1 || true)"
[ -z "$VMDK_POPULATED" ] && VMDK_POPULATED="$VMDK_FILE_SIZE"

VSYS_ID="$STEM"
sed \
    -e "s|@@VMDK_FILENAME@@|$VMDK_FILENAME|g" \
    -e "s|@@VMDK_FILE_SIZE@@|$VMDK_FILE_SIZE|g" \
    -e "s|@@VMDK_CAPACITY@@|$VMDK_CAPACITY|g" \
    -e "s|@@VMDK_POPULATED@@|$VMDK_POPULATED|g" \
    -e "s|@@PRODUCT_VERSION@@|$VERSION|g" \
    -e "s|@@VSYS_ID@@|$VSYS_ID|g" \
    -e "s|@@VCPUS@@|$IOLAB_VCPUS|g" \
    -e "s|@@RAM_MB@@|$IOLAB_RAM_MB|g" \
    "$SCRIPT_DIR/files/ova/iolab-appliance.ovf.tmpl" > "$OVA_STAGE/$OVF_FILENAME"

# ---------------------------------------------------------------------------
# Stage 7: tar the OVA - OVF FIRST, then vmdk, NO .mf manifest.
# An OVA is an uncompressed tar; the descriptor must precede the disk so a
# streaming importer parses hardware before the (large) disk payload.
# ---------------------------------------------------------------------------
echo "== pack-ova: assembling OVA (ovf first, no manifest) =="
rm -f "$OUT_OVA"
# -C into the stage dir so the tar holds bare filenames (no path prefix), which
# is what OVA importers expect. Explicit ordering: OVF, then vmdk.
tar -cf "$OUT_OVA" -C "$OVA_STAGE" "$OVF_FILENAME" "$VMDK_FILENAME"

echo "== pack-ova: done =="
ls -lh "$OUT_OVA" "$STAGE_VMDK"
echo
echo "OVA contents (must be: OVF first, then vmdk, no .mf):"
tar -tvf "$OUT_OVA"
cat <<EOF

Appliance ready:
  $OUT_OVA

Import (LAN-only appliance - the GUI has NO authentication):
  VirtualBox:  VBoxManage import "$STEM.ova"   (or File > Import Appliance)
               then set adapter 1 to Host-only, adapter 2 to NAT.
  VMware WS:   ovftool "$STEM.ova" iolab.vmx    (or File > Open)
  ESXi:        ovftool "$STEM.ova" vi://root@<esxi-host>/
  Defaults $IOLAB_VCPUS vCPU / $IOLAB_RAM_MB MB; minimum 2 GB / 1 vCPU. Then browse the guest's
  "control"-NIC IP at http://<vm-ip>:4001.
EOF
