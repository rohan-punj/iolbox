Implement and run **M5** of the Apple Silicon macOS track for iolbox, in the
worktree `J:\Claude code\iolab-m5-wt` on branch `luna/macos-m5-honest-caps`,
using luna at xhigh reasoning.

## Read first, in this exact order, before changing anything

1. `docs/macos-m5-plan.md` — the M5 plan (sol, medium).
2. `docs/macos-m5-plan-review.md` — the adversarial review of that plan. **It
   supersedes the plan wherever they disagree.** It found a BLOCKER (a missing
   GUI Start surface) and several MAJORs; all of its corrections are accepted
   except where the "Owner decisions" section below overrides.
3. `docs/macos-m5-prompt.md` — the immutable scope and the five observable
   acceptance criteria.
4. `docs/macos-m4-handoff.md`, then `docs/macos-m4-result.md`. **M4 is
   PARTIAL, not PASS.** Never describe it as complete.
5. `docs/macos-m3-handoff.md`, then `docs/macos-m1-handoff.md` — carried-forward
   gotchas that are still in force.
6. `docs/macos-arm64-plan.md` §M5 — immutable.

Precedence: owner decisions below > plan review > plan > M4 handoff > M4 result
> M3 handoff > M1 handoff > `macos-arm64-plan.md`.

## Owner decisions — these are settled, do not re-litigate

**D1 — `iolArchitectures` is CUT.** Review finding 3 wins. Do not add any new
hello field. The GUI's sole signal for 32-bit IOL support is
`hello.features.includes("i386")`, matching the existing `natgw` convention at
`app/src/lib/nodeCatalog.ts:54`. Every known older supervisor advertises
`i386`, so a new GUI against an old supervisor correctly keeps i386 enabled.
This also deletes the plan's entire §3 compatibility matrix — do not implement
it. `protocol.HelloResult` gains no new member.

**D2 — server-side i386 rejection STAYS, but hoisted.** Review finding 5 asked
whether it is in scope; the answer is yes, and it is deliberately a behavior
change *that can only fire when the switch is set*, so its non-Mac blast radius
is exactly zero. Rationale: without it, a Mac that has the honest hello and the
honest GUI still *attempts* an i386 spawn for any direct API client or stale
GUI, and fails deep in the launch path with an unrelated-looking error — the
same dishonesty relocated. Implement it per review findings 6 and 7:
- Validate the architecture of every targeted IOL image **before**
  `armDocCaptures` / `refreshFabric` / `prepareLabDir`
  (`supervisor/internal/server/handlers.go:568-598`), not inside `buildSpec`
  (`handlers.go:693-703`).
- Preserve the existing multi-node partial-failure shape. One bad image must
  produce a per-node failure entry, not a top-level request error.
- Document and test the exact semantics review finding 6 enumerates: a
  stopped i386 node rejects on Start; an already-running node is still returned
  as running by Start (it never reaches the spec path); Restart stops then
  rejects; `lab.load` of a doc referencing i386 still succeeds; autosave is
  untouched; same-topology adoption is unchanged.
- Only `image.ArchI386` is restricted. `unknown` stays allowed (plan §12.4).

**D3 — fail-open stands.** Absent signal means advertise `i386`. Review
finding 8 verified that both launcher `start` and `upgrade` run the full guest
sequence and atomically rewrite the drop-in, so the feared "old drop-in never
updated" hole does not exist through the launcher. Keep it that way and make
diagnostics flag drift rather than silently print a healthy capability policy.

**D4 — use a FRESH Lima machine `iolbox-m5-e2e`.** Review finding 11 is
correct that the M4 handoff never marked `iolbox-m4-e2e` reusable, and M4 is
PARTIAL. **Do not touch, stop, reconfigure, upgrade, or delete `iolbox-m4-e2e`
(currently *Running*), `iol22` (M0 evidence — forbidden), or any other existing
machine.** Do not delete anything to make room: the Mac has ~35 GiB free and
each machine costs ~3 GiB actual. A fresh machine also gives you a genuine
*create-path* proof of the new provisioning line, which reuse cannot.

**D5 — criterion 2 ("non-Mac targets still advertise i386") is capped at
PARTIAL by owner decision.** There is no reachable non-Mac amd64 Linux host
(no WSL distro, no Docker on the Windows host; no external target authorized).
The owner has explicitly accepted this proof instead:
 (a) a Go test pinning that a default/zero config still advertises `i386`; and
 (b) the **same M5 supervisor binary** started with the signal absent, and its
     hello captured, as a live process.
Do the (b) run inside the `iolbox-m5-e2e` guest — start a second supervisor on
a spare port with no `IOLBOX_DISABLE_I386` in its environment, capture a
correlated hello, and record it. **The result doc must state plainly that
criterion 2 was NOT proven on a genuine non-Mac deployment target** and must
list it as PARTIAL. Do not round this up. Do not attempt to install WSL or
Docker, and do not go looking for other machines on the network.

**D6 — the payload build path is already proven; do not redesign it.** Review
finding 14 is right that plan §10 is obsolete. The confirmed path is:
1. On Windows, `bash build-release.sh` in this worktree. It runs
   `npm run build:embed` then cross-compiles
   `supervisor/bin/supervisor-linux-amd64` with the GUI embedded, then restores
   the committed placeholder. `app/node_modules` is already installed (`npm ci`
   has been run). Node 26.7.0, npm 11.19.0, Go 1.26.4 are present.
2. `scp` that binary to the Mac.
3. On the **Mac** (not in the guest — review finding 14 established this is
   fine), extract a copy of
   `~/iolbox-m0/iolbox-server-v0.5.3-netprobe-cgroupfd-fix.tar.gz`, replace only
   `<top>/bin/supervisor`, `chmod 0755` it, and retar with
   `COPYFILE_DISABLE=1` to avoid AppleDouble members.
4. Name it unambiguously, e.g.
   `~/iolbox-m0/iolbox-server-m5-<short-id>.tar.gz`. **Never** name it
   v0.5.2/v0.5.3, and **never** let any harness default `--tarball` be used —
   see M4 handoff §2, which cost that session two false "regressions".
5. Prove the artifact before trusting it: `tar -tzvf` member+mode listing,
   guest `test -x` on `install.sh` and `bin/supervisor`, and
   `bin/supervisor --version` executed **in the Linux guest**, not on macOS.
A dry run of exactly this already produced
`~/iolbox-m0/iolbox-server-v0.5.4-m5-probe.tar.gz` (sha256
`804aab32718d8f072bc2c38e135a6ec18a83e79c011a2df25c56754e0d0da022`) from a
pre-change build. That probe is disposable; build your own from your actual
implementation and give it a distinct name.
Note: `runtime/pack-native.sh` **cannot** run on this Windows host — the `J:`
drive does not honor `chmod`, so its `-x` preflight on the vpcs binary always
fails. Do not try.

You also need a **darwin/arm64 launcher**: cross-compile it from Windows with
`cd tools/iolab-launcher && GOOS=darwin GOARCH=arm64 go build -o <out> .`, scp
it to the Mac, `chmod 0755`.

## Everything else: implement the plan as corrected by the review

Fold in every item in the review's "Corrections to fold into the plan" list
except #1's optional branch (D1 settles it: cut the field entirely) and #10
(D4 settles it: fresh machine). In particular, do not lose these:

- **Review finding 1 (BLOCKER)**: `app/src/lib/components/TopBar.svelte:57-62,
  293-296` has an always-enabled Start Lab button and `app/src/App.svelte:244-245`
  has a separate Start All disabled only by `running`. Both must be disabled,
  with the reason exposed, when the lab contains an unsupported-image IOL node.
  Derive one store-level lab-unsupported reason so every control uses identical
  logic and identical wording.
- **Review finding 9**: `execution=rosetta-amd64` must NOT depend on supervisor
  service/hello reachability. Derive it from live guest `uname -m == aarch64`,
  an enabled live Rosetta binfmt entry naming `/mnt/lima-rosetta/rosetta`, and a
  live passing amd64 loader canary. Report service/HTTP/hello/capability-policy
  as separate predicates. A crashed supervisor must be able to yield
  `execution=rosetta-amd64` together with `service=FAIL`.
- **Review finding 10**: parse `/var/lib/iolbox/macos-canary.json` with its real
  shape and its five-value verdict vocabulary (`PASS`, `FAIL_AUXV`,
  `FAIL_MISSING`, `FAIL_NOEXEC`, `FAIL_OTHER` —
  `packaging/macos/guest/30-canary.sh:47-60`). Never confuse its `verdict` with
  the structural attestation's `canary_verdict`
  (`40-install-payload.sh:134-150`). Host-mirrored values must be labelled "last
  attested", never presented as live.
- **Review finding 12 + 13**: keep the `ssh -L` tunnel for the browser proof
  (the M3 cookie/origin requirement does *not* break it — the browser's `Origin`
  and `Host` both stay `127.0.0.1:<local-port>`, and the session cookie is
  `Secure: r.TLS != nil` so it is set over plain http). Load `/` first to mint
  the cookie. Keep the router image
  `~/iolbox-m0/x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin` for the
  `hardware-m1.sh --phase install-image-and-lab` step; the `_l2-` file is the
  switch and is not interchangeable.
- **Review finding 15**: leave `docs/providers.md` and `docker/README.md` alone
  — their i386 statements are correctly scoped to non-Mac targets.
- Remove the hardcoded `execution=rosetta-amd64` / `guest_arch=aarch64` literal
  at `tools/iolab-launcher/macos_cli.go:276`. Add a test that drives mocked Lima
  responses and asserts on captured output — a source grep alone is not enough.

## Hardware verification — real hardware or it didn't happen

Mac: `rohansharma@192.168.101.166`. Key: **`.m5-ssh/iolbox_mac_m0` inside this
worktree** (gitignored). `~/.ssh/iolbox_mac_m0` is NOT readable from your
sandbox — M4 hit this and lost time to it; use the workspace-local copy.
`limactl` is at `/opt/homebrew/bin/limactl` and is **not** on the
non-interactive PATH, so always call it by absolute path or export the PATH.
macOS 26.6.1, arm64, 8 GB RAM, Lima 2.2.0, Go 1.26.6 present, **no node/npm**.

Prove each of the five acceptance criteria and record evidence under
`~/iolbox-m1/evidence-m5/<run-id>/` on the Mac:

1. Mac hello omits `i386` — correlated hello (match on request id; do not
   assume the first NDJSON frame is the reply), plus runtime `arch` still
   `x86_64` to prove the field's meaning was not altered.
2. Non-Mac default still advertises `i386` — per D5, PARTIAL, evidence as
   described there.
3. Diagnostics report measured `guest_arch=aarch64`, `execution=rosetta-amd64`,
   the real `uname -r`, and the canary verdict — captured alongside an
   independent ground-truth capture (`uname -m`, `uname -r`, raw binfmt, the
   canary JSON, the structural attestation, the effective drop-in) so the
   launcher's prose can be *compared*, not trusted. Also capture the
   machine-stopped state to exercise the unavailable/last-attested labels.
4. GUI never offers i386 — synthetic ELF32 fixture named
   `i86bi_m5_unsupported.bin` (valid ELF32/EM_386, >1 KiB; **never** a real
   Cisco binary), registered and confirmed `arch:i386`, then a **real rendered
   browser** over the `ssh -L` tunnel showing the palette entry disabled with a
   reason, Image Manager still listing it, both pickers disabling it, and a
   saved lab referencing it that cannot Start/Restart. Plus a direct `node.start`
   proving the server-side rejection.
5. x86_64 IOL lab unaffected — the real two-router
   `hardware-m1.sh --phase install-image-and-lab` run, both nodes running,
   its own IOS ping success threshold met.

If a step cannot be completed, record it as NOT RUN or PARTIAL with the reason.
**Do not round anything up.** M4's result doc exists precisely because an
earlier draft overstated what had actually been verified.

## Working rules

- **Never `git add -A` or `git add .`.** In fact: do **no** git operations at
  all. Your sandbox's `.git` is read-only and the owner commits from the main
  session with an explicit file list. Just leave the working tree in the state
  you want committed and tell the owner exactly which files you touched.
- Every `codex`-adjacent shell invocation you make that passes a prompt as an
  argument must redirect stdin from `/dev/null`, or it hangs forever.
- Do not edit: `runtime/build-rootfs.sh`, `runtime/fetch-vpcs.sh`, any M7 file,
  `runtime/files/iolbox-supervisor.service`, Docker files, `docs/providers.md`,
  `docker/README.md`, or any frozen M0-M4 plan/result/handoff document.
- Do not edit `docs/macos-m5-plan.md` or `docs/macos-m5-plan-review.md` — they
  are the record of what was decided going in.
- Run the offline suites before touching hardware: `cd supervisor && go test
  ./... && go vet ./...`; `cd tools/iolab-launcher && go test ./... && go vet
  ./...`; `cd app && npm run check && npm run build`; and the
  `packaging/macos/tests/` shell fixtures including `lint.sh`.
- Expect to find real defects on hardware. M2, M3 and M4 each found 6-11 that
  static review missed. Budget for several full iterations; a clean `go test`
  is not evidence.
- Do **not** write `docs/macos-m5-result.md` or `docs/macos-m5-handoff.md` —
  the owner writes those. Instead, when you are done, produce a factual
  implementation report at `docs/m5-session-logs/luna-implementation-report.md`
  with: the exact file list you changed, the offline test results verbatim,
  the hardware run IDs and evidence paths, a per-criterion PASS / PARTIAL /
  FAIL / NOT RUN table with the evidence path for each, and every defect you
  found and whether you fixed it and whether the fix was re-run on hardware.
