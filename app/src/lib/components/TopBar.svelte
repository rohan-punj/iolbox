<script lang="ts">
  import { labStore } from "../labStore.svelte";

  const anyRunning = $derived(labStore.labRunning);
  const providerLabel = $derived(
    labStore.activeProvider ? labStore.activeProvider.toUpperCase() : "—"
  );

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
</script>

<header class="topbar">
  <div class="left">
    <div class="brand">
      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
        <rect x="2" y="5" width="20" height="14" rx="2.5" fill="none" stroke="var(--accent)" stroke-width="1.6" />
        <circle cx="8" cy="12" r="1.6" fill="var(--accent)" />
        <circle cx="16" cy="12" r="1.6" fill="var(--accent)" />
        <path d="M9.5 12h5" stroke="var(--accent)" stroke-width="1.4" />
      </svg>
      <span>iolab</span>
    </div>
    <input class="lab-name" value={labStore.lab.name} oninput={updateName} aria-label="Lab name" />
  </div>

  <div class="center">
    <button class="btn start-stop" class:running={anyRunning} onclick={toggleLab}>
      {#if anyRunning}
        <span class="ico">■</span> Stop lab
      {:else}
        <span class="ico">▶</span> Start lab
      {/if}
    </button>
  </div>

  <div class="right">
    <button class="btn btn-ghost" onclick={() => (labStore.showImageManager = true)}>
      Images
    </button>
    <span
      class="pill status-pill"
      class:connected={labStore.providerStatus === "connected"}
      class:connecting={labStore.providerStatus === "connecting"}
      class:error={labStore.providerStatus === "error"}
    >
      <span class="status-dot"></span>
      {providerLabel}
    </span>
  </div>
</header>

<style>
  .topbar {
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--sp-3);
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    gap: var(--sp-4);
  }
  .left {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    min-width: 0;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 700;
    font-size: var(--fs-md);
    color: var(--text-primary);
    flex-shrink: 0;
  }
  .lab-name {
    background: transparent;
    border: 1px solid transparent;
    font-size: var(--fs-sm);
    color: var(--text-secondary);
    padding: 5px 8px;
    min-width: 140px;
    max-width: 280px;
  }
  .lab-name:hover {
    border-color: var(--border);
  }
  .lab-name:focus {
    background: var(--bg-1);
    border-color: var(--accent);
    color: var(--text-primary);
  }
  .center {
    flex-shrink: 0;
  }
  .start-stop {
    min-width: 116px;
    justify-content: center;
    background: var(--success);
    border-color: var(--success);
    color: #05130b;
    font-weight: 600;
  }
  .start-stop:hover {
    filter: brightness(1.08);
  }
  .start-stop.running {
    background: var(--danger);
    border-color: var(--danger);
    color: #1a0506;
  }
  .ico {
    font-size: 10px;
  }
  .right {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-shrink: 0;
  }
  .status-pill {
    background: var(--bg-3);
    color: var(--text-tertiary);
  }
  .status-pill.connecting {
    color: var(--warning);
  }
  .status-pill.connected {
    color: var(--success);
  }
  .status-pill.error {
    color: var(--danger);
  }
  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
  }
</style>
