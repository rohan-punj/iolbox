#!/usr/bin/env bash
# P1 acceptance gate for the internal/tool endpoint and tool fabric lifecycle.
# Run on the Linux appliance as root from a world-traversable checkout path.
set -Eeuo pipefail

P1_CONTROL_HOST=127.0.0.1
P1_CONTROL_PORT=4000
P1_SYSTEMD_UNIT=iolbox-supervisor
P1_LAB_ID=p1-gate
P1_TOOL_RUN_ROOT=/opt/iolbox/run
P1_OPTIONS_FILE="$P1_TOOL_RUN_ROOT/tool/1/options.json"
P1_TOOL_CAGE_NAME=tool-1
P1_TOOL_NETNS=iolt1
P1_TOOL_VETH=vtool1
P1_CONTROL_READY=0
P1_SERVICE_TOUCHED=0

p1_pass() {
	echo "PASS $*"
}

p1_fail() {
	echo "FAIL $*" >&2
	exit 1
}

p1_need() {
	command -v "$1" >/dev/null 2>&1 || p1_fail "prerequisite: missing required command: $1"
}

# Each request gets its own short-lived TCP connection. The reader deliberately
# ignores pushed events and only accepts a matching id with an explicit ok field;
# this is the same correlation rule used by the fabric harness.
p1_rpc() {
	local p1_op="$1" p1_args="$2"
	local p1_id="p1-${BASHPID}-${RANDOM}-${p1_op//./-}"
	python3 - "$P1_CONTROL_HOST" "$P1_CONTROL_PORT" "$p1_id" "$p1_op" "$p1_args" <<'PY'
import json
import socket
import sys

host, port_text, request_id, operation, args_text = sys.argv[1:]
request = {
    "id": request_id,
    "op": operation,
    "args": json.loads(args_text),
}
wire = (json.dumps(request, separators=(",", ":")) + "\n").encode("utf-8")

with socket.create_connection((host, int(port_text)), timeout=2.0) as sock:
    sock.settimeout(2.0)
    sock.sendall(wire)
    stream = sock.makefile("rb")
    while True:
        line = stream.readline()
        if not line:
            raise RuntimeError("control connection closed before the response")
        try:
            message = json.loads(line)
        except json.JSONDecodeError:
            continue
        if message.get("id") != request_id or "ok" not in message:
            continue
        print(json.dumps(message, separators=(",", ":")))
        break
PY
}

p1_tcp_probe() {
	python3 - "$P1_CONTROL_HOST" "$P1_CONTROL_PORT" <<'PY'
import socket
import sys

with socket.create_connection((sys.argv[1], int(sys.argv[2])), timeout=0.5):
    pass
PY
}

p1_response_ok() {
	local p1_response="$1"
	python3 - "$p1_response" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
if response.get("ok") is not True:
    error = response.get("error", {})
    raise SystemExit(f"{error.get('code', 'unknown')}: {error.get('message', 'request failed')}")
PY
}

p1_assert_ok() {
	local p1_response="$1" p1_label="$2"
	if ! p1_response_ok "$p1_response"; then
		p1_fail "$p1_label returned a non-ok response: $p1_response"
	fi
}

# InitRuntime runs ReapStale before ListenAndServe. A successful TCP connect is
# therefore followed by hello as the bounded readiness proof, without a guessed
# post-start sleep.
p1_wait_ready() {
	local p1_deadline p1_now p1_hello
	p1_deadline=$(( $(date +%s) + 30 ))
	while :; do
		p1_now="$(date +%s)"
		(( p1_now >= p1_deadline )) && return 1
		if p1_tcp_probe 2>/dev/null; then
			p1_hello="$(p1_rpc hello '{"client":"p1-gate"}' 2>/dev/null || true)"
			if [[ -n "$p1_hello" ]] && p1_response_ok "$p1_hello" >/dev/null 2>&1; then
				printf '%s\n' "$p1_hello"
				return 0
			fi
		fi
		sleep 0.1
	done
}

p1_systemd_pid() {
	local p1_pid
	p1_pid="$(systemctl show -p MainPID --value "$P1_SYSTEMD_UNIT")"
	[[ "$p1_pid" =~ ^[1-9][0-9]*$ ]] || return 1
	[[ -d "/proc/$p1_pid" ]] || return 1
	printf '%s\n' "$p1_pid"
}

p1_delegated_root() {
	local p1_pid="$1" p1_rel
	p1_rel="$(awk -F: '$1 == 0 { print $3; exit }' "/proc/$p1_pid/cgroup")"
	[[ "$p1_rel" == */supervisor ]] || return 1
	p1_rel="${p1_rel%/supervisor}"
	[[ -n "$p1_rel" && "$p1_rel" != "/" ]] || return 1
	printf '/sys/fs/cgroup%s\n' "$p1_rel"
}

p1_tool_pid() {
	local p1_cage="$1" p1_pid
	[[ -r "$p1_cage/cgroup.procs" ]] || return 1
	while IFS= read -r p1_pid; do
		[[ "$p1_pid" =~ ^[1-9][0-9]*$ ]] || continue
		if kill -0 "$p1_pid" 2>/dev/null; then
			printf '%s\n' "$p1_pid"
			return 0
		fi
	done < "$p1_cage/cgroup.procs"
	return 1
}

p1_wait_tool_pid() {
	local p1_cage="$1" p1_try
	for ((p1_try = 0; p1_try < 300; p1_try++)); do
		if p1_tool_pid "$p1_cage"; then
			return 0
		fi
		sleep 0.1
	done
	return 1
}

p1_assert_no_tool_cages() {
	local p1_root="$1" p1_path
	for p1_path in "$p1_root"/tool-*; do
		if [[ -e "$p1_path" ]]; then
			p1_fail "tool cage remains under delegated root: $p1_path"
		fi
	done
}

p1_assert_no_tool_netns() {
	local p1_netns_list
	if ! p1_netns_list="$(ip netns list)"; then
		p1_fail "could not enumerate network namespaces"
	fi
	if awk -v expected="$P1_TOOL_NETNS" '$1 == expected { found = 1 } END { exit found ? 0 : 1 }' <<<"$p1_netns_list"; then
		p1_fail "tool netns remains: $P1_TOOL_NETNS"
	fi
}

p1_assert_no_tool_veth() {
	if ip link show dev "$P1_TOOL_VETH" >/dev/null 2>&1; then
		p1_fail "tool host veth remains: $P1_TOOL_VETH"
	fi
}

p1_assert_hello_tools() {
	local p1_response="$1"
	python3 - "$p1_response" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
features = response.get("result", {}).get("features", [])
if "tools" not in features:
    raise SystemExit(f"hello features do not advertise tools: {features!r}")
PY
}

p1_assert_stub_pack() {
	local p1_response="$1"
	python3 - "$p1_response" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
packs = response.get("result", {}).get("packs", [])
stub = [pack for pack in packs if pack.get("id") == "stub"]
if len(stub) != 1:
    raise SystemExit(f"tool.listPacks did not return exactly one stub pack: {packs!r}")
if stub[0].get("transport") != "unix":
    raise SystemExit(f"stub pack transport is not unix: {stub[0]!r}")
PY
}

p1_assert_lab_load() {
	local p1_response="$1"
	python3 - "$p1_response" "$P1_LAB_ID" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
expected_id = sys.argv[2]
result = response.get("result", {})
if result.get("labId") != expected_id:
    raise SystemExit(f"lab.load returned labId={result.get('labId')!r}")
if sorted(node.get("id") for node in result.get("nodes", [])) != [1, 2]:
    raise SystemExit(f"lab.load did not return both fixture nodes: {result!r}")
PY
}

p1_assert_started() {
	local p1_response="$1"
	python3 - "$p1_response" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
started = response.get("result", {}).get("started", [])
if sorted(node.get("node") for node in started) != [1, 2]:
    raise SystemExit(f"lab.start did not return both nodes: {started!r}")
for node in started:
    if set(node) != {"node", "consolePort", "pid", "state"}:
        raise SystemExit(f"unexpected StartedNode shape: {node!r}")
    if node.get("state") != "running":
        raise SystemExit(f"node {node.get('node')} did not reach running: {node!r}")
tool = next(node for node in started if node.get("node") == 1)
if tool.get("consolePort") != 0 or tool.get("pid") != 0:
    raise SystemExit(f"tool StartedNode unexpectedly exposes process fields: {tool!r}")
PY
}

p1_assert_link_result() {
	local p1_response="$1" p1_label="$2"
	python3 - "$p1_response" "$p1_label" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
label = sys.argv[2]
result = response.get("result", {})
if set(result) != {"link"} or result.get("link") != 1:
    raise SystemExit(f"{label} returned unexpected link result: {result!r}")
PY
}

p1_assert_states() {
	local p1_response="$1" p1_expected="$2"
	python3 - "$p1_response" "$P1_LAB_ID" "$p1_expected" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
expected_id, expected_state = sys.argv[2:]
result = response.get("result", {})
if result.get("labId") != expected_id:
    raise SystemExit(f"status returned labId={result.get('labId')!r}")
nodes = {node.get("id"): node for node in result.get("nodes", [])}
if sorted(nodes) != [1, 2]:
    raise SystemExit(f"status did not return both fixture nodes: {result!r}")
for node_id, node in nodes.items():
    if node.get("state") != expected_state:
        raise SystemExit(f"node {node_id} state={node.get('state')!r}, expected {expected_state!r}")
PY
}

p1_assert_stop_result() {
	local p1_response="$1"
	python3 - "$p1_response" <<'PY'
import json
import sys

response = json.loads(sys.argv[1])
if response.get("result", {}).get("started") != []:
    raise SystemExit(f"lab.stop returned an unexpected result: {response!r}")
PY
}

p1_assert_supervisor_children_reaped() {
	local p1_supervisor_pid="$1" p1_zombies
	# Scope this check to zombies whose PPid is this supervisor: unrelated host
	# zombies are not evidence of the T1.8 orphan-reaping ownership split.
	p1_zombies="$(python3 - "$p1_supervisor_pid" <<'PY'
import glob
import sys

supervisor_pid = int(sys.argv[1])
zombies = []
for stat_path in glob.glob("/proc/[0-9]*/stat"):
    try:
        line = open(stat_path, encoding="utf-8").read()
        close_paren = line.rfind(") ")
        if close_paren < 0:
            continue
        fields = line[close_paren + 2:].split()
        # After comm, fields[0] is state and fields[1] is PPid.
        if len(fields) > 1 and fields[0] == "Z" and int(fields[1]) == supervisor_pid:
            zombies.append(stat_path.split("/")[2])
    except (OSError, ValueError):
        continue
print(" ".join(sorted(zombies)))
PY
 )"
	[[ -z "$p1_zombies" ]] || p1_fail "supervisor $p1_supervisor_pid still has unreaped child zombies: $p1_zombies"
}

p1_cleanup() {
	local p1_status=$?
	set +e
	if [[ "$P1_CONTROL_READY" == 1 ]]; then
		p1_rpc lab.stop '{"labId":"p1-gate"}' >/dev/null 2>&1 || true
	fi
	if [[ "$P1_SERVICE_TOUCHED" == 1 ]]; then
		systemctl restart "$P1_SYSTEMD_UNIT" >/dev/null 2>&1 || echo "WARN cleanup: systemctl restart $P1_SYSTEMD_UNIT failed" >&2
	fi
	exit "$p1_status"
}

trap p1_cleanup EXIT

[[ "$(id -u)" == 0 ]] || p1_fail "p1-gate.sh must run as root"
for P1_COMMAND in awk date fuser id ip python3 sed setpriv seq sleep sort stat systemctl; do
	p1_need "$P1_COMMAND"
done
[[ -x /opt/iolbox/iolbox-toollaunch ]] || p1_fail "prerequisite: missing /opt/iolbox/iolbox-toollaunch"
[[ -f /opt/iolbox/tools/packs/stub/pack.json ]] || p1_fail "prerequisite: missing installed stub pack manifest"
id ioltool >/dev/null 2>&1 || p1_fail "prerequisite: missing unprivileged account: ioltool"
P1_SETPRIV_VERSION="$(setpriv --version | sed -nE 's/.* ([0-9]+\.[0-9]+).*/\1/p' | head -n1)"
[[ -n "$P1_SETPRIV_VERSION" ]] || p1_fail "prerequisite: could not determine setpriv version"
[[ "$(printf '%s\n' "$P1_SETPRIV_VERSION" 2.33 | sort -V | head -n1)" == 2.33 ]] || \
	p1_fail "prerequisite: setpriv $P1_SETPRIV_VERSION is older than 2.33"
systemctl cat "$P1_SYSTEMD_UNIT" >/dev/null 2>&1 || p1_fail "prerequisite: systemd unit not found: $P1_SYSTEMD_UNIT"

# This fixture is deliberately two-ended: link.add requires both a tool and a
# fabric-capable peer, while VPCS is bundled and does not need an image/iourc.
P1_LAB_DOC='{"version":1,"id":"p1-gate","name":"P1 Gate","nodes":[{"id":1,"kind":"tool","name":"TOOL1","x":0,"y":0,"config":{"pack":"stub"}},{"id":2,"kind":"vpcs","name":"PC1","x":120,"y":0}],"links":[]}'
P1_LINK_DOC='{"id":1,"type":"p2p","endpoints":[{"node":1,"interface":"eth1"},{"node":2,"interface":"eth0"}]}'
P1_LOAD_ARGS="{\"lab\":$P1_LAB_DOC}"
P1_LINK_ARGS="{\"labId\":\"$P1_LAB_ID\",\"link\":$P1_LINK_DOC}"

echo "==> preparing the systemd-managed supervisor"
P1_SERVICE_TOUCHED=1
# A stale supervisor's comm is "supervisor", not the build filename; clear the
# listener by socket rather than relying on a filename-based pkill.
fuser -k 4000/tcp >/dev/null 2>&1 || true
systemctl restart "$P1_SYSTEMD_UNIT" || p1_fail "preparation: systemctl restart $P1_SYSTEMD_UNIT failed"
if ! P1_HELLO_RESPONSE="$(p1_wait_ready)"; then
	p1_fail "preparation: supervisor did not become ready within 30 seconds"
fi
P1_CONTROL_READY=1
p1_assert_ok "$P1_HELLO_RESPONSE" "hello"
p1_assert_hello_tools "$P1_HELLO_RESPONSE" || p1_fail "preparation: hello did not advertise tools"
P1_SUPERVISOR_PID="$(p1_systemd_pid)" || p1_fail "preparation: systemd has no live supervisor MainPID"

P1_PACKS_RESPONSE="$(p1_rpc tool.listPacks '{}')" || p1_fail "preparation: tool.listPacks request failed"
p1_assert_ok "$P1_PACKS_RESPONSE" "tool.listPacks"
p1_assert_stub_pack "$P1_PACKS_RESPONSE" || p1_fail "preparation: installed stub pack is not registered"

echo "==> step 1: load and start the pinned two-endpoint lab"
P1_LOAD_RESPONSE="$(p1_rpc lab.load "$P1_LOAD_ARGS")" || p1_fail "step 1: lab.load request failed"
p1_assert_ok "$P1_LOAD_RESPONSE" "step 1 lab.load"
p1_assert_lab_load "$P1_LOAD_RESPONSE" || p1_fail "step 1: lab.load returned the wrong fixture"
P1_START_RESPONSE="$(p1_rpc lab.start '{"labId":"p1-gate"}')" || p1_fail "step 1: lab.start request failed"
p1_assert_ok "$P1_START_RESPONSE" "step 1 lab.start"
p1_assert_started "$P1_START_RESPONSE" || p1_fail "step 1: StartedNode result did not prove both nodes running"
P1_STATUS_RESPONSE="$(p1_rpc status '{"labId":"p1-gate"}')" || p1_fail "step 1: status request failed"
p1_assert_ok "$P1_STATUS_RESPONSE" "step 1 status"
p1_assert_states "$P1_STATUS_RESPONSE" running || p1_fail "step 1: status did not report both nodes running"
if [[ "$(stat -c '%U:%G %a' "$P1_OPTIONS_FILE" 2>/dev/null || true)" != "ioltool:ioltool 600" ]]; then
	p1_fail "step 1: options file is not ioltool:ioltool 0600: $P1_OPTIONS_FILE"
fi
if ! P1_CGROUP_D="$(p1_delegated_root "$P1_SUPERVISOR_PID")"; then
	p1_fail "step 1: could not derive delegated cgroup root from supervisor $P1_SUPERVISOR_PID"
fi
P1_TOOL_CAGE="$P1_CGROUP_D/$P1_TOOL_CAGE_NAME"
if ! P1_TOOL_PID="$(p1_wait_tool_pid "$P1_TOOL_CAGE")"; then
	p1_fail "step 1: no live tool child appeared in $P1_TOOL_CAGE"
fi
p1_pass "step 1: node 1 reached running; options file is ioltool:ioltool 0600; tool PID=$P1_TOOL_PID"

echo "==> step 2: hot-connect and detach without restarting the tool"
P1_LINK_ADD_RESPONSE="$(p1_rpc link.add "$P1_LINK_ARGS")" || p1_fail "step 2: link.add request failed"
p1_assert_ok "$P1_LINK_ADD_RESPONSE" "step 2 link.add"
p1_assert_link_result "$P1_LINK_ADD_RESPONSE" "link.add" || p1_fail "step 2: link.add returned the wrong result"
if ! P1_TOOL_PID_AFTER_ADD="$(p1_tool_pid "$P1_TOOL_CAGE")"; then
	p1_fail "step 2: tool child disappeared after link.add"
fi
[[ "$P1_TOOL_PID_AFTER_ADD" == "$P1_TOOL_PID" ]] || p1_fail "step 2: link.add restarted tool PID $P1_TOOL_PID -> $P1_TOOL_PID_AFTER_ADD"
P1_STATUS_RESPONSE="$(p1_rpc status '{"labId":"p1-gate"}')" || p1_fail "step 2: status after link.add failed"
p1_assert_ok "$P1_STATUS_RESPONSE" "step 2 status after link.add"
p1_assert_states "$P1_STATUS_RESPONSE" running || p1_fail "step 2: link.add changed node state"

P1_LINK_REMOVE_RESPONSE="$(p1_rpc link.remove "$P1_LINK_ARGS")" || p1_fail "step 2: link.remove request failed"
p1_assert_ok "$P1_LINK_REMOVE_RESPONSE" "step 2 link.remove"
p1_assert_link_result "$P1_LINK_REMOVE_RESPONSE" "link.remove" || p1_fail "step 2: link.remove returned the wrong result"
if ! P1_TOOL_PID_AFTER_REMOVE="$(p1_tool_pid "$P1_TOOL_CAGE")"; then
	p1_fail "step 2: tool child disappeared after link.remove"
fi
[[ "$P1_TOOL_PID_AFTER_REMOVE" == "$P1_TOOL_PID" ]] || p1_fail "step 2: link.remove restarted tool PID $P1_TOOL_PID -> $P1_TOOL_PID_AFTER_REMOVE"
P1_STATUS_RESPONSE="$(p1_rpc status '{"labId":"p1-gate"}')" || p1_fail "step 2: status after link.remove failed"
p1_assert_ok "$P1_STATUS_RESPONSE" "step 2 status after link.remove"
p1_assert_states "$P1_STATUS_RESPONSE" running || p1_fail "step 2: link.remove changed node state"
p1_pass "step 2: link.add/link.remove hot-connected and detached with unchanged tool PID=$P1_TOOL_PID"

echo "==> step 3: SIGKILL the supervisor and verify startup stale-object sweep"
P1_OLD_SUPERVISOR_PID="$P1_SUPERVISOR_PID"
kill -KILL "$P1_OLD_SUPERVISOR_PID" || p1_fail "step 3: kill -9 supervisor $P1_OLD_SUPERVISOR_PID failed"
for P1_TRY in $(seq 1 100); do
	[[ ! -e "/proc/$P1_OLD_SUPERVISOR_PID" ]] && break
	sleep 0.1
done
[[ ! -e "/proc/$P1_OLD_SUPERVISOR_PID" ]] || p1_fail "step 3: supervisor PID $P1_OLD_SUPERVISOR_PID survived SIGKILL"

# The unit's ExecStart, PATH environment, Delegate settings, and restart policy
# are reused by this exact command; the test does not re-exec a guessed argv.
systemctl restart iolbox-supervisor || p1_fail "step 3: exact systemd restart command failed"
if ! P1_HELLO_RESPONSE="$(p1_wait_ready)"; then
	p1_fail "step 3: restarted supervisor did not pass TCP+hello readiness within 30 seconds"
fi
P1_CONTROL_READY=1
p1_assert_ok "$P1_HELLO_RESPONSE" "step 3 hello"
P1_SUPERVISOR_PID="$(p1_systemd_pid)" || p1_fail "step 3: restarted supervisor has no live MainPID"

# A restart does not auto-start the previous lab. Reload it explicitly, then
# prove the new runtime knows both nodes but has them stopped.
P1_LOAD_RESPONSE="$(p1_rpc lab.load "$P1_LOAD_ARGS")" || p1_fail "step 3: reloading lab after restart failed"
p1_assert_ok "$P1_LOAD_RESPONSE" "step 3 lab.load after restart"
p1_assert_lab_load "$P1_LOAD_RESPONSE" || p1_fail "step 3: reloaded lab has the wrong fixture"
P1_STATUS_RESPONSE="$(p1_rpc status '{"labId":"p1-gate"}')" || p1_fail "step 3: status after reload failed"
p1_assert_ok "$P1_STATUS_RESPONSE" "step 3 status after reload"
p1_assert_states "$P1_STATUS_RESPONSE" stopped || p1_fail "step 3: reloaded nodes were not stopped"
if ! P1_CGROUP_D="$(p1_delegated_root "$P1_SUPERVISOR_PID")"; then
	p1_fail "step 3: could not derive delegated cgroup root from restarted supervisor"
fi
[[ -d "$P1_CGROUP_D" ]] || p1_fail "step 3: delegated cgroup root is not a directory: $P1_CGROUP_D"
p1_assert_no_tool_netns
p1_assert_no_tool_cages "$P1_CGROUP_D"
p1_assert_no_tool_veth
p1_assert_supervisor_children_reaped "$P1_SUPERVISOR_PID"
p1_pass "step 3: supervisor restart swept iolt1, vtool1, tool-1, and supervisor-owned zombies"

echo "==> step 4: stop the reloaded lab and assert no runtime residue"
P1_STOP_RESPONSE="$(p1_rpc lab.stop '{"labId":"p1-gate"}')" || p1_fail "step 4: lab.stop request failed"
p1_assert_ok "$P1_STOP_RESPONSE" "step 4 lab.stop"
p1_assert_stop_result "$P1_STOP_RESPONSE" || p1_fail "step 4: lab.stop returned an unexpected result"
p1_assert_no_tool_netns
p1_assert_no_tool_veth
[[ ! -e "$P1_TOOL_RUN_ROOT/tool/1" ]] || p1_fail "step 4: tool socket/options directory remains: $P1_TOOL_RUN_ROOT/tool/1"
if [[ -e "$P1_TOOL_CAGE" ]]; then
	[[ ! -s "$P1_TOOL_CAGE/cgroup.procs" ]] || p1_fail "step 4: process remains in tool cage: $P1_TOOL_CAGE"
	p1_fail "step 4: tool cage directory remains: $P1_TOOL_CAGE"
fi
p1_pass "step 4: lab.stop left no tool netns, veth, cage, socket/options directory, or cage process"

echo "P1 PASS: tool lifecycle, hot fabric attach/detach, crash sweep, and clean stop completed on this Linux target."
