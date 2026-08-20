# M7 Phase 6 continuation prompt (this session)

Self-contained. Start **M7 Phase 6** of the Apple Silicon macOS track for
iolbox — same-machine Rosetta vs. native A/B metrics. Phase 5 is CLOSED
(exit criterion met: every plan-section-11 matrix row PASS or legitimate
`NOT_EXERCISABLE`, four-node post-soak capacity owner-waived 2026-08-20),
HEAD `2b6939f` on `luna/macos-m7-phase4-integration` in
`J:\Claude code\iolab-m7-phase4-wt`, working tree clean. Use **sol at
medium** only for planning/adversarial review of anything that redefines
what a gate/contract proves, and a **direct Sonnet Agent (not codex/CLI)**
for hardware execution.

**Owner ruling received at the start of this session**: the plan's default
Phase 6 thresholds (below) are approved as-is. They may not be relaxed
after results are seen without a new recorded decision and a repeated
promotion review — do not ask again mid-collection, and do not
self-relax a threshold because a result is close.

## Read first, in this order

1. **`docs/macos-m7-phase5-handoff.md`** in this worktree — full current
   status, the owner waiver, and why Phase 6 is next.
2. **`docs/macos-m7-plan.md`** section 12 (Phase 6) in
   `J:\Claude code\iolab-m7-wt` (the plan doc lives in the Phase 3
   reference worktree, not this one) — the authoritative spec, metric
   list, and thresholds. Quoted below for convenience but the doc is
   authoritative if there is ever a discrepancy.
3. **`docs/m7-phase4-file-mapping.md`** in this worktree — what the
   combined native-candidate artifact actually contains.
4. `docs/macos-release.md` and any release-tag/CHANGELOG evidence needed
   to positively identify the **exact shipped Rosetta artifact** (the
   currently-released, pre-M7 amd64/Rosetta-only build) — do not guess a
   tarball name; confirm it via the actual release process/tag.

## Plan section 12, verbatim (for convenience — the plan doc is authoritative)

> On the same physical Mac, rerun the exact shipped Rosetta artifact and
> combined native candidate with identical macOS/Lima, vCPU/RAM, image
> hash, labs, capture, and cache conditions. For two-node and four-node
> cases collect at least three runs, retaining every raw value, median,
> and worst case:
>
> - cold VM start to successful `GET /` and lab start to all usable prompts;
> - traffic counts, loss, and latency;
> - host CPU, guest load, and translator CPU at idle/during traffic;
> - per-IOL/VPCS/supervisor RSS, guest used/peak memory and swap;
> - Mac memory pressure and used/peak memory;
> - teardown duration and stale-resource count;
> - translator crashes, SIGILL/SIGSYS, and unexpected exits.
>
> Default thresholds (**owner-approved this session, fixed for the
> duration of collection**):
>
> - any new functional failure, crash, SIGILL/SIGSYS, stale resource, or
>   Rosetta dependency (in the native candidate) is unacceptable;
> - median VM or lab boot more than 25% slower than baseline is unacceptable;
> - median steady-state CPU, guest/host memory, or aggregate node RSS more
>   than 25% worse is unacceptable;
> - worse Mac memory-pressure state, sustained swap, or inability to run
>   the approved four-node tier is unacceptable;
> - packet loss more than one percentage point worse, failure of the
>   functional ping target, or any traffic-soak interruption is
>   unacceptable.
>
> Every completed run/result to which an approved threshold applies
> receives PASS or FAIL immediately. Any completed run exceeding its
> approved threshold is FAIL and forces NO-PROMOTE; it cannot be
> reclassified as HOLD or "insufficient sample." A missing metric is
> UNEVALUATED, never zero.
>
> **Exit:** raw runs, summaries, deltas, and threshold verdicts exist for
> every metric/topology. Include one defect/fix/rerun cycle if the first
> collection is invalid due to a product or measurement defect; a genuine
> threshold failure remains failure.

## Where things actually stand (do not re-derive this, it's already true)

- Phase 5 is fully closed. Every matrix row is PASS or legitimate
  `NOT_EXERCISABLE`, except four-node capacity in the post-soak position,
  which is owner-waived (not fixed) — see the Phase 5 handoff for the
  exact caveat. If Phase 6's four-node A/B runs happen to combine
  soak-like traffic immediately before a four-node topology on this same
  Mac, that constraint may resurface; do not assume it's fixed.
- **Same worktree, same branch** — no fresh worktree for Phase 6, stay on
  `luna/macos-m7-phase4-integration` in `J:\Claude code\iolab-m7-phase4-wt`.
- Nothing needs committing right now — clean at `2b6939f`.

## Your job this session

1. Re-verify the physical Mac is reachable at the expected IP with the
   known host key before trusting it (see hardware access below; DHCP and
   sleep have both bitten before).
2. Positively identify the two exact artifacts under test: the shipped
   Rosetta artifact (release tag/build, not a guess) and the combined
   native candidate (this worktree's HEAD build with
   `--profile native-arm64` available). Record both identities (hash/tag)
   in the evidence before collecting a single metric.
3. Establish identical conditions for both arms: same macOS/Lima version,
   same vCPU/RAM allocation, same image hash, same labs, same capture
   setup, same cache state (cold vs warm — pick one and hold it constant,
   record which). Write this down before running anything so both arms
   are provably comparable.
4. Collect at least 3 runs per topology (two-node, four-node) per arm,
   recording every raw value plus median and worst case for every metric
   in the list above. Missing metrics are `UNEVALUATED`, never zero or
   omitted silently.
5. Score every metric/topology/arm combination against the approved
   thresholds immediately as it completes — PASS or FAIL, no deferred
   judgment calls.
6. If a run is invalidated by a genuine product or measurement defect
   (not just an unfavorable result), you get one defect/fix/rerun cycle
   for that specific run — reproduce the defect independently first, fix
   it with the smallest owning change, rerun. A genuine threshold failure
   is not a defect and does not get a rerun to make it pass.
7. Produce the exit deliverable: raw runs, summaries, deltas, and
   threshold verdicts for every metric/topology/arm — this is what Phase
   7's mechanical promotion decision (plan section 13) will consume
   directly, so make it literal and complete rather than narrative.
8. Write a Phase 6 handoff doc (`docs/macos-m7-phase6-handoff.md`) at
   session end following the Phase 4/5 handoff format: status at a
   glance, full verdict table, defects found/fixed, hardware access
   notes, next session's actual job.

## Working pattern (read before starting — unchanged from Phase 4/5)

- **Direct Sonnet Agent execution beats codex/CLI indirection for
  hardware work.** Reserve sol-medium for planning/adversarial review of
  changes that redefine what a gate/contract proves, not every
  self-evidently-correct fix. Match rigor to actual stakes.
- **When you do use sol-medium, always pass `-m gpt-5.6-sol` explicitly**
  — the CLI's configured default model is `gpt-5.6-luna`, omitting `-m`
  silently runs the wrong model.
- **Critical process rule, stated because it cost real time repeatedly
  across this whole project**: any agent that starts a long-running or
  background command must poll it to completion itself — never end a
  turn assuming a passive notification will arrive. Actively invoking
  `Monitor`/`TaskOutput` with `block: true` on a real background process
  is legitimate polling, not a stall.
- SSH: `rohansharma@192.168.101.186`, key
  `J:\Claude code\iolab-m7-wt\.m7-ssh\iolbox_mac_m0` (lives in the Phase 3
  reference worktree, not this one) — verify the host key via
  `ssh-keyscan` before trusting a possibly-stale IP (known good key:
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
  — DHCP has moved this before, and the Mac has gone fully
  unreachable/asleep before too; no remote-wake mechanism, ask the owner).
  `limactl` is at `/opt/homebrew/bin/limactl`. `limactl delete`/`stop` and
  similar need `--tty=false` and `< /dev/null` to avoid hanging on stdin
  over non-tty SSH.
- The Mac is the owner's actively-used laptop — check `vm_stat`/running
  processes before starting VMs, stop VMs at real stopping points. Never
  touch `iolbox-m5-e2e` or `iolbox-m7-native-arm64-qemu`. Phase 5 also
  found there are no longer any of the five witness VMs
  `hardware-m4.sh`/`hardware-m4-phase5.sh` expect for RAM-reclaim — if
  Phase 6 needs a clean-memory baseline for a run, get it by stopping/not
  starting unrelated VMs, not by inventing new witness VMs under old
  names.
- Commit real progress at each meaningful checkpoint with clear, honest
  commit messages. Stage exact paths only, never `git add -A`/`git add .`.
- If you find a real bug, reproduce it independently before fixing it.
- Because this phase's numbers directly drive a NO-PROMOTE/PROMOTE gate,
  do not round a borderline result in either direction — report the exact
  measured value and let the fixed threshold decide it.
