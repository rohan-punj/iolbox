<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import {
    consoleUiStore,
    FONT_MIN,
    FONT_MAX,
    samePane,
    type ConsoleLayout,
    type PaneRef,
  } from "../consoleUiStore.svelte";
  import PaneBody from "./PaneBody.svelte";

  let collapsed = $state(false);

  // Console tabs are always the in-app web terminal now. Native telnet is a
  // global mode chosen in the left palette (consoleUiStore.consoleMode) which
  // makes the node Console button hand off to the OS telnet client instead of
  // opening a tab — so there is no per-tab web/native flip here anymore.
  //
  // Capture tabs: flipped shows the native Wireshark attach command as an
  // overlay ON TOP of the live summary, which keeps running underneath (the
  // stream is cheap and keeping it hot means flipping back shows no gap).


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
    consoleUiStore.setSearchOpenFor(null);
    consoleUiStore.setFocused({ kind: "capture", link: linkId });
  }
  function selectConsole(nodeId: number) {
    if (consoleUiStore.searchOpenFor !== nodeId) consoleUiStore.setSearchOpenFor(null);
    consoleUiStore.setFocused({ kind: "console", node: nodeId });
  }
  function closeCapture(linkId: number) {
    consoleUiStore.closePane(paneForCapture(linkId));
  }

  function closeConsole(nodeId: number) {
    consoleUiStore.closePane(paneForConsole(nodeId));
  }

  function closeLens(linkId: number) {
    consoleUiStore.closePane(paneForLens(linkId));
  }

  function flipCapture(linkId: number) {
    consoleUiStore.toggleNativeCapture(linkId);
  }

  function paneForConsole(nodeId: number): PaneRef {
    return { kind: "console", node: nodeId };
  }

  function paneForCapture(linkId: number): PaneRef {
    return { kind: "capture", link: linkId };
  }

  function paneForLens(linkId: number): PaneRef {
    return { kind: "lens", link: linkId };
  }

  function isFocused(ref: PaneRef): boolean {
    return samePane(consoleUiStore.focused, ref);
  }

  function isTiled(ref: PaneRef): boolean {
    return consoleUiStore.layout !== "tabs" && consoleUiStore.tiles.some((tile) => samePane(tile, ref));
  }

  function isVisible(ref: PaneRef): boolean {
    return consoleUiStore.layout === "tabs" ? isFocused(ref) : isTiled(ref);
  }

  function layoutLabel(layout: ConsoleLayout): string {
    return layout === "tabs" ? "Tabs" : `${layout.slice(4)}-up`;
  }

  function nextLayout(): ConsoleLayout {
    const layouts: ConsoleLayout[] = ["tabs", "tile2", "tile3", "tile4"];
    return layouts[(layouts.indexOf(consoleUiStore.layout) + 1) % layouts.length];
  }

  function allPanes(): PaneRef[] {
    return [
      ...labStore.openConsoleTabs.map((node) => paneForConsole(node)),
      ...labStore.openCaptureTabs.map((link) => paneForCapture(link)),
      ...labStore.openLensTabs.map((link) => paneForLens(link)),
    ];
  }

  function cycleLayout() {
    const next = nextLayout();
    consoleUiStore.setLayout(next);
    if (next !== "tabs") {
      if (consoleUiStore.focused) consoleUiStore.ensureTiled(consoleUiStore.focused);
      for (const ref of allPanes()) {
        if (consoleUiStore.tiles.length >= Number(next.slice(4))) break;
        consoleUiStore.ensureTiled(ref);
      }
    }
  }

  function toggleSearch() {
    const focused = consoleUiStore.focused;
    if (!focused || focused.kind !== "console") return;
    consoleUiStore.toggleSearchOpenFor(focused.node);
  }

  function openSearch(nodeId: number) {
    if (isFocused(paneForConsole(nodeId))) consoleUiStore.setSearchOpenFor(nodeId);
  }

  function closeSearch() {
    consoleUiStore.setSearchOpenFor(null);
  }

  function markNow() {
    const capturePos: Record<number, number> = {};
    for (const linkId of labStore.openCaptureTabs) {
      // Every open capture has one store-owned session. Snapshot zero too: a
      // mark taken before the first packet is still an exact stream boundary,
      // not an unknown position.
      capturePos[linkId] = consoleUiStore.captureDelivered[linkId] ?? 0;
    }
    consoleUiStore.addMark(capturePos);
  }

  // Link-menu "Capture in Wireshark…" fired labStore.openCapture() then set
  // this one-shot signal — jump straight to that capture tab's native overlay
  // instead of leaving the user on the plain live-summary view. Reset the
  // signal immediately so it doesn't re-fire (e.g. if the tab is later closed
  // and reopened by hand).
  // Inline icon glyphs (kept local per turf rules — no shared icons.svelte.ts).
  const DOCK_BOTTOM =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="1.5" y="9.5" width="13" height="4" rx="1" fill="currentColor" stroke="none" opacity="0.85"/></svg>';
  const DOCK_RIGHT =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="9.5" y="2.5" width="5" height="11" rx="1" fill="currentColor" stroke="none" opacity="0.85"/></svg>';
  const PAINT =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M3 9.5 9 3.5l3.5 3.5-6 6H3z"/><path d="M2 14.5h6" stroke-linecap="round"/></svg>';
  // Small "waveform" glyph marking a live-capture tab.
  const CAPTURE =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M1.5 8h2l1.5-4 2 8 2-6 1.5 4h2"/></svg>';
  const SHARK =
    '<svg viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12.5c3.5 0 4-7 8.5-8.5-.5 2 0 3.5 1 4.5s2 1.5 2.5 4z"/><path d="M1.5 14.5h13"/></svg>';
  const PIN =
    '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="m5 2 6 6"/><path d="m4 5 7 7"/><path d="m3 13 4-4"/><path d="M8 2h5l-2 3 1 3-3 1-3-3 1-3z"/></svg>';
  const LAYOUT =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.25"><rect x="1.5" y="2" width="13" height="12" rx="1.5"/><path d="M8 2v12M1.5 8h13"/></svg>';
  const FIND =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round"><circle cx="7" cy="7" r="4.25"/><path d="m10.25 10.25 3.25 3.25"/></svg>';
  const MARK =
    '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><path d="M3 2.5h10v11l-5-2.5-5 2.5z"/><path d="M5.5 5.5h5"/></svg>';

  function nodeName(id: number): string {
    return labStore.lab.nodes.find((n) => n.id === id)?.name ?? `#${id}`;
  }

  function isToolNode(id: number): boolean {
    return labStore.lab.nodes.find((n) => n.id === id)?.kind === "tool";
  }

  /** host:port for a node's telnet console, or null if the port isn't known yet. */
  function consoleAddr(nodeId: number): { host: string; port: number } | null {
    const port = labStore.consolePorts[nodeId];
    if (!port) return null;
    return { host: location.hostname || "localhost", port };
  }

  /** Click-to-copy. clipboard API needs a secure context and this page is
   *  plain-HTTP, so fall back to prompt() when it's unavailable/denied. */
  async function copyText(text: string, promptLabel: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        labStore.pushLog("info", `copied ${text}`);
        return;
      }
    } catch {
      /* fall through to prompt */
    }
    window.prompt(promptLabel, text);
  }

  async function copyAddr(nodeId: number) {
    const a = consoleAddr(nodeId);
    if (!a) return;
    await copyText(`${a.host}:${a.port}`, "Copy console address:");
  }
</script>

<div class="console-dock" class:collapsed class:side-right={consoleUiStore.dockSide === "right"}>
  <div class="dock-bar">
    <button class="collapse-btn" onclick={() => (collapsed = !collapsed)} aria-expanded={!collapsed}>
      <span class="chevron" class:flipped={collapsed}>▾</span>
      Consoles
      {#if labStore.openConsoleTabs.length + labStore.openCaptureTabs.length + labStore.openLensTabs.length > 0}
        <span class="count">{labStore.openConsoleTabs.length + labStore.openCaptureTabs.length + labStore.openLensTabs.length}</span>
      {/if}
    </button>

    {#if !collapsed}
      <div class="tabs" role="tablist">
        {#each labStore.openConsoleTabs as nodeId (nodeId)}
          {@const ref = paneForConsole(nodeId)}
          <div class="tab" class:tab-tool={isToolNode(nodeId)} class:active={isFocused(ref)}>
            <button
              class="tab-label"
              role="tab"
              aria-selected={isFocused(ref)}
              onmousedown={(event) => event.preventDefault()}
              onclick={() => selectConsole(nodeId)}
            >
              {nodeName(nodeId)}
            </button>
            {#if isTiled(ref)}<span class="tile-state" title="Tiled pane">tile</span>{/if}
            <button
              class="tab-pin"
              class:on={samePane(consoleUiStore.pinned, ref)}
              title={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane to tile 1"}
              aria-label={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane"}
              aria-pressed={samePane(consoleUiStore.pinned, ref)}
              onclick={(event) => { event.stopPropagation(); consoleUiStore.togglePinned(ref); }}
            >
              {@html PIN}
            </button>
            <button class="tab-close" title="Close" onclick={() => closeConsole(nodeId)}>
              ✕
            </button>
          </div>
        {/each}
        {#each labStore.openCaptureTabs as linkId (`cap-${linkId}`)}
          {@const ref = paneForCapture(linkId)}
          <div class="tab tab-capture" class:active={isFocused(ref)}>
            <button
              class="tab-label"
              role="tab"
              aria-selected={isFocused(ref)}
              title={captureTitle(linkId)}
              onclick={() => selectCapture(linkId)}
            >
              {@html CAPTURE}
              {captureTitle(linkId)}
            </button>
            {#if isTiled(ref)}<span class="tile-state" title="Tiled pane">tile</span>{/if}
            <button
              class="tab-pin"
              class:on={samePane(consoleUiStore.pinned, ref)}
              title={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane to tile 1"}
              aria-label={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane"}
              aria-pressed={samePane(consoleUiStore.pinned, ref)}
              onclick={(event) => { event.stopPropagation(); consoleUiStore.togglePinned(ref); }}
            >
              {@html PIN}
            </button>
            <button
              class="tab-ext"
              class:on={consoleUiStore.nativeCapture[linkId]}
              title={consoleUiStore.nativeCapture[linkId]
                ? "Hide native Wireshark command"
                : "Flip to native Wireshark (shows the attach command)"}
              aria-pressed={!!consoleUiStore.nativeCapture[linkId]}
              onclick={() => flipCapture(linkId)}
            >
              {@html SHARK}
            </button>
            <button
              class="tab-lens"
              title="Open Protocol Lens"
              onclick={() => labStore.openLens(linkId)}
            >Lens</button>
            <button class="tab-close" title="Close capture" onclick={() => closeCapture(linkId)}>
              ✕
            </button>
          </div>
        {/each}
        {#each labStore.openLensTabs as linkId (`lens-${linkId}`)}
          {@const ref = paneForLens(linkId)}
          <div class="tab tab-lens-tab" class:active={isFocused(ref)}>
            <button
              class="tab-label"
              role="tab"
              aria-selected={isFocused(ref)}
              title={`Protocol Lens · ${captureTitle(linkId)}`}
              onclick={() => consoleUiStore.setFocused(ref)}
            >
              Lens · {captureTitle(linkId)}
            </button>
            {#if isTiled(ref)}<span class="tile-state" title="Tiled pane">tile</span>{/if}
            <button
              class="tab-pin"
              class:on={samePane(consoleUiStore.pinned, ref)}
              title={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane to tile 1"}
              aria-label={samePane(consoleUiStore.pinned, ref) ? "Unpin pane" : "Pin pane"}
              aria-pressed={samePane(consoleUiStore.pinned, ref)}
              onclick={(event) => { event.stopPropagation(); consoleUiStore.togglePinned(ref); }}
            >
              {@html PIN}
            </button>
            <button class="tab-close" title="Close Lens" onclick={() => closeLens(linkId)}>×</button>
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
        <div class="font-ctl" title="Console text size ({consoleUiStore.fontSize}px)">
          <button
            class="dock-icon font-btn"
            title="Smaller console text"
            aria-label="Decrease console text size"
            disabled={consoleUiStore.fontSize <= FONT_MIN}
            onclick={() => consoleUiStore.bumpFontSize(-1)}
          >A−</button>
          <button
            class="dock-icon font-btn"
            title="Larger console text"
            aria-label="Increase console text size"
            disabled={consoleUiStore.fontSize >= FONT_MAX}
            onclick={() => consoleUiStore.bumpFontSize(1)}
          >A+</button>
        </div>
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
        <button
          class="dock-icon layout-control"
          title={`Console layout: ${layoutLabel(consoleUiStore.layout)} — click to use ${layoutLabel(nextLayout())}`}
          aria-label={`Console layout ${layoutLabel(consoleUiStore.layout)}`}
          onclick={cycleLayout}
        >
          {@html LAYOUT}
          <span>{layoutLabel(consoleUiStore.layout)}</span>
        </button>
        <button
          class="dock-icon"
          class:on={consoleUiStore.searchOpenFor !== null}
          title={consoleUiStore.focused?.kind === "console" ? "Find in this console" : "Focus a console to search"}
          aria-label="Find in this console"
          disabled={consoleUiStore.focused?.kind !== "console"}
          aria-pressed={consoleUiStore.searchOpenFor !== null}
          onclick={toggleSearch}
        >
          {@html FIND}
        </button>
        <button class="dock-icon mark-control" title="Mark capture now" aria-label="Mark capture now" onclick={markNow}>
          {@html MARK}
        </button>
        <button
          class="dock-icon"
          title="Close console — closes every console and capture tab"
          aria-label="Close console window"
          onclick={() => labStore.closeAllConsoles()}
        >✕</button>
      </div>
    {/if}
  </div>

  {#if !collapsed}
    <div
      class="term-area"
      class:tiled={consoleUiStore.layout !== "tabs"}
      class:layout-tile2={consoleUiStore.layout === "tile2"}
      class:layout-tile3={consoleUiStore.layout === "tile3"}
      class:layout-tile4={consoleUiStore.layout === "tile4"}
      class:single-tile={consoleUiStore.tiles.length <= 1}
    >
      {#each labStore.openConsoleTabs as nodeId (nodeId)}
        {@const ref = paneForConsole(nodeId)}
        <div
          class="term-slot"
          class:tiled={consoleUiStore.layout !== "tabs" && isTiled(ref)}
          class:untiled={consoleUiStore.layout !== "tabs" && !isTiled(ref)}
          class:hidden={consoleUiStore.layout === "tabs" && !isFocused(ref)}
        >
          <PaneBody {ref} visible={isVisible(ref)} focused={isFocused(ref)} />
        </div>
      {/each}
      {#each labStore.openCaptureTabs as linkId (`cap-${linkId}`)}
        {@const ref = paneForCapture(linkId)}
        <div
          class="term-slot"
          class:tiled={consoleUiStore.layout !== "tabs" && isTiled(ref)}
          class:untiled={consoleUiStore.layout !== "tabs" && !isTiled(ref)}
          class:hidden={consoleUiStore.layout === "tabs" && !isFocused(ref)}
        >
          <PaneBody {ref} visible={isVisible(ref)} focused={isFocused(ref)} />
        </div>
      {/each}
      {#each labStore.openLensTabs as linkId (`lens-${linkId}`)}
        {@const ref = paneForLens(linkId)}
        <div
          class="term-slot"
          class:tiled={consoleUiStore.layout !== "tabs" && isTiled(ref)}
          class:untiled={consoleUiStore.layout !== "tabs" && !isTiled(ref)}
          class:hidden={consoleUiStore.layout === "tabs" && !isFocused(ref)}
        >
          <PaneBody {ref} visible={isVisible(ref)} focused={isFocused(ref)} />
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
  .tab-label:focus-visible,
  .tab-pin:focus-visible,
  .tab-ext:focus-visible,
  .tab-lens:focus-visible,
  .tab-close:focus-visible,
  .dock-icon:focus-visible,
  .collapse-btn:focus-visible,
  .addr-chip:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
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
    color: var(--warning);
    flex-shrink: 0;
  }
  .tab.active .tab-label {
    color: var(--text-primary);
  }
  .tab-tool.active {
    border-bottom-color: var(--accent);
  }
  .tab-ext,
  .tab-lens,
  .tab-close,
  .tab-pin {
    all: unset;
    display: inline-flex;
    align-items: center;
    font-size: 11px;
    color: var(--text-tertiary);
    cursor: pointer;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }
  .tab-pin {
    padding: 3px;
  }
  .tab-pin.on {
    color: var(--accent);
  }
  .tab-pin :global(svg) {
    display: block;
  }
  .tile-state {
    padding: 2px 3px;
    color: var(--text-tertiary);
    font: 600 9px/1 var(--font-ui);
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .tab-ext:hover,
  .tab-lens:hover,
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
  .dock-icon:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .dock-icon.layout-control {
    gap: 4px;
    font-size: 10px;
    font-weight: 600;
  }
  .dock-icon.layout-control span {
    font-family: var(--font-ui);
  }
  /* A-/A+ console font-size control: a tight pair of text buttons. */
  .font-ctl {
    display: inline-flex;
    align-items: center;
    gap: 1px;
  }
  .font-btn {
    font: 600 11px/1 ui-monospace, monospace;
    min-width: 20px;
  }
  .font-btn:disabled {
    opacity: 0.35;
    cursor: default;
    background: none;
    color: var(--text-tertiary);
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
  .term-area.tiled {
    display: grid;
    gap: 1px;
    background: var(--border);
  }
  .term-area.layout-tile2 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: minmax(0, 1fr);
  }
  .term-area.layout-tile4 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-rows: repeat(2, minmax(0, 1fr));
  }
  .term-area.layout-tile3 {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    grid-template-rows: minmax(0, 1fr);
  }
  .term-area.single-tile {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
  }
  .term-slot.tiled {
    position: static;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .term-slot.untiled {
    display: none;
  }
  .term-slot.hidden {
    visibility: hidden;
    pointer-events: none;
  }
  /* Active (flipped-to-native) state of a tab's flip button. */
  .tab-ext.on {
    color: var(--accent);
  }
</style>
