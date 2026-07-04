# Redeploying a new build to each target

Every deployment target shares the SAME versioned supervisor binary
(`supervisor/bin/supervisor-linux-amd64`, built by the repo-root
`build-release.sh` with `-ldflags -X main.version=$(git describe ...)`).
Once a new binary/artifact set exists, use the steps below to push it out —
the GUI's Palette host-monitor footer shows `build <version>` from the
running supervisor's `hello.supervisor`, so after any redeploy you can
confirm it took by just looking at the footer instead of doing git
archaeology.

Build everything in one go with:

```sh
cd runtime
sudo ./build-all-targets.sh
```

or build only what changed with `build-release.sh` (repo root) plus the one
relevant `pack-*.sh` script — see `runtime/README.md` for each script's
flags. Per-target redeploy steps below assume the artifacts already exist
under `runtime/build/`.

## LXC (Proxmox, unprivileged container)

Fastest path for an already-running container: push just the binary and
restart the unit — no re-`pct create` needed.

```sh
pct push <vmid> supervisor/bin/supervisor-linux-amd64 /opt/iolbox/supervisor
pct exec <vmid> -- chmod 0755 /opt/iolbox/supervisor
pct exec <vmid> -- systemctl restart iolbox-supervisor
```

To deploy a fresh container from a new template instead (new `iolbox-ct-<ver>.tar.zst`
built by `pack-lxc.sh`), follow `runtime/files/lxc/pct-create.md` (same doc is
also shipped as `SETUP.md` inside the tarball).

## VMware (appliance vmx/vmdk)

The appliance vmdk is a full disk image, not something you patch in place —
rebuild it and swap the file:

```sh
cd runtime
sudo ./pack-vmware.sh --build-dir build --version <ver>
```

Then on the Windows side: stop the running VM, replace the old
`iolbox-appliance.vmdk`/`.vmx` (or the versioned pair) in place, and:

```powershell
vmrun -T ws start "iolbox-appliance.vmx" nogui
vmrun -T ws getGuestIPAddress "iolbox-appliance.vmx" -wait
# browse to http://<that-ip>:4001 and check the Palette footer's build line
```

## QEMU (bundled `qemu-system-x86_64.exe` via `tools/iolab-launcher`)

The launcher attaches `iolbox-disk.qcow2` with `snapshot=on` — every launch
already boots a fresh copy-on-write overlay off the golden disk, so a
redeploy is just: rebuild the qcow2, drop it next to the launcher exe, and
relaunch (no persistent guest state to worry about; `images\` and `labs\`
live on the Windows side and are unaffected).

```sh
cd runtime
sudo ./pack-qemu.sh --build-dir build --version <ver>
# -> runtime/build/iolbox-disk-<ver>.qcow2
```

```powershell
# stop the launcher if running, then:
copy runtime\build\iolbox-disk-<ver>.qcow2 <launcher-dir>\iolbox-disk.qcow2
# relaunch iolbox-launcher.exe
```

## Native (systemd Linux server / cloud VM / on-prem hypervisor guest)

`install.sh` (from `pack-native.sh`'s `iolbox-server-<ver>.tar.gz`) is meant
to also be re-run for an upgrade — it copies binaries/units in place and
(re)starts the service:

```sh
tar xzf iolbox-server-<ver>.tar.gz
cd iolbox-server-<ver>
sudo ./install.sh            # same flags as the original install (--bind etc.)
```

Or, for a binary-only bump without re-running the full installer:

```sh
sudo cp supervisor/bin/supervisor-linux-amd64 /opt/iolbox/supervisor
sudo chmod 0755 /opt/iolbox/supervisor
sudo systemctl restart iolbox-supervisor
```

## OVA (any OVF-importing hypervisor)

Like VMware, this is a whole-disk artifact — rebuild and re-import:

```sh
cd runtime
sudo ./pack-ova.sh --build-dir build --version <ver>
# -> runtime/build/iolbox-appliance-<ver>.ova
```

Import the new OVA in place of the old one (exact steps are
hypervisor-specific — vSphere/VirtualBox/etc. all have their own OVF
importer).

## WSL2

`iolbox-rootfs.tar` (from `pack-wsl.sh` / `build-all.sh`) becomes a WSL distro
via `wsl --import`. To redeploy, unregister the old distro and re-import:

```powershell
wsl --unregister iolbox
wsl --import iolbox C:\Users\<you>\iolbox-wsl runtime\build\iolbox-rootfs.tar
wsl -d iolbox -- systemctl status iolbox-supervisor.service
# browse to http://localhost:4001
```

(There's no in-place "just swap the binary" path for WSL since the whole
rootfs ships as one tarball — a `wsl --import` is the unit of redeploy.)

## Docker

Rebuild the image (build context MUST be the repo root, not `docker/`) and
recreate the container — see `docker/README.md` for the full build story:

```sh
bash build-release.sh   # repo root, refreshes supervisor/bin/supervisor-linux-amd64
docker build -f docker/Dockerfile -t iolbox:latest .
docker compose -f docker/compose.yml up -d --build
```

## Confirming the redeploy took

After any of the above, open the GUI and check the Palette's host-monitor
footer — it now shows `build <git describe>` sourced live from the running
supervisor's `hello.supervisor`. If it still shows the old (or a bare
`0.1.0`) version, the redeploy didn't actually replace the running binary —
re-check the steps above rather than assuming the fix shipped.
