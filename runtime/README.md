# iolab runtime

The **runtime** is the small Linux environment that actually executes IOL and
VPCS. Everything here builds **one** Debian-slim (bookworm) rootfs and then
packages that *same* rootfs two ways:

- a **WSL2 import tarball** (`iolab-rootfs.tar`) for the `wsl2` provider
- a **VMware Workstation/Player appliance** (`iolab-appliance.vmx` + `.vmdk`)
  for the `vmware` provider (the default on machines that already run VMware —
  see `docs/providers.md`)

Both providers run the identical rootfs, so a bug fixed once is fixed for
both. The `qemu-tcg` compatibility provider also reuses the VMware disk image
unmodified — see `qemu-compat.md`.

This directory is **build tooling only**. It produces artifacts under
`runtime/build/` (git-ignored) that get attached to GitHub releases. Nothing
here bakes in Cisco software — the rootfs is empty of images; users supply
their own `.bin`/`.iol` at runtime via the image library (see PLAN.md).

## Why these scripts don't run on this machine

They are authored on Windows but are **Linux build scripts** — they shell out
to `debootstrap`/`mmdebstrap`, `chroot`, `qemu-img`, and friends, none of
which exist here (and WSL is intentionally not installed on this box because
VMware is — see PLAN.md's nested-virt note). Run them on:

- a real Linux box,
- a throwaway Linux VM, or
- CI (GitHub Actions `ubuntu-latest` runner, which has debootstrap-capable
  APT and qemu-img preinstalled or apt-installable).

They all start with `set -euo pipefail`. Every script is `bash -n` clean
(see the syntax-check log noted in the top-level task report) — that only
proves they *parse*, not that they *run*; they still need a real Linux
builder to execute end-to-end.

## Layout

```
runtime/
  README.md              this file
  build-rootfs.sh         debootstrap the base rootfs -> runtime/build/rootfs/
  fetch-vpcs.sh            clone+build community VPCS (UDP-tunnel capable) -> vpcs binary
  pack-wsl.sh              rootfs -> iolab-rootfs.tar (wsl --import format)
  pack-vmware.sh           rootfs -> bootable disk -> vmdk + templated .vmx
  build-all.sh             orchestrator: rootfs -> [pack-wsl, pack-vmware]
  qemu-compat.md           qemu-system-x86_64 (TCG) fallback notes, reuses the vmware disk
  files/
    iolab-supervisor.service   systemd unit, autostarts the supervisor
    iolab-firstboot-iourc.service  systemd oneshot, runs before the above
    iolab-init.sh               fallback SysV-ish /etc/init.d style script (no-systemd path)
    firstboot-iourc.sh          generates iourc from the runtime's own hostid
    wsl.conf                    /etc/wsl.conf for the WSL2 import
    01-no-default-route.network  systemd-networkd drop-in: iface up, no gateway
    99-iolab.conf                sysctl drop-in (ip_forward off, etc — see file)
    iolab-appliance.vmx.tmpl    templated VMware VMX (placeholders substituted by pack-vmware.sh)
  build/                  OUTPUT (git-ignored) — rootfs/, *.tar, *.vmdk, *.vmx land here
```

## The one rootfs, two packages

`build-rootfs.sh` is the only script that touches `debootstrap`/APT. It
produces `runtime/build/rootfs/`, a directory tree that IS the runtime
filesystem — no compression, no partition table yet. Everything downstream
(`pack-wsl.sh`, `pack-vmware.sh`) consumes that same directory read-only-ish
(pack-vmware.sh copies it onto a disk image; pack-wsl.sh just tars it).

Rebuilding only one package (e.g. you changed something WSL-specific) does
**not** require re-running `build-rootfs.sh` — just re-run the one pack
script, as long as `runtime/build/rootfs/` still exists from a prior run.

## Airgap / no-default-route rationale

This is the fix for the `xml.cisco.com` L2-IOL abort documented in
`docs/providers.md` and previously hit on the PNetLab Noble port (see
`l2-iol-xml-cisco-nullroute` memory topic): L2 IOL images probe a
Cisco-owned hostname at boot and **abort** if that host is reachable-but-
rejecting (e.g. null-routed / RST). The fix used here is the same one
adopted on PNetLab: don't give the runtime a default route at all, so the
lookup/connect just times out / "network unreachable"s immediately instead
of getting an explicit rejection. Loopback and the single host-only/link
interface (with its own /24 or DHCP'd address) are configured; no
`ip route add default ...` anywhere in this rootfs. See
`files/01-no-default-route.network` and the network setup step inside
`build-rootfs.sh`.

This also happens to be correct for the product's "localhost-only, no
telemetry" posture — the runtime has no business making outbound
connections at all.

## The supervisor binary

The supervisor is built **separately** by the Go team at `supervisor/` (see
PLAN.md's repo layout) with:

```sh
cd supervisor && GOOS=linux GOARCH=amd64 go build -o bin/supervisor-linux-amd64 ./cmd/supervisor
```

`build-rootfs.sh` copies that binary into the rootfs at
`/opt/iolab/supervisor` (mode 0755, root:root). The input path is a script
parameter defaulting to `../supervisor/bin/supervisor-linux-amd64` (relative
to `runtime/`), so:

```sh
./build-rootfs.sh                                   # uses the default path
./build-rootfs.sh --supervisor-bin /path/to/binary   # explicit override
```

If the binary isn't present at build time, `build-rootfs.sh` **fails fast**
with a clear message rather than shipping a rootfs with no supervisor — the
whole point of this runtime is to autostart it.

### Cross-team assumption: `-gen-iourc`

`files/firstboot-iourc.sh` (run once, before the supervisor unit, by
`iolab-firstboot-iourc.service`) assumes the supervisor binary supports a
**keygen-only mode**:

```sh
/opt/iolab/supervisor -gen-iourc > /opt/iolab/iourc
```

i.e. when invoked with `-gen-iourc`, the supervisor reads the runtime's own
`hostid`/hostname (whatever the stock IOL keygen algorithm needs — the same
algorithm already implemented for the PNetLab port, see
`pnetlab-deb-port-project` memory topic for the UKSM/keygen facts), prints a
valid `iourc` file to **stdout**, and exits 0 — it does **not** start the
control-plane listener in this mode. This is a coordination point with the
supervisor team: if the real flag name/behavior differs, update
`files/firstboot-iourc.sh` and this paragraph together. The generated file
is written to `/opt/iolab/iourc` and is **not** baked into the image (it's
regenerated fresh on every first boot of every VM instance — see
`files/firstboot-iourc.sh` for the "only run once" guard using a marker
file at `/opt/iolab/.iourc-generated`).

## Sizing

Approximate, see each script's header comment for the breakdown:

- rootfs (uncompressed dir tree): **~180-260 MB** (debootstrap base ~120 MB +
  i386 multiarch libs ~20-40 MB + VPCS binary ~1 MB + supervisor binary
  ~10-15 MB static Go binary + iproute2/openssh-less minimal set). Docs/locale
  stripping claws back roughly 30-50 MB versus an unstripped debootstrap.
  The <150 MB target in the task brief is tight once `libc6:i386` is added;
  see `build-rootfs.sh`'s header comment for the honest breakdown and the
  knobs to hit it (drop `libc6:i386` entirely if only 64-bit IOL images are
  in play; it's the single biggest line item after base debootstrap).
- `iolab-rootfs.tar` (pack-wsl.sh output, uncompressed per `wsl --import`
  convention): roughly equal to the rootfs dir size, **~180-260 MB**.
- `iolab-appliance.vmdk` (pack-vmware.sh output, growable/sparse vmdk):
  allocated 4 GB virtual, but **actual bytes on disk are close to the rootfs
  size plus kernel/initrd/grub**, typically **~250-350 MB** sparse. VMware's
  monolithicSparse format only consumes space for written blocks.

## Quick start (on a Linux builder)

```sh
cd runtime
./build-all.sh --supervisor-bin /path/to/supervisor-linux-amd64
# -> runtime/build/iolab-rootfs.tar
# -> runtime/build/iolab-appliance.vmdk + iolab-appliance.vmx
```

Windows side, WSL2 provider:

```powershell
wsl --import iolab C:\Users\<you>\iolab-wsl runtime\build\iolab-rootfs.tar
wsl -d iolab -- /opt/iolab/supervisor -control-addr 127.0.0.1:4000 &  # normally systemd does this
```

Windows side, VMware provider: copy `runtime/build/iolab-appliance.vmx` +
`.vmdk` next to each other, then:

```powershell
vmrun -T ws start "iolab-appliance.vmx" nogui
vmrun -T ws getGuestIPAddress "iolab-appliance.vmx"   # sanity check; fixed IP is 192.168.171.2 by default
```

See `pack-vmware.sh` and `qemu-compat.md` for the fixed host-only IP
rationale and the QEMU-TCG fallback.
