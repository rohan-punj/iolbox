# M4 result — Apple Silicon macOS runtime and capacity qualification: **PARTIAL**

Updated 2026-08-14, end of the M4 session. Same honesty bar as M1/M2/M3:
this records exactly what ran on real hardware versus what only compiled or
was unit-tested, per item, with no rounding up.

Branch: **`luna/macos-m4-runtime`**, off `luna/macos-m3-ux`, in worktree
`J:\Claude code\iolab-m4-wt`. **Not merged anywhere** — the stack is `main`
← (unmerged) `luna/macos-m1-provisioner` ← (unmerged) `luna/macos-m3-ux` ←
(unmerged) `luna/macos-m4-runtime`. Plan `docs/macos-m4-plan.md` is frozen at
SHA-256 `b134c32372236d3dc29d20d550a21e4e408b1e1aaaa47b96122c10558414882a`,
which includes an **owner-approved scope deviation**: item 6's soak runs
600 s (10 minutes), not the 7,200 s (2 hours) named in
`docs/macos-arm64-plan.md` §M4 / `docs/macos-m4-prompt.md`. Every other
soak isolation/seal/failure-handling rule in the plan is unchanged and was
enforced at the 600 s duration.

---

## 1. Verdict: PARTIAL — the 8-item sequence never completed in one run

M4's acceptance criterion is a single continuous hardware sequence, items
1 → 8, machine-verified via `summary.json` + the independent record
verifier. **That full sequence was never completed this session.** Real,
reproducible product and harness defects were found at almost every item
attempted; each was root-caused and fixed, but the fixes were not always
re-run to a hardware PASS afterward before the session moved on. Do not
read "fixed" as "verified passing on hardware" below — they are called out
separately per item.

| Item | Disposition | Evidence |
|---|---|---|
| 1. VPCS/IOL bidirectional ping | **PASS on hardware** | `~/iolbox-m1/evidence-m4/m4-20260815T003747Z-33632-135014100/item-1/` (run7), reconfirmed by item-1 in run9 |
| 2. Multi-link + capture | Failed on hardware (ping-summary regex only accepted exactly 10/100-count totals; `captureShort`'s validation ping used `repeat 20`). Fix applied + unit-tested. **Not re-run on hardware after the fix.** | run9 evidence; fix in `macos_m4_runtime_darwin_test.go` (`m4IOSPingRE`) |
| 3. NAT client + counters | **Not executed by the automated harness this session at all.** A human manually confirmed the NAT node hands out DHCP (172.31.1.100) via the GUI, which is a weaker check than the automated 19/20-ping + iptables-counter bar this item requires. | none |
| 4. extnet mechanized disposition | **Not executed.** | none |
| 5. Four-node capacity + RAM-wall | **Not executed.** | none |
| 6. Isolated 600 s soak + power audit | **PASS on hardware**, as a standalone sub-test (`hardware-m4-power.sh`, item 6 in isolation) | `~/iolbox-m1/evidence-m4/power-m4-20260815T022722Z-39602-928438487/` — 654 s measured window, 10/10 traffic intervals, 0 packets lost, `SOAK-COMPLETE` independently re-hashed and verified PASS |
| 7. Forced launcher/VM termination + recovery | Failed on hardware (Lima port-forward parser rejected the VM's own block-style YAML). Fix applied + unit-tested with a hardware-derived regression test. **Not re-run end-to-end on hardware after the fix.** | `~/iolbox-m1/evidence-m4/startstop-m4-20260815T014401Z-37534-1656999020/`; fix in `tools/iolab-launcher/macos_ports.go` |
| 8. Final record + independent verifier | **Not exercised** — it aggregates 1-7, which never all passed together in one run. | none |

**Items genuinely proven on real Apple Silicon hardware this session: 1 and
6.** Items 2 and 7 have code fixes that are unit-tested but not yet
hardware-reconfirmed. Items 3, 4, 5, 8 were not attempted at all.

---

## 2. Defects found and fixed this session

All found by running the harness against real hardware, not by static
review — consistent with every prior M-session's experience.

### Harness-side (this branch, uncommitted until this session's commit)

1. **Console read race** (`m4Console.send`): a leftover prompt echo from
   the console-wake sequence could be read as the *response* to the next
   command sent, before that command's real output arrived — a ping issued
   right after opening the console would report zero output. Fixed with a
   `drainStale()` flush before every write.
2. **VPCS has no boot-time IP-injection path in the supervisor** (only
   `tool`/`pc` kinds read `Config["net"]`; classic `vpcs` is
   interactive-console-only, matching real VPCS/GNS3 behavior — confirmed
   this is by design, not a product bug). The fixture's `config.commands`
   field was silently never sent to the console. Fixed: the harness now
   parses each `vpcs`-kind node's `config.commands` from the fixture and
   issues them to its console before any ping.
3. **IOS ping-summary regex too narrow**: `m4IOSPingRE` only matched a
   literal `(10|100)` total-count group, so a valid `repeat 20` ping (used
   by `captureShort`'s own validation ping) reported "no complete ping
   summary" despite succeeding 19/20. Widened to accept any count.
4. **Ping read timeout too short for `-c 100`/`repeat 100`**: fixed 30-45 s
   timeouts assumed this vpcs build's ping is instantaneous; at ~1
   packet/second, a 100-count ping needs >100 s. Added `m4PingTimeout(count)`
   scaling the budget to the actual packet count, applied at every call site.
5. **IOS ping-summary parser inverted loss percentage**: assigned IOS's own
   "Success rate is X percent" straight into the `LossPct` field without
   inverting — a perfect 10/10 ping reported `loss_percent: 100`. Fixed to
   compute `lost/sent*100` directly; reconfirmed correct (`0`) in the
   passing soak run.
6. **Soak seal computed before its own cleanup finished writing to the
   file it seals**: `soak()` hashed `control.ndjson` into `SOAK-COMPLETE`,
   then called `stopLab()` afterward — but `stopLab`'s `lab.stop` request
   is logged into that same `control.ndjson` by every `r.request()` call,
   so the file changed *after* being sealed. Every soak, however clean,
   failed its own independent re-verification. Fixed by moving `stopLab`
   before the hashing/sealing block. Reconfirmed: seal now passes
   independent re-verification.
7. **`-args` flag misuse**: `hardware-m4.sh` (and the standalone
   `hardware-m4-power.sh` sub-script, which had its own copy) invoked the
   *compiled* test binary with `-args -m4-soak PATH` / `-args -m4-record
   PATH` — `-args` is only meaningful to `go test`'s own CLI wrapper, not
   when running the binary directly, so the verifier calls always failed
   with "flag provided but not defined: -args" regardless of the actual
   evidence. Fixed by dropping `-args` in both scripts.

### Product-level (fixed on `main`, hardware-verified, unrelated to this branch)

These surfaced while testing M4's VPCS-adjacent scope but are shared code
used by every deployment target, not Mac-specific — see their own commits
for full detail:

8. **Tool-pack binaries (netprobe/pc, aaa, webserver, httpclient, syslog,
   netsvc, secbench) were never built or shipped by the native/cloud-Linux
   install target** (`build-rootfs.sh`, used by WSL/VMware/OVA/qemu, already
   did this correctly — only `pack-native.sh`/`install.sh` had the gap).
   Fixed and merged: `main@e792c32`.
9. **The native cgroup-placement fallback broke every tool-pack launch
   whenever the primary clone3 `CLONE_INTO_CGROUP` placement fails** (which
   it does on this Rosetta-translated Apple Silicon host): the fallback
   wraps `iolbox-toollaunch` in `ip netns exec`, which unshares a fresh
   mount namespace for its `/sys` view — invisible to a cgroup2 directory
   created moments earlier in the parent namespace, so the path-based
   `--cgroup PATH` write always failed "no such file or directory" even
   though the directory plainly existed outside the netns wrapper. Fixed by
   passing the already-open cgroup directory fd via `cmd.ExtraFiles` and
   joining through `/proc/self/fd/N/cgroup.procs` instead of a path lookup.
   Merged: `main@df24ab1` (merge commit `4f7643c`). Verified live: netprobe
   (`pc`) and AAA Server nodes, previously failing to boot, now start and
   run — the fix is general (shared launch code), not netprobe-specific.

### Environment-local, not a code bug

10. A manually-staged switch IOL image
    (`x86_64_crb_linux_l2-adventerprisek9-ms.iol17.18.02.bin`) was copied
    into `/opt/iolbox/images/` without the executable bit, so the switch
    node failed to start with a `pty start: fork/exec` error. Fixed with
    `chmod 755`. Not a code defect — a one-off manual-staging mistake this
    session made and then fixed.

### Filed, not fixed this session

11. **Netprobe console command-history editing bug**: pressing the up
    arrow correctly recalls the previous command but the cursor cannot
    then be moved within it to edit before submitting. Confirmed live by
    the owner. Root cause not yet located — candidates are
    `app/src/lib/components/FloatingConsoleWindow.svelte` (client-side
    terminal) or `runtime/files/tools/packs/pc/gui/{cli,state}.go`
    (netprobe's own server-side history model). Filed as a background task
    (`task_ad8ced7a`); status unknown as of this doc — check separately.

---

## 3. A recurring operational mistake this session — read before running anything

**The M4 harness's default `--tarball` is the plain M4-scope
`iolbox-server-v0.5.2.tar.gz`, which has no tool-pack binaries at all.**
Every time `launcher_start` (in `hardware-m4.sh` or either sub-script) runs
without an explicit `--tarball`/`IOLBOX_M4_TARBALL` override, it silently
reinstalls that plain build over whatever was there before — including
**over the netprobe/cgroup-fix build**, twice, undetected until the owner
noticed netprobe had "regressed" and asked for explicit confirmation.

The fixed build lives at
`~/iolbox-m0/iolbox-server-v0.5.3-netprobe-cgroupfd-fix.tar.gz` on the Mac
(sha256 `eadfe20e926cf30766c3acb7c5daec49ee90fd546873176da03f17cdcc0c282c`),
and a copy is pulled to the Windows host at
`<session-scratchpad>/iolbox-tarballs/iolbox-server-v0.5.3-netprobe-cgroupfd-fix.tar.gz`.
**Any session running M4 tests that also cares about tool packs (items 3
NAT excepted — NAT is not a `tool`-kind node; items exercising `pc`/`aaa`/
etc. are) must pass `--tarball` explicitly.** M4's own 8-item sequence
itself does not require tool packs (only `iol`/`vpcs`/`nat` kinds), so this
only matters if a future session is also verifying the netprobe/cgroup-fd
fixes on the same VM — but it is very easy to forget, as this session
proved twice.

---

## 4. Sub-test scripts added this session

Two standalone scripts were added alongside `hardware-m4.sh`, each sourcing
its function definitions (everything before its own unconditional
`main "$@"`) to run one slice of the full sequence in isolation, useful for
iterating on a single item without paying for the whole 8-item run:

- `packaging/macos/tests/hardware-m4-startstop.sh` — preflight, baseline
  launch, forced-launcher-kill, forced-`limactl stop --force`, recovery,
  item-7's VPCS/IOL recovery-proof ping, final stop. (This is what
  surfaced defect #7 above.)
- `packaging/macos/tests/hardware-m4-power.sh` — preflight, baseline
  launch, the isolated 600 s soak with power audit, seal verification,
  final stop. (This is what surfaced defects #6 and #7-flag above, and
  proved item 6 PASS.)

Both default to the M4-scope `v0.5.2` tarball/image paths, matching
`hardware-m4.sh`'s own defaults, and accept the same `IOLBOX_M4_*`
environment overrides.

---

## 5. Environment as it actually is now

Unchanged from the M3 handoff except:

| Item | Value |
|---|---|
| Host | `rohansharma@192.168.101.166`, key `.m4-ssh/iolbox_mac_m0` (workspace-local copy of `~/.ssh/iolbox_mac_m0`, gitignored — the original is unreadable from inside the sandboxed session that did most of this work) |
| Supervisor build on `iolbox-m4-e2e` at end of session | `v0.5.2-7-ge792c32-dirty` (the netprobe/cgroup-fd-fixed build) — **verify this hasn't been silently reverted per §3 before trusting any packs-related test** |
| `iolbox-m4-e2e` Lima config | Has accumulated a `guestPortRange`/`hostPortRange` block-style rewrite from Lima's own YAML marshaler (see defect #7's fix) — this is now handled correctly, but if a *different* Mac/VM is used instead, verify the fix still applies there too |

### Lima machines on the Mac (end of session)

| Machine | State | Notes |
|---|---|---|
| `iol22` | Stopped | **M0 evidence machine. Never touch.** |
| `iolbox-m1-e2e`, `iolbox-m2-e2e`, `iolbox-m3-e2e` | Stopped | Reusable/deletable |
| `iolbox-m4-e2e` | Stopped | M4 working machine — has the fixed build installed (verify per §3), both IOL images (router + L2 switch) registered |
| `m1jammy`, `m1trixie` | Stopped | M1 profile canaries, reusable/deletable |

Evidence roots: `~/iolbox-m1/evidence-m4/` on the Mac (multiple run-id and
`startstop-`/`power-` prefixed subdirectories from this session's
iterative debugging — the two PASS runs are named explicitly in §1's
table; the rest are intermediate/failed attempts kept for traceability,
not cleaned up).

---

## 6. Open items going into M5

| # | Sev | Item |
|---|---|---|
| 1 | BLOCKING for M4 closure | Items 2 and 7 need a hardware re-run to confirm their fixes actually produce a PASS, not just a clean unit test. |
| 2 | BLOCKING for M4 closure | Items 3, 4, 5, 8 have never been attempted on hardware at all. |
| 3 | NOTE | §3's tarball-overwrite trap will recur for any future session unless it explicitly overrides `--tarball`. |
| 4 | NOTE | Defect #11 (console history/cursor) is filed but unresolved; check `task_ad8ced7a`'s outcome. |
| 5 | NOTE | Four-node qualification (item 5) may need more than this Mac's 8 GB RAM — still an undecided owner call, carried from M3. |
