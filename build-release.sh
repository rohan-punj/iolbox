#!/usr/bin/env bash
# Build a deployable supervisor binary WITH the real GUI embedded.
#
# The embed dir (supervisor/internal/web/dist) holds only a committed
# placeholder in git; the real Svelte bundle is generated and gitignored. So a
# plain `go build` ships the placeholder ("GUI not bundled in this build").
# ALWAYS build the binary through this script (or run build:embed first) — it
# is the single source of truth for a deployable binary.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> building GUI bundle into supervisor/internal/web/dist"
( cd app && npm run build:embed >/dev/null )

echo "==> cross-compiling linux/amd64 supervisor (GUI embedded)"
( cd supervisor && GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor )

# Sanity: the deployed index.html must NOT be the placeholder.
if grep -q "not bundled" supervisor/internal/web/dist/index.html; then
  echo "ERROR: embed dir still holds the placeholder — build:embed did not run" >&2
  exit 1
fi

echo "==> restoring the committed placeholder so the repo stays clean"
git checkout -- supervisor/internal/web/dist/index.html 2>/dev/null || true

ls -la supervisor/bin/supervisor-linux-amd64
echo "OK: supervisor/bin/supervisor-linux-amd64 has the real GUI embedded"
