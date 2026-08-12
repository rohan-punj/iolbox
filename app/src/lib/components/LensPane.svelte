<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { consoleUiStore, type ConsoleMark } from "../consoleUiStore.svelte";
  import type { LensEvent, LensProto } from "../lens";

  let {
    linkId,
    title,
    visible,
    focused,
  }: { linkId: number; title: string; visible: boolean; focused: boolean } = $props();

  const FILTERS: LensProto[] = ["arp", "dhcp", "stp", "cdp", "ospf", "hsrp"];
  const FILTER_LABELS: Record<LensProto, string> = {
    arp: "ARP",
    dhcp: "DHCP",
    stp: "STP",
    cdp: "CDP",
    ospf: "OSPF",
    hsrp: "HSRP",
    other: "Other",
  };

  let selectedProtocols = $state<LensProto[]>([]);
  let nodeFilter = $state<number | null>(null);
  let vlanFilter = $state<number | null>(null);
  let scrollEl: HTMLDivElement | undefined = $state();
  let stickToBottom = $state(true);

  const events = $derived(labStore.lensEvents[linkId] ?? []);
  const marks = $derived(consoleUiStore.marks);
  const stats = $derived(labStore.linkStats[linkId]);
  const link = $derived(labStore.lab.links.find((item) => item.id === linkId));
  const filteredEvents = $derived(
    events.filter((event) => {
      const protocolMatch = selectedProtocols.length === 0 || selectedProtocols.includes(event.proto);
      const nodeMatch = nodeFilter === null || event.src?.node === nodeFilter;
      const vlanMatch = vlanFilter === null || event.vlan === vlanFilter;
      return protocolMatch && nodeMatch && vlanMatch;
    })
  );

  type TimelineEntry =
    | { kind: "event"; event: LensEvent }
    | { kind: "mark"; mark: ConsoleMark; positionKnown: boolean };

  const timeline = $derived.by(() => {
    const result: TimelineEntry[] = filteredEvents.map((event) => ({ kind: "event", event }));
    for (const mark of marks) {
      const position = mark.capturePos[linkId];
      if (position === undefined) {
        result.push({ kind: "mark", mark, positionKnown: false });
        continue;
      }
      const index = result.findIndex((entry) => entry.kind === "event" && entry.event.seq >= position);
      result.splice(index < 0 ? result.length : index, 0, { kind: "mark", mark, positionKnown: true });
    }
    return result;
  });

  const firstTimestamp = $derived(events[0]?.tsMicros ?? null);
  const captureLive = $derived(Boolean(link?.capture?.enabled && labStore.labRunning));
  const emptyMessage = $derived(
    !captureLive
      ? "Capture not started. Open the capture tab or start the lab to begin."
      : events.length === 0
        ? "Capture live but no packets yet."
        : "No packets match the current filters."
  );
  const attributionBanner = $derived.by(() => {
    if (!stats) return null;
    if (stats.epAttrib === undefined) {
      return "This appliance could not open per-endpoint classifiers for this link, so events show MAC addresses instead of node names.";
    }
    const ambiguous = stats.epAttrib.find((entry) => entry.state === "ambiguous");
    if (!ambiguous) return null;
    const side = link?.endpoints[ambiguous.endpointIndex]?.node;
    const name = side === undefined
      ? `endpoint ${ambiguous.endpointIndex}`
      : labStore.lab.nodes.find((node) => node.id === side)?.name ?? `endpoint ${ambiguous.endpointIndex}`;
    return `${name} forwards traffic for other devices (it looks like a switch), so its frames show MAC addresses rather than a node name.`;
  });

  function toggleProtocol(proto: LensProto) {
    selectedProtocols = selectedProtocols.includes(proto)
      ? selectedProtocols.filter((item) => item !== proto)
      : [...selectedProtocols, proto];
  }

  function relativeTime(tsMicros: number): string {
    if (firstTimestamp === null) return "+0.0000";
    return `+${Math.max(0, (tsMicros - firstTimestamp) / 1_000_000).toFixed(4)}`;
  }

  function scrollChanged() {
    if (!scrollEl) return;
    stickToBottom = scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < 28;
  }

  $effect(() => {
    const count = timeline.length;
    const isVisible = visible;
    if (!isVisible || !stickToBottom || !scrollEl) return;
    void count;
    requestAnimationFrame(() => {
      if (scrollEl && stickToBottom) scrollEl.scrollTop = scrollEl.scrollHeight;
    });
  });
</script>

<div class="lens-pane" class:focused>
  <div class="lens-header">
    <div class="lens-title" title={title}>Protocol Lens · {title}</div>
    <span class="lens-count">{events.length} / 2000</span>
  </div>

  <div class="preset-row" aria-label="Protocol filters">
    <button class:active={selectedProtocols.length === 0} onclick={() => (selectedProtocols = [])}>All</button>
    {#each FILTERS as proto}
      <button class:active={selectedProtocols.includes(proto)} onclick={() => toggleProtocol(proto)}>
        {FILTER_LABELS[proto]}
      </button>
    {/each}
  </div>

  {#if nodeFilter !== null || vlanFilter !== null}
    <div class="active-filters">
      {#if nodeFilter !== null}
        <button class="filter-chip" onclick={() => (nodeFilter = null)}>
          {labStore.lab.nodes.find((node) => node.id === nodeFilter)?.name ?? `#${nodeFilter}`} ×
        </button>
      {/if}
      {#if vlanFilter !== null}
        <button class="filter-chip" onclick={() => (vlanFilter = null)}>VLAN {vlanFilter} ×</button>
      {/if}
    </div>
  {/if}

  {#if attributionBanner}
    <div class="lens-note">{attributionBanner}</div>
  {/if}

  <div class="lens-events" bind:this={scrollEl} onscroll={scrollChanged}>
    {#if filteredEvents.length === 0}
      <div class="lens-empty">{emptyMessage}</div>
    {:else}
      {#each timeline as entry (entry.kind === "event" ? `e-${entry.event.seq}` : `m-${entry.mark.id}`)}
        {#if entry.kind === "mark"}
          <div class="mark-divider">
            <span>{entry.mark.label}{entry.positionKnown ? "" : " · position unknown"}</span>
          </div>
        {:else}
          {@const event = entry.event}
          <div class="lens-event">
            <span class="event-time mono">{relativeTime(event.tsMicros)}</span>
            {#if event.src}
              <button class="node-chip" onclick={() => (nodeFilter = event.src?.node ?? null)}>{event.src.name}</button>
            {:else}
              <span class="mac-chip mono">{event.srcMac || "—"}</span>
            {/if}
            {#if event.proto === "other"}
              <span class="proto-chip">{event.proto}</span>
            {:else}
              <button class="proto-chip" onclick={() => toggleProtocol(event.proto)}>{FILTER_LABELS[event.proto]}</button>
            {/if}
            <span class="event-text">{event.text}</span>
            {#if event.vlan !== null}
              <button class="vlan-chip" onclick={() => (vlanFilter = event.vlan)}>VLAN {event.vlan}</button>
            {/if}
          </div>
        {/if}
      {/each}
    {/if}
  </div>
</div>

<style>
  .lens-pane {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    background: var(--term-bg);
    color: var(--term-ink);
    font-size: var(--fs-xs);
  }
  .lens-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px 4px;
    flex-shrink: 0;
  }
  .lens-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
    font-weight: 650;
  }
  .lens-count {
    margin-left: auto;
    color: var(--text-tertiary);
    white-space: nowrap;
  }
  .preset-row,
  .active-filters {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 4px 10px;
    flex-shrink: 0;
  }
  .preset-row button,
  .filter-chip,
  .node-chip,
  .proto-chip,
  .vlan-chip {
    all: unset;
    cursor: pointer;
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
    color: var(--text-secondary);
    white-space: nowrap;
  }
  .preset-row button:hover,
  .filter-chip:hover,
  .node-chip:hover,
  .proto-chip:hover,
  .vlan-chip:hover {
    color: var(--text-primary);
    background: var(--bg-hover);
  }
  .preset-row button.active {
    color: var(--accent);
    border-color: var(--accent-muted);
    background: var(--bg-2);
  }
  .filter-chip {
    color: var(--accent);
  }
  .lens-note {
    padding: 4px 10px 7px;
    color: var(--text-secondary);
    line-height: 1.35;
    flex-shrink: 0;
  }
  .lens-events {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px 10px 10px;
    font-family: var(--font-mono);
  }
  .lens-empty {
    padding: 22px 8px;
    color: var(--text-tertiary);
    font-family: var(--font-ui);
  }
  .lens-event {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 5px;
    min-height: 23px;
    line-height: 1.35;
  }
  .event-time {
    color: var(--text-tertiary);
    width: 64px;
    flex: 0 0 64px;
  }
  .node-chip {
    color: var(--accent);
    font-weight: 650;
  }
  .mac-chip {
    color: var(--text-secondary);
  }
  .proto-chip {
    color: var(--state-starting);
    font-weight: 650;
  }
  .event-text {
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }
  .vlan-chip {
    color: var(--warning);
  }
  .mark-divider {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-tertiary);
    font-family: var(--font-ui);
    font-size: 10px;
    margin: 6px 0;
  }
  .mark-divider::before,
  .mark-divider::after {
    content: "";
    height: 1px;
    background: var(--border);
    flex: 1;
  }
</style>
