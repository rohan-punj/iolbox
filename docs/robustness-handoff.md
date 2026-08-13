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

## Implementation log

### 2026-08-12 â€” items 1-2 and item 8 tap cleanup implemented

Added `Server.labMu` and wrapped every registered control-protocol handler with
`serializedHandler` (`supervisor/internal/server/server.go:107-259`), and took the same
lock in `ConsolePort`, `ConsoleSubscribe`, `CapturePort`, shutdown, the stats sampler, and
scheduled fault callbacks. This serializes lifecycle/topology work across TCP/WS control
connections and the two `time.AfterFunc` fault paths. Fault callbacks also verify that
their `loadedLab` is still current before applying kernel state, so a callback from an old
lab cannot act on unscoped tap/bridge names after `lab.load`.

`handleLabLoad` now retains the old lab as published state until all old node processes,
fabric objects, captures, and console ports are released (`handlers.go:106-217`); only
then does it assign `s.lab = ll`. This closes the cross-lab teardown window identified by
both reviews. `loadedLab` now provides lock-held document snapshots/deep copies, locked
`findNode`/`findLink`, node-id snapshots, link-fault mutation, and static-tap snapshots
(`loaded.go:199-315`). `refreshFabric` computes from a locked document snapshot and
publishes the new tap plan under `ll.mu` (`bridgeplan.go:73-79`); node/link add/remove,
image mutation, status, config/MAC reads, and fabric paths now use those snapshots or
explicit `ll.mu` sections. This removes the direct `ll.nodes`/`ll.doc` mutation/read
class that could produce `fatal error: concurrent map read and map write`.

Node removal now snapshots the removed node's static tap identities, closes/evicts any
matching iouyap bridge, deletes the tap devices, then refreshes the surviving plan
(`handlers.go:1077-1136`, `fabric_linux.go:302-329`; non-Linux is a no-op). This folds the
finding-#1 orphaned-kernel-tap gap into the same serialized path without changing the
already-deployed finding-#1 identity fix.

Regression coverage added: `TestConcurrentLabReadsAndTopologyEdits`
(`server_test.go:28-63`) mixes status, empty lab.start, and node add/remove from four
goroutines; `TestRefreshFabricDropsRemovedNodeTaps` (`links_test.go:127+`) verifies that a
removed node disappears from the static plan and surviving tap identities remain unique.
Verification: `go build ./...`, `go vet ./...`, and `go test ./...` all pass with the
repo-local writable Go cache; the targeted server tests pass. `go test -race ./internal/server`
was attempted with `CGO_ENABLED=1`, but this Windows environment has no `gcc` (`cgo:
C compiler "gcc" not found`), so race verification remains owed on a cgo-capable host.
VM live verification remains owed; no deployment was performed.

### 2026-08-12 — item 3 start transaction/idempotence implemented

`startNodes` now continues through the requested node set after an individual
failure and returns `protocol.StartResult{Started, Failed}` with per-node state
and error details (`supervisor/internal/server/handlers.go:595-725`; the result
shape is `supervisor/internal/protocol/verbs.go:161-176`). IOL/VPCS starts now
report an existing process only while its machine is actually starting/running,
discard stale handles left by an unexpected exit, and reject the spawn-to-running
transition failure through the same rollback path. NAT/tool/VPCS post-spawn
fabric-attach failures close the endpoint/process, release VPCS resources where
applicable, and mark the node crashed (`handlers.go:757-881`). The GUI's existing
bulk partial-result reporting surfaces `failed[]` instead of hiding successful
nodes (`app/src/lib/labStore.svelte.ts:999-1134`). Regression coverage added
`TestStartNodesReportsFailureAndContinues` (`supervisor/internal/server/server_test.go:46-105`),
using a build-spec image failure followed by an already-running VPCS node to pin
both continuation and idempotence.

Verification: `go build ./...`, `go vet ./...`, and `go test ./...` all pass from
the `supervisor/` module with the repo-local writable Go cache. `go test -race`
remains owed on a cgo-capable host because this Windows host has no `gcc`; VM
live verification remains owed and no deployment or commit was performed.

Items 4-7 remain pending.

### 2026-08-12 — item 4 TapBridge half-duplex teardown implemented

`iouyap.runPumps` now treats the first pump error as a bridge-fatal event:
it cancels the sibling, invokes the stop hook that closes the Unix socket and
tap fd, waits up to two seconds for both pump goroutines, and returns the first
error (`supervisor/internal/iouyap/iouyap.go:136-184`; wired by
`bridge_tap_linux.go:90-99`). The Linux TapBridge lifecycle regression injects
a tap-side write failure through test-only I/O hooks and asserts `Run` returns
the failure and closes the bridge (`iouyap/bridge_tap_linux_test.go:14-70`).
The server now inserts the `labBridge` into `ll.tapBridges` before launching
`Run`, then logs and evicts only the matching bridge on exit
(`supervisor/internal/server/fabric_linux.go:207-224`,
`bridgeplan.go:25-44`); `TestEvictTapBridgeOnPumpFailure` covers the eviction
and close behavior (`server_test.go:21-34`).

Verification: `go build ./...`, `go vet ./...`, and `go test ./...` all pass from
the `supervisor/` module with the repo-local writable Go cache. The Linux-only
injected-fd test is excluded by this Windows host's build tags; `-race` remains
owed because no `gcc` is installed, and VM live verification remains owed. No
deployment or commit was performed.

Items 5-7 remain pending.

### 2026-08-12 — items 5, 6, 7 finished; a half-built pass adopted, corrected and documented

**What was found half-built.** A prior interrupted pass had left uncommitted, undocumented
work on items 5, 6 and (unannounced) 7. Item 5 had the `waitDone`/`waitDoneOnce` plumbing on
`node.Process` with `signalWaitDone` deferred in `wait()` and in the VPCS launcher reaper,
plus a bare `p.waitForExit(2*time.Second)` at the end of `Stop`. Item 6 had the
`broadcastSub`+writer-goroutine skeleton in `broadcast.go`, `Encoder.WriteEventWithDeadline`
and `Encoder.Close` in `protocol/codec.go`, `ws.Conn.SetWriteDeadline`, and
`wsbridge.textFrameRWC.SetWriteDeadline`. Item 7 was silently present as
`retryTransientFabric`/`isTransientFabricError` in `fabric_linux.go`, wrapping `EnsureTap`
and the two `Attach` sites. The overall approach in all three was sound, so it was kept and
corrected rather than reverted; the corrections below are the substantive part.

**The `TestAcceptHandshake` failure was NOT caused by item 6.** Root-caused instead to a
pre-existing data race in the test itself: `Accept` flushes the 101 response to the wire
*inside* `Accept` (`ws/ws.go:100-103`), before `newConn` returns and before the httptest
handler goroutine stores the `*Conn` in the shared `gotConn` variable — so the client
goroutine routinely finished reading the handshake and asserted on `gotConn` while the
handler had not yet assigned it. Confirmed by `git stash push -- internal/ws/ws.go` and
running `-count=20` on the unmodified file: it still failed. The `ws.go` delta is a single
additive method and cannot affect `Accept`'s timing; the earlier "passes clean on a stash"
observation was scheduling luck. Fixed properly, not weakened: the handler now hands its
`Conn` over a buffered channel and the test selects on it with a 5s timeout
(`ws/ws_test.go:230-243`, `:287-294`). This also removes a real `-race` reportable write/read
pair. 30 consecutive runs pass.

**Item 5 — bounded wait for real process exit.** The half-built `waitForExit` was a no-op for
VPCS, which is the case that most needed it: `spawnVPCS` reaps the *launcher* (vpcs forks and
`setsid()`s away, so `cmd.Wait` returns milliseconds after spawn), meaning `waitDone` is
already closed long before `Stop` runs and proves nothing about the surviving daemon.
Replaced with `awaitTermination(pgid, consolePort, timeout)`
(`node/spawn_linux.go:479-512`), which branches on console model: for IOL (`pgid == 0`) the
reaper's `cmd.Wait` return is authoritative, so it waits on `waitDone`; for VPCS it polls
`vpcsResidue(consolePort)` (`:516-524`) at 20ms until nothing holds the node's console port —
via the same `pidListeningOn` / `pidsWithVPCSConsolePort` evidence `killVPCS` already uses to
decide what to kill, and the exact bind a replacement vpcs would fail on with "address
already in use". The timeout is now a named `stopWaitTimeout = 3s` (`:446-451`) rather than an
inline `2*time.Second`, and `waitForExit` returns a bool. `Stop`'s doc comment now states the
happens-before contract it provides (`:601-611`), and `handleNodeRestart` documents why its
`stopNode` → `startNodes` sequence is now overlap-free all the way down
(`server/handlers.go:552-558`) — `tool.Endpoint.Stop` already had exactly this bounded-wait
shape (`tool/endpoint_linux.go:178-190`), so this makes `node.Process.Stop` consistent with
the pattern already in the codebase rather than inventing one. `Stop` holds no lock across
the wait, and `teardown` (called before it) is idempotent with `wait()`'s own call.

**Item 6 — bounded fan-out, with one real defect found by the new tests.** Kept the
queue+writer-goroutine shape; rewrote `broadcast.go` around it. The material correction:
publish-side eviction on a full queue was a bare non-blocking send, and the new
`TestHealthySubscriberKeepsReceivingAfterPeerDropped` caught it dropping a *perfectly healthy*
subscriber — a burst emitted in a tight loop outruns the writer goroutine purely because the
scheduler has not run it yet. Added `broadcastEnqueueGrace = 250ms`
(`server/broadcast.go:28-36`): on a full queue, publish waits that long for the writer to
drain before evicting (`:126-143`). Worst case is paid once per subscriber, on the publish
that finally evicts it, so `publish` stays bounded and small while no longer being
trigger-happy. Second correction: the half-built code closed the stream on a *write* failure
but only unregistered on a *queue-overflow* eviction, which would have left a live but
permanently event-less connection — a GUI frozen with no reconnect. Split into `remove`
(clean unsubscribe; does not close, since `ServeConn` owns the stream) and `drop`
(unregister + `enc.Close()`, which unblocks both the wedged writer and `ServeConn`'s read
loop so the client reconnects) — `:96-113`. The writer loop now checks `done` first each
iteration so an unsubscribed connection stops promptly instead of draining a stale backlog
(`:73-93`). Sizing raised from 64/1s to `broadcastQueueSize = 256` / `broadcastWriteTimeout =
5s` for the same false-positive-drop reason. `WriteEventWithDeadline` was tidied to a named
`deadlineWriter` interface with the deadline set and cleared inside the encoder lock so a
concurrent `WriteResponse` cannot inherit an event's deadline (`protocol/codec.go:95-127`).
`ws.Conn.SetWriteDeadline` and `wsbridge.textFrameRWC.SetWriteDeadline` were kept as-is;
every subscriber's writer is an `io.ReadWriteCloser` (TCP `net.Conn` or the WS adapter), never
stdio, so `Encoder.Close` closing it is always correct.

**Item 7 — kept, made testable, scope documented.** The retry helpers were moved out of the
linux-only `fabric_linux.go` into a new platform-independent `server/fabric_retry.go` so they
compile and are testable on any host (they had no OS dependency and were therefore
unverifiable on this Windows box). Behavior preserved: 3 attempts, 25ms base doubling, so
worst-case added latency is 75ms per call — which matters because these run synchronously
inside handlers under `labMu`, once per tap and per link. Added a log line when a retry
actually clears, and a comment recording the deliberate scope limit: it wraps `ip tuntap add`
(`EnsureTap`) and `ip link set <tap> master <br>` (`Attach`), where kernel netdev teardown
being asynchronous makes EBUSY/EAGAIN genuinely self-clearing, and is deliberately NOT wrapped
around iouyap's `openTap`/TUNSETIFF, where EBUSY means a live pump still owns the tap — that
is finding #1's ownership bug, and retrying it would mask it and never succeed anyway. This is
exactly the framing codex sol-medium insisted on.

**Regression tests added.** `server/broadcast_test.go` (new, 4 tests):
`TestPublishDoesNotBlockOnWedgedSubscriber` parks the writer inside a socket write, then
asserts a `broadcastQueueSize+8` burst completes and the wedged subscriber is dropped *and*
its stream closed; `TestWriteDeadlineDropsWedgedSubscriber` asserts the encoder installs a
real, `broadcastWriteTimeout`-bounded deadline and that the resulting timeout drops+closes;
`TestHealthySubscriberKeepsReceivingAfterPeerDropped` is the one that caught the grace-period
defect; `TestUnsubscribeStopsWriterWithoutClosingStream` pins that a clean unsubscribe is
idempotent, stops the writer, and does not close the caller-owned stream.
`node/stop_wait_linux_test.go` (new, linux-tagged): `TestStopWaitsForChildExit` spawns a real
`sleep 60`, asserts `Stop` returns before `stopWaitTimeout` with `cmd.ProcessState` already
populated; `TestStopDoesNotWaitWithoutChild` pins the console-bridge no-child case;
`TestAwaitTerminationVPCSIgnoresLauncherReap` pins that the VPCS branch does not consult the
already-closed `waitDone`. `server/fabric_retry_test.go` (new):
`TestIsTransientFabricError` over three transient and four permanent strerror spellings, and
`TestRetryTransientFabricRetriesOnlyTransient` covering clears-on-third-attempt, permanent
error returning on attempt 1 with no backoff spent, and the never-clears case stopping at the
budget.

**Verification.** From `supervisor/`: `go build ./...`, `go vet ./...`, `go test ./...` all
clean, including `internal/ws` — the whole suite run at `-count=3` and `-count=2` with zero
failures, and the four broadcast tests specifically at `-count=5`. `GOOS=linux go vet ./...`
is clean too, which is how the linux-only `stop_wait_linux_test.go` and the moved fabric-retry
code were compile-verified from this host. `gofmt` clean on every file touched (the
half-built `broadcast.go` was not gofmt'd; three pre-existing unformatted files elsewhere —
`consolescript_test.go`, `painter/stp.go`, `relay/classify_test.go` — were left alone).
**`-race` still cannot run on this host**: `CGO_ENABLED=1 go test -race` fails with
`cgo: C compiler "gcc" not found`, confirmed again this session (`which gcc` → not found), so
race verification remains owed on a cgo-capable host — this is the third entry in a row
carrying that debt. VM live verification remains owed; item 5's VPCS console-port poll and
item 7's retry paths are both Linux-only and have had no live exercise. No deployment, no
commit — everything left uncommitted for review.

Items 1-8 are now all implemented. What remains owed across the whole track: `-race` on a
cgo-capable host, and a live end-to-end exercise on `192.168.111.154` (a tight
`node.restart` loop for item 5, and a deliberately wedged/suspended browser tab during a
`lab.start` for item 6).

### 2026-08-12 — finding #9 (cross-lab tap-name collision) fixed, plus finding #10 found while testing it

Reported from a Linux builder run of the full `internal/server` suite as root under `-race`:
`TestStartNodesReportsFailureAndContinues` fails with `TUNSETIFF iol1_X: device or resource
busy`, but only when all 97 tests in the package run together. Reasoned from the code (the
repro is not reproducible on this Windows host).

**Mechanism, confirmed — with one correction to the reported diagnosis.** The report
attributed the colliding kernel name to `computeStaticTaps`'s pseudo-instance counter
(`fabric.go:85`, `nextPseudo := netmap.PseudoInstanceBase`). That counter does restart at 500
for every fresh lab, but it names the `/tmp/netio<uid>/<pseudo>` SOCKET, not the tap. The tap
name is `fabric.TapName(netmap.InstanceID(nodeID), flatIndex)` = `iol<nodeID+1>_<port>`
(`fabric/commands.go:163`, `netmap.go:52`, used at `fabric.go:110-127`) — derived from the lab
DOCUMENT alone. Both identities are deterministic and unscoped by lab, so any two labs whose
first IOL node has id 0 both name a device `iol1_0`. The conclusion stands and is in fact
broader than reported: the collision needs only equal node ids, not equal pseudo counters.

Finding #1's fix evicts a stale pump BY TAP NAME, but only within one `*loadedLab`'s own
`ll.tapBridges` map (`fabric_linux.go:174-182`). Two unrelated labs share the kernel namespace
and share no map, so neither side can see the other's live pump — hence `TUNSETIFF` EBUSY.
`s.isCurrentLab(ll)` does not help: it only asks whether `ll` is still the lab of the server
that scheduled the callback, which stays true for an older lab in a test binary (each test
builds its own `*Server` and never tears the lab down) and across any window where two labs'
lifetimes overlap. Confirmed that the scheduled-fault path really does reach kernel device
operations: `scheduleFaultExpiry` → `clearFaultEffect` (`fabric_fault_handlers.go:23-31`) →
either `attachFabricLink` (`EnsureBridge`, detach-all-existing-members, `Attach <tap> master
<br>` — `fabric_linux.go:442-521`) or `clearLinkNetem` (`tc` per endpoint dev, `:535-544`);
and the sibling site `scheduleFaultActivation` (`fabric_fault_handlers.go:134`) → `applyActiveFault`
→ `reconcileFabricLinkDown` / `reconcileLinkFault`, i.e. the same class. Both sites needed the
guard.

**Fix — a process-global tap-ownership registry, plus an ownership check in both timers.**
New `supervisor/internal/server/tapowner.go` (platform-independent, ~100 lines of logic) holds
one map from kernel tap device name to the `{lab, pump}` that owns it, with `claimTap`,
identity-checked `releaseTap`, `tapClaimedByOtherLab`, `evictForeignTapClaim`, and
`linkTapClaimedElsewhere`. This is finding #1's by-identity eviction widened from per-lab to
per-process — deliberately the same shape, not a redesign.

- `startFabric` calls `evictForeignTapClaim(t.tapName, ll)` immediately before `EnsureTap`/
  `iouyap.NewTap` so a foreign pump is closed before `TUNSETIFF`, and `claimTap` right after
  inserting into `ll.tapBridges` (`fabric_linux.go:174-224`).
- `labBridge.close()` releases the claim (`bridgeplan.go:48-62`) — every pump-removal path
  (stale/ghost eviction, orphan sweep, `evictTapBridge`, `evictStaticTaps`, `teardownFabric`)
  already funnels through it, so no separate release plumbing was added.
- Both timer callbacks now refuse kernel work when any of the link's IOL endpoint taps is
  claimed by a different lab: `scheduleFaultExpiry` (`fabric_fault.go:229-235`) logs, emits,
  and returns; `scheduleFaultActivation` (`fabric_fault_handlers.go:155-165`) additionally
  rolls its own `Active` back. An UNCLAIMED tap deliberately reads as not-foreign, so a lab
  that never started its fabric behaves exactly as before.

Lock discipline: the registry lock is never held across another lab's `ll.mu` or across a
`close()`.

**Finding #10, found by the new test — an unconditional self-deadlock in BOTH fault timers.**
`loadedLab.findLink` takes `ll.mu` (`loaded.go:309-321`), and both timer callbacks called it
from INSIDE their own `ll.mu` section. `sync.Mutex` is not reentrant, so every scheduled fault
timer that got that far hung its goroutine forever holding `ll.mu` **and**, via the deferred
unlock, `s.labMu` — which permanently wedges the entire serialized control plane (every
handler, `ConsolePort`, `CapturePort`, the stats sampler, shutdown). This was introduced by
items 1-2, when `findLink` became lock-taking, and no test had ever driven a fault timer to
completion. Caught immediately by the first run of the new regression (a 120s test timeout
with both goroutines' stacks). Fixed by hoisting `findLink` out of the critical section in
both callbacks (`fabric_fault.go:211-219`, `fabric_fault_handlers.go:141-147`) — a two-line
move each, with a comment recording why the ordering is load-bearing. This is the one place
items 1-8's code was touched, and the finding-#9 fix genuinely required it (the timers could
not otherwise run at all).

**Regression tests** — new `supervisor/internal/server/tapowner_test.go`, all deterministic,
none relying on `-race` or full-suite timing, and none issuing a privileged command on any
platform (targets are existence-filtered and the fake labs have no real devices):
`TestUnrelatedLabsAllocateTheSameTapName` pins the premise (two independent labs both produce
`iol1_0` and the same pseudo id); `TestClaimTapDisplacesForeignOwner` covers claim /
identity-checked release / cross-lab eviction closing the foreign pump and unhooking it from
its own lab's `tapBridges`; `TestScheduledFaultTimerSkipsTapOwnedByAnotherLab` is the finding-#9
regression proper — an older lab that is still its own server's current lab (so `isCurrentLab`
passes) has a pending timer while an unrelated lab owns `iol1_0`, asserted for BOTH the
activation and expiry sites, checking that no fabric state was realised and the fault did not
stay active; `TestScheduledFaultTimerCompletes` is the finding-#10 regression (a timer must
finish and leave `ll.mu`/`s.labMu` free); `TestScheduledFaultTimerActsOnItsOwnTaps` is the
over-broadness control (unclaimed and self-owned taps are not foreign, and `close()` gives the
name back).

**Verification.** From `supervisor/`: `go build ./...`, `go vet ./...`, `go test ./...` all
clean; `internal/server` also at `-count=3`. `gofmt` clean on every file touched.
`GOOS=linux go vet ./...` clean, which is how the `fabric_linux.go` edits were compile-verified
from this Windows host.

### 2026-08-12 — builder `-race` verification + VM deploy of items 1-10

Builder: `192.168.226.10` (`ubuntu@`, key `~/.ssh/pnet_builder_key` — the documented
root/`iolbox` VM credentials do NOT work here; it has `gcc` 13.3.0 and passwordless sudo, but
only Node 18.19.1, too old for this repo's Vite — GUI embedding for the deploy binary was done
locally on Windows instead, see below). Working tree (all of items 1-10, uncommitted) synced via
`tar | ssh` (no `rsync` on this Windows host).

`go test -race ./internal/server/...` as root, `-count=1`, run 3x after cleaning the leaked
`iol1_0..3` taps from earlier non-root/no-cleanup attempts each time: all 3 clean, confirming
the finding-#9 `TUNSETIFF iol1_X: device or resource busy` failure in
`TestStartNodesReportsFailureAndContinues` is gone and the finding-#10 deadlock does not
reproduce under load. A full `go test -race ./...` (all packages, root) also came back clean for
every package items 1-10 touched. The remaining failures (`internal/egress`
`TestParseHexLEIPValue`, `internal/node` `TestPCConsoleBridgeBroadcastsToTwoClients`,
`internal/tool` `TestCageLinuxReadableCgroupV2` / `TestDetectProbeSocketHandshakeSucceedsRepeatedly`
/ `TestDetectLinuxProbeCleansEveryObject` / `TestLaunchVerifySetprivTransitionIsEmpirical` /
`TestNetnsCreateSysctlFailureTearsDownNetns`) were confirmed **pre-existing and unrelated**: each
reproduces identically against a baseline copy with `spawn_linux.go` reverted to its pre-item-5
`git show HEAD` content and the new `stop_wait_linux_test.go` removed — none of items 1-10 touch
`internal/egress` or `internal/tool` at all, and `internal/node`'s failure reproduces on baseline
too (confirmed flaky pre-existing, 2/5 failures on baseline alone). This is simply the first time
this whole suite has ever run as root on a real Linux kernel (dev is normally Windows), so
environment-dependent tests that were previously always skipped/erroring out are surfacing for
the first time — a pre-existing test-coverage gap, not a regression, and out of scope for this
track.

One real bug WAS found and fixed during this verification pass, in a test rather than production
code: `TestStartNodesReportsFailureAndContinues` includes a real IOL node, which makes
`s.startNodes` provision real kernel static taps via `startFabric` regardless of that node's own
buildSpec outcome — the test had no cleanup, so a second run (or the next unrelated test reusing
the same deterministic tap name) collided with EBUSY. Fixed with `t.Cleanup(func() {
s.teardownFabric(ll) })` (`supervisor/internal/server/server_test.go:86-91`), the same production
teardown path `lab.stop`/`lab.load` use. Verified 5x clean in isolation and as part of the full
suite after the fix.

Deploy: built locally on Windows via `bash build-release.sh` (stamped `v0.5.1-28-gacd246d-dirty`,
real GUI embedded, cross-compiled `linux/amd64`, no `gcc` needed for a non-race cross-compile).
Pushed to the VM (`192.168.111.154`) via `pscp`/`plink` per the documented pattern in
`redesign-handoff.md`. `systemctl restart iolbox-supervisor` completed and reported `active`
within the restart window; no orphaned `iol*`/`nat*`/`vtool*` taps afterward (the lab was left
stopped from the prior session, so this doesn't yet exercise items 4/5/6 under a live lab).

**Still owed**: a live end-to-end exercise on the VM itself — a tight `node.restart` loop (item
5), a deliberately wedged/suspended browser tab during `lab.start` (item 6), and generally
loading/starting a real lab to confirm no regression in everyday use, none of which this session
did (deploy was verified only via `systemctl is-active` + tap-leak check, not a live lab start).

### 2026-08-12 — finding #11: `dirstat`/`slowtee` teardown could never return, wedging the whole control plane

**The most severe finding in this track.** Found during the first live exercise of the
deployed items-1-10 build on `192.168.111.154`, by the most ordinary sequence there is:
load a lab with links → start it → stop it → **load the same lab again**. The second
`lab.load` hung the entire supervisor within seconds. Diagnosed from a real `kill -QUIT`
goroutine dump (not inferred): goroutine 239 permanently parked in `sync.WaitGroup.Wait`
inside `dirstat.(*Classifier).Close` ← `teardownFabric` ← `handleLabLoad`, while every other
connection's requests — including brand-new `hello` calls from fresh WebSocket connections —
queued forever behind `labMu`.

**Mechanism (confirmed by reading the code, matches the dump).** `Classifier.Close()`
(`dirstat/dirstat.go:410-419`, pre-fix) called `c.closer()` — which did `syscall.Close(fd)` on
each bound raw `AF_PACKET` socket (`dirstat_linux.go:76-83`, pre-fix) — and then `c.wg.Wait()`
for the `readLoop` goroutines (`dirstat_linux.go:117-135`, pre-fix). Each `readLoop` sits in
`syscall.Recvfrom(fd, buf, 0)` on a socket deliberately left in blocking mode (the comment at
`dirstat_linux.go:88-91` said so, and asserted that "closing the socket fd makes the blocked
recvfrom return an error"). That assertion is **wrong**: on Linux, `close(fd)` from another
thread does not reliably return a thread already parked inside a blocking `recvfrom` on that
same descriptor — it drops a reference, it does not wake the sleeper. So the read loops never
returned, `wg.Wait()` never returned, and `Close()` never returned. The same code also had a
latent fd-reuse hazard: the descriptor was closed while a loop might still `recvfrom` on it.

**Why it is catastrophic now, and worse than #10.** `Classifier.Close()` is reached from
`teardownFabric` (`server/fabric_linux.go:1100`), called by `handleLabLoad`
(`server/handlers.go:192`) while that handler runs inside items-1/2's `labMu` via
`serializedHandler`. One stuck `Close` therefore wedges `labMu` permanently — every handler,
`ConsolePort`, `CapturePort`, the stats sampler, shutdown — recoverable only by `kill -9`.
Finding #10 (the fault-timer self-deadlock) had the identical blast radius but needed a
*scheduled link fault* to fire; #11 needs only a lab that has links and has been started once,
i.e. reloading any normal lab. `dirstat` itself was never touched by items 1-10 — this is a
pre-existing bug — but items 1-2's serialization is what promotes "one classifier fails to
shut down" into "the appliance is dead".

**`slowtee` had the identical bug and is fixed in the same pass.** `slowtee.forwardLoop`
(`slowtee/slowtee_linux.go:105-128`, pre-fix) is a copy of the same blocking-`Recvfrom` +
close-to-stop pattern, and `Tee.Close()` is called from the *same* `teardownFabric` loop
(`fabric_linux.go:1102-1104`) plus `openLinkSlowTee` (`:737`). Leaving it would have left the
exact same wedge one LACP link away, so it was in scope rather than scope creep. No other
package is affected: `iouyap/tap_linux.go:71`, `extnet/endpoint_linux.go:363` and
`vtap/shim_linux.go:70` all `SetNonblock` and go through Go's poller (this is the same
`SetNonblock`/`internal/poll` property the "Ruled out" section records for Opus finding #8), and
`grep` confirms `dirstat` and `slowtee` were the only two blocking `Recvfrom` sites in the tree.

**Fix — bounded receive timeout + stop signal, then close.** `shutdown()` was evaluated and
**rejected**: `AF_PACKET` has no shutdown handler (`packet_ops` wires `sock_no_shutdown`), so
`shutdown(fd, SHUT_RDWR)` fails with `EOPNOTSUPP` and wakes nothing — it is the right answer for
stream sockets, not this one. Full poller integration (`SetNonblock` + `os.NewFile`) was rejected
too: `os.File.Read` gives no `sockaddr_ll`, and both loops need `sll_pkttype` to drop
`PACKET_OUTGOING` (dirstat would double-count; slowtee would forward its own injections into a
storm). The minimal correct mechanism is therefore `SO_RCVTIMEO`:

- `bindTap` now installs `SO_RCVTIMEO = recvTimeout` (250ms) on every socket, and treats a
  failure to install it as a bind failure — an unstoppable loop is worse than no directional
  data / no LACP passthrough (`dirstat/dirstat_linux.go:115-140`,
  `slowtee/slowtee_linux.go:92-117`).
- The loops take a `stop <-chan struct{}`, check it at the top of each iteration (so a
  continuously busy socket also stops), and treat `EAGAIN` as the timeout tick that re-checks it
  (`dirstat_linux.go:151-180`, `slowtee_linux.go:124-152`).
- The single `closer` hook was split into an ordered pair, `stopRead` (signal only) and
  `closeFDs` (`dirstat/dirstat.go:147-157`, `dirstat_linux.go:88-101`; `slowtee/slowtee.go:56-66`,
  `slowtee_linux.go:70-90`). `Close` signals, waits for the loops to *provably* return, and only
  then closes the descriptors — which also closes the pre-existing fd-reuse hole.
  `Close`/`closeDrain`/`waitTimeout` live in the platform-independent files
  (`dirstat/dirstat.go:412-475`, `slowtee/slowtee.go:68-131`).

**The hard timeout was kept as defense in depth** (`closeDrainTimeout = 3s`,
`dirstat/dirstat.go:40` and `slowtee/slowtee.go:34`). It is not a substitute for the fix —
the loops exit within 250ms — but given that being wrong here kills the whole appliance, `Close`
is now unconditionally bounded. Critically, a timed-out drain **leaks the sockets rather than
closing them**: a leaked fd on a lab reload is a rounding error, closing an fd under a live
`recvfrom` is a correctness bug. The timeout path logs.

**Regression tests.** Portable (run on this Windows host):
`dirstat/close_test.go` and `slowtee/close_test.go` —
`TestCloseDrainClosesFDsOnlyAfterReadLoopsExit` / `...ForwardLoopsExit` pin the ordering (fds
closed strictly after the loops return); `TestCloseIsBoundedWhenAReadLoopNeverExits` /
`...AForwardLoopNeverExits` model the pre-fix wedge exactly — a loop that never observes
teardown — and assert `Close` returns inside its budget, signals stop, and does **not** close
the fds; plus nil/stub no-op coverage. Linux-only, real raw sockets bound to `lo`, skipping
without `CAP_NET_RAW` (`dirstat/dirstat_linux_test.go`, `slowtee/slowtee_linux_test.go`):
`TestBindTapInstallsReceiveTimeout` proves `recvfrom` returns `EAGAIN` in about `recvTimeout`
instead of parking (asserted behaviorally — `syscall` has no `GetsockoptTimeval` and this repo
vendors no `x/sys`); `TestBlockedRecvfromUnblocksOnStop` is the regression proper (a `readLoop`
genuinely parked in `recvfrom` returns after `close(stop)`); `TestOpenCloseDrainsRealSockets`
drives the real `Open`/`Close` pair and asserts it drains in roughly one timeout tick.

**Verification.** From `supervisor/`: `go build ./...`, `go vet ./...`, `go test ./...` all clean,
with `internal/dirstat` and `internal/slowtee` also at `-count=3`. `GOOS=linux go vet ./...`
clean, which is how the Linux-only sources and their new tests were compile-verified from this
Windows host. `gofmt` clean on every file touched. Items 1-10 were not modified.

**Owed (user's side)**: `-race` on the cgo-capable builder, the Linux-only tests run as root
(they skip without `CAP_NET_RAW`), and above all a live re-run of the exact reproducer on
`192.168.111.154` — load a lab with links, start, stop, load again — confirming `lab.load`
returns and the control plane stays responsive. Left uncommitted and undeployed.

### 2026-08-12 — finding #12 (stop-a-running-PC self-deadlock) fixed + systematic ll.mu/labMu reentrancy audit

**The most severe finding in the track, worse than #10 and #11.** `syncPCNode`
(`supervisor/internal/server/pcstate.go:140-147`) read `ll.nodes[id]` under `ll.mu` and then
called `ll.findNode(id)` from INSIDE that same critical section — and items 1-2 had made
`findNode` take `ll.mu` itself (`loaded.go`). `sync.Mutex` is not reentrant, so the stopping
goroutine parked forever holding `ll.mu` AND (via `serializedHandler`'s deferred unlock)
`s.labMu`, wedging every handler, `ConsolePort`, `CapturePort`, the stats sampler, and
shutdown. Unlike #10 (needs a scheduled link fault) or #11 (needs a lab reload), this fires
on the single most ordinary operation there is: **stopping any running PC or tool-kind node**
(`stopNode` → `syncPCNode`, `handlers.go:1096-1102`) — 100% reproducible, no timing needed.
Reproduced live on `192.168.111.154` (goroutine dump: stopper parked in `Mutex.Lock` from
`findNode` at `loaded.go:298` ← `syncPCNode` at `pcstate.go:142` while holding `ll.mu` from
`:140`).

**Fix — locked-variant helper, not a hoist.** `syncPCNode` reads `nr` and the node document
in ONE atomic `ll.mu` section (that pairing is the point of the section), so instead of
finding #10's hoist, `loaded.go` now has `findNodeLocked(id)` (caller must hold `ll.mu`) and
the public `findNode` wraps it under the lock (`loaded.go:296-322`); `syncPCNode` calls the
locked variant (`pcstate.go:140-149`). `findNode`'s doc comment now states the
non-reentrancy contract explicitly.

**Regression test** — `TestStopRunningPCNodeReturns`
(`supervisor/internal/server/pcstate_test.go`): dispatches a real `node.stop` through the
serialized dispatcher against a PC-kind node whose runtime holds a live stub
`&tool.Endpoint{}`, with a 15s watchdog; then asserts the node reached `stopped`, the
endpoint was cleared, and BOTH `ll.mu` and `s.labMu` are takeable again. **Verified to catch
the bug**: with the fix temporarily reverted the test fails via its watchdog in 15s (not a
silent hang); with the fix it passes at `-count=3`. One enabling change outside `server/`:
`tool.(*Endpoint).endpointStopLiveness` (`tool/endpoint_linux.go:428-437`) now guards the
nil liveness channel so `Stop()` on a zero-value endpoint (the stub construction the server
tests already use, e.g. `fabric_fault_test.go:127`) is a safe no-op on Linux instead of a
`close(nil)` panic — production endpoints always get the channel from `Start` (`:82`),
behavior there is unchanged.

**Systematic reentrancy audit — every lock-taking helper and every caller.** This was the
actual deliverable: prove there is no THIRD instance waiting. Method: enumerated every
`ll.mu.Lock()` / `s.labMu` / `s.mu` / `tapOwners.mu` / `b.mu` (broadcaster) critical section
in `supervisor/internal/server/` and inspected what each calls while held — real call
chains, not textual proximity.

Lock-taking helpers on `loadedLab` (each takes `ll.mu`): `faultForLink`, `labDir`, `workDir`
(indirect: `findNode`+`labDir`, sequential, never while held), `get`, `nodeIDs`,
`docSnapshot`, `setLinkFault`, `staticTapsSnapshot`, `stopAll`, `findNode`, `findLink`,
`activateInitialFaults`, `previousPCState`, `mergePCState`. Per-file verdicts:

- `pcstate.go` — `syncPCNode` WAS finding #12 → **fixed**. `previousPCState`/`mergePCState`
  called by `syncPCNode` strictly outside its own lock section — safe. `handlePCSyncState`'s
  `ll.mu` section (`:192-204`) is a pure map/doc walk — safe.
- `fabric_fault.go` / `fabric_fault_handlers.go` — the two timer callbacks carry the
  finding-#10 fix (`findLink` after unlock, `:205-219` / `:135-147`) — verified still
  correct; every other `ll.mu` section (`cancelFaultTimer`, `cancelFaultTimersForNode`
  clone-then-relock, `activateInitialFaults`, `handleLinkSetFault`'s install/rollback
  sections, `scheduleFaultActivation`'s rollback sections) touches only `ll.linkFaults` /
  `cloneLabDoc` — safe. `cancelFaultTimerLocked` is a correctly-named locked variant, all
  callers hold the lock — safe.
- `handlers.go` — every `ll.mu` section (`tryAdoptLoad` ×2, `startNodes` armed-capture copy,
  `handleNodeAdd`/`Remove`/`SetImage` doc mutations, `handleNodeMACs` classifier-pointer copy
  (comment even records "never hold ll.mu while calling Attribution()"), link add/remove doc
  swaps, capture arm/release, `handleStatus` captures walk) is pure map/slice work — safe.
  `armDocCaptures` holds `ll.mu` across `s.capturePorts.Next/Release` — different lock
  (PortAllocator's own), no path back into `ll.mu` — safe. All `findNode`/`get`/
  `docSnapshot`/`labDir` calls sit outside any held section — safe.
- `fabric_linux.go` — all ~20 `ll.mu` sections are short map ops. The one nontrivial
  pattern: `ghost.close()` (`:174-182`) and `lb.close()` (`:245-252`) run INSIDE `ll.mu`
  sections; proven safe because `labBridge.close` (`bridgeplan.go:48-60`) takes only
  `tapOwners.mu` (`releaseTap`) plus `iouyap.TapBridge.Close`, which is a non-blocking
  `sync.Once` fd-close (`iouyap/bridge_tap_linux.go:216-231`) — no `ll.mu`, no wait on pump
  goroutines. `teardownFabric` snapshots everything under one lock, then closes/stops
  outside it — safe (its unlocked `ll.doc.Links` read at `:1113` is serialized by `labMu`,
  which every caller and every doc writer holds).
- `bridgeplan.go` — `evictTapBridge` and `refreshFabric` lock only around map ops;
  `docSnapshot`/`computeStaticTaps` called before the section — safe.
- `tapowner.go` — lock-ordering audit: `close`-under-`ll.mu` establishes
  `ll.mu → tapOwners.mu`; `evictForeignTapClaim` RELEASES `tapOwners.mu` (`:116`) before
  taking the foreign lab's `ll.mu` (`:121`) and before `close()` (`:129`), so `tapOwners.mu`
  is never held while acquiring any `ll.mu` — no cycle, safe.
- `server.go` — `ConsolePort`/`ConsoleSubscribe`/`CapturePort`/`shutdown` take
  `labMu → s.mu (release) → ll.get / ll.mu` — consistent ordering with every handler, safe.
  `isCurrentLab` takes only `s.mu`, never called with `s.mu` held — safe.
- `stats.go` — sampler takes `s.mu` (release) then `labMu` around `fabricStats`; matches
  handler ordering — safe.
- `broadcast.go` — `b.mu` never held while taking `ll.mu`/`labMu`; `emit` is only ever
  called OUTSIDE `ll.mu` sections (verified every `s.emit` and `machine.To` call site —
  `stopAll` copies the runtime list under lock and transitions outside it) — safe.
- `fabric_other.go`, `toolproxy.go`, `painter_collect_linux.go`, `painter_linux.go`,
  `imagescan.go` — helpers called with no lock held; `fabric_other.teardownFabric` /
  `reconcileFabricLinkDown` lock only around their own map writes — safe.

Conclusion: finding #12 was the last live instance of the class. Every other
helper/caller pair either uses a different lock, releases before calling, or was already
shaped as a `*Locked` variant.

**Verification.** From `supervisor/`: `go build ./...`, `go vet ./...`,
`go test -timeout=120s ./...` all clean; `internal/server` + `internal/tool` at `-count=2`
with `-timeout=60s` clean (a new self-deadlock would fail loud via the timeout, not hang);
`GOOS=linux go vet ./...` clean (how the `endpoint_linux.go` edit was compile-verified from
this Windows host); `gofmt` clean on all four touched files. `-race` remains owed on the
cgo-capable builder as before. **Owed (user's side)**: sync to the builder for `-race`, and
the live re-verification on `192.168.111.154` — load a lab with a PC node, start it,
`lab.stop`, and confirm the control plane stays responsive. Left uncommitted and
undeployed.

### 2026-08-12 — builder `-race` + live VM verification of finding #12, full end-to-end deploy

Synced to the builder (`192.168.226.10`), `go build`/`go vet` clean, `go test -race -count=1`
on `internal/server` clean including the new `TestStopRunningPCNodeReturns`, then a full
`go test -race ./...` — every package items 1-12 touched is clean; the remaining failures
(`internal/egress`, `internal/node` console-bridge flake, `internal/tool` cgroup/netns tests)
are the same pre-existing, unrelated ones confirmed against baseline earlier in this doc.

Built locally via `build-release.sh`, deployed to the VM via the documented `pscp`/`plink`
pattern, `systemctl restart iolbox-supervisor` → `active`.

**Live end-to-end verification** (the thing items 1-12 were ultimately for): wrote a small
throwaway Go NDJSON/WS control-protocol client (`golang.org/x/net/websocket`, not committed
to this repo) to drive the real deployed supervisor directly — `hello`, `status`, `lab.load`,
`lab.start`, `lab.stop` — since the in-app browser pane can't reach the appliance's private
IP. Key protocol gotchas hit and worked around: the GUI/control port is **4001**, not 80 (the
loopback-only port 4000 is unreachable off-VM); the WS control channel's session cookie comes
from a plain `GET /` (no login flow — LAN-trusted-appliance model, `wsbridge.go:174-186`);
each WS text frame carries exactly one JSON object with **no trailing newline** in either
direction (`wsbridge.go:701-733`'s `textFrameRWC` adapter strips it) — an NDJSON-style
`bufio.ReadString('\n')` client hangs forever against this, must use
`websocket.Message.Send`/`Receive` (one frame per call) instead; `lab.start`/`lab.stop`'s
`Nodes` field must be **omitted** (nil), not sent as `[]` — an explicit empty array means
"apply to zero nodes," only `nil` means "all" (`handleLabStart`/`handleLabStop`,
`handlers.go`).

Loaded the VM's real saved 5-node lab (`6bf354b6-ddb3-4e58-aaa4-40418ccc0161`: PC0, PC1, two
IOL switches, one tool node), started it — all 5 nodes reached `running`, real IOL/VPCS PIDs,
real `link.stats` events with STP frames and learned MACs flowing between nodes, confirming
the fabric/tap plane items 1-4/8 rely on is genuinely live, not just unit-tested. This
**exercise directly found and confirmed two production deadlocks that no test had ever
reached** (findings #11 and #12 above) — both fixed in this session, both now re-verified
live: reloading the same lab after a stop (finding #11's trigger) and stopping a running PC
node (finding #12's trigger) each completed cleanly twice in a row, with a completely
independent fresh WS connection's `hello` still answering immediately afterward each time
(proving `labMu` wasn't wedged) and zero orphaned `iol*`/`nat*`/`vtool*` kernel taps left
behind (`ip link show`) after either full lab lifecycle.

**Current state**: items 1-12 all implemented, `-race`-clean, live-verified against a real
5-node lab including a full load→start→stop→reload cycle and a PC-node stop, deployed to
`192.168.111.154`, lab left stopped/clean. Still uncommitted (this whole track spans 12
findings across several sessions — committing is a deliberate decision for the user to make,
not automatic). Not yet exercised: item 5's tight `node.restart` loop and item 6's
deliberately-wedged-subscriber scenario specifically (the general "does the control plane
stay responsive under real use" property they exist for was exercised broadly above, but
not those two exact scenarios).

### 2026-08-12 — items 1-12 + IOL MAC read committed and deployed; four follow-up GUI fixes

Items 1-12 above, plus the IOL-MAC `show interfaces` read feature (see
`docs/iol-mac-show-interfaces-plan.md`) and the toast-notification feature (see
`docs/toast-notifications-plan.md`), were committed as two commits (`eec2954`
backend, `8af5530` frontend) and deployed to `192.168.111.154` — see those docs for
detail. Four small follow-up GUI items landed in the same session, live-verified in
the dev server (`localhost:1420`, mock transport) but **not yet committed or deployed**:

1. **Serial interfaces don't work in this fabric — removed from node defaults.**
   `computeStaticTaps` (`supervisor/internal/server/fabric.go:74-78,106`) hardcodes
   `serialGroups=0` when allocating taps ("serial-interface taps are a later
   refinement") and the frontend already rejects serial link-fault endpoints
   (`CanvasInner.svelte:985-993`, `docs/protocol.md:175`) — serial adapters were
   inventory-only, never wired into the fabric. Changed the Router/Switch (IOL) node
   default from `ethernet: 1, serial: 1` to `ethernet: 2, serial: 0` in
   `buildDroppedNode()` (`app/src/lib/components/CanvasInner.svelte:527-529`) and the
   Inspector's fallback-when-missing defaults to match
   (`app/src/lib/components/Inspector.svelte:184,189`). Serial is still fully
   available as a manual override (min/max unchanged) for anyone who wants inventory
   rows without traffic. Live-verified: a freshly dropped router now shows Ethernet=2/
   Serial=0 in the Inspector.

2. **Console dock: switching tabs needed two clicks to type.** Root cause: clicking
   the `<button class="tab-label">` in `Console.svelte` gave the *button* browser
   focus by default (on mousedown, before the click handler runs), which raced with
   `ConsoleTerm.svelte`'s own `$effect` that calls `term.focus()` when the pane
   becomes focused (`ConsoleTerm.svelte:197-204`) — the button's default focus won,
   so the first click only switched the *visible* pane and a second click was needed
   to actually focus the terminal for typing. Fixed with
   `onmousedown={(event) => event.preventDefault()}` on the tab button
   (`Console.svelte:215-222`), which suppresses the browser's default focus-on-click
   for that button so `term.focus()` is the only thing that moves focus. Verified via
   a scripted single mousedown+mouseup+click on each tab in the dev server:
   `document.activeElement` was the `xterm-helper-textarea` immediately after, for
   both tabs, confirming one click is now enough.

3. **"Save config from NVRAM" and "Export config…" added to the IOL right-click
   context menu**, so both no longer require opening the Edit dialog or hovering
   the node's quick-action toolbar. "Save config from NVRAM" calls the existing
   `labStore.saveNodeConfig(nodeId)` (same handler the Edit dialog's button and the
   quick-action toolbar already used) to extract NVRAM into the lab doc.
   "Export config…" is new: `labStore.exportNodeConfig(nodeId)`
   (`labStore.svelte.ts`, next to `saveNodeConfig`) reuses the same `configExtract`
   NVRAM-read path and then downloads the result as `<node>-startup-config.txt` via
   the same Blob/anchor pattern as `downloadCapture`. Both wired into
   `buildNodeMenuItems()` in `CanvasInner.svelte`, IOL-only, enabled only while the
   node is running (same gating as the existing quick-action button).

4. **Auto-save toggle** (Settings → Lab → "Auto-save lab", on by default).
   `scheduleAutosave()` (`labStore.svelte.ts:1040-1047`) already debounce-saved after
   almost every mutating action unconditionally (gated only on the real-supervisor
   transport) — there was no way to turn it off. Added `autoSaveEnabled` state
   persisted to `localStorage["iolbox.autosave.enabled"]` (default `true` if unset),
   gated `scheduleAutosave()` on it, and added the toggle row to
   `SettingsDialog.svelte`, following the existing `chromeStore`
   localStorage-toggle pattern. Manual Save (the toolbar button /
   `saveLab(notify=true)`) is unaffected — this only gates the automatic debounced
   path, including config exports/extractions and every other place that already
   calls `scheduleAutosave()`.

Verification for all four: `npm run check` clean (0 errors/warnings) after each
change; all four live-verified in the dev server (`localhost:1420`, mock transport).
#3: right-clicked a stopped IOL node — "Export config…" present and disabled with
the expected tooltip; started the node, re-opened the menu — enabled; clicking it
fired a real anchor download (`blob:` URL, filename `R0-startup-config.txt`). #4:
toggled off in Settings → Lab, confirmed `localStorage["iolbox.autosave.enabled"]`
flipped to `"false"` and survived a page reload, then reset to `"true"`. None of
this session's four items are committed or deployed yet — build/deploy with
`build-release.sh` the same way as the rest of this session's work once reviewed.
