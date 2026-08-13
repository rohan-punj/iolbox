# Installing iolbox

iolbox is a lightweight, Windows-native lab tool for Cisco IOL and VPCS: a
Go supervisor with an embedded browser GUI drives a small Linux runtime
(wherever it runs best on your machine) and you drive it all from a browser
tab. **iolbox ships no Cisco software** — you supply your own IOL `.bin`/
`.iol` image files and hold the appropriate licenses for them; see the
"First steps" section below for how images get loaded in on each target.

The v0.5.2 release publishes six deployable artifacts (plus `capture-helper.exe`,
a small standalone Wireshark-bridge helper, and `SHA256SUMS-ci.txt` — which
covers only the CI-built artifacts; the VMware/OVA/QEMU disk-image artifacts
in sections 1, 2, and 6 are built and attached by hand and aren't in it).
Pick the row that matches your situation:

| Your situation | Artifact | Section |
|---|---|---|
| Windows desktop/laptop, want the simplest path | `iolbox-launcher-v0.5.2-windows.zip` | [QEMU disk (Windows launcher)](#6-qemu-disk-windows-bundled-launcher) |
| Windows desktop already running VMware Workstation/Player, or ESXi/VirtualBox | `iolbox-appliance-v0.5.2.ova` | [OVA](#1-ova-vmware-workstationplayer-esxi-virtualbox) |
| Windows with WSL2/Hyper-V already enabled | `iolbox-rootfs.tar` | [WSL rootfs](#3-wsl-rootfs-wsl2) |
| Proxmox homelab | `iolbox-ct-v0.5.2.tar.zst` | [Proxmox LXC](#4-proxmox-lxc) |
| Existing Linux server / cloud VM / on-prem hypervisor guest | `iolbox-server-v0.5.2.tar.gz` | [Native (systemd)](#5-native-systemd-linux-server) |

All six run the identical Go supervisor and (except the native target's
build) the identical Debian-slim runtime — see `runtime/README.md`'s "one
rootfs, two packages" note. Whichever you pick, the GUI ends up at
**`http://<host>:4001`**, and it has **no login of any kind**. Only expose
it on localhost or a network you trust (see the security note repeated in
each section).

Default sizing for every target is **4 vCPUs / 4096 MB RAM** (source:
`runtime/resources.env`, `IOLBOX_VCPUS=4` / `IOLBOX_RAM_MB=4096`); each
section below notes where to change it for that target.

A Docker image also exists as a seventh, build-from-source option if you'd
rather not touch WSL2/Hyper-V/VMware at all — see `docker/README.md`. It
isn't one of the six release artifacts and isn't covered in depth here.

---

## 1. OVA (VMware Workstation/Player, ESXi, VirtualBox)

**Artifact:** `iolbox-appliance-v0.5.2.ova`
**Source:** `runtime/pack-ova.sh`, `runtime/REDEPLOY.md`

### GUI import

1. Download `iolbox-appliance-v0.5.2.ova`.
2. VMware Workstation/Player: **File > Open**, pick the `.ova`, follow the
   import wizard. ESXi: use the web UI's **Create/Register VM > Deploy a
   virtual machine from an OVF or OVA file**. VirtualBox: **File > Import
   Appliance**.
3. **Expect an importer warning about a missing manifest.** This OVA is
   deliberately built with *no* `.mf` manifest file (see `pack-ova.sh`'s
   "NO manifest" comment) — this is expected, not a corrupt download.
4. Start the VM.

### CLI import (`ovftool`)

```bash
ovftool --acceptAllEulas --allowExtraConfig --name=iolbox \
    "iolbox-appliance-v0.5.2.ova" <dest-dir>\
```

### Boot and find the GUI

```powershell
vmrun -T ws start "iolbox.vmx" nogui
vmrun -T ws getGuestIPAddress "iolbox.vmx" -wait
# browse to http://<that-ip>:4001
```

(`open-vm-tools` is installed in the appliance so `getGuestIPAddress`
works; alternatively grep the appliance's fixed host-only MAC
`00:50:56:3f:ab:01` in `C:\ProgramData\VMware\vmnetdhcp.leases` — see
`runtime/README.md` "Networking".)

**Sizing:** baked in at build time from `runtime/resources.env` (4 vCPU /
4096 MB) into the OVF descriptor; change it after import via your
hypervisor's normal VM-settings edit (VMware: VM Settings; ESXi:
Edit Settings; VirtualBox: Settings > System).

**It worked if:** the GUI loads at `http://<vm-ip>:4001` and the Palette's
host-monitor footer shows a `build <version>` line (per `REDEPLOY.md`).

---

## 2. VMware vmdk+vmx (raw pre-converted pair)

**Artifact:** `iolbox-appliance-v0.5.2.vmdk` + `iolbox-appliance-v0.5.2.vmx`
**Source:** `runtime/pack-vmware.sh`, `runtime/REDEPLOY.md`

Attached separately from the OVA, built by hand alongside it (`pack-vmware.sh`
and `pack-ova.sh` both consume the same `build-rootfs.sh` output — see
`runtime/README.md`). For most people the OVA in
[section 1](#1-ova-vmware-workstationplayer-esxi-virtualbox) is still the
simpler pick — a single standard-format file with no manifest warning to
click through. Use this pair instead if you want to skip the OVF import
step entirely (e.g. deploying many copies): download both files into the
same folder and open the `.vmx` directly in VMware Workstation/Player
(**File > Open**), or `vmrun -T ws start "iolbox-appliance-v0.5.2.vmx" nogui`.

**Sizing** (once you have the `.vmx`): templated from
`runtime/resources.env` (4 vCPU / 4096 MB) — see `files/iolbox-appliance.vmx.tmpl`.
Edit the `.vmx` (`numvcpus`, `memsize`) or use VM Settings in the Workstation
UI, then power-cycle the VM.

---

## 3. WSL rootfs (WSL2)

**Artifact:** `iolbox-rootfs.tar`
**Source:** `runtime/REDEPLOY.md`, `runtime/README.md`

Requires WSL2 (i.e. the Windows Hyper-V platform already enabled). Per
`runtime/README.md`: **never enable Hyper-V on a machine you use for
VMware Workstation** — the two are mutually exclusive platforms on the same
box.

```powershell
wsl --import iolbox C:\Users\<you>\iolbox-wsl iolbox-rootfs.tar
wsl -d iolbox -- systemctl status iolbox-supervisor.service
# browse to http://localhost:4001
```

The imported distro autostarts the `iolbox-supervisor` systemd unit (and
the `iolbox-firstboot-iourc` oneshot that mints the IOL license before it)
on its own; the `systemctl status` check above is just a sanity check that
it actually came up.

To redeploy/upgrade later, since a WSL rootfs has no in-place binary swap:

```powershell
wsl --unregister iolbox
wsl --import iolbox C:\Users\<you>\iolbox-wsl <new-rootfs>.tar
```

**Sizing:** WSL2 uses its own global memory/CPU cap (`.wslconfig`), not
`runtime/resources.env` — this target isn't one of the ones the resources
file drives (see that file's comment listing which targets it covers).
Adjust via `%UserProfile%\.wslconfig`.

**It worked if:** `http://localhost:4001` loads and `wsl -d iolbox --
systemctl status iolbox-supervisor.service` shows active/running.

---

## 4. Proxmox LXC

**Artifact:** `iolbox-ct-v0.5.2.tar.zst`
**Source:** `runtime/files/lxc/pct-create.md` (canonical — also shipped as
`SETUP.md` inside the tarball itself)

### 1. Upload the template

```bash
scp iolbox-ct-v0.5.2.tar.zst root@<pve-host>:/var/lib/vz/template/cache/
```

(Or Datacenter > Storage > Content > Upload; the storage must allow "CT
Template".)

### 2. Create the container

```bash
pct create <vmid> local:vztmpl/iolbox-ct-v0.5.2.tar.zst \
    --unprivileged 1 \
    --hostname iolbox \
    --cores 4 \
    --memory 4096 \
    --swap 512 \
    --net0 name=eth0,bridge=vmbr0,ip=dhcp \
    --features nesting=0 \
    --onboot 1
```

`--cores`/`--memory` mirror `runtime/resources.env`'s defaults (4 / 4096);
resize live later with `pct set <vmid> --cores <n> --memory <mb>`, no
rebuild needed.

**Important:** set `--hostname` at creation and never rename it after
first boot — the IOL license (`iourc`) is minted from the container's
hostid + hostname at first boot, and renaming later invalidates it.

### 3. Add the TUN/TAP device lines the NAT node needs

Unprivileged CTs need explicit opt-in. Edit `/etc/pve/lxc/<vmid>.conf` on
the Proxmox host:

```bash
cat >> /etc/pve/lxc/<vmid>.conf <<'EOF'
lxc.cgroup2.devices.allow: c 10:200 rwm
lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file
EOF
```

Restart if it was already running: `pct stop <vmid> && pct start <vmid>`.

### 4. Start it and find the GUI

```bash
pct start <vmid>
pct exec <vmid> -- systemctl status iolbox-supervisor.service
pct exec <vmid> -- ip -4 addr show eth0
```

Browse to `http://<that-ip>:4001`.

**Security note (Proxmox-specific):** on a shared Proxmox host, `:4001`
(no auth) is reachable by anything that can route to the CT's
bridge/VLAN — a materially bigger exposure than a laptop-local appliance.
Put it behind a firewall rule, a restricted VLAN, or a reverse proxy with
auth; `pct set <vmid> --firewall 1` plus a CT firewall rule restricting
`:4001` to your admin subnet is the simplest fix.

**It worked if:** `http://<ct-ip>:4001` loads and
`pct exec <vmid> -- systemctl status iolbox-supervisor.service` is active.

---

## 5. Native (systemd Linux server)

**Artifact:** `iolbox-server-v0.5.2.tar.gz`
**Source:** `runtime/files/native/README.txt`, `runtime/files/native/install.sh`

For "bring your own Linux box": bare metal, a cloud VM, or an existing
on-prem hypervisor guest. Requires systemd, x86-64/glibc, and root.

```bash
tar xzf iolbox-server-v0.5.2.tar.gz
cd iolbox-server-v0.5.2
sudo ./install.sh                  # binds GUI/console/capture to 127.0.0.1 only
# or:
sudo ./install.sh --bind all       # binds 0.0.0.0 — LAN/VPN/tunnel reachable
```

`install.sh` installs to `/opt/iolbox` (binaries, `images/`, `labs/`,
`run/`), installs and enables the `iolbox-supervisor` and
`iolbox-firstboot-iourc` systemd units, writes `/etc/iolbox/bind.env`,
generates the IOL license immediately (no reboot needed), starts the
supervisor, and prints the GUI URL on completion.

Keep the hostname stable after install — the IOL license is keyed to this
host's hostid + hostname; a cloud instance with cloud-init hostname
randomization will silently break IOL licensing on the next reboot.
`install.sh` warns if your hostname looks cloud-managed/ephemeral.

**Changing the bind mode later:**

```bash
sudo $EDITOR /etc/iolbox/bind.env      # edit IOLBOX_WS_ADDR / CONSOLE / CAPTURE
sudo systemctl restart iolbox-supervisor.service
```

**Logs and status:**

```bash
journalctl -u iolbox-supervisor -f
systemctl status iolbox-supervisor iolbox-firstboot-iourc
```

**Uninstall:**

```bash
sudo ./uninstall.sh              # prompts before deleting images/labs
sudo ./uninstall.sh --yes        # non-interactive, deletes everything
```

**Sizing:** the native install doesn't source `runtime/resources.env`
directly (it's a bare binary + systemd units, not a VM/container with a
resource cap) — the host's own CPU/RAM is the ceiling; there's no
target-specific vCPU/RAM knob documented for this artifact.

**It worked if:** with `--bind local` (default),
`http://127.0.0.1:4001` loads from that same host, or via an SSH tunnel
(`ssh -L 4001:127.0.0.1:4001 <user>@<host>`); with `--bind all`,
`http://<host-ip>:4001` loads directly. `install.sh` prints the exact URL
to use at the end of the run.

---

## 6. QEMU disk (Windows, bundled launcher)

**Artifact:** `iolbox-launcher-v0.5.2-windows.zip` (one zip — the launcher
exe, the disk image, and the bundled `qemu/` folder all together; it depends
on the disk sitting right next to it, so it ships as a single archive rather
than separate downloads)
**Source:** `tools/iolab-launcher/README.md`, `THIRD_PARTY.md`,
`runtime/REDEPLOY.md`, `runtime/qemu-compat.md`

This is the simplest Windows path: no VMware, no WSL2/Hyper-V, no admin
rights, works on any Windows machine.

### Setup

1. Download `iolbox-launcher-v0.5.2-windows.zip` and extract it — everything
   needed is already laid out correctly inside:

```
iolbox-launcher.exe
iolbox-disk.qcow2
qemu\
  qemu-system-x86_64.exe
  *.dll
  share\...
THIRD_PARTY.md
```

(`THIRD_PARTY.md` confirms the launcher looks for `qemu\qemu-system-x86_64.exe`
and `iolbox-disk.qcow2` relative to its own exe path; both are overridable
for dev via `--qemu`/`--disk`.)

2. Double-click `iolbox-launcher.exe`. A console window opens and asks two
   questions — `vCPUs for the guest [4]:` and `RAM MB for the guest [4096]:`
   (press Enter twice to accept the defaults; passing `--smp`/`--mem` on the
   command line skips the questions entirely). It then boots the disk under
   QEMU/TCG (software emulation — no KVM/WHPX needed) and opens your browser
   to the GUI once it's up.

### What comes up

- **GUI:** `http://localhost:4001` — forwarded to `127.0.0.1` **only**;
  the launcher's own log output states this explicitly ("the GUI has no
  auth; the launcher forwards it to 127.0.0.1 only").
- Boot progress, and later a clean Ctrl-C shutdown, live in the launcher's
  console window.

### Be patient on first boot

TCG (pure software CPU emulation, no hardware acceleration) is slow — this
is the documented tradeoff of the zero-prerequisite fallback path (see
`runtime/qemu-compat.md`). Expect the guest to take **minutes**, not
seconds, to reach a running supervisor. While the launcher window shows
"still booting", let it sit before assuming something's wrong.

### Your data survives; the guest OS disk doesn't

The qcow2 is attached with `snapshot=on`: QEMU opens it read-only and
redirects every guest write to a temporary overlay that's discarded when
QEMU exits. That means:

- The shipped disk can never be corrupted by a hard kill.
- Nothing you do *inside the guest OS* persists between launches.
- Your actual data — IOL/VPCS images and saved labs — lives on the
  **Windows side** instead, in `images\` and `labs\` next to the launcher
  exe (created automatically on first launch). Every launch re-uploads
  everything in `images\` into the guest's image registry; `labs\*.json`
  files are pushed in on launch and your in-GUI edits are written back to
  that folder automatically (every 30s and once more on shutdown).

Override the folders with `--images-dir <path>` / `--labs-dir <path>`, or
disable sync entirely with `--no-sync`. Run `iolbox-launcher.exe -h` for
the full flag list, including `--mem` (default 4096 MB) and `--smp`
(default 4 vCPUs) — these mirror `runtime/resources.env`'s defaults but
are hardcoded in the launcher and kept in sync by hand (per
`runtime/README.md`), and are overridable per run.

### Raw QEMU invocation (non-launcher users)

If you'd rather drive QEMU yourself instead of using the launcher, this is
the reference command from `runtime/qemu-compat.md` (written for the
appliance vmdk, but the same `iolbox-disk.qcow2` boots the same way):

```sh
qemu-system-x86_64 \
    -machine pc \
    -accel tcg \
    -m 768 \
    -smp 1 \
    -drive file=iolbox-disk.qcow2,format=qcow2,if=virtio \
    -netdev user,id=net0,hostfwd=tcp:127.0.0.1:4000-:4000,hostfwd=tcp:127.0.0.1:9000-:9000,hostfwd=tcp:127.0.0.1:9001-:9001,hostfwd=tcp:127.0.0.1:5500-:5500 \
    -device virtio-net-pci,netdev=net0 \
    -display none \
    -serial none \
    -no-reboot
```

Adjust `-m`/`-smp` up (e.g. `-m 4096 -smp 4`) to match the documented
defaults — the `768`/`1` above is qemu-compat.md's minimal reference, not
a recommendation. Note the hostfwd list only reserves example console/
capture ports (9000, 9001, 5500) — `qemu-compat.md` flags that a real
per-node port range needs to be pre-reserved (e.g. 9000-9099 consoles,
5500-5599 capture) since qemu can't add hostfwd rules to an already-running
`-netdev user` instance.

**It worked if:** the browser opens to `http://localhost:4001` and the
Palette footer shows `build <version>`; the launcher's console window
reports "GUI is up".

---

## First steps after install

1. **Add an IOL image.** iolbox never bundles Cisco software — how you
   load an image depends on the target:
   - Launcher/QEMU target: drop the `.bin`/`.iol` file into the `images\`
     folder next to `iolbox-launcher.exe`; it's uploaded and registered on
     the next launch.
   - Any other target (OVA/vmdk/WSL/LXC/native): use the GUI's Images
     upload once you're browsed in at `:4001`.
2. **Create a lab.** Open the GUI at `http://<host>:4001`, drag nodes onto
   the canvas, wire them up, and pick an uploaded image for each IOL node
   from the image picker.
3. Console into a node from the GUI (embedded xterm.js), or right-click a
   link to start a live Wireshark capture.

## Troubleshooting

- **GUI not reachable at `:4001`:**
  - Confirm the supervisor is actually running: `systemctl status
    iolbox-supervisor` (native/LXC/WSL/appliance), or for the launcher/QEMU
    path check the launcher's console window for "GUI is up" vs an error.
  - Confirm you're using the right host/IP for the target — a VMware/LXC
    appliance needs its guest IP (`vmrun getGuestIPAddress` /
    `pct exec <vmid> -- ip -4 addr show eth0`), not `localhost`; the
    launcher/QEMU and native-`--bind local` targets use `127.0.0.1`/
    `localhost` specifically.
  - Remember `:4001` has **no authentication** — if you intentionally
    bound it to `0.0.0.0`/LAN (native `--bind all`, or a routable Proxmox
    bridge) and still can't reach it, check host/Proxmox firewall rules
    first, not the supervisor.
- **TCG (QEMU launcher) boot is slow:** this is expected — software CPU
  emulation with no hardware acceleration. Give it a few minutes on first
  launch rather than assuming it hung; the launcher's console window
  prints "still booting" progress lines while it waits.
- **Console / root access (appliance targets):** the OVA, vmdk/vmx, WSL, and
  LXC guests all use the default console login `root` / `iolbox` (fixed and
  deliberately non-secret — `runtime/build-rootfs.sh`). You normally never
  need it (the whole workflow is the browser GUI), but it's there if you must
  change the guest's static IP, inspect `journalctl -u iolbox-supervisor`, or
  regenerate the IOL license by hand. **Change it** (`passwd`) if the guest is
  reachable from an untrusted network.
- **Image not recognized / node won't start:** re-check the image actually
  landed — for the launcher, confirm the file is directly inside `images\`
  (not a subfolder) and re-launch so the sync step re-runs; for other
  targets, re-check the GUI's Images list after uploading. If a node fails
  to start with a license-looking error, the IOL license (`iourc`) may
  have been invalidated by a hostname change — see the "Hostname / IOL
  license" note in `runtime/files/native/README.txt` (native target) or
  the equivalent per-target regeneration steps (delete the generated
  `iourc` and restart the `iolbox-firstboot-iourc` unit, then the
  supervisor).
