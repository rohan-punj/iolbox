# iolab — build plan

A lightweight, Windows-native lab tool for Cisco **IOL** images and **VPCS**.
Draw topologies, console into nodes, live-capture links in Wireshark. No login,
single-user, localhost-only. Ships as a small GitHub-hosted app.

## Non-negotiable constraints

1. **IOL is a Linux ELF binary** → it cannot run on Windows directly. The app is a
   native Windows GUI + a thin Linux **runtime provider** that executes IOL/VPCS.
2. **WSL2 is not the default.** VMware Workstation users lose nested virtualization
   when the Windows Hypervisor Platform is enabled. The runtime layer is *pluggable*;
   VMware (`vmrun`) is the primary provider on this class of machine.
3. **We never distribute Cisco images.** Users supply their own `.bin`/`.iol`.
4. **Lightweight**: no database, no web server, no root daemon on Windows. One
   supervisor process inside the Linux runtime; one GUI app on Windows.

## Architecture (layers, top to bottom)

```
┌─────────────────────────────────────────────────────────────┐
│  GUI  — Tauri 2 + Svelte 5 + Svelte Flow  (Windows .exe/.msi) │
│  canvas · palette · image manager · consoles · capture UI    │
└──────────────────────────┬──────────────────────────────────┘
                           │ localhost TCP (JSON control protocol)
                           │ + telnet TCP (consoles) + TCP pcapng (capture)
┌──────────────────────────┴──────────────────────────────────┐
│  RUNTIME PROVIDER  (pluggable: provision/start/stop/endpoint)│
│  vmware(vmrun) · wsl2 · remote(ssh) · qemu-tcg               │
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
| `qemu-tcg` | Nothing else available                    | Pure software emulation, no hypervisor. "Compatibility mode," slow. |

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

- **Image library**: a Windows-side folder (default `%APPDATA%\iolab\images`) the
  GUI manages. "Add image" = copy a `.bin`/`.iol` in; the app fingerprints it
  (sha256), sniffs L2-vs-L3 and arch (i386/x86_64), and records metadata.
- Images are synced into the runtime on demand (shared folder / `vmrun` copy / scp).
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

- GUI: **Tauri 2**, **Svelte 5** (runes), **Svelte Flow** (`@xyflow/svelte`),
  **xterm.js**. Rust core owns process/provider orchestration on the Windows side.
- Supervisor: **Go** (single static linux/amd64 binary, trivial cross-compile).
- Runtime: Debian-slim rootfs (glibc + i386 multiarch for 32-bit IOL).
- Control protocol: newline-delimited JSON over TCP (see `docs/protocol.md`).

## Repo layout

```
iolab/
  PLAN.md  README.md  LICENSE  .gitignore
  docs/         architecture.md · lab-schema.md · protocol.md · providers.md
  contracts/    lab.schema.json  (canonical JSON Schema; codegen source of truth)
  supervisor/   Go module (control API, relays, iol/vpcs mgmt, nvram, iourc)
  runtime/      rootfs build scripts · appliance packaging · provider bootstrap
  app/          Tauri + Svelte frontend
    src-tauri/  Rust: provider orchestration, image library, capture helper
    src/        Svelte UI
  labs/         example lab JSON + pack-import script
  tools/        capture helper, dev scripts
```

## Model assignment for build tasks

- **Opus**: supervisor data-plane (UDP relay/hub + pcapng tee), provider interface
  design, NVRAM/iourc codecs — the correctness-critical, protocol-heavy parts.
- **Sonnet**: Tauri/Svelte scaffolding + UI components, image-manager plumbing,
  CI/packaging, docs, example labs — high-volume, well-specified surface area.
- **Fable (orchestrator)**: contracts, integration, review, P0 wiring/verification.
