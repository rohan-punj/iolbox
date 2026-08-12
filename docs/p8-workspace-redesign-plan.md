# P8 — Lab workspace redesign: slim top bar, icon rail, floating consoles, structured links, resource bar

Status: **dispatch plan. Reviewed by `codex sol-medium` (8 blocking + 4 minor, all applied —
§1a). Not implemented.** Eight batches (12–19) drawn from
`C:\Users\WS-HOME\Documents\Codex\2026-08-11\i\outputs\iolbox-redesign-session-spec.md`
(the "session brief" below) and its selected mockup
`…\iolbox-redesign-variants\variant-3-combined-refinement.png`. The whole plan is
**frontend-weighted**: seven of the eight batches never leave `app/`, and the eighth
(§13, the resource bar) reaches the supervisor only if the orchestrator elects to close the
RTT gap §5.7 documents — otherwise it is frontend-only too.

**§4 corrects nine load-bearing assumptions in the brief and its mockup that do not survive
contact with the current code — read §4 before §7–§14.** Four of those corrections *shrink*
a batch (the link hover-target and the two-ended interface labels are already built; the
bottom bar's height token already exists; the per-lab display prefs need no Go change), and
five *grow* one or move work into an explicit non-goal (the mockup's subnet labels have no
data source at all; there is no RTT telemetry anywhere; Structured routing is a geometry-
interface refactor, not a path swap; ContextMenu has no keyboard navigation; the brief's
"avoid glass/gradients/glow" directly contradicts three load-bearing rules in `DESIGN.md`).

**§3 is the reconciliation section and it is not optional reading.** This redesign's
"Floating Consoles" requirement lands on top of work this repo has already planned *and*
already built. §3 states, for P6 Batch 9 and for P7 Batch 10 separately, whether this plan
supersedes, extends, or coexists with it.

---

## 1. Model loop / process

1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it.** Two areas deserve disproportionate
   attention, and they are the two the brief itself leans on hardest. In both cases the
   failure class is the one p5, p6 and p7 all named: *code that builds, passes
   `npm run check`, renders something, and is quietly wrong.*
   - **§11 (Batch 16 — Structured link routing), specifically §11.2 and §11.3.** The brief
     says "render links as clean orthogonal/Manhattan paths" and calls it a display mode.
     Read against the code, the path string is the **smallest** part of the change.
     `FloatingEdge.svelte:248-349` does not produce a path; it produces a **geometry
     object** — `{ path, offsetPath(d, reversed), sChip, tChip, watcherChip, sOrigin,
     tOrigin }` — built on a closed-form quadratic evaluator `at(u)` (`:291-297`) that only
     exists because the path is a quadratic Bézier. **Twelve downstream consumers read that
     object** (the inventory was ten; sol-medium finding 10 found two more past `:530`):
     the traffic glow (`:356-360`), `BaseEdge` (`:362-375`), the fault pill
     (`:378-386`), the STP blocked/converging overlays (`:393`, `:398`), the best-path glow
     and its three `animateMotion` arrowheads (`:404-415`), the invisible hover catcher
     (`:422-428`), the Watcher's per-direction dashed flows and *their* `animateMotion`
     arrowheads (`:440-463`), both port chips (`:466-490`, `:493-519`), the STP reason
     popover (`:525`), **the routing best-path metric pill (`:537`, `geom.watcherChip`)** and
     **the Watcher label pills (`:545`, `geom.watcherChip` + an 18px per-row stack)**.
     An agent that adds `if (structured) path = getSmoothStepPath(...)`
     will ship a mode where the cable is orthogonal, the chips float off the cable, the
     Watcher's flows still bow on the old Bézier, and the STP overlay traces a curve that
     is no longer there — and every one of those renders without an error. Review §11.2's
     insistence that the batch's deliverable is a **mode-agnostic geometry interface**, and
     §11.3's rejection of `SVGGeometryElement.getPointAtLength` as the `at(u)` substitute.
   - **§10 (Batch 15 — floating consoles), specifically §10.2 and §10.4.** This is the
     batch with the most prior art and therefore the most opportunity to silently discard
     it. §3 decides that it **builds on P7 Batch 10** rather than replacing it, which means
     the review's job is to check that the reuse is real: the `ConsolePlacement` axis
     (P7 §4.1), the `PaneBody.svelte` extraction (P7 §4.2), the pointer-capture drag idiom
     (P7 §4.3), the `windowOrder`-derived z (P7 §6.6) and the three-site clamp invariant
     (P7 §6.7) are all carried forward *verbatim in intent*. What is genuinely new is
     §10.4's **placement policy**, and it exists because two things force it: the brief's
     "avoid opening new consoles directly over the selected node or the center of the
     visible topology" (brief:72), and `codex sol-medium`'s standing finding that P7's
     `setPlacement`/`ensureWindow` **cannot reach the viewport or `labStore`** from inside
     `consoleUiStore` without an illegal dependency (`consoleUiStore` must not import
     `labStore` — P6 §8.3, enforced today by the callback at `App.svelte:32-34`). §10.4
     resolves both with one decision. Review it against `consoleUiStore.svelte.ts:120-125`
     and `App.svelte:39-46`.
3. **`codex luna-xhigh` agent(s) implement.** Recommended order and parallelism in §6.
4. **The orchestrating session validates live** per §16 — using the browser-pane MCP tools
   against `npm run dev`'s mock transport, **not** Playwright (§15).

## 1a. Review — codex sol-medium findings applied

`codex sol-medium` reviewed the draft and found **8 blocking + 4 minor** (all fixed in this
document before implementation). They cluster: three are Batch 15's floating layer (coordinate
access, Lens lifecycle, pin state), two are Batch 16's geometry contract, two are the
selection/Restart pair in Batch 14's addendum, and one each are Batch 17's chip clamping and
Batch 18's file inventory. **Step 2 of §1 asked the review to concentrate on §10 and §11, and
five of the eight blocking findings landed there — the targeting was right, and the failure
class was the predicted one in every case: designs that would compile, render something, and be
quietly wrong.**

What sol-medium **confirmed** is unchanged and must **stay** unchanged — no edit below dilutes
any of it:

- **The P6/P7 reconciliation (§3) is sound.** Dock and floating are separate axes;
  `DockSide` (`consoleUiStore.svelte.ts:5`) and `ConsoleLayout` (`:10`) are untouched; P7's
  reviewed `PaneBody` extraction, pointer-capture drag primitive, `windowOrder`-derived
  z-index, three-site clamp invariant and per-`(labId, paneKey)` geometry persistence are
  genuinely reused rather than silently diverged from. §3.1/§3.2 and §10.1 stand as written.
- **`Console.svelte` really does have three pane kinds** — console, capture *and* lens — and
  all three are wired. §4.6's "grow a third arm" correction is real. (Finding 2 extends this
  to the *window* layer; it does not weaken it.)
- **`@xyflow/svelte ^1.6.1` genuinely exports `getSmoothStepPath`.** §5.5 and §11.4 stand; no
  dependency is needed and none may be added.
- **The `LinkGeometry` field set (`path` / `at` / `offsetPath` / chip anchors / transform
  origins) is sufficient for every real consumer** once finding 10's inventory gap is closed.
  **No consumer needs a DOM arc-length API**, so §11.3's rejection of `getPointAtLength` in the
  reactive path stands unweakened.
- **No RTT or latency telemetry exists anywhere in the protocol today**, and shipping none
  rather than fabricating one remains correct for this batch. Finding 11 is about the *name* a
  future slice may use, not about reversing §13.1.2.
- **The three `DESIGN.md` conflict citations are real** — Glass blur is load-bearing, the
  brand and node-face gradients are identity, glow is reserved for state and interaction — and
  §4.9's "introduce no new ones, delete no existing ones" resolution is the right one.

**One reading note for the implementing agents.** sol-medium's citations are against the
working tree *as of the review*, which continues to move (§2's "re-grep before editing" is not
boilerplate — this document's own edits shifted its line numbers while the fixes were being
applied). Where a fix below depends on a specific site, it names the **symbol** as well as the
line.

| # | Finding | Resolution |
|---|---|---|
| 1 | **The floating-console layer cannot obtain xyflow coordinates where the plan mounts it.** §10.4 had `FloatingConsoleLayer` — an `App.svelte` sibling — call `useSvelteFlow()`. That hook reads context published by `<SvelteFlowProvider>`, which exists only inside `Canvas.svelte` (`Canvas.svelte:8-10`, a three-line wrapper around `CanvasInner`). A sibling subtree has no access to it, so the placement policy would have thrown or silently degraded. | §10.4 rewritten around **the pattern this codebase already uses for exactly this problem**: `CanvasInner` publishes the projector on `labStore` and outside components read it — `labStore.screenToFlow` (`labStore.svelte.ts:51-53`) is wired at `CanvasInner.svelte:391`, nulled in the same teardown at `:399`, and consumed from outside the provider by `AnnoLine.svelte:67`; `labStore.canvasZoom` (`:50`, written at `:997`) is the scalar equivalent. Batch 15 therefore reads a **new mirror-image field `labStore.flowToScreen`** (plus `canvasPan` for finding 8), wired from the already-destructured `flowToScreenPosition` (`CanvasInner.svelte:300-301`). Mounting inside `Canvas.svelte`'s provider was considered and **rejected** with reasons (stacking context, lifetime, z-band). **Ownership is assigned so no gate breaks:** the two lines land with Batch 16/17 (which owns `CanvasInner` and needs the identical field for finding 8), Batch 15 only reads them and **must tolerate `null`** — its policy falls through to the cascade branch, so a window never fails to open. §10.12 gains a grep gate: no `useSvelteFlow` in the layer, `flowToScreen` present, Batch 15's `labStore` diff still empty. |
| 2 | **Lens rendering was fully specified; Lens WINDOW LIFECYCLE was not.** The layer's pane enumeration walked console + capture tabs only, and pruning reused `isOpen` (`consoleUiStore.svelte.ts:276-280`), which counts a Lens pane open for as long as its **capture** is open. Result under the draft: either a Lens pane that never gets a window (§4.6's failure, in a second place), or a zombie window that survives `labStore.closeLens()` forever. | New **§10.4a** makes the enumeration explicit and three-armed, sourced from the same three lists `Console.svelte`'s tab strip uses — including **`labStore.openLensTabs` (`labStore.svelte.ts:148`)**. §10.2.4 changes the store side: `reconcile` takes a **fourth argument** (`lenses`), `isOpen`'s lens arm keys off the **lens** set instead of the capture set, and the single driving `$effect` at `App.svelte:39-46` gains one read — so `consoleUiStore` still imports nothing from `labStore` (§10.12's grep is untouched). The Lens window now **opens on `openLens` (`:864-867`), closes on `closeLens` (`:870-872`), and on nothing else**; its close button calls `closeLens`, never `closeCapture`. Containment still works for free because `closeCapture` already clears that link from `openLensTabs` (`:972`). §10.11 gains the three-observation test (close Lens → capture survives; reopen; close capture → both go). |
| 3 | **`LinkGeometry.at()` was specified two incompatible ways** — §11.2 promised normalized **arc length**, §11.3 required lifting `FloatingEdge.svelte:291-297` *unchanged*, which is a closed-form quadratic evaluator in the curve **parameter**. They coincide only for a straight line, so building either one broke the other consumer — real arc length would move Free Flow's existing chips (violating §11.1.1's pixel-identical gate), and the parameter evaluator made the documented contract a lie. | New **§11.2a** picks **one** semantics: **`at()` is curve-parameter position, never arc length.** Free mode is `:291-297` verbatim (pixel identity preserved — the gate that mattered most); Structured mode walks its polyline by cumulative segment length, which for a polyline *is* its natural parametrization, and the plan says so plainly instead of over-promising. The parameter is renamed `t` in the interface, the doc comment states "NOT arc length" and warns against "fixing" it, and **every prior "arc-length" mention is corrected** (§5.4.4, §11.2's interface, §11.3, §17). The limitation is named rather than hidden: on a strongly bowed Free edge `at(0.5)` is the parameter midpoint, not the distance midpoint — **which is exactly today's shipped behavior**. True arc-length spacing becomes an explicit non-goal with the escape hatch specified (a *separate* `atLength(px)`, never a redefinition of `at()`). §11.10 gains `grep -rn "arc.length" app/src/lib/edges/` = empty. |
| 4 | **Batch 18's suppression-list file inventory was incomplete.** The batch's file list omitted the components that actually own the state the "never hide during an open menu or active drag" criterion must observe: `ContextMenu.svelte` (which owns **no** open flag — its existence *is* the open state, held in each parent's private `$state`) and `SplitPane.svelte` (`let dragging = $state(false)` at `:40`, unexported). The hard brief criterion was therefore unimplementable as scoped. | §5.8's table gains a **third column naming the state owner and what Batch 18 must do about it**, and §14.1 gains the primitive that makes it a one-liner per owner: **`chromeStore.hold()`**, a refcounted suppression handle taken in a `$effect` whose teardown releases it. `ContextMenu.svelte` takes the hold **for its lifetime**, which covers all four canvas call sites *and* §7's overflow menu *and* anything added later without `chromeStore` enumerating any of them; `SplitPane.svelte` takes it around `dragging`. The sweep that produced the column is recorded so it need not be repeated, and it found four more lifetime-mounted popovers (`AnnoStylePopover`, `ChangeImagePopover`, `IconPicker`, `InterfacePicker`) plus `dragMove.ts`. **Batch 18's file list grows from 5 to 12 files** in both §14.1 and §6's dispatch table, and its gate now names two specific exercises (a mounted `ContextMenu` held 5s; a `SplitPane` divider held 5s) plus `grep -n "chromeStore" ContextMenu.svelte SplitPane.svelte` non-empty. §6 also records the new 12↔18 collision on `ContextMenu.svelte`. |
| 5 | **"Restart does not exist" was factually wrong.** §4.5 shipped no Restart button on the grounds that no such verb exists and a client-side stop→start compound would race `nodeLocks`. All four legs of that reasoning are false against the code. | **Decision reversed; Restart ships.** §4.5 now cites the evidence: the verb is registered (`server/server.go:225`), the handler already sequences stop→start server-side in one call (`server/handlers.go:523-533`), the typed client method exists (`app/src/lib/supervisor.ts:191-193`), and the stop-then-restart reaper race is explicitly handled by `ReapDecision`'s `explicitlyStopped` flag (`node/state.go:81-95`), whose comment describes this exact scenario. §9.1.3 carries the design: **new `labStore.restartNode(nodeId)` = one `nodeLock` around one `node.restart` call**, shaped like `stopNode` (`:1078`). It also solves the one real subtlety the draft's worry was groping at: a restart emits an intermediate `node.state="stopped"` that would release an ordinary lock early (`:468`), so `acquireNodeLock` gains an optional `{holdUntilSettled}` flag whose locks are released only by the caller's own `finally` (the `wipe` idiom at `:1060-1069`), backstopped by the existing 60s timeout. Button gated on `isRunning`, placed after Console, reusing the existing `reset` glyph (`icons.svelte.ts:109`) — **no new icon** — with `stopPropagation` on `pointerdown` like every sibling. §9.2's testing bar and gate now cover it (including "the spinner persists across the intermediate `stopped` event"), §6 gains a **14a** row, §8.5's empty-`labStore` gate gains the one narrow carve-out, and §17 moves "a Restart node verb" out of the deferred list. |
| 6 | **Node selection and opening the Inspector were conflated.** §5.6, §4.5 and §9 keyed the selected-node toolset off `inspectorNodeId`, but a plain node click sets **only** `selectedNodeId` (`CanvasInner.svelte:560`); `inspectorNodeId` is set solely by `openEdit` (`:503-506`), reached from the context menu / `requestEdit` / double-click. The toolset would not have appeared on an ordinary click — the exact interaction the brief specifies — and the same error had §10.4's placement policy avoiding the wrong node. | Audited **every** selection-triggered use in the plan and corrected each. §5.6's bullet is rewritten as a full three-way fact block (who sets what, who clears what, what xyflow draws as `selected` — `:86`), quoting `labStore.svelte.ts:39-43`'s own comment that `inspectorNodeId` is "independent of `selectedNodeId`". §9.1.2 binds the reveal to `selectedNodeId`; §9.2 makes "plain left-click with the Inspector closed" the named regression test and greps `inspectorNodeId` out of `NodeActions.svelte`; §10.4's placement policy reads `selectedNodeId`, with a matching grep gate on the layer; §12.1.3's Escape handler is clarified to touch neither field. `inspectorNodeId` survives in exactly one role — "the Inspector pane is open" (`App.svelte:63`) — and §17 carries the standing rule. |
| 7 | **Floating "pin" had described behavior but no state model.** §10.5 described "keep on top" in prose while §10.2's store additions listed only `minimized` — no field, no method, nothing an implementer or a test could target. (Correctly *not* the docked `pinned` field, which the plan was right to refuse to overload.) | §10.2 gains **change 3: `pinnedWindows = $state<string[]>([])` + `togglePinnedWindow(key)` + `isWindowPinned(key)`**, an array to match `tiles`/`minimized`, cleared by `reconcile`'s hard reset and pruned with the rest. The behavioral contract is specified as **one change to one function**: `raiseWindow` appends as today, then **stably partitions `windowOrder`** so pinned keys follow unpinned ones — z-index stays purely `FLOAT_Z_BASE + index` (§10.8), so P7 §6.6 needs no second band. §10.5 replaces the prose with **five numbered, assertable properties**: ordering; pinned windows still reorder among themselves; pin exempts a window from *nothing* else (not clamping, not pruning, not minimize, not auto-hide — which never touches the console layer anyway); pin is not persisted; the button carries `aria-pressed`. §10.11 runs all five as observations and §10.12 greps that `pinned` (`:112`) and its readers are untouched. |
| 8 | **Viewport-clamped endpoint chips were hand-waved as "CSS where possible."** xyflow hardcodes its edge-label transform from flow coordinates (`node_modules/@xyflow/svelte/dist/lib/components/EdgeLabel/EdgeLabel.svelte:30` — `translate(-50%,-50%) translate({x}px,{y}px)`, inside a portalled viewport-transformed container), so no selector can clamp against the viewport. The plan would have knowingly shipped a hard brief criterion broken. | §12.1.4 rewritten with a **real mechanism, and it is deliberately the same solution finding 1 needs** — one shared wiring, not two. `CanvasInner`'s existing `onmove` handler (`:997`) also mirrors `labStore.canvasPan`, alongside the new `labStore.flowToScreen` and the existing `canvasZoom`. The chip then computes its own screen position **by arithmetic** — `screen = flow × zoom + pan` — and renders at `at(t) + delta` where `delta` is a clamp correction converted back to flow units. **This is explicitly not §11.3's rejected approach**: what was rejected is DOM *layout measurement* in a per-frame `$derived`; this is four multiplications over values the store already mirrors, with the chip's nominal size taken from a token constant rather than measured. Scope guarded: **only the two endpoint interface chips** clamp (the fault/best-path/Watcher/STP labels do not, and that is a named non-goal), clamping is display-only, and it lives in `FloatingEdge.svelte`'s template layer so **`routing.ts` stays flow-pure and §11.10's grep gate is unchanged**. §12.3 demonstrates it by panning a node off each of the four edges, at two zooms, with an FPS check for the "arithmetic, not measurement" claim; §16's criterion 4 row carries it. |
| 9 (minor) | **Rounded Structured corners and the geometry built on them disagreed.** The draft filleted the *visible* path while computing `at()`/`offsetPath` from the unfilleted polyline — so chips could sit up to `CORNER_R` off the cable, and worse, the best-path arrowheads (`:411`) and the Watcher's `animateMotion` flows (`:443`) would cut **square corners beside a visibly rounded cable**. | Fixed the preferred way — **one geometry, not two**. §11.3 now builds a `Seg[]` (a `line` / `quad` union) in which each interior vertex is *replaced* by its quadratic fillet (endpoints pulled back along each adjoining segment by `r = min(CORNER_R, half of each neighbour's length)`, control point at the original corner), and **`path`, `at()` and `offsetPath` are all derived from that same list**: `path` emits `L`/`Q`; `at()` walks it, evaluating fillets with **the same closed-form quadratic evaluator Free mode already uses**; `offsetPath` offsets segment-wise, mapping each fillet to a fillet on the shifted neighbours (exact for perpendicular segments, which axis-aligned routes always are). `pts` survives only as a construction intermediate and is **not** a field of `LinkGeometry` (gate-checkable). §11.9 gains a zoomed elbow check that the dashed flow and the arrowheads round the same corner the cable does. |
| 10 (minor) | **The "~10 consumers" inventory was stale** — it stopped scanning at `FloatingEdge.svelte:530`, missing the routing best-path metric pill (`:537`) and the Watcher label pills (`:545`), both of which read `geom.watcherChip`. The verification gate inherited the truncated range and would have silently skipped exactly the two newest consumers. | New **§11.2b** is a numbered twelve-row inventory with the two additions bolded, and **the verification range is corrected everywhere from `:352-530` to `:352`→end-of-template (`:552`)**: §1.2's summary, §5.4's closing line, §11.2, §11.8 and §11.10's "template unchanged" gate. §11.9's live checklist now walks all twelve **by number**, with the two additions checked explicitly (paint a routing best path; arm two Watcher rows and confirm both pills stack 18px apart on the cable). Re-confirmed, as the review expected: both consumers are covered by the existing `watcherChip` anchor, so **the interface itself needs nothing added** — this closes an inventory gap, it does not reopen the sufficiency question. |
| 11 (minor) | **The deferred "cheap honest RTT" recipe is not RTT.** Timing arbitrary requests end-to-end includes supervisor handler execution — the pending-request map (`app/src/lib/supervisor.ts:98`) carries no transport-level timestamps and there is no echo verb — so the number would be mislabeled from birth. | §4.7's deferral bullet is rewritten: the delta is **"RPC response time"** and that is the only name a later slice may ship it under, with the evidence (no timestamps in the pending map; `image.list` observed at 10–15s per `labStore.svelte.ts:66-72`'s own comment) and the statement that a genuine transport RTT needs a **new ping/pong verb pair carrying send timestamps** — that, not the delta, is what is deferred. §17's non-goal now names both data-plane and transport RTT and forbids shipping the delta as "RTT", "latency" **or** "link". §13.1.2's core decision — ship nothing this batch, leave the slot — is **unchanged**, as the review explicitly endorsed. |
| 12 (minor) | **§1's own process section mislabeled its two highest-risk batches**, calling Structured routing "Batch 15" and floating consoles "Batch 14" while the §6 dispatch table correctly numbers them 16 and 15. | Both corrected in §1 step 2 to match the dispatch table (§11 = **Batch 16**, §10 = **Batch 15**). While there, §1's own consumer count was corrected from ten to twelve per finding 10, and §6's Batch 18 row's section reference was corrected from §9 to §14.1. |

---

## 2. Relationship to prior plans, and the baseline these line numbers are against

- **The session brief is the source design doc**, and the mockup is the selected visual
  direction. Where the brief's *prose* and the mockup's *pixels* disagree, §4.1 records
  which wins and why. Where either contradicts the code, §4 records the correction and the
  evidence.
- **`docs/p6-…-console-workspace-plan.md` Batches 7, 8 and 9 are ALL on disk in the working
  tree, uncommitted.** This is a change from what P7 §3.7 recorded. The four checks:
  `git log` HEAD is still `25a1e05`; `git status` shows 39 modified files;
  `app/src/lib/lens.ts` and `app/src/lib/components/LensPane.svelte` exist;
  `protocol.EndpointAttrib` is populated on every `link.stats` event
  (`supervisor/internal/server/stats.go:109`, `toProtocolEndpointAttrib` at `:120-133`).
  So Batch 7 (Protocol Lens) landed after P7 was written, and **`Console.svelte` now has a
  third pane arm — `lens` — that P7 §4.2's `PaneBody` extraction does not account for**
  (§4.6).
- **`docs/p7-floating-console-mac-toggle-plan.md` is NOT implemented.** No
  `PaneBody.svelte`, no `FloatingConsoleWindow.svelte`, no `FloatingConsoleLayer.svelte`,
  no `dragMove.ts`, no `MacListPopover.svelte`, no `macs.ts`; `consoleUiStore.svelte.ts`
  has no `placement`, `windows` or `windowOrder` field (the file is 409 lines and ends at
  `toggleColorize()`), and `DockSide` is still `"bottom" | "right"`
  (`consoleUiStore.svelte.ts:5`). Its Batch 10 is a **design**, not code. §3 decides what
  happens to it.
- **P7's sol-medium review (5 blocking + 4 minor) is not in this repo.** There is no review
  section in `p7-…-plan.md` (its status line at `:3` still reads *"Not reviewed, not
  implemented"*) and no separate findings file in `docs/`. The one finding whose substance
  was relayed to this plan — `setPlacement` cannot reach viewport/`labStore` — is carried
  and resolved in §10.4. **An implementing agent must not assume the other findings were
  fixed; P7 Batch 10 is superseded as a unit (§3.2), so unrepaired findings inside it die
  with it rather than being inherited.**
- **`docs/p5-…-impairment-plan.md` is also in the working tree.** The `pc` node kind is
  fully present (`labTypes.ts:6`, `PcNode.svelte`, `interfaces.ts`), link faults are live
  (`LinkFault` at `labTypes.ts:75-84`, rendered at `FloatingEdge.svelte:377-387`).
  **Every line number in this document is against the working tree, not HEAD.** Re-grep
  before editing.
- **Standing posture, unchanged:** no Docker, no DB, no separate web server, one static Go
  supervisor binary, lightweight. **This plan adds no npm dependency** (§11.4 establishes
  that `@xyflow/svelte` already ships everything Structured routing needs).

---

## 3. Reconciliation — P6 Batch 9, P7 Batch 10, and this plan

### 3.1 P6 Batch 9 (the tiled/tabbed dock) — **COEXIST.** This plan extends it; it is not removed.

The brief says: *"Use movable, resizable floating console windows instead of a fixed console
pane"* (brief:16) and *"Consoles float, move, resize, minimize, and restore without shrinking
the canvas"* (brief:89). Read literally, both sentences constrain **where consoles live and
whether they steal canvas width**, and neither says "delete the dock". The acceptance
criterion is *"without shrinking the canvas"* — which the dock does today, because
`App.svelte:106-117` and `:130-141` mount `Console.svelte` inside a `SplitPane` that takes
its size out of `.center-col`.

**Decision: floating becomes the default placement; the dock survives as the alternative.**

Reasons, in order of weight:

1. **The tiled dock is a *different feature* that is already built and already good at
   something floating windows are bad at.** P6 Batch 9's `tile2/tile3/tile4`
   (`consoleUiStore.svelte.ts:10`, `:135-140`, grid CSS in `Console.svelte`) exists so a
   learner can watch four consoles **without overlapping** and without managing geometry.
   Floating windows overlap by construction. Deleting the dock would remove a shipped
   capability to satisfy a sentence that did not ask for its removal.
2. **P6 §8.6 anticipated exactly this and made the dock cheap to keep.** `PaneRef` +
   `paneKey()` (`consoleUiStore.svelte.ts:6-9`, `:22-24`) is the pane identity a floating
   window keys geometry by; `ConsoleTerm.svelte` is container-agnostic by construction (P6
   §8.6, third bullet). Keeping both placements costs one enum and one `{#if}` in
   `App.svelte`, not a parallel implementation.
3. **Losing the dock would strand the two tiled-only affordances.** `pinned`
   (`consoleUiStore.svelte.ts:112`, `setPinned` `:239-245`) means "always tile 1", which is
   meaningless for a free-floating window; the pin control in a floating title bar means
   "always on top" instead. Both are wanted; they are different verbs on the same word.
   §10.5 keeps them separate rather than collapsing them.

**What this plan does NOT do to Batch 9's code:** it does not change `layout`, `tiles`,
`ensureTiled` (`:168-184`), `trimTiles` (`:147-164`), `setFocused` (`:190-199`),
`syncFromLabStore` (`:202-210`), `advanceCaptureDelivery` (`:267-274`), `addMark`
(`:254-264`) or the tiled grid CSS. `reconcile` (`:283-353`) gains lines in both halves
(§10.3) and nothing else in that method is touched. **`ConsoleLayout` gains no fourth
value.** Floating is a *placement*, orthogonal to layout — which is the same conclusion
P7 §4.1 reached and it is confirmed here against the same three binary read sites
(`App.svelte:54`, `Console.svelte:253`, and `consoleUiStore.toggleDockSide` `:391-393`).

**Consistency with P6 §3.11 and §8.6 — confirmed on three of four, overridden on one:**

| P6 §8.6 constraint | status under this plan |
|---|---|
| `DockSide` stays `"bottom" \| "right"` | **honored.** §10.2 adds `ConsolePlacement`, a separate axis. `DockSide` is untouched at `consoleUiStore.svelte.ts:5`. |
| `PaneRef` + `paneKey()` are the pane identity; no tile logic keys off an array index | **honored, and now consumed as designed.** §10.2 keys `windows`, `windowOrder`, the `{#each}` and the geometry-persistence map by `paneKey(ref)`. |
| `ConsoleTerm.svelte` gains no knowledge of its container | **honored.** §10.6 forbids an `inWindow`/`floating`/`zIndex` prop; a window passes `visible: true` and `focused = isTopmost`. |
| "No drag-move primitive" (Batch 9's own non-goal) | **overridden, as §8.6 intended.** §8.6's wording is *"it is one small step from idea #10's primitive and should be built on it, not before it"* — this plan is that step. §10.7 lands `dragMove.ts` first and tile drag-reordering remains out of scope (§17). |

**P6 §3.11's claim on `DockSide`** — that idea #10 would add a third `"floating"` value — is
**explicitly overridden**, with the reasoning P7 §4.1 already established and this plan
re-verified: `App.svelte:54` computes `dockRight = dockSide === "right"`, so a third value
is *not* `"right"` and therefore mounts the **bottom** dock at `:106`, rendering a full
docked console underneath the floating windows with every pane mounted twice.

### 3.2 P7 Batch 10 (floating console windows) — **SUPERSEDED as the plan of record, but its design is REUSED, not redone.**

**Recommendation: do not implement `p7-…-plan.md` §6. Implement §10 of this document
instead. §10 is P7 §6 plus four additions and two corrections.**

This is deliberately *not* "P7 Batch 10 was wrong." P7 §6 did the load-bearing design work
on this exact problem and it is reused wholesale:

| P7 §6 decision | carried into §10 |
|---|---|
| §4.1 — placement is a **new axis** `ConsolePlacement = "dock" \| "float"`, `DockSide` untouched | **verbatim** (§10.2). Only the default flips (§3.2, correction 1). |
| §4.2 — a floating window must wrap the **pane body**, not `ConsoleTerm` directly, because `Console.svelte` privately owns `nativeCapture`, `searchOpenFor`, the Wireshark one-shot and the whole `.native-hold` overlay | **verbatim** (§10.3), extended by correction 2 below. This remains the batch's only structural refactor. |
| §4.3 — the drag primitive must use `SplitPane.svelte`'s **pointer-capture** idiom, not `AnnoLine.svelte`'s `window`-listener idiom (which leaks: neither annotation component imports `onDestroy`) | **verbatim** (§10.7). The reasoning is stronger here than in P7, because a redesign that makes floating the *default* multiplies the mid-drag-unmount exposure P7 §4.3 named. |
| §6.6 — z-index **derived from position in `windowOrder`**, not a monotonic counter; the 900–999 band is empty | **verbatim** (§10.8), re-verified: nothing in `app/src` occupies 61–999 (`ContextMenu.svelte:110` = 1000 is the floor above). |
| §6.7 — the title-bar-stays-in-viewport invariant enforced at **three** sites (move, resize, viewport-resize), and `winH` does not exist anywhere today | **verbatim** (§10.9). `App.svelte:28,67` still binds only `innerWidth`. |
| §6.9 — geometry persists per `(labId, paneKey)`; **membership does not** | **verbatim** (§10.10). |

**Four additions this redesign genuinely needs and P7 §6 did not scope:**

1. **Minimize + a minimized-console launcher** (brief:70-71). P7's title bar carries name,
   state LED, pin and close (P7 §6.4) — **no minimize**, and no launcher concept anywhere.
   §10.5 adds `minimized: Set<paneKey>` and a launcher strip.
2. **A placement policy** — "avoid opening new consoles directly over the selected node or
   the center of the visible topology" (brief:72). P7's `ensureWindow` cascades from a
   top-left origin by `24 * (index % 8)` and knows nothing about node positions. §10.4
   replaces it, and in doing so fixes the sol-medium finding that `setPlacement` cannot
   reach the viewport or `labStore` from inside the store.
3. **Float as the default placement.** P7 §6.1.1 defaults `"dock"`; the redesign's whole
   premise is a canvas that "should occupy essentially the entire window" (brief:65). §10.2
   defaults `"float"` with a one-time migration note.
4. **The `lens` pane arm.** §4.6.

**Two corrections to P7 §6 that a copy-paste implementer would otherwise inherit:**

1. **P7 §3.3's inventory of `Console.svelte`'s private state is two panes out of date.**
   It lists a console arm and a capture arm. The as-built file has **three**:
   `openLensTabs` tabs at `Console.svelte:337-362` and a `LensPane` arm at `:561-578`.
   `PaneBody.svelte` must have three arms or floating mode silently loses the Protocol Lens.
2. **Every line number in P7 §3.3, §6.3 and §6.4 is stale.** `nativeCapture` is at
   `Console.svelte:26` (P7 says `:25`), `searchOpenFor` at `:16` (P7 says `:15`), the
   Wireshark one-shot `$effect` at `:152-158` (P7 says `:140-146`), the `.native-hold` card
   at `:500-556` (P7 says `:455-514`), the console arm at `:451-480` and the capture arm at
   `:481-560`. P7 §2's "re-grep before editing" applies to P7's own citations.

**P7 Batches 11a and 11b (the per-node MAC list and the IOL-MAC toggle) are untouched by
this plan and remain implementable as written.** They share no file with any batch here
except `TopBar.svelte` (11b's toggle) and `NodeActions.svelte` (11a's button) — §6 records
the collision and §17 records that this plan does not schedule them.

---

## 4. Where the brief's design does not survive the code — nine corrections

### 4.1 The mockup shows six rail icons; the brief specifies five groups. The brief wins.

The mockup's rail reads, top to bottom: a filled accent **+**, a cursor/arrow, a link/chain,
a screen-with-arrow, an eye, a gear. The brief's Left Toolbar section (brief:20-30) names
exactly five, in order: **Add Nodes, Node Actions, Add Text, Add Shapes, Tools** — and the
first acceptance criterion is *"The left rail exposes exactly the five primary groups above
and expands only on demand"* (brief:83).

**Correction (decided):** implement the five named groups. The mockup's cursor and link
glyphs are not new features — selection is already the canvas default
(`CanvasInner.svelte:987` `selectionOnDrag={!panMode}`) and link-add is already a
hover-connector on the node face (`DESIGN.md` §5, "Connector affordance"), neither of which
should be moved into a rail button that would then have to arm and disarm a mode the canvas
does not have. **Do not add a "link mode".** The mockup's eye and gear map onto **Tools**
(Network Watcher is literally an eye glyph today, `Palette.svelte:195-198`) and onto the
**overflow menu** (§7), respectively.

### 4.2 The mockup's inline link subnet labels have no data source anywhere in this product

The mockup renders `10.0.0.0/30`, `192.168.1.0/24` etc. on each cable, and `10.1.1.10` under
each PC. **Nothing in this codebase knows a node's IP address.** `LabEndpoint` is
`{ node: number; interface: string }` and nothing else (`labTypes.ts:50-53`); `LabNode`
(`:25-43`) has no address field; `LabLink` (`:55-70`) has no subnet field;
`contracts/lab.schema.json` mirrors that. The only IP-shaped strings in a lab document live
inside the opaque `startupConfig` blob (`labTypes.ts:39`), and nothing parses it — the
supervisor injects it into NVRAM verbatim.

**Correction (decided): out of scope, named as a non-goal (§17), not silently dropped.**
Deriving subnets would mean either (a) an IOS `ip address` parser over `startupConfig`,
which is wrong the moment a learner configures an address from the CLI instead of the day-0
config — the *normal* case for a certification lab — or (b) a live `show ip interface brief`
poll per node, which is a new supervisor verb, a new poll loop and a new staleness model.
Both are their own project. **A label that is confidently wrong about a lab's addressing is
worse than no label**, and this repo has a standing rule about that (P6 §4.2's never-guess
discipline). The mockup's cable labels are therefore treated as *placeholder art for the
interface labels §12 actually implements*.

### 4.3 The link hover target already exceeds the brief's spec, and the two-ended interface labels already exist

The brief asks for *"a generous invisible pointer target, approximately 10–14 px wide"*
(brief:53) and for hovering to reveal *"the interface name at both ends … do not collapse
them into one ambiguous center label"* (brief:54-55).

Both are built. `.edge-hover-catch` is a transparent stroke of **18px** with
`pointer-events: stroke` (`FloatingEdge.svelte:422-428`, CSS at `:631-638`) — wider than
asked. Two `EdgeLabel`s render one `.port-chip` per endpoint at `t = 0.22` and `t = 0.78`
along the edge's own curve (`:466-490`, `:493-519`, anchors computed at `:310-315`); each
chip shows its **own** interface at rest and reveals `"<nodeName> <iface>"` on hover via
`.chip-detail` (`:475`, `:503`, hidden/revealed at `:743`, `:757`). Hovering either the
cable or either chip sets the shared `hot` flag (`:151`, `:425-426`, `:471-472`, `:499-500`)
which glows the whole link and raises both labels.

**Correction (decided):** §12 must **not** rebuild any of this and must **not** shrink the
18px catcher to "14px per spec". The real gaps §12 closes are three and only three:
**(a)** *selection* does not do what hover does — `selected` only recolors the stroke
(`:366`, `:562-565`) and never sets `hot`, so the brief's *"clicking a link selects it and
keeps the same endpoint emphasis visible until selection changes or Escape is pressed"*
(brief:57) is genuinely absent; **(b)** there is no **Escape** handler that clears
`labStore.selectedLinkId`; **(c)** the chips are not viewport-clamped (brief:56) — an
`EdgeLabel` at the screen edge clips.

### 4.4 There is no snap grid, and adding one is a one-prop change — but the *independence* criterion is the design work

Grepping `snapGrid|snapToGrid|SnapGrid` across `app/src` returns **zero hits**.
`@xyflow/svelte`'s `SvelteFlow` accepts a `snapGrid` prop
(`app/node_modules/@xyflow/svelte/dist/lib/container/SvelteFlow/SvelteFlow.svelte.d.ts:36`),
and the canvas already paints a dot grid at `gap={20}` (`CanvasInner.svelte:1000`), so
`snapGrid={[20, 20]}` aligns node drops to the dots the user can already see.

**Correction (decided):** the work is not the prop, it is honoring the acceptance criterion
*"Snap grid and link layout are independent controls"* (brief:87). §11.6 puts them in
**separate** persisted fields with **separate** menu entries and forbids any code path where
one reads the other — including the tempting one, where Structured routing "just snaps
elbows to the grid whenever snap is on". Structured routing quantises its own lanes
unconditionally (§11.5); it never reads the snap preference.

### 4.5 `NodeActions.svelte` is hover-triggered and has no Configure or Restart; the mockup's toolset is selection-triggered and has both

> **Revised by review findings 5 and 6.** The original text of this subsection asserted that
> "Restart is not a verb this product has" and keyed the selection reveal off
> `labStore.inspectorNodeId`. Both were wrong against the code. The corrected text follows;
> §9 carries the design.

`NodeActions.svelte` (183 lines) is rendered by all four node components and shows, state-
gated: **Start** when not busy (`:54-57`), **Stop** when busy (`:58-61`), **Console** when
running (`:62-65`), **Save config** when running *and* IOL (`:66-74`), **Wipe** when not
busy (`:75-78`), replaced wholesale by a spinner while `labStore.nodeLocks[nodeId]` is set
(`:48-52`). It is revealed by `:global(.face-node:hover) .node-actions` with a 120ms
forgiveness delay (`:121-130`) and it is **inside the zoomable canvas** (`z-index: 5`,
`:107`), so it scales with zoom.

The mockup shows a wider, labelled bar reading **Console · Configure · Restart**, anchored
below the *selected* node.

**Correction (decided): extend `NodeActions.svelte`; do not build a second component — but
do not adopt the mockup's three buttons literally either.**

- **Extend, because the alternative duplicates five state gates and the `nodeLocks`
  spinner.** A new selection-anchored component would need every rule at `:17-26` and
  `:48-52` re-derived, and the `onpointerdown={(e) => e.stopPropagation()}` on every button
  (`:55`, `:59`, `:63`, `:71`, `:76`) which is load-bearing — without it, pressing a button
  starts a node drag (`:7-10` says so).
- **Add `focus-within`-equivalent reveal on selection**, so the bar appears for
  **`labStore.selectedNodeId === nodeId`** as well as on hover. That is a one-line `class:`
  binding plus one CSS rule; it satisfies the brief's *"floating per-node action toolset …
  near a selected node"* without a rewrite. Hover reveal **stays** — removing it would be a
  regression with no acceptance criterion behind it.
- **Selection is `selectedNodeId`, NOT `inspectorNodeId` — they are two different things and
  this plan must never conflate them** (review finding 6). A plain left-click on a node runs
  `onNodeClick`, which sets **only** `labStore.selectedNodeId` (`CanvasInner.svelte:560`); a
  right-click sets it too (`:533`). `inspectorNodeId` is set **only** by `openEdit(nid)`
  (`:503-506`, which sets *both*), reached from the node context menu's Edit entry, from
  `linking.requestEdit`, and from the double-click path — a strictly narrower, explicitly
  "open the Inspector" action. `labStore` documents the split itself at `:38-43`
  ("Which node's Inspector pane is open, **independent of** `selectedNodeId`"), and
  `App.svelte:63` opens the right-hand pane on `inspectorNodeId`. **Keying the toolset off
  `inspectorNodeId` would mean the toolset never appears on an ordinary node click** — the
  exact interaction the brief specifies. Every selection-triggered feature in this plan
  therefore reads `selectedNodeId`. Both are cleared together by `onPaneClick` (`:584`,
  `:587`) and by node deletion (`labStore.svelte.ts:1223-1224`), so the "click empty canvas
  dismisses it" behavior is unchanged by the correction.
- **"Restart" IS a verb this product has, and it ships** (review finding 5 — this reverses the
  draft's decision). The evidence, all four legs:
  - the verb is registered: `s.disp.Handle("node.restart", s.handleNodeRestart)`
    (`supervisor/internal/server/server.go:225`);
  - the handler already does the ordered stop→start server-side:
    `s.stopNode(ll, args.Node)` then `return s.startNodes(ll, []int{args.Node})`
    (`supervisor/internal/server/handlers.go:523-533`) — one RPC, one round trip, and it does
    not resolve until the start leg has been issued;
  - the typed client method already exists:
    `nodeRestart(labId, node) → call<LabStartResult>("node.restart", …)`
    (`app/src/lib/supervisor.ts:191-193`), sitting between `nodeStart` and `nodeSetImage` with
    the comment at `:163` explicitly covering "node.start/stop/restart";
  - the reaper race that motivated the draft's refusal is **already handled** in the state
    machine: `ReapDecision` (`supervisor/internal/node/state.go:81-95`) keys off
    `explicitlyStopped` recorded on the *Process instance*, precisely so "a fast
    stop-then-restart of the same node id reuses the same Machine for a brand-new Process"
    cannot misattribute the old process's exit as a crash of the new one. That comment
    describes this exact scenario.

  **Decision: the toolset gains a Restart button**, wired to a new
  `labStore.restartNode(nodeId)` that wraps **one** `nodeLock` around the **one** existing
  `node.restart` call. §9.1 carries the full design, including the one subtlety the draft's
  race worry was groping at: the intervening `node.state="stopped"` event would release an
  ordinary lock early.
- **"Configure" already exists under another name — unchanged, and the review did not
  challenge it.** Selecting a node already exposes the Inspector path (`App.svelte:62-64`,
  `labStore.inspectorNodeId`), and the node context menu's Edit entry calls `openEdit`. The
  bar gains **no** Configure button. Adding a button that does what the click that revealed it
  already did is chrome for chrome's sake — and `DESIGN.md` §6 forbids exactly that class of
  decoration. (Note the correction above does *not* weaken this: plain selection does **not**
  open the Inspector today, but "Configure" is still reachable in one click from the node's
  own context menu, which is where every other per-node configuration action lives.)

### 4.6 `Console.svelte` has three pane kinds, not two — P7's `PaneBody` design must grow an arm

Covered in §3.2, correction 1. Stated here as a numbered correction because it is the single
easiest way for Batch 14 to ship a green build with a missing feature: floating mode with no
Protocol Lens looks completely finished.

### 4.7 There is no RTT telemetry, and no lab-uptime clock — the resource bar has a real backend gap

The mockup's bottom bar reads `CPU 12% · RAM 28% · RTT 2ms · 00:27:43`.

- **CPU and RAM exist and are already rendered.** `host.stats` carries
  `{cpuPct, memUsed, memTotal, diskUsed, diskTotal, cores}` (`protocol/verbs.go:617-621`,
  emitted every `statsInterval = 2s` at `server/stats.go:66-76`, mirrored into
  `labStore.hostStats` at `labStore.svelte.ts:132-138` / `:524-526`). `Palette.svelte:412-433`
  already draws three bars from it. The mock transport synthesises it too
  (`mockTransport.ts:100-132`), so the resource bar is verifiable in the dev server.
- **RTT does not exist.** `grep -rni "rtt|latency"` over `app/src/lib/protocol.ts` and
  `supervisor/internal/protocol/*.go` returns **nothing**. There is no ping, no
  round-trip measurement, no transport-level timing anywhere in the wire protocol.
- **Elapsed time does not exist.** Nothing records when a lab started. `labStore` has
  `nowTick` (`:142`, a 1s clock at `:198`) and `nodeLocks[].startedAt` (`:61`) — a per-action
  timestamp, not a lab clock. `labStore.labRunning` (`:83`) is derived from node states.

**Correction (decided), and it is a scope decision the orchestrator must make explicitly:**

- **CPU, RAM and connection status ship in §13 with no backend work.** Connection status is
  `labStore.providerStatus` + `labStore.activeProvider`, already rendered as a status pill at
  `TopBar.svelte:142-150` and moved down in §13.
- **Elapsed time ships in §13 as a *client-side* lab clock**, set when `labRunning` goes
  false→true and cleared on stop. It is honest ("time since the GUI observed the lab start"),
  it needs no protocol change, and it survives nothing — a page reload resets it, which §13.4
  requires the tooltip to say. A *durable* uptime would need a supervisor `startedAt`, which
  is deferred.
- **RTT is deferred as an explicit non-goal (§17), and the fallback recipe is named honestly
  so a later slice neither redesigns it nor mislabels it** (review finding 11 corrected the
  label): the browser already round-trips every request through `supervisor.ts`'s
  request/response client, so a `Date.now()` delta around an in-flight request, exponentially
  smoothed, is buildable with **zero protocol change**. **But that number is not an RTT of any
  kind.** The pending-request map (`app/src/lib/supervisor.ts:98`) records no transport-level
  timestamps — there is no send-time/receive-time pair on the wire and no echo verb — so the
  delta necessarily includes the supervisor's own handler execution (which for verbs like
  `image.list` is observed at 10–15s, per `labStore.svelte.ts:66-72`'s own comment). The
  correct name for it is **"RPC response time"**, and that is the only name a later slice may
  ship it under. It is neither the lab's data-plane RTT nor a transport latency; the mockup's
  placement next to CPU/RAM invites reading it as the former. **Do not ship a number labelled
  "RTT", "latency" or "link" that means something other than what a network engineer will
  assume it means.** A genuine transport RTT would need a new ping/pong verb pair carrying
  send timestamps — that is the deferred item, not the delta. §13.1 chooses: **ship nothing in
  this batch**, and leave the slot.

### 4.8 `ContextMenu.svelte` is the right base for the overflow menu but is not keyboard-operable

The final acceptance criterion is *"All controls are keyboard-operable, have accessible
names/tooltips, and respect reduced-motion preferences"* (brief:92).

`ContextMenu.svelte` gives §7 most of what an overflow menu needs for free: a
`MenuItem { label, action, danger?, disabled?, separator?, title?, submenu? }` type
(`:2-10`), fixed positioning from client coordinates (`:70`, `:109`), capture-phase
outside-dismiss that survives Svelte Flow's `stopPropagation` (`:41-43`, `:56`), Escape
(`:47-49`), and `role="menu"` / `role="menuitem"` (`:70`, `:81`).

What it does **not** have: **any arrow-key navigation or roving focus** — there is no
`keydown` handler beyond Escape, no `tabindex` management, and no focus is moved into the
menu on open. It also keys its `{#each}` by `item.label` (`:71`), which breaks the moment two
entries share a label, and it dismisses on `wheel` capture (`:44-46`, `:57`) — correct for a
canvas-anchored menu, **wrong for a top-bar menu the user may scroll under**.

**Correction (decided):** §7.3 extends `ContextMenu.svelte` with roving focus + Up/Down/Home/
End/Enter and an opt-out for the wheel dismissal, and keys the `{#each}` by a new required
`id` field. This is a shared-component change that benefits the four existing canvas menus
too — and it is the reason §7 is not "just move some buttons".

### 4.9 The brief's visual direction contradicts `DESIGN.md` in three places — align, do not delete

The brief: *"Preserve the current dark IOLBOX character and existing design tokens where
possible. Favor restrained cyan accents, crisp borders, compact spacing, Segoe UI Variable
for interface text, and Cascadia Code for console/interface identifiers. Avoid excessive
glow, gradients, glass effects, oversized cards, decorative pills, or generic dashboard
styling. Use color primarily for state and action, not decoration."* (brief:74-79)

**Confirmed alignment — most of that is already mandated, verbatim:**
- Segoe UI Variable + Cascadia Code, and the semantic split between them, is `DESIGN.md`
  §3 and its **Mono-For-Data Rule**; the tokens are `--font-ui` / `--font-mono` in
  `app/src/styles/theme.css`.
- Restrained cyan is `--accent: #4bc6d1` (`theme.css:95`) under the **One Accent Rule**
  (`DESIGN.md` §2: "≤10% of any screen … If two things on screen are both accent-colored and
  neither is selected/active/primary, one is wrong").
- "color primarily for state and action" is the **LED Rule** (`DESIGN.md` §2) plus the
  reserved four-color state set `--state-running/-starting/-crashed/-stopped`.
- "no generic dashboard styling" is `DESIGN.md` §6: *"Don't build metrics-dashboard chrome —
  no hero-number tiles, no identical icon-heading-text card grids."*
- "crisp borders / flat at rest" is the **Flat-At-Rest Rule** (`DESIGN.md` §4).

**Three genuine conflicts, resolved:**

1. **"Avoid … glass effects."** `DESIGN.md` §1 and §4 make the Glass theme's
   `backdrop-filter: saturate(180%) blur(20px)` **load-bearing theme identity**, and
   `themeStore.svelte.ts` ships Bench/Glass as a first-class user choice with a segmented
   control at `TopBar.svelte:152-163`. **Resolution: the brief is describing the Bench (dark)
   direction it was written against; it does not authorize deleting a theme.** Read "avoid
   glass effects" as `DESIGN.md`'s existing *"Don't use glassmorphism decoratively"* — new
   surfaces get `--blur` only where an existing floating panel already does
   (`WatcherPanel`, `PainterPanel`, `NodeActions.svelte:96-97`). **No batch may remove the
   Glass theme or the theme toggle.**
2. **"Avoid … gradients."** Two gradients are identity, not decoration: the brand mark
   (accent→violet, `TopBar.svelte:252-256`) and the node face's `160deg` tonal gradient
   (`DESIGN.md` §5, "Node face (signature)"). **Resolution: both survive. No batch
   introduces a new gradient.** That is the enforceable form of the instruction.
3. **"Avoid excessive glow."** `DESIGN.md` §4 calls the colored glow *"the most expressive
   elevation material here"* and spends it on selection, focus, live traffic and STP state.
   **Resolution: "excessive" is the operative word.** No new glow is introduced by any batch;
   §14's pass audits for glow on *resting* surfaces (which `DESIGN.md` already forbids) and
   removes any it finds.

**And one thing the brief needs that `DESIGN.md` already supplies:** `theme.css:54` declares
**`--statusbar-h: 26px`**, which is currently **used nowhere in `app/src`**. The bottom
resource bar's height token already exists and was reserved for exactly this. §13 uses it.

**Token budget.** The Impeccable design-drift hook runs on every `Edit|Write|MultiEdit`
(`~/.claude/plugins/marketplaces/impeccable/plugin/hooks/hooks.json`) and flags colors
outside `DESIGN.md`'s palette. **The good news is that this redesign needs no new color
token.** The resource bar's status dots reuse `--state-running` / `--state-starting` /
`--state-crashed` (`theme.css`, mirrored in `DESIGN.md`'s frontmatter at
`state-running: "#39d98a"`, `state-starting: "#f0b429"`, `state-crashed: "#ff5a5f"`), which
is also what the LED Rule requires. §14.2 is nevertheless a **deliberate `DESIGN.md` step**
for the two things that *are* new and are not colors: the rail's 40×40 hit-area token and the
floating-window chrome entry in §5's component list. Any batch that finds itself typing a hex
literal has made a mistake — stop and add the token to `DESIGN.md` first.

---

## 5. Facts established by reading the code (do not re-derive)

### 5.1 `TopBar.svelte` — the complete current inventory, and what moves

380 lines. Rendered once, unconditionally, at `App.svelte:70`. Height `var(--topbar-h)` = 48px
(`:228`, `theme.css:53`), `z-index: 5` (`:238`). Left-to-right, **every** control it renders
today:

| # | control | lines | handler | **§7 disposition** |
|---|---|---|---|---|
| 1 | brand mark (accent→violet gradient square) | `:118-119`, CSS `:246-259` | — | **STAYS** |
| 2 | lab-name inline input (`.lab-name mono`) | `:120-126`, `updateName` `:37-39` | writes `labStore.lab.name` | **STAYS** |
| 3 | node-count chip `· N nodes` | `:127`, `nodeCount` `:14` | — | **STAYS** (mockup shows it) |
| 4 | flex spacer | `:130` | — | stays |
| 5 | error pill (conditional, dismiss-on-click) | `:132-140` | clears `labStore.lastError` | **STAYS** — it is an error state, and brief:18 forbids auto-hiding one |
| 6 | provider status pill + LED | `:142-150`, `providerLabel` `:11-13` | — | **MOVES to the resource bar** (§13) — it is telemetry, and brief:17 asks for "connection status" there |
| 7 | Theme segmented control (Bench/Glass) | `:152-163` | `themeStore.set` | **MOVES to overflow** |
| 8 | **New** | `:165-167`, `newLab` `:52-54` | `labStore.openLab(emptyLab(…))` | **MOVES to overflow** (brief:14 names it) |
| 9 | **Labs** | `:169-171` | `labStore.showLabBrowser = true` | **MOVES to overflow** (brief:14 names it) |
| 10 | **Tasks** (toggle, `aria-pressed`) | `:173-181` | `labStore.showTasks` | **MOVES to overflow** (brief:14 names it) |
| 11 | **Save** (with 1.6s "Saved ✓" flash) | `:183-185`, `save` `:56-62` | `labStore.saveLab()` | **MOVES to overflow** (brief:14 names it) |
| 12 | Export YAML | `:188-190`, `exportYaml` `:76-78` | `labToYaml` + `download` `:64-72` | **MOVES to overflow** (brief:14, "import/export") |
| 13 | Export containerlab | `:191-193`, `exportClabFile` `:80-82` | `exportClab` | **MOVES to overflow** |
| 14 | Import | `:194-196`, `pickImport` `:84-86` | clicks the hidden `<input type=file>` | **MOVES to overflow** |
| 15 | hidden file input | `:198-204`, `onImportFile` `:87-112` | — | **stays in `TopBar.svelte`'s markup** (§7.4: a menu entry may not own a file input that unmounts on menu close) |
| 16 | **Images** | `:206-208` | `labStore.showImageManager = true` | **MOVES to overflow** (brief:14 names it) |
| 17 | fullscreen toggle | `:210-218`, `toggleFullscreen` `:29-35`, synced from `onfullscreenchange` `:26-28`/`:115` | Fullscreen API | **MOVES to overflow** (brief:14 names it) |
| 18 | **Start lab / Stop lab** (`.btn-primary`) | `:220-223`, `toggleLab` `:41-47` | `labStore.startLab/stopLab` | **STAYS** — the brief's "primary Run/Stop control" (brief:13) |

The mockup additionally shows a **Saved / N nodes** status pair next to the lab name. "Saved"
has a data source: `labStore.lastSavedAt` (written in `saveLab` at `labStore.svelte.ts:671`)
plus the debounced autosave at `:778-783`. §7.2 renders it as an LED-backed state chip per the
LED Rule, not a bare word.

### 5.2 `Palette.svelte` — 832 lines, and it has no search at all

Mounted at `App.svelte:91` inside a `SplitPane` (min 180, max 360, `storageKey
"iolbox.split.palette"`), or replaced by a 20px `.palette-rail` chevron button
(`App.svelte:73-81`) when `paletteUiStore.collapsed` (`paletteUiStore.svelte.ts`, key
`"iolbox.palette.collapsed"`).

Its complete section inventory:

| section | lines | contents |
|---|---|---|
| collapse chevron | `:97-104` | `paletteUiStore.toggle()` |
| **Session** | `:105-115` | Start all (`startAll` `:48-50`), Stop all (`stopAll` `:51-53`) |
| session rows | `:117-166` | Save configs (`:118-129`, gated on `hasRunningIol` `:38-42`), Console all (`:130-143`, gated on `hasRunningNode` `:44-46`), Force clean (`:144-154`), **Wipe all** (`:155-165`, `.danger`, `confirm()` at `:55`) |
| **Console** | `:168-187` | Web/Native segmented control → `consoleUiStore.setConsoleMode` |
| **View** | `:189-210` | Network watcher (`watcherStore.togglePanel()`), Topology painter (`painterStore.togglePanel()`) |
| **Nodes** | `:212-283` | VPCS (`:214-226`), PC (`:228-241`), Network tools (`:243-262`, gated on `toolPacks.length`), NAT gateway (`:264-283`, gated on `hasNat` `:29`, with the slirp egress warning `:31-36`) |
| **IOL images** | `:285-319` | one `.palette-item` per `labStore.images` entry (`:293-315`), loading/empty hints (`:286-292`), "Manage images…" (`:317-319`) |
| **Draw** | `:321-408` | five `.draw-btn` tools — text/rect/ellipse/note/line (`:322-386`) → `annoTool.arm()`; the `ANNO_COLORS` swatch row (`:387-399`); the armed hint (`:400-408`) |
| **Host** | `:410-438` | CPU/RAM/Disk bars from `labStore.hostStats` (`:412-430`), `{cores} vCPU`, supervisor build string (`:434-438`) |

**Answering the question directly: is `Palette.svelte`'s search/list logic reusable inside a
flyout?** *There is no search logic to reuse.* `grep -n "search"` over the file matches only
a CSS `filter: grayscale(…)` at `:695`. There is no filter input, no `$state` query, no
matching function. The device list is **three hand-written `.palette-item` blocks plus one
`{#each labStore.images}`** — not a data-driven list.

**Consequence for §8, and it is the batch's central design fact:** the Add Nodes flyout's
searchable, categorized list (brief:24) is **new construction over a new derived model**, not
a lift-and-shift. What *is* directly reusable is (a) the `onDragStart` payload contract
(`:11-18`: `dataTransfer.setData("application/iolbox-node", JSON.stringify({kind, imageId,
packId}))`), which `CanvasInner`'s drop handler already parses and which **must not change**;
(b) every gating derivation (`hasNat`, `natSlirp`, `toolPacks`, `defaultToolPack` `:22-25`,
`iolImages`); and (c) the whole Session/rows block, which moves into the **Node Actions**
flyout essentially verbatim because the brief's list for that flyout (brief:25 — "Save
Configs, Start All, Stop All, Console All, Force Clean, and Wipe All") is *exactly*
`Palette.svelte:105-166`, in a different order.

### 5.3 `NodeActions.svelte`

Covered in §4.5. Two additional facts §8/§9 depend on: it is rendered by **all four** node
components (`IolNode.svelte`, `VpcsNode.svelte`, `PcNode.svelte`, `ToolNode.svelte`), so one
edit reaches every node kind; and its buttons are 22×22 with 12px glyphs (`:131-141`,
`:149-153`), which is **below** the brief's 40×40 hit-area floor — but that floor is stated
for the *rail* (brief:30), not for in-canvas node chrome, and enlarging in-canvas buttons
would occlude the node. §9 does not resize them.

### 5.4 Edge geometry — what actually computes the path today

**There is no routing library and no built-in edge type in use.** `edgeTypes = { floating:
FloatingEdge }` (`CanvasInner.svelte:52`) is the only registration, and `FloatingEdge.svelte`
builds its own path.

The producer is one `$derived.by` block, `geom`, at `FloatingEdge.svelte:248-349`:

1. `getEdgeParams(s, t)` (`edges/floating.ts:78-89`) returns
   `{sx, sy, tx, ty, sourcePos, targetPos}` — the centre-to-centre intersections with each
   node's rectangle, **plus a `Position` for each end** (`floating.ts:52-66`,
   `getEdgePosition`). **`FloatingEdge` currently discards `sourcePos`/`targetPos`
   entirely** — it destructures only `raw.sx/sy/tx/ty` (`:254-255`, `:273-276`). This is the
   single most important fact in §11: the tangent sides an orthogonal router needs are
   already computed and already thrown away.
2. Parallel-link fan-out: `parallel` (`:221-236`) groups links by unordered node pair **read
   live from `labStore.lab.links`, deliberately not from xyflow's `data`** (`:212-220`
   explains why); `parallelSign` (`:237-241`) = `index - (count-1)/2`; `parallelOffset`
   (`:242`) = sign × `PARALLEL_SPACING` (26, `:205`); endpoint anchors slide along the node
   border by sign × `ENDPOINT_SPACING` (10, `:211`), clamped to the node radius (`:266-276`).
3. The path is one quadratic: `M sx sy Q cx cy tx ty` (`:288`), with the control point at the
   midpoint pushed perpendicular by `2 × parallelOffset` (`:284-286`).
4. **`at(u)` (`:291-297`) is a closed-form quadratic Bézier evaluator, parametrized by the
   curve PARAMETER `u`, not by arc length.** It is the only reason chips, the Watcher chip and
   the fault pill can sit *on* the cable — and §11.2a is where the plan commits to keeping that
   semantics rather than promising a distance-based one it would then have to break.
5. **`offsetPath(d, reversed)` (`:329-336`)** produces a *parallel* quadratic by translating
   all three control points along the same normal — valid **only** because the curve is a
   single quadratic with one global normal basis.
6. The returned object is `{path, offsetPath, sChip, tChip, watcherChip, sOrigin, tOrigin}`
   (`:338-348`).

Everything in the template — **`:352` to the end of the template (`:552`), not `:352-530`** —
reads that object. See §11.2b for the corrected inventory of **twelve** consumers (the draft
said ten and stopped at `:530`, missing the best-path metric pill at `:537` and the Watcher
label pills at `:545`).

### 5.5 What `@xyflow/svelte` actually ships for orthogonal routing

`app/package.json` pins `@xyflow/svelte ^1.6.1`. Its type surface (`dist/lib/index.d.ts`)
exports **`getSmoothStepPath`, `getBezierPath`, `getStraightPath`** and the edge components
**`SmoothStepEdge`, `StepEdge`**. `getSmoothStepPath`'s parameters
(`@xyflow/system/dist/esm/utils/edges/smoothstep-edge.d.ts:2-33`) are
`{sourceX, sourceY, sourcePosition?, targetX, targetY, targetPosition?, borderRadius = 5,
centerX?, centerY?, offset = 20, stepPosition = 0.5}`, and it returns the tuple
`[path, labelX, labelY, offsetX, offsetY]` (`:65`).

Read precisely, that gives §11 three things and withholds three:

**Gives:** (a) a correct, battle-tested elbow builder that already handles the
same-side/opposite-side/diagonal cases; (b) `borderRadius: 0` for true Manhattan corners;
(c) `centerX`/`centerY` and `stepPosition`, which are the knobs a **shared lane** is built
from — two links whose elbows are pushed to the same `centerX` share a vertical lane.

**Withholds:** (a) **no node avoidance** — it will route straight through an intervening
node; (b) **no path evaluator** — it returns a string plus a single label point, so §5.4's
`at(u)` has no counterpart; (c) **no parallel offset** — `offsetPath` has no counterpart
either.

### 5.6 Selection, hover and the canvas's own state

- Link selection is `labStore.selectedLinkId`, set by `onEdgeClick`
  (`CanvasInner.svelte:565-567`, wired at `:995`) and cleared by `onPaneClick` (`:571-587`)
  and four other sites (`:522`, `:557`, `:561`, `:611`, `:654`). The edge receives it as
  xyflow's `selected` prop via `buildEdges` (`:167`).
- **Node selection is `labStore.selectedNodeId`; the Inspector is `labStore.inspectorNodeId`;
  they are two fields with two different meanings** (corrected per review finding 6 — the
  draft of this bullet said node selection *was* `inspectorNodeId`, and §5.6/§9 were built on
  that error):
  - `selectedNodeId` (`labStore.svelte.ts:38`) is set by **`onNodeClick`** (`CanvasInner.svelte:560`
    — a plain left-click, which sets *nothing else* node-wise), by `onNodeContextMenu`
    (`:533`), by the drop-create path (`:437`), by the bulk paste path (`:709`), and by
    `openEdit` (`:504`). It is what `buildNodes` feeds to xyflow as `selected` (`:86`), i.e.
    it is what the canvas itself draws as selected.
  - `inspectorNodeId` (`:43`, documented there as "independent of `selectedNodeId`") is set
    by **`openEdit(nid)` only** (`:503-506`) and drives the right-hand pane (`App.svelte:63`).
    `openEdit` is reached from the node context menu's Edit entry, from
    `linking.requestEdit` (`:388`) and from double-click — never from a plain click.
  - Both are cleared together by `onPaneClick` (`:584`, `:587`) and by node removal
    (`labStore.svelte.ts:1223-1224`). `onEdgeClick` clears `selectedNodeId` (`:567`) but
    deliberately leaves `inspectorNodeId` alone.
  - **Consequence for this plan: anything keyed on "a node is selected" reads `selectedNodeId`.**
    `inspectorNodeId` may be read *only* by something that is genuinely about the Inspector
    pane being open. §9 is the one place this bites.
- There is **no global Escape handler** for selection. `ContextMenu.svelte:47-49` and
  `CanvasInner`'s annotation-tool disarm handle their own Escape; nothing clears
  `selectedLinkId`.
- `hot` is per-edge-instance component state (`FloatingEdge.svelte:151`) and is deliberately
  not shared — `:417-421` explains that this is what stops a background sibling's chip from
  rendering over the hovered edge's.

### 5.7 Telemetry and the resource bar's data sources

Covered in §4.7. Three additional facts §13 needs:

- `statsLoop` emits `host.stats` **unconditionally every 2s** whenever `memTotal > 0`
  (`server/stats.go:61-76`) — it is not gated on a lab running, so the bar has data at idle.
- The GUI mirror is `labStore.hostStats` (`labStore.svelte.ts:132-138`), `null` until the
  first sample; `Palette.svelte:431-433` renders a "Waiting for host stats…" state that §13
  should match rather than showing zeros.
- `Palette.svelte`'s `tone()` (`:85-87`) returns **`var(--accent)`** for the healthy case.
  In a persistent bottom bar that would put the rationed accent on screen at all times for no
  state reason — a direct One Accent Rule violation. §13.3 uses `--state-running` instead.

### 5.8 Auto-hide: nothing like it exists, and "modal" / "error state" have concrete referents

`grep -rni "autohide|auto-hide|idle|mousemove"` over `app/src` returns only unrelated
matches: `App.svelte:57` (a comment about the *inspector's* empty-selection collapse),
`CaptureTerm.svelte:77` (a one-shot idle *hint* printed into the terminal), and
`Palette.svelte:432` ("Waiting for host stats…"). **There is no timer-based chrome
visibility mechanism anywhere.** §14.1 (Batch 18) is new construction.

The brief's never-hide exception list (brief:18) maps onto this codebase as follows, and §14.1
must enumerate exactly these. **The third column is the part the draft of this table omitted
and review finding 4 caught: for two of the five rows the state that must be observed is
component-local `$state` that nothing outside the component can read today, so the row is not
implementable without editing that component.** Those files are now in Batch 18's file list.

| brief's term | concrete referent | who owns the state, and what Batch 18 must do about it |
|---|---|---|
| "an open menu" | any mounted `ContextMenu.svelte` (four canvas call sites, `CanvasInner.svelte:1047-1073`), the §7 overflow menu, `AnnoStylePopover`, `ChangeImagePopover`, `IconPicker`, `InterfacePicker` | **Not observable today.** `ContextMenu.svelte` takes `{x, y, items, onClose}` (`:12-17`) and owns no open flag — "open" is the *parent's* `{#if nodeMenu}`, one private `$state` per call site (`CanvasInner.svelte:495-501` and `TopBar`'s new one). **Batch 18 edits `ContextMenu.svelte`** to take a chrome hold for its own lifetime (§14.1's `chromeStore.hold()`), which covers **every** call site — present and future — without the store enumerating any of them. The four non-`ContextMenu` popovers each take the same hold. |
| "active drag tool" | `annoTool.active !== null` (`annoTool.svelte.ts`, read at `Palette.svelte:68`); a `SplitPane` drag in progress; a `dragMove` drag in progress (§10.7); an xyflow node drag between `onnodedrag` and `onnodedragstop` (`CanvasInner.svelte:989`) | `annoTool.active` is already a shared rune store — readable as-is. **`SplitPane.svelte`'s drag is `let dragging = $state(false)` at `:40`, component-local and unexported** — `onPointerDown` (`:52`) sets it, the pointer-up path clears it. **Batch 18 edits `SplitPane.svelte`** to take/release the same hold around `dragging`. `dragMove.ts` is new in Batch 15 and takes the hold in its own start/end. The xyflow node drag is observable from `CanvasInner`'s existing handlers. |
| "focused control" | `document.activeElement` matching `:focus-visible` inside any chrome surface | Global DOM read; no component change. |
| "modal" | `labStore.showPreflight` (`Preflight.svelte`, z 3000 — the runtime-provider dialog), `labStore.showImageManager` (`ImageManager.svelte`, z 2000), `labStore.showLabBrowser` (`LabBrowser.svelte`, z 1100), and `SwitchLabDialog.svelte` (z 1200), which is **always mounted** at `App.svelte:157` and self-gates on `labStore.pendingSwitch` | All four are `labStore` fields — readable as-is, no component change. |
| "error state" | `labStore.lastError !== null` (the pill at `TopBar.svelte:132-140`), and `labStore.providerStatus === "error"` | `labStore` fields — readable as-is. |

**The sweep that produced the third column, so a later agent does not have to repeat it:**
`grep -rn "\$state(false)\|\$state<.*null>(null)" app/src/lib/components app/src/lib/nodes`
was read for anything that gates a menu, popover, dialog or drag. Everything found falls into
one of the five rows above; the only two owners that are component-local and unreadable from
outside are `ContextMenu.svelte` and `SplitPane.svelte`. Popovers with their own local open
flags (`AnnoStylePopover`, `ChangeImagePopover`, `IconPicker`, `InterfacePicker`) are mounted
by a parent `{#if …}` exactly like `ContextMenu`, so they take the hold the same way — four
one-line `$effect`s, listed in §14.1. **If an implementing agent finds a sixth suppression
owner, it adds a `hold()` — it does not add a row to `chromeStore`.**

### 5.9 Where a per-lab display preference can live, and what it costs

`LabCanvas` is `{zoom?, pan?, background?}` (`labTypes.ts:87-91`); the Go mirror is
`lab.Canvas` (`supervisor/internal/lab/lab.go:24-29`); the schema is
`contracts/lab.schema.json:24-37` with **`"additionalProperties": false`** at `:27`.

Two facts make a new field cheap:

1. **`lab.saveDoc` stores the document text byte-for-byte.** `handleLabSaveDoc`
   (`server/labstore.go:62-86`) writes `args.Lab` to `<LabsDir>/<id>.yml` verbatim — its own
   comment at `:61` says *"The text is stored exactly as received."* It never round-trips
   through `lab.Lab`. So an unknown canvas field survives save/load intact.
2. **Go's decoder ignores unknown fields.** `DisallowUnknownFields` appears exactly once in
   the whole supervisor (`server/pcstate.go:43`) and not on the lab path, and `Lab.Validate`
   (`lab/validate.go:29+`) checks version/ids/kinds/interfaces and never touches `Canvas`.

**Therefore §11.6's `lab.canvas.linkLayout` and `lab.canvas.snapGrid` need: `labTypes.ts`
(+2 fields) and `contracts/lab.schema.json` (+2 properties, because `additionalProperties:
false` means omitting them makes the contract *lie* about a document the GUI writes).
Zero Go changes.** Do not add them to `lab.Canvas` in `lab.go` — presentational fields the
supervisor never reads do not belong in its struct, and adding them invites someone to
validate them.

### 5.10 z-index, as of this working tree

Re-verified against P7 §3.5, which is still accurate: 1–6 node internals and `TopBar` (5);
20 `SplitPane` divider and `FloatingEdge`; 30 in-canvas node panels; 40–60 edge label pop,
STP popover, `WatcherPanel`, `PainterPanel`; **61–999 empty**; 1000 `ContextMenu` /
`ChangeImagePopover` / `AnnoStylePopover`; 1100 `LabBrowser`; 1200 `IconPicker` /
`InterfacePicker` / `SwitchLabDialog`; 2000 `ImageManager`; 3000 `Preflight`.

`DESIGN.md` §6 already calls this spread *"ad-hoc"* and mandates a **semantic z-index scale**
for new work (canvas → sticky topbar → panels → dropdown/menu → dialog → modal → tooltip).
§14.3 lands that scale as named tokens; §10.8's floating windows and §13's bottom bar consume
it rather than inventing numbers.

---

## 6. Batch ordering and independence

| batch | § | scope | files touched | shares files with | notes |
|---|---|---|---|---|---|
| **12** | §7 | slim top bar + overflow menu | `TopBar.svelte`, `ContextMenu.svelte`, `icons.svelte.ts` | 13, 15, 17 (`TopBar`); 15 (`ContextMenu` items) | `ContextMenu` change is additive and shared |
| **13** | §13 | bottom resource bar | new `ResourceBar.svelte`, `App.svelte`, `theme.css`, `Palette.svelte` (host block removal) | 12 (`TopBar` status pill), 14/17 (`App.svelte`) | needs 12 only for the status-pill move |
| **14** | §8 | left icon rail + flyouts | new `IconRail.svelte`, new `RailFlyout.svelte`, new `nodeCatalog.ts`, `Palette.svelte` (**deleted**), `paletteUiStore.svelte.ts`, `App.svelte`, `annoTool.svelte.ts` (read-only) | 13 (`Palette` host block), 12/17 (`App.svelte`) | biggest deletion; do after 13 so the host block has a new home |
| **14a** | §9 | selected-node action toolset (+ Restart) | `NodeActions.svelte`, `labStore.svelte.ts` (**`restartNode` + the lock flag only** — §9.1.3) | none | folded into 14; the only batch permitted a `labStore` diff |
| **15** | §10 | floating consoles | `Console.svelte`, new `PaneBody.svelte`, new `FloatingConsoleWindow.svelte`, new `FloatingConsoleLayer.svelte`, new `dragMove.ts`, `consoleUiStore.svelte.ts`, `App.svelte` | 17 (`consoleUiStore`, `App.svelte`) | supersedes P7 §6 (§3.2) |
| **16** | §11 | link display modes (Free / Structured) | `FloatingEdge.svelte`, new `edges/routing.ts`, `edges/floating.ts`, `CanvasInner.svelte`, `labTypes.ts`, `contracts/lab.schema.json`, `ContextMenu` items in `TopBar.svelte`, `labStore.svelte.ts` (**the `flowToScreen`/`canvasPan` mirror only** — §10.4, §12.1.4; may instead land with 17) | 17 (`FloatingEdge`, `CanvasInner`, `labStore` mirror), 12 (`TopBar` menu), 15 (**reads** the mirror) | **hardest batch** |
| **17** | §12 | link hover/selection + endpoint labels + chip clamping | `FloatingEdge.svelte`, `CanvasInner.svelte`, `labStore.svelte.ts` (the mirror, if 16 did not land it) | 16 (`FloatingEdge`, `CanvasInner`, `labStore`) | **must follow 16** — it asserts identical behavior in both modes |
| **18** | §14.1 | auto-hide chrome | new `chromeStore.svelte.ts`, `App.svelte`, `TopBar.svelte`, `IconRail.svelte`, `ResourceBar.svelte`, **`ContextMenu.svelte`**, **`SplitPane.svelte`**, **`AnnoStylePopover.svelte`**, **`ChangeImagePopover.svelte`**, **`IconPicker.svelte`**, **`InterfacePicker.svelte`**, **`dragMove.ts`** | 12, 13, 14, 15 (`dragMove`), 16/17 (none) | **must be last but one** — it can only suppress chrome that exists; the seven bolded files are the suppression owners finding 4 added (§5.8, §14.1) |
| **19** | §14 | visual/typography pass + `DESIGN.md` | `DESIGN.md`, `theme.css`, touch-ups across the above | everything | **must be last** |

**Merge order: 12 → 13 → 14 → 15 → 16 → 17 → 18 → 19.**

**Safe parallelization — exactly one pair, with one post-review caveat.** **15 (floating
consoles) and 16 (link routing) write no common file** and may run concurrently by two agents;
15 lives in `components/` + `consoleUiStore`, 16 lives in `edges/` + `CanvasInner` + the lab
types. **Caveat (review finding 1):** both now *depend on* the two-line
`labStore.flowToScreen` / `canvasPan` mirror. **16 owns it, 15 only reads it, and 15 must
degrade gracefully when it is null** (§10.4) — so 15 can land first, in either order, and
neither agent may add the lines twice. Every other adjacency shares a written file:

- 12, 13, 14, 15 and 18 all edit `App.svelte`. The edits are in different regions (top bar
  markup, a new bottom sibling, the left branch at `:73-93`, a new floating layer sibling, a
  new store registration), but they are the same file — **serialize them.**
- 16 and 17 both rewrite regions of `FloatingEdge.svelte`, and 17's core claim ("identical
  hover, selection and interface-label behavior in both modes", brief:58) is unverifiable
  before 16 exists. **Never parallel.**
- 18 reads state owned by 12/13/14/15. Running it early produces a hide mechanism whose
  exception list references components that do not exist. **It also now edits
  `ContextMenu.svelte` (finding 4), which 12 extends** — the two edits are in different regions
  (12 adds roving focus + an `id` key; 18 adds one `$effect`), but they are the same file:
  **serialize them**, which the 12 → 18 merge order already does.

**Collision with P7 Batches 11a/11b, if the orchestrator schedules them:** 11b edits
`TopBar.svelte` (its toggle) — collides with 12. 11a edits `NodeActions.svelte` — collides
with §9's selection-reveal change. Neither is scheduled here; if both plans run, land P8's 12
and 9 first, since they *move* the surfaces P7's additions attach to.

---

## 7. Batch 12 — slim top bar + three-dot overflow menu

### 7.1 Decisions locked

1. **The top bar keeps seven things** (§5.1, amended post-implementation by explicit user
   direction): brand mark, lab-name input, Saved/node-count status pair, the error pill
   (conditional), the fullscreen toggle, the Run/Stop primary button, and the new three-dot
   button. Everything else in §5.1's table moves. **Fullscreen is not in the overflow
   menu** — it was drafted there originally, then pulled back into the main bar; do not
   re-add a `view-fullscreen` menu item, that would duplicate the control.
2. **The overflow menu is `ContextMenu.svelte`, extended — not a new component.** §4.8's
   three additions (roving focus, `id`-keyed items, opt-out wheel dismissal) land in the
   shared component and benefit the four canvas call sites.
3. **`ContextMenu`'s `MenuItem` gains `id: string` (required) and `checked?: boolean`.**
   `checked` renders a leading state mark for the toggles that move in (Theme, Tasks, and
   later §11's link layout and snap grid) and sets `aria-checked` with
   `role="menuitemradio"` / `role="menuitemcheckbox"`. **`label`-keyed `{#each}` (`:71`)
   becomes `id`-keyed** — required, because "Bench" and "Glass" would otherwise be a valid
   duplicate-label pair inside one submenu.
4. **The hidden file input stays in `TopBar.svelte`'s markup** (`:198-204`), outside the
   menu. A menu item calls `pickImport()`; the menu closes on click (`ContextMenu:23-27`),
   and an input that unmounted with the menu could not deliver its `change` event.
5. **`toggleLab` keeps `.btn-primary` and stays the single loud action** (`DESIGN.md` §2's
   One Accent Rule: "the single primary action"). The three-dot button is `.btn` ghost.
6. **The bar's height does not change.** `--topbar-h: 48px` (`theme.css:53`) is already slim;
   the "slim" in the brief is about *content*, and removing ten controls achieves it. Do not
   retune the token in this batch — §14 owns type/spacing.
7. **Nothing about `labStore` changes.** Every moved control keeps its exact handler.

### 7.2 The Saved / node-count status pair

The mockup renders two dot-prefixed chips: `● Saved` and `● 9 nodes`. Per the LED Rule
(`DESIGN.md` §2 — "State is *never* communicated by text color alone"), both get a real LED
dot, reusing the `.status-pill .led` treatment already at `TopBar.svelte:300-315`.

- **Saved** derives from `labStore.lastSavedAt` (`labStore.svelte.ts:671`) and the pending
  autosave (`:778-783`). Three states: `Saved` (`--state-running`), `Saving…`
  (`--state-starting`), `Unsaved` (`--state-stopped`). The existing 1.6s `justSaved` flash
  (`TopBar.svelte:17`, `:59-61`) is **replaced** by this chip — a persistent state chip and a
  transient flash saying the same thing is redundancy, and the brief asks for the chip.
- **node count** is `nodeCount` (`:14`) unchanged; its dot is `--state-running` when
  `labStore.labRunning`, `--state-stopped` otherwise. That is the mockup's second green dot
  and it earns its color.

### 7.3 `ContextMenu.svelte` — the shared extension

- `MenuItem` becomes `{ id: string; label: string; action: () => void; danger?; disabled?;
  separator?; title?; checked?: boolean; submenu?: MenuItem[] }`.
- **Roving focus.** On mount, focus the first non-disabled, non-separator item. Handle
  `ArrowDown`/`ArrowUp` (wrap), `Home`/`End`, `Enter`/`Space` (activate), `Escape` (close —
  already at `:47-49`). Implement with a `tabindex="-1"` list and an index in `$state`, not
  by calling `.focus()` on query-selector results.
- **New prop `dismissOnWheel = true`.** The four canvas call sites pass nothing and keep
  today's behavior (`:44-46`, `:57`); the top-bar menu passes `false`.
- **`{#each items as item (item.id)}`** at `:71`, and the same in the submenu block at `:87`.
  Update all four canvas builders (`buildNodeMenuItems`, `buildSelectionMenuItems`,
  `buildLinkMenuItems`, `buildAnnoMenuItems` in `CanvasInner.svelte`) to emit `id`.
- **Do not change** the capture-phase outside-dismiss (`:41-43`, `:56`) — its comment at
  `:35-40` explains it exists because Svelte Flow swallows bubbling pointer events.

### 7.4 Concrete file-level changes

**`app/src/lib/components/TopBar.svelte`** — the main diff.
- Delete the markup for items 6–14 and 16–17 in §5.1's table (`:142-218`), keeping their
  handler functions (`newLab`, `save`, `exportYaml`, `exportClabFile`, `pickImport`,
  `toggleFullscreen`, `syncFullscreen`, `download`, `safeName`, `onImportFile`) — they become
  the menu items' `action`s.
- Move the provider status pill (`:142-150`) out; §13 re-renders it from the same
  `labStore.providerStatus` / `activeProvider` derivations, which move with it (`:11-13`).
- Add the Saved/node-count pair (§7.2) after the lab-name input.
- Add the three-dot button + `menuOpen` `$state` + an anchor-rect computation, rendering
  `<ContextMenu id-keyed items={overflowItems()} dismissOnWheel={false} … />` anchored to the
  button's `getBoundingClientRect()` bottom-right.
- `overflowItems()` returns, with separators: **Lab** (New, Labs…, Save, Tasks[checked]) ·
  **Import / export** (Export YAML, Export containerlab, Import…) · **Library** (Images…) ·
  **View** (Theme submenu [Bench|Glass, radio], Fullscreen[checked]).
  §11.6 later inserts a **Link layout** radio pair and a **Snap grid** checkbox into a
  **Canvas** group here — that insertion point is this batch's only forward obligation.
- Keep `<svelte:window onfullscreenchange={syncFullscreen} />` (`:115`).
- Add a menu glyph to `icons.svelte.ts`'s `UI_GLYPHS` (`:105-140`): **`more`** (three
  vertical dots). Note `uiSvg` falls back to the `net` glyph for an unknown name
  (`icons.svelte.ts:206-209`), so **a typo'd name will not fail the build** — verify the
  rendered glyph visually, not just the type.

**`app/src/lib/components/ContextMenu.svelte`** — §7.3. **`app/src/lib/components/CanvasInner.svelte`** — `id` fields on four builders' items, no behavior change.

### 7.5 Testing bar

`app/` has **no test runner** — `package.json` scripts are `dev`, `build`, `build:embed`,
`preview`, `check`, `tauri`; there is no vitest and no `*.test.ts` under `app/src`. **Adding
one is out of scope** (P6 §6.3 set this precedent). The bar is therefore:

1. `cd app && npm run check` — green, zero new diagnostics.
2. Live, via the browser-pane tools against `npm run dev` (§15):
   - Every one of the ten moved controls is reachable from the three-dot menu and still
     performs its original action (New creates an empty lab; Save flips the Saved chip;
     Export YAML downloads; Import opens the file dialog; Images opens the manager; Tasks
     toggles the right pane; Theme switches Bench↔Glass; Fullscreen enters and the glyph
     flips).
   - The top bar renders exactly the six §7.1 elements plus the error pill when
     `labStore.lastError` is set.
   - Keyboard: `Tab` to the three-dot button, `Enter` opens, first item is focused,
     `ArrowDown`/`ArrowUp` wrap, `Home`/`End` jump, `Enter` activates and closes, `Escape`
     closes and returns focus to the button.
   - The four canvas context menus still open, dismiss on outside-click **and** on wheel, and
     activate correctly (regression on the shared component).
3. `git diff --stat` shows four files.

### 7.6 Acceptance gate

1–3 above, plus:
- `grep -c "class=\"btn\"" app/src/lib/components/TopBar.svelte` drops to **2** (the
  fullscreen toggle and the three-dot button, amended above); the only other button is
  `.btn.btn-primary`.
- `git diff app/src/lib/labStore.svelte.ts` is **empty**.
- Every `MenuItem` literal in the tree has an `id`; `grep -n "as item (item.label)"
  app/src/lib/components/ContextMenu.svelte` returns nothing.
- Brief acceptance criterion **"Current secondary top-bar actions are available from the
  three-dot menu"** (brief:88) is demonstrated item-by-item against §5.1's table in the PR.

---

## 8. Batch 14 — left icon rail + on-demand flyouts (replacing `Palette.svelte`)

### 8.1 Decisions locked

1. **Five rail buttons, in the brief's order** (brief:22-28), overriding the mockup's six
   (§4.1): **Add Nodes · Node Actions · Add Text · Add Shapes · Tools**.
2. **`Palette.svelte` is deleted, not hidden.** Its 832 lines are redistributed: Session +
   session rows → Node Actions flyout; Nodes + IOL images → Add Nodes flyout; Draw tools →
   Add Text (text only) and Add Shapes (rect/ellipse/note/line + colors); View → Tools;
   Console Web/Native segmented control → **the overflow menu** (§7.4's View group — it is a
   global console preference, not a canvas tool, and the brief's five groups have no home for
   it); Host monitor → **the resource bar** (§13, which is why Batch 13 lands first).
3. **The rail is always visible and never collapses.** `paletteUiStore.collapsed` and the
   `.palette-rail` chevron (`App.svelte:73-81`) are **deleted**; the key
   `"iolbox.palette.collapsed"` is abandoned (leaving a stale localStorage entry is harmless
   and cheaper than a migration). The `SplitPane` at `App.svelte:83-92` and its
   `"iolbox.split.palette"` key go with it — **this is what makes the canvas full-bleed**
   (brief:65: "Hidden panels must not reserve canvas space").
4. **Exactly one flyout open at a time; Escape and outside-click close it** (brief:30). The
   flyout is a **sibling overlay**, `position: absolute` inside `.canvas-area`, and
   **reserves no layout space** — it overlays the canvas, it does not push it.
5. **40×40 minimum hit area, icon-only, tooltip + `aria-label`, visible `aria-pressed` active
   state, keyboard-reachable** (brief:30). The rail is a `role="toolbar"` with roving
   `tabindex` (same pattern §7.3 gives `ContextMenu`).
6. **Add Text and Add Shapes arm the existing `annoTool` store.** `annoTool.arm("text")` etc.
   (`Palette.svelte:69-71`) is unchanged; the canvas's existing armed-cursor and placement
   handling (`CanvasInner.svelte:1032-1039`, `.canvas-wrap.arming`) is unchanged. **Add Text
   opens no flyout** — it arms directly and closes any open flyout (brief:26 describes an
   action, not a panel). Add Shapes opens a flyout with the four shape tools and the
   `ANNO_COLORS` swatch row.
7. **The Add Nodes flyout is searchable and categorized** (brief:24) over a **new derived
   catalog**, because §5.2 established there is nothing to reuse.
8. **Drag AND click-to-place** (brief:24). Drag keeps the exact existing
   `dataTransfer` contract (`Palette.svelte:11-18`) — **that payload shape must not change**,
   `CanvasInner`'s drop handler parses it. Click-to-place drops the node at the centre of the
   current viewport via `screenToFlowPosition` (already destructured at
   `CanvasInner.svelte:300-301`), which means the flyout needs a store-mediated call rather
   than a direct import; §8.3 specifies it.
9. **Wipe All keeps its `confirm()`** and its visual separation (brief:25). `Palette.svelte:55`
   already confirms; the separator is a `.sep` rule above the danger row.

### 8.2 The node catalog — new, small, derived

New `app/src/lib/nodeCatalog.ts`. One exported function, no store, no state:

```ts
export interface CatalogEntry {
  id: string;                  // stable: "vpcs" | "pc" | "tool" | "nat" | `iol:${imageId}`
  group: "Devices" | "Endpoints" | "Services" | "IOL images";
  name: string;                // "Router", "Switch", "VPCS", "PC", "NAT Gateway", …
  sub: string;                 // "L3 · x86_64", "Virtual PC", "Netprobe", …
  icon: string;                // key into iconSvg()
  search: string;              // lowercased haystack: name + sub + filename + arch + class
  drag: { kind: NodeKind; imageId?: string; packId?: string };
  disabled?: string;           // reason, when the entry is unavailable
}
export function nodeCatalog(): CatalogEntry[];
```

It reproduces `Palette.svelte`'s existing gating exactly — `hasNat` from
`labStore.features.includes("natgw")` (`:29`), the slirp note (`:31-36`), `toolPacks` +
`defaultToolPack` (`:21-25`), `iolImages` with the `img.class === "l2" ? "switch" : "router"`
mapping (`:304-305`) — and adds nothing new. Filtering is a plain
`entry.search.includes(query.toLowerCase())` over a `$derived` list; **no fuzzy matcher, no
dependency.**

### 8.3 Concrete file-level changes

**New `app/src/lib/components/IconRail.svelte`** — the rail. `role="toolbar"`,
`aria-orientation="vertical"`, five buttons, roving `tabindex`, `aria-pressed` bound to
"is my flyout open" (or, for Add Text, `annoTool.active === "text"`). Width ~52px, buttons
40×40 (`DESIGN.md` §5's icon-only buttons are 28px — §14.2 adds the rail's larger size as a
named component entry rather than a one-off).

**New `app/src/lib/components/RailFlyout.svelte`** — the shared container. Props
`{ title, onClose, children }`. Owns: the Escape handler, the capture-phase outside-pointerdown
dismiss (copy `ContextMenu.svelte:41-43`, `:56` — the same Svelte-Flow-swallows-bubbling
reasoning applies verbatim), focus trapping on open and focus return to the rail button on
close, and the panel chrome (`--panel`, `--blur`, hairline `--border`, `--radius-md`,
`--shadow-md` — the `WatcherPanel`/`PainterPanel` vocabulary).

**New `app/src/lib/paletteUiStore.svelte.ts` rewrite** → `railUiStore.svelte.ts`:
`open: RailPanel | null` where `RailPanel = "nodes" | "actions" | "shapes" | "tools"`,
`toggle(p)`, `close()`. **Session-only, not persisted** — a flyout that reopens itself on
reload contradicts "expands only on demand" (brief:83). It also carries the one piece of
cross-component plumbing §8.1.8 needs: `bindPlaceNode((drag) => …)`, registered once in
`CanvasInner.svelte` (which owns `screenToFlowPosition`), following the exact
`bindConsoleSelect` precedent at `App.svelte:32-34`. **Do not import `CanvasInner` from a
store.**

**`app/src/App.svelte`** — replace `:73-93` (the collapsed-rail button and the palette
`SplitPane`) with `<IconRail />`; render the flyout inside `.canvas-area` (`:96-105`) as a
sibling of `<Canvas />`, `WatcherPanel` and `PainterPanel`; delete `paletteWidth` (`:20`) and
the `Palette` import (`:7`).

**Delete `app/src/lib/components/Palette.svelte`.**

### 8.4 Testing bar

1. `npm run check` green.
2. Live (§15):
   - With no flyout open, the canvas spans from the rail's right edge to the window's right
     edge, and from the top bar to the resource bar. Measure it: `canvas-area` width equals
     `window.innerWidth - railWidth` **exactly**, with no `SplitPane` divider in the DOM.
   - Opening a flyout does **not** change `canvas-area`'s width (the acceptance criterion
     "Hidden panels must not reserve canvas space" is really a claim about the *open* case
     too — an overlay must not resize the canvas).
   - Exactly one flyout at a time: click Add Nodes, then Tools — the first closes.
   - Escape closes; outside-click closes; focus returns to the rail button.
   - Search: type `sw` in Add Nodes → only Switch entries; clear → full list; a query
     matching nothing shows an empty state, not a blank panel.
   - Drag a Router from the flyout onto the canvas → node created (proves the `dataTransfer`
     contract survived). Click a Router → node created at viewport centre.
   - Node Actions: all six actions present, Wipe All visually separated and confirming, and
     each disabled state matches the old palette (Start all disabled while running, Save
     configs disabled with no running IOL, Console all disabled with no running node).
   - Add Text arms immediately with no flyout; the canvas cursor becomes the crosshair;
     clicking places an inline-editable label.
   - Add Shapes: four tools + the color swatches; arming one closes the flyout or keeps it —
     state the choice in the PR and keep it consistent.
   - Keyboard: `Tab` reaches the rail once; Up/Down move between the five buttons;
     `Enter`/`Space` opens; `Tab` then moves into the flyout.
   - Every rail button's rendered box is ≥40×40 (measure in devtools).
3. `git status` shows `Palette.svelte` deleted and no other component orphaned
   (`grep -rn "Palette" app/src` returns only the deleted file's absence).

### 8.5 Acceptance gate

1–3, plus:
- Brief criterion **"The left rail exposes exactly the five primary groups above and expands
  only on demand"** (brief:83): the rail contains exactly 5 buttons, and on first paint after
  a reload no flyout is open.
- `grep -rn "iolbox.split.palette\|palette.collapsed" app/src` returns nothing.
- `git diff app/src/lib/annoTool.svelte.ts` is **empty** — the rail *calls* it, it does not
  change it.
- `git diff app/src/lib/labStore.svelte.ts` is **empty for the rail's own commits**. The
  addendum (§9) is the single carve-out: after it lands, the `labStore` diff for Batch 14 as a
  whole must contain **only** `restartNode`, the `acquireNodeLock` optional-flag argument, and
  the `:468` release guard — nothing else, and no change to `startNode`/`stopNode` bodies.

---

## 9. Batch 14 addendum — the selected-node action toolset

Folded into Batch 14 rather than given its own batch, because §4.5 established it is a
reveal change plus one CSS rule. **Post-review it is no longer a one-file change**: finding 5
puts Restart back in, which adds one method to `labStore.svelte.ts`. Two files, still no new
component.

### 9.1 Decisions locked

1. **Extend `NodeActions.svelte`. No new component.** (§4.5.)
2. **Reveal on hover OR selection, and selection means `selectedNodeId`.** Add
   `class:selected={labStore.selectedNodeId === nodeId}` to the root and one CSS rule
   mirroring `:123-130`'s reveal block. Hover reveal stays. **Not `inspectorNodeId`** — see
   §5.6 and §4.5; a plain node click never sets `inspectorNodeId`, so the draft's binding
   would have shipped a toolset that appears only when the Inspector happens to be open.
   This is grep-gated below.
3. **Restart ships** (review finding 5, reversing the draft). Its full design:
   - **New `labStore.restartNode(nodeId)`**, placed immediately after `stopNode`
     (`labStore.svelte.ts:1078-1086`) and shaped exactly like it:
     ```ts
     async restartNode(nodeId: number) {
       if (!this.acquireNodeLock(nodeId, "restarting", { holdUntilSettled: true })) return;
       try {
         await this.guarded(`restart node ${nodeId}`, async () => {
           await this.client.nodeRestart(this.lab.id, nodeId);
         });
       } finally {
         this.releaseNodeLock(nodeId);
       }
     }
     ```
     **One lock, one RPC.** There is no client-side stop→start compound to race: the ordering
     lives in `handleNodeRestart` (`server/handlers.go:523-533`), which calls `s.stopNode`
     then `s.startNodes` in one handler, and the old-process reaper is already safe against a
     fast stop-then-restart on the same node id (`node/state.go:81-95`).
   - **The one real subtlety, and it is why this is not a pure two-line change.** The lock is
     normally released by the next non-`starting` `node.state` event
     (`labStore.svelte.ts:468`). A restart emits `stopped` *in the middle* of the operation,
     which would release the lock early and flash the Start button back onto a node that is
     about to boot — the user could then click Start into a supervisor no-op. **Fix:
     `acquireNodeLock` gains an optional third argument `{holdUntilSettled?: boolean}` stored
     on the lock record, and the event-driven release at `:468` skips any lock carrying it.**
     A `holdUntilSettled` lock is released by its own `finally` (the `wipe`/`duplicate` idiom
     at `:1060-1069`), backstopped by the existing 60s safety timeout at `:1098-1101`. This is
     additive: no existing caller passes the flag, so `startNode`/`stopNode` behavior is
     byte-identical.
   - **Button placement and gating:** Restart renders **only when `isRunning`**, immediately
     after Console in the row. Glyph: the existing `reset` circular-arrow
     (`icons.svelte.ts:109`) at 12px — **no new icon**. `title`/`aria-label` = `"Restart"`.
     While the lock is held the whole row is already replaced by the spinner (`:48-52`), which
     now reads `restarting…` for free because the label renders `lock.action`.
   - **Not danger-styled.** `na-danger` is reserved for Wipe; a restart of a running lab node
     is an ordinary operational verb.
4. **Still no Configure button** (§4.5, unchallenged by the review). The PR states this
   explicitly against the mockup so a reviewer does not read it as an omission — and now also
   states that Restart, which the draft refused, is present.
5. **No size change.** The 22×22 buttons (`:131-141`) stay; the brief's 40×40 floor is a rail
   requirement (brief:30), and larger in-canvas buttons would occlude the node face.
6. **`onpointerdown={(e) => e.stopPropagation()}` stays on every button** — **including the new
   Restart button.** `:7-10` explains why; a new button added without it starts a node drag.

### 9.2 Testing bar and gate

**Reveal.** Select a node with a plain left-click (no context menu, no double-click, Inspector
closed) → the bar appears and **stays** after the pointer leaves. *This single check is the
regression test for finding 6 — under the draft's `inspectorNodeId` binding it fails.* Select a
second node → the first's bar disappears. Click empty canvas → `selectedNodeId` clears
(`CanvasInner.svelte:584`) and the bar disappears. Hover an *unselected* node → the bar still
appears and still fades with the 120ms delay.

**Restart.** With a running node: click Restart → the row is replaced by the spinner reading
`restarting…`; the node's LED goes stopped→starting→running; **the spinner persists across the
intermediate `stopped` event** (watch it, this is the `holdUntilSettled` check) and clears only
when the node reaches running/crashed. Console tabs behave as they do after a manual
stop+start. Restart is **absent** on a stopped node. Clicking Restart twice quickly is a no-op
the second time (the lock). Kill the supervisor mid-restart → the 60s safety timeout releases
the lock and the log carries the warning, i.e. no wedged node. Restart does not start a node
drag.

**Gate.** Every button still works and none of them starts a node drag.
`git diff --stat` shows **two** files (`NodeActions.svelte`, `labStore.svelte.ts`).
`grep -n "inspectorNodeId" app/src/lib/nodes/NodeActions.svelte` is **empty** (finding 6,
grep-checkable). `grep -n "nodeStop\|nodeStart" app/src/lib/labStore.svelte.ts` shows no new
call site inside `restartNode` (finding 5's "one verb, not a compound"). The `labStore` diff is
confined to `restartNode`, the `acquireNodeLock` signature, and the `:468` release guard —
**this is the one narrow exception to §8.5's empty-`labStore` gate, which applies to the rail
batch proper**; §8.5 is amended accordingly.

---

## 10. Batch 15 — floating console windows (supersedes P7 Batch 10)

**Read §3.2 first.** This section is P7 §6 with four additions and two corrections, not a new
design. Where a subsection says "as P7 §6.x", that text is authoritative and is not restated.

### 10.1 Decisions locked

1. **Placement is a new axis**, `ConsolePlacement = "dock" | "float"`, persisted at
   `iolbox.console.placement` — **as P7 §6.1.1 / §4.1**, whose three-binary-read-site
   argument (`App.svelte:54`, `Console.svelte:253`, `consoleUiStore.svelte.ts:391-393`) is
   re-verified against this working tree. **`DockSide` stays two-valued at
   `consoleUiStore.svelte.ts:5`.**
2. **Default is `"float"`** — the one substantive change from P7 §6.1.1. The redesign's
   premise is a full-bleed canvas (brief:65) and its criterion is "Consoles float, move,
   resize, minimize, and restore without shrinking the canvas" (brief:89). Existing users
   with a persisted `iolbox.console.dockSide` keep that value; it simply does not apply until
   they switch placement back to dock.
3. **Global mode.** All consoles float, or all dock. Per-pane mixing stays out of scope
   (P7 §6.1.2, §17).
4. **One window per `PaneRef`**; window identity, geometry key, z-order key and `{#each}` key
   are all `paneKey(ref)` (`consoleUiStore.svelte.ts:22-24`) — as P7 §6.1.3. **No window or
   tile logic may key off an array index.**
5. **`ConsoleTerm.svelte`, `CaptureTerm.svelte` and `LensPane.svelte` are not modified.**
   A window passes `visible: true` and `focused = isTopmost`. No `inWindow`/`floating`/
   `zIndex` prop — P6 §8.6, non-negotiable.
6. **`PaneBody.svelte` is extracted with THREE arms** (§4.6) and used by both owners — as
   P7 §6.1.5 / §4.2, corrected. Its diff in `Console.svelte` must be a **move**, not a
   rewrite.
7. **Drag and resize use pointer capture on the grabbed element**, through one new module —
   as P7 §6.1.6 / §4.3.
8. **Geometry persists per `(labId, paneKey)`; membership does not** — as P7 §6.1.7 / §6.9.
9. **A window's title bar can never leave the viewport**, enforced at three sites — as
   P7 §6.1.8 / §6.7.
10. **Nothing in this batch touches `labStore.svelte.ts`** — as P7 §6.1.9. The `reconcile`
    channel (`consoleUiStore.svelte.ts:283-353`, driven from `App.svelte:39-46`) is the only
    lifecycle path.
11. **Minimize is a third window state, not a close** (new, §10.5).
12. **New windows are placed by policy, not by a fixed cascade** (new, §10.4).

### 10.2 Store additions — `consoleUiStore.svelte.ts`

As P7 §6.2, with the constants, `placement`, `windows`, `windowOrder`, `nativeCapture` (moved
from `Console.svelte:26`), `searchOpenFor` (moved from `:16`), and the methods
`setPlacement` / `togglePlacement` / `moveWindow` / `resizeWindow` / `commitWindow` /
`raiseWindow` / `clampAllWindows` / `toggleNativeCapture` / `setSearchOpenFor`.

**Five changes to P7 §6.2's shape** (three as drafted, two added by review findings 2 and 7):

1. **`ensureWindow(ref, viewport)` becomes `ensureWindow(ref, geom: WindowGeom)`.** The store
   no longer computes placement. This is the direct fix for the sol-medium finding that
   `setPlacement`/`ensureWindow` cannot reach the viewport or `labStore` — see §10.4.
2. **New field `minimized = $state<string[]>([])`** (paneKeys), plus `toggleMinimized(key)`
   and `restoreWindow(key)` (§10.5).
3. **New field `pinnedWindows = $state<string[]>([])`** (paneKeys), plus
   `togglePinnedWindow(key)` and `isWindowPinned(key): boolean` (review finding 7 — §10.5
   described pin behavior with no state to hold it, so nothing was implementable or testable).
   Design notes that make it writable *and* assertable:
   - **A separate field from `pinned` (`:112`), by design and by gate.** `pinned` is the
     dock's single "always tile 1" pane and is `string | null`; `pinnedWindows` is a *set* of
     float-mode "keep on top" windows. §10.5 explains why overloading breaks a user who pins
     in float and then docks. §10.12 greps that `pinned`'s type and every use are unchanged.
   - **An array, not a `Set`**, matching `tiles` (`:11x`) and `minimized` — Svelte 5 runes
     track array reassignment naturally and the file has no `SvelteSet` import today. Order is
     insignificant; membership is the state.
   - `togglePinnedWindow(key)` adds/removes; **it never raises**. Pin and z-order are
     orthogonal operations, so a test can pin a background window and assert it rose to the
     pinned band without a separate click.
   - **`raiseWindow(key)` becomes pin-aware** (this is the only behavioral coupling, and it
     lives in one function): it appends `key` to `windowOrder` as today, then applies one
     stable partition — every key in `pinnedWindows` sorts after every key not in it,
     preserving relative order within each group. **z-index is still derived purely from
     `windowOrder`'s index** (§10.8), so pinning needs no second z band and P7 §6.6 is
     unchanged. Pinning an already-topmost window is a visible no-op; unpinning drops it back
     to its position among the unpinned by its own last-raise order.
4. **Lens panes are first-class in every window path** (review finding 2). `isOpen`
   (`:276-280`) treats `kind === "lens"` as open iff the **capture** is open —
   `if (ref.kind === "capture" || ref.kind === "lens") return captures.has(ref.link)` — which
   is right for the dock (`Console.svelte` renders a Lens tab only inside a capture's tab
   group) and **wrong for a floating window**: a Lens window would outlive
   `labStore.closeLens(linkId)` (`labStore.svelte.ts:870-872`) forever, because the capture it
   piggybacks on is still open. **Fix, in the store:** `reconcile` takes a **third** pane list
   — `reconcile(labId, consoles, captures, lenses)` — and `isOpen` becomes
   `if (ref.kind === "lens") return lenses.has(ref.link)`, with `capture` unchanged. The
   caller is the existing single `$effect` at `App.svelte:39-46`, which gains
   `labStore.openLensTabs` (`labStore.svelte.ts:148`) as a fourth read. This keeps
   `consoleUiStore` free of any `labStore` import (§10.12's grep still passes) and makes the
   Lens window's lifetime its own: `closeLens` removes the id from `openLensTabs`, the effect
   re-runs, the prune drops the window. Closing the *capture* also closes the Lens, because
   `labStore.closeCapture` already clears `openLensTabs` for that link
   (`labStore.svelte.ts:972`) — so the containment direction survives without a special case.
5. **`reconcile()` gains lines in both halves**, as P7 §6.2 specified, plus `minimized` and
   `pinnedWindows`: the hard-reset half (`:284-295`) also clears `windows`, `windowOrder`,
   `minimized`, `pinnedWindows`, `nativeCapture`, `searchOpenFor` (the **persisted** geometry
   map is *not* cleared — it is keyed by lab id and is the feature); the prune half
   (`:297-338`) drops entries in `windows` / `windowOrder` / `minimized` / `pinnedWindows`
   whose pane is no longer open, through the amended `isOpen`, drops `nativeCapture` keys for
   closed captures, and nulls `searchOpenFor` if its node is gone.

### 10.3 `PaneBody.svelte` — the extraction, with three arms

New `app/src/lib/components/PaneBody.svelte`, props `{ ref: PaneRef, visible, focused }`.
Body **moved verbatim** from `Console.svelte`:

- **console arm** ← `:459-478` (the `isToolNode` iframe branch `:460-466` and the
  `ConsoleTerm` `:468-476`), with `searchOpen` / `onOpenSearch` / `onCloseSearch` now reading
  and writing `consoleUiStore.searchOpenFor` instead of the component-local `let` at `:16`;
- **capture arm** ← `:489-558` (the `CaptureTerm` `:492-497` and the entire `.native-hold`
  overlay `:498-557`), with `nativeCapture[linkId]` now `consoleUiStore.nativeCapture[linkId]`;
- **lens arm** ← `:569-576` (the `LensPane`) — **this arm does not exist in P7 §6.3 and is
  the batch's easiest silent omission** (§4.6);
- the helpers those arms call move with them: `captureTitle` (`:29-36`), `isToolNode`
  (`:220-222`), `nodeName` (`:216-218`), `captureAddr` (`:167-171`), `wiresharkCmd` /
  `wiresharkCmdFull` (`:176-187`), `fmtBytes` (`:189-192`), `copyText` (`:233-244`), the
  `SHARK` glyph (`:205-206`), and every CSS rule under `.native-*` / `.tool-frame` /
  `.addr-chip`. `captureTitle` and `nodeName` are needed by **both** `PaneBody` and the window
  title bar — export them from a tiny shared module (`app/src/lib/paneLabels.ts`) rather than
  duplicating.
- The `.pane-frame` wrapper (`:459`) moves **into** `PaneBody` so both owners get identical
  containment; the dock's `.term-slot` positioning rules stay in `Console.svelte` unchanged.

**`Console.svelte` keeps:** `collapsed` (`:15`), the tab strip (`:264-363`), the dock-actions
row (`:365-438`), the layout/pin/search/mark controls, and the `.term-area` grid, now
rendering `<PaneBody>` in each slot.

**The Wireshark one-shot `$effect` (`:152-158`) moves to `App.svelte`**, exactly as P7 §6.3
argued: `App.svelte` mounts `Console.svelte` only in dock placement (`:106`, `:130`), so in
float placement nothing would consume `labStore.wiresharkOverlayFor` — the link menu's
"Capture in Wireshark…" would open a capture with no overlay and leave the signal set forever,
firing spuriously on the next dock. In `App.svelte` it calls
`consoleUiStore.setFocused({kind:"capture", link})` + `consoleUiStore.toggleNativeCapture(link)`
+ nulls the signal, and runs in both placements. This stays inside the narrow `App.svelte`
allowance P6 §8.9 carved out.

### 10.4 Placement policy — the new part, and the sol-medium fix

**Requirement:** "Avoid opening new consoles directly over the selected node or the center of
the visible topology" (brief:72).

**Constraint:** `consoleUiStore` must not import `labStore` (P6 §8.3 — `labStore.svelte.ts`
already imports `consoleUiStore`, so the reverse edge closes a singleton cycle), and it has no
access to the viewport or to xyflow's `flowToScreenPosition`.

**Decision: the store never computes a position. `FloatingConsoleLayer.svelte` does, and
passes a finished `WindowGeom` into `ensureWindow(ref, geom)`.** One decision, two problems
solved.

**How the layer gets flow→screen coordinates — corrected by review finding 1.** The draft said
the layer calls `useSvelteFlow()` itself. **It cannot.** `useSvelteFlow()` reads Svelte context
published by `<SvelteFlowProvider>`, and that provider exists only inside `Canvas.svelte`
(`app/src/lib/components/Canvas.svelte:8-10`, a three-line wrapper whose only child is
`CanvasInner`). `FloatingConsoleLayer.svelte` mounts as an `App.svelte` sibling — a different
subtree — where the context is absent and the hook throws or returns an unusable store. Two
repairs were considered:

- **(a) Mount the layer inside `Canvas.svelte`'s provider**, as a second child next to
  `CanvasInner`. Structurally possible (the provider renders no DOM box of its own).
  **Rejected:** it buries an app-level overlay inside the canvas component, puts floating
  windows inside a subtree whose stacking context and pointer semantics are xyflow's, and
  makes the layer's lifetime the canvas's. It also fights §10.8, which places windows in an
  app-level z band.
- **(b) `CanvasInner` publishes the projector; the layer reads it. — CHOSEN, because it is
  already this codebase's answer to exactly this question.** `labStore.screenToFlow`
  (`labStore.svelte.ts:51-53`) is a nullable function field wired in `CanvasInner`'s `onMount`
  (`CanvasInner.svelte:391`) from `useSvelteFlow`'s `screenToFlowPosition` and **nulled in the
  same effect's teardown** (`:399`); `AnnoLine.svelte:67` — a component outside the provider —
  calls it. `labStore.canvasZoom` (`:50`, written at `CanvasInner.svelte:997`) is the same
  pattern for the zoom scalar. **The floating layer needs the other direction, so Batch 15 adds
  the mirror-image field:**

  ```ts
  /** Flow→client coordinate projector, wired by CanvasInner (useSvelteFlow's
   *  flowToScreenPosition). Used by the floating-console placement policy and by
   *  Batch 17's viewport-clamped endpoint chips. */
  flowToScreen: ((x: number, y: number) => { x: number; y: number }) | null = null;
  ```

  wired one line below `:391` as
  `labStore.flowToScreen = (x, y) => flowToScreenPosition({ x, y });` and nulled one line below
  `:399`. `flowToScreenPosition` is **already destructured** at `CanvasInner.svelte:300-301`
  and already used at `:339`, so nothing new is imported.

  **This is a `labStore` change, and Batch 15's gate forbids one** (§10.1.10). The
  contradiction is resolved by *ownership*, not by exception: **the field lands in Batch 16's
  slice, not Batch 15's**, because §12 (Batch 17) needs the identical field for finding 8's
  chip clamping and Batch 16/17 already own `CanvasInner`. Batch 15 then only *reads* it.
  Consequences, all stated so no agent has to guess:
  - **Batch 15's `labStore` diff stays empty** and §10.12's gate is unweakened.
  - **Batch 15 must therefore tolerate `flowToScreen === null`** — which it must anyway: the
    layer can mount before `CanvasInner`'s `onMount` runs, and the field is null whenever the
    canvas is unmounted. §10.4's policy degrades to **step 3 (cascade)** when the projector is
    null, which is exactly the "viewport too small / no information" branch it already has.
    A window is never *blocked* on the projector.
  - If the orchestrator runs 15 and 16 in parallel (§6 permits it), whichever lands first
    carries the two lines; the other must not add them twice.

With that, the layer reads everything the policy needs: **`labStore.selectedNodeId`** (not
`inspectorNodeId` — review finding 6; the brief says "the selected node", and a plain click
sets only `selectedNodeId`, so keying on `inspectorNodeId` would have made the policy avoid
the wrong node, or no node at all, on the most common interaction) and `labStore.lab.nodes` for
the selected node's flow position, `labStore.flowToScreen` to project it, and the viewport from
its own `<svelte:window bind:innerWidth bind:innerHeight />`.

The policy, in order, first candidate that fits wins:

1. **Restore** — if the persisted map has a geometry for `(labId, paneKey)`, use it
   (re-clamped). Persistence beats policy; a user who placed a window means it.
2. **Avoid two rectangles** (the selected-node rect is skipped when `selectedNodeId` is null or
   `flowToScreen` is null — there is then nothing to avoid but the centre).
   Build `avoid = [selectedNodeScreenRect inflated by 48px,
   viewportCenterRect (the middle 40% × 40% of the canvas area)]`. Candidate origins are
   generated on a coarse lattice starting from the canvas area's bottom-right inset by
   `(WIN_DEFAULT_W + 24, WIN_DEFAULT_H + 24)`, stepping left then up in 24px increments.
   Take the first candidate whose rect intersects **neither** `avoid` rect **nor** any
   existing window's rect.
3. **Cascade fallback** — P7 §6.2's `24 * (index % 8)` offset from the canvas area's top-left,
   clamped. This is what runs when the viewport is too small for step 2 to succeed, and it is
   why step 2 may never loop unboundedly: cap the lattice at 64 candidates.

**Two properties this must have, and they are the review's checklist for §10.4:** it is
**deterministic** (same inputs → same position, so a test can assert it), and it is
**side-effect-free with respect to the canvas** — it reads node positions, it never writes
them. A policy that nudged the viewport to make room would violate brief:85's "Switching …
does not mutate topology data, move nodes".

### 10.4a Which panes get a window — the enumeration, and the Lens lifecycle

**Review finding 2.** The layer's `{#each}` source was drafted as "the open console and capture
tabs". That is `Console.svelte`'s *tab-strip* enumeration minus its third kind, and it produces
one of two bugs depending on how an implementer reads it: a Lens pane that **never gets a
window** (floating mode silently has no Protocol Lens — §4.6's exact failure), or, if the
implementer patches it by treating a Lens as part of its capture, a Lens window that
**never closes**, because `isOpen` keeps it alive as long as the capture is open (§10.2.4).

**The enumeration is explicit and has three arms, one per `PaneRef` kind**, and it uses the
same three sources `Console.svelte` already uses for its tab strip:

```ts
const panes: PaneRef[] = [
  ...labStore.openConsoleTabs.map((node) => ({ kind: "console", node })),
  ...labStore.openCaptureTabs.map((link) => ({ kind: "capture", link })),
  ...labStore.openLensTabs.map((link)   => ({ kind: "lens",    link })),  // labStore.svelte.ts:148
];
```

**Lens window lifecycle, stated as three independent facts** so no arm piggybacks on another:

- **Opens** when `labStore.openLens(linkId)` (`labStore.svelte.ts:864-867`) puts the id in
  `openLensTabs` — the same trigger that opens the dock's Lens tab. One `paneKey({kind:"lens",
  link})`, one window, §10.1.4 unchanged.
- **Closes** when `labStore.closeLens(linkId)` (`:870-872`) removes it — **and by nothing
  else**. The prune half of `reconcile` sees `openLensTabs` directly (§10.2.4), so the window
  disappears on that call and not one render later.
- **A Lens window's close button calls `labStore.closeLens(link)`**, exactly as the dock's Lens
  tab close does. It must **not** call `closeCapture`: closing the Lens leaves the capture
  running, which is the whole point of the Lens being a separate pane. The converse holds for
  free — `closeCapture` already clears that link from `openLensTabs` (`:972`), so closing a
  capture window takes its Lens window with it.

**A Lens window is a peer window, not a child.** It has its own geometry key, its own z-order
entry, its own pin and minimize state. It does not follow, dock to, or move with its capture's
window. (P7 §6.1.3's one-window-per-`PaneRef` rule already implies this; it is stated because
"the Lens belongs to the capture" is the intuition that produced the bug.)

### 10.5 Minimize, pin and the launcher — the second new part

- **Title bar controls, left to right:** state LED (from `labStore.nodeStates[nodeId]`,
  mirroring `TopBar.svelte:300-315`'s `.led` treatment), the pane name
  (`nodeName` / `captureTitle` from `paneLabels.ts`), spacer, **pin**, **minimize**, **close**
  — matching the mockup and brief:70.
- **Pin, in float placement, means exactly one thing: "keep on top of unpinned windows".**
  The state is `consoleUiStore.pinnedWindows` and its methods are `togglePinnedWindow(key)` /
  `isWindowPinned(key)` (§10.2.3 — review finding 7: the draft described this behavior with no
  field, no method and nothing a test could target). The behavior, stated tightly enough to
  write assertions against:
  1. **Ordering.** `raiseWindow` appends as today, then stably partitions `windowOrder` so
     every pinned key follows every unpinned key. z stays `FLOAT_Z_BASE + index` (§10.8).
     *Assertion: with A pinned and B unpinned, clicking B leaves A's computed `z-index`
     greater than B's.*
  2. **Raising among pinned windows still works.** Clicking pinned A when pinned C is above it
     puts A above C. Pin is a band, not a freeze. *Assertion: two pinned windows can be
     reordered against each other.*
  3. **Pin does NOT exempt a window from anything else.** It does not affect clamping (§10.9),
     it does not affect `reconcile`'s prune (closing the pane closes the window — a pinned
     window is not undismissable), it does not affect minimize (a pinned window can be
     minimized; it returns to the pinned band on restore), and it does not interact with
     Batch 18's auto-hide, which never hides the console layer at all (§14.1). *Assertion:
     switch labs with a window pinned → it is gone, and `pinnedWindows` is empty.*
  4. **Pin is not persisted.** Geometry persists per `(labId, paneKey)` (§10.1.8); pin is
     membership state and is cleared by `reconcile`'s hard reset, like `tiles` and `focused`.
  5. **Affordance.** The title-bar pin button is `aria-pressed={isWindowPinned(key)}` — which
     is what §16's criterion-10 `read_page` pass asserts.
  This is **not** `consoleUiStore.pinned` (`:112`), which means "always tile 1" in the dock.
  Two different verbs; two fields. **Do not overload `pinned`** — a user who pins in float and
  then docks would find an unrelated pane locked to tile 1.
- **Minimize** adds the paneKey to `minimized`. A minimized window renders **nothing** — not a
  0-height window, not `visibility: hidden`. Its `PaneBody` unmounts, and **that is a real
  consequence**: `ConsoleTerm.svelte`'s `onDestroy` disposes the terminal, so **minimizing
  would drop the scrollback**, which P6 §9 explicitly forbids for hidden panes ("They are live
  consoles; unmounting drops the WebSocket and the scrollback"). **Decision: a minimized
  window keeps its `PaneBody` mounted inside a `display: none` wrapper**, out of flow but not
  unmounted — the same `display: none`-not-`visibility: hidden` rule P6 §1a finding 5
  established for untiled panes, for the same reason. `visible={false}` is passed so no
  `fit()` runs against a 0×0 box; restoring passes `visible={true}` and the terminals'
  existing rAF-refit path handles the rest.
- **The launcher** is a compact horizontal strip of chips, one per minimized pane, docked to
  the bottom-left of the canvas area **above** the resource bar. Each chip shows the state LED
  + name and restores on click. It renders only when `minimized.length > 0`, so it costs
  nothing at rest. It is part of `FloatingConsoleLayer.svelte`, not a separate component.
- **The "return to dock" escape hatch** stays required, as P7 §6.4 argued: the dock-bar control
  that flips placement is unmounted in float mode, so a user who closes every window would
  otherwise be stranded. Put it in the overflow menu's View group (§7.4) **and** on the
  launcher strip.

### 10.6 – 10.10

`FloatingConsoleWindow.svelte` / `FloatingConsoleLayer.svelte` structure (§10.6), `dragMove.ts`
(§10.7), stacking (§10.8), clamping (§10.9) and persistence (§10.10) are **as P7 §6.4, §6.5,
§6.6, §6.7 and §6.9 respectively**, with three amendments:

- **§10.6:** the window renders `<PaneBody {ref} visible={!isMinimized} focused={isTopmost} />`,
  and the title bar is a `<div role="toolbar">` carrying `touch-action: none` — it must **not**
  be a `<button>`, and its own buttons must `stopPropagation` on `pointerdown` the way
  `NodeActions.svelte:55` does, so clicking Close never starts a drag.
- **§10.8:** `z = FLOAT_Z_BASE + Math.min(index, 99)` with `FLOAT_Z_BASE = 900`, re-verified
  against §5.10's empty 61–999 band. §14.3 later renames the literal to a semantic token; this
  batch may ship the constant.
- **§10.9:** the third enforcement site — `clampAllWindows` on viewport resize — needs
  `innerHeight`, which **does not exist anywhere today** (`App.svelte:28,67` binds only
  `innerWidth`). The layer binds its own.

### 10.11 Testing bar

1. `npm run check` green.
2. Live (§15):
   - Open two consoles → two floating windows, neither over the selected node, neither over
     the canvas centre, not overlapping each other. Note the coordinates.
   - Drag one by its title bar → it moves; release → position persists across a placement
     flip to dock and back.
   - Drag it toward each of the four viewport edges → at least 120px of title bar stays
     visible on every axis and the top edge never goes under the top bar.
   - Resize from the bottom-right grip → respects `WIN_MIN_W`/`WIN_MIN_H`; then drag to the
     right edge and **shrink** it — it must not slide out (P7 §6.7's second site).
   - Shrink the browser window to half width → every window is re-clamped and reachable
     (P7 §6.7's third site — the one that strands users in practice).
   - Click a background window → it comes to front, its terminal takes focus, and the dock's
     `labStore.activeConsoleTab` follows (the P6 §8.2a bidirectional sync still works through
     `setFocused`).
   - **Minimize a console with visible scrollback, then restore it: the scrollback is still
     there.** Inspect the wrapper in devtools while minimized — it must be `display: none`,
     and the `ConsoleTerm` must still be in the DOM.
   - **Pin (finding 7's five assertions, run as five observations):** pin a window, raise
     another → `javascript_tool` reads both computed `z-index`es and the pinned one is higher;
     pin a *second* window and click the first → it rises above the second, both still above
     every unpinned one; minimize a pinned window and restore it → still on top; the pin
     button's `aria-pressed` flips in `read_page`; close the pinned pane from the dock's own
     tab list → the window is gone (pin is not undismissable).
   - **Placement without a canvas projector:** open a console *before* touching the canvas
     (or with `labStore.flowToScreen` forced null in the console) → a window still opens, via
     the cascade branch, with no error (§10.4's null-tolerance).
   - **Placement avoids the SELECTED node, on a plain click** (finding 6): left-click a node
     once — no Inspector — then open its console; the window overlaps neither that node's rect
     nor the centre band. Under the draft's `inspectorNodeId` reading this check passes
     vacuously, so verify `labStore.selectedNodeId` is non-null and `inspectorNodeId` is null
     at the moment of the check.
   - Flip to dock placement → the exact prior dock side, layout and tile set return (P7 §6.1.1's
     no-migration claim).
   - **Open a Protocol Lens for a capturing link while floating** — the Lens window renders
     (this is §4.6's test; a two-arm `PaneBody` fails here and nowhere else).
   - **The Lens window's own lifecycle (finding 2), three observations:** with the Lens window
     open, **close the Lens** (its own close button, or the dock's Lens tab after flipping
     back) → the Lens window disappears **and the capture window is still there, still
     capturing**. Re-open the Lens → a window returns. Now **close the capture** → both
     windows disappear. Under the draft's enumeration the first observation fails (zombie
     window) or the whole row is untestable (no window ever appeared).
   - Flip a capture tab to native Wireshark from the link menu **while floating** — the
     overlay appears (this is §10.3's Wireshark-one-shot test).
   - Ctrl-F inside a floating console opens search (the `searchOpenFor` move).
   - Switch labs with three windows open, one minimized and one pinned → everything clears,
     no console errors, and `git diff app/src/lib/labStore.svelte.ts` is still empty.
   - A floating window sits **above** `WatcherPanel`/`PainterPanel` and **below** the Image
     Manager, the lab browser, Preflight and the interface picker.

### 10.12 Acceptance gate

1–2, plus:
- `git diff app/src/lib/consoleUiStore.svelte.ts` shows **no change** to `DockSide` (`:5`),
  `ConsoleLayout` (`:10`), `ensureTiled`, `trimTiles`, `setFocused`, `syncFromLabStore`,
  `advanceCaptureDelivery` or `addMark`.
- `git diff` shows **zero** changed lines in `ConsoleTerm.svelte`, `CaptureTerm.svelte`,
  `LensPane.svelte`, `labStore.svelte.ts`, and `supervisor/`.
- `grep -rn "labStore" app/src/lib/consoleUiStore.svelte.ts` is **empty**.
- `grep -n "window.addEventListener" app/src/lib/dragMove.ts` is **empty** (§4.3's whole
  point) and `onpointercancel` is bound alongside `onpointerup` in the window markup.
- `grep -c "PaneRef" app/src/lib/components/PaneBody.svelte` > 0 and the file contains all
  three arms (`ConsoleTerm`, `CaptureTerm`, `LensPane`).
- **Finding 1:** `grep -n "useSvelteFlow" app/src/lib/components/FloatingConsoleLayer.svelte`
  is **empty**, and `grep -n "flowToScreen" app/src/lib/components/FloatingConsoleLayer.svelte`
  is non-empty — the layer consumes the published projector, it does not reach for a context
  it is not inside. `git diff app/src/lib/labStore.svelte.ts` is still empty for this batch
  (the two projector lines belong to Batch 16/17 — §10.4).
- **Finding 2:** `grep -n "openLensTabs" app/src/lib/components/FloatingConsoleLayer.svelte`
  and `grep -n "openLensTabs" app/src/App.svelte` are both non-empty, and `reconcile`'s
  signature carries a fourth argument. `grep -n "kind === \"lens\"" app/src/lib/consoleUiStore.svelte.ts`
  shows the Lens arm keyed on the **lens** set, not the capture set.
- **Finding 7:** `grep -n "pinnedWindows\|togglePinnedWindow" app/src/lib/consoleUiStore.svelte.ts`
  is non-empty, and `git diff` shows **no change** to `pinned`'s declaration at `:112` or to
  any of its existing readers.
- **Finding 6:** `grep -n "inspectorNodeId" app/src/lib/components/FloatingConsoleLayer.svelte`
  is **empty**.
- `Console.svelte`'s diff for the moved regions is a **move**: the removed lines appear
  verbatim in `PaneBody.svelte`.
- The PR states which of P7 §6's decisions were reused unchanged (expected: all of §6.1.1–
  §6.1.9 except the default) and which were changed and why.

---

## 11. Batch 16 — link display modes: Free Flow preserved, Structured added

**This is the hardest batch in the plan and it must be scoped as such.** Its danger is not
that the algorithm is difficult; it is that the *easy* version of the algorithm produces a
mode that renders and is wrong in six places at once (§1.2).

### 11.1 Decisions locked

1. **Free Flow is byte-for-byte today's rendering.** `FloatingEdge.svelte:248-349` in Free
   mode must produce the identical `geom` object it produces now. A pixel diff of a lab in
   Free mode before and after this batch is the batch's first gate.
2. **The deliverable is a mode-agnostic `LinkGeometry` interface, not a second path string**
   (§11.2). Both modes construct one; the template reads only the interface.
3. **`getSmoothStepPath` is the elbow builder** (§5.5), with `borderRadius: 0`. **No custom
   elbow math, no third-party router, no new dependency.**
4. **`getEdgeParams`'s already-computed `sourcePos`/`targetPos` are consumed, not
   recomputed** (`edges/floating.ts:86-87`, currently discarded).
5. **Routing is computed purely in FLOW coordinates from node positions and the lab's link
   list.** Nothing in the routing path may read the viewport, the zoom, `window.innerWidth`,
   or any screen-space value. **This single invariant is what satisfies the brief's
   "Changing viewport size or zoom must not unexpectedly reroute the topology into a
   materially different layout" (brief:47)** — and it is free, because §5.4's current geometry
   is already pure flow-space. A global router that considered available screen space would
   violate the brief's own criterion. **State this in the PR.**
6. **Node avoidance and true shared lanes are scoped honestly** (§11.5): parallel-link lane
   separation ships; obstacle avoidance does **not** and is a named non-goal (§17).
7. **Presentation only.** No topology data, no interface binding, no node position, no lab
   runtime state changes (brief:34, brief:85). The mode is a `lab.canvas` field and nothing
   else (§11.6).
8. **A 120–180ms transition with reduced-motion support** (brief:49). It is a CSS transition
   on the `<path>`'s `d`… which does not animate. §11.7 decides what actually transitions.
9. **Existing labs open in Free Flow** (brief:40): a missing `lab.canvas.linkLayout` reads as
   `"free"`.

### 11.2 The `LinkGeometry` interface — the batch's real deliverable

New `app/src/lib/edges/routing.ts`:

```ts
export interface LinkGeometry {
  /** The cable's own path, in flow coordinates. */
  path: string;
  /** Point at CURVE PARAMETER t ∈ [0,1] — NOT arc length. See §11.2a; the
   *  parameter is the Bézier's own `u` in Free mode and the polyline's
   *  cumulative-length parameter in Structured mode. Do not "fix" this into
   *  arc length: Free Flow's chip positions are pinned to it. */
  at(t: number): { x: number; y: number };
  /** A path parallel to `path`, offset by `d` px along the local normal.
   *  `reversed` emits it target→source so one animation direction serves both. */
  offsetPath(d: number, reversed: boolean): string;
  /** Chip anchors and their transform-origins, as today. */
  sChip: { x: number; y: number };
  tChip: { x: number; y: number };
  watcherChip: { x: number; y: number };
  sOrigin: "left" | "right";
  tOrigin: "left" | "right";
}
export function freeGeometry(input: RouteInput): LinkGeometry;
export function structuredGeometry(input: RouteInput): LinkGeometry;
```

### 11.2a `at()` is curve-parameter position, not arc length — one semantics, stated once

**Review finding 3.** The draft documented `at(u)` as *"point at normalised arc-length u"* while
§11.3 simultaneously required lifting `FloatingEdge.svelte:291-297`'s evaluator **unchanged** —
and that evaluator is the closed-form quadratic `B(u) = (1-u)²P₀ + 2(1-u)uC + u²P₁`, which is
parametrized by the curve **parameter**, not by distance along it. The two coincide only for a
straight line. For any bowed Free Flow edge (`off ≠ 0`, i.e. every parallel link and every
curved run) they differ, so shipping real arc length would **move** where Free Flow's port
chips, fault pill, best-path pill and Watcher pills render — violating §11.1.1's
pixel-identical gate, which is the batch's own hardest constraint.

**Decision: `at()` is CURVE-PARAMETER position. It is not, and never claims to be, arc length.**

- **Free mode:** `at(t)` is `:291-297` verbatim. Byte-identical output; §11.1.1 holds.
- **Structured mode:** `at(t)` walks the polyline by **cumulative segment length**, which for a
  polyline *is* arc length. This is not an inconsistency to hide — it is the honest statement:
  *`at()` returns each mode's natural parametrization of its own path; the parameter is
  monotonic from source (0) to target (1) in both, and the anchor constants (`0.22`, `0.5`,
  `0.78`) are tuned per mode-shape rather than being a distance promise.*
- **Every place this document previously said "arc length" now says "curve parameter"**: this
  subsection, the interface comment above, §11.3's `at(u)` bullet, and §17's non-goal list.
  The identifier is `t`, not `u`, everywhere except inside the lifted Free-mode body (whose
  local `u` is left untouched so the diff stays a move).
- **Known limitation, named rather than silently promised:** on a strongly bowed Free Flow
  edge, `at(0.5)` is not the midpoint *by distance*; it is the parameter midpoint, which sits
  slightly toward the flatter half. This is **exactly today's behavior** — the fault pill, the
  best-path pill and the Watcher pills already render there and have since they shipped — so
  it is a limitation of the interface's honesty, not a regression. **True arc-length spacing is
  an explicit non-goal (§17.)** If a future consumer genuinely needs equal-distance spacing
  (e.g. evenly distributing N pills along a cable), it must add a separate
  `atLength(px: number)` built on a per-segment length table, and must not redefine `at()`.
- **No consumer needs arc length today.** Verified across all twelve of §11.2b's consumers:
  every one asks for a *named anchor* (`sChip`, `tChip`, `watcherChip`) or rides `offsetPath`,
  and `animateMotion` distributes its own timing along the path it is given regardless of how
  the endpoints were derived.

### 11.2b The consumer inventory, with its two missing entries

Review finding 10: the draft's inventory stopped at `FloatingEdge.svelte:530` and said "ten
consumers". Two more sit past that line and both read `geom.watcherChip`:

| # | consumer | line |
|---|---|---|
| 1 | traffic glow | `:356-360` |
| 2 | `BaseEdge` (the cable itself) | `:362-375` |
| 3 | fault pill | `:378-386` |
| 4 | STP blocked / converging overlays | `:393`, `:398` |
| 5 | best-path glow + three `animateMotion` arrowheads | `:404-415` |
| 6 | invisible hover catcher | `:422-428` |
| 7 | Watcher per-direction dashed flows + their `animateMotion` arrowheads | `:440-463` |
| 8 | source port chip | `:466-490` |
| 9 | target port chip | `:493-519` |
| 10 | STP reason popover | `:525` |
| **11** | **routing best-path metric pill** (`geom.watcherChip`) | **`:537`** |
| **12** | **Watcher label pills** (`geom.watcherChip.y + i * 18`, one per matching row) | **`:545`** |

**The verification range is therefore `:352` through the end of the template (`:552` in this
working tree), not `:352-530`.** §11.10's "template unchanged" gate and §11.9's live checklist
both use the corrected range — otherwise the gate silently skips the two newest consumers,
which are precisely the two most likely to be broken by a geometry change nobody re-read.
Both are covered by `watcherChip` and need no interface addition: **finding 10 closes an
inventory gap, it does not reopen the "is `LinkGeometry` sufficient" question.**

`RouteInput` carries the already-computed `{sx, sy, tx, ty, sourcePos, targetPos,
parallelSign, parallelCount, parallelIndex, px, py}` — i.e. everything
`FloatingEdge.svelte:248-286` computes today, extracted into a pure function.

**`freeGeometry` is a lift-and-shift of `:288-348`.** Its `at(t)` is the existing quadratic
**curve-parameter** evaluator (`:291-297` — §11.2a) and its `offsetPath` is the existing
three-point translate (`:329-336`), both moved unchanged. **This half must produce identical output.** The cheapest
way to prove it is to land `freeGeometry` first, in its own commit, with the template still
reading a `geom` that is now built by it — a pure refactor with no visual change.

`FloatingEdge.svelte`'s `geom` then becomes
`$derived(mode === "structured" ? structuredGeometry(input) : freeGeometry(input))`, and
**the entire template from `:352` to the end (`:552`) is untouched.** That is the design's
payoff: **twelve** consumers (§11.2b — the inventory was ten and finding 10 corrected it),
zero edits.

### 11.3 `structuredGeometry` — the polyline, and why `getPointAtLength` is rejected

`getSmoothStepPath` returns a path string. `LinkGeometry` needs `at(u)` and `offsetPath`,
which a string cannot answer. Two approaches were considered:

**Rejected: `SVGGeometryElement.getPointAtLength` on a detached `<path>`.** It answers `at(t)`
(as `getPointAtLength(u * getTotalLength())`) and nothing else — there is no `offsetPath`
counterpart, so the Watcher's per-direction flows (`FloatingEdge.svelte:440-463`) and the
best-path arrow (`:411`) would have no path to ride. It also requires creating and measuring a
DOM node inside a `$derived` that re-runs **every animation frame during a node drag**
(`:4-5` — the geometry recomputes from `positionAbsolute`, which updates per frame). Layout
measurement in a hot reactive path is exactly the kind of jank this canvas has otherwise
avoided.

**Decided: build the structured route as an explicit point list, and derive both from it.**

```ts
interface Polyline { pts: { x: number; y: number }[]; }
```

- **Route** — call `getSmoothStepPath({sourceX: sx, sourceY: sy, sourcePosition: sourcePos,
  targetX: tx, targetY: ty, targetPosition: targetPos, borderRadius: 0, offset: STEP_OFFSET,
  centerX, centerY})` to obtain the canonical elbow, **and reproduce its corner points
  directly** — with `borderRadius: 0` the returned path is a pure `M … L … L …` polyline, so
  the point list can be parsed back out of the string with one regex, or (preferred, and
  stated as the implementer's call in the PR) the same 2–3 corner points can be computed
  inline from `sourcePos`/`targetPos`/`centerX`/`centerY`, since with radius 0 there is no
  curve fitting left to reproduce. **Do not hand-roll the case analysis for which side faces
  which** — call `getSmoothStepPath` and use it, even if only to validate the inline result
  during development.
- **`path` — and the ONE geometry everything reads (review finding 9).** The draft rendered a
  filleted path while computing `at()`/`offsetPath` from the sharp-cornered `pts`, so chips
  could sit up to `CORNER_R` off the visible cable and — worse — the best-path arrowheads
  (`:411`) and the Watcher's `animateMotion` flows (`:443`) would turn **square corners beside
  a visually rounded cable**, which reads as a rendering bug at every elbow. **Fixed by
  building one segment list and deriving all three from it:**

  ```ts
  type Seg =
    | { kind: "line"; a: Pt; b: Pt }
    | { kind: "quad"; a: Pt; c: Pt; b: Pt };  // c = the original sharp vertex
  ```

  Construction from `pts`: each interior vertex `v` between segments `u→v` and `v→w` is
  replaced by a **quadratic fillet** whose endpoints are `v` pulled back by
  `r = min(CORNER_R, |uv|/2, |vw|/2)` along each adjoining segment, and whose control point is
  `v` itself. The two adjoining line segments are shortened to those endpoints. `CORNER_R = 4`
  (`DESIGN.md` §5's "gently rounded" character over a hard 90°); the `min` clamp is what keeps
  a very short segment from producing a self-crossing fillet.
  - **`path`** is emitted from `Seg[]`: `L` for lines, `Q c b` for fillets. There is no second,
    "sharp" path string anywhere in the module.
  - **`pts` is retained only as an intermediate.** Nothing outside `structuredGeometry` sees
    it. *Gate-checkable: `routing.ts` exposes no `pts` on `LinkGeometry`.*
- **`at(t)`** (curve parameter — §11.2a) — precompute each segment's length once per geometry
  build (3–9 segments after filleting), then a linear walk to the containing segment and a
  local evaluation: lines interpolate, fillets use **the same closed-form quadratic evaluator
  Free mode uses** (`:291-297`), which is the reuse that makes this cheap. Fillet lengths use
  the standard `(chord + control-polygon)/2` approximation — for a ≤4px fillet the error is
  sub-pixel and it costs no iteration. O(segments), no DOM, no allocation in the hot path.
- **`offsetPath(d, reversed)`** — offset the **segment list**, not the polyline: each line
  segment shifts along its own normal by `d`; each fillet maps to a fillet with the same
  construction on the shifted lines (endpoints on the shifted neighbours, control point at the
  corner shifted on both axes — exact for perpendicular neighbours, which axis-aligned routes
  always are). Emit in reverse order, with each segment reversed, when `reversed`. **The
  Watcher's dashed flows therefore ride a rounded path parallel to a rounded cable**, and the
  arrowheads round the same corners the cable does.
- **`sChip` / `tChip` / `watcherChip`** — `at(0.22)`, `at(0.78)`, `at(0.5)`, with the same
  per-parallel `tShift` nudge as `:310-315`. Because `at()` now walks the *rendered* segment
  list, **the chips sit on the cable exactly, not within a fillet radius of it** — which is
  brief:46's "Endpoint attachments must remain visually tied to the correct interface."
- **`sOrigin` / `tOrigin`** — derived from the first and last segment's direction rather than
  the global `dx` sign (`:346-347`), so a chip's hover-pop still expands *away* from its node.

### 11.4 No new dependency

`@xyflow/svelte ^1.6.1` already exports `getSmoothStepPath` (§5.5). `app/package.json` gains
nothing. `docs/build.md`'s `npm ci` path is untouched. **If an implementing agent finds
themselves adding a routing library, the design was misread.**

### 11.5 Lanes and node avoidance — what ships and what does not

**Ships: parallel-link lane separation.** The existing `parallelSign` (`:237-241`) already
gives each link in a parallel group a distinct signed slot. In Structured mode that sign
becomes a **lane offset applied to the elbow's shared axis**: pass
`centerX = midX + parallelSign * LANE_SPACING` (for a horizontal-dominant route) or
`centerY = midY + parallelSign * LANE_SPACING`. Two parallel links then run down two distinct
vertical lanes instead of overlapping — brief:45's "keeping parallel links distinguishable",
achieved with a value the code already computes.

**Ships: grid quantisation.** `LANE_SPACING` and `STEP_OFFSET` are multiples of the canvas's
own 20px dot gap (`CanvasInner.svelte:1000`), and lane centres are rounded to that gap. This
is brief:44's "aligned to the canvas snap grid" — and note it is achieved **without reading
the snap-grid preference** (§4.4), which is what keeps the two controls independent.

**Does NOT ship: obstacle avoidance, and cross-link shared lanes.** Routing a link *around*
an intervening node, or merging several links into one trunk lane, requires a **global**
solver over all nodes and links — a visibility graph or a grid A* — whose output is not a
per-edge pure function. That breaks §11.1.5's flow-space-purity invariant in practice: a
global solver's result for edge A depends on edges B…Z, so adding one link re-routes the
topology, which is precisely the instability brief:47 forbids. **It is a named non-goal
(§17)** with the honest reason stated, not omitted.

The consequence must be stated in the UI, not hidden: a Structured route **may** pass under a
node. `DESIGN.md`'s existing chip treatment already masks the cable behind an opaque chip
(`FloatingEdge.svelte`'s `.port-chip` background comment), and a node face is opaque, so the
visual result is a cable that disappears behind a device — legible, if not ideal. **Do not
add a "link goes under a node" warning.** Ship the mode, name the limitation in
`docs/` and in the PR.

### 11.6 The two preferences, and where they live

Per §5.9: **`labTypes.ts` + `contracts/lab.schema.json` only. Zero Go changes.**

```ts
export type LinkLayout = "free" | "structured";
export interface LabCanvas {
  zoom?: number;
  pan?: { x?: number; y?: number };
  background?: CanvasBackground;
  /** Presentation-only edge routing mode. Missing = "free". */
  linkLayout?: LinkLayout;
  /** Node drag snap-to-grid. Missing = false. Independent of linkLayout. */
  snapGrid?: boolean;
}
```

`contracts/lab.schema.json:24-37` gains both properties (it has
`"additionalProperties": false` at `:27`, so omitting them would make the contract lie about a
document the GUI writes).

- **`snapGrid` is consumed by exactly one line**: `snapGrid={snap ? [20, 20] : undefined}` on
  `<SvelteFlow>` (`CanvasInner.svelte:976-1000`).
- **`linkLayout` is consumed by exactly one line**: the `geom` selector in §11.2.
- **Neither reads the other. Anywhere.** Grep-checkable in the gate.
- Both get menu entries in §7.4's overflow menu under a new **Canvas** group: a
  `role="menuitemradio"` pair *Link layout: Free / Structured*, and a
  `role="menuitemcheckbox"` *Snap grid*. Writing either marks the lab dirty and triggers the
  existing debounced autosave (`labStore.scheduleAutosave`, `:778-783`) — which is how a
  per-lab display preference persists, with no new save path.

### 11.7 The transition, honestly

A CSS `transition` on an SVG `<path>`'s `d` attribute does not interpolate between a quadratic
and a polyline (and browser support for `d` interpolation requires identical command lists,
which these do not have). **Decision: cross-fade, do not morph.** On a mode change, render
both geometries for 150ms with the outgoing one fading to `opacity: 0` and the incoming from
`0` to `1`, then drop the outgoing. Under
`@media (prefers-reduced-motion: reduce)` the swap is instant. Six such media blocks already
exist in `app/src`; match their form. **Do not attempt `d` morphing** — it will silently do
nothing on the platforms that matter and the PR would claim an animation that never runs.

### 11.8 Concrete file-level changes

- **New `app/src/lib/edges/routing.ts`** — `LinkGeometry`, `RouteInput`, `freeGeometry`,
  `structuredGeometry`, the polyline helpers, `LANE_SPACING`/`STEP_OFFSET`/`CORNER_R`.
- **`app/src/lib/edges/FloatingEdge.svelte`** — `:248-349` shrinks to input assembly + the
  mode selector + the cross-fade. **The template from `:352` to its end (`:552`) is
  unchanged**, except the
  cross-fade wrapper. Expected diff: ~90 lines removed, ~45 added.
- **`app/src/lib/edges/floating.ts`** — unchanged (its `sourcePos`/`targetPos` finally get a
  consumer). **Do not "clean up" the now-used fields.**
- **`app/src/lib/components/CanvasInner.svelte`** — one `snapGrid` prop.
- **`app/src/lib/labTypes.ts`**, **`contracts/lab.schema.json`** — §11.6.
- **`app/src/lib/components/TopBar.svelte`** — two menu groups in `overflowItems()`.

### 11.9 Testing bar

1. `npm run check` green.
2. **The refactor gate, before the feature:** after landing `freeGeometry` alone, a lab with
   parallel links, a capture, an STP paint, a Watcher row and a fault renders **identically**
   — compare screenshots.
3. Live (§15), with a lab containing: two directly-linked routers, a 3-way parallel bundle, a
   node with 4 links at different angles, and one link whose straight route passes over a
   third node:
   - Toggle Free ↔ Structured repeatedly. Node positions, `lab.links` and every
     `endpoint.interface` are byte-identical before and after (dump `labStore.lab` in the
     console and diff) — brief:85.
   - In Structured mode, **all twelve consumers of §11.2b, checked by number** — the two the
     draft's inventory missed are checked last and explicitly: both port chips sit **on** the
     cable at both ends (8, 9); the Watcher's dashed flows ride **beside** the cable in both
     directions (7); the best-path arrows follow the orthogonal path (5); the STP blocked
     overlay traces the same route (4) and its reason popover opens on it (10); the fault pill
     sits at the midpoint (3); the traffic glow tracks the cable (1, 2); the invisible hover
     catcher follows the route — hover 4px off the cable and the link still highlights (6);
     **paint a routing best path and confirm the metric pill sits on the cable (11)**; and
     **arm two Watcher rows and confirm both label pills stack on the cable, 18px apart (12)**.
   - **Corners (finding 9):** at a Structured elbow, the cable is visibly rounded **and** the
     best-path arrowhead and the Watcher's dashed flow round the same corner — neither cuts a
     square turn beside a rounded cable. Zoom to 2.5 on one elbow and screenshot; the offset
     flow stays parallel through the corner, not crossing or pinching.
   - Parallel links occupy visibly distinct lanes.
   - Zoom from 0.15 to 2.5 and pan to each corner: **the route does not change shape.**
     Screenshot at three zooms and compare the polyline vertices in flow coordinates.
   - Resize the browser window from 1920 to 900 wide: same — no reroute.
   - Drag a node in Structured mode: links update continuously and remain orthogonal
     (brief:64).
   - Toggle snap grid **on** while in **Free** mode, and **off** while in **Structured**:
     both combinations work, proving independence (brief:87).
   - Reload the page: the lab reopens in its saved mode. Open a lab saved before this batch
     (no `linkLayout` field): it opens in Free (brief:40).
   - `prefers-reduced-motion: reduce` (devtools emulation): the mode swap is instant.

### 11.10 Acceptance gate

2–3 above, plus:
- `grep -rn "innerWidth\|innerHeight\|canvasZoom\|getViewport\|screenToFlow" app/src/lib/edges/routing.ts`
  is **empty** — the flow-space-purity invariant (§11.1.5), grep-checkable.
- `grep -n "snapGrid" app/src/lib/edges/` and `grep -n "linkLayout" app/src/lib/components/CanvasInner.svelte`
  are both **empty** — the independence invariant (§11.6), grep-checkable in both directions.
- `git diff app/package.json` is **empty** (§11.4).
- `git diff supervisor/` is **empty**.
- `git diff app/src/lib/edges/FloatingEdge.svelte` shows **no change** between the template's
  first consumer line (`:352`) and **the end of the template (`:552`, not `:530`)** except the
  cross-fade wrapper — the corrected range from §11.2b, so the best-path metric pill (`:537`)
  and the Watcher label pills (`:545`) are actually inside the gate (finding 10).
- **Finding 3:** `grep -rn "arc.length\|arcLength" app/src/lib/edges/` is **empty**, and
  `routing.ts`'s `at` is documented as curve parameter. The Free-mode pixel-identity gate
  (§11.9.2) is the enforcement — a switch to true arc length fails it on any bowed edge.
- **Finding 9:** `grep -n "pts" app/src/lib/edges/routing.ts` shows `pts` only inside
  `structuredGeometry`'s construction — it is not a field of `LinkGeometry`, and `at`/
  `offsetPath` close over the **segment list**, not over `pts`.
- Brief criteria **84** ("Free Flow preserves current link rendering; Structured provides
  grid-aligned orthogonal routing"), **85** (no mutation) and **87** (independent controls)
  are each demonstrated in the PR.

---

## 12. Batch 17 — link hover, selection, and endpoint interface labels

**Must follow Batch 16** (§6). Its central claim — "Structured and Free Flow modes must have
identical hover, selection, and interface-label behavior" (brief:58) — is unverifiable before
Structured exists.

### 12.1 Decisions locked

1. **Nothing that already works is rebuilt** (§4.3): not the 18px catcher, not the two
   `EdgeLabel` chips, not `.chip-detail`, not the `hot` mirroring. **The catcher is not
   shrunk to 14px.**
2. **Selection gets hover's emphasis.** `hot` becomes
   `$derived(hovered || selected)` where `hovered` is the existing local `$state` (`:151`)
   renamed. One line; it lights both chips, both `.chip-detail`s, and the cable, and it
   persists until selection changes — brief:57.
3. **Escape clears link selection.** A `<svelte:window onkeydown>` in `CanvasInner.svelte`
   sets `labStore.selectedLinkId = null` (and, for consistency, touches **neither**
   `selectedNodeId` nor `inspectorNodeId` — node selection has its own affordances and the
   brief only asks for the link case; note §9.2's dismissal check therefore uses a pane click,
   not Escape). Guard on `e.key === "Escape"` and on the event not originating inside an input
   or a terminal.
4. **Chips are clamped inside the viewport, and there is a real mechanism** (brief:56 — a hard
   acceptance criterion; review finding 8 rejected the draft's "CSS where possible, otherwise
   accept it"). **CSS cannot do this.** `EdgeLabel` hardcodes
   `style:transform="translate(-50%,-50%) translate({x}px,{y}px)"` from flow coordinates
   (`node_modules/@xyflow/svelte/dist/lib/components/EdgeLabel/EdgeLabel.svelte:30`) inside a
   portalled, viewport-transformed container. No selector can see the viewport edge from
   there, so a CSS-only "clamp" would have been an acceptance criterion knowingly shipped
   broken.

   **The mechanism: clamp in JS, in flow space, from a mirrored viewport — arithmetic only, no
   DOM measurement.** Three parts:

   - **Batch 16/17 publishes the projector** `labStore.flowToScreen` and mirrors the viewport
     translation alongside the existing `labStore.canvasZoom` (`:50`, written at
     `CanvasInner.svelte:997` on `onmove`): the same handler also writes
     `labStore.canvasPan = { x, y }` from `getViewport()`. This is the *same* two-line addition
     §10.4 needs for the floating layer — **one shared solution, wired once** (§10.4 records
     that Batch 16/17 owns these lines and Batch 15 only reads them).
   - **The chip computes its own screen position by arithmetic**, not by measuring:
     `screen = flow * zoom + pan` (plus the canvas area's own client offset, a constant read
     once per mount). A `$derived` in `FloatingEdge.svelte` produces a **clamp delta in flow
     units** — `dx = clamp(screenX, m, W - m) - screenX`, converted back by `/ zoom` — and the
     chip renders at `at(t) + delta`. `m` is a margin of half the chip's nominal width plus
     8px; the chip's nominal size is a **token-derived constant**, not a measurement, which is
     what keeps this off the layout path.
   - **This is not §11.3's rejected approach.** What was rejected there is *DOM layout
     measurement* (`getPointAtLength`, `getBoundingClientRect`) inside a `$derived` that reruns
     per animation frame during a drag. This is four multiplications on values the store
     already mirrors — no reflow, no node creation, and it recomputes on exactly the same
     dependency graph the edge already has.

   **Scope guards, so this stays small:** clamping applies **only** to the two endpoint
   interface chips (`:466-519`), not to the fault pill, best-path pill, Watcher pills or STP
   popover — the brief asks for the interface labels, and clamping every label would make
   overlapping stacks at a viewport corner. Clamping is **display-only**: it never moves the
   anchor the chip's leader/hover-pop is computed from, and the chip visibly "sticks" to the
   edge of the viewport while its cable continues off-screen, which is the intended read.
   **`routing.ts` is untouched by any of this** — the clamp lives in `FloatingEdge.svelte`'s
   template layer, so §11.1.5's flow-space-purity grep gate over `routing.ts` still passes,
   and §11.10's gate is unchanged.
5. **Semantic state colors are preserved** (brief:56). The existing precedence — admin-down,
   impaired, unexpectedly-down, capture-amber, STP red/amber, traffic glow, best-path — is
   unchanged; `hot`/selected emphasis composes **on top**, never replaces. `:562-575`'s
   cascade order is the contract.

### 12.2 Concrete file-level changes

`FloatingEdge.svelte`: rename `hot` → `hovered`, add `const hot = $derived(hovered || selected)`,
one `.is-selected` CSS rule that no longer needs to carry emphasis alone, and the endpoint-chip
clamp `$derived` (§12.1.4).
`CanvasInner.svelte`: the Escape handler, plus the `labStore.canvasPan` mirror and the
`labStore.flowToScreen` wiring/teardown if Batch 16 has not already landed them (§10.4).
`labStore.svelte.ts`: the two new fields only — `canvasPan`, `flowToScreen` — declared beside
their existing siblings `canvasZoom` (`:50`) and `screenToFlow` (`:53`).
Expected diff: ~55 lines across three files.

### 12.3 Testing bar and gate

In **both** modes, and the PR must show both: hover a cable → both chips expand and reveal
`R1 e0/0` and `SW1 e0/1` **separately at their own ends**, never merged at the centre; hover
either chip → same; click the cable → the same emphasis persists after the pointer leaves;
click another link → the first releases; press Escape → selection clears; a link with an
active fault keeps its fault color while selected; a capturing link keeps its amber. Drag a
node with a link selected → emphasis follows.

**Clamping (finding 8), demonstrated, not asserted** — in both modes: pan a lab until one
node is fully off-screen past the left edge → its interface chip **stays visible, pinned
inside the viewport**, still showing its own interface, while the cable runs off-screen.
Repeat for the right, top and bottom edges. Pan so **both** ends are off opposite edges → both
chips clamp, at opposite edges, and neither jumps to the centre. Zoom to 0.15 and to 2.5 and
repeat one edge — the clamp margin is constant in screen pixels, so the chip does not creep.
Drag a node continuously along the viewport edge with devtools' FPS meter open → no measurable
frame-time change versus the same drag before this batch (the "arithmetic, not measurement"
claim). `read_page` confirms the clamped chip still carries its accessible name.

**Gate.** Brief criteria **86** ("Hovering a link clearly identifies the interface name on both
connected nodes in either mode") and **58** are demonstrated in each mode; brief:56's clamp is
demonstrated by the four-edge pan above. `grep -rn "getBoundingClientRect\|getPointAtLength" app/src/lib/edges/`
is **empty** (the clamp is arithmetic — §12.1.4, and §11.3's rejection still stands).
`grep -rn "innerWidth\|getViewport" app/src/lib/edges/routing.ts` is still **empty** (§11.10
unchanged — the clamp lives in the template layer, not the router).
`git diff --stat` shows three files.

---

## 13. Batch 13 — bottom resource bar

### 13.1 Decisions locked

1. **`--statusbar-h: 26px` (`theme.css:54`) is the height token.** It already exists and is
   used nowhere; this is what it was reserved for.
2. **Four cells: CPU · RAM · Connection · Elapsed.** **No RTT** (§4.7) — shipping a number
   labelled RTT that means GUI↔supervisor transport latency, next to CPU and RAM, invites a
   network engineer to read it as data-plane latency. §17 records the deferral and the
   cheapest honest implementation.
3. **No disk cell.** The mockup has none, and `Palette.svelte`'s disk bar was a host-monitor
   affordance, not a lab one. Disk moves into the **Tools** flyout's host section (§8.1.2) or
   is dropped — implementer's call, stated in the PR.
4. **Each cell is `label · value · LED dot`**, per the LED Rule (`DESIGN.md` §2) — the mockup's
   dots earn their color from `--state-running` / `--state-starting` / `--state-crashed` at
   `Palette.svelte:85-87`'s existing thresholds (>75% amber, >90% red). **Not `--accent`**
   (§5.7).
5. **Values in mono, labels in the UI face** — the Mono-For-Data Rule (`DESIGN.md` §3).
6. **The bar is a sibling of `.body` inside `.shell`**, not inside `.center-col`. It spans the
   full window width beneath the rail and the canvas, as the mockup shows.
7. **It never grows.** No bars, no sparklines, no expansion on click. `DESIGN.md` §6 forbids
   metrics-dashboard chrome, and brief:90 asks for "compact and legible".
8. **Elapsed time is a client clock** (§4.7): set on `labRunning` false→true, cleared on
   true→false, formatted `HH:MM:SS` off the existing 1s `labStore.nowTick` (`:142`, `:198`) —
   **no new timer**. Its `title` says "since this page observed the lab start".

### 13.2 – 13.4 Concrete changes, testing, gate

**New `app/src/lib/components/ResourceBar.svelte`** — reads `labStore.hostStats`,
`labStore.providerStatus`, `labStore.activeProvider`, `labStore.labRunning`,
`labStore.nowTick`. Renders the "Waiting for host stats…" state when `hostStats` is null
(matching `Palette.svelte:431-433`) rather than zeros.
**`App.svelte`** — mount it after `.body`.
**`theme.css`** — no new token; `--statusbar-h` is consumed.
**`Palette.svelte`** — the host block (`:410-438`) is removed here **or** in Batch 14, not
both; §6's ordering puts Batch 13 first, so Batch 13 removes it and Batch 14 deletes the file.

**Testing:** the mock transport's synthetic `host.stats` (`mockTransport.ts:100-132`) drives
the bar in `npm run dev` — CPU/RAM update every 2s; the dots cross amber at 75% and red at 90%
(force by editing the mock's synthetic values in a scratch run, not in the committed file);
the connection cell reflects `providerStatus`; starting the mock lab starts the elapsed clock
and stopping it clears it; the bar's height measures exactly 26px; the canvas area shrinks by
exactly 26px and nothing overlaps. **Gate:** brief criterion **90**; `git diff supervisor/` is
empty; `grep -rn "var(--accent)" app/src/lib/components/ResourceBar.svelte` is empty (the One
Accent Rule).

---

## 14. Batch 18 (auto-hide) and Batch 19 (visual pass + `DESIGN.md`)

### 14.1 Batch 18 — auto-hide chrome

**New `app/src/lib/chromeStore.svelte.ts`** — the only new mechanism (§5.8 established nothing
like it exists).

- `hidden = $state(false)`, `enabled = $state(false)` persisted at `iolbox.chrome.autohide`,
  **default off** (a tool that hides its own controls on first run is hostile; brief:18 says
  "When a lab is running or actively being edited", which is a gate, not a default).
- **Show triggers:** any `pointermove` within 8px of the viewport's top or bottom edge; any
  `pointermove` at all (with a 250ms debounce, resetting the hide timer); `keydown` of the
  dedicated shortcut (**Alt**, matching the mockup's "press Alt to reveal"); any `focusin`
  within a chrome surface.
- **Hide condition:** `enabled && labStore.labRunning && idle > 2000ms && !suppressed`.
- **`suppressed` is a `$derived` over §5.8's exact five-row table** — an open menu, an active
  drag, a focused control, a modal, an error state. **The table in §5.8 is the specification;
  reproduce it row for row.** A suppression list that says "if a menu is open" without naming
  the eight menu components is not implementable.
- **`hold()` — the primitive two of those five rows require** (review finding 4; §5.8's third
  column is the evidence). Three of the five rows read shared state (`labStore` fields,
  `annoTool`) and cost nothing. **Two read `$state` that is private to a component and
  therefore cannot be observed from a store at all.** `chromeStore` gains:

  ```ts
  private holds = $state(0);
  /** Take a suppression hold for as long as something is open/dragging.
   *  Returns the release; calling it twice is a no-op. */
  hold(): () => void
  ```

  `suppressed` includes `holds > 0`. Callers use it from a single `$effect` whose teardown
  releases, so an unmount can never leak a hold (and a leaked hold's worst case is
  chrome that stays visible — the safe direction).

- **The two components Batch 18 must edit to make the criterion implementable, and the exact
  state each exposes:**
  - **`app/src/lib/components/ContextMenu.svelte`** — owns no open flag; its *existence* is the
    open state (`{#if nodeMenu}` in each parent, e.g. `CanvasInner.svelte:495-501`). Add one
    `$effect(() => chromeStore.hold())` at the top of the component, so **every** menu instance
    — the four canvas call sites, §7's overflow menu, and anything added later — suppresses
    auto-hide for its lifetime without `chromeStore` knowing any of them by name. **~3 lines.**
  - **`app/src/lib/components/SplitPane.svelte`** — owns `let dragging = $state(false)` (`:40`),
    set in `onPointerDown` (`:52`) and cleared on pointer-up, and nothing outside can read it.
    Add one `$effect` that takes the hold while `dragging` is true and releases when it goes
    false. **~4 lines.** (This is the dock divider; without it, dragging the console dock taller
    for >2s while a lab runs makes the top bar vanish mid-drag.)
  - The four non-`ContextMenu` popovers — `AnnoStylePopover.svelte`, `ChangeImagePopover.svelte`,
    `IconPicker.svelte`, `InterfacePicker.svelte` — take the same lifetime hold, one line each,
    for the same reason `ContextMenu` does.
  - `dragMove.ts` (Batch 15) takes the hold in its drag start/end; `annoTool.active`, the
    xyflow node drag, the four modal flags and the two error fields need **no** component edit.

- **Batch 18's file list is therefore:** new `chromeStore.svelte.ts`, `App.svelte`,
  `TopBar.svelte`, `IconRail.svelte`, `ResourceBar.svelte`, **`ContextMenu.svelte`**,
  **`SplitPane.svelte`**, **`AnnoStylePopover.svelte`**, **`ChangeImagePopover.svelte`**,
  **`IconPicker.svelte`**, **`InterfacePicker.svelte`**, **`dragMove.ts`**. §6's table carries
  the same list. The seven additions are each a single `$effect`; none changes a component's
  props, markup or behavior when auto-hide is off (the default).
- **Hiding is `transform: translateY()` + `opacity`, never `display: none` and never a layout
  change.** The canvas must **not** resize when chrome hides — a canvas that reflows every
  2 seconds is unusable, and xyflow would refit on every transition.
- Consumers: `TopBar.svelte`, `IconRail.svelte`, `ResourceBar.svelte` each add one
  `class:chrome-hidden` and one CSS rule, plus `@media (prefers-reduced-motion: reduce)`.
  The floating console layer is **never** hidden (a console is content, not chrome).

**Gate:** brief criterion **91** ("Auto-hide never removes controls during keyboard focus,
open menus, dragging, modal interactions, or errors") is demonstrated **five times, once per
row of §5.8's table**, and the PR names the component it exercised for each. Each run holds the
condition for **>4s** (twice the 2000ms idle threshold) with chrome still visible.

**The two rows that finding 4 showed were unimplementable get named exercises**, because they
are the two that would otherwise be "demonstrated" against a suppression the code cannot
actually observe:
- **open menu** — right-click the canvas to mount a `ContextMenu`, then do not move the mouse
  for 5s: the top bar, rail and resource bar are all still visible. Repeat with §7's overflow
  menu (the same component, a different call site — proving the lifetime hold covers both).
- **active drag** — press and hold the `SplitPane` divider (dock placement, console open) for
  5s without releasing: chrome stays. Release → the hide timer resumes.

`grep -n "chromeStore" app/src/lib/components/ContextMenu.svelte app/src/lib/components/SplitPane.svelte`
is **non-empty** — the grep-checkable form of finding 4. `git diff --stat` matches the
twelve-file list above, and the seven hold-only files show ≤5 changed lines each.

### 14.2 Batch 19 — the visual pass and the `DESIGN.md` step

1. **`DESIGN.md` gains two deliberate entries** (§4.9's token budget): a `rail-button`
   component entry (40×40, icon-only, the brief's hit-area floor — distinct from §5's existing
   28px icon button) and a `floating-window` component entry (`--panel` + `--blur` +
   hairline + `--radius-md` + `--shadow-md`, matching `WatcherPanel`/`PainterPanel`).
   **No new color.** If any batch needed one, it was a mistake (§4.9).
2. **The semantic z-index scale `DESIGN.md` §6 already mandates lands as named tokens** in
   `theme.css`: `--z-canvas`, `--z-topbar`, `--z-panel`, `--z-float`, `--z-menu`,
   `--z-dialog`, `--z-modal`, `--z-tooltip`, mapping onto §5.10's observed bands so nothing
   moves. §10.8's `FLOAT_Z_BASE = 900` becomes `--z-float`.
3. **Audit against the three resolved conflicts (§4.9):** no new gradient anywhere in the
   diff; no glow on a resting surface; the Glass theme still works on every new surface (the
   rail, the flyout, the floating window, the resource bar) — **check both themes on every
   new component**, which `DESIGN.md` §6 requires and which is the single most commonly
   skipped step.
4. **Type audit:** every value a CLI would print (node names, `e0/0`, ports, CPU %, RAM, the
   elapsed clock) is `--font-mono`; every label and button is `--font-ui`. The
   Mono-For-Data Rule is grep-auditable per new component.
5. **Reduced-motion audit:** the cross-fade (§11.7), the auto-hide transition (§14.1), the
   flyout open, the window raise. Six `prefers-reduced-motion` blocks exist today; every new
   animation adds one.

**Gate:** brief criterion **92** in full; `DESIGN.md`'s frontmatter `colors:` block is
**unchanged** (proving no new color was needed); every `z-index:` literal in
`app/src/lib/components/` introduced by batches 12–18 has been replaced by a token.

---

## 15. Verification approach — the browser-pane tools, not Playwright

The brief's kickoff prompt says *"Use Playwright against the running app"* (brief:96).
**Playwright is not installed in this environment** and `app/package.json` has no test runner
at all (§7.5). Adding one is out of scope.

**The equivalent, and what this plan mandates: Claude's own Browser-pane MCP tools driven
against the mock-transport dev server.** This path was exercised successfully earlier in this
session against this exact app.

- **Start:** `cd app && npm run dev`, then open `http://localhost:1420` in the browser pane.
  The dev server uses the **mock transport** (`app/src/lib/mockTransport.ts`), which
  synthesises `host.stats` every 2s (`:100-132`), so §13's resource bar and §5.7's telemetry
  are all exercisable without a runtime VM.
- **Prefer `read_page` over `screenshot`** for asserting text, structure, ARIA roles and
  `aria-pressed`/`aria-checked` state — it returns the accessibility tree, which is what most
  of §16's checks are actually about. Use `screenshot` only for the genuinely visual ones
  (§11's route-shape comparisons, §14.2's two-theme audit).
- **Use `javascript_tool` for measurement**, not eyeballing: element rects for the 40×40 hit
  area and the 26px bar, `getComputedStyle` for `display: none` vs `visibility: hidden`,
  `JSON.stringify(labStore.lab)` before/after a mode toggle for §11's no-mutation proof.
- **Use `resize_window`** for §10.9's third clamp site and §11's viewport-stability check, and
  its `colorScheme` for the two-theme audit.
- **One agent at a time.** Parallel subagents driving the browser pane cause screenshot
  timeouts; if batches 15 and 16 run concurrently (§6), their live verification must not.

## 16. The brief's ten acceptance criteria, as concrete checks

| # | brief:83-92 | batch | concrete check |
|---|---|---|---|
| 1 | Left rail exposes exactly five groups, expands only on demand | 14 | `read_page`: the rail's `role="toolbar"` has exactly 5 `button` children; after reload no flyout node exists in the tree |
| 2 | Free Flow preserves current rendering; Structured is grid-aligned orthogonal | 16 | screenshot diff of a fixed lab in Free, pre- vs post-batch; in Structured, `javascript_tool` reads every cable `<path>`'s `d` and asserts all segments are axis-aligned within the fillet radius |
| 3 | Switching modes does not mutate topology, move nodes, or change interfaces | 16 | `javascript_tool`: `JSON.stringify` the lab doc before and after two toggles — byte-identical except `canvas.linkLayout` |
| 4 | Hovering a link identifies the interface at both ends, in either mode; labels stay clamped inside the viewport and clear of node content | 17 | `computer` hover over the cable, then `read_page`: two distinct chips, each containing its own node name + interface; repeat in the other mode. **Clamp (finding 8):** pan a node fully off each of the four viewport edges in turn — its chip stays visible and inside, via §12.1.4's arithmetic clamp |
| 5 | Snap grid and link layout are independent | 16 | all four combinations exercised; grep gate (§11.10) proves no code path couples them |
| 6 | Secondary top-bar actions available from the three-dot menu | 12 | each of §5.1's ten "MOVES" rows activated from the menu and its effect observed |
| 7 | Consoles float, move, resize, minimize, restore — without shrinking the canvas | 15 | `javascript_tool` records `.canvas-area` `getBoundingClientRect()` with 0, 1 and 3 windows open and 1 minimized — identical every time. The three windows must include **one of each pane kind** (console, capture, **Lens**) so finding 2's third arm is exercised here too |
| 8 | The bottom resource bar stays compact and legible | 13 | measured height === 26px; all four cells present at 900px viewport width without wrapping or truncation |
| 9 | Auto-hide never removes controls during focus, menus, drag, modals, errors | 18 | five separate runs, one per §5.8 row, each holding the condition >4s with chrome still visible |
| 10 | All controls keyboard-operable, accessible names/tooltips, reduced-motion respected | all | `read_page` over the rail, the menu, the window title bar and the bar: every interactive node has an accessible name; a full `Tab` traversal reaches every control; devtools reduced-motion emulation on for one pass of §11.7 and §14.1 |

---

## 17. Explicit non-goals for the implementing agents

**Global**
- **Do not add a test runner to `app/`.** (§7.5.)
- **Do not add an npm dependency.** (§11.4.)
- **Do not touch `supervisor/`.** Every batch's gate asserts an empty `supervisor/` diff.
- **Do not remove the Glass theme, the theme toggle, the brand-mark gradient, or the node-face
  gradient** on the strength of brief:78. (§4.9.)
- **Do not introduce a hex literal.** Add the token to `DESIGN.md` first. (§4.9.)

**Batch 12**
- Do not build a second menu component; extend `ContextMenu.svelte`. (§7.1.2.)
- Do not move the hidden file input into the menu. (§7.1.4.)
- Do not retune `--topbar-h`. (§7.1.6.)

**Batch 14**
- Do not add a sixth rail button, a "select" mode, or a "link" mode. (§4.1.)
- Do not change the `dataTransfer` payload shape. (§8.1.8.)
- Do not persist which flyout was open. (§8.3.)
- Do not make the flyout reserve layout width. (§8.1.4.)
- Do not add a **Configure** button to `NodeActions.svelte`, and do not resize its buttons.
  (§9.1.) **Restart IS in scope** — one `labStore.restartNode` wrapping the existing
  `node.restart` verb (§4.5, §9.1.3, review finding 5). Do **not** implement it as a
  client-side `stopNode` → `startNode` compound.
- Do not key the toolset's reveal — or anything else in this plan meaning "a node is selected"
  — off `inspectorNodeId`. It is `selectedNodeId`. (§5.6, review finding 6.)

**Batch 15**
- Do not implement `docs/p7-…-plan.md` §6 separately. (§3.2.)
- Do not add a third `DockSide` value or a fourth `ConsoleLayout` value. (§3.1, §10.1.1.)
- Do not delete the tiled/tabbed dock. (§3.1.)
- Do not overload `consoleUiStore.pinned` for "keep on top" — the float pin is
  `pinnedWindows`. (§10.2.3, §10.5.)
- Do not unmount a minimized pane. (§10.5.)
- Do not call `useSvelteFlow()` from `FloatingConsoleLayer.svelte` — it is outside
  `Canvas.svelte`'s provider. Read `labStore.flowToScreen`. (§10.4, finding 1.)
- Do not enumerate floating panes from console + capture tabs alone; the Lens is a third arm
  with its own open/close lifecycle. (§10.4a, finding 2.)
- Do not use `window.addEventListener` for the drag. (§10.1.7.)
- Do not let the store compute window placement. (§10.4.)

**Batch 16**
- Do not add `if (structured) path = …` without the `LinkGeometry` interface. (§11.2.)
- Do not use `getPointAtLength` in the reactive path. (§11.3.)
- Do not build a global router, a visibility graph, or A*. (§11.5.)
- Do not read the viewport, zoom or window size in `routing.ts`. (§11.1.5.)
- Do not couple snap grid and link layout. (§11.6.)
- Do not attempt `d`-attribute morphing. (§11.7.)
- Do not redefine `at()` as arc length, and do not describe it as such. It is curve-parameter
  position, and Free Flow's pixel-identity gate depends on that. (§11.2a, finding 3.)
- Do not compute `at()`/`offsetPath` from the unfilleted polyline while rendering a filleted
  one. One segment list feeds all three. (§11.3, finding 9.)
- Do not add `linkLayout`/`snapGrid` to `supervisor/internal/lab/lab.go`. (§5.9.)

**Batch 17**
- Do not shrink the 18px hover catcher. (§4.3.)
- Do not rebuild the port chips or `.chip-detail`. (§4.3.)
- Do not add per-frame screen-space **measurement** (`getBoundingClientRect`,
  `getPointAtLength`, any layout read) to the edge. (§12.1.4.) The chip clamp is **arithmetic**
  over the mirrored zoom/pan — that is required, not forbidden (finding 8). "CSS-only clamping"
  is not an option: xyflow hardcodes the label transform
  (`EdgeLabel.svelte:30`).

**Batch 13 / 18 / 19**
- Do not ship a value labelled "RTT", "latency" or "link" derived from an RPC round trip.
  (§4.7, §13.1.2.)
- Do not make the resource bar expandable or add sparklines. (§13.1.7.)
- Do not default auto-hide to on. (§14.1.)
- Do not hide chrome with `display: none` or any layout-affecting property. (§14.1.)
- Do not hide the floating console layer. (§14.1.)

**Deferred, named rather than silently dropped**
- **Link subnet / IP labels** (the mockup's `10.0.0.0/30`). No data source exists; both
  candidate mechanisms are their own project. (§4.2.)
- **Data-plane RTT, and transport RTT.** Both need a real measurement path — for transport
  RTT, a ping/pong verb pair carrying send timestamps. The `Date.now()` delta around an
  in-flight request is **neither**: it includes supervisor handler time, so if a later slice
  ships it, it ships as **"RPC response time"**. (§4.7, review finding 11.)
- **True arc-length positioning along a link** (`atLength(px)`), and any consumer that needs
  equal-distance spacing. `at()` is curve parameter. (§11.2a, finding 3.)
- **Viewport clamping for labels other than the two endpoint interface chips** — the fault
  pill, best-path pill, Watcher pills and STP popover are unclamped by design. (§12.1.4.)
- **Durable lab uptime.** Needs a supervisor-side `startedAt`. (§4.7.)
- **Obstacle avoidance and cross-link trunk lanes in Structured mode.** (§11.5.)
- **Per-pane placement mixing** ("this console floats, that one docks"). (P7 §6.1.2.)
- **Tile drag-reordering.** Now buildable on `dragMove.ts`, still out of scope. (P6 §9.)
- ~~**A Restart node verb** with supervisor-side sequencing.~~ **Removed from this list by
  review finding 5 — the verb already exists and Restart ships in §9.**
- **P7 Batches 11a / 11b** (per-node MAC list, IOL-MAC toggle). Untouched and still
  implementable; §6 records their file collisions with batches 12 and 14.

---

## 18. Verification checklist

Run top to bottom. Every line is checkable by a command or a single observation.

**Before starting any batch**
- [ ] `git status` re-read; every line number in this document re-grepped against the working
      tree (§2 — the tree is 39 files ahead of HEAD `25a1e05`).
- [ ] Confirmed P6 Batches 7/8/9 are on disk (`app/src/lib/lens.ts` exists;
      `stats.go:109` sets `EpAttrib`) and P7 is not (`grep -rn "PaneBody\|dragMove\|ConsolePlacement" app/src`
      is empty).

**Per batch**
- [ ] `cd app && npm run check` — green, zero new diagnostics.
- [ ] The batch's own grep gates (§7.6, §8.5, §10.12, §11.10, §12.3, §13.4, §14.1, §14.2) all pass.
- [ ] `git diff supervisor/` is empty.
- [ ] `git diff app/package.json` is empty.
- [ ] `git diff --stat` matches the batch's declared file list in §6; a file outside it means
      the design drifted — report, do not widen.
- [ ] Live verification per §15, against `npm run dev` at `http://localhost:1420`, with the
      batch's testing-bar list recorded in the PR.
- [ ] Both themes (Bench and Glass) checked on every new or changed surface.
- [ ] `prefers-reduced-motion: reduce` checked for every new animation.

**Across the plan, before calling it done**
- [ ] All ten of the brief's acceptance criteria demonstrated per §16's table.
- [ ] `DESIGN.md`'s `colors:` frontmatter block is **unchanged** — no new color was needed
      (§4.9).
- [ ] `DESIGN.md` gained exactly the two component entries §14.2 specifies.
- [ ] `theme.css` gained the semantic z-index tokens and `--statusbar-h` is now consumed.
- [ ] `grep -rn "z-index: *[0-9]" app/src/lib/components/` shows no new literal introduced by
      batches 12–18.
- [ ] `app/src/lib/components/Palette.svelte` no longer exists and nothing references it.
- [ ] `grep -rn "labStore" app/src/lib/consoleUiStore.svelte.ts` is empty (no import cycle).
- [ ] `git diff app/src/lib/labStore.svelte.ts` is empty across batches 12, 13, 15 and 19, and
      contains **only** the three post-review additions elsewhere: `restartNode` + the
      `holdUntilSettled` lock flag + the `:468` release guard (batch 14's addendum, §9.1.3),
      and the `flowToScreen` / `canvasPan` mirror (batch 16 or 17, §10.4 / §12.1.4). Any other
      hunk means the design drifted.
- [ ] Batch 18's diff includes `ContextMenu.svelte` and `SplitPane.svelte` (§14.1, finding 4);
      a Batch 18 that touches neither has not implemented the never-hide criterion.
- [ ] The PR set states, in one place, the §3 reconciliation outcome: P6 Batch 9 coexists,
      P7 Batch 10 is superseded-by-reuse, and P7 Batches 11a/11b are untouched.

### Critical files for implementation

`app/src/App.svelte` · `app/src/lib/components/TopBar.svelte` ·
`app/src/lib/components/Palette.svelte` (deleted) · `app/src/lib/components/ContextMenu.svelte` ·
`app/src/lib/components/Console.svelte` · `app/src/lib/consoleUiStore.svelte.ts` ·
`app/src/lib/edges/FloatingEdge.svelte` · `app/src/lib/edges/floating.ts` ·
`app/src/lib/components/CanvasInner.svelte` · `app/src/lib/nodes/NodeActions.svelte` ·
`app/src/lib/labTypes.ts` · `contracts/lab.schema.json` · `app/src/styles/theme.css` ·
`DESIGN.md` · and the new files: `IconRail.svelte`, `RailFlyout.svelte`, `nodeCatalog.ts`,
`railUiStore.svelte.ts`, `PaneBody.svelte`, `FloatingConsoleWindow.svelte`,
`FloatingConsoleLayer.svelte`, `dragMove.ts`, `paneLabels.ts`, `edges/routing.ts`,
`ResourceBar.svelte`, `chromeStore.svelte.ts`.
