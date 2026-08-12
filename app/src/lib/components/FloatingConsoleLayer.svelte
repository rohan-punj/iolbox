<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import {
    WIN_DEFAULT_H,
    WIN_DEFAULT_W,
    cascadeGeom,
    clampGeom,
    consoleUiStore,
    paneKey,
    restoreGeom,
    type PaneRef,
    type Viewport,
  } from "../consoleUiStore.svelte";
  import { nodeName } from "../paneLabels";
  import FloatingConsoleWindow from "./FloatingConsoleWindow.svelte";

  const TOPBAR_H = 48;
  const NODE_W = 64;
  const NODE_H = 88;
  const NODE_AVOID_GAP = 48;
  type ScreenPoint = { x: number; y: number };
  type ScreenRect = { x: number; y: number; w: number; h: number };
  type FlowProjector = (x: number, y: number) => ScreenPoint;

  let winW = $state(typeof window !== "undefined" ? window.innerWidth : 1280);
  let winH = $state(typeof window !== "undefined" ? window.innerHeight : 800);
  const viewport = $derived<Viewport>({ w: winW, h: winH, topbarH: TOPBAR_H });

  const panes = $derived<PaneRef[]>([
    ...labStore.openConsoleTabs.map((node) => ({ kind: "console" as const, node })),
    ...labStore.openCaptureTabs.map((link) => ({ kind: "capture" as const, link })),
    ...labStore.openLensTabs.map((link) => ({ kind: "lens" as const, link })),
  ]);

  function windowZ(index: number): string {
    return `calc(var(--z-float) + ${Math.min(index, 99)})`;
  }
  const paneMap = $derived(new Map(panes.map((ref) => [paneKey(ref), ref])));
  const orderedPanes = $derived(
    consoleUiStore.windowOrder
      .map((key) => paneMap.get(key))
      .filter((ref): ref is PaneRef => ref !== undefined)
  );
  const minimizedPanes = $derived(
    consoleUiStore.minimized
      .map((key) => paneMap.get(key))
      .filter((ref): ref is PaneRef => ref !== undefined)
  );

  function flowProjector(): FlowProjector | null {
    return (labStore as unknown as { flowToScreen?: FlowProjector | null }).flowToScreen ?? null;
  }

  function intersects(a: ScreenRect, b: ScreenRect): boolean {
    return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;
  }

  function inflate(rect: ScreenRect, amount: number): ScreenRect {
    return {
      x: rect.x - amount,
      y: rect.y - amount,
      w: rect.w + amount * 2,
      h: rect.h + amount * 2,
    };
  }

  function selectedNodeRect(): ScreenRect | null {
    const selectedId = labStore.selectedNodeId;
    const projector = flowProjector();
    if (selectedId === null || !projector) return null;
    const node = labStore.lab.nodes.find((item) => item.id === selectedId);
    if (!node) return null;
    const topLeft = projector(node.x, node.y);
    const bottomRight = projector(node.x + NODE_W, node.y + NODE_H);
    const x = Math.min(topLeft.x, bottomRight.x);
    const y = Math.min(topLeft.y, bottomRight.y);
    return inflate(
      { x, y, w: Math.abs(bottomRight.x - topLeft.x), h: Math.abs(bottomRight.y - topLeft.y) },
      NODE_AVOID_GAP
    );
  }

  function centerRect(vp: Viewport): ScreenRect {
    const usableH = Math.max(0, vp.h - vp.topbarH);
    return {
      x: vp.w * 0.3,
      y: vp.topbarH + usableH * 0.3,
      w: vp.w * 0.4,
      h: usableH * 0.4,
    };
  }

  function visibleWindowRects(): ScreenRect[] {
    return orderedPanes
      .filter((ref) => !consoleUiStore.minimized.includes(paneKey(ref)))
      .map((ref) => consoleUiStore.windows[paneKey(ref)])
      .filter((geom): geom is NonNullable<typeof geom> => geom !== undefined)
      .map((geom) => ({ x: geom.x, y: geom.y, w: geom.w, h: geom.h }));
  }

  /** Deterministic three-step placement policy for a newly opened pane. */
  function placementFor(ref: PaneRef, index: number, vp: Viewport) {
    const key = paneKey(ref);
    const restored = restoreGeom(labStore.lab.id, key, vp);
    if (restored) return restored;

    const avoid = [centerRect(vp)];
    const selected = selectedNodeRect();
    if (selected) avoid.unshift(selected);
    const existing = visibleWindowRects();
    const startX = Math.max(0, vp.w - WIN_DEFAULT_W - 24);
    const startY = Math.max(vp.topbarH, vp.h - WIN_DEFAULT_H - 24);
    for (let candidate = 0; candidate < 64; candidate += 1) {
      const col = candidate % 8;
      const row = Math.floor(candidate / 8);
      const geom = clampGeom(
        { x: startX - col * 24, y: startY - row * 24, w: WIN_DEFAULT_W, h: WIN_DEFAULT_H },
        vp
      );
      const rect = { x: geom.x, y: geom.y, w: geom.w, h: geom.h };
      if (avoid.every((target) => !intersects(rect, target)) && existing.every((target) => !intersects(rect, target))) {
        return geom;
      }
    }
    return cascadeGeom(index, vp);
  }

  $effect(() => {
    if (consoleUiStore.placement !== "float") return;
    const vp = viewport;
    const open = panes;
    for (let index = 0; index < open.length; index += 1) {
      const ref = open[index];
      const key = paneKey(ref);
      if (!consoleUiStore.windows[key]) {
        consoleUiStore.ensureWindow(ref, placementFor(ref, index, vp));
      }
    }
  });

  $effect(() => {
    const vp = viewport;
    consoleUiStore.clampAllWindows(vp);
  });
</script>

<svelte:window bind:innerWidth={winW} bind:innerHeight={winH} />

<div class="floating-layer" aria-label="Floating console windows">
  {#each orderedPanes as ref (paneKey(ref))}
    {@const key = paneKey(ref)}
    <FloatingConsoleWindow
      {ref}
      labId={labStore.lab.id}
      {viewport}
      z={windowZ(consoleUiStore.windowOrder.indexOf(key))}
    />
  {/each}

  {#if minimizedPanes.length > 0 || consoleUiStore.placement === "float"}
    <div class="launcher" aria-label="Minimized consoles">
      {#each minimizedPanes as ref (paneKey(ref))}
        {@const key = paneKey(ref)}
        <button class="launcher-chip" onclick={() => consoleUiStore.restoreWindow(key)} title={`Restore ${key}`}>
          <span class="launcher-led" aria-hidden="true"></span>
          {#if ref.kind === "console"}
            <span>Console</span>
            <span class="mono">{nodeName(ref.node)}</span>
          {:else if ref.kind === "capture"}
            <span>Capture</span>
            <span class="mono">{ref.link}</span>
          {:else}
            <span>Lens</span>
            <span class="mono">{ref.link}</span>
          {/if}
        </button>
      {/each}
      <button class="return-dock" onclick={() => consoleUiStore.setPlacement("dock")} title="Return consoles to the dock">
        Return to dock
      </button>
    </div>
  {/if}
</div>

<style>
  .floating-layer {
    position: fixed;
    inset: 0;
    z-index: var(--z-float);
    pointer-events: none;
  }
  .floating-layer :global(.float-win) {
    pointer-events: auto;
  }
  .launcher {
    position: fixed;
    left: 64px;
    bottom: 34px;
    z-index: calc(var(--z-float) + 99);
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    max-width: calc(100vw - 80px);
    padding: var(--sp-1);
    pointer-events: auto;
    background: var(--panel);
    -webkit-backdrop-filter: var(--blur);
    backdrop-filter: var(--blur);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-sm);
  }
  .launcher-chip,
  .return-dock {
    all: unset;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    min-height: 26px;
    padding: 4px 8px;
    color: var(--text-secondary);
    font: 600 var(--fs-xs)/1 var(--font-ui);
    border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .launcher-chip:hover,
  .launcher-chip:focus-visible,
  .return-dock:hover,
  .return-dock:focus-visible {
    color: var(--text-primary);
    background: var(--bg-hover);
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }
  .launcher-led {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--state-starting);
  }
  .return-dock {
    color: var(--accent);
    border-left: 1px solid var(--border);
    border-radius: 0;
  }
</style>
