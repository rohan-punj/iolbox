#!/usr/bin/env bash
# pack-native.sh — package iolbox as a standalone installer tarball for ANY
# systemd glibc Linux server (bare metal, cloud VM, on-prem hypervisor guest
# — not the WSL2/VMware appliance rootfs the other pack-* scripts build, and
# not a macOS Lima/VZ guest — the M7 native-arm64 profile uses this same
# packager with --arch arm64, then the macOS launcher's install.sh flow runs
# it inside the guest). See docs/providers.md's `remote` provider and
# runtime/files/native/README.txt for what ships inside.
#
# Unlike pack-wsl.sh/pack-vmware.sh, this does NOT consume the debootstrap
# rootfs from build-rootfs.sh — a native install runs on the OPERATOR's own
# distro, whatever it already has, so this script only needs the two
# binaries (supervisor, vpcs) plus the support files under
# runtime/files/{,native/}. No root required to run this script.
#
# Output: runtime/build/iolbox-server-<version>.tar.gz (amd64, historical
# name) or runtime/build/iolbox-server-<version>-linux-<arch>.tar.gz when
# --arch is passed explicitly.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${IOLBOX_BUILD_DIR:-$SCRIPT_DIR/build}"
FILES_DIR="$SCRIPT_DIR/files"
NATIVE_DIR="$FILES_DIR/native"

VERSION="dev"
TARGET_ARCH="amd64"
ARCH_EXPLICIT=0
VALIDATE_ONLY=0
# Same default search order build-rootfs.sh uses for the supervisor binary;
# see that script's SUPERVISOR_BIN comment (matches PLAN.md repo layout +
# .gitignore's /supervisor/bin/). Preferred over the rootfs copy when both
# exist, per this script's brief ("prefer the explicit --supervisor-bin
# flag like the other pack scripts" — --supervisor-bin always wins).
# These are recomputed after flag parsing once TARGET_ARCH is final (see
# below) so --arch arm64 picks up supervisor-linux-arm64 instead of the
# amd64 default.
SUPERVISOR_BIN_DEFAULT_1="$SCRIPT_DIR/../supervisor/bin/supervisor-linux-$TARGET_ARCH"
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
# quick IOL-only smoke build) and don't want the extra build time. Every pack
# and the secbench attack binaries are cross-compiled for TARGET_ARCH, same as
# supervisor/toollaunch — a native-arm64 package must not silently ship amd64
# pack binaries.
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
  --arch ARCH              Payload architecture: amd64 (default) or arm64.
                           No-argument amd64 keeps the historical package
                           name/layout; an explicit --arch (including
                           --arch amd64) suffixes the package name with
                           -linux-<arch> and writes manifest.env.
  --supervisor-bin PATH   Path to the built Go supervisor binary. Build it
                           with the repo's build-release.sh (the GUI bundle
                           must be embedded — a plain 'go build' ships a
                           placeholder GUI). Default: first of
                             $SUPERVISOR_BIN_DEFAULT_1
                             $SUPERVISOR_BIN_DEFAULT_2
                           that exists.
  --vpcs-bin PATH          Path to a prebuilt linux/\$TARGET_ARCH vpcs binary.
                           Default: $VPCS_BIN_DEFAULT (run ./fetch-vpcs.sh
                           first, or drop a binary there by hand)
  --toollaunch-bin PATH    Path to a prebuilt linux/\$TARGET_ARCH
                           iolbox-toollaunch binary. Default: cross-compiled
                           on the fly from $TOOLLAUNCH_SRC_DEFAULT (requires
                           'go' on PATH).
  --no-packs               Skip building/staging the learning-tool packs
                           (aaa/webserver/httpclient/syslog/netsvc/pc/secbench).
                           PC/VPCS nodes will not work without them.
  --build-dir DIR          Output root (default: $BUILD_DIR)
  --out DIR                Where to write the tarball (default: --build-dir)
  --validate-only          Validate supplied/resolved ELF architectures
                           (supervisor, vpcs, toollaunch), then exit 0
                           without staging or packing anything.
  -h, --help               This help

Output: <out>/iolbox-server-<version>[-linux-<arch>].tar.gz
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="$2"; shift 2 ;;
        --arch) TARGET_ARCH="$2"; ARCH_EXPLICIT=1; shift 2 ;;
        --arch=*) TARGET_ARCH="${1#*=}"; ARCH_EXPLICIT=1; shift ;;
        --supervisor-bin) SUPERVISOR_BIN="$2"; shift 2 ;;
        --vpcs-bin) VPCS_BIN="$2"; shift 2 ;;
        --toollaunch-bin) TOOLLAUNCH_BIN="$2"; shift 2 ;;
        --no-packs) SKIP_PACKS=1; shift ;;
        --build-dir) BUILD_DIR="$2"; OUT_DIR="$BUILD_DIR"; shift 2 ;;
        --out) OUT_DIR="$2"; shift 2 ;;
        --validate-only) VALIDATE_ONLY=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

case "$TARGET_ARCH" in
    amd64|arm64) ;;
    *) echo "pack-native: --arch must be amd64 or arm64, got '$TARGET_ARCH'" >&2; exit 1 ;;
esac

# Normalize to absolute paths now that --build-dir/--out are final. Several
# steps below (the tool-pack build loop in particular) build inside a `cd`'d
# subshell; a relative BUILD_DIR/OUT_DIR would resolve against that subshell's
# cwd instead of the caller's, silently writing packs to the wrong place and
# leaving PACK_STAGE never created for the `install` calls that follow.
mkdir -p "$BUILD_DIR" "$OUT_DIR"
BUILD_DIR="$(cd "$BUILD_DIR" && pwd)"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"

# Recompute architecture-sensitive defaults after all flags have been parsed
# and BUILD_DIR is absolute; this preserves the no-argument amd64 paths while
# making --arch arm64 select the matching supervisor payload.
SUPERVISOR_BIN_DEFAULT_1="$SCRIPT_DIR/../supervisor/bin/supervisor-linux-$TARGET_ARCH"
SUPERVISOR_BIN_DEFAULT_2="$BUILD_DIR/rootfs/opt/iolbox/supervisor"
VPCS_BIN_DEFAULT="$BUILD_DIR/vpcs/vpcs"

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
  bash build-release.sh    # -> supervisor/bin/supervisor-linux-$TARGET_ARCH
                            # (embeds the GUI bundle; plain 'go build' does not)

...or pass --supervisor-bin /path/to/binary.
EOF
    exit 1
fi

if [ -z "$VPCS_BIN" ]; then
    VPCS_BIN="$VPCS_BIN_DEFAULT"
fi

# A Windows/Git-Bash cross-build workspace may not preserve the POSIX execute
# bit on a transferred ELF. The archive staging below applies 0755, while
# require_elf_arch immediately after this check proves the input is an ELF of
# the requested architecture, so require a regular file here, not host-mode
# metadata that Windows cannot represent faithfully.
if [ ! -f "$VPCS_BIN" ]; then
    cat >&2 <<EOF
pack-native: vpcs binary not found or not a regular file at:
  $VPCS_BIN

Run ./fetch-vpcs.sh first (needs network access), or drop a prebuilt
linux/$TARGET_ARCH vpcs binary at that path by hand, or pass --vpcs-bin PATH.
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

Pass --toollaunch-bin PATH to use a prebuilt linux/$TARGET_ARCH binary instead.
EOF
        exit 1
    fi
    command -v go >/dev/null 2>&1 || {
        cat >&2 <<EOF
pack-native: 'go' is not on PATH, needed to build iolbox-toollaunch from
  $TOOLLAUNCH_SRC

Install Go, or pass --toollaunch-bin PATH to use a prebuilt linux/$TARGET_ARCH
binary instead.
EOF
        exit 1
    }
    TOOLLAUNCH_BIN="$BUILD_DIR/iolbox-toollaunch-$TARGET_ARCH"
    echo "== pack-native: building iolbox-toollaunch from $TOOLLAUNCH_SRC (linux/$TARGET_ARCH) =="
    ( cd "$TOOLLAUNCH_SRC" && GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 go build -trimpath -o "$TOOLLAUNCH_BIN" . )
fi

if [ ! -x "$TOOLLAUNCH_BIN" ] && [ ! -f "$TOOLLAUNCH_BIN" ]; then
    echo "pack-native: iolbox-toollaunch binary not found at: $TOOLLAUNCH_BIN" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Fail-closed ELF architecture validation. A native package must never be
# assembled with a binary that cannot execute on the requested TARGET_ARCH —
# this replaces the earlier amd64-only, warning-only 'file' probe with a hard
# gate that understands both amd64 and arm64 ELF headers, using 'file' when
# present and falling back to 'readelf -h'.
# ---------------------------------------------------------------------------
require_elf_arch() {
    local path="$1"
    local expected="$2"
    local label="$3"
    local description

    if command -v file >/dev/null 2>&1; then
        if ! description="$(file -b -- "$path" 2>&1)"; then
            echo "$label: file failed while inspecting $path: $description" >&2
            return 1
        fi
        case "$expected:$description" in
            amd64:*"ELF 64-bit"*"x86-64"*) ;;
            arm64:*"ELF 64-bit"*"ARM aarch64"*) ;;
            arm64:*"ELF 64-bit"*"AArch64"*) ;;
            *)
                echo "$label: $path is not a linux/$expected ELF: $description" >&2
                return 1
                ;;
        esac
        return 0
    fi

    if command -v readelf >/dev/null 2>&1; then
        if ! description="$(readelf -h "$path" 2>&1)"; then
            echo "$label: readelf failed while inspecting $path: $description" >&2
            return 1
        fi
        case "$expected:$description" in
            amd64:*"Class:"*"ELF64"*"Machine:"*"X86-64"*) ;;
            arm64:*"Class:"*"ELF64"*"Machine:"*"AArch64"*) ;;
            *)
                echo "$label: $path is not a linux/$expected ELF (readelf header did not match)" >&2
                return 1
                ;;
        esac
        return 0
    fi

    echo "$label: neither 'file' nor 'readelf' is available to verify $path is linux/$expected; refusing to assemble an unverified native package" >&2
    return 1
}

require_elf_arch "$SUPERVISOR_BIN" "$TARGET_ARCH" "pack-native"
require_elf_arch "$VPCS_BIN" "$TARGET_ARCH" "pack-native"
require_elf_arch "$TOOLLAUNCH_BIN" "$TARGET_ARCH" "pack-native"

if [ "$VALIDATE_ONLY" -eq 1 ]; then
    echo "pack-native: architecture validation passed for linux/$TARGET_ARCH"
    exit 0
fi

# ---------------------------------------------------------------------------
# Learning-tool packs (skippable with --no-packs)
# ---------------------------------------------------------------------------
PACK_STAGE="$BUILD_DIR/native-packs-$TARGET_ARCH"
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
        echo "== pack-native: building $pack pack GUI (linux/$TARGET_ARCH) =="
        ( cd "$pack_dir/gui" && GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/$pack/$pack-gui" . )
        install -m 0644 "$pack_dir/pack.json" "$PACK_STAGE/$pack/pack.json"
        chmod 0755 "$PACK_STAGE/$pack/$pack-gui"
    done

    echo "== pack-native: building secbench pack GUI (linux/$TARGET_ARCH) =="
    secbench_dir="$FILES_DIR/tools/packs/secbench"
    [ -f "$secbench_dir/pack.json" ] || { echo "pack-native: pack manifest missing: $secbench_dir/pack.json" >&2; exit 1; }
    install -d -m 0755 "$PACK_STAGE/secbench/bin"
    ( cd "$secbench_dir/gui" && GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/secbench/secbench-gui" . )
    install -m 0644 "$secbench_dir/pack.json" "$PACK_STAGE/secbench/pack.json"

    SECBENCH_ATTACKS_SRC="${SECBENCH_ATTACKS_SRC:-$SECBENCH_ATTACKS_SRC_DEFAULT}"
    if [ ! -d "$SECBENCH_ATTACKS_SRC" ]; then
        echo "pack-native: secbench attack binaries source not found: $SECBENCH_ATTACKS_SRC" >&2
        exit 1
    fi
    echo "== pack-native: building secbench attack binaries (linux/$TARGET_ARCH) =="
    ( cd "$SECBENCH_ATTACKS_SRC" && for d in cmd/*/; do
        key="$(basename "$d")"
        GOOS=linux GOARCH="$TARGET_ARCH" CGO_ENABLED=0 go build -trimpath -o "$PACK_STAGE/secbench/bin/$key" "./$d"
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
if [ "$ARCH_EXPLICIT" -eq 1 ]; then
    PKG_NAME="iolbox-server-$VERSION-linux-$TARGET_ARCH"
else
    # Preserve the historical no-argument amd64 archive/top-level name.
    PKG_NAME="iolbox-server-$VERSION"
fi
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
# Keep the historical README content, but make the selected architecture
# explicit and correct the architecture-specific prose for arm64. The
# no-argument amd64 form remains the historical package name and payload
# semantics; explicit architectures get distinct names and metadata.
{
    if [ "$TARGET_ARCH" = "arm64" ]; then
        printf 'Architecture: linux/arm64 (AArch64)\n\n'
        sed -e 's/linux\/amd64/linux\/arm64/g' -e 's/x86-64/AArch64/g' "$NATIVE_DIR/README.txt"
    else
        printf 'Architecture: linux/amd64 (x86-64)\n\n'
        cat "$NATIVE_DIR/README.txt"
    fi
} > "$STAGE_DIR/README.txt"
chmod 0644 "$STAGE_DIR/README.txt"

# manifest.env — install.sh's fail-closed arch check reads this when present
# (see runtime/files/native/install.sh). Written for every explicit --arch
# invocation (including --arch amd64) so a caller that asked for a specific
# architecture always gets a package that can prove what it contains; the
# bare no-argument historical form stays manifest-free for byte-for-byte
# compatibility with pre-M7 amd64 packages.
if [ "$ARCH_EXPLICIT" -eq 1 ]; then
    {
        printf 'version=%s\n' "$VERSION"
        printf 'os=linux\n'
        printf 'arch=%s\n' "$TARGET_ARCH"
        printf 'supervisor_sha256=%s\n' "$(sha256sum "$SUPERVISOR_BIN" | awk '{print $1}')"
        printf 'vpcs_sha256=%s\n' "$(sha256sum "$VPCS_BIN" | awk '{print $1}')"
    } > "$STAGE_DIR/manifest.env"
    chmod 0644 "$STAGE_DIR/manifest.env"
fi

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
