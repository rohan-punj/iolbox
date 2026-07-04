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
  import { watcherStore, LABELS } from "../watcherStore.svelte";
  import { painterStore } from "../painterStore.svelte";

  let { id, source, target, selected, data }: EdgeProps = $props();

  // An edge's endpoints are stable for its lifetime — xyflow remounts the edge
  // component if source/target change (the id key changes with them). The hooks
  // are internally reactive over the node lookup, so reading the initial id here
  // is correct.
  // svelte-ignore state_referenced_locally
  const sourceNode = useInternalNode(source);
  // svelte-ignore state_referenced_locally
  const targetNode = useInternalNode(target);

  type EndpointInfo = { nodeId?: number; name: string; iface: string; telnet?: number };
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

  // Network Watcher — directional animated protocol overlays (PNetLab-style).
  // Gated on the watcher actually running AND a FRESH traffic sample (same
  // staleness window as the glow): a link that went quiet stops animating
  // within ~5s without any explicit clear. Direction mapping: CanvasInner
  // builds edges as source = link.endpoints[0].node / target =
  // endpoints[1].node, and the path below runs source→target — so protosDir's
  // [0] slot (frames sourced from endpoints[0]) animates ALONG the path and
  // the [1] slot animates in reverse.
  const watcherMatches = $derived.by(() => {
    if (!watcherStore.running || !traffic) return [];
    return watcherStore.matchFor(traffic);
  });
  // Intensity 0..1 scaled by log(fps), clamped. ~1 fps → dim, ~1000 fps → full.
  const glowIntensity = $derived.by(() => {
    if (!traffic) return 0;
    const v = Math.log10(Math.max(traffic.fps, 1)) / 3; // log10(1000)=3
    return Math.min(1, Math.max(0.15, v));
  });

  // R2.3 — hovering either chip glows the whole cable (same as hovering the
  // edge). `hot` mirrors the edge path's hover state to the chips and vice-versa.
  let hot = $state(false);

  // WS5b — Topology Painter overlays. A snapshot (painterStore.result) drives
  // per-endpoint STP badges (role/state) at the two link ends, red-dashed
  // rendering for blocked links, and a routing best-path highlight/dim. All of
  // this is gated on a snapshot being present; clearing it removes everything.
  const stpSourceBadge = $derived.by(() => {
    const src = info.source;
    if (src?.nodeId === undefined) return null;
    return painterStore.stpBadgeFor(src.nodeId, src.iface);
  });
  const stpTargetBadge = $derived.by(() => {
    const tgt = info.target;
    if (tgt?.nodeId === undefined) return null;
    return painterStore.stpBadgeFor(tgt.nodeId, tgt.iface);
  });
  // A link renders BLOCKED (red dashed) when EITHER end's STP port is blocked.
  const stpBlocked = $derived(
    (stpSourceBadge?.blocked ?? false) || (stpTargetBadge?.blocked ?? false)
  );
  // A link is still CONVERGING (amber dashed) when either end is LRN/LIS and
  // neither end is (yet) blocked — blocked takes visual priority since it's
  // the more actionable state (click for why).
  const stpTransitional = $derived(
    !stpBlocked &&
      ((stpSourceBadge?.transitional ?? false) || (stpTargetBadge?.transitional ?? false))
  );
  // The blocking reason to show in the popover (source end preferred).
  const stpBlockReason = $derived(
    stpSourceBadge?.blocked ? stpSourceBadge.reason
      : stpTargetBadge?.blocked ? stpTargetBadge.reason
      : undefined
  );

  // Routing best-path paint for THIS link, if any. When a routing snapshot is
  // up, non-winning links dim; winning links glow + carry a metric label.
  const routingPaint = $derived.by(() => {
    const linkId = info.linkId;
    if (linkId === undefined) return null;
    return painterStore.routingPaintFor(linkId);
  });
  const routingActive = $derived(painterStore.isRouting);
  const routingWinner = $derived(routingPaint !== null);
  // Dim a link only while a routing paint is displayed and this link lost.
  const routingDimmed = $derived(routingActive && !routingWinner);

  // Blocked-port reason popover open state (clicking a blocked-port badge).
  let reasonOpen = $state(false);

  // R2.x — PNetLab-style parallel-link fan-out. Each edge in a group of parallel
  // links (same unordered node pair) curves out symmetrically. The signed offset
  // is (index - (count-1)/2) * spacing: a lone link → 0 (straight), two links →
  // ±spacing/2, three → -spacing/0/+spacing, etc. Sign is anchored to the
  // source→target vector so A↔B and B↔A parallels don't collapse onto each other.
  const PARALLEL_SPACING = 26;
  // Item 1 — endpoint fan spacing. Parallel edges no longer share one anchor
  // point at the node border (which pinched them to a wedge); each edge's start
  // AND end anchor slides along the node border, perpendicular to the link axis,
  // by fanSign * ENDPOINT_SPACING, clamped to the node radius so anchors stay on
  // the node face. This makes parallel cables arrive visibly separated.
  const ENDPOINT_SPACING = 10;
  const parallelSign = $derived.by(() => {
    const idx = info.parallelIndex ?? 0;
    const count = info.parallelCount ?? 1;
    if (count <= 1) return 0;
    return idx - (count - 1) / 2;
  });
  const parallelOffset = $derived(parallelSign * PARALLEL_SPACING);

  // Quadratic-bezier path + label anchor points, recomputed whenever either node
  // moves. The control point sits at the segment midpoint pushed perpendicular to
  // the source→target line by parallelOffset, so parallel links bow apart. Chips
  // ride the curve (evaluated near each endpoint) so they fan out with the cable.
  const geom = $derived.by(() => {
    const s = sourceNode.current;
    const t = targetNode.current;
    if (!s || !t) return null;
    const raw = getEdgeParams(s, t);

    const dx0 = raw.tx - raw.sx;
    const dy0 = raw.ty - raw.sy;
    const len0 = Math.hypot(dx0, dy0) || 1;
    // Unit perpendicular to the endpoint line.
    const px = -dy0 / len0;
    const py = dx0 / len0;

    // Item 1 — FAN THE ENDPOINTS. Slide each parallel edge's source AND target
    // anchor along the node border, perpendicular to the link axis, by
    // sign * ENDPOINT_SPACING. Clamp the slide to the node's half-extent so the
    // anchor stays on the node face (never past the corner). Single links keep
    // the exact intersection point (endShift 0) → unchanged.
    const nodeRadius = (s: typeof sourceNode.current) => {
      const w = s?.measured?.width ?? s?.width ?? 64;
      const h = s?.measured?.height ?? s?.height ?? 64;
      return Math.min(w, h) / 2;
    };
    const rad = Math.min(nodeRadius(s), nodeRadius(t));
    const endShift = Math.max(-rad, Math.min(rad, parallelSign * ENDPOINT_SPACING));
    const sx = raw.sx + px * endShift;
    const sy = raw.sy + py * endShift;
    const tx = raw.tx + px * endShift;
    const ty = raw.ty + py * endShift;

    const dx = tx - sx;
    const dy = ty - sy;

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

    // Item 2 — labels sit ON the link line. The chip anchor is placed EXACTLY on
    // this edge's own curve (perpendicular offset 0); the chip's own opaque
    // background masks the line passing behind it. Parallel chips no longer need
    // an extra perpendicular nudge to separate — each chip rides ITS OWN curve
    // (the endpoint fan + bow already put the curves at different places), so
    // on-curve placement separates them for free. A tiny per-index t-shift keeps
    // chips of adjacent parallels from landing at the same distance-along, so
    // they don't collide where curves happen to cross near the node.
    const count = info.parallelCount ?? 1;
    const single = count <= 1;
    const idx = info.parallelIndex ?? 0;
    // Base chip t at 0.22 / 0.78; nudge by a small signed per-index amount.
    const tShift = single ? 0 : (idx - (count - 1) / 2) * 0.03;
    const su = 0.22 + tShift;
    const tu = 0.78 + tShift;
    const sPt = at(su);
    const tPt = at(tu);
    // Watcher chip rides the curve midpoint (t=0.5), same on-curve idiom as the
    // port chips — its own curve already separates it from parallel siblings,
    // so no extra perpendicular nudge is needed here either.
    const wPt = at(0.5);

    // Watcher flow paths — the same quadratic shifted perpendicular by d px so
    // the flow rides BESIDE the cable instead of on top of it (on-path dashes
    // were unreadable over the stroke). Positive d = one side, negative = the
    // other, so the two directions of one link never overlap each other.
    // reversed=true swaps start/end: marching "forward" along the reversed
    // path (and animateMotion with rotate="auto") then reads target→source,
    // which is how the endpoints[1]-sourced direction is rendered — one
    // animation direction serves both.
    const offsetPath = (d: number, reversed: boolean) => {
      const osx = sx + px * d, osy = sy + py * d;
      const ocx = cx + px * d, ocy = cy + py * d;
      const otx = tx + px * d, oty = ty + py * d;
      return reversed
        ? `M ${otx} ${oty} Q ${ocx} ${ocy} ${osx} ${osy}`
        : `M ${osx} ${osy} Q ${ocx} ${ocy} ${otx} ${oty}`;
    };

    return {
      path,
      offsetPath,
      // Perpendicular offset 0 → chip centered over the path.
      sChip: { x: sPt.x, y: sPt.y },
      tChip: { x: tPt.x, y: tPt.y },
      watcherChip: { x: wPt.x, y: wPt.y },
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
      (hot ? " is-hot" : "") +
      (routingWinner ? " is-bestpath" : "") +
      (routingDimmed ? " is-dimmed" : "")}
  />

  <!-- WS5b — STP blocked link: a red dashed overlay tracing this edge's own
       curve. Rendered above the cable; non-interactive (the badge below owns
       the click for the reason popover). -->
  {#if stpBlocked}
    <path class="stp-blocked" d={geom.path} />
  {:else if stpTransitional}
    <!-- STP still converging (LRN/LIS): a distinct amber dashed overlay, so it
         reads as neither settled-forwarding (green glow) nor settled-blocked
         (red dashed) — the user should Re-paint once it settles. -->
    <path class="stp-converging" d={geom.path} />
  {/if}

  <!-- WS5b — routing best-path highlight: a bright accent underlay along the
       winning edge, plus a direction arrow toward the next-hop endpoint. -->
  {#if routingWinner}
    <path class="bestpath-glow" d={geom.path} />
    {#each [0, -0.9, -1.8] as begin (begin)}
      <polygon class="bestpath-arrow" points="-1,-4.5 9,0 -1,4.5">
        <animateMotion
          dur="2.2s"
          repeatCount="indefinite"
          rotate="auto"
          path={routingPaint?.toEndpoint === 0 ? geom.offsetPath(0, true) : geom.path}
          begin={`${begin}s`}
        />
      </polygon>
    {/each}
  {/if}
  <!-- R2 — a wide invisible hover-catcher over THIS edge's own curve. Hovering
       it sets `hot`, which (a) glows this cable and (b) raises this edge's
       labels above sibling parallel edges (.slot-hot z-index). Because `hot`
       is per-edge-instance, only the hovered edge's hover-label appears — a
       background sibling's chip can no longer render on top of it. -->
  <path
    class="edge-hover-catch"
    d={geom.path}
    onpointerenter={() => (hot = true)}
    onpointerleave={() => (hot = false)}
    role="presentation"
  />

  <!-- Network Watcher overlays: one dashed flow per matching row and direction,
       shifted PERPENDICULAR off the cable (offsetPath) so the flow is readable
       beside the link instead of blending into its stroke. endpoints[0]-sourced
       traffic rides one side (positive offset), endpoints[1]-sourced the other
       (negative, path REVERSED so the same forward march + rotate="auto"
       arrowheads read target→source). Additional rows stack further out.
       Direction is made explicit by small arrowhead triangles carried along the
       flow with SMIL animateMotion (rotate="auto" orients them to the curve
       tangent in the direction of travel); negative begin values spread the
       train so arrows are always visible mid-link. -->
  {#each watcherMatches as m, mi (m.row.id)}
    {@const mag = 8 + mi * 7}
    {#each [
      ...(m.dir0 ? [{ d: geom.offsetPath(mag, false), key: "fwd" }] : []),
      ...(m.dir1 ? [{ d: geom.offsetPath(-mag, true), key: "rev" }] : []),
    ] as flow (flow.key)}
      <path
        class="watcher-dash"
        d={flow.d}
        style={`stroke:${m.row.color};stroke-dasharray:10 8`}
      />
      {#each [0, -0.8, -1.6] as begin (begin)}
        <polygon class="watcher-arrow" points="-1,-4.5 9,0 -1,4.5" fill={m.row.color}>
          <animateMotion
            dur="2.4s"
            repeatCount="indefinite"
            rotate="auto"
            path={flow.d}
            begin={`${begin}s`}
          />
        </polygon>
      {/each}
    {/each}
  {/each}

  {#if info.source}
    <EdgeLabel x={geom.sChip.x} y={geom.sChip.y} class={"port-chip-slot" + (hot ? " slot-hot" : "")}>
      <span
        class="port-chip"
        class:chip-hot={hot}
        style={`transform-origin:${geom.sOrigin} center`}
        onpointerenter={() => (hot = true)}
        onpointerleave={() => (hot = false)}
        role="presentation"
      >
        <span class="chip-detail">{info.source.name}</span><span class="chip-sep">&nbsp;</span>{info.source.iface}
      </span>
      {#if stpSourceBadge}
        <button
          class="stp-badge"
          class:blocked={stpSourceBadge.blocked}
          class:transitional={stpSourceBadge.transitional}
          title={stpSourceBadge.blocked
            ? "Blocked port — click for why"
            : stpSourceBadge.transitional
            ? `${stpSourceBadge.role} · ${stpSourceBadge.state} — still converging, Re-paint to refresh`
            : `${stpSourceBadge.role} · ${stpSourceBadge.state}`}
          onclick={() => stpSourceBadge?.blocked && (reasonOpen = !reasonOpen)}
        >{stpSourceBadge.role} {stpSourceBadge.state}</button>
      {/if}
    </EdgeLabel>
  {/if}

  {#if info.target}
    <EdgeLabel x={geom.tChip.x} y={geom.tChip.y} class={"port-chip-slot" + (hot ? " slot-hot" : "")}>
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
      {#if stpTargetBadge}
        <button
          class="stp-badge"
          class:blocked={stpTargetBadge.blocked}
          class:transitional={stpTargetBadge.transitional}
          title={stpTargetBadge.blocked
            ? "Blocked port — click for why"
            : stpTargetBadge.transitional
            ? `${stpTargetBadge.role} · ${stpTargetBadge.state} — still converging, Re-paint to refresh`
            : `${stpTargetBadge.role} · ${stpTargetBadge.state}`}
          onclick={() => stpTargetBadge?.blocked && (reasonOpen = !reasonOpen)}
        >{stpTargetBadge.role} {stpTargetBadge.state}</button>
      {/if}
    </EdgeLabel>
  {/if}

  <!-- WS5b — blocked-port reason popover, anchored at the link midpoint. Opens
       when the user clicks a blocked STP badge; shows the parsed reason string
       verbatim. Click the ✕ or the badge again to close. -->
  {#if stpBlocked && reasonOpen && stpBlockReason}
    <EdgeLabel x={geom.watcherChip.x} y={geom.watcherChip.y} class="stp-reason-slot">
      <div class="stp-reason" role="dialog" aria-label="Blocked port reason">
        <button class="stp-reason-close" aria-label="Close" onclick={() => (reasonOpen = false)}>✕</button>
        <div class="stp-reason-title">Why is this port blocked?</div>
        <div class="stp-reason-body">{stpBlockReason}</div>
      </div>
    </EdgeLabel>
  {/if}

  <!-- WS5b — routing best-path metric pill at the link midpoint (cost / FD /
       AS-path), tinted to the best-path accent. -->
  {#if routingWinner && routingPaint}
    <EdgeLabel x={geom.watcherChip.x} y={geom.watcherChip.y} class="bestpath-pill-slot">
      <span class="bestpath-pill">{routingPaint.label}</span>
    </EdgeLabel>
  {/if}

  <!-- Watcher label pills: one per matching row at the curve midpoint, stacked
       with small vertical offsets so multiple rows on one link stay legible.
       Border/text take the row colour so a pill visually pairs with its dashes. -->
  {#each watcherMatches as m, i (m.row.id)}
    <EdgeLabel x={geom.watcherChip.x} y={geom.watcherChip.y + i * 18} class="watcher-pill-slot">
      <span class="watcher-pill" style={`border-color:${m.row.color};color:${m.row.color}`}>
        {LABELS[m.row.proto].name}
      </span>
    </EdgeLabel>
  {/each}
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
  /* R2 — per-edge hover-catcher. Transparent, wide, follows this edge's OWN
     curve so hovering a background parallel link raises exactly that link (not
     the topmost one). */
  .edge-hover-catch {
    fill: none;
    stroke: transparent;
    stroke-width: 18;
    stroke-linecap: round;
    cursor: pointer;
    pointer-events: stroke;
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
  /* Network Watcher — dashed directional flows, offset beside the cable
     (offsetPath). stroke colour comes inline per row. One march animation
     serves both directions: the reverse direction's PATH is reversed, so a
     decreasing dashoffset always moves dashes in the direction of travel. The
     loop covers exactly two 18px dash periods (36px) so it's seamless. */
  .watcher-dash {
    fill: none;
    stroke-width: 2.5;
    stroke-linecap: round;
    opacity: 0.85;
    pointer-events: none;
    animation: watcher-march 0.9s linear infinite;
  }
  @keyframes watcher-march {
    to {
      stroke-dashoffset: -36;
    }
  }
  /* Direction arrowheads carried by SMIL animateMotion along the flow path. */
  .watcher-arrow {
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .watcher-dash {
      animation: none;
    }
    /* SMIL ignores the media query, so hide the moving arrows entirely —
       the static offset dashes still mark which links match. */
    .watcher-arrow {
      display: none;
    }
  }

  /* Watcher label pill — midpoint marker naming the matched filter, tinted to
     the row colour over a translucent theme-ground fill. Non-interactive. */
  :global(.svelte-flow__edge-label.watcher-pill-slot) {
    background: transparent;
    padding: 0;
    overflow: visible;
    pointer-events: none;
  }
  .watcher-pill {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    line-height: 1;
    border: 1px solid;
    border-radius: 999px;
    padding: 3px 7px;
    white-space: nowrap;
    pointer-events: none;
    background: color-mix(in oklab, var(--ground) 82%, transparent);
  }
  /* R2 — when this edge is hovered, raise BOTH its chips above every sibling
     edge's chips so an overlapping background link can't cover the hovered
     label. EdgeLabel writes an INLINE `z-index:0`, so !important is required to
     win over it. Well above the resting slot z-index and the hover-pop (20). */
  :global(.svelte-flow__edge-label.port-chip-slot.slot-hot) {
    z-index: 40 !important;
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
    /* Item 2 — the chip sits ON the link line, so its background must be OPAQUE
       to mask the cable passing behind it (a translucent chip let the stroke
       bleed through the label). Paint the opaque --ground first, then the
       (translucent) chip tint on top → the composite is fully opaque, no blur. */
    background: linear-gradient(color-mix(in oklab, var(--chip-bg) 100%, transparent), color-mix(in oklab, var(--chip-bg) 100%, transparent)), var(--ground);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    /* R1 — more compact so parallel-link chips need less room to separate. */
    padding: 2px 5px;
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

  /* ── WS5b Topology Painter ─────────────────────────────────────────────── */

  /* Blocked STP link: a red dashed overlay along the cable curve. */
  .stp-blocked {
    fill: none;
    stroke: #e5484d;
    stroke-width: 2.5;
    stroke-dasharray: 7 6;
    stroke-linecap: round;
    pointer-events: none;
    filter: drop-shadow(0 0 4px color-mix(in oklab, #e5484d 55%, transparent));
  }

  /* STP still-converging link (LRN/LIS): amber dashed, visually distinct from
     both the red-dashed BLK and the plain green-glow FWD cable — a cue to
     Re-paint once the tree settles. */
  .stp-converging {
    fill: none;
    stroke: #f5a623;
    stroke-width: 2.5;
    stroke-dasharray: 3 5;
    stroke-linecap: round;
    pointer-events: none;
    filter: drop-shadow(0 0 4px color-mix(in oklab, #f5a623 55%, transparent));
    animation: stp-converging-march 0.9s linear infinite;
  }
  @keyframes stp-converging-march {
    to {
      stroke-dashoffset: -16;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .stp-converging {
      animation: none;
    }
  }

  /* Routing best-path: a bright accent underlay + moving direction arrows. */
  :global(.svelte-flow__edge .floating-edge.is-bestpath) {
    stroke: var(--accent);
    stroke-width: 3;
  }
  /* Losing links dim while a routing snapshot is displayed. */
  :global(.svelte-flow__edge .floating-edge.is-dimmed) {
    stroke: var(--cable);
    opacity: 0.28;
  }
  .bestpath-glow {
    fill: none;
    stroke: var(--accent);
    stroke-width: 9;
    stroke-linecap: round;
    opacity: 0.22;
    pointer-events: none;
    filter: blur(2px);
  }
  .bestpath-arrow {
    fill: var(--accent);
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .bestpath-arrow {
      display: none;
    }
  }

  /* STP per-port badge: a compact role+state chip stacked under the port chip.
     Blocked ports go red and are clickable (open the reason popover). */
  .stp-badge {
    all: unset;
    box-sizing: border-box;
    display: block;
    margin-top: 2px;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
    letter-spacing: 0.02em;
    text-align: center;
    color: var(--ink);
    background: color-mix(in oklab, var(--accent) 22%, var(--ground));
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: 2px 4px;
    white-space: nowrap;
    cursor: default;
  }
  .stp-badge.blocked {
    color: #fff;
    background: color-mix(in oklab, #e5484d 82%, var(--ground));
    border-color: #e5484d;
    cursor: pointer;
  }
  .stp-badge.blocked:hover {
    background: #e5484d;
  }
  /* STP still-converging (LRN/LIS) badge: amber, distinct from both the
     accent-tinted settled FWD badge and the red BLK badge. Not clickable —
     there's no "reason" to show, just Re-paint. */
  .stp-badge.transitional {
    color: #1a1200;
    background: color-mix(in oklab, #f5a623 78%, var(--ground));
    border-color: #f5a623;
  }

  /* Blocked-port reason popover. */
  :global(.svelte-flow__edge-label.stp-reason-slot) {
    background: transparent;
    padding: 0;
    overflow: visible;
    z-index: 50 !important;
  }
  .stp-reason {
    position: relative;
    width: 240px;
    background: var(--tooltip-bg, var(--bg-2));
    border: 1px solid #e5484d;
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
    padding: 10px 12px;
    text-align: left;
  }
  .stp-reason-close {
    all: unset;
    box-sizing: border-box;
    position: absolute;
    top: 6px;
    right: 8px;
    cursor: pointer;
    color: var(--ink-3);
    font-size: 11px;
    line-height: 1;
    padding: 2px 4px;
    border-radius: var(--radius-sm);
  }
  .stp-reason-close:hover {
    color: var(--ink);
    background: var(--bg-hover);
  }
  .stp-reason-title {
    font-size: var(--fs-xs);
    font-weight: 700;
    color: #e5484d;
    margin-bottom: 4px;
    padding-right: 16px;
  }
  .stp-reason-body {
    font-size: var(--fs-xs);
    line-height: 1.4;
    color: var(--ink);
  }

  /* Routing metric pill (cost / FD / AS-path). */
  :global(.svelte-flow__edge-label.bestpath-pill-slot) {
    background: transparent;
    padding: 0;
    overflow: visible;
    pointer-events: none;
  }
  .bestpath-pill {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
    color: var(--accent);
    border: 1px solid var(--accent);
    border-radius: 999px;
    padding: 3px 7px;
    white-space: nowrap;
    background: color-mix(in oklab, var(--ground) 88%, transparent);
  }
</style>
