# Building iolbox from source

iolbox has four build outputs:

1. **supervisor** — Go binary, cross-compiled for **linux/amd64** (it runs inside
   the runtime, never on Windows). Built with the browser GUI embedded via
   `build-release.sh` at the repo root — a plain `go build` skips the GUI
   embed step and ships a placeholder ("GUI not bundled in this build").
2. **app** — the browser GUI: Svelte 5 + Vite, served by the supervisor itself
   over HTTP (no separate app process, no native Windows shell). `npm run
   build:embed` builds it straight into `supervisor/internal/web/dist/`,
   which `build-release.sh` then bakes into the Go binary via Go's `embed`
   package.
3. **runtime** — a Debian-slim rootfs packaged six ways (WSL2 import tar,
   VMware appliance, OVA, Proxmox LXC template, native systemd tarball, QEMU
   disk) — see `runtime/README.md` and `runtime/build-all-targets.sh`.
4. **Windows launcher + capture-helper** — two independent, plain Go exes
   (`tools/iolab-launcher`, `tools/capture-helper`) — no Rust/Tauri, no native
   toolchain beyond Go itself.

A legacy native shell exists under `app/src-tauri` (Tauri 2 + Rust), but it
is **not** the shipped product — see `.github/workflows/release.yml`'s notes
on why it was abandoned in favor of the browser-first design above. Don't
build it unless you're specifically reviving that path.

## Prerequisites (Windows dev box)

- Git, Node 20+ (Vite needs a recent Node — 18.19 has been confirmed too old
  to build this repo's frontend), Go 1.26.
- One runtime: **VMware Workstation/Player** (default here) or WSL2.
- Wireshark (for capture) — optional at build time.
- A Linux builder (or WSL/CI) **with root** to bake the runtime rootfs —
  `debootstrap`/`mmdebstrap`, loop devices, chroot, and `grub-install` for
  the disk-image targets. See `runtime/README.md`.
- Rust (MSVC toolchain) + VS C++ Build Tools — only if reviving the legacy
  `app/src-tauri` shell; not needed for anything else below.

## 1. Supervisor (with the embedded GUI)

```
bash build-release.sh
# -> supervisor/bin/supervisor-linux-amd64 (GUI embedded, version-stamped
#    from `git describe --tags`)
```

This is the **only** correct way to produce a deployable supervisor binary —
it runs `npm run build:embed` first and then asserts the embedded
`index.html` isn't the committed placeholder. To build the two pieces
separately (e.g. iterating on just the Go side without rebuilding the GUI
every time):

```
cd app && npm ci && npm run build:embed   # -> supervisor/internal/web/dist/
cd supervisor && go test ./...
GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor
```

## 2. GUI (frontend-only iteration)

```
cd app
npm ci
npm run dev     # mock backend, fast UI iteration at http://localhost:1420
npm run check   # svelte-check
```

`npm run dev` runs against a mock transport by default, not a real
supervisor — no runtime provider needed for pure UI work. To exercise it
against a real backend instead, point a build at a deployed supervisor's
`:4001` (see `docs/INSTALL.md` for how to stand one up).

## 3. Runtime (six packaging targets)

Must run as root on a real Linux box (loop devices, chroot, grub — Windows
can't do this). Feed it the supervisor binary from step 1:

```
cd runtime
sudo ./build-all-targets.sh
# -> build/iolbox-rootfs.tar (WSL), build/iolbox-appliance-*.{vmdk,vmx}
#    (VMware), build/iolbox-appliance-*.ova, build/iolbox-ct-*.tar.zst
#    (Proxmox LXC), build/iolbox-server-*.tar.gz (native), and
#    build/iolbox-disk-*.qcow2 (QEMU) — pass --skip-<target> to build a
#    subset, see build-all-targets.sh -h
```

See `runtime/README.md` and the individual `pack-*.sh` scripts if you only
need one target.

## 4. Windows launcher + capture helper

Both are plain Go exes — no Rust, no Tauri:

```
cd tools/iolab-launcher && go build -o iolbox-launcher.exe .
cd tools/capture-helper && GOOS=windows GOARCH=amd64 go build -o capture-helper.exe .
```

## Where images live

iolbox ships **no** Cisco images. At runtime the supervisor manages its own
local library — via the browser GUI's upload on most targets, or an
`images\` folder next to `iolbox-launcher.exe` (uploaded to the guest on
each launch) for the Windows-launcher/QEMU target, since that target's
runtime disk is ephemeral (see `docs/INSTALL.md` section 6). Nothing
image-related is required to build.
