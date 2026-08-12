import type { NodeKind } from "./labTypes";

export type RailPanel = "nodes" | "actions" | "shapes" | "tools";

export interface NodePlacement {
  kind: NodeKind;
  imageId?: string;
  packId?: string;
}

type PlaceNode = (drag: NodePlacement) => void;

class RailUiStore {
  open = $state<RailPanel | null>(null);
  private placeNodeCallback: PlaceNode | null = null;

  toggle(panel: RailPanel) {
    this.open = this.open === panel ? null : panel;
  }

  close() {
    this.open = null;
  }

  bindPlaceNode(callback: PlaceNode) {
    this.placeNodeCallback = callback;
    return () => {
      if (this.placeNodeCallback === callback) this.placeNodeCallback = null;
    };
  }

  placeNode(drag: NodePlacement) {
    this.placeNodeCallback?.(drag);
  }
}

export const railUiStore = new RailUiStore();
