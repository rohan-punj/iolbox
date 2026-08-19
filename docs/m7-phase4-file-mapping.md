# M7 Phase 4 — file mapping and status

Worktree `J:\Claude code\iolab-m7-phase4-wt`, branch
`luna/macos-m7-phase4-integration`, based on `luna/macos-m6-followups` @
`154b58b` (re-verified at session start). Source branch for reviewed units:
`luna/macos-m7-arm64` @ `99721d6` (Phase 3 CLOSED), worktree
`J:\Claude code\iolab-m7-wt` (read-only reference this session, never edited).

This records the mapping plan section 10 item 1 requires: what was brought
over from the reviewed M7 branch, what was left behind and why, and — where
a straight cherry-pick would have silently regressed the real M1-M6 product
— what was hand-merged instead and why.

## What "reviewed M7 units" excluded from this candidate

Per the continue-prompt's own instruction, none of the following were
touched, even though they're on `luna/macos-m7-arm64`:

- `docs/m7-evidence/**`, `docs/m7-session-logs/**`, `docs/macos-m7-*.md`,
  `docs/macos-arm64-plan*.md`, `docs/translation-rehearsal.md` — Phase 0-3's
  own evidence/planning trail, not launcher code.
- `tools/phase3-baking/**` (bakeoff.py, host-evidence.py, m3-flow.py,
  phase3-select.py, phase3lib.py, guest-provision.sh, guest-inventory.sh,
  accept-input.sh) — Phase 3's own bake-off/evidence-collection tooling. It
  measured and selected the translator; it does not run inside the real
  launcher's provisioning path (macos_lifecycle.go's runProvision uses
  packaging/macos/guest/*.sh, a completely different, launcher-owned step
  pipeline).
- `tools/translation-rehearsal/**` (rehearse.py, rehearse.sh) — Phase 2
  rehearsal tooling, same reasoning.
- `runtime/arch-validation-test.sh`, `runtime/package-contract-test.sh` (new
  in M7) — written and passing against M7's own pack-native.sh, which (see
  below) is NOT what this candidate ships. Adapting them to the hand-merged
  pack-native.sh is real remaining work, tracked as an open gap rather than
  attempted here; they are not launcher runtime code and Phase 4's actual
  gate is hardware evidence, not these scripts.
- `runtime/build-rootfs.sh`'s arm64 diff — only `pack-wsl.sh`/`pack-vmware.sh`
  consume the debootstrap rootfs it assembles. The macOS launcher's
  `selectPayload()` (tools/iolab-launcher/macos_lima.go) only ever looks for
  `iolbox-server-*.tar.gz`, which is `pack-native.sh`'s output. Out of scope
  for the Lima/VZ native-arm64 profile.

## Cherry-picked as-is (verified safe, no product regression)

| File | Commit(s) | Verification |
|---|---|---|
| `supervisor/internal/dirstat/dirstat_linux.go` | 1fc99d8 | comment-only |
| `supervisor/internal/iouyap/tap_linux.go` | 1fc99d8 | comment-only |
| `supervisor/internal/slowtee/slowtee_linux.go` | 1fc99d8 | comment-only |
| `supervisor/internal/tool/reap_linux.go` | 1fc99d8 | comment-only |
| `supervisor/internal/vtap/shim_linux.go` | 1fc99d8 | comment-only |
| `tools/p0-reaper/reaper_linux.go` | 1fc99d8 | comment-only |
| `tools/secbench-attacks-go/internal/attackcommon/raw_linux.go` | 1fc99d8 | comment-only |
| `supervisor/internal/egress/detect_linux.go` | 6a35ab1 | real byte-order bugfix in `parseHexLEIP`, arch-independent |
| `supervisor/internal/server/server_test.go` | 6a35ab1 | new `TestHelloArchExplicitTargets` + arch-aware hello-arch assertion |
| `supervisor/internal/{bcap,iouyap,slowtee,tool,vtap}/arm64_*_test.go` | 6a35ab1/a845cbc | new cross-arch test files |
| `supervisor/internal/tool/{cage,detect,manifest,netns}*_test.go` | a845cbc | extended test tables |
| `runtime/fetch-vpcs.sh` | 6a35ab1 | genuinely additive `--arch`/`VPCS_CC`/fail-closed ELF gate; touches no tool-pack code |

All verified: `go build ./...` and `go vet ./...` clean on windows/amd64 (dev
box); cross-compiles clean for linux/amd64, linux/arm64, darwin/arm64,
windows/amd64; `go test ./...` green for every touched package reachable on
the dev box (server, egress, dirstat directly; linux-only files verified by
cross-compile only, real linux execution deferred to the Mac's Lima guest).

## Hand-merged (straight cherry-pick would have regressed the product)

### `supervisor/internal/server/server.go`

M7's own `server.go` **removed `Config.DisableI386` entirely** and made
`hello`'s advertised `features` unconditionally include `"i386"`
(`handlers.go`: `features := []string{"nvram", "capture", "i386"}`, no
`DisableI386` check at all). That field is exactly the mechanism
`packaging/macos/guest/40-install-payload.sh` depends on
(`Environment=IOLBOX_DISABLE_I386=1` in the canary drop-in,
`verify_gated_unit` asserting it's present) to make the macOS i386 capability
policy honest after the Rosetta canary qualifies a guest — removing it would
have silently broken "i386 truthfulness", which plan section 10 item 5
explicitly requires preserving.

Taken instead: only `defaultHelloArch()` (a `runtime.GOARCH`-aware default
for `Config.Arch`, amd64 → "x86_64", else GOARCH) — a genuine, arch-neutral
improvement so a native arm64 supervisor's hello/status is truthful about
its own architecture. `DisableI386` and its `handlers.go` enforcement are
untouched from the M6 baseline.

### `runtime/pack-native.sh` and `runtime/files/native/install.sh`

M7's own versions of **both** files **silently dropped the entire
learning-tool-pack / `iolbox-toollaunch` / `ioltool`-service-account product
surface** — M7 Phase 0-3 only ever needed a bare supervisor+vpcs IOL/VPCS-
correctness payload for its own measurement, never the real PC/VPCS/aaa/
webserver/httpclient/syslog/netsvc/secbench GUI stack the M1-M6 product
actually ships. Applying either diff verbatim would have regressed PC/VPCS
nodes on **every** architecture, amd64 included — a severe, silent product
regression, not an arm64-specific one.

Taken instead (hand-merged onto the full M6 baseline, which still builds and
stages every pack): `--arch amd64|arm64`, `--validate-only`, a fail-closed
`require_elf_arch()` gate (replacing the old amd64-only warning-only probe)
applied to supervisor/vpcs/toollaunch, per-arch cross-compilation of
toollaunch and every learning-tool pack + secbench attack binaries,
arch-suffixed package naming when `--arch` is explicit, and a `manifest.env`
(version/os/arch/supervisor+vpcs sha256) written for explicit-arch builds.
No-argument invocation is byte-for-byte the historical amd64 behavior.
`install.sh` gained the matching manifest.env-driven fail-closed
architecture check (refuses to install — no directory created, no file
copied, systemctl never invoked — before any of that on a mismatch) ahead of
the old warning-only x86_64 check, which still applies unchanged when no
manifest.env ships. `ioltool` account creation and tools/packs installation
are byte-for-byte unchanged from the M6 baseline.

Verified: `bash -n` syntax-clean on all three scripts (Windows dev box has
no bash execution environment to actually run them; real execution
verification is deferred to the physical Mac / a Linux builder).

## New: `--profile auto|rosetta-amd64|native-arm64` selection layer

New file `tools/iolab-launcher/macos_profile_select.go` (+ test file), wired
into `macos_cli.go`'s `runDarwinCLI` ahead of the existing
`loadMacOSProfile` call. This is Phase-4-exclusive code — nothing here
existed on `luna/macos-m6-followups` or `luna/macos-m7-arm64` (M7 never
touched the launcher). See its package doc comment for the full precedence/
fail-closed contract. Summary:

- Explicit `--profile`/`IOLBOX_PROFILE` wins > persisted owner choice
  (`~/Library/Application Support/iolbox/profile-choice.env`) > `auto`.
- `auto` still defaults to `rosetta-amd64` until promotion; selecting
  `native-arm64` from `auto` requires the explicit test-only
  `IOLBOX_TEST_PREFER_NATIVE=1` env hook, and only when native preflight
  passes.
- `nativePreflight()` is non-mutating: Apple Silicon (hostFacts),
  Lima/VZ (`limactl info` vmTypes, read-only), digests (profile table pin),
  translator (identity constant), resources (hostFacts free disk).
- Forced `native-arm64` failing preflight is a hard error (fail-closed, no
  downgrade). Auto/persisted `native-arm64` failing preflight falls back to
  `rosetta-amd64` with a recorded `FallbackReason`.
- Legacy direct profile-table names (`debian13`/`jammy`/`debian12`) still
  resolve directly, unchanged, for backward compatibility.

**Real-hardware verification performed this session** (read-only, no VM
touched): `docs/m7-evidence/phase4/lima-vz-check-and-vm-inventory.log` —
confirms `limactl info` on the actual physical Mac
(`rohansharma@192.168.101.186`, Darwin 25.6.0, `arm64_T8103`) returns a
top-level `vmTypes` array (`["krunkit","qemu","vz"]`), exactly the JSON shape
`parseLimaVZSupport` assumes. Also confirms both protected Lima VMs
(`iolbox-m7-native-arm64-qemu` under `~/.lima-iolbox-m7p3`, `iolbox-m5-e2e`
under `~/.lima`) remain `Stopped` and untouched.

**Not yet done, and NOT wired into `status`/`diagnose` output**: `runStatus`/
`runDiagnose` in `macos_cli.go` still call `loadMacOSProfile` with the
already-resolved concrete profile name and report that row's fields; they do
not yet surface `profileSelectionResult`'s `Requested`/`Selected`/`Source`/
`FallbackReason`, or `rosetta_present` for a native guest (the existing
`collectDarwinDiagnostics`/`darwinDiagnostics` machinery in
`macos_diagnostics.go` is entirely Rosetta-canary-shaped — see below). This
is real remaining work.

## `packaging/macos/lima/iolbox-native-arm64.yaml` + `pinned-image-native-arm64.env`

Created and staged, adapted from the actual reviewed Phase 3 template
(`docs/macos-m7-phase3-execution-plan.md` section 3's "effective template":
`vmType: vz`, `arch: aarch64`, `rosetta: {enabled: false, binfmt: false}`)
into this launcher's `@TOKEN@` rendering convention, matching the shape of
`iolbox-trixie.yaml`/`iolbox-jammy.yaml`. Two deliberate departures from
Phase 3's exact bytes, both recorded in the files' own comments:

1. A real `virtiofs` `~` mount is included (Phase 3's own template used
   `mounts: []` for its narrower correctness measurement); Phase 4 item 4
   requires native-arm64 to also prove separate VM/state paths with correct
   host sync, which needs the mount every other profile gets.
2. The pin reuses this worktree's already-trusted `debian13` image
   URL/digest rather than re-verifying Phase 3's own separately-dated pin
   (`20260518-2482` vs. this worktree's `20260810-2566`) a second time on
   the physical Mac. The base cloud image is not what makes a guest
   "native" — Rosetta being disabled in the YAML, and the native payload/
   install path, are what does.

**`profiles.env` intentionally does NOT get a `native-arm64` row yet.**
Adding the row would make `nativePreflight`'s `digests` check pass while the
guest would actually still run the two **shared** files every profile row
points at by fixed name — `packaging/macos/guest/30-canary.sh` (executes an
amd64 loader **through Rosetta** and hard-requires Rosetta binfmt) and
`40-install-payload.sh` (installs the amd64 payload, then hard-asserts the
effective systemd unit's `ExecStart` is `/opt/iolbox/supervisor` translated
via `/mnt/lima-rosetta/rosetta` and that `IOLBOX_DISABLE_I386=1` is set) —
both of which are correctness-critical, heavily hardware-hardened Rosetta
gates that would either crash or (worse) silently misreport on a
Rosetta-disabled native-arm64 guest. Adding the row now would make
`nativePreflight` report "ready" for something that is not actually wired
up, which is precisely the kind of misrepresentation this project's rules
forbid. The honest state today: `--profile native-arm64` fails structurally
and clearly (`"native-arm64 profile is not present in this asset root's
profiles.env"`), not silently.

## Concrete remaining work (not started this session)

This is the real gap between what's committed here and Phase 4's exit
criterion. In rough dependency order:

1. Native-specific guest step scripts: a `30-canary-native.sh` (verifies NO
   Rosetta/binfmt present, runs a native arm64 loader/probe instead of the
   amd64 Rosetta one) and a native-aware `40-install-payload.sh` variant
   (or a profile-conditional branch in the existing one) that installs the
   `pack-native.sh --arch arm64` payload and asserts the effective
   `ExecStart` is the native arm64 supervisor, not a Rosetta-translated one.
   `macos_lifecycle.go`'s `runProvision` step-name wiring needs a matching
   per-profile-role branch (today it always runs the same four fixed
   filenames for every row).
2. Register the `native-arm64` row in `profiles.env` once (1) exists and is
   hardware-verified.
3. Extend `darwinDiagnostics`/`collectDarwinDiagnostics`
   (`macos_diagnostics.go`) and `runStatus`/`runDiagnose`
   (`macos_cli.go`) to report `rosetta_present` truthfully for a native
   guest (today `Execution`/`RosettaBinfmt` are hard-coded to the Rosetta
   canary's shape) and to surface `profileSelectionResult`'s requested/
   selected/fallback-reason fields (plan section 10 item 5).
4. Adapt `runtime/arch-validation-test.sh`/`package-contract-test.sh` to the
   hand-merged (packs-preserving) `pack-native.sh`, or accept them as
   superseded by direct hardware evidence.
5. Real hardware evidence for every plan section 10 item 4 case (forced
   native success; forced native preflight/runtime failure; forced Rosetta
   success+canary; auto native selection under test policy; auto fallback;
   persisted choice honored after restart; separate VM/state paths + host
   sync; recovery after a half-created native VM; recovery after forced
   launcher/VM termination). None of this was attempted this session beyond
   the one read-only `limactl info` verification above — items 1-2 must
   exist first, or "forced native success" cannot honestly be attempted at
   all (there is nothing for it to succeed at yet).

**Phase 4's exit criterion is NOT met.** This session produced: a clean,
tested, hardware-verified-in-part selection/preflight layer, and an honest,
non-misleading integration of the reviewed arch-portability units — but the
native-arm64 execution path itself (steps 1-3 above) does not exist yet, so
no forced-native/auto-fallback/isolation/recovery hardware case could be
honestly run.
