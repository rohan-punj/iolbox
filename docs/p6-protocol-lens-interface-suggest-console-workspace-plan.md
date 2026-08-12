# P6 — Protocol Lens, next-free-interface suggestion, learner console workspace

Status: **dispatch plan. Reviewed by `codex sol-medium`; findings applied (§1a). Not
implemented.** Three batches drawn from
`docs/learning-features-gui-ideas-plan.md` (GUI/UX ideas #7, #8, #9). All three are
**frontend-weighted**: Batch 8 touches exactly one file, Batch 9 is entirely in `app/`, and
Batch 7 is the only one that reaches the supervisor at all (and only for a ~25-line addition
to an existing read loop). **§4 corrects six load-bearing assumptions in the idea doc that do
not survive contact with the current code** — read §4 before §5/§6/§7. Two of those
corrections shrink a batch (idea #8 is already half-built; idea #9's tiling needs no
lifecycle work), and four of them grow one (idea #7's attribution, VLAN, DHCP and
single-socket constraints).

## 1. Model loop / process

1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it.** One area per batch needs
   disproportionate attention. In each case the failure mode is the same class p5 named for
   "netem on the bridge does nothing": *code that builds, passes `npm run check`, renders
   something, and is quietly wrong about the network.*
   - **Idea #7 — §7.3 (attribution) and §7.4 (what has to be decoded fresh).** The idea doc
     says "plain-English event timeline … `R2 sent OSPF Hello`". **The capture stream cannot
     say who sent it.** `bcap` runs `tcpdump -i <bridge>` (`bcap/capture_linux.go:45`) and
     `dirstat/dirstat.go:1-13` states in its own package doc that per-endpoint attribution is
     "the question a tcpdump-on-bridge capture can't [answer]". A Lens that names nodes by
     guessing — from IOL's `aabb.cc00.*` MAC convention, from "first MAC seen wins", from the
     link's endpoint order — will look right on a two-router lab and be wrong the first time
     a switch forwards a third device's frame across the link. Review §7.3's dirstat
     MAC-learning channel and its explicit **never-guess** degradation against
     `dirstat_linux.go:112-136` and `fabric_linux.go:831-870`.
     **Post-review, this is where two of the eight blocking findings landed and the risk is
     now stated more precisely:** the danger is not "a MAC-based map" in the abstract, it is
     (a) *learning a forwarded source MAC as if the relaying endpoint originated it* — which
     the first draft's "record every non-`PACKET_OUTGOING` source MAC, first-wins, 8 per
     endpoint" rule did, turning an IOL switch into a confident false attributor — and (b)
     *endpoint-index compaction*, because `fabricLinkTapDevs` throws the doc endpoint index
     away and `dirstat.Open` re-derives it from slice position, so a link with a tap on
     endpoint 1 but not endpoint 0 mislabels every frame as endpoint 0. §7.3 now answers both
     structurally (singular-MAC-with-ambiguity learning + aging; endpoint-indexed devs on the
     wire). Re-review §7.3 against `fabric_linux.go:875-887` and `dirstat_linux.go:45-87`
     specifically — a fix that "looks applied" but still reads a compacted slice reintroduces
     the whole class.
   - **Idea #8 — §6.1, the claim that this is a one-file change.** The idea doc says the
     picker should "pre-select … instead of listing all interfaces flat". **It already
     pre-selects** (`InterfacePicker.svelte:39-42` → `interfaces.ts:44-49`). The risk here is
     the opposite of the usual one: an implementing agent reads the idea row, does not read
     the component, and rebuilds selection logic that exists — or "improves" `usedInterfaces`
     / `nextFreeInterface`, which are shared with the Node Edit dialog. Review §6.1's
     restatement of the actual gap (the suggestion is invisible, not absent) and hold the
     batch to one file.
   - **Idea #9 — §8.2, the pane model: CSS grid participation, focus ownership, and the
     mark's stream position.** The idea doc says "extends existing xterm.js dock, no backend
     rework". True. The first draft named the `active` prop as the batch's "green build, dead
     feature" trap; **review corrected that** (§1a MINOR 1): both `ConsoleTerm.svelte:91-95`
     and `CaptureTerm.svelte:141-143` already observe their container with a `ResizeObserver`
     that refits and (in `ConsoleTerm`) re-sends NAWS on any real box change, so a
     visible-but-unselected pane is **not** proven to keep a stale `cols`/`rows`. The
     `visible`/`focused` split is still the right design — `term.focus()` must not be shared
     with the refit gate or four terminals fight for focus on every layout change, and the
     `!active` early return at `:114-121` is a genuine hole for a pane that is visible but not
     selected — but it is a correctness-and-ownership fix, not a rescue from a proven bug.
     The three things that *are* load-bearing here and each cost a blocking finding:
     **(a)** untiled panes must leave CSS-grid flow entirely (`visibility: hidden` keeps a
     grid item, so hidden tabs would eat tile cells and spawn implicit rows);
     **(b)** `labStore.openConsole()` selects the node it opens, so focus sync cannot be
     one-way out of the new store or a canvas-opened console never surfaces;
     **(c)** a mark needs a **per-link delivery sequence number**, not a wall clock, or Batch 7
     cannot place its divider. Review §8.2, §8.3, §8.5 against `Console.svelte:522-542`,
     `labStore.svelte.ts:997-1002`, and `ConsoleTerm.svelte:91-121`.
3. **`codex luna-xhigh` agent(s) implement. Recommendation: sequential, in the order 8 → 9 →
   7.** Batch 8 shares **zero** files with 9 and 7 and can merge immediately. Batches 9 and 7
   **both** rewrite `Console.svelte`'s tab/pane model and both touch `labStore.svelte.ts`'s
   capture state — they must not run in parallel. 9 first, because 7's Lens tab has to be a
   pane in whatever model 9 lands (a Lens built against today's `activeCapture` local state
   would be rewritten immediately).
4. **Orchestrating session deploys to the real appliance VM and validates live** per §10. The
   plan attempts no VM steps and runs no builds.

## 1a. Review — codex sol-medium findings applied

`codex sol-medium` reviewed the draft and found **8 blocking issues + 1 minor** (all fixed in
this document before implementation). Five of the eight are Batch 7's attribution and Lens API;
four are Batch 9's pane model. What sol-medium *confirmed* is unchanged and must **stay**
unchanged: Batch 8 really is one-file-only (`InterfacePicker.svelte:39` → `interfaces.ts:27,43`
→ `InterfacePicker.svelte:95`); `@xterm/addon-search` really is absent from `app/package.json`
and the lockfile (§8.4 stands); the tRel-reset / tsMicros-vs-`Date.now()` clock-skew argument
against wall-clock mark correlation is real and correctly identified (§8.5 stands, and finding
8 *strengthens* it rather than weakening it); DHCP's stated BOOTP wire offsets agree
byte-for-byte with `extnet/dhcp.go`'s decoder; DHCP and HSRP really are absent from the browser
dissector (`pcapng.ts:566` falls through to a bare port summary); and the VLAN analysis is
correct — **a single 802.1Q tag does not by itself break MAC-based attribution** (§4.3 is
unrevised in substance). None of the edits below dilute any of those.

**One reading note for the implementing agents.** sol-medium's citations are against the
working tree *as of the review*, which has moved since §3 was written (p5 is still in flight —
`labStore.openConsole` is at `:997` in the review's numbering and `:951-955` in §3.4's).
§2's "re-grep before editing" is therefore not boilerplate; where a fix below depends on a
specific site, it names the symbol as well as the line.

| # | Finding | Resolution |
|---|---|---|
| 1 | **Endpoint attribution falsely names switches as originators.** Learning *every* non-`PACKET_OUTGOING` source MAC on a tap (`dirstat_linux.go:117`, `:130`) maps every downstream host behind an L2 switch onto that switch, so the Lens renders "SW1 sent …" — a confident lie, in direct violation of the plan's own never-guess guarantee (plan:343, plan:614). | §4.2 / §7.3 redesigned around **singular-MAC learning with an explicit ambiguity state**: an endpoint is attributable only while it has sourced exactly **one** distinct MAC. The second distinct MAC flips that endpoint to `ambiguous` **permanently for the current learning window** and it attributes **nothing** — a relay/switch endpoint therefore yields MACs, never a node name. The wire field carries the state (`single` \| `ambiguous` \| `none`), the pane header explains it, and the never-guess fallback now actually triggers on the exact input that used to defeat it (§7.3, §7.6, §10.3.3). |
| 2 | **Endpoint-index ordering is already broken with one tap.** `fabricLinkTapDevs` (`fabric_linux.go:875`, `:887`) compacts away the endpoint indexes that `fabricLinkEndpointDevs` preserves, and `dirstat.Open` (`dirstat_linux.go:52`) re-derives the index from slice position — so a link with a tap on endpoint 1 but not endpoint 0 attributes everything to endpoint 0. | §7.3 changes `dirstat.Open`'s signature to take **`[]dirstat.EndpointDev{Index, Dev}`**, `openLinkDirstat` (`fabric_linux.go:594`) feeds it **`fabricLinkEndpointDevs`** directly, and the `ep > 1` guard becomes a guard on the **doc index**, not on loop position. The index also goes **on the wire** (`endpointIndex` per attribution record) instead of being implied by array position. §7.3 records that this is a **pre-existing** defect in `ProtosDir`/`ProtosSubtypeDir`'s documented `[ep0, ep1]` contract (`verbs.go:562-590`) that Batch 7 incidentally fixes, and §7.7 gains the regression test that pins it. |
| 3 | **Permanent, non-aging, first-wins MAC sets cannot uphold "never guess."** They retain stale or spoofed MACs forever, and a MAC appearing on *both* endpoints (a moved NIC/VM) has no defined handling. | §7.3 specifies real lifecycle: each endpoint keeps **one** candidate `{mac, firstSeen, lastSeen}`; a candidate unseen for **`macTTL = 5 min` expires** (evaluated lazily at snapshot time — no timer goroutine); an expired `ambiguous` endpoint **relearns** from scratch, so a genuinely re-MAC'd NIC recovers instead of being poisoned forever. A MAC observed on **both** endpoints enters a bounded **conflict set** (32, drop-oldest, also TTL'd) and is attributed by **neither** endpoint until it ages out. Memory is bounded by construction (1 candidate/endpoint + a capped conflict set), which retires the old "8 per endpoint" cap. Separately, §7.5 now resolves the node name **at event-creation time** and stores it on the `LensEvent`, so aging can never retroactively rewrite history already on screen. |
| 4 | **`lensEvent(frame, origLen)` cannot construct its own declared output.** It must return `seq` and absolute `tsMicros`, and neither exists in an Ethernet frame — both live on the browser-side `ParsedPacket` (`pcapng.ts:25`), outside the bytes (plan:671). | §7.5 changes the signature to **`lensEvent(pkt: ParsedPacket, seq: number, attrib): LensEvent \| null`** — `tsMicros` comes off the packet, and `seq` is supplied by the caller. §8.5 defines where `seq` comes from and makes it do double duty: a **per-link monotonic delivery counter** owned by `consoleUiStore` (`advanceCaptureDelivery(linkId, n)`), incremented once per delivered batch, surviving reconnects, reset only on `closeCapture`. It is the same counter finding 8 needs, so Lens events and marks are ordered on one axis by construction. |
| 5 | **Hidden panes still consume grid cells.** Dropping `position: absolute` from `.term-slot` while keeping `visibility: hidden` for untiled panes (plan:808, `Console.svelte:528`, `:539`) leaves those panes as grid items — they occupy intended tile slots and create implicit rows. | §8.3 redesigned: **only panes in `tiles` are grid items.** `tabs` layout keeps today's CSS byte-for-byte (`position: absolute; inset: 0` + `.hidden { visibility: hidden }`) so the default path is untouched; tiled layouts add `.term-area.tiled { display: grid }` and `.term-slot.tiled { position: static }`, and every pane **not** in `tiles` gets **`.term-slot.untiled { display: none }`** — out of flow entirely. The zero-box hazard that motivated `visibility: hidden` is handled explicitly instead of implicitly: `visible=false` for untiled panes (so no `fit()` runs against a 0×0 box), a `requestAnimationFrame` refit when a pane becomes tiled, and a `clientWidth/clientHeight === 0` guard in both terminals' fit paths. Nothing is unmounted, so §9's "do not unmount hidden panes" is intact. |
| 6 | **No sync path from existing "open console" callers into the new focus state.** `labStore.openConsole()` selects the node it opens (`labStore.svelte.ts:997`), but the plan only synced `focused → activeConsoleTab`, one way (plan:783) — so opening a console from the canvas leaves the old pane focused. | §8.2 makes the sync **explicitly bidirectional and non-oscillating**: `consoleUiStore` keeps a non-reactive shadow `lastSeenActiveConsole`; `setFocused()` writes `focused`, the shadow, and `labStore.activeConsoleTab` together, while `syncFromLabStore()` reacts **only** when `labStore.activeConsoleTab` differs from the shadow (i.e. someone else changed it) — adopting it as `focused` and, in a tiled layout, ensuring it is tiled by evicting the least-recently-focused non-pinned tile. The single call site is one `$effect` in `App.svelte` (§8.3), so `labStore` still shows **zero diff**. |
| 7 | **Contradictory ownership of pane/mark lifecycle.** `tiles`/`focused`/`pinned`/marks live in `consoleUiStore`, but their reset depends on lab-switch code in `labStore` (plan:777, `labStore.svelte.ts:528`) — while Batch 9's own acceptance gate forbids any `labStore` diff (plan:958). Stale panes and marks would survive a lab switch with no code path to clear them. | §8.2 removes the dependency instead of the gate: `consoleUiStore` gains **`reconcile({labId, consoles, captures})`**, driven by one `$effect` in `App.svelte` that reads `labStore.lab.id`, `labStore.openConsoleTabs`, `labStore.openCaptureTabs`. A changed `lab.id` clears `tiles`/`focused`/`pinned`/marks/delivery counters wholesale; an unchanged id prunes any pane or mark whose node/link is no longer in the open lists. This covers **all five** places `labStore` clears tab state today (`:323-327`, `:535-540`, `:891-892`, `:906-907`, `:1062-1063`) without knowing about any of them, and is robust to p5 adding a sixth. The gate stays as written and gains an explicit, narrow `App.svelte` allowance (§8.9). |
| 8 | **The mark record has no stream position.** `{id, wall, label}` (plan:870) cannot tell Batch 7 which packet boundary to divide at, and the plan itself (correctly) rejects wall-clock correlation. | §8.5's record becomes `{id, wall, label, capturePos: Record<linkId, number>}`, where each value is the **per-link delivery-sequence** value at mark time — the same counter finding 4 assigns to `LensEvent.seq`. A divider goes immediately before the first event whose `seq >= capturePos[linkId]`, which is exact and clock-free. `wall` is demoted in writing to label text and mark-vs-mark ordering only, and a mark for a link with no live counter renders as "position unknown" rather than guessing. |
| 9 (minor) | The `active`-gated-resize diagnosis was overstated: both terminals already refit via `ResizeObserver` and `ConsoleTerm` already sends NAWS (`:91`, `CaptureTerm:141`), so "tile four consoles and three never fit" was not established. | Softened everywhere it appeared — §1.2 (idea #9 bullet), §4.6, §8.1.3, §8.8 and §10.2.2 now claim only what is established: the `active` gate exists, it is a real hole for a visible-but-unselected pane on the *dock-side* effect, `term.focus()` sharing that flag is a real focus fight, and the `visible`/`focused` split is the right ownership regardless. The `ResizeObserver` safety net is now stated as a **fact in §3.2** rather than omitted, and §10.2.2 is reframed from "prove the bug is fixed" to "prove every tiled pane wraps at its own width", which is the claim the batch actually makes. |

## 2. Relationship to prior plans, and the baseline these line numbers are against

- **`docs/learning-features-gui-ideas-plan.md` is the source design doc.** Its locked
  intent is carried forward and not re-litigated: the Lens sits **next to** the existing
  capture view rather than replacing it; the interface suggestion is **read-only against
  existing link state, no new schema**; the console workspace **extends the existing dock**
  rather than introducing a new window system. Where the doc's *implementation* guidance
  contradicts the code, §4 records the correction and the evidence.
- **Idea #10 (floating console windows) is not in this plan, and §4.6 / §8.6 exist to keep
  Batch 9 from foreclosing it.** The idea doc already designs #10 as a third
  `DockSide` value (`"bottom" | "right" | "floating"`) with per-window `{x,y}` state, a
  drag-move primitive, z-stacking and per-node position persistence. Idea #9 is a
  *layout* feature on the same panes. They are one axis apart, and §8.2's pane model is
  chosen specifically so #10 can consume it as window identity later.
- **`docs/p5-netprobe-netsvc-impairment-plan.md` is partially IN FLIGHT in the working tree,
  uncommitted.** `git status` shows 36 modified files; `git log` HEAD is `25a1e05`. Batch C
  (link faults) has landed (`lab/LinkFault` in `labTypes.ts:75`, `server/fabric_fault.go`,
  `server/fabric_fault_handlers.go`, `protocol.EventLinkFault` at `message.go:71`) and Batch
  A (the `pc` node kind) is mid-implementation (`lab.go:57 KindPC`,
  `labTypes.ts:6` now `"iol" | "vpcs" | "nat" | "tool" | "pc"`,
  `message.go:65 EventNodePCState`, `interfaces.ts:13,48`, `InterfacePicker.svelte:90-91,
  105-106`). **Every line number in §3 is against the working tree, not HEAD.** Two
  consequences that are not optional:
  1. **Re-grep before editing.** `InterfacePicker.svelte` and `labStore.svelte.ts` are both
     modified files that Batch 8 and Batch 9 edit.
  2. **Batch 8 must not "fix" the `pc` arms** it will find in `InterfacePicker.svelte:90-91`
     and `:105-106`. They are p5's, they are correct, and touching them creates a conflict in
     someone else's in-flight change.
- **Standing posture, unchanged:** no Docker, no DB, no separate web server, one static Go
  supervisor binary, lightweight. Batch 9 proposes the plan's only new npm dependency and
  §8.4 makes its necessity, its verification, and its fallback explicit rather than assumed.

---

## 3. Facts established by reading the code (do not re-derive)

### 3.1 The console dock, as it exists today

`app/src/lib/components/Console.svelte` (639 lines) is the whole dock. It is mounted twice in
`App.svelte` — once inside a vertical `SplitPane` when `dockSide === "bottom"`
(`App.svelte:88-98`) and once inside a horizontal one when `"right"` (`:112-122`), gated on
`showConsole` (`:33-35`, true when either tab list is non-empty).

- **Two tab lists, two different owners.** Node-console tabs come from
  `labStore.openConsoleTabs` / `labStore.activeConsoleTab` (`Console.svelte:163-177`).
  Capture tabs come from `labStore.openCaptureTabs`, but their **selection is
  component-local**: `let activeCapture = $state<number | null>(null)` at `Console.svelte:11`.
  `selectCapture` (`:33-36`) sets `activeCapture` and nulls `labStore.activeConsoleTab`;
  `selectConsole` (`:37-40`) does the inverse. **There is no single "which pane is
  selected" value anywhere** — the pair `(labStore.activeConsoleTab, activeCapture)` encodes
  it, and one half of that pair does not survive `Console.svelte` unmounting.
- **Every open tab is already mounted and live.** `.term-slot { position: absolute; inset: 0 }`
  (`:528-531`) and `.term-slot.hidden { visibility: hidden; pointer-events: none }`
  (`:539-542`). The `{#each}` blocks at `:264-277` and `:278-344` render **every** open tab;
  the inactive ones are hidden with CSS, not unmounted. Their WebSockets stay connected and
  their xterm instances keep receiving. This is load-bearing for §8.1: tiling adds no process,
  socket, or component lifecycle work.
- **Tool nodes are an iframe, not a terminal.** `isToolNode` (`:118-120`) swaps
  `ConsoleTerm` for `<iframe src={/tool/${nodeId}/}>` (`:266-275`).
- **Capture tabs carry a "native Wireshark" overlay** driven by a second component-local map,
  `nativeCapture` (`:21`, flipped at `:54-56`), plus the one-shot
  `labStore.wiresharkOverlayFor` signal consumed by the `$effect` at `:63-69`. The overlay
  offers `labStore.downloadCapture(linkId)` and the `wireshark -k -i TCP@host:port` command
  (`:82-93`).
- **Dock actions** (`:208-258`): the console address chip, the A−/A+ font control, the
  colorize toggle, the dock-side toggle, and close-all. This row is where a layout control
  belongs (§8.3).

### 3.2 The two terminal renderers

- **`ConsoleTerm.svelte`** (205 lines). Constructs `new Terminal({convertEol, fontFamily,
  fontSize: consoleUiStore.fontSize, lineHeight, cursorBlink, theme})` at `:39-47` — **note
  what is absent: `scrollback` is never set**, so xterm's default applies. Loads exactly one
  addon, `FitAddon` (`:48-49`). Real transport at `:55-82` (`ConsoleTransport` +
  `ConsoleColorizer`); a mock shell at `:83-89`.
  **A `ResizeObserver` on the container is installed at `:91-95`, gated on nothing**: any real
  box-size change refits **and** re-sends NAWS, for every mounted terminal, selected or not.
  `CaptureTerm.svelte:141-143` has the same observer (fit only — a capture pane has no NAWS to
  send). This fact bounds §4.6's claim and is why the review downgraded it: a pane that is
  genuinely visible and genuinely changes size *is* refitted today. What the observer cannot
  cover is a pane whose box changes while it is out of flow — no observation fires, and a 0×0
  measurement is meaningless — which is why §8.3 adds an explicit refit on the untiled→tiled
  transition plus a zero-box guard.
  Four `$effect`s matter here:
  - `:104-109` — `if (active) { queueMicrotask(fit); term.focus(); }`. **Both fit and focus
    are gated on the same flag.**
  - `:114-121` — refit + `sendResize` when `consoleUiStore.dockSide` flips, **`if (!active)
    return`**.
  - `:124-127` — theme repaint.
  - `:132-140` — font-size change → `fit()` + `sendResize`, **not** gated on `active` (so
    font changes reach every open terminal, including hidden ones).
  `onDestroy` (`:98-102`) disposes the terminal: **closing a tab destroys its scrollback.**
- **`CaptureTerm.svelte`** (211 lines). Owns the capture WebSocket (`CaptureTransport`,
  `:110-130`) and the pcapng parser. `disableStdin: true`, fixed `fontSize: 12` (`:92-100`) —
  it does **not** follow `consoleUiStore.fontSize`. A **fresh `PcapngParser` per connection**
  (`:23`, and again in `onOpen` at `:111-113`) because every reconnect restarts the stream at
  its SHB. `onData` (`:115-127`) parses once and does two things with the result:
  `labStore.appendCapturePackets(linkId, pkts)` and `writePacket(pkt)` per packet. Its
  `$effect` at `:151-153` gates `fit()` on `active`, same trap as ConsoleTerm.
  `writePacket` (`:169-177`) renders `index | +tRel | addr | proto | len | info` from
  `summarize()`.

### 3.3 `consoleUiStore.svelte.ts` — the prefs precedent

134 lines, a rune-backed singleton exported at `:133`. Four prefs, each with its own
localStorage key declared at `:11-14` (`iolbox.console.dockSide`, `.colorize`, `.mode`,
`.fontSize`), each with an `initialX()` reader that swallows a localStorage failure
(`:27-62`), each with a `setX`/`toggleX` pair that writes through (`:79-130`).
`DockSide = "bottom" | "right"` at `:5`. Any new console-workspace pref follows this shape
exactly: one key constant, one guarded initializer, one setter that persists. **Nothing in
this store is per-lab or per-node today** — every value is a global view pref, which is why
§8.2 keeps tile *membership* out of it.

### 3.4 `labStore.svelte.ts` — console and capture tab state

- `openConsoleTabs = $state<number[]>([])` (`:72`), `activeConsoleTab = $state<number|null>`
  (`:73`), `openCaptureTabs = $state<number[]>([])` (`:123`),
  `capturePorts = $state<Record<number, number>>({})` (`:128`),
  `wiresharkOverlayFor` (`:134`), `captureRecorded` (`:755`).
- `openConsole` (`:951-955`) appends + selects; `closeConsole` (`:1005-1013`) removes and
  re-selects `openConsoleTabs[0] ?? null`; `closeAllConsoles` (`:1015-1018`) clears consoles
  and calls `closeCapture` for every capture tab.
- `openCapture` (`:724-742`) does four things: sets `link.capture = {enabled:true,
  mode:"live"}` **in the lab doc**, appends the tab, calls `client.captureStart`, and drops
  the recording buffer. `closeCapture` (`:806-823`) is symmetric and **withdraws the doc-level
  capture intent** (`:817-821`) so lab start does not re-arm a capture the user closed.
- **The capture recording buffer is a plain non-reactive `Map`** (`:752`) of *parsed* packets,
  capped at 64 MiB (`:753`, `CAPTURE_BUF_CAP`), appended by `appendCapturePackets`
  (`:760-773`), re-serialized on download by `encodePcapng` (`:785`). The comment at
  `:744-751` explains why parsed packets and not raw bytes: the stream spans reconnects.
- **Tab state resets in at least three places** — `:314-319` (lab load), `:501-506` (reset),
  `:852-857` / `:866-867` (lab switch) — **and review found more**: against the current working
  tree the clears are at `:323-327`, `:535-540`, `:891-892` (`forceClean`), `:906-907`
  (`stopLab`) and `:1062-1063` (`closeAllConsoles`), i.e. **five**, with p5 still in flight.
  The draft's plan of "any new per-tab state must reset in the same places" is therefore a
  design that is one merge away from being wrong, and §8.2b replaces it with a reconciler that
  observes the tab lists instead of hooking their clears (§1a finding 7).

### 3.5 The capture pipeline, end to end

1. `bcap.Start(bridgeName, bind, port)` (`bcap/capture_linux.go:36-56`) starts a pcapng TCP
   server and launches **`sudo -n tcpdump -i <bridgeName> -w - -U -s 0 -n`** (`:45`). Two
   facts from that argv: the capture point is **the link's Linux bridge**, not a per-endpoint
   tap, and **`-s 0` means full frames, untruncated**.
2. Frames stream through `parsePcapStream` → `pcapngServer.Broadcast` (`bcap/bcap.go:57,188`),
   re-serialized by `relay.PcapngWriter` (`relay/pcapng.go:33,103`).
3. `wsbridge` exposes `GET /capture/{linkId}` (`wsbridge.go:145`, handler `:555-590`,
   one-directional pump `bridgeCapture` `:602-644`), dialing the link's capture port.
4. The browser's `CaptureTransport` connects, `PcapngParser` (`pcapng.ts:45-176`) yields
   `ParsedPacket{index, tRel, tsMicros, data, origLen}` (`:25-37`).
   - `tRel` is **relative to the first packet of the current stream** (`:170-171`, `t0`), so
     it **resets to 0 on every reconnect**.
   - `tsMicros` is **absolute microseconds since the Unix epoch** (`:174`), preserved
     precisely so the download can re-serialize faithful timestamps.
5. `encodePcapng` (`:197-255`) rebuilds one clean section for the download.

**There is no libpcap and no cgo anywhere in this path** — `bcap` shells out to `tcpdump`.
The idea doc's "no libpcap/cgo added" is a description of the status quo, not a design
constraint anyone is at risk of violating.

### 3.6 What already parses protocols — and it is two implementations, not zero

| where | entry point | decodes |
|---|---|---|
| browser | `pcapng.ts:361` `summarize(frame, origLen)` | Ethernet II + one 802.1Q hop (`:371-374`), 802.3/LLC (`:380-382`, `:435-474`), **STP/RSTP/MST** incl. root id + path cost (`:481-502`), **SNAP → CDP** incl. Device ID TLV (`:509-530`), DTP, VTP, **ARP** with `Who has …? Tell …` prose (`:397-408`), IPv4 (`:532-540`), IPv6 (`:542-549`), TCP flags (`:348-358`), UDP, ICMP/ICMPv6 named types (`:325-342`), and a numeric-proto table naming **OSPF/EIGRP/GRE/ESP/AH/PIM/VRRP/IGMP** (`:579-590`) |
| supervisor | `relay/classify.go:38` `ClassifyDetailed(frame)` | the same families, plus **subtypes**: ARP request/reply (`:106-117`), OSPF hello/db-desc/ls-request/ls-update/ls-ack (`:171-189`), EIGRP (`:194-212`), ICMP split into PING vs ICMP with named subtypes (`:231-250`), BGP message types (`:313-330`), STP BPDU types (`:387-400`), plus port-based RIP/VXLAN/RADIUS/TACACS/BGP (`:356-378`) |

**Neither one decodes DHCP or HSRP.** `classify.go:367-378` `udpPort` covers only 520, 4789
and the four RADIUS ports; `pcapng.ts:567-571` renders any UDP frame as `UDP src:port >
dst:port` with no info string. The supervisor's one real DHCP implementation,
`extnet/dhcp.go` (`DecodePacket` `:100`, `Encode` `:154`, `Handle` `:220`, `leaser` `:263`),
serves the NAT gateway and is a **server**, not a sniffer — it is reached through
`extnet/dhcp_linux.go:70 parseDHCP` on the NAT tap only.

**Neither one keeps the VLAN id.** `classify.go:22` returns `tagged bool`; `pcapng.ts:371-374`
reads the tag only to advance past it.

### 3.7 `dirstat` — the only place endpoint attribution exists

`supervisor/internal/dirstat/dirstat.go:1-19` is the package doc, and its first sentence is
the fact Batch 7 is built on:

> "…the always-on, header-only, per-endpoint-tap directional traffic classifier for fabric
> links. It answers the question a tcpdump-on-bridge capture can't: which endpoint SOURCED
> each frame."

Mechanism (`dirstat_linux.go`):
- `Open(devs)` (`:45-87`) binds one raw `AF_PACKET`/`SOCK_RAW`/`ETH_P_ALL` socket per endpoint
  tap device, in **doc endpoint order** (`:35-36`), and **breaks at `ep > 1`** (`:53-55`) —
  a link is assumed to have exactly two endpoints.
- `readLoop` (`:117-136`) reads at most **`snapLen = 128` bytes** (`:21`) per frame, drops
  `PACKET_OUTGOING` frames as the peer's mirror (`:130-132`), and calls
  `relay.ClassifyDetailed(buf[:n])` → `c.count(ep, label, subtype)`.
- A device that cannot be bound is skipped; **on a non-root dev box every bind fails and
  `Open` returns a nil `*Classifier`** (`:39-44`), and the caller degrades to aggregate-only
  stats. Attribution is therefore **not guaranteed to exist**.
- Counters only — `Counters map[key]uint64` keyed by `{ep, label, subtype}`
  (`dirstat.go:27-37`). **There is no per-packet event path.**

Wired in at `server/fabric_linux.go:594` (`openLinkDirstat`, called from `:433` and `:580`),
snapshotted in `fabricStats` at `:831,865`, and surfaced as `LinkStatsData.ProtosDir` /
`ProtosSubtypeDir` (`protocol/verbs.go:562-590`), whose doc comment states the ordering
contract: `[fps sourced from endpoint 0, fps sourced from endpoint 1]`, endpoint order =
doc endpoint order.

### 3.8 The Network Watcher — the adjacent feature the Lens must not duplicate

`app/src/lib/watcherStore.svelte.ts` (246 lines) already ships a protocol-filter UI:
`ProtoKey` union (`:16-34`), `LABELS` mapping each key to backend label sets (`:36-55`),
`PROTO_ORDER` (`:59-62`), `SUBTYPES` mirroring `ClassifyDetailed`'s subtype vocabulary
(`:71-79`), up to `MAX_ROWS = 4` colored rows (`:108`, `:137`), and `matchFor(stats)`
(`:212-243`) which drives animated directional dash overlays on the canvas edges.

Its doc comment at `:64-70` is a constraint on Batch 7: *"The strings must match the backend
byte-for-byte — they key into `protosSubtypeDir[label][subtype]`."* Adding a `dhcp` or `hsrp`
key to `LABELS` without a matching backend label would create a row that silently never
matches.

Division of labour, decided: **Watcher = rates, on the canvas. Lens = individual events, in
the dock.** They share a protocol *vocabulary* (§7.5) and nothing else.

### 3.9 The interface picker and interface enumeration

- `app/src/lib/interfaces.ts` (49 lines): `allInterfaces(node)` (`:9-24`) — one `eth0` for
  vpcs/nat (`:12`), one `eth1` for tool/pc (`:13`), otherwise `e<a>/<p>` × 4 per ethernet
  group and `s<a>/<p>` × 4 per serial group (`:17-22`). `usedInterfaces(nodeId)` (`:27-35`) —
  **this is where "has no existing link yet" already lives**: it walks `labStore.lab.links`
  and collects every `endpoint.interface` on that node. `freeInterfaces` (`:38-41`) =
  all minus used. **`nextFreeInterface(node)` (`:44-49`) = `freeInterfaces()[0]`, with a
  first-interface fallback.**
- `app/src/lib/components/InterfacePicker.svelte` (196 lines). Its own header comment
  (`:2-5`) already says: *"Two selects … listing each node's FREE interfaces, pre-selecting
  the next free one."*
  - `options(node)` (`:29-33`) returns `{iface, used}[]` over **all** interfaces;
    `optsA`/`optsB` are `$derived` (`:34-35`) so they track `labStore.lab.links`.
  - **`onMount` at `:39-42` sets `ifA = nextFreeInterface(nodeA)` and `ifB =
    nextFreeInterface(nodeB)`.** The pre-selection the idea doc asks for is already here.
  - The `<select>`s (`:95-99`, `:110-114`) render every option with `disabled={o.used}` and
    a `" (used)"` suffix.
  - `noFreeA`/`noFreeB` (`:44-45`) drive a red "no free ports" state; `canConnect` (`:46`)
    gates the Connect button; `confirm()` (`:62-75`) builds the `LabLink` and calls
    `labStore.addLink` + `client.linkAdd`.
- **Who calls it:** `CanvasInner.svelte` owns the rubber-band link drag. `startLinkDrag`
  (`:347-356`), `onLinkMove` (`:358-368`), `onLinkUp` (`:370-381`) — and on a valid drop,
  `:379` sets `ifPicker = {x, y, sourceId, targetId}`, rendered at `:1013-1020`. **The canvas
  passes only two node ids and a screen position; it computes no candidate interfaces at
  all.** Every interface decision is inside the picker.
- `labTypes.ts`: `LabEndpoint {node, interface}` (`:48-53`), `LabLink {id, type?, endpoints,
  capture?, fault?}` (`:55-70`ff after p5's in-flight edits). **There is no per-node interface
  object and no "linked" flag** — "has no existing link" is a derived fact, computed by
  `usedInterfaces` on every read, and that is the correct and only place it lives.

### 3.10 xterm, as vendored today

`app/package.json` dependencies: `@xterm/xterm ^6.0.0` and **`@xterm/addon-fit ^0.11.0` —
that is the complete xterm surface.** `ls app/node_modules/@xterm` confirms exactly two
packages installed. **`@xterm/addon-search` is not present.** `docs/build.md:47` specifies
`npm ci`, i.e. an exact-lockfile install, so adding a dependency means updating
`package-lock.json` in the same change. `node_modules/` is gitignored (`.gitignore`), nothing
is vendored, so the build host must be online for `npm ci` — which it already must be.

### 3.11 What idea #10 has already claimed, that Batch 9 must not take

`docs/learning-features-gui-ideas-plan.md` `## Floating console windows (idea #10, design
detail)` locks: a third `DockSide` value `"floating"`; a new `FloatingConsoleWindow.svelte`
wrapping **the same unchanged `ConsoleTerm.svelte`**; drag-move via the
`pointermove`/`pointerup` pattern from `AnnoLine.svelte`/`AnnoShape.svelte`; resize
generalizing `SplitPane.svelte`'s drag math to two axes; a shared z-index counter;
**per-node-id position/size persistence in localStorage**; and viewport clamping. It closes
by naming idea #9 explicitly: floating mode "complements idea #9's tiled-consoles-inside-one-
dock approach rather than replacing it; the two aren't mutually exclusive".

Two hard constraints fall out (§8.6): Batch 9 **must not** add a third `DockSide` value, and
Batch 9's per-pane identity **must** be a value idea #10 can reuse as a window key.

---

## 4. Where the idea doc's design does not survive the code — six corrections

### 4.1 (#7) The parser is not missing. It exists twice. What is missing is an *event path*

The doc's fit note: *"Pure-Go parsing of a known protocol subset in the supervisor — no
libpcap/cgo added."* Read against §3.5 and §3.6, every clause of that is already true and
already built: there is no libpcap and no cgo (`bcap` shells out to `tcpdump`), and a Go
protocol classifier covering ARP/STP/CDP/OSPF and their subtypes has been in
`relay/classify.go` for some time, feeding the Watcher.

**Correction (decided):** Batch 7 writes **no new general-purpose classifier**, in Go or in
TypeScript. It (a) reuses the browser dissection that already runs on every captured packet
(`pcapng.ts:361`), refactored to expose its intermediate fields instead of only a formatted
string; (b) adds **only** the two decoders §3.6 proves are genuinely absent — DHCP and HSRP
(§7.4); and (c) adds the one thing neither implementation has, which is per-packet
**attribution** (§7.3). Reimplementing `summarize` as a second Go dissector in the supervisor
would produce a third parser, a third place for STP offsets to drift, and a new event stream
to rate-limit — for output the browser can already compute from bytes it already has.

### 4.2 (#7) "R2 sent …" cannot come from the capture stream

The doc's own example strings — *"R2 sent OSPF Hello"*, *"DHCP Offer 10.1.1.12"* — both name
a **sender**. The capture point is the bridge (`bcap/capture_linux.go:45`), the pcapng carries
no `sll_pkttype` and no ingress-device metadata, and `dirstat/dirstat.go:3` says so in as many
words. The link's doc endpoint order does not help: a frame crossing an IOL **switch** was
originated by a device that is not an endpoint of this link at all.

**Correction (decided):** attribution is a **supervisor-supplied per-endpoint MAC
attribution**, produced by a small addition to the loop that is already reading those exact
bytes — `readLoop` already has `buf[6:12]` in hand inside its 128-byte snaplen
(`dirstat_linux.go:118-133`). See §7.3 for the channel, the lifecycle, and — the part that
matters — the **never-guess** degradation. An unattributed event prints the MAC. It never
prints a node name it has not been told.

**The first draft of this correction was itself wrong, and the way it was wrong is the whole
lesson (§1a finding 1).** It proposed "learn every source MAC seen on the tap, first-wins,
capped at 8 per endpoint". That does not degrade on the switch case — it *fails confidently*
on it. Point a tap at an IOL switch and the first eight MACs it learns are eight **downstream
hosts**, so the Lens renders "SW1 sent …" for traffic SW1 merely forwarded. The never-guess
guarantee was written into §7.1.4 and then defeated by the mechanism meant to implement it.
The corrected rule inverts the default:

> **An endpoint is attributable only while exactly one distinct source MAC has ever been
> observed on its tap.** The second distinct MAC is not another entry in a set — it is
> *evidence the endpoint is a relay*, and it flips that endpoint to `ambiguous`, which
> attributes **nothing at all** (not even the first MAC, which may itself have been a
> forwarded one).

A directly-attached router, host, VPCS, PC or NAT endpoint sources exactly one MAC per
interface and is attributed. A switch, a bridge, a hypervisor uplink, or anything else
forwarding for others sources many, is detected on its second frame from a new source, and
attributes nothing thereafter. The lossy case — a switch endpoint whose *own* BPDU/CDP frames
also lose their name — is the correct trade: the Lens's job is naming the directly attached
node, and "shows a MAC" is a recoverable disappointment while "names the wrong node" is a
learner reading a false topology. §7.3 specifies the aging and cross-endpoint-conflict rules
that keep this honest over a long session.

Two alternatives were considered and are **rejected**, so they are not re-proposed:
- *Derive IOL MACs from the `aabb.cc00.*` convention.* The convention appears only in a doc
  comment in `painter/stp.go:40,182-186`; nothing in this repo computes or stores a node MAC.
  It would also cover IOL only — VPCS gets its MAC from vpcs itself (`node/argv.go:150-157`
  documents the `-m` uniqueness fix), and tool/NAT/pc endpoints get kernel-assigned veth MACs.
- *Learn attribution in the browser from CDP Device IDs.* `pcapng.ts:509-530` does pull the
  CDP Device ID, but it is a hostname, not a node id, it is absent unless CDP is enabled, and
  a wrong mapping is worse than none.

### 4.3 (#7) "Click-to-filter by VLAN" has no VLAN id to filter on

`classify.go:22,38` return `tagged bool` and nothing more; the Watcher's `dot1q` key
(`watcherStore.svelte.ts:54`) is therefore a *tagged/untagged* filter, not a per-VLAN one.
`pcapng.ts:371-374` peels one 802.1Q tag and discards the VID.

**Correction (decided):** Batch 7 extracts the 12-bit VID browser-side in the same peel it
already performs (§7.4). No supervisor change, no Watcher change. **Only one tag is peeled**
— Q-in-Q shows the outer VID, matching the existing single-hop behaviour of both parsers, and
the UI says "outer VLAN" rather than implying full stack decode.

**Review confirmed this analysis and it is not to be revisited (§1a).** In particular:
a single 802.1Q tag does **not** by itself break MAC-based attribution — the source MAC sits
at `frame[6:12]`, ahead of the tag, so `dirstat`'s `buf[6:12]` read (§7.3) and the browser's
`srcMac` are both tag-independent. The two real holes in attribution are **forwarded/relayed
source MACs** (§4.2, §1a finding 1) and **endpoint-index compaction** (§7.3, §1a finding 2),
and both are fixed there. Do not "harden" the VLAN path against an attribution problem it
does not have.

### 4.4 (#7) One WebSocket per link, not one per view

The Lens and the capture summary are two renderings of the same byte stream. Today
`CaptureTerm.svelte:110-130` **owns** the `CaptureTransport` for its link. A Lens component
that opened its own would give the tee two clients, double the browser-side parse, and
produce two independently-reconnecting streams with independent `tRel` origins.

**Correction (decided):** capture-stream ownership moves out of `CaptureTerm` and into
`labStore` (§7.2) — one `CaptureTransport` + one `PcapngParser` per link, created by
`openCapture()` and torn down by `closeCapture()`, with renderers subscribing. This is the
single structural refactor in Batch 7 and it is not optional: it is the only way to have two
views of one stream without two sockets. Its regression risk (the download buffer, the
reconnect/SHB reset, the idle hint) is covered in §7.7.

### 4.5 (#8) The picker already pre-selects the next free interface

The idea row asks for the picker to "pre-select/highlight the next interface that has no
existing link, instead of listing all interfaces flat". `InterfacePicker.svelte:39-42` already
calls `nextFreeInterface` for both ends; `interfaces.ts:27-41` already computes "has no
existing link" from `labStore.lab.links`; `:97` and `:113` already mark used options
`disabled` with a `" (used)"` suffix. The component's own header comment (`:2-5`) has said so
since R2.1.

**Correction (decided):** Batch 8 is **not** a selection-logic change. `interfaces.ts` is
**not modified at all**. The residual gap is a legibility gap, and it is real: the
pre-selected value is indistinguishable from a value the user chose, the list is still a flat
mixture of free and used ports (an IOL node with 4 ethernet groups renders 16 options with no
visual grouping), and nothing tells the user *why* `e0/1` is selected. Batch 8 fixes exactly
that, in one file (§6). **If the implementing agent's diff touches a second file, the scope
was misread.**

### 4.6 (#9) Tiling costs nothing structurally — and `active` costs everything

Two halves, and the idea doc gets the first one right for the wrong reason and misses the
second entirely.

*Cheaper than stated:* "2-4 tiled consoles" needs **no** change to component lifecycle,
transports, or terminal creation, because §3.1 establishes that every open tab is already
mounted and merely `visibility: hidden`. Tiling is a change to `.term-area`'s layout and to
which slots get the `hidden` class.

*Misowned, and — after review — **not** proven broken:* `ConsoleTerm.svelte:104-109` and
`:114-121` gate `fit()`/`sendResize()` on `active`, and `CaptureTerm.svelte:151-153` gates
`fit()` on it too, where `active` today means *selected*. The first draft concluded from this
that "tile four consoles and three never fit". **That conclusion was overstated and is
withdrawn (§1a MINOR 1).** §3.2 records why: both components install an ungated
`ResizeObserver` (`ConsoleTerm.svelte:91-95`, `CaptureTerm.svelte:141-143`) that refits — and,
in `ConsoleTerm`, re-sends NAWS — on any real box-size change, for every mounted terminal.
Moving a visible pane into a grid cell changes its box, so the observer would very likely
have covered it.

What **is** established, and is enough to justify the same design:
- **`term.focus()` shares the flag** (`:108`). Widening `active` to mean "visible" would make
  every tiled terminal grab focus on every layout change. Fit and focus must not be one prop.
- **The dock-side effect's `if (!active) return` (`:117`) is a genuine hole** for a pane that
  is visible but not selected: a `bottom`↔`right` flip that happens to leave a pane's pixel
  box unchanged sends it no NAWS, and the effect exists precisely because that flip is the
  case the observer is least trusted for.
- **The untiled→tiled transition is not observable at all** under §8.3's `display: none` rule
  (§1a finding 5): an out-of-flow element has no box for a `ResizeObserver` to report, and a
  0×0 measurement must never be fitted against.

**Correction (decided):** the prop splits into **`visible`** (drives `fit()` + `sendResize`,
plus an explicit rAF refit on becoming visible and a zero-box guard) and **`focused`** (drives
`term.focus()`), threaded from the new pane model (§8.2). This is the first thing to implement
in Batch 9. **State it in the PR as an ownership fix, not as a bug fix** — claiming it repaired
a proven wrap bug would be a claim §10.2 cannot substantiate on a pre-change baseline.

---

## 5. Batch ordering and independence

| batch | idea | files touched | shares files with |
|---|---|---|---|
| **§6** | #8 next-free-interface | `app/src/lib/components/InterfacePicker.svelte` | nothing |
| **§8** | #9 console workspace | `Console.svelte`, `ConsoleTerm.svelte`, `CaptureTerm.svelte`, `consoleUiStore.svelte.ts`, `App.svelte` (the `bindConsoleSelect` registration + one reconciler `$effect`, §8.3), + `package.json`/`package-lock.json` | §7 (`Console.svelte`, `CaptureTerm.svelte`, `consoleUiStore.svelte.ts`) |
| **§7** | #7 Protocol Lens | `Console.svelte`, `CaptureTerm.svelte`, `labStore.svelte.ts`, `consoleUiStore.svelte.ts` (moves the delivery-counter call site, §7.5), `pcapng.ts`, new `lens.ts` + `LensPane.svelte`, `dirstat*.go`, `fabric_linux.go`, `protocol/verbs.go`, `docs/protocol.md` | §8 |

Merge order **8 → 9 → 7**. §6 may merge at any time.

---

## 6. Batch 8 — next-free-interface suggestion, made visible (idea #8)

**Review status: confirmed, unchanged.** `codex sol-medium` independently verified §4.5's
central claim — the preselection genuinely already exists (`InterfacePicker.svelte:39` →
`interfaces.ts:27`, `:43`; used options already `disabled` at `InterfacePicker.svelte:95`) —
and returned **no finding** against this batch. Nothing below was revised in response to the
review, and the one-file scope is now doubly attested: **if the diff touches a second file,
the scope was misread.**

### 6.1 Decisions locked

1. **`app/src/lib/interfaces.ts` is not modified.** `usedInterfaces`/`freeInterfaces`/
   `nextFreeInterface` are shared with the Node Edit dialog (`interfaces.ts:1-3`) and already
   answer the question correctly. No new schema, no new store field, no new derived state in
   `labStore` — the idea row's "reads existing link state only" is satisfied by code that
   exists.
2. **The suggestion is named, not just applied.** The picker computes
   `const suggestA = $derived(nodeA ? nextFreeInterface(nodeA) : "")` (and `suggestB`) —
   `$derived`, not a second `onMount` read, so it tracks `labStore.lab.links`. `onMount`'s
   assignment of `ifA`/`ifB` (`:39-42`) is **kept exactly as is**: the initial value must not
   be reactive or it would stomp a user's choice when any link changes anywhere.
3. **Options are grouped, not reordered.** Two `<optgroup>`s — `Free` then `In use` — inside
   each `<select>`. Interface order **within** each group stays `allInterfaces` order
   (`e0/0, e0/1, …`), because a router's port order is the mental model and re-sorting it to
   float the suggestion to the top would be actively confusing. Used options keep
   `disabled` and drop the now-redundant `" (used)"` suffix (the group label says it).
4. **The suggested option is labelled inline**: the option whose value `=== suggest` renders
   `` `${iface} · next free` ``. One string, no icon, no color — it must survive both themes
   and a native `<select>` popup, which cannot be styled per-option reliably across browsers.
5. **A one-line hint sits under each select** when the current value equals the suggestion:
   *"`e0/1` — first port with no link"*, styled with the existing `.lab`/`.sub` type scale
   (`:143-147`, `:160-168`). When the user picks something else the hint switches to naming
   what would have been suggested, so the affordance stays discoverable.
6. **The fixed-interface branches are untouched.** `:90-91` and `:105-106` (vpcs/nat/tool/pc,
   one port each, currently mid-edit by p5) get no grouping and no hint — there is nothing to
   choose. **Do not refactor them.**
7. **No behavioural change to `confirm()`, `canConnect`, `noFreeA/B`, or the popover's
   position clamping.** A diff touching `:44-79` is out of scope.

### 6.2 Concrete file-level changes

**`app/src/lib/components/InterfacePicker.svelte`** — the only file.
- Import is unchanged (`nextFreeInterface` is already imported at `:8`).
- Add `suggestA`/`suggestB` `$derived` beside `optsA`/`optsB` (`:34-35`).
- Change `options()` (`:29-33`) to return `{free: string[], used: string[]}` **or** keep it
  returning `{iface, used}[]` and partition in the template — implementer's choice, but the
  partition must be computed once, not with two `.filter()` calls per render inside `{#each}`.
- Replace each `<select>`'s single `{#each}` (`:96-98`, `:111-113`) with two `<optgroup>`
  blocks, the free group carrying the `· next free` label on the matching option.
- Add the hint `<div class="hint">` under each select, plus one `.hint` style rule reusing
  `var(--fs-xs)` / `var(--ink-3)` as `.sub` does (`:143-147`).

Expected diff: **~45 lines in one file.** Nothing else.

### 6.3 Testing bar

`app/` has **no test runner** — `package.json` scripts are `dev`, `build`, `build:embed`,
`preview`, `check`, `tauri`. There is no vitest, no jsdom, no `*.test.ts` in `app/src`.
Stating that plainly is part of the bar: **this batch cannot be unit tested in this repo as it
stands, and adding a test runner is out of scope.** The bar is therefore:

1. `cd app && npm run check` — green, zero new diagnostics.
2. Manual, in the dev mock (`npm run dev`), recorded in the PR as a short checklist:
   - Drag from a fresh IOL node to another fresh IOL node → both selects open on `e0/0`,
     labelled `e0/0 · next free`, both hints visible, `In use` group **absent** (no used
     ports yet — an empty `<optgroup>` must not render).
   - Wire `e0/0`, then drag again between the same pair → selects open on `e0/1 · next free`;
     `e0/0` now appears under `In use`, disabled.
   - A node with `ethernet: 2, serial: 1` → 12 options across the two groups, ordering
     `e0/0…e0/3, e1/0…e1/3, s0/0…s0/3` preserved inside each group.
   - Change the selection away from the suggestion → the hint switches to naming the
     suggestion, and Connect still creates the link on the chosen interface.
   - A fully-wired node → unchanged "no free ports" state, Connect disabled.
   - A `vpcs`/`nat`/`tool` node → unchanged single fixed row, no groups, no hint.
3. **Regression grep:** `git diff --stat` shows exactly one changed file.

### 6.4 Acceptance gate

1–3 above, plus: `git diff app/src/lib/interfaces.ts` is **empty**, and
`git diff app/src/lib/components/InterfacePicker.svelte` contains **no change to lines
`:90-91` or `:105-106`** (p5's in-flight `pc` arms).

---

## 7. Batch 7 — Protocol Lens (idea #7)

### 7.1 Decisions locked

1. **The Lens is a third tab kind in the existing dock**, keyed by link id, rendered by a new
   `LensPane.svelte`. It is **not** a replacement for the capture tab and not a separate
   panel. Opening a Lens for a link opens (or reuses) that link's capture — one stream, two
   views, side by side under Batch 9's tiling.
2. **All dissection stays in the browser** (§4.1). The supervisor's only new output is the
   MAC→endpoint map (§7.3).
3. **Events are derived from the same parsed packets the capture buffer already receives.**
   No second socket, no second parse (§4.4, §7.2).
4. **Attribution is never guessed** (§4.2). Three display states, and the UI must be able to
   show all three: attributed (`R2`), unattributed-but-known-MAC
   (`aa:bb:cc:00:02:00`), and unavailable (`—`). The header carries a one-line reason
   distinguishing *"no classifier"* from *"this endpoint forwards for other devices"* (§7.6) —
   after review these are different facts with different lessons, and collapsing them into one
   "no attribution" message hides the single most instructive case the Lens has (§1a #1).
5. **Presets** are ARP, DHCP, STP, CDP, OSPF, HSRP — exactly the six the idea row names —
   plus "All". Multi-select, not radio.
6. **Filters are click-to-apply from the event rows themselves**: clicking a node chip filters
   to that node, clicking a VLAN chip filters to that VLAN, clicking a protocol chip toggles
   that preset. This is what "click-to-filter by node/VLAN" means concretely.
7. **Bounded, in-memory, session-only.** A ring of the last **2000** events per link,
   independent of the 64 MiB packet buffer (`labStore.svelte.ts:753`). No persistence, no lab
   doc field, no schema change.

### 7.2 Capture-stream ownership moves to `labStore` (the one refactor)

**Today:** `CaptureTerm.svelte` constructs `CaptureTransport` (`:110`), owns `parser`
(`:23`, reset in `onOpen` `:111-113`), calls `labStore.appendCapturePackets` (`:123`) and
renders (`:124`). `onDestroy` disconnects (`:145-149`).

**After:** `labStore` owns a private `captureSessions: Map<number, {transport, parser,
subscribers: Set<(pkts: ParsedPacket[]) => void>}>`.

- `openCapture(linkId)` (`:724-742`) additionally creates the session and connects.
- `closeCapture(linkId)` (`:806-823`) additionally disconnects and deletes it. Its existing
  behaviour — dropping the buffer, clearing `captureRecorded`, **withdrawing the doc-level
  capture intent** (`:817-821`), calling `client.captureStop` — is unchanged and must stay
  unchanged.
- The session's `onOpen` resets **the parser** (preserving `CaptureTerm.svelte:111-113`'s
  reason: every reconnect restarts at an SHB) and notifies subscribers of the reset so a
  renderer can re-emit its header.
- The session's `onData` parses once, calls the existing `appendCapturePackets` (unchanged),
  then takes the batch's first sequence number from
  `consoleUiStore.advanceCaptureDelivery(linkId, pkts.length)` (§7.5, §8.5), feeds
  `lens.push(linkId, pkts, firstSeq)`, then fans out to subscribers. **`CaptureTerm` must stop
  advancing the counter in the same commit** — it is the Batch 9 writer, and two writers
  double-count every delivery, which offsets every mark divider by a factor of two without
  any visible error.
- `CaptureTerm.svelte` becomes a pure renderer: subscribe on mount, unsubscribe on destroy,
  keep `writePacket`/`writeHeader`/`colorProto`/the idle-hint logic verbatim. **Its
  `retryNow` effect (`:163-167`) moves to the store** alongside the transport.
- The three tab-state reset sites (`:314-319`, `:501-506`, `:852-857`) must also tear down
  every session — a session outliving a lab switch would stream a stale link id.

**Non-goal inside this refactor:** do not change `CaptureTransport`, `PcapngParser`,
`encodePcapng`, `appendCapturePackets`'s cap logic, or the download path. The diff is
*ownership*, not behaviour.

### 7.3 Attribution — the dirstat MAC channel

This section was rewritten after review. **Three blocking findings (§1a #1, #2, #3) all landed
here**, and they are load-bearing on each other: a correct learning *rule* (#1) fed by a
correct endpoint *index* (#2) with a correct *lifecycle* (#3). Implementing two of the three
produces a feature that is wrong in a way live testing will not reliably surface.

#### 7.3.1 The endpoint index must come from the doc, not from a slice position

**The bug being fixed exists today, before Batch 7.** `openLinkDirstat`
(`fabric_linux.go:594`) calls `fabricLinkTapDevs`, which is a **compacting** wrapper: it maps
`fabricLinkEndpointDevs`' `[]endpointDev{EndpointIndex, Dev}` down to a bare `[]string`
(`fabric_linux.go:875-887`), dropping the index. `dirstat.Open` then re-derives the endpoint
index from **loop position** (`dirstat_linux.go:52-55`, `for ep, dev := range devs`). Every
endpoint that yields no dev — a VPCS with no shim tap yet, a tool/PC whose node is stopped, an
IOL interface with no static tap — is *skipped*, shifting every later endpoint down. A link
whose endpoint 0 is not yet up and whose endpoint 1 is attributes **all** of endpoint 1's
traffic to endpoint 0.

That already corrupts the shipped `ProtosDir` / `ProtosSubtypeDir` `[ep0, ep1]` ordering
contract documented at `protocol/verbs.go:562-590` — the Watcher's directional dashes can point
the wrong way on a half-started link. Batch 7 fixes it because it must, and §7.7 pins it.

**Change, exactly:**

```go
// dirstat.EndpointDev binds a tap device to the LAB DOCUMENT endpoint index it
// belongs to. Do not reconstruct this from slice position: the device list is
// sparse (a stopped or not-yet-attached endpoint contributes no device).
type EndpointDev struct {
    Index int    // lab.Link.Endpoints index
    Dev   string
}

func Open(devs []EndpointDev) (*Classifier, error)
```

- `Open`'s loop guards on **`d.Index > 1`** (`continue`, not `break` — a sparse list is not
  sorted-and-dense by contract) and passes `d.Index` into `readLoop`, replacing the `ep` loop
  variable at `dirstat_linux.go:52-71`. Everything downstream (`c.count(ep, …)`, `Counters`'
  `{ep, label, subtype}` key) is unchanged in shape and now correct in meaning.
- `openLinkDirstat` calls **`s.fabricLinkEndpointDevs(ll, l)`** and maps it to
  `[]dirstat.EndpointDev`. **`fabricLinkTapDevs` is not deleted and not changed** — the
  frames/bytes summation in `fabricStats` (`fabric_linux.go:855-860`) is order-independent and
  legitimately wants the compact form. The rule is narrower and easier to review: *anything
  that attributes must use the indexed form; anything that sums may use the compact one.*
- Endpoints beyond index 1 remain unattributed — the guard is retained, only its input is
  corrected. A 3-endpoint segment link gets attribution for doc endpoints 0 and 1 and `—` for
  the rest. Widening dirstat is out of scope (§11).

#### 7.3.2 The learning rule: singular MAC, with ambiguity as a first-class state

Per endpoint the `Classifier` keeps **one candidate**, not a set (§4.2 argues why a set is
unfixable):

```go
// attribCandidate is one endpoint's source-MAC attribution state. An endpoint is
// attributable ONLY in state single: the moment a second distinct source MAC is
// seen on the tap, the endpoint is a relay (switch/bridge/uplink) as far as we
// can tell, and it attributes nothing at all -- INCLUDING the first MAC, which
// may itself have been forwarded. Never widen this to "keep the first one".
type attribCandidate struct {
    mac       [6]byte
    state     attribState // attribNone | attribSingle | attribAmbiguous
    firstSeen int64       // monotonic nanos
    lastSeen  int64       // monotonic nanos, refreshed on every matching frame
}
```

- Populated in `readLoop` beside the existing `c.count(...)` call
  (`dirstat_linux.go:133-134`) from `buf[6:12]` — **already inside `snapLen = 128`**, so no
  snaplen change and no allocation beyond a 6-byte array copy. Guarded by the existing mutex
  (`dirstat.go:62-73`).
- Frames with a **group/multicast or all-zero source MAC** (`mac[0]&1 == 1`, or all zero) are
  ignored for learning entirely — they are malformed as a source and must not trip the
  ambiguity flip. They are still counted by `c.count` exactly as today.
- **`attribNone` + any valid source MAC → `attribSingle`.** **`attribSingle` + the same MAC →
  refresh `lastSeen`.** **`attribSingle` + a different MAC → `attribAmbiguous`**, candidate
  cleared, `lastSeen` refreshed. **`attribAmbiguous` + anything → refresh `lastSeen`.**
  Three transitions, no set, no cap to tune, O(1) memory per endpoint.

#### 7.3.3 Lifecycle: aging, relearn, and cross-endpoint conflict

Without this, "never guess" degrades into "never guess *today*" — the draft's permanent
first-wins sets would have served a MAC learned an hour and three lab edits ago (§1a #3).

- **TTL.** `macTTL = 5 * time.Minute`. A candidate whose `lastSeen` is older than `macTTL` is
  **expired**: `attribSingle` → `attribNone` (stale identity is dropped rather than served),
  and `attribAmbiguous` → `attribNone` (the endpoint gets a clean chance to relearn). Expiry
  is evaluated **lazily inside the snapshot call**, under the same lock — no timer, no
  goroutine, no new teardown path. A quiet-but-still-attached node simply falls back to MAC
  display until it speaks again, which is the honest answer.
- **Relearn is the point of expiring ambiguity.** A VM whose NIC MAC is replaced, or a node
  restarted with a new MAC, first makes its endpoint ambiguous (two distinct MACs), then —
  after `macTTL` of the old MAC not appearing — relearns the new one cleanly. A permanently
  poisoned endpoint would have been the alternative.
- **Cross-endpoint conflict.** A MAC currently attributed to endpoint 0 that then appears as a
  source on endpoint 1's tap (a moved NIC, a VM migrated across the link, a loop) is entered
  in a **bounded conflict set**: `maxConflict = 32`, drop-oldest, each entry TTL'd on the same
  `macTTL`. **A conflicted MAC is attributed by neither endpoint** for as long as it is in the
  set; the endpoint that held it as its candidate also drops to `attribNone` and may relearn.
  This is the specific case the review named and the draft had no answer for.
- Memory is bounded **by construction**, not by a tuned cap: two candidates plus at most 32
  conflict entries per link. The "8 per endpoint, first-wins" bound is retired.

#### 7.3.4 The wire shape — endpoint-indexed and confidence-carrying

`MACs()` is replaced by `Attribution() []EndpointAttrib`, returning a copy under the lock and
nil-safe on a nil `*Classifier` like every other method (`dirstat.go:41`, `:130`), mirroring
`Snapshot()`'s copy-out contract (`dirstat.go:39-52`).

`LinkStatsData` (`protocol/verbs.go:562`) gains:

```go
// EndpointAttrib is a per-endpoint source-MAC attribution HINT for one fabric
// link endpoint.
//
// EndpointIndex is the lab document's endpoint index and is carried explicitly:
// do NOT infer it from this slice's position, which is sparse (an endpoint with
// no tap contributes no entry).
//
// State is the only thing that licenses naming a node:
//   "single"    - exactly one source MAC has been observed on this endpoint's
//                 tap within the TTL; MAC is set; naming the endpoint's node for
//                 frames with that source MAC is warranted.
//   "ambiguous" - more than one source MAC has been observed: this endpoint is
//                 forwarding for other devices (a switch/bridge/uplink). MAC is
//                 empty. NOTHING may be attributed to this endpoint, including
//                 any MAC previously seen on it.
//   "none"      - nothing observed yet, or the observation aged out.
//
// A MAC absent from every entry is NOT evidence about who sent a frame. There is
// no reading of this field that yields a node name for a frame the supervisor
// did not positively attribute.
type EndpointAttrib struct {
    EndpointIndex int    `json:"endpointIndex"`
    State         string `json:"state"`         // "single" | "ambiguous" | "none"
    MAC           string `json:"mac,omitempty"` // lowercase colon-separated; set iff State=="single"
}

// EpAttrib is omitted entirely when the per-endpoint classifier is unavailable
// (non-root dev box: dirstat_linux.go:39-44), which the GUI renders as the
// attribution-unavailable banner rather than as "no traffic".
EpAttrib []EndpointAttrib `json:"epAttrib,omitempty"`
```

populated in `fabricStats` (`fabric_linux.go:831-870`) beside the existing `dc.Snapshot()`
(`:865`). It rides the existing `link.stats` event — **no new verb and no new event**, because
this is a slowly-changing hint on data the GUI already receives per link per tick, and a
dedicated channel would need its own lifecycle for no benefit.

#### 7.3.5 Browser side — resolve once, at event creation

`labStore` records `epAttrib` per link alongside `linkStats` (`:100-107`). Resolution is a
pure function in `lens.ts`:

```
srcMac → the unique EndpointAttrib with State=="single" && MAC==srcMac
       → link.endpoints[entry.endpointIndex].node → node.name
```

- **Zero matches → `null`.** **Two matches → `null`** (defensive: the supervisor's conflict
  set should already have prevented it; the browser does not adjudicate).
- `LensEvent.src` stores the **resolved result** — `{node, name} | null` — **at push time**,
  not the raw MAC to be re-resolved on render. Two reasons, both load-bearing: aging (§7.3.3)
  must never retroactively rewrite rows a learner already read, and a filter chip built on a
  value that can change under it is a filter that silently empties. `srcMac` is retained on
  the event for display and for the unattributed case.
- **There is no fallback that returns a node name.** `LensPane` renders the MAC when `src` is
  null and `—` when even the MAC is unavailable (§7.1.4's three states, unchanged).

**Documentation:** `docs/protocol.md`'s `link.stats` section gains `epAttrib` with the
`single`/`ambiguous`/`none` semantics and the explicit-index warning in prose, in the same
place `ProtosDir`'s endpoint-ordering contract is documented — and that existing contract's
prose gains a note that it is now honoured for sparse endpoint lists too (§7.3.1).

### 7.4 What has to be decoded fresh — implement exactly this

Everything else reuses `pcapng.ts`'s existing dissection. `summarize()` is refactored so its
already-computed intermediates (`etherType`, `l3` offset, MACs, IPs, ports, the L4 protocol
number) are available to a sibling `lensEvent()` without a second traversal —
`summarize()`'s **output shape and its callers must not change** (`CaptureTerm.svelte:170`).

**802.1Q VID** — at `pcapng.ts:371-374` the tag is already peeled; keep the 12 low bits of
`u16(frame, 14)` as `vlan`. Priority/DEI discarded. One tag only (§4.3).

**DHCPv4** — UDP src/dst ∈ {67, 68}. The frame is untruncated (`-s 0`, §3.5), so the whole
BOOTP payload is present. Read, from the UDP payload start:
- `op` (offset 0), `xid` (offset 4, 4 bytes), `yiaddr` (offset 16, 4 bytes),
  `chaddr` (offset 28, 6 bytes) — the same field offsets `extnet/dhcp.go:100-148`
  `DecodePacket` uses, which is the in-repo reference to check against (**read it; do not
  import it — it is Go, server-side, and serves the NAT gateway**).
- Options begin at offset 240 (236-byte header + the 4-byte magic cookie `63 82 53 63` —
  **verify the cookie before walking options**, and abandon the walk if it is absent).
  Walk TLVs; stop at 255. Needed: **53** (message type: 1 Discover, 2 Offer, 3 Request,
  4 Decline, 5 ACK, 6 NAK, 7 Release, 8 Inform), and **54** (server id) for the ACK/Offer
  prose. Skip 0 (pad, 1 byte, no length).
- Prose: `DHCP Discover`, `DHCP Offer 10.1.1.12` (yiaddr), `DHCP Request`,
  `DHCP ACK 10.1.1.12`, `DHCP NAK`.
- **`yiaddr` is `0.0.0.0` on Discover and Request** — do not print `0.0.0.0`.

**HSRP** — UDP dst 1985 (v1) and 1985/2029 for v2 IPv4/IPv6. v1 payload: `version`(0),
`opcode`(1: 0 Hello, 1 Coup, 2 Resign), `state`(2: 0 Initial, 1 Learn, 2 Listen, 4 Speak,
8 Standby, 16 Active), `group`(6), `virtual IP`(16, 4 bytes). Prose:
`HSRP Hello · group 1 · Active · vIP 10.1.1.1`. **HSRPv2 has a different, TLV-based layout —
decode v1 only, and label an unrecognised version `HSRP (v2)` with no field detail** rather
than misreading v1 offsets into a v2 packet.

**Everything else is prose over existing output:** OSPF gets its packet type from the OSPF
header byte at `l3 + IHL + 1` (the same offset `classify.go:171-189` reads; the browser does
not have this today, only the `OSPF` label from `pcapng.ts:587`) →
`OSPF Hello`/`DB Description`/`LS Request`/`LS Update`/`LS Ack`. ARP reuses `pcapng.ts:404`'s
existing prose verbatim. STP reuses `:498-501`'s root/cost string. CDP reuses `:522-525`'s
Device ID.

**Every decoder is bounds-checked and returns a degraded event rather than throwing** — the
contract `pcapng.ts:361` ("Defensive against truncated captures") and
`classify.go:9-11` both already state.

### 7.5 The event model

New `app/src/lib/lens.ts` (pure, no Svelte, no store import — testable in principle and
reviewable in isolation):

```ts
export interface LensEvent {
  seq: number;            // per-LINK monotonic delivery sequence; see below
  tsMicros: number;       // ABSOLUTE (pcapng.ts:174) — never tRel (see §8.5)
  proto: LensProto;       // "arp" | "dhcp" | "stp" | "cdp" | "ospf" | "hsrp" | "other"
  text: string;           // the plain-English line
  srcMac: string;
  dstMac: string;
  src: { node: number; name: string } | null;  // RESOLVED AT PUSH TIME (§7.3.5)
  vlan: number | null;    // outer 802.1Q VID
  srcIp?: string;
  dstIp?: string;
}

/** Build one event from an ALREADY-PARSED packet.
 *
 *  pkt supplies the two things a raw frame cannot: pkt.tsMicros (absolute, from
 *  the pcapng EPB — pcapng.ts:174) and pkt.origLen. The frame bytes are pkt.data.
 *  seq is assigned by the caller from the link's delivery counter (§8.5); it is
 *  NOT derivable from the frame, and it is deliberately NOT pkt.index, which
 *  restarts at 0 on every reconnect exactly as tRel does (pcapng.ts:170-171).
 *  attrib is the link's current EndpointAttrib list, resolved here and stored,
 *  never re-resolved on render (§7.3.5).
 */
export function lensEvent(
  pkt: ParsedPacket,
  seq: number,
  attrib: EndpointAttribView,
): LensEvent | null;
```

**Where `seq` comes from — the one answer for both the Lens and marks (§1a #4, #8).** It is a
**per-link monotonic delivery counter** owned by `consoleUiStore` and introduced by Batch 9
(§8.5), advanced once per delivered packet batch:

```ts
// returns the seq that the FIRST packet of this batch gets
advanceCaptureDelivery(linkId: number, n: number): number
```

- **Not derived from the stream.** `ParsedPacket.index` and `tRel` both restart at each
  reconnect (a fresh `PcapngParser` per connection, `CaptureTerm.svelte:111-113`); the
  delivery counter does not. It counts packets *this browser session has been handed* for this
  link, which is exactly the axis "mark capture now" means (§8.5).
- **Single writer.** In Batch 9 the increment lives in `CaptureTerm`'s `onData`. When Batch 7's
  §7.2 refactor moves stream ownership into `labStore`'s capture session, the **call site moves
  with it** and `CaptureTerm` stops advancing it — the counter's *home* stays
  `consoleUiStore` in both worlds, so marks written under Batch 9 remain valid under Batch 7.
  Two writers would double-count and silently offset every mark; §7.7 tests for exactly that.
- **Reset on `closeCapture(linkId)` only**, not on reconnect. Marks referring to a reset link
  render "position unknown" (§8.5) rather than landing somewhere plausible and wrong.

- **`proto` is the Lens's own vocabulary, deliberately separate from
  `watcherStore.ProtoKey`.** `watcherStore.svelte.ts:64-70` requires its keys to mirror
  backend `ClassifyDetailed` labels byte-for-byte; DHCP and HSRP have no backend labels, so
  adding them there would create rows that silently never match (§3.8). `lens.ts` carries a
  comment naming that relationship so a future reader does not "unify" them.
- **`other` events are kept, not dropped.** The "All" preset shows them (as the existing
  `summarize` one-liner) so a learner is never staring at an empty timeline wondering whether
  the capture is broken. Presets filter the *view*, not the ring.
- Ring per link: 2000 events, drop-oldest. Reactive length counter only — the ring itself is a
  plain array like `captureBuffers` (`labStore.svelte.ts:752`), so appending a packet burst
  does not invalidate a `$state` proxy 500 times.

### 7.6 `LensPane.svelte`

- Header: link title (reuse `Console.svelte:24-31` `captureTitle`), the six preset toggles +
  "All", an active-filter chip row (node / VLAN, each with an ✕), and the event count.
- Body: a virtualized-if-needed scrolling list, newest at the bottom, auto-scroll pinned
  unless the user has scrolled up (the standard log-pane rule — state it, implement it).
- Row: `+t · [node chip] · [proto chip] · text · [vlan chip]`. `t` is rendered **relative to
  the first event in the ring**, computed from `tsMicros`, not from `tRel` (§8.5).
- Empty states, all three distinguishable: capture not started; capture live but no packets
  yet (reuse `CaptureTerm.svelte:79-85`'s honest wording); packets flowing but every one
  filtered out by the current presets.
- **Attribution-state banner — three distinct messages, because the three causes need three
  different learner reactions** (§7.3.4):
  - `epAttrib` **absent**: "This appliance could not open per-endpoint classifiers for this
    link, so events show MAC addresses instead of node names." (§3.7's non-root / unbindable
    case, `dirstat_linux.go:39-44`.) Nothing the learner did; nothing they can fix.
  - one or more endpoints **`ambiguous`**: "`<endpoint side>` forwards traffic for other
    devices (it looks like a switch), so its frames show MAC addresses rather than a node
    name." This is the switch case, and saying it plainly is the feature — a learner reading
    "SW1 sent …" learns something false, a learner reading this learns what a switch *is*.
  - all endpoints **`none`**: no banner. The events themselves showing MACs is sufficient, and
    a banner on a link that simply has not talked yet is noise.
- The banner is **not** an error style. It is the same weight as the empty-state copy.

### 7.7 Testing bar

1. `cd supervisor && go build ./... && go vet ./... && go test ./...` — green.
2. **dirstat attribution tests** (`dirstat_test.go`, pure — the existing test file is platform
   independent). These are the unit tests for §1a findings 1 and 3, and they are the batch's
   only cheap defence against a silently-wrong attributor:
   - one MAC, many frames → `single`, that MAC, `lastSeen` refreshed;
   - **two distinct MACs → `ambiguous`, empty MAC, and the first MAC no longer attributed** —
     the switch case, asserted as a *negative*;
   - group/multicast and all-zero source MACs are ignored and do **not** trip ambiguity;
   - `lastSeen` older than `macTTL` → `none`, and a subsequent frame relearns to `single`;
     an aged-out `ambiguous` endpoint also returns to `none` and can relearn;
   - a MAC attributed to endpoint 0 that then appears on endpoint 1 → conflicted → **neither**
     endpoint attributes it; the conflict set caps at 32 drop-oldest and its entries age out;
   - `Attribution()` on a nil `*Classifier` returns nil without panicking.
3. **Endpoint-index regression test (§1a #2, §7.3.1)** — the one that pins a *pre-existing*
   bug: build a link whose endpoint **0** contributes no device and endpoint **1** does, open
   the classifier through the `[]EndpointDev` path, feed a frame, and assert both the
   `Counters` key and the `EndpointAttrib.EndpointIndex` say **1**. Against the old compacted
   `[]string` path this test fails, which is the proof it is testing the right thing — say so
   in the test's own comment so a future "simplification" back to `fabricLinkTapDevs` breaks
   loudly.
4. **`fabricStats` test**: a link whose Classifier is nil produces `LinkStatsData` with
   `EpAttrib == nil` (and therefore an absent JSON key, not `null` and not `[]`).
5. **Delivery-counter single-writer check (§7.5):** grep proof that
   `advanceCaptureDelivery` has exactly **one** call site after the §7.2 refactor, and that it
   is in `labStore`'s capture session and not in `CaptureTerm.svelte`.
4. `cd app && npm run check` — green.
5. **Single-socket proof (manual, mandatory):** open a link's capture tab **and** its Lens
   tab; the browser devtools Network panel shows **one** `/capture/<linkId>` WebSocket. Two
   means §4.4's refactor was not done.
6. **Download regression (manual):** with both tabs open, "Save .pcapng" still produces a file
   Wireshark opens — the §7.2 refactor moved the buffer's feeder, and this is the first thing
   it can break.
7. **Reconnect regression (manual):** stop and restart the lab with a Lens tab open; the
   capture summary re-emits its header, the Lens keeps its ring (it must **not** clear on
   reconnect — the events are still true), and the relative-time column does not jump
   backwards (the §8.5 `tRel` trap, in its Lens form).
8. Prose decoders are verified live, not by unit test — §10.3. Say so in the PR rather than
   claiming coverage a fixture-free browser module does not have.

### 7.8 Acceptance gate

All of the above, plus grep proofs — each one guards a fix that a plausible "simplification"
would silently undo:

1. `supervisor/internal/relay/classify.go`, `supervisor/internal/bcap/`,
   `supervisor/internal/extnet/dhcp.go` and `app/src/lib/watcherStore.svelte.ts` have **zero
   changed lines**. Any diff there means the design drifted into re-implementing or unifying a
   parser (§4.1) or into widening the Watcher's backend-mirrored vocabulary (§3.8).
2. **`grep -n 'fabricLinkTapDevs' supervisor/internal/server/fabric_linux.go` shows it is
   **not** called from `openLinkDirstat`** (§7.3.1). The only remaining callers may be
   summation paths. This is §1a finding 2's gate and it is mechanical: a diff that "fixed" the
   index inside `dirstat` while still being fed a compacted slice passes every other check
   here and is still wrong.
3. **`grep -n 'macs\s*\[2\]map\|first-wins\|maxMacs' supervisor/internal/dirstat/` is empty.**
   No set-of-MACs learning survived anywhere (§1a finding 1).
4. **The string `"ambiguous"` appears in `dirstat`, in `protocol/verbs.go`, in `lens.ts` and in
   `LensPane.svelte`** — the ambiguity signal is carried end to end, not dropped at a layer
   boundary, which is the most likely way this fix gets half-applied.
5. **`grep -rn 'advanceCaptureDelivery' app/src` shows exactly one call site** (§7.5).
6. **`grep -n 'lensEvent(' app/src` shows no call passing a bare `Uint8Array`** — the API takes
   a `ParsedPacket` because `seq`/`tsMicros` are not in the frame (§1a finding 4).

---

## 8. Batch 9 — learner console workspace (idea #9)

### 8.1 Decisions locked

1. **Tiling is a layout over the panes that already exist** (§4.6). No component is created
   or destroyed when the layout changes; a **tiled** pane stops being `position: absolute` and
   becomes a CSS-grid cell.
2. **Layouts: `tabs` (today's behaviour, the default), `tile2`, `tile3`, `tile4`.** Grid
   templates: 1×2, 1×3 (or 2+1 — implementer's choice, stated in the PR), 2×2. Panes beyond
   the tile count stay tab-selectable and are **taken out of flow entirely**, not merely made
   invisible — see §8.3, and §1a finding 5 for why `visibility: hidden` is not sufficient
   under a grid.
3. **`active` splits into `visible` and `focused`** (§4.6). This is the batch's first commit.
   Per §1a MINOR 1 this is an **ownership** fix (fit must not share a flag with focus; an
   out-of-flow pane must not be fitted) and is to be described that way in the PR, not as the
   repair of a proven wrap bug.
4. **The pinned compare pane is a pane reference, not a component.** `pinned: PaneRef | null`;
   a pinned pane always occupies tile slot 0 and is excluded from the rotation that fills the
   remaining slots.
5. **Search is per-pane, over one terminal's buffer.** No cross-console search, no persistent
   history, no server-side log. §8.4 decides the mechanism honestly.
6. **Marks are session-only and stream-positional, never clock-compared** (§8.5).
7. **Nothing in this batch adds a `DockSide` value or a drag-move primitive** (§4.6/§3.11).

### 8.2 The pane model — where it lives and why

New in `consoleUiStore.svelte.ts`, following the §3.3 shape:

```ts
export type PaneRef =
  | { kind: "console"; node: number }
  | { kind: "capture"; link: number }
  | { kind: "lens"; link: number };      // reserved for Batch 7; see §8.7
export type ConsoleLayout = "tabs" | "tile2" | "tile3" | "tile4";
```

- **`layout: ConsoleLayout`** — persisted, key `iolbox.console.layout`, default `"tabs"`.
- **`tiles: PaneRef[]`** — session-only, **not persisted**. It references lab-specific node
  and link ids. Persisting tiles would restore panes for a lab that is no longer open. State
  this in the code comment; it is exactly the kind of thing a later reader "fixes".
- **`focused: PaneRef | null`** — session-only. This **replaces** the split
  `(labStore.activeConsoleTab, Console.svelte:11 activeCapture)` encoding (§3.1).
  `labStore.activeConsoleTab` **stays** as the field the rest of the app reads
  (`Console.svelte:209-217` address chip, `labStore.openConsole`, `closeConsole`) — **do not
  delete it in this batch**; that is a separate cleanup with its own grep sweep.
- **`pinned: PaneRef | null`** — session-only.
- **`marks: ConsoleMark[]` and `captureDelivered: Record<number, number>`** — session-only
  (§8.5, §7.5).
- Helpers: `paneKey(ref): string` (`"console:3"` / `"capture:7"`) for `{#each}` keys and map
  indices, `samePane(a,b)`, `setLayout`, `toggleTile(ref)`, `setFocused(ref)`, `setPinned(ref)`,
  plus the two lifecycle entry points below.

#### 8.2a Focus sync is bidirectional, and shaped so it cannot oscillate (§1a finding 6)

The draft synced **`focused` → `labStore.activeConsoleTab`** only. That is not enough:
`labStore.openConsole(nodeId)` sets `activeConsoleTab = nodeId` itself
(`labStore.svelte.ts:997-1002`), and it is called from **outside the dock** — the canvas node
menu, `openConsoleByMode` (`:1033-1035`), and the open-all-consoles fan-out (`:1046-1047`).
With a one-way sync, double-clicking a node on the canvas opens its tab and leaves the
*previously* focused pane focused and on screen: the feature appears to do nothing.

`labStore` cannot be edited (§8.9's gate) **and cannot be imported** (`labStore.svelte.ts:8`
already imports `consoleUiStore`; the reverse edge would close a cycle between two singletons).
So the sync lives entirely on the store side that *can* be edited, with the outward write
delivered through a callback `App.svelte` registers once:

```ts
// consoleUiStore, non-reactive fields (plain fields, NOT $state):
#lastSeenActiveConsole: number | null = null;
#onSelectConsole: ((nodeId: number) => void) | null = null;

/** App.svelte calls this once: bindConsoleSelect((id) => { labStore.activeConsoleTab = id; }).
 *  A callback, not an import: labStore already imports this module. */
bindConsoleSelect(fn: (nodeId: number) => void) { this.#onSelectConsole = fn; }

setFocused(ref: PaneRef | null) {           // dock-initiated focus
  this.focused = ref;
  if (ref?.kind === "console") {
    this.#lastSeenActiveConsole = ref.node;
    this.#onSelectConsole?.(ref.node);       // the existing outward sync
  }
  // NOTE: a capture/lens pane does NOT null activeConsoleTab; the address chip
  // keeps naming the last focused console, which is today's behaviour.
}

syncFromLabStore(active: number | null) {   // externally-initiated focus
  if (active === this.#lastSeenActiveConsole) return;   // our own write, echoed
  this.#lastSeenActiveConsole = active;
  if (active === null) return;
  this.focused = { kind: "console", node: active };
  if (this.layout !== "tabs") this.ensureTiled({ kind: "console", node: active });
}
```

- **The shadow is what makes this safe.** It is written on every path that changes
  `activeConsoleTab`, so `syncFromLabStore` fires only on a value *someone else* set. There is
  no `$effect` writing a value another `$effect` reads back — the loop is cut by data, not by
  a flag or a `untrack()`.
- **`ensureTiled(ref)`** adds the pane to `tiles` if absent; if `tiles` is already at the
  layout's capacity it **evicts the least-recently-focused non-pinned tile** (a small
  non-reactive `Map<paneKey, number>` of focus timestamps, maintained by `setFocused` /
  `syncFromLabStore`). The pinned pane in slot 0 is never evicted (§8.1.4). Deterministic, and
  it means "open a console from the canvas while tiled" always surfaces that console.

#### 8.2b Lifecycle: `reconcile()`, so nothing in `labStore` has to change (§1a finding 7)

The draft leaned on `labStore`'s lab-load/reset/switch clears to drop stale panes, while
§8.9's gate forbids touching `labStore` — leaving stale pane refs and marks with no code path
to clear them. Resolved by **observing** rather than **hooking**:

```ts
reconcile(labId: string, consoles: number[], captures: number[]): void
```

- **`labId` differs from the last one seen → hard reset**: `tiles = []`, `focused = null`,
  `pinned = null`, `marks = []`, `captureDelivered = {}`, shadow cleared. A different lab
  shares no node or link identity with the previous one; nothing survives.
- **`labId` unchanged → prune**: drop any `PaneRef` (in `tiles`, `focused`, `pinned`) whose
  node is not in `consoles` or whose link is not in `captures`; drop `captureDelivered`
  entries and per-link `capturePos` entries for links no longer open; drop a mark once it has
  no remaining positions **and** no console still open (§8.5).
- **The single call site is one `$effect` in `App.svelte`** (§8.3), reading `labStore.lab.id`,
  `labStore.openConsoleTabs`, `labStore.openCaptureTabs`. Two properties matter: it survives
  `Console.svelte` unmounting (which happens exactly when both lists empty — the moment marks
  would otherwise be orphaned), and it is **agnostic to how many places `labStore` clears tab
  state**. Today there are five (`:323-327`, `:535-540`, `:891-892`, `:906-907`, `:1062-1063`)
  — the draft named three, which is itself evidence that enumerating them was the wrong
  design. p5 adding a sixth cannot break this.
- The same effect calls `syncFromLabStore(labStore.activeConsoleTab)` (§8.2a). One effect, one
  direction of dependency, zero `labStore` diff.

**Why `PaneRef` and not "the tab index":** idea #10 needs a stable per-window identity to key
`{x, y, w, h}` position persistence (§3.11), and `paneKey()` is exactly that key. A tile model
keyed by array position would have to be redone for #10.

### 8.3 Concrete file-level changes

**`consoleUiStore.svelte.ts`** — the types above, the new fields (`layout`, `tiles`, `focused`,
`pinned`, `marks`, `captureDelivered`, the non-reactive focus-recency map and
`#lastSeenActiveConsole`), `setLayout` persisting to `iolbox.console.layout`, and the helpers
including `ensureTiled`, `syncFromLabStore`, `reconcile`, `advanceCaptureDelivery`.
Roughly +160 lines, no change to existing fields. **`consoleUiStore` must not import
`labStore`.** The edge already runs the other way — `labStore.svelte.ts:8` imports
`consoleUiStore` — so a direct import here would close a module cycle between two
class-instance singletons. §8.2a's outward write therefore goes through a callback that
`App.svelte` registers once, which is why that effect exists rather than being a convenience.

**`Console.svelte`** —
- Delete the local `activeCapture` (`:11`); route `selectCapture`/`selectConsole`
  (`:33-40`) through `consoleUiStore.setFocused` (still writing `labStore.activeConsoleTab`
  for consoles, per §8.2).
- Tab strip (`:162-206`): each tab gets a small pin toggle and, in a tiled layout, an
  indication that it is currently tiled. The ✕ / Wireshark-flip buttons are unchanged.
  **Clicking an untiled tab while tiled calls `setFocused` → `ensureTiled`** (§8.2a), so it
  takes a cell by evicting the least-recently-focused non-pinned tile. Without this, a tab
  outside `tiles` would be `display: none` and clicking it would appear to do nothing — the
  §1a finding 5 fix creates this requirement, so it is not optional polish.
- **The layout CSS — rewritten after review (§1a finding 5).** The draft kept
  `.term-slot.hidden { visibility: hidden }` for untiled panes while making `.term-area` a
  grid. That does not work: a `visibility: hidden` element is **still a grid item**. With four
  tabs open and `tile2` selected, the two untiled panes would claim two of the grid's cells
  (or spill into implicit rows), so the tiled panes land in the wrong cells or off-screen, and
  the bug looks like "tiling is broken sometimes" rather than like a CSS-flow mistake.

  The rule is therefore **membership decides flow participation**, in three classes:

  | | `tabs` layout | tiled layout, pane ∈ `tiles` | tiled layout, pane ∉ `tiles` |
  |---|---|---|---|
  | `.term-area` | `position: relative` (unchanged) | `display: grid` + template per layout | — |
  | slot | `position: absolute; inset: 0` (unchanged, `:528-531`) | `position: static` grid item | **`display: none`** |
  | hiding | `.hidden { visibility: hidden }` (unchanged, `:539-542`) | never hidden | out of flow entirely |
  | `visible` prop | true only for the focused pane (today's behaviour) | **true** | **false** |

  Concretely: `.term-area.tiled { display: grid; }` with the template from `layout`,
  `.term-slot.tiled { position: static; min-width: 0; min-height: 0; }` (both `min-*: 0` are
  required or a terminal's intrinsic width will refuse to shrink its track), and
  `.term-slot.untiled { display: none; }`. **The existing `tabs` rules are not edited at all** —
  `git diff` on `:522-542` should show additions, not modifications, which is itself the
  cheapest evidence the default path is untouched.
- **The zero-box hazard `display: none` introduces, handled explicitly.** An out-of-flow pane
  has no box: its `ResizeObserver` (§3.2) reports nothing, and any `fit()` against it would
  measure 0×0 and set a garbage `cols`/`rows`. Two guards, both required:
  1. `visible=false` for untiled panes, and `fit()` is `visible`-gated — so the fit never runs
     while the pane is out of flow;
  2. on the untiled→tiled transition, `visible` flips true and the fit effect refits inside a
     **`requestAnimationFrame`** (not `queueMicrotask` — the microtask runs before layout, and
     the pane's box does not exist until after the style flush), **and** both terminals'
     fit paths early-return when `container.clientWidth === 0 || container.clientHeight === 0`.
     The guard is belt-and-braces on purpose: it also covers a dock collapsed to zero height by
     the `SplitPane`.
- Dock actions (`:208-258`) gain a layout control — a single cycling button with the existing
  `.dock-icon` treatment, matching the dock-side toggle's idiom (`:244-251`), plus a search
  toggle and a "Mark" button.
- Both `{#each}` blocks (`:264-277`, `:278-344`) pass `visible` and `focused` instead of
  `active`.
- The `wiresharkOverlayFor` `$effect` (`:63-69`) keeps working: it now calls `setFocused` and,
  in a tiled layout, ensures the pane is tiled.
- The native-Wireshark overlay (`:283-342`) is unchanged — but verify it still lays out inside
  a grid cell rather than the full dock (`position: absolute; inset: 0` at `:548-555` is
  relative to `.term-slot`, so it should; confirm visually).

**`ConsoleTerm.svelte`** —
- Props `active` → `visible, focused` (`:12`).
- `:104-109` splits: `if (visible) requestAnimationFrame(fit)` / `if (focused) term?.focus()`.
  `requestAnimationFrame`, not `queueMicrotask`, for the reason in §8.3's zero-box note.
- `:114-121`'s `if (!active) return` becomes `if (!visible) return`, and the effect must also
  depend on `consoleUiStore.layout` and on this pane's tiled-ness — a layout change resizes the
  box exactly like a dock-side flip does, and it is the same hole if it is missed.
- **Add the zero-box guard** to the fit path used by the effects **and** by the
  `ResizeObserver` (`:91-95`): skip when `container.clientWidth === 0 ||
  container.clientHeight === 0`, and do not send NAWS for a skipped fit. The observer keeps its
  ungated shape otherwise — it is the safety net §3.2 credits it as being, and narrowing it to
  `visible` would remove a mechanism that currently works.
- **Set `scrollback` explicitly** in the `Terminal` options (`:39-47`) — see §8.4.

**`CaptureTerm.svelte`** — same prop split (`:151-153` gates on `visible`), same zero-box
guard on its `ResizeObserver` (`:141-143`), and **`onData` advances the per-link delivery
counter**: `consoleUiStore.advanceCaptureDelivery(linkId, pkts.length)` once per batch, beside
the existing `appendCapturePackets` call (`:123`). This is the counter §8.5's marks index into
and §7.5's `LensEvent.seq` inherits; Batch 7 later moves this single call site into `labStore`'s
capture session and removes it here.

**`App.svelte`** — two additions, and they are the reason this file is in scope beyond "one
prop":
- `consoleUiStore.bindConsoleSelect((id) => { labStore.activeConsoleTab = id; })` once at
  setup (§8.2a) — the outward half of the focus sync, delivered as a callback because
  `consoleUiStore` must not import `labStore`.
- one `$effect` calling `consoleUiStore.reconcile(labStore.lab.id, labStore.openConsoleTabs,
  labStore.openCaptureTabs)` and `consoleUiStore.syncFromLabStore(labStore.activeConsoleTab)`
  (§8.2a, §8.2b). It lives here rather than in `Console.svelte` because `Console.svelte`
  unmounts when both tab lists empty (`:33-35` `showConsole`) — precisely the moment stale
  marks and pane refs must be pruned.
- Otherwise no logic change; verify the dock's `SplitPane` min/max (`:88-98`, `:112-122`)
  still gives four tiles a usable height, and raise the vertical `min` if not.

**`package.json` / `package-lock.json`** — see §8.4.

### 8.4 Searchable scrollback — decided, with the dependency question answered

**Fact (§3.10): `@xterm/addon-search` is not installed.** The idea row's "extends existing
xterm.js dock" is true of the tiling and false of the search — this is the assumption to
correct rather than to discover mid-implementation.

**Decision, in order:**

1. **Add `@xterm/addon-search` at whatever version its `peerDependencies` declare compatible
   with `@xterm/xterm ^6.0.0`.** Verify compatibility from the published package metadata
   before adding it — do **not** copy a version number from this plan or from an xterm 5-era
   example. Update `package-lock.json` in the same commit (`docs/build.md:47` runs `npm ci`,
   which fails on a lockfile that does not match `package.json`).
2. **If no v6-compatible release exists,** implement `app/src/lib/consoleSearch.ts` over
   xterm's public `term.buffer.active` API (`length`, `getLine(i)`, `line.translateToString()`):
   case-insensitive substring find, next/previous, a match count, and `term.scrollToLine()` to
   reveal a hit. ~60 lines, no highlight decoration (that is what the addon buys, and doing it
   by hand with decorations is out of scope). **Choose one and say which in the PR** — a
   silent skip of search would leave a third of the idea row unimplemented.
3. **Set `scrollback` explicitly** in `ConsoleTerm.svelte`'s `Terminal` options: today it is
   unset, so xterm's default applies, and **search can never find a line the buffer already
   dropped**. Lock `scrollback: 5000` — enough for a `show run` plus context on a few nodes,
   bounded enough that four open terminals do not accumulate unbounded memory. Capture panes
   are not searched in this batch (the Lens's filters are the right tool there).
4. **Be honest about the ceiling:** history does **not** survive closing a tab
   (`ConsoleTerm.svelte:98-102` disposes the terminal) and does not survive a page reload. The
   UI must not imply otherwise — the control is labelled "Find in this console", not
   "History".

Search UI: a small find bar inside the **focused** pane only, `Ctrl`/`Cmd`+`F` while a console
pane has focus, `Esc` to close, `Enter`/`Shift+Enter` for next/previous, live match count.
**The find bar must not steal `Ctrl+F` from the browser when no console pane is focused.**

### 8.5 "Mark capture now" — the timebase decision

**The record — corrected after review (§1a finding 8).** The draft's `{id, wall, label}` had
no stream position at all, so Batch 7 would have had nothing to place a Lens divider by except
the wall clock — the exact correlation this section spends the rest of its length rejecting.
The record carries the position explicitly:

```ts
export interface ConsoleMark {
  id: number;
  /** Human-readable label + ordering of marks RELATIVE TO EACH OTHER only.
   *  Never compared against a packet timestamp -- see the trap below. */
  wall: number;
  label: string;
  /** linkId -> the link's delivery-sequence value at the instant of the mark:
   *  the seq the NEXT packet delivered on that link will receive (§7.5).
   *  One entry per link that had a live capture session when the mark was taken;
   *  a link absent from this map renders "position unknown", never a guess. */
  capturePos: Record<number, number>;
}
```

Session-only, held in `consoleUiStore` (no lab-doc field, no schema change, no supervisor
involvement).

**The position axis is the per-link delivery counter defined in §7.5** —
`consoleUiStore.captureDelivered[linkId]`, advanced by `CaptureTerm`'s `onData` in Batch 9 and
by `labStore`'s capture session in Batch 7. It counts packets *handed to this browser session*
for this link. It does not restart on reconnect (unlike `ParsedPacket.index` and `tRel`), it
needs no clock, and it is the same number `LensEvent.seq` carries — so Batch 7's divider rule
is a comparison, not a correlation:

> insert the divider immediately **before the first event whose `seq >= mark.capturePos[linkId]`**;
> if no such event exists yet, the divider sits at the tail and new events land after it.

That is the entire cross-batch contract, and it is why §7.5 insists on a single writer: a
double-counted delivery offsets every divider without producing an error anywhere.

**On mark:**
- Every **visible console** pane writes a dim horizontal rule line into its terminal
  (`term.write`). A rule line is the only thing a mark can be in xterm — there is no line-index
  model to anchor to, and inserting one is exactly what a learner scrolling back needs to see.
- The mark's `capturePos` snapshots `captureDelivered[linkId]` for **every link with a live
  capture session**, not only the visible ones — a capture pane keeps streaming while untiled
  (§3.1), so restricting the snapshot to visible panes would produce marks that are correct on
  screen and missing where the learner looks next. Every **open capture** pane also writes the
  matching rule line at that position.

**The trap, and the decision:** do **not** place the capture-side mark by comparing
`Date.now()` to a packet timestamp.
- `ParsedPacket.tRel` (`pcapng.ts:170-171`) is relative to the current stream's first packet
  and **resets to 0 on every reconnect** (`CaptureTerm.svelte:111-113` builds a fresh parser
  per connection). A mark stored as a `tRel` silently relocates after any reconnect.
- `ParsedPacket.tsMicros` (`pcapng.ts:174`) is absolute — but it is stamped by **tcpdump on
  the appliance VM**, while `Date.now()` is the **browser's** clock. There is no clock sync
  between them and nothing in this repo establishes one. A skew of even a second puts the mark
  in the wrong place, and the failure is invisible.
- **Stream position sidesteps both.** The mark is inserted between two received packets, which
  is precisely what "mark capture now" means, and it needs no shared clock.

`wall` is retained only for the mark's own human-readable label and for ordering marks
relative to each other — **it is never an input to placement**, and the type comment says so
so that a later reader cannot reintroduce the correlation by "improving" the divider rule.
When the Lens exists (Batch 7), it consumes the same mark list and inserts a divider row at
the `capturePos` boundary above — which is the idea row's "syncing a timestamp into both
console history and the packet timeline", delivered without a shared clock.

**Bound:** 50 marks per session, drop-oldest. Marks are cleared and pruned by
`consoleUiStore.reconcile()` (§8.2b) — wholesale on a lab-id change, per-link on a capture
close — so no `labStore` edit is needed and no reset site has to be enumerated.

### 8.6 Not foreclosing idea #10

Non-negotiable, and each is checkable by grep in review:
- **`DockSide` stays `"bottom" | "right"`** (`consoleUiStore.svelte.ts:5`). Layout is an
  orthogonal axis. Idea #10 adds `"floating"` later without touching `ConsoleLayout`.
- **`PaneRef` + `paneKey()` are the pane identity** idea #10 will key its `{x, y, w, h}` map
  by. No tile logic may depend on a pane's array index.
- **`ConsoleTerm.svelte` gains no knowledge of its container.** Idea #10's design explicitly
  reuses it unchanged inside `FloatingConsoleWindow.svelte` (§3.11). The `visible`/`focused`
  props are container-agnostic **by construction** — a floating window passes `visible: true`.
  Do not add a `tileIndex` or `inDock` prop.
- **No drag-move primitive.** Reordering tiles by dragging is out of scope (§9); it is one
  small step from idea #10's primitive and should be built on it, not before it.

### 8.7 Interaction with Batch 7

`PaneRef`'s `"lens"` arm is declared in Batch 9 and unused until Batch 7 lands. That is
deliberate: it costs one union member now and saves re-typing every `PaneRef` consumer later.
Batch 9 must handle an unknown pane kind by ignoring it rather than throwing, so a
half-migrated state cannot white-screen the dock.

### 8.8 Testing bar

1. `cd app && npm run check` — green (and green **again** after the dependency addition, which
   is the most likely source of a type error).
2. `cd app && npm ci && npm run build` from a clean `node_modules` — proves the lockfile is
   consistent (§8.4 step 1). This is the one build step that must be run, because an
   inconsistent lockfile breaks `docs/build.md`'s documented path for everyone.
3. Manual, in the dev mock, recorded in the PR:
   - **The §4.6 claim, first — stated as what it is:** open two consoles, switch to `tile2`,
     run a wide command in the **non-focused** pane, and confirm it wraps at that pane's real
     width. Then narrow the dock and confirm both panes reflow. This is the batch's core
     claim; per §1a MINOR 1 it is *"every tiled pane wraps at its own width"*, **not** *"we
     fixed a bug where they didn't"* — the pre-change baseline may well have passed via the
     `ResizeObserver`, and the PR must not claim otherwise.
   - **The grid-flow check (§1a finding 5):** open **four** tabs, select `tile2`. Exactly two
     panes are on screen, each filling half the dock, with **no empty cell and no implicit
     third row**. Then click one of the two untiled tabs and confirm it takes a cell (evicting
     the least-recently-focused non-pinned one) rather than doing nothing. Inspect an untiled
     slot in devtools: it must be `display: none`, not `visibility: hidden`.
   - **The transition refit:** with `tile4` and four consoles, run `show run` in a pane, switch
     to `tile2` (dropping it out of flow) and back, then run a wide command in it again — it
     wraps at the new width, and its `cols` is not 0 or stale.
   - **Focus sync (§1a finding 6):** while tiled with two consoles, open a **third** node's
     console from the canvas. That console must become the focused, tiled, on-screen pane. Then
     click a tab in the dock and confirm the canvas-side selection follows — both directions,
     with no flicker or ping-pong (a two-way sync that oscillates shows as the focus ring
     bouncing between two panes).
   - **Lifecycle (§1a finding 7):** with tiles, a pin, and two marks set, switch to a different
     lab. No stale panes, no stale marks, no console errors — and `git diff` still shows zero
     lines changed in `labStore.svelte.ts`. Then close a single capture tab and confirm only
     *its* mark positions were pruned, not the whole mark list.
   - `tabs` layout is byte-for-byte the old behaviour: one visible pane, tab strip, ✕ closes,
     capture flip works, address chip tracks the focused console.
   - `tile2/3/4` with a mixture of console and capture panes, including a **tool node**
     (iframe pane, `Console.svelte:266-275`) in a tile.
   - Pin a pane, change focus repeatedly — the pinned pane never leaves slot 0.
   - Layout survives a page reload; tile membership does **not** (and the dock opens in `tabs`
     with the persisted layout applied only once panes exist — verify it does not open empty
     and broken).
   - Dock-side flip while tiled: every pane refits (this is `ConsoleTerm.svelte:114-121`'s
     effect, now `visible`-gated — the most likely place to get the dependency list wrong).
   - Search: find a string that is only in scrollback (scroll it off-screen first), confirm
     next/previous and the match count, confirm `Esc` closes and browser `Ctrl+F` is unaffected
     when the canvas has focus.
   - Mark: with two consoles and one capture pane open, click Mark — a rule appears in both
     consoles and in the capture at the current position; generate traffic and confirm new
     packets land **after** the rule. Then, in devtools, confirm the stored mark carries a
     numeric `capturePos[linkId]` (§1a finding 8) — a mark with only `{id, wall, label}` is the
     un-fixed design and Batch 7 cannot place a divider from it.
   - Close the last tiled pane → the dock returns to a sane state, not an empty grid.

### 8.9 Acceptance gate

1–3 above, plus:
- `git diff app/src/lib/consoleUiStore.svelte.ts` shows **no change** to `DockSide`
  (`:5`) or to any existing pref's persistence.
- `git diff` shows **zero** changed lines in `consoleTransport.ts`, `captureTransport.ts`,
  `consoleColorizer.ts`, `pcapng.ts`, **`labStore.svelte.ts`**, and `supervisor/` — Batch 9 is
  frontend-only and transport-neutral, and a diff outside `app/src/lib/components/`,
  `consoleUiStore.svelte.ts`, `App.svelte` and the two package files means the design drifted.
  **The `labStore` clause survives review unchanged** (§1a finding 7): the reset problem it
  used to create is solved by `reconcile()` (§8.2b), not by carving an exception. If an
  implementer finds themselves needing a `labStore` edit, the reconciler is wrong — report it
  rather than widening the gate.
- **`App.svelte`'s allowance is explicit and bounded:** the `bindConsoleSelect` registration
  and the one `reconcile`/`syncFromLabStore` `$effect` (§8.3), plus any `SplitPane` `min`
  adjustment. Anything else in `App.svelte` is out of scope.
- `grep -n "visibility: *hidden" app/src/lib/components/Console.svelte` still matches **only**
  the pre-existing `tabs`-layout `.term-slot.hidden` rule, and a `display: none` rule exists
  for untiled panes (§1a finding 5).
- `grep -rn "labStore" app/src/lib/consoleUiStore.svelte.ts` is **empty** — no import cycle
  (§8.3).
- The PR states which of §8.4's two search mechanisms was used, and why.

---

## 9. Explicit non-goals for the implementing agents

**Batch 8**
- **Do not modify `app/src/lib/interfaces.ts`.** It already computes the answer (§4.5).
- **Do not touch `InterfacePicker.svelte:90-91` or `:105-106`** — p5's in-flight `pc` arms.
- **Do not reorder interfaces** to float the suggestion to the top of the list (§6.1.3).
- **Do not add a store field, a derived link index, or any caching of "used interfaces".**
  `usedInterfaces` is O(links) on a document with tens of links.
- **Do not auto-create the link on drop** without the picker. The picker is the confirmation
  step and it is the whole feature's surface.
- **Do not add a test runner to `app/`** (§6.3).

**Batch 9**
- **Do not add a third `DockSide`**, a drag-move primitive, tile drag-reordering, or per-pane
  position/size persistence. That is idea #10 (§3.11, §8.6).
- **Do not unmount hidden panes** "to save memory". They are live consoles; unmounting drops
  the WebSocket and the scrollback (§3.1).
- **Do not delete `labStore.activeConsoleTab`** in this batch (§8.2).
- **Do not persist `tiles`, `focused`, `pinned`, the mark list, or the delivery counters**
  (§8.2, §8.5).
- **Do not hide untiled panes with `visibility: hidden` in a tiled layout** — they stay grid
  items and eat cells (§1a finding 5, §8.3). `display: none` for out-of-flow, and never
  unmount.
- **Do not import `labStore` from `consoleUiStore`** — the edge already runs the other way
  (`labStore.svelte.ts:8`) and closing it cycles two singletons (§8.3).
- **Do not sync focus one-way.** `labStore.openConsole()` selects the node it opens
  (`:997-1002`) and is called from the canvas; a one-way `focused → activeConsoleTab` sync
  leaves the wrong pane on screen (§1a finding 6, §8.2a).
- **Do not store a mark without a stream position** (§8.5). `{id, wall, label}` is the design
  the review rejected.
- **Do not implement cross-console search**, a server-side console log, or persistent history.
- **Do not place a capture-side mark by clock comparison** (§8.5). If a reviewer proposes it,
  the answer is there, with the mechanism.
- **Do not change `ConsoleTransport`, `CaptureTransport`, or `ConsoleColorizer`.**

**Batch 7**
- **Do not write a second protocol classifier in Go** (§4.1). `relay/classify.go` and
  `bcap/` have zero changed lines (§7.8).
- **Do not import, move, or refactor `supervisor/internal/extnet/dhcp.go`.** Read it as the
  field-offset reference; it stays exactly as it is, serving the NAT gateway.
- **Do not add DHCP/HSRP keys to `watcherStore.svelte.ts`** (§3.8, §7.5).
- **Do not raise `dirstat`'s `snapLen`** (`dirstat_linux.go:21`). The source MAC is already
  inside 128 bytes; raising it changes the cost of the always-on hot path for every link.
- **Do not remove `dirstat`'s `ep > 1` break** (`dirstat_linux.go:53-55`) to widen segment
  attribution (§10 / out of scope).
- **Do not open a second `/capture/<linkId>` WebSocket** (§4.4).
- **Do not guess a node name from a MAC** the supervisor did not attribute (§4.2, §7.1.4).
- **Do not learn a *set* of source MACs per endpoint**, at any cap, first-wins or otherwise
  (§1a finding 1, §7.3.2). One candidate, and the second distinct MAC means *relay*, not
  *another entry*.
- **Do not feed `dirstat.Open` a compacted device slice.** `fabricLinkTapDevs` is for
  summation only; attribution takes `fabricLinkEndpointDevs` (§1a finding 2, §7.3.1).
- **Do not make MAC learning permanent.** TTL, relearn and cross-endpoint conflict are part of
  the correctness argument, not tuning (§1a finding 3, §7.3.3).
- **Do not re-resolve a MAC to a node name at render time.** Resolve once at push time and
  store the result, or aging silently rewrites rows a learner already read (§7.3.5).
- **Do not give `lensEvent` a bare frame.** It needs the `ParsedPacket` (for `tsMicros`) and a
  caller-assigned `seq` (§1a finding 4, §7.5).
- **Do not add a new verb or event** for `epAttrib` (§7.3).
- **Do not persist Lens events, add a lab-doc field, or extend `contracts/lab.schema.json`.**

**All batches**
- Do not run `go build`, `npm run check`, or any test during the *planning* review pass — the
  implementing agents run them, per §6.3 / §7.7 / §8.8.
- Do not "fix" unrelated in-flight p5 changes found in the working tree (§2).

---

## 10. Live-VM / manual-verification checklist (orchestrator only)

Unit tests cannot prove any of this. §10.1 is cheap and unblocks nothing; §10.2 gates §10.3
(a Lens read in a mis-sized pane is a layout bug diagnosed as a parser bug).

### 10.1 Batch 8 — interface suggestion

Runs against the dev mock or the appliance; no VM state needed.
1. Fresh lab, two IOL routers with `ethernet: 2`. Drag → both selects show `e0/0 · next free`,
   `In use` group absent.
2. Connect. Drag the same pair again → `e0/1 · next free`; `e0/0` under `In use`, disabled.
3. Delete the first link → drag again → back to `e0/0 · next free` (the `$derived` suggestion
   tracked the doc, `interfaces.ts:27-35`).
4. A node with serial groups → serial ports appear last within each group, unre-ordered.
5. Wire every port on one node → "no free ports", Connect disabled, no hint, no crash.

### 10.2 Batch 9 — console workspace, on the appliance with real IOL nodes

1. Start a lab with **four** IOL routers. Open all four consoles. Switch to `tile4`.
2. **The §4.6 check.** In each of the four panes run `terminal width` / `show run | i hostname`
   and then a genuinely wide command (`show ip interface brief`, `show run`). **Every pane must
   wrap at its own width, not at 80 columns and not at the width of the focused pane.** Then
   drag the dock's `SplitPane` divider and repeat in two panes. A single mis-wrapped pane fails
   the batch. Record this as *"every tiled pane wraps at its own width"* — per §1a MINOR 1 it
   is **not** evidence that the pre-change code failed to, and no verdict line should say it
   is.
2a. **The grid-flow check (§1a finding 5), on real panes.** With four consoles open, switch
   `tile4 → tile2 → tile3 → tile4`. At every step the on-screen pane count equals the layout's
   tile count, cells are evenly sized, and no pane is clipped by an implicit extra row.
   Untiled slots are `display: none` in the inspector. Then click an untiled tab: it takes a
   cell immediately.
2b. **Focus sync (§1a finding 6).** With `tile2` active and two consoles tiled, open a third
   node's console **from the canvas**. It must appear tiled and focused. The dock address chip
   must name it.
3. Confirm from the node side where possible (`show line`, or simply that `show run` output is
   not visibly truncated/rewrapped mid-token).
4. Flip the dock bottom↔right while tiled — all four refit.
5. Pin one console, click through the other three — the pinned one stays in slot 0.
6. Open a **capture** tab on a link and tile it beside two consoles. Confirm the capture keeps
   streaming while unfocused (it was never unmounted).
7. Open a **tool node** console (iframe) in a tile — it renders, and the iframe is not
   reloaded by a layout change.
8. Search: `show run`, scroll the buffer, find a string only present in scrollback, walk
   next/previous. Then close and reopen the tab and confirm the buffer is empty — and that the
   UI never claimed otherwise (§8.4.4).
9. Mark: with two consoles + one capture open, click Mark, then generate ping traffic on the
   captured link. The rule line appears in all three, and the new packets land **after** the
   capture's rule. **Then break the stream deliberately:** `lab.stop`/`lab.start` (or otherwise
   force a capture reconnect) with the mark still on screen, generate more traffic, and confirm
   the rule has **not** moved. A `tRel`- or clock-based placement relocates here; a
   delivery-sequence placement cannot (§8.5).
10. Reload the page: layout persisted, tiles empty, no console errors.
11. `lab.stop` → `lab.load` a different lab: no stale panes, no stale marks, no stale pin —
    and confirm from `git diff` that this was achieved with **zero** `labStore` changes
    (§1a finding 7).

### 10.3 Batch 7 — Protocol Lens, against real traffic

Every prose string below must be read off a **real** capture. A parser that round-trips
against its own synthetic frame proves nothing (P4 §3.6's ACCT-REPLY field-order trap, and p5
§9.2.4's ~70-year NTP offset, are the same lesson).

1. **Single-socket proof first (blocking).** Open a link's capture and Lens tabs; devtools
   shows exactly one `/capture/<linkId>` WebSocket (§7.7.5). If there are two, stop — every
   subsequent timing observation is suspect.
2. **Attribution, positive case.** Two IOL routers on one link. Confirm `link.stats` carries
   `epAttrib` with two entries, each `state: "single"`, each with the right
   **`endpointIndex`**, and that Lens rows name `R1` / `R2` **correctly** — verify the MACs
   against `show interfaces e0/0 | i address` on each router, not against which name "looks
   right". Then **stop one node**: rows already in the ring keep their name (they were resolved
   at push time, §7.3.5), and after `macTTL` (5 min) its endpoint reports `none` — at which
   point *new* frames from it show MACs until it speaks again. Confirm both halves; the second
   is the aging behaviour §1a finding 3 introduced and it is a deliberate, visible change from
   the draft's "retained forever".
2a. **Endpoint-index case (§1a finding 2), which the two-router case cannot show.** Build a
   link where **endpoint 0's node is stopped** (no tap) and endpoint 1's is running and
   talking. Confirm `epAttrib` contains exactly one entry with **`endpointIndex: 1`** — not
   `0` — and that the Lens names endpoint 1's node, not endpoint 0's. Under the pre-fix
   compaction this step names the wrong router while looking entirely plausible, which is
   precisely why it is a separate numbered step.
3. **The negative case, which matters more than every positive one.** Insert an IOL **switch**
   between two routers, capture the switch↔router link, and generate traffic sourced from a
   third device. Required outcome: that endpoint reports **`state: "ambiguous"`** with **no**
   MAC, the pane shows the "forwards traffic for other devices" banner (§7.6), and **every**
   frame arriving from that side — including the switch's own BPDUs and CDP — shows a **MAC**,
   never a node name. A node name here is a correctness failure, not a cosmetic one; this is
   the exact input that defeated the draft design.
3a. **Conflict case.** Move a node (or its interface MAC) so the same MAC is sourced on both
   endpoints of one link. Both endpoints must stop attributing that MAC, and after `macTTL`
   the link must recover to correct attribution rather than staying poisoned (§7.3.3).
4. **Attribution-unavailable path.** Confirm the banner appears when `epAttrib` is absent (e.g.
   on the dev mock, or a link whose classifier did not bind — `dirstat_linux.go:39-44`), and
   that it is the *distinct* "could not open per-endpoint classifiers" wording, not the
   ambiguous-endpoint wording (§7.6).
5. **ARP:** `clear arp` then ping → `Who has …? Tell …` and the reply, matching what the
   capture tab's existing summary shows for the same frames (the two views must agree —
   they share the dissection).
6. **OSPF:** bring up an adjacency. Hellos every 10 s, then the full sequence during
   convergence: DB Description → LS Request → LS Update → LS Ack. **All five packet types must
   be observed**, not just Hello; a wrong offset shows only Hello and looks fine.
7. **DHCP:** `ip dhcp pool` on one router, `ip address dhcp` on the other. The Lens must show
   **Discover → Offer `<ip>` → Request → ACK `<ip>`**, and the two IPs must match the address
   the client actually installed (`show ip interface brief`). Then confirm Discover and Request
   do **not** print `0.0.0.0` (§7.4). Then run it again **through an `ip helper-address`
   relay** and confirm the relayed packets still decode (they are unicast with a nonzero
   `giaddr` — the option walk must not depend on the broadcast form).
8. **STP:** a switch with two links. Config BPDUs with a plausible root id and path cost;
   force a topology change (shut/no shut an access port) and confirm a TCN appears.
9. **CDP:** `cdp enable`; the Device ID must equal the neighbour's actual hostname
   (`show cdp neighbors` on the peer).
10. **HSRP:** `standby 1 ip <vip>` on both routers. Hellos naming group, state and vIP; force a
    failover (`standby 1 priority` / shut the active's link) and confirm the state transitions
    (Speak → Standby → Active) and any Coup/Resign are visible. **Confirm the version:** if the
    routers run HSRPv2, the rows must say `HSRP (v2)` with no field detail rather than
    plausible-looking garbage (§7.4).
11. **VLAN:** trunk the link, tag traffic on two VLANs, confirm the VID chip is correct on both
    and that click-to-filter isolates one.
12. **Click-to-filter by node:** click a node chip → only that node's events remain; the chip
    row shows the active filter with an ✕ that clears it.
13. **Ring bound:** run `ping … repeat 10000` and confirm the Lens stays responsive, the ring
    caps at 2000, and the browser tab's memory does not climb without bound (compare with the
    capture buffer's own 64 MiB cap, which is separate).
14. **Teardown:** close the Lens tab → capture keeps running. Close the capture tab → the WS
    closes, the doc's `capture.enabled` is withdrawn (`labStore.svelte.ts:817-821`), and the
    Lens tab closes with it (or, if it is left open, it must show the "capture not started"
    empty state rather than a frozen ring presented as live).

15. **Mark divider (cross-batch, §8.5 × §7.5).** With a Lens tab and a capture tab open on one
    link, click Mark, then generate traffic. The Lens divider must sit **between** the events
    that existed at mark time and the new ones, and must not move across a capture reconnect.
    Then confirm `grep -rn 'advanceCaptureDelivery' app/src` still shows exactly one call site
    — two writers double-count deliveries and offset the divider silently (§7.5).

Report a per-step verdict table. §10.3 step 1 failing blocks steps 5–13. **§10.3 steps 2a, 3
and 3a failing each block the batch on their own** — they are the three live proofs of the
review's attribution findings, and none of them can be substituted by a unit test or by the
two-router happy path.

---

## 11. Out of scope — named, not silently dropped

**Idea #8 / interface suggestion**
- **Suggesting a *pair* of interfaces by topology heuristic** (e.g. "routers usually connect
  e0/0↔e0/0"). Each end is independently suggested; nothing correlates them.
- **Interface aliasing / display names** (`Gi0/0` vs `e0/0`). `interfaces.ts` emits the lab
  document's spelling; the Palette's Router/Switch relabelling (commit `de1aa29`) did not
  change interface strings and neither does this.
- **The addressing worksheet** the source doc's row 8 originally carried before it was trimmed
  to the picker change alone. Not planned here, and not re-expanded.
- **A frontend test runner.** `app/` has none (§6.3); adding one is its own change.
- **MAC address viewer (idea #11).** Adjacent — and note that Batch 7's `epAttrib` (§7.3) is
  the first real MAC data this codebase would have, so #11 should be re-scoped against it
  rather than against its current "already-known data" assumption, which §3 found to be false.
  Note also that `epAttrib` deliberately carries **at most one MAC per endpoint** and hides it
  entirely on a relay endpoint (§7.3.2), so it is a poor foundation for a viewer that wants to
  enumerate MACs — #11 would need its own source, not a widening of this one.

**Idea #9 / console workspace**
- **Floating console windows (idea #10)** — the whole of §3.11. Batch 9 deliberately builds
  the pane identity it needs and stops.
- **Tile drag-reordering and drag-to-split.** Needs #10's primitive.
- **Persistent console history** across tab close or page reload, and any server-side console
  recording. §8.4.4 states the ceiling explicitly.
- **Cross-console search**, regex search, and search-result highlight decorations.
- **Search inside capture panes.** The Lens's filters are that feature (§8.4.3).
- **Per-pane font size.** `consoleUiStore.fontSize` stays global (`:70`).
- **Marks persisted into the lab document** or exported with a `.pcapng`.
- **Synchronised scrolling** between a pinned pane and its comparison.

**Idea #7 / Protocol Lens**
- **Any supervisor-side packet-event stream.** §4.1. If a future feature needs events without
  a browser attached (a headless checkpoint engine, idea #5), that is a different design with
  its own rate-limiting and its own review.
- **Per-endpoint attribution beyond endpoint index 1.** `dirstat_linux.go:53-55` caps at two;
  segment links get partial attribution and say so (§7.3).
- **Full CAM-table learning.** Attribution keeps **one** candidate MAC per endpoint and reports
  `ambiguous` the moment a second appears (§7.3.2). Naming the devices *behind* a switch is a
  different feature with a different (and much larger) correctness burden.
- **Q-in-Q / inner-VLAN decode.** Outer tag only (§4.3), matching both existing parsers.
- **DHCPv6, HSRPv2 field detail, VRRP, GLBP, LACP/LLDP detail, BGP/EIGRP message detail in the
  Lens.** The six presets the idea row names, plus the existing `summarize` line for
  everything else.
- **Protocol Lens on a link with no capture.** The Lens is a view of a capture stream; a link
  with no capture has no events. Auto-starting a capture from the Lens is in scope (§7.1.1);
  inventing an event source that does not need one is not.
- **Merging the Lens and the Network Watcher.** §3.8 divides them deliberately.
- **Exporting the Lens timeline** (CSV/JSON/markdown). Obvious follow-up; not v1.
- **Guided checkpoints (idea #5)** consuming Lens events as a test oracle. Depends on this;
  not part of it.

---

### Critical files for implementation

**Batch 8 (idea #8)**
- `J:\Claude code\iolab\app\src\lib\components\InterfacePicker.svelte` (:2-5 the header comment
  that already claims the feature; :29-35 `options`/`optsA`/`optsB`; **:39-42 the existing
  `nextFreeInterface` pre-selection — the fact that shrinks this batch**; :44-46
  `noFreeA`/`canConnect`; :90-91 / :105-106 **p5's in-flight `pc` arms, do not touch**;
  :95-99 / :110-114 the two selects; :143-147 / :160-168 the type scale the hint reuses)
- `J:\Claude code\iolab\app\src\lib\interfaces.ts` (**READ ONLY** — :27-35 `usedInterfaces` is
  where "has no existing link" already lives; :44-49 `nextFreeInterface`)
- `J:\Claude code\iolab\app\src\lib\components\CanvasInner.svelte` (:347-381 the link drag;
  **:379 the only place the picker is constructed** — it passes two node ids and a point, and
  computes no interfaces)

**Batch 9 (idea #9)**
- `J:\Claude code\iolab\app\src\lib\components\Console.svelte` (:11 the local `activeCapture`
  that §8.2 replaces; :33-40 selection; :63-69 the Wireshark one-shot; :162-206 the tab strip;
  :208-258 the dock actions; :264-344 the two `{#each}` blocks; **:522-542 `.term-area` /
  `.term-slot` / `.hidden` — the CSS that makes tiling cheap, and the CSS whose
  `visibility: hidden` rule at :539-542 must NOT be reused for untiled panes under a grid
  (§1a finding 5, §8.3): a hidden grid item is still a grid item**)
- `J:\Claude code\iolab\app\src\lib\components\ConsoleTerm.svelte` (:12 props; :39-47 the
  `Terminal` options **with no `scrollback`**; :48-49 the one loaded addon; **:104-109 and
  :114-121 — the `active` gates that are §4.6's trap**; :132-140 the font effect that is
  *not* gated, i.e. the shape the fixed effects should have; :98-102 dispose)
- `J:\Claude code\iolab\app\src\lib\components\CaptureTerm.svelte` (:151-153 the same gate;
  :91-143 mount/transport, which Batch 7 later moves)
- `J:\Claude code\iolab\app\src\lib\consoleUiStore.svelte.ts` (:5 `DockSide` **which must not
  gain a value**; :11-14 the key constants; :27-62 the guarded initializers; :79-130 the
  setter idiom the new prefs copy)
- `J:\Claude code\iolab\app\src\lib\labStore.svelte.ts` (**READ ONLY for Batch 9, §8.9** —
  :8 **it already imports `consoleUiStore`, so the reverse import is a cycle (§8.3)**;
  :997-1002 `openConsole`, which selects the node it opens and is the reason focus sync must be
  bidirectional (§8.2a); :1033-1035 `openConsoleByMode` and :1046-1047 the open-all fan-out,
  the other external callers; the five tab-clear sites listed under §3.4)
- `J:\Claude code\iolab\app\src\App.svelte` (:33-35 `showConsole` — **note the dock unmounts
  when both tab lists empty, which is why the reconciler effect lives here and not in
  `Console.svelte`, §8.3**; :88-98 / :112-122 the two `SplitPane` mounts whose `min`/`max`
  bound a four-tile grid)
- `J:\Claude code\iolab\app\package.json` (the complete xterm surface: `@xterm/xterm ^6.0.0`
  + `@xterm/addon-fit` only — §8.4)
- `J:\Claude code\iolab\docs\learning-features-gui-ideas-plan.md` (`## Floating console
  windows (idea #10, design detail)` — the design Batch 9 must not foreclose)

**Batch 7 (idea #7)**
- `J:\Claude code\iolab\app\src\lib\pcapng.ts` (:25-37 `ParsedPacket` — **:170-171 `tRel`
  resets per stream, :174 `tsMicros` is absolute**; :361-385 `summarize` and its 802.1Q peel
  at :371-374 where the VID is discarded; :435-530 the LLC/STP/CDP decoders to reuse;
  :551-591 `summarizeL4`, where DHCP/HSRP fall through to bare `UDP`)
- `J:\Claude code\iolab\app\src\lib\labStore.svelte.ts` (:724-742 `openCapture`, :806-823
  `closeCapture` **incl. the doc-intent withdrawal**; :744-773 the buffer + cap;
  :100-107 `linkStats` where `epAttrib` lands; the reset sites every new per-link state must
  join — **five of them, not three**: :323-327, :535-540, :891-892, :906-907, :1062-1063
  — which is why Batch 9 observes rather than enumerates (§8.2b) and Batch 7's session teardown
  must be driven the same way)
- `J:\Claude code\iolab\supervisor\internal\dirstat\dirstat.go` (**:1-19 the package doc — the
  citation for §4.2**; :27-73 the counter/Classifier shape; :39-52 `Snapshot`'s copy-out
  contract that `Attribution()` mirrors)
- `J:\Claude code\iolab\supervisor\internal\dirstat\dirstat_linux.go` (:21 `snapLen = 128`
  — **do not raise**; :45-87 `Open`, incl. **:52 `for ep, dev := range devs` — the
  slice-position-as-endpoint-index bug §7.3.1 fixes**, :53-55 the two-endpoint cap and :39-44
  the nil-Classifier degradation; **:117-136 `readLoop`, where `buf[6:12]` is already in
  hand**, and :130 the `PACKET_OUTGOING` drop whose *inverse* is not "this endpoint originated
  it" — only "this endpoint's tap received it", which is the whole of §1a finding 1)
- `J:\Claude code\iolab\supervisor\internal\server\fabric_linux.go` (:594 `openLinkDirstat`
  — **must be repointed from `fabricLinkTapDevs` to `fabricLinkEndpointDevs`, §7.3.1**;
  **:875-887 the two helpers: `fabricLinkEndpointDevs` preserves the endpoint index,
  `fabricLinkTapDevs` compacts it away**; :831-870 `fabricStats`, where `dc.Snapshot()` is read
  at :865 and `EpAttrib` is populated, and :855-860 the frames/bytes sum that may keep using
  the compact form)
- `J:\Claude code\iolab\supervisor\internal\protocol\verbs.go` (:562-590 `LinkStatsData` and
  the `[ep0, ep1]` endpoint-ordering contract — which §7.3.1 shows is **currently violated**
  for sparse endpoint lists and which `epAttrib`'s explicit `endpointIndex` replaces rather
  than inherits)
- `J:\Claude code\iolab\supervisor\internal\relay\classify.go` (**READ ONLY** — :22/:38 the
  `tagged bool` that is not a VLAN id; :171-189 the OSPF packet-type offsets to mirror
  browser-side; :367-378 `udpPort`, the proof DHCP/HSRP are absent)
- `J:\Claude code\iolab\supervisor\internal\extnet\dhcp.go` (**READ ONLY** — :84-98 the
  `Packet` field layout and :100-148 `DecodePacket`: the in-repo reference for DHCP offsets,
  to check against, not to import)
- `J:\Claude code\iolab\supervisor\internal\bcap\capture_linux.go` (**:45 — `tcpdump -i
  <bridge> … -s 0`: the capture point that cannot attribute, and the full-frame guarantee that
  makes browser-side DHCP option parsing possible**)
- `J:\Claude code\iolab\app\src\lib\watcherStore.svelte.ts` (**READ ONLY** — :36-79 the
  protocol vocabulary the Lens borrows without extending; :64-70 the byte-for-byte backend
  mirroring rule)
