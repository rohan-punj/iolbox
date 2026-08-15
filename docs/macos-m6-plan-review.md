# Adversarial review of `docs/macos-m6-plan.md`

Verdict: NOT READY FOR IMPLEMENTATION. The plan is substantially grounded in
the repository and preserves the broad M6 boundary, but two critical defects
make the required hardware qualification impossible as written. Several major
gaps would also prevent the evidence from satisfying the plan's own acceptance
table.

## CRITICAL findings

### CRITICAL-1 — Sections 5, 7.1, and 9.3 depend on human and GUI access that the implementation session does not have

The only concrete access supplied by the plan is SSH as
`rohansharma@192.168.101.166`. Sections 5 and 7.1 correctly forbid treating that
account, `sudo -iu`, or SSH alone as the fresh interactive account, but they
then require the owner to create a disposable macOS account, log into its Aqua
desktop, enable test access, use a real browser, handle possible Finder or
Gatekeeper UI, and later supply a licensed IOL image. A non-interactive Codex
session driving the Mac through the stated SSH login cannot perform those
actions, and the plan supplies neither credentials/SSH access for the clean
account nor a remote desktop/browser-control path into that account.

This is acknowledged in prose, but it remains a hard external prerequisite,
not an executable step. Without prior owner action, criteria 3–8 cannot be
completed and the session cannot reach M6 PASS. Elevate this to a top-level
pre-implementation gate: name the owner actions, require the clean username and
working SSH plus interactive desktop/browser-control channel to be handed off,
and require the owner-supplied IOL test input to be available by an agreed safe
path. The implementation session should stop before spending hardware time if
that gate is not met. If the owner must personally perform browser/Finder
steps, split those steps out explicitly and define how the automated session
receives their transcripts/screenshots.

### CRITICAL-2 — Sections 7.2 and 7.4 contradict each other about whether the initial install is the baseline or candidate

Section 7.2 downloads the unversioned `iolbox-macos-arm64.tar.gz` from the M6
draft, runs `./iolbox start`, and builds the real lab. Section 7.4 then says
“the preceding start/lab steps must initially use” a baseline archive built
from `cf19f2f`, and only afterward downloads the M6 candidate. Those statements
cannot both be true: the draft's unversioned asset is the candidate. Task 6.5
mentions baseline/candidate evidence copies, but section 7 never says how the
baseline reaches the clean account or how the browser download selects it.

There is a second impossibility hidden here: `cf19f2f` predates the proposed M6
packer and release job, so a checkout of that commit cannot itself produce the
claimed M6-format baseline archive. Define a reproducible hybrid baseline
recipe (for example, the reviewed candidate packer at a named commit plus the
launcher and native payload built from a clean `cf19f2f` checkout), state the
resulting provenance honestly, give the baseline a distinct evidence filename,
and rewrite the qualification order so 7.2 starts the baseline first. Then
download the actual draft candidate through the independent `curl` path and
upgrade to it. Do not call the hybrid baseline “workflow-produced” unless a
workflow really produced it.

## MAJOR findings

### MAJOR-1 — Sections 9.2 and 10 state the opposite of the required M5 handoff/result

The plan says M5 criterion 2 was NOT ATTEMPTED and was excluded by owner
direction. `docs/macos-m5-handoff.md` lines 12–18 and
`docs/macos-m5-result.md` lines 18–29 and 154–183 say it passed on the real
ordinary amd64 `noble-builder-vm`, with evidence retained. This is a direct
grounding error, and the invented “owner-supplied scope” has no support in the
required sources. Remove that risk and non-goal, and describe M5 as PASS while
retaining the genuinely current warning that M4 is PARTIAL.

### MAJOR-2 — Sections 7.2 and 7.5 do not provide a workable authenticated draft-release download contract

A GitHub draft release is not an ordinary public download directory. A fresh
browser profile must be authenticated and authorized to see a private/draft
release, and appending `/iolbox-macos-arm64.tar.gz` to a generic
`<draft-release-url>` is not a reliable GitHub asset URL for `curl`. The plan
does not state how the clean browser account is authenticated or how `curl`
authenticates without leaking a token into shell history/evidence. As written,
the first hardware download can fail before quarantine is testable.

Specify whether the repository/draft is public or private, the exact browser
access prerequisite, and a GitHub-supported asset download method. For private
drafts, use a narrowly scoped credential supplied outside transcripts and an
API/`gh release download` procedure that does not print the token; then verify
the downloaded response is the archive, not HTML. Preserve the requirement
that the browser path is a genuine browser download rather than replacing it
with CLI automation.

### MAJOR-3 — Section 2.3 requires the packer to compare payload identity but Task 2 gives it no trusted expected hash

The plan says the packer itself must fail when the payload hash differs from
the artifact downloaded from `build-linux`. Its defined inputs are only
`--launcher`, `--payload`, `--version`, `--output`, and
`--source-date-epoch`. A packer cannot know the trusted `build-linux` hash from
those values. Task 3 performs an external checksum check, but that does not
satisfy the stated packer invariant.

Either add an explicit trusted `--payload-sha256`/checksum-manifest input and
make the packer compare it, or assign the invariant solely to the release job
and revise section 2.3 accordingly. In either design, select exactly one
checksum line by exact basename and reject duplicates or malformed manifests.

### MAJOR-4 — The reproducibility requirement is stated as an outcome but lacks the concrete tar/gzip contract needed to guarantee it

Section 2.3 recognizes timestamps, ownership, modes, order, and the gzip clock,
but Tasks 2 and 3 do not select a tar implementation or require concrete flags.
Default `tar -czf` is not sufficient: filesystem enumeration order, uid/gid,
mode bits, mtimes, locale-sensitive sorting, pax atime/ctime records, and gzip
headers can vary. The two-build test can pass accidentally on one runner while
remaining non-reproducible elsewhere.

Because the job is explicitly Ubuntu, require GNU tar and a concrete recipe,
such as bytewise `LC_ALL=C` ordering, normalized modes, `--mtime` from validated
`SOURCE_DATE_EPOCH`, numeric owner/group zero, a fixed tar format with pax
atime/ctime eliminated, and piping the tar stream through `gzip -n` instead of
using unspecified gzip defaults. The test must use two independently created
staging directories and compare archive bytes/hashes, not simply repack the
same lingering stage.

### MAJOR-5 — Section 2.2 forbids several tar member types/metadata that Task 2.6 never tests

The archive contract bans hard links, device nodes, extended attributes,
AppleDouble files, and resource forks in addition to symlinks. Task 2.6 only
calls out symlinks and filename/content patterns. The packer's regular-file
checks in Task 2.3 are limited to launcher and payload, not every manifest
source. Therefore the task list does not establish the complete section 2.2
contract even though acceptance criterion 2 demands the exact layout.

Require every manifest source to be a regular, non-symlink file; inspect tar
type flags to reject hard links and special nodes; inspect member names for
`._*`/`.DS_Store` and resource-fork patterns; and verify no xattr/ACL pax
headers are emitted. Make those checks explicit in `release-layout-test.sh`.

### MAJOR-6 — Acceptance criterion 2 demands a Mac-side payload comparison that no section 7 step produces

Criterion 2 requires “payload hash comparison on the Mac” against the same
run's `build-linux` payload. The clean-machine steps download only the Mac
archive and outer checksum. They verify `SHA256SUMS`, but never download the
Linux artifact/checksum or record an independently trusted expected payload
hash on the Mac. An internal checksum proves self-consistency, not identity to
`build-linux`.

Add a narrow step that brings the exact `SHA256SUMS-ci.txt` (or a separately
published exact payload checksum) from the same workflow run to the Mac,
extracts the one payload member, and compares the hashes by exact basename.
Record both provenance and output in the evidence directory.

### MAJOR-7 — Task 4 would make `docs/INSTALL.md` historically false by changing only the count

The current guide says “The v0.5.2 release publishes six deployable artifacts”
and hard-codes v0.5.2 filenames throughout. M6 did not exist in that release.
Changing “six” to “seven” and adding the Mac section while retaining “v0.5.2”
would falsely claim that the old release contains the new archive. The plan
also says not to rewrite the other six sections, leaving no stated strategy for
the version references.

Make the header/table describe the release that will actually contain M6,
preferably using “current release” plus tag-relative download guidance or a
deliberate version update applied consistently. This can remain a narrow docs
change without redesigning the six existing install procedures.

### MAJOR-8 — Section 7.6's destructive filesystem operations are prose, not maximally narrow commands

The VM command is explicit, but “move only the extracted launcher/archive
directories to Trash” and “move the explicit ... data directory ... to Trash”
do not name source paths, destination names, quoting, collision handling, or
pre-delete validation. There will be multiple browser/curl/baseline/candidate
extraction directories with the same top-level name, so this ambiguity is
material. A mistaken current directory or unquoted `Application Support` path
could move the wrong tree or make the proof invalid.

Provide literal, quoted commands using already resolved absolute paths, unique
Trash destination names, and `test`/`pwd`/`limactl list` preconditions. Verify
the machine name equals `iolbox-debian13` and is stopped immediately before
`limactl delete`. Continue to forbid globs, recursive deletion, and broad
`~/.lima`/`~/.iolbox` removal.

## MINOR findings

### MINOR-1 — Section 2.1 misstates the actual payload lookup contract

`resolveAssetRoot` does find `lima/profiles.env` through candidates including
the executable directory, but it checks cwd-derived repository layouts before
executable-derived candidates. `selectPayload` then searches both the resolved
asset root and, when different, the current working directory, non-recursively,
and chooses the newest candidate by mtime. It does not search only “that same
asset-root directory.” The proposed one-payload archive layout is compatible
with the code, but the grounding claim is inaccurate. Describe the real search
order and note that the release's exact-one-payload invariant makes the mtime
selection harmless without changing launcher behavior.

### MINOR-2 — Section 2.3 claims an embedded launcher version that does not exist

The Go launcher has no `version` variable, `--version` output, or ldflag stamp,
and Task 3's `go build` command supplies no version ldflag. Thus “embedded
launcher version” cannot carry archive identity. Remove that claim, or make a
separately approved launcher-version feature explicit; silently adding it in
M6 would conflict with the plan's no-launcher-redesign boundary.

### MINOR-3 — Task 4 assumes a one-row-per-artifact table that is not present and misses the Docker ordinal conflict

`docs/INSTALL.md` has six numbered artifact sections but only five comparison
rows; the raw vmdk/vmx section has no dedicated row. Adding the Mac row creates
the sixth row, not literally a seventh row. Lines 35–37 also call Docker the
“seventh” overall build-from-source option; once there are seven release
artifacts it becomes the eighth option. State the desired table shape
accurately and update that ordinal as part of the narrow consistency edit.

### MINOR-4 — The ordered task list never assigns creation of the final result/handoff record

Section 8 refers to “the implementation result,” and section 11 requires a
detailed M6 handoff, but Tasks 1–6 name neither a repository result/handoff file
nor an explicit final-report-only decision. Given the M1–M5 convention and the
amount of hardware evidence, this can leave the acceptance table only in an
ephemeral response or on the Mac. Add an explicit final documentation task
(for example `docs/macos-m6-result.md` and, if desired, a handoff) or clearly
state that the final report is the authoritative handoff and where it is
retained.

## NIT findings

### NIT-1 — The archive README's INSTALL link target is underspecified

Task 2 says the shipped README “points to `docs/INSTALL.md`,” but that relative
path is not shipped in the archive. Require a usable HTTPS link, preferably
tag-pinned so the README for an old archive does not drift with `main`.

## Checks with no additional finding

- Scope boundary: apart from the issues above, the plan does not revive Tauri,
  sign/notarize, bundle Lima/a guest disk/Cisco software, or enter M7. It
  explicitly records the owner-approved decision not to close M4's backlog.
- Current packaging tree: every Lima/profile/pin and guest script listed in
  section 2.2 exists, and excluding `iolbox-mac.sh` plus `tests/` is consistent
  with the current Go launcher release boundary.
- Workflow dependency order: `build-linux` currently uploads the native payload
  and `SHA256SUMS-ci.txt`; a `build-macos` job with `needs: build-linux` can
  download that same-run artifact before the draft-release job. Adding
  `build-macos` to the release job's `needs` is the correct dependency shape.
- Gatekeeper command itself: `xattr -d com.apple.quarantine ./iolbox` is
  file-scoped, non-recursive, and uses no `sudo` or global Gatekeeper disable.
  The plan correctly requires observed behavior to replace assumptions.
- Archive placement: putting exactly one `iolbox-server-*.tar.gz` beside
  `iolbox` will work with the current non-recursive selection code and requires
  no `--assets-dir` or `--tarball` override.

