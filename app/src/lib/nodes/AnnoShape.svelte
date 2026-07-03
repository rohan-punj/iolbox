<script lang="ts">
  import { type NodeProps } from "@xyflow/svelte";
  import { tick } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { annoTool } from "../annoTool.svelte";

  let { id, data, width, height, selected }: NodeProps = $props();

  const annoId = $derived((data as any).annoId as string);
  const shape = $derived((data as any).shape as "rect" | "ellipse");
  const label = $derived((data as any).label as string | undefined);
  const color = $derived(((data as any).color as string | undefined) ?? "var(--accent)");
  const border = $derived(((data as any).border as number | undefined) ?? 2.5);
  const fillOpacity = $derived(((data as any).fillOpacity as number | undefined) ?? 0.12);
  const w = $derived((width as number) ?? 200);
  const h = $derived((height as number) ?? 120);

  // Inline label editing (context-menu "Edit label…" sets editRequestId).
  let editing = $state(false);
  let draft = $state("");
  let inputEl: HTMLInputElement | undefined = $state();
  $effect(() => {
    if (annoTool.editRequestId === annoId) {
      annoTool.editRequestId = null;
      void beginEdit();
    }
  });
  async function beginEdit() {
    draft = label ?? "";
    editing = true;
    await tick();
    inputEl?.focus();
    inputEl?.select();
  }
  function commitLabel() {
    if (!editing) return;
    editing = false;
    const next = draft.trim();
    if (next !== (label ?? "")) {
      labStore.updateAnnotation(annoId, { label: next || undefined } as any);
    }
  }
  function onLabelKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" || e.key === "Enter") {
      e.preventDefault();
      inputEl?.blur();
    }
  }

  // Double-click opens the compact style popover (color / border / fill opacity
  // / delete).
  function onDblClick(e: MouseEvent) {
    e.stopPropagation();
    annoTool.requestStyle?.(annoId, e.clientX, e.clientY, false);
  }

  // --- resize grip (bottom-right). Drag to resize w/h; min 60x40; autosaves on
  // release. `nodrag` so it never moves the node while resizing. ---
  const MIN_W = 60;
  const MIN_H = 40;
  let resizing = $state(false);
  let start = { px: 0, py: 0, w: 0, h: 0 };
  // Live overrides applied during a drag (the doc is written only on release).
  let liveW = $state<number | null>(null);
  let liveH = $state<number | null>(null);
  const dispW = $derived(liveW ?? w);
  const dispH = $derived(liveH ?? h);

  function onGripDown(e: PointerEvent) {
    e.preventDefault();
    e.stopPropagation();
    resizing = true;
    start = { px: e.clientX, py: e.clientY, w, h };
    liveW = w;
    liveH = h;
    window.addEventListener("pointermove", onGripMove);
    window.addEventListener("pointerup", onGripUp);
  }
  function onGripMove(e: PointerEvent) {
    if (!resizing) return;
    // Screen deltas are converted to flow deltas via the live viewport zoom so
    // resizing tracks the cursor 1:1 regardless of zoom.
    const zoom = labStore.canvasZoom || 1;
    liveW = Math.max(MIN_W, start.w + (e.clientX - start.px) / zoom);
    liveH = Math.max(MIN_H, start.h + (e.clientY - start.py) / zoom);
  }
  function onGripUp() {
    window.removeEventListener("pointermove", onGripMove);
    window.removeEventListener("pointerup", onGripUp);
    if (resizing && liveW !== null && liveH !== null) {
      labStore.updateAnnotation(annoId, { w: Math.round(liveW), h: Math.round(liveH) } as any);
    }
    resizing = false;
    liveW = null;
    liveH = null;
  }
</script>

<div
  class="anno-shape"
  class:selected
  style:width={`${dispW}px`}
  style:height={`${dispH}px`}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  <svg width={dispW} height={dispH} viewBox={`0 0 ${dispW} ${dispH}`} aria-hidden="true">
    {#if shape === "rect"}
      <rect
        x={border / 2}
        y={border / 2}
        width={dispW - border}
        height={dispH - border}
        rx="10"
        fill={color}
        fill-opacity={fillOpacity}
        stroke={color}
        stroke-width={border}
      />
    {:else}
      <ellipse
        cx={dispW / 2}
        cy={dispH / 2}
        rx={dispW / 2 - border / 2}
        ry={dispH / 2 - border / 2}
        fill={color}
        fill-opacity={fillOpacity}
        stroke={color}
        stroke-width={border}
      />
    {/if}
  </svg>
  {#if editing}
    <input
      bind:this={inputEl}
      bind:value={draft}
      class="anno-shape-input nodrag"
      spellcheck="false"
      placeholder="label"
      onblur={commitLabel}
      onkeydown={onLabelKeydown}
    />
  {:else if label}
    <span class="anno-shape-label">{label}</span>
  {/if}
  {#if selected && !editing}
    <button
      class="resize-grip nodrag"
      title="Drag to resize"
      aria-label="Resize"
      onpointerdown={onGripDown}
    ></button>
  {/if}
</div>

<style>
  .anno-shape {
    position: relative;
    display: grid;
    place-items: center;
  }
  .anno-shape svg {
    position: absolute;
    inset: 0;
    display: block;
  }
  .anno-shape.selected svg :global(rect),
  .anno-shape.selected svg :global(ellipse) {
    stroke-dasharray: 6 4;
  }
  .anno-shape.selected::after {
    content: "";
    position: absolute;
    inset: -3px;
    border-radius: var(--radius-md);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--accent) 40%, transparent);
    pointer-events: none;
  }
  .anno-shape-label {
    position: relative;
    z-index: 1;
    max-width: 90%;
    padding: 1px 6px;
    font-size: var(--fs-sm);
    font-weight: 600;
    color: var(--ink);
    text-align: center;
    background: color-mix(in oklab, var(--chip-bg) 90%, var(--ground));
    border-radius: var(--radius-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .anno-shape-input {
    position: relative;
    z-index: 1;
    width: 80%;
    text-align: center;
    background: var(--panel-solid);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    font-size: var(--fs-sm);
    padding: 3px 6px;
  }
  /* Bottom-right resize grip, only while selected. */
  .resize-grip {
    all: unset;
    position: absolute;
    right: -6px;
    bottom: -6px;
    width: 14px;
    height: 14px;
    border-radius: 3px;
    background: var(--accent);
    border: 2px solid var(--ground);
    box-shadow: var(--shadow-sm);
    cursor: nwse-resize;
    z-index: 2;
  }
</style>
