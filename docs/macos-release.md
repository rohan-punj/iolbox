# Apple Silicon macOS release recipe

This is the maintainer checklist for the unsigned `iolbox-macos-arm64`
artifact. M6 publishes the archive only as part of the normal GitHub draft
release; it does not sign, notarize, install Lima, bundle a guest disk, or
bundle Cisco software.

## Inputs and provenance

1. Start from the intended tag commit with full history available. Record the
   implementation commit, tag/ref, `git show -s --format=%ct` value used as
   `SOURCE_DATE_EPOCH`, Go version, and the exact workflow run URL/run ID.
2. Let `build-linux` produce the native payload and `SHA256SUMS-ci.txt`.
   `build-macos` must download that same-run `iolbox-linux` artifact, select
   exactly one `iolbox-server-<release-version>.tar.gz` by exact basename,
   reject malformed/duplicate checksum lines, verify it, and pass its digest
   to `pack-release.sh`. It must not rebuild a second native payload.
3. Record the launcher hash, payload hash, checksum-manifest path, archive hash,
   and the source/ref version. The archive's outer checksum detects corruption
   but is not publisher authentication because the artifact is unsigned.

## Workflow dispatch dry run

Run the Release workflow with `workflow_dispatch` before a tag push. Confirm
the jobs `build-linux` and `build-macos` finish successfully and that the
`iolbox-macos` workflow artifact contains exactly:

~~~text
iolbox-macos-arm64.tar.gz
iolbox-macos-arm64.tar.gz.sha256
~~~

Download that workflow artifact, retain its run ID, and run the layout test or
repeat its checks manually: exact top-level layout, modes, Mach-O arm64
launcher, one versioned native payload, internal/outer checksums, no symlinks
or special nodes, no xattr/ACL pax metadata, and no AppleDouble/resource-fork,
Cisco-image, or harness files. Preserve the two independent-stage hashes and
the final archive hash.

If `gh` or an authenticated GitHub API is unavailable, record that the dry run
could not be triggered/downloaded; a local hand-assembled archive is not
workflow evidence and cannot satisfy the hardware acceptance rows.

## Tag and draft creation

After the dry run, push the intended `v*` tag through the repository's normal
process. The release workflow creates a draft and attaches the Mac archive and
outer checksum alongside the existing artifacts. Check the draft metadata and
record both Mac asset IDs plus the same-run Linux workflow artifact ID before
hardware qualification. Do not publish the draft yet.

## Hardware qualification record

Use the exact M6 plan sequence and retain the evidence root. Record:

- Apple Silicon host facts, macOS build, Lima version, memory pressure, and
  final machine inventory;
- the explicit account/access deviation if qualification uses pre-existing
  `rohansharma` state instead of a genuinely fresh account;
- browser name/version, browser download URL/tag and extraction behavior,
  authenticated curl response metadata with credentials redacted, and xattr
  output plus exit status separately for archive, directory, and binary on
  both paths;
- the baseline qualification artifact
  `iolbox-macos-arm64-baseline-7b7b6ec.tar.gz`, its hybrid input provenance,
  packer commit/hash, and an explicit statement that it was not published;
- browser candidate archive provenance, outer/internal hashes, full listing,
  exact-basename payload comparison against the same run's
  `SHA256SUMS-ci.txt`, and the candidate evidence copy
  `iolbox-macos-arm64-candidate-<run-id>.tar.gz`;
- start/status/diagnose/canary/HTTP evidence, rendered browser/GUI evidence,
  the real owner-supplied x86_64 IOL two-node lab and bidirectional traffic,
  before/after identity tables for upgrade, and the browser reload after the
  supervisor restart;
- ordinary launcher removal/recovery evidence and the literal pre-browser and
  final destructive-reset preconditions, exact Trash destinations, and final
  absence of the named VM/data paths. The final destructive sequence is the
  last hardware act.

## Release-note checklist

The draft body, `docs/INSTALL.md`, this document, the archive README, provider
docs, and `THIRD_PARTY.md` must agree on all of the following:

- Lima is user-installed/managed and VZ/Rosetta is gated by the live canary;
- Debian 13/trixie is the default with its actual pinned image/kernel, and
  Jammy is the qualified compatibility profile; bookworm remains a candidate;
- the existing amd64 supervisor/VPCS and **x86_64 IOL only** run through
  Rosetta; i386/i86bi and arm64-native IOL are unsupported;
- the archive is unsigned, checksums are not publisher identity, and the exact
  observed quarantine/Gatekeeper procedure is file-scoped;
- GUI/console/capture are loopback-only and unauthenticated;
- host data, upgrade, ordinary non-destructive launcher removal, and optional
  narrowly scoped destructive reset have their actual boundaries stated;
- no Cisco software is distributed and QEMU is Windows-only, not in the Mac
  archive.

## Publication gate

Do not publish or un-draft while any M6 plan §8 criterion is `NOT RUN` or
`FAIL`. The result document must copy the full acceptance table and assign
exactly `PASS`, `NOT RUN`, or `FAIL` to every row, with failed evidence retained
when a corrected workflow/archive is rerun. A green workflow, static archive
review, or unit test never substitutes for the required real Apple Silicon
observation.
