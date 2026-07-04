<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { consoleUiStore } from "../consoleUiStore.svelte";
  import { iconSvg } from "../icons.svelte";
  import { annoTool, ANNO_COLORS, type AnnoTool } from "../annoTool.svelte";
  import { watcherStore } from "../watcherStore.svelte";
  import { painterStore } from "../painterStore.svelte";

  function onDragStart(e: DragEvent, kind: "iol" | "vpcs" | "nat", imageId?: string) {
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
  // "Save configs" only makes sense when running IOL nodes exist.
  const hasRunningIol = $derived(
    labStore.lab.nodes.some(
      (n) => n.kind === "iol" && labStore.nodeStates[n.id] === "running"
    )
  );
  // "Console all" — any node (not just IOL) can have a console.
  const hasRunningNode = $derived(
    labStore.lab.nodes.some((n) => labStore.nodeStates[n.id] === "running")
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
  async function forceClean() {
    await labStore.forceClean();
  }
  function consoleAll() {
    labStore.openAllConsoles();
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
  // Supervisor build (git describe, from the hello handshake) — makes it
  // obvious at a glance whether a deployment is stale.
  const supervisorVersion = $derived(labStore.supervisorVersion);
</script>

<div class="palette">
  <div class="section-title">Session</div>
  <div class="session-actions">
    <button class="btn btn-primary session-primary" onclick={startAll} disabled={running}>
      <svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" stroke="none" aria-hidden="true"><path d="M8 5.5v13l11-6.5z" /></svg>
      Start all
    </button>
    <button class="btn session-secondary" onclick={stopAll} disabled={!running}>
      <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor" stroke="none" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="1.5" /></svg>
      Stop all
    </button>
  </div>

  <div class="session-rows">
    <button
      class="action-row"
      onclick={saveConfigs}
      disabled={!hasRunningIol}
      title="Extract each running IOL node's saved NVRAM startup-config into the lab — write memory on the nodes first"
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
        <path d="M17 21v-8H7v8M7 3v5h8" />
      </svg>
      <span>Save configs</span>
    </button>
    <button
      class="action-row"
      onclick={consoleAll}
      disabled={!hasRunningNode}
      title={hasRunningNode
        ? "Open a console for every running node, honouring the Web/Native mode below"
        : "No running nodes — start the lab first"}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <path d="M7 9l3 3-3 3M13 15h4" />
      </svg>
      <span>Console all</span>
    </button>
    <button
      class="action-row"
      onclick={forceClean}
      title="Force-stop every node, tap/bridge and capture on the runtime — clears orphaned processes when nodes still show running or host CPU stays high after a normal stop."
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5" />
        <path d="M21 12a9 9 0 0 1-15 6.7L3 16M3 21v-5h5" />
      </svg>
      <span>Force clean</span>
    </button>
    <button
      class="action-row danger"
      onclick={wipeAll}
      disabled={running}
      title="Delete all saved NVRAM/state for this lab (stops nodes first). Cannot be undone."
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13M10 11v6M14 11v6" />
      </svg>
      <span>Wipe all</span>
    </button>
  </div>

  <div class="section-title">Console</div>
  <div
    class="seg"
    role="group"
    aria-label="Console open mode"
    title="How the node Console button opens: an in-app web console, or hand off to your OS telnet client (PuTTY etc.) for every node."
  >
    <button
      class="seg-btn"
      class:on={consoleUiStore.consoleMode === "web"}
      aria-pressed={consoleUiStore.consoleMode === "web"}
      onclick={() => consoleUiStore.setConsoleMode("web")}
    >Web</button>
    <button
      class="seg-btn"
      class:on={consoleUiStore.consoleMode === "native"}
      aria-pressed={consoleUiStore.consoleMode === "native"}
      onclick={() => consoleUiStore.setConsoleMode("native")}
    >Native</button>
  </div>

  <div class="section-title">View</div>
  <button
    class="action-row"
    title="Network Watcher — pick protocols to highlight as animated directional overlays on the links (PNetLab-style), read off link.stats"
    onclick={() => watcherStore.togglePanel()}
  >
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
    <span>Network watcher</span>
  </button>
  <button
    class="action-row"
    title="Topology Painter — snapshot live STP/OSPF/EIGRP/BGP decisions from the running nodes and paint them onto the links (root bridge, blocked ports, best-path)"
    onclick={() => painterStore.togglePanel()}
  >
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M18.37 2.63 14 7l-1.5-1.5a1.4 1.4 0 0 0-2 0l-1 1a1.4 1.4 0 0 0 0 2L11 10l-7 7v3h3l7-7 1.5 1.5a1.4 1.4 0 0 0 2 0l1-1a1.4 1.4 0 0 0 0-2L17 10l4.37-4.37a1.87 1.87 0 0 0-2.64-2.64Z" />
    </svg>
    <span>Topology painter</span>
  </button>

  <div class="section-title">Nodes</div>

  <div
    class="palette-item"
    draggable="true"
    role="button"
    tabindex="0"
    ondragstart={(e) => onDragStart(e, "vpcs")}
  >
    <span class="swatch vpcs" aria-hidden="true">{@html iconSvg("pc", 28)}</span>
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
      <span class="swatch nat" aria-hidden="true">{@html iconSvg("nat", 28)}</span>
      <div class="item-text">
        <div class="item-name">NAT Gateway</div>
        <div class="item-sub">Internet egress</div>
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
      <!-- Item 5 — real EVE device artwork per image class (l2→switch,
           l3/unknown→router), matching the icon a dropped node will show. -->
      <span class="swatch" class:l2={img.class === "l2"} aria-hidden="true">
        {@html iconSvg(img.class === "l2" ? "switch" : "router", 28)}
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
  {#if supervisorVersion}
    <div class="host-build mono" title="Supervisor build (git describe) — check this against the repo when a fix seems missing">
      build {supervisorVersion}
    </div>
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
  .host-build {
    font-size: 10px;
    color: var(--ink-3);
    text-align: right;
    padding: 0 2px;
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
  /* SESSION — a prominent Start/Stop pair over a list of icon action rows. */
  .session-actions {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
    margin-bottom: 6px;
  }
  .session-primary,
  .session-secondary {
    justify-content: center;
    gap: 6px;
    width: 100%;
    padding: 7px 8px;
    font-size: var(--fs-sm);
  }
  .session-primary svg,
  .session-secondary svg {
    flex-shrink: 0;
  }
  .session-rows {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin-bottom: var(--sp-2);
  }
  /* Menu-style row: leading icon + label, full width, hover-highlighted. */
  .action-row {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: 7px 8px;
    border-radius: var(--radius-md);
    font-size: var(--fs-sm);
    font-weight: 500;
    color: var(--ink-2);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .action-row svg {
    flex-shrink: 0;
    color: var(--ink-3);
    transition: color var(--transition-fast);
  }
  .action-row:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .action-row:hover svg {
    color: var(--accent);
  }
  .action-row:focus-visible {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .action-row:disabled {
    opacity: 0.42;
    cursor: not-allowed;
    background: none;
    color: var(--ink-2);
  }
  .action-row:disabled svg {
    color: var(--ink-3);
  }
  .action-row.danger {
    color: var(--danger);
  }
  .action-row.danger svg {
    color: var(--danger);
  }
  .action-row.danger:hover {
    background: var(--state-crashed-bg);
    color: var(--danger-hover);
  }
  .action-row.danger:hover svg {
    color: var(--danger-hover);
  }
  /* Segmented toggle (Console open mode). */
  .seg {
    display: flex;
    gap: 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    overflow: hidden;
    margin-bottom: var(--sp-2);
  }
  .seg-btn {
    all: unset;
    box-sizing: border-box;
    flex: 1;
    text-align: center;
    padding: 5px 6px;
    font-size: var(--fs-xs);
    font-weight: 550;
    color: var(--ink-2);
    background: var(--bg-2);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .seg-btn + .seg-btn {
    border-left: 1px solid var(--border-strong);
  }
  .seg-btn:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .seg-btn.on {
    background: var(--accent-muted);
    color: var(--accent);
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
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    color: var(--node-iol-l3);
  }
  .swatch.vpcs {
    color: var(--node-vpcs);
  }
  .swatch.nat {
    color: var(--accent);
  }
  .swatch :global(svg) {
    width: 28px;
    height: 28px;
  }
  .swatch :global(img) {
    width: 28px;
    height: 28px;
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
