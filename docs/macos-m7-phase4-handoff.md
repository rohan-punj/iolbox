# M7 Phase 4 handoff — real M1-M6 launcher integration, CLOSED

Written 2026-08-19, end of the Phase 4 session. Branch
`luna/macos-m7-phase4-integration`, worktree `J:\Claude code\iolab-m7-phase4-wt`,
based on `luna/macos-m6-followups` @ `154b58b` (re-verified as still the frozen
M1-M6 commit at session start). Working tree is clean.

**Companion doc**: `docs/macos-m7-phase4-continue-prompt.md` is the paste-ready
prompt for the next session (Phase 5).

## Status at a glance

- **Phase 4: DONE, exit criterion MET.** One combined artifact on the real
  M1-M6 launcher layout now supports `--profile auto|rosetta-amd64|native-arm64`
  and passes all 9 required scenarios from plan section 10 item 4, each with
  real-hardware evidence under `docs/m7-evidence/phase4/`:
  1. Forced native success — PASS
  2. Forced native preflight failure, fails closed — PASS
  3. Forced Rosetta success + working canary — PASS (also closes separate
     native/Rosetta VM/state paths + host sync, scenario 7)
  4. Auto native selection under the explicit test policy — PASS
  5. Auto fallback (failed native → real Rosetta) — PASS
  6. Persisted owner choice honored after restart — PASS
  7. Separate VM/state paths + host sync — PASS (evidenced inside scenario 3's log)
  8. Recovery after a half-created native VM — PASS (after a real fix)
  9. Recovery after forced launcher/VM termination — PASS
- **M1-M6 behavior confirmed intact**: `go build`/`vet`/`test ./...` green;
  cross-compiles clean for linux/amd64, linux/arm64, darwin/arm64,
  windows/amd64; lint.sh all-green; on hardware the rosetta-amd64 path shows
  correct i386 truthfulness, loopback-only port binding, HTTP 200 readiness.
- **Six real defects found and fixed on hardware this phase** (each
  independently reproduced before fixing, each given a unit test, each
  reverified live — see commits below for messages):
  1. A GNU-tar-only flag in `pack-native.sh` (BSD tar on the build side would
     have silently produced a bad package).
  2. A structurally-always-failing digest check in `nativePreflight()`.
  3. A wrong hello-arch string in the new native verify script.
  4. A stderr-noise parsing bug in `limactl list` output that only surfaces
     under a fresh isolated `LIMA_HOME` (i.e. invisible against the default
     `~/.lima`, so easy to miss without a truly fresh VM).
  5. A rosetta-presence-string ordering bug.
  6. A hardcoded `~/.lima` path that ignored `$LIMA_HOME` — this is the one
     that surfaced mid-recovery-scenario and forced a real fix/rerun cycle.
- **Phases 0-4: DONE.** Phase 5 (authoritative M3/M4 hardware matrix, plan
  section 11) is next. Phases 6-7 (A/B metrics, promotion decision) follow
  after Phase 5.

## What actually shipped this phase (file mapping)

Full detail in `docs/m7-phase4-file-mapping.md`. Short version:

- **Cherry-picked as-is** from the reviewed `luna/macos-m7-arm64` branch: the
  arch-neutral supervisor/tools fixes (byte-order bugfix in
  `egress/detect_linux.go`, new `arm64_*_test.go` files, `fetch-vpcs.sh`'s
  `--arch` support).
- **Hand-merged, not cherry-picked** (M7's own diff would have silently
  regressed the real M1-M6 product if applied verbatim):
  - `server.go` — M7 had deleted `DisableI386` entirely; kept it, took only
    the arch-aware hello default.
  - `pack-native.sh` / `install.sh` — M7 had stripped the entire
    tool-pack/toollaunch/ioltool product surface; kept it, added
    `--arch`/fail-closed ELF checks/`manifest.env` on top.
- **New this phase, not from the M7 branch at all** (the M7 branch never
  built a native-arm64 guest pipeline, only packaging/provisioning for it):
  - `packaging/macos/guest/{10-multiarch-native,30-canary-native,40-install-payload-native,50-verify-native}.sh`
    — native-arm64's own guest-side canary/install/verify pipeline. Fails
    closed if Rosetta is ever detected present; runs the still-x86_64 IOL
    binary through an in-guest qemu-user translator; asserts a plain-arm64
    `ExecStart`.
  - `packaging/macos/lima/profiles.env` — new `native-arm64` row.
  - `tools/iolab-launcher/macos_{profiles,lifecycle,diagnostics,cli,profile_select}.go`
    — the `--profile` selection layer (explicit flag > persisted choice >
    auto; auto still defaults to Rosetta pre-promotion, native preference is
    behind an explicit test-only policy hook) and truthful `status`/`diagnose`
    output (requested/selected profile, fallback reason, backend, translator,
    guest/supervisor arch, `rosetta_present`).
- **Deliberately left behind**: `tools/phase3-baking/**`,
  `tools/translation-rehearsal/**`, and M7's `build-rootfs.sh` diff (all
  Mac-side Phase 3 baking/provisioning tooling, not launcher code — no reason
  to carry it into the shipped launcher). Two M7 test scripts written against
  M7's packs-stripped script were also left behind since that script itself
  wasn't ported.

## Hardware access (current as of this session)

Same as the Phase 3 handoff — unchanged this phase:

- **Physical Mac**: `rohansharma@192.168.101.186` — DHCP has moved this
  before; verify via `ssh-keyscan` against the known host key
  (`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`)
  before trusting it. Key `.m7-ssh/iolbox_mac_m0` (relative to the M7 Phase 3
  worktree, `J:\Claude code\iolab-m7-wt`; not duplicated into this Phase 4
  worktree — reach across if you need the key file). The Mac has gone fully
  unreachable (asleep) mid-session before — ask the owner to wake it if so,
  no remote-wake mechanism exists.
- **`limactl` is at `/opt/homebrew/bin/limactl`**, not on default PATH.
- **`limactl delete`/`stop` (and possibly other subcommands) hangs on stdin**
  over non-tty SSH unless given `--tty=false` **and** `< /dev/null`. Apply to
  every non-interactive `limactl` call.
- **Protected VMs — never touch**: `iolbox-m5-e2e` (default `~/.lima`) and
  `iolbox-m7-native-arm64-qemu` (isolated `LIMA_HOME=~/.lima-iolbox-m7p3`,
  Phase 3's closed-history VM with a pinned disk identity). Both were
  independently reverified untouched at the end of this session.
- **This phase's own VMs**: stopped (not deleted) at session end under their
  own isolated `LIMA_HOME` paths — safe to resume from for Phase 5, or delete
  and recreate fresh if Phase 5 wants a clean start. A throwaway vpcs-builder
  VM used mid-session was deleted.
- **Owner L3 image**:
  `/Users/rohansharma/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`
  on the Mac, frozen SHA-256
  `b858503827356c55bb4e51f73fe4378ba46b6eefb20552fdccda03240cbab925` — never
  copy into the repo.

## Next session's actual job: Phase 5

Per `docs/macos-m7-plan.md` section 11 (`J:\Claude code\iolab-m7-wt`, the
Phase 3 reference worktree — plan doc itself wasn't duplicated into this
Phase 4 worktree). Unlike Phase 4, the plan does **not** call for a fresh
worktree for Phase 5 — it runs "the combined Phase 4 candidate," i.e.
continue directly in `J:\Claude code\iolab-m7-phase4-wt` on
`luna/macos-m7-phase4-integration`.

Phase 5 reruns the **authoritative M3/M4 hardware matrix** using the exact
Phase 0 procedures/browser steps/labs/image hash/expected observations —
**do not replace browser operations with API calls** unless a Phase 0
owner-approved equivalence mapping exists for that row. Required matrix
groups (plan section 11 has the full table): browser lifecycle, host
data/sync (survives restart, spaces/non-ASCII paths), consoles/forwarding
(two simultaneous, Mac-loopback-only), capture (valid pcapng), VPCS/IOL
(native-arm64 VPCS ↔ translated x86_64 IOL bidirectional traffic), multi-link,
NAT, extnet, capacity (two-node and four-IOL-node), traffic soak (2 hours
continuous with capture, record loss/faults/exits/CPU/memory), forced
termination (no stale taps/bridges/processes/sockets/corrupt state), and a
full Rosetta-exclusion mount/binfmt/process inventory before/during/after.

A defect gets the smallest owning fix and reruns that row plus adjacent
teardown/restart rows. The four-node/traffic-soak schedule includes at least
one defect/fix/full-soak rerun cycle. **Exit**: every row PASS; an
unavailable extnet/authoritative fixture is BLOCKED and must never be rounded
up to a complete matrix.

## Working pattern used this arc (recommended to continue)

Unchanged from Phase 3/4: direct execution via a Sonnet Agent running real
Bash/SSH commands — no codex/CLI indirection for implementation or hardware
execution. Reserve sol-medium (`codex exec -m gpt-5.6-sol`, **always pass
`-m` explicitly**) for planning/review work that actually changes what a
frozen gate/contract proves. Reproduce every real bug independently before
fixing it — this discipline caught 6 more real bugs this phase alone, on top
of 10+ in Phase 3. Background/long-running commands must be actively polled
to completion, never passively waited on.
