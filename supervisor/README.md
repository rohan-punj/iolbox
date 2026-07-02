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
  cmd/supervisor/      entrypoint: flags, control server, graceful shutdown
  internal/protocol/   NDJSON request/response/event framing + verb dispatcher
  internal/lab/        lab.schema.json Go structs + Validate()
  internal/netmap/     IOL interface addressing (e0/0 -> port index) + NETMAP file
  internal/relay/      UDP data plane: p2p forward, segment hub, pcapng capture tee
  internal/node/       process mgmt: state machine, port alloc, IOL/VPCS argv, spawn
  internal/iourc/      IOU license (iourc) generation from hostid+hostname
  internal/nvram/      IOL NVRAM startup-config encode/decode (GNS3 iou codec)
  internal/image/      image fingerprint (sha256), ELF arch parse, L2/L3 class sniff
  internal/server/     ties it together: control server + verb handlers + lab state
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
./supervisor -control-addr 127.0.0.1:4000 -image-dir /opt/iolab/images -run-dir /run/iolab
```

The control server binds **loopback only** (it refuses any non-loopback host).
Console ports are allocated from 9000+, capture ports from 5500+, and internal
UDP tunnel ports from 10000+ (all reported back in responses/`status`).

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
