# M7 Phase 7 continuation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Start **M7 Phase 7** of the Apple Silicon macOS track for iolbox — mechanical
promotion decision (plan section 13) plus the real follow-up work Phase 6
left open. Phase 6 is CLOSED (partial exit, `docs/macos-m7-phase6-handoff.md`),
HEAD `585aa1c` on `luna/macos-m7-phase4-integration` in
`J:\Claude code\iolab-m7-phase4-wt`, working tree clean. The owner has
**personally validated the running native-arm64 build** via its real GUI and
confirmed it passes their own check — that satisfies plan section 13's
"separate, explicit owner sign-off after personally reviewing the actual
measured/running result" requirement, so the code-level promotion step below
is now unblocked.

## Read first, in this order

1. **`docs/macos-m7-phase6-handoff.md`** in this worktree, specifically the
   "Owner promotion ruling" section and everything after it (added at the
   end of Phase 6 and just before this prompt was written) — full context
   on the ruling, what was validated, the two netprobe fixes found/merged
   during validation, and the exact ordered job list this prompt is
   continuing.
2. **`docs/macos-m7-plan.md`** section 13 (Phase 7) in
   `J:\Claude code\iolab-m7-wt` (plan doc lives in the Phase 3 reference
   worktree, not this one) — the authoritative gate-ledger structure and
   PROMOTE/NO-PROMOTE/HOLD/BLOCKED definitions.
3. `tools/iolab-launcher/macos_profile_select.go`'s top-of-file comment and
   `resolveProfileSelection` — the exact code gate you're flipping in job 1.

## Where things actually stand (do not re-derive this, it's already true)

- Native-arm64 cleanly PASSes every gate it was able to reach in Phase 6:
  VM boot parity, lab boot, bidirectional traffic, teardown, no crashes, no
  Rosetta dependency. The owner has since run it themselves through the
  real GUI and confirmed it looks right.
- rosetta-amd64's router console still doesn't work in this lab (0/5 across
  three independent sol-medium-verified fix attempts plus a timeout
  doubling, none of which changed the symptom) — genuinely unresolved, not
  swept under the rug.
- Four-node topology only got one confirming attempt per arm in Phase 6 —
  both failed, but this is explicitly flagged as not-yet-confirmed, not a
  settled regression.
- Two real netprobe console UX bugs (unrelated to the Rosetta-vs-native
  question) were found and fixed during validation: cursor-editing within a
  recalled history line, and a spurious blank line on a bare Enter. Both
  are merged into this line; the spurious-blank-line fix is ALSO already
  pushed to `main` directly (it's generic code with no arm64/macOS
  specificity), so every other build target picks it up on its next build.
- A `--cpus`/`--memory-gib` guest-sizing flag pair was added to the macOS
  launcher on owner request (previously hardcoded 4 vCPU/4GiB, no
  user-facing control) — mirrors the existing Windows `--smp`/`--mem`
  pattern.

## Your job this session, in order

1. **Flip the `auto`-selection code gate.** In
   `tools/iolab-launcher/macos_profile_select.go`, `resolveProfileSelection`
   currently only lets a bare `auto` selection prefer native-arm64 when the
   test-only `IOLBOX_TEST_PREFER_NATIVE=1` env var is set; otherwise it
   always defaults to rosetta-amd64. Change this so `auto` prefers
   native-arm64 whenever native preflight actually passes (same
   `nativePreflight` check already used for forced/persisted selections),
   falling back to rosetta-amd64 with a `FallbackReason` when it doesn't —
   i.e. delete the special-case test-only branch and make the real
   production path do what it was gated to do. Keep the explicit Rosetta
   fallback intact; this is exactly what plan section 13's PROMOTE clause
   describes ("Enable native eligibility for auto only in this reviewed
   combined line and retain explicit Rosetta fallback"). Update/add tests
   in `macos_profile_select_test.go` for the new default-auto-prefers-native
   behavior AND for the fallback-when-preflight-fails case — don't just
   flip the code and hope the existing tests still cover it correctly.
2. **Re-verify the rosetta-amd64 router stall and four-node capacity with
   the mature Go-based M4 test tooling** (`hardware-m4-phase5.sh`, real
   physical Mac, not a lightweight from-scratch harness) — run item-1
   (VPCS/IOL) against the M6 CI candidate archive identified in the Phase 6
   handoff. This settles whether the mature, already-hardened console
   driver reproduces the same stall (strengthens "real product issue") or
   succeeds (points at something specific to Phase 6's own lightweight
   harness). Same for four-node capacity — Phase 6's single-attempt,
   both-arms-fail result is not yet confirmed.
3. Depending on (2)'s outcome: either complete the remaining >=3-run 2-node
   rosetta-amd64 console-dependent metrics (if the stall turns out fixable
   or was a harness artifact) or formally close it as a permanent
   rosetta-amd64 functional gap.
4. **Run the actual Phase 7 mechanical decision**: build the gate ledger
   per plan section 13's categories, compute PROMOTE/NO-PROMOTE/HOLD/
   BLOCKED mechanically from it, and write/update `docs/macos-m7-result.md`
   reproducing that ledger with one row per gate, one raw evidence citation
   each, one verdict each — no row citing only a prose summary. Remember:
   a clean mechanical PROMOTE still only authorizes the gate ledger's own
   label, not a merge/tag/publish — that's the owner's call, surfaced in
   job 5/6, not decided here.
5. **Surface the `main` merge decision, don't execute it unprompted.**
   Nothing on this line is reachable by anyone building from `main` until
   it merges — lay out what's actually in the diff/scope for the owner to
   look at rather than merging on your own judgment.
6. **Surface whether macOS joins the official release pipeline, don't
   decide it.** `.github/workflows/release.yml` has never built macOS —
   adding it is a real scope decision (CI changes, a version tag, ongoing
   maintenance) for the owner, not something to fold in quietly.

## Working pattern (read before starting — unchanged from Phase 4/5/6)

- Direct Sonnet Agent execution for hardware work; sol-medium for
  planning/adversarial review of anything that redefines what a
  gate/contract proves, not every self-evidently-correct fix.
- Actively poll/block on anything long-running yourself — never end a turn
  assuming a passive notification will arrive.
- SSH: `rohansharma@192.168.101.186`, key
  `J:\Claude code\iolab-m7-wt\.m7-ssh\iolbox_mac_m0` — verify the host key
  via `ssh-keyscan` before trusting a possibly-stale IP (known good key:
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`).
  `limactl` at `/opt/homebrew/bin/limactl`; `stop`/`delete` need
  `--tty=false` and `< /dev/null`.
- Protected VMs, never touch: `iolbox-m5-e2e`, `iolbox-m7-native-arm64-qemu`.
  The owner-validation instance (`iolbox-native-arm64` under
  `~/.lima-iolbox-owner-validate`) is still running and reachable — don't
  tear it down without checking whether the owner is done with it.
- If you find a real bug, reproduce it independently before fixing it —
  this standard has caught well over twenty real bugs across the whole
  Phase 3-6 arc, including both netprobe fixes this session.
- Cross-branch/worktree fix audits (like the one that found the netprobe
  fixes) must use content diffs, not just commit ancestry — `main`'s
  history has been rewritten/squashed before, which makes ancestry-only
  checks produce false "not merged" results. See the personal
  `iolbox-release` skill (if available in this session) for the full
  method, or `docs/macos-m7-phase6-handoff.md`'s account of how it was
  done.
