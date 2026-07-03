<script lang="ts">
  // Open/manage stored labs (feature 3). A card grid of every doc in the durable
  // store; each card renders a mini topology thumbnail (inline SVG of the doc's
  // node x/y normalized into a small box, dots colored by kind, link lines).
  import { onMount } from "svelte";
  import { labStore } from "../labStore.svelte";
  import type { LabDocument, LabNode } from "../labTypes";

  let { onClose }: { onClose: () => void } = $props();

  let labs = $state<LabDocument[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  async function refresh() {
    loading = true;
    error = null;
    try {
      labs = await labStore.listLabs();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }
  onMount(refresh);

  const TH_W = 140;
  const TH_H = 90;
  const PAD = 12;

  type PlacedNode = LabNode & { px: number; py: number };
  type Segment = { x1: number; y1: number; x2: number; y2: number };

  // Normalize a doc's node coordinates into the thumbnail box + build the link
  // segments between placed nodes.
  function thumb(doc: LabDocument): { nodes: PlacedNode[]; segments: Segment[] } {
    const ns = doc.nodes;
    if (ns.length === 0) return { nodes: [], segments: [] };
    const xs = ns.map((n) => n.x);
    const ys = ns.map((n) => n.y);
    const minX = Math.min(...xs), maxX = Math.max(...xs);
    const minY = Math.min(...ys), maxY = Math.max(...ys);
    const spanX = maxX - minX || 1;
    const spanY = maxY - minY || 1;
    const placed: PlacedNode[] = ns.map((n) => ({
      ...n,
      px: PAD + ((n.x - minX) / spanX) * (TH_W - 2 * PAD),
      py: PAD + ((n.y - minY) / spanY) * (TH_H - 2 * PAD),
    }));
    const byId = new Map(placed.map((p) => [p.id, p]));
    const segments: Segment[] = [];
    for (const l of doc.links) {
      const a = byId.get(l.endpoints[0]?.node);
      const b = byId.get(l.endpoints[1]?.node);
      if (a && b) segments.push({ x1: a.px, y1: a.py, x2: b.px, y2: b.py });
    }
    return { nodes: placed, segments };
  }

  function dotColor(kind: string): string {
    switch (kind) {
      case "vpcs":
        return "var(--node-vpcs)";
      case "nat":
      case "mgmt":
        return "var(--accent)";
      default:
        return "var(--node-iol-l3)";
    }
  }

  function fmtDate(iso?: string): string {
    if (!iso) return "";
    const d = new Date(iso);
    if (isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
  }

  async function open(doc: LabDocument) {
    if (doc.id === labStore.lab.id) {
      onClose();
      return;
    }
    // Warn on unsaved changes only when the current lab was never saved OR was
    // modified since last save. We approximate "dirty" as "has content and is
    // not the doc currently being opened".
    if (labStore.lab.nodes.length > 0 && !labStore.currentLabSaved) {
      if (!confirm("The current lab hasn't been saved. Discard it and open this lab?")) return;
    }
    await labStore.openLab(doc, true);
    onClose();
  }

  async function remove(doc: LabDocument, ev: MouseEvent) {
    ev.stopPropagation();
    if (!confirm(`Delete "${doc.name}" from the store? This cannot be undone.`)) return;
    await labStore.deleteLab(doc.id);
    await refresh();
  }

  // Clone a lab (keeps the original — e.g. a starter lab — pristine) and open
  // the copy so the user edits the clone, not the source.
  async function clone(doc: LabDocument, ev: MouseEvent) {
    ev.stopPropagation();
    const copy = await labStore.cloneLab(doc);
    if (copy) {
      await labStore.openLab(copy, true);
      onClose();
    }
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
  <div class="dialog" role="dialog" aria-label="Open lab" aria-modal="true">
    <div class="head">
      <h3>Labs</h3>
      <button class="btn btn-ghost" onclick={onClose}>Close</button>
    </div>

    {#if loading}
      <div class="msg">Loading…</div>
    {:else if error}
      <div class="msg err">Could not list labs: {error}</div>
    {:else if labs.length === 0}
      <div class="msg">No saved labs yet. Use “Save” in the top bar to store the current lab.</div>
    {:else}
      <div class="grid">
        {#each labs as doc (doc.id)}
          {@const t = thumb(doc)}
          <div
            class="card"
            role="button"
            tabindex="0"
            onclick={() => open(doc)}
            onkeydown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                open(doc);
              }
            }}
          >
            <div class="thumb">
              <svg viewBox={`0 0 ${TH_W} ${TH_H}`} width="100%" height="100%" preserveAspectRatio="xMidYMid meet">
                {#each t.segments as s, i (i)}
                  <line x1={s.x1} y1={s.y1} x2={s.x2} y2={s.y2} class="th-link" />
                {/each}
                {#each t.nodes as n (n.id)}
                  <circle cx={n.px} cy={n.py} r="4" fill={dotColor(n.kind)} class="th-node" />
                {/each}
              </svg>
            </div>
            <div class="card-body">
              <div class="card-name" title={doc.name}>{doc.name}</div>
              <div class="card-meta">
                <span>{doc.nodes.length} {doc.nodes.length === 1 ? "node" : "nodes"}</span>
                <span>·</span>
                <span>{doc.links.length} {doc.links.length === 1 ? "link" : "links"}</span>
                {#if doc.modified}
                  <span>·</span>
                  <span>{fmtDate(doc.modified)}</span>
                {/if}
              </div>
            </div>
            <div class="card-actions">
              <button
                class="card-act"
                title="Clone this lab (keeps the original untouched)"
                aria-label="Clone lab"
                onclick={(e) => clone(doc, e)}
              >
                <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="9" y="9" width="12" height="12" rx="2" />
                  <path d="M5 15V5a2 2 0 0 1 2-2h10" />
                </svg>
              </button>
              <button
                class="card-act card-del"
                title="Delete lab"
                aria-label="Delete lab"
                onclick={(e) => remove(doc, e)}
              >✕</button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

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
    width: min(760px, 92vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    background: var(--panel-2);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 18px;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 14px;
  }
  h3 {
    margin: 0;
    font-size: var(--fs-lg);
    color: var(--ink);
  }
  .msg {
    padding: 30px 10px;
    text-align: center;
    color: var(--ink-3);
    font-size: var(--fs-sm);
  }
  .msg.err {
    color: var(--danger);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 12px;
    overflow-y: auto;
    padding: 2px;
  }
  .card {
    all: unset;
    position: relative;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    background: var(--bg-2);
    cursor: pointer;
    overflow: hidden;
    transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  }
  .card:hover {
    border-color: var(--accent);
    box-shadow: var(--shadow-sm);
  }
  .thumb {
    height: 96px;
    background: var(--ground);
    border-bottom: 1px solid var(--border-subtle);
  }
  .th-link {
    stroke: var(--cable);
    stroke-width: 1.2;
  }
  .th-node {
    stroke: var(--ground);
    stroke-width: 1;
  }
  .card-body {
    padding: 8px 10px;
  }
  .card-name {
    font-size: var(--fs-sm);
    font-weight: 600;
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .card-meta {
    display: flex;
    gap: 4px;
    margin-top: 3px;
    font-size: 11px;
    color: var(--ink-3);
    font-family: var(--font-mono);
  }
  .card-actions {
    position: absolute;
    top: 5px;
    right: 5px;
    display: flex;
    gap: 4px;
    opacity: 0;
    transition: opacity var(--transition-fast);
  }
  .card:hover .card-actions,
  .card:focus-within .card-actions {
    opacity: 1;
  }
  .card-act {
    all: unset;
    width: 20px;
    height: 20px;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    background: color-mix(in oklab, var(--ground) 70%, transparent);
    color: var(--ink-3);
    font-size: 11px;
    cursor: pointer;
    transition: color var(--transition-fast), background var(--transition-fast);
  }
  .card-act:hover {
    color: var(--ink);
    background: var(--ground);
  }
  .card-del:hover {
    color: var(--danger);
    background: var(--ground);
  }
</style>
