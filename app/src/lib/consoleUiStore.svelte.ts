// Console-dock UI preferences (dock side + colorizer on/off). Rune-backed
// singleton, persisted to localStorage — same idiom as themeStore. Kept out of
// labStore because these are pure view prefs, not lab/runtime state.

export type DockSide = "bottom" | "right";
/** How the node-hover Console button (and context-menu Console) opens a console:
 *  "web" = an in-app web console tab; "native" = hand off to the OS telnet
 *  client via the telnet:// scheme. Global, not per-node. */
export type ConsoleMode = "web" | "native";

const SIDE_KEY = "iolbox.console.dockSide";
const COLOR_KEY = "iolbox.console.colorize";
const MODE_KEY = "iolbox.console.mode";
const FONT_KEY = "iolbox.console.fontSize";

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

class ConsoleUiStore {
  dockSide = $state<DockSide>("bottom");
  colorize = $state(true);
  /** Global console-open mode (web tab vs native telnet client). Default web. */
  consoleMode = $state<ConsoleMode>("web");
  /** Terminal font size (px), shared by every console terminal, persisted. */
  fontSize = $state(FONT_DEFAULT);

  constructor() {
    this.dockSide = initialSide();
    this.colorize = initialColorize();
    this.consoleMode = initialMode();
    this.fontSize = initialFontSize();
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
