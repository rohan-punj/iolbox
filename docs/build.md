# Building iolab from source

iolab has three build outputs that fit together:

1. **supervisor** — Go binary, cross-compiled for **linux/amd64** (it runs inside
   the runtime, never on Windows).
2. **runtime** — a Debian-slim rootfs packaged as a WSL2 import tar and/or a VMware
   appliance, with the supervisor baked in and autostarted.
3. **app** — the Tauri (Rust + Svelte) Windows GUI, which bundles `capture-helper`.

## Prerequisites (Windows dev box)

- Git, Node 18+, Go 1.22+ (1.26 tested), Rust stable (MSVC toolchain) + VS C++ Build Tools.
- One runtime: **VMware Workstation/Player** (default here) or WSL2.
- Wireshark (for capture) — optional at build time.
- A Linux builder (or WSL/CI) to bake the runtime rootfs (`debootstrap`).

## 1. Supervisor

```
cd supervisor
go test ./...
GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor
```

## 2. Runtime

Runs on a Linux builder (see `runtime/README.md`). Feeds it the supervisor binary:

```
cd runtime
./build-all.sh ../supervisor/bin/supervisor-linux-amd64
# produces build/iolab-rootfs.tar (WSL) and build/appliance/*.vmx+*.vmdk (VMware)
```

## 3. Capture helper

```
cd tools/capture-helper
GOOS=windows GOARCH=amd64 go build -o ../../app/src-tauri/binaries/capture-helper.exe .
```

## 4. App

```
cd app
npm ci
npm run dev        # frontend-only, mock supervisor — fast UI iteration
npm run tauri dev  # full native app
npm run tauri build
```

## Where images live

iolab ships **no** Cisco images. At runtime the app manages a local library
(default `%APPDATA%\iolab\images`) and syncs images into the runtime on demand.
Nothing image-related is required to build.
