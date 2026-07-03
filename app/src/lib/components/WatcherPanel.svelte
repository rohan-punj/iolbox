<script lang="ts">
  // Network Watcher panel (PNetLab-style) — a compact floating card over the
  // canvas: up to 4 protocol filter rows (colour + protocol picker) and a
  // Start/Stop toggle. All state lives in watcherStore; FloatingEdge reads the
  // same store to draw the directional overlays, so this panel is pure chrome.
  import { watcherStore, LABELS, PROTO_ORDER, type ProtoKey } from "../watcherStore.svelte";

  const rows = $derived(watcherStore.rows);
  const running = $derived(watcherStore.running);
</script>

{#if watcherStore.panelOpen}
  <div class="watcher-panel" role="dialog" aria-label="Network Watcher">
    <div class="wp-header">
      <span class="wp-title">Network Watcher</span>
      <button
        class="wp-close"
        title="Close panel (a running watch keeps highlighting)"
        aria-label="Close"
        onclick={() => (watcherStore.panelOpen = false)}
      >✕</button>
    </div>

    <div class="wp-body">
      {#each rows as row (row.id)}
        <div class="wp-row">
          <!-- Native colour picker disguised as the row swatch: clicking opens
               the OS picker; the input's own value renders as the dot. -->
          <input
            type="color"
            class="wp-swatch"
            value={row.color}
            title="Pick flow colour"
            aria-label="Row colour"
            oninput={(e) => watcherStore.setColor(row.id, (e.currentTarget as HTMLInputElement).value)}
          />
          <select
            class="wp-select"
            value={row.proto}
            aria-label="Protocol filter"
            onchange={(e) => watcherStore.setProto(row.id, (e.currentTarget as HTMLSelectElement).value as ProtoKey)}
          >
            {#each PROTO_ORDER as key (key)}
              <option value={key}>{LABELS[key].name}</option>
            {/each}
          </select>
          {#if rows.length > 1}
            <button
              class="wp-remove"
              title="Remove filter"
              aria-label="Remove filter"
              onclick={() => watcherStore.removeRow(row.id)}
            >✕</button>
          {/if}
        </div>
      {/each}

      {#if watcherStore.canAddRow}
        <button class="wp-add" onclick={() => watcherStore.addRow()}>+ Add filter</button>
      {/if}
    </div>

    <div class="wp-footer">
      {#if running}
        <button class="wp-run stop" onclick={() => watcherStore.stop()}>Stop</button>
      {:else}
        <button class="wp-run start" onclick={() => watcherStore.start()}>Start</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* Floating card over the canvas, top-right. Anchored to .canvas-area
     (position:relative in App.svelte) so it sits under the top bar and never
     overlaps the inspector/console panes. Same surface treatment as the
     palette cards / console dock. */
  .watcher-panel {
    position: absolute;
    top: 12px;
    right: 12px;
    width: 260px;
    z-index: 60;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    overflow: hidden;
  }
  .wp-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
  }
  .wp-title {
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--ink-2);
  }
  .wp-close,
  .wp-remove {
    all: unset;
    box-sizing: border-box;
    cursor: pointer;
    color: var(--ink-3);
    font-size: 11px;
    line-height: 1;
    padding: 3px 5px;
    border-radius: var(--radius-sm);
  }
  .wp-close:hover,
  .wp-remove:hover {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .wp-body {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 10px;
  }
  .wp-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  /* The swatch IS an <input type=color>: strip the native chrome down to a
     round dot showing the picked colour; clicking opens the OS colour picker. */
  .wp-swatch {
    all: unset;
    box-sizing: border-box;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    overflow: hidden;
    cursor: pointer;
    flex-shrink: 0;
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .wp-swatch::-webkit-color-swatch-wrapper {
    padding: 0;
  }
  .wp-swatch::-webkit-color-swatch {
    border: none;
    border-radius: 50%;
  }
  .wp-swatch::-moz-color-swatch {
    border: none;
    border-radius: 50%;
  }
  .wp-swatch:hover {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .wp-select {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 6px;
  }
  .wp-add {
    all: unset;
    box-sizing: border-box;
    align-self: flex-start;
    cursor: pointer;
    font-size: var(--fs-xs);
    font-weight: 550;
    color: var(--accent);
    padding: 3px 4px;
    border-radius: var(--radius-sm);
  }
  .wp-add:hover {
    background: var(--bg-hover);
  }
  .wp-footer {
    padding: 8px 10px;
    border-top: 1px solid var(--border);
  }
  .wp-run {
    all: unset;
    box-sizing: border-box;
    display: block;
    width: 100%;
    text-align: center;
    padding: 6px 0;
    font-size: var(--fs-xs);
    font-weight: 600;
    border-radius: var(--radius-sm);
    cursor: pointer;
    color: #fff;
  }
  /* Green start / red stop, muted toward the theme so they don't scream. */
  .wp-run.start {
    background: color-mix(in oklab, #2f9e57 85%, var(--bg-2));
  }
  .wp-run.start:hover {
    background: #2f9e57;
  }
  .wp-run.stop {
    background: color-mix(in oklab, #c74848 85%, var(--bg-2));
  }
  .wp-run.stop:hover {
    background: #c74848;
  }
</style>
