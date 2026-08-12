<script lang="ts">
  import { onMount, tick } from "svelte";
  import type { Snippet } from "svelte";

  let {
    title,
    onClose,
    children,
  }: {
    title: string;
    onClose: () => void;
    children: Snippet;
  } = $props();

  let panelEl: HTMLDivElement | undefined = $state();
  let opener: HTMLElement | null = null;
  let closing = false;

  function focusableElements(): HTMLElement[] {
    if (!panelEl) return [];
    return Array.from(
      panelEl.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [href], [tabindex]:not([tabindex="-1"])'
      )
    );
  }

  function closeAndReturnFocus() {
    if (closing) return;
    closing = true;
    onClose();
    void tick().then(() => opener?.focus());
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      closeAndReturnFocus();
      return;
    }
    if (event.key !== "Tab" || !panelEl) return;
    const focusable = focusableElements();
    if (focusable.length === 0) {
      event.preventDefault();
      panelEl.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function handlePointerDown(event: PointerEvent) {
    if (panelEl && !panelEl.contains(event.target as Node)) closeAndReturnFocus();
  }

  onMount(() => {
    opener = document.activeElement as HTMLElement | null;
    void tick().then(() => focusableElements()[0]?.focus());

    // Attach after the opening click/right-click has finished. Capture is
    // required because Svelte Flow stops bubbling pointer events on the canvas.
    const attach = () => document.addEventListener("pointerdown", handlePointerDown, true);
    const timeout = setTimeout(attach, 0);
    return () => {
      clearTimeout(timeout);
      document.removeEventListener("pointerdown", handlePointerDown, true);
    };
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="rail-flyout" bind:this={panelEl} tabindex="-1" role="dialog" aria-label={title}>
  <header class="flyout-header">
    <h2>{title}</h2>
    <button class="flyout-close" title="Close" aria-label="Close" onclick={closeAndReturnFocus}>×</button>
  </header>
  <div class="flyout-body">
    {@render children()}
  </div>
</div>

<style>
  .rail-flyout {
    position: absolute;
    top: 12px;
    left: 12px;
    z-index: var(--z-panel);
    width: min(320px, calc(100% - 24px));
    max-height: calc(100% - 24px);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    color: var(--ink);
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
  }
  .flyout-header {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }
  h2 {
    margin: 0;
    color: var(--ink-2);
    font-size: var(--fs-xs);
    font-weight: 650;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .flyout-close {
    all: unset;
    box-sizing: border-box;
    width: 24px;
    height: 24px;
    display: grid;
    place-items: center;
    color: var(--ink-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .flyout-close:hover,
  .flyout-close:focus-visible {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .flyout-body {
    min-height: 0;
    overflow-y: auto;
    padding: 10px;
  }
  @media (prefers-reduced-motion: reduce) {
    .flyout-close {
      transition: none;
    }
  }
</style>
