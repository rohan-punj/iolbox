# Apple Silicon macOS implementation plan

## 1. Summary and ruling

**Ruling: iolbox is confirmed to run real Cisco IOL on Apple Silicon with the existing unmodified linux/amd64 native payload, so ship one unsigned `darwin/arm64` launcher archive that provisions a pinned Ubuntu 22.04/kernel 5.15 Lima VZ machine, gates Rosetta with the amd64 loader canary, installs that payload, and opens the existing browser GUI.** Lima is the sole required VM manager for the first supported release. It is user-installed, Apache-2.0, works on macOS 13.5, and owns the Virtualization.framework/Rosetta entitlement that this project cannot ship.

M0 is complete and is a **GO**, not a hypothesis. On an Apple M1 with macOS 13.5 and Lima 2.2.0, two Cisco IOL 17.18.02 routers using the unmodified `iolbox-server-v0.4.1.tar.gz` reached `running` within 10 seconds, exchanged traffic at 90% (9/10; 1/5/37 ms), and survived VM stop/start with the image cache, licence, host ID, and fabric intact (`docs/macos-m0-result.md:1-21`, `docs/macos-m0-result.md:58-79`). The amd64 supervisor ran through Rosetta as its existing systemd service; cgroup delegation, `/dev/net/tun`, image classification, NVRAM injection, IOL licensing, and the point-to-point data plane all worked. No supervisor port, arm64 VPCS, arm64 rootfs, or new guest disk is required for the MVP.

M0 also established the primary compatibility constraint: macOS 13.5 Rosetta aborts every amd64 executable under Ubuntu 24.04/kernel 6.8 on auxv type 28 (`AT_RSEQ_ALIGN`), while Ubuntu 22.04/kernel 5.15 works (`docs/macos-m0-result.md:37-56`). No authoritative source found during this revision establishes whether macOS 14 or 15 first fixed that behavior; that cutoff remains **UNVERIFIED**. Therefore the product rule is not “kernel < 6.3 or a guessed macOS version.” It is: pin the supported guest to Ubuntu 22.04 with kernel 5.15, hold that kernel, and execute `/lib64/ld-linux-x86-64.so.2 --version` through Rosetta before install and every start. A failed canary is a hard, actionable compatibility error. Newer guest kernels become supported only after the exact macOS/Rosetta pair passes the canary and the full qualification matrix.

“Installable binary” is achievable in a dependency-mediated form. The user downloads one iolbox archive containing an unsigned, stdlib-only macOS launcher, the existing versioned amd64 native tarball, a locked Lima template/provisioner, checksums, and notices. The user must install Lima separately; the launcher then performs the rest. With no Apple Developer account, iolbox cannot provide a notarized `.app`, `.dmg`, or warning-free double-click experience. Browser downloads may require right-click/Open or `xattr -d com.apple.quarantine`; that remains part of release qualification.

## 2. Constraints and non-goals

- No paid Apple Developer Program account. No phase depends on Developer ID, notarization, a provisioning profile, or a restricted entitlement.
- Lima is user-installed and is the only required VM manager for v1. iolbox does not redistribute Lima, Rosetta, QEMU, or any hypervisor.
- The supported guest is Ubuntu 22.04 arm64 with a pinned 5.15 kernel. Ubuntu 24.04/kernel 6.8 is explicitly unsupported on the measured macOS 13.5 Rosetta because all amd64 execution fails (`docs/macos-m0-result.md:37-56`).
- The provisioner must pin existing Ubuntu repositories to `[arch=arm64]` and add amd64-only entries using `archive.ubuntu.com` and `security.ubuntu.com`; `ports.ubuntu.com` has no amd64 indices (`docs/macos-m0-result.md:81-90`). The implementation targets Jammy's one-line `sources.list`; Noble's deb822 layout is not needed for v1.
- The Rosetta loader canary is mandatory even with the pinned guest. Kernel version checks are informative, not sufficient: the executable canary is the compatibility authority.
- Apple Silicon only. Intel Macs and universal host binaries are out of scope.
- The supported Cisco family is x86_64 IOL. Rosetta for Linux is 64-bit only, so old i386/`i86bi` images cannot work. This accepted parity loss must be stated in install and release documentation.
- The MVP reuses the linux/amd64 supervisor and VPCS. The native-arm64 branch is an independence option, not a release prerequisite.
- Full-VM x86 node types such as IOSv are out of scope. The absence of nested x86 KVM does not affect the proven IOL userspace path.
- Users supply Cisco images and licensing material. No artifact or CI job contains Cisco software.
- The primary deliverable is an unsigned CLI archive, not a native GUI, `.app`, `.dmg`, `.pkg`, Tauri application, Homebrew cask, or App Store package. The GUI remains the embedded browser application.
- The Tauri remote stub is dead code for this work. The shipping launcher is dependency-free, stdlib-only Go (`tools/iolab-launcher/README.md:11-16`) and the release workflow builds it directly (`.github/workflows/release.yml:16-45`).
- M0 did not prove four-node capacity, capture/Wireshark, NAT/extnet, VPCS in a lab, multi-link fabric, sustained soak, browser interaction, or Mac-side console/capture forwarding (`docs/macos-m0-result.md:97-106`). None is treated as working until its later acceptance test passes.

## 3. Execution-layer option analysis

| Rank | Execution layer | Evidence and performance | Delivery/licensing | Ruling and remaining cost |
|---|---|---|---|---|
| 1 | **Lima VZ arm64 Ubuntu 22.04/5.15 + Rosetta + existing amd64 payload** | **Confirmed.** M1/MacOS 13.5, Lima 2.2.0, two IOL 17.18.02 nodes, 10-second start, 9/10 ping, load 0.07, 889 MiB guest use, about 530 MiB RSS per node (`docs/macos-m0-result.md:23-32`, `docs/macos-m0-result.md:58-79`). | Lima is Apache-2.0 ([repository](https://github.com/lima-vm/lima)); VZ/Rosetta is enabled by `--vm-type=vz --rosetta` ([Lima multi-arch](https://lima-vm.io/docs/config/multi-arch/)). User installs Lima; iolbox ships only its launcher, template, and native payload. | **Primary.** Remaining work is deterministic provisioning, kernel/canary enforcement, Mac lifecycle, port forwarding, sync, UX qualification, and release packaging. |
| 2 | **OrbStack amd64 machine + Rosetta** | GNS3 prior art remains relevant, but iolbox is **UNPROVEN** because OrbStack could not start on the only test host. OrbStack 2.2.3 declares `LSMinimumSystemVersion=14.0`; M0 ran macOS 13.5 (`docs/macos-m0-result.md:23-35`). | Best integrated UX in principle. Free for personal/non-commercial use; currently $8/user/month billed annually for business use ([pricing](https://orbstack.dev/pricing)). | Optional post-v1 backend. It ranks below Lima because it excludes the proven macOS 13.5 host, is proprietary for business use, and has no iolbox test result. |
| 3 | **Native arm64 supervisor/VPCS + FEX or qemu-user for IOL** | Not hardware-tested. The branch `luna/macos-arm64-invariant` reportedly builds/vets the arm64 supervisor and parameterizes VPCS/rootfs; M0 confirms those changes are unnecessary for MVP. With FEX/qemu-user, this removes dependence on Apple's Rosetta/kernel compatibility, not merely translated-data-plane overhead. | Requires an arm64 guest/package and another translator. FEX is MIT; qemu-user brings GPL obligations. Can still be hosted by Lima, Fusion, or UTM. | Strategic independence path after v1. Promote only after a real two-/four-node comparison and a demonstrated Rosetta-independent boot. |
| 4 | **Colima VZ arm64 guest + Rosetta** | Shares Lima's underlying approach; not tested with iolbox. `colima start --vz-rosetta` is the documented switch ([Lima comparison](https://lima-vm.io/docs/faq/colima/)). | MIT, but container-oriented defaults add Docker machinery iolbox does not need. | Defer. It adds a second lifecycle/version matrix without improving the proven Lima path. |
| 5 | **Fusion/UTM/Parallels arm64 guest + FEX/qemu-user** | Plausible but untested. Fusion cannot run an x86 guest or expose Rosetta-for-Linux, so IOL needs an in-guest translator. | User installs the manager; ship provisioning only. Fusion/UTM bundles are backend-specific. | Local fallback for hosts without usable Rosetta, after the native/FEX path exists. Not MVP. |
| 6 | **Remote x86_64 Linux over SSH** | Existing native payload runs natively; performance depends on server/network. | Manual workflow exists (`docs/INSTALL.md:221-276`). Integrated tunnels would be new launcher work. | Supported operational fallback; not local Apple Silicon execution. |
| 7 | **Bundled QEMU aarch64 + HVF; arm64 guest + translator** | Technically plausible, no longer justified by product need. | iolbox would own a QEMU/dylib bundle, ad-hoc signature behavior, guest image, translator, quarantine, and GPL compliance (`THIRD_PARTY.md:9-20`, `THIRD_PARTY.md:79-91`). | Demoted indefinitely. User-installed Lima already supplies the working accelerated VM layer. |
| 8 | **Full-system x86_64 QEMU TCG** | Likely compatible and far slower because kernel and userspace are both emulated. The existing same-ISA Windows TCG path already warns of slow boots (`docs/INSTALL.md:321-330`). | QEMU and amd64 disk; GPL obligations apply. | Last resort, not UX-equivalent and not scheduled. |

### What M0 retired

The following are no longer risks or planned work:

- Whether real x86_64 IOL 17.18.02 executes under Rosetta.
- Whether the existing amd64 supervisor and native installer run in an arm64 Lima guest.
- Whether systemd, passwordless sudo, cgroup delegation, `/dev/net/tun`, point-to-point tap fabric, image scanning, licence generation, NVRAM injection, and restart persistence survive translation.
- Whether an arm64 supervisor, arm64 VPCS, or arm64 rootfs must precede a usable Mac release.
- Whether the two-node translated path has obviously unacceptable idle cost or boot time.

These conclusions are grounded in the executed record, not inferred from ABI layouts (`docs/macos-m0-result.md:58-79`). The review's F1 conclusion still matters: the supervisor is expected to be a clean arm64 recompile, but that recompile is now optional rather than merely low-risk.

### Kernel/Rosetta ruling

The M0 failure is an execution-environment compatibility problem, not an IOL defect. Kernel 6.8 supplies `AT_RSEQ_ALIGN`; the Rosetta bundled with macOS 13.5 aborts before the amd64 program starts. The same failure occurs with the glibc loader, so the provisioner needs no Cisco image to detect it (`docs/macos-m0-result.md:37-56`, `docs/macos-m0-result.md:88-90`).

The release must do all of the following:

1. Create a versioned Lima machine from a locked Ubuntu 22.04 arm64 image.
2. Keep the guest on the qualified 5.15 kernel and prevent unattended migration to a newer kernel series.
3. Configure arm64 and amd64 apt sources separately before installing amd64 libraries.
4. Confirm Rosetta binfmt registration.
5. Run `/lib64/ld-linux-x86-64.so.2 --version`; require exit status 0 and record the output.
6. Refuse install/start if the canary fails, showing macOS, Lima, guest kernel, Rosetta/binfmt state, and recovery instructions.

Apple documents Rosetta for amd64 Linux programs and current kernel performance patches, including a patch applicable to Linux 6.10 ([Apple](https://developer.apple.com/documentation/virtualization/accelerating-the-performance-of-rosetta)). That does not establish which macOS release fixed auxv type 28. macOS 14 and 15 behavior is therefore **UNVERIFIED** and must be measured, not inferred. Even if both pass, the v1 guest remains pinned; expanding the matrix is a later qualification decision.

### Diagnostics and i386 ruling

The supervisor's `arch: x86_64` is technically the executable architecture, but `runtime: debian-slim-12` is not an adequate description of this environment, and the hard-coded `i386` hello feature is false under 64-bit-only Rosetta (`supervisor/internal/server/handlers.go:44-53`, `supervisor/internal/server/server.go:126-132`; measured in `docs/macos-m0-result.md:91-95`). For v1:

- The Mac launcher `status`/diagnostic output must identify `backend=lima`, `guest_arch=aarch64`, the guest kernel, and `execution=rosetta-amd64` alongside the supervisor's self-report. Do not redefine the existing `arch` field.
- The Mac-provisioned service must cause the supervisor to omit `i386` from hello. This requires a small explicit runtime capability/configuration change after the concurrent branch is reconciled; it is not already implemented.
- Install and release documentation must say x86_64 IOL only. Do not advertise a fallback for i86bi.

### Final ruling and fallback ladder

Ship Lima-only v1 against the exact environment that passed M0. OrbStack becomes a later convenience adapter for macOS 14+ only after it passes the full matrix. The first local fallback is native arm64 components plus FEX/qemu-user because that removes the newly discovered Rosetta/kernel coupling. Remote x86_64 Linux remains the dependable floor. Bundled QEMU/HVF and full-system x86 TCG are not on the implementation schedule.

## 4. Architecture of the chosen path

```text
iolbox-macos-arm64.tar.gz                     unsigned GitHub release
  iolbox                                     darwin/arm64 stdlib Go launcher
  payload/iolbox-server-<version>.tar.gz      existing linux/amd64 native target
  templates/iolbox-lima.yaml                 NEW, locked Ubuntu 22.04 arm64 image
  provision/                                 NEW Jammy multiarch + kernel policy
  notices/ + checksums

macOS launcher
  detect supported Lima version
  create/start durable named VM
  run preflight:
    macOS/Lima/kernel inventory
    Rosetta binfmt registered
    /lib64/ld-linux-x86-64.so.2 --version == 0
    systemd + /dev/net/tun + sudo capability
  install/upgrade versioned native payload
  establish host-loopback GUI/console/capture reachability
  poll GET http://127.0.0.1:4001/ until status < 500
  open browser
  sync host images/labs through existing HTTP + WebSocket APIs
  status: show Lima + arm64 guest + Rosetta-amd64 execution explicitly
                       |
              Lima 2.2.0 VZ machine (initial qualified version)
              Ubuntu 22.04 arm64
              pinned kernel 5.15
              Rosetta binfmt
              amd64 loader/libs from archive/security.ubuntu.com
                       |
              existing amd64 native install
              /opt/iolbox/supervisor
              /opt/iolbox/vpcs
              /opt/iolbox/{images,labs,run}
                       |
              translated amd64 supervisor/VPCS/IOL
              native arm64 kernel tap/bridge/cgroup/networking
```

The release does not contain a guest disk or hypervisor. Lima downloads the locked guest image; the release records its URL/digest and provisions it reproducibly. The application payload remains the output of `runtime/pack-native.sh`, which already stages the amd64 supervisor, VPCS, systemd units, and installer (`runtime/pack-native.sh:113-205`). The installer already warns on arm64 and continues, which M0 proved correct (`runtime/files/native/install.sh:58-80`; `docs/macos-m0-result.md:65-76`).

Host launcher work stays in `tools/iolab-launcher/`. Portable code includes `foldersync.go`, `wsclient.go`, `imagecache.go`, `ports.go`, and `prompt.go`; `main.go:172-179` is presently Windows-specific for browser opening, `detect.go:53-90` invokes Windows tools, and default sync paths are currently beside the executable (`foldersync.go:486-498`). Darwin build boundaries, Lima lifecycle commands, `~/Library/Application Support/iolbox/{images,labs}`, and `/usr/bin/open` are therefore real work. Readiness reuses `waitForGUI`'s verified `GET /` status-below-500 behavior (`tools/iolab-launcher/qemu.go:300-325`); there is no `/api/health` (`supervisor/internal/wsbridge/wsbridge.go:141-156`).

The Mac host should not depend on backend mounts for durable images and labs. The launcher owns `~/Library/Application Support/iolbox`, synchronizes through the existing application APIs, and leaves `/opt/iolbox` as the guest source of truth during a running session. Port exposure must remain host-loopback because GUI, console, and capture listeners have no authentication (`runtime/files/native/install.sh:271-277`).

## 5. Phased slice plan

### M0 — completed Apple Silicon feasibility gate

**Status:** COMPLETE, GO. The authoritative record is `docs/macos-m0-result.md`; do not repeat or reinterpret it as a future phase.

**Observed acceptance:** Apple M1/macOS 13.5, Lima 2.2.0, Ubuntu 22.04/kernel 5.15, unchanged amd64 v0.4.1 payload, two IOL 17.18.02 nodes running within 10 seconds, point-to-point traffic 9/10 at 1/5/37 ms, GUI HTTP 200, and successful VM restart persistence (`docs/macos-m0-result.md:23-32`, `docs/macos-m0-result.md:58-79`).

**Estimate:** complete; no remaining engineering cost.

### M1 — make the proven guest reproducible and fail-safe

**Goal:** turn the hand-built M0 environment into an idempotent, versioned Lima template/provisioner.

**Files touched:** after reconciling the concurrent branch, new Mac/Lima packaging material in an appropriate packaging directory; `runtime/files/native/install.sh` only for defects demonstrated by the provisioner; install documentation. Exact new filenames are implementation decisions.

**Observable acceptance:** from a deleted Lima state, one non-interactive command creates Ubuntu 22.04 arm64 with the locked image digest and 5.15 kernel; `uname -m` reports `aarch64`; apt fetches arm64 only from the arm ports and amd64 only from `archive.ubuntu.com`/`security.ubuntu.com`; Rosetta binfmt is enabled; `/lib64/ld-linux-x86-64.so.2 --version` exits 0; the unmodified-version payload installs; systemd becomes active; `GET /` returns 200; restart retains kernel series, host ID, licence, and image cache. A negative test with a 6.8 guest fails at the canary before payload installation and prints the auxv-compatible remediation.

**Dependencies:** completed M0; Apple Silicon hardware for the Rosetta checks.

**Estimate:** 1-1.5 focused days.

### M2 — implement the Darwin launcher and Lima lifecycle

**Goal:** replace manual `limactl`/guest commands with one idempotent host CLI while preserving the Windows launcher.

**Files touched:** `tools/iolab-launcher/main.go`, `detect.go`, OS-specific browser/process files, and proposed new Darwin/Lima adapter files; existing portable launcher tests. New filenames must be identified as new in implementation.

**Observable acceptance:** `GOOS=darwin GOARCH=arm64 go build` succeeds in CI; on a clean Mac with supported Lima installed, one launcher command creates or reuses the named VM, runs M1 preflight/provisioning, waits on `GET /`, and opens the browser. `start`, `stop`, `status`, `diagnose`, and upgrade operations have deterministic exit codes; stop never deletes guest or host data. Diagnostics show macOS version, Lima version, guest kernel/arch, Rosetta canary result, and `execution=rosetta-amd64`. Existing Windows launcher tests/build remain green.

**Dependencies:** M1.

**Estimate:** 2-3 focused days.

### M3 — complete browser, sync, console, and capture UX

**Goal:** reach Windows-launcher UX parity from the Mac host, not merely from inside the guest.

**Files touched:** `tools/iolab-launcher/foldersync.go`, `wsclient.go`, `imagecache.go`, `ports.go`, Darwin browser/native-tool integration, capture helper/UI code only where the current Windows assumptions require it, and Mac install/provider docs.

**Observable acceptance:** Safari or Chrome is actually driven through image upload, image registration, lab creation, start, console input, stop, and reload. Host files in `~/Library/Application Support/iolbox/images` and `labs` round-trip across restart. Two simultaneous browser consoles work from macOS. Lima forwards GUI plus the required console/capture ranges to host loopback only. A live capture yields packets and a valid non-empty `.pcapng`; installed Wireshark opens it or the browser offers a working save path. Spaces and non-ASCII characters in the macOS user/data paths pass.

**Dependencies:** M2. This phase deliberately covers the browser, capture, and host port-forwarding items M0 did not test.

**Estimate:** 2-3 focused days.

### M4 — qualify the remaining runtime behaviors and capacity

**Goal:** close the unproven runtime list before release.

**Files touched:** test records and documentation; product/runtime files only for failures actually observed, after reconciling the concurrent branch.

**Observable acceptance:** on Apple Silicon, run and record: VPCS connected to IOL with bidirectional ping; a multi-link topology; NAT-node outbound connectivity and teardown; extnet attach/traffic/cleanup where Lima exposes a suitable interface; four IOL nodes on supported hardware; and a two-hour sustained traffic soak. Capture must remain valid during the multi-link and soak runs. After forced launcher and VM termination, restart leaves no stale taps/processes. Record boot time, packet loss, load, per-node RSS, guest memory, and host memory pressure.

**Dependencies:** M3. Four-node qualification may require a 16 GB Mac; an 8 GB acceptance threshold is an owner decision.

**Estimate:** 1-1.5 focused days if no product defect is found; defects are contingency, not hidden in this estimate.

### M5 — suppress false capabilities and finalize platform diagnostics

**Goal:** make the product report the measured execution environment honestly.

**Files touched:** after the concurrent branch is merged, `supervisor/internal/server/handlers.go` and its tests for a new explicit i386-disable capability/configuration; native service/provisioning configuration; launcher status/diagnostic output; install/release documentation. Do not alter the existing `arch` field's meaning.

**Observable acceptance:** the Mac-launched hello response omits `i386`; ordinary existing amd64 targets still advertise it; Mac diagnostics say `guest_arch=aarch64`, `execution=rosetta-amd64`, the actual kernel, and the canary result; the GUI never offers i86bi as supported on this target. An x86_64 IOL lab remains unaffected.

**Dependencies:** M1 defines the guest marker/configuration; reconcile `luna/macos-arm64-invariant` before changing shared supervisor/runtime files.

**Estimate:** 0.5-1 focused day.

### M6 — build, distribute, and qualify the unsigned one-download artifact

**Goal:** deliver the promised download with an honest prerequisite and repeatable release process.

**Files touched:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`, packaging/release documentation, `docs/INSTALL.md`, `docs/providers.md`, and `THIRD_PARTY.md` as applicable. No signing/notarization phase.

**Observable acceptance:** CI cross-builds/tests the launcher for `darwin/arm64` and produces `iolbox-macos-arm64.tar.gz` with launcher, exact native payload, locked Lima template/provisioner, checksums, and notices. A clean supported Mac with only Lima installed downloads that one archive and reaches the GUI with one documented iolbox command. Browser-download and `curl` paths record quarantine attributes and the exact successful first-run procedure. Upgrade preserves images, labs, licence identity, and the pinned guest kernel; uninstall distinguishes launcher removal from destructive VM/data deletion. Release notes name Lima, macOS, guest/kernel, x86_64-IOL-only, and unsigned/Gatekeeper constraints.

**Dependencies:** M1-M5.

**Estimate:** 1.5-2 focused days including one clean-machine pass.

### M7 — optional Rosetta-independent arm64/FEX path

**Goal:** remove dependence on Apple's Rosetta/kernel compatibility rather than unblock the release.

**Files touched:** the existing `luna/macos-arm64-invariant` work after review: arm64 supervisor release build, VPCS fetch/build, rootfs architecture parameterization, and FEX/qemu-user rehearsal; then separate provisioning/launcher selection.

**Observable acceptance:** on arm64 hardware, native supervisor and VPCS pass their tests and report arm64; no Rosetta mount/binfmt is present; only x86_64 IOL runs under FEX or qemu-user; the complete M3/M4 matrix passes; two-/four-node resource and boot metrics are compared with the Lima/Rosetta baseline. Promote only if it removes Rosetta as a dependency without unacceptable regressions.

**Dependencies:** independent of M1-M6 and not on the v1 critical path. The branch currently has build evidence but no arm64-hardware execution evidence.

**Estimate:** 3-5 focused days after branch reconciliation, excluded from the MVP total.

### Revised effort

| Phase | Estimate | Why it remains |
|---|---:|---|
| M0 | Complete | Real hardware GO; no remaining cost. |
| M1 | 1-1.5 days | Reproducible Jammy multiarch provisioning, kernel hold, canary, negative test. |
| M2 | 2-3 days | Darwin build boundaries and Lima lifecycle/diagnostics. |
| M3 | 2-3 days | Actual browser, sync, Mac port ranges, console, capture/Wireshark. |
| M4 | 1-1.5 days | Remaining node/network/topology/capacity/soak matrix. |
| M5 | 0.5-1 day | Honest translated diagnostics and i386 capability gating. |
| M6 | 1.5-2 days | CI/archive/docs/quarantine/clean-machine release pass. |
| **MVP total remaining** | **8-12 focused engineering days, approximately 1.5-2.5 weeks** | Single competent implementer; hardware and IOL licence already available. |
| M7 optional | +3-5 days | Rosetta-independent arm64/FEX path, not v1. |

The independent pre-M0 estimate was 3-5 focused weeks (`docs/macos-arm64-plan-review.md:284-301`). The new estimate is roughly half because M0 eliminated the arm64 application/rootfs port, VPCS port, custom guest disk, translation discovery, and the risk contingency around the core two-node data plane. It does not collapse to “one script”: the Mac launcher, locked provisioning, browser/sync/ports/capture UX, remaining runtime matrix, and unsigned clean-machine release path are still product work.

## 6. Risk register

| Rank | Risk | Likelihood / impact | Mitigation or kill criterion |
|---|---|---|---|
| 1 | Rosetta compatibility changes with guest kernel/macOS; a routine kernel upgrade can make every amd64 binary abort. | High / Critical | Pin Ubuntu 22.04/kernel 5.15, hold the kernel, run the loader canary before install and every start, record the full version tuple, and fail closed. Never support a new tuple from version inference alone. |
| 2 | Lima CLI, image templates, networking, or Rosetta integration changes underneath iolbox. | Medium / High | Declare supported Lima versions, lock guest URL/digest, isolate the adapter, test upgrade/downgrade, and retain remote x86_64 plus optional FEX paths. Kill automatic provisioning for an unqualified Lima major version. |
| 3 | Mac-side console/capture port forwarding or actual browser UX fails even though guest-local control works. | Medium / High | M3 drives real browsers and multiple forwarded ports from macOS. Do not release until GUI, two consoles, and capture work host-side on loopback. |
| 4 | Four-node labs exceed acceptable memory on common 8 GB Macs. | Medium / Medium-High | M4 measures four nodes and host memory pressure. Publish a 16 GB minimum for four-node support, or cap/reject node count on 8 GB, rather than claiming untested capacity. |
| 5 | NAT/extnet, VPCS, multi-link, capture, or soak reveals a path M0 did not exercise. | Medium / Medium-High | M4 has separate observable tests. A NAT/extnet limitation may be documented if core isolated labs remain sound; fabric corruption, capture corruption, or process leaks block release. |
| 6 | Holding kernel 5.15 conflicts with security/update expectations over the product lifetime. | Medium / High | Consume Jammy 5.15 security updates without crossing kernel series, publish the policy, monitor Ubuntu support, and qualify newer kernel/macOS pairs before Jammy's support horizon ends. M7 is the independence exit. |
| 7 | The unsigned launcher is blocked or perceived as unsafe. | High / Medium | Publish checksums, exact source/build provenance, right-click/Open and `xattr` recovery, and tested browser/curl flows. Never describe the archive as notarized or warning-free. |
| 8 | Unauthenticated guest listeners become LAN-accessible. | Medium / High | Bind/forward only to Mac loopback, inspect listeners in qualification, and refuse configurations that expose GUI/console/capture publicly by default (`runtime/files/native/install.sh:271-277`). |
| 9 | Multiarch apt sources drift or accidentally mix architectures/mirrors. | Medium / Medium | Generate Jammy-specific sources deterministically, assert each source's `arch`, run apt update in CI where possible, and validate loader/library architecture before service start. |
| 10 | Diagnostics advertise i386 or hide translation, causing invalid support conclusions. | Certain until M5 / Medium | Gate `i386`, surface the actual Lima/kernel/Rosetta tuple in launcher diagnostics, and test ordinary Linux targets for no regression. |
| 11 | OrbStack is treated as supported based on GNS3 prior art despite no iolbox run and a macOS 14 floor. | Medium / Low-Medium | Label it unproven/post-v1, require macOS 14+, and run the full M3/M4 matrix before adding the adapter. |
| 12 | Optional arm64 work consumes MVP attention without removing Rosetta. | Medium / Medium | Keep M7 separate; its success criterion requires Rosetta absent and FEX/qemu-user running IOL, not merely native supervisor compilation. |

Retired risks: real IOL translation viability, native installer execution on arm64, systemd/cgroup delegation, `/dev/net/tun`, two-node point-to-point forwarding, image classification, licensing, NVRAM injection, and restart persistence. These passed M0 and must not retain contingency budget.

## 7. Open questions for the owner

1. Set the minimum host OS: support the proven macOS 13.5 floor, or require macOS 14+ despite losing the only tested configuration? The plan recommends 13.5 for v1.
2. Accept the recommended deterministic rule—Ubuntu 22.04/kernel 5.15 regardless of host macOS—or spend qualification time testing whether macOS 14 and 15 Rosetta support kernel 6.3+? The plan recommends pinning for v1 and testing newer tuples later.
3. Confirm Lima as the sole supported v1 manager. OrbStack is unproven, requires macOS 14, and adds commercial licensing/support complexity.
4. What is the promised four-node hardware tier: must four IOL routers work on an 8 GB M1, or may documentation require 16 GB? M0 only proves two nodes on 8 GB.
5. Which NAT/extnet behaviors are release-blocking? Lima's VM boundary may constrain attaching labs directly to Mac physical interfaces even if NAT works.
6. Is launcher-only translated diagnostics sufficient for v1, or must the browser GUI display the Lima/kernel/Rosetta tuple as well?
7. Which additional x86_64 IOL versions must pass besides 17.18.02 before release?
8. Is the permanent unsigned Terminal-first experience acceptable, or should the Mac artifact remain marked experimental despite functional completion?
9. Should M7 begin in parallel as risk insurance against Rosetta/kernel coupling, or wait until the Lima v1 artifact ships?

## 8. Assumptions to validate

| Unverified claim | Exact experiment |
|---|---|
| macOS 14 or 15 Rosetta accepts `AT_RSEQ_ALIGN` from Linux 6.3+; the first fixed release is unknown. | On clean Apple Silicon hosts for each macOS minor under consideration, start the same Lima 6.8 guest and run `/lib64/ld-linux-x86-64.so.2 --version`; record Rosetta and OS build. If it passes, repeat the full M3/M4 matrix. Until then the cutoff is **UNVERIFIED**. |
| The provisioner can keep Ubuntu 22.04 on a supported 5.15 kernel through normal security upgrades. | Fresh-install the locked template, apply all updates, reboot twice, verify `uname -r`, canary, systemd, licence identity, and two-node ping; repeat after a simulated launcher upgrade. |
| Jammy arm64/amd64 apt source generation remains valid across mirrors and point releases. | From a clean image run `apt-get update`, install the exact arm64 native tools and amd64 loader/glibc/OpenSSL packages, then use `dpkg --print-foreign-architectures`, `apt-cache policy`, `file`, and `readelf` to prove provenance/architecture. |
| The launcher can statically forward or discover all GUI, console, and capture ports through current Lima without LAN exposure. | Start two nodes and captures, connect from macOS to every assigned port, inspect `lsof -nP -iTCP` on macOS, and attempt connection from another LAN host. |
| The browser GUI works end to end on macOS. | Drive Safari and Chrome through upload, registration, topology creation, start, browser console, capture save, stop, reload, and error recovery; HTTP 200 alone does not count. |
| Folder sync is portable to `~/Library/Application Support/iolbox`. | Run initial upload, periodic sync-out, final sync, conflict, deletion, restart, spaces, Unicode, and a large image test using the existing HTTP/WS path. |
| Capture and Wireshark tee work through translation and Lima forwarding. | Capture known ICMP traffic, validate the saved pcapng with Wireshark/tshark, open live capture from macOS, and compare packet counts with the ping run. |
| VPCS, NAT/extnet, and multi-link fabrics work in the proven environment. | Build separate minimal labs for each, assert bidirectional traffic, stop them, and verify no stale taps/bridges/iptables rules/processes remain. |
| Four IOL nodes are supportable on the chosen hardware floor. | Run a fixed four-router topology for two hours on 8 GB and 16 GB Macs; record boot time, packet loss, load, RSS, guest memory, macOS pressure/swap, and console latency. |
| The unsigned archive's quarantine instructions are stable. | Download the exact release candidate through Safari, Chrome, and `curl` on a clean Mac; record `xattr -l`, first-run UI, exit status, and the minimum successful recovery for each path. |
| OrbStack is a useful second backend for iolbox on macOS 14+. | On a macOS 14+ Mac install the then-current OrbStack, run the existing amd64 payload, then repeat M3/M4. GNS3 prior art is not acceptance evidence for iolbox. |
| The `luna/macos-arm64-invariant` branch works on real arm64 Linux and can remove Rosetta when paired with FEX/qemu-user. | Build/install on arm64 hardware, run native supervisor/VPCS tests, remove/disable Rosetta, run IOL under each translator, and repeat the two-/four-node matrix. |
