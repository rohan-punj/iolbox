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
  import ToolNode from "../nodes/ToolNode.svelte";
  import PcNode from "../nodes/PcNode.svelte";
  import AnnoText from "../nodes/AnnoText.svelte";
  import AnnoShape from "../nodes/AnnoShape.svelte";
  import AnnoLine, { LINE_PAD } from "../nodes/AnnoLine.svelte";
  import FloatingEdge from "../edges/FloatingEdge.svelte";
  import AnnoStylePopover from "./AnnoStylePopover.svelte";
  import ContextMenu, { type MenuItem } from "./ContextMenu.svelte";
  import ChangeImagePopover from "./ChangeImagePopover.svelte";
  import IconPicker from "./IconPicker.svelte";
  import InterfacePicker from "./InterfacePicker.svelte";
  import { uiSvg } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import { railUiStore, type NodePlacement } from "../railUiStore.svelte";
  import { dragNodeCountStore, NODE_SPACING_PX } from "../dragNodeCountStore.svelte";
  import { nextFreeInterface } from "../interfaces";
  import { annoTool } from "../annoTool.svelte";
  import type { Annotation, LabNode, LinkFault, NodeKind } from "../labTypes";

  // The NAT gateway reuses the VPCS single-interface node chrome; the
  // distinct glyph comes from its default icon (defaultIconFor). annoText /
  // annoShape are the Excalidraw-style annotation layer — rendered as flow nodes
  // (so drag/pan/zoom come free) but derived from lab.annotations, never lab.nodes.
  const nodeTypes: NodeTypes = {
    iol: IolNode,
    vpcs: VpcsNode,
    nat: VpcsNode,
    tool: ToolNode,
    pc: PcNode,
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
        // Tool nodes fall back to their pack's icon (see ToolNode.svelte) when
        // the node itself has no explicit icon override.
        packId: n.kind === "tool" ? (n.config?.pack as string | undefined) : undefined,
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
      nodeId,
      name: node?.name ?? `#${nodeId}`,
      iface,
      telnet: running ? labStore.consolePorts[nodeId] : undefined,
    };
  }

  function toFlowEdge(l: (typeof labStore.lab.links)[number]): Edge {
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
        fault: labStore.linkFaults[l.id],
        source: endpointInfo(a?.node ?? 0, a?.interface ?? ""),
        target: endpointInfo(b?.node ?? 0, b?.interface ?? ""),
      },
    };
  }

  // Plain per-link map. Parallel-link fan-out (N links between the same node pair
  // curving apart instead of stacking) is derived LIVE inside FloatingEdge from
  // the lab doc — see its `parallel` derived — because xyflow does not reliably
  // push updated edge `data` into an already-mounted sibling edge when a new
  // parallel link is added, which previously left the first cable un-fanned.
  function buildEdges(): Edge[] {
    return labStore.lab.links.map((l) => toFlowEdge(l));
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
    void labStore.linkFaults;
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
  const snap = $derived(labStore.lab.canvas?.snapGrid ?? false);
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
    const unbindPlaceNode = railUiStore.bindPlaceNode(placeNodeAtViewportCenter);
    const viewport = getViewport();
    labStore.canvasZoom = viewport.zoom;
    labStore.canvasPan = { x: viewport.x, y: viewport.y };
    labStore.flowToScreen = (x, y) => flowToScreenPosition({ x, y });
    // Annotation grips need to project screen→flow and know the live zoom.
    labStore.screenToFlow = (cx, cy) => screenToFlowPosition({ x: cx, y: cy });
    annoTool.requestStyle = (annoId, clientX, clientY, focusText) => {
      // Offset a touch so the popover doesn't cover the annotation's anchor.
      annoStyle = { x: clientX + 8, y: clientY + 8, annoId, focusText };
    };
    return () => {
      linking.start = null;
      linking.requestEdit = null;
      unbindPlaceNode();
      labStore.screenToFlow = null;
      labStore.flowToScreen = null;
      labStore.canvasPan = { x: 0, y: 0 };
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
    dragNodeCountStore.update(e.clientX, e.clientY, e.shiftKey);
  }

  function onDrop(e: DragEvent) {
    e.preventDefault();
    const raw = e.dataTransfer?.getData("application/iolbox-node");
    if (!raw) return;
    const { kind, imageId, packId } = JSON.parse(raw) as {
      kind: NodeKind;
      imageId?: string;
      packId?: string;
    };
    // Shift-drag: dragNodeCountStore tracked how far the cursor traveled
    // past the drop origin while Shift was held (App.svelte's onDragStart /
    // CanvasInner's onDragOver) and turned that into a count. Consume it
    // here rather than trusting e.shiftKey at drop time — the modifier can
    // legitimately be released a frame before the drop event fires.
    const dragCount = dragNodeCountStore.consume()?.count ?? 1;
    const img = labStore.images.find((i) => i.id === imageId);
    let lastId = -1;
    for (let i = 0; i < dragCount; i++) {
      const pos = screenToFlowPosition({ x: e.clientX + i * NODE_SPACING_PX, y: e.clientY });
      const id = labStore.nextNodeId();
      lastId = id;
      const node: LabNode = buildDroppedNode(kind, id, pos, img, packId);
      const registered = labStore.addNode(node);
      // A NAT gateway has no boot/config step and only exists to provide
      // egress — start it the moment it lands (after the supervisor ack'd
      // node.add, so node.start can find it). Other kinds stay stopped for
      // pre-start editing.
      if (kind === "nat") {
        void registered.then(() => labStore.startNode(id));
      }
    }
    labStore.selectedNodeId = lastId;
  }

  function placeNodeAtViewportCenter(drag: NodePlacement) {
    const rect = canvasEl?.getBoundingClientRect();
    if (!rect) return;
    const pos = screenToFlowPosition({
      x: rect.left + rect.width / 2,
      y: rect.top + rect.height / 2,
    });
    const id = labStore.nextNodeId();
    const img = labStore.images.find((i) => i.id === drag.imageId);
    const node: LabNode = buildDroppedNode(drag.kind, id, pos, img, drag.packId);
    const registered = labStore.addNode(node);
    labStore.selectedNodeId = id;
    if (drag.kind === "nat") {
      void registered.then(() => labStore.startNode(id));
    }
  }

  // Count existing nodes of a kind, for stable NAT1 naming.
  function nameForKind(kind: "nat"): string {
    const n = labStore.lab.nodes.filter((x) => x.kind === kind).length + 1;
    return `NAT${n}`;
  }

  function buildDroppedNode(
    kind: NodeKind,
    id: number,
    pos: { x: number; y: number },
    img?: (typeof labStore.images)[number],
    packId?: string
  ): LabNode {
    if (kind === "iol") {
      return {
        id,
        kind,
        name: `${img?.class === "l2" ? "SW" : "R"}${id}`,
        x: pos.x,
        y: pos.y,
        ram: 1024,
        ethernet: 1,
        serial: 1,
        image: img ? { id: img.id, filename: img.filename, class: img.class } : undefined,
      };
    }
    if (kind === "nat") {
      // Single-interface builtin node (eth0), doc shape mirrors a VPCS node.
      return { id, kind, name: nameForKind(kind), x: pos.x, y: pos.y };
    }
    if (kind === "tool") {
      const pack = labStore.toolPacks.find((p) => p.id === (packId ?? labStore.toolPacks[0]?.id));
      return {
        id,
        kind,
        name: `Tool${id}`,
        x: pos.x,
        y: pos.y,
        config: { pack: packId ?? labStore.toolPacks[0]?.id ?? "" },
        icon: pack?.icon,
      };
    }
    if (kind === "pc") {
      return { id, kind, name: `PC${id}`, x: pos.x, y: pos.y };
    }
    return { id, kind: "vpcs", name: `PC${id}`, x: pos.x, y: pos.y };
  }

  // --- context menus / popovers ---
  let nodeMenu = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let selectionMenu = $state<{ x: number; y: number; ids: number[] } | null>(null);
  let linkMenu = $state<{ x: number; y: number; linkId: number } | null>(null);
  let annoMenu = $state<{ x: number; y: number; annoId: string } | null>(null);
  let annoStyle = $state<{ x: number; y: number; annoId: string; focusText: boolean } | null>(null);
  let imagePopover = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let iconPicker = $state<{ x: number; y: number; nodeId: number } | null>(null);

  function openEdit(nid: number) {
    labStore.selectedNodeId = nid;
    labStore.inspectorNodeId = nid;
  }

  // Device-node ids currently selected in the flow (multi-select via shift/box),
  // mapped back to numeric lab node ids. Annotations never join the bulk menu.
  function selectedDeviceIds(): number[] {
    return nodes
      .filter((n) => n.selected && !isAnnoId(n.id))
      .map((n) => Number(n.id));
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
    // Right-clicking a node that is part of a multi-node selection acts on the
    // whole selection (bulk menu), matching how the selection rectangle behaves.
    const selected = selectedDeviceIds();
    if (selected.length > 1 && selected.includes(Number(node.id))) {
      selectionMenu = { x: event.clientX, y: event.clientY, ids: selected };
      return;
    }
    labStore.selectedNodeId = Number(node.id);
    nodeMenu = { x: event.clientX, y: event.clientY, nodeId: Number(node.id) };
  }

  // Right-click on the selection rectangle (xyflow draws one around a
  // multi-select) — same bulk menu as right-clicking a selected node.
  function onSelectionContextMenu({ event }: { nodes: Node[]; event: MouseEvent }) {
    event.preventDefault();
    const selected = selectedDeviceIds();
    if (selected.length === 0) return;
    selectionMenu = { x: event.clientX, y: event.clientY, ids: selected };
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
    labStore.inspectorNodeId = null;
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
      const target = e.target instanceof HTMLElement ? e.target : null;
      const inInputOrTerminal = Boolean(
        target?.matches("input, textarea, select, [contenteditable='true']") ||
          target?.isContentEditable ||
          target?.closest(".xterm, .term-container, .cap-container")
      );
      if (!inInputOrTerminal) labStore.selectedLinkId = null;
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
    // WS1: while an action is in flight on this node, disable sibling actions
    // (Console stays enabled — it's client-side, no WS).
    const locked = labStore.nodeLocks[nid] != null;
    return [
      {
        id: "node-start",
        label: "Start",
        disabled: locked || nodeState === "running" || nodeState === "starting",
        action: () => void labStore.startNode(nid),
      },
      {
        id: "node-stop",
        label: "Stop",
        disabled: locked || nodeState === "stopped",
        action: () => void labStore.stopNode(nid),
      },
      {
        id: "node-console",
        label: "Console",
        disabled: nodeState !== "running",
        action: () => labStore.openConsoleByMode(nid),
      },
      {
        id: "node-duplicate",
        label: "Duplicate",
        disabled: locked,
        action: () => {
          const newId = labStore.duplicateNode(nid);
          if (newId !== null) labStore.selectedNodeId = newId;
        },
      },
      {
        id: "node-wipe",
        label: "Wipe",
        // Mirrors the bulk-selection Wipe gate: requires the node be stopped.
        disabled: locked || nodeState !== "stopped",
        action: () => {
          if (!confirm("Wipe saved config/state for this node? This cannot be undone.")) return;
          void labStore.wipeNode(nid);
        },
      },
      { id: "node-separator-actions", separator: true, label: "sep1", action: () => {} },
      {
        id: "node-edit",
        label: "Edit…",
        action: () => openEdit(nid),
      },
      {
        id: "node-change-image",
        label: "Change image…",
        action: () => {
          imagePopover = { x: menu.x, y: menu.y, nodeId: nid };
        },
      },
      {
        id: "node-change-icon",
        label: "Change icon…",
        action: () => {
          iconPicker = { x: menu.x, y: menu.y, nodeId: nid };
        },
      },
      { id: "node-separator-danger", separator: true, label: "sep2", action: () => {} },
      {
        id: "node-delete",
        label: "Delete",
        danger: true,
        action: () => labStore.removeNode(nid),
      },
    ];
  }

  // Bulk menu for a multi-node selection. Each action fans out over the
  // selected ids, respecting per-node state (only start what's stopped, only
  // stop what's running, …) so a mixed selection never errors.
  function buildSelectionMenuItems(menu: { ids: number[] }): MenuItem[] {
    const ids = menu.ids;
    const stateOf = (id: number) => labStore.nodeStates[id] ?? "stopped";
    const startable = ids.filter((id) => stateOf(id) !== "running" && stateOf(id) !== "starting");
    const stoppable = ids.filter((id) => stateOf(id) === "running" || stateOf(id) === "starting");
    const running = ids.filter((id) => stateOf(id) === "running");
    // WS1: a bulk action is disabled when ANY selected node is mid-action.
    const anyLocked = ids.some((id) => labStore.nodeLocks[id] != null);
    return [
      // Header row — plain disabled item showing what the menu acts on.
      { id: "selection-summary", label: `${ids.length} nodes`, disabled: true, action: () => {} },
      {
        id: "selection-start",
        label: "Start",
        disabled: anyLocked || startable.length === 0,
        action: () => {
          for (const id of startable) void labStore.startNode(id);
        },
      },
      {
        id: "selection-stop",
        label: "Stop",
        disabled: anyLocked || stoppable.length === 0,
        action: () => {
          for (const id of stoppable) void labStore.stopNode(id);
        },
      },
      {
        id: "selection-console",
        label: "Console",
        disabled: running.length === 0,
        action: () => {
          // Same stagger as openAllConsoles: browsers throttle a burst of
          // programmatic external-protocol (telnet://) launches from a single
          // click handler, so the opens are spaced ~300ms apart.
          running.forEach((id, i) => {
            if (i === 0) labStore.openConsoleByMode(id);
            else setTimeout(() => labStore.openConsoleByMode(id), i * 300);
          });
        },
      },
      { id: "selection-separator-actions", separator: true, label: "sep1", action: () => {} },
      {
        id: "selection-duplicate",
        label: "Duplicate",
        disabled: anyLocked,
        action: () => {
          for (const id of ids) labStore.duplicateNode(id);
        },
      },
      {
        id: "selection-wipe",
        label: "Wipe",
        // Wipe requires stopped nodes (mirrors the single-node quick-action gate).
        disabled: anyLocked || stoppable.length > 0,
        action: () => {
          if (!confirm(`Wipe saved config/state for ${ids.length} nodes? This cannot be undone.`)) return;
          for (const id of ids) void labStore.wipeNode(id);
        },
      },
      { id: "selection-separator-danger", separator: true, label: "sep2", action: () => {} },
      {
        id: "selection-delete",
        label: "Delete",
        danger: true,
        action: () => {
          if (!confirm(`Delete ${ids.length} nodes and their links?`)) return;
          for (const id of ids) labStore.removeNode(id);
        },
      },
    ];
  }

  function buildLinkMenuItems(menu: { linkId: number }): MenuItem[] {
    const link = labStore.lab.links.find((l) => l.id === menu.linkId);
    const capturing = link?.capture?.enabled ?? false;
    const unsupportedReason = link ? linkFaultUnsupportedReason(link) : "unknown link";
    const faultChildren: MenuItem[] = [];
    if (link) {
      faultChildren.push({
        id: "link-fault-clear",
        label: "Clear fault",
        action: () => void labStore.setLinkFault(link.id, null),
      });
      const targets: Array<{ label: string; targetEndpoint?: number }> = [
        { label: "Both ends" },
        ...link.endpoints.map((ep, index) => ({
          label: `${endpointDisplay(ep)}`,
          targetEndpoint: index,
        })),
      ];
      for (const target of targets) {
        const suffix = target.targetEndpoint === undefined ? "" : ` (${target.label})`;
        faultChildren.push({
          id: `link-fault-down-${target.targetEndpoint ?? "both"}`,
          label: `Down${suffix}`,
          action: () => void labStore.setLinkFault(link.id, {
            down: true,
            ...(target.targetEndpoint === undefined ? {} : { targetEndpoint: target.targetEndpoint }),
          }),
        });
        faultChildren.push({
          id: `link-fault-delay-${target.targetEndpoint ?? "both"}`,
          label: `100 ms delay${suffix}`,
          action: () => void labStore.setLinkFault(link.id, {
            delayMs: 100,
            ...(target.targetEndpoint === undefined ? {} : { targetEndpoint: target.targetEndpoint }),
          }),
        });
        faultChildren.push({
          id: `link-fault-loss-${target.targetEndpoint ?? "both"}`,
          label: `20% loss${suffix}`,
          action: () => void labStore.setLinkFault(link.id, {
            lossPct: 20,
            ...(target.targetEndpoint === undefined ? {} : { targetEndpoint: target.targetEndpoint }),
          }),
        });
        faultChildren.push({
          id: `link-fault-rate-${target.targetEndpoint ?? "both"}`,
          label: `1 mbit rate${suffix}`,
          action: () => void labStore.setLinkFault(link.id, {
            rateKbit: 1000,
            ...(target.targetEndpoint === undefined ? {} : { targetEndpoint: target.targetEndpoint }),
          }),
        });
      }
      faultChildren.push({ id: "link-fault-separator", separator: true, label: "fault-sep", action: () => {} });
      faultChildren.push({
        id: "link-fault-custom",
        label: "Custom JSON…",
        title: "Enter a LinkFault JSON object; omit targetEndpoint for every endpoint",
        action: () => {
          const raw = window.prompt(
            'LinkFault JSON (for example {"delayMs":50,"lossPct":1,"targetEndpoint":0})'
          );
          if (raw === null) return;
          try {
            const fault = JSON.parse(raw) as LinkFault;
            void labStore.setLinkFault(link.id, fault);
          } catch {
            labStore.lastError = "Fault JSON is invalid";
            labStore.pushLog("error", labStore.lastError);
          }
        },
      });
    }
    return [
      {
        id: "link-faults",
        label: "Faults",
        disabled: Boolean(unsupportedReason),
        title: unsupportedReason || "Admin down/up and per-endpoint egress impairment",
        action: () => {},
        submenu: faultChildren,
      },
      { id: "link-separator-faults", separator: true, label: "fault-menu-sep", action: () => {} },
      {
        id: "link-live-capture",
        label: "Live capture…",
        action: () => labStore.openCapture(menu.linkId),
      },
      {
        id: "link-wireshark-capture",
        label: capturing ? "Stop capture" : "Capture in Wireshark…",
        action: () => {
          if (capturing) {
            const l = labStore.lab.links.find((x) => x.id === menu.linkId);
            if (!l) return;
            l.capture = { enabled: false };
            void labStore.client.captureStop(labStore.lab.id, l.id);
            return;
          }
          // A browser can't launch wireshark.exe directly — the product answer
          // is the capture tab's own "Open in Wireshark" overlay (Save .pcapng
          // + live-attach command). openCapture() starts the tee; the
          // wiresharkOverlayFor signal asks Console.svelte to jump straight to
          // that overlay instead of the plain live-summary tab.
          labStore.openCapture(menu.linkId);
          labStore.wiresharkOverlayFor = menu.linkId;
        },
      },
      {
        id: "link-delete",
        label: "Delete",
        danger: true,
        action: () => labStore.removeLink(menu.linkId),
      },
    ];
  }

  function endpointDisplay(ep: { node: number; interface: string }): string {
    const n = labStore.lab.nodes.find((node) => node.id === ep.node);
    return `${n?.name ?? `#${ep.node}`} ${ep.interface}`;
  }

  function linkFaultUnsupportedReason(link: (typeof labStore.lab.links)[number]): string {
    for (const ep of link.endpoints) {
      const n = labStore.lab.nodes.find((node) => node.id === ep.node);
      if (String(n?.kind) === "iol" && !/^e\d+\/\d+$/i.test(ep.interface)) {
        return `Unsupported: ${n?.name ?? `node ${ep.node}`} ${ep.interface} has no Ethernet static tap`;
      }
    }
    return "";
  }

  function buildAnnoMenuItems(menu: { annoId: string }): MenuItem[] {
    const anno = labStore.lab.annotations?.find((a) => a.id === menu.annoId);
    const items: MenuItem[] = [];
    items.push({
      id: "anno-edit",
      label: anno?.type === "text" ? "Edit text…" : "Edit label…",
      action: () => {
        annoTool.editRequestId = menu.annoId;
      },
    });
    items.push({
      id: "anno-duplicate",
      label: "Duplicate",
      action: () => {
        const newId = labStore.duplicateAnnotation(menu.annoId);
        if (newId !== null) labStore.selectedAnnotationId = newId;
      },
    });
    items.push({ id: "anno-separator", separator: true, label: "sep", action: () => {} });
    items.push({
      id: "anno-delete",
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
    snapGrid={snap ? [20, 20] : undefined}
    proOptions={{ hideAttribution: true }}
    onnodedragstop={onNodeDragStop}
    onconnect={onConnect}
    onnodecontextmenu={onNodeContextMenu}
    onselectioncontextmenu={onSelectionContextMenu}
    onedgecontextmenu={onEdgeContextMenu}
    onnodeclick={onNodeClick}
    onedgeclick={onEdgeClick}
    onpaneclick={onPaneClick}
    onmove={() => {
      const viewport = getViewport();
      labStore.canvasZoom = viewport.zoom;
      labStore.canvasPan = { x: viewport.x, y: viewport.y };
    }}
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
  {#if selectionMenu}
    <ContextMenu
      x={selectionMenu.x}
      y={selectionMenu.y}
      items={buildSelectionMenuItems(selectionMenu)}
      onClose={() => (selectionMenu = null)}
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
