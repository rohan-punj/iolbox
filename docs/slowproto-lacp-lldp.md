# Slow-protocols passthrough: LACP userspace tee + LLDP group_fwd freebie

Branch `feat/lacp-tee-lldp`. Two independent changes so IOS control protocols
that a stock Linux bridge drops can cross the iolbox fabric.

## Background (verified on the OVA, kernel 6.1.0-47)
The fabric is a Linux bridge (`iolbr<linkid>`) per link. It forwards data, CDP,
STP, and PAgP fine. It does **not** forward reserved link-local multicasts in the
`01:80:C2:00:00:0X` range that aren't enabled via `group_fwd_mask`, and the
kernel **refuses** to enable bits 0/1/2 (STP/pause/**LACP**) at all —
`group_fwd_mask` accepts `0xFFF8` but returns EIO for `0xFFFF` or bit 2. So:

- **LACP** (`01:80:C2:00:00:02`, EtherType `0x8809` slow-protocols): blocked;
  `channel-group … mode active/passive` port-channels stay Down, members `(s)`.
- **LLDP** (`01:80:C2:00:00:0E`), **EAPOL/802.1X** (`…:03`), etc.: blocked by
  default but the kernel *does* allow forwarding them via `group_fwd_mask`.
- Static (`mode on`) and **PAgP** (`mode desirable`, Cisco SNAP multicast) work
  already — leave them alone.

## Change 1 — LLDP freebie (`internal/fabric/commands.go`)
Create fabric bridges with `group_fwd_mask = 0xFFF8` so `…:03–0F` forward
(LLDP + 802.1X + others). Bit 2 (LACP) stays restricted — that's Change 2.
- `bridgeCreateCmds`: add `group_fwd_mask 0xfff8` (prefer at `ip link add …
  type bridge group_fwd_mask 0xfff8`; if the iproute2 on the runtime rejects it
  at add-time, append `ip link set <name> type bridge group_fwd_mask 0xfff8`).
- `0xFFF8` deliberately, NOT `0xFFFF` — the kernel returns EIO on bits 0/1/2.
- Test in `commands_test.go` asserting the argv carries `group_fwd_mask`+`0xfff8`.
- Works on every deployment target (pure bridge attribute, no kernel module).

## Change 2 — LACP userspace tee (`internal/slowtee`, wired in `fabric_linux.go`)
A small AF_PACKET forwarder that carries ONLY the slow-protocols multicast
(`01:80:C2:00:00:02`) between the two member taps of a p2p fabric link — the one
frame class the bridge can't pass and the kernel won't let it. Everything else
stays on the kernel bridge untouched. Portable (userspace, all targets), no DKMS.

Model it on `internal/dirstat` (same AF_PACKET-per-tap + `sll.Pkttype`
direction handling + Close teardown):
- New package `internal/slowtee` (slowtee.go / slowtee_linux.go /
  slowtee_other.go / slowtee_test.go).
- `Open(devs []string) (*Tee, error)`: for exactly two tap devnames, bind a raw
  `AF_PACKET/SOCK_RAW` socket to each (by ifindex, like dirstat.bindTap), one
  read goroutine per socket. Each goroutine reads a FULL frame (snaplen ~1600,
  not header-only), forwards it to the OTHER tap via `Sendto` with the peer's
  `SockaddrLinklayer{Ifindex:…}`.
- FORWARD ONLY frames that are (a) incoming — `sll.Pkttype != PACKET_OUTGOING`
  (drop our own injected frames → no loop) — AND (b) slow-protocols: dst MAC ==
  `01:80:c2:00:00:02` (equivalently EtherType `0x8809`). Tight filter so normal
  traffic is never duplicated (the bridge already carries it).
- `Close()`: idempotent, closes both fds, joins goroutines (dirstat pattern).
- Non-linux `slowtee_other.go`: `Open` returns nil (no-op).
- `slowtee_test.go`: unit-test the pure "is this a slow-protocols frame" filter
  (AF_PACKET itself needs root/netns, so keep the socket path out of unit tests
  — mirror how dirstat_test.go tests only the pure logic).

Wiring in `internal/server`:
- `loaded.go`: add `slowtees map[int]*slowtee.Tee` to loadedLab (init in the
  constructor next to `dirstats`).
- `fabric_linux.go`: add `openLinkSlowTee(ll, l)` mirroring `openLinkDirstat` —
  resolve devs via `fabricLinkTapDevs(ll, l)`; only open when `len(devs) == 2`
  and BOTH endpoints are IOL switches/routers (skip NAT/VPCS/segment; a
  port-channel is switch↔switch). Close+replace `ll.slowtees[l.ID]`.
  Call it right after `s.openLinkDirstat(ll, l)` in `attachFabricLink`.
- Close every tee in `teardownFabric` (alongside the `dirstats` close loop) and
  in the link-detach path (mirror how dirstats are torn down).
- SCOPE v1: p2p fabric links only (2 taps). N-port segments (hubs) are out of
  scope — LACP across a shared segment would need flood-to-all-members; document
  and skip.

## Gates (both changes)
- `GOOS=linux` and `GOOS=windows`: `go build ./... && go vet ./... && go test ./...`
  all green. stdlib-only.
- Manual (owner, on the OVA, at review): two L2 IOL switches, two links,
  `channel-group 1 mode active` on both → `show etherchannel summary` shows
  `Po1(SU)` with members `(P)` bundled (was `(SD)` / `(s)`); LLDP neighbors
  appear (`show lldp neighbors`) after enabling `lldp run`.

## Do NOT
- Merge to main — the owner merges when ready.
- Touch the kernel / ship a DKMS module (portability + WSL2/container targets
  have no controllable kernel; the userspace tee is deliberately chosen instead).
- Change PAgP/static/CDP/STP paths — they already work.
