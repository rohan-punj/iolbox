# M0 result — Apple Silicon feasibility gate: **GO**

Executed 2026-08-14 on real hardware. This supersedes the M0 assumptions in
`docs/macos-arm64-plan.md`; several of them were wrong in ways that matter.

## Verdict

**GO.** Two real Cisco IOL 17.18.02 routers booted and passed traffic on an
Apple M1, running the **existing unmodified amd64 iolbox payload**
(`iolbox-server-v0.4.1.tar.gz`, built for linux/amd64 on the Linux builder),
translated by Apple Rosetta inside an arm64 Linux VM.

No product code was changed. No arm64 port was required.

```
Sending 10, 100-byte ICMP Echos to 10.0.0.2, timeout is 2 seconds:
.!!!!!!!!!
Success rate is 90 percent (9/10), round-trip min/avg/max = 1/5/37 ms
```

(The single loss is first-packet ARP resolution, not a translation fault.)

## Test bed

| Item | Value |
|---|---|
| Host | MacBook Air, Apple M1, 8 GB RAM, macOS **13.5** (22G74) |
| Rosetta | preinstalled (`oahd` running) |
| Machine manager | **Lima 2.2.0** (Apache-2.0), `--vm-type=vz --rosetta` |
| Guest | Ubuntu **22.04**, kernel **5.15.0-185-generic**, aarch64, 4 vCPU / 4 GiB / 15 GiB |
| Payload | `iolbox-server-v0.4.1.tar.gz`, unmodified, `install.sh --bind all` |
| Image | `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`, sha256 `b858503827356c55…` |

OrbStack was attempted first and is **not usable on this host**: OrbStack 2.2.3
declares `LSMinimumSystemVersion 14.0` and this Mac runs 13.5.

## The blocking discovery: Rosetta is kernel-version-sensitive

**Ubuntu 24.04 (kernel 6.8) fails outright.** Every x86_64 binary — the glibc
loader, and IOL itself — dies at exec:

```
rosetta error: unhandled auxillary vector type 28
timeout: the monitored command dumped core
```

Auxv type 28 is `AT_RSEQ_ALIGN`, which Linux began emitting in 6.3. The Rosetta
build shipped with macOS 13.5 does not understand it and aborts the process.

**Ubuntu 22.04 (kernel 5.15) works.** That kernel emits no rseq auxv entries
(`LD_SHOW_AUXV=1` confirms), and the same binaries run.

Consequence for the plan: the guest image is **not** a free choice. Either pin a
kernel < 6.3, or require a macOS whose Rosetta handles the newer auxv. This
constraint is absent from the current plan and from the GNS3/OrbStack prior art,
and it would have been found only on hardware.

## What was proven

| Criterion | Result |
|---|---|
| Rosetta binfmt registered | `enabled`, interpreter `/mnt/lima-rosetta/rosetta`, magic `…02003e00` (EM_X86_64) |
| `/dev/net/tun` present | yes |
| systemd + passwordless sudo | yes |
| amd64 multiarch in arm64 guest | yes, after pointing amd64 at `archive.ubuntu.com` (see gotcha below) |
| x86_64 glibc loader executes | `ld.so (Ubuntu GLIBC 2.35-0ubuntu3.14)` |
| Real IOL executes | prints its own usage banner + `IOURC: Could not open iourc file` |
| `install.sh` on arm64 host | installs clean; **warns** on non-x86_64 uname and continues (`install.sh:74-78`) |
| Supervisor as systemd service | `active`, run via `/mnt/lima-rosetta/rosetta /opt/iolbox/supervisor …`, cgroup delegation OK |
| GUI on :4001 | HTTP 200 |
| Image scan + classify | `registered 1 image(s)` → `arch: x86_64, class: l3` (L2/L3 sniffer works under translation) |
| IOL licence generation | `install.sh` generated `/opt/iolbox/iourc` from hostid+hostname |
| Two-node lab start | both nodes `running` within **10 s** of `lab.start` |
| NVRAM startup-config injection | `Ethernet0/0  10.0.0.1  YES NVRAM  up  up` |
| Data plane (p2p link) | **ping 90% (9/10), 1/5/37 ms** |
| Restart persistence | after VM stop/start: service active, GUI 200, image `0 hashed, 1 from cache`, iourc intact, hostid stable, no stale taps |

Resource cost at idle with two IOL running: load average **0.07**, 889 MiB of
3.8 GiB used, ~530 MiB RSS per IOL node.

## Gotchas found

1. **arm64 Ubuntu multiarch does not serve amd64 from `ports.ubuntu.com`.**
   Adding `dpkg --add-architecture amd64` yields 404s on every index. amd64
   packages live on `archive.ubuntu.com`/`security.ubuntu.com`. The fix is to
   pin the existing entries to `[arch=arm64]` and add amd64-only sources. Any
   guest provisioner must do this.
2. **`ld.so --version` invoked directly is a valid Rosetta canary** — it
   reproduces the auxv failure without needing an IOL image, so the guest
   image can be gated before shipping any Cisco binary.
3. **`hello` advertises an `i386` feature it cannot honour here.** Rosetta is
   64-bit only, so i86bi images cannot run. The capability string is a lie on
   this platform and should be suppressed for the macOS target.
4. The supervisor reports `arch: x86_64` and `runtime: debian-slim-12` — it
   cannot tell it is translated. Fine functionally; misleading in diagnostics.

## What is NOT proven

- Four-node labs (8 GB host; not attempted).
- Capture / Wireshark tee, NAT/extnet, VPCS-in-lab, multi-link fabrics.
- Sustained soak (longest observed run ~12 minutes).
- Browser GUI interaction — the GUI was verified by HTTP status and control
  API only, never driven from a browser.
- Lima port-forwarding of console/capture ranges to the macOS side (the lab
  was driven from inside the guest).
- Any macOS ≥ 14 host, and therefore the OrbStack path entirely.

## Implications for the plan

- The MVP is **packaging and provisioning**, not porting. Rank the Lima path
  first; it is licence-free and worked on the oldest supported macOS.
- Add a hard guest-kernel constraint (< 6.3) or a Rosetta-capability preflight
  using the `ld.so` canary, and fail fast with a clear message.
- The optional native-arm64 workstream (branch `luna/macos-arm64-invariant`)
  buys independence from this Rosetta/kernel coupling. That is now its
  clearest justification, beyond performance.
