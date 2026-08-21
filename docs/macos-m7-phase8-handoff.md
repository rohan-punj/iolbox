# M7 Phase 8 handoff — first-tag readiness

Written 2026-08-20 (post-ship). `main` is at `dba09d5`. The M7 macOS
Apple-Silicon native-arm64 track is fully merged and pushed; this handoff
covers what happened after the Phase 7 gate ledger closed and what's left
before the first tag that actually ships native-arm64 and the redistributed
qemu bundle.

## Where things actually stand (do not re-derive this, it's already true)

`main`'s recent history, in order:

- `26e84b8` — Phase 7 gate ledger, `docs/macos-m7-result.md`. Mechanical
  outcome **NO-PROMOTE** (66-row frozen contract: 35 PASS/1 FAIL/1
  BLOCKED/29 UNEVALUATED, mostly uncollected-not-failed hardware evidence
  left on the Mac rather than committed, not actual failures). Native-arm64
  became the `auto` default anyway by **explicit owner override + personal
  GUI validation** — the plan's own section 13 reserves this as a distinct
  authority from a mechanical PROMOTE, and the ledger document is
  deliberately careful never to blur the two. Went through a `codex exec
  gpt-5.6-sol`/medium adversarial review before finalizing.
- `291854c` — the whole M7 branch (`luna/macos-m7-phase4-integration`, 59
  commits, Phases 1-6) merged into `main`, resolving 5 real conflicts
  against fixes that had landed on `main` independently in the meantime.
- `00fedce` — fixed `sysSetns` being hardcoded to the amd64 syscall number
  (308) with no arch tag; now split per-GOARCH (arm64 is 268), caught by a
  user-run background task alongside this session's work.
- `5943fc4` (`fix/auto-profile-existing-machine`) — closed a live
  regression: bare `auto` preferring native-arm64 (`e2ffe34`) derived the
  Lima VM name purely from `"iolbox-" + profile.Name` with zero
  pre-existing-VM awareness. Any existing Rosetta-amd64 user upgrading the
  launcher and running bare `auto` on qualifying Apple Silicon hardware
  would have been silently pointed at a nonexistent `iolbox-native-arm64`
  VM, orphaning their real `iolbox-debian13` VM and its labs. Fixed to
  prefer an existing, attested installation over a fresh native-arm64
  resolution. sol-medium reviewed (3 real defects it caught, fixed before
  commit), 15 tests including a negative control proving the test actually
  catches the bug.
- `247eeea` (`feat/release-native-arm64`) — wired native-arm64 into
  `.github/workflows/release.yml`: CI now builds a linux/arm64 native
  supervisor+toollaunch payload alongside the existing amd64 one and bundles
  both into the macOS archive. The arm64 payload is CI-artifact-only,
  explicitly not published as an eighth Linux release asset (an early draft
  would have leaked it into `iolbox-linux/**/*`, and separately would have
  broken `sha256sum -c` for existing Linux-asset verifiers by listing an
  unpublished file in the published manifest — fixed by splitting into two
  manifests).
- `dba09d5` (`luna/qemu-redistribution`) — **the owner's explicit decision**:
  redistribute `qemu-user` (not rely on the guest's own
  apt-at-provisioning-time fetch from Debian) because "this is a learner
  tool" — prioritize a self-contained, offline-capable archive. Implemented
  as a locked, signature-verified Debian package bundle (12 packages: the
  real `qemu-user`+`qemu-user-binfmt`, plus dependencies — **not**
  `qemu-user-static`, which turned out to be an empty transitional package
  in Debian 13/trixie with no emulator in it; every prior phase's citation
  of that package name was pointing at nothing) shipped inside the arm64
  native payload, installed via offline `dpkg -i`. Full GPL compliance:
  signed-index-pinned lockfile (`packaging/macos/guest-assets/qemu-user.lock`,
  `sources.lock`, `licenses.lock`), all 8 source packages' license/copyright
  texts shipped, corresponding source design in
  `packaging/macos/guest-assets/fetch-corresponding-source.sh` (**not yet
  run** — see below). This closes M7 gate ledger row P7-02 for real, not
  just a corrected notice. `codex exec gpt-5.6-sol`/medium reviewed the
  design (5 real blockers found and fixed, including a dependency-closure
  gap and an unpinned Multi-Arch version-lock risk) before implementation.

**Hardware-validated on the real Mac** (not just against Debian's package
metadata): offline `dpkg -i` succeeds for the full 12-package set with the
guest's network cut, `dpkg --audit`/`--verify` clean, and a real x86_64 IOL
binary correctly runs through the resulting `qemu-x86_64` binfmt handler
(proven by executing the owner's actual IOL binary and getting its real
usage banner, not a proxy test). Two more real bugs were caught and fixed
during that validation pass, both already in `dba09d5`:

- `92c5635` — `pack-native.sh`'s `--bundle-guest-qemu` guard tested the
  executable bit on `fetch-qemu-user.sh`, which is committed mode `0644`
  (every script under `packaging/macos` is) and was always invoked via
  `bash`, never executed directly. **This would have failed every CI
  release build silently** — the guard rejected a tree the build would
  otherwise have handled fine. Confirmed by building from a clean `git
  archive` export, matching what CI's own checkout does.
- `a7b5a02` — the guest's multiarch-version check used an unqualified
  `dpkg-query -W -f='${Version}' "$package"`, which matches every installed
  architecture of a Multi-Arch:same package and concatenates their version
  strings with no separator (`14.2.0-1914.2.0-19` for one arch's `14.2.0-19`
  printed twice). This is invisible on a **first** provisioning run (only
  one architecture's instance exists yet) and breaks every **re-run** with a
  misleading "image/lock drift" error. Fixed by qualifying every
  `dpkg-query` call with `:$(dpkg --print-architecture)` or `:amd64`
  explicitly, and requiring `install ok installed` status before comparing.

Both fixes were reproduced independently before being fixed, and
re-verified on a genuinely fresh Lima VM afterward (delete, re-provision
from scratch), matching this project's standing evidence discipline.

## What's still open — real, not cosmetic

None of these are believed to be broken; they are **genuinely unexercised**,
and this project's own standing rule is that an unrun check is UNEVALUATED,
not a pass:

1. **The `--bundle-guest-qemu` path has never run in real CI.** Everything
   above was built and validated by hand on the physical Mac
   (`rohansharma@192.168.101.186`) from a `git archive` export meant to
   *approximate* what `actions/checkout` + the `build-macos` job in
   `release.yml` would do — but no actual GitHub Actions run has ever
   exercised it. `92c5635`'s bug (which would have failed every CI build)
   was only caught because this session happened to build from a clean
   export by hand; a real CI run is the only way to be sure nothing else
   like it is lurking.
2. **The outer macOS archive was never assembled with the qemu bundle
   present.** This session built and validated the **payload**
   (`runtime/pack-native.sh --arch arm64 --bundle-guest-qemu`) directly, then
   provisioned a guest from it — it did **not** run the full
   `packaging/macos/pack-release.sh` (which packs the Darwin launcher +
   both Linux payloads + Lima profile/provisioner files into
   `iolbox-macos-arm64.tar.gz`) or `packaging/macos/tests/release-layout-test.sh`
   against a build that includes this bundle. The archive-layout assertions
   in that test were updated as part of `ac803bd`, but never actually run
   against a real assembled archive containing the bundle.
3. **The 207 MB corresponding-source asset has never been built or
   published.** `packaging/macos/guest-assets/fetch-corresponding-source.sh`
   exists and was reviewed/designed, but was not run in the hardware
   validation session (disk was the tightest constraint all session — 5.8
   GiB free at the end, and the source bundle alone is ~207 MB across 9
   packages checked against their `.dsc` files). This needs to actually run
   once, produce the real asset, and get attached as a release asset the way
   the design doc specifies — nobody has confirmed it works end-to-end.
4. **Minor, non-blocking**: the guest install script's comment claiming
   `update-binfmts --enable qemu-x86_64` makes registration "explicit and
   idempotent" is not accurate — the real registration is coming from
   systemd's `binfmt.d` (via `qemu-user-binfmt`'s own postinst), not from
   `update-binfmts`'s own database, so that call is a silent no-op (it's
   already guarded with `|| true`, so harmless in practice). Left alone
   deliberately rather than widen scope during hardware validation; worth a
   one-line comment fix whenever someone's next in that file.
5. **No real IOL lab was booted** through the redistributed translator —
   only the IOL binary's own usage banner was proven (no `iourc` licence
   was available in the validation guest). A booted two-node lab with real
   ping traffic through native-arm64 + the bundled qemu-user would be a
   stronger confirmation, though the M7 hardware arc already separately
   proved native-arm64 two-node and four-node traffic work (Phase 7's
   `docs/macos-m7-phase7-mature-reverify.md`) — this item is about
   confirming the *new packaging mechanism* specifically, not IOL
   correctness in general, which is already well-established.
6. **The supervisor/vpcs binaries in the validated payload were reused from
   a prior build**, not rebuilt from `luna/qemu-redistribution`'s own HEAD —
   that branch doesn't touch either, so this is expected, but note it if
   anyone later wants a payload built 100% from a single commit's fresh
   compile for release-note purposes.

## What this handoff is NOT saying

It is not saying anything is broken. Every check that has actually run has
passed, including two genuine hardware-only bugs found and fixed. It is
saying: don't tag a release yet, because items 1-3 above are the actual gap
between "validated by hand on one Mac" and "this is what a real tagged
release build produces."
