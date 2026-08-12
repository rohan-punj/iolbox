# iolbox redesign — handoff status

**Update (2026-08-12, latest): three UI polish fixes, all live-verified on the VM.**

1. **Netprobe (PC) node icon was boxed while VPCS's identical default icon rendered
   full-bleed.** [VpcsNode.svelte](../app/src/lib/nodes/VpcsNode.svelte) has always had
   "artwork icon" handling (`isArtworkIcon()` → drop the tile background/border, render
   the glyph at 58px instead of 28px) but [PcNode.svelte](../app/src/lib/nodes/PcNode.svelte)
   never got it, even though both kinds share the same default `"pc"` icon key. Ported the
   same `artwork` derived/class/CSS into `PcNode.svelte`. Verified: PC0's `.face` now has
   identical computed style to PC1's (`background: none`, transparent border, 58×58 glyph).
2. **Shift+drag node-add now prompts for an exact count instead of guessing from drag
   distance.** The old `dragNodeCountStore` derived a count from how far you dragged past
   the drop origin while Shift was held (`1 + floor(|dx| / 110px)`), which is what produced
   the "auto-picks 4 or 5" complaint — precise counts were hard to hit by feel. Replaced
   with a `shiftHeld` boolean; `CanvasInner.svelte`'s `onDrop` now calls
   `prompt("How many nodes to add?", "1")` when Shift was held at drop time, clamps
   1–20, and cancelling the prompt aborts the drop instead of adding anything. The live
   drag badge changed from `×N` to a static "drop to choose count" hint. Verified end-to-end
   on the VM via a scripted `DragEvent` sequence with `window.prompt` mocked to return
   `"3"` — exactly 3 nodes were added (4→7), then cleaned up back to the original 4.
3. **Context-menu hover/focus highlight didn't fill the row.** `ContextMenu.svelte`'s
   `.item` button used `all: unset`, which resets `display` to its CSS-initial value
   (`inline`) since `display` isn't an inherited property — so each item shrank to its
   label's text width instead of the menu's full width, and the hover background only
   painted behind the text. Added `display: flex; align-items: center; width: 100%` to
   `.item`, and changed the submenu chevron's `float: right` to `margin-left: auto` (floats
   are ignored on flex children). Verified: a "Save" item now measures 180px inside a
   190px-wide menu (the 10px gap is just the menu's own 4px×2 padding) — was previously
   sized to its ~40px label. Applies to every menu built via `ContextMenu.svelte`
   (right-click node/link menus, the top-bar "..." menu, submenus).

All three: `svelte-check` clean (0 errors/warnings), rebuilt via `build-release.sh`,
redeployed + supervisor restarted, and live-verified against the VM's 4-node lab. Nothing
committed to git — ask before committing.

**Update (2026-08-12, later): "Terminate link" is now manual, not timed.** Per-request,
the 3s `forSec` auto-clear was removed — `terminateLink()` now sets `{down:true}`
permanently (no `forSec`), and the context-menu item toggles between "Terminate link" and
"Resume link" depending on `labStore.linkFaults[linkId]?.active && .fault?.down`. New
`labStore.resumeLink()` clears the fault (`setLinkFault(linkId, null)`, same as the old
Faults-dialog "Clear fault" button). `svelte-check` clean, live-verified on the VM: down
state now holds indefinitely (confirmed past 5s, well beyond the old 3s window), menu
correctly flips to "Resume link" while down and back to "Terminate link" after resuming.
One environmental snag hit during verification, not a bug in this change: right after a
supervisor restart, clearing a fault on a **stopped** lab can fail with `no static tap for
node N ethX` — `attachFabricLink`'s IOL case needs a static tap that's provisioned when
the lab actually starts, and apparently doesn't survive/exist across a fresh supervisor
restart while stopped (pre-existing code path, `clearFaultEffect` → `attachFabricLink`,
shared by the old auto-expiry timer and the Faults-dialog checkbox — not new). Starting
the lab (`Start lab`) provisions the taps and Resume link then works immediately. Not
otherwise investigated further since it's orthogonal to the terminate/resume behavior
change and reproduces identically via the pre-existing Faults dialog clear path.

**Update (2026-08-12): everything below "P5–P8" is committed.** The "nothing has been
committed" framing in the original log (kept below as history) is stale — a long live
bug-bash + feature session turned every fix into its own commit, then kept going past the
original P5–P8 scope. Current `main` tip: `e3e549d`. Full list since the P5–P8 redesign
landed (`678791a`), newest first:

| Commit | What |
|---|---|
| *(uncommitted)* | Terminate link → manual down (no auto-clear); context menu toggles "Terminate link" ⇄ "Resume link"; new `labStore.resumeLink()` |
| `e3e549d` | Link faults: `LinkFaultDialog.svelte` (editable form, replaces hardcoded presets + raw-JSON prompt), one-click "Terminate link" (down 3s via existing `forSec`, then auto-clears), red `.fault-badge` on node faces when an attached link has an active fault |
| `805ecaa` | netprobe CLI: up/down arrow command-history recall (`ESC [ A`/`ESC [ B`, `RuntimeState.History()`) |
| `0325acd` | **Real bug fix** — IOL "Detect IOL MAC addresses" always showed "this port relays for other devices" on every occupied switch port. Root cause: a switch's own static tap is a userspace-owned TAP char device, so its "received" direction is what the switch itself transmits (including everything it floods/relays), not the peer's traffic. Fix: read the *peer* endpoint's dirstat bucket, not the node's own. Live-verified on the VM. |
| `18647cf` | Two Impeccable P3s: `width`/`height` transitions → `transform: scaleX/scaleY` (ImageManager progress bar, SplitPane divider), FloatingEdge chip hover eased (removed overshoot bezier) |
| `d157b19`…`9daf2dd` | The original live bug-bash batch: auto-hide-chrome edge-only reveal, netprobe capability fix (`ip addr replace` `NET_ADMIN`), tool-pack catalog icons, a11y fixes from `/impeccable audit`, Settings dialog consolidation, rail icon fixes, console text-size controls, double-click-to-console, MAC popover portal-positioning fix, `tun` module autoload, netprobe console echo, resource-bar flex fix, chrome-hold infinite-loop crash, submenu positioning |

Deployment: appliance VM at `192.168.111.154` (SSH via dropbear, `plink.exe`/`pscp.exe -scp`
— **`pscp` needs `-hostkey "SHA256:Bl9tYZnmwxNXjOURTUbb4xwe4obPe4UN296GG1vShXg"` explicitly
or it hangs silently waiting on a host-key prompt with no stdin**, root-caused this
session after two uploads deadlocked for ~8 minutes with zero CPU usage). Builder for a
full rootfs rebuild: `192.168.226.10`. Redeploy pattern (supervisor binary only, GUI is
embedded via `bash build-release.sh`):

```bash
bash build-release.sh   # from repo root; stamps version, builds GUI, cross-compiles linux/amd64
pscp -scp -pw iolbox -hostkey "SHA256:Bl9tYZnmwxNXjOURTUbb4xwe4obPe4UN296GG1vShXg" \
  supervisor/bin/supervisor-linux-amd64 root@192.168.111.154:/opt/iolbox/supervisor.new
plink -ssh -batch -hostkey "SHA256:Bl9tYZnmwxNXjOURTUbb4xwe4obPe4UN296GG1vShXg" -pw iolbox \
  root@192.168.111.154 "chmod 0755 /opt/iolbox/supervisor.new && mv -f /opt/iolbox/supervisor /opt/iolbox/supervisor.old && \
  mv /opt/iolbox/supervisor.new /opt/iolbox/supervisor && systemctl restart iolbox-supervisor && \
  sleep 2 && systemctl is-active iolbox-supervisor && rm -f /opt/iolbox/supervisor.old"
```

`systemctl restart` reliably takes the ~90s graceful-shutdown-timeout → SIGKILL path
against this supervisor (it owns many child processes/goroutines) — run it
`run_in_background`, expect the SIGKILL log lines, and check
`ip link show | grep -E 'iol|nat|vtool'` afterward for orphaned kernel taps if a lab was
running at the time. The netprobe (PC node) binary, `pc-gui`, is a **separate** binary from
the main supervisor — cross-compile it separately
(`cd runtime/files/tools/packs/pc/gui && GOOS=linux GOARCH=amd64 go build -o pc-gui .`) and
push to `/opt/iolbox/tools/packs/pc/pc-gui`, then restart that one node (not the whole
supervisor) via its GUI Restart button, if you change anything under
`runtime/files/tools/packs/pc/gui/`.

As of this session's end, the VM is running the `e3e549d` build (deployed, service
confirmed `active`, GUI loads with no console errors and no error banner). The lab loaded
on the VM has 4 nodes (PC0, PC1, SW2, SW2-2) wired PC0–SW2/e0/0, PC1–SW2/e0/1,
SW2-2/e0/0–SW2/e0/2; it was left **stopped** (not running) at last check.

## `e3e549d` live-verified (2026-08-12)

All three link-fault features were click-through verified against the VM's real 4-node lab
(PC0/PC1/SW2/SW2-2, stopped), using `javascript_tool` synthetic `PointerEvent`/`MouseEvent`
dispatch since `computer`/`read_page` are gated on this private-IP origin. No dev-server
shortcuts, no fake lab — real DOM, real websocket round-trips to the supervisor.

1. **Context menu** — right-clicking a link edge (`.edge-hover-catch` hit-catcher) shows
   `Faults…` and `Terminate link` as separate top-level `role="menuitem"` buttons, not
   nested under a submenu. Confirmed.
2. **`LinkFaultDialog.svelte`** — clicking `Faults…` opens `role="dialog"
   aria-label="Link fault"` with the Target segmented control (Both ends / PC0 eth1 / SW2
   e0/0), the Administratively down checkbox, and all six impairment fields
   (Delay/Jitter/Loss/Rate/Duplicate/Reorder). Set `lossPct=10`, clicked Apply → the edge
   picked up a `10% · both ends` label and both endpoint nodes (PC0, SW2) grew a
   `.fault-badge` (`!`, title "An impacted link is attached to this node"). **Re-opened the
   dialog and confirmed it re-seeds**: Loss field showed `10`, Target showed `Both ends` as
   active — the actual replacement behavior for the old preset menu works. `Clear fault`
   closed the dialog and removed both the edge label and both badges cleanly.
3. **Terminate link** — clicking it on a different link (`link-1`, PC1↔SW2) applied
   `is-admin-down` on the edge path plus 2 `.fault-badge`s within ~200ms, held for ~3s
   (polled every 200ms inside a single `javascript_tool` call to avoid tool-round-trip
   timing gaps — the first two attempts happened to poll after the 3s auto-clear had
   already fired, which looked like nothing applied at all until the poll loop was made
   tight enough to catch the window), then both cleared automatically at ~3.1s via the
   existing `forSec` expiry. No new code involved in the clear path, but this is the first
   time it was watched end-to-end with the new UI in front of it.
4. **Clean state** — confirmed no lingering fault, no badges, no `is-admin-down` classes on
   any of the 3 links after both tests.

No rebuild or redeploy was needed — `e3e549d` as already deployed on the VM works
correctly. `import()`-ing `labStore.svelte.ts` from the console to seed a fake lab is
still a dead end (gets a separate module instance) if this needs re-verifying without a
VM lab available — use a real lab instead, as done here.

## Older history (P5–P8 planning + implementation)

Plan docs, in dispatch order: `docs/p5-netprobe-netsvc-impairment-plan.md` →
`docs/p6-protocol-lens-interface-suggest-console-workspace-plan.md` →
`docs/p7-floating-console-mac-toggle-plan.md` → `docs/p8-workspace-redesign-plan.md`.
Each was Opus-drafted, adversarially reviewed by `codex sol-medium`, had every finding
applied, then implemented batch-by-batch by `codex luna-xhigh`. Idea backlog (the source
of P5–P8, plus unplanned ideas #4/#5/#6) is `docs/learning-features-gui-ideas-plan.md`.

### Status: all four plans (P5–P8) fully implemented and committed (see table above)

#### P5 — `netprobe` (PC node), `netsvc`, link fault injection

All three batches (A: PC node, B: `netsvc`, C: link impairment) landed and pass
`go build`/`go vet`/`go test` and `npm run check`/`build`. The netprobe capability bug
(`ip addr replace` "Operation not permitted") found during this session's live-VM pass was
a real gap in this batch — fixed in `a8ee811` (see table above) and live-verified.

#### P6 — Protocol Lens, next-free-interface suggestion, learner console workspace

Batch 8 (interface picker), Batch 9 (tiled/tabbed console dock), Batch 7 (Protocol Lens,
the never-guess MAC-attribution channel in `supervisor/internal/dirstat`) all landed. The
never-guess attribution channel is exactly what `0325acd` above fixed — it was reading the
wrong endpoint's bucket for a switch's own port, which this session found and fixed via
real live traffic on the VM.

**Post-landing fix:** the `tile2`/`tile4` CSS layout bug (two tiled panes rendered
half-height instead of side-by-side) was found and fixed directly — `Console.svelte:813-820`.

#### P7 — Floating console windows (superseded by P8), per-node MAC list

Floating consoles were superseded by P8 Batch 15 (reused P7's reviewed design). MAC-list
batches (11a VPCS/PC, 11b IOL via P6's dirstat channel) landed; the IOL MAC popover UI
itself later needed its own portal-positioning fix (`ea79aee`) and, this session, the
underlying dirstat-attribution bug fix (`0325acd`).

#### P8 — Full workspace redesign (top bar, rail, floating consoles, link routing, auto-hide)

All nine batches (12–19) landed and were live-verified per-batch during the original
implementation pass, then extensively iterated on further in this session's live bug-bash
(see the commit table at the top — most of `9daf2dd` through `d157b19` are P8-surface
fixes found by actually using the redesigned GUI against a real VM).

## What's next

1. **Verify `e3e549d` live** (see "OWED" section above) — the actual next action.
2. **Two remaining Impeccable P3 findings were explicitly left alone, not forgotten**:
   the `stroke-width` transitions on `FloatingEdge.svelte`'s cable path (line ~638) and
   traffic glow (line ~682) are SVG paint properties on a `<path>`, not CSS box
   dimensions — they don't cause HTML layout reflow the way `width`/`height` do, and
   there's no `transform` equivalent for line thickness, so the detector's finding there is
   a false positive. No action needed unless that judgment gets revisited.
3. **Deeper live-VM checklists from the original P5–P8 plans** (P5 §9, P6 §10, P7 §10, P8's
   own verification section) still haven't been run item-by-item — Free↔Structured link
   routing on a lab with parallel links, four-edge viewport-pan chip clamping, Glass theme
   on every new surface, keyboard-only navigation, reduced-motion emulation. Lower priority
   than #1 above; nothing here is a known bug, just unexercised surface area.
