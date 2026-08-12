<script lang="ts">
  import { labStore } from "./lib/labStore.svelte";
  import { consoleUiStore } from "./lib/consoleUiStore.svelte";
  import { railUiStore } from "./lib/railUiStore.svelte";
  import { dragNodeCountStore } from "./lib/dragNodeCountStore.svelte";
  import { nodeCatalog, type CatalogEntry } from "./lib/nodeCatalog";
  import { annoTool, ANNO_COLORS, type AnnoTool } from "./lib/annoTool.svelte";
  import { chromeStore } from "./lib/chromeStore.svelte";
  import { watcherStore } from "./lib/watcherStore.svelte";
  import { painterStore } from "./lib/painterStore.svelte";
  import { iconSvg, uiSvg } from "./lib/icons.svelte";
  import TopBar from "./lib/components/TopBar.svelte";
  import ResourceBar from "./lib/components/ResourceBar.svelte";
  import IconRail from "./lib/components/IconRail.svelte";
  import RailFlyout from "./lib/components/RailFlyout.svelte";
  import Canvas from "./lib/components/Canvas.svelte";
  import Inspector from "./lib/components/Inspector.svelte";
  import TasksPane from "./lib/components/TasksPane.svelte";
  import Console from "./lib/components/Console.svelte";
  import FloatingConsoleLayer from "./lib/components/FloatingConsoleLayer.svelte";
  import Preflight from "./lib/components/Preflight.svelte";
  import ImageManager from "./lib/components/ImageManager.svelte";
  import LabBrowser from "./lib/components/LabBrowser.svelte";
  import SwitchLabDialog from "./lib/components/SwitchLabDialog.svelte";
  import SettingsDialog from "./lib/components/SettingsDialog.svelte";
  import LinkFaultDialog from "./lib/components/LinkFaultDialog.svelte";
  import SplitPane from "./lib/components/SplitPane.svelte";
  import WatcherPanel from "./lib/components/WatcherPanel.svelte";
  import PainterPanel from "./lib/components/PainterPanel.svelte";

  let inspectorWidth = $state(300);
  let tasksWidth = $state(360);
  let consoleHeight = $state(240);
  let consoleWidth = $state(420);
  // Live viewport width so the right-docked console's drag limit scales with
  // the window: it may stretch to HALF the screen (a wide terminal for configs)
  // instead of the old fixed 720px cap.
  let winW = $state(typeof window !== "undefined" ? window.innerWidth : 1280);

  // consoleUiStore cannot import labStore without creating a singleton cycle.
  // Register the one outward focus-sync callback at the app boundary instead.
  consoleUiStore.bindConsoleSelect((nodeId) => {
    labStore.activeConsoleTab = nodeId;
  });
  consoleUiStore.bindPaneClose((ref) => {
    if (ref.kind === "console") labStore.closeConsole(ref.node);
    else if (ref.kind === "capture") labStore.closeCapture(ref.link);
    else labStore.closeLens(ref.link);
    return {
      nextCapture: labStore.openCaptureTabs[0],
      nextConsole: labStore.openConsoleTabs[0],
    };
  });

  // Pane and mark lifecycle belongs to the console UI store. Observing the
  // public tab lists keeps this robust to every labStore reset path, including
  // future ones, while leaving labStore itself untouched.
  $effect(() => {
    consoleUiStore.reconcile(
      labStore.lab.id,
      labStore.openConsoleTabs,
      labStore.openCaptureTabs,
      labStore.openLensTabs
    );
    consoleUiStore.syncFromLabStore(labStore.activeConsoleTab);
  });

  // Link-menu "Capture in Wireshark…" is a one-shot signal. Set the native
  // overlay on; do not toggle it if the overlay is already visible.
  $effect(() => {
    const linkId = labStore.wiresharkOverlayFor;
    if (linkId === null) return;
    consoleUiStore.setFocused({ kind: "capture", link: linkId });
    consoleUiStore.setNativeCapture(linkId, true);
    labStore.wiresharkOverlayFor = null;
  });

  // Only show the console dock when at least one tab is open — an empty dock
  // just steals canvas space. Placement (bottom bar vs right pane) is the
  // persisted user choice from consoleUiStore.
  const showConsole = $derived(
    labStore.openConsoleTabs.length > 0 ||
    labStore.openCaptureTabs.length > 0 ||
    labStore.openLensTabs.length > 0
  );
  const dockRight = $derived(consoleUiStore.dockSide === "right");

  // Right pane. The Tasks pane (TopBar toggle) takes precedence over the
  // empty-selection auto-hide and, while toggled on, wins even when a node is
  // selected. Otherwise the Inspector shows only when something is selected;
  // clicking empty canvas clears selection (CanvasInner onPaneClick), collapsing
  // the pane and handing its width back to the canvas.
  const showTasks = $derived(labStore.showTasks);
  const showInspector = $derived(
    !showTasks && (labStore.inspectorNodeId !== null || labStore.selectedLinkId !== null)
  );

  const NODE_GROUPS = ["Devices", "Endpoints", "Services", "IOL images"] as const;
  let nodeQuery = $state("");
  const catalog = $derived(nodeCatalog());
  const filteredCatalog = $derived(
    catalog.filter((entry) => entry.search.includes(nodeQuery.trim().toLowerCase()))
  );

  const running = $derived(labStore.labRunning);
  const hasRunningIol = $derived(
    labStore.lab.nodes.some(
      (node) => node.kind === "iol" && labStore.nodeStates[node.id] === "running"
    )
  );
  const hasRunningNode = $derived(
    labStore.lab.nodes.some((node) => labStore.nodeStates[node.id] === "running")
  );

  function onDragStart(e: DragEvent, entry: CatalogEntry) {
    if (!e.dataTransfer) return;
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("application/iolbox-node", JSON.stringify(entry.drag));
    dragNodeCountStore.begin(entry.drag, e.clientX, e.clientY);
  }

  function onDragEnd() {
    // Fires whether the drag ended in a drop or was cancelled (dropped
    // outside the canvas, Escape) — CanvasInner's onDrop already consumed
    // (and reset) the store on a real drop, so this is a no-op then and a
    // cleanup-only path otherwise.
    dragNodeCountStore.reset();
  }

  function placeNode(entry: CatalogEntry) {
    railUiStore.placeNode(entry.drag);
    railUiStore.close();
  }

  async function saveConfigs() {
    await labStore.saveAllConfigs();
  }

  async function wipeAll() {
    if (!confirm("Wipe all saved configs/state for this lab? This cannot be undone.")) return;
    await labStore.wipeLab();
  }

  function pickShape(tool: AnnoTool) {
    annoTool.arm(tool);
  }

  function pickColor(color: string) {
    annoTool.color = color;
  }

  let wasLabRunning = false;
  $effect(() => {
    const running = labStore.labRunning;
    if (running && !wasLabRunning) chromeStore.reveal();
    wasLabRunning = running;
    chromeStore.syncVisibility();
  });
  $effect(() => chromeStore.start());
</script>

<svelte:window
  bind:innerWidth={winW}
  onpointermove={(event) => chromeStore.onPointerMove(event)}
  onpointerup={() => chromeStore.onPointerUp()}
  onpointercancel={() => chromeStore.onPointerUp()}
  onkeydown={(event) => chromeStore.onKeyDown(event)}
  onfocusin={(event) => chromeStore.onFocusIn(event)}
/>

<div class="shell">
  <TopBar />

  <div class="body">
    <IconRail />

    <div class="center-col">
      <div class="canvas-area">
        <Canvas />
        {#if railUiStore.open === "nodes"}
          <RailFlyout title="Add Nodes" onClose={() => railUiStore.close()}>
            {#snippet children()}
              <label class="catalog-search">
                <span class="sr-only">Search nodes</span>
                <input bind:value={nodeQuery} type="search" placeholder="Search nodes" aria-label="Search nodes" />
                {#if nodeQuery}
                  <button type="button" title="Clear search" aria-label="Clear search" onclick={() => (nodeQuery = "")}>×</button>
                {/if}
              </label>

              {#each NODE_GROUPS as group}
                {@const groupEntries = filteredCatalog.filter((entry) => entry.group === group)}
                {#if groupEntries.length > 0 || (group === "IOL images") || (group === "Services" && (labStore.toolPacksLoading || labStore.toolPacksError))}
                  <section class="catalog-group">
                    <h3>{group}</h3>
                    {#if groupEntries.length > 0}
                      <div class="catalog-list">
                        {#each groupEntries as entry (entry.id)}
                          <button
                            class="catalog-item"
                            class:l2={entry.sub.startsWith("L2")}
                            draggable="true"
                            title={entry.sub}
                            aria-label={`${entry.name}, ${entry.sub}`}
                            onclick={() => placeNode(entry)}
                            ondragstart={(event) => onDragStart(event, entry)}
                            ondragend={onDragEnd}
                          >
                            <span class="catalog-icon" aria-hidden="true">{@html iconSvg(entry.icon, 26)}</span>
                            <span class="catalog-copy">
                              <strong>{entry.name}</strong>
                              <span>{entry.sub}</span>
                            </span>
                          </button>
                        {/each}
                      </div>
                    {:else if group === "Services" && labStore.toolPacksLoading}
                      <p class="flyout-hint">Loading learning tools…</p>
                    {:else if group === "Services" && labStore.toolPacksError}
                      <p class="flyout-hint error">Learning tools unavailable</p>
                    {:else if group === "IOL images" && labStore.imagesLoading}
                      <p class="flyout-hint">Loading images…</p>
                    {:else if group === "IOL images" && labStore.images.length === 0}
                      <p class="flyout-hint">No images yet. Open Image Manager to add one.</p>
                    {/if}
                  </section>
                {/if}
              {/each}

              {#if filteredCatalog.length === 0 && !labStore.imagesLoading}
                <p class="flyout-hint">No nodes match “{nodeQuery}”.</p>
              {/if}
              <button class="flyout-secondary" onclick={() => { labStore.showImageManager = true; railUiStore.close(); }}>
                {@html uiSvg("images", 14)}
                Manage images…
              </button>
            {/snippet}
          </RailFlyout>
        {:else if railUiStore.open === "actions"}
          <RailFlyout title="Node Actions" onClose={() => railUiStore.close()}>
            {#snippet children()}
              <div class="session-actions">
                <button class="flyout-primary" onclick={() => labStore.startLab()} disabled={running}>
                  {@html uiSvg("play", 14)} Start all
                </button>
                <button class="flyout-secondary" onclick={() => labStore.stopLab()} disabled={!running}>
                  {@html uiSvg("stop", 14)} Stop all
                </button>
              </div>
              <div class="flyout-actions">
                <button class="flyout-row" onclick={saveConfigs} disabled={!hasRunningIol} title="Extract each running IOL node's saved NVRAM startup-config into the lab — write memory on the nodes first">
                  {@html uiSvg("savecfg", 16)} <span>Save configs</span>
                </button>
                <button class="flyout-row" onclick={() => labStore.openAllConsoles()} disabled={!hasRunningNode} title={hasRunningNode ? "Open a console for every running node" : "No running nodes — start the lab first"}>
                  {@html uiSvg("console", 16)} <span>Console all</span>
                </button>
                <button class="flyout-row" onclick={() => labStore.forceClean()} title="Force-stop every node, tap/bridge and capture on the runtime">
                  {@html uiSvg("reset", 16)} <span>Force clean</span>
                </button>
                <div class="flyout-sep"></div>
                <button class="flyout-row danger" onclick={wipeAll} disabled={running} title="Delete all saved NVRAM/state for this lab. Cannot be undone.">
                  {@html uiSvg("wipe", 16)} <span>Wipe all</span>
                </button>
              </div>
            {/snippet}
          </RailFlyout>
        {:else if railUiStore.open === "shapes"}
          <RailFlyout title="Add Shapes" onClose={() => railUiStore.close()}>
            {#snippet children()}
              <div class="shape-tools" role="group" aria-label="Annotation shape tools">
                {#each (["rect", "ellipse", "note", "line"] as AnnoTool[]) as tool}
                  <button class="shape-tool" class:on={annoTool.active === tool} aria-pressed={annoTool.active === tool} onclick={() => pickShape(tool)}>
                    <span class="shape-icon" aria-hidden="true">{@html uiSvg(tool === "line" ? "lineShape" : tool === "note" ? "edit" : tool === "ellipse" ? "ellipseShape" : "rectShape", 16)}</span>
                    <span>{tool === "rect" ? "Rectangle" : tool[0].toUpperCase() + tool.slice(1)}</span>
                  </button>
                {/each}
              </div>
              <div class="color-group" role="group" aria-label="Annotation colour">
                <span class="group-label">Colour</span>
                <div class="color-swatches">
                  {#each ANNO_COLORS as color (color)}
                    <button class="color-swatch" class:on={annoTool.color === color} style:background={color} title={`Colour ${color}`} aria-label={`Colour ${color}`} aria-pressed={annoTool.color === color} onclick={() => pickColor(color)}></button>
                  {/each}
                </div>
              </div>
              {#if annoTool.active && annoTool.active !== "text"}
                <p class="flyout-hint accent">Click the canvas to place the {annoTool.active}.</p>
              {/if}
            {/snippet}
          </RailFlyout>
        {:else if railUiStore.open === "tools"}
          <RailFlyout title="Tools" onClose={() => railUiStore.close()}>
            {#snippet children()}
              <div class="flyout-actions">
                <button class="flyout-row" onclick={() => watcherStore.togglePanel()} title="Pick protocols to highlight as animated directional overlays on the links">
                  {@html uiSvg("net", 16)} <span>Network watcher</span>
                </button>
                <button class="flyout-row" onclick={() => painterStore.togglePanel()} title="Paint live STP/OSPF/EIGRP/BGP decisions onto the links">
                  {@html uiSvg("edit", 16)} <span>Topology painter</span>
                </button>
              </div>
            {/snippet}
          </RailFlyout>
        {/if}
        <!-- Network Watcher — floating filter card over the canvas, top-right
             (under the top bar). Rendered inside canvas-area so it never
             overlaps the inspector/console panes. -->
        <WatcherPanel />
        <!-- Topology Painter — sibling floating card. When the watcher card is
             also open it stacks below it (offset) so both stay usable. -->
        <PainterPanel />
      </div>
      {#if showConsole && consoleUiStore.placement === "dock" && !dockRight}
        <SplitPane
          direction="vertical"
          edge="end"
          bind:size={consoleHeight}
          min={80}
          max={520}
          storageKey="iolbox.split.consoleBottom"
        >
          <Console />
        </SplitPane>
      {/if}
    </div>

    {#if showTasks}
      <div class="inspector-pane" style:flex-basis={`${tasksWidth}px`}>
        <TasksPane />
      </div>
    {:else if showInspector}
      <div class="inspector-pane" style:flex-basis={`${inspectorWidth}px`}>
        <Inspector />
      </div>
    {/if}

    {#if showConsole && consoleUiStore.placement === "dock" && dockRight}
      <SplitPane
        direction="horizontal"
        edge="end"
        bind:size={consoleWidth}
        min={280}
        max={Math.max(720, Math.floor(winW * 0.5))}
        storageKey="iolbox.split.consoleRight"
      >
        <Console />
      </SplitPane>
    {/if}
  </div>
  <ResourceBar />
</div>

{#if showConsole && consoleUiStore.placement === "float"}
  <FloatingConsoleLayer />
{/if}

{#if dragNodeCountStore.active && dragNodeCountStore.shiftHeld}
  <div
    class="drag-count-badge"
    style:left={`${dragNodeCountStore.cursor.x + 16}px`}
    style:top={`${dragNodeCountStore.cursor.y + 16}px`}
  >
    drop to choose count
  </div>
{/if}

{#if labStore.showPreflight}
  <Preflight onDismiss={() => (labStore.showPreflight = false)} />
{/if}

{#if labStore.showImageManager}
  <ImageManager onClose={() => (labStore.showImageManager = false)} />
{/if}

{#if labStore.showLabBrowser}
  <LabBrowser onClose={() => (labStore.showLabBrowser = false)} />
{/if}

{#if labStore.showSettings}
  <SettingsDialog onClose={() => (labStore.showSettings = false)} />
{/if}

{#if labStore.showLinkFault}
  <LinkFaultDialog linkId={labStore.showLinkFault.linkId} onClose={() => (labStore.showLinkFault = null)} />
{/if}

<SwitchLabDialog />

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    width: 100vw;
  }
  .drag-count-badge {
    position: fixed;
    z-index: var(--z-modal);
    pointer-events: none;
    background: var(--accent);
    color: var(--bg-0);
    font-size: var(--fs-sm);
    font-weight: 600;
    font-family: var(--font-mono);
    padding: 2px 8px;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .center-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .canvas-area {
    flex: 1;
    min-height: 0;
    /* Anchor for the floating WatcherPanel (position:absolute). */
    position: relative;
  }
  .inspector-pane {
    flex-shrink: 0;
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    overflow: hidden;
  }
  .catalog-search {
    position: relative;
    display: block;
    margin-bottom: 12px;
  }
  .catalog-search input {
    width: 100%;
    padding-right: 30px;
  }
  .catalog-search button {
    all: unset;
    position: absolute;
    top: 50%;
    right: 6px;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    color: var(--ink-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transform: translateY(-50%);
  }
  .catalog-search button:hover {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .catalog-group + .catalog-group {
    margin-top: 14px;
  }
  .catalog-group h3,
  .group-label {
    display: block;
    margin: 0 0 6px;
    color: var(--ink-2);
    font-size: var(--fs-xs);
    font-weight: 650;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .catalog-list,
  .flyout-actions {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .catalog-item,
  .flyout-row,
  .flyout-primary,
  .flyout-secondary,
  .shape-tool {
    box-sizing: border-box;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: color var(--transition-fast), background var(--transition-fast), border-color var(--transition-fast);
  }
  .catalog-item {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 7px 8px;
    text-align: left;
    color: var(--ink);
    background: var(--bg-2);
  }
  .catalog-item:hover {
    background: var(--bg-hover);
    border-color: var(--accent);
  }
  .catalog-icon {
    flex: 0 0 30px;
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    color: var(--node-iol-l3);
  }
  .catalog-item.l2 .catalog-icon {
    color: var(--node-iol-l2);
  }
  .catalog-copy {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .catalog-copy strong {
    overflow: hidden;
    color: var(--ink);
    font-size: var(--fs-sm);
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .catalog-copy span {
    overflow: hidden;
    color: var(--ink-2);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .flyout-hint {
    margin: 8px 2px;
    color: var(--ink-3);
    font-size: var(--fs-xs);
    line-height: 1.45;
  }
  .flyout-hint.error,
  .flyout-row.danger {
    color: var(--danger);
  }
  .flyout-hint.accent {
    color: var(--accent);
  }
  .flyout-primary,
  .flyout-secondary {
    min-height: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 6px 9px;
    color: var(--ink);
    background: var(--bg-2);
    font-size: var(--fs-sm);
    font-weight: 550;
  }
  .flyout-primary {
    color: var(--accent-ink);
    background: var(--accent);
    border-color: var(--accent);
  }
  .flyout-primary:hover:not(:disabled),
  .flyout-secondary:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .flyout-primary:disabled,
  .flyout-secondary:disabled,
  .flyout-row:disabled {
    cursor: not-allowed;
    opacity: 0.42;
  }
  .flyout-actions {
    gap: 1px;
  }
  .flyout-row {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 34px;
    padding: 7px 8px;
    border-radius: var(--radius-md);
    color: var(--ink-2);
    cursor: pointer;
  }
  .flyout-row :global(svg) {
    flex: 0 0 auto;
    color: var(--ink-3);
  }
  .flyout-row:hover:not(:disabled),
  .flyout-row:focus-visible:not(:disabled) {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .flyout-row:hover:not(:disabled) :global(svg) {
    color: var(--accent);
  }
  .flyout-sep {
    height: 1px;
    margin: 6px 2px;
    background: var(--border);
  }
  .shape-tools {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 5px;
  }
  .shape-tool {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    min-height: 58px;
    padding: 7px 4px;
    color: var(--ink-2);
    background: var(--bg-2);
    font-size: var(--fs-xs);
  }
  .shape-tool:hover,
  .shape-tool.on {
    color: var(--accent);
    background: var(--accent-muted);
    border-color: var(--accent);
  }
  .shape-icon :global(svg) {
    width: 16px;
    height: 16px;
  }
  .color-group {
    margin-top: 14px;
  }
  .color-swatches {
    display: flex;
    flex-wrap: wrap;
    gap: 7px;
  }
  .color-swatch {
    width: 20px;
    height: 20px;
    padding: 0;
    border: 2px solid transparent;
    border-radius: 50%;
    box-shadow: 0 0 0 1px var(--border-strong);
    cursor: pointer;
  }
  .color-swatch.on {
    border-color: var(--ground);
    box-shadow: 0 0 0 2px var(--accent);
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .catalog-item,
    .flyout-row,
    .flyout-primary,
    .flyout-secondary,
    .shape-tool {
      transition: none;
    }
  }
</style>
