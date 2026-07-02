<script lang="ts">
  import { Handle, Position, type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { stateColor, stateLabel } from "../nodeVisuals";

  let { id, data, selected }: NodeProps = $props();

  const nodeId = $derived(Number(id));
  const state = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  const label = $derived((data as any).label as string);
</script>

<div class="vpcs-node" class:selected style:--state-color={stateColor(state)}>
  <Handle type="source" position={Position.Top} id="top" />
  <Handle type="source" position={Position.Right} id="right" />
  <Handle type="source" position={Position.Bottom} id="bottom" />
  <Handle type="source" position={Position.Left} id="left" />

  <div class="icon" aria-hidden="true">
    <svg viewBox="0 0 24 24" width="20" height="20">
      <rect x="3" y="4" width="18" height="12" rx="1.5" fill="currentColor" opacity="0.15" />
      <rect x="3" y="4" width="18" height="12" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
      <path d="M9 20h6M12 16v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
    </svg>
  </div>

  <div class="body">
    <div class="name">{label}</div>
    <div class="meta">VPCS</div>
  </div>

  <div class="state-dot" title={stateLabel(state)}></div>
</div>

<style>
  .vpcs-node {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 128px;
    padding: 8px 10px;
    background: var(--bg-2);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-sm);
    color: var(--node-vpcs);
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .vpcs-node.selected {
    border-color: var(--accent);
    box-shadow: var(--shadow-ring);
  }
  .icon {
    display: flex;
    flex-shrink: 0;
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
  }
  .state-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--state-color);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-color) 25%, transparent);
    flex-shrink: 0;
  }
  :global(.vpcs-node .svelte-flow__handle) {
    width: 8px;
    height: 8px;
    background: var(--border-strong);
    border: 1.5px solid var(--bg-0);
  }
  :global(.vpcs-node:hover .svelte-flow__handle) {
    background: var(--accent);
  }
</style>
