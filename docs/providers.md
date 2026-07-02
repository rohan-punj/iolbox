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

- Ship a small appliance (`iolab-appliance.vmx` + `.vmdk`, Debian-slim + kernel +
  supervisor autostart). ~768 MB RAM, host-only NIC.
- Lifecycle: `vmrun -T ws start <vmx> nogui` / `stop`. Player: `-T player`.
- Endpoint discovery: `vmrun getGuestIPAddress <vmx>` then control port on the
  host-only network; or a fixed host-only IP baked into the appliance.
- Image sync: `vmrun -gu <user> -gp <pw> CopyFileFromHostToGuest`, or a VMware
  shared folder mounted at `/opt/iolab/images`.
- **De-risked**: this is the same `vmrun`-managed-VM pattern already used daily for
  the PNetLab gate VMs.

## wsl2

- `wsl --import iolab <dir> iolab-rootfs.tar` (first run). No Store distro needed.
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
- glibc + `libc6:i386` (32-bit IOL), minimal userspace
- `/opt/iolab/supervisor` (Go binary) started by systemd/tiny init on boot
- no default route (avoids the `xml.cisco.com` L2-IOL abort), host-only/loopback
- iourc generated on first boot from the runtime's own hostid

Build scripts live in `runtime/`. Reproducible; nothing Cisco is ever baked in.
