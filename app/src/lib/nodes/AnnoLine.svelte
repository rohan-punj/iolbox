<script module lang="ts">
  // Padding around the bbox so stroke + grips aren't clipped; MUST match the PAD
  // CanvasInner uses when sizing/positioning the node.
  export const LINE_PAD = 10;
</script>

<script lang="ts">
  // Line annotation (item 8). The flow node spans the endpoints' bounding box
  // (positioned by CanvasInner at the box top-left, sized to the box + PAD). The
  // SVG line + two endpoint grips render in box-local coordinates. Dragging a
  // grip moves just that endpoint (writes x1/y1 or x2/y2 absolute in the doc);
  // dragging the node body moves the whole line (handled in CanvasInner's
  // drag-stop by translating both endpoints). Double-click opens the style
  // popover (color, width). Delete key handled globally like other annotations.
  import { type NodeProps } from "@xyflow/svelte";
  import { labStore } from "../labStore.svelte";
  import { annoTool } from "../annoTool.svelte";

  let { id, data, selected }: NodeProps = $props();

  const annoId = $derived((data as any).annoId as string);
  const x1 = $derived((data as any).x1 as number);
  const y1 = $derived((data as any).y1 as number);
  const x2 = $derived((data as any).x2 as number);
  const y2 = $derived((data as any).y2 as number);
  const color = $derived(((data as any).color as string | undefined) ?? "var(--accent)");
  const width = $derived(((data as any).width as number | undefined) ?? 2.5);
  // Item 7 — arrowheads + dashed style. arrow: none | one (at x2/y2) | both.
  const arrow = $derived(((data as any).arrow as "none" | "one" | "both" | undefined) ?? "none");
  const dash = $derived(((data as any).dash as boolean | undefined) ?? false);
  // Unique marker ids per line so multiple arrowed lines don't collide.
  const markerId = $derived(`anno-arrow-${annoId}`);
  // Marker sized in userSpace so it scales with stroke width; dash pattern also
  // scales so thick lines get proportional dashes.
  const markerSize = $derived(3 + width * 1.6);
  const dashArray = $derived(dash ? `${width * 2.4} ${width * 2}` : undefined);

  // Box-local origin = min corner minus pad.
  const ox = $derived(Math.min(x1, x2) - LINE_PAD);
  const oy = $derived(Math.min(y1, y2) - LINE_PAD);
  const boxW = $derived(Math.abs(x2 - x1) + LINE_PAD * 2);
  const boxH = $derived(Math.abs(y2 - y1) + LINE_PAD * 2);

  // Live endpoint override during a grip drag (doc written on release).
  let dragEnd = $state<null | { which: 1 | 2 }>(null);
  let live = $state<{ x: number; y: number } | null>(null);

  const p1 = $derived(dragEnd?.which === 1 && live ? live : { x: x1, y: y1 });
  const p2 = $derived(dragEnd?.which === 2 && live ? live : { x: x2, y: y2 });

  function onDblClick(e: MouseEvent) {
    e.stopPropagation();
    annoTool.requestStyle?.(annoId, e.clientX, e.clientY, false);
  }

  function startGrip(which: 1 | 2, e: PointerEvent) {
    e.preventDefault();
    e.stopPropagation();
    dragEnd = { which };
    live = which === 1 ? { x: x1, y: y1 } : { x: x2, y: y2 };
    window.addEventListener("pointermove", onGripMove);
    window.addEventListener("pointerup", onGripUp);
  }
  function onGripMove(e: PointerEvent) {
    if (!dragEnd) return;
    // Convert the client point to flow coords via the shared helper.
    const fp = labStore.screenToFlow?.(e.clientX, e.clientY);
    if (fp) live = { x: fp.x, y: fp.y };
  }
  function onGripUp() {
    window.removeEventListener("pointermove", onGripMove);
    window.removeEventListener("pointerup", onGripUp);
    if (dragEnd && live) {
      const patch =
        dragEnd.which === 1
          ? { x1: Math.round(live.x), y1: Math.round(live.y) }
          : { x2: Math.round(live.x), y2: Math.round(live.y) };
      labStore.updateAnnotation(annoId, patch as any);
    }
    dragEnd = null;
    live = null;
  }
</script>

<div
  class="anno-line"
  class:selected
  style:width={`${boxW}px`}
  style:height={`${boxH}px`}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  <svg width={boxW} height={boxH} viewBox={`0 0 ${boxW} ${boxH}`}>
    {#if arrow !== "none"}
      <!-- Item 7 — arrowhead marker, sized in userSpace so it tracks stroke
           width, filled with the line colour. orient=auto rotates to the line. -->
      <defs>
        <marker
          id={markerId}
          markerUnits="userSpaceOnUse"
          markerWidth={markerSize}
          markerHeight={markerSize}
          refX={markerSize * 0.85}
          refY={markerSize / 2}
          orient="auto"
        >
          <path d={`M0,0 L${markerSize},${markerSize / 2} L0,${markerSize} z`} fill={color} />
        </marker>
        <marker
          id={`${markerId}-start`}
          markerUnits="userSpaceOnUse"
          markerWidth={markerSize}
          markerHeight={markerSize}
          refX={markerSize * 0.15}
          refY={markerSize / 2}
          orient="auto-start-reverse"
        >
          <path d={`M0,0 L${markerSize},${markerSize / 2} L0,${markerSize} z`} fill={color} />
        </marker>
      </defs>
    {/if}
    <!-- Wide transparent hit-line so the thin cable is easy to grab/hover. -->
    <line
      class="hit"
      x1={p1.x - ox}
      y1={p1.y - oy}
      x2={p2.x - ox}
      y2={p2.y - oy}
    />
    <line
      class="visible"
      x1={p1.x - ox}
      y1={p1.y - oy}
      x2={p2.x - ox}
      y2={p2.y - oy}
      stroke={color}
      stroke-width={width}
      stroke-dasharray={dashArray}
      marker-end={arrow === "one" || arrow === "both" ? `url(#${markerId})` : undefined}
      marker-start={arrow === "both" ? `url(#${markerId}-start)` : undefined}
    />
  </svg>
  {#if selected}
    <button
      class="grip nodrag"
      style:left={`${p1.x - ox}px`}
      style:top={`${p1.y - oy}px`}
      aria-label="Move line start"
      onpointerdown={(e) => startGrip(1, e)}
    ></button>
    <button
      class="grip nodrag"
      style:left={`${p2.x - ox}px`}
      style:top={`${p2.y - oy}px`}
      aria-label="Move line end"
      onpointerdown={(e) => startGrip(2, e)}
    ></button>
  {/if}
</div>

<style>
  .anno-line {
    position: relative;
  }
  .anno-line svg {
    position: absolute;
    inset: 0;
    display: block;
    overflow: visible;
  }
  .hit {
    stroke: transparent;
    stroke-width: 14;
    stroke-linecap: round;
  }
  .visible {
    stroke-linecap: round;
  }
  .anno-line.selected .visible {
    filter: drop-shadow(0 0 3px color-mix(in oklab, var(--accent) 60%, transparent));
  }
  .grip {
    all: unset;
    position: absolute;
    width: 12px;
    height: 12px;
    transform: translate(-50%, -50%);
    border-radius: 50%;
    background: var(--accent);
    border: 2px solid var(--ground);
    box-shadow: var(--shadow-sm);
    cursor: grab;
    z-index: 2;
  }
  .grip:active {
    cursor: grabbing;
  }
</style>
