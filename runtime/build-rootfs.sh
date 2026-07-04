#!/usr/bin/env bash
# build-rootfs.sh — build the ONE Debian-slim (bookworm) rootfs that backs
# BOTH the WSL2 tarball (pack-wsl.sh) and the VMware appliance
# (pack-vmware.sh). See runtime/README.md for the two-packages-one-rootfs
# picture and docs/providers.md for the contract this script implements.
#
# MUST run as root (debootstrap needs to create device nodes / chroot) on a
# real Linux box or CI runner. Not runnable on this Windows dev machine —
# see runtime/README.md "Why these scripts don't run on this machine".
#
# --- Sizing (honest breakdown, see runtime/README.md "Sizing") -------------
#   debootstrap --variant=minbase bookworm base ........... ~100-120 MB
#   + libc6:i386 multiarch (32-bit IOL support, REQUIRED) ..  ~15-35 MB
#     (libc6:i386 itself is small; the i386 dynamic linker +
#      dependent libs like libgcc-s1:i386 pull in the rest)
#   + iproute2, systemd-{,timesyncd}, ca-certificates-free
#     minimal runtime deps ..................................  ~15-25 MB
#   + VPCS binary (fetch-vpcs.sh output) ...................  ~1 MB
#   + supervisor binary (Go, static, linux/amd64) ..........  ~10-15 MB
#   - docs/locale/man-page stripping (see strip_docs below) . -30 to -50 MB
#   ------------------------------------------------------------------------
#   Net: roughly 150-230 MB uncompressed. The task brief's "<~150 MB"
#   target is achievable ONLY by dropping libc6:i386 (i.e. only if 64-bit
#   IOL images are in scope) — with i386 multiarch included, as REQUIRED by
#   docs/providers.md ("32-bit IOL images need i386 glibc"), expect to land
#   in the 180-230 MB range. This tradeoff is called out explicitly rather
#   than silently blown through; see the --no-i386 flag below if a smaller,
#   64-bit-only build is ever wanted for a specific release channel.
# -----------------------------------------------------------------------

set -euo pipefail

# ---------------------------------------------------------------------------
# Parameters (all overridable via flags; see usage() below)
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLBOX_BUILD_DIR:-$SCRIPT_DIR/build}"
ROOTFS_DIR="$BUILD_DIR/rootfs"

SUITE="bookworm"                              # Debian 12, pinned deliberately (see "Reproducibility" below)
MIRROR="${DEBIAN_MIRROR:-https://deb.debian.org/debian}"
SUPERVISOR_BIN="$SCRIPT_DIR/../supervisor/bin/supervisor-linux-amd64"   # matches PLAN.md repo layout + .gitignore's /supervisor/bin/
VPCS_BIN="$BUILD_DIR/vpcs/vpcs"               # fetch-vpcs.sh's output path
INCLUDE_I386=1                                # docs/providers.md requires libc6:i386; opt out with --no-i386

usage() {
    cat <<EOF
Usage: $0 [options]

  --build-dir DIR         Output root (default: $BUILD_DIR)
  --supervisor-bin PATH   Path to the built Go supervisor binary
                           (default: $SUPERVISOR_BIN)
  --vpcs-bin PATH         Path to a prebuilt vpcs binary
                           (default: $VPCS_BIN — run ./fetch-vpcs.sh first,
                           or drop a binary there by hand on an airgapped
                           builder; see fetch-vpcs.sh's header comment)
  --suite NAME             Debian suite (default: $SUITE)
  --mirror URL             Debian mirror / snapshot URL (default: $MIRROR)
  --no-i386                Skip libc6:i386 multiarch (smaller, 64-bit-IOL-only build)
  -h, --help               This help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --build-dir) BUILD_DIR="$2"; ROOTFS_DIR="$BUILD_DIR/rootfs"; shift 2 ;;
        --supervisor-bin) SUPERVISOR_BIN="$2"; shift 2 ;;
        --vpcs-bin) VPCS_BIN="$2"; shift 2 ;;
        --suite) SUITE="$2"; shift 2 ;;
        --mirror) MIRROR="$2"; shift 2 ;;
        --no-i386) INCLUDE_I386=0; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    echo "build-rootfs.sh must run as root (debootstrap needs to mknod/chroot)." >&2
    echo "Try: sudo $0 $*" >&2
    exit 1
fi

# Prefer mmdebstrap when present: it's faster, doesn't need root for the
# unpack phase (though we're root anyway here for the chroot steps that
# follow), and produces smaller output by default. Fall back to debootstrap,
# which is universally available and is what the task brief names first.
DEBOOTSTRAP_TOOL=""
if command -v mmdebstrap >/dev/null 2>&1; then
    DEBOOTSTRAP_TOOL="mmdebstrap"
elif command -v debootstrap >/dev/null 2>&1; then
    DEBOOTSTRAP_TOOL="debootstrap"
else
    echo "Neither mmdebstrap nor debootstrap found. Install one:" >&2
    echo "  apt-get install debootstrap    # or" >&2
    echo "  apt-get install mmdebstrap" >&2
    exit 1
fi
echo "== build-rootfs: using $DEBOOTSTRAP_TOOL =="

if [ ! -f "$SUPERVISOR_BIN" ]; then
    cat >&2 <<EOF
build-rootfs: supervisor binary not found at:
  $SUPERVISOR_BIN

The whole point of this runtime is to autostart the supervisor (see
runtime/README.md), so building a rootfs without it is refused rather than
silently shipping a broken appliance. Build it first:

  cd supervisor && GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor

...or pass --supervisor-bin /path/to/binary.
EOF
    exit 1
fi

if [ ! -x "$VPCS_BIN" ]; then
    cat >&2 <<EOF
build-rootfs: vpcs binary not found or not executable at:
  $VPCS_BIN

Run ./fetch-vpcs.sh first (needs network access to clone+build), or drop a
prebuilt linux/amd64 vpcs binary at that path by hand (airgapped builder —
see fetch-vpcs.sh's header comment for the "manual drop path"). Refusing to
build a rootfs silently missing VPCS, since PLAN.md lists it as a first-
class node type alongside IOL.

...or pass --vpcs-bin /path/to/vpcs.
EOF
    exit 1
fi

mkdir -p "$BUILD_DIR"
if [ -d "$ROOTFS_DIR" ]; then
    echo "build-rootfs: removing previous rootfs at $ROOTFS_DIR"
    rm -rf "$ROOTFS_DIR"
fi
mkdir -p "$ROOTFS_DIR"

# ---------------------------------------------------------------------------
# Reproducibility note
# ---------------------------------------------------------------------------
# $SUITE is pinned to a codename ("bookworm"), not "stable", so this script
# doesn't silently start pulling Debian 13 the day trixie releases. For
# byte-for-byte reproducible rebuilds (e.g. verifying a release artifact),
# point --mirror at a Debian snapshot.debian.org URL instead of the rolling
# mirror, e.g.:
#   --mirror https://snapshot.debian.org/archive/debian/20260101T000000Z
# This is documented rather than defaulted-to because snapshot.debian.org
# is slower and rate-limited; day-to-day builds should use the live mirror.
# (Same tradeoff already made for the PNetLab v8 line — see
# pnetlab-build-streamline-impl memory topic re: SOURCE_DATE_EPOCH noise
# when comparing two builds against different mirror snapshots.)

# ---------------------------------------------------------------------------
# Stage 1: base bootstrap (amd64)
# ---------------------------------------------------------------------------
echo "== build-rootfs: bootstrapping $SUITE (amd64) into $ROOTFS_DIR =="

# Package set kept intentionally minimal — every package here is either
# required to boot (systemd-sysv, udev), required for networking config
# (iproute2 — systemd-networkd owns config, see
# files/80-ethernet-dhcp.network), required by IOL itself (libssl3 — some
# IOL images dlopen libcrypto), or required by the supervisor's runtime
# spawns (audited against exec.Command call sites in supervisor/internal):
#   sudo      extnet ALWAYS shells `sudo -n ip/iptables/sysctl` (even as
#             root — root passes sudo auth with no config, but the BINARY
#             must exist; detect_linux.go probes `sudo -n true`)
#   procps    /usr/sbin/sysctl (extnet flips net.ipv4.ip_forward for NAT)
#   iptables  NAT node MASQUERADE + FORWARD rules (commands.go)
#   tcpdump   the bridge-fabric capture (internal/bcap) shells
#             `sudo -n tcpdump -i br-<link> -w -` to tee a link's frames to the
#             GUI capture tab; without it captures come up EMPTY (the always-on
#             watcher uses in-process AF_PACKET so it still works — which is why
#             only the capture tab, not the watcher, was affected).
#   coreutils' hostid is used by -gen-iourc (in minbase already)
# ca-certificates is omitted on purpose: the runtime itself makes no
# outbound TLS connections (the NAT node MASQUERADEs lab traffic at L3;
# no TLS termination here).
# dbus: lets systemd-logind actually run, which handles the ACPI power
# button — without it a hypervisor-initiated shutdown (qemu QMP
# system_powerdown, vmrun soft stop, OVA guest shutdown) is never acted
# on in-guest and hosts fall back to hard-kill after their grace period.
BASE_INCLUDE="systemd,systemd-sysv,udev,dbus,iproute2,iputils-ping,libssl3,openssh-client,sudo,procps,iptables,tcpdump"
# openssh-client (not -server): the `remote` provider (docs/providers.md)
# is SSH-based but connects INTO an existing user-supplied Linux box, not
# into this appliance — this runtime is reached via the control protocol
# over TCP (docs/protocol.md), never SSH. openssh-client is included only
# because it's a handy debugging tool for the runtime maintainers (scp'ing
# a log out, etc.); drop it if that's judged not worth the ~2 MB.

if [ "$DEBOOTSTRAP_TOOL" = "mmdebstrap" ]; then
    mmdebstrap \
        --arch=amd64 \
        --variant=minbase \
        --include="$BASE_INCLUDE" \
        "$SUITE" "$ROOTFS_DIR" "$MIRROR"
else
    debootstrap \
        --arch=amd64 \
        --variant=minbase \
        --include="$BASE_INCLUDE" \
        "$SUITE" "$ROOTFS_DIR" "$MIRROR"
fi

# ---------------------------------------------------------------------------
# Stage 2: i386 multiarch for 32-bit IOL images
# ---------------------------------------------------------------------------
# This is the single most important non-default step in this whole script:
# many IOL images (older L2/L3 builds) are 32-bit ELF and dynamically link
# against 32-bit glibc. Without libc6:i386, those images fail to exec at
# all inside the runtime (ENOEXEC/"No such file or directory" — the classic
# missing-interpreter symptom, easy to misdiagnose as a permissions issue).
# docs/providers.md is explicit about this requirement; do not remove.
if [ "$INCLUDE_I386" -eq 1 ]; then
    echo "== build-rootfs: adding i386 multiarch (libc6:i386) =="
    # The chroot's apt needs working DNS; debootstrap/mmdebstrap don't
    # reliably leave a resolv.conf behind. Copy the builder's (dereference
    # the systemd-resolved symlink). Stage 6 truncates it again for the
    # shipped image.
    cp -L /etc/resolv.conf "$ROOTFS_DIR/etc/resolv.conf"
    chroot "$ROOTFS_DIR" dpkg --add-architecture i386
    chroot "$ROOTFS_DIR" env DEBIAN_FRONTEND=noninteractive apt-get update
    chroot "$ROOTFS_DIR" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        libc6:i386 libssl3:i386
else
    echo "== build-rootfs: --no-i386 given, skipping libc6:i386 (32-bit IOL images will NOT run) =="
fi

# ---------------------------------------------------------------------------
# Stage 3: strip docs/locales/caches to hit the size target
# ---------------------------------------------------------------------------
echo "== build-rootfs: stripping docs/locales/caches =="

# dpkg's own path-exclude mechanism would have been cleaner than a
# post-hoc rm, but debootstrap's initial unpack doesn't consistently honor
# /etc/dpkg/dpkg.cfg.d excludes written before it runs (variant-dependent),
# so we strip after the fact instead — simpler to reason about and verify.
rm -rf "$ROOTFS_DIR"/usr/share/doc/*
rm -rf "$ROOTFS_DIR"/usr/share/man/*
rm -rf "$ROOTFS_DIR"/usr/share/info/*
rm -rf "$ROOTFS_DIR"/usr/share/lintian/*
# Keep en_US.UTF-8 + C.UTF-8 (systemd/journald want a real locale present),
# drop every other locale's compiled data. locale-gen output, not the
# locale package itself, is the size cost worth removing.
find "$ROOTFS_DIR/usr/share/locale" -mindepth 1 -maxdepth 1 \
    ! -name 'en*' ! -name 'C.UTF-8' -exec rm -rf {} + 2>/dev/null || true
# apt/dpkg caches — large and worthless in a shipped image; apt-get clean
# is the "proper" way but a couple of these dirs accumulate outside its
# reach across the two apt-get invocations above.
chroot "$ROOTFS_DIR" apt-get clean
rm -rf "$ROOTFS_DIR"/var/lib/apt/lists/*
rm -rf "$ROOTFS_DIR"/var/cache/apt/*

# ---------------------------------------------------------------------------
# Stage 4: /opt/iolbox layout + binaries
# ---------------------------------------------------------------------------
echo "== build-rootfs: installing /opt/iolbox =="

install -d -m 0755 "$ROOTFS_DIR/opt/iolbox"
install -d -m 0755 "$ROOTFS_DIR/opt/iolbox/images"   # image library (uploads land here via the GUI's POST /api/upload/image)
install -d -m 0755 "$ROOTFS_DIR/opt/iolbox/run"      # supervisor's per-lab runtime state (sockets, NETMAPs, nvram scratch) — swept by prestart-clean.sh
install -d -m 0755 "$ROOTFS_DIR/opt/iolbox/labs"     # durable lab-document store (-labs-dir); seed labs materialize here on first connect when empty

# The supervisor binary itself (a FILE at /opt/iolbox/supervisor).
install -m 0755 -o root -g root "$SUPERVISOR_BIN" "$ROOTFS_DIR/opt/iolbox/supervisor"

# VPCS binary. Lives in /opt/iolbox, which the supervisor unit puts on PATH
# (the supervisor spawns `vpcs` by bare name).
install -m 0755 -o root -g root "$VPCS_BIN" "$ROOTFS_DIR/opt/iolbox/vpcs"

# firstboot-iourc.sh (called by both the systemd unit and the non-systemd
# fallback init script) + the ExecStartPre stale-state sweep.
install -m 0755 -o root -g root "$SCRIPT_DIR/files/firstboot-iourc.sh" "$ROOTFS_DIR/opt/iolbox/firstboot-iourc.sh"
install -m 0755 -o root -g root "$SCRIPT_DIR/files/prestart-clean.sh" "$ROOTFS_DIR/opt/iolbox/prestart-clean.sh"

# ---------------------------------------------------------------------------
# Stage 5: systemd units + non-systemd fallback
# ---------------------------------------------------------------------------
echo "== build-rootfs: installing systemd units =="

install -d -m 0755 "$ROOTFS_DIR/etc/systemd/system"
install -m 0644 "$SCRIPT_DIR/files/iolbox-supervisor.service" \
    "$ROOTFS_DIR/etc/systemd/system/iolbox-supervisor.service"
install -m 0644 "$SCRIPT_DIR/files/iolbox-firstboot-iourc.service" \
    "$ROOTFS_DIR/etc/systemd/system/iolbox-firstboot-iourc.service"

# Enable both units the "offline" way (symlink into multi-user.target.wants)
# rather than `systemctl enable` inside a chroot, which needs a running
# systemd/dbus and is flaky under chroot on some hosts. A plain symlink is
# exactly what `systemctl enable` would have produced for a unit whose
# [Install] section is just WantedBy=multi-user.target (true for both units
# here), so this is equivalent, not a shortcut.
install -d -m 0755 "$ROOTFS_DIR/etc/systemd/system/multi-user.target.wants"
ln -sf ../iolbox-supervisor.service \
    "$ROOTFS_DIR/etc/systemd/system/multi-user.target.wants/iolbox-supervisor.service"
ln -sf ../iolbox-firstboot-iourc.service \
    "$ROOTFS_DIR/etc/systemd/system/multi-user.target.wants/iolbox-firstboot-iourc.service"

# Non-systemd fallback (see files/iolbox-init.sh's header comment for when
# this path is actually used — qemu-compat initrd boots, mainly).
install -d -m 0755 "$ROOTFS_DIR/etc/init.d"
install -m 0755 "$SCRIPT_DIR/files/iolbox-init.sh" "$ROOTFS_DIR/etc/init.d/iolbox"

# ---------------------------------------------------------------------------
# Stage 6: networking — generic DHCP fallback; the VMware artifact gets
# per-NIC (MAC-matched) configs layered on top by pack-vmware.sh. The old
# no-default-route posture is retired — the NAT node needs an outbound
# default route (see files/80-ethernet-dhcp.network's HISTORY note).
# ---------------------------------------------------------------------------
echo "== build-rootfs: configuring networkd (DHCP fallback) =="

install -d -m 0755 "$ROOTFS_DIR/etc/systemd/network"
install -m 0644 "$SCRIPT_DIR/files/80-ethernet-dhcp.network" \
    "$ROOTFS_DIR/etc/systemd/network/80-ethernet-dhcp.network"

# Enable systemd-networkd.
chroot "$ROOTFS_DIR" systemctl enable systemd-networkd.service 2>/dev/null || \
    ln -sf /lib/systemd/system/systemd-networkd.service \
        "$ROOTFS_DIR/etc/systemd/system/multi-user.target.wants/systemd-networkd.service"

install -d -m 0755 "$ROOTFS_DIR/etc/sysctl.d"
install -m 0644 "$SCRIPT_DIR/files/99-iolbox.conf" "$ROOTFS_DIR/etc/sysctl.d/99-iolbox.conf"

# Static hostname — deliberately boring/fixed, and LOAD-BEARING twice
# over: (1) the firstboot iourc is keyed "iolbox = <key>" and IOL checks it
# against the running hostname; (2) the "127.0.1.1 iolbox" /etc/hosts line
# below keeps the hostname resolvable — without it every `sudo` call
# (extnet runs several per NAT-node start) stalls ~10s on DNS resolution.
# Hard-won on the reference VM; do not remove either side.
echo "iolbox" > "$ROOTFS_DIR/etc/hostname"
cat > "$ROOTFS_DIR/etc/hosts" <<'EOF'
127.0.0.1   localhost
127.0.1.1   iolbox
::1         localhost ip6-localhost ip6-loopback
EOF

# Empty resolv.conf, not a symlink to systemd-resolved's stub — the
# runtime itself does no name resolution (the NAT node hands lab nodes
# public DNS servers directly in DHCP leases; the supervisor never looks
# anything up). An empty-but-present file avoids glibc resolver
# warnings/retries that an entirely missing file can trigger. Maintainers
# debugging inside the VM can `echo nameserver 1.1.1.1 >/etc/resolv.conf`
# to make apt work (same idiom as the PNetLab airgapped hosts).
: > "$ROOTFS_DIR/etc/resolv.conf"

# ---------------------------------------------------------------------------
# Stage 7: WSL2-specific inert config (harmless on the VMware artifact)
# ---------------------------------------------------------------------------
install -m 0644 "$SCRIPT_DIR/files/wsl.conf" "$ROOTFS_DIR/etc/wsl.conf"

# ---------------------------------------------------------------------------
# Stage 8: root console login for support/debugging
# ---------------------------------------------------------------------------
# root password = "iolbox": fixed, documented, deliberately non-secret.
# There is NO sshd in this image, so the only way to use it is the VM
# console (VMware window / `wsl -d iolbox`) — acceptable for a single-
# tenant lab appliance, and it turns "the appliance won't come up" from a
# black box into a normal login-and-look debugging session. (The scaffold
# originally locked root entirely; that made the first real boot failure
# undiagnosable without rebuilding the image.)
echo 'root:iolbox' | chroot "$ROOTFS_DIR" chpasswd

echo "== build-rootfs: done =="
du -sh "$ROOTFS_DIR" 2>/dev/null || true
echo "Rootfs ready at: $ROOTFS_DIR"
echo "Next: ./pack-wsl.sh and/or ./pack-vmware.sh (or just ./build-all.sh)"
