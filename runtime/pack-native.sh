#!/usr/bin/env bash
# pack-native.sh — package iolbox as a standalone installer tarball for ANY
# systemd x86-64 glibc Linux server (bare metal, cloud VM, on-prem
# hypervisor guest — not the WSL2/VMware appliance rootfs the other pack-*
# scripts build). See docs/providers.md's `remote` provider and
# runtime/files/native/README.txt for what ships inside.
#
# Unlike pack-wsl.sh/pack-vmware.sh, this does NOT consume the debootstrap
# rootfs from build-rootfs.sh — a native install runs on the OPERATOR's own
# distro, whatever it already has, so this script only needs the two
# binaries (supervisor, vpcs) plus the support files under
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
# Source, not a prebuilt binary: unlike vpcs (fetched) and supervisor (built by
# build-release.sh), iolbox-toollaunch is trivial to cross-compile here with
# CGO disabled, matching how runtime/build-rootfs.sh builds it (see that
# script's `go build -trimpath` invocation for TOOLLAUNCH_BIN).
TOOLLAUNCH_SRC_DEFAULT="$SCRIPT_DIR/../tools/iolbox-toollaunch"
TOOLLAUNCH_SRC=""
TOOLLAUNCH_BIN=""
# Learning-tool packs (see docs/ for the P2/P3 plans). Each simple pack is a
# single static Go binary that is both the pack's AF_UNIX GUI and its
# lab-facing service; "pc" is the built-in netprobe pack PC/VPCS nodes require
# (see supervisor/internal/server/toolpacks.go). secbench additionally ships a
# directory of standalone attack binaries under tools/secbench-attacks-go.
# Skippable with --no-packs for callers that only need supervisor/vpcs (e.g. a
# quick IOL-only smoke build) and don't want the extra build time.
SIMPLE_PACKS="aaa webserver httpclient syslog netsvc pc"
SECBENCH_ATTACKS_SRC_DEFAULT="$SCRIPT_DIR/../tools/secbench-attacks-go"
SKIP_PACKS=0
OUT_DIR="$BUILD_DIR"

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
  --toollaunch-bin PATH    Path to a prebuilt linux/amd64 iolbox-toollaunch
                           binary. Default: cross-compiled on the fly from
                           $TOOLLAUNCH_SRC_DEFAULT (requires 'go' on PATH).
  --no-packs               Skip building/staging the learning-tool packs
                           (aaa/webserver/httpclient/syslog/netsvc/pc/secbench).
                           PC/VPCS nodes will not work without them.
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
        --toollaunch-bin) TOOLLAUNCH_BIN="$2"; shift 2 ;;
        --no-packs) SKIP_PACKS=1; shift ;;
        --build-dir) BUILD_DIR="$2"; OUT_DIR="$BUILD_DIR"; shift 2 ;;
        --out) OUT_DIR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

# Normalize to absolute paths now that --build-dir/--out are final. Several
# steps below (the tool-pack build loop in particular) build inside a `cd`'d
# subshell; a relative BUILD_DIR/OUT_DIR would resolve against that subshell's
# cwd instead of the caller's, silently writing packs to the wrong place and
# leaving PACK_STAGE never created for the `install` calls that follow.
mkdir -p "$BUILD_DIR" "$OUT_DIR"
BUILD_DIR="$(cd "$BUILD_DIR" && pwd)"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"

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

if [ -z "$TOOLLAUNCH_BIN" ]; then
    if [ -z "$TOOLLAUNCH_SRC" ]; then
        TOOLLAUNCH_SRC="$TOOLLAUNCH_SRC_DEFAULT"
    fi
    if [ ! -d "$TOOLLAUNCH_SRC" ]; then
        cat >&2 <<EOF
pack-native: iolbox-toollaunch source not found at:
  $TOOLLAUNCH_SRC

Pass --toollaunch-bin PATH to use a prebuilt linux/amd64 binary instead.
EOF
        exit 1
    fi
    command -v go >/dev/null 2>&1 || {
        cat >&2 <<EOF
pack-native: 'go' is not on PATH, needed to build iolbox-toollaunch from
  $TOOLLAUNCH_SRC

Install Go, or pass --toollaunch-bin PATH to use a prebuilt linux/amd64
binary instead.
EOF
        exit 1
    }
    TOOLLAUNCH_BIN="$BUILD_DIR/iolbox-toollaunch"
    echo "== pack-native: building iolbox-toollaunch from $TOOLLAUNCH_SRC =="
    ( cd "$TOOLLAUNCH_SRC" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$TOOLLAUNCH_BIN" . )
fi

if [ ! -x "$TOOLLAUNCH_BIN" ]; then
    echo "pack-native: iolbox-toollaunch binary not found or not executable at: $TOOLLAUNCH_BIN" >&2
    exit 1
fi

# Sanity: both binaries should be linux/amd64 ELF, since they're going onto
# an x86-64 Linux server (same check fetch-vpcs.sh already does for vpcs at
# build time; repeated here since --supervisor-bin/--vpcs-bin let a caller
# point at an arbitrary file that may not have gone through that check).
if command -v file >/dev/null 2>&1; then
    for b in "$SUPERVISOR_BIN" "$VPCS_BIN" "$TOOLLAUNCH_BIN"; do
        FILE_OUT="$(file -b "$b" 2>/dev/null || echo unknown)"
        case "$FILE_OUT" in
            *"ELF 64-bit"*"x86-64"*) : ;;
            *) echo "pack-native: WARNING - $b does not look like linux/amd64 ELF64: $FILE_OUT" >&2 ;;
        esac
    done
fi

# ---------------------------------------------------------------------------
# Learning-tool packs (skippable with --no-packs)
# ---------------------------------------------------------------------------
PACK_STAGE="$BUILD_DIR/native-packs"
if [ "$SKIP_PACKS" -eq 1 ]; then
    echo "== pack-native: --no-packs given, skipping tool packs (PC/VPCS nodes will not work) =="
else
    command -v go >/dev/null 2>&1 || {
        echo "pack-native: 'go' is not on PATH, needed to build the tool packs. Pass --no-packs to skip them (PC/VPCS nodes will not work), or install Go." >&2
        exit 1
    }
    rm -rf "$PACK_STAGE"
    for pack in $SIMPLE_PACKS; do
        pack_dir="$FILES_DIR/tools/packs/$pack"
        [ -f "$pack_dir/pack.json" ] || { echo "pack-native: pack manifest missing: $pack_dir/pack.json" >&2; exit 1; }
        echo "== pack-native: building $pack pack GUI (linux/amd64) =="
        ( cd "$pack_dir/gui" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/$pack/$pack-gui" . )
        install -m 0644 "$pack_dir/pack.json" "$PACK_STAGE/$pack/pack.json"
        chmod 0755 "$PACK_STAGE/$pack/$pack-gui"
    done

    echo "== pack-native: building secbench pack GUI (linux/amd64) =="
    secbench_dir="$FILES_DIR/tools/packs/secbench"
    [ -f "$secbench_dir/pack.json" ] || { echo "pack-native: pack manifest missing: $secbench_dir/pack.json" >&2; exit 1; }
    install -d -m 0755 "$PACK_STAGE/secbench/bin"
    ( cd "$secbench_dir/gui" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/secbench/secbench-gui" . )
    install -m 0644 "$secbench_dir/pack.json" "$PACK_STAGE/secbench/pack.json"

    SECBENCH_ATTACKS_SRC="${SECBENCH_ATTACKS_SRC:-$SECBENCH_ATTACKS_SRC_DEFAULT}"
    if [ ! -d "$SECBENCH_ATTACKS_SRC" ]; then
        echo "pack-native: secbench attack binaries source not found: $SECBENCH_ATTACKS_SRC" >&2
        exit 1
    fi
    echo "== pack-native: building secbench attack binaries (linux/amd64) =="
    ( cd "$SECBENCH_ATTACKS_SRC" && for d in cmd/*/; do
        key="$(basename "$d")"
        GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/secbench/bin/$key" "./$d"
    done )
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
    ; do
    if [ ! -f "$f" ]; then
        echo "pack-native: required source file missing: $f" >&2
        exit 1
    fi
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

# Binaries — 0755 (a 0644 supervisor binary fails fork/exec; see
# runtime/build-rootfs.sh's install invocation and install.sh's comment).
install -m 0755 "$SUPERVISOR_BIN" "$STAGE_DIR/bin/supervisor"
install -m 0755 "$VPCS_BIN" "$STAGE_DIR/bin/vpcs"
install -m 0755 "$TOOLLAUNCH_BIN" "$STAGE_DIR/bin/iolbox-toollaunch"

# Learning-tool packs, staged verbatim under tools/packs/<id>/ so install.sh
# can copy the whole tree into $PREFIX/tools/packs without per-pack knowledge.
if [ "$SKIP_PACKS" -ne 1 ]; then
    install -d -m 0755 "$STAGE_DIR/tools"
    cp -R "$PACK_STAGE" "$STAGE_DIR/tools/packs"
fi

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
