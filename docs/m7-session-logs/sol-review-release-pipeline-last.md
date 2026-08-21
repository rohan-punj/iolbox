The plan is not safe to implement unchanged. The payload-selection design is sound, but two blockers remain:

1. `./iolbox upgrade` can break for existing automatic Rosetta users by resolving to a new, nonexistent native machine.
2. The arm64 payload will also be published as a standalone Linux release asset, exceeding the sanctioned macOS-only scope.

## Q1. Backward compatibility

**Verdict: No, not fully backward compatible. The payload-format change is compatible; the profile-default transition is not.**

The working Rosetta upgrade path is safe only when Rosetta remains selected:

1. The launcher reads the persisted choice from `profile-choice.env` ([macos_profile_select.go:112](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:112>)).
2. A persisted `rosetta-amd64` resolves to the table’s `DEFAULT` row, currently `debian13` ([macos_profile_select.go:92](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:92>), [profiles.env:20](</J:/Claude code/iolab-release-native-wt/packaging/macos/lima/profiles.env:20>)).
3. The default machine remains `iolbox-debian13` ([macos_cli.go:207](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_cli.go:207>)).
4. With the proposed filtering, the untagged amd64 payload is selected.
5. Upgrade requires that machine to exist, stages a full replacement provision tree, and then swaps it into place ([macos_lifecycle.go:360](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lifecycle.go:360>), [macos_lima.go:394](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:394>), [macos_lima.go:426](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:426>)).

That path works for persisted `rosetta-amd64`, persisted legacy profiles such as `jammy`, or an explicit Rosetta selection.

The missed breaking case is an existing Rosetta user with no persisted explicit choice—which is normal because automatic choices are not persisted. With the new native assets present, `auto` can resolve to `native-arm64` ([macos_profile_select.go:353](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:353>)). That changes the default machine from `iolbox-debian13` to `iolbox-native-arm64` ([macos_cli.go:207](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_cli.go:207>)). Then:

```text
./iolbox upgrade
→ auto-native
→ machine iolbox-native-arm64
→ upgrade requires existing machine
→ failure: upgrade requires existing machine "iolbox-native-arm64"
```

The hard failure is explicit at [macos_lifecycle.go:365](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lifecycle.go:365>). Therefore the plan’s compatibility table at [macos-release-native-arm64-plan.md:417](</J:/Claude code/iolab-release-native-wt/docs/macos-release-native-arm64-plan.md:417>) is incomplete.

`start` is also not continuity-preserving: it creates a separate native VM while leaving the old Rosetta VM untouched. That does not destroy the old setup, but it changes the selected VM and guest-local state.

For old and new files coexisting, arch filtering prevents the catastrophic cross-architecture choice, but selection still chooses the newest same-architecture file across both `assetRoot` and the current directory ([macos_lima.go:510](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:510>), [macos_lima.go:533](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:533>)). A stale same-arch payload with a later manually modified mtime can still win. “Strictly better” is fair; “release-version exact” is not.

**Required plan correction:** define upgrade migration semantics. At minimum, if `upgrade` resolves to native but only the historical Rosetta machine exists, either retain that existing profile for this upgrade or fail with explicit migration/opt-out instructions before payload selection.

## Q2. Payload-selection correctness

**Verdict: Correct for the current profile table and all real selection branches.**

`selection.ProfileName` is the correct key:

- Explicit `native-arm64` returns `ProfileName: native-arm64` only after successful native preflight ([macos_profile_select.go:309](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:309>)).
- Explicit Rosetta or a legacy Rosetta row returns its concrete non-native row ([macos_profile_select.go:326](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:326>)).
- Persisted native success retains native; failure returns the default Rosetta row ([macos_profile_select.go:335](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:335>)).
- Auto native success returns the native row; missing row or failed preflight returns the Rosetta row ([macos_profile_select.go:359](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profile_select.go:359>)).

There is no current branch where `ProfileName == native-arm64` legitimately wants amd64, or where another current profile wants arm64.

Section 1.5’s ordering claim is also correct: selection and fallback finish at [macos_cli.go:184](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_cli.go:184>), the concrete profile is loaded at line 197, and payload selection occurs later at line 258. Nothing in payload discovery feeds back into profile resolution.

One sentence is overstated: “no new failure mode” at [macos-release-native-arm64-plan.md:139](</J:/Claude code/iolab-release-native-wt/docs/macos-release-native-arm64-plan.md:139>) is false. Missing or stale payloads can still fail after the profile is final. Section 5 itself correctly acknowledges that.

Keying on the profile name is correct today, but it intentionally treats any future second `NATIVE` row as amd64. That is acceptable only while the one-native-row invariant remains explicit.

## Q3. Mtime/lexicographic hazard

**Verdict: The core claim is verified and load-bearing, but “every Rosetta user” is too absolute.**

Current `selectPayload` accepts every `iolbox-server-*.tar.gz` candidate ([macos_lima.go:520](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:520>)), then sorts by descending mtime and ascending path on ties ([macos_lima.go:533](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:533>)).

The packer gives every member the same `SOURCE_DATE_EPOCH` mtime ([pack-release.sh:215](</J:/Claude code/iolab-release-native-wt/packaging/macos/pack-release.sh:215>)). In a clean extraction, the two payload mtimes therefore tie. Comparing:

```text
iolbox-server-vX-linux-arm64.tar.gz
iolbox-server-vX.tar.gz
```

the first differing characters are `-` and `.`, so the arm64 filename sorts first. An unmodified launcher would consequently stage arm64 into a Rosetta guest.

The exceptions to “every Rosetta user” are:

- `IOLBOX_TARBALL`/`--tarball` bypasses discovery entirely ([macos_lima.go:503](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:503>)).
- In a dirty/coexisting directory, another payload with a later mtime wins before the tie-break.

Thus the exact claim should be: **every Rosetta invocation using automatic payload discovery from a clean official two-payload extraction deterministically selects arm64.** That is sufficient to make launcher filtering mandatory.

## Q4. Scope creep

### (a) `build-release.sh --arch`

**Verdict: Necessary, but “byte-identical” is asserted rather than proven.**

The current amd64 build command is exactly one `GOOS=linux GOARCH=amd64 go build` invocation ([build-release.sh:20](</J:/Claude code/iolab-release-native-wt/build-release.sh:20>)). A loop can preserve that command, and the later arm64 compile need not mutate its result.

However, changing argument parsing, compile order, placeholder checking, and cleanup creates regression risk. The plan should require a test that:

- no arguments still creates only `supervisor-linux-amd64`;
- `--arch amd64,arm64` creates both;
- the amd64 compile uses the unchanged command and inputs;
- invalid, duplicate, and empty arch lists fail clearly.

Also add cleanup via a trap: adding a second compile adds another exit point before the current placeholder restoration at [build-release.sh:29](</J:/Claude code/iolab-release-native-wt/build-release.sh:29>).

### (b) Untagged amd64 payload

**Verdict: Correct. Do not pass `--arch amd64`.**

`--arch` sets `ARCH_EXPLICIT=1` ([pack-native.sh:105](</J:/Claude code/iolab-release-native-wt/runtime/pack-native.sh:105>)). That changes the package name to `-linux-amd64` ([pack-native.sh:343](</J:/Claude code/iolab-release-native-wt/runtime/pack-native.sh:343>)) and adds `manifest.env` ([pack-native.sh:408](</J:/Claude code/iolab-release-native-wt/runtime/pack-native.sh:408>)). Omitting `--arch` retains the historical untagged name and no manifest. Section 2 is correct.

### (c) Required arm64 packer argument

**Verdict: Required is the right contract.**

Once the six assets are in the manifest, qualifying `auto` users select native. An optional arm64 payload would permit an internally valid-looking archive that fails only after selection. Requiring the arm64 path and digest makes the invalid half-native state unrepresentable.

This intentionally breaks direct callers of the old packer CLI, so usage text, tests, and all invocations must change together. An optional argument is worse unless accompanied by a complete Rosetta-only archive mode that also omits the native row/assets.

### (d) Separate arm64 build directory

**Verdict: Necessary for VPCS and sufficient for the proposed sequential workflow.**

`fetch-vpcs.sh` always uses `$BUILD_DIR/vpcs/vpcs` ([fetch-vpcs.sh:15](</J:/Claude code/iolab-release-native-wt/runtime/fetch-vpcs.sh:15>)) and skips an existing executable ([fetch-vpcs.sh:123](</J:/Claude code/iolab-release-native-wt/runtime/fetch-vpcs.sh:123>)). Reusing `runtime/build` after the amd64 build would encounter the amd64 binary. An explicit arm64 invocation would at least reject it via `require_elf_arch`, but would not rebuild it.

A separate directory gives VPCS its own clone, output, toollaunch binary, and staging area. `--out runtime/build` then places only the final tarball in the shared artifact directory. The plan slightly overstates this: tool-pack staging is already arch-qualified at [pack-native.sh:284](</J:/Claude code/iolab-release-native-wt/runtime/pack-native.sh:284>), and toollaunch is arch-qualified at line 212. VPCS is the load-bearing collision.

## Q5. Gaps

**Verdict: The six-file list and local layout counts are correct, but the plan misses two release-level problems and several smaller contract updates.**

The six files are complete. The native row references one pin, one YAML, four native scripts, and the already-shipped shared kernel script ([profiles.env:23](</J:/Claude code/iolab-release-native-wt/packaging/macos/lima/profiles.env:23>)). `loadMacOSProfile` validates precisely those assets plus `lib.sh` and the guest directory ([macos_profiles.go:318](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_profiles.go:318>)). Counts of 24 manifest files and 27 staged non-checksum files are correct.

The major missed items are:

1. **The arm64 payload escapes the macOS artifact and becomes a public standalone Linux release asset.**  
   The build upload glob includes both payloads ([release.yml:174](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:174>)), and the release job later publishes everything under `iolbox-linux/**/*` ([release.yml:331](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:331>)). This exceeds “add native-arm64 as a macOS release artifact.” Section 3.4 is wrong to call the upload glob a no-change win. Put the arm64 payload in a separate CI-only artifact consumed by `build-macos`, or explicitly prevent it from entering the published Linux artifact.

2. **The real `upgrade` migration failure described in Q1 is absent.**  
   This will break a normal tagged-release upgrade for users who previously relied on automatic Rosetta selection.

Smaller omissions:

- The shipped native YAML still says the native canary/install scripts are an “OPEN GAP” ([iolbox-native-arm64.yaml:61](</J:/Claude code/iolab-release-native-wt/packaging/macos/lima/iolbox-native-arm64.yaml:61>)). The plan explicitly says not to change it, but shipping that comment is misleading.
- `pack-release.sh` usage says “All six inputs” ([pack-release.sh:21](</J:/Claude code/iolab-release-native-wt/packaging/macos/pack-release.sh:21>)); it must become eight inputs and document both payload roles.
- The packer logs only the amd64 payload ([pack-release.sh:238](</J:/Claude code/iolab-release-native-wt/packaging/macos/pack-release.sh:238>)); log both trusted hashes.
- “exact M6 layout” and M6 test comments should be updated ([pack-release.sh:192](</J:/Claude code/iolab-release-native-wt/packaging/macos/pack-release.sh:192>), [release-layout-test.sh:2](</J:/Claude code/iolab-release-native-wt/packaging/macos/tests/release-layout-test.sh:2>)).
- Release workflow comments still say the macOS job never discovers a second payload ([release.yml:183](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:183>).
- The release body’s general artifact inventory names only the untagged server payload ([release.yml:358](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:358>). If the scope leak is fixed, this can remain singular; otherwise it becomes factually incomplete.

No additional production launcher single-payload assumption exists beyond `selectPayload`. `stageFiles` accepts an arbitrary selected path/basename. Its hardcoded `30-canary.sh` check is real but non-triggering because the entire guest directory is copied and both canaries ship ([macos_lima.go:406](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:406>), [macos_lima.go:422](</J:/Claude code/iolab-release-native-wt/tools/iolab-launcher/macos_lima.go:422>)).

Section 3.4’s checksum claim is otherwise correct: both `sha256sum iolbox-server-*.tar.gz` and the build-artifact path glob match both files ([release.yml:168](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:168>), [release.yml:174](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:174>)).

## Q6. Legal prerequisite

**Verdict: The decision is procedurally correct, neither over-cautious nor sufficient by itself.**

The code really does cause Debian to install `qemu-user-static` and `binfmt-support` during native provisioning ([10-multiarch-native.sh:219](</J:/Claude code/iolab-release-native-wt/packaging/macos/guest/10-multiarch-native.sh:219>), [10-multiarch-native.sh:232](</J:/Claude code/iolab-release-native-wt/packaging/macos/guest/10-multiarch-native.sh:232>)). The current notice’s unqualified “QEMU applies only to Windows” language is therefore misleading about product behavior, even though no QEMU binary is embedded in the Mac tarball ([THIRD_PARTY.md:45](</J:/Claude code/iolab-release-native-wt/THIRD_PARTY.md:45>)).

The ledger explicitly records P7-02 as `UNEVALUATED` and says no review discharge exists ([macos-m7-result.md:329](</J:/Claude code/iolab-release-native-wt/docs/macos-m7-result.md:329>)). Updating the notice is not evidence that the required review occurred. Refusing to mark it discharged is correct.

The weak point is enforcement: prose saying “block tagging” is easy to bypass. Because the release workflow triggers on `v*` tags ([release.yml:6](</J:/Claude code/iolab-release-native-wt/.github/workflows/release.yml:6>)), the implementation should require a concrete owner/legal approval record before the first native-bearing tag. The review need not block merging packaging code, but it must block the tag that produces the artifact.

Overall: approve the selection/filtering and dual-payload packaging design only after fixing the upgrade migration semantics and preventing publication of the arm64 payload as an unintended standalone Linux artifact.