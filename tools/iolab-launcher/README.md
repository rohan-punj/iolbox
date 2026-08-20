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

## Apple Silicon M4 qualification harness

The opt-in Darwin test and Mac-side orchestrator are intentionally separate
from the normal launcher flow. Build the arm64 launcher and test binary, stage
the launcher, test binary, `packaging/macos/`, and the real executable IOL image
to the target Mac, then run:

```
packaging/macos/tests/hardware-m4.sh --machine iolbox-m4-e2e \
  --launcher ~/iolbox-m1/iolbox-launcher \
  --test-binary ~/iolbox-m1/iolbox-launcher-hardware.test \
  --assets-dir ~/iolbox-m1/packaging/macos \
  --image ~/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin
```

The run creates `~/iolbox-m1/evidence-m4/<run-id>/`. It executes VPCS/IOL,
multi-link capture, NAT, the mechanized extnet disposition, a fresh isolated
ten-minute soak, four-node capacity (including the prescribed RAM-wall retry),
forced recovery, final stop, and the independent record verifier. The soak is
an owner-approved ten-minute reduction of the originally planned long-duration
window and must not be started from a short-lived SSH command: the script keeps its
measurement process attached to the Mac-side run and writes `SOAK-COMPLETE`
only after the capture, metrics, heartbeats, resource rows, power audit, and
hash manifest pass.

The record verifier is platform-neutral and can also be run against a retained
host copy:

```
go test ./tools/iolab-launcher -run TestM4VerifyRecord -args \
  -m4-record <host-evidence-copy>/summary.json
```

All IOL fixtures use at least 1024 MB. `iol22` is rejected by exact-name
guards, stop paths never call `delete`, and positive WebSocket routes reuse
`wsDialWithSession`.

The GUI port is configurable with `IOLBOX_M4_GUI_PORT` or the orchestrator's
`--gui-port` flag. The staged guest verifier is checked before a run and must
use `IOLBOX_GUI_PORT` for both socket and `GET /` readiness; it must not rely
on the default `4001` by coincidence.

## Apple Silicon M5 capability policy

On a qualified arm64 Lima guest, the supervisor drop-in sets
`IOLBOX_DISABLE_I386=1` after the Rosetta loader canary passes. The hello
handshake then omits `i386`; no extra hello architecture field is used. The
GUI uses that one feature signal, so registered ELF32 images remain listed but
cannot be placed or started, while x86_64 images remain available.

Diagnostics keep these predicates separate: live guest architecture, Rosetta
binfmt registration, and a live passing loader canary determine
`execution=rosetta-amd64`; service, HTTP, hello, and capability-policy status
are reported independently. A stopped machine shows unavailable values and
labels host-mirrored evidence as last attested.

## Other flags

Run `iolbox-launcher.exe -h` for the full list (`--backend`, `--mem`, `--smp`,
`--ports`, `--boot-timeout`, `--shutdown-grace`, `--detect`, dev overrides
like `--qemu`/`--disk`/`--wsl-tar`, etc).
