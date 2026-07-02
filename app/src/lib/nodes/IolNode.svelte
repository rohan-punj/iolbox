<script lang="ts">
  import { Handle, Position, type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { stateColor, stateLabel } from "../nodeVisuals";

  let { id, data, selected }: NodeProps = $props();

  const nodeId = $derived(Number(id));
  const state = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  const isL2 = $derived((data as any).imageClass === "l2");
  const label = $derived((data as any).label as string);
  const imageLabel = $derived((data as any).imageLabel as string | undefined);
</script>

<div
  class="iol-node"
  class:selected
  class:l2={isL2}
  style:--state-color={stateColor(state)}
>
  <Handle type="source" position={Position.Top} id="top" />
  <Handle type="source" position={Position.Right} id="right" />
  <Handle type="source" position={Position.Bottom} id="bottom" />
  <Handle type="source" position={Position.Left} id="left" />

  <div class="icon" aria-hidden="true">
    {#if isL2}
      <!-- switch glyph -->
      <svg viewBox="0 0 24 24" width="22" height="22">
        <rect x="2" y="8" width="20" height="8" rx="1.5" fill="currentColor" opacity="0.15" />
        <rect x="2" y="8" width="20" height="8" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
        <path d="M6 8V5M10 8V5M14 8V5M18 8V5M6 16v3M10 16v3M14 16v3M18 16v3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
    {:else}
      <!-- router glyph -->
      <svg viewBox="0 0 24 24" width="22" height="22">
        <circle cx="12" cy="12" r="9" fill="currentColor" opacity="0.15" />
        <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.5" />
        <path d="M7 12h10M12 7v10M8.5 8.5l7 7M15.5 8.5l-7 7" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" />
      </svg>
    {/if}
  </div>

  <div class="body">
    <div class="name">{label}</div>
    <div class="meta">{imageLabel ?? "no image"}</div>
  </div>

  <div class="state-dot" title={stateLabel(state)}></div>
</div>

<style>
  .iol-node {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 148px;
    padding: 8px 10px;
    background: var(--bg-2);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-sm);
    color: var(--node-iol-l3);
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .iol-node.l2 {
    color: var(--node-iol-l2);
  }
  .iol-node.selected {
    border-color: var(--accent);
    box-shadow: var(--shadow-ring);
  }
  .icon {
    display: flex;
    flex-shrink: 0;
    color: inherit;
  }
  .body {
    min-width: 0;
    flex: 1;
  }
  .name {
    font-size: var(--fs-sm);
    font-weight: 600;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .meta {
    font-size: 10px;
    color: var(--text-tertiary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .state-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--state-color);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-color) 25%, transparent);
    flex-shrink: 0;
  }
  :global(.iol-node .svelte-flow__handle) {
    width: 8px;
    height: 8px;
    background: var(--border-strong);
    border: 1.5px solid var(--bg-0);
  }
  :global(.iol-node:hover .svelte-flow__handle) {
    background: var(--accent);
  }
</style>
