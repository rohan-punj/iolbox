# M7 Phase 5 handoff — authoritative M3/M4 matrix rerun, CLOSED (capacity row owner-waived)

Written 2026-08-20, end of the Phase 5 session. Branch
`luna/macos-m7-phase4-integration`, worktree `J:\Claude code\iolab-m7-phase4-wt`,
HEAD `fb49ce8`. Working tree is clean.

**Owner ruling received 2026-08-20**: the four-node capacity gap
(post-soak position, item 5) is **waived**. The owner accepted this as a
known hardware-capacity limit specific to this Mac, not a product defect,
and directed that Phase 5 close on that basis rather than block on a
retry or hardware change. See "The one real gap" below for the finding
this waives, and "Owner waiver" for the ruling itself.

**Companion doc**: none yet for Phase 6 — start there next. Phase 5's exit
criterion is now met (every row PASS, legitimate `NOT_EXERCISABLE`, or
owner-waived), so Phase 6 (plan section 12, same-machine Rosetta vs.
native A/B metrics) can begin, still on `luna/macos-m7-phase4-integration`.

## Status at a glance

- **Phase 5: CLOSED.** Every matrix-group row from plan section 11 is
  genuinely PASS or a legitimate `NOT_EXERCISABLE`, except **four-node
  capacity (item 5)**, which was honestly `BLOCKED/UNVERIFIED` in the
  plan-required post-soak position and has now been **owner-waived** (see
  below). This was a real, reproducible (2/2) hardware-capacity limit on
  this specific Mac, not a verification gap — see "The one real gap"
  below for full detail.
- **10 real defects found and fixed this session**, all independently
  reproduced before fixing (full list below). Several were long-standing
  latent bugs in the frozen `hardware-m4.sh` orchestrator that had
  apparently never been exercised on real hardware before this Phase 5
  session — not regressions introduced by Phase 4 or 5.
- **Owner-directed deviation, fully documented**: the plan's 7200s (2h)
  traffic-soak duration was reduced to 1200s (20 min) per explicit
  mid-session owner instruction. Consequence: multi-hour resource-drift
  effects were not exercised at the plan's full stated duration. Recorded
  in `docs/m7-evidence/phase5/STATUS.md` and multiple commit messages.

## Full plan-section-11 matrix verdict

| Matrix group | Verdict | Evidence |
|---|---|---|
| Browser lifecycle | **PASS** | `docs/m7-evidence/phase5/m3-rerun/.../browser-equivalent.txt` |
| Host data/sync (restart + spaces/non-ASCII) | **PASS** | same dir, `launcher-start-difficult-path.txt` |
| Consoles/forwarding (loopback-only, 2 concurrent) | **PASS** | same dir, `port-probe-host.txt`, `lsof-listeners.txt` |
| Capture (valid pcapng) | **PASS** | same dir, `browser-equivalent.txt` |
| VPCS/IOL (rosetta-amd64, item-1) | **PASS** | Mac-side hardware evidence, real ping traffic |
| VPCS/IOL (native-arm64) | **PASS** | `docs/m7-evidence/phase5/native-arm64/vpcs-iol-traffic.txt` — real bidirectional ICMP |
| Multi-link (item-2) | **PASS** (after real fix) | real hardware, both directions ≥99/100 |
| NAT (item-3) | **PASS** (after real fix) | real hardware, gateway + numeric-target ping |
| Extnet (item-4) | **NOT_EXERCISABLE** — legitimate decision-table result, not a waiver | real host-interface probes |
| Capacity (four-node, item-5) | **BLOCKED/UNVERIFIED in the plan-required post-soak position (2/2)**; functionally correct in isolation (3/3 standalone) | see below |
| Traffic soak | **PASS at 1200s** (owner-directed deviation from the plan's 7200s) | `SOAK-COMPLETE` seal verified, 20/20 rows |
| Forced termination (item-7) | **PASS** (via manual driving after fixing a real ordering bug) | real forced-kill + forced-VM-stop + recovery |
| Rosetta exclusion | **PASS** | `docs/m7-evidence/phase5/native-arm64/rosetta-inventory/{before,during,after}.txt` |

## The one real gap: four-node capacity in the post-soak position

With every harness/fixture bug fixed, two independent full orchestrator
runs (soak PASS, then item-5 immediately after) both hard-walled on **both**
the initial attempt and the one permitted cold retry — a genuine,
reproducible (2/2) capacity-margin limit on this specific Mac right after 20
minutes of sustained soak traffic. Not a flake: all four IOL processes
stayed alive throughout (ruled out OOM). The identical topology passes
reliably (3/3) when run standalone, not preceded by a soak.

Per the M4 plan's own terminal-state definition, this is exactly its
defined `M4 BLOCKED/UNVERIFIED` outcome, and the plan is explicit that "no
reduced topology or RAM is equivalent" — so it is reported as-is rather than
worked around. Closing this requires one of:
1. More RAM/CPU headroom on this specific Mac (a hardware/environment
   change, not a code fix), or
2. An explicit owner-approved capacity waiver for this specific row, or
3. Retrying at a time when the Mac has more free memory (the plan's own
   witness-VM-reclaim mechanism exists for exactly this, but this Mac no
   longer has the witness VMs `hardware-m4.sh` was written to reclaim RAM
   from — see defect 10 below).

None of these were a decision this session was authorized to make on its
own.

## Owner waiver

Ruling received 2026-08-20 (start of the following session, in response to
this handoff): **waive the finding**. The owner accepted the post-soak
four-node capacity limit as a known constraint of this specific Mac, not a
product defect — the underlying capability (four real concurrent IOL
nodes, real traffic, all consoles reachable) is proven correct in
isolation, and the failure is a resource-margin timing issue specific to
running immediately after a sustained soak on this hardware. No retry, no
hardware change, and no further Phase 5 work is required for this row.
Phase 5's exit criterion is met on that basis.

This does not retroactively fix defect 10 (missing witness VMs for
`hardware-m4.sh`'s RAM-reclaim path) or make the capacity tier reliable in
the post-soak position on this Mac — if a future phase or a real user
workflow needs four-node capacity immediately after sustained traffic on
this same hardware, that constraint still applies and should be
re-surfaced, not assumed fixed.

## 10 real defects found and fixed this session (all independently reproduced first)

1. **Item-2 multi-link**: `consoles[1]` (R2) never got `m4Enable()` called
   before its privileged-EXEC ping, so it failed deterministically at the
   unprivileged prompt.
2. **Item-3 NAT**: console probing was requested for the NAT infrastructure
   node, which has no telnet console at all — WS handshake 502.
3. **Item-5 `HardWall`** was only set at two early gates in
   `macos_m4_runtime_darwin_test.go`, missing the actual capacity-failure
   point — moved into `basicPhase`'s deferred cleanup so it covers any
   item-5 failure.
4. A **frequent (not rare) WS control-connection framing error** on the
   guest control channel — added a bounded reconnect-retry to the client.
5–7. **Three separate sentinel-checkpoint-on-a-stopped-VM ordering bugs**
   in the frozen `hardware-m4.sh` procedure (item-5 reclaim-retry, item-7
   forced-termination recovery, and the final checkpoint) — each calls
   `sentinel_checkpoint` (which needs a running guest) immediately after a
   `launcher_stop` that had just stopped it, guaranteeing failure whenever
   that code path is actually exercised on real hardware. These explained
   every "mysterious silent death" seen in automated runs this whole
   session. Fixed in the Phase-5-specific `hardware-m4-phase5.sh`
   orchestrator (the frozen `hardware-m4.sh` was deliberately left
   untouched). **These paths appear to have never been exercised on real
   hardware in this project's history before this Phase 5 session.**
8. **Item-5's required R1↔R3 diagonal ping was mathematically unachievable
   as written**: wrong target address (R4's interface, not R3's), plus two
   missing transit static routes on R1/R2. Found by the plan's own
   independent verifier (`TestM4VerifyRecord`) catching what item-5's
   primary pass/fail check missed — exactly what that verifier layer exists
   to do. Fixed the target and added the routes; this necessarily changed
   the frozen fixture `four-iol-ring.lab.json`'s SHA-256 from
   `f25d8e4b9a5a750c895696d6d7f609cabc03dcab83a040a921c6719a3df927ee` to
   `614a97008db0d9a2f48f9e0b475eb06e1d67fd4d9ba165dea81dc4b5b37f3161` — a
   deliberate, fully documented deviation (the original fixture could not
   pass its own stated bar due to a real defect, not a scope change).
9. M3's historically-frozen `iolbox-server-v0.5.2.tar.gz` payload predated
   Phase 4's `DisableI386` guest contract — swapped to the current payload
   Phase 4's own scenario 3 already validated.
10. `hardware-m4.sh`'s witness-VM preservation step references five VMs
    (`m1jammy`, `m1trixie`, `iolbox-m1-e2e`, `iolbox-m2-e2e`,
    `iolbox-m3-e2e`) that no longer exist anywhere on this Mac. Worked
    around with real substitute witnesses in `hardware-m4-phase5.sh` rather
    than fabricating stand-ins under those names. This is also the reason
    item 5's RAM-reclaim path has no real witness inventory to reclaim from
    on this Mac — directly relevant to closing the capacity gap above.

Two additional WS anomalies (an `item-1` read error and a pre-fix `item-5`
console EOF) did **not** reproduce on independent retry and were correctly
recorded as non-reproducing transients rather than fixed speculatively.

## Hardware access

Unchanged from the Phase 4 handoff:

- Physical Mac `rohansharma@192.168.101.186` — verify via `ssh-keyscan`
  against known host key
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
  before trusting it; the Mac went fully unreachable/asleep once mid-session
  this time too — no remote-wake mechanism, ask the owner.
- Key `.m7-ssh/iolbox_mac_m0` lives in the Phase 3 reference worktree
  `J:\Claude code\iolab-m7-wt`, not this one.
- `limactl` at `/opt/homebrew/bin/limactl`; `stop`/`delete` need
  `--tty=false` and `< /dev/null`.
- Protected VMs, never touch: `iolbox-m5-e2e`, `iolbox-m7-native-arm64-qemu`.
  Both independently reverified untouched at session end.
- Mac left in a clean state: no stray VMs or processes.

## Next session's actual job

**Start Phase 6.** Phase 5's exit criterion is met (owner-waived capacity
row, everything else PASS or legitimate `NOT_EXERCISABLE`). Begin plan
section 12 (same-machine Rosetta vs. native A/B metrics), still on
`luna/macos-m7-phase4-integration`, no fresh worktree needed. Carry
forward the waiver's caveat above: post-soak four-node capacity on this
Mac remains a known constraint, not a fixed one — don't assume it away if
Phase 6/7 work happens to combine soak-like load with four-node topologies
again.

## Working pattern used this arc (recommended to continue)

Unchanged: direct Sonnet Agent execution for hardware work, sol-medium
reserved for contract-redefining decisions, reproduce every bug
independently before fixing (10 more real bugs found this way this phase
alone, on top of 6 in Phase 4 and 10+ in Phase 3), active-poll every
long-running command to actual completion rather than assuming a passive
notification.
