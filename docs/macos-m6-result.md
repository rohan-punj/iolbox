# M6 result — Apple Silicon macOS release path: **PARTIAL / NOT QUALIFIED**

Updated 2026-08-15 on branch `luna/macos-m6-followups`, worktree
`J:\Claude code\iolab-m6-wt`. This records the implementation and the
qualification attempts available in this session. It does not call M6 a
release PASS: the workflow-produced candidate, authenticated draft assets,
and rendered browser proof were unavailable.

M5 remains **fully PASS**, including criterion 2 on `noble-builder-vm`.
M4 remains **PARTIAL** as documented; its unreconfirmed or unattempted items
were not chased here and are outside M6 scope.

## 1. Account and isolation deviation

Per explicit owner direction, this session used the pre-existing
`rohansharma` account on `192.168.101.166` instead of the genuinely fresh
account required by plan §0. The account already contained Lima machines and
existing iolbox host state from M1–M5. This is a recorded scope deviation, not
a clean-machine qualification: it weakens isolation from prior session state
and provides no proof of the experience of a genuinely first-time user.

The plan's fresh-account gate was therefore intentionally skipped. I did not
describe the Mac as clean, and I did not move the pre-existing host data into
Trash merely to simulate a reset. The temporary `iolbox-debian13` VM created
by the local smoke run was stopped and deleted explicitly; the pre-existing
host data was retained.

## 2. Verdict by acceptance criterion

The statuses below follow the plan's exact vocabulary. `NOT RUN` means that
the complete criterion was not meaningfully executed against the required
workflow-produced candidate; partial local checks are retained as evidence
but do not get rounded up to PASS.

| # | Criterion required for PASS | Status | Evidence / gap |
|---|---|---|---|
| 1 | CI tests/cross-builds the launcher and the workflow binary starts/diagnoses on the real Mac. | **NOT RUN** | Local `go vet`, `go test`, Darwin arm64 build, and Darwin test compilation passed. `actionlint` passed. No GitHub workflow run was available, so the Mac binary was not workflow-proven. |
| 2 | The workflow emits the exact archive/checksum, trusted same-run payload, exact layout/checksums, and reproducible bytes. | **NOT RUN** | The Linux/GNU-tar packer and layout test passed twice on the real Linux test VM with archive hash `a886b85c08eb11a2a6ed21b79119219e5242b1fe865d58e8f83969d4ccd63e69`. This was a local test input, not a workflow artifact and not same-run `build-linux` provenance. |
| 3 | After reset, the fresh account reaches the GUI from the genuine browser-downloaded candidate. | **NOT RUN** | The existing account's local smoke archive started successfully and returned HTTP 200, but there was no fresh account, browser candidate, or rendered GUI session. |
| 4 | Browser quarantine is recorded and the narrow Gatekeeper procedure succeeds. | **NOT RUN** | Local xattr inspection found no quarantine on the copied local archive, directory, or binary. The browser path and any rendered Gatekeeper observation were unavailable; no quarantine mutation was performed. |
| 5 | Authenticated `curl` candidate download, checksums, xattrs, payload identity, and upgrade succeed. | **NOT RUN** | `gh` had no usable credentials and no `IOLBOX_GH_TOKEN`/`GH_TOKEN` was present. No draft asset IDs or authenticated candidate were available, so no upgrade was claimed. |
| 6 | A real owner-supplied x86_64 IOL image runs in the saved two-node lab and passes traffic. | **NOT RUN** | A real owner-supplied x86_64 image was reachable under the existing Mac `~/iolbox-m0/` state, but no M6 workflow candidate and no rendered GUI/lab qualification were run. No synthetic image was used. |
| 7 | The hybrid `7b7b6ec` baseline upgrades to the curl candidate while preserving identities, data, kernel, saved lab, and traffic. | **NOT RUN** | Neither the distinct baseline qualification artifact nor an authenticated candidate was produced. No upgrade or before/after identity table was claimed. |
| 8 | Ordinary removal/recovery and both exact destructive sequences are proven, with the final reset last. | **NOT RUN** | The local smoke VM was stopped and removed as cleanup, but the literal baseline/candidate removal, recovery, pre-browser reset, and final destructive sequence were not run. Pre-existing host data remained intact. |
| 9 | Published draft text and all required docs/archive notices agree with real observations. | **NOT RUN** | The implementation added and statically reviewed the required docs and draft text, but there was no published/draft workflow artifact to review against real candidate observations. |

There were no acceptance-criterion product `FAIL`s in this session; the
missing statuses are qualification gaps caused by unavailable workflow,
GitHub-auth, fresh-account, and browser access. M6 is therefore not publishable
under the plan's publication gate.

## 3. Implementation delivered

- Added the exact 18-entry allow-list at
  `packaging/macos/release-manifest.txt`.
- Added `packaging/macos/pack-release.sh`, which accepts explicit launcher,
  payload, trusted payload digest, version, output, and source-date-epoch
  inputs; verifies Mach-O arm64 and payload identity; builds two independent
  stages; writes the 20-file internal `SHA256SUMS`; and uses the prescribed
  GNU tar/PAX/gzip determinism recipe.
- Added `packaging/macos/tests/release-layout-test.sh` for exact members,
  modes, regular-file-only entries, metadata/resource-fork/Cisco/test rejects,
  checksums, Mach-O arm64, payload identity, and independent reproducibility.
- Added the launcher Go vet/test, Darwin-selected test compilation, and
  `darwin/arm64` cross-build to `.github/workflows/ci.yml`.
- Added the `build-macos` job to `.github/workflows/release.yml`. It consumes
  exactly one selected `build-linux` payload and its `SHA256SUMS-ci.txt`, then
  packs/tests/uploads exactly `iolbox-macos-arm64.tar.gz` and its outer
  checksum. The draft release now depends on that job and includes the Mac
  support contract.
- Added the archive README, Apple Silicon provider documentation, maintainer
  release recipe, expanded installation contract, and Mac/VPCS third-party
  notice. The docs consistently state user-installed Lima, the Debian 13
  default and Jammy compatibility profile, the pinned kernel, Rosetta
  `x86_64`-IOL-only boundary, unsigned/quarantine procedure, loopback-only
  unauthenticated forwards, upgrade, and uninstall/reset boundaries.

`supervisor/internal/web/dist/index.html` is still the committed placeholder;
the real `build-release.sh` embed check was run and its placeholder-restore
step was completed.

## 4. Verification actually run

### Repository/workflow checks

- `go vet ./...` and `go test ./...` passed in `tools/iolab-launcher` using a
  workspace-local Go cache.
- `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build` passed, and the selected
  Darwin test binary compiled. `file` reported `Mach-O 64-bit arm64` for both.
- `bash packaging/macos/tests/lint.sh` passed all 25 scripts. ShellCheck was
  not installed and was recorded as skipped by that lint gate.
- `npm ci` and the real `build-release.sh` embed path passed with a
  workspace-local npm/Go cache. The build printed its committed placeholder
  restoration and the placeholder was rechecked afterward.
- `actionlint` v1.7.7 passed both workflow files.
- `git diff --check` passed.

### Real Linux/GNU-tar packer qualification

Because the Windows worktree did not have a usable Linux native build toolchain,
the packer and layout test were copied to the existing `iolbox-m5-e2e` Lima
guest, where GNU tar was available. With version `m6-local`, source-date epoch
`1786799053`, a real native payload input, and the cross-built Mach-O arm64
launcher, the packer created two identical independent-stage archives and the
layout test rebuilt the archive twice with the same hash:

```text
a886b85c08eb11a2a6ed21b79119219e5242b1fe865d58e8f83969d4ccd63e69
```

The resulting archive had the exact M6 member set, 20 internal checksums, the
trusted payload hash, regular files/directories only, expected modes, and no
forbidden metadata/resources. This is real GNU-tar evidence for the packer,
but not workflow evidence. A Windows Git Bash/NTFS layout attempt was not used
as evidence because it did not preserve executable bits for the archive files.

### Real Mac local smoke

The Linux-test archive was copied to
`/Users/rohansharma/iolbox-m6-local-smoke/iolbox-macos-arm64.tar.gz` on the
Mac, with its outer checksum. The Mac verified the outer and internal checksums,
reported `Mach-O 64-bit executable arm64`, found no quarantine attribute on
the copied archive/directory/binary, and ran:

```text
LIMACTL=/opt/homebrew/bin/limactl ./iolbox diagnose
LIMACTL=/opt/homebrew/bin/limactl ./iolbox start --no-browser --boot-timeout=10m
./iolbox status
./iolbox diagnose
curl --fail --silent --show-error --output /dev/null --write-out '%{http_code}\n' http://127.0.0.1:4001/
```

The local start exited 0; the named machine became Running; HTTP returned
200; diagnostics observed macOS 26.6.1 on host arm64, Lima 2.2.0, guest
`aarch64`, `execution=rosetta-amd64`, the pinned Debian 13 kernel,
`canary=PASS`, active service, and the existing M5 payload identity. This
proves a local smoke path only: it was not the workflow candidate, did not
use a fresh account, and did not include rendered browser or IOL-traffic
qualification.

## 5. Access blockers and evidence discipline

The in-app Browser reported `No browser is available.` The Chrome control
surface reported `Browser is not available: chrome`. Therefore no rendered
browser, download UI, Gatekeeper UI, GUI image picker, saved-lab view, or
browser reload was invented from HTTP results.

`gh auth status` could not read the configured GitHub CLI credentials file
(`Access denied`), and no usable GitHub token was present. Consequently no
`workflow_dispatch` dry run, workflow artifact, draft release, asset ID, or
authenticated curl download was claimed.

## 6. Final Mac state

The last observed Mac inventory was:

```text
iol22             Stopped       (untouched)
iolbox-m1-e2e     Stopped
iolbox-m2-e2e     Stopped
iolbox-m3-e2e     Stopped
iolbox-m4-e2e     Running       (restored to its pre-session state)
iolbox-m5-e2e     Stopped
m1jammy           Stopped
m1trixie          Stopped
iolbox-debian13   absent        (temporary local-smoke VM deleted)
```

Final memory observation was `7514M used`, `1682M wired`, `1447M compressor`,
`119M unused`. The pre-existing host data under
`/Users/rohansharma/Library/Application Support/iolbox` was retained. The
local smoke extraction was retained at
`/Users/rohansharma/iolbox-m6-local-smoke` as non-release evidence.

## 7. Next qualification handoff

Do not publish or call M6 PASS from this commit. The next session should obtain
GitHub authentication, trigger the release workflow's `workflow_dispatch`,
retain the exact run/artifact IDs, and qualify the workflow-produced candidate
through plan §7. The plan's separate baseline artifact must be made from clean
`7b7b6ec` inputs and explicitly not published. If browser control remains
unavailable, the OWNER-GUI rows must stay NOT RUN rather than being inferred
from curl or static HTTP.
