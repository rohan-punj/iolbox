# Proxmox LXC deployment — `pct create` recipe

This is the exact recipe for turning `iolbox-ct-<ver>.tar.zst` (built by
`runtime/pack-lxc.sh`) into a running **unprivileged** Proxmox LXC
container. It is also emitted as `SETUP.md` at the root of the tarball
itself (see pack-lxc.sh's header comment for why it's shipped in both
places) — this copy is the canonical source; keep them in sync if edited.

## 1. Upload the template

Copy `iolbox-ct-<ver>.tar.zst` to the Proxmox host's template storage, e.g.:

```sh
scp iolbox-ct-<ver>.tar.zst root@<pve-host>:/var/lib/vz/template/cache/
```

(Or upload via the Datacenter -> Storage -> Content -> Upload UI, storage
type must allow "CT Template".)

## 2. Create the container

```sh
pct create <vmid> local:vztmpl/iolbox-ct-<ver>.tar.zst \
    --unprivileged 1 \
    --hostname iolbox \
    --cores 4 \
    --memory 4096 \
    --swap 512 \
    --net0 name=eth0,bridge=vmbr0,ip=dhcp \
    --features nesting=0 \
    --onboot 1
```

Notes on each flag:

- `--unprivileged 1` — REQUIRED assumption behind everything else in this
  recipe (device cgroup allow-list below, no CAP_SYS_ADMIN games). A
  privileged CT also works but gives the container host-equivalent
  privilege for no benefit this workload needs.
- `--hostname iolbox` — **set it here, at creation, and never rename it
  after first boot.** The IOL license file (`iourc`) is minted at first
  boot from this CT's own hostid + hostname (see
  `iolbox-firstboot-iourc.service`); renaming later (`pct set <vmid>
  --hostname ...`) invalidates it and every IOL node will fail its
  license check on next start. If you need a different hostname than
  `iolbox`, pick it now, not later. (The hostname does NOT have to be
  literally `iolbox` — any value is fine, `iolbox` is just this doc's
  example — but it must be stable for the container's lifetime.)
- `--cores 4 --memory 4096` — matches the VMware appliance's sizing (see
  `runtime/files/iolbox-appliance.vmx.tmpl`). 4 GB covers the supervisor +
  a handful of IOL nodes; IOL nodes default to roughly 1 GB of guest RAM
  each in typical images, so budget `--memory` up if you plan to run more
  than 2-3 nodes concurrently (Proxmox lets you resize later with
  `pct set <vmid> --memory <mb>`, no rebuild needed). These mirror
  `runtime/resources.env` (the single source of truth for all targets);
  change a running container live with `pct set <vmid> --cores <n>
  --memory <mb>` — no rebuild needed.
- `--net0 ... ip=dhcp` — adjust bridge/VLAN/static-IP to your Proxmox
  network layout; this is exactly the config `pct` injects into the
  container's networkd config for you (see "Networking" below) — no
  in-image editing required.
- `--features nesting=0` — this container does not run Docker/LXC/KVM
  inside itself, so nesting stays off (smaller attack surface, no reason
  to pay for it).
- `--onboot 1` — optional; start automatically with the Proxmox host.

## 3. Add the two device/mount lines IOL's NAT node needs

Unprivileged CTs need explicit opt-in for TUN/TAP device access (the NAT
node's outbound path — see `runtime/README.md` "Networking") and the
`/dev/net/tun` character device. Edit the container's config directly
(`/etc/pve/lxc/<vmid>.conf` on the Proxmox host, or `pct set` where a flag
exists — these two lines have no `pct set` equivalent, so edit the file):

```sh
cat >> /etc/pve/lxc/<vmid>.conf <<'EOF'
lxc.cgroup2.devices.allow: c 10:200 rwm
lxc.mount.entry: /dev/net/tun dev/net/tun none bind,create=file
EOF
```

- `c 10:200 rwm` — major/minor 10:200 is the kernel's fixed `/dev/net/tun`
  device number; this line is what lets the unprivileged CT's cgroup even
  open the node once it's bind-mounted in.
- The `lxc.mount.entry` bind-mounts the HOST's `/dev/net/tun` node into
  the container at the same path. Without both lines together, the NAT
  node's tap-device creation fails (`open /dev/net/tun: no such device or
  address` / `operation not permitted`, depending on which line is
  missing).
- NET_ADMIN inside the container's own network namespace is already
  permitted for unprivileged CTs by default (it's a namespaced
  capability, not a host-wide one) — no extra `lxc.cap.drop` tuning
  needed for `ip tuntap`/`ip link`/`iptables` to work inside the CT.

Restart if the CT was already running when you edited the conf file:

```sh
pct stop <vmid> && pct start <vmid>
```

## 4. Start it and find the GUI

```sh
pct start <vmid>
pct exec <vmid> -- systemctl status iolbox-supervisor.service
pct exec <vmid> -- ip -4 addr show eth0
```

Browse to `http://<that-ip>:4001`.

## Security note: :4001 has NO authentication

Same posture as the WSL2/VMware artifacts (single-tenant, disposable lab
appliance — see `runtime/README.md` and `PLAN.md`): the supervisor's GUI/WS
bridge on `:4001` and the native telnet/capture ports it opens per node
have **no login**. On a shared Proxmox host this is reachable by anything
that can route to the CT's bridge/VLAN, which is a materially bigger
exposure than a laptop-local VMware appliance. Put it behind a firewall
rule, a VLAN with restricted access, or a reverse-proxy-with-auth — do
**not** expose `:4001` (or the console/capture port ranges) directly on a
routed/shared network. Proxmox's own firewall (`pct set <vmid> --firewall
1` + a datacenter/CT firewall rule restricting :4001 to your admin subnet)
is the simplest fix.

## Troubleshooting

- `journalctl -u iolbox-supervisor -f` inside the CT (`pct exec <vmid> --
  journalctl -u iolbox-supervisor -f`) — same as the other artifacts.
- If `sudo` inside the CT feels slow (~10s per invocation, visible as
  laggy NAT-node start), check `/etc/hosts` for a `127.0.1.1 <hostname>`
  line — Proxmox normally writes this for you, but
  `iolbox-firstboot-lxc-hosts.service` is a safety net that fills the gap
  if it's ever missing (see `runtime/pack-lxc.sh` for why).
- If `eth0` never gets an address, check whether Proxmox actually injected
  `/etc/systemd/network/eth0.network` (`pct exec <vmid> -- ls
  /etc/systemd/network/`) — if it's absent for some reason, the shipped
  `zz-lxc-eth-fallback.network` DHCP-on-eth0 catch-all should still bring
  the interface up, since it's the lowest-priority match.
