# M1 implementation prompt (luna, xhigh)

Paste the block below into a new session. It is self-contained.

---

Implement **M1** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab`, using **luna at xhigh reasoning**.

## Read first, in this order

1. `docs/macos-handoff.md` — state of the world, environment, gotchas.
2. `docs/macos-m0-result.md` — the executed hardware test record. **Ground
   truth**; every number in it was measured, not predicted.
3. `docs/macos-arm64-plan.md` §M1 — the slice being implemented.

Where these disagree: M0 result > plan > anything else.

## Context you must not re-litigate

iolbox already runs on Apple Silicon with the **existing unmodified linux/amd64
payload** under Rosetta in an arm64 Lima VM. Two real Cisco IOL routers booted
and pinged (90%, 9/10). There is **no arm64 port on the critical path** — the
arm64 work on branch `luna/macos-arm64-invariant` is optional M7 and is not
part of this slice. Do not extend it.

## The one constraint that drives this slice

macOS 13.5's Rosetta aborts **every** amd64 executable on Linux kernels ≥ 6.3
with `rosetta error: unhandled auxillary vector type 28` (`AT_RSEQ_ALIGN`).
Ubuntu 22.04 / kernel 5.15 works; Ubuntu 24.04 / kernel 6.8 does not. The macOS
version that fixes this is **UNVERIFIED** — so the product must gate on a
runtime canary, never on a version comparison.

## Scope — build exactly this

**A deterministic Jammy provisioner** that takes a fresh Mac with Lima
installed to a working iolbox guest, with no manual steps:

- Creates the Lima machine with a **locked** template: Ubuntu 22.04, VZ,
  Rosetta, pinned image URL + digest, explicit cpus/memory/disk. Do not rely on
  `template://ubuntu-22.04` resolving the same way forever.
- Fixes multiarch correctly: existing sources pinned `[arch=arm64]`, amd64
  sources added for `archive.ubuntu.com` / `security.ubuntu.com`. amd64 is NOT
  served from `ports.ubuntu.com` (404 on every index).
- Installs the amd64 loader/glibc/OpenSSL set the payload needs, and asserts
  their architecture rather than assuming it.
- **Holds the kernel** on the 5.15 series across normal `apt upgrade`, and
  makes that policy explicit and inspectable.
- Installs the existing amd64 native tarball and verifies the service.

**The canary, as a hard gate:**

- Run `/lib64/ld-linux-x86-64.so.2 --version` through Rosetta **before install
  and on every start**.
- Pass = a glibc version string. Fail = anything else, including the auxv error.
- On failure: **fail closed** with a specific, actionable message naming the
  detected macOS build, Lima version, guest kernel and the actual error. Never
  continue into a broken install. Never infer support from a version number.

**A negative test — this is not optional.** Prove a 6.8-kernel guest is
*correctly rejected*, not silently broken. A gate that has only ever been seen
to pass is not a gate. Note the Mac's disk is tight (~5.4 GB free), so make the
negative test cheap — it does not need a full iolbox install, only enough guest
to run the canary. Delete the throwaway machine afterwards.

## Out of scope — do not touch

The Darwin launcher (M2), browser/sync/console/capture UX (M3), the runtime
matrix (M4), i386 capability gating (M5), release packaging (M6), and the
arm64/FEX path (M7). Do not modify `docs/macos-m0-result.md`,
`docs/macos-arm64-plan.md`, or `docs/macos-arm64-plan-review.md`.

## Validation

The Mac is reachable at `rohansharma@192.168.101.144` with key
`~/.ssh/iolbox_mac_m0`. Homebrew and Lima are at `/opt/homebrew/bin/` and are
**not on the non-login PATH** — always use absolute paths over SSH.

M1 is done when, from a clean state on that Mac, one command produces a guest
that passes the canary, runs the supervisor, serves `GET /` with status < 500
(**there is no `/api/health`**), and boots the two-node IOL lab; and when a
deliberately-6.8 guest is rejected with the actionable error.

Report honestly: what you executed on hardware versus what you only wrote.
State any acceptance criterion you could not verify and why. Do not report M1
complete on the strength of code that has not run on the Mac.

## Working rules

- Work on a branch off `main`. **Do not commit to `main`, do not merge.**
  Note: `luna/macos-arm64-invariant` holds uncommitted M7 work — do not disturb
  it; branch from `main` instead.
- If driving codex: every `codex exec` with the prompt as an argument **must**
  end `< /dev/null` or it hangs forever on stdin. The codex sandbox has a
  read-only `.git`, so commits happen from the main session, not the agent.
- Match the repo's existing style: stdlib Go, small explicit shell scripts.
- Check free disk on the Mac before creating any VM.
