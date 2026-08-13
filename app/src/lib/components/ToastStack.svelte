<script lang="ts">
  import { flip } from "svelte/animate";
  import { labStore } from "../labStore.svelte";

  const reducedMotion =
    typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  function handleKey(event: KeyboardEvent, id: string) {
    if (event.key === "Escape") {
      event.preventDefault();
      labStore.dismissToast(id);
    }
  }
</script>

{#if labStore.toasts.length > 0}
  <section class="toast-stack" aria-label="Notifications">
    {#each labStore.toasts as toast (toast.id)}
      <article
        class="toast"
        class:toast-success={toast.severity === "success"}
        class:toast-info={toast.severity === "info"}
        class:toast-warning={toast.severity === "warning"}
        class:toast-danger={toast.severity === "danger"}
        class:toast-error={toast.severity === "error"}
        class:is-dismissing={toast.dismissing}
        role={toast.severity === "error" ? "alert" : "status"}
        aria-live={toast.severity === "error" ? "assertive" : "polite"}
        aria-atomic="true"
        onmouseenter={() => labStore.pauseToast(toast.id)}
        onmouseleave={() => labStore.resumeToast(toast.id)}
        onfocusin={() => labStore.pauseToast(toast.id)}
        onfocusout={() => labStore.resumeToast(toast.id)}
        onkeydown={(event) => handleKey(event, toast.id)}
        animate:flip={{ duration: reducedMotion ? 0 : 180 }}
      >
        <span class="led" aria-hidden="true"></span>
        <p>{toast.message}</p>
        <button
          class="dismiss"
          type="button"
          aria-label="Dismiss notification"
          onclick={() => labStore.dismissToast(toast.id)}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M7 7l10 10M17 7L7 17" />
          </svg>
        </button>
      </article>
    {/each}
  </section>
{/if}

<style>
  .toast-stack {
    position: fixed;
    top: calc(var(--topbar-h) + var(--sp-3));
    right: var(--sp-3);
    z-index: var(--z-menu);
    width: min(360px, calc(100vw - 2 * var(--sp-3)));
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    pointer-events: none;
  }

  .toast {
    --toast-tone: var(--ink);
    --toast-led: var(--state-stopped);
    min-height: 44px;
    max-width: 360px;
    display: grid;
    grid-template-columns: 8px minmax(0, 1fr) 28px;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px var(--sp-2) 10px var(--sp-3);
    pointer-events: auto;
    color: var(--ink);
    background: var(--toast-surface);
    -webkit-backdrop-filter: var(--toast-blur);
    backdrop-filter: var(--toast-blur);
    border: 1px solid color-mix(in oklab, var(--toast-tone) 42%, var(--border));
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    font-family: var(--font-ui);
    font-size: var(--fs-base);
    line-height: 1.4;
    animation: toast-enter var(--transition-base) ease-out both;
    transition: transform var(--transition-base), opacity var(--transition-fast);
  }

  .toast-success {
    --toast-tone: var(--success);
    --toast-led: var(--state-running);
  }

  .toast-info {
    --toast-tone: var(--ink);
    --toast-led: var(--state-stopped);
  }

  .toast-warning {
    --toast-tone: var(--warning);
    --toast-led: var(--state-starting);
  }

  .toast-danger {
    --toast-tone: var(--danger);
    --toast-led: var(--state-crashed);
  }

  .toast-error {
    --toast-tone: var(--danger);
    --toast-led: var(--state-crashed);
  }

  .toast.is-dismissing {
    animation: toast-exit var(--transition-fast) ease-in both;
    pointer-events: none;
  }

  .led {
    width: 8px;
    height: 8px;
    display: block;
    border-radius: 50%;
    background: var(--toast-led);
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--toast-led) 20%, transparent);
  }

  p {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }

  .dismiss {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    padding: 0;
    color: var(--ink-3);
    background: transparent;
    border: 0;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: color var(--transition-fast), background var(--transition-fast);
  }

  .dismiss:hover,
  .dismiss:focus-visible {
    color: var(--ink);
    background: var(--bg-hover);
  }

  .dismiss svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-linecap: round;
    stroke-width: 1.8;
  }

  @keyframes toast-enter {
    from {
      opacity: 0;
      transform: translateX(12px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateX(0) scale(1);
    }
  }

  @keyframes toast-exit {
    to {
      opacity: 0;
      transform: translateX(8px);
    }
  }

  @media (max-width: 640px) {
    .toast-stack {
      left: var(--sp-2);
      right: var(--sp-2);
      width: auto;
    }

    .toast {
      max-width: none;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .toast,
    .toast.is-dismissing,
    .dismiss {
      animation: none;
      transition: none;
    }

    .toast.is-dismissing {
      opacity: 0;
      transform: none;
    }
  }
</style>
