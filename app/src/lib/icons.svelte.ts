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
   * For builtin/custom-svg: the inner SVG markup (paths etc), drawn inside a
   * 0 0 24 24 stroke viewBox. For custom-png: undefined (use `href`).
   */
  inner?: string;
  /** For raster (custom-png) icons and imported single-file SVGs: a data URL. */
  href?: string;
}

const STROKE_ATTRS =
  'viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"';

// ---- bundled glyphs (stroke = currentColor) ----
const BUILTIN: Record<string, { label: string; inner: string }> = {
  router: {
    label: "Router",
    inner:
      '<circle cx="12" cy="12" r="8"/><path d="M12 5v5m0 0 3-2m-3 2-3-2M19 12h-5m0 0 2 3m-2-3 2-3M5 12h5m0 0-2 3m2-3-2-3M12 19v-5m0 0 3 2m-3-2-3 2"/>',
  },
  switch: {
    label: "Switch",
    inner:
      '<rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 4v3m0 13v-3m5-13v3m0 13v-3m5-13v3m0 13v-3M8 11l3-2m-3 4 3 2m8-4-3-2m3 4-3 2"/>',
  },
  "l3-switch": {
    label: "L3 Switch",
    inner:
      '<rect x="3" y="7" width="18" height="10" rx="2"/><path d="M7 4v3m10-3v3M7 20v-3m10 3v-3"/><path d="M8 12h8m0 0-2-2m2 2-2 2M16 15H8m0 0 2-2m-2 2 2 2"/>',
  },
  pc: {
    label: "PC",
    inner: '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8m-4-4v4"/>',
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
};

// ---- UI (chrome) glyphs, not device icons ----
export const UI_GLYPHS: Record<string, string> = {
  play: '<path d="M6 4l14 8-14 8V4Z" fill="currentColor" stroke="none"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor" stroke="none"/>',
  fit: '<path d="M4 9V5a1 1 0 0 1 1-1h4M20 9V5a1 1 0 0 0-1-1h-4M4 15v4a1 1 0 0 0 1 1h4m11-5v4a1 1 0 0 1-1 1h-4"/>',
  reset: '<path d="M4 4v6h6M20 20v-6h-6"/><path d="M20 10a8 8 0 0 0-14.5-4.3L4 10M4 14a8 8 0 0 0 14.5 4.3L20 14"/>',
  upload: '<path d="M12 15V4m0 0-4 4m4-4 4 4M5 19h14"/>',
  images: '<rect x="3" y="4" width="18" height="14" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m4 16 5-4 4 3 3-2 4 3"/>',
  net: '<path d="M12 3v6m0 0-3 3m3-3 3 3M6 21a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm12 0a2 2 0 1 0 0-4 2 2 0 0 0 0 4Zm-6-9v0M8 17l3-3m5 3-3-3"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
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

export function resolveIcon(key: string | undefined): IconEntry | undefined {
  if (!key) return undefined;
  if (key in BUILTIN) {
    return { key, label: BUILTIN[key].label, kind: "builtin", inner: BUILTIN[key].inner };
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
 * (%APPDATA%\iolab\icons) and returns { key, href }. In the browser/mock build
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
    // custom icons persist to %APPDATA%\iolab\icons across sessions. Until then
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
