# internal/iouyap

Bridges one IOL **netio unix-domain datagram socket** to one **UDP** endpoint,
mirroring the classic GNS3/IOU `iouyap` helper. This package owns nothing
outside itself; the integrator wires it into `internal/node` (NETMAP
generation) and `internal/relay` (the UDP peer) at the link-setup call sites.

## Why this exists

Per `docs/p0-spike.md` ("Architecture corrections" #2): real IOL does **not**
speak UDP. Every IOL interface listed in its NETMAP connects to a
**unix-domain datagram socket**. For same-host IOL-to-IOL links, both IOL
processes point at *each other's* netio socket directly — no relay, no
bridge, no `iouyap` needed (this is what P0 step 5's `NETMAP = "1:0/0 2:0/0"`
test confirmed carries traffic).

Three cases can't use that direct path and need a netio↔UDP bridge instead:

1. **Capture** — `internal/relay`'s pcapng tee only sees UDP traffic; a link
   you want to Wireshark must be routed through UDP so the relay can tee it.
2. **VPCS↔IOL** — VPCS speaks the UDP tunnel protocol natively (see
   `internal/relay`), never unix-domain netio. A link with one IOL endpoint
   and one VPCS endpoint needs *something* on the IOL side speaking netio and
   the VPCS side speaking UDP — that's this bridge.
3. **Cross-host** — a unix-domain socket can't cross a network boundary. A
   link spanning two runtimes needs UDP on the wire either way.

In all three cases, the link's NETMAP entry (or, for VPCS, the node's tunnel
config) points at an `iouyap`-owned netio socket instead of the peer IOL
directly. This package owns that socket, relays datagrams netio↔UDP, and the
UDP side talks to `internal/relay`'s bound endpoint (which tees to pcapng
and/or forwards to the far side, exactly as it does for VPCS-only segments
today).

```
IOL  --netio(unix)-->  [iouyap.Bridge]  --UDP-->  internal/relay  --UDP-->  peer (VPCS, cross-host relay, or another bridge)
IOL  <--netio(unix)--  [iouyap.Bridge]  <--UDP--  internal/relay  <--UDP--
```

## API

```go
package iouyap

type Config struct {
    NetioPath   string // unix-domain datagram socket path IOL connects to
    UDPLocal    int    // local UDP port this bridge binds (relay writes here)
    UDPRemote   int    // relay's UDP port this bridge forwards to
    Host        string // default "127.0.0.1" (iouyap.DefaultHost)
    StripHeader bool   // see "netio header" below
}

func New(cfg Config) (*Bridge, error)       // binds both sockets (linux only; see below)
func (b *Bridge) Run(ctx context.Context) error // pumps both directions until ctx is done
func (b *Bridge) Close() error                  // closes both sockets, removes the socket file
```

`New`/`Run`/`Close` have real socket implementations in `bridge_linux.go`
(`//go:build linux`) and a stub in `bridge_other.go` (`//go:build !linux`)
that validates `Config` and returns `ErrUnsupportedPlatform` — matching the
`internal/relay` pattern (`relay_linux.go` / `relay_other.go`) so the package
builds and its pure logic unit-tests on Windows dev boxes.

The framing/parsing (`header.go`) and the pump/transform logic
(`iouyap.go`'s `transform`/`pumpOne`/`runPump`) are platform-independent and
fully unit-tested with in-memory fakes — no real sockets needed to exercise
them.

### Unix-socket peer discovery

`unixgram` sockets have no connect/accept handshake. `Bridge` learns IOL's
socket address from the `Name` on the first datagram it reads via
`ReadFromUnix`, caches it, and uses it as the destination for UDP→netio
traffic. Until IOL has sent at least one datagram, inbound UDP frames are
dropped (nothing to deliver to yet) — this matches original `iouyap`
behaviour.

## Netio header — ASSUMPTIONS TO VERIFY IN P0

`HeaderSize = 8`, matching `relay.IOLHeaderSize` exactly (both packages frame
the *same* datagram on opposite sides of the netio↔UDP hop; they are kept
manually in sync rather than sharing an import, to avoid coupling sibling
`internal` packages). Layout, big-endian, per the classic iouyap/dynamips
`netio_filter` framing:

| Offset | Field    | Size | Meaning                                            |
|-------:|----------|-----:|-----------------------------------------------------|
| 0      | dst_ids  | 2    | destination "bridge"/netio channel id (peer's id)   |
| 2      | src_ids  | 2    | source "bridge"/netio channel id (this side's id)   |
| 4      | dst_port | 1    | destination port index within the peer's node       |
| 5      | src_port | 1    | source port index within this node                  |
| 6      | msg_type | 1    | `0` = data frame (`iouyap.MsgTypeData`); other values reserved |
| 7      | unused   | 1    | padding/channel byte, historically unused by iouyap |

**This has NOT been confirmed against a real IOL process's unix-socket
traffic.** P0 (`docs/p0-spike.md`) confirmed same-host NETMAP wiring carries
traffic over native netio sockets directly between two IOL instances, and
confirmed the UDP relay's pcapng tee works — but neither of those exercises
*this* bridge or captures the raw bytes IOL writes to a unix netio socket.

To verify: run IOL with a NETMAP entry pointing at a unix socket this package
(or a raw `socat`/`nc -u -U` capture) owns, and inspect the first few bytes of
what IOL sends. Adjust `HeaderSize` / the `off*` byte-offset constants /
`Header` fields in `header.go` (and mirror the change in
`relay.IOLHeaderSize`) if real IOL differs. `transform` in `iouyap.go` is the
single seam where any required rewriting (e.g. swapping src/dst ids between
directions, if real IOL turns out to need that) should be added — both pump
directions call through it, so a fix lands once.

`Config.StripHeader` documents the currently-assumed default (`true`): the
header is present end-to-end and is validated/passed through unmodified in
both directions, it is not dropped. Set it to `false` only if P0 finds IOL's
netio framing carries **no** header at all (raw ethernet frames on the unix
socket) — in that mode the bridge does zero header interpretation and passes
datagrams through byte-for-byte.

## How the supervisor is expected to wire a link through this bridge

This package is intentionally not wired into `internal/server` or
`internal/node` — that's the integrator's job, once the NETMAP/relay rework
lands. The expected shape:

1. **Decide a link needs the bridge**, i.e. it is *not* a same-host IOL↔IOL
   p2p/segment pair — capture requested, one endpoint is VPCS, or the peer is
   on another host.
2. **Allocate a `NetioPath`** for the IOL-side socket (e.g.
   `/opt/iolab/run/link-<id>-<nodeid>-<iface>.sock`) and put that path — not
   the peer node id/port — into the IOL endpoint's NETMAP entry, per
   `internal/netmap`'s `Build`. (`netmap.Build` today only emits IOL↔IOL
   `nodeid:port` lines for direct p2p; a bridged endpoint instead needs IOL's
   NETMAP to reference a unix socket path, which is a NETMAP syntax variant
   `internal/netmap` will need to support as part of this rework — out of
   scope for this package.)
3. **Start `internal/relay`'s `Manager` for the link** as already happens for
   VPCS/segment links, getting back the relay's bound `LocalPort` /
   `RemotePort` (and `CapturePort` if capture was requested).
4. **Construct an `iouyap.Config`**:
   - `NetioPath` = the path from step 2.
   - `UDPLocal`  = a fresh port for the bridge to bind (from
     `node.PortAllocator`, same allocator used for other per-link ports).
   - `UDPRemote` = the relay endpoint's `LocalPort` from step 3 (the bridge
     sends *to* the relay's receiving port).
   - `Host` = `"127.0.0.1"` for same-host; the peer runtime's reachable
     address for the cross-host case.
   - `StripHeader` = `true` until P0 says otherwise.
5. **`iouyap.New(cfg)` then `go bridge.Run(ctx)`**, tracked alongside the
   link's other lifecycle state so `bridge.Close()` runs when the link/lab is
   torn down (same lifecycle shape as `relay.Manager.Stop`/`StopAll`).
6. On the relay side, register the *bridge's* `UDPLocal` as that endpoint's
   `RemotePort` in the `relay.Config`/`UDPEndpoint` for this link, so the
   relay's forward direction lands on the bridge, which then delivers into
   IOL's netio socket.

Net effect: IOL only ever speaks netio to a local unix socket, unaware a
bridge and a UDP relay sit behind it; VPCS and the pcapng tee only ever see
UDP, unaware IOL is on the other end.
