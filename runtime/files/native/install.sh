#!/usr/bin/env bash
# install.sh — installs iolbox onto ANY systemd x86-64 glibc Linux server
# (bare metal, cloud VM, on-prem hypervisor guest — the "native" packaging
# target; see docs/providers.md's `remote` provider, which is the Windows
# side's client for a box this script has set up).
#
# Run as root from inside the extracted tarball directory:
#   sudo ./install.sh [--bind local|all] [--prefix /opt/iolbox]
#
# What it does, in order:
#   1. refuse to run as non-root; refuse if no systemd
#   2. check/install runtime deps (apt-get if available; else print+exit)
#   3. copy binaries + support files into place, install systemd units
#   4. write /etc/iolbox/bind.env (local or all) + the bind drop-in
#   5. enable + start iolbox-firstboot-iourc.service and iolbox-supervisor.service
#   6. run the firstboot iourc step immediately (don't make the operator wait
#      for a reboot to get a working IOL license)
#   7. print the GUI URL + the no-auth security warning
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFIX="/opt/iolbox"
BIND_MODE="local"

usage() {
    cat <<EOF
Usage: sudo $0 [options]

  --bind local|all   Listener exposure for the GUI/WS bridge (:4001), native
                      telnet consoles (:9000+), and native Wireshark capture
                      tees (:5500+). (default: local)
                        local -> 127.0.0.1 only (reach it via SSH tunnel or
                                 a browser running on this same machine)
                        all   -> 0.0.0.0 (reachable from your LAN/VPN/tunnel
                                 endpoint — NONE of these listeners have
                                 authentication; see the warning printed at
                                 the end of this script)
  --prefix DIR        Install directory (default: $PREFIX)
  -h, --help          This help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --bind) BIND_MODE="$2"; shift 2 ;;
        --prefix) PREFIX="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

case "$BIND_MODE" in
    local|all) : ;;
    *) echo "install.sh: --bind must be 'local' or 'all', got '$BIND_MODE'" >&2; exit 1 ;;
esac

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    echo "install.sh: must run as root (installs systemd units, binds to /opt, may apt-get install)." >&2
    echo "Try: sudo $0 $*" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    echo "install.sh: this host does not appear to be running systemd." >&2
    echo "  iolbox's autostart units (iolbox-supervisor.service,"  >&2
    echo "  iolbox-firstboot-iourc.service) require systemd. Refusing to"  >&2
    echo "  install — a non-systemd install would silently never autostart."  >&2
    exit 1
fi

ARCH="$(uname -m)"
if [ "$ARCH" != "x86_64" ]; then
    echo "install.sh: WARNING - uname -m reports '$ARCH', not x86_64." >&2
    echo "  The bundled supervisor/vpcs binaries are linux/amd64 only and will" >&2
    echo "  fail to exec on any other architecture. Continuing anyway (in case" >&2
    echo "  this check is wrong for your setup), but expect ENOEXEC if it isn't." >&2
fi

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------
# Same set build-rootfs.sh audits into the VMware/WSL rootfs (see that
# script's header comment): sudo (extnet always shells `sudo -n ...`, even
# as root, since detect_linux.go probes `sudo -n true`), iproute2 (`ip`),
# procps (`sysctl`), iptables (NAT node MASQUERADE/FORWARD rules).
NEEDED_BINS="sudo ip sysctl iptables"
MISSING=""
for b in $NEEDED_BINS; do
    command -v "$b" >/dev/null 2>&1 || MISSING="$MISSING $b"
done

if [ -n "$MISSING" ]; then
    echo "install.sh: missing required commands:$MISSING"
    if command -v apt-get >/dev/null 2>&1; then
        echo "install.sh: apt-get found, installing: iproute2 iptables procps sudo"
        DEBIAN_FRONTEND=noninteractive apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y iproute2 iptables procps sudo
        for b in $NEEDED_BINS; do
            command -v "$b" >/dev/null 2>&1 || {
                echo "install.sh: '$b' still missing after apt-get install; aborting." >&2
                exit 1
            }
        done
    else
        cat >&2 <<EOF
install.sh: no apt-get on this system — install these packages yourself,
then re-run this script:
  iproute2   (provides: ip)
  iptables   (provides: iptables)
  procps     (provides: sysctl)
  sudo       (provides: sudo — required even though you're root right now;
              the supervisor's NAT-node code path always shells out through
              sudo, see runtime/build-rootfs.sh's BASE_INCLUDE comment)
EOF
        exit 1
    fi
else
    echo "install.sh: all required commands present ($NEEDED_BINS)"
fi

# libssl3 / equivalent: some IOL images dlopen libcrypto. Debian 13 uses
# libssl3t64 for this runtime. Not hard-checked
# here (package name varies too much across distros — libssl3 on
# Debian/Ubuntu, openssl-libs on Fedora/RHEL, etc.) — if IOL nodes fail to
# start with a dlopen error, install your distro's OpenSSL 3.x runtime lib.
echo "install.sh: NOTE - if IOL nodes fail to start with a libcrypto/dlopen"
echo "  error, install your distro's OpenSSL 3.x shared library package"
echo "  (Debian/Ubuntu: libssl3; Debian 13: libssl3t64; Fedora/RHEL: openssl-libs)."

# ---------------------------------------------------------------------------
# Hostname sanity check
# ---------------------------------------------------------------------------
# The IOL license (iourc) is keyed to hostid+hostname; IOL checks it against
# the RUNNING hostname (see files/firstboot-iourc.sh / runtime/README.md).
# Cloud images commonly randomize the hostname on every boot (cloud-init's
# default), which would silently invalidate the license on the next reboot.
CUR_HOSTNAME="$(hostname)"
case "$CUR_HOSTNAME" in
    *.novalocal|ip-*|*-instance|localhost|localhost.localdomain)
        LOOKS_EPHEMERAL=1 ;;
    *)
        # A pure hex/decimal-looking hostname (some cloud images use the
        # instance id) is also a common ephemeral pattern.
        case "$CUR_HOSTNAME" in
            *[!a-zA-Z0-9.-]*) LOOKS_EPHEMERAL=0 ;;
            [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]*) LOOKS_EPHEMERAL=1 ;;
            *) LOOKS_EPHEMERAL=0 ;;
        esac
        ;;
esac

if [ "${LOOKS_EPHEMERAL:-0}" -eq 1 ] || command -v cloud-init >/dev/null 2>&1; then
    cat >&2 <<EOF
install.sh: WARNING - this host's hostname ('$CUR_HOSTNAME') looks like it
  may be cloud-init-managed or auto-generated. iolbox's IOL license (iourc)
  is minted from this hostname + the host's hostid and IOL validates it
  against the RUNNING hostname on every node start.

  DO NOT let this hostname change after install (cloud-init's default
  "randomize hostname per boot" behavior WILL break IOL licensing). If this
  is a cloud instance, pin the hostname permanently first:
    sudo hostnamectl set-hostname <a-name-that-never-changes>
    # and disable cloud-init's hostname management, e.g. by adding
    #   preserve_hostname: true
    # to /etc/cloud/cloud.cfg (path/mechanism varies by distro/image)

  If the license breaks later (IOL nodes refuse to start, console shows a
  license error), regenerate it:
    sudo rm -f /opt/iolbox/.iourc-generated /opt/iolbox/iourc
    sudo systemctl restart iolbox-firstboot-iourc.service
    sudo systemctl restart iolbox-supervisor.service
EOF
fi

# ---------------------------------------------------------------------------
# ioltool service account
# ---------------------------------------------------------------------------
# PC/VPCS nodes (the "tool" package) launch their pack GUI as a dedicated,
# unprivileged account named ioltool — supervisor/internal/tool/detect_linux.go's
# capability probe requires user.Lookup("ioltool") to succeed, and both its
# ambientCapTransition and unixProxy checks fail without it, which surfaces as
# "runtime does not support PC nodes" in the GUI. Every other packaging target
# (OVA/QEMU/LXC/WSL) gets this account for free because runtime/build-rootfs.sh
# creates it inside the shared rootfs image before those targets are packed. A
# native install (this script) runs on the operator's own box, which was never
# built from that rootfs, so create it here too — idempotently, since this
# script is safe to re-run.
if ! id ioltool >/dev/null 2>&1; then
    echo "install.sh: creating ioltool service account (used by PC/VPCS pack GUIs)"
    useradd -r -M -s /usr/sbin/nologin ioltool || {
        echo "install.sh: could not create the ioltool service account" >&2
        exit 1
    }
fi
id ioltool >/dev/null 2>&1 || {
    echo "install.sh: ioltool account still missing after useradd" >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Install files
# ---------------------------------------------------------------------------
echo "install.sh: installing to $PREFIX"

install -d -m 0755 "$PREFIX"
install -d -m 0755 "$PREFIX/images"
install -d -m 0755 "$PREFIX/run"
install -d -m 0755 "$PREFIX/labs"

# Learning-tool packs (PC/VPCS, aaa, webserver, httpclient, syslog, netsvc,
# secbench) — staged by pack-native.sh under tools/packs/. Installed at the
# supervisor's hardcoded default (server.go's ToolPacksDir), /opt/iolbox/tools
# /packs, same as iolbox-toollaunch above: nothing currently plumbs --prefix
# through to it. Absent when the tarball was built with --no-packs; PC/VPCS
# and the other tool nodes simply won't be available in that case (the
# supervisor logs a warning and starts with an empty tool-pack registry, same
# as any other pack-load failure).
if [ -d "$SCRIPT_DIR/tools/packs" ]; then
    echo "install.sh: installing tool packs"
    install -d -m 0755 /opt/iolbox/tools
    rm -rf /opt/iolbox/tools/packs
    cp -R "$SCRIPT_DIR/tools/packs" /opt/iolbox/tools/packs
    chown -R root:root /opt/iolbox/tools/packs
    find /opt/iolbox/tools/packs -type d -exec chmod 0755 {} +
    find /opt/iolbox/tools/packs -type f -name '*.json' -exec chmod 0644 {} +
    find /opt/iolbox/tools/packs -type f ! -name '*.json' -exec chmod 0755 {} +
else
    echo "install.sh: NOTE - no tools/packs bundled in this tarball; PC/VPCS and other tool nodes will not be available."
fi

# Binaries: 0755, not 0644 — a non-executable supervisor binary fails
# fork/exec at ExecStart with a confusing "Permission denied", a mistake
# already made once in this project (see runtime/README.md history /
# build-rootfs.sh's `install -m 0755` on the same two files).
install -m 0755 -o root -g root "$SCRIPT_DIR/bin/supervisor" "$PREFIX/supervisor"
install -m 0755 -o root -g root "$SCRIPT_DIR/bin/vpcs" "$PREFIX/vpcs"

# PC/VPCS nodes' ambient-capability transition (supervisor/internal/tool/launch.go)
# execs this helper at a hardcoded path, /opt/iolbox/iolbox-toollaunch, regardless
# of --prefix; install it there to match rather than under $PREFIX.
install -d -m 0755 /opt/iolbox
install -m 0755 -o root -g root "$SCRIPT_DIR/bin/iolbox-toollaunch" /opt/iolbox/iolbox-toollaunch

install -m 0755 -o root -g root "$SCRIPT_DIR/opt-iolbox/firstboot-iourc.sh" "$PREFIX/firstboot-iourc.sh"
install -m 0755 -o root -g root "$SCRIPT_DIR/opt-iolbox/prestart-clean.sh" "$PREFIX/prestart-clean.sh"

echo "install.sh: installing systemd units"
install -m 0644 "$SCRIPT_DIR/systemd/iolbox-supervisor.service" \
    /etc/systemd/system/iolbox-supervisor.service
install -m 0644 "$SCRIPT_DIR/systemd/iolbox-firstboot-iourc.service" \
    /etc/systemd/system/iolbox-firstboot-iourc.service

install -d -m 0755 /etc/systemd/system/iolbox-supervisor.service.d
install -m 0644 "$SCRIPT_DIR/systemd/10-bind.conf" \
    /etc/systemd/system/iolbox-supervisor.service.d/10-bind.conf

echo "install.sh: writing /etc/iolbox/bind.env (--bind $BIND_MODE)"
install -d -m 0755 /etc/iolbox
if [ "$BIND_MODE" = "all" ]; then
    install -m 0644 "$SCRIPT_DIR/systemd/bind.env.all" /etc/iolbox/bind.env
else
    install -m 0644 "$SCRIPT_DIR/systemd/bind.env.local" /etc/iolbox/bind.env
fi

echo "install.sh: installing sysctl drop-in"
install -d -m 0755 /etc/sysctl.d
install -m 0644 "$SCRIPT_DIR/etc/99-iolbox.conf" /etc/sysctl.d/99-iolbox.conf
sysctl --system >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# Enable + start
# ---------------------------------------------------------------------------
echo "install.sh: enabling + starting units"
systemctl daemon-reload
systemctl enable iolbox-firstboot-iourc.service iolbox-supervisor.service

# Run firstboot iourc generation NOW rather than waiting for the units to
# get there on their own — gives the operator an immediate pass/fail signal
# instead of having to go check `systemctl status` / journalctl afterwards.
systemctl start iolbox-firstboot-iourc.service
if [ -f "$PREFIX/iourc" ]; then
    echo "install.sh: IOL license generated at $PREFIX/iourc"
else
    echo "install.sh: WARNING - $PREFIX/iourc was not created; check:" >&2
    echo "    journalctl -u iolbox-firstboot-iourc.service" >&2
fi

systemctl start iolbox-supervisor.service

# Small settle window before checking status, so a fast-crashing unit
# (bad binary, port already in use) is caught here instead of leaving the
# operator to discover it later via a silently-unreachable GUI.
sleep 2
if ! systemctl is-active --quiet iolbox-supervisor.service; then
    echo "install.sh: WARNING - iolbox-supervisor.service is not active." >&2
    echo "  Check: journalctl -u iolbox-supervisor -e" >&2
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
if [ "$BIND_MODE" = "all" ]; then
    PRIMARY_IP="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1;i<=NF;i++) if ($i=="src") print $(i+1)}' | head -n1)"
    [ -n "$PRIMARY_IP" ] || PRIMARY_IP="<this-host-ip>"
    GUI_URL="http://$PRIMARY_IP:4001"
else
    GUI_URL="http://127.0.0.1:4001 (reach it via an SSH tunnel: ssh -L 4001:127.0.0.1:4001 <user>@<this-host>)"
fi

cat <<EOF

============================================================
 iolbox installed.

 GUI:  $GUI_URL

 Bind mode: $BIND_MODE
   (change later: edit /etc/iolbox/bind.env, then
    systemctl restart iolbox-supervisor.service)

 SECURITY WARNING: the GUI/WS bridge (:4001), native telnet
 consoles (:9000+), and native Wireshark capture tees (:5500+)
 have NO AUTHENTICATION. Anyone who can reach these ports can
 control every lab node on this host. LAN/VPN/SSH-tunnel access
 only — never expose this host's iolbox ports to the public
 internet (no port-forwarding a cloud instance's :4001, no
 opening it in a public-facing security group).

 Logs:   journalctl -u iolbox-supervisor -f
 Status: systemctl status iolbox-supervisor iolbox-firstboot-iourc
============================================================
EOF
