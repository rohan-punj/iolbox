# P2 Go wire-up — replace the secbench Python/scapy path with the proven Go binaries

Status: dispatch plan. The 18 Go modules under `tools/secbench-attacks-go/cmd/<key>/` are already wire-verified against the Python/scapy scripts (see `docs/p2-go-port-plan.md`). This plan does **only** the plumbing that makes the shipped pack run those binaries instead of `python attacks/<key>.py`, and rips out the Python payload/venv/wheelhouse. No attack-logic work. Scope is 5 code/config files + a build-install step + doc-staleness notes — **one batch is genuinely enough**; the only reason for internal ordering is that the security-rail fix (`stripIfaceFlag`) and the manifest existence-check are load-bearing and must not be skipped.

## Facts established by reading the code (do not re-derive)

- **The GUI runner is the authoritative launcher, not `pack.json`.** `runtime/files/tools/packs/secbench/gui/runner.go:217` builds `argv := append([]string{venvPython, attacksDir + "/" + m.Script, "--iface", labIface}, extra...)`. `m.Script` comes from the **hardcoded Go table** in `gui/modules.go` (e.g. `modules.go:64` `Script: "arp_scan.py"`), NOT from `pack.json`. `attacksDir` = `/opt/iolbox/tools/packs/secbench/attacks` (`runner.go:16`), `venvPython` = `/opt/iolbox/tools/venv/bin/python` (`runner.go:15`).
- **`pack.json`'s `modules[].script` IS still consumed — for load-time validation only.** `supervisor/internal/tool/manifest.go:49` calls `manifestResolve(packRoot, module.Script)` for every module; `manifestResolve` → `contained()` (`supervisor/internal/tool/tool.go:518`) uses `filepath.EvalSymlinks` with a per-component `os.Lstat` fallback, so **the resolved script path MUST physically exist inside the pack root or `LoadPack` fails** (`manifest.go:51` returns an error, dropping the whole secbench pack from the palette). The resolved value lands in `Pack.Scripts` (`manifest.go:53`) but is **never read at runtime** (grep: only `manifest_test.go:46` references it, asserting empty for the module-less stub). Net consequence: **if we delete `attacks/*.py` but leave `script: "attacks/<key>.py"`, the pack disappears from the palette** (per sol-medium finding #8: `LoadPacks` aggregates the failure and its caller logs a load warning — not fully silent from the supervisor's perspective, but silent from the user's, since nothing in the GUI surfaces that warning). The script field must be repointed to a real installed file, and the live-VM gate (step 5 below) should inspect the supervisor's load-warning log directly rather than relying solely on palette presence/absence.
- **`interpreter` is purely declarative.** `supervisor/internal/tool/tool.go:118` is a plain struct field; no supervisor code branches on it (confirmed by grep). The `stub` pack uses `"interpreter": "none"` (`supervisor/internal/tool/testdata/packs/stub/pack.json:5`).
- **NEW / not in the brief — Go's `flag` package makes `stripIfaceFlag` incomplete, a real eth1-lock regression.** `BaseParser` uses `flag.NewFlagSet(...)` (`tools/secbench-attacks-go/internal/attackcommon/attackcommon.go:32`) with `fs.StringVar(&opts.Iface, "iface", ...)` (`:34`). Go's `flag` package treats **`-iface` (single dash) as identical to `--iface`**, and accepts `-iface=eth2`. `stripIfaceFlag` (`gui/runner.go:174`) only strips `--iface`, `--iface=`, and `-i` — it does **not** strip `-iface` or `-iface=`. Under Python argparse single-dash-long was never a valid form, so this was safe; under the Go binaries a Raw-args value of `-iface eth2` survives the strip and (last-occurrence-wins, same as argparse) overrides the hardcoded `--iface eth1`. This is the one security-relevant regression in the whole change and **requires a code fix**, not just a test (see Step 2).
- **`EnforceLabIface` is the backstop that keeps the above from being catastrophic.** `attackcommon.go:57` refuses any iface != `eth1`, so a smuggled `-iface eth2` currently makes the binary exit rather than attack eth2 — *provided every one of the 18 `cmd/<key>/main.go` calls it*. Step 5 requires auditing that; it does not excuse skipping the `stripIfaceFlag` fix (the reviewed posture is strip-and-explicit, and defense-in-depth must be restored).
- **GUI argv construction already matches the Go flags.** `gui/server.go:216` emits `--<field.name> <value>` (field names match the Go flags per the brief), and `server.go:227` always appends `--count <n> --interval <f>`; `--selftest` reaches a module only via the Raw-args field (`server.go:230-231`) — there is no dedicated selftest button. Go `BaseParser` registers `--count` (default 0), `--interval` (default 1.0), `--selftest` (`attackcommon.go:35-37`), so no GUI arg-building change is needed.
- **Nothing else on the appliance needs Python.** `find runtime/files -name '*.py'` outside `secbench/attacks/` = none. Only other pack is `stub` (`interpreter: none`, Go GUI). `settings.html`/`raw.html` mention `config_aaa.py`/`common.py` only as **template prose**, not executed. So dropping `python3,python3-scapy,python3-venv,libpcap0.8` from `BASE_INCLUDE` is safe. (`util-linux` stays — it's there for the supervisor's `setpriv`, unrelated.)

## Decisions locked (so a fresh agent doesn't re-litigate them)

1. **On-disk binary path convention: `/opt/iolbox/tools/packs/secbench/bin/<key>`** (new `bin/` dir, root:root 0755, one binary per key, no extension). Rationale: these are compiled binaries, not "attacks" Python scripts; a fresh dir lets `build-rootfs.sh` make a clean cut (delete the whole `attacks/` + venv block, add a `bin/` block) instead of repurposing a python-named dir that still contains `.py` files during the transition. Lower-churn alternative (reuse `attacks/<key>`) is explicitly rejected for clarity; do not use it.
2. **`ModuleDef.Script` becomes the bare key** (`"arp_scan"`, not `"arp_scan.py"`); `pack.json`'s `modules[].script` becomes `"bin/<key>"` (the real installed file, satisfies `contained()`). Both consumers thus point at `/opt/iolbox/tools/packs/secbench/bin/<key>`.
3. **`pack.json` `interpreter`: `"venv"` → `"none"`** (matches stub; declarative-only).
4. **Delete the Python sources** (`attacks/*.py`, `attacks/common.py`, `requirements.txt`) from `runtime/files/tools/packs/secbench/`. They are provably dead weight; the proven replacement lives at `tools/secbench-attacks-go/`; git history is the fallback. Keeping them re-introduces the `contained()` existence trap and rots. Do **not** keep them "as documented fallback."
5. **Build all 18 binaries with an inline `for cmd/*` loop in `build-rootfs.sh`** — no new Makefile/helper in the Go module. The `cmd/*` directory set is the single source of truth for the key list (no separate 18-name manifest to drift), and `build-rootfs.sh` already does per-component `go build`. A helper script would be a new file to maintain for zero benefit.

---

## Batch 1 (single batch) — the wiring

### Step 0 — precondition: audit the eth1 backstop BEFORE touching any wiring code (added after sol-medium review — was Step 5, moved earlier since it gates whether Steps 1-4 are even safe to do)
Files: `tools/secbench-attacks-go/cmd/<key>/main.go` (all 18)
- Confirm each `main.go` calls `attackcommon.EnforceLabIface(opts.Iface)` (or equivalent) and exits non-zero on refusal, and that a `BaseParser` parse error (`flag.ContinueOnError`, `attackcommon.go:32`) is treated as fatal by `main` (not swallowed into a run with defaults). This is a read-only verification of the already-proven modules — if any module is missing the call, STOP and flag it to the orchestrator rather than "fixing" attack code under this wiring plan's scope. Expectation (confirmed by sol-medium's review): all 18 already comply — record that confirmation as evidence before proceeding to Step 1.

### Step 1 — GUI launcher: exec the Go binary instead of venv Python
File: `runtime/files/tools/packs/secbench/gui/runner.go`
- Replace the `venvPython`/`attacksDir` consts (`:15-16`) with a single `const binDir = "/opt/iolbox/tools/packs/secbench/bin"`. Update the comment on `:14` (it says "baked into the image layout by the Dockerfile" — there is no Dockerfile; say the rootfs builder installs the binaries here).
- Change the argv build at `:217` to:
  `argv := append([]string{binDir + "/" + m.Script, "--iface", labIface}, extra...)`
  i.e. `argv[0]` is now the binary itself (absolute path), no interpreter prefix. `r.start()` (`:96`, `exec.Command(argv[0], argv[1:]...)`) needs no change.
- **Preserve the eth1-lock exactly**: keep `extra = stripIfaceFlag(extra)` at `:216` and keep passing `--iface eth1` explicitly. Do NOT lean on the Go binary's `iface` default (`attackcommon.go:34` defaults to eth1) as the sole enforcement — the reviewed posture is explicit-and-stripped.
- Update the SAFETY comment block (`:190-203`): it currently says "Python's argparse keeps the LAST occurrence" and references `attacks/common.py`. The last-occurrence property is still true for Go's `flag` package, so the reasoning holds — but reword to name the Go binaries and `internal/attackcommon.EnforceLabIface`, and note the single-dash-long form now handled by Step 2.

### Step 2 — Harden `stripIfaceFlag` for Go's flag grammar (SECURITY-CRITICAL)
File: `runtime/files/tools/packs/secbench/gui/runner.go:174-188`
- Add the single-dash-long forms Go's `flag` package accepts. The switch (`:178-186`) must strip:
  - two-token: `a == "--iface" || a == "-iface" || a == "-i"` → skip the value token (`i++`)
  - glued: `strings.HasPrefix(a, "--iface=") || strings.HasPrefix(a, "-iface=")` → drop this token only
- Keep stripping `-i` even though the Go binaries don't register it (harmless; preserves the reviewed superset). Update the function doc comment to state it strips both single- and double-dash `iface` forms because Go's `flag` package treats them identically (this is the delta from the argparse era).

### Step 3 — GUI module table cosmetics
File: `runtime/files/tools/packs/secbench/gui/modules.go`
- Change every `Script: "<key>.py"` to `Script: "<key>"` (18 entries, `:64`–`:329`).
- Update stale "python helper" wording: file header `:6`, `ModuleDef` doc `:31`, and the `Script` field comment `:37` ("filename under .../attacks/") → "binary name under .../bin/".
- `moduledefs_test.go` needs no change (it only key-matches against `../pack.json` and asserts 18; it does not check the `.py` suffix or file existence).

### Step 4 — `pack.json`
File: `runtime/files/tools/packs/secbench/pack.json`
- `"interpreter": "venv"` (`:6`) → `"interpreter": "none"`.
- Every `"script": "attacks/<key>.py"` (18 entries) → `"script": "bin/<key>"`. This keeps `manifestResolve`/`contained()` happy on the appliance once the binaries are installed at `bin/<key>` (Step 6), and keeps the `LoadPack` existence check meaningful.

### Step 6 — `build-rootfs.sh`: build+install the 18 binaries, remove the Python payload
File: `runtime/build-rootfs.sh`
- **`BASE_INCLUDE` (`:228`)**: remove `python3,python3-scapy,python3-venv,libpcap0.8`. Keep everything else (esp. `util-linux`, `passwd`). Update the package-rationale comment (`:219-221`) — delete the "python3 + python3-scapy (the tool-pack attack helpers)" line; the attack helpers are now static Go binaries with no runtime deps.
- **Preflight (`:150-153`)**: remove the `python3`/`pip` requirement check (only the wheelhouse needed it). Keep the `go` check (`:137`).
- **Wheelhouse block (`:144-159`)**: delete entirely (`SECBENCH_WHEELHOUSE`, `rm -rf`/`mkdir`, `pip download`).
- **Add a binaries build step** next to the existing `SECBENCH_GUI_BIN` build (`:169-174`). Define `SECBENCH_ATTACKS_SRC="$SCRIPT_DIR/../tools/secbench-attacks-go"` and build into a staging dir, mirroring the module's own module path and the GUI's `go test` gate:
  ```sh
  echo "== build-rootfs: building secbench attack binaries (linux/amd64) =="
  SECBENCH_BIN_STAGE="$BUILD_DIR/secbench-bin"
  rm -rf "$SECBENCH_BIN_STAGE"; mkdir -p "$SECBENCH_BIN_STAGE"
  (
      cd "$SECBENCH_ATTACKS_SRC"
      go vet ./...
      go test ./...
      for d in cmd/*/; do
          key="$(basename "$d")"
          GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
              -o "$SECBENCH_BIN_STAGE/$key" "./$d"
      done
  )
  ```
  (The `for cmd/*` loop is the source of truth for the 18 keys — no hardcoded list.)
- **Directory reservations (`:319-320`)**: delete the `attacks/` and `venv/` `install -d` lines; add `install -d -m 0755 -o root -g root "$ROOTFS_DIR/opt/iolbox/tools/packs/secbench/bin"`.
- **Payload install block (`:340-367`)**: keep the `pack.json` install (`:340-342`) and the `secbench-gui` install (`:346-347`). **Delete** the `attacks/*.py` install (`:343-345`), the wheelhouse dir + copy (`:348-349`), the `requirements.txt` install (`:350-352`), the `python3 -m venv` (`:353`), the `pip install` (`:354-359`), the scapy import verify (`:360-361`), the `compileall` (`:362-363`), and the trailing `rm -rf` wheelhouse/requirements/pip-cache cleanup (`:364-367`). Replace with a loop installing each staged binary root:root 0755, matching the `secbench-gui` treatment:
  ```sh
  for bin in "$SECBENCH_BIN_STAGE"/*; do
      install -m 0755 -o root -g root "$bin" \
          "$ROOTFS_DIR/opt/iolbox/tools/packs/secbench/bin/$(basename "$bin")"
  done
  ```

### Step 7 — delete dead Python sources + fix pack docs
- Delete `runtime/files/tools/packs/secbench/attacks/` (all 18 `.py` + `common.py`) and `runtime/files/tools/packs/secbench/requirements.txt`.
- `runtime/files/tools/packs/secbench/README.md:7` — remove the wheelhouse/venv paragraph; state the pack ships 18 static Go binaries under `bin/` built from `tools/secbench-attacks-go/`.
- `runtime/files/tools/packs/secbench/gui/templates/raw.html:10` — the "(see `attacks/common.py`)" aside; reword to reference the Go `EnforceLabIface` rail. Cosmetic; do not let it block.

### Step 8 — flag (do not rewrite) the now-stale P2 design docs
- `docs/learning-tools-nodes-plan.md` T2.2 (`:434-438`) and `docs/learning-tools-nodes-spec.md` §7 (`:867-882`, and the venv decision at `:277-306`) describe the wheelhouse/venv approach as the P2 design. Add a one-line **"SUPERSEDED by `docs/p2-go-wireup-plan.md` — the shipped pack now runs static Go binaries, no Python/venv/wheelhouse"** note at the head of each of those sections. Leave the bodies intact as the historical record of P2's original design; a full rewrite is out of scope and would erase context reviewers may want.
- **Per sol-medium finding #7, this scope is incomplete — also add the same one-line note to:** `docs/p2-dispatch-plan.md` (still describes the Python payload/requirements file as current) and `docs/p2-go-port-plan.md` (cites Python source file paths that Step 7 deletes — flag its post-deletion status as ambiguous without the note, since this very wire-up plan cites it as proof of the Go modules' correctness).

### Step 9 — flag (do not implement) CI coverage debt (added after sol-medium finding #4)
TODO follow-up: add CI coverage for `tools/secbench-attacks-go` and the secbench GUI (`go vet`, `go test`, and linux/amd64 cross-build); this batch intentionally leaves CI unchanged.
`.github/workflows/ci.yml` (if this repo has GitHub Actions CI — check before assuming) currently has no job exercising `tools/secbench-attacks-go` or the secbench pack GUI; `build-rootfs.sh` only tests them at release-build time, and this plan's acceptance gate only requires local evidence from the implementing agent, not persistent CI. Adding CI jobs (`go vet`/`go test`/cross-build for both directories) is explicitly OUT OF SCOPE for this batch — record it as follow-up debt (e.g. a one-line TODO note in this doc or a follow-up task) rather than silently leaving it unmentioned.

---

## Acceptance gate

`go build` / `go vet` / `go test` passing is **necessary but explicitly NOT sufficient.** The implementing agent produces the diff and the local evidence below; the **orchestrating session (which has VM access) performs the redeploy-and-run validation** — the plan does not attempt VM steps from the implementing agent.

Implementing agent must produce, locally:
1. `cd tools/secbench-attacks-go && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
2. `cd runtime/files/tools/packs/secbench/gui && go test ./...` — green (incl. `moduledefs_test.go` still matching the edited `pack.json`).
3. A **new unit test for `stripIfaceFlag`** in the `gui` package asserting all of these are stripped: `--iface eth2`, `-iface eth2`, `--iface=eth2`, `-iface=eth2`, `-i eth2`; and that benign args survive. This is a required deliverable, not optional — it is the regression guard for the Step 2 fix.
4. A **rootfs-equivalent local install proof**: run the Step 6 build+install loop against a throwaway staging dir (not a full debootstrap) and assert all 18 files land at `.../secbench/bin/<key>`, are mode 0755, and are executable ELF linux/amd64. This proves the binaries actually get installed in place of the Python path.
5. **Cross-source key/path contract test (added after sol-medium review, finding #3)**: `cmd/*` directory names, `pack.json` `modules[].key`/`.script`, and `gui/modules.go`'s `ModuleDef{Key,Script}` table can drift independently — the existing `moduledefs_test.go` only checks manifest-vs-GUI key sets, not `Script`/`script` values or the actual `cmd/` directory names. Add a test (in the `gui` package, or a small standalone script run in this same CI/local step) asserting exact equality across all four: `cmd/<key>` dir exists, `ModuleDef.Key == key && ModuleDef.Script == key`, `pack.json` module has `key` and `script == "bin/"+key`, and the staged/installed binary basename == `key`. This catches a substituted/duplicated/orphaned binary mapping that 18-files-exist alone would miss.

Orchestrator, on the appliance VM (reuse this session's WS-probe node/lab pattern and the `p2-go-port-plan.md` veth-rename-to-`eth1` technique):
5. Rebuild/redeploy the rootfs; confirm the secbench pack still `LoadPack`s — check BOTH the palette (shows it) AND the supervisor's own startup log for the absence of a pack-load warning (per finding #8, don't rely on palette presence alone; a warning there is the direct signal a `script` path is wrong even if some other path masked it from the palette).
6. `Supervisor.Start` end-to-end for **at least one** module against a real `eth1`-bearing node (through the actual pack GUI, not the standalone binary) — confirm it spawns and logs.
7. **`--selftest` smoke pass for all 18 through the ACTUAL pack GUI** (put `--selftest` in each module's Raw-args field so it flows through `server.go:230-231` → `Supervisor.Start`'s argv build). **Tightened per sol-medium finding #2**: `runner.go:121`'s exit-watcher only ever logs `[supervisor] exited` regardless of success/failure — spawn-and-exit alone is NOT sufficient evidence. For each of the 18, the pulled log tail must contain that module's own `PASS: selftest <key>` line (from `attackcommon.SelftestOK`) and no `FATAL`/flag-parse-error line. This is what proves argv construction is correct for every module's module-specific flags, which the standalone-binary tests do not exercise.
8. **eth1-lock live assertion (required, not optional, tightened per sol-medium finding #1 — a critical distinction):** with a second interface present on the node, set a module's Raw-args to `-iface eth2` (single dash) and to `--iface eth2`. The ONLY acceptable outcome is that the launched argv (visible in the module's log tail, which echoes `strings.Join(argv, " ")` per `runner.go`) shows `--iface eth1` and nothing else — i.e. `stripIfaceFlag` actually stripped the injected flag before the hardcoded one was appended. **An `EnforceLabIface` refusal mentioning eth2 is a FAILURE of this test, not a pass** — that would mean the strip failed and only the binary's own backstop caught it, which is exactly the GUI-layer regression Step 2 exists to fix. Separately confirm zero traffic ever appears on eth2 during the attempt.
9. Confirm the shipped image no longer contains `python3`/scapy/the venv (`/opt/iolbox/tools/venv` absent; `dpkg -l | grep -i python3` empty) — this proves **dependency removal**, which is the real payoff (reduced runtime complexity/attack surface). **Per sol-medium finding #5, do NOT claim this proves an image-size reduction** — 18 separately-linked static Go binaries each duplicate the Go runtime and may not net out smaller than the removed Python/scapy/venv payload; if a size claim is wanted, measure before/after rootfs and compressed-artifact size explicitly rather than assuming. Also do not expect `libpcap0.8` to be absent — it likely remains as a transitive dependency of the retained `tcpdump` package even after removal from `BASE_INCLUDE`'s explicit list.

Report a per-step verdict table to the user; call out explicitly that the eth1-lock was hardened (Step 2) rather than merely preserved, since Go's flag grammar differs from argparse.

---

## Out of scope (do not do)
- Any change to the Go modules' packet-building/attack logic (already wire-proven in `p2-go-port-plan.md`).
- New attack features, new CLI flags, or renaming existing flags.
- Re-testing packet correctness / re-running the `p2-go-port-plan.md` byte-diff gate.
- Rewriting the historical P2 design docs (Step 8 is a one-line supersession note only).

### Critical files for implementation
- J:\Claude code\iolab\runtime\files\tools\packs\secbench\gui\runner.go
- J:\Claude code\iolab\runtime\files\tools\packs\secbench\gui\modules.go
- J:\Claude code\iolab\runtime\files\tools\packs\secbench\pack.json
- J:\Claude code\iolab\runtime\build-rootfs.sh
- J:\Claude code\iolab\supervisor\internal\tool\manifest.go (read-only reference — the `contained()` existence check that forces `script: "bin/<key>"`)
