# iolbox — Docker runtime

This packages the same Linux runtime described in `runtime/README.md` (IOL +
VPCS control plane, the `iolbox` supervisor binary, the embedded GUI) as a
**container** instead of a WSL2 import tarball or VMware appliance. One
supervisor binary, one package set, one behavior contract across all three —
see `runtime/README.md`'s "one rootfs, two packages" framing; this is a third
packaging of the same runtime, not a separate product.

Use this if you already run Docker (Docker Desktop on Windows/Mac, or a
Linux Docker host) and would rather not enable WSL2/Hyper-V or run a VMware
appliance VM.

## Build

The supervisor binary is **not** committed to git (see the repo's
`.gitignore` — `/supervisor/bin/`) and a plain `go build` ships a placeholder
GUI, not the real one. Always build it via the repo-root script first:

```sh
# from the repo root
bash build-release.sh
# -> supervisor/bin/supervisor-linux-amd64 (GUI embedded)
```

Then build the image. **Build context must be the repo root**, not
`docker/`, because the Dockerfile copies that binary in:

```sh
docker build -f docker/Dockerfile -t iolbox:latest .
```

or, using the compose file (which already sets `context: ..` for you):

```sh
docker compose -f docker/compose.yml up -d --build
```

vpcs is **not** copied in from the host — it's built from source inside the
image's first stage, pinned to the same GNS3 fork + tag
(`v0.8.3`) that `runtime/fetch-vpcs.sh` uses, so there's nothing extra to
build locally for it.

## Run

```sh
docker compose -f docker/compose.yml up -d
# browse to http://localhost:4001
```

## Sizing (vCPU / RAM)

`docker/compose.yml` sets `cpus:` and `mem_limit:` from `docker/.env`
(`IOLBOX_VCPUS=4`, `IOLBOX_RAM_MB=4096`), which mirrors `runtime/resources.env`
— the single source of truth for vCPU/RAM across every iolbox deployment
target (VMware appliance, OVA, Proxmox LXC, this container, and the Windows
QEMU launcher's own flag defaults). Edit `docker/.env` and re-run `docker
compose -f docker/compose.yml up -d` to change it, or override inline:

```sh
IOLBOX_RAM_MB=8192 docker compose -f docker/compose.yml up -d
```

Native telnet consoles and Wireshark capture tees also work directly against
the Docker host, same as they do against the appliance's IP:

```sh
telnet localhost <consolePort>          # ports 9000-9999
wireshark -k -i TCP@localhost:<capturePort>   # ports 5500-6499
```

(Console/capture port numbers come from the GUI/API per node/link — they are
not fixed per node, just drawn from those two ranges.)

## Upgrade

```sh
docker compose -f docker/compose.yml pull   # if pulling a published image, else:
bash build-release.sh                        # rebuild the supervisor binary
docker compose -f docker/compose.yml up -d --build
```

Uploaded images, saved labs, and the generated iourc license all live in
named volumes (`iolbox-images`, `iolbox-labs`, `iolbox-state`) that are **not**
removed by `up -d --build` or a plain `docker compose down` — only
`docker compose down -v` deletes them. Recreating the container (new image,
new container ID) is safe and expected on every upgrade.

## No authentication — LAN/tunnel only

The supervisor's GUI/WS bridge (`:4001`), telnet consoles, and Wireshark
capture streams have **no login and no auth of any kind** — this mirrors the
WSL2/VMware appliance exactly (`docs/protocol.md`, `supervisor/README.md`):
it's a single-tenant control plane meant to be reached from the machine
sitting next to it, or over a private link.

- Fine: `localhost`, a trusted home/lab LAN, an SSH `-L`/WireGuard tunnel.
- Not fine: publishing these ports on a cloud VM's public IP, a
  port-forwarded home router, or any network you don't fully trust.

If you need remote access, tunnel `4001` (and whichever console/capture
ports a given lab is using) over SSH or WireGuard rather than binding them
to a public interface.

## hostid / iourc persistence

IOL's license file (`iourc`) is generated once, at first container start,
from this instance's `hostid` + hostname (`supervisor -gen-iourc`, the same
mechanism `runtime/files/firstboot-iourc.sh` uses for the appliance). The
container's `hostname` is pinned to `iolbox` in `compose.yml`, but hostname
alone is **not enough** to keep the license stable:

`supervisor`'s `hostID()` (see `supervisor/cmd/supervisor/main.go`) shells
out to the `hostid` command, which reads `/etc/hostid` if present — and
**derives a value from the container's current IP address if that file is
absent** (standard glibc `gethostid(3)` fallback behavior). A container's IP
is not stable across `docker compose down && up` (new network namespace, new
DHCP-ish IPAM assignment each time), so without persisting `/etc/hostid`
itself, the hostid — and therefore the generated iourc — would silently
change, and the license would silently break, on every recreate (not just a
simple restart-in-place, which keeps the same network namespace and would
have hidden this).

`docker/entrypoint.sh` handles this by generating `/etc/hostid` once and
persisting **both** it and the resulting `iourc` in the `iolbox-state` named
volume, restoring them on every subsequent start before the supervisor
runs. This was chosen over baking a hostid into the image itself, because a
baked-in value would be identical across every user's container — the exact
problem `firstboot-iourc.sh` already avoids for `iourc` one layer up (never
bake the license itself into the image); the same argument applies to the
hostid that feeds it.

Net effect: `docker compose down && docker compose up -d` (same volumes)
keeps the same license. `docker compose down -v` (volumes deleted) or a
fresh `docker volume create`/first run generates a new one — same tradeoff
the appliance has when you build a brand-new VM.

## Ports published (and why exactly these ranges)

Verified directly against the port allocators in
`supervisor/internal/server/server.go`:

| Port(s)      | Purpose                                   | Source |
|--------------|--------------------------------------------|--------|
| 4001         | GUI + WS control/console bridge (`-ws-addr`) | `supervisor/cmd/supervisor/main.go` flag default |
| 9000-9999    | Native telnet consoles, one per running node | `server.go:100` `node.NewPortAllocator(9000, 1000)` |
| 5500-6499    | Native Wireshark capture tees, one per armed link | `server.go:101` `node.NewPortAllocator(5500, 1000)` |
| 10000-29999  | **NOT published** — internal relay/UDP tunnel ports between the supervisor and its spawned IOL/VPCS children | `server.go:102` `node.NewPortAllocator(10000, 20000)`; see `docs/protocol.md` |

## Package set (vs. the WSL2/VMware appliance)

Mirrors `runtime/build-rootfs.sh`'s `BASE_INCLUDE` (`iproute2`,
`iputils-ping`, `libssl3`, `sudo`, `procps`, `iptables`) plus `libc6:i386` +
`libssl3:i386` multiarch (32-bit IOL image support — same
`INCLUDE_I386=1`-by-default posture as the appliance build). Deliberate
diffs, both documented again inline in `docker/Dockerfile`:

- **`systemd`/`systemd-sysv`/`udev` dropped.** The container has no init
  system; `docker/entrypoint.sh` is PID 1 and does inline what the appliance
  splits across systemd units (prestart cleanup, sysctl, firstboot-iourc,
  then `exec`s the supervisor).
- **`openssh-client` dropped.** The appliance keeps it only as a maintainer
  debugging convenience (its own comment says so, not load-bearing);
  `docker exec`/`docker cp` cover the same need for a container.

## Structural validation done so far (docker not available on this host)

- `bash -n docker/entrypoint.sh` — passes.
- Dockerfile/compose.yml syntax hand-checked (see `docker/Dockerfile` /
  `docker/compose.yml` comments for the reasoning behind every
  non-obvious line).
- **Not yet done** (needs an actual Docker host — see the packaging
  kickoff doc / a Linux builder with internet access):
  - `docker compose -f docker/compose.yml config` — validates YAML +
    interpolation.
  - `docker build -f docker/Dockerfile -t iolbox:test .` — real build,
    including the vpcs source build and the i386 multiarch apt install.
  - `hadolint docker/Dockerfile`, if available.
  - Boot smoke: bring the container up, confirm the GUI loads at `:4001`,
    start an IOL node with an uploaded image, confirm the console (telnet)
    and a capture tee (Wireshark) both work host-side, then
    `docker compose down && up` and confirm the same iourc/hostid persist
    (`docker exec iolbox cat /etc/hostid` before/after; the license should
    keep validating without a re-flag from IOL).
