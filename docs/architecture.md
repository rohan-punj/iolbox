# Architecture — the data plane in detail

The high-level layer diagram is in [PLAN.md](../PLAN.md). This doc explains the one
genuinely load-bearing idea: **how nodes are wired without bridges, tap devices, or
root** — which is what lets iolab stay tiny.

## Everything is a UDP tunnel

IOL exposes each interface as a UDP socket pair (the `iol_wrapper` / iouyap
pattern). VPCS does the same natively (`-t`/`-u`). So "connecting two interfaces"
means: frames leaving interface A's UDP socket are delivered to interface B's UDP
socket, and vice versa. No kernel networking is involved at all.

```
IOL R1 e0/0  ──UDP──┐                 ┌──UDP──  IOL R2 e0/0
                    │  (p2p: direct)  │
                    └─────────────────┘
```

### Point-to-point links (`type: p2p`)
Exactly two endpoints. The supervisor tells each `iol_wrapper` the other's
UDP address; frames flow directly. **No relay process** in the steady state — the
lightest possible link.

### Segment links (`type: segment`)
More than two endpoints share a medium. The supervisor runs a small userspace
**hub**: every frame received from one member is flooded to all others. Hub (not
switch) semantics for v1 — no MAC learning — which is correct for a shared segment
and keeps the code trivial. VPCS and IOL L2 images provide any real switching.

## NETMAP and interface addressing

IOL reads a `NETMAP` file describing which node/interface connects to which. A
node's NETMAP id is its lab `node.id`. An interface encodes as
`port = adapter*16 + portInAdapter`:

- `e0/0` → adapter 0, port 0 → NETMAP port `0`
- `e0/1` → adapter 0, port 1 → NETMAP port `1`
- `s1/2` → serial adapter 1, port 2 → NETMAP port `18`

The supervisor's `netmap` package generates the file and the per-node working
directories at lab start. (This matches the `id = port*16 + group` convention used
by the existing IOL lab packs.)

## Capture is a tee, not a tap

Because frames already pass through userspace (the wrapper sockets, and the hub for
segments), capturing a link is just **copying** frames to a second sink — a pcapng
stream — as they go by. For a p2p link the supervisor inserts a pass-through relay
in the path only while capture is active; the relay:

1. forwards A↔B as before, and
2. writes each frame to a **pcapng** stream on a capture TCP port.

The IOL UDP payload carries a small fixed header ahead of the ethernet frame; the
relay strips it so the pcap holds clean ethernet frames (`LINKTYPE_ETHERNET`). On
Windows, `capture-helper` pipes that stream into `wireshark -k -i -`. See
[tools/README.md](../tools/README.md).

No promiscuous mode, no WinPcap/Npcap capture privileges, no bridge mirror port —
the frames are ours to copy because they were never in the kernel to begin with.

## Why this stays lightweight

| Heavy thing PNetLab/EVE need | Why iolab doesn't |
|---|---|
| tun/tap + Linux bridges per link | links are UDP tunnels in userspace |
| root daemon / setuid helpers | no kernel networking to privilege |
| Npcap/pcap capture stack | capture is a userspace frame copy |
| QEMU/Dynamips/Docker runtimes | only IOL (ELF) + VPCS (tiny) |
| web server + DB + multi-user auth | single-user, one JSON file, localhost |

The cost we accept: IOL/VPCS only, single-user, and a small Linux runtime to host
the ELF binaries. That trade is the entire point.
