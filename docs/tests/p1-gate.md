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
Date/target:
Command:
Result:
Notes:
```
