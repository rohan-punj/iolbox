# Third-party software distributed with iolbox

iolbox bundles the following third-party components. Their licenses are their
own; this file records what we ship, from where, pinned to an exact version and
checksum, and how we comply with each license.

---

## Apple Silicon macOS archive

The `iolbox-macos-arm64.tar.gz` archive contains the Go `iolbox` launcher,
**two** Linux payloads produced by `runtime/pack-native.sh` — one linux/amd64
and one linux/arm64 — the locked Lima profile/provisioner files, and this
notice. It contains no guest disk, hypervisor, Lima installation, or Cisco
software.

The launcher selects one payload from the profile it resolves: the
linux/arm64 payload for the `native-arm64` profile, the linux/amd64 payload
(run under Rosetta) for the `debian13`/`jammy`/`debian12` profiles.

### Separately installed and fetched components

- Lima is an Apache-2.0 licensed, user-installed prerequisite. iolbox neither
  redistributes nor installs, upgrades, or removes Lima.
- Lima downloads the digest-locked Debian 13/trixie default, Debian 13/trixie
  native-arm64, or Ubuntu 22.04/Jammy compatibility guest image. The guest
  image, apt package indexes, and guest packages are fetched at provisioning
  time and are not embedded in the archive.
- Users supply any legally held IOL/IOU image. iolbox does not distribute or
  depend on Cisco software. **x86_64 IOL is the only supported IOL
  architecture on every profile in this archive**; i386/i86bi and
  arm64-native IOL are unsupported. On the Rosetta profiles x86_64 IOL is
  translated by Apple's Rosetta; on `native-arm64` it is translated by
  qemu-user inside the guest (see the QEMU boundary note below).

### VPCS binary redistributed inside the payloads

`runtime/fetch-vpcs.sh` checks out the GNS3 VPCS project at the immutable
`v0.8.3` ref (release commit `3870ae8`) and builds it; `runtime/pack-native.sh`
places the resulting binary at `bin/vpcs` in the payload. Both payloads are
built from that same pinned ref — the linux/amd64 binary natively on the
builder, the linux/arm64 binary with `aarch64-linux-gnu-gcc` via
`fetch-vpcs.sh --arch arm64`. The upstream project is BSD-2-Clause licensed:

- Source repository: <https://github.com/GNS3/vpcs>
- Pinned source archive: <https://github.com/GNS3/vpcs/archive/refs/tags/v0.8.3.tar.gz>
- Pinned release: <https://github.com/GNS3/vpcs/releases/tag/v0.8.3>
- License: BSD-2-Clause, as identified by the upstream repository

The archive redistributes the resulting binaries, not a moving branch checkout.
The source URL and ref above are the corresponding source offer for these
versions; the build recipe and compiler/link flags are recorded in
`runtime/fetch-vpcs.sh`.

### QEMU and the Apple Silicon archive — what is and is not distributed

**No QEMU binary is contained in `iolbox-macos-arm64.tar.gz`.** The QEMU
section further below covers the separate Windows launcher bundle, where we
do redistribute QEMU binaries, and must not be read as a claim that the Mac
artifact contains QEMU.

However, selecting the **`native-arm64`** profile *causes* QEMU to be
installed on the user's machine, and that is worth stating plainly rather
than leaving to the reader. `packaging/macos/guest/10-multiarch-native.sh`
runs, inside the Lima guest:

```sh
apt-get install -y --no-install-recommends \
    qemu-user-static binfmt-support libc6:amd64 libssl3t64:amd64
```

so that x86_64 IOL can be translated in-guest on a profile that deliberately
has no Rosetta. `qemu-user-static` is GPL-licensed (QEMU is GPL-2.0, with
components under LGPL-2.1 and other compatible licenses); `binfmt-support` is
likewise GPL-licensed.

Boundary, stated explicitly:

- iolbox does **not** redistribute `qemu-user-static` or `binfmt-support`.
  They are fetched at provisioning time by the guest's own `apt`, from
  Debian's repositories, under Debian's packaging and copyright files — the
  same boundary this notice already draws for the guest image itself and for
  every other guest package.
- The distributor of those binaries is Debian, and Debian's corresponding
  source is available from <https://sources.debian.org/> and via
  `apt-get source qemu-user-static` on the guest. The exact versions selected
  in qualification were `qemu-user-static 1:10.0.11+ds-0+deb13u1` and
  `binfmt-support 2.2.2-7+b1`.
- No QEMU or binfmt-support binary, source, or copyright file is embedded in
  any iolbox artifact for macOS.

> **Open prerequisite, owner action required.** The M7 translator-selection
> record sets `redistribution_review_required: true`, and no record shows
> that review was performed (ledger row **P7-02**, `docs/macos-m7-result.md`).
> The paragraphs above correct a notice that had become misleading; they are
> **not** a substitute for that review and do not discharge it. It should be
> closed before the first tag that ships the `native-arm64` profile.

---

## QEMU (Windows build) — the `qemu` compatibility backend

The Windows launcher (`tools/iolab-launcher`) ships a trimmed copy of QEMU so the
`qemu` backend (software-emulated TCG, see `docs/providers.md` and
`runtime/qemu-compat.md`) works on any Windows machine with zero prerequisites.

### Pinned source

| Field | Value |
|---|---|
| Upstream | Stefan Weil's official QEMU-for-Windows builds — <https://qemu.weilnetz.de/w64/> |
| File | `qemu-w64-setup-20260501.exe` |
| Full URL | <https://qemu.weilnetz.de/w64/qemu-w64-setup-20260501.exe> |
| QEMU version | 11.0.0 release build (reports `11.0.50 (v11.0.0-12631-g54e84cdc7a)`) |
| Installer size | 190 MB (198,760,632 bytes) |
| **SHA-256 (installer, verified by download)** | `a8b29572afb4c6ad024b7de129c81033e9fd191b9e054e3a52ea0bed24ac19ef` |

`qemu.weilnetz.de` is the canonical upstream-linked Windows build (referenced
from qemu.org's download page). Note: recent installers are Authenticode-signed
with an **expired certificate** — Windows SmartScreen may warn on the installer.
This does not affect the extracted binaries we redistribute (we extract, we do
not run the installer on user machines).

### How the redistributable is produced (extraction, not install)

The upstream `.exe` is an NSIS installer. We do **not** run it on user machines;
we extract it once and ship a trimmed tree:

```sh
# 1. verify the pinned installer
sha256sum qemu-w64-setup-20260501.exe
#   -> a8b29572afb4c6ad024b7de129c81033e9fd191b9e054e3a52ea0bed24ac19ef

# 2. extract with 7-Zip (NSIS archive)
"C:\Program Files\7-Zip\7z.exe" x -oqemu-extract qemu-w64-setup-20260501.exe

# 3. trim to the headless x86_64 target:
#    - keep  qemu-system-x86_64.exe  + ALL *.dll (the runtime DLL set)
#    - keep  share/keymaps/  and the x86/i386 firmware blobs
#            (bios*.bin, vgabios*.bin, kvmvapic.bin, linuxboot*/multiboot*/pvh,
#             *-virtio.rom + pxe-*.rom, edk2-i386/x86_64 *.fd)
#    - drop  the other 55 qemu-system-*.exe targets, share/doc, and the
#            non-x86 firmware (openbios-*, edk2-aarch64/arm, dtb, ...)
```

Result: a `qemu/` directory of ~175 MB. Verified standalone on a clean box:
`qemu-system-x86_64.exe -version` and `-accel help` (lists `tcg` + `whpx`) both
run, so the DLL set is complete.

### Layout shipped next to the launcher exe

```
iolbox-launcher.exe
iolbox-disk.qcow2            (the runtime disk — a separate release asset, see below)
qemu/
  qemu-system-x86_64.exe
  *.dll                     (the runtime DLLs)
  share/
    keymaps/
    bios*.bin, vgabios*.bin, kvmvapic.bin, linuxboot*.bin, ...
```

The launcher looks for `qemu/qemu-system-x86_64.exe` and `iolbox-disk.qcow2`
relative to its own exe (`--qemu` / `--disk` override for dev). QEMU resolves its
BIOS/keymap data from the `share/` dir next to its exe automatically.

The `qemu/` directory and `*.qcow2` are **git-ignored** (see
`tools/iolab-launcher/.gitignore`) — they are release-time assets staged into the
bundle, never committed to the repo.

### License / GPL compliance

QEMU is licensed under the **GNU GPL v2** (with some components under LGPL v2.1
and other compatible licenses). We redistribute unmodified upstream binaries.

- The upstream binaries carry their license texts (`COPYING`, `COPYING.LIB`) in
  the installer; include a copy of `COPYING`/`COPYING.LIB` in the shipped `qemu/`
  directory (or link to them from the release notes) when assembling a release.
- **Written offer for source:** the corresponding source for this exact build is
  published by the upstream packager at the QEMU source page,
  <https://www.qemu.org/download/#source> (release 11.0.0), mirrored from
  <https://qemu.weilnetz.de/>. iolbox distributes QEMU **unmodified**; the source
  offer therefore points at upstream. Include this URL in the release notes /
  About page alongside the version above so the offer travels with the binary.

### The iolbox runtime disk (`iolbox-disk.qcow2`)

Not third-party — it is built from `runtime/` (Debian-slim rootfs + the iolbox
supervisor) by `runtime/pack-qemu.sh`. It contains **no Cisco software**; IOL
images are supplied by the user at runtime via the GUI. Debian's own components
inside the rootfs are under their respective licenses (Debian is DFSG-free);
this is the same rootfs the WSL2 and VMware artifacts ship, already covered by
the runtime build.
