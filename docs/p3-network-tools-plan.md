# P3 — Network learning-tool packs: AAA server, web server, HTTP client

Status: dispatch plan. Adds **three new data-driven tool packs** alongside `secbench`, plus **one small `Palette.svelte` collapse**. No supervisor Go changes are required for the MVP scope (RADIUS AAA + :8080 web server + HTTP client). One *optional, explicitly-scoped* supervisor enablement (privileged-port binding) is required only for the TACACS+ and port-80 stretch goals — see "The privileged-port constraint" below. This plan does **no** work on the palette/Inspector/wsbridge/manifest machinery: it already generically supports N packs.

## Model loop / process
1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it** (same as `p2-go-wireup-plan.md`).
3. **`codex luna-xhigh` agent(s) implement**, one pack per batch. The three packs are file-level independent (separate directories, separate `go.mod`s) and can run in parallel batches; the single shared edit is `runtime/build-rootfs.sh` (three additive, non-overlapping install blocks) and `app/src/lib/components/Palette.svelte` (one edit, Batch 4).
4. **Orchestrating session deploys to the appliance VM and validates live** per the per-pack checklists. The plan does not attempt VM steps.

## Relationship to `docs/p2-go-wireup-plan.md`
Independent. P2-wireup rewrites `secbench`'s build/install region of `build-rootfs.sh` (removes the wheelhouse/venv/`attacks/*.py` blocks around lines 144-367). P3 only **adds** new blocks (new `install -d` dirs, new `go build`s, new pack installs) and touches none of secbench's lines. If P2-wireup lands first the secbench region shrinks; P3's additions are appended after it either way. **No file-level collision.** Implement/review in either order.

---

## Facts established by reading the code (do not re-derive)

- **Pack registration is 100% data-driven.** `supervisor/internal/tool/manifest.go:79` `LoadPacks(dir)` enumerates immediate subdirs of the packs dir and `LoadPack`s each; there is no hardcoded pack list. A new pack = a new dir under `/opt/iolbox/tools/packs/<id>/` with a valid `pack.json` + its compiled GUI binary. Confirmed: only `secbench` and `stub` exist today (`runtime/files/tools/packs/`).
- **`LoadPack` only requires `gui.bin` to physically exist; `modules[].script` existence is only checked *per module*.** `manifest.go:43` resolves `gui.bin` via `manifestResolve`→`contained()` (must exist on disk or the pack drops from the palette). `manifest.go:48-54` resolves each `modules[].script` the same way. **A pack with `modules: []` therefore has zero script-existence traps** — only its `gui.bin` must be installed. All three P3 packs use `modules: []`, so this whole class of the trap that bit secbench in P2-wireup does not apply here.
- **`interpreter` is declarative-only** (`tool.go:118`, no code branches on it; grep-confirmed). Use `"none"` like `stub` (`runtime/files/tools/packs/stub/pack.json:4`).
- **Caps: only `NET_RAW` is allowlisted** (`tool.go:107` `AllowedCaps`), and `manifestCheckCaps` (`manifest.go:189`) rejects anything else. `caps: []` is legal and correct for all three P3 packs (none needs raw sockets).
- **CRITICAL — ambient caps are hardcoded, not derived from the manifest.** `endpoint_linux.go:278` sets `AmbientCaps: []string{"NET_RAW"}` unconditionally; the pack process runs as `User` = `ioltool` (`tool.go:264-266` default). Consequence: **a pack can never bind a port < 1024** (no `CAP_NET_BIND_SERVICE`, not root). This is the single most important constraint in this plan. See "The privileged-port constraint."
- **A pack process is the sole process in its node's netns, with `eth1` as its only data-plane interface** (`tool.go:69-71` `GuestIface = "eth1"`). Any listener it binds (`0.0.0.0:<port>`) is automatically confined to the lab fabric (eth1 + lo) with zero sandboxing code — the netns *is* the sandbox. This is the service-pack analog of secbench's `EnforceLabIface`; for a *listener* it is free (no allowlist code needed), because there is no other NIC to leak onto.
- **`eth1` gets an optional static IP already, no new code.** Inspector's IP/prefix/gateway fields (`Inspector.svelte:232-243`, `updateNet` at `:95-102`) thread to `Config.Net` (`tool.go:278`) → `AssignAddr` (`netns_linux.go:21`). A lab router points `tacacs-server host <thatIP>` / `radius-server host <thatIP>` / an HTTP GET at it. Nothing to build.
- **The pack GUI has NO login** (session convention). Reference template: `secbench/gui/main.go` (`main()` at `:27` — reads `IOLBOX_TOOL_OPTIONS` + `IOLBOX_TOOL_SOCK`, listens on the AF_UNIX socket, `chmod 0600`, serves `mux`) and `secbench/gui/server.go` `routes()` (`:46` — `GET /healthz` + app routes, no auth handler anywhere). wsbridge's T2.5 session+Origin check gates all `/tool/{nodeId}/*` generically; add no per-pack auth.
- **Static Go binaries in a netns+cgroup cage are lightweight** — secbench's are 5-15 MB static ELF, sub-second start, low idle RSS. **DefaultLimits (`tool.go:93-99`) = 2 GiB mem / 512 pids / 2 CPU** apply unless a pack sets `manifest.Limits` (`manifest.go:56-61` honors it). These three packs are near-idle daemons; they may optionally set a smaller `limits` (e.g. 256 MiB) — see decision 7.
- **Palette already iterates packs generically** (`Palette.svelte:224-245`): one draggable `.palette-item` per pack via `{#each toolPacks}`, each `onDragStart(e, "tool", undefined, pack.id)`. Adding a pack today auto-adds an icon here — exactly what the owner does *not* want.
- **Drop already defaults the pack when `packId` is undefined.** `CanvasInner.svelte:472-483` `buildDroppedNode`: for `kind==="tool"` it uses `packId ?? labStore.toolPacks[0]?.id` for both `config.pack` and the node face icon. So a generic palette entry that drags with `packId=undefined` produces a valid node backed by the first pack. **No `CanvasInner` change is needed.**
- **The Inspector pack selector already exists and needs zero changes** (`Inspector.svelte:213-230`, `<select>` → `updatePack` at `:91-94` sets `node.config.pack`). This is the owner's "pick the pack from the edit option" mechanism. Do not duplicate it.
- **`config.pack` is schema-mandatory for tool nodes** (`internal/lab/validate.go:67-73`: "config.pack is required" / "must be a non-empty string"). So the collapsed palette entry must drop a node with a concrete default pack id, never blank. The `toolPacks[0]` fallback satisfies this.
- **Icon registry** (`app/src/lib/icons.svelte.ts:45-102`) has device keys: `router, switch, l3-switch, pc, laptop, firewall, server, cloud, ap, nat, tool`. Palette renders pack icons via `iconSvg(pack.icon || "tool", 28)` (`Palette.svelte:234`). `server` and `cloud` are directly reusable; `tool` is the generic wrench glyph.
- **build-rootfs.sh pack pattern** (live line numbers, will shift under P2-wireup): GUI build `runtime/build-rootfs.sh:169-174` (`cd .../gui && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$BIN" .`); dir reservation `:317-319`; pack.json + GUI install `:340-347`.

---

## The privileged-port constraint (READ THIS BEFORE PICKING PORTS)

Because ambient caps are hardcoded to `NET_RAW` and the process is non-root `ioltool` (`endpoint_linux.go:278`), the pack **cannot bind any TCP/UDP port below 1024**. This is decisive:

| Service | Standard port | Privileged? | MVP decision |
|---|---|---|---|
| RADIUS auth / acct | UDP 1812 / 1813 | **No** (>1024) | **Bind directly. Zero supervisor change.** |
| TACACS+ | TCP 49 | **Yes** (<1024) | Stretch goal — needs supervisor enablement (below) |
| HTTP (web server) | TCP 80 | **Yes** (<1024) | **Default to :8080 (unprivileged).** :80 is stretch. |
| HTTP client (outbound) | n/a (client) | No bind | **Zero issue.** |

**Owner's resource question — answered without hedging:** Yes, all three are lightweight. Each is a single static Go binary (est. 6-10 MB), one process, near-idle (a UDP recvloop, a `net/http` server, or an idle client), well under the default 2 GiB/2 CPU cage. This is the same lightweight pattern P2-wireup adopted. No interpreter, no venv, no extra runtime. Confirmed.

**Supervisor enablement for privileged ports (stretch only — do NOT do in MVP):** to serve TACACS+ on :49 or HTTP on :80 at their real ports, exactly one of these small supervisor changes is needed, and it is the *only* exception to "no supervisor code to add a pack":
- **Option A (recommended if pursued):** set sysctl `net.ipv4.ip_unprivileged_port_start=1` inside the tool netns during netns setup (`netns_linux.go` / `netns.go` cmd builders). Scoped to the tool netns only; no cap change; lets `ioltool` bind any port on eth1. This is the least-privilege, most-contained option.
- **Option B:** add `CAP_NET_BIND_SERVICE` to the ambient set — but this requires editing the hardcoded `endpoint_linux.go:278` *and* `AllowedCaps` (`tool.go:107`) *and* its test (`tool_test.go:330-334`), and grants the cap process-wide. More blast radius than A.

The plan's MVP deliberately sidesteps this entirely by choosing RADIUS + :8080. TACACS+/:80 are flagged as a follow-up requiring Option A.

---

## Decisions locked (so a fresh agent doesn't re-litigate them)

1. **One self-contained binary per pack = the pack's `gui.bin` AND its service.** Unlike secbench (a GUI + 18 separate attack binaries built from a shared `tools/secbench-attacks-go` module), each P3 pack is a single persistent daemon: the same process serves the config GUI over the AF_UNIX socket *and* runs the service listener (RADIUS UDP loop / HTTP server) on eth1, started as a goroutine from `main()`. There is **no separate service binary and no `tools/<pack>-go` module.** Rationale: the service is stateful/persistent and shares config with the GUI in-process; splitting it would add IPC for zero benefit. (The HTTP-client pack has no listener at all — it performs the fetch in-process on request.)
2. **Repo layout:** `runtime/files/tools/packs/<pack>/` containing `pack.json` + `gui/` (own `go.mod`, `main.go`, `config.go`, `server.go`, service files, `templates/`, `static/`). Mirrors `secbench/gui/` **minus** `attacks/` and the shared external module. Pack ids: `aaa`, `webserver`, `httpclient`.
3. **Manifests use `modules: [], groups: [], options: [], caps: [], interpreter: "none"`, `gui.transport: "unix"`, `gui.console: "http"`, `gui.health: "/healthz"`, `gui.proxyRoutes: [{prefix:"/", allowWS:true}]`** (WS on `/` so a live-log fragment can stream like secbench; use htmx polling if WS is not needed — either is fine, keep `allowWS:true` for headroom). Empty `modules`/`groups` are valid (`stub` proves it) and avoid the script-existence trap.
4. **No login on any pack.** Copy the current `secbench/gui/main.go` + `server.go` structure verbatim as the "no-login GUI" skeleton. The only mandatory route is `GET /healthz` returning 200.
5. **AAA scope: RADIUS first (MVP), TACACS+ second (stretch).** Justification: (a) RADIUS binds unprivileged (1812/1813) → zero supervisor change; TACACS+ needs :49 → supervisor enablement. (b) Go has a solid RADIUS library (`layeh.com/radius`) while TACACS+ tooling is thin — but this project hand-rolls wire protocols from spec routinely (EIGRP/OSPF/HSRP/VRRP in secbench), so TACACS+ is *implementable*, just more code (MD5-obfuscated body per RFC 8907, TCP framing). (c) For Cisco device-admin labs TACACS+ is the more idiomatic protocol, so it is a first-class *stretch* goal, not dropped — the AAA pack GUI is designed from day one to show a protocol toggle with TACACS+ as a "coming/enable privileged ports" state. **MVP = RADIUS Access-Request/Accept/Reject + optional Accounting; TACACS+ authen/author added once Option-A port enablement lands.**

   **UPDATE (checked this session, per owner ask): the sibling PNetLab "AAA Suite" node (memory `pnetlab-aaa-suite`, different repo/product) is Docker-based and runs REAL FreeRADIUS + a real Shrubbery `tac_plus` binary** — its Go code is a supervisor/config-generator/web-GUI (spawns the real daemons, writes their config files, tails their logs, shells out to `radtest` for its built-in test client), not a hand-rolled protocol codec. **This does not transplant directly** into iolbox's architecture (no Docker, no apt-installed daemons — every pack here is one static Go binary in a netns), so decision 5's hand-roll-in-Go approach stands. Two things DO carry forward: (a) its GUI/UX shape (dashboard, RADIUS/TACACS+ config tabs, live auth-log tail, built-in test client) is a good reference for this pack's `templates/` layout — follow its tab structure loosely rather than inventing one from scratch; (b) a real-hardware protocol gotcha it hit: **modern FreeRADIUS (post-BlastRADIUS mitigation) silently drops Access-Requests lacking `Message-Authenticator`**, and real Cisco NAS clients send it by default — folded into §1.2's `radius.go` spec below as a MUST-support item, not optional polish.
6. **Web server pack's content model: single editable page + configurable port + access-log tail.** Not a CMS, not a file manager. See Batch 2.
7. **Per-pack `limits`:** set `"limits": {"memoryMax": 268435456, "pidsMax": 64, "cpuMax": "100000 100000", "swapMax": 0}` (256 MiB / 64 pids / 1 CPU) in each `pack.json`. These daemons are tiny; this makes the resource story explicit and self-documenting. (Optional but recommended; DefaultLimits also work.)
8. **Palette collapse:** replace the per-pack `{#each toolPacks}` loop with **one** generic entry labeled "Network tools", icon `tool` (existing wrench glyph), dragging `kind:"tool"` with an explicit deterministic default pack id (decision 9). Keep the loading/error states.
9. **Default pack for a freshly-dropped generic node:** compute in `Palette.svelte` as the first available of a preference order `["webserver","aaa","httpclient"]` falling back to `toolPacks[0]?.id`. Rationale: deterministic regardless of install/sort order, and `webserver` is the least surprising, most self-explanatory default for a new user. The user then re-picks via the Inspector selector (owner's stated flow). This is a 2-line `$derived` in Palette; `CanvasInner.buildDroppedNode` already handles the value.
10. **Icons (MVP = zero new SVG):** `webserver` → `server`; `httpclient` → `cloud`; `aaa` → `firewall`. All exist in the registry. **Optional polish (flagged work item, not blocking):** add a `shield` glyph for AAA and a `globe` glyph for the HTTP client to `icons.svelte.ts` `BUILTIN` so they don't visually collide with secbench (`firewall`) and NAT/cloud. New SVGs must be drawn (stroke, 0 0 24 24, `currentColor`) — a real task, called out, not hand-waved. The collapsed palette entry uses `tool` regardless.

---

## Batch 1 — AAA server pack (`aaa`)  [MVP: RADIUS]

### 1.1 Manifest — `runtime/files/tools/packs/aaa/pack.json`
```json
{
  "manifestVersion": 1,
  "id": "aaa",
  "name": "AAA Server",
  "icon": "firewall",
  "interpreter": "none",
  "gui": { "bin": "aaa-gui", "transport": "unix", "console": "http",
           "health": "/healthz", "proxyRoutes": [{ "prefix": "/", "allowWS": true }] },
  "caps": [],
  "options": [],
  "groups": [],
  "modules": [],
  "limits": { "memoryMax": 268435456, "pidsMax": 64, "cpuMax": "100000 100000", "swapMax": 0 }
}
```
`gui.bin: "aaa-gui"` must resolve (be installed) or the pack drops from the palette (`manifest.go:43-46`).

### 1.2 GUI+service binary — `runtime/files/tools/packs/aaa/gui/`
- `go.mod` (module `iolbox/tools/packs/aaa/gui`, Go 1.22+ for `net/http` `ServeMux` pattern routing as secbench uses). **Before writing a hand-rolled RADIUS codec, check the existing PNetLab AAA Suite node (see decision 5 update) for a reusable reference** — if it's a config wrapper around a real daemon (FreeRADIUS/tac_plus) rather than custom protocol code, that only informs the config/UX shape, not a code-reuse shortcut, and the codec still needs to be hand-rolled or a Go library vendored. Decision absent a code-reuse shortcut: hand-roll a minimal RADIUS codec (Access-Request/Accept/Reject, PAP `User-Password` decode per RFC 2865, Message-Authenticator optional) — it is ~200 lines and keeps the pack dependency-free and offline-buildable (matching the project's static-binary, no-network-build ethos). Vendoring `layeh.com/radius` is the fallback if the reviewer prefers; if vendored, it must be committed under the module (offline build). **Recommend hand-roll for MVP** unless the AAA Suite check turns up a directly reusable Go path.
- `main.go` — copy `secbench/gui/main.go` skeleton (`:27-68`): read `IOLBOX_TOOL_OPTIONS`/`IOLBOX_TOOL_SOCK`, serve `mux` on the unix socket, `chmod 0600`. **Add:** after building `app`, start the RADIUS listener goroutine: `go app.radius.Serve("0.0.0.0:1812")` (and `:1813` acct if implemented). Binding `0.0.0.0` is auto-confined to eth1+lo by the netns. If eth1 has no address yet the listener still binds (reachable once the Inspector IP is set); log a one-line notice.
- `config.go` — a `Store` mirroring `secbench/gui/config.go` (atomic write-to-`.tmp`+rename, `IOLBOX_TOOL_OPTIONS` path). Config shape:
  ```
  Config {
    SharedSecret string                  // one NAS secret for MVP (single shared secret)
    Clients      []{ Subnet, Secret }    // optional per-NAS secrets (phase 1.b)
    Users        []{ Username, Password, Service, PrivLvl }  // plaintext lab creds
    Protocol     string                  // "radius" (MVP) | "tacacs" (stretch, disabled if ports<1024 unavailable)
  }
  ```
- `radius.go` — the codec + `Serve(addr)`: UDP recvloop, parse Access-Request, verify shared secret + `User-Password`, look up `Users`, reply Access-Accept (with `Service-Type`/`Cisco-AVPair shell:priv-lvl=` for device-admin realism) or Access-Reject. Every attempt appends to an in-memory ring buffer (`newRing`, copy from `runner.go:23-52`) for the live auth log. **MUST support `Message-Authenticator`** (RFC 2869 attribute 80, HMAC-MD5 over the packet with a zeroed Message-Authenticator field, keyed by the shared secret) on inbound Access-Requests — carried forward from the sibling PNetLab "AAA Suite" project's real-hardware finding (memory `pnetlab-aaa-suite`): modern FreeRADIUS (post-BlastRADIUS CVE-2024-3596 mitigation) silently drops any Access-Request lacking it, and real Cisco IOS/IOS-XE RADIUS clients send it by default — a codec that doesn't validate/expect it risks silently failing against exactly the NAS devices this pack exists to test against. Verify inbound Message-Authenticator when present; do not require it to be present for MVP simplicity, but log a warning on requests missing it so a real compatibility gap is visible rather than silent.
- `server.go` — copy the `routes()`/`render()` shape from `secbench/gui/server.go:46-70`, **no login**. Routes: `GET /healthz`; `GET /{$}` dashboard (status: protocol, listen addr, eth1 present?, user count, last N auth attempts); `GET /settings` (edit shared secret + users); `POST /users/add`, `POST /users/delete`, `POST /settings/save`; `GET /frag/log` (htmx-polled auth-attempt tail). Reuse `secbench/gui/static/` (`pico.min.css`, `htmx.min.js`) — copy both files into `aaa/gui/static/`.
- `templates/` — `layout.html` (nav, no login), `dashboard.html`, `settings.html`. Adapt secbench's, drop the module-grid concepts (this is a **service dashboard**, not attack module cards, per the owner's framing).
- `util.go` — `hasLabIface()` (copy `runner.go:164-167`) to render an "eth1 not wired yet" banner.

### 1.3 TACACS+ (stretch, do NOT implement in MVP)
Stub the GUI's protocol toggle to show TACACS+ as "requires privileged-port enablement (see p3 plan §privileged-port)". Implementation, when unblocked by Option A: `tacacs.go` hand-rolling RFC 8907 authen START/REPLY + author, MD5 body obfuscation, TCP framing on :49. Tracked as a follow-up batch, not this one.

### Batch 1 acceptance gate (implementing agent, local)
1. `cd runtime/files/tools/packs/aaa/gui && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
2. Unit test for the RADIUS codec: craft an Access-Request with a known secret + `User-Password`, assert Accept for a valid user and Reject for a bad password; assert a wrong shared secret is rejected. (Regression guard for the hand-rolled codec — required.)
3. `healthz` returns 200 over the unix socket in a table-test using `httptest`/`net.Dial("unix", …)`.

---

## Batch 2 — Web server pack (`webserver`)

### 2.1 Manifest — `runtime/files/tools/packs/webserver/pack.json`
Same shape as Batch 1.1 with `id:"webserver"`, `name:"Web Server"`, `icon:"server"`, `gui.bin:"webserver-gui"`, `caps:[]`, empty modules/groups, the 256 MiB limits.

### 2.2 GUI+service — `runtime/files/tools/packs/webserver/gui/`
- `main.go` — secbench skeleton; **add** `go app.web.Serve()` starting an `http.Server` on the configured port (**default `:8080`**, unprivileged). `0.0.0.0:<port>` → auto-confined to eth1.
- `config.go` — `Config { ListenPort int (default 8080); IndexHTML string (default a simple "IOLbox lab web server" page); ExtraPaths map[string]string (optional path→body) }`. Atomic write pattern from secbench.
- `web.go` — the content server: serves `IndexHTML` at `/`, `ExtraPaths` entries, a `/healthz`-style liveness *for the lab side* is unnecessary (the pack GUI's own `/healthz` is on the unix socket, separate). Every request appends `method path status remoteIP` to a ring buffer → access-log tail. **On port change, restart the listener** (stop old `http.Server`, start new) — keep this simple: rebuild on save.
- `server.go` (GUI, unix socket, no login) — routes: `GET /healthz`; `GET /{$}` dashboard (listen port, eth1 addr, request count, access-log tail); `GET /settings` + `POST /settings/save` (edit `IndexHTML` in a `<textarea>`, set port); `GET /frag/log` (htmx access-log tail). Reuse `static/` (pico+htmx).
- Keep it genuinely simple: **one editable page + port + access log.** No file manager, no upload, no CMS (owner's explicit "simple html webserver").

### 2.3 Port 80 (stretch)
Document in the settings UI that binding :80 requires the privileged-port supervisor enablement (Option A). Default and validate the port field to ≥1024 in the MVP; allow <1024 to be *entered* but show a "requires privileged-port enablement" warning and refuse to restart the listener on it (bind would `EACCES`).

### Batch 2 acceptance gate (local)
1. `go build ./... && go vet ./... && go test ./...` — green.
2. Test: start `web.Serve` on `:0` (ephemeral), GET `/`, assert 200 + the configured `IndexHTML` body; assert access-log ring recorded the hit.
3. Test: port-change restart rebinds and serves the new port; old port stops.

---

## Batch 3 — HTTP client pack (`httpclient`)

### 3.1 Manifest — `runtime/files/tools/packs/httpclient/pack.json`
Same shape, `id:"httpclient"`, `name:"HTTP Client"`, `icon:"cloud"`, `gui.bin:"httpclient-gui"`, `caps:[]`, empty modules/groups, 256 MiB limits. **No listener** — outbound only, no port constraint at all.

### 3.2 GUI — `runtime/files/tools/packs/httpclient/gui/`
- `main.go` — secbench skeleton, unix socket only. No service goroutine.
- `server.go` (no login) — routes: `GET /healthz`; `GET /{$}` the client UI; `POST /fetch` performs the request server-side (in the netns, so it egresses eth1 onto the lab fabric) and returns a fragment with **status line, response headers, and the raw body** (escaped, in a `<pre>`). Optional: a request-history ring buffer + `GET /frag/history`.
- `client.go` — `net/http` client with a sane timeout (e.g. 15s), following the user's method (GET/POST/PUT/DELETE/HEAD), custom headers (textarea, one `Key: Value` per line), and body. Uses the default resolver (the netns can be given a DNS via the Inspector gateway/DNS or the lab; document that IP URLs always work). **Explicitly NOT a renderer:** it displays the raw response only.
- **HTML preview:** default **raw-only** (owner's "not a browser"). *Optional, nearly-free:* a "Preview" tab that drops the raw HTML into a `sandbox`-attributed `<iframe srcdoc>` with `sandbox="allow-same-origin"` *omitted* (i.e. fully sandboxed, no scripts, no same-origin) — this is a few lines and safe. Include it only if trivial; default hidden behind a tab so "raw" remains the primary view.
- UI shape: URL bar + method `<select>` + headers textarea + body textarea + "Send" → raw response viewer (status, headers, body). Reuse `static/` (pico+htmx).

### 3.3 SSRF / scope note
The client runs inside the node's netns whose only route is eth1 onto the lab fabric (plus whatever gateway the Inspector sets). It cannot reach the host or the appliance management plane unless the lab topology explicitly routes there — the netns confinement is the boundary. State this in the plan; no allowlist code needed for the MVP lab use case. (If the node is wired to a NAT gateway with internet egress, it can reach the internet — that is the same capability any lab node has and is acceptable.)

### Batch 3 acceptance gate (local)
1. `go build ./... && go vet ./... && go test ./...` — green.
2. Test: point `client.go` at an `httptest.Server`, assert the fragment contains the status, a known response header, and the raw body verbatim; assert a non-2xx status is displayed (not error-swallowed).
3. Test: request timeout is enforced (a hung server → surfaced error, not a hang).

---

## Batch 4 — Frontend: collapse the palette to one "Network tools" entry

File: `app/src/lib/components/Palette.svelte`

### 4.1 Replace the per-pack loop (`:224-245`)
Replace the `{#if toolPacks.length > 0}{#each toolPacks as pack}…{/each}` block with a **single** generic entry, preserving the loading/error branches:
```svelte
{#if toolPacks.length > 0}
  <div class="palette-item" draggable="true" role="button" tabindex="0"
       ondragstart={(e) => onDragStart(e, "tool", undefined, defaultToolPack)}
       title="Network tools — pick RADIUS/AAA, web server, or HTTP client after dropping">
    <span class="swatch tool" aria-hidden="true">{@html iconSvg("tool", 28)}</span>
    <div class="item-text">
      <div class="item-name">Network tools</div>
      <div class="item-sub">Learning tool</div>
    </div>
  </div>
{:else if labStore.toolPacksLoading}
  <div class="empty-hint">Loading learning tools…</div>
{:else if labStore.toolPacksError}
  <div class="empty-hint error-hint">Learning tools unavailable</div>
{/if}
```

### 4.2 Add the deterministic default (`$derived`, near `:21`)
```ts
const defaultToolPack = $derived(
  ["webserver", "aaa", "httpclient"].find((id) => toolPacks.some((p) => p.id === id))
    ?? toolPacks[0]?.id
);
```
Passing `defaultToolPack` as `packId` makes the dropped node deterministic; `buildDroppedNode` (`CanvasInner.svelte:472-483`) consumes it and also picks up the pack's icon. The user re-picks via the Inspector selector (`Inspector.svelte:213-230`) — unchanged.

### 4.3 No other frontend changes
`CanvasInner.svelte`, `Inspector.svelte`, `ToolNode.svelte`, `labStore` — untouched. The `onDragStart` signature (`Palette.svelte:11`) already accepts `packId`.

### Batch 4 acceptance gate
- App builds (`npm run build` / `svelte-check`) clean; the left sidebar shows exactly one "Network tools" entry regardless of how many packs are installed; dropping it creates a valid `tool` node whose `config.pack` is `webserver` (or first-available), editable to any installed pack in the Inspector.

---

## Batch 5 — build-rootfs.sh: build + install the three GUIs

File: `runtime/build-rootfs.sh` (additive only; will sit after whatever the P2-wireup edit leaves)

### 5.1 Build step (mirror the secbench GUI build at `:169-174`), once per pack
```sh
for pack in aaa webserver httpclient; do
  echo "== build-rootfs: building $pack pack GUI (linux/amd64) =="
  ( cd "$SCRIPT_DIR/files/tools/packs/$pack/gui"
    go vet ./...; go test ./...
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
        -o "$BUILD_DIR/$pack-gui" . )
done
```

### 5.2 Dir reservations (mirror `:317-319`)
```sh
for pack in aaa webserver httpclient; do
  install -d -m 0755 -o root -g root "$ROOTFS_DIR/opt/iolbox/tools/packs/$pack"
done
```

### 5.3 Install pack.json + GUI + static (mirror `:340-347`)
```sh
for pack in aaa webserver httpclient; do
  install -m 0644 -o root -g root \
    "$SCRIPT_DIR/files/tools/packs/$pack/pack.json" \
    "$ROOTFS_DIR/opt/iolbox/tools/packs/$pack/pack.json"
  install -m 0755 -o root -g root "$BUILD_DIR/$pack-gui" \
    "$ROOTFS_DIR/opt/iolbox/tools/packs/$pack/$pack-gui"
done
```
The `gui.bin` in each manifest (`aaa-gui` / `webserver-gui` / `httpclient-gui`) must exactly match the installed filename or `LoadPack` fails (`manifest.go:43-46`). Static assets (pico/htmx) are `//go:embed`ed into each GUI binary (as secbench does — `main.go:16-17`), so nothing extra to install.

### 5.4 No Python, no venv, no wheelhouse for these packs
All three are pure static Go, `CGO_ENABLED=0`, no runtime deps. Do not touch secbench's Python removal (that is P2-wireup's job).

### Batch 5 acceptance gate (local, rootfs-equivalent)
- Run 5.1+5.2+5.3 against a throwaway staging dir (not a full debootstrap); assert all three `pack.json` + `<pack>-gui` land at `.../packs/<pack>/`, GUIs are mode 0755 and are ELF linux/amd64, and each manifest's `gui.bin` filename matches the installed binary.

---

## Live-VM validation checklist (orchestrator, per pack; reuse the prior-phase VM/veth-rename-to-eth1 pattern)

**All packs, first:** rebuild/redeploy rootfs; confirm all three packs `LoadPack` (palette shows the single "Network tools" entry; the Inspector selector lists `AAA Server`, `Web Server`, `HTTP Client` alongside `Security Bench`). Drop a "Network tools" node, confirm it defaults to `webserver`, re-pick each pack in the Inspector, start each — confirm the GUI loads over `/tool/{nodeId}/` **with no login prompt** (T2.5 gate only).

**AAA (RADIUS):**
- Give the node a static eth1 IP (Inspector). Add a lab user + shared secret in the GUI.
- On a peer IOL router on the same fabric: `aaa new-model` + `radius server LAB` / `address ipv4 <aaa-eth1-ip> auth-port 1812 acct-port 1813` / `key <secret>`, `aaa authentication login default group radius local`. Telnet/console-auth with the lab user → **Access-Accept**; wrong password → **Access-Reject**; both visible in the GUI's live auth log.
- Confirm the RADIUS listener only ever answers on eth1 (there is no other NIC in the netns — assert by topology).

**Web server:**
- Static eth1 IP; edit `index.html` in the GUI; note the port (:8080).
- From a peer node with an HTTP client (an IOL router `copy http://<web-eth1-ip>:8080/ null:`, or the HTTP-client pack node) → fetch returns the configured page; the GUI access-log shows the hit.

**HTTP client:**
- Wire an HTTP-client node to the same fabric as the web-server node; from its GUI, GET `http://<web-eth1-ip>:8080/` → raw status + headers + body render; confirm it does **not** execute page scripts (raw view). POST/headers round-trip against the web server or the AAA node's healthz-equivalent.

**Cross-pack end-to-end:** one lab with all three packs + one IOL router: router authenticates against the AAA node, the HTTP-client node fetches the web-server node's page — all traffic confined to the lab fabric.

Report a per-pack, per-step verdict table. Explicitly note that TACACS+ and HTTP :80 were **deferred** pending the privileged-port supervisor enablement (Option A), not attempted in this scope.

---

## Out of scope (do not do)
- Any change to palette/Inspector/wsbridge/manifest machinery beyond Batch 4's single Palette collapse and Batch 5's additive installs.
- TACACS+ implementation, HTTP :80/:49 binding, and the privileged-port supervisor enablement (Option A) — flagged as a follow-up, gated on owner sign-off for the one supervisor touch.
- Per-pack auth / login pages (standing convention: T2.5 gates all `/tool/*`).
- New icon SVGs (`shield`/`globe`) — optional polish, flagged in decision 10, not required for MVP.
- HTML rendering in the HTTP client (raw-only by owner's framing; sandboxed-iframe preview optional).
- Touching secbench's build/install region (P2-wireup owns it).

---

### Critical files for implementation
- J:\Claude code\iolab\runtime\files\tools\packs\secbench\gui\main.go (no-login GUI + unix-socket skeleton to copy)
- J:\Claude code\iolab\runtime\files\tools\packs\secbench\gui\server.go (routes()/render(), no-login template)
- J:\Claude code\iolab\supervisor\internal\tool\endpoint_linux.go (line 278 — hardcoded ambient NET_RAW, the privileged-port constraint)
- J:\Claude code\iolab\runtime\build-rootfs.sh (lines 169-174 GUI build, 317-347 pack install — the patterns to mirror)
- J:\Claude code\iolab\app\src\lib\components\Palette.svelte (lines 224-245 — the loop to collapse)
- J:\Claude code\iolab\supervisor\internal\tool\manifest.go (read-only reference — LoadPack gui.bin existence check; modules:[] avoids the script trap)
