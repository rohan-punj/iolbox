#!/bin/sh
# /opt/iolbox/prestart-clean.sh — ExecStartPre for iolbox-supervisor.service.
#
# Clears state a crashed or SIGKILLed previous supervisor run can leave
# behind, so a fresh start never fails on "File exists" / bound ports:
#
#  - stale iolnatN tap / iolmgmtN macvtap devices (extnet.Close never ran;
#    the supervisor also self-heals these with a retry — @3bf895f — but a
#    boot-time sweep keeps the first start deterministic). The loop starts
#    at 0 and covers more slots than any realistic lab uses.
#  - per-lab run dirs (sockets, NETMAPs, NVRAM scratch). Lab state the user
#    cares about lives in the lab document store (-labs-dir), not here.
#
# Process cleanup is deliberately NOT done here: the unit's
# KillMode=control-group already guarantees no IOL/VPCS children outlive
# the supervisor.
#
# POSIX sh; runs as root (the unit's User=root applies to ExecStartPre too).
set -u

for i in 0 1 2 3 4 5 6 7 8 9 10 11; do
    ip tuntap del dev "iolnat$i" mode tap 2>/dev/null || true
    ip link delete "iolmgmt$i" type macvtap 2>/dev/null || true
done

rm -rf /opt/iolbox/run/* 2>/dev/null || true

exit 0
