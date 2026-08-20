## (A) Verdict

The ledger is **not sound as-is**.

Two different conclusions must be separated:

1. **Using the 74 rows exactly as the document currently scores them, `NO-PROMOTE` is mechanically entailed.** Two rows are labelled `FAIL`, and section 13 says any evaluated `FAIL` forces `NO-PROMOTE` ([plan:408–429](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:408>)).

2. **Those are not valid section-13 scores over the frozen ledger.** The document adds eight rows, inconsistently enforces the canonical-evidence rule, and derives both `FAIL`s from evidence that it simultaneously says cannot close a row. After applying the frozen contract consistently, neither current `FAIL` is contract-valid. The defensible current outcome is therefore **HOLD / EXPERIMENTAL**, with the additional defect that section-13 category 1 is absent from the frozen contract.

The authored arithmetic `44 + 2 + 1 + 27 = 74` is correct, but **74 is the wrong row universe**. The frozen ledger has 66 rows. Five `P0-*` and three `P7R-*` rows were added later.

## (B) Required fixes

1. **Restore the frozen row universe; do not count P0 or P7R as new gates.**

   The frozen preamble says Phase 7 “recomputes the atomic rows below and does not add a second summary gate” ([gate-ledger:6–10](<J:/Claude code/iolab-m7-wt/docs/m7-evidence/phase0/gate-ledger.md:6>)). Nevertheless:

   - `P0-01..P0-05` are added because category 1 was omitted ([result:100–105](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:100>)).
   - `P7R-01..P7R-03` are described as recomputations of P6 rows but then counted as three additional gates ([result:272–288](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:272>)).

   That is impermissible double-counting. Section 13 category 1 really is missing, but the remedy is to amend and re-freeze the contract—not invent five retrospective gates during Phase 7. The P7R observations must be bound to the existing P6 rows or remain supplementary observations.

2. **Apply the canonical-path rule consistently.**

   The frozen contract requires exactly one bound canonical raw item, and says an alternate copy cannot replace it ([gate-ledger:6–10](<J:/Claude code/iolab-m7-wt/docs/m7-evidence/phase0/gate-ledger.md:6>)). Yet the result accepts numerous alternate layouts:

   - Frozen Phase 4 paths require `phase4/<machine-id>/<run-id>/...`; the accepted files are flat `phase4/scenario*.log`.
   - Frozen Phase 5 paths require `phase5/<machine-id>/<run-id>/matrix/<row>/row.log`; the accepted files are under `m3-rerun/...` or `native-arm64/...`.
   - `P5-12` cites three brace-expanded files, not one canonical item.
   - `P7-02` substitutes the Phase 3 selection log for the frozen Phase 7 legal file.
   - `P7R-02` cites two directories, not one raw file.

   Either formally bind/migrate these artifacts to the frozen paths or score the rows `UNEVALUATED`. The current document selectively invokes the strict rule only when it produces a lower verdict.

3. **Correct the evidence-traceability accounting.**

   I verified the claimed **25 rows with no raw item**:

   - P4: `P4-01`, `02`, `11`, `14`, `15` — 5
   - P5: `P5-06..P5-11` — 6
   - P6: `P6-01..P6-12` — 12
   - P7: `P7-01`, `P7-03` — 2  
   - Total: **25**

   I also verified that `P7R-01..03` cite **three Mac-only locations** that are unavailable from either supplied worktree. So that literal claim is accurate—although the 74-row denominator is not legitimate.

   Spot-checks of more than ten citations found:

   - `P4-03`, `P4-04`, `P4-05`, `P4-06`, `P4-07`, `P4-08`, `P4-10`, and `P4-12`: files exist and contain command output, but are noncanonical alternate paths.
   - `P4-09`: its cited P4-05 file exists, but is already another row’s artifact and does not prove restart persistence.
   - `P5-01`, `P5-02`, `P5-03`, `P5-04`, and `P5-05`: cited files exist and are raw outputs, but none occupies the frozen row path.
   - `P5-12`: all three inventory files exist, but the row cites three artifacts rather than one.
   - P1-01, P2-02/P2-03, and P3-06 exist in the reference worktree and are genuine raw records. They are traceable, although the result’s relative links are broken from the promotion worktree.
   - P0 citations are prose/attestation decision documents. They are authoritative frozen inputs, but they are not canonical raw items for frozen ledger rows because those rows do not exist.
   - `P5-09` is the clearest prose-summary masquerade: it says `NONE` and assigns `FAIL` from `STATUS.md`/handoff prose ([result:P5-09](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:205>)).

4. **Correct the materially wrong row labels.**

   - **P1-03: `UNEVALUATED`, not `PASS`.**  
     The plan requires a real hello handshake and both architecture values ([plan:173–180](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:173>)). The amd64 half is source plus a unit test executed on arm64, not a live amd64 handshake. Static/source evidence cannot substitute for live execution.

   - **P2-03: `PASS`, not `BLOCKED`.**  
     Its gate is conditional: FEX must run “when owner-installable.” The canonical JSON establishes that it was not owner-installable without an unapproved ad-hoc build and was not considered eligible. The plan says FEX installation blocks only “if FEX is to be considered” ([plan:238–242](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:238>)). It was not. Nothing remained blocked because qemu-user qualified.

   - **P4-08: `UNEVALUATED`, not `PASS`.**  
     The raw file explicitly says the machine was already running and no VM creation/restart occurred. It proves persisted selection during `status`, not “honored after restart” ([scenario6:2–4](<J:/Claude code/iolab-m7-phase4-wt/docs/m7-evidence/phase4/scenario6-persisted-choice-honored.log:2>)).

   - **P4-09: `UNEVALUATED`, not `PASS`.**  
     Beyond the noncanonical/shared citation, the log shows two VM entries and a sync import. It does not prove state isolation plus host-sync preservation through the required lifecycle. The result itself calls the citation weak ([result:P4-09](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:179>)).

   - **P4-13: `UNEVALUATED` is correct.**  
     `status` passing cannot close a row that explicitly requires `status/diagnose`; `diagnose` was not run ([result:P4-13](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:183>)).

   - **P5-04: `UNEVALUATED` is correct.**  
     The existing single-link pcapng proves a useful subclaim, but the row requires multi-link and soak capture evidence too. Wireshark unavailability alone is not necessarily fatal because the plan allows browser saving, but the missing multi-link/soak artifacts prevent `PASS` ([plan:323–335](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:323>)).

   - **P5-08: current ledger label `UNEVALUATED`; underlying execution state `BLOCKED`.**  
     If the named raw host-interface probe were committed, section 11 compels `BLOCKED`: “An unavailable extnet … is BLOCKED and prevents promotion” ([plan:342–346](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:342>)). With no canonical raw item, however, the ledger cannot close it even as `BLOCKED`. This is the same two-layer distinction the document fails to apply to P7R-02.

   - **P5-09: `UNEVALUATED` in the current ledger, not `FAIL`.**  
     If its raw failed run were committed and canonically bound, it would be `FAIL`; the owner waiver would not rescore it. But currently the result assigns `FAIL` from prose while explicitly stating “No raw artifact in the repo.” That violates its own rules 1–2. The waiver does not produce `PASS`, but neither does prose produce a contract-valid `FAIL`.

   - **P5-12: `UNEVALUATED` under the exact one-item contract.**  
     The underlying evidence looks substantively persuasive, but the row cites three files rather than one frozen canonical `row.log`.

5. **Treat the changed Phase 5 fixture as a gate-contract violation, not a “PASS with deviation.”**

   `P0-04` admits that the frozen four-node fixture was replaced during Phase 5 without a Phase 0 mapping ([result:P0-04](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:112>)). Section 11 permits a replacement only when its Phase 0 owner-approved equivalence mapping exists ([plan:318–321](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:318>)).

   Therefore:

   - `P0-04` cannot legitimately be counted as an invented `PASS`.
   - Any P5/P7 measurement using the replacement cannot close the original frozen fixture gate without a valid pre-frozen mapping.
   - This cannot be repaired retrospectively merely by documenting the deviation.

6. **Recompute the outcome from contract-valid rows.**

   As authored, `NO-PROMOTE` follows from the authored scores. The document’s HOLD/BLOCKED discussion is logically correct *given those scores*: their predicates require “no gate has failed,” and the ledger says two have failed.

   But after removing the eight unauthorized rows and correcting the unsupported classifications:

   - Frozen rows: **66**
   - Contract-valid `FAIL`: **0**
   - Contract-valid `BLOCKED`: **0** at present, because P5-08 lacks its raw blocking probe
   - Conservative tally: approximately **23 PASS / 43 UNEVALUATED**
   - Outcome: **HOLD / EXPERIMENTAL**

   That tally includes downgrading every Phase 4/5 alternate-path result under the exact named-file rule and correcting P1-03/P2-03. If the P5-08 probe is canonically recorded, the underlying row becomes `BLOCKED`; section 13 then has an ambiguity because both HOLD and BLOCKED predicates can be true when UNEVALUATED and BLOCKED rows coexist. It declares mutual exclusivity but gives no precedence rule.

7. **Correct the owner-override interpretation and ordering.**

   The document correctly distinguishes “mechanical PROMOTE” from the owner’s product decision. It is wrong when it says the plan “reserves” authority to override the all-gates-pass requirement ([result:395–409](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:395>)).

   The actual rule is one-way:

   - Mechanical PROMOTE enables native eligibility.
   - Even then, merge/tag/publish needs separate owner sign-off ([plan:410–419](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:410>); [threshold approval:48–62](<J:/Claude code/iolab-m7-wt/docs/m7-evidence/phase0/threshold-approval.md:48>)).

   It does **not** say owner sign-off substitutes for mechanical PROMOTE. Section 10 is explicit: “Until promotion, `auto` continues to default to Rosetta” ([plan:289–293](<J:/Claude code/iolab-m7-wt/docs/macos-m7-plan.md:289>)).

   Therefore `e2ffe34` was not merely “ahead of any mechanical outcome”; it was a documented **exception to/violation of the planned ordering**. Personal GUI validation was necessary for ship authorization after PROMOTE, but it did not cure the missing PROMOTE prerequisite. The result is too lenient here.

   Also, “shipped in code” should be replaced with the exact state—landed on the integration branch, merged to main, included in an artifact, or released. Those are materially different events.

8. **Fix the internal arithmetic inconsistency.**

   The table says 27 UNEVALUATED ([result:426–437](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:426>)), but the next paragraph says 34 ([result:441–443](<J:/Claude code/iolab-m7-phase4-wt/docs/macos-m7-result.md:441>)). The table arithmetic sums correctly; “34” is erroneous.

## (C) Optional / judgement-call suggestions

1. Maintain separate fields for:

   - observed run result;
   - evidence admissibility;
   - ledger verdict.

   That would allow “observed FAIL, ledger UNEVALUATED because canonical artifact missing” without forcing contradictory prose.

2. Add an explicit precedence rule between ledger-level HOLD and BLOCKED. Section 13 currently allows both predicates to be true when one row is BLOCKED and another is UNEVALUATED.

3. Keep cross-branch evidence if desired, but use immutable commit-qualified links or an evidence manifest. The Phase 0–3 raw files are real and traceable in the supplied reference worktree; calling them nonexistent overstates the problem. The actual defect is that the result’s relative links do not resolve from its own branch.

4. Preserve the Phase 7 Rosetta results as supplementary compatibility findings. Because Rosetta remains the explicit fallback, a four-node defect in that path is material even if it does not become a newly invented ledger gate.

## (D) P7R-02 classification

The clean resolution is:

- **Run-level observation:** `FAIL`.
- **Current ledger-level verdict:** `UNEVALUATED`.
- **`BLOCKED`: incorrect.**

The two rules do not actually conflict; they govern different stages:

1. The threshold rule classifies a completed measured run. The M6 Rosetta artifact completed two attempts and failed the approved four-node capability. That observation receives `FAIL`; it cannot be softened to “insufficient sample” merely because only two runs were collected.

2. The canonical-evidence rule controls whether that observation can close a ledger gate. Here it cannot:

   - `P7R-02` is not a frozen gate.
   - It cites two directories rather than one canonical raw file.
   - Both directories are Mac-only.
   - It purports to recompute P6-08/P6-11 while being counted in addition to them.

Therefore the measured failure should be recorded outside the tally, while the applicable frozen P6 row remains `UNEVALUATED` until one canonical artifact is bound.

The confound does **not** justify changing the run-level `FAIL`: either the shipped M6 Rosetta path has a capacity limitation or it has a four-console payload defect. Both are failures of the exact shipped baseline artifact that section 12 asked to run. But the confound sharply limits the causal claim: the evidence establishes only that **the M6-vintage Rosetta artifact failed four-node operation on this Mac**, not that Rosetta translation itself is the cause.

The ledger can be finalized now only as **HOLD / EXPERIMENTAL with P6 capacity UNEVALUATED**. The confound must be resolved before claiming a Rosetta mechanism capacity limit or treating the result as representative of a current-HEAD fallback payload. It need not be resolved merely to preserve the historical run-level `FAIL` caveat.