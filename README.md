# iolbox

**A lightweight, Windows-native lab for Cisco IOL and VPCS.** Draw topologies,
console into every node, and live-capture any link straight into Wireshark. No
login, no database, no web server — a single small app.

> ⚠️ **iolbox ships no Cisco software.** It runs IOL/IOU images *you* supply and
> hold licenses for. See [Legal](#legal).

## Why

Existing options for running IOL on Windows are heavy (PNetLab/EVE/CML VMs) and
most break nested virtualization or fight with VMware Workstation. iolbox keeps the
GUI native and pushes only the tiny Linux execution layer into whatever hypervisor
you already have.

## How it works

IOL binaries are Linux ELF executables, so they can't run on Windows directly.
iolbox runs them inside a small **runtime provider** and talks to it over localhost:

- **VMware Workstation** (default here) — a headless helper VM driven by `vmrun`.
  No nested-virt loss, no Hyper-V, no conflict with your existing labs.
- **WSL2** — fastest, when the Hyper-V platform is already enabled.
- **Remote/existing Linux** over SSH.
- **QEMU (software)** — zero-prerequisite compatibility mode.

Nodes wire together over UDP tunnels (the `iol_wrapper` pattern), so a link is just
a relay — which is also where Wireshark taps in, no bridges or root required.

See [PLAN.md](PLAN.md) and [docs/](docs/) for the full design.

## Features

- Topology canvas — drag, connect, context menus (Svelte Flow)
- IOL (L2/L3) + VPCS nodes
- **Easy image loading & hot-swap** — drop a `.bin` in the library, swap any node's
  image from a picker; labs reference images by content id so they stay portable
- Per-node web consoles (xterm.js) + "open in external terminal"
- Right-click a link → live Wireshark capture (or record to `.pcapng`)
- Startup-config save/restore via NVRAM
- One-file JSON labs — diffable, shareable
- Automatic iourc licensing inside the runtime

## Status

**Scaffold complete — pre-P0.** All components build and verify in isolation; the
end-to-end run against a real IOL image ([P0 spike](docs/p0-spike.md)) is the next
milestone. Not yet released.

| Component | State |
|---|---|
| Supervisor (Go) | ✅ builds linux/amd64, `go test`/`vet`/`fmt` clean, stdlib-only |
| Runtime (rootfs + WSL/VMware appliance) | ✅ build scripts authored, `bash -n` clean |
| Capture helper (Wireshark bridge) | ✅ builds windows/amd64 |
| GUI (Tauri + Svelte Flow) | ✅ frontend verified interactive (mock backend); native compile in CI |
| P0 end-to-end (real IOL) | ⏳ needs a user-supplied image |

See [PLAN.md](PLAN.md) for the roadmap and [docs/p0-spike.md](docs/p0-spike.md) for
the exact next steps.

## Build from source

Prereqs: Windows 10/11, Node 18+, Rust (MSVC), Go 1.22+, and one runtime
(VMware Workstation/Player, or WSL2). See [docs/build.md](docs/build.md).

## Legal

iolbox is an independent open-source tool and is **not** affiliated with or endorsed
by Cisco Systems. It does not include, distribute, or generate Cisco IOS/IOL/IOU
software or license keys for use outside your entitlement. You are responsible for
lawfully obtaining images and complying with your license agreements. See
[LICENSE](LICENSE).
