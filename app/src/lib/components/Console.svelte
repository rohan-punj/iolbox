<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { consoleUiStore } from "../consoleUiStore.svelte";
  import ConsoleTerm from "./ConsoleTerm.svelte";
  import CaptureTerm from "./CaptureTerm.svelte";

  let collapsed = $state(false);
  // The dock's active tab is either a node console (activeConsoleTab) or a
  // capture view. We track a capture selection separately and prefer whichever
  // was most recently activated.
  let activeCapture = $state<number | null>(null);

  // Live-capture tab title: "R1 e0/0 ⇄ e0/0 SW1" from the link's endpoints.
  function captureTitle(linkId: number): string {
    const link = labStore.lab.links.find((l) => l.id === linkId);
    if (!link) return `capture #${linkId}`;
    const [a, b] = link.endpoints;
    const an = labStore.lab.nodes.find((n) => n.id === a?.node)?.name ?? `#${a?.node}`;
    const bn = labStore.lab.nodes.find((n) => n.id === b?.node)?.name ?? `#${b?.node}`;
    return `${an} ${a?.interface ?? ""} ⇄ ${b?.interface ?? ""} ${bn}`;
  }

  function selectCapture(linkId: number) {
    activeCapture = linkId;
    labStore.activeConsoleTab = null;
  }
  function selectConsole(nodeId: number) {
    labStore.activeConsoleTab = nodeId;
    activeCapture = null;
  }
  function closeCapture(linkId: number) {
    labStore.closeCapture(linkId);
    if (activeCapture === linkId) {
      activeCapture = labStore.openCaptureTabs[0] ?? null;
      if (activeCapture === null) labStore.activeConsoleTab = labStore.openConsoleTabs[0] ?? null;
    }
  }

  // Inline icon glyphs (kept local per turf rules — no shared icons.svelte.ts).
  const DOCK_BOTTOM =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="1.5" y="9.5" width="13" height="4" rx="1" fill="currentColor" stroke="none" opacity="0.85"/></svg>';
  const DOCK_RIGHT =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="9.5" y="2.5" width="5" height="11" rx="1" fill="currentColor" stroke="none" opacity="0.85"/></svg>';
  const PAINT =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M3 9.5 9 3.5l3.5 3.5-6 6H3z"/><path d="M2 14.5h6" stroke-linecap="round"/></svg>';
  const TELNET =
    '<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="9" rx="1.5"/><path d="M4 5.5l2 1.5-2 1.5M7.5 9h4" stroke-linecap="round" stroke-linejoin="round"/></svg>';
  // Small "waveform" glyph marking a live-capture tab.
  const CAPTURE =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M1.5 8h2l1.5-4 2 8 2-6 1.5 4h2"/></svg>';

  function nodeName(id: number): string {
    return labStore.lab.nodes.find((n) => n.id === id)?.name ?? `#${id}`;
  }

  /** host:port for a node's telnet console, or null if the port isn't known yet. */
  function consoleAddr(nodeId: number): { host: string; port: number } | null {
    const port = labStore.consolePorts[nodeId];
    if (!port) return null;
    return { host: location.hostname || "localhost", port };
  }

  function telnetTitle(nodeId: number): string {
    const a = consoleAddr(nodeId);
    return a ? `telnet ${a.host}:${a.port}` : "console port not assigned yet";
  }

  /** Flip a console to a native telnet client via the telnet:// scheme handler
   *  (PuTTY etc. on Windows). If no handler is registered the navigation fails
   *  silently — that's fine; the tooltip + copy-chip still tell the user the
   *  address to dial manually. */
  function openNative(nodeId: number) {
    const a = consoleAddr(nodeId);
    if (!a) {
      labStore.pushLog("warn", `node ${nodeId}: console port not assigned yet`, nodeId);
      return;
    }
    const url = `telnet://${a.host}:${a.port}`;
    try {
      window.open(url, "_self");
    } catch {
      /* no telnet:// handler — tooltip/copy chip cover it */
    }
    labStore.pushLog("info", `native console for ${nodeName(nodeId)} → ${url}`, nodeId);
  }

  /** Click-to-copy host:port. clipboard API needs a secure context and this page
   *  is plain-HTTP, so fall back to prompt() when it's unavailable/denied. */
  async function copyAddr(nodeId: number) {
    const a = consoleAddr(nodeId);
    if (!a) return;
    const text = `${a.host}:${a.port}`;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        labStore.pushLog("info", `copied ${text}`, nodeId);
        return;
      }
    } catch {
      /* fall through to prompt */
    }
    window.prompt("Copy console address:", text);
  }
</script>

<div class="console-dock" class:collapsed class:side-right={consoleUiStore.dockSide === "right"}>
  <div class="dock-bar">
    <button class="collapse-btn" onclick={() => (collapsed = !collapsed)} aria-expanded={!collapsed}>
      <span class="chevron" class:flipped={collapsed}>▾</span>
      Consoles
      {#if labStore.openConsoleTabs.length + labStore.openCaptureTabs.length > 0}
        <span class="count">{labStore.openConsoleTabs.length + labStore.openCaptureTabs.length}</span>
      {/if}
    </button>

    {#if !collapsed}
      <div class="tabs" role="tablist">
        {#each labStore.openConsoleTabs as nodeId (nodeId)}
          <div class="tab" class:active={labStore.activeConsoleTab === nodeId}>
            <button
              class="tab-label"
              role="tab"
              aria-selected={labStore.activeConsoleTab === nodeId}
              onclick={() => selectConsole(nodeId)}
            >
              {nodeName(nodeId)}
            </button>
            <button
              class="tab-ext"
              title={telnetTitle(nodeId)}
              aria-label="Open in native telnet client"
              onclick={() => openNative(nodeId)}
            >
              {@html TELNET}
            </button>
            <button class="tab-close" title="Close" onclick={() => labStore.closeConsole(nodeId)}>
              ✕
            </button>
          </div>
        {/each}
        {#each labStore.openCaptureTabs as linkId (`cap-${linkId}`)}
          <div class="tab tab-capture" class:active={activeCapture === linkId}>
            <button
              class="tab-label"
              role="tab"
              aria-selected={activeCapture === linkId}
              title={captureTitle(linkId)}
              onclick={() => selectCapture(linkId)}
            >
              {@html CAPTURE}
              {captureTitle(linkId)}
            </button>
            <button class="tab-close" title="Close capture" onclick={() => closeCapture(linkId)}>
              ✕
            </button>
          </div>
        {/each}
      </div>

      <div class="dock-actions">
        {#if labStore.activeConsoleTab !== null && consoleAddr(labStore.activeConsoleTab)}
          {@const a = consoleAddr(labStore.activeConsoleTab)}
          <button
            class="addr-chip mono"
            title="Click to copy — {a?.host}:{a?.port}"
            onclick={() => copyAddr(labStore.activeConsoleTab!)}
          >
            {a?.host}:{a?.port}
          </button>
        {/if}
        <button
          class="dock-icon"
          class:on={consoleUiStore.colorize}
          title={consoleUiStore.colorize ? "Colorizing on — click to disable" : "Colorizing off — click to enable"}
          aria-pressed={consoleUiStore.colorize}
          onclick={() => consoleUiStore.toggleColorize()}
        >
          {@html PAINT}
        </button>
        <button
          class="dock-icon"
          title={consoleUiStore.dockSide === "bottom" ? "Dock to right" : "Dock to bottom"}
          aria-label="Toggle dock side"
          onclick={() => consoleUiStore.toggleDockSide()}
        >
          {@html consoleUiStore.dockSide === "bottom" ? DOCK_RIGHT : DOCK_BOTTOM}
        </button>
      </div>
    {/if}
  </div>

  {#if !collapsed}
    <div class="term-area">
      {#each labStore.openConsoleTabs as nodeId (nodeId)}
        <div class="term-slot" class:hidden={labStore.activeConsoleTab !== nodeId}>
          <ConsoleTerm {nodeId} active={labStore.activeConsoleTab === nodeId} />
        </div>
      {/each}
      {#each labStore.openCaptureTabs as linkId (`cap-${linkId}`)}
        <div class="term-slot" class:hidden={activeCapture !== linkId}>
          <CaptureTerm {linkId} active={activeCapture === linkId} />
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .console-dock {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-1);
    border-top: 1px solid var(--border);
  }
  /* Right-docked: the pane is a full-height vertical column, so the top border
     of the bottom-dock becomes a left border instead. */
  .console-dock.side-right {
    border-top: none;
    border-left: 1px solid var(--border);
  }
  .console-dock.collapsed {
    height: auto;
  }
  .dock-bar {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 0 var(--sp-2);
    height: 32px;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .collapse-btn {
    all: unset;
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--text-secondary);
    cursor: pointer;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    flex-shrink: 0;
  }
  .collapse-btn:hover {
    background: var(--bg-hover);
  }
  .chevron {
    display: inline-block;
    transition: transform var(--transition-fast);
    font-size: 10px;
  }
  .chevron.flipped {
    transform: rotate(-90deg);
  }
  .count {
    background: var(--accent-muted);
    color: var(--accent);
    border-radius: var(--radius-full);
    font-size: 10px;
    padding: 1px 6px;
  }
  .tabs {
    display: flex;
    gap: 2px;
    overflow-x: auto;
    flex: 1;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 3px 4px 3px 10px;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    background: transparent;
    border-bottom: 2px solid transparent;
  }
  .tab.active {
    background: var(--bg-2);
    border-bottom-color: var(--accent);
  }
  .tab-label {
    all: unset;
    font-size: var(--fs-xs);
    color: var(--text-secondary);
    cursor: pointer;
    padding: 2px 4px;
  }
  .tab-capture .tab-label {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-family: var(--font-mono);
  }
  .tab-capture.active {
    border-bottom-color: var(--state-starting);
  }
  .tab-capture :global(svg) {
    color: var(--state-starting);
    flex-shrink: 0;
  }
  .tab.active .tab-label {
    color: var(--text-primary);
  }
  .tab-ext,
  .tab-close {
    all: unset;
    display: inline-flex;
    align-items: center;
    font-size: 11px;
    color: var(--text-tertiary);
    cursor: pointer;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }
  .tab-ext:hover,
  .tab-close:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .dock-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    margin-left: auto;
  }
  .addr-chip {
    all: unset;
    font-size: 11px;
    color: var(--text-secondary);
    cursor: pointer;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-subtle);
    white-space: nowrap;
  }
  .addr-chip:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
    border-color: var(--border);
  }
  .dock-icon {
    all: unset;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--text-tertiary);
    cursor: pointer;
    padding: 4px;
    border-radius: var(--radius-sm);
  }
  .dock-icon:hover {
    background: var(--bg-hover);
    color: var(--text-primary);
  }
  .dock-icon.on {
    color: var(--accent);
  }
  .term-area {
    flex: 1;
    min-height: 0;
    position: relative;
    background: var(--term-bg);
  }
  .term-slot {
    position: absolute;
    inset: 0;
  }
  .term-slot.hidden {
    visibility: hidden;
    pointer-events: none;
  }
</style>
