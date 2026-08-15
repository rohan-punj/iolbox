<script lang="ts">
  import { chromeStore } from "../chromeStore.svelte";
  import { labStore } from "../labStore.svelte";

  let { x, y, nodeId, onClose }: { x: number; y: number; nodeId: number; onClose: () => void } =
    $props();
  $effect(() => chromeStore.hold());

  let el: HTMLDivElement | undefined = $state();

  function handleWindowClick(e: MouseEvent) {
    if (el && !el.contains(e.target as Node)) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  const node = $derived(labStore.lab.nodes.find((n) => n.id === nodeId));

  async function pick(imageId: string) {
    const img = labStore.images.find((candidate) => candidate.id === imageId);
    if (img && !labStore.imageSupport(img).supported) return;
    await labStore.setNodeImage(nodeId, imageId);
    onClose();
  }
</script>

<svelte:window onmousedown={handleWindowClick} onkeydown={handleKey} />

<div class="popover" bind:this={el} style:left={`${x}px`} style:top={`${y}px`}>
  <div class="title">Change image — {node?.name}</div>
  <div class="list">
    {#each labStore.images as img (img.id)}
      {@const support = labStore.imageSupport(img)}
      <button class="row" class:active={node?.image?.id === img.id} disabled={!support.supported} title={support.reason ?? img.filename} onclick={() => pick(img.id)}>
        <span class="badge" class:l2={img.class === "l2"}>{img.class.toUpperCase()}</span>
        <span class="fname">{img.filename}</span>
        {#if !support.supported}<span class="unsupported">Unsupported</span>{/if}
      </button>
    {/each}
    {#if labStore.images.length === 0}
      <div class="empty">No images in library.</div>
    {/if}
  </div>
</div>

<style>
  .popover {
    position: fixed;
    z-index: 1000;
    width: 280px;
    max-height: 320px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    display: flex;
    flex-direction: column;
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
    padding: 4px;
  }
  .row {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--fs-sm);
  }
  .row:hover {
    background: var(--bg-hover);
  }
  .row.active {
    background: var(--accent-muted);
  }
  .badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 5px;
    border-radius: var(--radius-sm);
    background: var(--node-iol-l3);
    color: var(--ground);
    flex-shrink: 0;
  }
  .badge.l2 {
    background: var(--node-iol-l2);
  }
  .fname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }
  .empty {
    padding: var(--sp-3);
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
  }
</style>
