# P7 — Floating console windows, per-node MAC list with an IOL-parsing toggle

Status: **dispatch plan. Reviewed by `codex sol-medium`; findings applied (§1a). Not
implemented.** Two ideas from
`docs/learning-features-gui-ideas-plan.md` (GUI/UX rows #10 and #11), split into three
batches: **Batch 10** (floating console windows — the big one: a new drag-move primitive, new
window chrome, geometry persistence, viewport clamping, z-stacking), **Batch 11a** (the
per-node MAC list UI plus the VPCS/PC data path, which needs no parsing at all), and
**Batch 11b** (IOL MACs via live source-MAC learning, plus its own opt-in toggle).

**P6 Batch 7 has landed — Batch 11b is unblocked.** The draft of this plan (and the review of
it) were both written while Batch 7 was mid-implementation by a concurrent agent; §3.7's
"has not landed" evidence is **stale and has been replaced**. The channel is now on disk
end-to-end: `dirstat.Open(devs []EndpointDev)` (`dirstat_linux.go:45`),
`Classifier.Attribution() []EndpointAttrib` (`dirstat.go:321`) with the
singular-MAC / `ambiguous` / TTL / conflict-set lifecycle (`dirstat.go:43-49`, `:245-315`),
`openLinkDirstat` feeding `fabricLinkEndpointDevs` directly (`fabric_linux.go:594-599`),
`LinkStatsData.EpAttrib` on the wire (`verbs.go:559-610`, `docs/protocol.md:203`, `:242-252`),
and the browser side consuming it (`labStore.svelte.ts:121`, `:490-500`, `:851`, `lens.ts:8-16`,
`:75-77`, `LensPane.svelte:72-75`). `go test ./internal/dirstat/` is green.
**Batch 11b reuses that channel directly and builds no second MAC-learning mechanism**; §8.1
states the reuse points and the symbols to bind against.

**Read §4 before §6/§7/§8.** Four of the idea rows' load-bearing assumptions do not survive
contact with the current code, and one of them changes what Batch 11b's toggle actually buys.

---

## 1. Model loop / process

1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it.** One area per batch deserves
   disproportionate attention. The failure class is the one p5 and p6 both named: *code that
   builds, passes `npm run check`, renders something, and is quietly wrong.*
   - **Idea #10 — §6.3 (what has to move out of `Console.svelte`) and §6.5 (the drag
     primitive's ownership of pointer state).** The idea doc frames floating windows as "the
     same unchanged `ConsoleTerm.svelte` in a new wrapper", and that half is true. The half it
     misses is that `Console.svelte` privately owns four things a floating window also needs —
     `nativeCapture` (`Console.svelte:25`), `searchOpenFor` (`:15`), the Wireshark one-shot
     `$effect` (`:140-146`), and the whole capture-overlay card (`:455-514`) — and in floating
     mode **`Console.svelte` is not mounted at all** (`App.svelte:106,130` mount it only inside
     the two `SplitPane`s). An agent that builds `FloatingConsoleWindow.svelte` around
     `ConsoleTerm`/`CaptureTerm` directly will ship a floating mode where the Wireshark button,
     the .pcapng download and Ctrl-F silently do not exist, and it will look finished. Review
     §6.3's `PaneBody.svelte` extraction — it is the batch's only structural refactor and it is
     not optional. Then review §6.5: the codebase has **two** pointer-drag idioms and one of
     them leaks (`AnnoLine.svelte:61-63` adds `window` listeners and the component has no
     `onDestroy` at all), so "copy the annotation pattern" — which is what the idea doc says to
     do — is the wrong instruction. **Post-review, §6.5 is where a blocking finding landed:**
     the first draft's `beginDrag(e, spec): void` specified how a drag *starts* and left how it
     *continues and ends* to the implementer — "forward them to handles the module returns",
     with no handles in the signature — which is how you get a window that never moves, or
     module-global pointer state shared across every window. §6.5 now specifies the complete
     handle API and the exact markup bindings; re-review it against `SplitPane.svelte:52-81`
     and `:93-96`.
   - **Idea #11a — §7.4, where the VPCS MAC formula lives.** The MAC is
     `00:50:79:66:68:(NodeID & 0xff)` and the browser knows `NodeID`, so the cheapest possible
     implementation is four lines of TypeScript. That is the trap. The formula is documented in
     `argv.go:150-160` as a *consequence of the `-m` flag this repo passes* (`argv.go:173`); a
     second copy in `app/` has no way to notice when that flag changes, and the failure mode is
     a MAC list that is confidently, silently wrong — the exact thing the never-guess rule
     exists to prevent. Review §7.4's insistence that the derivation stays in Go, next to the
     flag, behind an exported function with a unit test.
   - **Idea #11b — §8.2, what the toggle actually gates.** The idea row says IOL MAC parsing
     "has a real always-on cost" and must be gated. §4.4 establishes that this is **not true of
     the mechanism Batch 11b reuses**: `dirstat` is already opened for every fabric link
     unconditionally (`fabric_linux.go:433`, `:580`, `:594`), its `readLoop` already reads
     `buf[6:12]` into scope (`dirstat_linux.go:117-136` + `snapLen = 128` at `:21`), and the
     learning it feeds now runs **unconditionally for the Lens**, on disk, today. The toggle
     is kept — it is the right *product* decision and the coordinator's explicit design — but
     the plan must not claim a CPU saving it cannot demonstrate, and (post-review) must not
     claim the *opposite* precision either: §4.4 no longer characterises the learning as "a
     cheap compare under an already-held lock", because `observeSource` takes its **own**
     lock after `count` has released it and does real candidate/conflict work
     (`dirstat.go:116-121` vs `:245-315`, called back-to-back at `dirstat_linux.go:133-140`).
     Review §8.2's honest framing,
     and review §8.5's rejection of the "gate the learning writes with an atomic flag"
     alternative, which would break P6's Lens the moment both features are on.
3. **`codex luna-xhigh` agent(s) implement. Recommendation: sequential, in the order
   11a → 10 → 11b.** 11a shares zero files with the other two and can merge immediately.
   10 must land after P6 Batch 9 (already on disk, §3.2) and before 11b only for reviewer
   sanity, not for correctness. 11b's P6 Batch 7 dependency is **satisfied** (§3.7, §8.1).
4. **The orchestrating session deploys to the real appliance VM and validates live** per §10.
   This plan attempts no VM steps and runs no builds.

## 1a. Review — codex sol-medium findings applied

`codex sol-medium` reviewed the draft and found **5 blocking issues + 4 minor** (all resolved in
this document before implementation). Four of the five blocking findings are Batch 10's — the
drag API, the Wireshark one-shot, `setPlacement`'s store-boundary violation, and the floating
close button — which is the correct concentration: Batch 10 is the only batch that moves state
across a component boundary. The fifth was a dependency-freshness finding that **time itself
resolved** (finding 1).

What sol-medium *confirmed* is unchanged and must **stay** unchanged, in its own words and this
plan's reasoning: the `PaneBody.svelte` extraction is genuinely necessary, because
`nativeCapture`, `searchOpenFor`, the capture overlay and the Wireshark one-shot consumer really
are private to `Console.svelte` and `Console.svelte` really is unmounted in float placement
(§4.2, §6.3); the `AnnoLine.svelte` window-listener-leak citation is accurate and choosing
`SplitPane.svelte`'s pointer-capture idiom instead is the right call (§3.4, §4.3); the VPCS MAC
formula citation (`argv.go:150`, `:173`) is accurate and a tested `node.VPCSMAC` export beside
`VPCSArgv` is the right single-source-of-truth design (§7.4); and Batch 11b's toggle correctly
gates only the **handler's use of** `Attribution()`, never dirstat's underlying learning (§4.4,
§8.2, §8.5). **None of the edits below weaken any of those**, and the last one in particular is
load-bearing: a "fix" that gates learning would break the Lens.

**One reading note for the implementing agents.** sol-medium's citations, and this plan's §3,
were written against a working tree in which P6 Batch 7 was **mid-implementation**. It has since
landed. `Console.svelte` grew from 887 to 956 lines and gained a **third pane kind** (`lens`),
so every `Console.svelte:NNN` in this document has moved by roughly +10 above `:200` and +30
below `:400`. §2's "re-grep before editing" is therefore not boilerplate; where a fix below
depends on a specific site it names the **symbol** as well as the line, and the fixed sections
carry post-Batch-7 numbers.

| # | Finding | Resolution |
|---|---|---|
| 1 | **The P6 Batch 7 dependency was assessed against a half-built tree.** The review found `EndpointDev`/`EndpointAttrib`/`Attribution` and an endpoint-indexed `Open` already present, but browser `epAttrib` plumbing absent and `dirstat_test.go` not compiling — so §3.7's "Batch 7 has NOT landed" and §8.1's "blocked on" framing were both wrong, in opposite directions, and an implementing agent following §8.1's four greps would have got a mixed verdict with no rule for what to do about it. | **Resolved by Batch 7 completing, not by a design change here.** Re-verified fresh: `dirstat.Open(devs []EndpointDev)` (`dirstat_linux.go:45`), `Attribution()` (`dirstat.go:321`) with `macTTL = 5m` (`:49`) and the bounded conflict set (`:68`, `:208-232`), `openLinkDirstat` → `fabricLinkEndpointDevs` (`fabric_linux.go:594-599`), `LinkStatsData.EpAttrib` (`verbs.go:559-610`), `docs/protocol.md:203`, `:242-252`, and the browser consumers (`labStore.svelte.ts:121`, `:490-500`, `:851`; `lens.ts:8-16`, `:75-77`; `LensPane.svelte:72-75`). `go test ./internal/dirstat/` is **green** (`go vet` also clean under `GOOS=linux`). §3.7 is rewritten from an absence-check into a **reuse map**; §8.1 now says "P6 Batch 7 has landed; reuse its dirstat MAC-learning channel directly" and its greps are a **cheap re-confirmation before starting**, not a gate that can block the batch. §5's `blocked by` cell for §8 changes from "P6 Batch 7 (not landed)" to "nothing". Two knock-ons the reviewer could not have seen: the `lens` pane kind now **exists** (`Console.svelte:566-580`, `labStore.openLensTabs`), so §6.3/§6.4 must extract and float it like any other pane, and §9's "do not build a floating window for a pane kind that does not exist yet" is retired. |
| 2 | **The drag primitive had no coherent event API.** `beginDrag(e, spec): void` returned nothing, yet §6.5 told the component to bind `onpointermove`/`onpointerup`/`onpointercancel` in markup and "forward them to handles the module returns" (plan:712, :723, :728). There were no handles. The two ways an implementer resolves that are a window that never moves, or module-global `let downX/downY/active` shared by every window — a real bug the moment two windows are dragged in one session, or one is unmounted mid-drag. | §6.5 rewritten around an explicit **handle object**: `beginDrag(e, spec): DragHandle \| null`, where `DragHandle = {move(e), end(e)}`, plus `DragSpec.onMove(x, y)` / `onEnd(x, y)` / `clamp(x, y)`. All drag state is **captured in the closure** `beginDrag` returns — nothing is module-scoped, so N windows dragging is N independent handles. The component stores `let drag: DragHandle \| null = null`, binds all three pointer events **on the same element** in markup exactly as `SplitPane.svelte:93-96` does, and `onpointerup`/`onpointercancel` both route to `end` and then null the handle. §6.5 now writes out the full component wiring (handle-side markup, the resize variant, the `null` return for a non-primary button) so nothing is left as an exercise. `window.addEventListener` remains forbidden (§9), and §6.12's gate now also greps that `dragMove.ts` declares no module-level mutable state. |
| 3 | **The Wireshark one-shot must SET the overlay, not toggle it.** Today's `$effect` (`Console.svelte:154-160`) does an **idempotent** `nativeCapture = {...nativeCapture, [linkId]: true}`. §6.3 moved that effect to `App.svelte` but had it call the store's `toggleNativeCapture(link)` — so "Capture in Wireshark…" on a link whose overlay was already up would **close** it. A silent inversion of the feature, in the one code path that has no user gesture to correct it. | §6.2 splits the setter: **`setNativeCapture(linkId, on: boolean)`** is the primitive (idempotent, what the store persists nothing about), and `toggleNativeCapture(linkId)` is a thin wrapper over it kept **only** for the user-driven controls — the tab-strip flip button (`Console.svelte:318-326`) and the overlay's own "Back to live summary" (`:552-554`), both of which are gestures on a visible state. §6.3's relocated one-shot calls **`setNativeCapture(link, true)`**, never the toggle. The same rule is written into §9 as a non-goal and into §6.12 as a gate line: **no automated, non-gesture call site may use a toggle.** `closeCapture`'s cleanup likewise becomes `setNativeCapture(linkId, false)` rather than a toggle or a raw map write (finding 5). |
| 4 | **`setPlacement(p)` could not do the work assigned to it without breaking the store boundary.** It was told to call `ensureWindow(ref, viewport)` for "every currently open pane", but `setPlacement` receives neither the pane list nor the viewport, and `consoleUiStore` **cannot** import `labStore` — the edge runs the other way (`labStore.svelte.ts:8` imports `consoleUiStore`), which is exactly why `bindConsoleSelect` (`:186-188`) and `reconcile` (`:283-353`) exist. | §6.2 redesigned so the **store stores and the caller computes**, matching P6's own `advanceCaptureDelivery` precedent (`consoleUiStore.svelte.ts:267-274`) and `reconcile`'s "caller passes the cross-store facts in" shape. `setPlacement(p)` is now **only** a persisted write of one enum — no enumeration, no viewport, no geometry. `ensureWindow(key, geom: WindowGeom)` takes an **already-resolved** geometry and does nothing but store it and append to `windowOrder`. All of the computation — enumerate `labStore.openConsoleTabs`/`openCaptureTabs`/`openLensTabs`, read the persisted map, apply the cascade, clamp against a viewport it measures itself — moves into **`FloatingConsoleLayer.svelte`**, which is the only place that has both the pane list and the viewport. Two pure helpers (`restoreGeom(labId, key)`, `cascadeGeom(index, viewport)`) are exported from the store as **functions, not methods**, so the layer can call them without the store reaching sideways. §6.12 gains the gate line: `grep -n "labStore" app/src/lib/consoleUiStore.svelte.ts` must return nothing. |
| 5 | **The floating close button could not reuse the cited close flow.** §6.4 told the window's close action to call "the existing `closeCapture` flow (`:43-54`)", but `closeCapture` is a **private function inside `Console.svelte`** (now `:44-57`) — which is not mounted in float placement. The window either fails to compile or, more likely, is "fixed" by inlining `labStore.closeCapture(linkId)` and silently dropping the focus hand-off **and** the native-overlay reset. | §6.2 promotes the three close flows onto the store as **`closePane(ref: PaneRef)`**, a single method that dispatches by kind and carries the full logic moved verbatim out of `Console.svelte:44-65`: compute `wasFocused` (matching `capture` **and** its sibling `lens` pane, as `:45-47` already does), call the right `labStore` closer through the existing callback binding rather than an import, `setNativeCapture(linkId, false)` for captures (finding 3), and re-focus the next open pane or `null`. `Console.svelte`'s three tab-strip close buttons (`:289`, `:334`, `:362`) and the floating window's close button then call **the same method**, so neither can drift. This needs one addition in the same shape as `bindConsoleSelect`: `bindPaneClose(fn)` registered from `App.svelte` beside the existing registration (`App.svelte:32-34`), preserving §6.1.9's "nothing in this batch touches `labStore.svelte.ts`" — the store still never imports it. |
| 6 (minor) | **The search-dismiss effect was omitted from the `PaneBody` extraction.** `Console.svelte:162-165` nulls `searchOpenFor` whenever focus leaves the console it belongs to. §6.3 listed the state and the props to move but not this effect, so once `searchOpenFor` lives in the store a search bar opened in one floating window would stay latent and pop back when that window is refocused. | §6.3's extraction list now **names it explicitly** and specifies where it goes: not into `PaneBody` (which would give every pane an effect that writes shared state), but into `consoleUiStore.setFocused()` itself — the one place that already knows focus changed. `setFocused(ref)` nulls `searchOpenFor` unless `ref` is the console that owns it, which is byte-equivalent to today's effect and correct for both owners. `Console.svelte`'s `$effect` at `:162-165` is **deleted**, not moved; §6.10 records the line count and §6.11.3 gains the check (open search in window A, focus B, refocus A — the bar is closed). |
| 7 (minor) | **The toggle's copy contradicted its behaviour.** §8.2 specified the title *"IOL MAC learning is off"*, but learning is unconditional and stays that way (§4.4, §8.5, and the review reconfirmed the design is right). The copy would have told the user the plan's own architecture is false. | §8.2's strings changed to describe **display**, which is what the toggle governs: off → *"Learned IOL MAC display is off — IOL addresses are inferred from live traffic"*; on → *"Learned IOL MAC display is on — IOL addresses come from observed frames"*; the empty-state row → *"turn on learned-MAC display to see this"*. The `aria-label` pair changed to match. §10.3.1's expected string is updated to the new copy so the live check does not assert the retired one. |
| 8 (minor) | **The "cheap compare under an already-held lock" claim is not supported by the code.** §4.4 asserted the learning adds a 6-byte copy "under a mutex that is already held". On disk, `count` takes and **releases** `c.mu` (`dirstat.go:116-121`), then `observeSource` takes it **again** (`:250`) and does candidate comparison, cross-endpoint conflict checks and a bounded-slice scan (`:245-315`) — two lock acquisitions per frame, not one, with real work in the second. | §4.4's cost paragraph softened to what is actually established — dirstat's sockets and its `snapLen = 128` read exist either way, the source MAC is already in the buffer, and the learning runs for the Lens regardless of this toggle — and the specific cost characterisation is **withdrawn**, exactly as P6 §1a finding 9 withdrew its overstated resize diagnosis. §8.5's rejection of an atomic gate is **unchanged in force** but re-argued on the correctness ground alone (two consumers, one of which is the Lens), with the cost sentence replaced by "unmeasured; §8.5's benchmark note stands". Nothing here licenses gating the learning. |
| 9 (minor) | **§3.6's "the mark-pruning condition looks inverted" is wrong.** `consoleUiStore.svelte.ts:322`'s `Object.keys(capturePos).length > 0 \|\| consoles.length > 0` is **P6's specified design**, stated in that plan's own words at `p6-…-plan.md:1198`: *"drop a mark once it has no remaining positions **and** no console still open"*. Marks persisting while any console is open is intended (they are written into console scrollback, `ConsoleTerm.svelte:257-271`) and bounded at 50 (`consoleUiStore.svelte.ts:262`). | §3.6 rewritten. The mark-pruning entry is no longer a "reads as unfinished" item; it is recorded as **specified behaviour** with the P6 citation, so a later reader does not "fix" it into dropping marks a user can still see. Nothing in Batch 10 was resting on the "bug" framing — the only other reference was §9's "do not fix it", which is retained but re-worded from "do not fix this oddity" to "this is not a defect; do not change it". §3.6's **other** item (the `tile2` 2×2 grid) was a real bug and **has already been fixed directly in the codebase** since the review ran (`Console.svelte:815-818` is now `repeat(2, …)` columns × one row); §3.6 records the fix rather than the defect, and §9's "do not fix the `tile2` grid" is retired as moot. |

---

## 2. Relationship to prior plans, and the baseline these line numbers are against

- **`docs/learning-features-gui-ideas-plan.md` is the source design doc.** Row #10 and its
  `## Floating console windows (idea #10, design detail)` section (`:206-253`) are carried
  forward nearly intact — §4.1 records the one place its `DockSide` proposal must change and
  why. Row #11 (`:32`) was **rewritten** to the per-node-list shape after this plan was
  dispatched; §7/§8 implement the rewritten row, not the earlier canvas-overlay framing. §4.5
  records what that change removes from scope, so a later reader does not resurrect it.
- **`docs/p6-protocol-lens-interface-suggest-console-workspace-plan.md` is load-bearing for
  both ideas and is being implemented concurrently.** Its state, as observed:
  - **Batch 9 (§8 of P6, the console pane model) is on disk in the working tree.**
    `consoleUiStore.svelte.ts` is 409 lines and carries `PaneRef`/`paneKey`/`samePane`
    (`:6-28`), `ConsoleLayout` (`:10`), `ConsoleMark` (`:15-20`), `layout`/`tiles`/`focused`/
    `pinned`/`marks`/`captureDelivered` (`:107-115`), `ensureTiled` (`:168-184`),
    `bindConsoleSelect` (`:186-188`), `setFocused` (`:190-199`), `syncFromLabStore`
    (`:202-210`), `advanceCaptureDelivery` (`:267-274`) and `reconcile` (`:283-353`).
    `Console.svelte` is **956** lines (887 before Batch 7 added the `lens` arm) with the tiled
    grid CSS (`:806-864`), and `App.svelte`
    carries the two registrations P6 §8.2a/§8.2b specified (`:32-34`, `:39-46`). **Treat this
    as ground truth and build on it.** §3.6 records what was flagged as possibly-unfinished on
    a first read and what each turned out to be.
  - **Batch 7 (§7 of P6, Protocol Lens + the dirstat MAC channel) HAS landed**, end to end:
    supervisor learning + `Attribution()`, the `epAttrib` wire field, the browser's
    `lens.ts`/`LensPane.svelte` consumers, and a **third pane kind** (`lens`) in
    `consoleUiStore.PaneRef` (`:6-9`) that `Console.svelte:566-580` renders and
    `labStore.openLensTabs` drives. §3.7 is the reuse map. **Batch 11b is unblocked** (§8.1),
    and **Batch 10 must float `lens` panes like any other pane** (§6.3, §6.4) — the earlier
    instruction to ignore that arm is retired (§1a finding 1).
  - **Batch 8 (§6 of P6, the interface picker) is irrelevant to this plan** and shares no
    files with it.
- **`docs/p5-netprobe-netsvc-impairment-plan.md` is also partially in flight.** `git status`
  shows 50 modified and 9 untracked files against HEAD `25a1e05`; the `pc` node kind exists
  (`labTypes.ts:6`, `lab.KindPC`, `app/src/lib/nodes/PcNode.svelte` untracked,
  `interfaces.ts:13`). **Every line number in this document is against the working tree, not
  HEAD.** Re-grep before editing; §9 names the p5-owned regions not to touch.
- **Standing posture, unchanged:** no Docker, no DB, no separate web server, one static Go
  supervisor binary, lightweight. **This plan adds no npm dependency** (P6 Batch 9 already
  added `@xterm/addon-search`, visible at `ConsoleTerm.svelte:5`).

---

## 3. Facts established by reading the code (do not re-derive)

### 3.1 The console dock's mount points and the two placement axes that exist today

`App.svelte` mounts `Console.svelte` **twice**, both times inside a `SplitPane`, both gated on
`showConsole`:

- `showConsole = openConsoleTabs.length > 0 || openCaptureTabs.length > 0` (`App.svelte:51-53`).
- `dockRight = consoleUiStore.dockSide === "right"` (`:54`).
- Bottom: `direction="vertical" edge="end" bind:size={consoleHeight} min=80 max=520
  storageKey="iolbox.split.consoleBottom"` (`:106-117`).
- Right: `direction="horizontal" edge="end" bind:size={consoleWidth} min=280
  max={Math.max(720, Math.floor(winW * 0.5))} storageKey="iolbox.split.consoleRight"`
  (`:130-141`).

`winW` is already tracked live (`:28`, `<svelte:window bind:innerWidth={winW} />` at `:67`).
There is **no `winH`** — Batch 10 needs one (§6.7).

`Console.svelte:241` is the root: `class:side-right={consoleUiStore.dockSide === "right"}`.
`ConsoleTerm.svelte:208-215` re-fits and re-sends NAWS when `consoleUiStore.dockSide` **or**
`consoleUiStore.layout` changes.

**Consequence, and this is the whole reason §4.1 exists:** three independent sites read
`dockSide` as a two-valued enum and each does something different with it. A third value
`"floating"` makes `dockRight` false at `App.svelte:54`, which mounts the *bottom* dock at
`:106`, which renders the whole docked console underneath the floating windows.

### 3.2 The pane model, as P6 Batch 9 actually left it on disk

`consoleUiStore.svelte.ts`:

- `DockSide = "bottom" | "right"` (`:5`) — **still two-valued**, as P6 §8.6 required.
- `PaneRef` is a discriminated union with a `"lens"` arm already declared (`:6-9`);
  `paneKey(ref)` yields `"console:3"` / `"capture:7"` / `"lens:7"` (`:22-24`); `samePane`
  compares by key (`:26-28`).
- Prefs each follow one shape: a key constant (`:34-38`), a guarded `initialX()` (`:51-98`), a
  `setX` that writes through (`:212-226`, `:355-406`). `LAYOUT_KEY = "iolbox.console.layout"`
  (`:38`) is the newest example, added by Batch 9.
- Session-only state is explicitly commented as not-persisted: *"Session-only pane refs. They
  must not be persisted across lab documents."* (`:109`).
- `reconcile(labId, consoles, captures)` (`:283-353`) hard-resets everything on a lab-id
  change (`:284-295`) and prunes per-pane state otherwise (`:297-338`), then re-establishes a
  focused pane (`:340-345`) and re-tiles (`:346-352`).
- The store **must not import `labStore`** — the edge runs the other way, which is why
  `bindConsoleSelect` (`:186-188`) exists and `App.svelte:32-34` registers the callback.

`Console.svelte` per-pane props, as passed today:

- Console panes: `<ConsoleTerm {nodeId} visible focused searchOpen onOpenSearch onCloseSearch
  marks />` (`:425-433`).
- Capture panes: `<CaptureTerm {linkId} visible focused marks />` (`:449-454`).
- `visible` = focused in `tabs` layout, tiled in a tiled layout (`isVisible`, `:80-82`).
- Tool nodes swap the terminal for `<iframe src={/tool/${nodeId}/}>` (`:417-423`, `isToolNode`
  at `:208-210`).

`ConsoleTerm.svelte` is container-agnostic exactly as P6 §8.6 demanded: props are
`{nodeId, visible, focused, searchOpen, onOpenSearch, onCloseSearch, marks}` (`:14-30`), the
`ResizeObserver` is ungated (`:184-188`), `visible` drives an rAF refit and `focused` drives
`term.focus()` in one effect (`:196-203`), and `onDestroy` disposes the terminal (`:190-194`).
**Nothing in it knows about docks, tiles, or grids.** A floating window can mount it as-is.

### 3.3 State that `Console.svelte` owns privately — the floating-mode blocker

Four things live in `Console.svelte`'s component scope and nowhere else:

**Line numbers here are post-Batch-7** (`Console.svelte` is 956 lines; see §1a's reading note).

| what | where | needed by a floating window? |
|---|---|---|
| `nativeCapture: Record<number, boolean>` — the Wireshark-overlay flip per capture tab | `:26`, flipped at `:67-69`, cleared at `:49`, read at `:320-324` and `:500` | **yes** — it gates the entire `.native-hold` card at `:500-560`, which is the only path to `labStore.downloadCapture()` (`:513`) and the `wireshark -k -i TCP@…` commands (`:177-190`) |
| `searchOpenFor: number \| null` | `:16`, toggled at `:124-136`, dismissed by the `$effect` at `:162-165`, threaded to `ConsoleTerm` at `:474-476` | **yes** — otherwise Ctrl-F does nothing in floating mode. **The dismiss effect moves too** (§1a finding 6) |
| the Wireshark one-shot `$effect` on `labStore.wiresharkOverlayFor` | `:154-160` — note it **sets** `true`, idempotently; it is not a flip | **yes** — the link-menu "Capture in Wireshark…" entry sets that signal and expects *something* mounted to consume and null it; with nothing mounted the signal never clears. **Its set-not-toggle semantics are load-bearing** (§1a finding 3) |
| the three close flows — `closeCapture` (`:44-57`), `closeConsole` (`:59-61`), `closeLens` (`:63-65`) | private component functions | **yes** — a floating window's close button cannot reach them. `closeCapture` in particular carries focus hand-off **and** the native-overlay reset, both of which an inlined `labStore.closeCapture()` silently drops (§1a finding 5) |
| `collapsed` | `:15`, `:255-262` | no — a floating window has its own close affordance; §9 forbids adding a minimize |

`markNow()` (`:138-147`) reads `consoleUiStore.captureDelivered` and calls
`consoleUiStore.addMark`, so marks already survive outside the component; only the *button*
lives in the dock bar.

### 3.4 The two pointer-drag idioms, and why they are not equivalent

**Idiom A — `window` listeners, no cleanup (`AnnoLine.svelte`, `AnnoShape.svelte`).**
`startGrip` sets local `$state` and attaches `window.addEventListener("pointermove"/"pointerup")`
(`AnnoLine.svelte:56-63`); `onGripUp` removes them and writes the doc (`:70-82`).
`AnnoShape.svelte:69-77` is the same shape for resize, and it converts screen deltas to flow
deltas by dividing by `labStore.canvasZoom` (`:80-84`) because it lives inside the zoomable
canvas. **Neither component imports `onDestroy`** — `AnnoLine.svelte:15` imports only
`NodeProps`. If the component unmounts mid-drag (a lab switch, an undo, a node delete), the two
`window` listeners survive with a captured closure over a dead component. That is latent today
because annotations are rarely unmounted mid-drag; it would not be latent for a console window
whose node can stop, close, or be reconciled away underneath the pointer.

**Idiom B — pointer capture on the handle element (`SplitPane.svelte`).**
`onPointerDown` sets `dragging` and calls `dividerEl.setPointerCapture(e.pointerId)` (`:52-56`);
`onPointerMove` computes the new size from the wrapper's `getBoundingClientRect()` and clamps to
`[min, max]` (`:57-69`); `onPointerUp` releases capture and persists to `storageKey` (`:70-81`).
Handlers are bound on the element in markup (`:93-96`) including `onpointercancel`, so an
unmount takes them with it and the browser releases the capture. Persistence restore happens
once before first paint (`:29-38`).

**Fact for §6.5:** Idiom B is the correct base for a window drag; Idiom A is what the idea doc
told us to copy (`learning-features-gui-ideas-plan.md:229-232`) and it is the weaker of the two
in exactly the dimension that matters here.

Neither idiom is a *move* primitive: SplitPane moves one edge along one axis against a parent
rect; the annotations move an SVG handle in flow coordinates. **Dragging a whole floating
window by its title bar in screen coordinates is genuinely new**, as the idea doc says
(`:212-217`).

### 3.5 z-index, as actually used

Grepped across `app/src/lib/components/*.svelte`, `nodes/*.svelte`, `edges/*.svelte`:

| band | occupants |
|---|---|
| 1–6 | node internals (`IolNode.svelte:129,164,187`, `VpcsNode.svelte:130,171,185`, `PcNode.svelte:67,78`, `NodeActions.svelte:107`), `TopBar.svelte:238` (5), canvas chrome (`CanvasInner.svelte:1160,1184,1216`), `ConsoleTerm.svelte:370` |
| 20 | `SplitPane.svelte:133` (the divider, deliberately above adjacent panes), `FloatingEdge.svelte:755` |
| 30 | in-canvas node panels — `ToolNode.svelte:250`, `PcNode.svelte:81` |
| 40–60 | edge label pop (`FloatingEdge.svelte:716`), STP reason popover (`:925` = 50), `WatcherPanel.svelte:190` and `PainterPanel.svelte:249` (60) |
| 1000–1200 | popovers and dialogs — `ChangeImagePopover.svelte:44`, `ContextMenu.svelte:110`, `AnnoStylePopover.svelte:219` (1000); `LabBrowser.svelte:200` (1100); `IconPicker.svelte:92`, `InterfacePicker.svelte:162`, `SwitchLabDialog.svelte:77` (1200) |
| 2000–3000 | `ImageManager.svelte:248` (2000), `Preflight.svelte:138` (3000) |

**There is no shared z-index scale and no stacking-context registry.** The 60–1000 band is
empty, which is where §6.6 puts floating windows.

### 3.6 Two things flagged on a first read of the Batch 9 code — and what each turned out to be

Both were raised in the draft as "looks unfinished". Neither is an open problem now, and the
record is kept so a later reader does not re-raise either one.

1. **`tile2`'s 2×2 grid was a real bug — and is ALREADY FIXED in the codebase.** The draft
   found `.term-area.layout-tile2` sharing `.layout-tile4`'s `grid-template-rows: repeat(2, …)`,
   which made two tiles half-height with an empty second row. That has since been corrected
   directly in the tree, **not by this plan**: `Console.svelte:815-818` now reads
   `grid-template-columns: repeat(2, minmax(0, 1fr)); grid-template-rows: minmax(0, 1fr)` —
   1×2, as P6 §8.1.2 specified — while `.layout-tile4` (`:819-822`) keeps the 2×2 it is named
   for. **Batch 10 must not re-fix it and must not regress it**; the tiled CSS block
   (`:806-864`) is otherwise untouched by this plan except for the pane bodies moving out
   (§6.3).
2. **`reconcile`'s mark-pruning condition is NOT a bug — it is P6's specified design.**
   `consoleUiStore.svelte.ts:322` keeps a mark when
   `Object.keys(capturePos).length > 0 || consoles.length > 0`, which is *literally* what P6
   wrote at `docs/p6-protocol-lens-interface-suggest-console-workspace-plan.md:1198`: *"drop a
   mark once it has no remaining positions **and** no console still open"*. Marks are meant to
   outlive their captures while a console is still open, because they are also written into
   console scrollback (`ConsoleTerm.svelte:257-271`) — a mark the user can still *see* must not
   be pruned out from under them — and the set is bounded at 50
   (`consoleUiStore.svelte.ts:262`). **Nothing in Batch 10 depends on this being wrong**, and it
   must not be "corrected" (§9).

### 3.7 `dirstat` today — P6 Batch 7's MAC channel, as landed, and what Batch 11b reuses

**This section replaces a draft that asserted Batch 7 had not landed.** That draft — and the
adversarial review of it — were both written while Batch 7 was mid-implementation by a
concurrent agent. It has since finished and merged. Re-verified fresh against the working tree
(§1a finding 1):

1. **The learning lifecycle exists and is tested.** `dirstat.go` carries
   `attribState` with `none`/`single`/`ambiguous` (`:43-45`), `macTTL = 5 * time.Minute`
   (`:49`), `attribCandidate{mac, state, firstSeen, lastSeen}` (`:61`), the bounded
   `conflictMAC` set (`:68`, added at `:208`, aged at `:229`), lazy expiry (`expireLocked`,
   `:223-232`), and `observeSource` (`:245-315`) implementing P6 §7.3.2's singular-MAC rule:
   first distinct MAC → `single`; a second distinct MAC → `ambiguous` with the first MAC
   **discarded**, not retained; a MAC seen on the *other* endpoint → conflict, and **neither**
   side attributes it. `go test ./internal/dirstat/` is green; `go vet` is clean under both the
   host `GOOS` and `GOOS=linux`.
2. **The endpoint-index defect P6 §7.3.1 named is fixed.**
   `func Open(devs []EndpointDev) (*Classifier, error)` (`dirstat_linux.go:45`) takes the
   **indexed** form and guards on `d.Index < 0 || d.Index > 1` (`:53-55`) — the doc index, not
   loop position. `openLinkDirstat` builds `[]dirstat.EndpointDev` straight from
   `s.fabricLinkEndpointDevs(ll, l)` (`fabric_linux.go:594-599`); the compacting
   `fabricLinkTapDevs` is no longer in that path.
3. **The attribution is on the wire and consumed.** `Classifier.Attribution()`
   (`dirstat.go:321-346`) copies out under the lock, expires lazily, sorts by endpoint index,
   and emits `MAC` **only** for a non-conflicted `single` (`:338-344`).
   `protocol.EndpointAttrib{EndpointIndex, State, MAC}` (`verbs.go:559-570`) and
   `LinkStatsData.EpAttrib` (`:605-610`, `omitempty` when the classifier could not be opened)
   carry it; `fabric_linux.go:830`, `:872` collect it per sample.
   `docs/protocol.md:203`, `:242-252` document it. The browser mirrors it at
   `labStore.svelte.ts:121`, `:490-500`, `:851`, and the Lens consumes it at `lens.ts:8-16`
   (`EndpointAttribView`, `LensAttribution.epAttrib`), `lens.ts:75-77` (`resolveSource` —
   returns `null` rather than guessing) and `LensPane.svelte:72-75` (the `ambiguous` banner).
4. **The `Attribution()` contract Batch 11b binds against**, in the real code rather than as a
   forward reference: nil-safe on a nil `*Classifier` (`dirstat.go:321-323`, mirroring
   `Snapshot()` at `:101`); one entry per endpoint that had a tap at open time — including
   endpoints with **no** observation, which report `State: "none"` rather than being absent
   (`:154`, `:178`, `:333-345`); `MAC` set **iff** `State == "single"`; `EndpointIndex` is the
   **lab document** endpoint index, which is why the slice is sparse and must never be read by
   position.

**Conclusion: Batch 11b reuses this channel directly.** §8.1 is a reuse map, not a blocker.
The only Go-side MAC handling *outside* it remains the NAT DHCP server
(`extnet/dhcp.go`, `extnet/dhcp_linux.go`, `gatewayMAC` at `:154-157`) and the browser's pcapng
dissector (`pcapng.ts` `mac()`, `dstMac`/`srcMac`) — neither is a source for this feature
(§7.3, §4.5).

### 3.8 What the supervisor deterministically knows about node MACs, per node kind

- **VPCS.** `argv.go:150-160` documents the formula as an observed property of vpcs 0.8.3's
  `pth_reader`: MAC = `00:50:79:66:68:XX` where `XX = (intra-process PC index + the "-m" value)
  & 0xff`. This repo passes `-m` = `NodeID` (`argv.go:173`) and always spawns **one** PC per
  node (`handlers.go:943`, `spec.VPCSCount = 1`), and `Spec.NodeID = n.ID`
  (`handlers.go:918-921`). So for every VPCS node in this product,
  **MAC = `00:50:79:66:68:(node.id & 0xff)`, and its single interface is `eth0`**
  (`interfaces.ts:12`). This is derivation from a flag we control, not a guess.
- **`pc` and `tool`.** These are netns nodes: a veth pair whose guest end is renamed to
  `GuestIface = "eth1"` inside netns `iolt<nodeID>` (`tool/netns.go:31-44`, `tool/tool.go:38`,
  `:71`), with the root-side half named `vtool<nodeID>` (`tool/tool.go:43`). The MAC is
  **kernel-assigned and not derivable** — but it is **readable**, exactly, with
  `ip netns exec iolt<id> cat /sys/class/net/eth1/address` (the argv builder for that already
  exists: `tool.NetnsExecArgs(nodeID, argv)`, `tool/tool.go:77`). Their single interface is
  `eth1` (`interfaces.ts:13`). **The root-side `vtool<id>` MAC is the *other* end of the pair
  and must never be reported as the node's** — that is the one easy mistake here.
- **NAT.** The tap is `iolnat<id>` (`fabric_linux.go:897`) in the root namespace; the DHCP
  server synthesises its own source MAC from the gateway IP (`extnet/dhcp_linux.go:154-157`)
  which is **not** the tap's link-layer address. Nothing in the tree records the tap's real MAC.
- **IOL.** Nothing. `painter/stp.go`'s doc comment mentions the `aabb.cc00.*` convention in
  prose only; P6 §4.2 already considered and **rejected** deriving IOL MACs from it. IOL MACs
  can only come from observing traffic.

### 3.9 `NodeActions.svelte` — the hover toolbar idea #11's button joins

101 lines of markup + CSS. Props are `{nodeId, state}` (`:15`); it is rendered by **all four**
node components — `IolNode.svelte:62`, `VpcsNode.svelte:66`, `PcNode.svelte:38`,
`ToolNode.svelte:61`. Buttons are state-driven: Start when `!isBusy` (`:54-57`), Stop when
`isBusy` (`:58-61`), **Console when `isRunning`** (`:62-65`), Save config when
`isRunning && isIol` (`:66-74`, `isIol` derived at `:24-26`), Wipe when `!isBusy` (`:75-78`).
An in-flight `labStore.nodeLocks[nodeId]` replaces the whole row with a spinner (`:48-52`,
`:21-22`).

Every button follows one shape and it must be matched exactly:

```svelte
<button class="na-btn" title="…" aria-label="…"
  onpointerdown={(e) => e.stopPropagation()} onclick={handler}
>{@html uiSvg("console", 12)}</button>
```

The `onpointerdown` stopPropagation is load-bearing — without it, pressing the button starts a
node drag (`:7-10` explains). `.na-btn` is 22×22 with a 12px glyph (`:131-153`).

### 3.10 The popover precedent — `ChangeImagePopover.svelte`

The smallest complete example of "a panel anchored at a click point, over the canvas":
props `{x, y, nodeId, onClose}` (`:4-5`); `bind:this={el}` plus a `window` mousedown handler
that closes on any click outside (`:9-11`) and Escape (`:12-14`), both registered via
`<svelte:window onmousedown={…} onkeydown={…} />` (`:25`); `position: fixed` with
`style:left`/`style:top` from the props (`:26`, `:43-46`) at `z-index: 1000`. Its caller
(`CanvasInner.svelte`) supplies client coordinates. `IconPicker.svelte:92` and
`AnnoStylePopover.svelte:219` are the same shape.

### 3.11 `TopBar.svelte` — the toggle idiom idea #11b's switch must match

The fullscreen control (`:210-218`) is the canonical single-state toggle button:

```svelte
<button
  class="btn"
  aria-pressed={isFullscreen}
  title={isFullscreen ? "Exit fullscreen (Esc)" : "Enter fullscreen"}
  aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
  onclick={toggleFullscreen}
>
  {@html uiSvg(isFullscreen ? "fullscreenExit" : "fullscreen", 13)}
</button>
```

Four properties to copy: `class="btn"` (not `.seg`, which is the two-option segmented control
used for Theme at `:152-163`); `aria-pressed` bound to the state; `title` **and** `aria-label`
both flipping with the state; a 13px `uiSvg` glyph. The "on" appearance is
`.btn.on { color: var(--accent); border-color: var(--accent); background: var(--accent-muted) }`
(`:354-358`) — used by the Tasks button (`:173-181`), which is the closest analogue since it
toggles a *persistent view state* rather than a browser API. Fullscreen deliberately does
**not** use `.on` because `aria-pressed` plus the swapped glyph already carry it.

State: fullscreen keeps a local `$state` synced from the real DOM via
`<svelte:window onfullscreenchange={syncFullscreen} />` (`:25-28`, `:115`) precisely because the
browser owns the truth. **A preference toggle has no such external owner and must not copy that
part** — it belongs in a store, following `consoleUiStore`'s key/initial/setter shape (§3.2).

`uiSvg(name, size)` falls back to the `net` glyph for an unknown name
(`icons.svelte.ts:206-209`), so a missing glyph degrades to a wrong-but-rendered icon rather
than a crash — which means **a typo'd icon name will not fail the build**. Batch 11b adds one
glyph to `UI_GLYPHS` (`icons.svelte.ts:106-140`).

### 3.12 Link-end labels on the canvas — read for §4.5 only

`FloatingEdge.svelte` renders one `EdgeLabel` per endpoint (`:465-491`, `:493-518`) positioned
on the edge's own quadratic at `t = 0.22 / 0.78` (`:299-315`), containing a `.port-chip` that
shows `iface` at rest and reveals `name` on hover (`:475`, `.chip-detail` hidden at `:743-746`).
A **second** badge can stack beneath the chip inside the same `EdgeLabel` — that is exactly what
the STP role/state badge does (`:477-489`, `.stp-badge` at `:881-905`), fed by
`painterStore.stpBadgeFor(nodeId, iface)` (`painterStore.svelte.ts:260-279`), which resolves a
`(nodeId, iface)` pair against a snapshot and **returns `null` rather than inventing a badge**.

So a canvas MAC label would have had a clean precedent to extend. It is recorded here because
§4.5 explains why this plan does not do that, so the option is visibly considered rather than
missed.

---

## 4. Where the idea doc's design does not survive the code — five corrections

### 4.1 (#10) `DockSide` must not gain a `"floating"` value — and this is not merely P6's rule

The idea doc locks *"a third mode, `DockSide: "bottom" | "right" | "floating"` … toggled from
the same control that currently switches bottom↔right"*
(`learning-features-gui-ideas-plan.md:219-221`). P6 §3.11/§8.6 forbade Batch 9 from adding a
third value, on the grounds that layout and placement are orthogonal axes and idea #10 would
add `"floating"` "later without touching `ConsoleLayout`". Read literally, that sentence
appears to *endorse* the third value — it does not, and the code says why.

`dockSide` is consumed by three sites that each treat it as a **binary**:

1. `App.svelte:54` — `dockRight = dockSide === "right"`, and `:106` mounts the bottom
   `SplitPane` when `showConsole && !dockRight`. A `"floating"` value is not `"right"`,
   therefore it mounts the bottom dock. The docked console would render **underneath** the
   floating windows, with every pane mounted twice.
2. `Console.svelte:241` — `class:side-right={dockSide === "right"}`, which only swaps a border
   (`:530-535`). Harmless, but it is a second binary read.
3. `ConsoleTerm.svelte:208-215` — an `$effect` that refits and re-sends NAWS on any `dockSide`
   change. Correct for a dock flip; meaningless for a mode where the terminal's box is owned by
   a window's `{w,h}`.
4. `consoleUiStore.svelte.ts:391-393` — `toggleDockSide()` is a strict two-way flip. A third
   value makes the existing dock-side button unreachable from `"floating"` without rewriting it.

**Correction (decided):** placement gets its **own axis**.

```ts
export type ConsolePlacement = "dock" | "float";
```

persisted at `iolbox.console.placement`, default `"dock"`. `DockSide` stays
`"bottom" | "right"` and keeps its meaning *within* `"dock"`; `ConsoleLayout` stays as-is and is
ignored while floating (§6.8). This satisfies P6 §8.6's grep-checkable constraint literally, and
it means flipping float→dock restores the user's exact previous dock side and tile set without
any migration. The idea doc's *user-facing* framing is preserved — one control in the dock-bar
action row cycles/toggles placement — only the type changes.

### 4.2 (#10) The floating window cannot wrap `ConsoleTerm` directly — it must wrap the pane body

The idea doc says the new window contains *"the same `ConsoleTerm.svelte` xterm content the
dock already renders unchanged"* (`:224-226`). True for a **console** pane and false for
everything else. §3.3 enumerates what else a pane carries: the tool-node iframe branch
(`Console.svelte:462-469`), the capture pane's native-Wireshark overlay card
(`:500-560`) with the .pcapng download (`:508-519`) and the two copyable commands
(`:530-548`), the per-console search plumbing (`:474-476`), the **`lens` pane arm** Batch 7
added (`:566-580`, `LensPane` with `{linkId, visible, focused, title}`), and the
`nativeCapture` / `searchOpenFor` state those read — plus the three private close flows
(`:44-65`).

**Correction (decided):** extract `PaneBody.svelte` — a component that takes a `PaneRef` and
renders exactly what is inside `.pane-frame` today for **all three** pane kinds
(`Console.svelte:461-481` consoles, `:491-562` captures, `:571-578` lens), including the
overlay. `Console.svelte` renders it inside its slots;
`FloatingConsoleWindow.svelte` renders it inside its chrome. The state it needs —
`nativeCapture`, `searchOpenFor`, and (per §1a finding 5) the close flows — moves into
`consoleUiStore` (§6.2, §6.3), which already
owns every sibling concern (`marks`, `focused`, `captureDelivered`) and already survives
`Console.svelte` unmounting, which is the exact condition floating mode creates.

This is Batch 10's **one** structural refactor. It is the same class of decision as P6 §7.2
(capture-stream ownership moving to `labStore`): not optional, and the reason the batch is
"medium" rather than "light".

### 4.3 (#10) "Copy the annotation drag pattern" is the wrong instruction

The idea doc: *"Same `pointermove`/`pointerup` pattern already used for annotation dragging in
`AnnoLine.svelte`/`AnnoShape.svelte`"* (`:229-232`).

§3.4 establishes that pattern attaches `window` listeners with no `onDestroy` in either
component. For an annotation that is latent. For a console window it is not: a floating window's
pane can vanish mid-drag through at least four paths that already exist —
`labStore.closeConsole` / `closeAllConsoles`, a node crash, `reconcile`'s prune
(`consoleUiStore.svelte.ts:299-302`), and a lab switch (`:284-295`).

**Correction (decided):** the new primitive uses `SplitPane`'s pointer-capture idiom
(`SplitPane.svelte:52-56`, `:70-81`) — handlers bound in markup on the title bar element,
`setPointerCapture` on pointerdown, `onpointercancel` wired alongside `onpointerup`
(`SplitPane.svelte:93-96`). Unmount then takes the handlers with it. §6.5 specifies it.

### 4.4 (#11b) The toggle cannot save the CPU the idea row implies — keep it anyway, for a different reason

Row #11 says IOL MAC learning *"has a real always-on cost"* and is therefore *"gated behind its
own separate toggle"*. Against the mechanism Batch 11b reuses, the cost claim does not hold:

- `dirstat` is opened for **every** fabric link already, unconditionally, at attach and re-attach
  (`fabric_linux.go:433`, `:580`, `:594-599`). Nothing about MAC display changes whether those
  raw sockets exist.
- Its `readLoop` already reads up to `snapLen = 128` bytes of every frame
  (`dirstat_linux.go:21`, `:117-140`) and already calls `relay.ClassifyDetailed` on them. The
  source MAC at `buf[6:12]` is **already in the buffer** — no snaplen change, no extra read.
- **P6 Batch 7 has landed, so that learning already runs, for the Protocol Lens, on every
  fabric link, today** — with this toggle off, with the MAC popover never opened, with the
  feature not built. A toggle that claimed to stop it would be lying, and one that actually
  stopped it would break the Lens (§8.5).

**What this section does NOT claim** (§1a finding 8): the draft characterised the learning as
"a 6-byte compare under a mutex that is already held". That is not what is on disk. `count`
takes and releases `c.mu` (`dirstat.go:116-121`); `observeSource` then takes it **again**
(`:250`) and does candidate comparison, a cross-endpoint conflict check and a bounded-slice
scan (`:245-315`) — two acquisitions per frame with real work in the second
(`dirstat_linux.go:133-140` calls them back to back). The per-frame cost is **unmeasured** and
this plan asserts nothing about its magnitude in either direction. The argument for keeping the
toggle stands on the product ground below, and the argument against gating the learning stands
on §8.5's correctness ground — neither needs a cost number.

**Correction (decided):** the toggle stays, and it is scoped honestly.

> **"Show learned IOL MACs" is an opt-in for *displaying inferred data*, not a performance
> switch.** Default **off**. When off, the GUI omits `learned` from its `node.macs` request and
> the supervisor never reads the attribution table for it; IOL rows render the explicit
> "turn on learned-MAC display to see this" state rather than a blank or an "unknown". When on, IOL
> rows populate from P6's never-guess channel and carry a visible `learned` provenance badge
> distinct from `derived`/`read`.

The product reason is the better one anyway: a derived VPCS MAC and a learned IOL MAC are
**epistemically different objects**, one is a fact about a flag this repo passes and the other
is an inference from observed frames that can read `ambiguous` or age out (P6 §7.3.3). Making a
learner opt in to the second is the same discipline as the never-guess rule itself. The UI copy
must say this; it must not say "reduces CPU", and it must not say **learning** is off when only
its **display** is (§1a finding 7). §8.2 carries the exact strings.

### 4.5 (#11) The per-node list means there is **no** canvas-rendering work — and no `TopBar` label switch

Row #11 was rewritten after this plan was dispatched. The earlier framing — a `TopBar` switch
that labels **every link end on the canvas** with its MAC — is **withdrawn**, and the withdrawal
removes real work rather than merely renaming it:

- No change to `FloatingEdge.svelte`. §3.12 established that a second badge under the port chip
  was feasible (the STP badge does exactly that, `:477-489`); it is simply not what is being
  built. **Batch 11a/11b must show zero diff in `app/src/lib/edges/`.**
- No canvas-wide label density problem, no interaction with the parallel-edge chip-separation
  math (`:299-315`), no new `EdgeLabel` z-index band (`:715-717`).
- The `TopBar` toggle survives, but it now governs **IOL learning disclosure** (§4.4), not label
  rendering. The file name `p7-floating-console-mac-toggle-plan.md` refers to that toggle.
- The display surface is the node hover toolbar (`NodeActions.svelte`, §3.9) plus a popover
  (§3.10) — one node's interfaces at a time, which is also what makes "brief format" meaningful.

One further correction inside the rewritten row: it cites `argv.go:150-173` as covering
"PC/VPCS". §3.8 shows that citation is **VPCS-only**. `pc` is p5's separate netns node kind
(`labTypes.ts:6`, `interfaces.ts:13`) whose MAC is kernel-assigned. The row's intent — "always
populated, no parsing" — still holds for `pc`, but through a **different mechanism**: an exact
read of `/sys/class/net/eth1/address` inside the node's netns (§7.5), which is a fact, not a
derivation and not an inference. Two sources, both non-guessing, and the popover labels which
one each row came from.

Finally, the row's example format `Gi0/0: aabb.cc00.0100` uses IOS-style interface naming that
**this product does not use** — `interfaces.ts:9-24` emits `e0/0`/`s0/0`/`eth0`/`eth1`, and P6
§11 puts interface aliasing explicitly out of scope. Render the lab document's spelling. Keep
the Cisco dotted-triplet MAC formatting, which is the part of that example that carries the
"brief" intent (§7.6).

---

## 5. Batch ordering and independence

| batch | idea | files touched | shares files with | blocked by |
|---|---|---|---|---|
| **§7** | #11a per-node MAC list + VPCS/PC data | `NodeActions.svelte`, new `MacListPopover.svelte`, new `macs.ts` (app), `labStore.svelte.ts`, `protocol.ts`, `mockTransport.ts`, `supervisor/internal/node/argv.go` (+ test), `supervisor/internal/server/handlers.go`, `server.go`, `protocol/verbs.go`, `docs/protocol.md` | §8 (the `node.macs` verb + handler + popover) | nothing |
| **§6** | #10 floating console windows | `Console.svelte`, new `PaneBody.svelte`, new `FloatingConsoleWindow.svelte`, new `FloatingConsoleLayer.svelte`, new `dragMove.ts`, `consoleUiStore.svelte.ts`, `App.svelte` | nothing in §7/§8 | P6 Batch 9 (**already on disk**, §3.2) |
| **§8** | #11b IOL learned MACs + opt-in toggle | `TopBar.svelte`, `icons.svelte.ts`, new `macUiStore.svelte.ts`, `MacListPopover.svelte`, `macs.ts`, `handlers.go`, `protocol/verbs.go`, `docs/protocol.md` | §7 | nothing — **P6 Batch 7 has landed** (§3.7) |

Merge order **11a → 10 → 11b**. §7 and §6 are fully independent of each other and could run in
parallel by two agents; they are sequenced only because 11a is small and merging it first
shrinks 11b's diff. §8's former blocker is gone: `EndpointAttrib` and `Attribution()` exist and
are tested (§3.7). §8.1's greps remain as a **30-second re-confirmation** that the symbols are
still where this plan says, not as a gate.

---

## 6. Batch 10 — floating console windows (idea #10)

### 6.1 Decisions locked

1. **Placement is a new axis, not a third `DockSide`** (§4.1).
   `ConsolePlacement = "dock" | "float"`, persisted at `iolbox.console.placement`, default
   `"dock"`. `DockSide` and `ConsoleLayout` keep their current types and values, untouched.
2. **Global mode first.** All consoles docked, or all floating. Per-pane mixing ("this one
   floats, that one stays docked") is explicitly a follow-on
   (`learning-features-gui-ideas-plan.md:250-253`, §11).
3. **One window per `PaneRef`.** Window identity, geometry-map key, z-order key and `{#each}`
   key are all `paneKey(ref)` (`consoleUiStore.svelte.ts:22-24`) — the identity P6 §8.2 built
   for exactly this. **No tile or window logic may key off an array index.**
4. **`ConsoleTerm.svelte`, `CaptureTerm.svelte` and `LensPane.svelte` are not modified.** P6
   §8.6 is non-negotiable and already satisfied by all three: a floating window passes
   `visible: true` and `focused = (this is the topmost window)`. Do **not** add an `inWindow`,
   `floating` or `zIndex` prop to any of them.
5. **`PaneBody.svelte` is extracted and used by both owners** (§4.2), covering **all three**
   pane kinds. It is the batch's only refactor and its diff in `Console.svelte` must be a
   *move*, not a rewrite.
10. **The store never reaches sideways.** `consoleUiStore` gains no import of `labStore`, no
    `window` read and no pane-list read; viewport, lab id and pane facts are parameters or
    bound callbacks (§6.2). This is the constraint that shapes `setPlacement`, `ensureWindow`,
    `commitWindow`, `clampAllWindows` and `closePane`.
11. **Automated call sites set; user gestures toggle.** The Wireshark one-shot uses
    `setNativeCapture(link, true)`; only the two visible flip controls use
    `toggleNativeCapture` (§6.2, §6.3).
6. **Drag and resize use pointer capture on the grabbed element** (§4.3), through one new
   module, not two ad-hoc copies.
7. **Geometry persists; membership does not.** `{x, y, w, h}` per `(labId, paneKey)` is written
   to localStorage; which panes are open remains `labStore`'s business and which are tiled
   remains session-only, exactly as P6 §8.2 requires (§6.9 justifies the distinction).
8. **A window's title bar can never leave the viewport** (§6.7). This is an invariant enforced
   at three sites, not a nicety.
9. **Nothing in this batch touches `labStore.svelte.ts`.** P6 Batch 9 established that the
   console UI can own its lifecycle through `reconcile` + the `App.svelte` effect
   (`App.svelte:39-46`); floating windows are one more consumer of that same reconciled state.

### 6.2 Store additions — `consoleUiStore.svelte.ts`

Following the file's established shape exactly (key constant → guarded `initialX()` → `setX`
that writes through, `:34-38` / `:51-98` / `:212-226`).

**The governing boundary rule, and the reason §6.2 looks the way it does** (§1a finding 4):
`consoleUiStore` **must never import `labStore`** — the edge already runs the other way
(`labStore.svelte.ts:8`), and reversing it creates a singleton import cycle. Two mechanisms in
the file already exist precisely because of this, and every addition below follows one of them:

- **`bindConsoleSelect(fn)`** (`:186-188`) — the store calls **out** through a callback that
  `App.svelte:32-34` registers. Used for anything the store must *do* to `labStore`.
- **`reconcile(labId, consoles, captures)`** (`:283-353`) and `advanceCaptureDelivery`
  (`:267-274`) — the **caller** reads the cross-store facts and hands the store finished
  values. Used for anything the store must *know* about `labStore` or the DOM.

So: **the store stores; the caller computes.** No method below reads a pane list, a viewport,
or a `labStore` field.

```ts
export type ConsolePlacement = "dock" | "float";
export interface WindowGeom { x: number; y: number; w: number; h: number; }
export interface Viewport { w: number; h: number; topbarH: number; }

const PLACEMENT_KEY = "iolbox.console.placement";
const GEOM_KEY = "iolbox.console.windows";   // Record<`${labId}|${paneKey}`, WindowGeom>

export const WIN_MIN_W = 320;
export const WIN_MIN_H = 160;
export const WIN_DEFAULT_W = 560;
export const WIN_DEFAULT_H = 320;
export const WIN_TITLE_H = 28;
/** Title-bar pixels that must remain inside the viewport on every axis (§6.7). */
export const WIN_KEEP_VISIBLE = 120;
```

**Three pure module-level functions** (exported, *not* methods — they touch no reactive state,
so the layer can call them freely and they are trivially reasoned about):

```ts
/** Clamp one geometry against a viewport. The §6.7 invariant, in one place. */
export function clampGeom(g: WindowGeom, vp: Viewport): WindowGeom;
/** The persisted geometry for a pane in a lab, already clamped — or null. */
export function restoreGeom(labId: string, key: string, vp: Viewport): WindowGeom | null;
/** A fresh cascaded geometry for the Nth window opened, already clamped. */
export function cascadeGeom(index: number, vp: Viewport): WindowGeom;
```

New reactive fields:

- `placement = $state<ConsolePlacement>("dock")`, initialised in the constructor beside the
  other five (`:127-133`).
- `windows = $state<Record<string, WindowGeom>>({})` — **session-live geometry, keyed by
  `paneKey`**. The store never invents an entry; the layer supplies it (§6.4).
- `windowOrder = $state<string[]>([])` — paneKeys, back-to-front. Z-index is derived from
  position in this array (§6.6), so there is no counter to overflow.
- `nativeCapture = $state<Record<number, boolean>>({})` — **moved from `Console.svelte:26`**.
- `searchOpenFor = $state<number | null>(null)` — **moved from `Console.svelte:16`**.

New methods — note what each one deliberately does **not** do:

- **`setPlacement(p: ConsolePlacement)`** — writes `this.placement` and persists to
  `PLACEMENT_KEY`. **That is all it does.** It does not enumerate panes, does not take a
  viewport, does not call `ensureWindow`. Geometry for newly-floating panes is established by
  `FloatingConsoleLayer.svelte`'s `$effect`, which runs on mount and on every pane-list change
  and therefore covers the dock→float flip for free (§6.4) — the caller that actually has both
  the pane list and the viewport. `tiles`/`pinned`/`layout` are **not** cleared, so float→dock
  restores the prior arrangement (§6.8).
- **`togglePlacement()`** — flips the enum through `setPlacement`. A user gesture only.
- **`ensureWindow(key: string, geom: WindowGeom)`** — if `windows[key]` is absent, set it to
  the **already-resolved, already-clamped** `geom` the caller computed; append `key` to
  `windowOrder` if missing. Idempotent, and it neither restores from localStorage nor cascades
  nor clamps — all three are the layer's job via the pure functions above.
- **`moveWindow(key, x, y)` / `resizeWindow(key, w, h)`** — write geometry during a drag. They
  do **not** persist (the drag is high-frequency) and they do **not** clamp; `dragMove`'s
  `clamp` hook has already applied `clampGeom` with the layer's live viewport (§6.5, §6.7).
- **`commitWindow(key)`** — called on pointerup only; persists the geometry map (§6.9). This
  is the only write to `GEOM_KEY` — and it needs the lab id, which the store does not have, so
  it takes it: **`commitWindow(labId: string, key: string)`**.
- **`clampAllWindows(vp: Viewport)`** — maps every entry through the pure `clampGeom`. Called
  by the layer on viewport resize (§6.7 site 3). The viewport is a **parameter**, never read
  from `window` inside the store.
- **`raiseWindow(key)`** — moves `key` to the end of `windowOrder`. Idempotent when already
  last, so a click on the focused window does not churn the array.
- **`setNativeCapture(linkId: number, on: boolean)`** — the **primitive**, idempotent. Every
  automated / one-shot call site uses this (§1a finding 3).
- **`toggleNativeCapture(linkId)`** — a one-line wrapper over `setNativeCapture`, kept **only**
  for the two user gestures that flip a state the user can see: the tab-strip flip button
  (`Console.svelte:318-326`) and the overlay's "Back to live summary" (`:552-554`).
  **No automated call site may use it** (§6.12, §9).
- **`setSearchOpenFor(nodeId: number | null)`**, `toggleSearchOpenFor(nodeId)` — the moved
  search state's setters, mirroring `Console.svelte:124-136`.
- **`closePane(ref: PaneRef)`** — the shared close flow (§1a finding 5), moved verbatim out of
  `Console.svelte:44-65` and dispatching by kind:
  - compute `wasFocused` first — for a `capture` ref this must match **both**
    `{kind:"capture",link}` and `{kind:"lens",link}`, exactly as `:45-47` does today;
  - call the right closer through **`bindPaneClose`** (below), never an import;
  - for `capture`: `this.setNativeCapture(linkId, false)` — not a toggle, not a raw map write;
  - if `wasFocused`, hand focus to the next open pane via the pane lists the callback owner
    passes back, else `setFocused(null)` — byte-identical to `:50-56`.
- **`bindPaneClose(fn: (ref: PaneRef) => { nextCapture?: number; nextConsole?: number })`** —
  registered once from `App.svelte`, beside the existing `bindConsoleSelect` (`App.svelte:32-34`).
  The callback is the **only** thing that touches `labStore` (`closeConsole` / `closeCapture` /
  `closeLens`, then reports the remaining open tabs). This is the `bindConsoleSelect` idiom
  applied to a second concern, and it is why §6.1.9's "nothing in this batch touches
  `labStore.svelte.ts`" survives intact.
- **`setFocused(ref)` gains one line** (§1a finding 6): after writing `focused`, null
  `searchOpenFor` unless `ref` is `{kind:"console", node: searchOpenFor}`. This is
  byte-equivalent to the `$effect` at `Console.svelte:162-165`, which is **deleted** rather than
  moved — putting it in `setFocused` means it fires for a floating window's click-to-focus and
  the dock's tab click through one path, instead of only wherever a component happened to be
  mounted.

`reconcile()` (`:283-353`) gains two lines in each half:

- **Hard reset** (`:284-295`): also clear `windows`, `windowOrder`, `nativeCapture`,
  `searchOpenFor`. The persisted geometry map is **not** cleared — it is keyed by lab id and is
  the feature (§6.9).
- **Prune** (`:297-338`): drop `windows`/`windowOrder` entries whose pane is no longer open
  (reuse the existing `isOpen` helper at `:276-280`), drop `nativeCapture` keys for closed
  captures, and null `searchOpenFor` if its node is gone.

Note that `reconcile` already receives `captures` and (post-Batch-7) must also account for
`lens` panes when pruning `windowOrder`; `isOpen` (`:276-280`) is the existing helper and
already handles the `lens` arm — **use it, do not re-derive the membership test.**

### 6.3 `PaneBody.svelte` — the extraction

New file `app/src/lib/components/PaneBody.svelte`. Props: `{ ref: PaneRef, visible: boolean,
focused: boolean }`. Its body is **moved verbatim** from `Console.svelte` — **all three** pane
kinds, because Batch 7's `lens` arm now exists (§1a finding 1):

- console arm ← `:461-481` (the `isToolNode` iframe branch at `:462-469` and the `ConsoleTerm`
  at `:470-478`), with `searchOpen` / `onOpenSearch` / `onCloseSearch` now reading and writing
  `consoleUiStore.searchOpenFor` / `setSearchOpenFor` instead of the component-local `let`;
- capture arm ← `:491-562` (the `CaptureTerm` at `:493-498` and the entire `.native-hold`
  overlay at `:500-560`), with `nativeCapture[linkId]` now
  `consoleUiStore.nativeCapture[linkId]` and the "Back to live summary" button (`:552-554`)
  calling `consoleUiStore.toggleNativeCapture(linkId)` — a **user gesture**, so the toggle is
  correct here and only here (§1a finding 3);
- **lens arm ← `:571-578`** (`<LensPane {linkId} visible focused title={captureTitle(linkId)} />`).
  `LensPane.svelte` is container-agnostic in exactly the way `ConsoleTerm` is and needs no
  change; a floating lens window passes `visible={true}` like any other.
- the helpers those arms call move with them: `captureTitle` (`:29-36`), `isToolNode`
  (`:222-224`), `nodeName` (`:218-220`), `captureAddr` (`:167-172`), `wiresharkCmd` /
  `wiresharkCmdFull` (`:177-190`), `fmtBytes` (`:192-195`), `copyText`, the
  `SHARK` glyph (`:210-211`) and every CSS rule under `.native-*` / `.tool-frame` / `.addr-chip`.
- The `.pane-frame` wrapper (`:461`, CSS at `:837-845`) moves **into** `PaneBody` so both owners
  get identical containment; the dock's `.term-slot` keeps its own positioning rules
  (`:806-809`, `:831-836`, `:846-848`, `:856-859`) unchanged, as does the whole
  `.term-area.layout-*` block (`:810-830`) — **including the already-fixed `tile2` rule**
  (§3.6, §1a finding 9).

`Console.svelte` keeps: `collapsed`, the tab strip, the dock-actions row, the layout/pin/
search/mark controls, and the `.term-area` grid minus the moved bodies. Its three tab-strip
close buttons (`:289`, `:334`, `:362`) now call `consoleUiStore.closePane(ref)` (§6.2), and the
private `closeCapture`/`closeConsole`/`closeLens` (`:44-65`) are **deleted**, not kept as
wrappers — two implementations of a close flow is exactly the drift §1a finding 5 is about.

**Two effects cannot simply stay put.**

1. **The Wireshark one-shot (`:154-160`).** `App.svelte` mounts `Console.svelte` only in dock
   placement, so in float placement nothing would consume `labStore.wiresharkOverlayFor`: the
   link menu's "Capture in Wireshark…" would open a capture tab whose overlay never appears,
   while the signal stays set forever and fires spuriously on the next dock. **Move it to
   `App.svelte`**, alongside the existing `reconcile` effect (`:39-46`), where it runs in both
   placements. It must do exactly what it does today:

   ```ts
   $effect(() => {
     const linkId = labStore.wiresharkOverlayFor;
     if (linkId === null) return;
     consoleUiStore.setFocused({ kind: "capture", link: linkId });
     consoleUiStore.setNativeCapture(linkId, true);   // SET, never toggle — §1a finding 3
     labStore.wiresharkOverlayFor = null;
   });
   ```

   **`setNativeCapture(linkId, true)`, not `toggleNativeCapture(linkId)`.** Today's line
   (`Console.svelte:158`) is an idempotent assignment; a toggle here means that invoking
   "Capture in Wireshark…" on a link whose overlay is *already* showing **closes** it — the
   feature inverted, in a code path with no user gesture to notice or correct it. This is a
   one-shot reaction to a signal, not a control. `App.svelte`'s diff stays within the narrow
   allowance P6 §8.9 already carved out.

2. **The search-dismiss effect (`:162-165`)** — `if (focused?.kind !== "console" ||
   searchOpenFor !== focused.node) searchOpenFor = null`. Once `searchOpenFor` is shared store
   state, leaving this in `Console.svelte` means it stops running in float placement, and a
   search bar opened in window A stays latent and reappears when A is refocused (§1a
   finding 6). It does **not** belong in `PaneBody` either — every pane would then own an
   effect that writes shared state, and N panes racing to null one field is the kind of thing
   that works until it doesn't. **Delete it and fold the same condition into
   `consoleUiStore.setFocused()`** (§6.2), which is the single place that already knows focus
   changed and is called by the dock's tab click, the floating window's click-to-front,
   `syncFromLabStore` and `closePane` alike.

### 6.4 `FloatingConsoleWindow.svelte` + `FloatingConsoleLayer.svelte`

**`FloatingConsoleLayer.svelte`** — mounted in `App.svelte` as a sibling of `.shell`, next to
`<SwitchLabDialog />` (`:157`), gated on `showConsole && consoleUiStore.placement === "float"`.
It:

**The layer is where all cross-store computation lives** (§1a finding 4). It is the only piece
of this batch that can see both the pane list (`labStore`) and the viewport (the DOM), which is
exactly why `setPlacement` and `ensureWindow` were stripped of that work in §6.2. It:

- tracks the viewport (`<svelte:window bind:innerWidth bind:innerHeight />`), derives
  `vp: Viewport = { w, h, topbarH }`, and calls `consoleUiStore.clampAllWindows(vp)` on change
  (§6.7 site 3);
- iterates `consoleUiStore.windowOrder` (**not** the tab lists — order *is* the z-order) and
  renders one `FloatingConsoleWindow` per key, resolving the key back to a `PaneRef`;
- **owns the geometry pipeline.** One `$effect` reads `labStore.openConsoleTabs`,
  `openCaptureTabs` and `openLensTabs`, and for every open pane whose `paneKey` is missing from
  `consoleUiStore.windows`, computes a **finished** geometry —
  `restoreGeom(labId, key, vp) ?? cascadeGeom(nextIndex, vp)`, both already clamped (§6.2) — and
  calls `consoleUiStore.ensureWindow(key, geom)` with it. The store receives a resolved
  `WindowGeom` and does nothing but store it. This one effect covers **three** cases with no
  extra code: the dock→float flip (every open pane is missing on the first run), a console
  opened from the canvas while floating, and a lab switch (after `reconcile`'s hard reset
  empties `windows`). It is the same "caller owns cross-store computation, store owns state"
  shape as `advanceCaptureDelivery` (`consoleUiStore.svelte.ts:267-274`) and `reconcile`;
- renders a small persistent "return to dock" affordance so a user who floats everything and
  then closes every window is not stranded with no way back (the dock-bar control that flipped
  placement is unmounted in float mode — this is the escape hatch, and it is required, not
  polish).

Because the layer supplies geometry for **whatever panes are open**, `lens` panes float exactly
like consoles and captures with no special case (§1a finding 1). `paneKey` (`:22-24`) already
yields `"lens:7"`.

**`FloatingConsoleWindow.svelte`** — props `{ ref: PaneRef, z: number }`. Structure:

- root `<div class="float-win" style:left style:top style:width style:height style:z-index>`,
  `position: fixed`;
- `onpointerdown` on the **root** (capture phase not needed) calls
  `consoleUiStore.raiseWindow(key)` + `consoleUiStore.setFocused(ref)` — click-to-front and
  focus in one gesture, reusing the store's existing bidirectional sync (`:190-199`) so the
  dock's address chip and `labStore.activeConsoleTab` stay correct;
- `.float-title` — a `<div role="toolbar">` carrying: the node/link name (reuse `nodeName` /
  `captureTitle` from `PaneBody`'s helpers — export them from a tiny shared module rather than
  duplicating), the node-state LED (mirror the `.led` treatment at `TopBar.svelte:300-315`,
  driven by `labStore.nodeStates[nodeId]`), the pin toggle already in the tab strip, and a
  close button calling **`consoleUiStore.closePane(ref)`** (§6.2, §1a finding 5);

  **The close button must not inline `labStore.closeConsole(nodeId)` / `closeCapture(linkId)`.**
  The flow it needs is `Console.svelte:44-65`, which is *private to a component that is not
  mounted in float placement* — so it is unreachable, and the tempting inline substitute
  silently drops two things: the focus hand-off to the next open pane (`:50-56`) and the
  native-overlay reset (`:49`), which would leave `nativeCapture[linkId]` true so that
  reopening the capture comes up on the Wireshark card instead of the live summary. §6.2 moves
  that logic onto the store as `closePane`, and **both** the dock's tab-strip buttons and this
  one call it — one implementation, no drift;
- the title bar is the drag handle (§6.5); it carries `touch-action: none` (same as
  `SplitPane.svelte:134`) and must **not** be a `<button>` — buttons inside it stop propagation
  on pointerdown the way `NodeActions.svelte:55` does, so a click on Close never starts a drag;
- `<PaneBody {ref} visible={true} focused={isTopmost} />`;
- a bottom-right `.float-grip` resize handle (§6.5).

Chrome styling follows the app's existing floating-card vocabulary — `PainterPanel.svelte:249`
and `WatcherPanel.svelte:190` are the closest precedents: `background: var(--panel)`,
`backdrop-filter: var(--blur)`, `border: 1px solid var(--border)`,
`border-radius: var(--radius-md)`, `box-shadow: var(--shadow-md)`.

### 6.5 `dragMove.ts` — the new primitive

New file `app/src/lib/dragMove.ts`. Not a component, not a Svelte action — a plain module with
one exported function, so it is unit-reasonable and has no lifecycle of its own.

**The complete API** (§1a finding 2 — the draft returned `void` and left "how a pointerdown
becomes ongoing movement and a clean release" unspecified, which yields either a window that
never moves or module-global drag state shared by every window):

```ts
export interface DragSpec {
  /** The value being dragged, as of pointerdown. */
  start: { x: number; y: number };
  /** Applied to every intermediate value before onMove/onEnd. */
  clamp: (x: number, y: number) => { x: number; y: number };
  /** Called on every move, with the clamped result. */
  onMove: (x: number, y: number) => void;
  /** Called exactly once, on pointerup or pointercancel, with the final clamped value. */
  onEnd: (x: number, y: number) => void;
}

/** The live drag. Every field of it is closure state — nothing is module-scoped. */
export interface DragHandle {
  move(e: PointerEvent): void;
  /** Idempotent: a second call (pointerup after pointercancel) is a no-op. */
  end(e: PointerEvent): void;
}

/**
 * Begin a pointer drag on the element the event was bound to. Returns the handle
 * the component drives, or null if this is not a drag (non-primary button, or the
 * event target is an interactive child that stopped propagation).
 */
export function beginDrag(e: PointerEvent, spec: DragSpec): DragHandle | null;
```

`beginDrag` returns `null` for `e.button !== 0`. Otherwise it captures `downX = e.clientX`,
`downY = e.clientY` and `spec` **in the closure it returns**, calls
`(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)` and `e.preventDefault()`
(`SplitPane.svelte:52-56`), and returns `{move, end}`:

- `move(e)` → `spec.onMove(...spec.clamp(start.x + e.clientX - downX, start.y + e.clientY - downY))`
- `end(e)` → releases the capture (`el.releasePointerCapture?.(e.pointerId)`,
  `SplitPane.svelte:73`), calls `spec.onEnd(...)` with the same clamped value, and sets an
  internal `done` flag so a second `end` is a no-op.

**Nothing in the module is mutable at module scope.** Two windows dragging in the same session
are two independent handles; a third window's `beginDrag` cannot see either. §6.12 gates this
with a grep.

**The component wiring, in full** — this is the part the draft left implicit. All three pointer
events are bound **in markup on the same element**, which is `SplitPane.svelte:93-96`'s idiom
verbatim, so an unmount takes the handlers with it and the browser releases the capture (§4.3):

```svelte
<script lang="ts">
  import { beginDrag, type DragHandle } from "../dragMove";
  let moveDrag: DragHandle | null = null;

  function onTitleDown(e: PointerEvent) {
    const g = consoleUiStore.windows[key];
    if (!g) return;
    moveDrag = beginDrag(e, {
      start: { x: g.x, y: g.y },
      clamp: (x, y) => clampPos(x, y, g.w, g.h, vp),          // §6.7
      onMove: (x, y) => consoleUiStore.moveWindow(key, x, y),
      onEnd: () => consoleUiStore.commitWindow(labId, key),   // §6.9 — the only persist
    });
  }
  function onTitleMove(e: PointerEvent) { moveDrag?.move(e); }
  function onTitleUp(e: PointerEvent) { moveDrag?.end(e); moveDrag = null; }
</script>

<div
  class="float-title"
  onpointerdown={onTitleDown}
  onpointermove={onTitleMove}
  onpointerup={onTitleUp}
  onpointercancel={onTitleUp}
>…</div>
```

The `.float-grip` resize handle repeats the same five lines with its **own**
`let sizeDrag: DragHandle | null` and its own spec (`start = {x: g.w, y: g.h}`,
`clamp = clampSize`, `onMove = resizeWindow`) — a second handle, not a shared one, and never a
shared module-level `active` flag. `onpointercancel` is bound in **both** places, not just
`onpointerup`: a cancel with no handler is precisely how a drag gets stuck.

**No `window.addEventListener` anywhere** (§4.3, §6.12).

Both interactions use the same primitive, differing only in the mapping:

- **move** — `x = start.x + (e.clientX - downX)`, `y = start.y + (e.clientY - downY)`,
  `clamp = clampPosition` (§6.7). Screen space, **no zoom divisor** — this is not
  `AnnoShape.svelte:80-84`, which divides by `labStore.canvasZoom` precisely because it lives
  inside the transformed canvas. A floating window is `position: fixed` outside the canvas;
  dividing by zoom here would be a subtle, hard-to-spot bug that only manifests after the user
  zooms the topology.
- **resize** — the same deltas applied to `w`/`h`, `clamp = clampSize` (`WIN_MIN_W`/`WIN_MIN_H`
  floors, plus the viewport ceiling), then the geometry re-clamped for position.

`onEnd` calls `consoleUiStore.commitWindow(labId, key)`, which is the only persistence write
(`SplitPane.svelte:74-80` idiom). The lab id is a **parameter** because the store cannot read
`labStore` (§6.2); the window component has it from the layer.

### 6.6 Stacking

Z-index is **derived from position in `windowOrder`**, not stored:

```
z = FLOAT_Z_BASE + index      // FLOAT_Z_BASE = 900
```

- `raiseWindow` is an array move-to-end, so ordering can never drift out of sync with the
  rendered z, and there is no monotonic counter to overflow past a modal after a long session —
  the failure the idea doc's "shared z-index counter" (`:233-235`) invites.
- The **900–999 band is empty today** (§3.5): floating windows therefore sit above every canvas
  panel (`WatcherPanel`/`PainterPanel` at 60, node panels at 30, the `SplitPane` divider at 20)
  and **below every popover, dialog and modal** (1000 / 1100 / 1200 / 2000 / 3000). That
  ordering is the correct one: the Image Manager, the lab browser and Preflight must remain
  usable with consoles floating, and the interface picker (1200) must appear over them.
- With more than 100 open panes the band would collide with `ChangeImagePopover` at 1000. Clamp:
  `z = FLOAT_Z_BASE + Math.min(index, 99)`. A hundred simultaneous consoles is not a scenario,
  but a clamp costs one expression and removes the class.
- `focused` for a `PaneBody` inside a window = "this key is last in `windowOrder`". One window
  is focused at a time, and `ConsoleTerm.svelte:196-203` calls `term.focus()` for exactly that
  one — the same single-focus invariant the dock has.

### 6.7 Viewport clamping — the invariant, and the four sites that enforce it

**Invariant:** for every window, at least `WIN_KEEP_VISIBLE` (120) horizontal pixels of the
title bar and the **full** title-bar height remain inside the viewport, and the top edge never
goes above the top bar.

```
minX = WIN_KEEP_VISIBLE - w
maxX = viewportW - WIN_KEEP_VISIBLE
minY = topbarH                      // never under the top bar (TopBar z-index 5, ours 900+)
maxY = viewportH - TITLE_H
```

**One implementation.** The invariant lives in the pure `clampGeom(g, vp)` exported from
`consoleUiStore.svelte.ts` (§6.2); `clampPos` and `clampSize` are thin projections of it for
`dragMove`'s `clamp` hook. **The store's `moveWindow`/`resizeWindow` do not clamp** — they
receive already-clamped values — so there is exactly one place the arithmetic can be wrong.

Enforced at exactly four sites, and **all four are required** — the idea doc names only the
first (`:240-242`):

1. **During a move** — `clampPos` as `dragMove`'s `clamp` (§6.5). Prevents the drag itself from
   stranding the window.
2. **During a resize** — a resize that shrinks `w` raises `minX`; a window dragged to the right
   edge and then narrowed would otherwise slide out. `clampSize` re-clamps position after every
   size change.
3. **On viewport resize** — `consoleUiStore.clampAllWindows(vp)` from the layer's
   `<svelte:window bind:innerWidth bind:innerHeight />`. This is the case that actually strands
   users in practice: float three windows on a 2560px monitor, undock the laptop, and two of
   them are past the right edge with no way back. `App.svelte:28,67` already tracks
   `innerWidth` for the dock's max-size computation; the layer needs its own binding for
   **height** as well, which does not exist anywhere today.
4. **On geometry creation** — `restoreGeom` and `cascadeGeom` both clamp before returning
   (§6.2), so the layer never hands `ensureWindow` an unclamped value. A lab saved on a large
   monitor therefore cannot reopen off-screen on a small one, which is site 3's failure with
   extra steps. This is the site that made `ensureWindow` viewport-dependent in the draft; the
   clamping moved to the caller, not away (§1a finding 4).

### 6.8 Interaction with P6 Batch 9's tiling

- `placement === "float"` ⇒ `App.svelte` mounts **neither** `SplitPane` (`:106`, `:130`), so
  `Console.svelte` is not mounted, so `layout`/`tiles`/`pinned` render nothing. They are **not
  cleared** — flipping back to `"dock"` restores the exact tile set, which is why `setPlacement`
  must not touch them.
- The dock-actions layout control (`Console.svelte:366-374`) is only reachable in dock
  placement, which is correct: a tiled grid is meaningless when every pane is its own window.
  The reverse control (float → dock) lives on the floating layer (§6.4).
- `ConsoleTerm.svelte:208-215` depends on `consoleUiStore.layout` and `dockSide`. In float
  placement neither changes, and the window's own size changes are covered by the ungated
  `ResizeObserver` (`:184-188`) — which fires because a floating window's box genuinely changes,
  unlike the untiled-pane case P6 §4.6 had to special-case. **No terminal change is needed.**
- Marks: `Console.svelte:126-133`'s `markNow()` button is dock-only. Float placement needs it
  too — put a mark button on the floating layer's toolbar calling the same store method. Do
  **not** duplicate the `capturePos` assembly logic; move it to a `consoleUiStore.markNow()`
  method that both callers use.

### 6.9 Persistence — what is saved, and why this does not contradict P6 §8.2

P6 §8.2 forbids persisting `tiles`, `focused`, `pinned`, marks and delivery counters, on the
grounds that they "reference lab-specific node and link ids" and restoring them "would restore
panes for a lab that is no longer open".

Geometry is a different object and the distinction must be stated in the code comment, because
it is exactly the kind of thing a later reader "fixes" in the wrong direction:

- **Membership** ("this pane is open / tiled / pinned") is a claim about the current session's
  state. Restoring it fabricates state.
- **Geometry** ("if a window for `console:3` in lab `abc` exists, it was last here") is a
  passive lookup that does nothing unless something *else* opens that pane. It is the same class
  as `SplitPane`'s `storageKey` (`SplitPane.svelte:29-38`, `:74-80`), which persists the console
  dock's height today and is not considered lab state.

Shape:

- One key, `iolbox.console.windows`, holding `Record<"<labId>|<paneKey>", WindowGeom>`.
  Namespacing by lab id means node `3` in one lab never inherits node `3`'s position from
  another — the hazard P6 was guarding against.
- Written **only** in `commitWindow(labId, key)` (drag/resize end), never per-move. The lab id
  is a parameter, not a store read (§6.2).
- Read by the **layer**, through the pure `restoreGeom(labId, key, vp)`, which **clamps on read**
  (§6.7 site 4) before the result ever reaches `ensureWindow`.
- Capped at 200 entries, drop-oldest by insertion order, so a user with many labs does not grow
  localStorage without bound. All reads and writes go through the same `try {} catch {}` guard
  every other pref in the file uses (`consoleUiStore.svelte.ts:51-58` is the template).
- `reconcile`'s hard reset clears the **live** `windows` map, not the persisted one.

### 6.10 Concrete file-level changes

| file | change | rough size |
|---|---|---|
| `app/src/lib/consoleUiStore.svelte.ts` | §6.2 — placement pref, `windows`/`windowOrder`, the pure `clampGeom`/`restoreGeom`/`cascadeGeom`, moved `nativeCapture` (`setNativeCapture` + the gesture-only `toggleNativeCapture`) / `searchOpenFor`, `ensureWindow(key, geom)` / `move` / `resize` / `commit(labId,key)` / `raise` / `clampAll(vp)` / `markNow`, `closePane` + `bindPaneClose`, one line in `setFocused`, two additions inside `reconcile` | +240 |
| `app/src/lib/dragMove.ts` | **new** — §6.5, `beginDrag → DragHandle`, no module-level state | +85 |
| `app/src/lib/components/PaneBody.svelte` | **new** — §6.3, moved from `Console.svelte`, **all three** pane kinds incl. `lens` | +210 (net ~0 across the two) |
| `app/src/lib/components/FloatingConsoleWindow.svelte` | **new** — §6.4 | +180 |
| `app/src/lib/components/FloatingConsoleLayer.svelte` | **new** — §6.4, owns the geometry pipeline and the viewport | +120 |
| `app/src/lib/components/Console.svelte` | bodies + helpers + `.native-*` CSS move out; `nativeCapture`/`searchOpenFor` reads redirect to the store; the three private close functions and the search-dismiss `$effect` (`:162-165`) are **deleted**, close buttons call `closePane`; the placement control joins the dock-actions row | −210 / +25 |
| `app/src/App.svelte` | mount the layer beside `<SwitchLabDialog />` (`:157`), gate the two `SplitPane`s on `placement === "dock"` (`:106`, `:130`), register `bindPaneClose` beside `bindConsoleSelect` (`:32-34`), move the Wireshark one-shot in beside the reconcile effect (`:39-46`) — as a **`setNativeCapture(link, true)`**, not a toggle | +32 |

**Zero diff** in `ConsoleTerm.svelte`, `CaptureTerm.svelte`, `labStore.svelte.ts`, and anything
under `supervisor/`.

### 6.11 Testing bar

1. `cd app && npm run check` — green. The `PaneBody` extraction is the likeliest source of a
   type error (the `PaneRef` union must be narrowed before `ref.node` / `ref.link`).
2. `cd app && npm run build` — green.
3. **Manual, in the dev mock (`mockTransport.ts`), before any VM work.** These are the cases a
   green build cannot speak to:
   - Open 3 consoles + 1 capture + **1 Lens** in dock mode with `tile3` selected. Flip to
     float: 5 windows, cascaded, none overlapping the top bar — **including the Lens window**
     (§1a finding 1). Flip back: `tile3` and the same three tiles.
   - Drag a window's title bar to each of the four viewport edges. It stops with ≥120px of title
     bar visible on the horizontal axes and the full title bar on the vertical ones. **Then drag
     a second window while the first is still on screen** — both move independently and neither
     jumps (the §6.5 handle is per-window closure state, not module-global).
   - Drag a window to the right edge, then narrow it with the corner grip. It slides back inside
     rather than off (§6.7 site 2).
   - Shrink the browser window to ~600×400 with three windows floating. All three re-clamp; none
     is unreachable.
   - Click each window in turn. The clicked one comes to front **and** the console address chip
     follows it, proving `setFocused` still reaches `labStore.activeConsoleTab` (`:190-199`).
   - Open the Image Manager and the interface picker with windows floating. Both render **above**
     every window (§6.6).
   - With a window focused, press Ctrl-F — the search bar appears inside that window
     (`ConsoleTerm.svelte:323`). **Now click a different window, then click back.** The search
     bar is **closed**, not latently reopened (§1a finding 6).
   - Flip a capture window to native Wireshark — the overlay card and
     the .pcapng download work (§4.2's whole point).
   - **With that overlay already showing**, invoke "Capture in Wireshark…" from the link's
     canvas menu again. The overlay **stays open** — it does not close (§1a finding 3). Repeat
     from a state where the overlay is off: it opens. Both directions, both placements.
   - **Close a floating capture window with its title-bar ✕**, then reopen the same capture from
     the canvas. It comes up on the **live summary**, not the Wireshark card, and focus moved to
     another open pane rather than to nothing (§1a finding 5 — this is the exact pair an inlined
     `labStore.closeCapture()` drops).
   - Drag a window, and while dragging, stop the node from the canvas so the pane is reconciled
     away. No console error, no orphaned listener, no stuck cursor (§4.3). Repeat with a
     `pointercancel` (drag off the window and release outside the browser).
   - Reload the page, reopen the same console: it reopens at its last position and size. Switch
     to a different lab and open a console with the same node id: it does **not** inherit the
     other lab's geometry (§6.9).
4. **No frontend test runner is added.** `app/` has none; P6 §6.3 and §9 both hold that line.

### 6.12 Acceptance gate

- `DockSide` is still `"bottom" | "right"` — `grep -n 'DockSide' app/src/lib/consoleUiStore.svelte.ts`
  shows no `"floating"`.
- `ConsoleTerm.svelte`, `CaptureTerm.svelte` and `LensPane.svelte` show **zero** diff.
- `labStore.svelte.ts` shows **zero** diff.
- No `window.addEventListener("pointermove"` anywhere in the batch's diff.
- **`grep -n "labStore" app/src/lib/consoleUiStore.svelte.ts` returns nothing** — the store
  boundary held; every cross-store fact arrived as a parameter or through a bound callback
  (§6.2, §1a finding 4).
- **`dragMove.ts` declares no module-level mutable state** — `grep -nE "^(let|var) " app/src/lib/dragMove.ts`
  returns nothing, and `beginDrag`'s return type is `DragHandle | null`, not `void`
  (§6.5, §1a finding 2).
- **`toggleNativeCapture` has exactly two call sites, both user gestures** — the tab-strip flip
  button and the overlay's "Back to live summary". The relocated Wireshark one-shot in
  `App.svelte` calls `setNativeCapture(link, true)` (§1a finding 3).
- **No `closeConsole` / `closeCapture` / `closeLens` call outside `App.svelte`'s `bindPaneClose`
  callback** — every close path goes through `consoleUiStore.closePane` (§1a finding 5).
- The search-dismiss `$effect` is gone from `Console.svelte` and its condition is in
  `setFocused` (§1a finding 6).
- No new `z-index` value ≥ 1000 in the batch's diff.
- The persisted geometry key contains the lab id.
- The `tile2` grid rule (`Console.svelte:815-818`) is **unchanged** — already fixed upstream,
  not this batch's to touch or regress (§3.6).
- Every checklist item in §6.11.3 passes in the mock, and §10.1 passes on the VM.

---

## 7. Batch 11a — per-node MAC list, VPCS/PC data path (idea #11, first half)

### 7.1 Decisions locked

1. **The surface is a button on the node hover toolbar plus a popover** (§3.9, §3.10). No
   canvas labels, no `TopBar` switch in this batch, **zero diff under `app/src/lib/edges/`**
   (§4.5).
2. **The button is on every node kind that can report a MAC** — `iol`, `vpcs`, `pc`, `tool` —
   and is shown regardless of run state, because what it shows differs by state and saying so is
   the feature. (Contrast the Console button, which is `isRunning`-gated at
   `NodeActions.svelte:62` because a console to a stopped node is meaningless.)
3. **Every MAC in the list carries a provenance and a state.** No row is ever a bare string.
   Three provenances: `derived` (VPCS, from the flag we pass), `read` (pc/tool, from the
   kernel), `learned` (IOL — Batch 11b only). States: `known`, `unknown`, and — 11b only —
   `ambiguous` and `disabled`.
4. **The VPCS formula stays in Go, beside the flag that causes it** (§1.2, §7.4). The browser
   never computes a MAC.
5. **One new verb, `node.macs`, request/response only.** No event, no polling, no addition to
   `link.stats`. The data is requested when a popover opens and discarded when it closes.
6. **Nothing here depends on P6 Batch 7.** Batch 11a ships and is useful with IOL rows reading
   "unknown".

### 7.2 The verb

Registered alongside its siblings in `server.go:221-226`:

```go
s.disp.Handle("node.macs", s.handleNodeMACs)
```

Request `{ "node": <id>, "learned": <bool> }` — `learned` defaults **false** and is Batch 11b's
opt-in (§8.2); Batch 11a's handler ignores it beyond validating the type.

Response (`protocol/verbs.go`, beside the other result types such as `StartResult` at `:161`):

```go
// NodeMAC is one interface's link-layer address for a node, with the PROVENANCE
// that licenses reporting it. There is no reading of this struct that yields a
// MAC the supervisor did not either compute from a flag it passed, read from the
// kernel, or positively learn from observed traffic.
//
// Source:
//   "derived" - computed from an argument this supervisor passed to the node
//               (VPCS -m; see node.VPCSMAC). Valid even while the node is stopped.
//   "read"    - read from the kernel for a device this supervisor created
//               (a netns node's GuestIface). Requires the node to be running.
//   "learned" - observed as the single source MAC on this endpoint's tap
//               (P6 Batch 7's dirstat attribution). Requires traffic AND the
//               learned-MAC DISPLAY opt-in. The supervisor learns either way;
//               the opt-in gates only whether this handler reports it.
//   ""        - nothing is known; State says why.
//
// State:
//   "known"     - MAC is set and is the interface's address.
//   "unknown"   - not knowable right now; Reason carries a short human phrase.
//   "ambiguous" - the endpoint relays for other devices, so no single address can
//                 be attributed to it (11b only; see P6 plan §7.3.2).
//   "disabled"  - knowable in principle, but the learned-MAC display opt-in is
//                 off (11b only). NOT a statement that learning is off.
type NodeMAC struct {
    Interface string `json:"interface"`         // the lab document's spelling: e0/0, eth0, eth1
    MAC       string `json:"mac,omitempty"`     // lowercase colon-separated; set iff State=="known"
    Source    string `json:"source,omitempty"`  // "derived" | "read" | "learned"
    State     string `json:"state"`             // "known" | "unknown" | "ambiguous" | "disabled"
    Reason    string `json:"reason,omitempty"`  // short phrase for the UI, e.g. "node not running"
}

type NodeMACsResult struct {
    Node int       `json:"node"`
    MACs []NodeMAC `json:"macs"`
}
```

The interface list comes from the same source the GUI uses, so the two can never disagree: the
handler enumerates the node's interfaces server-side using the node's kind and adapter counts —
the Go mirror of `interfaces.ts:9-24`. **If no such enumeration exists in Go, add it in
`internal/lab` and use it; do not have the browser send the interface list up**, which would
make the response's completeness depend on the caller.

`docs/protocol.md` gains a `node.macs` entry beside the other request verbs, with the `Source`
and `State` semantics reproduced in prose, in the same style as the existing `link.stats`
`protosDir` contract at `:203-240`.

### 7.3 Handler behaviour by node kind

| kind | interfaces | behaviour |
|---|---|---|
| `vpcs` | `eth0` | always `{mac: node.VPCSMAC(id, 0), source: "derived", state: "known"}` — **including while stopped**, because the formula is a property of the argv this supervisor will pass (§7.4) |
| `pc`, `tool` | `eth1` | running → read the kernel (§7.5), `source: "read"`, `state: "known"`; stopped → `state: "unknown"`, `reason: "node not running"` |
| `iol` | `e<a>/<p>` × 4 per ethernet group, `s<a>/<p>` × 4 per serial group | Batch 11a: every row `state: "unknown"`, `reason: "IOL MACs are learned from traffic"`. Batch 11b replaces this arm (§8.3) |
| `nat` | `eth0` | `state: "unknown"`, `reason: "not tracked for the NAT gateway"` — §3.8 establishes the tap's real MAC is nowhere in the tree and the DHCP server's synthesised source MAC (`extnet/dhcp_linux.go:154-157`) is **not** it. **Do not report `gatewayMAC`.** |

### 7.4 `node.VPCSMAC` — where the formula lives, and why not in TypeScript

New in `supervisor/internal/node/argv.go`, immediately below the `-m` documentation block it
depends on (`:150-160`):

```go
// VPCSMAC returns the MAC vpcs 0.8.3 will assign to PC index pcIndex of the node
// with the given id, GIVEN the "-m" value VPCSArgv passes (see the -m block
// above). It is a consequence of that flag, not an independent fact: if VPCSArgv
// ever stops passing "-m nodeID", this function is wrong and must change with it.
// That coupling is the entire reason this lives here and not in the GUI.
func VPCSMAC(nodeID, pcIndex int) string
```

with a unit test in `node_test.go` pinning `VPCSMAC(1, 0) == "00:50:79:66:68:01"`,
`VPCSMAC(0, 0) == "00:50:79:66:68:00"` and the `& 0xff` wrap at `VPCSMAC(256, 0)`, plus an
assertion that `VPCSArgv` still emits `-m <nodeID>` (`argv.go:173`) so the two cannot drift
silently.

The rejected alternative — four lines of TypeScript in `app/src/lib/macs.ts` — is cheaper and
wrong for a specific reason: `app/` has no test runner (§6.11.4) and no visibility into
`argv.go`, so the day `-m` changes (a vpcs upgrade, a multi-PC node, a uniqueness fix like the
one `argv.go:150-158` already documents having needed once) the GUI keeps confidently printing
the old MAC. A wrong MAC is worse than no MAC — the same argument P6 §4.2 makes about node names.

### 7.5 Reading a netns node's MAC

For `pc` / `tool`, the address is at `/sys/class/net/eth1/address` **inside** netns
`iolt<nodeID>` — the guest end of the veth pair (`tool/netns.go:31-44`, `tool.GuestIface` =
`"eth1"` at `tool/tool.go:71`, `tool.NetnsName` at `:38`). The argv builder already exists:

```go
tool.NetnsExecArgs(nodeID, []string{"cat", "/sys/class/net/" + tool.GuestIface + "/address"})
```

(`tool/tool.go:77`). Run it at request time only — this is a one-shot read behind a user action,
not a polled value.

**The trap, named explicitly:** the *root-side* device `vtool<nodeID>` (`tool/tool.go:43`) is in
the root namespace and trivially readable without any netns exec, and it is the device
`fabricLinkEndpointDevs` returns for these nodes (`fabric_linux.go:897-901`). It is the **other
end of the pair** and its MAC is not the node's. An implementing agent reaching for the easiest
read will find that one first. `/sys/class/net/vtool<id>/address` must appear nowhere in this
batch's diff.

### 7.6 GUI — `MacListPopover.svelte` and the toolbar button

**`NodeActions.svelte`** gains one button between Console (`:62-65`) and Save config (`:66-74`),
matching the shape at §3.9 exactly (`class="na-btn"`, `onpointerdown` stopPropagation, 12px
`uiSvg`). It sets a local `macPopover = {x, y}` from the click event's client coordinates and
renders `<MacListPopover>` when set. Guarded on kind ∈ {iol, vpcs, pc, tool}, derived the same
way `isIol` already is (`:24-26`).

**`MacListPopover.svelte`** — new, modelled line-for-line on `ChangeImagePopover.svelte` (§3.10):
props `{x, y, nodeId, onClose}`, `bind:this` + outside-mousedown + Escape via
`<svelte:window>`, `position: fixed` at `z-index: 1000`. On mount it issues `node.macs` through
the existing client (`labStore`'s transport, same path `labStore.setNodeImage` uses) and holds
the result in local `$state` — **not** in `labStore`, because it is request-scoped data with no
lifecycle beyond the popover, and putting it in the store would give `reconcile` one more thing
to prune for no benefit.

Rows render as a two-column monospace list using the lab document's interface spelling
(§4.5) and Cisco dotted-triplet MAC formatting, which is what "brief format" means here:

```
e0/0    aabb.cc00.0100   learned
e0/1    —                not seen yet
eth0    0050.7966.6801   derived
```

- Provenance is a small muted badge, visually subordinate to the address (reuse the
  `.stp-badge`-scale typography from `FloatingEdge.svelte:883-895` — 9px, 700 weight, mono — but
  in a neutral tint; a MAC's provenance is information, not an alarm).
- `state: "unknown"` renders `—` plus the `reason` string. **Never blank, never a guess.**
- Formatting helper lives in one new small module `app/src/lib/macs.ts` — `colonToDotted()` and
  nothing else. **No derivation, no formula** (§7.4).

**`mockTransport.ts`** gains a `case "node.macs"` (the switch runs `:184-511`) returning
plausible shaped data — VPCS/PC rows `known`, IOL rows `unknown` — so the dev mock exercises the
popover. Without it the request falls to `default:` (`:511`) and the popover shows an error in
every dev session.

### 7.7 Testing bar

1. `cd supervisor && go build ./... && go test ./internal/node/... ./internal/server/...` — green,
   including the new `VPCSMAC` tests (§7.4).
2. `cd app && npm run check && npm run build` — green.
3. In the dev mock: the button appears on IOL/VPCS/PC/tool nodes and not on NAT; the popover
   opens at the click point, closes on outside-click and Escape; a VPCS row shows a `derived`
   MAC; IOL rows show `—` with a reason.

### 7.8 Acceptance gate

- `grep -rn "50:79\|0050" app/src` returns nothing — the formula did not leak into the browser.
- `grep -rn "vtool" ` in the batch's diff returns nothing (§7.5).
- Zero diff under `app/src/lib/edges/`.
- `docs/protocol.md` documents `node.macs` including the `Source`/`State` semantics.
- The VPCS row is correct for a **stopped** node (§7.3) — the case an implementer will most
  likely gate on `isRunning` out of habit.

---

## 8. Batch 11b — IOL MACs from live learning, plus the opt-in toggle (idea #11, second half)

### 8.1 P6 Batch 7 has landed — reuse its dirstat MAC-learning channel directly

**This batch is not blocked.** The draft (and the review of it) were written while Batch 7 was
mid-implementation; §3.7 re-verifies the finished state. Batch 11b **builds no learning of its
own** — it reads an existing channel.

**Re-confirm before starting** — thirty seconds, and it catches a rebase onto a tree older than
this plan. These are a sanity check, **not a gate**; all four hit today:

```
grep -n "EndpointAttrib"  supervisor/internal/protocol/verbs.go                        # :559, :566, :610
grep -n "attribCandidate\|macTTL" supervisor/internal/dirstat/dirstat.go               # :49, :61, ...
grep -n "func Open(devs \[\]EndpointDev" supervisor/internal/dirstat/dirstat_linux.go  # :45
grep -n "fabricLinkEndpointDevs" supervisor/internal/server/fabric_linux.go            # :595 (in openLinkDirstat)
cd supervisor && go test ./internal/dirstat/                                           # ok
```

**Do not implement a MAC-learning loop in this batch under any circumstance** — not if a grep
misses, not "temporarily". P6 §7.3 is a 180-line design whose learning rule (singular-MAC with a
permanent-until-TTL `ambiguous` flip), endpoint-index correctness (`EndpointDev{Index, Dev}` from
`fabricLinkEndpointDevs`, not the compacted `fabricLinkTapDevs`), TTL/relearn lifecycle and
bounded cross-endpoint conflict set each absorbed a blocking review finding (P6 §1a findings
1, 2, 3). A second implementation would not merely duplicate that — it would almost certainly
reintroduce the "first source MAC wins, capped at N per endpoint" rule that P6 §4.2 documents as
*failing confidently* on any IOL switch, which is precisely the node kind idea #11's IOL half is
for. If a grep misses, the tree is wrong, not the plan: rebase.

**What Batch 11b consumes**, with real `file:line` (§3.7 has the fuller map):

- **`(*dirstat.Classifier).Attribution() []EndpointAttrib`** (`dirstat.go:321-346`) — nil-safe
  on a nil `*Classifier` (`:321-323`), mirroring `Snapshot()`'s copy-out contract (`:101`).
  Expiry is lazy inside it (`:330`), so a caller never has to age anything.
- **`protocol.EndpointAttrib{EndpointIndex, State, MAC}`** (`verbs.go:559-570`) with
  `State ∈ {"single","ambiguous","none"}` and `MAC` set **iff** `State == "single"` and the MAC
  is not in the conflict set (`dirstat.go:338-344`).
- **`EndpointIndex` is the lab document endpoint index**, not a slice position — the slice is
  sparse when an endpoint has no tap (`verbs.go:560-562`). §8.3's mapping depends on this and
  must never index by position.
- **The supervisor-side accessor**: the per-link classifiers live in `ll.dirstats`
  (`fabric_linux.go:601-618`, sampled at `:847-872`). `handleNodeMACs` reads them through the
  same `ll.mu` discipline that `:847` uses — **do not** hold `ll.mu` across the whole handler,
  and do not add a second map.

`dirstat` itself is **read-only for this batch**: zero diff in `supervisor/internal/dirstat/`
(§8.7).

### 8.2 The toggle — what it is, what it is not

Per §4.4, this is an **opt-in for displaying inferred data**, not a performance gate. That
framing must survive into the code and the UI copy.

- **State:** a new `app/src/lib/macUiStore.svelte.ts`, a rune-backed singleton with exactly one
  pref, following `consoleUiStore`'s shape (`:34-38` key constant, `:51-98` guarded initializer,
  `:212-226` write-through setter): key `iolbox.mac.learnIol`, default `false`.
  A whole file for one boolean is the right call here — it is not a console pref and
  `consoleUiStore`'s own doc comment (`:1-3`) scopes that file to the console dock.
- **Control:** one button in `TopBar.svelte`, matching the §3.11 idiom — `class="btn"`,
  `class:on={macUiStore.learnIol}` (the Tasks-button treatment at `:173-181`, since this toggles
  a persistent app preference rather than a browser API), `aria-pressed`, `title` and
  `aria-label` both flipping. Placed next to the fullscreen button (`:210-218`). One new glyph in
  `icons.svelte.ts`'s `UI_GLYPHS` (`:106-140`) — and note `uiSvg` silently falls back to the
  `net` glyph on a typo (`icons.svelte.ts:206-209`), so **eyeball the rendered icon**; a
  misspelled key will not fail the build.
- **Copy, exactly** — and note what changed and why (§1a finding 7). The draft said *"IOL MAC
  learning is off"*, which is **false**: learning is unconditional and stays that way (§4.4,
  §8.5, and the review reconfirmed that design is right). The toggle governs **display**, so the
  copy says display:
  - title/label, off: *"Learned IOL MAC display is off — IOL addresses are inferred from live
    traffic"*
  - title/label, on: *"Learned IOL MAC display is on — IOL addresses come from observed frames"*
  - the empty state in an IOL row when off: **"turn on learned-MAC display to see this"**
  - **not** "learning is off" or "enable IOL MAC learning" — the supervisor is learning either
    way and telling the user otherwise contradicts §8.5's whole architecture;
  - **not** "reduces CPU", "disables packet parsing", or any performance claim (§4.4).
- **Effect:** the GUI sends `node.macs` with `learned: macUiStore.learnIol`. When false the
  handler skips the attribution lookup entirely and returns
  `state: "disabled"` for every IOL row. VPCS/PC/tool rows are computed identically either way —
  **the toggle must have zero effect on them** (this is the single most likely place to get the
  gating wrong: a top-level `if (!learned) return unknownRows` would blank the derived rows too).

### 8.3 The handler's IOL arm — the mapping, and why it needs no guessing

The only inference in this batch is P6's, and it stops at the supervisor's MAC-to-endpoint
attribution. Turning that into per-node-per-interface rows is a pure lookup:

```
for each link L in the lab:
  for each endpoint index i in L.Endpoints:
    if L.Endpoints[i].Node == requested node:
      a := attribution(L)[where EndpointIndex == i]
      row for interface L.Endpoints[i].Interface :=
        a.State == "single"    -> {mac: a.MAC, source: "learned", state: "known"}
        a.State == "ambiguous" -> {state: "ambiguous", reason: "this port relays for other devices"}
        a.State == "none"      -> {state: "unknown", reason: "no traffic seen yet"}
        no attribution at all  -> {state: "unknown", reason: "per-endpoint attribution unavailable"}
```

**`L.Endpoints[i].Interface` *is* the interface name** (`labTypes.ts:48-53`; the lab document
carries `{node, interface}` per endpoint). So the endpoint index that P6's attribution is keyed
by resolves to an interface without any matching heuristic — no name canonicalisation, no
"probably e0/0". Contrast `painterStore.stpBadgeFor` (`painterStore.svelte.ts:260-279`), which
*does* need canonicalisation because the STP data comes back from IOS with IOS spelling; there
is no such gap here.

Rows for an IOL interface with **no link at all** stay `unknown` with reason
`"no link on this interface"` — an unconnected port has no tap and can never be learned, and
saying so is more useful than a bare dash.

The `d.Index > 1` cap (`dirstat_linux.go:53-55` — now a guard on the **doc** index, per P6
§7.3.1) means endpoints beyond index 1 on a segment link get no attribution. Those rows read `unknown` with reason
`"attribution covers two endpoints per link"`. Do **not** remove that cap (P6 §9).

### 8.4 Concrete file-level changes

| file | change |
|---|---|
| `app/src/lib/macUiStore.svelte.ts` | **new** — one pref, §8.2 (~50 lines) |
| `app/src/lib/components/TopBar.svelte` | one button beside `:210-218`, §8.2 |
| `app/src/lib/icons.svelte.ts` | one glyph in `UI_GLYPHS` (`:106-140`) |
| `app/src/lib/components/MacListPopover.svelte` | send `learned`; render `ambiguous` / `disabled` states and the `learned` provenance badge |
| `supervisor/internal/server/handlers.go` | the IOL arm, §8.3 |
| `supervisor/internal/protocol/verbs.go` | doc-comment additions for the two new `State` values |
| `docs/protocol.md` | `node.macs` gains the `learned` request field and the two states |
| `app/src/lib/mockTransport.ts` | mock IOL rows that flip with `learned` |

**Zero diff** in `supervisor/internal/dirstat/` — Batch 7 owns that package and this batch only
calls into it.

### 8.5 Explicitly rejected: gating dirstat's learning with an atomic flag

The obvious way to make the toggle "real" is an `atomic.Bool` in `dirstat` checked in `readLoop`
before the learning branch, flipped by a new verb. It is rejected for a reason that is not
aesthetic:

`dirstat`'s attribution **already has two consumers today**: the Protocol Lens, which needs it
whenever a Lens pane is open (`lens.ts:75-77`, `LensPane.svelte:72-75`), and this MAC list. A
flag driven by the MAC toggle would silently turn off Lens attribution for any user who leaves
the MAC toggle off — which is the default, and which would present as "the Lens stopped naming
nodes" with no visible cause. Making the flag an OR of two demands ("learn when the MAC toggle
is on **or** any Lens consumer is attached") means tracking Lens attachment as supervisor state:
new lifecycle, new failure modes, in exchange for a saving whose size **this plan does not
know** (§1a finding 8 — the draft's "6-byte compare under an already-held lock" was withdrawn;
`observeSource` takes its own lock and does real work, `dirstat.go:245-315`). The correctness
argument alone settles it. If the cost ever matters, the measured route is available and is the
only acceptable one: benchmark `readLoop` with and without the learning branch under load, and
revisit **with a number**. Not before, and not by guessing in either direction.

### 8.6 Testing bar

1. `cd supervisor && go build ./... && go test ./...` — green.
2. `cd app && npm run check && npm run build` — green.
3. In the dev mock: toggling the top-bar switch flips IOL rows between `"turn on learned-MAC
   display to see this"` and mock MACs, and **leaves the VPCS/PC rows byte-identical** (§8.2).
4. `grep -rn "MAC learning is o" app/src` returns nothing — the retired copy did not survive
   (§1a finding 7).
5. The real assertions are live-only — §10.3.

### 8.7 Acceptance gate

- The §8.1 re-confirmation greps all hit before the batch starts (they do today).
- Zero diff in `supervisor/internal/dirstat/`.
- No UI string anywhere claims learning is off (§8.2, §1a finding 7).
- No new atomic, flag or verb that gates dirstat's learning (§8.5).
- Toggling the switch changes no VPCS/PC/tool row.
- No IOL row ever renders a MAC the supervisor reported as `ambiguous` or `none`.
- §10.3 passes on the appliance against a real IOL switch — the case that defeats a naive
  implementation.

---

## 9. Explicit non-goals for the implementing agents

**Batch 10**
- **Do not add a third `DockSide` value** (§4.1). Placement is its own type.
- **Do not modify `ConsoleTerm.svelte` or `CaptureTerm.svelte`** — no `floating`, `inWindow`,
  `tileIndex` or `zIndex` prop (P6 §8.6, §6.1.4).
- **Do not modify `labStore.svelte.ts`.** P6 Batch 9 established the reconcile pattern; use it.
- **Do not use `window.addEventListener` for drag** (§4.3). Pointer capture on the handle.
- **Do not divide drag deltas by `labStore.canvasZoom`** (§6.5). That is `AnnoShape`'s concern
  because it lives inside the transformed canvas; a fixed-position window does not.
- **Do not persist `tiles`, `focused`, `pinned`, marks or delivery counters** to make float mode
  restore "properly" (P6 §8.2). Only geometry persists, and it is lab-namespaced (§6.9).
- **Do not implement snapping, tiling, minimize, maximize, or per-pane docked/floating mixing.**
  §11.
- **Do not open a real OS window** (`window.open`) for a console. The xterm would have to be
  remounted into a document with none of the app's CSS, and every store reference would cross a
  window boundary.
- **Do not touch the `tile2` grid rule** (`Console.svelte:815-818`) — it was a real bug and is
  **already fixed** in the tree (§3.6). Do not re-fix it and do not regress it while moving the
  pane bodies out.
- **Do not "correct" the mark-pruning condition** (`consoleUiStore.svelte.ts:322`). It is **not
  a defect** — it is P6's specified design (P6 plan `:1198`), and marks are meant to outlive
  their captures while a console is open (§3.6, §1a finding 9).
- **Do float the `lens` pane kind.** It exists now (`Console.svelte:566-580`,
  `labStore.openLensTabs`, `PaneRef`'s `"lens"` arm at `consoleUiStore.svelte.ts:9`); the
  earlier instruction to ignore it is retired. Still handle an **unknown** future kind by
  ignoring it, as P6 §8.7 requires.
- **Do not give `beginDrag` a `void` return or module-level drag state** (§6.5). One handle per
  drag, closure-scoped.
- **Do not call `toggleNativeCapture` from any automated call site** — the relocated Wireshark
  one-shot uses `setNativeCapture(link, true)` (§6.3). Toggles are for user gestures only.
- **Do not inline `labStore.closeConsole` / `closeCapture` / `closeLens` in the floating window.**
  Use `consoleUiStore.closePane(ref)` (§6.2) — the inline version drops the focus hand-off and
  the native-overlay reset.
- **Do not have `consoleUiStore` read `labStore`, `window`, or a pane list.** Callers pass
  viewport, lab id and pane facts in (§6.2). `grep -n "labStore" consoleUiStore.svelte.ts` must
  stay empty.

**Batch 11a**
- **Do not render MACs on the canvas.** Zero diff under `app/src/lib/edges/` (§4.5).
- **Do not compute a MAC in TypeScript** (§7.4).
- **Do not read `/sys/class/net/vtool<id>/address`** — that is the root-side veth, not the node's
  interface (§7.5).
- **Do not report the NAT gateway's synthesised DHCP source MAC** (`extnet/dhcp_linux.go:157`)
  as the NAT node's interface MAC (§7.3).
- **Do not add a polling event or extend `link.stats`.** One request verb, on demand (§7.1.5).
- **Do not store the response in `labStore`.** It is popover-scoped (§7.6).
- **Do not gate the VPCS row on the node running** (§7.3).
- **Do not rename interfaces to IOS spelling** to match the idea row's `Gi0/0` example (§4.5,
  P6 §11).

**Batch 11b**
- **Do not re-derive P6 Batch 7's channel** (§8.1). It has landed; the four greps re-confirm it.
  If one misses, rebase — do not build a substitute.
- **Do not implement MAC learning.** Not in `dirstat`, not in `bcap`, not in the browser off the
  capture stream, not "temporarily until Batch 7 lands".
- **Do not learn a *set* of MACs per endpoint** at any cap (P6 §1a finding 1) — if you find
  yourself writing this, you are re-implementing the thing §8.1 forbids.
- **Do not attribute a node name or a MAC from an `ambiguous` or `none` endpoint** (P6 §7.3.5).
- **Do not gate `dirstat`'s learning with a flag** (§8.5).
- **Do not let the toggle affect VPCS/PC/tool rows** (§8.2).
- **Do not claim a CPU saving in the UI copy** (§4.4).

**All batches**
- Do not run `go build`, `npm run check` or any test during the *planning* review pass — the
  implementing agents run them, per §6.11 / §7.7 / §8.6.
- Do not "fix" unrelated in-flight p5 or p6 changes found in the working tree (§2). Specifically:
  `InterfacePicker.svelte`'s `pc` arms, `PcNode.svelte`, `fabric_fault*.go`, and anything under
  `runtime/files/tools/packs/pc|netsvc/`.

---

## 10. Live-VM / manual-verification checklist (orchestrator only)

Unit tests cannot prove any of this. §10.1 is cheap; §10.2 is cheap; §10.3 is the only one that
can fail in a way the code review would not have caught, and it needs a real IOL switch.

### 10.1 Batch 10 — floating consoles, on the appliance

1. Start a 4-node lab (2 IOL, 1 VPCS, 1 PC). Open all four consoles docked, `tile4`. Open a
   capture **and its Lens** on one link too — the Lens pane exists now and must float (§1a
   finding 1).
2. Flip to float. Every open pane gets a window, cascaded, none under the top bar, each showing
   live output that was already streaming (nothing reconnected — watch for a re-login prompt;
   there must not be one). The Lens window keeps rendering events across the flip.
3. Type `show version` in one window; it wraps at that window's own width. Resize it narrower;
   the next command wraps at the new width — proof NAWS followed the window
   (`ConsoleTerm.svelte:184-188` doing its job through a resize the dock never produced).
4. Start a capture on a link, flip that window to native Wireshark, download the `.pcapng`, open
   it. This is §4.2's whole justification and it is the step most likely to reveal the extraction
   was incomplete. **Then, with the overlay still up, use the link's canvas menu "Capture in
   Wireshark…" again — the overlay must stay open, not close** (§1a finding 3).
5. Ctrl-F in a console window; search its scrollback. Click another window, click back: the
   search bar is closed (§1a finding 6).
6. Drag a window mostly off each edge; confirm it is always recoverable. Resize the browser
   window down to a laptop size; confirm all four re-clamp.
7. Stop a node from the canvas while its window is focused; the window closes cleanly (via
   `reconcile`'s prune), the remaining windows keep their z-order, no console errors. Then close
   a **capture** window with its own ✕ and reopen that capture: it returns on the live summary
   and focus landed on another pane (§1a finding 5).
8. Flip back to dock; `tile4` and the same four panes return.
9. Reload; reopen the same consoles floating; each returns to its last geometry.

### 10.2 Batch 11a — per-node MAC list

1. Stopped lab: open the MAC popover on a VPCS node. It shows `eth0` with a `derived` MAC.
2. Start the lab. On the VPCS console run `show ip` (VPCS prints its MAC) and confirm it matches
   the popover **byte for byte**. This is the only real test of §7.4's formula, and it is the one
   that would catch a wrong `-m` assumption.
3. Two VPCS nodes: their MACs differ in the last octet, matching their node ids (the bug
   `argv.go:150-158` documents having fixed).
4. A `pc` node, running: `eth1` shows a `read` MAC. Confirm against
   `ip netns exec iolt<id> ip link show eth1` on the appliance. Stop it: the row goes to
   `unknown` with "node not running".
5. An IOL node: every interface row reads `unknown` with the "learned from traffic" reason.
   Nothing is blank and nothing is guessed.
6. A NAT node: `unknown`, with the NAT-specific reason — **not** the gateway MAC.

### 10.3 Batch 11b — IOL MACs, against real traffic (the one that matters)

1. Toggle off (default). Every IOL row reads **"turn on learned-MAC display to see this"** — the
   §8.2 copy, which does **not** claim learning is off (§1a finding 7). VPCS and PC
   rows are unchanged from §10.2 — check this explicitly (§8.2).
2. Toggle on. IOL rows go to "no traffic seen yet" until frames flow.
3. Two IOL routers directly connected, both up, CDP/OSPF chattering. Both ends learn a single
   MAC each and show `learned` addresses. Verify each against `show interfaces e0/0 | include
   address` on the router itself. **They must match exactly.**
4. **The switch case, which is the whole point of P6's never-guess design.** Build
   `R1 — SW1 — R2` with SW1 an IOL L2 image. Ping R1→R2 so R1's and R2's MACs both cross SW1's
   ports. Open the MAC popover on **SW1**. Its link-facing interfaces must render **`ambiguous`
   with the relay explanation** — **not** R1's MAC, **not** R2's MAC, **not** a plausible-looking
   `aabb.cc00.xxxx`. A build that shows a MAC here has re-derived the defect P6 §4.2 documents
   and must not ship.
5. Leave the lab idle past P6's `macTTL` (5 min). A quiet endpoint's row degrades to
   "no traffic seen yet" rather than serving a stale address. Generate traffic; it relearns.
6. Restart one router. Its endpoint goes ambiguous (two MACs seen), then relearns cleanly after
   the TTL — P6 §7.3.3's relearn path, observed rather than assumed.
7. On a non-root dev box (or with `dirstat` unable to bind): every IOL row reads
   "per-endpoint attribution unavailable" rather than "no traffic seen yet" — the two are
   different facts and the UI must not conflate them.

---

## 11. Out of scope — named, not silently dropped

**Idea #10 / floating consoles**
- **Per-pane mixing** — some consoles docked, some floating. The idea doc defers it explicitly
  (`:250-253`); the store's `placement` is global by design and per-pane would need a
  `Record<paneKey, placement>` plus a rule for what the dock does when it holds zero panes.
- **Snapping and drag-to-tile between floating windows.** The idea doc names it as a follow-on
  (`:246-248`).
- **Minimize / maximize / roll-up.** A window that can be minimized needs a taskbar, which is a
  second surface.
- **Tile drag-reordering in the dock.** P6 §9 deferred it *to* this batch's primitive; it is
  still a separate change with its own acceptance surface.
- **Real OS windows** (`window.open` popouts), multi-monitor placement, and anything requiring
  the terminal to remount in another document (§9).
- **Per-window font size.** `consoleUiStore.fontSize` stays global (`:106`), as P6 §11 held.
- **Persisting *which* panes are open** across a reload. Only geometry persists (§6.9).

**Idea #11 / MAC list**
- **Canvas link-end MAC labels** and any `TopBar` switch that renders them (§4.5). Withdrawn by
  the row rewrite; `FloatingEdge.svelte`'s STP-badge slot (§3.12) remains available if it ever
  returns.
- **A full MAC/CAM table view** — every MAC a switch has learned. That is P6 §11's "full
  CAM-table learning", explicitly out of scope there and here: the attribution channel keeps
  **one** candidate per endpoint by design (P6 §7.3.2) and is structurally the wrong source for
  an enumeration.
- **NAT and root-side veth MACs** (§3.8, §7.3).
- **IPv6 link-local addresses, ARP tables, and neighbour discovery** in the same popover. A
  different data source with a different lifecycle.
- **Copy-to-clipboard on a MAC row.** Trivial to add later with `copyText`
  (`Console.svelte:221-232`); not v1, and not worth pulling that helper out of `PaneBody` for.
- **A supervisor-side gate on dirstat's learning** (§8.5).
- **MACs in the lab document, in export, or in `contracts/lab.schema.json`.** These are runtime
  observations, not lab content.
- **Interface aliasing** (`Gi0/0` vs `e0/0`) — P6 §11 owns that decision and it is unchanged.

---

### Critical files for implementation

**Batch 10 (idea #10)**
- `J:\Claude code\iolab\app\src\lib\consoleUiStore.svelte.ts` (:5 `DockSide` — **must stay
  two-valued**; :6-28 `PaneRef`/`paneKey`/`samePane` — the window identity; :34-38 the pref-key
  block the new key joins; :51-98 the guarded-initializer template; :107-115 the Batch 9 session
  fields; :168-184 `ensureTiled`; :186-199 `bindConsoleSelect`/`setFocused`; :276-353
  `isOpen`/`reconcile` — both halves gain window pruning; :382-393 `setDockSide`/`toggleDockSide`)
- `J:\Claude code\iolab\app\src\lib\components\Console.svelte` (**956 lines post-Batch-7 — every
  number below is against that, not the pre-Batch-7 tree**: **:16 `searchOpenFor` and :26
  `nativeCapture` — the two fields that must move to the store, and the reason floating mode
  cannot just wrap `ConsoleTerm`**; **:44-65 `closeCapture`/`closeConsole`/`closeLens` — private,
  unreachable from a floating window, and the flow §6.2's `closePane` takes over**; :154-160 the
  Wireshark one-shot that must move to `App.svelte` **as a SET, not a toggle**; :162-165 the
  search-dismiss `$effect` that is **deleted** and folded into `setFocused`; :461-481, :491-562
  and **:571-578 the three pane bodies** (console, capture, **lens**) that become
  `PaneBody.svelte`; :806-864 the `.term-area`/`.term-slot` CSS that stays; **:815-818 the
  `tile2` rule — already fixed upstream, do not touch**)
- `J:\Claude code\iolab\app\src\lib\components\ConsoleTerm.svelte` (**READ ONLY** — :14-30 the
  container-agnostic props a floating window passes unchanged; :184-188 the ungated
  `ResizeObserver` that makes window resizing free; :196-203 `visible`/`focused`) and
  `LensPane.svelte` (**READ ONLY** — same container-agnostic contract, `{linkId, visible,
  focused, title}`; a floating Lens window needs no change to it)
- `J:\Claude code\iolab\app\src\lib\components\SplitPane.svelte` (**the drag idiom to copy** —
  :29-38 persisted restore before first paint; :52-56 `setPointerCapture`; :57-69 clamped move;
  :70-81 release + persist; :93-96 handlers bound in markup incl. `onpointercancel`; :133-134
  `z-index: 20` + `touch-action: none`)
- `J:\Claude code\iolab\app\src\lib\nodes\AnnoLine.svelte` (**the drag idiom NOT to copy** —
  :56-63 `window` listeners attached with no `onDestroy` anywhere in the file; :64-82 the
  move/up pair) and `AnnoShape.svelte` (:69-90 the same shape for resize; **:80-84 the
  `canvasZoom` divisor that must NOT be carried over**, §6.5)
- `J:\Claude code\iolab\app\src\App.svelte` (:28,:67 `winW` — the layer needs `innerHeight` too;
  :32-34 `bindConsoleSelect`; :39-46 the reconcile effect the Wireshark one-shot joins; :51-53
  `showConsole`; **:54,:106,:130 the three `dockSide` reads that break under a third value**,
  §4.1; :157 `<SwitchLabDialog />` — the sibling mount point for the floating layer)

**Batch 11a (idea #11, first half)**
- `J:\Claude code\iolab\supervisor\internal\node\argv.go` (**:150-160 the `-m` doc block that
  documents the MAC formula; :161-183 `VPCSArgv`, :173 the `-m nodeID` this derives from** —
  `VPCSMAC` goes here and nowhere else, §7.4)
- `J:\Claude code\iolab\supervisor\internal\server\handlers.go` (:918-921 `buildSpec`'s
  `NodeID: n.ID`; :943 `spec.VPCSCount = 1` — the two facts that make the formula single-valued
  per node; the new `handleNodeMACs` goes beside its siblings)
- `J:\Claude code\iolab\supervisor\internal\tool\tool.go` (:38 `NetnsName`; **:43 `HostVethName`
  — the root-side veth that must NOT be read**; :71 `GuestIface = "eth1"`; :77 `NetnsExecArgs`)
  and `tool\netns.go` (:31-44 where the guest interface is created and renamed)
- `J:\Claude code\iolab\app\src\lib\nodes\NodeActions.svelte` (:15 props; :24-26 the `isIol`
  derivation pattern the new guard copies; :62-65 the Console button — the exact button shape,
  including the `onpointerdown` stopPropagation that prevents a node drag; :131-153 `.na-btn`)
- `J:\Claude code\iolab\app\src\lib\components\ChangeImagePopover.svelte` (**the popover
  template** — :4-5 props; :9-14 outside-click + Escape; :25 `<svelte:window>`; :26,:43-46
  `position: fixed` + `z-index: 1000`)
- `J:\Claude code\iolab\app\src\lib\interfaces.ts` (**READ ONLY** — :9-24 `allInterfaces`, the
  spelling the Go-side enumeration must match: :12 `eth0` for vpcs/nat, :13 `eth1` for tool/pc,
  :17-22 `e<a>/<p>`/`s<a>/<p>` for IOL)
- `J:\Claude code\iolab\app\src\lib\mockTransport.ts` (:184-511 the verb switch; :511 the
  `default:` a missing `node.macs` case would fall into)

**Batch 11b (idea #11, second half)**
- `J:\Claude code\iolab\docs\p6-protocol-lens-interface-suggest-console-workspace-plan.md`
  (**§7.3 in full** — :701-876: §7.3.1 the endpoint-index fix, §7.3.2 the singular-MAC learning
  rule, §7.3.3 TTL/relearn/conflict, §7.3.4 the `EndpointAttrib` wire shape, §7.3.5 the
  resolve-once rule; §4.2 at :289-334 for why the alternatives are rejected)
- `J:\Claude code\iolab\supervisor\internal\dirstat\dirstat.go` (**READ ONLY for this batch** —
  :43-49 the `attribState` values and `macTTL`; :61,:68 `attribCandidate`/`conflictMAC`;
  :116-121 `count`; :223-232 lazy expiry; :245-315 `observeSource`, the singular-MAC rule;
  **:321-346 `Attribution()` — the one function this batch calls**)
- `J:\Claude code\iolab\supervisor\internal\dirstat\dirstat_linux.go` (**READ ONLY for this
  batch** — :21 `snapLen = 128`; :45-70 `Open(devs []EndpointDev)`, the indexed form; :53-55 the
  `Index > 1` cap that stays; :117-140 `readLoop`, `count` then `observeSource`)
- `J:\Claude code\iolab\supervisor\internal\server\fabric_linux.go` (:594-599 `openLinkDirstat`,
  now feeding `fabricLinkEndpointDevs` directly; :601-618 the `ll.dirstats` map; :847-872 the
  sampling path that already calls `dc.Attribution()` — copy its locking discipline)
- `J:\Claude code\iolab\supervisor\internal\protocol\verbs.go` (:559-570 `EndpointAttrib`;
  :605-610 `LinkStatsData.EpAttrib` — the wire contract, and the doc comment §8.3's mapping
  relies on)
- `J:\Claude code\iolab\app\src\lib\components\TopBar.svelte` (**:210-218 the fullscreen button —
  the exact toggle idiom**; :173-181 the Tasks button, the closer analogue for a persistent
  preference; :354-358 `.btn.on`; :152-163 the `.seg` segmented control this is **not**)
- `J:\Claude code\iolab\app\src\lib\icons.svelte.ts` (:106-140 `UI_GLYPHS`; **:206-209 `uiSvg`'s
  silent fallback to the `net` glyph on an unknown name** — a typo will not fail the build)
- `J:\Claude code\iolab\app\src\lib\labTypes.ts` (:48-53 `LabEndpoint {node, interface}` — the
  reason §8.3's mapping needs no heuristic)
