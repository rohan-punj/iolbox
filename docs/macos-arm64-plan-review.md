# Review: docs/macos-arm64-plan.md (macOS Apple Silicon plan)

Scoped adversarial review, four axes: citation integrity, ruling scrutiny, phase
executability, sizing honesty. Reviewer is on Windows with no Apple hardware;
everything macOS-behavioral is marked owner-must-validate. No product code or
plan text was modified.

## 1. Verdict

The plan is sound enough to implement from: its architecture ruling
(VZ arm64 guest + Rosetta for the x86_64 IOL only, M0 gate first, remote/SSH
fallback) survives scrutiny, its citations are overwhelmingly accurate, and its
sequencing (M0 before any port work) is correct. The single most important
change it needs is to retract its central "arm64 blockers" evidence: the four
code locations it presents as amd64-specific defects that make the guest port
non-trivial (`reap_linux.go`, `vtap/shim_linux.go`, `dirstat`, `slowtee`) are
in fact already correct on linux/arm64 as written. M1 as specified would
"replace" code that needs no replacement, and risk #4's likelihood is inflated.
The guest-side Go port is close to a recompile; the real work is VPCS
compilation, the arm64 rootfs/packer, the VZ host launcher, and signing. A
second, cheaper improvement: add a Linux-only x86_64-userspace-translation
rehearsal (FEX/box64/qemu-user on any arm64 Linux box) that needs no Mac and
would de-risk the concept before Apple hardware is even secured.

## 2. Citation audit

Every citation that carries an architectural conclusion was opened and checked.
"VERIFIED" means the cited lines contain what the plan claims (line ranges
verified to within a few lines). Findings on *interpretation* are in section 3.

| # | Claim | Cited location | Status | What the code actually says |
|---|---|---|---|---|
| 1 | Launcher UX to preserve | `tools/iolab-launcher/README.md:3-21` | VERIFIED | Single exe boots guest, opens `http://localhost:4001`, qemu/wsl backends. |
| 2 | Launcher is dependency-free stdlib Go | `README.md:11-16`, `tools/iolab-launcher/go.mod` | VERIFIED | "Build (dependency-free, stdlib only)"; go.mod has zero requires. |
| 3 | Windows launcher install/UX docs | `docs/INSTALL.md:282-388` | VERIFIED | Section 6, QEMU disk (Windows, bundled launcher), through "It worked if". |
| 4 | Windows TCG boot already takes minutes | `docs/INSTALL.md:321-325` | VERIFIED | "Be patient on first boot / TCG ... is slow". |
| 5 | IOL 17.18.02 is x86_64 dynamically linked ELF, PTY console, unix DGRAM netio | `docs/p0-spike.md:54-87`, `:142-160` | VERIFIED | ELF64 x86-64, no i386 multiarch needed for 17.18; pty console confirmed; `/tmp/netio<uid>/<id>` unixgram convention, 8-byte header. |
| 6 | Repo distinguishes old i386 `i86bi` family | `docs/p0-spike.md:54-63` | VERIFIED | "i386 only matters for the older `i86bi_` images." |
| 7 | reap uses amd64-sized raw structs (portability blocker) | `supervisor/internal/tool/reap_linux.go:19-64` | WRONG (interpretation) | The code exists and is hand-laid-out, but the 128-byte siginfo layout (3×int32 preamble, union at offset 16) is identical on linux/arm64; `PR_SET_CHILD_SUBREAPER`, waitid flags are arch-independent; syscall numbers come from the per-GOARCH `syscall` package. Compile-time size assertion at lines 56-59 guards it. Not an arm64 defect. See finding F1. |
| 8 | amd64 `ifreq` layout in vtap shim (blocker) | `supervisor/internal/vtap/shim_linux.go:47-65` | WRONG (interpretation) | `[40]byte` ifreq and `TUNSETIFF = 0x400454CA` are byte-identical on arm64 (generic ioctl encoding, same struct size on all 64-bit Linux); `binary.LittleEndian` flags write is correct on little-endian arm64. Same for `iouyap/tap_linux.go`. See F1. |
| 9 | little-endian/amd64 byte-order assumptions in raw packet code | `dirstat/dirstat_linux.go`, `slowtee` | WRONG (interpretation) | `ethPAll = 0x0300 // htons(ETH_P_ALL) on little-endian amd64` — the *comment* says amd64; the value is correct on any little-endian arch, and arm64 Linux (and every Apple platform) is little-endian. Comment fix at most. See F1. |
| 10 | VPCS fetch is amd64-only, pinned v0.8.3, built from source | `runtime/fetch-vpcs.sh:12-26`, `:109-122` | VERIFIED | GNS3 fork pinned to tag v0.8.3, built from source with make, static link; sanity check warns unless output is x86-64 ELF64. The plan's M1 assumption (recompile the pinned source on arm64) is plausible — it is a source build, not a binary download. |
| 11 | IOL spawned under PTY by supervisor | `supervisor/internal/node/spawn_linux.go:72-149` | VERIFIED | `spawnIOL` exactly spans 91-150 with doc comment from 72; `pty.Start`, setsid+ctty, telnet bridge. |
| 12 | iouyap converts netio to tap frames | `supervisor/internal/iouyap/bridge_tap_linux.go:20-31`, `:67-76` | VERIFIED | TapBridge doc comment (netio header stripped, raw to tap fd) and ListenUnixgram/openTap at the cited lines. |
| 13 | tap/bridge via native `sudo -n ip` shell-outs | `supervisor/internal/fabric/commands.go:36-83`, `manager_linux.go:22-25` | VERIFIED | `ip tuntap add`/`ip link` argv builders; Manager doc says privileged `ip` via `sudo -n`. |
| 14 | capture is native tcpdump pipeline | `supervisor/internal/bcap/capture_linux.go:35-69` | VERIFIED | `sudo -n tcpdump -i <bridge> -w - -U -s 0 -n` piped through pcapng server. |
| 15 | Six existing runtime targets | `runtime/build-all-targets.sh:96-140` | VERIFIED | Orchestrates build-release + rootfs + WSL + VMware + OVA (+qemu/lxc/native below). |
| 16 | pack-qemu installs BIOS GRUB + `linux-image-amd64` | `runtime/pack-qemu.sh:38-46` | VERIFIED | "GPT, legacy-BIOS GRUB ... bios_grub p1 ... Debian's generic linux-image-amd64 (installed by lib-disk.sh)". Do-not-feed-arm64 conclusion is right. |
| 17 | build-release cross-compiles amd64 supervisor with embedded GUI | `build-release.sh:17-21` | VERIFIED | `npm run build:embed` then `GOOS=linux GOARCH=amd64 go build ... supervisor-linux-amd64`. |
| 18 | build-rootfs requires amd64 supervisor/VPCS | `runtime/build-rootfs.sh:42-46`, `:105-132` | VERIFIED | `SUPERVISOR_BIN=.../supervisor-linux-amd64`, `VPCS_BIN` defaults; hard-refusal checks with amd64 rebuild instructions. |
| 19 | build-rootfs bootstraps `--arch=amd64` | `runtime/build-rootfs.sh:255-266` | VERIFIED | mmdebstrap/debootstrap both `--arch=amd64`. |
| 20 | build-rootfs fetches/builds linux/amd64 helpers | `runtime/build-rootfs.sh:147-183` | VERIFIED (wording) | It *builds* (not fetches) toollaunch, secbench, and pack GUIs, all `GOOS=linux GOARCH=amd64`. Substance correct. |
| 21 | Remote provider "still a stub" | `app/src-tauri/src/providers/remote.rs:7-14`, `:43-60` | VERIFIED (file exists; framing see F4) | TODO(P1) block at 7-14; `provision/start/stop/sync_image` all return `NotImplemented` at 43-61. The file survives on the tree despite the browser-first pivot — my prior suspicion that the path was gone is REFUTED. But the whole Tauri tree is vestigial (CI builds "plain Go, no Rust/Tauri" per `runtime/README.md` CI section), so "stub" understates the integration cost — see F4. |
| 22 | Native server install path for remote fallback | `runtime/files/native/install.sh:1-18`, `:74-80` | VERIFIED | Header describes the native systemd x86-64 target; 74-80 is the `uname -m` x86_64 warning. |
| 23 | Port ranges 4001 / 9000-9049 / 5500-5529 | `tools/iolab-launcher/ports.go:13-24` | VERIFIED | Comment block documents exactly those ranges, loopback-only GUI. |
| 24 | Browser open is `cmd /c start` | `tools/iolab-launcher/main.go:172-180` | VERIFIED | `openBrowser` uses `exec.Command("cmd", "/c", "start", "", url)`. |
| 25 | `main.go:50-180` mixes portable entry with Windows ops | `tools/iolab-launcher/main.go:50-180` | VERIFIED | flag parsing/backend selection is portable; browser open is Windows-only, untagged. |
| 26 | `detect.go`/`wsl.go` are untagged but Windows-only in practice | `tools/iolab-launcher/detect.go`, `wsl.go` | VERIFIED | No build tags (`package main` directly), but hard-coded `wsl.exe`/`powershell.exe` invocations throughout. "Compile only for Windows" refactor boundary is right. |
| 27 | `qemu_windows.go` only console detachment; `qemu_other.go` no-op | those files | VERIFIED | 17-line `//go:build windows` file; 11-line `//go:build !windows` no-op. |
| 28 | `foldersync.go`/`wsclient.go`/`imagecache.go`/`ports.go`/`prompt.go` are portable Go | `tools/iolab-launcher/*` | VERIFIED | No Windows syscalls or exec; only comments mention `.exe`. `foldersync.go` is 592 lines, so ":32-576" is a fair span. |
| 29 | Images/labs dir chosen at `foldersync.go:486-498` | `tools/iolab-launcher/foldersync.go:486-498` | VERIFIED (with caveat) | `defaultSyncDirs` (exeDir\images, exeDir\labs) sits exactly there. Caveat: the `exeDir` argument is computed in `qemu.go:~200` from `os.Executable()`, so the macOS change touches the caller too, not just this function. |
| 30 | Sync session + final flush at `foldersync.go:506-576` | same file | VERIFIED | `startSyncSession`, `runPeriodicSyncOut`, `finalSyncOut` in that region. |
| 31 | Launcher startup/shutdown UX to preserve | `tools/iolab-launcher/qemu.go:192-218`, `:250-307` | VERIFIED | GUI-up log, folder-sync session, 30 s periodic sync, browser open; QMP powerdown with grace; `waitForGUI` HTTP-poll readiness. |
| 32 | `qemu.go:91-307` mostly portable, QEMU-specific | `tools/iolab-launcher/qemu.go` | VERIFIED | `buildArgs` starts at 91; `qmp.go` (91 lines) is QEMU-specific. |
| 33 | Supervisor not stdlib-only: creack/pty + x/net | `supervisor/go.mod` | VERIFIED | Exactly those two requires. |
| 34 | QEMU bundle GPL obligations precedent | `THIRD_PARTY.md:9-20`, `:79-91` | VERIFIED | Pinned Weil build table; GPLv2 compliance + written-offer text. |
| 35 | qcow2 disk 600-800 MB actual bytes | `runtime/README.md:193-200` | VERIFIED | "actual bytes ~600-800 MB with kernel + GRUB + open-vm-tools" (stated for the vmdk; same disk layout family). |
| 36 | Manual-builder upload pattern for CI-unbuildable artifacts | `runtime/README.md:239-272` | VERIFIED | CI section: three disk-image targets built and attached by hand; no nested virt on runners. |
| 37 | Draft-release job attaches assets, stays draft | `.github/workflows/release.yml:170-225` | VERIFIED | `release` job (tag-only) publishes via softprops with `draft: true`, body lists hand-attached artifacts. M7's "attach alongside current assets, keep draft" matches the existing mechanism. |
| 38 | `/api/health` may not exist (plan hedges) | M2 criterion | SETTLED: does not exist | Port 4001 routes (`supervisor/internal/wsbridge/wsbridge.go:143-156`): `/control`, `/console/`, `/capture/`, `/tool/`, `/api/upload/image`, and `/` (SPA). No health route anywhere. The repo's verified readiness probe is the launcher's own `waitForGUI`: HTTP GET `/` accepting any status < 500 (`qemu.go:~250-260`). M2/M3 should use that. |
| 39 | Native console emits `telnet://` URL | `app/src/lib/labStore.svelte.ts:1586-1617` | VERIFIED | `openNativeConsole` builds `telnet://host:port` and clicks a transient anchor. |
| 40 | Capture helper concept portable, discovery Windows-only | `tools/capture-helper/main.go:1-14`, `:203-219` | VERIFIED | Standalone pcapng-to-Wireshark pipe; `findWireshark` checks `C:\Program Files\...` plus PATH. |
| 41 | UI already constructs Wireshark commands + download | `app/src/lib/components/PaneBody.svelte:36-51`, `:120-153` | VERIFIED | `wiresharkCmd`/`wiresharkCmdFull` (hard-coded Windows full path); Save `.pcapng` + copyable live-attach commands. |
| 42 | Netio header is 8 bytes, arch-independent wire format | `supervisor/internal/iouyap/header.go` (plan implies via bridge citations) | VERIFIED | Explicit big-endian encode/decode of a byte-defined 8-byte header, confirmed against real IOL wire bytes. No struct-packing or pointer-size hazard across the Rosetta seam. |

External claims (Apple Rosetta-for-Linux procedure, entitlement names and the
flagged-unverified `com.apple.security.virtualization.rosetta`, QEMU
HVF-is-aarch64-only, GitHub hosted-runner nested-virt limits, Lima/Docker
licensing) are cited from documentation, hedged where unproven, and are
appropriately marked ASSUMPTION TO VALIDATE. I found no repo claim stated as
verified that the repo cannot support. The plan's honesty discipline here is
genuinely good.

## 3. Findings (ranked)

### F1 — MAJOR: the four named "arm64 blockers" are not blockers; M1's prescription is wrong

**What is wrong.** Section 1 and M1 present `reap_linux.go:19-64`,
`vtap/shim_linux.go:47-65` (plus the mirrored `iouyap/tap_linux.go`), and
byte-order assumptions in `dirstat`/`slowtee` as amd64-specific code that must
be "replaced"/"corrected" before the guest runs on arm64. This is the plan's
main evidence that the guest port is non-trivial. On inspection, all four are
already correct on linux/arm64:

- `reapSiginfo` (128 bytes; 3×int32 preamble, 8-byte-aligned union at offset
  16) matches the generic 64-bit Linux siginfo used by arm64 identically to
  x86-64. The comment cites x86-64 but the layout is shared. Syscall numbers
  (`SYS_WAITID`, `SYS_PRCTL`) are supplied per-GOARCH by package `syscall`;
  `PR_SET_CHILD_SUBREAPER` and the waitid flag values are arch-independent.
  The compile-time size assertion (lines 56-59) would catch any real mismatch.
- `TUNSETIFF = 0x400454CA` uses the generic ioctl encoding shared by x86-64
  and arm64; `struct ifreq` is 40 bytes on both; the `binary.LittleEndian`
  flags write is correct because arm64 Linux is little-endian.
- `ethPAll = 0x0300` is htons(ETH_P_ALL) on any little-endian arch. Only the
  comments say "amd64".

**Evidence.** Code quoted above (audit rows 7-9). The only genuinely
arch-specific inputs (syscall numbers) are handled by the toolchain.

**Correction.** Rewrite the Section 1 risk framing and M1: the supervisor is
expected to be a clean `GOOS=linux GOARCH=arm64` recompile
(CGO_ENABLED=0, pure Go, x/net + creack/pty are both arch-clean). M1 keeps
its *tests* (arm64 build, reap/tap/AF_PACKET/capture live exercise on arm64
Linux — that verification is still worth one session) but drops "replace" and
"correct" as work items, and updates the three "little-endian amd64" comments
to say "little-endian 64-bit Linux". Downgrade risk #4 likelihood from Medium
to Low. The honest long poles are VPCS arm64 compilation, the arm64 rootfs
target, and the VZ host launcher — not supervisor ABI work.

### F2 — MAJOR: option analysis omits non-Rosetta x86_64 userspace translators, losing a Mac-free rehearsal and a better NO-GO fallback

**What is wrong.** The execution-layer table jumps from Rosetta (row 1) to
full-system QEMU TCG (row 2). It never considers running the x86_64 IOL under
a userspace translator — FEX-Emu, box64, or qemu-user-static — inside the same
plain arm64 Linux guest. That family matters twice:

1. **Pre-M0 rehearsal without a Mac.** The project currently has no Apple
   Silicon hardware (risk #2, likelihood High). IOL-under-userspace-translation
   can be exercised today on any arm64 Linux box or `ubuntu-24.04-arm` runner:
   same PTY spawn, same unixgram netio, same iouyap seam. A pass strongly
   raises confidence that IOL tolerates translation (no exotic syscalls,
   no self-modifying-code surprises); a SIGILL/SIGSYS failure identifies the
   exact opcode/syscall to check against Rosetta before buying hardware.
   It is not proof of Rosetta (different translator), so M0 still gates — but
   it converts weeks of blocked waiting into evidence.
2. **Cheaper NO-GO fallback.** If Rosetta fails on a specific instruction but
   FEX/qemu-user works, an arm64 guest + userspace translator preserves the
   native-kernel data plane and avoids full-system TCG's cost. The plan's
   NO-GO branch goes straight to remote-or-TCG and never evaluates this.

**Evidence.** Option table rows 1-5 (`docs/macos-arm64-plan.md:25-31`);
risk #2; M0 dependency list ("This Windows workstation cannot execute this
gate").

**Correction.** Add a row 1.5 to the option table and a pre-M0 (or
M0-parallel) spike: run the supported L2/L3 IOL images under FEX-Emu and/or
qemu-user-static on arm64 Linux, record boot/ping/soak results. Keep Rosetta
as the shipping primary (Apple-supported, highest fidelity); position the
translator row explicitly as rehearsal + fallback, not product.

### F3 — MINOR: M2's readiness criterion names a route that does not exist; the repo's actual probe is GET /

**What is wrong.** M2's acceptance criterion is
`curl http://127.0.0.1:4001/api/health`, hedged with "(or the repository's
verified health route at implementation time)". This review settles it: there
is no health route. The wsbridge mux registers `/control`, `/console/`,
`/capture/`, `/tool/`, `/api/upload/image`, and the SPA at `/`
(`supervisor/internal/wsbridge/wsbridge.go:143-156`). The existing readiness
mechanism the whole Windows launcher relies on is `waitForGUI`: HTTP GET `/`
until any response with status < 500 (`tools/iolab-launcher/qemu.go:~250`).

**Correction.** Replace the M2 criterion with `curl -sf http://127.0.0.1:4001/`
returning the GUI index (or reuse `waitForGUI` semantics), and delete the
hedge. If a dedicated health route is wanted, that is a new cross-platform
supervisor feature and must be named as work, not assumed.

### F4 — MINOR: "the legacy remote provider is still a stub" understates the remote-integration cost

**What is wrong.** Row 5's cost cell leans on `app/src-tauri/src/providers/
remote.rs` being "still a stub". The citation is accurate (all methods return
`NotImplemented`), but the framing implies an integrated remote provider means
finishing that stub. The Tauri tree is vestigial: CI builds "plain Go, no
Rust/Tauri" (`runtime/README.md`, CI section), and the shipping host product
is the Go launcher. An integrated Mac remote client would be new launcher-side
work (SSH forwards + foldersync over the existing WS protocol) with no scaffold
to finish.

**Correction.** Reword row 5: "the documented fallback is the manual SSH
workflow against `runtime/files/native/install.sh`; an *integrated* remote
provider is new launcher work — the Tauri `remote.rs` stub is dead code and
not a head start." This keeps the (correct) low-cost claim for the manual
workflow while pricing the integrated variant honestly.

### F5 — MINOR: M2 is two phases wearing one label

**What is wrong.** M2's acceptance criteria mix (a) building and booting an
arm64 rootfs/kernel/initrd — doable entirely on Linux — with (b) proving the
Rosetta binfmt handler and an IOL prompt inside the guest, which requires a
Mac. The plan admits this ("access to a real Mac for the Rosetta half") but
still bills it as one focused session. The rootfs parameterization alone
(build-rootfs currently hard-codes amd64 in at least four places, audit rows
18-20, plus the i386 multiarch stage) is a full session.

**Correction.** Split: M2a (Linux-only: arm64 rootfs + native supervisor boots
to multi-user, GET / succeeds) and M2b (Mac: Rosetta mount/binfmt services +
IOL prompt). M2a can proceed in parallel with M0 hardware acquisition; M2b
depends on M0's harness.

### F6 — MINOR: the mac target silently drops the i386 IOL capability the other six targets ship

**What is wrong.** The plan excludes i86bi/i386 images as out of scope (correct
citation of `docs/p0-spike.md:54-63`), but does not state that this is a
*parity regression*: the current rootfs deliberately installs i386 multiarch
(`INCLUDE_I386=1`, "docs/providers.md requires libc6:i386",
`runtime/build-rootfs.sh:46` and its Stage 2), and the control protocol
advertises an `i386` feature (`docs/p0-spike.md:27`). Rosetta for Linux does
not translate 32-bit x86, so the exclusion is forced, not chosen — the plan
should say so and surface it to the owner (it is absent from Section 7's
open questions).

**Correction.** Add one sentence to Constraints and one owner question: "the
macOS target cannot run i86bi images at all (Rosetta is 64-bit only), unlike
every other target — confirm this parity loss is acceptable and document it in
INSTALL.md."

## 4. Ruling challenge

**Position: concur-with-changes.**

- The primary ruling (VZ arm64 guest + Rosetta translating only IOL, notarized
  arm64 DMG, remote/SSH documented fallback, all conditional on M0) is the
  right shape. The translation boundary is genuinely clean: I checked the seam
  the ruling depends on — IOL talks to native code only through byte-defined
  interfaces (8-byte big-endian netio header over unixgram sockets,
  `iouyap/header.go`; PTY byte streams; files). No struct-packed,
  pointer-sized, or fd-passing structure crosses the translated/native
  boundary, so a translated x86_64 IOL against a native arm64 supervisor has
  no ABI hazard beyond Rosetta's own fidelity, which M0 gates. The plan's
  claim that tap/netlink/AF_PACKET/tcpdump all stay native is confirmed by the
  code (audit rows 12-14).
- M0-before-M1 ordering is genuinely optimal: M0 needs only a stock arm64
  distro, Rosetta, amd64 libraries, and the IOL binaries — p0-spike proved
  two IOL instances forward traffic via NETMAP with no supervisor at all, so
  nothing in M1 is required to run M0, and a NO-GO wastes no port work.
- Changes required: (1) adopt F2 — evaluate FEX/box64/qemu-user as a Mac-free
  rehearsal and as the preferred NO-GO fallback ahead of full-system TCG;
  (2) adopt F1 — the ruling's supporting narrative that the guest contains
  arm64 blockers is wrong and should not be the justification for M1's size;
  (3) remote-as-primary was considered and I agree with its rejection *as the
  headline*, but only because M0 is cheap: if the owner cannot secure hardware
  and an Apple Developer account within a planning horizon, the documented
  remote workflow (which exists today via `install.sh` + SSH forwards) should
  ship as v1 rather than holding the Mac release hostage to procurement.
- The Lima/UTM dismissal is correct for shipping (one-artifact requirement),
  and the plan already endorses Lima as an M0 harness, which is the right use.

## 5. Phase-by-phase notes

- **M0** — Well-gated, observable criteria (prompt, 100/100 pings, 30-min
  soak, p95 echo < 300 ms). Entirely hardware-blocked; add the F2 Linux
  rehearsal so the project is not idle while procuring Macs. Realistically
  1-2 days once a machine exists, not one session, because amd64 library
  curation inside an arm64 rootfs is fiddly.
- **M1** — Shrinks substantially under F1: recompile + verify, plus the real
  item, VPCS v0.8.3 arm64 compilation (an old-school C codebase; expect minor
  Makefile/ifdef friction, budget half a day to a day). Keep the live arm64
  data-plane test; it is the cheap insurance that F1's analysis is right.
- **M2** — The real long pole together with M3. Split per F5. Fix the health
  route per F3. Note `VZLinuxBootLoader` kernel-format constraints
  (uncompressed arm64 Image vs Debian's compressed vmlinuz) as an explicit
  unknown — the plan never mentions kernel format, and it is exactly the kind
  of detail that eats a session.
- **M3** — Secretly two sessions: (a) the Windows/portable refactor of
  `main.go`/`detect.go`/`wsl.go` build boundaries (safe, testable on Windows
  today) and (b) new VZ/vsock darwin code (Mac-only). Doing (a) early keeps
  the Windows launcher shippable and de-risks the seam.
- **M4** — Well-scoped; note F29's caveat that `exeDir` plumbing in `qemu.go`
  is part of the change, not just `defaultSyncDirs`.
- **M5** — Fine. The hard-coded Windows Wireshark path in `PaneBody.svelte`
  (audit row 41) means GUI work, not just helper work — the plan's file list
  already includes it, good.
- **M6** — Criteria are objective and checkable (`codesign`, `stapler`,
  `spctl`). First-time Developer ID + notarization always costs more than one
  session; see sizing.
- **M7** — Matches the real workflow mechanism (draft release, hand-attached
  assets, audit row 37). The `ubuntu-24.04-arm` spike-first stance is correct.
- **M8** — Right to exist; "one M1/M2-class and one current M4-class Mac"
  doubles the hardware ask — the owner questions cover it, but expect this to
  be the schedule-slipper.
- **Missing phase check:** nothing structural is orphaned. Two omissions worth
  a line each: kernel-format handling for VZLinuxBootLoader (assign to M2) and
  the i386 parity statement (F6, assign to Constraints/INSTALL.md).

## 6. Effort estimate

Independent estimate, single competent implementer, hardware and Apple account
already in hand (procurement excluded — it is the true schedule risk, not the
engineering):

| Phase | Plan's implicit cost | My estimate |
|---|---|---|
| M0 | 1 session | 1-2 days (plus F2 rehearsal: 0.5-1 day, Mac-free, can start now) |
| M1 | 1 session | 0.5-1 day after F1 correction (was headed for 2-3 days of unnecessary struct work) |
| M2 (a+b) | 1 session | 3-5 days |
| M3 | 1 session | 4-6 days (VZ binding, vsock proxy both ways, refactor) |
| M4 | 1 session | 1-2 days |
| M5 | 1 session | 2-3 days |
| M6 | 1 session | 2-3 days first time (cert/notary/entitlement iteration) |
| M7 | 1 session | 2-3 days (keychain-in-CI, secret hygiene, arm64 runner spike) |
| M8 | 1 session | 1-2 days per hardware pass, expect two passes |
| **Total** | ~9 sessions | **roughly 3-5 focused weeks** of engineering, plus procurement lead time for two Macs, an Apple Developer account, and image licenses |

The plan's "each phase fits one focused session" framing undersells total cost
by roughly 2x, concentrated in M2/M3/M6/M7; it does, to its credit, surface
the expensive external dependencies plainly in Sections 6-7 rather than
burying them. **The M0 gate is unambiguously worth running first**: it is one
to two days on a single borrowed Mac against a multi-week build-out, it
requires zero product code, and a NO-GO redirects the project before any
port investment. With F2's Linux rehearsal added, the project can even buy
most of M0's confidence before the first Mac arrives.
