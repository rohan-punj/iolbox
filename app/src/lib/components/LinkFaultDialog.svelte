<script lang="ts">
  // Replaces the old right-click "Faults" submenu, which only offered four
  // hardcoded presets (100ms delay / 20% loss / 1mbit rate / down) plus a
  // raw-JSON prompt as the only way to reach any other value. This is a real
  // form over the same LinkFault fields the backend already accepts, seeded
  // from whatever fault (if any) is currently active on the link so editing
  // an existing fault doesn't mean re-typing it from scratch.
  import { labStore } from "../labStore.svelte";
  import type { LinkFault } from "../labTypes";

  let { linkId, onClose }: { linkId: number; onClose: () => void } = $props();

  const link = $derived(labStore.lab.links.find((l) => l.id === linkId));
  const current = $derived(labStore.linkFaults[linkId]?.fault ?? link?.fault ?? null);
  const targets = $derived(
    (link?.endpoints ?? []).map((ep, i) => ({
      index: i,
      label: `${labStore.lab.nodes.find((n) => n.id === ep.node)?.name ?? `#${ep.node}`} ${ep.interface}`,
    }))
  );

  // Seed the draft fields once from whatever fault is active when the dialog
  // opens; deliberately NOT reactive after that, so a server-pushed
  // linkFaults update mid-edit can't clobber what the user is typing.
  // svelte-ignore state_referenced_locally
  let target = $state<number | undefined>(current?.targetEndpoint);
  // svelte-ignore state_referenced_locally
  let down = $state(current?.down ?? false);
  // svelte-ignore state_referenced_locally
  let delayMs = $state<number | undefined>(current?.delayMs);
  // svelte-ignore state_referenced_locally
  let jitterMs = $state<number | undefined>(current?.jitterMs);
  // svelte-ignore state_referenced_locally
  let lossPct = $state<number | undefined>(current?.lossPct);
  // svelte-ignore state_referenced_locally
  let rateKbit = $state<number | undefined>(current?.rateKbit);
  // svelte-ignore state_referenced_locally
  let duplicatePct = $state<number | undefined>(current?.duplicatePct);
  // svelte-ignore state_referenced_locally
  let reorderPct = $state<number | undefined>(current?.reorderPct);

  function numField(e: Event): number | undefined {
    const raw = (e.currentTarget as HTMLInputElement).value;
    return raw === "" ? undefined : Number(raw);
  }

  const isEmpty = $derived(
    !down &&
      delayMs === undefined &&
      jitterMs === undefined &&
      lossPct === undefined &&
      rateKbit === undefined &&
      duplicatePct === undefined &&
      reorderPct === undefined
  );

  function buildFault(): LinkFault | null {
    if (isEmpty) return null;
    const fault: LinkFault = {};
    if (down) fault.down = true;
    if (delayMs !== undefined) fault.delayMs = delayMs;
    if (jitterMs !== undefined) fault.jitterMs = jitterMs;
    if (lossPct !== undefined) fault.lossPct = lossPct;
    if (rateKbit !== undefined) fault.rateKbit = rateKbit;
    if (duplicatePct !== undefined) fault.duplicatePct = duplicatePct;
    if (reorderPct !== undefined) fault.reorderPct = reorderPct;
    if (target !== undefined) fault.targetEndpoint = target;
    return fault;
  }

  function apply() {
    void labStore.setLinkFault(linkId, buildFault());
    onClose();
  }
  function clearFault() {
    void labStore.setLinkFault(linkId, null);
    onClose();
  }
  function onScrimDown(e: MouseEvent) {
    if (e.target === e.currentTarget) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window onkeydown={handleKey} />

<div class="scrim" role="presentation" onmousedown={onScrimDown}>
  <div class="dialog" role="dialog" aria-label="Link fault" aria-modal="true">
    <h3>Link fault</h3>

    {#if targets.length > 0}
      <section>
        <div class="row">
          <span class="row-label">Target</span>
          <div class="segmented">
            <button class:on={target === undefined} aria-pressed={target === undefined} onclick={() => (target = undefined)}>Both ends</button>
            {#each targets as t (t.index)}
              <button class:on={target === t.index} aria-pressed={target === t.index} onclick={() => (target = t.index)}>{t.label}</button>
            {/each}
          </div>
        </div>
      </section>
    {/if}

    <section>
      <label class="row toggle-row">
        <span class="row-copy">
          <span class="row-label">Administratively down</span>
          <span class="row-hint">Drops the link entirely, independent of the impairment values below</span>
        </span>
        <input type="checkbox" checked={down} onchange={() => (down = !down)} />
      </label>
    </section>

    <section>
      <h4>Egress impairment</h4>
      <div class="grid">
        <label class="field">
          <span class="row-label">Delay (ms)</span>
          <input type="number" min="0" value={delayMs ?? ""} placeholder="off" oninput={(e) => (delayMs = numField(e))} />
        </label>
        <label class="field">
          <span class="row-label">Jitter (ms)</span>
          <input type="number" min="0" value={jitterMs ?? ""} placeholder="off" oninput={(e) => (jitterMs = numField(e))} />
        </label>
        <label class="field">
          <span class="row-label">Loss (%)</span>
          <input type="number" min="0" max="100" value={lossPct ?? ""} placeholder="off" oninput={(e) => (lossPct = numField(e))} />
        </label>
        <label class="field">
          <span class="row-label">Rate (kbit)</span>
          <input type="number" min="1" value={rateKbit ?? ""} placeholder="off" oninput={(e) => (rateKbit = numField(e))} />
        </label>
        <label class="field">
          <span class="row-label">Duplicate (%)</span>
          <input type="number" min="0" max="100" value={duplicatePct ?? ""} placeholder="off" oninput={(e) => (duplicatePct = numField(e))} />
        </label>
        <label class="field">
          <span class="row-label">Reorder (%)</span>
          <input type="number" min="0" max="100" value={reorderPct ?? ""} placeholder="off" oninput={(e) => (reorderPct = numField(e))} />
        </label>
      </div>
    </section>

    <div class="actions">
      <button class="btn" onclick={clearFault}>Clear fault</button>
      <span class="spacer"></span>
      <button class="btn" onclick={onClose}>Cancel</button>
      <button class="btn btn-primary" onclick={apply}>Apply</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: var(--z-dialog);
    display: grid;
    place-items: center;
    background: rgba(4, 8, 13, 0.5);
    -webkit-backdrop-filter: blur(3px);
    backdrop-filter: blur(3px);
  }
  .dialog {
    width: min(420px, 92vw);
    max-height: 86vh;
    overflow-y: auto;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 20px;
  }
  h3 {
    margin: 0 0 14px;
    font-size: 15px;
  }
  section {
    margin-bottom: 16px;
  }
  section:last-of-type {
    margin-bottom: 0;
  }
  h4 {
    margin: 0 0 8px;
    color: var(--text-secondary);
    font-size: var(--fs-xs);
    font-weight: 650;
    letter-spacing: var(--ls-eyebrow, 0.04em);
    text-transform: uppercase;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 8px;
    margin: 0 -8px;
    border-radius: var(--radius-sm);
  }
  .toggle-row {
    cursor: pointer;
    transition: background var(--transition-fast);
  }
  .toggle-row:hover {
    background: var(--bg-hover);
  }
  .toggle-row:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .row-copy {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .row-label {
    font-size: 13px;
    color: var(--text-primary);
  }
  .row-hint {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
  }
  .segmented {
    display: inline-flex;
    flex: 0 0 auto;
    flex-wrap: wrap;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .segmented button {
    all: unset;
    box-sizing: border-box;
    padding: 4px 10px;
    color: var(--text-secondary);
    font-size: var(--fs-xs);
    cursor: pointer;
    transition: background var(--transition-fast), color var(--transition-fast);
  }
  .segmented button + button {
    border-left: 1px solid var(--border-strong);
  }
  .segmented button:hover:not(.on) {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
  .segmented button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .segmented button.on {
    color: var(--bg-0);
    background: var(--accent);
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field input {
    box-sizing: border-box;
    width: 100%;
    padding: 5px 8px;
    background: var(--bg-1);
    color: var(--text-primary);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    font-size: 13px;
    font-family: var(--font-mono);
    transition: border-color var(--transition-fast);
  }
  .field input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
  }
  .spacer {
    flex: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .segmented button,
    .toggle-row,
    .field input {
      transition: none;
    }
  }
</style>
