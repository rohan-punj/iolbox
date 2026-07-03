// Excalidraw-style annotation tool state, shared between the Palette DRAW
// cluster (which arms a tool) and CanvasInner (which places the annotation on
// the next pane click). Module-scoped so the palette can signal the canvas
// without prop drilling. Also carries a small fixed colour palette + a request
// to begin inline editing of a freshly-placed annotation.

export type AnnoTool = "text" | "note" | "rect" | "ellipse" | "line";

// Five theme-appropriate colours (readable on both Bench + Glass grounds).
export const ANNO_COLORS = [
  "#4bc6d1", // cable-cyan (accent)
  "#39d98a", // green
  "#f0b429", // amber
  "#ff5a5f", // red
  "#9d8bff", // violet
  "#a7bacf", // slate/neutral
];

export const ANNO_DEFAULT_COLOR = ANNO_COLORS[0];

class AnnoToolState {
  /** Armed tool; the next canvas pane-click places it, then disarms. null = off. */
  active = $state<AnnoTool | null>(null);
  /** Colour applied to the next placed annotation. */
  color = $state<string>(ANNO_DEFAULT_COLOR);
  /** Set to an annotation id by CanvasInner right after placing a text/shape so
   *  its node component starts in inline-edit mode. Cleared once consumed. */
  editRequestId = $state<string | null>(null);
  /** Callback wired by CanvasInner: open the floating style popover for an
   *  annotation near the given client point. focusText focuses the text field
   *  (used for dblclick on text/note so quick inline editing still works). */
  requestStyle: ((annoId: string, clientX: number, clientY: number, focusText: boolean) => void) | null =
    null;

  arm(tool: AnnoTool) {
    // Toggle off if the same tool is clicked again.
    this.active = this.active === tool ? null : tool;
  }
  disarm() {
    this.active = null;
  }
}

export const annoTool = new AnnoToolState();
