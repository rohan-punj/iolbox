<script lang="ts">
  // Minimal resizable-pane primitive: drag a divider to resize either a
  // "left" panel (horizontal split) or a "top" panel (vertical split).
  import type { Snippet } from "svelte";
  import { chromeStore } from "../chromeStore.svelte";

  let {
    direction = "horizontal",
    size = $bindable(260),
    min = 160,
    max = 520,
    edge = "start",
    storageKey,
    children,
  }: {
    direction?: "horizontal" | "vertical";
    size?: number;
    min?: number;
    max?: number;
    edge?: "start" | "end";
    /** When set, the chosen size persists to localStorage under this key. */
    storageKey?: string;
    children: Snippet;
  } = $props();

  // Restore a persisted size on mount (before first paint) so the pane opens at
  // the width/height the user last dragged it to. Intentionally a one-time read
  // of the initial prop values.
  // svelte-ignore state_referenced_locally
  if (storageKey) {
    try {
      const saved = Number(localStorage.getItem(storageKey));
      if (Number.isFinite(saved) && saved > 0) {
        size = Math.min(max, Math.max(min, saved));
      }
    } catch {
      /* localStorage may be unavailable (private mode) */
    }
  }

  let dragging = $state(false);
  let dividerEl: HTMLElement | undefined = $state();
  $effect(() => {
    if (dragging) return chromeStore.hold();
  });

  // Item 4 — the divider is the resize handle. The prior version measured the
  // pane against `e.currentTarget.parentElement` (the divider's parent = the
  // flex row/column that holds pane+divider). That is correct, but the divider
  // was ALWAYS rendered AFTER the pane; for an `edge="end"` dock (console at the
  // bottom / right) the handle ended up on the far outer edge of the window,
  // past the pane, so a drag on the *inner* seam between canvas and console
  // never landed on it. We now render the divider on the correct side of the
  // pane (before it for edge="end") AND resolve the wrapper via the divider's
  // own parent so the math is stable regardless of which side we captured on.
  function onPointerDown(e: PointerEvent) {
    dragging = true;
    dividerEl?.setPointerCapture(e.pointerId);
    e.preventDefault();
  }
  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    const wrapper = dividerEl?.parentElement;
    if (!wrapper) return;
    const rect = wrapper.getBoundingClientRect();
    let next: number;
    if (direction === "horizontal") {
      next = edge === "start" ? e.clientX - rect.left : rect.right - e.clientX;
    } else {
      next = edge === "start" ? e.clientY - rect.top : rect.bottom - e.clientY;
    }
    size = Math.min(max, Math.max(min, next));
  }
  function onPointerUp(e: PointerEvent) {
    if (!dragging) return;
    dragging = false;
    dividerEl?.releasePointerCapture?.(e.pointerId);
    if (storageKey) {
      try {
        localStorage.setItem(storageKey, String(Math.round(size)));
      } catch {
        /* ignore persistence failure */
      }
    }
  }
</script>

{#snippet handle()}
  <div
    class="divider"
    class:horizontal={direction === "horizontal"}
    class:vertical={direction === "vertical"}
    class:dragging
    bind:this={dividerEl}
    role="separator"
    aria-orientation={direction === "horizontal" ? "vertical" : "horizontal"}
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
  ></div>
{/snippet}

<!-- edge="end" docks (console bottom/right) put the handle on the INNER seam,
     i.e. BEFORE the pane in flow order; edge="start" docks (palette) put it
     after. -->
{#if edge === "end"}
  {@render handle()}
{/if}
<div
  class="pane"
  class:horizontal={direction === "horizontal"}
  class:vertical={direction === "vertical"}
  style:flex-basis={`${size}px`}
>
  {@render children()}
</div>
{#if edge === "start"}
  {@render handle()}
{/if}

<style>
  .pane {
    flex-shrink: 0;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
  }
  /* Item 4 — a real, grabbable handle. ≥6px hit area with a col/row-resize
     cursor. The thin visible rule lives in ::after; the whole .divider is the
     hit target, and its z-index sits above adjacent panes so the console's
     xterm/term-area can't swallow the pointerdown. */
  .divider {
    flex-shrink: 0;
    background: transparent;
    position: relative;
    z-index: 20;
    touch-action: none;
  }
  .divider.horizontal {
    width: 8px;
    cursor: col-resize;
    margin: 0 -4px;
  }
  .divider.vertical {
    height: 8px;
    cursor: row-resize;
    margin: -4px 0;
  }
  .divider::after {
    content: "";
    position: absolute;
    inset: 0;
    margin: auto;
    background: var(--border);
    border-radius: 2px;
    transition: background var(--transition-fast), transform var(--transition-fast);
  }
  .divider.horizontal::after {
    width: 3px;
    height: 100%;
    transform: scaleX(0.3333);
  }
  .divider.vertical::after {
    height: 3px;
    width: 100%;
    transform: scaleY(0.3333);
  }
  /* Grabbable affordance: the thin rule fattens + tints accent on hover/drag so
     the handle is discoverable, not invisible. Base rule paints the rule at
     1px via a 3px box scaled down (transform, not width/height, so this
     doesn't trigger layout on hover); hover/drag un-scales it to the full 3px. */
  .divider:hover::after,
  .divider.dragging::after {
    background: var(--accent);
    transform: none;
  }
</style>
