<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { iconSvg } from "../icons.svelte";
  import { annoTool, ANNO_COLORS, type AnnoTool } from "../annoTool.svelte";

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

  const armed = $derived(annoTool.active);
  function pickTool(tool: AnnoTool) {
    annoTool.arm(tool);
  }
  function pickColor(c: string) {
    annoTool.color = c;
  }

  // Host monitor: live CPU/RAM/disk of the runtime VM (host.stats events).
  const host = $derived(labStore.hostStats);
  function fmtGB(bytes: number): string {
    return (bytes / 1024 / 1024 / 1024).toFixed(1);
  }
  function pct(used: number, total: number): number {
    return total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
  }
  // Amber >75%, red >90% — same thresholds a PNetLab operator watches.
  function tone(p: number): string {
    return p > 90 ? "var(--state-crashed)" : p > 75 ? "var(--state-starting)" : "var(--accent)";
  }
  const memPct = $derived(host ? pct(host.memUsed, host.memTotal) : 0);
  const diskPct = $derived(host ? pct(host.diskUsed, host.diskTotal) : 0);
  const cpuPct = $derived(host ? Math.min(100, Math.round(host.cpuPct)) : 0);
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

  <div class="section-title">Draw</div>
  <div class="draw-tools" role="group" aria-label="Annotation tools">
    <button
      class="draw-btn"
      class:on={armed === "text"}
      aria-pressed={armed === "text"}
      title="Text — click the canvas to place, then type"
      onclick={() => pickTool("text")}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <path d="M5 6V5h14v1M12 5v14M9 19h6" />
      </svg>
      <span>Text</span>
    </button>
    <button
      class="draw-btn"
      class:on={armed === "rect"}
      aria-pressed={armed === "rect"}
      title="Rectangle — click the canvas to place"
      onclick={() => pickTool("rect")}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7">
        <rect x="4" y="6" width="16" height="12" rx="2" />
      </svg>
      <span>Rect</span>
    </button>
    <button
      class="draw-btn"
      class:on={armed === "ellipse"}
      aria-pressed={armed === "ellipse"}
      title="Ellipse — click the canvas to place"
      onclick={() => pickTool("ellipse")}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7">
        <ellipse cx="12" cy="12" rx="8" ry="6" />
      </svg>
      <span>Ellipse</span>
    </button>
    <button
      class="draw-btn"
      class:on={armed === "note"}
      aria-pressed={armed === "note"}
      title="Note — a text box with a coloured background fill"
      onclick={() => pickTool("note")}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round">
        <path d="M5 4h14v11l-5 5H5z" />
        <path d="M14 20v-5h5" />
      </svg>
      <span>Note</span>
    </button>
    <button
      class="draw-btn"
      class:on={armed === "line"}
      aria-pressed={armed === "line"}
      title="Line — click two points on the canvas"
      onclick={() => pickTool("line")}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round">
        <line x1="5" y1="19" x2="19" y2="5" />
        <circle cx="5" cy="19" r="1.6" fill="currentColor" stroke="none" />
        <circle cx="19" cy="5" r="1.6" fill="currentColor" stroke="none" />
      </svg>
      <span>Line</span>
    </button>
  </div>
  <div class="draw-colors" role="group" aria-label="Annotation colour">
    {#each ANNO_COLORS as c (c)}
      <button
        class="swatch-dot"
        class:on={annoTool.color === c}
        style:background={c}
        title="Colour"
        aria-label="Colour"
        aria-pressed={annoTool.color === c}
        onclick={() => pickColor(c)}
      ></button>
    {/each}
  </div>
  {#if armed}
    <div class="draw-hint">
      {#if armed === "line"}
        Click two points on the canvas to draw a line.
      {:else}
        Click the canvas to place the {armed}.
      {/if}
    </div>
  {/if}

  <div class="host-spacer"></div>
  <div class="section-title">Host</div>
  {#if host}
    <div class="host-mon" title="Runtime VM executing IOL — live CPU / RAM / disk">
      <div class="host-row">
        <span class="host-lbl">CPU</span>
        <div class="host-bar"><span style:width={`${cpuPct}%`} style:background={tone(cpuPct)}></span></div>
        <span class="host-val mono">{cpuPct}%</span>
      </div>
      <div class="host-row">
        <span class="host-lbl">RAM</span>
        <div class="host-bar"><span style:width={`${memPct}%`} style:background={tone(memPct)}></span></div>
        <span class="host-val mono">{fmtGB(host.memUsed)}/{fmtGB(host.memTotal)}G</span>
      </div>
      <div class="host-row">
        <span class="host-lbl">Disk</span>
        <div class="host-bar"><span style:width={`${diskPct}%`} style:background={tone(diskPct)}></span></div>
        <span class="host-val mono">{fmtGB(host.diskUsed)}/{fmtGB(host.diskTotal)}G</span>
      </div>
      <div class="host-cores mono">{host.cores} vCPU</div>
    </div>
  {:else}
    <div class="host-idle">Waiting for host stats…</div>
  {/if}
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
  .host-spacer {
    flex: 1;
    min-height: var(--sp-2);
  }
  .host-mon {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 8px;
    background: var(--panel-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
  }
  .host-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .host-lbl {
    font-size: var(--fs-xs);
    color: var(--ink-2);
    width: 30px;
    flex-shrink: 0;
  }
  .host-bar {
    flex: 1;
    height: 6px;
    border-radius: 3px;
    background: color-mix(in oklab, var(--ink-3) 30%, transparent);
    overflow: hidden;
  }
  .host-bar span {
    display: block;
    height: 100%;
    border-radius: 3px;
    transition: width 0.6s ease, background 0.6s ease;
  }
  .host-val {
    font-size: 10px;
    color: var(--ink-2);
    width: 74px;
    text-align: right;
    flex-shrink: 0;
  }
  .host-cores {
    font-size: 10px;
    color: var(--ink-3);
    text-align: right;
  }
  .host-idle {
    font-size: var(--fs-xs);
    color: var(--ink-3);
    padding: 6px 2px;
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

  /* DRAW cluster — annotation tools + colour swatches. */
  .draw-tools {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 4px;
  }
  .draw-btn {
    all: unset;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    padding: 7px 4px;
    border-radius: var(--radius-md);
    border: 1px solid var(--border-strong);
    background: var(--bg-2);
    color: var(--ink-2);
    font-size: 11px;
    font-weight: 550;
    cursor: pointer;
    transition: background var(--transition-fast), border-color var(--transition-fast), color var(--transition-fast);
  }
  .draw-btn:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .draw-btn.on {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-muted);
  }
  .draw-colors {
    display: flex;
    gap: 6px;
    margin-top: 6px;
    flex-wrap: wrap;
  }
  .swatch-dot {
    all: unset;
    box-sizing: border-box;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    cursor: pointer;
    border: 2px solid transparent;
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .swatch-dot.on {
    border-color: var(--ground);
    box-shadow: 0 0 0 2px var(--accent);
  }
  .draw-hint {
    margin-top: 6px;
    font-size: 11px;
    color: var(--accent);
    line-height: 1.4;
  }
</style>
