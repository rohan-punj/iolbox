# iolbox
<img width="1851" height="1161" alt="{914C57B9-2B87-485A-A5ED-23CBE02AD2E3}" src="https://github.com/user-attachments/assets/d8e7470f-59bc-4125-8202-742994a7b632" />

**A lightweight, Windows-native lab for Cisco IOL and VPCS.** Draw topologies,
console into every node, and live-capture any link straight into Wireshark. No
login, no database — a single small app with a browser-based GUI on localhost.

> ⚠️ **iolbox ships no Cisco software.** It runs IOL/IOU images *you* supply and
> hold licenses for. See [Legal](#legal).

## Why

Existing options for running IOL on Windows are heavy (PNetLab/EVE/CML VMs) and
most break nested virtualization or fight with VMware Workstation. iolbox keeps
the GUI a plain browser tab and pushes only the tiny Linux execution layer into
whatever hypervisor you already have.

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
| Windows desktop, simplest path | `iolbox-launcher-*-windows.zip` (launcher + disk + bundled QEMU) |
| VMware Workstation / ESXi / VirtualBox | `iolbox-appliance-*.ova` (or the `.vmdk`+`.vmx`) |
| WSL2 box | `iolbox-rootfs.tar` |
| Proxmox homelab | `iolbox-ct-*.tar.zst` |
| Existing Linux server | `iolbox-server-*.tar.gz` |

The GUI comes up at `http://<host>:4001` (no login — keep it on localhost or a
trusted network). **You supply your own IOL images.**

## Status

**v0.5.2 — live-verified against real IOL images across many sessions of use,
not just a one-time smoke test.** The GUI is browser-first now: a single Go
supervisor binary embeds the built Svelte frontend and serves it over HTTP —
no native Windows app to install. The `app/src-tauri` shell from an earlier
design is no longer the shipped product (the Windows deliverable is the plain
`tools/iolab-launcher` exe); see `.github/workflows/release.yml`'s notes.

| Component | State |
|---|---|
| Supervisor (Go) | ✅ builds linux/amd64, `go test`/`vet`/`fmt`/`-race` clean, live-deployed |
| Runtime (rootfs + WSL/VMware/OVA/QEMU/LXC/native) | ✅ six packaging targets, all release-built |
| Capture helper (Wireshark bridge) | ✅ builds windows/amd64 |
| GUI (browser-first Svelte 5, embedded in the supervisor) | ✅ live-verified in the browser against a real deployed appliance |
| End-to-end (real IOL images) | ✅ full lab lifecycles (load/start/console/capture/stop) repeatedly verified live on a deployed VM, across sessions |

See [PLAN.md](PLAN.md) for the original design (its GUI section predates the
browser-first pivot — the architecture above is current) and
[docs/](docs/) for detailed design/session notes.

## Build from source

Prereqs: Windows 10/11, Node 20+ (Vite needs a recent Node), Go 1.26, and one
runtime (VMware Workstation/Player, or WSL2). Rust/MSVC is only needed for the
legacy Tauri shell under `app/src-tauri`, not for building the shipped
product — see `build-release.sh` (supervisor + embedded GUI) and
`tools/iolab-launcher` (the plain-Go Windows deliverable). Full build steps:
[docs/build.md](docs/build.md) — note this predates the browser-first pivot in
places and is due for a refresh.

## Legal

iolbox is an independent open-source tool and is **not** affiliated with or endorsed
by Cisco Systems. It does not include, distribute, or generate Cisco IOS/IOL/IOU
software or license keys for use outside your entitlement. You are responsible for
lawfully obtaining images and complying with your license agreements. See
[LICENSE](LICENSE).
