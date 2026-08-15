Revise `docs/macos-m6-plan.md` in `J:\Claude code\iolab-m6-wt` (branch
`luna/macos-m6-followups`, now rebased onto `luna/macos-m5-honest-caps` @
`7b7b6ec`) to fold in every confirmed finding from
`docs/macos-m6-plan-review.md`, plus one additional correction below that the
review itself got a detail wrong on. This is still a **planning pass only** —
do not implement anything outside `docs/`. Edit the plan file in place (or
rewrite it); do not create a second plan file.

## The one correction to the review's own finding

`docs/macos-m6-plan-review.md` MAJOR-1 is right that the plan's premise was
wrong, but double check the exact current wording of `docs/macos-m5-handoff.md`
and `docs/macos-m5-result.md` on this branch (`7b7b6ec`) before restating
it — they were mid-edit by a concurrent session when the plan and review were
first written, and have since been corrected/committed twice more
(`a387c76`, `7b7b6ec`). Read them fresh, now, and use whatever they currently
say as ground truth. Do not trust the summary in this paragraph or in the
review file over the actual current file contents.

## Required changes

1. **Correct M5's status throughout.** Wherever the plan currently says M5
   criterion 2 (the amd64 non-Mac target) was NOT ATTEMPTED, blocked, or
   excluded by owner direction (§9 risk 2, §10 non-goal, and anywhere else),
   replace it with the actual current status from the M5 docs you just read.
   If M5 is now fully PASS, say so, drop the "M5 criterion 2 out of scope"
   non-goal/risk (there is nothing left to exclude), and make sure nothing
   else in the plan quietly depended on criterion 2 still being open.
2. **Fix CRITICAL-1** (human/interactive-desktop access gate): elevate this to
   an explicit top-level pre-implementation gate section near the top of the
   plan (before §1 or as new §0). Name exactly what owner action is required
   before a `luna-xhigh` hardware session can start: a fresh macOS account
   with working SSH access AND a way for the automated session to either (a)
   receive an interactive-desktop/browser-control channel into that account,
   or (b) have specific steps in §7.2 that require a real GUI browser
   performed BY the owner directly with transcripts/screenshots handed to the
   session, while everything else (curl path, CLI verification, lab/upgrade/
   uninstall over SSH) remains automatable. Rewrite §7 so it's explicit about
   which steps are agent-executable over SSH versus which require the owner's
   own hands, rather than presenting the whole sequence as if one session
   performs all of it.
3. **Fix CRITICAL-2** (baseline vs candidate contradiction in §7.2/§7.4):
   design a concrete, reproducible baseline recipe as the finding suggests
   (the reviewed M6 packer run against a clean `cf19f2f`-or-later-baseline-
   commit checkout for the launcher/native payload, not literally rebuilt at
   `cf19f2f` which predates the packer), give it a distinct evidence filename
   distinct from the candidate, and reorder §7 so the baseline installs and
   proves a working lab FIRST, then the candidate is independently downloaded
   through the curl path in §7.5 and upgraded to in §7.4. Be explicit this
   baseline archive is a qualification artifact, not something published in
   the release.
4. **Fix MAJOR-2** (draft-release download contract): state plainly whether
   this repo/its releases are public or private, and specify a real,
   GitHub-supported download method for both the browser step and the curl
   step (e.g. `gh release download` or a documented signed/authenticated URL
   for curl if private; a plain public URL if public) — check the actual repo
   visibility if you can determine it from the worktree remote, otherwise
   state the assumption explicitly and flag it as something to confirm before
   the hardware session.
5. **Fix MAJOR-3** (packer payload-hash invariant with no trusted input):
   add an explicit `--payload-sha256` (or manifest-checksum) input to the
   packer's defined flags in §2.3/Task 2, and make the packer itself verify
   it, OR explicitly move that invariant to the release job only and correct
   §2.3's wording so it doesn't claim the packer does something Task 2 never
   gives it the means to do. Pick one design and make both sections agree.
6. **Fix MAJOR-4** (concrete tar/gzip reproducibility recipe): add the
   specific flags/approach from the finding (GNU tar, `LC_ALL=C` sorting,
   normalized modes/uid/gid, `--mtime` from `SOURCE_DATE_EPOCH`, pax
   atime/ctime stripped, `gzip -n`) to Task 2/3, and require the
   reproducibility test to build from two independently staged directories,
   not a reused stage.
7. **Fix MAJOR-5** (incomplete member-type contract test): expand Task 2.6 /
   `release-layout-test.sh`'s required checks to cover hard links, device
   nodes, xattrs/ACL pax headers, AppleDouble/`.DS_Store` patterns, and that
   every manifest source (not just launcher/payload) is a regular
   non-symlink file.
8. **Fix MAJOR-6** (no Mac-side payload identity comparison): add a concrete
   step to §7 that brings the trusted `build-linux` payload checksum (e.g.
   `SHA256SUMS-ci.txt` from the same workflow run) to the Mac and compares it
   by exact basename against the shipped payload, with both provenance and
   result recorded in evidence.
9. **Fix MAJOR-7** (INSTALL.md version-count/false-history problem): change
   Task 4's INSTALL.md instructions so the artifact-count/version language
   describes the release that will actually contain M6 (not literally
   "v0.5.2"), using whatever framing (current release / tag-relative) avoids
   claiming the old release contains the new archive.
10. **Fix MAJOR-8** (destructive commands as vague prose): rewrite §7.6 with
    literal, quoted, absolute-path commands, explicit Trash destination
    names, and `test`/`pwd`/`limactl list` preconditions before every
    delete/move, exactly as the finding describes.
11. **Fix MINOR-1** (asset-lookup description): correct §2.1's description of
    `resolveAssetRoot`/`selectPayload` to match the actual search order the
    finding describes, and note the exact-one-payload invariant is what
    makes mtime-based selection harmless.
12. **Fix MINOR-2** (nonexistent embedded launcher version claim): remove the
    "embedded launcher version" identity claim from §2.3, or explicitly scope
    a small, separately-called-out ldflag version-stamp addition to Task 1/3
    if you judge it's worth adding — pick one and make the rest of the plan
    consistent with that choice.
13. **Fix MINOR-3** (INSTALL.md table/ordinal mismatch): correct Task 4 to
    account for the actual current row count and the Docker "seventh option"
    ordinal becoming eighth once the Mac row is added.
14. **Fix MINOR-4** (no assigned result/handoff task): add an explicit final
    task (e.g. producing `docs/macos-m6-result.md` and
    `docs/macos-m6-handoff.md`, matching the M1-M5 convention already used in
    this repo) to §3's task list.
15. **Fix NIT-1** (archive README INSTALL.md link): require an absolute,
    tag-pinned HTTPS link instead of a relative repo path.

## Working rules

- Sandbox `workspace-write`. Edit `docs/macos-m6-plan.md` for real.
- Do not touch anything outside `docs/`.
- After revising, do a final self-check: re-read your own updated §8
  acceptance table, §7 verbatim steps, and §9/§10 risks/non-goals together and
  confirm they're now internally consistent (this was itself a review
  finding pattern — criteria referencing evidence steps produce, not steps
  nothing verifies).
- End your final message with a short list of exactly what changed, referencing
  each review finding ID you addressed.
