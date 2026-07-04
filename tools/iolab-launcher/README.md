# iolbox-launcher

A single Windows exe that boots the iolbox Linux guest and gets a user to
`http://localhost:4001`. Picks a backend automatically:

- **qemu** — bundled `qemu-system-x86_64.exe` (TCG software emulation). Works
  everywhere, conflicts with nothing, needs no admin. The fallback.
- **wsl** — the `iolbox` WSL2 distro. Fastest, but only usable when an active
  Windows hypervisor is present (see `docs/providers.md`).

Build (dependency-free, stdlib only):

```
cd tools/iolab-launcher
GOOS=windows GOARCH=amd64 go build -o iolbox-launcher.exe .
```

## Ephemeral OS disk

The qemu backend attaches `iolbox-disk.qcow2` with `snapshot=on`. QEMU opens
the golden disk **read-only** and redirects every guest write to a temporary
overlay file that is discarded when qemu exits. That means:

- A hard kill (task-kill, crash, power loss) can **never corrupt** the shipped
  disk image.
- Every launch boots from the exact same clean state.
- Nothing written to the guest's root filesystem persists between runs.

(If qemu is hard-killed before it can clean up, its temp overlay file may be
orphaned in `%TMP%` — this is harmless scratch space; Windows/the user can
clean `%TMP%` at any time.)

The WSL backend has no qcow2 — its distro filesystem already persists on the
Windows FS across runs, so this concern doesn't apply there.

## images\ and labs\ folders

Because the OS disk is ephemeral, user data lives on the Windows filesystem
instead, next to the launcher exe:

- **`images\`** — drop `.bin`/`.iol` IOL image files here. Each launch, every
  image in this folder is uploaded to the guest and registered into the image
  registry, so it shows up in the GUI's image picker. Images are re-synced
  (re-uploaded + re-registered) on **every** launch, since the guest's
  registry doesn't persist either.
- **`labs\`** — saved labs persist here as `<labId>.json`. On launch, every
  `.json` file here is pushed into the guest. While iolbox is running, any lab
  you create or edit in the GUI is written back to this folder automatically
  (every 30 seconds, and once more on shutdown) — so your work is never lost
  when the guest disk resets.

Both folders are created automatically on first launch if missing; their
absolute paths are logged at startup.

The guest ships with its own built-in starter labs, re-seeded every boot.
These are never written into `labs\` and are never overwritten by a
same-named file you place there.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `--images-dir <path>` | `<exeDir>\images` | override the images folder |
| `--labs-dir <path>` | `<exeDir>\labs` | override the labs folder |
| `--no-sync` | off | disable all folder sync (pure ephemeral; folders are left untouched) |

Sync failures (e.g. the control channel isn't reachable yet) are logged as
warnings and never fail the launch — the GUI works with or without sync.

## Other flags

Run `iolbox-launcher.exe -h` for the full list (`--backend`, `--mem`, `--smp`,
`--ports`, `--boot-timeout`, `--shutdown-grace`, `--detect`, dev overrides
like `--qemu`/`--disk`/`--wsl-tar`, etc).
