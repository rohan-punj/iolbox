# Fix — bare `auto` must not silently abandon an existing Rosetta install

Written 2026-08-20. Branch `fix/auto-profile-existing-machine`, worktree
`J:\Claude code\iolab-autoprofile-fix-wt`, based on `main` at `00fedce`.

**This is a correctness bug in already-merged, already-pushed launcher code.**
It is not release-pipeline work and does not depend on native-arm64 ever
joining the official release pipeline.

---

## 1. The bug

`e2ffe34` ("auto now prefers native-arm64 per owner promotion ruling", on
`main` since the M7 merge at `291854c`) made bare `auto` resolve to
`native-arm64` whenever `nativePreflight` passes. Nothing anywhere in the
selection path asks whether the user already has a working Rosetta install.

The Lima machine name is derived from the resolved profile, with no
pre-existing-VM check:

`tools/iolab-launcher/macos_cli.go:207-209`
```go
if opts.Machine == "" {
    opts.Machine = "iolbox-" + profile.Name
}
```

So a user whose install is the machine `iolbox-debian13` gets pointed at
`iolbox-native-arm64` the moment `auto` prefers native.

**Who this hits: the default population.** Only *explicit* selections are ever
persisted — `persistProfileChoice` is called solely in the `explicitFlag`
branch (`macos_profile_select.go:321`), and its own doc comment says it is
"never called with `auto` itself". A user who has only ever typed
`./iolbox start` has no persisted choice, so they fall through to the bare-auto
branch every time.

Two distinct symptoms:

**`upgrade` — hard failure.** `macos_lifecycle.go:364-366`
```go
if config.Upgrade && !exists {
    return codedError(exitPreflight, "upgrade requires existing machine %q", machine)
}
```
The user runs the exact command `packaging/macos/README.release.md` documents
("From a later extracted release, run `./iolbox upgrade` …") and gets
`upgrade requires existing machine "iolbox-native-arm64"`.

**`start` — silent divergence, which is worse.** No such guard exists, so
`ensureMachineWithPorts` creates a *brand-new* native VM. The user's real
`iolbox-debian13` VM keeps existing, keeps consuming disk, and becomes
invisible to the launcher. Host-side images/labs are folder-synced so user
data survives, but guest-local state (the installed payload, the license/host
identity attestation, the image cache, anything not on a synced path) is
silently left behind in the orphaned VM.

This is only latent today because no shipped archive contains the
`native-arm64` pin file, so `nativePreflight`'s `digests` check always fails
and `auto` always falls back. It becomes live the moment those assets ship —
which is precisely what the release-pipeline track intends to do. Fixing it
here, standalone and first, is deliberate.

---

## 2. Decision

**Add one more `auto`-only fallback: if bare `auto` would select
`native-arm64`, but the user already has a Lima machine belonging to a
non-native profile row and does *not* have the native machine, keep them on
their existing profile.**

Result shape:

| field | value |
|---|---|
| `Requested` | `auto` |
| `Selected` | `rosetta-amd64` |
| `ProfileName` | the existing machine's row (e.g. `debian13`) |
| `Source` | `auto-existing-rosetta-machine` |
| `FallbackReason` | `existing Lima machine "iolbox-debian13" predates native-arm64; keeping this install on rosetta-amd64 (run --profile native-arm64 to migrate)` |

Because `FallbackReason` is non-empty, `runDarwinCLI:190-192` already prints
it to stderr — the user is told, every run, in one line, with the opt-in
command. No new print site needed.

### 2.1 Why no new flag

The coordinator left the opt-in mechanism to my judgement (new flag vs.
persisting a choice). **Neither: the existing explicit path already is the
migration control, and it is better than a new flag would be.**

`--profile native-arm64` (or `IOLBOX_PROFILE=native-arm64`) already:

- runs the **fail-closed** native preflight — a forced native selection that
  cannot work errors instead of silently degrading
  (`macos_profile_select.go:314-318`);
- **persists** the choice, so the migration is durable and every later bare
  `auto` run honours it via the `persisted` branch, never re-entering this
  new fallback at all;
- is already documented and already tested.

A new `--migrate-to-native` flag would be a fourth control surface meaning
exactly what the first one already means, and would need its own persistence,
its own precedence rules against the persisted choice, and its own tests.
Rejected as over-engineering.

### 2.2 Why the fallback is keyed on "a machine exists", not on a stored marker

The alternative — write a marker file on first successful Rosetta start and
read it here — would only help users who run the *new* binary at least once
before upgrading. The whole affected population is users upgrading *from* an
older binary that never wrote such a marker. Only the Lima machine list is
evidence that exists retroactively. It is also the exact thing that actually
matters: whether `opts.Machine` will resolve to a VM that exists.

### 2.3 Precedence — deliberately the narrowest possible

This new rule fires **only** in the bare-`auto`, `Source == "auto-native"`
case. It does not touch:

- explicit `--profile native-arm64` — still fail-closed, still migrates. This
  is the opt-in, so it must keep working exactly as-is.
- explicit `--profile rosetta-amd64` / a legacy row name — unaffected.
- a persisted choice of either kind — unaffected; the persisted branch returns
  before this code runs.
- `auto` that already fell back for another reason — already Rosetta.
- a fresh machine with no Lima machines at all — still gets native. **The
  promoted default is preserved for new installs**, which is the point of
  `e2ffe34` and is not being reverted.
- users who already migrated (`iolbox-native-arm64` exists) — still native.

### 2.4 `--machine` / `IOLBOX_MACHINE` override

When the user pins a machine name, the `"iolbox-" + profile.Name` derivation
does not apply, so *derived-name* reasoning is meaningless there.

*(The first draft exempted overridden machines entirely. The review found that
this leaves a genuinely destructive hole, and I accept it — see §6.)* The hole:
an existing **running** custom-named Rosetta VM under bare `auto` resolves to
native, `runProvision` finds the machine so `upgrade`'s existence guard
passes, a *running* machine skips attestation validation entirely in
`ensureMachineWithPorts` (`macos_lima.go:325-337`), and `stageFiles` then
installs the **native** profile's guest scripts into a **Rosetta** VM with
nothing downstream to catch it.

So the override is not exempt; it is handled on better evidence:

- override target **does not exist yet** → proceed native. Every M4–M7
  hardware harness lands here, because they pass disposable per-run machine
  names (`hardware-m4.sh`, `hardware-m4-phase7.sh`).
- override target **exists, attestation says non-native** → keep that
  attested profile.
- override target **exists, provenance unknown** → proceed native. We do not
  guess a profile from a name the user invented.

### 2.4a A machine name is not proof of its profile

The deepest point in the review, and it applies to the derived-name path too.
The name is only the *default* derivation; any `--machine` can be paired with
any `--profile`. The host structural-gate attestation
(`~/.iolbox/macos/<machine>-structural-gate.json`, `hostAttestationPath`)
records what the machine was actually provisioned as.

So the attestation **outranks the name in both directions**:

- attestation says the `iolbox-debian13`-named machine is really `native-arm64`
  → let native proceed;
- attestation names a different Rosetta row than the name implies → keep the
  attested row.

An attestation naming a row this asset root does not know is ignored, so a
stale or foreign file cannot inject an unknown profile name.

This is why the adjustment is invoked from `runDarwinCLI` — where
`opts.Machine` is known and still `""` unless the user set it — rather than
from inside `resolveProfileSelection`, which would need an eighth parameter
to learn the same fact. `resolveProfileSelection`'s documented
explicit > persisted > auto contract is left untouched.

### 2.5 Which row to keep

Prefer `table.Default` (`debian13`) when its machine exists; otherwise the
eligible non-native row whose machine exists, **in sorted name order**.

*"First in table order" — as the first draft said — is not implementable.*
`profileTable.Profiles` is a bare `map[string]macOSProfile`
(`macos_profiles.go:109-113`) with no row-order slice, and Go randomizes map
iteration, so a host with two equally-eligible rows would resolve differently
run to run. I caught this myself while reading the type; the review
independently flagged it. Sorting the candidate names makes it deterministic,
which is the "stable sorted priority" option the review accepted.

The first draft also claimed a `jammy`-only user "necessarily" has a persisted
choice and so never reaches this branch. **That is false** and the review was
right to say so: the VM can predate persistence entirely, `persistProfileChoice`
failure is deliberately non-fatal (`macos_profile_select.go:321-325`), and the
choice file can simply be deleted. The existence check handles them correctly
regardless, which is why it is keyed on machines rather than on assumptions
about persistence.

### 2.6 Failure of `limactl list`

**Fail closed for mutating commands (`start`, `upgrade`); log and continue for
`status`/`diagnose`/`stop`.**

*(This reverses the first draft of this note, which said "keep native, log and
continue" on the theory that `runProvision` would fail anyway. The review
showed that reasoning is wrong and I accept the correction — see §6.)*

The two listings are **separate calls**: this compatibility check lists
machines, and `runProvision` lists them again later
(`macos_lifecycle.go:359-362`), after profile loading, host checks, Lima
discovery, sync resolution, payload selection, and port setup. A transient
failure *here* followed by a success *there* would sail straight past the
guard and create exactly the orphaning second VM this fix exists to prevent.
"It would fail anyway" is not true.

Blind fallback to Rosetta is also wrong, because a transient error would then
silently deny a genuinely fresh host the promoted native default. Neither
default is safe, so the honest answer for a command that is about to *create*
something is to refuse and say why.

`status`/`diagnose`/`stop` mutate nothing, so they report the uncertainty and
carry on rather than denying the user their diagnostics — `diagnose` in
particular already tolerates an unavailable machine listing.

---

## 3. Implementation shape (as built)

`tools/iolab-launcher/macos_profile_select.go`:

```go
// machineNameForProfileRow mirrors macos_cli.go's derivation.
func machineNameForProfileRow(row string) string

// existingNonNativeProfileRow reports the profile-table row whose derived
// Lima machine already exists, preferring table.Default. Pure.
func existingNonNativeProfileRow(machines []machineInfo, table profileTable) (string, bool)

// adjustAutoSelectionForExistingInstall is the pure decision function.
func adjustAutoSelectionForExistingInstall(sel profileSelectionResult, table profileTable, machines []machineInfo) profileSelectionResult

// listLimaMachines shells the read-only `limactl list`, reusing
// parseMachineListing, in the same style as limaSupportsVZ.
func listLimaMachines(ctx context.Context, limactlPath string) ([]machineInfo, error)
```

plus the orchestration seam:

```go
// step two of profile resolution; fail-closed on inventory error for
// start/upgrade, tolerant for status/diagnose/stop
func finalizeAutoSelection(ctx context.Context, sel profileSelectionResult,
    table profileTable, command, machineOverride string,
    list func(context.Context) ([]machineInfo, error)) (profileSelectionResult, error)
```

`tools/iolab-launcher/macos_cli.go` calls exactly that, immediately after
`resolveProfileSelection` returns and **before** the `FallbackReason` print
(so the new reason is printed) and before `loadMacOSProfile` (so the machine
name is derived from the adjusted profile):

```go
selection, err = finalizeAutoSelection(context.Background(), selection, earlyTable,
    opts.Command, opts.Machine, func(ctx context.Context) ([]machineInfo, error) {
        return listLimaMachines(ctx, preflightLimactl)
    })
```

**Why the extra seam.** The review's sharpest testing point was that pure
helper tests "can pass even if `runDarwinCLI` never invokes the helper, or
invokes it after machine derivation". `runDarwinCLI` itself cannot be driven
on this builder — it refuses to run anywhere but Darwin/arm64 — so extracting
`finalizeAutoSelection` gives real orchestration coverage of everything except
the single call site: the adjustment being applied, the fail-closed branch,
the non-mutating tolerance, the override handling, and the fact that
`limactl list` is not shelled at all for a non-`auto-native` selection.

Two doc comments that this makes stale are corrected in the same change:
`resolveProfileSelection`'s "single entry point" claim, and the enumerated
`Source` values on `profileSelectionResult`.

---

## 4. Tests

The coordinator named one required case; I am adding the surrounding matrix so
the narrowness claimed in §2.3 is actually enforced rather than asserted.

| # | Case | Expect |
|---|---|---|
| 1 | **required:** existing `iolbox-debian13`, bare auto, native preflight passes | stays Rosetta; `ProfileName=debian13`; `Source=auto-existing-rosetta-machine`; `FallbackReason` non-empty |
| 2 | no machines at all, bare auto, preflight passes | native (promoted default preserved) |
| 3 | `iolbox-native-arm64` already exists | native |
| 4 | both native and Rosetta machines exist | native |
| 5 | unrelated machines only (`default`, `docker`) | native — a foreign Lima machine is not an iolbox install |
| 6 | `Source != "auto-native"` (explicit/persisted/already-fell-back) | unchanged, byte for byte |
| 7 | existing `iolbox-jammy` only, `table.Default` machine absent | keeps `jammy`, not `debian13` (§2.5) |
| 8 | `machineNameForProfileRow` matches the CLI derivation | `iolbox-debian13` |

Case 6 is the regression guard for §2.3, and case 2 is the guard that this
fix does not quietly revert `e2ffe34`.

Added after review (§6):

| # | Case | Expect |
|---|---|---|
| 9 | two eligible non-default rows, run 200× | identical result every time (§2.5 determinism) |
| 10 | attestation says the `iolbox-debian13`-named machine is native | native proceeds |
| 11 | attestation names a different Rosetta row than the name | attested row wins |
| 12 | `--machine` target absent (harness case) | native proceeds |
| 13 | `--machine` target exists, provenance unknown | native proceeds |
| 14 | `--machine` target exists, attested Rosetta | **protected** — the destructive case |
| 15 | `--machine` target exists, attested native | native proceeds |
| 16 | attestation names a row absent from the table | ignored |
| 17 | **orchestration:** `finalizeAutoSelection` applies the adjustment; effective machine is `iolbox-debian13` | passes |
| 18 | **orchestration:** inventory error on `start`/`upgrade` | `exitPreflight`, fail-closed |
| 19 | **orchestration:** inventory error on `status`/`diagnose`/`stop` | tolerated, selection unchanged |
| 20 | **orchestration:** non-`auto-native` selection | unchanged, and `limactl list` never shelled |

**Negative control.** With `adjustAutoSelectionForExistingInstall` forced to a
no-op, cases 1 and 17 fail with exactly the bug's signature
(`ProfileName = "native-arm64", want debian13`) and pass again once restored.
The required test has teeth; it is not asserting its own implementation.

Result: 15 test functions, 34 subtests, all passing;
`go build ./...`, `go vet ./...`, and the full `go test ./...` are green.

---

## 5. Explicitly out of scope

- Actually **migrating** an existing Rosetta VM to native (moving guest state
  between machines). Not attempted; the user opts in with
  `--profile native-arm64`, which builds a fresh native VM, and their old VM
  stays until they remove it.
- Offering to delete the orphaned Rosetta VM. Destructive; not on this path.
- Any release-pipeline change. Separate branch, separate worktree
  (`feat/release-native-arm64` in `J:\Claude code\iolab-release-native-wt`).
  That work **depends on this fix landing first**.

---

## 6. Review record (codex `gpt-5.6-sol`, medium)

Log: `docs/m7-session-logs/sol-review-auto-profile-fix.log`; verdict:
`docs/m7-session-logs/sol-review-auto-profile-fix-last.md`.

Verdict: *"The bug is real. The proposed derived-name compatibility rule is
directionally correct and preserves the fresh-install native default, but the
note is not implementation-ready as written."* Three material defects, all
accepted and all fixed before commit.

### Accepted and fixed

1. **`--machine` exempted a genuinely destructive path (Q4).** The strongest
   finding. A *running* custom-named Rosetta VM skips attestation validation,
   so native guest scripts would be staged into it with nothing downstream to
   catch it. §2.4 rewritten; overrides are now handled on attestation
   evidence instead of being exempt. Tests 12–15.
2. **"Keep native on `limactl list` failure" was wrong (Q5).** My rationale —
   "`runProvision` would fail anyway" — is false, because the two listings are
   separate calls and a transient failure here can be followed by a success
   there. Now fail-closed for `start`/`upgrade`. §2.6 rewritten. Tests 18–19.
3. **"First row in table order" is unimplementable (Q6).** `Profiles` is an
   unordered map. Now `table.Default` first, then sorted names. I caught this
   independently while reading the type; the review confirmed it. Test 9.
4. **A machine name is not proof of its profile (Q2, Q8).** Attestation now
   outranks the derived name in both directions. New §2.4a. Tests 10, 11, 16.
5. **Stale doc comments (Q3).** `resolveProfileSelection` is no longer the
   "single entry point", and `Source` gained a value. Both corrected.
6. **"Bare auto" was imprecise (Q2).** Explicit `--profile auto` /
   `IOLBOX_PROFILE=auto` resolve identically and also reach `auto-native`.
   Wording corrected in code and note.
7. **Pure-helper tests prove nothing about wiring (Q7).** Addressed by
   extracting `finalizeAutoSelection` and testing it directly, plus the
   negative control in §4.
8. **My "a `jammy` user necessarily has a persisted choice" was false (Q6).**
   Persistence can predate the feature, fail non-fatally, or be deleted.
   Corrected in §2.5.

### Noted, not acted on

- **Q1's point that the missing pin file is not the *only* thing keeping the
  bug latent** — `loadMacOSProfile` would also fail on the missing template
  and guest scripts. Correct, and it strengthens rather than changes this fix:
  the accurate framing is "the missing pin is the current deterministic
  auto-selection gate; the bug becomes operational once the complete native
  asset set ships." §1 already says the assets must ship as a set; no code
  change follows from it here.
- **Q3's suggestion of a request/context struct** instead of parameter growth.
  Fair as architecture, but this is an urgent fix to shipped selection logic
  and a signature refactor would enlarge the diff for no behavioural gain.
  Recorded as a deliberate deferral, not an oversight.
- **Q7's full `runDarwinCLI` orchestration test.** Cannot run on this builder
  (`runDarwinCLI` requires Darwin/arm64) and I will not claim a test I cannot
  execute. `finalizeAutoSelection` is the closest honest coverage; the single
  remaining uncovered element is the one call site, which is three lines and
  visible in the diff. A real end-to-end assertion belongs in the Mac hardware
  harnesses.

### Rejected

- Nothing. Every finding is either fixed or explicitly recorded above.
