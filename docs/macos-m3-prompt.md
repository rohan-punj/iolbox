# M3 implementation prompt (this session)

Self-contained brief for M3 of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab`, using **luna at xhigh reasoning** for implementation
and **sol at medium** for planning/review.

---

## Read first, in this order

1. `docs/macos-m2-result.md` — the executed M2 acceptance record. **Start here.**
2. `docs/macos-m1-handoff.md` — platform facts, gotchas, open items (D11/Q1).
3. `docs/macos-m1-result.md`, `docs/macos-m0-result.md` — earlier acceptance
   records. **Immutable.**
4. `docs/macos-arm64-plan.md` §M3 (lines ~161-171). **Immutable.**

Where these disagree, precedence is: M2 result > M1 handoff > M1 result >
M0 result > plan.

## Context you must not re-litigate

M1 and M2 are **complete and proven on hardware** (merged into
`luna/macos-m1-provisioner` at `3132b6b`). A Go Darwin launcher
(`tools/iolab-launcher/`) already does `start`/`stop`/`status`/`diagnose`/
`upgrade`, reads `packaging/macos/lima/profiles.env` as data, qualifies by
exact `(profile, product, build)` string match, and polls `GET /` for
readiness. Do not redesign any of that — extend it.

Measured on the reference Mac (macOS 26.6.1, Apple Silicon): two-node IOL lab
90% (9/10), `IOLBOX_HOST_LIMA` correctly reports the Lima version (D11 fixed).

**Gap this phase closes**: the current Lima templates
(`packaging/macos/lima/iolbox-*.yaml`) have **no explicit `portForward`
stanza**. M2's `GET /` readiness poll worked without one — that is almost
certainly Lima's automatic port-forwarding of ports it detects listening in
the guest, not a deliberate host-loopback contract. Nothing in M1/M2 has
verified that the **console range (9000-9049)** and **capture range
(5500-5529)** actually reach the Mac host, that they're loopback-only (not
`0.0.0.0`), or that two consoles can be open concurrently. `tools/iolab-launcher/ports.go`
is the **Windows/qemu** port model (`-netdev user,hostfwd=...`) — it does not
apply to Lima, which forwards ports via its own YAML config or vmnet, so this
phase needs a **Darwin-side equivalent**, not a reuse of `ports.go`.

## Scope — build exactly this

Reach Windows-launcher UX parity from the **Mac host**, not merely from
inside the guest, per `docs/macos-arm64-plan.md` §M3:

1. **Real browser driven end-to-end**: Safari or Chrome actually does image
   upload, image registration, lab creation, start, console input, stop,
   reload — from the Mac, through the launcher's `open browser` step and the
   forwarded GUI port.
2. **Host-side folder sync** (`foldersync.go` parity): host files under
   `~/Library/Application Support/iolbox/images` and `~/Library/Application
   Support/iolbox/labs` round-trip across a launcher restart. Confirm the
   existing `foldersync.go`/`imagecache.go` are OS-path-agnostic (they appear
   to use `os.UserConfigDir()`/similar, not a hardcoded Windows path — verify
   this rather than assuming it) and wire them into the Darwin lifecycle if
   they are not already.
3. **Host port forwarding for consoles and capture**: explicit, verified Lima
   port forwarding (loopback-only, `127.0.0.1`) for the GUI port, the full
   console range, and the full capture range. Two simultaneous browser
   consoles must work concurrently from the Mac.
4. **Live packet capture**: a capture session yields a valid, non-empty
   `.pcapng` reachable from the host — either Wireshark (if installed) opens
   it live, or the browser offers a working save path. Verify against
   whatever the Windows launcher's `wsclient.go`/capture plumbing already
   does, and give it a Darwin equivalent rather than re-deriving the protocol.
5. **Path robustness**: spaces and non-ASCII characters in the macOS
   username/data paths must not break upload, sync, or capture-file paths.

### Hard requirements carried from M1/M2 (unchanged, do not re-litigate)

1. Never re-encode `profiles.env` in Go; read it as data.
2. Never compare macOS versions numerically; qualification is exact
   `(profile, product, build)` string equality; unmeasured = informational,
   not a refusal.
3. NDJSON control-plane rules: string `id`, correlate replies by id (never
   read-one-line — the server pushes unsolicited event frames), `ok:true`
   means understood not succeeded (inspect `result.warnings`/`result.failed`),
   `lab.saveDoc`/`lab.listDocs` use raw YAML strings, `lab.load` takes a JSON
   object.
4. Readiness is `GET /` with status < 500 — there is no `/api/health`.
   Liveness (`active`, node `running`) is not readiness.
5. Surface Lima's stale-Rosetta-bottle warning in `diagnose` (already done in
   M2 — do not remove it).
6. Stdlib-only Go, `gofmt`'d. `stop` never deletes guest or host data — this
   now also covers host-side synced folders: **do not delete
   `~/Library/Application Support/iolbox/{images,labs}` from any code path**.

## Out of scope — do not touch

M4 (runtime matrix: multi-link, NAT, extnet, four-node, soak), M5 (i386
capability gating), M6 (release packaging/CI), M7 (arm64/FEX). Do not modify
`docs/macos-m0-result.md`, `docs/macos-m1-result.md`,
`docs/macos-m2-result.md`, `docs/macos-arm64-plan.md`,
`docs/macos-arm64-plan-review.md`. Do not change M1's guest provisioning
steps or M2's Lima lifecycle/CLI contract except for defects M3 demonstrates
against them.

## Validation

The Mac is reachable at `rohansharma@192.168.101.166` with key
`~/.ssh/iolbox_mac_m0`. Homebrew/Lima live at `/opt/homebrew/bin/` and are
**not on the non-login PATH** — use absolute paths over SSH. The Mac has
**bash 3.2.57 only** — any shell script touched must be 3.2-clean.

Existing machines on the Mac — `iol22` is the **irreplaceable M0 evidence
machine, never touch it**. `m1jammy`, `m1trixie`, `iolbox-m1-e2e`,
`iolbox-m2-e2e` are disposable and may be reused or deleted. Check free RAM
(`vm_stat`) before creating a new VM — this Mac has 8 GB and has run out of
headroom before; ask before stopping VMs that might be in active use.

Reuse `packaging/macos/tests/hardware-m1.sh` where it already covers a
criterion (lab boot/traffic); extend it or add a sibling script for the new
browser/console/capture/sync criteria rather than duplicating its console
protocol handling. Driving an actual Safari/Chrome session from this
environment means either: scripting the Mac's own browser via
`osascript`/AppleEvents over the same SSH session, or documenting precisely
why that's infeasible and using the lowest-level equivalent (raw HTTP +
WebSocket calls to the exact endpoints the browser would hit) — **state
which one you did and why**, do not silently substitute one for the other
without saying so.

M3 is done when, from the Mac: a browser-equivalent flow uploads an image,
registers it, creates a lab, starts it, sends console input to two nodes
concurrently, captures live traffic to a valid non-empty `.pcapng`, stops,
and reloads — and host-side images/labs survive a launcher restart — and a
username or data path containing a space and a non-ASCII character does not
break any of it. Report honestly what ran on hardware versus what only
compiled or was unit-tested. **Do not report M3 complete on the strength of
code that has not run on the Mac.**

## Working rules

- Work on branch `luna/macos-m3-ux` in worktree `J:\Claude code\iolab-m3-wt`,
  branched off `luna/macos-m1-provisioner` at `3132b6b`. **Do not commit to
  `main`, do not merge without asking.**
- If driving codex: pass the prompt as `- < prompt.md` on stdin — as an
  argument it hangs forever unless the command ends `< /dev/null`. The codex
  sandbox has a read-only `.git`; commits happen from the main session.
- Do not use `sed` on lines containing regex escapes — it interprets
  `\r`/`\n` and corrupts the file.
- Match the repo's existing style: stdlib Go, small explicit shell scripts.
- Check free disk on both boxes before creating VMs.

## Known open items inherited from M1/M2 (not required for M3, note only)

- **Q1**: ping latency variance (avg 5→23 ms, max 37→202 ms) never got
  repeat/warm-path measurements. Not M3's job unless it interferes with
  capture/console validation.
- The Rosetta stale-bottle warning path is unit-tested only, never fired live
  on real hardware. Not required for M3.
