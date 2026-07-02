// Console-dock UI preferences (dock side + colorizer on/off). Rune-backed
// singleton, persisted to localStorage — same idiom as themeStore. Kept out of
// labStore because these are pure view prefs, not lab/runtime state.

export type DockSide = "bottom" | "right";

const SIDE_KEY = "iolab.console.dockSide";
const COLOR_KEY = "iolab.console.colorize";

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

class ConsoleUiStore {
  dockSide = $state<DockSide>("bottom");
  colorize = $state(true);

  constructor() {
    this.dockSide = initialSide();
    this.colorize = initialColorize();
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
