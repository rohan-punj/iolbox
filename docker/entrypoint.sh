#!/bin/sh
# /opt/iolbox/entrypoint.sh — container PID 1 for the iolbox runtime image.
#
# No systemd here (see Dockerfile's stage-2 comment: systemd/udev are
# dropped from the image). This script folds together what the appliance
# splits across three systemd units — iolbox-firstboot-iourc.service,
# prestart-clean.sh (ExecStartPre), and iolbox-supervisor.service's
# ExecStart — into one linear sequence, then execs the supervisor as PID 1
# so it receives SIGTERM directly from `docker stop` (no init-shim needed;
# the supervisor's own signal.NotifyContext(os.Interrupt, syscall.SIGTERM)
# handles shutdown, and KillMode=control-group's job — not orphaning
# IOL/VPCS children — is instead the container boundary itself: killing
# pid 1 tears down every process in the container's pid namespace).
#
# POSIX sh on purpose, matching firstboot-iourc.sh / prestart-clean.sh.
set -eu

IOLBOX_DIR="/opt/iolbox"
SUPERVISOR="$IOLBOX_DIR/supervisor"

# ---------------------------------------------------------------------------
# Step 1: prestart cleanup (adapted from runtime/files/prestart-clean.sh).
#
# In the appliance this exists because a SIGKILLed previous supervisor run
# can leave stale iolnatN/iolmgmtN devices and per-lab run-dir state behind
# across a *reboot* of the same long-lived VM. A container is different:
# `docker run`/`docker compose up` normally starts in a fresh network
# namespace every time, so tap devices can't leak in from a previous
# container's crash — but /opt/iolbox/run CAN be non-empty on restart if it
# lives in a named volume (docker/compose.yml gives it one, alongside
# images/ and labs/, so an `iolbox upgrade` — pull a new image, recreate the
# container — doesn't lose uploaded images or lab documents). Sweep it the
# same way the appliance does, for the same reason: run/ is scratch state,
# never the durable store.
# ---------------------------------------------------------------------------
echo "entrypoint: prestart cleanup" >&2
for i in 0 1 2 3 4 5 6 7 8 9 10 11; do
    ip tuntap del dev "iolnat$i" mode tap 2>/dev/null || true
    ip link delete "iolmgmt$i" type macvtap 2>/dev/null || true
done
rm -rf "$IOLBOX_DIR/run"/* 2>/dev/null || true

# ---------------------------------------------------------------------------
# Step 2: sysctl (adapted from runtime/files/99-iolbox.conf).
#
# The appliance ships these as a /etc/sysctl.d drop-in applied at VM BOOT
# time by systemd-sysctl, before any container/namespace concept exists.
# There is no such boot phase here — sysctl.d drop-ins are inert in a
# container unless something applies them, so do it explicitly at
# entrypoint time instead. Requires --cap-add=NET_ADMIN (see
# docker/compose.yml); without it these writes fail — caught here with a
# warning rather than `set -e` aborting the whole container, since a
# missing NET_ADMIN cap will also make the NAT node fail loudly and
# separately when a lab actually starts one (same non-fatal posture as
# firstboot-iourc.sh's "don't hard-fail boot over this").
# ---------------------------------------------------------------------------
echo "entrypoint: sysctl (requires --cap-add=NET_ADMIN)" >&2
sysctl -w net.ipv4.ip_forward=0 >/dev/null 2>&1 || echo "entrypoint: WARNING sysctl net.ipv4.ip_forward failed (missing NET_ADMIN?)" >&2
sysctl -w net.ipv6.conf.all.forwarding=0 >/dev/null 2>&1 || true
sysctl -w net.ipv4.conf.all.accept_redirects=0 >/dev/null 2>&1 || true
sysctl -w net.ipv4.conf.all.send_redirects=0 >/dev/null 2>&1 || true
sysctl -w net.ipv6.conf.all.accept_redirects=0 >/dev/null 2>&1 || true
# ip_forward is intentionally left OFF at start, same as the appliance —
# the supervisor's NAT node (extnet) flips it on itself via `sudo -n sysctl
# -w net.ipv4.ip_forward=1` the moment a NAT endpoint starts (see
# supervisor/internal/extnet/commands.go). IOL's own inter-node links are
# userspace UDP tunnels and never touch this setting either way.

# ---------------------------------------------------------------------------
# Step 3: hostid + iourc (adapted from runtime/files/firstboot-iourc.sh).
#
# THE PERSISTENCE PROBLEM THIS SOLVES: supervisor's hostID() (see
# supervisor/cmd/supervisor/main.go) shells out to the `hostid` command
# first, falling back to reading /etc/hostid directly. Linux's hostid(1)/
# gethostid(3) return the contents of /etc/hostid if that file exists; if
# it does NOT exist, glibc DERIVES a value from the machine's current IP
# address instead (classic gethostid(3) fallback behavior). A container's
# IP changes on every recreate (new bridge network, DHCP-ish IPAM
# assignment) — so on a stock debian:bookworm-slim base with no
# /etc/hostid file, hostid would silently change across `docker compose up
# -d` cycles, silently invalidating the license every time the container
# is recreated (not just restarted-in-place).
#
# DECISION: generate /etc/hostid ONCE and persist it in the same named
# volume as iourc/images/labs (docker/compose.yml mounts
# /opt/iolbox/state), so both the hostid seed AND the derived iourc survive
# `docker compose down && up`, image upgrades, and host reboots — not just
# in-place restarts. This is the "bake/persist /etc/hostid in a volume"
# option from the two choices considered; baking it into the image itself
# was rejected because every user's image would then share the identical
# hostid/iourc pair, defeating hostid-keyed licensing (the same reasoning
# firstboot-iourc.sh already documents for never baking iourc at build
# time — the same argument applies one layer down to hostid).
# ---------------------------------------------------------------------------
STATE_DIR="$IOLBOX_DIR/state"
install -d -m 0755 "$STATE_DIR"

if [ ! -f /etc/hostid ]; then
    if [ -f "$STATE_DIR/hostid" ]; then
        echo "entrypoint: restoring /etc/hostid from persisted state volume" >&2
        cp "$STATE_DIR/hostid" /etc/hostid
    else
        echo "entrypoint: generating new /etc/hostid (first run on this volume)" >&2
        # genhostid(3) writes a pseudo-random 32-bit value derived from
        # hostname+time+pid; `hostid` alone only READS, so seed via a small
        # trick: use Python-free POSIX approach — /dev/urandom straight
        # into the 4-byte host-order file (same format `hostid`/gethostid
        # read: 4 raw bytes, host byte order — matches supervisor's own
        # binary.LittleEndian.Uint32 fallback reader in main.go's hostID()).
        head -c 4 /dev/urandom > /etc/hostid
        cp /etc/hostid "$STATE_DIR/hostid"
    fi
else
    # Defensive: keep the volume's copy in sync if /etc/hostid was somehow
    # already present (e.g. a custom base image change later).
    cp /etc/hostid "$STATE_DIR/hostid" 2>/dev/null || true
fi

IOURC_FILE="$IOLBOX_DIR/iourc"
if [ -f "$STATE_DIR/iourc" ]; then
    cp "$STATE_DIR/iourc" "$IOURC_FILE"
elif [ -x "$SUPERVISOR" ]; then
    echo "entrypoint: generating iourc (first run on this volume)" >&2
    if "$SUPERVISOR" -gen-iourc > "$IOURC_FILE.tmp" 2>"$STATE_DIR/.iourc-gen.log"; then
        mv "$IOURC_FILE.tmp" "$IOURC_FILE"
        chmod 0644 "$IOURC_FILE"
        cp "$IOURC_FILE" "$STATE_DIR/iourc"
        echo "entrypoint: generated $IOURC_FILE" >&2
    else
        rm -f "$IOURC_FILE.tmp"
        echo "entrypoint: WARNING supervisor -gen-iourc failed, see $STATE_DIR/.iourc-gen.log" >&2
    fi
else
    echo "entrypoint: WARNING $SUPERVISOR not found or not executable; skipping iourc generation" >&2
fi

# ---------------------------------------------------------------------------
# Step 4: exec the supervisor (PID 1 replaces itself — signals pass
# straight through to `docker stop`). Flag set mirrors
# runtime/files/iolbox-supervisor.service's ExecStart= exactly: 0.0.0.0
# binds for ws/console/capture, because here — same as the appliance's
# reasoning — the isolation boundary is one layer out (the appliance's
# hypervisor NIC there, the container network namespace + published ports
# here), not the bind address itself.
# ---------------------------------------------------------------------------
echo "entrypoint: starting supervisor" >&2
exec "$SUPERVISOR" \
    -control-addr 127.0.0.1:4000 \
    -ws-addr 0.0.0.0:4001 \
    -console-bind 0.0.0.0 \
    -capture-bind 0.0.0.0 \
    -image-dir "$IOLBOX_DIR/images" \
    -run-dir "$IOLBOX_DIR/run" \
    -labs-dir "$IOLBOX_DIR/labs" \
    -iourc "$IOURC_FILE"
