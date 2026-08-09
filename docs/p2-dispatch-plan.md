# P2 dispatch plan — scoped-agent execution of T2.1–T2.8

Status: **DISPATCH PLAN — REVISION 2.** Revision 1 was reviewed by `codex sol-medium`
and found to have 10 real defects (2 critical, 5 high, 2 medium) — a missing
proxy-route/WS-allowlist schema (B1 could not have implemented T2.5's allowlist at
all), an Origin-check algorithm that would 403 the appliance's own production
browser, a nonexistent stdlib tokenizer fallback, an under-specified security test
list, a traversal test that could pass without exercising the guard, an unassigned
iframe-sandbox attribute, missing rewriter edge cases, and a missing pack/GUI
module-key match test. All ten are resolved below (§0a). One finding (DTO
extension) is **not** taken — it re-opens ambiguity A1, which the owner already
resolved (§3, "Freeze as-is") for reasons that still hold; noted at that finding's
resolution instead of applied.
Source of truth for *what* to build: `docs/learning-tools-nodes-plan.md` §P2 (tasks
T2.1–T2.8 and its "Gate to P3"). Source of truth for *why*:
`docs/learning-tools-nodes-spec.md` §4.1 (secbench port), §6 (threat model), §7
(rootfs packaging). Neither is re-litigated here — every design decision in both is
final.

This document answers a different question: **how to hand T2.1–T2.8 to a small fleet
of scoped `codex exec luna-xhigh` agents (one job each, strictly file-scoped) with
the parallelism P2's actual shape warrants — no more.**

P2 is deliberately *not* a 17-batch fan-out like P1. It is **4 batches in 2 waves**.
Three reasons this is right, not lazy:

1. **P2 spans two toolchains and one high-stakes security task, but only ~8 tasks.**
   The security work (T2.4/T2.5/T2.6) is one tightly-coupled cluster in one Go
   package, not something to split across agents. The frontend (T2.7) is one agent's
   job regardless of how it is sliced. The packaging work (T2.1/T2.2) shares one file
   (`build-rootfs.sh`) and one payload tree, so it is one batch by ownership.
2. **Two of the eight tasks are largely already built by P1** and P2's real work on
   them is *finishing the test gap the gate names*, not implementing from scratch —
   see the "P1 already shipped most of T2.3 and T2.8" finding below. That collapses
   two would-be batches into one light verification batch.
3. **The owner's kickoff is explicit: "start simple, scale up only if asked … P1
   caught a 'why are we complicating this' course-correction when orchestration got
   ahead of what was asked."** So this plan keeps the wave/batch count proportionate
   and flags every judgment call for the owner rather than inflating ceremony.

---

## 0a. Revision 2 — sol-medium findings and resolutions

1. **(Critical, B1+B2) No proxy-route/WS allowlist schema exists.** Verified:
   `tool.Manifest`/`tool.GUI` (`supervisor/internal/tool/tool.go:110-130`) carry no
   proxy-route or WS-path allowlist field — B1 could not implement T2.5's "reject
   WS upgrade unless the pack manifest explicitly permits that path" requirement
   without inventing a schema, and B2 (writing `pack.json` concurrently) could drift
   from whatever B1 invented. **Fix:** `Manifest.GUI` gains a new field
   `ProxyRoutes []ProxyRoute` where `ProxyRoute{Prefix string, AllowWS bool}` — B1
   adds this to **`supervisor/internal/tool/tool.go`** (added to B1's OWNS list) and
   its manifest-validation logic; B2's `pack.json` must populate it (a single entry
   is enough for secbench: `{"prefix": "/", "allowWS": true}` unless the ported GUI
   uses a narrower WS path, which B2 must check against the ported `gui/main.go`).
   `ToolSocketPath` (D1) becomes `ToolProxyTarget(nodeID int) (socket string,
   routes []tool.ProxyRoute, ok bool)`.
2. **(Critical, B1) The Origin-allowlist algorithm as specified would reject the
   appliance's own browser in production.** Verified: the shipped unit binds
   `0.0.0.0:4001` (`runtime/files/iolbox-supervisor.service:36`); a real browser's
   `Origin` header carries the VM's actual address (e.g. `http://192.168.226.233:4001`),
   which can never string-equal a wildcard bind host. The plan doc's "derive from
   `-ws-addr`" text is corrected here: **the allowed origin is derived from the
   *inbound request's own* `Host` header** (normalized scheme+host, no
   `X-Forwarded-Host` trust — that header is stripped per T2.5 anyway) rather than
   a value precomputed from the listen address. This is a same-origin check, not an
   allowlist-of-one — it accepts any Origin/Referer that matches the Host the
   browser actually connected to, which is exactly "reject cross-site" without
   needing to know the VM's IP in advance. B1's OWNS list is unchanged (still just
   `wsbridge.go`/`proxy.go`); this only corrects the algorithm the doc pins.
3. **(High, B1) D2's stdlib fallback does not exist.** `html.NewTokenizer` is not a
   stdlib API (stdlib `html` only has `EscapeString`/`UnescapeString`); the real
   tokenizer is `golang.org/x/net/html`, not currently in `go.mod` (verified — the
   module has exactly one dependency, `github.com/creack/pty`). D2 is corrected:
   **use `golang.org/x/net/html`**, and B1's OWNS list gains
   `supervisor/go.mod` + `supervisor/go.sum` (the only dependency this batch may
   add — `go get golang.org/x/net/html` and nothing else).
4. **(High, B3) The T2.3 permission test cannot be *proven* by this plan's stated
   gate.** A `//go:build linux` uid-drop test compiles and vets under
   `GOOS=linux go build/vet` on the Windows dispatch box, but **does not run**
   there — `go vet` is static analysis, not execution. The dispatching session
   must additionally **run** `GOOS=linux go test ./internal/tool/...` as root on
   the real Linux VM/builder (same box as the P2 gate's manual e2e) before
   treating T2.3 as satisfied — added to §7's dispatching-session checklist and to
   "Gate to P3" below. Also corrected: the gate's package list now explicitly
   includes `internal/tool` (the restated gate previously named only
   wsbridge/server/protocol).
5. **(High, B1) The seven security tests under-cover the middleware boundary.**
   T2.5 requires the gate on `/console/*` and `/capture/*` too, Bearer-token
   equivalence, exact cookie attributes, and that the session cookie/forwarding
   headers are stripped before reaching the pack process — none of which the
   original seven tests observe. **B1's required test list is extended** (§6,
   items 8-11 added).
6. **(High, B1) The traversal test as described could pass without exercising the
   guard.** `http.ServeMux` itself canonicalizes `..` segments and 301-redirects
   before a handler ever runs; a test using a normal `http.Client` (which follows
   redirects) could observe 404 via ServeMux's own redirect, never touching B1's
   path-allowlist code. **Corrected test method** (§6, item 5): issue the raw
   request with redirect-following disabled (or write directly against the
   `http.Handler`), assert **no 3xx and no control-handler invocation**, and add a
   percent-encoded traversal variant (`%2e%2e`) checked via both `URL.Path` and
   `URL.RawPath`.
7. **(High, B4) The iframe `sandbox` attribute had no owning batch.** T2.5's
   `allow-same-origin` ruling is stated as a doc comment in B1's `proxy.go`, but
   B1 cannot touch frontend files and B4's original scope didn't pin the literal
   attribute value. **B4's OWNS entry for `ToolNode.svelte` now states the exact
   value**: `sandbox="allow-scripts allow-forms allow-same-origin"` — added to B4's
   acceptance criteria explicitly (§4 Wave 2, updated below).
8. **(Medium, B1) The rewriter test list was missing two pinned transfer-safety
   branches.** Added to B1's required tests (§6): a response that still carries
   `Content-Encoding` after `Accept-Encoding` removal (must pass through opaque,
   unrewritten) and a chunked `text/html` response (must be de-chunked, rewritten,
   and re-emitted with a corrected `Content-Length`, no stale `Transfer-Encoding`).
9. **(Medium, B2) The build-time manifest-to-GUI module-key match test was
   dropped from the batch instructions**, though T2.1 requires it. **Added to B2's
   OWNS/acceptance**: a test under `secbench/gui/**` comparing the GUI's
   `moduleDefs` keys against `pack.json`'s `modules[].key` set, wired into B2's own
   build gate before the pack is installed into the rootfs.
10. **(High, B3/B4 — noted, not applied.)** sol-medium flagged that A1 ("freeze the
    DTO") contradicts T2.8's literal text, which does describe `fields`/`mitigation`
    on the wire. This is correct as a reading of the plan doc, but the **owner
    already made this call** (§3 A1, confirmed "Freeze as-is (Recommended)"): the
    P2 gate's actual behaviour (drag → wire → console → attack → capture) does not
    depend on iolbox's own edit dialog rendering per-module fields, because
    secbench's `options: []` and its fields/mitigation drive **secbench's own**
    proxied htmx GUI inside the iframe, not `NodeEditDialog`. A1 stands as decided;
    B3 does not gain `verbs.go`/`toolpacks.go` ownership.

---

## 0. Findings from the current tree that shape this plan

Before decomposing, four facts verified against the checked-out `feat/learning-tool-nodes`:

- **F1 — T2.8's backend is ~90% already shipped in P1.** `internal/protocol/verbs.go`
  already defines `ToolListPacksArgs`/`ToolListPacksResult`/`ToolPackInfo`/
  `ToolModuleInfo` (lines 467–486); `internal/server/toolpacks.go` already implements
  `handleToolListPacks`, `toolPack(id)`, and `toolpacksLoad`; `server.go:203` already
  registers `s.disp.Handle("tool.listPacks", …)`; `toolpacks_test.go` already covers
  empty-array, metadata-mapping, lookup, and malformed-pack-tolerance. **What is
  genuinely missing for the gate:** a `tool.listPacks` **round-trip test through the
  dispatcher** (the existing tests call `handleToolListPacks` directly, not through
  `s.disp`), and a decision on whether `ToolModuleInfo`/`ToolPackInfo` must be
  *extended* to carry per-module `fields`/`mitigation` and pack-level `options` (see
  ambiguity A1). This is verification + a small possible additive extension, not a
  build.
- **F2 — T2.3's atomic-write + ownership code is already shipped in P1.**
  `endpoint_linux.go` already has `endpointPrepareSocketDir` (parent
  `root:root 0755`, dir `ioltool:ioltool 0700`) and `endpointWriteOptions`
  (temp-file in same dir → `Chown` `ioltool:ioltool` → `Chmod 0600` → `Sync` →
  `Rename`). **What is genuinely missing for the gate:** the required test that
  actually **binds the socket and opens `options.json` for read+write *as the
  `ioltool` uid*** (a uid-dropped subprocess), and asserts a non-`ioltool`/non-root
  uid is denied. `endpoint_test.go` today only checks the default-`{}` payload, all
  as root. This is a Linux-only integration test to add, not a rewrite.
- **F3 — `wsbridge` has no `/tool/*`, no auth, no Origin check today.** Confirmed
  `wsbridge.go`'s package doc (lines 24–34: "performs no Origin check … the trust
  boundary is the VM's own network exposure") and its mux (`New()`, lines ~134–150)
  registers `/control`, `/console/`, `/capture/`, `/api/upload/image`, `/` with zero
  authentication. T2.4/T2.5/T2.6 are genuinely new code. `Endpoint` exposes
  `State()`, `PID()`, `HostVeth()` but **not** its AF_UNIX socket path — the proxy
  needs a way to resolve `/run/iolbox/tool/<id>/gui.sock` (`endpointSocketName =
  "gui.sock"` is unexported). See deviation D1.
- **F4 — the frontend has no tool concept at all.** `labTypes.ts:6` is
  `NodeKind = "iol" | "vpcs" | "nat"`; there is no `ToolNode.svelte`; no
  `tool.listPacks` client in `supervisor.ts`/`protocol.ts`/`mockTransport.ts`; the
  exhaustive `kind` switches live in `Palette.svelte:9`, `CanvasInner.svelte:420/438/444`,
  `LabBrowser.svelte:65`, `icons.svelte.ts:201`, `interfaces.ts:12`,
  `mockTransport.ts:681`, `nodes/VpcsNode.svelte:40`. T2.7 is fully new work.

---

## 1. The three hazards this plan is built around

Same failure modes as P1, re-weighted for P2's actual surface:

1. **Same-file collision.** Two agents editing one file concurrently is a conflict,
   full stop. Ownership is per-*file*. No file appears in two concurrently-running
   batches. The two files most at risk of accidental co-ownership are
   `runtime/build-rootfs.sh` (T2.1 *and* T2.2 both edit it → same batch) and
   `internal/wsbridge/wsbridge.go` (T2.4 mux + T2.5 cookie/gate + T2.6 rewriter hook
   all edit it → same batch).
2. **Same-package symbol collision — exported *and* unexported.** Unlike P1, P2 does
   **not** put seven agents into one Go package. But two P2 batches (B1 and B3) both
   touch **package `tool`** and both touch **package `server`** — in *disjoint files*.
   That is exactly the P1-B01 hazard: git merges two files cleanly and `go build`
   explodes on a duplicate symbol. Mitigation (§4): B1 owns the production files in
   those packages and adds the one new exported accessor; B3 owns only *new test
   files* and prefixes every unexported test identifier `t23*` (in `tool`) / `t28*`
   (in `server`). Each reports its final symbol list for a pre-merge diff.
3. **Cross-platform compile split.** The dev/dispatch box is Windows.
   `go build ./...` there compiles only `_other.go`/non-`_linux` files, so a broken
   `_linux.go` is invisible locally. This matters for **exactly two** places in P2 —
   B1's `endpoint_linux.go` accessor and B3's `endpoint_linux_test.go` uid-drop test —
   and for the pack GUI binary (built `GOOS=linux`). Everything else in P2 (the whole
   wsbridge proxy/security/rewriter, the protocol/server verb, the Svelte frontend)
   is **portable Go or TypeScript with no build tags** — a welcome contrast to P1's
   heavy syscall work. Do **not** invent syscall/`_linux` hazards where none exist:
   the proxy is pure `net/http`, the rewriter is pure `golang.org/x/net/html`
   (already an indirect dep? verify — see D2), the security gate is pure cookie/Origin
   string logic.

There is **no** process-scope hazard analogous to P1's `PR_SET_CHILD_SUBREAPER`
subreaper problem in P2 — no batch touches the reaper, cgroup, or child-registry code.

---

## 2. Deviations from the plan doc's file map (deliberate, ownership-driven)

Only the places this dispatch differs from `learning-tools-nodes-plan.md`'s task
text. None change designed behaviour.

| # | Plan doc says | This dispatch says | Why |
|---|---|---|---|
| **D1** | T2.4 proxies "to the node's AF_UNIX socket" without pinning how the bridge learns the path | B1 adds one exported accessor `func (e *tool.Endpoint) SocketPath() string` (in `endpoint_linux.go` + `endpoint_other.go`) and one server method `func (s *Server) ToolSocketPath(nodeID int) (string, bool)` (new file `internal/server/toolproxy.go`) that the `wsbridge.ControlServer` interface gains | `endpointSocketName = "gui.sock"` is unexported; the proxy must resolve a *running* node's socket via the loaded-lab runtime (`nr.tool`), which only the server can reach. Deterministic recomputation from `tool.SocketDir` would still need the unexported filename. One accessor is cleaner than exporting the const |
| **D2** | T2.6 says "`golang.org/x/net/html`, or stdlib `html.NewTokenizer`" | **Use `golang.org/x/net/html`** if it is already in the module graph; otherwise **use stdlib `html`** — do **not** add a new direct dependency (module has exactly one: `github.com/creack/pty`). B1 must check `go list -m all` and state which it used | the shared-preamble no-new-deps rule (inherited from P1) forbids silently pulling `x/net`. `html.NewTokenizer` is stdlib and sufficient for attribute rewriting |
| **D3** | T2.8 lists protocol types / handler / dispatcher registration as work | those three **already exist** (finding F1); B3's T2.8 job is the **missing round-trip test** plus the additive DTO extension **iff** ambiguity A1 is resolved "extend" | correcting the task to the real remaining gap; do not re-create files P1 shipped |
| **D4** | T2.1 doesn't pin where the ported secbench GUI *source* lives in-repo | pack payload + GUI source live under `runtime/files/tools/packs/secbench/` (mirroring the existing `runtime/files/tools/packs/stub/` tree); the GUI is its own `package main` built `GOOS=linux` by `build-rootfs.sh`, installed beside `pack.json` exactly as the stub is (`build-rootfs.sh:320-323`) | matches the shipped stub-pack layout; keeps the pack GUI a standalone module with no symbol contact with the supervisor |

---

## 3. Ambiguities in the plan doc that forced a judgment call

Flagged for the owner (per MEMORY `drift-findings-need-confirmation`: present + ask,
do not silently decide product behaviour).

- **A1 — does `tool.listPacks`' wire DTO need per-module `fields`/`mitigation` and
  pack-level `options`?** Plan-doc T2.8 says the result carries "per-module
  `fields`/`mitigation` metadata the palette + edit dialog render," but the DTO P1
  actually shipped (`ToolModuleInfo{Key,Label,Group}`, `ToolPackInfo` with no
  `Options`) does **not**. For the **P2 gate specifically** this does not matter:
  secbench's `pack.json` has `options: []` (Victim Mode dropped, spec §4.1), and the
  fields/mitigation drive **secbench's own proxied htmx GUI**, which the browser
  renders inside the `/tool/<id>/` iframe — *not* iolbox's `NodeEditDialog`. So the
  drag→wire→console→ARP→capture gate passes with the existing DTO. **Judgment call:**
  treat the DTO as **already frozen**; B3 adds only the round-trip test, and any
  `fields`/`mitigation`/`options` enrichment is an **additive, optional** extension
  that B3 may include but that B4-FRONTEND does not depend on. *Owner: confirm the
  edit dialog is meant to show only Name + pack for secbench (options empty), not a
  per-module field editor — if the latter, A1 flips to "extend" and B3 must land the
  DTO change before B4 starts.*
- **A2 — is T2.6's rewriter needed at the gate, or is secbench already relative-URL
  clean?** The plan pins the rewriter as "the mechanism that makes 'only the AF_UNIX
  change' true" and requires a rewriter test at the gate, so it is **in scope
  regardless**. But whether secbench's htmx actually emits root-absolute URLs that
  break under `/tool/<id>/` is an empirical question answered only on the VM.
  **Judgment call:** B1 implements the rewriter to the pinned spec (tokenizer, bounded
  `text/html`, `<base>` neutralization, `Location:` rewrite) and ships the unit test;
  whether real secbench pages *need* it is a wave-2 e2e observation, not a batch
  blocker.
- **A3 — frontend-parallel-with-backend: safe or not?** The kickoff explicitly asks.
  **Judgment call: stagger, don't parallelize** (B4 in wave 2). Reasoning in §5.

---

## 4. Batch table (4 batches)

`OWNS` = the exclusive, exhaustive list of files that batch may create or modify.
Anything not listed is read-only for that agent.

### Wave 1 — 3 agents (all independent; disjoint files)

**B1 — proxy + security boundary + rewriter (T2.4 + T2.5 + T2.6).**
The highest-stakes batch in the phase; treat as security-review-grade.
Bundled because all three tasks edit the *same* `wsbridge.go` mux/`Bridge` struct and
the *same* `wsbridge_test.go`, and because the security gate (T2.5) must wrap the
proxy route (T2.4) the moment it exists — they cannot land separately without a
same-file collision or a window where a proxied pack page is served with no gate.
OWNS, exhaustively:
- `supervisor/internal/wsbridge/wsbridge.go` (mux: add `/tool/`; `Bridge` gains the
  boot `sessionToken`; the `/` catch-all sets the `iolbox_session` cookie; wrap
  `/control`,`/console/`,`/capture/`,`/tool/*` in the session+Origin gate)
- `supervisor/internal/wsbridge/proxy.go` (**new** — HTTP/WS reverse proxy to the
  node's AF_UNIX socket; the path-allowlist + `path.Clean` traversal guard; the
  URL rewriter, or split the rewriter into…)
- `supervisor/internal/wsbridge/rewrite.go` (**new, optional** — the T2.6 tokenizer
  rewriter, if B1 prefers it out of `proxy.go`; still same batch, so no collision)
- `supervisor/internal/wsbridge/wsbridge_test.go` (all seven T2.5 security tests +
  the T2.6 rewriter test — see acceptance below)
- `supervisor/internal/tool/endpoint_linux.go` + `supervisor/internal/tool/endpoint_other.go`
  (add the one exported `SocketPath()` accessor per D1 — **and nothing else**)
- `supervisor/internal/tool/tool.go` (**revision 2, finding 1** — add
  `ProxyRoute{Prefix string, AllowWS bool}` and `Manifest.GUI.ProxyRoutes
  []ProxyRoute`, plus manifest-validation for the new field; this is additive to
  the frozen P1 contract, not a redefinition — no other batch touches this file)
- `supervisor/go.mod` + `supervisor/go.sum` (**revision 2, finding 3** — the *only*
  dependency this batch may add: `go get golang.org/x/net/html`, nothing else)
- `supervisor/internal/server/toolproxy.go` (**new** — `func (s *Server)
  ToolProxyTarget(nodeID int) (socket string, routes []tool.ProxyRoute, ok bool)`
  per revision-2 finding 1, reading `nr.tool.SocketPath()` + the loaded manifest's
  `GUI.ProxyRoutes`; this is the concrete impl of the new `ControlServer` interface
  method)
Depends on: nothing new (F3). **Private prefix `toolproxy*`** for any unexported
identifier it adds in package `server`; in package `tool` it adds only the exported
`SocketPath` method and the `ProxyRoute` type (no unexported helpers — if it needs
one, prefix `toolsock*` and report it). **Build-tag discipline:** it touches
`endpoint_linux.go` → must pass `GOOS=linux go build ./...` and
`GOOS=linux go vet ./...`; `tool.go`, the wsbridge/proxy/rewriter code, and the new
`x/net/html` dependency are all portable and compile natively on Windows.

**SUPERSEDED by `docs/p2-go-wireup-plan.md` — the shipped pack now runs static Go binaries, no Python/venv/wheelhouse.**

**B2 — secbench pack port + offline wheelhouse (T2.1 + T2.2).**
Bundled because both edit `runtime/build-rootfs.sh` and both are "get the pack + its
Python runtime into the rootfs." Different toolchain from the Go batches (bash +
Python + a standalone Go GUI module) — a distinct agent skillset, safely concurrent
because it shares no file with any other batch.
OWNS, exhaustively:
- `runtime/files/tools/packs/secbench/pack.json` (**new** — 6 groups
  `recon`/`spoof`/`dhcp`/`stp`/`vlan`/`fhrp`, 18 L2/L3 modules; no `ngfw`, no
  `victim`; `gui.health` set; JSON tags matching `tool.Manifest` per spec §2.6 /
  §4.1; **revision 2, finding 1** — populate the new `gui.proxyRoutes` field B1
  adds to `tool.Manifest` — verify the ported GUI's actual WS path against
  `gui/main.go` before finalizing the entry, do not assume `/`)
- `runtime/files/tools/packs/secbench/attacks/*.py` (**new** — the 18 kept
  `attacks/*.py` + `common.py`, ported from
  `J:\Claude code\pnet-lab-nodes\nodes\secbench\attacks/`; the 10 `fw_*.py` +
  `fw_reach.py` are **not** ported, spec §4.1.1)
- `runtime/files/tools/packs/secbench/gui/**` (**new** — secbench `gui/` ported: the
  only code change is AF_UNIX listen (`net.Listen("unix", …)` replacing `:80`,
  `gui/main.go:33/71`) and the `moduleDefs`/htmx tabs trimmed to the 18 kept modules;
  drop `panorama.go`/`scenarios.go`/`victim.go` and the `ngfw` dead code per §4.1.2;
  keep the three interface locks as defense-in-depth)
- `runtime/build-rootfs.sh` (T2.2 wheelhouse: `pip download` scapy+pinned deps to
  `wheelhouse/` with `--require-hashes`; `venv` + `pip install --no-index
  --find-links`; import-verify `scapy` + `scapy.contrib.{cdp,lldp,dtp,ospf,eigrp}`;
  `py_compile`; delete wheelhouse+caches. **T2.1 install:** build the secbench GUI
  `GOOS=linux` and install the pack tree beside the stub, mirroring lines 297–323;
  the `BASE_INCLUDE` already carries `python3 python3-venv libpcap0.8 util-linux` and
  the `ioltool` account already exists — verify, extend only if a dep is missing)
- `runtime/files/tools/packs/secbench/requirements.txt` (**new** — pinned,
  hash-locked wheel list + recorded SBOM/hashes, spec §7)
- `runtime/files/tools/packs/secbench/README.md` (**new, optional** — pack-authoring
  note; the "JS-generated URLs unsupported in v1" constraint is stated authoritatively
  in `proxy.go` by B1 and may be cross-referenced here)
- `runtime/files/tools/packs/secbench/gui/moduledefs_test.go` (**new, revision 2
  finding 9** — compares the GUI's `moduleDefs` keys against `../pack.json`'s
  `modules[].key` set, one-to-one; wired into B2's own build gate, run before the
  pack is installed into the rootfs)
Depends on: nothing (the `tool.Manifest` struct tags it must match are frozen from
P1). **Build-tag discipline:** the pack GUI is built `GOOS=linux GOARCH=amd64`; its
build must be gated in `build-rootfs.sh` exactly as the existing helper binaries are
(`build-rootfs.sh:146-149`). No supervisor-package symbol contact (own `package main`).
**No `GOOS=linux go vet ./...` of the supervisor is affected by this batch.**

**B3 — backend test completion (T2.3 uid-drop test + T2.8 round-trip test).**
Light verification batch (findings F1, F2). Spans two Go packages in *new test files
only*, so it never collides with B1's production edits in those same packages.
OWNS, exhaustively:
- `supervisor/internal/tool/endpoint_linux_test.go` (**new, `//go:build linux`** —
  the T2.3 test: a uid-dropped subprocess **as `ioltool`** binds the socket in
  `/run/iolbox/tool/<id>/` *and* opens `options.json` for read+write and succeeds;
  asserts a non-`ioltool`, non-root uid is denied by the `0700`/`0600` modes. `t.Skip`
  cleanly when not root or not linux)
- `supervisor/internal/server/toolpacks_roundtrip_test.go` (**new** — drives
  `tool.listPacks` through the real dispatcher `s.disp` end-to-end, asserting the
  wire JSON shape, not by calling `handleToolListPacks` directly)
Depends on: nothing (code under test already exists). A1 is settled ("Freeze
as-is" — owner-confirmed; §0a finding 10); B3 does **not** gain `verbs.go`/
`toolpacks.go` ownership. **Private prefixes** `t23*` (package `tool`) and `t28*`
(package `server`) for every unexported test identifier; report the list.
**Build-tag discipline:** the T2.3 file is `//go:build linux` → `GOOS=linux go vet
./internal/tool/...` on the Windows dispatch box only proves it **compiles and
vets**, not that it passes. **Revision 2, finding 4:** the dispatching session
must additionally run `GOOS=linux go test ./internal/tool/... -run TestEndpoint`
(or equivalent) **as root on the real Linux VM/builder** before treating T2.3 as
satisfied — this cannot happen on the Windows dispatch box at all, since the uid-
drop behavior under test does not exist there.

### Wave 2 — 1 agent

**B4 — frontend slice (T2.7).**
OWNS, exhaustively:
- `app/src/lib/labTypes.ts` (`NodeKind += "tool"`)
- `app/src/lib/nodes/ToolNode.svelte` (**new** — status LED + name chip; panel opens
  the proxied GUI iframe at `/tool/${nodeId}/`; **revision 2, finding 7 — pinned
  literal value**: `<iframe src="/tool/{nodeId}/"
  sandbox="allow-scripts allow-forms allow-same-origin">`, per T2.5's ruling that
  `allow-same-origin` is kept because the pack is trusted first-party code — this
  exact attribute string is a required acceptance check, not left to agent judgment)
- `app/src/lib/components/Palette.svelte` (tool drag payload + icon)
- `app/src/lib/components/NodeEditDialog.svelte` (Name + pack + manifest `options`;
  per A1, options is empty for secbench)
- `app/src/lib/components/Console.svelte` (tool-panel tab hosting the `/tool/<id>/`
  iframe, alongside the existing console/capture tabs)
- `app/src/lib/components/CanvasInner.svelte` (register the `tool` node type; the
  `kind` union at :420/:438/:444)
- `app/src/lib/components/LabBrowser.svelte` (`kind` switch :65)
- `app/src/lib/components/InterfacePicker.svelte` (tool → single `eth1`)
- `app/src/lib/icons.svelte.ts` (:201 tool icon)
- `app/src/lib/interfaces.ts` (:12 tool → `["eth1"]`)
- `app/src/lib/nodes/VpcsNode.svelte` (:40 egress-warn guard only if the `kind`
  narrowing needs it — verify)
- `app/src/lib/protocol.ts` (`tool.listPacks` client types + `ToolPackInfo`/
  `ToolModuleInfo` matching `verbs.go` **as landed in wave 1**)
- `app/src/lib/supervisor.ts` (the `listPacks()` call transport, mirroring the other
  verb methods)
- `app/src/lib/labStore.svelte.ts` (tool-node state + pack-list state + error
  handling)
- `app/src/lib/mockTransport.ts` (:681 kind guard + a mock `tool.listPacks` response
  so the frontend runs without a live supervisor)
Depends on: **B1 (the `/tool/<id>/` proxy path exists and is gated) and B3 (the
`tool.listPacks` wire shape is final)** — see §5 for why this is wave 2, not wave 1.
No Go build tags. **Toolchain gates:** `npm run check` (svelte-check / tsc) and
`npm run build` from `app/` must pass; per MEMORY `pnetlab-gui-no-gate` a *simple*
GUI change needs no heavyweight gate, so the verification here is the type-check +
the wave-2 manual e2e, not a puppeteer harness.

---

## 5. Wave structure and the frontend-parallelism decision

| Wave | Batches | Concurrent agents | Gate before next wave |
|---|---|---|---|
| 1 | B1, B2, B3 | **3** | `go build ./... && go vet ./... && go test ./...` in `supervisor/`; **`GOOS=linux go build ./... && GOOS=linux go vet ./...`** (B1's `endpoint_linux.go` + B3's `endpoint_linux_test.go` compile only here); B2's `build-rootfs.sh` passes `bash -n`/`shellcheck` and the pack GUI builds `GOOS=linux`; the **private-symbol diff** across B1/B3's reported lists in packages `tool` and `server`; every B1 security test green |
| 2 | B4 | **1** | `npm run check && npm run build` in `app/`; then the manual e2e (agent-browser, per MEMORY `agent-browser-gui-checks`) |

**Why B4 is wave 2, not concurrent with wave 1 (ambiguity A3, kickoff's explicit
question):** The kickoff says frontend "can still be written against the documented
contract in parallel if you judge that safe." I judge **staggering is the better
call**, for three concrete reasons, none of them default assumption:

1. **The only cross-toolchain contract is runtime, not compile.** B4's `protocol.ts`
   is hand-written TypeScript matched to the *wire JSON* of `tool.listPacks` — it is
   not generated from Go, so a Go/TS mismatch surfaces at **runtime**, not at
   `go build`. If A1 flips to "extend" (B3 changes the DTO), a frontend written
   concurrently against the old shape silently mis-renders. Landing B3's final DTO
   first removes that entire failure class for the cost of one agent's serialization.
2. **B4's whole payoff is pointing an iframe at a proxy that exists.** With B1 landed,
   the wave-2 agent can immediately smoke `/tool/<id>/` in agent-browser; written
   concurrently it can only code blind against a path string. This mirrors P1's B17
   discipline (write against *landed, integrated* behaviour, not against the plan's
   description of it).
3. **It costs almost nothing.** B4 is one agent either way — staggering it adds one
   wave boundary, not parallel-agent throughput, so there is no speed argument for
   forcing it into wave 1.

If the owner wants the frontend moving sooner, the safe compromise is: B4 **may**
begin in wave 1 **restricted to the parts with zero backend coupling** — `labTypes.ts`
+ the exhaustive-switch fixups + `ToolNode.svelte`'s static shell — deferring only
`protocol.ts`/`supervisor.ts`/`labStore` (the `tool.listPacks` client) and the iframe
`src` wiring until B1/B3 land. That split is offered, not recommended; ask before
taking it.

**Dispatching discipline:** the dispatching session lands and commits wave 1 (and runs
the `GOOS=linux` build on both platforms, the private-symbol diff, and B1's security
suite) **before** opening wave 2. Within wave 1, the three agents run truly
concurrently and never see each other's output; the only shared-package contact
(B1 vs B3 in packages `tool` and `server`) is defused by file-disjointness +
prefixed test symbols.

### Dependency graph

```
wave 1   B1  → (nothing new)      [wsbridge proxy/security/rewriter + tool.SocketPath + tool.ProxyRoute + server.ToolProxyTarget + x/net/html dep]
         B2  → (nothing)          [secbench pack + wheelhouse; own package main GUI]
         B3  → (nothing)          [T2.3 uid test + T2.8 round-trip test; new test files only]

wave 2   B4  → B1 (proxy path), B3 (final tool.listPacks DTO)
```

### Explicitly not parallel-safe (claims to reject)

- **T2.4 ∥ T2.5** — both edit `wsbridge.go`'s mux and `Bridge` struct and
  `wsbridge_test.go`; and a proxy route without its gate is the exact CSRF-to-attack
  window the review forbade. Merged into B1.
- **T2.5 ∥ T2.6** — both edit `wsbridge.go` (gate wrap) / the proxy response path.
  Same batch.
- **T2.1 ∥ T2.2** — both edit `runtime/build-rootfs.sh`. Merged into B2.
- **B1 ∥ B3 in the same *file*** — never; B1 owns the production files in packages
  `tool`/`server`, B3 owns only new test files there. Safe *only* with the
  private-prefix rule (hazard 2); without it, a generic `newTestServer`/`fakePack`
  helper in each collides with no git conflict.
- **B4 ∥ wave 1 in full** — rejected (A3 above); the `tool.listPacks` client couples
  at runtime to B3's final DTO and the iframe couples to B1's landed proxy.

---

## 6. B1 acceptance criteria — the T2.5 security tests (plan-doc's seven, plus
revision-2 extensions from sol-medium findings 2/5/6/8)

All in `supervisor/internal/wsbridge/wsbridge_test.go`; the gate requires every one:

1. `Origin: http://evil.example` **with** a valid `iolbox_session` cookie to
   `/tool/7/attacks/arp_spoof` → **403**.
2. **No** `iolbox_session` cookie and no `Authorization: Bearer` → `GET /tool/7/…`
   **and** the `/tool/7` WS handshake → **401** (the cross-origin-rejection proof).
3. Valid cookie **+** correct Origin → **200/101** (happy path still works). The
   "correct Origin" fixture must use the algorithm from **revision-2 finding 2**:
   Origin/Referer matching the *request's own* `Host` header, not a value derived
   from a listen address — test with a `Host` other than `localhost` (e.g.
   `192.168.226.233:4001`) to actually exercise the fix, not just the
   `httptest`-default loopback host that would pass either algorithm.
4. WS upgrade to a **non-allowlisted** `/tool/7/…` path → **400/403** (uses the new
   `Manifest.GUI.ProxyRoutes` allowlist, revision-2 finding 1).
5. `/tool/7/../control` normalizes and does **not** reach `/control` → **404**.
   **Corrected method (revision-2 finding 6):** issue the request with an
   `http.Client` that has redirect-following **disabled** (or invoke the
   `http.Handler` directly), assert there is **no** 3xx response and the control
   handler is never invoked — do not rely on `net/http`'s own dot-segment
   redirect to produce the 404, that would prove `ServeMux`'s behavior, not the
   proxy's guard. Add a second case with percent-encoded traversal
   (`/tool/7/%2e%2e/control`), checked against both `URL.Path` and `URL.RawPath`.
6. A proxied HTML response carries `Content-Security-Policy: frame-ancestors 'self'`.
7. The shared gate also rejects an **unauthenticated `/control`** handshake (proving
   the gate is shared, not `/tool`-only).
8. **(revision 2, finding 5)** The shared gate also rejects unauthenticated
   `/console/*` and `/capture/*` handshakes (not just `/control` and `/tool/*`).
9. **(revision 2, finding 5)** `Authorization: Bearer <sessionToken>` (no cookie)
   succeeds equivalently to the cookie path for a headless/CLI-style client.
10. **(revision 2, finding 5)** The `iolbox_session` cookie set on the SPA response
    carries the exact attributes `HttpOnly; SameSite=Strict; Path=/` (assert on the
    parsed `Set-Cookie`, not just presence).
11. **(revision 2, finding 5)** A fake upstream handler on the AF_UNIX side asserts
    the proxied request carries **no** `iolbox_session` cookie and **no**
    `X-Forwarded-*`/`Forwarded` headers, while any unrelated cookies present on the
    original request are passed through intact.

Plus the **T2.6 rewriter tests**:
- A root-absolute `href`/`src`/`hx-get`/`<form action>` (and a `<base href>`, and a
  3xx `Location:`) in a `text/html` body is rewritten `/foo → /tool/<id>/foo`;
  non-HTML and over-cap bodies pass through unmodified; JS-generated URLs are
  documented-unsupported.
- **(revision 2, finding 8)** An upstream response that still carries a
  `Content-Encoding` header (despite `Accept-Encoding` removal on the outbound
  request) is passed through **opaque, unrewritten** — not tokenized.
- **(revision 2, finding 8)** A **chunked** `text/html` upstream response is
  de-chunked, rewritten, and re-emitted with a **corrected** `Content-Length` and
  no stale `Transfer-Encoding: chunked` header.

Additional B1 acceptance: inbound `X-Forwarded-*`/`Forwarded` are stripped and the
`iolbox_session` cookie is **not** forwarded to the netns pack process (also covered
by test 11 above); the proxy sends upstream requests with `Accept-Encoding` removed;
the `allow-same-origin` iframe-sandbox trade-off and its accepted-risk line are
recorded in `proxy.go`'s doc comment per T2.5 (the actual `sandbox` attribute value
is B4's responsibility, see Wave 2 below — B1 documents the ruling, does not render
the iframe).

---

## 7. Summary for the dispatching session

**4 batches, 2 waves, max concurrency 3 (wave 1).**

| Wave | Agents | Batches |
|---|---|---|
| 1 | 3 | **B1** wsbridge proxy + security gate + rewriter (+ `tool.SocketPath`, `tool.ProxyRoute`, `server.ToolProxyTarget`, `x/net/html`) · **B2** secbench pack port + offline wheelhouse + manifest/GUI key-match test · **B3** T2.3 uid-drop options test + T2.8 `tool.listPacks` round-trip test |
| 2 | 1 | **B4** frontend slice (palette / ToolNode / edit dialog / console tool-tab / `tool.listPacks` client) |

The shape is **wide-but-shallow** — three independent wave-1 agents on disjoint
toolchains (Go/security, rootfs/Python, Go/tests), then a single frontend tail that
codes against landed, integrated backend contracts. That is deliberately far lighter
than P1's 17-batch, 6-wave structure, because P2 has no in-package seven-way fan-out,
no syscall-heavy `_linux` surface beyond two files, and two of its eight tasks are
verification of code P1 already shipped.

**The one batch to treat as security-review-grade is B1** — it defines the auth model
`wsbridge` has never had and closes the CSRF-to-attack primitive; its seven security
tests plus the rewriter test are non-negotiable gate items.

**Three judgment calls the owner should confirm** (details §3): **A1** — the
`tool.listPacks` DTO is treated as already-frozen (edit dialog shows Name+pack for
secbench, options empty); if a per-module field editor is intended, B3 must extend
and land the DTO before B4. **A2** — the T2.6 rewriter ships regardless (gate requires
its test) even though whether secbench *needs* it is a VM observation. **A3** — the
frontend is staggered to wave 2 rather than run parallel; a zero-coupling wave-1 head
start is offered but not recommended.

Two things the dispatching session must do that no agent can: **(a)** run the
integration build on **both** `GOOS` between waves — the Windows-native build compiles
neither B1's `endpoint_linux.go` accessor nor B3's `endpoint_linux_test.go`; **(b)**
diff B1's and B3's reported unexported-symbol lists in packages `tool` and `server`
before merging wave 1.

---

## Gate to P3 (restated for traceability — not modified in substance; package list
and execution-vs-compile distinction corrected per revision-2 finding 4)

From `docs/learning-tools-nodes-plan.md` §P2 "Gate to P3":

`go test ./internal/wsbridge/... ./internal/server/... ./internal/protocol/...
./internal/tool/...` green (the `internal/tool` package added — the original
restatement omitted it despite the T2.3 test living there), including every T2.5
security test (§6 items 1-11 above), the T2.6 rewriter tests (§6), the T2.3
`options.json` ownership test (bind+read+write as `ioltool` — **must be an actual
execution on the Linux VM/builder**, not just a `GOOS=linux go vet` compile check,
per revision-2 finding 4), and a `tool.listPacks` round-trip test (T2.8). Manual
e2e (agent-browser, per MEMORY GUI-check policy): drag SecBench → wire to IOL switch →
node `running` → open console → htmx cards render correctly under the `/tool/<id>/`
prefix (rewriter works) → run ARP Spoof → frames visible in the link's capture tab.
Security assertions observable: a cross-site POST to a module endpoint (no cookie /
foreign Origin) returns 401/403.
