# iolab supervisor

The **supervisor** is the control + data plane for iolab. It is a single static
Go binary that runs **inside the Linux runtime** (WSL2, a VMware helper VM, a
remote box, or QEMU) and drives Cisco IOL + VPCS nodes: it spawns the processes,
bridges each console over a pty, wires same-host IOL nodes together natively via
NETMAP (and other links over UDP + the iouyap bridge), tees links to Wireshark,
generates the IOU license, and injects/extracts NVRAM startup-configs.

The design below reflects the **P0 findings against real IOL 17.18.02**
(`docs/p0-spike.md`): the console is a pty (not a TCP port IOL opens), same-host
IOL↔IOL links carry traffic over native unix-socket netio via a shared-directory
NETMAP (no relay), and nodes boot pre-configured from injected NVRAM so IOS-XE
PnP never engages.

The Windows GUI never talks to IOL directly — it speaks the NDJSON control
protocol (`docs/protocol.md`) to this supervisor over a loopback TCP socket.

## Layout

```
supervisor/
  cmd/supervisor/      entrypoint: flags, control server, ws bridge, graceful shutdown
  internal/protocol/   NDJSON request/response/event framing + verb dispatcher
  internal/lab/        lab.schema.json Go structs + Validate()
  internal/netmap/     IOL interface addressing (e0/0 -> a/p token) + whole-lab NETMAP file
  internal/relay/      UDP data plane: p2p forward, segment hub, pcapng capture tee (bridged links)
  internal/iouyap/     netio<->UDP bridge for bridged links (wired by server on linux; see bridgeplan.go)
  internal/node/       process mgmt: state machine, port alloc, IOL/VPCS argv, pty console spawn
  internal/iourc/      IOU license (iourc) generation from hostid+hostname
  internal/nvram/      IOL NVRAM startup-config encode/decode (GNS3 iou codec)
  internal/image/      image fingerprint (sha256), ELF arch parse, L2/L3 class sniff
  internal/server/     ties it together: control server + verb handlers + lab state
  internal/ws/         hand-rolled RFC 6455 WebSocket server (handshake + framing)
  internal/telnet/     telnet IAC negotiation state machine + NAWS encoding
  internal/wsbridge/   WebSocket listener: /control (NDJSON) + /console/{nodeId}
```

## Build

The runtime target is **linux/amd64** (IOL is a Linux ELF binary):

```sh
GOOS=linux GOARCH=amd64 go build -o supervisor ./cmd/supervisor
```

It also builds, vets, and unit-tests cleanly on the Windows/macOS dev box. All
Linux-only syscalls (process exec of the IOL ELF, UDP sockets, NVRAM file I/O)
are isolated behind `//go:build linux`, with platform stubs so the pure logic —
protocol parsing, NETMAP generation, lab validation, the pcapng writer, and the
iourc / NVRAM codecs — is testable everywhere:

```sh
go vet ./...        # clean on host and (GOOS=linux) target
go build ./...      # host
GOOS=linux GOARCH=amd64 go build ./...
go test ./...       # runs the pure-logic tests on any OS
```

## Run

```sh
./supervisor -control-addr 127.0.0.1:4000 -ws-addr 127.0.0.1:4001 -image-dir /opt/iolab/images -run-dir /run/iolab
```

The control server binds **loopback only** (it refuses any non-loopback host).
Console ports are allocated from 9000+, capture ports from 5500+, and internal
UDP tunnel ports from 10000+ (all reported back in responses/`status`).

Pass `-ws-addr ""` to disable the WebSocket bridge; it is **enabled by
default** on `127.0.0.1:4001`, started under the same context and graceful
shutdown as the TCP control listener.

`-console-bind` (default `127.0.0.1`) is the host the per-node IOL console
telnet listeners bind; `0.0.0.0` lets a native telnet client on the GUI host
dial `<vm-ip>:<consolePort>` directly (the console hub serves it alongside the
webconsole). `-capture-bind` (default `127.0.0.1`) is the same knob for each
link's pcapng capture tee: with `0.0.0.0`, a native Wireshark on the GUI host
attaches with `wireshark -k -i TCP@<vm-ip>:<capturePort>`. Both share the
`-ws-addr` trust boundary — the VM's own network exposure.

`-iourc` (default `/opt/iolab/iourc`) points at the runtime's IOU license; the
supervisor copies it into each lab's shared dir at start (see below).

## Node runtime model (P0-confirmed)

### Console = pty → telnet bridge (`internal/node/spawn_linux.go`)

Real IOL uses **stdin/stdout on a controlling pty** for its console and opens
**no** TCP port of its own (confirmed in P0). So on start the supervisor:

1. Binds a loopback telnet listener on the node's allocated `ConsolePort`
   **before** spawning (so a client dialing right after `node.console` never
   races the accept loop).
2. Allocates a pty with `github.com/creack/pty` and runs the node attached to
   the pty **slave** as its controlling terminal — `pty.Start` sets
   `SysProcAttr{Setsid:true, Setctty:true}`, i.e. exactly the
   `setsid`+`ctty` combination the manual P0 `socat …,pty,setsid,ctty` test
   proved works.
3. Keeps the pty **master** for the node's whole lifetime, owned by a
   per-node **console hub** (`internal/node/console_hub.go`) that multiplexes
   it across **all connected telnet clients concurrently** — the webconsole
   and any number of native telnet sessions at once. One hub goroutine owns
   `ptmx.Read` and broadcasts each chunk to every client (clients never read
   the pty themselves, so they can't steal bytes from each other);
   client→pty writes are serialized. Because the master persists
   independently of any console connection, **clients can attach, detach and
   reconnect freely without disturbing IOL** — no console client is required
   for the node to run.

Hub specifics:

- **Replay ring**: the hub keeps the most recent 8 KiB of pty output and
  replays it to every newly attached client, so a fresh session shows the
  current prompt/context instead of a blank screen.
- **Backpressure policy — drop the client**: each client has a bounded output
  queue (64 chunks of ≤4 KiB) pumped by its own writer goroutine; a client
  whose queue is full when a broadcast arrives is disconnected. A console
  stream is low-bandwidth — a client that far behind is dead or unrecoverably
  slow, and dropping it beats stalling the pty reader or buffering unboundedly.
- **Strictly non-blocking attach**: the telnet preamble and replay are queued
  through the client's writer, so a slow client can never stall the accept
  loop (the previous one-client-at-a-time bridge serviced clients
  sequentially — a webconsole holding the port left a native telnet connect
  stuck in the kernel backlog, "opens but does not work").

Telnet handling is minimal and pass-through: on attach the hub volunteers
`IAC WILL ECHO` + `IAC WILL SGA` (so a line-mode client goes char-at-a-time and
lets IOL own echo), and inbound IAC from each client is consumed/answered by a
per-client `internal/telnet.Negotiator` so it never leaks into the pty
(negotiation replies are routed through the client's write queue so the writer
goroutine remains the connection's single writer). pty→client is raw (IOL
emits no IAC over a pty). There is **no** `IOL_CONSOLE_PORT` env var and no
console argv flag — both were scaffold guesses P0 removed. `Environ()` still sets
`IOURC`.

The single dependency `github.com/creack/pty` (v1.1.24) is the standard, tiny,
maintained pty library; it is the supervisor's only third-party dep besides the
hand-rolled `internal/ws`. All pty/console code is behind `//go:build linux`
with a stub on other OSes.

### Shared lab dir + whole-lab NETMAP (`internal/server`, `internal/netmap`)

Every IOL instance of a lab runs with its **cwd set to one shared lab dir**,
`<run-dir>/<labId>/`, which holds:

- a **single `NETMAP`** describing every native same-host IOL↔IOL link,
- the **shared `iourc`** license (copied from `-iourc`), and
- per-node **`nvram_<id>`** files (5-digit, `nvram_%05d`).

IOL's unix-socket netio endpoints live in this cwd, so co-located instances find
each other. `prepareLabDir` writes all three artifacts before any node spawns
(it is idempotent and re-runs on every `lab.start`/`node.start`).

The NETMAP line format is IOL's **`<nodeid>:<adapter>/<port>`** token, e.g.
`1:0/0 2:0/0` — the exact form the P0 test used to bring two real IOL
`Ethernet0/0` line protocols up. (`netmap.Iface.Index()`, the flat
`adapter*16+port`, is **not** what the NETMAP file uses — and note the netio
header's port byte is the reverse nibble order, `iouyap.EncodePortByte`.)

### Native vs. bridged links (`internal/server/links.go`)

`wiringFor(link, isIOL)` is the single decision point:

- **Native** (`wiringNative`) — a point-to-point link whose **both** endpoints
  are IOL nodes, with **no capture** requested. Realized purely through the
  whole-lab NETMAP (netio); no UDP relay, no supervisor in the data path. This
  is the default fast path P0 validated.
- **Bridged** (`wiringBridged`) — a link that needs **capture**, involves a
  **VPCS** (or any non-IOL) endpoint, is a **segment**, or (future) spans
  **hosts**. These route through the UDP relay + pcapng tee, fronted by an
  `iouyap` netio↔UDP bridge. `nativeLinkSpecs` deliberately excludes bridged
  links from the NETMAP so a port is never double-wired.

### Bridged links: iouyap netio↔UDP bridge + pseudo-instances

A **bridged** link cannot use native netio, so its IOL endpoint(s) route through
the `internal/iouyap` netio↔UDP bridge in front of the UDP relay (which forwards
and, when capture is on, tees clean Ethernet frames to a pcapng `CapturePort`).
The wiring is computed once per (re)start as a whole-lab **bridge plan**
(`internal/server/bridgeplan.go`) so the NETMAP and the bridges agree.

**IOL netio socket convention (P0-confirmed via `lsof`).** Each IOL binds a unix
**datagram** socket at `/tmp/netio<uid>/<instance-id>` (`<uid>` = `os.Getuid()`;
instance-id = `netmap.InstanceID(nodeID)`). IOL derives a NETMAP peer's socket
path from that peer's instance id and sends its frames there (8-byte header). So
to bridge/capture a link we point the IOL endpoint's NETMAP entry at a
**pseudo-instance** that iouyap owns:

- **Pseudo-instance scheme** (`netmap.PseudoInstanceBase` /
  `AllocPseudoInstances`): each bridged IOL endpoint gets a pseudo-instance id
  from the reserved high pool **[500, 1024]**, allocated ascending and **skipping
  any id a real node already uses** (real nodes use `InstanceID(nodeID)=nodeID+1`,
  which starts at 1). The base is high enough to clear real instances in every
  practical single-host lab, and the skip guarantees no collision even if real
  instances reach into the pool. iouyap binds `/tmp/netio<uid>/<pseudoInstance>`.
- **NETMAP representation.** `netmap.Build(native, bridged...)` emits, for each
  bridged IOL endpoint, a line `<realInstance>:<adapter>/<port> <pseudoInstance>:0/0`.
  IOL treats the right-hand id like any peer and writes that interface's frames
  to `/tmp/netio<uid>/<pseudoInstance>` — the iouyap socket, not a peer IOL.
  `nativeLinkSpecs` still excludes bridged links from the native lines, so a port
  is never double-wired.

**The three seams** (all in `internal/server`, iouyap imported only in
`bridgeplan_linux.go`):

1. **NETMAP** — `netmapFor` renders native lines **plus** the plan's bridged
   pseudo-instance lines (`bridgePlan.bridgedEndpointsForNetmap`), written by
   `prepareLabDir` before any node spawns.
2. **iouyap bridges** — `prepareLabDir` → `startBridges` (Linux) creates
   `/tmp/netio<uid>` and starts one `iouyap.Bridge` per bridged IOL endpoint
   **before** IOL spawns (so the pseudo-instance socket exists when IOL
   connects), `go bridge.Run(ctx)`, tracked in `loadedLab.bridges` and
   `Close`d on stop (`stopBridges`, wired into `shutdown`/`lab.stop`).
3. **relay** — `relayConfigFor` returns the link's relay config **from the
   plan**, so the relay's UDP ports match the bridges. For each bridged IOL
   endpoint the bridge's `UDPRemote` = relay endpoint `LocalPort` (IOL→relay) and
   its `UDPLocal` = relay endpoint `RemotePort` (relay→IOL). `capture.start`
   records the intent, rebuilds the plan (flipping an IOL↔IOL link to bridged),
   and restarts the relay with the tee; `capture.stop` reverses it.

**Two supported cases:**

- **Capture on IOL↔IOL** — both endpoints become bridged: **2 iouyap bridges +
  1 p2p relay with the pcapng tee**. `capture.start` returns the `capturePort`.
  Because NETMAP is read once at boot, turning capture on/off on an
  already-running IOL↔IOL link only takes effect after the affected nodes
  **restart** (they re-read the NETMAP through the pseudo-instances).
- **VPCS↔IOL** — the IOL endpoint is bridged (pseudo-instance → iouyap → relay);
  the VPCS endpoint speaks UDP natively. `node.Spec.VPCSUDPLocal/Remote` (set in
  `buildSpec` from `bridgePlan.vpcsUDPFor`) become VPCS argv `-s <localUdp> -c
  <remoteUdp> -t 127.0.0.1`: VPCS **binds** the relay's delivery port (`-s` =
  relay `RemotePort`) and **sends** to the relay's receiving port (`-c` = relay
  `LocalPort`). The relay bridges the two endpoints (and tees if capture is on).

### Node id → IOL instance id (`netmap.InstanceID`)

IOL rejects instance id **0** and only accepts **1..1024**, but a lab `node.id`
is valid from 0 (schema `minimum: 0`). So the raw node id is **not** used as the
IOL instance id directly — `netmap.InstanceID(nodeID) = nodeID + 1` maps it into
range (node 0 → instance 1). `lab.Validate` calls `netmap.ValidateInstance` to
reject a node id that would exceed 1024 at load time. The instance id is applied
in the three places it must stay in sync — the **argv positional**
(`node.Spec.IOLArgv`), the **NETMAP node id** (`netmap.Entry.String`), and the
**`nvram_<id>` filename** (`server.nvramFilename`) — all via the one helper.
Console-port allocation is independent and stays keyed by node id.

### NVRAM startup-config injection (`internal/nvram`)

Hand-driving the console fights IOS-XE 17.18 PnP (P0), so nodes boot
**pre-configured**: `injectNVRAM` encodes a node's `startupConfig` into
`nvram_<id>` in the shared lab dir before spawn, and `buildSpec` sizes IOL's
`-n <KiB>` (via `node.NVRAMKiBFor`) to hold it. `config.save`/`config.extract`
decode the NVRAM file back out through the same codec. An empty `startupConfig`
writes no NVRAM (the node boots to its default config).

## WebSocket bridge (`internal/wsbridge`)

The TCP control protocol and raw telnet consoles aren't reachable from a
browser (no raw sockets), so the supervisor also exposes both over WebSocket
on a second loopback-only listener, `-ws-addr` (default `127.0.0.1:4001`).
This is what makes the desktop app's embedded webview and a plain browser
build use the *same* mechanism to reach the supervisor — there is no
separate browser-only code path.

### `GET /control`

Same NDJSON control protocol as the TCP listener (`docs/protocol.md`), just
carried over WebSocket **text frames** instead of newline-delimited bytes on
a raw socket: each text frame is exactly one JSON request object (no
embedded newline needed — the frame boundary *is* the message boundary).
Responses and pushed events come back the same way, one JSON object per text
frame.

This isn't a reimplementation of verb handling: both the TCP listener
(`internal/server`'s `ListenAndServe`/`serveConn`) and the WS `/control`
handler call the **same** `Server.ServeConn(ctx, rwc io.ReadWriteCloser)`
method — the shared per-connection NDJSON loop (decode request line →
dispatch through the one verb `Dispatcher` → encode response, plus
subscribing the connection to the event broadcaster). The WS handler just
wraps the `*ws.Conn` in a small adapter (`textFrameRWC`) that maps
`Read`/`Write` calls onto WS text frames — a bufio-style read buffer refills
from `ReadMessage()` and re-appends the `\n` the NDJSON decoder expects as
the line terminator; each `Write` (one NDJSON line already ending in `\n`)
becomes one WS text frame with that trailing newline stripped, since the WS
frame boundary already delimits the message. Verb dispatch, error codes, and
event payloads are therefore byte-for-byte identical to the TCP path — there
is exactly one implementation of every verb handler.

### `GET /console/{nodeId}`

Bridges to that node's telnet console port (the same port reported in
`lab.load`/`status`/`node.console`). The bridge:

1. Looks up the node's console port via `Server.ConsolePort(nodeId)` (404 if
   the lab isn't loaded or the node id is unknown).
2. Dials `127.0.0.1:<consolePort>` (plain TCP, the node's telnet listener).
3. Runs a bidirectional byte pump between the telnet socket and the WS
   connection until either side closes.

**Framing chosen:**
- **node → browser**: raw telnet bytes are fed through
  `internal/telnet.Negotiator`, which consumes/answers all IAC (`0xFF`)
  option-negotiation sequences server-side and emits only clean terminal
  bytes. Those clean bytes are sent to the browser as **binary** WS frames —
  xterm.js's `write()` accepts a `Uint8Array` directly, no decoding needed.
- **browser → node (keystrokes)**: sent as **binary** WS frames, written
  straight through to the telnet socket unmodified.
- **browser → node (resize)**: sent as a **text** WS frame containing a JSON
  object, `{"resize":{"cols":<int>,"rows":<int>}}`. This can be sent at any
  point in the session (not just before the first binary frame); the bridge
  translates it into an RFC 1073 NAWS subnegotiation
  (`IAC SB NAWS <cols-hi> <cols-lo> <rows-hi> <rows-lo> IAC SE`) written to
  the node's telnet socket. Text and binary frames are freely interleaved —
  the opcode alone distinguishes control messages from terminal data, so no
  in-band sentinel byte or length prefix is needed.
- **Reconnect**: the client simply opens a new WebSocket to
  `/console/{nodeId}`; the bridge dials a fresh telnet connection and starts
  a new `Negotiator` (telnet negotiation state is per-connection, not
  persisted). Nothing server-side needs explicit teardown/cleanup beyond the
  socket closes that already happen when a WS connection drops.
- **Multiple concurrent consoles**: each `/console/{nodeId}` WebSocket is an
  independent goroutine pair with its own telnet dial and its own
  `Negotiator`; there is no shared per-node session state, so opening
  consoles for several nodes (or the same node twice) at once just works.

### `POST /api/upload/image?filename=<basename>`

Puts an image file onto the VM for the browser GUI's "Add image" action. Body is
the raw file bytes (`application/octet-stream`); the endpoint does **not**
register the image — the GUI calls the `image.register` verb over `/control`
with the returned path afterwards.

- **Filename** is sanitized to a plain basename: any `/` or `\` path components
  are stripped, the remainder must match `[A-Za-z0-9._-]+` and end in `.bin` or
  `.iol` (case-insensitive); anything else is a 400.
- **Streaming** goes to `<ImageDir>/<name>.partial`, renamed to the final name on
  success, so an aborted/oversized transfer never leaves a truncated file (the
  `.partial` is deleted on any failure). The body is capped at 4 GiB via
  `http.MaxBytesReader`.
- **Responses**: `200 {"path":"/abs/path"}` on success; a JSON `{"error":...}`
  body with a 4xx/5xx status otherwise. If no image dir is configured the
  endpoint 503s.

The exact `/api/upload/image` pattern is more specific than the `/` catch-all, so
`ServeMux` routes it here rather than to the SPA fallback.

### Telnet IAC negotiation policy (`internal/telnet`)

`Negotiator` is a small explicit state machine (`Feed([]byte) []byte` +
`Reply() []byte`) that:
- strips all `IAC` (`0xFF`) command sequences from the data stream, including
  ones split across multiple `Read`s/`Feed` calls (TCP makes no framing
  guarantee here);
- un-escapes a literal `0xFF` data byte (`IAC IAC`);
- consumes and discards `IAC SB ... IAC SE` subnegotiations without emitting
  their payload as application data;
- answers `DO`/`DONT`/`WILL`/`WONT` automatically: **agrees** to Suppress
  Go-Ahead (both directions) and to the node enabling `ECHO` (Cisco-style IOL
  consoles echo server-side, so the bridge doesn't fight it) and `NAWS`;
  **refuses** everything else (`WONT`/`DONT`) so negotiation always
  terminates instead of hanging on an option neither side implements.

This mirrors the "answer everything so negotiation converges, get out of the
way otherwise" behaviour PNetLab's own webconsole bridge uses.

### Dependency choice: hand-rolled RFC 6455 (`internal/ws`), not a library

The supervisor had **zero** third-party dependencies before this change
(stdlib only, no `go.sum`). `internal/ws` (~350 lines) implements just the
server-side subset of RFC 6455 actually needed here: the HTTP Upgrade
handshake (`Accept`, via `http.Hijacker`), text/binary/ping/pong/close frame
read+write, masking/unmasking, and fragmented-message reassembly — no
permessage-deflate, no client-side framing. Rationale for hand-rolling over
taking `nhooyr.io/websocket` or `github.com/coder/websocket`:

- The RFC is small, stable (unchanged since 2011), and the loopback-only,
  single-trusted-client use case here doesn't need compression or strict
  fuzz-tested conformance — a general-purpose library would mostly be
  surface area we don't use.
- Keeps `go build`/`go mod tidy` fully offline-reproducible and keeps the
  supervisor's dependency graph (and therefore its trusted build/execution
  surface) unchanged.
- The seam is narrow (`ws.Conn.ReadMessage`/`WriteMessage`/`Accept`), so
  swapping to a real dependency later — if compression or wider client
  compatibility testing is ever needed — only touches `internal/ws` and its
  two callers in `internal/wsbridge`, not the protocol/dispatch layers.

If this ever needs to change, `go.mod`/`go.sum` are still stdlib-only today,
so a future `go mod tidy` after adding an import is a clean, easily reviewed
diff.

## Assumptions to verify against a real IOL image during the P0 spike

Every item below is implemented from the community-standard behaviour (GNS3 /
iouyap) and pinned to a **named constant or function** so P0 can correct it in
exactly one place. None of it embeds Cisco code.

1. **IOL netio header = 8 bytes** — `iouyap.HeaderSize`. ✅ CONFIRMED on real
   IOL 17.18.02 (docs/p0-spike.md "netio header layout"): big-endian
   `dst_id[2] src_id[2] dst_port[1] src_port[1] msg_type[1] channel[1]`, port
   byte packed `port<<4|adapter`, data msg_type `1`. The header exists only on
   the netio unix-socket side: `internal/iouyap` strips it toward UDP and
   constructs a correctly-addressed one toward IOL, so the UDP mesh (relay,
   VPCS, pcapng tee) carries raw Ethernet frames.

2. **IOU keygen algorithm** — `iourc.Key`.
   `key = int(hostid,16) + sum(ord(c) for hostname)`, then
   `md5(pad1 + pad2 + be32(key) + pad1)[:16]`, with the two well-known pad
   constants. **Verify** a real IOL accepts the generated key for the runtime's
   actual `hostid`+`hostname` and that the `~/.iourc` line format
   (`[license]\n<hostname> = <key>;`) is honoured.

3. **NVRAM binary format** — `nvram.Encode/Decode`, `nvram.BaseAddress`,
   `nvram.DefaultIOSVersion`.
   36-byte startup header (`magic 0xABCD`, format, internet checksum at off 4,
   IOS version, start/end addr, length) + config text + 4-byte padding, optional
   16-byte private-config header (`magic 0xFEDC`). **Verify** the exact
   `BASE_ADDRESS`, whether a real IOL is picky about the IOS-version field, and
   whether an empty/absent private-config section is accepted. Also confirm the
   **NVRAM filename** IOL writes (`server.nvramFilename` = `nvram_%05d`).

4. **IOL argv + console mechanism** — `node.Spec.IOLArgv`, `spawn_linux.go`.
   **RESOLVED in P0.** We pass `<image> [-e groups] [-s groups] -n <KiB>
   <instance-id>` and **deliberately omit `-l`** (the keepalive flag causes 100%
   idle CPU spin). The console is **not** a TCP port IOL opens and **not** an env
   var — it is IOL's controlling **pty**, bridged to `ConsolePort` by the
   supervisor (see "Node runtime model" above). `-n` is now sized to the injected
   NVRAM (`NVRAMKiBFor`). Remaining to verify on the real VM: that the argv order
   and `-n` sizing are accepted by every image line, and console readability
   end-to-end through the pty bridge under load.

5. **VPCS argv + spawn model** — `node.Spec.VPCSArgv`, `spawnVPCS` (spawn_linux.go).
   **RESOLVED on the VM (vpcs 0.8.3).** `vpcs -p <ConsolePort> -i <count> -s
   <localUdp> -c <remoteUdp> -t 127.0.0.1`. Unlike IOL, vpcs is its OWN telnet
   console server (it opens `-p <ConsolePort>` itself) and it DAEMONIZES (forks;
   the launcher exits immediately). So vpcs is spawned via a DISTINCT path
   (`spawnVPCS`): no pty, the supervisor does NOT bind ConsolePort, the process
   is put in its own group (`Setpgid`) so `Stop` kills the group (no orphan
   daemon), and the node is marked running only once ConsolePort accepts TCP
   (`waitConsoleReady`, 5s). vpcs 0.8.3 has NO name flag (`-N` is rejected).
   Remaining to verify on the VM: a PC pings the IOL LAN through the iouyap+relay
   path (`ip 10.0.0.10 10.0.0.1 24` then `ping 10.0.0.1`, P0 step 6), and that
   `node.stop`/`lab.stop` leave no orphan vpcs. Unexpected-death detection for a
   daemonized vpcs is currently minimal (a future periodic ConsolePort-listen
   check); documented here as a known gap.

6. **UDP tunnel port pairing** — `server.bridgePlan` (bridgeplan.go).
   Each bridged link endpoint gets a local (relay-receives) and remote
   (node-receives) UDP port; the IOL side's iouyap bridge pairs
   `UDPRemote=LocalPort` / `UDPLocal=RemotePort`, the VPCS side maps `-c`/`-s` to
   the same ports. **Verify** on the VM that frames flow both directions across
   the pairing (captured pcap shows the ICMP echoes, P0 step 7).

7. **L2/L3 class heuristic** — `image.SniffClass`.
   Presence of >=2 switching markers (`spanning-tree`, `mac-address-table`,
   `switchport`, `vlan`, `Switching`) implies L2. Advisory only; the GUI lets the
   user override. **Verify** against real L2 and L3 IOL binaries and tune the
   marker set if needed.

8. **Does IOL send `WILL ECHO`?** — `internal/telnet.Negotiator.handleOption`.
   The negotiator currently *agrees* to the node echoing (`IAC DO ECHO` in
   reply to `IAC WILL ECHO`), on the assumption IOL's console behaves like a
   classic Cisco CLI (server-side echo, line/character mode negotiated via
   `SGA`). If a real IOL console instead expects the **client** to echo (or
   sends nothing at all and just starts printing), xterm.js may show doubled
   or missing characters; the `WILL`/`DO` cases in `handleOption` are the
   single place to flip this policy.

9. **Does IOL honour NAWS?** — `internal/telnet.NAWS`, `wsbridge.handleTextFrame`.
   The bridge sends an RFC 1073 NAWS subnegotiation on resize, but never
   sends the opening `IAC WILL NAWS` volunteer message itself (`telnet.WillNAWS`
   exists but is currently unused by the bridge). **Verify** whether IOL/VPCS
   requires the client to first announce `WILL NAWS` before it will accept an
   unsolicited subnegotiation, and whether it does anything with the window
   size at all (Cisco IOS's `terminal width`/`length` are usually set
   in-band via CLI, not always wired to NAWS on IOL builds).

10. **Console port readiness race** — `wsbridge.handleConsole`.
    `/console/{nodeId}` dials the console port as soon as `ConsolePort`
    returns one (i.e. once the lab is loaded), not once the node is actually
    `running`. Today `node.start` flips to `running` optimistically as soon
    as the process spawns (see `server.startNodes`); if a real IOL takes
    noticeably long to open its telnet listener after process start, an
    early console dial will fail with connection-refused. **Verify** the
    real startup latency and consider gating the dial (or retrying briefly)
    on the `node.console` event instead of dialing immediately on WS
    connect.
