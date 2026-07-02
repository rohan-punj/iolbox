<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { themeStore } from "../themeStore.svelte";
  import { uiSvg } from "../icons.svelte";
  import { emptyLab, type LabDocument } from "../labTypes";

  const anyRunning = $derived(labStore.labRunning);
  const providerLabel = $derived(
    labStore.activeProvider ? labStore.activeProvider : "—"
  );
  const nodeCount = $derived(labStore.lab.nodes.length);

  // Brief "Saved ✓" confirmation after a manual save.
  let justSaved = $state(false);
  let importInput: HTMLInputElement | undefined = $state();

  function updateName(e: Event) {
    labStore.lab.name = (e.target as HTMLInputElement).value;
  }

  async function toggleLab() {
    if (anyRunning) {
      await labStore.stopLab();
    } else {
      await labStore.startLab();
    }
  }

  // Start a fresh empty lab through the same open/load path (image reconcile +
  // runtime reset). Guard against discarding unsaved work.
  async function newLab() {
    if (labStore.lab.nodes.length > 0 && !labStore.currentLabSaved) {
      if (!confirm("The current lab hasn't been saved. Discard it and start a new lab?")) return;
    }
    await labStore.openLab(emptyLab("Untitled lab"));
  }

  async function save() {
    const ok = await labStore.saveLab();
    if (ok) {
      justSaved = true;
      setTimeout(() => (justSaved = false), 1600);
    }
  }

  // Export the current doc as a downloadable .json file.
  function exportJson() {
    const doc = JSON.stringify($state.snapshot(labStore.lab), null, 2);
    const blob = new Blob([doc], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${(labStore.lab.name || "lab").replace(/[^\w.-]+/g, "_")}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function pickImport() {
    importInput?.click();
  }
  async function onImportFile(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ""; // allow re-importing the same file
    if (!file) return;
    try {
      const doc = JSON.parse(await file.text()) as LabDocument;
      if (!doc || !Array.isArray(doc.nodes) || !Array.isArray(doc.links)) {
        throw new Error("not a lab document");
      }
      if (labStore.lab.nodes.length > 0 && !labStore.currentLabSaved) {
        if (!confirm("The current lab hasn't been saved. Discard it and import this file?")) return;
      }
      await labStore.openLab(doc);
    } catch (err) {
      labStore.lastError = `import failed: ${(err as Error).message}`;
    }
  }
</script>

<header class="topbar">
  <div class="brand">
    <span class="brand-mark">{@html uiSvg("net", 13)}</span>
    <input
      class="lab-name mono"
      value={labStore.lab.name}
      oninput={updateName}
      aria-label="Lab name"
      spellcheck="false"
    />
    <span class="dim mono">· {nodeCount} {nodeCount === 1 ? "node" : "nodes"}</span>
  </div>

  <div class="spacer"></div>

  {#if labStore.lastError}
    <button
      class="pill error-pill"
      title="Dismiss"
      onclick={() => (labStore.lastError = null)}
    >
      {labStore.lastError}
    </button>
  {/if}

  <span
    class="pill status-pill"
    class:connected={labStore.providerStatus === "connected"}
    class:connecting={labStore.providerStatus === "connecting"}
    class:error={labStore.providerStatus === "error"}
  >
    <span class="led"></span>
    {providerLabel}
  </span>

  <div class="seg" role="group" aria-label="Theme">
    <button
      class:on={themeStore.current === "bench"}
      aria-pressed={themeStore.current === "bench"}
      onclick={() => themeStore.set("bench")}>Bench</button
    >
    <button
      class:on={themeStore.current === "glass"}
      aria-pressed={themeStore.current === "glass"}
      onclick={() => themeStore.set("glass")}>Glass</button
    >
  </div>

  <button class="btn" onclick={newLab} title="Start a new empty lab">
    {@html uiSvg("plus", 13)} New
  </button>

  <button class="btn" onclick={() => (labStore.showLabBrowser = true)}>
    {@html uiSvg("folder", 13)} Labs
  </button>

  <button class="btn" class:saved={justSaved} onclick={save} title="Save lab to the store">
    {@html uiSvg("save", 13)} {justSaved ? "Saved ✓" : "Save"}
  </button>

  <div class="seg io-seg" role="group" aria-label="Import / export">
    <button title="Export lab as JSON" aria-label="Export JSON" onclick={exportJson}>
      {@html uiSvg("download", 13)}
    </button>
    <button title="Import lab from JSON" aria-label="Import JSON" onclick={pickImport}>
      {@html uiSvg("upload", 13)}
    </button>
  </div>
  <input
    bind:this={importInput}
    type="file"
    accept="application/json,.json"
    style="display:none"
    onchange={onImportFile}
  />

  <button class="btn" onclick={() => (labStore.showImageManager = true)}>
    {@html uiSvg("images", 13)} Images
  </button>

  <button class="btn btn-primary" onclick={toggleLab}>
    {@html uiSvg(anyRunning ? "stop" : "play", 12)}
    {anyRunning ? "Stop lab" : "Start lab"}
  </button>
</header>

<style>
  .topbar {
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 0 var(--sp-3);
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    z-index: 5;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .brand-mark {
    width: 22px;
    height: 22px;
    border-radius: var(--radius-sm);
    display: grid;
    place-items: center;
    background: linear-gradient(
      150deg,
      var(--accent),
      color-mix(in oklab, var(--accent) 55%, #7a5cff)
    );
    color: var(--accent-ink);
    flex-shrink: 0;
  }
  .lab-name {
    background: transparent;
    border: 1px solid transparent;
    font-size: var(--fs-base);
    color: var(--ink);
    padding: 5px 8px;
    min-width: 120px;
    max-width: 260px;
    letter-spacing: 0.01em;
  }
  .lab-name:hover {
    border-color: var(--border);
  }
  .lab-name:focus {
    background: var(--panel-solid);
    border-color: var(--accent);
  }
  .dim {
    font-size: var(--fs-xs);
    color: var(--ink-3);
    white-space: nowrap;
    flex-shrink: 0;
  }
  .spacer {
    flex: 1;
  }
  .error-pill {
    border: 1px solid color-mix(in oklab, var(--state-crashed) 55%, transparent);
    background: color-mix(in oklab, var(--state-crashed) 14%, transparent);
    color: var(--state-crashed);
    font-size: var(--fs-xs);
    font-family: var(--font-ui);
    padding: 4px 10px;
    border-radius: var(--radius-full);
    cursor: pointer;
    max-width: 380px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status-pill .led {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--state-stopped);
  }
  .status-pill.connected .led {
    background: var(--state-running);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-running) 22%, transparent);
  }
  .status-pill.connecting .led {
    background: var(--state-starting);
  }
  .status-pill.error .led {
    background: var(--state-crashed);
  }

  .seg {
    display: inline-flex;
    padding: 3px;
    gap: 2px;
    background: var(--panel-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-full);
  }
  .seg button {
    font-family: var(--font-ui);
    font-size: var(--fs-xs);
    font-weight: 600;
    border: 0;
    background: transparent;
    color: var(--ink-3);
    padding: 4px 12px;
    border-radius: var(--radius-full);
    cursor: pointer;
    letter-spacing: 0.02em;
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .seg button:hover {
    color: var(--ink);
  }
  .seg button.on {
    background: var(--accent);
    color: var(--accent-ink);
  }

  .btn :global(svg) {
    width: 13px;
    height: 13px;
  }
  .btn.saved {
    color: var(--state-running);
    border-color: color-mix(in oklab, var(--state-running) 55%, transparent);
  }
  .io-seg {
    padding: 3px;
  }
  .io-seg button {
    display: grid;
    place-items: center;
    padding: 4px 8px;
    color: var(--ink-3);
  }
  .io-seg button:hover {
    color: var(--ink);
  }
  .io-seg :global(svg) {
    width: 13px;
    height: 13px;
  }
</style>
