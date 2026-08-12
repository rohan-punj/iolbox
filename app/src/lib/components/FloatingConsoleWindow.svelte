<script lang="ts">
  import { beginDrag, type DragHandle } from "../dragMove";
  import { labStore } from "../labStore.svelte";
  import {
    clampGeom,
    consoleUiStore,
    paneKey,
    type PaneRef,
    type Viewport,
  } from "../consoleUiStore.svelte";
  import { captureTitle, nodeName } from "../paneLabels";
  import PaneBody from "./PaneBody.svelte";

  let {
    ref,
    labId,
    viewport,
    z,
  }: { ref: PaneRef; labId: string; viewport: Viewport; z: number | string } = $props();

  const key = $derived(paneKey(ref));
  const geom = $derived(consoleUiStore.windows[key]);
  const minimized = $derived(consoleUiStore.minimized.includes(key));
  const topmost = $derived(consoleUiStore.windowOrder.at(-1) === key);
  const title = $derived(
    ref.kind === "console"
      ? nodeName(ref.node)
      : ref.kind === "capture"
        ? captureTitle(ref.link)
        : `Lens · ${captureTitle(ref.link)}`
  );
  const stateNodeId = $derived(
    ref.kind === "console"
      ? ref.node
      : labStore.lab.links.find((link) => link.id === ref.link)?.endpoints[0]?.node ?? null
  );
  const state = $derived(stateNodeId === null ? "stopped" : labStore.nodeStates[stateNodeId] ?? "stopped");

  const PIN =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="m5 2 6 6"/><path d="m4 5 7 7"/><path d="m3 13 4-4"/><path d="M8 2h5l-2 3 1 3-3 1-3-3 1-3z"/></svg>';
  const MINIMIZE =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M3 12.5h10"/></svg>';
  const CLOSE =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="m4 4 8 8M12 4l-8 8"/></svg>';

  let moveDrag: DragHandle | null = null;
  let sizeDrag: DragHandle | null = null;

  function focusWindow() {
    consoleUiStore.raiseWindow(key);
    if (ref.kind !== "console" || consoleUiStore.searchOpenFor !== ref.node) {
      consoleUiStore.setSearchOpenFor(null);
    }
    consoleUiStore.setFocused(ref);
  }

  function onTitleDown(event: PointerEvent) {
    const start = consoleUiStore.windows[key];
    if (!start) return;
    focusWindow();
    moveDrag = beginDrag(event, {
      start: { x: start.x, y: start.y },
      clamp: (x, y) => clampGeom({ ...start, x, y }, viewport),
      onMove: (x, y) => consoleUiStore.moveWindow(key, x, y),
      onEnd: () => consoleUiStore.commitWindow(labId, key),
    });
  }

  function onTitleMove(event: PointerEvent) {
    moveDrag?.move(event);
  }

  function onTitleEnd(event: PointerEvent) {
    moveDrag?.end(event);
    moveDrag = null;
  }

  function onGripDown(event: PointerEvent) {
    const start = consoleUiStore.windows[key];
    if (!start) return;
    focusWindow();
    sizeDrag = beginDrag(event, {
      start: { x: start.w, y: start.h },
      clamp: (w, h) => {
        const next = clampGeom({ ...start, w, h }, viewport);
        return { x: next.w, y: next.h };
      },
      onMove: (w, h) => {
        const next = clampGeom({ ...start, w, h }, viewport);
        consoleUiStore.resizeWindow(key, next.w, next.h);
        consoleUiStore.moveWindow(key, next.x, next.y);
      },
      onEnd: () => consoleUiStore.commitWindow(labId, key),
    });
  }

  function onGripMove(event: PointerEvent) {
    sizeDrag?.move(event);
  }

  function onGripEnd(event: PointerEvent) {
    sizeDrag?.end(event);
    sizeDrag = null;
  }

  function onGripKeydown(event: KeyboardEvent) {
    const current = consoleUiStore.windows[key];
    if (!current) return;
    const step = event.shiftKey ? 32 : 8;
    const delta = event.key === "ArrowRight" || event.key === "ArrowDown" ? step : event.key === "ArrowLeft" || event.key === "ArrowUp" ? -step : 0;
    if (!delta) return;
    event.preventDefault();
    const next = clampGeom(
      { ...current, w: current.w + (event.key === "ArrowLeft" || event.key === "ArrowRight" ? delta : 0), h: current.h + (event.key === "ArrowUp" || event.key === "ArrowDown" ? delta : 0) },
      viewport
    );
    consoleUiStore.resizeWindow(key, next.w, next.h);
    consoleUiStore.moveWindow(key, next.x, next.y);
    consoleUiStore.commitWindow(labId, key);
  }
</script>

<div
  class="float-win"
  role="dialog"
  aria-label={title}
  tabindex="-1"
  class:minimized
  style:left={`${geom?.x ?? 0}px`}
  style:top={`${geom?.y ?? viewport.topbarH}px`}
  style:width={`${geom?.w ?? 0}px`}
  style:height={`${geom?.h ?? 0}px`}
  style:z-index={z}
  onpointerdown={focusWindow}
>
  <div
    class="float-title"
    role="toolbar"
    tabindex="-1"
    aria-label={`${title} window controls`}
    onpointerdown={onTitleDown}
    onpointermove={onTitleMove}
    onpointerup={onTitleEnd}
    onpointercancel={onTitleEnd}
  >
    <span class="state-led" class:running={state === "running"} class:starting={state === "starting"} class:crashed={state === "crashed"}></span>
    <span class="float-title-text">{title}</span>
    <span class="float-spacer"></span>
    <button
      class="title-btn"
      class:on={consoleUiStore.isWindowPinned(key)}
      aria-label={consoleUiStore.isWindowPinned(key) ? "Unpin window" : "Pin window"}
      aria-pressed={consoleUiStore.isWindowPinned(key)}
      title={consoleUiStore.isWindowPinned(key) ? "Unpin window" : "Keep window above unpinned windows"}
      onpointerdown={(event) => event.stopPropagation()}
      onclick={() => consoleUiStore.togglePinnedWindow(key)}
    >
      {@html PIN}
    </button>
    <button
      class="title-btn"
      aria-label="Minimize window"
      title="Minimize window"
      onpointerdown={(event) => event.stopPropagation()}
      onclick={() => consoleUiStore.toggleMinimized(key)}
    >
      {@html MINIMIZE}
    </button>
    <button
      class="title-btn close"
      aria-label={`Close ${title}`}
      title={`Close ${title}`}
      onpointerdown={(event) => event.stopPropagation()}
      onclick={() => consoleUiStore.closePane(ref)}
    >
      {@html CLOSE}
    </button>
  </div>

  <div class="pane-content" class:minimized>
    <PaneBody {ref} visible={!minimized} focused={topmost} />
  </div>
  <div
    class="float-grip"
    role="button"
    aria-label={`Resize ${title}`}
    tabindex="0"
    onkeydown={onGripKeydown}
    onpointerdown={onGripDown}
    onpointermove={onGripMove}
    onpointerup={onGripEnd}
    onpointercancel={onGripEnd}
  ></div>
</div>

<style>
  .float-win {
    position: fixed;
    display: flex;
    flex-direction: column;
    min-width: var(--win-min-w, 320px);
    min-height: var(--win-min-h, 160px);
    overflow: hidden;
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
  }
  .float-win.minimized {
    display: none;
  }
  .float-title {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 28px;
    padding: 0 var(--sp-2);
    color: var(--text-primary);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border-subtle);
    touch-action: none;
    user-select: none;
    cursor: grab;
  }
  .float-title:active {
    cursor: grabbing;
  }
  .state-led {
    width: 7px;
    height: 7px;
    flex: 0 0 auto;
    border-radius: 50%;
    background: var(--state-stopped);
  }
  .state-led.running {
    background: var(--state-running);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-running) 22%, transparent);
  }
  .state-led.starting {
    background: var(--state-starting);
  }
  .state-led.crashed {
    background: var(--state-crashed);
  }
  .float-title-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 600 var(--fs-xs)/1 var(--font-mono);
  }
  .float-spacer {
    flex: 1;
  }
  .title-btn {
    all: unset;
    display: grid;
    width: 24px;
    height: 24px;
    place-items: center;
    color: var(--text-tertiary);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .title-btn:hover,
  .title-btn:focus-visible,
  .title-btn.on {
    color: var(--accent);
    background: var(--bg-hover);
  }
  .title-btn.close:hover {
    color: var(--state-crashed);
  }
  .pane-content {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .pane-content.minimized {
    display: none;
  }
  .float-grip {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 14px;
    height: 14px;
    cursor: nwse-resize;
    touch-action: none;
  }
  .float-grip:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  @media (prefers-reduced-motion: reduce) {
    .float-title,
    .title-btn {
      transition: none;
    }
  }
</style>
