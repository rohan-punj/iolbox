<script lang="ts">
  import { onMount } from "svelte";
  import { labStore } from "../labStore.svelte";
  import type { NodeMACsResult } from "../protocol";
  import { colonToDotted } from "../macs";
  import { portal } from "../portal";

  let { x, y, nodeId, onClose }: { x: number; y: number; nodeId: number; onClose: () => void } =
    $props();

  let el: HTMLDivElement | undefined = $state();
  let result = $state<NodeMACsResult | null>(null);
  let error = $state<string | null>(null);
  const nodeName = $derived(labStore.lab.nodes.find((node) => node.id === nodeId)?.name ?? `Node ${nodeId}`);

  function handleWindowClick(e: MouseEvent) {
    if (el && !el.contains(e.target as Node)) onClose();
  }

  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  // Clamp within viewport — mirrors InterfacePicker's px/py pattern. No
  // popover height is known ahead of render, so the bottom clamp uses a
  // conservative estimate (max-height below); a few px of slack is fine.
  const px = $derived(Math.max(8, Math.min(x, window.innerWidth - 358)));
  const py = $derived(Math.max(8, Math.min(y, window.innerHeight - 368)));

  onMount(() => {
    let alive = true;
    void labStore.client
      .nodeMACs(nodeId)
      .then((response) => {
        if (alive) result = response;
      })
      .catch((reason: unknown) => {
        if (alive) error = reason instanceof Error ? reason.message : String(reason);
      });
    return () => {
      alive = false;
    };
  });
</script>

<svelte:window onmousedown={handleWindowClick} onkeydown={handleKey} />

<div
  class="popover"
  use:portal
  bind:this={el}
  style:left={`${px}px`}
  style:top={`${py}px`}
  role="dialog"
  aria-label={`MAC addresses — ${nodeName}`}
>
  <div class="title">MAC addresses — <span class="mono">{nodeName}</span></div>
  {#if error}
    <div class="empty">Unable to read MAC addresses: {error}</div>
  {:else if !result}
    <div class="empty">Reading interface addresses…</div>
  {:else}
    <div class="list">
      {#each result.macs as mac (mac.interface)}
        <div class="row">
          <span class="iface">{mac.interface}</span>
          <span class="address">{mac.state === "known" && mac.mac ? colonToDotted(mac.mac) : "—"}</span>
          <span class="provenance">{mac.state === "known" ? mac.source : mac.reason}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .popover {
    position: fixed;
    z-index: var(--z-menu);
    width: 350px;
    max-width: min(350px, calc(100vw - 20px));
    max-height: 360px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }
  .title {
    padding: var(--sp-2) var(--sp-3);
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border);
  }
  .list {
    overflow-y: auto;
    padding: 5px 6px;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(48px, 0.7fr) minmax(124px, 1fr) minmax(96px, 1.25fr);
    align-items: baseline;
    gap: var(--sp-2);
    padding: 5px 6px;
    border-radius: var(--radius-sm);
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
    font-size: var(--fs-xs);
  }
  .row:hover {
    background: var(--bg-hover);
  }
  .iface {
    color: var(--text-primary);
  }
  .address {
    color: var(--text-primary);
    white-space: nowrap;
  }
  .provenance {
    overflow: hidden;
    color: var(--text-tertiary);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.03em;
    line-height: 1.3;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty {
    padding: var(--sp-3);
    color: var(--text-tertiary);
    font-size: var(--fs-xs);
  }
</style>
