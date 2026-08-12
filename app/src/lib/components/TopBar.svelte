<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { consoleUiStore } from "../consoleUiStore.svelte";
  import { macUiStore } from "../macUiStore.svelte";
  import { chromeStore } from "../chromeStore.svelte";
  import { uiSvg } from "../icons.svelte";
  import { emptyLab, type LabDocument } from "../labTypes";
  import { importClab, exportClab } from "../clab";
  import { labToYaml, labFromText } from "../yaml";
  import { load as yamlLoad } from "js-yaml";
  import ContextMenu, { type MenuItem } from "./ContextMenu.svelte";
  import { tick } from "svelte";

  const anyRunning = $derived(labStore.labRunning);
  const providerLabel = $derived(
    labStore.activeProvider ? labStore.activeProvider : "—"
  );
  const nodeCount = $derived(labStore.lab.nodes.length);

  let importInput: HTMLInputElement | undefined = $state();
  let menuButton: HTMLButtonElement | undefined = $state();
  let menuOpen = $state(false);
  let menuAnchor = $state({ x: 0, y: 0 });

  type SaveState = "saved" | "saving" | "unsaved";
  type AutosaveState = { autosaveTimer: ReturnType<typeof setTimeout> | null };
  const pendingAutosave = $derived.by(() => {
    // nowTick is the existing one-second lab clock; it makes the private
    // debounce timer observable here without changing labStore's API.
    void labStore.nowTick;
    return (labStore as unknown as AutosaveState).autosaveTimer !== null;
  });
  const saveState = $derived.by((): SaveState => {
    if (pendingAutosave) return "saving";
    return labStore.lastSavedAt === null ? "unsaved" : "saved";
  });

  // Fullscreen toggle. Escape already exits fullscreen natively (the
  // Fullscreen API's own browser behavior — no key handler needed here).
  // Tracked via the fullscreenchange event, not just the click handler's own
  // state flip, so the icon/label stay correct if the user exits with Escape,
  // F11, or the browser's own UI instead of this button.
  let isFullscreen = $state(false);
  function syncFullscreen() {
    isFullscreen = document.fullscreenElement !== null;
  }
  function toggleFullscreen() {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void document.documentElement.requestFullscreen();
    }
  }

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
  // runtime reset). openLab warns via SwitchLabDialog since this always
  // replaces the current lab with a different (freshly-uuid'd) one.
  async function newLab() {
    await labStore.openLab(emptyLab("Untitled lab"));
  }

  async function save() {
    await labStore.saveLab();
  }

  function closeMenu() {
    menuOpen = false;
    void tick().then(() => menuButton?.focus());
  }

  function toggleMenu() {
    if (menuOpen) {
      closeMenu();
      return;
    }
    const rect = menuButton?.getBoundingClientRect();
    if (!rect) return;
    const menuWidth = 240;
    const margin = 8;
    menuAnchor = {
      x: Math.max(margin, Math.min(window.innerWidth - menuWidth - margin, rect.right - menuWidth)),
      y: rect.bottom + 4,
    };
    menuOpen = true;
  }

  function overflowItems(): MenuItem[] {
    const separator = (id: string, label: string): MenuItem => ({
      id,
      label,
      separator: true,
      action: () => {},
    });
    return [
      // Lab
      {
        id: "lab-new",
        label: "New",
        disabled: labStore.labLoading,
        title: "Start a new empty lab",
        action: newLab,
      },
      {
        id: "lab-browser",
        label: "Labs…",
        disabled: labStore.labLoading,
        action: () => (labStore.showLabBrowser = true),
      },
      { id: "lab-save", label: "Save", title: "Save lab to the store", action: save },
      {
        id: "lab-tasks",
        label: "Tasks",
        checked: labStore.showTasks,
        title: "Lab tasks / instructions",
        action: () => (labStore.showTasks = !labStore.showTasks),
      },
      separator("group-import-export", "Import / export"),
      {
        id: "export-yaml",
        label: "Export YAML",
        title: "Export lab as YAML (.yml)",
        action: exportYaml,
      },
      {
        id: "export-containerlab",
        label: "Export containerlab",
        title: "Export as containerlab .clab.yml",
        action: exportClabFile,
      },
      {
        id: "import-lab",
        label: "Import…",
        title: "Import lab (.yml native, containerlab .clab.yml, or legacy .json)",
        action: pickImport,
      },
      separator("group-library", "Library"),
      {
        id: "library-images",
        label: "Images…",
        action: () => (labStore.showImageManager = true),
      },
      separator("group-view", "View"),
      {
        id: "view-return-consoles-to-dock",
        label: "Return consoles to dock",
        disabled: consoleUiStore.placement === "dock",
        title: "Switch floating console windows back to the dock",
        action: () => consoleUiStore.setPlacement("dock"),
      },
      {
        // Theme, Console mode, Auto-hide chrome, and Snap grid used to be
        // loose items/submenus right here — consolidated into one dialog
        // (SettingsDialog.svelte) alongside console dock/colorize/font-size
        // prefs that had no home in this menu at all. Kept OUT of that
        // dialog: "Detect IOL MAC addresses" (a data-observation behavior,
        // not a view preference) and "Link layout" below (a per-lab-document
        // canvas property, not a GUI preference).
        id: "view-settings",
        label: "Settings…",
        action: () => (labStore.showSettings = true),
      },
      {
        id: "view-learned-mac-display",
        // Static label, checkmark conveys state — matches every other
        // checkable item in this menu (Auto-hide chrome, Snap grid,
        // Free/Structured). Was "Learned IOL MAC display: on/off", which
        // both baked the state into the text AND had the on/off tooltips
        // swapped (off's tooltip described what on actually does).
        label: "Detect IOL MAC addresses",
        checked: macUiStore.learnIol,
        title: macUiStore.learnIol
          ? "IOL MAC addresses are inferred from observed live traffic (VPCS/PC addresses are always shown directly)"
          : "IOL MAC addresses are not shown — VPCS/PC addresses are always shown directly",
        action: () => macUiStore.toggleLearnIol(),
      },
      separator("group-canvas", "Canvas"),
      {
        id: "canvas-link-layout",
        label: "Link layout",
        action: () => {},
        submenu: [
          {
            id: "canvas-link-layout-free",
            label: "Free",
            checked: (labStore.lab.canvas?.linkLayout ?? "free") === "free",
            action: () => {
              const canvas = labStore.lab.canvas ?? (labStore.lab.canvas = {});
              canvas.linkLayout = "free";
              labStore.scheduleAutosave();
            },
          },
          {
            id: "canvas-link-layout-structured",
            label: "Structured",
            checked: labStore.lab.canvas?.linkLayout === "structured",
            action: () => {
              const canvas = labStore.lab.canvas ?? (labStore.lab.canvas = {});
              canvas.linkLayout = "structured";
              labStore.scheduleAutosave();
            },
          },
        ],
      },
    ];
  }

  function download(text: string, filename: string, mime: string) {
    const blob = new Blob([text], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  }
  const safeName = () => (labStore.lab.name || "lab").replace(/[^\w.-]+/g, "_");

  // Export the current doc as a downloadable .yml file (iolbox's native format).
  function exportYaml() {
    download(labToYaml($state.snapshot(labStore.lab)), `${safeName()}.yml`, "text/yaml");
  }
  // Export the current doc as a containerlab .clab.yml file.
  function exportClabFile() {
    download(exportClab($state.snapshot(labStore.lab)), `${safeName()}.clab.yml`, "text/yaml");
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
      const text = await file.text();
      // openLab (below) warns via SwitchLabDialog when the imported doc
      // (always a fresh uuid) replaces a different current lab.
      // A YAML file is either a containerlab topology (has a top-level
      // `topology:` key) or a native iolbox lab; JSON is a legacy iolbox lab.
      const isJson = text.trimStart().startsWith("{");
      const parsed = isJson ? null : (yamlLoad(text) as Record<string, unknown> | null);
      if (parsed && typeof parsed === "object" && "topology" in parsed) {
        const { doc, warnings } = importClab(text);
        await labStore.openLab(doc);
        if (warnings.length) {
          alert(`Imported "${doc.name}" from containerlab.\n\nNotes:\n` + warnings.map((w) => "• " + w).join("\n"));
        }
      } else {
        await labStore.openLab(labFromText(text));
      }
    } catch (err) {
      labStore.lastError = `import failed: ${(err as Error).message}`;
    }
  }
</script>

<svelte:window onfullscreenchange={syncFullscreen} />

<header class="topbar" class:chrome-hidden={chromeStore.hidden} data-chrome-surface data-provider={providerLabel}>
  <div class="brand">
    <span class="brand-mark">{@html uiSvg("net", 13)}</span>
    <input
      class="lab-name mono"
      value={labStore.lab.name}
      oninput={updateName}
      aria-label="Lab name"
      spellcheck="false"
    />
  </div>

  <div class="status-pair" aria-label="Lab status">
    <span class="pill status-pill save-status" class:saved={saveState === "saved"} class:saving={saveState === "saving"} class:unsaved={saveState === "unsaved"}>
      <span class="led"></span>
      {saveState === "saved" ? "Saved" : saveState === "saving" ? "Saving…" : "Unsaved"}
    </span>
    <span class="pill status-pill node-status" class:running={anyRunning} class:stopped={!anyRunning}>
      <span class="led"></span>
      <span class="mono">{nodeCount}</span> {nodeCount === 1 ? "node" : "nodes"}
    </span>
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

  <input
    bind:this={importInput}
    type="file"
    accept="application/json,.json,.yml,.yaml,.clab.yml"
    style="display:none"
    onchange={onImportFile}
  />

  <button
    class="btn"
    aria-pressed={isFullscreen}
    aria-label={isFullscreen ? "Exit fullscreen" : "Enter fullscreen"}
    title={isFullscreen ? "Exit fullscreen (Esc)" : "Enter fullscreen"}
    onclick={toggleFullscreen}
  >
    {@html uiSvg(isFullscreen ? "fullscreenExit" : "fullscreen", 13)}
  </button>

  <button
    bind:this={menuButton}
    class="btn"
    aria-expanded={menuOpen}
    aria-haspopup="menu"
    aria-label="More actions"
    title="More actions"
    onclick={toggleMenu}
  >
    {@html uiSvg("more", 15)}
  </button>

  {#if menuOpen}
    <ContextMenu
      x={menuAnchor.x}
      y={menuAnchor.y}
      items={overflowItems()}
      dismissOnWheel={false}
      onClose={closeMenu}
    />
  {/if}

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
    z-index: var(--z-topbar);
    transition: transform var(--transition-fast), opacity var(--transition-fast);
  }
  .topbar.chrome-hidden {
    transform: translateY(-100%);
    opacity: 0;
    pointer-events: none;
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
  .status-pair {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    flex-shrink: 0;
  }
  .spacer {
    flex: 1;
  }
  .error-pill {
    border: 1px solid color-mix(in oklab, var(--state-crashed) 55%, transparent);
    background: color-mix(in oklab, var(--state-crashed) 14%, transparent);
    color: var(--danger);
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
  .save-status.saved .led,
  .node-status.running .led {
    background: var(--state-running);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--state-running) 22%, transparent);
  }
  .save-status.saving .led {
    background: var(--state-starting);
  }
  .node-status.stopped .led,
  .save-status.unsaved .led {
    background: var(--state-stopped);
  }

  .btn :global(svg) {
    width: 13px;
    height: 13px;
  }
  @media (prefers-reduced-motion: reduce) {
    .topbar {
      transition: none;
    }
  }
</style>
