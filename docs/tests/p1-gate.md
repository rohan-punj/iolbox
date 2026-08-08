# P1 learning-tool node acceptance gate

This is the real-target acceptance gate for P1 (`T1.1`–`T1.12`) in
`docs/learning-tools-nodes-plan.md`. It drives the supervisor's raw NDJSON
control socket and exercises the installed `stub` tool pack together with a
bundled VPCS peer.

Run it as root on the Linux appliance from a world-traversable checkout path:

```sh
sudo bash docs/tests/p1-gate.sh
```

The gate is pinned to the systemd-managed runtime. Its restart command is
exactly:

```sh
systemctl restart iolbox-supervisor
```

That command reuses the unit's exact `ExecStart`, `PATH`, delegated cgroup, and
environment. The script deliberately does not guess a development supervisor
argv after the `kill -9` crash step.

## Prerequisites

The target must provide:

- a properly delegated systemd scope for the gate, with cgroup v2 access and
  `memory`, `pids`, and `cpu` controllers available;
- the `ioltool` system account;
- util-linux `setpriv` version 2.33 or newer;
- the installed stub pack at `/opt/iolbox/tools/packs/stub/pack.json`;
- the helper binary `/opt/iolbox/iolbox-toollaunch`;
- `iproute2`, `python3`, GNU `stat`, `systemctl`, and the usual POSIX shell
  utilities used by the gate, including `fuser`.

As with the P0 spike, run the gate inside a delegated scope rather than from a
bare shell. For example:

```sh
systemd-run --scope --unit=p1-gate -p Delegate=yes -p CPUAccounting=yes \
  -p MemoryAccounting=yes -p TasksAccounting=yes -p IOAccounting=yes -- \
  bash docs/tests/p1-gate.sh
```

Run the gate from a path traversable by `ioltool` (for example `/opt/...`, not
under a default `0700` `/root` directory). A failed attempt can leave
kernel-global objects if the process is killed before its trap runs; clear
stale `iolt*`, `vtool*`, and delegated `tool-*` objects between attempts, then
restart the unit so `InitRuntime` performs its precise stale-object sweep.

The landed systemd unit passes `-run-dir /opt/iolbox/run`, so this gate checks
`/opt/iolbox/run/tool/1/options.json` and removes
`/opt/iolbox/run/tool/1`. The plan prompt's illustrative `/run/iolbox/...`
path is the endpoint's zero-value default; the unit's explicit runtime path is
the authoritative path for this systemd gate.

## Fixture and wire shape

The lab is intentionally two-ended because `link.add` requires two endpoints:

```json
{"version":1,"id":"p1-gate","name":"P1 Gate","nodes":[
  {"id":1,"kind":"tool","name":"TOOL1","x":0,"y":0,"config":{"pack":"stub"}},
  {"id":2,"kind":"vpcs","name":"PC1","x":120,"y":0}],
 "links":[]}
```

The control protocol uses `op` (not `verb`). The link request follows the
landed `protocol.LinkArgs` shape:

```json
{"id":"p1-link-add","op":"link.add","args":{"labId":"p1-gate","link":
  {"id":1,"type":"p2p","endpoints":[
    {"node":1,"interface":"eth1"},
    {"node":2,"interface":"eth0"}]}}}
```

Responses are matched by request `id`; pushed events are skipped unless the
line contains an `ok` field. After restart, `status` is used to query the
loaded lab because the landed server registers `lab.listDocs`, not a
`lab.list` verb.

## Gate procedure

The script prints an explicit `PASS` or `FAIL` line for each step and exits
non-zero on the first failed assertion.

1. Restart the systemd unit, wait for a successful TCP connect followed by an
   `hello` response within 30 seconds, and verify that `tools` is advertised and
   the `stub` pack is registered. Before that restart, the script clears any
   stale listener with `fuser -k 4000/tcp`; this is intentional because a stale
   supervisor's comm is `supervisor`, not `supervisor-linux-amd64`. Load the
   pinned lab and start both nodes.
   Assert that tool node 1 reaches `running`, that the `StartedNode` payload has
   the landed shape, and that the options file is exactly
   `ioltool:ioltool 0600` using `stat -c '%U:%G %a'`.
2. Add then remove link 1. The operations must succeed as hot fabric
   attach/detach operations, and the tool child PID read from
   `/sys/fs/cgroup/<D>/tool-1/cgroup.procs` must remain unchanged after both.
3. Record the supervisor PID, send `kill -9`, and run the exact systemd restart
   command above. The bounded TCP+`hello` readiness check proves
   `InitRuntime` (including `ReapStale`) completed before the listener accepted
   the connection. Reload the lab explicitly and assert both nodes are known
   but stopped. Then assert that `iolt1`, `vtool1`, the delegated `tool-1`
   cage, and supervisor-owned zombie children are absent. The zombie check is
   ancestry-scoped: it counts only `/proc/*/stat` entries with state `Z` and
   PPid equal to the current supervisor, so unrelated host zombies cannot fail
   this gate.
4. Send `lab.stop` and assert no `iolt1` netns, `vtool1` link, delegated
   `tool-1` cage, `/opt/iolbox/run/tool/1` directory, or cage process remains.

## Real-target run log

Record the appliance, runtime image/build, exact command, and complete output
here after a real Linux run. Do not convert a failed assertion or an unmet
prerequisite into a passing record.

```text
Date/target: 2026-08-08, appliance VM 192.168.226.233 (Debian 12 bookworm)
Command: cd /opt/iolbox-p1 && bash p1-gate.sh
Result: P1 PASS: tool lifecycle, hot fabric attach/detach, crash sweep, and
        clean stop completed on this Linux target.
Notes: PASS on the 8th run of this gate against real hardware, after fixing
       4 real bugs found only by live execution (none caught by go
       build/vet/test, matching P0's pattern exactly):

       1. LauncherAvailable() selected setpriv purely by its version number
          (>=2.33). This appliance's setpriv 2.38.1 clears that floor but
          still fails the pinned transition at runtime with
          `setpriv: unknown capability "cap_net_raw"` -- confirmed directly
          on the VM, the same appliance limitation P0 already worked around
          empirically. Fixed by making LauncherAvailable() actually run the
          pinned transition once and check the resulting capability state
          before trusting setpriv, falling back to the native
          iolbox-toollaunch helper (confirmed correct: delivers
          0000000000002000 -- ambient CAP_NET_RAW only -- across all five
          capability sets).
       2. Detect()'s own probe misclassified two SUCCESSFUL checks as
          failures: (a) its post-delete veth verification wrapped the stat
          error before checking os.IsNotExist, which does not traverse %w
          chains, so a genuinely-deleted veth was reported as "still
          present"; (b) its AF_UNIX self-test closed the listener before
          collecting the Accept() goroutine's result, a scheduling race that
          reports "use of closed network connection" even when the dial
          genuinely succeeded. Root-caused with a standalone debug binary
          calling tool.Detect() directly and printing its Reasons map
          (temporary, removed, never committed).
       3 & 4. Two rounds of a TOCTOU race between direct-child spawn and the
          process-wide subreaper loop: a fast `ip` command can exit before a
          separate Registry.Add(pid) call runs, so the subreaper's
          independent 10ms poll reaps it as an unregistered orphan first,
          and the spawner's own cmd.Wait() then fails with
          "waitid: no child processes". First fixed across the 11 call
          sites the P1 dispatch plan's B10 batch had registered (which
          assumed the race window was negligible -- disproven on real
          hardware during a routine link.add); a second, worse instance
          then surfaced one layer earlier in tool.go's own shared
          runCmds/runCmdsBestEffort helpers (used by every netns/veth
          operation), which predated the whole registration effort and
          never registered at all. Fixed with PIDRegistry.StartAndAdd/
          ReapUnregistered, making spawn+register and peek+reap mutually
          exclusive under the registry's existing mutex.

       All four fixes are commits on feat/learning-tool-nodes after the P1
       dispatch plan's 17 batches landed. P1 is closed; see
       [[iolab-learning-tools-p0]] memory for the full history.
```
