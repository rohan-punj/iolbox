#!/usr/bin/env bash
# pack-lxc.sh — export the rootfs built by build-rootfs.sh as an
# unprivileged-Proxmox-LXC-compatible template tarball
# (iolab-ct-<ver>.tar.zst). See runtime/files/lxc/pct-create.md for the
# `pct create` recipe this artifact is meant to be consumed by.
#
# Must run AFTER build-rootfs.sh has produced runtime/build/rootfs/. Needs
# root ONLY to read a couple of root-owned files debootstrap can leave
# behind (same caveat pack-wsl.sh documents) — no loop devices, no chroot,
# no kernel/GRUB work, because an LXC container needs none of that: it
# shares the Proxmox host's kernel. This is the same "no kernel needed"
# shape as pack-wsl.sh, which is why this script is modeled directly on it
# rather than on pack-vmware.sh.
#
# --- Why a COPY, never the shared rootfs dir -------------------------------
# runtime/build/rootfs/ is shared by pack-wsl.sh and pack-vmware.sh too.
# Every CT-specific tweak below (dropping wsl.conf, adding LXC-only units,
# masking units) is applied to a throwaway rsync'd copy under
# runtime/build/lxc-work/rootfs/, never in place — mutating the shared dir
# would leak LXC-only state into the next WSL/VMware pack run in the same
# build session. (Same discipline pack-vmware.sh follows via its own
# WORK_DIR, just without the loop/mount machinery this artifact doesn't
# need.)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLAB_BUILD_DIR:-$SCRIPT_DIR/build}"
ROOTFS_DIR="$BUILD_DIR/rootfs"
WORK_DIR="$BUILD_DIR/lxc-work"
STAGE_DIR="$WORK_DIR/rootfs"
VERSION="dev"
OUT_TAR=""   # computed after VERSION is finalized, unless --out overrides it

usage() {
    cat <<EOF
Usage: $0 [--build-dir DIR] [--version VER] [--out FILE]

  --build-dir DIR   Root containing rootfs/ (default: $BUILD_DIR)
  --version VER     Version string embedded in the output filename
                     (default: $VERSION) -> iolab-ct-<VER>.tar.zst
  --out FILE        Output tarball path (default:
                     <build-dir>/iolab-ct-<VER>.tar.zst)
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; ROOTFS_DIR="$BUILD_DIR/rootfs"; WORK_DIR="$BUILD_DIR/lxc-work"; STAGE_DIR="$WORK_DIR/rootfs"; shift 2 ;;
        --version) VERSION="$2"; shift 2 ;;
        --out) OUT_TAR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ -z "$OUT_TAR" ]; then
    OUT_TAR="$BUILD_DIR/iolab-ct-$VERSION.tar.zst"
fi

if [ ! -d "$ROOTFS_DIR" ]; then
    echo "pack-lxc: no rootfs at $ROOTFS_DIR — run ./build-rootfs.sh first" >&2
    exit 1
fi

if ! command -v zstd >/dev/null 2>&1; then
    echo "pack-lxc: 'zstd' not found. Install it:" >&2
    echo "  apt-get install zstd" >&2
    exit 1
fi

echo "== pack-lxc: staging CT-specific rootfs copy at $STAGE_DIR =="
rm -rf "$WORK_DIR"
mkdir -p "$STAGE_DIR"

# -a preserves modes/symlinks/ownership exactly as build-rootfs.sh set them
# up (matches the rsync invocation pack-vmware.sh uses for the same
# "copy the proven rootfs onto the target-specific artifact" step).
rsync -aHAX --numeric-ids "$ROOTFS_DIR"/ "$STAGE_DIR"/

# ---------------------------------------------------------------------------
# Tweak 1: drop the WSL-only config. It's inert everywhere except under
# wslhost, but shipping it in an LXC template is just confusing cruft (a
# future maintainer finding /etc/wsl.conf inside a Proxmox CT and wondering
# what it's doing there) — same reasoning pack-vmware.sh could have applied
# but didn't bother to, since disk images are less commonly hand-inspected
# than a CT filesystem, which Proxmox users routinely `pct exec ... bash`
# into.
# ---------------------------------------------------------------------------
rm -f "$STAGE_DIR/etc/wsl.conf"

# ---------------------------------------------------------------------------
# Tweak 2: networking — DO NOT fight Proxmox's own hostname/network
# injection. See runtime/files/lxc/zz-lxc-eth-fallback.network's header for
# the full reasoning; short version:
#
#   Proxmox's `pct` detects this template runs systemd-networkd (no
#   ifupdown package present, /etc/systemd/network exists and is
#   populated — both true for this rootfs) and, for that case, injects its
#   OWN per-interface .network file (conventionally
#   /etc/systemd/network/eth0.network) built from the container's
#   `--net0 ...` config (static IP, VLAN, MTU, whatever the operator set).
#   That file is authoritative and this build has no way to know its
#   contents ahead of time.
#
#   The robust choice is therefore: ship NOTHING that competes for eth0 by
#   default, and add only a lowest-priority (zz- prefixed, so it always
#   sorts last among /etc/systemd/network/*.network) DHCP catch-all that
#   fires only if pct's injection didn't happen. This mirrors how every
#   other Debian-family Proxmox CT template behaves — none of them ship a
#   competing bespoke network config either, they all defer to pct.
#
# The rootfs's existing files/80-ethernet-dhcp.network (installed by
# build-rootfs.sh, matches Name=en*) is LEFT IN PLACE rather than deleted:
# it's harmless here (Proxmox CT NICs are eth0/eth1/..., never the
# predictable en* names — that renaming is done by udev, which does not
# meaningfully run in an unprivileged container, see Tweak 4 below), so it
# simply never matches anything and costs nothing to leave.
# ---------------------------------------------------------------------------
echo "== pack-lxc: installing lowest-priority eth0 DHCP fallback =="
install -m 0644 "$SCRIPT_DIR/files/lxc/zz-lxc-eth-fallback.network" \
    "$STAGE_DIR/etc/systemd/network/zz-lxc-eth-fallback.network"

# ---------------------------------------------------------------------------
# Tweak 2b: ensure /etc/network/ exists so Proxmox's create hook can write
# to it.
#
# This rootfs is systemd-networkd-only — build-rootfs.sh installs no
# ifupdown package, so debootstrap never creates the /etc/network/
# directory (there is no ifupdown to own it). BUT Proxmox's per-distro
# container setup plugin (PVE::LXC::Setup::Debian) unconditionally REWRITES
# /etc/network/interfaces during its post_create_hook, regardless of
# whether the container actually uses ifupdown — it opens
# "/etc/network/interfaces.tmp.<pid>" for atomic write and renames it into
# place. If /etc/network/ doesn't exist, that open() fails with
# "No such file or directory" and `pct create` aborts with
# "error in setup task PVE::LXC::Setup::post_create_hook", rolling back the
# whole container (the LV is created then removed) — i.e. the template is
# unusable via the documented `pct create` flow without this directory.
#
# VERIFIED on PVE 8.4.19: creating the CT from a rootfs lacking
# /etc/network/ fails exactly this way; adding an empty /etc/network/
# (with a stub interfaces file so the plugin has something to
# read/merge/rewrite) lets post_create_hook complete. The stub is inert at
# runtime: nothing in this rootfs runs ifupdown (no ifup/ifdown binaries,
# no networking.service), so /etc/network/interfaces is never consumed by
# the container itself — networkd + pct's injected eth0.network (or our
# zz- fallback) remain authoritative for actual addressing. This is purely
# to satisfy Proxmox's create-time contract, matching what every stock
# Debian-family CT template already ships.
# ---------------------------------------------------------------------------
echo "== pack-lxc: ensuring /etc/network/ exists for pct's create hook =="
install -d -m 0755 "$STAGE_DIR/etc/network"
install -d -m 0755 "$STAGE_DIR/etc/network/interfaces.d"
if [ ! -f "$STAGE_DIR/etc/network/interfaces" ]; then
    cat > "$STAGE_DIR/etc/network/interfaces" <<'IFACE_EOF'
# This file is present only to satisfy Proxmox's container create hook,
# which rewrites it. This rootfs uses systemd-networkd (no ifupdown), so
# nothing here is actually consumed at runtime. See pack-lxc.sh Tweak 2b.
auto lo
iface lo inet loopback

source /etc/network/interfaces.d/*
IFACE_EOF
    chmod 0644 "$STAGE_DIR/etc/network/interfaces"
fi

# ---------------------------------------------------------------------------
# Tweak 3: /etc/hosts 127.0.1.1 safety net.
#
# VERIFIED CLAIM: Proxmox's container setup (pve-container's per-distro
# hostname handling) manages /etc/hosts for unprivileged Debian-family CTs
# by default — it writes a "127.0.1.1 <hostname>" line matching whatever
# hostname `pct create --hostname` / `pct set --hostname` set, each time
# the hostname changes. That format is exactly what this runtime needs
# (see runtime/README.md's firstboot-iourc section: without it, every
# `sudo` the NAT node runs stalls ~10s on DNS resolution of the
# unresolvable hostname). So in the normal `pct create` flow, no extra
# work is needed here.
#
# What's shipped anyway: a SAFETY NET, not a duplicate mechanism — an LXC
# firstboot oneshot (iolab-firstboot-lxc-hosts.service ->
# firstboot-lxc-hosts.sh) that only appends a 127.0.1.1 line if one for the
# CURRENT hostname is missing. This covers the cases where Proxmox's
# management doesn't apply: a template imported/started outside the normal
# `pct create --hostname` flow, hand-built CT configs, or a future Proxmox
# version that changes this behavior. Cheap insurance against the exact
# failure mode (~10s sudo stalls) that was hard-won on the reference VM
# for the other two artifacts.
# ---------------------------------------------------------------------------
echo "== pack-lxc: installing /etc/hosts firstboot safety net =="
install -m 0755 -o root -g root "$SCRIPT_DIR/files/lxc/firstboot-lxc-hosts.sh" \
    "$STAGE_DIR/opt/iolab/firstboot-lxc-hosts.sh"
install -m 0644 "$SCRIPT_DIR/files/lxc/iolab-firstboot-lxc-hosts.service" \
    "$STAGE_DIR/etc/systemd/system/iolab-firstboot-lxc-hosts.service"
ln -sf ../iolab-firstboot-lxc-hosts.service \
    "$STAGE_DIR/etc/systemd/system/multi-user.target.wants/iolab-firstboot-lxc-hosts.service"

# ---------------------------------------------------------------------------
# Tweak 3b: make systemd-networkd start inside an UNPRIVILEGED CT.
#
# THE bug that otherwise leaves the CT with no network at all. Debian's
# stock systemd-networkd.service (bookworm, systemd 252) ships mount-
# namespace sandboxing directives — ProtectSystem=strict, ProtectHome=yes,
# ProtectProc=invisible, ProtectControlGroups=yes, ProtectKernelLogs=yes,
# ProtectKernelModules=yes, ProtectClock=yes, RestrictNamespaces=yes — that
# require setting up a private mount namespace (bind /run/systemd/unit-root,
# remount /proc, etc.). An unprivileged LXC container lacks the privilege to
# perform those mounts, so the unit dies at:
#
#   systemd-networkd.service: Failed to set up mount namespacing:
#     /run/systemd/unit-root/proc: Permission denied
#   systemd-networkd.service: Failed at step NAMESPACE ...
#   Main process exited, code=exited, status=226/NAMESPACE
#
# and after 5 rapid restarts systemd gives up ("start request repeated too
# quickly"). Result: networkd never runs, neither pct's injected
# eth0.network NOR our zz- DHCP fallback is ever applied, eth0 stays DOWN
# with no address, and the :4001 GUI is unreachable — a total-network
# failure of the artifact.
#
# Stock Debian Proxmox CT templates never hit this because they use
# ifupdown, not networkd; this networkd-based rootfs does, so it needs the
# fix. The fix is a drop-in that turns OFF exactly the sandboxing knobs that
# need mount-namespacing — networkd's actual job (talk to the kernel over
# rtnetlink, write /run/systemd/netif, run DHCP) needs none of that
# hardening to function; the hardening is pure defense-in-depth that an
# unprivileged CT already provides at the container boundary. VERIFIED on
# PVE 8.4.19: with this drop-in networkd goes active and eth0 gets its DHCP
# lease; without it, 226/NAMESPACE and no network.
# ---------------------------------------------------------------------------
echo "== pack-lxc: installing systemd-networkd unprivileged-CT drop-in =="
install -d -m 0755 "$STAGE_DIR/etc/systemd/system/systemd-networkd.service.d"
cat > "$STAGE_DIR/etc/systemd/system/systemd-networkd.service.d/override.conf" <<'NETWORKD_EOF'
# Installed by pack-lxc.sh (Tweak 3b). Disables the stock unit's mount-
# namespace sandboxing so systemd-networkd can start in an UNPRIVILEGED
# Proxmox LXC container, where those ProtectX=/RestrictNamespaces= knobs
# fail with status=226/NAMESPACE ("/run/systemd/unit-root/proc: Permission
# denied"). The container boundary already provides isolation; these
# in-unit hardening directives are redundant here and merely prevent the
# service from starting at all. Without this file the CT has NO network.
[Service]
ProtectProc=default
ProtectSystem=no
ProtectHome=no
ProtectControlGroups=no
ProtectKernelLogs=no
ProtectKernelModules=no
ProtectClock=no
RestrictNamespaces=no
PrivateMounts=no
NETWORKD_EOF
chmod 0644 "$STAGE_DIR/etc/systemd/system/systemd-networkd.service.d/override.conf"

# ---------------------------------------------------------------------------
# Tweak 4: mask units that can't (or shouldn't) run unprivileged inside an
# LXC container, so first boot doesn't spend time on units that will only
# fail, hang, or emit confusing journal noise.
#
#   systemd-networkd-wait-online.service — NOT explicitly enabled anywhere
#     in build-rootfs.sh, and nothing in this rootfs pulls in
#     network-online.target, so it should never actually run in practice.
#     Masked anyway (belt-and-suspenders): Proxmox CT boots are exactly
#     the environment where a surprise network-online.target dependency
#     (introduced by a future package update inside the image, e.g. if
#     open-vm-tools-equivalent tooling or a systemd bump changes default
#     WantedBy= wiring) turning into a multi-minute boot hang is most
#     likely to get blamed on "Proxmox is broken" instead of correctly
#     diagnosed. Masking costs nothing since nothing here needs it: DHCP
#     completion isn't a boot gate for this workload (the supervisor binds
#     0.0.0.0 and comes up fine before an address is even assigned).
#
#   systemd-udevd.service / systemd-udevd-{control,kernel}.socket — udev
#     device *management* (coldplug, renaming via .link/.rules) mostly
#     doesn't function in an unprivileged container (no CAP_MKNOD /
#     restricted netlink/uevent visibility from the container's netns),
#     but is NOT masked here: modern systemd (252, what bookworm ships)
#     auto-detects container virtualization via ConditionVirtualization
#     and a container-specific udev fallback path, and several other
#     enabled units (`iproute2`-adjacent tooling, `sudo`) don't depend on
#     udev having done anything. Masking it outright risks silently
#     breaking device-node creation this rootfs DOES rely on (e.g.
#     /dev/net/tun's bind-mount from the host still needs the mount
#     namespace's /dev to look sane) for a problem (boot hang) that, unlike
#     wait-online, doesn't actually manifest — udevd inside a container
#     starts, no-ops the parts it can't do, and exits/idles cleanly on
#     modern systemd. Left enabled; documented here so a maintainer who
#     later DOES see it hang has the context to reach for masking it too.
#
# Masked the same "offline symlink" way build-rootfs.sh enables units
# (`systemctl mask` needs a running systemd/dbus, which a chroot/rsync
# staging dir doesn't have) — masking is a symlink to /dev/null in
# /etc/systemd/system/, which is exactly what `systemctl mask` would
# produce for a unit that has no already-enabled symlinks to remove.
# ---------------------------------------------------------------------------
echo "== pack-lxc: masking systemd-networkd-wait-online.service =="
ln -sf /dev/null "$STAGE_DIR/etc/systemd/system/systemd-networkd-wait-online.service"

# ---------------------------------------------------------------------------
# Tweak 5: SETUP.md at the tarball root — the pct create recipe, so anyone
# who downloads/unpacks the artifact (or browses it via Proxmox's own
# "CT Template" content view, which lets you inspect a tarball's top level)
# finds the instructions right next to the files, without having to know
# this repo's docs/ layout. The canonical, more detailed copy lives at
# runtime/files/lxc/pct-create.md (source-controlled, easy to diff/review);
# this is a straight copy, not a divergent doc.
# ---------------------------------------------------------------------------
install -m 0644 "$SCRIPT_DIR/files/lxc/pct-create.md" "$STAGE_DIR/SETUP.md"

echo "== pack-lxc: creating $OUT_TAR from $STAGE_DIR =="

# Same tar contract as pack-wsl.sh: archive the CONTENTS of the staged
# rootfs (--directory + "."), not the parent dir, so top-level entries are
# etc/, opt/, usr/, ... — this is also exactly the layout `pct create`
# expects from a CT template tarball (it unpacks straight onto the
# container's rootfs). --numeric-owner for the same reason pack-wsl.sh uses
# it: preserve uid/gid numbers as-is; the archive's embedded owner *names*
# don't need to resolve against this builder's /etc/passwd, only the
# shipped rootfs's own /etc/passwd matters once unpacked. --xattrs is
# likewise deliberately omitted (see pack-wsl.sh's comment on the same
# flag) — no capability xattrs are set by anything this script installs,
# and dropping xattrs sidesteps the same class of import-path quirks noted
# there.
#
# zstd compression (-T0 auto-detects CPU count; -19 is a reasonably strong
# level appropriate for a release artifact built once and downloaded many
# times — pct's own documented/expected template formats include
# .tar.zst).
tar \
    --create \
    --directory "$STAGE_DIR" \
    --numeric-owner \
    --sort=name \
    . \
    | zstd -T0 -19 -q -o "$OUT_TAR"

echo "== pack-lxc: done =="
ls -lh "$OUT_TAR"
cat <<EOF

Proxmox-side (see runtime/files/lxc/pct-create.md for the full recipe):

  pct create <vmid> local:vztmpl/$(basename "$OUT_TAR") \\
      --unprivileged 1 \\
      --hostname iolab \\
      --cores 2 --memory 4096 --swap 512 \\
      --net0 name=eth0,bridge=vmbr0,ip=dhcp \\
      --features nesting=0

Then append to /etc/pve/lxc/<vmid>.conf (no pct-flag equivalent):

  lxc.cgroup2.devices.allow: c 10:200 rwm
  lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file

  pct start <vmid>
  # browse to http://<ct-ip>:4001  (NO AUTH — LAN/firewalled access only)

REMINDER: set --hostname at creation time and never rename afterward — the
IOL license (iourc) is minted at first boot from this hostname + the CT's
hostid; renaming later invalidates it.
EOF
