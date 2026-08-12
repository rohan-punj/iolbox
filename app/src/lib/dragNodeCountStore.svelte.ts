import type { NodePlacement } from "./railUiStore.svelte";

// Shared between App.svelte (drag source, in the "Add Nodes" rail flyout)
// and CanvasInner.svelte (drop target) — native HTML5 drag-and-drop can't
// carry live state through dataTransfer (it's write-only until drop), so a
// held Shift key needs a side channel to reach the drop handler while the
// drag is still in flight over the canvas.
const NODE_SPACING_PX = 110;

class DragNodeCountStore {
  active = $state(false);
  shiftHeld = $state(false);
  cursor = $state({ x: 0, y: 0 });
  private drag: NodePlacement | null = null;

  begin(drag: NodePlacement, clientX: number, clientY: number) {
    this.drag = drag;
    this.active = true;
    this.shiftHeld = false;
    this.cursor = { x: clientX, y: clientY };
  }

  /** Called from CanvasInner's ondragover — the only DnD event that fires
   *  repeatedly with a live shiftKey/position while dragging over the drop
   *  target (dragstart's modifier state goes stale the moment the key
   *  changes after the drag begins). */
  update(clientX: number, clientY: number, shiftKey: boolean) {
    if (!this.active) return;
    this.cursor = { x: clientX, y: clientY };
    this.shiftHeld = shiftKey;
  }

  /** Consumes the in-flight drag: returns the placement + whether Shift was
   *  held at drop time for the drop handler, then resets. Returns null if
   *  nothing was dragging (e.g. a plain non-node drag landed on the canvas). */
  consume(): { drag: NodePlacement; shiftHeld: boolean } | null {
    const drag = this.drag;
    const shiftHeld = this.shiftHeld;
    this.reset();
    return drag ? { drag, shiftHeld } : null;
  }

  reset() {
    this.active = false;
    this.shiftHeld = false;
    this.drag = null;
  }
}

export const dragNodeCountStore = new DragNodeCountStore();
export { NODE_SPACING_PX };
