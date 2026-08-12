import type { NodePlacement } from "./railUiStore.svelte";

// Shared between App.svelte (drag source, in the "Add Nodes" rail flyout)
// and CanvasInner.svelte (drop target) — native HTML5 drag-and-drop can't
// carry live state through dataTransfer (it's write-only until drop), so a
// held Shift key needs a side channel to update a count badge while the
// drag is still in flight over the canvas.
const NODE_SPACING_PX = 110;
const MAX_COUNT = 20;

class DragNodeCountStore {
  active = $state(false);
  count = $state(1);
  cursor = $state({ x: 0, y: 0 });
  private drag: NodePlacement | null = null;
  private originX = 0;

  begin(drag: NodePlacement, clientX: number, clientY: number) {
    this.drag = drag;
    this.originX = clientX;
    this.active = true;
    this.count = 1;
    this.cursor = { x: clientX, y: clientY };
  }

  /** Called from CanvasInner's ondragover — the only DnD event that fires
   *  repeatedly with a live shiftKey/position while dragging over the drop
   *  target (dragstart's modifier state goes stale the moment the key
   *  changes after the drag begins). */
  update(clientX: number, clientY: number, shiftKey: boolean) {
    if (!this.active) return;
    this.cursor = { x: clientX, y: clientY };
    this.count = shiftKey
      ? Math.min(MAX_COUNT, 1 + Math.floor(Math.abs(clientX - this.originX) / NODE_SPACING_PX))
      : 1;
  }

  /** Consumes the in-flight drag: returns the placement + node count for
   *  the drop handler, then resets. Returns null if nothing was dragging
   *  (e.g. a plain non-node drag landed on the canvas). */
  consume(): { drag: NodePlacement; count: number } | null {
    const drag = this.drag;
    const count = this.count;
    this.reset();
    return drag ? { drag, count } : null;
  }

  reset() {
    this.active = false;
    this.count = 1;
    this.drag = null;
  }
}

export const dragNodeCountStore = new DragNodeCountStore();
export { NODE_SPACING_PX };
