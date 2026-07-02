// ThemeProvider — D1. Writes data-theme onto the document root and persists the
// choice (localStorage now; a Tauri store can back this later without changing
// the call sites). Exposes a rune-backed singleton.

export type ThemeName = "bench" | "glass";

const STORAGE_KEY = "iolab.theme";

function initialTheme(): ThemeName {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === "bench" || saved === "glass") return saved;
  } catch {
    /* localStorage may be unavailable (private mode / SSR) */
  }
  return "bench";
}

class ThemeStore {
  current = $state<ThemeName>("bench");

  constructor() {
    this.current = initialTheme();
    this.apply();
  }

  private apply() {
    if (typeof document !== "undefined") {
      document.documentElement.setAttribute("data-theme", this.current);
    }
  }

  set(theme: ThemeName) {
    this.current = theme;
    this.apply();
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      /* ignore persistence failure */
    }
  }

  toggle() {
    this.set(this.current === "bench" ? "glass" : "bench");
  }
}

export const themeStore = new ThemeStore();
