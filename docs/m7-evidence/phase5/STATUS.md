# Phase 5 status — authoritative M3/M4 hardware matrix rerun

Continuing in `J:\Claude code\iolab-m7-phase4-wt`, branch
`luna/macos-m7-phase4-integration`, base commit `ce57cfb` (Phase 4 CLOSED).

## Setup performed this session

- Verified Mac reachability via `ssh-keyscan` host-key match (`192.168.101.186`).
- Built the combined launcher fresh from `ce57cfb` on the Mac itself
  (Go 1.26.6 via `/opt/homebrew/bin/go`, not previously on PATH): both
  `iolbox-launcher` and the `iolbox-launcher-hardware.test` binary, staged
  at `~/iolbox-p5-build/src` (git-archive of `ce57cfb`, extracted fresh).
- Rebuilt the native-arm64 payload via `runtime/pack-native.sh --arch arm64`
  using the Phase-4-era `supervisor-linux-arm64`/`vpcs` binaries already
  proven current (only `51ca386` touched supervisor source between the base
  commit and `ce57cfb`, and that commit predates when those binaries were
  built) plus freshly-cross-compiled toollaunch/packs. Output:
  `runtime/build/iolbox-server-p5-ce57cfb-linux-arm64.tar.gz` (79 MB).
- Found and fixed a **test-setup defect** (not a product bug): `git archive`
  does not preserve the executable bit reliably across this Windows→Mac
  transfer path, so `packaging/macos/tests/*.sh`, `packaging/macos/guest/*.sh`,
  and `runtime/*.sh` lost +x on the Mac. Fixed with `chmod +x` on the staged
  copy only (not committed — this is a transfer artifact, not a repo state).
- Found and neutralized **stale test state**: a `native-arm64` profile choice
  was persisted at `~/Library/Application Support/iolbox/profile-choice.env`
  from Phase 4's own scenario 6 (persisted-choice) test. The frozen M3/M4
  harnesses predate `--profile` entirely and assume the historical
  auto-defaults-to-Rosetta behavior, so M3/M4 runs now export
  `IOLBOX_PROFILE=rosetta-amd64` explicitly (highest-precedence explicit
  flag, per `macos_profile_select.go`) rather than resetting the owner's real
  persisted-choice file as a side effect of Phase 5 testing.
- Found and fixed a genuine **fixture-staleness issue**: the M3 harness's
  historically-frozen payload `iolbox-server-v0.5.2.tar.gz` predates Phase
  4's `DisableI386`-aware guest contract (`50-verify.sh`'s
  `verify_capability_hello`, added this phase). Reproduced independently by
  hand-querying the guest's real `hello` response over the control socket
  (`/proc/<pid>/environ` confirmed `IOLBOX_DISABLE_I386=1` was correctly set
  by the systemd drop-in; the v0.5.2 binary itself simply predates that
  policy and always advertises `i386`). Fix: use the same current payload
  Phase 4's own scenario 3 validated, `iolbox-server-m5-luna.tar.gz`
  (present on the Mac at `~/iolbox-m0/`), matching this task's brief to test
  "the real combined launcher artifact." No source change was needed or
  made for this one — swapping which tarball sits in `assets_dir` was
  sufficient and is the smaller, more faithful fix.
- One real source change, committed: `tools/iolab-launcher/macos_m4_runtime_darwin_test.go`
  — `m4SoakSeconds` changed from a hardcoded 600s constant to an
  `IOLBOX_M4_SOAK_SECONDS`-overridable var (default unchanged at 600s for
  every other caller). Reason: `docs/m7-evidence/phase0/m3-m4-inputs.md`
  records M4's own owner-approved reduced soak (600s, not the original two
  hours), but `docs/macos-m7-plan.md` section 11 (Phase 5) has its own
  stricter bar — "Continuous traffic runs two hours" — so Phase 5's traffic
  soak row cannot reuse M4's 600s harness unmodified. Checkpoint/verifier
  floors were re-audited: `raw.Duration < 600`, `TrafficRows < 10`, etc. are
  minimum floors, not exact-600 checks, so they already generalize correctly
  to a longer run.

## Row results so far

| Row | Status | Evidence |
|---|---|---|
| Browser lifecycle (M3 browser-equivalent) | **PASS** | `m3-rerun/m3-20260819T194911Z-28552/browser-equivalent.txt` |
| Host data/sync (default + restart) | **PASS** | same dir, `launcher-stop-default.txt`, `default-sync-files.txt` |
| Host data/sync (spaces/non-ASCII path) | **PASS** | same dir, `launcher-start-difficult-path.txt` ("M3 data café") |
| Consoles/forwarding (loopback-only, 2 concurrent) | **PASS** | same dir, `port-probe-host.txt`, `lsof-listeners.txt`, `gui-websocket.txt` |
| Capture (valid pcapng) | **PASS** | same dir, `browser-equivalent.txt` (896 bytes, 2 packets, sha256 recorded), `pcap-validator.txt` |
| M1 install-image-and-lab sub-phase (feeds M3) | **PASS** | `m3-20260819T194911Z-28552/m1-20260819T195052Z-28892/` — 90% ping (9/10) |

## M4 matrix run (hardware-m4-phase5.sh)

- **Attempt 1**: item-1 (VPCS/IOL) failed on a WS control-connection read
  error (`ws: non-FIN / fragmented frame not supported (opcode 8)`) reading
  the `lab.stop` response. Reproduced independently by (a) hand-driving the
  same `lab.stop` call via raw WS bytes against the still-running guest —
  no fragmentation observed, clean framing — and (b) rerunning item-1's
  exact Go test phase a second time against a fresh lab on the same VM,
  which **passed cleanly**. Conclusion: non-reproducing, one-off transient
  during the VM's very first cold-start control-plane burst, not a
  deterministic defect. No source change made for this (a speculative fix
  for an unconfirmed cause would itself be a discipline violation).
- **Attempt 2** (fresh VM): item-1 passed. **item-2 (multi-link) failed**,
  deterministically and for a real reason: `consoles[1]` (R2) never got
  `m4Enable()` called before its `ping 10.0.12.1 repeat 100` in the item-2
  ping-pair loop (`macos_m4_runtime_darwin_test.go` line ~848 area) — only
  `consoles[0]` (R1) was enabled. R2's console transcript
  (`item-2/console-1.txt`) shows it staying at the user-EXEC `R2>` prompt
  the whole time, and the extended `repeat` ping syntax requires privileged
  EXEC on real IOS, producing `% Invalid input detected at '^' marker`
  every time — deterministic, not a flake. **Fixed**: added the missing
  `m4Enable(consoles[1])` call, mirroring the existing `consoles[0]` call,
  with a comment explaining why (both consoles[0]/R1 and consoles[1]/R2 are
  IOL routers using the privileged `repeat` syntax; only consoles[2]/VPCS
  uses the `-c` form that works from user EXEC). `go vet`/`go build` clean
  for darwin/arm64. Rerun pending.

## Owner-directed deviation: traffic-soak duration

Per an explicit owner instruction received during this session (2026-08-19,
mid-run), the traffic-soak row's duration was reduced from the plan's
stated two hours (7200s) to **1200 seconds (20 minutes)**. This is a
deliberate, explicitly owner-approved deviation, not a silent rounding-up.
Consequence: resource-drift/degradation effects that only manifest under
multi-hour sustained load (slow memory growth, fd/socket leaks, capture
file growth pressure, thermal/power throttling) are **not exercised at the
plan's full stated duration** by this run. The soak row's PASS (if
achieved) certifies correctness and stability across 20 continuous minutes
of traffic + capture, not two hours.

Remaining rows (NAT, extnet, capacity, traffic soak, forced termination) run
next via the same `hardware-m4-phase5.sh` orchestrator, plus a separate
native-arm64-specific supplementary run for the two native-specific rows
(VPCS/IOL native traffic, Rosetta-exclusion inventory).

## M4 run continued: items 1-4 and soak all PASS; item-5 fixed; Mac went unreachable

- Full orchestrator rerun (post item-2/item-3 fixes): **item-1 PASS,
  item-2 PASS, item-3 PASS, item-4 NOT_EXERCISABLE** (real decision-table
  result: "no suitable Lima/extnet host interface in preserved probes" --
  this is the plan's explicitly-permitted non-waiver outcome, not a
  fabricated pass), **item-6 soak PASS** (1200s per the owner-directed
  deviation above; `SOAK-COMPLETE` seal verified, `TestM4VerifySoakSeal`
  PASS, 20/20 traffic rows, capture non-empty).
- **item-5 (four-node capacity) failed twice**, both times running
  immediately after the 20-minute soak, with a plain `EOF` reading a
  console inside the per-console ping loop (not at lab.start or
  openConsoles). Reproduced independently: a **standalone** item-5 run
  (no preceding soak) **passed** cleanly (428s), confirming the failure is
  specifically linked to running right after the soak, not a universal
  item-5 defect. Guest-side check during a failure: all 4 IOL processes
  were still alive (`ps` showed RSS ~550MB each, no OOM), so this is a
  resource-pressure timing race, not a crash. **Fixed**: item-5's
  `record.HardWall` is now set for ANY item-5 failure (previously only set
  at the two early gates, lab.start and openConsoles) so the existing
  reclaim-and-retry contract actually engages for this failure mode too.
  `go vet`/`go build` clean for darwin/arm64. Committed, not yet reverified
  on hardware.
- **Mac went unreachable mid-session** right after this fix, while
  resyncing/rebuilding for the next full rerun: `ssh` timed out and
  `ssh-keyscan` got no response at all across 8 retries spanning ~3
  minutes. This matches the documented "Mac has gone to sleep before"
  pattern in the Phase 4 handoff; there is no remote-wake mechanism
  available. **Stopping here and reporting rather than guessing** --
  remaining M4 items (item-5 rerun with the fix, item-7 forced termination,
  final record/verify), all native-arm64-specific rows (VPCS/IOL native
  traffic, Rosetta exclusion inventory), and re-verification of the M3 rows
  against the still-current source are all still pending real-hardware
  execution once the Mac is reachable again.
