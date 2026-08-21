# M7 Phase 8 continuation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Continue **M7 Phase 8** (first-tag readiness) for iolbox's macOS
Apple-Silicon native-arm64 track. `main` is at `dba09d5` in
`J:\Claude code\iolab` (the primary checkout may be on a different branch —
check `git branch --show-current` before assuming; work in a fresh
`git worktree add` off `main` if so, same pattern the last few sessions
used). Working tree elsewhere should be clean; verify with `git status`
before touching anything.

## Read first, in this order

1. **`docs/macos-m7-phase8-handoff.md`** in this worktree — full context on
   everything that shipped this arc (Phase 7 gate ledger, the auto-profile
   existing-install fix, the release-pipeline wiring, and the qemu-user
   redistribution with its two hardware-caught bugs) and the exact three
   real gaps this session is closing.
2. **`docs/macos-native-arm64-qemu-redistribution-plan.md`** — the design
   doc for the qemu bundle, if you need the full rationale/mechanics behind
   `packaging/macos/guest-assets/` before touching it.
3. **`docs/macos-release-native-arm64-plan.md`** — the design doc for the
   release-pipeline wiring itself, if you need `release.yml`/`pack-release.sh`
   context.
4. **`docs/macos-m7-result.md`** — the Phase 7 gate ledger, for the overall
   evidence-discipline standard this whole arc holds itself to (a missing
   metric is UNEVALUATED, never zero; no gate may cite only a prose
   summary) — apply the same standard to whatever you find this session.

## Your job this session, in order

1. **Get a real CI run to actually exercise `--bundle-guest-qemu`.** The
   `build-macos` job in `.github/workflows/release.yml` needs to run for
   real against the current `main` (or a throwaway tag/branch push that
   triggers it — check the workflow's `on:` triggers first) and produce a
   real `iolbox-macos-arm64.tar.gz` artifact. Confirm via the Actions run
   log (use `gh run list`/`gh run view`/`gh run download` — `gh` is
   available per this project's established pattern) that: the arm64
   payload builds, the qemu bundle assembly step in `runtime/pack-native.sh
   --bundle-guest-qemu` succeeds, and the produced archive actually contains
   the 12-package bundle plus `MANIFEST`/`SOURCE-OFFER.txt`/`notices/` (use
   `tar -tzf` on the downloaded artifact, don't just trust the log said
   success). If CI catches something the hand-built export missed — reread
   `docs/macos-m7-phase8-handoff.md`'s account of `92c5635` first, that's
   exactly the class of bug a real CI checkout would catch that a manual
   `git archive` export might not, depending on what differs.
2. **Run the outer archive assembly for real, with the bundle present.**
   Either as part of the same CI run (if step 1's run naturally produces
   this) or by hand on the Mac: run `packaging/macos/pack-release.sh`
   end-to-end with both Linux payloads (rosetta amd64 + native arm64, the
   arm64 one built `--bundle-guest-qemu`) as input, producing a real
   `iolbox-macos-arm64.tar.gz`, then run
   `packaging/macos/tests/release-layout-test.sh` against it and confirm
   every archive-layout assertion (including the ones `ac803bd` added for
   the qemu bundle's manifest/notices/license files) actually passes against
   a real assembled archive, not just the raw payload this session already
   validated.
3. **Build and publish (or at minimum, build and verify) the
   corresponding-source asset.** Run
   `packaging/macos/guest-assets/fetch-corresponding-source.sh` for real —
   confirm it produces the ~207 MB, 9-package source bundle the design doc
   describes, that every component's checksum matches its `.dsc` file, and
   that it's wired into the release process (as a GitHub release asset
   alongside the macOS archive, per the design doc) rather than just sitting
   as a local build artifact. If disk space is tight on whatever machine
   you're building this on (the physical Mac had only ~5.8 GiB free at the
   end of the last session — check current free space before assuming
   there's room), plan for that rather than discovering it mid-build.
4. **Once 1-3 are real and clean**, this is genuinely first-tag-ready.
   Surface the actual tag/release decision to the owner rather than cutting
   it yourself — same standing pattern this whole arc has used for
   consequential release actions (see how the `main`-merge and
   release-pipeline decisions were surfaced in Phase 7). Prepare a concrete
   summary of what the tag would include and what gates it passed, and ask.
5. **Optional, lower priority**: fix the inaccurate `update-binfmts`
   comment in `packaging/macos/guest/10-multiarch-native.sh` (flagged, not
   fixed, in the last session — it claims the call makes registration
   "explicit and idempotent," but the real registration comes from
   systemd's `binfmt.d`, not `update-binfmts`'s own database, so the call is
   actually a harmless no-op). A one-line comment fix, not a behavior
   change.
6. **Optional, lower priority**: boot a real two-node IOL lab through the
   native-arm64 profile with the redistributed qemu bundle specifically (not
   just the IOL binary's own usage banner, which is all last session proved)
   — a real `iourc` licence is needed in the guest for this, which wasn't
   available last session.

## Working pattern (read before starting — established across this whole
arc, including this session)

- Opus-tier agent execution for implementation and hardware work; `codex
  exec` at `gpt-5.6-sol`/medium reasoning effort for adversarial review of
  any plan-level artifact (design docs, the gate ledger, anything that
  redefines what a gate/contract proves) BEFORE implementing it — never the
  raw `mcp__codex__codex` MCP tool, it's blocked; always the CLI, always
  with `< /dev/null` before the pipe or it hangs forever waiting on stdin,
  always `--sandbox read-only` for a review-only pass.
- Every merge to `main` this arc got built/vet/tested (and YAML/shell
  syntax-checked where relevant) before pushing, and every push was
  confirmed with the user first except where they'd already given standing
  approval for a specific action — don't assume blanket future-push
  authorization from a past approval; ask each time unless told otherwise.
- Actively poll long-running commands (CI runs, hardware SSH sessions,
  backgrounded `codex exec` calls) yourself — never end a turn assuming a
  passive notification will arrive. This arc hit a specific gotcha twice:
  a background agent got marked "completed" by the harness while it was
  still actively waiting on its own backgrounded `codex exec` subprocess to
  finish composing a review — if you spawn a sub-agent for this work and it
  reports back mid-task text like "I'll wait for the monitor event" or
  "still composing its verdict," that is NOT a finished result; resume it
  with `SendMessage` and tell it to actually check whether the process
  finished rather than treating the notification as completion.
- If you find a real bug, reproduce it independently before fixing it, and
  re-verify any hardware fix on a genuinely fresh VM afterward — this
  standard caught two real bugs (`92c5635`, `a7b5a02`) in the qemu
  redistribution work alone.
- SSH to the physical Mac (if step 1/2/3 needs real hardware, not just CI):
  `rohansharma@192.168.101.186`, key
  `J:\Claude code\iolab-m7-wt\.m7-ssh\iolbox_mac_m0` — verify the host key
  via `ssh-keyscan` before trusting a possibly-stale IP (known good key:
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`).
  `limactl` at `/opt/homebrew/bin/limactl`; `stop`/`delete` need
  `--tty=false` and `< /dev/null`.
- Protected VMs, never touch: `iolbox-m5-e2e`, `iolbox-m7-native-arm64-qemu`
  (the latter did not exist on the Mac as of the last session — confirmed
  absent before anything else was touched, not deleted by this arc).
  `iolbox-m4-e2e` is the owner-approved test VM name for M4-harness-style
  hardware runs; it was left Stopped (not deleted) at the end of the last
  session, with real evidence still on disk at `~/.lima/iolbox-m4-e2e`.
