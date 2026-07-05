#!/bin/bash
# iolbox-issue.sh — regenerate /etc/issue, the pre-login console splash shown
# on the VM console (VMware/VirtualBox window, qemu display, serial) BEFORE the
# login prompt. It answers the only question a new user has at that screen —
# "where do I point my browser?" — with the IOLBOX wordmark, the supervisor's
# service state, and the live Web GUI URL.
#
# Run at boot and every ~20s by iolbox-issue.timer so the IP and status stay
# current (getty re-reads /etc/issue for every login prompt, so a rewrite is
# reflected the next time the screen is drawn).
#
# Dependency-light on purpose: the slim rootfs ships iproute2 + systemd but not
# `hostname -I`, so IP discovery uses `ip` only.
set -u

SUPERVISOR=/opt/iolbox/supervisor

# Primary routable IPv4 (the address a browser on the LAN would actually use):
# the source address the kernel picks for a route to the outside world.
ip_routable() {
    ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.][0-9.]*\).*/\1/p' | head -1
}
# Fallback for a runtime with no default route yet (still-acquiring DHCP, or a
# host-only-only appliance): the first global-scope IPv4 on any interface.
ip_global() {
    ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1
}

IP="$(ip_routable)"; [ -n "$IP" ] || IP="$(ip_global)"
STATE="$(systemctl is-active iolbox-supervisor 2>/dev/null || echo unknown)"
HOST="$(hostname 2>/dev/null || cat /etc/hostname 2>/dev/null)"
# -version is side-effect-free (prints the build string and exits); empty on an
# older binary that predates the flag, in which case the build line is omitted.
VER="$(timeout 3 "$SUPERVISOR" -version 2>/dev/null | head -1)"

if [ -n "$IP" ]; then
    URL="http://$IP:4001"
else
    URL="(waiting for a network address...)"
fi

# Write atomically. NOTE: agetty interprets backslash escapes in /etc/issue, so
# the wordmark is a '#'-block font with NO backslashes — it renders verbatim.
tmp="$(mktemp)" || exit 0
{
    cat <<'BANNER'

   ##### ##### #     ####  ##### #   #
     #   #   # #     #   # #   #  # #
     #   #   # #     ####  #   #   #
     #   #   # #     #   # #   #  # #
   ##### ##### ##### ####  ##### #   #

BANNER
    printf '   Lightweight IOL + VPCS lab appliance'
    [ -n "$VER" ] && printf '   (build %s)' "$VER"
    printf '\n'
    echo   '   ----------------------------------------------------'
    printf '   Web GUI : %s\n' "$URL"
    printf '   Service : iolbox-supervisor is %s\n' "$STATE"
    printf '   Host    : %s\n' "$HOST"
    echo   '   ----------------------------------------------------'
    echo   '   Console login: root / iolbox   (no SSH - console only)'
    echo   '   The GUI has no password of its own - keep it on a'
    echo   '   network you trust.'
    echo
} > "$tmp"
mv "$tmp" /etc/issue
chmod 0644 /etc/issue
