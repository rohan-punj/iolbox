#!/bin/sh
# /opt/iolbox/firstboot-lxc-hosts.sh — LXC-only defensive fixup for
# /etc/hosts, installed by pack-lxc.sh.
#
# WHY THIS EXISTS: build-rootfs.sh bakes a hardcoded "127.0.1.1 iolbox" line
# (see its Stage 6) because the WSL2/VMware artifacts always boot with the
# hostname "iolbox" (pinned via wsl.conf / the baked /etc/hostname). Proxmox
# is different: `pct create ... --hostname <whatever>` lets (and expects)
# the operator to pick the CT's hostname, and Proxmox rewrites
# /etc/hostname AND (per Proxmox's own container setup, pve-container's
# Debian.pm) appends a matching "127.0.1.1 <hostname>" line to /etc/hosts
# on every boot for unprivileged Debian-family CTs — this is standard,
# well-established Proxmox behavior, not something this script should
# fight or duplicate.
#
# This script is a SAFETY NET, not the primary mechanism: it runs after
# Proxmox's own hostname injection (see the Before=/After= ordering in
# iolbox-firstboot-iourc.service, which this reuses) and only ACTS if the
# expected "127.0.1.1 <current-hostname>" line is missing — e.g. a CT
# built from a raw tarball via `pct create ... --unprivileged 1
# --hostname iolbox` outside the normal Proxmox UI flow, or a future
# Proxmox version that stops managing /etc/hosts. Without SOME
# "127.0.1.1 <hostname>" resolvable line, every `sudo` the NAT node runs
# stalls ~10s on DNS (see runtime/README.md's firstboot-iourc section);
# this is the same class of bug that line was originally added to avoid.
#
# POSIX sh — same portability constraint as firstboot-iourc.sh.
set -eu

HOSTS_FILE="/etc/hosts"
CURRENT_HOSTNAME="$(cat /etc/hostname 2>/dev/null || hostname)"

if [ -z "$CURRENT_HOSTNAME" ]; then
    echo "firstboot-lxc-hosts: /etc/hostname empty/unreadable, skipping" >&2
    exit 0
fi

if grep -qE "^127\.0\.1\.1[[:space:]]+${CURRENT_HOSTNAME}([[:space:]]|\$)" "$HOSTS_FILE" 2>/dev/null; then
    # Proxmox (or a previous run of this script) already did the right
    # thing for the CURRENT hostname. Nothing to do — importantly, this
    # also means a mid-life `pct set --hostname` rename (which the whole
    # rest of this system asks users never to do, since it invalidates the
    # already-minted iourc) doesn't get silently patched over here either;
    # this script only fills a genuine gap, not papers over a rename.
    exit 0
fi

echo "firstboot-lxc-hosts: no 127.0.1.1 line for '$CURRENT_HOSTNAME', appending" >&2
printf '127.0.1.1\t%s\n' "$CURRENT_HOSTNAME" >> "$HOSTS_FILE"
