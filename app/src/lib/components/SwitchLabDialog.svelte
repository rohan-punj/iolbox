<script lang="ts">
  // Warns before switching the workspace to a different lab (iolbox runs
  // exactly one lab at a time; switching stops every running node and tears
  // down the current lab's fabric — see labStore.openLab/pendingSwitch). This
  // is the single warning surface for every switch entry point (Labs browser
  // open/clone, New, Import); those components no longer roll their own
  // confirm().
  import { labStore } from "../labStore.svelte";

  const pending = $derived(labStore.pendingSwitch);
  const targetName = $derived(pending?.lab.name || "Untitled lab");
  const currentName = $derived(labStore.lab.name || "Untitled lab");
  const running = $derived(labStore.labRunning);
  const dirty = $derived(labStore.currentLabDirty);

  let saving = $state(false);

  async function onSave() {
    saving = true;
    try {
      await labStore.resolveSwitch("save");
    } finally {
      saving = false;
    }
  }
  function onDiscard() {
    labStore.resolveSwitch("discard");
  }
  function onCancel() {
    labStore.cancelSwitch();
  }
  function onScrimDown(e: MouseEvent) {
    if (e.target === e.currentTarget) onCancel();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onCancel();
  }
</script>

<svelte:window onkeydown={handleKey} />

{#if pending}
  <div class="scrim" role="presentation" onmousedown={onScrimDown}>
    <div class="dialog" role="alertdialog" aria-label="Close current lab?" aria-modal="true">
      <h3>Close &ldquo;{currentName}&rdquo;?</h3>
      <p>
        iolbox runs one lab at a time. Opening &ldquo;{targetName}&rdquo; will
        {#if running}
          <strong>stop every running node</strong> in the current lab and remove
          its virtual cabling from the host.
        {:else}
          close the current lab.
        {/if}
      </p>
      {#if dirty}
        <p class="warn">This lab hasn't been saved.</p>
      {/if}
      <div class="actions">
        <button class="btn btn-ghost" onclick={onCancel} disabled={saving}>Cancel</button>
        {#if dirty}
          <button class="btn btn-danger" onclick={onDiscard} disabled={saving}>Close without saving</button>
          <button class="btn btn-primary" onclick={onSave} disabled={saving}>
            {saving ? "Saving…" : "Save and close"}
          </button>
        {:else}
          <button class="btn btn-primary" onclick={onDiscard} disabled={saving}>Close</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 1200;
    display: grid;
    place-items: center;
    background: rgba(4, 8, 13, 0.5);
    -webkit-backdrop-filter: blur(3px);
    backdrop-filter: blur(3px);
  }
  .dialog {
    width: min(440px, 92vw);
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 20px;
  }
  h3 {
    margin: 0 0 10px;
    font-size: 15px;
  }
  p {
    margin: 0 0 10px;
    font-size: 13px;
    color: var(--text-muted);
    line-height: 1.5;
  }
  p.warn {
    color: var(--warning);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
</style>
