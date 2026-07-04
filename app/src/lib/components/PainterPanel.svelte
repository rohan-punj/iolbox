<script lang="ts">
  // Topology Painter panel (WS5b) — a compact floating card over the canvas,
  // sibling to the Network Watcher. The user picks a protocol (STP / OSPF /
  // EIGRP / BGP) and, for routing protocols, a destination (a node loopback or
  // a typed prefix), then hits Paint for a one-shot live snapshot. All state
  // lives in painterStore; FloatingEdge reads the same store to draw the
  // badges/highlights, so this panel is pure chrome + the paint trigger.
  import {
    painterStore,
    PAINTER_PROTOS,
    PAINTER_PROTO_NAMES,
    destUsed,
    destRequired,
  } from "../painterStore.svelte";
  import { labStore } from "../labStore.svelte";
  import { watcherStore } from "../watcherStore.svelte";
  import type { PainterProto } from "../painterTypes";

  // Stack below the watcher card when it's also open so both stay usable
  // top-right. The watcher card is ~variable height; 320px clears it for the
  // common (1–2 row) case and the two never fully overlap regardless.
  const topOffset = $derived(watcherStore.panelOpen ? 332 : 12);

  const proto = $derived(painterStore.proto);
  const result = $derived(painterStore.result);
  const busy = $derived(painterStore.busy);
  const isStp = $derived(proto === "stp");

  // IOL nodes offered as destination picks (routing protocols). A pick just
  // fills the prefix box with a placeholder the user can refine — the frontend
  // has no authoritative loopback address, so we suggest a "<name> loopback"
  // hint and let them type the real prefix.
  const iolNodes = $derived(labStore.lab.nodes.filter((n) => n.kind === "iol"));

  // Any running node at all? Paint is pointless (and the backend returns all
  // hints) with nothing up — surface that up front.
  const anyRunning = $derived(
    Object.values(labStore.nodeStates).some((s) => s === "running")
  );

  // Nodes in the current snapshot that returned a not-running / no-data hint.
  const hintNodes = $derived(
    (result?.nodes ?? []).filter((n) => !n.running && n.hint)
  );

  // STP node -> VLAN flow state (painterStore).
  const stpNodeId = $derived(painterStore.stpNodeId);
  const stpVlans = $derived(painterStore.stpVlans);
  const stpVlanId = $derived(painterStore.stpVlanId);
  const stpVlansBusy = $derived(painterStore.stpVlansBusy);
  const stpVlansHint = $derived(painterStore.stpVlansHint);
  const canPaintStp = $derived(painterStore.canPaintStp);

  function fmtAgo(ts: number | null): string {
    if (ts == null) return "";
    const secs = Math.max(0, Math.round((labStore.nowTick - ts) / 1000));
    if (secs < 60) return `${secs}s ago`;
    return `${Math.round(secs / 60)}m ago`;
  }
</script>

{#if painterStore.panelOpen}
  <div class="painter-panel" role="dialog" aria-label="Topology Painter" style:top={`${topOffset}px`}>
    <div class="pp-header">
      <span class="pp-title">Topology Painter</span>
      <button
        class="pp-close"
        title="Close panel (a painted snapshot stays on the canvas)"
        aria-label="Close"
        onclick={() => (painterStore.panelOpen = false)}
      >✕</button>
    </div>

    <div class="pp-body">
      <!-- Protocol picker: a segmented row of the four paintable protocols. -->
      <div class="pp-protos" role="radiogroup" aria-label="Protocol">
        {#each PAINTER_PROTOS as p (p)}
          <button
            class="pp-proto"
            class:on={proto === p}
            role="radio"
            aria-checked={proto === p}
            onclick={() => painterStore.setProto(p as PainterProto)}
          >{PAINTER_PROTO_NAMES[p]}</button>
        {/each}
      </div>

      <!-- STP node -> VLAN flow: pick the probe node, Detect VLANs, then pick
           the VLAN to paint. STP is per-VLAN (backend redesign) — there is no
           destination for STP, just this three-step chain, and Paint stays
           disabled until a VLAN is chosen. -->
      {#if isStp}
        <div class="pp-stp">
          <label class="pp-dest-label" for="pp-stp-node">Node to probe</label>
          <div class="pp-dest-row">
            <select
              id="pp-stp-node"
              class="pp-stp-node"
              aria-label="Pick the node to probe for VLANs"
              value={stpNodeId ?? ""}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value;
                painterStore.setStpNode(v === "" ? null : Number(v));
              }}
            >
              <option value="">— node —</option>
              {#each iolNodes as n (n.id)}
                <option value={n.id}>{n.name}</option>
              {/each}
            </select>
            <button
              class="pp-detect"
              disabled={stpNodeId == null || stpVlansBusy}
              onclick={() => painterStore.detectVlans()}
            >{stpVlansBusy ? "Detecting…" : "Detect VLANs"}</button>
          </div>

          {#if stpVlans.length}
            <label class="pp-dest-label" for="pp-stp-vlan">VLAN</label>
            <select
              id="pp-stp-vlan"
              class="pp-stp-vlan"
              aria-label="Pick the VLAN to paint"
              value={stpVlanId ?? ""}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value;
                painterStore.stpVlanId = v === "" ? null : Number(v);
              }}
            >
              {#each stpVlans as v (v.id)}
                <option value={v.id}>{v.id} — {v.name}</option>
              {/each}
            </select>
          {/if}

          {#if stpVlansHint}
            <div class="pp-note pp-warn">{stpVlansHint}</div>
          {/if}
        </div>
      {/if}

      <!-- Destination selector — only for routing protocols. A node picker
           (fills the prefix box) plus a free-text prefix/host input. -->
      {#if destUsed(proto)}
        <div class="pp-dest">
          <label class="pp-dest-label" for="pp-dest-input">
            Destination{destRequired(proto) ? "" : " (optional)"}
          </label>
          <div class="pp-dest-row">
            <select
              class="pp-dest-node"
              aria-label="Pick a destination node"
              value={painterStore.destNodeId ?? ""}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value;
                if (v === "") {
                  painterStore.destNodeId = null;
                  return;
                }
                const id = Number(v);
                painterStore.destNodeId = id;
                const n = labStore.lab.nodes.find((x) => x.id === id);
                // Suggest a loopback-style prefix; the user refines the real one.
                painterStore.destText = `${n?.name ?? "R" + id} loopback`;
              }}
            >
              <option value="">— node —</option>
              {#each iolNodes as n (n.id)}
                <option value={n.id}>{n.name}</option>
              {/each}
            </select>
            <input
              id="pp-dest-input"
              class="pp-dest-input"
              type="text"
              placeholder="prefix / host (e.g. 10.0.0.0/24)"
              aria-label="Destination prefix or host"
              value={painterStore.destText}
              oninput={(e) => {
                painterStore.destText = (e.currentTarget as HTMLInputElement).value;
                painterStore.destNodeId = null;
              }}
            />
          </div>
        </div>
      {/if}

      {#if !anyRunning}
        <div class="pp-note pp-warn">
          No nodes are running — start the lab first, then paint.
        </div>
      {/if}

      {#if painterStore.error}
        <div class="pp-note pp-err">{painterStore.error}</div>
      {/if}

      <!-- Snapshot status: which proto/dest is displayed + how fresh. -->
      {#if result}
        <div class="pp-snapshot">
          <div class="pp-snap-line">
            <span class="pp-snap-proto">{PAINTER_PROTO_NAMES[result.proto]}</span>
            {#if result.proto === "stp" && result.vlan}
              <span class="pp-snap-dest">VLAN {result.vlan}</span>
            {:else if result.dest}
              <span class="pp-snap-dest">→ {result.dest}</span>
            {/if}
            <span class="pp-snap-age">{fmtAgo(painterStore.paintedAt)}</span>
          </div>
          {#if painterStore.bgpReason}
            <div class="pp-reason" title="BGP best-path tiebreak">
              {painterStore.bgpReason}
            </div>
          {/if}
          {#each hintNodes as hn (hn.node)}
            <div class="pp-hint">
              {labStore.lab.nodes.find((x) => x.id === hn.node)?.name ?? "#" + hn.node}:
              {hn.hint}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="pp-footer">
      <button
        class="pp-run"
        disabled={busy || (isStp && !canPaintStp)}
        title={isStp && !canPaintStp ? "Pick a node and detect a VLAN first" : undefined}
        onclick={() => painterStore.paint()}
      >
        {#if busy}Painting…{:else if result}Re-paint{:else}Paint{/if}
      </button>
      {#if result}
        <button class="pp-clear" onclick={() => painterStore.clear()}>Clear</button>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* Floating card over the canvas — same surface treatment as the watcher, but
     offset lower so both can be open at once without overlapping. */
  .painter-panel {
    position: absolute;
    top: 12px;
    right: 12px;
    width: 268px;
    z-index: 60;
    display: flex;
    flex-direction: column;
    background: var(--bg-2);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    overflow: hidden;
  }
  .pp-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-bottom: 1px solid var(--border);
  }
  .pp-title {
    font-size: var(--fs-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--ink-2);
  }
  .pp-close {
    all: unset;
    box-sizing: border-box;
    cursor: pointer;
    color: var(--ink-3);
    font-size: 11px;
    line-height: 1;
    padding: 3px 5px;
    border-radius: var(--radius-sm);
  }
  .pp-close:hover {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .pp-body {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 8px 10px;
  }
  /* Segmented protocol picker. */
  .pp-protos {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 4px;
  }
  .pp-proto {
    all: unset;
    box-sizing: border-box;
    text-align: center;
    cursor: pointer;
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--ink-2);
    padding: 5px 0;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--bg-1);
  }
  .pp-proto:hover {
    color: var(--ink);
    border-color: var(--accent);
  }
  .pp-proto.on {
    color: #fff;
    background: color-mix(in oklab, var(--accent) 88%, var(--bg-2));
    border-color: var(--accent);
  }
  .pp-dest,
  .pp-stp {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .pp-stp-node {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 6px;
  }
  .pp-detect {
    all: unset;
    box-sizing: border-box;
    flex: 0 0 auto;
    text-align: center;
    cursor: pointer;
    font-size: var(--fs-xs);
    font-weight: 600;
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 8px;
    white-space: nowrap;
  }
  .pp-detect:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }
  .pp-detect:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .pp-stp-vlan {
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 6px;
  }
  .pp-dest-label {
    font-size: var(--fs-xs);
    color: var(--ink-3);
  }
  .pp-dest-row {
    display: flex;
    gap: 6px;
  }
  .pp-dest-node {
    flex: 0 0 84px;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 6px;
  }
  .pp-dest-input {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-xs);
    color: var(--ink);
    background: var(--bg-1);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 4px 6px;
  }
  .pp-dest-input::placeholder {
    color: var(--ink-3);
  }
  .pp-note {
    font-size: var(--fs-xs);
    line-height: 1.35;
    padding: 5px 7px;
    border-radius: var(--radius-sm);
  }
  .pp-warn {
    color: var(--ink-2);
    background: var(--bg-1);
    border: 1px solid var(--border);
  }
  .pp-err {
    color: #fff;
    background: color-mix(in oklab, #c74848 82%, var(--bg-2));
  }
  .pp-snapshot {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding-top: 4px;
    border-top: 1px solid var(--border);
  }
  .pp-snap-line {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: var(--fs-xs);
  }
  .pp-snap-proto {
    font-weight: 700;
    color: var(--ink);
  }
  .pp-snap-dest {
    color: var(--ink-2);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pp-snap-age {
    margin-left: auto;
    color: var(--ink-3);
  }
  .pp-reason {
    font-size: var(--fs-xs);
    line-height: 1.35;
    color: var(--ink-2);
    background: var(--bg-1);
    border-left: 3px solid var(--accent);
    padding: 5px 7px;
    border-radius: var(--radius-sm);
  }
  .pp-hint {
    font-size: var(--fs-xs);
    line-height: 1.35;
    color: var(--ink-3);
  }
  .pp-footer {
    display: flex;
    gap: 8px;
    padding: 8px 10px;
    border-top: 1px solid var(--border);
  }
  .pp-run {
    all: unset;
    box-sizing: border-box;
    flex: 1;
    text-align: center;
    padding: 6px 0;
    font-size: var(--fs-xs);
    font-weight: 600;
    border-radius: var(--radius-sm);
    cursor: pointer;
    color: #fff;
    background: color-mix(in oklab, var(--accent) 88%, var(--bg-2));
  }
  .pp-run:hover {
    background: var(--accent);
  }
  .pp-run:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .pp-clear {
    all: unset;
    box-sizing: border-box;
    flex: 0 0 auto;
    text-align: center;
    padding: 6px 12px;
    font-size: var(--fs-xs);
    font-weight: 600;
    border-radius: var(--radius-sm);
    cursor: pointer;
    color: var(--ink-2);
    border: 1px solid var(--border-strong);
    background: var(--bg-1);
  }
  .pp-clear:hover {
    color: var(--ink);
    border-color: var(--accent);
  }
</style>
