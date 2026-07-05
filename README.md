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

## Install

Grab the artifact for your platform from the [latest release](../../releases/latest)
and follow **[docs/INSTALL.md](docs/INSTALL.md)** — step-by-step setup for all six
targets:

| Situation | Artifact |
|---|---|
| Windows desktop, simplest path | `iolbox-disk-*.qcow2` + the `iolbox-launcher.exe` bundle |
| VMware Workstation / ESXi / VirtualBox | `iolbox-appliance-*.ova` (or the `.vmdk`+`.vmx`) |
| WSL2 box | `iolbox-rootfs.tar` |
| Proxmox homelab | `iolbox-ct-*.tar.zst` |
| Existing Linux server | `iolbox-server-*.tar.gz` |

The GUI comes up at `http://<host>:4001` (no login — keep it on localhost or a
trusted network). **You supply your own IOL images.**

## Status

**v0.4.0 — validated end-to-end on real IOL 17.18.02.** Full stack works through
the real supervisor (register → load → start → console → native wiring → capture)
across every runtime provider; the six release artifacts are built and smoke-tested.
See [docs/INSTALL.md](docs/INSTALL.md) to get started.

| Component | State |
|---|---|
| Supervisor (Go) | ✅ builds linux/amd64, `go test`/`vet`/`fmt` clean, stdlib-only |
| Runtime (rootfs + WSL/VMware appliance) | ✅ build scripts authored, `bash -n` clean |
| Capture helper (Wireshark bridge) | ✅ builds windows/amd64 |
| GUI (Tauri + Svelte Flow) | ✅ frontend verified interactive (mock backend); native compile in CI |
| End-to-end (real IOL 17.18.02) | ✅ validated across VMware/WSL/LXC/native/QEMU; v0.4.0 artifacts smoke-tested |

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
