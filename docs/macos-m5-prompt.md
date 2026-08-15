# M5 implementation prompt (next session)

Paste the block below into a new session. It is self-contained.

---

Implement **M5** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab`, using **luna at xhigh reasoning** for implementation
and **sol at medium** for planning/review, the same pattern used for M2-M4.

## Read first, in this order

1. `docs/macos-m4-handoff.md` — **start here.** Current state, defects
   found and fixed, the tarball-overwrite operational trap, and open items
   going into M5.
2. `docs/macos-m4-result.md` — the executed M4 record. **M4 is PARTIAL, not
   PASS** — items 1 and 6 are proven on hardware; items 2 and 7 have fixes
   applied but not hardware-reconfirmed; items 3, 4, 5, 8 were never
   attempted. M5 does not functionally depend on the unexecuted items (NAT,
   extnet, four-node capacity, final record are independent test
   scenarios), but do not describe M4 as complete in anything you write.
3. `docs/macos-m3-handoff.md` and `docs/macos-m1-handoff.md` — earlier
   gotchas still in force (liveness vs readiness, console ring-buffer
   replay, `ram: 256` wedging IOL, the shared-worktree/branch gotcha, etc.).
4. `docs/macos-arm64-plan.md` §M5 (search for "M5 — suppress false
   capabilities and finalize platform diagnostics"). **Immutable.**

Where these disagree, precedence is: M4 handoff > M4 result > M3 handoff >
M1 handoff > plan.

## Context you must not re-litigate

M1-M4 are implemented on branch `luna/macos-m4-runtime` (off
`luna/macos-m3-ux`, off `luna/macos-m1-provisioner`, off `main` — none of
the parents are merged; branch from `luna/macos-m4-runtime`). A Go Darwin
launcher drives the whole VM lifecycle; the GUI is reachable through an
explicit loopback-only Lima port-forward contract; host folder sync
round-trips; a browser-equivalent HTTP/WS flow proves the full lab
lifecycle; VPCS/IOL bidirectional ping and a 600 s sustained-traffic soak
with a cryptographically sealed evidence manifest are both proven on real
Apple Silicon hardware.

Two shared-code defects were found and fixed during M4 testing and are
**already merged to `main`** (not just this branch): missing tool-pack
binaries in the native install target (`main@e792c32`), and a cgroup-fd
launch bug that broke every tool-pack node start on this Rosetta-translated
host (`main@df24ab1`, merge `4f7643c`). If you see tool-pack nodes
(netprobe/aaa/webserver/etc.) failing with "runtime does not support ...
nodes" again, first check (a) whether `main`'s two commits above are
actually present in your branch's ancestry, and (b) whether the VM's
running supervisor build string actually matches what you think you
installed — M4's session hit the exact same "regression" twice, both times
caused by the test harness's own default `--tarball` silently reinstalling
an old build. See M4 handoff §2 for the exact mechanism.

**Required first step, per the plan's own M5 dependency note**: "reconcile
`luna/macos-arm64-invariant` before changing shared supervisor/runtime
files." As of the end of the M4 session, the main `iolab` checkout (not a
worktree — `J:\Claude code\iolab` itself) is sitting on branch
`luna/macos-arm64-invariant` with **substantial uncommitted changes**
touching Linux-specific runtime files (`supervisor/internal/dirstat/
dirstat_linux.go`, `supervisor/internal/iouyap/tap_linux.go`,
`supervisor/internal/slowtee/slowtee_linux.go`, `supervisor/internal/tool/
reap_linux.go`, `supervisor/internal/vtap/shim_linux.go`, `tools/
p0-reaper/reaper_linux.go`, `tools/secbench-attacks-go/internal/
attackcommon/raw_linux.go`, plus `build-release.sh`, `runtime/
build-rootfs.sh`, `runtime/fetch-vpcs.sh`) and several untracked docs. This
looks like the arm64-native/FEX work referenced as M7 in the plan, but was
never confirmed as such this session — **investigate what this actually is
before touching any of the same files**, since M5's own scope
(`supervisor/internal/server/handlers.go` and diagnostic output) is shared
code that could conflict. Do not discard or commit that work without
understanding it first; ask the owner if its status is unclear.

Do not redesign the profile model, the port contract, the sync engine, the
browser-equivalent test pattern, or the tool-pack launch mechanism. Extend
them only where a genuine defect demands it, per the M2-M4 precedent (real
hardware testing found 6-11 real bugs each round — M5 should expect the
same discipline, not assume this phase is purely additive).

## Scope — build exactly this

Per `docs/macos-arm64-plan.md` §M5:

**Goal:** make the product report the measured execution environment
honestly — an Apple-Silicon-launched IOL lab currently advertises `i386`
support it cannot actually provide (Rosetta translates `amd64`, not
`i386`), and this needs to stop being advertised without breaking or
relabeling anything that's actually true.

**Files touched:** `supervisor/internal/server/handlers.go` and its tests,
for a new explicit i386-disable capability/configuration; native
service/provisioning configuration; launcher status/diagnostic output;
install/release documentation. **Do not alter the existing `arch` field's
meaning** — this is additive (a new signal), not a rename/repurpose of
something that already exists.

**Observable acceptance** (all must be shown on real hardware, not just
unit-tested — see M2-M4's own experience of what static review alone
misses):
- The Mac-launched `hello` response **omits** `i386`.
- Ordinary existing amd64 (non-Mac) targets **still advertise** `i386` —
  this must not regress any existing non-Mac deployment target (LXC,
  VMware, native/cloud Linux, WSL, OVA all share this code path; M4's own
  session found and fixed a bug in exactly this kind of shared-code blast
  radius, so treat "does this change anything for a non-Mac target" as a
  question to actively verify, not assume away).
- Mac diagnostics report `guest_arch=aarch64`, `execution=rosetta-amd64`,
  the actual guest kernel version, and the structural Rosetta canary
  result.
- The GUI never offers i386 IOL images as supported on an Apple Silicon
  target.
- An ordinary x86_64 IOL lab is completely unaffected by any of this.

**Dependencies:** M1 defines the guest marker/configuration this builds on.
Reconcile `luna/macos-arm64-invariant` first (see above).

**Estimate:** 0.5-1 focused day, per the plan — but M2-M4 each ran longer
than their own estimates once real hardware surfaced defects the estimate
didn't anticipate. Budget accordingly; do not treat the estimate as a
deadline that justifies skipping hardware verification.

## Working rules (same as M2-M4)

- Real hardware or it didn't happen. A capability check that only runs in
  a unit test with a mocked environment does not satisfy this milestone's
  acceptance criteria — M4's own session repeatedly found that fixes which
  passed cleanly in `go test` still failed the first time they actually
  ran against the Mac.
- Never `git add -A` or `git add .` — stage the exact file list by path,
  every time, in every worktree. M3's session found a genuine case of
  concurrent unrelated work sitting in the same worktree; assume this can
  recur.
- Write `docs/macos-m5-result.md` and `docs/macos-m5-handoff.md` at the
  end, using `docs/macos-m4-result.md`/`docs/macos-m4-handoff.md` as the
  format template. Same honesty bar: state plainly what ran on real
  hardware versus what only compiled or was unit-tested, per acceptance
  criterion, with no rounding up — M4's own result doc exists specifically
  because an earlier draft overstated what had actually been verified.
