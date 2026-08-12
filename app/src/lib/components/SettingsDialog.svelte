<script lang="ts">
  // Consolidates the GUI-only view preferences that used to be scattered
  // loose in the overflow menu (Auto-hide chrome, Theme, Console mode) plus
  // Snap grid and the console dock/colorize/font-size prefs that had no home
  // in that menu at all. Everything here already auto-persists to
  // localStorage (or, for Snap grid, the lab document) the instant it's
  // changed — same as before this dialog existed — so the Save button is a
  // dismiss action, not a commit step; nothing reverts on Cancel/Escape
  // because there's no draft state to discard.
  import { labStore } from "../labStore.svelte";
  import { chromeStore } from "../chromeStore.svelte";
  import { themeStore } from "../themeStore.svelte";
  import { macUiStore } from "../macUiStore.svelte";
  import { consoleUiStore, FONT_MIN, FONT_MAX } from "../consoleUiStore.svelte";

  let { onClose }: { onClose: () => void } = $props();

  function toggleSnapGrid() {
    const canvas = labStore.lab.canvas ?? (labStore.lab.canvas = {});
    canvas.snapGrid = !canvas.snapGrid;
    labStore.scheduleAutosave();
  }

  function setLinkLayout(layout: "free" | "structured") {
    const canvas = labStore.lab.canvas ?? (labStore.lab.canvas = {});
    canvas.linkLayout = layout;
    labStore.scheduleAutosave();
  }
  const linkLayout = $derived(labStore.lab.canvas?.linkLayout ?? "free");

  function onScrimDown(e: MouseEvent) {
    if (e.target === e.currentTarget) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window onkeydown={handleKey} />

<div class="scrim" role="presentation" onmousedown={onScrimDown}>
  <div class="dialog" role="dialog" aria-label="Settings" aria-modal="true">
    <h3>Settings</h3>

    <section>
      <h4>Appearance</h4>
      <div class="row">
        <span class="row-label">Theme</span>
        <div class="segmented">
          <button class:on={themeStore.current === "bench"} aria-pressed={themeStore.current === "bench"} onclick={() => themeStore.set("bench")}>Bench</button>
          <button class:on={themeStore.current === "glass"} aria-pressed={themeStore.current === "glass"} onclick={() => themeStore.set("glass")}>Glass</button>
        </div>
      </div>
      <label class="row toggle-row">
        <span class="row-copy">
          <span class="row-label">Auto-hide chrome</span>
          <span class="row-hint">Hide the top bar, rail, and resource bar after 2s idle</span>
        </span>
        <input type="checkbox" checked={chromeStore.enabled} onchange={() => chromeStore.toggleEnabled()} />
      </label>
    </section>

    <section>
      <h4>Canvas</h4>
      <div class="row">
        <span class="row-label">Link layout</span>
        <div class="segmented">
          <button class:on={linkLayout === "free"} aria-pressed={linkLayout === "free"} onclick={() => setLinkLayout("free")}>Free</button>
          <button class:on={linkLayout === "structured"} aria-pressed={linkLayout === "structured"} onclick={() => setLinkLayout("structured")}>Structured</button>
        </div>
      </div>
      <label class="row toggle-row">
        <span class="row-copy">
          <span class="row-label">Snap grid</span>
          <span class="row-hint">Snap dragged nodes to the 20px canvas grid</span>
        </span>
        <input type="checkbox" checked={labStore.lab.canvas?.snapGrid ?? false} onchange={toggleSnapGrid} />
      </label>
      <label class="row toggle-row">
        <span class="row-copy">
          <span class="row-label">Detect IOL MAC addresses</span>
          <span class="row-hint">Infer IOL addresses from observed live traffic (VPCS/PC addresses always show directly)</span>
        </span>
        <input type="checkbox" checked={macUiStore.learnIol} onchange={() => macUiStore.toggleLearnIol()} />
      </label>
    </section>

    <section>
      <h4>Console</h4>
      <div class="row">
        <span class="row-label">Open mode</span>
        <div class="segmented">
          <button class:on={consoleUiStore.consoleMode === "web"} aria-pressed={consoleUiStore.consoleMode === "web"} onclick={() => consoleUiStore.setConsoleMode("web")}>Web</button>
          <button class:on={consoleUiStore.consoleMode === "native"} aria-pressed={consoleUiStore.consoleMode === "native"} onclick={() => consoleUiStore.setConsoleMode("native")}>Native</button>
        </div>
      </div>
      <div class="row">
        <span class="row-label">Default placement</span>
        <div class="segmented">
          <button class:on={consoleUiStore.placement === "dock"} aria-pressed={consoleUiStore.placement === "dock"} onclick={() => consoleUiStore.setPlacement("dock")}>Dock</button>
          <button class:on={consoleUiStore.placement === "float"} aria-pressed={consoleUiStore.placement === "float"} onclick={() => consoleUiStore.setPlacement("float")}>Float</button>
        </div>
      </div>
      <div class="row">
        <span class="row-label">Dock side</span>
        <div class="segmented">
          <button class:on={consoleUiStore.dockSide === "bottom"} aria-pressed={consoleUiStore.dockSide === "bottom"} onclick={() => consoleUiStore.setDockSide("bottom")}>Bottom</button>
          <button class:on={consoleUiStore.dockSide === "right"} aria-pressed={consoleUiStore.dockSide === "right"} onclick={() => consoleUiStore.setDockSide("right")}>Right</button>
        </div>
      </div>
      <label class="row toggle-row">
        <span class="row-copy">
          <span class="row-label">Colorize console text</span>
          <span class="row-hint">Highlight IOS prompts/keywords in console output</span>
        </span>
        <input type="checkbox" checked={consoleUiStore.colorize} onchange={() => consoleUiStore.toggleColorize()} />
      </label>
      <div class="row">
        <span class="row-label">Text size ({consoleUiStore.fontSize}px)</span>
        <div class="segmented font-ctl">
          <button
            disabled={consoleUiStore.fontSize <= FONT_MIN}
            aria-label="Decrease console text size"
            onclick={() => consoleUiStore.bumpFontSize(-1)}
          >A−</button>
          <button
            disabled={consoleUiStore.fontSize >= FONT_MAX}
            aria-label="Increase console text size"
            onclick={() => consoleUiStore.bumpFontSize(1)}
          >A+</button>
        </div>
      </div>
    </section>

    <div class="actions">
      <button class="btn btn-primary" onclick={onClose}>Save</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: var(--z-dialog);
    display: grid;
    place-items: center;
    background: rgba(4, 8, 13, 0.5);
    -webkit-backdrop-filter: blur(3px);
    backdrop-filter: blur(3px);
  }
  .dialog {
    width: min(420px, 92vw);
    max-height: 86vh;
    overflow-y: auto;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 20px;
  }
  h3 {
    margin: 0 0 14px;
    font-size: 15px;
  }
  section {
    margin-bottom: 16px;
  }
  section:last-of-type {
    margin-bottom: 0;
  }
  h4 {
    margin: 0 0 8px;
    color: var(--text-secondary);
    font-size: var(--fs-xs);
    font-weight: 650;
    letter-spacing: var(--ls-eyebrow, 0.04em);
    text-transform: uppercase;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 8px;
    margin: 0 -8px;
    border-radius: var(--radius-sm);
  }
  .toggle-row {
    cursor: pointer;
    transition: background var(--transition-fast);
  }
  .toggle-row:hover {
    background: var(--bg-hover);
  }
  .toggle-row:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .row-copy {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .row-label {
    font-size: 13px;
    color: var(--text-primary);
  }
  .row-hint {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
  }
  .segmented {
    display: inline-flex;
    flex: 0 0 auto;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .segmented button {
    all: unset;
    box-sizing: border-box;
    padding: 4px 10px;
    color: var(--text-secondary);
    font-size: var(--fs-xs);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .segmented button + button {
    border-left: 1px solid var(--border-strong);
  }
  .segmented button:hover:not(.on):not(:disabled) {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
  .segmented button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .segmented button.on {
    color: var(--bg-0);
    background: var(--accent);
  }
  .segmented button:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .font-ctl button {
    min-width: 28px;
    font-family: var(--font-mono);
    font-weight: 600;
    text-align: center;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }
  @media (prefers-reduced-motion: reduce) {
    .segmented button,
    .toggle-row {
      transition: none;
    }
  }
</style>
