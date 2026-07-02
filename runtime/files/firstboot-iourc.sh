#!/bin/sh
# /opt/iolab/firstboot-iourc.sh
#
# Generates the IOL license file (iourc) from THIS runtime instance's own
# hostid, the first time the runtime boots. Never baked into the rootfs
# image at build time — a baked-in iourc would be identical across every
# user's VM/WSL instance and defeat the point of hostid-keyed licensing
# (see PLAN.md "IOL specifics baked in from day one" and the stock keygen
# facts in the pnetlab-deb-port-project memory topic).
#
# POSIX sh (not bash) on purpose: this also has to run from the non-systemd
# fallback init path (files/iolab-init.sh) on minimal images that lack
# systemd, so keep it portable — no bashisms.
set -eu

IOLAB_DIR="/opt/iolab"
IOURC_FILE="$IOLAB_DIR/iourc"
MARKER="$IOLAB_DIR/.iourc-generated"
SUPERVISOR="$IOLAB_DIR/supervisor"

# Idempotent: only generate once. Re-running this on every boot would be
# harmless in theory (same hostid -> same output) but we treat it as
# first-boot-only to keep the behavior obviously predictable and to avoid
# clobbering a file an advanced user may have hand-edited.
if [ -f "$MARKER" ]; then
    exit 0
fi

if [ ! -x "$SUPERVISOR" ]; then
    echo "firstboot-iourc: $SUPERVISOR not found or not executable; skipping iourc generation" >&2
    # Don't hard-fail boot over this — a missing supervisor binary is a
    # packaging bug that iolab-supervisor.service will also fail loudly on.
    exit 0
fi

# --- CROSS-TEAM ASSUMPTION -------------------------------------------------
# The supervisor binary is assumed to support a keygen-only invocation:
#
#     supervisor -gen-iourc > iourc
#
# i.e. given -gen-iourc, it reads the runtime's own hostid (whatever the
# stock IOL keygen algorithm needs), prints a complete, valid iourc file to
# stdout, and exits 0 WITHOUT starting the control-plane listener. This flag
# name/contract is coordinated in runtime/README.md — if the supervisor
# team lands a different flag or a different output convention (e.g. it
# writes the file itself instead of printing to stdout), update this line
# and the README paragraph together.
# ---------------------------------------------------------------------------
if "$SUPERVISOR" -gen-iourc > "$IOURC_FILE.tmp" 2>"$IOLAB_DIR/.iourc-gen.log"; then
    mv "$IOURC_FILE.tmp" "$IOURC_FILE"
    chmod 0644 "$IOURC_FILE"
    touch "$MARKER"
    echo "firstboot-iourc: generated $IOURC_FILE" >&2
else
    rm -f "$IOURC_FILE.tmp"
    echo "firstboot-iourc: supervisor -gen-iourc failed, see $IOLAB_DIR/.iourc-gen.log" >&2
    # Non-fatal: IOL nodes without a valid iourc will simply fail their own
    # license check when the supervisor spawns them, which surfaces to the
    # GUI as a node.state=crashed event with a readable reason — better than
    # blocking the whole runtime from booting.
    exit 0
fi
