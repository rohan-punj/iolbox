<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import ConsoleTerm from "./ConsoleTerm.svelte";

  let collapsed = $state(false);

  function nodeName(id: number): string {
    return labStore.lab.nodes.find((n) => n.id === id)?.name ?? `#${id}`;
  }

  function openExternal(nodeId: number) {
    // TODO(P2): invoke Tauri `open_external_console` — shells out to
    // Windows Terminal / telnet.exe pointed at the node's console port.
    labStore.pushLog("info", `open_external_console stub for node ${nodeId} (not wired to Tauri yet)`);
  }
</script>

<div class="console-dock" class:collapsed>
  <div class="dock-bar">
    <button class="collapse-btn" onclick={() => (collapsed = !collapsed)} aria-expanded={!collapsed}>
      <span class="chevron" class:flipped={collapsed}>▾</span>
      Consoles
      {#if labStore.openConsoleTabs.length > 0}
        <span class="count">{labStore.openConsoleTabs.length}</span>
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
              onclick={() => (labStore.activeConsoleTab = nodeId)}
            >
              {nodeName(nodeId)}
            </button>
            <button
              class="tab-ext"
              title="Open in external terminal"
              onclick={() => openExternal(nodeId)}
            >
              ⧉
            </button>
            <button class="tab-close" title="Close" onclick={() => labStore.closeConsole(nodeId)}>
              ✕
            </button>
          </div>
        {/each}
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
      {#if labStore.openConsoleTabs.length === 0}
        <div class="empty">No consoles open. Right-click a running node → Console.</div>
      {/if}
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
  .tab.active .tab-label {
    color: var(--text-primary);
  }
  .tab-ext,
  .tab-close {
    all: unset;
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
  .term-area {
    flex: 1;
    min-height: 0;
    position: relative;
    background: #08090c;
  }
  .term-slot {
    position: absolute;
    inset: 0;
  }
  .term-slot.hidden {
    visibility: hidden;
    pointer-events: none;
  }
  .empty {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-tertiary);
    font-size: var(--fs-sm);
  }
</style>
