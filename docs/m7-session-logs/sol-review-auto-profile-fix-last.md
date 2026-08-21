## Overall verdict

The bug is real. The proposed derived-name compatibility rule is directionally correct and preserves the fresh-install native default, but the note is not implementation-ready as written. It has three material defects:

- `--machine` bypasses protection for existing custom-named Rosetta machines.
- Continuing native after the compatibility `limactl list` fails is not safe; the later list can succeed.
- “First row in table order” cannot be implemented from the current `map`.

## Q1. Is the bug real?

**Verdict: Yes, with two qualifications.**

### (a) Bare auto is not persisted — confirmed

The explicit branch excludes both empty and `auto` at [macos_profile_select.go:309](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:309>). `persistProfileChoice` is called only inside that branch at [macos_profile_select.go:321](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:321>). Bare auto and explicit `--profile auto` therefore do not create the choice file.

With no persisted choice, execution reaches the auto branch at [macos_profile_select.go:353](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:353>) and returns `Source: "auto-native"` when preflight passes at [macos_profile_select.go:371](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:371>).

One nuance: persistence failure is deliberately nonfatal at [macos_profile_select.go:321](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:321>)-[325](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:325>). Therefore even some explicit users can lack a persisted choice.

### (b) Machine derivation has no compatibility check — confirmed

After loading the selected concrete profile, the CLI does exactly:

```go
if opts.Machine == "" {
    opts.Machine = "iolbox-" + profile.Name
}
```

at [macos_cli.go:197](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:197>)-[209](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:209>). No earlier code lists existing machines.

### (c) Upgrade hard-fails — confirmed

`runProvision` lists machines, searches only for the newly derived name, then rejects upgrade if it is absent at [macos_lifecycle.go:360](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:360>)-[366](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:366>).

That directly contradicts the documented expectation that upgrade preserves the Lima machine at [README.release.md:78](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/README.release.md:78>)-[81](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/README.release.md:81>).

### (d) Start creates a second VM — confirmed, assuming ordinary preflights pass

There is no corresponding start guard. An absent selected machine produces `state == ""`; `ensureMachineWithPorts` then invokes `limactl create` at [macos_lima.go:313](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:313>)-[323](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:323>). The existing differently named Rosetta VM is never considered.

“Silently” is mostly accurate: the CLI prints `profile=native-arm64` at [macos_cli.go:231](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:231>)-[232](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:232>), but emits no warning that it is abandoning another iolbox VM.

### Is the absent native pin the only latent blocker?

**No.** It is the immediate preflight blocker, but not the only missing shipped asset.

The current release manifest ships `profiles.env` and Rosetta assets, but omits the native pin, native template, and native guest scripts at [release-manifest.txt:6](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/release-manifest.txt:6>)-[20](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/release-manifest.txt:20>). Yet `profiles.env` references all those native files at [profiles.env:23](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/lima/profiles.env:23>).

The pin’s absence guarantees native preflight failure because it is read at [macos_profile_select.go:278](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:278>)-[285](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:285>). But adding only that pin would merely move failure later: `loadMacOSProfile` also requires the template and every selected guest script at [macos_profiles.go:319](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:319>)-[338](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:338>).

So the accurate statement is: **the missing pin is the current deterministic auto-selection gate; the bug becomes operational only once the complete native asset set ships.**

## Q2. Is the proposed rule correct and narrow?

**Verdict: Correct for the default derived-name case. It does not revert e2ffe34.**

`Source == "auto-native"` is only returned after:

- no non-auto explicit selection;
- no valid persisted selection;
- the native row exists; and
- native preflight passes.

That is enforced by [macos_profile_select.go:309](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:309>)-[350](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:350>) and [363](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:363>)-[371](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:371>).

Consequently it cannot affect:

- forced native;
- forced Rosetta or a legacy row;
- valid persisted selections;
- existing preflight fallback;
- fresh installs with no matching old machine;
- installations where `iolbox-native-arm64` already exists.

Fresh installs still retain the promoted native default.

Two qualifications:

- It also fires for explicit `--profile auto` and `IOLBOX_PROFILE=auto`, because the resolver deliberately treats those identically to empty selection at [macos_profile_select.go:309](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:309>) and [329](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:329>). That is reasonable auto semantics, but “bare auto only” is imprecise.
- A matching machine name is not proof of its actual profile. A user can create `iolbox-debian13` while overriding the profile/machine pairing. The host attestation records the real profile and should take precedence when available; see Q8.

## Q3. Placement

**Verdict: Operationally correct; architecturally acceptable if clearly treated as finalization of auto selection.**

The insertion must be:

1. after `resolveProfileSelection` at [macos_cli.go:184](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:184>);
2. before the fallback print at [macos_cli.go:194](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:194>);
3. before `loadMacOSProfile` at [macos_cli.go:197](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:197>).

A nonempty reason is genuinely printed to stderr at [macos_cli.go:194](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:194>)-[196](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:196>).

Keeping the pure adjustment in `macos_profile_select.go` and its I/O orchestration in `runDarwinCLI` is reasonable. But the current comment calls `resolveProfileSelection` the “single entry point” at [macos_profile_select.go:301](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:301>); that becomes false and must be updated, along with the documented `Source` values at [macos_profile_select.go:78](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:78>)-[82](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profile_select.go:82>).

“Avoiding an eighth parameter” is not a strong architectural reason; a request/context struct would avoid parameter growth. Nevertheless, the proposed placement is the narrowest practical location because it has both resolved selection and the un-derived `opts.Machine`.

## Q4. `--machine` / `IOLBOX_MACHINE`

**Verdict: Skipping fixes the exact derived-name bug, but leaves a real and potentially destructive hole.**

The override comes from `IOLBOX_MACHINE` at [macos_cli.go:41](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:41>)-[46](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:46>) or `--machine` at [macos_cli.go:90](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:90>). Therefore skipping is correct insofar as no alternate machine name will be derived.

But consider an existing, running custom-named Rosetta VM with bare auto:

- auto resolves native;
- the override prevents adjustment;
- `runProvision` finds the custom machine, so upgrade passes its existence guard;
- a running machine bypasses attestation validation at [macos_lima.go:325](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:325>)-[337](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:337>);
- `stageFiles` then installs the native profile’s scripts into that Rosetta VM at [macos_lifecycle.go:387](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:387>)-[392](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:392>).

A stopped mismatched VM is safer because attestation is checked at [macos_lima.go:339](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:339>)-[342](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lima.go:342>).

The M4–M7 harnesses explicitly pass disposable `--machine` names, for example [hardware-m4.sh:168](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/tests/hardware-m4.sh:168>)-[171](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/tests/hardware-m4.sh:171>) and [hardware-m4-phase7.sh:116](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/tests/hardware-m4-phase7.sh:116>)-[123](</J:/Claude code/iolab-autoprofile-fix-wt/packaging/macos/tests/hardware-m4-phase7.sh:123>). Protecting those tests should not justify exempting all real overridden machines. Native harnesses should explicitly set `--profile native-arm64`, or the launcher should inspect an existing override target’s attestation.

## Q5. `limactl list` failure

**Verdict: The note’s rationale is wrong. Neither native nor Rosetta fallback is safest; fail closed.**

The compatibility list and `runProvision` list are separate calls. A transient first failure can be followed by a successful second list. In that case “keep native” proceeds and can create the second VM the fix exists to prevent.

Also, `runProvision` is not immediate: profile loading, host checks, Lima discovery, sync resolution, payload selection, and port setup occur first at [macos_cli.go:197](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:197>)-[268](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:268>). Diagnose does not fail on list failure at all; it prints “machine listing unavailable” and returns success at [macos_cli.go:374](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:374>)-[376](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:376>).

Blind Rosetta fallback is also wrong for fresh installs because a transient list error would defeat the promoted native default.

For mutating commands, the safe behavior is: **if `auto-native` needs the existing-install check and listing fails, return `exitPreflight` without creating or modifying anything.** Diagnose can report the uncertainty without mutating.

## Q6. Which row to keep?

**Verdict: Default-first is sensible, but “first in table order” is impossible with the current model.**

`profileTable.Profiles` is a Go map at [macos_profiles.go:109](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:109>)-[113](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:113>). Parsing inserts rows into that map at [macos_profiles.go:196](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:196>)-[225](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:225>) and retains no row-order slice. Ranging it produces nondeterministic results.

A user with only `iolbox-jammy` should keep `jammy`; the proposed existence check gets that right. But the note’s assertion that such a user “necessarily” has a persisted choice is false: the VM can predate persistence, persistence can fail nonfatally, or the file can be removed.

Use a deterministic rule:

- default machine if present;
- otherwise, if exactly one known non-native row machine exists, use it;
- otherwise require explicit selection or define a stable sorted priority.

## Q7. Test matrix

**Verdict: Good pure-function coverage, insufficient regression coverage.**

The most important missing test is an orchestration-level test:

> Fake `limactl info` passes; first `limactl list` reports `iolbox-debian13`; config has no persisted choice; invoke bare-auto `upgrade`; assert the effective machine is `iolbox-debian13`, the fallback line is printed, and neither `find/create/start` targets `iolbox-native-arm64`.

Without that, every proposed pure helper test can pass even if `runDarwinCLI` never invokes the helper or invokes it after machine derivation.

Also add:

- first compatibility list fails, later list would succeed: command must fail closed and issue no create;
- existing running custom override with Rosetta attestation: must not apply native provisioning;
- two non-default legacy machines with no default: deterministic outcome;
- explicit `--profile auto`, because it also produces `auto-native`;
- the `opts.Machine != ""` guard itself, not merely helper input tests.

## Q8. Other missed consequences

The adjustment is otherwise correctly positioned to update all downstream profile-dependent behavior:

- concrete pin/template/guest scripts come from `selection.ProfileName` at [macos_cli.go:197](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:197>) and [macos_profiles.go:318](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:318>)-[338](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_profiles.go:338>);
- qualification uses the adjusted profile at [macos_cli.go:231](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:231>);
- guest provisioning receives the concrete profile and machine at [macos_lifecycle.go:202](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:202>)-[228](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:228>);
- diagnostics receive the adjusted selection at [macos_cli.go:336](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_cli.go:336>) and expose its fields at [macos_diagnostics.go:139](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_diagnostics.go:139>)-[150](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_diagnostics.go:150>);
- attestation is machine-specific at [macos_lifecycle.go:124](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:124>)-[126](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:126>) and validates the concrete profile at [macos_lifecycle.go:157](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:157>)-[163](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_lifecycle.go:163>);
- foldersync directories are host-global, not profile/machine-derived, at [macos_sync.go:19](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_sync.go:19>)-[36](</J:/Claude code/iolab-autoprofile-fix-wt/tools/iolab-launcher/macos_sync.go:36>).

The key omission is that **machine-name inference should not override contradictory attestation evidence**. If `iolbox-debian13-structural-gate.json` says the machine was provisioned as native, treating the name alone as Rosetta is wrong. Conversely, attestation is the best way to protect custom-named existing Rosetta machines.

Bottom line: implement the default-name compatibility fallback, but revise the note to fail closed on inventory errors, make legacy-row selection deterministic, and address or explicitly reject profile/machine mismatches for existing `--machine` targets.