<script lang="ts">
  // Network Watcher panel (PNetLab-style) — a compact floating card over the
  // canvas: up to 4 protocol filter rows (colour + protocol picker) and a
  // Start/Stop toggle. All state lives in watcherStore; FloatingEdge reads the
  // same store to draw the directional overlays, so this panel is pure chrome.
  import {
    watcherStore,
    LABELS,
    PROTO_ORDER,
    SUBTYPES,
    SUBTYPE_ANY,
    SUBTYPE_LABELS,
    type ProtoKey,
  } from "../watcherStore.svelte";

  const rows = $derived(watcherStore.rows);
  const running = $derived(watcherStore.running);

  // 16 preset flow colours — vivid hues that read as dashed strokes over the
  // cable colour on both themes. Shown in a popover grid next to the row's
  // swatch (the OS-native color dialog felt out of place); a 17th "custom"
  // cell still opens the native picker for anything beyond the presets.
  const SWATCHES = [
    "#b478e0", "#7c4dff", "#4fc3d9", "#2196f3",
    "#3f51b5", "#5fbf7a", "#2e7d32", "#cddc39",
    "#e0a63c", "#ff9800", "#ff5722", "#e53935",
    "#e91e63", "#9e9e9e", "#607d8b", "#8d6e63",
  ];
  /** Row id whose colour popover is open, or null. One at a time. */
  let pickerFor = $state<string | null>(null);
  function pick(rowId: string, color: string) {
    watcherStore.setColor(rowId, color);
    pickerFor = null;
  }
</script>

<svelte:window
  onpointerdown={(e) => {
    // Close the colour popover on any outside click (the popover and swatch
    // stop propagation via their own handlers' guard below).
    if (pickerFor !== null && !(e.target as HTMLElement).closest?.(".wp-picker, .wp-swatch")) {
      pickerFor = null;
    }
  }}
/>

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
        <div class="wp-row-group">
        <div class="wp-row">
          <!-- Row swatch opens a 16-colour preset popover (plus a native-input
               "custom" cell) anchored right next to it. -->
          <span class="wp-swatch-anchor">
            <button
              class="wp-swatch"
              style:background={row.color}
              title="Pick flow colour"
              aria-label="Row colour"
              aria-expanded={pickerFor === row.id}
              onclick={() => (pickerFor = pickerFor === row.id ? null : row.id)}
            ></button>
            {#if pickerFor === row.id}
              <div class="wp-picker" role="listbox" aria-label="Flow colour">
                {#each SWATCHES as c (c)}
                  <button
                    class="wp-picker-dot"
                    class:on={row.color.toLowerCase() === c}
                    style:background={c}
                    title={c}
                    aria-label={`Colour ${c}`}
                    onclick={() => pick(row.id, c)}
                  ></button>
                {/each}
                <!-- Custom: the native picker for anything beyond the presets. -->
                <label class="wp-picker-custom" title="Custom colour…">
                  <input
                    type="color"
                    value={row.color}
                    aria-label="Custom colour"
                    oninput={(e) => watcherStore.setColor(row.id, (e.currentTarget as HTMLInputElement).value)}
                  />
                  <span>+</span>
                </label>
              </div>
            {/if}
          </span>
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
          <!-- Subtype (packet-type) dropdown — only for protocols that have a
               sub-discriminator (BGP/ICMP/OSPF/EIGRP/ARP). Defaults to "any". -->
          {#if SUBTYPES[row.proto]}
            <select
              class="wp-select wp-subtype"
              value={row.subtype}
              aria-label="Packet type"
              title="Packet type (subtype filter)"
              onchange={(e) => watcherStore.setSubtype(row.id, (e.currentTarget as HTMLSelectElement).value)}
            >
              <option value={SUBTYPE_ANY}>any</option>
              {#each SUBTYPES[row.proto] ?? [] as st (st)}
                <option value={st}>{SUBTYPE_LABELS[st] ?? st}</option>
              {/each}
            </select>
          {/if}
          {#if rows.length > 1}
            <button
              class="wp-remove"
              title="Remove filter"
              aria-label="Remove filter"
              onclick={() => watcherStore.removeRow(row.id)}
            >✕</button>
          {/if}
        </div>

        <!-- Flow filters (src IP / dst IP / port). Rendered disabled: the
             backend `flows` per-tuple counter was deferred, so wiring these
             would silently no-op. Kept visible to match the reference UI and
             signal the capability is planned. -->
        <div class="wp-flow" title="Flow filters coming in a later build">
          <input
            class="wp-flow-input"
            type="text"
            placeholder="src IP"
            aria-label="Source IP (coming soon)"
            disabled
          />
          <input
            class="wp-flow-input"
            type="text"
            placeholder="dst IP"
            aria-label="Destination IP (coming soon)"
            disabled
          />
          <input
            class="wp-flow-input wp-flow-port"
            type="text"
            placeholder="port"
            aria-label="Port (coming soon)"
            disabled
          />
        </div>
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
  /* One filter = a protocol/subtype line stacked over its (disabled) flow line;
     rows separate with a hairline so the pairing reads clearly. */
  .wp-row-group {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .wp-row-group + .wp-row-group {
    padding-top: 6px;
    border-top: 1px solid var(--border);
  }
  .wp-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  /* Subtype select is secondary — narrower + slightly dimmer than the protocol
     picker so the protocol stays the row's primary control. */
  .wp-select.wp-subtype {
    flex: 0 1 88px;
    color: var(--ink-2);
  }
  /* Flow filters (src/dst/port), disabled until the backend flow table lands.
     Aligned under the protocol row, indented past the swatch. */
  .wp-flow {
    display: flex;
    gap: 6px;
    padding-left: 24px;
  }
  .wp-flow-input {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 3px 6px;
  }
  .wp-flow-port {
    flex: 0 0 52px;
  }
  .wp-flow-input:disabled {
    cursor: not-allowed;
    opacity: 0.5;
    background: var(--bg-2);
  }
  .wp-flow-input::placeholder {
    color: var(--ink-3);
  }
  /* Row swatch: a round dot showing the current colour; clicking toggles the
     preset popover anchored to it. */
  .wp-swatch-anchor {
    position: relative;
    display: inline-flex;
    flex-shrink: 0;
  }
  .wp-swatch {
    all: unset;
    box-sizing: border-box;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    cursor: pointer;
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .wp-swatch:hover,
  .wp-swatch[aria-expanded="true"] {
    box-shadow: 0 0 0 2px var(--accent);
  }
  /* 16-preset colour popover + a native-input "custom" cell. Anchored just
     left-below the swatch; overflows the panel edge intentionally (position
     absolute, high z) so all 17 cells stay visible in the 260px panel. */
  .wp-picker {
    position: absolute;
    top: 22px;
    left: -4px;
    z-index: 10;
    display: grid;
    grid-template-columns: repeat(6, 20px);
    gap: 5px;
    padding: 8px;
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
  }
  .wp-picker-dot {
    all: unset;
    box-sizing: border-box;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    cursor: pointer;
    box-shadow: 0 0 0 1px var(--border-strong);
  }
  .wp-picker-dot:hover {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .wp-picker-dot.on {
    box-shadow: 0 0 0 2px var(--ink);
  }
  /* Custom cell: a native color input hidden under a "+" dot so exotic colours
     stay reachable without the OS dialog dominating the UX. */
  .wp-picker-custom {
    position: relative;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    cursor: pointer;
    display: grid;
    place-items: center;
    color: var(--ink-2);
    font-size: 13px;
    font-weight: 600;
    box-shadow: 0 0 0 1px var(--border-strong);
    background: var(--bg-2);
  }
  .wp-picker-custom:hover {
    box-shadow: 0 0 0 2px var(--accent);
    color: var(--ink);
  }
  .wp-picker-custom input {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    opacity: 0;
    cursor: pointer;
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
