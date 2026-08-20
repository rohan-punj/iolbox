# M2 implementation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Implement **M2** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab`, using **luna at xhigh reasoning** for implementation and
**sol at medium** for planning.

## Read first, in this order

1. `docs/macos-m1-handoff.md` — **start here.** State of the world, the measured
   platform facts, 16 gotchas, open items. Written at the end of the M1 session.
2. `docs/macos-m1-result.md` — the executed M1 acceptance record.
3. `docs/macos-m0-result.md` — the M0 hardware record. **Immutable**, and
   correct *for macOS 13.5* — its baseline has moved; see the handoff §5.
4. `docs/macos-arm64-plan.md` §M2 — the slice being implemented. **Immutable.**

Where these disagree, precedence is: M1 handoff > M1 result > M0 result > plan.

## Context you must not re-litigate

M1 is **complete and proven on hardware**. One command takes a clean Mac to a
working guest running two real Cisco IOL routers passing traffic:

- Default guest is **Debian 13 trixie**, kernel `6.12.101+deb13-cloud-arm64`,
  pinned by `sha512:7a0eeb42…`. Jammy is the COMPATIBILITY profile.
- The Rosetta canary PASSes on kernels 5.15, 6.8 **and** 6.12 under macOS
  26.6.1. The `AT_RSEQ_ALIGN` constraint that defined M0 **does not apply** on
  this macOS. It still applies on macOS 13.5. The fix point is **UNVERIFIED** —
  never gate on a macOS version comparison; the runtime canary is the authority.
- Payload **v0.5.2** installs and upgrades in place, preserving images, cache,
  host id and licence.
- The canary is enforced **structurally** by a systemd `ExecStartPre` drop-in,
  so every boot/restart/crash-restart runs it before the amd64 supervisor.
- Measured: two-node IOL lab, **90% (9/10)**, RSS ~537 MiB/node.

Do not redesign the profile model, the pins, the exit-code contract, or the
structural gate. Extend them.

## Scope — build exactly this

**The Darwin launcher (`tools/iolab-launcher/`)**: replace the shell entry point
`packaging/macos/iolbox-mac.sh` with an idempotent Go CLI, **preserving the
Windows launcher and its tests**.

- `GOOS=darwin GOARCH=arm64 go build` succeeds in CI; existing Windows launcher
  tests and build stay green.
- One command on a clean Mac with Lima installed: create-or-reuse the named VM,
  run the M1 preflight/provisioning, wait on `GET /`, open the browser.
- `start`, `stop`, `status`, `diagnose`, upgrade — deterministic exit codes.
  **`stop` never deletes guest or host data.**
- Diagnostics show macOS version, Lima version, guest kernel/arch, canary
  result, and `execution=rosetta-amd64` alongside the supervisor's self-report.
  Do not redefine the existing `arch` field.

### Hard requirements carried from M1

1. **Do not re-encode the profile table in Go.** Read
   `packaging/macos/lima/profiles.env`. Duplicating it creates a divergence
   seam, and every defect on this project has been at a seam.
2. **Never compare macOS versions numerically.** Qualification is exact
   `(profile, product, build)` string equality. An unmeasured host is
   `UNMEASURED — CANARY REQUIRED`, which is not a refusal — the live canary
   decides.
3. **Control-plane client rules** (all measured, see handoff §4 items 8-11):
   NDJSON over `127.0.0.1:4000`; request `id` must be a **string**; the server
   pushes unsolicited event frames continuously, so **correlate replies by id**
   and never read-one-line; `ok:true` means understood, **not** succeeded —
   inspect `result.warnings` and `result.failed[]`; `lab.saveDoc`/`lab.listDocs`
   use raw **YAML strings** while `lab.load` takes a structured **JSON object**;
   the envelope and `result` both carry `id`.
4. **Liveness is not readiness.** Poll for the real signal: supervisor `active`
   does not mean :4001 is bound; node `running` does not mean IOS booted or the
   console is serving. There is **no `/api/health`** — readiness is `GET /`
   with status < 500.
5. **Surface Lima's Rosetta warning.** A stale Homebrew bottle makes Lima log
   `Unable to configure Rosetta: … unsupported build target macOS version` and
   still report `READY`, leaving a guest that cannot execute amd64 at all. The
   fix is `brew reinstall lima` (`brew upgrade` is a no-op when the version is
   current). `diagnose` must show this.
6. **`fmt` the Go, keep it stdlib-only** — the shipping launcher is
   dependency-free (`tools/iolab-launcher/README.md`).

## Out of scope — do not touch

M3 (browser/sync/console/capture UX), M4 (runtime matrix), M5 (i386 capability
gating), M6 (release packaging), M7 (arm64/FEX). Do not modify
`docs/macos-m0-result.md`, `docs/macos-arm64-plan.md`,
`docs/macos-arm64-plan-review.md`. Do not change the M1 guest steps except for
defects M2 demonstrates.

## Validation

The Mac is reachable at `rohansharma@192.168.101.166` (note: **not** .144) with
key `~/.ssh/iolbox_mac_m0`. Homebrew and Lima are at `/opt/homebrew/bin/` and
are **not on the non-login PATH** — always use absolute paths over SSH.

**The Mac has bash 3.2.57 only** — no Homebrew bash. Any shell you touch must
be 3.2-clean, and must be re-run there: `"${arr[@]}"` on an empty array is an
unbound-variable error under `set -u` on 3.2 but safe on 4.4+.

Existing machines on the Mac — `iol22` is the **irreplaceable M0 evidence
machine, never touch it**; `m1jammy`, `m1trixie`, `iolbox-m1-e2e` are M1's and
may be reused or deleted.

Reuse `packaging/macos/tests/hardware-m1.sh` as the acceptance harness rather
than writing a new one; it already encodes the protocol and console lessons
(readiness-aware console driver, `enable` before extended ping, take the **last**
`Success rate` match because the console replays its ring buffer).

M2 is done when, from a clean state on that Mac, the Go launcher produces a
guest that passes the canary, serves `GET /` with status < 500, boots the
two-node IOL lab, and survives a restart — and when `GOOS=darwin GOARCH=arm64
go build` plus the Windows launcher tests are green.

Report honestly: what you executed on hardware versus what you only wrote. State
any acceptance criterion you could not verify and why. **Do not report M2
complete on the strength of code that has not run on the Mac.**

## Working rules

- Work on a branch off `main`. **Do not commit to `main`, do not merge.**
  `luna/macos-m1-provisioner` holds M1; branch from `main` or from that branch
  if you need M1's provisioner present.
- If driving codex: pass the prompt as `- < prompt.md` on stdin. With the prompt
  as an argument it **hangs forever** unless the command ends `< /dev/null`.
  The codex sandbox has a read-only `.git`, so commits happen from the main
  session.
- Do not use `sed` to edit lines containing regex escapes — it interprets
  `\r`/`\n` and corrupts the file.
- Match the repo's existing style: stdlib Go, small explicit shell scripts.
- Check free disk on both boxes before creating VMs. J: has been full once
  already this project.

## Known open items inherited from M1

- **D11**: `IOLBOX_HOST_LIMA` reports `unknown` even when `limactl` is found.
  Fix this in the Go launcher — a canary failure message is not actionable
  without it.
- **Q1**: ping latency was worse than M0 at the same 90% success rate (avg
  5→23 ms, max 37→202 ms), both single samples. Take repeat and warm-path
  measurements; decide whether it is real before anyone treats it as a
  regression.
- `shellcheck` is installed on neither box, so `tests/lint.sh` honestly reports
  it SKIPPED. Installing it would raise the bar.
