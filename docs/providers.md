# Runtime providers (contract)

A **provider** hosts the Linux environment that runs the supervisor + IOL + VPCS.
The Windows-side app (Rust core in Tauri) selects and drives one. The provider
interface is deliberately tiny so new backends are cheap.

## Interface (Rust trait, conceptual)

```rust
trait Provider {
    fn id() -> &'static str;                 // "vmware" | "wsl2" | "remote" | "qemu"
    fn detect() -> Detection;                // available? preferred? why/why not
    fn provision(&self, appliance: &Path) -> Result<()>;  // one-time import/setup
    fn start(&self) -> Result<Endpoint>;     // boot runtime, return control endpoint
    fn stop(&self) -> Result<()>;
    fn endpoint(&self) -> Option<Endpoint>;  // { host, controlPort } on Windows localhost
    fn sync_image(&self, local: &Path) -> Result<String>; // copy image into runtime, return remote path
    fn health(&self) -> Health;
}
```

`Endpoint` is always reachable from Windows as `host:port` (usually
`127.0.0.1:4000`). Everything above the provider (GUI, supervisor protocol, lab
format, consoles, capture) is identical regardless of which provider is active.

## Selection & preflight (first run)

Detect and rank; present a plain-English choice, never auto-flip system features.

| Provider | `detect()` logic | Rank |
|---|---|---|
| `vmware` | `vmrun.exe` found (Workstation or Player) | **1 on VMware machines** |
| `wsl2` | `wsl.exe` present AND Hyper-V/VMP already enabled | 1 if enabled, else offered-with-warning |
| `remote` | user-configured host reachable | manual |
| `qemu` | always (bundled) | last resort |

**Critical rule:** never enable the Windows Hypervisor Platform / Hyper-V
automatically — it degrades VMware Workstation and kills nested virtualization.
If WSL2 would require enabling it, mark WSL2 "requires a Windows feature that will
affect VMware" and default to `vmware`.

## vmware (primary)

- Ship a small appliance (`iolbox-appliance.vmx` + `.vmdk`, Debian-slim + kernel +
  supervisor autostart). 2 vCPU / 4 GB RAM (IOL nodes default to 1 GB each),
  **two NICs**: host-only (control/GUI/console/capture) + NAT (the NAT node's
  MASQUERADE outbound path). Same shape as the proven iolbox-rt reference VM.
- Lifecycle: `vmrun -T ws start <vmx> nogui` / `stop`. Player: `-T player`.
- Endpoint discovery: `vmrun getGuestIPAddress <vmx> -wait` (open-vm-tools is
  in the image), or match the appliance's fixed host-only MAC
  `00:50:56:3f:ab:01` in `C:\ProgramData\VMware\vmnetdhcp.leases`. The
  host-only address is a DHCP lease — subnets differ per Workstation install,
  so no IP is baked in. GUI/WS bridge at `http://<ip>:4001`.
- Image sync: the GUI's image upload (`POST /api/upload/image` on :4001) —
  no vmrun guest-auth or shared folders needed.
- **De-risked**: this is the same `vmrun`-managed-VM pattern already used daily for
  the PNetLab gate VMs.

## wsl2

- `wsl --import iolbox <dir> iolbox-rootfs.tar` (first run). No Store distro needed.
- Endpoint: WSL2 localhost forwarding → `127.0.0.1:4000` directly.
- Image sync: images live on a Windows path visible at `/mnt/...`, or copied in.
- Fastest cold start, smallest footprint. Chosen only when Hyper-V already on.

## remote

- User supplies `ssh user@host`. App scps the supervisor binary + starts it,
  tunnels control/console/capture ports over SSH. Also the CI/headless mode.

## qemu (compatibility)

- Bundled `qemu-system-x86_64` + tiny kernel/initrd running the supervisor, TCG
  (no hypervisor). Slow (IOL idle spin hurts), but conflicts with nothing.
- Endpoint: user-mode net with hostfwd for control/console/capture ports.

## Appliance / rootfs build

Both the VMware appliance and the WSL tar come from **one** Debian-slim rootfs:
- glibc + `libc6:i386` (32-bit IOL), minimal userspace + what the supervisor
  shells out to at runtime: `sudo`, `iproute2`, `procps` (sysctl), `iptables`
  (NAT node)
- `/opt/iolbox/supervisor` (Go binary, GUI embedded — build via
  `build-release.sh`) started by systemd on boot with the full proven flag set
  (`-ws-addr 0.0.0.0:4001`, `-console-bind`/`-capture-bind 0.0.0.0`, labs/run
  dirs)
- hostname pinned to `iolbox` (`/etc/hostname` + `/etc/hosts` + `wsl.conf`) —
  the iourc is keyed to it and `sudo` stalls without the hosts line
- iourc generated on first boot from the runtime's own hostid
- default route on the NAT NIC (the NAT node needs outbound; the old
  no-default-route posture is retired — see runtime/README.md)

Build scripts live in `runtime/`. Reproducible; nothing Cisco is ever baked in.

## Apple Silicon macOS: Lima provider

The Apple Silicon path is the Go `iolbox` launcher plus a user-installed Lima;
it is not a resurrection of the conceptual Windows Tauri provider. Lima is an
external Apache-2.0 prerequisite. iolbox detects it and uses it, but never
installs, upgrades, reinstalls, or removes it.

- **Preflight and canary:** require Apple Silicon macOS, the selected supported
  macOS/profile qualification row, Lima VZ, and the Rosetta binfmt/canary
  gate. A READY Lima VM without a passing Rosetta canary is not sufficient.
  Diagnostics report `guest_arch=aarch64`, `execution=rosetta-amd64`, the
  measured kernel, service/HTTP readiness, and the live canary result.
- **Profiles:** the shipped locked table uses Debian 13/trixie as DEFAULT with
  the pinned `6.12.101+deb13-cloud-arm64` kernel; Ubuntu 22.04/Jammy is the
  pinned COMPATIBILITY profile on the 5.15 line; Debian 12/bookworm is an
  unqualified candidate while its digest is `PIN-ME`.
- **Durable guest:** the default machine is named `iolbox-debian13`. `start`
  creates it if absent or validates/reuses it if present; `upgrade` requires
  the existing machine and replaces the guest payload without deleting the
  VM or its data; `stop` only stops it.
- **Host-loopback contract:** GUI is `127.0.0.1:4001`; guest control is not
  forwarded; console ports are host loopback `9000-9049` and capture ports are
  host loopback `5500-5529`. The GUI has no authentication, so these forwards
  must not be exposed beyond the Mac without an independently secured tunnel.
- **Host-folder synchronization:** by default,
  `~/Library/Application Support/iolbox/images` and
  `~/Library/Application Support/iolbox/labs` synchronize with the running
  guest. Stop performs the final lab/image sync while the guest is still up.
  `--no-sync`, `--images-dir`, and `--labs-dir` are explicit user choices.
- **Capability boundary:** the shipped runtime supports x86_64 IOL through
  Rosetta only. i386/i86bi and arm64-native IOL are unsupported; this provider
  does not imply the separate M7/native-arm64 work.
- **Deletion boundary:** ordinary launcher removal is non-destructive. A
  destructive reset is a separately warned, literal operation that may delete
  only the named `iolbox-debian13` Lima machine and move the exact iolbox host
  data/attestation paths to uniquely named Trash destinations after verifying
  the machine is stopped. It never means deleting all of `~/.lima`, all of
  `~/.iolbox`, or the user's home data.
