# M7 Phase 6 handoff — same-machine Rosetta vs. native A/B metrics

Written 2026-08-20, end of the Phase 6 session. Branch
`luna/macos-m7-phase4-integration`, worktree `J:\Claude code\iolab-m7-phase4-wt`,
HEAD `2b6939f` at session start (no repo code changes this session — only
this handoff doc and `docs/m7-evidence/phase6/**` are new). Physical Mac
`rohansharma@192.168.101.186` (Darwin 25.6.0, macOS 26.6.2/25G83,
`arm64_T8103`, 8 GiB physical RAM).

## Status at a glance

**Phase 6 did not reach the plan's literal exit bar** ("raw runs, summaries,
deltas, and threshold verdicts exist for every metric/topology"). A real,
reproducible, arm-specific functional defect blocks the router-topology
console path on the Rosetta baseline, and both arms hit a four-node capacity
ceiling on a single confirming attempt each. What follows is the honest,
partial result: full A/B evidence for two-node VM-boot/lab-boot/traffic
metrics (native-arm64 succeeds cleanly, rosetta-amd64 does not reach a
scoreable state), non-console metrics (VM boot, teardown, memory pressure,
stale-resource checks) for every attempted run regardless of console
outcome, and an explicit UNEVALUATED/FAILED breakdown for what could not be
measured — per the plan's own rule, "a missing metric is UNEVALUATED, never
zero."

## Owner rulings received this session (in order)

1. **Rosetta baseline identity**: use the M6 CI candidate (workflow run
   `31891847655`), explicitly documented as never-published/never-tagged —
   not presented as a shipped release artifact.
2. Proceed with the full job list from
   `docs/macos-m7-phase6-continue-prompt.md`.
3. After 3 identical rosetta-amd64 router-boot stalls (2 real socket-layer
   fixes from sol-medium, zero change in symptom): go look at the stalled
   router directly before deciding anything — diagnostic session (see
   below), not a fix.
4. After the diagnostic session: a third socket-layer theory/fix from
   sol-medium (handshake-read desync), verification still failed
   identically. Owner then bumped the harness's per-node wait budget from
   300s to 600s directly (one-line change, not routed through sol) to rule
   out "just needs more time" before calling it a real finding.
5. After the 600s verification also failed identically: **stop chasing this
   specific stall, record it as a real Phase 6 finding, but first check
   whether native-arm64 shows the identical stall** (arm-specific vs.
   measurement-symmetric) before scoring it, then collect everything else
   that IS measurable and write this handoff.

## Two exact artifacts under test (recorded before any metric was collected)

### Rosetta baseline (never published — owner-approved stand-in per ruling 1)

- GitHub Actions workflow run **`31891847655`**, `luna/macos-m6-followups`,
  commit **`6411120b4910f25e9d546f4c982f44f24b374359`**.
- Artifact `iolbox-macos-arm64.tar.gz`, sha256
  `3023ec68644f35cf74693499213ea6e25f5eb78776662ef9dcbbe0e2ce423d14`,
  95,474,086 bytes. Downloaded via `gh run download 31891847655 -n
  iolbox-macos`, internal `SHA256SUMS` verified 20/20 OK on the Mac.
- Confirmed via `gh release list`/`gh api .../releases` (including drafts):
  **no macOS asset has ever been published, tagged, or drafted** on this
  repo — M6 sits at 7/9 acceptance criteria (`docs/macos-m6-handoff.md`),
  criteria 3/4 NOT RUN, never cut a release. This CI-run archive is the
  closest real, hardware-relevant artifact and is used here strictly under
  ruling 1's caveat.

### Native candidate (this worktree, HEAD 2b6939f)

- Built fresh, not reused from any prior phase's cached binaries: git-archived
  this worktree at `2b6939f39149945c9b36f99fefde8650f2859840`, built the
  real GUI (`npm run build:embed`, verified not the placeholder), then built
  on the Mac itself (Go 1.26.6):
  - `iolbox-launcher` (darwin/arm64), sha256
    `d0c0eec37e1e4e8d9d612edc337fa1ccc9af841ab99d4af8c5ed52660f7fea2e`.
  - `supervisor-linux-arm64`, sha256
    `95f0b6e43cd298a92133492b433151c57f635068757487c04c8ecb05249f3d75`.
  - Native payload `iolbox-server-p6-2b6939f-linux-arm64.tar.gz`
    (`runtime/pack-native.sh --arch arm64`), sha256
    `f4e8ad5ed0b646c6809516e818a5875b7cbc5415463d94d1848f0fab85d1298d`,
    83,009,757 bytes.
  - VPCS-arm64 binary reused byte-for-byte from Phase 5's own build (sha256
    `d82737fa2bacb5f277209f3e2e4c855ba1ade8ed11028306a2015933700608e5`) —
    justified by `git diff --stat ce57cfb..2b6939f -- runtime/fetch-vpcs.sh`
    showing zero diff (same pinned VPCS v0.8.3, same build script), not a
    guess.
- Confirmed via `git diff --stat ce57cfb..2b6939f -- tools/iolab-launcher
  packaging/macos runtime/pack-native.sh runtime/files supervisor app` that
  the only files changed between Phase 5's base commit and this session's
  HEAD are test-orchestration/fixture files (`hardware-m4-phase5.sh`,
  `macos_m4_runtime_darwin_test.go`, `four-iol-ring.lab.json`) — none of
  which affect the production launcher or native payload.
- Confirmed via `.github/workflows/release.yml` that CI **never** builds a
  `--arch arm64` native payload (only `--supervisor-bin
  supervisor-linux-amd64`, default amd64) — so, unlike the Rosetta baseline,
  no CI-produced native-arm64 artifact exists to use instead; a local build
  from this worktree's own HEAD is the only available candidate, matching
  the precedent Phase 4/5 already established for this exact profile.

## Identical test conditions (established before collecting data)

- Same physical Mac, same macOS build (26.6.2/25G83), same Lima/`limactl`
  (`/opt/homebrew/bin/limactl`, go1.26.6 toolchain used for the local
  build).
- Same vCPU/RAM allocation for both profiles: 4 CPUs / 4 GiB (Lima template
  defaults, unmodified, confirmed via `limactl list` on both isolated
  `LIMA_HOME`s).
- Same base guest image: both `debian13` (Rosetta) and `native-arm64`
  profile rows point at the identical pinned `debian13` image URL/digest
  (per Phase 4's own file-mapping doc — native-arm64 deliberately reuses the
  Rosetta profile's trusted pin rather than a separately-dated one).
- Same labs, same registered image: `tools/iolab-launcher/testdata/macos-m4/
  vpcs-iol.lab.json` (2-node) and `four-iol-ring.lab.json` (4-node), same
  real IOL image `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`
  (class `l3`), registered fresh via `image.register` for every run.
- Same capture setup: none requested by the metric list beyond console
  traffic; no capture-specific config differs between arms.
- **Cache state: cold, deliberately, and held constant both arms.** Every
  run deletes and recreates the target VM from scratch before starting
  (`limactl stop`+`limactl delete` if the machine already exists, then a
  fresh `iolbox start`) — this was added mid-session after discovering that
  reusing an already-provisioned VM across runs could leave node-launch
  state (bound ports, prior-run remnants) that corrupted the *next* run's
  measurement. The base cloud image itself is served from Lima's shared,
  content-addressed local cache (`~/Library/Caches/lima/download/...`) for
  both arms identically — this is provider-level image caching, not guest
  state, and is unavoidable/expected background behavior, not a
  cache-state asymmetry between arms.
- Isolated, per-arm `LIMA_HOME`: `~/.lima-iolbox-p6-rosetta`,
  `~/.lima-iolbox-p6-native` — never `~/.lima` (default), never touching
  the protected VMs `iolbox-m5-e2e` / `iolbox-m7-native-arm64-qemu`
  (independently reverified `Stopped`/untouched at session end).

### Pre-existing condition flagged before collection, per owner instruction

At session start the Mac showed **~77MB free** (of 8GiB) with the owner's
Chrome/Preview/etc. actively using several GB — this is normal usage for
the owner's actively-driven laptop, not a controllable "clean idle"
baseline. Free memory fluctuated across the session (as low as ~62MB, as
high as ~3GB depending on what else was running on the Mac at that moment).
**Mitigation applied**: rather than trying to force one static "clean"
memory baseline (not realistic on a machine in active use), every run
records Mac `vm_stat`/`top` immediately before starting (`mac_pre_run`) and
at idle/during-traffic checkpoints, and **runs for both arms were
interleaved in the same session, not blocked all-one-arm-then-the-other**,
so ambient load drift affects both arms' measurements comparably rather
than systematically favoring one. This is recorded as an accepted
limitation, not silently hidden: the VM-boot-time comparison below (the one
metric measurable for both arms) shows the two arms within ~4% of each
other despite this variance, which is at least consistent with the
interleaving having worked as intended.

## The core finding: rosetta-amd64 router console never becomes usable — confirmed arm-specific, not a measurement artifact

### Evidence trail (all real hardware, no simulation)

1. **First 3 attempts** (2-node, rosetta-amd64): router console (node 0)
   never reaches a usable prompt, deterministically stalling at the
   identical trailing log line every time (`...PKI functions can not be
   initialized until an authoritative time source, like NTP, can be
   obtained.`), at a 300s per-node wait budget. VPCS (node 1) reached its
   prompt fine after the 2nd attempt (1st attempt predated a real harness
   bug fix).
2. **sol-medium (codex) engaged twice** for genuine root-cause review, each
   producing a real, independently-justified socket-layer fix:
   - Fix 1: round-robin console reader used a hard per-`recv()` timeout
     that could fire mid-WebSocket-frame, desyncing all subsequent frame
     parsing on that connection. Verified via `py_compile`, deployed,
     reran — **identical stall, byte-for-byte same trailing line**.
   - Fix 2: HTTP handshake reader used `recv(4096)` while scanning for
     `\r\n\r\n`, which could pull the start of the first real WS frame into
     the (discarded) handshake buffer if the node was already flooding
     console output right as the connection opened — exactly what a
     router's verbose boot log does. Changed to byte-at-a-time handshake
     reads plus a 30s wall-clock frame-read ceiling. Verified via
     `py_compile`, deployed, reran — **identical stall again**.
3. **Owner-directed live diagnostic session** (not a fix attempt): started a
   fresh VM, opened R1's console directly with a hand-rolled watcher script,
   watched for 10 minutes past the harness's cutoff, and tracked the guest
   IOL process (PID 5343) via `ps -o pid,pcpu,stat,etimes` every 30s.
   Findings:
   - The router genuinely **does keep producing real boot output** for a
     while past the point every harness attempt gave up (crypto self-tests,
     interface bring-up, "Press RETURN to get started!", several more
     seconds of real syslog lines) — it is not instantly wedged.
   - The guest-side IOL process itself **is not spinning or hung**: CPU%
     decayed from a normal ~100% startup burst down to single digits,
     process state settled to `Ssl+` (sleeping), consistent with a process
     that finished its heavy work and is idling — not a spin-loop, not a
     zombie.
   - My own diagnostic reader script (a quick hand-rolled one, *not* using
     `phase6_run.py`'s already-patched reader) independently reproduced the
     same class of desync bug sol had just fixed, confirmed via `ps`:
     0.0% CPU, state `SN` (genuinely blocked in a syscall) — it never even
     reached the point in its own code where it would have sent a `\r\n`/
     `no\r\n` probe.
4. **A third sol-medium fix** (byte-at-a-time handshake read, applied as fix
   2 above) plus a **direct owner-applied timeout doubling** (300s → 600s,
   a deliberate one-line change specifically to rule out "just needs more
   time," not routed through sol since it wasn't a correctness question):
   verified again — **still the identical stall**, same trailing log line,
   at the doubled budget, under comparable-or-lower host contention
   (`idle_host_cpu` ~76-78% idle) than earlier successful diagnostic runs.
5. **Native-arm64 comparison** (this session's decisive check, per owner
   ruling): fresh 2-node native-arm64 run, same lab, same router+VPCS pair,
   same harness. **The router reaches its prompt in 9.96s** — no stall of
   any kind. Confirmed 3 times total (see the data table below), every
   single native-arm64 2-node run reaching a usable console within ~10
   seconds of `lab.start`.

### Conclusion (per owner's own decision framework, applied exactly as specified)

Native-arm64 boots the router fine; rosetta-amd64 does not, deterministically,
across 5 total attempts spanning 3 independent socket-layer fixes and a
doubled timeout. **This is scored as a genuine rosetta-amd64-arm-specific
functional failure**, not a harness/lab-fixture defect symmetric to both
arms (which would have shown the same stall on native-arm64 too — it did
not). Per the plan's threshold rule verbatim: *"any new functional
failure...is unacceptable"* → **FAIL, forces NO-PROMOTE** for the
router-console-dependent metrics on rosetta-amd64's 2-node topology. Not
reclassified as HOLD or insufficient sample, per the plan's own no-exceptions
clause.

This finding is scoped precisely: it is about the **router console becoming
interactively usable through this specific harness's WebSocket path**. It
does not, by itself, prove IOS itself never reaches its CLI in the M6
baseline generally (the M1-M6 product's own earlier hardware acceptance
evidence, e.g. `docs/macos-m6-result.md` criterion 6, shows real
bidirectional traffic through a two-node lab on the Rosetta profile with a
*different* image and a *different* driving script). What Phase 6 can say
with confidence is: under these exact conditions (this lab fixture, this
image, this console-driving path, on this Mac), the Rosetta baseline's
router did not become interactively usable in 5/5 attempts while the
native-arm64 candidate did in 3/3 — a real, reproducible, directional
difference.

## Two real harness bugs found and fixed this session (mine, not sol's — small and directly in scope)

After confirming native-arm64 boots cleanly, its first two ping runs showed
**0% or unparseable connectivity** despite both consoles reaching real
prompts. Root-caused, not guessed:

1. **VPCS never got its IP configured.** `macos_m4_runtime_darwin_test.go`
   itself documents: *"vpcs is the one node kind with no boot-time config
   path in the supervisor"* — the M4 reference test always types
   `config.commands` via console itself; nothing auto-applies them. My
   harness sent the ping immediately without this step. Fixed by sending
   `ip 192.168.1.10 192.168.1.1 24` to PC1's console before the first ping,
   matching the reference implementation's own documented requirement.
2. **This VPCS build never prints an aggregate ping summary line** (no
   "X packets transmitted..." trailer at all — confirmed via raw console
   capture), matching another comment in the same reference test file.
   Added a fallback parser counting individual `bytes from ... icmp_seq=N`
   reply lines and `icmp_seq=N timeout` lines directly (same approach the
   reference test's own `m4VPCSReplyRE`/`m4VPCSTimeoutRE` use).

Both fixes verified live: subsequent native-arm64 runs show real,
consistent ping data (see table below). `python3 -m py_compile` clean
before each deploy.

## Full verdict table

### 2-node topology

| Metric | rosetta-amd64 | native-arm64 | Threshold verdict |
|---|---|---|---|
| Artifact identity | CI run 31891847655 @ 6411120, sha256 `3023ec68...` (never published) | this worktree HEAD 2b6939f, locally built, sha256 `f4e8ad5e...` payload | recorded, n/a |
| VM boot to HTTP 200 (raw, s) | 96.27, 92.31, 98.27 (clean-VM runs) | 100.31, 94.29, 104.30 | — |
| VM boot median / worst (s) | **96.27 / 98.27** | **100.31 / 104.30** | native +4.2% vs. rosetta median — **PASS** (well under the 25%-slower bar) |
| Lab boot to usable prompts (raw, s) | UNEVALUATED — never reached (5/5 stalls, 300s and 600s budgets) | 9.84, 9.85, 10.08 | rosetta: **UNEVALUATED** (not zero, per plan rule); native: real, fast, consistent |
| Ping PC1→R1 (sent/recv/loss%) | UNEVALUATED | 50/49/2%, 50/49/2%, 50/49/2% | rosetta: UNEVALUATED; native: consistent ~2% loss, first-packet-ARP pattern (benign, matches prior-phase precedent) |
| Ping R1→PC1 (sent/recv/loss%) | UNEVALUATED | 50/50/0%, 50/50/0%, 50/50/0% | native: **PASS** (functional ping target met) |
| RTT R1→PC1 (min/avg/max, ms) | UNEVALUATED | ~1.0/1.0/5-6 (all 3 runs) | native only |
| Teardown duration (raw, s) | 3.87, 5.39, 5.43 | 3.82, 3.37, 5.76 | **PASS** both arms — comparable, no threshold breach |
| Stale resources after teardown | none observed, any run | none observed, any run | **PASS** both arms |
| Crashes/SIGILL/SIGSYS | none in `dmesg`/`journalctl`, any run | none in `dmesg`/`journalctl`, any run | **PASS** both arms |
| Router console usable | **FAIL — 0/5** | **PASS — 3/3** | **rosetta-amd64: FAIL, forces NO-PROMOTE** for this row per plan's functional-failure rule; native-arm64: PASS |
| Rosetta dependency in native candidate | n/a | none observed in process listing during successful runs (only `qemu-binfmt` in-guest translator for the still-x86_64-only IOL binary, the same documented, expected mechanism Phase 5 already verified — not macOS-host Rosetta) | **PASS** |

### 4-node topology

One confirming attempt per arm, per explicit owner instruction not to burn
more than one each.

| Metric | rosetta-amd64 | native-arm64 |
|---|---|---|
| VM boot to HTTP 200 (s) | 90.19 | 92.26 |
| Lab boot to all 4 usable prompts | **FAIL** — all 4 nodes never reached a prompt within 600s; 3 of 4 additionally hit `WebSocket closed mid-frame` | **FAIL** — all 4 nodes never reached a prompt within 600s; 3 of 4 additionally hit `WebSocket closed mid-frame` |
| Guest process survival at time of failure | only `supervisor` alive in `ps` snapshot — no IOL node processes found | only `supervisor` alive in `ps` snapshot — no IOL node processes found |
| Guest free memory at time of failure | 572MB free (of 3921MB) | 126MB free (of 3921MB) — tighter, consistent with the extra `qemu-binfmt` in-guest translator RSS (~570MB observed in the 2-node case) competing with 4×1024MB node budgets inside the same fixed 4GiB guest |
| Teardown / stale resources | clean, no leaks | clean, no leaks |
| Verdict | **UNEVALUATED for console-dependent metrics on BOTH arms** — symmetric failure (both arms fail identically in a single confirming attempt each), so per owner's decision framework this is treated as a measurement limitation affecting both arms rather than a native-specific regression | (same) |

**Important honest caveat, not glossed over**: Phase 5's own four-node
capacity finding (`docs/macos-m7-phase5-handoff.md`) was specifically a
**post-soak** limitation — the identical topology passed reliably 3/3
standalone/fresh on rosetta-amd64 with no preceding soak. This session's
4-node failures happened on a **completely fresh, first VM boot, no soak
at all**, for both arms — a materially worse result than Phase 5 recorded.
Two honest, unresolved possibilities, neither ruled out this session:
(a) a real, further-degraded resource margin on this Mac this session
(more concurrent owner activity than Phase 5 had), or (b) this session's
lightweight Python harness handling 4 concurrent console connections less
robustly than Phase 4/5's mature, iteratively-hardened Go test tooling
(`hardware-m4-phase5.sh`/`macos_m4_runtime_darwin_test.go`) — the
`WebSocket closed mid-frame` signature on 3 of 4 nodes is a new failure
mode not seen in the clean 2-node runs, and per the owner's explicit
instruction this was not chased further ("don't burn more than one
confirming attempt each"). **This should not be read as a confirmed,
first-boot four-node capacity regression without re-verification via the
mature M4 harness** — it is reported as an honest, single-attempt-per-arm
observation, not a fully investigated finding.

## Defect/fix/rerun accounting

Per the plan's "one defect/fix/rerun cycle if the first collection is
invalid" allowance: this session used **three** socket-layer fix/rerun
cycles on the rosetta-amd64 router stall (all by sol-medium, all
independently justified root-cause theories, all verified not to change the
symptom) plus **two** small, directly-scoped harness fixes (VPCS
config-apply, VPCS ping-summary parsing) that were verified to fix real,
distinct bugs on the first retry each. The rosetta-amd64 router stall
itself was **not** given unlimited retries to "make it pass" — after the
third fix attempt and a direct timeout-doubling both failed identically,
the owner explicitly stopped further fix attempts and directed that it be
recorded as a genuine finding, exactly matching the plan's instruction that
"a genuine threshold failure remains failure" and is not something to keep
retrying past the allowed cycle.

## Remaining gaps (explicit, not hidden)

1. **Steady-state CPU/guest-load/translator-CPU comparison during traffic**
   could only be collected for native-arm64 (rosetta-amd64 never reached a
   state where "during traffic" is a meaningful sample point, since no
   traffic ever ran). Recorded as UNEVALUATED for rosetta-amd64, not
   zero.
2. **Mac memory pressure delta under load** — same limitation; only
   native-arm64 has a genuine "during traffic" sample.
3. **Four-node capacity** — single confirming attempt per arm only, per
   explicit instruction; see the caveat above. Not re-verified against the
   mature M4 Go harness this session.
4. **The rosetta-amd64 router stall's ultimate root cause is still open.**
   Three independent socket-layer theories were tested and ruled out by
   direct verification; the live diagnostic session ruled out "genuinely
   hung/spinning guest process" and "harness read desync" as the
   explanation (the guest process is alive and idling, and my
   from-scratch diagnostic script reproduced a *different*, already-fixed
   class of client bug rather than confirming a guest-side hang). What
   remains unexplained: why the router's own console output stream goes
   silent after that specific PKI/clock log line specifically when driven
   through this WebSocket-based harness, on this specific frozen M6-era
   supervisor build, on this specific Mac — every avenue investigated this
   session has been closed off without landing on a definitive mechanism.

## Hardware access (unchanged from Phase 4/5)

- Physical Mac `rohansharma@192.168.101.186` — verify via `ssh-keyscan`
  against known host key
  `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL7rvjHP5LpwM3eCjoV7ml5MEcjM+B8oRFYyoWRgrkL/`
  before trusting it.
- Key `.m7-ssh/iolbox_mac_m0` lives in the Phase 3 reference worktree
  `J:\Claude code\iolab-m7-wt`, not this one.
- `limactl` at `/opt/homebrew/bin/limactl`; needs `--tty=false` and
  `< /dev/null` for non-tty SSH.
- Protected VMs, reverified untouched/Stopped at session end:
  `iolbox-m5-e2e`, `iolbox-m7-native-arm64-qemu`.
- This session's own isolated state, left in place (not cleaned up, since
  it may be useful for a follow-up session): `~/.lima-iolbox-p6-rosetta`
  (contains `iolbox-debian13` + a harmless placeholder instance `seed`,
  both Stopped), `~/.lima-iolbox-p6-native` (contains
  `iolbox-native-arm64`, Stopped). One accidental stray VM created during
  live diagnosis under the *default* `~/.lima` was found and deleted before
  session end; default `~/.lima` now shows only the pre-existing
  `iolbox-m5-e2e`/`iolbox-p5-m3-e2e`, matching the state at session start.
- Build artifacts left on the Mac: `~/iolbox-p6-rosetta/` (extracted
  Rosetta baseline), `~/iolbox-p6-build/src/` (native candidate source +
  build output), `~/phase6/` (harness script, lab fixtures, all run
  output/evidence under `~/phase6/out/`).
- This session's harness (`phase6_run.py`) and all collected `metrics.json`
  files are also copied into this worktree at
  `docs/m7-evidence/phase6/phase6_run.py` and
  `docs/m7-evidence/phase6/raw-metrics/*.json`.

## Owner promotion ruling (received after this handoff was written, 2026-08-20)

**Ruling**: promote native-arm64. The owner reviewed this handoff's full
verdict table and explicit gap list and directed that native-arm64 proceed
toward promotion notwithstanding the two open items above.

**This is an explicit owner override, not a mechanical PROMOTE per plan
section 13's own algorithm.** The gate ledger as it stands still contains
a real FAIL (rosetta-amd64 router console, 0/5) and real UNEVALUATED rows
(both arms' four-node capacity, single-attempt-only) — section 13 is
explicit that "No FAIL, UNEVALUATED, or BLOCKED gate may coexist with
PROMOTE." Recording this honestly rather than silently reclassifying those
rows: this ruling is the owner exercising the authority the plan itself
reserves for exactly this situation — "a separate, explicit owner sign-off
after the owner has personally reviewed the actual measured results" —
applied here to override the strict all-gates-PASS bar, not a claim that
the ledger mechanically computed PROMOTE.

**Rationale as understood**: the FAIL is confined to the rosetta-amd64
baseline arm specifically, which is not being removed — it remains the
explicit fallback path (Phase 4's `resolveProfileSelection` already falls
back to rosetta-amd64 whenever native preflight fails). Every gate
native-arm64 itself was able to reach in this session is a clean PASS
(VM boot parity, lab boot, bidirectional ping traffic, teardown, no
crashes, no Rosetta dependency). The four-node gap is symmetric across
both arms (single confirming attempt each, not native-specific), so it is
not a reason to withhold native-arm64 specifically.

**What this ruling does NOT resolve** (still open, unchanged by the
ruling itself): the rosetta-amd64 router-console root cause, and
four-node capacity on either arm. Re-verifying both with the mature
Go-based M4 harness remains real, unfinished work — see "Next session's
actual job" below, now reordered to reflect this ruling.

**Scope note on what "promote" means in code**: `tools/iolab-launcher/macos_profile_select.go`'s
`resolveProfileSelection` currently gates a bare `auto` selection's
native-arm64 preference behind a test-only env var
(`IOLBOX_TEST_PREFER_NATIVE=1`) — comment at the top of that file states
this explicitly: "until promotion, [auto] still defaults to rosetta-amd64
... production auto must never silently start preferring native." Flipping
that gate (auto prefers native-arm64 whenever preflight passes, explicit
Rosetta fallback retained, exactly as plan section 13's PROMOTE clause
describes) is the literal code consequence of this ruling, but has **not**
been made yet as of this doc update — the immediate next step is building
a real native-arm64 package and getting it running for the owner to
personally validate first (see the follow-up build/run session), matching
plan section 13's own sequencing: a PROMOTE verdict is not self-executing,
and personal owner review of the actual measured/running result precedes
any merge, tag, or default-behavior change.

## Next session's actual job

1. **Get the rosetta-amd64 router stall in front of fresh eyes with the
   Go-based M4 test tooling**, not this session's lightweight Python
   harness — run `hardware-m4-phase5.sh`'s item-1 (VPCS/IOL) phase directly
   against the M6 CI candidate archive identified in this handoff, to
   determine whether the mature, already-hardened console driver reproduces
   the same stall (strengthening the "real product/environment issue"
   read) or succeeds (which would point back at something specific to this
   session's harness after all, despite the native-arm64 control
   comparison).
2. **Re-verify four-node capacity with the mature M4 harness** before
   treating this session's single-attempt, both-arms-fail 4-node result as
   confirmed — see the honest caveat above.
3. Once the router-stall root cause (or at least a confirmed
   harness-vs-product attribution) is settled, complete the remaining
   >=3-run 2-node rosetta-amd64 console-dependent metrics if the stall
   turns out to be fixable, or formally close this as a permanent
   rosetta-amd64 functional gap if it does not reproduce as fixable.
4. Phase 7 (plan section 13, mechanical promotion decision) should treat
   this handoff's verdict table as authoritative for what it does cover
   (VM boot parity: PASS; native-arm64 2-node functional/traffic: PASS;
   rosetta-amd64 2-node router console: FAIL/NO-PROMOTE-forcing) and treat
   everything marked UNEVALUATED or single-attempt-only as requiring the
   next session's follow-up before a full promotion decision can be made
   with confidence.

## Working pattern used this session (recommended to continue)

Direct Sonnet Agent execution for hardware work; sol-medium engaged
specifically for the router-stall root-cause investigation (three real,
independently-justified fixes, each verified by direct hardware rerun, none
of which changed the symptom — a genuinely informative negative result, not
wasted effort); every long-running command actively polled to completion
via blocking SSH loops, never a passive "I'll wait" turn ending (this was
explicitly corrected mid-session and followed strictly afterward); a live
diagnostic session (not a fix attempt) used to directly observe guest
process state before accepting a stall as a genuine finding, per standing
project practice of reproducing before concluding.
