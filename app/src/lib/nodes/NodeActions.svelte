<script lang="ts">
  // Node hover quick-actions — a small horizontal action bar that floats above a
  // node on hover (PNetLab's play/console buttons over a device). Shared by both
  // node types (IolNode + VpcsNode). State-driven: Start when stopped/crashed,
  // Stop when running/starting, Console when running, Wipe when stopped.
  //
  // The bar is absolutely positioned above the face and carries `nodrag` so
  // pressing a button never begins a node drag (same trick as the connector
  // button). It sits below the top-right connector's z-index so it never
  // permanently occludes it; the connector wins the top-right corner.
  import { labStore } from "../labStore.svelte";
  import type { NodeState } from "../labTypes";
  import { uiSvg } from "../icons.svelte";

  let { nodeId, state }: { nodeId: number; state: NodeState } = $props();

  const isRunning = $derived(state === "running");
  const isBusy = $derived(state === "running" || state === "starting");
  // WS1: while a per-node action is in flight, hide the action buttons and show
  // a spinner + the pending action name so sibling actions aren't re-issued.
  const lock = $derived(labStore.nodeLocks[nodeId]);
  const isLocked = $derived(lock != null);
  // "Save config" (NVRAM extract) only applies to running IOL nodes.
  const isIol = $derived(
    labStore.lab.nodes.find((n) => n.id === nodeId)?.kind === "iol"
  );

  function start() {
    void labStore.startNode(nodeId);
  }
  function stop() {
    void labStore.stopNode(nodeId);
  }
  function console_() {
    labStore.openConsoleByMode(nodeId);
  }
  function saveConfig() {
    void labStore.saveNodeConfig(nodeId);
  }
  function wipe() {
    const node = labStore.lab.nodes.find((n) => n.id === nodeId);
    if (confirm(`Wipe saved config/state for ${node?.name ?? "this node"}? This cannot be undone.`))
      void labStore.wipeNode(nodeId);
  }
</script>

<div class="node-actions nodrag" role="toolbar" aria-label="Node quick actions">
  {#if isLocked}
    <span class="na-lock" aria-live="polite">
      <span class="na-spinner" aria-hidden="true"></span>
      <span class="na-lock-label">{lock?.action ?? "working"}…</span>
    </span>
  {:else}
  {#if !isBusy}
    <button class="na-btn" title="Start" aria-label="Start" onpointerdown={(e) => e.stopPropagation()} onclick={start}
      >{@html uiSvg("play", 12)}</button>
  {/if}
  {#if isBusy}
    <button class="na-btn" title="Stop" aria-label="Stop" onpointerdown={(e) => e.stopPropagation()} onclick={stop}
      >{@html uiSvg("stop", 12)}</button>
  {/if}
  {#if isRunning}
    <button class="na-btn" title="Console" aria-label="Console" onpointerdown={(e) => e.stopPropagation()} onclick={console_}
      >{@html uiSvg("console", 12)}</button>
  {/if}
  {#if isRunning && isIol}
    <button
      class="na-btn"
      title="Save config — extracts the node's saved NVRAM startup-config into the lab. Do write memory on the node first."
      aria-label="Save config"
      onpointerdown={(e) => e.stopPropagation()}
      onclick={saveConfig}
    >{@html uiSvg("savecfg", 12)}</button>
  {/if}
  {#if !isBusy}
    <button class="na-btn na-danger" title="Wipe" aria-label="Wipe" onpointerdown={(e) => e.stopPropagation()} onclick={wipe}
      >{@html uiSvg("wipe", 12)}</button>
  {/if}
  {/if}
</div>

<style>
  /* Floats above the node face; hidden until the node is hovered (the parent
     .face-node:hover rule below reveals it). Anchored to the face's top edge,
     centred horizontally. Kept clear of the top-right connector corner by
     sitting a touch higher and at a lower z-index. */
  .node-actions {
    position: absolute;
    left: 50%;
    top: -14px;
    transform: translate(-50%, -100%);
    display: flex;
    gap: 2px;
    padding: 3px;
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    opacity: 0;
    pointer-events: none;
    /* Forgiveness: slow fade-out (~120ms delay) so a slightly-off cursor path
       doesn't instantly kill the bar; show is immediate. */
    transition: opacity var(--transition-fast), transform var(--transition-fast);
    transition-delay: 120ms;
    z-index: 5;
  }
  /* Invisible hit-area bridging the gap between the floating bar and the face,
     so the pointer never crosses a dead zone travelling up to the buttons. It
     spans the bar's full width and reaches from the bar's bottom edge down to
     (and slightly past) the face top. No visual change. */
  .node-actions::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    top: 100%;
    height: 18px;
  }
  /* Reveal on node hover (or keyboard focus within the bar). Show is instant;
     the hide delay lives on the base rule above. */
  :global(.face-node:hover) .node-actions,
  .node-actions:hover,
  .node-actions:focus-within {
    opacity: 1;
    pointer-events: auto;
    transform: translate(-50%, -100%) translateY(-2px);
    transition-delay: 0ms;
  }
  .na-btn {
    all: unset;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    color: var(--ink-2);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .na-btn:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .na-danger:hover {
    color: var(--state-crashed);
  }
  .na-btn :global(svg) {
    width: 12px;
    height: 12px;
    pointer-events: none;
  }
  /* WS1 — in-flight lock chip: spinner + pending action name, in place of the
     action buttons. Uses --state-starting to read as "busy" in both themes. */
  .na-lock {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 0 5px;
    height: 22px;
    color: var(--ink-2);
    font-size: var(--fs-xs);
    letter-spacing: 0.02em;
  }
  .na-lock-label {
    text-transform: capitalize;
    white-space: nowrap;
  }
  .na-spinner {
    width: 11px;
    height: 11px;
    border-radius: 50%;
    border: 2px solid color-mix(in oklab, var(--state-starting) 30%, transparent);
    border-top-color: var(--state-starting);
    animation: na-spin 0.7s linear infinite;
  }
  @keyframes na-spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
