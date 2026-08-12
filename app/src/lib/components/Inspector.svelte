<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { stateColor, stateLabel } from "../nodeVisuals";
  import { iconSvg, defaultIconFor, iconRegistryVersion, uiSvg } from "../icons.svelte";
  import { usedInterfaces } from "../interfaces";
  import IconPicker from "./IconPicker.svelte";

  const node = $derived(labStore.inspectorNode);
  const nodeState = $derived(node ? labStore.nodeStates[node.id] ?? "stopped" : "stopped");
  const running = $derived(nodeState === "running" || nodeState === "starting");
  const image = $derived(labStore.images.find((i) => i.id === node?.image?.id));
  const isIol = $derived(node?.kind === "iol");
  const isTool = $derived(node?.kind === "tool");
  const isPc = $derived(node?.kind === "pc");

  let showImagePicker = $state(false);
  let iconPicker = $state<{ x: number; y: number } | null>(null);

  const iconKey = $derived(
    node
      ? node.icon ?? defaultIconFor(node.kind, image?.class ?? node.image?.class)
      : undefined
  );
  const iconMarkup = $derived((iconRegistryVersion(), iconSvg(iconKey, 20)));

  // Highest adapter group index consumed by an existing link, per family —
  // same rule NodeEditDialog used (warn, don't silently orphan a link).
  function minGroups(nodeId: number, family: "e" | "s"): number {
    const used = usedInterfaces(nodeId);
    let max = -1;
    for (const iface of used) {
      const m = iface.match(/^([es])(\d+)\/(\d+)$/);
      if (m && m[1] === family) max = Math.max(max, Number(m[2]));
    }
    return max + 1;
  }
  const minEth = $derived(node ? (usedInterfaces(node.id), minGroups(node.id, "e")) : 0);
  const minSer = $derived(node ? (usedInterfaces(node.id), minGroups(node.id, "s")) : 0);
  const ethWarn = $derived(isIol && !!node && (node.ethernet ?? 1) < minEth);
  const serWarn = $derived(isIol && !!node && (node.serial ?? 0) < minSer);

  const packId = $derived(typeof node?.config?.pack === "string" ? node.config.pack : "");
  const toolPackInvalid = $derived(
    isTool &&
      (!packId ||
        (labStore.toolPacks.length > 0 && !labStore.toolPacks.some((p) => p.id === packId)))
  );
  const netCfg = $derived(
    node?.config?.net as { ip?: string; prefixLen?: number; gateway?: string } | undefined
  );

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
    node.ram = Number((e.target as HTMLInputElement).value) || 1024;
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
  function toggleBoot() {
    if (!node) return;
    const on = node.config?.bootFromStartup !== false;
    node.config = { ...(node.config ?? {}), bootFromStartup: !on };
  }
  async function saveConfigFromNvram() {
    if (!node) return;
    await labStore.saveNodeConfig(node.id);
  }
  function updatePack(e: Event) {
    if (!node) return;
    node.config = { ...(node.config ?? {}), pack: (e.target as HTMLSelectElement).value };
  }
  function updateNet(field: "ip" | "prefixLen" | "gateway", value: string) {
    if (!node) return;
    const cur = (node.config?.net as { ip?: string; prefixLen?: number; gateway?: string }) ?? {};
    const next = { ...cur, [field]: field === "prefixLen" ? Number(value) || 24 : value };
    const cfg = { ...(node.config ?? {}), net: next.ip?.trim() ? next : undefined };
    if (!cfg.net) delete cfg.net;
    node.config = cfg;
  }

  let applyingConfig = $state(false);
  async function applyConfig() {
    if (!node || applyingConfig) return;
    applyingConfig = true;
    try {
      await labStore.applyNodeConfig(node.id);
    } finally {
      applyingConfig = false;
    }
  }
</script>

<div class="inspector">
  {#if !node}
    <div class="empty">
      <div class="empty-title">No selection</div>
      <div class="empty-sub">Right-click a node and choose Edit… to inspect it.</div>
    </div>
  {:else}
    <div class="header">
      <span class="state-pill" style:background={`color-mix(in srgb, ${stateColor(nodeState)} 18%, transparent)`} style:color={stateColor(nodeState)}>
        <span class="dot" style:background={stateColor(nodeState)}></span>
        {stateLabel(nodeState)}
      </span>
      <span class="kind">{node.kind.toUpperCase()}</span>
      <button
        class="close-btn"
        title="Close"
        aria-label="Close inspector"
        onclick={() => (labStore.inspectorNodeId = null)}
      >{@html uiSvg("x", 13)}</button>
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

    {#if isIol}
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
          <input type="number" min="32" step="32" value={node.ram ?? 1024} oninput={updateRam} />
        </label>
      </div>

      <div class="field-row">
        <label class="field">
          <span class="label">Ethernet adapters</span>
          <input type="number" min="0" max="16" value={node.ethernet ?? 1} oninput={updateEthernet} />
          {#if ethWarn}<span class="warn">≥ {minEth} needed — links use e{minEth - 1}/x</span>{/if}
        </label>
        <label class="field">
          <span class="label">Serial adapters</span>
          <input type="number" min="0" max="16" value={node.serial ?? 1} oninput={updateSerial} />
          {#if serWarn}<span class="warn">≥ {minSer} needed — links use s{minSer - 1}/x</span>{/if}
        </label>
      </div>

      <div class="field">
        <span class="label">Boot from startup-config</span>
        <button
          class="toggle"
          class:on={node.config?.bootFromStartup !== false}
          role="switch"
          aria-checked={node.config?.bootFromStartup !== false}
          onclick={toggleBoot}
        >
          <span class="sw"></span><span class="tl">{node.config?.bootFromStartup !== false ? "On" : "Off"}</span>
        </button>
      </div>

      <label class="field grow">
        <span class="label">Startup config</span>
        {#if running}
          <button
            class="btn btn-ghost savecfg-btn"
            title="Runs on the node's saved NVRAM — do write memory on the node first"
            onclick={saveConfigFromNvram}
          >Save config from NVRAM</button>
        {/if}
        <textarea
          class="mono config-editor"
          spellcheck="false"
          placeholder="! empty — image default config applies"
          value={node.startupConfig ?? ""}
          oninput={updateConfig}
        ></textarea>
      </label>
    {:else if isTool || isPc}
      {#if isTool}
      <div class="field">
        <span class="label">Pack</span>
        <select class="mono" value={packId} onchange={updatePack}>
          {#if packId && !labStore.toolPacks.some((p) => p.id === packId)}
            <option value={packId}>Unavailable · {packId}</option>
          {/if}
          {#each labStore.toolPacks as pack (pack.id)}
            <option value={pack.id}>{pack.name}</option>
          {/each}
        </select>
        {#if labStore.toolPacksLoading}
          <span class="warn hint">Loading installed packs…</span>
        {:else if labStore.toolPacksError}
          <span class="warn">Could not load installed packs: {labStore.toolPacksError}</span>
        {:else if toolPackInvalid}
          <span class="warn">Choose an installed pack.</span>
        {/if}
      </div>
      {/if}

      <div class="field-row">
        <label class="field">
          <span class="label">IP address</span>
          <input
            type="text"
            placeholder="unaddressed"
            value={netCfg?.ip ?? ""}
            oninput={(e) => updateNet("ip", (e.target as HTMLInputElement).value)}
          />
        </label>
        <label class="field">
          <span class="label">Prefix length</span>
          <input
            type="number"
            min="1"
            max="32"
            value={netCfg?.prefixLen ?? 24}
            oninput={(e) => updateNet("prefixLen", (e.target as HTMLInputElement).value)}
          />
        </label>
      </div>
      <label class="field">
        <span class="label">Gateway (optional)</span>
        <input
          type="text"
          placeholder="none"
          value={netCfg?.gateway ?? ""}
          oninput={(e) => updateNet("gateway", (e.target as HTMLInputElement).value)}
        />
      </label>
      <div class="vpcs-hint">Applied to eth1 when the node starts. PC console commands include <span class="mono">ip</span>, <span class="mono">ip dhcp</span>, <span class="mono">ping</span>, and <span class="mono">trace</span>.</div>
    {:else}
      <div class="vpcs-hint">VPCS nodes take their config from canned commands (set later).</div>
    {/if}

    {#if isIol || isTool || isPc}
      <button
        class="btn btn-success savecfg-btn"
        title="Push the changes above to the supervisor — editing the fields alone doesn't apply them"
        disabled={applyingConfig}
        onclick={applyConfig}
      >{applyingConfig ? "Saving…" : "Save"}</button>
      {#if running}
        <div class="vpcs-hint">Node is running — changes apply on next stop/start.</div>
      {/if}
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
    gap: var(--sp-2);
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
    flex: 1;
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
    font-weight: 600;
    letter-spacing: 0.04em;
  }
  .close-btn {
    all: unset;
    box-sizing: border-box;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: var(--radius-sm);
    color: var(--text-tertiary);
    cursor: pointer;
  }
  .close-btn:hover {
    color: var(--text);
    background: var(--bg-hover);
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
  .warn {
    font-size: var(--fs-xs);
    color: var(--danger);
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
  .toggle {
    all: unset;
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 6px 9px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    background: var(--panel-solid);
    cursor: pointer;
    width: fit-content;
  }
  .toggle .sw {
    width: 26px;
    height: 15px;
    border-radius: var(--radius-full);
    background: var(--border-strong);
    position: relative;
    transition: background var(--transition-fast);
  }
  .toggle .sw::after {
    content: "";
    position: absolute;
    top: 2px;
    left: 2px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--ink);
    transition: transform var(--transition-fast);
  }
  .toggle.on .sw {
    background: var(--accent);
  }
  .toggle.on .sw::after {
    transform: translateX(11px);
  }
  .toggle .tl {
    font-size: var(--fs-xs);
    color: var(--text-tertiary);
  }
  .savecfg-btn {
    align-self: flex-start;
    margin-bottom: 4px;
    font-size: var(--fs-xs);
  }
</style>
