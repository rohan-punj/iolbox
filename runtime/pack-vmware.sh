#!/usr/bin/env bash
# pack-vmware.sh — turn the rootfs built by build-rootfs.sh into a bootable
# VMware appliance: a GPT disk (ESP + root partition) with GRUB and a
# Debian cloud kernel, converted to .vmdk, plus a templated .vmx
# (runtime/files/iolab-appliance.vmx.tmpl) with the fixed host-only IP
# baked in. See docs/providers.md "vmware (primary)" for the contract.
#
# Must run as root (loop devices, mount, chroot, grub-install all need it)
# on a real Linux box. Requires: qemu-img, parted or sfdisk, grub-install
# (grub-pc-bin/grub-efi-amd64-bin depending on which flavor you keep — this
# script uses BIOS/legacy grub for simplicity, since VMware Workstation
# boots legacy BIOS by default unless firmware="efi" is set in the vmx,
# which we deliberately do NOT set), a Debian cloud kernel package
# (linux-image-cloud-amd64) OR the ability to `apt-get install
# linux-image-amd64` inside the chroot (this script does the latter — one
# less external input to track, at the cost of a slightly bigger kernel
# than the trimmed "cloud" flavor; acceptable for a build-time-only extra
# ~10-15MB after strip).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
ROOTFS_DIR="$BUILD_DIR/rootfs"
WORK_DIR="$BUILD_DIR/vmware-work"
RAW_DISK="$WORK_DIR/iolab-appliance.raw"
OUT_VMDK="$BUILD_DIR/iolab-appliance.vmdk"
OUT_VMX="$BUILD_DIR/iolab-appliance.vmx"

DISK_SIZE_MB=4096          # 4 GB virtual; sparse vmdk only consumes written blocks (see README.md sizing)
ESP_SIZE_MB=64             # BIOS-boot friendly; small ESP even though we use legacy grub (keeps the option open to flip to EFI later without repartitioning)
HOSTONLY_IP="192.168.171.2"        # MUST match runtime/build-rootfs.sh and docs/providers.md
HOSTONLY_PREFIX="24"

usage() {
    cat <<EOF
Usage: $0 [--build-dir DIR] [--hostonly-ip IP]

  --build-dir DIR     Root containing rootfs/ (default: $BUILD_DIR)
  --hostonly-ip IP    Fixed guest IP on the host-only NIC (default: $HOSTONLY_IP)
                       Must match what docs/providers.md documents for the
                       Windows-side provider to find without polling.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; ROOTFS_DIR="$BUILD_DIR/rootfs"; WORK_DIR="$BUILD_DIR/vmware-work"; RAW_DISK="$WORK_DIR/iolab-appliance.raw"; OUT_VMDK="$BUILD_DIR/iolab-appliance.vmdk"; OUT_VMX="$BUILD_DIR/iolab-appliance.vmx"; shift 2 ;;
        --hostonly-ip) HOSTONLY_IP="$2"; shift 2 ;;
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

for tool in qemu-img parted mkfs.ext4 mkfs.vfat grub-install chroot; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "pack-vmware: required tool '$tool' not found." >&2
        echo "  apt-get install qemu-utils parted e2fsprogs dosfstools grub-pc-bin grub-common" >&2
        exit 1
    }
done

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR"

# ---------------------------------------------------------------------------
# Stage 1: raw disk + GPT partition table (ESP + root)
# ---------------------------------------------------------------------------
echo "== pack-vmware: creating ${DISK_SIZE_MB}MB raw disk =="
qemu-img create -f raw "$RAW_DISK" "${DISK_SIZE_MB}M"

parted --script "$RAW_DISK" \
    mklabel gpt \
    mkpart ESP fat32 1MiB "${ESP_SIZE_MB}MiB" \
    set 1 esp on \
    mkpart root ext4 "${ESP_SIZE_MB}MiB" 100%

# Also mark the BIOS boot flag conceptually covered by grub-install
# --target=i386-pc writing its core.img into the space between the GPT
# header and the first partition (standard "GPT + legacy BIOS grub" trick);
# no separate bios_grub partition is carved out here because
# grub-install's i386-pc target on a GPT disk needs a dedicated
# bios_grub partition, not just free space — added below as partition 3
# to avoid grub-install failing with "This GPT partition label has no BIOS
# Boot Partition"; kept tiny (1 MiB) and created AFTER the ESP/root so
# their offsets above stay round numbers.
parted --script "$RAW_DISK" \
    mkpart bios_grub 1000KiB 1MiB \
    set 3 bios_grub on

# ---------------------------------------------------------------------------
# Stage 2: loop-mount, format, copy rootfs
# ---------------------------------------------------------------------------
echo "== pack-vmware: attaching loop device =="
LOOP_DEV="$(losetup --show -f -P "$RAW_DISK")"
trap 'losetup -d "$LOOP_DEV" 2>/dev/null || true' EXIT

ESP_PART="${LOOP_DEV}p1"
ROOT_PART="${LOOP_DEV}p2"

mkfs.vfat -F32 -n IOLABESP "$ESP_PART"
mkfs.ext4 -L iolab-root "$ROOT_PART"

MNT="$WORK_DIR/mnt"
mkdir -p "$MNT"
mount "$ROOT_PART" "$MNT"
mkdir -p "$MNT/boot/efi"
mount "$ESP_PART" "$MNT/boot/efi"

echo "== pack-vmware: copying rootfs onto disk (this preserves owners/perms/xattrs) =="
# -a preserves everything build-rootfs.sh set up (symlinks for the systemd
# unit enablement, root's shadow entry, file modes). -x stays on this
# filesystem only (irrelevant here since ROOTFS_DIR is a plain directory
# tree with nothing else mounted under it, but cheap insurance).
rsync -aHAX --numeric-ids "$ROOTFS_DIR"/ "$MNT"/

# Bind-mount pseudo-filesystems for the chroot steps (kernel install,
# grub-install) that follow — both need /proc, /dev, /sys to work.
for fs in proc sys dev dev/pts; do
    mkdir -p "$MNT/$fs"
    mount --bind "/$fs" "$MNT/$fs"
done
trap 'for fs in dev/pts dev sys proc; do umount -lf "$MNT/$fs" 2>/dev/null || true; done; umount -lf "$MNT/boot/efi" 2>/dev/null || true; umount -lf "$MNT" 2>/dev/null || true; losetup -d "$LOOP_DEV" 2>/dev/null || true' EXIT

# ---------------------------------------------------------------------------
# Stage 3: kernel + grub inside the chroot
# ---------------------------------------------------------------------------
echo "== pack-vmware: installing kernel + grub =="

# fstab: root by LABEL (survives the loop device's /dev/loopNpM numbering,
# which is only stable for THIS build — inside the VMware guest the disk
# will enumerate as /dev/sda or /dev/nvme0n1 depending on the vmx's
# scsi/nvme controller choice, never as a loop device).
cat > "$MNT/etc/fstab" <<EOF
LABEL=iolab-root   /           ext4    errors=remount-ro   0 1
LABEL=IOLABESP     /boot/efi   vfat    umask=0077           0 2
EOF

chroot "$MNT" env DEBIAN_FRONTEND=noninteractive apt-get update
# linux-image-amd64 (the metapackage, tracks current stable kernel ABI) —
# NOT linux-image-cloud-amd64, because the cloud flavor sheds some drivers
# (e.g. it assumes virtio-only) and this appliance's vmx uses lsilogic
# SCSI + e1000 NIC (see files/iolab-appliance.vmx.tmpl), which the generic
# flavor supports out of the box without us having to also switch the vmx
# to virtio and chase virtio-scsi/virtio-net driver inclusion. Slightly
# bigger kernel; simpler, more compatible boot.
chroot "$MNT" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    linux-image-amd64 grub-pc

chroot "$MNT" update-grub

# --target=i386-pc: legacy BIOS boot, matching the vmx NOT setting
# firmware="efi" (see this script's header comment). --recheck avoids
# grub-install caching a device map from a previous run's loop device
# node, which would otherwise wedge every second invocation of this
# script.
chroot "$MNT" grub-install --target=i386-pc --recheck "$LOOP_DEV"

# ---------------------------------------------------------------------------
# Stage 4: static IP on the host-only NIC (the fixed-IP discovery mechanism
# documented in docs/providers.md and runtime/README.md)
# ---------------------------------------------------------------------------
echo "== pack-vmware: writing static host-only IP $HOSTONLY_IP/$HOSTONLY_PREFIX =="

# Overrides the DHCP-based files/01-no-default-route.network (installed by
# build-rootfs.sh) with a higher-priority static config, SPECIFICALLY for
# the vmware artifact only — the WSL2 tarball keeps the DHCP config as-is
# since WSL2's own vEthernet/NAT already provides deterministic localhost
# forwarding to 127.0.0.1:4000 with no IP-guessing needed on the Windows
# side (see docs/providers.md "wsl2"). Higher priority is achieved purely
# by filename ordering (systemd-networkd applies configs in lexical
# order, later file wins on conflicting settings within matched scope) —
# 90- sorts after 01-, and matches by Name= against ALL the same
# interfaces, but a *static* Address=/no DHCP=yes here takes precedence
# for the fields it sets.
cat > "$MNT/etc/systemd/network/90-vmware-hostonly.network" <<EOF
# Installed only into the VMware disk image by pack-vmware.sh (NOT part of
# the base rootfs build-rootfs.sh produces) — gives the guest a
# predictable, static address on the host-only NIC so the Windows-side
# vmware provider can connect to $HOSTONLY_IP:4000 immediately after
# \`vmrun start ... nogui\` returns, without polling
# \`vmrun getGuestIPAddress\` (which additionally requires VMware Tools /
# open-vm-tools running in the guest — NOT installed here, see
# files/iolab-appliance.vmx.tmpl's isolation.tools.hgfs.disable note).
[Match]
Name=en* eth*

[Network]
Address=$HOSTONLY_IP/$HOSTONLY_PREFIX
DHCP=no
# Still explicitly no [Route] section: a static address is not a route.
# The no-default-route posture from files/01-no-default-route.network
# carries over unchanged — see runtime/README.md's rationale section.
EOF

# ---------------------------------------------------------------------------
# Stage 5: cleanup + unmount
# ---------------------------------------------------------------------------
echo "== pack-vmware: cleaning apt cache inside image =="
chroot "$MNT" apt-get clean
rm -rf "$MNT"/var/lib/apt/lists/*

sync
for fs in dev/pts dev sys proc; do
    umount -lf "$MNT/$fs"
done
umount -lf "$MNT/boot/efi"
umount -lf "$MNT"
losetup -d "$LOOP_DEV"
trap - EXIT

# ---------------------------------------------------------------------------
# Stage 6: raw -> vmdk
# ---------------------------------------------------------------------------
echo "== pack-vmware: converting raw -> vmdk =="
# monolithicSparse: single-file, growable — matches the "sparse ~250-350MB
# actual, 4GB virtual" sizing note in runtime/README.md. VMware Workstation
# reads this subformat natively; no need for the split/streamOptimized
# variants (those matter for OVA/ESXi distribution, not for a vmrun-driven
# Workstation/Player appliance).
qemu-img convert -f raw -O vmdk -o subformat=monolithicSparse \
    "$RAW_DISK" "$OUT_VMDK"

# ---------------------------------------------------------------------------
# Stage 7: template the .vmx
# ---------------------------------------------------------------------------
echo "== pack-vmware: templating .vmx =="
VMDK_FILENAME="$(basename "$OUT_VMDK")"
sed \
    -e "s|@@HOSTONLY_IP@@|$HOSTONLY_IP|g" \
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
  # control endpoint: $HOSTONLY_IP:4000  (fixed; no polling needed)
  # fallback discovery (requires open-vm-tools, NOT installed by default here):
  vmrun -T ws getGuestIPAddress "$(basename "$OUT_VMX")"
  vmrun -T ws stop "$(basename "$OUT_VMX")" nogui

IMPORTANT: confirm the Workstation install's vmnet1 is actually configured
host-only (not bridged/NAT) — see files/iolab-appliance.vmx.tmpl's
ethernet0.vnet comment. Use VMware's "Virtual Network Editor"
(vmnetcfg.exe on Windows) to check/pin this once per Workstation install.
EOF
