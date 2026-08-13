# IOL own-interface MACs via `show interfaces`: implementation plan

Status: planning only. This document is grounded in the working tree inspected on 2026-08-12. It intentionally proposes no traffic-path, console, parser, or UI implementation in this pass. Line numbers below refer to that inspected working tree and should be rechecked immediately before editing because several files already contain unrelated uncommitted work.

## Outcome and decision summary

Change the on-demand **MAC addresses** popover so an IOL node reports the effective MAC address IOS itself assigns to each Ethernet interface. Obtain one live console snapshot with:

```text
show interfaces | include ^[A-Za-z].* is |Hardware is .*address is
```

This is the IOS-side-filtered form of `show interfaces`: it retains each interface header and the corresponding hardware-address line while avoiding the rest of the long counters/configuration dump. The new parser must also accept unfiltered `show interfaces` output so it is independently robust and easy to fixture-test.

Parse blocks shaped like this operator-observed IOL output:

```text
Ethernet0/0 is up, line protocol is up
  Hardware is AmdP2, address is aabb.cc00.0800 (bia aabb.cc00.0800)
```

Use the address after `address is`, not the value after `bia`. The former is the interface's current/effective address and therefore reflects a configured MAC override; `bia` is only the burned-in address.

The authoritative console result must **replace**, not fall back to or merge with, the traffic-learned IOL path. Reuse `Source: "read"`: both the existing netns path and the new console path are direct reads of the node's own current address. Do not add `"configured"`, because that would force needless protocol/frontend churn while implying the value necessarily came from startup configuration rather than live operational state.

If the IOL node is stopped, has no console, the console is busy/wedged, or IOS omits an address for an interface, return that interface as `state: "unknown"` with a truthful reason. Never substitute dirstat data: the existing IOL classifier branch deliberately reads the **peer** endpoint's bucket to answer “what is attached to this port,” not the queried IOL node's own MAC (`supervisor/internal/server/handlers.go:1369-1408`). A learned fallback would therefore return the wrong device's address under the label “node MAC.”

## Verified current design

### Current `node.macs` path

- The server registers `node.macs` to `handleNodeMACs` (`supervisor/internal/server/server.go:215-243`). Every registered handler is wrapped by `serializedHandler`, which holds `s.labMu` for the call (`supervisor/internal/server/server.go:255-260`). This matters because an IOL console read can occupy the serialized control path for up to the existing eight-second `runShow` timeout; the popover must remain a one-shot request, not become a poll.
- `handleNodeMACs` decodes `NodeMACsArgs`, selects the current lab, finds the document/runtime node, and enumerates `lab.Interfaces(*n)` (`supervisor/internal/server/handlers.go:1271-1287`).
- VPCS uses the supervisor-known formula and returns `Source: "derived"` (`supervisor/internal/server/handlers.go:1288-1295`). PC/tool nodes call `readNetnsMAC` only while running and return `Source: "read"` (`supervisor/internal/server/handlers.go:1296-1309`); `readNetnsMAC` reads `/sys/class/net/<GuestIface>/address` and normalizes it to lowercase (`supervisor/internal/server/handlers.go:1426-1444`).
- IOL currently has no own-address read. It first creates disabled rows and exits unless `args.Learned` is true (`supervisor/internal/server/handlers.go:1310-1323`). When enabled, it copies dirstat classifiers (`supervisor/internal/server/handlers.go:1325-1333`), maps rows through the lab's links (`supervisor/internal/server/handlers.go:1335-1367`), deliberately examines the peer attribution bucket (`supervisor/internal/server/handlers.go:1369-1394`), and labels a single observed value `Source: "learned"` (`supervisor/internal/server/handlers.go:1395-1408`). Thus an unlinked interface starts as “no link,” and a linked interface still needs observed traffic.
- The Go wire contract says `learned` is the opt-in and defines `derived`, kernel `read`, and traffic `learned` provenance (`supervisor/internal/protocol/verbs.go:182-217`). The protocol documentation repeats those semantics (`docs/protocol.md:137-154`). The TypeScript contract accepts exactly those three source strings (`app/src/lib/protocol.ts:209-219`).
- The popover performs one `nodeMACs(nodeId, false)` call on mount and renders known rows as dotted MACs plus their source, otherwise their reason (`app/src/lib/components/MacListPopover.svelte:30-43`, `app/src/lib/components/MacListPopover.svelte:57-72`). It does not special-case a particular source, so `read` needs no rendering change.
- `SupervisorClient.nodeMACs` currently folds the persistent `macUiStore.learnIol` preference into the request (`app/src/lib/supervisor.ts:201-209`). The store is solely a local-storage-backed learned-IOL display preference (`app/src/lib/macUiStore.svelte.ts:1-37`), and Settings exposes it as “Detect IOL MAC addresses / Infer IOL addresses from observed live traffic” (`app/src/lib/components/SettingsDialog.svelte:79-85`). The dev mock similarly gates synthetic IOL `Source: "learned"` rows on the request flag (`app/src/lib/mockTransport.ts:354-404`).

### Existing console-scrape primitive

- Linux `runShow` accepts a context, loaded lab, node ID, and one command. It requires a runtime/process in `StateRunning`, wraps the operation in an eight-second timeout, and calls `nr.proc.RunExec`; stopped/no-console cases return errors (`supervisor/internal/server/painter_linux.go:15-20`, `supervisor/internal/server/painter_linux.go:22-59`). The non-Linux stub returns an explicit unsupported/not-loaded error so the common server package cross-compiles (`supervisor/internal/server/painter_other.go:11-16`).
- The command is run through the node's existing console hub, not a second telnet connection. `consoleHub.RunExec` claims the input-arbitration turn, subscribes to the hub's already-decoded output, feeds a `consolescript.Session`, and releases the turn afterward (`supervisor/internal/node/console_hub.go:589-628`). Interactive keystrokes queue rather than interleave.
- `consolescript.Session.RunExec` synchronizes the prompt, enters enable mode when needed, uses `do` without ejecting an interactive user from config mode, runs `terminal length 0`, captures until the prompt returns, and calls `CleanShowOutput` (`supervisor/internal/consolescript/consolescript.go:145-230`). `CleanShowOutput` normalizes CR/LF, removes the echoed command and trailing prompt, and trims blank edges (`supervisor/internal/consolescript/consolescript.go:233-256`). This makes one filtered `show interfaces` command preferable to one command per interface: one claimed turn, one consistent snapshot, and no pager.
- The supplied research pointer placed the tests in `server_test.go`, but the verified working tree places them in `supervisor/internal/server/painter_linux_test.go`. `fakeIOSPty` provides an end-to-end hub/console-script harness (`supervisor/internal/server/painter_linux_test.go:17-83`); `TestRunShowReturnsCleanOutput` checks echo/prompt cleaning (`supervisor/internal/server/painter_linux_test.go:86-104`), `TestRunShowNotRunningNode` checks stopped-node failure (`supervisor/internal/server/painter_linux_test.go:106-122`), and `TestRunShowUnderConcurrentInteractiveWrite` checks turn arbitration (`supervisor/internal/server/painter_linux_test.go:124-151`).

### Existing painter data flow and parser style

- The frontend calls `painter.stpVlans` and `painter.collect` through `SupervisorClient` (`app/src/lib/supervisor.ts:288-307`). `painterStore.detectVlans` stores the VLAN-discovery response (`app/src/lib/painterStore.svelte.ts:125-149`), and `painterStore.paint` stores the one-shot painter result (`app/src/lib/painterStore.svelte.ts:169-215`).
- The server registers the two painter verbs beside `node.macs` (`supervisor/internal/server/server.go:242-251`). Their handlers validate input, select the lab, and call the platform-specific collector with `context.Background()` (`supervisor/internal/server/handlers.go:1679-1722`).
- For STP, `collectNode` runs `show spanning-tree vlan <N>`, calls `painter.ParseSTPVlanBlock`, maps the pure parser result into protocol structs, and places it on the node response (`supervisor/internal/server/painter_collect_linux.go:86-105`, `supervisor/internal/server/painter_collect_linux.go:205-228`). VLAN discovery separately runs `show spanning-tree` and best-effort `show vlan brief`, then calls `painter.ParseSTPVlans` (`supervisor/internal/server/painter_collect_linux.go:231-265`).
- The stored result reaches the canvas through reactive consumers: `FloatingEdge` asks `painterStore` for per-endpoint badges and link paint (`app/src/lib/edges/FloatingEdge.svelte:210-252`), while `IolNode` derives the STP-root crown from the same store (`app/src/lib/nodes/IolNode.svelte:7-15`). This is the end-to-end pattern to copy conceptually: verb → `runShow` → pure parser → protocol result → frontend store/component.
- `stp.go` is the closest parser template. `splitSTPBlocks` splits a multi-section CLI dump before field parsing and tolerates blank/error preamble (`supervisor/internal/painter/stp.go:65-99`). Exported `ParseSTP` and `ParseSTPVlanBlock` have comments that name the exact commands/layout and return an empty result for no data (`supervisor/internal/painter/stp.go:145-173`); `parseSTPBlock` scans stanza state and table fields (`supervisor/internal/painter/stp.go:176-281`). `ParseSTPVlans` reuses the same blocks (`supervisor/internal/painter/stp.go:284-314`).
- The other parsers follow the same pure, tolerant string-to-struct approach: `ParseOSPFNeighbors` and `ParseOSPFRoute` document and scan table/route lines (`supervisor/internal/painter/ospf.go:49-133`), `ParseEIGRPTopology` maintains and flushes descriptor blocks (`supervisor/internal/painter/eigrp.go:39-112`), and `ParseBGP` maintains and flushes path blocks (`supervisor/internal/painter/bgp.go:41-118`). Shared loose interface recognition accepts Ethernet and Serial spellings (`supervisor/internal/painter/match.go:43-62`), while numeric helpers return zero rather than propagating fixture-noise errors (`supervisor/internal/painter/num.go:8-28`).
- Painter's package contract is currently pure and device-I/O-free so fixtures run on every platform (`supervisor/internal/painter/painter.go:1-12`). Adding the MAC parser here should preserve that separation; broaden the package comment from “Topology Painter only” to include other live IOS facts rather than putting console I/O into the parser package.
- `stp_test.go` keeps realistic raw IOS constants at file scope, then uses table-driven cases for normal, error, and empty input (`supervisor/internal/painter/stp_test.go:5-59`) and asserts structured fields in subtests (`supervisor/internal/painter/stp_test.go:60-91`). Follow that fixture shape for interface MACs.

### IOL interface inventory and naming

- `lab.Node` represents IOL Ethernet and Serial adapter-group counts; each group is documented as four ports (`supervisor/internal/lab/lab.go:60-77`). Lab link endpoints use short `e0/0` / `s1/1` spellings (`supervisor/internal/lab/lab.go:155-161`).
- `lab.Interfaces` intentionally enumerates from adapter counts rather than links so unconnected ports appear in the MAC list (`supervisor/internal/lab/interfaces.go:5-31`). It returns canonical short names through `netmap.Iface.String()`.
- `netmap.ParseIface` already accepts both short and IOS-long forms (`e0/0`, `Ethernet0/1`, `s1/2`, `Serial0/3`) and returns typed adapter/port coordinates (`supervisor/internal/netmap/netmap.go:119-179`). `IfacesForCounts` orders Ethernet then Serial and produces four ports per group (`supervisor/internal/netmap/netmap.go:181-203`). Use this canonicalizer at the server mapping boundary instead of inventing string replacements.
- IOL 17.18.02 was verified by this repository to expose `e0/0..e0/3` per Ethernet group and `s0/0..s0/3` per Serial group (`supervisor/internal/netmap/netmap.go:32-41`). Static NETMAP entries are created per interface independently of whether a link exists (`supervisor/internal/netmap/netmap.go:206-240`).
- Existing inconsistency to avoid broadening accidentally: `lab.Interfaces` defaults a nil Serial count to zero (`supervisor/internal/lab/interfaces.go:17-26`), while `buildSpec` currently defaults a nil Serial count to one (`supervisor/internal/server/handlers.go:1038-1057`). This plan does not resolve that separate inventory-default bug. Parser tests must include Serial text, and handler tests that expect Serial rows must set `Node.Serial` explicitly.

## Implementation plan

### 1. Add a pure IOS interface-MAC parser

Create `supervisor/internal/painter/interface_macs.go` and `interface_macs_test.go`.

Proposed public shape:

```go
// InterfaceMAC is one IOS interface whose current hardware address was present.
type InterfaceMAC struct {
    Interface     string
    InterfaceNorm string
    MAC           string // lowercase colon-separated
}

// ParseInterfaceMACs parses full or header/address-filtered `show interfaces`
// output from IOS/IOL and returns interfaces that contain a valid current
// "Hardware is ..., address is ..." value.
func ParseInterfaceMACs(out string) []InterfaceMAC
```

Implementation details:

1. Normalize CRLF per line with `strings.TrimRight(raw, "\r")`, matching the painter parsers.
2. Split the stream into interface blocks. Recognize an unindented header with a compiled regex whose first capture is the interface token and whose remainder accepts at least these state forms:
   - `Ethernet0/0 is up, line protocol is up`
   - `Ethernet0/1 is down, line protocol is down`
   - `Ethernet0/2 is administratively down, line protocol is down`
   - optional trailing status such as `(connected)`
   - tolerant additional physical states such as `reset`, without interpreting the state
3. Start a new block on each header and flush the preceding block, mirroring `splitSTPBlocks` / the EIGRP and BGP flush closures. Ignore address-like lines before the first valid header so data can never be attributed to the wrong interface.
4. Within a block, match the exact IOL hardware line with a compiled field regex conceptually equivalent to:

   ```text
   ^\s*Hardware is .*, address is ([0-9A-Fa-f.:-]+)(?:\s+\(bia\s+[0-9A-Fa-f.:-]+\))?
   ```

   Extract capture 1 only. The hardware description is intentionally unconstrained (`AmdP2` is known, other IOL images may differ).
5. Validate/normalize exactly 48 MAC bits. Accept IOS dotted (`aabb.cc00.0800`) and, defensively, colon/hyphen forms; emit `aa:bb:cc:00:08:00` to satisfy the existing Go protocol contract (`supervisor/internal/protocol/verbs.go:214-219`). Reject malformed, broadcast, all-zero, or non-6-byte values rather than returning unlicensed data. Preserve locally administered/unicast values because configured lab MACs may intentionally use them.
6. Return at most one record per block, using the first valid `address is` line. Keep raw `Interface` plus lowercase/trimmed `InterfaceNorm` in the established painter style. The server will do the stronger IOL-specific `netmap.ParseIface` conversion.
7. Skip blocks with no address line. That includes Serial interfaces whose hardware stanza may not expose an IEEE MAC, loopbacks, incomplete output, and unusual virtual interfaces. Empty input and `% ...` CLI errors return an empty slice, never panic.
8. Update `supervisor/internal/painter/painter.go:1-12` package documentation just enough to say the package parses pure live IOS CLI facts for the Topology Painter and other supervisor features. Do not add server/node imports or device I/O.

### 2. Replace the IOL branch in `handleNodeMACs`

In `supervisor/internal/server/handlers.go`:

1. Add the pure `internal/painter` import. Once the learned branch is removed, also remove the now-unused `internal/dirstat` import; its only uses in this file are the current IOL MAC branch (`supervisor/internal/server/handlers.go:1329`, `supervisor/internal/server/handlers.go:1343`, `supervisor/internal/server/handlers.go:1384`). Do not remove the classifiers themselves: Protocol Lens learning remains a separate feature.
2. Build one `protocol.NodeMAC` row per `lab.Interfaces(*n)` entry first, preserving deterministic lab spelling/order and ensuring ports with no link remain visible.
3. For `KindIOL`, check `nr.proc != nil` and `nr.machine.State() == node.StateRunning` before scraping. If not running, return all inventory rows as `unknown`, reason `node not running`. This agrees with `runShow`'s existing precondition (`supervisor/internal/server/painter_linux.go:36-46`) and with the PC/tool behavior (`supervisor/internal/server/handlers.go:1296-1307`).
4. Call exactly once:

   ```go
   out, err := s.runShow(context.Background(), ll, args.Node,
       "show interfaces | include ^[A-Za-z].* is |Hardware is .*address is")
   ```

   `context.Background()` matches existing painter handlers (`supervisor/internal/server/handlers.go:1701-1705`, `supervisor/internal/server/handlers.go:1718-1722`); `runShow` adds its own eight-second bound. If future protocol plumbing exposes a request context, pass it instead, but do not create a second timeout policy in this feature.
5. On `runShow` error, keep every row unknown with a concise UI reason such as `MAC unavailable (console busy or unreachable)`. Do not return an RPC error for a normal live-read miss: current `node.macs` returns per-row states for stopped/unavailable PC/tool reads (`supervisor/internal/server/handlers.go:1296-1309`), and the popover already renders those reasons inline (`app/src/lib/components/MacListPopover.svelte:64-69`).
6. Parse with `painter.ParseInterfaceMACs(out)`. For each parsed record, call `netmap.ParseIface(record.Interface)`, then use `Iface.String()` to obtain the canonical `eA/P` or `sA/P` lookup key. Ignore unsupported extra IOS interfaces (Loopback, Vlan, Port-channel) and parsed interfaces not present in the lab inventory. This maps `Ethernet0/0` safely onto row `e0/0` using existing code (`supervisor/internal/netmap/netmap.go:131-179`).
7. For a matched valid row, set `MAC`, `Source: "read"`, and `State: "known"`. For a remaining Ethernet row, use `State: "unknown"` and `Reason: "hardware address not reported by IOS"`. For Serial, use the more accurate `Reason: "interface has no IEEE MAC address"` unless a specific image actually reports a valid address, in which case accept it.
8. Remove the entire `args.Learned`/dirstat/link-attribution branch. The popover is about the selected node's own interface addresses; the peer-bucket behavior is semantically incompatible and topology/traffic dependent (`supervisor/internal/server/handlers.go:1369-1408`).

### 3. Update the protocol contract without adding a new source value

In `supervisor/internal/protocol/verbs.go`, `docs/protocol.md`, and `app/src/lib/protocol.ts`:

- Remove `NodeMACsArgs.Learned`; `NodeMACsArgs` only needs `Node`. Go's default JSON decoding ignores an obsolete `learned` field from an older GUI, so this is wire-compatible during a mixed-version transition.
- Redefine `read` as “directly read from the running node/runtime” with two documented mechanisms: netns kernel read for PC/tool, IOS console `show interfaces` for IOL. Keep `derived` for VPCS and `learned` only if some other public response still uses it; if `NodeMAC.source` is exclusive to `node.macs` (it currently is), remove `learned` from both Go comments and the TypeScript union after confirming no external consumer relies on it.
- Remove `disabled` from the documented `node.macs` state semantics and the TypeScript union if no other response uses it. Keep `ambiguous` only if retained for another valid node-own-MAC mechanism; otherwise simplify the contract to `known | unknown`. A conservative compatibility option is to leave the broader TS union temporarily but ensure the new server never emits `ambiguous`/`disabled` for IOL.
- Keep `Source: "read"`, so `MacListPopover.svelte` and the current TypeScript source union need no positive addition.
- Rewrite `docs/protocol.md:137-154` to state that IOL reads require a running, console-reachable node, use a one-shot IOS scrape, include unlinked Ethernet interfaces, and yield per-row unknown reasons on failure.

### 4. Remove the obsolete learned-IOL UI preference

The authoritative read should be automatic whenever the user opens the popover; it is no longer traffic inference and should not require opt-in.

- Change `app/src/lib/components/MacListPopover.svelte:30-34` to call `nodeMACs(nodeId)` without the obsolete boolean. No render/layout change is otherwise needed because `app/src/lib/components/MacListPopover.svelte:64-69` already displays `source: "read"` and per-row reasons generically.
- Simplify `SupervisorClient.nodeMACs` (`app/src/lib/supervisor.ts:201-209`) to send `{node}` and remove its `macUiStore` import.
- Remove the “Detect IOL MAC addresses” setting and import from `SettingsDialog.svelte` (`app/src/lib/components/SettingsDialog.svelte:79-85`). Delete `app/src/lib/macUiStore.svelte.ts` after confirming the references listed above are gone. The old local-storage key can be left inert; no migration is necessary.
- Update `app/src/lib/mockTransport.ts:354-404`: a running IOL should return deterministic `source: "read"` addresses for Ethernet rows regardless of a request flag; a stopped IOL should return unknown/`node not running`; explicit Serial rows should be unknown/no IEEE MAC unless the mock fixture intentionally models an address. This keeps dev mode representative of the real UI.
- Search the entire repository for `learnIol`, `mac.learnIol`, `learned-MAC display`, `args.Learned`, and `source: "learned"`; remove or rewrite only references tied to the popover. Do not disable or delete dirstat learning used by Protocol Lens.

## Regression tests

### Pure parser fixtures (`supervisor/internal/painter/interface_macs_test.go`)

Use file-scope raw constants and table-driven subtests like `supervisor/internal/painter/stp_test.go:5-91`.

Required fixtures/assertions:

1. **Observed IOL happy path:** exact `Ethernet0/0 is up...` plus `Hardware is AmdP2, address is aabb.cc00.0800 (bia ...)`; expect `Ethernet0/0` and `aa:bb:cc:00:08:00`.
2. **Multiple blocks:** several Ethernet interfaces with distinct MACs; verify order and no cross-block leakage.
3. **Administrative/link down:** include `is administratively down, line protocol is down` with a valid hardware line; it must still return the MAC. This is the critical unconnected/no-traffic case.
4. **Configured override:** `address is aabb.cc00.0801 (bia aabb.cc00.0800)`; expect `...:01`, proving the parser uses current address rather than BIA.
5. **L2/L3 spelling variance:** headers with optional trailing `(connected)` and status variants; both full `Ethernet0/0` and abbreviated `Et0/0` should parse if emitted by an image.
6. **Serial/no address:** a `Serial0/0` block with `Hardware is ...` but no `address is`; expect no parser record and no accidental reuse of the prior Ethernet MAC.
7. **Unrelated logical interfaces:** Loopback/Vlan/Port-channel blocks may be parsed only if they have a valid address, but the server-mapping test must prove they are ignored because they are not in `lab.Interfaces`/`netmap.ParseIface`'s IOL physical inventory.
8. **Malformed/missing data:** invalid hex, short/long MAC, all-zero, broadcast, hardware line before any header, header with no hardware line, empty input, and `% Invalid input detected`; expect safe omission/empty results.
9. **Transport text variance:** CRLF, leading/trailing blank lines, uppercase hex, and full unfiltered blocks with counters between header and hardware line.
10. **Filtered fixture:** exactly the two-line-per-interface output produced by the proposed command, proving the parser does not depend on omitted counter lines.

### Server/console integration (`supervisor/internal/server/painter_linux_test.go`)

Extend the existing `fakeIOSPty` harness rather than introducing a second console implementation. Give it a command-to-output map (and optionally a recorded-command slice) so `show interfaces | include ...` emits realistic fixture text before the prompt; retain existing default behavior so the three current `runShow` tests continue unchanged (`supervisor/internal/server/painter_linux_test.go:17-151`).

Add:

1. `TestNodeMACsReadsIOLShowInterfaces`: create a running IOL node with explicit Ethernet/Serial counts, attach it as `s.lab`, call `handleNodeMACs` with raw `{ "node": 1 }`, and assert:
   - the exact filtered command ran once;
   - echo and prompt never reach the parser result;
   - IOS `Ethernet0/0` maps to lab row `e0/0`;
   - MAC is lowercase colon-separated, state is known, source is read;
   - an admin-down/unlinked Ethernet fixture still returns known;
   - Serial/missing-address rows remain present and unknown;
   - extra IOS logical interfaces are ignored.
2. `TestNodeMACsIOLNotRunning`: mirror `TestRunShowNotRunningNode`; assert all inventory rows are unknown with `node not running` and no command is attempted.
3. `TestNodeMACsIOLConsoleUnavailableDoesNotUseLearnedFallback`: arrange a running state with no usable hub or force `runShow` failure; even if a dirstat classifier/link fixture contains a single peer MAC, assert no returned row uses it and every affected row is unknown. This locks the semantic boundary that motivated replacement.
4. If the fake supports it cheaply, `TestNodeMACsUnderConcurrentInteractiveWrite`: call the handler while an interactive byte is queued, mirroring `TestRunShowUnderConcurrentInteractiveWrite`, and assert it does not contaminate a parsed interface/MAC. The existing runShow test already covers the primitive, so this is useful but secondary to the handler mapping test.

Also update/add frontend unit tests if this app's current test harness covers stores/transports: `SupervisorClient.nodeMACs` sends no `learned` field, the mock returns IOL `read` data without an opt-in, and the popover displays a stopped-node reason. Do not add polling/timer tests because the component remains one-shot (`app/src/lib/components/MacListPopover.svelte:30-43`).

### Verification commands

Run from the repository root after implementation:

```text
go test ./supervisor/internal/painter
go test ./supervisor/internal/server
go test ./supervisor/...
```

Then run the app's existing typecheck/test commands as declared in `app/package.json`, and cross-compile/test the supervisor for the repository's supported non-Linux target to ensure the common handler still works with `painter_other.go`'s `runShow` stub (`supervisor/internal/server/painter_other.go:1-16`). Do not guess command names; inspect the scripts at implementation time.

## Runtime acceptance check on real IOL

1. Start an IOL node with at least two Ethernet interfaces, leave one administratively down/unlinked, and ensure neither interface has generated traffic.
2. On the real console, run both full `show interfaces` and the proposed filtered command. Capture the exact output as a permanent test fixture. Confirm the filter preserves every physical interface header and every `Hardware is ..., address is ...` line.
3. Open the GUI MAC popover. Verify each Ethernet row matches the value after `address is`, including the unlinked/admin-down interface. It should show `read`, not `learned`.
4. Configure an interface MAC override if supported by the image and repeat; verify the GUI follows `address is` while BIA remains unchanged.
5. Stop the node and reopen the popover; rows should remain listed but report `node not running`.
6. Leave a web/native console open and type while opening the popover; the scripted command may scroll in that same console by design (`supervisor/internal/server/painter_linux.go:25-34`), but typing/output must not interleave.

## Why unconnected interfaces are the key improvement

The current traffic path cannot know a node's own address without traffic and initially describes an unlinked port as “no link on this interface” (`supervisor/internal/server/handlers.go:1335-1340`), then “no traffic seen yet” when a link exists but no source has been observed (`supervisor/internal/server/handlers.go:1395-1409`). It is also looking at peer-originated traffic, not the IOL interface itself (`supervisor/internal/server/handlers.go:1369-1394`).

By contrast, `show interfaces` enumerates IOS's interface inventory and reports the hardware/current address as an interface property alongside operational state. An Ethernet interface can be `administratively down` or have line protocol down and still own that address; the parser explicitly treats state as metadata, not as a gate. The repository already models the same topology independence: `lab.Interfaces` includes ports from configured adapter counts rather than links (`supervisor/internal/lab/interfaces.go:5-31`), and static NETMAP entries exist per interface before links are drawn (`supervisor/internal/netmap/netmap.go:206-240`). The real-IOL acceptance fixture for an admin-down/unlinked port is mandatory because it proves the user-visible advantage rather than merely assuming it.

## Risks and guardrails

- **Long/slow console output:** use the IOS-side filtered command and one runShow turn. The existing eight-second timeout bounds a wedged session (`supervisor/internal/server/painter_linux.go:15-20`). Because all handlers hold `s.labMu` (`supervisor/internal/server/server.go:255-260`), do not poll or retry synchronously inside the handler.
- **CLI regex/version variance:** validate the exact filter on every supported IOL image class. If an image's pipe regex differs, prefer falling back to one unfiltered `show interfaces` call, not N per-interface calls and never traffic inference. The parser accepts both filtered and full text.
- **Console mode/password:** `consolescript` handles user EXEC and config-mode `do` without disrupting the user, but a configured enable password or unavailable prompt can time out (`supervisor/internal/consolescript/consolescript.go:145-191`). Report unknown rows honestly.
- **Interface-name mismatch:** canonicalize only with `netmap.ParseIface`; do not compare `Ethernet0/0` directly to `e0/0` (`supervisor/internal/netmap/netmap.go:131-179`). Ignore logical/unknown interfaces rather than creating rows not present in the lab model.
- **Serial interfaces:** HDLC/PPP Serial interfaces generally do not have IEEE MACs. Preserve their inventory rows when configured, but do not invent or derive a MAC. A missing `address is` is an expected per-row unknown, not a whole-command failure.
- **BIA vs current address:** always select `address is`; add the differing-BIA fixture so a future refactor cannot regress to the burned-in value.
- **Partial output:** block association prevents a hardware line from leaking to the previous/next interface. A successfully parsed subset should populate only those rows; all others stay unknown.
- **Stale traffic learner:** leave dirstat operational for Protocol Lens, but remove it from `node.macs`. No learned value may be used as an own-address fallback.
- **Existing dirty worktree:** `handlers.go`, protocol files, and other files already contain unrelated changes. The implementation pass must inspect `git diff` first and apply surgical patches without reverting or overwriting those edits.

## Definition of done

- A running IOL popover reports valid own Ethernet MACs from one filtered `show interfaces` scrape as `source: "read"`.
- Admin-down/unlinked Ethernet interfaces report their MAC without requiring a link or any traffic.
- Stopped/unreachable/no-address cases stay visible as truthful unknown rows.
- No `node.macs` path reads dirstat or emits a peer/learned MAC.
- The learned-IOL opt-in setting/store/request field and dev-mock behavior are removed or retired consistently.
- Parser fixtures cover raw and filtered IOS output, state/name variance, differing BIA, malformed/partial data, and Serial/no-address blocks.
- Linux handler integration tests exercise the existing runShow/console-hub harness, and existing runShow concurrency/cleaning tests still pass.
- Protocol docs and Go/TypeScript contracts describe the same direct-read semantics.
