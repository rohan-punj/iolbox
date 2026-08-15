# M6 plan — build, distribute, and qualify the unsigned Apple Silicon archive

Status: planning only. No M6 implementation has been attempted.

Branch/worktree baseline: `luna/macos-m6-followups`, rebased onto
`luna/macos-m5-honest-caps` at `7b7b6ec`, in
`J:\Claude code\iolab-m6-wt`. M6 depends on the already-implemented M1–M5
stack on this branch. It does not depend on those ancestor branches being
merged to `main` before implementation begins.

The current M5 handoff and result are ground truth: M5 is **PASS**, including
criterion 2 on the ordinary amd64 non-Mac `noble-builder-vm`. M6 has no open
M5 criterion to exclude or infer around. M4 remains PARTIAL as described in
§9.

## 0. Pre-implementation hardware-access gate

Do not start a `luna-xhigh` hardware session until the owner has done all of
the following:

1. Created a genuinely fresh local macOS account on `192.168.101.166`, logged
   into its Aqua desktop once, and handed off the clean username plus working
   SSH access to that account. SSH through `rohansharma`, `sudo -iu`, or an
   account whose per-user state was merely cleaned is not a substitute.
2. Supplied one of these two interactive paths:
   - an interactive-desktop/browser-control channel into that clean account
     which the automated session can drive; or
   - an agreement that the owner will personally perform the specifically
     labelled **OWNER-GUI** steps in §§7.2–7.4 and §7.6 (real browser download,
     any Finder or Gatekeeper UI action, rendered-GUI assertions, and
     screenshots), then
     hand the exact terminal transcript, browser metadata, screenshots, and
     downloaded files back to the session. All **AGENT-SSH** steps—including
     the curl path, CLI checks, baseline/lab/upgrade work, and uninstall—remain
     automatable over the clean account's SSH connection.
3. Made one legally held x86_64 IOL image available to the clean account by an
   agreed safe path, without placing it in the release archive or evidence
   bundle.
4. Confirmed the clean browser can authenticate to GitHub as a collaborator
   allowed to view the draft release, and supplied the automated session a
   narrowly scoped GitHub credential outside shell history, command tracing,
   and retained evidence for the authenticated REST `curl` steps in §7.5.

If any item is missing, stop before consuming hardware time. Record the
§8 rows 1–8 as NOT RUN where their Mac evidence cannot be produced; do not
approximate desktop/browser evidence with SSH or a plain HTTP request.

## 1. Scope and completion rule

M6 packages and qualifies what M1–M5 already built. It is not a launcher,
guest, runtime, or capability feature phase. Its goal is one repeatably built,
unsigned `iolbox-macos-arm64.tar.gz` whose only separately installed runtime
prerequisite is Lima. iolbox detects and uses Lima; it does not install,
upgrade, reinstall, or remove Lima.

The immutable scope is `docs/macos-arm64-plan.md` §M6. In particular:

- add launcher CI coverage for `darwin/arm64`;
- add a Mac archive job to the existing release pipeline without changing the
  other target artifacts or reviving the abandoned Tauri/Rust shell;
- package the Go launcher, the exact native Linux payload built by the same
  workflow, the existing locked Lima profiles/templates, the existing guest
  provisioner, checksums, and notices;
- add the Mac release-artifact row/seventh numbered artifact section to the
  current install guide
  and make the provider and third-party disclosures accurate;
- qualify browser and `curl` downloads, Gatekeeper/quarantine behavior, one
  command to the GUI, a real x86_64 IOL lab, full-artifact upgrade, and both
  non-destructive and destructive uninstall meanings on Apple Silicon;
- ship unsigned and document the user-consent path through Gatekeeper. Do not
  sign, notarize, clear quarantine automatically, disable Gatekeeper, or use
  `spctl --master-disable`;
- do no M7 work. Native-arm64 supervisor/VPCS plus FEX/qemu-user is a separate,
  independent phase and is neither a prerequisite nor a fallback for M6.

M6 may be called PASS only when every criterion in §8 is PASS. A green CI run,
successful cross-compile, archive listing, static review, or unit test is not
substituted for the required real-hardware observations.

## 2. Release design decisions

### 2.1 Artifact boundary

The release contains no hypervisor and no guest disk. The user installs Lima;
Lima downloads the image named and digest-locked by the shipped profile. The
archive contains no Cisco software. Users supply appropriately licensed
x86_64 IOL/IOU images.

The binary is named `iolbox`, so the documented first command is:

```sh
./iolbox start
```

No `--assets-dir` or `--tarball` is needed in the release layout. The current
`resolveAssetRoot` checks cwd-derived repository layouts before
executable-derived candidates when locating `lima/profiles.env`.
`selectPayload` then searches the resolved asset root and, when it differs,
the current working directory, non-recursively, and selects the newest matching
`iolbox-server-*.tar.gz` by mtime. The archive therefore places exactly one
payload beside `iolbox`; that exact-one invariant makes mtime selection
harmless when launched from the extracted directory. M6 does not change this
existing lookup behavior or add a `payload/` subdirectory.

`packaging/macos/iolbox-mac.sh` and `packaging/macos/tests/` do not ship. The
shell entry point was the M1 host driver and is superseded for users by the Go
launcher; the test harnesses are qualification source, not runtime material.
The complete guest scripts and every file referenced by `profiles.env` do
ship.

### 2.2 Exact archive layout

The gzip member has one top-level directory and no files beside it:

```text
iolbox-macos-arm64.tar.gz
└── iolbox-macos-arm64/
    ├── iolbox                                      mode 0755; darwin/arm64
    ├── README.md                                   mode 0644; Mac quick start
    ├── LICENSE                                     mode 0644; iolbox license
    ├── SHA256SUMS                                  mode 0644; every file below except itself
    ├── iolbox-server-<release-version>.tar.gz      mode 0644; exact build-linux output
    ├── lima/
    │   ├── profiles.env
    │   ├── iolbox-trixie.yaml
    │   ├── iolbox-jammy.yaml
    │   ├── iolbox-bookworm.yaml
    │   ├── pinned-image-debian13.env
    │   ├── pinned-image.env
    │   └── pinned-image-debian12.env
    ├── guest/
    │   ├── lib.sh
    │   ├── 10-multiarch-debian.sh
    │   ├── 10-multiarch.sh
    │   ├── 20-kernel-hold-debian.sh
    │   ├── 20-kernel-hold.sh
    │   ├── 30-canary.sh
    │   ├── 40-install-payload.sh
    │   └── 50-verify.sh
    └── notices/
        └── THIRD_PARTY.md
```

The three profile/template/pin sets are intentional. Debian 13/trixie is the
current DEFAULT and is pinned; Ubuntu 22.04/Jammy is the pinned COMPATIBILITY
profile; Debian 12/bookworm remains an unqualified CANDIDATE which the current
launcher refuses while its digest is unpinned. Omitting the candidate files
would make the shipped `profiles.env` internally incomplete; including them
does not advertise candidate support.

`SHA256SUMS` uses paths relative to `iolbox-macos-arm64/`, sorted bytewise and
generated after permissions are normalized. The release also attaches
`iolbox-macos-arm64.tar.gz.sha256` next to the archive so a user can verify the
download before extraction. That outer checksum is not inside the archive.
Checksums published in the same unsigned release provide corruption/tamper
detection after publication, not cryptographic publisher identity; the docs
must not describe them as a substitute for signing.

All directories in the archive have mode 0755. No symlinks, device nodes,
hard links, extended attributes, AppleDouble files, or resource forks are
permitted in the tar member set.

### 2.3 Repeatability and payload identity

Add one manifest as the allow-list for the archive. Every manifest source must
be a regular, non-symlink file. The packer must fail on a missing manifest
member, an unexpected staged member, more or fewer than one native payload
tarball, a launcher which is not Mach-O arm64, or a payload whose hash does not
match an explicit trusted `--payload-sha256` input. The release job derives
that input by selecting exactly one well-formed checksum line whose basename
exactly matches the payload from the same run's `SHA256SUMS-ci.txt`; duplicate,
missing, mismatched-basename, or malformed lines fail the job.

Packing runs on Ubuntu with GNU tar. Validate `SOURCE_DATE_EPOCH` as a
non-negative integer; stage twice into independently created directories; set
`LC_ALL=C`; sort the complete member list bytewise; normalize directories to
0755, `iolbox` to 0755, other files to 0644, numeric uid/gid to zero, and mtime
to `@${SOURCE_DATE_EPOCH}`; then create a fixed pax-format stream with
`--pax-option=delete=atime,delete=ctime` and pipe it through `gzip -n`. The
release-layout test requires the two independently staged archives to be
byte-identical and have identical SHA-256 values. The GitHub tag, native
payload filename, release metadata, and checksums carry version identity; the
launcher has no embedded version claim or new version-stamp feature in M6.

The pack operation uses the equivalent of this concrete recipe for each fresh
stage (with `stage-members.txt` generated by `LC_ALL=C sort` from the exact
manifest-expanded tree):

```sh
tar --version | grep -F 'GNU tar'
LC_ALL=C tar --create --format=pax --sort=name --owner=0 --group=0 --numeric-owner \
  --mode='u+rwX,go+rX,go-w' --mtime="@${SOURCE_DATE_EPOCH}" \
  --pax-option=delete=atime,delete=ctime --no-recursion \
  --files-from=stage-members.txt -C "$stage" | gzip -n > "$output"
```

## 3. Ordered implementation tasks and exact files

### Task 1 — add launcher CI, then protect the existing jobs

Files: `.github/workflows/ci.yml`.

1. Add a Go launcher job rooted at `tools/iolab-launcher` using the same Go
   version as the supervisor job.
2. Run `go vet ./...` and `go test ./...` natively so the launcher tests really
   execute.
3. Run `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o
   bin/iolbox-darwin-arm64 .`.
4. Compile the Darwin-selected test package with `GOOS=darwin GOARCH=arm64 go
   test -c` as an additional build-boundary check. Record honestly that this
   compiles Darwin-only tests but does not execute a Mach-O test binary on the
   Linux runner; the released binary is exercised on Apple Silicon in §7.
5. Leave the supervisor, contracts, frontend, and existing Windows/Tauri CI
   behavior otherwise unchanged. In particular, this task does not turn the
   abandoned Tauri shell into a release artifact.

### Task 2 — add a manifest-driven Mac packer

Files:

- new `packaging/macos/pack-release.sh`;
- new `packaging/macos/release-manifest.txt`;
- new `packaging/macos/README.release.md`;
- new `packaging/macos/tests/release-layout-test.sh`;
- `THIRD_PARTY.md` as described in Task 5.

1. Make the manifest enumerate exactly the `lima/` and `guest/` source files
   in §2.2 plus the release README, project license, and notice destination.
   Do not use a recursive copy of all `packaging/macos/`, which would silently
   ship harnesses and the obsolete shell entry point.
2. Give the packer explicit `--launcher`, `--payload`, `--payload-sha256`,
   `--version`, `--output`, and `--source-date-epoch` inputs. It must not search
   for “newest” build products in a CI workspace, and it must compare the
   payload bytes to the trusted expected hash before staging.
3. Verify the launcher format/architecture, payload basename/version, every
   manifest source's regular non-symlink type, manifest completeness, and exact
   staged tree before packing.
4. Copy `packaging/macos/README.release.md` to archive `README.md`, the repository
   `LICENSE` to `LICENSE`, and the updated repository `THIRD_PARTY.md` to
   `notices/THIRD_PARTY.md`.
5. Generate the internal and outer SHA-256 files and use the exact GNU tar/
   `gzip` recipe in §2.3: two independent staging directories, `LC_ALL=C`
   bytewise member ordering, normalized modes and numeric uid/gid zero,
   `--mtime` from validated `SOURCE_DATE_EPOCH`, pax atime/ctime deletion, and
   `gzip -n`.
6. Make `release-layout-test.sh` extract into a temporary directory and assert
   the exact path set, modes, Mach-O arm64 identity, one native payload,
   internal checksum verification, and reproducible byte-for-byte output from
   two independently created staging directories. Inspect tar type flags to
   reject symlinks, hard links, block/character devices, FIFOs, and other
   special nodes; reject xattr/ACL pax headers; reject AppleDouble `._*`,
   `.DS_Store`, resource-fork patterns, Cisco image patterns, and test
   harnesses; and independently prove every manifest source is a regular
   non-symlink file.
7. The shipped README must contain the minimal prerequisite/checksum/
   quarantine/start sequence and link to `docs/INSTALL.md` with an absolute,
   tag-pinned HTTPS URL; it must agree
   verbatim with the Gatekeeper procedure proven in §7 rather than being
   finalized from assumption before the hardware run.

### Task 3 — add the Mac release job using the exact Linux payload

Files: `.github/workflows/release.yml`.

1. Add `build-macos` on an Ubuntu runner with `needs: build-linux`.
2. Check out full tag history and use the workflow's existing Go version.
3. Download the `iolbox-linux` artifact produced by that same workflow run and
   select its one `iolbox-server-<release-version>.tar.gz` explicitly. From
   that artifact's `SHA256SUMS-ci.txt`, select exactly one valid checksum line
   by exact payload basename, reject duplicates/malformed lines, verify it,
   and pass the expected digest as `--payload-sha256`. Do not rebuild or
   re-pack a second native payload in the Mac job.
4. Run launcher vet/tests, cross-build `tools/iolab-launcher` with
   `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64`, and invoke
   `packaging/macos/pack-release.sh` with explicit paths and the tag/ref-derived
   version.
5. Run the release layout/reproducibility test and print `tar -tzf`, internal
   checksum verification, `file` output, and final archive hash into the job
   log before upload.
6. Upload an `iolbox-macos` workflow artifact containing only
   `iolbox-macos-arm64.tar.gz` and
   `iolbox-macos-arm64.tar.gz.sha256`.
7. Add `build-macos` to the draft-release job's `needs` list and add the two
   files to its attachment glob. Do not modify how OVA, VMware, WSL, Proxmox,
   native-systemd, QEMU disk, or Windows launcher artifacts are built or
   attached.
8. Extend the draft body/release-note template with the Mac support contract:
   user-installed Lima; qualified Apple Silicon macOS/profile matrix; actual
   default and compatibility guest/kernel pins; Rosetta-amd64 execution;
   **x86_64 IOL only**; no i386 and no arm64-native IOL; unsigned artifact and
   the documented Gatekeeper procedure; loopback-only GUI; no Cisco software.

For `workflow_dispatch`, the job builds and validates the archive but does not
publish it, matching the existing dry-run behavior. A tag run creates the same
draft release as today, now with the Mac archive attached.

### Task 4 — update installation and provider documentation narrowly

Files:

- `docs/INSTALL.md`;
- `docs/providers.md`;
- new `docs/macos-release.md`.

In `docs/INSTALL.md`:

1. Replace the historically fixed “v0.5.2 publishes six” framing with
   “the current release publishes seven” plus tag-relative download guidance,
   so the guide describes the release that actually contains M6 without
   claiming v0.5.2 did. Apply the chosen tag-relative framing consistently to
   filenames introduced or touched by this narrow edit.
2. Add an Apple Silicon/Lima row to the existing five-row comparison table;
   it becomes six rows because the raw vmdk/vmx artifact has no dedicated row.
   Update the Docker build-from-source wording from the seventh overall option
   to the eighth once the seventh release artifact exists.
3. Promote and expand the existing unnumbered “Apple Silicon macOS (Lima)”
   note into the seventh numbered artifact section, rather than leaving a
   duplicate summary elsewhere. Use the same structure and tone as the six
   current sections: artifact/source, prerequisites, checksum,
   download/extract, observed browser-versus-`curl` quarantine behavior,
   exact Gatekeeper procedure, `./iolbox start`, data location, upgrade,
   uninstall, sizing, security note, and “It worked if”. Do not rewrite the
   other six sections.
4. State that Lima is installed and managed by the user, the release is
   unsigned, the GUI/console/capture forwards are loopback-only and have no
   authentication, host data defaults to
   `~/Library/Application Support/iolbox/{images,labs}`, and stop/removing the
   launcher is non-destructive.
5. State the capability boundary exactly: x86_64 IOL only through Rosetta;
   i386/i86bi and arm64-native IOL are unsupported. Do not imply M7 exists.

In `docs/providers.md`, add Lima as the Apple Silicon provider: user-installed
prerequisite, VZ/Rosetta canary gate, durable named guest, locked profiles,
host-loopback port contract, host-folder synchronization, and destructive
delete boundary. Keep the Windows provider selection contract intact; do not
resurrect the conceptual Tauri provider implementation.

In `docs/macos-release.md`, document the maintainer release recipe: input
provenance, workflow dispatch dry run, manifest/layout verification, tag/draft
creation, outer/internal checksum checks, hardware qualification record, exact
release-note checklist, and the rule that the draft is not published while any
§8 criterion is NOT RUN or FAIL.

### Task 5 — make third-party and prerequisite notices travel with the archive

Files: `THIRD_PARTY.md` and the copied
`iolbox-macos-arm64/notices/THIRD_PARTY.md` generated by Task 2.

1. Add a Mac artifact section distinguishing software distributed in the
   archive from software fetched/installed separately.
2. Record that Lima is an Apache-2.0 user-installed prerequisite and is not
   redistributed or managed by iolbox.
3. Record that pinned Debian/Ubuntu guest images and guest packages are
   downloaded by Lima/apt and are not embedded in the archive.
4. Audit the exact native payload created by `runtime/pack-native.sh`, notably
   its GNS3 VPCS v0.8.3 binary, and add the pinned upstream/license/source
   notice needed for what the archive actually redistributes. The repository's
   complete notice file may retain its clearly labelled Windows-only QEMU
   section, but the Mac section and archive README must explicitly say QEMU is
   not in the Mac archive; never imply that notice means QEMU is shipped there.
5. Preserve the “no Cisco software” statement and do not call user-supplied
   IOL an iolbox dependency or redistributable.

### Task 6 — run automation gates before consuming hardware time

Files changed only if a packaging/doc defect is found in Tasks 1–5.

1. Check `git status` and `git log` first. This is a shared worktree: preserve
   unrelated changes (including `docs/m6-session-logs/`) and stage only the
   exact M6 implementation paths, never `git add .` or `git add -A`.
2. Run the launcher vet/tests, Darwin cross-build, Darwin test compilation,
   current `packaging/macos/tests/lint.sh`, and the new release-layout test.
3. Run a release workflow dry run and download its `iolbox-macos` artifact;
   do not qualify a locally hand-assembled substitute.
4. Verify the downloaded workflow artifact's outer checksum, exact listing,
   internal checksums, binary type, payload identity, and deterministic rebuild.
5. Prepare the upgrade baseline reproducibly: from a clean `7b7b6ec` checkout
   (a corrected, cf19f2f-or-later M5 baseline), build the Darwin launcher and
   native payload; then feed those inputs and their trusted hash to the
   reviewed M6 packer from the exact candidate implementation commit. Record
   both source commits, commands, hashes, manifest, and packer hash. Name the
   evidence copy `iolbox-macos-arm64-baseline-7b7b6ec.tar.gz`. It is a hybrid
   **qualification artifact**, not workflow-produced, not attached to the
   draft, and never published in the release. The actual workflow candidate
   remains `iolbox-macos-arm64.tar.gz`; retain a separate evidence copy named
   `iolbox-macos-arm64-candidate-<run-id>.tar.gz`.
6. Do not mark a criterion PASS here. These are pre-hardware gates only.

### Task 7 — write the implementation result and handoff

Files: new `docs/macos-m6-result.md` and `docs/macos-m6-handoff.md`.

After qualification, copy §8 into the result and assign PASS, NOT RUN, or FAIL
to every row with exact evidence links. The handoff names the implementation
commit, workflow run and draft/tag, both baseline and candidate provenance and
hashes, clean account/host, evidence root, defects and reruns, owner-performed
GUI steps, and final VM/data state, following the M1–M5 convention.

## 4. Documentation contract for Gatekeeper and quarantine

The hardware run decides observed facts, but it does not improvise the safety
policy. The documented procedure is:

1. download the archive and its outer checksum from the same draft release;
2. verify the outer checksum before extraction;
3. extract, then verify the archive's internal `SHA256SUMS`;
4. inspect and record `com.apple.quarantine` on the downloaded archive,
   extracted top-level directory, and `iolbox` binary;
5. if the binary has quarantine, remove it from that binary only with:

   ```sh
   xattr -d com.apple.quarantine ./iolbox
   ```

6. run `./iolbox start` normally.

No `sudo`, recursive home-directory clearing, Gatekeeper disablement, or
automatic xattr mutation by iolbox is allowed. If the real browser flow needs
a different narrower successful user-consent action (for example Finder
right-click → Open), use and demonstrate it, then replace the candidate command
above consistently in `README.md`, `docs/INSTALL.md`, `docs/macos-release.md`,
and release notes before publication. Do not document an unexecuted workaround.

An absent quarantine attribute is an observation, not an automatic failure:
`xattr` exit status and output must be recorded for both download paths. The
browser criterion nevertheless requires a real browser download. Do not add a
synthetic quarantine attribute to manufacture evidence. If no tested browser
produces quarantine, record that result and demonstrate the actual first-run
behavior; if Gatekeeper behavior required by the criterion cannot be observed,
mark that criterion FAIL rather than inferring it.

## 5. Clean-machine choice and prerequisites

Use a genuinely fresh local macOS user account on the existing supported M1
Mac at `192.168.101.166`, not the already-used `rohansharma` home and not a new
machine. This is the chosen interpretation of “clean machine” because Lima
machine state, iolbox host data, launcher attestations, caches, downloads, and
quarantine state are all per-user, while the known physical Apple Silicon,
macOS, and Homebrew Lima installation remain available. A different Mac would
add an owner-supplied-target dependency that M6 does not currently have.

Before the session, the owner creates and interactively logs into a disposable
account (record its actual name in the result) and enables the already-approved
test access. In that account, verify and record:

- `uname -m` is `arm64`, `sw_vers` values, and physical memory;
- `/opt/homebrew/bin/limactl --version` works; use this full path over SSH
  because Homebrew is absent from the non-login PATH;
- no `~/.lima`, `~/.iolbox`, or
  `~/Library/Application Support/iolbox` state exists before the run;
- no repo checkout, prior iolbox archive, launcher, native payload, or Cisco
  image exists before the download;
- aside from OS/browser tooling, Lima is the only separately installed iolbox
  runtime prerequisite.

Use the existing access path for coordination and evidence collection:

```text
SSH host: rohansharma@192.168.101.166
key:      J:\Claude code\iolab-m6-wt\.m5-ssh\iolbox_mac_m0
Mac Lima: /opt/homebrew/bin/limactl
```

The clean account must also meet §0's interactive-access gate. `sudo -iu` or
SSH alone is not a substitute for the browser-download or rendered-GUI
criteria. Existing per-user Lima machines under `rohansharma` must not be
reused. Coordinate stopping resource-consuming reusable machines if necessary,
but never touch the M0 evidence machine `iol22`.

Repository `https://github.com/rohan-punj/iolbox` is public (confirmed from its
unauthenticated GitHub repository page during this planning pass). A **draft**
release is nevertheless not public before publication. The clean browser must
be signed into an authorized collaborator account. The CLI path uses the
authenticated GitHub REST release-asset endpoint with asset IDs, not a URL
guessed by appending a filename to the draft-release page. Confirm both facts
again immediately before
the hardware session in case repository visibility or GitHub behavior changed.

## 6. Evidence record and status semantics

Create a timestamped evidence directory in the clean account and retain:

- host/Lima inventory and clean-state listing;
- workflow URL/run ID, commit/tag, archive hashes, full tar listing, manifest,
  checksum results, and the exact candidate release asset IDs;
- the baseline qualification artifact's separate filename, clean `7b7b6ec`
  launcher/payload provenance, reviewed packer commit/hash, and explicit
  statement that it was not published;
- the same run's trusted `SHA256SUMS-ci.txt`, exact-basename selection output,
  shipped-payload hash, and Mac-side comparison result;
- browser name/version and `curl --version`;
- separate xattr output plus exit status for archive, directory, and binary in
  both download paths;
- terminal transcript and any Gatekeeper UI screenshots for first run;
- launcher status/diagnose output, Lima list/config, guest kernel/arch,
  canary, service status, HTTP result, and hello capability response;
- browser/HTTP/WS lab evidence, console transcript, and ping summary;
- before/after upgrade identity hashes and pinned-kernel output;
- non-destructive removal/reinstall proof and final destructive-delete proof;
- a `summary.md` with exactly PASS, NOT RUN, or FAIL for every §8 row and links
  to the supporting files.

Status meanings:

- **PASS**: every sub-observation in the criterion was executed successfully
  against the workflow-produced candidate on real Apple Silicon where the
  criterion calls for hardware; criterion 7 additionally uses the explicitly
  labelled hybrid baseline qualification artifact from Task 6.5.
- **NOT RUN**: no meaningful attempt was made, including unavailable access or
  missing owner-supplied test input.
- **FAIL**: it was attempted and any required sub-observation failed or could
  not be completed. A later code/doc fix does not turn FAIL into PASS until the
  corrected workflow artifact is rerun through that criterion.

## 7. Verbatim clean-machine hardware qualification

Every step is labelled **AGENT-SSH** or **OWNER-GUI**. The owner may instead
hand the interactive channel to the agent, but an SSH-only session never
claims an OWNER-GUI observation. Replace only `<clean-user>`,
`<clean-account-key>`, `<run-id>`, `<archive-asset-id>`,
`<checksum-asset-id>`, and `<linux-artifact-id>`. Keep the
default machine/profile and the two distinct download roots throughout.

### 7.1 Preflight and isolation

1. **AGENT-SSH (from Windows):** prove both the coordination account and the
   owner-created clean account are reachable, then record host facts:

   ```powershell
   ssh -i ".m5-ssh/iolbox_mac_m0" rohansharma@192.168.101.166 "sw_vers; uname -m; /opt/homebrew/bin/limactl --version"
   ssh -i "<clean-account-key>" <clean-user>@192.168.101.166 "id; pwd; sw_vers; uname -m; /opt/homebrew/bin/limactl --version"
   ```

2. **AGENT-SSH:** in `/Users/<clean-user>`, record `top -l 1 -s 0`, Lima
   inventory, and `test ! -e` results for `.lima`, `.iolbox`, `Library/
   Application Support/iolbox`, repo checkouts, archives, and launchers. If any
   relevant path exists, stop; the owner replaces the account. Never clean a
   questionable home and call it fresh, reuse another user's VM, or touch
   `iol22`.
3. **OWNER-GUI:** confirm the Aqua session is the same `<clean-user>` and the
   authenticated browser can open the draft release. Record account/browser
   identity without capturing credentials.

### 7.2 Baseline qualification-artifact install and rendered GUI

The baseline installs first. It is
`iolbox-macos-arm64-baseline-7b7b6ec.tar.gz`, produced by the reviewed M6
packer but from launcher/native-payload inputs built in a clean `7b7b6ec`
checkout per Task 6.5. It is not a release asset.

1. **AGENT-SSH:** copy the baseline archive, its outer checksum, manifest, and
   provenance record into
   `/Users/<clean-user>/Downloads/iolbox-m6-baseline/`. Verify the recorded
   packer commit/hash and input commits, then run:

   ```sh
   cd "/Users/<clean-user>/Downloads/iolbox-m6-baseline"
   test "$(pwd)" = "/Users/<clean-user>/Downloads/iolbox-m6-baseline"
   shasum -a 256 -c "iolbox-macos-arm64-baseline-7b7b6ec.tar.gz.sha256"
   mkdir "extract"
   tar -xzf "iolbox-macos-arm64-baseline-7b7b6ec.tar.gz" -C "extract"
   cd "/Users/<clean-user>/Downloads/iolbox-m6-baseline/extract/iolbox-macos-arm64"
   shasum -a 256 -c "SHA256SUMS"
   ./iolbox start
   ./iolbox status
   ./iolbox diagnose
   ```

2. **AGENT-SSH:** require launcher exit 0, HTTP below 500 at
   `127.0.0.1:4001`, active service, live canary PASS, correct guest/profile/
   kernel facts, host-loopback-only GUI/console/capture, and no forwarded guest
   control port. Use `/opt/homebrew/bin/limactl`, never bare `limactl`.
3. **OWNER-GUI:** open the GUI in the clean Aqua session, prove it renders and
   is connected, and hand back screenshots plus the browser console transcript.
   A curl GET is not this evidence.

### 7.3 Baseline real x86_64 IOL lab

1. **AGENT-SSH:** stage the owner-supplied, legally held x86_64 IOL image only
   as test input. Automate registration and creation/start of the saved linked
   two-node lab over the authenticated local API; do not copy the image into
   evidence. Each node has `ram: 1024`.
2. **OWNER-GUI:** visibly confirm the registered image and inspect the running
   saved two-node lab. Confirm
   x86_64 is usable, i386 is absent/disabled, and arm64-native IOL is not
   implied. Hand back screenshots/DOM observations.
3. **AGENT-SSH:** wait for real console readiness, configure addresses, and
   prove bidirectional traffic. Use the last complete ping summary because
   consoles replay a ring buffer; wake consoles, disable pagination, and wait
   for final prompts. Save transcripts and no-loss or documented first-ARP-loss
   results. Browser-equivalent WS automation must first obtain the session
   cookie and send the matching `Origin`.
4. **AGENT-SSH:** reload the saved lab and prove traffic again. Record host and
   guest image/lab hashes/listings, hostid, hostname, exact iourc hash,
   supervisor version, `uname -r`, structural-gate records, and machine name.
   This is the before-upgrade identity table.

### 7.4 Candidate browser evidence and full-artifact upgrade

1. **OWNER-GUI:** only after §7.3 passes, use the authorized browser to download
   the draft's `iolbox-macos-arm64.tar.gz` and outer checksum into
   `/Users/<clean-user>/Downloads/iolbox-m6-browser/`. Record browser version,
   release URL/tag, whether it auto-extracted, and screenshots. Retain the
   original tarball. This genuine browser copy is kept separate and is not the
   copy used for upgrade. If the browser auto-extracted, move that extraction
   aside as evidence; the agent also extracts the retained tarball into the
   fixed `/Users/<clean-user>/Downloads/iolbox-m6-browser/extract/` root.
2. **AGENT-SSH:** before mutation, record `xattr -l` and
   `xattr -p com.apple.quarantine` plus exit status for browser archive,
   auto-extracted directory, and binary. Verify outer/internal checksums and
   record xattrs after extraction. If quarantine exists, apply only §4's narrow
   action; any Finder/Gatekeeper UI action is **OWNER-GUI** and must be handed
   back with transcript/screenshots. Retain this verified browser copy for the
   clean candidate start in §7.6.
3. **AGENT-SSH:** execute the independent authenticated `curl` path in §7.5,
   including the Mac-side same-run payload identity comparison. From
   `/Users/<clean-user>/Downloads/iolbox-m6-curl/extract/
   iolbox-macos-arm64`, run `./iolbox upgrade` with no overrides.
4. **AGENT-SSH:** require exit 0, active service, canary PASS, HTTP readiness,
   and expected candidate identity. Re-record every §7.3 step 4 value; image/
   lab bytes, hostid, hostname, iourc, machine name, and exact pinned kernel
   must match. Reload the browser fully after supervisor restart, then reload
   the saved lab and prove traffic again. **OWNER-GUI** reconfirms the candidate
   GUI's x86_64/i386/arm64 capability presentation. Preserve both before/after
   tables.

### 7.5 Authenticated `curl` download, quarantine, and payload identity

The repo is public, but the draft assets require authentication. The owner
supplies `IOLBOX_GH_TOKEN` securely; use `set +x`, never echo it, and exclude
credential entry/environment dumps from evidence. Obtain and record the two
release asset IDs and Linux workflow artifact ID from GitHub metadata, then:

```sh
mkdir "/Users/<clean-user>/Downloads/iolbox-m6-curl"
cd "/Users/<clean-user>/Downloads/iolbox-m6-curl"
test "$(pwd)" = "/Users/<clean-user>/Downloads/iolbox-m6-curl"
set +x
curl --fail --location --header "Accept: application/octet-stream" \
  --header "Authorization: Bearer ${IOLBOX_GH_TOKEN}" \
  --header "X-GitHub-Api-Version: 2022-11-28" \
  --output "iolbox-macos-arm64.tar.gz" \
  "https://api.github.com/repos/rohan-punj/iolbox/releases/assets/<archive-asset-id>"
curl --fail --location --header "Accept: application/octet-stream" \
  --header "Authorization: Bearer ${IOLBOX_GH_TOKEN}" \
  --header "X-GitHub-Api-Version: 2022-11-28" \
  --output "iolbox-macos-arm64.tar.gz.sha256" \
  "https://api.github.com/repos/rohan-punj/iolbox/releases/assets/<checksum-asset-id>"
curl --fail --location --header "Accept: application/vnd.github+json" \
  --header "Authorization: Bearer ${IOLBOX_GH_TOKEN}" \
  --header "X-GitHub-Api-Version: 2022-11-28" \
  --output "iolbox-linux-artifact.zip" \
  "https://api.github.com/repos/rohan-punj/iolbox/actions/artifacts/<linux-artifact-id>/zip"
```

Record response headers with authorization redacted, `curl --version`, file
types, and asset metadata; require a valid gzip/tar and ZIP rather than HTML.
Record archive xattrs and exit statuses, verify the outer checksum, extract to
`extract/`, verify internal `SHA256SUMS`, and record directory/binary xattrs.
Extract the Linux workflow artifact to `trusted-linux/`, locate its
`SHA256SUMS-ci.txt`, and compare by exact basename:

```sh
cd "/Users/<clean-user>/Downloads/iolbox-m6-curl"
payload="$(find "extract/iolbox-macos-arm64" -maxdepth 1 -type f -name 'iolbox-server-*.tar.gz' -print)"
test -n "$payload"
test "$(find "extract/iolbox-macos-arm64" -maxdepth 1 -type f -name 'iolbox-server-*.tar.gz' -print | wc -l | tr -d ' ')" = "1"
base="$(basename "$payload")"
test "$(awk -v b="$base" '$2 == b || $2 == "*" b {n++} END {print n+0}' "trusted-linux/SHA256SUMS-ci.txt")" = "1"
expected="$(awk -v b="$base" '$2 == b || $2 == "*" b {print $1}' "trusted-linux/SHA256SUMS-ci.txt")"
printf '%s\n' "$expected" | grep -Eq '^[0-9A-Fa-f]{64}$'
actual="$(shasum -a 256 "$payload" | awk '{print $1}')"
test "$actual" = "$expected"
printf 'workflow_run=%s payload=%s expected=%s actual=%s PASS\n' "<run-id>" "$base" "$expected" "$actual" | tee "payload-identity-mac.txt"
```

Apply §4 only when the candidate binary is actually quarantined. This curl
extraction is the sole upgrade input in §7.4.

### 7.6 Non-destructive removal, browser-copy clean start, and destructive reset

All paths below are literal and absolute after replacing `<clean-user>` once.
Never use a glob, `rm -rf`, a broad `.lima`/`.iolbox` move, or an unquoted
`Application Support` path.

1. **AGENT-SSH — ordinary launcher removal:** from the curl candidate, run
   `./iolbox stop`; require exit 0 and the exact machine to be Stopped. Move
   only the two extracted launcher trees to unique Trash destinations:

   ```sh
   cd "/Users/<clean-user>/Downloads/iolbox-m6-curl/extract"
   test "$(pwd)" = "/Users/<clean-user>/Downloads/iolbox-m6-curl/extract"
   test -d "/Users/<clean-user>/Downloads/iolbox-m6-curl/extract/iolbox-macos-arm64"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-curl-launcher"
   /opt/homebrew/bin/limactl list --format '{{.Name}} {{.Status}}' | grep -Fx 'iolbox-debian13 Stopped'
   mv "/Users/<clean-user>/Downloads/iolbox-m6-curl/extract/iolbox-macos-arm64" "/Users/<clean-user>/.Trash/iolbox-m6-curl-launcher"

   cd "/Users/<clean-user>/Downloads/iolbox-m6-baseline/extract"
   test "$(pwd)" = "/Users/<clean-user>/Downloads/iolbox-m6-baseline/extract"
   test -d "/Users/<clean-user>/Downloads/iolbox-m6-baseline/extract/iolbox-macos-arm64"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-baseline-launcher"
   /opt/homebrew/bin/limactl list --format '{{.Name}} {{.Status}}' | grep -Fx 'iolbox-debian13 Stopped'
   mv "/Users/<clean-user>/Downloads/iolbox-m6-baseline/extract/iolbox-macos-arm64" "/Users/<clean-user>/.Trash/iolbox-m6-baseline-launcher"
   ```

   Prove the VM, host data, images, and lab remain. Re-extract the verified curl
   archive into `/Users/<clean-user>/Downloads/iolbox-m6-recovery`, start it,
   and prove the same machine, lab, iourc, kernel, and traffic.
2. **AGENT-SSH — narrowly destructive reset before the clean candidate start:**
   stop the recovered launcher, then validate and delete only the default VM
   and move only the two named data paths to unique Trash destinations:

   ```sh
   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   /opt/homebrew/bin/limactl list --format '{{.Name}} {{.Status}}' | grep -Fx 'iolbox-debian13 Stopped'
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "1"
   /opt/homebrew/bin/limactl delete "iolbox-debian13"

   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   test -d "/Users/<clean-user>/Library/Application Support/iolbox"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-prebrowser-data"
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "0"
   mv "/Users/<clean-user>/Library/Application Support/iolbox" "/Users/<clean-user>/.Trash/iolbox-m6-prebrowser-data"

   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   test -f "/Users/<clean-user>/.iolbox/macos/iolbox-debian13-structural-gate.json"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-prebrowser-structural-gate.json"
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "0"
   mv "/Users/<clean-user>/.iolbox/macos/iolbox-debian13-structural-gate.json" "/Users/<clean-user>/.Trash/iolbox-m6-prebrowser-structural-gate.json"
   ```

3. **AGENT-SSH + OWNER-GUI — candidate clean start:** prove the default VM/data
   paths are absent, then from the already verified browser extraction run:

   ```sh
   cd "/Users/<clean-user>/Downloads/iolbox-m6-browser/extract/iolbox-macos-arm64"
   test "$(pwd)" = "/Users/<clean-user>/Downloads/iolbox-m6-browser/extract/iolbox-macos-arm64"
   test ! -e "/Users/<clean-user>/.lima/iolbox-debian13"
   test ! -e "/Users/<clean-user>/Library/Application Support/iolbox"
   ./iolbox start
   ./iolbox status
   ./iolbox diagnose
   ```

   Require provisioning/readiness/
   canary/status/diagnose PASS. **OWNER-GUI** proves the rendered GUI and any
   Gatekeeper consent path from the actual browser-downloaded candidate. This
   is the candidate clean-install evidence for §8 rows 3–4.
4. **AGENT-SSH — final destructive demonstration:** after every other evidence
   file is sealed, stop the browser-copy launcher and repeat the narrow reset
   with final, collision-free Trash names. This must be the last hardware act:

   ```sh
   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   /opt/homebrew/bin/limactl list --format '{{.Name}} {{.Status}}' | grep -Fx 'iolbox-debian13 Stopped'
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "1"
   /opt/homebrew/bin/limactl delete "iolbox-debian13"

   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   test -d "/Users/<clean-user>/Library/Application Support/iolbox"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-final-data"
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "0"
   mv "/Users/<clean-user>/Library/Application Support/iolbox" "/Users/<clean-user>/.Trash/iolbox-m6-final-data"

   cd "/Users/<clean-user>"
   test "$(pwd)" = "/Users/<clean-user>"
   test -f "/Users/<clean-user>/.iolbox/macos/iolbox-debian13-structural-gate.json"
   test ! -e "/Users/<clean-user>/.Trash/iolbox-m6-final-structural-gate.json"
   test "$(/opt/homebrew/bin/limactl list --format '{{.Name}}' | grep -cx 'iolbox-debian13')" = "0"
   mv "/Users/<clean-user>/.iolbox/macos/iolbox-debian13-structural-gate.json" "/Users/<clean-user>/.Trash/iolbox-m6-final-structural-gate.json"
   ```

Record the absent VM and recoverable Trash destinations. Documentation calls
launcher removal ordinary uninstall and the named VM/data deletion an optional,
warned destructive reset; `stop` is never called uninstall or deletion.

## 8. Acceptance criteria

The implementation result copies this table and assigns exactly one status to
each row.

| # | Criterion required for PASS | Required evidence |
|---|---|---|
| 1 | CI executes launcher vet/unit tests, compiles Darwin-selected tests, and cross-builds `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64`; the workflow-produced binary also starts/diagnoses on the real Mac. | Green CI job/log, workflow artifact provenance, `file` output, real-Mac command transcript. |
| 2 | The release workflow emits exactly `iolbox-macos-arm64.tar.gz` plus its outer checksum; the archive has exactly §2.2, all internal checksums pass, its native payload is byte-identical to the same run's `build-linux` payload, and independent stages reproduce identical bytes. | Workflow log/artifacts, both independent-stage hashes, extracted manifest, same-run `SHA256SUMS-ci.txt` provenance and exact-basename payload comparison recorded on the Mac in §7.5. |
| 3 | After the §7.6 reset, the fresh supported Apple Silicon account—with only Lima as the separately installed runtime prerequisite—reaches the rendered GUI using the genuine browser-downloaded candidate and one documented `./iolbox start`. | §7.1 clean-state inventory, §7.4 browser provenance, and §7.6 launcher/HTTP/OWNER-GUI/service/canary evidence. |
| 4 | Browser-candidate quarantine is recorded before mutation and the exact documented, narrow first-run Gatekeeper procedure succeeds without signing, notarization, global disablement, or automatic clearing. | §7.4 xattr output/exit status for archive/directory/binary plus §7.6 OWNER-GUI Gatekeeper transcript/screenshots and successful clean start. |
| 5 | The candidate is independently downloaded from authenticated GitHub draft asset IDs with `curl`, checksum-verified, extracted, xattr-checked, payload-identity-checked, and used for the upgrade; presence or absence of quarantine is reported rather than assumed. | §7.5 curl/version/asset-metadata transcript, separate hashes/listing/xattrs, `payload-identity-mac.txt`, and §7.4 upgrade result. |
| 6 | A real owner-supplied x86_64 IOL image runs in a saved two-node lab and passes bidirectional traffic through the shipped artifact; i386 and arm64-native IOL are not advertised as supported. | Rendered GUI/hello evidence, console transcripts, last complete ping summaries, saved-lab proof. |
| 7 | `./iolbox upgrade` from the distinctly named hybrid `7b7b6ec`-input baseline qualification artifact to the independently curl-downloaded workflow candidate preserves host and guest images/labs, hostid/hostname, exact iourc hash, Lima machine identity, and pinned guest kernel; the saved lab reloads and passes traffic afterward. | Task 6.5 baseline provenance/non-publication record, §7.3 before table, §7.4 after table, and post-upgrade browser/traffic evidence. |
| 8 | Plain launcher removal leaves the stopped VM and data intact and re-extraction recovers the same lab; each literal §7.6 destructive sequence affects only the exact named VM/data, and the final repetition is the last hardware act. | §7.6 preconditions, Lima/data listings before removal, after removal, after recovery, after pre-browser reset, and after final destructive cleanup with exact Trash destinations. |
| 9 | `docs/INSTALL.md`, archive README, provider docs, third-party notice, maintainer release doc, and draft release notes consistently name Lima, actual supported macOS/profile and guest/kernel matrix, Rosetta-amd64, x86_64-IOL-only, unsigned/Gatekeeper, loopback security, upgrade, and uninstall boundaries. | Exact published draft text/archive notice reviewed against the workflow artifact and real observations; no unsupported capability claim. |

If a criterion is rerun after a fix, retain the failed evidence and identify the
new workflow run/archive hash that earned PASS. Never relabel the earlier run.

## 9. Risks and assumptions

1. **M4 remains PARTIAL and is out of scope.** Only items 1 and 6 were proven
   on hardware; items 2 and 7 have unit-tested fixes not hardware-reconfirmed;
   items 3, 4, 5, and 8 were not attempted. M6 does not proactively execute or
   close that backlog. Because M6 packages and runs the cumulative M1–M5
   product, an unresolved M4 defect may surface naturally in the clean install,
   lab, upgrade, or uninstall run. Root-cause and record it. Fix M6 packaging/
   docs defects in scope; do not turn the session into an M4 backlog hunt. If
   the defect blocks an M6 criterion, that criterion is FAIL until resolved and
   rerun.
2. **M5 is fully PASS; M4 is the only inherited incomplete phase.** M5
   criterion 2 passed on `noble-builder-vm` as recorded in the current M5
   handoff/result. M6 has no amd64 non-Mac exception, inference, or acquisition,
   and no M6 step depends on that criterion remaining open.
3. **Hardware access is a gate, not an implementation assumption.** §0 requires
   a genuinely fresh account with SSH plus either agent-controlled interactive
   desktop/browser access or owner-executed GUI evidence handoff. If it is not
   met, rows 1–8 are NOT RUN wherever their Mac evidence cannot be produced.
   `rohansharma`, `sudo -iu`, and HTTP-only evidence are not silent fallbacks.
4. **Quarantine varies by browser, settings, extraction path, and macOS.** A
   browser may auto-extract, propagate quarantine differently, or Gatekeeper
   may present a different UI than expected. The plan records all layers before
   mutation and forbids synthetic xattrs. The candidate `xattr -d` procedure is
   deliberately file-scoped, but it must be replaced everywhere if hardware
   proves a different user action is the successful path.
5. **Unsigned provenance remains limited.** Internal and outer hashes detect
   mismatch but, when hosted beside the unsigned artifact, do not authenticate
   the publisher. Release and install docs must say the artifact is unsigned;
   M6 has no hidden signing/notarization work.
6. **Exact-payload coupling is a release risk.** Rebuilding the native payload
   independently in `build-macos`, selecting “newest”, or allowing multiple
   tarballs could ship different bits from the native-systemd artifact. The Mac
   job therefore consumes and hash-compares the exact `build-linux` output from
   the same run.
7. **Draft download authentication is required even though the repo is
   public.** Draft assets are collaborator-only until publication. Reconfirm
   visibility before the session, keep the clean browser authenticated, use
   exact GitHub REST asset IDs for curl, and keep the token out of history,
   tracing, saved headers, and environment dumps. HTML is a failed download.
8. **External downloads can fail.** Lima and the pinned guest image are not in
   the archive. A moved image URL, unavailable mirror, stale Homebrew Lima
   bottle, or Rosetta share warning can block clean provisioning. Keep the
   structural canary and digest refusal intact. `brew upgrade lima` is not
   remediation for a stale same-version bottle; the known action is
   `brew reinstall lima`, performed by the user outside iolbox.
9. **The 8 GB Mac is resource-constrained and already has other-user VMs.**
   Stop only owner-approved reusable machines if needed and never touch
   `iol22`. Use 1024 MB per IOL node and record host memory pressure so a
   resource failure is not mislabelled as archive corruption.
10. **Browser automation has protocol traps.** WebSocket evidence needs the
   HTTP session cookie plus same-origin `Origin`; consoles replay their ring
   buffer; and after supervisor restart the app may need a full page reload.
   Carry these M3/M5 facts forward before calling a product failure.
11. **Lima configuration has a known quoting trap.** If qualification
    diagnostics must inspect or reproduce a `limactl --set` operation, yq map
    keys in the expression must remain quoted. Do not “simplify” the locked
    template or a working launcher-generated expression while debugging the
    archive. Record the rendered per-machine Lima YAML before blaming the
    provisioner.
12. **Shared-worktree contamination is possible.** Earlier phases observed a
    concurrent session changing and committing files in the same worktree.
    Recheck branch/HEAD/status at every handoff, preserve the pre-existing
    `docs/m6-session-logs/` tree, and stage exact paths only.
13. **Schedule confidence is low.** The master estimate is 1.5–2 focused days,
    including one clean-machine pass. Every earlier Mac phase exceeded its
    estimate once real hardware exposed defects that CI/static review missed.
    Reserve an uninterrupted genuine clean-account hardware pass plus time for
    at least one corrected workflow rebuild/rerun; CI-green is only the entry
    gate, not schedule completion.

## 10. Explicit non-goals

- M4 backlog closure or proactive M4 regression hunting.
- Code signing, notarization, entitlements, a `.app`, `.dmg`, Homebrew formula,
  package installer, or automatic Gatekeeper/quarantine changes.
- Installing, upgrading, reinstalling, or uninstalling Lima for the user.
- A guest disk or bundled hypervisor.
- Tauri/Rust shell repair or release packaging.
- i386/i86bi enablement, arm64-native IOL, native-arm64 supervisor/VPCS,
  FEX/qemu-user, or any other M7 work.
- Changes to launcher lifecycle, sync, ports, GUI, supervisor, native payload,
  or guest provisioning behavior unless the later implementation session finds
  a defect directly preventing an M6 acceptance criterion and separately
  obtains the appropriate scope decision.

## 11. Estimate and handoff condition

Retain the master estimate of 1.5–2 focused days as the no-defect engineering
estimate: roughly half a day for CI/packer/release integration, half a day for
docs/notices and workflow dry run, and at least half to one full day for the
fresh-account browser/curl/lab/upgrade/uninstall sequence. This is not a promise
that the hardware pass will fit the estimate; plan capacity for iterative
artifact rebuilds based on M2–M5 experience.

The M6 implementation handoff must name the exact commit, workflow run, archive
SHA-256, clean account/host facts, evidence root, per-criterion status, defects
found, and final machine/data state. It must say PARTIAL unless all §8 rows are
PASS, regardless of how much code or documentation was completed.
