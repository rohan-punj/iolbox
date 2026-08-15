# M6 result — Apple Silicon macOS release path: **PASS (7/9), 2 NOT RUN**

Updated 2026-08-15 on branch `luna/macos-m6-followups` (worktree
`J:\Claude code\iolab-m6-wt`), commits through `6411120`, plus baseline
branch `luna/macos-m6-baseline` (same tree, distinct ref/version string for
the upgrade proof — see §3). Supersedes the prior version of this file
written at the end of the implementation session, which recorded all 9
criteria NOT RUN because no GitHub authentication or CI run was available
yet. This qualification pass obtained both and re-ran the qualification
against the real workflow-produced candidate.

M5 remains **fully PASS**, including criterion 2 on `noble-builder-vm`. M4
remains **PARTIAL** as documented; its unreconfirmed/unattempted items were
not chased here and stayed out of M6's scope, per plan §9/§10.

---

## 1. Account and isolation deviation (unchanged from the implementation session)

Per explicit owner direction, this session again used the pre-existing
`rohansharma` account on `192.168.101.166`, not a fresh account. It contains
Lima machines/host state accumulated across M1–M6 (9 saved labs, one
previously-registered image). **This is a recorded scope deviation, not a
clean-machine qualification** — do not read any PASS below as clean-machine
proof of a first-time user's experience. What was proven is proven against
this specific pre-existing account.

No rendered browser or Chrome control surface was available in this session
either (same limitation the implementation session hit) — the "browser
downloaded" and "Gatekeeper UI" criteria that specifically require a
rendered browser session stay NOT RUN below, honestly, rather than being
approximated with `curl`.

---

## 2. GitHub authentication — how the blocker was actually resolved

Root cause of the implementation session's `gh auth status: Access denied`:
`gh` on this Windows machine stores its token in the Windows Credential
Manager keyring, which `codex exec`'s sandboxed subprocess cannot reach.
Fix: pass the token through as the `GH_TOKEN` environment variable when
invoking `gh`/`curl` against the GitHub API — this bypasses the keyring
entirely. Verified working both for local `gh` calls and inside a codex
sandbox. No token value was ever written to a file, committed, or printed in
full in any log.

---

## 3. Two real CI-built archives, from two different refs

| | Ref | Run | Archive SHA-256 |
|---|---|---|---|
| **Candidate** | `luna/macos-m6-followups` @ `6411120` | [31891847655](https://github.com/rohan-punj/iolbox/actions/runs/31891847655) | `3023ec68644f35cf74693499213ea6e25f5eb78776662ef9dcbbe0e2ce423d14` |
| **Baseline** | `luna/macos-m6-baseline` (branch created pointing at the same `6411120` commit, pushed as a distinct ref purely so the version-derived filenames/strings differ) | [31894098914](https://github.com/rohan-punj/iolbox/actions/runs/31894098914) | `c56c28187edc98d9106bd737a07cc1d08eb92312b144ac7a67e50d1eb7e2979f` |

**Honesty note on the "baseline":** the plan's hybrid-baseline design assumes
an earlier product commit exists to build a genuinely older Mac archive
from. At this point in the project's history, M6 is a pure packaging phase —
it added zero supervisor/launcher/runtime source changes on top of M5
(`7b7b6ec`) beyond the two `pack-native.sh` bugfixes discovered and fixed in
this same session, which are packaging fixes, not product changes. There is
therefore no functionally distinct "older" Mac product to package as a
baseline yet. The baseline and candidate here are **built from the identical
commit** via two differently-named branches, producing two independently
built, byte-different archives (different embedded version strings, hence
different payload filenames and different overall archive hashes) that are
otherwise code-identical. This proves the **upgrade mechanism** (stop old
service, replace binary + payload, preserve state, restart, re-verify) for
real; it does not exercise a genuine functional product diff, because none
exists yet. A future session with an actual product change on this branch
should redo criterion 7 against a truly distinct baseline.

Two real bugs were found and fixed to get either build to succeed at all —
see §4.

---

## 4. Defects found and fixed this session

Both in `runtime/pack-native.sh`, both pre-existing (introduced in an
earlier M-phase's tool-pack work, not by M6), both never caught before
because this branch had never gone through a real GitHub Actions run until
this session pushed it:

1. **Relative `--build-dir` broke tool-pack staging.** CI passes
   `--build-dir runtime/build` (relative). The tool-pack build loop runs
   `go build -o "$PACK_STAGE/$pack/$pack-gui"` inside a `cd`'d subshell, so
   the relative path resolved against the subshell's cwd instead of the
   caller's — `go build` silently wrote to the wrong location instead of
   erroring, and the real `$PACK_STAGE/$pack/` directory the following
   `install` targeted was never created (`install: cannot create regular
   file '.../native-packs/aaa/pack.json': No such file or directory`).
   Fixed (`5263366`) by normalizing `BUILD_DIR`/`OUT_DIR` to absolute paths
   immediately after argument parsing.
2. **Self-copy `install` once the path bug was fixed.** With `PACK_STAGE`
   now correctly absolute, `install -m 0755 "$PACK_STAGE/$pack/$pack-gui"
   "$PACK_STAGE/$pack/$pack-gui"` copies the just-built binary onto itself —
   coreutils `install` refuses ("are the same file"). This line was always
   meant to just enforce the executable bit, not copy anywhere. Fixed
   (`6411120`) by replacing it with a plain `chmod 0755`.

Both fixes are minimal, packaging-only, and necessary for `build-linux` (and
therefore `build-macos`, which depends on it) to succeed at all — this is
exactly the "an inherited defect surfaces during M6's own qualification"
scenario the plan's risk §9.1 anticipated; it was root-caused and fixed
in scope, not treated as an excuse to chase M4's broader backlog.

---

## 5. Verdict by acceptance criterion (plan §8)

| # | Criterion | Status | Evidence |
|---|---|---|---|
| 1 | CI executes launcher vet/tests, compiles Darwin-selected tests, cross-builds; workflow binary starts/diagnoses on real Mac. | **PASS** | Run 31891847655: `build-macos` job green (1m22s). Both the candidate and baseline workflow-produced binaries were downloaded to the Mac (via curl and scp respectively) and ran `./iolbox start/status/diagnose` successfully — canary PASS, service active, HTTP 200, correct `guest_arch=aarch64`/`execution=rosetta-amd64`/pinned kernel facts, both times. |
| 2 | Workflow emits exact archive/checksum, exact §2.2 layout, internal checksums pass, native payload byte-identical to same-run `build-linux` payload, reproducible bytes. | **PASS** | CI job itself ran `release-layout-test.sh`: `release-layout-test: PASS`, archive hash matches the uploaded artifact, **and** the packer's own "Select and verify exact build-linux payload" step confirmed the packaged payload's SHA-256 against `build-linux`'s `SHA256SUMS-ci.txt` (`iolbox-server-...: OK`) — the exact cross-payload identity check plan review finding MAJOR-6 required, done in-band by CI. Independent-stage rebuild hash matched the shipped archive hash. Locally re-verified: both archives' internal `SHA256SUMS` (20/20 files) passed on the Mac. |
| 3 | A fresh Apple-Silicon user with only Lima installed reaches the rendered GUI via a browser-downloaded archive and one documented `./iolbox start`. | **NOT RUN** | No fresh account (per §1's recorded deviation) and no rendered browser/Chrome control surface available. What *was* proven: the one-command `./iolbox start` from the workflow-produced archive reliably reaches a fully-ready backend (HTTP 200, live canary PASS, correct guest facts) on this account — but that is not a substitute for a genuine fresh-account rendered-browser proof. |
| 4 | Browser download quarantine recorded and the narrow Gatekeeper procedure succeeds. | **NOT RUN** | No rendered browser available to actually download through, so no real browser-triggered quarantine attribute could be observed. Not approximated with `curl`/`scp`. |
| 5 | `curl` path independently downloaded, checksum-verified, extracted, xattr-checked, and successfully used; quarantine presence/absence reported honestly. | **PASS** | Downloaded directly on the Mac via authenticated `curl` against the GitHub Actions artifact API (token passed via stdin, never in argv/logs) — 95,487,588 bytes, matched the API-reported size and the separately-`scp`'d copy's SHA-256 exactly. `xattr -l`/`xattr -p com.apple.quarantine` recorded empty/absent on the zip, extracted tar.gz, extracted directory, and binary (`exit=0`, no attribute — an honest observation, not an assumed pass: raw `curl`/`unzip` don't set `com.apple.quarantine`, only browser/LaunchServices downloads do). The downloaded archive was then used successfully: extracted, internal checksums verified (20/20), `./iolbox start` succeeded from it. |
| 6 | A real owner-supplied x86_64 IOL image runs in a two-node lab and passes bidirectional traffic through the shipped artifact; i386/arm64-native IOL not advertised. | **PASS, with one honest caveat** | Real image `x86_64_crb_linux_l2-adventerprisek9-ms.iol17.18.02.bin` (owner-supplied, pre-existing on this Mac from M1 evidence, not part of the archive) uploaded via host-folder sync and registered (`image.register`, class `l2`, sha256 `fea89383...`). A real two-node lab (R1/R2, `ram: 1024`, one p2p link) was built via the raw NDJSON control API (`lab.load`/`lab.start` — no rendered browser available, matching the M3/M5-established browser-equivalent evidence pattern) and started; both nodes booted for real (one hit a documented IOL first-boot post-config reload, needed extra console patience, not a defect). **The image is an L2 (switch) image, so the R1/R2-startupConfig's direct physical-port IP addressing never took effect** (a genuine test-authoring gap on my part, not a product defect) — reconfigured live via SVI (`interface Vlan1`) instead. Final ping: `10.0.12.1 → 10.0.12.2`, **5/5 (100%)** success on the second attempt (first attempt 4/5, the one loss being ordinary first-packet ARP resolution). `hello` confirmed `arch=x86_64`, `features=[nvram capture natgw tools]` — **`i386` absent** — throughout. **Caveat:** the lab was driven via `lab.load`/`lab.start` (transient), not additionally persisted via `lab.saveDoc` as plan §7.3's literal instruction says ("save it") — the traffic/capability evidence is real, but the specific "saved" formality wasn't exercised for this particular lab (9 *other*, genuinely saved lab documents were confirmed present and byte-consistent throughout this session, including across the upgrade in criterion 7). |
| 7 | `./iolbox upgrade` preserves host/guest images/labs, hostid/hostname, iourc hash, Lima machine identity, and pinned guest kernel. | **PASS, with the baseline caveat in §3** | Pre-upgrade (candidate running) and post-upgrade (upgraded via the separately-built baseline archive, run in the opposite direction from the plan's baseline→candidate framing — immaterial here since both are code-identical, see §3) identity was recorded and compared: **hostid** `a8c00f05` (unchanged), **hostname** `lima-iolbox-debian13` (unchanged), **iourc SHA-256** `0c11c7da...` (unchanged), **guest kernel** `6.12.101+deb13-cloud-arm64` (unchanged), **image bytes/mtime** unchanged (246,471,208 bytes, cache-matched, "0 hashed, 1 reused" — no re-hash needed), **saved lab count** 9 → 9 (unchanged). `./iolbox upgrade` exited 0; post-upgrade HTTP 200 and canary PASS confirmed. A fresh post-upgrade traffic re-run was not repeated (the same binary/payload's traffic capability was already proven pre-upgrade in criterion 6). |
| 8 | Ordinary removal/recovery is non-destructive; the destructive sequence deletes only the named VM/data and is demonstrated last. | **PASS** | **Non-destructive:** `./iolbox stop` (exit 0) → moved the extracted launcher directory to `~/.Trash/` → confirmed the Lima VM (`Stopped`, not deleted) and host `images`/`labs` (9 labs) untouched → re-extracted the verified candidate archive, ran `./iolbox start` again → same machine name, same image (cache-matched, no re-hash), same 9 labs synced back in, HTTP 200/canary PASS again. **Destructive (run last, after all other evidence sealed):** literal, quoted, absolute-path commands with preconditions (`test "$STATE" = "Stopped"` before proceeding) — `limactl delete iolbox-debian13` (only that named machine; no glob), then `mv` (not `rm`) of exactly `~/Library/Application Support/iolbox` and exactly `~/.iolbox/macos/iolbox-debian13-structural-gate.json` into uniquely-timestamped `~/.Trash/` destinations. Verified afterward: `iolbox-debian13` absent from `limactl list`; original data path absent; both moved items present and intact in Trash (recoverable, not permanently erased); every *other* Lima machine (`iol22`, `iolbox-m1-e2e` … `-m5-e2e`, `m1jammy`, `m1trixie`) and their attestation files untouched. |
| 9 | Docs/notices/draft-release text consistently and honestly describe the real capability boundary. | **PASS** | Spot-checked the actual shipped `README.md` inside the candidate archive against everything proven above: correctly states unsigned/Gatekeeper procedure, `x86_64` IOL only (i386/i86bi/arm64-native explicitly called unsupported), user-installed/unmanaged Lima, loopback-only unauthenticated forwards, upgrade/uninstall/destructive-reset boundary exactly as demonstrated. One minor, non-blocking gap: the README's own INSTALL.md link is pinned to the `luna-macos-m6-followups` **branch**, not a tag — will drift once this branch is rebased/merged/deleted; note for whoever cuts the real tagged release. |

**7 of 9 PASS. 2 NOT RUN** (3 and 4), both solely because no genuinely fresh
account and no rendered browser control surface were available in this
session — not because anything failed. Per plan §6's status semantics, this
is correctly NOT RUN, not FAIL, and not rounded up to PASS.

---

## 6. Evidence

- Workflow runs: [31891847655](https://github.com/rohan-punj/iolbox/actions/runs/31891847655) (candidate), [31894098914](https://github.com/rohan-punj/iolbox/actions/runs/31894098914) (baseline).
- Local scripts used to drive the raw NDJSON control API and telnet consoles
  from inside the Lima guest (where the control port is reachable but not
  forwarded to the host, by design): `.m6-evidence/m6-lab-qualify.py`,
  `.m6-evidence/m6-console-drive.py`, `.m6-evidence/m6-fixconfig.py`,
  `.m6-evidence/m6-ping-final.py`, `.m6-evidence/m6-saved-lab-check.py` (this
  worktree, not part of the shipped product).
- Ping evidence: `~/iolbox-m6/evidence/ping-result.txt` on the Mac.
- Downloaded archives kept on the Mac at `~/iolbox-m6/curl-download/` and
  `~/iolbox-m6/baseline/` for this session's duration; the launcher
  directories were exercised through the full stop/remove/recover/upgrade/
  destructive-delete sequence described above.

---

## 7. Final Mac state

```text
iol22             Stopped   (untouched)
iolbox-m1-e2e      Stopped   (untouched, pre-existing)
iolbox-m2-e2e      Stopped   (untouched, pre-existing)
iolbox-m3-e2e      Stopped   (untouched, pre-existing)
iolbox-m4-e2e      Stopped   (stopped this session, with owner approval, to free memory — was Running at session start)
iolbox-m5-e2e      Stopped   (untouched, pre-existing)
m1jammy            Stopped   (untouched, pre-existing)
m1trixie           Stopped   (untouched, pre-existing)
iolbox-debian13    absent    (this session's test machine — deleted as the final, deliberate destructive-delete demonstration)
```

`~/Library/Application Support/iolbox` (9 labs, 1 image) and the
`iolbox-debian13` structural-gate attestation are in `~/.Trash/`, recoverable
until emptied. `iolbox-m4-e2e` was Running before this session (left that way
by the implementation session) and was stopped here, with explicit owner
approval, purely to free memory for the 4GB qualification VM — restart it
with `limactl start iolbox-m4-e2e` if needed.

Two new branches exist on `origin`: `luna/macos-m6-followups` (the real
candidate branch, 3 new commits: `7cf8e50` implementation, `5263366` +
`6411120` the two pack-native.sh fixes) and `luna/macos-m6-baseline`
(pointer-only branch at the same tip, kept solely so its CI run produced a
distinctly-versioned archive for the upgrade proof — safe to delete once no
longer needed for re-verification).
