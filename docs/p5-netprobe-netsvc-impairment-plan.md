# P5 — PC node (`netprobe`), infrastructure-services pack (`netsvc`), and per-link fault injection

Status: **dispatch plan. Reviewed by `codex sol-medium`; findings applied (§1a). Not
implemented.** Three batches drawn from
`docs/learning-features-gui-ideas-plan.md` (ideas #1, #2/#7, #3). Batch A is the largest
lift in this repo since the fabric migration — it adds a fifth first-class `NodeKind` and
touches ~20 kind-switch sites verified by grep below. Batches B and C are file-level
independent of A and of each other. **§4 corrects four load-bearing assumptions in the
idea doc that do not survive contact with the current code** — read §4 before §5/§6/§7.

## 1. Model loop / process

1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it.** One area per batch needs
   disproportionate attention — in each case the failure mode is *code that builds, passes
   its own unit tests, and does nothing (or the wrong thing) on real hardware*:
   - **Batch A — §5.2 and §5.3, the process/console/data-plane model.** The idea doc says
     "`spawnPC` modeled on `spawnVPCS`". §4.1 shows that is not implementable: VPCS carries
     its own userland TCP/IP stack and receives frames over a UDP tunnel, which a Go program
     using the kernel stack cannot consume. Get this wrong and PC nodes come up, show a
     prompt, and cannot ping anything. Review §5.2's netns-vs-host and §5.3's
     AF_UNIX-console-vs-TCP-console reasoning against `spawn_linux.go`, `argv.go:161-183`,
     `fabric_linux.go:601-640`, and `tool/endpoint_linux.go`.
   - **Batch B — §6.4, the DHCP/DNS/NTP wire layouts.** Same bug class as P4 §3.6's
     ACCT-REPLY field-order trap. NTP's 48-byte header and DNS's compression-pointer
     encoding are the two most likely to round-trip against themselves and fail against a
     real IOS `ntp server` / `ip name-server`. Review §6.4 byte-for-byte against RFC 5905
     §7.3 and RFC 1035 §4.1.4.
   - **Batch C — §7.2 (netem attachment point) and §7.4 (the `startFabric` re-attach
     interaction).** `tc qdisc add dev iolbrN root netem …` succeeds, reports no error, and
     impairs **nothing** for bridge-port-to-bridge-port forwarding. And `startFabric`
     self-heals bridge membership on every `node.start`, which will silently undo an
     admin-down link. Both are "green build, dead feature" traps. Verify §7.2's
     per-endpoint-egress claim and §7.4's bookkeeping fix.
3. **`codex luna-xhigh` agent(s) implement. Recommendation: three agents, Batch A alone
   and first-merged.** Batch A is the only batch that touches the supervisor's node
   dispatch, the lab schema, and the palette; B and C both re-read files A may have moved.
   B (a self-contained new pack directory + three `build-rootfs.sh` list entries) and C
   (supervisor `internal/fabric` + one new verb/event + `FloatingEdge`) share **zero**
   files and may run fully in parallel with each other. If A must run concurrently, B is
   the safer partner (B's only shared file with A is `runtime/build-rootfs.sh`, which A
   does not touch at all).
4. **Orchestrating session deploys to the real appliance VM and validates live** per §9.
   The plan attempts no VM steps and runs no builds.

## 1a. Review — codex sol-medium findings applied

`codex sol-medium` reviewed the draft and found **8 blocking issues + 1 minor** (all fixed in
this document before implementation). What sol-medium *confirmed* — §7.2's bridge-qdisc
reasoning, Batch A's process model, Batch B's privilege claim — is unchanged and must stay
unchanged.

| # | Finding | Resolution |
|---|---|---|
| 1 | "100% of links" is false: `computeStaticTaps` enumerates Ethernet only, so an IOL **serial** endpoint has no static tap even though `validate.go` accepts `s0/0`. | §3.4 / §7.1 rewritten: Batch C is scoped to **Ethernet-realizable fabric links**; `link.setFault` rejects a link with any tapless endpoint (`CodeUnsupported`), the GUI disables the Faults menu for it, and serial static-tap support is named out of scope (§10). No legacy relay fallback is invented. |
| 2 | Making `fabricLinkFullyAttached` return true for `down` links skips `EnsureBridge` and late-endpoint reconciliation. | §7.4 rewritten: the predicate stays a **pure kernel-state** check. `startFabric`/`attachFabricForNode` branch on the fault *first* into a new `reconcileFabricLinkDown`, which ensures the bridge, discovers every currently-present endpoint device, detaches the targeted ones, attaches the rest, and records the link realized. |
| 3 | `devs[0]` is not `endpoints[0]` (the slice is compacted), and `direction: 0\|1\|2` cannot address an N-endpoint segment. | New endpoint-indexed helper `fabricLinkEndpointDevs` → `[]endpointDev{EndpointIndex, Dev}` (§3.5, §7.2); `LinkFault.direction` **removed** and replaced by `targetEndpoint?: number` (absent = all endpoints), threaded through §7.5, §7.6, §7.7, §7.9 and §9.3. |
| 4 | An active netem fault is not reapplied when a stopped endpoint's device appears later. | §7.4 adds `reconcileLinkFault`, called after **every** attach path and node start: it applies the full fault to every present target device and clears qdiscs from every present non-target device. §7.9 gains the stopped-endpoint → set fault → start endpoint regression test. |
| 5 | `save` has no wire path to reach `config.pc.savedCommands` in the supervisor document. | §5.4a specifies the whole channel: the pack persists to its own `options.json` (existing atomic-rename `Store.Save` precedent), the supervisor **pulls** it over the node's existing GUI AF_UNIX socket via `GET /_iolbox/state`, merges the validated blob into `ll.doc` and emits `node.pcState`; the GUI writes it into the doc it sends to `lab.saveDoc`. Owner, protocol, synchronization and validation are all named. |
| 6 | `GUIBin: "pc-gui"` is a bare name; `exec.Command` would search `PATH`. | §4.2 / §5.4: the `pc` pack is loaded through the ordinary `tool.LoadPack` (absolute, containment-checked `GUIBin`) and then **moved out of** `s.toolPacks` into a private `s.pcPack` field. No hand-built `tool.Pack` literal anywhere. |
| 7 | `IOLBOX_PC_CLI_SOCK` cannot be delivered — `Config` has no such field and the launch env is a fixed list. | §5.3a: a narrowly typed `Config.CLISocket bool` + `tool.CLISocketFile(runRoot, nodeID)` helper + `Endpoint.CLISocketPath()`; the var is appended to the launch env only when the flag is set and added to `ScrubbedEnvAllowlist`. No general environment escape hatch. |
| 8 | Treating "cannot find device" as fatal for `opNetemClear` breaks idempotent teardown; and §3.5 wrongly claimed the NAT device is skipped when absent. | §7.3: `ClearNetem`'s benign set now includes the missing-**device** strings as well as the missing-**qdisc** strings, while permission/malformed failures stay fatal. §3.5 corrected (the NAT arm is unconditional); fault application uses an existence-filtered target set. §7.9 gains missing-device teardown tests. |
| 9 (minor) | The grep evidence misses a second stale in-repo reference. | §3.4 now names **`supervisor/README.md:199`** alongside `docs/bridge-fabric-retirement-map.md` as stale; correcting both is a separately scoped doc change (§10). |

## 2. Relationship to prior plans and repo conventions

- **`docs/learning-features-gui-ideas-plan.md` is the source design doc for all three
  features** (idea #1 `netprobe` + its "as a VPCS replacement" section; idea #2/#7
  `netsvc`; idea #3 link faults). Its locked decisions are carried forward verbatim and are
  **not re-litigated** here: PC is a first-class `NodeKind`, not a `tool` pack; the UI never
  says "netprobe"; `?`/`help` is in scope, not an afterthought; PC ships **alongside** VPCS,
  not as a replacement; faults default to inactive on lab reopen. Where the doc's
  *implementation* guidance contradicts the code, §4 records the correction and the reason.
- **`docs/p4-tacacs-syslog-plan.md`** (git, `1d14439`) — its Batch A landed. Its
  netns-scoped `ip_unprivileged_port_start=1` is now unconditional for **every** tool netns
  (`tool/netns.go:18-24`), so Batch B's privileged ports (53/67/69/123) need **zero new
  supervisor work**. Its `syslog` pack is the closest single-protocol structural analog for
  Batch B; `aaa` is the closest multi-protocol one.
- **Standing posture, unchanged and non-negotiable:** narrow/netns-scoped privilege only
  (never a broadened ambient capability or a host-wide sysctl), no Docker, no new runtime
  dependencies, one static Go binary per pack, keep it lightweight.

---

## 3. Facts established by reading the code (do not re-derive)

### 3.1 Node kinds and the two data planes

- `lab/lab.go:40-54` defines exactly four kinds: `KindIOL "iol"`, `KindVPCS "vpcs"`,
  `KindNAT "nat"`, `KindTool "tool"`. `labTypes.ts:6` mirrors them. `contracts/lab.schema.json:58-61`
  carries the `enum ["iol","vpcs","nat","tool"]` plus a long prose description of each.
- **There are two distinct data planes, not one:**
  - **Kernel-stack, netns + veth** — tool nodes. `tool/netns.go:29-41` (`netnsCreateVethCmds`)
    creates `vtool<N>` in the root ns and moves the peer into `iolt<N>`, renaming it to
    `tool.GuestIface` = `"eth1"` (`tool/tool.go:71`). The **root-side end stays in the root
    namespace on purpose** (`netns.go:72-74`) "so fabric capture and directional statistics
    can bind the same device as IOL, VPCS, and NAT taps".
  - **Userland-stack, UDP tunnel + shim tap** — VPCS. `argv.go:161-183` builds
    `vpcs -p <console> -i 1 -m <nodeID> -s <local> -c <remote> -t 127.0.0.1`;
    `argv.go:135-148` documents that "vpcs speaks the UDP tunnel protocol natively (it never
    speaks IOL netio), so its frames go to a per-VPCS udp<->tap shim (`internal/vtap`) whose
    tap joins the link's Linux bridge". `server/fabric_linux.go:601-640` (`setupVPCSFabric`)
    allocates the port pair, creates tap `iolvpc<N>` (`fabric.go`'s `vtapDevName`) and starts
    the shim before spawn.
- `node/spawn_linux.go:59-67` — `Spawn` switches on `spec.Kind` over exactly `"iol"` and
  `"vpcs"`; anything else is `"node: unknown kind %q"`. NAT and tool nodes never reach
  `Spawn` at all: `server/handlers.go:562-578` intercepts them in `startNodes` and routes to
  `startExtnetNode` / `startToolNode`.
- `node/spawn_linux.go:26-37` (the `Process` doc) is the authoritative statement of the two
  **console** models: IOL runs under a pty with a supervisor-bound telnet listener on
  `ConsolePort` fanned out by a `consoleHub`; VPCS **is its own telnet server**, daemonizes,
  and the supervisor binds nothing. For VPCS `ptmx`, `ln` and `hub` all stay nil, which is
  why `Process.Subscribe()` (`:483-491`) and `Process.RunExec` (`:499-507`) return
  nil/`ErrNoConsoleHub` for VPCS.
- `node/console_hub.go:290` — `newConsoleHub(pty io.ReadWriter, name string)`. **The hub
  takes an `io.ReadWriter`, not an `*os.File` and not a pty specifically.**
  `spawn_linux.go:470-475` (`NewProcessForTest`) already exercises that: it builds a working
  `Process` around an arbitrary `io.ReadWriter`. This is load-bearing for §5.3.

### 3.2 The tool endpoint machine (what a new node kind would otherwise have to duplicate)

`tool/endpoint_linux.go` owns, for one tool node: durable object records written *before*
each kernel object (`:283-292`), an ordered preclean (`:294-305`), a cgroup cage, the netns
+ veth, a 0700 socket dir and 0600 `options.json` owned by `ioltool` (`:520-580`), the
launch (`:257-281`), a readiness poll (`:332-361`), a liveness watchdog that tears the node
down (`:383-414`), an exit watcher (`:311-330`), and a teardown whose step order is the
exact reverse of `endpointSetupSteps() = {"cgroup","netns","veth"}` (`:589-591`), asserted by
`endpoint_test.go`. `endpointLaunchSpec` (`:257-281`) hands the child
`IOLBOX_TOOL_SOCK` / `IOLBOX_TOOL_OPTIONS` / `IOLBOX_PACK_DIR` / `IOLBOX_NODE_ID`, runs it as
`ioltool` with `AmbientCaps: ["NET_RAW"]` only.

`tool.Config` (`tool/tool.go`) carries `NodeID, Pack, Limits, Root, StateDir, RunDir, User,
InstanceID, Net *NetAddrConfig, Options []byte`. `tool.Pack` is
`{ID, Root string, Manifest, GUIBin string, Scripts map[string]string}` — **an in-memory
struct, not a filesystem lookup**, so a caller can construct one without a `pack.json` on
disk.

`tool/netns.go:18-24` — P4 Batch A landed: `netnsCreateNetnsCmds` now issues
`ip netns add iolt<N>` **and** `ip netns exec iolt<N> sysctl -w net.ipv4.ip_unprivileged_port_start=1`,
unconditionally, for every tool node.

### 3.3 The GUI proxy is already generic

`wsbridge/wsbridge.go:142-146` registers one `/tool/` handler; `:157-166` additionally
pre-screens `/tool` and `/tool/*` for traversal *before* `ServeMux` canonicalizes dot
segments. `wsbridge/proxy.go:32-88` (`handleTool`) is ~180 lines of generic reverse proxy
over `(socket, routes)`: AF_UNIX dialer, route-prefix allowlist (`matchingToolRoute`,
`:117-130`), WS-upgrade gating, `Forwarded`/`X-Forwarded-*` stripping, `iolbox_session`
cookie stripping (`:145-153`), `Location` rewriting, `frame-ancestors 'self'` injection, and
a full HTML tokenizer rewrite of `href`/`src`/`action`/`hx-*` attributes to the
`/tool/<id>/` prefix (`:185-216`), capped at 2 MiB.

The **only** node-specific input is `Server.ToolProxyTarget(nodeID)` (`server/toolproxy.go:13-42`),
which returns `(socketPath, []tool.ProxyRoute, ok)` and today rejects anything whose
`nr.tool == nil` or whose `config.pack` doesn't resolve.

### 3.4 The fabric — one wiring mode, not two

**The legacy UDP-relay wiring mode no longer exists.** Verified by grep, not assumed:

- `internal/relay` now exports only `Classify`/`ClassifyDetailed` (`classify.go:22,38`) and
  `PcapngWriter` (`pcapng.go:33`). There is no relay server, no `captureServer`, no
  `relay_linux.go`. Its remaining consumers are `internal/bcap` and `internal/dirstat`.
- `server/links.go` is down to one 6-line function, `isIOLMap`. **`wiringFor` is gone.**
- **`buildBridgePlan` / `rebuildBridgePlan` are gone**; `server/bridgeplan.go` now holds only
  `labBridge.close`, `netioDir`, `netioPathFor`, `realInstances`, `refreshFabric`.
- **Two documents are stale on exactly this point and must not be planned against:**
  `docs/bridge-fabric-retirement-map.md` (describes `wiringFor` and `buildBridgePlan` as
  live) **and `supervisor/README.md:199`**, whose "Native vs. bridged links" section still
  calls `wiringFor(link, isIOL, captureReady)` "the single decision point". Neither symbol
  exists in executable code. Correcting them is a separate, separately-reviewed doc change
  (§10) — flagged here so a reader who greps `wiringFor` and finds two hits does not conclude
  the relay path survives.
- `server/fabric.go:25-49` — `isFabricLink` returns true for any link with ≥2 endpoints all
  of whose node kinds are in `{IOL, NAT, VPCS, Tool}`; `fabricNodes` returns exactly that
  set. Its own doc comment states: "With mgmt retired, every fabric-capable node kind
  (IOL/NAT/VPCS/tool) is fabric, so every well-formed link is a fabric link."

**That doc comment is a statement about node *kinds*, and it is not the whole truth about
*realizability*.** `isFabricLink` never looks at the endpoint *interface*:

- `computeStaticTaps` (`fabric.go:79-131`) enumerates `netmap.IfacesForCounts(eth, 0)` —
  **zero serial groups**, and its own comment says so: "It enumerates ETHERNET interfaces
  only …; serial-interface taps are a later refinement."
- `lab/validate.go:124-128` validates an IOL endpoint through `netmap.ParseIface`, which
  **accepts `s0/0` / `Serial0/3` as readily as `e0/0`** (`netmap.go:136-179`).
- So a link on an IOL **serial** interface passes validation, passes `isFabricLink`, and then
  fails inside `attachFabricLink` at `fabric_linux.go:411-415` with
  `"fabric link %d: no static tap for node %d %s"`. **There is no legacy relay fallback to
  catch it** — the relay is gone (above).

**Consequences for Batch C, stated precisely:** there is exactly one wiring mode to support,
and its realizable domain is **fabric links every one of whose endpoints has a host device**
— i.e. IOL **Ethernet** endpoints (static tap), plus VPCS / NAT / tool / PC endpoints (all of
which have exactly one Ethernet-ish device by construction). An IOL serial endpoint is
**outside** that domain today. This is a pre-existing gap that Batch C neither creates nor
closes; §7.1 scopes around it explicitly rather than claiming coverage it does not have.

### 3.5 Per-endpoint host devices (the netem attachment surface)

`server/fabric_linux.go:701-726` — `(*Server).fabricLinkTapDevs(ll, l) []string` already
returns the root-namespace host device for **every** endpoint of a fabric link, in the
link's own endpoint order:

| endpoint kind | device | source |
|---|---|---|
| IOL | `iol<instance>_<flatIndex>` | `fabric.TapName` (`fabric/commands.go:109-115`) via `tapForEndpoint` |
| VPCS | `iolvpc<nodeID>` | `nr.vtapName`, from `vtapDevName` (`server/fabric.go`) |
| NAT | `iolnat<nodeID>` | hardcoded at `fabric_linux.go:714`, "matches extnet tapName" |
| tool | `vtool<nodeID>` | `tool.HostVethName` (`tool/tool.go:43`) |

**Read the switch carefully — the skipping is not uniform** (this matters for §7.2/§7.3):

- **VPCS** (`:709-712`) and **tool** (`:715-718`) are skipped when the node is not started
  (`nr.vtapName == ""` / `nr.tool == nil`).
- **IOL** (`:719-722`) is skipped when the endpoint has no static tap — which, per §3.4, is
  exactly the serial-interface case.
- **NAT** (`:713-714`) is **not** skipped: `"iolnat" + nodeID` is appended
  **unconditionally**, whether or not that device exists in the kernel. A stopped NAT
  endpoint therefore still contributes a name.

So the returned slice is **compacted**: `devs[i]` corresponds to `l.Endpoints[i]` only when
*no* endpoint was skipped, and the length check alone does not prove every returned name
names a live device. Two direct consequences for Batch C:

1. **Never index a returned slice positionally to identify an endpoint.** Batch C adds an
   endpoint-indexed sibling (§7.2):
   `fabricLinkEndpointDevs(ll, l) []endpointDev` where `endpointDev = {EndpointIndex int; Dev
   string}` — same switch, same skip rules, but each entry carries the index into
   `l.Endpoints` that produced it. `fabricLinkTapDevs` is refactored into a two-line wrapper
   over it so `fabricStats`, `openLinkDirstat`, `openLinkSlowTee` and
   `fabricLinkFullyAttached` keep byte-identical behaviour.
2. **Fault application must existence-filter.** A third helper,
   `fabricLinkFaultTargets(ll, l) []endpointDev`, is `fabricLinkEndpointDevs` filtered by
   `tapDeviceExists(dev)` — so a stopped NAT endpoint's phantom name never reaches `tc`.
   Only the fault path uses it; the existing consumers are untouched (changing what
   `fabricLinkFullyAttached` counts is out of scope and would alter attach behaviour).

`fabricLinkFullyAttached` (`:276-306`) compensates for the *skipping* case by treating
`len(devs) < len(l.Endpoints)` as a mismatch. Its existing consumers are `fabricStats`
(`:660-699`), `openLinkDirstat` (`:438-458`) and `openLinkSlowTee` (`:481-507`).

Bridge is `iolbr<linkID>` (`fabric/commands.go:120-126`). Bridge creation sets
`group_fwd_mask 0xfff8` and `disable_ipv6=1` (`commands.go:59-65`).

### 3.6 The privileged-command runner

`fabric/manager_linux.go` — `Manager` is stateless (`:39-42`). `runOne` (`:115-135`) execs
one argv under a 20s timeout, bare when `os.Geteuid()==0` and under `sudo -n` otherwise
(`sudoArgv`, `commands.go:98-104`), registering the pid with `tool.Registry.StartAndAdd` so
the subreaper cannot steal the wait. `runIdempotent` (`:93-108`) tolerates a benign first-command
failure per `op` (`isBenign`, `:141-157`: "file exists"/"device or resource busy" for
creates; "cannot find device"/"does not exist"/"no such device" for deletes and detach) and
unconditionally tolerates any `sysctl` step's failure (`:100-102`) because the IPv6-disable
hardening is not load-bearing.

`Manager` methods today: `EnsureTap`, `DeleteTap`, `EnsureBridge`, `DeleteBridge`, `Attach`
(`ip link set <tap> master <bridge>`), `Detach` (`ip link set <tap> nomaster`).

### 3.7 Link state, stats and the canvas

- `protocol/verbs.go:517-550` — `LinkStatsData{Link, FPS, BPS, Protos, ProtosDir,
  ProtosSubtypeDir}`. `ProtosDir` is `label -> [fps from endpoint 0, fps from endpoint 1]`,
  "where endpoint order matches the lab link's doc endpoints order" — the same ordering
  convention Batch C's `targetEndpoint` uses. Note `ProtosDir` is **fixed at two** entries,
  so it is the ordering rule Batch C borrows, **not** the addressing scheme: faults must
  address any of N endpoints (§7.2).
- `fabricStats` (`fabric_linux.go:660-699`) iterates **only `ll.fabricLinks`** (links
  actually attached) and derives frames/bytes from each endpoint tap's sysfs
  `statistics/rx_*` (`readTapCounters`, `:731-734`).
- `labStore.svelte.ts:100` holds `linkStats` as a plain `Record<number, …>`; `:119` notes
  link.stats is **silent for idle links** and the GUI decays glow on staleness; `:448-455`
  is the `"link.stats"` case; `:314`, `:500`, `:858` reset it.
- `CanvasInner.svelte:158-172` (`toFlowEdge`) builds edge `data` as
  `{linkId, capture, source, target}`. `:226-230` is the `$effect` that rebuilds edges — it
  explicitly `void`s `labStore.nodeStates` and `labStore.consolePorts` so those changes
  retrigger it.
- `FloatingEdge.svelte` (842 lines) reads `labStore.linkStats[linkId]` at `:52`, derives
  `glowing`/`glowIntensity` (`:58-77`), and composes the cable path's class string at
  `:296-299` from `is-capture` / `is-traffic` / `is-hot`, with a separate `.traffic-glow`
  underlay at `:283-292` and `.bestpath-glow` at `:319`. CSS for those lives at `:470-533`.
- Protocol verbs are registered in one table, `server/server.go:201-229`. Events are string
  constants in `protocol/message.go:63-69`. `docs/protocol.md` documents verbs under `##
  Verbs` and events under `## Events (server → GUI push)`.
- `mockTransport.ts:184-463` is a `switch (method)` covering every verb; an unhandled verb
  falls through. `:704-706` classifies a link as "bridged" (and therefore stats-worthy in
  the dev mock) when any endpoint kind is `vpcs`/`nat`/`tool`.

### 3.8 Packs, as shipped

- Five packs exist: `runtime/files/tools/packs/{aaa,httpclient,secbench,stub,syslog,webserver}`
  (`stub` is a test fixture). `aaa/gui/` now contains `tacacs.go`, `tacacs_wire.go` and their
  tests — **P4 Batch B landed**.
- Every non-secbench `pack.json` is byte-shape identical: `manifestVersion:1`, `id`, `name`,
  `icon`, `interpreter:"none"`, `gui:{bin, transport:"unix", console:"http",
  health:"/healthz", proxyRoutes:[{prefix:"/",allowWS:true}]}`, `caps:[]`, empty
  `options`/`groups`/`modules`, `limits:{memoryMax:268435456, pidsMax:64,
  cpuMax:"100000 100000", swapMax:0}`.
- **Icons in use:** `aaa`→`firewall`, `secbench`→`firewall` (**already collides** — icon keys
  are not unique and this is shipped precedent), `webserver`→`server`, `httpclient`→`cloud`,
  `syslog`→`tool`. Registry keys in `icons.svelte.ts:45-102`: `router, switch, l3-switch, pc,
  laptop, ap, nat, tool` plus `firewall, server, cloud`.
- `runtime/build-rootfs.sh` — pack integration is three list-driven loops, all now reading
  `for pack in aaa webserver httpclient syslog`: build at **`:177`**, dir reservation at
  **`:330`**, install at **`:361`**. (P4's plan cited `:176/:329/:360`; the file has since
  shifted by one line. Use the current numbers, and re-grep before editing.)
- `BASE_INCLUDE` (`build-rootfs.sh:239`) =
  `systemd,systemd-sysv,udev,dbus,iproute2,iputils-ping,libssl3,openssh-client,sudo,procps,iptables,tcpdump,util-linux,libcap2-bin,passwd`.
  **`iproute2` is present, so `tc` ships.** `sch_netem` is a *kernel module*, not a userspace
  package — see §7.8.
- `syslog/gui/main.go:13-46` is the canonical pack `main`: read `IOLBOX_TOOL_SOCK` +
  `IOLBOX_TOOL_OPTIONS` (hard-fail if either is empty) → load the store → start the lab-facing
  listener, **logging but not failing on error** → `hasLabIface()` warning → mkdir 0700, remove
  stale socket, `net.Listen("unix", …)`, `chmod 0600` → `http.Server{Handler: app.routes()}.Serve`.
- **Repo litter, do not touch:** `gui.exe` (~12 MB Windows artifacts) are checked in under
  `syslog/gui/`, `webserver/gui/`, `httpclient/gui/`. Inert (`build-rootfs.sh` builds with an
  explicit `-o`; `//go:embed` never sees them). P4 already flagged their removal as a
  separate change.

### 3.9 Every switch-on-`kind` site, verified by grep

The idea doc (§"Cost note") guesses at this list. The verified list is larger, and one of its
entries is wrong.

**Frontend (`app/src/lib/`):**

| file:line | what |
|---|---|
| `labTypes.ts:6` | the `NodeKind` union itself |
| `components/CanvasInner.svelte:41-49` | `nodeTypes` registry (`iol/vpcs/nat/tool` → components) |
| `components/CanvasInner.svelte:70` | `type: n.kind` on the flow node |
| `components/CanvasInner.svelte:80` | `packId` only for `kind === "tool"` |
| `components/CanvasInner.svelte:437-439` | auto-start on drop, `kind === "nat"` only |
| `components/CanvasInner.svelte:443-446` | `nameForKind` (NAT naming) |
| `components/CanvasInner.svelte:449-485` | `buildDroppedNode` — `iol` / `nat` / `tool` branches, `vpcs` fallback at `:484` (`name: \`PC${id}\``) |
| `components/Palette.svelte:213-227` | the VPCS palette chip (`onDragStart(e,"vpcs")`, glyph `iconSvg("pc",28)`, name "VPCS", sub "Virtual PC") |
| `components/Palette.svelte:660-662` | `.swatch.vpcs { color: var(--node-vpcs) }` |
| `components/InterfacePicker.svelte:90-91, 105-106` | fixed-interface rendering for `vpcs`/`nat`/`tool` |
| `interfaces.ts:12-13` | `allInterfaces` — `["eth0"]` for vpcs/nat, `["eth1"]` for tool |
| `interfaces.ts:47-48` | `nextFreeInterface` fallback |
| `icons.svelte.ts:212-217` | `defaultIconFor` — vpcs→`pc`, nat→`nat`, tool→`tool` |
| `components/LabBrowser.svelte:61-73` | `dotColor` thumbnail colour map |
| `components/Inspector.svelte:12-13` | `isIol` / `isTool` derivations |
| `components/Inspector.svelte:128` | `{node.kind.toUpperCase()}` chip |
| `components/Inspector.svelte:243-275` | the tool-only IP/prefix/gateway fields, `:275` "VPCS nodes take their config from canned commands" |
| `components/Console.svelte:118-120` | `isToolNode` — gates the telnet host:port chip |
| `nodes/VpcsNode.svelte:30, 40` | reads real kind from the doc (NAT reuses this component) |
| `nodes/NodeActions.svelte:24-26` | `isIol` — gates the "Save config" button |
| `clab.ts:33, 40, 177, 186-187, 200, 209` | containerlab import/export kind mapping |
| `mockTransport.ts:704-706` | dev-mock "bridged link" classification |

**Backend (`supervisor/internal/`):**

| file:line | what |
|---|---|
| `lab/lab.go:40-54` | the `Kind` constants |
| `lab/validate.go:50-80` | per-kind node validation; `default:` rejects unknown kinds |
| `lab/validate.go:124-147` | per-kind endpoint-interface validation + the single-endpoint cap for nat/tool |
| `node/spawn_linux.go:59-67` | `Spawn`'s kind switch |
| `node/argv.go:15` | `Spec.Kind` comment (`"iol" \| "vpcs"`) |
| `server/handlers.go:562-587` | `startNodes` dispatch: NAT → `startExtnetNode`, tool → `startToolNode`, VPCS → `setupVPCSFabric` then fall through |
| `server/handlers.go:786-808` | `buildSpec`'s kind switch |
| `server/handlers.go:115` | `lab.load` tool-pack known-pack check |
| `server/handlers.go:887` | `node.add` image binding, IOL only |
| `server/fabric.go:40-49` | `fabricNodes` |
| `server/fabric.go:99-102` | `computeStaticTaps` — IOL only |
| `server/fabric_linux.go:375-419` | `attachFabricLink`'s per-endpoint switch |
| `server/fabric_linux.go:565-587` | `detachFabricLink`'s per-endpoint switch |
| `server/fabric_linux.go:704-726` | `fabricLinkTapDevs`'s per-endpoint switch |
| `server/fabric_linux.go:519-532` | `linkIsIOLToIOL` (LACP tee gate) |
| `server/links.go:8-13` | `isIOLMap` |
| `server/loaded.go:129`, `nvram_linux.go:72`, `painter_collect_linux.go:33,55,240` | IOL-only guards (no change needed, listed for completeness) |
| `contracts/lab.schema.json:58-61` | the `kind` enum + description |

**Correction to the idea doc's guessed list:** it names
`supervisor/internal/netmap/netmap.go` as a site that "switches on node kind today". **It does
not** — `netmap` is pure interface/instance arithmetic and contains no `Kind` reference at
all. The IOL-only gating the doc is thinking of lives in `server/fabric.go:101` and
`server/links.go:11`. The doc also omits `Palette.svelte`, `Inspector.svelte`,
`Console.svelte`, `defaultIconFor`, all four `fabric*.go` switches, all five `handlers.go`
sites, `spawn_linux.go`'s `Spawn`, and `contracts/lab.schema.json`.

---

## 4. Where the idea doc's design does not survive the code — four corrections

These are stated up front because §5 is built on them. Each names the evidence.

### 4.1 `spawnPC` cannot be "modeled on `spawnVPCS`" for the data plane

The doc: *"Own backend spawn path `spawnPC` … modeled on `spawnVPCS` (binds `ConsolePort`
itself, serves the CLI directly)"*.

VPCS's network attachment is a **UDP frame tunnel into a userland TCP/IP stack**
(`argv.go:135-148`, `:161-183`; `vtap` shim at `fabric_linux.go:601-640`). VPCS implements
its own ARP/IP/ICMP in userspace; that is why `-s/-c/-t` work at all. A Go binary calling
`net.Dial`, `net.ListenPacket` or raw ICMP uses the **kernel** stack and cannot receive a UDP
tunnel. Reproducing VPCS's model for `netprobe` would mean writing a userspace TCP/IP stack —
categorically out of scope.

**Correction (decided):** PC's data plane is the **netns + veth** model (`tool/netns.go:18-41`),
identical to a tool node: `iolt<N>` namespace, guest `eth1`, root-side `vtool<N>` attached to
`iolbr<linkID>`. This is also the only model in which the doc's own
`net.ipv4.ping_group_range` requirement is coherent — a host-resident process has no
namespace to scope the sysctl to, and scoping it host-wide is exactly the broadening this
project forbids.

### 4.2 A hand-written `spawnPC` would duplicate the whole tool endpoint machine

§3.2 enumerates what `tool.Endpoint` already owns. A from-scratch `spawnPC` would need its
own cgroup cage, its own crash-recovery object records, its own preclean, its own readiness
and liveness watchers, and its own teardown-order invariant — every one of which has a test
today and none of which is PC-specific.

**Correction (decided):** `kind: "pc"` is first-class in the layers the idea doc actually
argues about — the document schema, validation, the palette, the node component, the
Inspector, the icon map — and the supervisor realises it through **`tool.Endpoint` with a
built-in, non-selectable pack**. Concretely:

- The `pc` pack is loaded by the **ordinary `tool.LoadPack`** and then held in a **private
  server field**, not in the selectable registry. **Do not hand-construct a `tool.Pack`
  literal.** `tool.Pack.GUIBin` is documented (`tool.go:204-213`) and used
  (`endpoint_linux.go:275` → `LaunchSpec.Binary`) as an **absolute, containment-checked**
  path; `LoadPack` produces `filepath.Join(packRoot, manifest.GUI.Bin)` after the `contained`
  check (`manifest.go:18-68`, asserted by `manifest_test.go:43-44`). A literal
  `GUIBin: "pc-gui"` would make the launcher resolve the name through `PATH` — `WorkDir`
  does **not** make a bare executable name resolve relative to the pack dir — which is both
  a bug and a privilege hazard. Concretely: `toolpacksLoad` loads every pack as it does
  today, then **moves** the one whose `ID == "pc"` out of `s.toolPacks` into a new private
  `s.pcPack tool.Pack` (+ `s.pcPackOK bool`), so it is never returned by
  `handleToolListPacks` and `s.toolPack("pc")` never resolves (§5.4).
- The node doc carries **no `config.pack`**. `validate.go`'s `KindPC` case must *not* require
  it (§5.4).
- Everything the doc asked for is preserved: own palette entry, own component, own doc
  fields, no manifest indirection visible to the user, no options-schema.

This is not a retreat from "first-class node kind"; it is where the first-classness has to
live to be observable, versus where reuse costs nothing.

### 4.3 `/pc/{id}/` is the wrong route

§3.3: `handleTool` is already fully generic over `(socket, routes)`, and carries the
traversal pre-screen, session-cookie stripping, WS allowlist, `Location` rewrite,
`frame-ancestors` CSP and the 180-line HTML attribute rewriter. A parallel `/pc/{id}/`
handler would either duplicate all of it or silently omit security controls.

**Correction (decided):** PC's GUI is served at **`/tool/{id}/`**, unchanged. The only
change is inside `Server.ToolProxyTarget` (`toolproxy.go:13-42`): accept a node whose
`Kind == KindPC`, skip the `config.pack` unmarshal, and return the built-in pack's
`proxyRoutes` (`[{prefix:"/", allowWS:true}]`). `PcNode.svelte`'s iframe `src` stays
`` `/tool/${nodeId}/` ``, exactly like `ToolNode.svelte:88`. Zero new wsbridge routes, zero
new security surface.

### 4.4 A netns-confined process cannot serve `ConsolePort` over TCP

If `netprobe` binds `Spec.ConsolePort` itself (the VPCS convention) it binds it **inside
`iolt<N>`**. `wsbridge.Config.DialConsole` defaults to `net.DialTimeout("tcp",
"127.0.0.1:<port>")` in the **root** namespace (`wsbridge.go:129-133`), and native telnet
from the GUI host dials `<vm-ip>:<port>`, also root-namespace. Neither can reach it. (The
`ConsoleBind` field exists precisely to make the *root-namespace* listener reachable —
`argv.go:28-32` notes VPCS ignores it because vpcs binds its own console on all interfaces,
which it can do only because vpcs runs in the root namespace.)

**Correction (decided):** the CLI is served on a **second AF_UNIX socket**, and the
supervisor binds `ConsolePort` in the root namespace and bridges. AF_UNIX sockets are
namespaced by the **mount/filesystem**, not the network namespace — which is exactly why the
existing tool GUI socket already works across the netns boundary. See §5.3 for the
mechanism, which is strictly better than VPCS's: it reuses `consoleHub` and therefore gets
multi-client fan-out, telnet negotiation, the zero-TCP-hop `ConsoleSubscribe` webconsole
path, and `RunExec` — none of which VPCS nodes have today.

---

## 5. Batch A — the PC node (`netprobe`)

### 5.1 Decisions locked (from the idea doc; not re-litigated)

1. New first-class `NodeKind: "pc"`. Own palette entry beside IOL/VPCS/NAT, own
   `PcNode.svelte`, own doc fields. Never routed through `kind:"tool", pack:"netprobe"`.
2. **The UI never says "netprobe".** Palette chip label = **"PC"**, sub-text "Virtual PC —
   addressing, ping/traceroute, DNS, TCP/UDP tools." Default node name `PC{id}` — which
   `CanvasInner.svelte:484` already produces for VPCS today, so the identity is unchanged;
   only the chip label at `Palette.svelte:224` catches up. `id` vs `name` split follows
   `secbench/pack.json`'s precedent.
3. **Ships alongside VPCS.** The VPCS palette entry, `spawnVPCS`, `VPCSArgv`, `vtap` and
   `setupVPCSFabric` are **not touched**. Demoting or removing VPCS is a later, separate
   decision.
4. VPCS-parity CLI: `ip <addr>/<prefix> <gateway>`, `ip dhcp`, `show ip`, `ping`, `trace`,
   `save`, `reset`, and **`?` / `help` as a real grouped command legend** — the doc is
   explicit that this is the in-CLI discoverability mechanism, not an afterthought. Plus
   netprobe-only: `dns <name>`, `tcp connect|listen <port>`, `udp send|listen <port>`,
   `flow start <dst> <rate> <size>`, `arp show`.
5. Web GUI panel for the richer views (ARP table, DHCP lease detail with options, DNS answer
   detail, flow generator, TCP/UDP echo log). **Both surfaces read and write the same
   in-process state** — no mode switch, no reconciliation.
6. Raw ICMP via a **netns-scoped** `net.ipv4.ping_group_range`, never a broadened ambient
   capability.

### 5.2 Process and data-plane model — decided

PC nodes are realised by `tool.Endpoint` (§4.2) with a built-in pack (§4.2), giving:

- netns `iolt<N>`, guest `eth1`, root-side `vtool<N>` — so `fabricNodes`,
  `attachFabricLink`, `detachFabricLink`, `fabricLinkTapDevs`, `dirstat` and bridge capture
  all work by adding `lab.KindPC` next to `lab.KindTool` in four `switch` arms
  (`fabric.go:44`, `fabric_linux.go:399`, `:578`, `:715`), with no new device naming.
- cgroup limits, options file, readiness/liveness, crash-recovery records — free.
- `AmbientCaps: ["NET_RAW"]` — already what `endpointLaunchSpec:277` grants every pack, and
  what `secbench` relies on. **Do not add capabilities.**

**Raw ICMP — decided, and it is *not* a new capability.** `NET_RAW` is already ambient, so
`ping` would work via `SOCK_RAW` today. Use **`SOCK_DGRAM`/`IPPROTO_ICMP` (unprivileged ping)
instead**, enabled by extending `netnsCreateNetnsCmds` (`tool/netns.go:18-24`) with a second
netns-scoped sysctl, exactly parallel to the one P4 added:

```go
{name: "ip", args: NetnsExecArgs(nodeID, []string{
    "sysctl", "-w", "net.ipv4.ping_group_range=0 2147483647"})[1:]},
```

Rationale, decided: (a) `ping_group_range` is per-netns (`net->ipv4.ping_group_range`), so
the blast radius is one namespace whose only non-`lo` interface is `eth1` — the same
containment argument P4 §2.5 made, and the same live proof obligation (§9.1); (b) an
unprivileged ICMP datagram socket kernel-manages the ID/sequence and cannot be used to forge
arbitrary IP headers, so it is *narrower* than the `NET_RAW` path already available; (c)
folding it into `netnsCreateNetnsCmds` means zero edits to `endpoint_linux.go`, zero new stub
in `netns_other.go`, and structural impossibility of a caller forgetting it — the identical
argument P4 §2.2 made and sol-medium accepted. **`endpointSetupSteps()` must stay
`{"cgroup","netns","veth"}`** (a sysctl has no independent lifetime); P4 §2.3 and its test
requirement apply verbatim.

Note the ordering constraint: `traceroute` over unprivileged ICMP sockets requires setting
`IP_TTL` per probe and reading the ICMP time-exceeded reply, which the kernel *does* deliver
to the matching datagram socket. `trace` must not silently fall back to a TCP or UDP
traceroute without saying so in its own output.

**Interface name — decided: `eth1`, not `eth0`.** `netnsCreateVethCmds` renames the moved
peer to `tool.GuestIface` = `"eth1"` (`netns.go:36`, `tool.go:71`), and
`netnsAddrCmds`/`netnsAttachVethCmds` are written against that constant. Making PC use
`eth0` means parameterising four command builders and their tests for a cosmetic difference.
The cost is that a PC node's interface reads `eth1` where a VPCS node reads `eth0`
(`interfaces.ts:12-13`); the Inspector hint text must say so plainly rather than papering
over it. **Flagged for sol-medium as the one decision here worth a second opinion** — if the
reviewer judges `eth0` parity more valuable than the command-builder fork, say so and the
resolution is to thread a `guestIface` field through `tool.Config` rather than to introduce a
second netns package.

### 5.3 Console transport — decided

`netprobe` serves its line-oriented CLI on a **second AF_UNIX socket**, path
`<socketDir>/cli.sock`, mode 0600, owned by `ioltool`, alongside the GUI socket
`IOLBOX_TOOL_SOCK` already handed to it by `endpointLaunchSpec:264`. A new env var
`IOLBOX_PC_CLI_SOCK` carries the path.

#### 5.3a How `IOLBOX_PC_CLI_SOCK` is actually delivered (decided — do not hand-wave it)

`endpointLaunchSpec` (`endpoint_linux.go:257-281`) builds a **fixed** environment from a
five-name host allowlist plus four `IOLBOX_*` values derived from `tool.Config`, and
`tool.Config` (`tool.go:267-283`) has no extra-environment and no CLI-socket field. Naming
the variable is therefore not enough; the plumbing is part of Batch A:

- **`tool/tool.go`** — a new path helper beside `OptionsFile` (`:285-291`), same shape and
  same doc-comment conventions:

  ```go
  // CLISocketFile returns the exact per-node PC CLI socket path under the tool
  // socket directory. The pack creates and owns it (ioltool:ioltool, 0600) inside
  // the 0700 socket directory; the supervisor dials it to bridge the console.
  func CLISocketFile(runRoot string, nodeID int) string {
      return filepath.Join(SocketDir(runRoot, nodeID), "cli.sock")
  }
  ```

- **`tool.Config`** — one narrowly typed field, **not** a general env map:
  `CLISocket bool // when true the launch env carries IOLBOX_PC_CLI_SOCK (PC nodes only)`.
  A general `Env map[string]string` is explicitly rejected: it would let any future caller
  inject arbitrary variables past `ScrubbedEnvAllowlist`, which is the exact escape hatch
  that allowlist exists to prevent.
- **`endpointLaunchSpec`** — appends `"IOLBOX_PC_CLI_SOCK="+CLISocketFile(cfg.RunDir,
  cfg.NodeID)` **only when `cfg.CLISocket`**, and bumps the slice pre-allocation from 9 to
  10. `ScrubbedEnvAllowlist` (`tool.go:220-230`) gains `"IOLBOX_PC_CLI_SOCK"`; any test that
  asserts the launch env is a subset of that allowlist keeps passing, and a test asserting
  the variable is **absent** for a plain tool node is required (§5.6).
- **`Endpoint`** — a `CLISocketPath() string` accessor beside `SocketPath()` (`:254-255`),
  returning the same `CLISocketFile(...)` value. `startPCNode` calls **that accessor**, never
  a locally re-joined path, so the child and the supervisor cannot drift.
- The endpoint does **not** create the socket (the pack does, at startup, exactly as it
  removes and re-creates the GUI socket); the endpoint's existing `os.RemoveAll` of the
  socket dir at teardown (`:467-471`) already cleans it up, so `endpointSetupSteps()` is
  untouched.

The supervisor, after `tool.Endpoint` reports ready:

1. Dials the CLI socket (`net.Dial("unix", path)`) — this succeeds across the netns because
   AF_UNIX is filesystem-scoped.
2. Wraps that `net.Conn` in `newConsoleHub(conn, node.Name)` (`console_hub.go:290` takes
   `io.ReadWriter`; `NewProcessForTest`, `spawn_linux.go:470-475`, already proves a non-pty
   `io.ReadWriter` works).
3. Binds `ConsolePort` on `s.cfg.ConsoleBind` in the **root** namespace and runs the existing
   `serveConsole` accept loop (`spawn_linux.go:100-103`, `:400-414`).

What this buys over VPCS's own-telnet-server model, for free: concurrent webconsole + native
telnet (the exact starvation bug `consoleHub` was written to fix), one shared
`telnet.Negotiator` per node, `Process.Subscribe()` for the zero-TCP-hop webconsole path
(`wsbridge.go:107-112` documents the VPCS TCP dial as "the one intentionally surviving
external-telnet-dial path in the whole supervisor" — PC does not add a second), and
`RunExec`.

**Structural consequence:** this needs a `*node.Process`-shaped object that owns a hub + a
listener but no `cmd`. `node.Process`'s fields (`spawn_linux.go:38-50`) already permit
`cmd == nil` — `NewProcessForTest` constructs exactly that — but `wait()` (`:424-434`)
dereferences `p.cmd` unconditionally. The implementing agent must add a constructor
(`node.NewConsoleBridge(conn io.ReadWriter, name string, ln net.Listener) *Process` or
equivalent) that starts `serveConsole` and **not** `wait`, and make `teardown()` the only
lifecycle exit. Do not "fix" `wait()` to nil-check; give the PC path its own constructor so
the IOL/VPCS reaping semantics are untouched.

**Reconnection:** if the CLI socket dies (pack crashed), the hub's `readLoop` sees EOF and
`shutdown()`s, disconnecting clients — correct. The endpoint's liveness watcher
(`endpoint_linux.go:383-414`) independently tears the node down. **Do not add a reconnect
loop**; a dead pack is a dead node.

### 5.4 Concrete file-level changes

**New — the pack binary** `runtime/files/tools/packs/pc/gui/` (module
`iolbox/tools/packs/pc/gui`, `go 1.22`), structurally copied from `syslog/gui/`:
`main.go`, `config.go`, `server.go`, `util.go`, `env.go`, `templates/{layout,dashboard,
arp,dhcp,dns,flow,sockets}.html`, `static/{pico.min.css,htmx.min.js}` (copied from
`aaa/gui/static/`), plus the netprobe-specific `cli.go`, `cli_help.go`, `state.go`,
`ping.go`, `trace.go`, `dnsq.go`, `sockets.go`, `flow.go`, `arp.go` and their tests.
`main.go` mirrors `syslog/gui/main.go:13-46` exactly, with **one addition**: it opens the CLI
AF_UNIX listener before the GUI one, and a CLI-listener failure is fatal (unlike a lab-facing
protocol listener, the CLI *is* the node's primary surface).

**A `pack.json` is written for `pc`, and it is load-bearing** — both so `build-rootfs.sh`'s
existing install loop needs no special case **and** so `tool.LoadPack` can produce the
absolute, containment-checked `GUIBin` the launcher requires (§4.2). `server.toolpacksLoad`
(`toolpacks.go:54-60`) must then **move** the `id == "pc"` entry out of `s.toolPacks` into
the private `s.pcPack` / `s.pcPackOK` fields — not merely filter it in the handler — so that:
`handleToolListPacks` cannot list it, `s.toolPack("pc")` cannot resolve it from
`startToolNode`, and `startPCNode` still gets a fully validated `tool.Pack`. If no `pc` pack
loaded (`!s.pcPackOK`), `startPCNode` fails with a clear `CodeUnsupported` error naming the
missing pack directory rather than launching something from `PATH`.

**`runtime/build-rootfs.sh`** — append `pc` to the three lists at `:177`, `:330`, `:361`
(re-grep first; §3.8).

**Schema and types:**
- `contracts/lab.schema.json:58-61` — add `"pc"` to the enum; extend the description with
  PC's single interface `eth1`, at-most-one-endpoint rule, and its `config` fields.
- `lab/lab.go` — `KindPC Kind = "pc"` with a doc comment.
- `labTypes.ts:6` — `"iol" | "vpcs" | "nat" | "tool" | "pc"`.
- **Doc fields.** PC reuses the existing `Config map[string]json.RawMessage`
  (`lab.go:74-76`) rather than adding top-level `Node` fields — the doc's "own lab-JSON
  shape" is satisfied by `config.net` (already the tool convention, decoded at
  `handlers.go:730-746` into `tool.NetAddrConfig`) plus a new `config.pc` blob for
  `{dhcp: bool, savedCommands: string[]}`. **Decided over new top-level fields** because
  every existing consumer (`lab.Node` decode, `labStore` round-trip, `clab.ts` export,
  seedlabs) already preserves `config` opaquely, and adding `Node.IP`/`Node.Gateway`
  top-level would need matching schema, TS, Go, and round-trip changes for zero benefit.
  **How `save` actually reaches `config.pc` is specified in §5.4a** — it is not implied by
  any machinery that exists today.

#### 5.4a `save` — the state-sync channel, specified end to end

**The problem, stated honestly.** `tool.Endpoint` is a one-way pipe: it writes
`options.json` *before* launch (`endpoint_linux.go:124`, `endpointWriteOptions`) and after
that the supervisor never reads the pack's state back. The pack's sockets serve **inbound**
connections. And `lab.saveDoc` does not serialise `ll.doc` at all — it stores the **client's**
document text verbatim (`protocol/verbs.go:109-122`: "The supervisor stores it verbatim and
does not parse it beyond extracting the id"). So a naive "the CLI persists via the
supervisor" has *two* missing legs, not one: pack → supervisor, and supervisor → the document
that actually gets saved. Both are designed here.

**Ownership.** The **pack** owns live PC state. The **supervisor** owns `ll.doc` and is the
only writer of it. The **GUI** owns durable persistence, because it is the GUI's document text
that `lab.saveDoc` writes.

**Leg 1 — pack → durable file (no new privilege, existing precedent).** `save` snapshots the
current addressing plus a replayable command list and writes it to the pack's **own**
`IOLBOX_TOOL_OPTIONS` file using the atomic write-temp-then-`os.Rename` `Store.Save` pattern
that every pack already ships (`syslog/gui/config.go:65-82`, tested by
`TestStoreAtomicSave`). The written object is `{"pc": {"dhcp": bool, "savedCommands":
[...]}, "rev": <monotonic int>}`. This is a *within-node-lifetime* durability step only: the
socket dir is removed at endpoint teardown (`endpoint_linux.go:467-471`), which is why leg 2
exists.

**Leg 2 — supervisor pulls, over the socket it already has.** The pack serves
`GET /_iolbox/state` on its **existing GUI AF_UNIX socket** returning that same JSON. The
supervisor reads it with an `http.Client` whose `DialContext` is
`net.Dial("unix", ep.SocketPath())` — the exact AF_UNIX-HTTP idiom already used in-repo by
`aaa/gui/radius_test.go`'s healthz test. Properties that make this safe without inventing an
authenticated control plane:

- **Direction.** The supervisor is the client. There is no inbound control verb the pack can
  invoke, so a compromised pack cannot *push* anything.
- **Identity is structural, not asserted.** The socket path is derived by the supervisor from
  the node id it is already holding (`Endpoint.SocketPath()`); any `nodeId` in the response
  body is **ignored**. One pack cannot write another node's state.
- **Blast radius.** Only `config.pc` is writable this way. `config.net`, `config.pack`, node
  name, position and every other doc field are untouched by the merge.
- **Reachability.** `/_iolbox/state` is GET-only, read-only, and returns exactly what the
  pack's own dashboard already renders, so the fact that the `{prefix:"/"}` proxy route also
  exposes it to the browser is harmless. **It must not accept POST/PUT** — assert that in a
  test.
- **Validation** (strict, in the supervisor, before the merge): response capped at 64 KiB and
  2 s; `json.Decoder` with `DisallowUnknownFields` into a typed
  `lab.PCState{DHCP bool; SavedCommands []string}`; ≤ 64 commands; each ≤ 200 bytes and
  printable ASCII only; anything failing → the pull is discarded with a logged warning and
  the previous doc value is kept.
- **Synchronization.** Single-writer by construction: the supervisor writes `options.json`
  only *before* launch, the pack owns it *after* launch, and the supervisor's merge into
  `ll.doc.Nodes[i].Config["pc"]` happens under the existing `ll.mu`. No lock is shared with
  the pack and no file is written by both sides while the node runs.

**Leg 3 — supervisor → GUI → `lab.saveDoc`.** A new verb **`pc.syncState`**
(`server.go:201-229` table) takes `{labId, node?}` — omitted `node` means every running PC
node in the lab — performs the pull, merges, and returns the merged state. It also emits a
new event **`node.pcState`** (`protocol/message.go:63-69`,
`PCStateData{Node int, State *PCState, Stale bool}`), which `labStore` handles by writing
`node.config.pc` into its in-memory doc. **The GUI's save path calls `pc.syncState` for the
lab and awaits it before issuing `lab.saveDoc`**, so the saved text carries the state. The
supervisor additionally pulls at `stopNode` (before teardown removes the socket dir) so a
stop-then-save still persists, and at `lab.stop`.

**Failure is never fatal.** A node that is stopped, whose socket is gone, or that answers
non-200 yields `stale:true` and the last known doc value. Saving a lab must never fail
because a PC node is down.

**`save`'s own output says what happened**, in one line, because the two-step nature is
otherwise invisible: `Saved to this node. Use Lab > Save to store it in the lab file.`

**Validation** (`lab/validate.go`):
- `:50-80` — add `case KindPC:` with **no `config.pack` requirement** (§4.2). Update the
  `default:` message at `:79` to list `pc`. Update the function's own doc comment (`:10-26`).
- `:124-147` — add `case KindPC:` requiring `ep.Interface == "eth1"` and enforcing the
  at-most-one-endpoint rule via the existing `extEndpoints` counter, byte-identical to the
  `KindTool` arm at `:138-146`.

**Supervisor node lifecycle:**
- `handlers.go:562-587` — add a `docNode.Kind == lab.KindPC` branch calling a new
  `startPCNode`, placed **before** the VPCS branch. `startPCNode` is `startToolNode`
  (`:684-772`) with the pack resolved from the built-in constant instead of `config.pack`,
  plus the §5.3 console bring-up and a `s.emit(protocol.EventNodeConsole, …)`.
- `handlers.go:875-893` (`node.add`) — PC needs a console port; the existing
  `s.consolePorts.Next()` at `:876` is unconditional, so **no change**. Verify this rather
  than assume it.
- `stopNode` (`handlers.go:~840`) — the `nr.tool != nil` branch must **first** run the
  §5.4a state pull (while the socket still exists), then tear down the PC's console (hub
  shutdown + listener close) and release nothing else. `node.remove` (`:898-960`) already
  calls `stopNode` then `consolePorts.Release`.
- `toolproxy.go:13-42` — accept `KindPC` (§4.3), returning `s.pcPack`'s `proxyRoutes`.
- `server.go:201-229` — register `pc.syncState`; `protocol/message.go:63-69` — add
  `EventNodePCState = "node.pcState"`; `docs/protocol.md` — document both (§5.4a).

**Fabric** — four one-line switch-arm additions, each `case lab.KindTool:` → `case
lab.KindTool, lab.KindPC:`: `fabric.go:44`, `fabric_linux.go:399`, `:578`, `:715`. Nothing
else in the fabric changes: PC's root-side device is `tool.HostVethName(nodeID)` = `vtool<N>`,
already what `fabricLinkTapDevs:717` returns.

**Frontend:**
- New `app/src/lib/nodes/PcNode.svelte` — `VpcsNode.svelte`'s face/`NodeActions`/connector/
  LED/name chrome (`:47-82`) merged with `ToolNode.svelte`'s `gui-button` (`:66-72`) and
  `tool-panel` iframe overlay (`:82-90`, `src={`/tool/${nodeId}/`}`,
  `sandbox="allow-scripts allow-forms allow-same-origin"`). **Both buttons on one face**, as
  the doc specifies. `NodeActions` already renders Console for any running node
  (`NodeActions.svelte:62-65`), so the console button needs no change.
- `CanvasInner.svelte:41-49` — `pc: PcNode` in `nodeTypes`.
- `CanvasInner.svelte:449-485` — a `if (kind === "pc")` branch in `buildDroppedNode`
  returning `{id, kind, name: \`PC${id}\`, x, y}`. **Do not touch the `vpcs` fallback at
  `:484`** — VPCS keeps its behaviour.
- `Palette.svelte` — a new chip beside the VPCS one (`:213-227` is the template), label
  "PC", sub "Virtual PC", glyph `iconSvg("pc", 28)`, `onDragStart(e, "pc")`, plus a
  `.swatch.pc` rule mirroring `:660-662`. **Both chips render**; the VPCS chip's label
  stays "VPCS" so the two are distinguishable during the alongside period.
- `interfaces.ts:12-13` — `if (node.kind === "tool" || node.kind === "pc") return ["eth1"]`;
  `:47-48` fallback likewise.
- `icons.svelte.ts:212-217` — `if (kind === "pc") return "pc"`. **Note the collision with
  VPCS**, which also maps to `"pc"`. Decided: accept it — the two node types *are* the same
  thing to a learner, and icon keys are already non-unique (`aaa` and `secbench` both use
  `firewall`).
- `InterfacePicker.svelte:90-91, 105-106` — add `pc` to the fixed-interface condition, and
  extend the ternary to render `eth1` for `pc` as well as `tool`.
- `LabBrowser.svelte:61-73` — `case "pc": return "var(--node-vpcs)"`.
- `Inspector.svelte` — a new `isPc` derivation; PC shows the IP/prefix/gateway fields
  (`:243-272`) and the Save button (`:277-283`), with hint text naming `eth1` and stating
  that the console CLI's `ip`/`ip dhcp` and these fields write the same state. The
  `:275` "VPCS nodes take their config from canned commands" branch is untouched.
- `clab.ts` — `pc` exports as `linux` (`:200`) and its interface maps like `vpcs`
  (`:40`, `:177`); on import, `kindOf` (`:33`) keeps returning `"vpcs"` for
  linux/host/alpine/client so **existing containerlab imports are unaffected** — this is a
  deliberate non-change, stated so it isn't "fixed" later by accident.
- `mockTransport.ts:704-706` — add `"pc"` to the bridged-kinds list so dev-mock link glow
  works on PC links.
- `Console.svelte:118-120` — **no change**; `isToolNode` tests `=== "tool"`, and PC *does*
  have a console port, so the telnet chip should render.

### 5.5 CLI grammar — implement exactly this

One line-oriented dispatcher over the AF_UNIX CLI socket. Prompt `PC> ` (VPCS uses `VPCS> `;
the difference is intentional and the node name is in the terminal title via the hub's
existing escape). CRLF and bare LF both terminate a line (`consoleHub`'s
`normalizeNVTLineEndings`, `console_hub.go:642`, already handles the NVT side).

| command | grammar | behaviour |
|---|---|---|
| `?`, `help` | `? [<command>]` | **Required.** Bare `?` prints the full legend, grouped `Addressing / Diagnostics / Services / Config`, one line per command with a one-line description. `? ping` prints that command's own usage. Backed by a static table keyed off the dispatcher so a command cannot exist without a help entry — assert that in a test. |
| `ip` | `ip <addr>/<prefix> [<gateway>]` | Static IPv4 on `eth1`. Replaces any existing address. `<gateway>` optional; when present installs a default route. Rejects a gateway outside the configured prefix with a clear message. |
| `ip dhcp` | `ip dhcp [-r]` | DHCPv4 client on `eth1`. `-r` releases. Retains the full lease (server id, options 1/3/6/51/54, and any 66/150) for `show ip` and the GUI's lease detail. |
| `show ip` | `show ip` | Address/prefix/gateway/MAC/MTU, DHCP state and lease remaining, resolvers. VPCS prints a similar block; match its field set, not its exact formatting. |
| `ping` | `ping <host> [-c <n>] [-i <ms>] [-s <bytes>] [-t <ttl>]` | Default `-c 5`. `<host>` may be a name (resolved via the configured resolvers) or a literal. Per-probe line + a summary line with min/avg/max and loss %. |
| `trace` | `trace <host> [-m <maxttl>] [-q <probes>]` | ICMP, `-m` default 30, `-q` default 3. Must state in its own output which probe method it used. |
| `save` | `save` | Writes the current addressing + a replayable command list into the pack's own options file (leg 1 of §5.4a) and bumps `rev`. It reaches `config.pc.savedCommands` in the lab document via the supervisor's `pc.syncState` pull, **not** by any write from inside the netns. Prints `Saved to this node. Use Lab > Save to store it in the lab file.` |
| `reset` | `reset` | Clears addressing, routes, DHCP lease, ARP cache view, and stops any listeners/flows. Does **not** clear saved config. |
| `dns` | `dns <name> [A\|AAAA\|CNAME\|PTR] [@<server>]` | Query type defaults to `A`. Prints the answer section with TTLs; the full answer (authority/additional, rcode, latency) goes to the GUI panel. |
| `tcp` | `tcp connect <host> <port> [-m <msg>]` \| `tcp listen <port> [-e]` \| `tcp close <port>` | `listen -e` = echo server. Connect reports the handshake result and RTT. |
| `udp` | `udp send <host> <port> <msg>` \| `udp listen <port> [-e]` \| `udp close <port>` | Same shape as `tcp`. |
| `flow` | `flow start <dst> <pps> <bytes> [-p udp\|tcp] [-d <port>]` \| `flow stop [<id>]` \| `flow show` | Bounded generator. **Hard caps enforced in code, not config**: ≤ 10 000 pps and ≤ 1500 bytes per flow, ≤ 4 concurrent flows. A lab tool that can saturate the appliance is a footgun. |
| `arp` | `arp show` \| `arp clear` | Reads the netns neighbour table. |

Unknown command → `% Unknown command "<tok>". Type ? for a list.` — Cisco-flavoured, matching
what a learner sees elsewhere in the lab. Never a Go stack trace, never a bare "error".

### 5.6 Testing bar

The bar is P4 §3.7's: **a test must check the code against an independent statement of the
truth, not against itself.**

1. **CLI parser table test** — every command in §5.5, each with at least one valid and one
   malformed invocation, asserting the exact rendered output line for the malformed case.
2. **Help-table completeness test** — iterate the dispatcher's command map and assert every
   entry has a help line and a group; assert the reverse (no orphan help entries). This is
   what stops `?` silently rotting as commands are added.
3. **State-sharing test** — apply `ip 10.0.0.1/24 10.0.0.254` through the CLI handler, then
   render the GUI dashboard handler and assert the same address appears; then the reverse,
   through the GUI's form handler and out through `show ip`. The doc's "both surfaces read
   and write the same state" is otherwise untested prose.
4. **DHCP client codec test** with hand-written `[]byte` literals for DISCOVER and REQUEST,
   and a decode test for a canned OFFER carrying options 1/3/6/51/54/66/150 — asserting each
   option surfaces in the lease struct. Do not test the client against this repo's own
   server; that proves nothing about interop.
5. **Flow-cap test** — assert the pps/size/concurrency caps reject rather than clamp
   silently.
6. **Console-bridge test** (supervisor side, `internal/node`): construct the PC `Process`
   over an `io.Pipe`, attach two clients, write a line, assert **both** receive it — the
   regression test for the starvation bug `consoleHub` exists to prevent.
7. **Netns command-sequence test** (`tool/netns_test.go`): extend `TestNetnsCreateSequence`'s
   `want` with the `ping_group_range` cmdSpec as the **third** element (after `netns add` and
   the P4 `ip_unprivileged_port_start` sysctl), and add the namespace-prefix assertion
   `args[0]=="netns" && args[1]=="exec" && args[2]==NetnsName(n) && args[3]=="sysctl"`. Do
   **not** weaken the existing eth1-escape assertion loop. Separately assert
   `endpointSetupSteps()` still equals exactly `[]string{"cgroup","netns","veth"}` — P4 §2.6
   step 4 established that the teardown-is-reverse test does not prove this on its own.
8. **`validate_test.go`** — a `pc` node with `config.pack` absent validates; a `pc` endpoint
   on `eth0` is rejected; two link endpoints on one `pc` node are rejected.
9. **`save` round-trip test, both legs (§5.4a).** Pack side: run `save` through the CLI
   handler, assert the options file contains the expected `{"pc":{…},"rev":N}` after an
   atomic rename, and assert `GET /_iolbox/state` over a `net.Listen("unix", …)` test socket
   returns it — and that `POST /_iolbox/state` is rejected. Supervisor side: stand up a fake
   AF_UNIX server returning a canned body and assert the merge lands in
   `ll.doc.Nodes[i].Config["pc"]` and **only** there (`config.net` and the node name
   unchanged); assert an over-length / unknown-field / non-ASCII body is discarded and the
   previous value survives; assert an unreachable socket yields `stale:true` and no error.
10. **Built-in-pack test (§4.2/§5.4).** After `toolpacksLoad`, assert `s.toolPack("pc")`
    returns false, `handleToolListPacks`'s result contains no `"pc"`, and `s.pcPack.GUIBin`
    is an **absolute** path under `s.pcPack.Root` (the regression test for a bare-name
    `GUIBin` searching `PATH`).
11. **Launch-env test (§5.3a).** `endpointLaunchSpec` with `CLISocket: true` contains
    `IOLBOX_PC_CLI_SOCK=` + `tool.CLISocketFile(runDir, nodeID)`; with `CLISocket: false`
    (every existing tool pack) the variable is **absent**; and the full env remains a subset
    of `ScrubbedEnvAllowlist`.
12. **Frontend `npm run check`** must be clean; the `NodeKind` union widening will surface
   every unhandled switch the grep in §3.9 missed. **Treat a new type error as a found site,
   not an annoyance.**

### 5.7 Batch A acceptance gate (local, implementing agent)

1. `cd supervisor && go build ./... && go vet ./... && go test ./...` — green.
2. `cd runtime/files/tools/packs/pc/gui && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
3. `cd app && npm run check` — green, with zero `// @ts-expect-error` or `as any` added to
   silence a kind switch.
4. All twelve §5.6 categories present and passing.
5. Grep proof in the PR description: `AmbientCaps`, `AllowedCaps`, `launchSetprivArgv`,
   `spawnVPCS`, `VPCSArgv`, `internal/vtap` and `setupVPCSFabric` are **untouched** (zero
   changed lines).
6. Grep proof that `handleToolListPacks`'s output cannot contain `"pc"` — i.e. the pack is
   moved into `s.pcPack` inside `toolpacksLoad`, and a test asserts `s.toolPack("pc")`
   returns `false`. Grep proof that **no `tool.Pack{…}` composite literal** is constructed
   outside `tool.LoadPack` (§4.2's `GUIBin` trap).
7. Staging-dir test of the three `build-rootfs.sh` edits: `pc/pack.json` + `pc-gui` land
   under `.../packs/pc/`, mode 0755, ELF linux/amd64, and `gui.bin` matches the installed
   filename.

---

## 6. Batch B — `netsvc` (DHCP + DNS + NTP + TFTP)

### 6.1 Placement — a normal `tool` pack, decided

Unlike PC, `netsvc` **is** a genuinely swappable, occasional pack: a lab either needs branch
infrastructure services or it does not, and it appears once per lab, not dozens of times.
That is exactly the case the manifest-driven pack system exists for. It gets a `pack.json`,
appears in the Inspector's pack `<select>` automatically, and needs **zero** palette,
`CanvasInner`, `labTypes`, schema, or supervisor changes — the "Network tools" chip
(`Palette.svelte:229-241`) already covers it.

Four services in **one** pack, not four packs — the opposite of P4 §4.1's syslog call, and
for a reason that is specific rather than contradictory: DHCP option 6 hands out a DNS
server, option 42 an NTP server, and options 66/150 a TFTP server. The pedagogical point is
that **one box hands a client the addresses of the other services and the client then uses
them** — split across four nodes with four `eth1` addresses, the learner configures the
cross-references by hand and the lesson evaporates. `aaa` (RADIUS + TACACS+ under one
dashboard) is the shipped precedent for a multi-protocol pack.

### 6.2 Privileged ports — already solved, no work required

`tool/netns.go:18-24` sets `net.ipv4.ip_unprivileged_port_start=1` in **every** tool netns,
unconditionally (P4 Batch A). DNS/53, DHCP/67, TFTP/69 and NTP/123 therefore bind as
`ioltool` with **no supervisor change, no manifest `caps` entry, and no capability grant**.
`pack.json` keeps `"caps": []`. State this in the pack's package doc so a future reader does
not "add" a capability that is not needed.

### 6.3 DHCP: clean-room reimplementation inside the pack — decided, with reasons

The repo already has a DHCP server: `supervisor/internal/extnet/dhcp.go` — a clean, pure
codec (`DecodePacket:100-148`, `Encode:154-184`, `Handle:220-238`) plus a round-robin
`leaser` (`:263-306`), unit-tested and shipped.

**It cannot be shared, and should not be copied wholesale:**

1. **Module boundary, hard.** It lives in `supervisor/internal/extnet` — an *internal*
   package of the supervisor module. Every pack is its own Go module
   (`runtime/files/tools/packs/*/gui/go.mod`). A pack literally cannot import it, and the
   fixes for that (vendoring, a new shared module, promoting `extnet` out of `internal/`) all
   cost more than the ~200 lines in question and would couple pack release to supervisor
   release.
2. **It encodes NAT policy in the codec.** `Encode` unconditionally emits option 3 (router) =
   `serverIP` (`:176`), option 6 = the package-level `dnsServers` var `[1.1.1.1, 8.8.8.8]`
   (`:77-80`, `:180`), and a fixed 1-hour lease (`:70`, `:172-174`). Those are correct for a
   NAT gateway and wrong for a configurable lab DHCP server.
3. **It parses only two options.** `DecodePacket`'s option loop (`:135-145`) extracts option
   53 and option 50 and discards everything else — including option 55 (parameter request
   list) and option 82 (relay agent information), which are *precisely* what §6.5 exists to
   display. `giaddr` is decoded (`:113`) but never surfaced.

**Decision: clean-room reimplementation inside `netsvc/gui/dhcp.go`, deliberately modeled on
`extnet/dhcp.go`'s *structure*** — pure codec + `Handle` that maps request-type to reply-type
so policy is testable without sockets + a separate `_linux.go` for the socket. Keep the same
constant naming so the two are diffable by a reviewer. **State in the file's package doc that
`extnet/dhcp.go` is the sibling implementation and why it was not reused**, so the
duplication is a recorded decision rather than an accident someone later "consolidates".

### 6.4 Wire formats — implement exactly this

#### DHCPv4 (RFC 2131 / RFC 2132), server

Header identical to `extnet/dhcp.go` (240-byte minimum, magic `0x63825363` at 236). Beyond
what that file does, `netsvc` must:

- **Parse and retain** option 55 (parameter request list — the raw code sequence, in order),
  option 82 (relay agent information: sub-option 1 `circuit-id`, sub-option 2 `remote-id`,
  each a nested TLV inside option 82's value), option 61 (client identifier), option 12
  (hostname), option 60 (vendor class). Retain unknown options as `(code, raw)` pairs — never
  silently drop, per the syslog pack's `Raw`-always-retained precedent.
- **Emit, configurably**: 1 (mask), 3 (router), 6 (DNS, multi-address one option — see
  `appendIPsOpt`, `extnet/dhcp.go:196-206`), 42 (NTP servers), 51/58/59 (lease/T1/T2), 54
  (server id), **66 (TFTP server *name*, a string)** and **150 (TFTP server *address list*,
  4 bytes per address — a Cisco extension, not in RFC 2132)**. 66 and 150 are different types
  and both are commonly configured on IOS; emitting an IP into option 66 as raw bytes is a
  classic bug. Assert both encodings against hand-written literals.
- **`giaddr` handling.** When `giaddr != 0` the client is behind a relay: the reply goes to
  `giaddr:67` (unicast), not to the broadcast address, and the pool must be selected by
  `giaddr`'s subnet. `extnet/dhcp.go` echoes `giaddr` (`:164`) but always replies on the tap.
  A relay-aware reply path is required here — it is the whole point of showing option 82.
- **Static reservations** by MAC, checked before the pool.
- **Lease table** with MAC, IP, hostname, requested-options list, relay info, bind time,
  expiry. In-memory only, matching every other pack.

#### DNS (RFC 1035), authoritative-only

12-byte header: `ID(2) | flags(2) | QDCOUNT(2) | ANCOUNT(2) | NSCOUNT(2) | ARCOUNT(2)`, all
big-endian. Flags: `QR(1) Opcode(4) AA(1) TC(1) RD(1) RA(1) Z(3) RCODE(4)` — **`Z` is 3 bits,
not 4**; a 4-bit `Z` shifts every RCODE and is the single most likely bug here.

- QNAME: length-prefixed labels terminated by a zero byte. **Compression pointers** are a
  two-byte value whose top two bits are `11` (`0xC0`); the low 14 bits are an offset from the
  start of the *message*. A parser must (a) cap total decompression jumps (16 is the
  conventional limit) so a self-referential pointer cannot loop forever, and (b) never
  follow a pointer that points forward. Test both.
- Answer RR: `NAME | TYPE(2) | CLASS(2) | TTL(4, signed per RFC but treat as uint32) |
  RDLENGTH(2) | RDATA`. Types: `A=1`, `NS=2`, `CNAME=5`, `PTR=12`, `AAAA=28`.
- **CNAME chasing:** when the queried name is a CNAME and the zone also holds the target's A
  record, return **both** RRs in the answer section (CNAME first, then A) and set `AA`. A
  learner needs to see the chain.
- PTR zone: `<reversed-octets>.in-addr.arpa`. Auto-derive PTRs from the A records rather than
  making the user type them twice — and say so in the GUI.
- Non-authoritative name → `RCODE=3` (NXDOMAIN) with `AA` set. No recursion, ever:
  `RD` in the query is echoed, `RA` is always 0. State that in the GUI so "why doesn't
  google.com resolve" answers itself.
- UDP/53 only. A response over 512 bytes sets `TC=1` and truncates the answer section.
  **TCP/53 fallback is out of scope** (§10).

#### NTP (RFC 5905), server, mode 4 only

48-byte packet, big-endian throughout. **This is the layout most likely to be got wrong by
copy-pasting field order:**

| offset | size | field |
|---|---|---|
| 0 | 1 | `LI(2) \| VN(3) \| Mode(3)` — LI in the **high** 2 bits, Mode in the **low** 3 |
| 1 | 1 | Stratum |
| 2 | 1 | Poll (signed log2 seconds) |
| 3 | 1 | Precision (signed log2 seconds) |
| 4 | 4 | Root Delay (16.16 fixed point) |
| 8 | 4 | Root Dispersion (16.16 fixed point) |
| 12 | 4 | Reference ID (4 ASCII chars for stratum 1, e.g. `LOCL`; the ref server's IPv4 for stratum ≥2) |
| 16 | 8 | Reference Timestamp |
| 24 | 8 | Originate Timestamp — **the client's transmit timestamp, echoed verbatim** |
| 32 | 8 | Receive Timestamp — when we received the request |
| 40 | 8 | Transmit Timestamp — when we sent the reply |

- Timestamps are 64-bit: **32 bits of seconds since 1900-01-01 + 32 bits of binary fraction**.
  The Unix→NTP offset is **2208988800**. A test must encode a known `time.Time` and compare
  against a hand-computed literal — a wrong offset or a seconds/fraction swap produces a
  packet a client accepts and then steps its clock by 70 years.
- **The Originate field is the #1 interop trap:** it must be the bytes from the client's
  *Transmit* field (offset 40 of the request), copied without reinterpretation. Getting it
  from our own clock makes every client compute a nonsense offset and reject the server.
- Reply mode = 4 (server), VN echoed from the request, LI = 0, Stratum configurable
  (default 3), Reference ID `LOCL` when stratum is 1.
- Ignore anything whose Mode is not 3 (client). Do not implement symmetric, broadcast, or
  control/private modes (`Mode 6`/`7` are a known amplification surface — reject explicitly
  and log).

#### TFTP (RFC 1350), sandboxed

- Opcodes: `RRQ=1`, `WRQ=2`, `DATA=3`, `ACK=4`, `ERROR=5`. RRQ/WRQ body is
  `filename\0mode\0` (mode is `netascii` | `octet` | `mail`, case-insensitive). DATA is
  `opcode(2) | block(2) | data(0..512)`. ACK is `opcode(2) | block(2)`. ERROR is
  `opcode(2) | code(2) | message\0`.
- **A DATA packet shorter than 512 bytes terminates the transfer.** A file whose length is an
  exact multiple of 512 therefore requires a final **zero-length** DATA packet. Omitting it
  makes exactly the transfers a learner is most likely to try (a padded config) hang. Test a
  512-byte and a 1024-byte file explicitly.
- Block numbers wrap 65535→0 (or →1, depending on implementation); for a lab, cap the served
  file size at 32 MiB and reject beyond, rather than picking a wrap convention.
- The first server reply comes from an **ephemeral port**, not 69, and the client's TID is
  the source port of its request. Serving the whole transfer from 69 works against some
  clients and not IOS. Get this right.
- **Sandbox, non-negotiable:** a fixed in-memory file map plus an upload area under the
  pack's own writable dir. Reject any filename containing `/`, `\`, `..`, a leading `~`, or a
  NUL; reject symlinks; cap uploads. `mode mail` → ERROR code 4.

### 6.5 GUI — wire-level visibility is the product

Routes mirror every other pack: `GET /healthz`, `GET /{$}`, `GET /settings` +
`POST /settings/save`, `GET /frag/<panel>` for polled fragments. One dashboard with four
service tiles (bind status + counters each), and a tab per service:

- **DHCP** — lease table (MAC, IP, hostname, bound, expires) plus, per lease, an expandable
  **"what the client asked for vs. what we sent"** view: the option-55 request list beside the
  options actually emitted, `giaddr`, and option 82's circuit-id/remote-id decoded. This is
  the single highest-value panel in the pack.
- **DNS** — query log with name, type, source, rcode, latency, and the chosen answer(s)
  including the CNAME chain. A "why NXDOMAIN" hint when the query fell outside the zone.
- **NTP** — per-client rows with the client's transmit timestamp, our receive/transmit
  timestamps, and the resulting offset/delay as the *client* would compute them.
- **TFTP** — transfer log: filename, mode, direction, block count, bytes, outcome, and the
  ephemeral TID used.

Follow the established htmx idiom exactly: 3s polling via `hx-trigger="every 3s"` with
`hx-include` on any filter controls so filters survive the refresh (P4 §4.6), and the
duplicated-`{{define}}` hazard applies — a rows block that appears in both the dashboard and
the polled fragment **must be kept byte-identical** or the two silently diverge.

**Bind-failure visibility, load-bearing** (P4 §3.5's rule, restated because it applies to
four listeners here): a failed bind records a mutex-guarded error string, banners it on the
dashboard naming the port, keeps the GUI up, and **still returns 200 from `/healthz`**. A
listener failure is degradation, not a crash — exiting would make the liveness watchdog
(`endpoint_linux.go:383-414`) tear the node down and hide the cause. Test the whole chain:
bind a port first, assert the goroutine sets the error field, render the dashboard, assert
the banner text, and separately assert `/healthz` is 200.

**Settings that change a bound port must actually rebind**, following `webserver/gui/web.go`'s
`Restart` pattern; on failure the **old** listener stays up rather than leaving the service
dead (P4 §4.3, sol-medium finding #10).

### 6.6 Concrete file-level changes

- New `runtime/files/tools/packs/netsvc/pack.json` — the standard shape (§3.8) with
  `id:"netsvc"`, `name:"Network Services"`, `gui.bin:"netsvc-gui"`, `caps:[]`, standard
  limits. **Icon: `"server"`** — shared with `webserver`. Icon keys are already non-unique
  (`aaa`/`secbench` both `firewall`); a dedicated glyph is optional polish (§10).
- New `runtime/files/tools/packs/netsvc/gui/` — `go.mod` (`module iolbox/tools/packs/netsvc/gui`,
  `go 1.22`), `main.go`/`config.go`/`server.go`/`util.go`/`env.go` structurally from
  `syslog/gui/`, plus `dhcp.go`, `dhcp_linux.go`, `dns.go`, `ntp.go`, `tftp.go` and tests,
  `templates/`, and `static/` copied from `aaa/gui/static/`.
- `runtime/build-rootfs.sh:177`, `:330`, `:361` — append `netsvc` to all three lists.
  Re-grep the line numbers first. **No `BASE_INCLUDE` change.**
- `app/src/lib/components/Palette.svelte:236` — the tooltip string currently reads
  `"Network tools — pick RADIUS/AAA, web server, HTTP client, or syslog collector after
  dropping"`. Append the services node. **String-only**; the `{#each}`, `defaultToolPack`,
  `onDragStart` and `buildDroppedNode` are untouched and the pack appears in the Inspector
  automatically.

### 6.7 Testing bar

1. `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
2. **Hand-written `[]byte` literals** for: a DHCP OFFER carrying options 1/3/6/42/51/54/66/150
   (asserting 66 is a *string* and 150 is a 4-byte-per-address list); a DHCP REQUEST carrying
   options 55 and 82 with both sub-options, asserting both are extracted; a DNS response with
   a CNAME→A chain **using a compression pointer**, asserting the pointer offset byte-for-byte;
   an NTP mode-4 reply asserting the Originate field equals the request's Transmit bytes and
   that a known `time.Time` encodes to the expected 64-bit value; TFTP DATA/ACK/ERROR frames.
3. **DNS pointer-loop test** — a message whose compression pointer points at itself must be
   rejected, not hang.
4. **TFTP boundary test** — a 512-byte file transfers as one DATA + one zero-length DATA; a
   1024-byte file as two + one; a 511-byte file as one.
5. **TFTP sandbox test** — `../../etc/passwd`, `/etc/passwd`, `a/b`, a NUL-embedded name, and
   a symlink all rejected with ERROR code 2 (access violation) or 1 (not found), never served.
6. **NTP mode filter test** — mode 6 and mode 7 requests produce no reply and one log line.
7. **DHCP relay test** — a request with non-zero `giaddr` selects the matching pool and the
   reply is addressed to `giaddr`, not broadcast.
8. Bind-failure chain test per §6.5 (all four listeners).
9. `healthz`-over-AF_UNIX test, copied from `aaa/gui/radius_test.go`.
10. Staging-dir test of the three `build-rootfs.sh` edits (as §5.7 step 7, for `netsvc`).

---

## 7. Batch C — per-link fault injection

### 7.1 Scope decision: one wiring mode, and its exact realizable domain

The task framing anticipated a fabric-vs-legacy-relay split. §3.4 establishes by grep that
**the legacy UDP-relay wiring mode has been removed**: `wiringFor`, `buildBridgePlan` and the
relay server no longer exist. **Two documents are stale on this point** —
`docs/bridge-fabric-retirement-map.md` and `supervisor/README.md:199` — and both are named in
§3.4 so a future reader's grep does not resurrect the relay. Correcting them is a separate doc
change (§10); Batch C must **not** implement a second wiring mode.

So Batch C targets the static-tap Linux-bridge fabric — the only wiring mode there is. **Its
realizable domain is narrower than `isFabricLink`'s kind test, and Batch C is scoped to the
narrower set, not the wider one:**

- **In scope — "Ethernet-realizable" fabric links:** every endpoint has a host-side device.
  That is IOL **Ethernet** endpoints (static tap from `computeStaticTaps`), plus VPCS
  (`iolvpc<n>`), NAT (`iolnat<n>`), tool and PC (`vtool<n>`) endpoints. IOL↔IOL, IOL↔VPCS,
  IOL↔NAT, IOL↔tool, IOL↔PC and N-endpoint segments are all in scope. **No fault type needs a
  second implementation** across that set.
- **Out of scope — IOL serial endpoints.** `computeStaticTaps` enumerates
  `IfacesForCounts(eth, 0)`, so an `s0/0` endpoint has **no tap**, and `attachFabricLink`
  already fails such a link with `"no static tap"` (§3.4). There is no relay fallback to
  impair instead. Batch C therefore does **not** claim to cover it and must not pretend to:

  - A helper `linkFaultSupported(ll, l) (bool, string)` returns false, with a reason naming
    the offending node + interface, when any endpoint of the link has no static tap
    (`tapForEndpoint` miss on an IOL endpoint). It is the single place this rule lives.
  - `link.setFault` rejects an unsupported link with `protocol.CodeUnsupported` and that
    reason string — **before** touching `ll.linkFaults`, `tc`, or the doc.
  - The canvas disables the **Faults** submenu for such a link and shows the same reason as
    the disabled item's `title`, so the user is told why rather than getting a silent no-op.
  - Adding serial static-tap support (which would fix realizability *and* faults in one move)
    is named as out of scope in §10 — it is a fabric change, not a fault change.

This scoping is asserted from the code in §3.4, not assumed, and it is deliberately stated as
a **subset** claim: the earlier "100% of links" framing was wrong.

### 7.2 Attachment point — decided, and the wrong answer named

**`tc … root netem` goes on each endpoint's own host-side device, never on the bridge.**

- A Linux bridge forwards frames **port to port inside the bridge module**. Those frames
  never traverse the `iolbr<N>` netdev's own root qdisc — that qdisc only sees frames the
  *host stack* transmits out of the bridge interface. `tc qdisc add dev iolbr7 root netem
  delay 100ms` therefore **succeeds, reports nothing, and impairs nothing** for IOL↔IOL
  traffic. This is the trap; flagged for sol-medium.
- The correct devices are exactly the ones `fabricLinkTapDevs` (`fabric_linux.go:701-726`,
  §3.5) already names: `iol<inst>_<idx>` (IOL), `iolvpc<n>` (VPCS), `iolnat<n>` (NAT),
  `vtool<n>` (tool/PC). **Reuse that switch** — it is the single place the per-endpoint
  device mapping lives, and `dirstat`/`slowtee`/`fabricStats` all key off it.
- **But do not reuse its return type.** `fabricLinkTapDevs` returns a **compacted** slice: a
  stopped VPCS/tool endpoint and a tapless (serial) IOL endpoint are skipped, so `devs[0]` is
  **not** necessarily `l.Endpoints[0]`. Any targeting scheme that indexes the returned slice
  silently impairs the wrong endpoint the moment one node is stopped. Batch C therefore adds
  (§3.5):

  ```go
  type endpointDev struct {
      EndpointIndex int    // index into l.Endpoints — the ONLY endpoint identity
      Dev           string // root-namespace host device
  }
  func (s *Server) fabricLinkEndpointDevs(ll *loadedLab, l *lab.Link) []endpointDev
  func (s *Server) fabricLinkFaultTargets(ll *loadedLab, l *lab.Link) []endpointDev // + tapDeviceExists filter
  ```

  and refactors `fabricLinkTapDevs` into a wrapper over `fabricLinkEndpointDevs` so every
  existing consumer keeps byte-identical behaviour. **Every fault operation selects devices
  by `EndpointIndex`, never by slice position.**
- **Direction semantics, stated explicitly because they are counter-intuitive.** A root qdisc
  on a device shapes that device's **egress**. For a tap or the root end of a veth, "egress"
  means *frames the host is delivering to the node behind it*. So:
  - netem on endpoint **A**'s device impairs traffic **arriving at A**;
  - a symmetric impairment ("this cable has 50 ms of latency") requires the qdisc on **both**
    endpoint devices, each with half the configured one-way delay (or the full value if the
    UI's field is labelled one-way — pick one and label the UI accordingly; **decided:
    the UI field is one-way delay, applied identically to both endpoints, and the tooltip
    says round-trip is 2×**);
  - a **targeted** impairment applies to exactly one endpoint's device, named by its **index
    into the link's doc endpoint list** (`LinkFault.targetEndpoint`, §7.5). That is the same
    ordering `LinkStatsData.ProtosDir`'s `[ep0, ep1]` convention uses (`verbs.go:530-540`),
    so the GUI has one ordering rule, not two — but unlike `ProtosDir` it is **not limited to
    two endpoints**.
  - For an N-endpoint **segment** link, per-endpoint egress is still correct and is in fact
    more expressive: `targetEndpoint: 2` on a 3-endpoint segment impairs traffic arriving at
    that one member, modelling a single bad drop cable. This is why the old
    `direction: 0|1|2` encoding was dropped — it could not name endpoint 2 at all, and
    contradicted this section and live gate §9.3.13.
  - **Absent `targetEndpoint` means all endpoints** (the symmetric case above).
    `targetEndpoint` must be an in-range index into `l.Endpoints`; out of range is a
    validation error naming the field and the link's endpoint count. Note that a targeted
    endpoint whose device is currently absent (node stopped) is **not** an error: the fault
    is recorded and applied when that device appears (§7.4).
- **Ingress is out of scope.** Impairing ingress needs `ifb` + `tc mirred` redirection, a new
  device class and a new teardown obligation, for a capability the egress model already
  covers from the other endpoint's side. Named, not silently dropped (§10).

### 7.3 The `tc` command set — implement exactly this

All fault parameters live in **one** `netem` qdisc per device, replaced atomically:

```
tc qdisc replace dev <dev> root netem \
    [delay <D>ms [<J>ms [<corr>%]] [distribution normal]] \
    [loss <L>%] \
    [duplicate <U>%] \
    [reorder <R>% <corr>%] \
    [rate <B>kbit]
```

Clear:

```
tc qdisc del dev <dev> root
```

Decisions and the traps behind them:

- **`replace`, not `add`.** `add` fails with `RTNETLINK answers: File exists` when a qdisc is
  already present, which is the normal case when the user edits an existing impairment.
  `replace` is idempotent and atomic.
- **`rate` inside `netem`, not a separate `tbf`.** `netem` has carried `rate` since iproute2
  3.8 / kernel 3.3. A `tbf`-on-top composition needs a parent/child handle scheme, an
  ordering decision (shape-then-delay vs delay-then-shape), and two teardown steps. One qdisc
  is the right answer for a lab.
- **`reorder` requires a non-zero `delay`.** `tc` rejects `reorder` without it. The validator
  must reject the combination in the API with a clear message rather than letting `tc` fail
  opaquely.
- **`distribution normal` requires the delay's jitter argument** and reads
  `/usr/lib/tc/normal.dist` from the **iproute2 data files**, which a slim rootfs may not
  ship. **Decided: omit `distribution` in v1** and use uniform jitter. If the live gate shows
  `/usr/lib/tc/*.dist` present, adding it is a follow-up. Do not discover this on the
  appliance.
- **Percentages** are validated 0–100 with at most 2 decimals; `delay` 0–10 000 ms; `rate`
  1 kbit–10 gbit. Reject out-of-range in the handler, not in `tc`.

**`internal/fabric` additions** (`commands.go` + `manager_linux.go`), reusing the existing
`runOne`/`sudoArgv` machinery verbatim:

- `netemCmds(dev string, f Netem) [][]string` and `netemClearCmds(dev string) [][]string` in
  `commands.go` — **pure data, no I/O**, so they are unit-testable on Windows exactly like
  `tapCreateCmds`/`sudoArgv` are today.
- `opNetemSet` / `opNetemClear` added to the `op` enum (`manager_linux.go:26-33`).
- `isBenign` (`:141-157`) gains a `case opNetemClear:` covering **both** classes of
  "nothing to do", because netem clearing runs during detach and teardown where either can
  legitimately happen:

  - **no qdisc on the device** — `"no such file or directory"`, `"cannot delete qdisc with
    handle of zero"`, `"invalid handle"`;
  - **no device at all** — `"cannot find device"`, `"does not exist"`, `"no such device"`.

  The second group is **required, not defensive**: §7.4 clears netem inside
  `detachFabricLink` and `teardownFabric`, and those run after a crash, after a partial
  startup, and after an endpoint's own teardown has already deleted its veth/tap. Treating a
  missing device as a failure there would make teardown non-idempotent — the exact bug class
  `isBenign` exists to prevent. Note this deliberately makes `opNetemClear`'s benign set a
  **superset** of `opDeleteTap`'s; that is a considered decision, not an accidental reuse, so
  write the arm out in full rather than falling through to the delete arm.

  **What stays fatal for `opNetemClear`:** permission failures (`"operation not permitted"`,
  `"sudo: a password is required"`) and malformed-command failures
  (`"what is \"…\"?"`, `"unknown qdisc"`, usage output). A clear that fails because we cannot
  run `tc` at all must be loud — silently swallowing it would leave a live impairment on a
  link the user believes is clean.
- `opNetemSet` gets **no** benign arm: `replace` is already idempotent, and a `SetNetem`
  against a device that does not exist is a real bug in the caller (which is why the fault
  path selects devices through the existence-filtered `fabricLinkFaultTargets`, §3.5).
- `(*Manager).SetNetem(ctx, dev, f)` / `ClearNetem(ctx, dev)`. `ClearNetem`'s doc comment
  must state plainly that an absent device is a successful, idempotent no-op.

### 7.4 Admin down/up — decided, and the bookkeeping trap it hits

**Admin down = `ip link set <dev> nomaster`; admin up = `ip link set <dev> master <bridge>`** —
i.e. the **existing** `Manager.Detach` / `Manager.Attach` (`manager_linux.go:72-79`), applied
to that link's endpoint devices.

Rejected alternative: `ip link set <dev> down`. On an IOL static tap that `iouyap` holds
open, and on the root end of a veth whose peer is in a live netns, down/up changes carrier
state in ways the pumps and the guest see differently; `nomaster` is the exact seam the
fabric already hot-plugs on, is already proven idempotent, and leaves every device up so
`dirstat`'s and `bcap`'s bound fds stay valid.

**The trap — and it will bite silently.** `startFabric` (`fabric_linux.go:227-231`) runs on
**every** `node.start` and re-attaches any link that is not `fabricLinkFullyAttached`:

```go
if !s.fabricLinkFullyAttached(ll, l) {
    if err := s.attachFabricLink(ll, l); err != nil { return err }
}
```

and `fabricLinkFullyAttached` (`:276-306`) returns false precisely when an endpoint's
`master` symlink is missing — which is exactly what admin-down creates. So starting *any*
node in the lab would silently restore an administratively-disabled link.

**Required fix, decided:** thread the link's fault state into that decision — **without
corrupting the predicate.** Add to `loadedLab` a `linkFaults map[int]activeFault` (guarded by
the existing `ll.mu`, alongside `fabricLinks`/`captures`/`dirstats`), where `activeFault`
holds the `LinkFault` plus its `active bool` marker and any pending `*time.Timer`.

**`fabricLinkFullyAttached` stays exactly what it is: a pure kernel-state predicate.** It
must **not** learn about faults and must **not** short-circuit to `true` for a down link.
Its three obligations (bookkeeping agrees; the bridge device exists; every endpoint that has
a device is a member of it — `:259-306`) are the only thing that proves the link is really
wired, and returning `true` from fault state alone would skip `EnsureBridge`, skip the
initial wiring of a lab whose persisted fault is `down` + `initial:true` (leaving a link with
**no bridge at all**), and skip the late-endpoint reconciliation that
`attachFabricForNode` depends on.

Instead, **branch before the predicate**, in both places that heal a link:

```go
// startFabric (fabric_linux.go:227-231) and attachFabricForNode (:537-...)
if f, ok := ll.activeFault(l.ID); ok && f.Down {
    if err := s.reconcileFabricLinkDown(ll, l, f); err != nil { return err }
} else if !s.fabricLinkFullyAttached(ll, l) {
    if err := s.attachFabricLink(ll, l); err != nil { return err }
}
```

**`reconcileFabricLinkDown(ll, l, f)` — the explicit down-state realization path.** It is
idempotent and does the full job, never a subset of it:

1. `mgr.EnsureBridge(ctx, iolbr<linkID>)` — the bridge exists even while the link is down, so
   an initially-down link is fully realized and admin-**up** is a pure attach.
2. `devs := s.fabricLinkFaultTargets(ll, l)` — every endpoint device that currently exists,
   endpoint-indexed (§3.5). A stopped endpoint simply is not in the set yet; it is handled
   when it starts, because `attachFabricForNode` runs this same function.
3. For each entry: **detach** it if it is in the down target set
   (`f.TargetEndpoint == nil || *f.TargetEndpoint == e.EndpointIndex`), **attach** it to the
   bridge otherwise — so `down` + `targetEndpoint` unplugs exactly one member of a segment
   while the rest keep forwarding.
4. Record the link as realized (`ll.fabricLinks[l.ID] = true`) and refresh
   `openLinkDirstat`/`openLinkSlowTee` exactly as `attachFabricLink` does, so stats and
   capture bookkeeping are not silently skipped for a down link.
5. Call `reconcileLinkFault` (below) so a fault carrying both `down` (one endpoint) and an
   impairment (the others) ends up consistent.

**And the second half of the same trap (finding #4): an active impairment must be reapplied
when a device appears later.** Applying `delay 50ms` to a link while its VPCS/tool/PC
endpoint is stopped shapes only the devices present at that moment; when the node starts,
`attachFabricLink` attaches its new device and the fault silently becomes **asymmetric** —
green build, wrong behaviour, invisible in the GUI. So:

- **`reconcileLinkFault(ll, l)`** is the single fault-application function. It computes
  `fabricLinkFaultTargets`, then for **every** present device: `SetNetem` with the complete
  active fault if that endpoint is targeted, `ClearNetem` if it is not. It is idempotent
  (`tc qdisc replace`) and safe to call repeatedly.
- It is called from **every** path that can change which devices exist or which are wired:
  the end of `attachFabricLink`, the end of `reconcileFabricLinkDown`, `attachFabricForNode`
  (i.e. after every `node.start`), and the `link.setFault` handler. "Attach then forget" is
  the failure mode; there is no attach path that may skip it.
- `detachFabricLink` (`:556-594`) clears netem on the endpoint devices **before** detaching,
  and cancels any scheduled-fault timer.
- `teardownFabric` (`:770-822`) clears netem on every device it is about to delete, and
  cancels every timer. Both rely on `ClearNetem` tolerating an absent device (§7.3).

Tests that must exist (§7.9): set a link admin-down → `node.start` another node in the same
lab → the link is **still** down; and set a symmetric fault while one endpoint node is
**stopped** → start that node → **both** devices carry the netem.

### 7.5 Fault model and lab JSON

```ts
// labTypes.ts, alongside LabLink.capture
export interface LinkFault {
  /** Administratively disabled (cable unplugged). Mutually exclusive with impairment. */
  down?: boolean;
  /** One-way delay, ms. Round-trip is 2x (applied to both endpoints). */
  delayMs?: number;
  /** One-way jitter, ms. Requires delayMs > 0. */
  jitterMs?: number;
  /** Packet loss, percent 0-100. */
  lossPct?: number;
  /** Rate limit, kbit/s. */
  rateKbit?: number;
  /** Duplication, percent. Advanced. */
  duplicatePct?: number;
  /** Reordering, percent. Advanced. Requires delayMs > 0. */
  reorderPct?: number;
  /** Which endpoint this fault applies to, as an INDEX into the link's own
   *  endpoints array (the same ordering LinkStatsData.ProtosDir uses).
   *  ABSENT = every endpoint (the symmetric default). A qdisc on endpoint i's
   *  device impairs traffic ARRIVING AT endpoint i (§7.2). Extensible to
   *  N-endpoint segments by construction; the earlier `direction: 0|1|2`
   *  encoding was removed because it could not name a third endpoint. */
  targetEndpoint?: number;
  /** When true, this fault is applied at lab.load/lab.start. Default (absent/false)
   *  means the definition is REMEMBERED but INACTIVE on reopen. */
  initial?: boolean;
}
```

`LabLink.fault?: LinkFault` (`labTypes.ts:55-64`), `lab.Link.Fault *Fault \`json:"fault,omitempty"\``
(`lab.go:113-118`), and the schema (`contracts/lab.schema.json`, the `links` items object).
Go side, `Fault.TargetEndpoint *int \`json:"targetEndpoint,omitempty"\`` — a **pointer**, so
"absent (all endpoints)" and "endpoint 0" are distinguishable; `targetEndpoint: 0` meaning
"all" would be an unfixable ambiguity later. The schema constrains it to
`{"type":"integer","minimum":0}` and the handler additionally rejects
`*targetEndpoint >= len(link.endpoints)`.

`down` composes with `targetEndpoint` the same way an impairment does: `down` alone unplugs
the whole link (detach every endpoint device), `down` + `targetEndpoint` unplugs that one
member of a segment (§7.4). `down` remains mutually exclusive with the impairment fields on
the **same** fault record.

**Default-inactive on reopen is the idea doc's locked decision** and is implemented as: at
`lab.load`/`lab.start` the supervisor applies **only** faults with `initial === true`; every
other persisted fault is loaded into `ll.linkFaults` as a *definition* with an `active:false`
marker and is **not** pushed to `tc`. The shape precedent is `Capture{Enabled,Mode}`
(`lab.go:129-132`) and its auto-arm at start (`fabric_linux.go:236-244`) — same mechanism,
opposite default, and the opposite default is deliberate: a saved lab that silently comes
back with 30% packet loss is a support ticket.

Scheduled and momentary failures are **runtime-only intents, never persisted as timers**:
`link.setFault` may carry `afterSec` (apply this fault in N seconds) and/or `forSec` (revert
after N seconds). The supervisor holds a `*time.Timer` per link in `loadedLab`, cancelled by
any subsequent `link.setFault`, `link.remove`, `node.stop`, `lab.stop`, and by
`detachFabricLink`/`teardownFabric` (§7.4).

### 7.6 Protocol: a new verb and a new event — decided, not `link.stats`

**Fault state must not ride `link.stats`.** `fabricStats` (`fabric_linux.go:660-699`) only
emits for links in `ll.fabricLinks`, only on the sampling tick, and `labStore.svelte.ts:119`
records that link.stats is **silent for idle links** with the GUI decaying glow on staleness.
An admin-down link carries no frames, produces no stats event, and would therefore never
deliver its own fault flag — the state the GUI most needs would be the one it never gets.

- **Verb `link.setFault`** — `server.go:201-229` registration table; args
  `{labId, link, fault: LinkFault|null, afterSec?, forSec?}`; `null` clears. Handler
  validates, in this order: **(a)** `linkFaultSupported` (§7.1) — an unsupported link
  (a tapless IOL serial endpoint) is rejected with `CodeUnsupported` and its reason
  **before** any state is touched; **(b)** §7.3's ranges, the `reorder`-needs-`delay` rule,
  and `down` exclusive with impairment; **(c)** `targetEndpoint` in range for this link's
  endpoint count. Then it updates `ll.doc.Links[i].Fault` and `ll.linkFaults`, applies via
  `reconcileLinkFault` / `reconcileFabricLinkDown` (§7.4), emits `link.fault`, and returns
  the applied state.
- **Event `link.fault`** — `protocol/message.go:63-69` (`EventLinkFault = "link.fault"`),
  payload `LinkFaultData{Link int, Fault *LinkFault, Active bool, Reason string}`. Emitted on
  every change **and replayed for every faulted link at `lab.start`**, exactly as
  `handleLabStart` already replays armed captures (`handlers.go:540-548`) — copy that
  pattern, it exists for this reason.
- `docs/protocol.md` — a `### link.setFault` entry under `## Verbs` and a `link.fault` entry
  under `## Events (server → GUI push)`.
- `mockTransport.ts:184-463` — a `case "link.setFault":` that updates the local doc and
  synthesises the event, or the browser-mock dev path silently no-ops.

### 7.7 Canvas rendering

- `labStore.svelte.ts` — `linkFaults = $state<Record<number, LinkFaultData>>({})` beside
  `linkStats` (`:100`); a `case "link.fault":` beside `:448`; reset it in all three places
  `linkStats` is reset (`:314`, `:500`, `:858`).
- `CanvasInner.svelte:158-172` — `toFlowEdge`'s `data` gains `fault: labStore.linkFaults[l.id]`.
- **`CanvasInner.svelte:226-230` must gain `void labStore.linkFaults;`.** That `$effect`
  already explicitly `void`s `nodeStates` and `consolePorts` to force a rebuild; without the
  same line for faults the edge class will not update when a fault changes. This is a
  two-word change that is trivially forgotten and produces a "the backend works but the
  canvas doesn't" bug.
- `FloatingEdge.svelte` — three visually distinct states, composed into the same class string
  the cable path already builds at `:296-299`:
  - **`is-admin-down`** — grey, dashed, no traffic glow, plus a small "⦸" badge at the cable
    midpoint. Suppress `is-traffic` entirely.
  - **`is-impaired`** — amber, dashed, glow retained (traffic *is* flowing), plus a compact
    badge summarising the fault (`50ms · 1%` / `10mbit`), with the full parameter set in the
    `title`. When `targetEndpoint` is present the badge gains a direction arrow pointing at
    that endpoint (`→ R2`, resolved through the link's own endpoint list, **not** through
    `source`/`target` — `toFlowEdge` only carries two of an N-endpoint segment's members) and
    the `title` names the endpoint node + interface. When it is absent the badge says
    `both ends`.
  - **unexpectedly down** — **derived, not transmitted**: the link is attached, has **no**
    active fault, and at least one endpoint node's `labStore.nodeStates[id] === "crashed"`.
    Render red dashed. Deriving it avoids inventing a supervisor-side "unexpected" concept
    that nothing can authoritatively decide.
  CSS goes beside the existing `.is-capture` / `.traffic-glow` rules at `:470-533`.
- Entry point: the link context menu (`CanvasInner.svelte:539-556`, `onEdgeContextMenu`
  already extracts `linkId`) gains a **Faults** submenu. **Decided over a new panel**: the
  menu already exists, is already link-scoped, and a fault is a per-link property, not a
  workspace. The submenu's target selector lists **every** endpoint of the link ("Both ends"
  plus one entry per endpoint, labelled `<node name> <interface>`), so a 3-endpoint segment
  is addressable from the UI — the `direction` radio pair could not have expressed it. For a
  link `linkFaultSupported` rejects (§7.1), the whole **Faults** item is rendered disabled
  with the supervisor's reason as its `title`; the client mirrors the same predicate locally
  so the menu is right before any round-trip.

### 7.8 Runtime dependency — verify, do not assume

`tc` ships with `iproute2`, already in `BASE_INCLUDE` (`build-rootfs.sh:239`) — confirmed, no
change needed.

**`sch_netem` is a kernel module, not a userspace package.** The appliance runs the host
VM's kernel; whether `sch_netem` is built in, available as a loadable module, or absent
entirely is **not determined by anything in this repo**. Consequences:

- The first `SetNetem` on a node must surface a clean, specific error if the qdisc is
  unavailable (`tc` reports `Error: Specified qdisc kind is unknown.` / `Unknown qdisc
  "netem"`), not a generic failure. Map that string to a dedicated protocol error whose
  message names the missing module.
- **Admin down/up must not depend on netem at all** (§7.4 uses only `ip link set … nomaster`),
  so a kernel without `sch_netem` still gets the single most-used fault type. State that as a
  deliberate degradation path.
- §9.3 makes proving `sch_netem` a **blocking** live gate, run before any impairment test.

### 7.9 Testing bar

1. `cd supervisor && go build ./... && go vet ./... && go test ./...` — green.
2. **`netemCmds` argv table test** (pure, runs on any OS — same class as
   `fabric/commands_test.go`'s existing tests): each fault combination → the exact expected
   argv slice, compared element by element. Include: delay only; delay+jitter; loss only;
   rate only; all six together; and the **rejected** combinations (`reorder` without `delay`,
   `down` together with `delayMs`).
3. **`isBenign(opNetemClear, …)` test** — **both** tolerated groups return true (the three
   missing-qdisc strings *and* the three missing-device strings, §7.3), while
   `"operation not permitted"`, `"sudo: a password is required"` and an `unknown qdisc`
   usage string return **false**. Plus `isBenign(opNetemSet, …)` returns false for
   everything.
4. **Missing-device teardown test** — `detachFabricLink` and `teardownFabric` over a link
   whose endpoint devices no longer exist complete **without error** and still emit their
   clear commands; the same run twice is a no-op (idempotency, not just tolerance).
5. **Endpoint-indexed targeting test** — build a 3-endpoint segment whose **first** endpoint
   node is stopped (so `fabricLinkTapDevs` compacts it away), set
   `targetEndpoint: 2`, and assert the netem lands on **endpoint 2's** device and on no
   other. A test that only ever exercises a fully-started 2-endpoint link cannot catch the
   compaction bug, so the stopped endpoint is mandatory, not incidental. Also assert an
   absent `targetEndpoint` produces commands for every present device, and that an
   out-of-range `targetEndpoint` is rejected by the handler.
6. **Late-endpoint reapply test (§7.4)** — set a symmetric `delay` while one endpoint node is
   **stopped**, then start it; assert `SetNetem` was issued for the newly appeared device
   with the **same** parameters, and that a device dropped from the target set gets
   `ClearNetem`. This is the asymmetric-fault regression.
7. **Unsupported-link test (§7.1)** — `link.setFault` on a link with an IOL **serial**
   endpoint returns `CodeUnsupported`, names the node + interface, and leaves
   `ll.linkFaults`, `ll.doc` and the command stream **untouched**.
8. **The §7.4 admin-down regression test**: link admin-down → start another node → link is
   still down (asserted against the command stream, not a live kernel) — and, separately,
   an **initially-down** link at `lab.start` still gets its `EnsureBridge` (the
   `fabricLinkFullyAttached`-short-circuit bug would have skipped it).
9. **Timer-cancellation test** — `link.setFault{afterSec:5}` then `link.remove` leaves no
   pending timer; same for `lab.stop`.
10. **Persistence test** — a lab saved with `fault{lossPct:30}` and no `initial` reloads with
    the fault present in the doc and **no `tc` command issued**; the same lab with
    `initial:true` reloads and issues exactly the expected commands. Include a
    `targetEndpoint` in one of the saved faults and assert it round-trips as a pointer (an
    explicit `0` survives and does not degrade to "all endpoints").
11. **Validation test** — every out-of-range value rejected at the handler with a message
    naming the field, `targetEndpoint` included.
12. `cd app && npm run check` green; a Svelte test (or a careful manual note in the PR) that
    the three edge classes are mutually exclusive.

### 7.10 Batch C acceptance gate (local)

1–12 above, plus: grep proof that `internal/relay`, `internal/iouyap`, `internal/vtap` and
`internal/bcap` are **untouched** (zero changed lines) — the fault feature lives entirely in
`internal/fabric` + `internal/server` + the protocol + the GUI, and any diff outside that set
means the design drifted.

---

## 8. Explicit non-goals for the implementing agents

**Batch A**
- **Do not touch `spawnVPCS`, `VPCSArgv`, `internal/vtap`, `setupVPCSFabric`, `teardownVPCS`,
  or the VPCS palette chip.** PC ships alongside. Zero changed lines in the VPCS path is an
  acceptance criterion (§5.7 step 5).
- **Do not write a `spawnPC` in `node/spawn_linux.go`,** and do not add a `"pc"` case to
  `Spawn`'s switch. PC never reaches `Spawn` (§4.2); it is intercepted in `startNodes`
  exactly as NAT and tool are.
- **Do not add a `/pc/` route to wsbridge** (§4.3).
- **Do not add capabilities.** `AmbientCaps`, `AllowedCaps`, `manifestCheckCaps` and
  `launchSetprivArgv` are untouched; `pack.json` keeps `caps: []`.
- **Do not change `endpointSetupSteps()`.** The `ping_group_range` sysctl has no independent
  lifetime (§5.2).
- **Do not hand-construct a `tool.Pack` literal** for `pc` (§4.2). Load it with
  `tool.LoadPack` so `GUIBin` is absolute and containment-checked; a bare `"pc-gui"` would be
  resolved through `PATH`.
- **Do not add a general environment map to `tool.Config`.** `IOLBOX_PC_CLI_SOCK` arrives via
  one typed boolean plus a path helper (§5.3a); anything more is an escape hatch around
  `ScrubbedEnvAllowlist`.
- **Do not invent a supervisor→pack control verb or a second inbound socket for `save`.** The
  channel is a supervisor-initiated pull over the pack's existing GUI socket (§5.4a).
- **Do not migrate existing labs.** No saved lab's VPCS node becomes a PC node.
- **Do not change `clab.ts`'s import mapping** — linux/host/alpine keep importing as `vpcs`.

**Batch B**
- **Do not import, move, or refactor `supervisor/internal/extnet/dhcp.go`** (§6.3). It stays
  exactly as it is, serving the NAT gateway.
- **Do not add a `caps` entry or any supervisor change for the privileged ports** — P4 already
  landed it (§6.2).
- **Do not add DNS recursion, forwarding, DNSSEC, or TCP/53.**
- **Do not add NTP modes 1/2/5/6/7**, and do not implement any control/private-mode handling
  beyond rejecting it.
- **Do not add persistence.** In-memory only, matching every other pack; a node restart loses
  leases and logs, and that is documented behaviour.
- **Do not touch any existing pack.**

**Batch C**
- **Do not put netem on the bridge** (§7.2). If a reviewer or a future reader proposes it,
  the answer is in §7.2 with the mechanism.
- **Do not implement ingress shaping** (`ifb` + `mirred`) (§7.2, §10).
- **Do not route fault state through `link.stats`** (§7.6).
- **Do not touch `internal/relay`, `internal/iouyap`, `internal/vtap`, `internal/bcap`, or
  `internal/dirstat`.**
- **Do not "fix" `docs/bridge-fabric-retirement-map.md` or `supervisor/README.md:199`** as
  part of this batch. Both are stale (§3.4); correcting them is its own small change (§10).
- **Do not implement serial static taps** to widen the fault domain (§7.1). Faults on a
  serial-endpoint link are refused, not emulated.
- **Do not short-circuit `fabricLinkFullyAttached` on fault state** (§7.4). It is a kernel
  predicate; the down path is its own function.
- **Do not index `fabricLinkTapDevs`' result positionally** to identify an endpoint (§3.5).
- **Do not implement per-VLAN or per-protocol impairment.** netem is per-device.

**All batches**
- Do not add per-pack authentication; wsbridge's session + Origin check gates `/tool/*`
  generically.
- Do not remove the checked-in `gui.exe` artifacts (§3.8).
- Do not run `go build`, `npm run check`, or any test during the *planning* review pass — the
  implementing agents run them, per §5.7 / §6.7 / §7.10.

---

## 9. Live-VM validation checklist (orchestrator only — the plan attempts no VM steps)

**§9.1 gates §9.2. §9.3 is independent of both.** A protocol failure diagnosed before its
enablement step has passed has been diagnosed at the wrong layer.

### 9.1 PC node — containment and bring-up

1. Redeploy the rootfs with Batch A. Drop a **PC** node and a **VPCS** node in the same lab,
   wire each to an IOL router.
2. **Namespace containment (required, not optional).** Read
   `cat /proc/sys/net/ipv4/ping_group_range` in the **root** netns *before* starting any
   node; record the actual value (do not hardcode an expectation). Start the PC node, then:
   - `ip netns exec iolt<N> cat /proc/sys/net/ipv4/ping_group_range` → `0	2147483647`
   - root netns re-read → **byte-for-byte unchanged from the pre-test reading.** A changed
     root value means the write leaked; revert Batch A's sysctl immediately.
   - `ip netns exec iolt<N> cat /proc/sys/net/ipv4/ip_unprivileged_port_start` → `1` (P4's
     knob, confirming the folded sequence still runs both commands in order).
3. **Data plane:** `ip netns exec iolt<N> ip -br addr` shows `eth1` up with the configured
   address; `ip -br link show vtool<N>` in the root ns shows `master iolbr<linkID>`.
4. **Console:** the node's Console button opens an xterm with a `PC>` prompt. **Simultaneously**
   open a native telnet to `<vm-ip>:<consolePort>` — both must be live at once and see each
   other's output (the multi-client property VPCS does not have).
5. **`?`** prints the full grouped legend. **`? ping`** prints ping's usage.
6. `ip 10.0.0.10/24 10.0.0.1`, then `show ip` reflects it, then **click the GUI button** —
   the in-canvas panel shows the same address without a reload. Then change it in the GUI and
   `show ip` in the console reflects that. (This is the doc's "two coexisting entry points"
   claim, and it is the one thing unit tests cannot prove.)
7. `ping <router-ip>` succeeds with a summary line. `trace <far-router-ip>` shows the hops.
   Confirm from the router side (`show ip arp`, `debug ip icmp`) that the source is the PC's
   `eth1` address, not the appliance host.
8. `ip dhcp` against a router running `ip dhcp pool` — lease acquired, `show ip` shows the
   lease, and the GUI's lease panel shows the options the router sent.
9. `dns <name> @<router-ip>` against `ip dns server`; `tcp listen 8080 -e` then connect from
   the router (`telnet <pc-ip> 8080`); `flow start <router-ip> 100 512` and confirm the link's
   canvas glow lights up and the Watcher shows the flow.
10. **VPCS regression:** the VPCS node in the same lab starts, its console works, and it pings
    the same router. Nothing about VPCS changed.
11. **Inspector:** the PC node's pack `<select>` **does not exist**, and the "Network tools"
    chip's Inspector dropdown **does not list "PC"**.
12. `lab.saveDoc` → reload the page → `lab.getDoc`: the PC node round-trips with its
    `config.net` and `config.pc` intact.

### 9.2 `netsvc`, against a real IOL router

Drop a Network tools node, select the **Network Services** pack, give it a static `eth1`
address, wire it to a router.

1. **Bind proof first:** `ip netns exec iolt<M> ss -lunp` shows `0.0.0.0:53`, `:67`, `:69`,
   `:123`; no bind-error banner on the dashboard.
2. **DHCP:** router `interface … ip address dhcp` → lease acquired; the GUI lease row shows the
   router's option-55 request list beside what was sent. Then configure `ip helper-address
   <netsvc-ip>` on a *second* router acting as a relay for a third segment and confirm
   `giaddr` and option 82 appear in the lease detail, and the reply reaches the relay.
3. **DNS:** router `ip name-server <netsvc-ip>` + `ip domain-lookup`; `ping <name>` resolves.
   Add a CNAME and confirm the router follows it. Query an unknown name and confirm the GUI
   shows NXDOMAIN with its "outside the zone" hint.
4. **NTP:** router `ntp server <netsvc-ip>`; wait for `show ntp status` to report
   synchronised and `show ntp associations` to show the peer reachable (`reach 377` takes a
   few minutes — allow for it). **The GUI's per-client row must show a plausible offset**; a
   ~70-year offset means the epoch constant is wrong, and a wildly wrong delay means the
   Originate field was not echoed.
5. **TFTP:** router `copy running-config tftp://<netsvc-ip>/test.cfg` and
   `copy tftp://<netsvc-ip>/test.cfg running-config`. Test **both** a file whose size is an
   exact multiple of 512 and one that is not. Attempt `copy tftp://<netsvc-ip>/../etc/passwd`
   and confirm it is refused.
6. **Cross-service:** configure the DHCP scope to hand out option 6 (= netsvc) and options
   66/150 (= netsvc), then on a fresh router interface do `ip address dhcp` and confirm the
   router picks up the DNS server *and* can `copy tftp:` without being told the address —
   this is the lesson the pack exists to teach, and it is the only step that proves all four
   services agree.

### 9.3 Link impairment

1. **BLOCKING: prove `sch_netem` exists.** On the appliance:
   `tc qdisc replace dev <any existing iol tap> root netem delay 10ms` then
   `tc qdisc show dev <that tap>` then `tc qdisc del dev <that tap> root`. If the add fails
   with "Specified qdisc kind is unknown", **stop**: only admin down/up is deliverable on this
   image, and that must be reported before any impairment test is attempted.
2. Two IOL routers, one link, an OSPF adjacency up.
3. **Admin down:** context menu → Faults → Down. The adjacency drops; the cable renders grey
   dashed with the ⦸ badge; `ip -br link show <both endpoint devs>` shows **no** master. Then
   **start a third node in the same lab** and confirm the link is **still down** (the §7.4
   regression, live).
4. **Admin up:** adjacency re-forms; cable returns to normal; `dirstat` traffic resumes.
5. **Delay:** apply 100 ms one-way. `ping` between the routers shows ~200 ms RTT (2× — the
   §7.2 convention, proven live). `tc qdisc show` on both endpoint devices shows the netem.
6. **Targeted delay:** apply 100 ms with `targetEndpoint: 0`. Ping from endpoint 1 → ~100 ms
   RTT; verify with `tc qdisc show` that exactly one device carries the qdisc, **and that it
   is the device belonging to `endpoints[0]` of the link in the saved doc** (not merely "some
   one device" — this is the compaction bug §7.9.5 guards).
7. **Loss:** 20 %. `ping … repeat 100` shows roughly 20 % loss (allow wide tolerance — netem
   loss is random).
8. **Rate:** 1 mbit. A TFTP or `copy` transfer across the link is visibly throttled; the
   canvas glow reflects the reduced rate.
9. **Reorder without delay** is refused by the GUI with a clear message (never reaches `tc`).
10. **Momentary failure:** "down for 10 seconds then restore" on an HSRP or STP topology —
    confirm the failover happens, the timer fires, and the link restores itself with **no**
    further user action. Then `lab.stop` mid-countdown on a fresh scheduled fault and confirm
    no timer fires afterwards.
11. **Persistence:** save the lab with an impairment and **without** `initial`, reload it, and
    confirm the fault is present in the Faults menu but **not applied** (`tc qdisc show` is
    clean, ping is normal). Then mark it initial, save, reload, and confirm it **is** applied
    at start.
12. **Teardown hygiene:** `lab.stop`, then `tc qdisc show` across every `iol*`/`vtool*` device
    — no netem survives. Then `lab.load` a different lab and confirm it starts clean.
13. **Segment link:** a 3-endpoint segment with loss applied to **the third** member
    (`targetEndpoint: 2` — the case the old `direction: 0|1|2` encoding could not express) —
    only that member's traffic is impaired. Then **stop** that member's node, confirm the
    fault survives in the GUI, restart it, and confirm the qdisc is **reapplied** to its new
    device without any user action (§7.4's late-endpoint reconciliation, live).
14. **Unsupported link:** wire a link on an IOL **serial** interface (`s0/0`) and confirm the
    context menu's **Faults** item is disabled with a reason naming the interface, and that
    forcing `link.setFault` over the protocol returns `unsupported` rather than a partial
    apply. (The link itself is expected not to come up — that is the pre-existing §3.4 gap,
    not a Batch C regression; record it as such.)
15. **Initially-down link:** save a lab whose link carries `fault{down:true, initial:true}`,
    reload and start it, and confirm `ip -d link show iolbr<linkID>` shows the bridge
    **exists** with no members. A missing bridge here means the
    `fabricLinkFullyAttached` short-circuit was reintroduced (§7.4).

Report a per-step verdict table. Any §9.1 step-2 failure blocks the rest of §9.1. A §9.3
step-1 failure blocks §9.3 steps 5–15 but not 3–4 and 14.

---

## 10. Out of scope — named, not silently dropped

**Batch A / PC**
- **Making PC the default palette entry and demoting VPCS.** The idea doc explicitly stages
  this after live verification. Not in this plan.
- **Removing VPCS.** It is a real bundled binary with near-zero resource cost and saved labs
  reference it.
- **`eth0` interface parity for PC.** §5.2 chose `eth1`; the alternative (threading a
  `guestIface` through `tool.Config` and the four netns command builders) is a follow-up if
  sol-medium judges parity worth it.
- **IPv6 addressing on PC.** `tapCreateCmds`/`bridgeCreateCmds` deliberately disable IPv6 on
  fabric devices (`fabric/commands.go:14-32`); PC's `eth1` is a veth, not a tap, so IPv6 is
  *not* disabled there — but no IPv6 CLI verbs ship in v1 and the GUI shows no v6 state.
- **`netprobe` as the engine for idea #5's guided checkpoints.** That plan depends on this
  one; it is not part of it.
- **A dedicated PC glyph** distinct from VPCS's `pc` (§5.4).

**Batch B / `netsvc`**
- **DHCPv6, PXE/BOOTP boot-file chaining, DDNS.**
- **DNS recursion, forwarding, zone transfers (AXFR/IXFR), DNSSEC, and TCP/53.** UDP/53,
  authoritative-only.
- **NTP client mode, symmetric peering, broadcast/multicast mode, authentication (MD5/SHA
  keys), and mode 6/7 control queries.** Server mode 4 only.
- **TFTP block-number wrap beyond 65535 blocks (32 MiB)** and the RFC 2347/2348/2349 option
  extensions (`blksize`, `timeout`, `tsize`).
- **Persistence of leases, zones, or transfer logs.**
- **A dedicated `netsvc` icon glyph** — it shares `server` with `webserver` (§6.6).

**Batch C / impairment**
- **Ingress impairment** (`ifb` + `tc mirred`) — §7.2.
- **Per-VLAN, per-protocol, or per-flow impairment.** netem is per-device.
- **Bandwidth shaping via `htb`/`tbf` class hierarchies**, burst tuning, and queue-length
  (`limit`) tuning. One flat netem qdisc.
- **`corrupt` and `gap`/`ecn` netem features.**
- **`distribution normal`/`pareto` delay distributions** — deferred pending the live check for
  `/usr/lib/tc/*.dist` (§7.3).
- **Fault presets** ("satellite link", "lossy Wi-Fi") — obvious follow-up once the primitives
  ship.
- **Faults on IOL serial-interface links** (§7.1). They have no static tap, so there is no
  device to attach a qdisc to and no relay path to impair instead; `link.setFault` refuses
  them explicitly.
- **Serial static-tap support in the fabric** (`computeStaticTaps` enumerates
  `IfacesForCounts(eth, 0)`). Adding it would fix serial-link *realizability* first and
  faults only incidentally; it is a fabric change with its own live gate, not a fault change.
- **Correcting `docs/bridge-fabric-retirement-map.md` and `supervisor/README.md:199`.** Both
  document `wiringFor`/`buildBridgePlan` as live; both are gone (§3.4). The retirement map's
  own header says ANALYSIS ONLY; the README section is normative-looking prose and is the
  more misleading of the two. Rewriting them is one separate doc change with its own review.

**Across all three**
- **Floating console windows (idea #10)** and the **learner console workspace (idea #9)**.
  Explicitly out of THIS plan: idea #10 needs a genuinely new drag-move UI primitive that
  does not exist anywhere in the codebase today, and the idea doc scopes it separately. Batch
  A adds a *second button* to a node face; it does not touch `consoleUiStore`, `SplitPane`, or
  the dock.
- **Protocol Lens (#7)**, **named baselines / config diff (#4)**, **guided checkpoints (#5)**,
  **the addressing worksheet (#8)**, and **`bgppeer` (#6)**.
- **Relaxing `webserver`'s `< 1024` port guard** now that P4 made :80 bindable
  (`webserver/gui/web.go:74-80`, `:110-112`). Still deferred, still tempting, still its own
  change with its own live gate.

---

### Critical files for implementation

**Batch A**
- `J:\Claude code\iolab\supervisor\internal\node\spawn_linux.go` (`Process` doc :26-37 = the two console models; `Spawn` :55-67 = the switch PC must NOT join; `spawnVPCS` :167-220 = the model that does not apply, §4.1; `NewProcessForTest` :470-475 = proof the hub takes any `io.ReadWriter`)
- `J:\Claude code\iolab\supervisor\internal\node\console_hub.go` (`newConsoleHub` :290 — the console bridge PC reuses)
- `J:\Claude code\iolab\supervisor\internal\tool\endpoint_linux.go` (`Start` :59, `endpointLaunchSpec` :257-281, `endpointSetupSteps` :589-591 — the machine PC reuses instead of duplicating)
- `J:\Claude code\iolab\supervisor\internal\tool\netns.go` (`netnsCreateNetnsCmds` :18-24 — the one-line `ping_group_range` insertion, §5.2; `GuestIface` rename at :36)
- `J:\Claude code\iolab\supervisor\internal\tool\tool.go` (`Config` :267-283 — where `CLISocket` lands; `OptionsFile` :285-291 — the shape `CLISocketFile` copies; `ScrubbedEnvAllowlist` :220-230; `Pack` :204-213 — the absolute-`GUIBin` contract)
- `J:\Claude code\iolab\supervisor\internal\tool\manifest.go` (`LoadPack` :18-68 — the containment + absolute-path construction §4.2 must go through)
- `J:\Claude code\iolab\runtime\files\tools\packs\syslog\gui\config.go` (`Store.Save` :65-82 — the atomic options-file write `save` reuses, §5.4a leg 1)
- `J:\Claude code\iolab\supervisor\internal\server\toolproxy.go` (:13-42 — the single function that must accept `KindPC`, §4.3)
- `J:\Claude code\iolab\supervisor\internal\server\handlers.go` (:562-587 `startNodes` dispatch; :684-772 `startToolNode`, the template for `startPCNode`)
- `J:\Claude code\iolab\supervisor\internal\lab\validate.go` (:50-80 and :124-147 — the two kind switches)
- `J:\Claude code\iolab\app\src\lib\nodes\VpcsNode.svelte` + `ToolNode.svelte` (the two halves `PcNode.svelte` merges: face/console chrome :47-82, and `gui-button` :66-72 + `tool-panel` :82-90)
- `J:\Claude code\iolab\app\src\lib\components\CanvasInner.svelte` (:41-49 registry, :449-485 `buildDroppedNode`, :226-230 the edge `$effect`)

**Batch B**
- `J:\Claude code\iolab\runtime\files\tools\packs\syslog\gui\main.go` (:13-46 — the canonical pack `main` to copy)
- `J:\Claude code\iolab\runtime\files\tools\packs\aaa\` (the multi-protocol pack precedent: one dashboard, one options file, per-protocol ring buffers)
- `J:\Claude code\iolab\supervisor\internal\extnet\dhcp.go` (READ ONLY — the sibling DHCP implementation, §6.3; its `DecodePacket` :100-148 / `Encode` :154-184 / `Handle` :220-238 / `leaser` :263-306 are the structural model, not an import)
- `J:\Claude code\iolab\runtime\build-rootfs.sh` (:177, :330, :361 — the three `for pack in …` lists; :239 `BASE_INCLUDE`, unchanged)

**Batch C**
- `J:\Claude code\iolab\supervisor\internal\server\fabric_linux.go` (`fabricLinkTapDevs` :701-726 = the attachment surface **and the compaction trap**, §3.5; `fabricLinkFullyAttached` :276-306 — stays a pure kernel predicate — and `startFabric` :227-231 = the re-attach trap, §7.4; `attachFabricForNode` :537 = the late-endpoint path both §7.4 fixes hang off; `attachFabricLink` :349-427; `detachFabricLink` :556-594; `teardownFabric` :770-822)
- `J:\Claude code\iolab\supervisor\internal\server\fabric.go` (`computeStaticTaps` :79-131 — `IfacesForCounts(eth, 0)`, the Ethernet-only enumeration that defines §7.1's realizable domain; `tapForEndpoint` :171-182 — the miss that `linkFaultSupported` tests)
- `J:\Claude code\iolab\supervisor\internal\lab\validate.go` (:124-128 — accepts serial via `netmap.ParseIface`, which is why validation is not a sufficient guard for §7.1)
- `J:\Claude code\iolab\supervisor\internal\fabric\commands.go` (:33-89 — the pure argv builders `netemCmds` joins; :98-104 `sudoArgv`)
- `J:\Claude code\iolab\supervisor\internal\fabric\manager_linux.go` (:26-33 the `op` enum, :93-108 `runIdempotent`, :141-157 `isBenign` — all three gain a netem arm)
- `J:\Claude code\iolab\supervisor\internal\protocol\verbs.go` (:506-550 — `LinkData`/`LinkStatsData`, and the `[ep0, ep1]` ordering convention faults must match)
- `J:\Claude code\iolab\app\src\lib\edges\FloatingEdge.svelte` (:52 stats read, :283-320 the class composition, :470-533 the CSS)
- `J:\Claude code\iolab\app\src\lib\labStore.svelte.ts` (:100 `linkStats` declaration, :448-455 the event case, :314/:500/:858 the resets)
