#!/usr/bin/env bash
# canary-debian12.sh - compatibility spelling for the generic profile probe.
#
# The implementation is intentionally shared: adding another candidate uses
# canary-probe.sh --profile <name>, not another probe implementation.
# Debian 12 remains CANDIDATE and UNVERIFIED on hardware; its 6.1 result is
# inferred from the 6.3 threshold until the generic probe measures it.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$test_dir/canary-probe.sh" --profile debian12 "$@"
