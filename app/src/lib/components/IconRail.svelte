<script lang="ts">
  import { tick } from "svelte";
  import { annoTool } from "../annoTool.svelte";
  import { chromeStore } from "../chromeStore.svelte";
  import { uiSvg } from "../icons.svelte";
  import { railUiStore, type RailPanel } from "../railUiStore.svelte";

  const RAIL_ITEMS = [
    { key: "nodes", label: "Add Nodes", icon: "plus", panel: "nodes" as RailPanel },
    // "Node Actions" opens Start all/Stop all/Save configs/Console all/
    // Force clean/Wipe all — session-wide run controls, not a checklist.
    { key: "actions", label: "Node Actions", icon: "play", panel: "actions" as RailPanel },
    { key: "text", label: "Add Text", icon: "edit" },
    // Was "net" (a fork/branch glyph meant for the network-watcher chrome)
    // reused here by accident — this button opens shape-drawing tools, so
    // it needs an actual shapes glyph.
    { key: "shapes", label: "Add Shapes", icon: "shapes", panel: "shapes" as RailPanel },
    // Was "more" (3-dot), which the top bar's own overflow button already
    // uses for "more options" — reusing it here read as a second overflow
    // menu rather than "Tools" (network watcher / topology painter).
    { key: "tools", label: "Tools", icon: "wrench", panel: "tools" as RailPanel },
  ] as const;

  let activeIndex = $state(0);
  let buttonEls: Array<HTMLButtonElement | undefined> = $state([]);

  function isPressed(item: (typeof RAIL_ITEMS)[number]) {
    if (item.key === "text") return annoTool.active === "text";
    return railUiStore.open === item.panel;
  }

  function activate(item: (typeof RAIL_ITEMS)[number]) {
    if (item.key === "text") {
      annoTool.arm("text");
      railUiStore.close();
      return;
    }
    railUiStore.toggle(item.panel);
  }

  function moveFocus(index: number) {
    activeIndex = (index + RAIL_ITEMS.length) % RAIL_ITEMS.length;
    void tick().then(() => buttonEls[activeIndex]?.focus());
  }

  function onKeydown(event: KeyboardEvent, index: number) {
    if (event.key === "ArrowDown" || event.key === "ArrowRight") {
      event.preventDefault();
      moveFocus(index + 1);
    } else if (event.key === "ArrowUp" || event.key === "ArrowLeft") {
      event.preventDefault();
      moveFocus(index - 1);
    } else if (event.key === "Home") {
      event.preventDefault();
      moveFocus(0);
    } else if (event.key === "End") {
      event.preventDefault();
      moveFocus(RAIL_ITEMS.length - 1);
    }
  }
</script>

<div class="icon-rail" class:chrome-hidden={chromeStore.hidden} data-chrome-surface role="toolbar" aria-label="Lab tools" aria-orientation="vertical">
  {#each RAIL_ITEMS as item, index (item.key)}
    <button
      class="rail-button"
      class:active={isPressed(item)}
      title={item.label}
      aria-label={item.label}
      aria-pressed={isPressed(item)}
      tabindex={activeIndex === index ? 0 : -1}
      bind:this={buttonEls[index]}
      onclick={() => activate(item)}
      onkeydown={(event) => onKeydown(event, index)}
    >
      {@html uiSvg(item.icon, 18)}
      <span class="tooltip" role="tooltip" aria-hidden="true">{item.label}</span>
    </button>
  {/each}
</div>

<style>
  .icon-rail {
    flex: 0 0 52px;
    width: 52px;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 8px 6px;
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border-right: 1px solid var(--border);
    transition: transform var(--transition-fast), opacity var(--transition-fast);
  }
  .icon-rail.chrome-hidden {
    transform: translateY(-100%);
    opacity: 0;
    pointer-events: none;
  }
  .rail-button {
    position: relative;
    flex: 0 0 40px;
    width: 40px;
    height: 40px;
    display: grid;
    place-items: center;
    padding: 0;
    color: var(--ink-2);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: color var(--transition-fast), background var(--transition-fast),
      border-color var(--transition-fast);
  }
  .rail-button:hover,
  .rail-button:focus-visible {
    color: var(--ink);
    background: var(--bg-hover);
    border-color: var(--border-strong);
  }
  .rail-button.active {
    color: var(--accent);
    background: var(--accent-muted);
    border-color: var(--accent);
  }
  .rail-button :global(svg) {
    width: 18px;
    height: 18px;
    pointer-events: none;
  }
  .tooltip {
    position: absolute;
    left: calc(100% + 8px);
    top: 50%;
    z-index: var(--z-tooltip);
    padding: 5px 8px;
    color: var(--ink);
    background: var(--panel-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-sm);
    font-size: var(--fs-xs);
    white-space: nowrap;
    opacity: 0;
    pointer-events: none;
    transform: translate(0, -50%);
    transition: opacity var(--transition-fast), transform var(--transition-fast);
  }
  .rail-button:hover .tooltip,
  .rail-button:focus-visible .tooltip {
    opacity: 1;
    transform: translate(2px, -50%);
  }
  @media (prefers-reduced-motion: reduce) {
    .icon-rail,
    .rail-button,
    .tooltip {
      transition: none;
    }
  }
</style>
