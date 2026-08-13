# iolbox — build plan

A lightweight, Windows-native lab tool for Cisco **IOL** images and **VPCS**.
Draw topologies, console into nodes, live-capture links in Wireshark. No login,
single-user, localhost-only. Ships as a small GitHub-hosted app.

> **Status: shipped, v0.5.2.** This is the *original* build plan — P0 through
> P4 below all happened and the product has been live-verified against real
> IOL images across many sessions since. One design decision changed along
> the way: the GUI pivoted from a native Tauri/Rust shell to browser-first
> (the Go supervisor embeds and serves the Svelte frontend over HTTP — see
> `.github/workflows/release.yml`'s notes). Everywhere below that still says
> "Tauri" or "native Windows GUI" describes that original design, not what
> shipped — see [README.md](README.md) for the current architecture and
> [docs/build.md](docs/build.md) for the current build steps.

## Non-negotiable constraints

1. **IOL is a Linux ELF binary** → it cannot run on Windows directly. The app is a
   Windows-hosted GUI (browser-based) + a thin Linux **runtime provider** that
   executes IOL/VPCS.
2. **WSL2 is not the default.** VMware Workstation users lose nested virtualization
   when the Windows Hypervisor Platform is enabled. The runtime layer is *pluggable*;
   VMware (`vmrun`) is the primary provider on this class of machine.
3. **We never distribute Cisco images.** Users supply their own `.bin`/`.iol`.
4. **Lightweight**: no database, no root daemon on Windows. One supervisor
   process inside the Linux runtime, serving its own GUI over localhost HTTP —
   no separate native Windows app process.

## Architecture (layers, top to bottom)

```
┌─────────────────────────────────────────────────────────────┐
│  GUI  — Svelte 5 + Svelte Flow, browser-first                │
│  served by the supervisor itself over localhost HTTP;        │
│  canvas · palette · image manager · consoles · capture UI    │
└──────────────────────────┬──────────────────────────────────┘
                           │ WebSocket (JSON control protocol)
                           │ + WS console streams + TCP pcapng (capture)
┌──────────────────────────┴──────────────────────────────────┐
│  RUNTIME PROVIDER  (pluggable: provision/start/stop/endpoint)│
│  vmware(vmrun) · wsl2 · remote(ssh) · qemu                   │
│  ─ hosts a Debian-slim environment ─                         │
│    ┌────────────────────────────────────────────────────┐   │
│    │  SUPERVISOR (Go, linux/amd64)                        │   │
│    │  spawns iol_wrapper + vpcs, UDP link relays (tee),   │   │
│    │  telnet console ports, iourc gen, NVRAM in/out       │   │
│    └────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**Why this is light:** IOL nodes wire to each other over **UDP tunnels** (the
`iol_wrapper` / iouyap pattern). VPCS speaks UDP tunnels natively. A link is a
pair of UDP forwards; a multi-access segment is a tiny userspace hub. Wireshark is
a **tee** on the relay, not a kernel tap — no bridges, no tun/tap, no root on
Windows. That removes ~everything heavy that PNetLab/EVE carry.

## Runtime providers (ranked selection at first run)

| Provider   | When chosen                              | Notes |
|------------|------------------------------------------|-------|
| `vmware`   | `vmrun.exe` present (default on this box) | Headless helper VM via `vmrun ... nogui`; endpoint via host-only IP. Free since Broadcom; also works with Player (`-T player`). |
| `wsl2`     | Hyper-V platform already enabled          | Fastest, smallest. Never auto-enables Hyper-V (would break VMware). |
| `remote`   | User points at an existing Linux/VM       | Supervisor self-deploys over SSH. Power-user + CI mode. |
| `qemu`     | Nothing else available                    | Pure software emulation (TCG), no hypervisor. "Compatibility mode," slow. |

First-run **preflight** detects VMware, Hyper-V state, importable WSL, and presents
a ranked choice with plain-English tradeoffs. Never fails cryptically.

## IOL specifics baked in from day one

- **iourc licensing**: generated automatically *inside* the runtime, where hostname
  is under our control (stock keygen from hostid). "Just works," no user step.
- Strip the `-l` keepalive flag (causes 100% CPU idle spin).
- Give the runtime **no default route** rather than null-routing `xml.cisco.com`
  (L2 IOL aborts if that host is explicitly unreachable).
- NETMAP wiring: `id = port*16 + group` (same encoding as our existing lab packs).
- **NVRAM startup-config**: inject at boot, extract on save (GNS3 iou nvram codec).
  Included in v1 — the difference between a toy and a study tool.

## Image loading & swapping (explicit requirement)

- **Image library**: for most targets (OVA/vmdk/WSL/LXC/native), the browser
  GUI's Images upload sends a `.bin`/`.iol` straight to the supervisor's own
  library (inside the runtime it's already running in) — no separate sync
  step. Only the Windows-launcher/QEMU target still uses a Windows-side
  folder (`images\` next to `iolbox-launcher.exe`, uploaded to the guest on
  each launch), since that target's runtime is ephemeral (see
  `docs/INSTALL.md` section 6). Either way the supervisor fingerprints each
  image (sha256), sniffs L2-vs-L3 and arch (i386/x86_64), and records metadata.
- **Swap in a lab**: a node references an image by **id**, not path. Right-click node
  → "Change image" → pick from library → applies on next start. Bulk "replace all
  nodes using image X with Y." Because the lab file stores only the id + a fallback
  filename, labs stay portable and images stay hot-swappable.

## Lab file = one JSON document

Portable, diffable, shareable. Nodes (x/y, image id, ram, ethernet/serial counts,
embedded startup-config), links (NETMAP-style endpoint pairs), metadata. Import =
copy the file. See `docs/lab-schema.md` (the contract) — our existing CCNA/CCNP/CCIE
IOL packs can be converted with a small script for an instant content library.

## Build order

- **P0 — spike (risk retirement, no GUI):** runtime appliance boots; one IOL boots
  with generated iourc + telnet console reachable from Windows; two IOL + one VPCS
  wired via UDP relays, pings pass; relay tees a pcap that opens in Windows
  Wireshark. Validate against **two** providers (vmware + wsl2). If P0 passes, the
  rest is known-good engineering.
- **P1 — control plane:** supervisor (spawn/stop/status, lab JSON, relays, consoles);
  provider interface with vmware + wsl2; Tauri shell + Svelte Flow canvas + palette;
  start/stop lab.
- **P2 — consoles:** embedded xterm.js console tabs; external-terminal launch.
- **P3 — capture:** live Wireshark tee + record-to-file; right-click link.
- **P4 — content & polish:** NVRAM save/extract; image manager UI; lab-pack import;
  remote + qemu providers; installer with preflight; CI + GitHub release.

## Tech stack

- GUI: **Svelte 5** (runes), **Svelte Flow** (`@xyflow/svelte`), **xterm.js** —
  built with Vite and embedded into the supervisor binary (Go's `embed`
  package), served over localhost HTTP to any browser. No Rust, no native
  Windows app process. (A Tauri 2 + Rust shell exists under `app/src-tauri`
  from the original design below but isn't the shipped product.)
- Supervisor: **Go** (single static linux/amd64 binary, trivial cross-compile).
- Runtime: Debian-slim rootfs (glibc + i386 multiarch for 32-bit IOL).
- Control protocol: WebSocket JSON control channel for the browser GUI
  (`/control`); the supervisor also exposes the original loopback-only
  NDJSON/TCP port for other clients — see `docs/protocol.md`.

## Repo layout

```
iolbox/
  PLAN.md  README.md  LICENSE  .gitignore
  docs/         architecture.md · lab-schema.md · protocol.md · providers.md
  contracts/    lab.schema.json  (canonical JSON Schema; codegen source of truth)
  supervisor/   Go module (control API, relays, iol/vpcs mgmt, nvram, iourc);
                embeds the built GUI (internal/web/dist) into its own binary
  runtime/      rootfs build scripts · six packaging targets (WSL/VMware/OVA/
                LXC/native/QEMU) · provider bootstrap
  app/          Svelte 5 + Vite frontend (the shipped GUI, embedded above)
    src-tauri/  legacy Rust shell — NOT the shipped product, see PLAN.md's
                status note at the top
    src/        Svelte UI
  labs/         example lab JSON + pack-import script
  tools/        iolab-launcher (Windows deliverable), capture helper, dev scripts
```

## Model assignment for build tasks

- **Opus**: supervisor data-plane (UDP relay/hub + pcapng tee), provider interface
  design, NVRAM/iourc codecs — the correctness-critical, protocol-heavy parts.
- **Sonnet**: Tauri/Svelte scaffolding + UI components, image-manager plumbing,
  CI/packaging, docs, example labs — high-volume, well-specified surface area.
- **Fable (orchestrator)**: contracts, integration, review, P0 wiring/verification.
