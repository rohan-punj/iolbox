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
  import FloatingEdge from "../edges/FloatingEdge.svelte";
  import ContextMenu, { type MenuItem } from "./ContextMenu.svelte";
  import ChangeImagePopover from "./ChangeImagePopover.svelte";
  import IconPicker from "./IconPicker.svelte";
  import InterfacePicker from "./InterfacePicker.svelte";
  import NodeEditDialog from "./NodeEditDialog.svelte";
  import { uiSvg } from "../icons.svelte";
  import { linking } from "../linking.svelte";
  import { nextFreeInterface } from "../interfaces";
  import type { LabNode } from "../labTypes";

  const nodeTypes: NodeTypes = { iol: IolNode, vpcs: VpcsNode };
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

  // Reconcile lab nodes into the flow-node array WITHOUT clobbering the fields
  // xyflow attaches during measurement (measured / internals / width / height /
  // handles). Rebuilding these from scratch on every store change would wipe the
  // measured dimensions and silently drop all edges (floating edges need
  // measured nodes). So we merge in place, preserving xyflow's managed fields.
  $effect(() => {
    const desired = labStore.lab.nodes.map(toFlowNode);
    // Read the current flow-node array untracked so this effect doesn't loop on
    // its own write (and doesn't re-run when xyflow mutates measured fields).
    const prev = new Map(untrack(() => nodes).map((n) => [n.id, n]));
    nodes = desired.map((d) => {
      const existing = prev.get(d.id);
      if (!existing) return d;
      return {
        ...existing,
        type: d.type,
        position: d.position,
        data: d.data,
        selected: d.selected,
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
      const ln = labStore.lab.nodes.find((n) => n.id === Number(fn.id));
      if (ln) {
        ln.x = fn.position.x;
        ln.y = fn.position.y;
      }
    }
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
  const { screenToFlowPosition, flowToScreenPosition, fitView, zoomIn, zoomOut, setViewport } =
    useSvelteFlow();

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
    return () => {
      linking.start = null;
      linking.requestEdit = null;
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
    const { kind, imageId } = JSON.parse(raw) as { kind: "iol" | "vpcs"; imageId?: string };
    const pos = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    const id = labStore.nextNodeId();
    const img = labStore.images.find((i) => i.id === imageId);
    const node: LabNode =
      kind === "iol"
        ? {
            id,
            kind,
            name: `R${id}`,
            x: pos.x,
            y: pos.y,
            ram: 1024,
            ethernet: 1,
            serial: 1,
            image: img ? { id: img.id, filename: img.filename, class: img.class } : undefined,
          }
        : {
            id,
            kind,
            name: `PC${id}`,
            x: pos.x,
            y: pos.y,
          };
    labStore.addNode(node);
    labStore.selectedNodeId = id;
  }

  // --- context menus / popovers ---
  let nodeMenu = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let linkMenu = $state<{ x: number; y: number; linkId: number } | null>(null);
  let imagePopover = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let iconPicker = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let editDialog = $state<{ nodeId: number } | null>(null);

  function openEdit(nid: number) {
    labStore.selectedNodeId = nid;
    editDialog = { nodeId: nid };
  }

  function onNodeContextMenu({ node, event }: { node: Node; event: MouseEvent }) {
    event.preventDefault();
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
    labStore.selectedNodeId = Number(node.id);
    labStore.selectedLinkId = null;
  }

  function onEdgeClick({ edge }: { edge: Edge }) {
    labStore.selectedLinkId = (edge.data as any)?.linkId as number;
    labStore.selectedNodeId = null;
  }

  function onPaneClick() {
    labStore.selectedNodeId = null;
    labStore.selectedLinkId = null;
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

</script>

<div class="canvas-wrap" bind:this={canvasEl} ondragover={onDragOver} ondrop={onDrop} role="application">
  <SvelteFlow
    bind:nodes
    bind:edges
    {nodeTypes}
    {edgeTypes}
    fitView
    minZoom={0.15}
    maxZoom={2.5}
    connectionMode={ConnectionMode.Loose}
    proOptions={{ hideAttribution: true }}
    onnodedragstop={onNodeDragStop}
    onconnect={onConnect}
    onnodecontextmenu={onNodeContextMenu}
    onedgecontextmenu={onEdgeContextMenu}
    onnodeclick={onNodeClick}
    onedgeclick={onEdgeClick}
    onpaneclick={onPaneClick}
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
    <button class="vc" title="Zoom in" onclick={() => void zoomIn({ duration: 150 })} aria-label="Zoom in">+</button>
    <button class="vc" title="Zoom out" onclick={() => void zoomOut({ duration: 150 })} aria-label="Zoom out">−</button>
    <button class="vc" title="Fit to content" onclick={fitContent} aria-label="Fit to content">{@html uiSvg("fit", 15)}</button>
    <button class="vc" title="Reset view" onclick={resetView} aria-label="Reset view">{@html uiSvg("reset", 15)}</button>
  </div>

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
  .vc :global(svg) {
    width: 15px;
    height: 15px;
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
