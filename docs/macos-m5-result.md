# M5 result — honest Apple Silicon capabilities and diagnostics: **PASS**

Updated 2026-08-15, combining Luna's implementation session (branch
`luna/macos-m5-honest-caps` @ `0f6f5d5`, worktree `J:\Claude code\iolab-m5-wt`),
a same-day continuation session that closed the browser-proof gap Luna's
session could not attempt and fixed two real GUI defects that browser proof
surfaced, and a second continuation pass that closed criterion 2 against a
real non-Mac amd64 Linux target. Same honesty bar as M1–M4: this records
exactly what ran on real hardware versus what only compiled or was
unit-tested, per criterion, with no rounding up. Plan: `docs/macos-m5-plan.md`.

Not merged anywhere. Stack: `main` ← (unmerged) `luna/macos-m1-provisioner` ←
(unmerged) `luna/macos-m3-ux` ← (unmerged, M4 PARTIAL, not restated here) ←
(unmerged) `luna/macos-m5-honest-caps`.

---

## 1. Verdict: PASS — all five criteria proven on real hardware

| # | Criterion | Disposition |
|---|---|---|
| 1 | Mac hello omits `i386`, retains runtime `arch:x86_64` | **PASS on hardware** |
| 2 | A real ordinary amd64 non-Mac target still advertises `i386` with no signal | **PASS on hardware** — `noble-builder-vm` (192.168.226.10, Ubuntu 24.04 LTS "noble", real x86_64 metal-equivalent VM, no relation to the Mac/Lima stack). See §4. |
| 3 | Status/diagnose values are measured, not hardcoded | **PASS on hardware** |
| 4 | GUI visibly retains-but-disables i386 images across every picker and existing-lab flow; server rejects a bypass | **PASS on hardware**, after two real defects found via live browser testing were fixed and reverified (see §2) |
| 5 | The real two-node x86_64 IOL lab still boots and passes traffic | **PASS on hardware** (proven pre-existing by the M1 harness re-run against the M5 build; not repeated in later continuation sessions since none of their fixes touched IOL start/traffic paths) |

Every criterion in the plan's completion gate (§13) is now satisfied by real
hardware evidence.

Note on the plan's original design: the plan (§3) specified an additive
`iolArchitectures: ["x86_64"]` hello field as the GUI's positive signal.
Luna's implementation deliberately cut that field and used
`hello.features.includes("i386")` as the sole capability signal instead — a
documented simplification, not an oversight (see the implementation report).
This does not weaken any of the criteria above; it changes the *mechanism*,
not the guarantee.

---

## 2. Defects found and fixed

### Found and fixed in Luna's implementation session (before any hardware run)

Six items, listed in the implementation report
(`docs/m5-session-logs/luna-implementation-report.md`, "Defects found during
this session"): a host/guest GUI port collision from reusing a fresh Mac
machine, a launcher/guest-verify port mismatch, a diagnostics host/guest port
mismatch, an IOS boot timeout too short for the actual image, an incorrect
Darwin-binary `--version` check, and the criterion-4 browser proof being
blocked entirely (no browser surface in that session).

### Found and fixed in the continuation session (via real rendered-browser testing)

Reached only because this session had actual browser tooling: an SSH tunnel
from Windows to the Mac's GUI port (`ssh -N -L 44101:127.0.0.1:4101`) driven
with a real browser control surface (DOM reads and in-page JS assertions, not
just an HTTP GET — the M5 plan explicitly calls out that "merely opening a
tab... is insufficient").

1. **Image Manager's bulk-replace "From" selector wrongly disabled the
   unsupported image.** The plan requires the unsupported image stay
   *selectable* in "From" (so a user can migrate nodes off it) and only be
   excluded from "To". `ImageManager.svelte` reused the identical
   `!imageSupport(img).supported` disabled condition for both `<select>`s.
   Confirmed live: both options showed `disabled: true` before the fix, only
   "To" after. Fixed by removing the disabled binding from "From" only.
2. **The per-node hover Start/Restart quick-actions never checked image
   support at all.** `nodes/NodeActions.svelte` is the one image-adjacent
   surface listed in the plan's exact file set that never called
   `labStore.nodeImageSupport()` — unlike the lab-level Start button and the
   canvas context menu, which both already did. Loading the exact synthetic
   i386 lab Luna's session created and hovering its node showed a plain,
   enabled "Start" button with no reason — a direct violation of the plan's
   "disable the hover Start/Restart affordance for an unsupported existing
   IOL node and expose the reason." Fixed by deriving the same
   `nodeImageSupport(node).reason` and using it to disable+retitle both
   buttons, mirroring the existing context-menu pattern.

Both fixes were rebuilt into a fresh embedded-GUI supervisor binary
(`GOOS=linux GOARCH=amd64`), redeployed onto the already-provisioned
`iolbox-m5-e2e` guest by stopping the service, swapping
`/opt/iolbox/supervisor`, and restarting, then reverified live in the browser
after each swap. Full offline suites (`go test ./...` ×2 packages, `go vet`,
`npm run check`, `npm run build`, `node --experimental-strip-types
tests/image-support.test.ts`) were rerun green after the final fix. No
server-side (Go) code was touched by either fix.

### A test-harness artifact, not a product defect

The exact synthetic `m5-unsupported-lab` lab document Luna's session created
via the raw NDJSON API was present on disk in `/opt/iolbox/labs/` but was
**silently dropped from the GUI's "Labs" picker** — the frontend's
`labListDocs()` deliberately skips any stored doc that fails to parse.
Root-caused to the hand-authored raw-API test fixture's YAML:
`startupConfig: |\n      \n` (a literal block scalar with an effectively
blank body) immediately followed by a mapping key at column 0, which
js-yaml's strict parser rejects as "bad indentation of a mapping entry."
Reproduced directly against the app's own `js-yaml` `load()`. This is not a
product bug: the frontend's own serializer (`labToYaml`/`dump`) never emits
that shape for an empty string, so a lab saved through the ordinary GUI Save
flow round-trips fine. Confirmed by rewriting the fixture's `startupConfig`
as `''` (what the frontend itself would emit): the lab immediately reappeared
in the Labs picker, loaded, and rendered its node. No code change was made
for this item. Future hardware sessions that hand-author lab YAML directly
via the raw API should use `''`/empty-string form for empty multiline fields,
not a literal block scalar, to stay parseable by the frontend.

---

## 3. Evidence

Luna's session evidence: `~/iolbox-m1/evidence-m5/m5-luna-20260815T0118/` and
`~/iolbox-m1/evidence-m5/m1-20260815T052417Z-43472/` on the Mac (per-criterion
files listed in the implementation report).

Continuation session evidence: `~/iolbox-m1/evidence-m5/continue-20260815/` on
the Mac —

- `continue-summary.txt` — full narrative of what was tested, found, and fixed
- `hello-after-fix.txt` — correlated hello after the final redeploy, confirms
  `i386` still omitted, `features`/`arch` unchanged by either GUI fix
- `palette-fixed.json`, `image-manager-fixed.json`, `node-actions-fixed.json`
  — live DOM/JS assertion dumps (`disabled`, `title`, `draggable` on the real
  rendered elements) captured through the browser control surface, before and
  after each fix
- `supervisor-linux-amd64-m5cont2` + `supervisor-sha256.txt` — the final
  redeployed binary and its hash

No pixel screenshot was captured (this session's browser control surface has
no visible/compositing pane to screenshot in this environment), but the DOM
assertions are stronger proof than a screenshot alone would be: they confirm
the actual `disabled`/`draggable`/`title` state of the live-rendered elements
via the running page's own JavaScript context, not just their appearance.

Criterion 2 evidence: `~/iolbox-m1/evidence-m5/criterion2-20260815/` on the
Mac —

- `hello-frames.ndjson` — every NDJSON frame from the session, including the
  correlated `m5-nonmac` hello (`features` includes `i386`, no
  `iolArchitectures`), an `image.register` of a synthetic ELF32/EM_386
  fixture correctly detected as `arch:i386`, and a `lab.load`/`node.start` of
  that image that reaches `starting`/`stopped` — never `image_arch_mismatch`
  — proving the arch-policy gate does not fire when `DisableI386` is unset
- `service-environment.txt` — `systemctl show ... -p Environment`, confirms
  no `IOLBOX_DISABLE_I386` was ever set on this target
- `host-uname.txt`, `supervisor-version.txt`, `payload-sha256.txt` — host
  identity and exact build/payload used
- `install.log`, `uninstall.log` — full `install.sh --bind local` /
  `uninstall.sh --yes` transcripts

---

## 4. Criterion 2 — closed against a real non-Mac amd64 target

Target: `noble-builder-vm`, static IP `192.168.226.10`, a real VMware-hosted
Ubuntu 24.04.4 LTS ("noble") x86_64 VM used as this project's PNetLab Noble
build box — unrelated to the Mac/Lima stack used for criteria 1/3/4/5, and
not itself an iolbox development machine. Owner-directed for this specific
check.

Procedure, matching plan §11 exactly: copied Luna's exact M5 payload
(`iolbox-server-m5-luna.tar.gz`, sha256
`fa9f919229c440f9ac97b54586a7bed6f8c0c4db4a26df7371f40d085d4838fb` — verified
before and after transfer) from the Mac, through Windows, to the builder VM;
ran `sudo ./install.sh --bind local` with **no** `IOLBOX_DISABLE_I386` set;
sent one correlated hello over the guest-loopback control socket; confirmed
`features` includes `i386` and no `iolArchitectures` field is present (the
implementation's negative-only signal — see §1's note); confirmed
`systemctl show ... -p Environment` carries no `IOLBOX_DISABLE_I386`. Went
one step further than the plan's minimum and registered a synthetic
ELF32/EM_386 test fixture (same shape as the Mac criterion-4 fixture, no
Cisco content): the server correctly classified it `arch:i386` and let a
`node.start` proceed to `starting`/`stopped` with no `image_arch_mismatch` —
the direct behavioral contrast to the Mac, where the identical shape of
image is rejected at that exact gate.

Cleanup: ran the payload's own `uninstall.sh --yes` immediately after
capturing evidence, confirmed `iolbox-supervisor.service` no longer exists
and `/opt/iolbox`/`/etc/iolbox` are gone, and removed the extracted tarball
and evidence copies from the VM's home directory. The builder VM was
returned to exactly its normal PNetLab-Noble-build-only state — nothing
iolbox-related was left installed or running on it.

---

## 5. Environment as it actually is now

| Item | Value |
|---|---|
| Host (Mac, criteria 1/3/4/5) | `rohansharma@192.168.101.166`, key `.m5-ssh/iolbox_mac_m0` |
| `iolbox-m5-e2e` Lima machine | **Stopped** (returned to that state at the end of the continuation session). Supervisor installed: `m5-cont-20260815b` (the final, both-fixes-applied build). GUI reachable at Mac-local `127.0.0.1:4101` when running. |
| `iolbox-m4-e2e` | Left exactly as found — **Running**, untouched by any M5 session (pre-existing state from an earlier session) |
| `iol22` | Untouched. Never touch. |
| Other Mac machines (`iolbox-m1-e2e`, `-m2-e2e`, `-m3-e2e`, `m1jammy`, `m1trixie`) | Untouched, Stopped |
| Host (Windows, criterion 2) | `noble-builder-vm`, `ubuntu@192.168.226.10` |
| `noble-builder-vm` | Returned to its pre-session state — no iolbox service, no `/opt/iolbox`/`/etc/iolbox`, home directory has only its normal PNetLab-build contents |

---

## 6. Open items going into M6

| # | Sev | Item |
|---|---|---|
| 1 | NOTE | M4 remains PARTIAL — reusing `iolbox-m4-e2e`/its images for other work does not imply M4's unexecuted items passed. |
| 2 | NOTE | If a future session hand-authors lab YAML directly via the raw NDJSON API (bypassing the GUI's own serializer), use `''` for empty multiline string fields, not a literal block scalar — see §2's test-harness-artifact note. |
| 3 | NOTE | The uncommitted worktree diff spans Luna's implementation and the continuation sessions' two Svelte fixes; no commit has been made anywhere. The owner should decide the commit/squash shape before merging toward M6. |
