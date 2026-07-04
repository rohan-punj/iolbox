#!/bin/sh
# /opt/iolbox/prestart-clean.sh — ExecStartPre for iolbox-supervisor.service.
#
# Clears state a crashed or SIGKILLed (or hard-swapped) previous supervisor run
# can leave behind, so a fresh start never fails on "File exists" / bound ports /
# "iouyap ... netio: address in use". Everything here is best-effort + idempotent.
#
#  - stray IOL / VPCS / iouyap / tcpdump processes. KillMode=control-group
#    SHOULD reap the supervisor's children, but an IOL that escapes the cgroup
#    (observed after an unclean restart) otherwise keeps netio sockets + fabric
#    taps alive and wedges the next lab.start — so sweep them here too.
#  - every stale iolbox network device: fabric bridges (iolbr<linkid>),
#    per-interface taps (iol<inst>_<flat>), vpcs taps (iolvpc<id>), and extnet
#    devices (iolnat<n> / iolmgmt<n>). `ip link delete` handles all link kinds.
#  - stale netio sockets IOL/iouyap bind under /tmp/netio* (the fabric shim's
#    per-instance unix sockets) — NOT under the run dir, so swept separately.
#  - per-lab run dirs (sockets, NETMAPs, NVRAM scratch). Durable lab state the
#    user cares about lives in the lab document store (-labs-dir), not here.
#
# POSIX sh; runs as root (the unit's User=root applies to ExecStartPre too).
# Needs procps (pkill), iproute2 (ip), coreutils — all in the runtime rootfs.
set -u

# Stray processes from a previous unclean run.
pkill -9 -f '/opt/iolbox/images/' 2>/dev/null || true   # IOL image binaries
pkill -9 -x tcpdump 2>/dev/null || true                  # bridge-capture tees
pkill -9 -x vpcs 2>/dev/null || true
pkill -9 -x iouyap 2>/dev/null || true

# Every stale iolbox network device (bridges, fabric taps, vpcs taps, extnet).
# Nothing else on this single-purpose appliance is named "iol*".
for l in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | awk -F'@' '{print $1}'); do
    case "$l" in
        iol*) ip link delete "$l" 2>/dev/null || true ;;
    esac
done

# Stale fabric netio sockets + per-lab run scratch.
rm -rf /tmp/netio* 2>/dev/null || true
rm -rf /opt/iolbox/run/* 2>/dev/null || true

exit 0
