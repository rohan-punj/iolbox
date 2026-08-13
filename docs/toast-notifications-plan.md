# Toast notifications implementation plan

## Scope and architectural decision

This is a planning document only. The implementation pass should add passive, top-right lifecycle confirmations without changing the supervisor protocol. The protocol already separates correlated request/response messages from unsolicited server pushes (`supervisor/internal/protocol/message.go:11-24`, `supervisor/internal/protocol/message.go:36-40`), and all relevant verbs are already registered (`supervisor/internal/server/server.go:227-249`).

Keep notification ownership in the existing `LabStore` singleton. It is already the one shared Svelte 5 rune store (`app/src/lib/labStore.svelte.ts:1-2`), subscribes to `SupervisorClient.onEvent` before the handshake so pushes are not missed (`app/src/lib/labStore.svelte.ts:213-227`), and funnels those pushes through one `handleEvent` switch (`app/src/lib/labStore.svelte.ts:459-538`). `SupervisorClient` itself already multiplexes all push handlers over its single transport subscription (`app/src/lib/supervisor.ts:42-47`, `app/src/lib/supervisor.ts:70-73`, `app/src/lib/supervisor.ts:100-111`). Do **not** add a second WebSocket listener, a component-level `client.onEvent`, or a second transport-facing store.

Use the signal that proves each operation actually completed:

- Use the awaited RPC result for user-initiated operations such as save, start, stop, restart, wipe, delete, clone, force-clean, and config extraction. These are correlated request completions; the client methods and result types already expose the needed data (`app/src/lib/supervisor.ts:140-162`, `app/src/lib/supervisor.ts:165-194`, `app/src/lib/protocol.ts:194-201`).
- Use the existing `handleEvent` path only when the push is itself the authoritative lifecycle signal: an unsolicited `node.state="crashed"`, `capture.started`/`capture.stopped`, or an asynchronously activated/cleared `link.fault`. The frontend's complete push union is defined at `app/src/lib/protocol.ts:35-119`.
- Do not generate generic toasts for every `node.state="starting"|"running"|"stopped"`. A single five-node `lab.start` emits per-node state transitions because every state-machine change calls the server's `node.state` callback (`supervisor/internal/server/handlers.go:359-363`); toasting those would be noisy and would duplicate the one RPC-level lab summary.

The feature is an **Operate-mode** addition: quiet, factual feedback for users building/running network labs. That matches the product's “instrument-grade precision” requirement (`PRODUCT.md:43-59`) and its rule that motion must be functional and reduced-motion-safe (`PRODUCT.md:89-91`, `PRODUCT.md:103-108`).

## Event and verb mapping

Message templates below use the current lab document to resolve `{nodeName}` from an id and use the protocol's numeric link id for `{linkId}`. Counts must use singular/plural helpers (`1 node`, `2 nodes`). A “success source” of **RPC** means enqueue only after the awaited call resolved successfully; never enqueue at click time.

| Source | Trigger/condition | Exact message template | Severity | Why this signal is correct |
|---|---|---|---|---|
| `lab.saveDoc` RPC, explicit/manual save only | `saveLab({ notify: true })` completes | `Lab saved — {labName}` | `success` | `saveLab` already awaits PC-state sync and the durable save before marking the lab saved (`app/src/lib/labStore.svelte.ts:663-680`). Do not toast from `lastSavedAt`, because autosave uses the same method after a 1.2 s debounce (`app/src/lib/labStore.svelte.ts:784-795`). Change the method to accept `notify = true`, and call it with `false` from `scheduleAutosave`; this prevents an edit/drag from producing a toast every debounce cycle. |
| `node.start` RPC | result contains the requested id in `started` and has no failure for it | `{nodeName} started` | `success` | `node.start` returns the same `LabStartResult` as `lab.start` (`app/src/lib/supervisor.ts:165-170`, `app/src/lib/supervisor.ts:185-186`), so the result confirms the start rather than merely acknowledging the click. The existing node lock still follows terminal `node.state` events (`app/src/lib/labStore.svelte.ts:58-65`, `app/src/lib/labStore.svelte.ts:461-475`). |
| `node.stop` RPC | call resolves | `{nodeName} stopped` | `info` | The server's stop handler calls synchronous `stopNode` and only then returns (`supervisor/internal/server/handlers.go:530-540`); `stopNode` transitions the runtime to stopped in its terminal paths (`supervisor/internal/server/handlers.go:1076-1123`). Neutral/info avoids presenting a deliberate stop as either an error or a celebratory success. |
| `node.restart` RPC | result contains requested id in `started` and has no failure for it | `{nodeName} restarted` | `success` | Restart synchronously stops the old instance, then calls `startNodes`, so the response represents the replacement start (`supervisor/internal/server/handlers.go:543-559`). |
| `lab.start` RPC | zero failures, at least one node started | `Lab started — {startedCount} {nodeWord} running` | `success` | `handleLabStart` expands a nil node selection to all lab nodes and returns only after `startNodes` completes (`supervisor/internal/server/handlers.go:388-414`). One summary intentionally replaces all per-node running toasts. |
| `lab.start` RPC | result has failures | `Lab start incomplete — {failedCount} of {totalCount} nodes failed` | `error` | Bulk start explicitly returns successful and failed nodes together (`supervisor/internal/protocol/verbs.go:161-174`). Keep the existing detailed `lastError` banner for node-by-node error text; the toast is only the short transient summary. |
| `lab.start` action | current lab contains zero nodes | `Lab has no nodes to start` | `info` | Edge-case feedback prevents a silent no-op. It should be decided client-side before sending the verb, analogous to the existing missing-image preflight (`app/src/lib/labStore.svelte.ts:999-1009`). |
| `lab.stop` RPC | call resolves | `Lab stopped — all {nodeCount} {nodeWord} stopped` | `info` | A full stop expands to all ids, synchronously stops each, tears down fabric/captures, and then responds (`supervisor/internal/server/handlers.go:417-457`). This single message satisfies both “lab stopped” and “all nodes stopped”; do not enqueue a second “all nodes stopped” toast. For an empty lab, use `Lab stopped — no nodes were running`. |
| `lab.wipe` RPC, all nodes | result returns `wiped` ids | `Lab wiped — saved state cleared for {wipedCount} {nodeWord}` | `info` | Wipe is destructive but successfully completed, so neutral is calmer than green. The result explicitly lists wiped ids (`supervisor/internal/protocol/verbs.go:267-279`), and the server stops each target before deleting its NVRAM (`supervisor/internal/server/handlers.go:482-515`). |
| `lab.wipe` RPC, one node | requested id appears in `wiped` | `{nodeName} wiped — saved state cleared` | `info` | Same completion semantics as the all-node wipe, but preserve the node name before awaiting in case the document changes. The current method already keeps its action lock until the RPC settles (`app/src/lib/labStore.svelte.ts:1092-1103`). |
| `lab.reap` RPC | call resolves | `Force clean complete — stopped {reapedCount} {nodeWord} and cleared relays` | `info` | The result contains the reaped count (`supervisor/internal/protocol/verbs.go:281-286`); the handler also tears down fabric and captures before returning (`supervisor/internal/server/handlers.go:460-479`). Replace the current log-only success with log + toast, not toast instead of log (`app/src/lib/labStore.svelte.ts:1021-1040`). |
| `lab.deleteDoc` RPC | call resolves | `Lab deleted — {labName}` | `info` | Stored-lab deletion is significant and irreversible. Change `deleteLab` to accept the display name (the caller already has the full document at `app/src/lib/components/LabBrowser.svelte:90-95`) so the toast need not expose an opaque id. |
| clone flow (`lab.saveDoc` inside `cloneLab`) | copy is persisted | `Lab copy created — {copyName}` | `success` | `cloneLab` creates a new id/name and persists it before returning the copy (`app/src/lib/labStore.svelte.ts:697-719`). Do not also emit a generic “lab opened” toast when the caller opens that copy, or the one user action will produce two confirmations. |
| `config.extract` RPC, one node | extraction resolves | `Config saved — {nodeName}` | `success` | The existing action applies the returned startup-config and then schedules autosave (`app/src/lib/labStore.svelte.ts:1492-1500`). This confirms the extraction, while the autosave remains silent. |
| `config.extract` RPC, all running IOL nodes | extraction resolves | `Configs saved — {targetCount} {nodeWord}` | `success` | The store already computes the exact running-IOL target list and handles the zero-target case as a warning log (`app/src/lib/labStore.svelte.ts:1502-1515`). Keep that zero-target log-only behavior; it is not a completed lifecycle action. |
| pushed `node.state` | `state === "crashed"` | `{nodeName} crashed` | `error` | A crash can occur after an action has returned or without a current click; the push is authoritative. The event only carries node id and state (`app/src/lib/protocol.ts:37-40`), so do not invent a cause. Detailed RPC failures continue to live in `lastError`/logs. Dedupe by `node-crashed:{id}`. |
| pushed `capture.started` | direct or automatic capture becomes live | `Capture started — link {linkId}` | `info` | The event supplies both link and allocated capture port (`app/src/lib/protocol.ts:57-60`), and the store already treats it as the source for `capturePorts` (`app/src/lib/labStore.svelte.ts:484-486`). Coalesce multiple starts during lab startup into `{count} captures started`. |
| pushed `capture.stopped` | capture actually stops | `Capture stopped — link {linkId}` | `info` | The store already clears the port only on this pushed event (`app/src/lib/labStore.svelte.ts:487-491`). Coalesce multiple full-lab-stop events into `{count} captures stopped`; if a `lab.stop` summary is pending/just emitted, suppress the capture-stopped aggregate because fabric/capture teardown is already part of that action (`supervisor/internal/server/handlers.go:436-445`). |
| `link.setFault` RPC | `fault !== null`, no delay | `Link {linkId} impairment active` | `warning` | Applying an operational fault is significant but expected. The existing action is an awaited guarded RPC (`app/src/lib/labStore.svelte.ts:1051-1069`); use amber, which is already the semantic transitional/warning color (`app/src/styles/theme.css:68-78`). |
| `link.setFault` RPC | `fault !== null`, `afterSec > 0` | `Link {linkId} impairment scheduled` | `info` | The RPC confirms scheduling, not activation. When the later pushed `link.fault` reports `active: true`, replace this keyed toast with `Link {linkId} impairment active` at `warning`; the event exposes `fault`, `active`, and `reason` (`app/src/lib/protocol.ts:91-94`). |
| `link.setFault` RPC or pushed `link.fault` | fault cleared/inactive outside full lab stop | `Link {linkId} restored` | `success` | This is a user-meaningful recovery. Suppress the server's automatic inactive replay during `lab.stop`, because that handler intentionally emits an inactive fault state for every persisted fault (`supervisor/internal/server/handlers.go:446-454`) and the lab-stop toast already explains the lifecycle transition. |

### Explicit non-mappings

- Do not toast `node.state="starting"|"running"|"stopped"` generically; use action-level summaries as described above. Keep `node.state="crashed"` because it is an unsolicited exceptional transition.
- Do not toast `node.console`, `node.pcState`, `link.stats`, or `host.stats`; these are state/data feeds, not user lifecycle confirmations (`app/src/lib/protocol.ts:41-47`, `app/src/lib/protocol.ts:69-105`).
- Do not toast `link.up`/`link.down` for every topology realization; the typed events exist (`app/src/lib/protocol.ts:49-56`) but the canvas is the appropriate continuous status surface.
- Do not turn arbitrary pushed `log` messages into toasts. The store intentionally retains a bounded 200-line log (`app/src/lib/labStore.svelte.ts:541-543`), and generic promotion would create duplicates and unpredictable noise.
- Do not toast routine autosaves, topology node/link add/remove, node-image changes, or app-start lab restore/load. Those operations are already directly visible in the edited surface, and startup toasts would read as user confirmations for actions the user did not take.

## State and component design

### Store additions in `app/src/lib/labStore.svelte.ts`

Add exported types near `LogLine`:

- `ToastSeverity = "success" | "info" | "warning" | "error"`.
- `ToastNotification = { id: string; key?: string; severity: ToastSeverity; message: string; createdAt: number; duration: number; count?: number; dismissing?: boolean }`.

Add `toasts = $state<ToastNotification[]>([])`, a private waiting queue, per-toast timeout maps, and these store methods:

- `enqueueToast(input)`: dedupe by `key`; update/restart the timer instead of adding a second copy; cap total visible + queued notifications at 20 by dropping the oldest non-error queued item first.
- `dismissToast(id)`: mark `dismissing`, wait `var(--transition-fast)`'s concrete 120 ms, then remove and promote the next queued item. Manual dismiss uses the same exit path.
- `pauseToast(id)` / `resumeToast(id)`: retain remaining time on pointer hover and keyboard focus, rather than letting a focused close button vanish.
- `resolveNodeName(id)`: current document name with fallback `Node {id}`. Snapshot the string before awaited destructive calls.
- A narrow coalescer for `capture.started` and `capture.stopped`: collect ids for 750 ms, then enqueue one toast (single-link template or aggregate count). Keep coalescing separate from general dedupe so unrelated success messages are never merged.

Show at most four toasts simultaneously; keep later entries queued and start their auto-dismiss clocks only when promoted to visible. New visible items go at the top, older items move downward. Use stable keyed iteration so updates do not recreate unrelated items.

Do not expose `SupervisorClient` to the toast component. Extend the existing `handleEvent` cases at `app/src/lib/labStore.svelte.ts:459-538` to enqueue crash/capture/fault notifications after updating authoritative state, and enqueue RPC-driven messages inside the existing action methods after successful awaits. The component only renders and calls `labStore.dismissToast/pauseToast/resumeToast`.

The current `guarded` helper clears `lastError` on success and catches failures into `lastError` plus logs (`app/src/lib/labStore.svelte.ts:1186-1195`). Either make it return `Promise<boolean>` or keep enqueue calls inside its successful callback. In both designs, never emit a success toast after `guarded` swallowed a failure. Preserve `lastError` behavior exactly.

### New component

Create `app/src/lib/components/ToastStack.svelte`.

Render:

- One fixed stack wrapper labeled `Notifications`, but not itself live.
- One keyed toast article per visible item: semantic status LED, message, and a 28 × 28 px dismiss button labeled `Dismiss notification`.
- Give each success/info/warning article polite status semantics and each error article assertive alert semantics. Keeping the semantics on sibling items avoids nested live regions. Do not move keyboard focus when a toast appears.

Mount `<ToastStack />` once in `app/src/App.svelte`, importing it with the other top-level components at `app/src/App.svelte:12-29`. Place it after the main shell and before modal/dialog conditionals (the overlay region begins after the shell at `app/src/App.svelte:350-387`). That makes it independent of canvas, console, inspector, and chrome auto-hide layout while keeping dialogs above it through the existing z-index scale.

## Exact visual and motion specification

The visual authority is the existing “Bench & Glass” system: hue-biased surfaces, a quiet single accent, and semantic status LEDs (`DESIGN.md:150-179`, `DESIGN.md:183-231`). Existing floating windows already use the combination of `var(--panel)`, `var(--blur)`, `var(--border)`, `var(--radius-md)`, and `var(--shadow-md)` (`app/src/lib/components/FloatingConsoleWindow.svelte:218-232`), while the shared theme defines spacing, radii, transition timings, and z-index bands (`app/src/styles/theme.css:37-66`). The toast should look like a compact instrument readout, not a generic rounded SaaS card.

### Geometry

- Stack: `position: fixed; top: calc(var(--topbar-h) + var(--sp-3)); right: var(--sp-3); z-index: var(--z-menu); width: min(360px, calc(100vw - 2 * var(--sp-3))); display: flex; flex-direction: column; gap: var(--sp-2); pointer-events: none;`.
- Starting below the 48 px top bar preserves the app's top-right fullscreen/menu/start controls; `--topbar-h` is 48 px and the spacing scale is 4/8/12/16/20/24/32 px (`app/src/styles/theme.css:37-55`). `--z-menu` is above ordinary floating windows but below dialogs and modals (`app/src/styles/theme.css:58-66`).
- Toast: minimum height 44 px, maximum width 360 px, padding `10px var(--sp-2) 10px var(--sp-3)`, `border-radius: var(--radius-md)`, grid columns `8px minmax(0, 1fr) 28px`, and `pointer-events: auto`. Use `gap: var(--sp-2)`. Long text wraps to at most three lines; do not truncate the lifecycle subject.
- At `max-width: 640px`, use `left: var(--sp-2); right: var(--sp-2); width: auto;` and keep the same top offset.

### Thirty-percent glass material

Add semantic toast surface tokens to the existing theme blocks rather than hard-coding theme selectors inside the component:

- Bench: `--toast-surface: rgba(16, 22, 31, 0.30);` — the RGB is the existing solid Bench panel `#10161f` (`app/src/styles/theme.css:84-92`).
- Glass: `--toast-surface: rgba(255, 255, 255, 0.30);` — the RGB is the existing solid Glass panel `#ffffff` (`app/src/styles/theme.css:168-175`).
- Shared: `--toast-blur: saturate(180%) blur(20px);` — exactly the existing Glass vibrancy recipe (`app/src/styles/theme.css:211-217`). This toast-specific token is required because the current Bench `--blur` is deliberately `none` (`app/src/styles/theme.css:127-134`), while the feature explicitly requires a frosted 30%-opaque material in both themes.

Apply `background: var(--toast-surface)`, both prefixed and unprefixed `backdrop-filter: var(--toast-blur)`, `border: 1px solid color-mix(in oklab, var(--toast-tone) 42%, var(--border))`, `box-shadow: var(--shadow-md)`, and `color: var(--ink)`. The only new colors are alpha forms of existing panel colors. Severity uses existing semantic tokens:

- `success` → `--success` / LED fill `--state-running`.
- `info` → text remains `--ink`; LED fill `--state-stopped` (neutral, does not spend the primary accent).
- `warning` → `--warning` / LED fill `--state-starting`.
- `error` → `--danger` / LED fill `--state-crashed`.

Those variables and light-theme contrast-safe ink overrides already exist (`app/src/styles/theme.css:68-78`, `app/src/styles/theme.css:237-251`). Render an 8 px round LED plus message text, and give the LED a subtle `0 0 0 3px color-mix(in oklab, var(--toast-tone) 20%, transparent)` halo. This follows the design rule that status is shape + color + label, never color alone (`DESIGN.md:201-231`). Use `var(--font-ui)` and `var(--fs-base)` for message chrome; node/lab names inside the message may use a nested `.mono` span because CLI-like data is intentionally monospace (`DESIGN.md:233-255`, `app/src/styles/theme.css:14-33`).

The dismiss button is transparent at rest, uses `var(--ink-3)`, and gains `var(--bg-hover)` plus `var(--ink)` on hover/focus. Rely on the global two-pixel accent focus ring (`app/src/styles/theme.css:320-324`).

### Stacking and timing

- Enter: 180 ms (`--transition-base`) from `translateX(12px) scale(0.98)` + opacity 0 to rest.
- Exit: 120 ms (`--transition-fast`) to `translateX(8px)` + opacity 0, then remove and promote the queue.
- Reflow: remaining cards transition `transform` for 180 ms; do not use spring/bounce motion.
- Maximum visible: 4. Queue cap: 20 total.
- Auto-dismiss: success 4,000 ms; info 4,500 ms; warning 6,000 ms; error 8,000 ms. Pause while any part of the toast is hovered or keyboard-focused; resume with remaining time.
- Keyed duplicates update in place and restart the timer. Capture bursts use the 750 ms aggregation window described above. Lab start/stop always produce one summary, never one toast per node.

## Theme and accessibility behavior

The app has two explicit themes, `bench` and `glass`, persisted to local storage and applied as `data-theme` on `<html>` (`app/src/lib/themeStore.svelte.ts:1-16`, `app/src/lib/themeStore.svelte.ts:19-45`). Because every toast color/surface references theme tokens, open toasts should update immediately when the user changes theme; no component-side theme branch is needed. Verify text contrast over representative busy and empty canvas areas in both themes. The current ink ramps are designed for WCAG AA on their normal grounds (`PRODUCT.md:96-101`), but the new 30%-opaque surface must be tested over real canvas content rather than assumed compliant.

Accessibility requirements:

- The fixed wrapper uses only `aria-label="Notifications"`. Each non-error toast uses `role="status" aria-live="polite"`; each error uses `role="alert" aria-live="assertive"`. Add `aria-atomic="true"` to every toast so a coalesced count update reads as a complete sentence, without nesting one live region inside another.
- Include a visible message and LED shape; severity must not be communicated by color alone, consistent with the app's status rule (`PRODUCT.md:83-85`, `PRODUCT.md:106-108`).
- New toasts never steal focus. The dismiss button is keyboard reachable, has `aria-label="Dismiss notification"`, and uses the existing global focus-visible treatment (`app/src/styles/theme.css:320-324`). If a focused toast auto-dismisses, pause it instead; if manually dismissed, return focus naturally to the document rather than forcing it elsewhere.
- Add `@media (prefers-reduced-motion: reduce)` that removes enter, exit, and reflow transforms/animations; opacity may change instantly. This matches existing component-level reduced-motion handling (`app/src/lib/components/TopBar.svelte:402-406`, `app/src/lib/components/FloatingConsoleWindow.svelte:337-342`) and the product-level requirement (`PRODUCT.md:103-105`).
- Do not announce routine autosaves. Frequent live-region chatter during drag/edit work would be distracting.

## Coexistence with existing notification-adjacent UI

Keep the top-bar error pill. `labStore.lastError` is documented as the last user-visible action failure (`app/src/lib/labStore.svelte.ts:81-84`), `guarded` writes it and the log on RPC failure (`app/src/lib/labStore.svelte.ts:1186-1195`), and `TopBar` renders it as a dismissible, truncated red pill (`app/src/lib/components/TopBar.svelte:243-251`, `app/src/lib/components/TopBar.svelte:365-378`). The robustness handoff identifies this exact red banner as an important node-start failure surface (`docs/robustness-handoff.md:8-16`, `docs/robustness-handoff.md:77-87`).

The toast system must **coexist with, not replace**, that pill:

- Persistent/detailed RPC failures remain in `lastError` and logs.
- Do not also enqueue the same full RPC error string as a toast.
- A partial lab start may enqueue the short red summary from the table while the banner retains detailed per-node causes.
- An unsolicited `node.state="crashed"` gets a red toast because there may be no failed RPC and therefore no banner.
- Successful actions continue clearing `lastError` through `guarded`; toast dismissal must never mutate `lastError`.

Also preserve the top-bar save status pill. It derives “Saving/Saved/Unsaved” from the autosave timer and `lastSavedAt` (`app/src/lib/components/TopBar.svelte:23-34`, `app/src/lib/components/TopBar.svelte:230-238`). Toasts confirm explicit saves only; the pill remains the continuous autosave/status indicator.

## Prioritized implementation tasks

1. **Add notification state and lifecycle helpers to `labStore.svelte.ts`.** Define types, visible/queued state, keyed dedupe, timers with hover/focus pause, 120 ms exit removal, max-four promotion, queue cap, node-name lookup, and the capture burst coalescer. Keep the existing single event subscription.
2. **Wire required RPC completions.** Add explicit-save versus autosave notification control; then wire node start/stop/restart, lab start/stop, whole/node wipe, and force-clean. Capture returned counts/results before forming messages. Ensure failures never fall through to success toasts.
3. **Wire additional high-value lifecycle actions.** Add lab delete/clone and config extraction confirmations, then direct/scheduled link-fault messages. Pass the lab display name into `deleteLab` from `LabBrowser` rather than displaying an id.
4. **Wire pushed exceptions and runtime events in the existing `handleEvent`.** Add crash, capture, and asynchronous fault handling after authoritative state updates. Add explicit suppression for full lab stop/start replay so bulk operations remain one-toast actions.
5. **Create and mount `ToastStack.svelte`.** Render the max-four keyed stack, status LEDs, wrap-safe copy, dismiss controls, live-region semantics, pause/resume handlers, and reduced-motion classes. Mount it once at the `App.svelte` root below the top bar and below dialog z-bands.
6. **Add theme tokens and styles.** Add the exact 30% Bench/Glass surfaces and shared 20 px vibrancy filter to `theme.css`; use only existing semantic ink, status, border, radius, spacing, transition, shadow, and z-index tokens elsewhere.
7. **Unit-level behavior verification.** If the repository's frontend test harness remains absent, extract pure queue/dedupe helpers into a small TypeScript module only if needed for deterministic tests; otherwise exercise through the mock transport. Verify: repeated same-key update, fifth-toast queueing/promotion, timer pause/resume, manual dismissal, queue cap preserving errors, node-id fallback, zero-node lab start, partial bulk start, and no autosave toast.
8. **Static checks.** From `app/`, run `npm run check` (the script is `svelte-check --tsconfig ./tsconfig.json`) and `npm run build` (`app/package.json:6-12`). Fix all new diagnostics; do not change the Go protocol for this feature.
9. **Bounded visual check in the dev server.** Run `npm run dev`; verify Bench and Glass, 1280 px desktop and ≤640 px width, top-bar/control clearance, four-item stack, long lab/node names, warning/error contrast, hover/focus timer pause, keyboard dismissal, theme switching while visible, and reduced-motion emulation. Trigger a five-node start/stop and multiple armed captures to confirm coalescing. Confirm the existing top-bar error pill and save-status pill remain usable and unobscured.

## Acceptance summary

The implementation is complete when every required action produces exactly one factual completion toast, bulk operations do not fan out into per-node noise, crashes remain visible even without an RPC failure, manual saves notify while autosaves stay quiet, both themes use a real 30%-opaque frosted material built from existing tokens, four notifications stack accessibly below the top bar, and the current persistent error/save indicators continue to serve their existing roles.
