# Third-party software distributed with iolbox

iolbox bundles the following third-party components. Their licenses are their
own; this file records what we ship, from where, pinned to an exact version and
checksum, and how we comply with each license.

---

## Apple Silicon macOS archive

The `iolbox-macos-arm64.tar.gz` archive contains the Go `iolbox` launcher,
**two** Linux payloads produced by `runtime/pack-native.sh` — one linux/amd64
and one linux/arm64 — the locked Lima profile/provisioner files, this notice,
and (inside the linux/arm64 payload) the redistributed Debian translator
packages described below. It contains no guest disk, hypervisor, Lima
installation, or Cisco software.

The launcher selects one payload from the profile it resolves: the
linux/arm64 payload for the `native-arm64` profile, the linux/amd64 payload
(run under Rosetta) for the `debian13`/`jammy`/`debian12` profiles.

### Separately installed and fetched components

- Lima is an Apache-2.0 licensed, user-installed prerequisite. iolbox neither
  redistributes nor installs, upgrades, or removes Lima.
- Lima downloads the digest-locked Debian 13/trixie default, Debian 13/trixie
  native-arm64, or Ubuntu 22.04/Jammy compatibility guest image. The guest
  image, apt package indexes, and guest packages are fetched at provisioning
  time and are not embedded in the archive — **with one deliberate exception**:
  the `native-arm64` translator package set (`qemu-user` and friends) IS
  embedded and redistributed. See "QEMU and the Apple Silicon archive" below.
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

### QEMU and the Apple Silicon archive — redistributed, deliberately

**`iolbox-macos-arm64.tar.gz` redistributes QEMU.** This reverses the earlier
boundary, under which the guest's own `apt` fetched the translator at
provisioning time and iolbox redistributed nothing. The owner's decision is
that iolbox should ship these packages itself: iolbox is a learner tool, and
the archive working self-contained outranks minimizing what we ship.

It also removes a real failure mode rather than only a theoretical one. The
`native-arm64` guest image is digest-pinned, but the old `apt-get install`
resolved against live trixie — so a Debian point release could move `libc6`
underneath a pinned image at any time and change, or break, what a student
got. Pinning the packages removes that.

The `native-arm64` profile has no Rosetta by design, so x86_64 IOL is
translated in-guest by qemu-user. `packaging/macos/guest/10-multiarch-native.sh`
now installs that translator from bundled `.deb` files with `dpkg`, with **no
`apt-get` and no network fallback** — enforced by
`packaging/macos/tests/qemu-bundle-test.sh`, not merely intended.

#### What ships

Twelve Debian binary packages, redistributed **bit-for-bit unmodified**, inside
the linux/arm64 payload under `guest-assets/qemu-user/`:

| | Packages |
|---|---|
| Translator (arm64) | `qemu-user`, `qemu-user-binfmt`, `qemu-user-static`, `binfmt-support`, `libpipeline1` |
| amd64 runtime | `libc6`, `libgcc-s1`, `gcc-14-base`, `libssl3t64`, `openssl-provider-legacy`, `libzstd1`, `zlib1g` |

Roughly 67 MiB of `.deb` files. Installed guest footprint is **unchanged** —
the previous apt path installed the same packages.

> **`qemu-user-static` is a transitional package in trixie** (`Section:
> oldlibs`) containing no emulator at all — only compat symlinks. The actual
> emulator binaries are in `qemu-user`, and the binfmt registration files in
> `qemu-user-binfmt`. The M7 record (P7-02) names only `qemu-user-static` and
> `binfmt-support` because those were the two packages *requested*; `apt`
> resolved the rest silently. A bundle built from those two names alone would
> ship no working translator. The amd64 entries beyond `libc6`/`libssl3t64`
> are likewise closure that `apt` used to resolve and an offline `dpkg` will
> not.

#### Pinning and provenance

Pinned in `packaging/macos/guest-assets/qemu-user.lock`, regenerated by
`generate-lock.sh`, which derives every hash from Debian's **OpenPGP-signed**
archive metadata:

```
InRelease (signed by the Debian archive key)
  → sha256 of main/binary-<arch>/Packages.xz, taken from InRelease
    → sha256 of each .deb, taken from the verified Packages index
```

A hand-committed checksum would only prove the download had not changed since
someone wrote it down — not that the original bytes were authentic Debian
packages. Since `.deb` maintainer scripts run as **root** in the guest, that
distinction matters.

The lock is pinned to a `snapshot.debian.org` timestamp matching the **build
date of the pinned Lima guest image**. That is not cosmetic: `libc6`,
`libgcc-s1`, `gcc-14-base`, `libssl3t64`, `libzstd1` and `zlib1g` are all
`Multi-Arch: same`, which Debian requires to be co-installed at **identical**
versions across architectures. Pinning to the image's own date makes the amd64
packages we ship match the arm64 ones the guest already has, by construction.
Moving the image pin without regenerating the lock will break installation;
the guest script detects that case and says so explicitly.

Selected versions:
`qemu-user`/`qemu-user-binfmt`/`qemu-user-static` `1:10.0.11+ds-0+deb13u1`,
`binfmt-support` `2.2.2-7+b1` — the same versions M7 qualification selected.

#### License / GPL compliance

QEMU is **GPL-2.0** (components under LGPL-2.1 and other compatible licenses);
`binfmt-support` and `libpipeline1` are **GPL-3.0+**; `glibc` is LGPL-2.1+/GPL-2+;
`gcc-14` is GPL-3.0+ with the GCC Runtime Library Exception; `openssl` is
Apache-2.0; `zlib` and `libzstd` are permissive. Eight Debian source packages
in total. All are redistributed unmodified.

- **License texts are included.** For each source package, Debian's DEP-5
  `copyright` file ships at `guest-assets/qemu-user/notices/<source>.copyright`,
  extracted from the pinned `.deb` at build time so it cannot drift from what
  actually shipped.

  DEP-5 copyright files *reference* license texts at
  `/usr/share/common-licenses/` rather than embedding them — which resolves on
  an installed Debian system because `base-files` provides that directory, and
  resolves to nothing in an archive. So the referenced texts ship in full at
  `guest-assets/qemu-user/notices/licenses/`. The build fails if a copyright
  file references a text that was not shipped.

- **Corresponding source accompanies the binaries.** GPLv2 §3 and GPLv3 §6 both
  permit discharging the source obligation by accompanying the binaries with
  source; the written-offer alternatives carry extra conditions (GPLv2 §3(b)
  requires a three-year offer valid to any third party, and §3(c)'s
  pass-through is limited to noncommercial redistribution). iolbox therefore
  **publishes the source itself**, as the release asset
  `iolbox-macos-arm64-corresponding-source.tar.gz`, from the same release page
  as the archive — rather than relying on a pointer to Debian's archive.

  It is assembled by `packaging/macos/guest-assets/fetch-corresponding-source.sh`
  from `sources.lock`, and contains the `.dsc`, upstream `.orig` tarball and
  Debian packaging tarball for every source package. Per-package Debian URLs
  are also listed in `guest-assets/qemu-user/SOURCE-OFFER.txt`.

  Source versions are read from the verified index, never from listing a pool
  directory: pool directories hold many versions at once, and binary version
  ≠ source version for binNMUs (`binfmt-support` binary `2.2.2-7+b1` ← source
  `2.2.2-7`) and renamed sources (`libc6` ← `glibc`, `libgcc-s1` ← `gcc-14`).

- **Attribution** is carried by the per-package copyright files; iolbox claims
  no copyright in these packages and has not patched, rebuilt, trimmed or
  repackaged them.

A user-facing summary of all of the above ships at archive top level as
`notices/REDISTRIBUTED-PACKAGES.md`.

#### Scope of the offline claim

Bundling makes **package installation** offline. It does **not** make
`native-arm64` offline end-to-end: Lima still downloads the digest-locked
Debian cloud image on first run. The accurate claim is that once the guest
image is present, provisioning no longer depends on Debian's mirrors.

> **P7-02 resolved.** The M7 translator-selection record set
> `redistribution_review_required: true` (ledger row **P7-02**,
> `docs/macos-m7-result.md`), and no record showed that review had been
> performed. It has now been performed. The outcome was a decision **to
> redistribute deliberately, with full compliance**, rather than to rely on
> the provisioning-time boundary argument — which is what this section, the
> pinned lock, the shipped notices and the published corresponding source
> implement. Design record:
> `docs/macos-native-arm64-qemu-redistribution-plan.md`.
>
> One question is explicitly **not** settled by engineering and is recorded
> for the owner: whether the chosen mechanism (accompanying source published
> from the same distribution point) is the form of compliance the project
> wants to stand behind long-term, versus also maintaining a standing
> three-year written offer. Shipping source alongside the binary is the
> stronger of the two common options and is what is implemented; adding a
> formal written offer is a policy choice, not a technical gap.

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
