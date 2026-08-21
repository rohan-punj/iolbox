# iolbox
<img width="1787" height="1052" alt="003 - &#39;iolbox&#39;" src="https://github.com/user-attachments/assets/f8aa2e64-42ae-422e-9acc-f41f2f60681f" />
<img width="1784" height="1050" alt="002 - &#39;iolbox&#39;" src="https://github.com/user-attachments/assets/4ccc8558-098d-4740-b311-c4cb1b107635" />


**A lightweight, single-user lab for Cisco IOL and VPCS — on Windows, Linux, or an Apple Silicon Mac.** Draw topologies,
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
- **Lima** (macOS Apple Silicon) — native-arm64 or Rosetta guest profiles.

Nodes wire together over UDP tunnels (the `iol_wrapper` pattern), so a link is just
a relay — which is also where Wireshark taps in, no bridges or root required.

See [PLAN.md](PLAN.md), [docs/](docs/), and the
[project wiki](https://github.com/rohan-punj/iolbox/wiki) (architecture,
control protocol, build pipeline, known footguns) for the full design.

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
(`<tag>` below = the release tag, e.g. `v0.6.0`). Whichever you pick, the GUI
comes up at `http://<host>:4001` (no login — keep it on localhost or a trusted
network), and **you supply your own IOL images**. Full step-by-step setup,
sizing, and troubleshooting for every target: **[docs/INSTALL.md](docs/INSTALL.md)**.

**Windows, simplest path** — `iolbox-launcher-<tag>-windows.zip` (launcher +
disk + bundled QEMU; no VMware, no WSL2, no admin rights):
extract, double-click `iolbox-launcher.exe`, accept the vCPU/RAM prompts.
It boots under QEMU/TCG (software emulation — first boot takes minutes) and
opens your browser to the GUI. Images go in `images\` next to the exe.

**VMware Workstation / ESXi / VirtualBox** — `iolbox-appliance-<tag>.ova`:
import it (File > Open / Deploy OVF / Import Appliance — the missing-manifest
warning is expected), start the VM, browse to `http://<vm-ip>:4001`.
Prefer skipping OVF import (e.g. many copies)? Grab the raw
`iolbox-appliance-<tag>.vmdk` + `.vmx` pair instead — both files in one
folder, then open the `.vmx` or `vmrun -T ws start "iolbox-appliance-<tag>.vmx" nogui`.

**WSL2** — `iolbox-rootfs.tar`:

```powershell
wsl --import iolbox C:\Users\<you>\iolbox-wsl iolbox-rootfs.tar
# browse to http://localhost:4001
```

**Proxmox LXC** — `iolbox-ct-<tag>.tar.zst`: upload to CT-template storage, then

```bash
pct create <vmid> local:vztmpl/iolbox-ct-<tag>.tar.zst --unprivileged 1 \
    --hostname iolbox --cores 4 --memory 4096 \
    --net0 name=eth0,bridge=vmbr0,ip=dhcp --onboot 1
```

plus two TUN/TAP device lines in the CT config — see
[docs/INSTALL.md §4](docs/INSTALL.md).

**Linux server (systemd, x86-64)** — `iolbox-server-<tag>.tar.gz`:

```bash
tar xzf iolbox-server-<tag>.tar.gz && cd iolbox-server-<tag>
sudo ./install.sh          # localhost-only; use --bind all for LAN access
```

**macOS Apple Silicon (Lima)** — `iolbox-macos-arm64.tar.gz` (+ `.sha256`):
requires [Lima](https://lima-vm.io/), installed separately. Verify the
checksums, extract, then either double-click `IOLbox.app` (an optional,
unsigned convenience launcher included in the archive) or run the CLI
directly:

```sh
./iolbox start             # GUI at http://127.0.0.1:4001
```

`IOLbox.app` opens a real Terminal window running that same command, so
every prompt (including the first-run vCPU/RAM choice) is still visible —
it's a Finder/Dock-friendly starter, not a background app or control panel.
It's unsigned like the CLI binary: on a **browser download**, run
`xattr -dr com.apple.quarantine` on the extracted folder **before** the
first double-click, or Gatekeeper deletes the app outright rather than just
warning about it.

**To stop:** run `./iolbox stop` from the CLI, or — from `IOLbox.app`'s
Terminal window — type `stop` at the `iolbox>` prompt that appears once
start finishes (`status`/`diagnose` also work there). Closing the window
without typing `stop` leaves the VM running in the background; nothing
stops it for you.

On a fresh install the launcher prefers the **native-arm64** profile (arm64
supervisor/VPCS; x86_64 IOL translated by redistributed qemu-user inside the
guest); the Rosetta **debian13**/**jammy** profiles remain available via
`--profile`. x86_64 IOL only, on every profile. Details:
[docs/INSTALL.md §7](docs/INSTALL.md) and the wiki's
[macOS Lima platform notes](https://github.com/rohan-punj/iolbox/wiki/macOS-Lima-Platform-Notes).

## Status

**v0.6.0 — live-verified against real IOL images across many sessions of use,
not just a one-time smoke test.** The GUI is browser-first now: a single Go
supervisor binary embeds the built Svelte frontend and serves it over HTTP —
no native Windows app to install. The `app/src-tauri` shell from an earlier
design is no longer the shipped product (the Windows deliverable is the plain
`tools/iolab-launcher` exe); see `.github/workflows/release.yml`'s notes.

| Component | State |
|---|---|
| Supervisor (Go) | ✅ builds linux/amd64, `go test`/`vet`/`fmt`/`-race` clean, live-deployed |
| Runtime (rootfs + WSL/VMware/OVA/QEMU/LXC/native/macOS-Lima) | ✅ eight packaging targets, all release-built |
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
