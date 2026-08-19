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

All remaining rows (VPCS/IOL, multi-link, NAT, extnet, capacity, traffic
soak, forced termination, Rosetta exclusion) are next via `hardware-m4.sh`
plus a native-arm64-specific supplementary run for the two native-specific
rows. Not yet started as of this checkpoint.
