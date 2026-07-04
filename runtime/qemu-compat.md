# qemu-tcg compatibility provider

Notes for the `qemu` provider from `docs/providers.md` ("qemu (compatibility)")
— the zero-prerequisite fallback when neither VMware nor an enabled Hyper-V
platform (for `wsl2`) is available. Bundled `qemu-system-x86_64`, software
emulation only (TCG, no KVM/WHPX acceleration), so it's slow — IOL's known
idle-CPU-spin behavior (see PLAN.md "Strip the `-l` keepalive flag" and the
`iol-l1-keepalive-cpu-noble` memory topic) hurts more here than under any
hardware-accelerated provider. Treat this as "it works everywhere" insurance,
not a first choice.

## Reuses the VMware disk, unmodified

This is the whole point of building one rootfs and one disk image: the exact
`iolbox-appliance.raw`/`.vmdk` produced by `pack-vmware.sh` (before or after
the raw→vmdk conversion — qemu reads vmdk natively) boots fine under
`qemu-system-x86_64` too. No separate "qemu rootfs" build step exists or is
needed. If only the `.vmdk` was kept (typical release artifact) and the
intermediate `.raw` was discarded, `qemu-img convert -f vmdk -O raw
iolbox-appliance.vmdk iolbox-appliance.raw` reverses it if a raw file is
preferred, but qemu can also boot the vmdk directly with `-drive
file=iolbox-appliance.vmdk,format=vmdk`.

## Launch command (reference)

```sh
qemu-system-x86_64 \
    -machine pc \
    -accel tcg \
    -m 768 \
    -smp 1 \
    -drive file=iolbox-appliance.vmdk,format=vmdk,if=virtio \
    -netdev user,id=net0,hostfwd=tcp:127.0.0.1:4000-:4000,hostfwd=tcp:127.0.0.1:9000-:9000,hostfwd=tcp:127.0.0.1:9001-:9001,hostfwd=tcp:127.0.0.1:5500-:5500 \
    -device virtio-net-pci,netdev=net0 \
    -display none \
    -serial none \
    -no-reboot
```

Notes on each piece:

- `-accel tcg`: explicit, not left to qemu's default-picks-best-available
  logic — this provider exists specifically for machines where KVM/WHPX/HAXM
  are NOT usable (that's why VMware/WSL2 weren't picked), so silently
  upgrading to an accelerator here would defeat the "always works" premise
  and reintroduce the same VMware-vs-Hyper-V conflict this provider is meant
  to avoid.
- `if=virtio` for the disk: the appliance's on-disk kernel is
  `linux-image-amd64` (see `pack-vmware.sh`'s Stage 3 comment), a generic
  Debian kernel that includes `virtio_blk`/`virtio_net` modules built-in on
  Debian's default config — no separate virtio-enabled kernel build needed
  despite the vmx template using `lsilogic`/`e1000` instead (both are also
  supported by the same generic kernel; qemu-tcg is simply given the faster
  virtio path here since there's no vmx to keep IDE/SCSI-compatible for).
- **hostfwd ports**: control (4000), two example console ports (9000/9001 —
  the actual per-node allocation is dynamic and reported back over the
  control protocol per `docs/protocol.md`; a real qemu-provider
  implementation needs to either pre-reserve a wide hostfwd range at launch
  time, since qemu can't add port forwards to a running `-netdev user`
  instance, or renegotiate/relaunch when the supervisor reports new port
  allocations — this is a known limitation called out here for the
  provider-implementation team, not solved by this runtime layer), and one
  example capture port (5500). A real implementation should reserve a
  block (e.g. 9000-9099 consoles, 5500-5599 capture) up front.
- `-display none -serial none`: fully headless; the appliance is reached
  exclusively over the forwarded TCP ports, same as the other providers.
- `-no-reboot`: a guest kernel panic should surface as "provider stopped
  unexpectedly" to the GUI rather than qemu silently looping a reboot.

## No static host-only IP needed here

Unlike the `vmware` provider (fixed `192.168.171.2`, see `pack-vmware.sh`),
qemu's `-netdev user` mode is host-NAT-like from the guest's perspective but
exposes ports back to the *host* as plain `127.0.0.1:<port>` — so the
Windows-side (or Linux CI-side) provider code always talks to `127.0.0.1`
with the hostfwd ports above, never a guest-assigned IP. This is actually
the simplest endpoint story of all four providers.

## No default route: still true here

`-netdev user` gives the guest a default route to qemu's built-in NAT/DHCP
by default (unlike VMware host-only, which has no upstream at all). The
in-guest `files/01-no-default-route.network` config (DHCP for address only,
`UseGateway=false`) is what prevents the guest from actually installing that
default route — so the xml.cisco.com abort protection (see
`runtime/README.md`) still holds under qemu, but it's enforced entirely
in-guest here rather than also being enforced by the network topology itself
(as host-only mode gives for free under VMware). Don't swap `-netdev user`
for `-netdev socket`/tap thinking it's "more secure" without re-checking this
assumption.

## Not wired into build-all.sh

`build-all.sh` only orchestrates `pack-wsl.sh` and `pack-vmware.sh` — there
is no `pack-qemu.sh`, because there is nothing to pack; the vmware artifact
already is the qemu artifact. A future `remote`/CI runner that wants a
ready-made qemu launch script (rather than the reference command above) can
add a thin `run-qemu.sh` wrapper without touching the build pipeline at all.
