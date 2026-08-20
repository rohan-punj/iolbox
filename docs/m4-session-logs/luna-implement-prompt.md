Implement and run M4 of the Apple Silicon macOS track for iolbox, in
J:\Claude code\iolab-m4-wt on branch luna/macos-m4-runtime, using luna at
xhigh reasoning.

## Resuming after a resolved provisioning blocker

Your immediately prior attempt (this session) got as far as creating
`iolbox-m4-e2e`, booting it, and passing its supervisor/Rosetta canary, then
correctly stopped and reported BLOCKED before item 1: `iolbox-m3-e2e` was
Running and holding host port 4001, and when you tried an alternate port
(4011) the guest verification step checked the hardcoded port 4001 instead
of the configured port — a real bug, not a hardware limitation.

Both are now resolved:
- The owner explicitly approved stopping `iolbox-m3-e2e` (confirmed idle: no
  evidence writes in 30+ minutes, no other active session) precisely because
  it is listed as disposable/reusable in `docs/macos-m4-prompt.md`. It has
  been stopped (not deleted) over SSH and `limactl list` now shows every VM
  Stopped except your preserved, still-Stopped `iolbox-m4-e2e`. Host port
  4001 is free again — reconfirm this yourself before relying on it.
- Regardless of the port now being free, fix the hardcoded-4001 guest
  verification bug you found before continuing — do not leave a harness that
  only works by coincidence of port reuse. Find wherever the verification
  step checks readiness/port and make it use the actually configured port
  (env var / flag / discovered value), the same way `m4Runtime.fixture`
  already reads `IOLBOX_M4_FIXTURES` rather than hardcoding a path.

Continue provisioning `iolbox-m4-e2e` on host port 4001 (now free) using your
existing preserved VM and evidence tree under `~/iolbox-m1/evidence-m4/` on
the Mac — you do not need to recreate the VM from scratch if it is still in
a valid, verifiable Stopped state. Re-verify its state and canary before
trusting it. Proceed through the full plan v2 hardware sequence from item 1
onward.

## Resuming from a prior interrupted attempt in this same worktree

A prior attempt (this session) already made real progress before being
stopped: it reached the Mac over SSH using `.m4-ssh/iolbox_mac_m0` (working —
keep using it, still gitignored, still never referenced from a committed
file/script), fixed a Windows-compile bug in the shared ping type, ran local
preflight, and was about to record the live Mac baseline. It also correctly
noted `docs/macos-m4-plan.md`'s frozen hash as `9897A1D1...4579DD` at that
point.

**That hash is now stale on purpose.** The owner has just directed a scope
change directly in `docs/macos-m4-plan.md`: item 6 (the sustained soak) is
now **10 minutes (600 seconds)**, not two hours (7,200 seconds). This is
recorded explicitly near the top of the plan file under "Owner-approved scope
deviation," and every duration/checkpoint/sample-count reference in the
item-6 rows, the isolated-soak protocol section, and the RAM-wall/ordering
text has already been updated to match (600 s, checkpoints at 0/2.5/5/7.5/10
minutes, ≥10 traffic summaries, ≥11 resource samples, final-minute traffic
requirement, 60 s max heartbeat gap). Treat the CURRENT content and hash of
`docs/macos-m4-plan.md` as the new frozen baseline for the rest of this
session — recompute and record its hash now, and use that recorded hash for
your own scope-gate check at the end, not the stale one from before. The file
remains read-only/frozen; do not edit it further.

Continue from where the prior attempt left off rather than restarting from
nothing: reuse its harness code, fixture files, and preflight evidence
already on disk, but update every place that still encodes the old 7,200 s /
two-hour soak duration or its derived checkpoint/sample-count values (for
example the `m4SoakSeconds = 7200` constant and the "shorter than 7200
seconds" error text in `tools/iolab-launcher/macos_m4_runtime_darwin_test.go`,
plus any doc/comment referencing "two-hour") to the new 600 s figures so the
code and the plan agree. Verify the Windows-compile fix for the shared
Darwin-only ping type actually took (re-run `go build ./...` and
`go vet ./...` for both darwin/arm64 and windows/amd64 targets) before
proceeding to hardware.

## Read first, in this exact order, before doing anything else

1. docs/macos-m4-plan.md — **"M4 implementation plan v2 (final)"**. This is
   your governing execution plan, already adversarially reviewed. Follow it
   exactly, including its execution ordering, evidence contract, RAM-wall
   state machine, soak isolation/seal requirements, and completion-gate
   verifier. Treat this file as frozen/read-only input, not something you
   edit.
2. docs/macos-m4-plan-review.md — the adversarial review that shaped v2, for
   context on *why* each gate exists.
3. docs/macos-m4-prompt.md — the original scope/requirements prompt. Where
   plan v2 and this prompt could be read as disagreeing, plan v2 wins (it was
   built from this prompt plus the M3/M1 docs and already resolves the
   ambiguities).
4. docs/macos-m3-handoff.md, docs/macos-m3-result.md, docs/macos-m1-handoff.md
   — prior-milestone gotchas still in force.

## What you are actually doing

This is a real hardware qualification run, not a code-review exercise. You
have SSH access to the target Mac. IMPORTANT: the key under `~/.ssh/` is not
readable from inside this sandbox (a prior attempt confirmed
`Permission denied` loading it, even with the `.ssh` directory added via
`--add-dir` — this is a Windows sandbox ACL restriction on that specific
file, not a missing grant). A working copy of the same key has been placed
inside this worktree, which the sandbox already owns for read/write, at:

  .m4-ssh/iolbox_mac_m0   (chmod 600, gitignored via .gitignore)

Use it exactly like this:

  ssh -i ".m4-ssh/iolbox_mac_m0" rohansharma@192.168.101.166

This copy must never be committed (it is already in .gitignore — verify that
stays true) and must never be referenced from any committed file, script, or
doc; only use it as an ad hoc `-i` argument during this session's hardware
commands.

Homebrew/Lima live at /opt/homebrew/bin/ and are NOT on the non-login PATH —
always use /opt/homebrew/bin/limactl explicitly. Bash on the Mac is 3.2.57
only (no bash 4+ syntax). `iol22` is irreplaceable M0 evidence — never start,
stop, modify, or delete it, under any circumstance, including RAM-pressure
reclamation. `iolbox-m3-e2e` was observed Running (not Stopped) at last
check — this branch has a documented history of concurrent sessions, so
follow plan v2's preflight (record git log/status, record `limactl list`,
reconcile before touching anything) before assuming ownership of any VM.

Build the M4 evidence/test harness as instructed by plan v2 (stdlib-only Go,
reuse tools/iolab-launcher's wsDialWithSession, m3ReadPrompt/
m3SendConcurrently, the browser-equivalent HTTP/WS pattern — do not
reinvent them). Stage it to the Mac, then actually execute the full
hardware sequence plan v2 specifies:

VPCS/IOL end-to-end -> multi-link short run -> NAT -> extnet disposition ->
fresh isolated 2-hour soak -> four-node -> forced-termination recovery ->
final record — and produce docs/macos-m4-result.md from the verified
evidence, following plan v2's per-item bars and its machine-verifiable
completion gate (summary.json + record-verifier).

The 2-hour soak is long. Once its fixture is loaded and traffic/capture are
confirmed healthy, you may continue working other plan items (NAT, extnet,
build/test work) per plan v2's ordering, but the soak's isolation rules in
plan v2 are absolute — do not run anything that could invalidate its
measurement window.

Your prior attempt this session already wrote `packaging/macos/tests/hardware-m4.sh`
and Go test files referencing `~/.ssh/iolbox_mac_m0` directly — before running
anything, update every such reference to accept the key path as a parameter
or environment variable (do not hardcode either path), and pass
`.m4-ssh/iolbox_mac_m0` at invocation time this session. Do not hardcode the
`.m4-ssh` path into any committed script either — it's a local, gitignored,
session-only credential location, not a repo convention.

## Non-negotiable constraints (repeated because they are the most commonly
## silently dropped)

- Never re-encode profiles.env in Go; read it as data.
- Compare `(profile, product, build)` by exact string equality, never
  numeric version comparison.
- NDJSON: string `id`, correlate by id, `ok:true` means understood not
  succeeded; YAML `lab.saveDoc` vs JSON `lab.load` asymmetry is real.
- Readiness is `GET /` < 500, never liveness/active/running.
- Every WS route (`/control`, `/console/{id}`, `/capture/{id}`) needs a
  session cookie + same-origin Origin header — reuse wsDialWithSession.
- IOS needs an active console wake (`\r\n` on connect, periodic re-poke).
- `ram: 256` wedges modern IOL images — use 1024 MB minimum in any fixture.
- Stdlib-only Go, gofmt'd. `stop` never deletes guest or host data.
- Never `git add -A` or `git add .` — stage your own exact file list by path.
- Out of scope, do not touch: M5/M6/M7, and the frozen result/plan docs
  listed in docs/macos-m4-prompt.md's "Out of scope" section, and
  docs/macos-m4-plan.md itself (read-only per plan v2's scope gate).

## If you hit the RAM wall on four-node qualification (item 5)

Follow plan v2's deterministic RAM-wall state machine exactly: the owner has
already pre-approved attempting the ordered reclamation pass over the
disposable VMs (m1jammy, m1trixie, iolbox-m1-e2e, iolbox-m2-e2e,
iolbox-m3-e2e — never iol22, never the M4 VM, and only after confirming none
is owned by concurrent activity) before one cold retry. If the retry still
fails, mark item 5 and overall M4 BLOCKED/UNVERIFIED per plan v2 and stop —
do not silently reduce scope, do not fabricate a pass.

## Deliverables

- The M4 test/evidence harness code (new files only, under
  tools/iolab-launcher and/or a dedicated M4 evidence tooling path — follow
  plan v2's expected file list).
- Raw evidence tree on the Mac host as plan v2 specifies (summary.json,
  NDJSON/CSV samples, console transcripts, control responses, pcapng,
  README.txt, SOAK-COMPLETE seal manifest).
- docs/macos-m4-result.md: what ran on real hardware vs. what only compiled
  or was unit-tested, every item's disposition (PASS/FAIL/BLOCKED/
  UNVERIFIED/NOT EXERCISABLE with evidence), and any criterion you could not
  verify and why — same honesty bar as M1/M2/M3.

Do not commit. Report back what changed, what passed on hardware, what
didn't, and exactly which files you touched (for the main session to review
and stage/commit).
