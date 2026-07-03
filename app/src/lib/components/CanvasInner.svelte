<script lang="ts">
  import {
    SvelteFlow,
    Background,
    BackgroundVariant,
    ConnectionMode,
    useSvelteFlow,
    Position,
    type Node,
    type Edge,
    type Connection,
    type NodeTypes,
    type EdgeTypes,
  } from "@xyflow/svelte";
  import "@xyflow/svelte/dist/style.css";
  import { onMount, untrack } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { themeStore } from "../themeStore.svelte";
  import IolNode from "../nodes/IolNode.svelte";
  import VpcsNode from "../nodes/VpcsNode.svelte";
  import AnnoText from "../nodes/AnnoText.svelte";
  import AnnoShape from "../nodes/AnnoShape.svelte";
  import AnnoLine, { LINE_PAD } from "../nodes/AnnoLine.svelte";
  import FloatingEdge from "../edges/FloatingEdge.svelte";
  import AnnoStylePopover from "./AnnoStylePopover.svelte";
  import ContextMenu, { type MenuItem } from "./ContextMenu.svelte";
  import ChangeImagePopover from "./ChangeImagePopover.svelte";
  import IconPicker from "./IconPicker.svelte";
  import InterfacePicker from "./InterfacePicker.svelte";
  import NodeEditDialog from "./NodeEditDialog.svelte";
  import { uiSvg } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import { nextFreeInterface } from "../interfaces";
  import { annoTool } from "../annoTool.svelte";
  import type { Annotation, LabNode } from "../labTypes";

  // NAT gateway + MGMT bridge reuse the VPCS single-interface node chrome; the
  // distinct glyph comes from their default icon (defaultIconFor). annoText /
  // annoShape are the Excalidraw-style annotation layer — rendered as flow nodes
  // (so drag/pan/zoom come free) but derived from lab.annotations, never lab.nodes.
  const nodeTypes: NodeTypes = {
    iol: IolNode,
    vpcs: VpcsNode,
    nat: VpcsNode,
    mgmt: VpcsNode,
    annoText: AnnoText,
    annoShape: AnnoShape,
    annoLine: AnnoLine,
  };
  const edgeTypes: EdgeTypes = { floating: FloatingEdge };

  // Fixed node box. Provided explicitly (width/height + a handle on every side)
  // so xyflow can resolve edge endpoints deterministically without waiting on
  // async measurement — the floating-edge geometry is then computed from these
  // in getEdgeParams. Sidesteps the "measured is empty → all edges dropped"
  // failure mode with custom nodes.
  const NODE_W = 64;
  const NODE_H = 88;
  const NODE_HANDLES = [
    { id: "top", type: "source" as const, position: Position.Top, x: NODE_W / 2, y: 0 },
    { id: "right", type: "source" as const, position: Position.Right, x: NODE_W, y: NODE_H / 2 },
    { id: "bottom", type: "source" as const, position: Position.Bottom, x: NODE_W / 2, y: NODE_H },
    { id: "left", type: "source" as const, position: Position.Left, x: 0, y: NODE_H / 2 },
  ];

  function toFlowNode(n: LabNode): Node {
    const img = labStore.images.find((i) => i.id === n.image?.id);
    return {
      id: String(n.id),
      type: n.kind,
      position: { x: n.x, y: n.y },
      width: NODE_W,
      height: NODE_H,
      handles: NODE_HANDLES,
      data: {
        label: n.name,
        icon: n.icon,
        imageClass: img?.class ?? n.image?.class ?? "unknown",
        imageLabel: img?.filename ?? n.image?.filename,
      },
      selected: labStore.selectedNodeId === n.id,
    };
  }

  // Annotation flow-node id namespace. Device nodes use String(numericId); the
  // "anno-" prefix keeps the two disjoint so drag-stop / delete can tell them
  // apart (isAnnoId) and never collide with a numeric node id.
  const ANNO_PREFIX = "anno-";
  const isAnnoId = (fid: string) => fid.startsWith(ANNO_PREFIX);
  const annoIdFromFlow = (fid: string) => fid.slice(ANNO_PREFIX.length);

  // Derive one Svelte Flow node from an annotation. Shapes carry explicit
  // width/height (drag-movable box); text is auto-sized. Both sit BELOW device
  // nodes (low zIndex), take no handles, and are selectable but not
  // connectable — they are pure canvas decoration.
  function toAnnoFlowNode(a: Annotation): Node {
    // Lines carry their own bbox-derived position; text/shapes use a.x/a.y.
    const pos =
      a.type === "line"
        ? { x: Math.min(a.x1, a.x2) - LINE_PAD, y: Math.min(a.y1, a.y2) - LINE_PAD }
        : { x: a.x, y: a.y };
    const common = {
      id: ANNO_PREFIX + a.id,
      position: pos,
      selectable: true,
      connectable: false,
      deletable: true,
      draggable: true,
      zIndex: -1,
      selected: labStore.selectedAnnotationId === a.id,
    };
    if (a.type === "text") {
      return {
        ...common,
        type: "annoText",
        data: { annoId: a.id, text: a.text, size: a.size, color: a.color, font: a.font, fill: a.fill },
      };
    }
    if (a.type === "line") {
      return {
        ...common,
        type: "annoLine",
        width: Math.abs(a.x2 - a.x1) + LINE_PAD * 2,
        height: Math.abs(a.y2 - a.y1) + LINE_PAD * 2,
        data: { annoId: a.id, x1: a.x1, y1: a.y1, x2: a.x2, y2: a.y2, color: a.color, width: a.width, arrow: a.arrow, dash: a.dash },
      };
    }
    return {
      ...common,
      type: "annoShape",
      width: a.w,
      height: a.h,
      data: {
        annoId: a.id,
        shape: a.type,
        label: a.label,
        color: a.color,
        border: a.border,
        fillOpacity: a.fillOpacity,
      },
    };
  }

  function endpointInfo(nodeId: number, iface: string) {
    const node = labStore.lab.nodes.find((n) => n.id === nodeId);
    const running = labStore.nodeStates[nodeId] === "running";
    return {
      name: node?.name ?? `#${nodeId}`,
      iface,
      telnet: running ? labStore.consolePorts[nodeId] : undefined,
    };
  }

  function toFlowEdge(
    l: (typeof labStore.lab.links)[number],
    parallelIndex: number,
    parallelCount: number
  ): Edge {
    const [a, b] = l.endpoints;
    return {
      id: `link-${l.id}`,
      type: "floating",
      source: String(a?.node ?? 0),
      target: String(b?.node ?? 0),
      selected: labStore.selectedLinkId === l.id,
      data: {
        linkId: l.id,
        capture: l.capture?.enabled ?? false,
        source: endpointInfo(a?.node ?? 0, a?.interface ?? ""),
        target: endpointInfo(b?.node ?? 0, b?.interface ?? ""),
        // R2.x — PNetLab-style parallel-link fan-out. Links sharing the same
        // unordered node pair each get an index within the group so FloatingEdge
        // can curve them out symmetrically (see buildEdges).
        parallelIndex,
        parallelCount,
      },
    };
  }

  // Group links by their unordered node-id pair so N parallel links between the
  // same two nodes fan out instead of stacking. Each link is tagged with its
  // index within the group and the group size; FloatingEdge derives a
  // perpendicular offset from these. The pair key sorts the two node ids so
  // A↔B and B↔A land in the same group; the source/target sign difference is
  // handled inside FloatingEdge (offset is signed by the source→target vector).
  function buildEdges(): Edge[] {
    const groups = new Map<string, number>();
    return labStore.lab.links.map((l) => {
      const [a, b] = l.endpoints;
      const na = a?.node ?? 0;
      const nb = b?.node ?? 0;
      const key = na < nb ? `${na}-${nb}` : `${nb}-${na}`;
      const idx = groups.get(key) ?? 0;
      groups.set(key, idx + 1);
      return { link: l, key, idx };
    }).map(({ link, key, idx }) => {
      const count = groups.get(key) ?? 1;
      return toFlowEdge(link, idx, count);
    });
  }

  let nodes = $state.raw<Node[]>([]);
  let edges = $state.raw<Edge[]>([]);

  // Item 6 — canvas pan behaviour. By default a plain left-drag on empty canvas
  // does NOT pan (panOnDrag=false); panning is Ctrl+left-drag (panActivationKey).
  // A plain drag then draws a selection box (selectionOnDrag) — desirable. The
  // "pan" toggle in the controls cluster flips panOnDrag on for mouse-only users;
  // Escape or clicking it again turns it back off.
  let panMode = $state(false);

  // Reconcile lab nodes into the flow-node array WITHOUT clobbering the fields
  // xyflow attaches during measurement (measured / internals / width / height /
  // handles). Rebuilding these from scratch on every store change would wipe the
  // measured dimensions and silently drop all edges (floating edges need
  // measured nodes). So we merge in place, preserving xyflow's managed fields.
  $effect(() => {
    const desired = [
      ...labStore.lab.nodes.map(toFlowNode),
      ...(labStore.lab.annotations ?? []).map(toAnnoFlowNode),
    ];
    // Read the current flow-node array untracked so this effect doesn't loop on
    // its own write (and doesn't re-run when xyflow mutates measured fields).
    const prev = new Map(untrack(() => nodes).map((n) => [n.id, n]));
    nodes = desired.map((d) => {
      const existing = prev.get(d.id);
      if (!existing) return d;
      // Merge, preserving xyflow's managed measurement fields. Annotation shape
      // nodes carry width/height in the doc, so re-assert those too (device
      // nodes keep the fixed NODE_W/H already baked into toFlowNode).
      return {
        ...existing,
        type: d.type,
        position: d.position,
        width: d.width ?? existing.width,
        height: d.height ?? existing.height,
        data: d.data,
        selected: d.selected,
        zIndex: d.zIndex,
      };
    });
  });
  $effect(() => {
    // Recompute edges whenever links, node names, states or console ports change.
    void labStore.nodeStates;
    void labStore.consolePorts;
    edges = buildEdges();
  });

  // Sync dragged positions back into the lab doc (debounced via drag-stop).
  function onNodeDragStop({ nodes: dragged }: { nodes: Node[] }) {
    for (const fn of dragged) {
      if (isAnnoId(fn.id)) {
        const aid = annoIdFromFlow(fn.id);
        const anno = labStore.lab.annotations?.find((a) => a.id === aid);
        if (anno?.type === "line") {
          // The node position is the bbox top-left; translate both endpoints by
          // the delta from the old bbox origin so the whole line moves as one.
          const oldX = Math.min(anno.x1, anno.x2) - LINE_PAD;
          const oldY = Math.min(anno.y1, anno.y2) - LINE_PAD;
          const dx = fn.position.x - oldX;
          const dy = fn.position.y - oldY;
          labStore.updateAnnotation(aid, {
            x1: anno.x1 + dx,
            y1: anno.y1 + dy,
            x2: anno.x2 + dx,
            y2: anno.y2 + dy,
          } as Partial<Annotation>);
          continue;
        }
        labStore.updateAnnotation(aid, {
          x: fn.position.x,
          y: fn.position.y,
        } as Partial<Annotation>);
        continue;
      }
      const ln = labStore.lab.nodes.find((n) => n.id === Number(fn.id));
      if (ln) {
        ln.x = fn.position.x;
        ln.y = fn.position.y;
      }
    }
    labStore.notifyTopologyChanged();
  }

  function onConnect(connection: Connection) {
    if (!connection.source || !connection.target) return;
    const srcId = Number(connection.source);
    const tgtId = Number(connection.target);
    if (srcId === tgtId) return;
    const srcNode = labStore.lab.nodes.find((n) => n.id === srcId);
    const tgtNode = labStore.lab.nodes.find((n) => n.id === tgtId);
    if (!srcNode || !tgtNode) return;
    const srcIface = nextInterface(srcNode);
    const tgtIface = nextInterface(tgtNode);
    const link = {
      id: labStore.nextLinkId(),
      type: "p2p" as const,
      endpoints: [
        { node: srcId, interface: srcIface },
        { node: tgtId, interface: tgtIface },
      ],
    };
    labStore.addLink(link);
    if (labStore.lab.id) void labStore.client.linkAdd(labStore.lab.id, link);
  }

  function nextInterface(node: LabNode): string {
    return nextFreeInterface(node);
  }

  // --- drag from palette to create nodes ---
  let canvasEl: HTMLDivElement | undefined = $state();
  const { screenToFlowPosition, flowToScreenPosition, fitView, getViewport, setViewport } =
    useSvelteFlow();

  // Zoom around the canvas centre. The built-in zoomIn/zoomOut helpers proved
  // unreliable here (they drive a d3 transition on the pane that no-ops in this
  // embed), so we compute the new viewport directly and animate via setViewport
  // — the same path fit/reset use, which is known-good. Scaling about the
  // viewport centre keeps the focal point stable.
  const ZOOM_STEP = 1.2;
  function zoomBy(factor: number) {
    const vp = getViewport();
    const rect = canvasEl?.getBoundingClientRect();
    const cx = (rect?.width ?? 0) / 2;
    const cy = (rect?.height ?? 0) / 2;
    const nextZoom = Math.min(2.5, Math.max(0.15, vp.zoom * factor));
    // Keep the flow-point under the centre pinned: x' = cx - (cx - x)*(z'/z).
    const k = nextZoom / vp.zoom;
    const x = cx - (cx - vp.x) * k;
    const y = cy - (cy - vp.y) * k;
    void setViewport({ x, y, zoom: nextZoom }, { duration: 150 });
  }

  // --- R2.1: PNetLab-style link-add (connector → rubber-band → drop → picker) ---
  // The rubber-band is drawn in the canvas-wrap's LOCAL screen coordinates. The
  // source anchor is the node centre projected via flowToScreenPosition; the head
  // follows the pointer. Drop hit-tests the whole target node's bounding box (no
  // precise handle required); on a valid drop the Interface Picker opens.
  let rubber = $state<{ x1: number; y1: number; x2: number; y2: number } | null>(null);
  let ifPicker = $state<{ x: number; y: number; sourceId: number; targetId: number } | null>(null);
  let linkSourceId = 0;

  function localPoint(clientX: number, clientY: number) {
    const r = canvasEl?.getBoundingClientRect();
    return { x: clientX - (r?.left ?? 0), y: clientY - (r?.top ?? 0) };
  }

  function nodeCenterScreen(nodeId: number) {
    const n = labStore.lab.nodes.find((x) => x.id === nodeId);
    if (!n) return null;
    return flowToScreenPosition({ x: n.x + NODE_W / 2, y: n.y + NODE_H / 2 });
  }

  // Whole-node hit test in flow coordinates (from the pointer's flow position).
  function nodeAtFlow(fx: number, fy: number, exclude: number): LabNode | null {
    for (const n of labStore.lab.nodes) {
      if (n.id === exclude) continue;
      if (fx >= n.x && fx <= n.x + NODE_W && fy >= n.y && fy <= n.y + NODE_H) return n;
    }
    return null;
  }

  function startLinkDrag(nodeId: number, ev: PointerEvent) {
    linkSourceId = nodeId;
    linking.sourceId = nodeId;
    const center = nodeCenterScreen(nodeId);
    const c = center ? localPoint(center.x, center.y) : localPoint(ev.clientX, ev.clientY);
    const head = localPoint(ev.clientX, ev.clientY);
    rubber = { x1: c.x, y1: c.y, x2: head.x, y2: head.y };
    window.addEventListener("pointermove", onLinkMove);
    window.addEventListener("pointerup", onLinkUp);
  }

  function onLinkMove(ev: PointerEvent) {
    if (!rubber) return;
    const head = localPoint(ev.clientX, ev.clientY);
    // Keep the source anchor pinned to the (possibly panned) node centre.
    const center = nodeCenterScreen(linkSourceId);
    const c = center ? localPoint(center.x, center.y) : { x: rubber.x1, y: rubber.y1 };
    rubber = { x1: c.x, y1: c.y, x2: head.x, y2: head.y };
    const fp = screenToFlowPosition({ x: ev.clientX, y: ev.clientY });
    const target = nodeAtFlow(fp.x, fp.y, linkSourceId);
    linking.dropTargetId = target ? target.id : null;
  }

  function onLinkUp(ev: PointerEvent) {
    window.removeEventListener("pointermove", onLinkMove);
    window.removeEventListener("pointerup", onLinkUp);
    const fp = screenToFlowPosition({ x: ev.clientX, y: ev.clientY });
    const target = nodeAtFlow(fp.x, fp.y, linkSourceId);
    rubber = null;
    linking.sourceId = null;
    linking.dropTargetId = null;
    if (target) {
      ifPicker = { x: ev.clientX, y: ev.clientY, sourceId: linkSourceId, targetId: target.id };
    }
  }

  onMount(() => {
    linking.start = startLinkDrag;
    linking.requestEdit = openEdit;
    // Annotation grips need to project screen→flow and know the live zoom.
    labStore.screenToFlow = (cx, cy) => screenToFlowPosition({ x: cx, y: cy });
    annoTool.requestStyle = (annoId, clientX, clientY, focusText) => {
      // Offset a touch so the popover doesn't cover the annotation's anchor.
      annoStyle = { x: clientX + 8, y: clientY + 8, annoId, focusText };
    };
    return () => {
      linking.start = null;
      linking.requestEdit = null;
      labStore.screenToFlow = null;
      annoTool.requestStyle = null;
      window.removeEventListener("pointermove", onLinkMove);
      window.removeEventListener("pointerup", onLinkUp);
    };
  });

  // D5: fit-to-content once after first mount (does not clamp panning afterward).
  onMount(() => {
    queueMicrotask(() => void fitView({ padding: 0.25, maxZoom: 1 }));
  });

  function resetView() {
    void setViewport({ x: 0, y: 0, zoom: 1 }, { duration: 200 });
  }
  function fitContent() {
    void fitView({ padding: 0.2, duration: 200, maxZoom: 1.4 });
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    const raw = e.dataTransfer?.getData("application/iolab-node");
    if (!raw) return;
    const { kind, imageId } = JSON.parse(raw) as {
      kind: "iol" | "vpcs" | "nat" | "mgmt";
      imageId?: string;
    };
    const pos = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    const id = labStore.nextNodeId();
    const img = labStore.images.find((i) => i.id === imageId);
    const node: LabNode = buildDroppedNode(kind, id, pos, img);
    labStore.addNode(node);
    labStore.selectedNodeId = id;
  }

  // Count existing nodes of a kind, for stable NAT1/MGMT1 naming.
  function nameForKind(kind: "nat" | "mgmt"): string {
    const prefix = kind === "nat" ? "NAT" : "MGMT";
    const n = labStore.lab.nodes.filter((x) => x.kind === kind).length + 1;
    return `${prefix}${n}`;
  }

  function buildDroppedNode(
    kind: "iol" | "vpcs" | "nat" | "mgmt",
    id: number,
    pos: { x: number; y: number },
    img?: (typeof labStore.images)[number]
  ): LabNode {
    if (kind === "iol") {
      return {
        id,
        kind,
        name: `R${id}`,
        x: pos.x,
        y: pos.y,
        ram: 1024,
        ethernet: 1,
        serial: 1,
        image: img ? { id: img.id, filename: img.filename, class: img.class } : undefined,
      };
    }
    if (kind === "nat" || kind === "mgmt") {
      // Single-interface builtin nodes (eth0), doc shape mirrors a VPCS node.
      return { id, kind, name: nameForKind(kind), x: pos.x, y: pos.y };
    }
    return { id, kind: "vpcs", name: `PC${id}`, x: pos.x, y: pos.y };
  }

  // --- context menus / popovers ---
  let nodeMenu = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let linkMenu = $state<{ x: number; y: number; linkId: number } | null>(null);
  let annoMenu = $state<{ x: number; y: number; annoId: string } | null>(null);
  let annoStyle = $state<{ x: number; y: number; annoId: string; focusText: boolean } | null>(null);
  let imagePopover = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let iconPicker = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let editDialog = $state<{ nodeId: number } | null>(null);

  function openEdit(nid: number) {
    labStore.selectedNodeId = nid;
    editDialog = { nodeId: nid };
  }

  function onNodeContextMenu({ node, event }: { node: Node; event: MouseEvent }) {
    event.preventDefault();
    if (isAnnoId(node.id)) {
      const aid = annoIdFromFlow(node.id);
      labStore.selectedAnnotationId = aid;
      labStore.selectedNodeId = null;
      labStore.selectedLinkId = null;
      annoMenu = { x: event.clientX, y: event.clientY, annoId: aid };
      return;
    }
    labStore.selectedNodeId = Number(node.id);
    nodeMenu = { x: event.clientX, y: event.clientY, nodeId: Number(node.id) };
  }

  function onEdgeContextMenu({ edge, event }: { edge: Edge; event: MouseEvent }) {
    event.preventDefault();
    const linkId = (edge.data as any)?.linkId as number;
    labStore.selectedLinkId = linkId;
    linkMenu = { x: event.clientX, y: event.clientY, linkId };
  }

  function onNodeClick({ node }: { node: Node }) {
    if (isAnnoId(node.id)) {
      labStore.selectedAnnotationId = annoIdFromFlow(node.id);
      labStore.selectedNodeId = null;
      labStore.selectedLinkId = null;
      return;
    }
    labStore.selectedNodeId = Number(node.id);
    labStore.selectedLinkId = null;
    labStore.selectedAnnotationId = null;
  }

  function onEdgeClick({ edge }: { edge: Edge }) {
    labStore.selectedLinkId = (edge.data as any)?.linkId as number;
    labStore.selectedNodeId = null;
    labStore.selectedAnnotationId = null;
  }

  function onPaneClick({ event }: { event: MouseEvent } = { event: undefined as any }) {
    // The Line tool is a two-click placement: first click sets one endpoint,
    // second sets the other. Handle it before the single-click place path.
    if (annoTool.active === "line" && event) {
      handleLineClick(event);
      return;
    }
    // An armed DRAW tool places its annotation at the click point, then disarms.
    if (annoTool.active && event) {
      placeAnnotation(annoTool.active, event);
      annoTool.disarm();
      return;
    }
    labStore.selectedNodeId = null;
    labStore.selectedLinkId = null;
    labStore.selectedAnnotationId = null;
  }

  // --- Line tool: click first endpoint, then second (Escape cancels). ---
  let linePending = $state<{ x: number; y: number } | null>(null);
  function handleLineClick(event: MouseEvent) {
    const p = screenToFlowPosition({ x: event.clientX, y: event.clientY });
    if (!linePending) {
      linePending = { x: p.x, y: p.y };
      return;
    }
    const id = labStore.newAnnotationId();
    const anno: Annotation = {
      id,
      type: "line",
      x1: linePending.x,
      y1: linePending.y,
      x2: p.x,
      y2: p.y,
      color: annoTool.color,
    };
    labStore.addAnnotation(anno);
    labStore.selectedAnnotationId = id;
    labStore.selectedNodeId = null;
    labStore.selectedLinkId = null;
    linePending = null;
    annoTool.disarm();
  }

  // Place a new annotation at the pointer's flow position. Text opens inline
  // editing immediately (via annoTool.editRequestId, consumed by AnnoText);
  // shapes drop a default ~200x120 box, drag-movable + double-click-to-label.
  function placeAnnotation(tool: typeof annoTool.active, event: MouseEvent) {
    if (!tool) return;
    const p = screenToFlowPosition({ x: event.clientX, y: event.clientY });
    const id = labStore.newAnnotationId();
    let anno: Annotation;
    if (tool === "text" || tool === "note") {
      anno = {
        id,
        type: "text",
        x: p.x,
        y: p.y,
        text: tool === "note" ? "Note" : "Text",
        size: "m",
        color: annoTool.color,
        fill: tool === "note",
      };
    } else if (tool === "rect" || tool === "ellipse") {
      // Centre the default box on the click point.
      const w = 200;
      const h = 120;
      anno = {
        id,
        type: tool,
        x: p.x - w / 2,
        y: p.y - h / 2,
        w,
        h,
        color: annoTool.color,
      };
    } else {
      return; // line handled separately (two-click)
    }
    labStore.addAnnotation(anno);
    labStore.selectedAnnotationId = id;
    labStore.selectedNodeId = null;
    labStore.selectedLinkId = null;
    if (tool === "text" || tool === "note") annoTool.editRequestId = id;
  }

  // Delete the selected annotation on Delete/Backspace (when not typing in a
  // field). Node/link deletion stays on the context menu — matching prior UX.
  function onWindowKeydown(e: KeyboardEvent) {
    // Escape cancels an in-progress line placement (or a fully-armed tool).
    if (e.key === "Escape") {
      if (linePending) {
        linePending = null;
        annoTool.disarm();
        return;
      }
      if (annoTool.active) annoTool.disarm();
      // Item 6 — Escape also cancels an active pan tool.
      if (panMode) panMode = false;
      return;
    }
    if (e.key !== "Delete" && e.key !== "Backspace") return;
    const t = e.target as HTMLElement | null;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    if (labStore.selectedAnnotationId) {
      e.preventDefault();
      labStore.removeAnnotation(labStore.selectedAnnotationId);
    }
  }

  function buildNodeMenuItems(menu: { x: number; y: number; nodeId: number }): MenuItem[] {
    const nid = menu.nodeId;
    const nodeState = labStore.nodeStates[nid] ?? "stopped";
    return [
      {
        label: "Start",
        disabled: nodeState === "running" || nodeState === "starting",
        action: () => void labStore.startNode(nid),
      },
      {
        label: "Stop",
        disabled: nodeState === "stopped",
        action: () => void labStore.stopNode(nid),
      },
      {
        label: "Console",
        disabled: nodeState !== "running",
        action: () => labStore.openConsole(nid),
      },
      { separator: true, label: "sep1", action: () => {} },
      {
        label: "Edit…",
        action: () => openEdit(nid),
      },
      {
        label: "Change image…",
        action: () => {
          imagePopover = { x: menu.x, y: menu.y, nodeId: nid };
        },
      },
      {
        label: "Change icon…",
        action: () => {
          iconPicker = { x: menu.x, y: menu.y, nodeId: nid };
        },
      },
      { separator: true, label: "sep2", action: () => {} },
      {
        label: "Duplicate",
        action: () => {
          const newId = labStore.duplicateNode(nid);
          if (newId !== null) labStore.selectedNodeId = newId;
        },
      },
      {
        label: "Delete",
        danger: true,
        action: () => labStore.removeNode(nid),
      },
    ];
  }

  function buildLinkMenuItems(menu: { linkId: number }): MenuItem[] {
    const link = labStore.lab.links.find((l) => l.id === menu.linkId);
    const capturing = link?.capture?.enabled ?? false;
    return [
      {
        label: "Live capture…",
        action: () => labStore.openCapture(menu.linkId),
      },
      {
        label: capturing ? "Stop capture" : "Capture in Wireshark",
        action: () => {
          const l = labStore.lab.links.find((x) => x.id === menu.linkId);
          if (!l) return;
          if (capturing) {
            l.capture = { enabled: false };
            void labStore.client.captureStop(labStore.lab.id, l.id);
          } else {
            l.capture = { enabled: true, mode: "live" };
            void labStore.client.captureStart(labStore.lab.id, l.id);
          }
        },
      },
      {
        label: "Delete",
        danger: true,
        action: () => labStore.removeLink(menu.linkId),
      },
    ];
  }

  function buildAnnoMenuItems(menu: { annoId: string }): MenuItem[] {
    const anno = labStore.lab.annotations?.find((a) => a.id === menu.annoId);
    const items: MenuItem[] = [];
    items.push({
      label: anno?.type === "text" ? "Edit text…" : "Edit label…",
      action: () => {
        annoTool.editRequestId = menu.annoId;
      },
    });
    items.push({ separator: true, label: "sep", action: () => {} });
    items.push({
      label: "Delete",
      danger: true,
      action: () => labStore.removeAnnotation(menu.annoId),
    });
    return items;
  }
</script>

<svelte:window onkeydown={onWindowKeydown} />

<div
  class="canvas-wrap"
  class:arming={annoTool.active !== null}
  class:panning={panMode}
  bind:this={canvasEl}
  ondragover={onDragOver}
  ondrop={onDrop}
  role="application"
>
  <SvelteFlow
    bind:nodes
    bind:edges
    {nodeTypes}
    {edgeTypes}
    fitView
    minZoom={0.15}
    maxZoom={2.5}
    connectionMode={ConnectionMode.Loose}
    panOnDrag={panMode}
    panActivationKey="Control"
    selectionOnDrag={!panMode}
    proOptions={{ hideAttribution: true }}
    onnodedragstop={onNodeDragStop}
    onconnect={onConnect}
    onnodecontextmenu={onNodeContextMenu}
    onedgecontextmenu={onEdgeContextMenu}
    onnodeclick={onNodeClick}
    onedgeclick={onEdgeClick}
    onpaneclick={onPaneClick}
    onmove={() => (labStore.canvasZoom = getViewport().zoom)}
    colorMode={themeStore.current === "glass" ? "light" : "dark"}
  >
    <Background variant={BackgroundVariant.Dots} gap={20} size={1.4} bgColor="transparent" patternColor="var(--dot)" />
  </SvelteFlow>

  <!-- R2.1: rubber-band cable following the cursor during a link-add drag. -->
  {#if rubber}
    <svg class="rubber-layer" aria-hidden="true">
      <path
        class="rubber-cable"
        d={`M ${rubber.x1} ${rubber.y1} L ${rubber.x2} ${rubber.y2}`}
      />
    </svg>
  {/if}

  <!-- D5: bench view controls -->
  <div class="view-controls">
    <!-- Item 6 — pan tool. When on, plain left-drag pans (mouse-only users);
         when off, plain drag draws a selection box and only Ctrl+drag pans. -->
    <button
      class="vc"
      class:on={panMode}
      title={panMode ? "Pan tool on — click or Esc to turn off" : "Pan tool — drag to pan (or hold Ctrl)"}
      aria-label="Toggle pan tool"
      aria-pressed={panMode}
      onclick={() => (panMode = !panMode)}
    >{@html uiSvg("hand", 15)}</button>
    <button class="vc" title="Zoom in" onclick={() => zoomBy(ZOOM_STEP)} aria-label="Zoom in">+</button>
    <button class="vc" title="Zoom out" onclick={() => zoomBy(1 / ZOOM_STEP)} aria-label="Zoom out">−</button>
    <button class="vc" title="Fit to content" onclick={fitContent} aria-label="Fit to content">{@html uiSvg("fit", 15)}</button>
    <button class="vc" title="Reset view" onclick={resetView} aria-label="Reset view">{@html uiSvg("reset", 15)}</button>
  </div>

  {#if annoTool.active === "line"}
    <div class="line-hint">
      {linePending
        ? "Click to set the line's second endpoint (Esc cancels)."
        : "Click to set the line's first endpoint (Esc cancels)."}
    </div>
  {/if}

  {#if labStore.lab.nodes.length === 0}
    <div class="empty-state">
      <div class="empty-title">The bench is clear</div>
      <div class="empty-sub">Drag a device onto the bench to start wiring a lab.</div>
    </div>
  {/if}

  {#if nodeMenu}
    <ContextMenu
      x={nodeMenu.x}
      y={nodeMenu.y}
      items={buildNodeMenuItems(nodeMenu)}
      onClose={() => (nodeMenu = null)}
    />
  {/if}
  {#if linkMenu}
    <ContextMenu
      x={linkMenu.x}
      y={linkMenu.y}
      items={buildLinkMenuItems(linkMenu)}
      onClose={() => (linkMenu = null)}
    />
  {/if}
  {#if annoMenu}
    <ContextMenu
      x={annoMenu.x}
      y={annoMenu.y}
      items={buildAnnoMenuItems(annoMenu)}
      onClose={() => (annoMenu = null)}
    />
  {/if}
  {#if annoStyle}
    <AnnoStylePopover
      x={annoStyle.x}
      y={annoStyle.y}
      annoId={annoStyle.annoId}
      focusText={annoStyle.focusText}
      onClose={() => (annoStyle = null)}
    />
  {/if}
  {#if imagePopover}
    <ChangeImagePopover
      x={imagePopover.x}
      y={imagePopover.y}
      nodeId={imagePopover.nodeId}
      onClose={() => (imagePopover = null)}
    />
  {/if}
  {#if iconPicker}
    <IconPicker
      x={iconPicker.x}
      y={iconPicker.y}
      current={labStore.lab.nodes.find((n) => n.id === iconPicker!.nodeId)?.icon}
      onPick={(key) => {
        labStore.setNodeIcon(iconPicker!.nodeId, key);
      }}
      onClose={() => (iconPicker = null)}
    />
  {/if}

  {#if ifPicker}
    <InterfacePicker
      x={ifPicker.x}
      y={ifPicker.y}
      sourceId={ifPicker.sourceId}
      targetId={ifPicker.targetId}
      onClose={() => (ifPicker = null)}
    />
  {/if}

  {#if editDialog}
    <NodeEditDialog nodeId={editDialog.nodeId} onClose={() => (editDialog = null)} />
  {/if}
</div>

<style>
  .canvas-wrap {
    position: relative;
    width: 100%;
    height: 100%;
    background: var(--ground);
  }
  /* A DRAW tool is armed — the next canvas click drops the annotation. */
  .canvas-wrap.arming :global(.svelte-flow__pane) {
    cursor: crosshair;
  }
  /* Item 6a — empty-canvas cursor is the default arrow, NOT grab. With
     panOnDrag=false the pane never gets xyflow's `.draggable` grab cursor, but
     selectionOnDrag adds a `.selection` pointer cursor; force default here so an
     idle canvas reads as "arrow". Overridden by .arming (crosshair) and by the
     pan tool (.panning → xyflow's own grab/grabbing on .draggable). */
  .canvas-wrap:not(.arming):not(.panning) :global(.svelte-flow__pane) {
    cursor: default;
  }
  :global(.svelte-flow) {
    background: var(--ground);
  }
  /* xyflow's default node box chrome is unwanted — our nodes draw their own.
     Keep width/display defaults so xyflow can still measure the node. */
  :global(.svelte-flow__node) {
    background: transparent;
    border: none;
    padding: 0;
    box-shadow: none;
    border-radius: 0;
  }
  :global(.svelte-flow__node.selected) {
    box-shadow: none;
  }

  /* R2.1 — rubber-band cable overlay (screen-space, above the flow). */
  .rubber-layer {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
    z-index: 6;
  }
  .rubber-cable {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.5;
    stroke-dasharray: 6 5;
    stroke-linecap: round;
  }

  .view-controls {
    position: absolute;
    left: 12px;
    bottom: 12px;
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 3px;
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-sm);
    z-index: 5;
  }
  .vc {
    all: unset;
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    color: var(--ink-2);
    font-size: 15px;
    cursor: pointer;
  }
  .vc:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  /* Item 6 — pressed state for the pan toggle. */
  .vc.on {
    background: var(--accent-muted);
    color: var(--accent);
  }
  .vc :global(svg) {
    width: 15px;
    height: 15px;
  }

  .line-hint {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 6;
    padding: 6px 12px;
    font-size: var(--fs-xs);
    color: var(--accent-ink);
    background: var(--accent);
    border-radius: var(--radius-full);
    box-shadow: var(--shadow-md);
    pointer-events: none;
  }
  .empty-state {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    pointer-events: none;
    gap: 6px;
    text-align: center;
  }
  .empty-title {
    font-size: var(--fs-lg);
    color: var(--ink-2);
    font-weight: 600;
  }
  .empty-sub {
    font-size: var(--fs-sm);
    color: var(--ink-3);
  }
</style>
