<script lang="ts">
  import {
    SvelteFlow,
    Background,
    Controls,
    MiniMap,
    BackgroundVariant,
    ConnectionMode,
    useSvelteFlow,
    type Node,
    type Edge,
    type Connection,
    type NodeTypes,
  } from "@xyflow/svelte";
  import "@xyflow/svelte/dist/style.css";
  import { labStore } from "../labStore.svelte";
  import IolNode from "../nodes/IolNode.svelte";
  import VpcsNode from "../nodes/VpcsNode.svelte";
  import ContextMenu, { type MenuItem } from "./ContextMenu.svelte";
  import ChangeImagePopover from "./ChangeImagePopover.svelte";
  import type { LabNode } from "../labTypes";

  const nodeTypes: NodeTypes = { iol: IolNode, vpcs: VpcsNode };

  function toFlowNode(n: LabNode): Node {
    const img = labStore.images.find((i) => i.id === n.image?.id);
    return {
      id: String(n.id),
      type: n.kind,
      position: { x: n.x, y: n.y },
      data: {
        label: n.name,
        imageClass: img?.class ?? n.image?.class ?? "unknown",
        imageLabel: img?.filename ?? n.image?.filename,
      },
      selected: labStore.selectedNodeId === n.id,
    };
  }

  function toFlowEdge(l: (typeof labStore.lab.links)[number]): Edge {
    const [a, b] = l.endpoints;
    return {
      id: `link-${l.id}`,
      source: String(a?.node ?? 0),
      target: String(b?.node ?? 0),
      label: `${a?.interface ?? ""} — ${b?.interface ?? ""}`,
      selected: labStore.selectedLinkId === l.id,
      style: l.capture?.enabled ? "stroke:var(--accent);stroke-width:2.5" : undefined,
      data: { linkId: l.id },
    };
  }

  let nodes = $state.raw<Node[]>([]);
  let edges = $state.raw<Edge[]>([]);

  $effect(() => {
    nodes = labStore.lab.nodes.map(toFlowNode);
  });
  $effect(() => {
    edges = labStore.lab.links.map(toFlowEdge);
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
    const used = new Set(
      labStore.lab.links
        .flatMap((l) => l.endpoints)
        .filter((e) => e.node === node.id)
        .map((e) => e.interface)
    );
    if (node.kind === "vpcs") return "eth0";
    const adapters = node.ethernet ?? 1;
    for (let a = 0; a < Math.max(adapters, 4); a++) {
      for (let p = 0; p < 4; p++) {
        const iface = `e${a}/${p}`;
        if (!used.has(iface)) return iface;
      }
    }
    return "e0/0";
  }

  // --- drag from palette to create nodes ---
  let canvasEl: HTMLDivElement | undefined = $state();
  const { screenToFlowPosition } = useSvelteFlow();

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
            ram: 256,
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
  }

  // --- context menus ---
  let nodeMenu = $state<{ x: number; y: number; nodeId: number } | null>(null);
  let linkMenu = $state<{ x: number; y: number; linkId: number } | null>(null);
  let imagePopover = $state<{ x: number; y: number; nodeId: number } | null>(null);

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

  // Built on demand at render time (not via $derived) so they never read
  // reactive menu state after the menu closes.
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
        label: "Change image…",
        action: () => {
          imagePopover = { x: menu.x, y: menu.y, nodeId: nid };
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
    return [
      {
        label: "Capture in Wireshark",
        action: () => {
          const link = labStore.lab.links.find((l) => l.id === menu.linkId);
          if (link) {
            link.capture = { enabled: true, mode: "live" };
            void labStore.client.captureStart(labStore.lab.id, link.id);
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
    fitView
    minZoom={0.2}
    maxZoom={2}
    connectionMode={ConnectionMode.Loose}
    proOptions={{ hideAttribution: true }}
    onnodedragstop={onNodeDragStop}
    onconnect={onConnect}
    onnodecontextmenu={onNodeContextMenu}
    onedgecontextmenu={onEdgeContextMenu}
    onnodeclick={onNodeClick}
    onedgeclick={onEdgeClick}
    onpaneclick={onPaneClick}
    colorMode="dark"
  >
    <Background variant={BackgroundVariant.Dots} gap={20} size={1} patternColor="var(--border)" />
    <Controls showLock={false} />
    <MiniMap
      nodeColor={() => "var(--accent)"}
      maskColor="rgba(13,17,23,0.75)"
      style="background:var(--bg-2);border:1px solid var(--border);"
    />
  </SvelteFlow>

  {#if labStore.lab.nodes.length === 0}
    <div class="empty-state">
      <div class="empty-title">Empty canvas</div>
      <div class="empty-sub">Drag a node from the palette to get started.</div>
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
</div>

<style>
  .canvas-wrap {
    position: relative;
    width: 100%;
    height: 100%;
    background: var(--bg-0);
  }
  :global(.svelte-flow) {
    background: var(--bg-0);
  }
  :global(.svelte-flow__edge-path) {
    stroke: var(--border-strong);
    stroke-width: 1.75;
  }
  :global(.svelte-flow__edge.selected .svelte-flow__edge-path) {
    stroke: var(--accent);
  }
  :global(.svelte-flow__edge-text) {
    fill: var(--text-tertiary);
    font-size: 10px;
  }
  :global(.svelte-flow__edge-textbg) {
    fill: var(--bg-1);
  }
  :global(.svelte-flow__controls) {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    box-shadow: var(--shadow-md);
  }
  :global(.svelte-flow__controls-button) {
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
    fill: var(--text-secondary);
  }
  :global(.svelte-flow__controls-button:hover) {
    background: var(--bg-hover);
  }
  :global(.svelte-flow__minimap) {
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-md);
  }
  :global(.svelte-flow__attribution) {
    display: none;
  }
  .empty-state {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    pointer-events: none;
    gap: 4px;
  }
  .empty-title {
    font-size: var(--fs-lg);
    color: var(--text-secondary);
    font-weight: 600;
  }
  .empty-sub {
    font-size: var(--fs-sm);
    color: var(--text-tertiary);
  }
</style>
