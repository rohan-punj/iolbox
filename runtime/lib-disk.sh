#!/usr/bin/env bash
# lib-disk.sh — SHARED disk-image builder for the iolab runtime appliances.
#
# This library takes a finished rootfs directory (built by build-rootfs.sh)
# and produces a bootable GPT RAW disk image: legacy-BIOS GRUB + a Debian
# kernel, root on ext4 (LABEL=iolab-root), and the per-NIC systemd-networkd
# configs the VMware/OVA appliances need. It leaves FORMAT CONVERSION to the
# caller — pack-vmware.sh converts the raw disk to monolithicSparse vmdk,
# pack-ova.sh will convert it to a streamOptimized vmdk, pack-qemu.sh to a
# qcow2. All three share the exact same disk contents this library builds.
#
# ---------------------------------------------------------------------------
# INTERFACE CONTRACT (stable — pack-ova.sh / pack-qemu.sh consume this verbatim)
# ---------------------------------------------------------------------------
#
# Consume it with:
#     source "$SCRIPT_DIR/lib-disk.sh"
#     build_raw_disk --rootfs DIR --out RAW [options]
#
# build_raw_disk options:
#   --rootfs DIR       (required) finished rootfs directory to copy onto the disk
#   --out RAW          (required) output raw-disk path. The file is created
#                      (or overwritten) by this function. Its parent dir must
#                      exist. The caller converts this raw file to its target
#                      format afterward, then may delete it.
#   --files-dir DIR    Directory holding the networkd templates / vmx files
#                      (default: "<dir of lib-disk.sh>/files"). Only used to
#                      resolve nothing today — networkd configs are written
#                      inline — but reserved so a future consumer can override
#                      the source of drop-in configs without editing this lib.
#   --size-mb N        Virtual disk size in MiB (default: 16384 = 16 GB). The
#                      vmdk/qcow2 stays sparse, so this is a ceiling, not a
#                      bytes-on-disk cost — the empty tail of the fs converts
#                      to a handful of MiB. What DOES cost real bytes is
#                      deleted-file residue in used-then-freed blocks; see
#                      --zerofree below.
#   --hostonly-mac MAC MAC for the host-only NIC's 10-hostonly.network match
#                      (default: 00:50:56:3f:ab:01). MUST match the vmx template
#                      ethernet0 for VMware. Ignored if --no-nic-configs.
#   --nat-mac MAC      MAC for the NAT NIC's 20-nat.network match
#                      (default: 00:50:56:3f:ab:02). MUST match vmx ethernet1.
#                      Ignored if --no-nic-configs.
#
#   Size levers (ALL opt-in; omitting every one reproduces the historical
#   byte-comparable default build):
#   --zerofree         Zero every free block of the root ext4 (via `zerofree`,
#                      or a dd zero-fill fallback) just before unmount, so
#                      `qemu-img convert` sparse-skips them. THIS IS THE
#                      EFFECTIVE size lever: it measured the vmdk from
#                      ~1.55 GiB down to ~0.81 GiB. The bulk of the "extra"
#                      bytes in the default image is NOT the ext4 inode table
#                      (an empty 16 GB ext4 converts to ~5 MiB) — it is the
#                      apt-downloaded kernel/grub/open-vm-tools .debs that are
#                      unpacked, then `apt-get clean`ed: ext4 frees those
#                      blocks but never zeroes them, so the converter still
#                      copies their non-zero contents. Requires the `zerofree`
#                      package on the builder (falls back to dd if absent).
#                      Default: off (leaves freed blocks intact = current size).
#   --inode-count N    Pass `mkfs.ext4 -N N` to cap the inode-table inode count
#                      (e.g. 65536 is ample for the image-library use case).
#                      NOTE: measured to have negligible effect on the sparse
#                      vmdk size (the default itable already converts near-
#                      sparse) — kept for completeness / future tuning, but
#                      --zerofree is the lever that matters. Default: unset.
#   --lazy-init        Pass `mkfs.ext4 -E lazy_itable_init=1,lazy_journal_init=1`
#                      (itable/journal finished lazily in-guest on first mount).
#                      NOTE: measured NOT to shrink the sparse vmdk (and, paired
#                      with a used rootfs, marginally grew it) — do not rely on
#                      it for size; use --zerofree. Default: off (eager init).
#
#   Content levers (opt-in):
#   --no-i386          Exclude the i386 multiarch libraries from the disk copy
#                      (rsync --exclude of the i386 loader + lib dirs). Use for
#                      64-bit-IOL-only channels. Does NOT re-run build-rootfs;
#                      it filters an existing rootfs at copy time. Default: keep
#                      i386 (matches a default build-rootfs rootfs).
#   --no-vmtools       Skip installing open-vm-tools in the chroot. OVA/qemu
#                      targets don't want the VMware guest agent; the VMware
#                      target does (it powers `vmrun getGuestIPAddress`).
#                      Default: install open-vm-tools.
#   --no-nic-configs   Skip writing the MAC-matched 10-hostonly/20-nat networkd
#                      configs. The rootfs's generic 80-ethernet-dhcp.network
#                      (installed by build-rootfs) then governs all NICs. Use
#                      for qemu where NIC MACs aren't the VMware fixed pair.
#                      Default: write the two MAC-matched configs.
#
# Requirements (checked by build_raw_disk): run as root; qemu-img, parted,
# mkfs.ext4, rsync, chroot, losetup on PATH; grub-pc is installed INSIDE the
# chroot so the builder host doesn't need grub tooling.
#
# On success the raw disk at --out is a complete bootable image; on any error
# the function tears down its loop device + bind mounts via an EXIT trap and
# returns non-zero (callers run under `set -euo pipefail`, so this aborts them).
# ---------------------------------------------------------------------------

# Guard against double-sourcing.
if [ -n "${_IOLAB_LIB_DISK_SOURCED:-}" ]; then
    return 0 2>/dev/null || true
fi
_IOLAB_LIB_DISK_SOURCED=1

# build_raw_disk — see INTERFACE CONTRACT above.
build_raw_disk() {
    # --- defaults (reproduce the historical pack-vmware behavior) ----------
    local rootfs_dir="" out_raw="" files_dir=""
    local size_mb=16384
    local mac_hostonly="00:50:56:3f:ab:01"
    local mac_nat="00:50:56:3f:ab:02"
    local inode_count=""          # unset -> mkfs default
    local lazy_init=0
    local zerofree=0
    local no_i386=0
    local no_vmtools=0
    local no_nic_configs=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --rootfs)        rootfs_dir="$2"; shift 2 ;;
            --out)           out_raw="$2"; shift 2 ;;
            --files-dir)     files_dir="$2"; shift 2 ;;
            --size-mb)       size_mb="$2"; shift 2 ;;
            --hostonly-mac)  mac_hostonly="$2"; shift 2 ;;
            --nat-mac)       mac_nat="$2"; shift 2 ;;
            --inode-count)   inode_count="$2"; shift 2 ;;
            --lazy-init)     lazy_init=1; shift ;;
            --zerofree)      zerofree=1; shift ;;
            --no-i386)       no_i386=1; shift ;;
            --no-vmtools)    no_vmtools=1; shift ;;
            --no-nic-configs) no_nic_configs=1; shift ;;
            *) echo "build_raw_disk: unknown option: $1" >&2; return 2 ;;
        esac
    done

    if [ -z "$rootfs_dir" ] || [ -z "$out_raw" ]; then
        echo "build_raw_disk: --rootfs and --out are required." >&2
        return 2
    fi
    if [ "$(id -u)" -ne 0 ]; then
        echo "build_raw_disk: must run as root (loop/mount/chroot/grub-install)." >&2
        return 1
    fi
    if [ ! -d "$rootfs_dir" ]; then
        echo "build_raw_disk: no rootfs at $rootfs_dir" >&2
        return 1
    fi

    if [ -z "$files_dir" ]; then
        local _lib_dir
        _lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        files_dir="$_lib_dir/files"
    fi

    local tool
    for tool in qemu-img parted mkfs.ext4 rsync chroot losetup; do
        command -v "$tool" >/dev/null 2>&1 || {
            echo "build_raw_disk: required tool '$tool' not found." >&2
            echo "  apt-get install qemu-utils parted e2fsprogs rsync" >&2
            return 1
        }
    done

    local work_dir mnt
    work_dir="$(dirname "$out_raw")"
    mnt="$work_dir/lib-disk-mnt"
    mkdir -p "$mnt"

    # -----------------------------------------------------------------------
    # Stage 1: raw disk + GPT partition table (bios_grub + root)
    # -----------------------------------------------------------------------
    echo "== lib-disk: creating ${size_mb}MB raw disk at $out_raw =="
    qemu-img create -f raw "$out_raw" "${size_mb}M"

    # p1 bios_grub 1MiB..2MiB (holds grub core.img for i386-pc); p2 root ext4.
    parted --script "$out_raw" \
        mklabel gpt \
        mkpart bios_grub 1MiB 2MiB \
        set 1 bios_grub on \
        mkpart root ext4 2MiB 100%

    # -----------------------------------------------------------------------
    # Stage 2: loop-mount, format, copy rootfs
    # -----------------------------------------------------------------------
    echo "== lib-disk: attaching loop device =="
    local loop_dev root_part
    loop_dev="$(losetup --show -f -P "$out_raw")"
    # shellcheck disable=SC2064
    trap "losetup -d '$loop_dev' 2>/dev/null || true" RETURN
    root_part="${loop_dev}p2"

    # losetup -P asks the kernel to scan the partition table, but the
    # /dev/loopNp2 device node is created asynchronously by udev — mkfs.ext4
    # can otherwise race ahead and die with "The file ...p2 does not exist"
    # (observed intermittently across back-to-back disk builds). Wait for the
    # node to materialise before formatting.
    udevadm settle 2>/dev/null || true
    for _ in $(seq 1 50); do
        [ -b "$root_part" ] && break
        sleep 0.2
        partprobe "$loop_dev" 2>/dev/null || true
    done
    if [ ! -b "$root_part" ]; then
        echo "lib-disk: partition node $root_part never appeared after losetup -P" >&2
        exit 1
    fi

    # ext4 mkfs — size levers applied here. Defaults (no --inode-count,
    # no --lazy-init) reproduce the historical `mkfs.ext4 -q -L iolab-root`.
    local mkfs_opts=(-q -L iolab-root)
    if [ "$lazy_init" -eq 1 ]; then
        mkfs_opts+=(-E lazy_itable_init=1,lazy_journal_init=1)
    fi
    if [ -n "$inode_count" ]; then
        mkfs_opts+=(-N "$inode_count")
    fi
    echo "== lib-disk: mkfs.ext4 ${mkfs_opts[*]} =="
    mkfs.ext4 "${mkfs_opts[@]}" "$root_part"

    mount "$root_part" "$mnt"
    # Re-arm trap to also unmount bind mounts + root on RETURN/error.
    # shellcheck disable=SC2064
    trap "for fs in dev/pts dev sys proc; do umount -lf '$mnt/\$fs' 2>/dev/null || true; done; umount -lf '$mnt' 2>/dev/null || true; losetup -d '$loop_dev' 2>/dev/null || true" RETURN

    echo "== lib-disk: copying rootfs onto disk =="
    # -aHAX --numeric-ids preserves symlinks (systemd unit enablement),
    # root's shadow entry, file modes, xattrs/ACLs, hardlinks.
    local rsync_excludes=()
    if [ "$no_i386" -eq 1 ]; then
        echo "== lib-disk: --no-i386: excluding i386 multiarch libs from copy =="
        # The i386 multiarch tree lives under these paths; the loader is
        # /lib/ld-linux.so.2 (and the i386 lib dir). Excluding them yields a
        # 64-bit-only disk. build-rootfs may or may not have installed them;
        # excludes are harmless if the paths are absent.
        rsync_excludes+=(
            --exclude '/usr/lib/i386-linux-gnu'
            --exclude '/lib/i386-linux-gnu'
            --exclude '/lib/ld-linux.so.2'
            --exclude '/usr/lib32'
            --exclude '/lib32'
        )
    fi
    rsync -aHAX --numeric-ids "${rsync_excludes[@]}" "$rootfs_dir"/ "$mnt"/

    # Bind-mount pseudo-filesystems for the chroot steps (kernel install,
    # grub-install) that follow — all need /proc, /dev, /sys.
    local fs
    for fs in proc sys dev dev/pts; do
        mkdir -p "$mnt/$fs"
        mount --bind "/$fs" "$mnt/$fs"
    done

    # -----------------------------------------------------------------------
    # Stage 3: kernel + grub (+ optional open-vm-tools) inside the chroot
    # -----------------------------------------------------------------------
    echo "== lib-disk: installing kernel + grub${no_vmtools:+ (no open-vm-tools)} =="

    # fstab: root by LABEL (inside the guest the disk enumerates as /dev/sda,
    # never as this build's loop device).
    cat > "$mnt/etc/fstab" <<EOF
LABEL=iolab-root   /   ext4   errors=remount-ro   0 1
EOF

    # The shipped rootfs has an EMPTY resolv.conf — apt in this chroot needs a
    # real one. Restored to empty in stage 5.
    cp -L /etc/resolv.conf "$mnt/etc/resolv.conf"

    chroot "$mnt" env DEBIAN_FRONTEND=noninteractive apt-get update

    # linux-image-amd64 (generic metapackage) — NOT the cloud flavor, which
    # sheds the lsilogic/e1000e drivers this appliance's vmx uses. grub-pc for
    # legacy BIOS boot. open-vm-tools only for VMware targets.
    local pkgs=(linux-image-amd64 grub-pc)
    if [ "$no_vmtools" -eq 0 ]; then
        pkgs+=(open-vm-tools)
    fi
    chroot "$mnt" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        "${pkgs[@]}"

    chroot "$mnt" update-grub

    # --target=i386-pc: legacy BIOS boot (vmx does NOT set firmware="efi").
    # --recheck avoids a cached device map from a prior loop-device node.
    chroot "$mnt" grub-install --target=i386-pc --recheck "$loop_dev"

    # -----------------------------------------------------------------------
    # Stage 4: per-NIC networkd configs, matched by fixed MACs (optional)
    # -----------------------------------------------------------------------
    if [ "$no_nic_configs" -eq 0 ]; then
        echo "== lib-disk: writing MAC-matched networkd configs =="

        # Host-only NIC: DHCP lease from VMware's vmnetdhcp (subnet varies per
        # install). NO gateway/DNS from this segment — the default route must
        # stay on the NAT NIC or the NAT node MASQUERADEs into a dead end.
        cat > "$mnt/etc/systemd/network/10-hostonly.network" <<EOF
# Written by lib-disk.sh. MAC matches ethernet0 in the vmx template.
[Match]
MACAddress=$mac_hostonly

[Network]
DHCP=ipv4

[DHCPv4]
UseGateway=false
UseDNS=false
UseRoutes=false
EOF

        # NAT NIC: full DHCP — its gateway IS the NAT node's outbound path.
        cat > "$mnt/etc/systemd/network/20-nat.network" <<EOF
# Written by lib-disk.sh. MAC matches ethernet1 in the vmx template.
[Match]
MACAddress=$mac_nat

[Network]
DHCP=ipv4
EOF
    else
        echo "== lib-disk: --no-nic-configs: leaving generic 80-ethernet-dhcp.network in place =="
    fi

    # -----------------------------------------------------------------------
    # Stage 5: cleanup + unmount
    # -----------------------------------------------------------------------
    echo "== lib-disk: cleaning apt cache inside image =="
    chroot "$mnt" apt-get clean
    rm -rf "$mnt"/var/lib/apt/lists/*
    : > "$mnt/etc/resolv.conf"

    sync
    for fs in dev/pts dev sys proc; do
        umount -lf "$mnt/$fs"
    done

    # Zero the free blocks so `qemu-img convert` sparse-skips them. This is
    # the effective size lever (see header). zerofree needs the fs mounted
    # READ-ONLY; remount ro, run it, then unmount. Fall back to a dd zero-fill
    # (fill a file with zeros, delete it) if zerofree isn't installed — same
    # net effect, just slower and needs transient free space.
    if [ "$zerofree" -eq 1 ]; then
        echo "== lib-disk: zeroing free space (zerofree) =="
        sync
        if command -v zerofree >/dev/null 2>&1; then
            mount -o remount,ro "$root_part" "$mnt"
            zerofree "$root_part"
        else
            echo "   zerofree not found; using dd zero-fill fallback"
            dd if=/dev/zero of="$mnt/.iolab-zerofill" bs=1M 2>/dev/null || true
            rm -f "$mnt/.iolab-zerofill"
            sync
        fi
    fi

    umount -lf "$mnt"
    losetup -d "$loop_dev"
    trap - RETURN
    rmdir "$mnt" 2>/dev/null || true

    echo "== lib-disk: raw disk ready at $out_raw =="
}
