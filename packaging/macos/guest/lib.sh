# lib.sh — shared helpers for the iolbox macOS/Lima guest provisioning steps.
#
# SOURCED, never executed. Every script under packaging/macos/guest/ starts
# with:
#     . "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
#
# The host entry point (packaging/macos/iolbox-mac.sh) stages this whole
# directory into the guest at $IOLBOX_PROVISION_DIR and runs the numbered
# steps in order. Keeping the helpers in one file is what makes the exit-code
# contract below uniform across steps — the host distinguishes "your Mac
# cannot run this" (2) from "the guest is misconfigured" (3) purely by the
# exit status it gets back.

# ---------------------------------------------------------------------------
# Exit-code contract. Shared by guest steps AND the host entry point.
# ---------------------------------------------------------------------------
# 0  success
# 1  usage / internal error (bad flags, missing file we shipped ourselves)
# 2  ROSETTA CANARY FAILED — this Mac + guest kernel pair cannot execute
#    amd64. Fail closed; never continue into an install. See guest/30-canary.sh.
# 3  preflight failed — wrong guest arch, no systemd, no /dev/net/tun, no
#    Rosetta binfmt registration, kernel outside the qualified series.
# 4  apt / repository failure (multiarch sources, amd64 package install)
# 5  post-install verification failed — payload installed but the service or
#    the GUI listener did not come up.
IOLBOX_EXIT_OK=0
IOLBOX_EXIT_USAGE=1
IOLBOX_EXIT_CANARY=2
IOLBOX_EXIT_PREFLIGHT=3
IOLBOX_EXIT_APT=4
IOLBOX_EXIT_VERIFY=5

# ---------------------------------------------------------------------------
# The qualified environment. Single source of truth for the guest side; the
# pinned image URL/digest lives host-side in lima/pinned-image.env because the
# host is what downloads it.
# ---------------------------------------------------------------------------
IOLBOX_KERNEL_SERIES="${IOLBOX_KERNEL_SERIES:-5.15}"
IOLBOX_EXPECTED_UNAME_R="${IOLBOX_EXPECTED_UNAME_R:-}"
IOLBOX_PROFILE="${IOLBOX_PROFILE:-jammy}"
IOLBOX_PROFILE_STATUS="${IOLBOX_PROFILE_STATUS:-UNMEASURED}"
IOLBOX_IMAGE_QUALIFICATION="${IOLBOX_IMAGE_QUALIFICATION:-}"
IOLBOX_GUEST_ARCH="aarch64"
IOLBOX_LOADER="/lib64/ld-linux-x86-64.so.2"
IOLBOX_POLICY_FILE="/etc/iolbox/macos-guest-policy"
IOLBOX_GUI_PORT="${IOLBOX_GUI_PORT:-4001}"

# ---------------------------------------------------------------------------
# Logging. Everything diagnostic goes to stderr so a step's stdout stays
# machine-readable when a caller wants to capture it.
# ---------------------------------------------------------------------------
iolbox_step_name="${IOLBOX_STEP_NAME:-$(basename "${0:-guest}")}"

log()  { printf '[%s] %s\n' "$iolbox_step_name" "$*" >&2; }
warn() { printf '[%s] WARNING: %s\n' "$iolbox_step_name" "$*" >&2; }

# die <exit-code> <message...>
die() {
    local code="$1"; shift
    printf '[%s] ERROR: %s\n' "$iolbox_step_name" "$*" >&2
    exit "$code"
}

# ---------------------------------------------------------------------------
# Host facts, threaded in from the Mac side.
#
# The guest cannot see its own hypervisor host, but a compatibility failure is
# only actionable if the message names the macOS build and Lima version that
# produced it. iolbox-mac.sh exports these before invoking any step; they are
# "unknown" when a step is run by hand from inside the guest.
# ---------------------------------------------------------------------------
IOLBOX_HOST_MACOS="${IOLBOX_HOST_MACOS:-unknown}"        # e.g. "13.5 (22G74)"
IOLBOX_HOST_LIMA="${IOLBOX_HOST_LIMA:-unknown}"          # e.g. "2.2.0"
IOLBOX_MACHINE="${IOLBOX_MACHINE:-unknown}"              # Lima machine name
IOLBOX_HOST_MACOS_PRODUCT="${IOLBOX_HOST_MACOS_PRODUCT:-}"
IOLBOX_HOST_MACOS_BUILD="${IOLBOX_HOST_MACOS_BUILD:-}"

# ---------------------------------------------------------------------------
# Small shared predicates.
# ---------------------------------------------------------------------------

# have <command> — true if the command exists.
have() { command -v "$1" >/dev/null 2>&1; }

# kernel_series — "5.15" from "5.15.0-185-generic".
kernel_series() { uname -r | cut -d. -f1,2; }

# iolbox_kernel_series_from_value <kernel-or-series>
#
# The host profile table may provide either a series (6.12) or the exact
# running release (6.12.101+deb13-cloud-arm64).  The policy always compares
# the first two numeric components as the series and, when supplied, compares
# the complete uname -r separately.
iolbox_kernel_series_from_value() {
    printf '%s\n' "$1" | cut -d. -f1,2
}

# iolbox_expected_uname_r [suite]
#
# Exact-kernel qualification is intentionally narrow.  Jammy preserves its
# 5.15 series for M0 comparability but has no exact release pin in the profile
# table.  Trixie's pinned image supplies the measured exact release.  The
# explicit environment value lets the host profile table remain authoritative.
iolbox_expected_uname_r() {
    local suite="${1:-}"

    if [ -n "$IOLBOX_EXPECTED_UNAME_R" ]; then
        printf '%s\n' "$IOLBOX_EXPECTED_UNAME_R"
        return 0
    fi
    case "${suite:-$IOLBOX_PROFILE}" in
        debian13|trixie)
            printf '%s\n' '6.12.101+deb13-cloud-arm64' ;;
        *)
            printf '\n' ;;
    esac
}

# assert_kernel_qualification <exit-code> [expected-uname-r]
#
# Kernel policy is reproducibility metadata.  The executable canary remains
# the only Rosetta authority; this helper never claims that a kernel series is
# universally safe or unsafe.
assert_kernel_qualification() {
    local exit_code="$1" expected_uname="${2:-}" actual_uname actual_series expected_series

    actual_uname="$(uname -r)"
    expected_series="$(iolbox_kernel_series_from_value "$IOLBOX_KERNEL_SERIES")"
    actual_series="$(iolbox_kernel_series_from_value "$actual_uname")"
    [ -n "$expected_series" ] && [ "$actual_series" = "$expected_series" ] || die "$exit_code" \
        "guest kernel series '$actual_series' is outside qualified '$IOLBOX_KERNEL_SERIES'; kernel policy is reproducibility metadata and the executable canary is the authority"
    if [ -z "$expected_uname" ]; then
        expected_uname="$(iolbox_expected_uname_r)"
    fi
    if [ -n "$expected_uname" ] && [ "$actual_uname" != "$expected_uname" ]; then
        die "$exit_code" \
            "guest kernel '$actual_uname' does not match the profile's exact qualified kernel '$expected_uname'"
    fi
}

# iolbox_image_qualification [suite]
iolbox_image_qualification() {
    local suite="${1:-}"

    if [ -n "$IOLBOX_IMAGE_QUALIFICATION" ]; then
        printf '%s\n' "$IOLBOX_IMAGE_QUALIFICATION"
        return 0
    fi
    case "${suite:-$IOLBOX_PROFILE}" in
        debian12|bookworm) printf '%s\n' 'UNPINNED (candidate; unqualified while image is unpinned)' ;;
        jammy|debian13|trixie) printf '%s\n' 'PINNED' ;;
        *) printf '%s\n' 'UNKNOWN' ;;
    esac
}

# iolbox_kernel_qualification [suite]
iolbox_kernel_qualification() {
    local suite="${1:-${IOLBOX_PROFILE}}"

    case "$suite" in
        jammy) printf '%s\n' 'preserve the 5.15 series for M0 comparability' ;;
        debian13|trixie) printf '%s\n' 'preserve the pinned image exact kernel 6.12.101+deb13-cloud-arm64 (series 6.12)' ;;
        debian12|bookworm) printf '%s\n' 'series 6.1 is recorded for reproducibility; exact kernel and image remain unqualified while the image is unpinned' ;;
        *) printf '%s\n' 'profile-supplied kernel qualification' ;;
    esac
}

# text_contains_exact_line <captured-lines> <wanted>
#
# Consume the complete text before returning.  This is deliberately not a
# producer | grep -q pipeline: under pipefail, grep -q can close early and
# turn a large producer's SIGPIPE into status 141.
text_contains_exact_line() {
    local text="$1" wanted="$2" line

    while IFS= read -r line || [ -n "$line" ]; do
        [ "$line" = "$wanted" ] && return 0
    done <<< "$text"
    return 1
}

# iolbox_json_escape <text> -- escape a value for a JSON string.
iolbox_json_escape() {
    local LC_ALL=C
    local value="$1" char code i

    for ((i = 0; i < ${#value}; i++)); do
        char="${value:i:1}"
        case "$char" in
            \\) printf '\\\\' ;;
            '"') printf '\\"' ;;
            $'\b') printf '\\b' ;;
            $'\f') printf '\\f' ;;
            $'\n') printf '\\n' ;;
            $'\r') printf '\\r' ;;
            $'\t') printf '\\t' ;;
            *)
                printf -v code '%d' "'${char}"
                if [ "$code" -lt 32 ]; then
                    printf '\\u%04x' "$code"
                else
                    printf '%s' "$char"
                fi
                ;;
        esac
    done
}

# elf_machine <path> — prints the ELF e_machine value as a bare hex number
# ("3e" for x86-64, "b7" for aarch64), or nothing if the file is not ELF.
# Deliberately implemented with od(1) only: `file` and `readelf` are not
# guaranteed present on a cloud image, and this is used to ASSERT that the
# packages we installed really are amd64 rather than trusting apt's word.
elf_machine() {
    local f="$1" magic
    [ -r "$f" ] || return 1
    magic="$(od -An -tx1 -N2 -j0 "$f" 2>/dev/null | tr -d ' \n')"
    [ "$magic" = "7f45" ] || return 1          # 0x7f 'E'
    od -An -tx1 -N1 -j18 "$f" 2>/dev/null | tr -d ' \n'
}

# assert_amd64_elf <path> — die() unless the file is an x86-64 ELF.
assert_amd64_elf() {
    local f="$1" m
    m="$(elf_machine "$f" || true)"
    [ "$m" = "3e" ] || die "$IOLBOX_EXIT_APT" \
        "$f is not an x86-64 ELF (ELF e_machine=0x${m:-none}). The amd64 \
multiarch set did not install correctly — see packaging/macos/guest/10-multiarch.sh."
}
