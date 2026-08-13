# P0 spike — risk-retirement runbook

P0 proves the risky, unknown parts work against a **real IOL image** before we
build more on top. If every step below passes, everything after P0 is known-good
engineering. No GUI is involved — this is supervisor + runtime + capture only.

Requires: one real IOL image (user-supplied, L3), VMware Workstation (primary
provider here), Wireshark, and the built supervisor + runtime.

## Build inputs

```
# supervisor (linux target)
cd supervisor && GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor

# runtime appliance with that supervisor baked in (on a Linux builder)
cd runtime && ./build-all.sh ../supervisor/bin/supervisor-linux-amd64

# capture helper (windows)
cd tools/capture-helper && GOOS=windows GOARCH=amd64 go build -o capture-helper.exe .
```

## Steps & pass criteria

| # | Step | Pass criteria | Assumption it validates |
|---|------|---------------|--------------------------|
| 1 | `vmrun -T ws start iolbox-appliance.vmx nogui`; connect to control port | `hello` returns supervisor version + `features` includes `i386` | VMware provider boots headless; endpoint reachable from Windows |
| 2 | Copy IOL image into runtime; `image.register` | returns id, `class`, `arch=i386\|x86_64` | ELF sniff + i386 multiarch present |
| 3 | First-boot iourc generated | `/opt/iolbox/iourc` exists; IOL licenses OK | keygen algorithm correct for this hostid |
| 4 | `lab.load` + `node.start` one IOL (R1); telnet its console port from Windows | IOS boots to prompt; **no `-l` flag** → idle CPU low | console mechanism + keepalive-flag fix |
| 5 | Start R2; `link.add` p2p R1e0/0–R2e0/0; configure /30 both sides | `ping` R1→R2 succeeds | UDP p2p wiring + NETMAP encoding |
| 6 | Start VPCS PC1; link to R1 e0/1; set PC IP | PC1 pings R1 LAN IP | VPCS UDP tunnel + mixed IOL/VPCS segment |
| 7 | `capture.start` on the R1–R2 link; run `capture-helper -connect <ip>:<capPort>` | Wireshark opens, shows the ICMP echoes as clean ethernet frames | pcapng tee + iouyap header strip/rewrite + helper bridge |
| 8 | Close Wireshark, reopen via helper `-relaunch` | capture resumes with valid header | header-buffer/relaunch logic |
| 9 | `config.save` R1; stop; start; `config.extract` | startup-config round-trips through NVRAM | NVRAM codec |
| 10 | Repeat step 1–7 under the **wsl2** provider | same results | provider abstraction holds across backends |

## Known assumptions to confirm here

These were flagged during the initial build as needing a real image — P0 is where
they get confirmed or corrected. Each has a clearly-marked spot in the code:

- **IOL netio header** (layout + which side of the bridge carries it) —
  `supervisor/internal/iouyap`. ✅ CONFIRMED, see "netio header layout" below;
  the UDP mesh is raw ethernet, the header exists only on the unix netio side.
- **Telnet console mechanism** (native IOL telnet port vs pty bridge) — `supervisor/internal/node`.
- **iourc keygen** hostid→key — `supervisor/internal/iourc`.
- **NVRAM format** header/checksum — `supervisor/internal/nvram`.
- **L2 vs L3 class heuristic** — `supervisor/internal/image`.
- **pcapng first-read-is-header** heuristic — `tools/capture-helper`.
- **supervisor `-gen-iourc` flag** name — coordinated with `runtime/firstboot-iourc.sh`.
- **host-only IP** (default `192.168.171.2`) — `runtime/pack-vmware.sh` + vmware provider.

## P0 findings — 2026-07-02 (real IOL 17.18.02, x86_64)

Environment: isolated Ubuntu 24.04 VMware VM (`J:\iolbox-runtime-vm`, host-only
192.168.111.0/24, key `iolbox_key`), built from an Ubuntu cloud image — no WSL, no
project VMs touched. Supervisor + both images + VPCS 0.8.3 deployed to `/opt/iolbox`.

**Confirmed:**
- ✅ **Image sniff**: `x86_64_crb_linux-...iol17.18.02.bin` is ELF64 x86-64; runs on
  stock Ubuntu 24.04 glibc — **no i386 multiarch needed** for the 17.18.02 line
  (`ldd` shows all libs present). i386 only matters for the older `i86bi_` images.
- ✅ **iourc keygen ACCEPTED by real IOL.** `supervisor -gen-iourc` on hostid
  `a8c0986f` / hostname `iolbox-rt` produced `iolbox-rt = 97824a8bfa46e85b;`. IOL
  booted (`IOS On Unix - Cisco Systems confidential…`) with **no license error**.
  The community keygen in `internal/iourc` is correct.
- ✅ **Console mechanism = pty, NOT a TCP port.** IOL uses stdin/stdout on a
  controlling pty for its console and opens **no** telnet port itself. A
  pty→TCP bridge (validated here with `socat TCP-LISTEN,fork EXEC:…,pty,setsid,ctty`)
  exposes it as telnet. **Action:** `internal/node/spawn_linux.go` must allocate a
  pty, run IOL attached to it (`setsid`+`ctty`), and bridge that pty to the node's
  telnet console port — the `wsbridge` console path then dials that port. Resolves
  the open "console mechanism" assumption (it is neither env-selected nor 2000+id).
- ✅ Benign: IOL prints `Warning: Abnormal ciscoversion string … we parsed - NULL`
  on this build — not fatal, ignore.

- ✅ **NETMAP wiring CARRIES TRAFFIC between two real IOL.** Two IOL instances
  sharing a dir with `NETMAP = "1:0/0 2:0/0"` (native unix-socket netio, no relay,
  no supervisor): both sides reported `%LINEPROTO-5-UPDOWN: Line protocol on
  Interface Ethernet0/0, changed state to up`. IOL only brings Et line protocol up
  when the netio peer is present and L1/L2 keepalives cross the link — so the
  same-host NETMAP data path works. (A clean end-to-end `ping` via hand-driven
  console was flaky because **IOS-XE 17.18 PnP zero-touch** flaps the interface
  `administratively down` on an unconfigured box — a console-driving artifact, not
  a wiring issue. The product injects an NVRAM startup-config so nodes boot
  configured and PnP never engages.)

## Architecture corrections P0 forces on the supervisor

1. **Console = pty→telnet bridge.** `internal/node/spawn_linux.go` currently pipes
   IOL to the supervisor's stdout and assumes a bogus `IOL_CONSOLE_PORT` env.
   Rework: allocate a pty, run IOL with it as controlling terminal
   (`setsid`+`ctty`), and bridge pty↔the node's telnet `ConsolePort`. Proven with
   `socat PTY,link=… EXEC:…,pty,setsid,ctty`.
2. **Same-host links use native NETMAP, not UDP.** `internal/relay` binds UDP and
   assumes IOL speaks UDP — real IOL speaks unix-socket netio. Rework: for
   same-host p2p/segment links, generate the NETMAP so IOL devices connect directly
   via netio (no relay). Keep the UDP relay/pcapng tee for the **capture** and
   **cross-host** cases, fronted by an `iouyap`-style netio↔UDP bridge inserted
   only when capture/hub is requested.
3. **Config via NVRAM injection, not console-driving.** Nodes boot with their
   `startupConfig` written to NVRAM (`internal/nvram`), so they come up configured
   and IOS-XE PnP never interferes.

## FULL END-TO-END through the real supervisor — 2026-07-02 ✅

Ran the actual supervisor on the runtime VM (`drive-supervisor.py`: `hello →
image.register → lab.load → lab.start → status`) with a 2×IOL lab whose
startup-configs (IP + `no shutdown` on Et0/0) were injected into NVRAM. Result:

```
image.register: id=b858503827356c55 class=l3 arch=x86_64
lab.start: node 1 console 9000 running; node 2 console 9001 running
# connected to R1's console via the supervisor's pty->telnet bridge:
R1> enable
R1# show ip interface brief | include Ethernet0/0
Ethernet0/0   10.0.0.1   YES NVRAM   up   up
R1# ping 10.0.0.2 repeat 5
.!!!!  Success rate is 80 percent (4/5), round-trip min/avg/max = 1/1/1 ms
```

Everything validated at once through the real orchestrator:
- ✅ image sniff (class l3, arch x86_64)
- ✅ pty→telnet console bridge (interactive `R1>` prompt over TCP)
- ✅ NVRAM injection (booted as hostname `R1`, IP `10.0.0.1` shown as `NVRAM` source,
  IOS-XE PnP never engaged — the boot-configured approach works)
- ✅ native NETMAP wiring (Et0/0 `up/up`)
- ✅ **ping succeeds** (80% = 4/5; the single drop is the normal first-packet ARP miss)

**One bug found + fixed:** IOL rejects **instance id 0** (valid range 1–1024). The
supervisor used lab `node.id` directly as the IOL instance id, so a node with id 0
exited immediately. Fix: map node id → IOL instance id (`nodeID+1`, guarded ≤1024)
consistently across argv + NETMAP + nvram filename. Validated with ids 1,2;
**re-validated after the fix with node ids 0,1 (the failing case): node 0 boots
(IOL instance 1), R1# console over the pty bridge, Et0/0 10.0.0.1 NVRAM up/up, and
`ping 10.0.0.2` = `!!!!!` Success rate 100 percent (5/5).**

Remaining P0 items (not risk — plumbing): VPCS↔IOL and Wireshark capture via the
`internal/iouyap` bridge (built, not yet wired into the server link path).

**IOL netio socket convention (confirmed via lsof on real IOL):** each IOL binds a
unix **DGRAM** socket at **`/tmp/netio<uid>/<instance-id>`** (e.g. instance 1 →
`/tmp/netio1000/1`, instance 2 → `/tmp/netio1000/2`; `<uid>` = numeric user id).
IOL derives a NETMAP peer's socket path from the peer's instance id and sends
datagrams there (8-byte header carrying the port channel). So to bridge/capture a
link, `iouyap` binds `/tmp/netio<uid>/<pseudo-instance>` and the bridged endpoint's
NETMAP entry points at that pseudo-instance; iouyap relays netio↔UDP into the
supervisor's relay (pcapng tee + forward). This is the exact seam `internal/iouyap`
was built for.

## Capture + bridged-link status — 2026-07-02

Ran a 2×IOL lab with **capture enabled** on the link (bridged via iouyap+relay+tee)
and pinged 20× to generate traffic; `tshark` analyzed the captured pcapng stream.

- ✅ **Wireshark capture PRODUCES VALID CAPTURES.** The pcapng stream is valid
  (SHB magic `0a0d0d0a`) and `tshark` decodes **clean Ethernet frames** — R1's
  `ARP Who has 10.0.0.2? Tell 10.0.0.1` and both routers' LOOP keepalives. The tee →
  relay → pcapng → `StripIOLHeader` chain works; a real `capture-helper` → Wireshark
  would show live link traffic.
- ⚠️ **Bridging a link currently breaks its L2 connectivity** (`ping 0/20`). R1's
  frames reach the relay/tee (captured) but the peer IOL never accepts them, so no
  ARP reply. Root cause = **IOL netio header addressing**: the 8-byte header encodes
  a dst port-channel keyed to the socket it was sent to (the pseudo-instance); when
  the frame is relayed to the peer's real netio socket the header still addresses
  the pseudo-instance, so the receiving IOL drops it. `internal/iouyap` forwards the
  header verbatim — it must **rewrite the src/dst channel fields per hop** so each
  IOL sees frames addressed to its own interface (same requirement VPCS↔IOL needs:
  strip header toward VPCS, add a correctly-addressed header toward IOL).

**VPCS spawn model validated (2026-07-02):** VPCS 0.8.3 runs via the supervisor's
forking-daemon path (`-p ConsolePort -i N -s/-c/-t`, no `-N`); its `VPCS>` console
is reachable through the supervisor-assigned port; process-group + /proc-owner kill
cleans it up. VPCS↔IOL ping still `not reachable` — SAME netio-header root cause as
capture (below), not a VPCS-spawn issue.

**Remaining deep piece (the one real unknown left):** reverse-engineer IOL's exact
8-byte netio header semantics (src/dst instance + port fields) by decoding real
native-IOL netio datagrams, then implement per-hop header rewriting in
`internal/iouyap`. Until then: native same-host IOL↔IOL links work perfectly (ping
100%), capture yields valid pcaps but interrupts the captured link, and VPCS↔IOL
needs the same header work. This is focused networking R&D, not architecture risk.

## netio header layout — CONFIRMED (2026-07-02)

Resolved the "remaining deep piece" above. Method: a Python MITM
(`/opt/iolbox/hdr-mitm.py` + `hdr-test.sh` on the runtime VM) bound two
pseudo-instance sockets (`/tmp/netio1000/501`, `/tmp/netio1000/502`) with
`NETMAP = "1:0/1 501:0/0" / "2:1/0 502:0/0"` — deliberately asymmetric
interfaces (Et0/1 vs Et1/0) so the port-byte nibble order is unambiguous —
then hexdumped and selectively rewrote every datagram between two real IOL
17.18.02 instances.

**Observed emission** (instance 1, Ethernet0/1, NETMAP peer 501:0/0):

```
01 f5 00 01 00 10 01 00   + ethernet frame
```

Confirmed layout, big-endian:

| Offset | Field    | Size | Confirmed value/meaning                             |
|-------:|----------|-----:|-----------------------------------------------------|
| 0      | dst_id   | 2    | destination instance id (`0x01f5` = 501)            |
| 2      | src_id   | 2    | source instance id (`0x0001` = 1)                   |
| 4      | dst_port | 1    | dest interface, **`port<<4 \| adapter`** (0/0 = 0)  |
| 5      | src_port | 1    | src interface (Et0/1 = `0x10`, Et1/0 = `0x01`)      |
| 6      | msg_type | 1    | **`1` = data frame** (every observed frame)         |
| 7      | channel  | 1    | `0` on every observed frame                         |

Two corrections vs the classic-iouyap assumption previously coded in
`internal/iouyap/header.go`: the port byte is `port<<4|adapter` (the REVERSE
of `netmap.Iface.Index()`'s `adapter*16+port`), and data msg_type is **1**,
not 0.

**Acceptance:** forwarding verbatim → receiving IOL drops the frame (the dst
fields still name the pseudo-instance; confirmed by the earlier capture test —
frames reach the tee but the peer never replies). A rewriting MITM delivered
dst-corrected frames both ways; end-to-end ping through the rewrite is
validated via the supervisor path (run-capture.sh / run-vpcs.sh), not the MITM
harness — the MITM run's expect-driven console config proved flaky (PnP DHCP
kept the test interfaces unconfigured), while the supervisor path uses NVRAM
injection and avoids console-driving entirely.

**RE-VALIDATED on the VM, 2026-07-02 — both bridged-link cases PASS:**

- ✅ **Capture-preserving link** (`run-capture.sh`): ping through the
  capture-enabled IOL↔IOL link = **95% (19/20)** (was 0/20), and the pcapng
  now shows the full conversation — ARP who-has → ARP reply → 19 ICMP echo
  request/reply pairs (38 ICMP frames), tshark-clean. Capture no longer
  breaks the link it captures.
- ✅ **VPCS↔IOL** (`run-vpcs.sh`): VPCS PC1 10.0.0.10/24 ping IOL R1 10.0.0.1
  = **4/5 replies ~0.5 ms** (seq=1 ARP-miss timeout, normal; was "not
  reachable"). No orphan vpcs/supervisor/IOL after teardown.

**Second bug found during re-validation (fixed same day):** the supervisor
never started the UDP relay for bridged links at `lab.start` — only
`capture.start`/`link.add` started relays, so a VPCS↔IOL link declared in the
lab doc had both edges (iouyap 10001→10000, VPCS 10003→10002) pumping into
unbound relay ports (traced with tcpdump on lo: frames in, zero forwards, no
listener on the relay's LocalPorts). That's why capture "worked" while plain
VPCS links didn't. Fix: `prepareLabDir` now calls `startLinkRelays` (one relay
per bridged link from the same plan) right after `startBridges`, before any
node spawns.

**VPCS console gotchas (for any driver/harness):** the syntax is
`ip <addr>/<mask> <gateway>` (`ip 10.0.0.10/24 10.0.0.1` — addr-then-gw-then-
masklen silently no-ops), the command runs a duplicate-address probe and takes
up to ~5 s before confirming with `VPCS : <addr> <mask> gateway <gw>` (wait for
that line, don't just wait for the prompt), and the first line sent after
connecting to the console is sometimes swallowed (send a bare CR first, retry
the command if the confirmation never appears).

**Fix implemented in `internal/iouyap`:** the UDP mesh now carries **raw
ethernet frames** (VPCS's native convention — no header on UDP at all).
`stripToUDP` drops the 8-byte header on netio→UDP; `wrapToNetio` constructs a
fresh header on UDP→netio addressed `dst = (LocalInstance,
LocalAdapter/LocalPort)`, `src = (PseudoInstance, 0/0)` — exactly the peer the
served IOL's NETMAP line names. `iouyap.Config` gained those addressing fields
(populated by `server.buildBridgePlan` from the lab doc), and the relay's
pcapng tee no longer strips anything (`relay.StripIOLHeader` deleted). This
one change fixes capture-preserving-links AND VPCS↔IOL, since both were
dropping on the same mis-addressed header.

## P0 status: core risks RETIRED ✅

Every hard unknown is now confirmed against real IOL 17.18.02: image sniff, iourc
keygen accepted, console mechanism (pty), and NETMAP wiring carries traffic. What
remains is **known-good engineering** (the three reworks above + VPCS wiring +
Wireshark tee via iouyap), not risk. Next: implement the `node`/`relay` rework and
run it through the actual supervisor with NVRAM-injected configs.

## Exit

P0 passes when steps 1–9 pass on VMware and step 10 passes on WSL2. Then proceed to
P1 (wire the GUI's real `TcpTransport` to the supervisor, replacing the mock).
