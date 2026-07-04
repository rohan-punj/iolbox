# Bridge-fabric data-plane migration — design

Status: **PROPOSED** (design only — not yet implemented). Author handoff:
2026-07-04. Companion kickoff: `J:\Claude code\iolbox-bridge-fabric-kickoff-prompt.md`.

This document proposes replacing iolbox's userspace UDP-tunnel link fabric with an
EVE-NG / PNetLab-style **Linux-bridge fabric with static per-interface taps**, and
folds in a **version-stamping** workstream so every deployment reports its build.

---

## 1. Why reopen the "no bridges" decision

`docs/architecture.md` deliberately chose a *bridgeless, tapless* data plane: IOL
interfaces are UDP socket pairs, frames flow directly node↔node over UDP, and there
is "no relay process in the steady state." That choice bought a clean host-side
capture tee and a simple remote/native story. But it has a structural flaw we hit
in production:

**IOL reads its `NETMAP` exactly once, at process start.** The current design makes
each interface's wiring *depend on the link topology* — an interface is only wired
to a relay/pseudo-instance if a link for it exists in the plan. So:

- Draw a link to an **already-running** IOL node → the plan rebuilds and the NETMAP
  on disk updates, but the running IOL never re-reads it → the new interface never
  connects → **no DHCP, no traffic** until that node is restarted.
- Root-caused 2026-07-04 on the builder with real IOL 17.18.02: a **clean**
  `lab.load` + `lab.start` of the exact mixed topology (R1 `e0/0` native to SW1 +
  `e0/1` bridged to NAT, `startupConfig: ip address dhcp`) gets DHCP fine
  (`172.31.1.100`). VPCS↔NAT also works. Only the **incremental GUI workflow**
  (link added after the node is running) fails. The supervisor even comments
  "the IOL side still needs a node restart to re-read its NETMAP" (`handlers.go`).

Worse, coping with dynamic re-plumbing spawned a whole apparatus of fragile
machinery — the UDP relay's per-link port-pair juggling, `resyncExtnetPorts`,
`Rebind`, sticky `linkAssign`, ephemeral-port handling — ~44 touchpoints that exist
*only* because ports get re-plumbed as the topology changes. This is the same code
behind past "R1 stops getting DHCP after edits" and the `d1b8dac` rebind saga.

## 2. The core insight (not "add a bridge")

The fix is not fundamentally "use a bridge." It is: **make each node's per-interface
wiring topology-INDEPENDENT so a running node never needs re-plumbing.**

- Every node interface maps to **its own fixed tap at boot**, present whether or not
  a link exists for it. The `NETMAP` becomes **static** — computed from the node's
  adapter count alone, never from the link set.
- "Drawing a link" becomes "attach these two taps to a Linux bridge" — a pure
  **runtime** operation (`ip link set <tap> master br-<linkid>`) that touches no
  running node. Removing/reshaping a link is `nomaster` / re-master.

The Linux bridge is just the hot-pluggable L2 mechanism. This is exactly how
EVE-NG/PNetLab hot-connect nodes with zero restarts, and it is battle-proven at
scale. It also makes the entire dynamic-port-plumbing bug class **structurally
impossible**.

## 3. Target architecture

```
IOL instance ──netio(unix)──> iouyap (netio↔tap) ──> tapX ──┐
                                                            ├─ br-<linkid> (Linux bridge)
peer node interface ─────────────────────────────> tapY ──┘

NAT node:  veth/tap on br-<linkid> @ 172.31.n.1 + dnsmasq (DHCP) + iptables MASQUERADE
VPCS:      vpcs(UDP) <──> udp↔tap shim <──> tapZ ──> br-<linkid>
Capture:   tcpdump -i br-<linkid> -w - | pcapng TCP stream (existing capture port model)
```

Every link is a bridge; every node interface is a tap. Uniform, inspectable,
hot-pluggable.

## 4. Component-by-component migration

| Component | Today | After |
|---|---|---|
| `netmap` | per-interface mapping derived from the link plan | **static** per-interface tap mapping from adapter count only |
| `iouyap` | netio ↔ **UDP** | netio ↔ **tap** (iouyap already supports tap mode — EVE-NG uses it) |
| bridge mgr | — | **NEW**: create/destroy `br-<linkid>`, attach/detach taps (~200-400 lines + privileged cmds) |
| `relay` | UDP port-pair forwarding + capture tee | **retired** for IOL/VPCS/NAT links |
| `extnet`/NAT | userspace DHCP on the relay + MASQUERADE | bridge member (veth @ .1) + **dnsmasq** + MASQUERADE |
| VPCS | UDP-native into relay | **udp↔tap shim** per VPCS interface (VPCS stays UDP; shim bridges to a tap) |
| capture | tee UDP relay → pcapng TCP | `tcpdump -i br-<linkid>` → pcapng TCP (same host port model) |
| link.add/remove | rebuildBridgePlan + relay start + rebind + sticky assigns | bridge attach/detach (no re-plumb, no rebind, no sticky) |

**Retired outright**: `resyncExtnetPorts`, `Rebind`, `linkAssign` stickiness,
ephemeral-port handling, most of the relay's port juggling.

## 5. Wrinkles, risks, and open decisions

1. **VPCS is UDP-native.** GNS3 vpcs speaks UDP tunnels, not taps, so it needs a
   small UDP↔tap shim per interface. It does not fall out of the pure-tap model for
   free. (Decision: shim vs. a different PC node type.)
2. **NAT rework.** Today's clever userspace DHCP (no dnsmasq) becomes a standard
   dnsmasq on a bridge veth. Cleaner, but a new dependency + process to supervise.
   (Decision: dnsmasq vs. keep the userspace DHCP server but bind it to a bridge tap.)
3. **Native IOL↔IOL perf.** Today uses IOU netio unix sockets (very fast). Bridge
   path is tap→bridge→tap (kernel, more copies). EVE-NG proves it scales, but it is a
   real change. (Decision: all-bridge for uniformity+capture, or keep native netio as
   a fast path for *captureless* IOL↔IOL and only bridge when needed?)
4. **Unconnected interfaces** get a pre-created tap (bounded by adapter count, not
   16). More kernel objects per lab; manageable.
5. **Constrained envs (LXC/Docker).** Taps+bridges in the container netns need
   `NET_ADMIN` + `/dev/net/tun` (both already required by the NAT node). The fabric
   widens the privileged surface; **must revalidate on the unprivileged Proxmox CT
   and Docker** where the userspace-relay model was more container-friendly.
6. **Per-link bridge vs. shared segment bridge.** A `p2p` link = a 2-port bridge; a
   `segment`/hub link = an N-port bridge. Straightforward, but decide naming/lifecycle.
7. **Capture transport changes** from relay-tee to tcpdump-on-bridge. Net win (every
   link becomes capturable, including native IOL↔IOL which today can't be), but the
   `internal/relay` capture code + `captureTransport` GUI path need rework.

## 6. Phased plan (each phase independently testable on the builder)

- **Phase 0 — spike (de-risk the core claim).** On the builder with real IOL: two
  IOL instances, each interface on its own tap via iouyap tap-mode, taps NOT bridged
  at boot; then `ip link ... master br0` at runtime and prove L2 passes **without
  restarting either IOL**. This single experiment validates or kills the whole plan.
- **Phase 1 — static netmap + iouyap tap-mode + bridge manager** for IOL↔IOL.
- **Phase 2 — NAT on bridge** (dnsmasq + veth + MASQUERADE); prove IOL→NAT DHCP
  **hot-connects** (link drawn after IOL running → gets a lease, no restart).
- **Phase 3 — VPCS udp↔tap shim.**
- **Phase 4 — capture on bridge** (tcpdump → pcapng TCP); rewire GUI capture.
- **Phase 5 — retire** relay/rebind/sticky-assign; simplify link.add/remove/lab.start.
- **Phase 6 — per-target validation** (see matrix).

## 7. Validation matrix (Phase 6)

Every cell must pass **hot-connect (link added to a running node → works, no
restart)** plus the listed check:

| | VMware | OVA | native | QEMU-TCG | WSL2 | LXC (unpriv) | Docker |
|---|---|---|---|---|---|---|---|
| IOL↔IOL L2 | ping | ping | ping | ping | ping | ping | ping |
| IOL↔NAT DHCP | lease | lease | lease | lease | lease | **lease (priv check)** | **lease (priv check)** |
| VPCS↔NAT DHCP | lease | … | … | … | … | … | … |
| capture a link | pcapng | … | … | … | … | … | … |
| native telnet / host Wireshark | ok | … | … | … | … | … | … |

Builder repro harness (proven this session): drive raw NDJSON on `127.0.0.1:4000`
(skip pushed events by matching `id` + presence of `ok`); telnet the console ports
for `show ip int brief`; real IOL runs on the builder (`chmod +x` the image + iourc
via `supervisor -gen-iourc`). **Gotcha:** a stale supervisor's process comm is
`supervisor` (not `-linux-amd64`), so `pkill -x supervisor-linux-amd64` misses it —
use `fuser -k 4000/tcp` / `pkill -f`.

## 8. Persistence workstream — version stamping (do alongside, independent)

Today `hello` reports a hardcoded `supervisor: "0.1.0"`, so **you cannot tell what
build a deployment runs** — which is exactly why "is localhost old?" was
unanswerable without git archaeology. Independent of the fabric work:

- Bake the git version into the binary: `-ldflags "-X main.version=$(git describe
  --tags --always --dirty)"` in `build-release.sh`; replace the hardcoded const in
  `cmd/supervisor/main.go` and the `"0.1.0"` fallback in `server.go`.
- It already flows through `hello.supervisor`; surface it in the GUI (the Palette
  host-monitor footer is the natural home).
- Add a one-command **rebuild-all-targets** wrapper and per-target **redeploy** notes
  (LXC: `pct push` the binary + restart; QEMU: rebuild disk + relaunch; etc.).

Result: every deployment shows its build at a glance, and "does this deployment have
fix X?" becomes a lookup, not an investigation. This does **not** auto-propagate
fixes — existing deployments still need rebuild+redeploy — but it makes staleness
*visible*, which is the actual pain.

## 9. Recommendation summary

The fabric migration is the correct long-term destination — PNetLab/EVE-NG parity,
zero-restart hot-connect forever, and it lets us **delete** the rebind/sticky-assign
machinery that keeps generating these DHCP bugs. Cost is dominated not by lines
(it's a wash — retire fragile dynamic plumbing, add a smaller static fabric) but by
being the biggest data-plane rework in the project, touching every node kind and all
seven deployment targets. **Gate the whole thing on the Phase 0 spike.** Ship the
cheap restart-on-link-change fix in the current architecture first to unblock users.
