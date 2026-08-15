#!/usr/bin/env bash
# pack-native.sh — package iolbox as a standalone installer tarball for ANY
# systemd x86-64 glibc Linux server (bare metal, cloud VM, on-prem
# hypervisor guest — not the WSL2/VMware appliance rootfs the other pack-*
# scripts build). See docs/providers.md's `remote` provider and
# runtime/files/native/README.txt for what ships inside.
#
# Unlike pack-wsl.sh/pack-vmware.sh, this does NOT consume the debootstrap
# rootfs from build-rootfs.sh — a native install runs on the OPERATOR's own
# distro, whatever it already has, so this script only needs the supervisor
# and vpcs binaries, the tool-pack GUIs (built here, the same way
# build-rootfs.sh builds them — see that script's Stage 1), the
# iolbox-toollaunch helper, plus the support files under
# runtime/files/{,native/}. No root required to run this script.
#
# Output: runtime/build/iolbox-server-<version>.tar.gz
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLBOX_BUILD_DIR:-$SCRIPT_DIR/build}"
FILES_DIR="$SCRIPT_DIR/files"
NATIVE_DIR="$FILES_DIR/native"

VERSION="dev"
# Same default search order build-rootfs.sh uses for the supervisor binary;
# see that script's SUPERVISOR_BIN comment (matches PLAN.md repo layout +
# .gitignore's /supervisor/bin/). Preferred over the rootfs copy when both
# exist, per this script's brief ("prefer the explicit --supervisor-bin
# flag like the other pack scripts" — --supervisor-bin always wins).
SUPERVISOR_BIN_DEFAULT_1="$SCRIPT_DIR/../supervisor/bin/supervisor-linux-amd64"
SUPERVISOR_BIN_DEFAULT_2="$BUILD_DIR/rootfs/opt/iolbox/supervisor"
SUPERVISOR_BIN=""
VPCS_BIN_DEFAULT="$BUILD_DIR/vpcs/vpcs"     # fetch-vpcs.sh's output path
VPCS_BIN=""
OUT_DIR="$BUILD_DIR"

# Tool-pack GUIs + iolbox-toollaunch: these are what let kind=="pc" (the
# built-in netprobe virtual PC) and kind=="tool" nodes boot at all — see
# supervisor/internal/server/toolpacks.go and internal/tool/launch.go
# (launchNativePath is hardcoded to /opt/iolbox/iolbox-toollaunch). They
# are plain stdlib-only Go binaries with no debootstrap/root dependency,
# so this script builds them itself rather than requiring build-rootfs.sh
# to have run first.
TOOLLAUNCH_SOURCE="$SCRIPT_DIR/../tools/iolbox-toollaunch"
TOOLLAUNCH_BIN="$BUILD_DIR/iolbox-toollaunch"
SECBENCH_GUI_BIN="$BUILD_DIR/secbench-gui"
SECBENCH_ATTACKS_SRC="$SCRIPT_DIR/../tools/secbench-attacks-go"
SECBENCH_BIN_STAGE="$BUILD_DIR/secbench-bin"
# P3 network-tool packs, same set build-rootfs.sh builds/installs. "pc" is
# the netprobe virtual PC (kind=="pc" in the lab schema); the others are
# kind=="tool" learning-tool nodes.
TOOL_PACKS="aaa webserver httpclient syslog netsvc pc"

usage() {
    cat <<EOF
Usage: $0 [options]

  --version X.Y.Z         Version string embedded in the output filename and
                           the top-level directory inside the tarball
                           (default: $VERSION)
  --supervisor-bin PATH   Path to the built Go supervisor binary. Build it
                           with the repo's build-release.sh (the GUI bundle
                           must be embedded — a plain 'go build' ships a
                           placeholder GUI). Default: first of
                             $SUPERVISOR_BIN_DEFAULT_1
                             $SUPERVISOR_BIN_DEFAULT_2
                           that exists.
  --vpcs-bin PATH          Path to a prebuilt linux/amd64 vpcs binary.
                           Default: $VPCS_BIN_DEFAULT (run ./fetch-vpcs.sh
                           first, or drop a binary there by hand)
  --build-dir DIR          Output root (default: $BUILD_DIR)
  --out DIR                Where to write the tarball (default: --build-dir)
  -h, --help               This help

Output: <out>/iolbox-server-<version>.tar.gz
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --supervisor-bin) SUPERVISOR_BIN="$2"; shift 2 ;;
        --vpcs-bin) VPCS_BIN="$2"; shift 2 ;;
        --build-dir) BUILD_DIR="$2"; OUT_DIR="$BUILD_DIR"; shift 2 ;;
        --out) OUT_DIR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

# ---------------------------------------------------------------------------
# Resolve binary sources
# ---------------------------------------------------------------------------
if [ -z "$SUPERVISOR_BIN" ]; then
    if [ -f "$SUPERVISOR_BIN_DEFAULT_1" ]; then
        SUPERVISOR_BIN="$SUPERVISOR_BIN_DEFAULT_1"
    elif [ -f "$SUPERVISOR_BIN_DEFAULT_2" ]; then
        SUPERVISOR_BIN="$SUPERVISOR_BIN_DEFAULT_2"
    fi
fi

if [ -z "$SUPERVISOR_BIN" ] || [ ! -f "$SUPERVISOR_BIN" ]; then
    cat >&2 <<EOF
pack-native: supervisor binary not found. Checked:
  $SUPERVISOR_BIN_DEFAULT_1
  $SUPERVISOR_BIN_DEFAULT_2

Build it first (from the repo root):
  bash build-release.sh    # -> supervisor/bin/supervisor-linux-amd64
                            # (embeds the GUI bundle; plain 'go build' does not)

...or pass --supervisor-bin /path/to/binary.
EOF
    exit 1
fi

if [ -z "$VPCS_BIN" ]; then
    VPCS_BIN="$VPCS_BIN_DEFAULT"
fi

if [ ! -x "$VPCS_BIN" ]; then
    cat >&2 <<EOF
pack-native: vpcs binary not found or not executable at:
  $VPCS_BIN

Run ./fetch-vpcs.sh first (needs network access), or drop a prebuilt
linux/amd64 vpcs binary at that path by hand, or pass --vpcs-bin PATH.
EOF
    exit 1
fi

# Sanity: both binaries should be linux/amd64 ELF, since they're going onto
# an x86-64 Linux server (same check fetch-vpcs.sh already does for vpcs at
# build time; repeated here since --supervisor-bin/--vpcs-bin let a caller
# point at an arbitrary file that may not have gone through that check).
if command -v file >/dev/null 2>&1; then
    for b in "$SUPERVISOR_BIN" "$VPCS_BIN"; do
        FILE_OUT="$(file -b "$b" 2>/dev/null || echo unknown)"
        case "$FILE_OUT" in
            *"ELF 64-bit"*"x86-64"*) : ;;
            *) echo "pack-native: WARNING - $b does not look like linux/amd64 ELF64: $FILE_OUT" >&2 ;;
        esac
    done
fi

for f in \
    "$FILES_DIR/iolbox-supervisor.service" \
    "$FILES_DIR/iolbox-firstboot-iourc.service" \
    "$FILES_DIR/firstboot-iourc.sh" \
    "$FILES_DIR/prestart-clean.sh" \
    "$FILES_DIR/99-iolbox.conf" \
    "$NATIVE_DIR/install.sh" \
    "$NATIVE_DIR/uninstall.sh" \
    "$NATIVE_DIR/README.txt" \
    "$NATIVE_DIR/10-bind.conf.tmpl" \
    "$NATIVE_DIR/iolbox-bind.env.local" \
    "$NATIVE_DIR/iolbox-bind.env.all" \
    "$FILES_DIR/tools/packs/secbench/pack.json" \
    ; do
    if [ ! -f "$f" ]; then
        echo "pack-native: required source file missing: $f" >&2
        exit 1
    fi
done
for pack in $TOOL_PACKS; do
    if [ ! -f "$FILES_DIR/tools/packs/$pack/pack.json" ]; then
        echo "pack-native: required source file missing: $FILES_DIR/tools/packs/$pack/pack.json" >&2
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# Build the tool-pack GUIs + iolbox-toollaunch helper
# ---------------------------------------------------------------------------
# Same build as build-rootfs.sh's Stage 1 (see that script for the fuller
# go vet/go test gate used in the release pipeline); kept lighter here since
# this script is also meant to be runnable standalone by an operator who
# just wants a native install.
echo "== pack-native: building tool launch helper (linux/amd64) =="
(
    cd "$TOOLLAUNCH_SOURCE"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$TOOLLAUNCH_BIN" .
)

echo "== pack-native: building secbench pack GUI (linux/amd64) =="
(
    cd "$FILES_DIR/tools/packs/secbench/gui"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$SECBENCH_GUI_BIN" .
)

echo "== pack-native: building secbench attack binaries (linux/amd64) =="
rm -rf "$SECBENCH_BIN_STAGE"; mkdir -p "$SECBENCH_BIN_STAGE"
(
    cd "$SECBENCH_ATTACKS_SRC"
    for d in cmd/*/; do
        key="$(basename "$d")"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
            -o "$SECBENCH_BIN_STAGE/$key" "./$d"
    done
)

for pack in $TOOL_PACKS; do
    echo "== pack-native: building $pack pack GUI (linux/amd64) =="
    (
        cd "$FILES_DIR/tools/packs/$pack/gui"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
            -o "$BUILD_DIR/$pack-gui" .
    )
done

# ---------------------------------------------------------------------------
# Assemble the staging tree
# ---------------------------------------------------------------------------
PKG_NAME="iolbox-server-$VERSION"
STAGE_ROOT="$BUILD_DIR/native-stage"
STAGE_DIR="$STAGE_ROOT/$PKG_NAME"

echo "== pack-native: staging $STAGE_DIR =="
rm -rf "$STAGE_DIR"
install -d -m 0755 "$STAGE_DIR/bin"
install -d -m 0755 "$STAGE_DIR/opt-iolbox"
install -d -m 0755 "$STAGE_DIR/systemd"
install -d -m 0755 "$STAGE_DIR/etc"
install -d -m 0755 "$STAGE_DIR/tools/packs"

# Binaries — 0755 (a 0644 supervisor binary fails fork/exec; see
# runtime/build-rootfs.sh's install invocation and install.sh's comment).
install -m 0755 "$SUPERVISOR_BIN" "$STAGE_DIR/bin/supervisor"
install -m 0755 "$VPCS_BIN" "$STAGE_DIR/bin/vpcs"
install -m 0755 "$TOOLLAUNCH_BIN" "$STAGE_DIR/bin/iolbox-toollaunch"

# Tool packs: without these, every kind=="pc" (netprobe) and kind=="tool"
# node has no GUI/service binary to launch and fails to boot, even though
# the supervisor itself starts fine (toolpacksLoad only logs a warning when
# the packs dir is empty/missing — see internal/server/toolpacks.go).
install -d -m 0755 "$STAGE_DIR/tools/packs/secbench"
install -d -m 0755 "$STAGE_DIR/tools/packs/secbench/bin"
install -m 0644 "$FILES_DIR/tools/packs/secbench/pack.json" \
    "$STAGE_DIR/tools/packs/secbench/pack.json"
install -m 0755 "$SECBENCH_GUI_BIN" \
    "$STAGE_DIR/tools/packs/secbench/secbench-gui"
for bin in "$SECBENCH_BIN_STAGE"/*; do
    install -m 0755 "$bin" \
        "$STAGE_DIR/tools/packs/secbench/bin/$(basename "$bin")"
done
for pack in $TOOL_PACKS; do
    install -d -m 0755 "$STAGE_DIR/tools/packs/$pack"
    install -m 0644 "$FILES_DIR/tools/packs/$pack/pack.json" \
        "$STAGE_DIR/tools/packs/$pack/pack.json"
    install -m 0755 "$BUILD_DIR/$pack-gui" \
        "$STAGE_DIR/tools/packs/$pack/$pack-gui"
done

# Reused verbatim from runtime/files/ — same scripts the WSL/VMware rootfs
# ships, no VMware-specific path assumptions in either (both are plain
# POSIX sh operating only under /opt/iolbox, which install.sh also uses as
# its default --prefix).
install -m 0755 "$FILES_DIR/firstboot-iourc.sh" "$STAGE_DIR/opt-iolbox/firstboot-iourc.sh"
install -m 0755 "$FILES_DIR/prestart-clean.sh" "$STAGE_DIR/opt-iolbox/prestart-clean.sh"

# Stock systemd units, byte-identical to the WSL/VMware copies — the bind
# addresses are overridden by the 10-bind.conf drop-in, not by editing the
# unit itself, so this one flag set stays audited-once across every
# packaging target (see runtime/README.md).
install -m 0644 "$FILES_DIR/iolbox-supervisor.service" "$STAGE_DIR/systemd/iolbox-supervisor.service"
install -m 0644 "$FILES_DIR/iolbox-firstboot-iourc.service" "$STAGE_DIR/systemd/iolbox-firstboot-iourc.service"
install -m 0644 "$NATIVE_DIR/10-bind.conf.tmpl" "$STAGE_DIR/systemd/10-bind.conf"
install -m 0644 "$NATIVE_DIR/iolbox-bind.env.local" "$STAGE_DIR/systemd/bind.env.local"
install -m 0644 "$NATIVE_DIR/iolbox-bind.env.all" "$STAGE_DIR/systemd/bind.env.all"

install -m 0644 "$FILES_DIR/99-iolbox.conf" "$STAGE_DIR/etc/99-iolbox.conf"

install -m 0755 "$NATIVE_DIR/install.sh" "$STAGE_DIR/install.sh"
install -m 0755 "$NATIVE_DIR/uninstall.sh" "$STAGE_DIR/uninstall.sh"
install -m 0644 "$NATIVE_DIR/README.txt" "$STAGE_DIR/README.txt"

# ---------------------------------------------------------------------------
# Tar it up
# ---------------------------------------------------------------------------
install -d -m 0755 "$OUT_DIR"
OUT_TAR="$OUT_DIR/$PKG_NAME.tar.gz"

echo "== pack-native: creating $OUT_TAR =="
tar \
    --create --gzip \
    --file "$OUT_TAR" \
    --directory "$STAGE_ROOT" \
    --numeric-owner \
    --owner=0 --group=0 \
    --sort=name \
    "$PKG_NAME"

rm -rf "$STAGE_ROOT"

echo "== pack-native: done =="
ls -lh "$OUT_TAR"
cat <<EOF

Install on the target server:
  tar xzf $(basename "$OUT_TAR")
  cd $PKG_NAME
  sudo ./install.sh              # or: sudo ./install.sh --bind all

See runtime/files/native/README.txt (also shipped inside the tarball) for
the full flag/security rundown.
EOF
