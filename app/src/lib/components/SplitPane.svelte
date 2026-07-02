<script lang="ts">
  // Minimal resizable-pane primitive: drag a divider to resize either a
  // "left" panel (horizontal split) or a "top" panel (vertical split).
  import type { Snippet } from "svelte";

  let {
    direction = "horizontal",
    size = $bindable(260),
    min = 160,
    max = 520,
    edge = "start",
    children,
  }: {
    direction?: "horizontal" | "vertical";
    size?: number;
    min?: number;
    max?: number;
    edge?: "start" | "end";
    children: Snippet;
  } = $props();

  let dragging = $state(false);

  function onPointerDown(e: PointerEvent) {
    dragging = true;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }
  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    const wrapper = (e.currentTarget as HTMLElement).parentElement;
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
  function onPointerUp() {
    dragging = false;
  }
</script>

<div
  class="pane"
  class:horizontal={direction === "horizontal"}
  class:vertical={direction === "vertical"}
  style:flex-basis={`${size}px`}
>
  {@render children()}
</div>
<div
  class="divider"
  class:horizontal={direction === "horizontal"}
  class:vertical={direction === "vertical"}
  class:dragging
  role="separator"
  aria-orientation={direction === "horizontal" ? "vertical" : "horizontal"}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
></div>

<style>
  .pane {
    flex-shrink: 0;
    overflow: hidden;
    min-width: 0;
    min-height: 0;
  }
  .divider {
    flex-shrink: 0;
    background: transparent;
    position: relative;
    z-index: 5;
  }
  .divider.horizontal {
    width: 5px;
    cursor: col-resize;
    margin: 0 -2px;
  }
  .divider.vertical {
    height: 5px;
    cursor: row-resize;
    margin: -2px 0;
  }
  .divider::after {
    content: "";
    position: absolute;
    inset: 0;
    margin: auto;
    background: var(--border);
    transition: background var(--transition-fast);
  }
  .divider.horizontal::after {
    width: 1px;
    height: 100%;
  }
  .divider.vertical::after {
    height: 1px;
    width: 100%;
  }
  .divider:hover::after,
  .divider.dragging::after {
    background: var(--accent);
  }
  .divider.horizontal {
    width: 5px;
  }
</style>
