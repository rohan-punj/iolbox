# Bundling and redistributing qemu-user + binfmt-support in the macOS native-arm64 archive

**Status:** reviewed by Codex `gpt-5.6-sol` (medium) and revised; implemented.
Review log: `docs/m7-session-logs/sol-review-qemu-redistribution.log`.
See §9 for the review response — five blockers were raised and all five changed the design.
**Branch:** `luna/qemu-redistribution` (worktree `iolab-qemu-redist-wt`), off `main` @ `247eeea`
**Supersedes:** the "iolbox does **not** redistribute" boundary in `THIRD_PARTY.md` §"QEMU and the Apple Silicon archive"
**Closes:** M7 ledger row **P7-02** (`docs/macos-m7-result.md`), `redistribution_review_required: true`

---

## 0. The decision this document implements

The owner has decided that iolbox **should redistribute** `qemu-user-static` and
`binfmt-support` itself, bundled into the macOS archive, rather than relying on
the guest's `apt-get` fetching them at provisioning time.

The stated reasoning is that **this is a learner tool**: the archive working
self-contained and offline-reliably for students outranks minimizing what
iolbox ships, and actually taking on redistribution with full GPL compliance is
the right posture rather than leaning on the apt-at-provisioning-time boundary
argument.

This document is the *how*. It does not re-litigate the *whether*.

---

## 1. What is actually being redistributed — corrected

The M7 record (P7-02, from
`docs/m7-evidence/phase3/rohans-macbook-air-t8103/20260819T171741Z/qemu-user/selection/translator-selection.log`)
names two packages:

- `qemu-user-static 1:10.0.11+ds-0+deb13u1`
- `binfmt-support 2.2.2-7+b1`

**Those two packages alone do not contain a translator.** This was verified
directly against the Debian pool during this design (§7.1), and it is the single
most important correction in this document.

In Debian trixie, `qemu-user-static` is a **transitional package**:

```
Package: qemu-user-static
Version: 1:10.0.11+ds-0+deb13u1
Section: oldlibs
Depends: qemu-user (>= 1:10.0.11+ds-0+deb13u1), qemu-user-binfmt (>= 1:10.0.11+ds-0+deb13u1)
Description: QEMU user mode emulation (compat/transitional package)
 In the past, this package provided statically-built qemu user-mode emulation
 binaries. Now this functionality is provided by qemu-user package...
```

Its 70 KB payload is *only* compat symlinks (`qemu-x86_64-static -> qemu-x86_64`),
`qemu-debootstrap`, two man pages, and the copyright file. The real emulator
binaries live in **`qemu-user`** (64 MB `.deb`, 464 MB installed), and the binfmt
registration files live in **`qemu-user-binfmt`** (2 KB).

The current `10-multiarch-native.sh` works only because `apt` silently resolves
those two transitive dependencies. **A bundle built from the two names on record
would ship a working-looking install with no emulator in it.** Any offline
bundle must therefore carry `qemu-user` and `qemu-user-binfmt` explicitly.

### 1.1 The bundle set (dependency-complete closure)

`dpkg -i` performs **no** dependency resolution, so the bundle must be complete.
Resolved from the trixie `Packages` indices (§7.2):

**arm64 — the translator itself**

| Package | Version | `.deb` size |
|---|---|---|
| `qemu-user` | `1:10.0.11+ds-0+deb13u1` | 64,201,280 |
| `qemu-user-binfmt` | `1:10.0.11+ds-0+deb13u1` | 2,068 |
| `qemu-user-static` | `1:10.0.11+ds-0+deb13u1` | 70,188 |
| `binfmt-support` | `2.2.2-7+b1` | 68,200 |
| `libpipeline1` | `1.5.8-1` | 40,224 |

`qemu-user` in trixie has **no `Depends:` field at all** — it is statically
linked. This is why the closure does not explode; it was the main risk going in
and it is not a problem.

`libpipeline1` is a `binfmt-support` dependency. It is very likely already
present in the Lima Debian cloud image, but 40 KB is not worth a guess — it is
bundled. `init-system-helpers` (a `Pre-Depends`) is `Priority: required` and
guaranteed present in any Debian rootfs; it is not bundled.

> **Revised after review.** Reinstalling an identical version is *not* a
> no-op — `dpkg -i` re-unpacks and re-runs maintainer scripts and triggers,
> which for binfmt/qemu means touching live registration state. The installer
> therefore skips any package already at the pinned version (§9, finding 9).

**amd64 — the foreign runtime the x86_64 IOL binary links against**

| Package | Version | `.deb` size |
|---|---|---|
| `libc6` | `2.41-12+deb13u3` | 2,849,576 |
| `libgcc-s1` | `14.2.0-19` | 72,772 |
| `gcc-14-base` | `14.2.0-19` | 49,432 |
| `libssl3t64` | `3.5.6-1~deb13u2` | 2,447,924 |
| `libzstd1` | `1.5.7+dfsg-1` | 303,836 |
| `zlib1g` | `1:1.3.dfsg+really1.3.1-1+b1` | 88,892 |
| `openssl-provider-legacy` | `3.5.6-1~deb13u2` | 312,688 |

`libgcc-s1`, `gcc-14-base`, `libzstd1`, `zlib1g` and `openssl-provider-legacy`
are transitive dependencies of `libc6:amd64` / `libssl3t64:amd64` that the
current apt line never had to name. They are required for an offline
`dpkg -i`.

**Total: 12 packages, ≈ 67.3 MiB.**

> **`gcc-14-base` was added after review.** The first draft of this table was
> hand-walked and stopped one level short: `libgcc-s1:amd64` declares
> `Depends: gcc-14-base (= 14.2.0-19)`. The closure is now computed
> programmatically by `generate-lock.sh` from the verified index rather than
> by hand, so this class of miss cannot recur. See §9, finding 1.

> **Multi-Arch constraint.** Every amd64 package above except
> `openssl-provider-legacy` is `Multi-Arch: same`, which Debian requires to be
> co-installed at the *identical* version as the guest's existing arm64
> instance. This is why the lock is pinned to a Debian snapshot matching the
> guest image's build date rather than to current trixie. See §9, finding 2.

### 1.2 Note on what does *not* change

The **installed** footprint in the guest is unchanged (~464 MB for `qemu-user`).
The current apt path already installs exactly these packages; bundling changes
*where the bytes come from*, not *what ends up installed*.

The cost is mainly **archive size** — but not *only* that, as the first draft
claimed. The launcher stages the whole payload permanently under
`/opt/iolbox-provision/payload`, so the guest also retains roughly the
compressed bundle size on disk in addition to the installed packages
(§9, finding 13).

### 1.3 Rejected: trimming `qemu-user` to just `qemu-x86_64`

`qemu-user` ships ~30 emulator binaries; iolbox needs exactly one
(`qemu-x86_64`). Trimming would cut ~60 MB from the archive.

**Rejected**, for now:

- It turns "we redistribute Debian's binary packages unmodified" into "we
  redistribute a derived work", which is a materially longer GPL story to tell
  correctly (still permitted, but §3's simple treatment stops applying).
- It breaks `dpkg` installation, and with it every `dpkg-query` assertion in
  `verify_runtime()` (`10-multiarch-native.sh:182-211`) — the exact assertions
  that constitute the M7 evidence for this profile.
- It replaces Debian's tested `postinst`/`update-binfmts` registration with
  hand-rolled binfmt writes.

Recorded as a possible future optimization, explicitly not taken here. The
Windows QEMU precedent *does* trim, but it trims a `.zip`-like NSIS tree with no
package manager and no installed-state assertions on the other side; the
situations are not analogous.

---

## 2. Acquisition, pinning and verification at build time

### 2.1 Where this runs

`.github/workflows/release.yml`'s `build-macos` job runs on **`ubuntu-latest`**
(release.yml:252) — there is no macOS runner in the workflow at all. The arm64
payload it consumes is produced upstream in `build-linux` (also `ubuntu-latest`)
by `runtime/pack-native.sh --arch arm64` (release.yml:192-198).

So Debian tooling is available. It is nonetheless **not used**.

### 2.2 Decision: direct pinned HTTPS fetch + sha256 lockfile, not `apt-get download`

`apt-get download qemu-user` on the builder resolves against *whatever
`deb.debian.org/dists/trixie` currently serves*. That is a moving target: trixie
point releases and security updates change `libc6`, `libssl3t64` and `qemu`
versions without notice, and the builder is Ubuntu, so it would additionally
need a trixie `sources.list`, `dpkg --add-architecture arm64`, and an `apt`
sandbox — machinery whose only output is "some `.deb` files".

Instead: **fetch exact pool URLs over HTTPS and verify each against a committed
sha256 lockfile.** No apt, no dpkg, no containers, no foreign-arch setup on the
builder. Every byte that ships is named in git.

> **Revised after review.** Two things changed here. (a) The lock is pinned to
> a specific `snapshot.debian.org` timestamp — matching the pinned guest
> image's build date — rather than resolved against current trixie, which
> fixes both reproducibility and the Multi-Arch version-agreement problem.
> (b) The hashes are no longer hand-committed: `generate-lock.sh` derives them
> from Debian's OpenPGP-signed `InRelease` → `Packages.xz` → `.deb` chain,
> because a hand-written hash proves only that a download did not change, not
> that the original bytes were authentic Debian packages — and these `.deb`s
> run maintainer scripts as root. See §9, findings 2, 6 and 7.

The lockfile is `packaging/macos/guest-assets/qemu-user.lock` — one row per
package, plus a header recording the snapshot timestamp, suite, `InRelease`
sha256 and signature state:

```
# package | version | arch | sha256 | pool path (relative to a Debian archive root)
qemu-user|1:10.0.11+ds-0+deb13u1|arm64|bb194fdc...|pool/main/q/qemu/qemu-user_10.0.11+ds-0+deb13u1_arm64.deb
```

Fetch order per row, first success wins, **sha256 decides in every case**:

1. `https://deb.debian.org/debian/<pool path>` — fast, CDN-backed, but Debian
   *removes* superseded pool files when a package is updated.
2. `https://snapshot.debian.org/archive/debian/<TIMESTAMP>/<pool path>` — the
   permanent archive, immune to supersession, but slow and rate-limited.

This is deliberately belt-and-braces and it answers the reproducibility
question directly: **the lockfile, not the mirror, is the pin.** A mirror that
serves the wrong bytes fails the hash and the build stops. `deb.debian.org` is
the fast path; `snapshot.debian.org` is what keeps the build reproducible after
the pool rotates. Both were verified live (§7.3).

Verified during design: all 11 pool URLs return 200 on `deb.debian.org` today,
and `snapshot.debian.org` serves both the pool path and the `/mr/binary/` API
for these exact versions.

### 2.3 Where the fetch lives

New script **`packaging/macos/guest-assets/fetch-qemu-user.sh`**, invoked from
`runtime/pack-native.sh` only when a new opt-in flag is passed:

```
pack-native.sh --arch arm64 --bundle-guest-qemu
```

The flag is **opt-in and off by default** on purpose. `pack-native.sh` also
produces the generic "any systemd glibc Linux server" package; a plain arm64
server install has no business carrying 67 MiB of x86_64 translation for a
Lima guest. Only the macOS release path passes it (release.yml:192-198).

Fail-closed: if the flag is passed and any row cannot be fetched or fails its
hash, `pack-native.sh` exits non-zero. There is no "carry on without the
bundle" branch — that is precisely the silent-degradation mode this change
exists to remove.

---

## 3. GPL compliance

Both packages are GPL. `qemu-user` is QEMU: GPL-2.0 with components under
LGPL-2.1 and other compatible licenses. `binfmt-support` is GPL-3.0+. Once
iolbox ships the binaries, iolbox owes the obligations — this is the whole point
of the owner's decision.

The Windows QEMU section of `THIRD_PARTY.md` is the house precedent: pin an
exact version + checksum, document how the redistributable was produced,
document the shipped layout, include the license texts in the shipped directory,
and record a written offer for source pointing at the corresponding source for
that exact version. This plan **matches or exceeds** it on each point.

### 3.1 License text — Debian's own `copyright` files, not a generic GPL

Verified by extraction (§7.1), Debian packages carry their canonical
copyright/license file at `/usr/share/doc/<package>/copyright`, in machine-
readable DEP-5 format. There is no top-level `COPYING`. So the Windows recipe's
"include `COPYING`/`COPYING.LIB`" translates to "include the `copyright` files".

These are *better* than a generic GPL text: `qemu-user-static`'s copyright file
is 31,253 bytes of per-file license attribution across the whole QEMU tree,
including the `Files-Excluded` list recording what Debian stripped from
upstream. A generic `COPYING` would lose all of that. Using it is not a
shortcut — it is the correct artifact.

Two independent copies ship, deliberately:

1. **Inside each `.deb`**, untouched — automatic, since the packages ship
   unmodified.
2. **At the archive top level**, extracted and committed to the repo, so a user
   can read the license without extracting a payload and then a `.deb`:

```
iolbox-macos-arm64/notices/qemu-user.copyright        (31,253 bytes)
iolbox-macos-arm64/notices/binfmt-support.copyright   (967 bytes)
```

**Provenance is verifiable.** The M7 record captured sha256s of these exact
files as installed on the qualification Mac. Both reproduce byte-for-byte from
the pool today (§7.1):

| File | sha256 | M7 record |
|---|---|---|
| `qemu-user-static/copyright` | `7092076611fd6b8499ae39720ed86b7977a5dfa378038966d85d9987f7cfe784` | matches |
| `binfmt-support/copyright` | `6b3d59446db277b4c13b9acc08cb1a24b7dcceaa7cd1ca8522720ccbaa40665e` | matches |

That is a strong result: the bytes this plan proposes to ship are provably the
same bytes that were legally reviewed during M7 qualification.

Note `qemu-user`, `qemu-user-binfmt` and `qemu-user-static` are all built from
the **`qemu`** source package and share one copyright file; shipping it once
under the name `qemu-user.copyright` covers all three.

### 3.2 Written offer for corresponding source

Recorded in `THIRD_PARTY.md` and carried in the archive at
`notices/qemu-user.copyright`'s side as part of the notices section.

For **QEMU** (`qemu-user`, `qemu-user-binfmt`, `qemu-user-static`), source
package `qemu`, version `1:10.0.11+ds-0+deb13u1`:

- `https://deb.debian.org/debian/pool/main/q/qemu/qemu_10.0.11+ds-0+deb13u1.dsc`
- `https://deb.debian.org/debian/pool/main/q/qemu/qemu_10.0.11+ds.orig.tar.xz`
- `https://deb.debian.org/debian/pool/main/q/qemu/qemu_10.0.11+ds-0+deb13u1.debian.tar.xz`
- Browsable: `https://sources.debian.org/src/qemu/1%3A10.0.11%2Bds-0%2Bdeb13u1/`
- Equivalent: `apt-get source qemu-user-static` on a trixie guest with
  `deb-src` enabled.

For **binfmt-support**, binary version `2.2.2-7+b1`:

- `https://deb.debian.org/debian/pool/main/b/binfmt-support/binfmt-support_2.2.2-7.dsc`
- `https://deb.debian.org/debian/pool/main/b/binfmt-support/binfmt-support_2.2.2.orig.tar.gz`
- `https://deb.debian.org/debian/pool/main/b/binfmt-support/binfmt-support_2.2.2-7.debian.tar.xz`

**The `+b1` is deliberate and must not be "corrected".** `2.2.2-7+b1` is a
binNMU — a binary-only rebuild by Debian's buildds against updated build-deps,
with **no source change**. The corresponding source for binary `2.2.2-7+b1` is
source `2.2.2-7`. There is no `binfmt-support_2.2.2-7+b1.dsc` and asking for one
returns 404. All six source URLs above were verified to return 200 (§7.3).

Because iolbox ships these `.deb` files **bit-for-bit unmodified**, the offer
correctly points at Debian upstream — exactly the reasoning the Windows section
already uses ("iolbox distributes QEMU **unmodified**; the source offer
therefore points at upstream"). §1.3's decision not to trim is what keeps this
sentence true.

### 3.3 Open legal question — flagged, not answered

**GPL-3.0 §6 / GPL-2.0 §3 permit satisfying the source obligation either by
accompanying the binaries with source, or by a written offer valid for three
years. A URL pointing at a third party's archive (Debian) is the weaker of the
available options, and its sufficiency is a legal judgment, not an engineering
one.** GPL-2.0 §3(c) explicitly permits passing along a received offer only for
noncommercial distribution.

This plan reproduces the posture `THIRD_PARTY.md` already takes for the Windows
QEMU build, so it is at least *internally consistent* and does not make iolbox's
position worse. But it does not independently establish that a
pointer-to-Debian offer is sufficient for iolbox's distribution.

The conservative alternative — mirroring the ~50 MB of `qemu` source tarballs
into the release, or hosting them on a URL iolbox controls — is mechanically
easy and is *not* proposed here only because it is a legal call above this
document's pay grade.

**Superseded by the review.** Sol's position — which I accepted — is that this
is not an ancillary legal question to flag but a release-blocking
implementation choice, and that a compliant option was mechanically available.
The design now **accompanies the binaries with corresponding source**,
published as a release asset from the same page
(`fetch-corresponding-source.sh`). See §9, finding 5, and the narrowed
residual question in §8.

---

## 4. Guest-side install, offline

### 4.1 Delivery: inside the arm64 payload tarball

Three routes exist for getting `.deb` files into the guest. Chosen route and
reasoning:

| Route | Verdict |
|---|---|
| **(a)** New rows in `release-manifest.txt` | **Rejected.** `pack-release.sh:139` and `release-layout-test.sh:171` both require every manifest source to be a regular non-symlink file *inside the repo* — so this mandates committing 67 MiB of binary `.deb` blobs into git, permanently, per version bump. |
| **(b)** Extra `limactl copy` + widened flatten glob | **Rejected.** `macos_lima.go:415` flattens only `*.sh` out of the staged guest dir. Widening it means Go launcher changes and a new copy/mv pair — more moving parts for no benefit. |
| **(c)** Carried inside the arm64 payload tarball | **Chosen.** |

Route (c) requires **no Go launcher change, no `release-manifest.txt` change to
carry the `.deb`s, and no change to any of the five hardcoded archive-layout
assertions** for the `.deb`s themselves. The payload tarball is already:

- built by `pack-native.sh --arch arm64`,
- sha256-verified end-to-end through CI (`release.yml:289-329`, then
  `pack-release.sh:118-121`),
- covered by the archive's internal `SHA256SUMS`,
- copied to the guest by `limactl copy`.

The `.deb`s inherit every one of those guarantees for free.

**Ordering — the one thing that makes this work.** Provisioning order is
`10-multiarch-native.sh` → `20-kernel-hold` → `30-canary` →
`40-install-payload-native.sh` → `50-verify` (`macos_lifecycle.go:312`). Step 10
runs *long before* the payload-install step. But staging happens **once, before
the whole sequence** (`macos_lifecycle.go:388`), and the env map — including
`IOLBOX_PAYLOAD_TARBALL=/opt/iolbox-provision/payload/<basename>`
(`macos_lifecycle.go:222`) — is applied uniformly to **every** step
(`macos_lifecycle.go:202-222`).

So `10-multiarch-native.sh` can read `$IOLBOX_PAYLOAD_TARBALL` and extract just
the `guest-assets/qemu-user/` subtree from it. The tarball is on the guest and
addressable at step 10. **This was the load-bearing uncertainty in the design
and it resolves cleanly.**

Payload layout addition:

```
iolbox-server-<version>-linux-arm64/
  guest-assets/
    qemu-user/
      arm64/*.deb          (5 files)
      amd64/*.deb          (6 files)
      qemu-user.copyright
      binfmt-support.copyright
      MANIFEST             (package|version|arch|sha256|filename, mirrors the lockfile)
      SOURCE-OFFER.txt     (§3.2, verbatim)
```

### 4.2 The `10-multiarch-native.sh` change

`run_provision()` currently does: `dpkg --add-architecture amd64` →
`apt-get update` → `apt-get install ... ` → `update-binfmts --enable`.

It becomes: `dpkg --add-architecture amd64` → **extract bundle → verify each
`.deb` sha256 against the bundled `MANIFEST` → `dpkg -i` the whole set** →
`update-binfmts --enable`.

`dpkg --add-architecture amd64` is still required before installing any `:amd64`
package and stays exactly as it is.

Notable properties:

- **`verify_runtime()` (lines 182-211) needs no change.** It asserts *end state*
  — `dpkg-query` install status, `dpkg --print-foreign-architectures`, the
  binfmt handler at `/proc/sys/fs/binfmt_misc/qemu-x86_64`, and real ELF
  `e_machine` bytes via `assert_amd64_elf`. All of that is equally true after
  `dpkg -i`. This is a good sign the original script was written at the right
  altitude.
- Bundling `qemu-user-static` (the transitional package) even though nothing
  needs it functionally is a **deliberate 70 KB spend**: it keeps
  `package_is_installed_native qemu-user-static` (line 185) true, preserving
  continuity with the M7 evidence and the pinned version on record in P7-02.
- `IOLBOX_EXIT_APT` (4) remains the correct failure code; only its doc comment
  widens from "apt / repository failure" to also cover local dpkg installs.
- `dpkg -i` is order-insensitive when given all packages in one invocation —
  it unpacks all, then configures all — so no manual topological ordering is
  needed. The set being dependency-complete (§1.1) is what makes this safe.

### 4.3 No silent network fallback — enforced, not just intended

The offline motivation is defeated entirely if the script quietly falls back to
apt when the bundle is missing or broken. So:

- **No `apt-get update` and no `apt-get install` remain in the native
  provisioning path at all.** Not as a fallback, not behind a conditional. The
  commands are deleted, not guarded.
- Missing bundle, failed extraction, sha256 mismatch, or `dpkg -i` failure all
  `die "$IOLBOX_EXIT_APT"` immediately.
- `packaging/macos/tests/source-policy-test.sh` gains an assertion that
  `10-multiarch-native.sh` contains no `apt-get install` / `apt-get update`
  invocation, so a future edit cannot reintroduce the network dependency
  without tripping a test.

That last item is what turns "we intend this to be offline" into something
mechanically checked.

### 4.4 Honest scope of the offline claim

Bundling makes **package installation** offline. It does **not** make the
`native-arm64` profile offline end-to-end: Lima still downloads the digest-locked
Debian cloud image on first run (`iolbox-native-arm64.yaml:24-27`). The archive
is not an air-gapped install.

The accurate claim is: *once the guest image is present, native-arm64
provisioning no longer depends on Debian's mirrors, and no longer breaks when a
trixie point release moves `libc6`.* `THIRD_PARTY.md` and the release notes must
say that and not overclaim. **Removing the trixie-moves-under-us failure mode is
arguably the bigger practical win for a learner tool than offline per se** — the
current path is one Debian point release away from an unreproducible guest.

---

## 5. `THIRD_PARTY.md` changes

1. **§"Separately installed and fetched components"** — the bullet saying guest
   packages "are fetched at provisioning time and are not embedded in the
   archive" gains an explicit carve-out for the bundled translator set.
2. **§"QEMU and the Apple Silicon archive"** — retitled and rewritten. The
   headline "**No QEMU binary is contained in `iolbox-macos-arm64.tar.gz`**"
   is now false and is replaced. The three "Boundary, stated explicitly"
   bullets — in particular "iolbox does **not** redistribute `qemu-user-static`
   or `binfmt-support`" and "No QEMU or binfmt-support binary, source, or
   copyright file is embedded in any iolbox artifact for macOS" — are replaced
   with the affirmative description: what ships, at which pinned versions and
   checksums, in which layout, produced how.
3. **New "License / GPL compliance" subsection**, mirroring the Windows section's
   shape: license texts included (§3.1), written offer for source (§3.2),
   copyright attribution, and the explicit statement that the `.deb` files are
   redistributed unmodified.
4. **The "Open prerequisite, owner action required" block is resolved and
   closed.** It is replaced with a short record of what actually happened: the
   review contemplated by `redistribution_review_required: true` was performed,
   and its outcome was a decision to redistribute deliberately with full
   compliance rather than to rely on the provisioning-time boundary. That block
   asked for the question to be answered before the first tag shipping
   `native-arm64`; it has now been answered, and the answer is recorded here and
   in `docs/macos-m7-result.md`.
5. The Windows QEMU section is **not** touched.

`docs/macos-m7-result.md`'s P7-02 row gets a back-annotation pointing at this
document, so the ledger stops reporting an open obligation that is now closed.

---

## 6. Version bump and changelog

**Yes — this warrants a version bump and a changelog entry**, on three grounds:

- Archive contents change materially (+67 MiB, new `notices/` files, new payload
  subtree).
- Redistribution posture changes, which is legally significant and must be
  discoverable from release notes, not just from a doc diff.
- Provisioning behavior changes: a guest that previously required Debian mirror
  reachability at step 10 no longer does.

Concretely: a **minor** bump, a `CHANGELOG.md` entry, and a release-notes line in
`release.yml` replacing the current "installed from Debian at provisioning time"
wording (release.yml:448), which becomes false.

The release notes must also carry the written offer for source (§3.2), matching
what the Windows section already instructs ("Include this URL in the release
notes / About page alongside the version above so the offer travels with the
binary").

---

## 7. What was verified during design, and how

All of the following was run against the live Debian archive from the
development host. No claim in §1-§3 is assumed.

### 7.1 The packages were downloaded and extracted

`qemu-user-static_10.0.11+ds-0+deb13u1_arm64.deb` and
`binfmt-support_2.2.2-7+b1_arm64.deb` were fetched from `deb.debian.org` and
unpacked with a hand-written `ar` + `tar.xz` reader (no `dpkg` on this host).

- Confirmed `qemu-user-static`'s `control` declares `Section: oldlibs` and a
  transitional description, and its `data.tar.xz` contains **88 entries, none of
  which is an emulator binary** — only symlinks, `qemu-debootstrap`, man pages
  and docs. This is the §1 correction, established by extraction rather than
  inference.
- Confirmed `qemu-user-binfmt` contains only `/usr/lib/binfmt.d/qemu-*.conf`
  symlinks, including `qemu-x86_64.conf`.
- Extracted `/usr/share/doc/<pkg>/copyright` from both and computed sha256:
  both **match the M7 record exactly** (§3.1 table). Both are DEP-5 format;
  neither package contains a top-level `COPYING`, confirming the §3.1 approach.

### 7.2 The dependency closure was computed from the real indices

`dists/trixie/main/binary-{arm64,amd64}/Packages.xz` were downloaded and parsed.
This produced the version, size, sha256 and pool path for all 11 packages in
§1.1, and established that `qemu-user` carries no `Depends:`.

### 7.3 Every URL this plan depends on was probed

- All 11 binary pool URLs: **200** on `deb.debian.org`.
- `snapshot.debian.org`: **200** for the `qemu-user` pool path, and its
  `/mr/binary/qemu-user/1:10.0.11+ds-0+deb13u1/binfiles` API returns per-arch
  hashes — so the §2.2 fallback is real.
- All six source-package URLs in §3.2: **200**.
- `sources.debian.org/src/qemu/1%3A10.0.11%2Bds-0%2Bdeb13u1/`: **200**.
- Confirmed no `binfmt-support_2.2.2-7+b1.dsc` exists, supporting the §3.2
  binNMU reasoning.

### 7.4 What cannot be verified on this host

No Docker, no WSL distro, no Apple Silicon Mac, no Lima. Therefore:

- `dpkg -i` of the bundle against a real trixie arm64 guest is **unverified**.
  The dependency closure is derived from `Packages` metadata, which is
  authoritative for what `dpkg` will demand, but "metadata says complete" is not
  "`dpkg` accepted it".
- Whether `libpipeline1` is already in the Lima cloud image is **unverified**
  (bundled defensively).
- The end-to-end native-arm64 provisioning run is **unverified** and needs real
  hardware.

These are listed again in §8 as owner-facing gaps, not buried here.

---

## 8. Open questions for the owner

These are stated plainly rather than answered, because each is a judgment
outside what this work can settle on its own.

1. **Is "accompanying source, published from the same release page" the
   compliance posture the project wants to stand behind?** It is implemented
   and is the stronger of the two common options (GPLv2 §3(a)/GPLv3 §6(a)-style
   accompaniment, rather than a §3(b) written offer). Whether to *also*
   maintain a standing three-year written offer with a retention commitment is
   a policy decision, not a technical gap. **I am not qualified to rule on the
   exact wording's legal sufficiency and have not tried to.**

2. **Is +67 MiB on the macOS archive acceptable?** §1.3 documents a ~60 MiB
   saving available by trimming `qemu-user` to the single `qemu-x86_64`
   emulator, deliberately not taken because it costs the "unmodified
   redistribution" story and every `dpkg`-based assertion. Reversible if the
   owner prefers the smaller archive.

3. **Hardware validation is a release gate, not an open question.** Per §9
   finding 11, the bundle is a *new qualified configuration* — it pins exact
   glibc/OpenSSL/GCC-runtime versions that M7 never qualified as a set. Before
   tagging, the native-arm64 profile must be exercised on a real Apple Silicon
   Mac, on **both** a pristine pinned guest and a provisioning re-run (the
   re-run path exercises the skip-if-already-installed branch).

4. **The Windows QEMU section may have the same weakness this work just fixed
   for macOS.** Sol observed in passing that `THIRD_PARTY.md:179` points at a
   generic upstream QEMU source page while the shipped Windows build is
   *trimmed* and reports a post-release commit — so its "unmodified, source
   offer points upstream" reasoning may not hold. **Out of scope for this
   branch** (nothing here touches Windows), recorded so it is not lost.

5. **The M7 evidence under-records what was installed.** P7-02 names only the
   two packages that were *requested*; `apt` silently installed `qemu-user`
   and `qemu-user-binfmt` as well. Back-annotated in `docs/macos-m7-result.md`,
   but worth knowing if that ledger is ever used as an inventory.

---


## 9. Adversarial review response (Codex `gpt-5.6-sol`, medium)

Per the owner's standing instruction that any plan-level artifact be reviewed
by Codex `gpt-5.6-sol` at medium reasoning effort before being treated as
final. Full log: `docs/m7-session-logs/sol-review-qemu-redistribution.log`.

Its verdict was that the central QEMU correction and the delivery path were
right, but the design was **"not yet shippable"** on two independent grounds:
an incomplete/version-fragile package closure, and a compliance treatment
covering only part of what the archive actually redistributes. That was
correct on both counts. Every blocker changed the implementation.

### Blockers — all accepted

**1. The closure was incomplete.** `libgcc-s1:amd64` declares
`Depends: gcc-14-base (= 14.2.0-19)`, which §1.1 omitted. Verified directly
against the index — the hand-walk stopped one level short.

Fixed structurally rather than by adding one row: `generate-lock.sh` now
computes the transitive `Depends`/`Pre-Depends` closure programmatically from
the verified index, stopping only at `Essential: yes` / `Priority: required`
packages. **12 packages**, not 11.

**2. Multi-Arch same-version co-installation was unsolved.** `libc6`,
`libgcc-s1`, `gcc-14-base`, `libssl3t64`, `libzstd1` and `zlib1g` are all
`Multi-Arch: same`; Debian requires co-installed instances to be at identical
versions. Pinning amd64 against *current* trixie proves nothing about the
digest-pinned guest image's arm64 versions.

Fixed by pinning the lock to a `snapshot.debian.org` timestamp matching the
**pinned image's build date** (`20260810`), so the versions agree by
construction rather than coincidence. Independently confirmed: the snapshot at
that date carries exactly the versions locked. A guest-side preflight
(`check_multiarch_versions`) additionally compares each bundled amd64 version
against the installed native one and fails with an actionable message naming
the image/lock drift, instead of letting dpkg emit a confusing error deep in a
transaction.

**3. Notices covered 2 of 8 source packages.** Correct and important. The
bundle spans `qemu`, `binfmt-support`, `glibc`, `gcc-14`, `openssl`,
`libzstd`, `zlib`, `libpipeline`. Fixed: `fetch-qemu-user.sh` extracts the
copyright file from every bundled `.deb` at build time — so notices cannot
drift from what shipped — producing all 8.

**4. DEP-5 files are not standalone license texts.** Also correct: Debian
copyright files *reference* `/usr/share/common-licenses/`, which does not
exist in an archive. Verified that the shipped files reference 11 distinct
texts. Fixed: the complete `common-licenses` set (17 files) is extracted from
a pinned `base-files` and shipped at `notices/licenses/`, and the build
**fails** if a copyright file references a text that was not shipped.

**5. §3.3 deferred a release-blocking choice.** The original §3.3 flagged
source-offer sufficiency as an open legal question. Sol argued that a URL to
Debian is not by itself iolbox's offer, and that the doc had to *choose*.
Accepted. The design now takes the strongest mechanically-available option:
**accompany the binaries with source**, published as
`iolbox-macos-arm64-corresponding-source.tar.gz` from the same release page
(`fetch-corresponding-source.sh`, wired into `release.yml`). Verified end to
end — 207 MB, 9 source packages, every component checked against the `.dsc`.

The residual question is narrowed and restated honestly in §8: whether to
*also* maintain a standing three-year written offer is a policy choice, not a
technical gap.

### Majors — accepted

- **6, snapshot underspecification.** The lock now records the snapshot
  timestamp, suite, `InRelease` sha256 and signature state in its header, so
  the whole chain is re-derivable. Sol's point that snapshot.debian.org is a
  poor high-availability build dependency stands; `deb.debian.org` remains the
  fast path with snapshot as the durable fallback, hash-decided either way.
- **7, provenance gap.** Accepted in full and the most valuable finding after
  the closure bug. `generate-lock.sh` now walks
  `InRelease` (gpgv) → `Packages.xz` sha256 → per-`.deb` sha256. Verified
  locally: *Good signature from "Debian Archive Automatic Signing Key
  (13/trixie)"*.
- **8, `dpkg -i` ordering overconfidence.** Now `dpkg --install` →
  `dpkg --configure --pending` → **`dpkg --audit` must be empty**.
  `--force-depends` is not used and is asserted absent by test.
- **9, reinstall is not a no-op.** Correct — re-unpacking re-runs maintainer
  scripts and triggers, which for binfmt/qemu means touching live registration
  state. Sidestepped entirely: `install_bundle` skips any package already at
  the pinned version.
- **10, layout gates.** All five hardcoded gates updated for the one new
  top-level member (`notices/REDISTRIBUTED-PACKAGES.md`). The `.deb`s stay
  inside the payload and change no outer assertion, as sol noted they could.
- **11, not the M7-qualified set.** Accepted: hardware validation is now a
  release **gate** (§8), not an open question.
- **12, parser fail-closed.** Implemented and tested: exact count, rejection
  of unlisted `.deb`s, duplicate rows, unsafe basenames, non-regular files,
  bad arch, non-`.deb` entries, malformed hashes.

### Minors — accepted

- **13, guest disk footprint.** Correct; the earlier "cost is archive size,
  not guest disk" claim ignored that the launcher stages the payload
  permanently under `/opt/iolbox-provision/payload`. Restated.
- **14, "both packages are GPL" too narrow.** Rewritten throughout.

### Where I disagreed

Only on emphasis, and it changed nothing material:

- On **finding 5**, sol wrote that "matching the Windows wording does not
  establish sufficiency" and called the Windows precedent itself weak. Both
  true, and I followed the recommendation. But I note the original §3.3 was
  not claiming sufficiency — it flagged the question precisely *because* it
  was unresolved, which is the behaviour the brief asked for. Sol's stronger
  point, which I accepted, is that flagging is the wrong response when a
  compliant option is mechanically available; it is not that the flag was
  wrong to exist.
- Sol's aside that the **Windows QEMU section is itself weak** (generic
  upstream source page, trimmed bundle, post-release commit) is very likely
  correct and is a real latent issue. It is deliberately **out of scope** here
  — this branch does not touch the Windows section. Recorded in §8 so it is
  not lost.

### Not adopted

Nothing was rejected outright.
