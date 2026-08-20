#!/usr/bin/env bash
# 40-install-payload.sh — install the existing unmodified amd64 payload.
#
# In order, this step:
#   1. runs the preceding Rosetta canary and propagates its exit code;
#   2. installs the structural systemd ExecStartPre drop-in before invoking the
#      payload installer, so its first supervisor start is gated;
#   3. extracts the staged native tarball and runs ./install.sh --bind;
#   4. reloads and inspects the effective unit, requiring a gated active
#      supervisor; and
#   5. writes the guest structural-gate attestation only after all gates pass.
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

IOLBOX_CANARY_DROP_IN="${IOLBOX_CANARY_DROP_IN:-/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf}"
IOLBOX_STRUCTURAL_GATE_JSON="${IOLBOX_STRUCTURAL_GATE_JSON:-/var/lib/iolbox/macos-structural-gate.json}"

usage() {
    cat <<EOF
Usage: $0

  Installs the payload named by IOLBOX_PAYLOAD_TARBALL using IOLBOX_BIND.
EOF
}

stop_ungated_supervisor() {
    if systemctl is-active --quiet iolbox-supervisor.service 2>/dev/null; then
        systemctl stop iolbox-supervisor.service || die "$IOLBOX_EXIT_PREFLIGHT" \
            'could not stop an already-active supervisor before installing the structural gate'
    fi
}

install_canary_drop_in() {
    local drop_in_tmp

    chmod 0755 "$IOLBOX_PROVISION_DIR/30-canary.sh" || die "$IOLBOX_EXIT_PREFLIGHT" \
        "could not make the canary executable: $IOLBOX_PROVISION_DIR/30-canary.sh"
    install -d -m 0755 -- "$(dirname "$IOLBOX_CANARY_DROP_IN")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create the supervisor drop-in directory'
    drop_in_tmp="$(mktemp "${IOLBOX_CANARY_DROP_IN}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_PREFLIGHT" 'could not create the supervisor canary drop-in temporary file'
    printf '%s\n' \
        '[Service]' \
        '# Structural macOS/Lima gate: every boot, restart, crash-restart, and manual start runs the executable canary first.' \
        'Environment=IOLBOX_DISABLE_I386=1' \
        'ExecStartPre=/opt/iolbox-provision/30-canary.sh --quiet' > "$drop_in_tmp"
    chmod 0644 -- "$drop_in_tmp" || die "$IOLBOX_EXIT_PREFLIGHT" \
        'could not set supervisor canary drop-in permissions'
    if cmp -s -- "$IOLBOX_CANARY_DROP_IN" "$drop_in_tmp" 2>/dev/null; then
        rm -f -- "$drop_in_tmp"
    else
        mv -f -- "$drop_in_tmp" "$IOLBOX_CANARY_DROP_IN" || \
            die "$IOLBOX_EXIT_PREFLIGHT" 'could not install the supervisor canary drop-in'
    fi
    [ -f "$IOLBOX_CANARY_DROP_IN" ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "structural gate drop-in is missing after install: $IOLBOX_CANARY_DROP_IN"
}

reload_systemd() {
    systemctl daemon-reload || die "$IOLBOX_EXIT_PREFLIGHT" \
        'systemctl daemon-reload failed after installing the structural gate'
}

verify_gated_unit() {
    local unit_text show_text

    if ! unit_text="$(systemctl cat iolbox-supervisor.service 2>&1)"; then
        die "$IOLBOX_EXIT_VERIFY" \
            "systemctl cat iolbox-supervisor.service failed: $unit_text"
    fi
    case "$unit_text" in
        *'Environment=IOLBOX_DISABLE_I386=1'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl cat does not show the honest Apple Silicon i386 capability policy' ;;
    esac
    case "$unit_text" in
        *"ExecStartPre=/opt/iolbox-provision/30-canary.sh --quiet"*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl cat does not show the required macOS canary ExecStartPre' ;;
    esac
    case "$unit_text" in
        *'ExecStart=/opt/iolbox/supervisor'*|*'ExecStart=/mnt/lima-rosetta/rosetta'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl cat does not show an amd64 supervisor ExecStart after the canary gate' ;;
    esac

    # systemd orders every ExecStartPre before ExecStart regardless of the
    # backing-file concatenation order used by systemctl cat. Confirm the
    # effective properties as well as the human-auditable cat output.
    if ! show_text="$(systemctl show iolbox-supervisor.service -p Environment -p ExecStartPre -p ExecStart 2>&1)"; then
        die "$IOLBOX_EXIT_VERIFY" \
            "systemctl show of the gated supervisor failed: $show_text"
    fi
    case "$show_text" in
        *'Environment='*'IOLBOX_DISABLE_I386=1'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl show does not expose the honest i386 capability policy' ;;
    esac
    case "$show_text" in
        *'ExecStartPre='*'/opt/iolbox-provision/30-canary.sh --quiet'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl show does not expose the canary as an effective ExecStartPre' ;;
    esac
    case "$show_text" in
        *'ExecStart='*'/opt/iolbox/supervisor'*|*'ExecStart='*'/mnt/lima-rosetta/rosetta'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" \
            'systemctl show does not expose the amd64 supervisor ExecStart' ;;
    esac
}

host_macos_product() {
    local value="${IOLBOX_HOST_MACOS_PRODUCT:-}"

    if [ -n "$value" ]; then
        printf '%s\n' "$value"
    elif [[ "$IOLBOX_HOST_MACOS" == *' ('*')' ]]; then
        printf '%s\n' "${IOLBOX_HOST_MACOS%% (*}"
    else
        printf '%s\n' 'unknown'
    fi
}

host_macos_build() {
    local value="${IOLBOX_HOST_MACOS_BUILD:-}"

    if [ -n "$value" ]; then
        printf '%s\n' "$value"
    elif [[ "$IOLBOX_HOST_MACOS" == *' ('*')' ]]; then
        value="${IOLBOX_HOST_MACOS##* (}"
        printf '%s\n' "${value%)}"
    else
        printf '%s\n' 'unknown'
    fi
}

write_structural_attestation() {
    local product build kernel timestamp tmp json

    product="$(host_macos_product)"
    build="$(host_macos_build)"
    kernel="$(uname -r)"
    timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    [ "$product" != unknown ] && [ "$build" != unknown ] || die "$IOLBOX_EXIT_VERIFY" \
        'cannot attest the structural gate without the host macOS product and build'
    json="$(printf '{"schema":1,"profile":"%s","macos_product":"%s","macos_build":"%s","lima_version":"%s","drop_in":"%s","canary_verdict":"PASS","kernel":"%s","timestamp":"%s"}\n' \
        "$(iolbox_json_escape "$IOLBOX_PROFILE")" \
        "$(iolbox_json_escape "$product")" \
        "$(iolbox_json_escape "$build")" \
        "$(iolbox_json_escape "$IOLBOX_HOST_LIMA")" \
        "$(iolbox_json_escape "$IOLBOX_CANARY_DROP_IN")" \
        "$(iolbox_json_escape "$kernel")" \
        "$(iolbox_json_escape "$timestamp")")"
    install -d -m 0755 -- "$(dirname "$IOLBOX_STRUCTURAL_GATE_JSON")" || \
        die "$IOLBOX_EXIT_VERIFY" 'could not create structural-gate attestation directory'
    tmp="$(mktemp "${IOLBOX_STRUCTURAL_GATE_JSON}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_VERIFY" 'could not create structural-gate attestation temporary file'
    printf '%s' "$json" > "$tmp"
    chmod 0644 -- "$tmp" || die "$IOLBOX_EXIT_VERIFY" \
        'could not set structural-gate attestation permissions'
    mv -f -- "$tmp" "$IOLBOX_STRUCTURAL_GATE_JSON" || \
        die "$IOLBOX_EXIT_VERIFY" 'could not install structural-gate attestation'
}

main() {
    # NOTE: work_dir is deliberately NOT local. cleanup() is registered with
    # `trap cleanup EXIT`, and an EXIT trap runs after this function has already
    # returned — at which point a `local` would be out of scope and `set -u`
    # aborts with "work_dir: unbound variable". Observed on hardware (macOS
    # 26.6.1, Debian 13 guest) AFTER a fully successful install, and it would
    # equally have fired on the early canary-failure exit above, turning a clear
    # compatibility refusal into a confusing internal error.
    local canary rc payload_name payload_version install_script install_dir supervisor_version

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

    # A previously-running service may predate this drop-in. Stop it before
    # changing the unit so the next start is necessarily a gated start.
    stop_ungated_supervisor

    # This exact invocation is the hard gate. Never install a payload into a
    # guest which cannot execute amd64, regardless of what install.sh reports.
    if "$IOLBOX_PROVISION_DIR/30-canary.sh"; then
        :
    else
        canary=$?
        log "30-canary.sh failed; refusing payload installation"
        exit "$canary"
    fi

    # This must happen before payload install.sh's first systemctl start. The
    # installer overwrites the base unit and then daemon-reloads/starts it;
    # leaving the drop-in until afterwards would recreate the original race.
    install_canary_drop_in
    reload_systemd

    payload_name="$(basename "$IOLBOX_PAYLOAD_TARBALL")"
    payload_version="${payload_name#iolbox-server-}"
    payload_version="${payload_version%.tar.gz}"
    work_dir="$(mktemp -d /tmp/iolbox-payload.XXXXXX)" || die "$IOLBOX_EXIT_USAGE" \
        'could not create disposable payload work directory'
    cleanup() {
        # Guarded because the EXIT trap also fires on paths that never reached
        # the mktemp above (usage errors, a failed canary).
        [ -n "${work_dir:-}" ] && rm -rf -- "$work_dir"
        return 0
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

    # install.sh normally reloads systemd itself, but this explicit reload is
    # part of the structural proof and also covers a payload implementation
    # that changes the unit without reloading it.
    reload_systemd
    verify_gated_unit
    if ! systemctl is-active --quiet iolbox-supervisor.service; then
        die "$IOLBOX_EXIT_VERIFY" \
            'structural gate failed: iolbox-supervisor.service did not become active after the gated start'
    fi
    write_structural_attestation
    log "structural gate passed and attested at $IOLBOX_STRUCTURAL_GATE_JSON"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
