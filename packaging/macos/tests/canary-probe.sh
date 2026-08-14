#!/usr/bin/env bash
# canary-probe.sh - measure the Rosetta canary for any table-defined profile.
#
# This mirrors negative-kernel68.sh's safety structure but chooses the pinned
# guest image, multiarch step, and expected verdict from lima/profiles.env.  It
# creates only a minimal throwaway VZ + Rosetta machine, stages lib.sh, the
# selected multiarch step, and the shared 30-canary.sh, then removes the exact
# machine it created unless --keep is supplied.  No payload or iolbox install
# is staged.  A BLOCKED profile is intentionally probeable: a measured PASS
# would be an unexpected result worth surfacing, not a reason to skip the test.
#
# Debian 12 remains CANDIDATE and UNVERIFIED on hardware: its 6.1 safety is
# inferred from the 6.3 threshold and has never been executed on Apple Silicon
# Rosetta.  Debian 13 is BLOCKED only by today's measured baseline expectation;
# the probe is the prescribed way to revisit that answer.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
lima_dir="$repo_root/packaging/macos/lima"
guest_dir="$repo_root/packaging/macos/guest"
profiles_file="$lima_dir/profiles.env"

limactl_bin="${IOLBOX_LIMACTL:-limactl}"
profile_requested=''
keep=0
dry_run=0
machine_created=0
machine_create_attempted=0
cleanup_done=0
machine_name=''
expected_machine_name=''
rendered_yaml=''
host_macos='unknown'
host_lima='unknown'

min_free_kib=$((3 * 1024 * 1024))

usage() {
    cat <<'USAGE'
Usage: canary-probe.sh --profile <name> [--keep] [--dry-run]

Measure the shared Rosetta canary in a minimal throwaway guest selected from
the profile table.  The observed verdict is compared with the profile's
macOS 13.5 baseline expectation.

  --profile <name>  required table-defined profile (jammy, debian12, ...)
  --keep             retain the throwaway guest for investigation
  --dry-run          print exact commands without using Lima
  -h, --help         show this help
USAGE
}

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

profile_value() {
    local variable="$1" value

    # The variable name is read from the repository-owned profile table.  The
    # profile table parser restricts it to the two expected pin variable forms
    # before this function is called, so indirect expansion is safe here and
    # remains compatible with macOS Bash 3.2.
    # shellcheck disable=SC2294
    eval "value=\${$variable-}"
    printf '%s\n' "$value"
}

load_profile() {
    local record

    [ -r "$profiles_file" ] || fail "profile table is missing: $profiles_file"
    # shellcheck disable=SC1090
    . "$profiles_file"
    [ -n "${IOLBOX_PROFILE_TABLE:-}" ] || fail "profile table is empty: $profiles_file"
    record="$(printf '%s\n' "$IOLBOX_PROFILE_TABLE" | awk -F '|' -v wanted="$profile_requested" '$1 == wanted { print; exit }')"
    [ -n "$record" ] || fail "unknown profile '$profile_requested'; run iolbox-mac.sh profiles"
    IFS='|' read -r PROFILE_NAME PROFILE_GUEST PROFILE_PIN_ENV PROFILE_TEMPLATE \
        PROFILE_MULTIARCH_STEP PROFILE_KERNEL_HOLD_STEP PROFILE_KERNEL_SERIES \
        PROFILE_STATUS PROFILE_CANARY_EXPECTED PROFILE_CANARY_BASIS \
        PROFILE_IMAGE_URL_VAR PROFILE_IMAGE_SHA_VAR PROFILE_IMAGE_URL_TOKEN \
        PROFILE_IMAGE_SHA_TOKEN <<EOF
$record
EOF
    case "$PROFILE_IMAGE_URL_VAR:$PROFILE_IMAGE_SHA_VAR" in
        IOLBOX_*_IMAGE_URL:IOLBOX_*_IMAGE_SHA256) ;;
        *) fail "profile $PROFILE_NAME has invalid pin variable names" ;;
    esac
    # shellcheck disable=SC1090
    . "$lima_dir/$PROFILE_PIN_ENV"
    PROFILE_IMAGE_URL="$(profile_value "$PROFILE_IMAGE_URL_VAR")"
    PROFILE_IMAGE_SHA256="$(profile_value "$PROFILE_IMAGE_SHA_VAR")"
    [ -n "$PROFILE_IMAGE_URL" ] || fail "profile $PROFILE_NAME has no image URL in $PROFILE_PIN_ENV"
    [ -n "$PROFILE_IMAGE_SHA256" ] || fail "profile $PROFILE_NAME has no image SHA256 in $PROFILE_PIN_ENV"
    PROFILE_KERNEL_RUNTIME_SERIES="$(printf '%s\n' "$PROFILE_KERNEL_SERIES" | cut -d. -f1,2)"
    machine_name="iolbox-probe-$PROFILE_NAME"
    expected_machine_name="$machine_name"
}

collect_host_facts() {
    local product build raw

    if command -v sw_vers >/dev/null 2>&1; then
        product="$(sw_vers -productVersion 2>/dev/null || true)"
        build="$(sw_vers -buildVersion 2>/dev/null || true)"
        if [ -n "$product" ] && [ -n "$build" ]; then
            host_macos="$product ($build)"
        elif [ -n "$product" ]; then
            host_macos="$product (build unknown)"
        fi
    fi
    raw="$($limactl_bin --version 2>/dev/null || true)"
    host_lima="$(printf '%s\n' "$raw" | sed -nE 's/.*([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' | head -n 1)"
    [ -n "$host_lima" ] || host_lima='unknown'
}

machine_exists() {
    "$limactl_bin" list --format '{{.Name}}' 2>/dev/null | grep -Fxq "$machine_name"
}

free_disk_kib() {
    local available

    available="$(df -Pk "${HOME:-.}" 2>/dev/null | awk 'NR == 2 { print $4 }' || true)"
    case "$available" in
        ''|*[!0-9]*) return 1 ;;
    esac
    printf '%s\n' "$available"
}

format_disk() {
    awk -v kb="$1" 'BEGIN {printf "%.2f GiB", kb / 1048576}'
}

require_pinned_probe_image() {
    if [ "$PROFILE_IMAGE_SHA256" = 'PIN-ME-SHA256' ]; then
        fail "profile $PROFILE_NAME is unpinned; fetch the checksum for $PROFILE_IMAGE_URL from the matching SHA256SUMS file before probing"
    fi
    case "$PROFILE_IMAGE_SHA256" in
        ''|*[!0123456789abcdefABCDEF]*)
            fail "profile $PROFILE_NAME has an invalid image SHA256; record the published checksum before probing"
            ;;
    esac
    if [ "${#PROFILE_IMAGE_SHA256}" -ne 64 ]; then
        fail "profile $PROFILE_NAME has an invalid image SHA256; record the published checksum before probing"
    fi
}

render_probe_template() {
    local unresolved

    rendered_yaml="$(mktemp "${TMPDIR:-/tmp}/iolbox-probe-${PROFILE_NAME}.XXXXXX.yaml")" || \
        fail 'could not create temporary Lima template'
    sed \
        -e "s|$PROFILE_IMAGE_URL_TOKEN|$PROFILE_IMAGE_URL|g" \
        -e "s|$PROFILE_IMAGE_SHA_TOKEN|$PROFILE_IMAGE_SHA256|g" \
        -e 's|@CPUS@|1|g' \
        -e 's|@MEMORY@|1GiB|g' \
        -e 's|@DISK@|8GiB|g' \
        "$lima_dir/$PROFILE_TEMPLATE" > "$rendered_yaml"
    unresolved="$(grep -E '@(IOLBOX_[A-Z0-9_]+|CPUS|MEMORY|DISK)@' "$rendered_yaml" || true)"
    [ -z "$unresolved" ] || fail "rendered probe template still contains placeholders: $rendered_yaml"
}

cleanup() {
    local status=$?

    if [ "$cleanup_done" -eq 1 ]; then
        exit "$status"
    fi
    cleanup_done=1
    if [ -n "$rendered_yaml" ] && [ -f "$rendered_yaml" ]; then
        rm -f -- "$rendered_yaml"
    fi

    # If an interrupt arrived during create, discover whether the exact name
    # now exists.  Never delete a machine that this invocation did not own.
    if [ "$machine_created" -eq 0 ] && [ "$machine_create_attempted" -eq 1 ] && \
        [ "$machine_name" = "$expected_machine_name" ] && machine_exists; then
        machine_created=1
    fi
    if [ "$machine_created" -eq 1 ] && [ "$keep" -eq 0 ]; then
        if [ "$machine_name" = "$expected_machine_name" ]; then
            printf 'Deleting throwaway Lima machine: %s\n' "$machine_name" >&2
            "$limactl_bin" delete --force "$machine_name" || \
                printf 'WARNING: could not delete throwaway Lima machine: %s\n' "$machine_name" >&2
        else
            printf 'WARNING: refusing to delete unexpected machine name: %s\n' "$machine_name" >&2
        fi
    elif [ "$machine_created" -eq 1 ]; then
        printf 'Keeping throwaway Lima machine: %s\n' "$machine_name" >&2
    fi
    exit "$status"
}

print_profile_context() {
    printf 'profile: %s\n' "$PROFILE_NAME"
    printf '  guest: %s\n' "$PROFILE_GUEST"
    printf '  kernel series: %s\n' "$PROFILE_KERNEL_SERIES"
    printf '  status: %s\n' "$PROFILE_STATUS"
    printf '  expected on macOS 13.5 baseline: %s (%s)\n' \
        "$PROFILE_CANARY_EXPECTED" "$PROFILE_CANARY_BASIS"
    if [ "$PROFILE_NAME" = debian12 ]; then
        printf '%s\n' '  UNVERIFIED: Debian 12 kernel 6.1 is inferred safe from the 6.3 threshold and has never been executed on Apple Silicon Rosetta.'
    fi
}

print_dry_run() {
    local available rendered_path

    printf '%s\n' 'DRY-RUN: no Lima commands will be executed and no throwaway machine will be created.'
    print_profile_context
    printf '  image URL: %s\n' "$PROFILE_IMAGE_URL"
    printf '  image SHA256: %s\n' "$PROFILE_IMAGE_SHA256"
    if [ "$PROFILE_IMAGE_SHA256" = 'PIN-ME-SHA256' ]; then
        printf '%s\n' '  real-run gate: REFUSED until the exact Debian image URL is checked against its published SHA256SUMS.'
    fi
    available="$(free_disk_kib || true)"
    if [ -n "$available" ]; then
        printf '  preflight free disk: %d KiB (%s); requirement: %d KiB (3 GiB)\n' \
            "$available" "$(format_disk "$available")" "$min_free_kib"
    else
        printf '%s\n' '  preflight free disk: unavailable (df -Pk could not report a number)'
    fi
    rendered_path="${TMPDIR:-/tmp}/iolbox-probe-${PROFILE_NAME}.rendered.yaml"
    printf '  sed -e "s|%s|<profile image URL>|g" -e "s|%s|<profile image SHA256>|g" -e "s|@CPUS@|1|g" -e "s|@MEMORY@|1GiB|g" -e "s|@DISK@|8GiB|g" "%s" > "%s"\n' \
        "$PROFILE_IMAGE_URL_TOKEN" "$PROFILE_IMAGE_SHA_TOKEN" \
        "$lima_dir/$PROFILE_TEMPLATE" "$rendered_path"
    printf "  %s list --format '{{.Name}}'\n" "$limactl_bin"
    printf '  %s create --name="%s" "%s"\n' "$limactl_bin" "$machine_name" "$rendered_path"
    printf '  %s start "%s" --tty=false\n' "$limactl_bin" "$machine_name"
    printf '  %s copy "%s/lib.sh" "%s:/tmp/iolbox-lib.sh"\n' "$limactl_bin" "$guest_dir" "$machine_name"
    printf '  %s copy "%s/%s" "%s:/tmp/iolbox-multiarch.sh"\n' \
        "$limactl_bin" "$guest_dir" "$PROFILE_MULTIARCH_STEP" "$machine_name"
    printf '  %s copy "%s/30-canary.sh" "%s:/tmp/iolbox-canary.sh"\n' \
        "$limactl_bin" "$guest_dir" "$machine_name"
    printf '  %s shell "%s" sudo mv /tmp/iolbox-lib.sh /opt/iolbox-probe/lib.sh\n' "$limactl_bin" "$machine_name"
    printf '  %s shell "%s" sudo mv /tmp/iolbox-multiarch.sh /opt/iolbox-probe/%s\n' \
        "$limactl_bin" "$machine_name" "$PROFILE_MULTIARCH_STEP"
    printf '  %s shell "%s" sudo mv /tmp/iolbox-canary.sh /opt/iolbox-probe/30-canary.sh\n' "$limactl_bin" "$machine_name"
    printf '  %s shell "%s" sudo -E env IOLBOX_PROFILE=%s IOLBOX_KERNEL_SERIES=%s bash /opt/iolbox-probe/%s\n' \
        "$limactl_bin" "$machine_name" "$PROFILE_NAME" "$PROFILE_KERNEL_RUNTIME_SERIES" "$PROFILE_MULTIARCH_STEP"
    printf '  %s shell "%s" sudo -E env IOLBOX_PROFILE=%s IOLBOX_KERNEL_SERIES=%s bash /opt/iolbox-probe/30-canary.sh\n' \
        "$limactl_bin" "$machine_name" "$PROFILE_NAME" "$PROFILE_KERNEL_RUNTIME_SERIES"
    printf '  verdict: compare observed PASS/FAIL_AUXV with expected %s; unexpected result exits non-zero\n' "$PROFILE_CANARY_EXPECTED"
    printf '  %s delete --force "%s"  # cleanup unless --keep\n' "$limactl_bin" "$machine_name"
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --profile)
            [ "$#" -gt 1 ] || { usage >&2; exit 1; }
            profile_requested="$2"
            shift 2
            ;;
        --keep)
            keep=1
            shift
            ;;
        --dry-run)
            dry_run=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            usage >&2
            exit 1
            ;;
    esac
done

[ -n "$profile_requested" ] || { usage >&2; exit 1; }
load_profile

if [ "$dry_run" -eq 1 ]; then
    print_dry_run
    exit 0
fi

trap cleanup EXIT
trap 'exit 130' INT TERM

command -v "$limactl_bin" >/dev/null 2>&1 || fail "limactl not found: $limactl_bin"
case "$(uname -s 2>/dev/null || true):$(uname -m 2>/dev/null || true)" in
    Darwin:arm64) ;;
    *) fail "canary probe must run on an Apple Silicon Mac; detected $(uname -s 2>/dev/null || printf unknown)/$(uname -m 2>/dev/null || printf unknown)" ;;
esac

collect_host_facts
free_kib="$(free_disk_kib || true)"
case "$free_kib" in
    ''|*[!0-9]*) fail 'could not determine free disk space from df -Pk $HOME' ;;
esac
printf 'Preflight free disk: %d KiB (%s); requirement: %d KiB (3 GiB).\n' \
    "$free_kib" "$(format_disk "$free_kib")" "$min_free_kib"
[ "$free_kib" -ge "$min_free_kib" ] || \
    fail "insufficient free disk: ${free_kib} KiB available, ${min_free_kib} KiB required"
require_pinned_probe_image

[ -f "$guest_dir/lib.sh" ] || fail "missing shared guest helper: $guest_dir/lib.sh"
[ -f "$guest_dir/$PROFILE_MULTIARCH_STEP" ] || \
    fail "profile $PROFILE_NAME multiarch step is missing: $guest_dir/$PROFILE_MULTIARCH_STEP"
[ -f "$guest_dir/30-canary.sh" ] || fail "missing shared canary step: $guest_dir/30-canary.sh"
[ -f "$lima_dir/$PROFILE_TEMPLATE" ] || \
    fail "profile $PROFILE_NAME Lima template is missing: $lima_dir/$PROFILE_TEMPLATE"

if machine_exists; then
    fail "refusing to use or delete existing Lima machine: $machine_name"
fi

printf 'Creating throwaway Lima machine: %s\n' "$machine_name"
render_probe_template
machine_create_attempted=1
if "$limactl_bin" create --name="$machine_name" "$rendered_yaml" && \
    "$limactl_bin" start "$machine_name" --tty=false; then
    machine_created=1
else
    if machine_exists; then
        machine_created=1
    fi
    fail "could not create/start $machine_name"
fi

probe_guest_dir='/opt/iolbox-probe'
"$limactl_bin" shell "$machine_name" sudo mkdir -p "$probe_guest_dir"
"$limactl_bin" copy "$guest_dir/lib.sh" "$machine_name:/tmp/iolbox-lib.sh"
"$limactl_bin" copy "$guest_dir/$PROFILE_MULTIARCH_STEP" "$machine_name:/tmp/iolbox-multiarch.sh"
"$limactl_bin" copy "$guest_dir/30-canary.sh" "$machine_name:/tmp/iolbox-canary.sh"
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-lib.sh "$probe_guest_dir/lib.sh"
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-multiarch.sh "$probe_guest_dir/$PROFILE_MULTIARCH_STEP"
"$limactl_bin" shell "$machine_name" sudo mv /tmp/iolbox-canary.sh "$probe_guest_dir/30-canary.sh"
"$limactl_bin" shell "$machine_name" sudo chmod 0755 \
    "$probe_guest_dir/$PROFILE_MULTIARCH_STEP" "$probe_guest_dir/30-canary.sh"

printf 'Running selected shared multiarch step: %s\n' "$PROFILE_MULTIARCH_STEP"
"$limactl_bin" shell "$machine_name" sudo -E env \
    IOLBOX_PROFILE="$PROFILE_NAME" \
    IOLBOX_KERNEL_SERIES="$PROFILE_KERNEL_RUNTIME_SERIES" \
    IOLBOX_PROVISION_DIR="$probe_guest_dir" \
    IOLBOX_MACHINE="$machine_name" \
    IOLBOX_HOST_MACOS="$host_macos" \
    IOLBOX_HOST_LIMA="$host_lima" \
    bash "$probe_guest_dir/$PROFILE_MULTIARCH_STEP"

printf 'Running shared Rosetta canary: guest/30-canary.sh\n'
canary_output=''
canary_status=0
if canary_output="$("$limactl_bin" shell "$machine_name" sudo -E env \
    IOLBOX_PROFILE="$PROFILE_NAME" \
    IOLBOX_KERNEL_SERIES="$PROFILE_KERNEL_RUNTIME_SERIES" \
    IOLBOX_PROVISION_DIR="$probe_guest_dir" \
    IOLBOX_MACHINE="$machine_name" \
    IOLBOX_HOST_MACOS="$host_macos" \
    IOLBOX_HOST_LIMA="$host_lima" \
    bash "$probe_guest_dir/30-canary.sh" 2>&1)"; then
    canary_status=0
else
    canary_status=$?
fi
printf '%s\n' "$canary_output"

observed_verdict=''
if [ "$canary_status" -eq 0 ]; then
    observed_verdict='PASS'
else
    observed_verdict="$(printf '%s\n' "$canary_output" | sed -n 's/^Verdict: //p' | head -n 1)"
    [ -n "$observed_verdict" ] || observed_verdict='FAIL_OTHER'
fi

printf '\nCanary verdict\n'
printf '  profile: %s\n' "$PROFILE_NAME"
printf '  expected on macOS 13.5 baseline: %s (%s)\n' \
    "$PROFILE_CANARY_EXPECTED" "$PROFILE_CANARY_BASIS"
printf '  observed: %s (guest exit %d)\n' "$observed_verdict" "$canary_status"
if [ "$observed_verdict" = "$PROFILE_CANARY_EXPECTED" ]; then
    printf '%s\n' '  expectation match: YES'
else
    printf '%s\n' '  expectation match: NO (UNEXPECTED RESULT; this changes the product support decision)'
    exit 1
fi

exit 0
