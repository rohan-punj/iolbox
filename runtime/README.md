# iolbox runtime

The **runtime** is the small Linux environment that actually executes IOL and
VPCS. Everything here builds **one** Debian-slim (bookworm) rootfs and then
packages that *same* rootfs two ways:

- a **WSL2 import tarball** (`iolbox-rootfs.tar`) for the `wsl2` provider
- a **VMware Workstation/Player appliance** (`iolbox-appliance.vmx` + `.vmdk`)
  for the `vmware` provider (the default on machines that already run VMware —
  see `docs/providers.md`)

Both providers run the identical rootfs, so a bug fixed once is fixed for
both. The `qemu-tcg` compatibility provider also reuses the VMware disk image
unmodified — see `qemu-compat.md`.

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
  build-rootfs.sh          debootstrap the base rootfs -> runtime/build/rootfs/
  fetch-vpcs.sh             clone+build GNS3 vpcs (static link) -> vpcs binary
  pack-wsl.sh               rootfs -> iolbox-rootfs.tar (wsl --import format)
  pack-vmware.sh            rootfs -> bootable GPT disk -> vmdk + templated .vmx
  build-all.sh              orchestrator: rootfs -> [pack-wsl, pack-vmware]
  qemu-compat.md            qemu-system-x86_64 (TCG) fallback notes
  files/
    iolbox-supervisor.service      systemd unit, autostarts the supervisor
    iolbox-firstboot-iourc.service systemd oneshot, runs before the above
    prestart-clean.sh             ExecStartPre: stale tap devices + run-dir sweep
    firstboot-iourc.sh            generates iourc from the runtime's own hostid
    iolbox-init.sh                 non-systemd fallback init script
    wsl.conf                      /etc/wsl.conf for the WSL2 import (pins hostname!)
    80-ethernet-dhcp.network      networkd fallback: plain DHCP on en*
    99-iolbox.conf                 sysctl drop-in (ip_forward off until NAT node starts)
    iolbox-appliance.vmx.tmpl      templated VMware VMX (4 vCPU / 4 GB / 2 NICs)
  build/                   OUTPUT (git-ignored)
```

## The one rootfs, two packages

`build-rootfs.sh` is the only script that touches `debootstrap`/APT. It
produces `runtime/build/rootfs/`, a directory tree that IS the runtime
filesystem. Everything downstream (`pack-wsl.sh`, `pack-vmware.sh`) consumes
that same directory (pack-vmware.sh copies it onto a disk image and adds a
kernel + GRUB + open-vm-tools + per-NIC network configs; pack-wsl.sh just
tars it).

Rebuilding only one package does **not** require re-running
`build-rootfs.sh` — just re-run the one pack script.

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

- Root console login: user `root`, password `iolbox` (no sshd in the image;
  console only). Open the VM in the Workstation UI instead of `nogui`.
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

## CI (GitHub Actions) — what a release build needs

The whole pipeline runs unprivileged-runner-friendly on `ubuntu-latest`
(sudo is passwordless there; loop devices and chroots work on the standard
runners):

```yaml
jobs:
  runtime-artifacts:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4       # GUI bundle for the embed
        with: { node-version: 20 }
      - uses: actions/setup-go@v5
        with: { go-version: 'stable' }
      - run: sudo apt-get update && sudo apt-get install -y
             debootstrap qemu-utils parted e2fsprogs rsync
             debian-archive-keyring build-essential
      - run: cd app && npm ci
      - run: bash build-release.sh        # GUI-embedded supervisor binary
      - run: cd runtime && sudo ./build-all.sh
             --supervisor-bin ../supervisor/bin/supervisor-linux-amd64
      - uses: actions/upload-artifact@v4   # or softprops/action-gh-release on tags
        with:
          name: iolbox-runtime
          path: |
            runtime/build/iolbox-rootfs.tar
            runtime/build/iolbox-appliance.vmdk
            runtime/build/iolbox-appliance.vmx
```

Notes:
- `debian-archive-keyring` matters on Ubuntu builders — without it
  debootstrap proceeds UNVERIFIED with only a warning.
- Artifact sizes: tar ~170 MB, vmdk ~1.6 GB (sparse file; consider
  zstd-compressing the vmdk for release assets).
- No secrets, no Cisco bits — everything fetched is public Debian + GNS3
  vpcs source.
- What CI cannot do: boot-smoke the VMware appliance (no nested VMware).
  The boot smoke stays a manual/release-checklist step; see the
  drive-appliance smoke notes in the repo docs.
