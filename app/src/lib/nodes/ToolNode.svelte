<script lang="ts">
  import { Handle, Position, type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { stateLabel } from "../nodeVisuals";
  import { iconSvg, uiSvg, iconRegistryVersion } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import NodeActions from "./NodeActions.svelte";

  let { id, data, selected }: NodeProps = $props();

  const nodeId = $derived(Number(id));
  const nodeState = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  const isLocked = $derived(labStore.nodeLocks[nodeId] != null);
  const isDropTarget = $derived(linking.dropTargetId === nodeId);
  const isLinkSource = $derived(linking.sourceId === nodeId);
  const label = $derived((data as any).label as string);
  // Falls back to the node's learning-tool pack's own icon (e.g. Security
  // Bench's firewall glyph) before the generic wrench, matching the
  // node.icon ?? defaultIconFor(...) pattern IolNode/VpcsNode already use.
  const packId = $derived((data as any).packId as string | undefined);
  const pack = $derived(labStore.toolPacks.find((p) => p.id === packId));
  const iconKey = $derived(((data as any).icon as string | undefined) ?? pack?.icon ?? "tool");
  const glyph = $derived((iconRegistryVersion(), iconSvg(iconKey, 30)));
  let guiOpen = $state(false);

  function onConnectorDown(ev: PointerEvent) {
    ev.preventDefault();
    ev.stopPropagation();
    linking.start?.(nodeId, ev);
  }

  function onDblClick(ev: MouseEvent) {
    ev.stopPropagation();
    linking.requestEdit?.(nodeId);
  }

  function openGui(ev: MouseEvent) {
    ev.preventDefault();
    ev.stopPropagation();
    guiOpen = true;
  }
</script>

<div
  class="node face-node tool"
  class:selected
  class:drop-target={isDropTarget}
  class:linking={isLinkSource}
  class:locked={isLocked}
  data-state={nodeState}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  <Handle type="source" position={Position.Top} id="top" />
  <Handle type="source" position={Position.Right} id="right" />
  <Handle type="source" position={Position.Bottom} id="bottom" />
  <Handle type="source" position={Position.Left} id="left" />

  <div class="face">
    <NodeActions {nodeId} state={nodeState} />
    {#if isLocked}
      <span class="lock-overlay" aria-hidden="true"><span class="lock-ring"></span></span>
    {/if}
    <span class="glyph" aria-hidden="true">{@html glyph}</span>
    <button
      class="gui-button nodrag"
      title="Open learning-tool GUI"
      aria-label="Open learning-tool GUI"
      onclick={openGui}
      onpointerdown={(e) => e.stopPropagation()}
    >{@html uiSvg("tool", 12)}</button>
    <button
      class="connector nodrag"
      title="Drag to another node to connect"
      aria-label="Connect this node"
      onpointerdown={onConnectorDown}
    >{@html uiSvg("link", 12)}</button>
  </div>
  <div class="name mono"><span class="led" title={stateLabel(nodeState)}></span>{label}</div>

  {#if guiOpen}
    <div class="tool-panel nodrag" role="dialog" tabindex="-1" aria-label={`${label} learning-tool GUI`} onpointerdown={(e) => e.stopPropagation()}>
      <div class="panel-head">
        <span>{label} GUI</span>
        <button class="panel-close" aria-label="Close learning-tool GUI" onclick={() => (guiOpen = false)}>×</button>
      </div>
      <iframe src={`/tool/${nodeId}/`} sandbox="allow-scripts allow-forms allow-same-origin" title={`${label} proxied GUI`}></iframe>
    </div>
  {/if}
</div>

<style>
  .node {
    position: relative;
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
    color: var(--accent);
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .node.selected .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 26%, transparent), var(--shadow-md);
  }
  .node.drop-target .face {
    border-color: var(--accent);
    box-shadow: 0 0 0 4px color-mix(in oklab, var(--accent) 34%, transparent), var(--shadow-md);
  }
  .glyph {
    display: grid;
    place-items: center;
  }
  .glyph :global(svg) {
    width: 30px;
    height: 30px;
  }
  .connector,
  .gui-button {
    all: unset;
    position: absolute;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    cursor: pointer;
    box-shadow: var(--shadow-md);
    z-index: 6;
  }
  .connector {
    top: -6px;
    right: -6px;
    background: var(--accent);
    color: var(--accent-ink);
    cursor: crosshair;
    opacity: 0;
    transform: scale(0.6);
    transition: opacity var(--transition-fast), transform var(--transition-fast);
  }
  .gui-button {
    left: -6px;
    bottom: -6px;
    background: var(--panel-solid);
    border: 1px solid var(--accent);
    color: var(--accent);
    opacity: 0;
    transform: scale(0.7);
    transition: opacity var(--transition-fast), transform var(--transition-fast), background var(--transition-fast);
  }
  .face:hover .connector,
  .connector:focus-visible,
  .node.linking .connector,
  .face:hover .gui-button,
  .gui-button:focus-visible {
    opacity: 1;
    transform: scale(1);
  }
  .gui-button:hover {
    background: var(--accent-muted);
  }
  .connector :global(svg),
  .gui-button :global(svg) {
    width: 12px;
    height: 12px;
    pointer-events: none;
  }
  .name {
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
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }
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
  @keyframes lock-spin { to { transform: rotate(360deg); } }
  .tool-panel {
    position: absolute;
    top: 74px;
    left: 0;
    width: min(620px, 70vw);
    height: min(430px, 62vh);
    z-index: 30;
    display: flex;
    flex-direction: column;
    background: var(--panel-solid);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }
  .panel-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex: 0 0 30px;
    padding: 0 8px 0 10px;
    color: var(--ink-2);
    background: var(--bg-2);
    border-bottom: 1px solid var(--border-subtle);
    font-size: var(--fs-xs);
  }
  .panel-close {
    all: unset;
    color: var(--ink-3);
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    padding: 3px 5px;
  }
  .panel-close:hover { color: var(--ink); }
  iframe {
    display: block;
    width: 100%;
    height: 100%;
    flex: 1;
    border: 0;
    background: white;
  }
  :global(.face-node .svelte-flow__handle) {
    width: 9px;
    height: 9px;
    opacity: 0;
    pointer-events: none;
  }
</style>
