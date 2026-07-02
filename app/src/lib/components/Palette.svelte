<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { iconSvg } from "../icons.svelte";

  function onDragStart(e: DragEvent, kind: "iol" | "vpcs" | "nat" | "mgmt", imageId?: string) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData(
      "application/iolab-node",
      JSON.stringify({ kind, imageId })
    );
  }

  const iolImages = $derived(labStore.images);
  const running = $derived(labStore.labRunning);
  // Feature-gated builtin nodes (feature 5). Only shown when the supervisor
  // advertised the capability in its hello handshake.
  const hasNat = $derived(labStore.features.includes("natgw"));
  const hasMgmt = $derived(labStore.features.includes("mgmt"));
  // "Save configs" only makes sense when running IOL nodes exist.
  const hasRunningIol = $derived(
    labStore.lab.nodes.some(
      (n) => n.kind === "iol" && labStore.nodeStates[n.id] === "running"
    )
  );

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
  async function saveConfigs() {
    await labStore.saveAllConfigs();
  }
</script>

<div class="palette">
  <div class="section-title">Lab</div>
  <div class="lab-controls">
    <button class="btn lab-btn" onclick={startAll} disabled={running}>Start all</button>
    <button class="btn lab-btn" onclick={stopAll} disabled={!running}>Stop all</button>
    <button
      class="btn lab-btn"
      onclick={saveConfigs}
      disabled={!hasRunningIol}
      title="Extract each running IOL node's saved NVRAM startup-config into the lab — write memory on the nodes first"
    >Save configs</button>
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

  {#if hasNat}
    <div
      class="palette-item"
      draggable="true"
      role="button"
      tabindex="0"
      ondragstart={(e) => onDragStart(e, "nat")}
      title="NAT gateway to the outside network (single eth0)"
    >
      <span class="swatch nat" aria-hidden="true">{@html iconSvg("nat", 16)}</span>
      <div class="item-text">
        <div class="item-name">NAT Gateway</div>
        <div class="item-sub">Internet egress</div>
      </div>
    </div>
  {/if}

  {#if hasMgmt}
    <div
      class="palette-item"
      draggable="true"
      role="button"
      tabindex="0"
      ondragstart={(e) => onDragStart(e, "mgmt")}
      title="Out-of-band management bridge (single eth0)"
    >
      <span class="swatch mgmt" aria-hidden="true">{@html iconSvg("mgmt", 16)}</span>
      <div class="item-text">
        <div class="item-name">MGMT Bridge</div>
        <div class="item-sub">Management net</div>
      </div>
    </div>
  {/if}

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
        <div class="item-sub">
          <span class="class-badge" class:l2={img.class === "l2"}>{img.class.toUpperCase()}</span>
          <span class="arch">{img.arch}</span>
        </div>
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
    color: var(--ink-2);
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
    /* Stronger row border so cards read as discrete, tappable rows in dark. */
    border: 1px solid var(--border-strong);
    background: var(--bg-2);
    cursor: grab;
    transition: background var(--transition-fast), border-color var(--transition-fast);
  }
  .palette-item:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
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
  .swatch.nat,
  .swatch.mgmt {
    color: var(--accent);
  }
  .swatch :global(svg) {
    width: 16px;
    height: 16px;
  }
  .swatch.l2 {
    color: var(--node-iol-l2);
  }
  .item-text {
    min-width: 0;
  }
  .item-name {
    /* Full-ink filename — the dimmer --text-primary alias already maps to --ink,
       but be explicit so this row never inherits a muted color. */
    font-size: var(--fs-sm);
    font-weight: 550;
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .item-sub {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 2px;
    font-size: 11px;
    color: var(--ink-2);
  }
  /* Class badge (L3/L2): filled pill, high-contrast against the row so the
     class is legible at a glance even in the dark Glass theme. */
  .class-badge {
    font-weight: 700;
    font-size: 10px;
    letter-spacing: 0.03em;
    color: var(--accent-ink);
    background: var(--node-iol-l3);
    padding: 1px 5px;
    border-radius: var(--radius-sm);
  }
  .class-badge.l2 {
    background: var(--node-iol-l2);
  }
  .arch {
    /* "x86_64"/"i386" — secondary ink, not tertiary, so it stays readable. */
    color: var(--ink-2);
    font-family: var(--font-mono);
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
