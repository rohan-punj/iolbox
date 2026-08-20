# M5 luna implementation report

Run date: 2026-08-15 (Mac local time)

Worktree: `J:\\Claude code\\iolab-m5-wt`

Branch: `luna/macos-m5-honest-caps`

Hardware: `rohansharma@192.168.101.166`, macOS 26.6.1 (25G76), arm64, Lima
2.2.0. The fresh machine was `iolbox-m5-e2e`. I did not stop, reconfigure,
upgrade, or delete `iolbox-m4-e2e`, `iol22`, or any other existing machine.

## Implementation

- Removed the proposed `iolArchitectures`/Hello-field approach entirely.
  The GUI uses `hello.features.includes("i386")` as the sole capability signal.
- Added the fail-open supervisor policy (`IOLBOX_DISABLE_I386=1` omits `i386`;
  absent/other values retain it) and hoisted i386 image rejection before
  capture/fabric/preparation side effects. Rejection remains a per-node
  failure and only applies to `image.ArchI386`; unknown architecture remains
  allowed.
- Added one shared lab-level unsupported-image reason and applied it to the
  palette, Start Lab, Start All, node actions, image pickers, and Image
  Manager surfaces. Unsupported images remain visible and selectable only as
  disabled entries with the same reason.
- Added measured macOS diagnostics: guest architecture, independent Rosetta
  binfmt evidence, live loader canary, kernel, service, HTTP, hello, capability
  policy, structural attestation, and effective drop-in are separate values.
  Stopped machines report unavailable versus last-attested values.
- Added correlated hello verification to the macOS guest verification path.
- Kept the supervisor guest GUI listener at 4001 while allowing a host-side
  GUI port override. This was needed because the required fresh M5 machine
  was created while M4 owned the default host ports.

## Exact files changed

- `supervisor/internal/server/server.go`
- `supervisor/internal/server/handlers.go`
- `supervisor/cmd/supervisor/main.go`
- `supervisor/cmd/supervisor/main_test.go`
- `supervisor/internal/server/m5_capability_test.go`
- `app/src/lib/imageSupport.ts`
- `app/src/lib/nodeCatalog.ts`
- `app/src/lib/labStore.svelte.ts`
- `app/src/App.svelte`
- `app/src/lib/components/TopBar.svelte`
- `app/src/lib/components/CanvasInner.svelte`
- `app/src/lib/components/Inspector.svelte`
- `app/src/lib/components/ChangeImagePopover.svelte`
- `app/src/lib/components/ImageManager.svelte`
- `app/tests/image-support.test.ts`
- `tools/iolab-launcher/macos_diagnostics.go`
- `tools/iolab-launcher/macos_diagnostics_test.go`
- `tools/iolab-launcher/macos_cli.go`
- `tools/iolab-launcher/macos_ports.go`
- `tools/iolab-launcher/macos_lifecycle.go`
- `packaging/macos/guest/40-install-payload.sh`
- `packaging/macos/guest/50-verify.sh`
- `packaging/macos/tests/capability-policy-test.sh`
- `packaging/macos/tests/hardware-m1.sh`
- `docs/protocol.md`
- `docs/INSTALL.md`
- `tools/iolab-launcher/README.md`
- `docs/m5-session-logs/luna-implementation-report.md`

No frozen M0-M4 document, Docker file, `docs/providers.md`,
`runtime/build-rootfs.sh`, `runtime/fetch-vpcs.sh`, M7 file, or native
supervisor systemd base unit was changed.

## Offline gates

The commands below were rerun after the final source changes.

### Supervisor

Command:

```text
cd supervisor && go test ./... && go vet ./...
```

Verbatim test output:

```text
ok   github.com/rohanpunj/iolbox/supervisor/cmd/supervisor (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/bcap (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/consolescript (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/dirstat (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/egress (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/extnet (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/fabric (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/image (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/iourc (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/iouyap (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/lab (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/netmap (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/node (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/nvram (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/painter (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/protocol (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/relay (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/server (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/slowtee (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/tool (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/vtap (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/web 0.366s
ok   github.com/rohanpunj/iolbox/supervisor/internal/ws (cached)
ok   github.com/rohanpunj/iolbox/supervisor/internal/wsbridge 0.772s
```

`go vet ./...` exited 0 with no diagnostics.

### Launcher

Command:

```text
cd tools/iolab-launcher && go test ./... && go vet ./...
```

Verbatim test output:

```text
ok   github.com/rohanpunj/iolbox/tools/iolab-launcher (cached)
```

`go vet ./...` exited 0 with no diagnostics.

### App

Command:

```text
cd app && npm run check && npm run build && node --experimental-strip-types tests/image-support.test.ts
```

Verbatim result lines:

```text
COMPLETED 311 FILES 0 ERRORS 0 WARNINGS 0 FILES_WITH_PROBLEMS
✓ built in 1.71s
image support tests: 5 passed
```

The Vite build emitted only the existing large-chunk warning; it exited 0.

### macOS shell fixtures

Commands:

```text
cd packaging/macos/tests
bash lint.sh
bash canary-classify-test.sh
bash profile-lifecycle-test.sh
bash source-policy-test.sh
bash kernel-policy-test.sh
bash capability-policy-test.sh
```

Verbatim summaries:

```text
Summary: 14 cases, 0 failures
Summary: 18 cases, 0 failures
Summary: 7 cases, 0 failures
Summary: 11 cases, 0 failures
capability policy tests: 5 passed
lint.sh: all checks passed
SKIPPED shellcheck is not installed — these scripts have NOT been
        shellcheck-clean-verified on this host.
```

## Payload and launcher artifacts

`bash build-release.sh` was run with a worktree-local Go cache after the
Windows default cache location returned Access Denied. The build embedded the
GUI and cross-compiled `supervisor/bin/supervisor-linux-amd64`; the script
restored the committed placeholder afterward.

The Mac payload was created from the M0 v0.5.3 probe tarball by replacing only
`bin/supervisor` and retarring with `COPYFILE_DISABLE=1`:

```text
~/iolbox-m0/iolbox-server-m5-luna.tar.gz
sha256=fa9f919229c440f9ac97b54586a7bed6f8c0c4db4a26df7371f40d085d4838fb
```

The Mac tar listing showed executable `install.sh` and executable
`bin/supervisor` (13,428,235 bytes). In the Linux guest,
`test -x /opt/iolbox/supervisor` passed and `supervisor --version` returned:

```text
v0.5.2-22-g0f6f5d5-dirty
```

The Darwin launcher was cross-compiled with `GOOS=darwin GOARCH=arm64`, copied
to the Mac, chmodded 0755, and identified there as `Mach-O 64-bit executable
arm64`.

## Hardware runs and criteria

All evidence below is on the Mac under `~/iolbox-m1/evidence-m5/`.

| Criterion | Result | Evidence |
|---|---|---|
| 1. Mac hello omits `i386`, runtime arch remains `x86_64` | PASS | `m5-luna-20260815T0118/criterion-1-hello.txt`; correlated request id `m5-criterion-1-hello`, `runtime_arch=x86_64`, features omit `i386`, guest `uname -m=aarch64` |
| 2. Non-Mac default advertises `i386` | PARTIAL by owner decision | `m5-luna-20260815T0118/criterion-2-go-default.txt`; `criterion-2-live-absent-signal.txt` shows the same M5 binary run with `env -u IOLBOX_DISABLE_I386` returned `features=[...,"i386",...]`. This was not a genuine non-Mac deployment target and is not being rounded up. |
| 3. Diagnostics measure architecture, execution, kernel, canary, and stopped labels | PASS | `m5-luna-20260815T0118/diagnose-live-fixed.txt`, `ground-truth.txt`, `diagnose-stopped.txt`; live output reports `guest_arch=aarch64`, `execution=rosetta-amd64`, kernel `6.12.101+deb13-cloud-arm64`, `canary_verdict=PASS`, service/HTTP/hello separately; stopped output reports unavailable and last-attested values. |
| 4. GUI never offers i386 | PARTIAL | Protocol/state evidence is in `m5-luna-20260815T0118/criterion-4-fixture-register.txt` and `criterion-4-unsupported-lab-and-rejection.txt`: synthetic 2,048-byte ELF32/EM_386 registered as `arch:i386`, appears in `image.list`, saved lab loads, and direct Start/Restart both return per-node `image_arch_mismatch`. The required rendered browser screenshot over `ssh -L` was NOT RUN because both available browser surfaces were unavailable in this session; no browser PASS is claimed. |
| 5. x86_64 IOL lab unaffected | PASS | `m1-20260815T052417Z-43472/`; exact router image `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`, both nodes running, `last-success-rate.txt` is `Success rate is 90 percent (9/10...)`. |

### Criterion 3 ground truth

The independent capture records:

```text
uname -m: aarch64
uname -r: 6.12.101+deb13-cloud-arm64
binfmt: enabled; interpreter /mnt/lima-rosetta/rosetta; flags OCF
canary JSON verdict: PASS
structural attestation canary_verdict: PASS
effective drop-in: Environment=IOLBOX_DISABLE_I386=1
ExecStartPre=/opt/iolbox-provision/30-canary.sh --quiet
```

The live diagnostic explicitly reports `service=PASS` and
`execution=rosetta-amd64` independently. The earlier pre-fix diagnostic that
reported HTTP unavailable is retained as evidence of the defect discovery;
`diagnose-live-fixed.txt` is the final run.

### Criterion 5 hardware runs

The first real M1 phase run was `m1-20260815T051923Z-43180`. It correctly
copied and registered the real image, started both nodes, and then failed the
script's 180-second console-prompt deadline while IOS was still booting. The
captured console is retained in that run directory.

After making the timeout configurable and rerunning the same phase with
`IOLBOX_CONSOLE_BOOT_TIMEOUT=300`, run
`m1-20260815T052417Z-43472` passed:

```text
console LAST Success rate: Success rate is 90 percent (9/10), round-trip min/avg/max = 1/23/200 ms
ok: last console Success rate is 90% (>= 80%)
PASS: M1 hardware acceptance evidence collected in /Users/rohansharma/iolbox-m1/evidence-m5/m1-20260815T052417Z-43472
```

## Defects found during this session

1. The initial host-port remap incorrectly set both Lima guest and host GUI
   ports to 4101. Fixed by keeping guest 4001 and remapping only the host side
   in `macos_ports.go` and `macos_lima.go`. Launcher tests passed and the fresh
   machine was stopped/restarted with the corrected contract; provisioning then
   reached GUI readiness.
2. The launcher passed the host GUI port into guest verification, so
   `50-verify.sh` checked guest port 4101 instead of 4001. Fixed in
   `macos_lifecycle.go`; the complete guest sequence was rerun successfully.
3. Diagnostics used the host GUI port for its in-guest HTTP probe, yielding a
   false HTTP failure while hello and service were healthy. Fixed in
   `macos_diagnostics.go` and `macos_cli.go`; the corrected live diagnostic
   reports HTTP 200 and capability policy PASS.
4. The first real IOS hardware run hit the existing 180-second prompt limit
   before the 17.18.02 image finished booting. Fixed by making the harness
   timeout configurable with the unchanged 180-second default; rerun at 300
   seconds passed with 90% ping success.
5. A first artifact-verification command attempted `--version` on the Darwin
   host copy. It was not used as evidence. The required verification was then
   performed in the Linux guest and recorded in
   `guest-payload-executable-version.txt`.
6. The rendered browser proof could not be executed because the in-app browser
   and Chrome control surfaces both reported unavailable. The SSH tunnel itself
   was live and `GET /` returned 200, but no screenshot or visual browser claim
   is made; criterion 4 remains PARTIAL.

The owner should write the final M5 result and handoff documents from this
report. This session did not write either frozen result/handoff document.
