# M4 handoff — Apple Silicon macOS track

Updated 2026-08-14, end of the M4 session. Read this **before**
`docs/macos-m3-handoff.md` — this supersedes it for anything M4 changed;
M1-M3's own findings are still current and not repeated here.

Branch: **`luna/macos-m4-runtime`**, off `luna/macos-m3-ux`, in worktree
`J:\Claude code\iolab-m4-wt`. **Not merged anywhere.**

---

## 1. Status: PARTIAL, not PASS — read `docs/macos-m4-result.md` in full

Short version: items 1 (VPCS/IOL) and 6 (soak+power) are genuinely proven
on real Apple Silicon hardware. Items 2 and 7 have real fixes that are
unit-tested but not hardware-reconfirmed. Items 3, 4, 5, 8 were never
attempted this session. Eleven real defects were found and fixed (two of
them shared product code, already merged to `main` and hardware-verified
independently of this branch); one more is filed but unresolved. Full
per-item table and defect list in `docs/macos-m4-result.md`.

**Do not treat M4 as closed.** The owner explicitly chose to move on to M5
with this state — that's a scope call the owner is entitled to make, but a
future session should not assume "M4 handoff exists" means "M4 passed."

---

## 2. The tarball-overwrite trap — the single most important thing to know

`hardware-m4.sh`'s (and both sub-scripts') default `--tarball` is the
plain M4-scope build with **no tool packs**. Running the harness without
an explicit override silently reinstalls that plain build over any
fixed/richer build already on the VM. This happened twice this session and
each time looked exactly like a regression until traced back to the
harness's own default. If you're testing anything touching `pc`/`aaa`/
`webserver`/etc. node kinds, pass
`--tarball ~/iolbox-m0/iolbox-server-v0.5.3-netprobe-cgroupfd-fix.tar.gz`
(or a newer one, if it exists — check `~/iolbox-m0/*.tar.gz` and compare
mtimes/hashes) every single time, and re-verify the running supervisor's
version string after any `launcher_start` before trusting pack behavior.

---

## 3. Sub-test scripts, for iterating on one item without the full 8-item run

- `packaging/macos/tests/hardware-m4-startstop.sh` — items covering forced
  launcher kill / forced VM stop / recovery, in isolation.
- `packaging/macos/tests/hardware-m4-power.sh` — item 6 (soak+power audit)
  in isolation, ~11 minutes wall clock.

Both work by sourcing everything in `hardware-m4.sh` *except* its own
trailing `main "$@"` line, so they reuse the exact same
preflight/sentinel/cleanup functions. **If you edit `hardware-m4.sh`,
check whether either sub-script has its own inline copy of the changed
line** — `hardware-m4-power.sh` had a stale inline copy of the
`soak-seal-verify` command that silently kept the old `-args` bug alive
for a full extra ~11-minute run after the shared script was already fixed,
because the sub-script's OWN copy wasn't touched. Prefer having sub-scripts
call into sourced functions rather than inline-copying lines from
`hardware-m4.sh`, if extending this pattern further.

---

## 4. Two shared-code fixes landed on `main`, unrelated to this branch

Both hardware-verified, both affect every deployment target (not
Mac-specific), both found while testing M4's VPCS/tool-pack-adjacent scope:

- `main@e792c32` — native install target was missing all tool-pack
  binaries (netprobe, aaa, webserver, httpclient, syslog, netsvc,
  secbench).
- `main@df24ab1` (merge `4f7643c`) — the native cgroup-placement fallback
  used a path-based `cgroup.procs` write that's invisible once wrapped in
  `ip netns exec` (fresh mount namespace hides the parent's cgroup2 view).
  Fixed to use an inherited fd instead. This is the fix that actually made
  netprobe/AAA/etc. boot; the binaries-shipped fix alone wasn't sufficient.

If a future session sees tool-pack nodes failing with `runtime does not
support ... nodes` again, check `git log main` for whether these two
commits are still present before re-diagnosing from scratch — and check
§2's tarball trap first, since that's the far more likely cause of an
apparent regression.

---

## 5. Environment as it actually is now

See `docs/macos-m4-result.md` §5 for the full table (host, key path,
current supervisor build string, VM inventory). One item worth repeating
here: the SSH key used throughout this session is a workspace-local copy
at `.m4-ssh/iolbox_mac_m0` (gitignored), not `~/.ssh/iolbox_mac_m0`
directly — the sandboxed environment that did most of this work couldn't
read the real path even with directory grants. If working from an
unsandboxed environment, the real `~/.ssh/iolbox_mac_m0` works fine
directly and the workspace-local copy isn't needed.

---

## 6. Process notes carried forward + new ones from this session

- Same `codex exec` stdin-hang and sandbox-write gotchas as before (see
  M3 handoff §7) — unchanged.
- **Never assume a background/detached hardware run's failure is the
  "real" answer without checking whether the harness itself has a bug** —
  of the defects found this session, more were in the test harness
  (`-args` misuse, seal-ordering, regex too narrow, timeout too short,
  console read race) than in the product. Read the actual stdout/stderr of
  a failing `record_command`, not just the top-level `die()` message,
  before concluding a hardware defect.
- A Lima VM's own `lima.yaml` can get rewritten in a different YAML style
  (block sequence instead of flow) by Lima's own tooling after a
  stop/start cycle — a launcher that hand-parses this file (rather than
  using a real YAML library) needs to tolerate both styles for fields it
  itself wrote in only one style at VM-creation time. This bit the
  start/stop recovery test specifically, but would affect any code path
  that re-inspects an already-running VM's port-forward contract.
