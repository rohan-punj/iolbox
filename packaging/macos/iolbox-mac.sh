#!/usr/bin/env bash
# iolbox-mac.sh — the dependency-free macOS/Lima entry point for M1.
#
# In order, `provision` resolves the selected profile, checks the Apple Silicon
# host, Lima, and free disk; renders the locked profile template from its pin
# file; creates or reuses the named VZ machine; starts it; copies the guest
# steps and payload through limactl copy; and runs the selected steps in order.
# A failed guest step stops the run and is returned unchanged, so the caller
# can distinguish the Rosetta canary (2), preflight (3), apt (4), and verify
# (5) contracts defined by guest/lib.sh.  `canary`, `status`, `destroy`, and
# `doctor` are small lifecycle/diagnostic operations around that same machine.
#
# macOS ships Bash 3.2.  This is the only M1 script with that constraint:
# there are no associative arrays, Bash 4 parameter case conversion, or
# bulk array readers here. The guest scripts run under Jammy's Bash 5 instead.
# Debian 13 is the DEFAULT; Jammy is COMPATIBILITY and Debian 12 is an
# unpinned CANDIDATE. The live guest canary is the only compatibility authority.
#
# The Mac home is not used as a guest mount for staging.  limactl copy is used
# explicitly because it works with the read-only home mount in the template.
# The 5 GiB free-space gate is intentionally documented here: M0 measured
# about 5.4 GB free, while a sparse 15 GiB VM and a several-hundred-MB image
# download can still exhaust a small Mac volume.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIMA_DIR="$SCRIPT_DIR/lima"
PROFILE_TABLE_FILE="$LIMA_DIR/profiles.env"
ENV_FILE=""
TEMPLATE_FILE=""
GUEST_DIR="$SCRIPT_DIR/guest"
GUEST_PROVISION_DIR="${IOLBOX_PROVISION_DIR:-/opt/iolbox-provision}"
IOLBOX_GUI_PORT="${IOLBOX_GUI_PORT:-4001}"
IOLBOX_BIND="${IOLBOX_BIND:-all}"
# The standalone canary does not need a host payload, but the shared guest
# command contract still exports this name on every step.  Provision replaces
# this harmless sentinel with the staged tarball path during preflight.
IOLBOX_PAYLOAD_TARBALL="${IOLBOX_PAYLOAD_TARBALL:-$GUEST_PROVISION_DIR/payload/unknown.tar.gz}"
IOLBOX_TMP_ROOT="${TMPDIR:-/tmp}"
IOLBOX_STATE_ROOT="${IOLBOX_STATE_DIR:-${HOME:-.}/Library/Application Support/iolbox}"

# The threshold is in GiB, while df -Pk reports KiB.  Five GiB is the lowest
# documented launch threshold and leaves a small amount of headroom above the
# approximately 5.4 GB free recorded by M0; it is a gate, not a promise that
# a nearly-full APFS volume can absorb arbitrary guest growth.
MIN_FREE_DISK_GIB=5
MIN_FREE_DISK_KB=$((MIN_FREE_DISK_GIB * 1024 * 1024))

COMMAND="provision"
COMMAND_SET=0
DRY_RUN=0
YES=0
PROFILE_REQUESTED="${IOLBOX_PROFILE:-}"

LIMACTL_BIN=""
HOST_OS="unknown"
HOST_ARCH="unknown"
HOST_MACOS="unknown"
HOST_MACOS_PRODUCT="unknown"
HOST_MACOS_BUILD="unknown"
HOST_LIMA="unknown"
HOST_LIMA_RAW="unknown"
MACHINE=""
MACHINE_LISTING=""
MACHINE_STATE=""
HOST_ATTESTATION_DIR=""
HOST_ATTESTATION_FILE=""
PAYLOAD_PATH=""
PAYLOAD_BASENAME=""
RENDERED_YAML=""
PROFILE_NAME=""
PROFILE_ROLE=""
PROFILE_GUEST=""
PROFILE_PIN_ENV=""
PROFILE_TEMPLATE=""
PROFILE_MULTIARCH_STEP=""
PROFILE_KERNEL_HOLD_STEP=""
PROFILE_KERNEL_SERIES=""
PROFILE_EXPECTED_UNAME_R=""
PROFILE_KERNEL_RUNTIME_SERIES=""
PROFILE_STATUS=""
PROFILE_QUALIFICATION="UNMEASURED — CANARY REQUIRED"
PROFILE_QUALIFICATION_VERDICT=""
PROFILE_QUALIFICATION_STATUS=""
PROFILE_QUALIFICATION_EVIDENCE=""
IOLBOX_IMAGE_URL=""
IOLBOX_IMAGE_DIGEST=""
IOLBOX_IMAGE_BYTES=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [--profile <name>] [--dry-run] [--yes] [COMMAND]

Provision or inspect a table-defined iolbox arm64 Lima guest profile.

Commands:
  provision       create or reuse, start, stage, and run all guest steps (default)
  canary          run only the staged guest/30-canary.sh in the existing VM
  status          show host, Lima, guest, canary, service, and GET / state
  destroy         stop and delete the VM (requires --yes or confirmation)
  doctor          print the complete diagnostic bundle and exit successfully
  profiles        print the available guest profiles and their status

Options:
  --profile <name> select a profile (default: table DEFAULT; IOLBOX_PROFILE is honored)
  --dry-run       print the commands that would run; do not invoke Lima or write state
  --yes           skip the destructive confirmation required by destroy
  -h, --help      show this help

Environment:
  LIMACTL         explicit limactl path; otherwise standard Homebrew paths are tried
  IOLBOX_TARBALL  payload path; otherwise newest iolbox-server-*.tar.gz is selected
  IOLBOX_PROFILE  profile name; --profile takes precedence when both are set
  IOLBOX_MACHINE  optional machine override; default is iolbox-<profile>
  IOLBOX_BIND     guest install bind mode (default: all)
  IOLBOX_GUI_PORT guest GUI port (default: 4001)
EOF
}

log() {
    printf '[iolbox-mac] %s\n' "$*" >&2
}

warn() {
    printf '[iolbox-mac] WARNING: %s\n' "$*" >&2
}

die() {
    local code="$1"
    shift
    printf '[iolbox-mac] ERROR: %s\n' "$*" >&2
    exit "$code"
}

load_profile_table() {
    local default_count

    if [ ! -r "$PROFILE_TABLE_FILE" ]; then
        die 1 "profile table is missing: $PROFILE_TABLE_FILE"
    fi
    # shellcheck disable=SC1090
    . "$PROFILE_TABLE_FILE"
    [ -n "${IOLBOX_PROFILE_TABLE:-}" ] || die 1 \
        "profile table is empty: $PROFILE_TABLE_FILE"
    [ -n "${IOLBOX_QUALIFICATION_TABLE:-}" ] || die 1 \
        "qualification table is empty: $PROFILE_TABLE_FILE"
    default_count="$(printf '%s\n' "$IOLBOX_PROFILE_TABLE" | awk -F '|' '$2 == "DEFAULT" { count++ } END { print count + 0 }')"
    [ "$default_count" -eq 1 ] || die 1 \
        "profile table must contain exactly one DEFAULT row (found $default_count)"
    if [ -z "$PROFILE_REQUESTED" ]; then
        PROFILE_REQUESTED="$(printf '%s\n' "$IOLBOX_PROFILE_TABLE" | awk -F '|' '$2 == "DEFAULT" { print $1; exit }')"
    fi
}

select_profile() {
    local record

    record="$(printf '%s\n' "$IOLBOX_PROFILE_TABLE" | awk -F '|' \
        -v wanted="$PROFILE_REQUESTED" '$1 == wanted { print; exit }')"
    [ -n "$record" ] || die 1 \
        "unknown profile '$PROFILE_REQUESTED'; run $(basename "$0") profiles"
    IFS='|' read -r PROFILE_NAME PROFILE_ROLE PROFILE_GUEST PROFILE_PIN_ENV PROFILE_TEMPLATE \
        PROFILE_MULTIARCH_STEP PROFILE_KERNEL_HOLD_STEP PROFILE_KERNEL_SERIES \
        PROFILE_EXPECTED_UNAME_R <<EOF
$record
EOF
    ENV_FILE="$LIMA_DIR/$PROFILE_PIN_ENV"
    TEMPLATE_FILE="$LIMA_DIR/$PROFILE_TEMPLATE"
    PROFILE_STATUS="$PROFILE_ROLE"
    PROFILE_KERNEL_RUNTIME_SERIES="$(printf '%s\n' "$PROFILE_KERNEL_SERIES" | cut -d. -f1,2)"
}

select_qualification() {
    local record

    PROFILE_QUALIFICATION="UNMEASURED — CANARY REQUIRED"
    PROFILE_QUALIFICATION_VERDICT=""
    PROFILE_QUALIFICATION_STATUS=""
    PROFILE_QUALIFICATION_EVIDENCE=""
    if [ "$HOST_MACOS_PRODUCT" = unknown ] || [ "$HOST_MACOS_BUILD" = unknown ]; then
        return 0
    fi
    record="$(printf '%s\n' "$IOLBOX_QUALIFICATION_TABLE" | awk -F '|' \
        -v profile="$PROFILE_NAME" -v product="$HOST_MACOS_PRODUCT" \
        -v build="$HOST_MACOS_BUILD" \
        '$1 == profile && $2 == product && $3 == build { print; exit }')"
    if [ -n "$record" ]; then
        IFS='|' read -r PROFILE_NAME_QUALIFIED HOST_MACOS_PRODUCT_QUALIFIED \
            HOST_MACOS_BUILD_QUALIFIED PROFILE_QUALIFICATION_VERDICT \
            PROFILE_QUALIFICATION_STATUS PROFILE_QUALIFICATION_EVIDENCE <<EOF
$record
EOF
        PROFILE_QUALIFICATION="$PROFILE_QUALIFICATION_VERDICT ($PROFILE_QUALIFICATION_STATUS)"
    fi
}

profile_summary() {
    printf '  profile: %s\n' "$PROFILE_NAME"
    printf '  role: %s\n' "$PROFILE_ROLE"
    printf '  guest: %s\n' "$PROFILE_GUEST"
    printf '  kernel series: %s\n' "$PROFILE_KERNEL_SERIES"
    printf '  qualification: %s\n' "$PROFILE_QUALIFICATION"
    if [ -n "$PROFILE_QUALIFICATION_EVIDENCE" ]; then
        printf '  qualification evidence: %s\n' "$PROFILE_QUALIFICATION_EVIDENCE"
    fi
}

print_profiles_command() {
    local name role guest pin template multiarch hold series expected
    local qualification_record qualification

    printf '%-12s %-16s %-24s %-10s %s\n' 'NAME' 'ROLE' 'GUEST' 'KERNEL' 'QUALIFICATION'
    while IFS='|' read -r name role guest pin template multiarch hold series expected; do
        [ -n "$name" ] || continue
        qualification='UNMEASURED — CANARY REQUIRED'
        if [ "$HOST_MACOS_PRODUCT" != unknown ] && [ "$HOST_MACOS_BUILD" != unknown ]; then
            qualification_record="$(printf '%s\n' "$IOLBOX_QUALIFICATION_TABLE" | awk -F '|' \
                -v profile="$name" -v product="$HOST_MACOS_PRODUCT" \
                -v build="$HOST_MACOS_BUILD" \
                '$1 == profile && $2 == product && $3 == build { print; exit }')"
            if [ -n "$qualification_record" ]; then
                qualification="$(printf '%s\n' "$qualification_record" | awk -F '|' '{ print $4 " (" $5 ")" }')"
            fi
        fi
        printf '%-12s %-16s %-24s %-10s %s\n' \
            "$name" "$role" "$guest" "$series" "$qualification"
    done <<EOF
$IOLBOX_PROFILE_TABLE
EOF
}

assert_profile_provisionable() {
    # Static roles and qualification rows are descriptive. Only the live guest
    # canary may reject a host/profile pair.
    :
}

assert_profile_pinned() {
    local checksum_url

    if [ "$IOLBOX_IMAGE_DIGEST" = PIN-ME ]; then
        checksum_url="${IOLBOX_IMAGE_URL%/*}/SHA512SUMS"
        die 3 \
            "refusing to provision profile $PROFILE_NAME: its image digest is still PIN-ME. Debian publishes SHA512SUMS only; fetch the checksum for exactly $IOLBOX_IMAGE_URL from $checksum_url, record an algorithm-qualified sha512 digest in $ENV_FILE, and retry."
    fi
}

cleanup() {
    if [ -n "$RENDERED_YAML" ] && [ -f "$RENDERED_YAML" ]; then
        rm -f "$RENDERED_YAML"
    fi
}

trap cleanup EXIT

# `limactl` is not reliably on PATH over a non-login SSH session.  An explicit
# LIMACTL wins; otherwise use the two Homebrew locations from the M0 host and
# only then fall back to command lookup.
locate_limactl() {
    local candidate

    if [ -n "${LIMACTL:-}" ]; then
        if [ -x "$LIMACTL" ]; then
            printf '%s\n' "$LIMACTL"
            return 0
        fi
        candidate="$(command -v "$LIMACTL" 2>/dev/null || true)"
        if [ -n "$candidate" ] && [ -x "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
        return 1
    fi

    for candidate in /opt/homebrew/bin/limactl /usr/local/bin/limactl; do
        if [ -x "$candidate" ]; then
            printf '%s\n' "$candidate"
            return 0
        fi
    done

    candidate="$(command -v limactl 2>/dev/null || true)"
    if [ -n "$candidate" ]; then
        printf '%s\n' "$candidate"
        return 0
    fi
    return 1
}

collect_host_facts() {
    local product build

    HOST_OS="$(uname -s 2>/dev/null || printf 'unknown')"
    HOST_ARCH="$(uname -m 2>/dev/null || printf 'unknown')"
    HOST_MACOS="unknown"
    HOST_MACOS_PRODUCT="unknown"
    HOST_MACOS_BUILD="unknown"
    if [ "$HOST_OS" = "Darwin" ] && command -v sw_vers >/dev/null 2>&1; then
        product="$(sw_vers -productVersion 2>/dev/null || true)"
        build="$(sw_vers -buildVersion 2>/dev/null || true)"
        if [ -n "$product" ] && [ -n "$build" ]; then
            HOST_MACOS_PRODUCT="$product"
            HOST_MACOS_BUILD="$build"
            HOST_MACOS="$product ($build)"
        elif [ -n "$product" ]; then
            HOST_MACOS_PRODUCT="$product"
            HOST_MACOS="$product (build unknown)"
        fi
    fi
}

extract_version() {
    local raw="$1"
    printf '%s\n' "$raw" | sed -nE 's/[^0-9]*([0-9]+\.[0-9]+\.[0-9]+).*/\1/p' | head -n 1 || true
}

read_lima_version() {
    HOST_LIMA_RAW="$("$LIMACTL_BIN" --version 2>&1 || true)"
    HOST_LIMA="$(extract_version "$HOST_LIMA_RAW")"
    if [ -z "$HOST_LIMA" ]; then
        HOST_LIMA="unknown"
    fi
}

load_config() {
    local digest_hex digest_length

    if [ ! -r "$ENV_FILE" ]; then
        printf 'missing pinned configuration: %s\n' "$ENV_FILE" >&2
        return 1
    fi
    # shellcheck disable=SC1090
    . "$ENV_FILE"

    case "$IOLBOX_IMAGE_URL" in
        https://*) ;;
        *) printf 'invalid or missing pinned image URL in %s\n' "$ENV_FILE" >&2; return 1 ;;
    esac
    case "$IOLBOX_IMAGE_DIGEST" in
        PIN-ME) ;;
        sha256:*|sha512:*) ;;
        ''|*)
            printf 'invalid algorithm-qualified image digest in %s\n' "$ENV_FILE" >&2
            return 1
            ;;
    esac
    if [ "$IOLBOX_IMAGE_DIGEST" != PIN-ME ]; then
        case "$IOLBOX_IMAGE_DIGEST" in
            sha256:*) digest_length=64 ;;
            sha512:*) digest_length=128 ;;
            *) printf 'pinned image digest has the wrong length in %s\n' "$ENV_FILE" >&2; return 1 ;;
        esac
        digest_hex="${IOLBOX_IMAGE_DIGEST#*:}"
        if [ "${#digest_hex}" -ne "$digest_length" ]; then
            printf 'pinned image digest has the wrong length in %s\n' "$ENV_FILE" >&2
            return 1
        fi
        case "$digest_hex" in
            ''|*[!0123456789abcdefABCDEF]*)
                printf 'pinned image digest is not hexadecimal in %s\n' "$ENV_FILE" >&2
                return 1
                ;;
        esac
    fi
    case "$IOLBOX_IMAGE_BYTES" in
        ''|*[!0-9]*) printf 'image byte count must be decimal in %s\n' "$ENV_FILE" >&2; return 1 ;;
    esac
    : "${IOLBOX_CPUS:?missing IOLBOX_CPUS in $ENV_FILE}"
    : "${IOLBOX_MEMORY:?missing IOLBOX_MEMORY in $ENV_FILE}"
    : "${IOLBOX_DISK:?missing IOLBOX_DISK in $ENV_FILE}"
    MACHINE="${IOLBOX_MACHINE:-iolbox-$PROFILE_NAME}"
    HOST_ATTESTATION_DIR="${HOME:-.}/.iolbox/macos"
    HOST_ATTESTATION_FILE="$HOST_ATTESTATION_DIR/${MACHINE}-structural-gate.json"
}

free_disk_kb() {
    local disk_path="${HOME:-.}"
    local available

    available="$(df -Pk "$disk_path" 2>/dev/null | awk 'NR == 2 {print $4}' || true)"
    case "$available" in
        ''|*[!0-9]*) return 1 ;;
    esac
    printf '%s\n' "$available"
}

format_disk_kb() {
    awk -v kb="$1" 'BEGIN {printf "%.2f GiB", kb / 1048576}'
}

file_mtime() {
    if [ "$HOST_OS" = "Darwin" ]; then
        stat -f '%m' "$1" 2>/dev/null
    else
        stat -c '%Y' "$1" 2>/dev/null
    fi
}

# Find the newest payload without ls -t or nullglob, both of which become
# awkward around spaces and unmatched globs on the Mac.  The search locations
# are intentionally only next to this script and the caller's current folder.
discover_payload() {
    local candidate mtime
    local newest_mtime=-1
    local current_dir

    PAYLOAD_PATH=""
    PAYLOAD_BASENAME=""
    if [ -n "${IOLBOX_TARBALL:-}" ]; then
        if [ -f "$IOLBOX_TARBALL" ]; then
            PAYLOAD_PATH="$IOLBOX_TARBALL"
            PAYLOAD_BASENAME="$(basename "$PAYLOAD_PATH")"
            return 0
        fi
        return 1
    fi

    current_dir="$(pwd -P)"
    for candidate in "$SCRIPT_DIR"/iolbox-server-*.tar.gz "$current_dir"/iolbox-server-*.tar.gz; do
        if [ -f "$candidate" ]; then
            mtime="$(file_mtime "$candidate" 2>/dev/null || printf '0')"
            case "$mtime" in
                ''|*[!0-9]*) mtime=0 ;;
            esac
            if [ "$mtime" -ge "$newest_mtime" ]; then
                newest_mtime="$mtime"
                PAYLOAD_PATH="$candidate"
                PAYLOAD_BASENAME="$(basename "$candidate")"
            fi
        fi
    done
    [ -n "$PAYLOAD_PATH" ]
}

require_payload() {
    if discover_payload; then
        return 0
    fi
    if [ -n "${IOLBOX_TARBALL:-}" ]; then
        die 3 "IOLBOX_TARBALL is set but is not a regular file: $IOLBOX_TARBALL"
    fi
    die 3 "no payload found; looked for IOLBOX_TARBALL, next to $SCRIPT_DIR, and in $(pwd -P) (iolbox-server-*.tar.gz)"
}

validate_guest_layout() {
    local required missing
    missing=""
    if [ ! -d "$GUEST_DIR" ]; then
        die 1 "guest directory is missing: $GUEST_DIR"
    fi
    for required in lib.sh "$PROFILE_MULTIARCH_STEP" "$PROFILE_KERNEL_HOLD_STEP" \
        30-canary.sh 40-install-payload.sh 50-verify.sh; do
        if [ ! -f "$GUEST_DIR/$required" ]; then
            missing="$missing $required"
        fi
    done
    if [ -n "$missing" ]; then
        die 1 "guest directory is incomplete; missing:$missing"
    fi
}

preflight() {
    local available

    assert_profile_provisionable
    assert_profile_pinned
    collect_host_facts
    if [ "$HOST_OS" != "Darwin" ]; then
        die 3 "this entry point runs on macOS; detected $HOST_OS ($HOST_ARCH)"
    fi
    if [ "$HOST_ARCH" != "arm64" ]; then
        die 3 "Apple Silicon arm64 is required; uname -m reported $HOST_ARCH"
    fi
    if [ "$HOST_MACOS" = "unknown" ]; then
        die 3 "could not read macOS product version/build with sw_vers"
    fi
    select_qualification
    if ! LIMACTL_BIN="$(locate_limactl)"; then
        die 3 "Lima was not found. Install Lima or set LIMACTL to its executable (tried /opt/homebrew/bin/limactl, /usr/local/bin/limactl, then PATH)"
    fi
    read_lima_version
    if [ "$HOST_LIMA" = "unknown" ]; then
        warn "could not parse Lima version from: $HOST_LIMA_RAW; the qualified M0 host used Lima 2.2.0"
    fi

    available="$(free_disk_kb || true)"
    if [ -z "$available" ]; then
        die 3 "could not measure free disk space at ${HOME:-.} with df -Pk"
    fi
    log "profile=$PROFILE_NAME host=$HOST_MACOS arch=$HOST_ARCH Lima=$HOST_LIMA free=$(format_disk_kb "$available") threshold=${MIN_FREE_DISK_GIB}.00 GiB"
    if [ "$available" -lt "$MIN_FREE_DISK_KB" ]; then
        die 3 "free disk is $(format_disk_kb "$available"), below the ${MIN_FREE_DISK_GIB} GiB minimum; the 15 GiB sparse VM and profile image download need more headroom"
    fi

    require_payload
    validate_guest_layout
    export IOLBOX_HOST_MACOS="$HOST_MACOS"
    export IOLBOX_HOST_MACOS_PRODUCT="$HOST_MACOS_PRODUCT"
    export IOLBOX_HOST_MACOS_BUILD="$HOST_MACOS_BUILD"
    export IOLBOX_HOST_LIMA="$HOST_LIMA"
    export IOLBOX_MACHINE="$MACHINE"
    export IOLBOX_PROFILE="$PROFILE_NAME"
    export IOLBOX_PROFILE_STATUS="$PROFILE_STATUS"
    export IOLBOX_KERNEL_SERIES="$PROFILE_KERNEL_RUNTIME_SERIES"
    export IOLBOX_EXPECTED_UNAME_R="$PROFILE_EXPECTED_UNAME_R"
    export IOLBOX_PROVISION_DIR="$GUEST_PROVISION_DIR"
    export IOLBOX_PAYLOAD_TARBALL="$GUEST_PROVISION_DIR/payload/$PAYLOAD_BASENAME"
    export IOLBOX_BIND IOLBOX_GUI_PORT
}

machine_state() {
    local listing

    if ! listing="$("$LIMACTL_BIN" list --format '{{.Name}}|{{.Status}}' 2>&1)"; then
        MACHINE_LISTING="$listing"
        MACHINE_STATE=""
        return 1
    fi
    MACHINE_LISTING="$listing"
    MACHINE_STATE="$(printf '%s\n' "$listing" | awk -F '|' -v target="$MACHINE" '$1 == target {print $2; exit}')"
    return 0
}

attestation_has() {
    local needle="$1"
    grep -F -- "$needle" "$HOST_ATTESTATION_FILE" >/dev/null 2>&1
}

host_attestation_is_valid() {
    local line_count

    [ -r "$HOST_ATTESTATION_FILE" ] || return 1
    line_count="$(awk 'END { print NR + 0 }' "$HOST_ATTESTATION_FILE" 2>/dev/null || printf '0')"
    [ "$line_count" -eq 1 ] || return 1
    attestation_has '"schema":1' || return 1
    attestation_has "\"profile\":\"$PROFILE_NAME\"" || return 1
    attestation_has "\"macos_product\":\"$HOST_MACOS_PRODUCT\"" || return 1
    attestation_has "\"macos_build\":\"$HOST_MACOS_BUILD\"" || return 1
    attestation_has "\"lima_version\":\"$HOST_LIMA\"" || return 1
    attestation_has '"drop_in":"/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"' || return 1
    attestation_has '"canary_verdict":"PASS"' || return 1
    attestation_has '"kernel":"' || return 1
    attestation_has '"timestamp":"' || return 1
    return 0
}

require_host_attestation() {
    if ! host_attestation_is_valid; then
        die 3 "refusing to start existing stopped machine $MACHINE before the structural canary gate: missing or invalid host attestation $HOST_ATTESTATION_FILE for profile=$PROFILE_NAME macos_product=$HOST_MACOS_PRODUCT macos_build=$HOST_MACOS_BUILD lima_version=$HOST_LIMA"
    fi
}

sync_host_attestation() {
    local guest_attestation
    local temp_file

    guest_attestation="$("$LIMACTL_BIN" shell "$MACHINE" sudo -n cat /var/lib/iolbox/macos-structural-gate.json 2>&1)" || die 3 "guest structural gate attestation is unavailable at /var/lib/iolbox/macos-structural-gate.json; refusing to authorize a future stopped-machine start: $guest_attestation"
    if [ "$(printf '%s\n' "$guest_attestation" | awk 'END { print NR + 0 }')" -ne 1 ]; then
        die 3 "guest structural gate attestation is not single-line JSON; refusing to write $HOST_ATTESTATION_FILE"
    fi
    mkdir -p "$HOST_ATTESTATION_DIR" || die 3 "could not create host attestation directory: $HOST_ATTESTATION_DIR"
    temp_file="$HOST_ATTESTATION_FILE.tmp.$$"
    printf '%s\n' "$guest_attestation" > "$temp_file" || die 3 "could not write host attestation staging file: $temp_file"
    mv -f "$temp_file" "$HOST_ATTESTATION_FILE" || die 3 "could not install host attestation: $HOST_ATTESTATION_FILE"
    if ! host_attestation_is_valid; then
        rm -f -- "$HOST_ATTESTATION_FILE"
        die 3 "guest structural gate attestation did not match the current profile/host facts; refusing to authorize future starts"
    fi
}

render_template() {
    local unresolved

    if [ ! -r "$TEMPLATE_FILE" ]; then
        die 1 "Lima template is missing: $TEMPLATE_FILE"
    fi
    RENDERED_YAML="$(mktemp "$IOLBOX_TMP_ROOT/iolbox-$PROFILE_NAME.XXXXXX")"
    sed \
        -e "s|@IOLBOX_IMAGE_URL@|$IOLBOX_IMAGE_URL|g" \
        -e "s|@IOLBOX_IMAGE_DIGEST@|$IOLBOX_IMAGE_DIGEST|g" \
        -e "s|@CPUS@|$IOLBOX_CPUS|g" \
        -e "s|@MEMORY@|$IOLBOX_MEMORY|g" \
        -e "s|@DISK@|$IOLBOX_DISK|g" \
        "$TEMPLATE_FILE" > "$RENDERED_YAML"
    unresolved="$(grep -E '@(IOLBOX_[A-Z0-9_]+|CPUS|MEMORY|DISK)@' "$RENDERED_YAML" || true)"
    if [ -n "$unresolved" ]; then
        die 1 "rendered Lima template still contains placeholders: $RENDERED_YAML"
    fi
}

ensure_machine() {
    local state
    local created=0

    if ! machine_state; then
        die 3 "could not query Lima machine list with $LIMACTL_BIN: $MACHINE_LISTING"
    fi
    state="$MACHINE_STATE"
    if [ -z "$state" ]; then
        if [ -n "$HOST_ATTESTATION_FILE" ]; then
            rm -f -- "$HOST_ATTESTATION_FILE"
        fi
        log "creating Lima machine $MACHINE from rendered locked template"
        render_template
        "$LIMACTL_BIN" create --name="$MACHINE" "$RENDERED_YAML"
        state="Stopped"
        created=1
    else
        log "reusing existing Lima machine $MACHINE (state=$state)"
    fi
    case "$state" in
        Running|running)
            ;;
        Stopped|stopped)
            if [ "$created" -eq 0 ]; then
                require_host_attestation
            fi
            log "starting Lima machine $MACHINE"
            "$LIMACTL_BIN" start "$MACHINE" --tty=false
            ;;
        *)
            die 3 "refusing to start existing Lima machine $MACHINE in unrecognized state '$state'"
            ;;
    esac
}

stage_files() {
    local stage_dir="/tmp/iolbox-provision-stage"
    local stage_payload="/tmp/iolbox-payload.tar.gz"

    log "staging guest scripts and $PAYLOAD_BASENAME through limactl copy"
    # Both destinations are fixed, narrow staging paths.  Removing them first
    # makes a second provision deterministic even if a previous run left extra
    # guest files behind.
    "$LIMACTL_BIN" shell "$MACHINE" sudo -n rm -rf "$stage_dir" "$GUEST_PROVISION_DIR"
    "$LIMACTL_BIN" copy "$GUEST_DIR" "$MACHINE:$stage_dir"
    "$LIMACTL_BIN" copy "$PAYLOAD_PATH" "$MACHINE:$stage_payload"
    # Flatten the staged tree into $GUEST_PROVISION_DIR rather than `mv`ing the
    # directory itself.  Two separate hazards made the naive `mv` wrong, and both
    # were observed on hardware (macOS 26.6.1, Debian 13 guest):
    #
    #   1. `limactl copy <dir> <machine>:<path>` creates <path>/<basename-of-dir>,
    #      so the steps land one level deeper than the destination path suggests.
    #   2. `mv SRC DEST` when DEST already exists moves SRC *inside* DEST instead
    #      of renaming it — and DEST already existed because the payload mkdir ran
    #      first.  The two compounded into
    #      /opt/iolbox-provision/iolbox-provision-stage/guest/10-multiarch-debian.sh
    #      and every step then failed with exit 127.
    #
    # Copying the step files by content is immune to both, and to any future
    # change in how limactl lays out a directory copy.
    "$LIMACTL_BIN" shell "$MACHINE" sudo -n mkdir -p "$GUEST_PROVISION_DIR/payload"
    "$LIMACTL_BIN" shell "$MACHINE" sudo -n sh -c \
        "set -e; \
         src=\"\$(find '$stage_dir' -name 'lib.sh' -print -quit)\"; \
         [ -n \"\$src\" ] || { echo 'staging: lib.sh not found under $stage_dir' >&2; exit 1; }; \
         src=\"\$(dirname \"\$src\")\"; \
         cp -f \"\$src\"/*.sh '$GUEST_PROVISION_DIR/'; \
         chmod 0755 '$GUEST_PROVISION_DIR'/*.sh; \
         rm -rf '$stage_dir'"
    "$LIMACTL_BIN" shell "$MACHINE" sudo -n mv "$stage_payload" "$GUEST_PROVISION_DIR/payload/$PAYLOAD_BASENAME"
    # Fail here rather than at the first step's exit 127, which reads like a
    # broken script instead of a broken staging step.
    "$LIMACTL_BIN" shell "$MACHINE" sudo -n test -f "$GUEST_PROVISION_DIR/30-canary.sh" \
        || die 1 "staging failed: $GUEST_PROVISION_DIR/30-canary.sh is missing after limactl copy"
}

record_canary() {
    local result="$1"
    local code="$2"
    local state_file

    if [ "$DRY_RUN" -eq 1 ]; then
        return 0
    fi
    state_file="$IOLBOX_STATE_ROOT/lima-canary-$MACHINE.txt"
    if ! mkdir -p "$IOLBOX_STATE_ROOT"; then
        warn "could not create canary state directory: $IOLBOX_STATE_ROOT"
        return 0
    fi
    if ! {
        printf 'result=%s\n' "$result"
        printf 'exit=%s\n' "$code"
        printf 'host_macos=%s\n' "$HOST_MACOS"
        printf 'host_lima=%s\n' "$HOST_LIMA"
        printf 'recorded_utc=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    } > "$state_file"; then
        warn "could not record last canary result: $state_file"
    fi
}

run_guest_step() {
    local step_name="$1"
    local step_path
    local step_code

    step_path="$GUEST_PROVISION_DIR/$step_name"
    log "running guest step $step_name"
    if "$LIMACTL_BIN" shell "$MACHINE" sudo -E env \
        "IOLBOX_HOST_MACOS=$IOLBOX_HOST_MACOS" \
        "IOLBOX_HOST_LIMA=$IOLBOX_HOST_LIMA" \
        "IOLBOX_MACHINE=$IOLBOX_MACHINE" \
        "IOLBOX_PROFILE=$PROFILE_NAME" \
        "IOLBOX_PROFILE_STATUS=$PROFILE_STATUS" \
        "IOLBOX_KERNEL_SERIES=$PROFILE_KERNEL_RUNTIME_SERIES" \
        "IOLBOX_EXPECTED_UNAME_R=$PROFILE_EXPECTED_UNAME_R" \
        "IOLBOX_PROVISION_DIR=$IOLBOX_PROVISION_DIR" \
        "IOLBOX_PAYLOAD_TARBALL=$IOLBOX_PAYLOAD_TARBALL" \
        "IOLBOX_BIND=$IOLBOX_BIND" \
        "IOLBOX_GUI_PORT=$IOLBOX_GUI_PORT" \
        bash "$step_path"; then
        if [ "$step_name" = "30-canary.sh" ]; then
            record_canary pass 0
        fi
        return 0
    else
        step_code=$?
        if [ "$step_name" = "30-canary.sh" ]; then
            record_canary fail "$step_code"
        fi
        printf '[iolbox-mac] ERROR: guest step %s exited with %s\n' "$step_name" "$step_code" >&2
        return "$step_code"
    fi
}

run_all_guest_steps() {
    local step_name step_code
    local -a steps=(
        "$PROFILE_MULTIARCH_STEP"
        "$PROFILE_KERNEL_HOLD_STEP"
        30-canary.sh
        40-install-payload.sh
        50-verify.sh
    )

for step_name in ${steps[@]+"${steps[@]}"}; do
        if [ -f "$GUEST_DIR/$step_name" ]; then
            if run_guest_step "$step_name"; then
                :
            else
                step_code=$?
                exit "$step_code"
            fi
        else
            die 1 "selected guest step is missing: $GUEST_DIR/$step_name"
        fi
    done
    sync_host_attestation
}

guest_value() {
    local value

    if value="$("$LIMACTL_BIN" shell "$MACHINE" "$@" 2>&1)"; then
        printf '%s' "$value"
    else
        printf 'unavailable (%s)' "$value"
    fi
}

print_canary_state() {
    local state_file="$IOLBOX_STATE_ROOT/lima-canary-$MACHINE.txt"

    if [ -r "$state_file" ]; then
        sed 's/^/  /' "$state_file"
    else
        printf '  unknown (no host-side canary result recorded)\n'
    fi
}

status_command() {
    local state kernel arch rosetta service http

    collect_host_facts
    select_qualification
    printf 'iolbox status\n'
    profile_summary
    if ! LIMACTL_BIN="$(locate_limactl)"; then
        die 3 "Lima was not found; status needs limactl to inspect machine $MACHINE"
    fi
    read_lima_version
    if ! machine_state; then
        die 3 "could not query Lima machine list with $LIMACTL_BIN: $MACHINE_LISTING"
    fi
    state="$MACHINE_STATE"
    printf '  host macOS: %s\n' "$HOST_MACOS"
    printf '  host arch: %s\n' "$HOST_ARCH"
    printf '  Lima: %s (%s)\n' "$HOST_LIMA" "$LIMACTL_BIN"
    printf '  machine: %s\n' "$MACHINE"
    printf '  machine state: %s\n' "${state:-not created}"
    printf '  last canary result:\n'
    print_canary_state

    if [ -z "$state" ]; then
        printf '  guest kernel: unavailable (machine not created)\n'
        printf '  guest arch: unavailable (machine not created)\n'
        printf '  Rosetta binfmt: unavailable (machine not created)\n'
        printf '  supervisor service: unavailable (machine not created)\n'
        printf '  GET /: unavailable (machine not created)\n'
        return 0
    fi
    if [ "$state" != "Running" ] && [ "$state" != "running" ]; then
        printf '  guest kernel: unavailable (machine is not running)\n'
        printf '  guest arch: unavailable (machine is not running)\n'
        printf '  Rosetta binfmt: unavailable (machine is not running)\n'
        printf '  supervisor service: unavailable (machine is not running)\n'
        printf '  GET /: unavailable (machine is not running)\n'
        return 0
    fi

    kernel="$(guest_value uname -r)"
    arch="$(guest_value uname -m)"
    rosetta="$(guest_value sudo -n cat /proc/sys/fs/binfmt_misc/rosetta)"
    service="$(guest_value sudo -n systemctl is-active iolbox-supervisor.service)"
    http="$(guest_value sudo -n curl --silent --show-error --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$IOLBOX_GUI_PORT/")"
    printf '  guest kernel: %s\n' "$kernel"
    printf '  guest arch: %s\n' "$arch"
    printf '  Rosetta binfmt: %s\n' "$rosetta"
    printf '  supervisor service: %s\n' "$service"
    printf '  GET /: %s\n' "$http"
}

print_hostagent_warnings() {
    local hostagent_log="${HOME:-.}/.lima/$MACHINE/ha.stderr.log"
    local matches

    printf 'Lima hostagent Rosetta warnings (%s):\n' "$hostagent_log"
    if [ -r "$hostagent_log" ]; then
        matches="$(grep -E 'Unable to configure Rosetta|unsupported build target macOS version' "$hostagent_log" || true)"
        if [ -n "$matches" ]; then
            printf '%s\n' "$matches" | sed 's/^/  /'
        else
            printf '%s\n' '  none found'
        fi
    else
        printf '%s\n' '  log unavailable'
    fi
    printf '%s\n' '  remediation: brew reinstall lima (brew upgrade is a no-op when the version is already current; reinstall re-pours the bottle)'
    printf '%s\n' '  Lima READY is not evidence that Rosetta was configured.'
}

doctor_command() {
    local available state kernel arch rosetta service http

    collect_host_facts
    select_qualification
    printf 'iolbox doctor\n'
    profile_summary
    printf 'host:\n'
    printf '  system: %s\n' "$HOST_OS"
    printf '  architecture: %s\n' "$HOST_ARCH"
    printf '  macOS product/build: %s\n' "$HOST_MACOS"
    available="$(free_disk_kb || true)"
    if [ -n "$available" ]; then
        printf '  free disk at %s: %s\n' "${HOME:-.}" "$(format_disk_kb "$available")"
    else
        printf '  free disk at %s: unknown\n' "${HOME:-.}"
    fi
    printf '  free disk threshold: %s GiB\n' "$MIN_FREE_DISK_GIB"

    printf 'lima:\n'
    LIMACTL_BIN="$(locate_limactl 2>/dev/null || true)"
    if [ -n "$LIMACTL_BIN" ]; then
        read_lima_version
        printf '  executable: %s\n' "$LIMACTL_BIN"
        printf '  version: %s\n' "$HOST_LIMA"
        if machine_state; then
            printf '  machine list:\n%s\n' "$MACHINE_LISTING"
        else
            printf '  machine list: unavailable (%s)\n' "$MACHINE_LISTING"
        fi
    else
        printf '  executable: not found (tried Homebrew paths and PATH)\n'
        printf '  version: unknown\n'
        printf '  machine list: unavailable\n'
    fi

    printf 'guest:\n'
    if [ -z "$LIMACTL_BIN" ]; then
        printf '  machine: %s\n' "$MACHINE"
        printf '  state/kernel/arch/Rosetta/service/GET /: unavailable\n'
    elif ! machine_state; then
        printf '  machine: %s\n' "$MACHINE"
        printf '  state/kernel/arch/Rosetta/service/GET /: unavailable (%s)\n' "$MACHINE_LISTING"
    else
        state="$MACHINE_STATE"
        printf '  machine: %s\n' "$MACHINE"
        printf '  state: %s\n' "${state:-not created}"
        if [ "$state" = "Running" ] || [ "$state" = "running" ]; then
            kernel="$(guest_value uname -r)"
            arch="$(guest_value uname -m)"
            rosetta="$(guest_value sudo -n cat /proc/sys/fs/binfmt_misc/rosetta)"
            service="$(guest_value sudo -n systemctl is-active iolbox-supervisor.service)"
            http="$(guest_value sudo -n curl --silent --show-error --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$IOLBOX_GUI_PORT/")"
            printf '  kernel: %s\n' "$kernel"
            printf '  arch: %s\n' "$arch"
            printf '  Rosetta binfmt: %s\n' "$rosetta"
            printf '  supervisor service: %s\n' "$service"
            printf '  GET /: %s\n' "$http"
        else
            printf '  kernel/arch/Rosetta/service/GET /: unavailable (machine is not running)\n'
        fi
    fi
    printf 'last canary:\n'
    print_canary_state
    print_hostagent_warnings
    return 0
}

destroy_command() {
    local state answer

    if ! LIMACTL_BIN="$(locate_limactl)"; then
        die 3 "Lima was not found; destroy needs limactl to remove machine $MACHINE"
    fi
    if ! machine_state; then
        die 3 "could not query Lima machine list with $LIMACTL_BIN: $MACHINE_LISTING"
    fi
    state="$MACHINE_STATE"
    if [ -z "$state" ]; then
        log "machine $MACHINE does not exist; nothing to destroy"
        return 0
    fi
    if [ "$YES" -ne 1 ]; then
        printf 'This stops and permanently deletes Lima machine %s and its guest lab data. Continue? [y/N] ' "$MACHINE" >&2
        if ! read -r answer; then
            die 1 "could not read confirmation; rerun with --yes"
        fi
        case "$answer" in
            y|Y|yes|YES) ;;
            *) log "destroy cancelled"; return 0 ;;
        esac
    fi
    if [ "$state" = "Running" ] || [ "$state" = "running" ]; then
        "$LIMACTL_BIN" stop "$MACHINE"
    fi
    "$LIMACTL_BIN" delete "$MACHINE"
}

print_dry_common() {
    local candidate available

    collect_host_facts
    select_qualification
    candidate="$(locate_limactl 2>/dev/null || true)"
    printf 'DRY-RUN: no Lima command, VM mutation, template temp file, or canary state write will occur.\n'
    profile_summary
    printf '  host system: %s\n' "$HOST_OS"
    printf '  host arch: %s\n' "$HOST_ARCH"
    printf '  macOS product/build: %s\n' "$HOST_MACOS"
    if [ -n "$candidate" ]; then
        printf '  limactl found (not invoked): %s\n' "$candidate"
    else
        printf '  limactl: unknown/not found in this environment (not invoked)\n'
    fi
    printf '  image URL: %s\n' "${IOLBOX_IMAGE_URL:-unknown}"
    printf '  image digest: %s\n' "${IOLBOX_IMAGE_DIGEST:-unknown}"
    if [ "${IOLBOX_IMAGE_DIGEST:-}" = PIN-ME ]; then
        printf '%s\n' '  real-run gate: REFUSED until this profile has a published SHA512 pin (see the exact SHA512SUMS URL in the real-run error).'
    fi
    available="$(free_disk_kb || true)"
    if [ -n "$available" ]; then
        printf '  free disk measured read-only at %s: %s (gate not enforced)\n' "${HOME:-.}" "$(format_disk_kb "$available")"
    else
        printf '  free disk: unknown (gate not enforced)\n'
    fi
}

print_dry_guest_command() {
    local step_name="$1"
    printf '  %s shell "%s" sudo -E env IOLBOX_HOST_MACOS="%s" IOLBOX_HOST_LIMA="%s" IOLBOX_MACHINE="%s" IOLBOX_PROFILE="%s" IOLBOX_PROFILE_STATUS="%s" IOLBOX_KERNEL_SERIES="%s" IOLBOX_EXPECTED_UNAME_R="%s" IOLBOX_PROVISION_DIR="%s" IOLBOX_PAYLOAD_TARBALL="%s" IOLBOX_BIND="%s" IOLBOX_GUI_PORT="%s" bash "%s/%s"\n' \
        "${LIMACTL_BIN:-limactl}" "$MACHINE" "$HOST_MACOS" "${HOST_LIMA:-unknown}" "$MACHINE" "$PROFILE_NAME" "$PROFILE_STATUS" "$PROFILE_KERNEL_RUNTIME_SERIES" "$PROFILE_EXPECTED_UNAME_R" "$GUEST_PROVISION_DIR" "$GUEST_PROVISION_DIR/payload/${PAYLOAD_BASENAME:-unknown.tar.gz}" "$IOLBOX_BIND" "$IOLBOX_GUI_PORT" "$GUEST_PROVISION_DIR" "$step_name"
}

dry_run_provision() {
    local step_name
    local dry_yaml="$IOLBOX_TMP_ROOT/iolbox-$PROFILE_NAME.rendered.yaml"
    local -a steps=(
        "$PROFILE_MULTIARCH_STEP"
        "$PROFILE_KERNEL_HOLD_STEP"
        30-canary.sh
        40-install-payload.sh
        50-verify.sh
    )

    print_dry_common
    printf 'provision commands:\n'
    printf '  source "%s"\n' "$ENV_FILE"
    if discover_payload; then
        printf '  payload selected: %s\n' "$PAYLOAD_PATH"
    else
        PAYLOAD_BASENAME='iolbox-server-<newest>.tar.gz'
        printf '  payload lookup: would fail unless IOLBOX_TARBALL or a matching tarball appears next to %s or in %s\n' "$SCRIPT_DIR" "$(pwd -P)"
    fi
    printf '  sed -e "s|@IOLBOX_IMAGE_URL@|<pinned URL>|g" -e "s|@IOLBOX_IMAGE_DIGEST@|<algorithm-qualified digest>|g" -e "s|@CPUS@|%s|g" -e "s|@MEMORY@|%s|g" -e "s|@DISK@|%s|g" "%s" > "%s"\n' "$IOLBOX_CPUS" "$IOLBOX_MEMORY" "$IOLBOX_DISK" "$TEMPLATE_FILE" "$dry_yaml"
    printf '  "%s" list --format "{{.Name}}|{{.Status}}"\n' "${LIMACTL_BIN:-limactl}"
    printf '  if machine "%s" is absent: delete stale host attestation "%s" before create\n' "$MACHINE" "$HOST_ATTESTATION_FILE"
    printf '  if machine "%s" is absent: "%s" create --name="%s" "%s"\n' "$MACHINE" "${LIMACTL_BIN:-limactl}" "$MACHINE" "$dry_yaml"
    printf '  if existing machine "%s" is Stopped: require valid host attestation before "%s" start\n' "$MACHINE" "${LIMACTL_BIN:-limactl}"
    printf '  if fresh machine "%s": "%s" start "%s" --tty=false\n' "$MACHINE" "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" sudo -n rm -rf /tmp/iolbox-provision-stage "%s"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE" "$GUEST_PROVISION_DIR"
    printf '  "%s" copy "%s" "%s:/tmp/iolbox-provision-stage"\n' "${LIMACTL_BIN:-limactl}" "$GUEST_DIR" "$MACHINE"
    printf '  "%s" copy "<payload>" "%s:/tmp/iolbox-payload.tar.gz"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" sudo -n mkdir -p "%s/payload"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE" "$GUEST_PROVISION_DIR"
    printf '  "%s" shell "%s" sudo -n mv /tmp/iolbox-provision-stage "%s"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE" "$GUEST_PROVISION_DIR"
    printf '  "%s" shell "%s" sudo -n mv /tmp/iolbox-payload.tar.gz "%s/payload/<payload>"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE" "$GUEST_PROVISION_DIR"
for step_name in ${steps[@]+"${steps[@]}"}; do
        print_dry_guest_command "$step_name"
    done
    printf '  after gated service success: copy guest /var/lib/iolbox/macos-structural-gate.json to "%s"\n' "$HOST_ATTESTATION_FILE"
}

dry_run_canary() {
    print_dry_common
    printf 'canary commands:\n'
    printf '  "%s" list --format "{{.Name}}|{{.Status}}"\n' "${LIMACTL_BIN:-limactl}"
    print_dry_guest_command 30-canary.sh
    printf '  (would record pass/fail locally after the guest exit code; no state write in dry-run)\n'
}

dry_run_status() {
    print_dry_common
    printf 'status commands:\n'
    printf '  "%s" --version\n' "${LIMACTL_BIN:-limactl}"
    printf '  "%s" list --format "{{.Name}}|{{.Status}}"\n' "${LIMACTL_BIN:-limactl}"
    printf '  "%s" shell "%s" uname -r\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" uname -m\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" sudo -n cat /proc/sys/fs/binfmt_misc/rosetta\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" sudo -n systemctl is-active iolbox-supervisor.service\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" shell "%s" sudo -n curl --silent --show-error --output /dev/null --write-out "%%{http_code}" http://127.0.0.1:%s/\n' "${LIMACTL_BIN:-limactl}" "$MACHINE" "$IOLBOX_GUI_PORT"
    printf '  read "%s/lima-canary-%s.txt"\n' "$IOLBOX_STATE_ROOT" "$MACHINE"
}

dry_run_destroy() {
    print_dry_common
    printf 'destroy commands:\n'
    printf '  "%s" list --format "{{.Name}}|{{.Status}}"\n' "${LIMACTL_BIN:-limactl}"
    printf '  prompt: This stops and permanently deletes Lima machine %s and its guest lab data. Continue? [y/N]\n' "$MACHINE"
    printf '  if Running: "%s" stop "%s"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
    printf '  "%s" delete "%s"\n' "${LIMACTL_BIN:-limactl}" "$MACHINE"
}

dry_run_doctor() {
    print_dry_common
    printf 'doctor commands:\n'
    printf '  sw_vers -productVersion\n'
    printf '  sw_vers -buildVersion\n'
    printf '  uname -m\n'
    printf '  df -Pk "%s"\n' "${HOME:-.}"
    printf '  "%s" --version\n' "${LIMACTL_BIN:-limactl}"
    printf '  "%s" list --format "{{.Name}}|{{.Status}}"\n' "${LIMACTL_BIN:-limactl}"
    printf '  guest facts (when %s is Running): uname -r, uname -m, binfmt, systemctl is-active, GET /\n' "$MACHINE"
    printf '  read "%s/lima-canary-%s.txt"\n' "$IOLBOX_STATE_ROOT" "$MACHINE"
    printf '  grep -E "Unable to configure Rosetta|unsupported build target macOS version" "%s/.lima/%s/ha.stderr.log"\n' "${HOME:-.}" "$MACHINE"
    printf '%s\n' '  remediation if matched: brew reinstall lima (brew upgrade is a no-op when already current; reinstall re-pours the bottle)'
}

dry_run_command() {
    case "$COMMAND" in
        provision) dry_run_provision ;;
        canary) dry_run_canary ;;
        status) dry_run_status ;;
        destroy) dry_run_destroy ;;
        doctor) dry_run_doctor ;;
        *) die 1 "unknown command: $COMMAND" ;;
    esac
}

while [ $# -gt 0 ]; do
    case "$1" in
        provision|canary|status|destroy|doctor|profiles)
            if [ "$COMMAND_SET" -eq 1 ]; then
                die 1 "only one subcommand may be specified"
            fi
            COMMAND="$1"
            COMMAND_SET=1
            shift
            ;;
        --profile)
            if [ "$#" -lt 2 ]; then
                die 1 '--profile requires a profile name'
            fi
            PROFILE_REQUESTED="$2"
            shift 2
            ;;
        --dry-run)
            DRY_RUN=1
            shift
            ;;
        --yes)
            YES=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf 'unknown option or command: %s\n' "$1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

load_profile_table
select_profile

if [ "$COMMAND" = profiles ]; then
    collect_host_facts
    print_profiles_command
    exit 0
fi

if ! load_config; then
    if [ "$COMMAND" = "doctor" ]; then
        MACHINE="${IOLBOX_MACHINE:-iolbox-$PROFILE_NAME}"
        warn "pinned configuration could not be loaded; doctor will report remaining facts"
    else
        die 1 "cannot load pinned Lima configuration"
    fi
fi

if [ "$DRY_RUN" -eq 1 ]; then
    dry_run_command
    exit 0
fi

case "$COMMAND" in
    provision)
        preflight
        ensure_machine
        stage_files
        run_all_guest_steps
        ;;
    canary)
        collect_host_facts
        select_qualification
        if ! LIMACTL_BIN="$(locate_limactl)"; then
            die 3 "Lima was not found; canary needs limactl to inspect machine $MACHINE"
        fi
        read_lima_version
        if ! machine_state; then
            die 3 "could not query Lima machine list with $LIMACTL_BIN: $MACHINE_LISTING"
        fi
        case "$MACHINE_STATE" in
            Running|running) ;;
            '') die 3 "machine $MACHINE does not exist; run provision first" ;;
            *) die 3 "machine $MACHINE is not running; start it before running canary" ;;
        esac
        export IOLBOX_HOST_MACOS="$HOST_MACOS"
        export IOLBOX_HOST_MACOS_PRODUCT="$HOST_MACOS_PRODUCT"
        export IOLBOX_HOST_MACOS_BUILD="$HOST_MACOS_BUILD"
        export IOLBOX_HOST_LIMA="$HOST_LIMA"
        export IOLBOX_MACHINE="$MACHINE"
        export IOLBOX_PROFILE="$PROFILE_NAME"
        export IOLBOX_PROFILE_STATUS="$PROFILE_STATUS"
        export IOLBOX_KERNEL_SERIES="$PROFILE_KERNEL_RUNTIME_SERIES"
        export IOLBOX_EXPECTED_UNAME_R="$PROFILE_EXPECTED_UNAME_R"
        export IOLBOX_PROVISION_DIR="$GUEST_PROVISION_DIR"
        export IOLBOX_BIND IOLBOX_GUI_PORT
        run_guest_step 30-canary.sh
        ;;
    status)
        status_command
        ;;
    destroy)
        destroy_command
        ;;
    doctor)
        doctor_command
        ;;
    profiles)
        print_profiles_command
        ;;
    *)
        die 1 "unknown command: $COMMAND"
        ;;
esac
