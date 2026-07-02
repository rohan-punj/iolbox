<script lang="ts">
  // R2.1 — Interface Picker popover, shown on drop of a link-add drag. Two
  // selects (local iface on node A, remote iface on node B) listing each node's
  // FREE interfaces, pre-selecting the next free one. VPCS collapses to eth0.
  // Confirm creates the link via labStore + client.linkAdd; Cancel aborts.
  import { onMount } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { allInterfaces, usedInterfaces, freeInterfaces, nextFreeInterface } from "../interfaces";
  import type { LabNode, LabLink } from "../labTypes";

  let {
    x,
    y,
    sourceId,
    targetId,
    onClose,
  }: {
    x: number;
    y: number;
    sourceId: number;
    targetId: number;
    onClose: () => void;
  } = $props();

  const nodeA = $derived(labStore.lab.nodes.find((n) => n.id === sourceId) as LabNode | undefined);
  const nodeB = $derived(labStore.lab.nodes.find((n) => n.id === targetId) as LabNode | undefined);

  // Per-node interface option lists: all interfaces, with used ones flagged.
  function options(node: LabNode | undefined) {
    if (!node) return [] as { iface: string; used: boolean }[];
    const used = usedInterfaces(node.id);
    return allInterfaces(node).map((iface) => ({ iface, used: used.has(iface) }));
  }
  const optsA = $derived(options(nodeA));
  const optsB = $derived(options(nodeB));

  let ifA = $state("");
  let ifB = $state("");
  onMount(() => {
    if (nodeA) ifA = nextFreeInterface(nodeA);
    if (nodeB) ifB = nextFreeInterface(nodeB);
  });

  const noFreeA = $derived(nodeA ? freeInterfaces(nodeA).length === 0 : true);
  const noFreeB = $derived(nodeB ? freeInterfaces(nodeB).length === 0 : true);
  const canConnect = $derived(!!nodeA && !!nodeB && !noFreeA && !noFreeB && ifA !== "" && ifB !== "");

  let el: HTMLDivElement | undefined = $state();
  let armed = $state(false);
  onMount(() => {
    const t = setTimeout(() => (armed = true), 0);
    return () => clearTimeout(t);
  });
  function handleWindowDown(e: MouseEvent) {
    if (!armed) return;
    if (el && !el.contains(e.target as Node)) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
  }

  function confirm() {
    if (!canConnect || !nodeA || !nodeB) return;
    const link: LabLink = {
      id: labStore.nextLinkId(),
      type: "p2p",
      endpoints: [
        { node: nodeA.id, interface: ifA },
        { node: nodeB.id, interface: ifB },
      ],
    };
    labStore.addLink(link);
    if (labStore.lab.id) void labStore.client.linkAdd(labStore.lab.id, link);
    onClose();
  }

  // Clamp within viewport.
  const px = $derived(Math.max(8, Math.min(x, window.innerWidth - 268)));
  const py = $derived(Math.max(56, Math.min(y, window.innerHeight - 200)));
</script>

<svelte:window onmousedown={handleWindowDown} onkeydown={handleKey} />

<div class="ifpick" bind:this={el} style:left={`${px}px`} style:top={`${py}px`} role="dialog" aria-label="Choose interfaces">
  <div class="ph">Connect {nodeA?.name ?? "?"} &harr; {nodeB?.name ?? "?"}</div>
  <div class="sub">Dropped on {nodeB?.name ?? "node"}. Choose the interfaces to wire.</div>
  <div class="two">
    <div class="col">
      <span class="lab">{nodeA?.name ?? ""} — local</span>
      {#if nodeA?.kind === "vpcs"}
        <div class="fixed mono">eth0</div>
      {:else if noFreeA}
        <div class="fixed mono none">no free ports</div>
      {:else}
        <select class="mono" bind:value={ifA} aria-label="Local interface">
          {#each optsA as o (o.iface)}
            <option value={o.iface} disabled={o.used}>{o.iface}{o.used ? " (used)" : ""}</option>
          {/each}
        </select>
      {/if}
    </div>
    <span class="arrow mono">&harr;</span>
    <div class="col">
      <span class="lab">{nodeB?.name ?? ""} — remote</span>
      {#if nodeB?.kind === "vpcs"}
        <div class="fixed mono">eth0</div>
      {:else if noFreeB}
        <div class="fixed mono none">no free ports</div>
      {:else}
        <select class="mono" bind:value={ifB} aria-label="Remote interface">
          {#each optsB as o (o.iface)}
            <option value={o.iface} disabled={o.used}>{o.iface}{o.used ? " (used)" : ""}</option>
          {/each}
        </select>
      {/if}
    </div>
  </div>
  <div class="acts">
    <button class="btn btn-ghost" onclick={onClose}>Cancel</button>
    <button class="btn btn-primary" onclick={confirm} disabled={!canConnect}>Connect</button>
  </div>
</div>

<style>
  .ifpick {
    position: fixed;
    z-index: 1200;
    width: 258px;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 12px;
  }
  .ph {
    font-size: var(--fs-sm);
    font-weight: 650;
    margin-bottom: 2px;
    color: var(--ink);
  }
  .sub {
    font-size: var(--fs-xs);
    color: var(--ink-3);
    margin-bottom: 10px;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: end;
    gap: 8px;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .lab {
    font-size: 9px;
    letter-spacing: var(--ls-eyebrow);
    text-transform: uppercase;
    color: var(--ink-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .arrow {
    color: var(--ink-3);
    padding-bottom: 7px;
  }
  select {
    width: 100%;
    font-size: var(--fs-sm);
    background: var(--panel-solid);
    color: var(--ink);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 6px;
  }
  .fixed {
    font-size: var(--fs-sm);
    color: var(--ink-2);
    padding: 7px 2px;
  }
  .fixed.none {
    color: var(--danger);
  }
  .acts {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
  }
</style>
