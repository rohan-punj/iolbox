# iolbox runtime

The **runtime** is the small Linux environment that actually executes IOL and
VPCS. Everything here builds **one** Debian-slim (bookworm) rootfs and then
packages that *same* rootfs six ways:

- a **WSL2 import tarball** (`iolbox-rootfs.tar`) for the `wsl2` provider
- a **VMware Workstation/Player appliance** (`iolbox-appliance-*.vmx` +
  `.vmdk`) for the `vmware` provider (the default on machines that already
  run VMware — see `docs/providers.md`)
- an **OVA** (`iolbox-appliance-*.ova`) — the same appliance as a single
  standard-format file for ESXi/VirtualBox/generic OVF import
- a **Proxmox LXC template** (`iolbox-ct-*.tar.zst`)
- a **native systemd tarball** (`iolbox-server-*.tar.gz`) for "bring your own
  Linux box"
- a **QEMU disk** (`iolbox-disk-*.qcow2`) for the bundled Windows launcher
  (`tools/iolab-launcher`, the `qemu` provider's zero-prerequisite fallback)

Every target runs the identical rootfs, so a bug fixed once is fixed for
all six — see `runtime/build-all-targets.sh`, which wraps the whole pipeline
end to end.

This directory is **build tooling only**. It produces artifacts under
`runtime/build/` (git-ignored) that get attached to GitHub releases. Nothing
here bakes in Cisco software — the rootfs is empty of images; users supply
their own `.bin`/`.iol` at runtime via the GUI's image upload.

The configuration baked in here mirrors the **proven reference runtime**
(the hand-built `iolbox-rt` Ubuntu VM every product feature was validated
on): same supervisor flags, same `/etc/hosts` hostname line, same stale-
device cleanup, same two-NIC (host-only + NAT) shape.

## Why these scripts don't run on this (Windows) machine

They are authored on Windows but are **Linux build scripts** — they shell out
to `debootstrap`/`mmdebstrap`, `chroot`, `qemu-img`, and friends. Run them on:

- a real Linux box or VM (the iolbox-rt reference VM works: ~5 GB free disk
  needed),
- CI (GitHub Actions `ubuntu-latest`, which can apt-install everything).

Builder prerequisites (Debian/Ubuntu):

```sh
sudo apt-get install debootstrap qemu-utils parted e2fsprogs rsync \
                     build-essential git
```

## Layout

```
runtime/
  README.md               this file
  REDEPLOY.md              per-target redeploy/upgrade steps for a built artifact
  qemu-compat.md            qemu-system-x86_64 (TCG) fallback notes
  resources.env             single source of truth for vCPU/RAM across every target
  build-rootfs.sh           debootstrap the base rootfs -> runtime/build/rootfs/
  fetch-vpcs.sh             clone+build GNS3 vpcs (static link) -> vpcs binary
  lib-disk.sh               shared disk-build code (partition/loop/mkfs/chroot/grub)
                             consumed by pack-vmware.sh, pack-ova.sh, pack-qemu.sh
  pack-wsl.sh               rootfs -> iolbox-rootfs.tar (wsl --import format)
  pack-vmware.sh            rootfs -> bootable GPT disk -> vmdk + templated .vmx
  pack-ova.sh               rootfs -> bootable GPT disk -> streamOptimized OVA
  pack-lxc.sh               rootfs -> Proxmox LXC template tarball (.tar.zst)
  pack-native.sh            supervisor+vpcs+files/native -> systemd install tarball
  pack-qemu.sh              rootfs -> compressed qcow2 (--no-vmtools --no-nic-configs)
  build-all.sh              orchestrator: rootfs -> [pack-wsl, pack-vmware] (legacy;
                             prefer build-all-targets.sh for a full release build)
  build-all-targets.sh      orchestrator: repo-root build-release.sh -> build-all.sh
                             -> pack-ova/pack-lxc/pack-native/pack-qemu, all 6 targets
  files/
    iolbox-supervisor.service      systemd unit, autostarts the supervisor
    iolbox-firstboot-iourc.service systemd oneshot, runs before the above
    iolbox-dropbear.service        minimal SSH server (redeploy/debug access)
    dropbear-keygen.sh             host-key generation for the above
    prestart-clean.sh             ExecStartPre: stale tap devices + run-dir sweep
    firstboot-iourc.sh            generates iourc from the runtime's own hostid
    iolbox-init.sh                 non-systemd fallback init script
    wsl.conf                      /etc/wsl.conf for the WSL2 import (pins hostname!)
    80-ethernet-dhcp.network      networkd fallback: plain DHCP on en*
    99-iolbox.conf                 sysctl drop-in (ip_forward off until NAT node starts)
    iolbox-appliance.vmx.tmpl      templated VMware VMX (4 vCPU / 4 GB / 2 NICs)
    console/                       pre-login console-banner unit (GUI URL + status)
    lxc/                           Proxmox-specific units + pct-create.md recipe
    native/                        systemd install/uninstall scripts for pack-native.sh
    ova/                           templated OVF descriptor for pack-ova.sh
    tools/                         tool-node packs (aaa/webserver/pc/...) baked into rootfs
  build/                   OUTPUT (git-ignored)
```

## The one rootfs, six packages

`build-rootfs.sh` is the only script that touches `debootstrap`/APT. It
produces `runtime/build/rootfs/`, a directory tree that IS the runtime
filesystem. Every `pack-*.sh` script downstream consumes that same
directory:

- `pack-wsl.sh` just tars it (`iolbox-rootfs.tar`).
- `pack-native.sh` doesn't touch the rootfs at all — it packages the
  supervisor/vpcs binaries plus `files/native/` directly, since a native
  install runs on the operator's own distro.
- `pack-vmware.sh`, `pack-ova.sh`, and `pack-qemu.sh` all copy the rootfs
  onto a disk image via the shared `lib-disk.sh` (partition → loop → mkfs →
  rsync rootfs → chroot kernel/GRUB → per-target network configs), then
  diverge only in the final container format (vmdk+vmx / streamOptimized
  OVA / compressed qcow2) and a few profile flags (`pack-qemu.sh` always
  builds with `--no-vmtools --no-nic-configs --zerofree`, since the QEMU
  provider has no guest-IP discovery to power and gets its NIC MAC from
  QEMU itself, not a baked networkd config).
- `pack-lxc.sh` also skips the disk-image path — a Proxmox container shares
  the host kernel, so it just tars the rootfs with LXC-specific unit
  overrides from `files/lxc/`.

Rebuilding only one package does **not** require re-running
`build-rootfs.sh` — just re-run the one pack script. The three disk-image
scripts (`pack-vmware.sh`/`pack-ova.sh`/`pack-qemu.sh`) need root (loop
devices, chroot, `grub-install`); `pack-wsl.sh`/`pack-lxc.sh`/`pack-native.sh`
don't, but are harmless to run as root too.

## Resource sizing (vCPU / RAM)

`runtime/resources.env` is the single place to change vCPU/RAM for every
deployment target: `IOLBOX_VCPUS` (default 4) and `IOLBOX_RAM_MB` (default
4096). `pack-vmware.sh` and `pack-ova.sh` both source it at build time and
substitute the values into the `.vmx` / OVF descriptor. The Proxmox LXC
container reads it via the `pct create` recipe (`files/lxc/pct-create.md`
— `--cores`/`--memory`, editable live after creation with `pct set`).
Docker reads `docker/.env` (kept in sync with this file). The Windows QEMU
launcher (`tools/iolab-launcher`) isn't built from this file — its
`--smp`/`--mem` flag defaults are hardcoded in `main.go` but kept in sync
by hand, and remain overridable per-run regardless.

## Networking (and why there IS a default route now)

The appliance has **two NICs**, same as the reference VM:

- **host-only** (fixed MAC `00:50:56:3f:ab:01`): the control plane. The
  Windows side reaches the GUI/WS bridge (`:4001`), native telnet consoles
  and Wireshark capture tees over this segment. DHCP — every Workstation
  install picks its own host-only subnet, so a baked static IP would be
  wrong everywhere except the machine that built the image. Discovery:
  `vmrun getGuestIPAddress` (open-vm-tools is installed), or grep the fixed
  MAC in `C:\ProgramData\VMware\vmnetdhcp.leases`.
- **NAT** (fixed MAC `00:50:56:3f:ab:02`): the **NAT node's** outbound
  path — lab traffic is MASQUERADEd out through the default route this NIC
  provides. Also handy for maintainer `apt` inside the appliance (put a
  nameserver in `/etc/resolv.conf` first; it ships empty).

An earlier iteration of these scripts enforced a strict no-default-route
"airgap" posture to dodge the `xml.cisco.com` L2-IOL boot abort seen on
PNetLab. That abort triggers on *reachable-but-rejecting* (null-route/RST),
not on genuine internet, and the reference VM has run L2 IOL 17.18 behind a
real default route throughout — so the posture was retired when the NAT
node made outbound a product feature.

## The supervisor binary

Build it with the repo's **`build-release.sh`** (repo root), never a plain
`go build` — the plain build ships a placeholder GUI instead of the real
embedded Svelte bundle:

```sh
bash build-release.sh    # -> supervisor/bin/supervisor-linux-amd64
```

`build-rootfs.sh` copies that binary to `/opt/iolbox/supervisor` and fails
fast if it's missing.

### First boot: iourc

`files/firstboot-iourc.sh` runs once (before the supervisor unit) and calls
`supervisor -gen-iourc` to mint `/opt/iolbox/iourc` from the runtime's own
hostid + hostname. The hostname is pinned to `iolbox` in **three** places
that must stay in sync: `/etc/hostname`, the `127.0.1.1 iolbox` line in
`/etc/hosts` (without which every `sudo` the NAT node runs stalls ~10 s on
DNS), and `wsl.conf`'s `hostname=iolbox` (without which WSL2 uses the
Windows hostname and IOL rejects the license).

## Debugging a built appliance

- Root console login: user `root`, password `iolbox`. Open the VM in the
  Workstation UI instead of `nogui` if you need the console.
- SSH: `ssh root@<vm-ip>` (same password), served by dropbear
  (`iolbox-dropbear.service`) — a ~200 KB SSH server, not openssh-server.
  This is the primary way to push a rebuilt binary/redeploy scripts onto a
  running appliance; VM guest automation (`vmrun runProgramInGuest`) has
  proven unreliable against this minimal image (process exec fails even
  though guest auth and file-copy guestOps work).
- Supervisor logs: `journalctl -u iolbox-supervisor -f`.
- The unit's `KillMode=control-group` means bouncing the supervisor also
  reaps every IOL/VPCS child — no orphan hunting.

## Sizing (measured targets)

- rootfs dir: ~250-300 MB (minbase + i386 multiarch + sudo/procps/iptables
  + supervisor ~11 MB + vpcs ~1 MB, docs/locales stripped)
- `iolbox-rootfs.tar`: about the same as the rootfs dir
- `iolbox-appliance.vmdk`: 16 GB virtual (the image library lives inside the
  appliance and IOL images run 100-400 MB each), **sparse** — actual bytes
  ~600-800 MB with kernel + GRUB + open-vm-tools.

## Quick start (on a Linux builder)

```sh
cd runtime
sudo ./build-all.sh --supervisor-bin ../supervisor/bin/supervisor-linux-amd64
# -> runtime/build/iolbox-rootfs.tar
# -> runtime/build/iolbox-appliance.vmdk + iolbox-appliance.vmx
```

To rebuild the supervisor binary AND every packaging target (WSL, VMware,
OVA, LXC, native, qemu-compat) in one command, use `build-all-targets.sh`
instead — see `REDEPLOY.md` for the per-target redeploy steps once the
artifacts are built:

```sh
cd runtime
sudo ./build-all-targets.sh
```

Windows side, VMware provider: copy `iolbox-appliance.vmx` + `.vmdk` into an
empty directory, then:

```powershell
vmrun -T ws start "iolbox-appliance.vmx" nogui
vmrun -T ws getGuestIPAddress "iolbox-appliance.vmx" -wait
# browse to http://<that-ip>:4001
```

Windows side, WSL2 provider (only where Hyper-V is already enabled — never
enable it on a VMware machine):

```powershell
wsl --import iolbox C:\Users\<you>\iolbox-wsl runtime\build\iolbox-rootfs.tar
wsl -d iolbox -- systemctl status iolbox-supervisor.service
# browse to http://localhost:4001
```

## CI (GitHub Actions) — what a release build actually does

`.github/workflows/release.yml` triggers on a `v*` tag push (or
`workflow_dispatch` for a dry run — build/test only, no draft). It builds
what it can on `ubuntu-latest` and drafts a release; the three disk-image
targets (vmdk/vmx, OVA, qcow2) need a root-capable builder for loop
devices/chroot/`grub-install` and are built and attached **by hand** — see
the "Quick start" commands above and the draft release body itself, which
lists exactly what's still missing.

Two jobs feed the draft:

- **`build-windows`** (`windows-latest`): vets/tests and builds
  `tools/iolab-launcher` and `tools/capture-helper` — plain Go, no Rust/Tauri.
- **`build-linux`** (`ubuntu-latest`): `npm ci` in `app/`, `bash
  build-release.sh` (GUI-embedded supervisor), installs
  `debootstrap zstd build-essential`, runs `fetch-vpcs.sh`, then
  `sudo build-rootfs.sh` and — from that one rootfs — `sudo pack-wsl.sh`,
  `sudo pack-lxc.sh`, and `pack-native.sh` (root-agnostic, run under sudo
  anyway). Generates `SHA256SUMS-ci.txt` over those three artifacts (it does
  **not** cover the hand-built vmdk/vmx/ova/qcow2/launcher-zip — checksum
  them separately if you need that).

The `release` job (only on an actual tag, not `workflow_dispatch`) downloads
both jobs' artifacts and publishes them as a **draft** GitHub Release via
`softprops/action-gh-release` — never live, since it's deliberately
incomplete until the disk-image artifacts are attached by hand.

Notes:
- No secrets, no Cisco bits — everything CI fetches is public Debian + GNS3
  vpcs source.
- What CI cannot do: boot-smoke any of the appliance targets (no nested
  virtualization on the runners). That stays a manual/release-checklist
  step on the builder or a real target machine.
