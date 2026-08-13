# iolbox design brief — "Bench & Glass"

The design brief behind the shipped frontend redesign: it replaced the
placeholder GitHub-dark scaffold with the "Bench" (dark) and "Glass" (light)
visual identity and the interaction changes now live in the product's
Settings dialog (Appearance → Theme).

## The problem with what we have

The scaffold theme is competent but generic — near-black `#0d1117`, a safe blue
accent, rounded cards. It reads as "AI-coded default." A network-lab tool has a
world of its own to draw from; the redesign should look like an **instrument**,
not a web template.

## Identity: the lab bench

Ground the look in the subject's materials: rack equipment, terminal phosphor,
LED status indicators, colour-coded patch cabling, the monospace CLI. Two themes
share one token system and one layout; only the surface treatment differs.

### Theme A — "Bench" (dark, default)
An instrument at rest on a dark bench. The ground is a **blue-black slate**, not
pure black — a neutral biased toward the accent so it reads as chosen. Status is
shown as **LEDs with a soft glow**, the way real gear signals state. Data — node
names, interfaces, telnet ports — is set in **mono**, because that is the language
of the CLI the user lives in.

### Theme B — "Glass" (light, "apple glass white")
Frosted translucent panels over a soft, faintly-cool white, with `backdrop-filter`
blur + saturation, hairline borders, and gentle shadows — the Apple vibrancy
material. Apple's system blue is the accent here (authentic to the reference).
Panels float above the canvas; the topology reads through the glass.

## Tokens (both themes)

| Role | Bench (dark) | Glass (light) |
|---|---|---|
| ground | `#0a0e14` | `#eef1f6` |
| panel | `#10161f` | `rgba(255,255,255,0.62)` + blur(20px) saturate(180%) |
| panel-2 / elevated | `#151d28` | `rgba(255,255,255,0.80)` |
| border (hairline) | `#223041` | `rgba(16,26,42,0.10)` |
| ink / secondary / tertiary | `#eaf0f7` / `#9db0c6` / `#61748c` | `#1b2431` / `#566579` / `#8a97a8` |
| accent (used once, sparingly) | cable-cyan `#4bc6d1` | apple-blue `#0a84ff` |
| canvas dot-grid | `#1a2531` | `#d4dbe6` |

**Semantic status (separate from accent — LED palette, both themes):**
running `#39d98a` · starting `#f0b429` (soft pulse) · crashed `#ff5a5f` ·
stopped `#5b6b7f`. **Link/cable tints** carry meaning: default cable = border
colour; capture-active = amber glow; selected = accent.

## Type

- **Data face (mono):** `"Cascadia Code","JetBrains Mono",ui-monospace,"SF Mono",Consolas,monospace`
  — node names, interface ids, telnet ports, RAM, anything the CLI would print.
- **UI face:** `system-ui,"Segoe UI",-apple-system,Roboto,sans-serif` — buttons,
  labels, prose.
- Type scale 11/12/13/14/16/20; uppercase eyebrows get `letter-spacing: 0.06em`.
- Never link a webfont CDN (blocked); if a custom face is wanted, inline it.

*Rationale:* mono-for-data is the one type move that makes the tool feel native to
its audience instead of like a generic dashboard. It's the signature.

## The four interaction requirements (precise specs)

### 1. Changeable icons
- Each node has an `icon` field (already in the schema). Ship a bundled,
  license-clean SVG icon set (avoid Cisco-brand marks). Default icon derives
  from image class (L3→router, L2→switch, vpcs→PC); user overrides per node.
- **Icon picker**: right-click node → "Change icon…" and an Inspector control open
  a popover grid of glyphs; click swaps live. Allow "Import SVG…" for custom.
- Icons are tintable via `currentColor` so they inherit theme + status.

### 2. Connector labels = PNetLab-style hover-pop
- Replace xyflow's flat SVG edge text with **HTML labels** (EdgeLabelRenderer),
  one small **mono chip** at each end of a link showing the local interface (e.g.
  `Gi0/0`); the running node's chip also carries its `telnet NNNNN`.
- **Rest state**: small, low-contrast, unobtrusive. **On hover** (of the chip or
  its link): the chip **scales up ~1.6×**, lifts with a shadow, gains the glassy
  tooltip background, and reveals the full detail (`R1 Gi0/0 · telnet 30013`).
  120–160ms ease; `transform-origin` toward the node. This is the exact
  "jumps out bigger on hover" behaviour from classic PNetLab.
- Respect `prefers-reduced-motion` (cross-fade instead of scale).

### 3. Floating edges — links exit the side facing the neighbour
- Drop fixed source/target handles. Compute each edge's endpoint as the
  intersection of the centre-to-centre line with the node's perimeter (the xyflow
  "floating edges" recipe: a `getEdgeParams(source,target)` helper feeding
  `getBezier/StraightPath`). Recompute on drag so cables re-anchor live.
- A node keeps four connectable sides; new links attach on whichever side faces
  the other node. Multiple links to different neighbours fan out naturally.

### 4. Background + infinite canvas
- Keep the **PNetLab dotted grid** (xyflow `Background` `Dots`, gap 20). The
  canvas is **already infinite** — xyflow pans without bounds; we just remove the
  `fitView` clamp and set no `translateExtent`. Add a "reset view" and
  fit-to-content control. Optional cross/grid variants as a view setting.

## "Doesn't look AI-coded" — the checklist

- Neutrals are hue-biased toward the accent, not pure grey/black.
- Mono carries the data; UI face carries the chrome. Not one neutral face for all.
- Status is **shape + colour** (LED with glow), not just a coloured word.
- Spacing comes from a scale and from flex/grid `gap`, not ad-hoc margins.
- One bold move (the accent / the glass) and everything else quiet.
- Real empty states and real copy ("Drag a device onto the bench"), no lorem.
- No gratuitous motion — every animation is functional (hover-pop, LED pulse,
  cable re-anchor, theme fade).

## Revisions round 2 (2026-07-02) — link-add, node edit, link glow

Three interaction changes requested after the redesign kickoff. These refine D2/D3
and add a node-edit dialog. Reference: PNetLab's connect-and-pick workflow.

### R2.1 — PNetLab-style link-add (connector-on-hover → drag → interface picker)
The current handle-to-handle connect is too fiddly. Match PNetLab exactly:
- **Hover a node** → a **connector affordance appears on the node** (a small
  crosshair/link glyph overlaid on the node icon, like the orange connector in the
  reference). It only shows on hover (and on keyboard focus), never at rest.
- **Press on that affordance and drag** → a live "rubber-band" link follows the
  cursor from the source node center.
- **Drop anywhere over node B** — the drop does **not** need to land on a precise
  handle/port. Hit-test the whole target node's bounding box; if the pointer is
  over any part of node B, it's a valid drop. Highlight node B (accent ring) while
  hovered as a drop target.
- **On drop → open an Interface Picker** popover near the drop point: two columns
  (or two selects) — **local interface on A** and **remote interface on B** — each
  listing that node's *free* interfaces (used ones disabled), pre-selecting the
  next free one on each side. Confirm creates the link with the chosen endpoints;
  Cancel aborts. VPCS auto-selects `eth0` (only one), so its column collapses to a
  label.
- Implementation notes: use a full-node connectable target (xyflow: a node-sized
  target handle or `isValidConnection` + `onconnectend` hit-testing the pane), not
  four tiny port handles. The visible per-side floating anchor (D2) is still where
  the *edge* attaches after creation; the *drop* is node-level. Keep the rubber-band
  styled like a cable (accent, slightly thicker) during the drag.

### R2.2 — Node edit dialog
Right-click node → **"Edit…"** (and double-click node) opens a modal to change:
- **Name**, **Icon** (opens the icon picker incl. Import — R from round 1),
- **Image** (library picker; hot-swap by id),
- **RAM (MB)** (number, sensible min per class),
- **Ethernet adapters** and **Serial adapters** counts (each adapter = 4 ports;
  changing these re-derives available interfaces and must not orphan existing links
  — warn if reducing below a count that has links attached),
- **Boot from startup-config** toggle (on = inject the node's `startupConfig` into
  NVRAM at boot; off = boot image default) plus the startup-config editor,
- Applied on **Save**; changes to adapters/RAM/image take effect on next node start
  (show a "restart required" hint if the node is running).
This supersedes the lighter Inspector-only editing for the full set of properties;
the Inspector stays as the quick-glance/quick-edit panel, "Edit…" is the full form.

### R2.3 — Link hover glow
Hovering a link (edge) **or** its interface chip highlights the whole cable: a
soft accent **glow** (`filter: drop-shadow` in the accent color) + slight
stroke-width increase, 120ms ease. Capture-active links already glow amber; on
hover they intensify. This makes it obvious which cable a chip belongs to and which
link a right-click will target. Respect `prefers-reduced-motion` (glow only, no
width animation).

## Revisions from review (2026-07-02)

Three changes requested after seeing the "Bench & Glass" mockup:

1. **Icons must be uploadable (PNetLab-style).** Beyond the bundled set + picker,
   the user imports their own icons exactly like PNetLab's icon management: an
   "Import icon…" action accepts SVG/PNG, stores it in a per-user icon library
   (`%APPDATA%\iolbox\icons`), and it appears in the picker for any node. Custom
   raster icons render as-is; SVG icons are tintable when single-colour. The node
   `icon` field stores the library key. This is part of D4.

2. **Consoles modeled on PNetLab's webconsole.** The in-app terminal should match
   PNetLab's web console behaviour and features: a browser-grade **telnet-over-
   WebSocket** terminal (xterm.js) — telnet IAC negotiation handled server-side,
   window-size (NAWS) propagation, copy/paste, reconnect-on-drop, and multiple
   concurrent node consoles as tabs. Because browsers can't open raw telnet, this
   WS bridge is the *same* mechanism that makes the browser build's consoles work
   — desktop and browser consoles unify on it. Lives in the supervisor (WS bridge)
   + Console components. Folds into the console work and Track 3.

3. **Improve text readability.** The mockup's smaller mono labels trade legibility
   for density. Raise the floor: minimum 12px for any persistent UI text, stronger
   ink/secondary contrast (hit WCAG AA on both themes), slightly looser line-height
   on config/console text, and keep the tiny sizes only for the rest-state port
   chips (which enlarge on hover anyway). Verify contrast in D6.

## Themeability (so it stays "modernizable")

All of the above is driven by `theme.css` custom properties under
`:root[data-theme="bench"]` / `[data-theme="glass"]`. Adding a third theme = one
token block. Components reference tokens only — no hard-coded colours. A
`ThemeProvider` writes `data-theme` on `<html>` and persists the choice.
