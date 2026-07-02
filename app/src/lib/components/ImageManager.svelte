<script lang="ts">
  import { labStore } from "../labStore.svelte";

  let { onClose }: { onClose: () => void } = $props();

  let replaceFrom = $state<string>("");
  let replaceTo = $state<string>("");
  let busy = $state(false);

  function fmtSize(bytes: number): string {
    if (bytes > 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`;
    return `${(bytes / 1_000).toFixed(0)} KB`;
  }

  function usageCount(imageId: string): number {
    return labStore.lab.nodes.filter((n) => n.image?.id === imageId).length;
  }

  async function addImage() {
    // Stubs a Tauri file-picker invoke; in dev/mock we fabricate a path.
    busy = true;
    try {
      const fakeName = `custom-image-${Math.floor(Math.random() * 1000)}.bin`;
      await labStore.client.imageRegister(`C:\\fake\\path\\${fakeName}`);
      const { images } = await labStore.client.imageList();
      labStore.images = images;
    } finally {
      busy = false;
    }
  }

  async function removeImage(id: string) {
    if (usageCount(id) > 0) {
      labStore.pushLog("warn", `Cannot remove image ${id}: still referenced by ${usageCount(id)} node(s)`);
      return;
    }
    await labStore.client.imageRemove(id);
    labStore.images = labStore.images.filter((i) => i.id !== id);
  }

  function doReplace() {
    if (!replaceFrom || !replaceTo || replaceFrom === replaceTo) return;
    labStore.replaceImageEverywhere(replaceFrom, replaceTo);
    replaceFrom = "";
    replaceTo = "";
  }

  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window onkeydown={handleKey} />

<div class="scrim" onclick={onClose} role="presentation">
  <div
    class="modal"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
    role="dialog"
    aria-modal="true"
    aria-label="Image manager"
    tabindex="-1"
  >
    <div class="modal-header">
      <h2>Image library</h2>
      <button class="btn btn-icon btn-ghost" onclick={onClose} aria-label="Close">✕</button>
    </div>

    <div class="toolbar">
      <button class="btn btn-primary" onclick={addImage} disabled={busy}>
        {busy ? "Adding…" : "+ Add image…"}
      </button>
      <span class="hint">Registers a .bin/.iol via file picker (stubbed in dev).</span>
    </div>

    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Filename</th>
            <th>Class</th>
            <th>Arch</th>
            <th>Size</th>
            <th>SHA-256</th>
            <th>In use</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each labStore.images as img (img.id)}
            <tr>
              <td class="fname" title={img.filename}>{img.filename}</td>
              <td><span class="badge" class:l2={img.class === "l2"}>{img.class.toUpperCase()}</span></td>
              <td class="mono">{img.arch}</td>
              <td>{fmtSize(img.size)}</td>
              <td class="mono sha" title={img.sha256}>{img.sha256.slice(0, 12)}…</td>
              <td>{usageCount(img.id)}</td>
              <td>
                <button
                  class="btn btn-icon btn-ghost"
                  disabled={usageCount(img.id) > 0}
                  title={usageCount(img.id) > 0 ? "In use — swap nodes off it first" : "Remove"}
                  onclick={() => removeImage(img.id)}
                  aria-label="Remove image"
                >
                  🗑
                </button>
              </td>
            </tr>
          {/each}
          {#if labStore.images.length === 0}
            <tr><td colspan="7" class="empty">No images registered yet.</td></tr>
          {/if}
        </tbody>
      </table>
    </div>

    <div class="bulk">
      <div class="bulk-title">Replace all nodes using image X with Y</div>
      <div class="bulk-row">
        <select bind:value={replaceFrom}>
          <option value="">From…</option>
          {#each labStore.images as img (img.id)}
            <option value={img.id}>{img.filename} ({usageCount(img.id)} nodes)</option>
          {/each}
        </select>
        <span class="arrow">→</span>
        <select bind:value={replaceTo}>
          <option value="">To…</option>
          {#each labStore.images as img (img.id)}
            <option value={img.id}>{img.filename}</option>
          {/each}
        </select>
        <button class="btn" disabled={!replaceFrom || !replaceTo} onclick={doReplace}>Apply</button>
      </div>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(3, 5, 8, 0.6);
    backdrop-filter: blur(2px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 2000;
  }
  .modal {
    width: min(820px, 92vw);
    max-height: 82vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }
  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--sp-4);
    border-bottom: 1px solid var(--border);
  }
  .modal-header h2 {
    margin: 0;
    font-size: var(--fs-lg);
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
    border-bottom: 1px solid var(--border-subtle);
  }
  .hint {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
  }
  .table-wrap {
    overflow-y: auto;
    padding: 0 var(--sp-4);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fs-sm);
  }
  thead th {
    position: sticky;
    top: 0;
    background: var(--bg-2);
    text-align: left;
    padding: var(--sp-2) var(--sp-2);
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    font-weight: 600;
    border-bottom: 1px solid var(--border);
  }
  tbody td {
    padding: var(--sp-2);
    border-bottom: 1px solid var(--border-subtle);
    color: var(--text-primary);
  }
  .fname {
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sha {
    color: var(--text-tertiary);
  }
  .badge {
    font-size: 9px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    background: var(--node-iol-l3);
    color: #0d1117;
  }
  .badge.l2 {
    background: var(--node-iol-l2);
  }
  .empty {
    text-align: center;
    color: var(--text-tertiary);
    padding: var(--sp-6);
  }
  .bulk {
    padding: var(--sp-3) var(--sp-4) var(--sp-4);
    border-top: 1px solid var(--border);
  }
  .bulk-title {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    font-weight: 600;
    margin-bottom: var(--sp-2);
  }
  .bulk-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .bulk-row select {
    flex: 1;
    min-width: 0;
  }
  .arrow {
    color: var(--text-tertiary);
  }
</style>
