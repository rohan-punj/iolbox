# Learning-tool nodes — execution plan

Status: **EXECUTION PLAN** (implementation sequencing; no code in this document).
Author handoff: 2026-08-08.

This plan **supersedes §8 (the P0–P4 phase list) of
`docs/learning-tools-nodes-spec.md`**. Every design decision in that spec still
stands — read the spec for *why* a thing is shaped the way it is (netns boundary,
Level A / AF_UNIX host, cap transition, cgroup cage, pack contract); this document
does not repeat those rationales. What it adds is the concrete, no-open-questions
resolution of a review punch list produced by **two further passes** beyond the
first review already folded into the spec: a second `codex sol-medium` pass and a
final `fable` pass. Fourteen still-open items from those two passes are each
resolved below as a specific implementation decision pinned to a phase, a file, and
a gating acceptance test. The traceability table in §T maps every one of the 14 to
the phase and task that closes it — that table is the first thing a reviewer should
check.

The spec's phase *shape* (P0 spike → P1 headless engine+lifecycle → P2 pack+GUI
slice → P3 hardening → P4 second pack) is preserved, with **one forced
reordering** called out explicitly in §P2: punch-list item **#1** pulls the
`/tool/{nodeId}` reverse-proxy **security** work forward out of P3 into P2, because
P2 is the phase that first serves a proxied pack page and the security boundary
must ship in the same phase as the thing it guards — never after. The spec's
original P2/P3 split put "serve the proxied GUI" in P2 and "design its CSP/auth" in
P3; that split is rejected here.

---

## Package / file map (grounding for every task below)

New Go package **`supervisor/internal/tool/`** (peer to `internal/extnet`,
`internal/fabric`, `internal/vtap`), with build-tagged files mirroring the extnet
convention (`_linux.go` real, `_other.go` stub so non-Linux still compiles):

| File | Responsibility |
|---|---|
| `tool.go` | Portable types: `Endpoint`, `Config`, `Capabilities`, `Pack`, `Manifest`, `GateFeatures()`, `Supports()`. No syscalls. |
| `manifest.go` | `pack.json` load + validate: schema/`manifestVersion`, caps-allowlist (`NET_RAW` only), path canonicalization+containment, `gui.health` field. Portable, unit-tested. |
| `detect_linux.go` / `detect_other.go` | `Detect()` operational probe + capability matrix (§4.3 of spec). |
| `endpoint_linux.go` / `endpoint_other.go` | `Start/Stop/AttachBridge/DetachBridge/State`, exit-watcher, readiness/liveness, per-Start preclean. |
| `cage_linux.go` | cgroup v2 subtree mgmt (create-in-delegated-root, subtree_control, limits, `cgroup.kill`, populated-wait, rmdir). |
| `netns_linux.go` | netns add/del, veth create-temp-move-rename, `/31` mgmt fallback + its iptables. |
| `launch_linux.go` | the cap/securebits transition launcher (setpriv argv or the small native launcher fallback, §P0). |
| `reap_linux.go` | `ReapStale()` durable-id state-file sweep + the subreaper `wait4` loop (ownership-split: peek with `WNOWAIT`, reap only unregistered orphans). |
| `instance.go` | durable per-install identity (UUID persisted to `/var/lib/iolbox/instance-id`, `flock`) + the created-object state file, used to scope `ReapStale`. |
| `proxy.go` (in **`internal/wsbridge/`**, not `internal/tool`) | the `/tool/{nodeId}` reverse proxy + its security checks + URL rewriter. Lives with the other bridge routes. |

Touched existing files: `internal/lab/lab.go` + `internal/lab/validate.go`
(KindTool); `internal/server/loaded.go` (`nodeRuntime.tool` field);
`internal/server/handlers.go` (`startToolNode`, `stopNode` branch,
`handleToolListPacks`); `internal/server/fabric.go` (**portable** fabric
eligibility — `fabricNodes`/`isFabricLink`, spec §4.2 point 1) +
`internal/server/fabric_linux.go` (the Linux-only fabric points: attach/detach/
late-start/`fabricLinkTapDevs`/`fabricLinkFullyAttached`/slow-tee-skip/teardown,
spec §4.2 points 2–8); `internal/server/server.go` (call `tool.Detect` +
`tool.ReapStale` + subreaper setup at startup, merge `GateFeatures`, register the
`tool.listPacks` verb); `internal/protocol/verbs.go` (`ToolListPacksArgs`/
`ToolListPacksResult` types); `internal/wsbridge/wsbridge.go` (register `/tool/`
route + the session-auth gate, T2.5); `runtime/build-rootfs.sh` +
`runtime/files/iolbox-supervisor.service` + `runtime/files/prestart-clean.sh`
(rootfs deps, `Delegate=yes`, sweep). Frontend: `app/src/lib/labTypes.ts`,
`components/Palette.svelte`, new `app/src/lib/nodes/ToolNode.svelte`,
`components/NodeEditDialog.svelte`, `components/Console.svelte` (tool panel),
`lib/protocol.ts` (`tool.listPacks` client).

> **File-drift note (review High #7):** the fabric **eligibility** switch
> (`fabricNodes`, `isFabricLink`) lives in the **portable** `fabric.go`
> (verified: `fabric.go:39-48`, a `switch doc.Nodes[i].Kind` over
> `KindIOL/KindNAT/KindVPCS`), **not** in `fabric_linux.go`. Only the runtime
> attach/detach/stats points are Linux-only (`fabric_linux.go:296`, `:474`,
> `:603`). Adding `KindTool` to eligibility is therefore a `fabric.go` edit and
> must compile for the `_other` (non-Linux) build too; the spec §4.2 citation of
> `fabric_linux.go:142` for eligibility is stale and is corrected here.

---

## P0 — Spike: prove the risky primitives on the builder (no pack, no frontend)

**STATUS (2026-08-08): T0.1–T0.8 PASS on real hardware (appliance VM
192.168.226.233). T0.9 not yet run (blocked by a live lab already using the
production `iolbr0` bridge on that box — the test correctly refused to touch it).
7 real implementation bugs were found and fixed getting here; see
`docs/tests/p0-spike.md`'s run log and `git log --oneline main..feat/learning-tool-nodes`
on branch `feat/learning-tool-nodes` for the full list. P0 is not fully closed until
T0.9 runs clean on a lab-free target.**

**Goal:** on a real target runtime, prove every kernel-primitive claim the design
rests on with explicit pass/fail acceptance tests plus a **hostile probe program**,
so P1 builds on proven ground rather than assumptions.

This phase also **promotes reusable artifacts** so P1 has real launch targets: the
hostile-probe harness's trivial socket-binding binary becomes the **stub GUI**
(item #11), and the launcher/cap-transition code and cgroup helpers written here are
the first drafts of `launch_linux.go` and `cage_linux.go`.

### Tasks

- **T0.1 — Stub GUI binary (`tools/tool-stubgui/main.go`, promoted to P1).** A
  ~40-line Go binary: `net.Listen("unix", $IOLBOX_TOOL_SOCK)`, serve `GET
  /healthz` → `200 ok` and `GET /` → a one-line page. Reads its socket path and an
  options-file path from env (the scrubbed-env contract, spec §2.6). This is the
  launch target for P0 acceptance tests **and** is promoted to be P1's `running`
  target (**item #11**). Its health route is `"/healthz"` — the concrete value P1's
  probe hits (**item #12**).
- **T0.2 — Cap/securebits transition launcher.** Implement the spec §2.5.2
  ordered transition. **Decision (item #13):** attempt the pinned `setpriv` argv
  first; the P0 test gates on the *final observed state*, and if the installed
  `setpriv` version cannot express the full securebits lock, fall back to the small
  native launcher. Pin exactly:
  ```
  setpriv --reuid ioltool --regid ioltool --clear-groups --no-new-privs \
          --bounding-set -all,+cap_net_raw \
          --inh-caps    -all,+cap_net_raw \
          --ambient-caps -all,+cap_net_raw \
          -- <target> <args...>
  ```
  Version-check at build/probe time: `setpriv --version` must be util-linux ≥ 2.33
  (ambient-caps support). If `SECBIT_NOROOT`/`SECBIT_KEEP_CAPS` locks are required
  and `setpriv` won't set them, use a native Go pre-exec launcher (`prctl`
  `PR_SET_NO_NEW_PRIVS`, `PR_CAPBSET_DROP` loop, `SECBIT_*` via `prctl PR_SET_SECUREBITS`,
  `capset`, `setgroups/setgid/setuid`, `PR_CAP_AMBIENT_RAISE`, then `execve`).
- **T0.3 — cgroup delegation probe (item #4 + review Critical #2 — the
  no-internal-processes rule).** cgroup v2 forbids a cgroup that has any controller
  enabled in its `cgroup.subtree_control` from **also directly containing
  processes** — only leaf cgroups may hold processes once their parent enables a
  controller. The supervisor's own delegated cgroup (from `/proc/self/cgroup`)
  *contains the supervisor process*, so we may **not** enable controllers on it and
  hang cages off it directly. The probe must prove the correct **3-level layout**:
  - **level 1 — delegated root** = the supervisor's delegated cgroup `<D>` (the
    systemd service cgroup under `Delegate=yes`, or a probe-created dir): enables
    controllers, **holds NO processes**.
  - **level 2 — supervisor leaf** = `<D>/supervisor/`: the probe **migrates its own
    PID here first** (write `$$` to `<D>/supervisor/cgroup.procs`) so `<D>` is
    emptied of processes **before** any controller is enabled on it.
  - enable controllers **only after** `<D>` is process-empty: write `+memory +pids
    +cpu` to `<D>/cgroup.subtree_control`.
  - **level 3 — cage leaves** = `<D>/tool-<id>/` (siblings of `supervisor/`): the
    probe cage; assert `memory.max` OOM-kills a hog and `pids.max` bounds a fork
    bomb **inside a level-3 leaf**.
  Assert the ordering guard explicitly: writing `subtree_control` on `<D>` **while
  the probe PID is still in `<D>`** returns `EBUSY` (proves the rule is real and the
  migrate-first sequence is necessary). Never `/sys/fs/cgroup/iolbox/` fs-root.
- **T0.4 — veth create-order probe (item #3).** Prove the collision-free sequence
  (see §P1 T1.5 for the pinned commands); assert a host whose primary NIC is named
  `eth1` is untouched (create a dummy `eth1` in root ns first, run the sequence,
  assert the dummy still exists and the netns `eth1` is the veth peer).
- **T0.5 — AF_UNIX reachability + socket-dir permissions probe (item #2).**
  Create `/run/iolbox/tool/<id>/` **owned by `ioltool`, mode `0700`**, parent
  `/run/iolbox/tool/` **root-owned `0755`** (traversable, not writable). Assert
  `ioltool` (the launched stub GUI) can `bind` the socket AND read/write the
  options file in the same dir; assert the root-ns probe can dial the socket.
- **T0.6 — Reaper loop probe (item #5 + review Critical #3 — do NOT race
  `exec.Cmd.Wait()`).** The supervisor already spawns its **direct** children
  (the pack GUI, and in production IOL/VPCS) via `exec.Cmd`, and Go runs a
  per-child `cmd.Wait()` goroutine for each (verified `spawn_linux.go:188` and
  `:396`). A naïve supervisor-wide `wait4(-1, WNOHANG)` loop reaps **those** PIDs
  too, racing Go's own `Wait()` — whichever calls first wins, the loser gets
  `ECHILD` and the exit status is lost. The probe must prove the **ownership-split**
  reaper that avoids this:
  - The reaper maintains a registry of **directly-spawned PIDs** (every `exec.Cmd`
    the supervisor starts registers its PID; deregisters when its `cmd.Wait()`
    returns).
  - The `SIGCHLD` loop calls `wait4(-1, WNOHANG|WNOWAIT)` to **peek** at a reapable
    child *without* reaping it. If the peeked PID is **in the registry** → it is a
    direct child; **leave it** for its `cmd.Wait()` goroutine (do nothing). If it is
    **not** in the registry → it is a re-parented orphan (a grandchild whose parent
    died); reap it with a real `wait4(pid, WNOHANG)`.
  Probe: launch stub-GUI (registered) → it forks grandchildren (unregistered) →
  `kill -9` the stub-GUI; assert (a) the stub-GUI's exit status is delivered to
  **its** `cmd.Wait()` (not swallowed by the loop), (b) the reparented orphans are
  reaped by the loop (no `Z` state in `/proc/*/stat`), and (c) `cgroup.kill` clears
  the subtree. Kill (cgroup) and reap (loop) are separate mechanisms, per item #5.
- **T0.7 — Stale-cleanup after `kill -9`.** `kill -9` the whole harness mid-run;
  a fresh instance's `ReapStale()` (durable-id + state file, §P1 T1.9) removes leftover
  netns/veth/cgroup and the hostile probe cannot survive the sweep.
- **T0.8 — Hostile probe program (`tools/tool-hostile/main.go`).** Tries to:
  regain a dropped cap after exec, enumerate host interfaces/routes, perform a
  NET_ADMIN op (add a route / set promisc), escape its cgroup, read a host file
  outside the pack, and survive parent termination. **Every attempt must fail**
  except the accepted-risk reads (host file read *succeeds* — that is the honestly
  accepted §2.5.3 boundary, item #14; the test asserts it succeeds and the harness
  records it as the documented accepted risk, not a bug).
- **T0.9 — Fabric / L2-forwarding / capture proof (review High #9 — the
  foundation P1's 8 fabric-integration points all rest on).** This is the *data-
  plane* proof, distinct from T0.4's device-creation proof: a frame must actually
  **cross the bridge and be visible in a capture**, because P1 wires eight fabric
  points (spec §4.2) onto this assumption and must not build on an unproven data
  plane. Concretely, on the builder:
  1. Create a manual bridge `iolbr0`; run the T0.4 netns+veth sequence so the
     bridge-side `vtool<id>` sits in root ns attached to `iolbr0` and `eth1` is in
     the netns.
  2. Bring up a **second** endpoint on `iolbr0` — either a second tool netns, or a
     reused existing kind (an IOL/VPCS tap already on the fabric) — so there are two
     L2 peers on the bridge.
  3. From inside `iolt<id>`, the stub sends a scapy ARP/broadcast frame on `eth1`;
     assert the **second** endpoint receives it (frame egresses the bridge to the
     peer, proving forwarding — not just link-up).
  4. `tcpdump -i iolbr0` (the exact existing bridge-capture path) records the frame,
     proving the tool's traffic is capturable with **zero new capture code** (spec
     §1.3.2).
  Explicit pass/fail: peer RX count ≥ 1 for the sent frame **and** the capture pcap
  contains it. If forwarding or capture fails here, the fabric integration is
  wrong and P1 does not start.

### Gate to P1
Run `sudo bash docs/tests/p0-spike.sh` on each candidate target (VMware/OVA/native
QEMU first). Observable pass: all of T0.1–T0.9 print `PASS`; `/proc/self/status` of
the launched stub shows `CapEff/CapPrm/CapInh/CapAmb == cap_net_raw only` and
`CapBnd` == cap_net_raw only; hostile probe prints `DENIED` for every isolation
attempt; **T0.9 shows the peer received the frame and the bridge capture contains
it**. If any primitive fails on a target, that target is `unsupported` and the
approach is re-examined before P1.

---

## P1 — `internal/tool` engine + `tool` kind, headless, with cgroup + full lifecycle

**Goal:** a `tool`-kind node with no real pack loads, starts to `running`,
hot-connects/detaches, survives supervisor `kill -9`, and stops cleanly — proven
against the **stub GUI** promoted from P0 (item #11).

### Tasks

- **T1.1 — Schema + validation.** `lab.go`: add `KindTool Kind = "tool"`.
  `validate.go`: `kind==tool` requires `config.pack` (known installed pack id),
  single interface `eth1`, at-most-one link endpoint, multi-home rejected. No new
  top-level `Node` fields (rides `Config`).
- **T1.2 — `nodeRuntime.tool *tool.Endpoint`** in `loaded.go`, beside `extnet` /
  `vtap`.
- **T1.3 — Manifest contract (`manifest.go`) — items #7, #8, #12.**
  - **#7 single source of truth:** `pack.json` is **validated supervisor-side
    metadata ONLY** — used for node-config validation and palette display. The pack
    GUI's own compiled module defs remain **authoritative** for what actually runs.
    A **build-time test** (`manifest_keys_test.go`, run in the pack's own build)
    asserts the set of module keys in `pack.json` equals the set of compiled
    `moduleDefs` keys; a mismatch fails the build. State in the file's doc comment
    that the supervisor never executes from `pack.json`'s `modules` list.
  - **#8 path canonicalization:** every path in `pack.json` (`gui.bin`, each
    `script`) is **pack-relative**; resolution = `filepath.Join(packRoot, p)` then
    `filepath.EvalSymlinks`/`Clean`, then assert `strings.HasPrefix(resolved,
    packRoot+sep)` — reject `..` and symlink escape. Remove the spec's stale
    "absolute path inside the pack directory" wording; the manifest stores
    *relative* paths, resolution *produces* a contained absolute path.
  - **#12 health path:** add required `gui.health` string field (e.g.
    `"/healthz"`). Liveness/readiness probes (T1.7) hit **exactly** this path — a
    200 means "serving"; a 404/connection-refused means "wedged or route missing",
    and the two are no longer conflated by an arbitrary `GET /`.
  - caps allowlist = `{NET_RAW}` only; scrubbed-env allowlist as spec §2.6.
- **T1.4 — cgroup cage (`cage_linux.go`) — item #4 + review Critical #2.**
  **3-level hierarchy, enforcing cgroup v2's no-internal-processes rule** (see T0.3):
  the supervisor's own delegated cgroup `<D>` (from `/proc/self/cgroup`; `Delegate=yes`
  guarantees write access) is the **controller-enabling root that must hold NO
  processes**, so cages are **never** children of `<D>` directly while the supervisor
  also sits in `<D>`. Instead:
  - **Startup (T1.11), in order:** (a) create `<D>/supervisor/`; (b) migrate the
    supervisor's own PID into it (`echo $$ > <D>/supervisor/cgroup.procs`) so `<D>`
    is process-empty; (c) **only then** enable `+memory +pids +cpu` in
    `<D>/cgroup.subtree_control`. Doing (c) before (b) fails `EBUSY` — the guard is
    real, not theoretical.
  - **Per node at `Start`:** create the **sibling leaf** cage
    `<D>/tool-<id>/` (a peer of `<D>/supervisor/`, node-id-derived name so fabric
    code and `ReapStale` can compute it — see T1.9 for how install-scoping is
    achieved without embedding a tag in the cgroup **name**). Write `memory.max`,
    `pids.max`, `cpu.max`, `memory.swap.max=0` **before** launch; child placed via
    `clone3(CLONE_INTO_CGROUP)` (`SysProcAttr.CgroupFD`, Go 1.22+) with
    `cgroup.procs` pre-exec write as fallback. Atomic placement means limits bind
    before the child can allocate.
  No `/sys/fs/cgroup/iolbox/` hardcode; the whole subtree hangs off `<D>`, which is
  install-scoped by construction (a second install has a different service cgroup).
- **T1.5 — netns + veth (`netns_linux.go`) — item #3.** Pinned collision-free
  sequence:
  ```
  ip netns add iolt<id>
  TMP=vtoolp<id>                         # deterministic temp name, unique per node, never "eth1"
  ip link add vtool<id> type veth peer name $TMP
  ip link set $TMP netns iolt<id>
  ip netns exec iolt<id> ip link set $TMP name eth1     # rename INSIDE the netns
  ip netns exec iolt<id> ip link set eth1 up
  ip netns exec iolt<id> ip link set lo up
  ip link set vtool<id> up
  ```
  The peer is **never** named `eth1` in the root netns; it is created under a unique
  temp name, moved, then renamed to `eth1` inside `iolt<id>`. Bridge-side `vtool<id>`
  stays in root ns for capture/dirstat.
- **T1.6 — `/31` mgmt fallback + its firewalling (`netns_linux.go`) — item #10.**
  Only used when a pack manifest declares `gui.transport != "unix"`. When used,
  document at the call site that the ported interface-locks (`stripIfaceFlag`,
  `enforce_lab_iface`, `SO_BINDTODEVICE`) become **load-bearing again** (a second
  interface `mgmt0` now exists in the netns, so "exactly one non-lo interface" no
  longer holds by construction). Inside the netns, install restrictive rules on
  `mgmt0`: default-drop FORWARD, allow only host↔`mgmt0` on the mgmt `/31`, no
  forwarding between `mgmt0` and `eth1`, host-only. AF_UNIX remains the default and
  needs none of this.
- **T1.7 — exit-watcher + readiness + liveness (`endpoint_linux.go`) — item #12.**
  `cmd.Wait()` goroutine → `running→crashed` on unexpected exit + teardown.
  Readiness: after launch, HTTP `GET <gui.health>` over the AF_UNIX socket until 200
  or bounded timeout, then flip `running`. Liveness: periodic `GET <gui.health>`; N
  consecutive non-200 → `crashed` + `cgroup.kill`. Probes hit `gui.health`
  specifically, never an arbitrary path.
- **T1.8 — subreaper reap loop (`reap_linux.go`) — item #5 + review Critical #3.**
  **Ownership model, stated unambiguously:** `exec.Cmd.Wait()` remains
  **authoritative for every DIRECT child** the supervisor spawns — the pack GUI here,
  exactly as IOL/VPCS already work today (`spawn_linux.go:188`, `:396`). The
  subreaper loop handles **ONLY re-parented orphans** (grandchildren the pack GUI
  spawned — e.g. a scapy attack script — that were orphaned when the GUI died without
  reaping them); these reparent to the supervisor via `PR_SET_CHILD_SUBREAPER` and
  are **never** a PID any `exec.Cmd` is waiting on. Mechanism (from T0.6): the
  supervisor sets `PR_SET_CHILD_SUBREAPER=1` at startup (T1.11) and maintains a
  **registry of directly-spawned PIDs** (register on `exec.Cmd` start, deregister
  when that child's own `cmd.Wait()` returns). The `SIGCHLD`-driven loop uses
  `wait4(-1, WNOHANG|WNOWAIT)` to **peek**; a peeked PID **in** the registry is left
  untouched for its `cmd.Wait()`, a peeked PID **not** in the registry is reaped with
  `wait4(pid, WNOHANG)`. This is **separate from and in addition to** `cgroup.kill`
  (which terminates but does not reap). The loop lives at **supervisor scope** (not
  per-endpoint) because orphans reparent to the supervisor, not to any Endpoint. The
  registry is what prevents the loop from stealing the exit status of the GUI/IOL/VPCS
  direct children — never a blind `wait4(-1)` reap.
- **T1.9 — durable install identity + state-file `ReapStale` (`instance.go`,
  `reap_linux.go`) — item #9 + review Critical #4.** The reviewed design was
  **self-contradictory**: a per-start random UUID (or a `boot_id`, which changes
  every reboot) *cannot* recognize a **previous** instance's leftover objects — which
  defeats the exact "clean up after `kill -9`" scenario the P0/P1 gates prove. Fixed
  with a **durable identity that survives process restart but is still scoped to
  this install**, plus a state file — **not** by embedding a random tag in kernel
  object names (which also collided with the deterministic `vtool<id>`/`iolt<id>`
  names fabric code and spec §4.2 point 5 require, and blew the 15-char `IFNAMSIZ`
  limit on veth names):
  - **Durable install id:** a UUID generated **once on first run** and persisted to
    `/var/lib/iolbox/instance-id` (mode `0600`), **read on every subsequent start**
    (never regenerated while the file exists; `flock` around the read-or-create). It
    is the same value across a crash-and-restart of the **same** install, and
    different for a genuinely different install (a second appliance has its own data
    dir → its own file). This is the identity the reviewed draft lacked.
  - **Deterministic, node-id-derived kernel object names** (unchanged from spec):
    cgroup `<D>/tool-<id>/` (T1.4), netns `iolt<id>`, veth `vtool<id>` / `mtool<id>`
    (fabric-computable from node id alone, within `IFNAMSIZ`), socket dir
    `/run/iolbox/tool/<id>/`. **No random tag in any name.**
  - **State file as the lookup key:** `/var/lib/iolbox/tool-objects.json`, owned by
    the install, records — keyed under the durable install id — every kernel object
    this install has created (cgroup path, netns name, veth names, socket dir),
    **written before each object is created** and pruned after clean teardown.
  - **`ReapStale()` on startup** reads the state file for **THIS install's id** and
    destroys exactly the objects it lists — which, because the id is durable, are the
    prior (crashed) run's leftovers of the **same** install — via `cgroup.kill` +
    rmdir, `ip netns del`, veth del, socket-dir rm. It then best-effort sweeps the
    supervisor's **own delegated cgroup subtree `<D>`** for any `tool-*` leaf not in
    the file (belt-and-suspenders; `<D>` is install-scoped by construction, so this
    can never touch another install's cages). It **never** wildcard-sweeps host-global
    `iolt*`/`vtool*` names that could belong to a different install.
  - **Documented charter assumption (stated once):** iolbox is
    single-supervisor-per-host (`PLAN.md:4`). The durable-id + `<D>`-subtree scoping
    defends against cross-*generation* leaks (old process / prior crash) **and**
    against a genuinely separate install, not against an unsupported concurrent
    second supervisor sharing this install's data dir. Do not re-litigate per object.
  - `prestart-clean.sh` keeps its blunt best-effort `iol*` sweep for the
    catastrophic case, but the in-process `ReapStale` is the durable, precise path.
- **T1.10 — `startToolNode` + `stopNode` branch + fabric points (item #6 of §4.2).**
  `handlers.go`: `KindTool` branch in `startNodes` calling `startToolNode` (preclean
  → cage → netns/veth → socket dir → launch w/ transition → exit-watcher → readiness
  → `running` → `attachFabricForNode`). `stopNode` gains `nr.tool != nil` →
  `Endpoint.Stop()` (bounded SIGTERM-to-pgroup then `cgroup.kill`, then detach/del
  veth/netns/cgroup/socketdir, all idempotent). **Fabric points across TWO files
  (review High #7 — corrected from the spec's single `fabric_linux.go` claim):**
  - **`fabric.go` (portable):** spec §4.2 **point 1** — add `KindTool` to
    `fabricNodes` (verified `fabric.go:39-48`, the `switch doc.Nodes[i].Kind` case
    list) so a tool endpoint is fabric-eligible; `isFabricLink` then admits its link.
    This edit must compile on the `_other` build too (portable file).
  - **`fabric_linux.go` (Linux-only):** spec §4.2 **points 2–8** — attach
    (`:296` switch), detach (`:474` switch), late-start (`attachFabricForNode`),
    `fabricLinkTapDevs`→`vtool<id>` (`:603` region, feeds stats + dirstat),
    `fabricLinkFullyAttached`, slow-tee conscious-skip (`linkIsIOLToIOL` already
    excludes tool), teardown loop.
- **T1.11 — startup wiring + systemd `Delegate=yes` (item #4 + review Critical #2).**
  `server.go`: call `tool.Detect()` and `tool.ReapStale()` beside `extnet.Detect`
  (verified `server.go:122`, `s.caps = extnet.Detect(...)` — the tool calls go
  immediately after); set `PR_SET_CHILD_SUBREAPER=1` on the supervisor process (T1.8);
  merge `tool.Capabilities.GateFeatures()` (`"tools"`) into hello (registered beside
  the other verbs at `server.go:132-158`); register `tool.listPacks` (item #8).
  **cgroup bring-up, in the exact no-internal-processes order (T1.4):** (1) create
  `<D>/supervisor/`; (2) migrate the supervisor's own PID into it so `<D>` is
  process-empty; (3) enable `+memory +pids +cpu` in `<D>/cgroup.subtree_control`
  — all **before any cage is created**. Steps (1)–(2) MUST precede (3) or (3) fails
  `EBUSY`. `iolbox-supervisor.service`: add `Delegate=yes` (and `Delegate=memory pids
  cpu` for explicitness) so the supervisor owns a writable delegated sub-cgroup on
  systemd-managed OVA/native targets instead of fighting systemd for the fs root.
- **T1.12 — `Detect` capability matrix (`detect_linux.go`) — item #4 probe parity.**
  The probe creates its cage as a **level-3 leaf under the same delegated root `<D>`**
  as production tool cages (T1.4) — a sibling of `<D>/supervisor/`, after the
  migrate-first controller enable — not at fs root, so a pass genuinely predicts
  production success (a probe that enabled controllers on a different, empty parent
  would not prove the real `<D>` accepts subtree_control). Matrix:
  `{netnsCreate, vethCreate, vethMoveRename, cgroupDelegated, ambientCapTransition,
  unixProxy}`; `cgroupDelegated` fails if the migrate-first + subtree_control
  sequence errors on the real `<D>`. Advertise `"tools"` only if all required pass.

### Gate to P2
`go test ./internal/tool/... ./internal/server/...` green. Then, driving raw NDJSON
against a running supervisor (fabric-harness style) with a one-node stub-pack lab:
`lab.load`+`lab.start` → node reaches `running` (readiness probe against stub GUI's
`/healthz` succeeds, item #11/#12); `link.add`/`link.remove` hot-connect/detach with
**no** node restart; `kill -9 <supervisor>` then restart → `ReapStale` removes the
node's cage/netns/veth (assert `ip netns list` and `ls <D>/tool-*`
empty for this install) with **no zombie** in `/proc` (item #5 assertion); `lab.stop`
leaves nothing behind.

---

## P2 — secbench pack + palette + AF_UNIX proxy **with its security boundary** (frontend slice)

**Goal:** first user-visible slice — drag SecBench, wire it to an IOL switch, open
its console (the reverse-proxied htmx GUI), run ARP Spoof, watch it in capture.

**FORCED REORDERING (item #1):** the `/tool/{nodeId}` reverse-proxy **security
design** — a **new boot-token session gate** (`iolbox_session` HttpOnly
SameSite=Strict cookie, minted per supervisor boot, validated on `/control` **and**
`/tool/*`), an **Origin allowlist**, reject-by-default WS upgrades, path/traversal
allowlist, and CSP `frame-ancestors` — is built and tested **in this phase**, not
deferred to P3. It is genuinely *new* code, not a reuse: wsbridge has **no** auth or
Origin check today (verified `wsbridge.go:24-34`, `:134-141`). Rationale (from
review): a pack-controlled page served on the control-plane origin, with no Origin
check and no session, lets a cross-site request remotely trigger an attack module —
a CSRF-to-attack primitive. The boundary must exist the moment a proxied pack page
is first served, which is here. The spec's original P2-serves / P3-secures split is
explicitly overridden. Full mechanism, exact names, and tests: **T2.5**.

### Tasks

- **T2.1 — secbench pack port.** `pack.json` (6 groups, 18 L2/L3 modules; no
  `ngfw`, no `victim`, spec §4.1) with `gui.health` set; the 18 `attacks/*.py` +
  `common.py`; GUI trimmed to 18 modules, listening on AF_UNIX (`net.Listen("unix",
  …)`) — the only GUI change. Build-time key-match test (T1.3/#7) wired into the
  pack build.
- **SUPERSEDED by `docs/p2-go-wireup-plan.md` — the shipped pack now runs static Go binaries, no Python/venv/wheelhouse.**
- **T2.2 — offline wheelhouse in rootfs (`build-rootfs.sh`).** Per spec §7:
  `pip download` scapy+pinned deps to `wheelhouse/` with `--require-hashes`; install
  `--no-index --find-links` into `/opt/iolbox/tools/venv`; verify imports; add
  `python3 python3-venv libpcap0.8 util-linux` to `BASE_INCLUDE`; create the
  `ioltool` account; `py_compile`; delete wheelhouse+caches. Record hashes/SBOM.
- **T2.3 — socket-dir + `options.json` permission wiring (item #2 + review
  High #5).** `startToolNode` creates `/run/iolbox/tool/<id>/` **chown `ioltool`,
  mode `0700`**, parent `/run/iolbox/tool/` **root-owned `0755`** (traversable, not
  writable). The node's **options/config file** (the ex-`/firstboot.cfg` intake) is
  written into this same `ioltool`-owned dir so the GUI can read (and, if it must,
  rewrite) it; its path is passed via the scrubbed env. **Pinned ownership/mode/write
  discipline (was never specified):**
  - Name it exactly `/run/iolbox/tool/<id>/options.json`.
  - Created **`ioltool:ioltool`, mode `0600`** (readable/writable only by the tool
    account; no group/other access — it may carry per-lab option values).
  - **Written atomically:** the supervisor writes to a temp file in the **same dir**
    (`options.json.tmp-<rand>`), `fchown`s it to `ioltool:ioltool`, `chmod 0600`,
    `fsync`, then `rename(2)` over `options.json` — so the GUI never observes a
    partial/half-written file, and never a root-owned file it cannot re-open.
  - **Test (required):** a unit/integration test that, **running as the `ioltool`
    user** (or a uid-dropped subprocess), `bind`s the socket in the dir **and**
    opens `options.json` for both read and write and succeeds; and asserts a
    non-`ioltool` uid (other than root) is denied by the `0700`/`0600` modes. This
    is the test that proves the ownership actually lets the launched GUI function.
- **T2.4 — `/tool/{nodeId}` reverse proxy (`wsbridge/proxy.go`).** HTTP/WS proxy
  (not a raw byte pump) to the node's AF_UNIX socket. Serves secbench's htmx UI in
  the console-dock panel.
- **T2.5 — proxy security (item #1 + review Critical #1) — SAME PHASE. First
  builds a real auth model, because wsbridge has none today.** Verified: `wsbridge`
  currently has **no session/auth/Origin concept at all** — its package doc
  (`wsbridge.go:24-34`) states "The WebSocket handshake performs no Origin check …
  the trust boundary is the VM's own network exposure", and the mux
  (`wsbridge.go:134-141`) registers `/control`, `/console/`, `/capture/`,
  `/api/upload/image`, `/` with **zero** authentication. So "reuse control-session
  authorization" has nothing to reuse; this task **defines** that authorization and
  applies it to **both** `/control` and `/tool/*`.

  **Auth model (minimal, appliance-appropriate):**
  - **Boot bootstrap token.** At supervisor start, mint a 256-bit random
    `sessionToken` (`crypto/rand`), held in memory for the process lifetime (a new
    one each boot — no persistence).
  - **Delivery to the SPA.** When the bridge serves the SPA shell (the `/`
    catch-all → `web.Handler()`, `wsbridge.go:147`), it sets the response cookie
    **`iolbox_session=<sessionToken>`** with attributes **`HttpOnly; SameSite=Strict;
    Path=/`** (add `Secure` whenever the listener is TLS). The SPA never reads the
    value (HttpOnly); the browser re-attaches it automatically on same-site requests,
    **including the WS handshakes** to `/control` and `/tool/*`.
  - **Validation middleware** wrapping `/control`, `/console/*`, `/capture/*`, and
    `/tool/*` (the network-exposed routes; the loopback NDJSON TCP control socket in
    `internal/server` stays local-trust and is unchanged). It requires **both**:
    1. **Session:** the `iolbox_session` cookie equals `sessionToken`, else **401**.
       For a headless/CLI client with no cookie jar, an **`Authorization: Bearer
       <sessionToken>`** header is accepted equivalently.
    2. **Origin allowlist:** the request `Origin` header (WS handshake) — or `Referer`
       host as fallback for a plain GET — equals the configured control-plane origin
       (derived from `-ws-addr`), else **403**. Requests with a foreign or absent
       Origin on a state-changing path are rejected.
    Because the cookie is **`SameSite=Strict`**, a cross-site page's request carries
    **no** `iolbox_session` at all → 401 before the Origin check even matters; the two
    checks are belt-and-suspenders and together close the **CSRF-to-attack** primitive
    the review named (a cross-site page can neither present the boot token nor a valid
    Origin, so it can never POST to `/tool/7/attacks/arp_spoof`).
  - **iframe sandbox.** The ToolNode renders the proxied pack GUI in
    `<iframe sandbox="allow-scripts allow-forms">`. **`allow-same-origin` is
    considered and its trade-off stated explicitly:** omitting it puts the pack page
    in a unique **opaque origin** (cannot script the parent SPA, cannot read its DOM
    or ride its `iolbox_session` cookie toward `/control`) — the stronger posture —
    **but** secbench's htmx UI issues same-origin XHR that, from an opaque origin,
    becomes credential-less cross-origin and breaks. **Decision:** keep
    `allow-same-origin` **only because the pack is first-party, immutable, root-owned
    trusted code** (spec §6.1) — the same trust ruling that underlies "netns is fine"
    — so a *malicious pack scripting the parent* is out of threat model, while the
    threat that IS in model (a **cross-site** attacker) is fully closed by
    SameSite=Strict + Origin allowlist regardless of the sandbox bit. The residual
    same-origin exposure is bounded by: the **HttpOnly** cookie (a pack cannot read or
    exfiltrate the token), a response **`Content-Security-Policy: frame-ancestors
    'self'`** (a cross-site page cannot embed the pack frame), and header
    sanitization below. Record this as an accepted-risk line beside §2.5.3.
  - **Reject-by-default WS upgrades:** an `Upgrade: websocket` on `/tool/…` is
    refused unless the pack manifest's proxy allowlist explicitly permits that path.
    Test: WS upgrade to a non-allowlisted path → 400/403.
  - **Path allowlist + traversal:** only paths matching the pack's declared route
    prefixes are proxied; everything else 404. The proxy `path.Clean`s and rejects
    any `..`/escape so `/tool/7/../control` can never address the control plane.
  - **Header sanitization:** strip inbound `X-Forwarded-*`/`Forwarded`, and do **not**
    forward the `iolbox_session` cookie to the netns pack process (the session gate is
    consumed at the proxy; the pack never sees the token).

  **Required tests (all in `wsbridge_test.go`):**
  - `Origin: http://evil.example` (with a valid cookie) to `/tool/7/attacks/arp_spoof`
    → **403**.
  - **No** `iolbox_session` cookie and no Bearer → `GET /tool/7/…` and the `/tool/7`
    WS handshake → **401** (this is the exact cross-origin-rejection proof the review
    demanded: a cross-site request, lacking the SameSite=Strict cookie, is rejected).
  - Valid cookie + correct Origin → **200/101** (the happy path still works).
  - WS upgrade to a non-allowlisted `/tool/7/…` path → **400/403**.
  - `/tool/7/../control` normalizes and does not reach `/control` → **404**.
  - A proxied HTML response carries `Content-Security-Policy: frame-ancestors 'self'`.
  - The same session gate now also rejects an unauthenticated `/control` handshake
    (proving the gate is shared, not `/tool`-only).
- **T2.6 — proxy URL rewriter (item #6 + review High #6 — pin the concrete
  mechanism).** **Decision: proxy-side rewriting** (keeps "don't touch the pack GUI"
  intact — the AF_UNIX switch stays the *only* GUI change). secbench's htmx UI emits
  **root-absolute** URLs that break under the `/tool/{nodeId}/` prefix. The reviewed
  wording glossed over transfer-encoding realities; the mechanism is pinned here so
  the rewriter is actually implementable and can never see bytes it cannot parse:
  - **Strip upstream compression:** the proxy sends the upstream (netns AF_UNIX GUI)
    request with **`Accept-Encoding` removed**, so responses arrive uncompressed and
    are rewritable as plain text. (The GUI is loopback/AF_UNIX — no bandwidth cost.)
    Any `Content-Encoding` still present on a response is treated as opaque and
    **not** rewritten (passed through only for non-HTML asset types).
  - **Rewrite only bounded `text/html`:** rewrite a response **only** when its
    `Content-Type` is `text/html` **and** its length (post de-chunk) is within a hard
    cap (e.g. 2 MiB); buffer that bounded body fully, rewrite, and re-emit with a
    corrected `Content-Length` (dropping any inbound `Transfer-Encoding: chunked` —
    Go's `net/http` server re-frames the response, so chunking is handled by
    buffering, not streamed-through byte-patching). Non-HTML (JS/CSS/images) and
    over-cap bodies are streamed through **unmodified**.
  - **Parse, don't regex:** rewrite with a real HTML tokenizer
    (`golang.org/x/net/html`, or stdlib `html.NewTokenizer`) — never string
    substitution — rewriting root-absolute `href`/`src`/`action`/`hx-get`/`hx-post`/
    `hx-*`/`<form action>` and `<link>`/`<script>` asset paths from `/foo` →
    `/tool/<id>/foo`.
  - **Neutralize `<base>`:** if the pack page emits a `<base href>`, rewrite it to
    `/tool/<id>/` (or strip it) so it cannot re-root relative URLs outside the
    prefix.
  - **Redirects:** rewrite `Location:` headers on 3xx from `/foo` → `/tool/<id>/foo`.
  - **WS URLs:** rewrite root-absolute WS targets the page opens in markup to
    `/tool/<id>/foo`.
  - **JS-generated URLs are explicitly UNSUPPORTED in v1** — this is a real
    **pack-authoring constraint**, documented, not silently dropped: a pack GUI's
    client-side JS must **not** build `fetch()`/`WebSocket()`/dynamic-`src` URLs from
    a hardcoded leading `/` (the proxy cannot see or rewrite strings assembled at
    runtime in the browser). Packs must use **relative** URLs (no leading slash) or
    read a base path the page exposes. secbench's htmx uses attribute-driven
    root-absolute URLs (rewritable) and does not construct URLs in JS, so it ports;
    a future pack that does must follow this constraint. State it in `proxy.go`'s doc
    **and** in the pack-authoring section of the manifest contract.
  The rewriter is the mechanism that makes the "Level A = only AF_UNIX change" claim
  true; state in `proxy.go` that base-path rewriting lives here, not in the pack.
- **T2.7 — frontend slice.** `labTypes.ts` `NodeKind` += `"tool"` + fix every
  exhaustive switch; `Palette.svelte` drag payload; **new**
  `app/src/lib/nodes/ToolNode.svelte` (status LED + name chip, panel opens the
  proxied GUI iframe); `NodeEditDialog.svelte` (Name + pack + manifest `options`);
  `Console.svelte` tool-panel tab hosting the `/tool/{nodeId}` iframe; `tool.listPacks`
  **client** transport/types/state/error handling in `protocol.ts`.
- **T2.8 — `tool.listPacks` BACKEND wiring (review High #8 — was missing; the plan
  had only frontend files).** The verb needs a full server round-trip, mirroring the
  existing verbs (registered at `server.go:132-158`, typed in
  `internal/protocol/verbs.go`, handled in `handlers.go`):
  - **Protocol types** in `internal/protocol/verbs.go`: `ToolListPacksArgs` (empty /
    no args) and `ToolListPacksResult` (`[]PackInfo` — id, name, icon, groups,
    per-module `fields`/`mitigation` metadata the palette + edit dialog render).
  - **Handler** `handleToolListPacks` in `internal/server/handlers.go` (or a small
    `tool_handlers.go`): reads the **pack registry** and returns the installed packs'
    validated manifest metadata (never the raw `pack.json`; the already-validated
    in-memory `[]tool.Pack`).
  - **Dispatcher registration** in `server.go`: `s.disp.Handle("tool.listPacks",
    s.handleToolListPacks)` alongside the others.
  - **Pack registry ownership:** `tool.Detect()` (T1.12, run at startup) enumerates
    and validates `/opt/iolbox/tools/packs/*/pack.json` (manifest contract T1.3) and
    caches the resulting `[]tool.Pack` on the server (a `s.packs` field beside
    `s.caps`); `handleToolListPacks` serves from that cache. This is the single
    source of installed-pack truth the frontend `tool.listPacks` client (T2.7) and
    `validate.go`'s `config.pack`-known check (T1.1) both consult.

### Gate to P3
`go test ./internal/wsbridge/... ./internal/server/... ./internal/protocol/...`
green, incl. every T2.5 security test (401 with no `iolbox_session` cookie, 403 on
foreign Origin, happy-path 200/101, WS-upgrade reject, traversal 404, CSP header,
shared-gate `/control` reject), the T2.6 rewriter test, the T2.3 `options.json`
ownership test (bind+read+write as `ioltool`), and a `tool.listPacks` round-trip test
(T2.8). Manual e2e (agent-browser, per MEMORY GUI-check policy): drag SecBench → wire
to IOL switch → node `running` → open console → htmx cards render correctly under the
`/tool/<id>/` prefix (rewriter works) → run ARP Spoof → frames visible in the link's
capture tab. Security assertions observable: a cross-site POST to a module endpoint
(no cookie / foreign Origin) returns 401/403.

---

## P3 — hardening + per-target matrix (web boundary already shipped in P2)

**Goal:** finish the residual hardening and prove the feature on every target.
Because item #1 moved proxy security into P2, P3 no longer *designs* the web
boundary — it **regression-tests** it across targets and finishes the narrower
items.

### Tasks

- **T3.1 — NET_ADMIN-mediated-verb tightening.** Implement the narrow
  supervisor-mediated verbs for the few modules needing a one-shot NET_ADMIN op in
  the netns (route add for routing-protocol spoof), so no long-lived process ever
  holds NET_ADMIN (spec §2.5.1).
- **T3.2 — accepted-risk documentation (item #14).** Add to spec §2.5.3's honest
  scope list (and mirror in `tool.go` doc) the explicit statement: **concurrent tool
  nodes run under the SAME shared uid `ioltool` with no PID/user namespace, so they
  can `signal`/`ptrace` each other and read each other's `options.json` files.** This
  is a real accepted-risk item under the "netns is fine" ruling — recorded, not
  fixed in v1. The P0 hostile probe (T0.8) already demonstrates the cross-node read
  succeeds; reference that as the evidence.
- **T3.3 — per-target validation matrix.** VMware/OVA/native/QEMU/WSL2/LXC/Docker:
  `tool up`, `hot-connect`, `capture a tool link`, `kill -9` recovery, **and** re-run
  the P2 proxy-security tests on each. Unprivileged LXC / rootless-Docker fail closed
  (feature `tools` absent, palette hides tool nodes). Mount/PID/user namespaces
  remain out of scope (spec §0.2) — recorded as a known limitation, not a task.

### Gate to P4
The matrix table is filled with actual pass/fail + measured compressed image size
per target (spec §7 owes real numbers).

---

## P4 — (optional) second tool pack, to prove the plugin contract

**Goal:** confirm a second, non-attack Level-A pack drops in via `pack.json` +
rootfs venv/payload with **zero new Go supervisor code** — the real test of the
§2.6 contract and of item #7's "GUI defs authoritative, manifest metadata only"
split. No native-Svelte-cards (former "Level B") deliverable.

---

## T. Traceability — every punch-list item → phase → task/file

| # | Punch-list item (short) | Phase | Concrete task / file |
|---|---|---|---|
| 1 | Proxy security built in **same phase** as first proxied page — **NEW boot-token session gate** (`iolbox_session` HttpOnly SameSite=Strict cookie on `/control`+`/tool/*`), Origin allowlist, reject-by-default WS, path/traversal allowlist, CSP `frame-ancestors`, header sanitize | **P2** (forced-forward from P3) | **T2.5** — `internal/wsbridge/wsbridge.go` (session gate) + `proxy.go`; tests in `wsbridge_test.go` |
| 2 | Per-node socket/option dir owned by `ioltool` (parent root); GUI socket **and** option file both readable/writable by `ioltool` | P0 probe + **P2** wiring | **T0.5**, **T2.3** — `/run/iolbox/tool/<id>/` chown `ioltool` `0700`; `options.json` `ioltool:ioltool 0600` atomic (temp+rename); `endpoint_linux.go` |
| 3 | veth create-order bug — deterministic temp name → move → rename to `eth1` inside netns | P0 probe + **P1** | **T0.4**, **T1.5** — `netns_linux.go` pinned sequence |
| 4 | cgroup delegation + **no-internal-processes 3-level layout** — `Delegate=yes`; `<D>` root holds NO procs; supervisor migrates to `<D>/supervisor/` leaf **before** enabling subtree_control; cages are `<D>/tool-<id>/` sibling leaves; probe same parent as prod | **P1** | **T0.3/T1.4/T1.11/T1.12** — `cage_linux.go`, `server.go`, `detect_linux.go`, `iolbox-supervisor.service` |
| 5 | Reaping — `PR_SET_CHILD_SUBREAPER` + **ownership-split** loop (`WNOWAIT` peek + directly-spawned-PID registry; reap only unregistered orphans) so it never races `exec.Cmd.Wait()`; separate from `cgroup.kill` | P0 probe + **P1** | **T0.6**, **T1.8** — `reap_linux.go` (supervisor-scope loop) |
| 6 | Mount-prefix contract — proxy-side rewriting with pinned mechanism (strip `Accept-Encoding`, bounded `text/html` buffer + de-chunk, `x/net/html` tokenizer, `<base>` neutralize, redirects/WS/form; **JS-generated URLs unsupported = pack constraint**) | **P2** | **T2.6** — `wsbridge/proxy.go` rewriter |
| 7 | Manifest single source of truth — `pack.json` = metadata only; GUI compiled defs authoritative; build-time key-match test | **P1** contract + **P2** wiring | **T1.3**, **T2.1** — `manifest.go`, `manifest_keys_test.go` |
| 8 | Manifest path canonicalization — pack-relative, join→canonicalize→assert-contained; fix "absolute path" wording | **P1** | **T1.3** — `manifest.go` |
| 9 | `ReapStale()` scope — **durable per-install id** (UUID persisted `/var/lib/iolbox/instance-id`, read not regenerated) + **state-file lookup**; deterministic node-id-derived object names; `<D>`-subtree scoping; single-supervisor charter stated once | **P1** | **T1.9** — `instance.go`, `reap_linux.go` |
| 10 | `/31` mgmt fallback — interface-locks become load-bearing; `mgmt0` restrictive host-only iptables | **P1** | **T1.6** — `netns_linux.go` |
| 11 | P1 needs a real launch target — stub GUI promoted from P0 | P0 build + **P1** gate | **T0.1** → P1 gate — `tools/tool-stubgui/main.go` |
| 12 | `gui.health` manifest field; liveness/readiness hit that path specifically | **P1** | **T1.3**, **T1.7** — `manifest.go`, `endpoint_linux.go`; stub uses `/healthz` (T0.1) |
| 13 | `setpriv` pinned + version-checked (or native launcher); P0 test asserts final `/proc/self/status` state, not exit 0 | **P0** | **T0.2** — `launch_linux.go`; gate asserts CapEff/Prm/Inh/Amb/Bnd |
| 14 | §2.5.3 must state concurrent nodes share uid `ioltool`, no PID/user ns → mutual signal/ptrace/option-file read | P0 evidence + **P3** doc | **T0.8**, **T3.2** — spec §2.5.3 + `tool.go` doc |

All 14 items are mapped to at least one phase task above.

### T.2 — final adversarial pass (this revision) → resolution

A `sol-medium` pass on the execution plan itself surfaced 9 further items (4
Critical, 5 High), each verified against the current tree and resolved **in place**
above:

| Finding | Verified fact | Resolved in |
|---|---|---|
| **C1** proxy has no auth to "reuse" | `wsbridge.go:24-34/134-141` — no Origin check, no session, unauthenticated mux | **T2.5** + P2 intro — new `iolbox_session` boot-token gate + Origin allowlist; iframe trade-off stated |
| **C2** cgroup violates no-internal-processes | enabling `subtree_control` on the cgroup that holds the supervisor PID is illegal | **T0.3/T1.4/T1.11/T1.12** — 3-level `<D>` → `<D>/supervisor/` → `<D>/tool-<id>/` |
| **C3** reaper races `exec.Cmd.Wait()` | IOL/VPCS/GUI are direct `exec.Cmd` children with `cmd.Wait()` (`spawn_linux.go:188/396`); `wait4(-1)` steals their status | **T0.6/T1.8** — `WNOWAIT` peek + spawned-PID registry; reap orphans only |
| **C4** instance tag self-contradictory | a per-start random tag can't recognize the prior crash's objects; tagged names blow `IFNAMSIZ` and clash with `vtool<id>` | **T1.9** — durable persisted install id + state file; deterministic object names |
| **H5** `options.json` mode unpinned | never specified | **T2.3** — `ioltool:ioltool 0600`, atomic temp+rename, bind/read/write-as-`ioltool` test |
| **H6** rewriter ignores compression/chunking/`<base>`/JS URLs | glossed in draft | **T2.6** — strip `Accept-Encoding`, bounded HTML buffer, `x/net/html`, `<base>` fix, JS-URL unsupported constraint |
| **H7** fabric eligibility file drift | `fabricNodes`/`isFabricLink` are in **portable** `fabric.go:39-48`, not `fabric_linux.go` | file map note + T1.10 split (eligibility = `fabric.go`; attach/detach/stats = `fabric_linux.go`) |
| **H8** `tool.listPacks` backend unlisted | verbs live in `server.go:132-158` + `protocol/verbs.go` + `handlers.go`; plan listed only frontend | **T2.8** — protocol types + handler + dispatcher registration + pack-registry ownership |
| **H9** P0 lacked a fabric/L2/capture proof | plan's P0 (T0.1–T0.8) dropped spec §8-P0-item-1 | **T0.9** — frame crosses `iolbr0` to a peer + is captured, gated before P1 |
