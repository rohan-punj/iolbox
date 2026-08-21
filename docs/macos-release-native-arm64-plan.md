# Plan — add native-arm64 to the official macOS release pipeline

Written 2026-08-20. Branch `feat/release-native-arm64`, worktree
`J:\Claude code\iolab-release-native-wt`, based on `main` at `291854c`
(the M7 merge, "macOS Apple-Silicon native-arm64 launcher support").

**Scope, one sentence:** make a tagged release actually ship the
`native-arm64` profile that M7 built — a second, real `linux/arm64`
supervisor+VPCS+toollaunch+packs payload, plus the six profile assets the
archive is currently missing — **without changing anything about the
existing Rosetta/amd64 artifact**.

Owner has explicitly approved this scope. This document is the plan-level
artifact required to go through `codex gpt-5.6-sol` (medium) review before
implementation; §10 records that review and where I disagreed with it.

---

## 0. What I verified first (do not take the brief's word for it)

Three things in the framing needed correcting before planning:

1. **The primary checkout is not on `main`.** `J:\Claude code\iolab` is
   checked out on `luna/macos-arm64-invariant` at `1a4f617` with untracked
   `docs/macos-*.md` files — mid-flight work. `main` *is* at `291854c` as
   stated, but committing there would have required disturbing that branch.
   All work here is therefore in a fresh worktree
   `J:\Claude code\iolab-release-native-wt` on `feat/release-native-arm64`,
   branched from `main`. Nothing is pushed, tagged, or merged.

2. **The gap is wider than "CI never builds an arm64 payload".**
   `packaging/macos/release-manifest.txt` is missing **all six** assets the
   `native-arm64` row references. A shipped archive today contains the
   `native-arm64` row in `profiles.env` (it is copied verbatim) but none of
   the files that row points at. So the profile is advertised and
   unrunnable.

3. **The M7 gate ledger's mechanical outcome is `NO-PROMOTE`**
   (`docs/macos-m7-result.md` §0: 35 PASS · 1 FAIL · 1 BLOCKED · 29
   UNEVALUATED). native-arm64 became the `auto` default at `e2ffe34` by
   **explicit owner override**, not by passing the gate. That is the owner's
   call and is not re-litigated here — but it directly shapes §8
   (rollout) and §7 (the undischarged legal row), and the release notes must
   not imply a clean gate.

---

## 1. Current state, precisely

### 1.1 What CI builds today

`build-linux` (ubuntu-latest) produces exactly one native payload:

```
runtime/pack-native.sh --supervisor-bin supervisor/bin/supervisor-linux-amd64 \
    --build-dir runtime/build --version "$VERSION"
```

No `--arch`, so `pack-native.sh` takes its `ARCH_EXPLICIT=0` branch and emits
the historical untagged name `iolbox-server-<version>.tar.gz` with no
`manifest.env`.

`build-macos` (also ubuntu-latest) downloads that one artifact, verifies its
digest against `SHA256SUMS-ci.txt`, cross-builds `darwin/arm64`, and calls
`pack-release.sh` with exactly one `--payload`/`--payload-sha256` pair.

### 1.2 The four places that hardcode "exactly one, untagged payload"

| File | Assertion |
|---|---|
| `packaging/macos/pack-release.sh` | `EXPECTED_PAYLOAD="iolbox-server-${VERSION}.tar.gz"`, and a basename equality check that **rejects** an `-linux-arm64` name |
| `packaging/macos/pack-release.sh` | `[ "${#MANIFEST_SOURCES[@]}" -eq 18 ]`, a hardcoded `required_dest` list, a hardcoded `expected` member tree, and `[ ... -eq 20 ]` staged-file count |
| `packaging/macos/tests/release-layout-test.sh` | hardcoded `expected-list.txt`, `SHA256SUMS` line count `= 20`, and `find ... -name 'iolbox-server-*.tar.gz' \| wc -l` `= 1` |
| `tools/iolab-launcher/macos_lima.go` | `selectPayload` globs `iolbox-server-*.tar.gz` with **no arch discrimination** |

### 1.3 The missing six

`profiles.env`'s `native-arm64` row references these; none are in
`release-manifest.txt`:

```
packaging/macos/lima/pinned-image-native-arm64.env  -> lima/pinned-image-native-arm64.env
packaging/macos/lima/iolbox-native-arm64.yaml       -> lima/iolbox-native-arm64.yaml
packaging/macos/guest/10-multiarch-native.sh        -> guest/10-multiarch-native.sh
packaging/macos/guest/30-canary-native.sh           -> guest/30-canary-native.sh
packaging/macos/guest/40-install-payload-native.sh  -> guest/40-install-payload-native.sh
packaging/macos/guest/50-verify-native.sh           -> guest/50-verify-native.sh
```

(`20-kernel-hold-debian.sh` is shared with `debian13` and is already shipped.)

**Why they must land as a set.** `nativePreflight` validates the *pin file*
only (`digests` check). It does **not** stat the YAML template or the guest
scripts — those are validated later in `loadMacOSProfile`, which returns
`codedError(exitUsage, "profile asset is missing: %s")`. That is a **hard
exit, not a fallback**. So shipping the pin file without the YAML would turn
today's graceful `auto-fallback-rosetta` into a hard exit 1 for every
Apple Silicon user. All six, or none.

### 1.4 The payload-selection hazard (the sharpest finding)

`selectPayload` sorts candidates newest-mtime-first, tie-breaking on
lexicographic path:

```go
sort.Slice(candidates, func(i, j int) bool {
    if candidates[i].MTime.Equal(candidates[j].MTime) {
        return candidates[i].Path < candidates[j].Path
    }
    return candidates[i].MTime.After(candidates[j].MTime)
})
```

`pack-release.sh` stamps **every** archive member with the same
`--mtime="@${SOURCE_DATE_EPOCH}"`. Both GNU tar and macOS bsdtar restore
mtimes on extract. So in an extracted two-payload archive the mtimes are
*equal* and the lexicographic tie-break decides — and
`iolbox-server-v0.5.3-linux-arm64.tar.gz` sorts **before**
`iolbox-server-v0.5.3.tar.gz`, because `-` (0x2D) < `.` (0x2E).

**Consequence if we ship two payloads without touching the launcher: every
Rosetta invocation that uses automatic payload discovery, from a clean
extraction of the official two-payload archive, deterministically gets the
arm64 payload staged into its amd64 guest.** Profile-aware selection is a
correctness prerequisite of shipping the second payload, not an enhancement.

Two honest exceptions to "every", per review: `IOLBOX_TARBALL`/`--tarball`
bypasses discovery entirely, and in a *dirty* directory (old and new files
coexisting) some other payload may simply have a later mtime and win before
the tie-break is ever reached. Neither weakens the conclusion — it is still
deterministic for the clean official case, which is the case that ships.

### 1.5 Ordering — why keying off the resolved profile is sound

`runDarwinCLI` order is unambiguous:

1. `resolveAssetRoot` → 2. `loadProfileTableOnly` → 3. `collectHostFacts` →
4. **`resolveProfileSelection`** (fallback decided and final here) →
5. fallback message → 6. `loadMacOSProfile` → … →
9. **`selectPayload`** → 10. `runProvision`/`stageFiles`.

The payload is consumed **strictly after** the fallback decision is final,
including `auto-fallback-rosetta`. Nothing about the payload feeds back into
the profile decision, so passing the resolved profile into `selectPayload`
introduces no ordering problem.

It does not follow that there is "no new failure mode" — that phrasing, in the
first draft of this document, was overstated and the review was right to call
it. A missing or stale payload still fails *after* the profile is locked in,
with no fallback left. §5's "required, not optional" decision exists precisely
to make the missing-payload half of that unrepresentable.

`resolveProfileSelection`'s real semantics, which the selection logic must
respect:

- **explicit** `--profile native-arm64` → preflight failure is a **hard
  error**, never downgraded ("refusing to fall back (fail-closed)").
- **persisted** `native-arm64` → preflight failure degrades to
  `persisted-fallback-rosetta`.
- **auto** → prefers native (post-`e2ffe34`); any native failure degrades to
  `auto-fallback-rosetta`. **Auto never errors on a native failure.**

In every one of those branches `selection.ProfileName` is the final answer by
the time `selectPayload` runs. Keying on it is correct for all five sources
(`explicit-flag`, `persisted`, `persisted-fallback-rosetta`, `auto-native`,
`auto-fallback-rosetta`).

---

## 2. Decision: archive layout (additive, Rosetta byte-unchanged)

The archive gains **one** member:

```
iolbox-macos-arm64/iolbox-server-<version>.tar.gz             (amd64, UNCHANGED)
iolbox-macos-arm64/iolbox-server-<version>-linux-arm64.tar.gz (arm64, NEW)
```

plus the six profile assets from §1.3.

**The amd64 payload keeps its historical untagged name and is built exactly
as today — no `--arch` flag.** This is deliberate and is the single most
important constraint in this plan. Passing `--arch amd64` would flip
`pack-native.sh` into `ARCH_EXPLICIT=1`, renaming the tarball to
`iolbox-server-<v>-linux-amd64.tar.gz` and adding a `manifest.env` that
`install.sh` then arch-checks. That would be exactly the "silently expanding
scope beyond adding native-arm64 as a release artifact" failure mode. Rejected.

The arm64 name is not invented here — it is what `pack-native.sh --arch arm64`
already emits.

**Rejected alternative: a second archive** (`iolbox-macos-arm64-native.tar.gz`).
It would double the download matrix and force users to pick an architecture
*before* the launcher has run its preflight — which is precisely the decision
`resolveProfileSelection` exists to make for them, on their machine, with
fallback. One archive, two payloads, launcher decides. Keep it.

---

## 3. `release.yml` — `build-linux` changes

### 3.1 arm64 supervisor

`build-release.sh` is hardcoded to `GOARCH=amd64`. It gains an `--arch` flag
accepting a comma-separated list; default `amd64` so the no-argument
behaviour is byte-identical:

```sh
bash build-release.sh --arch amd64,arm64
# -> supervisor/bin/supervisor-linux-amd64
#    supervisor/bin/supervisor-linux-arm64
```

**One `npm run build:embed`, N compiles.** This is the reason to put the flag
in `build-release.sh` rather than adding a bare `go build` step in the
workflow, and the reason not to use a separate `ubuntu-24.04-arm` runner job:

- A bare `go build ./cmd/supervisor` in the workflow would ship the committed
  placeholder GUI. `build-release.sh`'s header says so in as many words, and
  the script restores the placeholder via `git checkout --` when it finishes,
  so a later step in the same job would silently get the placeholder. This is
  a documented past incident; do not re-open it.
- A separate arm64 runner job would need its own `npm ci` + `build:embed`,
  and nothing guarantees the two Vite bundles are byte-identical. Building
  both supervisors from one embed *guarantees* both payloads carry the same
  GUI. That is a correctness argument, not a convenience one.

The placeholder assertion runs once, after the single embed, before any
compile.

### 3.2 arm64 VPCS

VPCS is C, so it needs a real cross toolchain:

```yaml
- name: Install rootfs/packaging build deps
  run: sudo apt-get install -y debootstrap zstd build-essential gcc-aarch64-linux-gnu
```

```sh
IOLBOX_BUILD_DIR=runtime/build-arm64 VPCS_CC=aarch64-linux-gnu-gcc \
  runtime/fetch-vpcs.sh --arch arm64
```

**The separate `IOLBOX_BUILD_DIR` is load-bearing.** `fetch-vpcs.sh` writes to
`$BUILD_DIR/vpcs/vpcs` for *both* architectures and short-circuits with
`if [ -x "$VPCS_OUT_DIR/vpcs" ]; then ... exit 0`. Reusing `runtime/build`
would make the arm64 invocation a silent no-op that leaves the amd64 binary
in place — and `pack-native.sh --arch arm64` would then hard-fail on
`require_elf_arch` (correctly, but confusingly). A distinct build dir avoids
the collision with **no change to `fetch-vpcs.sh`**.

`--arch arm64` sets `VPCS_ARCH_EXPLICIT=1`, so `fetch-vpcs.sh` fails closed on
a wrong ELF rather than warning. Good.

Residual risk: the static link (`LDFLAGS="-static -lpthread -lutil"`) under
`aarch64-linux-gnu-gcc`. Ubuntu's `gcc-aarch64-linux-gnu` ships a static
libc, so this is expected to work, but it is the one genuinely new build step
in this plan and it is unverifiable on the Windows workstation (§9).

### 3.3 arm64 payload

```sh
runtime/pack-native.sh --arch arm64 \
    --supervisor-bin supervisor/bin/supervisor-linux-arm64 \
    --build-dir runtime/build-arm64 \
    --out runtime/build \
    --version "$VERSION"
```

`--build-dir runtime/build-arm64` keeps the arm64 vpcs, the cross-built
toollaunch, and the arm64 tool-pack stage separate from the amd64 ones;
`--out runtime/build` puts the finished tarball beside the amd64 one so the
existing checksum and upload globs pick it up.

`pack-native.sh` already cross-compiles toollaunch, all six simple packs, the
secbench GUI, and every secbench attack binary for `TARGET_ARCH` with
`CGO_ENABLED=0`, and already runs `require_elf_arch` on supervisor/vpcs/
toollaunch. **No change to `pack-native.sh` is needed.**

### 3.4 Checksums, and the artifact scope leak

```sh
sha256sum iolbox-rootfs.tar iolbox-ct-*.tar.zst iolbox-server-*.tar.gz > SHA256SUMS-ci.txt
```

The checksum glob does match both payloads with no change, and that part is
fine.

**But the `upload-artifact` path glob matching both payloads is a bug, not a
win.** This was the review's second blocker and my §3.4 originally called it a
freebie. `release.yml:174` uploads `runtime/build/iolbox-server-*.tar.gz` into
the `iolbox-linux` artifact, and the `release` job at `release.yml:331`
publishes `iolbox-linux/**/*` wholesale onto the draft release. So the arm64
payload would become a **public standalone Linux release asset** — an eighth
shipped target nobody approved, with its own support expectations, drawn from
a build that exists only to feed the macOS archive.

That is exactly the "silently expanding scope" failure this plan is supposed
to avoid, and I missed it.

**Fix:** the arm64 payload goes into its own CI-only artifact
(`iolbox-macos-payload-arm64`) that only `build-macos` downloads. It is never
part of `iolbox-linux` and therefore never reaches the `release` job's
publish list. Concretely:

- the `iolbox-linux` upload glob becomes explicit rather than wildcard, so a
  future third payload cannot leak the same way;
- `SHA256SUMS-ci.txt` still covers both (it is the integrity manifest
  `build-macos` verifies against, and it is genuinely useful there);
- a second `upload-artifact` step publishes only the arm64 tarball plus that
  manifest for `build-macos`'s consumption.

If the owner later *wants* a standalone linux/arm64 server target, that is a
deliberate product decision with its own docs, install guide, and support
boundary — not a side effect of this change.

---

## 4. `release.yml` — `build-macos` changes

The existing "Select and verify exact build-linux payload" step is
generalised into a small bash function invoked twice (amd64, arm64) rather
than duplicated, because its awk-based manifest validation is the security-
relevant part and must not drift between the two copies. It keeps its current
guarantees for each payload:

- exactly one file matching the expected name,
- exactly one `SHA256SUMS-ci.txt`,
- the manifest is well-formed (every line 2 fields, 64 hex chars),
- exactly one line for this basename,
- `sha256sum -c` passes.

Outputs become `path`/`sha256` and `arm64-path`/`arm64-sha256`.

`pack-release.sh` is then called with both pairs (§5), and
`release-layout-test.sh` with both pairs (§6).

---

## 5. `pack-release.sh` changes

New required flags, named by **architecture** not by "native":

```
--payload-arm64 PATH --payload-arm64-sha256 SHA256
```

Rationale for the name: "native" is already overloaded in this repo —
`pack-native.sh` builds the "native (systemd) server tarball" for *both*
architectures, while `native-arm64` is a macOS *profile*. `--native-payload`
would be genuinely ambiguous at the call site. `--payload-arm64` is not.

The existing `--payload`/`--payload-sha256` keep their exact current meaning
(the amd64/Rosetta payload) and their exact current basename check.

**Decision: the arm64 payload is REQUIRED, not optional.** Argument: with the
six profile assets shipped, `nativePreflight` passes on a qualifying host and
`auto` selects native. If the arm64 payload were absent, `selectPayload` would
fail at step 9 — *after* the profile is locked in, with no fallback path —
producing a hard preflight error on a machine that was working yesterday. An
optional flag makes it possible for CI to emit that archive. Making it
required means the packer physically cannot build a half-native archive. The
cost is that there is no "emergency Rosetta-only cut" escape hatch; if that is
ever needed the honest fix is a flag that omits the six profile assets *and*
the payload together, which is not built now (YAGNI, and it would need its own
layout contract).

Mechanical changes:

| Constant | From | To |
|---|---|---|
| manifest entry count | 18 | 24 |
| `required_dest` list | 18 destinations | +6 native destinations |
| `expected` staged member tree | 25 lines | +7 lines (6 assets + arm64 payload) |
| staged file count before `SHA256SUMS` | 20 | 27 |

24 manifest files + `iolbox` + 2 payloads = 27. The double-stage byte-identity
check and the deterministic tar flags are untouched.

---

## 6. `release-layout-test.sh` changes

- `--payload-arm64` / `--payload-arm64-sha256` added and required.
- `expected-list.txt` gains the 6 assets and the arm64 payload (7 lines).
- `SHA256SUMS` line count `20` → `27`.
- The `find ... -name 'iolbox-server-*.tar.gz' | wc -l` assertion changes from
  `= 1` to `= 2`, **and** gains two explicit named checks: exactly one
  `iolbox-server-<v>.tar.gz` and exactly one
  `iolbox-server-<v>-linux-arm64.tar.gz`. A bare count of 2 would pass on two
  arm64 payloads.
- Both payloads' digests are checked against their trusted `--payload*-sha256`.
- The reproducibility re-pack loop passes both pairs through to the packer.
- The forbidden-member regex is unchanged. Verified by inspection that
  `iolbox-server-<v>-linux-arm64.tar.gz` does not match any alternative in
  `(\._[^/]*|\.DS_Store|.*namedfork.*|.*resourcefork.*|.*cisco.*|.*\.i86bi[^/]*|.*\.iol[^/]*)$`
  — in particular `.*\.iol[^/]*$` needs a literal `.iol`, and the archive path
  contains `/iolbox`, not `.iolbox`.

**New assertion worth adding:** the arm64 payload's inner `manifest.env` says
`arch=arm64`. The layout test should extract it and assert that, so a mislabelled
payload is caught by the packaging gate rather than 40 minutes into a guest
provision. Cheap, and it is the only in-archive proof that the second payload
is genuinely arm64.

---

## 7. Launcher changes (`tools/iolab-launcher`)

### 7.1 Signature

```go
func selectPayload(explicit, assetRoot, profileName string) (string, error)
```

Callers: `macos_cli.go:258` passes `selection.ProfileName`.

### 7.2 Matching rules

Let `wantARM64 := profileName == nativeProfileTableName`.

- **`explicit != ""` (i.e. `IOLBOX_TARBALL` / `--tarball`) still wins
  unconditionally.** It keeps its exact current behaviour: stat, must be a
  regular file, return it. This is non-negotiable for backward compatibility —
  every M4–M7 hardware harness in `packaging/macos/tests/` drives the launcher
  by pointing `IOLBOX_TARBALL` at a hand-built tarball with an arbitrary path,
  and hard-failing on an arch/name mismatch would break all of them.
  We do add a **warning** (not an error) to stderr when the explicit
  basename's arch tag contradicts the selected profile, so a human staging the
  wrong file by hand sees it immediately.
- **`wantARM64`**: candidate must have prefix `iolbox-server-` and suffix
  `-linux-arm64.tar.gz`.
- **otherwise (amd64/Rosetta)**: candidate must have prefix `iolbox-server-`
  and suffix `.tar.gz`, and must **not** have suffix `-linux-arm64.tar.gz`.
  `iolbox-server-<v>-linux-amd64.tar.gz` is accepted, because
  `pack-native.sh --arch amd64` can legitimately produce it even though CI
  does not.
- Sorting (newest mtime, lexicographic tie-break) and the two search roots
  (`assetRoot`, then `cwd` if different) are unchanged. Once the candidate set
  is arch-filtered the tie-break is no longer load-bearing, but leaving it
  alone keeps the diff minimal and keeps behaviour identical for the
  single-payload case.
- Error text names the arch and the profile:
  `no iolbox-server-*-linux-arm64.tar.gz payload found for profile "native-arm64"`.

### 7.3 Why key on `nativeProfileTableName` and not the `NATIVE` role

The role column (`NATIVE`) is arguably more semantic, but every other
arch-sensitive decision in the launcher keys on the
`nativeProfileTableName` constant (`resolveProfileSelectionName`,
`nativePreflight`). Using a second, different key for the same concept invites
them to disagree later. One key. Noted as a deliberate choice, not an
oversight.

### 7.4 Backward compatibility, enumerated

| Situation | Behaviour |
|---|---|
| Old archive (1 untagged payload), new launcher, `auto` | native preflight fails on the missing pin file → `auto-fallback-rosetta` → amd64 matcher finds the untagged payload. **Works, unchanged.** |
| New archive, `auto`, non-qualifying host | `auto-fallback-rosetta` → amd64 matcher → untagged payload. **Works.** |
| New archive, `auto`, qualifying host, **existing Rosetta install** | `auto-existing-rosetta-machine` → amd64 matcher → untagged payload. **Works — but only because of the separate `fix/auto-profile-existing-machine` fix; see below.** |
| New archive, `auto`, qualifying host, **fresh machine** | `auto-native` → arm64 matcher → tagged payload. **New behaviour, intended.** |
| New archive, `--profile rosetta-amd64` | amd64 matcher → untagged payload. **Works.** |
| New archive, `IOLBOX_TARBALL` set | explicit wins, as today, + a warning on mismatch. **Works.** |
| User extracted new archive over an old one | arch filtering removes the cross-architecture hazard. It does **not** make selection release-exact: a stale *same-arch* payload with a later mtime still wins. Strictly better, not perfect. |

**Correction — this table was wrong when first written.** It claimed
unqualified backward compatibility. The review found, and I confirmed in code,
that an existing Rosetta user with **no persisted choice** (the default
population — `persistProfileChoice` is only ever called on the explicit-flag
branch) would have `auto` resolve to `native-arm64`, which changes the derived
Lima machine from `iolbox-debian13` to `iolbox-native-arm64`
(`macos_cli.go:207-209`). That makes `./iolbox upgrade` hard-fail at
`macos_lifecycle.go:364-366` (`upgrade requires existing machine
"iolbox-native-arm64"`) and makes `./iolbox start` silently build a *second*
VM while orphaning the real one.

That is a **live bug on `main` today**, independent of this plan — it is
merely latent because no shipped archive contains the native pin file, so
preflight always fails. Shipping the six assets is exactly what makes it live.

It is therefore fixed **separately and first**, on
`fix/auto-profile-existing-machine` off `main`, with its own design note
(`docs/macos-auto-profile-existing-machine-fix.md`) and its own sol review.
**This release-pipeline work depends on that fix being on `main` first** and
must not be merged before it.

### 7.5 Out of scope, flagged not skipped

- **`stageFiles` hardcodes `30-canary.sh`.** `macos_lima.go` asserts
  `test -f $newDir/30-canary.sh` regardless of profile, never using
  `p.canaryStep()`. It passes today only because the whole `guest/` directory
  is copied and `30-canary.sh` happens to be present — which remains true
  after this change, since both canaries ship. So this is latent, not
  triggered, and fixing it is a behaviour change to the Rosetta path's
  staging assertions. **Flagged, not fixed here.**
- **`packaging/macos/iolbox-mac.sh`** is the Rosetta-era shell harness. It is
  explicitly excluded from the archive (the layout test's forbidden-member
  regex names it), its step list hardcodes `40-install-payload.sh`, and its
  `discover_payload()` has the same un-filtered glob. Pointed at a
  two-payload directory it would pick by mtime. It has **no native profile
  support at all** and giving it some is a separate piece of work.
  **Flagged, not fixed here.**
- **`payload_version` cosmetics.** `40-install-payload-native.sh` derives
  `payload_version="${payload_name#iolbox-server-}"; ...%.tar.gz`, so an arm64
  payload logs `v0.5.3-linux-arm64` as its "version". Cosmetic, in one log
  line. Left alone deliberately — trimming it would mean editing a guest
  script that the M7 hardware evidence was collected against.

---

## 8. Release notes and `THIRD_PARTY.md`

### 8.1 Release-notes text in `release.yml`

The current sentence is now wrong:

> The existing amd64 supervisor/VPCS and **x86_64 IOL only** run through
> Rosetta; i386/i86bi and arm64-native IOL are unsupported.

Replacement must say, accurately:

- The archive carries **two** payloads; the launcher picks one per resolved
  profile.
- `native-arm64` runs the supervisor, VPCS, and tool packs as **real arm64
  binaries**; x86_64 IOL is still translated, by **qemu-user inside the
  guest** rather than by Rosetta.
- The Rosetta profiles (`debian13` default, `jammy` compatibility) are
  unchanged and run the amd64 payload under Rosetta.
- **x86_64 IOL only, on both profiles.** i386/i86bi remain unsupported, and
  arm64-native IOL remains unsupported (nobody ships an aarch64 IOL).
- `auto` prefers native on a qualifying host and **falls back to Rosetta**
  when the native preflight fails; `--profile native-arm64` fails closed;
  `IOLBOX_PROFILE=rosetta-amd64` is the durable opt-out.
- `native-arm64` has **no row in `IOLBOX_QUALIFICATION_TABLE`**, so the
  launcher prints `UNMEASURED — CANARY REQUIRED` for it. The notes should not
  imply a qualified profile. This is honest and matches §0.3.

### 8.2 `THIRD_PARTY.md` — yes, it must change, and it ships

`THIRD_PARTY.md` is manifest entry 3: it is copied into the archive as
`notices/THIRD_PARTY.md`. It is a **shipped legal notice**, and it currently
contains statements that this change falsifies:

1. "the exact native Linux payload produced by `runtime/pack-native.sh`" —
   singular. Now two.
2. "builds its Linux/amd64 binary; `runtime/pack-native.sh` places that binary
   at `bin/vpcs`" — now built twice, amd64 and arm64, from the same pinned
   `v0.8.3` / `3870ae8` ref.
3. "x86_64 IOL is the only supported IOL architecture in this Rosetta
   profile" — the archive is no longer single-profile.
4. "QEMU is **not** in the Apple Silicon archive. The QEMU notice below
   applies only to the separate Windows launcher bundle and must not be read
   as a claim that the Mac artifact contains QEMU."

Point 4 is the delicate one. The literal claim **remains true** — no QEMU
binary is in the tarball. But `packaging/macos/guest/10-multiarch-native.sh`
runs, in the guest:

```sh
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    qemu-user-static binfmt-support libc6:amd64 libssl3t64:amd64
```

so choosing the native profile *causes* GPL'd QEMU to be installed on the
user's machine. Leaving a notice that reads "must not be read as a claim that
the Mac artifact contains QEMU" without qualifying it would be misleading by
omission once native is the `auto` default.

**My legal analysis** (stated so the owner can overrule it, not asserted as
settled): iolbox does not redistribute qemu-user-static. Debian does, from
Debian's own repositories, to the user's own guest, under Debian's own
copyright files and source offer. GPL-2.0's source-provision obligation
attaches to the distributor of the binary; iolbox is not that party, exactly
as it is not the distributor of the Debian guest image, the apt indexes, or
any other guest package — a boundary this notice **already** draws for the
guest image. So the obligation I believe is real here is **accuracy of
notice**, and that is what I propose to discharge: rewrite the paragraph to
say plainly that the native profile installs `qemu-user-static` and
`binfmt-support` into the guest from Debian at provisioning time, that they
are GPL-licensed, that iolbox does not redistribute them, and where Debian's
corresponding source lives.

### 8.3 P7-02 — surfaced, not silently discharged

`docs/macos-m7-result.md` row **P7-02** is `UNEVALUATED`. Its raw item
(`.../qemu-user/selection/translator-selection.log`) records
`qemu-user-static 1:10.0.11+ds-0+deb13u1`, `binfmt-support 2.2.2-7+b1`, both
copyright sha256s, `owner_image_excluded: true`,
`owner_licence_excluded: true`, `source_notice_obligations_recorded: true` —
**and `redistribution_review_required: true`, with no record anywhere that
the review was performed.**

I am **not** marking that review done. Writing an accurate notice is not the
same act as performing the redistribution review the M7 contract demanded, and
I have no authority to record one. Position:

> **Prerequisite, owner action required.** Fixing the notice (§8.2) is in
> scope and is done here because shipping a knowingly misleading legal notice
> is worse than the status quo. Discharging `redistribution_review_required:
> true` is **not** in scope and remains open. It should be closed before the
> first tag that ships the native profile, and my §8.2 analysis is offered as
> input to it, not as its conclusion.

This is a blocking item for *tagging*, not for merging this branch.

### 8.4 `packaging/macos/README.release.md`

Also ships (as `README.md`). Its prerequisite list says "Apple Silicon macOS
with **Rosetta available**", which is wrong for the native profile, and its
"Upgrade and support boundary" says the runtime executes everything "under
Rosetta". Both need the same two-profile treatment as §8.1. In scope.

---

## 9. Rollout and versioning

**Decision: a normal next tag. No new feature flag, no opt-in period.**

Arguments for:

- The opt-in/opt-out mechanism **already exists and is richer than a flag
  would be**: `--profile` / `IOLBOX_PROFILE`, a persisted choice in
  `~/Library/Application Support/iolbox/profile-choice.env`, and `auto`'s
  fail-open-to-Rosetta. Adding a build-time flag on top would be a fourth,
  redundant control surface.
- `auto` already prefers native in shipped code (`e2ffe34`, owner ruling). A
  flag here would *contradict* a decision the owner already made rather than
  gate a new one.
- Every native failure path degrades to Rosetta except forced
  `--profile native-arm64`, which is an explicit user request to fail closed.

But the honest counterweight, which the release notes must carry:

- **This change is what makes `auto`'s native preference actually bite.**
  Today the preference is inert: the pin file is missing from every shipped
  archive, so `auto` always falls back. After this, `auto` will genuinely land
  on native for qualifying hosts. Users upgrading in place will change
  runtime, guest, and translator without asking for it.
- The gate ledger is `NO-PROMOTE` with 29 UNEVALUATED rows (§0.3), and
  P7R-02 records the **Rosetta** four-node tier as a hard `FAIL` 0/2 while
  native measured 4/4 — the two arms genuinely diverge and neither is fully
  characterised.

**Therefore:** normal tag, but the first tag carrying this ships with an
explicit, prominent notes line naming native-arm64 as the new default on
qualifying Apple Silicon and naming `IOLBOX_PROFILE=rosetta-amd64` as the
one-line opt-out. That is a documentation-level rollout control, which is the
right weight for a change whose escape hatches already exist and are tested.
A code-level flag would be over-engineering a blocker.

---

## 10. Local verification plan, and its honest limits

No GitHub Actions run is available. Simulation on the Windows workstation:

| Step | Feasible locally? |
|---|---|
| `build-release.sh --arch amd64,arm64` | **Yes** — pure Go cross-compile + one Vite build. Baseline already run green. |
| `pack-native.sh --arch arm64` toollaunch + 7 tool packs + secbench attacks | **Yes** — all Go, `CGO_ENABLED=0`. |
| `fetch-vpcs.sh --arch arm64` | **No.** No `gcc`, no `aarch64-linux-gnu-gcc`, no WSL distro, no Docker on this box. |
| `pack-release.sh` with both payloads | **Yes** — GNU tar 1.35, `file`, `sha256sum`, `gzip` all present. |
| `release-layout-test.sh` | **Yes.** |
| `go build/vet/test ./...` in `tools/iolab-launcher` | **Yes.** |
| `release.yml` YAML parse | **Yes** (`python3 -c "import yaml; ..."`). |
| Actually running the native guest on a Mac | **No.** Out of reach here. |

For the one gap, the arm64 VPCS binary, I will substitute a **clearly
labelled `linux/arm64` Go stand-in** passed via `--vpcs-bin`. It is a genuine
aarch64 ELF, so it exercises `require_elf_arch` and every packaging assertion
truthfully — but the resulting tarball is a **plumbing fixture, not a
releasable payload**, and will be described that way in the report and never
committed. The real `aarch64-linux-gnu-gcc` static link of VPCS is the single
step in this plan that first executes on CI, and that is stated plainly rather
than papered over.

---

## 11. Change inventory

| File | Change |
|---|---|
| `build-release.sh` | `--arch` (comma list), default `amd64`; one embed, N compiles |
| `.github/workflows/release.yml` | `gcc-aarch64-linux-gnu` dep; arm64 vpcs step; arm64 pack-native step; dual payload select/verify; dual args to packer + layout test; new release-notes text |
| `packaging/macos/release-manifest.txt` | +6 native profile assets (18 → 24) |
| `packaging/macos/pack-release.sh` | `--payload-arm64{,-sha256}` required; counts 18→24, 20→27; member tree +7 |
| `packaging/macos/tests/release-layout-test.sh` | dual payload args; counts; per-arch named assertions; `manifest.env` arch check |
| `tools/iolab-launcher/macos_lima.go` | `selectPayload` gains `profileName`, arch filtering, arch-named errors, mismatch warning |
| `tools/iolab-launcher/macos_cli.go` | pass `selection.ProfileName` |
| `tools/iolab-launcher/macos_lima_test.go` | new cases: two-payload dir per profile, equal-mtime tie, missing-arch error, explicit override |
| `THIRD_PARTY.md` | two payloads; VPCS both arches; rewritten QEMU-boundary paragraph; legal prerequisite noted |
| `packaging/macos/README.release.md` | two-profile prerequisites and support boundary |
| `packaging/macos/lima/iolbox-native-arm64.yaml` | **comment only** — drop the stale "OPEN GAP" note (review Q5; reverses my §7.5 position) |

Not changed, deliberately: `runtime/pack-native.sh`, `runtime/fetch-vpcs.sh`,
`packaging/macos/lima/profiles.env`, every native guest **script**, and the
amd64 payload's name, contents, and build command.

Depends on: `fix/auto-profile-existing-machine` landing on `main` first
(§7.4). This branch must not merge before it.

---

## 12. Review record (codex `gpt-5.6-sol`, medium)

Full log: `docs/m7-session-logs/sol-review-release-pipeline.log`; verdict:
`docs/m7-session-logs/sol-review-release-pipeline-last.md`.

Headline: *"The plan is not safe to implement unchanged. The payload-selection
design is sound, but two blockers remain."*

### Accepted in full

1. **Blocker — `upgrade` migration semantics were missing (Q1).** Correct, and
   the most important finding of the review. Folded into §7.4 above and split
   out into its own branch/fix. My original compatibility table was simply
   wrong.
2. **Blocker — the arm64 payload leaks into the published Linux assets (Q5).**
   Correct and entirely missed by me. §3.4 rewritten; the payload now travels
   in a CI-only artifact.
3. **§1.5's "no new failure mode" is overstated (Q2).** Fair. Missing or stale
   payloads can still fail *after* the profile is final. Sentence corrected.
4. **§1.4's "every Rosetta user" is too absolute (Q3).** Fair. The precise
   claim is *every Rosetta invocation using automatic discovery from a clean
   official two-payload extraction*. `IOLBOX_TARBALL` bypasses discovery, and
   a dirty directory can be decided by mtime before the tie-break. Corrected.
5. **`build-release.sh --arch` needs a `trap` for placeholder restoration
   (Q4a).** Good catch — a second compile adds another exit path before the
   existing `git checkout --` restore. Adopting the trap, plus the four
   behavioural tests it lists.
6. **Assorted stale text (Q5 minors).** `pack-release.sh`'s "All six inputs"
   usage → eight; log *both* trusted hashes, not just amd64; the "exact M6
   layout" wording in the packer and the layout test; `release.yml:183`'s
   comment claiming the macOS job "never rebuilds or discovers a second
   payload". All adopted.
7. **The legal item needs a real gate, not prose (Q6).** Right: "block
   tagging" written in a doc is trivially bypassed because the workflow fires
   on `v*`. Adopted — see below for how far I'm willing to go.

### Disagreed with, or narrowed

- **Q5's note that the shipped `iolbox-native-arm64.yaml` still carries an
  "OPEN GAP" comment.** The review is right that shipping it is misleading,
  and I originally said not to touch native guest assets. **I now agree and
  will fix the comment** — it is a pure comment change to a file whose
  *behaviour* the M7 hardware evidence covers, so correcting stale prose does
  not invalidate that evidence. This is a reversal of my §7.5 position, made
  deliberately, and limited to the comment text only.
- **Q4c, "requiring `--payload-arm64` intentionally breaks direct callers of
  the old packer CLI, so usage text, tests, and all invocations must change
  together."** Agreed on substance, but I want it recorded that the only
  in-repo callers are `release.yml` and `release-layout-test.sh`; there is no
  external contract to break. The review's framing implies a wider blast
  radius than exists.
- **Q2's "keying on the profile name intentionally treats any future second
  `NATIVE` row as amd64."** True, and I keep the decision (§7.3), but I am
  **not** adding speculative machinery for a second native row. Instead the
  one-native-row invariant is made explicit in a comment at the constant, so
  the next person adding a `NATIVE` row is told what else must change. YAGNI,
  with a tripwire.
- **Q6's implied remedy of a hard CI gate on the legal review.** I am adopting
  a *checkable* gate rather than a prose promise, but I am **not** inventing
  an approval-record format and wiring the release workflow to enforce it —
  that is an owner/process decision about what constitutes legal sign-off, and
  a launcher agent inventing the schema for it would be worse than the
  problem. What I do: state the prerequisite in the plan, in `THIRD_PARTY.md`,
  and in the release-notes body. **Flagged as an owner decision, unresolved by
  design.**

### Not adopted

- Nothing was rejected outright. Every finding is either adopted or narrowed
  above.
