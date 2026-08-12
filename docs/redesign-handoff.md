# iolbox redesign — handoff status

Live log. Nothing described here has been committed to git — everything is sitting as
uncommitted working-tree changes across `supervisor/` and `app/`. No live-VM verification
has been performed for anything below beyond a manual mock-transport browser pass noted at
the bottom; every batch's own implementation report says so explicitly where it applies.

Plan docs, in dispatch order: `docs/p5-netprobe-netsvc-impairment-plan.md` →
`docs/p6-protocol-lens-interface-suggest-console-workspace-plan.md` →
`docs/p7-floating-console-mac-toggle-plan.md` → `docs/p8-workspace-redesign-plan.md`.
Each was Opus-drafted, adversarially reviewed by `codex sol-medium`, had every finding
applied, then implemented batch-by-batch by `codex luna-xhigh`. Idea backlog (the source
of P5–P8, plus unplanned ideas #4/#5/#6) is `docs/learning-features-gui-ideas-plan.md`.

## Status: all four plans (P5–P8) fully implemented

## P5 — `netprobe` (PC node), `netsvc`, link fault injection

**Fully implemented, locally verified.** All three batches (A: PC node, B: `netsvc`, C:
link impairment) landed and pass `go build`/`go vet`/`go test` and `npm run check`/`build`.
Not verified: live-VM dataplane behavior (real ping/DHCP/DNS/TFTP traffic, `sch_netem`
module availability, VPCS regression).

## P6 — Protocol Lens, next-free-interface suggestion, learner console workspace

**Fully implemented, locally verified.** Batch 8 (interface picker), Batch 9 (tiled/tabbed
console dock — `consoleUiStore` gained `tiles`/`focused`/`pinned`/`reconcile()`/marks),
Batch 7 (Protocol Lens — `lens.ts`, `LensPane.svelte`, the never-guess MAC-attribution
channel in `supervisor/internal/dirstat`) all landed. Not verified: live traffic through a
real switch (the never-guess attribution claim), real DHCP/OSPF/HSRP/VLAN traffic.

**Post-landing fix:** the `tile2`/`tile4` CSS layout bug (two tiled panes rendered
half-height instead of side-by-side) was found and fixed directly —
`Console.svelte:813-820`, confirmed live in-browser.

## P7 — Floating console windows (superseded, see P8), per-node MAC list

**Plan fully reviewed/fixed. Batch 10 (floating consoles) was NOT implemented as P7 — P8
Batch 15 superseded it, reusing its reviewed design.** MAC-list batches:

- **Batch 11a (VPCS/PC MAC data path + popover UI) — landed.** `node.macs` verb,
  `node.VPCSMAC` in Go, `MacListPopover.svelte` on the hover toolbar. **Live-verified**:
  the button appears on the hover toolbar, the popover opens and correctly shows
  `eth1 — node not running` for a stopped PC node.
- **Batch 11b (IOL MACs via P6 Batch 7's channel + opt-in toggle) — landed**, with a
  placement correction made live during dispatch: the plan's text said to add a dedicated
  `TopBar.svelte` button "next to the fullscreen button" — stale, since P8 Batch 12 had
  already restructured the top bar. Implemented instead as a `checked` item in the
  overflow menu's View group (`macUiStore` + `view-learned-mac-display`).

## P8 — Full workspace redesign (top bar, rail, floating consoles, link routing, auto-hide)

**All nine batches landed.** Merge order per the plan: **12 → 13 → 14(+14a) → 15 ∥ 16 →
17 → 18 → 19.**

| Batch | Scope | Status |
|---|---|---|
| 12 | Slim top bar + three-dot overflow menu | ✅ Landed, live-verified |
| 13 | Bottom resource bar (CPU/RAM/Connection/Elapsed) | ✅ Landed, live-verified — disk cell dropped, confirmed intentional by user |
| 14 | Left icon rail, replacing `Palette.svelte` (deleted, 832 lines redistributed) | ✅ Landed, live-verified |
| 14a | Selected-node action toolset, incl. Restart (review reversed the original "no Restart" decision) | ✅ Landed, live-verified — Restart appears only while running, right after Console |
| 15 | Floating console windows (supersedes P7 Batch 10) | ✅ Landed, live-verified — opens floating by default with pin/minimize/close + resize |
| 16 | Structured (orthogonal) link routing alongside Free Flow | ✅ Landed — all gates pass (flow-purity, `pts` internal-only, no arc-length leakage). Not live-verified (needs a multi-node lab with parallel links) |
| 17 | Link hover/selection + endpoint interface labels + chip clamping | ✅ Landed — 3 files, ~70 lines, both grep gates pass. Not live-verified |
| 18 | Auto-hide chrome | ✅ Landed — 12 files, default off, both hard-to-observe suppression cases (ContextMenu lifetime, SplitPane drag) confirmed via grep gate. Not live-verified (needs a running lab + idle wait) |
| 19 | Visual pass + `DESIGN.md` alignment | ✅ Landed — `DESIGN.md` colors block byte-identical (confirmed via SHA-256), 8 semantic z-index tokens added, all in-scope raw z-index literals tokenized, Mono-For-Data gaps fixed, reduced-motion count 6→14 |

**Live user correction applied mid-pipeline:** the fullscreen toggle was pulled back out
of the overflow menu into the main top bar as a visible icon button (`TopBar.svelte`),
overriding Batch 12's original "exactly six things" framing — the plan doc's §7.1/§7.6
were updated to match (now "seven things", gate grep count is 2 not 1). Live-confirmed
present in the main bar.

**Known non-regressions confirmed by each batch's own acceptance-gate greps** (not all
independently re-verified by a human): `Palette.svelte` fully deleted with no dangling
references; `labStore.svelte.ts` diff confined to exactly `restartNode` +
`acquireNodeLock`'s new flag; `inspectorNodeId` vs `selectedNodeId` conflation (review
finding 6) fixed and grep-gated in `NodeActions.svelte`.

## Manual live-browser pass (post-Batch-19)

Ran a real click-through against `npm run dev` (mock transport, `http://localhost:1420`):
preflight dialog → click-to-place a PC node (viewport-center placement confirmed) → node
count updated live in the top bar → hovered the node, opened MAC addresses popover (shows
`eth1 — node not running`) → started the lab → Restart button appeared post-start, right
after Console → opened the console → **it opened as a floating window by default**, with
`dialog "PC0"`, a `toolbar "PC0 window controls"` (Pin/Minimize/Close), a resize handle,
and the "Return consoles to the dock" launcher escape hatch, all present. **Zero console
errors** through the entire flow. This confirms the redesign holds together end-to-end,
not just per-batch in isolation — though it does not replace the deeper per-criterion
checks (Structured routing, auto-hide's 2s idle timer, chip clamping at viewport edges,
Glass theme on new surfaces) that each batch's own testing bar calls for and that still
have not been run live.

## What's next

The implementation phase (P5 through P8) is complete. Remaining work is verification and
process, not more code:

1. **Deeper live-browser verification** — the quick pass above proved the happy path
   works, but each batch's own testing bar has specific checks not yet run live:
   Free↔Structured toggling on a lab with parallel links and byte-identical `lab.links`
   before/after; the four-edge viewport-pan chip-clamping test; auto-hide's actual 2s
   idle-then-hide behavior and the two named 5s-hold suppression exercises; Glass theme
   on every new surface (rail, flyout, floating window, resource bar); keyboard-only
   navigation through the rail and overflow menu; reduced-motion emulation.
2. **Live-VM validation** — none of P5–P8's live-VM checklists (P5 §9, P6 §10, P7 §10, P8's
   own verification section) have been run. This needs the actual appliance VM, not the
   mock transport used above (real IOL/VPCS/PC processes, real traffic, `sch_netem`
   availability, etc.).
3. **Git strategy** — nothing has been committed anywhere across P5–P8. Needs a decision on
   staging granularity (per-batch, per-plan, or one consolidated commit) — not yet
   discussed with the user.
