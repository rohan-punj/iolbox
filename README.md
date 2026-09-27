# iolbox

**Lightweight network labs for Cisco IOL and VPCS, with a browser-based GUI.**
Draw a topology, open node consoles, and capture link traffic in Wireshark.
Runs on Windows, Linux, and Apple Silicon macOS.

<img width="1787" height="1052" alt="iolbox topology canvas and node consoles in light mode" src="https://github.com/user-attachments/assets/f8aa2e64-42ae-422e-9acc-f41f2f60681f" />

<details>
<summary>Dark mode</summary>

<img width="1784" height="1050" alt="iolbox topology canvas and node consoles in dark mode" src="https://github.com/user-attachments/assets/4ccc8558-098d-4740-b311-c4cb1b107635" />

</details>

## Features

- Drag-and-drop topologies with IOL L2/L3 and VPCS nodes.
- Image library, per-node consoles, and external terminal support.
- Live Wireshark capture and recording to `.pcapng`.
- Startup-config save/restore and portable JSON lab files.

**No Cisco software is included.** Supply images you lawfully obtained and are
licensed to use. See [image setup](docs/INSTALL.md#first-steps-after-install).

## Download

Choose a package from the [latest release](https://github.com/rohan-punj/iolbox/releases/latest).
Verify it against [SHA256SUMS.txt](https://github.com/rohan-punj/iolbox/releases/latest/download/SHA256SUMS.txt).

| Platform | Download | Start here |
|---|---|---|
| Windows x64 | [Windows bundle](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-windows-amd64.zip) | Extract and run `iolbox-launcher.exe`; QEMU and the guest disk are included. |
| Apple Silicon Mac | [Mac archive](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-macos-arm64.tar.gz) | Install Lima separately, then follow the [Mac setup](packaging/macos/README.release.md). |
| Linux x64 with systemd | [Linux installer](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-linux-amd64.tar.gz) | Extract, enter the `iolbox-server-<version>` folder, and run `sudo ./install.sh`. |
| VMware Workstation | [VMware bundle](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-vmware-amd64.zip) | Extract and open `iolbox-vmware.vmx`. |
| ESXi / VirtualBox / OVF import | [OVA appliance](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-appliance-amd64.ova) | Import the appliance and start it. |
| WSL2 | [WSL rootfs](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-wsl-amd64.tar) | Import with WSL; see the [installation guide](docs/INSTALL.md#3-wsl-rootfs-wsl2). |
| Proxmox LXC | [LXC template](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-lxc-amd64.tar.zst) | Upload as a CT template; follow the [device setup](docs/INSTALL.md#4-proxmox-lxc). |

The Windows and Mac launchers are unsigned. Windows may show a SmartScreen
warning. On Mac, verify checksums and inspect quarantine before running the
launcher; the [Mac guide](packaging/macos/README.release.md#quick-start) explains
the CLI and optional `IOLbox.app` paths. Mac requires Apple Silicon and
user-installed Lima, and supports x86_64 IOL images.

The Mac release also provides its [archive checksum](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-macos-arm64.tar.gz.sha256)
and [corresponding source](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-macos-arm64-corresponding-source.tar.gz)
for redistributed packages. A [standalone QEMU disk](https://github.com/rohan-punj/iolbox/releases/latest/download/iolbox-disk.qcow2)
and [capture helper](https://github.com/rohan-punj/iolbox/releases/latest/download/capture-helper.exe)
are available for existing setups.

## First lab

1. Start your chosen runtime. The launcher opens the GUI in your browser;
   for an appliance, browse to `http://<vm-ip>:4001`.
2. Add your licensed IOL image. On Windows, place it in `images\` next to
   the launcher before starting; other targets support GUI image upload.
3. Drag nodes onto the canvas, connect them, select their images, and start the lab.
4. Open a node console, or right-click a link to capture traffic. Install
   Wireshark separately for live viewing.

Full instructions: [installation](docs/INSTALL.md),
[images and capture](docs/INSTALL.md#first-steps-after-install), and
[building from source](docs/build.md).

## Security and license

iolbox is a single-user tool with **no authentication**. Keep its GUI, console,
and capture endpoints on localhost, an isolated trusted network, or a secured
tunnel; do not expose them publicly. See [security guidance](docs/INSTALL.md#sizing-and-security).

iolbox is independent of Cisco and is not endorsed by Cisco Systems.
You are responsible for your image licenses. See [LICENSE](LICENSE) and
[third-party notices](THIRD_PARTY.md).
