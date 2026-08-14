#!/usr/bin/env bash
# 40-install-payload.sh — install the existing unmodified amd64 payload.
#
# In order, this step:
#   1. runs the preceding Rosetta canary and propagates its exit code;
#   2. extracts the staged native tarball into a disposable work directory;
#   3. runs ./install.sh --bind from the extracted payload directory; and
#   4. reports the payload and installed supervisor versions.
#
# Reinstalling the same tarball is intentional and safe: the work directory
# is disposable, while install.sh writes the existing /opt/iolbox binaries and
# service files without deleting images, labs, or /opt/iolbox/iourc. The
# installer's non-x86_64 uname warning is expected in this arm64 guest because
# Rosetta translates the amd64 payload; do not turn that warning into a block.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

usage() {
    cat <<EOF
Usage: $0

  Installs the payload named by IOLBOX_PAYLOAD_TARBALL using IOLBOX_BIND.
EOF
}

main() {
    local canary rc payload_name payload_version work_dir install_script install_dir supervisor_version

    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help) usage; return 0 ;;
            *) usage >&2; die "$IOLBOX_EXIT_USAGE" "unknown option: $1" ;;
        esac
    done

    [ "$(id -u)" -eq 0 ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "40-install-payload.sh must run as root"
    [ -n "${IOLBOX_PROVISION_DIR:-}" ] || die "$IOLBOX_EXIT_USAGE" \
        'IOLBOX_PROVISION_DIR is not set'
    [ -n "${IOLBOX_PAYLOAD_TARBALL:-}" ] || die "$IOLBOX_EXIT_USAGE" \
        'IOLBOX_PAYLOAD_TARBALL is not set'
    [ -n "${IOLBOX_BIND:-}" ] || die "$IOLBOX_EXIT_USAGE" \
        'IOLBOX_BIND is not set'
    case "$IOLBOX_BIND" in
        local|all) : ;;
        *) die "$IOLBOX_EXIT_USAGE" "IOLBOX_BIND must be local or all, got '$IOLBOX_BIND'" ;;
    esac
    [ -f "$IOLBOX_PROVISION_DIR/30-canary.sh" ] || die "$IOLBOX_EXIT_USAGE" \
        "canary script is missing: $IOLBOX_PROVISION_DIR/30-canary.sh"
    [ -f "$IOLBOX_PAYLOAD_TARBALL" ] || die "$IOLBOX_EXIT_USAGE" \
        "payload tarball is missing: $IOLBOX_PAYLOAD_TARBALL"

    # This exact invocation is the hard gate. Never install a payload into a
    # guest which cannot execute amd64, regardless of what install.sh reports.
    if "$IOLBOX_PROVISION_DIR/30-canary.sh"; then
        :
    else
        canary=$?
        log "30-canary.sh failed; refusing payload installation"
        exit "$canary"
    fi

    payload_name="$(basename "$IOLBOX_PAYLOAD_TARBALL")"
    payload_version="${payload_name#iolbox-server-}"
    payload_version="${payload_version%.tar.gz}"
    work_dir="$(mktemp -d /tmp/iolbox-payload.XXXXXX)" || die "$IOLBOX_EXIT_USAGE" \
        'could not create disposable payload work directory'
    cleanup() {
        rm -rf -- "$work_dir"
    }
    trap cleanup EXIT

    if tar -xzf "$IOLBOX_PAYLOAD_TARBALL" -C "$work_dir"; then
        :
    else
        rc=$?
        die "$rc" "payload extraction failed (exit $rc): tar -xzf $IOLBOX_PAYLOAD_TARBALL -C $work_dir"
    fi
    install_script="$(find "$work_dir" -type f -name install.sh -perm -u+x -print -quit 2>/dev/null || true)"
    [ -n "$install_script" ] || die "$IOLBOX_EXIT_USAGE" \
        "extracted payload does not contain an executable install.sh: $payload_name"
    install_dir="$(dirname "$install_script")"

    # install.sh deliberately warns that uname -m is aarch64 and continues;
    # that warning is expected under Rosetta and is not an install failure.
    if (cd "$install_dir" && ./install.sh --bind "$IOLBOX_BIND"); then
        :
    else
        rc=$?
        die "$rc" "payload installer failed (exit $rc): ./install.sh --bind $IOLBOX_BIND"
    fi

    if [ -x /opt/iolbox/supervisor ]; then
        if supervisor_version="$(/opt/iolbox/supervisor --version 2>&1)"; then
            supervisor_version="${supervisor_version%%$'\n'*}"
            log "installed version: payload=$payload_version supervisor=$supervisor_version"
        else
            warn "payload=$payload_version installed, but /opt/iolbox/supervisor --version could not be executed"
        fi
    else
        log "installed version: payload=$payload_version"
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
