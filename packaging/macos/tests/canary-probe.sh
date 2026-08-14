#!/usr/bin/env bash
# canary-probe.sh - measure the Rosetta canary for any table-defined profile.
#
# This chooses the pinned guest image and multiarch step from lima/profiles.env.
# The observed verdict is compared only with an exact profile/product/build row.
# creates only a minimal throwaway VZ + Rosetta machine, stages lib.sh, the
# selected multiarch step, and the shared 30-canary.sh, then removes the exact
# machine it created unless --keep is supplied.  No payload or iolbox install
# is staged. Static profile roles never gate this probe; the live canary does.
#
# An absent qualification row is an UNRECORDED HOST. It is not a refusal and a
# PASS is a valid new measurement; this script never edits profiles.env.
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
host_macos_product='unknown'
host_macos_build='unknown'
host_lima='unknown'
qualification_found=0
qualification_verdict=''
qualification_status=''
qualification_evidence=''

min_free_kib=$((3 * 1024 * 1024))

usage() {
    cat <<'USAGE'
Usage: canary-probe.sh --profile <name> [--keep] [--dry-run]

Measure the shared Rosetta canary in a minimal throwaway guest selected from
the profile table. The observed verdict is compared only with an exact
profile/product/build qualification row.

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

load_profile() {
    local record

    [ -r "$profiles_file" ] || fail "profile table is missing: $profiles_file"
    # shellcheck disable=SC1090
    if ! . "$profiles_file"; then
        fail "could not source profile table: $profiles_file"
    fi
    [ -n "${IOLBOX_PROFILE_TABLE:-}" ] || fail "profile table is empty: $profiles_file"
    [ -n "${IOLBOX_QUALIFICATION_TABLE:-}" ] || fail "qualification table is empty: $profiles_file"
    record="$(printf '%s\n' "$IOLBOX_PROFILE_TABLE" | awk -F '|' -v wanted="$profile_requested" '$1 == wanted { print; exit }')"
    [ -n "$record" ] || fail "unknown profile '$profile_requested'; run iolbox-mac.sh profiles"
    IFS='|' read -r PROFILE_NAME PROFILE_ROLE PROFILE_GUEST PROFILE_PIN_ENV PROFILE_TEMPLATE \
        PROFILE_MULTIARCH_STEP PROFILE_KERNEL_HOLD_STEP PROFILE_KERNEL_SERIES \
        PROFILE_EXPECTED_UNAME_R <<EOF
$record
EOF
    [ -r "$lima_dir/$PROFILE_PIN_ENV" ] || \
        fail "profile pin file is missing: $lima_dir/$PROFILE_PIN_ENV"
    # shellcheck disable=SC1090
    if ! . "$lima_dir/$PROFILE_PIN_ENV"; then
        fail "could not source profile pin file: $lima_dir/$PROFILE_PIN_ENV"
    fi
    PROFILE_IMAGE_URL="$IOLBOX_IMAGE_URL"
    PROFILE_IMAGE_DIGEST="$IOLBOX_IMAGE_DIGEST"
    [ -n "$PROFILE_IMAGE_URL" ] || fail "profile $PROFILE_NAME has no image URL in $PROFILE_PIN_ENV"
    [ -n "$PROFILE_IMAGE_DIGEST" ] || fail "profile $PROFILE_NAME has no image digest in $PROFILE_PIN_ENV"
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
            host_macos_product="$product"
            host_macos_build="$build"
            host_macos="$product ($build)"
        elif [ -n "$product" ]; then
            host_macos_product="$product"
            host_macos="$product (build unknown)"
        fi
    fi
    raw="$($limactl_bin --version 2>/dev/null || true)"
    host_lima="$(printf '%s\n' "$raw" | sed -nE 's/[^0-9]*([0-9]+\.[0-9]+\.[0-9]+).*/\1/p' | head -n 1)"
    [ -n "$host_lima" ] || host_lima='unknown'
}

lookup_qualification() {
    local record

    qualification_found=0
    qualification_verdict=''
    qualification_status=''
    qualification_evidence=''
    if [ "$host_macos_product" = unknown ] || [ "$host_macos_build" = unknown ]; then
        return 0
    fi
    record="$(printf '%s\n' "$IOLBOX_QUALIFICATION_TABLE" | awk -F '|' \
        -v profile="$PROFILE_NAME" -v product="$host_macos_product" \
        -v build="$host_macos_build" \
        '$1 == profile && $2 == product && $3 == build { print; exit }')"
    if [ -n "$record" ]; then
        IFS='|' read -r qualified_profile qualified_product qualified_build \
            qualification_verdict qualification_status qualification_evidence <<EOF
$record
EOF
        qualification_found=1
    fi
}

machine_exists() {
    local state

    if ! state="$(machine_state)"; then
        return 1
    fi
    [ -n "$state" ]
}

machine_state() {
    local listing

    if ! listing="$("$limactl_bin" list --format '{{.Name}}|{{.Status}}' 2>/dev/null)"; then
        return 1
    fi
    printf '%s\n' "$listing" | awk -F '|' -v target="$machine_name" '$1 == target {print $2; exit}'
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
    local digest_hex expected_length

    if [ "$PROFILE_IMAGE_DIGEST" = 'PIN-ME' ]; then
        fail "profile $PROFILE_NAME is unpinned; Debian publishes SHA512SUMS only. Fetch the checksum for $PROFILE_IMAGE_URL from ${PROFILE_IMAGE_URL%/*}/SHA512SUMS before probing"
    fi
    case "$PROFILE_IMAGE_DIGEST" in
        sha256:*) expected_length=64 ;;
        sha512:*) expected_length=128 ;;
        *)
            fail "profile $PROFILE_NAME has an invalid algorithm-qualified image digest; record the published digest before probing"
            ;;
    esac
    digest_hex="${PROFILE_IMAGE_DIGEST#*:}"
    [ "${#digest_hex}" -eq "$expected_length" ] || \
        fail "profile $PROFILE_NAME has an algorithm-qualified digest of the wrong length; record the published digest before probing"
    case "$digest_hex" in
        ''|*[!0123456789abcdefABCDEF]*)
            fail "profile $PROFILE_NAME has a non-hexadecimal image digest; record the published digest before probing"
            ;;
    esac
}

render_probe_template() {
    local unresolved

    rendered_yaml="$(mktemp "${TMPDIR:-/tmp}/iolbox-probe-${PROFILE_NAME}.XXXXXX.yaml")" || \
        fail 'could not create temporary Lima template'
    sed \
        -e "s|@IOLBOX_IMAGE_URL@|$PROFILE_IMAGE_URL|g" \
        -e "s|@IOLBOX_IMAGE_DIGEST@|$PROFILE_IMAGE_DIGEST|g" \
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
    printf '  role: %s\n' "$PROFILE_ROLE"
    printf '  guest: %s\n' "$PROFILE_GUEST"
    printf '  kernel series: %s\n' "$PROFILE_KERNEL_SERIES"
    if [ "$qualification_found" -eq 1 ]; then
        printf '  qualification: %s (%s)\n' "$qualification_verdict" "$qualification_status"
        printf '  qualification evidence: %s\n' "$qualification_evidence"
    else
        printf '%s\n' '  qualification: UNRECORDED HOST'
    fi
}

print_dry_run() {
    local available rendered_path

    printf '%s\n' 'DRY-RUN: no Lima commands will be executed and no throwaway machine will be created.'
    print_profile_context
    printf '  image URL: %s\n' "$PROFILE_IMAGE_URL"
    printf '  image digest: %s\n' "$PROFILE_IMAGE_DIGEST"
    if [ "$PROFILE_IMAGE_DIGEST" = 'PIN-ME' ]; then
        printf '%s\n' '  real-run gate: REFUSED until the exact Debian image URL is checked against its published SHA512SUMS.'
    fi
    available="$(free_disk_kib || true)"
    if [ -n "$available" ]; then
        printf '  preflight free disk: %d KiB (%s); requirement: %d KiB (3 GiB)\n' \
            "$available" "$(format_disk "$available")" "$min_free_kib"
    else
        printf '%s\n' '  preflight free disk: unavailable (df -Pk could not report a number)'
    fi
    rendered_path="${TMPDIR:-/tmp}/iolbox-probe-${PROFILE_NAME}.rendered.yaml"
    printf '  sed -e "s|@IOLBOX_IMAGE_URL@|<profile image URL>|g" -e "s|@IOLBOX_IMAGE_DIGEST@|<algorithm-qualified digest>|g" -e "s|@CPUS@|1|g" -e "s|@MEMORY@|1GiB|g" -e "s|@DISK@|8GiB|g" "%s" > "%s"\n' \
        "$lima_dir/$PROFILE_TEMPLATE" "$rendered_path"
    printf "  %s list --format '{{.Name}}|{{.Status}}'\n" "$limactl_bin"
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
    printf '  %s shell "%s" sudo -E env IOLBOX_PROFILE=%s IOLBOX_KERNEL_SERIES=%s IOLBOX_EXPECTED_UNAME_R=%s bash /opt/iolbox-probe/%s\n' \
        "$limactl_bin" "$machine_name" "$PROFILE_NAME" "$PROFILE_KERNEL_RUNTIME_SERIES" "$PROFILE_EXPECTED_UNAME_R" "$PROFILE_MULTIARCH_STEP"
    printf '  %s shell "%s" sudo -E env IOLBOX_PROFILE=%s IOLBOX_KERNEL_SERIES=%s IOLBOX_EXPECTED_UNAME_R=%s bash /opt/iolbox-probe/30-canary.sh\n' \
        "$limactl_bin" "$machine_name" "$PROFILE_NAME" "$PROFILE_KERNEL_RUNTIME_SERIES" "$PROFILE_EXPECTED_UNAME_R"
    printf '%s\n' '  verdict: compare observed PASS only with an exact profile/product/build row; a canary failure exits 2'
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

[ -f "$guest_dir/lib.sh" ] || fail "missing shared guest helper: $guest_dir/lib.sh"
[ -f "$guest_dir/$PROFILE_MULTIARCH_STEP" ] || \
    fail "profile $PROFILE_NAME multiarch step is missing: $guest_dir/$PROFILE_MULTIARCH_STEP"
[ -f "$guest_dir/30-canary.sh" ] || fail "missing shared canary step: $guest_dir/30-canary.sh"
[ -f "$lima_dir/$PROFILE_TEMPLATE" ] || \
    fail "profile $PROFILE_NAME Lima template is missing: $lima_dir/$PROFILE_TEMPLATE"

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
lookup_qualification
free_kib="$(free_disk_kib || true)"
case "$free_kib" in
    ''|*[!0-9]*) fail 'could not determine free disk space from df -Pk $HOME' ;;
esac
printf 'Preflight free disk: %d KiB (%s); requirement: %d KiB (3 GiB).\n' \
    "$free_kib" "$(format_disk "$free_kib")" "$min_free_kib"
[ "$free_kib" -ge "$min_free_kib" ] || \
    fail "insufficient free disk: ${free_kib} KiB available, ${min_free_kib} KiB required"
require_pinned_probe_image

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
    IOLBOX_EXPECTED_UNAME_R="$PROFILE_EXPECTED_UNAME_R" \
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
    IOLBOX_EXPECTED_UNAME_R="$PROFILE_EXPECTED_UNAME_R" \
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
printf '  host qualification key: %s|%s|%s\n' "$PROFILE_NAME" "$host_macos_product" "$host_macos_build"
printf '  observed: %s (guest exit %d)\n' "$observed_verdict" "$canary_status"
if [ "$canary_status" -ne 0 ]; then
    if [ "$qualification_found" -eq 1 ]; then
        printf '  recorded row: %s (%s) [%s]\n' "$qualification_verdict" "$qualification_status" "$qualification_evidence"
    else
        printf '%s\n' '  qualification: UNRECORDED HOST'
    fi
    printf '%s\n' '  canary failure: exit 2 (the live canary is the authority)'
    exit 2
fi
if [ "$qualification_found" -eq 0 ]; then
    printf '%s\n' '  qualification: UNRECORDED HOST'
    printf '%s\n' '  observed PASS accepted as a new measurement; profiles.env was not changed'
    exit 0
fi
printf '  recorded row: %s (%s) [%s]\n' "$qualification_verdict" "$qualification_status" "$qualification_evidence"
if [ "$observed_verdict" = "$qualification_verdict" ]; then
    printf '%s\n' '  exact qualification match: YES'
else
    printf '%s\n' '  REGRESSION: exact qualification row mismatched the live canary'
    exit 1
fi

exit 0
