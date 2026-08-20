Read J:\Claude code\iolab-m4-wt\docs\macos-m4-prompt.md in full — it is the
self-contained implementation prompt for M4 of the Apple Silicon macOS track
for iolbox. Follow its "Read first" list in order (macos-m3-handoff.md,
macos-m3-result.md, macos-m1-handoff.md, macos-arm64-plan.md §M4) before
writing anything.

You are running as the PLANNING pass (sol, medium reasoning). Do not
implement or edit any product/runtime code. Your job is to produce a
detailed, actionable implementation plan for M4 as a markdown file at
J:\Claude code\iolab-m4-wt\docs\macos-m4-plan.md covering:

1. Exact hardware sequencing for the 8 qualification items in the prompt's
   "Scope" section, in an order that respects the RAM-budget risk (item 5,
   four IOL nodes) and lets the 2-hour soak (item 6) start as early as
   possible and run in the background while other items proceed.
2. For each item: what exactly will be run/observed, what "recorded with
   real numbers" means concretely (which metrics, how captured), and the
   pass/fail bar.
3. Where the existing M1/M2/M3 test harness (tools/iolab-launcher, the
   browser-equivalent E2E pattern, wsDialWithSession, m3ReadPrompt/
   m3SendConcurrently) can be reused vs where new test/record code is
   needed — per the prompt, this is a test/record phase, "product/runtime
   files only for failures actually observed."
4. A concrete plan for freeing RAM headroom on the 8GB Mac if item 5 (four
   IOL nodes) hits a hard wall: which of the disposable VMs (m1jammy,
   m1trixie, iolbox-m1-e2e, iolbox-m2-e2e, iolbox-m3-e2e — never iol22) to
   stop/delete and in what order, before escalating to the owner.
5. Explicit call-outs of every hard requirement from the prompt's "Hard
   requirements carried from M1/M2/M3" section as constraints the
   implementer (luna) must respect, so they aren't silently dropped.
6. A file-by-file list of what you expect to change or add (mostly under
   tools/iolab-launcher and docs/), respecting the "Out of scope — do not
   touch" list verbatim.

Write the plan to J:\Claude code\iolab-m4-wt\docs\macos-m4-plan.md and stop.
Do not touch git. Do not implement.
