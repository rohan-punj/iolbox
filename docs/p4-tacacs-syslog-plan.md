# P4 — Finish TACACS+ and add a syslog receiver (privileged-port enablement + two pack deliverables)

Status: dispatch plan. Closes the two items `docs/p3-network-tools-plan.md` explicitly deferred (decision 5's TACACS+ stretch, and the privileged-port supervisor enablement) and adds a new `syslog` pack. **Exactly one supervisor Go change** (netns sysctl, Option A) plus **one edit inside the shipped `aaa` pack** (additive files only — `radius.go` is not touched) plus **one new pack directory** and its three additive `build-rootfs.sh` list entries. No palette / Inspector / wsbridge / manifest work: pack registration is already 100% data-driven and the palette was collapsed to a single generic entry in P3 Batch 4.

## Model loop / process
1. **Opus writes this plan** (done).
2. **`codex sol-medium` adversarially reviews it.** Two areas need disproportionate attention:
   - **§3 TACACS+ wire format.** A wrong pad-derivation chain, a wrong header field order, a wrong `seq_no` rule, or the ACCT-REPLY field-order trap (§3.6) produces a codec that compiles, round-trips against itself in unit tests, and *silently fails against real Cisco IOS* — the same bug class sol-medium caught in the RADIUS/secbench work. Review §3.2 byte-for-byte against RFC 8907.
   - **§2 Option A security reasoning.** Does netns-scoping the sysctl actually hold, or is there a gap (a path by which the knob leaks to the root netns, or by which a pack reaches something new because it can now bind :49/:514)?
3. **`codex luna-xhigh` agent(s) implement.** **Recommendation: three batches, two agents minimum, Batch A alone.** Batch A (§2) is supervisor-internal and changes the start path of *every* tool node including the already-shipped secbench/webserver/httpclient — a different blast radius and a different reviewer-attention level from pack-local protocol code. An agent holding `internal/tool/` in its head should not also be freelancing RFC 8907 in the same pass. Batches B (§3) and C (§4) are file-level independent of each other except for `runtime/build-rootfs.sh` (B touches none of it; C appends `syslog` to all **three** existing `for pack in …` lists — corrected count per sol-medium finding #14, an earlier draft said "two") and may run in parallel. **Order: A first and merged**, because B's and C's *local* gates do not depend on it but every one of their *live* gates does.
4. **Orchestrating session deploys to the real appliance VM and validates live** per §6. The plan attempts no VM steps.

## Relationship to prior plans
- `docs/p3-network-tools-plan.md` — this plan executes its "privileged-port constraint / Option A" recommendation and its decision-5 TACACS+ stretch. **Option A vs B is not re-litigated** (§2.1 only re-verifies the code still matches what P3 claimed).
- `docs/p2-go-wireup-plan.md` — unrelated; no file overlap.

---

## 1. Facts established by reading the code (do not re-derive)

**Supervisor / privileged ports**
- `supervisor/internal/tool/endpoint_linux.go:278` sets `AmbientCaps: []string{"NET_RAW"}` unconditionally in `endpointLaunchSpec()`. **P3's claim still holds; the line number has not drifted.**
- The cap set is enforced twice over: `supervisor/internal/tool/launch_test.go:32-36` pins the exact `setpriv` argv — `--bounding-set -all,+cap_net_raw`, `--inh-caps -all,+cap_net_raw`, `--ambient-caps -all,+cap_net_raw`. There is no `CAP_NET_BIND_SERVICE` anywhere in the launch path, and the process runs as `ioltool` (`endpoint_linux.go:491-493`).
- **Consequence, confirmed: no pack can bind a port < 1024 today.** TACACS+ (TCP 49) and syslog (UDP 514) are both blocked. RADIUS (UDP 1812) shipped precisely because it sidesteps this.
- `supervisor/internal/tool/netns.go:15-17` — `netnsCreateNetnsCmds(nodeID)` returns exactly one `cmdSpec`: `{name:"ip", args:{"netns","add",NetnsName(nodeID)}}`.
- `netns.go:29` shows the netns-exec cmdSpec idiom: `{name: "ip", args: NetnsExecArgs(nodeID, []string{…})[1:]}`. `NetnsExecArgs` (`tool.go:77-81`) prepends `ip netns exec <ns>`; the `[1:]` drops the leading `"ip"` because `cmdSpec.name` already carries it.
- `netns_linux.go:7-9` — `CreateNetns` is `runCmds(netnsCreateNetnsCmds(nodeID))`. `runCmds` (`tool.go:470-490`) is the **must-succeed** path: it returns the first failure wrapped with combined output. `runCmdsBestEffort` (`tool.go:495-506`) is used only by the teardown/cleanup wrappers (`DeleteNetns`, `DeleteVeth`, `TeardownMgmt`). **A setup step belongs in `runCmds`.**
- `endpoint_linux.go:102-116` is the setup sequence: `CreateNetns` → `CreateVethPair` → optional `AssignAddr`, with failures routed through `endpointStartFailure` (which tears the node down). **Correction per sol-medium finding #14**: `endpointRecordObject()` precedes the veth setup step specifically, not independently every one of the three calls — verify the actual call sites when implementing rather than assuming a 1:1 record-then-call pattern for all three.
- `endpointSetupSteps()` (`endpoint_linux.go:589-591`) returns `{"cgroup","netns","veth"}` and drives teardown ordering; `endpoint_test.go:37-43` asserts teardown is its exact reverse. **A sysctl has no teardown object** (it dies with the netns), so this list must NOT be changed — see §2.3.
- `netns_other.go` mirrors every `netns_linux.go` export with `ErrUnsupportedPlatform`. Any new exported function needs a stub there; **folding into `CreateNetns` avoids that entirely** (§2.2).
- **`procps` is already in the rootfs.** `runtime/build-rootfs.sh:238` — `BASE_INCLUDE="systemd,systemd-sysv,udev,dbus,iproute2,iputils-ping,libssl3,openssh-client,sudo,procps,iptables,tcpdump,util-linux,libcap2-bin,passwd"`. `sysctl` (from `procps`, at `/usr/sbin/sysctl`) exists in the shipped image. **No `BASE_INCLUDE` change is needed.**
- **`/usr/sbin` is on the supervisor's PATH.** `runtime/files/iolbox-supervisor.service:29` — `Environment=PATH=/opt/iolbox:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, and the non-systemd fallback `runtime/files/iolbox-init.sh:50` sets the same. `runCmds` uses `exec.Command` with bare names and inherits that env, and `ip netns exec` passes the environment through to the exec'd binary. **A bare `sysctl` resolves.**
- `net.ipv4.ip_unprivileged_port_start` has been **per-network-namespace since kernel 4.11** (`net->ipv4.sysctl_ip_unprivileged_port_start`), and the bind check consults it for **AF_INET6 as well as AF_INET** despite the `ipv4.` name. Debian bookworm ships 6.1. Reading/writing it through `ip netns exec <ns> sysctl …` targets the exec'd task's netns, because `/proc/sys/net` is resolved against the reading task's `nsproxy` — this is the same idiom as the universally used `ip netns exec <ns> sysctl -w net.ipv4.ip_forward=1`.

**The `aaa` pack as shipped**
- `runtime/files/tools/packs/aaa/gui/` = `main.go`, `config.go`, `server.go`, `radius.go`, `radius_test.go`, `util.go`, `env.go`, `templates/{layout,dashboard,settings,log}.html`, `static/{pico.min.css,htmx.min.js}`, `go.mod` (`module iolbox/tools/packs/aaa/gui`, `go 1.22`).
- `main.go:24-28` starts the service as a bare goroutine — `go func(){ if err := app.radius.Serve("0.0.0.0:1812"); err != nil { log.Printf(...) } }()` — **before** the AF_UNIX GUI listener is set up (`:32-49`). A listener failure today only logs; the GUI still comes up and `/healthz` still passes. This shape is the template for TACACS+ and syslog, with one addition (§3.5): the error must also be *visible in the dashboard*, not only in a log nobody reads.
- `server.go:17-24` — `App{store *Store; radius *RadiusServer}`, `NewApp(store)`. `routes()` at `:26-41`: `GET /static/`, `GET /healthz`, `GET /{$}`, `GET /settings`, `POST /settings/save`, `POST /users/add`, `POST /users/delete`, `GET /frag/log`. No login, per standing convention (wsbridge's T2.5 session+Origin check gates all `/tool/{nodeId}/*` generically).
- `server.go:112-122` — `render(w, page, data)` does `template.ParseFS(assets, "templates/layout.html", "templates/"+page)` then `ExecuteTemplate(w, "layout", data)`. Assets are `//go:embed templates/*.html static/*` (`server.go:14-15`).
- `radius.go:45-79` — `AuthAttempt{At, Remote, User, Result, Message}` and the package-level `attemptRing` / `newRing(max)` / `Add` / `List`. **Both are reusable by a second protocol as-is; `newRing` is not RADIUS-specific.**
- `config.go:23-28` — `Config{SharedSecret, Clients []Client, Users []User, Protocol string}`; `User{Username, Password, Service, PrivLvl}`. `defaultConfig()` (`:36-38`) = `{SharedSecret:"labsecret", Protocol:"radius", Users:[]}`. `Load()` (`:59-61`) normalizes an empty `Protocol` to `"radius"`. `Store` writes atomically (`.tmp` + rename, `:104-108`).
- `templates/dashboard.html:5` polls `/frag/log` with `hx-get … hx-trigger="every 3s" hx-swap="innerHTML"`, rendering the `attempts` table defined identically in `dashboard.html:7` and `log.html:2`. **This duplicated `attempts` block is the pattern to extend, and must be edited in both files or the dashboard and the polled fragment will diverge.**
- `templates/settings.html:2` currently renders the protocol `<select>` with `TACACS+ (requires privileged-port enablement)` and the prose "MVP serves RADIUS on UDP 1812. TACACS+ is reserved for the privileged-port supervisor follow-up." **This is the stub text this plan replaces.**
- `radius_test.go` is the bar for codec tests: a table of accept / bad-password / wrong-shared-secret cases driven through `server.handlePacket` (`:36`), a `healthz`-over-AF_UNIX test (`:51`), and an atomic-store test asserting no `.tmp` survives (`:105`).
- `pack.json` shape (all P3 packs identical): `manifestVersion:1`, `interpreter:"none"`, `gui:{bin,transport:"unix",console:"http",health:"/healthz",proxyRoutes:[{prefix:"/",allowWS:true}]}`, `caps:[]`, empty `options`/`groups`/`modules`, `limits:{memoryMax:268435456,pidsMax:64,cpuMax:"100000 100000",swapMax:0}`.
- `runtime/build-rootfs.sh` pack integration is exactly three list-driven blocks: build `:176-185`, dir reservation `:329-331`, install `:360-366` — all three iterate `for pack in aaa webserver httpclient`.
- **Icons taken:** `aaa`→`firewall`, `webserver`→`server`, `httpclient`→`cloud`. Remaining uncollided registry keys (`app/src/lib/icons.svelte.ts:46-102`): `router, switch, l3-switch, pc, laptop, ap, nat, tool`.
- **Palette needs no change.** `Palette.svelte:228-241` renders one generic "Network tools" entry; a new pack appears automatically in the Inspector's pack `<select>`. The only stale artifact is the tooltip string at `Palette.svelte:235` (`"…pick RADIUS/AAA, web server, or HTTP client after dropping"`), which does not enumerate a syslog pack — see §4.7.
- **Pre-existing repo litter, flag do-not-touch:** `aaa/gui/gui.exe` and `webserver/gui/gui.exe` (~12 MB Windows build artifacts) are checked in. They are inert (`build-rootfs.sh` builds with `-o "$BUILD_DIR/$pack-gui"`, and `//go:embed` never sees them). Removing them is out of scope here; raise separately.

---

## 2. Batch A — Option A: netns-scoped unprivileged-port start (supervisor)

### 2.1 Confirmation, not re-litigation
P3 §"privileged-port constraint" recommended Option A (netns sysctl) over Option B (`CAP_NET_BIND_SERVICE` in the ambient set). Re-read of the current code confirms the premise is unchanged: `endpoint_linux.go:278` still hardcodes `NET_RAW`, and `launch_test.go:32-36` pins the setpriv bounding/inheritable/ambient sets so Option B would require editing `endpoint_linux.go` + `AllowedCaps` in `tool.go` + `tool_test.go` + `launch_test.go` and would grant the cap **process-wide** to every pack. **Option A is adopted. Do not reconsider.**

### 2.2 The change — fold the sysctl into `netnsCreateNetnsCmds`

File: `supervisor/internal/tool/netns.go`, function at `:15-17`. Append one `cmdSpec`:

```go
{name: "ip", args: NetnsExecArgs(nodeID, []string{
    "sysctl", "-w", "net.ipv4.ip_unprivileged_port_start=1"})[1:]},
```

Resulting argv: `ip netns exec iolt<N> sysctl -w net.ipv4.ip_unprivileged_port_start=1`.

**Why this exact form (decided, with the alternatives named and rejected):**
- **`sysctl -w`, not `sh -c "echo 1 > /proc/sys/…"`.** `procps` is already in `BASE_INCLUDE` (`build-rootfs.sh:238`) and `/usr/sbin` is on the supervisor's PATH (`iolbox-supervisor.service:29`), so the package-dependency argument for the `/proc` write does not apply here — there is nothing to add. Against `sh -c`: every single `cmdSpec` in `netns.go` is a direct argv with no shell, and introducing a shell into a root-run command sequence is both a style break and a (small, needless) new injection surface. **Do not use `sh -c`.** If a future rootfs ever drops `procps`, the `/proc`-write fallback is the documented escape hatch — but it is not what ships here.
- **`-w` without `-q`** so the confirmation line is captured in `runCmds`' combined-output error text when it fails.
- **`runCmds`, not `runCmdsBestEffort`.** `CreateNetns` already uses `runCmds`, and this must be a hard error: a silently-skipped sysctl leaves `ioltool` unable to bind :49/:514, which surfaces hours later as "TACACS+ mysteriously doesn't answer" rather than as a start-time failure. `sysctl -w` exits non-zero on an unknown or unwritable key. (The exit code is still not fully trusted — §6 requires a read-back.)
- **Folded into `netnsCreateNetnsCmds` rather than added as a new exported `EnableUnprivilegedPorts(nodeID)`.** Folding means (a) zero edits to `endpoint_linux.go`, (b) zero new stub in `netns_other.go`, (c) it is structurally impossible for a future netns-creating call site to forget it. The rejected alternative — a separate exported wrapper called between `endpoint_linux.go:102-104` and `:109` — is more surface for identical behaviour. The sysctl does not depend on the veth existing, only on the netns existing, so ordering inside `netnsCreateNetnsCmds` (immediately after `netns add`) is correct.
- Update the `netnsCreateNetnsCmds` doc comment (`netns.go:13-14`) to state that namespace creation now also opens the namespace's unprivileged port range, and why.

### 2.3 What must NOT change
- **`endpointSetupSteps()` (`endpoint_linux.go:589-591`) stays `{"cgroup","netns","veth"}`.** The sysctl is not a kernel object with an independent lifetime — it is destroyed with the netns. Adding a step would break `endpoint_test.go:37-43`'s reverse-order invariant for no benefit and would imply a teardown action that does not exist.
- No change to `AmbientCaps`, `AllowedCaps`, `manifestCheckCaps`, `launchSetprivArgv`, or any pack's `caps: []`.
- No change to `netns_other.go` (no new export).

### 2.4 Applies to ALL tool nodes, unconditionally — decided
Not opt-in per pack. Rationale:
1. **It matches the established philosophy.** P3 §"Facts" fixed the posture that *the netns is the sandbox, not a per-pack allowlist* — a pack's listener is confined to the lab fabric because `eth1` is the only non-`lo` interface in the namespace, with zero per-pack code. A per-pack privileged-port allowlist would be the first exception to that, and would need a new manifest field, a new validator, a new test, and a new way for a pack to be misconfigured.
2. **It removes the argument that a "privileged-port pack" is expensive.** Once the knob is unconditional there is no per-pack cost at all — which is precisely why §4.1 can put syslog in its own pack instead of bundling it into `aaa`.
3. **It grants nothing.** A pack that does not bind a low port is unaffected; nothing about secbench/webserver/httpclient changes behaviourally.

The only real cost is availability: if the sysctl write fails, *every* tool node fails to start, not just AAA/syslog. Given the appliance ships its own kernel (bookworm 6.1, well past the 4.11 per-netns floor) and its own `procps`, fail-fast is the correct trade — and the failure text names the exact sysctl, so it diagnoses itself. State this risk in the commit message.

### 2.5 Security posture — this is a namespace-scoped kernel knob, not a capability grant
The plan must say this explicitly, because "let the unprivileged process bind port 49" reads like a privilege escalation and is not one:
- **No capability changes.** The bounding, inheritable, and ambient sets remain `-all,+cap_net_raw` (`launch_test.go:32-36`), the process remains `ioltool` with `--no-new-privs`. `ioltool` gains nothing outside this namespace: it cannot bind :22 or :443 in the root netns, cannot touch the appliance management plane, and cannot write this sysctl itself (writing it requires `CAP_NET_ADMIN` in the owning user namespace; the supervisor writes it as root *before* the pack process is launched).
- **The blast radius is one namespace whose only interface is `eth1`.** `net.ipv4.ip_unprivileged_port_start` is per-netns (`net->ipv4.sysctl_ip_unprivileged_port_start`, kernel ≥4.11). Writing it inside `iolt<N>` cannot be observed or inherited by the root netns or by any other node's netns.
- **Qualification, per sol-medium finding #9**: the plan's earlier "every socket is reachable only from the lab fabric" claim is true for the packs in THIS plan (TACACS+ and syslog both bind to serve the lab, and the node's only non-`lo` interface is `eth1`), but is not universally true of every tool netns — `netns.go` also constructs an `mgmt0` interface for a TCP management fallback path. A hypothetical future pack that binds `0.0.0.0:<port>` would be reachable via `mgmt0` too, not just `eth1`. This plan's own listeners are unaffected (nothing here changes), but state this precisely rather than as a blanket "only eth1" guarantee — a future pack author relying on that blanket claim could get it wrong.
- **What actually changes:** the pack process may now bind `0.0.0.0:1-1023` on `eth1`+`lo` *inside its own namespace*. Since that namespace already permitted binding every port ≥1024 on the same two interfaces, the delta is "which port number the lab-facing listener sits on" — not "who can reach it." The reachability boundary is unmoved.
- **Threat-model continuity:** pack processes are fully trusted first-party code under the T2.5 model established earlier in this project (the netns + cgroup cage exists to contain *bugs and lab traffic*, not to defend against a malicious pack); wsbridge's session+Origin check remains the sole gate on `/tool/{nodeId}/*`. This change does not shift any assumption that model rests on. **Do not re-derive the whole security model in the commit or the code comment — cite it.**
- **The one thing a reviewer should genuinely check** (flagged for sol-medium): whether `ip netns exec`'s mount handling could ever cause the write to land on the *root* netns's `/proc/sys` — i.e. whether `/proc/sys/net` is reliably resolved against the exec'd task's netns rather than the mount's. The kernel resolves `/proc/sys/net` per reading task's `nsproxy`, which is why `ip netns exec <ns> sysctl -w net.ipv4.ip_forward=1` is the standard idiom; §6.1's read-back from *inside* the netns plus a read-back from the *root* netns (which must still show the distro default `1024`) is the empirical proof and is a required live gate, not optional.

### 2.6 Batch A acceptance gate (implementing agent, local)
1. `cd supervisor && go build ./... && go vet ./... && go test ./...` — green.
2. Extend `TestNetnsCreateSequence` (`netns_test.go:8-33`): the `want` slice gains, as the **second** element (immediately after `{"netns","add","iolt7"}`), `{name:"ip", args:[]string{"netns","exec","iolt7","sysctl","-w","net.ipv4.ip_unprivileged_port_start=1"}}`. The existing eth1-escape assertion loop (`:23-32`) is unaffected (the new args contain no `eth1`) and must not be weakened.
3. New assertion (own test): the sysctl cmdSpec is namespace-prefixed. **Corrected per sol-medium review — the plan's original index was wrong**: after `NetnsExecArgs(nodeID, …)[1:]` the argv is `{"netns","exec",NetnsName(n),"sysctl","-w","net.ipv4.ip_unprivileged_port_start=1"}`, so the assertion is `args[0]=="netns" && args[1]=="exec" && args[2]==NetnsName(n) && args[3]=="sysctl"` (NOT `args[3]==NetnsName(n)` as an earlier draft of this plan said — that would reject the correct implementation). This is the unit-test analogue of the eth1-escape guard and is the single most valuable regression test in this batch.
4. **Corrected per sol-medium review**: `endpoint_test.go`'s `TestEndpointTeardownIsReverseOfSetup` passing does NOT by itself prove §2.3 was honoured — `endpointTeardownSteps()` derives its result directly from `endpointSetupSteps()`, so that test stays green even if the setup list changes. Instead, add an explicit assertion (in `netns_test.go` or `endpoint_test.go`) that `endpointSetupSteps()` still equals exactly `[]string{"cgroup","netns","veth"}` — that is the actual proof no new setup-step entry was added for the sysctl.
5. Grep proof in the PR description: `AmbientCaps`, `AllowedCaps`, and `launchSetprivArgv` are untouched.
6. **New, per sol-medium finding #8**: add a test proving that if the sysctl write fails (second command in the netns-create sequence), the already-created netns is still torn down cleanly by the existing `endpointStartFailure` path — i.e. a mid-setup failure at this new step doesn't leak a netns the way a successful-but-incomplete setup could.

---

## 3. Batch B — TACACS+ in the `aaa` pack (RFC 8907)

Directory: `runtime/files/tools/packs/aaa/gui/`. **`radius.go` and `radius_test.go` are not edited. Zero lines.** New files plus narrowly-scoped edits to `main.go`, `server.go`, `config.go`, and the templates.

### 3.1 Decisions locked
1. **Both listeners run unconditionally, always.** RADIUS on UDP 1812 and TACACS+ on TCP 49 both start as goroutines from `main()`, mirroring `main.go:24-28` exactly. `Config.Protocol` **no longer gates anything**: it is demoted to a dashboard "primary protocol" hint, its accepted values become `"radius" | "tacacs" | "both"`, and `defaultConfig()` / `Load()`'s empty-value normalisation change from `"radius"` to `"both"`. Rationale: (a) it needs no listener lifecycle machinery — the riskiest, most stateful part of the `webserver` pack (`web.go:109-127`'s `Restart`) is avoided entirely; (b) it removes the failure mode where a user configures `tacacs-server host` on a router and then spends twenty minutes discovering a GUI toggle was set to "radius"; (c) an idle listener in a netns costs nothing, and device-admin labs routinely run both. Existing on-disk `options.json` files carrying `"protocol":"radius"` load unchanged and simply highlight RADIUS on the dashboard.
2. **TACACS+ key is its own config field, `TacacsKey`, falling back to `SharedSecret` when empty.** Cisco labs commonly reuse one secret; separating them is one field and one `if`. **`TacacsKey`/`SharedSecret` must not both be empty — refuse to serve TACACS+ (log a clear "no key configured" state, keep the GUI up) rather than run with an empty key** (per sol-medium finding #7 — RFC 8907 requires a shared secret; an empty key is not a safe default). **This is a deliberately restricted single-key lab profile, not a multi-client RFC 8907-conformant server** (per sol-medium finding #3 — the RFC expects per-client key lookup keyed by source address via `Config.Clients`, which this plan leaves unused, matching the RADIUS pack's own existing single-shared-secret MVP scope). State this explicitly in the settings page prose and in `tacacs.go`'s package doc comment so it's a documented trust-boundary decision, not an accidental gap.
3. **ASCII login only for AUTHEN.** PAP/CHAP/MS-CHAP/MS-CHAPv2 (`authen_type` 0x02/0x03/0x05/0x06) are parsed far enough to be *rejected with a clear `FAIL` and a logged reason*, never silently ignored. §7 flags them as follow-up.
4. **AUTHOR is in scope**; **ACCT is in scope too** — see §3.6 for the reasoning and the one trap it carries.
5. **`Users []User`'s existing `Username`/`Password`/`PrivLvl` fields are reused verbatim; `Service` is NOT reused for TACACS+ AUTHOR matching — corrected per sol-medium finding #5, a real functional bug in the original draft.** The existing `Service` field is RADIUS-specific vocabulary (defaults to `"login"` per `settings.html`, interpreted as a RADIUS Service-Type attribute in `radius.go`). TACACS+'s AUTHOR request carries a completely different vocabulary in its `service=` arg (typically `"shell"` for exec sessions). Comparing them literally means every existing/default lab user (`Service:"login"`) would authenticate successfully via TACACS+ AUTHEN and then get AUTHOR `FAIL` on a `service=shell` request — breaking exactly the Cisco exec-authorization flow this feature exists to demonstrate, and failing the plan's own §6.2 step 5 live gate. **Fix: add a new field `User.TacacsService string` (default `"shell"` when unset via `Load()`'s normalization, matching what Cisco IOS actually sends for `aaa authorization exec`), and match AUTHOR's `service=` arg against `TacacsService`, never against the RADIUS `Service` field.** `PrivLvl` remains shared between both protocols (it drives RADIUS's `Cisco-AVPair shell:priv-lvl=` and TACACS+'s AUTHOR `priv-lvl=` arg identically, which is fine — priv-level is a protocol-agnostic concept, unlike `Service`).
6. **The live log is one merged table, not a parallel one.** Implemented with zero edits to `radius.go` (§3.5).

### 3.2 Wire format — implement exactly this, do not re-derive from the RFC

New file `tacacs_wire.go` (codec only — no I/O, no config, so it is testable in isolation).

**Header — exactly 12 bytes, big-endian:**

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 1 | `version` | `(major<<4)\|minor`. major = `0xc`. minor = `0x0` (default) or `0x1` (used for PAP/CHAP one-shot login). So the byte is `0xc0` or `0xc1`. **Echo the client's version byte verbatim in the reply.** **Validate it against the packet type/authen_type per sol-medium finding #6 (RFC 8907 §5.4.1): minor version MUST be 0 for ASCII AUTHEN and for every AUTHOR/ACCT packet; minor version 1 is only valid for a PAP/CHAP/MS-CHAP AUTHEN START.** Since this pack rejects non-ASCII `authen_type` anyway (§3.3), in practice this means: reject any AUTHOR/ACCT packet with minor version 1, and reject any AUTHEN packet with minor version 1 (since only ASCII is supported) — both with `ERROR`, not `FAIL`, since this is a version-negotiation failure, not a credential failure. |
| 1 | 1 | `type` | `AUTHEN=0x01`, `AUTHOR=0x02`, `ACCT=0x03` |
| 2 | 1 | `seq_no` | Client packets are odd (START=1), server packets even. **Server reply `seq_no` = request `seq_no` + 1.** A session must end before 255 wraps; treat `seq_no==255` on a request as a protocol error. |
| 3 | 1 | `flags` | `UNENCRYPTED=0x01`, `SINGLE_CONNECT=0x04` |
| 4 | 4 | `session_id` | uint32 BE, chosen by the client. **Echo verbatim.** |
| 8 | 4 | `length` | uint32 BE, length of the body *after* the header |

Reject: `length > 65535` (sanity cap; RFC permits more but a lab NAS never sends it), any `type` outside 1-3, any major version != `0xc`.

**Body obfuscation — the pseudo-pad.** The body is XOR'd with a pad derived from MD5. Let `S` = the 4 raw `session_id` bytes **exactly as they appear in the header** (big-endian), `K` = the shared key bytes, `V` = the 1 `version` byte, `N` = the 1 `seq_no` byte. Then:

```
MD5_1 = MD5(S || K || V || N)
MD5_2 = MD5(S || K || V || N || MD5_1)
MD5_3 = MD5(S || K || V || N || MD5_2)
...
MD5_i = MD5(S || K || V || N || MD5_{i-1})
pad   = MD5_1 || MD5_2 || … , truncated to len(body)
plaintext = body XOR pad
```
(Corrected typo per sol-medium finding #14: it is `MD5_2`, not `MDSD_2`.)

Each `MD5_i` for i>1 appends the **full 16-byte previous digest**, not a prefix of it. The operation is its own inverse (XOR), so the same function encrypts and decrypts — **which is exactly why a round-trip test proves nothing** (§3.7).

**Always reject the `UNENCRYPTED` flag (`0x01`) unconditionally — not only when a key happens to be configured** (per sol-medium finding #7: since §3.1 now requires a non-empty key at all times, "unencrypted body" is never legitimate; reply `ERROR` and log "client sent unencrypted body — refusing"). There is no cleartext-allowed mode in this pack.

**Wrong-key detection is probabilistic, not guaranteed — word it that way in code comments and tests (per sol-medium finding #7).** The XOR obfuscation carries no integrity check (no MAC), so a body de-obfuscated with the wrong key does not reliably fail to parse — it can coincidentally produce structurally-valid-looking length fields. The codec's job is to validate every fixed enum value, every length field's internal consistency (e.g. START's 4 length bytes summing to `len(body)-8`), and every string field's bounds AFTER de-obfuscation, and reject on the first inconsistency — this catches most wrong-key cases in practice but is not a cryptographic guarantee. Do not claim "wrong key is always rejected" anywhere in code comments or the plan's own test descriptions.

**AUTHEN START body** (`type=AUTHEN`, `seq_no=1`):
`action(1) | priv_lvl(1) | authen_type(1) | authen_service(1) | user_len(1) | port_len(1) | rem_addr_len(1) | data_len(1) | user | port | rem_addr | data`
- `action`: `LOGIN=0x01`, `CHPASS=0x02`, `SENDAUTH=0x04`
- `authen_type`: `ASCII=0x01`, `PAP=0x02`, `CHAP=0x03`, `MSCHAP=0x05`, `MSCHAPV2=0x06`
- `authen_service`: `NONE=0x00`, `LOGIN=0x01`, `ENABLE=0x02`, `PPP=0x03`, …
- Validate: the four length bytes must sum to exactly `len(body)-8`, else malformed.

**AUTHEN REPLY body** (server → client):
`status(1) | flags(1) | server_msg_len(2 BE) | data_len(2 BE) | server_msg | data`
- `status`: `PASS=0x01`, `FAIL=0x02`, `GETDATA=0x03`, `GETUSER=0x04`, `GETPASS=0x05`, `RESTART=0x06`, `ERROR=0x07`, `FOLLOW=0x21`
- `flags`: `NOECHO=0x01` — **must be set on the GETPASS reply** so the NAS does not echo the typed password.
- Note the width change: START uses 1-byte lengths, REPLY uses **2-byte** lengths. This asymmetry is a classic implementation bug; assert it in the codec test.

**AUTHEN CONTINUE body** (client → server):
`user_msg_len(2 BE) | data_len(2 BE) | flags(1) | user_msg | data`
- **The flags byte comes *after* both length fields here**, unlike REPLY where flags is second. Getting this wrong shifts every subsequent byte. `flags` bit `ABORT=0x01` — on abort, log the `data` field as the abort reason and end the session without a reply.

**AUTHOR REQUEST body:**
`authen_method(1) | priv_lvl(1) | authen_type(1) | authen_service(1) | user_len(1) | port_len(1) | rem_addr_len(1) | arg_cnt(1) | arg_1_len(1) … arg_N_len(1) | user | port | rem_addr | arg_1 … arg_N`
- `authen_method`: `TACACSPLUS=0x06` is what a router that authenticated via TACACS+ sends.
- Args are `key=value` (mandatory) or `key*value` (optional) ASCII strings — e.g. `service=shell`, `cmd*`.

**AUTHOR REPLY body:**
`status(1) | arg_cnt(1) | server_msg_len(2 BE) | data_len(2 BE) | arg_1_len(1) … arg_N_len(1) | server_msg | data | arg_1 … arg_N`
- `status`: `PASS_ADD=0x01`, `PASS_REPL=0x02`, `FAIL=0x10`, `ERROR=0x11`, `FOLLOW=0x21`. **Note `FAIL` is `0x10`, not `0x02`** — AUTHOR status values are a different enum from AUTHEN's.
- Note the ordering: all the per-arg *lengths* precede `server_msg`, and the arg *bodies* come last, after `data`.

### 3.3 Session logic — `tacacs.go`

**TCP framing.** TACACS+ is TCP, so unlike RADIUS there is no one-datagram-one-request shortcut:
- `net.Listen("tcp", "0.0.0.0:49")`, accept loop, **one goroutine per connection** (spiritually the same shape as `webserver/gui/web.go`'s `http.Server`, but hand-rolled because this is not HTTP).
- Per connection: read exactly 12 header bytes with `io.ReadFull`, parse `length`, then `io.ReadFull` exactly that many body bytes. **Never `conn.Read` into a big buffer and assume one packet arrived** — TCP will coalesce and split, and a naive read is the bug that makes this work on a loopback test and fail against a real router.
- `SetReadDeadline` per read (30s idle) so a hung NAS cannot pin a goroutine forever; `defer conn.Close()`.
- Per-connection session state: `session_id`, `version`, last `seq_no`, the username collected so far, and which prompt is outstanding. Reject any packet whose `session_id` differs from the connection's first — one session per connection.
- **Do not advertise `SINGLE_CONNECT`.** Reply `flags=0`. Per RFC, a client that requested it and does not see it echoed closes the connection after the session. Close the connection ourselves after emitting any final status (`PASS`/`FAIL`/`ERROR` for AUTHEN, any AUTHOR status, any ACCT status). IOS opens a fresh connection per AUTHEN/AUTHOR/ACCT exchange; that is expected and cheap.
- Cap concurrent connections (e.g. 64) and drop beyond that with a logged notice — the cage's `pidsMax:64` is about processes, not goroutines, so the pack owns this bound itself.

**ASCII login flow — handle both shapes IOS produces:**
- START with a **non-empty** `user` → reply `GETPASS` + `NOECHO`, `server_msg="Password: "`. Next CONTINUE's `user_msg` is the password → `PASS` or `FAIL`.
- START with an **empty** `user` → reply `GETUSER`, `server_msg="Username: "`. CONTINUE carries the username → reply `GETPASS`+`NOECHO`. Next CONTINUE carries the password → `PASS`/`FAIL`.
- Credential check: linear scan of `cfg.Users` for `Username`+`Password` equality — identical semantics to `radius.go:184-191`, so RADIUS and TACACS+ can never disagree about who is a valid lab user.
- On `FAIL`, set `server_msg` to a generic string (do not distinguish "no such user" from "bad password" on the wire; the *log* records the distinction, the wire does not).
- `authen_type` != ASCII → `FAIL` with `server_msg="only ASCII login is supported"` and a log entry naming the requested type. Never `ERROR` for this (an `ERROR` makes IOS retry; a `FAIL` lets it fall through to the `local` method, which is the behaviour a lab wants).

**AUTHOR:** answer `aaa authorization exec default group tacacs+ local`.
- Parse args; if `service=<User.TacacsService>` (see §3.1 decision 5 — a distinct field from RADIUS's `Service`, default `"shell"`) is present (with or without `cmd*`), look the user up and reply **`PASS_ADD` (0x01)** with `arg_cnt=1` and `arg_1 = "priv-lvl=<User.PrivLvl>"`. `PASS_ADD` means "accept, and add these attributes" — the correct choice, versus `PASS_REPL` which tells the NAS to discard its own attributes.
- If `cfg.Users` has no match, or the requested `service=` value does not match the user's `TacacsService` → `FAIL` (0x10).
- Unknown service → `PASS_ADD` with `arg_cnt=0` (permissive, lab-friendly) and a log line. State this choice in a code comment so it is a decision, not an accident.

### 3.4 Config and settings changes (`config.go`, `server.go`, `templates/settings.html`)
- `config.go`: add `TacacsKey string \`json:"tacacsKey"\`` to `Config`, and add `TacacsService string \`json:"tacacsService"\`` to `User` (per §3.1 decision 5 — distinct from `Service`, `Load()` normalizes an empty value to `"shell"`). `defaultConfig()` → `Protocol: "both"`. `Load()`'s normalisation (`:59-61`) → empty becomes `"both"`; accept `radius|tacacs|both`, anything else normalises to `"both"`. `cloneConfig` needs no change beyond covering the new string fields.
- `server.go` `saveSettings` (`:56-72`): accept `"both"` in the protocol whitelist at `:64`; persist `tacacs_key` into `cfg.TacacsKey`.
- `templates/settings.html:2`: replace the "requires privileged-port enablement" `<option>` label and the trailing `<p class="muted">` prose. The `<select>` gains a `both` option (default). Add a `TACACS+ key` password input with placeholder text stating it falls back to the shared secret when blank. New prose: RADIUS on UDP 1812, TACACS+ on TCP 49, both always listening.

### 3.5 Dashboard and merged live log — zero edits to `radius.go`
- Give `TacacsServer` its **own** `attemptRing` via the existing package-level `newRing(100)` and reuse the existing `AuthAttempt` struct verbatim.
- In `server.go`, add `App.tacacs *TacacsServer` (constructed in `NewApp` alongside `radius`) and a merge helper:
  ```go
  type LogRow struct { AuthAttempt; Proto string }
  func (a *App) attempts() []LogRow  // tag radius rows "radius", tacacs rows "tacacs+", concat, sort by At
  ```
  `dashboard` (`:43-50`) and `logFragment` (`:108-110`) both switch to `a.attempts()`. **`radius.go` is not opened.**
- Templates: `dashboard.html:7` and `log.html:2` both define the `attempts` block — add a `Proto` column **to both, identically**, or the polled fragment will silently diverge from the initial render.
- `dashboard.html:4`'s status grid gains a TACACS+ tile: `TCP 0.0.0.0:49` plus its live bind status.
- **New, and load-bearing:** `App` gains `radiusErr`/`tacacsErr` (mutex-guarded strings) set by the listener goroutines in `main.go`. When a bind fails — most likely `EACCES` on :49 if Batch A is not deployed — the dashboard renders a loud banner: *"TACACS+ could not bind TCP 49: permission denied. This appliance is missing the netns unprivileged-port setting (see p4 plan §2); redeploy the supervisor."* Today `main.go:24-28` only `log.Printf`s, which on a real appliance means the failure is invisible. **The GUI must stay up and `/healthz` must still return 200 when a lab listener fails** — a bind failure is a service degradation, not a pack crash, and turning it into an exit would make the supervisor's liveness watchdog (`endpoint_linux.go:383-413`) tear the node down and hide the real cause. **Test the whole chain, not just `/healthz` (per sol-medium finding #13): the acceptance test must (1) start a listener on an already-bound port so `Serve` fails, (2) assert the goroutine actually sets `tacacsErr`/`radiusErr` (not just that `Serve` returned an error), (3) render the dashboard and assert the banner text appears, (4) separately assert `/healthz` is still 200.** A test that only checks `/healthz` can pass while the error silently never reaches the dashboard — exactly the regression this requirement exists to prevent.
- `main.go`: add a second goroutine mirroring `:24-28` for `app.tacacs.Serve("0.0.0.0:49")`, and a `defer app.tacacs.Close()` beside `:46`.

### 3.6 Accounting (ACCT) — IN SCOPE, with the trap named
Called: **include it.** It is roughly 50-60 lines because it reuses the AUTHOR arg-encoding wholesale, and it pays for itself twice: `aaa accounting exec default start-stop group tacacs+` becomes demonstrable, and the resulting log rows (`cmd=show running-config`, `task_id=…`) are exactly what a learner needs to *see* to understand what accounting is. Refusing it would leave a third of the AAA acronym unimplemented in a pack literally named `aaa`.

**ACCT REQUEST body:**
`flags(1) | authen_method(1) | priv_lvl(1) | authen_type(1) | authen_service(1) | user_len(1) | port_len(1) | rem_addr_len(1) | arg_cnt(1) | arg_1_len(1) … | user | port | rem_addr | arg_1 …`
- `flags`: `START=0x02`, `STOP=0x04`, `WATCHDOG=0x08`.

**ACCT REPLY body — THE TRAP:**
`server_msg_len(2 BE) | data_len(2 BE) | status(1) | server_msg | data`
- **`status` is the LAST fixed field here, not the first.** AUTHEN REPLY and AUTHOR REPLY both lead with `status`; ACCT REPLY does not. Writing ACCT REPLY by copy-pasting the AUTHOR REPLY encoder is the single most likely bug in this batch and produces a packet that a router rejects with a generic error. Assert the exact byte layout in a test with a hand-written literal `[]byte`.
- `status`: `SUCCESS=0x01`, `ERROR=0x02`, `FOLLOW=0x21`.
- **Behaviour: `SUCCESS` for any valid flag combination, `ERROR` for an invalid one — "always SUCCESS" (an earlier draft's claim) is wrong per sol-medium finding #4.** RFC 8907 §7.2 defines `START=0x02`, `STOP=0x04`, `WATCHDOG=0x08` as the only meaningful bit(s) in the low nibble; validate `flags & 0x0e` against exactly `{START, STOP, WATCHDOG, START|WATCHDOG}` (a watchdog update can carry the START bit to indicate a status change) — any other combination (no bits set, `START|STOP` together, `STOP|WATCHDOG` together, etc.) is malformed and gets `ERROR`, not `SUCCESS`. For a valid combination, this is still a lab collector, not a billing system, so always reply `SUCCESS` and append one merged-log row per record with the flags decoded to `start`/`stop`/`watchdog` and the args joined; a bare `WATCHDOG` record (no START/STOP) logs but its args are otherwise ignored.

### 3.7 Testing bar — `tacacs_wire_test.go`, `tacacs_test.go`
The bar is `tools/secbench-attacks-go/internal/attackcommon/packet_test.go`'s checksum-invariant pattern: **a test must check the code against an independent statement of the truth, not against the code itself.** For an XOR-based obfuscation this is not a nicety — a round-trip test of a self-inverse function passes even if the pad is entirely wrong (e.g. if `session_id` were serialised little-endian, or `version` omitted).

Required:
1. **Pad vector, independently computed.** A test that builds the expected pad with a straight-line, inlined `crypto/md5` chain written out longhand in the test body — never by calling the codec's pad function — for a fixed `(session_id, key, version, seq_no)` and a body length that forces **at least three** MD5 blocks (i.e. body > 32 bytes), proving the chaining rule and not just the first digest. Compare byte-for-byte.
2. **Header layout literal.** Encode a header and compare against a hand-written `[]byte{0xc0, 0x01, 0x01, 0x00, …}` literal. Catches field-order and endianness errors.
3. **Body layout literals** for AUTHEN START, AUTHEN REPLY, AUTHEN CONTINUE, AUTHOR REQUEST, AUTHOR REPLY, ACCT REQUEST, ACCT REPLY — each against a hand-written literal that a reviewer can check against §3.2/§3.6 line by line. **In particular: START's 1-byte lengths vs REPLY's 2-byte lengths; CONTINUE's flags-after-lengths; ACCT REPLY's status-last.**
4. **Wrong-key negative test.** Decode a body obfuscated with key A using key B → the parsed length fields must not validate; assert the codec *rejects* it rather than producing garbage strings. (This is what makes a wrong shared secret a clean failure instead of a mysterious one.)
5. **Session-level table test** mirroring `radius_test.go:16-49`: valid user → `PASS`; bad password → `FAIL`; unknown user → `FAIL`; wrong key → rejected (worded/asserted as "codec validation catches this case," not "wrong key is cryptographically guaranteed to be caught" — see §3.2); PAP `authen_type` → `FAIL` with the documented message; AUTHOR `service=<User.TacacsService>` (e.g. `"shell"`) → `PASS_ADD` with `priv-lvl=<configured>`; **AUTHOR with a user whose `Service` (the RADIUS field) happens to be `"shell"` but `TacacsService` is unset/different → must still resolve correctly against `TacacsService`, not `Service`** (the regression test for finding #5); ACCT START → `SUCCESS` + one log row; **ACCT with an invalid flag combination (e.g. `START|STOP` together, or no bits set) → `ERROR`, not `SUCCESS`** (finding #4); **AUTHOR/ACCT packet with minor version 1 → `ERROR`** (finding #6); **empty/unconfigured key → TACACS+ refuses to serve (logged, GUI stays up)** (finding #7); **`UNENCRYPTED` flag set → always `ERROR`, even if this were somehow reached with a key configured** (finding #7).
6. **TCP framing test.** Drive a real `net.Pipe`/loopback connection and deliver a START packet **split across two `Write`s mid-body**, plus two packets **coalesced into one `Write`**. Both must parse correctly. This is the test that catches the naive-`Read` bug before a router does.
7. `seq_no` test: server replies always carry request `seq_no + 1`; an out-of-order or wrapped `seq_no` is rejected.

### 3.8 Batch B acceptance gate (local)
1. `cd runtime/files/tools/packs/aaa/gui && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
2. `git diff --stat` shows **zero changed lines in `radius.go` and `radius_test.go`**, and all pre-existing tests in `radius_test.go` pass unmodified.
3. All seven §3.7 test categories present and passing.
4. `/healthz` still 200 when the TACACS+ bind fails (simulate by pointing `Serve` at an already-bound port in a test).
5. `templates/settings.html` no longer contains the string "requires privileged-port enablement", and `dashboard.html`/`log.html` `attempts` blocks are byte-identical to each other apart from their surrounding `{{define}}`.

---

## 4. Batch C — the `syslog` pack

### 4.1 Placement decision: its own pack, `runtime/files/tools/packs/syslog/` — decided
The bundling argument ("one pack pays the privileged-port cost") **evaporates under §2.4**: the sysctl is unconditional for every tool node, so there is no per-pack cost to pay and nothing to amortise. What remains:
- **Single responsibility, matching the existing layout.** `aaa` / `webserver` / `httpclient` are each one thing with one dashboard, one options file, one ring buffer. Folding a syslog collector into `aaa` would give that pack three services and a dashboard that is really three dashboards, and would put syslog config in the same `options.json` as authentication secrets.
- **A real topology need.** `tacacs-server host` and `logging host` are routinely different boxes at different addresses. Separate packs mean separate nodes, separate `eth1` IPs, and separate placement on the canvas — a learner can wire a log collector on one segment and AAA on another. Bundled, they are forced to share one IP.
- **Failure isolation.** A syslog flood cannot degrade the AAA node's authentication path if they are separate cages.
- Cost: one `pack.json`, one `go.mod`, and three list entries in `build-rootfs.sh`. Cheap.

(The sibling PNetLab "AAA Suite" node bundles RADIUS+TACACS+rsyslog because it is a Docker image running three real daemons under one supervisor — a packaging constraint that does not exist here, where every pack is one static Go binary in a netns. Its *UX* is worth borrowing; its packaging is not, consistent with P3 decision 5's finding that the sibling's Go code does not transplant.)

### 4.2 Manifest — `runtime/files/tools/packs/syslog/pack.json`
Identical in shape to `aaa/pack.json`, with `id:"syslog"`, `name:"Syslog Server"`, `gui.bin:"syslog-gui"`, `caps:[]`, empty `options`/`groups`/`modules`, and the same 256 MiB / 64 pid / 1 CPU limits. **Icon: `"tool"`** — the only uncollided registry key that is not a device glyph (`firewall`, `server`, and `cloud` are taken by `aaa`, `webserver`, `httpclient`). A dedicated document/list glyph in `icons.svelte.ts` is optional polish, flagged in §7, not required.

### 4.3 Pack skeleton
`runtime/files/tools/packs/syslog/gui/` with `go.mod` (`module iolbox/tools/packs/syslog/gui`, `go 1.22`), and `main.go` / `config.go` / `server.go` / `util.go` / `env.go` copied structurally from `aaa/gui/` — same AF_UNIX + `chmod 0600` startup, same atomic-write `Store`, same `hasLabIface()` banner, same no-login `routes()` with `GET /healthz`. Copy `static/pico.min.css` + `static/htmx.min.js` from `aaa/gui/static/` (they are `//go:embed`ed, so nothing extra installs).

`Config`:
```
ListenPort int    // default 514
MaxEntries int    // ring size, default 500
```
No filter state in config — filtering is a query parameter (§4.6), never persisted.

**Settings changes must actually take effect — per sol-medium finding #10, an earlier draft left this unspecified and it would have silently no-op'd.** Mirror `webserver/gui/web.go`'s existing `Restart()` pattern (already established in this project for exactly this problem — a config change to a bound listener): `POST /settings/save` calls a `Receiver.Restart(newPort)` that stops the current `net.PacketConn` (if bound) and starts a new one on the new port, returning an error (surfaced as the same bind-failure banner pattern as §3.5) if the new port can't be bound — on failure, the OLD listener stays running rather than leaving the pack with no listener at all. `MaxEntries` changes take effect on save too: resize the ring by copying up to the new max most-recent entries (never silently drop the whole log on a resize).

### 4.4 Receiver — `syslog.go` (UDP 514 only)
- `net.ListenUDP("udp", 0.0.0.0:<ListenPort>)`, single read loop, **8192-byte buffer**. RFC 3164 caps a message at 1024 bytes but real IOS exceeds it. **Truncation detection, corrected per sol-medium finding #11**: a plain `ReadFromUDP` into an exactly-8192-byte buffer cannot distinguish "datagram was exactly 8192 bytes" from "datagram was larger and got truncated" — `n == len(buf)` is not proof of truncation. Use `ReadMsgUDP` and check the returned flags for `syscall.MSG_TRUNC` (Linux supports this), or read into an 8193-byte buffer and treat `n > 8192` as the truncation signal with the stored/displayed message capped at 8192. Either way, add a test that sends an exactly-8192-byte datagram (not truncated) and a >8192-byte datagram (truncated) and asserts they're distinguished correctly — not just a single boundary-adjacent test.
- One `Entry` per datagram appended to a ring identical in shape to `radius.go:53-79` / `web.go:23-49`:
  ```
  Entry { Received time.Time; SourceIP string; Facility int; Severity int;
          DeviceTime string; Hostname string; Tag string; Message string; Raw string }
  ```
- **`Received` (the receiver's own clock) is the authoritative Time column.** Device clocks in a lab are unsynced and IOS commonly reports `*Mar 1 00:01:02` since boot. `DeviceTime` is shown as a separate, clearly-labelled column. Sorting is always by `Received`.
- **`Raw` is always retained**, whatever the parser makes of the line. Nothing is ever lost to a parse failure.
- Bind failure handling is identical to §3.5: record the error, keep the GUI up, banner it on the dashboard, `/healthz` still 200.

### 4.5 Parsing — lenient, in this order (the real-hardware gotcha)
1. **`<PRI>`** — leading `<`, up to 3 digits, `>`. `facility = PRI/8`, `severity = PRI%8`. If absent, treat as unparsed and keep `Raw`.
2. **RFC 5424** if the next token is `1` followed by a space: `VERSION SP TIMESTAMP SP HOSTNAME SP APP-NAME SP PROCID SP MSGID SP STRUCTURED-DATA [SP MSG]`. **Included** — it is a modest addition given the fields are space-delimited, and rsyslog/journald senders on the same lab fabric emit it. **STRUCTURED-DATA is captured as an opaque string, not parsed into named elements** (SD-element parsing with escaping rules is the expensive part and buys a lab nothing) — **but locating where it ENDS still requires a bracket/quote-aware scanner, corrected per sol-medium finding #12.** A naive whitespace split is NOT sufficient: STRUCTURED-DATA is one or more `[SD-ID param=value ...]` blocks that can themselves contain spaces inside quoted param values, and `"`, `]`, and `\` inside a quoted value are backslash-escaped. The scanner must track bracket depth and quote state character-by-character to find the true end of the STRUCTURED-DATA field (or the `-` no-data marker) before splitting off MSG — do not split on the first space after the position where STRUCTURED-DATA starts. Test with: SD containing an embedded space in a quoted value, an escaped quote (`\"`), an escaped backslash (`\\`), and an escaped closing bracket (`\]`) — not just "SD present" vs. "SD absent as `-`".
3. **RFC 3164** otherwise: `Mmm dd hh:mm:ss` (**day is space-padded, `Mar  1`, not zero-padded — a single-space split loses the field**), then `HOSTNAME`, then `TAG[PID]:`, then the message.
4. **Cisco IOS reality — the item that must not be skipped.** A real IOS device sends, e.g.:
   `<190>123: *Mar  1 00:01:02.345: %SYS-5-CONFIG_I: Configured from console by console`
   After the `<PRI>` there is an optional **sequence number `NNN:`** (from `service sequence-numbers`), and the timestamp carries a leading `*` (clock-not-authoritative marker) and **fractional seconds** — none of which is RFC 3164 conformant. A strict 3164 parser fails on every message a Cisco router sends, which would make this pack useless for its single primary use case. The parser must: skip an optional leading `\d+: `, tolerate a leading `*` or `.` on the timestamp, tolerate `.mmm` fractional seconds, and treat a leading `%FACILITY-SEV-MNEMONIC:` token as the `Tag`.
5. **Final fallback:** `Hostname = SourceIP`, `Message = Raw`, severity/facility from PRI if it parsed. Never drop a datagram.

### 4.6 GUI — live tail with a filter that survives the refresh
Routes mirror the established shape: `GET /healthz`, `GET /{$}` dashboard, `GET /settings` + `POST /settings/save` (port, ring size), `GET /frag/log`.
- Table columns: **Received | Severity | Source | Host | Tag | Message**, newest first, with a severity CSS class for colouring (emerg/alert/crit/err = red, warning = amber, notice/info/debug = normal).
- Filter: a text input (`name="q"`, case-insensitive substring over `Raw`+`Hostname`+`Tag`+`Message`) and a minimum-severity `<select>` (`name="sev"`). Both are read as query parameters by `/frag/log` and applied at render time against the ring — **no filter state on the server, no persistence.**
- **The filter must survive the 3s auto-refresh**, which is the one non-obvious htmx detail: the polling container carries `hx-include="[name='q'],[name='sev']"` alongside `hx-get="/frag/log" hx-trigger="every 3s"`, so each poll re-sends the current control values. (This is the sibling AAA Suite's `hx-include` pattern, re-implemented on this project's own ring-buffer + polled-fragment idiom rather than imported.)
- Same duplicated-`{{define}}` hazard as §3.5: the rows block appears in both `dashboard.html` and `log.html` and must be kept identical.
- Add a "Clear" button (`POST /clear`) that empties the ring — cheap, and genuinely useful mid-lab.
- **No persistence beyond the in-memory ring**, matching every other pack. A node restart loses the log; that is the documented, intended behaviour.

### 4.7 build-rootfs.sh — three additive list edits
`runtime/build-rootfs.sh` lines `176` (build loop), `329` (dir reservation loop), and `360` (install loop) each read `for pack in aaa webserver httpclient`. Append `syslog` to all three. The comment at `:172-175` describing the P3 packs should gain a sentence naming the syslog receiver. **Nothing else in the file changes** — no `BASE_INCLUDE` edit (§1: `procps` is already there), no new install block.

Also update the stale palette tooltip at `app/src/lib/components/Palette.svelte:235` — currently `"Network tools — pick RADIUS/AAA, web server, or HTTP client after dropping"` — to mention the syslog collector. This is a **string-only** change; the `{#each}` collapse, `defaultToolPack` (`:23-24`), `onDragStart`, `CanvasInner.buildDroppedNode`, and the Inspector selector are all untouched, and the new pack appears in the Inspector automatically.

### 4.8 Batch C acceptance gate (local)
1. `cd runtime/files/tools/packs/syslog/gui && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...` — green.
2. **Parser table test with real-shaped inputs, asserting field-by-field**, including at minimum: the exact Cisco line from §4.5(4) with the sequence number and `*`-prefixed fractional timestamp; the same line *without* `service sequence-numbers`; a textbook RFC 3164 line with a space-padded day (`Mar  1`); an RFC 5424 line with `STRUCTURED-DATA` present and with `-`; a line with no `<PRI>`; a bare non-syslog string. Every case must yield an `Entry` with `Raw` intact.
3. Receiver test: `ListenUDP` on `:0`, send three datagrams, assert three ring entries with correct `SourceIP` and monotonic `Received`.
4. Ring-cap test: `MaxEntries+10` datagrams → exactly `MaxEntries` retained, oldest dropped.
5. Filter test: `/frag/log?q=…&sev=…` returns only matching rows; an empty `q` returns all.
6. `healthz`-over-AF_UNIX test copied from `radius_test.go:51-89`.
7. Staging-dir test of the three `build-rootfs.sh` edits: `syslog/pack.json` + `syslog-gui` land under `.../packs/syslog/`, mode 0755, ELF linux/amd64, and `gui.bin` matches the installed filename (a mismatch drops the pack from the palette via `manifest.go:43-46`).

---

## 5. Explicit non-goals for the implementing agents
- **Do not touch `radius.go` / `radius_test.go`.** RADIUS shipped, works, and is tested. The merged log is designed (§3.5) specifically so it needs zero edits there.
- **Do not touch `webserver/gui/web.go`.** Its `serve`/`Restart` guards (`web.go:74-80`, `:110-112`) still reject ports < 1024 with the message "requires privileged-port enablement" — which becomes *stale* once Batch A lands. Relaxing it to allow :80 is a real, tempting one-line change and is **explicitly out of scope** (§7 follow-up), because "don't re-touch shipped packs" is worth more here than closing a cosmetic inconsistency in the same PR.
- **Do not touch** the palette `{#each}`/`defaultToolPack` logic, `CanvasInner.svelte`, `Inspector.svelte`, `ToolNode.svelte`, `labStore`, wsbridge, or `manifest.go`. The only frontend change in this entire plan is one tooltip string (§4.7).
- **Do not add per-pack auth.** T2.5 gates `/tool/*` generically.
- **Do not add `CAP_NET_BIND_SERVICE`** anywhere, and do not modify `AllowedCaps` or the setpriv argv.

---

## 6. Live-VM validation checklist (orchestrator only — the plan attempts no VM steps)

Run **§6.1 before §6.2 and §6.3.** If a protocol test fails without §6.1 having passed first, the failure has been diagnosed at the wrong layer.

### 6.1 Option A — prove the knob, at the right scope (gate for everything after)
1. Rebuild/redeploy the rootfs with Batch A. Start any tool node (secbench is fine — this is node-agnostic by §2.4).
2. **Inside the node's netns, the value is 1:**
   `ip netns exec iolt<N> cat /proc/sys/net/ipv4/ip_unprivileged_port_start` → `1`
3. **In the root netns, the value is unchanged from its OWN pre-test baseline — measure, don't assume (corrected per sol-medium finding #15).** Read `cat /proc/sys/net/ipv4/ip_unprivileged_port_start` in the root netns BEFORE starting any tool node (expected to be the distro default, `1024`, but record whatever it actually is — do not hardcode `1024` as the expected value in case this specific image's boot config already changed it) — then re-read it AFTER the tool node starts and assert it is byte-for-byte unchanged from the pre-test reading. **This is the containment proof from §2.5 and is not optional.** A changed root-netns value means the write leaked and Batch A must be reverted immediately.
4. Start a node with a *different* node ID and confirm step 2 holds for it too, and that no other namespace was disturbed.
5. Confirm the supervisor log carries no `tool: command "ip" failed` for the sysctl, and that a node whose sysctl write *would* fail is reported as a start failure (not a silent success) — e.g. by inspecting the error path, not by breaking a live appliance.
6. Regression: secbench, webserver, and httpclient nodes all still start and serve their GUIs with no login prompt.

### 6.2 TACACS+ against a REAL IOL router
Give the `aaa` node a static `eth1` IP via the Inspector. In the GUI, set the shared secret / TACACS+ key and add a lab user with a known password and `PrivLvl 15`.

1. **Bind proof first, protocol second:**
   `ip netns exec iolt<N> ss -lntp` → the pack process listening on `0.0.0.0:49`.
   `ip netns exec iolt<N> ss -lunp` → still listening on `0.0.0.0:1812` (RADIUS unbroken).
   The dashboard shows no bind-error banner.
2. On a peer IOL router on the same fabric:
   ```
   aaa new-model
   tacacs-server host <aaa-eth1-ip> key <secret>
   aaa authentication login default group tacacs+ local
   aaa authorization exec default group tacacs+ local
   ```
3. **Accept:** log in with the valid lab user → succeeds; the GUI's merged log shows a `tacacs+` row with result accept.
4. **Reject:** log in with a wrong password → denied; the log shows a `tacacs+` reject. Confirm the router then falls through to `local` as configured (proving `FAIL`, not `ERROR`, was sent — §3.3).
5. **Privilege level:** after a successful login, `show privilege` on the router reports **15**, proving the AUTHOR `PASS_ADD` + `priv-lvl=15` arg was both sent and understood. Change the user to `PrivLvl 1` in the GUI, re-login, confirm `show privilege` reports 1. *(A login that succeeds but lands at priv 1 when 15 was configured means AUTHOR is broken while AUTHEN works — the exact split this step exists to detect.)*
6. **Wrong key:** temporarily change the router's `key` → login fails and the GUI logs a key/decode rejection rather than a malformed-user attempt.
7. **Accounting:** add `aaa accounting exec default start-stop group tacacs+`, log in and out, confirm start and stop rows appear in the merged log.
8. **RADIUS regression:** repeat the P3 RADIUS check (`radius server LAB` / `address ipv4 … auth-port 1812 acct-port 1813`) and confirm accept/reject still work and appear correctly tagged `radius` in the same merged table.

### 6.3 Syslog from that same IOL router
Give the `syslog` node a static `eth1` IP.
1. **Bind proof first:** `ip netns exec iolt<M> ss -lunp` → listening on `0.0.0.0:514`; no bind-error banner on the dashboard.
2. On the router: `logging host <syslog-eth1-ip>` (plus `logging trap informational`).
3. Generate traffic: `conf t` / `exit` (emits `%SYS-5-CONFIG_I`), then shut/no-shut an interface (emits `%LINK-3-UPDOWN` and `%LINEPROTO-5-UPDOWN`).
4. Confirm the live tail renders each message with a **parsed** Tag (`%SYS-5-CONFIG_I`), correct Source IP, and a plausible severity colour — not a wall of unparsed raw lines. *(Unparsed rows here mean §4.5(4)'s Cisco-format handling is wrong; the messages are arriving, so it is a parser bug, not a network bug.)*
5. Enable `service sequence-numbers` on the router and confirm messages still parse (the optional `NNN:` prefix).
6. Type a filter (e.g. `LINK`) and confirm it narrows the table **and survives the 3s auto-refresh** (the `hx-include` check from §4.6).
7. Set minimum severity to `warning` and confirm informational messages disappear.
8. Overflow: generate more than `MaxEntries` messages, confirm the oldest are dropped and the GUI stays responsive.

### 6.4 Cross-pack end-to-end
One lab: an IOL router + the `aaa` node + the `syslog` node. The router authenticates admin logins against TACACS+ **and** ships its logs to the syslog node — so a failed login attempt is visible in *both* the AAA node's auth log and (via `%SYS-5-LOGIN_FAILED` / AAA logging) the syslog node's tail. All traffic confined to the lab fabric.

Report a per-step verdict table. Any §6.1 failure blocks §6.2/§6.3 entirely — do not attempt protocol tests against a node that cannot bind.

---

## 7. Out of scope — named, not silently dropped
- **TACACS+ PAP / CHAP / MS-CHAP / MS-CHAPv2** (`authen_type` 0x02/0x03/0x05/0x06). Parsed and cleanly `FAIL`ed with a logged reason; not implemented. Follow-up: PAP is the cheapest addition (credentials arrive in the START `data` field, no CONTINUE round-trip).
- **TACACS+ `action` CHPASS (0x02) and SENDAUTH (0x04)**, and the `FOLLOW` (0x21) redirect status on any reply type.
- **Per-NAS TACACS+ keys.** `Config.Clients []Client{Subnet,Secret}` exists in `config.go:11-14` and remains unused by both protocols; one shared key each. Follow-up.
- **TCP syslog (RFC 6587 octet-counting/non-transparent framing) and TLS syslog (RFC 5425).** UDP 514 only, per the owner's "simple" framing.
- **RFC 5424 STRUCTURED-DATA element parsing.** The 5424 *header* is parsed (§4.5(2)); SD is retained as an opaque string.
- **Syslog persistence, rotation, relaying/forwarding, and export/download.** In-memory ring only, matching every other pack.
- **Relaxing `webserver`'s `< 1024` port guard to allow :80** now that Option A makes it bindable (`web.go:74-80`, `:110-112`, and the stale warning text in its settings template). Deliberately deferred — see §5. This is the most likely thing a reviewer will want folded in; it should be its own small change with its own live gate.
- **New icon glyphs** (`shield` for AAA, `globe` for HTTP client, a document/list glyph for syslog) in `app/src/lib/icons.svelte.ts` — still the optional polish P3 decision 10 flagged.
- **Removing the checked-in `gui.exe` build artifacts** from `aaa/gui/` and `webserver/gui/`.

---

### Critical files for implementation
- J:\Claude code\iolab\supervisor\internal\tool\netns.go (`netnsCreateNetnsCmds` at :15-17 — the one-line Option A insertion; :29 is the netns-exec cmdSpec idiom to copy)
- J:\Claude code\iolab\supervisor\internal\tool\netns_test.go (`TestNetnsCreateSequence` at :8-33 — the `want` slice to extend, and the escape-guard pattern to replicate for the sysctl)
- J:\Claude code\iolab\runtime\files\tools\packs\aaa\gui\radius.go (READ ONLY — `AuthAttempt`/`attemptRing`/`newRing` at :45-79 are reused verbatim by TACACS+; the codec's shape at :143-194 is the model for `tacacs.go`)
- J:\Claude code\iolab\runtime\files\tools\packs\aaa\gui\server.go (`App`/`NewApp` :17-24, `routes()` :26-41, `logFragment` :108-110 — where the second listener and the merged log wire in)
- J:\Claude code\iolab\runtime\files\tools\packs\aaa\gui\main.go (:24-28 — the listener-goroutine shape to duplicate for TACACS+ and to copy into the syslog pack, plus the bind-error visibility fix)
- J:\Claude code\iolab\runtime\build-rootfs.sh (:176, :329, :360 — the three `for pack in aaa webserver httpclient` lists that gain `syslog`; :238 `BASE_INCLUDE` confirmed already containing `procps`)
