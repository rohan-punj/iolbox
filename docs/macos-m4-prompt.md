# M4 implementation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Implement **M4** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab`, using **luna at xhigh reasoning** for implementation
and **sol at medium** for planning/review, the same pattern used for M2/M3.

## Read first, in this order

1. `docs/macos-m3-handoff.md` — **start here.** Current state, defects found
   and fixed, the shared-worktree/branch gotcha, and open items going into
   M4.
2. `docs/macos-m3-result.md` — the executed M3 acceptance record.
3. `docs/macos-m1-handoff.md` — earlier gotchas still in force (liveness vs
   readiness, console ring-buffer replay, `ram: 256` wedging IOL, etc.).
4. `docs/macos-arm64-plan.md` §M4 (search for "M4 — qualify the remaining
   runtime behaviors and capacity"). **Immutable.**

Where these disagree, precedence is: M3 handoff > M3 result > M1 handoff >
plan.

## Context you must not re-litigate

M1, M2, and M3 are **complete and proven on hardware**, on branch
`luna/macos-m3-ux` (off `luna/macos-m1-provisioner`, off `main` — neither
parent is merged; branch from `luna/macos-m3-ux`). A Go Darwin launcher
drives the whole lifecycle; the GUI is reachable through an explicit
loopback-only Lima port-forward contract; host folder sync round-trips
correctly; a browser-equivalent HTTP/WS flow proves upload, lab
save/list/load/start, dual consoles, and live capture all work end to end.

**Important state change since M3 was scoped**: a separate concurrent
session already fixed VPCS/PC node support on this Mac (commit `9916fb9`,
already on this branch) — `ioltool` account provisioning, the native
cap-transition launcher, and tool packs. **That commit's own message says
full end-to-end PC-node-reaches-running confirmation is still owed.** M4's
first real acceptance criterion needs VPCS, so **run that end-to-end
confirmation as your first hardware step** before building the rest of M4's
matrix on top of it — don't assume the commit alone is a hardware PASS.

Do not redesign the profile model, the port contract, the sync engine, or
the browser-equivalent test pattern. Extend them only where a genuine defect
demands it, per the M2/M3 precedent (found 6-7 real bugs each round, every
one only visible by actually running on hardware).

## Scope — build exactly this

Per `docs/macos-arm64-plan.md` §M4: **qualify the remaining runtime
behaviors and capacity before release.** This is primarily a test/record
phase, not a feature build — "product/runtime files only for failures
actually observed."

Run and record, on Apple Silicon:
1. **VPCS connected to IOL with bidirectional ping** — confirm the
   `9916fb9` fix actually works end to end first (see above).
2. **A multi-link topology** (more than the two-node p2p case M1/M2/M3 used).
3. **NAT-node outbound connectivity and teardown.**
4. **extnet attach/traffic/cleanup**, where Lima exposes a suitable
   interface — if it doesn't, record that honestly as not exercisable on
   this host rather than skipping silently.
5. **Four IOL nodes** on supported hardware.
6. **A two-hour sustained traffic soak.** Capture must remain valid
   throughout the multi-link and soak runs.
7. After a **forced** launcher and VM termination, restart must leave **no
   stale taps/processes**.
8. Record: boot time, packet loss, load, per-node RSS, guest memory, and
   host memory pressure.

### Hard requirements carried from M1/M2/M3 (unchanged, do not re-litigate)

1. Never re-encode `profiles.env` in Go; read it as data.
2. Never compare macOS versions numerically; qualification is exact
   `(profile, product, build)` string equality.
3. NDJSON control-plane rules: string `id`, correlate by id, `ok:true`
   means understood not succeeded, YAML vs JSON asymmetry between
   `lab.saveDoc`/`lab.load`.
4. Readiness is `GET /` < 500, not liveness (`active`/`running` is not
   proof — see M1 handoff gotcha 14 and M3's own console/capture readiness
   races, which needed active retries, not passive waits).
5. The GUI bridge requires a session cookie + same-origin `Origin` on every
   WS route (`/control`, `/console/{id}`, `/capture/{id}`) — reuse
   `wsDialWithSession` from `tools/iolab-launcher/wsclient.go`, do not
   re-invent raw dials.
6. IOS needs an active console wake (`\r\n` on connect, periodic re-poke) —
   reuse the pattern in `tools/iolab-launcher/macos_browser_e2e_darwin_test.go`'s
   `m3ReadPrompt`/`m3SendConcurrently`, don't rebuild it.
7. `ram: 256` wedges modern IOL images without crashing — use 1024 MB (or
   higher, for four-node RAM-pressure testing) in any new lab fixture.
8. Stdlib-only Go, `gofmt`'d, `stop` never deletes guest or host data.

## Out of scope — do not touch

M5 (i386 capability gating), M6 (release packaging/CI), M7 (arm64/FEX). Do
not modify `docs/macos-m0-result.md`, `docs/macos-m1-result.md`,
`docs/macos-m2-result.md`, `docs/macos-m3-result.md`,
`docs/macos-arm64-plan.md`, `docs/macos-arm64-plan-review.md`. Do not
redesign M1's guest provisioning, M2's Lima lifecycle/CLI, or M3's port/sync
contract except for defects M4 demonstrates against them.

## Validation

The Mac is reachable at `rohansharma@192.168.101.166` with key
`~/.ssh/iolbox_mac_m0`. Homebrew/Lima live at `/opt/homebrew/bin/` and are
**not on the non-login PATH**. Bash on the Mac is **3.2.57 only**.

`iol22` is the **irreplaceable M0 evidence machine, never touch it**.
`m1jammy`, `m1trixie`, `iolbox-m1-e2e`, `iolbox-m2-e2e`, `iolbox-m3-e2e` are
disposable and may be reused or deleted. Check actual free RAM with
`top -l 1 -s 0 | grep PhysMem` before creating a new VM — raw `vm_stat` free
pages under-report what's actually reclaimable on this 8 GB Mac (see M3
handoff §5). **Four-node qualification may need more than 8 GB** — the plan
flags this as an unmade owner decision; if you hit a hard wall, say so
explicitly rather than silently reducing scope, and ask.

**Check for concurrent activity before starting**: this exact worktree/branch
had a separate session committing directly to it during M3 (§4 of the M3
handoff). Run `git log --oneline -5` and `git status` first; never
`git add -A`/`git add .` — stage your own exact file list by path.

The two-hour soak is long — budget for it explicitly rather than
discovering partway through that the session will run out of time. Consider
starting it early and doing other M4 items while it runs in the background,
then returning to check the results.

M4 is done when every criterion above has been executed on the real Mac and
recorded with real numbers (not asserted from code review). Report exactly
what ran on hardware versus what only compiled or was unit-tested, and any
criterion you could not verify and why — per the same honesty bar M1/M2/M3
were held to.

## Working rules

- Work on a branch off `luna/macos-m3-ux` (e.g. `luna/macos-m4-runtime`) in
  a fresh worktree, unless there's a good reason to continue directly on
  `luna/macos-m3-ux` — ask if unsure, given the concurrent-activity history.
- If driving codex: pass the prompt as `- < prompt.md` on stdin — never bare,
  it hangs forever without `< /dev/null`. Give **sol** a `workspace-write`
  sandbox for any pass whose deliverable is a file (a `read-only` sandbox
  lost an entire M3 plan once when the final write was rejected).
- The codex sandbox's `.git` is read-only; commits happen from the main
  session, staged by explicit file list.
- Do not use `sed` on lines with regex escapes.
- Match the repo's existing style: stdlib Go, small explicit shell scripts.
