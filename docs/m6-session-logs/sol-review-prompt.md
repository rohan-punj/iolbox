Adversarially review `docs/macos-m6-plan.md` (M6 of the Apple Silicon macOS
track for iolbox), in `J:\Claude code\iolab-m6-wt` on branch
`luna/macos-m6-followups`. This is a **review pass only** — do not implement
anything, do not edit the plan yourself. Your deliverable is a plain-text
review in your final message, plus a written file `docs/macos-m6-plan-review.md`
listing every finding with a severity (CRITICAL/MAJOR/MINOR/NIT) and a clear
description of the defect and, where obvious, the fix.

## Context

`docs/macos-m6-plan.md` was just produced by a separate sol-medium planning
pass, based on `docs/macos-arm64-plan.md` §M6 (the canonical scope), the M1-M5
handoff/result docs, and the actual current contents of `.github/workflows/`,
`docs/INSTALL.md`, and `packaging/macos/`. A `luna-xhigh` implementation
session will follow your review and execute whatever the plan (as corrected
by your findings) says, including real Apple Silicon hardware qualification
against `rohansharma@192.168.101.166` using key `.m5-ssh/iolbox_mac_m0`.

Read, in this order: `docs/macos-m6-plan.md` (the plan under review),
`docs/macos-arm64-plan.md` §M1-M7 (the canonical source of truth the plan
must not deviate from without a stated reason), `docs/macos-m5-handoff.md` +
`docs/macos-m5-result.md`, `docs/macos-m4-handoff.md` + `docs/macos-m4-result.md`,
and the actual current `.github/workflows/ci.yml`, `.github/workflows/release.yml`,
`docs/INSTALL.md`, `packaging/macos/` tree, and `tools/iolab-launcher/macos_*.go`
asset-lookup logic the plan claims to be building on.

## What to adversarially check

1. **Scope fidelity**: does every task in the plan actually trace back to
   `docs/macos-arm64-plan.md` §M6's stated files/acceptance, with no silent
   scope creep (redesigning launcher behavior, touching M4/M5/M7 territory)
   and no silent scope-shrinkage (an §M6 acceptance bullet quietly dropped
   or weakened)?
2. **Grounding in real repo state**: does the plan's claimed archive layout,
   asset-lookup contract, existing CI/release job structure, and INSTALL.md
   structure match what is actually in the worktree right now, or does it
   assume/invent structure that isn't there? Spot-check the specific claims
   (e.g. "the launcher finds `lima/profiles.env` beside its executable and
   searches that same asset-root non-recursively for `iolbox-server-*.tar.gz`")
   against the actual Go source.
3. **Executability by a hardware-driving agent**: §5 requires "the owner
   creates and interactively logs into a disposable account" and "interactive
   desktop login for the real browser download and GUI work" — a `luna-xhigh`
   codex session drives the Mac over SSH with a non-interactive shell. Is this
   plan actually executable by an automated session, or does it have a hard
   dependency on human-in-the-loop action (creating a macOS user account,
   physically/remotely operating a GUI browser) that the plan doesn't flag
   clearly enough as a prerequisite the implementation session cannot itself
   satisfy? If so, say so as a finding — this needs to be visible, not buried.
4. **Internal consistency**: do the acceptance criteria in §8, the verbatim
   qualification steps in §7, and the risks in §9 agree with each other and
   with the task list in §3? Look specifically for criteria that reference
   evidence the task list never actually produces, or task-list steps whose
   output nothing in §7/§8 ever verifies.
5. **Determinism/reproducibility claims**: is the "two identical builds produce
   identical SHA-256" requirement in §2.3 actually achievable given `tar`/`gzip`
   default behavior (timestamps, member order, file metadata, uid/gid) on the
   CI runner's OS, or does it need explicit flags the plan doesn't call out?
6. **Security/safety**: anything in the Gatekeeper/quarantine handling (§4) or
   the destructive-delete step (§7.6) that could do something broader than
   intended if a real command differs slightly from what's written (e.g. an
   `xattr -d` or `limactl delete`/`rm`/Trash-move that could be typo'd into
   something recursive/broader) — flag any command that isn't maximally
   narrow/explicit as written.
7. **Anything a static read reveals as simply wrong or missing** — a task that
   can't actually be done as described, a file path that doesn't exist, a
   criterion that isn't falsifiable, a step order that can't work (e.g.
   depending on an artifact from a job that hasn't run yet).

## Working rules

- Sandbox may be read-only for this pass — you are not required to persist
  anything except the review file itself, so use `workspace-write` (not
  read-only) so `docs/macos-m6-plan-review.md` actually gets written; a prior
  M3 planning pass lost its entire output when it ran read-only and the write
  was rejected. Do not touch any other file.
- Be genuinely adversarial: your job is to find real defects before a
  hardware session burns time on them, not to rubber-stamp the plan. If you
  find nothing wrong in a section, say so explicitly rather than skipping it
  silently.
- Rank findings by severity and be specific: file/section reference, the
  exact defect, and (where obvious) the fix.
