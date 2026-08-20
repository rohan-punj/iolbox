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
  available. Stopped and reported rather than guessing.

## Session resumed: Mac reachable again

Reverified reachability directly (`ssh-keyscan` host-key match) before
resuming. HEAD was `322b307`.

### Item-5 HardWall fix reverified, plus three more real bugs found

Resyncing/rebuilding to reverify the HardWall fix surfaced a real,
previously-latent class of bug: **every automated `hardware-m4-phase5.sh`
background run died silently, without any error captured, immediately
after `item-5`'s hard-wall retry engaged** (and, separately, after `item-7`'s
forced-VM-stop, and at the very final checkpoint). Root-caused by manually
driving each exact sequence step-by-step against a real evidence directory:

- `sentinel_checkpoint after-ram-reclaim` (item-5's reclaim/retry path) and
  `sentinel_checkpoint after-forced-vm-stop` (item-7's forced-termination
  path) both dial the guest via `limactl shell`, but both were being called
  *immediately after* a `launcher_stop`/`forced-vm-stop` that had just
  stopped that same VM -- guaranteed failure the moment either path is
  actually exercised on real hardware ("FAIL: guest sentinel failed at
  after-ram-reclaim" / "...at after-forced-vm-stop"). No M4 run in this
  project's history appears to have exercised either branch on real
  hardware before this session.
- The terminal `launcher_stop final; sentinel_checkpoint final` had the same
  bug in reverse (no subsequent restart to fix it against), so was fixed by
  swapping to `sentinel_checkpoint final; launcher_stop final` instead.

All three fixed in `hardware-m4-phase5.sh` (not the frozen `hardware-m4.sh`)
in commits `6ac0af9`, `9043dbd`, `98c9f7b`. This also explains why every
automated run this session had appeared to "die mysteriously" right after
those transition points -- it wasn't a nohup/disown artifact, it was this.

### The WS control-connection framing error became frequent, not rare

Across many fresh-VM automated runs, items 1/2/3 each independently failed
on a real minority of attempts (roughly a third to a half) with
`wsclient.go`'s "non-FIN / fragmented frame not supported (opcode 8)"
reading a control response -- always resolved by rerunning the same phase.
Given how much wall-clock time a full from-scratch rerun cost every time
this hit, this crossed the bar from "document as a transient" to "apply a
bounded mitigation": `m4Runtime.request()` now reconnects the control WS
once and replays the same request on this specific error signature (commit
`49e32d0`). This measurably improved throughput for the rest of the
session; the underlying root cause (most likely the frozen v0.5.2-lineage
M1-M6 supervisor's WS write path racing its own host.stats/node.state
broadcast events under load) remains unfixed and out of Phase 5's scope.

### A real, deterministic item-5 fixture defect, found by the independent verifier

A full clean run (items 1-4 PASS, extnet NOT_EXERCISABLE, soak PASS,
item-5 attempt-1 PASS per its own internal check) was run through to
`TestM4VerifyRecord`, the independent verifier plan item 8 requires. It
caught something item-5's own pass/fail logic missed: **both raw
`pings.ndjson` rows for the required "R1<->R3 diagonal" check showed
`sent:100 received:0` (100% loss)**. Root-caused via the fixture's actual
routing table: `m4FourNodeTraffic`'s first diagonal ping targeted
`10.0.34.2` (R4's own address on the R3-R4 link, not R3's -- should be
`10.0.34.1`), and even corrected, neither diagonal direction was routable
end-to-end because `four-iol-ring.lab.json` was missing two transit static
routes (R2 to reach 10.0.34.0/30, R1 to reach 10.0.23.0/30 for the reply
path). Fixed in commit `2825507`: corrected the target address and added
the two missing routes. This **changes the frozen fixture's SHA-256** from
`f25d8e4b9a5a750c895696d6d7f609cabc03dcab83a040a921c6719a3df927ee`
(recorded in `docs/m7-evidence/phase0/m3-m4-inputs.md`) to
`614a97008db0d9a2f48f9e0b475eb06e1d67fd4d9ba165dea81dc4b5b37f3161` -- a
deliberate, fully-documented deviation, made because the original fixture
could not pass its own stated acceptance bar, not a scope change.

Reverified with the corrected fixture: **three independent standalone
item-5 runs (no preceding soak) all passed cleanly**, including the
corrected diagonal pings showing `100/100` and `100/100` received in both
directions (raw evidence:
`native-arm64/` sibling isn't this -- see `m4-full-clean-run` evidence
below for the rosetta-amd64 four-node run).

### Real finding: four-node capacity is genuinely resource-constrained in the plan's required post-soak position

With every harness/fixture bug now fixed, **two independent full
`hardware-m4-phase5.sh` runs** (items 1-4 PASS, soak PASS at the
owner-directed 1200s) both hit **item-5 hard-walling on both the initial
attempt and the cold retry** (`EOF` reading a console mid-boot, ~110s into
each attempt; all four IOL processes were still alive afterward per `ps`,
ruling out an OOM kill). This is the plan's own defined terminal state
("M4 BLOCKED/UNVERIFIED") when a hard wall survives the one permitted cold
retry -- and per the M4 plan, "no reduced topology or RAM is equivalent",
so this is not something to route around.

This is a genuine, reproducible (2/2) finding, not a flake and not a bug:
**the four-node/1024MB-per-node IOL capacity tier is not reliably
achievable on this specific Mac immediately after a sustained
traffic+capture soak**, even though the same topology and fixture are
proven functionally correct in isolation (3/3 standalone passes with no
preceding soak). The M4-era "reclaim RAM from other VMs" mitigation this
Mac's retry path is supposed to use has nothing to reclaim from (the
witness VMs it was designed around no longer exist on this Mac -- see the
earlier `hardware-m4-phase5.sh` design note), so a cold VM restart alone is
the only headroom recovery this retry can offer, and it was not enough
either time.

**Capacity (four-node) row verdict: BLOCKED/UNVERIFIED in the plan-required
post-soak position, on this specific Mac, as of this session.** The
underlying product capability (four real concurrent IOL nodes on a real
ring, forwarding real traffic, all four consoles reachable) is proven
correct; what's not proven is that this Mac has enough headroom to run it
reliably right after 20 minutes of sustained soak traffic. Rounding this up
to PASS would misrepresent a real hardware-capacity finding.

### Native-arm64-specific rows

Two rows the plan calls out specifically for the native-arm64 profile, not
covered by the rosetta-amd64 M3/M4 rerun above:

- **VPCS/IOL native traffic**: launched `--profile native-arm64` for real
  (canary passed, `qualification` line confirmed), registered the frozen L3
  image, loaded `vpcs-iol.lab.json`, and drove both consoles directly over
  the real console WebSocket protocol (raw binary frames, matching
  `wsclient.go`'s actual framing -- not a JSON envelope). Real bidirectional
  traffic: router-to-VPCS ping 9/10 (90%), VPCS-to-router ping showing real
  ICMP replies with real RTTs (`docs/m7-evidence/phase5/native-arm64/vpcs-iol-traffic.txt`).
  **PASS.**
- **Rosetta exclusion**: full mount/binfmt/process inventory captured
  before IOL execution, during IOL execution, and after a real launcher
  restart (`docs/m7-evidence/phase5/native-arm64/rosetta-inventory/{before,during,after}.txt`).
  All three show zero Rosetta mounts and zero Rosetta processes; the
  supervisor and vpcs processes run as plain native aarch64 binaries with no
  `/mnt/lima-rosetta/rosetta` prefix (compare directly against the
  rosetta-amd64 profile's own process list earlier in this document, which
  *does* show that prefix on every amd64 process -- confirming this is a
  meaningful, working comparison, not an untested assertion). The one
  x86_64 binary that must run (IOL itself, still upstream-only x86_64) is
  translated via the guest's own Linux `qemu-binfmt` (`x86_64-binfmt-P`),
  a completely different, in-guest-only mechanism from macOS-host Rosetta.
  **PASS.**
