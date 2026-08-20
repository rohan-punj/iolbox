# Build and run a real native-arm64 package for owner validation (this session)

Self-contained. Phase 6 (`docs/macos-m7-phase6-handoff.md`) found native-arm64
cleanly PASSes every gate it was able to reach (VM boot parity, lab boot,
real bidirectional traffic, teardown, no crashes, no Rosetta dependency);
the owner has since ruled to promote native-arm64 (see the "Owner promotion
ruling" section appended to that handoff doc). Working tree is clean at
HEAD `2b13791` on `luna/macos-m7-phase4-integration` in
`J:\Claude code\iolab-m7-phase4-wt`.

**This session's job is narrow and different from every prior Phase 4/5/6
session: build a real native-arm64 package and get it running and reachable
so the OWNER can personally validate it by hand. Do NOT run the Phase 6
measurement harness, do NOT run the M3/M4 hardware matrix, do NOT attempt
automated end-to-end verification of your own. Your job ends once the
package is built, running, reachable, and you've reported exactly how to
reach it — the owner does the actual validation.**

## Read first

1. `docs/macos-m7-phase6-handoff.md`, specifically the "Two exact artifacts
   under test" section's "Native candidate" subsection (lines ~64-94) — it
   documents the exact build recipe already used and verified once this
   session, reuse it verbatim rather than improvising a new one.
2. The "Owner promotion ruling" section (appended after this handoff was
   originally written) for the full context of why this build/run session
   exists now.

## Build recipe (reuse Phase 6's own, verified-working recipe)

On the physical Mac, from a fresh checkout/archive of this worktree's
current HEAD (`2b13791` — confirm via `git rev-parse HEAD` before
archiving, in case anything landed after this prompt was written):

1. `git archive` (or equivalent) this worktree's HEAD to a clean build
   directory on the Mac — do not build from a dirty/uncommitted tree.
2. Build the real GUI: `npm run build:embed` (wherever the repo's GUI
   package lives) — verify the output is the real bundle, not a
   placeholder (Phase 6 hit this exact gotcha before; check bundle size/
   contents, not just exit code).
3. Build `iolbox-launcher` for darwin/arm64 (Go, from
   `tools/iolab-launcher`).
4. Build `supervisor-linux-arm64` (Go, from `supervisor/`), embedding the
   real GUI from step 2 — a plain `go build` without the embedded GUI
   ships a placeholder; use the repo's real `build-release.sh` path, not
   a bare `go build`.
5. `runtime/pack-native.sh --arch arm64` → produces
   `runtime/build/iolbox-server-<version>-linux-arm64.tar.gz`.
6. VPCS-arm64 binary: check `git diff --stat` on `runtime/fetch-vpcs.sh`
   between this HEAD and Phase 5/6's base commits — if it's unchanged
   (Phase 6 found zero diff), reusing Phase 6's already-built VPCS-arm64
   binary is justified and documented precedent; otherwise rebuild fresh
   via `runtime/fetch-vpcs.sh --arch arm64`.
7. Record every artifact's sha256 as you go (matches this project's
   established evidence discipline) — you don't need a full write-up doc
   this session, but keep the hashes somewhere retrievable in case the
   owner asks what exactly they're looking at.

## Run it — leave it running and reachable, don't tear down

1. Start the launcher forced to native-arm64 explicitly (not `auto` — the
   `auto`-default code gate has NOT been flipped yet, see the handoff's
   scope note; forcing `-profile native-arm64` is the correct way to run
   the promoted candidate directly): `iolbox-launcher start -profile
   native-arm64` (omit `-no-browser` if you want it to also open on the
   Mac's own display; harmless either way since the owner will reach it
   via port-forward from their own machine).
2. Confirm `GET http://127.0.0.1:4001/` returns 200 on the Mac itself.
3. **Do not load a lab, do not run a scripted ping test, do not drive the
   console.** The owner wants to experience the real GUI/flow themselves,
   not see a pre-scripted result. Leave the launcher idle and ready at
   the login/dashboard screen.
4. **Do not stop the VM or the launcher process at the end of this
   session** — the opposite of every prior Phase 4/5/6 session's cleanup
   step. Leave it running so the owner can reach it whenever they're
   ready, for as long as they need.
5. Report back exactly how the owner (on their own Windows machine, not
   the Mac) can reach it — the practical path is an SSH local port
   forward:
   `ssh -i "J:\Claude code\iolab-m7-wt\.m7-ssh\iolbox_mac_m0" -L 4001:127.0.0.1:4001 rohansharma@192.168.101.186`
   then open `http://127.0.0.1:4001/` in a browser on the Windows side.
   Confirm this exact command works (test it yourself once, then close
   your test connection — leave the actual launcher process running on
   the Mac, only the port-forward is ephemeral/per-session) before
   reporting it as ready.

## Hardware access (unchanged from Phase 4/5/6)

- Physical Mac `rohansharma@192.168.101.186` — verify via `ssh-keyscan`
  against known host key
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
  before trusting it.
- Key `.m7-ssh/iolbox_mac_m0` lives in the Phase 3 reference worktree
  `J:\Claude code\iolab-m7-wt`, not this one.
- `limactl` at `/opt/homebrew/bin/limactl`; `stop`/`delete` need
  `--tty=false` and `< /dev/null` — not that you should need either this
  session, since you're not tearing anything down.
- Protected VMs, never touch: `iolbox-m5-e2e`, `iolbox-m7-native-arm64-qemu`.
- Use an isolated `LIMA_HOME` for this build (not the default `~/.lima`,
  not either of Phase 6's `~/.lima-iolbox-p6-*` dirs — pick a clearly
  named new one, e.g. `~/.lima-iolbox-owner-validate`, so it's obvious
  this is the owner-facing instance and nothing else touches it
  accidentally).
- The Mac is the owner's actively-used laptop and this VM is meant to
  stay running for them — be mindful of resource use (this is a single
  2-4 GiB VM, should be fine) but don't add anything else heavy alongside
  it this session.

## Working pattern (unchanged)

- Actively poll/block on anything long-running yourself — never end a
  turn assuming a passive notification will arrive.
- If you hit a real build defect (not just "it's slow"), fix the smallest
  owning thing and note it, but this session isn't the place for another
  deep investigation — if something looks like it needs real
  root-causing beyond an obvious one-line fix, stop and report rather
  than going deep, since the owner is waiting to validate, not waiting
  for another multi-hour investigation.
- Commit nothing unless you actually changed repo code to fix a real
  build defect — a normal build+run session shouldn't need a commit at
  all.
