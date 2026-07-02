<script lang="ts">
  // D2 + D3 — a custom floating edge with PNetLab-style hover-pop port chips.
  //
  // The path re-anchors live because it reads the two nodes via useInternalNode,
  // whose positionAbsolute updates every animation frame during a drag. The two
  // HTML chips (one per endpoint) render through EdgeLabel (xyflow's
  // EdgeLabelRenderer equivalent). EdgeLabel owns the positioning transform
  // (inline translate(-50%,-50%) translate(x,y)), so the hover-pop scale lives on
  // an *inner* element to avoid fighting that inline transform: small +
  // low-contrast at rest, scaling ~1.65× with a glass tooltip + full detail on
  // hover, transform-origin pointing back toward the node.
  import {
    BaseEdge,
    EdgeLabel,
    getBezierPath,
    useInternalNode,
    type EdgeProps,
  } from "@xyflow/svelte";
  import { getEdgeParams } from "./floating";

  let { id, source, target, selected, data }: EdgeProps = $props();

  // An edge's endpoints are stable for its lifetime — xyflow remounts the edge
  // component if source/target change (the id key changes with them). The hooks
  // are internally reactive over the node lookup, so reading the initial id here
  // is correct.
  // svelte-ignore state_referenced_locally
  const sourceNode = useInternalNode(source);
  // svelte-ignore state_referenced_locally
  const targetNode = useInternalNode(target);

  type EndpointInfo = { name: string; iface: string; telnet?: number };
  const info = $derived(
    (data as { source?: EndpointInfo; target?: EndpointInfo; capture?: boolean } | undefined) ?? {}
  );

  // R2.3 — hovering either chip glows the whole cable (same as hovering the
  // edge). `hot` mirrors the edge path's hover state to the chips and vice-versa.
  let hot = $state(false);

  // Bezier path + label anchor points, recomputed whenever either node moves.
  const geom = $derived.by(() => {
    const s = sourceNode.current;
    const t = targetNode.current;
    if (!s || !t) return null;
    const { sx, sy, tx, ty, sourcePos, targetPos } = getEdgeParams(s, t);
    const [path] = getBezierPath({
      sourceX: sx,
      sourceY: sy,
      sourcePosition: sourcePos,
      targetPosition: targetPos,
      targetX: tx,
      targetY: ty,
    });
    // Nudge each chip a little in from the node toward the link so it clears the
    // node face; transform-origin then points back toward the node.
    const dx = tx - sx;
    const dy = ty - sy;
    const len = Math.hypot(dx, dy) || 1;
    const off = 15;
    return {
      path,
      sChip: { x: sx + (dx / len) * off, y: sy + (dy / len) * off },
      tChip: { x: tx - (dx / len) * off, y: ty - (dy / len) * off },
      // origin inside each chip pointing back at its own node
      sOrigin: dx >= 0 ? "left" : "right",
      tOrigin: dx >= 0 ? "right" : "left",
    };
  });
</script>

{#if geom}
  <BaseEdge
    {id}
    path={geom.path}
    class={"floating-edge" +
      (selected ? " is-selected" : "") +
      (info.capture ? " is-capture" : "") +
      (hot ? " is-hot" : "")}
  />

  {#if info.source}
    <EdgeLabel x={geom.sChip.x} y={geom.sChip.y} class="port-chip-slot">
      <span
        class="port-chip"
        class:chip-hot={hot}
        style={`transform-origin:${geom.sOrigin} center`}
        onpointerenter={() => (hot = true)}
        onpointerleave={() => (hot = false)}
        role="presentation"
      >
        <span class="chip-detail">{info.source.name} </span>{info.source.iface}{#if info.source.telnet}<span
            class="chip-sep">·</span><span class="chip-detail">telnet {info.source.telnet}</span
          >{/if}
      </span>
    </EdgeLabel>
  {/if}

  {#if info.target}
    <EdgeLabel x={geom.tChip.x} y={geom.tChip.y} class="port-chip-slot">
      <span
        class="port-chip"
        class:chip-hot={hot}
        style={`transform-origin:${geom.tOrigin} center`}
        onpointerenter={() => (hot = true)}
        onpointerleave={() => (hot = false)}
        role="presentation"
      >
        <span class="chip-detail">{info.target.name} </span>{info.target.iface}{#if info.target.telnet}<span
            class="chip-sep">·</span><span class="chip-detail">telnet {info.target.telnet}</span
          >{/if}
      </span>
    </EdgeLabel>
  {/if}
{/if}

<style>
  /* Edge path tints (cable / selected / capture-active). */
  :global(.svelte-flow__edge .floating-edge) {
    stroke: var(--cable);
    stroke-width: 2;
    /* R2.3 — glow + width change animate together (~120ms). */
    transition: stroke 120ms ease, stroke-width 120ms ease, filter 120ms ease;
  }
  :global(.svelte-flow__edge .floating-edge.is-selected) {
    stroke: var(--accent);
    stroke-width: 2.5;
  }
  :global(.svelte-flow__edge .floating-edge.is-capture) {
    stroke: var(--state-starting);
    stroke-width: 2.5;
    filter: drop-shadow(0 0 5px color-mix(in oklab, var(--state-starting) 60%, transparent));
  }

  /* R2.3 — link hover glow. `is-hot` is set when the edge OR either chip is
     hovered. The whole cable glows in the accent and thickens slightly. */
  :global(.svelte-flow__edge:hover .floating-edge),
  :global(.svelte-flow__edge .floating-edge.is-hot) {
    stroke: var(--accent);
    stroke-width: 3.25;
    filter: drop-shadow(0 0 6px color-mix(in oklab, var(--accent) 75%, transparent));
  }
  /* Capture-active links already glow amber; intensify on hover. */
  :global(.svelte-flow__edge:hover .floating-edge.is-capture),
  :global(.svelte-flow__edge .floating-edge.is-capture.is-hot) {
    stroke: var(--state-starting);
    filter: drop-shadow(0 0 8px color-mix(in oklab, var(--state-starting) 85%, transparent));
  }
  /* Widen the invisible interaction stroke so the cable is easy to hover. */
  :global(.svelte-flow__edge .svelte-flow__edge-interaction) {
    stroke-width: 18;
  }

  @media (prefers-reduced-motion: reduce) {
    /* glow only, no width animation */
    :global(.svelte-flow__edge .floating-edge) {
      transition: stroke 120ms ease, filter 120ms ease;
    }
    :global(.svelte-flow__edge:hover .floating-edge),
    :global(.svelte-flow__edge .floating-edge.is-hot) {
      stroke-width: 2;
    }
  }

  /* The EdgeLabel wrapper handles positioning; let the inner chip catch hovers. */
  :global(.svelte-flow__edge-label.port-chip-slot) {
    background: transparent;
    padding: 0;
    overflow: visible;
  }

  /* Hover-pop port chips (PNetLab feel). */
  .port-chip {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: var(--fs-chip);
    line-height: 1;
    color: var(--ink-2);
    background: var(--chip-bg);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 3px 6px;
    white-space: nowrap;
    cursor: default;
    transform: scale(1);
    transition: transform 140ms cubic-bezier(0.2, 0.9, 0.3, 1.2), color 140ms ease,
      background 140ms ease, box-shadow 140ms ease, border-color 140ms ease;
  }
  .chip-detail {
    display: none;
    color: var(--ink-3);
  }
  .chip-sep {
    display: none;
    margin: 0 4px;
    color: var(--ink-3);
  }
  .port-chip:hover {
    transform: scale(1.65);
    color: var(--ink);
    background: var(--tooltip-bg);
    border-color: var(--accent);
    box-shadow: var(--shadow-md);
    position: relative;
    z-index: 20;
  }
  .port-chip:hover .chip-detail,
  .port-chip:hover .chip-sep {
    display: inline;
  }
  /* When the edge (or its sibling chip) is hovered, signal both chips. */
  :global(.svelte-flow__edge:hover) .port-chip,
  .port-chip.chip-hot {
    color: var(--ink);
    border-color: var(--accent);
  }

  @media (prefers-reduced-motion: reduce) {
    .port-chip {
      transition: opacity 140ms ease, color 140ms ease, background 140ms ease,
        border-color 140ms ease;
    }
    .port-chip:hover {
      transform: scale(1.18);
    }
  }
</style>
