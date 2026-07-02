<script lang="ts">
  import { labStore } from "../labStore.svelte";

  function onDragStart(e: DragEvent, kind: "iol" | "vpcs", imageId?: string) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData(
      "application/iolab-node",
      JSON.stringify({ kind, imageId })
    );
  }

  const iolImages = $derived(labStore.images);
  const running = $derived(labStore.labRunning);

  async function startAll() {
    await labStore.startLab();
  }
  async function stopAll() {
    await labStore.stopLab();
  }
  async function wipeAll() {
    if (!confirm("Wipe all saved configs/state for this lab? This cannot be undone.")) return;
    await labStore.wipeLab();
  }
</script>

<div class="palette">
  <div class="section-title">Lab</div>
  <div class="lab-controls">
    <button class="btn lab-btn" onclick={startAll} disabled={running}>Start all</button>
    <button class="btn lab-btn" onclick={stopAll} disabled={!running}>Stop all</button>
    <button class="btn lab-btn btn-danger" onclick={wipeAll} disabled={running}>Wipe all</button>
  </div>

  <div class="section-title">Nodes</div>

  <div
    class="palette-item"
    draggable="true"
    role="button"
    tabindex="0"
    ondragstart={(e) => onDragStart(e, "vpcs")}
  >
    <span class="swatch vpcs" aria-hidden="true">
      <svg viewBox="0 0 24 24" width="16" height="16">
        <rect x="3" y="4" width="18" height="12" rx="1.5" fill="currentColor" opacity="0.2" />
        <rect x="3" y="4" width="18" height="12" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
      </svg>
    </span>
    <div class="item-text">
      <div class="item-name">VPCS</div>
      <div class="item-sub">Virtual PC</div>
    </div>
  </div>

  <div class="section-title">IOL images</div>
  {#if iolImages.length === 0}
    <div class="empty-hint">No images yet. Open the Image Manager to add one.</div>
  {/if}
  {#each iolImages as img (img.id)}
    <div
      class="palette-item"
      draggable="true"
      role="button"
      tabindex="0"
      ondragstart={(e) => onDragStart(e, "iol", img.id)}
      title={img.filename}
    >
      <span class="swatch" class:l2={img.class === "l2"} aria-hidden="true">
        {#if img.class === "l2"}
          <svg viewBox="0 0 24 24" width="16" height="16">
            <rect x="2" y="8" width="20" height="8" rx="1.5" fill="currentColor" opacity="0.2" />
            <rect x="2" y="8" width="20" height="8" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" />
          </svg>
        {:else}
          <svg viewBox="0 0 24 24" width="16" height="16">
            <circle cx="12" cy="12" r="9" fill="currentColor" opacity="0.2" />
            <circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="1.5" />
          </svg>
        {/if}
      </span>
      <div class="item-text">
        <div class="item-name">{img.filename}</div>
        <div class="item-sub">{img.class.toUpperCase()} · {img.arch}</div>
      </div>
    </div>
  {/each}

  <button class="btn manage-btn" onclick={() => (labStore.showImageManager = true)}>
    Manage images…
  </button>
</div>

<style>
  .palette {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--sp-3);
    height: 100%;
    overflow-y: auto;
  }
  .section-title {
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-tertiary);
    margin: var(--sp-2) 0 2px;
  }
  .section-title:first-child {
    margin-top: 0;
  }
  .lab-controls {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: var(--sp-2);
  }
  .lab-btn {
    justify-content: center;
    width: 100%;
    font-size: var(--fs-xs);
    padding: 6px 8px;
  }
  .palette-item {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 7px 8px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-subtle);
    background: var(--bg-1);
    cursor: grab;
    transition: background var(--transition-fast), border-color var(--transition-fast);
  }
  .palette-item:hover {
    background: var(--bg-hover);
    border-color: var(--border);
  }
  .palette-item:active {
    cursor: grabbing;
  }
  .swatch {
    display: flex;
    flex-shrink: 0;
    color: var(--node-iol-l3);
  }
  .swatch.vpcs {
    color: var(--node-vpcs);
  }
  .swatch.l2 {
    color: var(--node-iol-l2);
  }
  .item-text {
    min-width: 0;
  }
  .item-name {
    font-size: var(--fs-sm);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-sub {
    font-size: 10px;
    color: var(--text-tertiary);
  }
  .empty-hint {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    padding: var(--sp-2);
    line-height: 1.5;
  }
  .manage-btn {
    margin-top: var(--sp-2);
    justify-content: center;
    width: 100%;
  }
</style>
