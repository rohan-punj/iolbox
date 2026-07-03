#!/usr/bin/env bash
# pack-vmware.sh — turn the rootfs built by build-rootfs.sh into a bootable
# VMware appliance: a GPT disk (bios_grub + root) with legacy-BIOS GRUB and a
# Debian kernel, converted to .vmdk, plus a templated .vmx
# (runtime/files/iolab-appliance.vmx.tmpl). See docs/providers.md
# "vmware (primary)" for the contract.
#
# Must run as root (loop devices, mount, chroot, grub-install all need it)
# on a real Linux box. Requires: qemu-img, parted, mkfs.ext4, grub tooling
# is installed INSIDE the chroot (grub-pc), so the builder itself only
# needs qemu-utils, parted, e2fsprogs, rsync.
#
# Disk layout (GPT, legacy BIOS boot — VMware Workstation's default; the
# vmx template deliberately does not set firmware="efi"):
#   p1  bios_grub  1MiB..2MiB   (grub-install --target=i386-pc core.img home)
#   p2  root ext4  2MiB..100%   (LABEL=iolab-root)
# No ESP: nothing here boots EFI, and one partition fewer is one less
# thing to break. 16 GB virtual size — the image library
# (/opt/iolab/images) lives INSIDE the appliance and real IOL images run
# 100-400 MB each; the vmdk is monolithicSparse so actual on-disk size
# stays near the written bytes (~600-800 MB with kernel + tools).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
ROOTFS_DIR="$BUILD_DIR/rootfs"
WORK_DIR="$BUILD_DIR/vmware-work"
RAW_DISK="$WORK_DIR/iolab-appliance.raw"
OUT_VMDK="$BUILD_DIR/iolab-appliance.vmdk"
OUT_VMX="$BUILD_DIR/iolab-appliance.vmx"

DISK_SIZE_MB=16384         # 16 GB virtual; sparse vmdk only consumes written blocks

# Fixed NIC MACs — MUST match files/iolab-appliance.vmx.tmpl. The networkd
# configs written below match on these so "which NIC is host-only" never
# depends on interface naming (ens160 vs ens33 vs eth0).
MAC_HOSTONLY="00:50:56:3f:ab:01"
MAC_NAT="00:50:56:3f:ab:02"

usage() {
    cat <<EOF
Usage: $0 [--build-dir DIR]

  --build-dir DIR     Root containing rootfs/ (default: $BUILD_DIR)
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; ROOTFS_DIR="$BUILD_DIR/rootfs"; WORK_DIR="$BUILD_DIR/vmware-work"; RAW_DISK="$WORK_DIR/iolab-appliance.raw"; OUT_VMDK="$BUILD_DIR/iolab-appliance.vmdk"; OUT_VMX="$BUILD_DIR/iolab-appliance.vmx"; shift 2 ;;
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

for tool in qemu-img parted mkfs.ext4 rsync chroot; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "pack-vmware: required tool '$tool' not found." >&2
        echo "  apt-get install qemu-utils parted e2fsprogs rsync" >&2
        exit 1
    }
done

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"

# ---------------------------------------------------------------------------
# Stage 1: raw disk + GPT partition table (bios_grub + root)
# ---------------------------------------------------------------------------
echo "== pack-vmware: creating ${DISK_SIZE_MB}MB raw disk =="
qemu-img create -f raw "$RAW_DISK" "${DISK_SIZE_MB}M"

parted --script "$RAW_DISK" \
    mklabel gpt \
    mkpart bios_grub 1MiB 2MiB \
    set 1 bios_grub on \
    mkpart root ext4 2MiB 100%

# ---------------------------------------------------------------------------
# Stage 2: loop-mount, format, copy rootfs
# ---------------------------------------------------------------------------
echo "== pack-vmware: attaching loop device =="
LOOP_DEV="$(losetup --show -f -P "$RAW_DISK")"
trap 'losetup -d "$LOOP_DEV" 2>/dev/null || true' EXIT

ROOT_PART="${LOOP_DEV}p2"

mkfs.ext4 -q -L iolab-root "$ROOT_PART"

MNT="$WORK_DIR/mnt"
mkdir -p "$MNT"
mount "$ROOT_PART" "$MNT"

echo "== pack-vmware: copying rootfs onto disk =="
# -a preserves everything build-rootfs.sh set up (symlinks for the systemd
# unit enablement, root's shadow entry, file modes).
rsync -aHAX --numeric-ids "$ROOTFS_DIR"/ "$MNT"/

# Bind-mount pseudo-filesystems for the chroot steps (kernel install,
# grub-install) that follow — both need /proc, /dev, /sys to work.
for fs in proc sys dev dev/pts; do
    mkdir -p "$MNT/$fs"
    mount --bind "/$fs" "$MNT/$fs"
done
trap 'for fs in dev/pts dev sys proc; do umount -lf "$MNT/$fs" 2>/dev/null || true; done; umount -lf "$MNT" 2>/dev/null || true; losetup -d "$LOOP_DEV" 2>/dev/null || true' EXIT

# ---------------------------------------------------------------------------
# Stage 3: kernel + grub + open-vm-tools inside the chroot
# ---------------------------------------------------------------------------
echo "== pack-vmware: installing kernel + grub + open-vm-tools =="

# fstab: root by LABEL (inside the guest the disk enumerates as /dev/sda,
# never as this build's loop device).
cat > "$MNT/etc/fstab" <<EOF
LABEL=iolab-root   /   ext4   errors=remount-ro   0 1
EOF

# The finished rootfs ships an EMPTY resolv.conf (see build-rootfs.sh
# stage 6) — apt inside this chroot needs a real one. Restore the empty
# file after the installs below.
cp -L /etc/resolv.conf "$MNT/etc/resolv.conf"

chroot "$MNT" env DEBIAN_FRONTEND=noninteractive apt-get update
# linux-image-amd64 (the generic metapackage) — NOT linux-image-cloud-amd64:
# the cloud flavor sheds non-virtio drivers and this appliance's vmx uses
# lsilogic SCSI + e1000e NICs, which the generic flavor supports out of the
# box. open-vm-tools makes `vmrun getGuestIPAddress` work — the primary
# host-only IP discovery mechanism (the appliance's host-only address is a
# DHCP lease whose subnet differs per Workstation install; a baked static
# IP would only be right on the machine that built the image).
chroot "$MNT" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    linux-image-amd64 grub-pc open-vm-tools

chroot "$MNT" update-grub

# --target=i386-pc: legacy BIOS boot, matching the vmx NOT setting
# firmware="efi". --recheck avoids grub-install caching a device map from
# a previous run's loop device node.
chroot "$MNT" grub-install --target=i386-pc --recheck "$LOOP_DEV"

# ---------------------------------------------------------------------------
# Stage 4: per-NIC networkd configs, matched by the vmx's fixed MACs
# ---------------------------------------------------------------------------
echo "== pack-vmware: writing MAC-matched networkd configs =="

# Host-only NIC: DHCP lease from VMware's vmnetdhcp (subnet varies per
# install — that's WHY it's DHCP). No gateway/DNS ever wanted from this
# segment even if a misconfigured vmnet offers one: the default route must
# stay on the NAT NIC or the NAT node MASQUERADEs into a dead end.
cat > "$MNT/etc/systemd/network/10-hostonly.network" <<EOF
# Written by pack-vmware.sh (VMware artifact only). MAC matches
# ethernet0 in the vmx template.
[Match]
MACAddress=$MAC_HOSTONLY

[Network]
DHCP=ipv4

[DHCPv4]
UseGateway=false
UseDNS=false
UseRoutes=false
EOF

# NAT NIC: full DHCP — its gateway IS the NAT node's outbound path.
cat > "$MNT/etc/systemd/network/20-nat.network" <<EOF
# Written by pack-vmware.sh (VMware artifact only). MAC matches
# ethernet1 in the vmx template.
[Match]
MACAddress=$MAC_NAT

[Network]
DHCP=ipv4
EOF

# ---------------------------------------------------------------------------
# Stage 5: cleanup + unmount
# ---------------------------------------------------------------------------
echo "== pack-vmware: cleaning apt cache inside image =="
chroot "$MNT" apt-get clean
rm -rf "$MNT"/var/lib/apt/lists/*
: > "$MNT/etc/resolv.conf"

sync
for fs in dev/pts dev sys proc; do
    umount -lf "$MNT/$fs"
done
umount -lf "$MNT"
losetup -d "$LOOP_DEV"
trap - EXIT

# ---------------------------------------------------------------------------
# Stage 6: raw -> vmdk
# ---------------------------------------------------------------------------
echo "== pack-vmware: converting raw -> vmdk =="
# monolithicSparse: single-file, growable. VMware Workstation reads this
# subformat natively; the split/streamOptimized variants only matter for
# OVA/ESXi distribution.
qemu-img convert -f raw -O vmdk -o subformat=monolithicSparse \
    "$RAW_DISK" "$OUT_VMDK"
rm -f "$RAW_DISK"

# ---------------------------------------------------------------------------
# Stage 7: template the .vmx
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
