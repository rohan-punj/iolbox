# M7 Phase 5 continuation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Start **M7 Phase 5** of the Apple Silicon macOS track for iolbox — rerun the
authoritative M3/M4 hardware matrix against the real combined launcher. Phase
4 is CLOSED (exit criterion MET: all 9 required scenarios PASS with
real-hardware evidence, HEAD `168e66f` on `luna/macos-m7-phase4-integration`
in `J:\Claude code\iolab-m7-phase4-wt`). Use **sol at medium** only for
planning/adversarial review of anything that actually changes what a frozen
gate/contract proves, and a **direct Sonnet Agent (not codex/CLI)** for
hardware implementation/execution.

## Read first, in this order

1. **`docs/macos-m7-phase4-handoff.md`** in `J:\Claude code\iolab-m7-phase4-wt`
   — full current status, what shipped in Phase 4, hardware access facts, and
   the Phase 5 job description. Read it completely before doing anything.
2. **`docs/macos-m7-plan.md`** section 11 (Phase 5) in
   `J:\Claude code\iolab-m7-wt` (the plan doc lives in the Phase 3 reference
   worktree, not this one) — the authoritative spec and full matrix-group
   table for what this phase requires.
3. **`docs/macos-m7-phase4-file-mapping.md`** in this worktree — what Phase 4
   actually ported/hand-merged/left-behind, useful context for understanding
   the combined artifact you're now testing end-to-end.
4. Phase 0's original matrix procedures/browser steps/labs/image
   hash/expected observations/result formats (referenced by plan section 11
   — locate via `docs/m7-evidence/phase0/` in `J:\Claude code\iolab-m7-wt`)
   — Phase 5 must follow these exactly, not improvise new ones.

## Where things actually stand (do not re-derive this, it's already true)

- Phase 4 is fully closed: `--profile auto|rosetta-amd64|native-arm64`
  selection is real and working on the combined M1-M6 + native-arm64
  launcher artifact. All 9 required scenarios PASS with evidence in
  `docs/m7-evidence/phase4/`. Do not re-run or re-verify these — trust the
  handoff doc's citations.
- **This is the same worktree Phase 5 continues in** — unlike Phase 3→4,
  Phase 5 does NOT get a fresh worktree. Stay on
  `luna/macos-m7-phase4-integration` in `J:\Claude code\iolab-m7-phase4-wt`.
- Nothing needs committing right now — this worktree's tree is clean as of
  commit `168e66f`.
- Phase 4's own Lima VMs are stopped (not deleted) under their own isolated
  `LIMA_HOME` paths — reusable, or delete and recreate fresh if you want a
  clean Phase 5 start. The two protected VMs (`iolbox-m5-e2e`,
  `iolbox-m7-native-arm64-qemu`) must still never be touched.

## Your job this session

1. Re-verify the physical Mac is reachable at the expected IP with the known
   host key before trusting it (DHCP has moved it before; the Mac has gone
   to sleep before too).
2. Locate and read Phase 0's frozen matrix procedures/labs/image hash so
   Phase 5 reproduces them exactly, not an approximation.
3. Work through every matrix group in plan section 11: browser lifecycle,
   host data/sync, consoles/forwarding, capture, VPCS/IOL, multi-link, NAT,
   extnet, capacity (two-node and four-IOL-node), traffic soak (2h
   continuous with capture), forced termination, and Rosetta-exclusion
   inventory (before/during/after). Use real Safari/Chrome browser
   operations, not API calls, unless a Phase 0 owner-approved equivalence
   mapping exists for that specific row.
4. A defect gets the smallest owning fix, then rerun that row plus adjacent
   teardown/restart rows. Budget at least one defect/fix/full-soak rerun
   cycle for the four-node/traffic-soak schedule specifically.
5. If any extnet or authoritative fixture is genuinely unavailable, mark
   that row BLOCKED — never round it up to a complete matrix.
6. Do not start Phase 6 or later without every Phase 5 row actually PASS (or
   honestly BLOCKED with reason).

## Working pattern (read before starting)

- **Direct Sonnet Agent execution beats codex/CLI indirection for hardware
  work.** Reserve sol-medium/codex for planning and adversarial review of
  changes that redefine what a gate/contract proves — not every small,
  self-evidently-correct bugfix. Match rigor to actual stakes (Phase 4 found
  and fixed 6 real bugs this way without over-engineering any of them — see
  `iolab-m7-avoid-overengineering-blockers` memory for the underlying lesson
  from Phase 3).
- **When you do use sol-medium, always pass `-m gpt-5.6-sol` explicitly.**
  The `codex exec` CLI's configured default model is `gpt-5.6-luna` (the
  implementation model) — omitting `-m` silently runs the wrong model.
- **Critical process rule, stated because it cost real time repeatedly
  across this whole project**: any agent that starts a long-running or
  background command must poll it to completion itself — **never end a turn
  with "I'll wait for the notification" as a passive assumption.** Actually
  invoking `Monitor` or `TaskOutput` with `block: true` on a real background
  process is legitimate active polling, not a stall.
- SSH: `rohansharma@192.168.101.186`, key
  `J:\Claude code\iolab-m7-wt\.m7-ssh\iolbox_mac_m0` (lives in the Phase 3
  reference worktree, not this one) — verify the host key via `ssh-keyscan`
  before trusting a possibly-stale IP (known good key:
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
  — DHCP has moved this before, and the Mac has gone fully
  unreachable/asleep before too — ask the owner to wake it if so, there's no
  remote-wake mechanism). `limactl` is at `/opt/homebrew/bin/limactl`.
  `limactl delete`/`stop` and similar need `--tty=false` and `< /dev/null` to
  avoid hanging on stdin over non-tty SSH.
- The Mac is the owner's actively-used laptop — check `vm_stat`/running
  processes before starting VMs, stop VMs when you hit a real stopping
  point. Never touch `iolbox-m5-e2e` or `iolbox-m7-native-arm64-qemu`
  (Phase 3's closed-history VM).
- Commit real progress at each meaningful checkpoint with clear, honest
  commit messages. Stage exact paths only, never `git add -A`/`git add .`.
- If you find a real bug, reproduce it independently before fixing it — this
  standard caught well over fifteen real bugs across the Phase 3+4 arc.
