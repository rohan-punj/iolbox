You are running the ADVERSARIAL REVIEW pass (sol, medium reasoning) for M4 of
the Apple Silicon macOS track for iolbox, in J:\Claude code\iolab-m4-wt.

Read, in order:
1. docs/macos-m4-prompt.md — the self-contained implementation prompt.
2. docs/macos-m4-plan.md — the plan just produced by a prior sol pass. This is
   what you are reviewing.
3. docs/macos-m3-handoff.md, docs/macos-m3-result.md, docs/macos-m1-handoff.md,
   docs/macos-arm64-plan.md §M4 — the same source docs the plan was built from.

Your job: find every way the plan could let luna (the xhigh implementer) ship
an M4 that looks complete but isn't. Be genuinely adversarial — assume the
implementer will take the path of least resistance under time pressure unless
the plan closes it off. Specifically check:

- Does the plan actually force real hardware numbers for every one of the 8
  scope items, with no way to satisfy a bar via compile/unit-test/code-review
  evidence alone?
- Does the execution ordering actually protect the 2-hour soak's measurement
  window from being invalidated by other items running concurrently (NAT,
  extnet) or sequentially (four-node, forced-termination)?
- Is the RAM-wall handling concrete enough that luna can execute it without
  further judgment calls, and does it correctly protect iol22 and preserve
  the disposable VMs' evidence before deleting/reusing them?
- Are all 8 "hard requirements carried from M1/M2/M3" (readiness vs liveness,
  NDJSON id correlation, session cookie + Origin on every WS route, IOS
  console wake pattern, ram:256 wedging, stdlib-only Go, gofmt, stop never
  deletes data) each concretely enforced somewhere in the plan, not just
  mentioned?
- Does the plan risk violating "Out of scope — do not touch" (M5/M6/M7,
  frozen result docs, redesigning profile/port/sync/browser-equivalent
  patterns)?
- Are the completion checklist bars objectively verifiable from recorded
  evidence, or do any of them rely on luna's own unverified say-so?
- Any gaps in the extnet "not exercisable" honest-failure path — could it be
  used to silently skip real work?
- Any missing failure-recovery guidance if the soak crashes/hangs partway,
  or if the Mac reboots/sleeps during the 2-hour window?

Do NOT edit macos-m4-plan.md directly. Write your findings to
J:\Claude code\iolab-m4-wt\docs\macos-m4-plan-review.md as a numbered list of
concrete required fixes (not vague concerns — each item must say exactly what
in the plan is insufficient and what change would close the gap), followed by
a short section of things the plan already does well and should not be
changed. Do not implement or touch git. Stop after writing the review file.
