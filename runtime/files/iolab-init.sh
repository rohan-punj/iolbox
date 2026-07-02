#!/bin/sh
# /etc/init.d/iolab-init.sh — fallback autostart path for the (unlikely,
# but requested) non-systemd case: a stripped rootfs variant or a
# qemu-tcg/initrd boot that doesn't carry a full systemd. Debian bookworm's
# default init IS systemd, so on the artifacts this project actually ships
# (pack-wsl.sh, pack-vmware.sh) the systemd units in this same directory
# are what runs. This script exists so the rootfs still self-starts the
# supervisor if someone boots it under a minimal non-systemd init (e.g. a
# busybox-init initrd for the qemu-compat path in ../qemu-compat.md, or a
# hand-rolled `init=/opt/iolab/firstboot-iourc.sh`-style boot param).
#
# POSIX sh, LSB-ish init script. Installed to /etc/init.d/iolab and
# symlinked from the appropriate rcN.d/ if update-rc.d / sysv-rc is present;
# for a true init=-style single-script boot, the kernel cmdline can point
# straight at this file (see qemu-compat.md).
#
### BEGIN INIT INFO
# Provides:          iolab
# Required-Start:    $network $local_fs
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:
# Short-Description: iolab supervisor (non-systemd fallback)
### END INIT INFO

set -eu

IOLAB_DIR=/opt/iolab
SUPERVISOR="$IOLAB_DIR/supervisor"
PIDFILE=/var/run/iolab-supervisor.pid
LOGFILE=/var/log/iolab-supervisor.log

start() {
    # Same first-boot iourc generation the systemd path gets via
    # iolab-firstboot-iourc.service — must run before the supervisor so a
    # freshly-booted runtime has a license file ready before any node
    # start request can arrive from the GUI.
    "$IOLAB_DIR/firstboot-iourc.sh" || true

    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "iolab-supervisor already running (pid $(cat "$PIDFILE"))"
        return 0
    fi

    echo "starting iolab-supervisor"
    # start-stop-daemon isn't guaranteed present on a truly minimal image,
    # so background it directly and capture the pid ourselves. No restart
    # supervision loop here (systemd's Restart=always covers that case);
    # this fallback path favors "boots at all" over "self-heals forever".
    nohup "$SUPERVISOR" -control-addr 127.0.0.1:4000 -image-dir "$IOLAB_DIR/images" \
        >>"$LOGFILE" 2>&1 &
    echo $! > "$PIDFILE"
}

stop() {
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        rm -f "$PIDFILE"
    fi
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "iolab-supervisor running (pid $(cat "$PIDFILE"))"
    else
        echo "iolab-supervisor not running"
    fi
}

case "${1:-}" in
    start) start ;;
    stop) stop ;;
    restart) stop; start ;;
    status) status ;;
    *)
        echo "usage: $0 {start|stop|restart|status}" >&2
        exit 1
        ;;
esac
