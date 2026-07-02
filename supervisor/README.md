# iolab supervisor

The **supervisor** is the control + data plane for iolab. It is a single static
Go binary that runs **inside the Linux runtime** (WSL2, a VMware helper VM, a
remote box, or QEMU) and drives Cisco IOL + VPCS nodes: it spawns the processes,
wires them together over UDP tunnels, tees links to Wireshark, generates the
IOU license, and injects/extracts NVRAM startup-configs.

The Windows GUI never talks to IOL directly — it speaks the NDJSON control
protocol (`docs/protocol.md`) to this supervisor over a loopback TCP socket.

## Layout

```
supervisor/
  cmd/supervisor/      entrypoint: flags, control server, ws bridge, graceful shutdown
  internal/protocol/   NDJSON request/response/event framing + verb dispatcher
  internal/lab/        lab.schema.json Go structs + Validate()
  internal/netmap/     IOL interface addressing (e0/0 -> port index) + NETMAP file
  internal/relay/      UDP data plane: p2p forward, segment hub, pcapng capture tee
  internal/node/       process mgmt: state machine, port alloc, IOL/VPCS argv, spawn
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

1. **IOL UDP header size = 8 bytes** — `relay.IOLHeaderSize`.
   The iouyap header is `dst_ids[2] src_ids[2] dst_port[1] src_port[1]
   msg_type[1] channel[1]`. The capture tee strips these 8 bytes so the pcapng
   contains clean Ethernet frames. **Verify** the real on-wire header length and
   that the Ethernet frame begins immediately after it.

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

4. **IOL argv + console mechanism** — `node.Spec.IOLArgv`, `node.Spec.Environ`.
   We pass `<image> [-e groups] [-s groups] -n 64 <instance-id>` and
   **deliberately omit `-l`** (the keepalive flag causes 100% idle CPU spin).
   The telnet console port is advertised via env (`IOL_CONSOLE_PORT`, alongside
   `IOURC`). **Verify** the exact IOL flags, and whether IOL selects its telnet
   console via the environment or via its built-in default
   (`127.0.0.1:(2000+id)`); adjust `IOLArgv`/`Environ` accordingly.

5. **VPCS argv** — `node.Spec.VPCSArgv`.
   `vpcs -N <name> -p <consolePort>`, up to 9 PCs per process, per-PC UDP tunnels
   wired by the relay layer. **Verify** the exact VPCS UDP tunnel flags
   (`-s`/`-c`/`-e`) for the bundled VPCS build.

6. **UDP tunnel port pairing** — `server.buildRelayConfig`.
   Each endpoint gets a local (relay-receives) and remote (node-receives) port.
   **Verify** how `iol_wrapper` / VPCS expect the local/remote UDP ports to be
   configured (NETMAP + per-instance socket paths vs. explicit UDP ports).

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
