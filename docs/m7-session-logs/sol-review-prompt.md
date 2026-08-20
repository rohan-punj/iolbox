Adversarial review of a gate ledger. Read-only; do not edit anything.

TASK: Review `docs/macos-m7-result.md` (in this worktree, J:\Claude code\iolab-m7-phase4-wt) against the exact rules in section 13 of `docs/macos-m7-plan.md`. That plan file is NOT in this worktree -- read it at `J:\Claude code\iolab-m7-wt\docs\macos-m7-plan.md`, section 13 ("Phase 7 - mechanical promotion decision"), plus section 11 (Phase 5 exit rule) and section 12 (Phase 6 thresholds).

Also read, as the authoritative frozen inputs:
- `J:\Claude code\iolab-m7-wt\docs\m7-evidence\phase0\gate-ledger.md` (the frozen atomic row contract)
- `J:\Claude code\iolab-m7-wt\docs\m7-evidence\phase0\threshold-approval.md` (approved thresholds + decision rules + the owner's ship-decision ruling)
- `docs/macos-m7-phase5-handoff.md`, `docs/macos-m7-phase6-handoff.md`, `docs/macos-m7-phase7-mature-reverify.md` (in THIS worktree)
- spot-check actual evidence files under `docs/m7-evidence/phase4/`, `phase5/`, `phase6/` in this worktree

ANSWER THESE QUESTIONS SPECIFICALLY:

1. EVIDENCE TRACEABILITY. Does every row cite real, traceable RAW evidence rather than a prose summary? Spot-check at least 10 cited paths and report any that do not exist, or that are prose docs masquerading as raw evidence. The ledger claims 25 of 74 rows cite no raw item at all and 3 more cite Mac-only paths -- verify that claim.

2. MISCATEGORISED ROWS. Is any row rounded the wrong way? Look hard in BOTH directions: (a) something genuinely UNEVALUATED or FAIL being scored PASS; (b) something genuinely PASS being over-strictly scored UNEVALUATED. In particular scrutinise: P0-01..P0-05 (added by this doc; the frozen contract has no Phase 0 rows -- is adding them legitimate under section 13 category 1, or is it inventing gates?); P5-04 (scored UNEVALUATED because Wireshark was unavailable and multi-link/soak pcapng is missing, though a valid single-link pcapng exists); P5-08 extnet (scored UNEVALUATED, argued it would be BLOCKED if its probe output were committed -- which is right?); P5-09 (scored FAIL despite an owner waiver); P4-09 (PASS on a citation shared with P4-05); P4-13 (UNEVALUATED because `diagnose` was never run even though `status` fully passes).

3. IS THE OUTCOME ENTAILED? The doc computes NO-PROMOTE. Verify this is actually entailed by the rows AS SCORED under section 13's mutual-exclusivity rules (PROMOTE / NO-PROMOTE / HOLD / BLOCKED). Check the reasoning that HOLD and BLOCKED are unavailable because "no gate has failed" is false. If the outcome does not follow, say what does.

4. THE ROSETTA FOUR-NODE ROW (P7R-02) -- THE CENTRAL QUESTION. It is scored FAIL: rosetta-amd64 four-node capacity, standalone/fresh position, hard wall 0/2 across two independent fresh-VM runs, phase.json status "UNVERIFIED", all four IOL processes still alive (no OOM), driver EOF at ~110-116s, two of four consoles never advancing past their first line.
   There is an UNRESOLVED CONFOUND: the two arms differ in build vintage as well as execution mode -- rosetta ran the M6-vintage launcher+payload; native ran current HEAD with two netprobe console fixes merged since. So the result could be (a) a genuine Rosetta capacity limit, or (b) an M6 payload multi-console defect already fixed at HEAD. This was not eliminated.
   ALSO: the raw evidence for this row lives ONLY on the Mac (`~/iolbox-p7-build/src/evidence-m4-p7/rosetta/...`) and was never committed to the repo.
   Questions: Is FAIL the correct section-13 label given the confound, or does section 13 / the approved threshold rules demand a different label (UNEVALUATED? BLOCKED?)? Note the approved rule "Any completed run exceeding an approved threshold is FAIL and forces NO-PROMOTE; it cannot be reclassified as HOLD or 'insufficient sample'" versus the contract rule that a row cannot be closed by evidence that is not the named raw file. These two rules appear to pull in opposite directions for this row -- which governs? Does the confound need to be RESOLVED before this ledger can be called final, or can the ledger be final with the confound recorded as a caveat?
   Note also: rosetta-amd64 is NOT being removed; it remains the explicit fallback path.

5. PROMOTE vs OWNER OVERRIDE. The doc insists a mechanical PROMOTE never occurred and that native-arm64's promotion rests on an explicit owner override ruling plus personal GUI validation. It also notes the code change flipping `auto` to prefer native (commit e2ffe34) shipped BEFORE any mechanical outcome, contrary to plan section 10.3's "until promotion, auto continues to default to Rosetta". Is that distinction drawn correctly and completely? Is anything conflated? Is the doc too harsh or not harsh enough about the ordering?

6. Anything else materially wrong, missing, internally inconsistent, or arithmetically incorrect (check the tally: 44 PASS / 2 FAIL / 1 BLOCKED / 27 UNEVALUATED = 74 rows).

Be adversarial and concrete. Cite file:line or row IDs. Where you disagree with the ledger, state the label you would assign instead and the exact rule text that compels it. Do NOT recommend rounding any UNEVALUATED or FAIL up to PASS unless a specific raw artifact actually supports it. Structure your answer as: (A) verdict on whether the ledger is sound as-is, (B) numbered list of required fixes, (C) numbered list of optional/judgement-call suggestions, (D) your answer to the P7R-02 classification question.
