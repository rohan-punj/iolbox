<script lang="ts">
  export interface MenuItem {
    label: string;
    action: () => void;
    danger?: boolean;
    disabled?: boolean;
    separator?: boolean;
  }

  let {
    x,
    y,
    items,
    onClose,
  }: { x: number; y: number; items: MenuItem[]; onClose: () => void } = $props();

  import { onMount } from "svelte";

  let menuEl: HTMLDivElement | undefined = $state();

  function handleClick(item: MenuItem) {
    if (item.disabled) return;
    item.action();
    onClose();
  }

  // Dismiss on any pointerdown outside the menu. Uses a CAPTURE-phase document
  // listener rather than <svelte:window onmousedown> because Svelte Flow calls
  // stopPropagation() on pointer events inside the pane, which would otherwise
  // swallow the dismiss when the user clicks back onto the canvas. Also dismiss
  // when the canvas starts panning/zooming (wheel / pane drag) so the menu never
  // floats detached from the node it belongs to.
  function handlePointerDown(e: PointerEvent) {
    if (menuEl && !menuEl.contains(e.target as Node)) onClose();
  }
  function handleWheel() {
    onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  onMount(() => {
    // Defer attaching until after the current event loop tick so the very
    // right-click/contextmenu event that opened this menu doesn't immediately
    // close it.
    const attach = () => {
      document.addEventListener("pointerdown", handlePointerDown, true);
      document.addEventListener("wheel", handleWheel, true);
    };
    const t = setTimeout(attach, 0);
    return () => {
      clearTimeout(t);
      document.removeEventListener("pointerdown", handlePointerDown, true);
      document.removeEventListener("wheel", handleWheel, true);
    };
  });
</script>

<svelte:window onkeydown={handleKey} />

<div class="menu" bind:this={menuEl} style:left={`${x}px`} style:top={`${y}px`} role="menu">
  {#each items as item (item.label)}
    {#if item.separator}
      <div class="sep"></div>
    {:else}
      <button
        class="item"
        class:danger={item.danger}
        disabled={item.disabled}
        onclick={() => handleClick(item)}
        role="menuitem"
      >
        {item.label}
      </button>
    {/if}
  {/each}
</div>

<style>
  .menu {
    position: fixed;
    z-index: 1000;
    min-width: 190px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    padding: 4px;
    display: flex;
    flex-direction: column;
  }
  .item {
    all: unset;
    box-sizing: border-box;
    padding: 7px 10px;
    font-size: var(--fs-sm);
    color: var(--text-primary);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .item:hover:not(:disabled) {
    background: var(--bg-hover);
  }
  .item:disabled {
    color: var(--text-disabled);
    cursor: not-allowed;
  }
  .item.danger {
    color: var(--danger);
  }
  .item.danger:hover {
    background: rgba(240, 87, 93, 0.12);
  }
  .sep {
    height: 1px;
    background: var(--border);
    margin: 4px 2px;
  }
</style>
