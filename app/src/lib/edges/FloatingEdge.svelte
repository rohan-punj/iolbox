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
    useInternalNode,
    type EdgeProps,
  } from "@xyflow/svelte";
  import { getEdgeParams } from "./floating";
  import { labStore } from "../labStore.svelte";

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
    (data as {
      linkId?: number;
      source?: EndpointInfo;
      target?: EndpointInfo;
      capture?: boolean;
      parallelIndex?: number;
      parallelCount?: number;
    } | undefined) ?? {}
  );

  // Feature 2 — traffic-driven glow. Read this link's most recent throughput
  // sample; treat it as live only when it arrived within STALE_MS (link.stats
  // fires at most every 2s and only for links that forwarded traffic, so idle
  // links naturally decay to no-glow once the clock ticks past the window).
  const STALE_MS = 5000;
  const traffic = $derived.by(() => {
    const linkId = info.linkId;
    if (linkId === undefined) return null;
    const s = labStore.linkStats[linkId];
    if (!s) return null;
    const age = labStore.nowTick - s.ts;
    if (age > STALE_MS) return null;
    return s;
  });
  const glowing = $derived(traffic !== null);
  // Intensity 0..1 scaled by log(fps), clamped. ~1 fps → dim, ~1000 fps → full.
  const glowIntensity = $derived.by(() => {
    if (!traffic) return 0;
    const v = Math.log10(Math.max(traffic.fps, 1)) / 3; // log10(1000)=3
    return Math.min(1, Math.max(0.15, v));
  });

  // R2.3 — hovering either chip glows the whole cable (same as hovering the
  // edge). `hot` mirrors the edge path's hover state to the chips and vice-versa.
  let hot = $state(false);

  // R2.x — PNetLab-style parallel-link fan-out. Each edge in a group of parallel
  // links (same unordered node pair) curves out symmetrically. The signed offset
  // is (index - (count-1)/2) * spacing: a lone link → 0 (straight), two links →
  // ±spacing/2, three → -spacing/0/+spacing, etc. Sign is anchored to the
  // source→target vector so A↔B and B↔A parallels don't collapse onto each other.
  const PARALLEL_SPACING = 26;
  const parallelOffset = $derived.by(() => {
    const idx = info.parallelIndex ?? 0;
    const count = info.parallelCount ?? 1;
    if (count <= 1) return 0;
    return (idx - (count - 1) / 2) * PARALLEL_SPACING;
  });

  // Quadratic-bezier path + label anchor points, recomputed whenever either node
  // moves. The control point sits at the segment midpoint pushed perpendicular to
  // the source→target line by parallelOffset, so parallel links bow apart. Chips
  // ride the curve (evaluated near each endpoint) so they fan out with the cable.
  const geom = $derived.by(() => {
    const s = sourceNode.current;
    const t = targetNode.current;
    if (!s || !t) return null;
    const { sx, sy, tx, ty } = getEdgeParams(s, t);

    const dx = tx - sx;
    const dy = ty - sy;
    const len = Math.hypot(dx, dy) || 1;
    // Unit perpendicular to the endpoint line.
    const px = -dy / len;
    const py = dx / len;
    // Control point: midpoint pushed out. Doubling the offset makes the curve
    // *pass* near ±offset at its apex (a quadratic reaches half its control
    // displacement at t=0.5), matching the intended fan spacing.
    const off = parallelOffset;
    const cx = (sx + tx) / 2 + px * off * 2;
    const cy = (sy + ty) / 2 + py * off * 2;

    const path = `M ${sx} ${sy} Q ${cx} ${cy} ${tx} ${ty}`;

    // Evaluate the quadratic B(u) so each chip follows ITS end of the curve.
    const at = (u: number) => {
      const m = 1 - u;
      return {
        x: m * m * sx + 2 * m * u * cx + u * u * tx,
        y: m * m * sy + 2 * m * u * cy + u * u * ty,
      };
    };
    return {
      path,
      sChip: at(0.16),
      tChip: at(0.84),
      // origin inside each chip pointing back at its own node
      sOrigin: dx >= 0 ? "left" : "right",
      tOrigin: dx >= 0 ? "right" : "left",
    };
  });
</script>

{#if geom}
  {#if glowing}
    <!-- Traffic glow: a wide, soft accent underlay beneath the cable. Opacity +
         width scale with log(fps) so a busy link reads hotter. -->
    <path
      class="traffic-glow"
      d={geom.path}
      style={`stroke-width:${6 + glowIntensity * 10}px;opacity:${0.18 + glowIntensity * 0.4}`}
    />
  {/if}
  <BaseEdge
    {id}
    path={geom.path}
    class={"floating-edge" +
      (selected ? " is-selected" : "") +
      (info.capture ? " is-capture" : "") +
      (glowing ? " is-traffic" : "") +
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
        <span class="chip-detail">{info.source.name}</span><span class="chip-sep">&nbsp;</span>{info.source.iface}{#if glowing && traffic}<span class="chip-fps"> · {traffic.fps.toFixed(1)} fps</span>{/if}
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
        <span class="chip-detail">{info.target.name}</span><span class="chip-sep">&nbsp;</span>{info.target.iface}
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

  /* Feature 2 — traffic glow underlay. A soft, wide accent halo drawn beneath
     the cable; width/opacity are set inline from log(fps). Non-interactive so
     it never steals the cable's hover. */
  .traffic-glow {
    fill: none;
    stroke: var(--accent);
    stroke-linecap: round;
    pointer-events: none;
    filter: blur(2px);
    transition: opacity 400ms ease, stroke-width 400ms ease;
  }
  :global(.svelte-flow__edge .floating-edge.is-traffic) {
    stroke: color-mix(in oklab, var(--accent) 80%, var(--cable));
  }
  .chip-fps {
    color: var(--accent);
    font-variant-numeric: tabular-nums;
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
    /* Readability — chip labels were small/dim. Up ~1px from --fs-chip and use
       the primary ink for contrast against a more-opaque chip background. */
    font-size: calc(var(--fs-chip) + 1px);
    line-height: 1;
    color: var(--ink);
    background: color-mix(in oklab, var(--chip-bg) 92%, var(--ground));
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
  .chip-detail,
  .chip-sep {
    display: none;
    color: var(--ink-2);
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
