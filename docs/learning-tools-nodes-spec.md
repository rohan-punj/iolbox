# Learning-tool nodes — implementation spec

Status: **PROPOSED** (design only — no code changes). Author handoff: 2026-08-08.
Revised 2026-08-08 after adversarial review (`docs/learning-tools-nodes-spec-review.md`,
`codex sol-medium`) and an owner scope ruling. All file/line citations below were
re-verified against the current tree.

This document specifies a new node **kind** for iolbox — a *learning tool* node —
that brings PNetLab-style "learning tool" containers (the reference being
**pnet-secbench**, `J:\Claude code\pnet-lab-nodes\nodes\secbench\`) into the
appliance **natively**, as lightweight bundled nodes, without dragging a Docker
engine into the runtime. It is grounded in the current bridge-fabric data plane
(`supervisor/internal/fabric`, `supervisor/internal/extnet`) and the existing
node lifecycle (`supervisor/internal/server/handlers.go`, `internal/node`).

The one-line thesis: **a learning-tool node is a supervisor-managed process tree
running inside a dedicated Linux network namespace, wired to a lab link by a veth
into `iolbr<linkid>` — the same bridge fabric IOL/VPCS/NAT already use, under the
same real device name the fabric actually issues.** The netns is the *network*
isolation boundary; it makes "lock every attack to the lab NIC" structural rather
than argparse hygiene. It is **not** a full container: process, mount, PID, and
user isolation are deliberately out of scope for v1 (see §0.2).

---

## 0. Scope ruling and what this revision changes

### 0.1 Owner ruling: **"netns is fine."**

The review's Critical #2 argued that a network namespace plus two capabilities is
not a container replacement and pushed to make user/mount/PID namespaces + seccomp
+ an LSM profile *prerequisites* for any tool pack. The project owner has
**explicitly rejected** expanding v1 that far. For this personal, single-user lab
appliance the accepted isolation boundary is:

> a per-node **network namespace** + a **veth** into the bridge fabric, with
> **narrowly-scoped ambient capabilities**, plus a **cgroup v2** resource cage.

That boundary is what §2 and §5 specify. It is honest about what it does and does
not contain (§2.5). We do not re-litigate the container-parity question here.

### 0.2 Deliberately deferred (so a future reader knows it wasn't missed)

Fuller sandboxing was **considered and deferred**, not overlooked:

- **Mount namespace (`CLONE_NEWNS`)** + a minimal executable root — deferred. The
  tool shares the appliance rootfs; a malicious/edited pack script can read other
  files. Mitigated operationally by shipping only immutable, root-owned first-party
  packs (§2.6, §6).
- **PID namespace (`CLONE_NEWPID`)** — deferred. Consequence: the pack GUI is an
  ordinary host-PID child of the supervisor, **not** PID 1 of a namespace, which
  actually *simplifies* reaping in v1 (§5.3). If a PID namespace is ever added, a
  real init/reaper as PID 1 becomes mandatory — noted so the lifecycle design does
  not silently break under that change.
- **User namespace (`CLONE_NEWUSER`) / uid remapping** — deferred. The tool runs
  as a real unprivileged uid with ambient caps, not a mapped fake-root.
- **seccomp / AppArmor / SELinux profile** — deferred; no LSM confinement in v1.

These are the classes Docker's defaults cover that netns does not. The v1 stance:
**tool packs are privileged, host-adjacent first-party code**, treated as trusted,
not as a sandbox for untrusted plugins (§6.1). Any future third-party-pack story
must add the deferred layers first.

### 0.3 What this revision fixes versus the reviewed draft

- Corrected bridge naming to `iolbr<linkid>` and re-scoped `internal/fabric` vs
  `internal/extnet` ownership; corrected every overstated "already implemented"
  claim (§0.4).
- Redesigned GUI reachability to an **AF_UNIX** transport that needs no IP, no
  `CAP_NET_BIND_SERVICE`, and no bridge exposure (§3.1, was Critical #1).
- Rewrote the capability mechanism to a correct permitted/inheritable/ambient +
  securebits + bounding-set transition, granting **NET_ADMIN to setup only** and
  the long-lived GUI/scripts **NET_RAW only** (§2.5, was Critical #3).
- Enumerated **every** fabric switch/eligibility point a `tool` kind must touch
  (§4.2, was High #6).
- Made lifecycle/start-stop robustness concrete: supervision, reaping, cgroup
  create-before-start with guaranteed cleanup, stale-state recovery, and bounded
  graceful-stop-then-kill (§5, the main ask; was Critical #4 / High #7 / Medium #10).
- Fixed the build story to an offline pinned-hash wheelhouse and marked the size
  as an estimate pending measurement (§7, was High #14).
- Replaced copied-from-NAT feature detection with an operational probe + capability
  matrix (§4.3, was Medium #11).
- Acknowledged the closed node-kind union and the real frontend vertical slice;
  tightened the pack contract without adding signing infra (§3, §2.6; was #12/#13).
- Re-sequenced P0–P4 so the spike de-risks the *actual* risky assumptions and
  cgroup/reaper work is early, not deferred (§8).

### 0.4 Factual corrections against the current tree (re-verified)

| Draft claim | Reality (verified) |
|---|---|
| bridge is `br-<linkid>` | `fabric.BridgeName(linkID)` returns **`iolbr%d`** (`internal/fabric/commands.go:99-108`). Note `extnet.go`'s own doc comments still say the stale `br-<linkid>` — the code does not; do not trust those comments. |
| `internal/fabric` does netns/veth/iptables | `internal/fabric` **only** builds `ip` argv for taps + bridges + `ip link … master` (`commands.go:14-71`). No netns, no veth, no iptables. |
| iptables/NAT is a fabric primitive | iptables MASQUERADE/FORWARD + gateway addressing live in **`internal/extnet`** (`commands.go:83-99`, `endpoint_linux.go`), run as root/`sudo -n`, **not** as delegated child capabilities. |
| a second external-node kind exists | `lab.Kind` and `extnet.Kind` contain **NAT only** (`lab/lab.go:40-49`, `extnet/extnet.go:22-25`). "mgmt" does not exist. |
| NAT is a model for a supervised process tree | NAT is **process-less** — a tap fd + DHCP goroutine driven directly by the server (`extnet/endpoint_linux.go:21-24`). It is a good model for *device* preclean/attach, **not** for process supervision. |
| feature detection | `extnet.Detect` is only `/dev/net/tun` present **and** `sudo -n true` (`extnet/detect_linux.go:41-52`) — proves neither netns nor veth nor cgroup delegation. |
| rootfs already close | `BASE_INCLUDE` has **no** python/venv/libpcap/util-linux (`runtime/build-rootfs.sh:186`); the only chroot pip/venv step shown is the optional i386 install. |
| palette is a small addition | `Palette.svelte:9` drag type and `labTypes.ts:6` `NodeKind` are **closed unions** `"iol" \| "vpcs" \| "nat"`; canvas registers only IOL/VPCS/NAT. |

---

## 1. Problem framing

### 1.1 What a "learning tool" node is

pnet-secbench is representative of a whole class: a small node that is **not** a
router/switch/PC image but a **teaching instrument** wired onto a lab link. It is
built from exactly two ingredients:

- **A small web-GUI supervisor** (secbench `gui/`, a stdlib-ish Go server) that
  renders module cards and spawns/kills child helper processes on demand
  (`gui/runner.go` `Supervisor.Start/Stop`, one `Runner` per module, output
  captured into a 400-line ring buffer).
- **~28 short Python scapy scripts** (`attacks/*.py`), grouped into **18 L2/L3
  attack+recon modules** (ARP spoof, DHCP starve/rogue, STP root hijack, CDP/LLDP
  flood, VLAN hop, HSRP/VRRP hijack, rogue OSPF/EIGRP, IPv6 RA/DHCPv6) and a
  **10-module NGFW-test group** (`fw_*` — node→firewall test traffic; the code has
  10, two more than the README's stale "8-module" prose: it also carries `fw_ssl`
  and `fw_scan`), plus a deliberately vulnerable **Victim Mode** target (secbench
  `victim/`, a separate stdlib Go binary run as `nobody`) that exists only as the
  app-layer server the NGFW tests fire at. Each module is a declarative `ModuleDef`
  (`gui/modules.go:35`). **iolbox ports only the 18 L2/L3 modules — the 10-module
  NGFW group and Victim Mode are dropped as out of product scope; see the §4.1
  ruling.**

The defining property: it is **"fundamentally Python scripts with a small web
GUI"** (user's words). No kernel, no ELF emulation, no per-node OS.

### 1.2 Why Docker-in-appliance is the wrong fit *for iolbox*

Docker buys secbench three things, and we replace each with a kernel primitive we
already operate — but with **honest** scope (see §2.5 for what we do *not* get):

| What Docker gives secbench | iolbox replacement (and its true scope) |
|---|---|
| **Interface isolation** — sees only `eth0`(mgmt)+`eth1`(lab). | A per-node **network namespace** containing only the lab veth + `lo`. *Strictly better for network reach.* Does not isolate FS/PID/IPC/users. |
| **Dependency packaging** — scapy venv baked into an image layer. | One venv baked into the appliance rootfs, shared read-only. Smaller, no registry. |
| **Capability scoping** — `--cap-add=NET_ADMIN,NET_RAW`. | Ambient **NET_RAW** on the child; **NET_ADMIN** only on the supervisor's setup path (§2.5). Narrower than the container's default cap set. |
| **cgroup limits** — `cpu: 2, ram: 2048` in the template. | A **cgroup v2** cage created before launch (§5.4). |
| **seccomp/AppArmor defaults** | **Not replaced** — no LSM/seccomp in v1 (§0.2). |

Docker's costs are exactly what iolbox exists to avoid (`PLAN.md:8`,
`docs/architecture.md:64`, `PRODUCT.md:27`): a root daemon, an image store + pull
path, a second networking model beside the bridge fabric, and hundred-MB layers in
a "single static Go binary" appliance.

The honest counter-argument, and its bound: hand-rolled netns/veth/cap/cgroup
setup is code we own and must get right. The mitigating fact is that iolbox
**already issues privileged `ip` for taps/bridges** (`internal/fabric`) and **runs
a privileged tap+DHCP+iptables data plane** (`internal/extnet`). A tool node adds
netns + veth + cgroup + a *supervised process tree* to that already-privileged
surface. The process tree is genuinely new (NAT is process-less) — §5 treats it as
a first-class subsystem, not a small extension of the NAT endpoint.

### 1.3 Constraints the design must honor

1. **Single static Go binary + embedded assets.** No new long-lived daemon. The
   supervisor stays the one process; tool children are spawned/reaped by it.
2. **Bridge-fabric native + capturable.** The tool's lab traffic transits
   `iolbr<linkid>`, so the existing `tcpdump -i iolbr<linkid>` capture records it
   with zero new capture code.
3. **Hot-connect.** Drawing/removing a link attaches/detaches at runtime with no
   node restart, as `attachFabricLink` does for IOL/VPCS/NAT (`fabric_linux.go:283`).
4. **Feature-gated + degrade cleanly.** Runtimes that cannot host tool nodes must
   advertise the absence in `hello.features` and reject `node.start` with
   `unsupported` — the pattern `natgw` uses today, but gated on an **operational
   probe** (§4.3), not package presence.
5. **Trust boundary honesty.** These are *attack tools* and privileged first-party
   code (§6).

---

## 2. Engine / backend design

### 2.1 Lab-doc schema: a `tool` node kind

Add a fourth `Kind` alongside `iol`/`vpcs`/`nat` (`lab/lab.go:40-49`) and extend
the frontend `NodeKind` union (`labTypes.ts:6`, currently closed — see §3):

```go
const KindTool Kind = "tool"   // a bundled learning-tool node (e.g. secbench)
```

A tool node reuses the existing `Node` struct with **no new top-level fields** —
tool-specific data rides in the reserved `Config map[string]json.RawMessage`
(`lab.go:70`):

```jsonc
{
  "id": 7, "kind": "tool", "name": "SecBench",
  "x": 240, "y": 96, "icon": "Firewall-2D-Generic-S.svg",
  "config": { "pack": "secbench" }
}
```

Like `nat`, a tool node has **exactly one connectable interface** named `eth1`
(§2.5) and is referenced by **at most one** link endpoint. Validation
(`internal/lab/validate.go`) gains: `kind==tool` requires `config.pack` to be a
known installed pack id; `eth1` is its only legal interface; multi-homing is
rejected. The supervisor validates `config` against the **pack manifest** (§2.6),
not against `lab.schema.json`, so the lab file stays forward-compatible.

### 2.2 Where it plugs into the lifecycle

The tool node is a **third node model** beside "spawned pty process" (IOL,
`internal/node`) and "process-less tap endpoint" (NAT, `internal/extnet`). It is a
*supervised process tree inside a netns + cgroup*. It slots into `startNodes`
(`handlers.go:529`) as a new branch, peer to `KindNAT` (`:540`) and the `KindVPCS`
setup (`:552`):

```go
if docNode.Kind == lab.KindTool {
    started, err := s.startToolNode(ll, docNode, nr)   // new; see §5
    ...
    continue
}
```

`nodeRuntime` (`internal/server/loaded.go`) gains a `tool *tool.Endpoint` field
beside `extnet` and `vtap`. A new package `internal/tool` owns the netns/veth/
cgroup/process machinery. Its surface (shape mirrors `internal/extnet`, but it
supervises a **process tree**, which extnet does not):

- `tool.Detect() tool.Capabilities` — operational probe (§4.3), called once at
  startup beside `extnet.Detect` (`server.go:122`).
- `tool.Capabilities.GateFeatures()` → advertises `"tools"` when supported.
- `tool.ReapStale()` — startup sweep of leaked netns/veth/cgroups/processes from a
  prior instance (§5.5). New: extnet only precleans its own named device lazily at
  Start; tool nodes leak *processes*, so this is promoted to an explicit sweep.
- `tool.Start(cfg) (*tool.Endpoint, error)` — precleans this node's stale objects,
  creates the cgroup **first**, then the netns + veth + AF_UNIX dir, then launches
  the pack GUI **into the cgroup** with the capability transition (§2.5, §5).
- `Endpoint.AttachBridge(br)` / `DetachBridge()` — moves the veth's bridge-side end
  onto/off `iolbr<linkid>`; the hot-connect seam (§4).
- `Endpoint.Stop()` — bounded graceful-stop-then-kill of the whole cgroup, then
  deletes veth + netns + cgroup (§5.6).
- `Endpoint.State()` / an exit-watcher goroutine — accurate `running →
  stopped/crashed` reporting (§5.2).

The node state machine (`internal/node/state.go`) is reused unchanged.

### 2.3 The network model — netns + veth into the link bridge

```
                       tool node's network namespace (netns iolt<id>)
 iolbr<linkid> ─veth─ vtool<id>  │  eth1 (peer veth end, MOVED into the netns)
  (Linux bridge,      (bridge     │   ├─ scapy helpers bind here (ambient NET_RAW)
   the lab link)       side, in   │   └─ the ONLY non-lo interface in the netns
                       root netns) │  lo
```

Setup — supervisor runs these as root (like extnet's privileged path), **not** the
child:

1. `ip netns add iolt<id>`.
2. `ip link add vtool<id> type veth peer name eth1`.
3. `ip link set eth1 netns iolt<id>`; `ip netns exec iolt<id> ip link set eth1 up`;
   `ip link set vtool<id> up`. (Optionally `ip netns exec iolt<id> ip link set lo up`.)
4. On link draw: `ip link set vtool<id> master iolbr<linkid>` (`AttachBridge`).

Why this shape:

- **Capture for free** — frames transit `iolbr<linkid>`; existing tcpdump records them.
- **Hot-connect for free** — attach/detach is a pure `ip link … master/nomaster`
  op that never restarts the tool child.
- **Network isolation stronger than the container had** — the only non-loopback
  interface in `iolt<id>` is `eth1`; there is no `eth0`, no `docker0`, no host route.
  The child physically cannot send anywhere but the lab link.

The **bridge-side end `vtool<id>` stays in the root netns**, so the always-on
directional classifier (`dirstat.Open`, `fabric_linux.go:346`) and bridge capture
can bind it exactly as they bind an IOL/VPCS/NAT tap.

### 2.4 Bundling Python + dependencies WITHOUT Docker

**Decision: one vendored venv baked into the appliance rootfs at build time,
shared read-only by every tool node.** See §7 for the corrected build recipe
(offline pinned-hash wheelhouse, not a runtime `pip install`).

- venv at `/opt/iolbox/tools/venv`; packs under `/opt/iolbox/tools/packs/<packid>/`
  (scripts, the pack's GUI binary, the manifest). All **root-owned, mode 0755/0644,
  immutable in the shipped image** (§6.1).

### 2.5 Privilege model — the most important security question

secbench grants `CAP_NET_RAW + CAP_NET_ADMIN` at the container and spends enormous
effort locking every helper to `eth1` (three independent mechanisms:
`runner.go:205` `stripIfaceFlag`, `common.py:45` `enforce_lab_iface`, `common.py:170`
`SO_BINDTODEVICE(eth1)`) — *only because the container has more than one interface.*

**The netns collapses that.** In `iolt<id>` there is exactly one non-loopback
interface, and we name it `eth1`, so `enforce_lab_iface("eth1")` holds by
construction. We keep the three locks as belt-and-suspenders but they are no longer
load-bearing.

#### 2.5.1 Which cap goes where (fixes Critical #3's over-grant)

- **NET_ADMIN is a *setup* capability, granted to no long-lived tool process.**
  Bringing `eth1` up, addressing, routes, promisc, qdisc — all of that is done by
  the **supervisor as root** at netns setup, or by a tiny short-lived helper the
  supervisor invokes for a specific op. The pack GUI and the scapy children **never**
  receive NET_ADMIN.
- **NET_RAW is the only ambient capability the long-lived tree gets.** scapy's raw
  L2 send/recv (the actual attack surface) needs `CAP_NET_RAW`. It is granted
  ambiently to the pack GUI so the scapy children it execs inherit it.
- The few modules that genuinely need a NET_ADMIN op (e.g. adding a route for a
  routing-protocol spoof) call a **narrow supervisor-mediated verb** that performs
  that one op in the netns as root, rather than the module holding NET_ADMIN.

This directly implements the review's fix: NET_ADMIN only where proven necessary,
never on the long-lived GUI.

#### 2.5.2 The capability transition, done correctly

`setpriv --ambient-caps` alone is **insufficient** — ambient caps require the cap
be in *both* permitted and inheritable, and a formerly-root process can regain caps
across `execve` unless securebits and the bounding set are handled. The correct
ordered transition (supervisor is root; child target uid is a dedicated
unprivileged `ioltool` account created at rootfs build):

1. `prctl(PR_SET_NO_NEW_PRIVS, 1)` — bars privilege regain via setuid binaries/file
   caps on any later exec.
2. Drop the **bounding set** to exactly `{CAP_NET_RAW}` (`PR_CAPBSET_DROP` for every
   other cap) — a cap absent from the bounding set can never be reacquired.
3. Set **securebits** `SECBIT_KEEP_CAPS | SECBIT_NOROOT` (and their locks) so the
   upcoming setuid does not clear caps and root's uid-0 special-casing is off.
4. `capset()`: permitted = inheritable = `{CAP_NET_RAW}`.
5. `setgroups([])`, `setgid(ioltool)`, `setuid(ioltool)`.
6. Raise `CAP_NET_RAW` into the **ambient** set (`PR_CAP_AMBIENT_RAISE`) — valid
   because step 4 put it in permitted∩inheritable; ambient survives `execve`.
7. `execve` the pack GUI (already inside the netns and cgroup).

**Implementation:** `util-linux` `setpriv` performs this whole sequence
declaratively and in the right order — this is why §7 adds it to the rootfs:

```
setpriv --reuid ioltool --regid ioltool --clear-groups --no-new-privs \
        --bounding-set -all,+cap_net_raw \
        --inh-caps    -all,+cap_net_raw \
        --ambient-caps -all,+cap_net_raw \
        <pack-gui> ...
```

Go's `SysProcAttr.AmbientCaps` is an alternative but does **not** drop the bounding
set or set securebits, so it must be paired with explicit prctl calls in a
pre-exec step — `setpriv` is preferred for correctness. The netns/cgroup entry is
handled separately (§5.4): the child is placed in the cgroup at spawn and run via
`ip netns exec iolt<id> setpriv … <pack-gui>` (or `setns` + `setpriv` natively).

**Mandatory tests (P0, §8):** `/proc/self/status` (`CapEff/CapPrm/CapInh/CapAmb/
CapBnd`), `capsh --print`, an attempt to regain a dropped cap after exec, an
attempt to enumerate host interfaces/routes from the netns, and confirmation the
child cannot perform a NET_ADMIN op.

#### 2.5.3 Honest scope of the boundary

- **Network:** fully isolated (netns) — the strong guarantee, and the one that
  matters most for an attack tool.
- **Capabilities:** ambient NET_RAW only; NET_ADMIN never on the long-lived tree.
- **Resources:** cgroup v2 cage (§5.4).
- **Filesystem / PID / users / syscalls:** **not** isolated in v1 (§0.2). A
  compromised/edited pack script runs as `ioltool` with NET_RAW and can read the
  shared rootfs (image library, other labs' configs) and see host PIDs. This is
  accepted only because packs are immutable, root-owned, first-party code on a
  single-user appliance (§6.1). It is the explicit price of the "netns is fine"
  ruling.

### 2.6 A generic tool-pack plugin contract (so it's not secbench-only)

A tool node's behavior is driven by a **manifest** the pack ships, a port of
secbench's `ModuleDef` (`gui/modules.go:35`) from a compiled Go var to a
declarative file read at `Detect` time:

`/opt/iolbox/tools/packs/<packid>/pack.json`:

```jsonc
{
  "manifestVersion": 1,
  "id": "secbench", "name": "Security Bench",
  "icon": "Firewall-2D-Generic-S.svg",
  "interpreter": "venv",
  "gui": { "bin": "secbench-gui", "transport": "unix", "console": "http" },
  "caps": ["NET_RAW"],                 // allowlisted; NET_ADMIN is NEVER grantable here
  "options": [],                       // secbench's only option was "victim", now dropped (§4.1)
  "groups": ["recon","spoof","dhcp","stp","vlan","fhrp"],
  "modules": [ { "key": "arp_spoof", "label": "ARP Spoof / MITM", "group": "spoof",
                 "script": "attacks/arp_spoof.py", "fields": [ { "name": "target", "type": "ipv4" } ],
                 "mitigation": { "text": "ip arp inspection vlan ..." } } /* … */ ]
}
```

Contract rules (tightened per review #13; **no signing infra in v1**):

- **Packs are immutable and root-owned**, enumerated **at build time**. v1 supports
  **first-party packs only, built into the release; there is no runtime pack
  installation** (§6.1). A signed-pack trust model is future work — flagged, not
  designed here.
- `caps` is validated against an allowlist that contains **`NET_RAW` only** — a
  manifest can never request `NET_ADMIN` or anything else. Unknown/oversized caps
  fail `Detect` for that pack.
- The GUI binary and every `script` path must resolve to an **absolute path inside
  the pack directory** after canonicalization (no `..`, no symlink escape).
- The child is launched with a **scrubbed environment** (explicit allowlist:
  `PATH`, `PYTHONHOME`/venv, the pack dir, the node's option file path) so one
  pack's Python cannot be steered to import another writable artifact.
- `manifestVersion` is required; the supervisor rejects unknown major versions.

Integration approach — **committed to "Level A": the pack ships its own GUI.**
secbench already *is* a web GUI supervisor; iolbox hosts it in the netns and
reverse-proxies its AF_UNIX socket (§3.1), rendering it in an iframe/panel. This is
minimal Python/Go work and reuses the exact htmx UI the pack already maintains.

> **Alternative considered and rejected — "Level B" (iolbox re-renders native
> Svelte cards from the manifest and drives `tool.exec`/`tool.tail`, retiring the
> per-pack Go GUI).** Rejected because for a **first-party-only** pack story (§6.1)
> it buys nothing Level A lacks while doubling the maintenance surface — two
> renderers (the pack's own GUI *and* iolbox's native cards) reading the same
> manifest, kept in sync forever. Hosting the pack's shipped GUI is strictly
> simpler and is the design of record. The `pack.json` manifest still carries the
> per-module `fields`/`mitigation` metadata (it drives secbench's own GUI), so a
> future native-card renderer remains *possible* without a schema change — but it
> is **not** a planned deliverable and is not spec'd as a parallel option.

---

## 3. GUI design

Adding a `tool` kind is **a real vertical slice of frontend work, not a small
addition** (review #12). The node-kind union is closed today: `NodeKind = "iol" |
"vpcs" | "nat"` (`labTypes.ts:6`), the palette drag type is the same literal union
(`Palette.svelte:9`), and canvas registration maps only IOL/VPCS/NAT (NAT reuses
`VpcsNode`). This slice touches: the `NodeKind` union + every exhaustive `switch`
over it, the drag payload, a **new** `ToolNode.svelte` (not a remap of an existing
node), `NodeEditDialog`, the console dock (which today accepts terminal/capture
tabs, not iframe/tool panels), a new `tool.listPacks` protocol verb with client
transport/types/state/error-handling, and the reverse-proxy client. Plan it as its
own slice with schema + client-store design + tests.

### 3.1 GUI reachability — AF_UNIX, no IP, no privileged port (fixes Critical #1)

The reviewed draft had the GUI listen on netns-local `:80` and assumed the
supervisor could dial it. It cannot: a process in `iolt<id>` is reachable only via
that netns's loopback or an addressed interface, and the supervisor lives in the
**root** netns; `:80` also needs `CAP_NET_BIND_SERVICE`, which the allowlist
excludes. Redesign:

**Primary transport: an AF_UNIX socket.** AF_UNIX sockets are objects in the
*filesystem/mount* namespace, **not** the network namespace. Because v1 does **not**
use a mount namespace (§0.2), a process inside `iolt<id>` and the root-ns supervisor
share the same filesystem view, so a socket path bound inside the netns is dialable
by the supervisor directly — with **no IP, no veth-for-management, no bridge
exposure, and no `CAP_NET_BIND_SERVICE`.**

- The supervisor pre-creates `/run/iolbox/tool/<id>/` (root-owned, `0700`, then the
  socket node itself owned by `ioltool` so the GUI can `bind`).
- The pack GUI binds `/run/iolbox/tool/<id>/gui.sock` — for a stdlib Go server this
  is a one-line change (`net.Listen("unix", path)` instead of `":80"`), declared in
  the manifest as `"transport": "unix"`. This is the only required change to
  secbench's GUI for hosting.
- A new wsbridge route `GET /tool/{nodeId}/…` **reverse-proxies HTTP/WS to that
  socket** (HTTP/WS proxy, not a raw byte pump like `/console`/`/capture`). The
  Svelte side renders it in a panel/iframe — secbench's existing htmx UI, unchanged.

**Fallback for a TCP-only pack GUI: a dedicated management veth /31.** If a pack
truly cannot bind AF_UNIX, the supervisor creates a **second** veth pair
`mtool<id>`↔`mgmt0`, keeps the root-ns end and moves `mgmt0` into `iolt<id>`, and
assigns a link-local `/31` (e.g. `169.254.<n>.0/31` root side, `.1/31` netns side)
that **only** the supervisor routes to. The GUI binds `mgmt0:<highport>` (>1024, no
`CAP_NET_BIND_SERVICE`). This management veth is a pure point-to-point host↔netns
link and is **never** attached to any lab bridge, so management traffic is fully
separate from lab traffic. AF_UNIX is preferred (no addressing, no second device);
the /31 is documented only as the escape hatch.

The reverse proxy is a **new web security boundary** (review #9). wsbridge today has
no Origin check and only fixed `/control`,`/console`,`/capture` routes
(`wsbridge.go:134-137`, package doc `:30`). The `/tool/{nodeId}` route must: reject
any proxy path not on a per-pack allowlist, reject arbitrary WS upgrades by default,
sanitize forwarded headers, serve the pack UI under a strict CSP (`frame-src`/
`default-src` scoped so a pack page cannot reach `/control`), and be gated by the
same control-session authorization as the rest of the bridge. Designed in P3;
called out here so it is not forgotten.

### 3.2 In-canvas node + its module UI

A new `src/lib/nodes/ToolNode.svelte` peer to `IolNode`/`VpcsNode`. Rest state is a
normal node face (status LED at the head of the mono name chip per `DESIGN.md` §2)
showing `running`/`stopped`. The module console opens as a **panel/tab** like the
existing console/capture tabs, and (per the Level A commitment, §2.6) **iframes the
reverse-proxied pack GUI** (§3.1) — secbench's own htmx module cards, unchanged.

### 3.3 Output/log streaming

Under Level A (§2.6), **the pack GUI owns its own 400-line-per-module ring buffer
and iolbox just reverse-proxies it** — no log-streaming code is added to
`internal/tool`. secbench's dashboard already polls each module's buffer over htmx
every 3s; that traffic rides the `/tool/{nodeId}` proxy unchanged. (The rejected
Level B would instead have mirrored the ring buffer in `internal/tool` and streamed
it over a `GET /tool/{nodeId}/log/{moduleKey}` WS colorized by the `[LEVEL]` tag —
not built, since Level B is not the design of record.)

### 3.4 Composition with existing node interactions

- **Link-add:** one interface (`eth1`), one-link cap, so `InterfacePicker` collapses
  to that single choice (same UX as the single-interface NAT node).
- **Multi-select / drag / delete:** unchanged; `node.remove` drops touching links +
  rebuilds the bridge plan — the tool detach hooks into that path (§4).
- **Node edit dialog:** shows Name + pack + the manifest's `options`; no RAM/image/
  ethernet.
- **Capture** on the tool's link works with the standard right-click-link capture.

---

## 4. Backend integration details

### 4.1 Porting pnet-secbench (the first tool pack)

#### 4.1.1 Which modules port — the scope ruling

secbench ships **28 modules** in `gui/modules.go`: 18 L2/L3 attack+recon modules and
a 10-module `fw_*` NGFW-test group, plus a **Victim Mode** app-layer target. The
selection test for iolbox is a single question: **does this module teach something a
CCNA/CCNP-level student mitigates with a Cisco IOS config on an IOL router/switch in
*this* lab?** iolbox runs only Cisco IOL images (routers/switches) + VPCS — there is
no firewall appliance image and no app-layer server image to act as a
device-under-test. Anything whose payoff depended on a firewall/app-layer target
iolbox cannot instantiate is dropped.

**KEEP — the 18 L2/L3 modules (all of them), organized by secbench's existing
groups.** Each pairs a scapy attack against an IOL switch/router with the exact IOS
config that defeats it, and the whole attack→mitigate→re-run loop runs entirely
on-segment between the tool node, an IOL device, and (optionally) a VPCS host — no
DUT beyond what iolbox already runs:

| Group (`pack.json`) | Modules kept | The IOS mitigation the student configures on IOL |
|---|---|---|
| `recon` | ARP Scan, DHCP Discover, Passive Sniff | recon only (no mitigation) — but pure on-segment L2 discovery that *feeds* the spoof/DHCP modules; needs nothing but the lab link, so it stays. |
| `spoof` | ARP Spoof / MITM, MAC Spoof, CAM/MAC Flood | Dynamic ARP Inspection + DHCP snooping; port-security sticky + violation restrict; port-security max-MAC + err-disable. |
| `dhcp` | Rogue DHCP Server, DHCP Starvation | DHCP snooping + trusted uplink; DHCP snooping rate-limit + port-security. |
| `stp` | STP Root Hijack, CDP Flood/Spoof, LLDP Flood/Spoof | BPDU Guard + Root Guard; `no cdp run`/`no cdp enable`; `no lldp run`/`no lldp transmit\|receive`. |
| `vlan` | DTP Trunk Hop, 802.1Q Double-Tag Hop | `switchport nonegotiate` (static access); native-VLAN hardening (park + prune off the trunk). |
| `fhrp` | HSRP Hijack, VRRP Hijack, OSPF Rogue Adjacency, EIGRP Rogue Adjacency, IPv6 RA/DHCPv6 Spoof | HSRP MD5 auth; VRRP auth; OSPF MD5 auth + `passive-interface default`; EIGRP MD5 key-chain auth; IPv6 RA Guard + DHCPv6 Guard. |

Every kept module already targets `eth1`, already `--selftest`s, already emits
`[LEVEL]` lines, and already handles the Linux-bridge "all-zero source MAC dropped"
reality (`common.py:74/93`), so they port unchanged.

**DROP — the 10-module NGFW group (`ngfw`), whole.** `fw_http`, `fw_eicar`,
`fw_url`, `fw_dns`, `fw_vuln`, `fw_file`, `fw_dlp`, `fw_dos`, `fw_ssl`, `fw_scan`.
Every one fires application-layer test traffic **at a next-gen firewall
device-under-test** (PA-VM/VM-Series, FortiGate VM) sitting between the node and a
server behind it, and its "mitigation" is a **Palo Alto / FortiGate security
profile** — App-ID/decryption, Antivirus, URL-filter, Anti-Spyware/DNS-Security,
IPS, File-Blocking, DLP, Zone/DoS Protection, decryption-profile cert checks, recon
protection. **None has a Cisco IOS mitigation the student configures on an IOL
device, and iolbox has no firewall image to fire at.** They were built for
PCNSE/NSE-style firewall labs, not L2/L3 IOS labs — no device to test against, so
dropped in full. (This includes `fw_ssl` and `fw_scan`, which the README's stale
"8-module" prose omits.)

**DROP — Victim Mode (`victim/` and its sub-features).** The vulnerable-target Go
binary (SQLi/XSS/cmd-injection/path-traversal sinks, the EICAR/file-type download
repo, the DLP SMTP/FTP sinks on `:2525`/`:2121`, the bad-cert "cert zoo" on
`:9101–9104`) exists **solely as the inside server the NGFW tests fire at** — it is
an HTTP/app-layer target for exercising firewall *security profiles*, not a Cisco
L2/L3 device with an IOS mitigation. Nothing in it teaches a switch/router config;
its entire audience is the `fw_*` group, which is itself dropped. So Victim Mode
goes with it, along with its `victim` node option (§2.1, §2.6) and its separate
`nobody` listener process (which simplifies the lifecycle, not complicates it — one
fewer long-lived process in the tool cage; no §5/§2.5 change needed).

**Net for the first pack: 18 modules across 6 groups kept; 10 NGFW modules + Victim
Mode dropped.** `pack.json`'s `groups` therefore lists six
(`recon`/`spoof`/`dhcp`/`stp`/`vlan`/`fhrp`), and the `ngfw` group and `victim`
option are gone.

#### 4.1.2 What ports, changes, and is dropped mechanically

**Kept as-is (the bulk):** the 18 L2/L3 `attacks/*.py` + `common.py` (see §4.1.1),
the scapy venv recipe (§7), and `gui/` (the Level A host target, §2.6) with its three
interface locks retained as defense-in-depth. The GUI's compiled `moduleDefs` and the
htmx tabs drop to the six kept groups (the `ngfw` group, its `selectOptions`
dropdowns, `targetHint`, the `Target`/victim settings, and the `panorama`/`scenarios`
firewall-lab helpers become dead code and are removed from the ported GUI).

**Dropped:** the entire `ngfw` module set + `attacks/fw_*.py`; the `victim/` binary;
plus the Docker scaffolding — `Dockerfile`, `entrypoint.sh` (its `/firstboot.cfg`
wait is a PNetLab mechanism iolbox lacks — replaced by iolbox writing the node's
options into the netns scratch dir at launch), `template/secbench.yml` (→
`pack.json`), and the PNetLab-gate harness (→ an `internal/tool` test + a builder
smoke).

**Changed (small):** the GUI listens on AF_UNIX not `:80` (§3.1); `hasLabIface`
(`runner.go:165`) always passes now (netns guarantees `eth1`); config intake reads
iolbox-passed options instead of `/firstboot.cfg`.

### 4.2 Every fabric point a `tool` kind must be added to (fixes High #6)

Adding `case KindTool` to `attachFabricLink` alone is **not** sufficient — an
unknown kind currently falls through to the IOL "static tap" default and fails with
"no static tap" (`fabric_linux.go:317-323`), and stats/eligibility are computed
separately. The complete set, verified in the current tree:

1. **Fabric eligibility/planning** — `fabricNodes(doc)` / `isFabricLink`
   (`fabric_linux.go:142`) must treat a tool endpoint as fabric-eligible so its link
   is realised at all.
2. **`attachFabricLink` switch** (`fabric_linux.go:293-327`) — add
   `case node.Kind == lab.KindTool` → skip if `nr.tool == nil` (not yet started),
   else `nr.tool.AttachBridge(iolbr<linkid>)`.
3. **`detachFabricLink` switch** (`fabric_linux.go:471-489`) — add the tool case →
   `nr.tool.DetachBridge()`.
4. **Late-start attachment** — `startToolNode` must call `attachFabricForNode`
   (`fabric_linux.go:443`) after the veth is up, mirroring `startExtnetNode`
   (`handlers.go:646`), so a tool started after its bridge already exists gets wired.
5. **Stats / directional-classifier endpoint discovery** — `fabricLinkTapDevs`
   (`fabric_linux.go:598-616`) must return the tool's bridge-side dev `vtool<id>`;
   this feeds `fabricStats` (`:555`, frame/byte glow) and `openLinkDirstat` (`:346`,
   per-direction protocol breakdown). Without it the tool link shows no traffic and
   no directional data.
6. **`fabricLinkFullyAttached`** (`fabric_linux.go:213-240`) — works once
   `fabricLinkTapDevs` returns `vtool<id>` (it checks the kernel `master` symlink via
   `tapMasterIs`), so the restart-skip logic self-heals correctly. Verify the
   bridge-side end is what carries the `master` symlink.
7. **LACP slow-protocols tee** — `linkIsIOLToIOL` (`fabric_linux.go:427`) correctly
   excludes a tool endpoint (a tool is not IOL), so `openLinkSlowTee` skips it. No
   change needed; noted so it is a conscious skip, not an oversight.
8. **Teardown / rebuild** — `stopNode` (`handlers.go:690`) gains a `nr.tool != nil`
   branch (§5.6); `teardownFabric` (`fabric_linux.go:660-708`) gains a loop over
   tool endpoints to `Stop()` them (mirroring the `teardownVPCS` loop at `:703`);
   `handleLabReap` (`handlers.go:400`, "Force clean") already iterates `stopNode` +
   `teardownFabric`, so it covers tool nodes for free once those two know about tool.
9. **Tests** — link.add/link.remove hot-connect, node.restart, lab.stop, and
   stats-classifier discovery for a tool node, matching the fabric harness cadence.

### 4.3 Runtime feature detection (fixes Medium #11)

`extnet.Detect` (`/dev/net/tun` + `sudo -n true`) proves **none** of what a tool node
needs, and `/dev/net/tun` is not even required for a veth-only node. `tool.Detect`
runs an **operational, cleanup-verified probe** and caches a **capability matrix**,
not one optimistic boolean:

- Create a throwaway `iolprobe` netns; create a veth pair; move one end into it;
  create a probe cgroup under the delegated cgroup root and set `pids.max`/
  `memory.max`; run a trivial process into the cgroup+netns with the §2.5.2 cap
  transition; assert `/proc/self/status` shows exactly `CAP_NET_RAW`.
- **Tear all of it down and verify removal** (netns gone, veth gone, cgroup rmdir'd).
- Cache per-primitive results: `{netnsCreate, vethCreate, vethMoveNetns,
  cgroupDelegated, ambientCapTransition, unixProxy}` with a specific failure reason.
- Advertise `"tools"` only if every required primitive passed; otherwise the palette
  hides tool nodes and `node.start` rejects with `unsupported` and the cached reason.

Runs at server startup beside `extnet.Detect`. Unprivileged LXC / rootless-Docker
targets, where netns/cgroup delegation is blocked, fail closed cleanly — the same
degrade `natgw` does today.

---

## 5. Lab / node lifecycle robustness (the main ask)

This is the round's priority. NAT is process-less and IOL/VPCS each solve only part
of the problem; a tool node is a **supervised process tree** and must be treated as
a first-class lifecycle subsystem. The design below leaves no open lifecycle
question: it specifies supervision, reaping, resource caging with guaranteed
cleanup, stale-state recovery, and bounded stop, and it says exactly what
`lab.start`/`lab.stop` do for a tool node.

### 5.1 The process tree, and why the cgroup is the control handle

```
supervisor (root, child-subreaper)
  └─ ip netns exec iolt<id> setpriv … pack-GUI     (ioltool, NET_RAW, in cgroup tool-<id>)
        ├─ python attacks/arp_spoof.py             (inherits netns + cgroup + NET_RAW)
        └─ python attacks/…                        (inherits …)
```

A scapy attack script is a child of the **pack GUI**, not of the supervisor. If we
tracked only the GUI's pid we could not reliably find, account for, or kill its
descendants — exactly the class of bug that produced spinning orphan VPCS processes
(`spawn_linux.go:295-320`). The robust handle is the **cgroup**: every descendant is
a member regardless of fork/setsid/reparenting, `cgroup.events:populated` tells us
if *anything* is still alive, and `cgroup.kill` terminates the whole subtree
atomically. The cgroup — not pid-chasing — is the authoritative accounting and kill
mechanism for a tool node.

### 5.2 Process supervision and accurate state

`tool.Start` records the GUI `*exec.Cmd` and launches an **exit-watcher goroutine**
(mirrors IOL's `wait()`, `spawn_linux.go:395-410`):

- On `cmd.Wait()` return: if the node was deliberately stopped, state is already
  `stopped`; otherwise transition `running → crashed` and emit `node.state`, then run
  teardown (§5.6). This is the "GUI process died" path.
- **Readiness:** after launch, poll the AF_UNIX socket for a successful `Accept`/
  connect (the analog of `waitConsoleReady`, `spawn_linux.go:203`) with a bounded
  timeout; only then flip to `running` and expose `/tool/{nodeId}`. If it never comes
  up, kill the cgroup and mark `crashed`.
- **Wedged/zombied detection:** a GUI process that is alive but not serving is a
  distinct failure from a crash. A periodic liveness check (a lightweight HTTP GET on
  the socket, or reading `cgroup.events`) that fails for N consecutive intervals
  transitions the node to `crashed` and kills the cgroup, so a hung tool does not
  masquerade as `running`. (Compare the project lesson `systemctl is-active` lies on
  restart loops — assert the *socket is serving*, not just that a pid exists.)

### 5.3 Init / reaper

In v1 there is **no PID namespace** (§0.2), so the pack GUI is an ordinary host-PID
child of the supervisor — it is **not** PID 1 and does not inherit PID-1 reaping
duties. Reaping is guaranteed by two mechanisms:

- The pack GUI reaps its **own** scapy children (secbench's `Supervisor` already does
  via `cmd.Wait()` per `Runner`). Its children are its direct children, so its normal
  wait loop reaps them; no zombies while the GUI lives.
- If the GUI dies **without** reaping (crash), its children are orphaned. The
  supervisor sets `PR_SET_CHILD_SUBREAPER` so those orphans reparent to the
  **supervisor** (kept inside our accounting), and — decisively — they remain in the
  tool cgroup, so `cgroup.kill` reaps them on teardown regardless of reparenting.

**Forward-looking guard:** if a PID namespace is ever added (§0.2), the pack GUI
becomes PID 1 of that namespace and *must* run under a minimal init/reaper (a
`tini`-style wrapper as the namespace's PID 1) or stopped modules zombie
indefinitely. Documented so that change does not silently regress reaping.

### 5.4 cgroup v2 resource cage — created BEFORE the process starts

Moved to P1 (not late hardening), per Critical #4. `RLIMIT_AS` is explicitly **not**
a substitute — it bounds one process's address space, not aggregate RSS, CPU, or
descendant count. `systemd-run --scope` is **not** used (not portable to WSL/LXC,
may lack controller delegation). We manage a cgroup v2 subtree directly:

- At `tool.Start`, **before** launching the GUI: create
  `/sys/fs/cgroup/iolbox/tool-<id>/` and write, from the manifest/defaults
  (secbench's template implies `cpu:2, ram:2048`):
  - `memory.max` (hard RSS cap; the cgroup OOM-kills the tree instead of the appliance),
  - `pids.max` (bounds a fork-bomb / DHCP-starve module),
  - `cpu.weight` and/or `cpu.max` (bounds a flood module's CPU),
  - `memory.swap.max = 0`.
- **Place the child into the cgroup atomically at spawn** via
  `clone3(CLONE_INTO_CGROUP)` — Go exposes this as `SysProcAttr.CgroupFD`/
  `UseCgroupFD` (Go 1.22+). Fallback: write the child pid to `cgroup.procs` in a
  pre-exec step before `execve`. Atomic placement is preferred so limits bind before
  the child can allocate.
- **Guaranteed cleanup on every failure path:** `tool.Start` uses the same
  reverse-what-you-did discipline as `extnet.Start` (`endpoint_linux.go:110-168`) —
  any error after cgroup creation runs the teardown (`cgroup.kill` then rmdir, netns
  del, veth del). Teardown is idempotent and best-effort (missing objects are not
  errors), mirroring `runCmdsBestEffort` (`endpoint_linux.go:89-103`).
- **Supervisor crash / `kill -9`:** the cgroup and its processes survive (as do the
  netns/veth). They are reclaimed on the next start by the stale-recovery sweep
  (§5.5) — the appliance never leaks a tool cage across a hard restart.

### 5.5 Stale-state recovery on supervisor start (fixes the recurring orphan class)

The project has repeatedly been burned by orphans surviving a hard restart: spinning
orphan VPCS (`spawn_linux.go:301-306`), stranded IOL holding gigabytes
(`handlers.go:153-157`), leftover extnet devices causing "File exists"
(`endpoint_linux.go:121-146`). The existing self-healing is **lazy and
per-object**: `extnet.Start` precleans its own named device and retries
(`endpoint_linux.go:126,135-146`); `startFabric` gates on the *actual device*
existing, not just bookkeeping, and rebuilds anything missing (`fabric_linux.go:98,
164-168`); `handleLabReap` ("Force clean") force-stops everything on demand
(`handlers.go:400-413`). There is **no** global startup reap today.

A tool node cannot rely on lazy per-Start device preclean **alone**, because unlike a
tap it leaves **running processes** that a device-delete does not kill. So the tool
design adds an explicit startup sweep **and** a per-Start preclean, both keyed on the
deterministic naming convention (`tool-<id>`, `iolt<id>`, `vtool<id>`):

- **`tool.ReapStale()` — called once at supervisor startup** (a new step beside
  `extnet.Detect` in `server.New`, `server.go:122`). It:
  1. enumerates `/sys/fs/cgroup/iolbox/tool-*`, writes `cgroup.kill` to each (SIGKILL
     the whole subtree atomically), waits for `cgroup.events:populated=0`, then rmdir;
  2. `ip netns list` → deletes every `iolt*` netns;
  3. deletes any leftover `vtool*` / `mtool*` veth device;
  4. removes `/run/iolbox/tool/*` socket dirs.
  All best-effort and idempotent. After this, no tool cage from a prior instance
  survives — the same guarantee IOL/VPCS/NAT effectively get, made explicit because
  processes are involved.
- **Per-`Start` preclean:** `tool.Start` re-runs the same teardown for *this* node id
  before creating anything (mirrors `extnet.Start`'s opening `runTeardown`), so a
  re-add after an in-lifetime crash starts from clean kernel state, with one retry
  after a short pause for EBUSY (a fd still held when the first delete ran) — the
  exact self-heal `extnet.setupWithRetry` uses.

Because `stopNode` gains a `nr.tool` branch (§5.6) and `handleLabReap` already loops
`stopNode` + `teardownFabric`, the GUI "Force clean" button also covers tool nodes
with no extra wiring.

### 5.6 Bounded graceful-stop-then-kill (mirrors IOL/VPCS stop)

`Endpoint.Stop()` (called from `stopNode`, `handlers.go:690`, and teardown) mirrors
the existing stop discipline (`Process.Stop`, `spawn_linux.go:499-517`; `killVPCS`,
`:309-320`) but uses the cgroup as the authoritative kill:

1. Transition the node to `stopped` first (so the exit-watcher treats the imminent
   exit as expected, not a crash — exactly `Process.wait`'s `StateStopped` guard,
   `spawn_linux.go:399-402`).
2. Send `SIGTERM` to the GUI process group (spawned `Setpgid`, like VPCS) for a
   graceful shutdown; wait **bounded** (e.g. 5s, sized like `vpcsConsoleReadyTimeout`).
3. If `cgroup.events:populated` is still non-empty after the timeout, write
   `cgroup.kill` — SIGKILL the entire subtree atomically. This is *stronger* than
   VPCS's argv/port matching because cgroup membership is exact and needs no
   heuristics.
4. `DetachBridge` (nomaster the bridge-side veth), then delete veth, delete netns,
   rmdir the cgroup, remove the socket dir. All best-effort/idempotent.

### 5.7 What lab.start / lab.stop orchestrate for tool nodes

Spelled out to the same concreteness as the existing kinds:

- **`lab.start` / `node.start`** (`startNodes`, `handlers.go:496`): the new
  `KindTool` branch calls `startToolNode`, which — after the whole-lab fabric refresh
  and `prepareLabDir` that already run — does, in order: per-node preclean (§5.5);
  create cgroup + write limits (§5.4); `ip netns add` + veth create + move `eth1` in +
  bring up (§2.3); create the AF_UNIX dir (§3.1); launch the GUI into the cgroup with
  the cap transition (§2.5.2) and start the exit-watcher (§5.2); await readiness; flip
  to `running`; call `attachFabricForNode` so a tool whose bridge already exists
  hot-connects now (mirrors `startExtnetNode`, `handlers.go:646`). Any failure reverses
  everything created (§5.4). The tool's bridge itself is created by the whole-lab
  `startFabric` pass exactly as for other kinds.
- **`lab.stop`** (`handleLabStop` → `stopNode` per node, then `teardownFabric` when
  stopping all, `handlers.go:387-390`): each tool node's `stopNode` branch runs
  `Endpoint.Stop()` (§5.6). `teardownFabric` additionally loops tool endpoints to
  `Stop()` any not already stopped (mirroring the `teardownVPCS` loop,
  `fabric_linux.go:703-707`), guaranteeing no netns/veth/cgroup/process survives a
  full stop.
- **`node.remove`** already drops touching links + rebuilds the bridge plan; the tool
  detach rides `detachFabricLink` (§4.2) and the node's `Stop()`.
- **Load-over-running** (`handlers.go:160-168`) already stops the outgoing lab's nodes
  before dropping the reference; once `stopNode` knows `nr.tool`, tool nodes are torn
  down there too — no new code beyond the `stopNode` branch.

---

## 6. Open questions / risks

1. **Script trust boundary (the big one).** Packs run as `ioltool` with NET_RAW and,
   in v1, **can read the shared rootfs and see host PIDs** (no mount/PID ns, §0.2).
   The netns bounds *where traffic goes*; it does not sandbox the filesystem.
   Mitigation for v1: **first-party, immutable, root-owned packs built into the
   release; no runtime pack installation** (§2.6). Third-party packs are **out of
   scope** until the deferred sandbox layers (§0.2) and a signed-pack trust model
   exist. This is the deliberate consequence of the "netns is fine" ruling, recorded
   so it is a known limitation, not a surprise.
2. **Reverse-proxy web boundary.** The `/tool/{nodeId}` proxy is a new same-origin
   surface (review #9); its CSP/allowlist/auth design (§3.1) is a P3 deliverable and a
   real risk if shipped loose.
3. **Multi-tenant / shared-appliance safety.** iolbox is charter-single-user
   (`PLAN.md:4`). If ever shared (a `-capture-bind 0.0.0.0` remote story), an
   attack-tool node is a very different risk. **Owed:** a "tool nodes require
   single-user/local mode" gate.
4. **Capability-transition correctness on every runtime.** The §2.5.2 sequence is
   fiddly and kernel/version-sensitive; the §4.3 probe and the P0 hostile test are
   what keep it honest. It must be re-proven per target (§8 P3).
5. **`ip netns`/`setpriv` vs native syscalls.** Shelling out to `ip`/`setpriv` is
   consistent with the fabric and preferred for correctness; native `setns`/`unshare`/
   `capset` in Go is less code-dependency but more surface to get wrong. **Decision:**
   `ip`/`setpriv` for v1.
6. **Resource limits bound the tree, not the wire.** cgroup caps CPU/RSS/PIDs; a flood
   module can still saturate the one lab bridge — arguably correct for a teaching tool
   (the student wants to see the flood). Conscious "no global wire rate cap" decision.

---

## 7. Packaging / build impact

- **`runtime/build-rootfs.sh` (`:186`):** `BASE_INCLUDE` currently has no Python.
  Add `python3 python3-venv libpcap0.8 util-linux` (`libpcap` for scapy raw L2,
  `util-linux` for `setpriv`). Also create the dedicated unprivileged `ioltool`
  account (§2.5).
- **Offline pinned-hash wheelhouse (fixes High #14 — NOT a runtime `pip install`):**
  the reviewed draft's "`pip install scapy` in the chroot" implied network at build.
  Instead, at rootfs build time:
  1. on the build host, `pip download` scapy (+ its pinned deps) into a `wheelhouse/`
     with a `requirements.txt` carrying `--require-hashes`;
  2. copy `wheelhouse/` into the chroot and
     `python3 -m venv /opt/iolbox/tools/venv` then
     `pip install --no-index --find-links=/wheelhouse --require-hashes -r
     requirements.txt` — fully offline, hash-verified;
  3. **verify imports in the produced rootfs**:
     `python -c 'import scapy; from scapy.contrib import cdp, lldp, dtp, ospf, eigrp'`
     (the exact set secbench's Dockerfile verifies);
  4. `py_compile` the venv, then delete the wheelhouse and pip caches.
  This keeps the shipped image self-contained and airgapped (matching the build
  ethos in MEMORY). Record hashes/SBOM for the wheels.
- **Per-target matrix (`docs/bridge-fabric-migration.md` §7):** netns + veth + cgroup
  delegation must be **re-proven** on each target; the §4.3 probe fails closed where
  they are blocked (unprivileged LXC, rootless Docker), the `tools` feature drops, and
  the palette hides tool nodes — clean degrade, same as `natgw`.
- **`build-release.sh`: unchanged** — tool packs are rootfs payload, not compiled into
  the supervisor. (Under the committed Level A design the pack UI is the pack's own
  proxied GUI, so nothing is added to the embedded Svelte bundle beyond the
  `ToolNode` slice itself.)
- **Image size: a ROUGH ESTIMATE pending measurement, not a fact.** The dominant cost
  is the shared scapy venv, which is essentially fixed regardless of module count — so
  dropping the 10 NGFW modules + Victim Mode barely moves it: it removes ~10 small
  `fw_*.py` scripts and the `victim/` Go binary (tens to low-hundreds of KB), not any
  venv dependency (the NGFW tests used stdlib HTTP/sockets, not extra scapy layers).
  The trimmed payload is *estimated* at **~35–55 MB** (down marginally from the prior
  ~40–60 MB, still venv-dominated) — **unverified; measure the final compressed
  artifact per target** (OVA/QEMU/LXC/WSL/native/VMware) and record actuals before
  quoting a number. It is dramatically smaller than a Docker approach (no base image,
  no engine, no per-node layer) but the exact size must be measured.

---

## 8. Phased implementation plan

Re-sequenced so **P0 de-risks the actually-risky assumptions** (not just L2
forwarding) and **cgroup + lifecycle land early**. Gate the whole feature on P0.

- **P0 — spike (de-risk the core claims; no GUI, no pack).** On the builder, prove
  each as an **explicit acceptance test**, including a **hostile probe program**:
  1. **Fabric/L2 + capture:** netns + veth into a manual `iolbr0`, `eth1` moved in;
     a second node on the bridge receives a scapy ARP; the frame is captured on the
     bridge.
  2. **Capability transition:** Go → `setpriv` (bounding-set + inh + ambient +
     no-new-privs) → python → scapy holds **exactly** `CAP_NET_RAW`. Verify via
     `/proc/self/status` + `capsh --print`; the hostile probe tries to **regain** a
     dropped cap, **enumerate host interfaces/routes**, and perform a **NET_ADMIN
     op** — all must fail.
  3. **GUI reachability:** a stub GUI binds AF_UNIX inside the netns; the root-ns
     supervisor reverse-proxies it — no IP, no `CAP_NET_BIND_SERVICE`.
  4. **cgroup enforcement:** cgroup created **before** launch; a memory hog is
     OOM-killed at `memory.max`; a fork bomb is bounded by `pids.max`; the hostile
     probe **cannot escape its cgroup**.
  5. **Reaping:** GUI spawns children, is killed; children are reaped (subreaper +
     `cgroup.kill`), no zombies, `cgroup.events:populated=0`.
  6. **Stale cleanup after `kill -9`:** hard-kill the supervisor mid-run; on restart
     `ReapStale()` removes the leftover netns/veth/cgroup/processes; the hostile
     probe **cannot survive** the sweep.
  7. **Runtime support:** the §4.3 probe passes on the target and fails closed on
     unprivileged LXC/rootless-Docker.
  If any fail, the approach is wrong.
- **P1 — `internal/tool` + `tool` kind, headless, WITH cgroup + lifecycle.** Schema
  `KindTool` + validation; `tool.Detect` capability matrix (§4.3);
  `Start/Stop/AttachBridge/DetachBridge`; the exit-watcher + readiness + wedged
  detection (§5.2); **cgroup create-before-start + guaranteed cleanup (§5.4)**;
  `ReapStale` at startup + per-Start preclean (§5.5); bounded graceful-stop-then-kill
  (§5.6); the `startToolNode` branch and **all** fabric points in §4.2; the `tools`
  hello feature. Prove `lab.load`+`lab.start` of a one-node lab brings a tool node
  `running`, hot-connects/detaches on `link.add`/`link.remove` with no restart,
  survives a supervisor `kill -9` + restart with no leaks, and stops cleanly. Drive
  raw NDJSON like the fabric harness.
- **P2 — secbench pack (the committed Level A host, §2.6) + palette + AF_UNIX proxy
  console (the frontend vertical slice).** `pack.json` (six groups, 18 modules — no
  `ngfw`, no `victim`, §4.1); the offline wheelhouse baked into the rootfs (§7); the
  pack GUI (trimmed to the 18 L2/L3 modules) hosted in the netns on AF_UNIX and
  reverse-proxied via `/tool/{nodeId}`; the `NodeKind` union extension + drag payload
  + **new** `ToolNode.svelte` + `NodeEditDialog` + `tool.listPacks` verb/client-store.
  End-to-end: drag SecBench, wire it to an IOL switch, open its console, run ARP
  Spoof, watch it in the capture tab. First user-visible slice.
- **P3 — hardening + web boundary + per-target matrix.** The `/tool/{nodeId}` proxy
  security design (CSP/allowlist/auth, §3.1, review #9); NET_ADMIN-mediated-verb
  tightening; the per-target validation matrix (VMware/OVA/native/QEMU/WSL2/LXC/
  Docker) for `tool up`, `hot-connect`, `capture a tool link`, and `kill -9` recovery.
  Mount/PID/user namespaces remain **out of scope** (§0.2) — record their absence as
  a known limitation, not a P3 task.
- **P4 — (optional) a second tool pack, to prove the plugin contract.** Add a
  second, non-attack pack (also a Level A self-hosted GUI, §2.6) and confirm it drops
  in through `pack.json` + the rootfs venv/payload with **no new Go supervisor code** —
  the real test of the §2.6 contract. (Note: manifest-driven native Svelte cards —
  the former "Level B" — are **not** a deliverable; that alternative was considered
  and rejected, §2.6.)
