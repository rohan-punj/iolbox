<script lang="ts">
  import { labStore } from "../labStore.svelte";

  let { onClose }: { onClose: () => void } = $props();

  let replaceFrom = $state<string>("");
  let replaceTo = $state<string>("");
  let busy = $state(false);
  let uploadProgress = $state<number | null>(null); // 0..100 while an upload is in flight
  let uploadError = $state<string | null>(null);
  let fileInput: HTMLInputElement | undefined = $state();

  function fmtSize(bytes: number): string {
    if (bytes > 1_000_000) return `${(bytes / 1_000_000).toFixed(1)} MB`;
    return `${(bytes / 1_000).toFixed(0)} KB`;
  }

  function usageCount(imageId: string): number {
    return labStore.lab.nodes.filter((n) => n.image?.id === imageId).length;
  }

  function addImage() {
    uploadError = null;
    fileInput?.click();
  }

  /** Upload the file to the same-origin supervisor HTTP endpoint with progress,
   *  then register the resulting path as a library image. Mock transport has
   *  no HTTP server to upload to, so it fabricates a path instead (keeps the
   *  existing dev click-through flow working with no backend). */
  async function onFileChosen(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ""; // allow re-selecting the same file later
    if (!file) return;

    uploadError = null;
    busy = true;
    try {
      const path =
        labStore.transportKind === "mock"
          ? `C:\\fake\\path\\${file.name}`
          : await uploadImageFile(file);
      await labStore.client.imageRegister(path);
      const { images } = await labStore.client.imageList();
      // Adopt + reconcile + push to the supervisor: on a fresh runtime this
      // is the moment seed labs become startable.
      await labStore.onImagesUpdated(images);
    } catch (e) {
      uploadError = (e as Error).message;
      labStore.pushLog("error", `image upload failed: ${uploadError}`);
    } finally {
      busy = false;
      uploadProgress = null;
    }
  }

  /** POST the file to /api/upload/image with progress via XHR (fetch has no
   *  upload-progress event). Resolves with the server-reported path. */
  function uploadImageFile(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      const url = `/api/upload/image?filename=${encodeURIComponent(file.name)}`;
      xhr.open("POST", url);
      xhr.setRequestHeader("Content-Type", "application/octet-stream");
      uploadProgress = 0;

      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable) uploadProgress = Math.round((ev.loaded / ev.total) * 100);
      };

      xhr.onload = () => {
        let body: { path?: string; error?: string } = {};
        try {
          body = JSON.parse(xhr.responseText);
        } catch {
          // fall through to status-based handling below
        }
        if (xhr.status >= 200 && xhr.status < 300 && body.path) {
          resolve(body.path);
        } else {
          reject(new Error(body.error || `upload failed (HTTP ${xhr.status})`));
        }
      };
      xhr.onerror = () => reject(new Error("network error during upload"));
      xhr.onabort = () => reject(new Error("upload aborted"));

      xhr.send(file);
    });
  }

  async function removeImage(id: string) {
    if (usageCount(id) > 0) {
      labStore.pushLog("warn", `Cannot remove image ${id}: still referenced by ${usageCount(id)} node(s)`);
      return;
    }
    try {
      // NOTE: the real supervisor does not implement image.remove yet (only
      // image.list/image.register are registered — see
      // supervisor/internal/server/server.go). Under the mock this always
      // succeeds; against a real supervisor it currently replies
      // {ok:false,code:"unsupported"}, so drop the image locally regardless
      // rather than surfacing a hard failure for an unshipped verb.
      await labStore.client.imageRemove(id);
    } catch (e) {
      labStore.pushLog("warn", `image.remove not supported by supervisor: ${(e as Error).message}`);
    }
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
      <input
        bind:this={fileInput}
        type="file"
        accept=".bin,.iol"
        class="file-input-hidden"
        onchange={onFileChosen}
      />
      <button class="btn btn-primary" onclick={addImage} disabled={busy}>
        {busy ? "Adding…" : "+ Add image…"}
      </button>
      {#if uploadProgress !== null}
        <div class="progress-wrap" role="progressbar" aria-valuenow={uploadProgress} aria-valuemin="0" aria-valuemax="100">
          <div class="progress-bar" style:transform={`scaleX(${uploadProgress / 100})`}></div>
          <span class="progress-label">{uploadProgress}%</span>
        </div>
      {:else}
        <span class="hint">
          {labStore.transportKind === "mock"
            ? "Registers a .bin/.iol (fabricated path in dev)."
            : "Upload a .bin/.iol image (250–300 MB files may take a while)."}
        </span>
      {/if}
    </div>
    {#if uploadError}
      <div class="upload-error" role="alert">
        {uploadError}
        <button class="btn btn-icon btn-ghost" onclick={() => (uploadError = null)} aria-label="Dismiss error">✕</button>
      </div>
    {/if}

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
            <tr>
              <td colspan="7" class="empty">
                {labStore.imagesLoading ? "Loading images…" : "No images registered yet."}
              </td>
            </tr>
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
    background: var(--scrim);
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
  .file-input-hidden {
    display: none;
  }
  .progress-wrap {
    position: relative;
    flex: 1;
    height: 18px;
    min-width: 140px;
    background: var(--bg-1);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .progress-bar {
    position: absolute;
    inset: 0 0 0 0;
    transform-origin: left;
    background: var(--accent, #5b8cff);
    transition: transform 120ms linear;
    will-change: transform;
  }
  .progress-label {
    position: relative;
    display: block;
    text-align: center;
    font-size: var(--fs-xs);
    line-height: 18px;
    color: var(--text-primary);
    mix-blend-mode: difference;
  }
  .upload-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    margin: 0 var(--sp-4) var(--sp-2);
    padding: var(--sp-2) var(--sp-3);
    border: 1px solid color-mix(in oklab, var(--state-crashed, #e5484d) 55%, transparent);
    background: color-mix(in oklab, var(--state-crashed, #e5484d) 14%, transparent);
    color: var(--danger);
    border-radius: var(--radius-md);
    font-size: var(--fs-xs);
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
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: var(--radius-sm);
    background: var(--node-iol-l3);
    color: var(--ground);
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
