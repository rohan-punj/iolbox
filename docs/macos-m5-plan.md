# M5 plan — honest Apple Silicon capabilities and diagnostics

Planning baseline: `luna/macos-m5-honest-caps` at `0f6f5d5`, branched from
`luna/macos-m4-runtime`. This is a plan only. M4 is **PARTIAL**, not PASS:
items 1 and 6 were proven on hardware; items 2 and 7 have fixes that have not
been hardware-reconfirmed; items 3, 4, 5, and 8 were not attempted. M5 does
not close or restate those gaps.

The required reconciliation of `luna/macos-arm64-invariant` is already done at
`1fc99d8`. That work is M7 groundwork, touches no M5 file, is not in this
branch, and must not be merged for M5. In particular, M5 must not edit
`runtime/build-rootfs.sh`: the Mac consumes the **amd64** native payload under
Rosetta, not M7's arm64 rootfs profile. The existing `INCLUDE_I386` /
`--no-i386` build option is therefore adjacent but not the Mac capability
signal.

## 1. Verified baseline and terminology

The owner's seven surveyed facts are correct:

- `supervisor/internal/server/handlers.go` unconditionally creates
  `features := []string{"nvram", "capture", "i386"}` and only gates extnet and
  tool features.
- `server.Config.Runtime` and `Arch` default to `debian-slim-12` and `x86_64`;
  `supervisor/cmd/supervisor/main.go` supplies neither and has no current flag
  or environment input for a Mac capability policy.
- There are two different architecture axes. In this plan, **runtime arch**
  means hello's existing `arch` field. Its meaning and default remain exactly
  unchanged. **Image arch** means `image.Info.Arch`, parsed from the image ELF
  header as `i386`, `x86_64`, or `unknown`. GUI support decisions use image
  arch only.
- The GUI stores `hello.features`, but only `nodeCatalog.ts` consumes a feature
  today, and only for `natgw`. No GUI code reads `i386`. Images can enter the
  UI through the Add Nodes catalog, two image pickers, bulk replacement,
  automatic missing-image reconciliation, and existing lab documents.
- `docs/protocol.md` incorrectly calls `i386` an always-present base feature.
- `runDiagnose` hardcodes `execution=rosetta-amd64` and
  `guest_arch=aarch64`; `runStatus` lacks those fields. The literal is a defect,
  not evidence.
- M1's durable anchor is the existing
  `10-iolbox-macos-canary.conf` drop-in plus the guest/host structural
  attestation. The drop-in survives a native payload upgrade that rewrites the
  base unit.

One deployment-list correction is required. In addition to LXC, VMware,
native/cloud Linux, WSL, and OVA, the same supervisor is shipped in the QEMU
qcow2/Windows-launcher target and built directly into the Docker target.

## 2. Decision: an explicit fail-open environment switch

Use one boolean service environment variable:

```text
IOLBOX_DISABLE_I386=1
```

`supervisor/cmd/supervisor/main.go` parses only the exact value `1` and passes
`DisableI386: true` into `server.Config`. Missing, empty, `0`, and unrecognised
values all leave the default false. Add a small pure parser so this contract is
unit-testable without starting the daemon. Do not add a general execution
profile model.

The macOS provisioner writes the environment line into the existing
`/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf`
alongside its `ExecStartPre`. All other deployments have no such environment
line and retain the default.

### Candidate comparison

| Design | Translated target other than Lima/Rosetta | Missing-signal behavior | Non-Mac blast radius | Offline testing | In-place upgrade |
|---|---|---|---|---|---|
| Supervisor CLI flag such as `-disable-i386` | Yes, if that target's service explicitly supplies it | Can be made fail-open | A default-off flag is safe, but a systemd drop-in cannot append to an existing `ExecStart`; it must clear and repeat the complete command, coupling the Mac override to every future base-unit flag | Easy | Fragile: the drop-in survives, but its copied `ExecStart` becomes stale when the rewritten base unit changes |
| **Environment variable in the Mac drop-in** | **Yes; it describes a capability restriction, not Lima identity** | **Fail-open** | **Absent everywhere else; no shared unit, Docker entrypoint, or runtime package changes** | **Easy through parser and `server.Config` tests** | **Strong: systemd merges `Environment` with whatever new `ExecStart` the payload installs, and M1 already proved the drop-in survives** |
| Runtime self-detection in the supervisor | A Rosetta-path probe is Lima-specific; a real 32-bit exec probe is more generic | Probe errors force an uncomfortable choice between false denial and false advertisement | Highest: every target executes new startup probing; binfmt layout, containers, permissions, and chroots can create false results like M4's shared-code failure class | Possible only with extensive filesystem/exec fakes | Survives, but moves policy and platform probing into every deployment |

The environment switch wins because it is the smallest explicit deployment
contract and does not infer platform identity from incidental host state. A
future non-Lima translated target can set the same restriction after its own
qualification.

Absence deliberately means **advertise i386**. This is fail-open and preserves
the existing non-Mac feature array byte-for-byte. It is required to avoid
silently withdrawing a capability from WSL, VMware/OVA, LXC, QEMU, native
Linux, or Docker when they upgrade. The tradeoff is real: a Mac that loses its
drop-in could lie again. Detect that configuration drift in three places:

1. `40-install-payload.sh` verifies that `systemctl cat` and `systemctl show`
   expose both the canary `ExecStartPre` and `IOLBOX_DISABLE_I386=1`.
2. `50-verify.sh` sends a correlated hello request to port 4000 and fails the
   Mac verification if `i386` is present or the positive architecture list is
   wrong.
3. `iolbox diagnose` reports an invariant failure when a valid Mac structural
   attestation exists but the effective drop-in/hello advertises i386.
   Launcher `start`/`upgrade` must not report readiness until `50-verify.sh`
   passes.

Use a boolean, not a richer configured descriptor. `execution=rosetta-amd64`
and `guest_arch=aarch64` are observations, while `DisableI386` is policy derived
from a known limitation. Feeding configured strings into diagnostics would
repeat the current hardcoded-literal defect. Capability policy and positive
diagnostics are therefore independent: the former comes from the durable
drop-in; the latter is derived from live measurements and the canary record.

## 3. Supervisor and protocol surface

### Configuration and enforcement

Add `DisableI386 bool` to `server.Config`. In `handleHello`:

- always keep `nvram` and `capture`;
- append `i386` only when `DisableI386` is false;
- leave extnet/tool feature gating and feature order unchanged.

Also enforce the same policy in the IOL `buildSpec` path. When the selected
registered image has `image.ArchI386` and `DisableI386` is true, return the
already-defined `image_arch_mismatch` protocol error with a message such as
`node N: i386 IOL images are disabled by this runtime`. Do not reject
`x86_64` or `unknown` images in M5; changing the treatment of `unknown` is a
separate compatibility decision. This server-side check protects direct API
clients, stale/cached GUIs, node-level starts, and pre-existing labs. Bulk
start keeps its existing partial-failure behavior.

### Hello JSON

Do not change `arch`; it remains the runtime's advertised architecture and is
still `x86_64` on the Mac because the deployed supervisor/runtime payload is
amd64. Add this optional field to `protocol.HelloResult`:

```json
"iolArchitectures": ["x86_64"]
```

Its exact Go/TypeScript type is `[]string` / `string[]`, JSON name
`iolArchitectures`. Emit it only when `DisableI386` is true, using
`omitempty`; the value is exactly `["x86_64"]`. The ordinary default hello
therefore receives no new field and retains its old bytes/semantics. A Mac
hello becomes conceptually:

```json
{
  "supervisor": "<build>",
  "runtime": "debian-slim-12",
  "arch": "x86_64",
  "features": ["nvram", "capture", "...gated features..."],
  "iolArchitectures": ["x86_64"],
  "egress": "..."
}
```

The feature omission remains the compatibility-level negative capability;
the positive list removes ambiguity for the new GUI. A bare omission alone is
not sufficient for the GUI design because a client seeing no `i386` needs to
know whether the server made an authoritative architecture decision or is too
old to report one.

Compatibility rules:

- An old GUI ignores the additive `iolArchitectures` field. It may still draw
  an i386 image as selectable, so the new server-side start rejection is
  mandatory. Normal packaged use embeds the new GUI in the new supervisor,
  but cached/independent clients must still be safe.
- A new GUI talking to an old supervisor first checks whether
  `iolArchitectures` is present. If absent but legacy `features` contains
  `i386` (true for the current and documented old supervisor), i386 remains
  enabled. If both are absent, the GUI treats support as unknown and disables
  i386 with `This supervisor does not report 32-bit IOL support; upgrade the
  supervisor.` It must not silently guess supported.
- A new GUI talking to the M5 Mac uses the present list as authoritative and
  disables image arch `i386`.

Update `docs/protocol.md` with this conditional, the compatibility fallback,
and an explicit sentence distinguishing runtime `arch` from each image's ELF
`arch`.

## 4. GUI behavior and exact files

Recommend **show-but-disabled with a reason**, not hiding and not a warning
that still permits selection. Hiding makes a legitimately staged `i86bi_*`
image appear lost; allowing it with a warning describes an image as usable
when the runtime knows it cannot execute it.

Create one pure helper in `app/src/lib/imageSupport.ts` that accepts hello's
`features`, optional `iolArchitectures`, and a `LibraryImage`, and returns
`{ supported: boolean, reason?: string }`. Only image arch `i386` is restricted
in M5. Store the optional list distinctly from an empty list in
`labStore.svelte.ts` so “field absent” is not collapsed into “authoritative
empty set.”

Apply the helper at every image surface:

- `app/src/lib/protocol.ts`: add optional `iolArchitectures?: string[]`.
- `app/src/lib/labStore.svelte.ts`: retain the list from hello; expose
  `imageSupport`/`nodeImageSupport`; exclude unsupported images from automatic
  same-class reconciliation; reject unsupported targets in `setNodeImage` and
  `replaceImageEverywhere`; preflight `startLab`, `startNode`, and
  `restartNode` with the image filename and reason before any RPC.
- `app/src/lib/nodeCatalog.ts`: keep i386 entries in “IOL images” but set the
  existing `CatalogEntry.disabled` string to the reason.
- `app/src/App.svelte`: actually honor `entry.disabled`: set native
  `disabled`, make it non-draggable, do not call placement handlers, expose the
  reason in visible secondary text/title and the accessible label, and add a
  stable `data-image-id`/`data-supported` hook for the browser proof.
- `app/src/lib/components/ChangeImagePopover.svelte` and
  `app/src/lib/components/Inspector.svelte`: show i386 choices disabled with
  the same reason rather than filtering them out.
- `app/src/lib/components/ImageManager.svelte`: continue listing and allowing
  upload/removal of i386 files, add a Support column/badge with the reason,
  allow an unsupported image in the “From” selector so users can migrate away
  from it, and exclude it from the “To” selector.
- `app/src/lib/components/CanvasInner.svelte`: disable node and multi-selection
  Start actions when the relevant existing IOL node is unsupported; use the
  context menu's `title` for the reason and exclude unsupported nodes from the
  bulk `startable` set.
- `app/src/lib/nodes/NodeActions.svelte`: disable the hover Start/Restart
  affordance for an unsupported existing IOL node and expose the reason.
- `app/src/lib/mockTransport.ts`: keep the ordinary mock as legacy-supported
  (`features` contains `i386`) and add/use a Mac fixture in pure tests rather
  than changing the default development experience.

An existing lab document that references i386 remains loadable, visible,
editable, saveable, and exportable. The node keeps its original image binding;
there is no silent deletion or substitution. The Inspector explains that the
image is unavailable on this runtime, Start/Restart are disabled, and the user
can select an x86_64 replacement. Defensive store checks and the supervisor's
`image_arch_mismatch` rejection cover any missed UI path.

## 5. Native macOS provisioning configuration

Extend `packaging/macos/guest/40-install-payload.sh`'s existing
`install_canary_drop_in`; do not add a second drop-in. It should atomically
write:

```ini
[Service]
# Structural macOS/Lima gate ...
Environment=IOLBOX_DISABLE_I386=1
ExecStartPre=/opt/iolbox-provision/30-canary.sh --quiet
```

One file is preferable because both lines express the same qualified Mac
runtime invariant, are installed before the payload's first start, share the
existing attestation/verification path, and cannot drift independently by
drop-in ordering. Do not replace or repeat the base `ExecStart`.

Enhance `verify_gated_unit` to inspect both human-readable `systemctl cat` and
effective `systemctl show -p Environment -p ExecStartPre -p ExecStart`. Extend
`50-verify.sh` with a correlated hello read (do not assume the first NDJSON
frame is the reply) and require: `i386` absent and
`iolArchitectures:["x86_64"]` present. Add the effective capability signal to
the fact block/evidence.

An in-place upgrade proceeds as M1 proved: the provisioner stops any ungated
service, runs the canary, atomically rewrites the same drop-in including the
environment line, daemon-reloads, then runs the native installer. The
installer may overwrite the base unit; it does not overwrite the drop-in.
After the gated restart, effective-unit and hello verification must pass before
the structural attestation is mirrored host-side. Images, labs, iourc, and
cache are untouched.

## 6. Launcher diagnostics: measured values only

Remove the hardcoded lines at `macos_cli.go:276`. Refactor status and diagnose
to share a small diagnostic snapshot helper, with unit-testable parsing and
formatting. Both commands print these exact keys, one per line:

```text
guest_arch=<value>
execution=<value>
guest_kernel=<value>
structural_canary=<value>
```

Measurement sources:

- `guest_arch`: live `limactl shell <machine> uname -m`.
- `guest_kernel`: live `uname -r`.
- `structural_canary`: the latest guest
  `/var/lib/iolbox/macos-canary.json` verdict/timestamp, cross-checked with the
  configured `ExecStartPre`; when the guest cannot be queried, clearly label
  the host-mirrored `<machine>-structural-gate.json` value as the **last
  attested** result rather than current state.
- `execution`: report `rosetta-amd64` only when all measured facts agree:
  guest arch is `aarch64`; the live binfmt entry is enabled and names
  `/mnt/lima-rosetta/rosetta`; the canary record is PASS for amd64 loader
  execution; and the supervisor service/hello is reachable. Otherwise print
  `unknown (<specific failed predicate>)`. A configured
  `IOLBOX_DISABLE_I386=1` is never an input to this value.

`diagnose` retains its raw binfmt, effective drop-in, service, HTTP, hello, and
live-canary detail after the four summary keys. Its live canary line remains
`live_canary=PASS` or `live_canary=FAIL (<error>)`; it must not promote a
failed live canary to `execution=rosetta-amd64`. It also prints
`capability_policy=PASS` only when the effective environment and hello agree,
otherwise `capability_policy=FAIL (<reason>)`.

Unavailable-state contract:

| Situation | `guest_arch` / `guest_kernel` | `execution` | `structural_canary` |
|---|---|---|---|
| Machine not created or stopped | `unavailable (machine is not running)` | same | `PASS (last attested <timestamp>)` if a valid host mirror exists, otherwise `unknown (never recorded)` |
| Lima says running but guest shell is unreachable | `unavailable (guest unreachable: <error>)` | `unknown (guest unreachable)` | last host-attested value, explicitly labelled, or `unknown (never recorded)` |
| Canary has never run | live arch/kernel still print if reachable | `unknown (no passing Rosetta canary record)` | `unknown (never recorded)` |
| Live facts disagree or canary failed | measured arch/kernel print | `unknown (<mismatch/failure>)` | the actual non-PASS verdict and timestamp |

Do not print the profile's expected arch/kernel as if measured. Existing
`kernel pin:` and profile facts may remain, but must stay separately labelled
as expectations.

## 7. Blast-radius matrix

No runtime packer, rootfs, Dockerfile, Docker entrypoint, or non-Mac systemd
unit changes for M5. Every target receives the updated supervisor binary/GUI
when rebuilt, but only the Mac drop-in supplies the switch.

| Target sharing the supervisor | Signal present? | Hello/features | Image execution and GUI |
|---|---|---|---|
| Apple Silicon Lima/VZ + Rosetta | Yes, from macOS guest provisioning | omits `i386`; adds `iolArchitectures:["x86_64"]`; existing runtime `arch` stays `x86_64` | i386 shown unsupported and server-rejected; x86_64 unchanged |
| WSL2 rootfs | No | default hello remains legacy-shaped and includes `i386` | unchanged |
| VMware vmdk/vmx appliance | No | includes `i386` | unchanged |
| OVA imported into VMware/ESXi/VirtualBox | No | includes `i386` | unchanged; it is the same appliance rootfs in another package |
| Proxmox LXC template | No | includes `i386` | unchanged |
| Native systemd x86-64 Linux: bare metal, cloud VM, on-prem guest | No; `/etc/iolbox/bind.env` is not changed | includes `i386` | unchanged |
| QEMU qcow2 used by the Windows launcher/qemu-compat provider | No | includes `i386` | unchanged |
| Docker build-from-source target | No; it execs the supervisor directly and has no systemd drop-in | includes `i386` | unchanged |

Concrete non-Mac checks:

1. On the Windows dev host, run the cross-platform server tests, especially
   `TestHelloDefaultAdvertisesI386`; this pins the no-signal default without
   requiring Linux data-plane privileges.
2. On an actual amd64 Linux deployment, install/start the M5 payload **without**
   setting `IOLBOX_DISABLE_I386`, then send a correlated hello to
   `127.0.0.1:4000` and save the reply. Require `features` to contain `i386`,
   require `iolArchitectures` to be absent, and start one i386 image if a legal
   test image is available.

No usable non-Mac deployment is currently known from this planning session:
Docker is unavailable and WSL enumeration returns
`Wsl/EnumerateDistros/Service/E_ACCESSDENIED`. The owner must either provide an
amd64 Linux target or approve an ephemeral CI/native-Linux smoke. This is a
real-hardware acceptance blocker for the non-Mac criterion, not something a
Mac SSH run can prove.

## 8. Exact implementation file set

Expected product/configuration files:

- `supervisor/internal/server/server.go`
- `supervisor/internal/server/handlers.go`
- `supervisor/internal/protocol/verbs.go`
- `supervisor/cmd/supervisor/main.go`
- focused `_test.go` files beside those packages
- `app/src/lib/protocol.ts`
- `app/src/lib/imageSupport.ts` (new) and `app/tests/imageSupport.test.ts` (new)
- `app/src/lib/labStore.svelte.ts`
- `app/src/lib/nodeCatalog.ts`
- `app/src/App.svelte`
- `app/src/lib/components/{ChangeImagePopover,Inspector,ImageManager,CanvasInner}.svelte`
- `app/src/lib/nodes/NodeActions.svelte`
- `app/src/lib/mockTransport.ts`
- `packaging/macos/guest/{40-install-payload,50-verify}.sh`
- a focused macOS shell test under `packaging/macos/tests/`
- `tools/iolab-launcher/macos_cli.go`
- `tools/iolab-launcher/macos_lifecycle.go` and/or one new focused diagnostics
  helper, plus tests
- `docs/protocol.md`, `docs/INSTALL.md`, and
  `tools/iolab-launcher/README.md`

Do not edit `runtime/build-rootfs.sh`, `runtime/fetch-vpcs.sh`, any M7 file,
the base `runtime/files/iolbox-supervisor.service`, Docker files, or frozen
M0-M4 plan/result documents. `docs/INSTALL.md` and the launcher README should
state that Apple Silicon supports x86_64 IOL only, that staged i386 images stay
visible but disabled, and show the four diagnostic keys. M6 still owns the
one-download release artifact, signing/quarantine procedure, and final release
assembly; do not pull those into M5.

## 9. Offline and unit tests

Add and run these exact focused tests before the full suites:

### Supervisor

- `supervisor/internal/server/server_test.go`
  - `TestHelloDefaultAdvertisesI386`: zero/default config contains `i386`,
    omits `iolArchitectures`, and keeps runtime `arch == "x86_64"`.
  - `TestHelloDisableI386OmitsFeature`: `DisableI386:true` omits `i386`, emits
    exactly `["x86_64"]`, and does not change runtime `arch`.
  - `TestDisableI386RejectsI386Image`: `buildSpec`/single-node start returns
    `image_arch_mismatch` before spawn.
  - `TestDisableI386AllowsX8664Image`: the same config reaches the ordinary
    x86_64 spec path unchanged.
- Update `TestHelloAdvertisesGatedFeatures` so its base-feature assertion still
  pins the default and does not weaken extnet coverage.
- `supervisor/cmd/supervisor/main_test.go`
  - `TestDisableI386Environment`: table-test absent, empty, `0`, `1`, and
    malformed values; only `1` enables the switch.

Commands:

```text
cd supervisor
go test ./internal/server -run 'TestHello(DefaultAdvertisesI386|DisableI386OmitsFeature)|TestDisableI386(RejectsI386Image|AllowsX8664Image)'
go test ./cmd/supervisor -run TestDisableI386Environment
go test ./...
go vet ./...
```

### GUI

- `app/tests/imageSupport.test.ts`
  - `authoritative Mac list disables i386`
  - `authoritative Mac list allows x86_64`
  - `legacy i386 feature preserves old supervisor support`
  - `missing list and missing legacy feature fails closed as unknown`
  - `unsupported image remains visible with actionable reason`
- Exercise store guards for lab start, node start, reassignment, bulk replace,
  and reconciliation either by pure extracted helpers in the same test or a
  small store-focused test; do not rely only on Svelte typechecking.

Commands:

```text
cd app
node --test tests/imageSupport.test.ts
npm run check
npm run build
```

### Provisioning and launcher

- `packaging/macos/tests/capability-config-test.sh` (new): render/source the
  drop-in helpers in a temporary directory and assert the environment line,
  canary line, atomic/idempotent rewrite, effective-unit parser, and hello
  verifier for both Mac-negative and ordinary-positive fixtures.
- Extend `packaging/macos/tests/lint.sh`'s enumerated scripts if necessary, then
  run all existing shell fixture tests.
- `tools/iolab-launcher/macos_diagnostics_test.go`:
  - `TestMeasuredDiagnosticsReportsRosettaAMD64`
  - `TestMeasuredDiagnosticsNeverTrustsConfiguredCapability`
  - `TestMeasuredDiagnosticsUnavailableStates`
  - `TestMeasuredDiagnosticsCanaryNeverRun`
  - `TestCapabilityPolicyMismatchIsReported`
  - `TestStatusAndDiagnoseContainNoHardcodedExecutionLiteral` (drive mocked
    Lima responses and captured output; a source-text grep alone is not enough).

Commands:

```text
bash packaging/macos/tests/lint.sh
bash packaging/macos/tests/capability-config-test.sh
cd tools/iolab-launcher
go test ./...
go vet ./...
```

## 10. Payload build and delivery for M5 hardware work

M5 needs a new GUI-embedded linux/amd64 supervisor. Reusing the old v0.5.3
tarball unchanged would invalidate the test. The deleted `iolab-runtime-vm`
builder is not required for this focused payload if the following controlled
splice is used:

1. In a disposable copy under `J:\tmp` (not this worktree), run `npm ci` and
   `npm run build:embed`, then cross-compile
   `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` from
   `supervisor/cmd/supervisor` with an explicit `m5-<implementation-id>` ldflag.
   Windows already has Node, npm, and Go. This reproduces the deployable part
   of `build-release.sh` without its tracked-placeholder restore touching the
   implementation worktree.
2. Use the known-good Mac base
   `~/iolbox-m0/iolbox-server-v0.5.3-netprobe-cgroupfd-fix.tar.gz` (sha256
   `eadfe20e926cf30766c3acb7c5daec49ee90fd546873176da03f17cdcc0c282c`).
   Copy it and the new supervisor into the running `iolbox-m4-e2e` Linux guest,
   extract to `mktemp -d`, replace only the payload's supervisor with mode
   0755, and repack under a new, unambiguous name such as
   `iolbox-server-m5-<implementation-id>.tar.gz`.
3. Copy the new tarball back to `~/iolbox-m1/m5/`, record its sha256 and full
   tar member list, and execute its supervisor `--version` inside the guest.
   Preserve the original base tarball. Never name or pass the new artifact as
   v0.5.2/v0.5.3.
4. Stage the updated `packaging/macos/` assets and newly built darwin/arm64
   launcher under `~/iolbox-m1/m5/`; do not reuse the older staged guest
   scripts.

This produces a valid focused hardware-test payload while retaining the
known-good VPCS/tool-pack/install content that M4 depended on. It is not the
final release build. Before M6/release, recreate a Linux builder or use the
repository's Release workflow and run `runtime/pack-native.sh` normally.
Open risk: the splice procedure needs one dry run to confirm the base
tarball's top-level layout and ownership/mode preservation; abort rather than
guess if its supervisor path is not unique.

## 11. Real-hardware verification

Use `rohansharma@192.168.101.166`, key
`.m5-ssh/iolbox_mac_m0`, profile `debian13`, and the existing stopped
`iolbox-m4-e2e` machine. `iol22` is forbidden. Reusing `iolbox-m4-e2e` is
intentional: the handoff marks it reusable and it already has the qualified
x86_64 router/L2 images. All launcher start/upgrade invocations must pass the
new M5 tarball explicitly; never accept a harness default.

The implementation should add a small Bash-3.2-safe
`packaging/macos/tests/hardware-m5.sh` that records every command/status and
creates `~/iolbox-m1/evidence-m5/<run-id>/`. The manual command sequence it
wraps is:

```powershell
$Key = 'J:\Claude code\iolab-m5-wt\.m5-ssh\iolbox_mac_m0'
$Mac = 'rohansharma@192.168.101.166'
$Stage = '~/iolbox-m1/m5'
ssh -i $Key $Mac "mkdir -p $Stage"
scp -i $Key <new-darwin-launcher> "${Mac}:$Stage/iolbox-launcher-m5"
scp -i $Key -r 'J:\Claude code\iolab-m5-wt\packaging\macos' "${Mac}:$Stage/assets"
scp -i $Key <new-m5-payload> "${Mac}:$Stage/iolbox-server-m5-<implementation-id>.tar.gz"
ssh -i $Key $Mac "chmod 0755 $Stage/iolbox-launcher-m5 $Stage/assets/tests/hardware-m5.sh"
ssh -i $Key $Mac "$Stage/assets/tests/hardware-m5.sh --machine iolbox-m4-e2e --profile debian13 --launcher $Stage/iolbox-launcher-m5 --assets-dir $Stage/assets --tarball $Stage/iolbox-server-m5-<implementation-id>.tar.gz --x86-image ~/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin --evidence-parent ~/iolbox-m1/evidence-m5"
```

The harness must perform, in order:

1. Record `sw_vers`, host/guest `uname`, Lima version/list, input hashes, exact
   launcher and supervisor versions, tar member list, and the explicit command
   line. Evidence: `environment.txt`, `inputs.sha256`, `payload-members.txt`,
   `versions.txt`.
2. Run launcher `upgrade` with `--machine iolbox-m4-e2e --profile debian13
   --assets-dir ... --tarball <exact-M5-path> --no-browser`, then `start` with
   the same explicit tarball if needed. Poll actual HTTP/WS readiness, not just
   Lima state. Evidence: `upgrade.log`, `start.log`, `readiness.txt`, effective
   `systemctl cat/show`, guest and mirrored structural JSON.
3. Capture the correlated hello response and assert: `i386` absent,
   `iolArchitectures` exactly `["x86_64"]`, and runtime `arch` still
   `x86_64`. Evidence: `hello-mac.json` plus an assertion log. This proves
   acceptance criterion 1 and guards the “do not repurpose arch” rule.
4. Run `iolbox status` and `iolbox diagnose`; require measured
   `guest_arch=aarch64`, `execution=rosetta-amd64`, the live `uname -r` value,
   and canary PASS. Independently capture `uname -m`, `uname -r`, binfmt,
   `/var/lib/iolbox/macos-canary.json`, the structural attestation, and the
   effective drop-in, then compare rather than trusting launcher prose.
   Evidence: `status-running.txt`, `diagnose-running.txt`, and
   `diagnostic-ground-truth.txt`. Stop the machine and record
   `status-stopped.txt` to exercise unavailable/last-attested labels.
5. Create/register a **synthetic test-only ELF32** file named
   `i86bi_m5_unsupported.bin` (valid ELF32/EM_386 header, >1 KiB) so no Cisco
   binary is copied into evidence. Confirm `image.list` reports `arch:i386`.
   Use a real browser through the loopback GUI to prove the palette shows it
   disabled with the Rosetta reason, Image Manager keeps it visible as
   Unsupported, both image pickers disable it, and a saved lab already
   referencing it stays visible but cannot Start/Restart. Also send a direct
   `node.start` and require server-side `image_arch_mismatch`. Evidence:
   `image-list-i386.json`, `direct-i386-start.json`, browser DOM assertions,
   and screenshots `gui-palette-disabled.png`, `gui-image-manager.png`, and
   `gui-existing-lab.png`.
6. Run the existing M1 two-router phase against the real x86_64 image after
   the M5 upgrade:

   ```text
   IOLBOX_EVIDENCE_ROOT=<m5-evidence>/x86_64-lab \
     <assets>/tests/hardware-m1.sh \
       --phase install-image-and-lab \
       --machine iolbox-m4-e2e \
       --image ~/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin
   ```

   Require both nodes running and the final IOS ping success threshold already
   enforced by that harness. Evidence: the M1 phase's `lab-start.json`, status,
   console transcript, and `last-success-rate.txt`. This proves the ordinary
   x86_64 IOL path is unaffected (criterion 5).
7. Stop cleanly, hash the complete evidence tree, and write a small summary
   that marks each criterion PASS/FAIL/NOT RUN without rounding up.

The M3 browser-equivalent HTTP/WS flow is useful for hello, image registration,
and direct start rejection, but it is **not sufficient** for criterion 4: it
does not render the Svelte components or prove that controls are visibly
disabled. Establish an SSH loopback tunnel from Windows if needed
(`ssh -i <key> -N -L 44001:127.0.0.1:4001 <Mac>`) and use a real Chrome/Safari
session at `http://127.0.0.1:44001/`. Capture both screenshots and DOM
properties through the browser. Merely opening a tab, as the optional M3 probe
does, is also insufficient.

Criterion 2 cannot logically be demonstrated “against the Mac” because it is
about a non-Mac target. After the Mac sequence, run this on an owner-supplied
amd64 Linux system or ephemeral amd64 Linux CI host using the same M5 payload:

```bash
sudo ./install.sh --bind local
exec 3<>/dev/tcp/127.0.0.1/4000
printf '%s\n' '{"id":"m5-nonmac","op":"hello","args":{"client":"m5-hardware"}}' >&3
while IFS= read -r -t 5 line <&3; do
  printf '%s\n' "$line" | tee -a hello-frames.ndjson
  case "$line" in *'"id":"m5-nonmac"'*) break;; esac
done
systemctl show iolbox-supervisor.service -p Environment > service-environment.txt
```

Save `hello-frames.ndjson`, `service-environment.txt`, supervisor version, host
`uname -a`, and payload sha256. Require `i386` present and no
`IOLBOX_DISABLE_I386`. If no such target is supplied, M5's result must remain
PARTIAL for criterion 2 even if all Mac checks pass.

## 12. Risks and owner decisions

1. **Non-Mac hardware is unresolved.** WSL is inaccessible in this session and
   Docker is absent. The owner must name an amd64 Linux target or approve an
   ephemeral CI smoke; unit tests alone do not meet the real-target criterion.
2. **Focused payload splice versus fresh native package.** The splice is a
   concrete way to run M5 without the deleted builder and preserves the known
   good v0.5.3 pack contents. The owner may instead recreate the preserved
   Linux builder. Either way, record the exact tarball hash and never let an M4
   harness default overwrite it.
3. **Old/cached GUI wording in the acceptance criterion is ambiguous.** No
   additive server field can force an old GUI to render a disabled row. This
   plan defines the criterion against the M5 embedded GUI and makes old clients
   safe through server-side rejection. If “never offers” is intended to cover
   arbitrary old cached clients visually, the only solution is an explicit UI
   version/cache invalidation requirement outside the stated 0.5–1 day scope.
4. **Unknown image arch remains allowed.** M5 targets the proven i386 false
   claim only. Treating `unknown` as unsupported would be safer in the abstract
   but would change existing behavior without an acceptance requirement.
5. **Diagnostic derivation must not overclaim.** A binfmt filename alone is not
   enough. `execution=rosetta-amd64` requires the combined live arch, enabled
   interpreter, passing amd64 canary record, and reachable translated service.
   Any parser uncertainty prints `unknown`, not the expected profile value.
6. **Synthetic ELF32 browser fixture.** This proves classification and UI
   behavior without distributing Cisco software. If the owner requires a real
   `i86bi_*` file for the GUI proof, it must be supplied privately and must not
   enter the repository/evidence bundle.
7. **M4 remains PARTIAL.** Reusing `iolbox-m4-e2e` and its image does not imply
   that unexecuted M4 items passed. M5 result/handoff must repeat that boundary.

## 13. Completion gate

M5 is PASS only when all offline suites pass and evidence proves:

1. Mac hello omits `i386`, positively lists only x86_64 IOL, and retains
   runtime `arch:x86_64`.
2. A real ordinary amd64 non-Mac target still advertises `i386` with no signal.
3. Status/diagnose values match independent measurements and the structural
   canary, with no hardcoded success path.
4. The M5 GUI visibly retains-but-disables i386 images across every picker and
   existing-lab flow, and the server rejects a bypass.
5. The real two-node x86_64 IOL lab still boots and passes traffic.

Anything not run is recorded as NOT RUN/PARTIAL in `docs/macos-m5-result.md`;
the implementation session must also write `docs/macos-m5-handoff.md` using
the M4 documents' evidence-first format.
