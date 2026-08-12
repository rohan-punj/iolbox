<script lang="ts">
  export interface MenuItem {
    id: string;
    label: string;
    action: () => void;
    danger?: boolean;
    disabled?: boolean;
    separator?: boolean;
    title?: string;
    checked?: boolean;
    submenu?: MenuItem[];
  }

  let {
    x,
    y,
    items,
    onClose,
    dismissOnWheel = true,
  }: {
    x: number;
    y: number;
    items: MenuItem[];
    onClose: () => void;
    dismissOnWheel?: boolean;
  } = $props();

  import { onMount, tick } from "svelte";
  import { chromeStore } from "../chromeStore.svelte";

  $effect(() => chromeStore.hold());

  let menuEl: HTMLDivElement | undefined = $state();
  let itemEls: Record<string, HTMLButtonElement | undefined> = $state({});
  let focusedIndex = $state(0);
  let openSubmenuId = $state<string | null>(null);

  type FocusItem = { id: string; item: MenuItem; parentId?: string };

  function isFocusable(item: MenuItem): boolean {
    return !item.separator && !item.disabled;
  }

  function focusItems(): FocusItem[] {
    const result: FocusItem[] = items.filter(isFocusable).map((item) => ({
      id: item.id,
      item,
    }));
    const parent = items.find((item) => item.id === openSubmenuId);
    if (parent?.submenu) {
      result.push(
        ...parent.submenu
          .filter(isFocusable)
          .map((item) => ({ id: item.id, item, parentId: parent.id })),
      );
    }
    return result;
  }

  function focusAt(index: number) {
    const focusable = focusItems();
    if (focusable.length === 0) return;
    focusedIndex = (index + focusable.length) % focusable.length;
    const target = focusable[focusedIndex];
    if (target.parentId) openSubmenuId = target.parentId;
    void tick().then(() => itemEls[target.id]?.focus());
  }

  function focusItem(id: string) {
    const index = focusItems().findIndex((entry) => entry.id === id);
    if (index >= 0) focusAt(index);
  }

  function openSubmenu(item: MenuItem) {
    const first = item.submenu?.find(isFocusable);
    if (!first) return;
    openSubmenuId = item.id;
    void tick().then(() => focusItem(first.id));
  }

  function handleClick(item: MenuItem) {
    if (item.disabled) return;
    item.action();
    onClose();
  }

  function handleSubmenuClick(item: MenuItem) {
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

  function handleItemKey(e: KeyboardEvent, item: MenuItem, parentId?: string) {
    const current = focusItems().findIndex((entry) => entry.id === item.id);
    if (current < 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      focusAt(current + 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      focusAt(current - 1);
    } else if (e.key === "Home") {
      e.preventDefault();
      focusAt(0);
    } else if (e.key === "End") {
      e.preventDefault();
      focusAt(focusItems().length - 1);
    } else if (e.key === "ArrowRight" && item.submenu && !parentId) {
      e.preventDefault();
      openSubmenu(item);
    } else if (e.key === "ArrowLeft" && parentId) {
      e.preventDefault();
      openSubmenuId = null;
      void tick().then(() => focusItem(parentId));
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      if (item.submenu && !parentId) openSubmenu(item);
      else if (!item.disabled) {
        item.action();
        onClose();
      }
    }
  }

  onMount(() => {
    const first = focusItems()[0];
    if (first) {
      focusedIndex = 0;
      void tick().then(() => itemEls[first.id]?.focus());
    }
    // Defer attaching until after the current event loop tick so the very
    // right-click/contextmenu event that opened this menu doesn't immediately
    // close it.
    const attach = () => {
      document.addEventListener("pointerdown", handlePointerDown, true);
      if (dismissOnWheel) document.addEventListener("wheel", handleWheel, true);
    };
    const t = setTimeout(attach, 0);
    return () => {
      clearTimeout(t);
      document.removeEventListener("pointerdown", handlePointerDown, true);
      if (dismissOnWheel) document.removeEventListener("wheel", handleWheel, true);
    };
  });
</script>

<svelte:window onkeydown={handleKey} />

<div class="menu" bind:this={menuEl} style:left={`${x}px`} style:top={`${y}px`} role="menu">
  {#each items as item (item.id)}
    {#if item.separator}
      <div class="sep"></div>
    {:else}
      <button
        class="item"
        class:danger={item.danger}
        class:checkable={item.checked !== undefined}
        disabled={item.disabled}
        title={item.title}
        aria-checked={item.checked}
        aria-haspopup={item.submenu ? "menu" : undefined}
        tabindex="-1"
        role={item.checked === undefined ? "menuitem" : "menuitemcheckbox"}
        bind:this={itemEls[item.id]}
        onclick={() => handleClick(item)}
        onkeydown={(e) => handleItemKey(e, item)}
      >
        <span class="state-mark" class:checked={item.checked} aria-hidden="true"></span>
        {item.label}
      </button>
      {#if item.submenu}
        <div class="submenu" class:open={openSubmenuId === item.id} role="menu">
          {#each item.submenu as child (child.id)}
            {#if child.separator}
              <div class="sep"></div>
            {:else}
              <button
                class="item"
                class:danger={child.danger}
                class:checkable={child.checked !== undefined}
                disabled={child.disabled}
                title={child.title}
                aria-checked={child.checked}
                tabindex="-1"
                role={child.checked === undefined ? "menuitem" : "menuitemradio"}
                bind:this={itemEls[child.id]}
                onclick={() => handleSubmenuClick(child)}
                onkeydown={(e) => handleItemKey(e, child, item.id)}
              >
                <span class="state-mark" class:checked={child.checked} aria-hidden="true"></span>
                {child.label}
              </button>
            {/if}
          {/each}
        </div>
      {/if}
    {/if}
  {/each}
</div>

<style>
  .menu {
    position: fixed;
    z-index: var(--z-menu);
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
  .state-mark {
    position: relative;
    display: inline-block;
    width: 14px;
    height: 12px;
    margin-right: 4px;
  }
  .state-mark.checked::after {
    content: "";
    position: absolute;
    left: 3px;
    top: 1px;
    width: 5px;
    height: 8px;
    border-right: 1.5px solid var(--accent);
    border-bottom: 1.5px solid var(--accent);
    transform: rotate(45deg);
  }
  .item:has(+ .submenu) {
    padding-right: 24px;
  }
  .item:has(+ .submenu)::after {
    content: "›";
    float: right;
    color: var(--text-secondary);
  }
  .submenu {
    position: absolute;
    left: calc(100% - 4px);
    top: 4px;
    min-width: 230px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    padding: 4px;
    display: none;
  }
  .menu > .item:hover + .submenu,
  .menu > .submenu:hover,
  .menu > .submenu.open {
    display: flex;
    flex-direction: column;
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
