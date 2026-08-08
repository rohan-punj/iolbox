# P1 dispatch plan — scoped-agent execution of T1.1–T1.12

Status: **DISPATCH PLAN — REVISION 2** (orchestration only; no design decisions, no product code). Revision 1 was reviewed by `codex sol-medium` and found **unsafe to execute as written** (4 critical, 5 high, 6 medium). This revision resolves all 15 findings.
Source of truth for *what* to build: `docs/learning-tools-nodes-plan.md` §P1 (T1.1–T1.12) and its "Package / file map". Source of truth for *why*: `docs/learning-tools-nodes-spec.md`. Neither is re-litigated here.

This document answers a different question: **how to hand T1.1–T1.12 to a fleet of scoped `codex exec luna-xhigh` agents (one job each, strictly file-scoped) with maximum safe parallelism**, and what `codex sol-medium` must review *before* the first implementation agent starts.

---

## Revision history — what changed in revision 2

Each line is one sol-medium finding and its resolution. Batch numbers below are **revision-2** numbers (see §2); revision 1's B10/B11/B12/B13 shifted by one because a new batch was inserted at B10.

1. **(Critical) Direct-child PID registry was incomplete.** `PR_SET_CHILD_SUBREAPER` is process-wide, so the reap loop can steal the exit status of *any* unregistered `exec.Cmd` child in the supervisor — not just tool children. **New batch B10** (wave 2) registers every one of the six pre-existing direct-child spawn/wait sites (`node/spawn_linux.go`, `bcap/capture_linux.go`, `extnet/endpoint_linux.go`, `extnet/detect_linux.go`, `fabric/manager_linux.go`, `fabric/detect_linux.go`) with `tool.Registry`. §2, §3, §4 and §5 all updated; B10 must land before B11 (reap) and B14 (which calls `StartReaper`).
2. **(Critical) B08 had a same-wave dependency on B07.** `NetnsExecArgs` moved into **B01's** frozen portable `tool.go` (wave 1). B07 and B08 now both consume it and neither depends on the other. Recorded in B01 §5 item 4a and in B07/B08's "already defined" lists.
3. **(Critical) Shared-symbol mitigation covered only exported helpers.** Every batch prompt now carries a **private-symbol prefix rule** scoped to its own files (`cage*`, `netns*`, `launch*`, `reap*`, `endpoint*`, `detect*`, `manifest*`, `inst*`, `toolpacks*`, `toolnode*`, `fabtool*`) and must report its final private symbol list in its summary for a pre-merge diff. §0 hazard 2 rewritten.
4. **(Critical) The reap batch repeated a P0-fixed bug.** `wait4` rejects `WNOWAIT` with `EINVAL`. **B11's prompt now pins the exact P0 mechanism** from `tools/p0-reaper/reaper_linux.go`: raw `waitid(P_ALL,…,WEXITED|WNOHANG|WNOWAIT)` via `syscall.Syscall6(syscall.SYS_WAITID, …)` with the hand-laid-out 128-byte `siginfo` struct, then `wait4(pid, WNOHANG)` only for an unregistered peeked pid. `wait4`+`WNOWAIT` is explicitly forbidden.
5. **(Critical) The P1 stub could not start under the frozen env contract.** The proven stub reads **`IOLBOX_TOOL_OPTIONS`** (not `IOLBOX_TOOL_OPTS`) and hard-exits if it is unset or the file is unreadable. Renamed in B01's `ScrubbedEnvAllowlist` and in B08; **B01 adds `Config.Options` + `OptionsFile()`**; **B12 must atomically create an `ioltool:ioltool 0600` `options.json`** before launch; **B15 populates `Config.Options`**. New deviation **D9**.
6. **(High) B08's cgroup-placement fallback was unimplementable.** Go has no safe arbitrary pre-exec hook. **B03's helper binary gains `--cgroup <path>`** (it moves its own pid into that cgroup *before* the privilege transition, while it still has root's write access); **B08 consumes it** instead of inventing a Go-side write. New deviation **D10**.
7. **(High) B17 was placed beside what it asserts on.** The gate harness now gets **its own wave 6**, dispatched only after B15/B16 land and the integration build is green.
8. **(High) B17's fixture was underspecified.** Pinned to an exact **two-endpoint fixture** (one `tool` node + one `vpcs` node, one link) with literal NDJSON payloads in the fabric-harness style; the post-restart lab reload, the supervisor readiness wait, and an **ancestry-scoped** zombie assertion (PPid == supervisor pid, state `Z`) replace the host-global `/proc/*/stat` grep.
9. **(High) Portable-test requirements did not match file ownership.** Added a portable non-syscall file to each affected batch: `cage.go`/`cage_test.go` (B06), `netns.go`/`netns_test.go` (B07), `reap.go`/`reap_test.go` + Linux-only `reap_linux_test.go` (B11), `detect.go`/`detect_test.go` + `detect_linux_test.go` (B13), `fabric_test.go` (B16). Syscalls stay build-tagged; only pure logic moves. `NetnsExecArgs` did **not** go to `netns.go` — see finding 2, it is in `tool.go`.
10. **(High) P10's resolution silently moved validation from load time to start time.** `internal/lab` stays structural-only (no `internal/tool` import), but **B15 adds a registry-aware known-pack check at the server's `lab.load` boundary** in `handlers.go`, so the check happens at load time as T1.1 requires. New deviation **D11**.
11. **(High) `InitCgroupRoot` idempotency was broken by its own discovery rule.** Pinned in B06: if the cgroup discovered from `/proc/self/cgroup` is already named exactly `supervisor`, its **parent** is `<D>`; plus a cached-root fast path so a true re-entrant call is a no-op. A repeat-call test on the path-selection logic is required.
12. **(Medium) `LoadPacks` partial-success semantics did not reach the server.** Pinned: `LoadPacks` always returns the valid packs slice **plus** a non-fatal aggregated warning. B14 caches the slice regardless of the error and logs the warning; a one-valid/one-invalid test is required.
13. **(Medium) Reaper shutdown was unspecified.** `cmd/supervisor/main.go` has **no** `defer`-based subsystem-stop pattern (shutdown is `signal.NotifyContext` → goroutines return → `wg.Wait()` → drain `errCh` → `log.Fatalf`), and a `defer` would be skipped by `log.Fatalf`. B14's prompt pins the call site: **immediately after `wg.Wait()` returns, before `close(errCh)`**.
14. **(Medium) B03's prompt contradicted the shared preamble.** B03 carries an explicit narrow exception: it may create its **own new** `go.mod` (new standalone module); the no-go.mod rule is about not touching *existing* modules. B03's acceptance criteria gain the missing `GOOS=linux GOARCH=amd64 go build` **and** `go vet` gates.
15. **(Medium) Batch sizing confirmed / one merge.** B01 stays one batch (mitigated by findings 3 and 9 rather than a split). B07 stays one batch, with a sanctioned split recorded if judged too large. B12 stays one batch. **Old B13 (`loaded.go`) is merged into B14** — same wave, no concurrency benefit; `loaded.go` is now in B14's OWNS list.

---

## 0. The three hazards this plan is built around

Everything below follows from three failure modes specific to concurrent agents on this codebase:

1. **Same-file collision.** Two agents editing `handlers.go` concurrently produce a conflict, full stop. Ownership is therefore per-*file*, not per-*task*, and no file appears in two concurrently-running batches.
2. **Same-package symbol collision — exported *and* unexported.** `supervisor/internal/tool/` is one Go package built from ~18 files. Two agents in the same wave each writing a `runCmds` helper, an `ErrUnsupportedPlatform`, a `netnsName()`, a `parseEvents()`, or a `buildArgv()` into *different files* of the same package produces a **duplicate-symbol build break with no merge conflict** — git merges it cleanly and `go build` explodes. This is the single most likely way this dispatch goes wrong, and revision 1 only defended the exported half. It is now prevented by **two** rules: (a) **B01 freezes every shared/exported symbol first**, and every downstream prompt carries an explicit "do NOT define these" list; and (b) **every batch prefixes every unexported identifier it introduces with its own batch prefix** (`cage*`, `netns*`, `launch*`, `reap*`, `endpoint*`, `detect*`, `manifest*`, `inst*`) and reports its final private symbol list in its summary, so the dispatching session can diff the union for collisions before merging a wave.
3. **Cross-platform compile split.** The dev/dispatch box is Windows. `go build ./...` there compiles only `_other.go` files, so a broken `_linux.go` is invisible locally. Every batch that writes a `_linux.go` **must** gate on `GOOS=linux go build ./...` and `GOOS=linux go vet ./...`, not just the native build. Corollary from finding 9: a batch that owns *only* `_linux`-suffixed files cannot have portable tests at all — so every such batch also owns a **portable non-syscall file** where its pure logic (parsing, argv building, decision logic) is factored out and tested on the dev box.

A fourth hazard was surfaced by the review and is really a consequence of hazard 2 at *process* scope rather than symbol scope:

4. **`PR_SET_CHILD_SUBREAPER` is process-wide, not tool-scoped.** The moment `InitRuntime` sets it, *every* orphaned grandchild in the whole supervisor reparents onto us, and the reap loop's ownership split is only safe if **every** direct `exec.Cmd` child in the entire binary is in `tool.Registry`. Revision 1 registered only new tool-node children. **B10 exists solely to close this**, and it is a hard prerequisite of B11 and B14.

Consequence: this is **not** a "fan out all 12 tasks at once" plan. It is a narrow wave-1 contract batch, a wide wave-2, a converging tail, and a solo gate-harness wave at the end.

---

## 1. Deviations from the plan doc's file map (deliberate, ownership-driven)

These are the only places this dispatch plan differs from `learning-tools-nodes-plan.md`'s file map. They exist to make file ownership disjoint or to make the design implementable; none change designed behaviour. **sol-medium should confirm each is acceptable.** D9–D11 are new in revision 2.

| # | Plan doc says | This dispatch says | Why |
|---|---|---|---|
| D1 | `_other.go` stubs exist only for `detect` and `endpoint` | every `X_linux.go` batch also owns `X_other.go` | portable callers (`server.go`, `handlers.go`) call `InitCgroupRoot`/`ReapStale`; those need non-Linux stubs, and a shared stub file would be a co-owned file |
| D2 | PID registry lives in `reap_linux.go` | `PIDRegistry` type + package singleton live in portable `tool.go` (B01); `reap_linux.go` consumes it | lets B12 (endpoint) and B10 (existing children) register without waiting on B11 (reap) — moves endpoint from wave 4 to wave 3 and makes B10 a wave-2 batch |
| D3 | cgroup bring-up ordering described under `server.go` (T1.11) | mechanism implemented as exported `tool.InitCgroupRoot()` in `cage_linux.go`; `server.go` only calls it | keeps all cgroup-fs syscalls in one owned file; makes T1.11's `server.go` edit thin |
| D4 | `handleToolListPacks` in `handlers.go` | in new `internal/server/toolpacks.go`, owned by the same batch that owns `server.go` | `register()` (in `server.go`) must reference the handler; splitting them across waves would leave a wave that does not compile |
| D5 | `instance.go` portable | `instance.go` (portable JSON/paths) + `instance_lock_linux.go` / `instance_lock_other.go` | `flock` is not portable; the plan's "portable" label is not achievable as written |
| D6 | native cap-transition fallback unspecified in P1 | promoted to a shipped helper binary `tools/iolbox-toollaunch/` (installed `/opt/iolbox/iolbox-toollaunch`) | Go cannot safely do `capset`/`setuid`/`PR_CAP_AMBIENT_RAISE` in a pre-exec hook from a threaded runtime; P0 already proved the standalone-binary shape (`tools/p0-launcher`) |
| D7 | `contracts/lab.schema.json` not listed as touched | it is touched (its `kind` enum is closed: `["iol","vpcs","nat"]`) | omission in the plan's touched-file list |
| D8 | startup calls described as "beside `extnet.Detect`" in `server.New` | a new `Server.InitRuntime()` called from `cmd/supervisor/main.go`; `New()` stays side-effect-free | `New()` runs in ~12 unit tests across two packages; `Detect`/`ReapStale`/subreaper/cgroup-migrate would fire in every one. See punch-list P8 |
| **D9** | spec §2.6's scrubbed-env list is written `IOLBOX_TOOL_OPTS`, and `pack.json` has no options *file* | the frozen name is **`IOLBOX_TOOL_OPTIONS`**, pointing at `<runRoot>/tool/<id>/options.json`, created `ioltool:ioltool 0600` by `Endpoint.Start` before launch; `tool.Config` gains an `Options []byte` payload | the **P0-proven** stub (`tools/tool-stubgui/main.go:24-45`) reads `IOLBOX_TOOL_OPTIONS`, hard-exits if it or `IOLBOX_TOOL_SOCK` is unset, and then **reads *and* writes** that file. The proven binary is not being changed to match a doc; the doc is being corrected to match the proven binary. Without this the P1 gate cannot start a single node |
| **D10** | T0.2 leaves cgroup placement entirely to `SysProcAttr.CgroupFD` | the helper binary also accepts `--cgroup <path>` and writes its own pid to `<path>/cgroup.procs` **before** the privilege transition | revision 1's stated fallback ("a `cgroup.procs` pre-exec write" from Go) is not implementable — `os/exec` offers no safe arbitrary pre-exec hook from a threaded runtime, which is the same reason D6 exists. The helper is already the place where "do privileged thing, then drop, then `execve`" lives, and it still holds root's write access to that file at that point |
| **D11** | T1.1 puts the "known installed pack id" check in `lab.Validate` | `lab.Validate` stays structural (`config.pack` present + non-empty, no `internal/tool` import); the **registry-aware** known-pack check runs at the server's `lab.load` boundary in `handlers.go` (B15) | keeps T1.1's *timing* (rejected at load, not at start) without the layering inversion of `internal/lab` importing `internal/tool`. Revision 1's P10 moved the check to `startToolNode` only, which silently changed behaviour: an invalid doc would have loaded fine and only failed on start |

---

## 2. Batch table (17 batches)

`OWNS` = the exclusive, exhaustive list of files that batch may create or modify. Anything not listed is read-only for that agent.

### Wave 1 — 3 agents

**B01 — `tool` package contract (portable types + shared helpers)**
Implements: the portable half of T1.3's type surface, the naming rules pinned in T1.9, and the shared-helper inventory that prevents hazard #2.
OWNS: `supervisor/internal/tool/tool.go`, `supervisor/internal/tool/tool_test.go`
Depends on: nothing. **Critical path — everything in waves 2–6 codes against this.**

**B02 — Lab-doc + wire contracts (T1.1 + T1.11's protocol types)**
OWNS: `supervisor/internal/lab/lab.go`, `supervisor/internal/lab/validate.go`, `supervisor/internal/lab/lab_test.go`, `contracts/lab.schema.json`, `supervisor/internal/protocol/verbs.go`
Depends on: nothing (`KindTool` is a string const; `internal/lab` and `internal/protocol` must **not** import `internal/tool`).

**B03 — cap-transition helper binary (promote `tools/p0-launcher`, + `--cgroup` per D10)**
OWNS: `tools/iolbox-toollaunch/` (new dir: `go.mod`, `main.go`, `launch_linux.go`, `launch_other.go`)
Depends on: nothing (own **new** Go module, mirrors the existing `tools/p0-*` layout — see the go.mod exception in its prompt).

### Wave 2 — 7 agents

**B04 — manifest load/validate (T1.3 items #7/#8/#12)**
OWNS: `supervisor/internal/tool/manifest.go`, `manifest_test.go`, `testdata/packs/**`
Depends on: B01. Private prefix `manifest*`.

**B05 — durable install identity + object state file (T1.9 bullets 1 & 3)**
OWNS: `supervisor/internal/tool/instance.go`, `instance_lock_linux.go`, `instance_lock_other.go`, `instance_test.go`
Depends on: B01. Private prefix `inst*`.

**B06 — cgroup cage + delegated-root bring-up (T1.4, and T1.11's cgroup mechanism per D3)**
OWNS: `supervisor/internal/tool/cage.go` (**portable, new — finding 9**), `cage_linux.go`, `cage_other.go`, `cage_test.go` (**portable, new**), `cage_linux_test.go`
Depends on: B01. Private prefix `cage*`.

**B07 — netns + veth + /31 mgmt fallback (T1.5, T1.6)**
OWNS: `supervisor/internal/tool/netns.go` (**portable, new — finding 9**), `netns_linux.go`, `netns_other.go`, `netns_test.go`
Depends on: B01 (including `NetnsExecArgs`, which lives in `tool.go` per finding 2 — **do not define it here**). Private prefix `netns*`.

**B08 — cap-transition launcher wrapper (T1 half of T0.2 promotion)**
OWNS: `supervisor/internal/tool/launch_linux.go`, `launch_other.go`, `launch_test.go`
Depends on: B01 (types **and `NetnsExecArgs`**), B03 (helper binary path/argv incl. `--cgroup` — **name/contract only**, no code dependency). **No dependency on B07** (finding 2). Private prefix `launch*`.

**B09 — packaging / rootfs (T1.11 systemd half + P0 run-log's un-baked prerequisites + stub pack fixture)**
OWNS: `runtime/files/iolbox-supervisor.service`, `runtime/build-rootfs.sh`, `runtime/files/prestart-clean.sh`, `runtime/files/tools/packs/stub/pack.json` (new)
Depends on: B03 (binary name), B01 (`pack.json` field names must match the `Manifest` struct tags).

**B10 — register EXISTING direct children with `tool.Registry` (NEW in revision 2 — finding 1)**
OWNS, exhaustively — these six files and nothing else:
`supervisor/internal/node/spawn_linux.go` · `supervisor/internal/bcap/capture_linux.go` · `supervisor/internal/extnet/endpoint_linux.go` · `supervisor/internal/extnet/detect_linux.go` · `supervisor/internal/fabric/manager_linux.go` · `supervisor/internal/fabric/detect_linux.go`
Depends on: B01 (`Registry`) only. Dispatched in the **same wave as** the other wave-2 batches, not depending on any of them — but **strictly before B11 (reap) and B14 (which calls `StartReaper`)** can be considered safe.

### Wave 3 — 3 agents

**B11 — reap loop + `ReapStale` (T1.8, T1.9 bullets 3–5)** *(was B10 in revision 1)*
OWNS: `supervisor/internal/tool/reap.go` (**portable, new — finding 9**), `reap_linux.go`, `reap_other.go`, `reap_test.go` (portable), `reap_linux_test.go` (**new, Linux-only**)
Depends on: B01 (`PIDRegistry`, `ReapConfig`), B05 (state file API), B06 (`KillCage`/`RemoveCage`/`ListCages`), B07 (`DeleteNetns`/`DeleteVeth`), **B10 (registry completeness — the ownership split is unsound until every existing direct child is registered)**. Private prefix `reap*`.

**B12 — Endpoint lifecycle (T1.7 + the `tool.Start/Stop/AttachBridge/DetachBridge/State` surface)** *(was B11)*
OWNS: `supervisor/internal/tool/endpoint_linux.go`, `endpoint_other.go`, `endpoint_test.go`
Depends on: B01, B04, B05, B06, B07, B08. **Not** on B11 (thanks to D2). Private prefix `endpoint*`.

**B13 — `Detect` capability matrix (T1.12)** *(was B12)*
OWNS: `supervisor/internal/tool/detect.go` (**portable, new — finding 9**), `detect_linux.go`, `detect_other.go`, `detect_test.go` (portable), `detect_linux_test.go` (**new, Linux-only**)
Depends on: B01, B06, B07, B08. Must use the *production* cage/netns/launch functions (probe parity is the whole point of T1.12). Private prefix `detect*`.

### Wave 4 — 1 agent

**B14 — supervisor startup wiring (T1.11 Go half) + pack registry + `tool.listPacks` + `nodeRuntime.tool` (T1.2, absorbed from old B13 per finding 15)**
OWNS: `supervisor/internal/server/server.go`, `supervisor/internal/server/toolpacks.go` (new), `supervisor/internal/server/toolpacks_test.go` (new), `supervisor/internal/server/loaded.go`, `supervisor/cmd/supervisor/main.go`
Depends on: B01, B02 (protocol DTOs), B04 (`LoadPacks`), B06 (`InitCgroupRoot`), B10 (registry completeness — `StartReaper` is unsafe without it), B11 (`ReapStale`, `StartReaper`), B12 (`tool.Endpoint` on both build tags, for `loaded.go`), B13 (`Detect`). Private prefix `toolpacks*`.

### Wave 5 — 2 agents

**B15 — node lifecycle handlers (T1.10 handlers half + D11's load-time pack check)**
OWNS: `supervisor/internal/server/handlers.go`, `supervisor/internal/server/toolnode_test.go` (new)
Depends on: B02 (`lab.KindTool`), B12 (`Endpoint`), B14 (`nr.tool`, `s.toolCaps`, `s.toolPacks`, `s.toolPack`). Private prefix `toolnode*`.

**B16 — fabric integration (T1.10 fabric half, spec §4.2 points 1–8)**
OWNS: `supervisor/internal/server/fabric.go`, `supervisor/internal/server/fabric_test.go` (**portable, new — finding 9**), `supervisor/internal/server/fabric_linux.go`, `supervisor/internal/server/fabric_linux_test.go`
Depends on: B01 (`HostVethName`), B02, B12, B14 (`nr.tool`). Private prefix `fabtool*`.

### Wave 6 — 1 agent

**B17 — P1 gate harness (the "Gate to P2" NDJSON procedure)**
OWNS: `docs/tests/p1-gate.sh`, `docs/tests/p1-gate.md`
Depends on: **everything, and specifically on B15 and B16 being landed, integrated and green** — it asserts on the exact object names, verbs, error codes and paths those two finalise, and (finding 7) a harness written concurrently with them can only guess. It shares no file with anything, so it is alone in its wave purely for correctness, not for ownership.

---

## 3. Dependency graph

Edge list form (`X → Y` means "Y must land before X"). The ASCII picture from revision 1 is dropped in favour of this list: with B10 added and old B13 merged away, the picture no longer fits legibly and the list is what the dispatching session actually checks.

```
wave 1   B01  → (nothing)
         B02  → (nothing)
         B03  → (nothing)

wave 2   B04  → B01
         B05  → B01
         B06  → B01
         B07  → B01                       [NetnsExecArgs comes from B01, not from B07]
         B08  → B01, B03 (argv contract incl. --cgroup; name only, no code dep)
         B09  → B01 (Manifest tags), B03 (binary name)
         B10  → B01 (Registry) only       [no edge to any other wave-2 batch]

wave 3   B11  → B01, B05, B06, B07, B10
         B12  → B01, B04, B05, B06, B07, B08          [NOT B11 — see D2]
         B13  → B01, B06, B07, B08

wave 4   B14  → B01, B02, B04, B06, B10, B11, B12, B13

wave 5   B15  → B02, B12, B14
         B16  → B01, B02, B12, B14

wave 6   B17  → B15, B16 landed AND the integration build green
```

### Non-obvious edges, and why they are real (not task-number order)

- **B11 (reap) → B05 (instance), not the reverse.** T1.9's `ReapStale` reads `/var/lib/iolbox/tool-objects.json` keyed by the durable install id. The plan's task numbering puts T1.8 (reap loop) before T1.9 (durable id), but the *code* dependency runs the other way: durable id + state-file API must exist before the sweep can be written. Extracted from T1.9's third bullet.
- **B11 → B06 and B07.** `ReapStale` destroys via `cgroup.kill` + rmdir, `ip netns del`, veth del — it calls cage and netns primitives.
- **B11 → B10, and B14 → B10 (NEW, finding 1, the most important new edge in this revision).** `StartReaper`'s entire safety argument is "a peeked pid that is in `tool.Registry` belongs to somebody's `cmd.Wait()`, so leave it alone." `PR_SET_CHILD_SUBREAPER` is set **process-wide** by `InitRuntime`, so from that moment the loop sees SIGCHLD for the IOL pty child, the VPCS launcher, the `tcpdump` capture, and every `ip`/`sudo` command run by `extnet`, `fabric` and their `Detect` probes. If any of those is unregistered, the loop's `wait4(pid, WNOHANG)` can collect its status first and the owning `cmd.Wait()` then returns `ECHILD` — an IOL node that silently never reports its exit, or a `runCmds` that reports a spurious failure. **B10 closes that hole and must be in the tree before either B11 or B14 is exercised.**
- **B13 (detect) → B06/B07/B08.** T1.12 explicitly requires the probe create its cage as a level-3 leaf under the **same** delegated root as production, after the migrate-first controller enable. A probe with its own copy of the cgroup code would not prove what T1.12 says it must prove. Hard code dependency, not resemblance.
- **B12 (endpoint) → B04 (manifest).** `Start` resolves `gui.bin` and the readiness probe hits `gui.health`; both come out of the validated `Pack`. T1.7 and T1.3 item #12 are coupled.
- **B08 does NOT depend on B07 (changed in revision 2).** Revision 1 had B08 calling `NetnsExecArgs` from B07 with both in wave 2 — a same-wave code dependency, and one of the four criticals. `NetnsExecArgs` is a pure string-slice builder with no syscalls, so it now lives in B01's `tool.go`; both B07 and B08 consume it from the frozen contract.
- **B14 → B12**, not merely "both are server files". `nodeRuntime.tool *tool.Endpoint` (now inside B14 after the old-B13 merge) needs the type on the `!linux` build too, which is `endpoint_other.go`.
- **B15 → B14.** `handleHello` (in `handlers.go`) merges `tool.Capabilities.GateFeatures()`, `startToolNode` gates on `s.toolCaps.Supports(...)` mirroring `startExtnetNode`'s `s.caps.Supports` at `handlers.go:596`, and D11's load-time check calls `s.toolPack(id)`. All three read `Server` state B14 declares.
- **B16 → B14.** `fabric_linux.go`'s attach/detach/teardown branches dereference `nr.tool`, declared in `loaded.go`, which B14 now owns.
- **B15 ∥ B16 is safe.** They touch disjoint files (`handlers.go` vs `fabric.go`/`fabric_linux.go`), and neither calls a function the other defines: `startToolNode` calls the *pre-existing* `s.attachFabricForNode`. Note the functional (not compile) coupling: the P1 gate cannot pass until **both** land, because `fabricNodes` must admit `KindTool` for the link to be realised at all — which is exactly why B17 now waits for both (finding 7).

### Explicitly **not** parallel-safe (claims to reject)

- T1.4 ∥ T1.11 — the cgroup bring-up sequence and the cage creation are the same mechanism; splitting them across agents duplicates the `subtree_control` logic. Merged into B06.
- T1.5 ∥ T1.6 — same file (`netns_linux.go`). Merged into B07. If B07 is judged too large the **only** sanctioned split gives T1.6 its own exclusively-owned `mgmt_linux.go`/`mgmt_other.go`/`mgmt_test.go`; a second agent must never be pointed at `netns_linux.go`.
- T1.8 ∥ T1.9 — same file (`reap_linux.go`) for the sweep half. Merged into B11; only the durable-id/state-file half is split out to B05 (different file).
- **B11 ∥ B10 is not safe** even though their files are disjoint — not a build hazard but a correctness one. B10 is in wave 2 and B11 in wave 3 precisely so the registry is complete before any reap code exists to be run.
- **B17 ∥ B15/B16 is not safe** (finding 7). It shares no file with them, so revision 1 called it parallel — but it must assert against their *landed, integrated* behaviour (exact error codes, exact `StartedNode` shape, whether the link actually realises). A concurrently-written harness can only encode guesses. Own wave.
- Any claim that the seven wave-2 batches are safe **without B01 landing first** is wrong: they would each invent their own naming helpers and command runners.
- Any claim that the wave-2 batches are safe on B01 alone **without the private-prefix rule** is also wrong (finding 3): B01 freezes the exported/shared surface, but four of them independently need a parser, three need an argv builder, and generic names for those collide in-package with no git conflict.

---

## 4. Wave structure

| Wave | Batches | Concurrent agents | Gate before next wave |
|---|---|---|---|
| 1 | B01, B02, B03 | **3** | `go build ./... && go vet ./... && go test ./...` in `supervisor/`; `GOOS=linux go build ./...`; `tools/iolbox-toollaunch` builds **and vets** for linux/amd64 |
| 2 | B04, B05, B06, B07, B08, B09, B10 | **7** | as above, plus `GOOS=linux go vet ./internal/tool/...` clean; plus the **private-symbol diff**: union the seven summaries' private symbol lists and confirm no name appears twice in package `tool` |
| 3 | B11, B12, B13 | **3** | as above; `go test ./internal/tool/...` green on the dev box (portable tests); private-symbol diff again |
| 4 | B14 | **1** | as above; `go test ./internal/server/... ./internal/wsbridge/...` still green (no regression in the ~12 existing `New()` call sites; confirm no test calls `InitRuntime`) |
| 5 | B15, B16 | **2** | full `go test ./internal/tool/... ./internal/server/...` green = the first half of "Gate to P2" |
| 6 | B17 | **1** | `bash -n` / `shellcheck` clean; then run `docs/tests/p1-gate.sh` on the appliance for the second half of "Gate to P2" |

**17 batches, 6 waves, max concurrency 7 (wave 2).**

Dispatching discipline: the dispatching session (you) **lands and commits each wave before opening the next**. Within a wave, agents run truly concurrently and never see each other's output. Between waves, the dispatching session is responsible for the integration build and for the private-symbol diff — no agent is asked to fix another agent's file.

---

## 5. Scoped agent prompts

Each block below is the complete prompt for one `codex exec luna-xhigh` invocation. A shared preamble applies to all of them.

### Shared preamble (prepend to every prompt verbatim)

> You are implementing one scoped batch of P1 of the iolbox learning-tool-nodes work, on branch `feat/learning-tool-nodes`.
>
> **Read first:** `docs/learning-tools-nodes-plan.md` §P1 (the tasks T1.1–T1.12 and the "Package / file map" table) and `docs/p1-dispatch-plan.md` §1 (deviations) and §6 (resolved punch list). Read `docs/learning-tools-nodes-spec.md` only for background rationale — **every design decision in both documents is final; do not re-derive, re-litigate, or "improve" them.**
>
> **File scope is absolute.** You may create/modify ONLY the files listed under OWNS in your batch. Other agents are editing other files concurrently. If you believe a file outside your OWNS list must change, **stop and report it** instead of editing it.
>
> **Symbol discipline — exported.** `supervisor/internal/tool/` is one Go package assembled from many files by many agents. Do NOT define any identifier listed in your prompt's "already defined in `tool.go` — do not redefine" section, and do not add general-purpose helpers (command runners, path helpers, error vars) — if you need one that is not in the ledger, stop and report.
>
> **Symbol discipline — unexported (this is new and it is not optional).** Every unexported identifier you introduce — function, type, const, var — must begin with your batch's assigned private prefix, given in your prompt. A generic name like `parseEvents`, `buildArgv`, `runOne` or `sweep` in a different file of the same package is a **duplicate-symbol build break that git merges cleanly**. At the end, **list every private identifier you defined in your summary**; the dispatching session diffs the union across the wave before merging.
>
> **Style:** match the conventions of `supervisor/internal/extnet/` exactly — `//go:build linux` real files with `//go:build !linux` stubs, package/function doc comments that explain *why* not *what*, errors wrapped with a `tool: ` prefix, no new third-party dependencies (the module has exactly one: `github.com/creack/pty`).
>
> **Build gates (all must pass from `supervisor/`, or from your module dir for tools/):**
> `go build ./...` · `go vet ./...` · `go test ./...` · `GOOS=linux go build ./...` · `GOOS=linux go vet ./...`
> The dev box is Windows: the native build compiles only `_other.go` files, so **the `GOOS=linux` gates are the ones that actually check your Linux code.** A batch that only passes the native build is not done.
>
> Do not touch git state (no commit, no branch, no stash). Do not modify `go.mod`/`go.sum`. *(One batch, B03, carries an explicit narrow exception to this last rule in its own prompt.)*

---

### B01 — `tool` package contract

> **OWNS:** `supervisor/internal/tool/tool.go`, `supervisor/internal/tool/tool_test.go` — and nothing else.
> **Private prefix:** none needed — the shared unexported helpers below are *the ledger* every other batch is forbidden to redefine, so they keep their plain names. Do not add unexported identifiers beyond the ones listed here.
>
> Create the new package `supervisor/internal/tool` (peer to `internal/extnet`, `internal/fabric`, `internal/vtap`). This file is the **frozen contract** every other file in the package will be written against by other agents, in parallel, without seeing each other. Precision here is the whole job. Zero syscalls; must compile and its tests must pass on Windows.
>
> Per the plan's file map, `tool.go` holds "portable types: `Endpoint`, `Config`, `Capabilities`, `Pack`, `Manifest`, `GateFeatures()`, `Supports()`. No syscalls." Note: `Endpoint` itself is defined in `endpoint_linux.go`/`endpoint_other.go` by a later batch (mirroring `extnet`) — you define everything else.
>
> Define exactly:
>
> 1. **Package doc comment.** What a tool node is (a supervised process tree inside a netns + cgroup v2 cage, running with ambient `CAP_NET_RAW` only); the three-model framing from spec §2.2 (spawned-pty IOL / process-less-tap NAT / this); and that the privileged data plane is `_linux` with `_other` stubs.
> 2. `type Kind string`; `const KindTool Kind = "tool"`.
> 3. `var ErrUnsupportedPlatform = errors.New("tool: tool endpoints are only supported on linux")` — **the package's single unsupported-platform error**; every `_other.go` stub in the package will return this one.
> 4. **Deterministic kernel-object naming** (T1.9 bullet 2: node-id-derived, no random tag, within the 15-char `IFNAMSIZ` limit). Exported funcs, each with a doc comment stating the IFNAMSIZ budget where relevant:
>    `NetnsName(nodeID int) string` → `iolt<id>`; `HostVethName(nodeID int) string` → `vtool<id>`; `PeerTempName(nodeID int) string` → `vtoolp<id>` (the deterministic temp name from T1.5, never `eth1`); `MgmtVethName(nodeID int) string` → `mtool<id>` (T1.6 only); `CageName(nodeID int) string` → `tool-<id>` (leaf dir name, not a path); `SocketDir(runRoot string, nodeID int) string` → `<runRoot>/tool/<id>`, defaulting `runRoot` to `/run/iolbox`; `const GuestIface = "eth1"`.
> 4a. **`func NetnsExecArgs(nodeID int, argv []string) []string`** — returns `["ip","netns","exec", NetnsName(nodeID), argv...]`. **This lives here, in the frozen wave-1 contract, deliberately** (dispatch finding 2): both the netns batch (B07) and the launcher batch (B08) consume it and they run concurrently in wave 2, so it cannot live in either of their files without creating a same-wave code dependency. It is a pure string-slice builder with no syscalls, so it belongs in the portable file anyway. Doc-comment that fact so nobody later "tidies" it into `netns_linux.go`.
> 5. **`Limits`** struct (`MemoryMax int64`, `PidsMax int`, `CPUMax string` in `"<quota> <period>"` form, `SwapMax int64`) + `DefaultLimits()` returning the punch-list-pinned values: 2048 MiB memory, 512 pids, `"200000 100000"` cpu (2 cores), swap 0.
> 6. **Manifest types**, JSON tags exactly matching spec §2.6's `pack.json` example: `Manifest{ManifestVersion, ID, Name, Icon, Interpreter, GUI, Caps []string, Options []Option, Groups []string, Modules []Module, Limits *Limits}`; `GUI{Bin, Transport, Console, Health string}`; `Module{Key, Label, Group, Script string, Fields []Field, Mitigation *Mitigation}`; `Field{Name, Type, Label string, Required bool}`; `Mitigation{Text string}`; `Option{Name, Type, Label string}`. Plus `const ManifestVersion = 1` and `var AllowedCaps = []string{"NET_RAW"}` (T1.3: allowlist is NET_RAW only; NET_ADMIN is never grantable).
> 7. **`Pack`** — a loaded, validated pack: `{ID, Root string, Manifest Manifest, GUIBin string, Scripts map[string]string}` where `GUIBin` and `Scripts` values are **absolute, canonicalized, containment-checked** paths (T1.3 item #8 — the manifest stores relative paths; resolution produces contained absolute paths).
> 8. **`ScrubbedEnvAllowlist`** (spec §2.6): `PATH`, `HOME`, `LANG`, `PYTHONHOME`, `PYTHONPATH`, `IOLBOX_TOOL_SOCK`, **`IOLBOX_TOOL_OPTIONS`**, `IOLBOX_PACK_DIR`, `IOLBOX_NODE_ID`. Doc-comment why the list exists (one pack's Python must not be steerable to import another writable artifact) **and** doc-comment the name specifically: it is `IOLBOX_TOOL_OPTIONS`, **not** `IOLBOX_TOOL_OPTS`, because the P0-proven GUI `tools/tool-stubgui/main.go:25-28` reads `IOLBOX_TOOL_OPTIONS` and exits non-zero if it (or `IOLBOX_TOOL_SOCK`) is empty. The proven binary is not being changed; see dispatch deviation D9. Getting this string wrong silently breaks readiness at the P1 gate.
> 9. **`Capabilities`** (T1.12 matrix): `{NetnsCreate, VethCreate, VethMoveRename, CgroupDelegated, AmbientCapTransition, UnixProxy bool; Reasons map[string]string}` + `OK() bool` (all six true) + `GateFeatures() []string` (returns `["tools"]` iff `OK()`; mirror `extnet.Capabilities.GateFeatures` at `extnet.go`) + `Supports(k Kind) bool`.
> 10. **`Config`** — one tool endpoint to bring up: `{NodeID int, Pack Pack, Limits Limits, Root CgroupRoot, StateDir, RunDir, User string, InstanceID string, Options []byte}`. `User` defaults to `"ioltool"`, `StateDir` to `/var/lib/iolbox`, `RunDir` to `/run/iolbox`. **`Options`** is the raw JSON payload written to the per-node options file before launch (deviation D9); doc-comment that a nil/empty `Options` means the endpoint writes `{}` — it must never leave the file absent, because the GUI hard-exits when it cannot read it.
> 11. **`func OptionsFile(runRoot string, nodeID int) string`** → `<runRoot>/tool/<id>/options.json` (built on `SocketDir`). Doc-comment the pinned ownership/mode the endpoint batch must apply: owner `ioltool:ioltool`, mode `0600`, inside the `0700` `ioltool`-owned socket dir; this is the exact file the GUI reads **and rewrites** at startup.
> 12. **`CgroupRoot`** `{Delegated, SupervisorLeaf string}` — the T1.4 3-level layout: `Delegated` is `<D>` (the supervisor's delegated cgroup from `/proc/self/cgroup`, controller-enabling root that holds no processes), `SupervisorLeaf` is `<D>/supervisor/`. Also `const SupervisorLeafName = "supervisor"` — B06's idempotency correction keys off this exact leaf name, so it is pinned here rather than spelled twice.
> 13. **`LaunchSpec`** `{NodeID int, Netns string, CgroupFD *os.File, CgroupPath string, Binary string, Args []string, Env []string, User string, AmbientCaps []string, WorkDir string}`. `CgroupPath` is the absolute cage path, carried so the launcher can pass `--cgroup` to the helper binary when the `CgroupFD` path is unavailable (deviation D10).
> 14. **`ReapConfig`** `{Root CgroupRoot, StateDir, RunDir, InstanceID string}`.
> 15. **Object-state records** (T1.9 bullet 3): `ObjectRecord{NodeID int, CgroupPath, Netns, HostVeth, MgmtVeth, SocketDir string}` and `ObjectState{InstanceID string, Objects map[string]ObjectRecord}` (key = decimal node id), with JSON tags — this is the on-disk shape of `/var/lib/iolbox/tool-objects.json`.
> 16. **`PIDRegistry`** (T1.8, and see dispatch-plan deviation D2 — it lives here, portable, not in `reap_linux.go`): mutex-guarded `map[int]struct{}` with `NewPIDRegistry()`, `Add(pid)`, `Remove(pid)`, `Contains(pid) bool`, `Len() int`, plus a package-level singleton `var Registry = NewPIDRegistry()`. Doc-comment the ownership split verbatim from T1.8: `exec.Cmd.Wait()` is authoritative for every direct child; the subreaper loop peeks **non-destructively with `waitid`+`WNOWAIT`** and reaps **only** PIDs absent from this registry, so it can never steal a direct child's exit status. **Add one further paragraph** (dispatch finding 1): `PR_SET_CHILD_SUBREAPER` is *process-wide*, so this registry must hold **every** direct `exec.Cmd` child anywhere in the supervisor — IOL/VPCS, `tcpdump` capture, and every `ip`/`sudo` command run by `extnet` and `fabric` — not only tool children. A separate batch registers those existing sites; state that here so nobody later assumes the registry is tool-scoped.
> 17. **Shared command/path helpers** (unexported; mirror `internal/extnet/commands.go`, which is portable): `type cmdSpec struct{ name string; args []string }`, `runCmds([]cmdSpec) error`, `runCmdsBestEffort([]cmdSpec)`, `fileExists(string) bool`, `contained(root, target string) (string, bool)` (EvalSymlinks + Clean + `strings.HasPrefix(resolved, root+sep)`). **These exist here so that seven agents writing into this package next wave do not each invent their own.**
>
> **Tests (`tool_test.go`, must pass on Windows):** table tests for every naming function including IFNAMSIZ bounds at large node ids; `NetnsExecArgs` produces exactly `ip netns exec iolt<id> <argv...>` and does not alias the caller's slice; `GateFeatures`/`Supports`/`OK` across the matrix; `contained()` against `..` traversal and an absolute-path escape; `DefaultLimits` values; `OptionsFile` under a custom and a defaulted `runRoot`; `ScrubbedEnvAllowlist` contains the literal string `"IOLBOX_TOOL_OPTIONS"` and does **not** contain `"IOLBOX_TOOL_OPTS"` (this assertion is the regression guard for deviation D9); JSON round-trip of `Manifest` against a literal copied from spec §2.6's `pack.json` example; JSON round-trip of `ObjectState`.
>
> **Acceptance:** compiles standalone (it has no dependencies); all five build gates green; `go test ./internal/tool/...` green natively on Windows. **Report the final exported symbol list in your summary** — the dispatching session publishes it to the wave-2 agents.

---

### B02 — Lab-doc + wire contracts (T1.1)

> **OWNS:** `supervisor/internal/lab/lab.go`, `supervisor/internal/lab/validate.go`, `supervisor/internal/lab/lab_test.go`, `contracts/lab.schema.json`, `supervisor/internal/protocol/verbs.go`.
> **Private prefix:** none required (you add no unexported helpers); if you find you need one, prefix it `tool` and report it.
>
> Implement **T1.1** plus the protocol DTOs T1.11 needs. Four small, purely declarative edits:
>
> 1. `lab.go`: add `KindTool Kind = "tool"` to the `Kind` const block (currently `KindIOL`/`KindVPCS`/`KindNAT`), with a doc comment matching the existing style: a bundled learning-tool node (e.g. secbench) run as a supervised process tree in a netns + cgroup cage; exactly one connectable interface, `"eth1"`; tool-specific data rides the existing `Config map[string]json.RawMessage` — **no new top-level `Node` fields** (T1.1 is explicit about this).
> 2. `validate.go`: add `case KindTool` to the kind switch. It requires `n.Config["pack"]` to be present and to decode to a **non-empty string**. **Do NOT check the pack id against installed packs here** — `internal/lab` must not import `internal/tool` (layering; `lab` is a pure document package with one dependency, `netmap`). Per dispatch deviation **D11**, the registry-aware known-pack check happens at the server's `lab.load` boundary, so T1.1's load-time *timing* is preserved without the layering inversion; **say exactly that in a comment here** so a later reader does not "restore" the check. Then extend the endpoint loop: `KindTool` behaves like `KindNAT` — exactly one legal interface, but `"eth1"` not `"eth0"`, and at most one link endpoint (reuse the existing `extEndpoints` counter). Update the `default:` error message to `kind must be iol, vpcs, nat or tool`.
> 3. `contracts/lab.schema.json`: add `"tool"` to the `kind` enum (currently `["iol","vpcs","nat"]`) and extend that property's `description` and the endpoint `interface` description to cover tool/`eth1`. The Go struct tags mirror this schema exactly — keep them in sync.
> 4. `protocol/verbs.go`: add, at the end beside the other verb types, `ToolListPacksArgs struct{}` and `ToolListPacksResult{Packs []ToolPackInfo}` with `ToolPackInfo{ID, Name, Icon, Transport string; Groups []string; Modules []ToolModuleInfo}` and `ToolModuleInfo{Key, Label, Group string}`. **`internal/protocol` must not import `internal/tool`** — these are standalone wire DTOs; the server maps `tool.Pack` → `ToolPackInfo`.
>
> **Tests:** extend `lab_test.go` — a valid single-link tool node passes; a tool node with no `config.pack` fails; `config.pack` present but empty fails; `config.pack` present but not a JSON string fails; a tool endpoint on `"eth0"` fails; a tool node referenced by two link endpoints fails; an existing iol/vpcs/nat lab still validates unchanged.
>
> **Acceptance:** compiles standalone (no dependency on `internal/tool`); all five build gates; `go test ./internal/lab/... ./internal/protocol/...` green. Existing `internal/server` tests must still pass — you are only *adding* an enum case.

---

### B03 — cap-transition helper binary

> **OWNS:** the new directory `tools/iolbox-toollaunch/` (`go.mod`, `main.go`, `launch_linux.go`, `launch_other.go`) — and nothing else. Do not modify `tools/p0-launcher/`; it stays as the P0 acceptance artifact.
> **Private prefix:** none required — this is its own `package main` in its own module, so in-package collisions with other batches are impossible.
>
> **Explicit exception to the shared preamble:** the preamble says "do not modify `go.mod`/`go.sum`". That rule is about not touching **existing** modules' manifests while other agents build against them. **You must create a brand-new `tools/iolbox-toollaunch/go.mod`** for this new standalone module, exactly as `tools/p0-launcher/`, `tools/p0-reaper/` and `tools/tool-stubgui/` each have their own. Copy the `go` directive and module-path style from `tools/p0-launcher/go.mod`. Add **no** dependencies: this module is stdlib-only, deliberately (the same reason `tools/p0-reaper` spells out its kernel ABI constants by hand rather than importing `golang.org/x/sys`). Do not touch any other module's `go.mod`/`go.sum`.
>
> Promote the proven P0 launcher (`tools/p0-launcher/launch_linux.go`, 218 lines, T0.2, **passed on real hardware**) into the production helper binary the supervisor execs.
>
> Why a separate binary rather than a Go pre-exec hook: the spec §2.5.2 transition requires `PR_SET_NO_NEW_PRIVS` → `PR_CAPBSET_DROP` loop → `PR_SET_SECUREBITS(SECBIT_KEEP_CAPS|SECBIT_NOROOT + locks)` → `capset` → `setgroups/setgid/setuid` → `PR_CAP_AMBIENT_RAISE` → `execve`, **in that exact order**. Go's threaded runtime cannot safely perform that sequence between fork and exec. P0 already proved the standalone-binary shape.
>
> Requirements:
> - `main.go`: usage `iolbox-toollaunch [--cgroup <path>] --user <name> --caps cap_net_raw -- <target> [args...]`. Read the P0 `main.go` for the existing flag/arg shape and keep it compatible. Fail loudly with a **distinguishable exit code + stderr message per step**, so `Detect` and `startToolNode` can report *which* step failed.
> - **`--cgroup <path>` (NEW — dispatch deviation D10, and the launcher-wrapper batch B08 depends on this contract).** When present, **before** any part of the privilege transition, write this process's own pid (decimal, no newline required but harmless) to `<path>/cgroup.procs`, and fail with its own distinguishable exit code if that write fails. Ordering is the whole point and must be doc-commented at the top of the function: at that moment the process is still root and still holds the capabilities needed to write a delegated `cgroup.procs`; after `capset`/`setuid` it does not. The `execve`d target then inherits cgroup membership, so its memory/pids/cpu limits bind before it can allocate — the same guarantee `clone3(CLONE_INTO_CGROUP)` gives, obtained without any Go-side pre-exec hook. When `--cgroup` is absent, do nothing (the supervisor placed us via `SysProcAttr.CgroupFD`).
> - `launch_linux.go`: lift the P0 implementation essentially as-is (`requireStartingPrivilege`, `capGet`/`capApply`, the bounding-set drop, securebits, `launchTransition`). **Preserve every comment that records a real-hardware finding.** It must end in `syscall.Exec` — never fork.
> - `launch_other.go`: `//go:build !linux` stub that exits with a clear "linux only" message, so the module builds on the dev box.
> - The binary assumes it is **already** inside the target netns (the supervisor arranges that via `ip netns exec`). It may or may not already be in the target cgroup — that is exactly what `--cgroup` covers. Document both at the top of `main.go`.
>
> **Acceptance:** from `tools/iolbox-toollaunch/`: `go build ./...` and `go vet ./...` native; **`GOOS=linux GOARCH=amd64 go build ./...` and `GOOS=linux GOARCH=amd64 go vet ./...`** — the vet gate is not optional, you are writing a `_linux.go` file that never compiles on the dev box otherwise. **Report the exact final argv contract in your summary, including the `--cgroup` flag's placement and its failure exit code** — the packaging batch (B09) and the launcher-wrapper batch (B08) both code against it, concurrently, without seeing your tree.

---

### B04 — manifest load + validate (T1.3)

> **OWNS:** `supervisor/internal/tool/manifest.go`, `supervisor/internal/tool/manifest_test.go`, `supervisor/internal/tool/testdata/packs/**`.
> **Private prefix: `manifest*`** — every unexported identifier you add starts with it (`manifestResolve`, `manifestCheckCaps`, …). Report the list.
>
> Implement **T1.3** (items #7, #8, #12). Portable, no syscalls beyond filesystem reads; tests run on Windows.
>
> Implement exactly these signatures (other agents are coding against them right now):
> - `func LoadPack(root string) (Pack, error)` — read `<root>/pack.json`, validate, resolve paths, return a `Pack`.
> - `func LoadPacks(dir string) ([]Pack, error)` — enumerate immediate subdirectories of `dir` (the pack root, `/opt/iolbox/tools/packs`), `LoadPack` each. **Partial-success semantics are part of the contract and must be doc-commented explicitly** (dispatch finding 12): the returned slice **always** contains every pack that loaded successfully, sorted by id, **even when the returned error is non-nil**; the error is an aggregate (`errors.Join`) of the per-pack failures and is a **warning, never fatal** — one malformed pack must not prevent the supervisor from offering the others. A missing `dir` yields an empty slice and a **nil** error. Say in the doc comment that the caller (`Server.InitRuntime`) is required to cache the slice regardless of the error and log the error at warn level.
> - `func (m Manifest) Validate() error`.
>
> Rules, from T1.3:
> - **#7, single source of truth:** the file's **doc comment must state** that `pack.json` is validated supervisor-side metadata ONLY — used for node-config validation and palette display — and that **the supervisor never executes from `pack.json`'s `modules` list**; the pack GUI's own compiled module defs remain authoritative for what actually runs. (The companion build-time `manifest_keys_test.go` runs in the pack's own build and is out of scope for this batch — see dispatch punch-list P20.)
> - **#8, path canonicalization:** every path in `pack.json` (`gui.bin` and each module's `script`) is **pack-relative**. Resolution = `filepath.Join(packRoot, p)` then `EvalSymlinks`/`Clean`, then assert the result is prefixed by `packRoot + separator`. Reject `..` and symlink escape. Use the `contained()` helper already in `tool.go` — do not write your own.
> - **#12, health path:** `gui.health` is a **required** non-empty string beginning with `/` (e.g. `"/healthz"`). Doc-comment why: the liveness/readiness probes hit exactly this path, so 200 means "serving" and 404/refused means "wedged or route missing" — the two are no longer conflated by an arbitrary `GET /`.
> - `manifestVersion` required; reject anything other than `ManifestVersion` (unknown major versions are rejected, not tolerated).
> - `caps` validated against `AllowedCaps` (NET_RAW only). Any other entry — notably `NET_ADMIN` — is a hard error naming the offending cap.
> - `id`, `name`, `gui.bin` required; `gui.transport` must be `"unix"` or `"tcp"` (default `"unix"`); module `key` non-empty and unique; each module's `group` must appear in `groups`.
> - If `Manifest.Limits` is nil, the `Pack` carries `DefaultLimits()`.
>
> **Already defined in `tool.go` — do not redefine:** `Manifest`, `GUI`, `Module`, `Field`, `Mitigation`, `Option`, `Pack`, `Limits`, `DefaultLimits`, `ManifestVersion`, `AllowedCaps`, `ScrubbedEnvAllowlist`, `contained`, `fileExists`, `ErrUnsupportedPlatform`.
>
> **Tests:** golden valid manifest under `testdata/packs/stub/` matching the B09 stub pack byte-for-byte in its field set; rejection cases for each rule above (missing `gui.health`, `gui.health` not starting with `/`, `NET_ADMIN` in caps, `manifestVersion: 2`, a `script` of `../../etc/passwd`, a duplicate module key, an unknown group); and — **required by finding 12** — `LoadPacks` over a dir containing **one valid and one malformed** pack asserts *both* that the valid pack is present in the returned slice *and* that the returned error is non-nil and names the malformed pack.
>
> **Acceptance:** compiles against `tool.go` only; all five build gates; portable tests green on Windows.

---

### B05 — durable install identity + object state file (T1.9)

> **OWNS:** `supervisor/internal/tool/instance.go`, `instance_lock_linux.go`, `instance_lock_other.go`, `instance_test.go`.
> **Private prefix: `inst*`.** Report the list.
>
> Implement **T1.9 bullets 1 and 3** — the durable identity and the state file. (Bullets 4–5, the `ReapStale` sweep itself, belong to a different agent working in `reap_linux.go`; do not write sweep logic.)
>
> Context that makes this batch load-bearing (T1.9 verbatim in substance): the reviewed design was self-contradictory — a per-start random UUID, or a `boot_id` which changes every reboot, *cannot* recognize a previous instance's leftover objects, which defeats the exact "clean up after `kill -9`" scenario the P0/P1 gates prove. The fix is a durable identity that survives process restart but is still scoped to this install, plus a state file — **not** a random tag embedded in kernel object names (which would collide with the deterministic `vtool<id>`/`iolt<id>` names fabric code requires, and blow the 15-char `IFNAMSIZ` limit).
>
> Implement:
> - `func InstanceID(stateDir string) (string, error)` — a UUID generated **once on first run** and persisted to `<stateDir>/instance-id` (mode `0600`), **read on every subsequent start and never regenerated while the file exists**. `flock` around the whole read-or-create. Same value across a crash-and-restart of the same install; a genuinely different install has its own data dir and therefore its own file. Generate the UUID from `crypto/rand` (v4 formatting by hand — no new dependencies).
> - `func LoadObjectState(stateDir string) (ObjectState, error)` — read `<stateDir>/tool-objects.json`; a missing file yields a zero-valued state and no error.
> - `func RecordObject(stateDir, instanceID string, rec ObjectRecord) error` — **called before each kernel object is created**; read-modify-write under the same flock, `0600`, fsync-then-rename for atomicity.
> - `func PruneObject(stateDir, instanceID string, nodeID int) error` — called after clean teardown.
> - Per dispatch-plan deviation D5, the flock lives in `instance_lock_linux.go` / `instance_lock_other.go` behind a small unexported interface (`instWithFileLock(path string, fn func() error) error`) — `syscall.Flock` is not portable, and the plan's "portable `instance.go`" label is not achievable as written. The `!linux` stub may use a plain `O_CREATE|O_EXCL` lockfile so tests run on the dev box.
> - Doc-comment the **charter assumption once**, from T1.9: iolbox is single-supervisor-per-host (`PLAN.md:4`); the durable-id + `<D>`-subtree scoping defends against cross-generation leaks (old process / prior crash) and against a genuinely separate install, **not** against an unsupported concurrent second supervisor sharing this install's data dir. Do not restate this per object elsewhere.
>
> **Already defined in `tool.go` — do not redefine:** `ObjectState`, `ObjectRecord`, `fileExists`, `contained`, `ErrUnsupportedPlatform`, naming helpers.
>
> **Tests (portable, `t.TempDir()`):** `InstanceID` returns the same value across repeated calls and across a simulated restart; it does not regenerate when the file exists; a record→load→prune round trip; two records for different node ids coexist; a state file belonging to a *different* instance id is loaded but its objects are not attributed to this id; a truncated/corrupt `tool-objects.json` is a reported error, not a panic.
>
> **Acceptance:** all five build gates; portable tests green on Windows.

---

### B06 — cgroup cage + delegated-root bring-up (T1.4, T1.11's cgroup mechanism)

> **OWNS:** `supervisor/internal/tool/cage.go` (**new, portable, no build tag**), `cage_linux.go`, `cage_other.go`, `cage_test.go` (**new, portable**), `cage_linux_test.go`.
> **Private prefix: `cage*`.** Report the list.
>
> Implement **T1.4** in full, plus the T1.11 cgroup bring-up sequence, which per dispatch-plan deviation D3 lives here as an exported function (`server.go` will only call it).
>
> **File split (dispatch finding 9 — revision 1 asked for portable tests from a batch that owned only `_linux` files, which is impossible):** all *pure logic* goes in the untagged `cage.go` and is tested from `cage_test.go` on the dev box — `/proc/self/cgroup` line parsing, the `<D>` path-selection rule below, `cgroup.events` parsing, `cpu.max`/`memory.max` value formatting, and the ordered list of writes `CreateCage` performs. All *syscalls and filesystem writes* stay in `cage_linux.go`, which calls into those pure functions. Anything needing a real cgroup fs goes in `cage_linux_test.go` behind a skip.
>
> The design is fixed and was proven on real hardware in P0/T0.3 — implement it exactly, do not redesign:
>
> cgroup v2 forbids a cgroup with any controller enabled in `cgroup.subtree_control` from also directly containing processes. The supervisor's own delegated cgroup `<D>` (read from `/proc/self/cgroup`; `Delegate=yes` guarantees write access) contains the supervisor process, so cages are **never** children of `<D>` directly. **3-level layout:** level 1 = `<D>` (controller-enabling root, holds no processes); level 2 = `<D>/supervisor/` (holds the supervisor's own PID); level 3 = `<D>/tool-<id>/` cage leaves, **siblings of `supervisor/`**.
>
> - `func InitCgroupRoot() (CgroupRoot, error)` — the T1.11 startup sequence **in this exact order**: (a) discover `<D>`; (b) create `<D>/supervisor/`; (c) migrate the supervisor's own PID into it (`echo $$ > <D>/supervisor/cgroup.procs`) so `<D>` is process-empty; (d) **only then** write `+memory +pids +cpu` to `<D>/cgroup.subtree_control`. Doing (d) before (c) fails `EBUSY` — **the guard is real, not theoretical (proven in T0.3); doc-comment it and never reorder.** No `/sys/fs/cgroup/iolbox/` hardcode anywhere — the whole subtree hangs off `<D>`, which is install-scoped by construction (a second install has a different service cgroup).
>
>   **Idempotency correction — this is a defect fixed in dispatch revision 2 (finding 11), implement it as written.** T1.11 requires a repeat call to succeed, but the naive discovery rule breaks its own idempotency: on the *first* call `/proc/self/cgroup`'s unified `0::` line yields `<D>`, and we migrate into `<D>/supervisor`; on a *second* call the same line now yields **`<D>/supervisor`**, and reapplying the rule would create `<D>/supervisor/supervisor` and enable controllers on the wrong node. Two mechanisms, both required:
>   1. **Path-selection rule (pure, in `cage.go`, exported to the package as `cageSelectRoot(procCgroupPath string) (delegated, leaf string)`):** if the last path element of the discovered cgroup is exactly `SupervisorLeafName` (`"supervisor"`, pinned in `tool.go`), then `<D>` is its **parent** and the leaf is the discovered path itself; otherwise `<D>` is the discovered path and the leaf is `<D>/supervisor`. Never append a second `supervisor` element.
>   2. **Cached root:** a package-level `cageRoot`/`cageRootOnce` (mutex- or `sync.Once`-guarded) holding the successfully-initialized `CgroupRoot`, so a genuinely re-entrant call inside one process returns it and performs **no** filesystem work at all.
>   The write steps must additionally each be individually idempotent (`MkdirAll`-style create; a migrate of a pid already in the target leaf is success; a `subtree_control` write whose controllers are already enabled is success).
> - `func CreateCage(root CgroupRoot, nodeID int, lim Limits) (path string, fd *os.File, err error)` — create the sibling leaf `<D>/tool-<id>/` using `CageName(nodeID)`; write `memory.max`, `pids.max`, `cpu.max`, `memory.swap.max=0` **before** returning, so limits bind before the child can allocate; open the directory with `O_PATH|O_DIRECTORY` and return the fd for `SysProcAttr.CgroupFD` (`clone3(CLONE_INTO_CGROUP)`, Go 1.22+). The caller closes the fd.
> - `func KillCage(path string) error` — write `1` to `cgroup.kill` (SIGKILL the whole subtree atomically). Note in the doc comment that this **terminates but does not reap** — reaping is the separate subreaper loop's job (T1.8).
> - `func CagePopulated(path string) (bool, error)` — parse `cgroup.events` for `populated 0/1` (parser in `cage.go`).
> - `func WaitCageEmpty(path string, timeout time.Duration) error`.
> - `func RemoveCage(path string) error` — best-effort rmdir; a missing cage is not an error.
> - `func ListCages(root CgroupRoot) ([]string, error)` — every `tool-*` leaf directly under `<D>`. This is what `ReapStale`'s belt-and-suspenders sweep uses; because `<D>` is install-scoped, it can never touch another install's cages. **`supervisor/` is never a cage** — exclude it explicitly, do not rely on the `tool-` prefix alone to do it silently.
> - `cage_other.go` (`//go:build !linux`): stubs for all of the above returning `ErrUnsupportedPlatform` / zero values, so portable callers (`server.go`) compile on the dev box.
>
> `tools/p0-stale/cleanup_linux.go` (`killCgroup`, `populatedZero`) is proven P0 code for the kill/populated logic — reuse its approach.
>
> **Already defined in `tool.go` — do not redefine:** `CgroupRoot`, `SupervisorLeafName`, `Limits`, `DefaultLimits`, `CageName`, `runCmds`, `runCmdsBestEffort`, `fileExists`, `ErrUnsupportedPlatform`.
>
> **Tests.** `cage_test.go` (portable, must pass on Windows): `cageSelectRoot` table test covering (a) a fresh `/system.slice/iolbox-supervisor.service` → `<D>` = that path, leaf = `.../supervisor`; (b) **the repeat call** `/system.slice/iolbox-supervisor.service/supervisor` → `<D>` = the parent, leaf = the input unchanged — this is the finding-11 regression guard and is required; (c) a path whose last element merely *contains* `supervisor` (e.g. `supervisor-2`) is **not** treated as the leaf; `cgroup.events` parsing against fixture strings including a malformed line; `cpu.max` formatting from `Limits`; the `CreateCage` write-order list asserts limits precede the fd open. `cage_linux_test.go`: anything requiring a real cgroup fs, behind a skip when `/sys/fs/cgroup/cgroup.controllers` is absent or the process is not root — never a test that fails on the dev box or on an unprivileged CI runner.
>
> **Acceptance:** all five build gates — in particular `GOOS=linux go vet ./internal/tool/...` must be clean, since none of your `_linux` code compiles natively on Windows — and `go test ./internal/tool/...` green on Windows, which is now meaningful because `cage.go` exists.

---

### B07 — netns + veth + /31 mgmt fallback (T1.5, T1.6)

> **OWNS:** `supervisor/internal/tool/netns.go` (**new, portable, no build tag**), `netns_linux.go`, `netns_other.go`, `netns_test.go`.
> **Private prefix: `netns*`.** Report the list.
>
> Implement **T1.5** and **T1.6**.
>
> **File split (dispatch finding 9):** the untagged `netns.go` holds the **pure command-list builders** — functions returning `[]cmdSpec` for create-netns, create-veth, attach, detach, delete, and the mgmt `/31` + firewall rule set — and `netns_test.go` asserts their exact argv and ordering on the dev box. `netns_linux.go` holds only the thin Linux entry points that hand those lists to `runCmds`/`runCmdsBestEffort`. Revision 1 asked for portable tests from a batch owning only `_linux` files; that was impossible, hence this split.
>
> **T1.5 — the pinned, collision-free sequence.** Implement exactly this ordering; it was proven in T0.4 including the hostile case of a host whose primary NIC is named `eth1`:
> ```
> ip netns add iolt<id>
> TMP=vtoolp<id>                       # deterministic temp name, unique per node, never "eth1"
> ip link add vtool<id> type veth peer name $TMP
> ip link set $TMP netns iolt<id>
> ip netns exec iolt<id> ip link set $TMP name eth1     # rename INSIDE the netns
> ip netns exec iolt<id> ip link set eth1 up
> ip netns exec iolt<id> ip link set lo up
> ip link set vtool<id> up
> ```
> The peer is **never** named `eth1` in the root netns — created under a unique temp name, moved, then renamed inside `iolt<id>`. The bridge-side `vtool<id>` **stays in the root netns** so the always-on directional classifier (`dirstat.Open`, `fabric_linux.go:346`) and bridge capture can bind it exactly as they bind an IOL/VPCS/NAT tap.
>
> **T1.6 — `/31` mgmt fallback + firewalling.** Used **only** when a pack manifest declares `gui.transport != "unix"`. When used, **document at the call site** that the ported interface-locks (`stripIfaceFlag`, `enforce_lab_iface`, `SO_BINDTODEVICE`) become **load-bearing again** — a second interface `mgmt0` now exists in the netns, so "exactly one non-lo interface" no longer holds by construction. Inside the netns install restrictive rules on `mgmt0`: default-drop FORWARD; allow only host↔`mgmt0` on the mgmt `/31`; no forwarding between `mgmt0` and `eth1`; host-only. AF_UNIX remains the default and needs none of this. The host-side end is `MgmtVethName(nodeID)` = `mtool<id>` and the netns side is `mgmt0` (dispatch punch-list P17). Carry the P19 file-level comment: this path is **unexercised at the P1 gate** because the only P1 pack is the `unix`-transport stub.
>
> Signatures other agents are coding against:
> `CreateNetns(nodeID int) error`, `CreateVethPair(nodeID int) error`, `AttachVethToBridge(nodeID int, br string) error` (`ip link set vtool<id> master <br>`), `DetachVethFromBridge(nodeID int) error` (`nomaster`), `DeleteNetns(nodeID int) error`, `DeleteVeth(nodeID int) error`, `SetupMgmt(nodeID int) (hostCIDR, guestCIDR string, err error)`, `TeardownMgmt(nodeID int) error`.
>
> **`NetnsExecArgs` is NOT yours.** Per dispatch finding 2 it lives in the frozen `tool.go` (B01, wave 1), because the launcher batch (B08) runs concurrently with you and consumes it. **Do not define it, do not move it, do not shadow it** — call `NetnsExecArgs(nodeID, argv)` when you need the `ip netns exec` prefix.
>
> Delete paths are **best-effort and idempotent** — a missing object is never an error (mirror `runCmdsBestEffort`, `extnet/endpoint_linux.go:89-103`).
>
> **Already defined in `tool.go` — do not redefine:** every naming helper (`NetnsName`, `HostVethName`, `PeerTempName`, `MgmtVethName`, `GuestIface`), `NetnsExecArgs`, `cmdSpec`, `runCmds`, `runCmdsBestEffort`, `ErrUnsupportedPlatform`.
>
> **Tests (`netns_test.go`, portable, must pass on Windows):** follow `internal/extnet/commands_test.go` — assert the exact argv sequence **and its ordering** for T1.5 (create-netns → add pair with temp name → move → rename-inside → up eth1 → up lo → up host side), that no emitted argv ever names `eth1` outside a `netns exec` prefix, and the exact mgmt rule set for T1.6. This is the only way T1.5's ordering is regression-protected, since no CI runs as root.
>
> **Acceptance:** all five build gates; portable command-builder tests green on Windows.
>
> *(Sizing note for the dispatching session, not for the agent: if this batch is judged too large, the only sanctioned split gives T1.6 its own exclusively-owned `mgmt.go`/`mgmt_linux.go`/`mgmt_other.go`/`mgmt_test.go`. A second agent must never be pointed at `netns_linux.go`.)*

---

### B08 — cap-transition launcher wrapper

> **OWNS:** `supervisor/internal/tool/launch_linux.go`, `launch_other.go`, `launch_test.go`.
> **Private prefix: `launch*`.** Report the list. (Your pure argv/env builders live in `launch_linux.go` but must be written as tag-free-able pure functions; if you find `launch_test.go` cannot reach them from the dev box, factor them into an untagged `launch.go` — **and report that you added a file**, since it is a change to your OWNS list.)
>
> Per the plan's file map, `launch_linux.go` is "the cap/securebits transition launcher (setpriv argv or the small native launcher fallback, §P0)". The transition *itself* is already implemented — the T0.2 decision is: **attempt the pinned `setpriv` argv first; fall back to the native launcher** if the installed `setpriv` cannot express the full securebits lock. Your job is the supervisor-side wrapper that chooses, builds argv, scrubs the environment, and assembles the `exec.Cmd`.
>
> Implement:
> - `func LauncherAvailable() (mode string, err error)` — probe `setpriv --version` and require util-linux ≥ 2.33 (ambient-caps support). Return `"setpriv"` or `"native"` (the helper binary at `/opt/iolbox/iolbox-toollaunch`, whose argv contract B03 publishes), or an error if neither is usable.
> - `func Launch(spec LaunchSpec) (*exec.Cmd, error)` — build and `Start()` the child. It must:
>   1. Prefix with the netns entry — call `NetnsExecArgs(spec.NodeID, inner)` from `tool.go`. **Do not rebuild that prefix and do not import it from `netns_linux.go`** (it is not there; see dispatch finding 2).
>   2. Then the transition. Pinned setpriv argv, exactly:
>      ```
>      setpriv --reuid ioltool --regid ioltool --clear-groups --no-new-privs \
>              --bounding-set -all,+cap_net_raw \
>              --inh-caps    -all,+cap_net_raw \
>              --ambient-caps -all,+cap_net_raw \
>              -- <target> <args...>
>      ```
>      Or the native helper `/opt/iolbox/iolbox-toollaunch --user ioltool --caps cap_net_raw -- <target> <args...>` with the equivalent flags.
>   3. Set `SysProcAttr.CgroupFD = int(spec.CgroupFD.Fd())` with `UseCgroupFD = true` — atomic `clone3(CLONE_INTO_CGROUP)` placement, so limits bind before the child can allocate.
>   4. **Cgroup-placement fallback (corrected in dispatch revision 2 — finding 6 / deviation D10; the previous instruction was unimplementable).** If the `UseCgroupFD` start fails (old kernel, or the fd path is unavailable), **do not attempt any Go-side pre-exec `cgroup.procs` write** — `os/exec` provides no safe arbitrary pre-exec hook from Go's threaded runtime, which is the same reason the helper binary exists at all. Instead, fall back to `mode == "native"` and pass **`--cgroup <spec.CgroupPath>`** to `/opt/iolbox/iolbox-toollaunch`, which writes its own pid into that cgroup **while it is still root**, before its privilege transition and its final `execve`. If `mode` is `"setpriv"` and `UseCgroupFD` is unavailable, switch to the native helper for this launch rather than launching un-caged; an un-caged tool process is a hard failure, not a degraded success. Doc-comment all of this at the fallback site.
>   5. Set `Setpgid: true` (the bounded SIGTERM-to-process-group stop in T1.10/§5.6 depends on it, mirroring VPCS).
>   6. Build the environment from `ScrubbedEnvAllowlist` **only** — never inherit the supervisor's environ wholesale.
>   7. Register the started PID with `tool.Registry` (`Registry.Add(cmd.Process.Pid)`) **immediately after `Start()`**, with no intervening work, before returning. It is the caller's job to `Registry.Remove` when its `cmd.Wait()` returns. Doc-comment that this is what stops the process-wide subreaper loop from stealing this direct child's exit status (T1.8).
> - `func ScrubEnv(extra map[string]string) []string` — allowlist filter + the per-node additions: `IOLBOX_TOOL_SOCK`, **`IOLBOX_TOOL_OPTIONS`** (note the name — it is `OPTIONS`, not `OPTS`; the P0-proven GUI `tools/tool-stubgui/main.go:25` hard-exits if it is unset, see dispatch deviation D9), `IOLBOX_PACK_DIR`, `IOLBOX_NODE_ID`.
> - `launch_other.go`: stubs returning `ErrUnsupportedPlatform`.
>
> **Already defined in `tool.go` — do not redefine:** `LaunchSpec`, `ScrubbedEnvAllowlist`, `NetnsExecArgs`, `OptionsFile`, `Registry`/`PIDRegistry`, `cmdSpec`, `runCmds`, `ErrUnsupportedPlatform`.
> **Defined by B03 in a separate module — call by path/argv, do not write:** `/opt/iolbox/iolbox-toollaunch` including its `--cgroup` flag.
> **You have no dependency on the netns batch.** If you think you do, you have rebuilt something that is already in `tool.go` — stop and report.
>
> **Tests (`launch_test.go`, portable):** assert the pinned setpriv flag order **verbatim**; assert the netns prefix precedes the transition which precedes the target; assert the native-helper argv including `--cgroup <path>` placement matches B03's published contract; assert `ScrubEnv` lets through exactly the allowlist and nothing else (feed it a `SECRET=1` and an `IOLBOX_TOOL_OPTS=…` and assert both are dropped — the second is the deviation-D9 regression guard).
>
> **Acceptance:** all five build gates; portable tests green.

---

### B09 — packaging / rootfs (T1.11 systemd half + stub pack)

> **OWNS:** `runtime/files/iolbox-supervisor.service`, `runtime/build-rootfs.sh`, `runtime/files/prestart-clean.sh`, and the new `runtime/files/tools/packs/stub/pack.json`.
> **Private prefix:** n/a (no Go).
>
> No Go. Make the appliance rootfs able to actually run a tool node. Three sources: T1.11's systemd requirement, the P0 real-hardware run log in `docs/tests/p0-spike.md` (which lists exactly what the appliance did **not** ship and had to be installed ad hoc — explicitly deferred to P1 as "a packaging concern"), and spec §2.4/§7.
>
> 1. **`iolbox-supervisor.service`** — add `Delegate=yes` **and** `Delegate=memory pids cpu` for explicitness (T1.11), plus `CPUAccounting=yes`, `MemoryAccounting=yes`, `TasksAccounting=yes`, `IOAccounting=yes`. The P0 run log is unambiguous that without a properly delegated scope the `cpu` controller is not even listed and the whole cgroup path fails immediately. Add a comment block explaining, in the file's existing house style, that the supervisor owns a writable delegated sub-cgroup `<D>` and migrates itself into `<D>/supervisor/` so `<D>` can hold controllers (the cgroup v2 no-internal-processes rule). Keep `KillMode=control-group`; do **not** add sandboxing directives.
> 2. **`build-rootfs.sh`** — install: util-linux `setpriv` ≥ 2.33, `libcap2-bin` (`capsh`), `python3` + `python3-scapy`; create the unprivileged system account `ioltool` (`useradd -r -M -s /usr/sbin/nologin ioltool`); create `/opt/iolbox/tools/` with `packs/` and the vendored venv location from spec §2.4 (venv population itself is P2 — create the directory and document the gap); create `/run/iolbox/tool/` root-owned `0755` (traversable, not writable — the T0.5-proven permission shape); build and install `tools/iolbox-toollaunch` to `/opt/iolbox/iolbox-toollaunch` (mode `0755`, root-owned) alongside the supervisor binary; install `runtime/files/tools/packs/stub/pack.json` to `/opt/iolbox/tools/packs/stub/pack.json` and the built `tools/tool-stubgui` binary to `/opt/iolbox/tools/packs/stub/tool-stubgui`. Everything root-owned, `0755`/`0644`, immutable in the shipped image (spec §6.1).
> 3. **`prestart-clean.sh`** — keep its blunt best-effort `iol*` sweep for the catastrophic case (T1.9 is explicit that it stays); add a comment that the precise, durable path is the in-process `tool.ReapStale()`, and that the existing `iol*` link loop already covers `iolt*`/`vtool*` incidentally. Add removal of `/run/iolbox/tool/*` socket dirs (which also removes each node's `options.json`, a per-run file — say so). Do not add a cgroup sweep here — that is `ReapStale`'s job and doing it blindly from a shell script would cross install boundaries.
> 4. **`runtime/files/tools/packs/stub/pack.json`** — the stub pack fixture the P1 gate loads (punch-list P11). Pinned: `manifestVersion: 1`, `id: "stub"`, `name: "Stub Tool"`, `interpreter: "none"`, `gui: {bin: "tool-stubgui", transport: "unix", console: "http", health: "/healthz"}`, `caps: ["NET_RAW"]`, empty `options`, `groups: []`, `modules: []`. `/healthz` is the concrete value the T1.7 readiness/liveness probes hit (items #11/#12) and is served by `tools/tool-stubgui/main.go:72`. Field names must match the `Manifest` struct tags in `supervisor/internal/tool/tool.go` exactly.
>
> **Note for step 2 (dispatch deviation D9):** the stub GUI requires **both** `IOLBOX_TOOL_SOCK` and `IOLBOX_TOOL_OPTIONS` to be set and requires the options file to already exist and be readable/writable by `ioltool`. The **supervisor** creates that file at start time (`options.json` inside the per-node `0700` socket dir) — do **not** ship a static options file in the pack directory, and do not make `/opt/iolbox/tools/packs/` writable to work around it.
>
> **Acceptance:** `sh -n` / `bash -n` clean on every edited script; `systemd-analyze verify` on the unit if available, otherwise a careful read against the existing file's conventions. Do not run the rootfs build. Report the exact installed paths in your summary.

---

### B10 — register EXISTING direct children with `tool.Registry` (NEW in revision 2)

> **OWNS, exhaustively — these six files and nothing else:**
> `supervisor/internal/node/spawn_linux.go` · `supervisor/internal/bcap/capture_linux.go` · `supervisor/internal/extnet/endpoint_linux.go` · `supervisor/internal/extnet/detect_linux.go` · `supervisor/internal/fabric/manager_linux.go` · `supervisor/internal/fabric/detect_linux.go`
> **Private prefix:** these are four different packages, none of them `tool`, so in-package collision with concurrent batches is impossible. Add **no** new helpers at all if you can avoid it; if one file genuinely needs one, prefix it `toolreg` and report it.
>
> **Why this batch exists (read this before touching anything).** T1.8's subreaper design has exactly one safety invariant: *the reap loop peeks non-destructively, and reaps only a pid that is **absent** from `tool.Registry`, so it can never steal a direct child's exit status.* `PR_SET_CHILD_SUBREAPER` is set with `prctl` on the **whole supervisor process** — it is not scoped to tool nodes. From the moment `Server.InitRuntime` sets it, the loop receives SIGCHLD for **every** child the binary spawns. So the invariant only holds if **every** direct `exec.Cmd` child in the entire supervisor is registered — not just the ones tool nodes create. Revision 1 of this dispatch wired registration into new tool code only; this batch closes the hole. Failure mode if it is skipped: the loop's `wait4(pid, WNOHANG)` collects an IOL node's exit status first, the owning `cmd.Wait()` then returns `ECHILD`, and the node silently never transitions to `crashed`/`stopped` — or an `extnet` setup command reports a spurious failure. This is a real-hardware-class bug, not a theoretical one.
>
> **The mechanical rule, applied at every site below:**
> ```go
> if err := cmd.Start(); err != nil { /* existing error path, unchanged */ }
> tool.Registry.Add(cmd.Process.Pid)          // immediately, no intervening work
> ...
> err := cmd.Wait()
> tool.Registry.Remove(cmd.Process.Pid)       // immediately after Wait returns
> ```
> **`cmd.Run()` must be split.** `Run()` is `Start()` + `Wait()` with no window in between, so there is no place to register the pid. Rewrite each `Run()` call site as an explicit `Start()` / `Add` / `Wait()` / `Remove` sequence that **preserves the existing error semantics exactly** — same wrapped error text, same `context.DeadlineExceeded` check, same stdout/stderr buffers, same return values. If `Start()` fails there is no pid and nothing to register or remove. This is a mechanical refactor: **do not change behaviour, timeouts, error strings, or control flow.**
>
> **The six sites, exhaustively (verified in the current tree — if you find a seventh in these six files, register it too and call it out in your summary):**
> 1. **`internal/node/spawn_linux.go`** — the IOL path: `pty.Start(cmd)` around `:119` starts a direct child (pty.Start calls `cmd.Start()` internally, so register from `cmd.Process.Pid` immediately after it returns successfully), reaped by `(*Process).wait`'s `p.cmd.Wait()` at `:396` — remove there. And the VPCS path: `cmd.Start()` at `:171`, reaped by the `go func() { _ = cmd.Wait() }()` at `:188` — register after `:171`, remove inside that goroutine after `Wait()` returns. **Note the VPCS subtlety in a comment:** vpcs daemonizes, so that `Wait()` returns almost immediately and the *real* vpcs lives on in the process group — the registry entry is for the short-lived launcher only, which is correct, because the daemonized grandchild is precisely the kind of re-parented orphan the subreaper loop is *supposed* to be able to reap.
> 2. **`internal/bcap/capture_linux.go`** — `cmd.Start()` at `:52` (the `sudo -n tcpdump` child), reaped by `_ = c.cmd.Wait()` in `Close` at `:119`. Register after `:52`, remove after `:119`'s `Wait()`.
> 3. **`internal/extnet/endpoint_linux.go`** — **two** sites, both currently `cmd.Run()`: inside `runCmds` at `:70`/`:73` and inside `runCmdsBestEffort` at `:95`/`:97`. Split both per the rule above; these run on every endpoint setup and teardown, so they are the highest-frequency unregistered children in the binary. Keep `context.CommandContext`'s timeout behaviour and the `ctx.Err() == context.DeadlineExceeded` branch byte-identical in effect.
> 4. **`internal/extnet/detect_linux.go`** — `exec.Command("sudo","-n","true").Run()` at `:51`. Also a direct child. Split it: keep the function's `bool` return and its exact semantics (`== nil`).
> 5. **`internal/fabric/manager_linux.go`** — `runOne`'s `cmd.Run()` at `:109`/`:112`. Split per the rule; preserve the `(string, error)` return and the deadline branch.
> 6. **`internal/fabric/detect_linux.go`** — `exec.Command("sudo","-n","true").Run()` at `:11`, same shape as site 4.
>
> **Explicitly EXCLUDED — and you must state this decision in your summary:** `supervisor/cmd/supervisor/main.go:136` runs `exec.Command("hostid").Output()`. It is reached only from `hostID()` ← `generateIourc()` ← the `-print-iourc` flag branch at `main.go:56`, which runs **before `server.New(...)` is ever constructed** and therefore long before `InitRuntime` sets `PR_SET_CHILD_SUBREAPER`; that path also exits the process immediately afterwards. There is no reap loop in existence when it runs, so registering it would be dead code that adds an `internal/tool` import to `package main` for no reason. **Do not register it. Do not edit `cmd/supervisor/main.go` at all** — another batch owns that file. Confirm in your summary that you checked this site and excluded it for this reason.
>
> **Import note.** Each of the four packages you touch gains an `internal/tool` import. That is a legal direction — `internal/tool` imports none of `node`, `bcap`, `extnet`, `fabric`, so no cycle is possible. `tool.Registry` and `PIDRegistry` are declared in the **portable** `tool.go`, so all six files still compile under both build tags.
>
> **A note on the narrow start race, to be doc-commented once (in `spawn_linux.go`) and not re-litigated:** between `Start()` returning and `Add(pid)` executing there is a microscopic window in which a child that exits instantly could be peeked by the loop and reaped as an "orphan". This is the exact shape the P0 reaper fixture proved on real hardware (`tools/p0-reaper/reaper_linux.go:85-90` registers immediately after `cmd.Start()`), the loop polls at 10 ms, and the mitigation is simply that `Add` is the **very next statement** after `Start` with no logging, no state-machine transition and no allocation in between. Do not invent a lock, a pre-registration scheme, or a "reserve the pid" mechanism — that would be a design change, and design changes are out of scope.
>
> **Already defined in `tool.go` — do not redefine:** `Registry`, `PIDRegistry` and all its methods.
>
> **Tests.** Add no new test files (you own none). Your correctness bar is: **every existing test in these four packages still passes unchanged**, which is exactly the right bar for a behaviour-preserving refactor. Run `go test ./internal/node/... ./internal/bcap/... ./internal/extnet/... ./internal/fabric/...` and report the result.
>
> **Acceptance:** all five build gates. `GOOS=linux go build ./...` and `GOOS=linux go vet ./...` are the load-bearing ones — **every file you touch is `_linux`-tagged and none of them compile on the dev box's native build.** In your summary, list all six sites with their final line numbers, confirm the `main.go` exclusion, and state explicitly whether you found any additional direct-child site inside your six files.

---

### B11 — subreaper reap loop + `ReapStale` (T1.8, T1.9)

> **OWNS:** `supervisor/internal/tool/reap.go` (**new, portable, no build tag**), `reap_linux.go`, `reap_other.go`, `reap_test.go` (portable), `reap_linux_test.go` (**new, `//go:build linux`**).
> **Private prefix: `reap*`.** Report the list.
>
> Two related mechanisms, both in this file group per the plan's file map. Per dispatch finding 9, the **decision logic** lives in the untagged `reap.go` and is tested from `reap_test.go` on the dev box; the syscalls live in `reap_linux.go` and get a real Linux-only test in `reap_linux_test.go`.
>
> **T1.8 — the ownership-split subreaper loop.** Stated unambiguously in the plan, proven in T0.6 on real hardware; implement exactly, do not "simplify":
> `exec.Cmd.Wait()` remains **authoritative for every DIRECT child** the supervisor spawns — the pack GUI, and also (as of dispatch finding 1, already landed by batch B10 in the previous wave) IOL/VPCS, the `tcpdump` capture, and every `ip`/`sudo` command in `extnet` and `fabric`. This loop handles **ONLY re-parented orphans** — grandchildren the pack GUI spawned (e.g. a scapy attack script) that were orphaned when the GUI died without reaping them; these reparent to the supervisor via `PR_SET_CHILD_SUBREAPER` and are **never** a PID any `exec.Cmd` is waiting on. The loop lives at **supervisor scope**, not per-endpoint, because orphans reparent to the supervisor, not to any Endpoint. Never a blind `wait4(-1)` reap.
>
> **The peek mechanism is PINNED and non-negotiable — this is a bug that P0/T0.6 already found and fixed on real hardware, and dispatch revision 1 reintroduced it in prose (finding 4).**
> - **`wait4` does not accept `WNOWAIT`.** The kernel's `kernel_wait4()` screens its options argument against `~(WNOHANG|WUNTRACED|WCONTINUED|__WNOTHREAD|__WCLONE|__WALL)` and returns **`EINVAL`** for any other bit; `WNOWAIT` (`0x01000000`) is not in that set. A "peek" built on `wait4` therefore **never peeks** — every call fails with `EINVAL` before it can report anything, and the ownership split silently degrades to "reap nothing" or, worse, to a destructive `wait4`. **`wait4` with `WNOWAIT` is forbidden anywhere in this batch.** Do not write it in a comment as an alternative either.
> - **The peek must be raw `waitid(2)`,** because `WNOWAIT` is a waitid-only option and `package syscall` exposes no `Waitid` wrapper on linux/amd64. **Copy the proven implementation from `tools/p0-reaper/reaper_linux.go` (353 lines, passing on real hardware) — specifically `peekReapable` at `:217-233`, its constant block at `:19-31`, and its `siginfo` struct at `:44-62` — and adapt it into `reap_linux.go`.** That means, concretely, all of:
>   - hand-declared kernel ABI constants `pAll = 0` (`P_ALL`), `wNoHang = 0x00000001`, `wExited = 0x00000004`, `wNoWait = 0x01000000`, spelled out because **this package must not gain a `golang.org/x/sys` dependency** (the module has exactly one third-party dep, `github.com/creack/pty`) — the same reason `p0-launcher` spells out its prctl/capset constants;
>   - the hand-laid-out 128-byte `siginfo` struct with **its layout comment preserved verbatim**: on x86-64 the union is 8-byte aligned and starts at offset **16, not 12** (`__ARCH_SI_PREAMBLE_SIZE == 4 * sizeof(int)`), so `si_pid` is an `int32` at offset 16. Keep the two compile-time size assertions (`_ = unsafe.Sizeof(siginfo{}) - 128` and `_ = 128 - unsafe.Sizeof(siginfo{})`) — a short or long struct makes one negative and fails the build, which is the only cheap guard against a silent ABI mismatch;
>   - the call itself: `syscall.Syscall6(syscall.SYS_WAITID, pAll, 0, uintptr(unsafe.Pointer(&info)), wExited|wNoHang|wNoWait, 0, 0)`;
>   - the errno handling: `0` → return `int(info.pid)` (a `WNOHANG` waitid with nothing ready **still succeeds** and reports emptiness by writing `si_signo == 0` and `si_pid == 0`, so pid 0 means "nothing ready", not an error); `ECHILD` and `EINTR` → return `0, nil` (no children, or an interrupted poll — the caller just polls again); anything else → return the errno.
> - **The reap is the second, separate call, and only for an unregistered pid:** `pid, err := reapPeek()`; if `err == nil && pid > 0 && !reg.Contains(pid)` then `syscall.Wait4(pid, &status, syscall.WNOHANG, nil)` — **no `WNOWAIT` on this one**; it is the destructive collection, and it is scoped to a single specific pid, never `-1`. A peeked pid **in** the registry is left untouched, still reapable, for its owner's `cmd.Wait()`. Mirror `reapLoop` at `p0-reaper/reaper_linux.go:235-257`, including its 10 ms sleep and its `select` on a stop channel.
>
> Implement: `func SetSubreaper() error` (`prctl(PR_SET_CHILD_SUBREAPER, 1)` — constant `36`, hand-declared) and `func StartReaper(reg *PIDRegistry) (stop func())` returning a `stop` closure that is **idempotent** (a second call is a no-op) and that **returns only after the loop goroutine has exited**, because its caller invokes it during shutdown and must not race the process exit.
>
> **T1.9 bullets 4–5 — `ReapStale`.** `func ReapStale(cfg ReapConfig) error`, called once at supervisor startup. It:
> 1. reads the state file for **THIS install's durable id** and destroys exactly the objects it lists — which, because the id is durable, are the prior (crashed) run's leftovers of the **same** install — via `KillCage` + `WaitCageEmpty` + `RemoveCage`, `DeleteNetns`, `DeleteVeth`, socket-dir rm (which removes that node's `options.json` with it);
> 2. then **best-effort sweeps `<D>`** (`ListCages`) for any `tool-*` leaf not in the file — belt-and-suspenders; `<D>` is install-scoped by construction, so this can never touch another install's cages;
> 3. **never** wildcard-sweeps host-global `iolt*`/`vtool*` names that could belong to a different install. This prohibition is load-bearing — do not add a convenience `ip netns list | grep iolt` sweep.
> All steps best-effort and idempotent; a missing object is not an error. Prune the state file for every object successfully destroyed.
>
> `reap_other.go`: stubs — `SetSubreaper` returns nil, `StartReaper` returns a no-op `stop`, and **`ReapStale` off Linux is a no-op returning nil**, so `server.go` can call it unconditionally.
>
> **Already defined in `tool.go` — do not redefine:** `PIDRegistry`, `Registry`, `ReapConfig`, `ObjectState`, `ObjectRecord`, naming helpers, `cmdSpec`, `runCmdsBestEffort`.
> **Defined by earlier batches — call, do not write:** `LoadObjectState`/`PruneObject`/`InstanceID` (instance.go); `KillCage`/`WaitCageEmpty`/`RemoveCage`/`ListCages` (cage_linux.go); `DeleteNetns`/`DeleteVeth` (netns_linux.go).
> **Assume, do not re-verify:** every pre-existing direct `exec.Cmd` child in the supervisor is already registered with `tool.Registry` — batch B10 did that in the previous wave. Do **not** edit any file outside `internal/tool` to "make sure".
>
> **Tests.** `reap_test.go` (portable, dev box): factor "given this state file and this cage listing, which objects should be destroyed, in what order" into a pure function in `reap.go` and table-test it — including the case where the state file belongs to a **different** instance id (nothing destroyed) and the belt-and-suspenders case (a `tool-*` leaf not in the file **is** swept, and `supervisor` never is). `reap_linux_test.go` (`//go:build linux`, skips unless it can fork): a real end-to-end of the ownership split modelled on the P0 fixture — start a registered child and an unregistered orphan, assert the registered child's status is delivered to its own `cmd.Wait()` (not stolen) and the orphan is collected by the loop and its `/proc` entry disappears.
>
> **Acceptance:** all five build gates; portable tests green. **In your summary, quote the exact `Syscall6` line and the four constant values you used**, so the dispatching session can diff them against `tools/p0-reaper/reaper_linux.go` without opening your tree.

---

### B12 — Endpoint lifecycle (T1.7 + the `tool.Start/Stop/Attach/Detach/State` surface)

> **OWNS:** `supervisor/internal/tool/endpoint_linux.go`, `endpoint_other.go`, `endpoint_test.go`.
> **Private prefix: `endpoint*`.** Report the list.
>
> This is the batch that assembles every primitive the other agents built into the actual node lifecycle. Mirror the structure of `internal/extnet/endpoint_linux.go` (356 lines) closely — especially its **reverse-what-you-did teardown discipline** (`endpoint_linux.go:110-168`) and its best-effort idempotent teardown (`:89-103`).
>
> Surface (from spec §2.2, and referenced by four downstream batches):
> `type Endpoint struct{...}`; `func Start(cfg Config) (*Endpoint, error)`; `func (e *Endpoint) Stop() error`; `func (e *Endpoint) AttachBridge(br string) error`; `func (e *Endpoint) DetachBridge()`; `func (e *Endpoint) State() string`; `func (e *Endpoint) PID() int`; `func (e *Endpoint) HostVeth() string`.
>
> **`Start` order (spec §5.7 / T1.10), each step recorded to the state file *before* the object is created (T1.9):**
> per-node preclean (re-run this node's teardown before creating anything — mirrors `extnet.Start`'s opening `runTeardown`, with **one retry after a short pause on EBUSY**, the exact self-heal `extnet.setupWithRetry` uses) → `CreateCage` + limits → `CreateNetns` + `CreateVethPair` → socket dir `/run/iolbox/tool/<id>` **owned by `ioltool`, mode `0700`** (parent `/run/iolbox/tool/` root-owned `0755`, traversable not writable — the T0.5-proven shape) → **write the options file (see below)** → `Launch` into the cgroup with the cap transition → start the exit-watcher → await readiness → `running`.
> **Any error after cgroup creation runs the full teardown** (`cgroup.kill` then rmdir, netns del, veth del, socket dir rm). Teardown is idempotent and best-effort; missing objects are not errors.
>
> **Options file — REQUIRED, and the node cannot start without it (dispatch deviation D9 / finding 5).** Immediately after creating the socket dir and **before** `Launch`, write `OptionsFile(cfg.RunDir, cfg.NodeID)` = `<runDir>/tool/<id>/options.json`:
> - content = `cfg.Options` if non-empty, else the two bytes `{}` — **never** leave the file absent or empty;
> - written **atomically**: create a temp file in the *same* directory, write, `chown` to the `ioltool` uid/gid, `chmod 0600`, `fsync`, `rename` over the target. Same-directory rename so it is atomic on the same filesystem, and chown-before-rename so the file is never briefly visible as root-owned;
> - uid/gid resolved with `os/user.Lookup(cfg.User)` (default `"ioltool"`); a missing account is a hard `Start` error naming the account (punch-list P18 handles the *detect*-time degradation; at start time it is a failure);
> - `IOLBOX_TOOL_OPTIONS` in the launch environment points at this exact path.
> **Why this is load-bearing, doc-comment it:** the P0-proven GUI (`tools/tool-stubgui/main.go:24-45`) hard-exits when `IOLBOX_TOOL_OPTIONS` is unset, when the file cannot be read, **or** when it cannot be written back — it deliberately appends to the file to prove an unprivileged `ioltool` process can round-trip a `0600` file it owns. Ownership `ioltool:ioltool` and mode `0600` are therefore not cosmetic; get either wrong and the readiness probe times out with no useful error at the P1 gate.
>
> **T1.7 — exit-watcher, readiness, liveness:**
> - Exit-watcher: a `cmd.Wait()` goroutine → `running → crashed` on **unexpected** exit + teardown. It must `Registry.Remove(pid)` **immediately** when `Wait()` returns. Guard on the endpoint's own state so a `Stop()`-initiated exit is treated as expected, not a crash (exactly `Process.wait`'s `StateStopped` guard, `spawn_linux.go:399-402`).
> - Readiness: after launch, HTTP `GET <gui.health>` over the AF_UNIX socket until 200 or a bounded timeout, **then** flip `running`. Use the values pinned in dispatch punch-list P15/P16: **10 s bound, 100 ms polls**.
> - Liveness: periodic `GET <gui.health>`, **every 5 s, N = 3** consecutive non-200 → `crashed` + `cgroup.kill`. Probes hit `gui.health` specifically, **never an arbitrary path** — a 404 must be distinguishable from "serving".
>
> **`Stop` (spec §5.6):** transition to `stopped` **first** (so the exit-watcher treats the imminent exit as expected) → `SIGTERM` to the GUI **process group** (spawned `Setpgid`, like VPCS), wait bounded (5 s, sized like `vpcsConsoleReadyTimeout`) → if `cgroup.events:populated` is still non-empty, `cgroup.kill` (SIGKILL the whole subtree atomically — stronger than VPCS's argv/port matching because cgroup membership is exact) → `DetachBridge` → delete veth, delete netns, rmdir cgroup, remove socket dir (taking `options.json` with it), prune the state-file record. All best-effort/idempotent.
>
> **`AttachBridge`/`DetachBridge`** are pure `ip link ... master/nomaster` on `vtool<id>` and must **never restart the child** — this is the hot-connect seam the whole design rests on, and the P1 gate asserts the child PID is unchanged across both.
>
> **Already defined — do not redefine:** everything in `tool.go` (including `OptionsFile` and `NetnsExecArgs`); `LoadPack` (manifest.go); `RecordObject`/`PruneObject`/`InstanceID` (instance.go); `CreateCage`/`KillCage`/`CagePopulated`/`WaitCageEmpty`/`RemoveCage` (cage_linux.go); `CreateNetns`/`CreateVethPair`/`AttachVethToBridge`/`DetachVethFromBridge`/`DeleteNetns`/`DeleteVeth` (netns_linux.go); `Launch`/`ScrubEnv` (launch_linux.go).
>
> `endpoint_other.go`: exactly mirror `extnet/endpoint_other.go` — an empty `Endpoint` struct so signatures resolve off Linux, `Start` returning `ErrUnsupportedPlatform`, no-op `Stop`/`AttachBridge`/`DetachBridge`/`State`/`PID`/`HostVeth`. **This file is what lets `internal/server` compile on the dev box — get it right.**
>
> **Tests (`endpoint_test.go`, portable):** the readiness/liveness decision logic (given a sequence of probe results and the pinned cadence, when do we flip state? assert exactly 3 consecutive non-200s, not 2, trip liveness); the teardown-step ordering (factor the reverse-order teardown into a pure step list and assert it is the exact reverse of the setup list); the options-file **content** rule (nil `Config.Options` → the two bytes `{}`). Real-kernel tests (ownership/mode of the written file, the actual launch) go behind a root+Linux skip.
>
> **Acceptance:** all five build gates. `GOOS=linux go build ./...` must succeed with **all** wave-2 files present — if a sibling function signature differs from this prompt, **report the discrepancy rather than editing the sibling's file**.
>
> *(Sizing note for the dispatching session, not the agent: a future split could carve the socket/options/health helpers into a prior batch. Not required for this revision — the file is single-owner and the dependencies are all satisfied.)*

---

### B13 — `Detect` capability matrix (T1.12)

> **OWNS:** `supervisor/internal/tool/detect.go` (**new, portable, no build tag**), `detect_linux.go`, `detect_other.go`, `detect_test.go` (portable), `detect_linux_test.go` (**new, `//go:build linux`**).
> **Private prefix: `detect*`.** Report the list.
>
> Implement **T1.12**. `func Detect(root CgroupRoot) Capabilities`.
>
> Per dispatch finding 9, the untagged `detect.go` holds the pure parts — the matrix aggregation, the `Reasons` map assembly, the ordered probe-step list, and the per-step reason strings — so `detect_test.go` can exercise them on the dev box. `detect_linux.go` holds the actual probe; `detect_linux_test.go` holds its root-gated real run.
>
> The critical constraint, quoted from T1.12: the probe creates its cage as a **level-3 leaf under the same delegated root `<D>`** as production tool cages — a sibling of `<D>/supervisor/`, after the migrate-first controller enable — **not at fs root**, so a pass genuinely predicts production success. *A probe that enabled controllers on a different, empty parent would not prove the real `<D>` accepts `subtree_control`.* Therefore: call the **production** `CreateCage`/`CreateNetns`/`CreateVethPair`/`Launch` functions — do not write a parallel probe implementation of any of them.
>
> Matrix: `{netnsCreate, vethCreate, vethMoveRename, cgroupDelegated, ambientCapTransition, unixProxy}`. `cgroupDelegated` fails if the migrate-first + `subtree_control` sequence errors on the real `<D>`. Advertise `"tools"` only if all required pass; record a **specific failure reason** per primitive in `Capabilities.Reasons` (spec §4.3) so `node.start` can reject with `unsupported` and the cached reason.
>
> Probe procedure (spec §4.3): throwaway `iolprobe` netns; veth pair; move one end in; probe cgroup as a level-3 leaf under `<D>` with `pids.max`/`memory.max` set; run a trivial process into cgroup+netns with the §2.5.2 cap transition; assert `/proc/self/status` shows exactly `CAP_NET_RAW`. **Tear all of it down and verify removal** (netns gone, veth gone, cgroup rmdir'd) — a probe that leaks is a failed probe. `unixProxy` = can bind and dial an AF_UNIX socket under the socket-dir path with the pinned ownership/mode (punch-list P14 — it does **not** depend on `internal/wsbridge`, which is P2). Resolve the `ioltool` account with `os/user.Lookup` here; a missing account fails `ambientCapTransition` with a specific reason and the runtime degrades to "tools unsupported", exactly as `natgw` degrades (punch-list P18).
>
> Use a node id far outside the lab range for probe object names so a probe can never collide with a live node.
>
> `detect_other.go`: mirror `extnet/detect_other.go` — `Detect` returns a zero `Capabilities{}` off Linux, so the dev box never advertises the feature.
>
> **Already defined — do not redefine:** everything in `tool.go`; the cage/netns/launch functions listed above.
>
> **Tests.** `detect_test.go` (portable): `Capabilities.OK`/`GateFeatures`/`Reasons` aggregation against injected matrices (mirror `extnet/extnet_test.go`'s injected-`Capabilities` pattern, which is exactly why `GateFeatures` is pure) — assert that one false bit suppresses `"tools"` and that its reason survives into `Reasons`. `detect_linux_test.go` (`//go:build linux`, skips unless root and `/sys/fs/cgroup/cgroup.controllers` exists): the real probe runs, returns a matrix, and — the assertion that matters — **leaves nothing behind** (netns, veth and probe cgroup all absent afterwards).
>
> **Acceptance:** all five build gates; portable tests green.

---

### B14 — supervisor startup wiring + pack registry + `tool.listPacks` + `nodeRuntime.tool` (T1.11 Go half, T1.2)

> **OWNS:** `supervisor/internal/server/server.go`, `supervisor/internal/server/toolpacks.go` (new), `supervisor/internal/server/toolpacks_test.go` (new), `supervisor/internal/server/loaded.go`, `supervisor/cmd/supervisor/main.go`.
> **Private prefix: `toolpacks*`** for anything you add in `toolpacks.go`/`server.go`. Report the list. (`loaded.go` and `main.go` edits add no helpers.)
>
> Implement the Go half of **T1.11**, plus **T1.2** (absorbed from a batch that was too small to dispatch alone). You are the only agent in wave 4.
>
> **Critical structural constraint (dispatch punch-list P8 / deviation D8, already resolved — implement it this way):** `server.New()` is called by roughly a dozen unit tests across `internal/server` and `internal/wsbridge`. `extnet.Detect` is safe there because it is a read-only probe; **`tool.Detect`, `tool.ReapStale`, `tool.InitCgroupRoot` and `PR_SET_CHILD_SUBREAPER` are not** — they create and destroy real kernel objects and migrate the supervisor's own PID. So:
> - `New()` stays side-effect-free. Add the fields only: `toolCaps tool.Capabilities`, `toolRoot tool.CgroupRoot`, `toolPacks []tool.Pack`, `toolStop func()`, and a `ToolPacksDir string` / `StateDir string` to `Config` (defaulting to `/opt/iolbox/tools/packs` and `/var/lib/iolbox`).
> - Add `func (s *Server) InitRuntime() error` — the actual startup sequence, called **only** from `cmd/supervisor/main.go`, immediately after `server.New(...)` and before `ListenAndServe`. **Log-and-continue on failure** (a target without delegation must degrade to "tools unsupported", exactly the way `natgw` degrades today), never `log.Fatal`.
> - `InitRuntime` order: (1) `tool.SetSubreaper()`; (2) `tool.InitCgroupRoot()` → `s.toolRoot` — the migrate-first controller-enable sequence, **before any cage is created**; (3) `tool.InstanceID(stateDir)`; (4) `tool.ReapStale(...)` — the prior-crash sweep, **before** `Detect` so the probe runs on clean state; (5) `tool.StartReaper(tool.Registry)` → `s.toolStop`; (6) `tool.Detect(s.toolRoot)` → `s.toolCaps`; (7) `tool.LoadPacks(cfg.ToolPacksDir)` → `s.toolPacks`.
> - **Step (7) partial success is part of the contract (dispatch finding 12).** `tool.LoadPacks` returns the valid packs slice **and** a possibly-non-nil aggregated warning. **Cache the slice unconditionally** — `s.toolPacks = packs` before you even look at the error — then `log.Printf` the error at warning level and continue. A single malformed pack on the appliance must never cost the operator every other pack, and must never fail `InitRuntime`.
> - `register()`: add `s.disp.Handle("tool.listPacks", s.handleToolListPacks)` beside the other verbs.
> - New file `toolpacks.go`: `handleToolListPacks` — map `s.toolPacks` to `protocol.ToolListPacksResult`/`ToolPackInfo` (fields defined by the contracts batch; an empty registry must marshal to `[]`, not `null`), plus `func (s *Server) toolPack(id string) (tool.Pack, bool)`, which the next wave uses for **both** the `lab.load`-time known-pack check (deviation D11) and the `startToolNode` resolution.
>
> **`loaded.go` (T1.2, absorbed):** add `tool *tool.Endpoint` to `nodeRuntime`, **beside `extnet` and `vtap`**, with a doc comment in the file's existing style explaining what it is (the running netns+cgroup process tree for a `tool`-kind node; nil for other kinds and until started) and how it differs from its neighbours (unlike `extnet` it supervises a process tree; unlike `proc` it is not a pty-spawned `node.Process`). Also extend `stopAll()` — it currently handles `nr.proc`, `nr.vtap`, `nr.extnet`. Add: `if nr.tool != nil { _ = nr.tool.Stop(); nr.tool = nil; nr.machine.To(node.StateStopped) }`, following the `extnet` branch's exact shape. Note in a comment that the authoritative per-node teardown is `stopNode`'s branch (added by a different batch, next wave); this is the whole-lab safety net. `tool.Endpoint` exists on both build tags, so this compiles on Windows.
>
> **`cmd/supervisor/main.go` — two edits, and the second one is easy to get wrong:**
> 1. Call `srv.InitRuntime()` immediately after `server.New(...)` (around `:71`) and before the goroutine that runs `ListenAndServe`. Log its error and continue — do not `log.Fatal`.
> 2. **Reaper shutdown (dispatch finding 13 — read this whole bullet before writing anything).** The reap loop must be stopped on shutdown, and **this file has no `defer`-based subsystem-stop pattern to follow — do not invent one, and specifically do not use `defer`.** The actual shutdown shape in this file is: `ctx, stop := signal.NotifyContext(...)` → two goroutines run `ListenAndServe(ctx)` and the ws bridge → on signal both return → **`wg.Wait()` (`:103`)** → `close(errCh)` → a `for err := range errCh` loop that calls **`log.Fatalf`** on the first non-nil error → the clean-shutdown log line. **`log.Fatalf` calls `os.Exit`, which does not run deferred functions**, so a `defer srv.StopRuntime()` placed near the top would simply be skipped on exactly the error path where an orphaned goroutine matters most. Therefore: **call the stop immediately after `wg.Wait()` returns and BEFORE `close(errCh)`** — at that point both listeners have exited, nothing new will be spawned, and the drain/`Fatalf` has not yet had a chance to exit the process. Add `func (s *Server) StopRuntime()` in `server.go` which calls `s.toolStop` if non-nil (and is safe to call when `InitRuntime` failed or never ran, and safe to call twice). **Put a comment at the call site stating why it is not a `defer`**, so the next person does not "clean it up" back into a bug.
>
> **Do NOT touch `handlers.go` or `fabric*.go`** — those are the next wave's edits; they will read `s.toolCaps`, `s.toolPacks`, `s.toolPack` and `nr.tool`, which you declare.
>
> **Tests (`toolpacks_test.go`, portable):** `handleToolListPacks` over an injected `s.toolPacks` (empty → empty list, **not null**; populated → correct DTO mapping including `Modules`); `toolPack` hit/miss; and — **required by finding 12** — a test that constructs a packs dir with **one valid and one invalid** pack, runs the load step, and asserts the server ends up with the valid pack available (i.e. the non-nil error did not discard it). `StopRuntime` is a no-op when `toolStop` is nil and is safe called twice.
>
> **Acceptance:** all five build gates; `go test ./internal/server/... ./internal/wsbridge/...` green — **in particular, no existing test may start doing kernel work**, which is the whole point of keeping `New()` inert. Verify by confirming no test calls `InitRuntime`. In your summary, quote the exact three lines of `main.go` around the `StopRuntime` call so the ordering can be checked without opening the tree.

---

### B15 — node lifecycle handlers (T1.10, handlers half, + D11)

> **OWNS:** `supervisor/internal/server/handlers.go`, `supervisor/internal/server/toolnode_test.go` (new).
> **Private prefix: `toolnode*`.** Report the list.
>
> Implement the `handlers.go` half of **T1.10**. Another agent is concurrently editing `fabric.go`/`fabric_linux.go` — **do not touch those files**; `attachFabricForNode` already exists and you only call it.
>
> 1. **`startNodes` branch** (currently `KindNAT` at `:540`, `KindVPCS` at `:552`): add
>    ```go
>    if docNode.Kind == lab.KindTool {
>        started, err := s.startToolNode(ll, docNode, nr)
>        ...
>        continue
>    }
>    ```
>    placed as a peer to the `KindNAT` branch.
> 2. **`startToolNode`** — model it on `startExtnetNode` (`:596`) step for step, including its capability gate, its already-running short-circuit, its state-machine transitions and its `attachFabricForNode` tail:
>    gate on `s.toolCaps.Supports(tool.KindTool)` → `protocol.CodeUnsupported` with the cached `Reasons` detail; short-circuit if `nr.tool != nil`; `nr.machine.To(node.StateStarting)`; decode `config.pack` from `docNode.Config` and resolve it via `s.toolPack(id)` → `CodeBadRequest` if unknown; build `tool.Config`; `tool.Start(cfg)`; on error `nr.machine.To(node.StateCrashed)` and return `CodeNodeSpawnFailed`; on success `nr.tool = ep`, `nr.machine.To(node.StateRunning)`, then `s.attachFabricForNode(ll, n.ID)` so a tool started after its bridge already exists hot-connects now (mirrors `startExtnetNode`, `handlers.go:646`). Tool nodes have **no console** — return a `StartedNode` with no console port, exactly as the NAT path does.
>    **Populate `tool.Config.Options`** from the node document — decode `docNode.Config["options"]` if present and pass its raw JSON through; if absent pass nil, and `tool.Start` writes `{}`. Doc-comment that the endpoint turns this into the `0600 ioltool`-owned `options.json` the GUI reads via `IOLBOX_TOOL_OPTIONS` (deviation D9), and that a tool node cannot start without that file existing.
>    The internal ordering inside `tool.Start` (preclean → cage → netns/veth → socket dir → options file → launch w/ transition → exit-watcher → readiness) is already implemented in `internal/tool`; do not reimplement any of it here.
> 3. **Load-time known-pack check (deviation D11 — this is a behaviour requirement from T1.1, not a nicety).** At the server's `lab.load` boundary in this file — the same place the loaded document is validated before it is accepted — after `lab.Validate` succeeds, iterate the document's `KindTool` nodes and reject the **load** with `protocol.CodeBadRequest` naming the node and the unknown pack id if `s.toolPack(id)` misses. Rationale to doc-comment: T1.1 requires the pack to be a **known installed id**, checked at load; `internal/lab` cannot do it without importing `internal/tool` (a layering inversion), and doing it only in `startToolNode` would let an invalid document load successfully and fail later — a silent behaviour change. The structural half (`config.pack` present and non-empty) stays in `lab.Validate`; only the registry-aware half lives here. Keep the `startToolNode` check too — it is cheap and covers a pack removed between load and start.
> 4. **`stopNode` branch** (`:690`, beside the existing `nr.extnet != nil` branch at `:695`): `if nr.tool != nil { _ = nr.tool.Stop(); nr.tool = nil; nr.machine.To(node.StateStopped) }`. `Endpoint.Stop()` is itself the bounded SIGTERM-to-pgroup then `cgroup.kill`, then detach/del veth/netns/cgroup/socketdir, all idempotent — the handler is a thin call. Note in a comment that `handleLabReap` ("Force clean", `:400`) already loops `stopNode` + `teardownFabric`, so tool nodes are covered there for free.
> 5. **`handleHello`** (`:31`): `features = append(features, s.toolCaps.GateFeatures()...)` beside the existing `s.caps.GateFeatures()` line, so `"tools"` is advertised only when the runtime detected support.
>
> **Tests (`toolnode_test.go`, portable):** `startToolNode` rejects with `CodeUnsupported` when `s.toolCaps` is zero-valued (this is the off-Linux path, so it works on the dev box); rejects with `CodeBadRequest` for an unknown pack id; **`lab.load` of a document whose tool node names an uninstalled pack is rejected at load with `CodeBadRequest`** (the D11 regression guard) while the same document with an installed pack loads; `handleHello` includes `"tools"` with an injected all-true `toolCaps` and omits it otherwise; `stopNode` on a node with a nil `tool` is a no-op.
>
> **Acceptance:** all five build gates; full `go test ./internal/server/...` green. Note in your summary that the tool link is not actually realised until the concurrent fabric batch lands `KindTool` in `fabricNodes` — that is expected and not a bug in your batch.

---

### B16 — fabric integration (T1.10 fabric half, spec §4.2)

> **OWNS:** `supervisor/internal/server/fabric.go`, `supervisor/internal/server/fabric_test.go` (**new, portable**), `supervisor/internal/server/fabric_linux.go`, `supervisor/internal/server/fabric_linux_test.go`.
> **Private prefix: `fabtool*`.** Report the list.
>
> Implement the fabric half of **T1.10**. Another agent is concurrently editing `handlers.go` — do not touch it. Every point below is verified against the current tree; the plan doc corrects a stale spec citation, so read carefully.
>
> **`fabric.go` (PORTABLE — this edit must compile on the `_other` / non-Linux build too):** spec §4.2 **point 1** — add `lab.KindTool` to `fabricNodes`' kind switch (verified at `fabric.go:39-48`, the `switch doc.Nodes[i].Kind` over `KindIOL/KindNAT/KindVPCS`) so a tool endpoint is fabric-eligible; `isFabricLink` then admits its link with no further change. Update both functions' doc comments, which currently say "every node kind (IOL/NAT/VPCS)". **Note:** the spec's §4.2 citation of `fabric_linux.go:142` for eligibility is **stale** — eligibility is in the portable file. This is review High #7.
>
> **`fabric_linux.go` (Linux-only)** — spec §4.2 points 2–8:
> - **point 2, `attachFabricLink` switch (`:296`)**: add `case node != nil && node.Kind == lab.KindTool` → skip if `nr.tool == nil` (not yet started), else `nr.tool.AttachBridge(br)`. Model on the `KindNAT` case at `:304`.
> - **point 3, `detachFabricLink` switch (`:474`)**: add the tool case → `nr.tool.DetachBridge()`, mirroring `:478`.
> - **point 4, late-start**: no change needed in `attachFabricForNode` (`:443`) itself — `startToolNode` calls it. Verify and note.
````
````markdown
> - **point 5, `fabricLinkTapDevs` (`:598-616`)**: return the tool's **bridge-side** dev `tool.HostVethName(id)` (`vtool<id>`). This feeds `fabricStats` (`:555`, frame/byte glow) **and** `openLinkDirstat` (`:346`, per-direction protocol breakdown). Without it the tool link shows no traffic and no directional data.
> - **point 6, `fabricLinkFullyAttached` (`:213-240`)**: works for free once `fabricLinkTapDevs` returns `vtool<id>` — it checks the kernel `master` symlink via `tapMasterIs`. **Verify the bridge-side end is what carries the `master` symlink** and say so in a comment; the restart-skip logic self-heals correctly only if that holds.
> - **point 7, LACP slow-protocols tee**: `linkIsIOLToIOL` (`:427`) already excludes a tool endpoint (a tool is not IOL), so `openLinkSlowTee` skips it. **No change needed** — add a one-line comment recording this as a *conscious* skip, not an oversight.
> - **point 8, teardown**: `teardownFabric` (`:660-708`) gains a loop over tool endpoints to `Stop()` them, mirroring the `teardownVPCS` loop at `:703`, so no netns/veth/cgroup/process survives a full stop.
>
> **Tests.** New portable `fabric_test.go` (dispatch finding 9 — the eligibility edit is portable, so its test must be too, and it must run on the dev box): a lab with a tool node and one link yields that link in the fabric plan (`fabricNodes` admits the node, `isFabricLink` admits the link); a tool node with no link is eligible but produces no link; existing IOL/NAT/VPCS eligibility is unchanged. Extend `fabric_linux_test.go` for the Linux-only points: `fabricLinkTapDevs` returns `vtool<id>` for a tool endpoint.
>
> **Acceptance:** all five build gates. `fabric.go` is portable — **if your edit there needs anything from a `_linux` file, you have made a mistake.** Full `go test ./internal/server/...` green.

---

### B17 — P1 gate harness

> **OWNS:** `docs/tests/p1-gate.sh`, `docs/tests/p1-gate.md`.
> **Private prefix:** n/a (shell). Prefix shell functions `p1_` to avoid clashing with anything sourced.
>
> **You are alone in wave 6, dispatched only after B15 and B16 have landed and the integration build is green** (dispatch finding 7). That is deliberate: you assert on the exact verb names, error codes, `StartedNode` shape and kernel object names those two batches finalise, and a harness written concurrently with them could only encode guesses. **Read the landed `handlers.go` and `fabric.go`/`fabric_linux.go` before writing a single assertion**, and if anything in this prompt disagrees with the tree, the tree wins — report the discrepancy.
>
> Write the executable acceptance gate for P1, in the shape and house style of the existing, proven `docs/tests/p0-spike.sh` + `docs/tests/p0-spike.md` (read both first — including the p0-spike.md run log, whose operational warnings apply here too: run inside a properly delegated systemd scope, run from a world-traversable path, clear kernel state between failed attempts).
>
> Implement the "Gate to P2" procedure from `docs/learning-tools-nodes-plan.md`, driving raw NDJSON against a running supervisor on `127.0.0.1:4000` in the **fabric-harness style** proven this project (`docs/bridge-fabric-migration.md:139`: one JSON object per line on the control socket, matching responses by `id` and skipping pushed events by requiring the presence of an `ok` field). Its **gotcha applies to you directly**: a stale supervisor's process comm is `supervisor`, not `supervisor-linux-amd64`, so `pkill -x supervisor-linux-amd64` misses it — use `fuser -k 4000/tcp` or `pkill -f`.
>
> **The fixture is PINNED — a two-endpoint lab (dispatch finding 8; the previous "one-node lab" could not satisfy step 2, because `link.add` needs two endpoints).** One `tool` node using the installed `stub` pack, and one `vpcs` node as the far end. VPCS is chosen deliberately: it is bundled, needs no image or `iourc`, and is the cheapest second endpoint that makes a real link. Literal document, to be sent as the `lab.load` argument:
> ```json
> {"version":1,"id":"p1-gate","name":"P1 Gate","nodes":[
>   {"id":1,"kind":"tool","name":"TOOL1","x":0,"y":0,"config":{"pack":"stub"}},
>   {"id":2,"kind":"vpcs","name":"PC1","x":120,"y":0}],
>  "links":[]}
> ```
> and the link added in step 2:
> ```json
> {"id":"<n>","verb":"link.add","args":{"labId":"p1-gate","a":{"nodeId":1,"interface":"eth1"},"b":{"nodeId":2,"interface":"eth0"}}}
> ```
> (Confirm the exact `link.add` argument spelling against the landed `handlers.go`/`links.go` before use — this is the one place this prompt is most likely to be stale.)
>
> **Steps, each with an explicit `PASS`/`FAIL` line:**
> 1. `lab.load` + `lab.start` → node 1 reaches `running`. That means the readiness probe against the stub GUI's `/healthz` succeeded (items #11/#12), which in turn means the supervisor created `/run/iolbox/tool/1/options.json` as `ioltool:ioltool 0600` and exported `IOLBOX_TOOL_OPTIONS` — assert the file's owner and mode directly with `stat -c '%U:%G %a'`, because a wrong value here is the single most likely cause of a readiness timeout and the error is otherwise opaque.
> 2. `link.add` then `link.remove` hot-connect and detach with **no node restart** — capture the tool child's PID before (via the supervisor's node state or `/sys/fs/cgroup/<D>/tool-1/cgroup.procs`) and assert it is **unchanged** after each operation.
> 3. **Crash and sweep.** Record the supervisor PID. `kill -9` it. Restart it with the **exact same command line and environment** the unit uses (`systemctl restart iolbox-supervisor` when running under systemd; otherwise re-exec the same argv you started it with — pin whichever you use in `p1-gate.md`). **Readiness wait, pinned:** poll `127.0.0.1:4000` for a successful TCP connect, then send a `hello` and require an `ok` response, with a 30 s bound. This is a sufficient readiness signal *by construction*: `InitRuntime` — which contains `ReapStale` — runs to completion before `ListenAndServe` is called, so an accepted control connection proves the sweep already finished. Do not sleep a fixed interval.
>    Then **reload the lab**, explicitly: the supervisor does not auto-start labs after a restart, so re-send the same `lab.load` document from step 1 (it is idempotent) and then query `lab.list`/node state to confirm the nodes are known and **stopped**, not running. Assert the sweep worked: `ip netns list` shows no `iolt1`; `ls <D>/` shows no `tool-*` leaf (discover `<D>` from `/proc/<supervisor-pid>/cgroup`'s `0::` line and strip a trailing `/supervisor`); `ip link show vtool1` fails.
>    **Zombie assertion — ancestry-scoped, not host-global (dispatch finding 8).** Do **not** grep all of `/proc/*/stat` for state `Z`: on a busy appliance that is false-positive-prone and can fail the gate for something entirely unrelated. Instead enumerate `/proc/[0-9]*/stat`, and count only entries whose **state field is `Z` AND whose PPid field equals the current supervisor PID** — those are exactly the un-reaped children the T1.8 ownership split is responsible for. Assert the count is zero. State in a comment why the scope is the supervisor's own children.
> 4. `lab.stop` leaves nothing behind: no `iolt*` netns for this lab, no `vtool*` link, no `<D>/tool-*` cage, no `/run/iolbox/tool/1` directory, and no process in the cage.
>
> Explicit `PASS`/`FAIL` per step, a non-zero exit on any failure, and a cleanup trap. **Do not paper over a failure as a pass** — the p0-spike.md note about this applies and is the reason that note exists.
>
> `p1-gate.md` documents prerequisites (delegated scope, the `ioltool` account, `setpriv` ≥ 2.33, the installed stub pack at `/opt/iolbox/tools/packs/stub/`, the helper binary at `/opt/iolbox/iolbox-toollaunch`), the exact restart command chosen for step 3, and reserves a "Real-target run log" section for results, matching p0-spike.md's structure.
>
> **Acceptance:** `bash -n docs/tests/p1-gate.sh` clean; `shellcheck` clean if available. Do not run it — it needs the Linux appliance.

---

## 6. Punch list — resolve BEFORE dispatching

These are places where the P1 plan is genuinely ambiguous or silent and an implementing agent would have to guess. Each has a **recommended resolution**; the dispatching session should confirm or override, then bake the answer into the affected batch prompt. Items P1–P8 are **blocking** (they change wave-1/wave-2 prompts). P23–P27 are new in revision 2 and come out of the sol-medium review.

**Blocking — must be pinned before B01 dispatches**

- **P1 — Shared-helper ownership (highest risk).** Seven agents write into package `tool` in wave 2. Without a single owner, several will independently define a command runner (`runCmds`), an unsupported-platform error, a `contained()` path check, or a `netnsName()` helper — a duplicate-symbol build break that git merges cleanly. *Resolution: B01 owns the complete helper inventory listed in its prompt, and every downstream prompt carries an explicit "do not redefine" list. No batch may add a general-purpose helper.* **Revision 2 addition (finding 3):** the exported half was never the whole risk — downstream prompts still ask agents to invent **private** helpers (parsers in cage, command builders in netns, argv builders in launch, sweep logic in reap, socket/options helpers in endpoint, probe helpers in detect), and generic private names collide identically. *Extended resolution: every batch gets a private-symbol prefix scoped to its own files (`cage*`, `netns*`, `launch*`, `reap*`, `endpoint*`, `detect*`, `manifest*`, `inst*`, `toolpacks*`, `toolnode*`, `fabtool*`) and must report its final private symbol list; the dispatching session diffs the union before merging each wave.*
- **P2 — Where the deterministic name helpers live.** T1.9 pins the *names* (`iolt<id>`, `vtool<id>`, `tool-<id>`, `/run/iolbox/tool/<id>`) but not the file. They are needed by cage, netns, reap, endpoint, detect **and** `fabric_linux.go`. *Resolution: portable `tool.go` (B01), so `fabric_linux.go` and portable tests can both use them.* **Revision 2 addition (finding 2):** `NetnsExecArgs` joins them there for the same reason plus a stronger one — B07 and B08 both consume it in the same wave, so it cannot live in either.
- **P3 — `instance.go` cannot be portable as specified.** The file map lists it without a build tag, but T1.9 requires `flock`. *Resolution: deviation D5 — portable `instance.go` + `instance_lock_{linux,other}.go`.*
- **P4 — Missing `_other.go` stubs.** The file map lists `_other` only for `detect` and `endpoint`, but `server.go` calls `InitCgroupRoot`/`ReapStale`/`SetSubreaper`/`StartReaper` and must compile on the dev box. *Resolution: deviation D1 — every `X_linux.go` batch also owns `X_other.go`.* **Revision 2 addition (finding 9):** stubs make the package *compile* off Linux but do not make it *testable* — a batch owning only `_linux` files has nowhere to put a portable test. Each affected batch also owns an untagged pure-logic file (`cage.go`, `netns.go`, `reap.go`, `detect.go`, `fabric_test.go`).
- **P5 — Native launcher fallback: in-process or shipped binary?** T0.2 says "the small native launcher fallback" without pinning its production form; P0's is a standalone binary (`tools/p0-launcher`, own module). Go cannot safely do the `capset`/`setuid`/ambient-raise sequence in a pre-exec hook. *Resolution: deviation D6 — `tools/iolbox-toollaunch/` (own module, mirroring the `tools/p0-*` layout), installed to `/opt/iolbox/iolbox-toollaunch`. Confirm the binary name — B08 and B09 both hardcode it.* **Revision 2 addition (finding 6 / deviation D10):** the same "Go has no safe pre-exec hook" argument invalidates revision 1's cgroup-placement fallback, so the helper also grows `--cgroup <path>`, applied before the privilege transition. B03 defines it; B08 consumes it.
- **P6 — PID registry instance scope.** T1.8 says the loop is "supervisor scope" but not whether the registry is a package singleton, a `Server` field, or passed through `Config`. Endpoint (which registers) and reap (which reads) are different batches. *Resolution: package-level `tool.Registry` singleton declared in `tool.go` (deviation D2), which also lets B12 run in wave 3 instead of wave 4.* **Revision 2 addition (finding 1):** "supervisor scope" is stronger than it looks — `PR_SET_CHILD_SUBREAPER` is process-wide, so the registry must contain **every** direct `exec.Cmd` child in the binary, including six pre-existing sites that predate this work. New batch B10, wave 2, hard prerequisite of B11 and B14.
- **P7 — Where the cgroup bring-up code lives.** T1.11 describes the migrate-first sequence under "`server.go`", but T1.4 describes the same mechanism under `cage_linux.go`. *Resolution: deviation D3 — implemented as `tool.InitCgroupRoot()` in `cage_linux.go`; `server.go` calls it.* **Revision 2 addition (finding 11):** T1.11 also requires the call to be idempotent, and the discovery rule breaks its own idempotency (on a second call `/proc/self/cgroup` points at `<D>/supervisor`, so re-applying the rule yields `<D>/supervisor/supervisor`). Pinned correction: a discovered cgroup whose last element is exactly `supervisor` means `<D>` is its **parent**; plus a cached root so a re-entrant call does no filesystem work.
- **P8 — `server.New()` is called by ~12 unit tests.** T1.11 says to call `tool.Detect` + `tool.ReapStale` + subreaper setup "beside `extnet.Detect`" at `server.go:122` — i.e. inside `New()`. `extnet.Detect` is a harmless read-only probe; these are not: `InitCgroupRoot` migrates the supervisor's own PID, `Detect` creates and destroys real kernel objects, `ReapStale` deletes them. Running that in every `internal/server` and `internal/wsbridge` test is unacceptable, and on Linux CI it would be actively destructive. *Resolution: deviation D8 — `New()` stays inert; a new `Server.InitRuntime()` holds the sequence and is called only from `cmd/supervisor/main.go`. **This adds `supervisor/cmd/supervisor/main.go` to the touched-file list, which the plan doc does not mention.*** **Revision 2 addition (finding 13):** the symmetric question — how the reap loop *stops* — was unanswered. `main.go` has no `defer`-based subsystem-stop pattern and its shutdown ends in `log.Fatalf` (which skips defers), so the stop is pinned to run immediately after `wg.Wait()` and before the `errCh` drain.

**Non-blocking — pin before the owning batch dispatches**

- **P9 — `contracts/lab.schema.json` is a touched file the plan omits.** Its `kind` enum is closed (`["iol","vpcs","nat"]`) and the Go struct tags are documented as mirroring it exactly. *Resolution: B02 owns it.*
- **P10 — "known installed pack id" validation location.** T1.1 puts it in `validate.go`, which would force `internal/lab` to import `internal/tool` (a layering inversion — `lab` is a pure document package with one dependency, `netmap`). *Revision 1's resolution moved the check to `startToolNode` only — and that was wrong (finding 10): it silently changed **timing**, so an invalid document would load successfully and fail only at start. **Revised resolution (deviation D11):** `lab.Validate` stays structural (`config.pack` present, non-empty, a string, no `internal/tool` import), and B15 adds the registry-aware known-pack check at the server's `lab.load` boundary in `handlers.go`, which already depends on B14's `s.toolPack`. Load-time behaviour is preserved; the layering is not inverted. The `startToolNode` check is kept as well, covering a pack removed between load and start.*
- **P11 — There is no stub pack.** The P1 gate requires "a one-node stub-pack lab", but no `pack.json` exists for the P0-promoted `tools/tool-stubgui`, and no install path is specified. *Resolution: pin id `stub`, path `/opt/iolbox/tools/packs/stub/pack.json`, `gui.bin: "tool-stubgui"`, `gui.health: "/healthz"`, `transport: "unix"`, `caps: ["NET_RAW"]` (B09), with a matching `testdata` copy in B04.* **Revision 2 note (finding 8):** "one-node lab" is also wrong for the gate itself — `link.add` needs a second endpoint. The fixture is now a two-node lab (`stub` tool + `vpcs`), pinned literally in B17.
- **P12 — `tool.listPacks` result shape is unspecified.** T1.11 says "register `tool.listPacks` (item #8)" with no DTO. *Resolution: `ToolPackInfo{ID, Name, Icon, Transport, Groups, Modules}` with `ToolModuleInfo{Key, Label, Group}` — enough for the P2 palette, no more. An empty registry marshals to `[]`, not `null`.*
- **P13 — Resource-limit defaults have no home.** Spec §5.4 says limits come "from the manifest/defaults (secbench's template implies cpu:2, ram:2048)", but §2.6's `pack.json` schema has **no `limits` field**. *Resolution: add an optional `limits` object to the manifest **and** pin `DefaultLimits()` = 2048 MiB / 512 pids / `"200000 100000"` / swap 0 in `tool.go`. Confirm the pids and cpu numbers — they are inferred, not stated anywhere.*
- **P14 — What does the `unixProxy` capability probe actually test?** The proxy itself (`internal/wsbridge/proxy.go`) is P2/T2.5, but the matrix bit is in P1's `Detect`. *Resolution: probe = bind + dial an AF_UNIX socket in a `0700` `ioltool`-owned dir under `/run/iolbox/tool/`, with no `wsbridge` dependency.*
- **P15 / P16 — Probe cadence and timeouts are unspecified.** T1.7 says "until 200 or a bounded timeout" and "N consecutive non-200" without numbers. *Resolution: readiness 10 s bound with 100 ms polls; liveness every 5 s, N=3. Confirm — these are invented, and 5 s is the value §5.6 pins for graceful stop by analogy to `vpcsConsoleReadyTimeout`.*
- **P17 — `mtool<id>` is named but never created.** It appears in T1.9's deterministic-name list but nothing in T1.5's pinned sequence creates it. *Resolution: it is the T1.6 `/31` mgmt veth's host-side end (the netns side being `mgmt0`). Confirm — if wrong, T1.9's state-file record shape changes.*
- **P18 — Where does the `ioltool` uid come from, and what if the account is missing?** T0.5 pins the socket dir as `ioltool`-owned `0700`, but nothing says how the uid is resolved or what happens on a target without the account. *Resolution: `os/user.Lookup("ioltool")` at `Detect` time; absence fails `ambientCapTransition` with a specific reason and the runtime degrades to "tools unsupported", exactly as `natgw` degrades. Revision 2 note: **B12 needs the same lookup** for the options file's ownership, so both batches do it — with their own prefixed helpers (`detectLookupUser` vs `endpointLookupUser`), never a shared unprefixed `lookupUser`. That duplication is deliberate and is cheaper than a cross-batch symbol dependency.*
- **P19 — Is T1.6 (`/31` mgmt fallback) actually exercised in P1?** The only P1 pack is the stub, which uses `transport: "unix"`, so the entire `/31` + iptables path is dead code at the P1 gate. *Resolution: implement it (it is listed in P1) with unit tests on rule/command generation only, and a file-level comment stating it is unexercised until a `transport: "tcp"` pack exists. Alternatively, defer to P3 — **the dispatching session should decide**, since it is roughly a third of B07. Revision 2 note: if it is kept and B07 is judged too large, the sanctioned split gives T1.6 its own `mgmt*.go` files (see B07's sizing note) — never a second agent in `netns_linux.go`.*
- **P20 — T1.3's `manifest_keys_test.go` has no home in P1.** It is specified to "run in the pack's own build" and assert that `pack.json`'s module keys equal the compiled `moduleDefs` keys — but there is no real pack in P1. *Resolution: defer the build-time test to P2 (when secbench is ported); B04 implements only the doc-comment half of item #7.*
- **P21 — `handleLabReap` and load-over-running are asserted-by-inheritance.** Spec §4.2 point 8 and §5.7 say "Force clean" and load-over-running cover tool nodes "for free" once `stopNode` and `teardownFabric` know about tool. That is true, but nothing tests it. *Resolution: add the assertion to the B17 gate harness rather than a new Go test — it is step 4's reload-then-stop sequence.*

**Punch-list status: 27 items, 11 blocking (P1–P8 from revision 1, plus P22, P23, P24 added or upgraded in revision 2; P25–P27 are prompt-level fixes that must land in their owning prompts before the affected wave).**

---

## 7. `codex sol-medium` review — scope and timing

**Timing: revision 1 of this document was reviewed once, before any `luna-xhigh` agent was dispatched. It returned "UNSAFE to dispatch as written" with 15 findings.** This revision resolves all 15 (see the Revision history at the top of this document, one line per finding). Not on implementation code — code review is a separate, later concern this plan does not solve.

### Re-review prompt (revision 2)

A second `sol-medium` pass is **verification-scoped**, not a fresh review: the design is unchanged, the deviations are unchanged except for the four new ones, and re-deriving the whole plan wastes the pass. Use this prompt:

> You are re-reviewing **revision 2** of an execution/dispatch plan, not a design. Revision 1 was found unsafe; the 15 findings and their intended fixes are listed in the document's "Revision history" section at the top.
>
> Read `docs/p1-dispatch-plan.md` (revision 2), then `docs/learning-tools-nodes-plan.md` §P1 (T1.1–T1.12 + the "Package / file map" table + "Gate to P2"), then the current tree at the files the plan claims to touch. `docs/learning-tools-nodes-spec.md` is background only.
>
> **The design is final. Do not review or propose changes to the design** (netns boundary, cap transition, 3-level cgroup cage, durable-id sweep, Level A pack contract). Do not propose new P1 scope.
>
> Do exactly three things, in this order:
>
> 1. **Verify each of the 15 findings is actually resolved**, using the Revision history as your checklist. For each, say RESOLVED / PARTIALLY RESOLVED / NOT RESOLVED and point at the specific section, batch prompt or deviation that does (or fails to do) the work. Findings 1 (direct-child registration, now batch B10 + deviation D9), 4 (the `waitid` mechanism in B11), and 5 (`IOLBOX_TOOL_OPTIONS` + the options file, deviations D10 and punch-list P22/P23) are the three where a partial fix is still a broken appliance — check those hardest.
> 2. **Re-check parallel safety for the CHANGED structure only.** What moved: wave 2 gained a seventh batch (B10) which edits four packages outside `internal/tool`; `NetnsExecArgs` moved from B07 to B01; several batches gained portable `X.go`/`X_test.go` files; old B13 folded into B14 (wave 4 is now a single agent); B17 moved to its own wave 6. For every pair now sharing a wave, verify (a) OWNS lists are disjoint and (b) neither needs a Go identifier the other defines. Verify specifically that B10's six files are owned by no other batch, that B10 importing `internal/tool` from `internal/node`/`internal/bcap`/`internal/extnet`/`internal/fabric` creates **no import cycle**, and that B08 no longer depends on anything B07 defines.
> 3. **Stress-test the new private-prefix mitigation (finding 3 / punch-list P25).** Walk the wave-2 and wave-3 prompts and ask: with the prefix rule and the symbol-list reporting in place, is there still a plausible way two concurrent agents produce a duplicate package-level symbol in `internal/tool` or `internal/server`? Pay attention to B12 (endpoint) and B13 (detect), which both do user lookup and socket work, and to B15/B16 in `internal/server`.
>
> Output: for part 1, the 15-item verdict table. For parts 2 and 3, a prioritized list of concrete defects with file/section references and a specific fix for each. Finish with a plain statement: is the wave structure safe to execute as written, and if not, exactly which batch must move before dispatch.

After the re-review reports: the dispatching session folds any remaining fixes into the batch prompts, and only then dispatches wave 1.

---

## Summary for the dispatching session

**17 batches, 6 waves, max concurrency 7.**

| Wave | Agents | Batches |
|---|---|---|
| 1 | 3 | B01 `tool.go` contract · B02 lab/schema/protocol · B03 `iolbox-toollaunch` binary (+`--cgroup`) |
| 2 | 7 | B04 manifest · B05 instance/state-file · B06 cage · B07 netns/veth · B08 launcher wrapper · B09 packaging · **B10 register existing direct children** |
| 3 | 3 | B11 reap+`ReapStale` · B12 endpoint lifecycle (+options file) · B13 `Detect` |
| 4 | 1 | B14 `server.go`+`toolpacks.go`+`loaded.go`+`main.go` |
| 5 | 2 | B15 `handlers.go` (+load-time pack check) · B16 fabric |
| 6 | 1 | B17 gate harness |

The shape is deliberately narrow-then-wide-then-converging, with a solo tail. Wave 1 is small because **B01 freezes every cross-file symbol in package `tool`** — that single batch is what makes the seven-way wave 2 safe. Waves 4–5 are narrow because `internal/server` has few files and they all reference each other. Wave 6 exists solely so the gate harness is written against **landed, integrated** behaviour rather than against this document's description of it.

Ordering extracted from task descriptions rather than task numbers, in four places worth noting: **T1.9's durable id must precede T1.8's sweep** (the plan numbers them the other way); **T1.4's cage and T1.11's bring-up are one mechanism, not two tasks**; **T1.12's `Detect` must call the production cage/netns/launch code**, so it lands after all three rather than alongside them; and — new in revision 2 — **T1.8 has a second half the plan never spelled out**: registering the supervisor's *pre-existing* direct children, which must land before the subreaper loop exists at all.

**Punch list: 27 items, 11 blocking.** The three most consequential:

- **P24 (new, top blocker)** — T1.8's ownership split is only sound if `tool.Registry` contains **every** direct `exec.Cmd` child the supervisor spawns. Revision 1 wired registration only into new tool-node code, leaving IOL, VPCS, tcpdump capture and every `ip`/`sudo` helper in `internal/extnet`/`internal/fabric` unregistered. The moment `PR_SET_CHILD_SUBREAPER` goes process-wide and the loop starts, those look like orphans and get reaped out from under their own `cmd.Wait()`. Fixed by batch **B10** in wave 2, before both the reap loop (B11) and `StartReaper`'s caller (B14). This ranks above P8 because P8 produces loud test failures while this produces silent, intermittent, appliance-only corruption.
- **P22/P23 (new)** — the P1 gate's stub GUI (`tools/tool-stubgui`, P0-proven) reads **`IOLBOX_TOOL_OPTIONS`** and hard-exits when it is unset, then reads *and writes* that file. The plan froze the name `IOLBOX_TOOL_OPTS`, provided no options value in `tool.Config`, told no batch to create the file, and pinned no ownership/mode. As written, the P1 gate could not start its own fixture. Fixed by renaming the frozen variable, adding `OptionsPath`/`Options` to `Config` (B01), requiring **B12** to create `<socketDir>/options.json` as `ioltool:ioltool 0600` atomically before launch, and requiring **B15** to populate the options data. This is a missing prerequisite, not new scope.
- **P1 + P25** — seven wave-2 agents write into one Go package. Independently-invented *shared* helpers (`runCmds`, `ErrUnsupportedPlatform`, `contained`, name helpers) produce duplicate-symbol breaks that **git merges cleanly and `go build` rejects** — mitigated by B01 owning the complete inventory plus explicit "do not redefine" lists (P1). Revision 2 closes the other half: *private* helpers (`cleanup`, `probe`, `parseEvents`, `lookupUser`, `writeAtomic`) collide the same way, and "add no general-purpose helpers" does not prevent it. Mitigated by the mandatory per-batch private-prefix table in §2 plus every batch reporting its final unexported symbol list for a pre-merge diff (P25).

The other blockers are P2 (name-helper location, now also covering `NetnsExecArgs`), P3 (`instance.go` can't be portable — `flock`), P4 (missing `_other.go` stubs, extended to portable `X.go` files), P5 (native launcher = shipped binary, name needs pinning), P6 (PID registry scope), P7 (cgroup bring-up code location), P8 (`New()` must gain no new destructive side effects), P26 (B08's cgroup fallback moves into B03's `--cgroup` flag) and P27 (reaper shutdown wiring in `main.go`, explicitly not via `defer`).

Non-blocking highlights: **P10's resolution was corrected** — the known-installed-pack check stays at **load time**, moved to the server's `lab.load` boundary rather than deferred to start time (deviation D11); the stub `pack.json` still has to be created for the gate (P11); `limits` has no home in the manifest schema (P13); readiness/liveness numbers are invented (P15/P16); `mtool<id>` is named but never created (P17); the `ioltool` lookup is now needed by two batches, deliberately duplicated behind prefixed helpers (P18); and **T1.6's `/31` fallback is unexercised dead code at the P1 gate** (P19 — still worth an explicit implement-vs-defer call, it's about a third of B07).

Three things the dispatching session must do that no agent can do for it: **(a)** diff each wave's reported unexported-symbol lists before merging (P25's enforcement point); **(b)** run the integration build on **both** `GOOS` between every wave — the Windows-native build compiles almost none of this work; **(c)** hold B17 until wave 5 is green, because a gate harness written against a plan rather than against landed code asserts on names that may not exist.
