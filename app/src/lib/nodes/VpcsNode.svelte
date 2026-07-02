<script lang="ts">
  import { Handle, Position, type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { stateLabel } from "../nodeVisuals";
  import { iconSvg, defaultIconFor, iconRegistryVersion, isArtworkIcon, uiSvg } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import NodeActions from "./NodeActions.svelte";

  let { id, data, selected }: NodeProps = $props();

  const nodeId = $derived(Number(id));
  const state = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  const isDropTarget = $derived(linking.dropTargetId === nodeId);
  const isLinkSource = $derived(linking.sourceId === nodeId);

  function onConnectorDown(ev: PointerEvent) {
    ev.preventDefault();
    ev.stopPropagation();
    linking.start?.(nodeId, ev);
  }
  function onDblClick(ev: MouseEvent) {
    ev.stopPropagation();
    linking.requestEdit?.(nodeId);
  }
  const label = $derived((data as any).label as string);
  const iconKey = $derived(
    ((data as any).icon as string | undefined) ?? defaultIconFor("vpcs")
  );
  // Artwork icons carry their own plate and render full-bleed, PNetLab-style.
  const artwork = $derived((iconRegistryVersion(), isArtworkIcon(iconKey)));
  const glyph = $derived((iconRegistryVersion(), iconSvg(iconKey, artwork ? 58 : 28)));
</script>

<div
  class="node face-node vpcs"
  class:selected
  class:artwork
  class:drop-target={isDropTarget}
  class:linking={isLinkSource}
  data-state={state}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  <Handle type="source" position={Position.Top} id="top" />
  <Handle type="source" position={Position.Right} id="right" />
  <Handle type="source" position={Position.Bottom} id="bottom" />
  <Handle type="source" position={Position.Left} id="left" />

  <div class="face">
    <NodeActions {nodeId} {state} />
    <span class="glyph" aria-hidden="true">{@html glyph}</span>
    <button
      class="connector nodrag"
      title="Drag to another node to connect"
      aria-label="Connect this node"
      onpointerdown={onConnectorDown}
    >{@html uiSvg("link", 12)}</button>
  </div>
  <div class="name mono"><span class="led" title={stateLabel(state)}></span>{label}</div>
</div>

<style>
  .node {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    width: 64px;
  }
  .face {
    position: relative;
    width: 64px;
    height: 64px;
    border-radius: 14px;
    background: linear-gradient(160deg, var(--node-face), var(--node-face-2));
    border: 1px solid var(--border-strong);
    display: grid;
    place-items: center;
    box-shadow: var(--shadow-md);
    color: var(--node-vpcs);
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .node.selected .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 26%, transparent), var(--shadow-md);
  }
  .node.selected .glyph {
    color: var(--accent);
  }
  /* R2.1 — link-add connector affordance (hover/focus only). */
  .connector {
    all: unset;
    position: absolute;
    top: -6px;
    right: -6px;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-ink);
    display: grid;
    place-items: center;
    cursor: crosshair;
    opacity: 0;
    transform: scale(0.6);
    transition: opacity var(--transition-fast), transform var(--transition-fast);
    box-shadow: var(--shadow-md);
    z-index: 6;
  }
  .face:hover .connector,
  .connector:focus-visible,
  .node.linking .connector {
    opacity: 1;
    transform: scale(1);
  }
  .connector :global(svg) {
    width: 12px;
    height: 12px;
    pointer-events: none;
  }
  .node.drop-target .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 4px color-mix(in oklab, var(--accent) 34%, transparent), var(--shadow-md);
  }
  .glyph {
    display: grid;
    place-items: center;
    color: inherit;
  }
  .glyph :global(svg),
  .glyph :global(img) {
    width: 28px;
    height: 28px;
  }
  /* Artwork icons ARE the node: hide the tile, let the icon fill the face.
     Selection/drop rings are re-asserted below so they survive this reset. */
  .node.artwork .face {
    background: none;
    border-color: transparent;
    box-shadow: none;
  }
  .node.artwork .glyph :global(svg),
  .node.artwork .glyph :global(img) {
    width: 58px;
    height: 58px;
  }
  .node.artwork.selected .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 26%, transparent);
  }
  .node.artwork.drop-target .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 4px color-mix(in oklab, var(--accent) 34%, transparent);
  }
  /* Status LED — now an inline dot at the head of the name chip (was a
     face-corner dot). Same state colors + pulse; the face corner is left to the
     connector button alone. */
  .led {
    flex-shrink: 0;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--state-stopped);
  }
  .node[data-state="running"] .led {
    background: var(--state-running);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--state-running) 24%, transparent);
  }
  .node[data-state="starting"] .led {
    background: var(--state-starting);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--state-starting) 24%, transparent);
    animation: led-pulse 1.1s ease-in-out infinite;
  }
  .node[data-state="crashed"] .led {
    background: var(--state-crashed);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--state-crashed) 24%, transparent);
  }
  @keyframes led-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  .name {
    /* Readability — up ~1px and a touch more opaque backing vs the dot grid. */
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: calc(var(--fs-xs) + 1px);
    color: var(--ink);
    background: color-mix(in oklab, var(--chip-bg) 92%, var(--ground));
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 1px 7px;
    letter-spacing: 0.02em;
    max-width: 108px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  :global(.face-node .svelte-flow__handle) {
    width: 9px;
    height: 9px;
    background: var(--border-strong);
    border: 1.5px solid var(--ground);
    opacity: 0;
    transition: opacity var(--transition-fast);
  }
  :global(.face-node:hover .svelte-flow__handle) {
    opacity: 1;
    background: var(--accent);
  }
</style>
