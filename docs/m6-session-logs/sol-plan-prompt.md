Plan **M6** of the Apple Silicon macOS track for iolbox, in
`J:\Claude code\iolab-m6-wt` (a git worktree on branch
`luna/macos-m6-followups`, branched from `luna/macos-m5-honest-caps` at
commit `cf19f2f`). Do not implement anything. Your deliverable is a written
plan file, `docs/macos-m6-plan.md`, plus a plain-text summary in your final
message.

## Read first, in this order

1. `docs/macos-m5-handoff.md` and `docs/macos-m5-result.md` — M5 (the
   immediately prior phase) is **PARTIAL**: criteria 1, 3, 4, 5 are proven on
   real Apple Silicon hardware; criterion 2 (a real ordinary amd64 non-Mac
   target still advertising `i386` with no signal) was never attempted in
   either M5 session — no amd64 Linux/CI target was available. This is an
   owner-supplied-target blocker, not a code gap, and it is **explicitly out
   of scope for M6** — do not fold it in, do not propose acquiring an amd64
   target as part of this plan.
2. `docs/macos-m4-handoff.md` and `docs/macos-m4-result.md` — M4 is also
   **PARTIAL** (items 1 and 6 proven on hardware; items 2 and 7 have
   unit-tested fixes never hardware-reconfirmed; items 3, 4, 5, 8 never
   attempted). This backlog is **also explicitly out of scope for M6** — note
   its existence in your plan's risk/assumptions section (M6 packages and
   ships what M1-M5 built, so an unresolved M4 defect could surface during
   M6's own clean-machine install pass, in which case root-cause and record
   it, but do not go hunt down M4's backlog proactively) and move on.
3. `docs/macos-arm64-plan.md` §M6 (search for "M6 — build, distribute, and
   qualify the unsigned one-download artifact"). **This is the canonical,
   immutable scope definition for M6** — plan exactly this, not a
   reinterpretation of it. Also read §M1-M5 immediately above it for context
   on what's already built, and §M7 immediately after it so you understand
   what M6 explicitly excludes (the Rosetta-independent arm64/FEX path is a
   separate, independent phase — do not let M6 depend on it or attempt any
   part of it).
4. `docs/macos-m3-handoff.md` and `docs/macos-m1-handoff.md` — earlier
   gotchas still in force (the shared-worktree/branch gotcha, `ram: 256`
   wedging IOL, console ring-buffer replay, liveness vs readiness, Lima yq
   quoting, WS session-cookie+Origin requirement, etc. — carry these forward
   into any hardware verification steps you plan).
5. `.github/workflows/ci.yml` and `.github/workflows/release.yml` as they
   exist right now on this branch — CI already cross-builds the Go
   supervisor for `linux/amd64` and tests it; `release.yml` already has a
   `build-windows` job (the actual shipped Windows deliverable is
   `tools/iolab-launcher`, a plain Go exe — the Tauri/Rust shell under
   `app/src-tauri` was abandoned and is not known to still build, do not
   resurrect it) plus five other target jobs (OVA, WSL rootfs, Proxmox LXC,
   native systemd, QEMU disk). There is **no macOS/Darwin job yet** — M6 adds
   one following the same pattern.
6. `docs/INSTALL.md` — the existing install doc's structure and tone (six
   deployable artifacts today, each with its own numbered section, a
   comparison table up top, a security note per section). M6 adds a seventh
   row/section for the Mac artifact in the same style, not a rewrite of the
   rest of the doc.
7. `tools/iolab-launcher/macos_*.go` (the M1-M5 Darwin launcher source) and
   `packaging/macos/` (the guest provisioning payload M1 built and M2-M5
   extended) — this is what M6 packages and distributes; skim enough to know
   what actually needs to end up in the release archive (launcher binary,
   native payload/provisioning scripts, locked Lima template/profile,
   checksums, third-party notices) without redesigning any of it.

Where sources disagree, precedence is: this prompt > `docs/macos-arm64-plan.md`
§M6 > M5 handoff/result > M4 handoff/result > M3/M1 handoffs.

## Context you must not re-litigate

M1-M5 are implemented and committed on branch `luna/macos-m5-honest-caps`
(this worktree's parent commit `cf19f2f`) — off `luna/macos-m4-runtime`, off
`luna/macos-m3-ux`, off `luna/macos-m1-provisioner`, off `main`. None of the
ancestor branches are merged to `main`. A Go Darwin launcher drives the whole
Lima VM lifecycle; the GUI is reachable through an explicit loopback-only
port-forward contract; host folder sync round-trips; a browser-equivalent
HTTP/WS flow proves the full lab lifecycle; VPCS/IOL bidirectional ping and a
sustained-traffic soak are proven on real Apple Silicon hardware; the Mac
build now honestly omits `i386` from its capability advertisement while an
x86_64 IOL lab remains unaffected. Do not redesign any of this — M6 is a
packaging/distribution/qualification phase, not a feature phase.

## Scope — plan exactly this (per `docs/macos-arm64-plan.md` §M6)

**Goal:** deliver the promised download with an honest prerequisite (Lima
must be installed by the user; iolbox does not manage installing Lima
itself, per M1) and a repeatable release process.

**Files to plan changes for:** `.github/workflows/ci.yml`,
`.github/workflows/release.yml`, packaging/release documentation,
`docs/INSTALL.md`, `docs/providers.md`, and `THIRD_PARTY.md` as applicable.
**No code-signing or notarization phase** — the artifact ships unsigned, and
the plan must say how first-run Gatekeeper quarantine is handled/documented,
not attempt to bypass or suppress it.

**Observable acceptance the plan must design toward** (all must be provable
on real hardware for the implementation session that follows this plan, not
just unit-tested — see M2-M5's own repeated experience of what static
review/CI-green alone misses):
- CI cross-builds and tests the launcher for `darwin/arm64`.
- The release pipeline produces `iolbox-macos-arm64.tar.gz` containing: the
  launcher binary, the exact native provisioning payload M1-M5 built, a
  locked Lima template/provisioner, checksums, and third-party notices.
- A clean supported Mac with only Lima installed can download that one
  archive and reach the GUI using one documented `iolbox` command.
- Both the browser-download path and a `curl` download path are checked for
  macOS quarantine attributes (`com.apple.quarantine` xattr), and the exact
  successful first-run procedure past Gatekeeper (right-click-Open, or
  `xattr -d`, or whatever the plan lands on) is documented and will be
  demonstrated, not assumed.
- Upgrade preserves images, labs, licence identity, and the pinned guest
  kernel (mirrors the in-place-upgrade path M1/M5 already proved for the
  supervisor binary alone — this is the full-artifact version of that).
- Uninstall clearly distinguishes plain launcher removal from destructive
  VM/data deletion.
- Release notes name Lima, macOS, the guest/kernel, "x86_64 IOL only" (per
  M5's honesty work — do not let the release notes imply i386 or
  arm64-native IOL support that doesn't exist), and the unsigned/Gatekeeper
  constraint.

**Dependencies:** M1-M5 (already done, this branch). Do not block M6's plan
on M4's unresolved backlog or M5's criterion 2 — both are explicitly
out-of-scope per above.

**Estimate per the master plan:** 1.5-2 focused days including one
clean-machine pass — but flag in your plan, based on M2-M5's own experience,
that every prior phase in this track ran longer than its estimate once real
hardware surfaced defects the estimate didn't anticipate; the plan should
budget for a genuine clean-machine verification pass, not treat CI-green as
sufficient.

## What the plan document must contain

Write `docs/macos-m6-plan.md` with (at minimum):
1. A concrete, ordered task breakdown (CI job additions, release.yml job
   additions, packaging script/manifest changes, doc changes) with the exact
   files each task touches.
2. The exact archive contents and directory layout for
   `iolbox-macos-arm64.tar.gz`.
3. A step-by-step clean-machine qualification procedure a hardware session
   can follow verbatim (download → quarantine check → first-run → GUI
   reachable → run a lab → upgrade path → uninstall path), referencing the
   Mac hardware access already documented in `docs/macos-m5-handoff.md` §5-6
   (SSH host `rohansharma@192.168.101.166`, key `.m5-ssh/iolbox_mac_m0`, the
   `limactl` full-path gotcha, the WS-reconnect-needs-a-reload gotcha).
4. Explicit acceptance criteria phrased so a later session can mark each one
   PASS/NOT RUN/FAIL on real hardware, matching the honesty bar M1-M5's own
   result docs already established (no rounding up "compiled" or
   "unit-tested" to "passed").
5. A risks/assumptions section that names: the M4 backlog and M5 criterion 2
   as known-adjacent-but-out-of-scope items; any assumption about what
   machine/account the clean-machine pass will actually run on (the existing
   Mac already has Lima machines on it from M1-M5 — the plan should say
   whether the clean-machine pass needs a genuinely fresh Lima install/fresh
   user account on that same hardware, or a different machine, and pick one
   with a stated reason); and anything about the unsigned/Gatekeeper flow
   that could plausibly not work as assumed.

## Working rules

- Sandbox is `workspace-write` — write the plan file for real, don't just
  describe it in your final message.
- Do not touch any file outside `docs/` in this pass — this is a planning
  pass, not an implementation pass.
- Keep the plan grounded in what's actually in this worktree right now (the
  CI/release workflows, the INSTALL doc, the packaging/macos tree) — read
  them, don't assume their contents from the M1-M5 docs' descriptions alone.
