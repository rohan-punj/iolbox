#!/usr/bin/env bash
# Build deployable supervisor binaries WITH the real GUI embedded.
#
# The embed dir (supervisor/internal/web/dist) holds only a committed
# placeholder in git; the real Svelte bundle is generated and gitignored. So a
# plain `go build` ships the placeholder ("GUI not bundled in this build").
# ALWAYS build the binary through this script (or run build:embed first) — it
# is the single source of truth for a deployable binary.
#
# Usage: build-release.sh [--arch amd64|arm64|amd64,arm64]
#
# With no --arch this behaves exactly as it always has: one
# GOOS=linux GOARCH=amd64 build producing supervisor/bin/supervisor-linux-amd64.
# --arch may be repeated or given a comma-separated list.
#
# Why the multi-arch case lives HERE rather than as extra `go build` steps in
# the release workflow: this script runs `npm run build:embed` once and then
# RESTORES the committed placeholder when it finishes. Any compile that runs
# after this script has exited therefore silently embeds the placeholder — a
# real, previously-shipped failure mode. Building every architecture inside
# one invocation, from one embed, is what guarantees each binary carries the
# same real GUI bundle.
set -euo pipefail
cd "$(dirname "$0")"

ARCHS=""

die() { echo "build-release: $*" >&2; exit 1; }

while [ "$#" -gt 0 ]; do
    case "$1" in
        --arch)
            [ "$#" -ge 2 ] || die "--arch needs a value"
            ARCHS="${ARCHS:+$ARCHS,}$2"
            shift 2
            ;;
        --arch=*)
            value="${1#*=}"
            [ -n "$value" ] || die "--arch needs a value"
            ARCHS="${ARCHS:+$ARCHS,}$value"
            shift
            ;;
        -h|--help)
            sed -n '1,20p' "$0"
            exit 0
            ;;
        *) die "unknown option: $1" ;;
    esac
done

# Historical default: no flag means amd64 only, byte-for-byte the same command
# as before this script learned --arch.
[ -n "$ARCHS" ] || ARCHS="amd64"

# Normalize to a deduplicated, validated, order-preserving list.
SELECTED=""
IFS=',' read -r -a _requested <<< "$ARCHS"
for arch in "${_requested[@]}"; do
    arch="$(printf '%s' "$arch" | tr -d '[:space:]')"
    [ -n "$arch" ] || die "--arch contains an empty entry (got '$ARCHS')"
    case "$arch" in
        amd64|arm64) ;;
        *) die "--arch must be amd64 or arm64, got '$arch'" ;;
    esac
    case " $SELECTED " in
        *" $arch "*) die "--arch lists $arch more than once" ;;
    esac
    SELECTED="${SELECTED:+$SELECTED }$arch"
done

# Capture the git describe BEFORE any subshell cd's around, so the version
# always reflects the repo commit being built, not wherever go build runs from.
VERSION="$(git describe --tags --always --dirty)"
echo "==> stamping version: $VERSION"
echo "==> target architectures: $SELECTED"

# Restore the committed placeholder on EVERY exit path. Before --arch there
# was exactly one compile and one exit point; now a failure in the second
# compile would otherwise leave the generated bundle sitting in the working
# tree. The trap keeps "the repo stays clean" true unconditionally.
restore_placeholder() {
    git checkout -- supervisor/internal/web/dist/index.html 2>/dev/null || true
}
trap restore_placeholder EXIT

echo "==> building GUI bundle into supervisor/internal/web/dist"
( cd app && npm run build:embed >/dev/null )

# Sanity: the deployed index.html must NOT be the placeholder. Checked once,
# after the single embed and BEFORE any compile, so no architecture can ship
# the placeholder.
if grep -q "not bundled" supervisor/internal/web/dist/index.html; then
  echo "ERROR: embed dir still holds the placeholder — build:embed did not run" >&2
  exit 1
fi

for arch in $SELECTED; do
    echo "==> cross-compiling linux/$arch supervisor (GUI embedded)"
    ( cd supervisor && GOOS=linux GOARCH="$arch" go build -ldflags "-X main.version=$VERSION" -o "bin/supervisor-linux-$arch" ./cmd/supervisor )
done

echo "==> restoring the committed placeholder so the repo stays clean"
restore_placeholder

for arch in $SELECTED; do
    ls -la "supervisor/bin/supervisor-linux-$arch"
    echo "OK: supervisor/bin/supervisor-linux-$arch has the real GUI embedded"
done
