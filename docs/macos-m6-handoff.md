# M6 handoff — Apple Silicon macOS release path

Updated 2026-08-15, end of the M6 qualification session (a follow-on to the
implementation session on the same day). Branch `luna/macos-m6-followups`,
worktree `J:\Claude code\iolab-m6-wt`.

## Status

M6 is **7 of 9 acceptance criteria PASS**, real CI, real hardware. Read
`docs/macos-m6-result.md` for the complete per-criterion table and evidence.
The 2 NOT RUN criteria (3: fresh-account rendered-browser GUI reach; 4:
browser-download Gatekeeper proof) are NOT RUN solely because no genuinely
fresh macOS account and no rendered browser/Chrome control surface were
available in this session — nothing failed, nothing was faked to look like a
pass.

M5 is **fully PASS**, including criterion 2 on `noble-builder-vm`. M4 is
still **PARTIAL** and out of scope for this handoff.

## What changed since the implementation-session handoff

The implementation session (same day, earlier) recorded all 9 criteria NOT
RUN because `gh auth status` failed inside its codex sandbox (Windows
Credential Manager keyring inaccessible to the sandboxed subprocess) and no
CI run had ever been attempted. This session:

1. **Fixed the GitHub-auth blocker**: pass the token via `GH_TOKEN` env var
   instead of relying on `gh`'s keyring-backed config — works for both local
   `gh` calls and codex-sandboxed ones. No token was ever written to a file,
   logged, or committed.
2. **Pushed `luna/macos-m6-followups` to `origin`** (with explicit owner
   confirmation first — this creates visible CI activity on the real repo)
   and **dispatched `release.yml`** against it for the first time ever.
3. **Found and fixed two real, pre-existing bugs** in `runtime/pack-native.sh`
   that blocked `build-linux` (and therefore `build-macos`, which depends on
   it) from succeeding at all — see `docs/macos-m6-result.md` §4. Neither bug
   was introduced by M6; both were latent since an earlier M-phase's
   tool-pack work and simply never exercised through real CI before.
4. Once CI was green, **downloaded the real workflow-produced archive** (both
   via `scp` and via an independent authenticated `curl` against the GitHub
   Actions artifact API) and ran the qualification sequence against real
   Apple Silicon hardware: one-command start, real owner-supplied IOL image
   + real two-node lab + real bidirectional traffic, a second independently-
   built "hybrid baseline" archive (see result doc §3 for why it's
   code-identical to the candidate at this point in the project) upgraded
   into the running candidate with full identity/data preservation proven,
   then the full non-destructive-removal-and-recovery **and** (last, as the
   plan requires) destructive-delete sequence.

## Code and docs to review

Same files as the implementation-session handoff named, unchanged since
then except the two `pack-native.sh` fixes:

- `runtime/pack-native.sh` — **2 new fixes this session** (`5263366`,
  `6411120`): absolute-path normalization for `BUILD_DIR`/`OUT_DIR`, and a
  self-copy `install` → `chmod` fix that the path fix exposed.
- `.github/workflows/ci.yml`, `.github/workflows/release.yml`,
  `packaging/macos/release-manifest.txt`, `packaging/macos/pack-release.sh`,
  `packaging/macos/tests/release-layout-test.sh`, `packaging/macos/
  README.release.md`, `docs/INSTALL.md`, `docs/providers.md`,
  `THIRD_PARTY.md`, `docs/macos-release.md` — all from the implementation
  session, spot-checked this session and found accurate against real
  hardware observations (see result doc §5 row 9).

## Minor findings to fold into a future pass (non-blocking)

1. **`lab.listDocs`/`lab.getDoc` return raw YAML strings**, not parsed JSON
   objects, contrary to what `docs/protocol.md`'s schema sketch implies. The
   frontend clearly does the YAML↔JSON conversion client-side. Not a defect
   — just a documentation-precision gap worth fixing in `protocol.md`
   sometime.
2. **Archive README's `docs/INSTALL.md` link is branch-pinned**
   (`luna-macos-m6-followups`), not tag-pinned — will drift once this branch
   is merged/rebased/deleted. Fix when cutting the first real tagged release
   with this artifact.
3. **Criterion 6's lab was `lab.load`/`lab.start`'d, not additionally
   `lab.saveDoc`'d** as plan §7.3's literal instruction says. Real traffic
   and capability evidence are solid; the "saved" formality specifically
   wasn't exercised for that particular lab (the 9 pre-existing genuinely
   saved labs were confirmed present/consistent throughout, including across
   the upgrade).

## Mac final state

```text
iol22             Stopped   untouched
iolbox-m1-e2e      Stopped   untouched, pre-existing
iolbox-m2-e2e      Stopped   untouched, pre-existing
iolbox-m3-e2e      Stopped   untouched, pre-existing
iolbox-m4-e2e      Stopped   stopped THIS session (owner-approved, to free ~4GB for the qualification VM — was Running before)
iolbox-m5-e2e      Stopped   untouched, pre-existing
m1jammy            Stopped   untouched, pre-existing
m1trixie           Stopped   untouched, pre-existing
iolbox-debian13    absent    deleted as this session's final, deliberate destructive-delete demonstration
```

`~/Library/Application Support/iolbox` (9 labs + 1 image) and the
`iolbox-debian13` structural-gate attestation are in `~/.Trash/` on the Mac —
recoverable until emptied, not gone. If a future session needs that data
back, restore from Trash before assuming it needs to be reconstructed.

`iolbox-m4-e2e` was left Running by the M5/M6 implementation sessions; it was
stopped here purely for memory headroom (8GB Mac, only 140MB free at the
start of this session). Restart with `/opt/homebrew/bin/limactl start
iolbox-m4-e2e` if a future session needs it.

Two branches on `origin`: `luna/macos-m6-followups` (the real work, 3 new
commits beyond `7b7b6ec`) and `luna/macos-m6-baseline` (a pointer-only branch
at the same tip, created solely to get a distinctly-versioned CI archive for
the upgrade proof — safe to delete once no longer needed).

## Next session

1. **Get a genuinely fresh macOS account + rendered browser access** (either
   an interactive-desktop channel an automated session can drive, or the
   owner personally performing the browser-download/Gatekeeper steps and
   handing back transcripts/screenshots) to close criteria 3 and 4 — the
   only two remaining gaps.
2. **When this branch (or a descendant with a real product change) is ready
   to cut an actual tagged release**, redo criterion 7 against a genuinely
   distinct baseline — the "hybrid baseline" used here was code-identical to
   the candidate because M6 shipped no product changes, only packaging; that
   won't be true of the next real release.
3. Fix the tag-pinning and `protocol.md` items in the minor-findings list
   above whenever convenient — neither blocks anything.
