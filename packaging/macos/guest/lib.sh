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
IOLBOX_KERNEL_SERIES="${IOLBOX_KERNEL_SERIES:-5.15}"   # see docs/macos-m0-result.md:37-56
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

# ---------------------------------------------------------------------------
# Small shared predicates.
# ---------------------------------------------------------------------------

# have <command> — true if the command exists.
have() { command -v "$1" >/dev/null 2>&1; }

# kernel_series — "5.15" from "5.15.0-185-generic".
kernel_series() { uname -r | cut -d. -f1,2; }

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
