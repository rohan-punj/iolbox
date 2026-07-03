#!/usr/bin/env bash
# uninstall.sh — removes what install.sh installed. Run as root.
#
#   sudo ./uninstall.sh [--yes]
#
# --yes skips the interactive prompt before deleting user data
# (images/ and labs/ under /opt/iolab) — use for scripted/CI teardown.
# Without it, user data is only removed after an explicit y/N confirmation;
# everything else (binaries, units, sysctl drop-in, /etc/iolab) is removed
# unconditionally since none of it is user data.
set -euo pipefail

PREFIX="/opt/iolab"
ASSUME_YES=0

usage() {
    cat <<EOF
Usage: sudo $0 [options]

  --prefix DIR   Install directory to remove (default: $PREFIX)
  --yes          Don't prompt before deleting images/ and labs/ (user data)
  -h, --help     This help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix) PREFIX="$2"; shift 2 ;;
        --yes) ASSUME_YES=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "uninstall.sh: must run as root." >&2
    echo "Try: sudo $0 $*" >&2
    exit 1
fi

echo "uninstall.sh: stopping + disabling units"
systemctl stop iolab-supervisor.service 2>/dev/null || true
systemctl stop iolab-firstboot-iourc.service 2>/dev/null || true
systemctl disable iolab-supervisor.service 2>/dev/null || true
systemctl disable iolab-firstboot-iourc.service 2>/dev/null || true

echo "uninstall.sh: removing systemd units + drop-in"
rm -f /etc/systemd/system/iolab-supervisor.service
rm -f /etc/systemd/system/iolab-firstboot-iourc.service
rm -rf /etc/systemd/system/iolab-supervisor.service.d
systemctl daemon-reload
systemctl reset-failed iolab-supervisor.service iolab-firstboot-iourc.service 2>/dev/null || true

echo "uninstall.sh: removing sysctl drop-in"
rm -f /etc/sysctl.d/99-iolab.conf

echo "uninstall.sh: removing /etc/iolab (bind.env)"
rm -rf /etc/iolab

if [ ! -d "$PREFIX" ]; then
    echo "uninstall.sh: $PREFIX does not exist, nothing more to do."
    exit 0
fi

# User data lives under $PREFIX/images (uploaded IOL/VPCS images, can be
# hundreds of MB to GBs) and $PREFIX/labs (durable lab documents — the
# thing a user would be most upset to lose silently). Everything else
# under $PREFIX (the supervisor/vpcs binaries, run/ scratch state, the
# generated iourc) is disposable and removed without asking.
HAS_USER_DATA=0
for d in "$PREFIX/images" "$PREFIX/labs"; do
    if [ -d "$d" ] && [ -n "$(ls -A "$d" 2>/dev/null)" ]; then
        HAS_USER_DATA=1
    fi
done

if [ "$HAS_USER_DATA" -eq 1 ] && [ "$ASSUME_YES" -ne 1 ]; then
    echo ""
    echo "uninstall.sh: $PREFIX/images and/or $PREFIX/labs contain data"
    echo "  (uploaded IOL/VPCS images and/or saved labs)."
    printf "  Delete this user data too? [y/N] "
    read -r REPLY
    case "$REPLY" in
        y|Y|yes|YES) : ;;
        *)
            echo "uninstall.sh: keeping $PREFIX/images and $PREFIX/labs."
            echo "  Removing everything else under $PREFIX..."
            find "$PREFIX" -mindepth 1 -maxdepth 1 \
                ! -name images ! -name labs -exec rm -rf {} +
            echo "uninstall.sh: done (user data preserved at $PREFIX/images, $PREFIX/labs)"
            exit 0
            ;;
    esac
fi

echo "uninstall.sh: removing $PREFIX"
rm -rf "$PREFIX"

echo "uninstall.sh: done"
