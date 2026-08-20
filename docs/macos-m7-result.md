# M7 result — Phase 7 gate ledger and mechanical promotion decision

Written 2026-08-20. Branch `luna/macos-m7-phase4-integration`, worktree
`J:\Claude code\iolab-m7-phase4-wt`, HEAD `4424c9a` at drafting time.

This document exists because `docs/macos-m7-plan.md` section 13 requires it:
"`docs/macos-m7-result.md` must reproduce the ledger and outcome. Each row
links one raw item and states one verdict; no gate may cite only a prose
summary."

The row set is the frozen Phase 0 contract at
`docs/m7-evidence/phase0/gate-ledger.md` (in the `iolab-m7-wt` reference
worktree), recomputed here per section 13's instruction that "Phase 7
recomputes the atomic rows below and does not add a second summary gate for
the same evidence."

---

## 0. Headline

| Question | Answer |
|---|---|
| Mechanical outcome for the ledger as a whole | **NO-PROMOTE** |
| Mechanical outcome for "native-arm64 as the `auto` default" | **Not a plan-defined outcome.** Section 13 defines no arm-scoped verdict. Scored strictly, it is also not PROMOTE. |
| Did a mechanical PROMOTE ever occur? | **No. Not at any point in the M7 arc.** |
| Was native-arm64 promoted anyway? | **Yes — by explicit owner override, on record, landed on the integration branch at `e2ffe34` (not merged to `main`, not tagged, not released).** |
| Row tally (frozen 66-row contract) | 35 PASS · 1 FAIL · 1 BLOCKED · 29 UNEVALUATED |
| Reviewed by | Codex `gpt-5.6-sol`, medium effort — see §7. Its dissent (outcome HOLD, not NO-PROMOTE) is recorded in §5.5. |

These last two lines are different things and are kept different throughout
this document. See section 4.

---

## 1. How verdicts were assigned

Four values only, per the frozen contract: `PASS`, `FAIL`, `UNEVALUATED`,
`BLOCKED`.

The scoring rules actually applied, all quoted from the frozen documents:

1. **"No gate may cite only a prose summary."** (plan §13) A row whose only
   support is a handoff's or `STATUS.md`'s narrative claim is `UNEVALUATED`,
   however confident that narrative is. This single rule is responsible for
   most `UNEVALUATED` rows below.
2. **"A summary or alternate copy cannot replace the named raw file."**
   (gate-ledger preamble) Evidence that exists only on the physical Mac and
   was never copied into the repository cannot close a row.
3. **"A missing metric is `UNEVALUATED`, never zero."**
   (`phase0/threshold-approval.md`, plan §12)
4. **"Every completed run/result to which an approved threshold applies
   receives `PASS` or `FAIL` immediately. Any completed run exceeding an
   approved threshold is `FAIL` and forces `NO-PROMOTE`; it cannot be
   reclassified as `HOLD` or 'insufficient sample.'"**
   (`phase0/threshold-approval.md`)
5. **"Later inconvenience, sample concerns, or a packaging label cannot
   downgrade a failure."** (plan §13) An owner waiver is an *override*
   recorded alongside the row, not a mechanical re-score of it.

### 1.1 The rule stated uniformly (added after adversarial review)

The first draft of this document was faulted by the `gpt-5.6-sol` review for
invoking the strict evidence rule "only when it produces a lower verdict."
That criticism was correct and is fixed here. The rule now applied, in one
direction, to every row:

- **No artifact at all, or prose only → `UNEVALUATED`.** No exceptions.
- **A genuine raw artifact exists but sits at a non-canonical path →
  scored on the artifact**, with the path-binding failure recorded once,
  globally, against P7-01 rather than re-argued per row. See §1.2.
- **An artifact that does not actually demonstrate the row's stated
  claim → `UNEVALUATED`**, even if it demonstrates a neighbouring claim.
  This is what moved P4-08 and P4-09 down in this revision.
- **A completed run that exceeded an approved threshold → `FAIL`**,
  regardless of where its artifact currently lives. See §5.5, which is a
  recorded disagreement with the review.

### 1.2 Path-binding defect (applies to all of Phases 4, 5, 6)

The frozen contract binds each row to a template such as
`phase4/<machine-id>/<run-id>/scenarios/forced-native-success.log`, and states
that "exactly one sanitized machine ID and one UTC run ID are bound to that
template" at the start of the applicable phase.

**That binding was never performed for Phase 4, Phase 5, or Phase 6.** The
artifacts sit at flat, ad-hoc paths (`phase4/scenario1-forced-native-success.log`,
`phase5/m3-rerun/…`, `phase6/raw-metrics/…`).

The review argued this alone should reduce every Phase 4/5 row to
`UNEVALUATED`. **This document declines**, on the ground that the contract's
prohibition is on "a summary or alternate copy" replacing the named raw file —
and these files are neither. They are the primary raw command output, written
once, at a path nobody bound. Downgrading thirteen genuine real-hardware
results on a filing formalism would misrepresent what was measured just as
badly as rounding them up would.

But the defect is real, it is uniform, and it is recorded against **P7-01**.
A reader who prefers the review's stricter reading can apply it mechanically:
it moves Phase 4 to 0 PASS and Phase 5 to 0 PASS, and — importantly — **it does
not change the outcome**, because the outcome is already NO-PROMOTE.

### 1.3 Row universe: 66 frozen rows, plus 8 addendum rows

The review correctly caught that the first draft's 74-row tally silently mixed
two different things. Corrected:

- **The frozen contract contains exactly 66 rows**: P1 (15), P2 (3), P3 (6),
  P4 (15), P5 (12), P6 (12), P7 (3). These are the only rows section 13's
  outcome rules were written against.
- **`P0-01`–`P0-05` are addenda**, added by this document because section 13
  category 1 ("native substrate provenance/attestation") has no rows in the
  frozen contract. That omission is a genuine contract defect, but the remedy
  is to amend and re-freeze the contract, not to mint five retrospective gates
  during Phase 7.
- **`P7R-01`–`P7R-03` are recomputations**, not new gates. They re-measure the
  cells underlying P6-02, P6-08 and P6-11. They must not be counted a second
  time alongside those rows.

§5 therefore tallies the frozen 66 as the authoritative universe and reports
the addenda separately.

### A note on rows 1 and 2

Applying rules 1 and 2 honestly is the most consequential judgement in this
document, and it cuts against work that was genuinely done. Phases 4, 5 and 7
really were executed on real hardware, and their handoffs are detailed and
candid. But the contract's canonical directory layouts
(`phase4/<machine-id>/<run-id>/provenance/…`,
`phase5/<machine-id>/<run-id>/matrix/…/row.log`,
`phase6/<machine-id>/<run-id>/…/thresholds/*.log`) **do not exist in the
repository at all**, and for Phases 5 and 7 the underlying raw artifacts were
left on the Mac. A ledger that scored those rows `PASS` on the strength of the
handoff text would be exactly the "prose summary" closure section 13 forbids.

This is not a claim the work did not happen. It is a claim that **the
repository cannot currently prove it happened**, which is what the gate
contract measures.

---

## 2. The ledger

### 2.-1 Citation base — read this before following any path

Citations below are repository-relative, but **they do not all resolve from the
same worktree**, and that is itself a traceability finding:

- **Phase 0, 1, 2, 3 paths** (`docs/m7-evidence/phase0/…` through `phase3/…`)
  exist **only on branch `luna/macos-m7-arm64`**, checked out at
  `J:\Claude code\iolab-m7-wt`. `git ls-files docs/m7-evidence` on **this**
  branch (`luna/macos-m7-phase4-integration`, where this result document lives)
  returns only `phase4/`, `phase5/`, `phase6/`.
- **Phase 4, 5, 6 paths** exist on this branch.
- **Phase 7 paths** (`docs/m7-evidence/phase7/…`) exist on **no** branch; the
  directory was never created. The Phase 7 raw artifacts are on the Mac only.

So the promotion candidate branch does not carry the evidence for 29 of the
rows it is being promoted on. Nothing is lost — the Phase 0–3 evidence is real
and committed — but it is not present in the line under review, and a reader
checking this ledger against this worktree will find those paths missing. This
is recorded as a defect against P7-01 (ledger integrity) and listed in
section 6.

### 2.0 Phase 0 — native substrate provenance and attestation

Section 13 category 1 is "native substrate provenance/attestation." The frozen
Phase 0 gate-ledger table begins at Phase 1 and contains no rows for this
category. These rows are added here to satisfy category 1; the omission from
the frozen contract is itself flagged in section 6.

| ID | Atomic gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P0-01 | Physical Apple Silicon target identity is attested and frozen | `docs/m7-evidence/phase0/target-attestation.md` | PASS (owner-attested: `Rohans-MacBook-Air.local`, Apple M1 `T8103`, `hw.memsize = 8589934592`, macOS `26.6.1`/`25G76`; SSH host key confirmed identity continuity with the M1–M6 Mac) |
| P0-02 | Approved native arm64 Linux substrate class is attested, and hosted x86_64-emulated-as-arm64 evidence excluded | `docs/m7-evidence/phase0/target-attestation.md` (section "Approved native arm64 Linux substrate") | PASS **with a disclosed deviation**: substrate is a Lima `vz` VM on the *same* physical Mac used for Phase 3+, not an independent arm64 Linux machine. The record states this plainly — "a disclosed deviation from a stronger two-independent-machine design … It is not hidden or treated as independent-host replication." Confirmed not full-system CPU emulation and not a hosted-runner class. |
| P0-03 | Immutable M1–M6 source commit is frozen | `docs/m7-evidence/phase0/m1-m6-source-commit.md` | PASS |
| P0-04 | M3/M4 authoritative inputs, fixtures, hashes, and equivalence mappings are frozen | `docs/m7-evidence/phase0/m3-m4-inputs.md` | **FAIL as a contract condition** *(revised after adversarial review)*. The frozen `four-iol-ring.lab.json` SHA-256 `f25d8e4b…927ee` was replaced mid-Phase-5 with `614a9700…f3161` (commit `2825507`) because the original could not pass its own acceptance bar. Plan §11 permits a replacement **only** "when its Phase 0 owner-approved equivalence mapping exists" — and unlike the browser-equivalence mapping, **no such pre-frozen mapping exists for this fixture**. The substitution is fully documented, which is to its credit, but documentation after the fact is not the pre-approval §11 requires, and it cannot be cured retrospectively. **Consequence: every four-node and multi-link result measured on the replacement fixture — P5-06, P5-09, P7R-01, P7R-02 — rests on an input the contract did not authorise.** This does not make those measurements untrue; it means they do not close the frozen fixture's gate. |
| P0-05 | Phase 6 regression thresholds are owner-approved and frozen before collection | `docs/m7-evidence/phase0/threshold-approval.md` | PASS (owner APPROVE recorded 2026-08-17, thresholds accepted exactly as proposed, decision rules frozen) |

### 2.1 Phase 1 — native arm64 build and live execution

All fourteen rows carry a real raw artifact in-repo. Verdicts reproduced from
the frozen ledger, which recorded them against those artifacts.

| ID | Atomic gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P1-01 | Native supervisor Go tests pass separately | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T180321Z/build/supervisor-tests.log` | PASS |
| P1-02 | `build-release.sh` native build completes with recorded output/version | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/build/build-release.log` | PASS |
| P1-03 | Hello handshake is truthful: arm64=`arm64`, amd64=`x86_64` | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T143305Z/build/hello-handshake.log` | PASS (arm64 proven live over the wire; amd64 proven by source + unit test on the arm64 target — no amd64 Linux target existed in-guest pre-Phase-2, recorded as an intentional scope boundary) |
| P1-04 | `fetch-vpcs.sh` fails closed on wrong ELF, passes matching positives | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/architecture/fetch-vpcs-arch.log` | PASS |
| P1-05 | `build-rootfs.sh` rejects mismatched supervisor/VPCS, passes both arches | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/architecture/build-rootfs-arch.log` | PASS |
| P1-06 | `pack-native.sh --arch` validates both inputs, preserves no-arg amd64 | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/architecture/pack-native-arch.log` | PASS |
| P1-07 | Inspection tools present; failures are hard failures, not skips | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/inventory/inspection-tools.log` | PASS |
| P1-08 | Pinned VPCS v0.8.3 upstream suite, or distinct substitute suite | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/vpcs/vpcs-suite.log` | PASS (no upstream automated suite exists, confirmed by inspection; frozen substitute suite run twice with real two-process UDP-tunnel ping traffic) |
| P1-09A | `build-rootfs.sh --arch arm64` completes from the exact selected inputs | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/build/rootfs-build.log` | PASS |
| P1-09 | Arm64 rootfs: matching AArch64 supervisor/VPCS, no i386, no translator/Rosetta packages | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T140000Z/build/rootfs-content.log` | PASS (145 packages, all `arm64`/`all`, zero `i386`, zero translator packages) |
| P1-10 | Reap lifecycle: exit status and zero zombie/process-tree residue | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T130740Z/live/reap.log` | PASS |
| P1-11 | Tap/ifreq lifecycle: named tap, flags/name round-trip, link up/down, clean fd | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T130740Z/live/tap-ifreq.log` | PASS |
| P1-12 | AF_PACKET frame received byte-for-byte; packet resources cleaned | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T130740Z/live/af-packet.log` | PASS |
| P1-13 | Slowtee deterministic chunks reach both sinks; helper/FIFO cleanup exact | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T130740Z/live/slowtee.log` | PASS |
| P1-14 | Capture produces valid non-empty pcapng with expected frames, zero residue | `docs/m7-evidence/phase1/rohans-macbook-air-t8103/20260817T130740Z/live/capture.log` | PASS |

**Phase 1: 15/15 PASS.**

### 2.2 Phase 2 — translation correctness rehearsal

| ID | Atomic gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P2-01 | 60-second qemu-user smoke uses owner image/licence and `--x86-rootfs` | `docs/m7-evidence/phase2/rohans-macbook-air-t8103/20260818T052325Z/qemu-user/smoke-60s.log` | PASS |
| P2-02 | At least one candidate has clean correctness JSON: prompts, 100% five-packet ping, full interval, no unexpected fatal signal | `docs/m7-evidence/phase2/rohans-macbook-air-t8103/20260818T053004Z/qualification/translation-correctness.json` | PASS (`error: null`, `faults: []`, `two_node_ping.passed: true` at 100% (5/5), `soak` 1800.0037 s of 1800 requested) |
| P2-03 | FEX is run when owner-installable and has mandatory JSON before eligibility | `docs/m7-evidence/phase2/rohans-macbook-air-t8103/20260818T053004Z/qualification/fex-eligibility.json` | **BLOCKED** — FEX-Emu documents no install path for this guest's Debian 13 trixie; not owner-installable, so not installed and not run. Section 4 external-dependency class. |

**Phase 2: 2 PASS, 1 BLOCKED.**

### 2.3 Phase 3 — package, provision, and bake-off

| ID | Atomic gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P3-01 | Arm64 package is architecture-distinct, fail-closed, preserves amd64 | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T150954Z/qemu-user/package/arm64-package.log` | PASS |
| P3-02 | Pinned arm64 Lima/VZ template: distinct machine/disk/state, image URL/digest, omits Rosetta | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T152019Z/qemu-user/package/lima-vz-template.log` | PASS |
| P3-03 | Each eligible translator provisions a clean guest with exact payload/helpers/networking | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260818T142203Z/qemu-user/installs/provisioning.log` | PASS (I1/I2/I3-PROVISION all exit 0, all 9 phases PASS in each, fresh VM disk per install) |
| P3-04 | Three clean installs and three cold boots per candidate, reproducible and fully measured | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T114655Z/qemu-user/boots/bake-off-runs.log` | PASS |
| P3-05 | Guest preflight proves AArch64 guest/supervisor/VPCS, x86_64 IOL, full inventory, no Rosetta | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260818T201456Z/qemu-user/preflight/guest-inventory.log` | PASS (all four Rosetta count categories zero in every label) |
| P3-06 | One translator wins bake-off/legal review, starts systemd, `GET /` below 500, hello, persists across stop/start | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T171741Z/qemu-user/selection/translator-selection.log` | PASS (`qemu-user` selected; `http_before/after` both 200, `persistence_match: true`, `input_verdicts` P3-01..P3-05 all PASS) |

**Phase 3: 6/6 PASS.**

### 2.4 Phase 4 — real M1–M6 launcher integration

The contract's canonical layout (`phase4/<machine-id>/<run-id>/{provenance,
scenarios,isolation,recovery,diagnostics}/`) does not exist. The directory is
flat: eight scenario logs plus one preflight/inventory log.

| ID | Atomic gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P4-01 | Fresh worktree starts at frozen `154b58b`, clean status, M7 mapping | **NONE** | **UNEVALUATED** — no provenance artifact. `macos-m7-phase4-handoff.md` asserts it in prose ("based on `luna/macos-m6-followups` @ `154b58b` … Working tree is clean"); nothing was captured. |
| P4-02 | Reviewed build/package/profile units merge against real M1–M6 layout | **NONE** (`docs/m7-phase4-file-mapping.md` is a hand-written table, not a build log) | **UNEVALUATED** — verification column claims "`go build ./...` and `go vet ./...` clean" with no captured output. |
| P4-03 | Forced `native-arm64` success | `docs/m7-evidence/phase4/scenario1-forced-native-success.log` | PASS (`selected_profile=native-arm64`, `profile_source=explicit-flag`, `canary_verdict=PASS`, `supervisor_arch=arm64`, `GUI_HTTP=200`) |
| P4-04 | Forced native preflight/runtime failure fails closed | `docs/m7-evidence/phase4/scenario2-forced-native-preflight-failure.log` | PASS (`digests=FAIL`, "refusing to fall back (fail-closed)", `EXIT=3`, subsequent `limactl list` shows no half-created VM) |
| P4-05 | Forced `rosetta-amd64` success and canary | `docs/m7-evidence/phase4/scenario3-forced-rosetta-success.log` | PASS (`translator=rosetta`, `rosetta_present=true`, `rosetta_binfmt=enabled … interpreter /mnt/lima-rosetta/rosetta`, `GUI_HTTP=200`) |
| P4-06 | `auto` native selection under the explicit test policy | `docs/m7-evidence/phase4/scenario4-auto-native-selection.log` | PASS (`requested_profile=auto`, `selected_profile=native-arm64`, `profile_source=auto-native-test-policy`, `EXIT=0`) |
| P4-07 | `auto` fallback from failed native to actual Rosetta | `docs/m7-evidence/phase4/scenario5-auto-fallback-rosetta.log` | PASS (`profile_source=auto-fallback-rosetta`, `fallback_reason` populated, landed on the real running Rosetta VM) |
| P4-08 | Persisted owner choice is honored after restart | `docs/m7-evidence/phase4/scenario6-persisted-choice-honored.log` | **UNEVALUATED** *(downgraded from PASS after adversarial review — correctly flagged)*. The artifact genuinely proves the *selection* half: `IOLBOX_PROFILE_SELECTION=rosetta-amd64` is honored, `profile_source=persisted`, `EXIT=0`. But the log states the machine was **already running and no VM creation or restart occurred**, so the row's operative clause — "**after restart**" — was never exercised. An artifact that proves a neighbouring claim does not close the stated one (§1.1). |
| P4-09 | Native and Rosetta VM/state paths isolated; host sync preserved | **No own file** — embedded in `docs/m7-evidence/phase4/scenario3-forced-rosetta-success.log`, which is already P4-05's canonical item | **UNEVALUATED** *(downgraded from PASS after adversarial review — correctly flagged)*. Two defects, either sufficient: (a) the row has **no artifact of its own**, violating "exactly one canonical raw evidence item" per row; (b) what the shared log actually shows is a `limactl list` with two VM entries at distinct directories plus a sync import line — a **point-in-time snapshot**, not a demonstration that isolation and host-sync preservation hold *through* the required create/stop/restart lifecycle. The first draft called this citation "weak" and scored it PASS anyway; that was the selectivity §1.1 now forbids. |
| P4-10 | Recovery after a half-created native VM | `docs/m7-evidence/phase4/scenario8-recovery-half-created-vm.log` | PASS (genuine failure then "retry #2 after fixing limaHomePath" recovering to `state: Running`, `canary_verdict=PASS`) |
| P4-11 | Recovery after forced **launcher** termination | **NONE** | **UNEVALUATED** — scenario 9 SIGKILLs the *Lima hostagent* (the VM), not the launcher. The only launcher SIGKILL in evidence is scenario 8's mid-creation kill, which is P4-10's artifact. The handoff collapses launcher and VM termination into one scenario; the contract keeps them atomic. |
| P4-12 | Recovery after forced **VM** termination | `docs/m7-evidence/phase4/scenario9-recovery-forced-termination.log` | PASS (post-kill `Stopped`, restart recovers to `Running`, `canary_verdict=PASS`, `GET /: 200`) |
| P4-13 | `status`/`diagnose` truthfully report profile, fallback, backend/VM, guest/kernel, architectures, translator, `rosetta_present` | Partial: status blocks inside scenarios 1/3/4/5/6/8/9 | **UNEVALUATED (row as written)** — the `status` half is fully and truthfully supported, every required field present. But **`diagnose` is never run anywhere in the Phase 4 evidence**; the only `diagnose.txt` in the repo is a Phase 5 rosetta artifact. The row requires both. |
| P4-14 | Loopback exposure/readiness, i386 truthfulness, and Windows builds/tests remain correct | **NONE** | **UNEVALUATED** — i386 truthfulness is only indirect (`IOLBOX_DISABLE_I386=1` drop-in, `capability_policy=PASS`); loopback is a *declared* port-contract line, not a bind/exposure probe; **Windows builds/tests have no artifact at all**, only the handoff's "cross-compiles clean for … windows/amd64". |
| P4-15 | After any defect, rerun failed case plus forced-native, forced-Rosetta, fallback, isolation, recovery | **NONE** | **UNEVALUATED** — the handoff claims six defects "each reverified live"; scenario 8 embeds one fix/rerun cycle, but no artifact shows the required post-defect re-run *set*. |

**Phase 4: 7 PASS, 8 UNEVALUATED.**

### 2.5 Phase 5 — authoritative M3/M4 hardware matrix

Only two subtrees were committed: `m3-rerun/m3-20260819T194911Z-28552/` and
`native-arm64/`. **The M4 orchestrator evidence (items 2–7) was never copied
into the repository**; `STATUS.md` and the Phase 7 doc point at Mac-only paths.

| ID | Matrix gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P5-01 | Browser lifecycle: upload, registration, lab create/start, console, stop, reload, saved-lab recovery | `docs/m7-evidence/phase5/m3-rerun/m3-20260819T194911Z-28552/browser-equivalent.txt` | PASS — `--- PASS: TestMacOSBrowserEquivalentE2E (57.25s)`. Legitimate under the Phase 0 **owner-approved** browser-equivalence mapping (`phase0/m3-m4-inputs.md`, "Browser-equivalence mapping — CLOSED, owner-approved"). `browser-tabs.txt` honestly records "browser tab probe unavailable": no literal Safari/Chrome click-through exists, and the approved mapping's known differences (no browser-side JS rendering, no download-prompt/Gatekeeper flows) are therefore untested. |
| P5-02 | Host data/sync: images/labs survive restart; spaces and non-ASCII paths round-trip | `docs/m7-evidence/phase5/m3-rerun/m3-20260819T194911Z-28552/launcher-start-difficult-path.txt` | PASS (real non-ASCII path `M3 data café`; "rescued 8 guest lab(s)", "imported 1 host image(s)"; default-path restart survival in `launcher-stop-default.txt` / `default-sync-files.txt`) |
| P5-03 | Consoles/forwarding: two simultaneous consoles; Mac-loopback-only GUI/console/capture | `docs/m7-evidence/phase5/m3-rerun/m3-20260819T194911Z-28552/lsof-listeners.txt` | PASS (every forwarded port bound loopback-only, e.g. `TCP 127.0.0.1:4001 (LISTEN)`; `port-probe-host.txt` "probed 80 forwarded console/capture ports"; two genuinely simultaneous consoles via `m3OpenConsoles(t, ports, []int{0,1})` + `m3SendConcurrently` in the passing run) |
| P5-04 | Capture: live traffic and valid non-empty multi-link/soak pcapng opened/saved as required | `docs/m7-evidence/phase5/m3-rerun/m3-20260819T194911Z-28552/pcap-validator.txt` | **UNEVALUATED** — the M3 single-link capture is real and valid (`pcapng … bytes=896 packets=2 sha256=850c2653…`), but two sub-claims of the row are unmet: the validator itself records "Wireshark tools unavailable; stdlib pcapng validator was authoritative", and the row's **multi-link and soak pcapng have no artifact in the repo**. Scored on the row as written, not on its strongest sub-part. |
| P5-05 | VPCS/IOL: native VPCS and translated IOL bidirectional traffic with counts/loss/latency | `docs/m7-evidence/phase5/native-arm64/vpcs-iol-traffic.txt` | PASS for the **native-arm64** arm — `Success rate is 90 percent (9/10), round-trip min/avg/max = 1/2/16 ms` router→VPCS, plus ten real ICMP replies VPCS→router. The rosetta-amd64 item-1 run claimed PASS in the handoff has **no raw file in the repo**. |
| P5-06 | Multi-link: every expected path forwards and teardown is clean | **NONE** | **UNEVALUATED** — handoff claims "PASS (after real fix) … both directions ≥99/100"; prose only, item-2 raw output is on the Mac. |
| P5-07 | NAT: outbound connectivity in qualified mode; stop removes NAT state | **NONE** | **UNEVALUATED** — handoff claims "PASS (after real fix) … gateway + numeric-target ping"; prose only. |
| P5-08 | Extnet: attach, traffic, detach, cleanup under the frozen interface/mapping | **NONE** | **UNEVALUATED** — the handoff records `NOT_EXERCISABLE` ("no suitable Lima/extnet host interface in preserved probes"), explicitly "a legitimate decision-table result, not a waiver". **If that probe output were in the repo this row would be `BLOCKED`** under plan §11's "an unavailable extnet … is BLOCKED and prevents promotion" and §13's section-4 external-dependency class. It is not in the repo, so it cannot be scored `BLOCKED` from prose alone. Either way it is not `PASS` and either way it prevents PROMOTE. |
| P5-09 | Capacity: authoritative two-node and four-IOL-node labs reach prompts and forward traffic | **NONE** | **FAIL (owner-waived for Phase 5's exit criterion; the waiver is an override, not a re-score)** — `STATUS.md`: "Capacity (four-node) row verdict: BLOCKED/UNVERIFIED in the plan-required post-soak position". Two independent full orchestrator runs hard-walled on both the initial attempt and the one permitted cold retry (2/2), `EOF` reading a console ~110 s in, all four IOL processes still alive (no OOM). Three standalone (no-soak) runs passed. **No raw artifact in the repo.** See §3 for the Phase 7 re-measurement, which supersedes the *scope* of this finding. |
| P5-10 | Traffic soak: two hours with capture, loss/fault/exit/CPU/load/memory record | **NONE** | **UNEVALUATED** — handoff claims "PASS at 1200s … `SOAK-COMPLETE` seal verified, 20/20 rows"; prose only. Independently, this is a **reduced bar**: the owner directed 1200 s against the plan's stated 7200 s, so multi-hour resource-drift effects were never exercised. Even with the artifact, this would be `PASS` only against the reduced duration. |
| P5-11 | Forced termination: launcher and VM termination recover with no stale resources or corrupt state | **NONE** | **UNEVALUATED** — handoff claims "PASS (via manual driving after fixing a real ordering bug)"; prose only. |
| P5-12 | Rosetta exclusion: pre/during/post inventory is Rosetta-free | `docs/m7-evidence/phase5/native-arm64/rosetta-inventory/{before,during,after}.txt` | PASS (all three show empty mount sections and no Rosetta process; supervisor runs as a plain native binary on `6.12.101+deb13-cloud-arm64 … aarch64`; binfmt list is all `qemu-*`, no `rosetta` entry — directly contrastable with the Phase 4 rosetta log's `interpreter /mnt/lima-rosetta/rosetta`) |

**Phase 5: 5 PASS, 1 FAIL (waived), 6 UNEVALUATED.**

Plan §11's exit bar — "every row is PASS" — is not met.

### 2.6 Phase 6 — same-Mac A/B thresholds

**Structural finding that governs every row in this section.** Not one
canonical Phase 6 path exists. The contract binds
`docs/m7-evidence/phase6/<machine-id>/<run-id>/{2,4}-node/{raw,thresholds}/*.log`.
What exists is a flat `raw-metrics/` directory of six JSON files plus
`phase6_run.py`. There is **no `<machine-id>/<run-id>` binding, no
`2-node`/`4-node` split, and zero `thresholds/*.log` files of any kind** — no
`functional.log`, `boot.log`, `resources.log`, `pressure-tier.log`, or
`traffic.log` for either topology.

`phase6_run.py` is a pure recorder. It collects values and `json.dump`s them;
it computes **no median, no delta, no baseline comparison, and asserts no
threshold**. Every threshold verdict in the Phase 6 handoff was computed by
hand in prose — precisely what §13 forbids as a row citation.

**Sample-count finding.** The frozen preamble requires "at least three runs"
per topology, and the approved decision rules repeat it. Actual: native 2-node
**3**, rosetta 2-node **1** (`"run": 5`), native 4-node **1**, rosetta 4-node
**1** (both 4-node files named `smoketest-`). The Phase 6 handoff cites three
rosetta 2-node VM-boot values; two of those three have no artifact.

Consequence: every A/B *delta* row lacks a baseline term, and no row can be
closed. All twelve are `UNEVALUATED` — under rule 3, "a missing metric is
UNEVALUATED, never zero", not `FAIL`.

| ID | A/B gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P6-01 | Two-node raw run completeness and identical-condition record | contract path `…/2-node/raw/runs.log`: **NONE**. Nearest: `docs/m7-evidence/phase6/raw-metrics/native-2node-{final-verify,run-b,run-c}-metrics.json`, `rosetta-2node-saved-metrics.json` | **UNEVALUATED** — rosetta n=1 against a required n≥3; no identical-condition record file. |
| P6-02 | Two-node: no new functional failure/crash/SIGILL/SIGSYS/stale resource/Rosetta dependency | `…/2-node/thresholds/functional.log`: **NONE** | **UNEVALUATED** — native raw is clean (empty dmesg/journal crash greps, `teardown_ok: true` ×3); rosetta raw shows `lab_boot_ok: false`, `node 0: no prompt within 600s`. But Phase 7 re-scored that stall PASS 3/3 and *that* evidence is Mac-only. Nothing in-repo settles the row. |
| P6-03 | Two-node: median VM/lab boot not >25% slower than baseline | `…/2-node/thresholds/boot.log`: **NONE** | **UNEVALUATED** — native `vm_boot_seconds` 100.31/94.29/104.30 (median 100.31); rosetta single 96.27 → +4.2%, but n=1 yields no median or worst case. Lab boot has no baseline at all: native 9.84/9.85/10.08 vs rosetta `lab_boot_seconds: null`. |
| P6-04 | Two-node: median steady-state CPU/guest-host memory/aggregate node RSS not >25% worse | `…/2-node/thresholds/resources.log`: **NONE** | **UNEVALUATED** — the harness never measured what the row requires. `guest_process_rss_kb` lists only `supervisor`/`vpcs`; **no IOL node RSS was captured**, so "aggregate node RSS" does not exist. Host CPU is whole-Mac `top` on an actively-used laptop, not steady-state process CPU. |
| P6-05 | Two-node: no worse memory pressure, sustained swap, or four-node-tier failure | `…/2-node/thresholds/pressure-tier.log`: **NONE** | **UNEVALUATED** — guest swap is 0 in every 2-node run; Mac pressure recorded only as raw `vm_stat` counters with no delta computed. The row's four-node-tier clause is separately unmet (see P6-11). |
| P6-06 | Two-node: packet loss not >1 pp worse; functional ping passes; no soak interruption | `…/2-node/thresholds/traffic.log`: **NONE** | **UNEVALUATED** — native has real data (PC1→R1 50/49 = 2.0% ×3; R1→PC1 50/50 = 0.0% ×3), but rosetta `"pings": []` is empty. A one-armed result cannot satisfy an A/B delta gate. |
| P6-07 | Four-node raw run completeness and identical-condition record | `…/4-node/raw/runs.log`: **NONE**. Nearest: `docs/m7-evidence/phase6/raw-metrics/smoketest-{native,rosetta}-4node-metrics.json` | **UNEVALUATED** — exactly one run per arm (`"run": 0`) against n≥3. |
| P6-08 | Four-node: no new functional failure/crash/SIGILL/SIGSYS/stale resource/Rosetta dependency | `…/4-node/thresholds/functional.log`: **NONE** | **UNEVALUATED** — in-repo raw shows both arms failing (`lab_boot_ok: false`, ~526 k / ~417 k characters of `lab_boot_node_errors` dominated by `EOFError('WebSocket closed mid-frame')`). Phase 7 measured native **4/4 PASS** with the mature harness, so the in-repo native failure is affirmatively unsupported — and the superseding evidence is not in the repo. |
| P6-09 | Four-node: median VM/lab boot not >25% slower | `…/4-node/thresholds/boot.log`: **NONE** | **UNEVALUATED** — VM boot native 92.26 s / rosetta 90.19 s, n=1 each (no median). Lab boot unmeasurable: `lab_boot_seconds: null` on both arms. |
| P6-10 | Four-node: median steady-state CPU/guest-host memory/aggregate node RSS not >25% worse | `…/4-node/thresholds/resources.log`: **NONE** | **UNEVALUATED** — only `supervisor` RSS captured at failure time (native 24,632 kB; rosetta 70,288 kB); no node processes survived, so no steady state ever existed to sample. |
| P6-11 | Four-node: no worse memory pressure, sustained swap, or inability to run the approved four-node tier | `…/4-node/thresholds/pressure-tier.log`: **NONE** | **UNEVALUATED (Phase 6 evidence)** — guest `free -m` at failure only, `Swap: 0` both arms, no pressure delta computed. **This row's "inability to run the approved four-node tier" clause is separately and authoritatively measured in §3 below (P7R-01 / P7R-02), which is where its real verdict lives.** |
| P6-12 | Four-node: packet loss not >1 pp worse; functional ping passes; no soak interruption | `…/4-node/thresholds/traffic.log`: **NONE** | **UNEVALUATED** — `"pings": []` on both 4-node runs. No packet was ever sent; a missing metric is UNEVALUATED, never zero. |

**Phase 6: 0 PASS, 12 UNEVALUATED.**

Plan §12's exit bar — "raw runs, summaries, deltas, and threshold verdicts
exist for every metric/topology" — is not met.

### 2.7 Phase 7 — promotion mechanics and legal hygiene

| ID | Promotion gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P7-01 | Ledger integrity: every atomic gate has exactly one canonical raw item and one verdict | `docs/m7-evidence/phase7/<machine-id>/<run-id>/ledger-integrity/check.log`: **NONE** (the `phase7/` directory does not exist in any worktree) | **UNEVALUATED**, and substantively it would **FAIL**: by this document's own count, **25 of the 66 frozen rows cite no raw item at all** (independently re-verified by the adversarial reviewer), a further 3 recomputation rows (P7R-01/02/03) cite Mac-only paths that were never committed, **a further 24 (all of Phase 1–3) cite paths that exist only on a different branch and not on the promotion candidate** (see §2.-1), the canonical `<machine-id>/<run-id>` binding was never performed for Phases 4–6 at all (§1.2), P4-09 has no file of its own, and P4-13 cites only a partial. This document is the check; it does not pass. |
| P7-02 | Legal hygiene and translator notices/obligations are complete for the selected product | `docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T171741Z/qemu-user/selection/translator-selection.log` (the `"legal"` block of the `SELECTION` record) | **UNEVALUATED** — the raw item genuinely exists and records the selected translator's packages (`qemu-user-static 1:10.0.11+ds-0+deb13u1`, `binfmt-support 2.2.2-7+b1`), both copyright-file sha256s, `owner_image_excluded: true`, `owner_licence_excluded: true`, and `source_notice_obligations_recorded: true`. **But the same block sets `redistribution_review_required: true`, and no record anywhere shows that review was performed.** Corroborating: `THIRD_PARTY.md` was last touched at `7cf8e50` (M6), predates the native-arm64 profile, still frames the Mac archive as Rosetta-only ("x86_64 IOL is the only supported IOL architecture in this Rosetta profile") and asserts "QEMU is **not** in the Apple Silicon archive" — while `packaging/macos/guest/10-multiarch-native.sh:233` apt-installs `qemu-user-static` into the native guest, and the plan itself notes "FEX is MIT; qemu-user brings GPL obligations." The obligation is *recorded* but not *discharged*. |
| P7-03 | Mechanical promotion decision is reproducible from the ledger with no FAIL/UNEVALUATED/BLOCKED hidden by prose | `docs/m7-evidence/phase7/<machine-id>/<run-id>/promotion/decision.log`: **NONE** | **UNEVALUATED** as a contract artifact. The frozen ledger's own closing text: "The `P7-03` file is a future decision record, not permission to promote." **This document discharges the row's substance** — the decision below is reproducible from the rows and hides nothing — but the bound raw file does not exist. |

**Phase 7: 0 PASS, 3 UNEVALUATED.**

---

## 3. Phase 7 re-measurement rows (recomputation, not new gates)

Section 13 states Phase 7 "recomputes the atomic rows below". Three cells were
re-measured on 2026-08-20 with the mature Go M4 harness
(`packaging/macos/tests/hardware-m4-phase7.sh`), superseding Phase 6's
lightweight `phase6_run.py`. These are recomputations of P6-02, P6-08 and
P6-11 split by arm — **not** additional summary gates. They are given their own
rows because the two arms produced opposite results and collapsing them would
hide the divergence.

Full narrative: `docs/macos-m7-phase7-mature-reverify.md`.

| ID | Recomputed gate | One canonical raw evidence item | Verdict |
|---|---|---|---|
| P7R-01 | Four-node capacity tier, **native-arm64**, standalone/no-soak | `~/iolbox-p7-build/src/evidence-m4-p7/native/m4-20260820T230249Z-83021-2650479422/` on the Mac — **not in the repository** | **PASS 4/4** as measured (two independent fresh-VM runs × 2 attempts). All four nodes `running`, all four consoles substantive, ping series `[100,100,100,100,99,100,99,100,100,100]`, per-attempt wall time 23–25 s. Guest cost ~1.0–1.15 GiB leaving ~2.15 GiB available — "not a machine near a wall". **Citation caveat:** the raw artifacts are Mac-only, so this cannot close P6-08/P6-11 under the contract's rule 2. |
| P7R-02 | Four-node capacity tier, **rosetta-amd64**, standalone/no-soak | `~/iolbox-p7-build/src/evidence-m4-p7/rosetta/m4-20260820T225336Z-81841-2039620124/` and `…/m4-20260820T225809Z-82279-1838404713/` on the Mac — **not in the repository** | **FAIL — hard wall, 0/2.** See the dedicated discussion below. |
| P7R-03 | Router console usable, **rosetta-amd64**, two-node (item-1) | `~/iolbox-p7-build/src/evidence-m4-p7/rosetta/m4-20260820T222011Z-79262-107393690/` on the Mac — **not in the repository** | **PASS 3/3** as measured. `p7-item-status.txt` `exit=0` ×3, three `phase.json` `status = PASS`, `R1>` reached ~33–38 s after each phase start, 100/100 packets both directions. **This supersedes Phase 6's "rosetta-amd64 router console usable: FAIL — 0/5"**, which is now attributed to a defect in Phase 6's own console reader, not the product. Correction recorded in `docs/macos-m7-phase7-mature-reverify.md` (Q1 verdict) and back-annotated into `docs/macos-m7-phase6-handoff.md`. **Citation caveat:** Mac-only; the in-repo Phase 6 JSON still shows the failure and nothing in-repo shows the pass. |

### P7R-02 — the rosetta-amd64 four-node FAIL, in full

**Verdict: FAIL.** Recorded deliberately as `FAIL`, not `UNEVALUATED`, and the
reasoning is stated rather than assumed:

- The runs **completed**. They were not aborted, not blocked on a missing
  dependency, and not lost to harness death before measurement — the
  per-attempt `phase.json` is fully written in both runs.
- An approved threshold applies. `phase0/threshold-approval.md` item 4 makes
  "inability to run the approved four-node tier" unacceptable.
- The frozen decision rule is unambiguous: "Every completed run/result to
  which an approved threshold applies receives `PASS` or `FAIL` immediately.
  Any completed run exceeding an approved threshold is `FAIL` and forces
  `NO-PROMOTE`; it cannot be reclassified as `HOLD` or 'insufficient sample.'"

Measured signature, identical across two independent fresh-VM runs:
`phase.json` `"status": "UNVERIFIED"`, phase duration ~110 s (A) and ~116 s
(B); **all four IOL processes still `running` at the end**, ruling out an OOM
kill; Go driver failure at `macos_m4_runtime_darwin_test.go:1769: EOF`; **two
of four consoles never progressed past their first line** (59-byte
transcripts containing only `initial wake=\r\n prompt=R2>` / `prompt=R4>`;
which two moves between runs, that two of four stall does not). Run A's
consoles first woke ~106 s after phase start versus ~12 s on native — a ~9×
difference.

**Host headroom does not explain it.** The native control run B was
deliberately launched *after* both rosetta runs under worse host pressure —
149 MB PhysMem unused / 4484 MB swap, versus rosetta A's 340 MB / 4093 MB —
and still passed 2/2 with full ping sets.

**Caveat, recorded so this row is never read as an unconditional,
fully-isolated verdict.** The two arms differ in **more than execution mode**:
rosetta ran the M6-vintage launcher and M6 payload; native ran the
current-HEAD launcher and the newer owner-validated payload containing two
netprobe console fixes the M6 payload lacks. The observed symptom — consoles
stalling after their first line, then a WebSocket EOF — is precisely the class
those fixes touch. The result therefore supports either reading:

- **(a)** four concurrent nodes under Rosetta translation exceed what this Mac
  sustains (the capacity reading); or
- **(b)** the M6 payload has a multi-console defect that four nodes expose and
  two do not (the build-vintage reading, already fixed at HEAD).

Reading (b) is not idle: P7R-03 showed the *same* M6 artifact passing
two-node console 3/3, so whatever bites here needs four consoles to appear.
**This confound was not eliminated.** Closing it requires re-running the
rosetta arm with a current-HEAD `linux-amd64` payload so only execution mode
differs; that was not attempted (no such payload on the Mac, and only 6.7 GiB
free after four fresh VMs).

**Why the confound does not change the verdict.** A confound that might
*soften* a failure does not erase the failure that was actually measured. The
approved rules forbid reclassifying a completed threshold-exceeding run as
"insufficient sample", and reading (b) — a real product defect in the shipped
M6 payload — is not obviously more favourable than reading (a) anyway. What
the confound legitimately changes is the **scope** of the claim: this row
establishes that *the M6 rosetta artifact hard-walls at four nodes on this
Mac*, not that *Rosetta translation as a mechanism cannot sustain four nodes*.

**Where this cannot cleanly map to a plan-defined outcome — stated
explicitly rather than waived.** Section 13's four outcomes are properties of
the ledger, not of an individual arm. The plan has no vocabulary for "FAIL on
the fallback path, PASS on the promoted path". Three specific gaps:

1. Section 13 offers no mechanism to scope a FAIL to one profile. Read
   literally, this row alone forces NO-PROMOTE for the whole ledger.
2. The row's raw evidence is Mac-only, so under the contract's rule 2 it
   arguably cannot close a row *at all* — which would make it UNEVALUATED and
   still block PROMOTE, just via a different clause.
3. The confound means the row may be measuring build vintage rather than
   execution mode, and the plan provides no "measured but not isolated"
   verdict value.

**This is flagged for review rather than resolved unilaterally.** Note that
all three gaps lead to the same place — NO-PROMOTE — so the outcome in
section 5 does not depend on which is correct.

### Consequence for the Phase 5 waiver

The owner's Phase 5 waiver framed the hard wall as "a known Mac-specific
hardware-capacity limit". Phase 7 shows that framing is **too broad**: it
reproduces on the **rosetta arm only**, and native-arm64 — the profile `auto`
now prefers — passes four-node cleanly. The waiver should narrow from "a Mac
capacity limit" to "a rosetta-arm limit on this Mac, possibly a build-vintage
defect". **Narrowing a waiver's scope is an owner action, not this
document's**; it is recorded here as a recommendation.

---

## 4. Owner rulings and waivers on record — separate from the mechanical ledger

These are real, they are binding on the project, and **none of them converts
any row above into `PASS`**. Section 13's outcomes are computed from the rows
alone; the rulings sit beside the computation, not inside it.

| # | Ruling | Where recorded | Effect on the ledger |
|---|---|---|---|
| 1 | **Thresholds approved** exactly as proposed, with the frozen decision rules | `docs/m7-evidence/phase0/threshold-approval.md` | Enables Phase 6 scoring. Scores P0-05 PASS. |
| 2 | **Browser-equivalence mapping approved** as equivalent to a literal browser click-through | `docs/m7-evidence/phase0/m3-m4-inputs.md` | Legitimises P5-01's citation. Genuine mechanical effect, because it was frozen *before* execution. |
| 3 | **PROMOTE is not self-executing**: a mechanical PROMOTE authorises the ledger's own label only; merge/tag/publish needs separate explicit owner sign-off after personal review of measured results | `docs/m7-evidence/phase0/threshold-approval.md`, "Owner ruling on the ship decision" | Constrains what any outcome below authorises. |
| 4 | **Traffic-soak reduced 7200 s → 1200 s** by explicit mid-session instruction | `docs/m7-evidence/phase5/STATUS.md`; `docs/macos-m7-phase5-handoff.md` | Lowers P5-10's bar. Does not supply the missing artifact. |
| 5 | **Four-node capacity waived** as a known Mac-specific limit, Phase 5 to close on that basis | `docs/macos-m7-phase5-handoff.md`, "Owner waiver" | Lets Phase 5 *close*. Does **not** re-score P5-09, per §13 "Later inconvenience, sample concerns, or a packaging label cannot downgrade a failure." Scope now known to be too broad (§3). |
| 6 | **Promote native-arm64** notwithstanding the open items | `docs/macos-m7-phase6-handoff.md`, "Owner promotion ruling" | **An override, self-described as such.** No effect on any row. |
| 7 | **Owner personally validated the running native-arm64 build via its GUI** | `docs/macos-m7-phase6-handoff.md`, "Owner personally validated…" | Satisfies ruling 3's separate-sign-off requirement. Produced two real netprobe fixes (`0a43e96`, `339f6b1`). Not gate evidence. |

### The distinction this document refuses to blur

The Phase 6 handoff states the position itself, and it is worth quoting because
it is the project's own words, not this document's inference:

> **This is an explicit owner override, not a mechanical PROMOTE per plan
> section 13's own algorithm.** The gate ledger as it stands still contains a
> real FAIL … and real UNEVALUATED rows … — section 13 is explicit that "No
> FAIL, UNEVALUATED, or BLOCKED gate may coexist with PROMOTE." Recording this
> honestly rather than silently reclassifying those rows…

So: **native-arm64 was promoted, and the ledger never computed PROMOTE.** Both
are true. What would be false — and what this document declines to write — is
that the ledger *mechanically* reached PROMOTE.

**Correction made after adversarial review.** The first draft wrote that the
owner "holds an override authority the plan itself reserves." That was wrong,
and the review was right to catch it. The plan's authority runs **one way
only**:

- A mechanical PROMOTE enables native eligibility for `auto`; **and even then**,
  merge, tag, or publish requires separate explicit owner sign-off after
  personal review of measured results.
- **Nowhere does the plan say owner sign-off substitutes for a mechanical
  PROMOTE.** The sign-off is an *additional* gate layered on top of PROMOTE,
  not an alternative route to it.

The owner's ruling 6 is therefore not an exercise of reserved authority — it is
a **deviation from the planned ordering**, taken knowingly and recorded
candidly by the Phase 6 handoff itself. It remains entirely legitimate as a
product decision: the owner owns the product and may decide to proceed on
incomplete gates. But this document should not have dressed that up as
something the plan anticipated. It did not.

### Sequencing note, recorded plainly

Plan §10.3 says: "Until promotion, `auto` continues to default to Rosetta; the
test hook for native preference must be explicit." That gate was flipped at
`e2ffe34` — `auto` now prefers native-arm64 whenever native preflight passes,
with explicit Rosetta fallback retained.

**Stated precisely, because the review correctly objected to the vague phrase
"shipped in code":** commit `e2ffe34` is **landed on the integration branch
`luna/macos-m7-phase4-integration` only**. It is **not merged to `main`, not
tagged, not built into any published artifact, and not released.** Those are
materially different events and none of them has occurred.

**And stated honestly:** because §10.3 conditions the flip on "promotion", and
no mechanical PROMOTE exists, `e2ffe34` is a **departure from the plan's stated
ordering**, not merely a change that ran ahead of a formality. The first draft
framed it as "the sequence ruling 3 contemplates"; that was too lenient.
Ruling 3 governs what happens *after* PROMOTE. It does not authorise a
default-behaviour change *instead of* PROMOTE. Personal validation 7 was
genuine and valuable, but it satisfies a post-PROMOTE requirement — it does not
cure the missing PROMOTE prerequisite.

This is recorded so no future reader infers the code change followed a
mechanical PROMOTE. It did not. Whether to keep, gate, or revert `e2ffe34` is
an owner decision and is explicitly **not** decided here.

---

## 5. Mechanical outcome

### 5.1 Tally

**The frozen 66-row contract — the authoritative universe** (§1.3):

| Verdict | Count | Rows |
|---|---:|---|
| PASS | 35 | P1 ×15, P2 ×2, P3 ×6, P4 ×7, P5 ×5 |
| FAIL | 1 | P5-09 |
| BLOCKED | 1 | P2-03 |
| UNEVALUATED | 29 | P4 ×8, P5 ×6, P6 ×12, P7 ×3 |
| **Total** | **66** | |

Per phase: P1 15/15 PASS · P2 2 PASS + 1 BLOCKED · P3 6/6 PASS ·
P4 7 PASS + 8 UNEVALUATED · P5 5 PASS + 1 FAIL + 6 UNEVALUATED ·
P6 12 UNEVALUATED · P7 3 UNEVALUATED.

**Addendum rows, tallied separately and never mixed into the above** (§1.3):

| Verdict | Count | Rows |
|---|---:|---|
| PASS | 6 | P0-01, P0-02, P0-03, P0-05, P7R-01, P7R-03 |
| FAIL | 2 | P0-04 (fixture substitution without a pre-frozen mapping), P7R-02 |
| **Total** | **8** | |

P7R-01/02/03 are recomputations of the cells beneath P6-02, P6-08 and P6-11;
they are **not** counted a second time in the frozen tally, and the P6 rows they
re-measure remain `UNEVALUATED` there.

### 5.2 Applying section 13's rules, in the order the plan states them

**PROMOTE** requires "every gate is PASS. No FAIL, UNEVALUATED, or BLOCKED
gate may coexist with PROMOTE." The frozen universe holds 1 FAIL, 1 BLOCKED,
and 29 UNEVALUATED rows. **PROMOTE is unavailable** — and would remain so even
if every disputed row in this document were resolved in the most favourable
direction, because 12 Phase 6 rows have no threshold artifact of any kind.

**NO-PROMOTE** applies when "any evaluated gate is FAIL, including any
completed run that exceeds an approved threshold." **P5-09 is FAIL** inside the
frozen universe, and P7R-02 is the named case — a completed run exceeding
approved threshold 4. **NO-PROMOTE applies.**

**HOLD / EXPERIMENTAL** requires that "no gate has failed". A gate has failed.
The plan adds: "HOLD cannot override FAIL." **HOLD is unavailable**, despite
the 29 UNEVALUATED rows that would otherwise put the ledger here.

**BLOCKED** requires that "no gate has failed". A gate has failed, and the plan
adds "BLOCKED cannot coexist with PROMOTE" and "Ordinary code defects and
measured regressions are not BLOCKED." **BLOCKED is unavailable** as the
ledger-level outcome, notwithstanding P2-03's genuine row-level BLOCKED.

The outcomes are mutually exclusive and exactly one survives.

**A contract ambiguity worth recording** (raised by the review, and real):
section 13 gives no precedence rule between HOLD and BLOCKED when one row is
BLOCKED and another is UNEVALUATED and nothing has failed. Both predicates can
be true simultaneously, yet the outcomes are declared mutually exclusive. That
ambiguity does **not** bite here — a FAIL exists, which excludes both — but it
should be repaired before any future ledger lands in that state.

### 5.3 Outcome

> ## Mechanical outcome: **NO-PROMOTE**

This holds under multiple independent sufficient conditions, so it is robust to
disagreement about any single row:

1. **P5-09** — four-node capacity, 2/2 hard wall in the plan-required
   post-soak position. Owner-waived for Phase 5's exit, but §13 forbids a
   waiver downgrading a failure.
2. **P7R-02** — rosetta-amd64 four-node, a completed run exceeding approved
   threshold 4. FAIL forces NO-PROMOTE by name.
3. **Even if both FAILs were vacated** — which is exactly what the adversarial
   review argues for, see §5.5 — 29 UNEVALUATED rows and 1 BLOCKED row remain.
   The best reachable outcome would be HOLD, which by its own terms "keeps
   `auto` on Rosetta". **PROMOTE is unreachable under every reading of this
   evidence considered by either this document or its reviewer.**

### 5.5 Recorded disagreement with the adversarial review

The `gpt-5.6-sol` review accepted the measured facts but reached a different
ledger outcome: **HOLD / EXPERIMENTAL**, on the reasoning that neither FAIL is
"contract-valid" because neither cites a canonically-bound raw artifact — P5-09
says `NONE`, and P7R-02's artifacts are Mac-only. It proposed splitting each
row into a run-level observation (`FAIL`) and a ledger-level verdict
(`UNEVALUATED`).

**The split itself is a good idea and is adopted** — §1.1 now separates "what
was observed" from "what the artifact admits". **The conclusion drawn from it
is rejected**, for three reasons:

1. **The frozen decision rule keys on the run, not on the filing.** Its words
   are: "Every *completed run/result* to which an approved threshold applies
   receives PASS or FAIL immediately. Any *completed run* exceeding an approved
   threshold is FAIL and **forces NO-PROMOTE**." Both rosetta four-node
   measurements are completed runs against an approved threshold. The rule
   attaches at that moment.
2. **§13 forecloses this specific downgrade.** "Later inconvenience, sample
   concerns, or a packaging label cannot downgrade a failure." An artifact left
   on the Mac is precisely later inconvenience and packaging. Reading the
   canonical-path rule to suppress a measured failure would let **missing
   paperwork improve the outcome** — NO-PROMOTE becomes HOLD by *not* filing
   evidence. That inverts the rule's purpose, which is to stop unproven
   *success* claims.
3. **The asymmetry is deliberate and is the safer error.** The evidence rule
   exists so nobody claims a PASS they cannot show. Applying it to erase a FAIL
   they *did* observe serves no protective purpose and actively misleads.

**Where the review is right and this document now agrees:** the causal claim
must stay narrow. P7R-02 establishes that *the M6-vintage Rosetta artifact
failed four-node operation on this Mac* — not that Rosetta translation is
inherently incapable. §3 already says this; the confound must be resolved
before any broader claim is made.

**Net effect of the disagreement: none on the outcome.** Sol's own §5.3 point 3
concedes PROMOTE is unreachable either way. The dispute is whether the label is
NO-PROMOTE or HOLD. This document says NO-PROMOTE and shows its work; a reader
who prefers the review's reasoning gets HOLD, which still "keeps `auto` on
Rosetta" and still contradicts the shipped `e2ffe34` default.

### 5.4 The scoped question: "native-arm64 as the `auto` default"

Asked as posed, and answered honestly: **section 13 defines no arm-scoped
mechanical outcome.** Its four verdicts are properties of the whole ledger.
There is no plan-defined way to compute "PROMOTE for native-arm64 while
rosetta-amd64 holds a FAIL", so any such answer is an extrapolation, not a
mechanical result.

Scored anyway, as far as the plan's own vocabulary permits:

- **Rows measured specifically on the native-arm64 arm are uniformly clean**:
  P4-03, P4-06, P5-05, P5-12, P7R-01 all PASS; Phase 6's single in-repo native
  four-node failure is affirmatively superseded by P7R-01's 4/4.
- **But native-arm64 still owns UNEVALUATED rows** — P4-13 (`diagnose` never
  run), P4-14 (loopback probe, i386 probe, Windows builds), P4-01/P4-02
  (provenance), and every Phase 6 A/B row, whose deltas are undefined without a
  rosetta baseline. An A/B threshold cannot be satisfied one-armed.
- Therefore, **even scoped to native-arm64, the result is not PROMOTE.** It is
  at best HOLD, and HOLD "keeps `auto` on Rosetta" — which is not what the
  shipped code now does.

**The honest summary:** `auto` preferring native-arm64 today rests on owner
override rulings 6 and 7, not on a mechanical gate result. That is a legitimate
basis under the plan's own reserved authority. It is not a PROMOTE.

### 5.5 What this outcome does and does not authorise

Per ruling 3 and §13, even a mechanical PROMOTE would authorise only the
ledger's own label. **NO-PROMOTE authorises nothing at all.** It does not
reverse the owner's override, does not require reverting `e2ffe34`, and does
not decide the merge-to-`main` or release-pipeline questions — those remain
separate decisions that surface to the owner. Rosetta-amd64 remains the
explicit fallback path and is not being removed; P7R-02 does not change that.

---

## 6. What would have to change for a mechanical PROMOTE

Ordered by cost, cheapest first. Items 1–2 are clerical and would resolve
roughly half the UNEVALUATED rows without any new hardware time.

1. **Copy the existing raw evidence off the Mac into the repository.** The M4
   orchestrator output for Phase 5 items 2–7 and every Phase 7
   `evidence-m4-p7/` directory already exist and were measured; they were never
   committed. This alone addresses P5-06, P5-07, P5-08, P5-09, P5-10, P5-11 and
   gives P7R-01/02/03 contract-valid citations.
2. **Capture the artifacts for checks that were run but not recorded**:
   Phase 4 provenance (P4-01), the build/vet/test output (P4-02), a `diagnose`
   run (P4-13), loopback/i386/Windows compatibility output (P4-14), and the
   post-defect rerun set (P4-15).
3. **Re-run Phase 6 to its actual contract**: ≥3 runs per topology **per arm**,
   with node-process RSS actually sampled, and threshold files that compute
   medians and deltas rather than leaving them to prose. Twelve rows depend on
   this and none can move without it.
4. **Discharge P7-02's `redistribution_review_required: true`** and update
   `THIRD_PARTY.md` to cover the native-arm64 profile's `qemu-user-static`
   (GPL) rather than describing the Mac archive as Rosetta-only.
5. **Resolve P7R-02's confound** by re-running the rosetta arm with a
   current-HEAD `linux-amd64` payload. This determines whether the FAIL is a
   capacity limit or an already-fixed M6 payload defect — but note that under
   §13 a resolved-and-still-failing result stays a FAIL.
6. **Bring the Phase 0–3 evidence onto the promotion candidate branch** (or
   record an explicit owner-approved cross-branch citation base), so the line
   being promoted actually carries the evidence it is promoted on. See §2.-1.
7. **Repair the frozen contract itself**: add Phase 0 rows for section 13
   category 1 (the frozen ledger has none), split P4-11/P4-12 in the evidence
   layout as the contract already splits them in rows, and give P4-09 its own
   file rather than sharing P4-05's.

None of these are decisions this document makes. They are the mechanical
distance between the current ledger and a PROMOTE.

---

## 7. Adversarial review record

Per the owner's standing instruction that any plan-level artifact be reviewed
by Codex `gpt-5.6-sol` at medium reasoning effort before being treated as
final, this ledger was reviewed on 2026-08-20.

- Prompt: `docs/m7-session-logs/sol-review-prompt.md`
- Full run log: `docs/m7-session-logs/sol-review-run.log`
- Findings as delivered: `docs/m7-session-logs/sol-review-findings.md`

The review's headline was that the ledger was **"not sound as-is."** That was
substantially correct. Changes made in response:

| # | Review finding | Disposition |
|---|---|---|
| 1 | 74-row tally illegitimately mixed frozen rows with 8 invented ones; frozen universe is 66 | **Accepted** — §1.3, §5.1 now tally 66 frozen + 8 addenda separately |
| 2 | Strict evidence rule applied "only when it produces a lower verdict" | **Accepted** — §1.1 states the rule uniformly; §1.2 records the path-binding defect once, globally |
| 3 | P4-08 proves selection, not "after restart" | **Accepted** — downgraded to UNEVALUATED |
| 4 | P4-09 has no artifact of its own and shows only a snapshot | **Accepted** — downgraded to UNEVALUATED |
| 5 | Phase 5 fixture substitution had no pre-frozen equivalence mapping | **Accepted** — P0-04 revised to FAIL as a contract condition, with consequences named for P5-06/P5-09/P7R-01/P7R-02 |
| 6 | Plan does not "reserve" owner authority to override the all-gates-PASS bar | **Accepted** — §4 corrected; ruling 6 restated as a deviation from planned ordering |
| 7 | "Shipped in code" is imprecise | **Accepted** — §4 now states `e2ffe34` is branch-only: not merged, not tagged, not released |
| 8 | 27-vs-34 arithmetic inconsistency | **Accepted** — fixed |
| 9 | No HOLD/BLOCKED precedence rule in §13 | **Accepted** — recorded in §5.2 as a contract defect |
| 10 | Traceability claim (25 rows with no raw item, 3 Mac-only) | **Independently verified as accurate** by the reviewer |
| 11 | Both FAILs are not contract-valid; outcome should be HOLD | **Rejected, with reasons** — §5.5 |
| 12 | P1-03 should be UNEVALUATED (amd64 hello not proven live) | **Rejected** — see below |
| 13 | P2-03 should be PASS, not BLOCKED | **Rejected** — see below |
| 14 | P5-12 should be UNEVALUATED (cites three files, not one) | **Rejected** — see below |

### The three row-level rejections, stated so they can be overruled

- **P1-03 (kept PASS).** The review is factually right that the amd64 half of
  the hello contract rests on source plus a unit test run on arm64, not a live
  amd64 handshake. But this row was adjudicated and frozen during Phase 1 with
  that limitation **disclosed in its own verdict text**, and no amd64 Linux
  target existed in-guest before Phase 2. Re-scoring a frozen, honestly-caveated
  Phase 1 adjudication from Phase 7 is a larger act than it appears. Recorded
  as a live challenge; it does not affect the outcome.
- **P2-03 (kept BLOCKED).** The review argues the gate is conditional ("when
  owner-installable"), FEX was not owner-installable, qemu-user qualified, and
  so nothing remained blocked — making it PASS. That reading is plausible, but
  moving a row **up** to PASS on an argument rather than an artifact is exactly
  the direction this document must not travel, and BLOCKED is the conservative
  label (it cannot coexist with PROMOTE either way). Kept as frozen.
- **P5-12 (kept PASS).** The row requires a mount/binfmt/process inventory
  **before, during, and after** — three temporal captures by definition. Citing
  three files for a three-part gate is not the "alternate copy" the contract
  forbids; the contract's one-item rule is simply ill-fitting here, which is
  itself worth noting as a contract defect.

Nothing in this review was allowed to round any `UNEVALUATED` or `FAIL` up to
`PASS`, and nothing was allowed to blur the owner-override / mechanical-PROMOTE
distinction — the review in fact **sharpened** that distinction, and §4 is
stricter now than before it.

---

## 8. Handoff pointer

Per §13, `docs/macos-m7-handoff.md` "lists remaining work without implying
unrun checks passed." Section 6 above is the authoritative remaining-work list
for the promotion decision specifically. Nothing in this document should be
read as asserting that any unrun check passed; the 29 UNEVALUATED frozen rows are
unrun or unrecorded, and they are labelled as such rather than rounded.
