# M2 result — Darwin launcher (`tools/iolab-launcher/`): **PASS**

Executed 2026-08-14 on the same real hardware as M1. This is the M2
counterpart to `docs/macos-m1-result.md` and records only what was measured.

## Verdict

**PASS.** `GOOS=darwin GOARCH=arm64 go build` succeeds, the pre-existing
Windows launcher build and tests are unchanged and green, and the Go binary
— cross-compiled on the dev box and shipped to the Mac, never built on the
Mac itself — drove a clean-state `start` to a passing canary, a `GET /` under
500, a two-node IOL lab, and survived both a launcher-driven `stop`/`start`
cycle and the M1 hardware harness's VM-internal restart. `upgrade` ran
in-place and preserved image cache, licence, and all 8 saved labs.

```
console LAST Success rate: Success rate is 90 percent (9/10), round-trip min/avg/max = 1/22/198 ms
ok: last console Success rate is 90% (>= 80%)
EXIT=0
```

## Test bed

| Item | Value |
|---|---|
| Host | Same MacBook Air, Apple M1, 8 GB RAM, macOS **26.6.1** (25G76) — `rohansharma@192.168.101.166` |
| Lima | **2.2.0**, `/opt/homebrew/bin/limactl` |
| Machine | `iolbox-m2-e2e` (disposable; never touched `iol22`) |
| Guest | Debian **13 trixie**, kernel **6.12.101+deb13-cloud-arm64** (unchanged from M1) |
| Payload | `iolbox-server-v0.5.2.tar.gz` (same file M1 used) |
| Image | `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`, image id `b858503827356c55` |
| Launcher binary | `GOOS=darwin GOARCH=arm64 go build .` at commit `63db4b7`, scp'd to the Mac, never compiled there |
| Evidence | `~/iolbox-m1/evidence-m2/m1-20260814T141743Z-14679/` (lab) and `.../m1-20260814T141904Z-14934/` (persistence), on the Mac |

Before this run, the Mac had ~71 MB free RAM with three leftover M1 machines
(`iolbox-m1-e2e`, `m1jammy`, `m1trixie`) running. All three were stopped
(never deleted) to make room; they were explicitly reusable/deletable per
the M2 task brief. `iol22` was not touched.

## What was proven, on hardware

| Criterion | Result |
|---|---|
| `status` on an absent machine | reports `not created`; `lima: 2.2.0 (/opt/homebrew/bin/limactl)` — **not** `unknown` (D11) |
| `start` from clean state | create → provision → canary → attestation → `GET /`; exit 0 |
| Qualification | exact-string `debian13 / 26.6.1 / 25G76` → `PASS (SUPPORTED)`, no numeric comparison |
| Live canary | `PASS`, recorded to `~/Library/Application Support/iolbox/lima-canary-iolbox-m2-e2e.txt` |
| Host structural-gate attestation | written to `~/.iolbox/macos/iolbox-m2-e2e-structural-gate.json`, mode 0600, schema/profile/product/build/Lima version/verdict all populated |
| Readiness | `GET /` → **200** |
| Control plane `hello` | NDJSON reply correlated by string id over `127.0.0.1:4000` |
| `diagnose` (D11 + fields) | `IOLBOX_HOST_LIMA=2.2.0`; `execution=rosetta-amd64` and `guest_arch=aarch64` reported **separately** from the untouched supervisor `arch=x86_64` field; Rosetta stale-bottle warning path present (none found on this run — Lima is current) |
| `packaging/macos/tests/hardware-m1.sh --phase install-image-and-lab` | **PASS** — image register/list, `lab.saveDoc`/`lab.listDocs`/`lab.load`/`lab.start`, both routers `running` |
| Two-node data plane | **90% (9/10), round-trip 1/22/198 ms** — matches M1's measured baseline |
| `hardware-m1.sh --phase persistence-check` (VM-internal restart) | **PASS** — supervisor active, control listener bound, `GET /` 200, hostid/iourc/image-cache/saved lab/journal gate evidence all intact |
| Launcher's own `stop` | machine → `Stopped`; host attestation and canary-record files still present afterward (never deleted) |
| Launcher's own `start` after `stop` | exit 0, `GET /` 200, all 8 saved labs (7 seeded + the harness's) still present via `lab.listDocs` |
| `upgrade` in place (same v0.5.2 payload) | exit 0, `GET /` 200, image cache and `/opt/iolbox/iourc` (licence) preserved, all 8 labs preserved |
| `GOOS=windows GOARCH=amd64 go build .` + existing Windows tests | unchanged and green, run on the Windows dev box |

Final state left on the Mac: `iolbox-m2-e2e` **stopped** (not deleted), all
other machines also stopped, matching the RAM state found before this run.

## Deviations from `M2-PLAN-SOL.md`

None in behavior. One implementation defect the plan's own review step (sol-
medium, codex) caught before hardware execution: `stageFiles` originally
`rm -rf`'d the live `/opt/iolbox-provision` before copying and validating its
replacement, which could strand the structural canary gate if a subsequent
`limactl copy` failed partway. Fixed at commit `63db4b7` to build and validate
a full replacement under a separate staging path first, then swap it in only
once `30-canary.sh` is confirmed present in the new tree — before any
hardware execution occurred, so the fix is exercised throughout the evidence
above, not just claimed.

## What was not exercised

- The Rosetta stale-bottle warning's actual detection/remediation text is
  unit-tested from log fixtures only; this Mac's Lima bottle is current, so
  the live path through `diagnose` never had a real warning to surface.
- `-c` codepaths for host `--limactl` / `--assets-dir` overrides pointing at
  paths *other than* the ones used here were not separately exercised on
  hardware (covered by unit tests only).
- Cold-vs-warm ping remeasurement (Q1 from the M1 handoff) was not repeated
  here; M2 was scoped to the launcher, not to re-litigating M1's data-plane
  numbers, and this run's single 90%/1-22-198ms sample is consistent with
  both of M1's prior samples.
