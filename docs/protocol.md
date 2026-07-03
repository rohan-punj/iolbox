# Supervisor control protocol (contract)

The GUI (Windows) talks to the **supervisor** (inside the Linux runtime) over a
single TCP connection. This is the seam that makes the runtime provider pluggable:
the GUI never knows whether the supervisor is in WSL2, a VMware VM, a remote box,
or QEMU.

## Transport

- **Control**: newline-delimited JSON (NDJSON), one request or event per line,
  UTF-8. Request/response are correlated by `id`. Server may also push unsolicited
  `event` messages. Bound to `127.0.0.1` inside the runtime; the provider exposes
  it to Windows localhost (WSL forwarding / host-only IP:port / ssh tunnel).
- **Consoles**: raw TCP telnet, one port per node (allocated at start, reported in
  status). GUI connects an xterm.js session per port.
- **Capture**: raw TCP pcapng byte stream, one port per capturing link (allocated
  on capture-start). The Windows helper pipes it into `wireshark -k -i -`. The
  WebSocket bridge also re-exposes this stream to browsers at
  `GET /capture/{linkId}` (see below).

Default control port: **4000**. Console base: **9000+**. Capture base: **5500+**.
All configurable; actual allocations always come back in `status`/responses.

## Framing

Request:
```json
{"id":"<client-uuid>","op":"<verb>","args":{...}}
```
Response (success):
```json
{"id":"<client-uuid>","ok":true,"result":{...}}
```
Response (error):
```json
{"id":"<client-uuid>","ok":false,"error":{"code":"...","message":"..."}}
```
Event (server push, no id correlation required):
```json
{"event":"node.state","data":{"node":3,"state":"running"}}
```

## Verbs

### `hello`
Handshake + capability/version negotiation. First message.
- args: `{ "client": "iolab-gui/0.1.0" }`
- result: `{ "supervisor": "0.1.0", "runtime": "debian-slim-12", "arch": "x86_64", "features": ["nvram","capture","i386"] }`

The `features` array always contains the base capabilities `nvram`, `capture`,
`i386`. It additionally contains:
- `natgw` — the runtime supports **nat** nodes (has `/dev/net/tun` and
  passwordless `sudo`).
- `mgmt` — the runtime supports **mgmt** nodes (the above plus a usable
  management interface).

These are detected once at supervisor startup. Starting a `nat`/`mgmt` node on a
runtime that did not advertise the matching feature returns an `unsupported`
error (`"runtime does not support nat/mgmt nodes"`).

### `image.list`
- result: `{ "images": [ { "id","filename","class","arch","sha256","size" } ] }`

### `image.register`
Called after the provider has synced an image file into the runtime. Supervisor
fingerprints + sniffs class/arch authoritatively.
- args: `{ "path": "/opt/iolab/images/<file>" }`
- result: `{ "id","class","arch","sha256" }`

### `lab.load`
Load (not start) a lab document. Validates against schema, allocates ids.
- args: `{ "lab": <lab.json> }`
- result: `{ "labId", "nodes":[{"id","consolePort"}], "warnings":[...] }`

### `lab.saveDoc` / `lab.listDocs` / `lab.getDoc` / `lab.deleteDoc`
Durable lab-document store, separate from the runtime `lab.load`/`lab.start`
lifecycle: these verbs persist and retrieve whole lab documents on disk
(`<labs-dir>/<id>.json`, default `/opt/iolab/labs`, configurable with
`-labs-dir`) without loading or starting anything. The document is stored
byte-for-byte as received (unknown fields preserved); the id comes from the
document's own `id` field and must match `[A-Za-z0-9_-]+`.
- `lab.saveDoc` — args: `{ "lab": <lab.json> }`; result: `{ "id": "<lab id>" }`.
  Creates the labs dir on first save and overwrites any existing copy.
- `lab.listDocs` — args: `{}`; result: `{ "labs": [ <lab.json>, ... ] }` (every
  stored doc, parsed back from disk; unreadable/malformed files are skipped).
- `lab.getDoc` — args: `{ "labId": "..." }`; result: `{ "lab": <lab.json> }`;
  `not_found` error if absent.
- `lab.deleteDoc` — args: `{ "labId": "..." }`; result: `{}`. Deleting a missing
  document is not an error.

### `lab.start` / `lab.stop`
Start/stop all nodes (or a subset).
- args: `{ "labId", "nodes":[<id>...]|null }`
- result: `{ "started":[{"node","consolePort","pid","state"}] }`

### `lab.wipe`
Reset node state (like PNetLab's wipe): stop the targeted nodes and delete their
persisted per-node NVRAM (`nvram_<id>`) so they next boot from the injected
startup-config again. Non-existent NVRAM is not an error.
- args: `{ "labId", "nodes":[<id>...]|null }`
- result: `{ "wiped":[<id>...] }`

### `node.start` / `node.stop` / `node.restart`
Single-node lifecycle. Same shape as above for one node.

### `node.setImage`
Hot-swap the image bound to a node (applied on next start).
- args: `{ "labId","node","imageId" }`
- result: `{ "node","imageId","class" }`

### `link.add` / `link.remove`
Wire/unwire two endpoints at runtime (creates/destroys UDP relay or hub port).
- args: `{ "labId","link": <link.json> }`

### `capture.start` / `capture.stop`
Tee a link to a pcapng TCP stream (and/or file).
- args: `{ "labId","link":<id>,"mode":"live|file","file":"<path?>" }`
- result: `{ "link","capturePort","file?" }`

**Doc-driven auto-arm:** every lab (re)start additionally arms a capture for
each doc link whose `capture.enabled` is true — a port is allocated and the
link's relay starts with its pcapng tee **before** any node spawns, and a
`capture.started {link,capturePort}` event is emitted per armed link on every
start (idempotent — the GUI re-learns ports after reloads/restarts and uses
them to reconnect live capture tabs). So "enable capture and restart the lab"
works with no explicit `capture.start` call. A full `lab.stop` stops the tee'd
relays, emits `capture.stopped` per link, and releases the ports; the next
start re-arms from the doc. `lab.load` releases the outgoing lab's capture and
relay/bridge UDP ports silently.

The tee listener binds the host given by the supervisor's `-capture-bind` flag
(default `127.0.0.1`; the `/capture/{linkId}` WS bridge always dials loopback).
With `-capture-bind 0.0.0.0` a native Wireshark on the GUI host can attach
directly: `wireshark -k -i TCP@<vm-ip>:<capturePort>`.

### `config.save` / `config.extract`
Extract NVRAM startup-configs back out of running/last-run nodes into the lab doc.
- args: `{ "labId","nodes":[<id>...]|null }`
- result: `{ "configs":[{"node","startupConfig"}] }`

### `status`
Full snapshot.
- result: `{ "labId", "nodes":[{"id","state","consolePort","pid","ram","image"}], "links":[{"id","capturePort?"}] }`

## Events (server → GUI push)

- `node.state`  `{node,state}` — state ∈ `starting|running|stopped|crashed`
- `node.console` `{node,consolePort}` — console became reachable
- `link.up` / `link.down` `{link}`
- `capture.started` / `capture.stopped` `{link,capturePort}`
- `link.stats` `{link,fps,bps}` — per-link forwarded throughput over the last 2s
  sampling interval: `fps` is frames/sec forwarded (float, one decimal), `bps`
  is bytes/sec forwarded, both summed across directions (and hub fan-out).
  Emitted at most every 2s and ONLY for a link that forwarded traffic during the
  interval (idle links stay silent), so the GUI can drive traffic-based link
  glow directly off these events. Only **bridged** links (VPCS, segment,
  captured, cross-host) have a relay and therefore stats; native same-host
  IOL↔IOL links carry traffic via the whole-lab NETMAP with no relay and produce
  no `link.stats` events.
- `log` `{level,message,node?}`

## WebSocket bridge endpoints (browser transport)

The supervisor's WS bridge (default `:4001`) re-exposes the control protocol and
per-node/per-link byte streams to browsers, which cannot open raw TCP sockets:

- `GET /control` — the NDJSON control protocol, one JSON object per text frame,
  dispatched through the same handler core as the TCP control listener.
- `GET /console/{nodeId}` — the node's telnet console as binary WS frames (after
  server-side IAC negotiation); a `{"resize":{"cols":C,"rows":R}}` text frame
  propagates a NAWS window-size update.
- `GET /capture/{linkId}` — the link's active pcapng capture stream as binary WS
  frames, so a browser can render a live packet view. Upgrades to WebSocket then
  pumps the raw pcapng byte stream from the link's capture port until either
  side closes; client→server frames are ignored/drained. Returns **404** with a
  JSON body `{"error":"..."}` (before the upgrade) when the link has no active
  capture.

## Error codes

`schema_invalid`, `image_not_found`, `image_arch_mismatch`, `iourc_failed`,
`node_spawn_failed`, `port_unavailable`, `nvram_codec_failed`, `not_loaded`,
`not_found`, `unsupported`.

## Node state machine

```
stopped ─start→ starting ─(iol ready)→ running ─stop→ stopped
                    │                        │
                    └────── crashed ─────────┘  (crashed also reachable from running)
```

## Interface addressing (IOL)

- NETMAP line per connection: `<a>:<iface> <b>:<iface>` where a node's NETMAP id is
  the lab `node.id`, and iface encodes `port = adapter*16 + portInAdapter`.
- `e0/0` → adapter 0, port 0. `s1/2` → serial adapter 1, port 2.
- p2p links: direct UDP tunnel between the two `iol_wrapper` sockets, no relay.
- segment links: every endpoint tunnels to a userspace hub that floods frames.
- Any link may be intercepted by inserting the tee relay in the path (capture).

## External-world node kinds (`nat`, `mgmt`)

Two special node kinds connect a lab to the outside world. Both are
**supervisor-internal** endpoints — no spawned process — that pump raw ethernet
frames between a kernel tap/macvtap device and the connected link's UDP relay
(the mesh carries raw ethernet, so a tap fd yields/accepts exactly those
frames). Topologically they behave like a VPCS endpoint: the link is bridged and
the endpoint gets its relay UDP ports from the whole-lab bridge plan. They have
exactly one connectable interface, `eth0`, and may be referenced by at most one
link endpoint. They have no console. The node state machine still reports
`running`/`stopped`, so the GUI treats them uniformly.

- **`nat`** — a NAT gateway. The supervisor creates a tap with a per-node
  gateway address `172.31.<n>.1/24` (`<n>` allocated per nat node), enables
  `ip_forward`, adds an iptables MASQUERADE for `172.31.<n>.0/24` out the VM's
  default-route interface plus a FORWARD accept pair, and runs a minimal DHCP
  server on the tap (pool `172.31.<n>.100-199`, router+DNS = the gateway, 1h
  lease). A connected lab node can `ip dhcp` and reach the internet. Requires the
  `natgw` feature.
- **`mgmt`** — a management-network bridge. The supervisor creates a `macvtap`
  in bridge mode on the VM's management interface (configurable via the
  supervisor's `-mgmt-iface` flag; empty auto-picks the first UP non-loopback
  ethernet iface that is not the default-route iface). Connected lab nodes appear
  directly on the management L2 network with their own MACs. No IP/NAT on the
  supervisor side. Requires the `mgmt` feature.

Lab-doc shape (nat/mgmt node + its single link):
```json
{"id":9,"kind":"nat","name":"Internet","x":0,"y":0}
{"id":10,"kind":"mgmt","name":"OOB","x":0,"y":0}
{"id":0,"endpoints":[{"node":1,"interface":"e0/0"},{"node":9,"interface":"eth0"}]}
```
