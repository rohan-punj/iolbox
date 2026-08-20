Implement **M6** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab-m6-wt` (worktree on branch `luna/macos-m6-followups`,
rebased onto `luna/macos-m5-honest-caps` @ `7b7b6ec`, itself on top of
`b5ff742` which added this milestone's plan). Use full xhigh reasoning. This
is an implementation session, not a planning session — write real code, real
workflow files, real docs, and actually run the qualification steps you have
access to.

## Read first, in this order

1. `docs/macos-m6-plan.md` — **the authoritative plan for this session.**
   Follow its task breakdown (§3), archive layout (§2.2), determinism recipe
   (§2.3), documentation contract for Gatekeeper (§4), evidence/status
   semantics (§6), verbatim qualification steps (§7), acceptance criteria
   (§8), and non-goals (§10). Where anything below in this prompt conflicts
   with the plan, the plan wins except for the one scope deviation named next.
2. `docs/macos-m6-plan-review.md` — the adversarial review that shaped the
   plan; useful context for *why* certain sections are written the way they
   are (e.g. why §7 is split into AGENT-SSH vs OWNER-GUI steps, why there's a
   separate baseline/candidate design).
3. `docs/macos-m5-handoff.md` and `docs/macos-m5-result.md` — M5 is **fully
   PASS**, including criterion 2 (closed on `noble-builder-vm`). Nothing in
   M5 is open. Do not describe M5 as PARTIAL or blocked in anything you write.
4. `docs/macos-m4-handoff.md` / `docs/macos-m4-result.md` — M4 remains
   PARTIAL (items 2, 7 fixed-but-not-hardware-reconfirmed; items 3, 4, 5, 8
   never attempted). This is explicitly **out of scope** for M6 (plan §9
   risk 1, §10 non-goal) — do not chase it. If an M4-era defect blocks an M6
   criterion during your own qualification pass, root-cause and record it,
   fix only what's needed to unblock M6, and move on.
5. `docs/macos-arm64-plan.md` §M6 (and §M1-M5, §M7 for context) — the
   immutable master scope definition the plan itself is built from.

## Owner-approved scope deviation from plan §0

The plan's §0 pre-implementation gate calls for a genuinely fresh macOS user
account plus either agent-controlled interactive desktop/browser access or
the owner personally performing the labelled **OWNER-GUI** steps. **The
owner has explicitly approved skipping that gate for this session**: qualify
against the existing `rohansharma` account on `192.168.101.166` (the same
account M1-M5 used) instead of a fresh account, and perform every step —
including the browser-download and Gatekeeper steps the plan labels
OWNER-GUI — however the automated session actually can (e.g. via whatever
browser-equivalent HTTP/curl proof M3/M5 already established is acceptable
evidence when a real interactive desktop isn't available, same honesty bar
as those sessions: if a step genuinely requires a rendered browser you don't
have, do not fake it — mark that specific criterion NOT RUN and say exactly
why, do not silently substitute a weaker check without saying so).

This is a **known, recorded deviation, not silent scope reduction**:
- Do not claim "clean machine" in your result doc. State plainly that this
  session ran against the pre-existing `rohansharma` account with pre-existing
  Lima machines from M1-M5 present, not a fresh account, per explicit owner
  direction, and name exactly what that weakens (no true isolation from prior
  session state; no proof of a genuinely first-time user's experience).
- Still follow the plan's evidence discipline for whatever you *can* observe
  on this account: real download, real checksum verification, real quarantine
  attribute inspection, real `./iolbox start`, real IOL lab, real upgrade,
  real uninstall/recovery/destructive-delete sequence over SSH.
- If the browser-download and Gatekeeper criteria (plan §8 rows 3-5) truly
  cannot be exercised without a rendered browser session you don't have
  access to, mark them NOT RUN with a clear one-line reason — do not invent a
  passing result for a step you didn't actually perform. Check first whether
  this session actually has a browser control surface available (the M5
  continuation session did, via an SSH tunnel + DOM assertions) before
  assuming it doesn't — if you do have one, use it and get real evidence,
  same discipline as M5 §2 in `docs/macos-m5-handoff.md`.

## Access you have for this session

```text
SSH host: rohansharma@192.168.101.166
key:      J:\Claude code\iolab-m6-wt\.m5-ssh\iolbox_mac_m0
          (from Windows: ssh -i ".m5-ssh/iolbox_mac_m0" rohansharma@192.168.101.166)
Mac Lima: /opt/homebrew/bin/limactl (not on the default SSH PATH — always use the full path)
```

This is the same SSH key/host M1-M5 used, copied into this worktree already
(`.m5-ssh/iolbox_mac_m0`, gitignored, do not commit it). You have
`workspace-write` sandbox access in this worktree and network access to reach
the Mac over SSH — use it to actually run the qualification commands in plan
§7 (the AGENT-SSH-labelled ones at minimum; attempt the OWNER-GUI-labelled
ones too per the scope deviation above, honestly reporting what you could and
couldn't actually prove).

`iol22` is untouched, always. Other Lima machines on this Mac
(`iolbox-m1-e2e` through `iolbox-m5-e2e`, `m1jammy`, `m1trixie`) belong to
prior sessions — check `top -l 1 -s 0 | grep PhysMem` before starting a new
VM (8 GB Mac), stop (never delete) other machines only if genuinely needed
for memory headroom, and leave everything you don't need to touch exactly as
found.

You will need one real, legally-held x86_64 IOL image for plan §7.3/§8 row 6.
If none is reachable from this session, say so plainly and mark that
criterion NOT RUN rather than fabricating lab evidence — do not use a
synthetic/placeholder image and call it a real IOL run.

## Working rules (same discipline as M2-M5)

- Real hardware or it didn't happen. A packer/CI check that only runs in a
  unit test with mocked inputs does not satisfy an M6 acceptance criterion
  that calls for real-hardware or real-workflow evidence — build the actual
  GitHub Actions changes, actually trigger/validate them (via
  `workflow_dispatch` dry run per plan Task 3 if you have `gh`/API access
  from this session, otherwise document exactly what you could and couldn't
  trigger and why), and actually run the produced artifact on the Mac.
- Never `git add -A` or `git add .` in this shared worktree — stage the exact
  file list by path, every time. Check `git status`/`git log` before staging
  anything, per plan Task 6.1 — this project has hit genuine concurrent-work
  collisions in shared worktrees more than once (see M3's finding and M5's
  handoff §7 note about the last-minute dist/index.html cleanup); assume it
  can recur and re-check state at every commit, not just at the start.
- Do not touch `supervisor/internal/web/dist/index.html` unless you are
  intentionally running a real `npm run build:embed` immediately followed by
  `build-release.sh`'s placeholder-restore step (or leaving it untouched
  entirely) — M5 shipped a real-vs-placeholder mismatch in that exact file
  that had to be cleaned up after the fact; do not repeat it.
- Follow the plan's exact archive layout, manifest, determinism recipe, and
  destructive-command literalness (plan §2.2, §2.3, §7.6) — these sections
  exist because the adversarial review specifically found vague/incomplete
  versions of them unsafe or unverifiable. Do not simplify them back down.
- Do not do M7 work, do not chase M4's backlog, do not sign/notarize, do not
  install/manage Lima for the user — per plan §10.
- Write `docs/macos-m6-result.md` and `docs/macos-m6-handoff.md` at the end,
  using `docs/macos-m5-result.md`/`docs/macos-m5-handoff.md` as the format
  template (plan Task 7 / MINOR-4 fix already calls for this). Same honesty
  bar as every prior M-session: state plainly what ran on real hardware vs.
  what only compiled/unit-tested/dry-ran, per acceptance criterion in plan
  §8, with no rounding up — and explicitly call out the rohansharma-account
  deviation from plan §0 in both documents so a future session doesn't
  mistake this for a true clean-machine PASS.
- Commit your work when done, staging exact paths (never `-A`/`.`). Do not
  merge this branch anywhere or push. Leave the Mac in a sane state and
  record its final state (which machines running/stopped, what was installed
  where) in the result doc, same as M1-M5's own handoffs do.
