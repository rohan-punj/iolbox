# iolbox P1 dispatch — wave 4+ kickoff

Paste the section under **PROMPT** into a new session.

---

## PROMPT

Continue the **iolbox learning-tool nodes P1** implementation. Repo: `J:\Claude code\iolab`
(git, folder named `iolab`, product `iolbox`), branch **`feat/learning-tool-nodes`**
off `main` (NOT merged — stay on this branch, do not merge to `main` without asking).

**READ FIRST:** memory `[[iolab-learning-tools-p0]]` (full P0 history + P1 kickoff),
then `docs/p1-dispatch-plan.md` in full — it is the authoritative execution plan for
P1: 17 batches (B01–B17) across 6 waves, each with an exact `codex exec luna-xhigh`
scoped prompt in its §5. `docs/learning-tools-nodes-plan.md` §P1 (tasks T1.1–T1.12)
is the underlying design those batches implement — final, do not re-derive or
re-litigate it. `docs/learning-tools-nodes-spec.md` is background only.

### Where things stand

P0 is closed (T0.1–T0.9 all pass on real hardware, `docs/tests/p0-spike.md`).

P1's dispatch plan went through a real adversarial review cycle before any code was
written: an Opus agent drafted the wave/batch structure, `codex sol-medium`
adversarially reviewed it and found **15 real defects** (not nitpicks — e.g. the
original plan would have let the new subreaper loop steal exit-status from
*existing* IOL/VPCS/capture processes outside the tool-node feature entirely, and
froze an env-var name that didn't match the already-proven P0 stub GUI, which would
have silently broken the P1 acceptance gate). All 15 were resolved by revising
`docs/p1-dispatch-plan.md` to **revision 2** before dispatching a single implementation
agent. A second sol-medium re-review of revision 2 was **skipped by explicit owner
instruction** ("skip the sol review and start implementation") — so revision 2 has
had one full adversarial pass, not two. Keep that in mind if something in a later
wave looks structurally off; it wasn't re-checked a second time.

**Waves 1–3 are landed and committed** on `feat/learning-tool-nodes`:
- Wave 1 (`fab859a`): B01 `tool.go` frozen contract, B02 lab/schema/protocol wire
  types, B03 `tools/iolbox-toollaunch` cap-transition binary.
- Wave 2 (`a2bc01f`): B04 manifest, B05 instance/state-file, B06 cgroup cage, B07
  netns/veth, B08 launcher wrapper, B09 packaging/rootfs, B10 (registers every
  *pre-existing* direct `exec.Cmd` child — IOL/VPCS/capture/extnet/fabric — with
  `tool.Registry`, closing the sol-medium-found subreaper hole before the reap loop
  itself existed).
- Wave 3 (B11 reap loop + `ReapStale`, B12 endpoint lifecycle, B13 `Detect`) —
  **dispatched but not yet confirmed landed as of this handoff being written.**
  Check `git log --oneline -5` first. If it's not committed yet, the three agents
  may still be mid-run or may have finished without being integrated — check
  `git status` in the repo root: if `supervisor/internal/tool/reap_linux.go` (or
  similar B11 files), `endpoint_linux.go`, and `detect_linux.go` exist as untracked
  files, run the same integration procedure described below before proceeding to
  wave 4.

### The model loop (owner-confirmed this session — use exactly this, no other model)

- **Plan revisions / structural changes to the dispatch plan itself** → **Opus**
  agent (via the `Agent` tool, `subagent_type: "Plan"`, `model: "opus"`,
  `run_in_background: false` so you get the result inline — Opus agents in this
  harness cannot write files directly when run this way; they return the full
  document text in their response and the orchestrating session writes it to disk).
  Long documents get truncated mid-response; if that happens, send a **second**
  agent call asking for exactly the missing continuation (quote the exact tail you
  already have so the new call can pick up the join point precisely — this
  happened once already this session and worked cleanly), rather than trying to
  reformat/rebuild it yourself.
- **Adversarial review of the dispatch plan** → `codex sol-medium` via `codex exec`
  CLI (NOT the MCP transport), `--sandbox read-only`,
  `-m gpt-5.6-sol -c model_reasoning_effort='"medium"'`,
  `--output-last-message <file>`. This is genuinely slow — real runs this session
  took **35–45 minutes of wall-clock time** for a ~700-line plan doc against ~15
  files. Launch it in the background (`&` + redirect stdout to a log file), then
  poll with a **real** `sleep`-based loop in a backgrounded Bash call (`run_in_background: true`)
  rather than relying on `ScheduleWakeup` delays alone — this session observed
  `ScheduleWakeup`'s requested delay not corresponding reliably to real elapsed
  wall-clock time, which made "is it stuck or just slow" hard to judge from
  `ScheduleWakeup`-driven checks alone. A background Bash poll with a real `sleep 15`
  loop, checked via `tasklist | grep codex` for liveness (memory-usage growth =
  still working) and file mtime (not just size) for progress, is the reliable
  signal.
- **Implementation, one batch per agent, one commit-sized unit per wave** →
  `codex exec` `-m gpt-5.6-luna -c model_reasoning_effort='"xhigh"'`,
  `--sandbox workspace-write` (NOT read-only — these agents write real files),
  same `--output-last-message <file>` pattern. Dispatch every batch in a wave
  **concurrently** (background each `codex exec` call with `&`, then a single
  `wait` at the end of the shell script so one `run_in_background: true` Bash
  call notifies you once the *whole wave* is done) — this is how waves 1–3 were
  run and it worked cleanly with zero file or symbol collisions across 3+7+3 = 13
  concurrent agents so far.
- Each batch's exact prompt = the shared preamble (`docs/p1-dispatch-plan.md` §5,
  "Shared preamble") **prepended** to that batch's own `### BNN — ...` section.
  Extract both with `sed -n 'START,ENDp' docs/p1-dispatch-plan.md` and `cat` them
  together into a prompt file before invoking `codex exec` — do not paraphrase or
  summarize a batch's prompt, use it verbatim, it was written to be precise enough
  that an agent with zero other context can implement it correctly.

### Post-wave integration procedure (do this after every wave, before dispatching the next)

1. `git status --short` in the repo root — confirm every wave's batches touched
   disjoint files (this has held for all 13 agents dispatched so far; a real
   collision would show as the same file modified by two batches, which cannot
   happen if OWNS lists were disjoint — but verify, don't assume).
2. Delete any stray `.gocache*` directories agents create as a workaround for
   concurrent `go build` cache contention (this happened in wave 2; `.gitignore`
   now has `.gocache*/` to keep them out of `git add -A`, but they still exist on
   disk and should be cleaned up so they don't pile up).
3. From `J:\Claude code\iolab\supervisor`: `go build ./...`, `go vet ./...`,
   `go test ./...` (native — this is the real regression gate for the ~90% of the
   codebase that isn't Linux-only), then `GOOS=linux GOARCH=amd64 go build ./...`
   and `GOOS=linux GOARCH=amd64 go vet ./...` (this is the real gate for the
   `_linux.go` files — the native build does not compile them at all, so a batch
   that "passed its own gates" can still be broken here if two batches' `_linux`
   files interact in a way neither agent could see in isolation; this happened
   once in wave 2 — B10 reported the Linux build blocked by a symbol from B06 that
   simply hadn't finished landing yet when B10 ran its own check mid-flight; it
   resolved cleanly once everything was actually merged, but **verify this
   yourself after every wave, don't trust an individual agent's self-reported
   gate status as the final word** once other concurrent agents' work is merged in).
4. From `J:\Claude code\iolab\tools\iolbox-toollaunch`: `go build ./...`,
   `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=amd64 go vet ./...`
   (its own separate module).
5. Spot-check that critical sol-medium-fix details actually landed as written —
   e.g. after wave 1, `grep IOLBOX_TOOL_OPT` and `grep NetnsExecArgs` in
   `tool.go` to confirm the env-var rename and the frozen-contract function
   actually exist as specified, rather than trusting the agent's prose summary.
6. Commit the wave with a message that names which batches landed and briefly why
   each matters (see the wave-1/wave-2 commit messages, `fab859a`/`a2bc01f`, for
   the level of detail expected — this is a real audit trail for a security-
   adjacent feature, not boilerplate).
7. Extract the next wave's batch prompts and dispatch.

### Immediately after wave 3 lands

Wave 4 is **B14 only** (single agent — old B13 was merged into it during the
sol-medium revision, so wave 4 has no parallelism to exploit). Extract its prompt
(`docs/p1-dispatch-plan.md`, search `### B14`), dispatch it alone, integrate, commit.

Then wave 5 is B15 + B16 (2 parallel agents), then wave 6 is B17 alone (gate
harness — deliberately dispatched last, after wave 5's integration build is green,
because a gate harness written against a plan document rather than landed code
would assert on names that might not exist).

### After B17 lands: the real acceptance gate

`go test`/`go vet` passing is necessary but **not sufficient** — P0's entire lesson
(7 real bugs, none caught by static review, all caught by live-hardware execution)
applies here too. Once wave 6 lands, run the actual `docs/tests/p1-gate.sh` (or
whatever B17 named it) against a real target — the same appliance VM used for P0
(`J:\iolab-v041-smoke\iolbox-v041-smoke\iolbox-v041-smoke.vmx`, `192.168.226.233`,
`vmrun` access, `MSYS_NO_PATHCONV=1` + `-T ws` required, see
`[[iolab-learning-tools-p0]]` for the full VM-access gotcha list). Confirm you have
a **lab-free** box before running anything that touches `iolbr0` — check for the
owner's own running lab first and ask before stopping it, exactly as was done to
close T0.9. "Gate to P2" (the acceptance criteria) is defined in
`docs/learning-tools-nodes-plan.md` right after the P1 task list — `lab.load`+
`lab.start` → node reaches `running`; hot link connect/detach with no restart;
`kill -9` the supervisor + restart → `ReapStale` removes the cage/netns/veth with
no zombie; `lab.stop` leaves nothing behind.

If anything fails on real hardware that passed `go test`, treat it exactly like a
P0 bug: capture live evidence yourself (the orchestrating session, not an agent —
none of the codex/Opus agents have VM access), then hand a scoped `luna-xhigh`
agent the precise diagnosis to fix, redeploy, re-run. Do not skip straight to "the
code looks right, ship it."
