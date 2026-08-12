import { chromeStore } from "./chromeStore.svelte";

export interface DragSpec {
  start: { x: number; y: number };
  clamp: (x: number, y: number) => { x: number; y: number };
  onMove: (x: number, y: number) => void;
  onEnd: (x: number, y: number) => void;
}

export interface DragHandle {
  move(e: PointerEvent): void;
  end(e: PointerEvent): void;
}

/** Start a pointer-capture drag with all state isolated to the returned handle. */
export function beginDrag(e: PointerEvent, spec: DragSpec): DragHandle | null {
  if (e.button !== 0) return null;
  const element = e.currentTarget as HTMLElement | null;
  if (!element) return null;
  const downX = e.clientX;
  const downY = e.clientY;
  let done = false;

  const valueFor = (event: PointerEvent) =>
    spec.clamp(
      spec.start.x + event.clientX - downX,
      spec.start.y + event.clientY - downY
    );

  element.setPointerCapture(e.pointerId);
  e.preventDefault();
  const releaseHold = chromeStore.hold();

  return {
    move(event) {
      if (done) return;
      const value = valueFor(event);
      spec.onMove(value.x, value.y);
    },
    end(event) {
      if (done) return;
      done = true;
      element.releasePointerCapture?.(event.pointerId);
      const value = valueFor(event);
      try { spec.onEnd(value.x, value.y); } finally { releaseHold(); }
    },
  };
}
