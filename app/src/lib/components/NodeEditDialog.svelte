<script lang="ts">
  // R2.2 — Node edit dialog. Full-form editor for a lab node: name, icon, image,
  // RAM, ethernet/serial adapter counts, boot-from-startup-config toggle + the
  // startup-config editor. Opened from the node context menu ("Edit…"),
  // double-click, and the Inspector's "Edit node…" button. Changes persist onto
  // the lab node via labStore; adapter/RAM/image changes on a running node show a
  // "restart required" hint. Reducing adapter counts below what existing links
  // use is warned and blocked from silently orphaning links.
  import { onMount } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { iconSvg, defaultIconFor, iconRegistryVersion } from "../icons.svelte";
  import { usedInterfaces, allInterfaces } from "../interfaces";
  import IconPicker from "./IconPicker.svelte";
  import type { LabNode } from "../labTypes";

  let { nodeId, onClose }: { nodeId: number; onClose: () => void } = $props();

  const node = $derived(labStore.lab.nodes.find((n) => n.id === nodeId) as LabNode | undefined);
  const image = $derived(labStore.images.find((i) => i.id === node?.image?.id));
  const nodeState = $derived(labStore.nodeStates[nodeId] ?? "stopped");
  const running = $derived(nodeState === "running" || nodeState === "starting");
  const isIol = $derived(node?.kind === "iol");

  // Local editable copy — committed to the lab node on Save.
  let name = $state("");
  let icon = $state<string | undefined>(undefined);
  let imageId = $state<string | undefined>(undefined);
  let ram = $state(256);
  let ethernet = $state(1);
  let serial = $state(1);
  let bootConfig = $state(true);
  let startupConfig = $state("");

  onMount(() => {
    if (!node) return;
    name = node.name;
    icon = node.icon;
    imageId = node.image?.id;
    ram = node.ram ?? (node.kind === "iol" ? 256 : 0);
    ethernet = node.ethernet ?? 1;
    serial = node.serial ?? 0;
    bootConfig = node.config?.bootFromStartup !== false;
    startupConfig = node.startupConfig ?? "";
  });

  const iconKey = $derived(
    (iconRegistryVersion(), icon ?? defaultIconFor(node?.kind ?? "iol", image?.class ?? node?.image?.class))
  );
  const iconMarkup = $derived(iconSvg(iconKey, 21));
  const kindLabel = $derived(
    node?.kind === "vpcs" ? "VPCS" : (image?.class ?? node?.image?.class) === "l2" ? "IOL · L2" : "IOL · L3"
  );

  // Highest adapter group index consumed by an existing link, per family.
  // e.g. e2/3 links → ethernet must stay ≥ 3 (groups 0..2). Warn if the edited
  // count would drop below the count of groups that currently carry links.
  function minGroups(family: "e" | "s"): number {
    const used = usedInterfaces(nodeId);
    let max = -1;
    for (const iface of used) {
      const m = iface.match(/^([es])(\d+)\/(\d+)$/);
      if (m && m[1] === family) max = Math.max(max, Number(m[2]));
    }
    return max + 1; // number of groups that must remain
  }
  const minEth = $derived((usedInterfaces(nodeId), minGroups("e")));
  const minSer = $derived((usedInterfaces(nodeId), minGroups("s")));
  const ethWarn = $derived(isIol && ethernet < minEth);
  const serWarn = $derived(isIol && serial < minSer);
  const hasWarn = $derived(ethWarn || serWarn);

  // "restart required" hint: only when a runtime-affecting field changed.
  const restartHint = $derived(
    running &&
      !!node &&
      (ram !== (node.ram ?? (node.kind === "iol" ? 256 : 0)) ||
        ethernet !== (node.ethernet ?? 1) ||
        serial !== (node.serial ?? 0) ||
        imageId !== node.image?.id)
  );

  let iconPickerPos = $state<{ x: number; y: number } | null>(null);
  function openIconPicker(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    iconPickerPos = { x: r.right + 8, y: r.top };
  }

  let el: HTMLDivElement | undefined = $state();
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }
  function onScrimDown(e: MouseEvent) {
    if (e.target === e.currentTarget) onClose();
  }

  async function save() {
    if (!node || hasWarn) return;
    node.name = name.trim() || node.name;
    node.icon = icon;
    if (isIol) {
      node.ram = Math.max(0, ram);
      node.ethernet = Math.max(0, ethernet);
      node.serial = Math.max(0, serial);
      node.config = { ...(node.config ?? {}), bootFromStartup: bootConfig };
      node.startupConfig = startupConfig;
      // Hot-swap image by id through the store (also notifies supervisor).
      if (imageId && imageId !== node.image?.id) {
        await labStore.setNodeImage(nodeId, imageId);
      }
    }
    onClose();
  }
</script>

<svelte:window onkeydown={handleKey} />

<div class="scrim" role="presentation" onmousedown={onScrimDown}>
  <div class="dialog" bind:this={el} role="dialog" aria-label="Edit node" aria-modal="true">
    {#if node}
      <h3>Edit {node.name}</h3>
      <div class="dsub">{kindLabel}</div>

      <div class="dgrid">
        <div class="field">
          <span class="label">Name</span>
          <input class="mono" bind:value={name} />
        </div>

        <div class="field">
          <span class="label">Icon</span>
          <div class="icon-inline">
            <div class="prev">{@html iconMarkup}</div>
            <button class="btn btn-ghost" onclick={openIconPicker}>Change icon…</button>
          </div>
        </div>

        {#if isIol}
          <div class="field">
            <span class="label">Image</span>
            <select class="mono" bind:value={imageId}>
              {#each labStore.images as img (img.id)}
                <option value={img.id}>{img.class.toUpperCase()} · {img.filename}</option>
              {/each}
            </select>
          </div>

          <div class="row2">
            <div class="field">
              <span class="label">RAM (MB)</span>
              <input class="mono" type="number" min="32" step="32" bind:value={ram} />
            </div>
            <div class="field">
              <span class="label">Ethernet adapters</span>
              <input class="mono" type="number" min="0" max="16" bind:value={ethernet} />
              {#if ethWarn}<span class="warn">≥ {minEth} needed — links use e{minEth - 1}/x</span>{/if}
            </div>
          </div>

          <div class="row2">
            <div class="field">
              <span class="label">Serial adapters</span>
              <input class="mono" type="number" min="0" max="16" bind:value={serial} />
              {#if serWarn}<span class="warn">≥ {minSer} needed — links use s{minSer - 1}/x</span>{/if}
            </div>
            <div class="field">
              <span class="label">Boot from startup-config</span>
              <button
                class="toggle"
                class:on={bootConfig}
                role="switch"
                aria-checked={bootConfig}
                onclick={() => (bootConfig = !bootConfig)}
              >
                <span class="sw"></span><span class="tl">{bootConfig ? "On" : "Off"}</span>
              </button>
            </div>
          </div>

          <div class="field">
            <span class="label">Startup-config</span>
            <textarea
              class="mono cfg"
              spellcheck="false"
              placeholder="! empty — image default config applies"
              bind:value={startupConfig}
            ></textarea>
          </div>
        {:else}
          <div class="vpcs-hint">VPCS nodes have a single eth0 interface and take canned commands.</div>
        {/if}

        {#if restartHint}
          <div class="restart">Node is running — image/RAM/adapter changes apply on next start.</div>
        {/if}
      </div>

      <div class="acts">
        <button class="btn btn-ghost" onclick={onClose}>Cancel</button>
        <button class="btn btn-primary" onclick={save} disabled={hasWarn}>Save</button>
      </div>
    {/if}
  </div>
</div>

{#if iconPickerPos && node}
  <IconPicker
    x={iconPickerPos.x}
    y={iconPickerPos.y}
    current={iconKey}
    onPick={(key) => {
      icon = key;
      iconPickerPos = null;
    }}
    onClose={() => (iconPickerPos = null)}
  />
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 1100;
    display: grid;
    place-items: center;
    background: rgba(4, 8, 13, 0.5);
    -webkit-backdrop-filter: blur(3px);
    backdrop-filter: blur(3px);
  }
  :global([data-theme="glass"]) .scrim {
    background: rgba(120, 140, 170, 0.28);
  }
  .dialog {
    width: 390px;
    max-height: 88vh;
    overflow: auto;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 18px;
  }
  h3 {
    margin: 0 0 2px;
    font-size: var(--fs-lg);
    color: var(--ink);
  }
  .dsub {
    font-size: var(--fs-xs);
    color: var(--ink-3);
    margin-bottom: 16px;
  }
  .dgrid {
    display: flex;
    flex-direction: column;
    gap: 13px;
  }
  .row2 {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .label {
    font-size: 9.5px;
    letter-spacing: var(--ls-eyebrow);
    text-transform: uppercase;
    color: var(--ink-3);
  }
  input,
  select,
  textarea {
    width: 100%;
    font-size: var(--fs-sm);
    color: var(--ink);
    background: var(--panel-solid);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 8px;
  }
  textarea.cfg {
    min-height: 80px;
    line-height: 1.5;
    resize: vertical;
  }
  .icon-inline {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .icon-inline .prev {
    width: 38px;
    height: 38px;
    border-radius: 9px;
    background: var(--node-face-2);
    border: 1px solid var(--border-strong);
    display: grid;
    place-items: center;
    color: var(--accent);
    flex-shrink: 0;
  }
  .icon-inline .prev :global(svg),
  .icon-inline .prev :global(img) {
    width: 21px;
    height: 21px;
  }
  .toggle {
    all: unset;
    display: inline-flex;
    align-items: center;
    gap: 10px;
    cursor: pointer;
  }
  .toggle .sw {
    width: 38px;
    height: 22px;
    border-radius: 999px;
    background: var(--border-strong);
    position: relative;
    transition: background 0.15s ease;
    flex-shrink: 0;
  }
  .toggle.on .sw {
    background: var(--accent);
  }
  .toggle .sw::after {
    content: "";
    position: absolute;
    top: 2px;
    left: 2px;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: #fff;
    transition: left 0.15s ease;
  }
  .toggle.on .sw::after {
    left: 18px;
  }
  .toggle .tl {
    font-size: var(--fs-sm);
    color: var(--ink);
  }
  .warn {
    font-size: 10.5px;
    color: var(--danger);
  }
  .restart {
    font-size: 11px;
    color: var(--warning);
    background: color-mix(in oklab, var(--warning) 12%, transparent);
    border: 1px solid color-mix(in oklab, var(--warning) 30%, transparent);
    border-radius: var(--radius-sm);
    padding: 7px 9px;
  }
  .vpcs-hint {
    font-size: var(--fs-xs);
    color: var(--ink-3);
    line-height: 1.5;
  }
  .acts {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 18px;
  }
</style>
