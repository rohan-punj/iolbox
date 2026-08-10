// Left-palette collapse preference. Rune-backed singleton, persisted to
// localStorage — same idiom as consoleUiStore/themeStore. A pure view pref,
// not lab/runtime state, so it lives outside labStore.

const COLLAPSED_KEY = "iolbox.palette.collapsed";

function initialCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === "1";
  } catch {
    /* localStorage may be unavailable (private mode / SSR) */
  }
  return false;
}

class PaletteUiStore {
  collapsed = $state(false);

  constructor() {
    this.collapsed = initialCollapsed();
  }

  toggle() {
    this.collapsed = !this.collapsed;
    try {
      localStorage.setItem(COLLAPSED_KEY, this.collapsed ? "1" : "0");
    } catch {
      /* ignore persistence failure */
    }
  }
}

export const paletteUiStore = new PaletteUiStore();
