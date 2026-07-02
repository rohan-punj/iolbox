// R2.1 — PNetLab-style link-add drag state, shared between the node connector
// affordance (which starts the drag) and CanvasInner (which owns the rubber-band
// overlay + hit-testing + Interface Picker). Kept module-scoped so the connector
// button inside a custom node can signal the canvas without prop drilling through
// xyflow's node renderer.

type StartFn = (nodeId: number, ev: PointerEvent) => void;

class LinkingState {
  /** Registered by CanvasInner; the connector calls it on pointerdown. */
  start: StartFn | null = null;
  /** Registered by CanvasInner; a node's double-click calls it to open Edit. */
  requestEdit: ((nodeId: number) => void) | null = null;
  /** Node currently being hovered as a drop target (for the accent ring). */
  dropTargetId = $state<number | null>(null);
  /** Source node id while a link drag is in progress (connector stays lit). */
  sourceId = $state<number | null>(null);
}

export const linking = new LinkingState();
