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
  on capture-start). The Windows helper pipes it into `wireshark -k -i -`.

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

### `lab.start` / `lab.stop`
Start/stop all nodes (or a subset).
- args: `{ "labId", "nodes":[<id>...]|null }`
- result: `{ "started":[{"node","consolePort","pid","state"}] }`

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
- `log` `{level,message,node?}`

## Error codes

`schema_invalid`, `image_not_found`, `image_arch_mismatch`, `iourc_failed`,
`node_spawn_failed`, `port_unavailable`, `nvram_codec_failed`, `not_loaded`,
`unsupported`.

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
