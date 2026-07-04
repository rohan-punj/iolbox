# Bridge-fabric retirement & refactor map

Status: **ANALYSIS ONLY** — companion to `docs/bridge-fabric-migration.md` (the
design) and `docs/architecture.md` (the current UDP-tunnel design being
replaced). This document is the file+line-anchored inventory the migration
design's "~44 touchpoints" estimate refers to, produced by reading the actual
`internal/relay`, `internal/iouyap`, `internal/netmap`, `internal/extnet`, and
`internal/server` packages as they exist today (repo `iolbox`, no branch info
available — read-only pass, not committed).

All paths below are relative to `J:\Claude code\iolbox\supervisor\` unless
stated otherwise.

---

## 1. The `wiringFor` seam

```go
// internal/server/links.go:66
func wiringFor(link *lab.Link, isIOL map[int]bool, captureReady bool) linkWiring
```

This is **the single function** that decides, per link, whether it is realized
as `wiringNative` (whole-lab NETMAP, no relay/bridge, direct IOL netio) or
`wiringBridged` (iouyap netio↔UDP bridge + UDP relay). Its body
(`links.go:66-87`):

- non-p2p (segment) → bridged
- capture enabled on the link → bridged
- endpoint count != 2 → bridged
- any endpoint not IOL (VPCS/NAT/mgmt) → bridged
- otherwise, if `captureReady` (lab-level flag, default ON) → bridged
- otherwise → native

**Callers of `wiringFor`** (every one of these is a control-plane decision
point that currently asks "is this link topology-dependent-native or
relay-bridged"):

1. `internal/server/bridgeplan.go:216` — `buildBridgePlan`, pass 1 (deciding which links need pseudo-instances/relay ports)
2. `internal/server/bridgeplan.go:250` — `buildBridgePlan`, pass 2 (building the actual bridged-link entries)
3. `internal/server/bridgeplan.go:433` — `rebuildBridgePlan` (deciding which sticky assignments to keep vs release)
4. `internal/server/links.go:121` — `nativeLinkSpecs` (deciding which links get a NETMAP line)
5. `internal/server/handlers.go:736` — `handleLinkAdd` (deciding whether a newly added/upserted link needs a relay start at all, or is "native, needs restart to take effect")
6. `internal/server/handlers.go:953` — `handleCaptureStop` (deciding whether to restart the (un-teed) relay after capture.stop, vs. let the link fall back to native)
7. Test call sites: `links_test.go:35,40,58,89`, `extnet_test.go:120,123` (unit tests of the predicate itself)

**Under the static-tap fabric, this seam collapses to nothing** (or to a
trivial constant): every interface gets its own tap at boot regardless of
topology, so there is no "native vs bridged" distinction left to decide — all
links become bridge-attach operations. `wiringFor`, `linkWiring`
(`links.go:8-40`), `wiringNative`/`wiringBridged` and every caller above are
retirement candidates. The only surviving question post-migration is *link
kind* (p2p 2-port bridge vs. segment N-port bridge), which is a much simpler,
purely-topological read (no IOL-ness, no capture-readiness, no native/bridged
split).

---

## 2. NETMAP generation — topology-DEPENDENT today: **YES**

Proof, `internal/netmap/netmap.go:245-273`:

```go
// Build produces the NETMAP file content from native IOL-to-IOL p2p pairings and
// bridged IOL endpoints. ...
func Build(links []LinkSpec, bridged ...BridgedEndpoint) string {
	var lines []string
	for _, link := range links {
		var iol []Entry
		for _, ep := range link.Endpoints {
			if !ep.IsIOL { continue }
			ifc, err := ParseIface(ep.Interface)
			if err != nil { continue }
			iol = append(iol, Entry{NodeID: ep.NodeID, Iface: ifc})
		}
		if link.P2P && len(iol) == 2 {
			lines = append(lines, iol[0].String()+" "+iol[1].String())
		}
	}
	for _, be := range bridged {
		if line, ok := bridgedLine(be); ok {
			lines = append(lines, line)
		}
	}
	...
}
```

`Build`'s two inputs are both link-plan-derived:
- `links []LinkSpec` comes from `nativeLinkSpecs(doc)` (`links.go:115-133`),
  which iterates `doc.Links` and calls `wiringFor` per link — i.e. **a NETMAP
  line exists for an interface only if a link for it currently exists in the
  doc AND that link resolved native**.
- `bridged ...BridgedEndpoint` comes from
  `bridgePlan.bridgedEndpointsForNetmap()` (`bridgeplan.go:346-360`), which
  flattens `plan.links[i].endpoints` — again, only interfaces that are
  endpoints of a currently-bridged link appear.

An interface with **no link at all** gets **no NETMAP line whatsoever** today.
This is exactly the structural flaw `bridge-fabric-migration.md` §1
describes: IOL reads NETMAP once at boot, so a link drawn after boot updates
the doc/NETMAP-on-disk but the running process never re-reads it.

Call site that writes it: `internal/server/handlers.go:1104-1110`
(`netmapFor`), which combines `nativeLinkSpecs(ll.doc)` +
`ll.bridge.bridgedEndpointsForNetmap()` — confirming the whole-lab NETMAP is
assembled fresh from the **current link set** on every `prepareLabDir` call
(itself called from `startNodes`, `handlers.go:383`, on every node start /
lab start / link add/remove that triggers a plan rebuild).

**What must change for static NETMAP:** `netmap.Build` (or a new
`netmap.BuildStatic`) must be driven from **node adapter/serial counts alone**
— e.g. for each IOL node, emit a line for every possible interface up to
`Ethernet` and `Serial` count (from `node.Spec.Ethernet`/`.Serial`, see
`handlers.go:531-532`), pointing every interface at its own fixed
pseudo-instance/tap **whether or not a link exists for it in the doc**. The
`LinkSpec`/`EndpointSpec`/`BridgedEndpoint` types (`netmap.go:191-221`) and the
link-iteration logic in `Build` become dead weight; a much smaller
per-node-per-interface generator replaces them. `nativeLinkSpecs` (`links.go:115`)
and `bridgedEndpointsForNetmap` (`bridgeplan.go:346`) are both retired as part
of this.

---

## 3. Retirement list (symbols, file:line, why, callers)

### 3.1 `wiringFor` / `linkWiring` and the native/bridged split
- `internal/server/links.go:8-40` — `linkWiring` type + `wiringNative`/`wiringBridged` consts + `String()`. Why: no dichotomy once every interface is a static tap. Callers: all of §1's list.
- `internal/server/links.go:66-87` — `wiringFor` itself.
- `internal/server/links.go:115-133` — `nativeLinkSpecs`. Why: native/bridged NETMAP split disappears.
- `internal/server/bridgeplan.go:346-360` — `bridgedEndpointsForNetmap`. Why: same.

### 3.2 `resyncExtnetPorts`
- `internal/server/handlers.go:777-794` — full function body. Why: exists **only** because a nat/mgmt endpoint's UDP relay ports can be re-plumbed mid-session (ephemeral start-before-link-exists, then a plan rebuild reassigns real ports) and the endpoint must be told to re-bind. With a static tap fabric a nat/mgmt endpoint is a permanent bridge member from creation — there is no port to resync.
- Caller: `internal/server/handlers.go:767` (`handleLinkAdd`, immediately after starting/restarting the link's relay). Only one call site.
- Depends on: `Endpoint.Rebind` (see 3.3) and `bridgePlan.extnetUDPFor` (`bridgeplan.go:395-404`, itself retirement-adjacent since relay UDP mapping goes away).

### 3.3 `Rebind` / `Endpoint.Rebind`
- `internal/extnet/endpoint_linux.go:234-258` — `func (e *Endpoint) Rebind(sendPort, listenPort int) error` and its doc comment (`endpoint_linux.go:225-233`) which explicitly narrates the ephemeral-port bug this exists to patch ("a nat/mgmt node can be started before its link exists... the endpoint must move its socket to match or the two never meet").
- Supporting machinery that exists only for Rebind: `Endpoint.mu`/`cfg`/`udpConn`/`sendTo`/`pumpStop`/`pumpWG` fields (`endpoint_linux.go:36-46`), `startPumps`/`stopPumps` (`endpoint_linux.go:192-223`), `bindRelay` (`endpoint_linux.go:269-290`), `Ports()` accessor (`endpoint_linux.go:260-267`).
- Callers: `resyncExtnetPorts` (`handlers.go:790`) is the only production caller found. Why retired: with a veth/tap permanently on a bridge, there is no relay socket to rebind — the endpoint's kernel-side plumbing is static from `Start`.
- Note: `Start`/`Close`/the tap-open/teardown-cmd logic in the same file are **kept** (NAT/mgmt device lifecycle is still needed) — only the relay-socket-rebind portion of `Endpoint` retires. This file needs careful surgical splitting, not wholesale deletion.

### 3.4 `linkAssign` / `loadedLab.assigns` stickiness
- `internal/server/bridgeplan.go:105-119` — `linkAssign` and `epAssign` types.
- `internal/server/bridgeplan.go:125-135` — `(*linkAssign).compatible`.
- `internal/server/bridgeplan.go:138-143` — `(*linkAssign).release`.
- `internal/server/loaded.go:30-41` — `loadedLab.assigns` field + its extensive comment explaining exactly why stickiness exists ("Without stickiness, rebuilds re-allocated in link-id order... silently desyncing long-running endpoints... Observed as 'R1 stops getting DHCP offers after some topology edits'").
- `internal/server/loaded.go:70` — `assigns: make(map[int]*linkAssign)` init in `newLoadedLab`.
- Why retired: stickiness is a workaround for relay-port churn on rebuild. Static per-interface taps never get reassigned — there's nothing to keep "sticky" because there's no allocation event tied to topology changes at all.
- Callers/usage sites: `buildBridgePlan` (`bridgeplan.go:194-244`, the entire pass-1/pass-2 sticky-vs-fresh logic), `rebuildBridgePlan` (`bridgeplan.go:426-469`, the release-on-no-longer-bridged loop), test `TestStickyAssignmentsAcrossLinkRemoval` (`bridgeplan_test.go:209-262`, see §7).

### 3.5 Ephemeral-port handling
- `internal/extnet/endpoint_linux.go:228-231` (comment) — describes the GUI auto-starting a NAT before its link exists, forcing an ephemeral relay port bind.
- The actual ephemeral bind path is `bindRelay` (`endpoint_linux.go:269-290`) called from `Start` (`endpoint_linux.go:176`) with whatever `cfg.SendPort`/`cfg.ListenPort` were resolved at that moment (`handlers.go:464-472`, `startExtnetNode`: `if send, listen, ok := ll.bridge.extnetUDPFor(n.ID); ok { cfg.SendPort = send; cfg.ListenPort = listen }` — note the `ok` is false, leaving zero values, when no link/plan exists yet, i.e. an ephemeral/unbound state that `Rebind` later corrects).
- Why retired: with a veth/tap always present and bridge-attached, there is no "before its link exists" state to be ephemeral about — a NAT node's kernel-side device exists independent of any bridge attachment, and attaching to `br-<linkid>` is a separate, idempotent runtime step.

### 3.6 Relay port-pair juggling (the UDP relay itself, for IOL/VPCS/NAT links)
- `internal/relay/relay.go` (whole file) — `UDPEndpoint`, `Config`, `Relay` interface, `Manager` (`Start`/`Stop`/`Stats`/`StopAll`). Why: the relay's entire reason to exist is forwarding between dynamically-assigned UDP port pairs; a bridge does this in the kernel with static taps.
- `internal/relay/relay_linux.go` (whole file) — `udpRelay`, `newRelay`, `pump` (the P2P/hub forwarding loop, `relay_linux.go:91-175`).
- Per the migration doc's component table: relay is "retired for IOL/VPCS/NAT links" but **capture's tee mechanism concept survives** in spirit (moves to `tcpdump -i br-<linkid>`, see §5) — so `classify.go`/`pcapng.go` (protocol classification + pcapng framing) likely **partially survive** as building blocks for the new tcpdump-based capture path, but the UDP-forwarding core (`relay_linux.go` pump loop, port binding) does not.
- Callers of `relay.Manager` (i.e. everywhere a relay is Start/Stop'd — all of these lose their reason to exist once bridging replaces UDP forwarding):
  - `internal/server/bridgeplan_linux.go:82-84` — `startLinkRelays` (Stop-then-Start every bridged link's relay at lab start)
  - `internal/server/handlers.go:155,251,280,757-758,805,899,949,954-955,1011` — lab.load old-lab cleanup, lab.stop, lab.reap, handleLinkAdd, handleLinkRemove, handleCaptureStart/Stop, releaseCaptures
  - `internal/server/bridgeplan.go:82` (import), used throughout `bridgeplan.go` to construct `relay.Config`

### 3.7 `startLinkRelays`
- `internal/server/bridgeplan_linux.go:70-88` — full function. Why: starts (or restarts) the UDP relay for every bridged link at lab start "so bridged links carry traffic from lab start." Under bridge fabric, "carrying traffic" is just having the tap attached to `br-<linkid>` — no relay process to start.
- Caller: need to grep `startLinkRelays(` call site — **not found being called from `handlers.go` in the excerpts read**; likely called from `prepareLabDir` (Linux-only lab-prep path not fully read in this pass — **flag as unconfirmed**, recommend a targeted grep before deletion in P5).

### 3.8 Sticky reuse in `buildBridgePlan`
- `internal/server/bridgeplan.go:194-340` — the entire function. This is the biggest single retirement item: pseudo-instance allocation (`AllocPseudoInstances`), relay UDP port allocation (`udp.Next()`), and the sticky-assignment bookkeeping are ALL premised on links being the unit of dynamic wiring. Under static taps, there is no "plan" to (re)build from the link set — taps exist from node creation; only bridge membership changes.
- Retires or radically shrinks: `bridgedLink`/`bridgedEndpoint`/`iouyapConfig` types (`bridgeplan.go:65-103`), `netioDir`/`netioPathFor` (`bridgeplan.go:145-158`, though the netio-dir convention itself likely survives for the tap-mode iouyap), `realInstances` (`bridgeplan.go:162-170`).
- `rebuildBridgePlan` (`bridgeplan.go:426-469`) and `bridgeplan_linux.go`'s `startBridges` (`bridgeplan_linux.go:23-68`) / `stopBridges` (`bridgeplan_linux.go:92-100`) are all plan-rebuild machinery that a "create every tap once at boot, then just (de)attach from a bridge" model replaces with something much simpler (no sticky assigns, no per-rebuild port churn).

### Summary count

| # | Item | Files touched |
|---|---|---|
| 1 | `wiringFor` + `linkWiring` type/consts | links.go |
| 2 | `nativeLinkSpecs` | links.go |
| 3 | `bridgedEndpointsForNetmap` | bridgeplan.go |
| 4 | `resyncExtnetPorts` | handlers.go |
| 5 | `Endpoint.Rebind` + supporting pump/bind machinery | endpoint_linux.go |
| 6 | `linkAssign`/`epAssign` types + `compatible`/`release` | bridgeplan.go |
| 7 | `loadedLab.assigns` field + init | loaded.go |
| 8 | Ephemeral-port bind path in `startExtnetNode`/`bindRelay` | handlers.go, endpoint_linux.go |
| 9 | `relay.Manager`/`Config`/`UDPEndpoint`/`Relay` iface | relay.go |
| 10 | `udpRelay`/`newRelay`/`pump` | relay_linux.go |
| 11 | `startLinkRelays` | bridgeplan_linux.go |
| 12 | `buildBridgePlan` (pseudo-instance + port allocation core) | bridgeplan.go |
| 13 | `bridgedLink`/`bridgedEndpoint`/`iouyapConfig` types | bridgeplan.go |
| 14 | `rebuildBridgePlan`, `startBridges`, `stopBridges` (in current relay-pairing form) | bridgeplan.go, bridgeplan_linux.go |
| 15 | All `relay.Manager` call sites in handlers.go (10+ distinct lines) | handlers.go |
| 16 | `vpcsUDPFor`/`extnetUDPFor` (relay-port-based lookups) | bridgeplan.go |

Counting individual named symbols + their distinct call sites (not just the
16 grouped rows above) I count **~38-42 concrete touchpoints** across
links.go, bridgeplan.go, bridgeplan_linux.go, handlers.go, loaded.go,
relay.go, relay_linux.go, and endpoint_linux.go — **consistent with, and a
reasonable sanity-check for, the design doc's "~44 touchpoints" estimate**.
See §7 for exact per-touchpoint test coverage.

---

## 4. NAT (extnet) rework surface

Today's NAT node (`internal/extnet/endpoint_linux.go` + `commands.go` +
`dhcp.go`/`dhcp_linux.go`):

- **Device**: `ip tuntap add dev <iolnatN> mode tap user <owner>` (`commands.go:37`), addressed as gateway `ip addr add 172.31.<n>.1/24 dev <iface>` (`commands.go:38`), brought up (`commands.go:39`).
- **NAT**: `sysctl -w net.ipv4.ip_forward=1` (`commands.go:40`) + MASQUERADE (`commands.go:62-65`) + two FORWARD accept rules (`commands.go:67-79`), all as exact `-A`/`-D` pairs (`delRule`, `commands.go:83-93`) so teardown is precise.
- **DHCP**: a from-scratch userspace BOOTP/DHCP server (`dhcp.go`, `dhcp_linux.go`) — DISCOVER→OFFER, REQUEST→ACK only, fixed 1h lease, round-robin pool `172.31.<n>.100-199` (`extnet.go:107-110`). It intercepts DHCP frames in `pumpRelayToTap` (`endpoint_linux.go:367-377`) **before** they reach the kernel tap — "DHCP handled in userspace; never touches the kernel."
- **Data path to the lab node**: `pumpTapToRelay`/`pumpRelayToTap` (`endpoint_linux.go:297-384`) — the tap's frames go out over the UDP relay socket (`e.udpConn`/`e.sendTo`), the exact mechanism `Rebind` re-points.

**What a "veth-on-bridge + dnsmasq + MASQUERADE" replacement touches:**
- `natSetupCmds`/`natTeardownCmds` (`commands.go:35-58`) — `ip tuntap add ... mode tap` becomes `ip link add veth... ` (or keep tap, just attach it: `ip link set <iface> master br-<linkid>`) — MASQUERADE/FORWARD rules likely unchanged in shape (still keyed on `sub.Network()`/`defaultIface`).
- `Endpoint.dhcp` (`endpoint_linux.go:31`) and the whole `dhcp.go`/`dhcp_linux.go` userspace server — migration doc §5.2 flags this as an open decision: replace with real `dnsmasq` (new process to supervise) or keep the userspace server but bind it to the bridge-attached tap directly (no relay indirection). Either way, `pumpRelayToTap`'s DHCP-intercept-before-tap logic (`endpoint_linux.go:367-377`) goes away if dnsmasq owns the tap directly; it's simplified (still intercepts, but no relay-socket layer) if the userspace server is kept.
- `pumpTapToRelay`/`pumpRelayToTap` (`endpoint_linux.go:297-384`) and the `udpConn`/`sendTo`/pump-generation fields entirely retire if the tap is bridge-attached directly (kernel forwards, no userspace pump needed for non-DHCP traffic) — this is the single biggest simplification in the whole migration for the NAT node. Only DHCP interception (if kept userspace) would still need *some* tap-facing code, but no relay/UDP side at all.
- `extnet.Config.SendPort`/`.ListenPort`/`.Host` (`extnet.go:127-134`) and `bridgePlan.extnetUDPFor` (`bridgeplan.go:395-404`) retire — nothing to wire to a relay port anymore.
- `SubnetAllocator`/`Subnet` (`extnet.go:40-110`) — **survives unchanged**; subnet addressing is orthogonal to the data-plane transport.
- `Capabilities`/`Detect`/gating (`extnet.go:155-189`, `detect_linux.go`) — **survives**; NAT/mgmt support detection is still meaningful (NET_ADMIN/tuntap availability), migration doc §5.5 flags this as needing **revalidation** on unprivileged LXC/Docker since bridges+taps widen the privileged surface.

---

## 5. Capture rework surface

Today: `wiringFor` forces a plain IOL↔IOL p2p link to `wiringBridged` when
`link.Capture.Enabled` or lab-level `captureReady` is on (`links.go:70-71,83-84`).
The relay's `Config.CapturePort` (`relay.go:36`) triggers `newCaptureServer`
(`capture_linux.go:31-46`) inside `newRelay` (`relay_linux.go:75-82`); every
datagram the relay's `pump` receives is teed via `r.tee.Broadcast(datagram)`
(`relay_linux.go:114-115`) to every connected TCP client as pcapng
(`captureServer.Broadcast`, `capture_linux.go:73-83`, using `PcapngWriter`
from `pcapng.go`, not read in this pass but referenced throughout
`capture_linux.go`).

**Handlers/ports involved:**
- `handleCaptureStart` (`handlers.go:846-924`) — allocates a capture TCP port (`s.capturePorts.Next()`), records intent in `ll.captures`, rebuilds the bridge plan so the link (re)carries a tee, restarts its relay with `CapturePort` set.
- `handleCaptureStop` (`handlers.go:926-960`) — releases the port, rebuilds without the tee; if capture-ready keeps the link bridged, a fresh (tee-less) relay restarts; else the link "reverts to native" pending node restart.
- `armDocCaptures` (`handlers.go:969-993`) — auto-arms every doc-declared `capture.enabled` link's port before the plan is built, on every `startNodes`.
- GUI side: `app/src/lib/captureTransport.ts` — `CaptureTransport` class dials `ws(s)://<host>/capture/{linkId}` (`captureTransport.ts:29-31`), which the supervisor's `internal/wsbridge/wsbridge.go` upgrades: `linkIDFromPath` (`wsbridge.go:291-295`) extracts the id, `b.ctrl.CapturePort(linkID)` (`wsbridge.go:314`) looks up the currently-armed capture port and 404s if none (`wsbridge.go:315-318`) — this is the `captureTransport` GUI path the task asked to locate.

**What moving to `tcpdump -i br-<linkid>` → pcapng TCP touches:**
- The tee mechanism moves from "relay broadcasts every forwarded datagram to connected TCP clients" to "spawn/manage a `tcpdump -i br-<linkid> -w -` process per active capture, piping its stdout into the same pcapng-over-TCP model." This likely **reuses** `captureServer`/`PcapngWriter` (`capture_linux.go`, `pcapng.go`) as the client-facing TCP server, but the **frame source** changes from relay-pump-tee to tcpdump-stdout-parse (or raw pcap passthrough if tcpdump already emits pcapng, avoiding re-framing).
- `wiringFor`'s `link.Capture.Enabled` bridged-forcing branch (`links.go:70-71`) disappears — since EVERY link is a bridge in the new model, EVERY link (including native IOL↔IOL) becomes capturable without ever restarting a node — this is called out as a **net win** in the migration doc §5.7 ("every link becomes capturable, including native IOL↔IOL which today can't be").
- `relay.Config.CapturePort`/`CaptureBind` (`relay.go:35-42`) and the tee-wiring inside `newRelay` (`relay_linux.go:75-82`) retire along with the rest of the relay.
- `handleCaptureStart`/`handleCaptureStop`/`armDocCaptures` are rewritten to manage a tcpdump-process lifecycle keyed by `br-<linkid>` instead of a relay-tee lifecycle keyed by link id — the port-allocation/announce (`s.emit(protocol.EventCaptureStarted, ...)`) and GUI-facing `/capture/{linkId}` contract likely **stay the same shape** (same event, same WS endpoint) even though the backing implementation changes completely.
- `Classify`/`classify.go` (protocol classification for per-proto stats, referenced in `relay_linux.go:124` and `relay.go:57-65`) — the link.stats feature (per-protocol fps breakdown) currently derives from the relay's pump loop counting forwarded frames; this needs a new counting point once the relay pump is gone (likely: count frames as tcpdump/bridge stats, or a lightweight sniffer on `br-<linkid>` in parallel with the capture tee). **Flagged as open — not fully resolved by reading the design doc**, which does not mention `link.stats`/`ProtoStats` at all.

---

## 6. VPCS path

Today: VPCS speaks UDP natively (GNS3 vpcs `-s`/`-c` flags). In the bridge
plan, a VPCS endpoint (`bridgedEndpoint.vpcs = true`, `bridgeplan.go:328-330`)
gets relay UDP ports like any bridged endpoint but **no** iouyap bridge/pseudo
instance (VPCS never uses netio). `bridgePlan.vpcsUDPFor` (`bridgeplan.go:378-387`)
maps a VPCS node id to `(sendPort, listenPort)` from its relay endpoint;
`buildSpec` (`handlers.go:538-547`) wires these into `node.Spec.VPCSUDPLocal`/
`.VPCSUDPRemote` (VPCS binds the relay's delivery port `-s`, sends to the
relay's receiving port `-c`).

**Where a udp↔tap shim slots in:** VPCS still speaks UDP (unchanged — the
migration doc §5.1 confirms "VPCS stays UDP; shim bridges to a tap" and flags
this as a real decision point, not a free win). The shim replaces the relay as
VPCS's peer: instead of binding to `relayEP.LocalPort`/`.RemotePort` pair
managed by `buildBridgePlan`, VPCS's UDP socket pair would bind to a small
new per-VPCS-interface shim process/goroutine that reads VPCS's UDP frames and
writes them into a dedicated tap (`tapZ` in the migration doc's diagram),
which is then bridge-attached like any other interface. This shim is **new
code**, not a repurposing of `relay_linux.go`'s pump (though the read-transform-
write shape is similar enough that `relay_linux.go`'s pump loop or
`iouyap`'s `runPump`/`pumpOne` (`iouyap.go:127-162`) could plausibly be adapted
as the shim's implementation basis — worth flagging as a reuse opportunity for
P3, not guessing further here).
`vpcsUDPFor` (`bridgeplan.go:378-387`) and the VPCS-specific fields in
`bridgedEndpoint` (`vpcs`, `pcIndex`, `bridgeplan.go:79-80`) retire in their
current form (relay-port-pair-based) and are replaced by whatever addressing
the new shim needs (likely just "which tap does this VPCS's shim own",
topology-independent like everything else).

---

## 7. Test surface

Tests that assert retirement-targeted behavior and will need rewriting or
deletion:

- **`internal/server/bridgeplan_test.go`**
  - `TestBridgePlanCapturedIOLtoIOL` (`:20`) — pins pseudo-instance assignment + iouyap<->relay UDP pairing for a captured IOL↔IOL link. Fully retargeted (no pseudo-instances/relay ports under static taps).
  - `TestBridgePlanVPCStoIOL` (`:94`) — pins VPCS relay-port wiring. Needs rewrite once VPCS goes through a udp↔tap shim instead of the relay.
  - `TestNetmapIncludesBridgedLines` (`:145`) — pins the bridged-NETMAP-line format (`<real>:<a>/<p> <pseudo>:0/0`). Needs a NEW static-NETMAP equivalent (`netmap.Build` becomes adapter-count-driven).
  - `TestBridgePlanReleasesPortsOnRebuild` (`:177`) — pins port release on rebuild. Retires with the whole port-allocation model.
  - `TestStickyAssignmentsAcrossLinkRemoval` (`:209-262`, the test explicitly named in the task) — pins the sticky-assignment fix itself. **Deleted outright** once `linkAssign`/`assigns` retire — there's no more "assignment" to keep sticky.

- **`internal/server/links_test.go`**
  - `TestWiringForNativeIOLtoIOL` (`:26`) — asserts `wiringFor`'s native/bridged split by capture-ready flag. Retires with `wiringFor`.
  - `TestCaptureReadyDefaultBridgesInterIOL` (`:48`) — same; also pins `nativeLinkSpecs` emitting zero specs. Retires.
  - `TestWiringForBridgedCases` (`:67`) — pins vpcs/segment/capture-enabled always bridging. Retires (or is repurposed into a much simpler "what kind of bridge does this link need" test).
  - `TestNetmapForOnlyNativeLinks` (`:97`) — pins today's mixed native+bridged NETMAP output. Replaced by a static-NETMAP test asserting adapter-count-driven lines regardless of link presence.
  - `TestLabDirIsShared` (`:124`), `TestNVRAMInjectionRoundTrip` (`:139`), `TestInstanceIDConsistentAcrossArgvNetmapNvram` (`:161`) — **NOT retirement targets**; these test NVRAM/instance-id/labdir concerns orthogonal to the wiring model and should survive unchanged (though `TestInstanceIDConsistentAcrossArgvNetmapNvram` calls `netmap.Build` with the OLD `LinkSpec` shape at `:177-180`, so it will need a mechanical signature update even though its *intent* survives).

- **`internal/server/extnet_test.go`**
  - `TestBridgePlanExtnetUDP` (`:73`) — pins NAT-endpoint relay-port mapping (`extnetUDPFor`). Retires with the relay-port model.
  - `TestExtnetLinkIsBridged` (`:110`) — pins nat/mgmt links always bridging via `wiringFor`. Retires with `wiringFor`; likely replaced by "nat/mgmt nodes are always bridge members" being trivially true (no test needed) or a bridge-membership-shape test.
  - `TestHelloAdvertisesGatedFeatures` (`:17`), `TestStartNatUnsupported` (`:52`) — **NOT retirement targets**, orthogonal to wiring (capability gating, unsupported-kind rejection).

- **Not found in this pass, flagged for the implementer to locate before P5**: any `_test.go` covering `Endpoint.Rebind` directly (searched `extnet_test.go`, `commands_test.go`, `dhcp_test.go` were not opened in full — **recommend grepping `Rebind` across `internal/extnet/*_test.go` before deleting the method**, since a dedicated rebind test would need outright deletion rather than adaptation).
- Similarly not fully confirmed: `internal/relay/*_test.go` test files (not enumerated in this pass) almost certainly test `udpRelay`'s pump/tee logic directly and will need wholesale deletion or replacement with bridge-manager/tcpdump-capture tests. **Flag: grep `internal/relay/*_test.go` before P5.**

---

## Phase mapping

| Retirement item | Phase |
|---|---|
| `wiringFor` / `linkWiring` / `nativeLinkSpecs` | P1 (static netmap removes the native/bridged split) |
| NETMAP topology-dependence (`netmap.Build` link-driven form) | P1 |
| iouyap netio↔UDP → netio↔tap (`bridge_linux.go` pump direction) | P1 |
| `bridgedEndpointsForNetmap`, `bridgedLink`/`bridgedEndpoint`/`iouyapConfig` types | P1 (replaced by static per-interface tap assignment) |
| Bridge manager (new: create/destroy `br-<linkid>`, attach/detach taps) | P1 |
| NAT device setup (`natSetupCmds`/teardown) → veth/tap-on-bridge | P2 |
| Userspace DHCP → dnsmasq or bridge-bound userspace DHCP (open decision) | P2 |
| `Endpoint.Rebind` + pump/bind machinery in `endpoint_linux.go` | P2 (NAT no longer needs relay-port rebinding once bridge-attached) |
| Ephemeral-port handling in `startExtnetNode`/`bindRelay` | P2 |
| `resyncExtnetPorts` | P2 (its only caller, `handleLinkAdd`'s relay restart, goes away with the relay) |
| VPCS udp↔tap shim (new code) | P3 |
| `vpcsUDPFor`, VPCS-specific `bridgedEndpoint` fields | P3 |
| Capture: relay-tee → `tcpdump -i br-<linkid>`; `captureTransport`/`/capture/{linkId}` handler rework | P4 |
| `link.stats`/`ProtoStats`/`Classify` re-homing (open question, not addressed by the design doc) | P4 (must be resolved before P5 can delete the relay pump that currently computes these) |
| `relay.Manager`/`Config`/`Relay` iface, `udpRelay`/`newRelay`/pump loop | P5 |
| `linkAssign`/`epAssign`/`loadedLab.assigns` stickiness | P5 |
| `buildBridgePlan`'s pseudo-instance/port-allocation core, `rebuildBridgePlan`, `startBridges`/`stopBridges` (current form), `startLinkRelays` | P5 |
| Test deletions/rewrites (`TestStickyAssignmentsAcrossLinkRemoval`, `bridgeplan_test.go`, `links_test.go`, `extnet_test.go` cases listed in §7) | P5 (delete alongside the code they pin) |

---

## Open questions / unconfirmed (flagged rather than guessed)

1. `startLinkRelays` (`bridgeplan_linux.go:70`) — caller not located in the files read this pass (likely `prepareLabDir`, a Linux-only file not in the requested reading list). Confirm before deleting.
2. `link.stats`/`ProtoStats`/`Classify` per-protocol counting has no described replacement in `bridge-fabric-migration.md` — the design doc doesn't mention it at all. This is a real gap the phase-4/5 implementer needs to resolve, not something this analysis can resolve by reading code alone.
3. `internal/relay/*_test.go` and `Rebind`-specific tests in `internal/extnet` were not enumerated/read in full this pass — recommend a targeted grep sweep immediately before P5 test cleanup.
4. Exact reuse-vs-rewrite of `capture_linux.go`/`pcapng.go` for the tcpdump-based capture path is a judgment call for the P4 implementer, not fully determinable from this read-only pass.
