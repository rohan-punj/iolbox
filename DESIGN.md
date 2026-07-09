---
name: iolbox
description: A Windows-native lab bench for Cisco IOL and VPCS — an instrument, not a dashboard.
colors:
  # Accent — one per theme, used sparingly ("used once").
  accent-cable-cyan: "#4bc6d1"      # Bench (dark) accent
  accent-cable-cyan-strong: "#63d6e0"
  accent-apple-blue: "#0060df"      # Glass (light) accent — Apple's deeper on-white system blue (AA both ways)
  # Bench (dark) surfaces + ink
  ground-bench: "#0a0e14"           # blue-black slate, not pure black
  panel-bench: "#10161f"
  panel-2-bench: "#151d28"
  panel-elevated-bench: "#1a2430"
  border-bench: "#223041"
  border-strong-bench: "#30455a"
  ink-bench: "#eaf0f7"
  ink-2-bench: "#a7bacf"
  ink-3-bench: "#7286a0"
  node-face-bench: "#131b26"
  # Glass (light) surfaces + ink
  ground-glass: "#eef1f6"
  panel-solid-glass: "#ffffff"
  border-glass: "#1a2a42"           # ≈ rgba(16,26,42,0.10) over ground
  ink-glass: "#1b2431"
  ink-2-glass: "#4a5769"
  ink-3-glass: "#71808f"
  # IOL device-class accents (L3 uses the theme accent; L2 is violet)
  node-iol-l2-bench: "#9d8bff"
  node-iol-l2-glass: "#7a5cff"
  # Status LEDs — identical on both themes (semantic, separate from accent)
  state-running: "#39d98a"
  state-starting: "#f0b429"
  state-crashed: "#ff5a5f"
  state-stopped: "#5b6b7f"
  # Terminal phosphor
  term-bg-bench: "#08090c"
  term-ink-bench: "#cfe6d8"
  term-accent-bench: "#7be0a0"
typography:
  title:
    fontFamily: "Segoe UI Variable Text, Inter, Segoe UI, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0.005em"
  body:
    fontFamily: "Segoe UI Variable Text, Inter, Segoe UI, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "0.005em"
  label:
    fontFamily: "Segoe UI Variable Text, Inter, Segoe UI, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 650
    lineHeight: 1
    letterSpacing: "0.06em"
  data-mono:
    fontFamily: "Cascadia Code, Cascadia Mono, JetBrains Mono, ui-monospace, SF Mono, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.4
    letterSpacing: "normal"
  chip-mono:
    fontFamily: "Cascadia Code, Cascadia Mono, JetBrains Mono, ui-monospace, SF Mono, Consolas, monospace"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "normal"
rounded:
  sm: "5px"
  md: "8px"
  lg: "11px"
  node: "14px"
  full: "999px"
spacing:
  sp-1: "4px"
  sp-2: "8px"
  sp-3: "12px"
  sp-4: "16px"
  sp-5: "20px"
  sp-6: "24px"
  sp-8: "32px"
components:
  button:
    backgroundColor: "{colors.panel-2-bench}"
    textColor: "{colors.ink-bench}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "6px 12px"
  button-primary:
    backgroundColor: "{colors.accent-cable-cyan}"
    textColor: "#05161a"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "6px 12px"
  button-danger:
    backgroundColor: "#00000000"
    textColor: "{colors.state-crashed}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "6px 12px"
  button-ghost:
    backgroundColor: "#00000000"
    textColor: "{colors.ink-bench}"
    typography: "{typography.label}"
    rounded: "{rounded.md}"
    padding: "6px 12px"
  input:
    backgroundColor: "{colors.panel-solid-glass}"
    textColor: "{colors.ink-bench}"
    typography: "{typography.body}"
    rounded: "{rounded.sm}"
    padding: "6px 8px"
  pill:
    backgroundColor: "{colors.panel-2-bench}"
    textColor: "{colors.ink-2-bench}"
    typography: "{typography.label}"
    rounded: "{rounded.full}"
    padding: "4px 10px"
  node-face:
    backgroundColor: "{colors.node-face-bench}"
    textColor: "{colors.ink-bench}"
    rounded: "{rounded.node}"
    size: "64px"
  port-chip:
    backgroundColor: "{colors.panel-bench}"
    textColor: "{colors.ink-bench}"
    typography: "{typography.chip-mono}"
    rounded: "{rounded.sm}"
    padding: "2px 5px"
---

# Design System: iolbox

## 1. Overview

**Creative North Star: "Bench & Glass"**

iolbox is a network-lab **instrument**, and its identity is the duality named in its
two themes. **Bench** is the instrument at rest on a dark workbench — a blue-black
slate ground (`#0a0e14`), not pure black, biased toward the accent so it reads as
*chosen*. **Glass** is the same instrument under studio light — Apple-vibrancy
frosted panels floating over a faintly-cool white, the topology reading through the
material. One token system, one layout; only the surface treatment changes. Every
component references tokens; there are no hard-coded colors anywhere in the app.

The system draws exclusively from the subject's own world: rack equipment, terminal
phosphor, LED status indicators, color-coded patch cabling, the monospace CLI. That
sourcing is the whole point. This is a tool built for **Cisco certification students**
who live in a terminal, so anything the CLI would print — node names, interface ids,
telnet ports, RAM — is set in **mono**, and the UI chrome is set in the system UI
face. That single split is the signature; it is what makes the tool feel native to
its audience instead of like a generic admin dashboard.

The system explicitly rejects the "AI-coded default": near-black `#0d1117`, a safe
blue accent, rounded generic cards, and one neutral sans doing every job. It rejects
metrics-dashboard chrome (hero-number tiles, identical card grids) and the dated,
heavy feel of the incumbent lab web consoles (PNetLab / EVE-NG / CML). It borrows
their good *interactions* — hover-pop port labels, connector-on-hover link-add,
the dotted infinite canvas — never their weight.

**Key Characteristics:**
- Hue-biased neutrals (slate tuned toward the accent), never pure grey or black.
- Mono carries the data; the UI face carries the chrome — never one face for all.
- Status is an **LED with a soft glow** (shape + color + label), never a bare word.
- One bold move per surface (the accent, or the glass material); everything else quiet.
- Tactile & responsive: elements come alive under the cursor (hover-pop chips, LED
  pulse, connector affordance, cable glow) — but every motion is functional.

## 2. Colors

A restrained palette: hue-biased neutrals carry ~90% of every surface, a single
accent is rationed, and a fixed four-color LED set is reserved for state.

### Primary
- **Cable-Cyan** (`#4bc6d1`, Bench) / **Apple Blue** (`#0060df`, Glass): the one
  accent. It marks selection, focus rings, the primary action, active links, the
  connector affordance, and L3 device glyphs — and almost nothing else. Its rarity
  is what gives it force. The Glass value is Apple's *deeper on-white* system blue
  (dark mode uses `#0a84ff`; on a light surface Apple drops to a deeper blue) —
  dark enough to clear AA both as label text and as a solid fill under white text.

### Secondary
- **Patch Violet** (`#9d8bff` Bench / `#7a5cff` Glass): the L2 device-class accent,
  distinguishing switches from routers on the canvas without spending the primary
  accent. Also seeds the brand-mark gradient (accent → violet).

### Tertiary — Status LEDs (theme-independent)
A closed set, identical on both themes, used *only* for node/link/provider state:
- **Running** (`#39d98a`) — steady green LED.
- **Starting** (`#f0b429`) — amber, soft pulse.
- **Crashed** (`#ff5a5f`) — red.
- **Stopped** (`#5b6b7f`) — cool grey, unlit.
- Capture-active links glow in the Starting amber; STP-blocked links render red-dashed.

### Neutral
- **Slate Ground** (`#0a0e14` Bench / `#eef1f6` Glass): the workbench beneath the
  canvas and the app body.
- **Panel** (`#10161f` Bench / `rgba(255,255,255,0.62)`+blur Glass): topbar, side
  panels, dialogs. On Glass this is a translucent vibrancy material.
- **Ink ramp** — `#eaf0f7 / #a7bacf / #7286a0` (Bench), `#1b2431 / #4a5769 / #71808f`
  (Glass): primary / secondary / tertiary text. The secondary and tertiary steps are
  deliberately raised (Bench) and darkened (Glass) to clear WCAG AA on their grounds.
- **Border** (`#223041` Bench / `~rgba(16,26,42,0.10)` Glass): hairline dividers and
  control strokes; a `-strong` step (`#30455a` Bench) for inputs and segmented groups.
- **Terminal phosphor** (`#08090c` bg / `#cfe6d8` ink / `#7be0a0` accent): the xterm
  console surface, held slightly greener and darker than the app — a screen within a screen.

### Named Rules
**The One Accent Rule.** The primary accent appears on ≤10% of any screen. It is
spent on selection, focus, the single primary action, and live links — never as
decoration, never as a fill for a resting panel. If two things on screen are both
accent-colored and neither is selected/active/primary, one is wrong.

**The LED Rule.** State is *never* communicated by text color alone. It is an LED
dot with a colored `box-shadow` halo (plus a pulse for the transitional Starting
state), backed by a label. The four state colors are reserved — they may not be
borrowed for accent or decoration.

## 3. Typography

**UI Font:** Segoe UI Variable Text (with Inter, then Segoe UI, then `system-ui`)
**Data / Mono Font:** Cascadia Code (with Cascadia Mono, JetBrains Mono, `ui-monospace`, Consolas)

**Character:** A functional two-face pairing on a hard contrast axis — a variable
humanist UI sans against a coding monospace. The pairing is not decorative; it is
semantic. The face a string is set in tells you *what kind of thing it is*: chrome
or data. Both are OS-native stacks, so no webfont is ever fetched (CDN fonts are
blocked in the Tauri shell; a custom face would have to be inlined).

### Hierarchy
The scale is tight and instrument-dense: **11 / 12 / 13 / 14 / 16 / 20px**. There
is no display type — this is a tool, not a page. The readability floor for
persistent UI is 12px; 11px is reserved for rest-state port chips that enlarge on hover.
- **Title** (600, 20px / `--fs-xl`): dialog and panel headings. The largest type in the app.
- **Section head** (600, 16px / `--fs-lg`): sub-headings inside panels and dialogs.
- **Body** (400, 13px / `--fs-base`): default UI text, labels, prose.
- **Label / Eyebrow** (650, 12px, `letter-spacing: 0.06em`, UPPERCASE): the `.eyebrow`
  and `.pill` micro-labels. Deliberate and rationed — a system label, not scaffolding.
- **Data (mono)** (400, 13px): node names, interface ids, telnet ports, RAM, config —
  anything the CLI would print. Rendered in Cascadia.
- **Port chip (mono)** (400, 11–12px): the smallest data face; grows on hover.

### Named Rules
**The Mono-For-Data Rule.** If the value would appear in a CLI — a hostname, an
interface (`Gi0/0`), a telnet port, a byte count — it is set in the mono face. If it
is chrome — a button, a heading, a description — it is set in the UI face. This split
is the identity; collapsing everything to one face destroys it.

## 4. Elevation

A hybrid system: surfaces are **flat and tonal at rest**, and shadow appears as a
response to elevation or state, not as ambient decoration. Depth is built primarily
from tonal layering (`ground` → `panel` → `panel-2` → `panel-elevated`) and hairline
borders. On the **Glass** theme, floating panels add `backdrop-filter: saturate(180%)
blur(20px)` so the canvas reads through them — this is the theme's one intentional use
of glassmorphism, and it is load-bearing (it *is* the Glass identity), never decorative.

The most expressive elevation material here is the **colored glow**: selection,
focus, live traffic, and STP state are all rendered as `box-shadow` / `drop-shadow`
halos in `color-mix(in oklab, …)`, so a running link, a selected node, and an
elected STP root each read at a glance.

### Shadow Vocabulary
- **`--shadow-sm`** (`0 1px 2px rgba(0,0,0,0.4)`): resting hairline lift on small controls.
- **`--shadow-md`** (`0 8px 24px rgba(0,0,0,0.45)` Bench): node faces, dialogs, popovers, the hover-popped chip.
- **`--shadow-lg`** (`0 18px 48px rgba(0,0,0,0.55)`): the largest modal surfaces.
- **`--shadow-ring`** (`0 0 0 3px var(--accent-muted)`): the accent focus/selection ring.
- **State glows** (inline `box-shadow` via `color-mix`): the running/starting/crashed
  LED halos, the cable traffic-glow, the golden STP-root pulse.

### Named Rules
**The Flat-At-Rest Rule.** A resting panel is defined by its tonal step and its
hairline border — not by a shadow. Shadows and glows are earned by state: hover,
selection, focus, elevation above the canvas, or live activity. A drop-shadow on a
static, unselected surface is a smell.

## 5. Components

The felt character is **tactile & responsive**: controls are precise and quiet at
rest, then come alive under the cursor — the connector affordance surfaces, the port
chip pops, the LED pulses, the cable glows. Nothing bounces; motion eases out fast
(120–180ms) and every animation has a `prefers-reduced-motion` alternative.

### Buttons
- **Shape:** gently rounded (8px, `--radius-md`); icon-only buttons are 28px squares.
- **Default:** `--panel-2` fill, hairline `--border-strong`, `--ink` text, weight 550;
  hover shifts the border to the accent (a quiet cue, not a fill change).
- **Primary:** accent fill, accent-ink text, weight 600 — the single loud action per bar (e.g. *Start lab*).
- **Danger:** transparent with a `--danger` border and text; hover tints the crashed-red wash.
- **Ghost:** transparent, borderless; hover picks up `--bg-hover`. For toolbar density.
- **Toggle (`.on`):** active toolbar toggles gain accent text + border + `--accent-muted` fill.

### Segmented control (Bench / Glass, import-export)
- A pill-shaped group (`--radius-full`) on a `--panel-2` track with a hairline border;
  inner buttons are transparent until active. The active segment fills with the accent
  and accent-ink text. Used for the theme switch and the I/O cluster.

### Pills / Chips
- **Pill:** full-radius, `--panel-2` fill, uppercase 12px label, `--ink-2` text, hairline border.
- **Status pill:** carries a leading LED dot that recolors + halos by provider state
  (connected/connecting/error).
- **Error pill:** crashed-red tinted (`color-mix` 14% fill, 55% border), dismissible.

### Inputs / Fields
- **Style:** solid `--panel-solid` fill, `--border-strong` hairline, 5px radius, 13px UI text.
- **Focus:** border shifts to the accent; the native outline is removed in favor of that
  border shift. Global `:focus-visible` is a 2px accent outline for keyboard users.
- **Inline field (lab name):** transparent until hover (border appears) / focus (panel fill + accent border) — an editable label that doesn't look like a form.

### Dialogs / Panels / Popovers
- **Corners:** `--radius-md` / `--radius-lg`. **Background:** `--panel` / `--panel-elevated`
  (translucent + blur on Glass). **Border:** hairline. **Shadow:** `--shadow-md`/`-lg`.
- Popovers escape canvas clipping via fixed/portal positioning, and honor a semantic layer order.

### Node face (signature)
- A 64px rounded-square (14px) "device" tile: a `160deg` gradient of `--node-face`
  → `--node-face-2`, hairline `--border-strong`, `--shadow-md`. A themeable SVG glyph
  (tintable via `currentColor`) sits centered; L2 devices tint violet, L3 the accent.
- **Selection / drop-target:** accent border + a `color-mix` accent ring (26% / 34%).
- **Status LED:** a 7px dot at the head of the node's **mono** name chip, recoloring
  and haloing by state (pulse on Starting).
- **Connector affordance (link-add):** a 20px accent circle pinned to the corner, at
  `opacity:0 scale(0.6)` at rest, animating to full on `.face:hover`, `:focus-visible`,
  or while linking — PNetLab's connect-on-hover, made native.
- **Artwork icons** render full-bleed (the icon *is* the node; tile chrome drops away),
  with the selection/drop rings re-asserted so they survive.

### Port chip (signature — the PNetLab hover-pop)
- An HTML label riding on each end of a link, showing the local interface (`Gi0/0`)
  in **mono**, sitting *on* the cable with an opaque background masking the stroke behind it.
- **Rest:** small (11–12px), low-contrast, unobtrusive.
- **Hover:** scales **1.65×** with a spring-ish ease (`cubic-bezier(0.2,0.9,0.3,1.2)`),
  swaps to the glass `--tooltip-bg`, gains an accent border + `--shadow-md`, and reveals
  the full detail (`R1 Gi0/0`), with `transform-origin` pointing back toward its node.
  Hovering either chip or the cable glows the whole link. Reduced-motion caps the pop at 1.18×.

### Cables (floating edges)
- 2px `--cable` strokes that re-anchor live to the node perimeter as nodes drag;
  parallel links fan apart on symmetric bezier curves.
- **Selected:** accent, 2.5px. **Capture-active:** amber + drop-shadow. **Hover/hot:**
  accent, 3.25px, accent drop-shadow. **Live traffic:** a soft blurred accent underlay
  whose width/opacity scale with `log(fps)`.

## 6. Do's and Don'ts

### Do:
- **Do** bias every neutral toward the accent hue — the slate ground `#0a0e14` is
  *chosen*, not default. Keep neutrals hue-tuned, never pure grey/black.
- **Do** set anything the CLI would print (hostnames, `Gi0/0`, telnet ports, RAM,
  config) in the **Cascadia mono** face; set chrome in the Segoe UI Variable face.
- **Do** show state as an **LED dot with a colored glow** and a label — shape + color + word.
- **Do** ration the accent to ≤10% of a screen: selection, focus, the one primary action, live links.
- **Do** keep surfaces flat at rest; let shadow/glow be earned by hover, selection, focus, or live activity.
- **Do** give every animation (hover-pop, LED pulse, cable re-anchor, STP-root glow,
  theme fade) a `prefers-reduced-motion` alternative — this is a hard requirement.
- **Do** verify AA contrast on **both** themes; the raised/darkened secondary and
  tertiary ink steps exist precisely so muted text still clears 4.5:1.
- **Do** consolidate stacking onto a **semantic z-index scale** (canvas → sticky topbar
  → panels → dropdown/menu → dialog → modal/preflight → tooltip). Give new layers a
  named step, not a fresh magic number.

### Don't:
- **Don't** reproduce the "AI-coded default": near-black `#0d1117`, a safe blue accent,
  rounded generic cards, one neutral sans for everything. That scaffold is what this system replaced.
- **Don't** build metrics-dashboard chrome — no hero-number tiles, no identical icon-heading-text card grids.
- **Don't** let the tool inherit the heavy, dated feel of PNetLab / EVE-NG / CML web
  consoles. Borrow their interactions, not their weight or their visual language.
- **Don't** convey status by a bare colored word with no LED shape behind it.
- **Don't** spend the primary accent on decoration; if two unselected/inactive things are accent-colored, one is wrong.
- **Don't** put a drop-shadow on a static, unselected surface. Flat at rest.
- **Don't** use glassmorphism decoratively — the Glass blur is load-bearing theme
  identity on floating panels only, never a default card treatment.
- **Don't** reach for arbitrary z-index values (`1000`, `1100`, `1200`, `2000`, `3000`,
  `999 !important`). The current spread is ad-hoc; new work must use the semantic scale above.
- **Don't** ship display type — the scale tops out at 20px on purpose. This is an instrument, not a landing page.
