# iolab roadmap — phases

Where we are and what's next. The scaffold (supervisor, runtime, GUI shell,
capture path, CI) is **complete and committed**. Two tracks now run in parallel:
a **design/UX track** that needs no hardware, and a **runtime track** gated on a
real IOL image.

## Status legend
✅ done · 🔜 ready to start · ⏳ blocked on a real IOL image · ◻ later

## Track 1 — Design & canvas UX (no hardware needed, start now)

Implements `docs/design-brief.md`. All frontend; verifiable in the browser preview.

| Phase | Scope | Model | Depends on |
|---|---|---|---|
| **D1 — Theme system** 🔜 | Two themes (Bench dark / Glass light) via `data-theme` token blocks; `ThemeProvider` + persisted toggle in the top bar; migrate every component off hard-coded colour to tokens | Sonnet | — |
| **D2 — Floating edges** 🔜 | `getEdgeParams` perimeter-intersection helper; custom `FloatingEdge`; links exit the side facing the neighbour and re-anchor on drag | Opus | — |
| **D3 — Hover-pop labels** 🔜 | HTML interface chips via `EdgeLabelRenderer`; small at rest, scale ~1.6× + glass tooltip on hover; show `iface · telnet NNNNN`; reduced-motion fallback | Sonnet | D1 |
| **D4 — Icon system** 🔜 | Bundled licence-clean SVG set; per-node `icon` override; icon-picker popover (grid + Import SVG); tint via `currentColor` | Sonnet | D1 |
| **D5 — Canvas polish** 🔜 | Confirm infinite pan (remove `fitView` clamp, no `translateExtent`); PNetLab dot grid tuned per theme; reset-view / fit-to-content; grid/cross variants as a setting | Sonnet | D1 |
| **D6 — Design QA** 🔜 | Against the "doesn't look AI-coded" checklist; both themes; keyboard focus; `prefers-reduced-motion`; screenshot set | Fable | D1–D5 |

D1 and D2 can start together (different files). One integration pass at the end.

## Track 2 — Runtime bring-up (gated on your IOL image)

| Phase | Scope | Model | Depends on |
|---|---|---|---|
| **P0 — Spike** ⏳ | `docs/p0-spike.md`: boot IOL, console, wire 2×IOL+VPCS, ping, Wireshark tee; confirm the pinned assumptions | Fable+Opus | **a real IOL `.bin`** |
| **P1 — Control plane** ◻ | Implement `TcpTransport` (currently a stub) over Tauri; replace MockTransport; wire real provider bodies (vmware first) | Opus | P0 |
| **P2 — Live consoles/capture** ◻ | Real telnet consoles in xterm; `capture-helper` launch from the app | Sonnet | P1 |
| **P3 — Config & images** ◻ | NVRAM save/extract UI; image sync into the runtime; lab-pack import UI | Sonnet | P1 |

## Track 3 — Browser option (your "have it as a browser option too")

The frontend is already a Vite/Svelte app that runs in a browser (that's how the
preview verified it). A browser target is nearly free because the transport is
already abstracted (`MockTransport` / `TcpTransport`). Browsers can't open raw
TCP, so:

| Phase | Scope | Model | Depends on |
|---|---|---|---|
| **B1 — WS bridge** ◻ | Add a WebSocket listener to the supervisor (or a tiny local proxy) that fronts the NDJSON control protocol; consoles over WS too | Opus | P1 |
| **B2 — `WsTransport` + web build** ◻ | Third transport; `npm run build` served locally; same UI, same themes; "open in browser" from the app | Sonnet | B1, D1 |

Result: **one frontend, three shells** — Tauri desktop, browser tab, and the mock
demo — differing only in transport. No UI fork.

## Recommended sequencing

1. **Now:** kick off **D1 + D2** (Sonnet + Opus in parallel), then D3–D5, then D6.
   This lands the whole visual redesign without needing hardware.
2. **When you supply an IOL image:** run **P0**, then P1→P2→P3.
3. **After P1:** B1→B2 for the browser option.

Design and runtime are independent, so the redesign can be done and merged while
the IOL image is still being sourced. One release build at the very end of a track,
not mid-stream.
