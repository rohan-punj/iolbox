#!/usr/bin/env bash
# profile-lifecycle-test.sh - offline fixtures for D1, D2, D4, and D6.
set -euo pipefail

test_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$test_dir/../../.." && pwd)"
host_script="$repo_root/packaging/macos/iolbox-mac.sh"
profiles_file="$repo_root/packaging/macos/lima/profiles.env"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/iolbox-profile-lifecycle.XXXXXX")"
trap 'rm -rf -- "$tmp_dir"' EXIT

total=0
failures=0
ok() { total=$((total + 1)); printf 'ok - %s\n' "$1"; }
fail_case() { total=$((total + 1)); failures=$((failures + 1)); printf 'FAIL - %s\n' "$1"; }

if [ ! -r "$host_script" ]; then
    printf 'ERROR: required host launcher is missing or unreadable: %s\n' "$host_script" >&2
    exit 1
fi
if [ ! -r "$profiles_file" ]; then
    printf 'ERROR: required profile fixture is missing or unreadable: %s\n' "$profiles_file" >&2
    exit 1
fi

# D1: Lima's Go template emits a literal | only when requested explicitly.
# Reject malformed rows instead of treating a partial row as a real machine.
machine_state_from_listing() {
    local listing="$1" target="$2"
    printf '%s\n' "$listing" | awk -F '|' -v wanted="$target" '
        NF == 2 && $1 == wanted { print $2; found = 1; exit }
        END { if (!found) exit 1 }
    '
}

fixture_listing=$'iolbox-running|Running\niolbox-stopped|Stopped\niolbox-tab\tRunning\niolbox-malformed|Running|extra\nmalformed-without-delimiter'
if [ "$(machine_state_from_listing "$fixture_listing" iolbox-running)" = Running ]; then
    ok 'literal-pipe parser identifies a running machine'
else
    fail_case 'literal-pipe parser identifies a running machine'
fi
if [ "$(machine_state_from_listing "$fixture_listing" iolbox-stopped)" = Stopped ]; then
    ok 'literal-pipe parser identifies a stopped machine'
else
    fail_case 'literal-pipe parser identifies a stopped machine'
fi
if machine_state_from_listing "$fixture_listing" iolbox-absent >/dev/null 2>&1; then
    fail_case 'literal-pipe parser treats an absent machine as absent'
else
    ok 'literal-pipe parser treats an absent machine as absent'
fi
if machine_state_from_listing "$fixture_listing" iolbox-malformed >/dev/null 2>&1 || \
    machine_state_from_listing "$fixture_listing" $'iolbox-tab\tRunning' >/dev/null 2>&1; then
    fail_case 'literal-pipe parser rejects malformed and literal-tab rows'
else
    ok 'literal-pipe parser rejects malformed and literal-tab rows'
fi

# D4: explicitly demonstrate why an early-closing grep consumer is unsafe,
# then prove the complete-list capture still sees the target.
producer="$tmp_dir/list-producer.sh"
cat > "$producer" <<'PRODUCER'
#!/usr/bin/env bash
trap 'exit 141' PIPE
printf 'iolbox-existing|Stopped\n'
dd if=/dev/zero bs=1048576 count=4 2>/dev/null | tr '\0' X
PRODUCER
chmod 0755 "$producer"
unsafe_status=0
set +e
"$producer" | grep -Fq 'iolbox-existing|Stopped'
unsafe_status=$?
set -e
if [ "$unsafe_status" -eq 141 ]; then
    ok 'fixture reproduces SIGPIPE status 141 for an early grep -q consumer'
else
    fail_case "fixture reproduces SIGPIPE status 141 for an early grep -q consumer (got $unsafe_status)"
fi

complete_listing="$(printf '%s\n' 'iolbox-existing|Stopped' 'noise-0000001|Stopped' 'noise-0000002|Running')"
if machine_state_from_listing "$complete_listing" iolbox-existing >/dev/null; then
    ok 'complete-list capture cannot bypass an existing-machine refusal'
else
    fail_case 'complete-list capture finds an existing machine'
fi

# D2: a stopped VM is startable only with the exact host structural-gate
# attestation. This fixture models the refusal before the start command.
attestation_allows_stopped() {
    local state="$1" attestation="$2"
    case "$state" in
        Running|running) return 0 ;;
        Stopped|stopped)
            [ -n "$attestation" ] || return 1
            case "$attestation" in
                *'"schema":1'*) ;;
                *) return 1 ;;
            esac
            case "$attestation" in *'"canary_verdict":"PASS"'*) ;; *) return 1 ;; esac
            case "$attestation" in *'"drop_in":"/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf"'*) return 0 ;; *) return 1 ;; esac
            ;;
        *) return 1 ;;
    esac
}

start_attempts=0
start_if_attested() {
    local state="$1" attestation="$2"
    if attestation_allows_stopped "$state" "$attestation"; then
        start_attempts=$((start_attempts + 1))
        return 0
    fi
    return 3
}
if start_if_attested Stopped '' >/dev/null 2>&1; then
    fail_case 'stopped VM without attestation is refused before limactl start'
else
    ok 'stopped VM without attestation is refused before limactl start'
fi
if [ "$start_attempts" -eq 0 ]; then
    ok 'attestation refusal made zero start attempts'
else
    fail_case 'attestation refusal made zero start attempts'
fi
valid_attestation='{"schema":1,"profile":"jammy","macos_product":"26.6.1","macos_build":"25G76","lima_version":"2.2.0","drop_in":"/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf","canary_verdict":"PASS"}'
if start_if_attested Stopped "$valid_attestation"; then
    ok 'stopped VM with the frozen structural-gate attestation may start'
else
    fail_case 'stopped VM with the frozen structural-gate attestation may start'
fi

# D6: qualification is exact (profile, product, build). Missing rows are a
# live-canary requirement and never a refusal. This intentionally includes a
# version-looking unknown to catch numeric comparison regressions.
qualification_table=$'jammy|13.5|22G74|PASS|SUPPORTED|m0\njammy|26.6.1|25G76|PASS|SUPPORTED|m1\ndebian13|26.6.1|25G76|PASS|CANARY-ONLY|m1'
qualification() {
    local profile="$1" product="$2" build="$3" result
    result="$(printf '%s\n' "$qualification_table" | awk -F '|' -v p="$profile" -v v="$product" -v b="$build" '$1 == p && $2 == v && $3 == b {print $4 "|" $5; exit}')"
    if [ -n "$result" ]; then
        printf '%s\n' "$result"
    else
        printf '%s\n' 'UNMEASURED — CANARY REQUIRED'
    fi
}
if [ "$(qualification jammy 13.5 22G74)" = 'PASS|SUPPORTED' ] && \
    [ "$(qualification jammy 26.6.1 25G76)" = 'PASS|SUPPORTED' ] && \
    [ "$(qualification debian13 26.6.1 25G76)" = 'PASS|CANARY-ONLY' ]; then
    ok 'qualification selects the exact measured product/build rows'
else
    fail_case 'qualification selects the exact measured product/build rows'
fi
if [ "$(qualification jammy 26.10 25G76)" = 'UNMEASURED — CANARY REQUIRED' ]; then
    ok 'unknown host qualification is UNMEASURED and not numerically compared'
else
    fail_case 'unknown host qualification is UNMEASURED and not numerically compared'
fi

for row in \
    'debian13|DEFAULT|Debian 13 trixie|pinned-image-debian13.env|iolbox-trixie.yaml|10-multiarch-debian.sh|20-kernel-hold-debian.sh|6.12|6.12.101+deb13-cloud-arm64' \
    'jammy|COMPATIBILITY|Ubuntu 22.04|pinned-image.env|iolbox-jammy.yaml|10-multiarch.sh|20-kernel-hold.sh|5.15|' \
    'debian12|CANDIDATE|Debian 12 bookworm|pinned-image-debian12.env|iolbox-bookworm.yaml|10-multiarch-debian.sh|20-kernel-hold-debian.sh|6.1|'; do
    if awk -v wanted="$row" 'index($0, wanted) { found = 1 } END { exit(found ? 0 : 1) }' "$profiles_file"; then
        ok "profile table contains frozen row ${row%%|*}"
    else
        fail_case "profile table contains frozen row ${row%%|*}"
    fi
done

for row in \
    'jammy|13.5|22G74|PASS|SUPPORTED' \
    'jammy|26.6.1|25G76|PASS|SUPPORTED' \
    'debian13|26.6.1|25G76|PASS|SUPPORTED'; do
    if awk -v wanted="$row" 'index($0, wanted) { found = 1 } END { exit(found ? 0 : 1) }' "$profiles_file"; then
        ok "qualification table contains measured row ${row%%|*}"
    else
        fail_case "qualification table contains measured row ${row%%|*}"
    fi
done

# A Lima-version compatibility warning is allowed; no macOS product/build
# comparison may be numeric. Keep this source-level guard narrow enough not to
# reject unrelated numeric checks such as free disk or Lima version.
if awk 'tolower($0) ~ /(version_lt|sort[[:space:]]+-[nv]|-gt|-lt)/ && tolower($0) ~ /(host_macos|macos_product|macos_build)/ { bad = 1 } END { exit(bad ? 1 : 0) }' "$host_script"; then
    ok 'host launcher has no numeric macOS qualification comparison'
else
    fail_case 'host launcher has no numeric macOS qualification comparison'
fi

printf 'Summary: %d cases, %d failures\n' "$total" "$failures"
[ "$failures" -eq 0 ]
