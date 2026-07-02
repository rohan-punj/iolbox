<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { stateColor, stateLabel } from "../nodeVisuals";
  import { iconSvg, defaultIconFor, iconRegistryVersion } from "../icons.svelte";
  import IconPicker from "./IconPicker.svelte";

  const node = $derived(labStore.selectedNode);
  const nodeState = $derived(node ? labStore.nodeStates[node.id] ?? "stopped" : "stopped");
  const image = $derived(labStore.images.find((i) => i.id === node?.image?.id));

  let showImagePicker = $state(false);
  let iconPicker = $state<{ x: number; y: number } | null>(null);

  const iconKey = $derived(
    node
      ? node.icon ?? defaultIconFor(node.kind, image?.class ?? node.image?.class)
      : undefined
  );
  const iconMarkup = $derived((iconRegistryVersion(), iconSvg(iconKey, 20)));

  function openIconPicker(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    iconPicker = { x: r.left - 210, y: r.top };
  }

  async function changeImage(imageId: string) {
    if (!node) return;
    await labStore.setNodeImage(node.id, imageId);
    showImagePicker = false;
  }

  function updateName(e: Event) {
    if (!node) return;
    node.name = (e.target as HTMLInputElement).value;
  }
  function updateRam(e: Event) {
    if (!node) return;
    node.ram = Number((e.target as HTMLInputElement).value) || 256;
  }
  function updateEthernet(e: Event) {
    if (!node) return;
    node.ethernet = Number((e.target as HTMLInputElement).value) || 0;
  }
  function updateSerial(e: Event) {
    if (!node) return;
    node.serial = Number((e.target as HTMLInputElement).value) || 0;
  }
  function updateConfig(e: Event) {
    if (!node) return;
    node.startupConfig = (e.target as HTMLTextAreaElement).value;
  }
</script>

<div class="inspector">
  {#if !node}
    <div class="empty">
      <div class="empty-title">No selection</div>
      <div class="empty-sub">Click a node on the canvas to inspect it.</div>
    </div>
  {:else}
    <div class="header">
      <span class="state-pill" style:background={`color-mix(in srgb, ${stateColor(nodeState)} 18%, transparent)`} style:color={stateColor(nodeState)}>
        <span class="dot" style:background={stateColor(nodeState)}></span>
        {stateLabel(nodeState)}
      </span>
      <span class="kind">{node.kind.toUpperCase()}</span>
    </div>

    <label class="field">
      <span class="label">Name</span>
      <input type="text" value={node.name} oninput={updateName} />
    </label>

    <div class="field">
      <span class="label">Icon</span>
      <button class="icon-btn" onclick={openIconPicker}>
        <span class="icon-glyph">{@html iconMarkup}</span>
        <span class="icon-lab">Change icon…</span>
      </button>
    </div>

    {#if node.kind === "iol"}
      <label class="field">
        <span class="label">Image</span>
        <div class="image-row">
          <span class="image-name" title={image?.filename ?? node.image?.filename}>
            {image?.filename ?? node.image?.filename ?? "none"}
          </span>
          <button class="btn btn-ghost" onclick={() => (showImagePicker = !showImagePicker)}>
            Change
          </button>
        </div>
        {#if showImagePicker}
          <div class="image-picker">
            {#each labStore.images as img (img.id)}
              <button class="image-opt" onclick={() => changeImage(img.id)}>
                <span class="badge" class:l2={img.class === "l2"}>{img.class.toUpperCase()}</span>
                {img.filename}
              </button>
            {/each}
          </div>
        {/if}
      </label>

      <div class="field-row">
        <label class="field">
          <span class="label">RAM (MB)</span>
          <input type="number" min="32" step="32" value={node.ram ?? 256} oninput={updateRam} />
        </label>
      </div>

      <div class="field-row">
        <label class="field">
          <span class="label">Ethernet adapters</span>
          <input type="number" min="0" max="16" value={node.ethernet ?? 1} oninput={updateEthernet} />
        </label>
        <label class="field">
          <span class="label">Serial adapters</span>
          <input type="number" min="0" max="16" value={node.serial ?? 1} oninput={updateSerial} />
        </label>
      </div>

      <label class="field grow">
        <span class="label">Startup config</span>
        <textarea
          class="mono config-editor"
          spellcheck="false"
          placeholder="! empty — image default config applies"
          value={node.startupConfig ?? ""}
          oninput={updateConfig}
        ></textarea>
      </label>
    {:else}
      <div class="vpcs-hint">VPCS nodes take their config from canned commands (set later).</div>
    {/if}
  {/if}
</div>

{#if iconPicker && node}
  <IconPicker
    x={iconPicker.x}
    y={iconPicker.y}
    current={iconKey}
    onPick={(key) => labStore.setNodeIcon(node!.id, key)}
    onClose={() => (iconPicker = null)}
  />
{/if}

<style>
  .inspector {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-3);
    height: 100%;
    overflow-y: auto;
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    text-align: center;
    gap: 4px;
    color: var(--text-tertiary);
  }
  .empty-title {
    font-size: var(--fs-md);
    color: var(--text-secondary);
    font-weight: 600;
  }
  .empty-sub {
    font-size: var(--fs-xs);
  }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .state-pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 9px;
    border-radius: var(--radius-full);
    font-size: var(--fs-xs);
    font-weight: 600;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }
  .kind {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    font-weight: 600;
    letter-spacing: 0.04em;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field.grow {
    flex: 1;
    min-height: 160px;
  }
  .field-row {
    display: flex;
    gap: var(--sp-2);
  }
  .field-row .field {
    flex: 1;
  }
  .label {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    font-weight: 500;
  }
  .image-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .image-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-sm);
    padding: 6px 8px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .image-picker {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 160px;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 4px;
  }
  .image-opt {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 6px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-xs);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .image-opt:hover {
    background: var(--bg-hover);
  }
  .badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 5px;
    border-radius: var(--radius-sm);
    background: var(--node-iol-l3);
    color: var(--ground);
    flex-shrink: 0;
  }
  .badge.l2 {
    background: var(--node-iol-l2);
  }
  .config-editor {
    flex: 1;
    resize: none;
    min-height: 140px;
    line-height: 1.5;
    padding: var(--sp-2);
  }
  .vpcs-hint {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    line-height: 1.5;
  }
  .icon-btn {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    cursor: pointer;
    background: var(--panel-solid);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 7px 9px;
    color: var(--ink);
  }
  .icon-btn:hover {
    border-color: var(--accent);
  }
  .icon-glyph {
    display: grid;
    place-items: center;
    color: var(--accent);
  }
  .icon-glyph :global(svg),
  .icon-glyph :global(img) {
    width: 18px;
    height: 18px;
  }
  .icon-lab {
    font-size: var(--fs-sm);
  }
</style>
