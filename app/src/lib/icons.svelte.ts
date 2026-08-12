// Icon registry — D4. A bundled set of licence-clean stroke device glyphs
// (currentColor-tintable so they inherit theme + node status), plus a runtime
// store for user-imported icons (SVG or PNG). Nodes persist an icon *key*; this
// registry resolves the key to renderable markup.
//
// The bundled glyphs are simple original stroke drawings adapted from the
// "Bench & Glass" mockup — no Cisco-brand marks.

import { invoke } from "@tauri-apps/api/core";

export type IconKind = "builtin" | "custom-svg" | "custom-png";

export interface IconEntry {
  /** Stable registry key stored on the node (node.icon). */
  key: string;
  /** Human label shown in the picker. */
  label: string;
  kind: IconKind;
  /**
   * For builtin/custom-svg: the inner SVG markup (paths etc). Stroke glyphs
   * draw inside the default 0 0 24 24 stroke viewBox; filled artwork carries
   * its own `viewBox` and paints its own fills (see viewBox).
   * For custom-png: undefined (use `href`).
   */
  inner?: string;
  /**
   * Present on filled-artwork builtins (the EVE-style device set): the SVG
   * viewBox the inner markup was drawn against. Its presence switches
   * iconSvg from the stroke/currentColor wrapper to a plain wrapper so the
   * artwork's own fills render as designed.
   */
  viewBox?: string;
  /** For raster (custom-png) icons and imported single-file SVGs: a data URL. */
  href?: string;
}

const STROKE_ATTRS =
  'viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"';

// ---- bundled glyphs ----
// Stroke glyphs (no viewBox field) tint via currentColor inside the shared
// 24x24 stroke wrapper. The router/switch/pc defaults are the EVE-style
// two-tone artwork (plate #fafafa, glyph #3c708a) used across the user's lab
// packs; they carry their own viewBox and fills.
const BUILTIN: Record<string, { label: string; inner: string; viewBox?: string }> = {
  router: {
    label: "Router",
    viewBox: "-0.5 -0.5 57 57",
    inner:
      '<ellipse cx="28" cy="28" rx="28" ry="28" fill="#fafafa" stroke="none"/><path d="M 42.65 45.22 L 35.59 38.3 L 32.71 41.23 L 30.51 30.65 L 41.16 32.62 L 38.3 35.52 L 45.37 42.45 Z M 26.8 31.78 L 19.87 38.84 L 22.8 41.72 L 12.23 43.92 L 14.19 33.28 L 17.1 36.12 L 24.02 29.06 Z M 13.46 10.72 L 20.52 17.65 L 23.4 14.72 L 25.6 25.29 L 14.95 23.33 L 17.8 20.42 L 10.74 13.5 Z M 29.02 24.09 L 35.95 17.02 L 33.02 14.14 L 43.59 11.95 L 41.63 22.59 L 38.73 19.74 L 31.8 26.81 Z M 28 0 C 12.54 0 0 12.54 0 28 C 0 43.46 12.54 56 28 56 C 43.46 56 56 43.46 56 28 C 56 12.54 43.46 0 28 0 Z M 28 0.71 C 43.08 0.71 55.29 12.92 55.29 28 C 55.29 43.08 43.08 55.29 28 55.29 C 12.92 55.29 0.71 43.08 0.71 28 C 0.71 12.92 12.92 0.71 28 0.71 Z" fill="#3c708a" stroke="none"/>',
  },
  switch: {
    label: "Switch",
    viewBox: "-0.5 -0.5 53 53",
    inner:
      '<path d="M 3.69 -0.28 C 1.93 -0.28 0.5 1.15 0.5 2.9 L 0.5 48.54 C 0.5 50.29 1.93 51.72 3.69 51.72 L 49.31 51.72 C 51.07 51.72 52.5 50.29 52.5 48.54 L 52.5 2.9 C 52.5 1.15 51.07 -0.28 49.31 -0.28 Z" fill="#fafafa" stroke="none"/><path d="M 24.81 41.86 L 24.81 37.72 L 13.79 37.72 L 13.79 34.44 L 4.4 39.98 L 13.79 45.44 L 13.79 41.86 Z M 29.96 22.04 L 29.96 17.9 L 18.94 17.9 L 18.94 14.62 L 9.55 20.17 L 18.94 25.62 L 18.94 22.04 Z M 23.77 32.02 L 23.77 27.88 L 34.8 27.88 L 34.8 24.61 L 44.18 30.15 L 34.8 35.61 L 34.8 32.02 Z M 28.54 12.54 L 28.54 8.4 L 39.56 8.4 L 39.56 5.12 L 48.94 10.67 L 39.56 16.12 L 39.56 12.54 Z M 3.69 -0.28 C 1.93 -0.28 0.5 1.15 0.5 2.9 L 0.5 48.54 C 0.5 50.29 1.93 51.72 3.69 51.72 L 49.31 51.72 C 51.07 51.72 52.5 50.29 52.5 48.54 L 52.5 2.9 C 52.5 1.15 51.07 -0.28 49.31 -0.28 Z M 3.69 1.15 L 49.31 1.15 C 50.29 1.15 51.06 1.92 51.06 2.9 L 51.06 48.54 C 51.06 49.52 50.29 50.29 49.31 50.29 L 3.69 50.29 C 2.71 50.29 1.94 49.52 1.94 48.54 L 1.94 2.9 C 1.94 1.92 2.71 1.15 3.69 1.15 Z" fill="#3c708a" stroke="none"/>',
  },
  "l3-switch": {
    label: "L3 Switch",
    inner:
      '<rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 4v3m10-3v3M7 20v-3m10 3v-3"/><path d="M8 12h8m0 0-2-2m2 2-2 2M16 15H8m0 0 2-2m-2 2 2 2"/>',
  },
  pc: {
    label: "PC",
    viewBox: "-0.5 -0.5 50 40",
    inner:
      '<path d="M 5.85 4.65 L 43.84 4.65 L 43.84 26.24 L 5.85 26.24 Z M 4.96 2 L 45.04 2 C 46.61 2 47.85 3.15 47.85 4.61 L 47.85 26.52 C 47.85 27.98 46.61 29.13 45.04 29.13 L 4.96 29.13 C 3.38 29.13 2.15 27.98 2.15 26.52 L 2.15 4.61 C 2.15 3.15 3.38 2 4.96 2 Z M 4.96 0 C 2.23 0 0 2.08 0 4.61 L 0 26.52 C 0 29.06 2.23 31.14 4.96 31.14 L 20.55 31.14 L 20.55 35.7 L 13.1 35.7 C 11.83 35.7 10.79 36.66 10.79 37.84 C 10.79 39.03 11.83 39.99 13.1 39.99 L 36.67 39.99 C 37.94 39.99 38.97 39.03 38.97 37.84 C 38.97 36.66 37.94 35.7 36.67 35.7 L 30.24 35.7 L 30.24 31.14 L 45.04 31.14 C 47.77 31.14 50 29.06 50 26.52 L 50 4.61 C 50 2.08 47.77 0 45.04 0 Z" fill="#3c708a" stroke="none"/>',
  },
  laptop: {
    label: "Laptop",
    inner: '<rect x="5" y="5" width="14" height="9" rx="1.5"/><path d="M3 18h18l-1-2H4l-1 2Z"/>',
  },
  firewall: {
    label: "Firewall",
    inner:
      '<rect x="3" y="4" width="18" height="16" rx="1.5"/><path d="M3 9h18M3 15h18M9 4v5m0 6v5m6-16v5m0 6v5M6 9v6m12-6v6"/>',
  },
  server: {
    label: "Server",
    inner:
      '<rect x="4" y="3" width="16" height="7" rx="1.5"/><rect x="4" y="14" width="16" height="7" rx="1.5"/><path d="M8 6.5h.01M8 17.5h.01"/>',
  },
  cloud: {
    label: "Cloud",
    inner: '<path d="M7 18a4 4 0 0 1 0-8 5 5 0 0 1 9.6-1.5A3.5 3.5 0 0 1 17 18H7Z"/>',
  },
  ap: {
    label: "Access Point",
    inner: '<circle cx="12" cy="17" r="2"/><path d="M8.5 13.5a5 5 0 0 1 7 0M6 11a8 8 0 0 1 12 0M12 19v-2"/>',
  },
  // NAT gateway — a globe with an outward arrow (translation to the outside).
  nat: {
    label: "NAT Gateway",
    inner:
      '<circle cx="10" cy="12" r="7"/><path d="M3 12h14M10 5c2 2 2 12 0 14M10 5c-2 2-2 12 0 14"/><path d="M17 6h4v4M21 6l-5 5"/>',
  },
  tool: {
    label: "Learning Tool",
    inner:
      '<path d="m14.5 5.5 4-4 4 4-4 4"/><path d="m18.5 9.5-8.8 8.8a2.5 2.5 0 0 1-3.5 0l-.5-.5a2.5 2.5 0 0 1 0-3.5l8.8-8.8"/><path d="m5.5 18.5-3 3M14 3a5 5 0 0 0-6.2 6.2"/>',
  },
};

// ---- UI (chrome) glyphs, not device icons ----
export const UI_GLYPHS: Record<string, string> = {
  play: '<path d="M6 4l14 8-14 8V4Z" fill="currentColor" stroke="none"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor" stroke="none"/>',
  fit: '<path d="M4 9V5a1 1 0 0 1 1-1h4M20 9V5a1 1 0 0 0-1-1h-4M4 15v4a1 1 0 0 0 1 1h4m11-5v4a1 1 0 0 1-1 1h-4"/>',
  reset: '<path d="M4 4v6h6M20 20v-6h-6"/><path d="M20 10a8 8 0 0 0-14.5-4.3L4 10M4 14a8 8 0 0 0 14.5 4.3L20 14"/>',
  // Pan tool — an open hand. Toggled on = drag-to-pan with the mouse only.
  hand: '<path d="M8 11V5.5a1.5 1.5 0 0 1 3 0V10m0-1.5a1.5 1.5 0 0 1 3 0V11m0-1a1.5 1.5 0 0 1 3 0v4a5 5 0 0 1-5 5h-1.5a5 5 0 0 1-3.6-1.5L5 15.5a1.5 1.5 0 0 1 2.2-2L8 14.5"/>',
  upload: '<path d="M12 15V4m0 0-4 4m4-4 4 4M5 19h14"/>',
  download: '<path d="M12 4v11m0 0-4-4m4 4 4-4M5 19h14"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  folder: '<path d="M3 6a1 1 0 0 1 1-1h5l2 2h8a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6Z"/>',
  save: '<path d="M5 4h11l3 3v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1Z"/><path d="M8 4v5h7V4M8 21v-6h8v6"/>',
  images: '<rect x="3" y="4" width="18" height="14" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m4 16 5-4 4 3 3-2 4 3"/>',
  net: '<path d="M12 3v6m0 0-3 3m3-3 3 3M6 21a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm12 0a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm-6-9v0M8 17l3-3m5 3-3-3"/>',
  link: '<path d="M9 12h6M8.5 8.5 6 11a3.5 3.5 0 0 0 5 5l1.5-1.5m3-3L17 10a3.5 3.5 0 0 0-5-5l-1.5 1.5"/>',
  edit: '<path d="M4 20h4l10-10-4-4L4 16v4Zm10-14 4 4"/>',
  tasks: '<path d="M9 6h11M9 12h11M9 18h11"/><path d="m3.5 5.5 1 1 2-2M3.5 11.5l1 1 2-2M3.5 17.5l1 1 2-2"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  console: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M13 15h4"/>',
  wipe: '<path d="M5 7h14M9 7V5h6v2M7 7l1 12h8l1-12"/><path d="M10 11v5m4-5v5"/>',
  // Add-Shapes flyout tool icons — one shape each, distinct from "net"/"link"
  // (those are for the network-watcher/link-menu chrome, not annotations).
  rectShape: '<rect x="4" y="6" width="16" height="12" rx="1.5"/>',
  ellipseShape: '<ellipse cx="12" cy="12" rx="8" ry="6"/>',
  lineShape: '<path d="M5 19 19 5"/><circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none"/><circle cx="19" cy="5" r="1.5" fill="currentColor" stroke="none"/>',
  // Floppy-disk "save config" glyph.
  savecfg:
    '<path d="M5 4h11l3 3v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1Z"/><path d="M8 4v5h7V4M8 21v-6h8v6"/>',
  // Four detached corner brackets pointing outward — enter fullscreen.
  fullscreen:
    '<path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M3 16v3a2 2 0 0 0 2 2h3M21 16v3a2 2 0 0 1-2 2h-3"/>',
  // Corner brackets pointing inward — exit fullscreen.
  fullscreenExit:
    '<path d="M9 3v3a2 2 0 0 1-2 2H4M15 3v3a2 2 0 0 0 2 2h3M9 21v-3a2 2 0 0 0-2-2H4M15 21v-3a2 2 0 0 1 2-2h3"/>',
  chevronLeft: '<path d="M15 5 8 12l7 7"/>',
  chevronRight: '<path d="M9 5l7 7-7 7"/>',
  more: '<circle cx="12" cy="5" r="1.5" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/><circle cx="12" cy="19" r="1.5" fill="currentColor" stroke="none"/>',
};

/** Runtime store of user-imported icons, keyed by registry key. */
const custom = new Map<string, IconEntry>();

/** Version counter so Svelte $derived can react to imports. */
let registryVersion = $state(0);

/** Read this in a reactive context to re-run when the registry changes. */
export function iconRegistryVersion(): number {
  return registryVersion;
}

export function isBuiltinIcon(key: string): boolean {
  return key in BUILTIN;
}

/**
 * True when the icon is self-contained artwork (EVE-style builtins with their
 * own viewBox+fills, or any user-imported image) rather than a tintable
 * stroke glyph. Node faces render artwork full-bleed with no tile chrome —
 * the artwork carries its own plate — matching the PNetLab/EVE canvas look.
 */
export function isArtworkIcon(key: string | undefined): boolean {
  const entry = resolveIcon(key);
  if (!entry) return false;
  return entry.viewBox !== undefined || entry.kind !== "builtin";
}

export function resolveIcon(key: string | undefined): IconEntry | undefined {
  if (!key) return undefined;
  if (key in BUILTIN) {
    const b = BUILTIN[key];
    return { key, label: b.label, kind: "builtin", inner: b.inner, viewBox: b.viewBox };
  }
  return custom.get(key);
}

/** All pickable icons: bundled first, then user imports (import order). */
export function listIcons(): IconEntry[] {
  // touch version so callers in a reactive context re-run on import
  void registryVersion;
  const builtins: IconEntry[] = Object.entries(BUILTIN).map(([key, v]) => ({
    key,
    label: v.label,
    kind: "builtin",
    inner: v.inner,
    viewBox: v.viewBox,
  }));
  return [...builtins, ...custom.values()];
}

/** Full <svg> markup for a device glyph (stroke, currentColor). */
export function iconSvg(key: string | undefined, size = 24): string {
  const entry = resolveIcon(key) ?? resolveIcon("router")!;
  if (entry.kind === "custom-png" && entry.href) {
    return `<img src="${entry.href}" width="${size}" height="${size}" alt="" style="object-fit:contain;display:block" />`;
  }
  if (entry.kind === "custom-svg" && entry.href) {
    // Imported single-file SVG kept as-is (may be multi-colour) via <img>.
    return `<img src="${entry.href}" width="${size}" height="${size}" alt="" style="object-fit:contain;display:block" />`;
  }
  if (entry.viewBox) {
    // Filled artwork: its paths paint their own fills; no stroke tinting.
    return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${entry.viewBox}" width="${size}" height="${size}" style="display:block">${entry.inner}</svg>`;
  }
  return `<svg ${STROKE_ATTRS} width="${size}" height="${size}">${entry.inner}</svg>`;
}

/** Full <svg> markup for a UI chrome glyph. */
export function uiSvg(name: keyof typeof UI_GLYPHS | string, size = 15): string {
  const inner = UI_GLYPHS[name] ?? UI_GLYPHS.net;
  return `<svg ${STROKE_ATTRS} width="${size}" height="${size}">${inner}</svg>`;
}

/** Default icon derived from image class / node kind. */
export function defaultIconFor(kind: string, imageClass?: string): string {
  if (kind === "vpcs") return "pc";
  if (kind === "nat") return "nat";
	if (kind === "tool") return "tool";
	if (kind === "pc") return "pc";
  if (imageClass === "l2") return "switch";
  return "router"; // l3 / unknown
}

function slugify(name: string): string {
  return (
    "custom-" +
    name
      .toLowerCase()
      .replace(/\.[a-z0-9]+$/i, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 40) || "custom-icon"
  );
}

/** Register an imported icon in-memory; returns the assigned key. */
export function registerCustomIcon(opts: {
  name: string;
  kind: "custom-svg" | "custom-png";
  href?: string;
  inner?: string;
}): string {
  let key = slugify(opts.name);
  let n = 1;
  while (custom.has(key) || key in BUILTIN) key = `${slugify(opts.name)}-${n++}`;
  custom.set(key, {
    key,
    label: opts.name.replace(/\.[a-z0-9]+$/i, ""),
    kind: opts.kind,
    href: opts.href,
    inner: opts.inner,
  });
  registryVersion++;
  return key;
}

/**
 * Import an icon file. In the desktop (Tauri) build this calls the `import_icon`
 * command, which persists the file into the per-user library
 * (%APPDATA%\iolbox\icons) and returns { key, href }. In the browser/mock build
 * (or if the command is unavailable) it falls back to reading the File into a
 * data URL and registering it in-memory.
 *
 * Returns the registry key to store on the node, or null if cancelled/failed.
 */
export async function importIconFromFile(file: File): Promise<string | null> {
  const isSvg = file.type === "image/svg+xml" || /\.svg$/i.test(file.name);
  const kind: "custom-svg" | "custom-png" = isSvg ? "custom-svg" : "custom-png";

  // Try the Tauri command seam first (desktop build). If it isn't there
  // (browser/dev), this throws and we fall through to the in-memory path.
  try {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const res = (await invoke("import_icon", {
      name: file.name,
      bytes: Array.from(bytes),
    })) as { key: string; href: string; label?: string };
    // The command already persisted + returned a servable href.
    custom.set(res.key, {
      key: res.key,
      label: res.label ?? file.name.replace(/\.[a-z0-9]+$/i, ""),
      kind,
      href: res.href,
      inner: undefined,
    });
    registryVersion++;
    return res.key;
  } catch {
    // TODO(desktop): implement the `import_icon` Tauri command in src-tauri so
    // custom icons persist to %APPDATA%\iolbox\icons across sessions. Until then
    // this in-memory browser fallback keeps the feature fully functional in dev.
    const href = await readFileAsDataUrl(file);
    return registerCustomIcon({ name: file.name, kind, href });
  }
}

function readFileAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as string);
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}
