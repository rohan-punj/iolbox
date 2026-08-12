<script lang="ts">
  // D4 — icon picker popover. A grid of bundled + imported glyphs plus an
  // "Import icon…" action (SVG/PNG). Opened from the node context menu
  // ("Change icon…") and from the Inspector. Writing the chosen key onto the
  // node is delegated to the caller via onPick.
  import { onMount } from "svelte";
  import { chromeStore } from "../chromeStore.svelte";
  import { listIcons, iconSvg, importIconFromFile, iconRegistryVersion } from "../icons.svelte";

  let {
    x,
    y,
    current,
    onPick,
    onClose,
  }: {
    x: number;
    y: number;
    current?: string;
    onPick: (key: string) => void;
    onClose: () => void;
  } = $props();
  $effect(() => chromeStore.hold());

  let el: HTMLDivElement | undefined = $state();
  let fileInput: HTMLInputElement | undefined = $state();
  let importing = $state(false);
  // Ignore the very click that opened the picker (it bubbles to window).
  let armed = $state(false);
  onMount(() => {
    const t = setTimeout(() => (armed = true), 0);
    return () => clearTimeout(t);
  });

  const icons = $derived((iconRegistryVersion(), listIcons()));

  function handleWindowDown(e: MouseEvent) {
    if (!armed) return;
    if (el && !el.contains(e.target as Node)) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  async function onFile(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (!file) return;
    importing = true;
    try {
      const key = await importIconFromFile(file);
      if (key) onPick(key);
    } finally {
      importing = false;
      if (fileInput) fileInput.value = "";
    }
  }

  // Clamp within viewport.
  const px = $derived(Math.max(8, Math.min(x, window.innerWidth - 214)));
  const py = $derived(Math.max(56, Math.min(y, window.innerHeight - 260)));
</script>

<svelte:window onmousedown={handleWindowDown} onkeydown={handleKey} />

<div class="picker" bind:this={el} style:left={`${px}px`} style:top={`${py}px`} role="dialog" aria-label="Choose node icon">
  <div class="ph">Node icon</div>
  <div class="grid">
    {#each icons as ico (ico.key)}
      <button
        class="swatch"
        class:on={current === ico.key}
        title={ico.label}
        onclick={() => onPick(ico.key)}
      >
        {@html iconSvg(ico.key, 20)}
      </button>
    {/each}
  </div>
  <button class="btn import" onclick={() => fileInput?.click()} disabled={importing}>
    {importing ? "Importing…" : "Import icon…"}
  </button>
  <input
    bind:this={fileInput}
    type="file"
    accept="image/svg+xml,image/png,.svg,.png"
    onchange={onFile}
    hidden
  />
</div>

<style>
  .picker {
    position: fixed;
    z-index: 1200;
    width: 198px;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 10px;
  }
  .ph {
    font-size: var(--fs-xs);
    letter-spacing: var(--ls-eyebrow);
    text-transform: uppercase;
    color: var(--ink-3);
    font-weight: 650;
    margin-bottom: 8px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 6px;
  }
  .swatch {
    all: unset;
    box-sizing: border-box;
    aspect-ratio: 1;
    border-radius: 8px;
    background: var(--node-face-2);
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
    cursor: pointer;
    color: var(--ink-2);
  }
  .swatch:hover {
    border-color: var(--accent);
    color: var(--ink);
  }
  .swatch.on {
    border-color: var(--accent);
    color: var(--accent);
    background: color-mix(in oklab, var(--accent) 12%, transparent);
  }
  .swatch :global(svg),
  .swatch :global(img) {
    width: 20px;
    height: 20px;
  }
  .import {
    margin-top: 8px;
    width: 100%;
    justify-content: center;
  }
</style>
