Read `J:\Claude code\iolab-m5-wt\docs\macos-m5-prompt.md` in full — it is the
self-contained implementation prompt for M5 of the Apple Silicon macOS track
for iolbox. Follow its "Read first" list in order (`docs/macos-m4-handoff.md`,
`docs/macos-m4-result.md`, `docs/macos-m3-handoff.md`,
`docs/macos-m1-handoff.md`, `docs/macos-arm64-plan.md` §M5) before writing
anything.

You are running as the PLANNING pass (sol, medium reasoning). **Do not
implement or edit any product/runtime/test code.** Your only output is a
markdown plan file at `J:\Claude code\iolab-m5-wt\docs\macos-m5-plan.md`.

## Already done for you — do not redo, do not re-litigate

**The M5 prompt's "required first step" (reconcile `luna/macos-arm64-invariant`)
is COMPLETE.** The owner's session investigated the uncommitted work in the
main `J:\Claude code\iolab` checkout and committed it as
`luna/macos-arm64-invariant@1fc99d8` ("M7 groundwork: parameterize the runtime
build by target architecture"). Findings you can rely on:

- It is plan §M7 work (arm64/FEX, off the v1 critical path), not M5 work.
- Files it touches: `build-release.sh` (adds a linux/arm64 cross-compile),
  `runtime/build-rootfs.sh` (`--arch amd64|arm64`, arm64 forces
  `INCLUDE_I386=0`), `runtime/fetch-vpcs.sh` (`--arch`, `VPCS_CC`),
  `runtime/README.md`, `docs/translation-rehearsal.md`,
  `tools/translation-rehearsal/{rehearse.sh,rehearse.py}`, and comment-only
  edits in seven `*_linux.go` files.
- **It touches ZERO files in M5's scope.** In particular it does not touch
  `supervisor/internal/server/handlers.go`, the launcher, or the
  `packaging/macos/` tree. It is NOT merged into the M5 line and must not be.
- The one adjacency worth knowing: `runtime/build-rootfs.sh`'s `INCLUDE_I386`
  flag already exists and already has a `--no-i386` mode. If your M5 design
  wants to touch that file, say so explicitly and explain the interaction —
  but note the Mac target runs the **amd64** rootfs under Rosetta, so the
  arm64 profile is not the Mac's profile.

Your workspace is the worktree `J:\Claude code\iolab-m5-wt` on branch
`luna/macos-m5-honest-caps`, freshly branched from `luna/macos-m4-runtime`
(HEAD `0f6f5d5`). Do not switch branches, do not commit, do not stage.

## Ground truth already gathered — verify, then build on it

I surveyed the code before launching you. Confirm each of these by reading the
files yourself; correct me in the plan if I am wrong anywhere.

1. `supervisor/internal/server/handlers.go:46` — `handleHello` builds
   `features := []string{"nvram", "capture", "i386"}` unconditionally, then
   appends `s.caps.GateFeatures()` (extnet) and `s.toolCaps.GateFeatures()`
   (tools). So `i386` is a hardcoded base feature with no gate at all today.
2. `supervisor/internal/server/server.go:26-60,129-134` — `Config` carries
   `Runtime` and `Arch`, defaulted in `New` to `"debian-slim-12"` /
   `"x86_64"`. `supervisor/cmd/supervisor/main.go:62-72` never sets either,
   so every deployment gets the defaults. There is currently **no** flag or
   env var that could carry a Mac-specific signal into the supervisor.
   Whatever mechanism M5 adds has to be plumbed from provisioning through
   `main.go` into `Config`.
3. `supervisor/internal/image/image.go` — `Arch` is `i386` | `x86_64` |
   `unknown`, parsed from the ELF header (`ParseArch`). This is the *image's*
   architecture and is a different axis from hello's `arch` field, which is the
   *runtime's*. The plan's "do not alter the existing `arch` field's meaning"
   applies to hello's field; be explicit about which of the two you mean
   everywhere in the plan.
4. GUI: `app/src/lib/labStore.svelte.ts:137,469` stores `hello.features`;
   `app/src/lib/nodeCatalog.ts:54` is the only consumer today and only reads
   `"natgw"`. **Nothing in the GUI currently reads `"i386"` at all.** So
   "the GUI never offers i386 IOL images as supported on an Apple Silicon
   target" requires new GUI code, not just a backend change. Find where images
   are surfaced to the user (image list, node-add / image picker) and say
   exactly which components must change.
5. `docs/protocol.md:77-80` documents the hello result including `i386` in the
   features array. It must be updated to describe the new conditional.
6. Launcher: `tools/iolab-launcher/macos_cli.go`. `runDiagnose` (line ~269)
   **already prints a hardcoded literal** `execution=rosetta-amd64` and
   `guest_arch=aarch64` at line 276, and separately prints *measured* `guest
   kernel:` / `guest arch:` (`uname -r` / `uname -m`) and a `live canary:
   PASS/FAIL` at lines 308-326. `runStatus` (line ~238) prints measured guest
   kernel/arch but **not** the execution/guest_arch pair. Treat the existing
   hardcoded literal as a defect to be fixed, not as the acceptance criterion
   already being met — M5's bar is that these are *reported honestly from
   measurement*, and a hardcoded string that would keep printing
   `guest_arch=aarch64` on a non-Mac or on a mis-provisioned guest is exactly
   the dishonesty this milestone exists to remove.
7. M1's guest marker: `packaging/macos/guest/40-install-payload.sh`
   `write_structural_attestation()` writes
   `/var/lib/iolbox/macos-structural-gate.json` with fields
   `{schema, profile, macos_product, macos_build, lima_version, drop_in,
   canary_verdict, kernel, timestamp}`, mirrored host-side to
   `$HOME/.iolbox/macos/<machine>-structural-gate.json` and parsed by
   `tools/iolab-launcher/macos_lifecycle.go:105`. The systemd drop-in
   `/etc/systemd/system/iolbox-supervisor.service.d/10-iolbox-macos-canary.conf`
   runs `30-canary.sh --quiet` as `ExecStartPre` on every start. This is the
   "guest marker/configuration" the plan says M1 defines and M5 builds on.

## What the plan must cover

Write `docs/macos-m5-plan.md`. It must be specific enough that an
implementer can execute it without re-deriving design decisions. Cover:

### A. The mechanism — how does the supervisor learn it is translated?

Compare at least three candidate designs and pick one, with reasons:
 (a) a supervisor CLI flag (e.g. `-disable-i386` / `-exec-environment`) set by
     the macOS systemd drop-in;
 (b) an environment variable in the systemd unit;
 (c) runtime self-detection inside the supervisor (e.g. probing binfmt
     `/proc/sys/fs/binfmt_misc/rosetta`, or actually attempting to exec a
     32-bit ELF).

Judge them against, at minimum: does it work for a target that is translated
but *not* Lima/Rosetta; does it fail **closed** or **open** if the signal is
missing; can it be wrong on a non-Mac target (blast radius — recall M4 found a
shared-code bug in exactly this class); is it testable offline; does it
survive an in-place payload upgrade that rewrites the base systemd unit (M1
proved the drop-in does survive that — check whether your mechanism does).
State explicitly whether the absence of the signal means "advertise i386"
(fail-open, preserves every existing non-Mac target byte-for-byte) or "do not
advertise" (fail-closed) — and defend the choice. Note that fail-open is what
protects the "ordinary amd64 targets still advertise i386" criterion, but it
also means a Mac that loses its configuration silently starts lying again;
propose how to detect that case.

Also decide: is this a single boolean ("i386 disabled") or a richer execution
descriptor (`execution=rosetta-amd64`, `guest_arch=aarch64`) that the
i386 decision is derived from? The acceptance criteria want both the negative
capability and the positive diagnostics, so say whether they share one
mechanism or are two independent ones, and why.

### B. Protocol surface

Exactly what changes in the hello result. Remember: **do not alter the
existing `arch` field's meaning**, and additive-only. If you propose new hello
fields, give their exact JSON names and types and say what an *old* GUI does
when it sees them (forward compatibility) and what a *new* GUI does when it
talks to an *old* supervisor that omits them (backward compatibility). Say
whether a bare feature-array omission is sufficient by itself for the GUI
criterion, or whether the GUI needs a positive signal too (it currently has no
way to distinguish "supervisor too old to report" from "i386 genuinely
unsupported" — address this).

### C. GUI change

Name the exact files/components and the exact user-visible behavior. "Never
offers i386 IOL images as supported" needs a decision: are i386 images hidden,
shown-but-disabled with a reason, or shown with a warning? Recommend one and
justify it from the user's point of view (an owner who has legitimately staged
an `i86bi_*` image and wonders where it went is a real failure mode). Include
what happens to a lab document that *already* references an i386 image.

### D. Launcher diagnostics

Exactly what `iolbox status` and `iolbox diagnose` must print after M5, and
where each value is *measured* from. Kill the hardcoded literal at
`macos_cli.go:276`. Say what each field prints when the machine is not
running, when the guest is unreachable, and when the canary has never run.

### E. Native service / provisioning configuration

Which file in `packaging/macos/guest/` (or a new one) carries the signal, how
it is installed, and how it interacts with the existing canary drop-in. If you
add a second drop-in, justify it over extending the existing one. State
explicitly what happens on an in-place upgrade.

### F. Blast-radius analysis — mandatory, be exhaustive

Enumerate **every** deployment target that shares this code path (the prompt
names LXC, VMware, native/cloud Linux, WSL, OVA — verify that list against
`runtime/` and `docker/` yourself and correct it). For each, state precisely
what changes and what does not. Then specify a concrete, runnable check that
proves a non-Mac target still advertises `i386` — ideally one that can run on
the Windows dev host or in `go test`, plus one that runs on a real non-Mac
target if one is reachable.

### G. Test plan, split honestly into two lists

- **Offline / unit tests**: exact test names and files, including a test that
  pins the default (no signal ⇒ `i386` present) and one that pins the Mac case.
- **Real-hardware verification**: the exact command sequence to run against
  the Mac at `rohansharma@192.168.101.166` (key `.m5-ssh/iolbox_mac_m0` in the
  worktree, gitignored) that demonstrates **each** of the five acceptance
  criteria, and the exact evidence artifact each produces. Be concrete about
  which Lima machine to use and which tarball to install — see the M4
  handoff's §2 tarball-overwrite trap, which is a live hazard for any harness
  you specify here. Note that M5 needs a **rebuilt supervisor payload** (the
  code change is in the supervisor), so the plan must say how a new payload
  tarball gets built and delivered to the Mac; the M1 handoff's open items note
  the Linux builder VM was deleted, so identify how the M5 build actually
  happens and flag it as a risk if unresolved.

The one criterion that cannot be shown by SSH alone is "the GUI never offers
i386" — specify how that is proven (the M3 handoff describes a
browser-equivalent HTTP/WS flow; say whether that is sufficient or whether a
real browser check is needed, and if so, how).

### H. Risks and open questions for the owner

Anything you cannot resolve from the repo. Be explicit rather than guessing.

## Rules

- Plan only. No code edits. No git operations.
- Read the actual files; do not trust my summary above without checking.
- Where the M4 handoff/result and the plan disagree, precedence is: M4
  handoff > M4 result > M3 handoff > M1 handoff > `macos-arm64-plan.md`.
- M4 is **PARTIAL**, not PASS. Do not describe it as complete anywhere.
- Prefer the smallest change that satisfies all five acceptance criteria. This
  milestone is estimated at 0.5-1 day; a design that requires rearchitecting
  the capability system is almost certainly wrong.
- Call out anywhere the acceptance criteria are ambiguous rather than silently
  choosing an interpretation.
