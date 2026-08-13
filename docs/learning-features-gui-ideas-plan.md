# iolbox — learning-friendly feature & GUI ideas (plan / idea backlog)

Status: **idea backlog.** Several of these (netprobe, link faults/impairment,
Protocol Lens, next-free-interface suggestion, the tiled/floating console
workspace, per-node MAC list) have since shipped — see each pack/feature's
own code and `README.md`'s Status section for current state. The remaining
ideas below (named baselines/config-diff, guided checkpoints/workbook,
`bgppeer`) are still unscoped backlog. Grounded in the current architecture
(single Go supervisor binary, browser-first Svelte 5 + Svelte Flow GUI,
tool-node packs = one static Go binary each, no Docker, no DB).

Existing tool-node packs for reference: `aaa` (RADIUS+TACACS+), `webserver`,
`httpclient`, `syslog`, `secbench` (18 Go-ported attack/recon modules).

---

## Feature ideas (new tool-node packs / supervisor features)

| # | Idea | Learner problem it solves | Fit | Cost |
|---|------|---------------------------|-----|------|
| 1 | **`netprobe`** — configurable Go host node (ping/traceroute/DNS lookup/TCP+UDP connect+listen/echo/repeated flows, ARP+DHCP-lease viewer) | No lightweight "PC" today to test ACLs, NAT, PBR, QoS, DHCP relay, DNS from — just IOL/VPCS | One static Go binary, same pattern as existing packs | Medium |
| 2 | **`netsvc`** — combined DHCP+DNS+NTP+TFTP service node | Branch-services labs need 3-4 idle routers today just to act as infra | One binary, reuses the netns `ip_unprivileged_port_start` trick already proven for TACACS+ | Medium |
| 3 | **Link faults / impairment** (`tc netem`: delay, jitter, loss, down/up, scheduled/momentary failure) | STP/HSRP/OSPF convergence labs need a way to break links realistically | Supervisor-side (apply netem to the existing veth/relay), lab-JSON field, canvas fault indicator (disabled vs. impaired vs. unexpectedly-down) | Medium |
| 4 | **Named baselines + config diff** on top of existing NVRAM save/restore | Beginners avoid experimenting because "undo" is unclear | Extends what already exists (NVRAM + lab JSON) — no new subsystem | Light–medium |
| 5 | **Guided checkpoints / workbook** — objectives + pass/fail checks (see full list in the design-detail section below) attached to a lab, run over `netprobe`'s machine-only API — never the human-visible CLI, so a check's own traffic never appears typed-out in a learner's console — with a lab-editor UI for authoring checks by hand | Turns any lab into a self-checking exercise, no LMS/DB needed | Lab-JSON schema + supervisor-side checks using `netprobe`/capture as the test engine | Medium (depends on #1) |
| 6 | **`bgppeer`** — scoped Go BGP speaker (session, prefix advertise, AS-path/MED/communities, message log, received-route table) | CCNP BGP labs burn several IOL routers just to be neighbors | One binary, outbound-only avoids the privileged-port issue entirely | Medium–heavy |

## GUI/UX ideas

| # | Idea | Fit | Cost |
|---|------|-----|------|
| 7 | **Protocol Lens** — plain-English event timeline next to the existing Wireshark capture ("R2 sent OSPF Hello", "DHCP Offer 10.1.1.12"), presets for ARP/DHCP/STP/CDP/OSPF/HSRP, click-to-filter by node/VLAN | Pure-Go parsing of a known protocol subset in the supervisor — no libpcap/cgo added | Medium |
| 8 | **Next-free-interface suggestion on drag-connect** — when a link is dragged onto a node, the interface picker pre-selects/highlights the next interface that has no existing link, instead of listing all interfaces flat | Planning-metadata-free — reads existing link state only, no new schema | Light |
| 9 | **Learner console workspace** — 2-4 tiled consoles, pinned compare pane, searchable history, "mark capture now" syncing a timestamp into both console history and the packet timeline | Extends existing xterm.js dock, no backend rework | Medium |
| 10 | **Floating console windows** — each open console can be dragged anywhere on screen instead of being confined to the one shared bottom/right dock | New reusable draggable-window primitive (none exists yet); frontend-only, xterm content unchanged | Medium |
| 11 | **MAC address list — per node, with an IOL-parsing toggle** — a per-node MAC list (button on the node hover toolbar, `NodeActions.svelte`, alongside the existing Console button — original idea #11 shape, not a canvas-wide overlay) listing every interface's MAC (`Gi0/0: aabb.cc00.0100`). PC/VPCS entries are always populated — iolbox derives those MACs itself, `argv.go:150-173`, no parsing needed. IOL routers/switches compute their own MACs internally with no supervisor-side formula to replicate, so IOL entries require live source-MAC learning off the capture path — and because that parsing has a real always-on cost, it is gated behind its own separate toggle (off by default; PC/VPCS are unaffected by this toggle either way) | Two-tier: PC/VPCS trivial and always-on; IOL requires wiring into the capture/dirstat pipeline, opt-in via its own toggle, and must degrade to "unknown" rather than guess, same never-guess rule as Protocol Lens attribution | Medium |

**Suggested order:** next-free-interface (8) → `netprobe` (1, unlocks everything downstream)
→ link faults (3) → baselines/diff (4) → guided checkpoints (5) → console
workspace (9) → floating windows (10) → Protocol Lens (7) → `netsvc` (2) →
`bgppeer` (6, save for last — biggest).

---

## `netprobe` as a VPCS replacement

Question raised: can `netprobe` (idea #1) replace the bundled VPCS node, and
can it get VPCS-style config commands (`ip 10.0.0.1/24 <gw-ip>`, `show ip`,
`ping`, `save`, etc.)?

**Yes — and it's a natural fit, because `netprobe` is a strict superset of
what VPCS does for a learner.** VPCS today is just a lightweight endpoint with
a telnet-style CLI; `netprobe` covers the same ground (static/DHCP addressing,
ping, traceroute) plus DNS, TCP/UDP connect/listen, ARP/lease inspection, and
repeated-flow generation — all useful for the exact CCNA/CCNP scenarios this
project targets.

### Why VPCS is architecturally different from the other tool-node packs

Per `supervisor/internal/node/spawn_linux.go`: VPCS is **its own telnet
server** — it opens `Spec.ConsolePort` itself and serves a `VPCS>` prompt
directly (no pty, no supervisor-managed console_hub). This is a different
process model from every existing tool-node pack, which instead runs headless
in a netns and is configured via the wsbridge-proxied mini web GUI
(`/tool/{id}/...`), with no console port at all.

To feel like a genuine VPCS replacement, `netprobe` needs to **support both**
of these interaction models, not just the tool-node web GUI:

1. **Console CLI (VPCS-compatible surface)** — `netprobe` binds
   `Spec.ConsolePort` itself (same convention as `spawnVPCS`) and serves a
   small line-oriented CLI over that TCP port, going through the same
   `console_hub`/webconsole/native-telnet path every other node already uses.
   Minimum VPCS-parity command set:
   - `ip <addr>/<prefix> <gateway>` — set static IPv4 + default gateway
   - `ip dhcp` — DHCP-configure the interface
   - `show ip` — current address/gateway/DHCP-lease state
   - `ping <host> [-c count]`, `trace <host>`
   - `save` — persist current config as the node's startup state (mirrors
     VPCS's own `save`, and plugs into idea #4's baseline/diff work for free)
   - `reset` — clear to unconfigured
   - `?` / `help` — **VPCS parity, don't skip this**: real VPCS prints a
     one-screen command legend on a bare `?`, which is how a first-time user
     discovers the whole CLI without external docs. `netprobe`'s `?` should
     list every command below (base + extensions) with a one-line
     description each, grouped (addressing / diagnostics / services /
     config), same spirit as VPCS's own help screen. This is small to build
     (a static help table keyed off the command dispatcher) but matters a
     lot for a learner tool — it's the in-CLI discoverability mechanism, not
     an afterthought.
   Plus `netprobe`-only extensions in the same CLI style once the basics are
   solid: `dns <name>`, `tcp connect/listen <port>`, `udp send/listen <port>`,
   `flow start <dst> <rate> <size>`, `arp show`.
2. **Web GUI panel** (existing tool-node convention) for the richer views —
   ARP table, DHCP lease detail with options, DNS answer detail, flow
   generator controls, TCP/UDP echo/log — everything that doesn't map to a
   single terse CLI verb.

Both surfaces read/write the same in-process state, so a learner can configure
via typed commands (muscle memory carried over from VPCS/real Cisco gear) and
then flip to the web panel to see the same state visualized, or vice versa.

### User-facing naming: never say "netprobe" in the UI

`netprobe` is an internal codename only (same pattern `secbench`'s
`pack.json` already uses: `"id": "secbench"` vs. `"name": "Security Bench"` —
the id is for developers/config, the `name` field is what learners see).

- **Palette label / default node name: "PC"**, not "VPCS" and not
  "netprobe". VPCS is the underlying tool's brand name, not a description of
  function, and a learner has no reason to know what VPCS is. "PC" is
  actually already the de-facto identity today — the existing VPCS palette
  entry already defaults every dropped node's name to `PC{id}`
  (`CanvasInner.svelte:484`) even though the palette chip itself currently
  says "VPCS" (`Palette.svelte:223`) — the label just hasn't caught up to
  the default name yet.
- Put the "what does this actually do" explanation in the palette
  tooltip/description instead of the chip label, e.g. *"Virtual PC —
  addressing, ping/traceroute, DNS, TCP/UDP tools."*
- When `netprobe` is ready to become the palette's "PC" entry, this is a
  same-label swap (the chip already says roughly the right thing via the
  default node name) — not a rename migration that would confuse existing
  users or saved labs.

### CLI ⇄ GUI: two coexisting entry points, not a mode switch

Not a toggle a learner has to flip — both surfaces are just always
available, mirroring two patterns that already exist independently in the
codebase today:

- **Console button** — from `NodeActions.svelte` (already shared by
  IOL/VPCS nodes): opens the existing console dock (xterm), same
  infrastructure VPCS already uses for its telnet CLI.
- **GUI button** — from `ToolNode.svelte`: a small `gui-button` on the node
  face that opens an in-canvas `tool-panel` overlay, an `<iframe>` pointed
  at the proxied `/tool/{id}/` config page (the same mechanism every other
  tool-node pack's config GUI already uses).

The PC node gets **both buttons on one node face** — nothing new to build for
the switching mechanism itself, just compose the two existing UI pieces onto
a node type that currently only has one of them. Since both read/write the
same live process state, a learner can type `ip 10.0.0.1/24 10.0.0.254` in
the console, then click the GUI button and see that same address reflected
immediately — no explicit "switch mode" action, no state to reconcile, just
click whichever view is useful at that moment. Both can be open side by side.

### Architecture: dedicated node kind, not a generic `tool` pack

PC/`netprobe` is expected to be the **most common node on the canvas** — the
default "start every lab with one of these" node, dropped many times per
session. That expected frequency is itself the reason it should NOT go
through the generic manifest-driven `tool` pack system (`pack.json`,
options-schema fields, `proxyRoutes`, the pack-selector dropdown in
Inspector). That machinery exists to make arbitrary, occasional, swappable
packs pluggable (`aaa`/`webserver`/`httpclient`/`syslog`/`secbench` — all
collapsed into one shared "Network tools" palette entry per the existing
convention). PC isn't one of several interchangeable options a learner picks
from a dropdown; it's a foundational node type, same tier as `iol`/`vpcs`/`nat`
today, and should get the same first-class treatment:

- **New `NodeKind: "pc"`** in `labTypes.ts`'s `NodeKind` union
  (`"iol" | "vpcs" | "nat" | "tool"` → add `"pc"`), not routed through
  `kind: "tool", pack: "netprobe"`.
- **Own top-level palette entry**, next to IOL/VPCS/NAT — not folded into the
  shared "Network tools" chip the other 5 packs use.
- **Own Svelte component** `PcNode.svelte` — essentially `VpcsNode.svelte`'s
  face/console wiring merged with `ToolNode.svelte`'s `gui-button`/
  `tool-panel` overlay pattern, rather than reusing the generic
  manifest-driven `ToolNode.svelte` (which is built for arbitrary
  options-schema fields PC doesn't need).
- **Own backend spawn path** `spawnPC` in
  `supervisor/internal/node/spawn_linux.go`, modeled on `spawnVPCS` (binds
  `ConsolePort` itself, serves the CLI directly) but also serving its small
  embedded config GUI on loopback, proxied via a plain `/pc/{id}/` route —
  skipping the full pack-manifest/`proxyRoutes`/options-schema system that
  exists purely for pluggable third-party-feeling packs.
- **Own lab-JSON shape** — `ip`/`gateway`/etc. as first-class node fields
  (like VPCS's canned-commands doc field today), not a generic pack config
  blob.

**Cost note**: `kind` is pattern-matched in enough places that this is a
medium, not light, lift — `CanvasInner`'s node-type registry,
`InterfacePicker`, `LabBrowser`'s color map, `icons.svelte.ts`,
`interfaces.ts`, `mockTransport.ts`, `clab.ts` import/export, plus
supervisor-side `lab.go`/`validate.go`/`netmap.go` all switch on node kind
today and would need a `"pc"` case. It's a one-time cost that buys the
simplicity the project's "keep it lightweight" ethos wants for the node
that'll appear dozens of times per lab — worth paying once rather than
carrying manifest/proxy indirection on the hottest path in the app forever.

### Practical rollout

- Ship the `"pc"` kind **alongside** VPCS first (new palette entry), not as a
  replacement — prove out the console-CLI-plus-GUI-overlay combination on a
  real lab before touching the existing VPCS code path or default palette.
- Once the CLI covers the VPCS-parity command set above and has been
  live-verified against a real lab (same bar as every other feature in this
  project: build, deploy to the VM, run a real topology, not just unit
  tests), consider making `"pc"` the default in the palette and demoting
  VPCS to "legacy/lightweight" rather than deleting it outright — VPCS is a
  real bundled third-party binary with its own value (near-zero resource
  use), and some learners' saved labs already reference it by content id.
- Raw ICMP for `ping`/`trace` needs the same narrow-privilege treatment as
  everything else in this project: scope `net.ipv4.ping_group_range` to the
  PC node's own netns rather than broadening ambient capabilities
  host-wide.

---

## Floating console windows (idea #10, design detail)

Checked the current implementation before scoping this: `consoleUiStore.svelte.ts`
has a single shared `DockSide = "bottom" | "right"` — every open console is a
tab inside **one** panel docked to one screen edge (`SplitPane.svelte` handles
drag-*resize* of that panel's edge, nothing more). There is no drag-*move*
primitive anywhere in the codebase today — `CanvasInner`/`Palette` do
node/palette drag-and-drop, `AnnoLine`/`AnnoShape` do annotation-handle
dragging, but nothing repositions an arbitrary panel around the screen. So
"draggable console windows, not bound to bottom/right" is a genuinely new UI
primitive, not a tweak to the existing dock — worth being upfront about that
cost rather than presenting it as small.

**Proposed shape**: a third mode, `DockSide: "bottom" | "right" | "floating"`
(persisted the same way as today), toggled from the same control that
currently switches bottom↔right. In floating mode, each open console becomes
its **own** independent window instead of a tab in the shared dock:

- New `FloatingConsoleWindow.svelte` — title bar (node name, LED state,
  close button) + the same `ConsoleTerm.svelte` xterm content the dock
  already renders unchanged (the terminal itself doesn't care about its
  container's position, so no console/websocket logic changes).
- **Drag-move**: pointerdown on the title bar → track pointer delta → update
  the window's `{x, y}` in a small per-window state map in
  `consoleUiStore`. Same `pointermove`/`pointerup` pattern already used for
  annotation dragging in `AnnoLine.svelte`/`AnnoShape.svelte`, just applied
  to a panel instead of an SVG handle.
- **Resize**: a corner grip reusing the drag-math `SplitPane.svelte` already
  has for its one edge, generalized to two axes.
- **Stacking**: click-to-front bumps a shared z-index counter (same idea as
  node z-ordering already used on the canvas for hover-to-front).
- **Persistence**: last position/size per node id, so reopening a console
  reopens where the learner left it (localStorage, same idiom as
  `dockSide`/`fontSize` today).
- **Bounds**: clamp `{x, y}` so a window's title bar can never drag fully
  off-screen and become unreachable — a real gotcha with floating panels,
  not a nice-to-have.
- **Multi-window value**: this is what actually helps a learner compare two
  or more router consoles side by side without being constrained to one
  shared dock's width/height — complements idea #9's tiled-consoles-inside-
  one-dock approach rather than replacing it; the two aren't mutually
  exclusive (floating mode could itself support snapping windows into a
  tiled arrangement later, but that's a follow-on, not part of this).

**Rollout**: ship as a global mode toggle first (all consoles docked, or all
floating) — simplest to build and reason about. Per-console "some docked,
some floating" mixing is a reasonable follow-up once the floating primitive
itself is proven, not needed for a first cut.

---

## Guided checkpoints / workbook (idea #5, design detail)

### Checks run over `netprobe`'s machine channel, never the human CLI

`netprobe`'s console is one shared session (`console_hub` fan-out — the same
mechanism IOL/VPCS already use for concurrent webconsole + native telnet):
every connected viewer reads and writes the *same* stream, character for
character. If checkpoint automation drove its `ping`/`dns`/etc. commands
through that socket, a learner with the console open would see checks fire
live, interleaved with their own typing — confusing, and it corrupts the
"this is what I typed" mental model a console is supposed to preserve.

`netprobe` already has a second, separate channel precedent: the `save`
command's persistence design pulls structured state from the pack over
`GET /_iolbox/state` on the GUI socket, machine-only, no terminal involved.
Checkpoints reuse that shape rather than inventing a third: a small
machine-only HTTP surface on the same GUI socket —
`POST /_iolbox/check {kind, args} → {pass, detail, evidence}` — that runs
the check's underlying action (ping, DNS query, TCP connect, ...) using
`netprobe`'s own internals directly, not by feeding text through the CLI
parser at all. The CLI and the checker both end up calling the same Go
functions underneath; they just don't share a terminal. A learner's console
stays exactly what they typed into it, nothing more.

### More check types than the original four

The original four (ping success, DHCP lease, packet-seen-on-link,
convergence-within-window) were the obvious ones. Others that fit the same
"an action `netprobe`/capture can already observe or perform, wrapped in a
pass/fail rule" shape:

- **DNS resolution** — `netprobe` queries a name via `netsvc` (or any DNS
  server in the lab) and checks it resolves to an expected address (or
  NXDOMAINs, for a negative-test lab teaching filtering/split-DNS).
- **TCP/UDP reachability** — connect to `host:port` and check success/
  refused/timeout — the generic shape behind "is my ACL doing what I think",
  reusable for any service a lab stands up (`netsvc`'s TFTP/NTP, a
  `webserver` pack node, a router's own vty).
- **NTP sync status** — did `netprobe` (or a router, if scrapeable) actually
  sync to `netsvc`'s NTP service, not just "did a packet cross the wire".
- **TFTP round-trip** — push then pull a file through `netsvc`'s TFTP and
  diff it, for backup/restore-config labs.
- **Route/reachability shape** — from `netprobe`, trace to a target and
  assert the path goes through (or explicitly avoids) a named node — checks
  policy routing / traffic engineering outcomes, not just "does it work".
- **Interface admin/oper state** — did a specific interface come up/down as
  expected after a config change or (idea #3) an injected link fault —
  cheap, since idea #3's fault model already tracks this state.
- **Config-line present/absent** — grep the saved NVRAM config (idea #4's
  baseline/diff machinery) for an expected line or pattern — the
  "did they actually type the command" check for labs that are more about
  syntax than end-to-end behavior.
- **Convergence timing, generalized** — not just "link down → reachability
  restored within N seconds" but any before/after pair: trigger an event
  (link fault, `node.restart`, a config push) and assert some other check
  from this list passes again within a window — turns every check above
  into a timing check for free, rather than needing convergence-within-window
  as a separate special case.
- **DHCP lease detail, not just success** — assert the *specific* offered
  address, lease time, or option (e.g. "PC1 must get an address in
  10.0.20.0/24 from VLAN 20's scope" — catches a DHCP-relay/scope
  misconfiguration a bare "got *an* IP" check would miss).
- **Packet count/rate on a link** — not just "was this protocol seen" but
  "was traffic seen within an expected volume range" — catches a flapping
  interface or a routing loop a single boolean seen-check wouldn't.

All of these reduce to the same primitive: something `netprobe` can do or
observe, plus a pass/fail rule and (for the timing family) a trigger + a
window. No new subsystem is needed beyond `netprobe` itself, `netsvc`'s
existing state, capture/dirstat's existing observability, and idea #4's
config-diff machinery — the check catalogue above is really an inventory of
things those four already know how to answer, wrapped in one schema.

### Authoring checks by hand — where and how

Checks need a home in the lab-editor UI that doesn't require hand-editing
the lab JSON, the same way link faults (idea #3) get a canvas affordance
rather than being JSON-only:

- **A "Checkpoints" panel**, sibling to `Inspector.svelte` in the left/right
  UI chrome (same docking convention, not a new top-level surface) — a flat
  list of objectives for the current lab, each with its check type, target
  node/link, expected value, and current pass/fail/not-run status.
- **"Add check" starts from context, not a blank form**: right-click a node
  → "Add checkpoint: ping from here", or right-click a link →
  "Add checkpoint: verify traffic seen" (`ContextMenu.svelte` already has
  this per-node/per-link menu pattern from link-fault and capture
  authoring) — pre-fills the target so authoring a check is a couple of
  clicks, not a form with node-id dropdowns.
- **A type-specific mini-form** per check kind (reusing the check-catalogue
  list above as the dropdown of kinds) — e.g. the DHCP-lease-detail check
  asks for expected subnet/scope, the config-line check asks for a
  text/regex pattern, the reachability-path check asks for a via/avoid node.
- **Manual "run now"** on any single check, plus **"run all"** for the whole
  lab — so an instructor authoring checks can verify their own check is
  correct against a working topology before handing the lab to students,
  without needing a student's mistake to exercise the fail path.
- **Objective text is free-form**, decoupled from the check's mechanics —
  the instructor writes "Configure OSPF between R1 and R2" for a human to
  read, and separately attaches one or more of the checks above as the
  automated proof; a check with no objective text is allowed too (silent
  self-test), and an objective with no check attached is allowed (a manual/
  ungraded step in the workbook) — the two are linked, not fused.
- **Storage**: an array on the lab document alongside `LinkFault`-style
  additions from idea #3 — `contracts/lab.schema.json` gains a `checkpoints`
  array, portable in the same `.yml` lab file, no separate DB/LMS table.
