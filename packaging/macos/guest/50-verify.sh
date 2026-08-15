#!/usr/bin/env bash
# 50-verify.sh — prove that the translated guest is ready and persistent.
#
# In order, this step:
#   1. polls systemd and ss until the supervisor is active and the GUI socket
#      is genuinely bound;
#   2. polls GET / until its HTTP status is below 500;
#   3. asserts the loader canary, image registry, licence, and supervisor;
#   4. prints a compact fact block; and
#   5. optionally compares restart facts with /var/lib/iolbox/macos-verify.json.
#
# A systemd active state alone is insufficient because it can remain active
# during a restart loop. The port and real HTTP request are separate gates.
# There is intentionally no /api/health endpoint: the GUI root is the
# readiness contract. --persistence records restart invariants and fails only
# on a regression (kernel/host identity loss, licence loss, or cache shrink).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$SCRIPT_DIR/lib.sh"

IOLBOX_VERIFY_TIMEOUT_SECONDS="${IOLBOX_VERIFY_TIMEOUT_SECONDS:-30}"
IOLBOX_VERIFY_JSON="${IOLBOX_VERIFY_JSON:-/var/lib/iolbox/macos-verify.json}"
IOLBOX_IMAGE_CACHE_FILE="${IOLBOX_IMAGE_CACHE_FILE:-/opt/iolbox/images/.image-cache.json}"
IOLBOX_ROSETTA_BINFMT_FILE="${IOLBOX_ROSETTA_BINFMT_FILE:-/proc/sys/fs/binfmt_misc/rosetta}"
IOLBOX_STRUCTURAL_GATE_JSON="${IOLBOX_STRUCTURAL_GATE_JSON:-/var/lib/iolbox/macos-structural-gate.json}"

usage() {
    cat <<EOF
Usage: $0 [--persistence]

  --persistence  compare restart invariants with
                  $IOLBOX_VERIFY_JSON and update the record on success.
  -h, --help      show this help.
EOF
}

port_is_bound() {
    ss -ltn 2>/dev/null | awk -v suffix=":$IOLBOX_GUI_PORT" '
        NR > 1 && length($4) >= length(suffix) && substr($4, length($4) - length(suffix) + 1) == suffix { found = 1 }
        END { exit(found ? 0 : 1) }
    '
}

wait_for_service_and_port() {
    local deadline=$((SECONDS + IOLBOX_VERIFY_TIMEOUT_SECONDS))

    while [ "$SECONDS" -lt "$deadline" ]; do
        supervisor_status="$(systemctl is-active iolbox-supervisor.service 2>/dev/null || true)"
        gui_bound=0
        if port_is_bound; then
            gui_bound=1
        fi
        if [ "$supervisor_status" = active ] && [ "$gui_bound" -eq 1 ]; then
            return 0
        fi
        sleep 1
    done
    return 1
}

wait_for_http_ready() {
    local deadline=$((SECONDS + IOLBOX_VERIFY_TIMEOUT_SECONDS))

    http_status=000
    while [ "$SECONDS" -lt "$deadline" ]; do
        http_status="$(curl --silent --show-error --output /dev/null \
            --write-out '%{http_code}' --connect-timeout 1 --max-time 3 \
            "http://127.0.0.1:$IOLBOX_GUI_PORT/" 2>/dev/null || true)"
        if [[ "$http_status" =~ ^[1-4][0-9][0-9]$ ]]; then
            return 0
        fi
        sleep 1
    done
    return 1
}

verify_structural_attestation() {
    local gate

    if [ ! -f "$IOLBOX_STRUCTURAL_GATE_JSON" ]; then
        die "$IOLBOX_EXIT_VERIFY" "structural gate assertion: missing $IOLBOX_STRUCTURAL_GATE_JSON"
    fi
    gate="$(cat "$IOLBOX_STRUCTURAL_GATE_JSON")"
    case "$gate" in
        *'"schema":1'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" 'structural gate assertion: attestation schema is missing' ;;
    esac
    case "$gate" in
        *'"drop_in":"/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" 'structural gate assertion: attestation has the wrong drop-in path' ;;
    esac
    case "$gate" in
        *'"canary_verdict":"PASS"'*) : ;;
        *) die "$IOLBOX_EXIT_VERIFY" 'structural gate assertion: attestation does not record canary PASS' ;;
    esac
    case "$gate" in
        *'"macos_product":""'*|*'"macos_product":"unknown"'*)
            die "$IOLBOX_EXIT_VERIFY" 'structural gate assertion: attestation lacks macOS product' ;;
        *) : ;;
    esac
    case "$gate" in
        *'"macos_build":""'*|*'"macos_build":"unknown"'*)
            die "$IOLBOX_EXIT_VERIFY" 'structural gate assertion: attestation lacks macOS build' ;;
        *) : ;;
    esac
}

rosetta_binfmt_state() {
    if [ ! -r "$IOLBOX_ROSETTA_BINFMT_FILE" ]; then
        printf 'missing\n'
    elif grep -q '^enabled' "$IOLBOX_ROSETTA_BINFMT_FILE"; then
        printf 'enabled\n'
    elif grep -q '^disabled' "$IOLBOX_ROSETTA_BINFMT_FILE"; then
        printf 'disabled\n'
    else
        printf 'unknown\n'
    fi
}

get_host_id() {
    local host_id

    if host_id="$(hostid 2>/dev/null)" && [ -n "$host_id" ]; then
        printf '%s\n' "$host_id"
        return 0
    fi
    if [ -r /etc/hostid ]; then
        od -An -tx1 -N4 /etc/hostid | tr -d ' \n'
        return 0
    fi
    return 1
}

get_registered_image_count() {
    local response count

    if ! exec 3<>/dev/tcp/127.0.0.1/4000; then
        return 1
    fi
    if ! printf '%s\n' '{"id":"macos-verify","op":"image.list","args":{}}' >&3; then
        exec 3>&-
        exec 3<&-
        return 1
    fi
    if ! IFS= read -r -t 3 response <&3; then
        exec 3>&-
        exec 3<&-
        return 1
    fi
    exec 3>&-
    exec 3<&-
    case "$response" in
        *'"ok":true'*) : ;;
        *) return 1 ;;
    esac
    count="$(printf '%s\n' "$response" | awk -F'"filename"' '{ n += NF - 1 } END { print n + 0 }')"
    case "$count" in
        ''|*[!0-9]*) return 1 ;;
        *) printf '%s\n' "$count" ;;
    esac
}

verify_capability_hello() {
    local request_id='macos-verify-hello' response

    if ! exec 3<>/dev/tcp/127.0.0.1/4000; then
        return 1
    fi
    if ! printf '%s\n' '{"id":"macos-verify-hello","op":"hello","args":{"client":"50-verify"}}' >&3; then
        exec 3>&-
        exec 3<&-
        return 1
    fi
    response=''
    while IFS= read -r -t 5 response <&3; do
        case "$response" in
            *'"id":"'"$request_id"'"'*) break ;;
        esac
    done
    exec 3>&-
    exec 3<&-
    case "$response" in
        *'"id":"'"$request_id"'"'*'"ok":true'*) : ;;
        *) return 1 ;;
    esac
    case "$response" in
        *'"arch":"x86_64"'*) : ;;
        *) return 1 ;;
    esac
    case "$response" in
        *'"features"'*'"i386"'*) return 1 ;;
        *) : ;;
    esac
    capability_hello_line="$response"
}

get_image_cache_count() {
    if [ ! -f "$IOLBOX_IMAGE_CACHE_FILE" ]; then
        printf '0\n'
        return 0
    fi
    awk -F'"sha256"' '{ n += NF - 1 } END { print n + 0 }' "$IOLBOX_IMAGE_CACHE_FILE"
}

json_kernel_series() {
    sed -n 's/.*"kernel_series":"\([^"]*\)".*/\1/p' "$1" | head -n1
}

json_host_id() {
    sed -n 's/.*"host_id":"\([^"]*\)".*/\1/p' "$1" | head -n1
}

json_iourc() {
    sed -n 's/.*"iourc":\(true\|false\).*/\1/p' "$1" | head -n1
}

json_cache_count() {
    sed -n 's/.*"image_cache_count":\([0-9][0-9]*\).*/\1/p' "$1" | head -n1
}

write_persistence_record() {
    local kernel_series_value="$1" host_id="$2" iourc_value="$3" cache_count="$4" tmp

    install -d -m 0755 -- "$(dirname "$IOLBOX_VERIFY_JSON")" || die "$IOLBOX_EXIT_VERIFY" \
        "persistence assertion: could not create $(dirname "$IOLBOX_VERIFY_JSON")"
    tmp="$(mktemp "${IOLBOX_VERIFY_JSON}.iolbox-tmp.XXXXXX")" || \
        die "$IOLBOX_EXIT_VERIFY" "persistence assertion: could not create temporary record"
    printf '{"kernel_series":"%s","host_id":"%s","iourc":%s,"image_cache_count":%s}\n' \
        "$kernel_series_value" "$host_id" "$iourc_value" "$cache_count" > "$tmp"
    chmod 0644 -- "$tmp" || die "$IOLBOX_EXIT_VERIFY" \
        "persistence assertion: could not set record permissions"
    mv -f -- "$tmp" "$IOLBOX_VERIFY_JSON" || die "$IOLBOX_EXIT_VERIFY" \
        "persistence assertion: could not install $IOLBOX_VERIFY_JSON"
}

check_persistence() {
    local kernel_series_value="$1" host_id="$2" iourc_value="$3" cache_count="$4"
    local old_kernel old_host old_iourc old_cache

    if [ -f "$IOLBOX_VERIFY_JSON" ]; then
        old_kernel="$(json_kernel_series "$IOLBOX_VERIFY_JSON")"
        old_host="$(json_host_id "$IOLBOX_VERIFY_JSON")"
        old_iourc="$(json_iourc "$IOLBOX_VERIFY_JSON")"
        old_cache="$(json_cache_count "$IOLBOX_VERIFY_JSON")"
        case "$old_kernel:$old_host:$old_cache" in
            *::*|*:*:|*:*:*[!0-9]*)
                die "$IOLBOX_EXIT_VERIFY" \
                    "persistence assertion: prior record is malformed: $IOLBOX_VERIFY_JSON" ;;
        esac
        case "$old_iourc" in
            true|false) : ;;
            *) die "$IOLBOX_EXIT_VERIFY" \
                "persistence assertion: prior record is malformed: $IOLBOX_VERIFY_JSON" ;;
        esac
        [ "$old_kernel" = "$kernel_series_value" ] || die "$IOLBOX_EXIT_VERIFY" \
            "persistence assertion: kernel series regressed from $old_kernel to $kernel_series_value"
        [ "$old_host" = "$host_id" ] || die "$IOLBOX_EXIT_VERIFY" \
            "persistence assertion: host ID changed from $old_host to $host_id"
        if [ "$old_iourc" = true ] && [ "$iourc_value" != true ]; then
            die "$IOLBOX_EXIT_VERIFY" \
                'persistence assertion: /opt/iolbox/iourc disappeared after restart'
        fi
        if [ "$cache_count" -lt "$old_cache" ]; then
            die "$IOLBOX_EXIT_VERIFY" \
                "persistence assertion: image-cache count shrank from $old_cache to $cache_count"
        fi
        log "persistence record matched: kernel=$kernel_series_value host_id=$host_id iourc=$iourc_value image_cache_count=$cache_count"
    else
        log "persistence baseline recorded: kernel=$kernel_series_value host_id=$host_id iourc=$iourc_value image_cache_count=$cache_count"
    fi
    write_persistence_record "$kernel_series_value" "$host_id" "$iourc_value" "$cache_count"
}

main() {
    local persistence=0 kernel_series_value host_id iourc_value cache_count
    local loader_output loader_line supervisor_version registered_images capability_hello_line

    while [ $# -gt 0 ]; do
        case "$1" in
            --persistence) persistence=1; shift ;;
            -h|--help) usage; return 0 ;;
            *) usage >&2; die "$IOLBOX_EXIT_USAGE" "unknown option: $1" ;;
        esac
    done

    [ "$(id -u)" -eq 0 ] || die "$IOLBOX_EXIT_PREFLIGHT" \
        "50-verify.sh must run as root"
    case "$IOLBOX_VERIFY_TIMEOUT_SECONDS" in
        ''|*[!0-9]*) die "$IOLBOX_EXIT_USAGE" \
            "IOLBOX_VERIFY_TIMEOUT_SECONDS must be a non-negative integer" ;;
    esac
    case "$IOLBOX_GUI_PORT" in
        ''|*[!0-9]*) die "$IOLBOX_EXIT_USAGE" "IOLBOX_GUI_PORT must be numeric" ;;
    esac
    have systemctl || die "$IOLBOX_EXIT_VERIFY" \
        'service assertion: systemctl is not available'
    have ss || die "$IOLBOX_EXIT_VERIFY" 'socket assertion: ss is not available'
    have curl || die "$IOLBOX_EXIT_VERIFY" 'readiness assertion: curl is not available'

    if ! wait_for_service_and_port; then
        supervisor_status="$(systemctl is-active iolbox-supervisor.service 2>/dev/null || true)"
        if [ "$supervisor_status" != active ]; then
            die "$IOLBOX_EXIT_VERIFY" \
                "service assertion: systemctl is-active iolbox-supervisor.service returned '${supervisor_status:-unknown}'"
        fi
        if ! port_is_bound; then
            die "$IOLBOX_EXIT_VERIFY" \
                "socket assertion: ss -ltn shows no listener on GUI port $IOLBOX_GUI_PORT"
        fi
    fi
    if ! wait_for_http_ready; then
        die "$IOLBOX_EXIT_VERIFY" \
            "readiness assertion: GET http://127.0.0.1:$IOLBOX_GUI_PORT/ did not return status < 500 (last status ${http_status:-unknown})"
    fi

    verify_structural_attestation

    if ! verify_capability_hello; then
        die "$IOLBOX_EXIT_VERIFY" \
            'capability assertion: correlated hello did not prove i386 is absent with runtime arch x86_64'
    fi

    if loader_output="$("$IOLBOX_LOADER" --version 2>&1)"; then
        loader_line="${loader_output%%$'\n'*}"
    else
        die "$IOLBOX_EXIT_VERIFY" \
            "glibc loader canary assertion: $IOLBOX_LOADER --version failed: $loader_output"
    fi
    if supervisor_version="$(/opt/iolbox/supervisor --version 2>&1)"; then
        supervisor_version="${supervisor_version%%$'\n'*}"
    else
        die "$IOLBOX_EXIT_VERIFY" 'supervisor assertion: /opt/iolbox/supervisor --version failed'
    fi
    if ! registered_images="$(get_registered_image_count)"; then
        die "$IOLBOX_EXIT_VERIFY" \
            'image registry assertion: supervisor image.list could not be queried'
    fi
    if ! host_id="$(get_host_id)" || [ -z "$host_id" ]; then
        die "$IOLBOX_EXIT_VERIFY" 'persistence assertion: guest host ID could not be read'
    fi
    case "$host_id" in
        *[!a-zA-Z0-9._-]*) die "$IOLBOX_EXIT_VERIFY" \
            "persistence assertion: unsafe host ID value '$host_id'" ;;
    esac
    assert_kernel_qualification "$IOLBOX_EXIT_VERIFY"
    kernel_series_value="$(kernel_series)"
    if [ -e /opt/iolbox/iourc ]; then
        iourc_value=true
    else
        iourc_value=false
        die "$IOLBOX_EXIT_VERIFY" 'licence assertion: /opt/iolbox/iourc does not exist'
    fi
    cache_count="$(get_image_cache_count)"
    case "$cache_count" in
        ''|*[!0-9]*) die "$IOLBOX_EXIT_VERIFY" \
            'image-cache assertion: cache count is not a non-negative integer' ;;
    esac

    printf '\n=== iolbox macOS guest verification ===\n'
    printf 'guest_kernel=%s\n' "$(uname -r)"
    printf 'guest_arch=%s\n' "$(uname -m)"
    printf 'rosetta_binfmt=%s\n' "$(rosetta_binfmt_state)"
    printf 'glibc_loader_canary=%s\n' "$loader_line"
    printf 'capability_hello=%s\n' "$capability_hello_line"
    printf 'supervisor_version=%s status=%s\n' "$supervisor_version" "$supervisor_status"
    printf 'host_id=%s\n' "$host_id"
    printf 'iourc_exists=%s\n' "$iourc_value"
    printf 'registered_images=%s\n' "$registered_images"
    printf 'image_cache_count=%s\n' "$cache_count"
    printf 'gui_http_status=%s\n' "$http_status"
    printf '=== end verification ===\n'

    if [ "$persistence" -eq 1 ]; then
        check_persistence "$kernel_series_value" "$host_id" "$iourc_value" "$cache_count"
    fi
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
