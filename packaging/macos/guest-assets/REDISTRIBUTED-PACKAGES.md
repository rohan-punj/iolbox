# Redistributed third-party packages in this archive

This archive contains software from the Debian Project in addition to iolbox's
own code. This notice tells you what, under which licenses, and where to get
the corresponding source.

For the full iolbox third-party notice, see `notices/THIRD_PARTY.md`.

## What is redistributed

The `native-arm64` profile runs on Apple Silicon **without** Rosetta, so an
x86_64 IOL binary is translated inside the Lima guest by QEMU's user-mode
emulation. iolbox ships those Debian packages rather than having the guest
download them at provisioning time, so that provisioning works without
depending on Debian's mirrors being reachable — and so that a Debian point
release cannot change what gets installed underneath a pinned guest image.

The packages are redistributed **bit-for-bit unmodified** as published by
Debian. iolbox claims no copyright in them, and has not patched, rebuilt,
trimmed or repackaged them.

They ship inside the linux/arm64 payload in this archive, under:

```
iolbox-server-<version>-linux-arm64.tar.gz
  └── guest-assets/qemu-user/
        arm64/*.deb        the translator (qemu-user, qemu-user-binfmt,
                           qemu-user-static, binfmt-support, libpipeline1)
        amd64/*.deb        the amd64 runtime the translated binary links
                           against (libc6, libgcc-s1, gcc-14-base,
                           libssl3t64, openssl-provider-legacy, libzstd1,
                           zlib1g)
        MANIFEST           every package with version, architecture and SHA-256
        SOURCE-OFFER.txt   the full corresponding-source offer
        notices/           Debian copyright files, one per source package
        notices/licenses/  the license texts those copyright files reference
```

`MANIFEST` is the authoritative record of exactly what shipped.

## Licenses

The packages span eight Debian source packages:

| Source package | Provides | License |
|---|---|---|
| `qemu` | qemu-user, qemu-user-binfmt, qemu-user-static | GPL-2.0, with components under LGPL-2.1 and other compatible licenses |
| `binfmt-support` | binfmt-support | GPL-3.0+ |
| `glibc` | libc6:amd64 | LGPL-2.1+, GPL-2+ |
| `gcc-14` | libgcc-s1:amd64, gcc-14-base:amd64 | GPL-3.0+ with the GCC Runtime Library Exception |
| `openssl` | libssl3t64:amd64, openssl-provider-legacy:amd64 | Apache-2.0 |
| `libzstd` | libzstd1:amd64 | BSD-3-Clause / GPL-2 |
| `zlib` | zlib1g:amd64 | zlib license |
| `libpipeline` | libpipeline1 | GPL-3.0+ |

Per-file copyright attribution is in each package's Debian copyright file, at
`guest-assets/qemu-user/notices/<source>.copyright` inside the payload.

Debian copyright files identify licenses by name and reference their texts at
`/usr/share/common-licenses/`, which exists on an installed Debian system but
not here — so the referenced texts ship in full at
`guest-assets/qemu-user/notices/licenses/`.

## Corresponding source

The complete corresponding source for every redistributed package is published
as a release asset alongside this archive:

```
iolbox-macos-arm64-corresponding-source.tar.gz
```

available from the same iolbox release page this archive came from.

It contains, per source package, the Debian `.dsc`, the upstream `.orig`
tarball and the Debian packaging tarball — everything needed to rebuild these
binaries. The same source is also available directly from Debian; the exact
per-package URLs are listed in `guest-assets/qemu-user/SOURCE-OFFER.txt`
inside the payload, and permanently from <https://snapshot.debian.org/>.

## What is *not* redistributed

- **Lima** is a user-installed prerequisite. iolbox neither ships nor installs it.
- **The Debian guest image** is downloaded by Lima at first run, not embedded here.
- **Cisco IOL/IOU images** are supplied by you. iolbox distributes no Cisco software.
