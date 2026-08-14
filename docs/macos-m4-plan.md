# M4 implementation plan v2 (final) — remaining runtime behavior and capacity qualification

Plan revision pass: sol, medium reasoning. Implementation/qualification pass:
luna, xhigh reasoning. This is the final reviewed plan. During implementation
this file is a frozen input, not an implementation output.

This plan follows, in precedence order, `docs/macos-m3-handoff.md`,
`docs/macos-m3-result.md`, `docs/macos-m1-handoff.md`, and the immutable M4
section of `docs/macos-arm64-plan.md`.

## Outcome and governing rules

M4 is an evidence-first Apple Silicon hardware qualification. It is complete
only when items 1–7 meet their applicable hardware bars, item 8 is derived
from those raw records, every inherited-requirement gate passes, and the
independent record verifier exits zero. Compilation, unit tests, commit
`9916fb9`, Lima/systemd/node liveness, and an NDJSON reply containing
`ok:true` are not hardware acceptance.

The normal implementation change is an M4 test/evidence harness, fixtures, and
`docs/macos-m4-result.md`. Product/runtime files may change only after a
hardware failure is preserved and reduced to that code: make the smallest
fix, add a focused regression, and rerun the failed hardware item and every
affected earlier item. Do not redesign the profile model, port contract, sync
engine, browser-equivalent HTTP/WS flow, guest provisioning, or launcher
lifecycle.

An item is `PASS`, `FAIL`, `BLOCKED`, or `UNVERIFIED`; item 4 alone may be
`NOT_EXERCISABLE` under the exact host-interface rule below. A preserved and
escalated four-node RAM wall is honest `BLOCKED/UNVERIFIED` and leaves M4
`INCOMPLETE`; it is never an alternate completion bar.

## Preflight, ownership, and evidence contract

Before consuming Mac state, the implementation must:

1. Work from a fresh branch/worktree off the recorded current
   `luna/macos-m3-ux` base unless the owner explicitly directs otherwise.
   Record the base commit, `git log --oneline -5`, `git status --short`, and
   the initial `git diff --name-status`. Reconcile `9916fb9` and newer
   concurrent changes. Never use `git add -A` or `git add .`.
2. Freeze this plan: record its SHA-256, set `docs/macos-m4-plan.md` read-only
   for the implementation session (on Windows,
   `(Get-Item docs/macos-m4-plan.md).IsReadOnly = $true`), and make the final
   verifier require the same hash and no diff for this path.
3. Use only `iolbox-m4-e2e` or another explicitly owner-approved disposable
   M4 VM. Every wrapper must reject the exact name `iol22` before any start,
   stop, reset, reuse, archive, or delete operation. `iol22` is irreplaceable
   M0 evidence and is never touched.
4. Generate an opaque run identifier such as
   `m4-<UTC>-<host-pid>-<random>` before the first probe. Put it in the
   evidence root, harness environment, fixture metadata where supported, and
   every attempt record. Capture each returned lab ID and node/link ID from
   control responses; never infer ownership from a name alone.
5. Create `~/iolbox-m1/evidence-m4/<run-id>/` with immutable per-phase and
   per-attempt subdirectories. Every command record contains command/argv,
   cwd, start/end UTC, monotonic start/end when relevant, stdout, stderr, exit
   status, and SHA-256. Preserve consoles, control/event streams, pcapng,
   metrics CSV/NDJSON, inventories, journals, and a human README. Hardware,
   compile/unit, and static-analysis evidence live under distinct roots.
6. Record `sw_vers`, `uname -a`, `sysctl -n hw.memsize`, `sysctl -n hw.ncpu`,
   Bash version, `/opt/homebrew/bin/limactl --version` and machine/network
   inventory, disk availability, exact source revision, and hashes of the
   Darwin launcher/test binaries, IOL image, fixtures, and packaging assets.
   Use `/opt/homebrew/bin/limactl` explicitly; Mac Bash is 3.2.57.
7. Before any VM creation/start, record `top -l 1 -s 0 | grep PhysMem`,
   `memory_pressure -Q`, `sysctl vm.swapusage`, and `vm_stat`. The `top`
   PhysMem result is the reclaimable-memory gate; raw `vm_stat` free pages are
   not. Also inventory exact VM name/state, config path/hash, disk identity and
   size, Lima/VZ PIDs and RSS, and an owner/session marker.
8. Build/stage the Darwin arm64 launcher and opt-in test in the established
   way. Run ordinary launcher tests, `go vet`, Darwin arm64 build/test compile,
   Windows amd64 build, and Windows tests, but store these only as
   compile/unit evidence.

### Per-VM preservation before reuse, reset, or deletion

This applies to every disposable VM, including a stopped M3 passing machine.
Before reuse, reset, or deletion, an exact-name preservation wrapper must:

1. Refuse `iol22`, the active M4 VM when deletion was requested, unresolved
   names, and glob/pattern input. Record current state, owner/session marker,
   Lima configuration, disk path/identity/size/hash where practical, and
   Lima/VZ PIDs/RSS.
2. Inventory and archive relevant guest-local acceptance evidence, labs,
   images, image executable mode, data/persistence sentinels, and service
   state into `vm-preservation/<machine>/<UTC>/`. If the guest is stopped,
   use a read-only or owner-approved safe inspection path; do not start a VM
   owned by another run merely to archive it.
3. Inventory the pre-existing host evidence directories for that machine and
   record their file hashes without rewriting them. Copy the guest archive to
   the host, write a SHA-256 manifest, then independently read back and verify
   every copied file and the manifest. Record the final VM state.
4. Refuse reuse/reset/delete and escalate if ownership is unclear or archive
   verification fails. A stopped VM is never deleted for RAM reclamation.
   Delete is allowed only for demonstrated disk pressure or a verified
   still-resident Lima/VZ allocation, through the exact-name wrapper, after
   preservation verifies and the action is recorded.

### Run ownership map and cleanup attribution

For every item, take a before snapshot and create
`ownership-map.<phase>.json` mapping the run ID and returned lab ID to node and
link IDs, exact PIDs/PPIDs/commands, interfaces/bridges/taps, listeners,
routes, namespaces/veth/cgroups, subnets, and iptables rules/counter identity.
Corroborate returned identities with process and network inventories. Cleanup
is exact post-stop set subtraction: `after - before`, restricted to objects in
the ownership map. Pre-existing unrelated objects neither fail the item nor
may be deleted. Broad `pkill`, interface-name globs, broad rule deletion, or
host-global cleanup is forbidden. A newly leaked M4 object remains present in
the failing evidence until its cause is understood; do not delete it to
manufacture zero.

### Data-preservation lifecycle invariant

Before the first M4 start, create unique host and guest sentinel files and
manifests/hashes covering the IOL image, lab storage, synced data, and unrelated
user-data samples. At each checkpoint below, independently verify existence,
size, mode where relevant, and hash; record the Lima machine still exists and
that no M4 harness argv/path invoked `delete` for the M4 VM:

- after every `lab.stop` and launcher `stop`;
- after each recovery case and forced VM restart;
- after every RAM-headroom action that affects the M4 VM; and
- after the final stop.

A missing, rewritten, or mode-changed sentinel fails the phase at the first
checkpoint where it appears. `stop` is therefore proven non-destructive over
the whole lifecycle, not asserted from final state.

## Fixed hardware execution order

The orchestrator enforces this state sequence and refuses out-of-order phases:

1. Preflight, preservation, sentinels, clean baseline, and harness self-check.
2. Item 1: VPCS–IOL proof, by itself and as the first hardware criterion.
3. Item 2: short multi-link run with capture; stop it and prove exact cleanup.
4. Item 3: NAT reachability/teardown; stop it and prove the item baseline is
   restored.
5. Item 4: mechanized extnet disposition; if exercised, stop it and prove the
   item baseline is restored.
6. Item 6: load a fresh, clean multi-link fixture, establish a fresh capture
   connection and fixed node/link set, then run the isolated two-hour soak.
   Seal and independently verify its evidence before any later state change.
7. Item 5: four-node capacity, using the deterministic RAM state machine.
8. Item 7: forced launcher termination, then forced VM termination and
   recovery.
9. Item 8: final hardware-derived record, scope gate, independent verification,
   final non-destructive stop, and sentinel verification.

During the soak's uninterrupted 7,200-second measurement window there are no
node/link starts or stops, other hardware phases, supervisor/launcher/VM
lifecycle commands, fixture reloads, or competing qualification harnesses.
The node/link/PID ownership set is fixed at the start and checked throughout.
Background work may only parse or copy already-written host evidence and must
not consume or command the test VM. NAT and extnet never overlap the soak.

## Per-item hardware-evidence matrix and acceptance bars

All raw artifacts carry wall-clock UTC and, where duration matters, monotonic
timestamps. Every active runtime phase records guest `/proc/loadavg`, `uptime`,
`free -b`, selected `/proc/meminfo`, kernel/OOM evidence, host `top` load and
PhysMem, `memory_pressure -Q`, swap, Lima/VZ RSS, and each active node's
`/proc/<pid>/status` VmRSS plus full `ps` identity. Required numbers are parsed
from the phase's own raw hardware files, never from prose or unit/compile logs.
Missing, stale, or unparsable data fails or unverifies that phase; it is never
reported as zero.

| Item | Mandatory raw hardware artifacts and numeric fields | Threshold/disposition |
|---|---|---|
| 1. VPCS–IOL | Loaded fixture and control/events; `started`/`failed`; both full console transcripts; two prompt times; four ping summaries (10 cold and 100 warm in each direction: sent/received/lost/loss/latency); IOL and VPCS PID/RSS; resource samples; before/running/after ownership inventories and exact cleanup counts. | First hardware item. Both real prompts; neither node failed; each warm direction is exactly 100 sent and at least 99 received; cold loss retained; all owned child/link/listener residue is zero after stop. |
| 2. Multi-link short run | Fixture, returned lab/node/link mapping, prompt/start times; VPCS↔R1 and R1↔R2 100-packet counts in both directions; bridge/device membership; fresh authenticated capture stream with start/end bytes, blocks, packet count and timestamps, SHA-256 and structural validation; resource and cleanup inventories. | All three fixed nodes reach prompts; each direction receives at least 99/100; mapping matches fixture; valid pcapng has SHB, IDB, complete symmetric blocks and packets after traffic began; packet count/time advance; exact owned residue zero after stop. |
| 3. NAT | `hello.features`; client address/route; chosen numeric target; timestamped Mac and ordinary-guest upstream controls immediately before and after; gateway and 20-packet outbound summaries; before/after `ip addr/route/link`, `iptables-save` and matching FORWARD/MASQUERADE counter values/deltas; tap/bridge map; resources and cleanup subtraction. | NAT starts; gateway succeeds; client receives at least 19/20 from the numeric target; matching forwarding and MASQUERADE deltas are positive; owned NAT residue is zero. Either failed control makes the attempt `UNVERIFIED` and requires a new reachable target/run—never PASS or waiver. |
| 4. extnet | All inputs to the decision table below with command exit statuses. If exercised: peer precheck, attach state, at least 20 packets each direction, capture/console if contract supplies it, resource samples, and exact cleanup counts. | Only “no suitable Lima interface” yields `NOT_EXERCISABLE`. An available interface requires at least 19/20 each way and zero owned residue. Missing product/permission/peer/harness path with a suitable interface is `FAIL/BLOCKED`. |
| 5. Four IOL | Attempt number; fixed profile/config; one `lab.start` result; four console transcripts, prompt times, PIDs and RSS values; 15-second boot samples; four adjacent-link warm summaries and R1↔R3 100-packet summaries; aggregate RSS; guest/host memory/load extrema; OOM/MALLOCFAIL/kernel/supervisor logs; cleanup and RAM-state-machine evidence. | Same 4 GiB Debian 13 guest, four 1024 MB IOL nodes concurrently; each prompt ≤150 s; every adjacent link passes; each end-to-end direction ≥99/100; no hard-wall condition; exact owned residue zero. Exactly one initial attempt and, only after prescribed reclamation, one cold retry. |
| 6. Isolated soak | Attempt identity; fixed ownership set; start/end UTC and monotonic times; process start identities; traffic/metrics/collector heartbeat rows; pcapng checkpoints at elapsed 0, 30, 60, 90, and 120 minutes; interval and cumulative ping counts; per-node RSS and resource extrema; power/sleep/boot records; watchdog events; collector exit; final pcapng/count/hash validation; seal manifest and independent verification output. | One uninterrupted monotonic duration ≥7,200 s; ≥120 traffic summaries and ≥121 endpoint-inclusive resource samples; no heartbeat gap >90 s; cumulative loss ≤1%; no unexplained total-failure interval; one capture connection, advancing at every checkpoint, valid through the end with traffic in final five minutes; no sleep/reboot/reconnect/restart/death/OOM/wedge; verified seal. |
| 7. Forced recovery | Exact launcher PID/phase/SIGKILL argv and nonzero exit; exact Lima forced-stop help/argv/status; before/kill/restart ownership maps; both launcher restart and `GET / <500` readiness times; old-object set-subtraction counts; recovery fixture start/prompts and bidirectional ping counts; cleanup counts; sentinel manifests/hashes at every checkpoint. | Both forced exits proven; both restarts reach real HTTP readiness; every old owned PID/listener/interface/bridge/tap/NAT rule/netns/veth/cgroup/collector count is exactly zero before recovery load; recovery traffic meets item-1 warm bar and clean stop; every persistence hash matches. |
| 8. Record | Verified `summary.json`, verifier stdout/stderr/status, requirement matrix, source/hash index, scope diff, and result cross-check. | This is aggregation of items 1–7, not a separate hardware test. It passes only when the verifier recomputes and accepts all applicable fields. |

### Item implementation details retained from prior milestones

- Item 1 uses one IOL L3 node with explicit `ram: 1024` and one VPCS in a
  /24. Load through browser-equivalent `/control`, inspect `warnings` and
  `failed`, use privileged IOS extended ping, and take the last complete VPCS
  or IOS result rather than echoed/ring-buffer replay. Commit `9916fb9` is
  context only.
- Item 2 uses two 1024 MB IOL routers and one VPCS with independent R1–R2 and
  VPCS–R1 links (the NAT endpoint/link may exist only in the separate item-3
  fixture). Open every applicable console, disable IOS pagination, and capture
  R1–R2 through an authenticated same-origin `/capture/{link}` connection.
- Item 3 requires advertised `natgw`. Select the numeric target before the
  phase and run the Mac and ordinary guest-path control pings immediately
  before and after the NAT-client run. Record lease/static subnet, default
  route, gateway, and rule identity/counters. DNS is not part of the bar.
- Item 5 uses a four-node ring R1–R2–R3–R4–R1 with unique /30s and routes for
  R1↔R3. Start all four in one request, open consoles concurrently, and never
  reduce node count/RAM, substitute VPCS, or treat sequential pairs as equal.
- Item 7 kills only recorded exact PIDs/names. Confirm the supported Lima
  forced-stop syntax from its installed `--help`. After each forced case,
  inventory before loading recovery traffic so a fresh supervisor object is
  not confused with an old leak.

### Mechanized extnet decision table

Before NAT or extnet changes state, preserve raw `hello.features`, accepted
node/attach kinds and the documented current extnet contract; Lima VM
configuration and `limactl list --json` (or installed equivalent); host Lima
network inventory; and guest `ip -details link`, `ip addr`, and `ip route`, all
with exit statuses. Suitability is determined only by the existing extnet
contract. There is no invented default-route-interface exclusion unless that
contract explicitly contains it.

| Host probe | Contract/path probe | Required result |
|---|---|---|
| No interface satisfies the documented Lima/extnet host prerequisites | Any | `NOT_EXERCISABLE`; name the exact absent prerequisite and preserve every raw probe/status. |
| A suitable interface exists | Product node/attach kind absent, permission missing, harness path absent, or no known reachable peer | `FAIL/BLOCKED`; do not add an M4 product feature and do not relabel as non-exercisable. |
| A suitable interface and complete existing path exist | Known peer precheck fails | `UNVERIFIED`; preserve controls and rerun only when peer is reachable. |
| A suitable interface, complete path, and reachable known peer exist | Attach/traffic/cleanup runs | Apply item-4 traffic and zero-residue bars; failure is `FAIL`. |

### NAT reachability control

The selected numeric target and rationale are written before item 3. From the
Mac and from the guest's ordinary upstream path (not through the candidate NAT
node), send timestamped controls immediately before and after the phase and
retain counts/latency/exit status. Both endpoints must prove the target was
reachable across the bracket. These controls diagnose upstream outage but do
not weaken the NAT bar: the NAT client still needs 19/20 and positive matching
rule-counter deltas.

## Isolated soak protocol, failure handling, and seal

### Launch and power audit

Run the Mac-side soak harness independently of SSH (recorded service/session or
`nohup`-style parentage, logs, PID file, and exit file). Verify AC power and
record `pmset -g batt`, `pmset -g custom`, `pmset -g assertions`, and
`sysctl -n kern.boottime`. Start `caffeinate` with a recorded assertion tied to
the exact harness PID (for example `caffeinate -dimsu -w <harness-pid>`) and
prove the assertion is active. Record `kern.boottime` again at the endpoint
and capture a time-bounded `pmset -g log` sleep/wake excerpt covering only the
attempt interval.

The watchdog runs outside the traffic/collector workers and checks distinct
traffic, sampler, and collector heartbeats. A missed heartbeat over 90 seconds,
worker death, collector reconnect, invalid block, stalled packet timestamp,
harness hang/crash, sleep/wake, reboot/boottime change, or unexpected VM/node
identity change fails the attempt.

### Attempt failure rule

On failure or disconnect, the independent harness first closes no evidence
over the top of the attempt. It records partial pcapng and metrics, worker exit
statuses, exact process/network ownership inventory, launcher/VM status,
service and kernel journals, power logs, and the failure reason in that
attempt's immutable directory. Only then may it kill recorded owned PIDs and
restore the clean baseline with exact operations. It creates a new attempt
directory for any retry.

Any interruption means restart from zero: a sleep, reboot, collector restart,
heartbeat gap, crash, hang, disconnect that kills/interrupts the harness, or
other failed attempt requires a new uninterrupted 7,200-second run. Attempts
cannot be resumed or concatenated, and no sample or capture block from a failed
attempt may be copied into the passing attempt.

### `SOAK-COMPLETE` seal

After at least 7,200 monotonic seconds and only if every soak bar has passed:

1. Stop traffic at the scheduled boundary, let the collector close normally,
   flush and `fsync` the pcapng, metrics, heartbeat, and index files, and record
   worker exit statuses. A partial or merely closed capture is insufficient.
2. Validate the final complete pcapng block and recompute duration, interval
   counts, packet/loss counts, resource extrema, checkpoint advancement, file
   sizes, and SHA-256 hashes from raw files.
3. Write a manifest containing schema/version, run and attempt IDs, fixed
   ownership set, start/end UTC and monotonic duration, all counts, every raw
   relative path/hash, and validator command/status. Create
   `SOAK-COMPLETE` atomically by writing/fsyncing a temporary file, fsyncing
   its directory, then renaming it. Failed bars create `SOAK-FAILED`, never
   the complete marker.
4. Retain/copy the marker and all raw soak files on the Mac host, independently
   re-hash them, rerun the record validator against that host copy, and save
   its output/status.

Item 5, forced termination, VM/launcher stop, M4 VM reuse/reset/deletion, or
any fixture change is refused until the host copy's seal and independent
validation both verify. If sealing fails, preserve the attempt and run a fresh
full soak; later phases remain locked.

## Deterministic four-node RAM-wall state machine

The state machine is concurrency-safe and has no discretionary “nonessential
VM” category:

1. **INVENTORY:** Record every VM's exact name/state, config, disk identity,
   Lima/VZ PIDs/RSS, memory allocation, and owner/session marker. Confirm the
   soak seal. Preserve every VM before any reuse/reset/delete as specified
   above.
2. **INITIAL:** From the clean stopped M4 lab, record headroom and perform
   exactly one four-node attempt in the fixed 4 GiB profile.
3. **PASS:** If all item-5 bars pass, stop the lab, verify cleanup and
   sentinels, and continue.
4. **HARD_WALL:** This state is entered only by a recorded VM allocation/start
   failure, guest OOM kill, IOL `%SYS-2-MALLOCFAIL`, any missing required
   sample, or any of four nodes failing to reach a real prompt within 150
   seconds. Preserve the complete attempt before reclamation.
5. **RECLAIM:** Consider only, and in this exact order, `m1jammy`, `m1trixie`,
   `iolbox-m1-e2e`, `iolbox-m2-e2e`, `iolbox-m3-e2e`. Before stopping each,
   prove exact name is neither `iol22` nor the M4 VM and its marker shows no
   concurrent owner. If owned/unclear, stop the state machine and ask that
   owner; do not stop it. Stop each eligible running VM one at a time and
   record Lima list, PhysMem, pressure, and swap after each. Kill only exact
   qualification-session PIDs. Ordinary reclamation never deletes a stopped
   VM. Deletion requires the separately proven disk/resident-allocation reason
   and verified archive wrapper.
6. **COLD_RETRY:** Stop the M4 VM non-destructively, verify sentinels and
   machine existence, start it cold, prove `GET / <500`, then perform exactly
   one retry with the unchanged fixed profile/topology.
7. **FINAL:** A passing retry continues. Any retry hard wall makes item 5
   `BLOCKED/UNVERIFIED` and M4 `INCOMPLETE`; preserve all records and ask the
   owner. Only the owner may move qualification to a 16 GB Apple Silicon Mac.

No third attempt, silent overcommit, lower RAM, smaller topology, or different
hardware is accepted without an explicit new owner decision.

## Versioned record and independent verifier

`summary.json` uses schema `iolbox.macos-m4.summary/v2` and contains at least:

```json
{
  "schema": "iolbox.macos-m4.summary/v2",
  "run_id": "...",
  "base_commit": "...",
  "identity": {"profile": "...", "product": "...", "build": "...", "host": {}},
  "time": {"start_utc": "...", "end_utc": "..."},
  "items": {
    "1": {"status": "PASS", "start_utc": "...", "end_utc": "...", "metrics": {}, "sources": []},
    "2": {"status": "PASS", "start_utc": "...", "end_utc": "...", "metrics": {}, "sources": []},
    "3": {"status": "PASS", "start_utc": "...", "end_utc": "...", "metrics": {}, "sources": []},
    "4": {"status": "PASS", "decision": "...", "start_utc": "...", "end_utc": "...", "metrics": {}, "sources": []},
    "5": {"status": "PASS", "attempts": [], "metrics": {}, "sources": []},
    "6": {"status": "PASS", "attempt_id": "...", "seal": {}, "metrics": {}, "sources": []},
    "7": {"status": "PASS", "cases": [], "metrics": {}, "sources": []}
  },
  "requirements": {},
  "scope": {},
  "artifacts": [],
  "overall": "PASS"
}
```

Every metric is an object containing typed `value`/`unit` plus a relative raw
hardware `source_path`, source SHA-256, command-record path, and source
start/end time. Each artifact has path, class (`hardware`, `unit`, `compile`,
or `static`), SHA-256, size, and producing time. Item times must be contained
in the run interval; source times must be contained in the owning item/attempt.
Item 8 is not stored as an independently passing test; `overall` is computed
from items 1–7, requirements, scope, and artifact integrity.

Implement a platform-neutral independent command, exposed for example as:

```
go test ./tools/iolab-launcher -run TestM4VerifyRecord -args \
  -m4-record <host-evidence-copy>/summary.json
```

The verifier opens raw files, validates hashes and classifications, recomputes
ping/capture/cleanup/sample counts, monotonic durations and resource extrema,
checks timestamp containment/freshness, soak attempt isolation and seal,
extnet decision, RAM attempt count, sentinels, requirements and diff scope.
It rejects missing/stale/unparsable samples, any number sourced from
compile/unit logs, unverified hashes, and a non-PASS applicable threshold, and
exits nonzero on any error. Save command/stdout/stderr/status as hardware-record
evidence. Generate or cross-check `docs/macos-m4-result.md` from the verified
summary; the result names that exact verifier invocation and output artifact.

## Named inherited-requirements matrix

The result and `summary.json.requirements` use IDs `M4-REQ-A` through
`M4-REQ-H`. Every row records commands, start/end times, exit statuses, and
artifact paths/hashes. A missing/nonpassing row blocks completion.

| ID | Executable gate and required evidence |
|---|---|
| `M4-REQ-A` Profiles are data | Record the scoped diff and run existing profile tests. A static/diff gate proves `profiles.env` is still read as data and no Go literal/table re-encodes it. |
| `M4-REQ-B` Exact identity | A profile test accepts exact `(profile, product, build)` string equality and rejects near/non-equal profile, product, and build values. Static/diff inspection rejects numeric macOS-version comparison. |
| `M4-REQ-C` NDJSON semantics | Tests inject events and wrong IDs before the matching string ID, prove correlation ignores them, then return `ok:true` with `warnings`/`failed` and assert those are evaluated. Exercise raw-YAML `lab.saveDoc` and structured-JSON `lab.load`. |
| `M4-REQ-D` Observable readiness | Hardware request/timing logs prove readiness only at `GET /` status below 500 and show bounded active retries for consoles/capture/dynamic endpoints. Lima `Running`, systemd `active`, and node `running` timestamps are retained only as nonacceptance diagnostics. |
| `M4-REQ-E` Browser WS authentication | Separate negative missing-cookie and bad-Origin probes fail, and positive session-cookie plus same-origin probes pass for `/control`, every console opened in acceptance, and every capture. Positive code reuses `wsDialWithSession`; diff gate rejects new raw dials. |
| `M4-REQ-F` IOS console discipline | Transcripts/event logs show initial `\r\n`, periodic re-pokes, fragmented-frame accumulation, pagination disablement, privileged EXEC for extended ping, selection of the last complete success summary, and a final prompt after each command. |
| `M4-REQ-G` RAM floor | Fixture validation tests reject missing IOL RAM and every value below 1024 MB. The artifact index and hardware fixtures prove every IOL is at least 1024 MB and item 5 remains four fixed nodes. |
| `M4-REQ-H` Implementation and persistence | `gofmt -l` output is empty for every changed Go file; import audit permits only stdlib/repository-local additions; `go.mod` hash/diff is unchanged. Every lifecycle sentinel checkpoint passes and command audit proves launcher/M4 stop never invokes delete or removes data. |

Also preserve IOL image executable mode and the established M1 console
ring-buffer rule. Reuse `wsDialWithSession`, correlated control helpers,
`m3OpenConsoleWithRetry`, `m3ReadPrompt`/`m3SendConcurrently`, active wake and
re-poke behavior, browser-equivalent `GET /`, and the existing structural
pcapng validator rather than redesigning them.

## Harness and expected implementation changes

Unconditional implementation outputs are:

- `tools/iolab-launcher/macos_m4_runtime_darwin_test.go`: opt-in phased real
  hardware driver, authentication probes, traffic, extnet decision, ownership,
  sampling, soak, four-node and recovery assertions.
- `tools/iolab-launcher/macos_m4_record_test.go` (or small matching `_test.go`
  files): platform-neutral parsers, schema/verifier, fixture validation,
  pcapng/metrics/cleanup/NDJSON tests.
- `tools/iolab-launcher/testdata/macos-m4/vpcs-iol.lab.json`.
- M4 multi-link short/NAT/clean-soak fixtures with separate phase state so NAT
  cannot overlap the soak; each IOL has explicit `ram: 1024` or higher.
- `tools/iolab-launcher/testdata/macos-m4/four-iol-ring.lab.json`.
- `tools/iolab-launcher/README.md`: opt-in invocation, prerequisites,
  isolated two-hour warning, evidence/verification layout.
- `packaging/macos/tests/hardware-m4.sh`: Bash 3.2 orchestrator, protected-name
  and ownership gates, VM preservation, sentinels, isolated watchdog soak and
  seal, deterministic RAM state machine, exact-PID recovery, and collation.
- `packaging/macos/tests/lint.sh` only if required to cover the new harness.
- `docs/macos-m4-result.md`, written from/cross-checked against actual verified
  hardware evidence; incomplete runs are recorded honestly.

`docs/macos-m4-plan.md` is deliberately absent: it remains read-only and
hash-stable throughout implementation. Do not add an extnet fixture unless
the decision table reaches the executable path supported by the current
contract. Existing browser E2E, WS client, launcher lifecycle/profile/port/sync
and product/runtime files remain unchanged unless a preserved hardware defect
requires a narrow conditional fix.

## Scope diff gate and out of scope

At the end, save
`git diff --name-status <recorded-base>...HEAD` and a working-tree
`git diff --name-status` as scope artifacts. The verifier fails if they show:

- this frozen plan, M0–M3 result documents, `docs/macos-arm64-plan.md`, or
  `docs/macos-arm64-plan-review.md` changed;
- any M5/M6/M7 implementation or release packaging/CI work;
- a profile/port/sync/browser-equivalent flow, M1 provisioning, M2 Lima/CLI,
  or M3 contract redesign; or
- any unexplained product/runtime file.

A conditional runtime file is accepted only when the summary links the
preserved failing hardware evidence, root cause, exact focused change, new
regression test, and successful affected hardware rerun. Stage only exact
owned paths. Do not install Wireshark; the stdlib structural parser is
authoritative, with existing `capinfos`/`tshark` allowed only as a cross-check.

## Machine-verifiable completion

M4 is complete only when the independent verifier exits zero over the retained
host evidence copy and proves all of the following from raw artifacts:

- items 1–3 and 5–7 are `PASS`; item 4 is `PASS` or the decision table's
  narrowly valid `NOT_EXERCISABLE`;
- VPCS was first, the short multi-link/NAT/extnet phases were cleaned, and the
  passing soak was a fresh isolated attempt with a verified `SOAK-COMPLETE`;
- four concurrent 1024 MB IOL nodes passed—`BLOCKED/UNVERIFIED` is incomplete;
- all resource, traffic, capture, recovery, cleanup, power, and persistence
  numbers recompute from correctly timed hardware sources;
- all eight named requirements gates and the final scope gate pass;
- hardware claims are separated from compile/unit/static evidence;
- final stop preserves every sentinel, the M4 VM still exists stopped, `iol22`
  was untouched, and the frozen plan hash is unchanged.

Any nonzero verifier result makes `overall` `INCOMPLETE`, with the precise
`FAIL`, `BLOCKED`, or `UNVERIFIED` reasons retained in the final record.
