#!/usr/bin/env bash
# lint.sh — static checks for every shell script under packaging/macos/.
#
# M1's provisioner cannot be exercised end-to-end anywhere except an Apple
# Silicon Mac with Lima installed, so static checking is the only gate that
# runs everywhere (developer laptop, Linux builder, CI). It is deliberately
# cheap and dependency-optional:
#
#   1. `bash -n` on every script — always runs, catches syntax errors.
#   2. `shellcheck -x` on every script — runs only if shellcheck is installed,
#      and is reported as SKIPPED (not passed) when it is not. A skipped check
#      must never read as a green one.
#   3. A house-style sweep: every executable script declares
#      `#!/usr/bin/env bash` and sets `set -euo pipefail`.
#
# Note on step 1 and macOS: iolbox-mac.sh must run under macOS's bash 3.2,
# which `bash -n` on a bash 5 host will NOT catch (bash 5 parses bash-5-only
# syntax happily). That gap is real and is called out in packaging/macos/README.md;
# it is closed only by running the script on the Mac.
#
# Usage: packaging/macos/tests/lint.sh [--strict]
#   --strict   treat a missing shellcheck as a failure instead of a skip
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"          # packaging/macos
STRICT=0

usage() {
    cat <<EOF
Usage: $0 [--strict]

  --strict    fail if shellcheck is not installed (default: skip it)
  -h, --help  this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --strict) STRICT=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

fail=0
note() { printf '%s\n' "$*" >&2; }

# Collect scripts: everything ending in .sh, plus lib.sh (sourced, not
# executed — still worth parsing).
scripts=()
while IFS= read -r f; do
    scripts+=("$f")
done < <(find "$ROOT" -type f -name '*.sh' | sort)

if [ "${#scripts[@]}" -eq 0 ]; then
    note "lint.sh: found no shell scripts under $ROOT — nothing to check."
    exit 1
fi

note "lint.sh: checking ${#scripts[@]} script(s) under $ROOT"

# M3 adds a Mac-host harness; keep its presence explicit so a packaging
# omission cannot make the generic recursive sweep look complete.
if [ ! -f "$ROOT/tests/hardware-m3.sh" ]; then
    note "  FAIL    inventory hardware-m3.sh is missing"
    fail=1
else
    note "  ok      inventory hardware-m3.sh is present"
fi

# --- 1. syntax -------------------------------------------------------------
for f in ${scripts[@]+"${scripts[@]}"}; do
    if bash -n "$f" 2>/dev/null; then
        note "  ok      bash -n  ${f#"$ROOT"/}"
    else
        note "  FAIL    bash -n  ${f#"$ROOT"/}"
        bash -n "$f" || true
        fail=1
    fi
done

# --- 2. shellcheck ---------------------------------------------------------
if command -v shellcheck >/dev/null 2>&1; then
    for f in ${scripts[@]+"${scripts[@]}"}; do
        # -x follows `source`d files (the guest steps all source lib.sh).
        if shellcheck -x -S style "$f"; then
            note "  ok      shellcheck  ${f#"$ROOT"/}"
        else
            note "  FAIL    shellcheck  ${f#"$ROOT"/}"
            fail=1
        fi
    done
else
    if [ "$STRICT" -eq 1 ]; then
        note "  FAIL    shellcheck not installed and --strict was given"
        fail=1
    else
        note "  SKIPPED shellcheck is not installed — these scripts have NOT been"
        note "          shellcheck-clean-verified on this host."
    fi
fi

# --- 3. house style --------------------------------------------------------
for f in ${scripts[@]+"${scripts[@]}"}; do
    base="${f#"$ROOT"/}"
    # lib.sh is sourced, never executed: it intentionally has no shebang and
    # must NOT set -e (that would leak into whatever sources it).
    case "$base" in
        guest/lib.sh) continue ;;
    esac
    if ! head -1 "$f" | grep -q '^#!/usr/bin/env bash$'; then
        note "  FAIL    style     $base: first line is not '#!/usr/bin/env bash'"
        fail=1
    fi
    if ! grep -q '^set -euo pipefail$' "$f"; then
        note "  FAIL    style     $base: missing 'set -euo pipefail'"
        fail=1
    fi
done

# --- 4. regression traps ---------------------------------------------------
# A complete capture followed by awk is intentional. A producer feeding
# grep -q can receive SIGPIPE under pipefail, returning 141 and bypassing an
# "already exists" refusal. Keep this check textual and cheap so it also
# covers host-only scripts that are not exercised on this machine.
pipeline_hits="$(grep -R -n -E 'limactl[[:space:]]+list[^|]*\|[[:space:]]*grep[[:space:]]+(-[A-Za-z]*q|--quiet)' "$ROOT" --include='*.sh' 2>/dev/null || true)"
if [ -n "$pipeline_hits" ]; then
    note "  FAIL    safety    limactl list is piped to grep -q (SIGPIPE/pipefail hazard):"
    note "$pipeline_hits"
    fail=1
else
    note "  ok      safety    no early-closing grep consumer for Lima machine lists"
fi

# Bash expands every argument to an assignment builtin before assigning any
# of them. Under set -u, `local a=1 b=\"$a/x\"` therefore aborts; bash -n
# cannot see that semantic error. Reject the compact same-line pattern.
self_ref_hits="$(find "$ROOT" -type f -name '*.sh' -exec awk '
    /(^|[;[:space:]])local[[:space:]]+[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+[[:space:]]+[A-Za-z_][A-Za-z0-9_]*="[$][A-Za-z_][A-Za-z0-9_]*/ {
        print FILENAME ":" FNR ":" $0
    }
' {} +)"
if [ -n "$self_ref_hits" ]; then
    note "  FAIL    safety    same-line local assignment references an earlier local:"
    note "$self_ref_hits"
    fail=1
else
    note "  ok      safety    no same-line local self-reference assignment"
fi

if [ "$fail" -ne 0 ]; then
    note "lint.sh: FAILED"
    exit 1
fi
note "lint.sh: all checks passed"
