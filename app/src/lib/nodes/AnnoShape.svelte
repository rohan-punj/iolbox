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
  const w = $derived((width as number) ?? 200);
  const h = $derived((height as number) ?? 120);

  let editing = $state(false);
  let draft = $state("");
  let inputEl: HTMLInputElement | undefined = $state();

  async function beginEdit() {
    draft = label ?? "";
    editing = true;
    await tick();
    inputEl?.focus();
    inputEl?.select();
  }

  function commit() {
    if (!editing) return;
    editing = false;
    const next = draft.trim();
    if (next !== (label ?? "")) {
      labStore.updateAnnotation(annoId, { label: next || undefined } as any);
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" || e.key === "Enter") {
      e.preventDefault();
      inputEl?.blur();
    }
  }

  function onDblClick(e: MouseEvent) {
    e.stopPropagation();
    beginEdit();
  }
</script>

<div
  class="anno-shape"
  class:selected
  style:width={`${w}px`}
  style:height={`${h}px`}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  <svg width={w} height={h} viewBox={`0 0 ${w} ${h}`} aria-hidden="true">
    {#if shape === "rect"}
      <rect
        x="1.25"
        y="1.25"
        width={w - 2.5}
        height={h - 2.5}
        rx="10"
        fill={color}
        fill-opacity="0.12"
        stroke={color}
        stroke-width="2.5"
      />
    {:else}
      <ellipse
        cx={w / 2}
        cy={h / 2}
        rx={w / 2 - 1.5}
        ry={h / 2 - 1.5}
        fill={color}
        fill-opacity="0.12"
        stroke={color}
        stroke-width="2.5"
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
      onblur={commit}
      onkeydown={onKeydown}
    />
  {:else if label}
    <span class="anno-shape-label">{label}</span>
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
</style>
