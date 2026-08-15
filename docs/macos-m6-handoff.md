# M6 handoff — Apple Silicon macOS release path

Updated 2026-08-15 at the end of the M6 implementation session on branch
`luna/macos-m6-followups`, worktree `J:\Claude code\iolab-m6-wt`.

## Status

M6 implementation is present, but M6 qualification is **PARTIAL / NOT
QUALIFIED**. Read `docs/macos-m6-result.md` for the complete acceptance table.
No workflow-produced candidate, authenticated draft asset, or rendered-browser
proof was available, so no acceptance criterion was rounded up to PASS.

M5 is **fully PASS**, including criterion 2 on `noble-builder-vm`. M4 is still
**PARTIAL** and its backlog is out of scope for this handoff.

## Explicit account deviation

The owner explicitly approved using the existing `rohansharma` account on
`192.168.101.166`, not a fresh account. It contained pre-existing M1–M5 Lima
machines and host data. This weakens isolation from prior state and proves
nothing about a first-time user's experience. Do not call this a clean-machine
qualification in a later document.

## Code and docs to review

- `.github/workflows/ci.yml`: launcher vet/test, Darwin-selected test compile,
  and `darwin/arm64` cross-build.
- `.github/workflows/release.yml`: exact same-run Linux payload selection,
  `build-macos`, archive verification/upload, and draft release contract.
- `packaging/macos/release-manifest.txt`: exact 18-entry archive allow-list.
- `packaging/macos/pack-release.sh`: explicit-input deterministic packer.
- `packaging/macos/tests/release-layout-test.sh`: exact archive/reproducibility
  gate.
- `packaging/macos/README.release.md`, `docs/INSTALL.md`, `docs/providers.md`,
  `THIRD_PARTY.md`, and `docs/macos-release.md`: user and maintainer contracts.

The committed web distribution file remains the placeholder
`supervisor/internal/web/dist/index.html`; the real embed/build path was run
and restored it before handoff.

## Verification evidence

Repository gates passed: launcher vet/test, Darwin arm64 build and selected
test compilation, script lint, real `build-release.sh` embed, `actionlint`,
and `git diff --check`.

On the existing M5 Lima guest, GNU-tar pack/layout verification passed with
independent and repeated archive hash:

```text
a886b85c08eb11a2a6ed21b79119219e5242b1fe865d58e8f83969d4ccd63e69
```

On the Mac, the copied local smoke archive verified checksums, had no observed
quarantine, started the temporary default profile, produced HTTP 200, and
reported the expected arm64 host / aarch64 guest / Rosetta-amd64 / pinned
kernel/canary facts. This is local smoke evidence only; the archive was not a
workflow artifact and used the pre-existing account.

The in-app browser and Chrome surfaces were unavailable, and `gh` could not
read credentials; therefore no workflow dispatch, draft asset download,
browser/Gatekeeper proof, authenticated curl upgrade, or IOL lab traffic run
was claimed. A real owner-supplied x86_64 IOL image was reachable in the
pre-existing Mac state, but it was not used to fabricate M6 lab evidence.

## Mac final state

`iol22` is untouched and Stopped. `iolbox-m4-e2e` is Running, as it was before
this session; `iolbox-m1-e2e`, `iolbox-m2-e2e`, `iolbox-m3-e2e`,
`iolbox-m5-e2e`, `m1jammy`, and `m1trixie` are Stopped. The temporary
`iolbox-debian13` created by local smoke was stopped and deleted. Existing host
data was retained, and the local smoke extraction remains at
`/Users/rohansharma/iolbox-m6-local-smoke`.

## Next session

Start with the plan's §7 sequence after obtaining authenticated GitHub access:

1. Trigger `workflow_dispatch` and preserve the green run, `iolbox-macos`
   artifact, exact archive/checksum, same-run Linux artifact ID, and payload
   identity.
2. Produce and retain the unpublished hybrid baseline from clean `7b7b6ec`
   inputs.
3. Repeat qualification with whatever browser control is genuinely available.
   If rendered browser access is still unavailable, leave OWNER-GUI criteria
   NOT RUN and say why.
4. Run the real owner-supplied x86_64 two-node lab, upgrade, ordinary removal/
   recovery, and exact destructive sequences only after their preconditions are
   satisfied. Keep the final destructive sequence last.
