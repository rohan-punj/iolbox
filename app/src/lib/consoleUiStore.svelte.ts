// Console-dock UI preferences (dock side + colorizer on/off). Rune-backed
// singleton, persisted to localStorage — same idiom as themeStore. Kept out of
// the runtime store because these are pure view prefs, not lab/runtime state.

export type DockSide = "bottom" | "right";
export type PaneRef =
  | { kind: "console"; node: number }
  | { kind: "capture"; link: number }
  | { kind: "lens"; link: number };
export type ConsoleLayout = "tabs" | "tile2" | "tile3" | "tile4";
export type ConsolePlacement = "dock" | "float";

export interface WindowGeom {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface Viewport {
  w: number;
  h: number;
  topbarH: number;
}

const PLACEMENT_KEY = "iolbox.console.placement";
const GEOM_KEY = "iolbox.console.windows";

export const WIN_MIN_W = 320;
export const WIN_MIN_H = 160;
export const WIN_DEFAULT_W = 560;
export const WIN_DEFAULT_H = 320;
export const WIN_TITLE_H = 28;
export const WIN_KEEP_VISIBLE = 120;

function initialPlacement(): ConsolePlacement {
  try {
    const saved = localStorage.getItem(PLACEMENT_KEY);
    if (saved === "dock" || saved === "float") return saved;
  } catch {
    /* localStorage may be unavailable */
  }
  return "float";
}

function clampValue(value: number, min: number, max: number): number {
  if (min > max) return min;
  return Math.min(max, Math.max(min, value));
}

/** Keep the title bar reachable while preserving the caller's finished size. */
export function clampGeom(g: WindowGeom, vp: Viewport): WindowGeom {
  const w = Math.max(WIN_MIN_W, Number.isFinite(g.w) ? g.w : WIN_DEFAULT_W);
  const h = Math.max(WIN_MIN_H, Number.isFinite(g.h) ? g.h : WIN_DEFAULT_H);
  const x = Number.isFinite(g.x) ? g.x : 0;
  const y = Number.isFinite(g.y) ? g.y : vp.topbarH;
  return {
    x: clampValue(x, WIN_KEEP_VISIBLE - w, vp.w - WIN_KEEP_VISIBLE),
    y: clampValue(y, vp.topbarH, vp.h - WIN_TITLE_H),
    w,
    h,
  };
}

/** Restore one persisted geometry entry, clamping it for the current viewport. */
export function restoreGeom(labId: string, key: string, vp: Viewport): WindowGeom | null {
  try {
    const raw = localStorage.getItem(GEOM_KEY);
    if (!raw) return null;
    const saved = JSON.parse(raw) as Record<string, WindowGeom>;
    const value = saved[`${labId}|${key}`];
    if (!value || ![value.x, value.y, value.w, value.h].every(Number.isFinite)) return null;
    return clampGeom(value, vp);
  } catch {
    return null;
  }
}

/** Deterministic fallback cascade, already clamped for the current viewport. */
export function cascadeGeom(index: number, vp: Viewport): WindowGeom {
  const offset = 24 * (Math.max(0, index) % 8);
  return clampGeom({ x: offset, y: vp.topbarH + offset, w: WIN_DEFAULT_W, h: WIN_DEFAULT_H }, vp);
}

/** Session-only marker shared by terminal renderers and the future Lens pane.
 * `wall` is for human-readable ordering only; capture placement uses the
 * per-link delivery sequence in `capturePos`, never a clock comparison. */
export interface ConsoleMark {
  id: number;
  wall: number;
  label: string;
  capturePos: Record<number, number>;
}

export function paneKey(ref: PaneRef): string {
  return ref.kind === "console" ? `console:${ref.node}` : `${ref.kind}:${ref.link}`;
}

export function samePane(a: PaneRef | null, b: PaneRef | null): boolean {
  return a !== null && b !== null && paneKey(a) === paneKey(b);
}
/** How the node-hover Console button (and context-menu Console) opens a console:
 *  "web" = an in-app web console tab; "native" = hand off to the OS telnet
 *  client via the telnet:// scheme. Global, not per-node. */
export type ConsoleMode = "web" | "native";

const SIDE_KEY = "iolbox.console.dockSide";
const COLOR_KEY = "iolbox.console.colorize";
const MODE_KEY = "iolbox.console.mode";
const FONT_KEY = "iolbox.console.fontSize";
const LAYOUT_KEY = "iolbox.console.layout";

/** Console terminal font size bounds + default. 15 (not xterm's tiny default)
 *  reads comfortably on a HiDPI laptop; the A-/A+ control walks this range. */
export const FONT_MIN = 9;
export const FONT_MAX = 24;
export const FONT_DEFAULT = 15;

function clampFont(n: number): number {
  if (!Number.isFinite(n)) return FONT_DEFAULT;
  return Math.min(FONT_MAX, Math.max(FONT_MIN, Math.round(n)));
}

function initialFontSize(): number {
  try {
    const saved = localStorage.getItem(FONT_KEY);
    if (saved != null) return clampFont(Number(saved));
  } catch {
    /* localStorage may be unavailable */
  }
  return FONT_DEFAULT;
}

function initialSide(): DockSide {
  try {
    const saved = localStorage.getItem(SIDE_KEY);
    if (saved === "bottom" || saved === "right") return saved;
  } catch {
    /* localStorage may be unavailable (private mode / SSR) */
  }
  return "bottom";
}

function initialColorize(): boolean {
  try {
    // Default ON; only an explicit "0" disables it.
    return localStorage.getItem(COLOR_KEY) !== "0";
  } catch {
    return true;
  }
}

function initialMode(): ConsoleMode {
  try {
    return localStorage.getItem(MODE_KEY) === "native" ? "native" : "web";
  } catch {
    return "web";
  }
}

function initialLayout(): ConsoleLayout {
  try {
    const saved = localStorage.getItem(LAYOUT_KEY);
    if (saved === "tabs" || saved === "tile2" || saved === "tile3" || saved === "tile4") {
      return saved;
    }
  } catch {
    /* localStorage may be unavailable */
  }
  return "tabs";
}

class ConsoleUiStore {
  dockSide = $state<DockSide>("bottom");
  placement = $state<ConsolePlacement>("float");
  colorize = $state(true);
  /** Global console-open mode (web tab vs native telnet client). Default web. */
  consoleMode = $state<ConsoleMode>("web");
  /** Terminal font size (px), shared by every console terminal, persisted. */
  fontSize = $state(FONT_DEFAULT);
  /** Layout is a global view preference; pane membership is deliberately not persisted. */
  layout = $state<ConsoleLayout>("tabs");
  /** Session-only pane refs. They must not be persisted across lab documents. */
  tiles = $state<PaneRef[]>([]);
  focused = $state<PaneRef | null>(null);
  pinned = $state<PaneRef | null>(null);
  /** Live geometry and order are session membership; only geometry snapshots persist. */
  windows = $state<Record<string, WindowGeom>>({});
  windowOrder = $state<string[]>([]);
  minimized = $state<string[]>([]);
  /** Float-mode keep-on-top state. Deliberately distinct from dock `pinned`. */
  pinnedWindows = $state<string[]>([]);
  nativeCapture = $state<Record<number, boolean>>({});
  searchOpenFor = $state<number | null>(null);
  marks = $state<ConsoleMark[]>([]);
  /** link id -> sequence assigned to the next packet delivered for that link. */
  captureDelivered = $state<Record<number, number>>({});

  // These fields intentionally stay plain: the callback breaks the import cycle
  // that would result from importing the runtime store here, and the shadow prevents the
  // App effect from echoing our own active-console write back into focus state.
  private lastSeenActiveConsole: number | null = null;
  private onSelectConsole: ((nodeId: number) => void) | null = null;
  private onClosePane: ((ref: PaneRef) => { nextCapture?: number; nextConsole?: number }) | null = null;
  private lastLabId: string | null = null;
  private focusRecency = new Map<string, number>();
  private focusClock = 0;
  private nextMarkId = 1;

  constructor() {
    this.dockSide = initialSide();
    this.placement = initialPlacement();
    this.colorize = initialColorize();
    this.consoleMode = initialMode();
    this.fontSize = initialFontSize();
    this.layout = initialLayout();
  }

  private capacity(): number {
    if (this.layout === "tile2") return 2;
    if (this.layout === "tile3") return 3;
    if (this.layout === "tile4") return 4;
    return 0;
  }

  private noteFocus(ref: PaneRef) {
    this.focusClock += 1;
    this.focusRecency.set(paneKey(ref), this.focusClock);
  }

  private trimTiles() {
    const max = this.capacity();
    if (max === 0 || this.tiles.length <= max) return;
    const kept = [...this.tiles];
    while (kept.length > max) {
      const candidates = kept.filter((ref) => !samePane(ref, this.pinned));
      if (candidates.length === 0) break;
      const evicted = candidates.reduce((oldest, ref) => {
        const oldTime = this.focusRecency.get(paneKey(oldest)) ?? 0;
        const refTime = this.focusRecency.get(paneKey(ref)) ?? 0;
        return refTime < oldTime ? ref : oldest;
      });
      const index = kept.findIndex((ref) => samePane(ref, evicted));
      if (index >= 0) kept.splice(index, 1);
      else break;
    }
    this.tiles = kept;
  }

  /** Add a pane to the current tiled layout, evicting the least-recently-focused
   * non-pinned pane when the layout is full. */
  ensureTiled(ref: PaneRef) {
    if (this.layout === "tabs") return;
    if (ref.kind !== "console" && ref.kind !== "capture" && ref.kind !== "lens") return;
    const next = [...this.tiles];
    let changed = false;
    if (this.pinned && !next.some((item) => samePane(item, this.pinned))) {
      next.unshift(this.pinned);
      changed = true;
    }
    if (!next.some((item) => samePane(item, ref))) {
      next.push(ref);
      changed = true;
    }
    this.noteFocus(ref);
    if (changed) this.tiles = next;
    this.trimTiles();
  }

  bindConsoleSelect(fn: (nodeId: number) => void) {
    this.onSelectConsole = fn;
  }

  bindPaneClose(fn: (ref: PaneRef) => { nextCapture?: number; nextConsole?: number }) {
    this.onClosePane = fn;
  }

  setPlacement(placement: ConsolePlacement) {
    this.placement = placement;
    try {
      localStorage.setItem(PLACEMENT_KEY, placement);
    } catch {
      /* ignore persistence failure */
    }
  }

  togglePlacement() {
    this.setPlacement(this.placement === "dock" ? "float" : "dock");
  }

  ensureWindow(ref: PaneRef, geom: WindowGeom) {
    const key = paneKey(ref);
    if (!this.windows[key]) this.windows = { ...this.windows, [key]: geom };
    if (!this.windowOrder.includes(key)) this.windowOrder = [...this.windowOrder, key];
  }

  moveWindow(key: string, x: number, y: number) {
    const current = this.windows[key];
    if (!current) return;
    this.windows = { ...this.windows, [key]: { ...current, x, y } };
  }

  resizeWindow(key: string, w: number, h: number) {
    const current = this.windows[key];
    if (!current) return;
    this.windows = { ...this.windows, [key]: { ...current, w, h } };
  }

  commitWindow(labId: string, key: string) {
    const geom = this.windows[key];
    if (!geom) return;
    try {
      const raw = localStorage.getItem(GEOM_KEY);
      const saved = raw ? (JSON.parse(raw) as Record<string, WindowGeom>) : {};
      const entry = `${labId}|${key}`;
      delete saved[entry];
      saved[entry] = geom;
      const entries = Object.entries(saved).slice(-200);
      localStorage.setItem(GEOM_KEY, JSON.stringify(Object.fromEntries(entries)));
    } catch {
      /* ignore persistence failure */
    }
  }

  clampAllWindows(vp: Viewport) {
    const next: Record<string, WindowGeom> = {};
    let changed = false;
    for (const [key, geom] of Object.entries(this.windows)) {
      const clamped = clampGeom(geom, vp);
      next[key] = clamped;
      if (clamped.x !== geom.x || clamped.y !== geom.y || clamped.w !== geom.w || clamped.h !== geom.h) {
        changed = true;
      }
    }
    if (changed) this.windows = next;
  }

  private partitionWindowOrder(order: string[]): string[] {
    const pinned = new Set(this.pinnedWindows);
    return [...order.filter((key) => !pinned.has(key)), ...order.filter((key) => pinned.has(key))];
  }

  raiseWindow(key: string) {
    const next = this.windowOrder.filter((item) => item !== key);
    if (this.windows[key] && !next.includes(key)) next.push(key);
    this.windowOrder = this.partitionWindowOrder(next);
  }

  toggleMinimized(key: string) {
    this.minimized = this.minimized.includes(key)
      ? this.minimized.filter((item) => item !== key)
      : [...this.minimized, key];
  }

  restoreWindow(key: string) {
    this.minimized = this.minimized.filter((item) => item !== key);
    this.raiseWindow(key);
  }

  togglePinnedWindow(key: string) {
    this.pinnedWindows = this.pinnedWindows.includes(key)
      ? this.pinnedWindows.filter((item) => item !== key)
      : [...this.pinnedWindows, key];
    this.windowOrder = this.partitionWindowOrder(this.windowOrder);
  }

  isWindowPinned(key: string): boolean {
    return this.pinnedWindows.includes(key);
  }

  setNativeCapture(linkId: number, on: boolean) {
    if (this.nativeCapture[linkId] === on) return;
    this.nativeCapture = { ...this.nativeCapture, [linkId]: on };
  }

  toggleNativeCapture(linkId: number) {
    this.setNativeCapture(linkId, !this.nativeCapture[linkId]);
  }

  setSearchOpenFor(nodeId: number | null) {
    this.searchOpenFor = nodeId;
  }

  toggleSearchOpenFor(nodeId: number) {
    this.setSearchOpenFor(this.searchOpenFor === nodeId ? null : nodeId);
  }

  closePane(ref: PaneRef) {
    const wasFocused =
      samePane(this.focused, ref) ||
      (ref.kind === "capture" && samePane(this.focused, { kind: "lens", link: ref.link }));
    if (ref.kind === "console" && this.searchOpenFor === ref.node) this.searchOpenFor = null;
    const result = this.onClosePane?.(ref);
    if (ref.kind === "capture") this.setNativeCapture(ref.link, false);
    if (!wasFocused) return;
    if (result?.nextCapture !== undefined) {
      this.setFocused({ kind: "capture", link: result.nextCapture });
    } else if (result?.nextConsole !== undefined) {
      this.setFocused({ kind: "console", node: result.nextConsole });
    } else {
      this.setFocused(null);
    }
  }

  setFocused(ref: PaneRef | null) {
    this.focused = ref;
    if (!ref) return;
    this.noteFocus(ref);
    if (ref.kind === "console") {
      this.lastSeenActiveConsole = ref.node;
      this.onSelectConsole?.(ref.node);
    }
    if (this.layout !== "tabs") this.ensureTiled(ref);
  }

  /** Adopt a console selected outside the dock, without echoing our own write. */
  syncFromLabStore(active: number | null) {
    if (active === this.lastSeenActiveConsole) return;
    this.lastSeenActiveConsole = active;
    if (active === null) return;
    const ref: PaneRef = { kind: "console", node: active };
    this.focused = ref;
    this.noteFocus(ref);
    if (this.layout !== "tabs") this.ensureTiled(ref);
  }

  setLayout(layout: ConsoleLayout) {
    this.layout = layout;
    try {
      localStorage.setItem(LAYOUT_KEY, layout);
    } catch {
      /* ignore persistence failure */
    }
    if (layout !== "tabs") {
      if (this.pinned && !this.tiles.some((ref) => samePane(ref, this.pinned))) {
        this.tiles = [this.pinned, ...this.tiles];
      }
      this.trimTiles();
      if (this.focused) this.ensureTiled(this.focused);
    }
  }

  toggleTile(ref: PaneRef) {
    if (this.layout === "tabs") return;
    if (this.tiles.some((item) => samePane(item, ref))) {
      if (!samePane(ref, this.pinned)) {
        this.tiles = this.tiles.filter((item) => !samePane(item, ref));
      }
      return;
    }
    this.ensureTiled(ref);
  }

  setPinned(ref: PaneRef | null) {
    this.pinned = ref;
    if (!ref || this.layout === "tabs") return;
    this.tiles = this.tiles.filter((item) => !samePane(item, ref));
    this.tiles = [ref, ...this.tiles];
    this.trimTiles();
  }

  togglePinned(ref: PaneRef) {
    this.setPinned(samePane(this.pinned, ref) ? null : ref);
  }

  /** Add a session mark with a stream position snapshot for every link that has
   * delivered packets. The counters survive reconnects and reset on close via
   * reconcile(), so Batch 7 can place a divider without a shared clock. */
  addMark(capturePos: Record<number, number>): ConsoleMark {
    const wall = Date.now();
    const mark: ConsoleMark = {
      id: this.nextMarkId++,
      wall,
      label: `Mark ${this.nextMarkId - 1} · ${new Date(wall).toLocaleTimeString()}`,
      capturePos: { ...capturePos },
    };
    this.marks = [...this.marks, mark].slice(-50);
    return mark;
  }

  /** Advance once per delivered packet batch; returns the first packet's seq. */
  advanceCaptureDelivery(linkId: number, n: number): number {
    const first = this.captureDelivered[linkId] ?? 0;
    this.captureDelivered = {
      ...this.captureDelivered,
      [linkId]: first + Math.max(0, n),
    };
    return first;
  }

  private isOpen(ref: PaneRef, consoles: Set<number>, captures: Set<number>, lenses: Set<number>): boolean {
    if (ref.kind === "console") return consoles.has(ref.node);
    if (ref.kind === "capture") return captures.has(ref.link);
    if (ref.kind === "lens") return lenses.has(ref.link);
    return false;
  }

  /** Own all pane/mark cleanup here so runtime state remains untouched. */
  reconcile(labId: string, consoles: number[], captures: number[], lenses: number[]) {
    if (this.lastLabId !== labId) {
      this.lastLabId = labId;
      this.tiles = [];
      this.focused = null;
      this.pinned = null;
      this.windows = {};
      this.windowOrder = [];
      this.minimized = [];
      this.pinnedWindows = [];
      this.nativeCapture = {};
      this.searchOpenFor = null;
      this.marks = [];
      this.captureDelivered = {};
      this.lastSeenActiveConsole = null;
      this.focusRecency.clear();
      this.focusClock = 0;
      this.nextMarkId = 1;
    }

    const consoleSet = new Set(consoles);
    const captureSet = new Set(captures);
    const lensSet = new Set(lenses);
    const nextTiles = this.tiles.filter((ref) => this.isOpen(ref, consoleSet, captureSet, lensSet));
    if (nextTiles.length !== this.tiles.length) this.tiles = nextTiles;
    if (this.focused && !this.isOpen(this.focused, consoleSet, captureSet, lensSet)) this.focused = null;
    if (this.pinned && !this.isOpen(this.pinned, consoleSet, captureSet, lensSet)) this.pinned = null;

    const openKeys = new Set<string>([
      ...consoles.map((node) => paneKey({ kind: "console", node })),
      ...captures.map((link) => paneKey({ kind: "capture", link })),
      ...lenses.map((link) => paneKey({ kind: "lens", link })),
    ]);
    const nextWindows = Object.fromEntries(
      Object.entries(this.windows).filter(([key]) => openKeys.has(key))
    );
    if (Object.keys(nextWindows).length !== Object.keys(this.windows).length) this.windows = nextWindows;
    const nextOrder = this.windowOrder.filter((key) => openKeys.has(key));
    if (nextOrder.length !== this.windowOrder.length) this.windowOrder = nextOrder;
    const nextMinimized = this.minimized.filter((key) => openKeys.has(key));
    if (nextMinimized.length !== this.minimized.length) this.minimized = nextMinimized;
    const nextPinnedWindows = this.pinnedWindows.filter((key) => openKeys.has(key));
    if (nextPinnedWindows.length !== this.pinnedWindows.length) this.pinnedWindows = nextPinnedWindows;

    const nextDelivered: Record<number, number> = {};
    for (const [key, value] of Object.entries(this.captureDelivered)) {
      if (captureSet.has(Number(key))) nextDelivered[Number(key)] = value;
    }
    const deliveredKeys = Object.keys(this.captureDelivered);
    const nextDeliveredKeys = Object.keys(nextDelivered);
    const deliveredChanged =
      deliveredKeys.length !== nextDeliveredKeys.length ||
      nextDeliveredKeys.some((key) => this.captureDelivered[Number(key)] !== nextDelivered[Number(key)]);
    if (deliveredChanged) this.captureDelivered = nextDelivered;

    const nextNativeCapture: Record<number, boolean> = {};
    for (const [key, value] of Object.entries(this.nativeCapture)) {
      if (captureSet.has(Number(key))) nextNativeCapture[Number(key)] = value;
    }
    if (Object.keys(nextNativeCapture).length !== Object.keys(this.nativeCapture).length) {
      this.nativeCapture = nextNativeCapture;
    }
    if (this.searchOpenFor !== null && !consoleSet.has(this.searchOpenFor)) this.searchOpenFor = null;

    const nextMarks = this.marks
      .map((mark) => {
        const capturePos: Record<number, number> = {};
        for (const [key, value] of Object.entries(mark.capturePos)) {
          if (captureSet.has(Number(key))) capturePos[Number(key)] = value;
        }
        return Object.keys(capturePos).length > 0 || consoles.length > 0
          ? { ...mark, capturePos }
          : null;
      })
      .filter((mark): mark is ConsoleMark => mark !== null);
    const marksChanged =
      nextMarks.length !== this.marks.length ||
      nextMarks.some((mark, index) => {
        const current = this.marks[index];
        if (!current || current.id !== mark.id) return true;
        const currentKeys = Object.keys(current.capturePos);
        const nextKeys = Object.keys(mark.capturePos);
        return (
          currentKeys.length !== nextKeys.length ||
          nextKeys.some((key) => current.capturePos[Number(key)] !== mark.capturePos[Number(key)])
        );
      });
    if (marksChanged) this.marks = nextMarks;

    if (!this.focused) {
      const firstConsole = consoles[0];
      const firstCapture = captures[0];
      if (firstConsole !== undefined) this.focused = { kind: "console", node: firstConsole };
      else if (firstCapture !== undefined) this.focused = { kind: "capture", link: firstCapture };
    }
    if (this.layout !== "tabs") {
      if (this.pinned && !this.tiles.some((ref) => samePane(ref, this.pinned))) {
        this.tiles = [this.pinned, ...this.tiles];
      }
      if (this.focused) this.ensureTiled(this.focused);
      this.trimTiles();
    }
  }

  setFontSize(px: number) {
    this.fontSize = clampFont(px);
    try {
      localStorage.setItem(FONT_KEY, String(this.fontSize));
    } catch {
      /* ignore persistence failure */
    }
  }

  /** Grow/shrink the console font by delta px (used by the A-/A+ control). */
  bumpFontSize(delta: number) {
    this.setFontSize(this.fontSize + delta);
  }

  setConsoleMode(mode: ConsoleMode) {
    this.consoleMode = mode;
    try {
      localStorage.setItem(MODE_KEY, mode);
    } catch {
      /* ignore persistence failure */
    }
  }

  toggleConsoleMode() {
    this.setConsoleMode(this.consoleMode === "web" ? "native" : "web");
  }

  setDockSide(side: DockSide) {
    this.dockSide = side;
    try {
      localStorage.setItem(SIDE_KEY, side);
    } catch {
      /* ignore persistence failure */
    }
  }

  toggleDockSide() {
    this.setDockSide(this.dockSide === "bottom" ? "right" : "bottom");
  }

  setColorize(on: boolean) {
    this.colorize = on;
    try {
      localStorage.setItem(COLOR_KEY, on ? "1" : "0");
    } catch {
      /* ignore persistence failure */
    }
  }

  toggleColorize() {
    this.setColorize(!this.colorize);
  }
}

export const consoleUiStore = new ConsoleUiStore();
