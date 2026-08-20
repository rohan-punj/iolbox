#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

bash -n "$ROOT/guest/40-install-payload.sh" || fail '40-install-payload.sh syntax'
bash -n "$ROOT/guest/50-verify.sh" || fail '50-verify.sh syntax'
grep -Fq 'Environment=IOLBOX_DISABLE_I386=1' "$ROOT/guest/40-install-payload.sh" || fail 'drop-in omits i386 policy'
grep -Fq 'systemctl show iolbox-supervisor.service -p Environment -p ExecStartPre -p ExecStart' "$ROOT/guest/40-install-payload.sh" || fail 'effective Environment is not verified'
grep -Fq "request_id='macos-verify-hello'" "$ROOT/guest/50-verify.sh" || fail 'hello correlation id missing'
grep -Fq '"arch":"x86_64"' "$ROOT/guest/50-verify.sh" || fail 'runtime arch assertion missing'
grep -Fq '"i386"' "$ROOT/guest/50-verify.sh" || fail 'i386 absence assertion missing'

printf 'capability policy tests: 5 passed\n'
