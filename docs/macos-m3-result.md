# M3 result — browser, sync, console, and capture UX: **PASS**

Executed 2026-08-14 on the same real hardware as M0/M1/M2. This is the M3
counterpart to `docs/macos-m2-result.md` and records only what was measured.

## Verdict

**PASS.** From a clean Mac state, the Darwin launcher's explicit port-forward
contract, host folder sync, and a browser-equivalent HTTP/WebSocket flow all
ran successfully against a real Lima guest — image upload/register, raw YAML
save/list, structured JSON load, a two-node lab start, two concurrent
consoles, a live capture producing a structurally valid non-empty pcapng,
stop, and reload. Host-side sync round-tripped at both the default path and
a difficult (space + non-ASCII) path. `upgrade` preserved all labs. The
Windows launcher build and tests remain unchanged and green.

```
macos_browser_e2e_darwin_test.go:217: pcapng path=.../M3 data café/captures/M3 link 0.pcapng bytes=1056 packets=4 capturePort=5500 sha256=3fa2dfb9...
macos_browser_e2e_darwin_test.go:235: browser-equivalent HTTP/WS flow passed for lab seed-2-routers (reload p0-two-routers-one-pc)
--- PASS: TestMacOSBrowserEquivalentE2E (51.45s)
PASS: M1 hardware acceptance evidence collected in .../m1-20260814T184421Z-25092
```

## Test bed

| Item | Value |
|---|---|
| Host | Same Mac, Apple Silicon, 8 GB RAM, macOS **26.6.1** (25G76) — `rohansharma@192.168.101.166` |
| Lima | **2.2.0**, `/opt/homebrew/bin/limactl` |
| Machine | `iolbox-m3-e2e` (disposable; never touched `iol22`) |
| Guest | Debian **13 trixie**, kernel **6.12.101+deb13-cloud-arm64** (unchanged) |
| Payload | `iolbox-server-v0.5.2.tar.gz` (same file M1/M2 used) |
| Image | `x86_64_crb_linux-adventerprisek9-ms.iol17.18.02.bin`, image id `b858503827356c55` |
| Launcher/test binaries | `GOOS=darwin GOARCH=arm64 go build`/`go test -c` at commit `4d25795`, scp'd to the Mac, never compiled there |
| Evidence | `~/iolbox-m1/evidence-m3/m3-20260814T184248Z-24776/` on the Mac |

## What was proven, on hardware

| Criterion | Result |
|---|---|
| Explicit Lima port-forward contract | `diagnose`/`status`: `GUI=127.0.0.1:4001 consoles=127.0.0.1:9000-9049 captures=127.0.0.1:5500-5529 guest-control=not-forwarded` |
| Full 81-port synthetic probe | all allowed ports reachable and loopback-only (`lsof` verified); LAN address and host `4000` both refused |
| GUI `/control` WebSocket session/Origin | reused for readiness/diagnose instead of guest port 4000; the raw control socket is no longer host-forwarded at all |
| D11 | `IOLBOX_HOST_LIMA=2.2.0`, never `unknown` |
| Host folder sync — default path | `~/Library/Application Support/iolbox/{images,labs}`: rescue-then-import on start, export before stop, verified via file listing |
| Host folder sync — difficult path | `M3 data café/{images,labs}` (space + non-ASCII): same rescue/import/export round-trip, plus image upload/registration with a fingerprint-cache reuse check |
| `packaging/macos/tests/hardware-m1.sh --phase install-image-and-lab` | **PASS** — two-node data plane **90% (9/10)**, matching the established baseline |
| Browser-equivalent HTTP/WS flow | image upload → `image.register` → `lab.saveDoc`/`lab.listDocs` (raw YAML) → `lab.load` (structured JSON, no warnings) → `lab.start` (both nodes, no failures) → two concurrent `/console/{id}` sessions reaching a real IOS prompt and returning command output → `capture.start` + direct `127.0.0.1:5500` dial + `/capture/0` binary stream → pcapng structurally validated (SHB/IDB/EPB, block-length symmetry, ≥1 packet) → `lab.stop` → reload, identity confirmed |
| `upgrade` in place | preserved all 8 labs (7 seed + the test's own), image cache reused |
| Final `stop` | `iolbox-m3-e2e` left **Stopped**, not deleted; `iol22`/`iolbox-m1-e2e`/`iolbox-m2-e2e` untouched |
| `GOOS=windows GOARCH=amd64 go build .` + existing Windows tests | unchanged and green |

## Defects found and fixed, all only surfaced by running against real Lima/hardware

None of these were caught by `go test ./...`, `go vet`, or sol's static
adversarial review — each needed an actual Lima guest and a real browser-
equivalent flow to reproduce.

1. **Lima `--set` yq expression syntax.** `limactl start --set='.portForwards=[{guestPort: 4001, ...}]'`
   (unquoted map keys) failed with `lexer: invalid input text`. Lima's
   embedded yqlib requires quoted keys (`{"guestPort": 4001, ...}`); confirmed
   by testing directly against a disposable stopped instance before fixing
   `macos_ports.go`.
2. **Missing GUI-bridge session cookie/Origin.** `wsbridge.go`'s
   `requireSession`/`sameOrigin` gate every `/control`, `/console/{id}`, and
   `/capture/{id}` WebSocket exactly like a real browser tab — a session
   cookie set on `GET /` plus a same-origin `Origin` header. Neither the Go
   launcher's `dialControlWS` nor the hardware harness's own raw WS probe
   sent either, so both got `401`. Fixed with a shared `wsDialWithSession`
   helper that fetches the cookie via one `GET /` first, exactly as a
   browser's cookie jar would.
3. **Console needs an active wake, not passive listening.** IOS does not
   print a fresh prompt on its own once boot completes; it prints one in
   response to input. `hardware-m1.sh`'s proven console driver already knew
   this (sends `\r\n` on connect and re-pokes while waiting) but the new
   Go E2E console-open logic did not — fixed to match.
4. **Frame-boundary substring matching.** Telnet echo often arrives one or
   two bytes per WebSocket frame; checking each frame's payload for a
   substring in isolation missed matches that straddled a frame boundary.
   Fixed to accumulate across reads before checking, as the prompt-detection
   code already did.
5. **`show version` pagination.** Without `terminal length 0` first, IOS
   prints `--More--` and blocks on further output until another keypress,
   which the pager swallows rather than echoing — stranding the console
   mid-page for whatever is sent next. Fixed by disabling pagination
   immediately after reaching a prompt, and by tightening the completion
   check to require a prompt specifically as the *last* line received
   (a bare "contains a prompt character anywhere" check falsely matched
   mid-banner or mid-page content).
6. **Lima capture-port forward propagation lag.** A freshly-armed
   `capture.start` port can be refused for a beat immediately afterward —
   Lima's dynamic per-port forwarding notices a new guest listener via its
   own polling loop, not instantly. Fixed with a short bounded retry on the
   direct-dial verification.
7. **256 MB per-node RAM silently wedges IOL 17.18.02.** The repository's
   `labs/example-p0.lab.json` fixture (used only by this harness) requested
   256 MB per node. A same-day upstream commit (`b470412`, on `main` via
   `fix/iol-ram-floor`, not yet in this branch's ancestry) had already
   diagnosed this exact failure mode on real hardware: IOL doesn't crash at
   256 MB, it wedges mid-init with `%SYS-2-MALLOCFAIL`, while `lab.start`
   still reports `ok:true`/`running` and nothing in the control protocol
   distinguishes it from a healthy boot. Fixed by aligning the fixture to
   1024 MB, matching the now-proven-correct M1 baseline, without pulling the
   unrelated upstream branch into this one.

## What was not exercised

- No dedicated Wireshark validation: the Mac has neither `capinfos` nor
  `tshark` installed, so the harness recorded that the in-process pcapng
  structural validator was authoritative (per plan) rather than
  cross-checking against a real Wireshark build.
- The optional `osascript` Safari/Chrome tab probe found no active GUI
  session on this run (`browser tab probe unavailable; deterministic
  evidence remains browser-equivalent HTTP/WS`), so literal Safari/Chrome
  click-driven automation was never exercised — by design, per the plan's
  stated preference for the deterministic HTTP/WS equivalent over unreliable
  unattended AppleEvent scripting.
- A live VPCS/"PC" node was not started as part of this harness (the fixture
  loads one but only nodes 0/1, both IOL, are started). Separately, live
  interactive use of the Mac during this session's testing surfaced that
  VPCS/"PC" nodes fail outright on this guest with "runtime does not
  support PC nodes" — root-caused to a missing `ioltool` system account and
  is unrelated to M3; tracked as a follow-up, not part of this result.
