<script lang="ts">
  import { Handle, Position, type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { stateLabel } from "../nodeVisuals";
  import { iconSvg, defaultIconFor, iconRegistryVersion, isArtworkIcon, uiSvg } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import { painterStore } from "../painterStore.svelte";
  import NodeActions from "./NodeActions.svelte";

  let { id, data, selected }: NodeProps = $props();

  const nodeId = $derived(Number(id));
  // WS5b — crown this node when an STP snapshot marks it the root bridge.
  const isStpRoot = $derived(painterStore.stpRootNodeIds.includes(nodeId));
  const state = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  // WS1: per-node action lock drives a progress overlay on the face.
  const isLocked = $derived(labStore.nodeLocks[nodeId] != null);
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
  const imageClass = $derived((data as any).imageClass as string);
  const isL2 = $derived(imageClass === "l2");
  const label = $derived((data as any).label as string);
  const iconKey = $derived(
    ((data as any).icon as string | undefined) ?? defaultIconFor("iol", imageClass)
  );
  // Re-render when a custom icon is imported/selected. Artwork icons carry
  // their own plate and render full-bleed (no tile chrome), PNetLab-style.
  const artwork = $derived((iconRegistryVersion(), isArtworkIcon(iconKey)));
  const glyph = $derived((iconRegistryVersion(), iconSvg(iconKey, artwork ? 58 : 30)));
</script>

<div
  class="node face-node"
  class:selected
  class:artwork
  class:l2={isL2}
  class:drop-target={isDropTarget}
  class:linking={isLinkSource}
  class:locked={isLocked}
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
    {#if isStpRoot}
      <span class="stp-crown" title="STP root bridge" aria-label="STP root bridge">👑</span>
    {/if}
    {#if isLocked}
      <span class="lock-overlay" aria-hidden="true"><span class="lock-ring"></span></span>
    {/if}
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
    color: var(--ink);
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .node.l2 .face {
    color: var(--node-iol-l2);
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
  /* R2.1 — accent ring while this node is the hovered drop target. */
  .node.drop-target .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 4px color-mix(in oklab, var(--accent) 34%, transparent), var(--shadow-md);
  }
  .glyph {
    display: grid;
    place-items: center;
    color: inherit;
  }
  /* WS1 — action-lock progress overlay over the face: a dimming scrim + a
     spinning ring in the --state-starting colour. Works on both the tiled and
     artwork faces (it covers the whole face box). */
  .lock-overlay {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    display: grid;
    place-items: center;
    background: color-mix(in oklab, var(--node-face) 55%, transparent);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    z-index: 4;
    pointer-events: none;
  }
  .lock-ring {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 3px solid color-mix(in oklab, var(--state-starting) 28%, transparent);
    border-top-color: var(--state-starting);
    animation: lock-spin 0.75s linear infinite;
  }
  @keyframes lock-spin {
    to {
      transform: rotate(360deg);
    }
  }
  /* WS5b — STP root-bridge crown, pinned to the top-right of the face. */
  .stp-crown {
    position: absolute;
    top: -10px;
    right: -6px;
    font-size: 16px;
    line-height: 1;
    z-index: 6;
    pointer-events: none;
    filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.5));
  }
  .glyph :global(svg) {
    width: 30px;
    height: 30px;
  }
  .glyph :global(img) {
    width: 30px;
    height: 30px;
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

  /* Handles stay in the DOM so floating edges can anchor to them, but are never
     visible or hit-testable — linking uses the dedicated connector button, so
     the four hover dots around the icon are gone. */
  :global(.face-node .svelte-flow__handle) {
    width: 9px;
    height: 9px;
    opacity: 0;
    pointer-events: none;
  }
</style>
