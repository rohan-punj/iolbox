# Phase 7 mature-tooling re-verification (item-1 rosetta router console, item-5 four-node both arms)

Written 2026-08-20 during the M7 Phase 7 session. Branch
`luna/macos-m7-phase4-integration`, worktree `J:\Claude code\iolab-m7-phase4-wt`,
HEAD `72ff9ee` at the time of the runs (job 1's `auto`-prefers-native gate
flip had already landed at `e2ffe34`). Physical Mac
`rohansharma@192.168.101.186`, host key verified via `ssh-keyscan` against
the known-good `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
before any command was run.

This document answers the two questions
`docs/macos-m7-phase6-handoff.md` left open in its "Remaining gaps" items 3
and 4, using the **mature, iteratively-hardened Go M4 tooling**
(`hardware-m4.sh`'s own functions + `macos_m4_runtime_darwin_test.go`), not
Phase 6's lightweight from-scratch Python harness.

## Status at a glance

| Question | Verdict |
|---|---|
| Q1. Does the rosetta-amd64 router-console stall reproduce under the mature harness? | **NO — does not reproduce. item-1 PASSED 3/3.** Attributes Phase 6's 0/5 stall to that session's own lightweight harness, not the product. |
| Q2. Does four-node capacity (item-5) pass/fail/hard-wall under the mature harness, on either arm? | **UNEVALUATED — not run.** Blocked on host resource exhaustion on the Mac (3.0 GiB free disk vs. the launcher's own 5 GiB minimum; ~99 MB free RAM). Explicitly NOT rounded to any verdict. |

## Tooling: what was run, and why not the full matrix

Neither question needs the full item-1 → item-7 sequence that
`hardware-m4.sh`'s `main()` (and `hardware-m4-phase5.sh`'s copy of it) runs.
A new reduced driver was written for this,
`packaging/macos/tests/hardware-m4-phase7.sh`, which:

- **sources `hardware-m4.sh`'s function definitions unchanged** — the same
  technique `hardware-m4-power.sh`, `hardware-m4-startstop.sh` and
  `hardware-m4-phase5.sh` already use;
- runs the identical `preflight()` (including its hard assertion that the VM
  name is exactly `iolbox-m4-e2e`, satisfied by passing
  `IOLBOX_M4_MACHINE=iolbox-m4-e2e`), `write_owner()`, `create_sentinels()`,
  `create_guest_sentinel()`, `sentinel_checkpoint()`, `ownership_snapshot()`
  and `run_phase()` discipline around every item it runs — host + guest
  sentinels created before the first item and re-verified after **every**
  item (pass or fail), ownership snapshots (ps/lsof/ifconfig/iptables/
  limactl, host and guest) after every item, and the delete-audit at every
  checkpoint;
- runs an arbitrary ordered, repeat-allowed item list via `IOLBOX_M7_ITEMS`,
  giving each occurrence its own evidence suffix (`p7-1`, `p7-2`, …) so
  repeats do not overwrite each other.

Rationale for the reduced driver rather than `hardware-m4-phase5.sh`: running
the whole matrix — including the 1200 s traffic soak, NAT, extnet
disposition and forced-termination recovery — to answer "does item-1's
console come up" would cost hours per data point, and would additionally
entangle the answer with the soak, which Phase 5 already proved changes
item-5's outcome (3/3 standalone item-5 passes vs. a 2/2 post-soak hard
wall). For Q2 specifically, the post-soak position is exactly the variable
Phase 5 already measured and the owner already waived; what is unmeasured is
the **standalone/fresh** position, so keeping the soak out of the picture is
the point of this driver, not a shortcut around rigor.

### Two changes made in the new (non-frozen) script, both documented in it

Following this project's practice of fixing in the newer script rather than
the frozen `hardware-m4.sh`:

1. **Optional `--console-host-start` / `--capture-host-start` passthrough**
   (`IOLBOX_M7_CONSOLE_HOST_START` / `IOLBOX_M7_CAPTURE_HOST_START`). The
   launcher refuses to start when its default host port ranges are busy, and
   on this Mac the owner's own long-lived validation instance legitimately
   holds 4001 and 9000. This is measurement-neutral: the M4 Go driver never
   dials a host console or capture port — `openConsoles` →
   `m3OpenConsoleWithRetry(r.guiAddr, …)` dials `127.0.0.1:<GUI port>` and
   nothing else — so moving these ranges changes only which host ports the
   launcher forwards on. With neither variable set, the argv built is
   byte-for-byte what the frozen `launcher_start()` builds.
2. **`launcher_start baseline` is guarded with `|| die`.** `launcher_start()`
   *returns* its failing status rather than calling `die()`, so under
   `set -e` an unguarded call (as in `hardware-m4.sh`'s own `main()`) aborts
   the entire run **with no message at all** — the run simply vanishes,
   leaving only a half-populated evidence directory. This was hit twice for
   real this session (ports busy, then disk below minimum) and is the same
   class of silent-death symptom Phase 5 spent hours chasing for other
   reasons. This guard is what surfaced the disk exhaustion in Q2 below
   instead of leaving another unexplained vanished run.

## Q1 — rosetta-amd64 router console under the mature harness: **DOES NOT REPRODUCE**

### Exact artifact under test

The M6 CI candidate named in the Phase 6 handoff as the rosetta baseline,
copied to a working directory so the pristine download stays untouched:

- Source: `~/iolbox-p6-rosetta/iolbox-macos-arm64/` (GitHub Actions workflow
  run `31891847655`, commit `6411120b4910f25e9d546f4c982f44f24b374359`).
- Archive sha256 re-verified on the Mac this session:
  `3023ec68644f35cf74693499213ea6e25f5eb78776662ef9dcbbe0e2ce423d14` —
  **matches** the Phase 6 handoff's recorded value.
- Internal `SHA256SUMS`: **20/20 OK**, zero mismatches.
- Launcher binary `iolbox` sha256
  `e59c0ba19dab47818a5c72594f16f55c635d991e9e30d4ff9d4239d2628dd738`;
  payload `iolbox-server-luna-macos-m6-followups.tar.gz` sha256
  `81ace4622a779ccc1599ba7bb221826a5ba7f56d686c85463725f97667b3cb1f`.
- Profile: the artifact's own default, `debian13` / role `DEFAULT` — i.e.
  the Rosetta path. Confirmed from the run's own launcher stdout:
  `profile=debian13 role=DEFAULT guest=Debian 13 trixie`. A persisted
  `IOLBOX_PROFILE_SELECTION=native-arm64` exists at
  `~/Library/Application Support/iolbox/profile-choice.env` from earlier
  phases; the M6 launcher predates that file entirely (`strings` shows no
  `profile-choice` and no `native-arm64` symbol in it), so it cannot and did
  not influence this run, and the owner's file was left untouched rather
  than reset as a testing side effect.

The **driver** is the current-HEAD mature harness, deliberately: product
under test = the M6 rosetta artifact, test driver = the hardened Go tooling.
Built fresh on the Mac from a `git archive` of HEAD `72ff9ee` (Go 1.26.6,
`/opt/homebrew/bin/go`) at `~/iolbox-p7-build/src`:
`go build -o iolbox-launcher .` and
`go test -c -tags hardware -o iolbox-launcher-hardware.test .`, both clean.
Fixture `four-iol-ring.lab.json` sha256
`614a97008db0d9a2f48f9e0b475eb06e1d67fd4d9ba165dea81dc4b5b37f3161`,
matching Phase 5's corrected fixture.

### Exact command

```
IOLBOX_M4_MACHINE=iolbox-m4-e2e
IOLBOX_M4_GUI_PORT=4011
IOLBOX_M7_CONSOLE_HOST_START=9100
IOLBOX_M7_CAPTURE_HOST_START=5600
IOLBOX_M4_LAUNCHER=$HOME/iolbox-p7-rosetta-assets/iolbox
IOLBOX_M4_ASSETS_DIR=$HOME/iolbox-p7-rosetta-assets
IOLBOX_M4_TARBALL=$HOME/iolbox-p7-rosetta-assets/iolbox-server-luna-macos-m6-followups.tar.gz
IOLBOX_M4_TEST_BINARY=$SRC/tools/iolab-launcher/iolbox-launcher-hardware.test
IOLBOX_M4_FIXTURES=$SRC/tools/iolab-launcher/testdata/macos-m4
IOLBOX_M4_IMAGE=$HOME/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin
IOLBOX_M4_EVIDENCE_PARENT=$SRC/evidence-m4-p7/rosetta
IOLBOX_M7_ITEMS="item-1 item-1 item-1"
  bash packaging/macos/tests/hardware-m4-phase7.sh
```
(run via `nohup … > ~/p7-rosetta-item1.log 2>&1 < /dev/null &`, actively
polled to completion; wrapper retained on the Mac at `~/p7-rosetta-item1.sh`)

### Evidence directory

`~/iolbox-p7-build/src/evidence-m4-p7/rosetta/m4-20260820T222011Z-79262-107393690/`
on the Mac.

### Raw result

`p7-item-status.txt`:

```
item-1	suffix=p7-1	exit=0
item-1	suffix=p7-2	exit=0
item-1	suffix=p7-3	exit=0
```

`item-1/p7-{1,2,3}/phase.json` — all three `status = PASS`:

| Run | start_utc | end_utc | duration |
|---|---|---|---|
| p7-1 | 22:21:58.92Z | 22:24:34.39Z | 155.5 s |
| p7-2 | 22:24:35.23Z | 22:27:09.37Z | 154.1 s |
| p7-3 | 22:27:10.14Z | 22:29:41.95Z | 151.8 s |

**The router console reached a usable prompt in every run**, which is the
exact thing Phase 6 recorded as 0/5 (`item-1/p7-*/console-0.txt`, first
line):

```
p7-1  [2026-08-20T22:22:36.939627Z] initial wake=\r\n prompt=R1>
p7-2  [2026-08-20T22:25:12.028116Z] initial wake=\r\n prompt=R1>
p7-3  [2026-08-20T22:27:44.698708Z] initial wake=\r\n prompt=R1>
```

i.e. `R1>` roughly 33–38 s after each phase start — not 300 s, not 600 s,
not never. VPCS (`console-1.txt`) reached `VPCS>` in ~4 s each run.

Critically, **the specific log line Phase 6 stalled at is present in these
transcripts and was consumed without incident**: each `console-0.txt`
contains 2–3 `%PKI-6-…` lines (the PKI/authoritative-time-source class of
message Phase 6 recorded as its byte-for-byte identical trailing line), and
the harness read straight past them to the prompt, ran `terminal length 0`,
and got `[final-prompt] R1>`.

Real bidirectional traffic, all three runs (`item-1/p7-*/pings.ndjson`),
100 % of the 100-packet pings in both directions:

```
p7-1  ping 192.168.1.1  -c 10      sent 10  recv  9  (10% loss, ARP first-packet)
p7-1  ping 192.168.1.10 repeat 10  sent 10  recv 10  (0%)
p7-1  ping 192.168.1.1  -c 100     sent 100 recv 100 (0%, avg 2.77 ms)
p7-1  ping 192.168.1.10 repeat 100 sent 100 recv 100 (0%)
p7-2  … -c 100 sent 100 recv 100 (0%, avg 1.93 ms) … repeat 100 sent 100 recv 100 (0%)
p7-3  … -c 100 sent 100 recv 100 (0%, avg 2.09 ms) … repeat 100 sent 100 recv 100 (0%)
```

The single dropped packet in each 10-packet warm-up is the known benign
first-packet-ARP pattern already documented in prior phases.

Discipline artifacts all clean: host sentinel verified at `after-launch`,
`after-item-1-p7-{1,2,3}` and `final`; guest sentinel verified at the same
five checkpoints; machine state `Running` at all five; delete-audit empty at
all five. Protected VMs re-checked after the run and untouched:
`iolbox-m5-e2e` `Stopped` (default `LIMA_HOME`),
`iolbox-m7-native-arm64-qemu` `Stopped` (`~/.lima-iolbox-m7p3`). The owner's
`iolbox-native-arm64` under `~/.lima-iolbox-owner-validate` was left
`Running` and untouched throughout.

### Q1 verdict

**NO — the rosetta-amd64 router-console stall does NOT reproduce under the
mature M4 tooling. 3/3 clean PASS on the identical artifact, identical lab
fixture, identical IOL image, same Mac.**

Per the Phase 6 handoff's own stated decision rule for this exact outcome
("If it does NOT reproduce … this points back at Phase 6's lightweight
from-scratch harness as the likely cause of the original stall, not the
underlying product"), the Phase 6 finding **"rosetta-amd64 router console
usable: FAIL — 0/5"** is not supported by the mature harness and should not
stand as a product/environment defect. The most probable remaining
explanation is a defect in Phase 6's own `phase6_run.py` console reader —
consistent with the fact that Phase 6's own live diagnostic session found
the guest IOL process alive and idling (not hung), and that a
hand-rolled diagnostic reader written during that session independently
reproduced a WebSocket-framing desync bug of exactly this class.

Two honest caveats, neither of which changes the verdict:

- This does not retroactively prove *what* was wrong with `phase6_run.py`;
  it establishes only that the product+artifact combination Phase 6 scored
  as FAIL passes cleanly when driven by the mature harness.
- Phase 6's native-arm64 control comparison (3/3 pass) was previously read
  as proving the stall was arm-specific rather than harness-symmetric. That
  inference is now weakened, not confirmed: an asymmetric harness bug (e.g.
  one whose timing only bites when the extra Rosetta translation layer slows
  the console output stream) explains both Phase 6's arm asymmetry and this
  session's clean rosetta result, and is more parsimonious than a product
  defect that a hardened driver cannot see at all.

**Consequence for the Phase 7 gate ledger**: the "Router console usable /
rosetta-amd64: FAIL" row from `docs/macos-m7-phase6-handoff.md`'s verdict
table is superseded by this re-verification and should be re-scored **PASS
(3/3, mature tooling)**. That removes the single explicit FAIL that the
handoff's own "Owner promotion ruling" section had to override.

## Q2 — four-node capacity (item-5) under the mature harness: **UNEVALUATED**

### What was already settled before this session, and is not re-litigated here

Phase 5 produced a real, mature-tooling verdict for **rosetta-amd64 in the
plan-required post-soak position**: item-5 hard-walled on both the initial
attempt and the permitted cold retry, reproducibly (2/2), across two
independent full `hardware-m4-phase5.sh` runs, with all four IOL processes
still alive afterward (ruling out an OOM kill). The owner ruled on
2026-08-20 that this is **waived** as a known Mac-specific hardware-capacity
limit, not a product defect. That finding and that waiver stand unchanged.

What was genuinely open coming into this session: (a) native-arm64's
four-node behaviour under mature tooling (Phase 6 tried it once, with the
lightweight harness, and got a different symptom — "WebSocket closed
mid-frame" on 3 of 4 nodes), and (b) whether Phase 6's claim of a *fresh,
no-soak* four-node failure on both arms — materially worse than Phase 5's
post-soak-only finding — holds up.

### What actually happened: blocked on host resources, run not started

The rosetta item-5 attempt was launched (wrapper
`~/p7-rosetta-item5.sh`, `IOLBOX_M7_ITEMS="item-5 item-5"`, everything else
identical to the Q1 run) and **aborted in `launcher_start baseline`, before
any measurement**:

```
FAIL: launcher_start baseline failed (see .../commands/launcher-start-baseline.stderr)
$ cat .../commands/launcher-start-baseline.stderr
free disk is below the 5 GiB minimum
```

Evidence directory (half-populated, preflight only, no item-5 attempt):
`~/iolbox-p7-build/src/evidence-m4-p7/rosetta/m4-20260820T223038Z-80168-3272939081/`

Measured host state at that moment:

- **Disk**: `/System/Volumes/Data` 228 GiB total, 195 GiB used, **3.0 GiB
  available, 99 % capacity** — below the launcher's own 5 GiB precondition.
- **RAM**: `top -l 1` reported `PhysMem: 7533M used (1181M wired, 3700M
  compressor), 99M unused` of 8 GiB physical; `sysctl vm.swapusage` showed
  `used = 5134.56M` of 6144 M swap.
- The owner's validation instance `iolbox-native-arm64`
  (`~/.lima-iolbox-owner-validate`) is **Running** and holds a 4 GiB
  allocation plus host ports 4001 and 9000
  (`lsof` → `limactl` PID 78118).

Accumulated per-phase state accounting for the disk (measured, for whoever
decides what to reclaim):

| Path | Size | Note |
|---|---|---|
| `~/Library/Caches/lima/download/by-url-sha256/` | 13 G total, 7 entries | Lima base-image cache; see breakdown below |
| `~/iolbox-m7-phase3` | 8.5 G | Phase 3 run artifacts |
| `~/.lima/iolbox-m5-e2e` | 3.7 G | **protected, do not touch** |
| `~/.lima-iolbox-p6-native/iolbox-native-arm64` | 3.2 G | Phase 6 leftover, Stopped |
| `~/.lima-iolbox-owner-validate/iolbox-native-arm64` | 3.2 G | owner's, Running |
| `~/.lima/iolbox-p5-m3-e2e` | 3.0 G | Phase 5 M3 evidence VM, Stopped |
| `~/.lima-iolbox-p4/iolbox-native-arm64` | 2.9 G | Phase 4 leftover, Stopped |
| `~/.lima/iolbox-m4-e2e` | 2.8 G | this session's M4 VM, Stopped |
| `~/.lima-iolbox-p6-rosetta/iolbox-debian13` | 2.8 G | Phase 6 leftover, Stopped |
| `~/.lima-iolbox-p4/iolbox-debian13` | 2.5 G | Phase 4 leftover, Stopped |
| `~/.lima-iolbox-p6-rosetta/seed` | 2.4 G | Phase 6 placeholder instance |
| `~/.lima-iolbox-m7p3/iolbox-m7-native-arm64-qemu` | (3.5 G dir) | **protected, do not touch** |

Lima base-image cache breakdown, cross-referenced against the pins actually
in use (`pinned-image-debian13.env`, both at current HEAD and in the M6
candidate, pin `debian-13-genericcloud-arm64-20260810-2566.qcow2`):

| Cache entry | Size | URL | Referenced? |
|---|---|---|---|
| `e15852e5…` | 1.5 G | debian-13 trixie **20260810-2566** | **YES — current pin, keep** |
| `a1e07686…` | 249 M | nerdctl 2.3.5 linux-arm64 | **YES — in the running hostagent's argv, keep** |
| `ea9e1862…` | 3.3 G | ubuntu 26.04 resolute 20260720 | not referenced by any iolbox profile |
| `a86fcad9…` | 2.4 G | ubuntu 24.04 noble 20260705 | not referenced by any iolbox profile |
| `3f1c81a4…` | 2.2 G | ubuntu 22.04 jammy **20260705** | superseded — jammy profile pins 20260807 |
| `e937eed3…` | 1.5 G | debian-13 trixie **20260712-2537** | superseded pin |
| `fa691324…` | 1.5 G | debian-13 trixie **20260518-2482** | superseded pin |

The three superseded/unreferenced base images alone (`3f1c81a4`, `e937eed3`,
`fa691324`) are 5.2 G, and are content-addressed downloads Lima re-fetches on
demand if ever needed again — the cheapest, most reversible reclaim
available. **No deletion was performed**, per this project's standing rule
that agents do not `rm -rf` on shared hosts and instead report paths.

### Why this was not worked around

Two reasons, both about evidence quality rather than convenience:

1. **The port conflict was worked around; the resource exhaustion was not,
   deliberately.** item-5 is specifically a *resource-headroom* measurement —
   four concurrent IOL nodes at 1024 MB each inside a fixed 4 GiB guest.
   Running it with 99 MB of host RAM free, 5.1 GiB of swap already in use,
   3 GiB of disk free, and a second 4 GiB VM resident would guarantee a
   failure that says nothing whatsoever about the product or about this
   Mac's real headroom. Phase 6's own unresolved caveat already names host
   resource margin as one of the two candidate explanations for its 4-node
   result; reproducing that ambiguity with worse conditions would add noise,
   not evidence.
2. **The reclaimable state is the owner's to release.** The obvious
   candidates are the owner's still-Running validation instance, prior
   phases' VM directories, and cached base images — none of which an agent
   should remove unilaterally on a shared host.

### Q2 verdict

**UNEVALUATED — for both arms.** Not attempted-and-failed; **not attempted**.
Per the plan's own rule (section 13, "a missing metric is UNEVALUATED, never
zero"), this is explicitly not scored as a pass, a fail, or a hard wall.

Specifically:

- **rosetta-amd64, post-soak position**: unchanged from Phase 5 — reproducible
  (2/2) hard wall, **owner-waived** as a Mac-capacity limit. Not re-run this
  session and, given Phase 5's 2/2 reproducibility and the owner's ruling,
  did not need to be.
- **rosetta-amd64, standalone/fresh position**: **UNEVALUATED this session.**
  Phase 5's 3/3 standalone passes remain the most recent mature-tooling data
  point and directly contradict Phase 6's single-attempt fresh-boot failure
  claim; that contradiction is still unresolved.
- **native-arm64, any position**: **UNEVALUATED.** This is the genuinely
  unmeasured cell. Phase 6's single lightweight-harness attempt
  ("WebSocket closed mid-frame" on 3 of 4 nodes) should not be treated as
  confirmed, particularly now that Q1 has shown that same harness producing
  a console failure the mature tooling cannot reproduce at all.

### What unblocks it

One owner decision, then roughly 30–40 minutes of runtime per arm:

1. Free ≥ 5 GiB of disk (the 5.2 GiB of superseded/unreferenced Lima base
   images above is the least destructive option; prior phases' Stopped VM
   directories are the next tier).
2. Ideally also stop the `iolbox-native-arm64` validation instance under
   `~/.lima-iolbox-owner-validate` for the duration, so four-node capacity is
   measured against a representative headroom rather than against a Mac
   already carrying a second 4 GiB VM. (`limactl stop --tty=false` with stdin
   from `/dev/null`, per this Mac's documented requirement.)

Then, for each arm:

```
IOLBOX_M7_ITEMS="item-5 item-5" bash packaging/macos/tests/hardware-m4-phase7.sh
```

with the rosetta env block above, and with `IOLBOX_M4_LAUNCHER` /
`IOLBOX_M4_ASSETS_DIR` / `IOLBOX_M4_TARBALL` repointed at a native-arm64
build plus `--profile native-arm64` for the native arm. A current-HEAD
launcher is already built at
`~/iolbox-p7-build/src/tools/iolab-launcher/iolbox-launcher`; native payload
candidates already on the Mac are
`~/iolbox-p6-build/src/runtime/build/iolbox-server-p6-2b6939f-linux-arm64.tar.gz`
(Phase 6's, sha256 `f4e8ad5e…`) and
`~/iolbox-owner-validate-build/iolbox-server-dev-linux-arm64.tar.gz` (the
owner-validated build, newer — includes both netprobe console fixes).

## What this session did NOT do

- Did not delete or modify anything on the Mac outside its own
  `~/iolbox-p7-*` working directories and the `iolbox-m4-e2e` VM in the
  default `LIMA_HOME` (the M4 harness's own owner-approved namespace).
- Did not touch `iolbox-m5-e2e` or `iolbox-m7-native-arm64-qemu`; both
  re-verified `Stopped` after the run.
- Did not stop, tear down, or otherwise disturb the owner's
  `iolbox-native-arm64` validation instance; it was `Running` before and
  after.
- Did not reset the owner's persisted `profile-choice.env`.
- Did not change any production source. The only repo change is the new
  `packaging/macos/tests/hardware-m4-phase7.sh` test driver and this
  document.
