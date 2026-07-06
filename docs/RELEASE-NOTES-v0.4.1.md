# iolbox v0.4.1

A lightweight, Windows-native lab for Cisco IOL and VPCS. **Ships no Cisco
software** — supply your own IOL/IOU images and hold the appropriate
licenses for them.

This release folds in the v0.4.0 workstreams (node-start latency, refresh
survives a browser reload, LACP/LLDP passthrough) plus a v0.4.1 follow-up
round focused on trustworthiness of the shipped binaries and a real build fix.

## Highlights

- **Faster node start.** Starting/stopping a node no longer re-walks every
  fabric link in the lab: a warm `node.start` dropped from re-attaching the
  whole lab's links to skipping everything already correctly wired
  (kernel-state verified, not just bookkeeping). Measured on a 9-link lab:
  cold `lab.start` 1083 ms → 579 ms; warm `node.start` down to ~3 ms.
- **A browser refresh no longer kills your running lab.** Previously,
  reloading the GUI tore down and reloaded the lab from scratch — stopping
  every running node. The GUI now detects a lab that's already running and
  adopts it in place; the server does the same for a second tab opening the
  same lab. An explicit switch to a *different* lab still does a real
  teardown-and-load, as before.
- **LACP and LLDP now cross point-to-point links.** A userspace tee carries
  slow-protocol frames (LACP, plus LLDP/802.1X via a bridge `group_fwd_mask`
  change) across the static-tap fabric, so link-aggregation and neighbor
  discovery labs behave like real hardware.
- **Pre-login console banner.** Every appliance (OVA, VMware, WSL, LXC) now
  shows an IOLBOX splash on the VM console *before* login — the Web GUI URL,
  live service status, and build version — refreshed every 20s. No more
  guessing the IP from a bare Debian login prompt.
- **Smarter GUI caching.** The embedded browser GUI's static assets are now
  cached correctly: content-hashed bundle files are cached for a year
  (immutable — a rebuild always ships new URLs), while the app shell
  revalidates on every load via ETag, so an upgrade is picked up immediately
  instead of risking a stale cached shell requesting vanished assets.
- **Windows launcher: back to a plain, predictable exe.** An in-between
  build added a local web control-panel to the launcher; it's been reverted
  in favor of the original simple, predictable design (an unsigned exe
  binding a listener and accepting uploads at startup is exactly the shape
  that trips antivirus heuristics). In its place: the launcher now asks for
  vCPU/RAM interactively on a normal double-click (`vCPUs for the guest
  [4]:` / `RAM MB for the guest [4096]:` — Enter twice for defaults), while
  `--smp`/`--mem` or a non-interactive/scripted launch skip the prompt
  entirely.
- **Build fix: the repo now actually builds from a fresh clone.** A
  `.gitignore` pattern (`iourc*`) had been silently excluding the
  `supervisor/internal/iourc` source package from every commit — the
  packaged binaries were never affected (they were built from a local
  checkout that still had the untracked files on disk), but anyone cloning
  the repo and running `go build` would have hit `no required module
  provides package .../internal/iourc`. Fixed and verified by CI building
  clean from this tag.

## Downloads — which one do I want?

| Situation | Artifact |
|---|---|
| Windows desktop, simplest path | `iolbox-launcher-v0.4.1-windows.zip` (launcher + disk + bundled QEMU, all in one archive) |
| VMware Workstation / ESXi / VirtualBox | `iolbox-appliance-v0.4.1.ova` |
| WSL2 already enabled | `iolbox-rootfs.tar` |
| Proxmox homelab | `iolbox-ct-v0.4.1.tar.zst` |
| Existing Linux server | `iolbox-server-v0.4.1.tar.gz` |

Full step-by-step instructions for every target: **[docs/INSTALL.md](https://github.com/rohan-punj/iolbox/blob/main/docs/INSTALL.md)**.

The GUI comes up at `http://<host>:4001` — it has **no login of any kind**;
keep it on localhost or a network you trust.

> **Note on the Windows launcher exe:** it's unsigned. Some antivirus tools
> (Malwarebytes in particular) may flag a fresh, never-seen, unsigned Go
> binary heuristically on first run — this is a false positive, not a
> detection of anything in the binary. Code signing is planned for a future
> release.

## Verifying your download

`SHA256SUMS.txt` (attached to this release) lists the checksum for every
artifact:

```sh
sha256sum -c SHA256SUMS.txt
```

## What's not included

The raw VMware `.vmdk`/`.vmx` pair is intentionally not attached to this
release — the OVA above is the same appliance in a single, smaller,
standard-format file. If you specifically need the pre-converted raw disk,
open an issue and it can be added.

## Full changelog

`v0.3.2...v0.4.1` — see the [commit log](https://github.com/rohan-punj/iolbox/compare/v0.3.2...v0.4.1).
