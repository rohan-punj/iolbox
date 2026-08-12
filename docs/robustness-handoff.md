# iolbox lab start/stop robustness — handoff status

Live log for a distinct thread from `redesign-handoff.md` (that doc covers the P5–P8 UI
redesign + netprobe/tool-naming fixes; this one covers a backend robustness investigation
into "sometimes lab start/stop doesn't work" reports). Started 2026-08-12 after the user
reported intermittent node-start failures and nodes not reliably going green.

## What triggered this

User report, with a screenshot: a node start failed with a red banner reading
`START NODE 0 FAILED: FABRIC IOUYAP /TMP/NETIO0/500: ...` (truncated in the screenshot),
plus "sometimes the nodes do not go green, sometimes node give this error." Ask was: get an
Opus-model deep review of the lab start/stop robustness, then get `codex exec` at
`gpt-5.6-sol` / `model_reasoning_effort=medium` ("sol-medium") to adversarially verify those
findings against the real code before trusting any of it — the two-lens review pattern this
user has used on other projects (see memory `trellis-lean-process-shape`).

## Process used (for repeating this pattern later)

1. **Opus deep review** (`Agent` tool, `model: "opus"`, `subagent_type: general-purpose`,
   run in foreground) — briefed with the two symptoms, a starting-point list of relevant
   packages (not asserted exhaustive), and an explicit "review only, no code changes, cite
   file:line, rate your own confidence" instruction. Took ~10 minutes, produced 9 ranked
   findings + minor observations + suggested regression tests. Full text preserved at
   `scratch/opus-robustness-findings.md` (this repo's `scratch/` is an untracked, git-ignored
   scratch area used across many past sessions — not committed, but has survived so far;
   don't rely on it surviving indefinitely, the essential content is folded into this doc).
2. **codex sol-medium adversarial critique** — `codex exec -m gpt-5.6-sol
   -c model_reasoning_effort=medium -s read-only --skip-git-repo-check "<prompt>" < /dev/null
   2>&1 | tee scratch/codex-sol-medium-critique.txt`, run via `Bash` with
   `run_in_background: true` from `J:\Claude code\iolab` (codex requires cwd to be exactly
   the git repo root, not a parent — `--skip-git-repo-check` alone isn't enough, it still
   error'd with "not inside a trusted directory" from the parent dir).
   **Gotcha that cost ~40 minutes the first time**: even with a prompt given as a positional
   argument, `codex exec` ALSO waits to read stdin to EOF (per `codex exec --help`: "if stdin
   is piped and a prompt is also provided, stdin is appended as a `<stdin>` block"). A
   backgrounded `Bash` command leaves stdin as an open, never-closing pipe, so the process
   hangs forever waiting for EOF that never comes — confirmed via `Get-Process` showing ~0.06
   CPU-seconds accumulated after 37 minutes (i.e. genuinely blocked on read, not slow). Fix:
   always append `< /dev/null` to `codex exec` invocations run through a backgrounded Bash
   tool call. The retry (with `< /dev/null`) completed correctly in a few minutes.
3. Model name resolution: this user's codex CLI doesn't have a model literally named `sol` —
   the working id is **`gpt-5.6-sol`** (found by trial: `gpt-5-sol`, `sol`,
   `gpt-5-codex-sol`, `gpt-5.1-sol` all 400 "not supported"; `gpt-5.6-sol` resolved cleanly).
   Codex's own default profile model is `gpt-5.6-luna` at `medium` effort — so `luna` and
   `sol` are sibling model names on the `5.6` line, not different effort tiers of the same
   model.

## Finding #1 — FIXED and deployed (commit `b0e5aa8`)

**This was the exact symptom reported.** Root cause, confirmed both by Opus's static
analysis and a live reproduction on the VM (`192.168.111.154`) before the fix, then
disproven-by-fix-working after:

`startFabric` (`supervisor/internal/server/fabric_linux.go`) runs a whole-lab static-tap
walk on *every* `node.start`, and its "is this tap already realised" skip check compared
only **netio paths** (`ll.tapBridges[t.netioPath]`), never tap **identity**.
`computeStaticTaps` (`supervisor/internal/server/fabric.go`) allocates pseudo-instance netio
paths by walking IOL nodes in id order with a sequential counter — if that walk's shape
changes (node added/removed, or in this session's case, accumulated over a long-running
supervisor process across several test-node add/remove cycles), an existing IOL interface's
tap gets reassigned a *different* netio path while its *old* pump — still holding the tap's
fd open under the stale path key — is never found or closed. The next `startFabric` then
tries to open a *fresh* pump on a tap a live "ghost" pump already owns →
`TUNSETIFF <tap>: device or resource busy`, exactly the reported error.

Live-reproduced twice in a row with different paths/taps each time
(`/tmp/netio0/502: TUNSETIFF iol3_2`, then `/tmp/netio0/500: TUNSETIFF iol3_0` — same failure
class, map-iteration-order-dependent which tap hit it first).

**Fix**: `labBridge` (`supervisor/internal/server/bridgeplan.go`) now tracks its own
`tapName`. `startFabric`'s skip check requires the tap name to also match
(`hasPump && lb.tapName == t.tapName && tapDeviceExists(...)`), and before creating a new
pump it scans `ll.tapBridges` for any *other* entry bound to the same `tapName` under a
different (stale) path and evicts it first. A new end-of-loop sweep also evicts any
`tapBridges` entry whose path has dropped out of the current `ll.staticTaps` set entirely
(the plain node-removal orphan-leak case). Also fixed a related frontend issue: `startNode`
(`app/src/lib/labStore.svelte.ts`) had no lock-release path when the start RPC itself was
*rejected* (as this exact failure is, before any `node.state` event for that node can ever
fire) — the node stayed locked/stuck for the full 60s safety timeout. Now releases
immediately on RPC rejection.

**Verification**: `go build`/`go vet`/`go test ./...` clean, `svelte-check` clean. Deployed
to the VM (full `supervisor` binary — not just the `pc-gui` pack this time, since the fix is
in `supervisor/internal/server/`). Reproduced the bug once more pre-fix to confirm baseline,
then after deploying the fix a full 5-node lab start (PC0, PC1, SW2, SW2-2, Tool4) succeeded
cleanly with no error banner. Lab left stopped/clean afterward.

**codex sol-medium's independent verification of this fix** (not just the original
diagnosis): confirmed it fully addresses the EBUSY mechanism — covers both "same path, wrong
tap" and "same tap, old path" collision directions — and found no new risk (lock ordering is
fine, `TapBridge.Close` is `sync.Once`-protected against double-close). One thing it flagged
as *not quite right* in the original Opus writeup: the "randomized map iteration = coin
flip" explanation for intermittency was imprecise (the original code *skipped* wrongly-owned
paths rather than closing them, so map order didn't decide the outcome the way Opus implied)
— the real intermittency was accumulated topology *history*, which the live reproduction
already established regardless of the exact mechanism-of-imprecision.

**One gap still open from finding #1**: the fix closes orphaned *pumps* (sockets/fds) but
does not delete the removed node's tap *devices* — since they've already dropped out of
`ll.staticTaps`, a later full `teardownFabric` has no record to find them by. This is now a
kernel-resource leak (harmless taps sitting around), not an EBUSY risk (the owning fd is
closed), so it's lower priority — folded into the "remaining work" list below as item 8.

## Remaining findings — NOT yet fixed, both reviewers converged on this priority order

Both Opus (original) and codex sol-medium (adversarial pass) independently rank these the
same way once finding #1 is excluded. **None of this is committed yet** — this is exactly
where the next session should pick up, after confirming scope/priority with the user (they
were asked "proceed with #1-2 next, or scope differently?" at the end of this session with
no answer yet).

1. **Serialize lab mutations** (`supervisor/internal/server/`) — no lock protects
   `startNodes`/`stopNode`/`startFabric`/`teardownFabric`/`handleLabLoad`/link handlers
   across control connections. Root-causes a `NewTap` stat-then-bind TOCTOU
   (`iouyap/bridge_tap_linux.go:56-70`) and — most seriously — `handleLabLoad` publishing the
   new lab *before* tearing down the old one, with device names not lab-scoped, so a
   concurrent start against the new lab can have its devices deleted by the old lab's
   teardown by name. **Confirmed by both reviewers.** Concurrency sources: second browser
   tab, the loopback TCP control listener, fault timers (`time.AfterFunc` in
   `fabric_fault.go`/`fabric_fault_handlers.go`).
2. **Fix `ll.nodes`/`ll.doc` locking** (`supervisor/internal/server/handlers.go`,
   `loaded.go`) — `loadedLab.get` reads `ll.nodes` under `ll.mu`, but `handleNodeAdd`/
   `handleNodeRemove`/`handleLinkAdd`/`handleLinkRemove`/`refreshFabric` all mutate without
   the lock. A concurrent console-connect (`ConsolePort`/`ConsoleSubscribe`, per-connection
   goroutines) racing a `node.add`/`node.remove` is a genuine Go
   `fatal error: concurrent map read and map write` — **this crashes the entire supervisor
   process**, killing every running node, not just failing one action. codex sol-medium
   rates this the most severe of the remaining findings for exactly that reason.
3. **Make start operations transactional/idempotent** — `startNodes`
   (`handlers.go:546-657`) is sequential and returns on the *first* failure with no partial
   result reporting; earlier-started nodes stay started, later ones are never attempted, and
   the GUI's "Start all" then disables itself (`labRunning` = any node running/starting) with
   no easy recovery except Stop-all/Start-all or per-node clicks. Also: IOL/VPCS have no
   already-running guard (NAT/tool/PC do), so re-starting a partially-started lab aborts on
   "address already in use." **New from codex's pass**: NAT/tool/generic-VPCS start paths can
   report "start failed" while leaving the node's process/endpoint actually running (no
   rollback on a fabric-attach failure after spawn) — the PC node's own start path already
   does this correctly (stops both resources, marks crashed on attach failure), so there's a
   working pattern in this same codebase to copy for the other three kinds.
4. **Fix `TapBridge` pump half-duplex-death lifecycle**
   (`supervisor/internal/iouyap/iouyap.go`, `bridge_tap_linux.go`) — `pumpNetioToTap` (unlike
   `pumpTapToNetio`) returns hard on a tap write error instead of retrying; `Run` waits for
   *both* pump directions before cancelling, so if only one dies the bridge sits
   half-duplex forever; the launch site in `fabric_linux.go` discards `Run`'s error entirely
   (no log, no eviction from `ll.tapBridges`). Net effect: a node can show green with a dead
   data plane, unrecoverable short of a full lab stop.
5. **New from codex's pass — wait for process exit on stop/restart**
   (`supervisor/internal/node/spawn_linux.go:539`, `handlers.go:527`) — `Process.Stop` sends
   SIGKILL and returns without waiting for `cmd.Wait`/reaping; `node.restart` immediately
   calls `startNodes` right after `stopNode`. The old and new IOL process can briefly overlap
   for the same real instance. Whether this produces a reproducible NETIO conflict wasn't
   verified (would need a VM test or tracing `/tmp/netio<uid>/<instance>` during a tight
   restart loop) but the overlap window itself is certain and cheap to close with a bounded
   wait.
6. **Bounded/nonblocking event delivery**
   (`supervisor/internal/server/broadcast.go`, `internal/ws/ws.go`) — `publish` writes to
   subscribers synchronously from the emitting goroutine; no `SetWriteDeadline` anywhere in
   the `ws` package. A single wedged subscriber (stale tab, suspended laptop) can block every
   later node-state emission — i.e. `lab.start` hangs mid-loop — until the kernel's TCP
   retransmit timeout (minutes). Needs a bounded per-subscriber channel + dedicated writer
   goroutine that drops a subscriber that falls behind.
7. **Targeted bounded retries — only after 1-2 above, not instead of them.** codex pushed
   back hard on treating "no retry" as a standalone fix target: retrying an `EBUSY` caused by
   a still-live owner can mask the real ownership bug and may never succeed within any
   reasonable retry budget. Fix serialization/ownership first; retries are legitimate
   defense-in-depth for genuinely transient kernel timing *after* that.
8. **Delete orphaned tap devices for removed nodes** — the residual gap in the finding-1 fix
   noted above. Low priority (kernel-resource leak only, not correctness), but easy to fold
   in alongside item 1's mutation-lock work since it's in the same code path
   (`handleNodeRemove`/`teardownFabric`).

## Ruled out — do not re-investigate

- **Opus finding #8** ("`os.File.Close()` on the tap doesn't synchronously release the tun
  device") — codex traced Go's `internal/poll` semantics and found `openTap`
  (`iouyap/tap_linux.go:68`) calls `SetNonblock` before wrapping the fd, which makes
  `Close()` actually wait for the poller-held reference to drain before returning. The
  suggested "Close() returns but the tap is still busy" window does not exist for this
  code's actual fd construction. **Confirmed WRONG, don't chase this one.**
- **Opus finding #9** ("no retry, no backoff, anywhere") — literally false as stated: VPCS
  console-readiness polling, tap-to-NETIO read retries, and PC console dialing all already
  retry. Rated **OVERSTATED** by codex — real gap, wrong framing (see priority item 7 above
  for the corrected framing).

## Suggested regression tests (from Opus, not yet written)

1. Finding #1 (now fixed, but no test exists yet): load a 3-node lab, start it, `node.remove`
   the middle node, `node.start` a survivor; assert every `ifaceTap` in `ll.staticTaps` has a
   `tapBridges` entry whose bound tap name matches, and `len(ll.tapBridges)` equals the total
   live tap count.
2. Item 3: stub one node's `buildSpec` to fail; assert the other nodes still start and the
   response reports them.
3. Item 2/2 (mutation lock): run the existing server tests under `-race` with a goroutine
   hammering `status`/`ConsolePort` while `node.add`/`node.remove`/`lab.start` run
   concurrently — should surface the map-race crash directly.
4. Item 4 (TapBridge lifecycle): inject a write error into the tap side of a `TapBridge`;
   assert `Run` returns and the bridge is evicted from `ll.tapBridges`.

## Where to pick this up

Full raw text of both reviews (much more verbatim file:line detail than fits here) is at
`scratch/opus-robustness-findings.md` and `scratch/codex-sol-medium-critique.txt` in this
repo — untracked, read them if they still exist; this document is the durable summary if
they don't. Next session: confirm with the user whether to proceed with priority items 1-2
(mutation lock + `ll.nodes`/`ll.doc` locking — the two rated most severe, both touching
`supervisor/internal/server/handlers.go` and friends fairly broadly) or a narrower scope.
